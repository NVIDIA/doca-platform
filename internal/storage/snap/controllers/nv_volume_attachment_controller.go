/*
Copyright 2024 NVIDIA

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"

	snapstoragev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/internal/storage"

	"github.com/fluxcd/pkg/runtime/patch"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	nvVolumeAttachmentFinalizer      = "storage.nvidia.com/attachment-protection"
	nvVolumeAttachmentControllerName = "nvVolumeAttachmentController"
)

// NVVolumeAttachment reconciles a VolumeAttachment object
type NVVolumeAttachment struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=volumeattachments,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=volumeattachments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=volumeattachments/finalizers,verbs=update
// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=svvolumeattachments,verbs=get;list;watch;update;patch;create;delete
// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=svvolumeattachments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;update;patch;create;delete

// nolint
func (r *NVVolumeAttachment) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := ctrllog.FromContext(ctx)
	log.Info("Reconciling")

	nvVolumeAttachment := &snapstoragev1.VolumeAttachment{}
	if err := r.Get(ctx, req.NamespacedName, nvVolumeAttachment); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	patcher := patch.NewSerialPatcher(nvVolumeAttachment, r.Client)
	// Defer a patch call to always patch the object when Reconcile exits.
	defer func() {
		log.Info("Patching")
		if err := patcher.Patch(ctx, nvVolumeAttachment,
			patch.WithFieldOwner(nvVolumeAttachmentControllerName),
			patch.WithStatusObservedGeneration{},
		); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	// Handle deletion reconciliation loop.
	if !nvVolumeAttachment.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, nvVolumeAttachment)
	}
	// Add finalizer if not set.
	if !controllerutil.ContainsFinalizer(nvVolumeAttachment, nvVolumeAttachmentFinalizer) {
		log.Info("Adding finalizer")
		controllerutil.AddFinalizer(nvVolumeAttachment, nvVolumeAttachmentFinalizer)
		return ctrl.Result{}, nil
	}

	return r.reconcile(ctx, nvVolumeAttachment)
}

// SetupWithManager sets up the controller with the Manager.
func (r *NVVolumeAttachment) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&snapstoragev1.VolumeAttachment{}).
		Watches(&snapstoragev1.SVVolumeAttachment{},
			handler.EnqueueRequestsFromMapFunc(r.persistentSVVolumeAttachmentToReq)).
		Complete(r)
}

func (r *NVVolumeAttachment) persistentSVVolumeAttachmentToReq(ctx context.Context, resource client.Object) []reconcile.Request {
	volumeAttachmentList := &snapstoragev1.VolumeAttachmentList{}
	if err := r.List(ctx, volumeAttachmentList); err != nil {
		return nil
	}
	requests := []reconcile.Request{}
	for _, item := range volumeAttachmentList.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      item.GetName(),
				Namespace: item.GetNamespace(),
			}})
	}
	return requests
}

func (r *NVVolumeAttachment) reconcileDelete(ctx context.Context, nvVolumeAttachment *snapstoragev1.VolumeAttachment) (ctrl.Result, error) {

	// Waiting for status.dpu.attached field is set to False
	if nvVolumeAttachment.Status.DPU.Attached {
		return ctrl.Result{RequeueAfter: storage.RequeueInterval}, nil
	}

	// Delete SV-VolumeAttachment if exist
	if nvVolumeAttachment.Spec.VolumeAttachmentRef != nil {
		svVolumeAttachment := &snapstoragev1.SVVolumeAttachment{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      nvVolumeAttachment.Spec.VolumeAttachmentRef.Name,
			Namespace: nvVolumeAttachment.Spec.VolumeAttachmentRef.Namespace,
		}, svVolumeAttachment); err != nil {
			if apierrors.IsNotFound(err) {
				// SV-VolumeAttachment is already deleted
				nvVolumeAttachment.Status.StorageAttached = false
				controllerutil.RemoveFinalizer(nvVolumeAttachment, nvVolumeAttachmentFinalizer)
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, err
		}

		deleteSVVolumeAttachment := &snapstoragev1.SVVolumeAttachment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nvVolumeAttachment.Spec.VolumeAttachmentRef.Name,
				Namespace: nvVolumeAttachment.Spec.VolumeAttachmentRef.Namespace,
			},
		}
		if err := r.Delete(ctx, deleteSVVolumeAttachment); err != nil {
			if apierrors.IsNotFound(err) {
				// SV-VolumeAttachment is already deleted
				nvVolumeAttachment.Status.StorageAttached = false
				controllerutil.RemoveFinalizer(nvVolumeAttachment, nvVolumeAttachmentFinalizer)
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, err
		}
	}

	controllerutil.RemoveFinalizer(nvVolumeAttachment, nvVolumeAttachmentFinalizer)
	return ctrl.Result{}, nil
}

func (r *NVVolumeAttachment) reconcile(ctx context.Context, nvVolumeAttachment *snapstoragev1.VolumeAttachment) (ctrl.Result, error) {
	// Retrieve the NV-Volume object to get the storage vendor name
	if nvVolumeAttachment.Spec.Source.VolumeRef == nil {
		return ctrl.Result{}, fmt.Errorf("the reference of NV-Volume is nil in NV-VolumeAttachment %s/%s",
			nvVolumeAttachment.Namespace, nvVolumeAttachment.Name)
	}
	nvVolume := &snapstoragev1.Volume{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      nvVolumeAttachment.Spec.Source.VolumeRef.Name,
		Namespace: nvVolumeAttachment.Spec.Source.VolumeRef.Namespace,
	}, nvVolume); err != nil {
		return ctrl.Result{}, err
	}
	// Set spec.parameters with NV-StoragePolicy.storageParameters
	nvVolumeAttachment.Spec.Parameters = nvVolume.Spec.StoragePolicyParameters

	// Check the csiDriver.Spec.AttachRequired to see if need create SV-VolumeAttachment
	if nvVolumeAttachment.Spec.VolumeAttachmentRef == nil {
		// Retrieve the CSI driver object to check if require attachment
		csiDriverName := nvVolume.Spec.VolumeSpecDPU.CSIReference.CSIDriverName
		csiDriver := &storagev1.CSIDriver{}
		if err := r.Get(ctx, types.NamespacedName{
			Name: csiDriverName,
		}, csiDriver); err != nil {
			return ctrl.Result{}, err
		}

		// Check if need to create SV-VolumeAttachment object
		if *csiDriver.Spec.AttachRequired {
			if nvVolume.Spec.VolumeSpecDPU.CSIReference.PVCRef == nil {
				return ctrl.Result{}, fmt.Errorf("the reference of PVC is nil in NV-Volume %s/%s",
					nvVolume.Namespace, nvVolume.Name)
			}

			// Retrieve the PVC object to get the pv name
			pvcObject := &corev1.PersistentVolumeClaim{}
			if err := r.Get(ctx, types.NamespacedName{
				Name:      nvVolume.Spec.VolumeSpecDPU.CSIReference.PVCRef.Name,
				Namespace: nvVolume.Spec.VolumeSpecDPU.CSIReference.PVCRef.Namespace,
			}, pvcObject); err != nil {
				return ctrl.Result{}, err
			}

			// Create SV-VolumeAttachment object
			pvName := pvcObject.Spec.VolumeName
			svVolumeAttachment := &snapstoragev1.SVVolumeAttachment{
				ObjectMeta: metav1.ObjectMeta{
					Name:        nvVolumeAttachment.Name,
					Namespace:   nvVolumeAttachment.Namespace,
					Labels:      make(map[string]string),
					Annotations: make(map[string]string),
				},
				Spec: storagev1.VolumeAttachmentSpec{
					NodeName: nvVolumeAttachment.Spec.NodeName,
					Source: storagev1.VolumeAttachmentSource{
						PersistentVolumeName: &pvName,
					},
				},
			}
			if err := r.Create(ctx, svVolumeAttachment); err != nil {
				return ctrl.Result{}, err
			}

			// Set VolumeAttachmentRef
			nvVolumeAttachment.Spec.VolumeAttachmentRef = &snapstoragev1.ObjectRef{
				APIVersion: snapstoragev1.GroupVersion.String(),
				Kind:       snapstoragev1.SVVolumeAttachmentKind,
				Name:       svVolumeAttachment.Name,
				Namespace:  svVolumeAttachment.Namespace,
			}
			return ctrl.Result{}, nil
		} else {
			// No need to create SV-VolumeAttachment
			// Set status.storageAttached to True
			nvVolumeAttachment.Status.StorageAttached = true
			return ctrl.Result{}, nil
		}
	} else {
		// SV-VolumeAttachment is created, check if status.storageAttached is True

		// Retrieve the SV-VolumeAttachment object
		svVolumeAttachment := &snapstoragev1.SVVolumeAttachment{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      nvVolumeAttachment.Spec.VolumeAttachmentRef.Name,
			Namespace: nvVolumeAttachment.Spec.VolumeAttachmentRef.Namespace,
		}, svVolumeAttachment); err != nil {
			return ctrl.Result{}, err
		}

		if svVolumeAttachment.Status.Attached {
			nvVolumeAttachment.Status.StorageAttached = true
			return ctrl.Result{}, nil
		}
		return ctrl.Result{RequeueAfter: storage.RequeueInterval}, nil
	}
}
