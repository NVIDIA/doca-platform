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
	"encoding/json"
	"fmt"
	"reflect"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	dpucluster "github.com/nvidia/doca-platform/pkg/dpucluster"

	maintenancev1alpha1 "github.com/Mellanox/maintenance-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func Ready(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)
	state := dpu.Status.DeepCopy()

	// Check deletion condition
	if !dpu.DeletionTimestamp.IsZero() {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	if err := HandleNodeEffect(ctx, ctrlCtx.Client, *dpu.Spec.NodeEffect, dpu.Spec.DPUNodeName, dpu.Namespace); err != nil {
		updateFalseDPUCondReady(state, "NodeEffectError", err.Error())
		state.Phase = provisioningv1.DPUError
		return *state, nil
	}

	tenantNamespace := dpu.Spec.Cluster.Namespace
	tenantName := dpu.Spec.Cluster.Name

	dpuCluster := &provisioningv1.DPUCluster{}
	err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: tenantNamespace, Name: tenantName}, dpuCluster)
	if err != nil {
		if apierrors.IsNotFound(err) {
			updateFalseDPUCondReady(state, "DPUClusterNotFound", err.Error())
			return *state, err
		}
		updateFalseDPUCondReady(state, "GetDPUClusterError", err.Error())
		return *state, err
	}

	newClient, err := dpucluster.NewConfig(ctrlCtx.Client, dpuCluster).Client(ctx)
	if err != nil {
		updateFalseDPUCondReady(state, "DPUClusterClientGetError", err.Error())
		return *state, err
	}

	node := &corev1.Node{}
	if err := newClient.Get(ctx, types.NamespacedName{Namespace: tenantNamespace, Name: dpu.Name}, node); err != nil {
		if apierrors.IsNotFound(err) {
			updateFalseDPUCondReady(state, "NodeNotFound", err.Error())
			return *state, err
		}
		updateFalseDPUCondReady(state, "GetNodeError", err.Error())
		return *state, err
	}

	if !reflect.DeepEqual(state.Addresses, node.Status.Addresses) {
		state.Addresses = make([]corev1.NodeAddress, len(node.Status.Addresses))
		copy(state.Addresses, node.Status.Addresses)
	}

	if !cutil.IsNodeReady(node) {
		err = fmt.Errorf("DPU's Node %s is not Ready", node.Name)
		updateFalseDPUCondReady(state, "NodeNotReady", err.Error())
		return *state, err
	} else {
		cond := cutil.DPUCondition(provisioningv1.DPUCondReady, "DPUReady", "")
		cutil.SetDPUCondition(state, cond)
		lastAppliedLabels := make(map[string]string)
		if node.Annotations != nil {
			if lastAppliedLabelsStr, ok := node.Annotations[cutil.LastAppliedLabelsOnDPUKey]; ok {
				if err := json.Unmarshal([]byte(lastAppliedLabelsStr), &lastAppliedLabels); err != nil {
					logger.Error(err, "Failed to unmarshal last applied labels")
					return *state, err
				}
			}
		}
		// If the last applied labels are not equal to the DPU's cluster node labels, then we need to update the labels
		if dpu.Spec.Cluster.NodeLabels == nil {
			dpu.Spec.Cluster.NodeLabels = make(map[string]string)
		}
		if !reflect.DeepEqual(dpu.Spec.Cluster.NodeLabels, lastAppliedLabels) {
			state.Phase = provisioningv1.DPUClusterConfig
			logger.V(3).Info(fmt.Sprintf("node %s needs to update label", node.Name))
			return *state, nil
		}
		return *state, nil
	}
}

func updateFalseDPUCondReady(status *provisioningv1.DPUStatus, reason string, message string) {
	cond := cutil.DPUCondition(provisioningv1.DPUCondReady, reason, message)
	cond.Status = metav1.ConditionFalse
	cutil.SetDPUCondition(status, cond)
}

func HandleNodeEffect(ctx context.Context, k8sClient client.Client, nodeEffect provisioningv1.NodeEffect, nodeName string, namespace string) error {
	if nodeEffect.IsNoEffect() {
		return nil
	}

	nn := types.NamespacedName{
		Namespace: "",
		Name:      nodeName,
	}
	node := &corev1.Node{}
	if err := k8sClient.Get(ctx, nn, node); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("node %s not found in HandleNodeEffect", nodeName)
		}
		return fmt.Errorf("failed to get node %s in HandleNodeEffect, err: %v", nodeName, err)
	}

	if nodeEffect.Taint != nil {
		originalNode := node.DeepCopy()
		taintFound := false
		for i, taint := range node.Spec.Taints {
			if taint.Key == nodeEffect.Taint.Key {
				node.Spec.Taints = append(node.Spec.Taints[:i], node.Spec.Taints[i+1:]...)
				taintFound = true
				break
			}
		}
		if taintFound {
			patch := client.StrategicMergeFrom(originalNode)
			if err := k8sClient.Patch(ctx, node, patch); err != nil {
				return fmt.Errorf("failed to patch node %s after removing the Taint: %+v, err: %v", nodeName, nodeEffect.Taint, err)
			}
		}
	}

	if nodeEffect.IsDrain() {
		return DeleteNodeMaintenanceCR(ctx, k8sClient, nodeName, namespace)
	}
	return nil
}

func DeleteNodeMaintenanceCR(ctx context.Context, k8sClient client.Client, nodeName string, namespace string) error {
	maintenanceNN := types.NamespacedName{
		Namespace: namespace,
		Name:      nodeName,
	}
	maintenance := &maintenancev1alpha1.NodeMaintenance{}
	err := k8sClient.Get(ctx, maintenanceNN, maintenance)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// node maintenance CR has been deleted
			return nil
		} else {
			return fmt.Errorf("failed to get NodeMaintenance (%s) err: %v", maintenanceNN, err)
		}
	}

	// remove ProvisioningGroupName from maintenance.spec.additionalRequestors
	originalMaintenance := maintenance.DeepCopy()
	for i, requestor := range maintenance.Spec.AdditionalRequestors {
		if requestor == cutil.ProvisioningGroupName {
			maintenance.Spec.AdditionalRequestors = append(maintenance.Spec.AdditionalRequestors[:i], maintenance.Spec.AdditionalRequestors[i+1:]...)
			break
		}
	}
	patch := client.MergeFrom(originalMaintenance)
	if err := k8sClient.Patch(ctx, maintenance, patch); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to patch NodeMaintenance (%s) after removing Spec.AdditionalRequestors, err: %v", maintenanceNN.Name, err)
	}

	// delete node maintenance CR
	if err := cutil.DeleteObjects(ctx, k8sClient, maintenance); err != nil {
		return fmt.Errorf("failed to delete NodeMaintenance (%s), err: %v", maintenanceNN, err)
	}

	return nil
}
