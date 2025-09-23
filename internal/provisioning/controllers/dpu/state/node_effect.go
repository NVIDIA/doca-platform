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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func NodeEffect(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)
	state := dpu.Status.DeepCopy()

	// Check deletion condition
	if !dpu.DeletionTimestamp.IsZero() {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	nodeEffect := dpu.Spec.NodeEffect

	if nodeEffect.IsNoEffect() {
		logger.V(3).Info(fmt.Sprintf("NodeEffect is set to \"NoEffect\" for node: %s", dpu.Spec.DPUNodeName))
		return handleNodeEffectCompletion(ctx, state, "NoEffect")
	}

	// Check for the presence of the specified Node
	dpuNode := &provisioningv1.DPUNode{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.DPUNodeName}, dpuNode); err != nil {
		if apierrors.IsNotFound(err) {
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondNodeEffectReady.String(), err, "DPUNodeNotFound", err.Error()))
			return *state, err
		}
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondNodeEffectReady.String(), err, "GetDPUNodeError", err.Error()))
		return *state, err
	}

	node := &corev1.Node{}
	if dpuNode.Status.KubeNodeRef != nil {
		// Check for the presence of the specified Node
		if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: "", Name: dpu.Spec.DPUNodeName}, node); err != nil {
			if apierrors.IsNotFound(err) {
				cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondNodeEffectReady.String(), err, "NodeNotFound", err.Error()))
				return *state, err
			}
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondNodeEffectReady.String(), err, "GetNodeError", err.Error()))
			return *state, err
		}
	} else if nodeEffect.IsCustomLabel() || nodeEffect.IsTaint() || nodeEffect.IsDrain() {
		err := fmt.Errorf("node effect (%s) is not supported for non k8s environment", nodeEffect.String())
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondNodeEffectReady.String(), err, "InvalidNodeEffect", err.Error()))
		return *state, err
	}

	dpunodemaintenanceName, err := cutil.GenerateDPUNodeMaintenanceObjectName(dpu.Spec.DPUNodeName, dpu.Spec.NodeEffect)
	if err != nil {
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondNodeEffectReady.String(), err, "InvalidNodeEffect", err.Error()))
		return *state, err
	}
	dpunodemaintenance := &provisioningv1.DPUNodeMaintenance{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpunodemaintenanceName}, dpunodemaintenance); err != nil {
		if apierrors.IsNotFound(err) {
			// This is a pre-condition for creating DPUNodeMaintenance object.
			// DPUNode should be changed to not ready in DPUNode controller, and we should wait for it before creating DPUNodeMaintenance object.
			if cutil.IsDPUNodeReady(dpuNode) {
				cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondNodeEffectReady.String(), err, "DPUNodeIsReady", "waiting for DPUNode to be not ready"))
				return *state, nil
			}
			// Create DPUNodeMaintenance object if it doesn't exist
			logger.V(3).Info(fmt.Sprintf("Creating DPUNodeMaintenance (%s/%s)", dpu.Namespace, dpunodemaintenanceName))
			if err := createDPUNodeMaintenance(ctx, ctrlCtx.Client, dpunodemaintenanceName, dpu); err != nil {
				cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondNodeEffectReady.String(), err, "FailedCreateDPUNodeMaintenance", err.Error()))
				return *state, err
			}
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondNodeEffectReady.String(), err, "CreatedDPUNodeMaintenance", "CreatedDPUNodeMaintenance"))
			return *state, nil
		} else {
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondNodeEffectReady.String(), err, "FailedGetDPUNodeMaintenance", err.Error()))
			return *state, err
		}
	} else {
		// If DPUNodeMaintenance object is being deleted, log message and requeue.
		if dpunodemaintenance.DeletionTimestamp != nil {
			err = fmt.Errorf("DPUNodeMaintenance (%s/%s) is being deleted", dpunodemaintenance.Namespace, dpunodemaintenance.Name)
			logger.V(3).Info(err.Error())
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondNodeEffectReady.String(), err, "DPUNodeMaintenanceDeleting", err.Error()))
			return *state, nil
		}
		// If DPU is not in Requestor, add it
		if !isDPUInRequestor(dpunodemaintenance, dpu.Name) {
			if err := addRequestorAndUpdateForce(ctx, ctrlCtx.Client, dpunodemaintenance, dpu.Name, *dpu.Spec.NodeEffect.Force); err != nil {
				cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondNodeEffectReady.String(), err, "FailedAddRequestor", err.Error()))
				return *state, err
			}
		}
		// checking if node effect is applied, if not, reconsile again
		if !cutil.IsNodeEffectApplied(dpunodemaintenance) {
			err = fmt.Errorf("node effect is in progress")
			logger.V(3).Info(err.Error())
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondNodeEffectReady.String(), err, "NodeEffectInProgress", err.Error()))
			return *state, nil
		}
	}

	return handleNodeEffectCompletion(ctx, state, "NodeEffectCompleted")
}

// handleNodeEffectCompletion handles the common logic for completing a node effect
func handleNodeEffectCompletion(ctx context.Context, state *provisioningv1.DPUStatus, reason string) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)

	// Set the condition to ready with the specific reason
	cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondNodeEffectReady, reason, ""))

	// Check for post-provisioning node effect status field
	if state.PostProvisioningNodeEffect != nil && *state.PostProvisioningNodeEffect {
		// Clear the status field and transition to DPUClusterConfig state
		state.PostProvisioningNodeEffect = nil
		state.Phase = provisioningv1.DPUClusterConfig
		logger.V(3).Info("Post-provisioning node effect completed, transitioning to DPUClusterConfig")
		return *state, nil
	}

	// Default transition to next phase
	state.Phase = provisioningv1.DPUInitializeInterface
	return *state, nil
}

func createDPUNodeMaintenance(ctx context.Context, k8sClient client.Client, name string, dpu *provisioningv1.DPU) error {
	logger := log.FromContext(ctx)
	if dpu.Spec.NodeEffect == nil {
		return fmt.Errorf("node effect is nil")
	}
	requestors := make([]string, 0)
	if dpu.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors != nil {
		requestors = dpu.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors
	}

	jsonStr, err := cutil.MarshalJSON(requestors)
	if err != nil {
		return fmt.Errorf("failed to marshal node maintenance additional requestors: %w", err)
	}
	dpunodemaintenance := &provisioningv1.DPUNodeMaintenance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: dpu.Namespace,
			Annotations: map[string]string{
				// this annotation is only used to store the service additional requestors
				cutil.LastAppliedNodeMaintenanceAdditionalRequestorsOnDPUKey: jsonStr,
			},
		},
		Spec: provisioningv1.DPUNodeMaintenanceSpec{
			DPUNodeName: dpu.Spec.DPUNodeName,
			NodeEffect:  dpu.Spec.NodeEffect.DeepCopy(),
			Requestor:   requestors,
		},
	}

	// clear NodeMaintenanceAdditionalRequestors from the spec as it's managed separately via the Requestor field
	dpunodemaintenance.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors = []string{}
	// append DPU name to Requestor
	dpunodemaintenance.Spec.Requestor = append(dpunodemaintenance.Spec.Requestor, dpu.Name)
	if err := k8sClient.Create(ctx, dpunodemaintenance); err != nil {
		return err
	}
	logger.V(3).Info(fmt.Sprintf("Successfully created DPUNodeMaintenance (%s/%s) object", dpunodemaintenance.Namespace, dpunodemaintenance.Name))
	return nil
}

// addRequestorAndUpdateForce adds the requestor to the DPUNodeMaintenance CR.
// if dpu.Spec.NodeEffect.Force is true, and dpunodemaintenance.Spec.NodeEffect.Force is false, update dpunodemaintenance.Spec.NodeEffect.Force to true. Because true is stronger than false
// setting force to true is used for critical service update, it means the node effect should be applied immediately.
func addRequestorAndUpdateForce(ctx context.Context, k8sClient client.Client, dpunodemaintenance *provisioningv1.DPUNodeMaintenance, requestor string, force bool) error {
	originalDPUNodeMaintenance := dpunodemaintenance.DeepCopy()
	found := false
	for _, r := range dpunodemaintenance.Spec.Requestor {
		if r == requestor {
			found = true
			break
		}
	}
	if !found {
		if force && dpunodemaintenance.Spec.NodeEffect.Force != nil && !*dpunodemaintenance.Spec.NodeEffect.Force {
			dpunodemaintenance.Spec.NodeEffect.Force = &force
		}
		dpunodemaintenance.Spec.Requestor = append(dpunodemaintenance.Spec.Requestor, requestor)
		patch := client.MergeFrom(originalDPUNodeMaintenance)
		if err := k8sClient.Patch(ctx, dpunodemaintenance, patch); err != nil {
			return fmt.Errorf("failed to patch dpunodemaintenance %s, err: %v", originalDPUNodeMaintenance.Name, err)
		}
	}

	return nil
}

func isDPUInRequestor(dpunodemaintenance *provisioningv1.DPUNodeMaintenance, dpuName string) bool {
	for _, r := range dpunodemaintenance.Spec.Requestor {
		if r == dpuName {
			return true
		}
	}
	return false
}
