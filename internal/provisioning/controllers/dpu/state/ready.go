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
	dpucluster "github.com/nvidia/doca-platform/internal/dpucluster"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/dms"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/hostnetwork"

	maintenancev1alpha1 "github.com/Mellanox/maintenance-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type dpuReadyState struct {
	dpu *provisioningv1.DPU
}

func (st *dpuReadyState) Handle(ctx context.Context, client client.Client, option dutil.DPUOptions) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)
	state := st.dpu.Status.DeepCopy()
	if isDeleting(st.dpu) {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	if err := healthyCheck(ctx, st.dpu, client, option); err != nil {
		state.Phase = provisioningv1.DPUError
		updateFalseDPUCondReady(state, "DPUNodeNotReady", err.Error())
	}

	if err := HandleNodeEffect(ctx, client, *st.dpu.Spec.NodeEffect, st.dpu.Spec.NodeName, st.dpu.Namespace); err != nil {
		state.Phase = provisioningv1.DPUError
		updateFalseDPUCondReady(state, "NodeEffectError", err.Error())
		return *state, err
	}

	nn := types.NamespacedName{
		Namespace: st.dpu.Namespace,
		Name:      st.dpu.Name,
	}

	dpu := provisioningv1.DPU{}
	if err := client.Get(ctx, nn, &dpu); err != nil {
		updateFalseDPUCondReady(state, "DPUGetError", err.Error())
		return *state, err
	}

	dpuName := dpu.Name

	tenantNamespace := dpu.Spec.Cluster.Namespace
	tenantName := dpu.Spec.Cluster.Name

	dpuCluster := &provisioningv1.DPUCluster{}
	err := client.Get(ctx, types.NamespacedName{Namespace: tenantNamespace, Name: tenantName}, dpuCluster)
	if err != nil {
		return *state, err
	}

	newClient, err := dpucluster.NewConfig(client, dpuCluster).Client(ctx)
	if err != nil {
		updateFalseDPUCondReady(state, "DPUClusterClientGetError", err.Error())
		return *state, err
	}

	node := &corev1.Node{}
	if err := newClient.Get(ctx, types.NamespacedName{Namespace: tenantNamespace, Name: dpuName}, node); err != nil {
		updateFalseDPUCondReady(state, "DPUNodeGetError", err.Error())
		return *state, err
	}

	if !cutil.IsNodeReady(node) {
		updateFalseDPUCondReady(state, "DPUNodeNotReady", fmt.Errorf("DPU Node %s is not Ready", node.Name).Error())
		logger.V(3).Info(fmt.Sprintf("DPU Node is not Ready: %v", node.Status.Conditions))
		return *state, nil
	} else {
		cond := cutil.DPUCondition(provisioningv1.DPUCondReady, "DPUNodeReady", "")
		cutil.SetDPUCondition(state, cond)
		if cutil.NeedUpdateLabels(dpu.Spec.Cluster.NodeLabels, node.Labels) {
			state.Phase = provisioningv1.DPUClusterConfig
			logger.V(3).Info(fmt.Sprintf("node %s needs to update label", node.Name))
			return *state, nil
		}
		return *state, nil
	}
}

// Check if the DMS pod exist, if not, restore the missing pod
func healthyCheck(ctx context.Context, dpu *provisioningv1.DPU, client client.Client, option dutil.DPUOptions) error {
	nn := types.NamespacedName{
		Namespace: dpu.Namespace,
		Name:      cutil.GenerateDMSPodName(dpu.Name),
	}
	dmsPod := &corev1.Pod{}
	if err := client.Get(ctx, nn, dmsPod); err != nil {
		if apierrors.IsNotFound(err) {
			return dms.CreateDMSPod(ctx, client, dpu, option)
		}
		return err
	}

	nn = types.NamespacedName{
		Namespace: dpu.Namespace,
		Name:      cutil.GenerateHostnetworkPodName(dpu.Name),
	}
	hostnetworkPod := &corev1.Pod{}
	if err := client.Get(ctx, nn, hostnetworkPod); err != nil {
		if apierrors.IsNotFound(err) {
			return hostnetwork.CreateHostNetworkSetupPod(ctx, client, dpu, option)
		}
		return err
	}
	return nil
}

func updateFalseDPUCondReady(status *provisioningv1.DPUStatus, reason string, message string) {
	cond := cutil.DPUCondition(provisioningv1.DPUCondReady, reason, message)
	cond.Status = metav1.ConditionFalse
	cutil.SetDPUCondition(status, cond)
}

func HandleNodeEffect(ctx context.Context, k8sClient client.Client, nodeEffect provisioningv1.NodeEffect, nodeName string, namespace string) error {
	if nodeEffect.NoEffect {
		return nil
	}

	nn := types.NamespacedName{
		Namespace: "",
		Name:      nodeName,
	}
	node := &corev1.Node{}
	if err := k8sClient.Get(ctx, nn, node); err != nil {
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

	if nodeEffect.Drain != nil {
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
	if err := cutil.DeleteObject(k8sClient, maintenance); err != nil {
		return fmt.Errorf("failed to delete NodeMaintenance (%s), err: %v", maintenanceNN, err)
	}

	return nil
}
