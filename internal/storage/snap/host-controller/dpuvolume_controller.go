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
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/dpuclusterhelper"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/indexers"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/utils"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/vendorselector"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/volumeprovisioner"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	utilsPredicates "github.com/nvidia/doca-platform/pkg/utils/predicates"

	"github.com/fluxcd/pkg/runtime/patch"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"
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

	dpuClusterHelper  dpuclusterhelper.DPUClusterHelper
	ownedByHelper     utils.OwnedByHelper
	vendorSelector    vendorselector.VendorSelector
	volumeProvisioner volumeprovisioner.VolumeProvisioner
}

const (
	dpuVolumeControllerName = "dpuvolumecontroller"
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
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete

// Reconcile manages DPUVolume lifecycle including vendor selection, volume provisioning, and cleanup.
//
//nolint:dupl
func (r *DPUVolumeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Reconciling")
	dpuVolume := &storagev1.DPUVolume{}
	if err := r.Client.Get(ctx, req.NamespacedName, dpuVolume); err != nil {
		if apierrors.IsNotFound(err) {
			reqLog.Info("DPUVolume not found, ensure that all resources are removed from DPUClusters")
			_, err = r.dpuClusterResourcesCleanup(ctx, nil, req.NamespacedName)
		}
		return FinalizeReconcileResult(ctrl.Result{}, err)
	}

	patcher := patch.NewSerialPatcher(dpuVolume, r.Client)

	r.initializeStatus(dpuVolume)
	conditions.EnsureConditions(dpuVolume, storagev1.DPUVolumeConditions)

	// Defer a patch call to always patch the object when Reconcile exits.
	defer func() {
		r.finalizeConditions(dpuVolume, reterr)
		reqLog.Info("Patching")
		patchErr := patcher.Patch(ctx, dpuVolume,
			patch.WithFieldOwner(dpuVolumeControllerName),
			patch.WithStatusObservedGeneration{},
			patch.WithOwnedConditions{Conditions: conditions.TypesAsStrings(storagev1.DPUVolumeConditions)},
		)
		if kerrors.FilterOut(patchErr, apierrors.IsNotFound) != nil {
			reterr = kerrors.NewAggregate([]error{reterr, patchErr})
		}
		if patchErr == nil {
			// wait for the cache to be updated with the expected volume state, we wait only if patching was successful
			if err := r.waitForStateUpdateInCache(ctx, dpuVolume); err != nil {
				reterr = kerrors.NewAggregate([]error{reterr, err})
			}
		}
	}()

	// Handle deletion reconciliation loop.
	if !dpuVolume.ObjectMeta.DeletionTimestamp.IsZero() {
		return FinalizeReconcileResult(r.reconcileDelete(ctx, dpuVolume))
	}

	// Add finalizer if not set.
	if !controllerutil.ContainsFinalizer(dpuVolume, storagev1.DPUVolumeFinalizer) {
		reqLog.Info("Adding finalizer")
		controllerutil.AddFinalizer(dpuVolume, storagev1.DPUVolumeFinalizer)
		return ctrl.Result{}, nil
	}

	return FinalizeReconcileResult(r.reconcile(ctx, dpuVolume))
}

// waitForStateUpdateInCache waits until the cache is updated with the expected volume state
func (r *DPUVolumeReconciler) waitForStateUpdateInCache(ctx context.Context, dpuVolume *storagev1.DPUVolume) error {
	reqLog := ctrllog.FromContext(ctx)
	err := wait.PollUntilContextTimeout(ctx, cacheUpdateCheckInterval, cacheUpdateTimeout,
		true, func(ctx context.Context) (bool, error) {
			dpuVolumeFromCache := &storagev1.DPUVolume{}
			err := r.Get(ctx, client.ObjectKeyFromObject(dpuVolume), dpuVolumeFromCache)
			if err != nil {
				return false, err
			}
			return equality.Semantic.DeepEqual(dpuVolume.Status.State, dpuVolumeFromCache.Status.State), nil
		})
	if err != nil {
		reqLog.Error(err, "Failed to wait for cache update")
		return err
	}
	reqLog.Info("Cache updated with the expected volume state")
	return nil
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

// isDPUVolumeVendorSelected checks if the DPUVolume vendor is selected (all required fields are set)
func (r *DPUVolumeReconciler) isDPUVolumeVendorSelected(dpuVolume *storagev1.DPUVolume) bool {
	return dpuVolume.Status.State != nil &&
		dpuVolume.Status.State.DPUCluster != nil &&
		dpuVolume.Status.State.DPUCluster.Name != "" &&
		dpuVolume.Status.State.DPUCluster.Namespace != "" &&
		dpuVolume.Status.State.SelectedDPUStorageVendorName != nil && *dpuVolume.Status.State.SelectedDPUStorageVendorName != "" &&
		dpuVolume.Status.State.StorageVendorPluginName != nil && *dpuVolume.Status.State.StorageVendorPluginName != "" &&
		dpuVolume.Status.State.StorageClassName != nil && *dpuVolume.Status.State.StorageClassName != "" &&
		dpuVolume.Status.State.CSIDriverName != nil && *dpuVolume.Status.State.CSIDriverName != ""
}

// applyVendorSelectionResult applies the vendor selection result to the DPUVolume
func (r *DPUVolumeReconciler) applyVendorSelectionResult(dpuVolume *storagev1.DPUVolume, selectedVendorInfo *vendorselector.SelectedVendorInfo) {
	dpuVolume.Status.State.DPUCluster = &storagev1.ObjectReference{
		Namespace: selectedVendorInfo.DPUClusterNamespace,
		Name:      selectedVendorInfo.DPUClusterName,
	}
	dpuVolume.Status.State.SelectedDPUStorageVendorName = &selectedVendorInfo.SelectedDPUStorageVendorName
	dpuVolume.Status.State.StorageVendorPluginName = &selectedVendorInfo.StorageVendorPluginName
	dpuVolume.Status.State.StorageClassName = &selectedVendorInfo.StorageClassName
	dpuVolume.Status.State.CSIDriverName = &selectedVendorInfo.CSIDriverName
	dpuVolume.Status.State.Parameters = selectedVendorInfo.Parameters
}

// applyVolumeProvisioningResult applies the volume provisioning result to the DPUVolume status
func (r *DPUVolumeReconciler) applyVolumeProvisioningResult(dpuVolume *storagev1.DPUVolume, provisionResult *volumeprovisioner.ProvisionResult) {
	if provisionResult.Data == nil {
		return
	}
	dpuVolume.Status.State.PersistentVolumeClaimRef = &storagev1.ObjectReference{
		Namespace: provisionResult.Data.PVCNamespace,
		Name:      provisionResult.Data.PVCName,
	}
	dpuVolume.Status.State.VolumeInfo = &storagev1.VolumeInfo{
		VolumeName:       ptr.To(provisionResult.Data.VolumeName),
		Capacity:         provisionResult.Data.Capacity,
		AccessModes:      provisionResult.Data.AccessModes,
		VolumeMode:       provisionResult.Data.VolumeMode,
		VolumeAttributes: provisionResult.Data.VolumeAttributes,
	}
}

// reconcile handles vendor selection and volume provisioning for DPUVolume resources
//
//nolint:unparam
func (r *DPUVolumeReconciler) reconcile(ctx context.Context, dpuVolume *storagev1.DPUVolume) (ctrl.Result, error) {
	reqLog := ctrllog.FromContext(ctx)
	if r.isDPUVolumeVendorSelected(dpuVolume) {
		reqLog.Info("Volume vendor is already selected")
		conditions.AddTrue(dpuVolume, storagev1.ConditionDPUVolumeScheduled)
	} else {
		vendorSelectionResult, err := r.vendorSelector.SelectVendorForDPUVolume(ctx, dpuVolume)
		if err != nil {
			reqLog.Error(err, "Failed to select vendor for DPUVolume")
			return ctrl.Result{}, err
		}
		if !vendorSelectionResult.Selected {
			reqLog.Info("DPUVolume vendor is not selected", "reason", vendorSelectionResult.Reason)
			r.setPending(dpuVolume, vendorSelectionResult.Reason)
			return ctrl.Result{}, nil
		}
		// Apply the selection result to the DPUVolume
		r.applyVendorSelectionResult(dpuVolume, vendorSelectionResult.SelectedVendorInfo)
		reqLog.Info("DPUVolume vendor selected successfully")
		// We need to commit vendor selection decision (ensure that the cache is updated with this info) before we proceed with the PVC creation.
		// Otherwise, we can create PVC in multiple DPU clusters and for multiple vendors.
		return ctrl.Result{}, nil
	}

	primaryDPUClusterKey := client.ObjectKey{
		Namespace: dpuVolume.Status.State.DPUCluster.Namespace,
		Name:      dpuVolume.Status.State.DPUCluster.Name,
	}
	reqLog = reqLog.WithValues("primaryDPUCluster", primaryDPUClusterKey)
	primaryDPUCluster, err := r.dpuClusterHelper.GetDPUCluster(ctx, primaryDPUClusterKey)
	if err != nil {
		reqLog.Error(err, "Failed to get DPUCluster object for primary DPU cluster")
		return ctrl.Result{}, err
	}
	primaryDPUClusterClient, err := r.dpuClusterHelper.GetClient(ctx, primaryDPUCluster)
	if err != nil {
		reqLog.Error(err, "Failed to get client for primary DPU cluster")
		return ctrl.Result{}, err
	}
	// Get target DPU clusters
	targetDPUClusters, err := r.dpuClusterHelper.GetTargetDPUClusters(ctx, []client.ObjectKey{primaryDPUClusterKey})
	if err != nil {
		return ctrl.Result{}, err
	}
	// Return error if no target clusters are found
	if len(targetDPUClusters) == 0 {
		err := fmt.Errorf("no target DPU clusters found")
		reqLog.Error(err, "No target DPU clusters found, can't create Volume CR")
		// in this case this is an error, because we must create Volume CR at least in the primary DPU cluster
		return ctrl.Result{}, err
	}
	dpuClustersClients, err := r.dpuClusterHelper.GetDPUClusterClients(ctx, targetDPUClusters)
	if err != nil {
		return ctrl.Result{}, err
	}
	volumeProvisioningResult, err := r.volumeProvisioner.Provision(ctx, primaryDPUClusterClient, dpuClustersClients, dpuVolume)
	if err != nil {
		reqLog.Error(err, "Failed to provision volume in DPU clusters")
		return ctrl.Result{}, err
	}
	conditions.AddTrue(dpuVolume, storagev1.ConditionDPUVolumeReconciled)
	if !volumeProvisioningResult.Ready {
		reqLog.Info("Volume is not provisioned yet", "reason", volumeProvisioningResult.Reason)
		return ctrl.Result{}, nil
	}
	// Apply volume provisioning result data to DPUVolume status
	r.applyVolumeProvisioningResult(dpuVolume, &volumeProvisioningResult)
	dpuVolume.Status.Phase = ptr.To(storagev1.DPUVolumePhaseBound)
	conditions.AddTrue(dpuVolume, storagev1.ConditionDPUVolumeBound)
	reqLog.Info("DPUVolume provisioned", "volumeInfo", dpuVolume.Status.State.VolumeInfo)
	return ctrl.Result{}, nil
}

// reconcileDelete handles DPUVolume deletion by checking for attachments, cleaning up resources in target clusters, and removing finalizers
//
//nolint:unparam
func (r *DPUVolumeReconciler) reconcileDelete(ctx context.Context, dpuVolume *storagev1.DPUVolume) (ctrl.Result, error) {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Reconciling delete")

	dpuVolumeAttachments, err := r.getDPUVolumeAttachmentsByVolumeName(ctx, dpuVolume.Name)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get DPUVolumeAttachments for volume %s: %w", dpuVolume.Name, err)
	}
	if len(dpuVolumeAttachments) > 0 {
		reqLog.Info("can't remove attached volume, waiting for detachment")
		r.setAwaitingDeletion(dpuVolume, "volume is attached")
		return ctrl.Result{}, nil
	}

	targetCluster := client.ObjectKey{}
	if dpuVolume.Status.State != nil && dpuVolume.Status.State.DPUCluster != nil {
		targetCluster = client.ObjectKey{
			Namespace: dpuVolume.Status.State.DPUCluster.Namespace,
			Name:      dpuVolume.Status.State.DPUCluster.Name,
		}
	} else {
		reqLog.Info("DPUCluster for DPUVolume is unknown, trying to clean up resources in all ready DPUClusters")
	}
	cleanupResult, err := r.dpuClusterResourcesCleanup(ctx, []client.ObjectKey{targetCluster}, client.ObjectKeyFromObject(dpuVolume))
	if err != nil {
		reqLog.Error(err, "Failed to cleanup DPUVolume")
		return ctrl.Result{}, err
	}

	// remove finalizer if all resources are deleted
	if !cleanupResult.Completed {
		reqLog.Info("Resources related to DPUVolume still exist in DPUClusters, waiting for them to be removed", "reason", cleanupResult.Reason)
		r.setAwaitingDeletion(dpuVolume, cleanupResult.Reason)
	} else {
		reqLog.Info("Resources related to DPUVolume not found in DPUClusters, removing finalizer")
		controllerutil.RemoveFinalizer(dpuVolume, storagev1.DPUVolumeFinalizer)
	}
	return ctrl.Result{}, nil
}

// getDPUVolumeAttachmentsByVolumeName returns DPUVolumeAttachments that reference the specified DPUVolume name.
func (r *DPUVolumeReconciler) getDPUVolumeAttachmentsByVolumeName(ctx context.Context, dpuVolumeName string) ([]storagev1.DPUVolumeAttachment, error) {
	reqLog := ctrllog.FromContext(ctx)
	dpuVolumeAttachmentList := &storagev1.DPUVolumeAttachmentList{}
	if err := r.Client.List(ctx, dpuVolumeAttachmentList,
		client.MatchingFields{indexers.DPUVolumeAttachmentSpecDPUVolumeName: dpuVolumeName}, client.InNamespace(r.Options.Namespace)); err != nil {
		reqLog.Error(err, "Failed to list DPUVolumeAttachments by index", "dpuVolumeName", dpuVolumeName)
		return nil, err
	}
	return dpuVolumeAttachmentList.Items, nil
}

// setPending sets the DPUVolume to pending state
func (r *DPUVolumeReconciler) setPending(dpuVolume *storagev1.DPUVolume, msg string) {
	conditions.AddFalse(dpuVolume, storagev1.ConditionDPUVolumeReconciled,
		conditions.ReasonPending, conditions.ConditionMessage(msg))
}

// setAwaitingDeletion sets the DPUVolume to awaiting deletion state
func (r *DPUVolumeReconciler) setAwaitingDeletion(dpuVolume *storagev1.DPUVolume, msg string) {
	conditions.AddFalse(dpuVolume, storagev1.ConditionDPUVolumeReconciled,
		conditions.ReasonAwaitingDeletion, conditions.ConditionMessage(msg))
}

// dpuClusterResourcesCleanup removes Volume and PVC resources for a DPUVolume from target DPUClusters
func (r *DPUVolumeReconciler) dpuClusterResourcesCleanup(ctx context.Context,
	mandatoryDPUClusters []client.ObjectKey, dpuVolume client.ObjectKey) (DPUClusterResourcesCleanupResult, error) {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("DPUCluster resources cleanup", "dpuVolume", dpuVolume, "mandatoryDPUClusters", mandatoryDPUClusters)
	targetDPUClusters, err := r.dpuClusterHelper.GetTargetDPUClusters(ctx, mandatoryDPUClusters)
	if err != nil {
		reqLog.Error(err, "Failed to get target DPUClusters")
		return DPUClusterResourcesCleanupResult{}, err
	}
	if len(targetDPUClusters) == 0 {
		reqLog.Info("No DPUClusters for cleanup, skipping cleanup")
		return DPUClusterResourcesCleanupResult{Completed: true}, nil
	}
	reqLog.Info("DPUClusters selected for cleanup", "dpuClusters", targetDPUClusters)
	dpuClustersClients, err := r.dpuClusterHelper.GetDPUClusterClients(ctx, targetDPUClusters)
	if err != nil {
		return DPUClusterResourcesCleanupResult{}, err
	}
	reqLog.Info("Call Remove volume")
	removeResult, err := r.volumeProvisioner.Remove(ctx, dpuClustersClients, dpuVolume)
	if err != nil {
		reqLog.Error(err, "Failed to remove Volume in DPU clusters")
		return DPUClusterResourcesCleanupResult{}, err
	}
	reqLog.Info("DPUCluster resources cleanup completed", "removeResult", removeResult)
	return DPUClusterResourcesCleanupResult{Completed: removeResult.Completed, Reason: removeResult.Reason}, nil
}

// enqueueDPUVolumeByDPUCluster map function that enqueues all DPUVolume objects for reconciliation
//
//nolint:dupl
func (r *DPUVolumeReconciler) enqueueDPUVolumeByDPUCluster(ctx context.Context, o client.Object) []reconcile.Request {
	reqLog := ctrllog.FromContext(ctx).WithValues("controller", "dpuvolume")
	result := func() []reconcile.Request {
		dpuVolumeList := &storagev1.DPUVolumeList{}
		if err := r.Client.List(ctx, dpuVolumeList, client.InNamespace(r.Options.Namespace)); err != nil {
			return nil
		}
		result := make([]reconcile.Request, 0, len(dpuVolumeList.Items))
		for _, dpuVolume := range dpuVolumeList.Items {
			result = append(result, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&dpuVolume)})
		}
		return result
	}()
	if len(result) > 0 {
		reqLog.Info("Enqueued DPUVolume objects by DPUCluster", "dpuCluster", client.ObjectKeyFromObject(o), "result", result)
	}
	return result
}

// enqueueDPUVolumeByDPUVolumeAttachment enqueues DPUVolume objects referenced by DPUVolumeAttachment
//
//nolint:dupl
func (r *DPUVolumeReconciler) enqueueDPUVolumeByDPUVolumeAttachment(ctx context.Context, o client.Object) []reconcile.Request {
	reqLog := ctrllog.FromContext(ctx).WithValues("controller", "dpuvolume")
	result := func() []reconcile.Request {
		dpuVolumeAttachment, ok := o.(*storagev1.DPUVolumeAttachment)
		if !ok {
			return nil
		}
		return []reconcile.Request{{
			NamespacedName: client.ObjectKey{
				Namespace: r.Options.Namespace,
				Name:      dpuVolumeAttachment.Spec.DPUVolumeName,
			},
		}}
	}()
	if len(result) > 0 {
		reqLog.Info("Enqueuing DPUVolume objects by DPUVolumeAttachment", "dpuVolumeAttachment", client.ObjectKeyFromObject(o), "result", result)
	}
	return result
}

// enqueueDPUVolumeByDPUStoragePolicy enqueues DPUVolume objects referenced by DPUStoragePolicy
//
//nolint:dupl
func (r *DPUVolumeReconciler) enqueueDPUVolumeByDPUStoragePolicy(ctx context.Context, o client.Object) []reconcile.Request {
	reqLog := ctrllog.FromContext(ctx).WithValues("controller", "dpuvolume")
	result := func() []reconcile.Request {
		dpuStoragePolicy, ok := o.(*storagev1.DPUStoragePolicy)
		if !ok {
			return nil
		}
		dpuVolumeList := &storagev1.DPUVolumeList{}
		if err := r.Client.List(ctx, dpuVolumeList, client.InNamespace(r.Options.Namespace),
			client.MatchingFields{indexers.DPUVolumeSpecDPUStoragePolicyName: dpuStoragePolicy.Name}); err != nil {
			return nil
		}
		result := make([]reconcile.Request, 0, len(dpuVolumeList.Items))
		for _, dpuVolume := range dpuVolumeList.Items {
			if r.isDPUVolumeVendorSelected(&dpuVolume) {
				continue
			}
			result = append(result, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(&dpuVolume),
			})
		}
		return result
	}()
	if len(result) > 0 {
		reqLog.Info("Enqueuing DPUVolume objects by DPUStoragePolicy", "dpuStoragePolicy", client.ObjectKeyFromObject(o), "result", result)
	}
	return result
}

// enqueueDPUVolumeByPVCInDPUCluster enqueues DPUVolume objects referenced by PVC in DPU cluster
//
//nolint:dupl
func (r *DPUVolumeReconciler) enqueueDPUVolumeByPVCInDPUCluster(ctx context.Context, o client.Object) []reconcile.Request {
	reqLog := ctrllog.FromContext(ctx).WithValues("controller", "dpuvolume")
	result := ReconcileRequestByOwnedBy(r.ownedByHelper, o, r.Options.Namespace)
	if len(result) > 0 {
		reqLog.Info("Enqueuing DPUVolume objects by PVC in DPU cluster", "pvc", client.ObjectKeyFromObject(o), "result", result)
	}
	return result
}

// enqueueDPUVolumeByVolumeInDPUCluster enqueues DPUVolume objects referenced by Volume in DPU cluster
//
//nolint:dupl
func (r *DPUVolumeReconciler) enqueueDPUVolumeByVolumeInDPUCluster(ctx context.Context, o client.Object) []reconcile.Request {
	reqLog := ctrllog.FromContext(ctx).WithValues("controller", "dpuvolume")
	result := ReconcileRequestByOwnedBy(r.ownedByHelper, o, r.Options.Namespace)
	if len(result) > 0 {
		reqLog.Info("Enqueuing DPUVolume objects by Volume in DPU cluster", "volume", client.ObjectKeyFromObject(o), "result", result)
	}
	return result
}

// WatchDPUClusterPVC registers a watch for PVCs in the DPU cluster
func (r *DPUVolumeReconciler) WatchDPUClusterPVC(_ context.Context, _ client.Client, _ client.ObjectKey) (dpucluster.Watcher, error) {
	return dpucluster.NewWatcher(dpucluster.WatcherOptions{
		Name:         "dpuvolume-watch-pvc",
		Watcher:      r.controller,
		Kind:         &corev1.PersistentVolumeClaim{},
		EventHandler: handler.EnqueueRequestsFromMapFunc(r.enqueueDPUVolumeByPVCInDPUCluster),
	}), nil
}

// WatchDPUClusterVolume registers a watch for Volumes in the DPU cluster
func (r *DPUVolumeReconciler) WatchDPUClusterVolume(_ context.Context, _ client.Client, _ client.ObjectKey) (dpucluster.Watcher, error) {
	return dpucluster.NewWatcher(dpucluster.WatcherOptions{
		Name:         "dpuvolume-watch-volume",
		Watcher:      r.controller,
		Kind:         &storagev1.Volume{},
		EventHandler: handler.EnqueueRequestsFromMapFunc(r.enqueueDPUVolumeByVolumeInDPUCluster),
	}), nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DPUVolumeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.dpuClusterHelper = dpuclusterhelper.New(mgr.GetClient(), r.RemoteCache)
	r.ownedByHelper = utils.New(dpuVolumeOwnedByAnnotation)
	r.vendorSelector = vendorselector.New(mgr.GetClient(), r.dpuClusterHelper, vendorselector.Options{
		Namespace: r.Options.Namespace,
	})
	r.volumeProvisioner = volumeprovisioner.New(r.Options.TargetNamespace, r.ownedByHelper)
	c, err := ctrl.NewControllerManagedBy(mgr).
		For(&storagev1.DPUVolume{}).
		Watches(&provisioningv1.DPUCluster{}, handler.EnqueueRequestsFromMapFunc(r.enqueueDPUVolumeByDPUCluster)).
		Watches(&storagev1.DPUStoragePolicy{}, handler.EnqueueRequestsFromMapFunc(r.enqueueDPUVolumeByDPUStoragePolicy)).
		// watch for deletion of DPUVolumeAttachments to unblock removal of the volume when it is detached
		Watches(&storagev1.DPUVolumeAttachment{}, handler.EnqueueRequestsFromMapFunc(r.enqueueDPUVolumeByDPUVolumeAttachment),
			builder.WithPredicates(utilsPredicates.PredicateFuncsByEventTypes(event.DeleteEvent{}))).
		Build(r)
	if err != nil {
		return err
	}
	r.controller = c
	return nil
}
