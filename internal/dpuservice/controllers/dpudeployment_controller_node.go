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
	"github.com/nvidia/doca-platform/internal/utils"
	"github.com/nvidia/doca-platform/pkg/conditions"

	"github.com/fluxcd/pkg/runtime/patch"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	dpuDeploymentNodeControllerName = "dpudeploymentnodecontroller"
)

// pauseDPUDeploymentNodeReconciler pauses the DPUNodeMaintenance Reconciler associated with DPUDeployments
// by doing noop reconciliation loops. This is helpful to make tests faster and less complex
var pauseDPUDeploymentNodeReconciler bool

// +kubebuilder:rbac:groups=svc.dpu.nvidia.com,resources=dpudeployments;dpuservices,verbs=get;list;watch
// +kubebuilder:rbac:groups=provisioning.dpu.nvidia.com,resources=dpunodemaintenances;dpunodes,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch;update;patch

// DPUDeploymentNodeReconciler reconciles Node Objects that are affected by a DPUDeployment
type DPUDeploymentNodeReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// SetupWithManager sets up the controller with the Manager.
func (r *DPUDeploymentNodeReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named(dpuDeploymentNodeControllerName).
		For(&corev1.Node{}).
		Watches(&provisioningv1.DPUNodeMaintenance{}, handler.EnqueueRequestsFromMapFunc(r.DPUNodeMaintenanceToNode)).
		Watches(&dpuservicev1.DPUDeployment{}, handler.EnqueueRequestsFromMapFunc(r.DPUDeploymentToNode)).
		Complete(r)
}

// DPUNodeMaintenanceToNode maps DPUNodeMaintenance objects to the corev1.Node that they are referencing
func (r *DPUDeploymentNodeReconciler) DPUNodeMaintenanceToNode(ctx context.Context, o client.Object) []ctrl.Request {
	log := ctrllog.FromContext(ctx)
	result := []ctrl.Request{}
	dpuNodeMaintenance, ok := o.(*provisioningv1.DPUNodeMaintenance)
	if !ok {
		log.Error(nil, "failed to convert object to DPUNodeMaintenance, bad type")
		return nil
	}

	if !conditions.IsTrue(dpuNodeMaintenance, provisioningv1.ConditionNodeEffectApplied) {
		return result

	}

	result = append(result, ctrl.Request{NamespacedName: types.NamespacedName{Name: dpuNodeMaintenance.Spec.DPUNodeName}})
	return result
}

// DPUDeploymentToNode maps DPUDeployments to corev1.Nodes via the provisioningv1.DPUNode that should be reconciled in
// order to add the initial label for in cluster DPUServices if that doesn't exist and also to cleanup the labels for
// terminating DPUDeployments.
func (r *DPUDeploymentNodeReconciler) DPUDeploymentToNode(ctx context.Context, o client.Object) []ctrl.Request {
	log := ctrllog.FromContext(ctx)
	result := []ctrl.Request{}
	dpuDeployment, ok := o.(*dpuservicev1.DPUDeployment)
	if !ok {
		log.Error(nil, "failed to convert object to DPUDeployment, bad type")
		return nil
	}

	// Skip non terminating DPUDeployments or DPUDeployments that do not have DPUServiceReconciled condition true for the
	// current generation
	isTerminating := !dpuDeployment.DeletionTimestamp.IsZero()
	hasSuccessfullyReconciledDPUServices := dpuDeployment.DeletionTimestamp.IsZero() &&
		conditions.IsTrue(dpuDeployment, dpuservicev1.ConditionDPUServicesReconciled) &&
		conditions.Get(dpuDeployment, dpuservicev1.ConditionDPUServicesReconciled).ObservedGeneration == dpuDeployment.Generation
	if !isTerminating && !hasSuccessfullyReconciledDPUServices {
		return nil
	}

	// Find the matching corev1.Nodes based on what the DPUDeployment targets
	matchingNodes, err := getDPUDeploymentMatchingNodeNames(ctx, r.Client, dpuDeployment)
	if err != nil {
		log.Error(err, fmt.Sprintf("failed to find matching corev1.Nodes for DPUDeployment %v", client.ObjectKeyFromObject(dpuDeployment)))
		return nil
	}

	for _, matchingNode := range matchingNodes {
		result = append(result, ctrl.Request{NamespacedName: types.NamespacedName{Name: matchingNode}})
	}

	return result
}

// getDPUDeploymentMatchingNodeNames returns the names of the corev1.Nodes that a DPUDeployment matches by checking the
// DPUSet NodeSelectors against the provisioningv1.DPUNodes they target.
func getDPUDeploymentMatchingNodeNames(ctx context.Context, c client.Client, dpuDeployment *dpuservicev1.DPUDeployment) ([]string, error) {
	var matchingNodes []string
	for _, dpuSet := range dpuDeployment.Spec.DPUs.DPUSets {
		labelSelector, err := utils.LabelSelectorAsSelector(dpuSet.NodeSelector)
		if err != nil {
			return nil, fmt.Errorf("failed to parse label selector from nodeSelector found in dpudeployment.spec.dpus.dpusets: %w", err)
		}

		dpuNodeList := &provisioningv1.DPUNodeList{}
		if err := c.List(ctx, dpuNodeList, client.MatchingLabelsSelector{Selector: labelSelector}); err != nil {
			return nil, fmt.Errorf("failed to list DPUNodes: %w", err)
		}

		for _, dpuNode := range dpuNodeList.Items {
			// If we don't have this field set, it means that the we are not in the Host Trusted use case, where
			// in cluster disruptive upgrades are not applicable.
			if dpuNode.Status.KubeNodeRef == nil {
				continue
			}
			matchingNodes = append(matchingNodes, *dpuNode.Status.KubeNodeRef)
		}
	}

	slices.Sort(matchingNodes)
	uniqueMatchingNodes := slices.Compact(matchingNodes)
	return uniqueMatchingNodes, nil
}

func (r *DPUDeploymentNodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	log := ctrllog.FromContext(ctx)
	if pauseDPUDeploymentNodeReconciler {
		log.Info("noop reconciliation")
		return ctrl.Result{}, nil
	}

	log.Info("Reconciling")

	defer func() {
		log.Info("Finished reconciling")
	}()

	node := &corev1.Node{}
	if err := r.Client.Get(ctx, req.NamespacedName, node); err != nil {
		if apierrors.IsNotFound(err) {
			// Return early if the object is not found.
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Return early if the object is getting deleted
	if !node.ObjectMeta.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	return r.reconcile(ctx, node)
}

// reconcile handles the main reconciliation loop
//
//nolint:unparam
func (r *DPUDeploymentNodeReconciler) reconcile(ctx context.Context, node *corev1.Node) (ctrl.Result, error) {
	originalNode := node.DeepCopy()

	// Get all the DPUServices that are created by a DPUDeployment
	// TODO: Improve filtering to target only current services for completeness
	dpuServiceList := &dpuservicev1.DPUServiceList{}
	if err := r.Client.List(ctx,
		dpuServiceList,
		client.HasLabels{dpuservicev1.ParentDPUDeploymentNameLabel},
		client.MatchingFields{dpuServiceDeployInClusterField: strconv.FormatBool(true)}); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list DPUServices: %w", err)
	}

	// Get all DPUDeployments
	dpuDeploymentList := &dpuservicev1.DPUDeploymentList{}
	if err := r.Client.List(ctx, dpuDeploymentList); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list DPUDeployments: %w", err)
	}

	// Get all the DPUNodeMaintenance objects related to this node
	dpuNodeMaintenanceList := &provisioningv1.DPUNodeMaintenanceList{}
	if err := r.Client.List(ctx, dpuNodeMaintenanceList, client.MatchingFields{dpuNodeMaintenanceDPUNodeNameField: node.Name}); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list DPUNodeMaintenance objects: %w", err)
	}

	// Create a map holding a deep copy of the original dpuNodeMaintenance objects
	originalDPUNodeMaintenance := make(map[types.NamespacedName]*provisioningv1.DPUNodeMaintenance)
	for _, dpuNodeMaintenance := range dpuNodeMaintenanceList.Items {
		originalDPUNodeMaintenance[client.ObjectKeyFromObject(&dpuNodeMaintenance)] = dpuNodeMaintenance.DeepCopy()
	}

	// Handle any DPUNodeMaintenance that might be related to this node and requires handling
	modifiedDPUNodeMaintenanceObjects, err := handleDPUNodeMaintenanceObjects(node, dpuServiceList.Items, dpuNodeMaintenanceList.Items)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to handle DPUNodeMaintenance objects: %w", err)
	}

	// Ensure corev1.Node has labels related to in-cluster DPUServices managed by a DPUDeployment. This function ensures
	// that the disruptive and non-disruptive DPUServices can be scheduled when they are first created and a DPUNodeMaintenance
	// object is not created.
	if err := ensureNodeLabelsFromDPUServices(ctx, r.Client, node, dpuDeploymentList.Items, dpuServiceList.Items); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to ensure node labels from in-cluster DPUServices: %w", err)
	}

	// Remove labels from the corev1.Node that are related to a DPUDeployment that is currently being deleted
	if err := removeNodeLabelsFromTerminatingDPUDeployment(node, dpuDeploymentList.Items, dpuServiceList.Items); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to remove node labels from terminating DPUDeployment: %w", err)
	}

	// Remove stale in-cluster DPUService version labels from the corev1.Node
	if err := removeStaleNodeLabels(node, dpuServiceList.Items); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to remove stale node labels: %w", err)
	}

	// Patch the corev1.Node first to ensure that the labels are adjusted as needed and then patch the dpuNodeMaintenance
	// objects to remove the relevant requestors. We do it that way to avoid race conditions where the dpuNodeMaintenance
	// objects are patched but the corev1.Node patch fails leading to stale labels on the corev1.Node that are not updated.
	patcher := patch.NewSerialPatcher(originalNode, r.Client)
	if err := patcher.Patch(ctx, node, patch.WithFieldOwner(dpuDeploymentNodeControllerName)); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to patch Node %s: %w", client.ObjectKeyFromObject(node), err)
	}

	for _, dpuNodeMaintenance := range modifiedDPUNodeMaintenanceObjects {
		patcher := patch.NewSerialPatcher(originalDPUNodeMaintenance[client.ObjectKeyFromObject(dpuNodeMaintenance)], r.Client)
		if err := patcher.Patch(ctx, dpuNodeMaintenance, patch.WithFieldOwner(dpuDeploymentNodeControllerName)); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to patch DPUNodeMaintenance %s: %w", client.ObjectKeyFromObject(dpuNodeMaintenance), err)
		}
	}

	return ctrl.Result{}, nil
}

// handleDPUNodeMaintenanceObjects filters the given DPUNodeMaintenance by condition, discovers if any of them has a requestor
// added by the DPUDeployment controller and is related to one of its services, adjusts the corev1.Node
// labels in place and removes the requestor from the DPUNodeMaintenance. Notice that neither the corev1.Node nor the DPUNodeMaintenance
// objects are patched and are expected to be patched outside of this function.
func handleDPUNodeMaintenanceObjects(node *corev1.Node, dpuServices []dpuservicev1.DPUService, dpuNodeMaintenanceObjects []provisioningv1.DPUNodeMaintenance) ([]*provisioningv1.DPUNodeMaintenance, error) {
	// Filter DPUNodeMaintenance objects that have condition NodeEffectApplied to ensure that we are are not updating
	// corev1.Node labels proactively
	readyDPUNodeMaintenanceObjects := make([]*provisioningv1.DPUNodeMaintenance, 0, len(dpuNodeMaintenanceObjects))
	for i := range dpuNodeMaintenanceObjects {
		if conditions.IsTrue(&dpuNodeMaintenanceObjects[i], provisioningv1.ConditionNodeEffectApplied) {
			readyDPUNodeMaintenanceObjects = append(readyDPUNodeMaintenanceObjects, dpuNodeMaintenanceObjects[i].DeepCopy())
		}
	}
	// Return early if there are no DPUNodeMaintenance objects for this node
	if len(readyDPUNodeMaintenanceObjects) == 0 {
		return nil, nil
	}
	// Adjust corev1.Node labels and DPUNodeMaintenance requestors to prepare for patching
	for i := range readyDPUNodeMaintenanceObjects {
		dpuNodeMaintenance := readyDPUNodeMaintenanceObjects[i]
		for _, dpuService := range dpuServices {
			potentialRequestor := getRequestorForDPUObjectVersion(dpuService.Labels[dpuservicev1.ParentDPUDeploymentNameLabel], dpuService.Name)
			if !slices.Contains(dpuNodeMaintenance.Spec.Requestor, potentialRequestor) {
				continue
			}

			// Get DPUService version label key from the nodeSelector this DPUService has in order to use it for the corev1.Node
			// label
			dpuServiceVersionLabelKey, err := getDPUServiceVersionLabelKeyFromDPUService(dpuService, dpuService.Name)
			if err != nil {
				return nil, fmt.Errorf("failed to find DPUService version label key for DPUService %s: %w", dpuService.Name, err)
			}

			// Add corev1.Node label for this DPUService
			nodeLabels := node.GetLabels()
			if nodeLabels == nil {
				nodeLabels = make(map[string]string)
			}

			nodeLabels[dpuServiceVersionLabelKey] = dpuService.Name
			node.SetLabels(nodeLabels)

			// Remove requestor from the DPUNodeMaintenance
			dpuNodeMaintenance.Spec.Requestor = slices.DeleteFunc(dpuNodeMaintenance.Spec.Requestor, func(requestor string) bool {
				return requestor == potentialRequestor
			})
		}
	}

	return readyDPUNodeMaintenanceObjects, nil
}

// getDPUServiceVersionLabelKeyFromDPUService gets the DPUServiceVersion Label Key from the nodeSelector of the given DPUService
// based on the given DPUServiceVersion that is provided.
func getDPUServiceVersionLabelKeyFromDPUService(dpuService dpuservicev1.DPUService, dpuServiceVersion string) (string, error) {
	if dpuService.Spec.ServiceDaemonSet == nil {
		return "", fmt.Errorf("spec.ServiceDaemonSet can't be nil for DPUService %s owned by a DPUDeployment", dpuService.Name)
	}
	if dpuService.Spec.ServiceDaemonSet.NodeSelector == nil {
		return "", fmt.Errorf("spec.ServiceDaemonSet.NodeSelector can't be nil for DPUService %s", dpuService.Name)
	}
	for _, term := range dpuService.Spec.ServiceDaemonSet.NodeSelector.NodeSelectorTerms {
		for _, req := range term.MatchExpressions {
			if slices.Contains(req.Values, dpuServiceVersion) {
				return req.Key, nil
			}
		}
	}
	return "", fmt.Errorf("no key exists that match version %s", dpuServiceVersion)
}

// ensureNodeLabelsFromDPUServices checks whether the corev1.Node has labels associated with the given in-cluster DPUServices
// managed by a DPUDeployment. If any label is missing, the corev1.Node is updated in memory. This function is needed
// to ensure that non-disruptive and disruptive in-cluster services are scheduled when they are created for the first time
// if a DPUNodeMaintenance is not created.
func ensureNodeLabelsFromDPUServices(ctx context.Context, c client.Client, node *corev1.Node, dpuDeployments []dpuservicev1.DPUDeployment, dpuServices []dpuservicev1.DPUService) error {
	for _, dpuDeployment := range dpuDeployments {
		// Skip terminating DPUDeployments
		if !dpuDeployment.DeletionTimestamp.IsZero() {
			continue
		}

		// Find the matching corev1.Nodes based on what the DPUDeployment targets
		matchingNodes, err := getDPUDeploymentMatchingNodeNames(ctx, c, &dpuDeployment)
		if err != nil {
			return fmt.Errorf("failed to find matching corev1.Nodes for DPUDeployment %v: %w", client.ObjectKeyFromObject(&dpuDeployment), err)
		}

		// If the node doesn't match the selector the DPUDeployment is targeting, move to the next DPUDeployment
		if !slices.Contains(matchingNodes, node.Name) {
			continue
		}

		for _, dpuService := range dpuServices {
			dpuServiceVersionLabelKey, err := getDPUServiceVersionLabelKeyFromDPUService(dpuService, dpuService.Name)
			if err != nil {
				return fmt.Errorf("failed to find DPUService version label key for DPUService %s: %w", dpuService.Name, err)
			}

			// Add corev1.Node label for this DPUService
			nodeLabels := node.GetLabels()
			if nodeLabels == nil {
				nodeLabels = make(map[string]string)
			}

			// Skip this DPUService if the label exists, even outdated. Existing labels must be updated only via the handleDPUNodeMaintenanceObjects
			if _, ok := nodeLabels[dpuServiceVersionLabelKey]; ok {
				continue
			}

			nodeLabels[dpuServiceVersionLabelKey] = dpuService.Name
			node.SetLabels(nodeLabels)
		}
	}
	return nil
}

// removeNodeLabelsFromTerminatingDPUDeployment filters out the DPUDeployments that are currently being deleted and using
// the given dpuservices, it removes the labels from the corev1.Node that are no longer relevant.
func removeNodeLabelsFromTerminatingDPUDeployment(node *corev1.Node, dpuDeployments []dpuservicev1.DPUDeployment, dpuServices []dpuservicev1.DPUService) error {
	for _, dpuDeployment := range dpuDeployments {
		// Skip non terminating DPUDeployments
		if dpuDeployment.DeletionTimestamp.IsZero() {
			continue
		}

		for _, dpuService := range dpuServices {
			// If the service doesn't belong to the currently checked DPUDeployment, continue
			if dpuService.Labels[dpuservicev1.ParentDPUDeploymentNameLabel] != getParentDPUDeploymentLabelValue(client.ObjectKeyFromObject(&dpuDeployment)) {
				continue
			}
			// Get DPUService version label key from the nodeSelector this DPUService has in order to remove it from
			// the corev1.Node labels
			dpuServiceVersionLabelKey, err := getDPUServiceVersionLabelKeyFromDPUService(dpuService, dpuService.Name)
			if err != nil {
				return fmt.Errorf("failed to find DPUService version label key for DPUService %s: %w", dpuService.Name, err)
			}

			nodeLabels := node.GetLabels()
			delete(nodeLabels, dpuServiceVersionLabelKey)
			node.SetLabels(nodeLabels)
		}
	}
	return nil
}

// removeStaleNodeLabels removes stale corev1.Node labels that exist on nodes from DPUServices managed by DPUDeployments
// that no longer exist. We assume the DPUServices passed as input are managed by a DPUDeployment.
func removeStaleNodeLabels(node *corev1.Node, dpuServices []dpuservicev1.DPUService) error {
	// Get all the label keys that are related to in-cluster DPUServices versions managed by a DPUDeployment
	staleDPUServiceVersionLabelKeys := make(map[string]interface{})
	for labelKey := range node.GetLabels() {
		if strings.HasPrefix(labelKey, inClusterDPUServiceVersionLabelKeyPrefix) {
			staleDPUServiceVersionLabelKeys[labelKey] = struct{}{}
		}
	}

	for _, dpuService := range dpuServices {
		// Get DPUService version label key from the nodeSelector this DPUService has in order to remove it from
		// the corev1.Node labels
		dpuServiceVersionLabelKey, err := getDPUServiceVersionLabelKeyFromDPUService(dpuService, dpuService.Name)
		if err != nil {
			return fmt.Errorf("failed to find DPUService version label key for DPUService %s: %w", dpuService.Name, err)
		}

		// Remove this label from the stale keys
		delete(staleDPUServiceVersionLabelKeys, dpuServiceVersionLabelKey)
	}

	// Remove all the stale labels from the in memory corev1.Node
	nodeLabels := node.GetLabels()
	for label := range staleDPUServiceVersionLabelKeys {
		delete(nodeLabels, label)
	}
	node.SetLabels(nodeLabels)

	return nil
}
