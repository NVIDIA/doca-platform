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
	"slices"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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
