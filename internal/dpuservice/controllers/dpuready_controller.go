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

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	sfcsetcontroller "github.com/nvidia/doca-platform/internal/servicechainset/controllers"
	"github.com/nvidia/doca-platform/internal/utils"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	"github.com/nvidia/doca-platform/pkg/dpuselector"
	predicateutils "github.com/nvidia/doca-platform/pkg/utils/predicates"

	"github.com/fluxcd/pkg/runtime/patch"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list
// +kubebuilder:rbac:groups=operator.dpu.nvidia.com,resources=dpfoperatorconfigs,verbs=get;list;watch
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=dpuservices,verbs=get;list;watch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpus,verbs=get;list;watch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpus/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpunodes,verbs=get;list;watch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpunodemaintenances,verbs=get;list;watch;update;patch

type DPUReadyReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	controller  controller.Controller
	RemoteCache *dpucluster.RemoteCache

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
	nodePredicate := predicate.NewPredicateFuncs(func(o client.Object) bool {
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
		For(&provisioningv1.DPUNode{}).
		Watches(&corev1.Node{}, handler.EnqueueRequestsFromMapFunc(r.nodeToDPUNodeReq), builder.WithPredicates(nodePredicate)).
		Watches(&provisioningv1.DPU{}, handler.EnqueueRequestsFromMapFunc(r.dpuToDPUNodeReq), builder.WithPredicates(predicateutils.ReadyConditionChanged())).
		Watches(&dpuservicev1.DPUServiceChain{}, handler.EnqueueRequestsFromMapFunc(r.sfcObjToDPUNodeReq), builder.WithPredicates(predicateutils.ReadyConditionChanged())).
		Watches(&dpuservicev1.DPUServiceInterface{}, handler.EnqueueRequestsFromMapFunc(r.sfcObjToDPUNodeReq), builder.WithPredicates(predicateutils.ReadyConditionChanged())).
		Build(r)
	if err != nil {
		return err
	}
	r.controller = c
	return nil
}

// dpuToDPUNodeReq maps DPU changes to DPUNode reconcile requests
func (r *DPUReadyReconciler) dpuToDPUNodeReq(ctx context.Context, obj client.Object) []reconcile.Request {
	dpu := obj.(*provisioningv1.DPU)
	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{
			Namespace: dpu.Namespace,
			Name:      dpu.Spec.DPUNodeName,
		},
	}}
}

// nodeToDPUNodeReq maps Node changes from management cluster to DPUNode reconcile requests using the KubeNodeRef index
func (r *DPUReadyReconciler) nodeToDPUNodeReq(ctx context.Context, obj client.Object) []reconcile.Request {
	node := obj.(*corev1.Node)
	log := ctrllog.FromContext(ctx)

	dpuNodeList := &provisioningv1.DPUNodeList{}
	if err := r.List(ctx, dpuNodeList,
		client.MatchingFields{dpuNodeKubeNodeRefField: node.Name},
	); err != nil {
		log.Error(err, "Failed to list DPUNodes for node", "node", node.Name)
		return nil
	}

	requests := make([]reconcile.Request, 0, len(dpuNodeList.Items))
	for _, dpuNode := range dpuNodeList.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&dpuNode),
		})
	}
	return requests
}

func (r *DPUReadyReconciler) sfcObjToDPUNodeReq(ctx context.Context, obj client.Object) []reconcile.Request {
	dpuClusterSelector, nodeSelector := extractSelectors(ctx, obj)
	ctx = ctrllog.IntoContext(ctx, ctrl.LoggerFrom(ctx).WithValues(
		"objectType", fmt.Sprintf("%T", obj),
		"object", client.ObjectKeyFromObject(obj)),
	)

	// Fast path: both selectors are nil, meaning "all clusters, all nodes"
	if dpuClusterSelector == nil && nodeSelector == nil {
		return r.listAllDPUNodes(ctx)
	}

	// Selective path: filter by selectors using remote cache
	return r.selectiveDPUNodeFiltering(ctx, dpuClusterSelector, nodeSelector)
}

// listAllDPUNodes lists all DPUNodes from the host cluster (fast path for nil selectors)
func (r *DPUReadyReconciler) listAllDPUNodes(ctx context.Context) []reconcile.Request {
	log := ctrllog.FromContext(ctx)
	dpuNodeList := &provisioningv1.DPUNodeList{}
	if err := r.List(ctx, dpuNodeList); err != nil {
		log.Error(err, "Failed to list DPUNodes", "objectType")
		return nil
	}

	requests := make([]reconcile.Request, 0, len(dpuNodeList.Items))
	for _, dpuNode := range dpuNodeList.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&dpuNode),
		})
	}
	return requests
}

// selectiveDPUNodeFiltering filters DPUNodes based on DPUClusterSelector and NodeSelector
func (r *DPUReadyReconciler) selectiveDPUNodeFiltering(ctx context.Context, dpuClusterSelector *metav1.LabelSelector, nodeSelector *metav1.LabelSelector) []reconcile.Request {
	log := ctrllog.FromContext(ctx)
	// Get all DPUCluster configs
	dpuClusterConfigs, err := dpucluster.GetConfigs(ctx, r.Client)
	if err != nil {
		log.Error(err, "Failed to get DPUCluster configs")
		return nil
	}

	// Filter to matching clusters
	matchingClusters, err := utils.GetMatchingDPUClusters(dpuClusterConfigs, dpuClusterSelector)
	if err != nil {
		log.Error(err, "Failed to match DPUClusters")
		return nil
	}

	if len(matchingClusters) == 0 {
		log.V(1).Info("No matching DPUClusters found")
		return nil
	}

	dpfOperatorConfig, err := utils.GetDPFOperatorConfig(ctx, r.Client)
	if err != nil {
		log.Error(err, "Failed to get DPFOperatorConfig")
		return nil
	}

	requestMap := make(map[reconcile.Request]bool)
	// For each matching cluster, list nodes with NodeSelector filter
	for _, clusterConfig := range matchingClusters {
		clusterKey := client.ObjectKey{
			Namespace: clusterConfig.Cluster.Namespace,
			Name:      clusterConfig.Cluster.Name,
		}
		log.WithValues("cluster", clusterKey)

		dpuClusterClient, err := r.RemoteCache.GetClient(clusterKey)
		if err != nil {
			log.Error(err, "Failed to get cached client for DPUCluster (cluster may not be connected yet), skipping")
			continue
		}

		nodeList := &corev1.NodeList{}
		listOpts := []client.ListOption{}

		// Apply NodeSelector if specified
		selector, err := utils.LabelSelectorAsSelector(nodeSelector)
		if err != nil {
			log.Error(err, "Failed to parse NodeSelector, skipping cluster")
			continue
		}
		listOpts = append(listOpts, client.MatchingLabelsSelector{Selector: selector})

		if err := dpuClusterClient.List(ctx, nodeList, listOpts...); err != nil {
			log.Error(err, "Failed to list Nodes in DPUCluster, skipping cluster")
			continue
		}

		// Map each node to a DPUNode reconcile request
		for _, node := range nodeList.Items {
			nn, found := extractDPUNodeFromNodeLabels(&node, dpfOperatorConfig.GetNamespace())
			if found {
				requestMap[reconcile.Request{NamespacedName: nn}] = true
			}
		}
	}
	requests := []reconcile.Request{}
	for request := range requestMap {
		requests = append(requests, request)
	}

	return requests
}

// extractSelectors extracts DPUClusterSelector and NodeSelector from a DPUServiceChain or DPUServiceInterface
func extractSelectors(ctx context.Context, obj client.Object) (*metav1.LabelSelector, *metav1.LabelSelector) {
	switch v := obj.(type) {
	case *dpuservicev1.DPUServiceChain:
		return v.GetDPUClusterSelector(), v.GetServiceChainSetLabelSelector()

	case *dpuservicev1.DPUServiceInterface:
		return v.GetDPUClusterSelector(), v.GetServiceInterfaceSetLabelSelector()

	default:
		ctrllog.FromContext(ctx).Error(fmt.Errorf("unexpected object type %T", obj), "Failed to extract selectors")
		return nil, nil
	}
}

// Reconcile handles the reconciliation of DPUNode objects for DPU readiness
func (r *DPUReadyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	dpuNode := provisioningv1.DPUNode{}
	log := ctrllog.FromContext(ctx)
	log.Info("Reconciling DPUNode", "dpuNode", req.NamespacedName)
	if err := r.Get(ctx, req.NamespacedName, &dpuNode); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Return early if the object is getting deleted
	if !dpuNode.ObjectMeta.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	return r.reconcile(ctx, &dpuNode)
}

type reconcileScope struct {
	dpus           []provisioningv1.DPU
	dpuNode        *provisioningv1.DPUNode
	node           *corev1.Node // nil in zero-trust mode
	dpuList        []provisioningv1.DPU
	dpuServiceList *dpuservicev1.DPUServiceList
}

func (r *DPUReadyReconciler) newReconcileScope(ctx context.Context, dpuNode *provisioningv1.DPUNode) (*reconcileScope, error) {
	dpuList, err := r.getDPUsByDPUNode(ctx, dpuNode)
	if err != nil {
		return nil, fmt.Errorf("could not get the DPUs for DPUNode: %w", err)
	}

	dpuServiceList := &dpuservicev1.DPUServiceList{}
	if err := r.Client.List(ctx, dpuServiceList, client.MatchingFields{
		dpuServiceDeployInClusterField: strconv.FormatBool(false),
	}); err != nil {
		return nil, fmt.Errorf("failed to list DPUServices: %w", err)
	}

	// Fetch the corev1.Node only if KubeNodeRef is set (host-trusted mode)
	var node *corev1.Node
	if name := ptr.Deref(dpuNode.Status.KubeNodeRef, ""); name != "" {
		node = &corev1.Node{}
		if err := r.Get(ctx, client.ObjectKey{Name: name}, node); err != nil {
			return nil, fmt.Errorf("failed to get Node %s: %w", name, err)
		}
	}

	return &reconcileScope{
		dpus:           dpuList,
		dpuNode:        dpuNode,
		node:           node,
		dpuList:        dpuList,
		dpuServiceList: dpuServiceList,
	}, nil
}

func (r *DPUReadyReconciler) reconcile(ctx context.Context, dpuNode *provisioningv1.DPUNode) (ctrl.Result, error) {
	scope, err := r.newReconcileScope(ctx, dpuNode)
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileDPUReadyTaints(ctx, scope); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile DPUServices: %w", err)
	}

	if err := r.reconcileDPUNodeMaintenance(ctx, scope); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile DPUNodeMaintenance: %w", err)
	}

	if err := r.reconcileDPUOperationalConditions(ctx, scope); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile DPU operational conditions: %w", err)
	}

	return ctrl.Result{}, nil
}

// reconcileDPUReadyTaints reconciles the DPUServices for a given node
// It returns the list of ready DPUServices
func (r *DPUReadyReconciler) reconcileDPUReadyTaints(ctx context.Context, scope *reconcileScope) error {
	if r.DisableDPUReadyTaints {
		return nil
	}

	// Skip taint operations in zero-trust mode (when there's no corev1.Node)
	if scope.node == nil {
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
	dpuNodeMaintenances, err := r.getDPUNodeMaintenanceObjects(ctx, scope.dpuNode)
	if err != nil {
		return fmt.Errorf("failed to get DPUNodeMaintenance objects: %w", err)
	}

	if len(dpuNodeMaintenances) == 0 {
		return nil
	}

	if err := r.patchDPUNodeMaintenanceObjects(ctx, scope, dpuNodeMaintenances); err != nil {
		return fmt.Errorf("failed to patch DPUNodeMaintenance objects for DPUNode %s: %w", scope.dpuNode.Name, err)
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

func (r *DPUReadyReconciler) getDPUNodeMaintenanceObjects(ctx context.Context, dpuNode *provisioningv1.DPUNode) ([]*provisioningv1.DPUNodeMaintenance, error) {
	// Get all the DPUNodeMaintenance objects related to this DPUNode
	dpuNodeMaintenanceList := &provisioningv1.DPUNodeMaintenanceList{}
	if err := r.Client.List(ctx, dpuNodeMaintenanceList,
		client.MatchingFields{dpuNodeMaintenanceDPUNodeNameField: dpuNode.Name},
	); err != nil {
		return nil, fmt.Errorf("failed to list DPUNodeMaintenance objects: %w", err)
	}

	readyObjects := make([]*provisioningv1.DPUNodeMaintenance, 0, len(dpuNodeMaintenanceList.Items))
	for i := range dpuNodeMaintenanceList.Items {
		if conditions.IsTrue(&dpuNodeMaintenanceList.Items[i], provisioningv1.ConditionNodeEffectApplied) {
			readyObjects = append(readyObjects, &dpuNodeMaintenanceList.Items[i])
		}
	}
	// Return early if there are no DPUNodeMaintenance objects for this DPUNode
	if len(readyObjects) == 0 {
		return nil, nil
	}

	return readyObjects, nil
}

// getDPUsByDPUNode retrieves the DPU objects associated with a given DPUNode
func (r *DPUReadyReconciler) getDPUsByDPUNode(ctx context.Context, dpuNode *provisioningv1.DPUNode) ([]provisioningv1.DPU, error) {
	selector := dpuselector.New(dpuselector.WithIndexerField{FieldName: dpuNodeNameField})
	dpus, err := selector.ListDPUsForNode(ctx, r.Client, dpuNode)
	if err != nil {
		return nil, err
	}

	if len(dpus) == 0 {
		return nil, fmt.Errorf("no DPU found for DPUNode %s", dpuNode.Name)
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
	clusterKey := client.ObjectKey{
		Namespace: dpu.Spec.Cluster.Namespace,
		Name:      dpu.Spec.Cluster.Name,
	}
	dpuClusterClient, err := r.RemoteCache.GetClient(clusterKey)
	if err != nil {
		return nil, fmt.Errorf("could not get cached client for dpucluster: %w", err)
	}

	dpuNode := &corev1.Node{}
	if err := dpuClusterClient.Get(ctx, types.NamespacedName{Name: dpu.Name}, dpuNode); err != nil {
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
	clusterKey := client.ObjectKey{
		Namespace: dpu.Spec.Cluster.Namespace,
		Name:      dpu.Spec.Cluster.Name,
	}
	dpuClusterClient, err := r.RemoteCache.GetClient(clusterKey)
	if err != nil {
		return nil, fmt.Errorf("could not get cached client for dpucluster: %w", err)
	}

	// Get all pods (filtered by label selector in cache config, then filter by node name)
	podList := &corev1.PodList{}
	if err := dpuClusterClient.List(ctx, podList,
		client.MatchingFields{nodeNameField: dpu.Name},
	); err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	dpuNode := &corev1.Node{}
	if err := dpuClusterClient.Get(ctx, client.ObjectKey{Name: dpu.Name}, dpuNode); err != nil {
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
				service.Status.ServiceID == serviceID {
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
	// This is done once per DPUCluster during registration.
	dpfOperatorConfig, err := utils.GetDPFOperatorConfig(ctx, r.Client)
	if err != nil {
		return nil, err
	}
	return dpucluster.NewWatcher(dpucluster.WatcherOptions{
		Name:         "dpuready-pod-watcher",
		Kind:         &corev1.Pod{},
		EventHandler: &podEventHandler{client: c, dpuNodeDefaultNamespace: dpfOperatorConfig.Namespace},
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

// WatchServiceChains sets up watches for ServiceChain objects in DPU clusters
func (r *DPUReadyReconciler) WatchServiceChains(ctx context.Context, c client.Client, cluster client.ObjectKey) (dpucluster.Watcher, error) {
	dpfOperatorConfig, err := utils.GetDPFOperatorConfig(ctx, r.Client)
	if err != nil {
		return nil, err
	}
	return dpucluster.NewWatcher(dpucluster.WatcherOptions{
		Name:         "dpuready-servicechain-watcher",
		Kind:         &dpuservicev1.ServiceChain{},
		EventHandler: &serviceChainEventHandler{client: c, dpuNodeDefaultNamespace: dpfOperatorConfig.Namespace},
		Predicates: []predicate.Predicate{
			newServiceChainReadyPredicate(),
		},
		Watcher: r.controller,
	}), nil
}

// WatchServiceInterfaces sets up watches for ServiceInterface objects in DPU clusters
func (r *DPUReadyReconciler) WatchServiceInterfaces(ctx context.Context, c client.Client, cluster client.ObjectKey) (dpucluster.Watcher, error) {
	dpfOperatorConfig, err := utils.GetDPFOperatorConfig(ctx, r.Client)
	if err != nil {
		return nil, err
	}
	return dpucluster.NewWatcher(dpucluster.WatcherOptions{
		Name:         "dpuready-serviceinterface-watcher",
		Kind:         &dpuservicev1.ServiceInterface{},
		EventHandler: &serviceInterfaceEventHandler{client: c, dpuNodeDefaultNamespace: dpfOperatorConfig.Namespace},
		Predicates: []predicate.Predicate{
			newServiceInterfaceReadyPredicate(),
		},
		Watcher: r.controller,
	}), nil
}

// WatchNodes sets up watches for Node objects in DPU clusters to detect node condition changes
// such as those reported by node-problem-detector
func (r *DPUReadyReconciler) WatchNodes(ctx context.Context, c client.Client, cluster client.ObjectKey) (dpucluster.Watcher, error) {
	dpfOperatorConfig, err := utils.GetDPFOperatorConfig(ctx, r.Client)
	if err != nil {
		return nil, err
	}
	return dpucluster.NewWatcher(dpucluster.WatcherOptions{
		Name:         "dpuready-node-watcher",
		Kind:         &corev1.Node{},
		EventHandler: &nodeInDPUClusterEventHandler{client: c, dpuNodeDefaultNamespace: dpfOperatorConfig.Namespace},
		Predicates: []predicate.Predicate{
			// Only trigger on node condition changes
			newNodeConditionPredicate(),
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
			// Accept create events if the pod is already ready.
			pod := e.Object.(*corev1.Pod)
			return getPodReadyCondition(pod) == corev1.ConditionTrue
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldPod := e.ObjectOld.(*corev1.Pod)
			newPod := e.ObjectNew.(*corev1.Pod)

			// Get readiness status for both old and new pod
			oldReady := getPodReadyCondition(oldPod)
			newReady := getPodReadyCondition(newPod)

			// Only trigger reconciliation if:
			// 1. Pod readiness changed (transition to/from ready state)
			if oldReady != newReady {
				return true
			}

			// 2. Pod deletion timestamp set (pod is being deleted)
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

// getPodReadyCondition returns the status of the PodReady condition
func getPodReadyCondition(pod *corev1.Pod) corev1.ConditionStatus {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady {
			return cond.Status
		}
	}
	return corev1.ConditionUnknown
}

// enqueueDPUNodeFromNodeInDPUCluster retrieves the dpuNode name from the DPU node labels and enqueues a request
// for the corresponding DPUNode.
func enqueueDPUNodeFromNodeInDPUCluster(ctx context.Context, c client.Client, nodeName string, dpuNodeDefaultNamespace string, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	log := ctrllog.FromContext(ctx)

	// Get the node from the DPU cluster
	node := &corev1.Node{}
	if err := c.Get(ctx, client.ObjectKey{Name: nodeName}, node); err != nil {
		log.Error(err, "Failed to get node from DPU cluster", "name", nodeName)
		return
	}

	// Extract DPUNode name and namespace from node labels
	nn, found := extractDPUNodeFromNodeLabels(node, dpuNodeDefaultNamespace)
	if !found {
		log.Error(fmt.Errorf("DPUNode reference to which the node %s in the DPUCluster belongs to not found", node.Name), "Failed to get DPUNode reference for node in DPUCluster")
		return
	}

	// Enqueue request for DPUNode
	enqueueRequests([]reconcile.Request{{NamespacedName: nn}}, q)
}

// podEventHandler is a handler for pod events
type podEventHandler struct {
	client client.Client
	// dpuNodeDefaultNamespace is the default namespace where to look for the DPUNode.
	// Name and namespace labels are expected on dpucluster corev1.Node objects.
	// It can happen that only the name label is present, in which case the default namespace is used.
	dpuNodeDefaultNamespace string
}

func (p *podEventHandler) handleEvent(ctx context.Context, obj client.Object, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	if pod, ok := obj.(*corev1.Pod); ok {
		enqueueDPUNodeFromNodeInDPUCluster(ctx, p.client, pod.Spec.NodeName, p.dpuNodeDefaultNamespace, q)
	} else {
		ctrllog.FromContext(ctx).Error(fmt.Errorf("event expected a Pod but got a %T", obj), "Failed to convert object")
	}
}

func (p *podEventHandler) Create(ctx context.Context, e event.CreateEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	p.handleEvent(ctx, e.Object, q)
}

func (p *podEventHandler) Update(ctx context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	p.handleEvent(ctx, e.ObjectNew, q)
}

func (p *podEventHandler) Delete(ctx context.Context, e event.DeleteEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	p.handleEvent(ctx, e.Object, q)
}

func (p *podEventHandler) Generic(ctx context.Context, e event.GenericEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	// Generic is not implemented
	// because we are not interested in generic events and the predicate filters out all generic events
}

// serviceChainEventHandler is a handler for ServiceChain events
type serviceChainEventHandler struct {
	client client.Client
	// dpuNodeDefaultNamespace is the default namespace where to look for the DPUNode.
	// Name and namespace labels are expected on dpucluster corev1.Node objects.
	// It can happen that only the name label is present, in which case the default namespace is used.
	dpuNodeDefaultNamespace string
}

func (s *serviceChainEventHandler) handleEvent(ctx context.Context, obj client.Object, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	if sc, ok := obj.(*dpuservicev1.ServiceChain); ok {
		if sc.Spec.Node != nil {
			enqueueDPUNodeFromNodeInDPUCluster(ctx, s.client, *sc.Spec.Node, s.dpuNodeDefaultNamespace, q)
		}
	} else {
		ctrllog.FromContext(ctx).Error(fmt.Errorf("event expected a ServiceChain but got a %T", obj), "Failed to convert object")
	}
}

func (s *serviceChainEventHandler) Create(ctx context.Context, e event.CreateEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	s.handleEvent(ctx, e.Object, q)
}

func (s *serviceChainEventHandler) Update(ctx context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	s.handleEvent(ctx, e.ObjectNew, q)
}

func (s *serviceChainEventHandler) Delete(ctx context.Context, e event.DeleteEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	s.handleEvent(ctx, e.Object, q)
}

func (s *serviceChainEventHandler) Generic(ctx context.Context, e event.GenericEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	s.handleEvent(ctx, e.Object, q)
}

// serviceInterfaceEventHandler is a handler for ServiceInterface events
type serviceInterfaceEventHandler struct {
	client client.Client
	// dpuNodeDefaultNamespace is the default namespace where to look for the DPUNode.
	// Name and namespace labels are expected on dpucluster corev1.Node objects.
	// It can happen that only the name label is present, in which case the default namespace is used.
	dpuNodeDefaultNamespace string
}

func (s *serviceInterfaceEventHandler) handleEvent(ctx context.Context, obj client.Object, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	if si, ok := obj.(*dpuservicev1.ServiceInterface); ok {
		if si.Spec.Node != nil {
			enqueueDPUNodeFromNodeInDPUCluster(ctx, s.client, *si.Spec.Node, s.dpuNodeDefaultNamespace, q)
		}
	} else {
		ctrllog.FromContext(ctx).Error(fmt.Errorf("event expected a ServiceInterface but got a %T", obj), "Failed to convert object")
	}
}

func (s *serviceInterfaceEventHandler) Create(ctx context.Context, e event.CreateEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	s.handleEvent(ctx, e.Object, q)
}

func (s *serviceInterfaceEventHandler) Update(ctx context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	s.handleEvent(ctx, e.ObjectNew, q)
}

func (s *serviceInterfaceEventHandler) Delete(ctx context.Context, e event.DeleteEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	s.handleEvent(ctx, e.Object, q)
}

func (s *serviceInterfaceEventHandler) Generic(ctx context.Context, e event.GenericEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	s.handleEvent(ctx, e.Object, q)
}

// nodeInDPUClusterEventHandler is a handler for Node events in DPU clusters
type nodeInDPUClusterEventHandler struct {
	client client.Client
	// dpuNodeDefaultNamespace is the default namespace where to look for the DPUNode.
	dpuNodeDefaultNamespace string
}

func (n *nodeInDPUClusterEventHandler) handleEvent(ctx context.Context, obj client.Object, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	if node, ok := obj.(*corev1.Node); ok {
		enqueueDPUNodeFromNodeInDPUCluster(ctx, n.client, node.Name, n.dpuNodeDefaultNamespace, q)
	} else {
		ctrllog.FromContext(ctx).Error(fmt.Errorf("event expected a Node but got a %T", obj), "Failed to convert object")
	}
}

func (n *nodeInDPUClusterEventHandler) Create(ctx context.Context, e event.CreateEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	n.handleEvent(ctx, e.Object, q)
}

func (n *nodeInDPUClusterEventHandler) Update(ctx context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	n.handleEvent(ctx, e.ObjectNew, q)
}

func (n *nodeInDPUClusterEventHandler) Delete(ctx context.Context, e event.DeleteEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	n.handleEvent(ctx, e.Object, q)
}

func (n *nodeInDPUClusterEventHandler) Generic(ctx context.Context, e event.GenericEvent, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	n.handleEvent(ctx, e.Object, q)
}

// newNodeConditionPredicate creates a predicate that filters node events to only trigger on condition changes
func newNodeConditionPredicate() predicate.Funcs {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			// Accept create events for nodes that have conditions
			node := e.Object.(*corev1.Node)
			return len(node.Status.Conditions) > 0
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldNode := e.ObjectOld.(*corev1.Node)
			newNode := e.ObjectNew.(*corev1.Node)

			// Only trigger reconciliation if node conditions have changed
			return !nodeConditionsEqual(oldNode.Status.Conditions, newNode.Status.Conditions)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Don't reconcile on node deletion
			// TODO(tgiese): handle delete events.
			return false
		},
		GenericFunc: func(e event.GenericEvent) bool {
			// we are not interested in generic events
			return false
		},
	}
}

// newServiceChainReadyPredicate creates a predicate that filters ServiceChain events
// to only trigger on Ready condition changes, avoiding reconciliation bursts on startup
func newServiceChainReadyPredicate() predicate.Funcs {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			// Skip create events to avoid reconciliation burst during initial cache sync
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldSC := e.ObjectOld.(*dpuservicev1.ServiceChain)
			newSC := e.ObjectNew.(*dpuservicev1.ServiceChain)

			// Only trigger if Ready condition changed
			oldReady := meta.IsStatusConditionTrue(oldSC.Status.Conditions, string(conditions.TypeReady))
			newReady := meta.IsStatusConditionTrue(newSC.Status.Conditions, string(conditions.TypeReady))
			return oldReady != newReady
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Reconcile on delete - a chain going away matters
			return true
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
	}
}

// newServiceInterfaceReadyPredicate creates a predicate that filters ServiceInterface events
// to only trigger on Ready condition changes, avoiding reconciliation bursts on startup
func newServiceInterfaceReadyPredicate() predicate.Funcs {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			// Skip create events to avoid reconciliation burst during initial cache sync
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldSI := e.ObjectOld.(*dpuservicev1.ServiceInterface)
			newSI := e.ObjectNew.(*dpuservicev1.ServiceInterface)

			// Only trigger if Ready condition changed
			oldReady := meta.IsStatusConditionTrue(oldSI.Status.Conditions, string(conditions.TypeReady))
			newReady := meta.IsStatusConditionTrue(newSI.Status.Conditions, string(conditions.TypeReady))
			return oldReady != newReady
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Reconcile on delete - an interface going away matters
			return true
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
	}
}
