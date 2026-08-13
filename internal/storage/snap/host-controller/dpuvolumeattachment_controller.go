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

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/controllerattacher"
	dpuattacher "github.com/nvidia/doca-platform/internal/storage/snap/host-controller/dpuattacher"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/dpuclusterhelper"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/indexers"
	"github.com/nvidia/doca-platform/internal/storage/snap/host-controller/utils"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	"github.com/nvidia/doca-platform/pkg/dpuselector"
	utilsPredicates "github.com/nvidia/doca-platform/pkg/utils/predicates"

	"github.com/fluxcd/pkg/runtime/patch"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
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

// DPUVolumeAttachmentReconciler reconciles a DPUVolumeAttachment object
type DPUVolumeAttachmentReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	controller  controller.Controller
	RemoteCache *dpucluster.RemoteCache
	Options     Options

	dpuClusterHelper   dpuclusterhelper.DPUClusterHelper
	ownedByHelper      utils.OwnedByHelper
	dpuSelector        dpuselector.DPUSelector
	controllerAttacher controllerattacher.ControllerAttacher
	dpuAttacher        dpuattacher.DPUAttacher
}

const (
	dpuVolumeAttachmentControllerName = "dpuvolumeattachmentcontroller"
	// annotation to save reference to the DPUVolumeAttachment in objects in DPUClusters that are created by this controller
	dpuVolumeAttachmentOwnedByAnnotation = "storage.dpu.nvidia.com/owned-by-dpuvolumeattachment"

	// preferredStorageDPUAnnotation is the name of the annotation used by
	// snap-host-controller to select the appropriate DPU when multiple DPUs
	// are available for a DPUNode. This annotation is expected to be injected
	// into the DPU object by the DPUDeployment controller.
	// Note: storage.dpu.nvidia.com "group" cannot be used, because annotations
	// containing dpu.nvidia.com are not allowed by the DPUDeployment API.
	// This annotation will be removed in the future when proper support for
	// multiple DPUs is implemented for the storage subsystem.
	preferredStorageDPUAnnotation = "storage.nvidia.com/preferred-dpu"
)

// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=dpuvolumeattachments,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=dpuvolumeattachments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=dpuvolumeattachments/finalizers,verbs=update
// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=dpuvolumes,verbs=get;list;watch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpuclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpus,verbs=get;list;watch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpunodes,verbs=get;list;watch

// Reconcile manages DPUVolumeAttachment lifecycle including attachment, detachment, and cleanup.
func (r *DPUVolumeAttachmentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Reconciling")

	dpuVolumeAttachment := &storagev1.DPUVolumeAttachment{}
	if err := r.Client.Get(ctx, req.NamespacedName, dpuVolumeAttachment); err != nil {
		if apierrors.IsNotFound(err) {
			reqLog.Info("DPUVolumeAttachment not found, ensure that all resources are removed from DPUClusters")
			_, err := r.dpuClusterResourcesCleanup(ctx, nil, req.NamespacedName)
			return FinalizeReconcileResult(ctrl.Result{}, err)
		}
		return ctrl.Result{}, err
	}

	patcher := patch.NewSerialPatcher(dpuVolumeAttachment, r.Client)

	conditions.EnsureConditions(dpuVolumeAttachment, storagev1.DPUVolumeAttachmentConditions)

	// Defer a patch call to always patch the object when Reconcile exits.
	defer func() {
		r.finalizeConditions(dpuVolumeAttachment, reterr)
		reqLog.Info("Patching")
		err := patcher.Patch(ctx, dpuVolumeAttachment,
			patch.WithFieldOwner(dpuVolumeAttachmentControllerName),
			patch.WithStatusObservedGeneration{},
			patch.WithOwnedConditions{Conditions: conditions.TypesAsStrings(storagev1.DPUVolumeAttachmentConditions)},
		)
		if kerrors.FilterOut(err, apierrors.IsNotFound) != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()
	// Handle deletion reconciliation loop.
	if !dpuVolumeAttachment.ObjectMeta.DeletionTimestamp.IsZero() {
		return FinalizeReconcileResult(r.reconcileDelete(ctx, dpuVolumeAttachment))
	}
	// Add finalizer if not set.
	if !controllerutil.ContainsFinalizer(dpuVolumeAttachment, storagev1.DPUVolumeAttachmentFinalizer) {
		reqLog.Info("Adding finalizer")
		controllerutil.AddFinalizer(dpuVolumeAttachment, storagev1.DPUVolumeAttachmentFinalizer)
		return ctrl.Result{}, nil
	}
	return FinalizeReconcileResult(r.reconcile(ctx, dpuVolumeAttachment))
}

// finalizeConditions finalizes the conditions of the DPUVolumeAttachment object
func (r *DPUVolumeAttachmentReconciler) finalizeConditions(dpuVolumeAttachment *storagev1.DPUVolumeAttachment, err error) {
	// in case of any error set ConditionDPUVolumeAttachmentReconciled to false with error reason
	if err != nil {
		conditions.AddFalse(dpuVolumeAttachment, storagev1.ConditionDPUVolumeAttachmentReconciled,
			conditions.ReasonError, conditions.ConditionMessage(err.Error()))
	}
	conditions.SetSummary(dpuVolumeAttachment)
}

// reconcile handles volume attachment process including validation and attachment to DPU nodes
//
//nolint:unparam
func (r *DPUVolumeAttachmentReconciler) reconcile(ctx context.Context,
	dpuVolumeAttachment *storagev1.DPUVolumeAttachment) (ctrl.Result, error) {
	reqLog := ctrllog.FromContext(ctx)

	dpuVolume := &storagev1.DPUVolume{}
	dpuVolumeKey := client.ObjectKey{Namespace: r.Options.Namespace, Name: dpuVolumeAttachment.Spec.DPUVolumeName}
	if err := r.Client.Get(ctx, dpuVolumeKey, dpuVolume); err != nil {
		if apierrors.IsNotFound(err) {
			reqLog.Info("DPUVolume not found", "dpuVolume", dpuVolumeKey)
			r.setPending(dpuVolumeAttachment, fmt.Sprintf("DPUVolume %s not found", dpuVolumeAttachment.Spec.DPUVolumeName))
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get DPUVolume %s: %w", dpuVolumeKey, err)
	}
	if !conditions.IsTrue(dpuVolume, storagev1.ConditionDPUVolumeBound) {
		reqLog.Info("DPUVolume is not ready", "dpuVolume", dpuVolumeKey)
		r.setPending(dpuVolumeAttachment, fmt.Sprintf("DPUVolume %s is not ready", dpuVolumeKey))
		return ctrl.Result{}, nil
	}
	dpuNode := &provisioningv1.DPUNode{}
	dpuNodeKey := client.ObjectKey{Namespace: r.Options.Namespace, Name: dpuVolumeAttachment.Spec.DPUNodeName}
	if err := r.Client.Get(ctx, dpuNodeKey, dpuNode); err != nil {
		if apierrors.IsNotFound(err) {
			reqLog.Info("DPUNode not found", "dpuNode", dpuNodeKey)
			r.setPending(dpuVolumeAttachment, fmt.Sprintf("DPUNode %s not found", dpuNodeKey))
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get DPUNode: %w", err)
	}
	selectedDPU, err := r.dpuSelector.GetDPUForNode(ctx, r.Client, dpuNode)
	if err != nil {
		reqLog.Error(err, "Failed to select DPU")
		return ctrl.Result{}, err
	}
	if !conditions.IsTrue(selectedDPU, conditions.ConditionType(provisioningv1.ConditionReady)) {
		reqLog.Info("DPU is not ready", "dpu", selectedDPU.Name)
		r.setPending(dpuVolumeAttachment, fmt.Sprintf("DPU %s is not ready", selectedDPU.Name))
		return ctrl.Result{}, nil
	}
	primaryClusterClient, dpuNodeClusterClient, err := r.getDPUClusterClients(ctx, dpuVolume, selectedDPU)
	if err != nil {
		reqLog.Error(err, "Failed to get DPUCluster clients")
		return ctrl.Result{}, err
	}

	conditions.AddTrue(dpuVolumeAttachment, storagev1.ConditionDPUVolumeAttachmentReconciled)

	controllerAttachResult, err := r.controllerAttacher.ControllerAttach(ctx, primaryClusterClient, dpuVolumeAttachment, dpuVolume, selectedDPU)
	if err != nil {
		reqLog.Error(err, "Failed to attach Volume to DPU", "volume", dpuVolume.Name, "dpuCluster", primaryClusterClient.DPUCluster.Name)
		return ctrl.Result{}, fmt.Errorf("failed to attach Volume to DPU: %w", err)
	}
	r.updateDPUVolumeAttachmentStatusFromControllerAttach(dpuVolumeAttachment, controllerAttachResult)
	if !controllerAttachResult.Ready {
		reqLog.Info("ControllerAttach not ready", "reason", controllerAttachResult.Reason)
		return ctrl.Result{}, nil
	}
	reqLog.Info("ControllerAttach completed", "dpuCluster", primaryClusterClient.DPUCluster.Name)
	dpuAttacherResult, err := r.dpuAttacher.DPUAttach(ctx, dpuNodeClusterClient, dpuVolumeAttachment, selectedDPU)
	if err != nil {
		reqLog.Error(err, "Failed to create VolumeAttachment in DPU cluster", "volume", dpuVolume.Name, "dpuCluster", dpuNodeClusterClient.DPUCluster.Name)
		return ctrl.Result{}, fmt.Errorf("failed to create VolumeAttachment in DPU cluster: %w", err)
	}
	r.updateDPUVolumeAttachmentStatusFromDPUAttach(dpuVolumeAttachment, dpuAttacherResult)
	if !dpuAttacherResult.Ready {
		reqLog.Info("DPUAttach not ready", "reason", dpuAttacherResult.Reason)
		return ctrl.Result{}, nil
	}
	reqLog.Info("DPUAttach completed", "dpuCluster", dpuNodeClusterClient.DPUCluster.Name)
	return ctrl.Result{}, nil
}

// updateDPUVolumeAttachmentStatusFromControllerAttach updates the status of the DPUVolumeAttachment object from the ControllerAttach result
func (r *DPUVolumeAttachmentReconciler) updateDPUVolumeAttachmentStatusFromControllerAttach(
	dpuVolumeAttachment *storagev1.DPUVolumeAttachment, controllerAttachResult controllerattacher.ControllerAttachResult) {
	if controllerAttachResult.Ready {
		dpuVolumeAttachment.Status.Message = nil
		dpuVolumeAttachment.Status.ControllerAttached = ptr.To(true)
		dpuVolumeAttachment.Status.AttachmentMetadata = controllerAttachResult.AttachmentMetadata
		conditions.AddTrue(dpuVolumeAttachment, storagev1.ConditionDPUVolumeAttachmentControllerAttached)
	} else {
		msg := fmt.Sprintf("ControllerAttach not ready: %s", controllerAttachResult.Reason)
		dpuVolumeAttachment.Status.Message = &msg
		conditions.AddFalse(dpuVolumeAttachment, storagev1.ConditionDPUVolumeAttachmentControllerAttached,
			conditions.ReasonPending, conditions.ConditionMessage(msg))
	}
}

// updateDPUVolumeAttachmentStatusFromDPUAttach updates the status of the DPUVolumeAttachment object from the DPUAttach result
func (r *DPUVolumeAttachmentReconciler) updateDPUVolumeAttachmentStatusFromDPUAttach(
	dpuVolumeAttachment *storagev1.DPUVolumeAttachment, dpuAttacherResult dpuattacher.DPUAttachResult) {
	if dpuAttacherResult.Ready {
		dpuVolumeAttachment.Status.Message = nil
		dpuVolumeAttachment.Status.DPUAttached = ptr.To(true)
		if dpuAttacherResult.Data != nil {
			dpuStatus := &storagev1.AttachmentStatusDPU{}
			if dpuAttacherResult.Data.PCIAddress != "" {
				dpuStatus.PCIAddress = ptr.To(dpuAttacherResult.Data.PCIAddress)
			}
			if dpuAttacherResult.Data.FuncVUID != "" {
				dpuStatus.FuncVUID = ptr.To(dpuAttacherResult.Data.FuncVUID)
			}
			if dpuAttacherResult.Data.DeviceName != "" {
				dpuStatus.DeviceName = ptr.To(dpuAttacherResult.Data.DeviceName)
			}
			dpuStatus.NVMEAttrs = dpuAttacherResult.Data.NVMEAttrs
			dpuStatus.VirtioFSAttrs = dpuAttacherResult.Data.VirtioFSAttrs
			dpuVolumeAttachment.Status.DPU = dpuStatus
		}
		conditions.AddTrue(dpuVolumeAttachment, storagev1.ConditionDPUVolumeAttachmentDPUAttached)
	} else {
		msg := fmt.Sprintf("DPUAttach not ready: %s", dpuAttacherResult.Reason)
		dpuVolumeAttachment.Status.Message = &msg
		conditions.AddFalse(dpuVolumeAttachment, storagev1.ConditionDPUVolumeAttachmentDPUAttached,
			conditions.ReasonPending, conditions.ConditionMessage(msg))
	}
}

// dpuClusterResourcesCleanup removes VolumeAttachment and related resources from DPU clusters
func (r *DPUVolumeAttachmentReconciler) dpuClusterResourcesCleanup(ctx context.Context,
	mandatoryDPUClusters []client.ObjectKey, dpuVolumeAttachment client.ObjectKey) (DPUClusterResourcesCleanupResult, error) {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("DPUCluster resources cleanup", "dpuVolumeAttachment", dpuVolumeAttachment, "mandatoryDPUClusters", mandatoryDPUClusters)

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
	reqLog.Info("Call DPUDetach")
	dpuDetachResult, err := r.dpuAttacher.DPUDetach(ctx, dpuClustersClients, dpuVolumeAttachment)
	if err != nil {
		reqLog.Error(err, "DPUDetach failed")
		return DPUClusterResourcesCleanupResult{}, err
	}
	if !dpuDetachResult.Completed {
		reqLog.Info("DPUDetach not completed", "reason", dpuDetachResult.Reason)
		return DPUClusterResourcesCleanupResult{Completed: false, Reason: dpuDetachResult.Reason}, nil
	}
	reqLog.Info("Call ControllerDetach")
	controllerDetachResult, err := r.controllerAttacher.ControllerDetach(ctx, dpuClustersClients, dpuVolumeAttachment)
	if err != nil {
		reqLog.Error(err, "ControllerDetach failed")
		return DPUClusterResourcesCleanupResult{}, err
	}
	if !controllerDetachResult.Completed {
		reqLog.Info("ControllerDetach not completed", "reason", controllerDetachResult.Reason)
		return DPUClusterResourcesCleanupResult{Completed: false, Reason: controllerDetachResult.Reason}, nil
	}
	reqLog.Info("DPUCluster resources cleanup completed")
	return DPUClusterResourcesCleanupResult{Completed: true}, nil
}

// reconcileDelete handles DPUVolumeAttachment deletion by cleaning up resources and removing finalizers
//
//nolint:unparam
func (r *DPUVolumeAttachmentReconciler) reconcileDelete(ctx context.Context,
	dpuVolumeAttachment *storagev1.DPUVolumeAttachment) (ctrl.Result, error) {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Reconciling delete")

	mandatoryDPUClusters := []client.ObjectKey{}
	dpuVolume := &storagev1.DPUVolume{}
	dpuVolumeKey := client.ObjectKey{Namespace: r.Options.Namespace, Name: dpuVolumeAttachment.Spec.DPUVolumeName}
	err := r.Client.Get(ctx, dpuVolumeKey, dpuVolume)
	if err != nil && !apierrors.IsNotFound(err) {
		reqLog.Error(err, "Failed to get DPUVolume", "dpuVolume", dpuVolumeKey)
		return ctrl.Result{}, err
	}
	if !apierrors.IsNotFound(err) && dpuVolume.Status.State != nil && dpuVolume.Status.State.DPUCluster != nil &&
		dpuVolume.Status.State.DPUCluster.Name != "" && dpuVolume.Status.State.DPUCluster.Namespace != "" {
		// dpuVolume exists
		mandatoryDPUClusters = append(mandatoryDPUClusters, client.ObjectKey{
			Namespace: dpuVolume.Status.State.DPUCluster.Namespace,
			Name:      dpuVolume.Status.State.DPUCluster.Name,
		})
	}
	dpuNode := &provisioningv1.DPUNode{}
	dpuNodeKey := client.ObjectKey{Namespace: r.Options.Namespace, Name: dpuVolumeAttachment.Spec.DPUNodeName}
	err = r.Client.Get(ctx, dpuNodeKey, dpuNode)
	if err != nil && !apierrors.IsNotFound(err) {
		reqLog.Error(err, "Failed to get DPUNode", "dpuNode", dpuNodeKey)
		return ctrl.Result{}, err
	}
	if !apierrors.IsNotFound(err) {
		// dpuNode exists
		selectedDPU, err := r.dpuSelector.GetDPUForNode(ctx, r.Client, dpuNode)
		if err != nil {
			reqLog.Error(err, "Failed to select DPU")
			return ctrl.Result{}, err
		}
		if selectedDPU.Spec.Cluster.Name != "" && selectedDPU.Spec.Cluster.Namespace != "" {
			mandatoryDPUClusters = append(mandatoryDPUClusters, client.ObjectKey{
				Namespace: selectedDPU.Spec.Cluster.Namespace,
				Name:      selectedDPU.Spec.Cluster.Name,
			})
		}
	}
	result, err := r.dpuClusterResourcesCleanup(ctx, mandatoryDPUClusters, client.ObjectKeyFromObject(dpuVolumeAttachment))
	if err != nil {
		reqLog.Error(err, "Failed to cleanup DPUVolumeAttachment")
		return ctrl.Result{}, err
	}
	if !result.Completed {
		reqLog.Info("Resources related to DPUVolumeAttachment still exist in DPUClusters, waiting for them to be removed")
		r.setAwaitingDeletion(dpuVolumeAttachment, result.Reason)
		return ctrl.Result{}, nil
	} else {
		reqLog.Info("Resources related to DPUVolumeAttachment not found in DPUClusters, removing finalizer")
		controllerutil.RemoveFinalizer(dpuVolumeAttachment, storagev1.DPUVolumeAttachmentFinalizer)
	}
	return ctrl.Result{}, nil
}

// getDPUClusterClients returns clients for primary and DPU node clusters
func (r *DPUVolumeAttachmentReconciler) getDPUClusterClients(
	ctx context.Context, dpuVolume *storagev1.DPUVolume, dpu *provisioningv1.DPU) (dpuclusterhelper.ClientForDPUCluster, dpuclusterhelper.ClientForDPUCluster, error) {

	if dpuVolume.Status.State == nil || dpuVolume.Status.State.DPUCluster == nil ||
		dpuVolume.Status.State.DPUCluster.Name == "" || dpuVolume.Status.State.DPUCluster.Namespace == "" {
		return dpuclusterhelper.ClientForDPUCluster{}, dpuclusterhelper.ClientForDPUCluster{}, fmt.Errorf("DPUVolume %s has no DPUCluster", dpuVolume.Name)
	}
	if dpu.Spec.Cluster.Name == "" || dpu.Spec.Cluster.Namespace == "" {
		return dpuclusterhelper.ClientForDPUCluster{}, dpuclusterhelper.ClientForDPUCluster{}, fmt.Errorf("DPU %s has no DPUCluster", dpu.Name)
	}
	primaryClusterKey := client.ObjectKey{
		Namespace: dpuVolume.Status.State.DPUCluster.Namespace,
		Name:      dpuVolume.Status.State.DPUCluster.Name,
	}
	dpuNodeClusterKey := client.ObjectKey{
		Name:      dpu.Spec.Cluster.Name,
		Namespace: dpu.Spec.Cluster.Namespace,
	}
	primaryCluster, err := r.dpuClusterHelper.GetDPUCluster(ctx, primaryClusterKey)
	if err != nil {
		return dpuclusterhelper.ClientForDPUCluster{}, dpuclusterhelper.ClientForDPUCluster{},
			fmt.Errorf("failed to get DPUCluster %s: %w", primaryClusterKey, err)
	}
	dpuNodeCluster, err := r.dpuClusterHelper.GetDPUCluster(ctx, dpuNodeClusterKey)
	if err != nil {
		return dpuclusterhelper.ClientForDPUCluster{}, dpuclusterhelper.ClientForDPUCluster{},
			fmt.Errorf("failed to get DPUCluster %s: %w", dpuNodeClusterKey, err)
	}
	primaryClusterClient, err := r.dpuClusterHelper.GetClient(ctx, primaryCluster)
	if err != nil {
		return dpuclusterhelper.ClientForDPUCluster{}, dpuclusterhelper.ClientForDPUCluster{},
			fmt.Errorf("failed to get client for DPUCluster %s: %w", primaryClusterKey, err)
	}
	dpuNodeClusterClient, err := r.dpuClusterHelper.GetClient(ctx, dpuNodeCluster)
	if err != nil {
		return dpuclusterhelper.ClientForDPUCluster{}, dpuclusterhelper.ClientForDPUCluster{},
			fmt.Errorf("failed to get client for DPUCluster %s: %w", dpuNodeClusterKey, err)
	}
	return primaryClusterClient, dpuNodeClusterClient, nil
}

// setPending sets the DPUVolumeAttachment to pending state
func (r *DPUVolumeAttachmentReconciler) setPending(dpuVolumeAttachment *storagev1.DPUVolumeAttachment, msg string) {
	conditions.AddFalse(dpuVolumeAttachment, storagev1.ConditionDPUVolumeAttachmentReconciled,
		conditions.ReasonPending, conditions.ConditionMessage(msg))
}

// setAwaitingDeletion sets the DPUVolumeAttachment to awaiting deletion state
func (r *DPUVolumeAttachmentReconciler) setAwaitingDeletion(dpuVolumeAttachment *storagev1.DPUVolumeAttachment, msg string) {
	conditions.AddFalse(dpuVolumeAttachment, storagev1.ConditionDPUVolumeAttachmentReconciled,
		conditions.ReasonAwaitingDeletion, conditions.ConditionMessage(msg))
}

// enqueueDPUVolumeAttachmentByVolumeAttachment enqueues DPUVolumeAttachment objects when VolumeAttachment changes in DPU cluster
//
//nolint:dupl
func (r *DPUVolumeAttachmentReconciler) enqueueDPUVolumeAttachmentByVolumeAttachment(ctx context.Context,
	o client.Object) []reconcile.Request {
	reqLog := ctrllog.FromContext(ctx).WithValues("controller", "dpuvolumeattachment")
	result := ReconcileRequestByOwnedBy(r.ownedByHelper, o, r.Options.Namespace)
	if len(result) > 0 {
		reqLog.Info("Enqueued DPUVolumeAttachment objects by VolumeAttachment",
			"volumeAttachment", client.ObjectKeyFromObject(o), "result", result)
	}
	return result
}

// enqueueDPUVolumeAttachmentBySVVolumeAttachment enqueues DPUVolumeAttachment objects when SVVolumeAttachment changes in DPU cluster
//
//nolint:dupl
func (r *DPUVolumeAttachmentReconciler) enqueueDPUVolumeAttachmentBySVVolumeAttachment(ctx context.Context,
	o client.Object) []reconcile.Request {
	reqLog := ctrllog.FromContext(ctx).WithValues("controller", "dpuvolumeattachment")
	result := ReconcileRequestByOwnedBy(r.ownedByHelper, o, r.Options.Namespace)
	if len(result) > 0 {
		reqLog.Info("Enqueued DPUVolumeAttachment objects by SVVolumeAttachment",
			"svVolumeAttachment", client.ObjectKeyFromObject(o), "result", result)
	}
	return result
}

// enqueueDPUVolumeAttachmentByDPUCluster enqueues all DPUVolumeAttachment objects when DPUCluster changes
//
//nolint:dupl
func (r *DPUVolumeAttachmentReconciler) enqueueDPUVolumeAttachmentByDPUCluster(ctx context.Context, o client.Object) []reconcile.Request {
	reqLog := ctrllog.FromContext(ctx).WithValues("controller", "dpuvolumeattachment")
	dpuVolumeAttachmentList := &storagev1.DPUVolumeAttachmentList{}
	if err := r.Client.List(ctx, dpuVolumeAttachmentList, client.InNamespace(r.Options.Namespace)); err != nil {
		reqLog.Error(err, "Failed to list DPUVolumeAttachments")
		return nil
	}
	result := make([]reconcile.Request, 0, len(dpuVolumeAttachmentList.Items))
	for _, dpuVolumeAttachment := range dpuVolumeAttachmentList.Items {
		result = append(result, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&dpuVolumeAttachment),
		})
	}
	if len(result) > 0 {
		reqLog.Info("Enqueued DPUVolumeAttachment objects by DPUCluster", "dpuCluster", client.ObjectKeyFromObject(o), "result", result)
	}
	return result
}

// enqueueDPUVolumeAttachmentByDPU enqueues related DPUVolumeAttachment objects when DPU changes
//
//nolint:dupl
func (r *DPUVolumeAttachmentReconciler) enqueueDPUVolumeAttachmentByDPU(ctx context.Context, o client.Object) []reconcile.Request {
	reqLog := ctrllog.FromContext(ctx).WithValues("controller", "dpuvolumeattachment")
	dpu, ok := o.(*provisioningv1.DPU)
	if !ok {
		return nil
	}
	// Find DPUVolumeAttachments that reference the DPUNode associated with this DPU
	dpuVolumeAttachmentList := &storagev1.DPUVolumeAttachmentList{}
	if err := r.Client.List(ctx, dpuVolumeAttachmentList,
		client.InNamespace(r.Options.Namespace),
		client.MatchingFields{indexers.DPUVolumeAttachmentSpecDPUNodeName: dpu.Spec.DPUNodeName}); err != nil {
		reqLog.Error(err, "Failed to list DPUVolumeAttachments")
		return nil
	}
	result := make([]reconcile.Request, 0, len(dpuVolumeAttachmentList.Items))
	for _, dpuVolumeAttachment := range dpuVolumeAttachmentList.Items {
		result = append(result, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&dpuVolumeAttachment),
		})
	}
	if len(result) > 0 {
		reqLog.Info("Enqueued DPUVolumeAttachment objects by DPU", "dpu", client.ObjectKeyFromObject(dpu), "result", result)
	}
	return result
}

// enqueueDPUVolumeAttachmentByDPUNode enqueues related DPUVolumeAttachment objects when DPUNode changes
//
//nolint:dupl
func (r *DPUVolumeAttachmentReconciler) enqueueDPUVolumeAttachmentByDPUNode(ctx context.Context, o client.Object) []reconcile.Request {
	reqLog := ctrllog.FromContext(ctx).WithValues("controller", "dpuvolumeattachment")
	dpuNode, ok := o.(*provisioningv1.DPUNode)
	if !ok {
		return nil
	}
	// Find DPUVolumeAttachments that reference this DPUNode
	dpuVolumeAttachmentList := &storagev1.DPUVolumeAttachmentList{}
	if err := r.Client.List(ctx, dpuVolumeAttachmentList,
		client.InNamespace(r.Options.Namespace),
		client.MatchingFields{indexers.DPUVolumeAttachmentSpecDPUNodeName: dpuNode.Name}); err != nil {
		reqLog.Error(err, "Failed to list DPUVolumeAttachments")
		return nil
	}
	result := make([]reconcile.Request, 0, len(dpuVolumeAttachmentList.Items))
	for _, dpuVolumeAttachment := range dpuVolumeAttachmentList.Items {
		result = append(result, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&dpuVolumeAttachment),
		})
	}
	if len(result) > 0 {
		reqLog.Info("Enqueued DPUVolumeAttachment objects by DPUNode", "dpuNode", client.ObjectKeyFromObject(dpuNode), "result", result)
	}
	return result
}

// enqueueDPUVolumeAttachmentByDPUVolume enqueues related DPUVolumeAttachment objects when DPUVolume changes

//nolint:dupl
func (r *DPUVolumeAttachmentReconciler) enqueueDPUVolumeAttachmentByDPUVolume(ctx context.Context, o client.Object) []reconcile.Request {
	reqLog := ctrllog.FromContext(ctx).WithValues("controller", "dpuvolumeattachment")
	dpuVolume, ok := o.(*storagev1.DPUVolume)
	if !ok {
		return nil
	}

	// Find DPUVolumeAttachments that reference this DPUVolume
	dpuVolumeAttachmentList := &storagev1.DPUVolumeAttachmentList{}
	if err := r.Client.List(ctx, dpuVolumeAttachmentList,
		client.InNamespace(r.Options.Namespace),
		client.MatchingFields{indexers.DPUVolumeAttachmentSpecDPUVolumeName: dpuVolume.Name}); err != nil {
		reqLog.Error(err, "Failed to list DPUVolumeAttachments")
		return nil
	}

	result := make([]reconcile.Request, 0, len(dpuVolumeAttachmentList.Items))
	for _, dpuVolumeAttachment := range dpuVolumeAttachmentList.Items {
		result = append(result, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&dpuVolumeAttachment),
		})
	}

	if len(result) > 0 {
		reqLog.Info("Enqueued DPUVolumeAttachment objects by DPUVolume", "dpuVolume", client.ObjectKeyFromObject(dpuVolume), "result", result)
	}
	return result
}

// WatchDPUClusterVolumeAttachment registers a watch for VolumeAttachments in the DPU cluster
func (r *DPUVolumeAttachmentReconciler) WatchDPUClusterVolumeAttachment(_ context.Context, _ client.Client, _ client.ObjectKey) (dpucluster.Watcher, error) {
	return dpucluster.NewWatcher(dpucluster.WatcherOptions{
		Name:         "dpuvolumeattachment-watch-volumeattachment",
		Watcher:      r.controller,
		Kind:         &storagev1.VolumeAttachment{},
		EventHandler: handler.EnqueueRequestsFromMapFunc(r.enqueueDPUVolumeAttachmentByVolumeAttachment),
	}), nil
}

// WatchDPUClusterSVVolumeAttachment registers a watch for SVVolumeAttachments in the DPU cluster
func (r *DPUVolumeAttachmentReconciler) WatchDPUClusterSVVolumeAttachment(_ context.Context, _ client.Client, _ client.ObjectKey) (dpucluster.Watcher, error) {
	return dpucluster.NewWatcher(dpucluster.WatcherOptions{
		Name:         "dpuvolumeattachment-watch-svvolumeattachment",
		Watcher:      r.controller,
		Kind:         &storagev1.SVVolumeAttachment{},
		EventHandler: handler.EnqueueRequestsFromMapFunc(r.enqueueDPUVolumeAttachmentBySVVolumeAttachment),
	}), nil
}

// selectDPUFunction selects the appropriate DPU from a list of candidate DPUs.
//
// For single DPU, returns it directly. For multiple DPUs, selects the one with
// the preferred storage annotation (storage.nvidia.com/preferred-dpu).
// Returns error if no candidates, multiple preferred, or none preferred when multiple available.
func selectDPUFunction(ctx context.Context, dpuNode *provisioningv1.DPUNode, dpus []provisioningv1.DPU) (*provisioningv1.DPU, error) {
	if len(dpus) == 0 {
		return nil, errors.New("no candidate DPUs provided")
	}
	if len(dpus) == 1 {
		return &dpus[0], nil
	}
	var preferred []provisioningv1.DPU
	for _, dpu := range dpus {
		if dpu.Annotations != nil {
			if _, ok := dpu.Annotations[preferredStorageDPUAnnotation]; ok {
				preferred = append(preferred, dpu)
			}
		}
	}
	if len(preferred) == 0 {
		return nil, fmt.Errorf("multiple DPUs provided but none has %s annotation", preferredStorageDPUAnnotation)
	}
	if len(preferred) == 1 {
		return &preferred[0], nil
	}
	return nil, fmt.Errorf("multiple DPUs have %s annotation", preferredStorageDPUAnnotation)
}

// SetupWithManager sets up the controller with the Manager.
func (r *DPUVolumeAttachmentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.dpuClusterHelper = dpuclusterhelper.New(mgr.GetClient(), r.RemoteCache)
	r.ownedByHelper = utils.New(dpuVolumeAttachmentOwnedByAnnotation)
	r.dpuSelector = dpuselector.New(dpuselector.WithInNamespace{Namespace: r.Options.Namespace}, dpuselector.WithDPUSelectFunc{SelectFunc: selectDPUFunction})
	r.controllerAttacher = controllerattacher.NewControllerAttacher(r.Options.TargetNamespace, r.ownedByHelper)
	r.dpuAttacher = dpuattacher.NewDPUAttacher(r.Options.TargetNamespace, r.ownedByHelper)
	c, err := ctrl.NewControllerManagedBy(mgr).
		For(&storagev1.DPUVolumeAttachment{}).
		Watches(&provisioningv1.DPUCluster{}, handler.EnqueueRequestsFromMapFunc(r.enqueueDPUVolumeAttachmentByDPUCluster)).
		Watches(&provisioningv1.DPU{}, handler.EnqueueRequestsFromMapFunc(r.enqueueDPUVolumeAttachmentByDPU)).
		Watches(&provisioningv1.DPUNode{}, handler.EnqueueRequestsFromMapFunc(r.enqueueDPUVolumeAttachmentByDPUNode)).
		Watches(&storagev1.DPUVolume{}, handler.EnqueueRequestsFromMapFunc(r.enqueueDPUVolumeAttachmentByDPUVolume),
			builder.WithPredicates(utilsPredicates.PredicateFuncsByEventTypes(event.UpdateEvent{}))).
		Build(r)
	if err != nil {
		return err
	}
	r.controller = c
	return nil
}
