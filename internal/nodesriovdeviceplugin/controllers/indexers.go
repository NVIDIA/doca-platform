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

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// dpuNodeNameField is the field indexer for DPU resources by DPUNode name.
	dpuNodeNameField = "spec.dpuNodeName"

	// dpuNodeKubeNodeRefField is the field indexer for DPUNode resources by KubeNodeRef.
	dpuNodeKubeNodeRefField = "status.kubeNodeRef"

	// podTargetNodeField is the field indexer for managed Pod resources by target node name
	// extracted from the pod's node affinity.
	podTargetNodeField = "spec.affinity.nodeAffinity.targetNode"
)

// SetupIndexers initializes all field indexers required by the node SRIOV device plugin controllers.
func SetupIndexers(ctx context.Context, mgr ctrl.Manager) error {
	// Set up the index for DPU lookups by dpuNodeName.
	if err := mgr.GetFieldIndexer().IndexField(
		ctx,
		&provisioningv1.DPU{},
		dpuNodeNameField,
		func(obj client.Object) []string {
			dpu := obj.(*provisioningv1.DPU)
			return []string{dpu.Spec.DPUNodeName}
		},
	); err != nil {
		return fmt.Errorf("failed to register indexer for DPU CR by dpuNodeName: %w", err)
	}

	// Set up the index for DPUNode lookups by kubeNodeRef.
	if err := mgr.GetFieldIndexer().IndexField(
		ctx,
		&provisioningv1.DPUNode{},
		dpuNodeKubeNodeRefField,
		func(obj client.Object) []string {
			dpuNode := obj.(*provisioningv1.DPUNode)
			return []string{ptr.Deref(dpuNode.Status.KubeNodeRef, "")}
		},
	); err != nil {
		return fmt.Errorf("failed to register indexer for DPUNode CR by kubeNodeRef: %w", err)
	}

	// Set up the index for managed Pod lookups by target node from affinity.
	if err := mgr.GetFieldIndexer().IndexField(
		ctx,
		&corev1.Pod{},
		podTargetNodeField,
		func(obj client.Object) []string {
			pod := obj.(*corev1.Pod)
			return []string{getTargetNodeFromPod(pod)}
		},
	); err != nil {
		return fmt.Errorf("failed to register indexer for Pod by target node: %w", err)
	}

	return nil
}

// getTargetNodeFromPod extracts the target node name from the pod's node affinity.
func getTargetNodeFromPod(pod *corev1.Pod) string {
	if pod.Spec.Affinity == nil ||
		pod.Spec.Affinity.NodeAffinity == nil ||
		pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		return ""
	}

	for _, term := range pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
		for _, req := range term.MatchFields {
			if req.Key == metav1.ObjectNameField && req.Operator == corev1.NodeSelectorOpIn && len(req.Values) > 0 {
				return req.Values[0]
			}
		}
	}
	return ""
}
