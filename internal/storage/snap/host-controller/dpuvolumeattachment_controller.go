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
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	utilsPredicates "github.com/nvidia/doca-platform/pkg/utils/predicates"

	"github.com/fluxcd/pkg/runtime/patch"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
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
}

const (
	dpuVolumeAttachmentControllerName = "dpuvolumeattachmentcontroller"
)

// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=dpuvolumes,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=dpuvolumes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=dpuvolumes/finalizers,verbs=update
// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=dpuvolumeattachments,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=dpuvolumeattachments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=storage.dpu.nvidia.com,resources=dpuvolumeattachments/finalizers,verbs=update
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpuclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpus,verbs=get;list;watch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpunodes,verbs=get;list;watch

// Reconcile reconciles changes in a DPUVolumeAttachment.
//
//nolint:dupl
func (r *DPUVolumeAttachmentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Reconciling")

	dpuCluster := &provisioningv1.DPUCluster{}
	targetDPUCluster := r.Options.DPUCluster
	err := r.Get(ctx, targetDPUCluster, dpuCluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get DPU cluster %s: %w", targetDPUCluster.String(), err)
	}
	if err := r.watchVolumeAttachmentsInDPUCluster(ctx, targetDPUCluster); err != nil {
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

	dpuVolumeAttachment := &storagev1.DPUVolumeAttachment{}
	if err := r.Client.Get(ctx, req.NamespacedName, dpuVolumeAttachment); err != nil {
		if apierrors.IsNotFound(err) {
			// DPUVolumeAttachment is not found, but we need to ensure that the corresponding
			// VolumeAttachment in the DPU cluster is also removed to avoid orphaned resources.
			return cleanupOrphanedObject(ctx, dpuClusterClient,
				r.getVolumeAttachmentNameFromDPUVolumeAttachmentName(req.NamespacedName),
				&storagev1.VolumeAttachment{},
				"VolumeAttachment")
		}
		return ctrl.Result{}, err
	}

	patcher := patch.NewSerialPatcher(dpuVolumeAttachment, r.Client)

	conditions.EnsureConditions(dpuVolumeAttachment, storagev1.DPUVolumeAttachmentConditions)

	// Defer a patch call to always patch the object when Reconcile exits.
	defer func() {
		r.finalizeConditions(dpuVolumeAttachment, reterr)
		reqLog.Info("Patching")
		if err := patcher.Patch(ctx, dpuVolumeAttachment,
			patch.WithFieldOwner(dpuVolumeAttachmentControllerName),
			patch.WithStatusObservedGeneration{},
			patch.WithOwnedConditions{Conditions: conditions.TypesAsStrings(storagev1.DPUVolumeAttachmentConditions)},
		); kerrors.FilterOut(err, apierrors.IsNotFound) != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	// Handle deletion reconciliation loop.
	if !dpuVolumeAttachment.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, dpuClusterClient, dpuVolumeAttachment)
	}

	// Add finalizer if not set.
	if !controllerutil.ContainsFinalizer(dpuVolumeAttachment, storagev1.DPUVolumeAttachmentFinalizer) {
		reqLog.Info("Adding finalizer")
		controllerutil.AddFinalizer(dpuVolumeAttachment, storagev1.DPUVolumeAttachmentFinalizer)
		return ctrl.Result{}, nil
	}

	return r.reconcile(ctx, dpuClusterClient, dpuVolumeAttachment)
}

func (r *DPUVolumeAttachmentReconciler) finalizeConditions(dpuVolumeAttachment *storagev1.DPUVolumeAttachment, err error) {
	// in case of any error set ConditionDPUVolumeAttachmentReconciled to false with error reason
	if err != nil {
		conditions.AddFalse(dpuVolumeAttachment, storagev1.ConditionDPUVolumeAttachmentReconciled,
			conditions.ReasonError, conditions.ConditionMessage(err.Error()))
	}
	conditions.SetSummary(dpuVolumeAttachment)
}

// reconcile handles the main reconciliation loop
//
//nolint:unparam
func (r *DPUVolumeAttachmentReconciler) reconcile(ctx context.Context, dpuClusterClient client.Client,
	dpuVolumeAttachment *storagev1.DPUVolumeAttachment) (ctrl.Result, error) {
	reqLog := ctrllog.FromContext(ctx)

	dpuVolumeNamespacedName := types.NamespacedName{
		Name: dpuVolumeAttachment.Spec.DPUVolumeName, Namespace: dpuVolumeAttachment.Namespace}
	dpuVolume := &storagev1.DPUVolume{}
	err := r.Client.Get(ctx, dpuVolumeNamespacedName, dpuVolume)
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, fmt.Errorf("failed to read %s CR %s: %w", storagev1.DPUVolumeKind, dpuVolumeNamespacedName.String(), err)
	}
	if apierrors.IsNotFound(err) {
		return r.reconcilePending(reqLog, dpuVolumeAttachment,
			fmt.Sprintf("%s %s not found", storagev1.DPUVolumeKind, dpuVolumeNamespacedName.String()))
	}

	if !dpuVolume.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcilePending(reqLog, dpuVolumeAttachment,
			fmt.Sprintf("%s %s is being deleted", storagev1.DPUVolumeKind, dpuVolumeNamespacedName.String()))
	}

	if !conditions.IsTrue(dpuVolume, conditions.TypeReady) {
		return r.reconcilePending(reqLog, dpuVolumeAttachment,
			fmt.Sprintf("%s %s is not ready yet", storagev1.DPUVolumeKind, dpuVolumeNamespacedName.String()))
	}

	dpuNodeNamespacedName := types.NamespacedName{Name: dpuVolumeAttachment.Spec.DPUNodeName, Namespace: dpuVolumeAttachment.Namespace}
	dpuNode := &provisioningv1.DPUNode{}

	err = r.Client.Get(ctx, dpuNodeNamespacedName, dpuNode)
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, fmt.Errorf("failed to read %s CR %s: %w", provisioningv1.DPUNodeKind, dpuNodeNamespacedName.String(), err)
	}
	if apierrors.IsNotFound(err) {
		return r.reconcilePending(reqLog, dpuVolumeAttachment,
			fmt.Sprintf("%s %s not found", provisioningv1.DPUNodeKind, dpuNodeNamespacedName.String()))
	}

	if !dpuNode.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcilePending(reqLog, dpuVolumeAttachment,
			fmt.Sprintf("%s %s is being deleted", provisioningv1.DPUNodeKind, dpuNodeNamespacedName.String()))
	}

	dpuList, err := r.getDPUsByDPUNode(ctx, dpuNode)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list %s CRs: %w", provisioningv1.DPUKind, err)
	}
	if len(dpuList) == 0 {
		return r.reconcilePending(reqLog, dpuVolumeAttachment, fmt.Sprintf("%s related to %s %s not found",
			provisioningv1.DPUKind, provisioningv1.DPUNodeKind, dpuNodeNamespacedName.String()))
	}

	if len(dpuList) > 1 {
		// when the multi DPU support will be implement the controller should be updated to handle this scenario properly
		return ctrl.Result{}, fmt.Errorf("multiple %s CRs found for %s %s, this is not supported",
			provisioningv1.DPUKind, provisioningv1.DPUNodeKind, dpuNodeNamespacedName.String())
	}

	selectedDPU := dpuList[0]

	if !selectedDPU.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcilePending(reqLog, dpuVolumeAttachment,
			fmt.Sprintf("%s %s is being deleted", provisioningv1.DPUKind, client.ObjectKeyFromObject(&selectedDPU).String()))
	}

	conflictingDPUVolumeAttachments, err := r.getConflictingDPUVolumeAttachments(ctx, dpuVolumeAttachment)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get conflicting DPUVolumeAttachments: %w", err)
	}

	// Prevent multiple DPUVolumeAttachments with the same DPUVolumeName
	// and DPUNodeName from existing simultaneously. If such attachments
	// exist, wait for them to be deleted before proceeding. Skip this
	// check for attachments that have already been fully reconciled to
	// avoid unnecessary pending states after controller restarts.
	if len(conflictingDPUVolumeAttachments) > 0 && !conditions.IsTrue(dpuVolumeAttachment, storagev1.ConditionDPUVolumeAttachmentReconciled) {
		return r.reconcilePending(reqLog, dpuVolumeAttachment,
			fmt.Sprintf("DPUVolume %s has multiple attachments to DPUNode %s, waiting for the conflicting attachments to be removed",
				dpuVolumeNamespacedName.String(), dpuNodeNamespacedName.String()))
	}

	desiredVolumeAttachment := r.getDesiredVolumeAttachment(dpuVolumeAttachment, selectedDPU)
	apiVolumeAttachment := &storagev1.VolumeAttachment{}
	err = dpuClusterClient.Get(ctx,
		r.getVolumeAttachmentNameFromDPUVolumeAttachmentName(client.ObjectKeyFromObject(dpuVolumeAttachment)),
		apiVolumeAttachment)
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, fmt.Errorf("failed to read VolumeAttachment from DPU cluster: %w", err)
	}
	if apierrors.IsNotFound(err) {
		reqLog.Info("VolumeAttachment not found in the DPU cluster, creating")
		apiVolumeAttachment = desiredVolumeAttachment
		if err := dpuClusterClient.Create(ctx, apiVolumeAttachment); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to create VolumeAttachment in DPU cluster: %w", err)
		}
	} else {
		reqLog.Info("VolumeAttachment found in the DPU cluster")
		// Check if VolumeAttachment is being deleted in the DPU cluster.
		// This is an edge case that may occur due to manual intervention (e.g. manual deletion of the VolumeAttachment in the DPU cluster).
		// In this case we wait for deletion to complete and then recreate the VolumeAttachment.
		if !apiVolumeAttachment.ObjectMeta.DeletionTimestamp.IsZero() {
			reqLog.Info("VolumeAttachment in the DPU cluster is deleting")
			r.setAwaitingDeletion(dpuVolumeAttachment)
			return ctrl.Result{}, nil
		}
		if !r.validateExistingVolumeAttachmentSpec(desiredVolumeAttachment, apiVolumeAttachment) {
			reqLog.Info("VolumeAttachment in the DPU cluster has different parameters, recreate")
			if err := dpuClusterClient.Delete(ctx, apiVolumeAttachment); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to delete VolumeAttachment in DPU cluster: %w", err)
			}
			r.setAwaitingDeletion(dpuVolumeAttachment)
			return ctrl.Result{}, nil
		}
	}
	r.updateDPUVolumeAttachmentStatus(dpuVolumeAttachment, apiVolumeAttachment)
	conditions.AddTrue(dpuVolumeAttachment, storagev1.ConditionDPUVolumeAttachmentReconciled)
	return ctrl.Result{}, nil
}

//nolint:unparam
func (r *DPUVolumeAttachmentReconciler) reconcileDelete(ctx context.Context,
	dpuClusterClient client.Client, dpuVolumeAttachment *storagev1.DPUVolumeAttachment) (ctrl.Result, error) {
	reqLog := ctrllog.FromContext(ctx)
	reqLog.Info("Reconciling delete")
	r.setAwaitingDeletion(dpuVolumeAttachment)
	apiVolumeAttachment := &storagev1.VolumeAttachment{}
	err := dpuClusterClient.Get(ctx,
		r.getVolumeAttachmentNameFromDPUVolumeAttachmentName(client.ObjectKeyFromObject(dpuVolumeAttachment)),
		apiVolumeAttachment)
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, fmt.Errorf("failed to get VolumeAttachment from DPU cluster: %w", err)
	}
	if apierrors.IsNotFound(err) {
		reqLog.Info("VolumeAttachment in the DPU cluster not found, removing finalizer")
		controllerutil.RemoveFinalizer(dpuVolumeAttachment, storagev1.DPUVolumeAttachmentFinalizer)
		return ctrl.Result{}, nil
	}
	if !apiVolumeAttachment.GetDeletionTimestamp().IsZero() {
		reqLog.Info("VolumeAttachment in the DPU cluster is already deleting")
		return ctrl.Result{}, nil
	}
	reqLog.Info("Delete VolumeAttachment")
	err = dpuClusterClient.Delete(ctx, apiVolumeAttachment)
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, fmt.Errorf("failed to delete VolumeAttachment from DPU cluster: %w", err)
	}
	if apierrors.IsNotFound(err) {
		reqLog.Info("VolumeAttachment in the DPU cluster not found, removing finalizer")
		controllerutil.RemoveFinalizer(dpuVolumeAttachment, storagev1.DPUVolumeAttachmentFinalizer)
		return ctrl.Result{}, nil
	}
	reqLog.Info("VolumeAttachment in the DPU cluster is deleting")
	return ctrl.Result{}, nil
}

// reset the state in the DPUVolumeAttachment CR that is build from the state of the VolumeAttachment CR in DPU cluster and
// set ConditionDPUVolumeAttachmentReconciled to ReasonAwaitingDeletion
func (r *DPUVolumeAttachmentReconciler) setAwaitingDeletion(dpuVolumeAttachment *storagev1.DPUVolumeAttachment) {
	conditions.AddFalse(dpuVolumeAttachment, storagev1.ConditionDPUVolumeAttachmentReconciled,
		conditions.ReasonAwaitingDeletion, "VolumeAttachment is deleting")
}

// returns name of the VolumeAttachment CR in the DPU cluster for the provided DPUVolumeAttachment name from the host cluster.
func (r *DPUVolumeAttachmentReconciler) getVolumeAttachmentNameFromDPUVolumeAttachmentName(dpuVolumeAttachmentName types.NamespacedName) types.NamespacedName {
	return types.NamespacedName{Namespace: r.Options.TargetNamespace, Name: dpuVolumeAttachmentName.Name}
}

// getDesiredVolumeAttachment creates a VolumeAttachment object with the desired configuration
// based on the DPUVolumeAttachment and selected DPU.
func (r *DPUVolumeAttachmentReconciler) getDesiredVolumeAttachment(dpuVolumeAttachment *storagev1.DPUVolumeAttachment, selectedDPU provisioningv1.DPU) *storagev1.VolumeAttachment {
	volumeAttachmentNamespacedName := r.getVolumeAttachmentNameFromDPUVolumeAttachmentName(client.ObjectKeyFromObject(dpuVolumeAttachment))
	return &storagev1.VolumeAttachment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      volumeAttachmentNamespacedName.Name,
			Namespace: volumeAttachmentNamespacedName.Namespace,
		},
		Spec: storagev1.VolumeAttachmentSpec{
			// the name of the DPU's k8s Node object in the DPUCluster
			NodeName: selectedDPU.Name,
			Source: storagev1.VolumeSource{
				VolumeRef: &storagev1.ObjectRef{
					// Name of Volume object in the DPU Cluster match the name of the DPUVolume CR
					// to which the DPUVolumeAttachment points
					Name:      dpuVolumeAttachment.Spec.DPUVolumeName,
					Namespace: r.Options.TargetNamespace,
				},
			},
			FunctionTypeConfig: dpuVolumeAttachment.Spec.FunctionTypeConfig,
		},
	}
}

// updateDPUVolumeAttachmentStatus updates the DPUVolumeAttachment status fields based on the
// VolumeAttachment status from the DPU cluster.
func (r *DPUVolumeAttachmentReconciler) updateDPUVolumeAttachmentStatus(dpuVolumeAttachment *storagev1.DPUVolumeAttachment, apiVolumeAttachment *storagev1.VolumeAttachment) {
	// reset the status to make sure that we are in sync with the VolumeAttachment object state in the DPU cluster,
	// we need to keep the conditions and observed generation
	origStatus := dpuVolumeAttachment.Status
	dpuVolumeAttachment.Status = storagev1.DPUVolumeAttachmentStatus{}
	dpuVolumeAttachment.Status.Conditions = origStatus.Conditions
	dpuVolumeAttachment.Status.ObservedGeneration = origStatus.ObservedGeneration

	dpuVolumeAttachment.Status.ControllerAttached = &apiVolumeAttachment.Status.StorageAttached
	dpuVolumeAttachment.Status.DPUAttached = &apiVolumeAttachment.Status.DPU.Attached
	if apiVolumeAttachment.Status.Message != "" {
		dpuVolumeAttachment.Status.Message = &apiVolumeAttachment.Status.Message
	}
	dpuVolumeAttachment.Status.AttachmentMetadata = apiVolumeAttachment.Spec.Parameters
	dpuVolumeAttachment.Status.DPU = &storagev1.AttachmentStatusDPU{}
	if apiVolumeAttachment.Status.DPU.PCIDeviceAddress != "" {
		dpuVolumeAttachment.Status.DPU.PCIAddress = &apiVolumeAttachment.Status.DPU.PCIDeviceAddress
	}
	if apiVolumeAttachment.Status.DPU.DeviceName != "" {
		dpuVolumeAttachment.Status.DPU.DeviceName = &apiVolumeAttachment.Status.DPU.DeviceName
	}
	if apiVolumeAttachment.Status.DPU.BdevAttrs.NVMeNsID > 0 || apiVolumeAttachment.Status.DPU.BdevAttrs.NVMeUUID != "" {
		dpuVolumeAttachment.Status.DPU.NVMEAttrs = &storagev1.NVMEAttrs{
			NamespaceID:   &apiVolumeAttachment.Status.DPU.BdevAttrs.NVMeNsID,
			NamespaceUUID: &apiVolumeAttachment.Status.DPU.BdevAttrs.NVMeUUID,
		}
	}
	if apiVolumeAttachment.Status.DPU.FSdevAttrs.FilesystemTag != "" {
		dpuVolumeAttachment.Status.DPU.VirtioFSAttrs = &storagev1.VirtioFSAttrs{
			FilesystemTag: &apiVolumeAttachment.Status.DPU.FSdevAttrs.FilesystemTag,
		}
	}

	if *dpuVolumeAttachment.Status.ControllerAttached {
		conditions.AddTrue(dpuVolumeAttachment, storagev1.ConditionDPUVolumeAttachmentControllerAttached)
	} else {
		conditions.AddFalse(dpuVolumeAttachment, storagev1.ConditionDPUVolumeAttachmentControllerAttached, conditions.ReasonPending, "volume is not attached by the controller yet")
	}
	if *dpuVolumeAttachment.Status.DPUAttached {
		conditions.AddTrue(dpuVolumeAttachment, storagev1.ConditionDPUVolumeAttachmentDPUAttached)
	} else {
		message := "volume is not attached to dpu"
		if dpuVolumeAttachment.Status.Message != nil {
			message = fmt.Sprintf("volume is not attached to dpu, reason: %s", *dpuVolumeAttachment.Status.Message)
		}
		conditions.AddFalse(dpuVolumeAttachment, storagev1.ConditionDPUVolumeAttachmentDPUAttached, conditions.ReasonPending,
			conditions.ConditionMessage(message))
	}
}

// check that spec of the existing VolumeAttachment has desired spec
func (r *DPUVolumeAttachmentReconciler) validateExistingVolumeAttachmentSpec(desiredVolumeAttachment *storagev1.VolumeAttachment,
	actualVolumeAttachment *storagev1.VolumeAttachment) bool {
	return desiredVolumeAttachment.Spec.NodeName == actualVolumeAttachment.Spec.NodeName &&
		equality.Semantic.DeepEqual(desiredVolumeAttachment.Spec.Source, actualVolumeAttachment.Spec.Source) &&
		equality.Semantic.DeepEqual(desiredVolumeAttachment.Spec.FunctionTypeConfig, actualVolumeAttachment.Spec.FunctionTypeConfig)
}

// getDPUByDPUNode returns the list of DPUs object related to the provided DPUNode
func (r *DPUVolumeAttachmentReconciler) getDPUsByDPUNode(ctx context.Context, dpuNode *provisioningv1.DPUNode) ([]provisioningv1.DPU, error) {
	dpuList := &provisioningv1.DPUList{}
	if err := r.Client.List(ctx, dpuList, client.MatchingFields{dpuNodeNameIndexKey: dpuNode.Name}, client.InNamespace(r.Options.Namespace)); err != nil {
		return nil, fmt.Errorf("failed to list DPUs in the API: %w", err)
	}
	return dpuList.Items, nil
}

// getConflictingDPUVolumeAttachments returns a list of DPUVolumeAttachments that have the same DPUVolumeName and DPUNodeName.
func (r *DPUVolumeAttachmentReconciler) getConflictingDPUVolumeAttachments(ctx context.Context, dpuVolumeAttachment *storagev1.DPUVolumeAttachment) ([]storagev1.DPUVolumeAttachment, error) {
	dpuVolumeAttachmentList := &storagev1.DPUVolumeAttachmentList{}
	if err := r.Client.List(ctx, dpuVolumeAttachmentList, client.MatchingFields{dpuVolumeNameIndexKey: dpuVolumeAttachment.Spec.DPUVolumeName},
		client.InNamespace(r.Options.Namespace)); err != nil {
		return nil, fmt.Errorf("failed to list DPUVolumeAttachments in the API: %w", err)
	}
	conflictingDPUVolumeAttachments := []storagev1.DPUVolumeAttachment{}
	for _, a := range dpuVolumeAttachmentList.Items {
		if a.Name == dpuVolumeAttachment.Name && a.Namespace == dpuVolumeAttachment.Namespace {
			// skip the DPUVolumeAttachment that is being reconciled
			continue
		}
		if a.Spec.DPUNodeName == dpuVolumeAttachment.Spec.DPUNodeName {
			conflictingDPUVolumeAttachments = append(conflictingDPUVolumeAttachments, a)
		}
	}
	return conflictingDPUVolumeAttachments, nil
}

// reconcilePending is helper function to set ConditionDPUVolumeAttachmentReconciled
// to pending with provided reason
func (r *DPUVolumeAttachmentReconciler) reconcilePending(log logr.Logger, obj conditions.GetSet, msg string) (ctrl.Result, error) {
	conditions.AddFalse(obj,
		storagev1.ConditionDPUVolumeAttachmentReconciled,
		conditions.ReasonPending,
		conditions.ConditionMessage(msg))
	log.Info(msg)
	return ctrl.Result{}, nil
}

// enqueueAllDPUVolumeAttachments returns a MapFunc that enqueues all DPUVolumeAttachments
// without any filtering. This is used when a change affects all attachments.
func (r *DPUVolumeAttachmentReconciler) enqueueAllDPUVolumeAttachments() handler.MapFunc {
	return r.getEnqueueFunction(nil)
}

// enqueueDPUVolumeAttachmentByVolumeAttachment returns a MapFunc that enqueues a DPUVolumeAttachment
// based on changes to a VolumeAttachment in the target DPU cluster. This maps VolumeAttachment events
// from the remote DPU cluster to trigger reconciliation of the corresponding DPUVolumeAttachment in
// the host cluster. Only VolumeAttachments from the target namespace are processed.
func (r *DPUVolumeAttachmentReconciler) enqueueDPUVolumeAttachmentByVolumeAttachment() handler.MapFunc {
	return func(ctx context.Context, o client.Object) []reconcile.Request {
		va, ok := o.(*storagev1.VolumeAttachment)
		if !ok {
			return nil
		}
		if va.GetNamespace() != r.Options.TargetNamespace {
			// ignore the object if it is originated not from the target namespace
			return nil
		}
		return []reconcile.Request{{NamespacedName: client.ObjectKey{Namespace: r.Options.Namespace, Name: va.Name}}}
	}
}

// enqueueDPUVolumeAttachmentsByDPUNode returns a MapFunc that enqueues all DPUVolumeAttachments
// that reference the provided DPUNode by name. This is used when a DPUNode change should
// trigger reconciliation of related volume attachments.
func (r *DPUVolumeAttachmentReconciler) enqueueDPUVolumeAttachmentsByDPUNode() handler.MapFunc {
	return r.getEnqueueFunction(
		func(o client.Object, dpuVolumeAttachment *storagev1.DPUVolumeAttachment) bool {
			return dpuVolumeAttachment.Spec.DPUNodeName == o.GetName()
		})
}

// enqueueDPUVolumeAttachmentsByDPU returns a MapFunc that enqueues all DPUVolumeAttachments
// that are related to the provided DPU. The relationship is determined by matching the DPU's
// DPUNodeName with the attachment's DPUNodeName.
func (r *DPUVolumeAttachmentReconciler) enqueueDPUVolumeAttachmentsByDPU() handler.MapFunc {
	return r.getEnqueueFunction(
		func(o client.Object, dpuVolumeAttachment *storagev1.DPUVolumeAttachment) bool {
			dpu, ok := o.(*provisioningv1.DPU)
			if !ok {
				return false
			}
			return dpu.Spec.DPUNodeName == dpuVolumeAttachment.Spec.DPUNodeName
		})
}

// enqueueDPUVolumeAttachmentsByDPUVolume returns a MapFunc that enqueues all DPUVolumeAttachments
// that reference the provided DPUVolume by name. This is used when a DPUVolume change should
// trigger reconciliation of all attachments that reference it.
func (r *DPUVolumeAttachmentReconciler) enqueueDPUVolumeAttachmentsByDPUVolume() handler.MapFunc {
	return r.getEnqueueFunction(
		func(o client.Object, dpuVolumeAttachment *storagev1.DPUVolumeAttachment) bool {
			return dpuVolumeAttachment.Spec.DPUVolumeName == o.GetName()
		})
}

// enqueueDPUVolumeAttachmentsByConflicting returns a MapFunc that enqueues all DPUVolumeAttachments
// that have the same DPUVolumeName and DPUNodeName. This is used when a DPUVolumeAttachment change should
// trigger reconciliation of all attachments that have the same DPUVolumeName and DPUNodeName.
func (r *DPUVolumeAttachmentReconciler) enqueueDPUVolumeAttachmentsByConflicting() handler.MapFunc {
	return r.getEnqueueFunction(
		func(o client.Object, dpuVolumeAttachment *storagev1.DPUVolumeAttachment) bool {
			in, ok := o.(*storagev1.DPUVolumeAttachment)
			if !ok {
				return false
			}
			return in.Spec.DPUVolumeName == dpuVolumeAttachment.Spec.DPUVolumeName &&
				in.Spec.DPUNodeName == dpuVolumeAttachment.Spec.DPUNodeName
		})
}

// getEnqueueFunction is a helper function that returns a MapFunc for enqueuing DPUVolumeAttachments
// based on a provided filter function. If the filter function is nil, all DPUVolumeAttachments
// are enqueued. This helper is used by other enqueue methods to create filtered event handlers.
func (r *DPUVolumeAttachmentReconciler) getEnqueueFunction(
	filter func(o client.Object, dpuVolumeAttachment *storagev1.DPUVolumeAttachment) bool) handler.MapFunc {
	return func(ctx context.Context, o client.Object) []ctrl.Request {
		result := []ctrl.Request{}
		dpuVolumeAttachmentList := &storagev1.DPUVolumeAttachmentList{}
		if err := r.Client.List(ctx, dpuVolumeAttachmentList, client.InNamespace(r.Options.Namespace)); err != nil {
			return nil
		}
		for _, m := range dpuVolumeAttachmentList.Items {
			if filter == nil || filter(o, &m) {
				name := client.ObjectKey{Namespace: m.Namespace, Name: m.Name}
				result = append(result, ctrl.Request{NamespacedName: name})
			}
		}
		return result
	}
}

func (r *DPUVolumeAttachmentReconciler) watchVolumeAttachmentsInDPUCluster(ctx context.Context, dpuCluster client.ObjectKey) error {
	return r.RemoteCache.Watch(ctx, dpuCluster, dpucluster.NewWatcher(dpucluster.WatcherOptions{
		Name:         "dpuvolumeattachment-watch-volumeattachment",
		Watcher:      r.controller,
		Kind:         &storagev1.VolumeAttachment{},
		EventHandler: handler.EnqueueRequestsFromMapFunc(r.enqueueDPUVolumeAttachmentByVolumeAttachment()),
	}))
}

// SetupWithManager sets up the controller with the Manager.
func (r *DPUVolumeAttachmentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	c, err := ctrl.NewControllerManagedBy(mgr).
		For(&storagev1.DPUVolumeAttachment{}).
		// watch for DPUVolumeAttachment deletion and reconcile all attachments that have the same DPUVolumeName and DPUNodeName
		Watches(&storagev1.DPUVolumeAttachment{}, handler.EnqueueRequestsFromMapFunc(r.enqueueDPUVolumeAttachmentsByConflicting()),
			builder.WithPredicates(utilsPredicates.PredicateFuncsByEventTypes(event.DeleteEvent{}))).
		Watches(&provisioningv1.DPUCluster{}, handler.EnqueueRequestsFromMapFunc(r.enqueueAllDPUVolumeAttachments())).
		// watch for DPUNode creation and reconcile all attachments that have reference to it
		Watches(&provisioningv1.DPUNode{}, handler.EnqueueRequestsFromMapFunc(r.enqueueDPUVolumeAttachmentsByDPUNode()),
			builder.WithPredicates(utilsPredicates.PredicateFuncsByEventTypes(event.CreateEvent{}))).
		Watches(&provisioningv1.DPU{}, handler.EnqueueRequestsFromMapFunc(r.enqueueDPUVolumeAttachmentsByDPU()),
			builder.WithPredicates(utilsPredicates.PredicateFuncsByEventTypes(event.CreateEvent{}))).
		// watch for DPUVolume creation and reconcile all attachments that have reference to it
		Watches(&storagev1.DPUVolume{}, handler.EnqueueRequestsFromMapFunc(r.enqueueDPUVolumeAttachmentsByDPUVolume()),
			builder.WithPredicates(utilsPredicates.PredicateFuncsByEventTypes(event.CreateEvent{}, event.UpdateEvent{}))).
		Build(r)
	if err != nil {
		return err
	}
	r.controller = c
	return nil
}
