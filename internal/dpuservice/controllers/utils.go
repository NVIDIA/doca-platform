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
	"slices"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	schedulingv1 "k8s.io/component-helpers/scheduling/corev1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func getRevisionHistoryLimitList(oldRevisions []client.Object, revisionHistoryLimit int32) []client.Object {
	sortObjectsByCreationTimestamp(oldRevisions)
	toRetain := make([]client.Object, 0, revisionHistoryLimit)
	for i := len(oldRevisions) - 1; i >= 0; i-- {
		if int32(len(toRetain)) >= revisionHistoryLimit {
			break
		}
		toRetain = append(toRetain, oldRevisions[i])
	}

	return toRetain
}

// sortObjectsByCreationTimestamp sort by creation time and only disable the newest one. The other ones can be deleted
func sortObjectsByCreationTimestamp(objects []client.Object) {
	slices.SortFunc(objects, func(t, u client.Object) int {
		return t.GetCreationTimestamp().Compare(u.GetCreationTimestamp().Time)
	})
}

// newObjectLabelSelectorWithOwner creates a LabelSelector for an Object with the given k/v and owner
func newObjectLabelSelectorWithOwner(key, value string, owner types.NamespacedName) *metav1.LabelSelector {
	return &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      key,
				Operator: metav1.LabelSelectorOpIn,
				Values:   []string{value},
			},
			{
				Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
				Operator: metav1.LabelSelectorOpIn,
				Values:   []string{getParentDPUDeploymentLabelValue(owner)},
			},
		},
	}
}

// newObjectNodeSelectorWithOwner creates a NodeSelector for an Object with the given k/v and owner
func newObjectNodeSelectorWithOwner(key, value string, owner types.NamespacedName) *corev1.NodeSelector {
	return &corev1.NodeSelector{
		NodeSelectorTerms: []corev1.NodeSelectorTerm{
			{
				MatchExpressions: []corev1.NodeSelectorRequirement{
					{
						Key:      key,
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{value},
					},
					{
						Key:      dpuservicev1.ParentDPUDeploymentNameLabel,
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{getParentDPUDeploymentLabelValue(owner)},
					},
				},
			},
		},
	}
}

// newInClusterNodeSelectorFromDPUSetSelector creates a NodeSelector for an in-cluster DPUService
func newInClusterNodeSelectorFromDPUSetSelector(versionKey string, versionValue string, dpuSets []dpuservicev1.DPUSet) *corev1.NodeSelector {
	nodeSelectorTerms := []corev1.NodeSelectorTerm{}
	for _, dpuSet := range dpuSets {
		nodeSelector := dpuSet.NodeSelector

		// If we can't find any nodeSelector in the DPUSets, it means that it targets all DPUNodes, therefore we return
		// a nodeSelector that matches all nodes that have the correct DPUService version label.
		// TODO: Check for race conditions, what happens if a user has a DPUDeployment applied and:
		// * Reduces the nodes that the DPUDeployment should handle
		// * Increases the nodes that the DPUDeployment should handle
		if nodeSelector == nil {
			return &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{
				{
					MatchExpressions: []corev1.NodeSelectorRequirement{
						{
							Key:      versionKey,
							Operator: corev1.NodeSelectorOpIn,
							Values:   []string{versionValue},
						},
					},
				},
			}}
		}

		for key, value := range nodeSelector.MatchLabels {
			nodeSelectorTerms = append(nodeSelectorTerms, corev1.NodeSelectorTerm{
				MatchExpressions: []corev1.NodeSelectorRequirement{
					{
						Key:      versionKey,
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{versionValue},
					},
					{
						Key:      key,
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{value},
					},
				},
			})
		}
	}

	// If we don't have any nodeSelector, then it means that we have no DPUSet, therefore, the DPUService should not target any node.
	// In that case, the nodeSelector should contain just the DPUService version label, but we do not expect the DPUDeployment
	// Node Controller to add that label to any of the nodes.
	if len(nodeSelectorTerms) == 0 {
		nodeSelectorTerms = append(nodeSelectorTerms, corev1.NodeSelectorTerm{
			MatchExpressions: []corev1.NodeSelectorRequirement{
				{
					Key:      versionKey,
					Operator: corev1.NodeSelectorOpIn,
					Values:   []string{versionValue},
				},
			},
		})
	}
	return &corev1.NodeSelector{NodeSelectorTerms: nodeSelectorTerms}
}

func listNodesByNodeAffinity(ctx context.Context, c client.Client, nodeSelectorTerms []corev1.NodeSelectorTerm) ([]corev1.Node, error) {
	// If a user does not define a NodeSelector we List and return all nodes.
	if len(nodeSelectorTerms) == 0 {
		nodeList := &corev1.NodeList{}
		err := c.List(ctx, nodeList)
		if err != nil {
			return nil, err
		}
		return nodeList.Items, nil
	}
	var matchedNodes []corev1.Node
	for _, term := range nodeSelectorTerms {
		selector := convertNodeSelectorTermToSelector(term)
		nodeList := &corev1.NodeList{}
		listOptions := &client.ListOptions{
			LabelSelector: selector,
		}
		err := c.List(ctx, nodeList, listOptions)
		if err != nil {
			return nil, err
		}
		matchedNodes = append(matchedNodes, nodeList.Items...)
	}
	return deduplicateNodes(matchedNodes), nil
}

func convertNodeSelectorTermToSelector(term corev1.NodeSelectorTerm) labels.Selector {
	reqs := []labels.Requirement{}
	for _, expr := range term.MatchExpressions {
		var op selection.Operator
		switch expr.Operator {
		case corev1.NodeSelectorOpIn:
			op = selection.In
		case corev1.NodeSelectorOpNotIn:
			op = selection.NotIn
		case corev1.NodeSelectorOpExists:
			op = selection.Exists
		case corev1.NodeSelectorOpDoesNotExist:
			op = selection.DoesNotExist
		case corev1.NodeSelectorOpGt:
			op = selection.GreaterThan
		case corev1.NodeSelectorOpLt:
			op = selection.LessThan
		default:
			continue
		}

		req, err := labels.NewRequirement(expr.Key, op, expr.Values)
		if err == nil {
			reqs = append(reqs, *req)
		}
	}
	return labels.NewSelector().Add(reqs...)
}

func deduplicateNodes(nodes []corev1.Node) []corev1.Node {
	seen := make(map[string]struct{})
	var unique []corev1.Node
	for _, node := range nodes {
		if _, exists := seen[node.Name]; !exists {
			seen[node.Name] = struct{}{}
			unique = append(unique, node)
		}
	}
	return unique
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

// enqueueRequests enqueues requests into q after deduplication
func enqueueRequests(requests []ctrl.Request, q workqueue.TypedRateLimitingInterface[ctrl.Request]) {
	reqs := make(map[ctrl.Request]struct{})

	// deduplicate requests
	for _, req := range requests {
		reqs[req] = struct{}{}
	}

	for req := range reqs {
		q.Add(req)
	}
}
