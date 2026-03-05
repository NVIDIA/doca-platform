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

//nolint:dupl
package controllers

import (
	"context"
	"fmt"
	"sort"
	"strings"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	sfcsetcontroller "github.com/nvidia/doca-platform/internal/servicechainset/controllers"
	"github.com/nvidia/doca-platform/pkg/conditions"

	"github.com/fluxcd/pkg/runtime/patch"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// maxUnreadyItemsInMessage is the maximum number of unready items (pods, interfaces, chains) to include
	// in a condition message before truncating with "... (X more)"
	maxUnreadyItemsInMessage = 5
)

func (r *DPUReadyReconciler) reconcileDPUOperationalConditions(ctx context.Context, scope *reconcileScope) error {
	log := ctrllog.FromContext(ctx)

	var errs []error
	for _, dpu := range scope.dpus {
		// Fetch common cluster data once
		clusterData, err := r.getDPUClusterData(ctx, dpu)
		if err != nil {
			errs = append(errs, fmt.Errorf("fetch cluster data for DPU %s: %w", dpu.Name, err))
			continue
		}

		// Only update operational conditions for the specific DPU being reconciled
		operationalConditions, err := r.aggregateOperationalConditions(ctx, clusterData, dpu, scope.dpuServiceList)
		if err != nil {
			log.Error(err, "Errors during condition aggregation, some conditions set to Unknown", "dpu", dpu.Name)
			errs = append(errs, fmt.Errorf("aggregate operational conditions for DPU %s: %w", dpu.Name, err))
		}

		// Always update conditions, even if there were errors during aggregation
		if err := r.updateDPUOperationalConditions(ctx, dpu, operationalConditions); err != nil {
			errs = append(errs, fmt.Errorf("update operational conditions for DPU %s: %w", dpu.Name, err))
			continue
		}

		log.V(1).Info("Successfully updated operational conditions", "dpu", dpu.Name)
	}
	return kerrors.NewAggregate(errs)
}

// dpuClusterData holds commonly used data fetched once from the DPU cluster
type dpuClusterData struct {
	client client.Client
	node   *corev1.Node
}

func (r *DPUReadyReconciler) aggregateOperationalConditions(ctx context.Context, clusterData *dpuClusterData, dpu provisioningv1.DPU, dpuServiceList *dpuservicev1.DPUServiceList) ([]metav1.Condition, error) {
	// 1. Node Problems condition from node-problem-detector
	nodeProblemsCondition := r.aggregateNodeProblemsCondition(clusterData.node)
	result := []metav1.Condition{nodeProblemsCondition}

	var errs []error
	// 2. & 3. DPU Service Pods conditions (critical and non-critical) - fetch pods once
	criticalPodsCondition, nonCriticalPodsCondition, err := r.aggregateDPUServicePodsConditions(ctx, dpu.Name, dpuServiceList, clusterData.client)
	if err != nil {
		errs = append(errs, fmt.Errorf("aggregate DPUService Pods conditions: %w", err))
		// Set both pod conditions to Unknown
		criticalPodsCondition = conditions.NewUnknownCondition(
			string(provisioningv1.DPUOperationalCondDPUServiceCriticalPodsReady),
			"InternalError",
			"Failed to list or aggregate DPU service pods",
		)
		nonCriticalPodsCondition = conditions.NewUnknownCondition(
			string(provisioningv1.DPUOperationalCondDPUServiceNonCriticalPodsReady),
			"InternalError",
			"Failed to list or aggregate DPUServices or Pods",
		)
	}
	result = append(result, criticalPodsCondition, nonCriticalPodsCondition)

	// 4. DPU Service Interfaces condition
	interfacesCondition, err := r.aggregateDPUServiceInterfacesCondition(ctx, dpu, clusterData)
	if err != nil {
		errs = append(errs, fmt.Errorf("aggregate DPUServiceInterfaces condition: %w", err))
		// Set interfaces condition to Unknown
		interfacesCondition = conditions.NewUnknownCondition(
			string(provisioningv1.DPUOperationalCondDPUServiceInterfacesReady),
			"InternalError",
			"Failed to list or aggregate ServiceInterfaces",
		)
	}
	result = append(result, interfacesCondition)

	// 5. DPU Service Chains condition
	chainsCondition, err := r.aggregateDPUServiceChainsCondition(ctx, dpu, clusterData)
	if err != nil {
		errs = append(errs, fmt.Errorf("aggregate DPUServiceChains condition: %w", err))
		// Set chains condition to Unknown
		chainsCondition = conditions.NewUnknownCondition(
			string(provisioningv1.DPUOperationalCondDPUServiceChainsReady),
			"InternalError",
			"Failed to list or aggregate ServiceChains",
		)
	}
	result = append(result, chainsCondition)

	// 6. Summary OperationalReady condition - aggregate all conditions
	summaryCondition := r.aggregateOperationalReadySummary(result)
	result = append([]metav1.Condition{summaryCondition}, result...)

	return result, kerrors.NewAggregate(errs)
}

// getDPUClusterData fetches commonly used data from the DPU cluster once
func (r *DPUReadyReconciler) getDPUClusterData(ctx context.Context, dpu provisioningv1.DPU) (*dpuClusterData, error) {
	clusterKey := client.ObjectKey{
		Namespace: dpu.Spec.Cluster.Namespace,
		Name:      dpu.Spec.Cluster.Name,
	}
	dpuClusterClient, err := r.RemoteCache.GetClient(clusterKey)
	if err != nil {
		return nil, fmt.Errorf("could not get cached client for dpucluster: %w", err)
	}

	node := &corev1.Node{}
	if err := dpuClusterClient.Get(ctx, types.NamespacedName{Name: dpu.Name}, node); err != nil {
		return nil, fmt.Errorf("failed to get the dpu node %s: %w", dpu.Name, err)
	}

	return &dpuClusterData{
		client: dpuClusterClient,
		node:   node,
	}, nil
}

// aggregateOperationalReadySummary creates a summary condition based on all operational conditions
func (r *DPUReadyReconciler) aggregateOperationalReadySummary(operationalConditions []metav1.Condition) metav1.Condition {
	condType := string(provisioningv1.DPUOperationalCondReady)

	var notReadyConditions []string
	var unknownConditions []string

	for _, cond := range operationalConditions {
		switch cond.Status {
		case metav1.ConditionFalse:
			notReadyConditions = append(notReadyConditions, cond.Type)
		case metav1.ConditionUnknown:
			unknownConditions = append(unknownConditions, cond.Type)
		}
	}

	reason := "AllReady"
	message := "All operational conditions are ready"
	status := metav1.ConditionTrue

	switch {
	case len(notReadyConditions) > 0 && len(unknownConditions) > 0:
		reason = "NotReadyAndUnknown"
		message = fmt.Sprintf("Not ready conditions: %s, Unknown conditions: %s", strings.Join(notReadyConditions, ", "), strings.Join(unknownConditions, ", "))
		status = metav1.ConditionFalse
	case len(notReadyConditions) > 0:
		reason = "NotReady"
		message = fmt.Sprintf("Not ready conditions: %s", strings.Join(notReadyConditions, ", "))
		status = metav1.ConditionFalse
	case len(unknownConditions) > 0:
		reason = "Unknown"
		message = fmt.Sprintf("Unknown conditions: %s", strings.Join(unknownConditions, ", "))
		status = metav1.ConditionUnknown
	}
	return conditions.NewCondition(condType, reason, message, status)
}

func (r *DPUReadyReconciler) aggregateNodeProblemsCondition(node *corev1.Node) metav1.Condition {
	// TODO: use provisioningv1.GetNodeProblemDetectorConditions() as soon as MR 4072 is merged
	expectedConditions := []string{
		"OVSHealthy",
		"DPUModeCorrect",
		"UplinkHealthy",
		"SRIOVHealthy",
		"MTUConfigured",
	}

	nodeConditionsMap := make(map[string]corev1.NodeCondition, len(node.Status.Conditions))
	for _, condition := range node.Status.Conditions {
		nodeConditionsMap[string(condition.Type)] = condition
	}

	problematicConditions := []string{}
	for _, expectedType := range expectedConditions {
		condition, exists := nodeConditionsMap[expectedType]
		// Following Kubernetes Node convention: False means healthy, True means there's a problem
		if condition.Status == corev1.ConditionFalse {
			continue
		}

		reason := condition.Reason
		if !exists {
			reason = "Unknown"
		}
		problematicConditions = append(problematicConditions, fmt.Sprintf("%s=%s", expectedType, reason))
	}

	if len(problematicConditions) > 0 {
		return conditions.NewFalseCondition(
			string(provisioningv1.DPUOperationalCondNodeProblemsReady),
			"NodeProblemDetectorNotReady",
			fmt.Sprintf("Node problems detected (%d/%d): %s",
				len(expectedConditions)-len(problematicConditions),
				len(expectedConditions),
				strings.Join(problematicConditions, ", "),
			),
		)
	}

	return conditions.NewTrueCondition(
		string(provisioningv1.DPUOperationalCondNodeProblemsReady),
		"NoProblemsDetected",
		fmt.Sprintf("All node health checks passing (Ready=%d)", len(expectedConditions)),
	)
}

// aggregateDPUServicePodsConditions fetches pods once and returns both critical and non-critical conditions
func (r *DPUReadyReconciler) aggregateDPUServicePodsConditions(ctx context.Context, dpuName string, dpuServiceList *dpuservicev1.DPUServiceList, dpuClusterClient client.Client) (metav1.Condition, metav1.Condition, error) {
	// Build service ID criticality map
	serviceIDCriticalityMap := make(map[string]bool, len(dpuServiceList.Items))
	for i := range dpuServiceList.Items {
		dpuService := &dpuServiceList.Items[i]
		if dpuService.Spec.ServiceID != nil {
			_, isCritical := dpuService.Labels[criticalDPUServiceLabel]
			serviceIDCriticalityMap[*dpuService.Spec.ServiceID] = isCritical
		}
	}

	// List all pods with service ID label once
	podList := &corev1.PodList{}
	if err := dpuClusterClient.List(ctx, podList,
		client.HasLabels{dpuservicev1.DPFServiceIDLabelKey},
		client.MatchingFields{nodeNameField: dpuName},
	); err != nil {
		return metav1.Condition{}, metav1.Condition{}, fmt.Errorf("failed to list pods: %w", err)
	}

	// Separate pods by criticality in a single pass
	var criticalPodCount, nonCriticalPodCount int
	var criticalNotReadyPods, nonCriticalNotReadyPods []string

	for i := range podList.Items {
		pod := &podList.Items[i]
		serviceID := pod.Labels[dpuservicev1.DPFServiceIDLabelKey]

		podIsCritical, serviceExists := serviceIDCriticalityMap[serviceID]
		if !serviceExists {
			continue
		}

		isReady := isPodRunning(pod)

		if podIsCritical {
			criticalPodCount++
			if !isReady {
				criticalNotReadyPods = append(criticalNotReadyPods, pod.Name)
			}
		} else {
			nonCriticalPodCount++
			if !isReady {
				nonCriticalNotReadyPods = append(nonCriticalNotReadyPods, pod.Name)
			}
		}
	}

	var criticalCondition metav1.Condition
	var nonCriticalCondition metav1.Condition
	// Build critical condition
	if len(criticalNotReadyPods) > 0 {
		criticalCondition = conditions.NewFalseCondition(
			string(provisioningv1.DPUOperationalCondDPUServiceCriticalPodsReady),
			"PodsNotReady",
			fmt.Sprintf("Critical Pods not ready (%d/%d Pods): %s",
				criticalPodCount-len(criticalNotReadyPods),
				criticalPodCount,
				formatUnreadyItems(criticalNotReadyPods)),
		)
	} else {
		criticalCondition = conditions.NewTrueCondition(
			string(provisioningv1.DPUOperationalCondDPUServiceCriticalPodsReady),
			"AllPodsReady",
			fmt.Sprintf("All critical Pods are ready (%d Pods)", criticalPodCount),
		)
	}

	// Build non-critical condition
	if len(nonCriticalNotReadyPods) > 0 {
		nonCriticalCondition = conditions.NewFalseCondition(
			string(provisioningv1.DPUOperationalCondDPUServiceNonCriticalPodsReady),
			"PodsNotReady",
			fmt.Sprintf("Pods not ready (%d/%d Pods): %s",
				nonCriticalPodCount-len(nonCriticalNotReadyPods),
				nonCriticalPodCount,
				formatUnreadyItems(nonCriticalNotReadyPods)),
		)
	} else {
		nonCriticalCondition = conditions.NewTrueCondition(
			string(provisioningv1.DPUOperationalCondDPUServiceNonCriticalPodsReady),
			"AllPodsReady",
			fmt.Sprintf("All Pods are ready (%d Pods)", nonCriticalPodCount),
		)
	}

	return criticalCondition, nonCriticalCondition, nil
}

// aggregateDPUServiceInterfacesCondition aggregates the interfaces condition
func (r *DPUReadyReconciler) aggregateDPUServiceInterfacesCondition(ctx context.Context, dpu provisioningv1.DPU, clusterData *dpuClusterData) (metav1.Condition, error) {
	// Get all DPUServiceInterfaces from management cluster
	dpuServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
	if err := r.Client.List(ctx, dpuServiceInterfaceList,
		client.HasLabels{dpuservicev1.ParentDPUDeploymentNameLabel},
	); err != nil {
		return metav1.Condition{}, fmt.Errorf("failed to list DPUServiceInterfaces: %w", err)
	}

	// Fetch ServiceInterfaces from DPU cluster
	serviceInterfaceList := &dpuservicev1.ServiceInterfaceList{}
	if err := clusterData.client.List(ctx, serviceInterfaceList,
		client.MatchingFields{nodeNameField: dpu.Name},
	); err != nil {
		return metav1.Condition{}, fmt.Errorf("failed to list ServiceInterfaces: %w", err)
	}

	serviceInterfaceReadiness := make(map[string]bool, len(serviceInterfaceList.Items))
	for _, si := range serviceInterfaceList.Items {
		setName := si.Labels[sfcsetcontroller.ServiceInterfaceSetNameLabel]
		setNamespace := si.Labels[sfcsetcontroller.ServiceInterfaceSetNamespaceLabel]
		if setName == "" || setNamespace == "" {
			continue
		}
		key := setNamespace + "/" + setName
		serviceInterfaceReadiness[key] = conditions.IsTrue(&si, conditions.TypeReady)
	}

	// Build and return condition
	return buildServiceInterfacesCondition(ctrllog.FromContext(ctx), dpuServiceInterfaceList.Items, serviceInterfaceReadiness, clusterData.node), nil
}

// aggregateDPUServiceChainsCondition aggregates the chains condition
func (r *DPUReadyReconciler) aggregateDPUServiceChainsCondition(ctx context.Context, dpu provisioningv1.DPU, clusterData *dpuClusterData) (metav1.Condition, error) {
	// Get all DPUServiceChains from management cluster
	dpuServiceChainList := &dpuservicev1.DPUServiceChainList{}
	if err := r.Client.List(ctx, dpuServiceChainList,
		client.HasLabels{dpuservicev1.ParentDPUDeploymentNameLabel},
	); err != nil {
		return metav1.Condition{}, fmt.Errorf("failed to list DPUServiceChains: %w", err)
	}

	// Fetch ServiceChains from DPU cluster
	serviceChainList := &dpuservicev1.ServiceChainList{}
	if err := clusterData.client.List(ctx, serviceChainList,
		client.MatchingFields{nodeNameField: dpu.Name},
	); err != nil {
		return metav1.Condition{}, fmt.Errorf("failed to list ServiceChains: %w", err)
	}

	serviceChainReadiness := make(map[string]bool, len(serviceChainList.Items))
	for _, sc := range serviceChainList.Items {
		setName := sc.Labels[sfcsetcontroller.ServiceChainSetNameLabel]
		setNamespace := sc.Labels[sfcsetcontroller.ServiceChainSetNamespaceLabel]
		if setName == "" || setNamespace == "" {
			continue
		}
		key := setNamespace + "/" + setName
		serviceChainReadiness[key] = conditions.IsTrue(&sc, conditions.TypeReady)
	}

	// Build and return condition
	return buildServiceChainsCondition(dpuServiceChainList.Items, serviceChainReadiness, clusterData.node), nil
}

// buildServiceInterfacesCondition constructs the ServiceInterface condition.
// True: the ServiceInterfaces created by the associated DPUServiceInterface exist and are ready.
// False: the ServiceInterface created by the associated DPUServiceInterface is either absent or not ready on this node.
func buildServiceInterfacesCondition(log logr.Logger, dpuServiceInterfaces []dpuservicev1.DPUServiceInterface, readinessMap map[string]bool, node *corev1.Node) metav1.Condition {
	var notReadyInterfaces []string
	var applicableCount int

	for _, iface := range dpuServiceInterfaces {
		// Check if this interface should be created on this node
		matches, err := nodeMatchesLabelSelector(node, iface.Spec.Template.Spec.NodeSelector)
		if err != nil || !matches {
			log.Error(err, "Failed to evaluate node selector for ServiceInterface, skipping in condition calculation", "interface", iface.Name)
			continue
		}

		applicableCount++

		key := iface.GetNamespace() + "/" + iface.GetName()
		isReady, exists := readinessMap[key]

		if !exists || !isReady {
			notReadyInterfaces = append(notReadyInterfaces, iface.Name)
		}
	}

	condType := string(provisioningv1.DPUOperationalCondDPUServiceInterfacesReady)
	if len(notReadyInterfaces) > 0 {
		return conditions.NewFalseCondition(
			condType,
			"ServiceInterfacesNotReady",
			fmt.Sprintf("ServiceInterfaces not ready (%d/%d): %s",
				applicableCount-len(notReadyInterfaces),
				applicableCount,
				formatUnreadyItems(notReadyInterfaces)),
		)
	}

	return conditions.NewTrueCondition(
		condType,
		"AllServiceInterfacesReady",
		fmt.Sprintf("All ServiceInterfaces are ready (Ready=%d)", applicableCount),
	)
}

// buildServiceChainsCondition constructs the ServiceChains condition.
// True: the ServiceChains created by the associated DPUServiceChain exist and are ready.
// False: the ServiceChain created by the associated DPUServiceChain is either absent or not ready on this node.
func buildServiceChainsCondition(dpuServiceChains []dpuservicev1.DPUServiceChain, readinessMap map[string]bool, node *corev1.Node) metav1.Condition {
	var notReadyChains []string
	var applicableCount int

	for _, chain := range dpuServiceChains {
		// Check if this chain should be created on this node
		matches, err := nodeMatchesLabelSelector(node, chain.Spec.Template.Spec.NodeSelector)
		if err != nil || !matches {
			continue
		}

		applicableCount++

		key := chain.GetNamespace() + "/" + chain.GetName()
		isReady, exists := readinessMap[key]

		if !exists || !isReady {
			notReadyChains = append(notReadyChains, chain.Name)
		}
	}

	condType := string(provisioningv1.DPUOperationalCondDPUServiceChainsReady)
	if len(notReadyChains) > 0 {
		return conditions.NewFalseCondition(
			condType,
			"ServiceChainsNotReady",
			fmt.Sprintf("ServiceChains not ready (%d/%d): %s",
				applicableCount-len(notReadyChains),
				applicableCount,
				formatUnreadyItems(notReadyChains)),
		)
	}

	return conditions.NewTrueCondition(
		condType,
		"AllServiceChainsReady",
		fmt.Sprintf("All ServiceChains are ready (Ready=%d)", applicableCount),
	)
}

func (r *DPUReadyReconciler) updateDPUOperationalConditions(ctx context.Context, dpu provisioningv1.DPU, operationalConditions []metav1.Condition) error {
	// Get the latest DPU object to avoid conflicts
	latestDPU := &provisioningv1.DPU{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(&dpu), latestDPU); err != nil {
		return fmt.Errorf("failed to get latest DPU: %w", err)
	}

	patcher := patch.NewSerialPatcher(latestDPU, r.Client)
	for _, condition := range operationalConditions {
		meta.SetStatusCondition(&latestDPU.Status.OperationalConditions, condition)
	}

	return patcher.Patch(ctx, latestDPU,
		patch.WithFieldOwner(dpureadyControllerName),
		patch.WithStatusObservedGeneration{},
	)
}

// nodeConditionsEqual compares two slices of node conditions for equality
func nodeConditionsEqual(a, b []corev1.NodeCondition) bool {
	if len(a) != len(b) {
		return false
	}

	aMap := make(map[corev1.NodeConditionType]corev1.NodeCondition)
	for _, cond := range a {
		aMap[cond.Type] = cond
	}

	for _, condB := range b {
		condA, exists := aMap[condB.Type]
		if !exists {
			return false
		}
		// Compare key fields (ignore LastTransitionTime and LastHeartbeatTime for comparison)
		if condA.Status != condB.Status || condA.Reason != condB.Reason || condA.Message != condB.Message {
			return false
		}
	}

	return true
}

// formatUnreadyItems formats a list of unready item names into a message string.
// If the list exceeds maxUnreadyItemsInMessage, it truncates with "and X more".
func formatUnreadyItems(items []string) string {
	if len(items) == 0 {
		return ""
	}

	sort.Strings(items)
	if len(items) > maxUnreadyItemsInMessage {
		return fmt.Sprintf("%s, ... (%d more)",
			strings.Join(items[:maxUnreadyItemsInMessage], ", "), len(items)-maxUnreadyItemsInMessage)
	}

	return strings.Join(items, ", ")
}
