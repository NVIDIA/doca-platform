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

package controllers

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	sfcsetcontroller "github.com/nvidia/doca-platform/internal/servicechainset/controllers"
	"github.com/nvidia/doca-platform/internal/utils"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	"github.com/nvidia/doca-platform/pkg/dpuselector"

	"github.com/fluxcd/pkg/runtime/patch"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/util/workqueue"
	schedulingv1 "k8s.io/component-helpers/scheduling/corev1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=dpuservices,verbs=get;list;watch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpus,verbs=get;list;watch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpunodemaintenances,verbs=get;list;watch;update;patch

type DPUReadyReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	controller controller.Controller

	// DisableDPUReadyTaints, if set to true, will disable the addition of DPU-ready taints to nodes.
	DisableDPUReadyTaints bool
}

const (
	// taintKey is the key for the taint applied to host worker nodes that are not DPU-ready.
	taintKey = "dpu.nvidia.com/dpu-ready"
	// taintEffect is the effect for the DPU-ready taint, preventing scheduling on non-ready host worker nodes.
	taintEffect = corev1.TaintEffectNoSchedule
	// dpuEnabledLabelKey is the label key indicating that a node has DPU enabled.
	dpuEnabledLabelKey = "feature.node.kubernetes.io/dpu-enabled"
	// dpuEnabledLabelValue is the label value indicating that a node has DPU enabled.
	dpuEnabledLabelValue = "true"
	// criticalDPUServiceLabel is the label used to identify critical DPU services.
	criticalDPUServiceLabel = "svc.dpu.nvidia.com/critical"
	// dpureadyControllerName is the name of the controller for the DPUReadyReconciler.
	dpureadyControllerName = "dpureadycontroller"
)

// SetupWithManager configures the controller with the manager and sets up required indexes and predicates
func (r *DPUReadyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	p := predicate.NewPredicateFuncs(func(o client.Object) bool {
		node, ok := o.(*corev1.Node)
		if !ok {
			return false
		}

		// Skip control plane nodes
		if _, exists := node.ObjectMeta.Labels["node-role.kubernetes.io/control-plane"]; exists {
			return false
		}

		if _, exists := node.ObjectMeta.Labels["node-role.kubernetes.io/master"]; exists {
			return false
		}

		// Skip nodes without DPU feature discovery label
		if val, exists := node.ObjectMeta.Labels[dpuEnabledLabelKey]; !exists || val != dpuEnabledLabelValue {
			return false
		}
		return true
	})

	c, err := ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Node{}, builder.WithPredicates(p)).
		Build(r)
	if err != nil {
		return err
	}
	r.controller = c
	return nil
}

// Reconcile handles the reconciliation of Node objects for DPU readiness
func (r *DPUReadyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	node := corev1.Node{}
	log := ctrllog.FromContext(ctx)
	log.Info("Reconciling node", "node", req.Name)
	if err := r.Get(ctx, req.NamespacedName, &node); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Return early if the object is getting deleted
	if !node.ObjectMeta.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	return r.reconcile(ctx, &node)
}

type reconcileScope struct {
	node           *corev1.Node
	dpuList        []provisioningv1.DPU
	dpuServiceList *dpuservicev1.DPUServiceList
}

func (r *DPUReadyReconciler) newReconcileScope(ctx context.Context, node *corev1.Node) (*reconcileScope, error) {
	dpuList, err := r.getDPUsByNodeName(ctx, node.Name)
	if err != nil {
		return nil, fmt.Errorf("could not get the dpunode from node: %w", err)
	}

	dpuServiceList := &dpuservicev1.DPUServiceList{}
	if err := r.Client.List(ctx, dpuServiceList, client.MatchingFields{
		dpuServiceDeployInClusterField: strconv.FormatBool(false),
	}); err != nil {
		return nil, fmt.Errorf("failed to list DPUServices: %w", err)
	}

	return &reconcileScope{
		node:           node,
		dpuList:        dpuList,
		dpuServiceList: dpuServiceList,
	}, nil
}

func (r *DPUReadyReconciler) reconcile(ctx context.Context, node *corev1.Node) (ctrl.Result, error) {
	scope, err := r.newReconcileScope(ctx, node)
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileDPUReadyTaints(ctx, scope); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile DPUServices: %w", err)
	}

	if err := r.reconcileDPUNodeMaintenance(ctx, scope); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile DPUNodeMaintenance: %w", err)
	}

	return ctrl.Result{}, nil
}

// reconcileDPUReadyTaints reconciles the DPUServices for a given node
// It returns the list of ready DPUServices
func (r *DPUReadyReconciler) reconcileDPUReadyTaints(ctx context.Context, scope *reconcileScope) error {
	if r.DisableDPUReadyTaints {
		return nil
	}

	log := ctrllog.FromContext(ctx)
	log.V(3).Info("Reconciling DPU ready taints for node", "node", scope.node.Name)
	dpuServicesReadyStatus, err := r.getServicesStatus(ctx, scope.dpuList, scope.dpuServiceList)
	if err != nil {
		return err
	}

	criticalServicesReady := isCriticalServiceReady(dpuServicesReadyStatus, r.getCriticalDPUServiceList(scope.dpuServiceList).Items)
	if criticalServicesReady {
		if err = r.removeTaintIfExists(ctx, scope.node); err != nil {
			return fmt.Errorf("error removing taint from node %s: %w", scope.node.Name, err)
		}
	} else {
		if err = r.addTaintIfDoesNotExist(ctx, scope.node); err != nil {
			return fmt.Errorf("error adding taint to node %s: %w", scope.node.Name, err)
		}
	}

	return nil
}

func (r *DPUReadyReconciler) reconcileDPUNodeMaintenance(ctx context.Context, scope *reconcileScope) error {
	dpuNodeMaintenances, err := r.getDPUNodeMaintenanceObjects(ctx, scope.node)
	if err != nil {
		return fmt.Errorf("failed to get DPUNodeMaintenance objects: %w", err)
	}

	if len(dpuNodeMaintenances) == 0 {
		return nil
	}

	if err := r.patchDPUNodeMaintenanceObjects(ctx, scope, dpuNodeMaintenances); err != nil {
		return fmt.Errorf("failed to patch DPUNodeMaintenance objects for node %s: %w", scope.node.Name, err)
	}

	return nil
}

func (r *DPUReadyReconciler) readyDPUServiceChains(ctx context.Context, dpuList []provisioningv1.DPU) ([]dpuservicev1.DPUServiceChain, error) {
	dpuServiceChainList := &dpuservicev1.DPUServiceChainList{}
	if err := r.Client.List(ctx, dpuServiceChainList, client.HasLabels{
		dpuservicev1.ParentDPUDeploymentNameLabel,
	}); err != nil {
		return nil, fmt.Errorf("failed to list DPUServiceChains: %w", err)
	}
	dpuServiceChainsReadyStatus, err := r.getDPUServiceChainsStatus(ctx, dpuList, dpuServiceChainList)
	if err != nil {
		return nil, err
	}
	dpuServicesChains := getReadyDPUServiceChainsFromList(dpuServiceChainList.Items, dpuServiceChainsReadyStatus)

	return dpuServicesChains, nil
}

func (r *DPUReadyReconciler) readyDPUServices(ctx context.Context, scope *reconcileScope) ([]dpuservicev1.DPUService, error) {
	dpuServicesReadyStatus, err := r.getServicesStatus(ctx, scope.dpuList, scope.dpuServiceList)
	if err != nil {
		return nil, err
	}
	dpuServices := getReadyDPUServicesFromList(scope.dpuServiceList.Items, dpuServicesReadyStatus)
	return dpuServices, nil
}

func (r *DPUReadyReconciler) patchDPUNodeMaintenanceObjects(
	ctx context.Context,
	scope *reconcileScope,
	dpuNodeMaintenanceObjects []*provisioningv1.DPUNodeMaintenance,
) error {
	oldDPUNodeMaintenanceObjects := make(map[types.NamespacedName]*provisioningv1.DPUNodeMaintenance, len(dpuNodeMaintenanceObjects))
	for _, dpuNodeMaintenance := range dpuNodeMaintenanceObjects {
		oldDPUNodeMaintenanceObjects[client.ObjectKeyFromObject(dpuNodeMaintenance)] = dpuNodeMaintenance.DeepCopy()
	}

	dpuServices, err := r.readyDPUServices(ctx, scope)
	if err != nil {
		return err
	}

	dpuServicesChains, err := r.readyDPUServiceChains(ctx, scope.dpuList)
	if err != nil {
		return err
	}

	for _, dpuNodeMaintenance := range dpuNodeMaintenanceObjects {
		for _, dpuService := range dpuServices {
			potentialRequestor := getRequestorForDPUObjectVersion(dpuService.Labels[dpuservicev1.ParentDPUDeploymentNameLabel], dpuService.Name)
			if !slices.Contains(dpuNodeMaintenance.Spec.Requestor, potentialRequestor) {
				continue
			}
			dpuNodeMaintenance.Spec.Requestor = slices.DeleteFunc(dpuNodeMaintenance.Spec.Requestor, func(requestor string) bool {
				return requestor == potentialRequestor
			})
		}
		for _, dpuServiceChain := range dpuServicesChains {
			potentialRequestor := getRequestorForDPUObjectVersion(dpuServiceChain.Labels[dpuservicev1.ParentDPUDeploymentNameLabel], dpuServiceChain.Name)
			if !slices.Contains(dpuNodeMaintenance.Spec.Requestor, potentialRequestor) {
				continue
			}
			dpuNodeMaintenance.Spec.Requestor = slices.DeleteFunc(dpuNodeMaintenance.Spec.Requestor, func(requestor string) bool {
				return requestor == potentialRequestor
			})
		}
	}
	for _, dpuNodeMaintenance := range dpuNodeMaintenanceObjects {
		patcher := patch.NewSerialPatcher(oldDPUNodeMaintenanceObjects[client.ObjectKeyFromObject(dpuNodeMaintenance)], r.Client)
		if err := patcher.Patch(ctx, dpuNodeMaintenance, patch.WithFieldOwner(dpureadyControllerName)); err != nil {
			return fmt.Errorf("failed to patch DPUNodeMaintenance %s: %w", client.ObjectKeyFromObject(dpuNodeMaintenance), err)
		}
	}

	return nil
}

// getCriticalDPUServiceList gets the list of critical DPU services from the list of all DPU services
func (r *DPUReadyReconciler) getCriticalDPUServiceList(dpuServiceList *dpuservicev1.DPUServiceList) *dpuservicev1.DPUServiceList {
	criticalDPUServices := &dpuservicev1.DPUServiceList{}
	for _, dpuService := range dpuServiceList.Items {
		if _, ok := dpuService.Labels[criticalDPUServiceLabel]; ok {
			criticalDPUServices.Items = append(criticalDPUServices.Items, dpuService)
		}
	}

	return criticalDPUServices
}

func (r *DPUReadyReconciler) getDPUNodeMaintenanceObjects(ctx context.Context, node *corev1.Node) ([]*provisioningv1.DPUNodeMaintenance, error) {
	// Get all the DPUNodeMaintenance objects related to this node
	dpuNodeMaintenanceList := &provisioningv1.DPUNodeMaintenanceList{}
	if err := r.Client.List(ctx, dpuNodeMaintenanceList, client.MatchingFields{dpuNodeMaintenanceDPUNodeNameField: node.Name}); err != nil {
		return nil, fmt.Errorf("failed to list DPUNodeMaintenance objects: %w", err)
	}

	readyObjects := make([]*provisioningv1.DPUNodeMaintenance, 0, len(dpuNodeMaintenanceList.Items))
	for i := range dpuNodeMaintenanceList.Items {
		if conditions.IsTrue(&dpuNodeMaintenanceList.Items[i], provisioningv1.ConditionNodeEffectApplied) {
			readyObjects = append(readyObjects, &dpuNodeMaintenanceList.Items[i])
		}
	}
	// Return early if there are no DPUNodeMaintenance objects for this node
	if len(readyObjects) == 0 {
		return nil, nil
	}

	return readyObjects, nil
}

// getDPUsByNodeName retrieves the DPU objects associated with a given host node name
func (r *DPUReadyReconciler) getDPUsByNodeName(ctx context.Context, nodeName string) ([]provisioningv1.DPU, error) {
	selector := dpuselector.New(dpuselector.WithIndexerField{FieldName: dpuNodeNameField})
	dpus, err := selector.ListDPUsForNode(ctx, r.Client, &provisioningv1.DPUNode{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
		}})
	if err != nil {
		return nil, err
	}

	if len(dpus) == 0 {
		return nil, fmt.Errorf("no DPU found for node name %s", nodeName)
	}

	return dpus, nil
}

func (r *DPUReadyReconciler) getDPUServiceChainsStatus(ctx context.Context, dpuList []provisioningv1.DPU,
	dpuServiceChainList *dpuservicev1.DPUServiceChainList) (map[string]bool, error) {

	var errs []error
	allDPUStatuses := make([]map[string]bool, 0, len(dpuList))

	for _, dpu := range dpuList {
		serviceChainsReadyStatus, err := r.getServiceChainsStatusPerDPU(ctx, &dpu, dpuServiceChainList)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		allDPUStatuses = append(allDPUStatuses, serviceChainsReadyStatus)
	}

	dpuServiceChainReadyStatus := statusesAggregation(allDPUStatuses)

	return dpuServiceChainReadyStatus, kerrors.NewAggregate(errs)
}

// getServicesStatus checks if all DPU services are running on the DPU node.
// It connects to the DPU cluster and verifies that all service pods are truly ready.
// Returns a map of service names and their readiness status for all DPUs.
func (r *DPUReadyReconciler) getServicesStatus(
	ctx context.Context,
	dpuList []provisioningv1.DPU,
	dpuServiceList *dpuservicev1.DPUServiceList,
) (map[string]bool, error) {
	var errs []error
	allDPUStatuses := make([]map[string]bool, 0, len(dpuList))

	for _, dpu := range dpuList {
		servicesReadyStatus, err := r.getServicesStatusPerDPU(ctx, &dpu, dpuServiceList)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		allDPUStatuses = append(allDPUStatuses, servicesReadyStatus)
	}

	finalServicesReadyStatus := statusesAggregation(allDPUStatuses)

	return finalServicesReadyStatus, kerrors.NewAggregate(errs)
}

// statusesAggregation combines statuses from multiple DPUs using an all-or-nothing strategy.
// An object is considered ready only if it's ready on ALL DPUs where it should be running.
func statusesAggregation(allDPUStatuses []map[string]bool) map[string]bool {
	finalStatuses := make(map[string]bool)

	for _, dpuStatus := range allDPUStatuses {
		for service, isReady := range dpuStatus {
			if _, exists := finalStatuses[service]; !exists {
				// First DPU reporting this service - initialize with its status
				finalStatuses[service] = isReady
				continue
			}
			// Subsequent DPUs - apply AND logic (once false, stays false)
			finalStatuses[service] = finalStatuses[service] && isReady
		}
	}

	return finalStatuses
}

func (r *DPUReadyReconciler) getServiceChainsStatusPerDPU(ctx context.Context, dpu *provisioningv1.DPU, dpuServiceChainList *dpuservicev1.DPUServiceChainList) (map[string]bool, error) {
	log := ctrllog.FromContext(ctx)
	dpuClusterClient, err := dpucluster.K8sClusterToDPUClusterConfig(r.Client, &(dpu.Spec.Cluster)).Client(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not get the kubeconfig for the dpucluster: %w", err)
	}

	dpuNode := &corev1.Node{}
	err = dpuClusterClient.Get(ctx, types.NamespacedName{Name: dpu.Name}, dpuNode)
	if err != nil {
		return nil, fmt.Errorf("failed to get the dpu node %s: %w", dpu.Name, err)
	}

	serviceChainsReadyStatus := make(map[string]bool)
	for _, dpuServiceChain := range dpuServiceChainList.Items {
		serviceChainsReadyStatus[dpuServiceChain.Name] = false
	}

	for _, dpuServiceChain := range dpuServiceChainList.Items {
		matches, err := nodeMatchesLabelSelector(dpuNode, dpuServiceChain.Spec.Template.Spec.NodeSelector)
		if err != nil {
			log.Error(err, "failed to match node selector for service chain",
				"serviceChain", dpuServiceChain.Name)
			continue
		}
		if !matches {
			// service chain should not be running on this node, continue to the next service chain
			// remove it from the list of service chains
			delete(serviceChainsReadyStatus, dpuServiceChain.Name)
			continue
		}
		serviceChainList := &dpuservicev1.ServiceChainList{}
		if err := dpuClusterClient.List(ctx, serviceChainList,
			client.MatchingLabels(map[string]string{
				sfcsetcontroller.ServiceChainSetNameLabel:      dpuServiceChain.GetName(),
				sfcsetcontroller.ServiceChainSetNamespaceLabel: dpuServiceChain.GetNamespace(),
			}),
			client.InNamespace(dpuServiceChain.GetNamespace())); err != nil {
			return nil, err
		}

		if len(serviceChainList.Items) == 0 {
			log.V(1).Info("service chain not found",
				"dpuServiceChain", dpuServiceChain.Name,
				"serviceChain", serviceChainList.Items,
				"dpu", dpu.Name)
			continue
		}

		serviceChains := []dpuservicev1.ServiceChain{}
		for _, svc := range serviceChainList.Items {
			if svc.Spec.Node != nil && *svc.Spec.Node == dpuNode.Name {
				serviceChains = append(serviceChains, svc)
				break
			}
		}

		if len(serviceChains) == 0 {
			log.V(1).Info("service chain not found",
				"dpuServiceChain", dpuServiceChain.Name,
				"dpu", dpu.Name)
			continue
		}

		if len(serviceChains) > 1 {
			log.V(1).Error(nil, "more than one service chain found",
				"dpuServiceChain", dpuServiceChain.Name,
				"serviceChains", serviceChains,
				"dpu", dpu.Name)
			continue
		}

		// Check if the ServiceChain is ready
		if conditions.IsTrue(&serviceChains[0], conditions.TypeReady) {
			serviceChainsReadyStatus[dpuServiceChain.Name] = true
		}
	}
	return serviceChainsReadyStatus, nil
}

// getServicesStatusPerDPU returns a list of services that are ready.
// Services are considered ready if they have running and ready pods on the DPU.
func (r *DPUReadyReconciler) getServicesStatusPerDPU(ctx context.Context, dpu *provisioningv1.DPU, dpuServiceList *dpuservicev1.DPUServiceList) (map[string]bool, error) {
	log := ctrllog.FromContext(ctx)
	dpuClusterClient, _, err := dpucluster.K8sClusterToDPUClusterConfig(r.Client, &(dpu.Spec.Cluster)).Clientset(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not get the kubeconfig for the dpucluster: %w", err)
	}

	// get all running pods on the dpuNode
	podList, err := dpuClusterClient.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=%s", dpu.Name),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods on the node %s: %w", dpu.Name, err)
	}

	dpuNode, err := dpuClusterClient.CoreV1().Nodes().Get(ctx, dpu.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get the dpu node %s: %w", dpu.Name, err)
	}

	servicesReadyStatus := make(map[string]bool)
	// initialize all services to false
	for _, service := range dpuServiceList.Items {
		servicesReadyStatus[service.Name] = false
	}
	for _, service := range dpuServiceList.Items {
		if service.Spec.ServiceDaemonSet != nil {
			matches, err := nodeMatchesNodeSelector(dpuNode, service.Spec.ServiceDaemonSet.NodeSelector)
			if err != nil {
				log.Error(err, "failed to match node selector for service",
					"service", service.Name)
				continue
			}
			if !matches {
				// service should not be running on this node, continue to the next service
				delete(servicesReadyStatus, service.Name)
				continue
			}
		}

		var servicePod *corev1.Pod
		// get the matching pod from the running pod list
		for _, pod := range podList.Items {
			// Match pods using the DPFServiceIDLabelKey label which is set by the service daemonset
			if serviceID, exists := pod.Labels[dpuservicev1.DPFServiceIDLabelKey]; exists &&
				service.Spec.ServiceID != nil && serviceID == *service.Spec.ServiceID {
				servicePod = &pod
				break
			}
			/*
				serviceID is optional. If it does not exist, match with podname substring
				// example
				// dpuservice: sriov-device-plugin
				// pod:kube-sriov-device-plugin-dr8lf
			*/
			if strings.Contains(pod.Name, service.Name) {
				servicePod = &pod
				break
			}

		}
		if servicePod == nil {
			log.V(1).Info("service pod not found",
				"service", service.Name,
				"dpu", dpu.Name)
			// Service is not ready if pod is not found
			continue
		}
		if !isPodRunning(servicePod) {
			log.Info("service pod not ready",
				"service", service.Name,
				"pod", servicePod.Name,
				"dpu", dpu.Name)
			// Service is not ready if pod is not running
			continue
		}
		servicesReadyStatus[service.Name] = true
	}
	return servicesReadyStatus, nil
}

// patchNode applies a strategic merge patch to update the node's taints
func (r *DPUReadyReconciler) patchNode(ctx context.Context, taints []corev1.Taint, node *corev1.Node) error {
	originalNode := node.DeepCopy()
	node.Spec.Taints = taints
	patch := client.StrategicMergeFrom(originalNode)
	if err := r.Client.Patch(ctx, node, patch); err != nil {
		return fmt.Errorf("failed to patch node %s: %w", node.Name, err)
	}
	return nil
}

// addTaintIfDoesNotExist adds the DPU-ready taint to a node if it doesn't already exist
func (r *DPUReadyReconciler) addTaintIfDoesNotExist(ctx context.Context, node *corev1.Node) error {
	log := ctrllog.FromContext(ctx)
	taint := corev1.Taint{
		Key:    taintKey,
		Effect: taintEffect,
	}

	for _, taint := range node.Spec.Taints {
		if taint.Key == taintKey {
			return nil
		}
	}

	taints := append(node.Spec.Taints, taint)
	log.Info("Adding taint to node", "nodeName", node.Name)
	return r.patchNode(ctx, taints, node)
}

// removeTaintIfExists removes the DPU-ready taint from a node if it exists
func (r *DPUReadyReconciler) removeTaintIfExists(ctx context.Context, node *corev1.Node) error {
	log := ctrllog.FromContext(ctx)
	newTaints := []corev1.Taint{}
	taintExists := false
	for _, taint := range node.Spec.Taints {
		if taint.Key != taintKey {
			newTaints = append(newTaints, taint)
		} else {
			taintExists = true
		}
	}
	if !taintExists {
		return nil
	}
	log.Info("Removing taint from node", "nodeName", node.Name)
	return r.patchNode(ctx, newTaints, node)
}

// isPodRunning checks if a pod is truly ready by examining the PodReady condition on pod status
func isPodRunning(pod *corev1.Pod) bool {
	// terminating pods, return false
	if pod.DeletionTimestamp != nil {
		return false
	}

	// any other phase than running, return false
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}

	// pod.status.phase could be running but one of the container might not be running
	// corev1.podready is set by kubelet which guarantees that all containers are properly running
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady {
			return cond.Status == corev1.ConditionTrue
		}
	}
	return false
}

func getReadyDPUServiceChainsFromList(dpuServiceChainList []dpuservicev1.DPUServiceChain, serviceChainsReadyStatus map[string]bool) []dpuservicev1.DPUServiceChain {
	dpuServiceChains := []dpuservicev1.DPUServiceChain{}
	for _, dpuServiceChain := range dpuServiceChainList {
		if serviceChainsReadyStatus[dpuServiceChain.Name] {
			dpuServiceChains = append(dpuServiceChains, dpuServiceChain)
		}
	}
	return dpuServiceChains
}

func getReadyDPUServicesFromList(dpuServiceList []dpuservicev1.DPUService, servicesReadyStatus map[string]bool) []dpuservicev1.DPUService {
	dpuServices := []dpuservicev1.DPUService{}
	for _, dpuService := range dpuServiceList {
		if servicesReadyStatus[dpuService.Name] {
			dpuServices = append(dpuServices, dpuService)
		}
	}
	return dpuServices
}

// nodeMatchesNodeSelector checks if a node matches the given node selector criteria
func nodeMatchesNodeSelector(node *corev1.Node, nodeSelector *corev1.NodeSelector) (bool, error) {
	// If there's no node selector, all nodes are valid
	if nodeSelector == nil {
		return true, nil
	}

	// Kubernetes has a helper function that does this matching for us
	res, err := schedulingv1.MatchNodeSelectorTerms(node, nodeSelector)
	return res, err
}

// nodeMatchesLabelSelector checks if a node matches the given label selector criteria
func nodeMatchesLabelSelector(node *corev1.Node, labelSelector *metav1.LabelSelector) (bool, error) {
	// If there's no label selector, all nodes are valid
	if labelSelector == nil {
		return true, nil
	}

	// Convert the label selector to a selector
	selector, err := utils.LabelSelectorAsSelector(labelSelector)
	if err != nil {
		return false, fmt.Errorf("failed to parse label selector: %w", err)
	}

	// Check if the node labels match the selector
	return selector.Matches(labels.Set(node.Labels)), nil
}

func isCriticalServiceReady(servicesStatus map[string]bool, criticalServices []dpuservicev1.DPUService) bool {
	// Check that ALL critical services are ready
	for _, criticalService := range criticalServices {
		ready := servicesStatus[criticalService.Name]
		if !ready {
			return false // If any critical service is not ready, return false
		}
	}
	return true
}

// WatchServicePods sets up watches for service pods in DPU clusters that have a service id label
func (r *DPUReadyReconciler) WatchServicePods(ctx context.Context, c client.Client, cluster client.ObjectKey) (dpucluster.Watcher, error) {
	return dpucluster.NewWatcher(dpucluster.WatcherOptions{
		Name:         "dpuready-pod-watcher",
		Kind:         &corev1.Pod{},
		EventHandler: &podEventHandler{client: c},
		Predicates: []predicate.Predicate{
			// filter only pods with DPFServiceIDLabelKey (critical service pods)
			newLabelPredicate(),
			// filter only relevant phase transition events
			// We already watch node so new pod status will be checked on node events reconciliation
			// We most likely only need to trigger reconciliation if the pod is transitioning to/from Running phase
			// and if the pod is being deleted because this means the corresponding service readiness should be checked
			newPhasePredicate(),
		},
		Watcher: r.controller,
	}), nil
}

func newLabelPredicate() predicate.Predicate {
	return predicate.NewPredicateFuncs(func(obj client.Object) bool {
		pod := obj.(*corev1.Pod)
		_, hasServiceID := pod.Labels[dpuservicev1.DPFServiceIDLabelKey]
		return hasServiceID
	})
}

func newPhasePredicate() predicate.Funcs {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			// we are not interested in create events
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldPod := e.ObjectOld.(*corev1.Pod)
			newPod := e.ObjectNew.(*corev1.Pod)

			// We only care about transitions to/from Running phase
			oldPhase := oldPod.Status.Phase
			newPhase := newPod.Status.Phase

			// Only trigger reconciliation if:
			// 1. Pod is transitioning To Running phase (from any other phase)
			if oldPhase != corev1.PodRunning && newPhase == corev1.PodRunning {
				return true
			}

			// 2. Pod is transitioning FROM Running phase (to any other phase)
			if oldPhase == corev1.PodRunning && newPhase != corev1.PodRunning {
				return true
			}

			// 3. Pod deletion timestamp set (pod is being deleted)
			if oldPod.DeletionTimestamp == nil && newPod.DeletionTimestamp != nil {
				return true
			}

			// Ignore all other updates
			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// accept all delete events
			return true
		},
		GenericFunc: func(e event.GenericEvent) bool {
			// we are not interested in generic events
			return false
		},
	}
}

// podEventHandler is a handler for pod events
type podEventHandler struct {
	client client.Client
}

// handlePodEventHelper retrieve the node name from the pod, and use the remote cache provided client to get the node.
// It then retrieve the host node name from the node labels and enqueues a request for the host node
func (p *podEventHandler) handlePodEventHelper(ctx context.Context, pod *corev1.Pod, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	log := ctrllog.FromContext(ctx)

	// get the node from the dpu cluster
	node := &corev1.Node{}
	if err := p.client.Get(ctx, client.ObjectKey{Name: pod.Spec.NodeName}, node); err != nil {
		log.Error(err, "Failed to get node from DPU cluster", "name", pod.Spec.NodeName)
		return
	}
	// find the host node name from the node labels
	host, exists := node.Labels[cutil.HostNameDPULabelKey]
	if !exists {
		log.Error(fmt.Errorf("host name not found for node %s", node.Name), "Failed to get host name for node")
		return
	}

	p.enqueue([]reconcile.Request{{NamespacedName: client.ObjectKey{Name: host}}}, q)
}

func (p *podEventHandler) Create(ctx context.Context, e event.CreateEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	// Create is not implemented
	// because we are not interested in create events and the predicate filters out all create events
}

// Update finds the host node name from the node labels and enqueues a request for the host node
func (p *podEventHandler) Update(ctx context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	reqLog := ctrllog.FromContext(ctx)

	pod, ok := e.ObjectNew.(*corev1.Pod)
	if !ok {
		reqLog.Error(fmt.Errorf("event expected a Pod but got a %T", e.ObjectNew), "Failed to convert object")
		return
	}

	p.handlePodEventHelper(ctx, pod, q)
}

// Delete finds the host node name from the node labels and enqueues a request for the host node
func (p *podEventHandler) Delete(ctx context.Context, e event.DeleteEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	reqLog := ctrllog.FromContext(ctx)

	pod, ok := e.Object.(*corev1.Pod)
	if !ok {
		reqLog.Error(fmt.Errorf("event expected a Pod but got a %T", e.Object), "Failed to convert object")
		return
	}

	p.handlePodEventHelper(ctx, pod, q)
}

func (p *podEventHandler) Generic(ctx context.Context, e event.GenericEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	// Generic is not implemented
	// because we are not interested in generic events and the predicate filters out all generic events
}

// enqueue enqueues requests into q after deduplication
func (p *podEventHandler) enqueue(requests []ctrl.Request, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	reqs := make(map[ctrl.Request]struct{})

	// deduplicate requests
	for _, req := range requests {
		reqs[req] = struct{}{}
	}

	for req := range reqs {
		q.Add(req)
	}
}
