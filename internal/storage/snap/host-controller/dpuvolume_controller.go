/*
Copyright 2025 NVIDIA

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

package hostcontroller

import (
	"context"
	"errors"
	"fmt"
	"maps"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	utilsPredicates "github.com/nvidia/doca-platform/pkg/utils/predicates"

	"github.com/fluxcd/pkg/runtime/patch"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// DPUVolumeReconciler reconciles a DPUVolume object
type DPUVolumeReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	controller  controller.Controller
	RemoteCache *dpucluster.RemoteCache
	Options     Options
}

const (
	dpuVolumeControllerName    = "dpuvolumecontroller"
	storageParametersPolicyKey = "policy"
	// annotation to save reference to the DPUVolume in objects in DPUClusters that are created by this controller
	dpuVolumeOwnedByAnnotation = "storage.dpu.nvidia.com/owned-by-dpuvolume"
)

// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=dpuvolumes,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=dpuvolumes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=dpuvolumes/finalizers,verbs=update
// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=dpuvolumeattachments,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=dpuvolumeattachments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=dpuvolumeattachments/finalizers,verbs=update
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpuclusters,verbs=get;list;watch

// Reconcile reconciles changes in a DPUVolume.
//
//nolint:dupl
func (r *DPUVolumeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Reconciling")

	dpuCluster := &provisioningv1.DPUCluster{}
	targetDPUCluster := r.Options.DPUCluster
	err := r.Get(ctx, targetDPUCluster, dpuCluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get DPU cluster %s: %w", targetDPUCluster.String(), err)
	}
	if err := r.watchVolumesInDPUCluster(ctx, targetDPUCluster); err != nil {
		if errors.Is(err, dpucluster.ErrDPUClusterNotConnected) {
			reqLog.Info("cluster is not connected, requeue", "cluster", targetDPUCluster.String())
			return ctrl.Result{Requeue: true, RequeueAfter: requeueIntervalOnDpuClusterNotConnected}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to start watch for DPU cluster %s: %w", targetDPUCluster.String(), err)
	}

	dpuClusterClient, err := r.RemoteCache.GetClient(targetDPUCluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get client for DPU cluster %s: %w", targetDPUCluster.String(), err)
	}

	dpuVolume := &storagev1.DPUVolume{}
	if err := r.Client.Get(ctx, req.NamespacedName, dpuVolume); err != nil {
		if apierrors.IsNotFound(err) {
			// DPUVolume is not found, but we need to ensure that the corresponding
			// Volume in the DPU cluster is also removed to avoid orphaned resources.
			return cleanupOrphanedObject(ctx, dpuClusterClient,
				r.getVolumeNameFromDPUVolumeName(req.NamespacedName),
				&storagev1.Volume{},
				"Volume")
		}
		return ctrl.Result{}, err
	}

	patcher := patch.NewSerialPatcher(dpuVolume, r.Client)

	r.initializeStatus(dpuVolume)
	conditions.EnsureConditions(dpuVolume, storagev1.DPUVolumeConditions)

	// Defer a patch call to always patch the object when Reconcile exits.
	defer func() {
		r.finalizeConditions(dpuVolume, reterr)
		reqLog.Info("Patching")
		if err := patcher.Patch(ctx, dpuVolume,
			patch.WithFieldOwner(dpuVolumeControllerName),
			patch.WithStatusObservedGeneration{},
			patch.WithOwnedConditions{Conditions: conditions.TypesAsStrings(storagev1.DPUVolumeConditions)},
		); kerrors.FilterOut(err, apierrors.IsNotFound) != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	// Handle deletion reconciliation loop.
	if !dpuVolume.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, dpuClusterClient, dpuVolume)
	}

	// Add finalizer if not set.
	if !controllerutil.ContainsFinalizer(dpuVolume, storagev1.DPUVolumeFinalizer) {
		reqLog.Info("Adding finalizer")
		controllerutil.AddFinalizer(dpuVolume, storagev1.DPUVolumeFinalizer)
		return ctrl.Result{}, nil
	}

	return r.reconcile(ctx, dpuClusterClient, dpuVolume)
}

// initializeStatus initializes the status of the DPUVolume object
func (r *DPUVolumeReconciler) initializeStatus(dpuVolume *storagev1.DPUVolume) {
	if dpuVolume.Status.State == nil {
		dpuVolume.Status.State = &storagev1.DPUVolumeState{}
	}
	if dpuVolume.Status.Phase == nil {
		dpuVolume.Status.Phase = ptr.To(storagev1.DPUVolumePhasePending)
	}
}

// finalizeConditions finalizes the conditions of the DPUVolume object
func (r *DPUVolumeReconciler) finalizeConditions(dpuVolume *storagev1.DPUVolume, err error) {
	// in case of any error set ConditionDPUVolumeReconciled to false with error reason
	if err != nil {
		conditions.AddFalse(dpuVolume, storagev1.ConditionDPUVolumeReconciled,
			conditions.ReasonError, conditions.ConditionMessage(err.Error()))
	}
	conditions.SetSummary(dpuVolume)
}

// reconcile handles the main reconciliation loop
//
//nolint:unparam
func (r *DPUVolumeReconciler) reconcile(ctx context.Context, dpuClusterClient client.Client, dpuVolume *storagev1.DPUVolume) (ctrl.Result, error) {
	reqLog := ctrllog.FromContext(ctx)

	volumeNamespacedName := r.getVolumeNameFromDPUVolumeName(client.ObjectKeyFromObject(dpuVolume))
	desiredVolume := r.getDesiredVolumeFromDPUVolume(dpuVolume, volumeNamespacedName)

	apiVolume := &storagev1.Volume{}
	err := dpuClusterClient.Get(ctx, volumeNamespacedName, apiVolume)
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, fmt.Errorf("failed to read Volume from DPU cluster: %w", err)
	}
	if apierrors.IsNotFound(err) {
		reqLog.Info("Volume not found in the DPU cluster, creating")
		apiVolume = desiredVolume
		if err := dpuClusterClient.Create(ctx, apiVolume); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to create Volume in DPU cluster: %w", err)
		}
	} else {
		reqLog.Info("Volume found in the DPU cluster")
		// Check if Volume is being deleted in the DPU cluster.
		// This is an edge case that may occur due to manual intervention (e.g. manual deletion of the Volume in the DPU cluster).
		// In this case we wait for deletion to complete and then recreate the Volume.
		if !apiVolume.ObjectMeta.DeletionTimestamp.IsZero() {
			reqLog.Info("Volume in the DPU cluster is deleting")
			r.setAwaitingDeletion(dpuVolume, nil)
			return ctrl.Result{}, nil
		}
		if !r.validateExistingVolumeSpec(desiredVolume, apiVolume) {
			reqLog.Info("Volume in the DPU cluster has different parameters, recreate")
			if err := dpuClusterClient.Delete(ctx, apiVolume); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to delete Volume in DPU cluster: %w", err)
			}
			r.setAwaitingDeletion(dpuVolume, nil)
			return ctrl.Result{}, nil
		}
	}
	r.updateDPUVolumeStatusFromVolume(dpuVolume, apiVolume)
	conditions.AddTrue(dpuVolume, storagev1.ConditionDPUVolumeReconciled)
	return ctrl.Result{}, nil
}

// getDesiredVolumeFromDPUVolume creates a desired Volume object from a DPUVolume specification
func (r *DPUVolumeReconciler) getDesiredVolumeFromDPUVolume(dpuVolume *storagev1.DPUVolume, volumeNamespacedName types.NamespacedName) *storagev1.Volume {
	parameters := maps.Clone(dpuVolume.Spec.Parameters)
	if parameters == nil {
		parameters = map[string]string{}
	}
	parameters[storageParametersPolicyKey] = dpuVolume.Spec.DPUStoragePolicyName
	vol := &storagev1.Volume{
		ObjectMeta: metav1.ObjectMeta{
			Name:      volumeNamespacedName.Name,
			Namespace: volumeNamespacedName.Namespace,
		},
		Spec: storagev1.VolumeSpec{
			StorageParameters: parameters,
			Request: storagev1.VolumeRequest{
				VolumeMode:  dpuVolume.Spec.VolumeMode,
				AccessModes: dpuVolume.Spec.AccessModes,
			},
		},
	}
	if dpuVolume.Spec.Resources.Requests.Storage() != nil {
		vol.Spec.Request.CapacityRange.Request = *dpuVolume.Spec.Resources.Requests.Storage()
	}
	if dpuVolume.Spec.Resources.Limits.Storage() != nil {
		vol.Spec.Request.CapacityRange.Limit = *dpuVolume.Spec.Resources.Limits.Storage()
	}
	return vol
}

// updateDPUVolumeStatusFromVolume updates the status of the DPUVolume object from the state of the Volume object in the DPU cluster
func (r *DPUVolumeReconciler) updateDPUVolumeStatusFromVolume(dpuVolume *storagev1.DPUVolume, volume *storagev1.Volume) {
	// reset the status to make sure that we are in sync with the Volume object state in the DPU cluster
	dpuVolume.Status.State = &storagev1.DPUVolumeState{}
	dpuVolume.Status.Phase = ptr.To(storagev1.DPUVolumePhasePending)

	if volume.Status.State == storagev1.VolumeStateAvailable {
		dpuVolume.Status.Phase = ptr.To(storagev1.DPUVolumePhaseBound)
	}

	dpuVolume.Status.State.DPUCluster = &storagev1.ObjectReference{
		Name:      r.Options.DPUCluster.Name,
		Namespace: r.Options.DPUCluster.Namespace,
	}
	dpuVolume.Status.State.Parameters = maps.Clone(volume.Spec.StorageParameters)
	maps.Copy(dpuVolume.Status.State.Parameters, volume.Spec.StoragePolicyParameters)

	if volume.Spec.VolumeSpecDPU.StorageVendorName != "" {
		dpuVolume.Status.State.SelectedDPUStorageVendorName = &volume.Spec.VolumeSpecDPU.StorageVendorName
	}
	if volume.Spec.VolumeSpecDPU.StorageVendorPluginName != "" {
		dpuVolume.Status.State.StorageVendorPluginName = &volume.Spec.VolumeSpecDPU.StorageVendorPluginName
	}
	if volume.Spec.VolumeSpecDPU.CSIReference.StorageClassName != "" {
		dpuVolume.Status.State.StorageClassName = &volume.Spec.VolumeSpecDPU.CSIReference.StorageClassName
	}
	if volume.Spec.VolumeSpecDPU.CSIReference.CSIDriverName != "" {
		dpuVolume.Status.State.CSIDriverName = &volume.Spec.VolumeSpecDPU.CSIReference.CSIDriverName
	}
	if volume.Spec.VolumeSpecDPU.CSIReference.PVCRef != nil &&
		volume.Spec.VolumeSpecDPU.CSIReference.PVCRef.Name != "" {
		dpuVolume.Status.State.PersistentVolumeClaimRef = &storagev1.ObjectReference{
			Name:      volume.Spec.VolumeSpecDPU.CSIReference.PVCRef.Name,
			Namespace: volume.Spec.VolumeSpecDPU.CSIReference.PVCRef.Namespace,
		}
	}
	dpuVolume.Status.State.VolumeInfo = &storagev1.VolumeInfo{
		VolumeAttributes: volume.Spec.VolumeSpecDPU.VolumeAttributes,
		Capacity:         corev1.ResourceList{corev1.ResourceStorage: volume.Spec.Request.CapacityRange.Request},
		AccessModes:      volume.Spec.Request.AccessModes,
		VolumeMode:       volume.Spec.Request.VolumeMode,
	}

	if dpuVolume.Status.State.SelectedDPUStorageVendorName != nil && dpuVolume.Status.State.StorageVendorPluginName != nil {
		conditions.AddTrue(dpuVolume, storagev1.ConditionDPUVolumeScheduled)
	} else {
		conditions.AddFalse(dpuVolume, storagev1.ConditionDPUVolumeScheduled, conditions.ReasonPending, "volume is not scheduled yet")
	}
	if dpuVolume.Status.Phase != nil && *dpuVolume.Status.Phase == storagev1.DPUVolumePhaseBound {
		conditions.AddTrue(dpuVolume, storagev1.ConditionDPUVolumeBound)
	} else {
		conditions.AddFalse(dpuVolume, storagev1.ConditionDPUVolumeBound, conditions.ReasonPending, "volume is not bound yet")
	}
}

//nolint:unparam
func (r *DPUVolumeReconciler) reconcileDelete(ctx context.Context, dpuClusterClient client.Client, dpuVolume *storagev1.DPUVolume) (ctrl.Result, error) {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Reconciling delete")

	dpuVolumeAttachments, err := r.getDPUVolumeAttachmentsByVolumeName(ctx, dpuVolume.Name)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get DPUVolumeAttachments for volume %s: %w", dpuVolume.Name, err)
	}
	if len(dpuVolumeAttachments) > 0 {
		reqLog.Info("can't remove attached volume, waiting for detachment")
		r.setAwaitingDeletion(dpuVolume, ptr.To("volume is attached"))
		return ctrl.Result{}, nil
	}
	r.setAwaitingDeletion(dpuVolume, nil)
	apiVolume := &storagev1.Volume{}
	err = dpuClusterClient.Get(ctx, r.getVolumeNameFromDPUVolumeName(client.ObjectKeyFromObject(dpuVolume)), apiVolume)
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, fmt.Errorf("failed to get Volume from DPU cluster: %w", err)
	}
	if apierrors.IsNotFound(err) {
		reqLog.Info("Volume in the DPU cluster not found, removing finalizer")
		controllerutil.RemoveFinalizer(dpuVolume, storagev1.DPUVolumeFinalizer)
		return ctrl.Result{}, nil
	}
	if !apiVolume.GetDeletionTimestamp().IsZero() {
		reqLog.Info("Volume in the DPU cluster is already deleting")
		return ctrl.Result{}, nil
	}
	reqLog.Info("Delete Volume")
	err = dpuClusterClient.Delete(ctx, apiVolume)
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, fmt.Errorf("failed to delete Volume from DPU cluster: %w", err)
	}
	if apierrors.IsNotFound(err) {
		reqLog.Info("Volume in the DPU cluster not found, removing finalizer")
		controllerutil.RemoveFinalizer(dpuVolume, storagev1.DPUVolumeFinalizer)
		return ctrl.Result{}, nil
	}
	reqLog.Info("Volume in the DPU cluster is deleting")
	return ctrl.Result{}, nil
}

// reset the state in the DPUVolume CR that is build from the state of the Volume CR in DPU cluster and
// set ConditionDPUVolumeReconciled to ReasonAwaitingDeletion,
// if msg is not nil, set it as a message for the condition. Default message is "Volume is deleting".
func (r *DPUVolumeReconciler) setAwaitingDeletion(dpuVolume *storagev1.DPUVolume, msg *string) {
	conditionMsg := "Volume is deleting"
	if msg != nil {
		conditionMsg = *msg
	}
	conditions.AddFalse(dpuVolume, storagev1.ConditionDPUVolumeReconciled,
		conditions.ReasonAwaitingDeletion, conditions.ConditionMessage(conditionMsg))
}

// returns name of the Volume CR in the DPU cluster for the provided DPUVolume name from the host cluster.
func (r *DPUVolumeReconciler) getVolumeNameFromDPUVolumeName(dpuVolumeName types.NamespacedName) types.NamespacedName {
	return types.NamespacedName{Namespace: r.Options.TargetNamespace, Name: dpuVolumeName.Name}
}

// check that spec of the Volume has the desired spec
func (r *DPUVolumeReconciler) validateExistingVolumeSpec(desiredVolume *storagev1.Volume, actualVolume *storagev1.Volume) bool {
	return equality.Semantic.DeepEqual(desiredVolume.Spec.StorageParameters, actualVolume.Spec.StorageParameters) &&
		equality.Semantic.DeepEqual(desiredVolume.Spec.Request, actualVolume.Spec.Request)
}

// getDPUVolumeAttachmentsByVolumeName returns DPUVolumeAttachments that reference the specified DPUVolume name.
func (r *DPUVolumeReconciler) getDPUVolumeAttachmentsByVolumeName(ctx context.Context, dpuVolumeName string) ([]storagev1.DPUVolumeAttachment, error) {
	dpuVolumeAttachmentList := &storagev1.DPUVolumeAttachmentList{}
	if err := r.Client.List(ctx, dpuVolumeAttachmentList,
		client.MatchingFields{dpuVolumeNameIndexKey: dpuVolumeName}, client.InNamespace(r.Options.Namespace)); err != nil {
		return nil, err
	}
	return dpuVolumeAttachmentList.Items, nil
}

// enqueueAllDPUVolumes returns a MapFunc that enqueues all DPUVolume objects for reconciliation
func (r *DPUVolumeReconciler) enqueueAllDPUVolumes() handler.MapFunc {
	return r.getEnqueueFunction(nil)
}

// enqueueDPUVolumeByVolume returns a MapFunc that maps Volume objects to corresponding DPUVolume reconcile requests
func (r *DPUVolumeReconciler) enqueueDPUVolumeByVolume() handler.MapFunc {
	return func(ctx context.Context, o client.Object) []reconcile.Request {
		vol, ok := o.(*storagev1.Volume)
		if !ok {
			return nil
		}
		if vol.GetNamespace() != r.Options.TargetNamespace {
			// ignore the object if it is originated not from the target namespace
			return nil
		}
		return []reconcile.Request{{NamespacedName: client.ObjectKey{Namespace: r.Options.Namespace, Name: vol.Name}}}
	}
}

// enqueueDPUVolumeByDPUVolumeAttachment returns a MapFunc that enqueues DPUVolume objects referenced by DPUVolumeAttachment
func (r *DPUVolumeReconciler) enqueueDPUVolumeByDPUVolumeAttachment() handler.MapFunc {
	return func(ctx context.Context, o client.Object) []reconcile.Request {
		dpuVolumeAttachment, ok := o.(*storagev1.DPUVolumeAttachment)
		if !ok {
			return nil
		}
		return []reconcile.Request{{NamespacedName: client.ObjectKey{Namespace: r.Options.Namespace, Name: dpuVolumeAttachment.Spec.DPUVolumeName}}}
	}
}

// getEnqueueFunction returns a function that can be used to enqueue DPUVolume filtered by the filter function.
// if the filter function is nil, all DPUVolume are enqueued
func (r *DPUVolumeReconciler) getEnqueueFunction(
	filter func(o client.Object, dpuVolume *storagev1.DPUVolume) bool) handler.MapFunc {
	return func(ctx context.Context, o client.Object) []ctrl.Request {
		result := []ctrl.Request{}
		dpuVolumeList := &storagev1.DPUVolumeList{}
		if err := r.Client.List(ctx, dpuVolumeList, client.InNamespace(r.Options.Namespace)); err != nil {
			return nil
		}
		for _, m := range dpuVolumeList.Items {
			if filter == nil || filter(o, &m) {
				name := client.ObjectKey{Namespace: m.Namespace, Name: m.Name}
				result = append(result, ctrl.Request{NamespacedName: name})
			}
		}
		return result
	}
}

func (r *DPUVolumeReconciler) watchVolumesInDPUCluster(ctx context.Context, dpuCluster client.ObjectKey) error {
	return r.RemoteCache.Watch(ctx, dpuCluster, dpucluster.NewWatcher(dpucluster.WatcherOptions{
		Name:         "dpuvolume-watch-volume",
		Watcher:      r.controller,
		Kind:         &storagev1.Volume{},
		EventHandler: handler.EnqueueRequestsFromMapFunc(r.enqueueDPUVolumeByVolume()),
	}))
}

// SetupWithManager sets up the controller with the Manager.
func (r *DPUVolumeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	c, err := ctrl.NewControllerManagedBy(mgr).
		For(&storagev1.DPUVolume{}).
		Watches(&provisioningv1.DPUCluster{}, handler.EnqueueRequestsFromMapFunc(r.enqueueAllDPUVolumes())).
		// watch for deletion of DPUVolumeAttachments to unblock removal of the volume when it is detached
		Watches(&storagev1.DPUVolumeAttachment{}, handler.EnqueueRequestsFromMapFunc(r.enqueueDPUVolumeByDPUVolumeAttachment()),
			builder.WithPredicates(utilsPredicates.PredicateFuncsByEventTypes(event.DeleteEvent{}))).
		Build(r)
	if err != nil {
		return err
	}
	r.controller = c
	return nil
}
