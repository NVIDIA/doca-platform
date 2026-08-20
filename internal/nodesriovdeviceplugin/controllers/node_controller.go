/*
Copyright 2026 NVIDIA

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

	noderesourcesv1 "github.com/nvidia/doca-platform/api/noderesources/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/nodesriovdeviceplugin/common"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/utils/predicates"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/flowcontrol"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete,namespace=system
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch,namespace=system
// +kubebuilder:rbac:groups="",resources=configmaps/finalizers,verbs=update
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpus;dpunodes,verbs=get;list;watch
// +kubebuilder:rbac:groups=noderesources.dpu.nvidia.com,resources=nodesriovdevicepluginconfigs,verbs=get;list;watch

// NodeReconciler reconciles Node objects and manages per-node SRIOV device plugin pods.
type NodeReconciler struct {
	client.Client
	Scheme             *runtime.Scheme
	Namespace          string
	OwnerConfigMapName string
	DevicePluginConfig DevicePluginConfig
	// FailedPodsBackoff is used to avoid hot-looping when pods fail immediately after creation.
	FailedPodsBackoff *flowcontrol.Backoff
}

// Reconcile handles the reconciliation logic for a Node.
func (r *NodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)
	log.Info("Reconciling")

	defer func() {
		log.Info("Finished reconciling")
	}()

	// Get the Node.
	node := &corev1.Node{}
	if err := r.Client.Get(ctx, req.NamespacedName, node); err != nil {
		if apierrors.IsNotFound(err) {
			// Node was deleted, clean up any managed pods.
			return r.reconcileDelete(ctx, req.Name)
		}
		return ctrl.Result{}, err
	}

	// Return early if the node is getting deleted.
	if !node.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, node.Name)
	}

	return r.reconcile(ctx, node)
}

// reconcile handles the main reconciliation logic.
func (r *NodeReconciler) reconcile(ctx context.Context, node *corev1.Node) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)

	// Find all target DPUs for this node (DPUs that have annotation pointing to a config).
	targetDPUs, err := r.getTargetDPUsForNode(ctx, node.Name)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get target DPUs for node: %w", err)
	}

	// If no target DPUs, ensure no pod exists.
	if len(targetDPUs) == 0 {
		log.V(1).Info("No target DPUs for node, ensuring no managed pod exists")
		return r.ensureNoPod(ctx, node.Name)
	}

	// Build the input config for the init container.
	inputConfigResult, err := r.buildInputConfig(ctx, targetDPUs)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to build input config: %w", err)
	}

	// If input config is empty (all DPUs had errors or missing config), ensure no pod exists.
	if len(inputConfigResult.inputConfig) == 0 {
		log.Info("Empty input config after building, ensuring no managed pod exists")
		return r.ensureNoPod(ctx, node.Name)
	}

	ownerRef, err := r.getOwnerReferencesForPod(ctx)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get owner references for pod: %w", err)
	}

	// Build desired pod.
	desiredPod, err := buildDesiredPod(
		node.Name, r.Namespace, inputConfigResult.inputConfig,
		&r.DevicePluginConfig, ownerRef)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to build desired pod: %w", err)
	}

	// Get existing managed pods for this node.
	existingPods, err := r.getManagedPodsForNode(ctx, node.Name)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get managed pods for node: %w", err)
	}

	// Handle pod lifecycle.
	return r.reconcilePod(ctx, node.Name, desiredPod, existingPods)
}

// reconcileDelete handles cleanup when a node is deleted.
func (r *NodeReconciler) reconcileDelete(ctx context.Context, nodeName string) (ctrl.Result, error) {
	return r.ensureNoPod(ctx, nodeName)
}

// ensureNoPod deletes any managed pods for the given node.
func (r *NodeReconciler) ensureNoPod(ctx context.Context, nodeName string) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)

	pods, err := r.getManagedPodsForNode(ctx, nodeName)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get managed pods for node: %w", err)
	}

	for _, pod := range pods {
		log.Info("Deleting managed pod", "pod", pod.Name)
		if err := r.deletePod(ctx, pod); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// reconcilePod reconciles the pod state for a node.
func (r *NodeReconciler) reconcilePod(ctx context.Context, nodeName string,
	desiredPod *corev1.Pod, existingPods []*corev1.Pod) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)

	// Handle multiple pods (unexpected state).
	if len(existingPods) > 1 {
		log.Info("Multiple managed pods found, deleting all and recreating")
		for _, pod := range existingPods {
			if err := r.deletePod(ctx, pod); err != nil {
				return ctrl.Result{}, err
			}
		}
		// we are watching for delete events, so no need to requeue explicitly
		return ctrl.Result{}, nil
	}

	// No existing pod, create one.
	if len(existingPods) == 0 {
		log.Info("Creating managed pod", "pod", desiredPod.Name)
		if err := r.Client.Create(ctx, desiredPod); err != nil && !apierrors.IsAlreadyExists(err) {
			return ctrl.Result{}, fmt.Errorf("failed to create pod: %w", err)
		}
		// the pod was created or already exists, waiting for create event to validate pod spec
		return ctrl.Result{}, nil
	}

	// Single existing pod, check if it needs to be updated.
	existingPod := existingPods[0]

	if isPodInTerminalState(existingPod) {
		return r.handlePodInTerminalState(ctx, nodeName, existingPod)
	}

	// Check if pod is outdated and needs recreation.
	if isPodOutdated(existingPod, desiredPod) {
		log.Info("Pod is outdated, deleting and recreating", "pod", existingPod.Name)
		if err := r.deletePod(ctx, existingPod); err != nil {
			return ctrl.Result{}, err
		}
		// we are watching for delete events, so no need to requeue explicitly
		return ctrl.Result{}, nil
	}

	log.V(1).Info("Pod spec is up to date", "pod", existingPod.Name)
	return ctrl.Result{}, nil
}

// deletePod deletes a pod if it is not already being deleted.
func (r *NodeReconciler) deletePod(ctx context.Context, pod *corev1.Pod) error {
	if !pod.ObjectMeta.DeletionTimestamp.IsZero() {
		return nil
	}
	if err := r.Client.Delete(ctx, pod); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete pod %s: %w", pod.Name, err)
	}
	return nil
}

// handlePodInTerminalState handles a failed pod with backoff to avoid hot-looping.
func (r *NodeReconciler) handlePodInTerminalState(ctx context.Context, nodeName string,
	pod *corev1.Pod) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)

	now := r.FailedPodsBackoff.Clock.Now()
	if r.FailedPodsBackoff.IsInBackOffSinceUpdate(nodeName, now) {
		delay := r.FailedPodsBackoff.Get(nodeName)
		log.Info("Deleting failed pod is limited by backoff",
			"pod", pod.Name, "node", nodeName, "delay", delay)
		return ctrl.Result{RequeueAfter: delay}, nil
	}

	r.FailedPodsBackoff.Next(nodeName, now)

	log.Info("Pod is in failed state, deleting and recreating", "pod", pod.Name)
	if err := r.deletePod(ctx, pod); err != nil {
		return ctrl.Result{}, err
	}
	// we watch for delete events, so no need to requeue explicitly
	return ctrl.Result{}, nil
}

// getTargetDPUsForNode finds all DPUs attached to a node that have the config annotation.
func (r *NodeReconciler) getTargetDPUsForNode(ctx context.Context, nodeName string) ([]*provisioningv1.DPU, error) {
	log := ctrllog.FromContext(ctx)
	// First, find all DPUNodes that reference this Kubernetes node.
	dpuNodeList := &provisioningv1.DPUNodeList{}
	if err := r.Client.List(ctx, dpuNodeList,
		client.InNamespace(r.Namespace),
		client.MatchingFields{dpuNodeKubeNodeRefField: nodeName},
	); err != nil {
		return nil, fmt.Errorf("failed to list DPUNodes: %w", err)
	}

	if len(dpuNodeList.Items) == 0 {
		return nil, nil
	}

	// There should be at most one DPUNode per Kubernetes node.
	if len(dpuNodeList.Items) > 1 {
		dpuNodeNames := make([]string, len(dpuNodeList.Items))
		for i, dn := range dpuNodeList.Items {
			dpuNodeNames[i] = dn.Name
		}
		return nil, fmt.Errorf("found multiple DPUNodes referencing node %s: %v", nodeName, dpuNodeNames)
	}

	dpuNode := &dpuNodeList.Items[0]

	if !dpuNode.DeletionTimestamp.IsZero() {
		return nil, nil
	}

	// Find all DPUs associated with this DPUNode.
	dpuList := &provisioningv1.DPUList{}
	if err := r.Client.List(ctx, dpuList,
		client.InNamespace(r.Namespace),
		client.MatchingFields{dpuNodeNameField: dpuNode.Name},
	); err != nil {
		return nil, fmt.Errorf("failed to list DPUs for DPUNode %s: %w", dpuNode.Name, err)
	}

	targetDPUs := make([]*provisioningv1.DPU, 0, len(dpuList.Items))
	for i := range dpuList.Items {
		dpu := &dpuList.Items[i]

		if !dpu.DeletionTimestamp.IsZero() {
			continue
		}
		// Check if DPU has non empty config annotation.
		if configName := dpu.Annotations[DPUDevicePluginConfigAnnotationKey]; configName == "" {
			continue
		}

		// Validate DPU has serial number.
		if dpu.Spec.SerialNumber == "" {
			continue
		}

		// Check if DPU has HostNetworkReady condition set to true.
		// Note: at the moment DPUCondHostNetworkReady do not contain ObservedGeneration,
		cond := conditions.Get(dpu, conditions.ConditionType(provisioningv1.DPUCondHostNetworkReady))
		if cond == nil || cond.Status != metav1.ConditionTrue {
			log.Info("DPU HostNetworkReady condition is not true, skipping", "dpu", dpu.Name, "node", nodeName)
			continue
		}

		targetDPUs = append(targetDPUs, dpu)
	}

	return targetDPUs, nil
}

// buildInputConfigResult holds the result of building the input config.
type buildInputConfigResult struct {
	// inputConfig is the per-node input config for the init container.
	inputConfig common.NodeInputConfig
}

// buildInputConfig constructs the per-node input config from target DPUs.
func (r *NodeReconciler) buildInputConfig(ctx context.Context, targetDPUs []*provisioningv1.DPU) (*buildInputConfigResult, error) {
	log := ctrllog.FromContext(ctx)
	inputConfig := make(common.NodeInputConfig)

	for _, dpu := range targetDPUs {
		configName := dpu.Annotations[DPUDevicePluginConfigAnnotationKey]
		config := &noderesourcesv1.NodeSRIOVDevicePluginConfig{}
		if err := r.Client.Get(ctx, types.NamespacedName{
			Namespace: r.Namespace,
			Name:      configName,
		}, config); err != nil {
			if apierrors.IsNotFound(err) {
				log.Info("NodeSRIOVDevicePluginConfig not found, skipping DPU",
					"config", configName, "dpu", dpu.Name)
				continue
			}
			return nil, fmt.Errorf("failed to get config %s for DPU %s: %w", configName, dpu.Name, err)
		}

		if errList := common.ValidateNodeSRIOVDevicePluginConfig(r.DevicePluginConfig.DefaultResourcePrefix, config); len(errList) != 0 {
			log.Error(errList.ToAggregate(), "NodeSRIOVDevicePluginConfig is not valid, skipping DPU",
				"config", configName, "dpu", dpu.Name)
			continue
		}

		// Add to input config.
		inputConfig[dpu.Spec.SerialNumber] = config.Spec.DevicePluginResources
	}

	if errList := common.ValidateCrossDPUResourceTypes(r.DevicePluginConfig.DefaultResourcePrefix, inputConfig); len(errList) != 0 {
		return nil, fmt.Errorf("conflicting resource types across DPUs: %w", errList.ToAggregate())
	}

	return &buildInputConfigResult{
		inputConfig: inputConfig,
	}, nil
}

// getManagedPodsForNode retrieves all pods managed by this controller for a given node.
func (r *NodeReconciler) getManagedPodsForNode(ctx context.Context, nodeName string) ([]*corev1.Pod, error) {
	podList := &corev1.PodList{}
	if err := r.Client.List(ctx, podList,
		client.InNamespace(r.Namespace),
		client.MatchingLabels{ManagedByLabelKey: ManagedByLabelValue},
		client.MatchingFields{podTargetNodeField: nodeName},
	); err != nil {
		return nil, err
	}

	pods := make([]*corev1.Pod, len(podList.Items))
	for i := range podList.Items {
		pods[i] = &podList.Items[i]
	}
	return pods, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// NOTE: When adding or changing watches here, also update cache configuration in manager_options.go
	// to match.
	bldr := ctrl.NewControllerManagedBy(mgr).
		Named(nodeControllerName).
		For(&corev1.Node{}, builder.WithPredicates(
			predicates.PredicateFuncsByEventTypes(event.CreateEvent{}, event.DeleteEvent{}))).
		Watches(&provisioningv1.DPU{},
			handler.EnqueueRequestsFromMapFunc(r.dpuToNode),
			builder.WithPredicates(predicate.Or(
				predicate.AnnotationChangedPredicate{},
				dpuHostNetworkReadyChangedPredicate(),
			))).
		Watches(&provisioningv1.DPUNode{},
			handler.EnqueueRequestsFromMapFunc(r.dpuNodeToNode),
			builder.WithPredicates(dpuNodeKubeNodeRefChangedPredicate())).
		Watches(&corev1.Pod{}, handler.EnqueueRequestsFromMapFunc(r.podToNode)).
		Watches(
			&noderesourcesv1.NodeSRIOVDevicePluginConfig{},
			handler.EnqueueRequestsFromMapFunc(r.configToNodes),
		)
	if r.OwnerConfigMapName != "" {
		bldr = bldr.Watches(&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(r.anyToAllNodes),
			builder.WithPredicates(predicate.NewPredicateFuncs(func(obj client.Object) bool {
				return obj.GetNamespace() == r.Namespace && obj.GetName() == r.OwnerConfigMapName
			})),
		)
	}
	return bldr.Complete(r)
}

// dpuNodeKubeNodeRefChangedPredicate returns a predicate that filters DPUNode events
// to only trigger on create, delete, generic, or updates where Status.KubeNodeRef changed.
func dpuNodeKubeNodeRefChangedPredicate() predicate.Funcs {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldDPUNode, ok1 := e.ObjectOld.(*provisioningv1.DPUNode)
			newDPUNode, ok2 := e.ObjectNew.(*provisioningv1.DPUNode)
			if !ok1 || !ok2 {
				return false
			}
			// Check if KubeNodeRef changed.
			oldRef := oldDPUNode.Status.KubeNodeRef
			newRef := newDPUNode.Status.KubeNodeRef
			if oldRef == nil && newRef == nil {
				return false
			}
			if oldRef == nil || newRef == nil {
				return true
			}
			return *oldRef != *newRef
		},
	}
}

// dpuHostNetworkReadyChangedPredicate returns a predicate that filters DPU events
// to only trigger on create, delete, generic, or updates where HostNetworkReady condition changed.
func dpuHostNetworkReadyChangedPredicate() predicate.Funcs {
	condType := conditions.ConditionType(provisioningv1.DPUCondHostNetworkReady)
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldDPU, ok1 := e.ObjectOld.(*provisioningv1.DPU)
			newDPU, ok2 := e.ObjectNew.(*provisioningv1.DPU)
			if !ok1 || !ok2 {
				return false
			}
			oldCond := conditions.Get(oldDPU, condType)
			newCond := conditions.Get(newDPU, condType)
			oldReady := oldCond != nil && oldCond.Status == metav1.ConditionTrue
			newReady := newCond != nil && newCond.Status == metav1.ConditionTrue
			return oldReady != newReady
		},
	}
}

// dpuToNode maps DPU events to the Kubernetes Node they are attached to.
func (r *NodeReconciler) dpuToNode(ctx context.Context, obj client.Object) []reconcile.Request {
	log := ctrllog.FromContext(ctx)
	dpu, ok := obj.(*provisioningv1.DPU)
	if !ok {
		log.Error(nil, "failed to convert object to DPU, bad type")
		return nil
	}

	// Find the DPUNode for this DPU.
	dpuNode := &provisioningv1.DPUNode{}
	if err := r.Client.Get(ctx, types.NamespacedName{
		Namespace: dpu.Namespace,
		Name:      dpu.Spec.DPUNodeName,
	}, dpuNode); err != nil {
		if !apierrors.IsNotFound(err) {
			log.Error(err, "failed to get DPUNode", "dpuNodeName", dpu.Spec.DPUNodeName)
		}
		return nil
	}

	return r.dpuNodeToNode(ctx, dpuNode)
}

// dpuNodeToNode maps DPUNode events to the Kubernetes Node.
func (r *NodeReconciler) dpuNodeToNode(ctx context.Context, obj client.Object) []reconcile.Request {
	log := ctrllog.FromContext(ctx)
	dpuNode, ok := obj.(*provisioningv1.DPUNode)
	if !ok {
		log.Error(nil, "failed to convert object to DPUNode, bad type")
		return nil
	}

	if dpuNode.Status.KubeNodeRef == nil || *dpuNode.Status.KubeNodeRef == "" {
		return nil
	}

	return []reconcile.Request{
		{NamespacedName: types.NamespacedName{Name: *dpuNode.Status.KubeNodeRef}},
	}
}

// anyToAllNodes reconcile all nodes
func (r *NodeReconciler) anyToAllNodes(ctx context.Context, obj client.Object) []reconcile.Request {
	log := ctrllog.FromContext(ctx)
	nodeList := &corev1.NodeList{}
	if err := r.Client.List(ctx, nodeList); err != nil {
		log.Error(err, "failed to list nodes")
		return nil
	}
	requests := make([]reconcile.Request, 0, len(nodeList.Items))
	for _, node := range nodeList.Items {
		requests = append(requests, reconcile.Request{NamespacedName: types.NamespacedName{Name: node.Name}})
	}
	return requests
}

// podToNode maps Pod events to the Node they are scheduled on.
func (r *NodeReconciler) podToNode(ctx context.Context, obj client.Object) []reconcile.Request {
	log := ctrllog.FromContext(ctx)
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		log.Error(nil, "failed to convert object to Pod, bad type")
		return nil
	}

	// Only process pods managed by this controller.
	if pod.Labels[ManagedByLabelKey] != ManagedByLabelValue {
		return nil
	}

	// Get the target node from the pod's affinity.
	nodeName := getTargetNodeFromPod(pod)
	if nodeName == "" {
		return nil
	}

	return []reconcile.Request{
		{NamespacedName: types.NamespacedName{Name: nodeName}},
	}
}

// configToNodes maps NodeSRIOVDevicePluginConfig events to all nodes that reference it.
func (r *NodeReconciler) configToNodes(ctx context.Context, obj client.Object) []reconcile.Request {
	log := ctrllog.FromContext(ctx)
	config, ok := obj.(*noderesourcesv1.NodeSRIOVDevicePluginConfig)
	if !ok {
		log.Error(nil, "failed to convert object to NodeSRIOVDevicePluginConfig, bad type")
		return nil
	}

	// Find all DPUs that reference this config.
	dpuList := &provisioningv1.DPUList{}
	if err := r.Client.List(ctx, dpuList, client.InNamespace(r.Namespace)); err != nil {
		log.Error(err, "failed to list DPUs")
		return nil
	}

	nodeNames := make(map[string]struct{})
	for _, dpu := range dpuList.Items {
		configName := dpu.Annotations[DPUDevicePluginConfigAnnotationKey]
		if configName != config.Name {
			continue
		}

		// Get the node for this DPU.
		dpuNode := &provisioningv1.DPUNode{}
		if err := r.Client.Get(ctx, types.NamespacedName{
			Namespace: dpu.Namespace,
			Name:      dpu.Spec.DPUNodeName,
		}, dpuNode); err != nil {
			continue
		}
		if dpuNode.Status.KubeNodeRef != nil && *dpuNode.Status.KubeNodeRef != "" {
			nodeNames[*dpuNode.Status.KubeNodeRef] = struct{}{}
		}
	}

	requests := make([]reconcile.Request, 0, len(nodeNames))
	for nodeName := range nodeNames {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: nodeName},
		})
	}
	return requests
}

// getOwnerReferencesForPod retrieves the owner references to be set on managed Pods.
// This ensures that Pods managed by this controller are cleaned up during uninstallation.
// Since there is no suitable CR to attach, a dedicated ConfigMap serves as the owner.
// The ConfigMap should exist for the controller's lifetime and be deleted during uninstallation.
func (r *NodeReconciler) getOwnerReferencesForPod(ctx context.Context) ([]metav1.OwnerReference, error) {
	if r.OwnerConfigMapName == "" {
		return []metav1.OwnerReference{}, nil
	}
	configMap := &corev1.ConfigMap{}
	if err := r.Client.Get(ctx, types.NamespacedName{
		Namespace: r.Namespace,
		Name:      r.OwnerConfigMapName,
	}, configMap); err != nil {
		return nil, fmt.Errorf("failed to get ConfigMap %s/%s: %w", r.Namespace, r.OwnerConfigMapName, err)
	}

	ownerRef := metav1.NewControllerRef(configMap, corev1.SchemeGroupVersion.WithKind("ConfigMap"))
	return []metav1.OwnerReference{*ownerRef}, nil
}
