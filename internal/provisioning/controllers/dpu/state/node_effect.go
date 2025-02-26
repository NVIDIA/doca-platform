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

	maintenancev1alpha1 "github.com/Mellanox/maintenance-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type dpuNodeEffectState struct {
	dpu *provisioningv1.DPU
}

func (st *dpuNodeEffectState) Handle(ctx context.Context, k8sClient client.Client, _ dutil.DPUOptions) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)
	state := st.dpu.Status.DeepCopy()
	if isDeleting(st.dpu) {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	nodeEffect := st.dpu.Spec.NodeEffect

	if nodeEffect.NoEffect {
		logger.V(3).Info(fmt.Sprintf("NodeEffect is set to \"NoEffect\" for node: %s", st.dpu.Spec.NodeName))
		state.Phase = provisioningv1.DPUPending
		cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondNodeEffectReady, "", ""))
		return *state, nil
	}

	nodeName := st.dpu.Spec.NodeName

	nn := types.NamespacedName{
		Namespace: "",
		Name:      nodeName,
	}
	node := &corev1.Node{}
	if err := k8sClient.Get(ctx, nn, node); err != nil {
		return *state, fmt.Errorf("failed to get node %s: %v", nodeName, err)
	}

	if len(nodeEffect.CustomLabel) != 0 {
		logger.V(3).Info(fmt.Sprintf("NodeEffect is set to \"CustomLabel\" for node: %s", st.dpu.Spec.NodeName))
		if err := cutil.AddLabelsToNode(ctx, k8sClient, node, nodeEffect.CustomLabel); err != nil {
			return *state, err
		}
	} else if nodeEffect.Taint != nil {
		logger.V(3).Info(fmt.Sprintf("NodeEffect is set to \"Taint\" for node: %s", st.dpu.Spec.NodeName))
		taintExist := false
		for _, t := range node.Spec.Taints {
			if t.Key == nodeEffect.Taint.Key {
				taintExist = true
				break
			}
		}
		if !taintExist {
			node.Spec.Taints = append(node.Spec.Taints, *nodeEffect.Taint)
			if err := k8sClient.Update(ctx, node); err != nil {
				return *state, err
			}
		}
	} else if nodeEffect.Drain != nil {
		logger.V(3).Info(fmt.Sprintf("NodeEffect is set to \"Drain\" for node: %s", nodeName))
		maintenanceNN := types.NamespacedName{
			Namespace: st.dpu.Namespace,
			Name:      nodeName,
		}
		maintenance := &maintenancev1alpha1.NodeMaintenance{}
		if err := k8sClient.Get(ctx, maintenanceNN, maintenance); err != nil {
			if apierrors.IsNotFound(err) {
				// Create node maintenance CR
				owner := metav1.NewControllerRef(st.dpu, provisioningv1.DPUGroupVersionKind)
				logger.V(3).Info(fmt.Sprintf("Createing NodeMaintenance (%s)", maintenanceNN))
				if err = createNodeMaintenance(ctx, k8sClient, owner, nodeName, st.dpu.Namespace); err != nil {
					setDPUCondNodeEffectReady(state, metav1.ConditionFalse, errorOccurredReason, err.Error())
					state.Phase = provisioningv1.DPUError
					return *state, fmt.Errorf("failed to get NodeMaintenance (%s), err: %v", maintenanceNN, err)
				}
				return *state, nil
			} else {
				setDPUCondNodeEffectReady(state, metav1.ConditionFalse, errorOccurredReason, err.Error())
				state.Phase = provisioningv1.DPUError
				return *state, fmt.Errorf("failed to get NodeMaintenance (%s), err: %v", maintenanceNN, err)
			}
		} else {
			if err := addAdditionalRequestor(ctx, k8sClient, maintenance); err != nil {
				setDPUCondNodeEffectReady(state, metav1.ConditionFalse, errorOccurredReason, err.Error())
				state.Phase = provisioningv1.DPUError
				return *state, err
			}
			// check NM status
			if done := checkNodeMaintenanceProgress(maintenance); done {
				logger.V(3).Info(fmt.Sprintf("NodeMaintenance (%s/%s) succeeded", maintenance.Namespace, maintenance.Name))
			} else {
				logger.V(3).Info(fmt.Sprintf("NodeMaintenance (%s/%s) is processing", maintenance.Namespace, maintenance.Name))
				return *state, nil
			}
		}
	}
	state.Phase = provisioningv1.DPUPending
	cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondNodeEffectReady, "", ""))

	return *state, nil
}

// add ProvisioningGroupName to AdditionalRequestors
func addAdditionalRequestor(ctx context.Context, k8sClient client.Client, maintenance *maintenancev1alpha1.NodeMaintenance) error {
	for _, requestor := range maintenance.Spec.AdditionalRequestors {
		if requestor == cutil.ProvisioningGroupName {
			// ProvisioningGroupName already exist in AdditionalRequestors
			return nil
		}
	}

	originalMaintenance := maintenance.DeepCopy()
	maintenance.Spec.AdditionalRequestors = append(maintenance.Spec.AdditionalRequestors, cutil.ProvisioningGroupName)
	patch := client.MergeFrom(originalMaintenance)
	if err := k8sClient.Patch(ctx, maintenance, patch); err != nil {
		return fmt.Errorf("failed to patch node maintenance %s, err: %v", originalMaintenance.Name, err)
	}
	return nil
}

func createNodeMaintenance(ctx context.Context, k8sClient client.Client, owner *metav1.OwnerReference, nodeName string, namespace string) error {
	logger := log.FromContext(ctx)
	nodeMaintenance := &maintenancev1alpha1.NodeMaintenance{
		ObjectMeta: metav1.ObjectMeta{
			Name:            nodeName,
			Namespace:       namespace,
			OwnerReferences: []metav1.OwnerReference{*owner},
		},
		Spec: maintenancev1alpha1.NodeMaintenanceSpec{
			RequestorID:          cutil.NodeMaintenanceRequestorID,
			NodeName:             nodeName,
			DrainSpec:            &maintenancev1alpha1.DrainSpec{Force: true, DeleteEmptyDir: true},
			AdditionalRequestors: []string{cutil.ProvisioningGroupName},
		},
	}
	if err := k8sClient.Create(ctx, nodeMaintenance); err != nil {
		return err
	}
	logger.V(3).Info("Successfully created NodeMaintenance CR", "node", nodeName, "NodeMaintanence", nodeMaintenance)
	return nil
}

func checkNodeMaintenanceProgress(maintenance *maintenancev1alpha1.NodeMaintenance) bool {
	if condition := meta.FindStatusCondition(maintenance.Status.Conditions, maintenancev1alpha1.ConditionTypeReady); condition != nil {
		return condition.Status == metav1.ConditionTrue
	}
	return false
}

func setDPUCondNodeEffectReady(state *provisioningv1.DPUStatus, status metav1.ConditionStatus, reason, message string) {
	cond := cutil.DPUCondition(provisioningv1.DPUCondNodeEffectReady, "", "")
	cond.Status = status
	cond.Reason = reason
	cond.Message = message
	cutil.SetDPUCondition(state, cond)
}
