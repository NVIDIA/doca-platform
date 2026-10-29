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
	"strings"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/dpucluster"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	schedulingv1 "k8s.io/component-helpers/scheduling/corev1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list
// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=dpuservices,verbs=get;list;watch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpus,verbs=get;list;watch

type DPUReadyController struct {
	client.Client
	Scheme *runtime.Scheme
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
)

// isCriticalDPUServicePresent checks if there are any critical DPU services defined in the cluster
func (c *DPUReadyController) isCriticalDPUServicePresent(ctx context.Context,
	criticalDPUServices *dpuservicev1.DPUServiceList) (bool, error) {
	// get the list of services that ideally must be running on the node and have a critical label

	if err := c.List(ctx, criticalDPUServices, client.MatchingLabels{criticalDPUServiceLabel: ""}); err != nil {
		return false, fmt.Errorf("failed to list critical DPUServices: %w", err)
	}

	return len(criticalDPUServices.Items) > 0, nil
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

// getDPUByNodeName retrieves the DPU object associated with a given x86 host node name
func (c *DPUReadyController) getDPUByNodeName(ctx context.Context, nodeName string) (*provisioningv1.DPU, error) {
	dpuList := &provisioningv1.DPUList{}
	listOpts := []client.ListOption{
		client.MatchingFields{"spec.dpuNodeName": nodeName},
	}
	if err := c.Client.List(ctx, dpuList, listOpts...); err != nil {
		return nil, err
	}
	for _, dpu := range dpuList.Items {
		if dpu.Spec.DPUNodeName == nodeName {
			return &dpu, nil
		}
	}

	if len(dpuList.Items) == 0 {
		return nil, fmt.Errorf("no DPU found for node name %s", nodeName)
	}

	// Assuming there's only one DPU per node name, change this for multi-DPU support
	return &dpuList.Items[0], nil
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

// areAllServicesRunning checks if all critical DPU services are running on the DPU node.
// It connects to the DPU cluster and verifies that all critical service pods are truly ready.
// Returns true only if all critical services have ready pods running on the DPU.
func (c *DPUReadyController) areAllServicesRunning(ctx context.Context, hostNode *corev1.Node,
	criticalDPUServices *dpuservicev1.DPUServiceList) (bool, error) {

	dpu, err := c.getDPUByNodeName(ctx, hostNode.Name)
	if err != nil {
		return false, fmt.Errorf("could not get the dpunode from node: %w", err)
	}

	log := ctrllog.FromContext(ctx)
	dpuClusterClient, _, err := dpucluster.K8sClusterToDPUClusterConfig(c.Client, &(dpu.Spec.Cluster)).Clientset(ctx)
	if err != nil {
		return false, fmt.Errorf("could not get the kubeconfig for the dpucluster: %w", err)
	}

	// get all running pods on the dpuNode
	podList, err := dpuClusterClient.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=%s", dpu.Name),
	})
	if err != nil {
		return false, fmt.Errorf("failed to list pods on the node %s: %w", dpu.Name, err)
	}

	dpuNode, err := dpuClusterClient.CoreV1().Nodes().Get(ctx, dpu.Name, metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("failed to get the dpu node %s: %w", dpu.Name, err)
	}

	for _, service := range criticalDPUServices.Items {
		if service.Spec.ServiceDaemonSet != nil {
			matches, err := nodeMatchesNodeSelector(dpuNode, service.Spec.ServiceDaemonSet.NodeSelector)
			if err != nil {
				return false, fmt.Errorf("failed to match node selector for service %s: %w",
					service.Name, err)
			}
			if !matches {
				// service should not be running on this node, continue to the next service
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
			log.Info("critical service pod not found",
				"service", service.Name)
			return false, nil
		}
		if !isPodRunning(servicePod) {
			log.Info("critical service pod not ready",
				"service", service.Name,
				"pod", servicePod.Name)
			return false, nil
		}
	}
	return true, nil
}

// patchNode applies a strategic merge patch to update the node's taints
func (c *DPUReadyController) patchNode(ctx context.Context, taints []corev1.Taint, node *corev1.Node) error {
	originalNode := node.DeepCopy()
	node.Spec.Taints = taints
	patch := client.StrategicMergeFrom(originalNode)
	if err := c.Client.Patch(ctx, node, patch); err != nil {
		return fmt.Errorf("failed to patch node %s: %w", node.Name, err)
	}
	return nil
}

// addTaintIfDoesNotExist adds the DPU-ready taint to a node if it doesn't already exist
func (c *DPUReadyController) addTaintIfDoesNotExist(ctx context.Context, node *corev1.Node) error {
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
	return c.patchNode(ctx, taints, node)
}

// removeTaintIfExists removes the DPU-ready taint from a node if it exists
func (c *DPUReadyController) removeTaintIfExists(ctx context.Context, node *corev1.Node) error {
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
	return c.patchNode(ctx, newTaints, node)
}

// Reconcile handles the reconciliation of Node objects for DPU readiness
func (c *DPUReadyController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var (
		node                  corev1.Node
		nodeReady             bool
		criticalServiceExists bool
		err                   error
	)
	log := ctrllog.FromContext(ctx)
	log.Info("Reconciling node", "node", req.Name)
	if err = c.Get(ctx, req.NamespacedName, &node); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	criticalDPUServicesList := &dpuservicev1.DPUServiceList{}
	// Check if there are any critical DPU services
	criticalServiceExists, err = c.isCriticalDPUServicePresent(ctx, criticalDPUServicesList)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !criticalServiceExists {
		// remove previously added taints
		if err = c.removeTaintIfExists(ctx, &node); err != nil {
			return ctrl.Result{},
				fmt.Errorf("error removing taint from node %s: %w",
					node.Name, err)
		}
		return ctrl.Result{}, nil
	}

	if nodeReady, err = c.areAllServicesRunning(ctx, &node, criticalDPUServicesList); err != nil {
		return ctrl.Result{}, err
	}

	if nodeReady {
		if err = c.removeTaintIfExists(ctx, &node); err != nil {
			return ctrl.Result{},
				fmt.Errorf("error removing taint from node %s: %w",
					node.Name, err)
		}
		return ctrl.Result{}, nil
	}

	if err = c.addTaintIfDoesNotExist(ctx, &node); err != nil {
		return ctrl.Result{},
			fmt.Errorf("error adding taint to node %s: %w",
				node.Name, err)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager configures the controller with the manager and sets up required indexes and predicates
func (c *DPUReadyController) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	// Set up the index for DPU lookups by node name
	if err := mgr.GetFieldIndexer().IndexField(
		ctx,
		&provisioningv1.DPU{},
		"spec.dpuNodeName",
		func(obj client.Object) []string {
			dpu := obj.(*provisioningv1.DPU)
			return []string{dpu.Spec.DPUNodeName}
		},
	); err != nil {
		return err
	}

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

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Node{}, builder.WithPredicates(p)).
		Complete(c)
}
