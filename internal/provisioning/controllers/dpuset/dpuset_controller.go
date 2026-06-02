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

package dpuset

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"sort"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/digest"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/events"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/reboot"
	"github.com/nvidia/doca-platform/internal/release"
	"github.com/nvidia/doca-platform/internal/utils"
	"github.com/nvidia/doca-platform/pkg/conditions"

	"github.com/fluxcd/pkg/runtime/patch"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	// controller name that will be used when
	DPUSetControllerName = "dpuset"
)

type DPUSetOptions struct {
	DPUInstallInterface string
}

// DPUSetReconciler reconciles a DPUSet object
type DPUSetReconciler struct {
	client.Client
	Options  DPUSetOptions
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpusets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpusets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpusets/finalizers,verbs=update
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpuflavors,verbs=get;list;watch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpudevices,verbs=get;list;watch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpunodes,verbs=get;list;watch

func (r *DPUSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconcile")

	dpuSet := &provisioningv1.DPUSet{}
	if err := r.Get(ctx, req.NamespacedName, dpuSet); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get DPUSet %w", err)
	}

	patcher := patch.NewSerialPatcher(dpuSet, r.Client)
	// Defer a patch call to always patch the object when Reconcile exits.
	defer func() {
		logger.Info("Patching")
		if err := patcher.Patch(ctx, dpuSet,
			patch.WithFieldOwner(DPUSetControllerName),
			patch.WithStatusObservedGeneration{},
			patch.WithOwnedConditions{Conditions: conditions.TypesAsStrings(provisioningv1.DPUSetConditions)},
		); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	conditions.EnsureConditions(dpuSet, provisioningv1.DPUSetConditions)

	if !dpuSet.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.reconcileDelete(ctx, dpuSet)
	}

	return r.Handle(ctx, dpuSet)
}

func (r *DPUSetReconciler) validateDPUSet(dpuSet *provisioningv1.DPUSet) error {
	if r.Options.DPUInstallInterface == string(provisioningv1.InstallViaRedFish) {
		if dpuSet.Spec.DPUTemplate.Spec.NodeEffect.IsCustomLabel() || dpuSet.Spec.DPUTemplate.Spec.NodeEffect.IsTaint() || dpuSet.Spec.DPUTemplate.Spec.NodeEffect.IsDrain() {
			message := fmt.Sprintf("NodeEffect is not allowed to be %s when using RedFish Install Interface", dpuSet.Spec.DPUTemplate.Spec.NodeEffect.String())
			conditions.AddFalse(dpuSet, provisioningv1.ConditionDPUSetReconciled, conditions.ReasonError, conditions.ConditionMessage(message))
			return fmt.Errorf("invalid NodeEffect: %s", message)
		}
	}
	return nil
}

func (r *DPUSetReconciler) reconcileDelete(ctx context.Context, dpuSet *provisioningv1.DPUSet) error {
	logger := log.FromContext(ctx)
	logger.Info("Reconcile Delete DPUSet")

	dpuList := &provisioningv1.DPUList{}
	if err := r.List(ctx, dpuList, client.MatchingLabels{
		cutil.DPUSetNameLabel:      dpuSet.Name,
		cutil.DPUSetNamespaceLabel: dpuSet.Namespace,
	}); err != nil {
		return fmt.Errorf("failed to list DPUs %w", err)
	}

	// Processing logic:
	// - Iterate over all DPUs.
	// - If a DPU is already in deletion, log the event and skip it.
	// - Otherwise, initiate deletion.
	// - After the loop, check if any DPUs exist. If none remain, return an error to trigger exponential backoff.
	for _, dpu := range dpuList.Items {
		if !dpu.DeletionTimestamp.IsZero() {
			logger.Info("Waiting for DPU deletion", "DPU", dpu.Name)
			continue
		}

		if err := r.Delete(ctx, &dpu); err != nil {
			logger.Error(err, "Failed to delete DPU", "DPU", dpu.Name)
			return fmt.Errorf("failed to delete DPU %w", err)
		}

		logger.Info("Delete DPU", "DPU", dpu.Name)
	}
	if len(dpuList.Items) > 0 {
		return fmt.Errorf("not all DPUs are deleted")
	}

	if err := r.cleanupCollisionLabels(ctx, dpuSet); err != nil {
		return fmt.Errorf("failed to clean up collision labels: %w", err)
	}

	controllerutil.RemoveFinalizer(dpuSet, provisioningv1.DPUSetFinalizer)

	return nil
}

// cleanupCollisionLabels removes this DPUSet's collision label from all DPUs that have it.
func (r *DPUSetReconciler) cleanupCollisionLabels(ctx context.Context, dpuSet *provisioningv1.DPUSet) error {
	collisionKey := cutil.GenerateDPUSetCollisionLabelKey(dpuSet.Name)
	dpuList := &provisioningv1.DPUList{}
	if err := r.List(ctx, dpuList, client.InNamespace(dpuSet.Namespace), client.MatchingLabels{
		collisionKey: "true",
	}); err != nil {
		return fmt.Errorf("failed to list DPUs with collision label for DPUSet %s: %w", dpuSet.Name, err)
	}
	for i := range dpuList.Items {
		if err := r.removeCollisionLabel(ctx, &dpuList.Items[i], collisionKey); err != nil {
			return fmt.Errorf("failed to remove collision label from DPU %s: %w", dpuList.Items[i].Name, err)
		}
	}
	return nil
}

func (r *DPUSetReconciler) Handle(ctx context.Context, dpuSet *provisioningv1.DPUSet) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if err := r.validateDPUSet(dpuSet); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to validate DPUSet %w", err)
	}

	dpuClusterList := &provisioningv1.DPUClusterList{}
	if err := r.List(ctx, dpuClusterList); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list DPUClusters %w", err)
	}

	if dpuSet.GetLabels() == nil {
		dpuSet.SetLabels(make(map[string]string))
	}

	// Only compute hash if template spec has changed
	var dpuTemplateSpecHash string
	existingHash, exists := dpuSet.GetLabels()[cutil.DPUSetDPUTemplateSpecHashLabelKey]
	// Check if we need to recompute by comparing generation or if hash is missing
	if exists && dpuSet.Status.ObservedGeneration == dpuSet.Generation {
		dpuTemplateSpecHash = existingHash
	} else {
		// First time or hash is missing, compute it
		dpuTemplateSpecHash = calculateDPUTemplateSpecDigest(&dpuSet.Spec.DPUTemplate.Spec)
	}

	dpuSet.GetLabels()[cutil.DPUSetDPUTemplateSpecHashLabelKey] = dpuTemplateSpecHash

	// Add finalizer if not set.
	if !controllerutil.ContainsFinalizer(dpuSet, provisioningv1.DPUSetFinalizer) {
		controllerutil.AddFinalizer(dpuSet, provisioningv1.DPUSetFinalizer)
		return ctrl.Result{}, nil
	}

	// Get dpuDevice map by dpuNodeSelector and dpuSelector
	dpuDeviceMap, err := r.getDPUDeviceMap(ctx, dpuSet)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get DPUDevice map %w", err)
	}
	logger.Info(fmt.Sprintf("DPUSet %s/%s selected %d DPUDevices", dpuSet.Namespace, dpuSet.Name, len(dpuDeviceMap)))

	if err := r.validateDPUClusterMetadata(dpuSet, dpuDeviceMap); err != nil {
		conditions.AddFalse(dpuSet, conditions.TypeReady, conditions.ConditionReason("ClusterMetadataConflict"), conditions.ConditionMessage(err.Error()))
		r.Recorder.Eventf(dpuSet, corev1.EventTypeWarning, events.EventClusterMetadataConflictReason, "%v", err)
		return ctrl.Result{}, nil
	}

	// Get dpu map which are owned by dpuset
	dpuMap, err := r.GetDPUsMap(ctx, dpuSet)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get DPU map %w", err)
	}

	dpusCreated, dpusOwnedByOtherDPUSet, err := r.createMissingDPUs(ctx, dpuSet, dpuDeviceMap, dpuMap)
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := r.removeStaleCollisionLabels(ctx, dpuSet, dpuDeviceMap); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.deleteStaleDPUs(ctx, dpuSet, dpuDeviceMap, dpuMap); err != nil {
		return ctrl.Result{}, err
	}

	// handle rolling update
	dpuMap, err = r.GetDPUsMap(ctx, dpuSet)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get DPUs %w", err)
	}

	switch dpuSet.Spec.Strategy.Type {
	case provisioningv1.OnDeleteStrategyType:
		if err := r.onDelete(ctx, dpuSet, dpuMap, dpuClusterList.Items); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to on delete DPUs %w", err)
		}
	case provisioningv1.RollingUpdateStrategyType:
		if err := r.rolloutRolling(ctx, dpuSet, dpuMap, len(dpuDeviceMap), dpuClusterList.Items); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to rollout DPU %w", err)
		}
	default:
		return ctrl.Result{}, fmt.Errorf("unsupported strategy type %q", dpuSet.Spec.Strategy.Type)
	}

	dpusUpdated, err := r.UpdateDPUs(ctx, dpuSet, dpuMap, dpuDeviceMap)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update node effect ApplyOnLabelChange for DPUs %w", err)
	}

	if err := r.reconcileDPUOutdatedStatus(ctx, dpuSet, dpuMap, dpuClusterList.Items); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile DPU Outdated status: %w", err)
	}

	// Check if any DPU changes were made (created or updated)
	dpusChanged := dpusCreated || dpusUpdated

	if err := r.UpdateDPUSetStatus(ctx, dpuSet, dpusChanged, len(dpuDeviceMap), dpusOwnedByOtherDPUSet); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update DPUSet status: %w", err)
	}

	return ctrl.Result{}, nil
}

// reconcileDPUOutdatedStatus writes (or clears) dpu.status.outdated on each DPU
// owned by the DPUSet. The struct is set whenever a disruptive template field
// has diverged (regardless of strategy); it is cleared when the DPU matches
// the template or only the cluster-selector has drifted.
//
// The dpuMap argument is a snapshot captured earlier in the same reconcile,
// so entries can be stale by the time this method runs (e.g. onDelete /
// rolloutRolling may have just deleted a DPU). We skip entries already marked
// for deletion in the snapshot, and tolerate NotFound on the status patch for
// DPUs that have been deleted since — there's nothing to update on the API
// server, and treating it as fatal would fail the whole reconcile.
func (r *DPUSetReconciler) reconcileDPUOutdatedStatus(ctx context.Context, dpuSet *provisioningv1.DPUSet,
	dpuMap map[string]provisioningv1.DPU, dpuClusters []provisioningv1.DPUCluster) error {
	logger := log.FromContext(ctx)
	for i := range dpuMap {
		dpu := dpuMap[i]
		if !dpu.DeletionTimestamp.IsZero() {
			continue
		}

		outdated, reason, msg := r.detectOutdated(*dpuSet, dpu, dpuClusters)

		var desired *provisioningv1.DPUOutdated
		if outdated {
			desired = &provisioningv1.DPUOutdated{Reason: reason, Message: msg}
		}
		if !outdatedNeedsUpdate(dpu.Status.Outdated, desired) {
			continue
		}

		if desired != nil {
			// Preserve TimeStamp across reconciles when Reason is unchanged so
			// it reflects when the current drift was first observed.
			if dpu.Status.Outdated != nil && dpu.Status.Outdated.Reason == desired.Reason {
				desired.TimeStamp = dpu.Status.Outdated.TimeStamp
			} else {
				desired.TimeStamp = metav1.Now()
			}
		}

		patched := dpu.DeepCopy()
		patched.Status.Outdated = desired

		logger.V(2).Info("Updating DPU Outdated status",
			"DPU", fmt.Sprintf("%s/%s", dpu.Namespace, dpu.Name),
			"outdated", outdated, "reason", reason)
		err := r.Status().Patch(ctx, patched, client.MergeFrom(&dpu), client.FieldOwner(DPUSetControllerName))
		switch {
		case err == nil:
		case apierrors.IsNotFound(err):
			// The DPU was deleted (likely by onDelete / rolloutRolling earlier
			// in this same reconcile) after we took the dpuMap snapshot.
			// Nothing to update; carry on with the remaining DPUs.
			logger.V(2).Info("Skipping DPU Outdated status patch; DPU no longer exists",
				"DPU", fmt.Sprintf("%s/%s", dpu.Namespace, dpu.Name))
		default:
			return fmt.Errorf("failed to patch DPU (%s/%s) outdated status: %w", dpu.Namespace, dpu.Name, err)
		}
	}
	return nil
}

// outdatedNeedsUpdate reports whether dpu.status.outdated must be patched to
// reach the desired state. It returns true iff exactly one of curr/desired is
// nil, or both are non-nil with a different Reason or Message. TimeStamp is
// intentionally not compared because it is derived from Reason continuity.
func outdatedNeedsUpdate(curr, desired *provisioningv1.DPUOutdated) bool {
	if (curr == nil) != (desired == nil) {
		return true
	}
	if curr == nil {
		return false
	}
	return curr.Reason != desired.Reason || curr.Message != desired.Message
}

// SetupWithManager sets up the controller with the Manager.
func (r *DPUSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&provisioningv1.DPUSet{}).
		Owns(&provisioningv1.DPU{}).
		Watches(&provisioningv1.DPUDevice{},
			handler.EnqueueRequestsFromMapFunc(r.resourceToDPUSetReq)).
		Watches(&provisioningv1.DPUNode{},
			handler.EnqueueRequestsFromMapFunc(r.resourceToDPUSetReq)).
		Watches(&provisioningv1.DPUFlavor{},
			handler.EnqueueRequestsFromMapFunc(r.flavorToDPUSetReq)).
		Watches(&provisioningv1.DPUCluster{},
			handler.EnqueueRequestsFromMapFunc(r.resourceToDPUSetReq)).
		Complete(r)
}

func (r *DPUSetReconciler) resourceToDPUSetReq(ctx context.Context, resource client.Object) []reconcile.Request {
	requests := make([]reconcile.Request, 0)
	dpuSetList := &provisioningv1.DPUSetList{}
	if err := r.List(ctx, dpuSetList); err == nil {
		for _, item := range dpuSetList.Items {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      item.GetName(),
					Namespace: item.GetNamespace(),
				}})
		}
	}
	return requests
}

func (r *DPUSetReconciler) flavorToDPUSetReq(ctx context.Context, resource client.Object) []reconcile.Request {
	flavor := resource.(*provisioningv1.DPUFlavor)
	dpuSetList := &provisioningv1.DPUSetList{}
	if err := r.List(ctx, dpuSetList); err != nil {
		return nil
	}
	requests := []reconcile.Request{}
	for _, item := range dpuSetList.Items {
		if item.Spec.DPUTemplate.Spec.DPUFlavor != flavor.Name {
			continue
		}
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      item.GetName(),
				Namespace: item.GetNamespace(),
			},
		})
	}
	return requests
}

func (r *DPUSetReconciler) getDPUDeviceMap(ctx context.Context, dpuSet *provisioningv1.DPUSet) (map[string]provisioningv1.DPUDevice, error) {
	dpuDeviceMap := make(map[string]provisioningv1.DPUDevice)

	// 1. Construct the label selector if dpuNodeSelector is specified
	nodeSelector := labels.Everything()
	if dpuSet.Spec.DPUNodeSelector != nil {
		var err error
		nodeSelector, err = metav1.LabelSelectorAsSelector(dpuSet.Spec.DPUNodeSelector)
		if err != nil {
			return nil, fmt.Errorf("invalid dpuNodeSelector: %w", err)
		}
	}
	dpuNodeList := &provisioningv1.DPUNodeList{}
	if err := r.List(ctx, dpuNodeList, &client.ListOptions{
		Namespace:     dpuSet.Namespace,
		LabelSelector: nodeSelector,
	}); err != nil {
		return nil, fmt.Errorf("failed to list DPUNodes: %w", err)
	}

	// 2. Construct the selector from dpuDeviceSelector (or deprecated dpuSelector) if it is specified
	deviceSelector := labels.Everything()
	// Prefer the new field, but fall back to deprecated field for backward compatibility
	if dpuSet.Spec.DPUDeviceSelector != nil {
		var err error
		deviceSelector, err = metav1.LabelSelectorAsSelector(dpuSet.Spec.DPUDeviceSelector)
		if err != nil {
			return nil, fmt.Errorf("invalid dpuDeviceSelector: %w", err)
		}
	} else {
		//nolint:staticcheck // Intentionally using deprecated field for backward compatibility
		for key, value := range dpuSet.Spec.DPUSelector {
			req, err := labels.NewRequirement(key, selection.Equals, []string{value})
			if err != nil {
				return nil, fmt.Errorf("invalid requirement for key %s: %w", key, err)
			}
			// Add the requirement to the selector
			deviceSelector = deviceSelector.Add(*req)
		}
	}

	// 3. List DPUDevices based on the constructed label selector
	for _, node := range dpuNodeList.Items {
		// Skip nodes that are being deleted
		if !node.DeletionTimestamp.IsZero() {
			continue
		}
		selector := deviceSelector.DeepCopySelector()
		// Select DPUDevices belonging to the given DPUNode
		// DPUNodeNameLabel is required. Lack of it means many other labels are also missing, creating DPU for such DPUDevice ends up with failure
		nodeReq, err := labels.NewRequirement(provisioningv1.DPUNodeNameLabel, selection.Equals, []string{node.Name})
		if err != nil {
			return nil, fmt.Errorf("invalid requirement for key %s: %w", provisioningv1.DPUNodeNameLabel, err)
		}
		selector = selector.Add(*nodeReq)
		listOptions := client.ListOptions{
			Namespace:     dpuSet.Namespace,
			LabelSelector: selector,
		}
		dpuDeviceList := &provisioningv1.DPUDeviceList{}
		if err := r.List(ctx, dpuDeviceList, &listOptions); err != nil {
			return nil, fmt.Errorf("failed to list DPUDevices: %w", err)
		}
		// 4. Populate the device map
		for _, dpuDevice := range dpuDeviceList.Items {
			dpuDeviceMap[dpuDevice.Name] = dpuDevice
		}
	}
	return dpuDeviceMap, nil
}

func (r *DPUSetReconciler) GetDPUsMap(ctx context.Context, dpuSet *provisioningv1.DPUSet) (map[string]provisioningv1.DPU, error) {
	dpuMap := make(map[string]provisioningv1.DPU)
	dpuList := &provisioningv1.DPUList{}
	if err := r.List(ctx, dpuList, client.MatchingLabels{
		cutil.DPUSetNameLabel:      dpuSet.Name,
		cutil.DPUSetNamespaceLabel: dpuSet.Namespace,
	}); err != nil {
		return dpuMap, err
	}
	for _, dpu := range dpuList.Items {
		dpuMap[dpu.Spec.DPUDeviceName] = dpu
	}
	return dpuMap, nil
}

func (r *DPUSetReconciler) createDPU(ctx context.Context, dpuSet *provisioningv1.DPUSet, dpuDevice *provisioningv1.DPUDevice) error {
	logger := log.FromContext(ctx)
	dpuNodeName, ok := dpuDevice.Labels[provisioningv1.DPUNodeNameLabel]
	if !ok {
		return fmt.Errorf("missing label %s on DPUDevice %s", provisioningv1.DPUNodeNameLabel, dpuDevice.Name)
	}
	labels := map[string]string{
		cutil.DPUSetNameLabel:           dpuSet.Name,
		cutil.DPUSetNamespaceLabel:      dpuSet.Namespace,
		cutil.DPUDeviceNameLabel:        dpuDevice.Name,
		provisioningv1.DPUNodeNameLabel: dpuNodeName,
	}
	for k, v := range dpuSet.Labels {
		labels[k] = v
	}

	for k, v := range dpuDevice.Labels {
		labels[k] = v
	}

	owner := metav1.NewControllerRef(dpuSet, provisioningv1.GroupVersion.WithKind(provisioningv1.DPUSetKind))

	userLabels, userAnnotations, err := cutil.MergeDPUClusterNodeMetadata(dpuSet.Spec.DPUTemplate.Spec.Cluster, dpuDevice.Spec.Cluster)
	if err != nil {
		return fmt.Errorf("merge cluster metadata for DPUDevice %s: %w", dpuDevice.Name, err)
	}
	clusterNodeLabels := buildDPUClusterNodeLabels(userLabels, dpuNodeName, dpuDevice.Namespace)
	clusterSpec := provisioningv1.ClusterSpec{
		NodeLabels:      clusterNodeLabels,
		NodeAnnotations: copyStringMapIfNonEmpty(userAnnotations),
	}

	dpu := &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cutil.GenerateDPUName(dpuNodeName, dpuDevice.Name),
			Namespace:       dpuSet.Namespace,
			Labels:          labels,
			Annotations:     make(map[string]string),
			OwnerReferences: []metav1.OwnerReference{*owner},
		},
		Spec: provisioningv1.DPUSpec{
			DPUNodeName:   dpuNodeName,
			DPUDeviceName: dpuDevice.Name,
			NodeEffect:    dpuSet.Spec.DPUTemplate.Spec.NodeEffect,
			Cluster: provisioningv1.K8sCluster{
				ClusterSpec: clusterSpec,
			},
			DPUFlavor:    dpuSet.Spec.DPUTemplate.Spec.DPUFlavor,
			AstraEnabled: dpuSet.Spec.DPUTemplate.Spec.AstraEnabled,
			SecureBoot:   dpuSet.Spec.DPUTemplate.Spec.SecureBoot,
			SerialNumber: dpuDevice.Spec.SerialNumber,
			PCIAddress:   dpuDevice.Status.PCIAddress,
		},
	}

	if dpuSet.Spec.DPUTemplate.Spec.BFB != nil && dpuSet.Spec.DPUTemplate.Spec.BFB.Name != "" {
		dpu.Spec.BFB = dpuSet.Spec.DPUTemplate.Spec.BFB.Name
	}
	if dpuSet.Spec.DPUTemplate.Spec.BlueFieldSoftware != nil && dpuSet.Spec.DPUTemplate.Spec.BlueFieldSoftware.Name != "" {
		dpu.Spec.BlueFieldSoftware = dpuSet.Spec.DPUTemplate.Spec.BlueFieldSoftware.Name
	}

	if dpuSet.Spec.DPUTemplate.Spec.Cluster != nil && dpuSet.Spec.DPUTemplate.Spec.Cluster.Selector != nil {
		dpu.Spec.Cluster.Selector = dpuSet.Spec.DPUTemplate.Spec.Cluster.Selector.DeepCopy()
	}

	// do we really need this?
	for k, v := range dpuSet.Spec.DPUTemplate.Annotations {
		dpu.Annotations[k] = v
	}
	if v, ok := dpuSet.Spec.DPUTemplate.Annotations[reboot.HostPowerCycleRequireKey]; ok {
		dpu.Annotations[reboot.HostPowerCycleRequireKey] = v
	}
	// Set last applied labels and annotations on DPU
	err = cutil.SetAnnotationFromStringMap(&dpu.ObjectMeta, cutil.LastAppliedLabelsOnDPUKey, userLabels)
	if err != nil {
		return fmt.Errorf("failed to set last applied labels on DPU (%s/%s): %w", dpu.Namespace, dpu.Name, err)
	}
	err = cutil.SetAnnotationFromStringMap(&dpu.ObjectMeta, cutil.LastAppliedAnnotationsOnDPUKey, userAnnotations)
	if err != nil {
		return fmt.Errorf("failed to set last applied annotations on DPU (%s/%s): %w", dpu.Namespace, dpu.Name, err)
	}
	if err := r.Create(ctx, dpu); err != nil {
		return err
	}
	msg := fmt.Sprintf("Created DPU: (%s/%s)", dpu.Namespace, dpu.Name)
	logger.V(2).Info(msg)
	r.Recorder.Eventf(dpuSet, corev1.EventTypeNormal, events.EventSuccessfulCreateDPUReason, msg)

	return nil
}

func (r *DPUSetReconciler) updatePCIAddress(ctx context.Context, dpu *provisioningv1.DPU, dpuDevice *provisioningv1.DPUDevice) error {
	if dpuDevice.Status.PCIAddress == nil ||
		(dpu.Spec.PCIAddress != nil && *dpu.Spec.PCIAddress == *dpuDevice.Status.PCIAddress) {
		return nil
	}
	log.FromContext(ctx).Info(fmt.Sprintf("Updating PCI address of DPU(%s/%s) to %s", dpu.Namespace, dpu.Name, *dpuDevice.Status.PCIAddress))
	patcher := patch.NewSerialPatcher(dpu, r.Client)
	dpu.Spec.PCIAddress = dpuDevice.Status.PCIAddress
	return patcher.Patch(ctx, dpu)
}

func (r *DPUSetReconciler) onDelete(ctx context.Context, dpuSet *provisioningv1.DPUSet, dpuMap map[string]provisioningv1.DPU, dpuClusters []provisioningv1.DPUCluster) error {
	logger := log.FromContext(ctx)
	if dpuSet.Spec.DPUTemplate.Spec.Cluster == nil {
		return nil
	}
	for _, dpu := range dpuMap {
		if !matchDPUClusterSelector(dpuSet.Spec.DPUTemplate.Spec.Cluster.Selector, dpu.Spec.Cluster, dpuClusters) {
			if err := r.Delete(ctx, &dpu); err != nil {
				return err
			}
			msg := fmt.Sprintf("Deleted DPU: (%s/%s) for DPUNode %s", dpu.Namespace, dpu.Name, dpu.Spec.DPUNodeName)
			logger.Info(msg)
			r.Recorder.Eventf(dpuSet, corev1.EventTypeNormal, events.EventSuccessfulDeleteDPUReason, msg)
		}
	}
	return nil
}

func (r *DPUSetReconciler) rolloutRolling(ctx context.Context, dpuSet *provisioningv1.DPUSet,
	dpuMap map[string]provisioningv1.DPU, total int, dpuClusters []provisioningv1.DPUCluster) error {
	var maxUnavailable *intstr.IntOrString
	//nolint:staticcheck // SA1019: MaxUnavailable is deprecated but still supported
	if dpuSet.Spec.Strategy.RollingUpdate != nil {
		maxUnavailable = dpuSet.Spec.Strategy.RollingUpdate.MaxUnavailable
	}
	scaledValue, err := intstr.GetScaledValueFromIntOrPercent(intstr.ValueOrDefault(
		maxUnavailable, intstr.FromInt(0)), total, true)
	if err != nil {
		return err
	}

	if scaledValue <= 0 {
		scaledValue = 1
	} else if scaledValue > total {
		scaledValue = total
	}

	// The DPUs that have deleted should be considered as unavailable DPUs
	unavaiable := total - len(dpuMap)
	for _, dpu := range dpuMap {
		if isUnavailable(&dpu) {
			// A DPU which is not ready should be considered an unavailable DPU. Skip this one
			unavaiable++
		}
	}

	for _, dpu := range dpuMap {
		if disrupted := r.needDisruptDPU(*dpuSet, dpu, dpuClusters); !disrupted {
			continue
		}

		failedMsg := fmt.Sprintf("Failed to Delete DPU: (%s/%s) from DPUNode %s", dpu.Namespace, dpu.Name, dpu.Spec.DPUNodeName)
		successMsg := fmt.Sprintf("Delete DPU: (%s/%s) from DPUNode %s", dpu.Namespace, dpu.Name, dpu.Spec.DPUNodeName)
		if isUnavailable(&dpu) {
			if err := r.Delete(ctx, &dpu); err != nil {
				r.Recorder.Eventf(dpuSet, corev1.EventTypeNormal, events.EventFailedDeleteDPUReason, failedMsg)
				return err
			}
			r.Recorder.Eventf(dpuSet, corev1.EventTypeNormal, events.EventSuccessfulDeleteDPUReason, successMsg)
		} else if unavaiable < scaledValue {
			if err := r.Delete(ctx, &dpu); err != nil {
				r.Recorder.Eventf(dpuSet, corev1.EventTypeNormal, events.EventFailedDeleteDPUReason, failedMsg)
				return err
			}
			r.Recorder.Eventf(dpuSet, corev1.EventTypeNormal, events.EventSuccessfulDeleteDPUReason, successMsg)
			unavaiable++
		}
	}
	return nil
}

func isUnavailable(dpu *provisioningv1.DPU) bool {
	_, cond := cutil.GetDPUCondition(&dpu.Status, provisioningv1.DPUCondReady.String())
	return cond == nil || cond.Status == metav1.ConditionFalse || !dpu.DeletionTimestamp.IsZero()
}

// needDisruptDPU reports whether the DPU's spec has diverged from the owning
// DPUSet's DPUTemplate in any way that requires recreating the DPU (used by
// rolloutRolling). It is a thin wrapper over computeDPUDrift, which both
// callers use to evaluate field-level drift in a single place.
func (r *DPUSetReconciler) needDisruptDPU(dpuSet provisioningv1.DPUSet, dpu provisioningv1.DPU, dpuClusters []provisioningv1.DPUCluster) bool {
	return !r.computeDPUDrift(dpuSet, dpu, dpuClusters).empty()
}

func (r *DPUSetReconciler) collectDPUStatistics(dpuMap map[string]provisioningv1.DPU) map[provisioningv1.DPUPhase]int {
	dpuStatistics := make(map[provisioningv1.DPUPhase]int)
	for _, dpu := range dpuMap {
		// Skip DPUs that are being deleted
		if !dpu.DeletionTimestamp.IsZero() {
			continue
		}
		switch dpu.Status.Phase {
		case "":
			dpuStatistics[provisioningv1.DPUInitializing]++
		default:
			dpuStatistics[dpu.Status.Phase]++
		}
	}
	return dpuStatistics
}

func (r *DPUSetReconciler) UpdateDPUSetStatus(ctx context.Context, dpuSet *provisioningv1.DPUSet, dpusChanged bool, totalSelectedDPUs int, dpusOwnedByOtherDPUSet int) error {
	dpuMap, err := r.GetDPUsMap(ctx, dpuSet)
	if err != nil {
		return fmt.Errorf("failed to get DPU map from cache: %w", err)
	}

	dpuStatistics := r.collectDPUStatistics(dpuMap)
	if !reflect.DeepEqual(dpuStatistics, dpuSet.Status.DPUStatistics) {
		dpuSet.Status.DPUStatistics = dpuStatistics
	}

	if dpusChanged {
		// DPUs were just created, deleted, or updated - they won't be ready until they reconcile
		// The DPU watch will trigger a new reconcile when they update their status
		conditions.AddFalse(dpuSet, conditions.TypeReady, conditions.ReasonPending, "DPUs are being reconciled")
		return nil
	}

	// Check readiness conditions
	dpuSetReady := true
	for _, dpu := range dpuMap {
		// Skip DPUs that are being deleted - they shouldn't block DPUSet readiness
		if !dpu.DeletionTimestamp.IsZero() {
			continue
		}

		if dpu.Status.ObservedGeneration != dpu.Generation {
			dpuSetReady = false
		}

		// Simply comparing the dpu metadata generation and observed generation is not sufficient, because if the DPU is not updated in the dpuset controller cache at this time,
		// the DPU observed generation will be the same as the generation, but the DPU is stale.
		// So we introduce the dpu template spec hash to the label of the DPU, and compare it with the dpu set template spec hash, to ensure the DPU is up to date.
		if dpu.Status.Phase != provisioningv1.DPUReady {
			dpuSetReady = false
		}
		if hash, exists := dpu.GetLabels()[cutil.DPUSetDPUTemplateSpecHashLabelKey]; exists && hash != dpuSet.GetLabels()[cutil.DPUSetDPUTemplateSpecHashLabelKey] {
			dpuSetReady = false
		}
	}

	switch {
	case dpuSetReady && dpusOwnedByOtherDPUSet == 0:
		conditions.AddTrue(dpuSet, conditions.TypeReady)
	case dpusOwnedByOtherDPUSet > 0:
		dpusOwnedMsg := fmt.Sprintf("%d/%d DPU(s) cannot be provisioned, owned by another DPUSet. To identify the conflicting DPUs, filter the DPUs by the label: %s",
			dpusOwnedByOtherDPUSet, totalSelectedDPUs, cutil.GenerateDPUSetCollisionLabelKey(dpuSet.Name))
		conditions.AddFalse(dpuSet, conditions.TypeReady, conditions.ReasonPending,
			conditions.ConditionMessage(dpusOwnedMsg))
	default:
		conditions.AddFalse(dpuSet, conditions.TypeReady, conditions.ReasonPending, "Some DPUs are not ready")
	}

	return nil
}

func (r *DPUSetReconciler) createMissingDPUs(ctx context.Context, dpuSet *provisioningv1.DPUSet, dpuDeviceMap map[string]provisioningv1.DPUDevice, dpuMap map[string]provisioningv1.DPU) (bool, int, error) {
	dpusCreated := false
	dpusOwnedByOtherDPUSet := 0
	logger := log.FromContext(ctx)
	for dpuDeviceName, dpuDevice := range dpuDeviceMap {
		var err error
		if dpu, exists := dpuMap[dpuDeviceName]; exists {
			err = r.updatePCIAddress(ctx, &dpu, &dpuDevice)
		} else {
			if dpuSet.IsAstraEnabledForNonBlueField4(dpuDevice) {
				msg := fmt.Sprintf("Skipping DPU creation for DPUDevice (%s/%s): astraEnabled requires DPUType=BlueField4, current DPUType=%s",
					dpuDevice.Namespace, dpuDevice.Name, dpuDevice.Status.DPUType)
				logger.Info(msg)
				r.Recorder.Eventf(dpuSet, corev1.EventTypeWarning, events.EventFailedCreateDPUReason, msg)
				continue
			}
			err = r.createDPU(ctx, dpuSet, &dpuDevice)
			if err == nil {
				dpusCreated = true
			} else if apierrors.IsAlreadyExists(err) {
				owned, labelErr := r.isDPUOwnedByOtherDPUSet(ctx, dpuSet, &dpuDevice)
				if labelErr != nil {
					return dpusCreated, dpusOwnedByOtherDPUSet, labelErr
				}
				if owned {
					dpusOwnedByOtherDPUSet++
				}
				continue
			}
		}
		if err != nil {
			return dpusCreated, dpusOwnedByOtherDPUSet, err
		}
	}
	return dpusCreated, dpusOwnedByOtherDPUSet, nil
}

func (r *DPUSetReconciler) isDPUOwnedByOtherDPUSet(ctx context.Context, dpuSet *provisioningv1.DPUSet, dpuDevice *provisioningv1.DPUDevice) (bool, error) {
	logger := log.FromContext(ctx)
	existingDPU := &provisioningv1.DPU{}
	dpuName := cutil.GenerateDPUName(dpuDevice.Labels[provisioningv1.DPUNodeNameLabel], dpuDevice.Name)
	if err := r.Get(ctx, types.NamespacedName{Name: dpuName, Namespace: dpuSet.Namespace}, existingDPU); err != nil {
		return false, nil
	}
	ownerDPUSetName := existingDPU.Labels[cutil.DPUSetNameLabel]
	if ownerDPUSetName == dpuSet.Name {
		return false, nil
	}

	if err := r.addCollisionLabel(ctx, existingDPU, dpuSet.Name); err != nil {
		return false, fmt.Errorf("failed to add collision label to DPU %s/%s: %w", existingDPU.Namespace, existingDPU.Name, err)
	}

	if ownerDPUSetName == "" {
		logger.Info(fmt.Sprintf("DPU %s/%s exists but has no owner label - cannot be managed by this DPUSet",
			existingDPU.Namespace, existingDPU.Name))
	} else {
		logger.Info(fmt.Sprintf("DPU %s/%s already owned by DPUSet %s",
			existingDPU.Namespace, existingDPU.Name, ownerDPUSetName))
	}
	return true, nil
}

// addCollisionLabel adds the dpuset-collision label to a DPU to indicate that
// another DPUSet attempted to claim it.
func (r *DPUSetReconciler) addCollisionLabel(ctx context.Context, dpu *provisioningv1.DPU, dpuSetName string) error {
	collisionKey := cutil.GenerateDPUSetCollisionLabelKey(dpuSetName)
	if dpu.Labels[collisionKey] == "true" {
		return nil
	}
	patcher := patch.NewSerialPatcher(dpu, r.Client)
	if dpu.Labels == nil {
		dpu.Labels = make(map[string]string)
	}
	dpu.Labels[collisionKey] = "true"
	return patcher.Patch(ctx, dpu)
}

// removeCollisionLabel removes the given collision label key from a DPU.
func (r *DPUSetReconciler) removeCollisionLabel(ctx context.Context, dpu *provisioningv1.DPU, collisionKey string) error {
	if _, exists := dpu.Labels[collisionKey]; !exists {
		return nil
	}
	patcher := patch.NewSerialPatcher(dpu, r.Client)
	delete(dpu.Labels, collisionKey)
	return patcher.Patch(ctx, dpu)
}

// removeStaleCollisionLabels removes this DPUSet's collision label from DPUs whose
// DPUDevice is no longer targeted by this DPUSet (e.g. selector changed).
func (r *DPUSetReconciler) removeStaleCollisionLabels(ctx context.Context, dpuSet *provisioningv1.DPUSet, dpuDeviceMap map[string]provisioningv1.DPUDevice) error {
	collisionKey := cutil.GenerateDPUSetCollisionLabelKey(dpuSet.Name)
	dpuList := &provisioningv1.DPUList{}
	if err := r.List(ctx, dpuList, client.InNamespace(dpuSet.Namespace), client.MatchingLabels{
		collisionKey: "true",
	}); err != nil {
		return fmt.Errorf("failed to list DPUs with collision label for DPUSet %s: %w", dpuSet.Name, err)
	}
	for i := range dpuList.Items {
		dpu := &dpuList.Items[i]
		if _, targeted := dpuDeviceMap[dpu.Spec.DPUDeviceName]; !targeted {
			if err := r.removeCollisionLabel(ctx, dpu, collisionKey); err != nil {
				return fmt.Errorf("failed to remove collision label from DPU %s: %w", dpu.Name, err)
			}
		}
	}
	return nil
}

func (r *DPUSetReconciler) deleteStaleDPUs(ctx context.Context, dpuSet *provisioningv1.DPUSet, dpuDeviceMap map[string]provisioningv1.DPUDevice, dpuMap map[string]provisioningv1.DPU) error {
	logger := log.FromContext(ctx)
	for dpuDeviceName, dpu := range dpuMap {
		if _, exists := dpuDeviceMap[dpuDeviceName]; !exists {
			logger.Info(fmt.Sprintf("Deleting DPU: (%s/%s) for DPUNode %s", dpu.Namespace, dpu.Name, dpu.Spec.DPUNodeName))
			if err := r.Delete(ctx, &dpu); err != nil {
				return fmt.Errorf("failed to delete DPU: (%s/%s) for DPUNode %s: %w", dpu.Namespace, dpu.Name, dpu.Spec.DPUNodeName, err)
			}
			msg := fmt.Sprintf("Deleted DPU: (%s/%s) for DPUNode %s", dpu.Namespace, dpu.Name, dpu.Spec.DPUNodeName)
			logger.V(2).Info(msg)
			r.Recorder.Eventf(dpuSet, corev1.EventTypeNormal, events.EventSuccessfulDeleteDPUReason, msg)
		}
	}
	return nil
}

func (r *DPUSetReconciler) validateDPUClusterMetadata(dpuSet *provisioningv1.DPUSet, dpuDeviceMap map[string]provisioningv1.DPUDevice) error {
	for name, dd := range dpuDeviceMap {
		if _, _, err := cutil.MergeDPUClusterNodeMetadata(dpuSet.Spec.DPUTemplate.Spec.Cluster, dd.Spec.Cluster); err != nil {
			return fmt.Errorf("DPUDevice %q: %w", name, err)
		}
	}
	return nil
}

// UpdateDPUs updates the DPUs in the DPUSet.
// in this function, it will:
// 1. update merged cluster node labels and annotations on DPUs (template + DPUDevice)
// 2. update the NodeEffect Action fields for DPUs
// 3. update the ApplyOnLabelChange field for DPUs
// 4. update the NodeMaintenanceAdditionalRequestors field for DPUs
// Returns true if any DPUs were updated, false otherwise.
func (r *DPUSetReconciler) UpdateDPUs(ctx context.Context, dpuSet *provisioningv1.DPUSet, dpuMap map[string]provisioningv1.DPU, dpuDeviceMap map[string]provisioningv1.DPUDevice) (bool, error) {
	anyUpdated := false
	for i := range dpuMap {
		dpu := dpuMap[i]
		dpuDevice, ok := dpuDeviceMap[dpu.Spec.DPUDeviceName]
		if !ok {
			continue
		}
		userLabels, userAnn, mergeErr := cutil.MergeDPUClusterNodeMetadata(dpuSet.Spec.DPUTemplate.Spec.Cluster, dpuDevice.Spec.Cluster)
		if mergeErr != nil {
			return false, mergeErr
		}
		update := false
		patcher := patch.NewSerialPatcher(&dpu, r.Client)
		// 1. update the merged cluster node labels and annotations for DPU
		if updateLabelsAndAnnotationsForDPU(ctx, &dpu, dpuSet, userLabels, userAnn) {
			update = true
		}
		// 2. update the NodeEffect Action fields for DPU
		if updateNodeEffectAction(ctx, dpuSet, &dpu) {
			update = true
		}
		// 3. update the ApplyOnLabelChange field for DPU
		if updateNodeEffectApplyOnLabelChange(ctx, dpuSet, &dpu) {
			update = true
		}
		// 4. update the NodeMaintenanceAdditionalRequestors field for DPU
		expectedRequestors := dpuSet.Spec.DPUTemplate.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors
		if updateNodeMaintenanceAdditionalRequestors(ctx, &dpu, expectedRequestors) {
			update = true
		}

		if update {
			dpu.GetLabels()[cutil.DPUSetDPUTemplateSpecHashLabelKey] = dpuSet.GetLabels()[cutil.DPUSetDPUTemplateSpecHashLabelKey]
			if err := patcher.Patch(ctx, &dpu); err != nil {
				return false, fmt.Errorf("failed to patch DPU (%s/%s): %w", dpu.Namespace, dpu.Name, err)
			}
			anyUpdated = true
		}
	}

	return anyUpdated, nil
}

func buildDPUClusterNodeLabels(userLabels map[string]string, dpuNodeName, dpuDeviceNamespace string) map[string]string {
	out := make(map[string]string)
	for k, v := range userLabels {
		out[k] = v
	}
	out[provisioningv1.DPUNodeNameLabel] = dpuNodeName
	out[provisioningv1.DPUNodeNamespaceLabel] = dpuDeviceNamespace
	out[release.DPFVersionLabelKey] = release.DPFVersion()
	return out
}

func copyStringMapIfNonEmpty(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func normalizeStringMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func isNodeLabelUpdateNeeded(dpu *provisioningv1.DPU, mergedUserLabels map[string]string) (needUpdate bool, newLabels map[string]string, removedLabels []string) {
	newLabels = normalizeStringMap(mergedUserLabels)
	lastLabels := cutil.GetStringMapFromAnnotation(dpu.Annotations, cutil.LastAppliedLabelsOnDPUKey)
	if reflect.DeepEqual(newLabels, lastLabels) {
		return false, nil, nil
	}
	return true, newLabels, cutil.GetRemovedLabels(lastLabels, newLabels)
}

func isNodeAnnotationUpdateNeeded(dpu *provisioningv1.DPU, mergedUserAnnotations map[string]string) (needUpdate bool, newAnnotations map[string]string, removedAnnotations []string) {
	newAnnotations = normalizeStringMap(mergedUserAnnotations)
	lastAnnotations := cutil.GetStringMapFromAnnotation(dpu.Annotations, cutil.LastAppliedAnnotationsOnDPUKey)
	if reflect.DeepEqual(newAnnotations, lastAnnotations) {
		return false, nil, nil
	}
	return true, newAnnotations, cutil.GetRemovedLabels(lastAnnotations, newAnnotations)
}

func updateNodeLabelsForDPU(ctx context.Context, dpu *provisioningv1.DPU, newLabels map[string]string, removedLabels []string) {
	logger := log.FromContext(ctx)
	if dpu.Spec.Cluster.NodeLabels == nil {
		dpu.Spec.Cluster.NodeLabels = make(map[string]string)
	}
	for k, v := range newLabels {
		dpu.Spec.Cluster.NodeLabels[k] = v
	}
	for _, k := range removedLabels {
		delete(dpu.Spec.Cluster.NodeLabels, k)
	}
	err := cutil.SetAnnotationFromStringMap(&dpu.ObjectMeta, cutil.LastAppliedLabelsOnDPUKey, newLabels)
	if err != nil {
		logger.V(3).Info(fmt.Sprintf("failed to set last applied labels on DPU (%s/%s): %v", dpu.Namespace, dpu.Name, err))
	}
}

func updateNodeAnnotationsForDPU(ctx context.Context, dpu *provisioningv1.DPU, newAnnotations map[string]string, removedAnnotations []string) {
	logger := log.FromContext(ctx)
	if dpu.Spec.Cluster.NodeAnnotations == nil {
		dpu.Spec.Cluster.NodeAnnotations = make(map[string]string)
	}
	for k, v := range newAnnotations {
		dpu.Spec.Cluster.NodeAnnotations[k] = v
	}
	for _, k := range removedAnnotations {
		delete(dpu.Spec.Cluster.NodeAnnotations, k)
	}
	if len(dpu.Spec.Cluster.NodeAnnotations) == 0 {
		dpu.Spec.Cluster.NodeAnnotations = nil
	}
	err := cutil.SetAnnotationFromStringMap(&dpu.ObjectMeta, cutil.LastAppliedAnnotationsOnDPUKey, newAnnotations)
	if err != nil {
		logger.V(3).Info(fmt.Sprintf("failed to set last applied annotations on DPU (%s/%s): %v", dpu.Namespace, dpu.Name, err))
	}
}

func updateLabelsAndAnnotationsForDPU(ctx context.Context, dpu *provisioningv1.DPU, dpuSet *provisioningv1.DPUSet, mergedUserLabels, mergedUserAnnotations map[string]string) bool {
	needUpdateLabels, newLabels, removedLabels := isNodeLabelUpdateNeeded(dpu, mergedUserLabels)
	needUpdateAnnotations, newAnnotations, removedAnnotations := isNodeAnnotationUpdateNeeded(dpu, mergedUserAnnotations)

	changed := false
	if needUpdateLabels {
		updateNodeLabelsForDPU(ctx, dpu, newLabels, removedLabels)
		changed = true
	}
	if needUpdateAnnotations {
		updateNodeAnnotationsForDPU(ctx, dpu, newAnnotations, removedAnnotations)
		changed = true
	}
	if dpuSet.Spec.DPUTemplate.Spec.Cluster != nil && dpuSet.Spec.DPUTemplate.Spec.Cluster.Selector != nil {
		sel := dpuSet.Spec.DPUTemplate.Spec.Cluster.Selector.DeepCopy()
		if !reflect.DeepEqual(dpu.Spec.Cluster.Selector, sel) {
			dpu.Spec.Cluster.Selector = sel
			changed = true
		}
	} else if dpu.Spec.Cluster.Selector != nil {
		dpu.Spec.Cluster.Selector = nil
		changed = true
	}
	return changed
}

// updateNodeMaintenanceAdditionalRequestors updates the NodeMaintenanceAdditionalRequestors field for existing DPUs when the DPUSet template changes.
// This function ensures that changes to the NodeMaintenanceAdditionalRequestors field are propagated to existing DPUs without requiring recreation.
func updateNodeMaintenanceAdditionalRequestors(ctx context.Context, dpu *provisioningv1.DPU, expectedRequestors []string) bool {
	logger := log.FromContext(ctx)
	sort.Strings(expectedRequestors)
	currentRequestors := dpu.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors

	sort.Strings(currentRequestors)

	if !slices.Equal(currentRequestors, expectedRequestors) {
		dpu.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors = expectedRequestors
		logger.V(3).Info(fmt.Sprintf("Updating NodeMaintenanceAdditionalRequestors: %v for DPU (%s/%s)", expectedRequestors, dpu.Namespace, dpu.Name))
		return true
	}

	return false
}

// updateNodeEffectAction updates the NodeEffect Action fields (Taint, Drain, NoEffect, etc.) for existing DPUs when the DPUSet template changes.
// This function ensures that changes to the Action fields are propagated to existing DPUs without requiring recreation.
func updateNodeEffectAction(ctx context.Context, dpuSet *provisioningv1.DPUSet, dpu *provisioningv1.DPU) bool {
	logger := log.FromContext(ctx)

	if dpu.Status.Phase != provisioningv1.DPUReady {
		return false
	}

	// Get the expected Action from the DPUSet template
	expectedAction := dpuSet.Spec.DPUTemplate.Spec.NodeEffect.Action

	// Get current Action from DPU
	currentAction := dpu.Spec.NodeEffect.Action

	// Check if Action has changed
	if !reflect.DeepEqual(currentAction, expectedAction) {
		logger.V(3).Info(fmt.Sprintf("Updating NodeEffect Action for DPU (%s/%s)", dpu.Namespace, dpu.Name))
		dpu.Spec.NodeEffect.Action = expectedAction
		return true
	}

	return false
}

// updateNodeEffectApplyOnLabelChange updates the ApplyOnLabelChange field for existing DPUs when the DPUSet template changes.
// This function ensures that changes to the ApplyOnLabelChange field are propagated to existing DPUs without requiring recreation.
func updateNodeEffectApplyOnLabelChange(ctx context.Context, dpuSet *provisioningv1.DPUSet, dpu *provisioningv1.DPU) bool {
	logger := log.FromContext(ctx)

	// Get the expected ApplyOnLabelChange value from the DPUSet template
	expectedApplyOnLabelChange := dpuSet.Spec.DPUTemplate.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange

	// Update ApplyOnLabelChange for existing DPUs if it has changed

	// Get current ApplyOnLabelChange value from DPU
	currentApplyOnLabelChange := dpu.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange

	// Check if ApplyOnLabelChange has changed
	if !reflect.DeepEqual(currentApplyOnLabelChange, expectedApplyOnLabelChange) {
		logger.V(3).Info(fmt.Sprintf("Updating ApplyOnLabelChange for DPU (%s/%s)", dpu.Namespace, dpu.Name))
		dpu.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange = expectedApplyOnLabelChange
		return true
	}

	return false
}

// calculateDPUTemplateSpecDigest calculates the digest of the DPU template spec.
func calculateDPUTemplateSpecDigest(spec *provisioningv1.DPUTemplateSpec) string {
	config := spec.DeepCopy()
	return digest.Short(digest.FromObjects(config), 10)
}

// matchDPUClusterSelector checks if the DPU cluster selector updated and matches the current cluster.
func matchDPUClusterSelector(selectorFromDPUSet *metav1.LabelSelector, dpuK8sCluster provisioningv1.K8sCluster, clusterList []provisioningv1.DPUCluster) bool {
	// if the DPU is not assigned to a cluster, we consider it as a match.
	if dpuK8sCluster.Name == "" || dpuK8sCluster.Namespace == "" {
		return true
	}

	if selector, err := utils.LabelSelectorAsSelector(selectorFromDPUSet); err == nil {
		for _, cluster := range clusterList {
			if selector.Matches(labels.Set(cluster.Labels)) {
				if dpuK8sCluster.Name == cluster.Name && dpuK8sCluster.Namespace == cluster.Namespace {
					return true
				}
			}
		}
	}
	return false
}
