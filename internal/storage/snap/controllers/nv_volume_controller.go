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
	"errors"
	"fmt"
	"math"
	"math/rand"
	"time"

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
	nvVolumeFinalizer      = "storage.nvidia.com/volume-protection"
	nvVolumeControllerName = "nvVolumeController"
)

// NVVolume reconciles a Volume object
type NVVolume struct {
	client.Client
	Scheme          *runtime.Scheme
	Recorder        record.EventRecorder
	ConfigNamespace string
}

// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=volumes,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=volumes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=volumes/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=persistentvolumes,verbs=get;list;watch;update;patch

// nolint
func (r *NVVolume) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := ctrllog.FromContext(ctx)
	log.Info("Reconciling")

	nvVolume := &snapstoragev1.Volume{}
	if err := r.Get(ctx, req.NamespacedName, nvVolume); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	patcher := patch.NewSerialPatcher(nvVolume, r.Client)
	// Defer a patch call to always patch the object when Reconcile exits.
	defer func() {
		log.Info("Patching")
		if err := patcher.Patch(ctx, nvVolume,
			patch.WithFieldOwner(nvVolumeControllerName),
			patch.WithStatusObservedGeneration{},
		); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	// Handle deletion reconciliation loop.
	if !nvVolume.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, nvVolume)
	}
	// Add finalizer if not set.
	if !controllerutil.ContainsFinalizer(nvVolume, nvVolumeFinalizer) {
		log.Info("Adding finalizer")
		controllerutil.AddFinalizer(nvVolume, nvVolumeFinalizer)
		return ctrl.Result{}, nil
	}

	return r.reconcile(ctx, nvVolume)
}

// SetupWithManager sets up the controller with the Manager.
func (r *NVVolume) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&snapstoragev1.Volume{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Watches(&corev1.PersistentVolume{},
			handler.EnqueueRequestsFromMapFunc(r.persistentNVVolumeToReq)).
		Complete(r)
}

func (r *NVVolume) persistentNVVolumeToReq(ctx context.Context, resource client.Object) []reconcile.Request {
	nvVolumeList := &snapstoragev1.VolumeList{}
	if err := r.List(ctx, nvVolumeList); err != nil {
		return nil
	}
	requests := []reconcile.Request{}
	for _, item := range nvVolumeList.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      item.GetName(),
				Namespace: item.GetNamespace(),
			}})
	}
	return requests
}

func (r *NVVolume) reconcile(ctx context.Context, nvVolume *snapstoragev1.Volume) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)

	nvVolume.Status.State = snapstoragev1.VolumeStateInProgress

	storagePolicyName, ok := nvVolume.Spec.StorageParameters["policy"]
	if !ok {
		return ctrl.Result{}, errors.New("missing 'policy' parameter in storageParameters")
	}
	storagePolicy := &snapstoragev1.StoragePolicy{}
	storagePolicyNN := types.NamespacedName{
		Name:      storagePolicyName,
		Namespace: r.ConfigNamespace,
	}
	if err := r.Get(ctx, storagePolicyNN, storagePolicy); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info(fmt.Sprintf("storage policy %s/%s not found", storagePolicyNN.Namespace, storagePolicyNN.Name))
			return ctrl.Result{RequeueAfter: storage.RequeueInterval}, nil
		}
		return ctrl.Result{}, err
	}
	if storagePolicy.Status.State != snapstoragev1.StorageVendorStateValid {
		return ctrl.Result{}, fmt.Errorf("the state of storage policy %s/%s is %s",
			storagePolicy.Namespace, storagePolicy.Name, storagePolicy.Status.State)
	}

	// set volume.Spec.StoragePolicyRef and volume.Spec.StoragePolicyParameters
	if nvVolume.Spec.StoragePolicyRef == nil {
		log.Info("Set StoragePolicyRef and StoragePolicyParameters")
		nvVolume.Spec.StoragePolicyRef = &snapstoragev1.ObjectRef{
			APIVersion: snapstoragev1.GroupVersion.String(),
			Kind:       snapstoragev1.StoragePolicyKind,
			Name:       storagePolicy.Name,
			Namespace:  storagePolicy.Namespace,
		}
		nvVolume.Spec.StoragePolicyParameters = storagePolicy.Spec.StorageParameters
	}

	// set volume.Spec.VolumeSpecDPU.CSIReference and create PVC object
	if nvVolume.Spec.VolumeSpecDPU.CSIReference.PVCRef == nil {
		return r.handlePVC(ctx, nvVolume, storagePolicy)
	}

	// set volume.Spec.Volume and volume.Status.State according to PV object
	if nvVolume.Status.State != snapstoragev1.VolumeStateAvailable {
		pvList := &corev1.PersistentVolumeList{}
		if err := r.List(ctx, pvList); err != nil {
			return ctrl.Result{}, err
		}

		for _, pv := range pvList.Items {
			if pv.Spec.ClaimRef.Name == nvVolume.Spec.VolumeSpecDPU.CSIReference.PVCRef.Name &&
				pv.Spec.ClaimRef.Namespace == nvVolume.Spec.VolumeSpecDPU.CSIReference.PVCRef.Namespace &&
				pv.Status.Phase == corev1.VolumeBound {

				nvVolume.Spec.VolumeSpecDPU.ID = string(nvVolume.ObjectMeta.UID)
				nvVolume.Spec.VolumeSpecDPU.Capacity = *pv.Spec.Capacity.Storage()
				nvVolume.Spec.VolumeSpecDPU.AccessModes = pv.Spec.AccessModes
				nvVolume.Spec.VolumeSpecDPU.ReclaimPolicy = pv.Spec.PersistentVolumeReclaimPolicy
				if pv.Spec.CSI != nil {
					nvVolume.Spec.VolumeSpecDPU.VolumeAttributes = pv.Spec.CSI.VolumeAttributes
				} else {
					// This code should not be reached
					msg := fmt.Sprintf("the VolumeAttributes field of pv %s is nil, can not update NV-volume object %s/%s",
						pv.Name, nvVolume.Namespace, nvVolume.Name)
					return ctrl.Result{}, errors.New(msg)
				}
				if nvVolume.Labels == nil {
					nvVolume.Labels = map[string]string{}
				}
				nvVolume.Labels["volumeId"] = string(nvVolume.ObjectMeta.UID)
				nvVolume.Status.State = snapstoragev1.VolumeStateAvailable
				return ctrl.Result{}, nil
			}
		}
	}

	return ctrl.Result{}, nil
}

func (r *NVVolume) handlePVC(ctx context.Context, nvVolume *snapstoragev1.Volume, storagePolicy *snapstoragev1.StoragePolicy) (ctrl.Result, error) {
	storageVendorName, err := r.getStorageVendor(ctx, storagePolicy)
	if err != nil {
		return ctrl.Result{}, err
	}

	storageVendor := &snapstoragev1.StorageVendor{}
	storageVendorNN := types.NamespacedName{
		Name:      storageVendorName,
		Namespace: r.ConfigNamespace,
	}

	if err := r.Get(ctx, storageVendorNN, storageVendor); err != nil {
		return ctrl.Result{}, err
	}

	// create pvc
	owner := metav1.NewControllerRef(nvVolume, snapstoragev1.VolumeGroupVersionKind)
	pvcName := fmt.Sprintf("%s-%s", nvVolume.Name, "pvc")
	storageClassName := storageVendor.Spec.StorageClassName
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:            pvcName,
			Namespace:       nvVolume.Namespace,
			Labels:          make(map[string]string),
			Annotations:     make(map[string]string),
			OwnerReferences: []metav1.OwnerReference{*owner},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &storageClassName,
			VolumeMode:       nvVolume.Spec.Request.VolumeMode,
			AccessModes:      nvVolume.Spec.Request.AccessModes,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: nvVolume.Spec.Request.CapacityRange.Request,
				},
			},
		},
	}

	if err := r.Create(ctx, pvc); err != nil {
		return ctrl.Result{}, err
	}

	// Retrieve the storage class object to get the CSI driver name
	storageClass := &storagev1.StorageClass{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      storageClassName,
		Namespace: r.ConfigNamespace,
	}, storageClass); err != nil {
		return ctrl.Result{}, err
	}

	nvVolume.Spec.VolumeSpecDPU.CSIReference.CSIDriverName = storageClass.Provisioner
	nvVolume.Spec.VolumeSpecDPU.CSIReference.StorageClassName = storageClassName
	nvVolume.Spec.VolumeSpecDPU.CSIReference.PVCRef = &snapstoragev1.ObjectRef{
		Kind:       pvc.Kind,
		APIVersion: pvc.APIVersion,
		Name:       pvc.Name,
		Namespace:  pvc.Namespace,
	}

	// update volume.storageVendorName and volume.storageVendorName here
	// because it is hard to update these two fields from PV object
	nvVolume.Spec.VolumeSpecDPU.StorageVendorName = storageVendor.Name
	nvVolume.Spec.VolumeSpecDPU.StorageVendorPluginName = storageVendor.Spec.PluginName
	return ctrl.Result{}, nil
}

// find the storage vendor according to the storage policy algorithm
func (r *NVVolume) getStorageVendor(ctx context.Context, storagePolicy *snapstoragev1.StoragePolicy) (string, error) {
	vendors := storagePolicy.Spec.StorageVendors
	// only support LocalNVolumes and Random policy in Jan release
	switch storagePolicy.Spec.StorageSelectionAlg {
	case snapstoragev1.LocalNVolumes:
		return r.localNVolumesAlg(ctx, storagePolicy)
	case snapstoragev1.Random:
		return r.randomAlg(vendors)
	default:
		return r.localNVolumesAlg(ctx, storagePolicy)
	}
}

func (r *NVVolume) randomAlg(vendors []string) (string, error) {
	seed := time.Now().UnixNano()
	generator := rand.New(rand.NewSource(seed))
	randomNumber := generator.Intn(len(vendors))
	if randomNumber >= len(vendors) {
		// This code should not be reached
		return "", fmt.Errorf("the random number %v is greater than or equal to the number of vendors",
			randomNumber)
	}
	return vendors[randomNumber], nil
}

func (r *NVVolume) localNVolumesAlg(ctx context.Context, storagePolicy *snapstoragev1.StoragePolicy) (string, error) {
	nvVolumeList := &snapstoragev1.VolumeList{}
	if err := r.List(ctx, nvVolumeList); err != nil {
		return "", err
	}

	vendorCounterMap := make(map[string]int)
	for _, nvVolume := range nvVolumeList.Items {
		// filter the volumes which belongs to this storage policy
		if nvVolume.Spec.StoragePolicyRef != nil {
			if nvVolume.Spec.StoragePolicyRef.Name != storagePolicy.Name ||
				nvVolume.Spec.StoragePolicyRef.Namespace != storagePolicy.Namespace {
				continue
			}
			vendorCounterMap[nvVolume.Spec.VolumeSpecDPU.StorageVendorName]++
		}
	}

	// find the vendor with minimal number of volumes.
	min := math.MaxInt
	// if all vendor has no volume, will select the first vendor from storage policy vendor list
	vendor := storagePolicy.Spec.StorageVendors[0]
	for storageVendor, volumeCounter := range vendorCounterMap {
		if volumeCounter < min {
			min = volumeCounter
			vendor = storageVendor
		}
	}

	return vendor, nil
}

func (r *NVVolume) reconcileDelete(ctx context.Context, nvVolume *snapstoragev1.Volume) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)
	// check if there is any NV-VolumeAttachment refer this NV-Volume
	nvVolumeAttachmentList := &snapstoragev1.VolumeAttachmentList{}
	if err := r.List(ctx, nvVolumeAttachmentList); err != nil {
		return ctrl.Result{}, err
	}

	for _, volumeAttachment := range nvVolumeAttachmentList.Items {
		if volumeAttachment.Spec.Source.VolumeRef == nil {
			continue
		}
		if volumeAttachment.Spec.Source.VolumeRef.Name == nvVolume.Name &&
			volumeAttachment.Spec.Source.VolumeRef.Namespace == nvVolume.Namespace {
			log.Info(fmt.Sprintf("volume attachment %s/%s is refer to volume %s/%s",
				volumeAttachment.Namespace, volumeAttachment.Name, nvVolume.Namespace, nvVolume.Name))
			return ctrl.Result{RequeueAfter: storage.RequeueInterval}, nil
		}
	}

	// delete PVC if exist
	if nvVolume.Spec.VolumeSpecDPU.CSIReference.PVCRef != nil {
		pvcNN := types.NamespacedName{
			Name:      nvVolume.Spec.VolumeSpecDPU.CSIReference.PVCRef.Name,
			Namespace: nvVolume.Spec.VolumeSpecDPU.CSIReference.PVCRef.Namespace,
		}

		pvcObject := &corev1.PersistentVolumeClaim{}

		if err := r.Get(ctx, pvcNN, pvcObject); err != nil {
			if apierrors.IsNotFound(err) {
				log.Info("Removing finalizer")
				controllerutil.RemoveFinalizer(nvVolume, nvVolumeFinalizer)
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, err
		}

		deletePVC := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nvVolume.Spec.VolumeSpecDPU.CSIReference.PVCRef.Name,
				Namespace: nvVolume.Spec.VolumeSpecDPU.CSIReference.PVCRef.Namespace,
			},
		}

		if err := r.Delete(ctx, deletePVC); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}
