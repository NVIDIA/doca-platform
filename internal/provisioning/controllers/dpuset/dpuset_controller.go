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
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"sort"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/events"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/reboot"
	"github.com/nvidia/doca-platform/internal/release"

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

// DPUSetReconciler reconciles a DPUSet object
type DPUSetReconciler struct {
	client.Client
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
		); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	if !dpuSet.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.reconcileDelete(ctx, dpuSet)
	}

	return r.Handle(ctx, dpuSet)
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

	controllerutil.RemoveFinalizer(dpuSet, provisioningv1.DPUSetFinalizer)

	return nil
}

func (r *DPUSetReconciler) Handle(ctx context.Context, dpuSet *provisioningv1.DPUSet) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

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

	// Get dpu map which are owned by dpuset
	dpuMap, err := r.getDPUsMap(ctx, dpuSet)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get DPU map %w", err)
	}

	if err := r.createMissingDPUs(ctx, dpuSet, dpuDeviceMap, dpuMap); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.deleteStaleDPUs(ctx, dpuSet, dpuDeviceMap, dpuMap); err != nil {
		return ctrl.Result{}, err
	}

	// handle rolling update
	dpuMap, err = r.getDPUsMap(ctx, dpuSet)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get DPUs %w", err)
	}

	switch dpuSet.Spec.Strategy.Type {
	case provisioningv1.OnDeleteStrategyType:
		// do nothing, waiting for user delete DPU object manually.
	case provisioningv1.RollingUpdateStrategyType:
		if err := r.rolloutRolling(ctx, dpuSet, dpuMap, len(dpuDeviceMap)); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to rollout DPU %w", err)
		}
	}

	if err := r.updateDPUs(ctx, dpuSet, dpuMap); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update node effect ApplyOnLabelChange for DPUs %w", err)
	}

	updateDPUSetStatus(dpuSet, dpuMap)

	return ctrl.Result{}, nil
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

	// 2. Construct the selector from dpuSelector if it is specified
	deviceSelector := labels.Everything()
	for key, value := range dpuSet.Spec.DPUSelector {
		req, err := labels.NewRequirement(key, selection.Equals, []string{value})
		if err != nil {
			return nil, fmt.Errorf("invalid requirement for key %s: %w", key, err)
		}
		// Add the requirement to the selector
		deviceSelector = deviceSelector.Add(*req)
	}

	// 3. List DPUDevices based on the constructed label selector
	for _, node := range dpuNodeList.Items {
		selector := deviceSelector.DeepCopySelector()
		// Select DPUDevices belonging to the given DPUNode
		// DPUNodeNameLabel is required. Lack of it means many other labels are also missing, creating DPU for such DPUDevice ends up with failure
		nodeReq, err := labels.NewRequirement(cutil.DPUNodeNameLabel, selection.Equals, []string{node.Name})
		if err != nil {
			return nil, fmt.Errorf("invalid requirement for key %s: %w", cutil.DPUNodeNameLabel, err)
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

func (r *DPUSetReconciler) getDPUsMap(ctx context.Context, dpuSet *provisioningv1.DPUSet) (map[string]provisioningv1.DPU, error) {
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
	dpuNodeName, ok := dpuDevice.Labels[cutil.DPUNodeNameLabel]
	if !ok {
		return fmt.Errorf("missing label %s on DPUDevice %s", cutil.DPUNodeNameLabel, dpuDevice.Name)
	}
	labels := map[string]string{
		cutil.DPUSetNameLabel:      dpuSet.Name,
		cutil.DPUSetNamespaceLabel: dpuSet.Namespace,
		cutil.DPUDeviceNameLabel:   dpuDevice.Name,
		cutil.DPUNodeNameLabel:     dpuNodeName,
	}
	for k, v := range dpuSet.Labels {
		labels[k] = v
	}

	for k, v := range dpuDevice.Labels {
		labels[k] = v
	}

	owner := metav1.NewControllerRef(dpuSet, provisioningv1.GroupVersion.WithKind(provisioningv1.DPUSetKind))

	clusterNodeLabels := map[string]string{}
	if dpuSet.Spec.DPUTemplate.Spec.Cluster != nil && dpuSet.Spec.DPUTemplate.Spec.Cluster.NodeLabels != nil {
		for k, v := range dpuSet.Spec.DPUTemplate.Spec.Cluster.NodeLabels {
			clusterNodeLabels[k] = v
		}
	}
	clusterNodeLabels[cutil.HostNameDPULabelKey] = dpuNodeName
	clusterNodeLabels[release.DPFVersionLabelKey] = release.DPFVersion()

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
			BFB:           dpuSet.Spec.DPUTemplate.Spec.BFB.Name,
			NodeEffect:    dpuSet.Spec.DPUTemplate.Spec.NodeEffect,
			Cluster: provisioningv1.K8sCluster{
				NodeLabels: clusterNodeLabels,
			},
			DPUFlavor:    dpuSet.Spec.DPUTemplate.Spec.DPUFlavor,
			SerialNumber: dpuDevice.Spec.SerialNumber,
			PCIAddress:   dpuDevice.Status.PCIAddress,
			// TODO(klausm): deprecated field, it'll be removed later.
			// BMCIP:        "",
		},
	}

	// do we really need this?
	for k, v := range dpuSet.Spec.DPUTemplate.Annotations {
		dpu.Annotations[k] = v
	}
	if v, ok := dpuSet.Spec.DPUTemplate.Annotations[reboot.HostPowerCycleRequireKey]; ok {
		dpu.Annotations[reboot.HostPowerCycleRequireKey] = v
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

func (r *DPUSetReconciler) rolloutRolling(ctx context.Context, dpuSet *provisioningv1.DPUSet,
	dpuMap map[string]provisioningv1.DPU, total int) error {
	scaledValue, err := intstr.GetScaledValueFromIntOrPercent(intstr.ValueOrDefault(
		dpuSet.Spec.Strategy.RollingUpdate.MaxUnavailable, intstr.FromInt(0)), total, true)
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
		if disrupted := r.needDisruptDPU(*dpuSet, dpu); !disrupted {
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

// TODO: check more informations
// needDisruptDPU is used to check if the DPU needs to be disrupted.
func (r *DPUSetReconciler) needDisruptDPU(dpuSet provisioningv1.DPUSet, dpu provisioningv1.DPU) bool {
	if (dpu.Spec.BFB != dpuSet.Spec.DPUTemplate.Spec.BFB.Name) || (dpu.Spec.DPUFlavor != dpuSet.Spec.DPUTemplate.Spec.DPUFlavor) {
		return true
	}

	return false
}

func updateDPUSetStatus(dpuSet *provisioningv1.DPUSet,
	dpuMap map[string]provisioningv1.DPU) {
	dpuStatistics := make(map[provisioningv1.DPUPhase]int)
	for _, dpu := range dpuMap {
		switch dpu.Status.Phase {
		case "":
			dpuStatistics[provisioningv1.DPUInitializing]++
		default:
			dpuStatistics[dpu.Status.Phase]++
		}
	}
	if reflect.DeepEqual(dpuStatistics, dpuSet.Status.DPUStatistics) {
		return
	}
	dpuSet.Status.DPUStatistics = dpuStatistics
}

func (r *DPUSetReconciler) createMissingDPUs(ctx context.Context, dpuSet *provisioningv1.DPUSet, dpuDeviceMap map[string]provisioningv1.DPUDevice, dpuMap map[string]provisioningv1.DPU) error {
	for dpuDeviceName, dpuDevice := range dpuDeviceMap {
		var err error
		if dpu, exists := dpuMap[dpuDeviceName]; exists {
			err = r.updatePCIAddress(ctx, &dpu, &dpuDevice)
		} else {
			err = r.createDPU(ctx, dpuSet, &dpuDevice)
		}
		if err != nil {
			return err
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

// check if the node labels for DPU need to be updated
func (r *DPUSetReconciler) isNodeLabelUpdateNeeded(ctx context.Context, dpuSet *provisioningv1.DPUSet) (bool, map[string]string, []string) {
	logger := log.FromContext(ctx)
	if dpuSet.Spec.DPUTemplate.Spec.Cluster == nil {
		return false, nil, nil
	}

	if dpuSet.Spec.DPUTemplate.Spec.Cluster.NodeLabels == nil {
		return false, nil, nil
	}

	newLabels := dpuSet.Spec.DPUTemplate.Spec.Cluster.NodeLabels
	oldLabels := make(map[string]string)
	if dpuSet.Annotations != nil {
		if lastAppliedLabelsStr, ok := dpuSet.Annotations[cutil.LastAppliedLabelsOnDPUKey]; ok {
			if err := json.Unmarshal([]byte(lastAppliedLabelsStr), &oldLabels); err != nil {
				logger.Error(err, "Failed to unmarshal last applied labels")
				return false, nil, nil
			}
		}
	} else {
		dpuSet.Annotations = make(map[string]string)
		if jsonStr, err := cutil.MarshalJSON(newLabels); err != nil {
			logger.Error(err, "Failed to marshal new labels")
			return false, nil, nil
		} else {
			dpuSet.Annotations[cutil.LastAppliedLabelsOnDPUKey] = jsonStr
		}
	}

	if !reflect.DeepEqual(newLabels, oldLabels) {
		removedLabels := cutil.GetRemovedLabels(oldLabels, newLabels)
		return true, newLabels, removedLabels
	}
	return false, nil, nil
}

// in this function, it will:
// 1. update the node labels for DPUs
// 2. update the NodeEffect Action fields for DPUs
// 3. update the ApplyOnLabelChange field for DPUs
// 4. update the NodeMaintenanceAdditionalRequestors field for DPUs
func (r *DPUSetReconciler) updateDPUs(ctx context.Context, dpuSet *provisioningv1.DPUSet, dpuMap map[string]provisioningv1.DPU) error {
	needUpdateLabels, newLabels, removedLabels := r.isNodeLabelUpdateNeeded(ctx, dpuSet)
	if needUpdateLabels {
		// Update the last applied labels annotation
		if jsonStr, err := cutil.MarshalJSON(newLabels); err != nil {
			return fmt.Errorf("failed to marshal new labels: %w", err)
		} else {
			dpuSet.Annotations[cutil.LastAppliedLabelsOnDPUKey] = jsonStr
		}
	}
	for i := range dpuMap {
		dpu := dpuMap[i] // copy into new variable
		update := false
		patcher := patch.NewSerialPatcher(&dpu, r.Client)
		// 1. update node labels for DPU
		if needUpdateLabels {
			updateNodeLabelsForDPU(&dpu, newLabels, removedLabels)
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
		expectedRequestors := []string{}
		if dpuSet.Spec.DPUTemplate.Spec.NodeEffect != nil {
			expectedRequestors = dpuSet.Spec.DPUTemplate.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors
		}
		if updateNodeMaintenanceAdditionalRequestors(ctx, &dpu, expectedRequestors) {
			update = true
		}

		if update {
			if err := patcher.Patch(ctx, &dpu); err != nil {
				return fmt.Errorf("failed to patch DPU (%s/%s): %w", dpu.Namespace, dpu.Name, err)
			}
		}
	}

	return nil
}

// updateNodeMaintenanceAdditionalRequestors updates the NodeMaintenanceAdditionalRequestors field for existing DPUs when the DPUSet template changes.
// This function ensures that changes to the NodeMaintenanceAdditionalRequestors field are propagated to existing DPUs without requiring recreation.
func updateNodeMaintenanceAdditionalRequestors(ctx context.Context, dpu *provisioningv1.DPU, expectedRequestors []string) bool {
	logger := log.FromContext(ctx)
	sort.Strings(expectedRequestors)
	var currentRequestors []string
	if dpu.Spec.NodeEffect != nil {
		currentRequestors = dpu.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors
	} else {
		dpu.Spec.NodeEffect = &provisioningv1.NodeEffect{}
	}

	sort.Strings(currentRequestors)

	if !slices.Equal(currentRequestors, expectedRequestors) {
		dpu.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors = expectedRequestors
		logger.V(3).Info(fmt.Sprintf("Updating NodeMaintenanceAdditionalRequestors: %v for DPU (%s/%s)", expectedRequestors, dpu.Namespace, dpu.Name))
		return true
	}

	return false
}

// updates the node labels for DPU
func updateNodeLabelsForDPU(dpu *provisioningv1.DPU, newLabels map[string]string, removedLabels []string) {
	if dpu.Spec.Cluster.NodeLabels == nil {
		dpu.Spec.Cluster.NodeLabels = make(map[string]string)
	}
	for k, v := range newLabels {
		dpu.Spec.Cluster.NodeLabels[k] = v
	}
	for _, k := range removedLabels {
		delete(dpu.Spec.Cluster.NodeLabels, k)
	}
}

// updateNodeEffectAction updates the NodeEffect Action fields (Taint, Drain, NoEffect, etc.) for existing DPUs when the DPUSet template changes.
// This function ensures that changes to the Action fields are propagated to existing DPUs without requiring recreation.
func updateNodeEffectAction(ctx context.Context, dpuSet *provisioningv1.DPUSet, dpu *provisioningv1.DPU) bool {
	logger := log.FromContext(ctx)

	if dpu.Status.Phase != provisioningv1.DPUReady {
		return false
	}

	// Get the expected Action from the DPUSet template
	var expectedAction provisioningv1.Action
	if dpuSet.Spec.DPUTemplate.Spec.NodeEffect != nil {
		expectedAction = dpuSet.Spec.DPUTemplate.Spec.NodeEffect.Action
	}

	// Get current Action from DPU
	var currentAction provisioningv1.Action
	if dpu.Spec.NodeEffect != nil {
		currentAction = dpu.Spec.NodeEffect.Action
	}

	// Check if Action has changed
	if !reflect.DeepEqual(currentAction, expectedAction) {
		logger.V(3).Info(fmt.Sprintf("Updating NodeEffect Action for DPU (%s/%s)", dpu.Namespace, dpu.Name))
		// Ensure NodeEffect struct exists
		if dpu.Spec.NodeEffect == nil {
			dpu.Spec.NodeEffect = &provisioningv1.NodeEffect{}
		}
		// Update Action fields
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
	var expectedApplyOnLabelChange *bool
	if dpuSet.Spec.DPUTemplate.Spec.NodeEffect != nil {
		expectedApplyOnLabelChange = dpuSet.Spec.DPUTemplate.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange
	}

	// Update ApplyOnLabelChange for existing DPUs if it has changed

	// Get current ApplyOnLabelChange value from DPU
	var currentApplyOnLabelChange *bool
	if dpu.Spec.NodeEffect != nil {
		currentApplyOnLabelChange = dpu.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange
	}

	// Check if ApplyOnLabelChange has changed
	if !reflect.DeepEqual(currentApplyOnLabelChange, expectedApplyOnLabelChange) {
		logger.V(3).Info(fmt.Sprintf("Updating ApplyOnLabelChange for DPU (%s/%s)", dpu.Namespace, dpu.Name))
		// Ensure NodeEffect struct exists
		if dpu.Spec.NodeEffect == nil {
			dpu.Spec.NodeEffect = &provisioningv1.NodeEffect{}
		}
		// Update ApplyOnLabelChange field
		dpu.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange = expectedApplyOnLabelChange
		return true
	}

	return false
}
