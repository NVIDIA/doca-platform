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

package state

import (
	"context"
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func Error(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	state := dpu.Status.DeepCopy()
	if !dpu.DeletionTimestamp.IsZero() {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	ctrlCtx.DPUInProvisioningMap.Remove(dutil.DPUID(dpu.UID))

	if err := RemoveNodeEffect(ctx, ctrlCtx.Client, dpu); err != nil {
		err = fmt.Errorf("failed to remove node effect: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondError.String(), err, "RemoveNodeEffectError", err.Error()))
		return *state, err
	}
	return *state, nil
}

func RemoveNodeEffect(ctx context.Context, k8sClient client.Client, dpu *provisioningv1.DPU) error {
	nodeEffect := dpu.Spec.NodeEffect
	nodeName := dpu.Spec.DPUNodeName
	namespace := dpu.Namespace
	if nodeEffect.IsNoEffect() {
		return nil
	}

	if nodeEffect.IsDrain() {
		return DeleteNodeMaintenanceCR(ctx, k8sClient, nodeName, namespace)
	}

	if nodeEffect.IsCustomAction() {
		jobName := GetCustomActionJobName(nodeEffect, dpu)
		nn := types.NamespacedName{
			Namespace: dpu.Namespace,
			Name:      jobName,
		}
		job := &batchv1.Job{}
		if err := k8sClient.Get(ctx, nn, job); err == nil {
			if err := k8sClient.Delete(ctx, job); err != nil {
				return err
			}
		}
	}

	nn := types.NamespacedName{
		Namespace: "",
		Name:      nodeName,
	}
	node := &corev1.Node{}
	if err := k8sClient.Get(ctx, nn, node); err != nil {
		// K8s node has been removed, no need remove node effect.
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	originalNode := node.DeepCopy()
	needPatch := false
	if len(nodeEffect.CustomLabel) != 0 {
		needPatch = true
		for k := range nodeEffect.CustomLabel {
			delete(node.Labels, k)
		}
	} else if nodeEffect.Taint != nil {
		for i, taint := range node.Spec.Taints {
			if taint.Key == nodeEffect.Taint.Key {
				node.Spec.Taints = append(node.Spec.Taints[:i], node.Spec.Taints[i+1:]...)
				needPatch = true
				break
			}
		}
	}
	if needPatch {
		patch := client.StrategicMergeFrom(originalNode)
		if err := k8sClient.Patch(ctx, node, patch); err != nil {
			return fmt.Errorf("failed to patch node %s ,err: %v", nodeName, err)
		}
	}
	return nil
}
