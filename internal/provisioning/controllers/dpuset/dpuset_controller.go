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
	"regexp"
	"strconv"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/events"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/reboot"
	"github.com/nvidia/doca-platform/internal/utils"

	"github.com/fluxcd/pkg/runtime/patch"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	// controller name that will be used when
	DPUSetControllerName    = "dpuset"
	DefaultFirstDPULabelKey = "dpu-0-pci-address"
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

func (r *DPUSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconcile")

	dpuSet := &provisioningv1.DPUSet{}
	if err := r.Get(ctx, req.NamespacedName, dpuSet); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get DPUSet %w", err)
	}

	if !dpuSet.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, dpuSet)
	}

	return r.Handle(ctx, dpuSet)
}

func (r *DPUSetReconciler) reconcileDelete(ctx context.Context, dpuSet *provisioningv1.DPUSet) (ctrl.Result, error) {
	if err := r.finalizeDPUSet(ctx, dpuSet); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to finalize DPUSet %w", err)
	}

	return ctrl.Result{}, nil
}

func (r *DPUSetReconciler) Handle(ctx context.Context, dpuSet *provisioningv1.DPUSet) (ctrl.Result, error) {
	var err error
	logger := log.FromContext(ctx)

	// Add finalizer if not set.
	if !controllerutil.ContainsFinalizer(dpuSet, provisioningv1.DPUSetFinalizer) {
		controllerutil.AddFinalizer(dpuSet, provisioningv1.DPUSetFinalizer)
		if err := r.Client.Update(ctx, dpuSet); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to add DPUSet finalizer %w", err)
		}
		return ctrl.Result{}, nil
	}

	// Get node map by nodeSelector
	nodeMap, err := r.getNodeMap(ctx, dpuSet.Spec.NodeSelector)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get Node list %w", err)
	}

	// Get dpu map which are owned by dpuset
	dpuMap, err := r.getDPUsMap(ctx, dpuSet)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get DPU list %w", err)
	}

	// create dpu for the node
	for nodeName, node := range nodeMap {
		if _, ok := dpuMap[nodeName]; !ok {
			dpuIndexMap := getDPUIndexFromSelector(dpuSet.Spec.DPUSelector, node.Labels)
			for _, index := range dpuIndexMap {
				if err = r.createDPU(ctx, dpuSet, node, index); err != nil {
					returnErr := fmt.Errorf("failed to created DPU index %v on node %s %w", index, node.Name, err)
					r.Recorder.Eventf(dpuSet, corev1.EventTypeWarning, events.EventFailedCreateDPUReason, returnErr.Error())
					return ctrl.Result{}, returnErr
				}
			}

		} else {
			delete(dpuMap, nodeName)
		}
	}

	// delete dpu if node does not exist
	for nodeName, dpu := range dpuMap {
		if err := r.Delete(ctx, &dpu); err != nil {
			returnErr := fmt.Errorf("failed to Delete DPU: (%s/%s) from node %s %w", dpu.Namespace, dpu.Name, nodeName, err)
			r.Recorder.Eventf(dpuSet, corev1.EventTypeNormal, events.EventSuccessfulDeleteDPUReason, returnErr.Error())
			return ctrl.Result{}, returnErr
		}
		msg := fmt.Sprintf("Delete DPU: (%s/%s) from node %s", dpu.Namespace, dpu.Name, nodeName)
		r.Recorder.Eventf(dpuSet, corev1.EventTypeNormal, events.EventSuccessfulDeleteDPUReason, msg)
		logger.V(2).Info(msg)
	}

	// handle rolling update
	dpuMap, err = r.getDPUsMap(ctx, dpuSet)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get DPUs %w", err)
	}

	switch dpuSet.Spec.Strategy.Type {
	case provisioningv1.RecreateStrategyType:
		// do nothing, waiting for user delete DPU object manually.
	case provisioningv1.RollingUpdateStrategyType:
		if err := r.rolloutRolling(ctx, dpuSet, dpuMap, len(nodeMap)); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to rollout DPU %w", err)
		}
	}

	if err := updateDPUSetStatus(ctx, dpuSet, dpuMap, r.Client); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update DPUSet status %w", err)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DPUSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&provisioningv1.DPUSet{}).
		Owns(&provisioningv1.DPU{}).
		Watches(&corev1.Node{},
			handler.EnqueueRequestsFromMapFunc(r.nodeToDPUSetReq),
			builder.WithPredicates(predicate.LabelChangedPredicate{})).
		Watches(&provisioningv1.DPUFlavor{},
			handler.EnqueueRequestsFromMapFunc(r.flavorToDPUSetReq)).
		Complete(r)
}

func (r *DPUSetReconciler) nodeToDPUSetReq(ctx context.Context, resource client.Object) []reconcile.Request {
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

func (r *DPUSetReconciler) getNodeMap(ctx context.Context, nselector *metav1.LabelSelector) (map[string]corev1.Node, error) {
	nodeMap := make(map[string]corev1.Node)
	nodeList := &corev1.NodeList{}
	nodeSelector, err := utils.LabelSelectorAsSelector(nselector)
	if err != nil {
		return nodeMap, err
	}
	listOptions := client.ListOptions{
		LabelSelector: nodeSelector,
	}

	if err := r.List(ctx, nodeList, &listOptions); err != nil {
		return nodeMap, err
	}

	for _, node := range nodeList.Items {
		nodeMap[node.Name] = node
	}
	return nodeMap, nil
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
		dpuMap[dpu.Spec.NodeName] = dpu
	}
	return dpuMap, nil
}

func (r *DPUSetReconciler) createDPU(ctx context.Context, dpuSet *provisioningv1.DPUSet,
	node corev1.Node, dpuIndex int) error {
	logger := log.FromContext(ctx)

	labels := map[string]string{cutil.DPUSetNameLabel: dpuSet.Name, cutil.DPUSetNamespaceLabel: dpuSet.Namespace}
	for k, v := range dpuSet.Labels {
		labels[k] = v
	}

	for k, v := range node.Labels {
		if strings.HasSuffix(k, fmt.Sprintf(cutil.DPUPCIAddress, dpuIndex)) {
			if len(v) != 0 {
				labels[cutil.DPUPCIAddressLabel] = v
			} else {
				return fmt.Errorf("the label of %s on node %s is empty", cutil.DPUPCIAddress, node.Name)
			}
		}
		if strings.HasSuffix(k, fmt.Sprintf(cutil.DPUPFName, dpuIndex)) {
			if len(v) != 0 {
				labels[cutil.DPUPFNameLabel] = v
			} else {
				return fmt.Errorf("the label of %s on node %s is empty", cutil.DPUPFName, node.Name)
			}
		}
	}
	for _, address := range node.Status.Addresses {
		if address.Type == corev1.NodeInternalIP {
			labels[cutil.DPUHostIPLabel] = address.Address
			break
		}
	}

	if _, ok := labels[cutil.DPUPCIAddressLabel]; !ok {
		return fmt.Errorf("DPU on node %s cannot be created due to lack of pci address of dpu", node.Name)
	}

	dpuName := fmt.Sprintf("%s-%s", strings.ToLower(node.Name), labels[cutil.DPUPCIAddressLabel])
	owner := metav1.NewControllerRef(dpuSet,
		provisioningv1.GroupVersion.WithKind("DPUSet"))

	clusterNodeLabels := map[string]string{}
	if dpuSet.Spec.DPUTemplate.Spec.Cluster != nil {
		clusterNodeLabels = dpuSet.Spec.DPUTemplate.Spec.Cluster.NodeLabels
	}
	dpu := &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			Name:            dpuName,
			Namespace:       dpuSet.Namespace,
			Labels:          labels,
			Annotations:     make(map[string]string),
			OwnerReferences: []metav1.OwnerReference{*owner},
		},
		Spec: provisioningv1.DPUSpec{
			NodeName:   node.Name,
			BFB:        dpuSet.Spec.DPUTemplate.Spec.BFB.Name,
			NodeEffect: dpuSet.Spec.DPUTemplate.Spec.NodeEffect,
			Cluster: provisioningv1.K8sCluster{
				NodeLabels: clusterNodeLabels,
			},
			DPUFlavor:           dpuSet.Spec.DPUTemplate.Spec.DPUFlavor,
			AutomaticNodeReboot: *dpuSet.Spec.DPUTemplate.Spec.AutomaticNodeReboot,
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
		if update, err := r.needUpdate(ctx, *dpuSet, dpu); err != nil {
			return err
		} else if !update {
			continue
		}

		failedMsg := fmt.Sprintf("Failed to Delete DPU: (%s/%s) from node %s", dpu.Namespace, dpu.Name, dpu.Spec.NodeName)
		successMsg := fmt.Sprintf("Delete DPU: (%s/%s) from node %s", dpu.Namespace, dpu.Name, dpu.Spec.NodeName)
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
func (r *DPUSetReconciler) needUpdate(ctx context.Context, dpuSet provisioningv1.DPUSet, dpu provisioningv1.DPU) (bool, error) {
	logger := log.FromContext(ctx)
	// update dpu node label
	newLabel := dpuSet.Spec.DPUTemplate.Spec.Cluster.NodeLabels
	oldLabel := dpu.Spec.Cluster.NodeLabels

	if cutil.NeedUpdateLabels(newLabel, oldLabel) {
		if dpu.Spec.Cluster.NodeLabels == nil {
			dpu.Spec.Cluster.NodeLabels = make(map[string]string)
		}
		patcher := patch.NewSerialPatcher(&dpu, r.Client)
		// merge label from DPUSet to DPU CR
		for k, v := range newLabel {
			dpu.Spec.Cluster.NodeLabels[k] = v
		}
		if err := patcher.Patch(ctx, &dpu); err != nil {
			return false, err
		} else {
			logger.V(3).Info(fmt.Sprintf("DPU (%s/%s) label update to %v", dpu.Namespace, dpu.Name, newLabel))
		}
	}

	return dpu.Spec.BFB != dpuSet.Spec.DPUTemplate.Spec.BFB.Name || dpu.Spec.DPUFlavor != dpuSet.Spec.DPUTemplate.Spec.DPUFlavor, nil
}

func updateDPUSetStatus(ctx context.Context, dpuSet *provisioningv1.DPUSet,
	dpuMap map[string]provisioningv1.DPU, client client.Client) error {
	dpuStatistics := make(map[provisioningv1.DPUPhase]int)
	for _, dpu := range dpuMap {
		switch dpu.Status.Phase {
		case "":
			dpuStatistics[provisioningv1.DPUInitializing]++

		case provisioningv1.DPUInitializing:
			dpuStatistics[provisioningv1.DPUInitializing]++

		case provisioningv1.DPUPending:
			dpuStatistics[provisioningv1.DPUPending]++

		case provisioningv1.DPUDMSDeployment:
			dpuStatistics[provisioningv1.DPUDMSDeployment]++

		case provisioningv1.DPUHostNetworkConfiguration:
			dpuStatistics[provisioningv1.DPUHostNetworkConfiguration]++

		case provisioningv1.DPUOSInstalling:
			dpuStatistics[provisioningv1.DPUOSInstalling]++

		case provisioningv1.DPUClusterConfig:
			dpuStatistics[provisioningv1.DPUClusterConfig]++

		case provisioningv1.DPUReady:
			dpuStatistics[provisioningv1.DPUReady]++

		case provisioningv1.DPUError:
			dpuStatistics[provisioningv1.DPUError]++

		case provisioningv1.DPUDeleting:
			dpuStatistics[provisioningv1.DPUDeleting]++
		}
	}

	needUpdate := false
	if len(dpuStatistics) != len(dpuSet.Status.DPUStatistics) {
		needUpdate = true
	} else {
		for key, count1 := range dpuStatistics {
			count2, ok := dpuSet.Status.DPUStatistics[key]
			if !ok {
				needUpdate = true
				break
			} else if count1 != count2 {
				needUpdate = true
				break
			}
		}
	}
	if needUpdate {
		dpuSet.Status.DPUStatistics = dpuStatistics
		if err := client.Status().Update(ctx, dpuSet); err != nil {
			return err
		}
	}

	return nil
}

func (r *DPUSetReconciler) finalizeDPUSet(ctx context.Context, dpuSet *provisioningv1.DPUSet) error {
	controllerutil.RemoveFinalizer(dpuSet, provisioningv1.DPUSetFinalizer)
	if err := r.Client.Update(ctx, dpuSet); err != nil {
		return err
	}
	return nil
}

func getDPUIndexFromSelector(dpuSelector map[string]string, nodeLabels map[string]string) map[int]int {
	dpuIndexMap := make(map[int]int)

	if len(dpuSelector) != 0 {
		for key, value := range dpuSelector {
			if v, ok := nodeLabels[key]; ok && value == v {
				keyRegex := regexp.MustCompile(`^feature.node.kubernetes.io/dpu-\d+-.*$`)
				// key must match "feature.node.kubernetes.io/dpu-index-" pattern
				if keyRegex.MatchString(key) {
					// get DPU index from lable key
					indexRegex := regexp.MustCompile(`dpu-(\d+)-.*?`)
					matches := indexRegex.FindStringSubmatch(key)
					if len(matches) > 1 {
						if index, err := strconv.Atoi(matches[1]); err == nil {
							dpuIndexMap[index] = index
						}
					}
				}
			}
		}
	} else {
		// If dpuSelector is not set, use index 0 DPU by default
		for k := range nodeLabels {
			if strings.HasSuffix(k, fmt.Sprintf(cutil.DPUPCIAddress, 0)) {
				dpuIndexMap[0] = 0
				break
			}
		}
	}
	return dpuIndexMap
}
