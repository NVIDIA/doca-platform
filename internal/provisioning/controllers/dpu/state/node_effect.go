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
	"strconv"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/utils/tracing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
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

	// An in-place mark while already in Node Effect stays here. The flag makes
	// completion exit to DPU Config. A label-driven postProvisioningNodeEffect
	// is dropped so the two exits are not both set.
	if reconfigInProgress(dpu) {
		armInPlaceNodeEffect(state)
		cutil.SetDPUCondition(state, &metav1.Condition{
			Type:    provisioningv1.DPUCondReconfiguring.String(),
			Status:  metav1.ConditionTrue,
			Reason:  reconfigPendingReason,
			Message: strconv.FormatInt(int64(reconfigRound(dpu)), 10),
		})
	}

	// Hold a DPU with a hold node effect in Node Effect while a DPF upgrade is in progress, so it does not
	// create or join a DPUNodeMaintenance and does not move on to provisioning against a half upgraded
	// deployment. An external system may release the hold only once the upgrade completed, so waiting
	// for such a DPU would deadlock the upgrade. Other node effects complete on their own and the
	// upgrade waits for them instead.
	if dpu.Spec.NodeEffect.IsHold() && ctrlCtx.DPFOperatorConfig != nil && ctrlCtx.DPFOperatorConfig.UpgradeInProgress() {
		hold, err := holdForUpgrade(ctx, ctrlCtx, dpu)
		if err != nil {
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondNodeEffectReady.String(), err, "FailedGetDPUNodeMaintenance", err.Error()))
			return *state, err
		}
		if hold {
			err := fmt.Errorf("DPF Operator upgrade from version %s is in progress", ptr.Deref(ctrlCtx.DPFOperatorConfig.Status.Version, ""))
			logger.Info("Waiting for the DPF Operator upgrade to complete before progressing the node effect")
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondNodeEffectReady.String(), err, cutil.ReasonDPFOperatorUpgradeInProgress, err.Error()))
			return *state, nil
		}
	}

	nodeEffect := dpu.Spec.NodeEffect

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
			var additionalRequestors []string
			if dpu.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors != nil {
				additionalRequestors = dpu.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors
			}

			if err := addRequestorAndUpdateForce(ctx, ctrlCtx.Client, dpunodemaintenance, dpu.Name, additionalRequestors, *dpu.Spec.NodeEffect.Force); err != nil {
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

// holdForUpgrade reports whether a DPU in Node Effect with a hold node effect is held while a DPF upgrade
// is in progress. It is held until it is a requestor of a DPUNodeMaintenance whose node effect is applied,
// i.e. whose hold was released. Such a DPU is not held: DPUs on the same DPUNode share the
// DPUNodeMaintenance, which is only deleted once all of them left it, so holding it could leave another
// DPU, and with it the upgrade, waiting in Node Effect Removal. A hold released after the component
// rollout started thus lets the DPU provision during the rollout. Releasing a hold during an upgrade is
// not supported and documented as such.
// The DPUNodeMaintenance controller is not held, it still applies a node effect it already started.
func holdForUpgrade(ctx context.Context, ctrlCtx *dutil.ControllerContext, dpu *provisioningv1.DPU) (bool, error) {
	name, err := cutil.GenerateDPUNodeMaintenanceObjectName(dpu.Spec.DPUNodeName, dpu.Spec.NodeEffect)
	if err != nil {
		// Not held, so the invalid node effect is reported below.
		return false, nil
	}
	dpunodemaintenance := &provisioningv1.DPUNodeMaintenance{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: name}, dpunodemaintenance); err != nil {
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	}
	return dpunodemaintenance.DeletionTimestamp != nil ||
		!isDPUInRequestor(dpunodemaintenance, dpu.Name) ||
		!cutil.IsNodeEffectApplied(dpunodemaintenance), nil
}

// handleNodeEffectCompletion handles the common logic for completing a node effect
func handleNodeEffectCompletion(ctx context.Context, state *provisioningv1.DPUStatus, reason string) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)

	// Set the condition to ready with the specific reason
	cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondNodeEffectReady, reason, ""))

	// In-place reconfig wins when both exit flags are set. Clear the label-driven
	// flag as well so a later Node Effect does not follow it to Cluster Config.
	if state.PostProvisioningDPUConfig != nil && *state.PostProvisioningDPUConfig {
		state.PostProvisioningDPUConfig = nil
		state.PostProvisioningNodeEffect = nil
		state.Phase = provisioningv1.DPUConfig
		// An agent restart while Ready runs Run() and advances lastStartupTime
		// without passing through DPU Config, so agentLastStartupTime stays behind.
		// Copy the current startup time here. DPU Config then waits until this
		// in-place Run reports a newer one, instead of accepting the previous NoAction.
		if state.AgentStatus != nil && state.AgentStatus.LastStartupTime != nil {
			state.AgentLastStartupTime = state.AgentStatus.LastStartupTime.DeepCopy()
		}
		logger.V(3).Info("In-place reconfig node effect completed, transitioning to DPUConfig")
		return *state, nil
	}

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
	requestors := make([]string, 0)
	if dpu.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors != nil {
		requestors = dpu.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors
	}

	jsonStr, err := cutil.MarshalJSON(requestors)
	if err != nil {
		return fmt.Errorf("failed to marshal node maintenance additional requestors: %w", err)
	}
	lastAppliedAdditionalRequestorsOnDPUKey := cutil.GenerateLastAppliedAdditionalRequestorsOnDPUAnnotationKey(dpu.Name)
	dpunodemaintenance := &provisioningv1.DPUNodeMaintenance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: dpu.Namespace,
			Labels: map[string]string{
				provisioningv1.DPUNodeNameLabel: dpu.Spec.DPUNodeName,
			},
			Annotations: map[string]string{
				// this annotation is only used to store the service additional requestors
				lastAppliedAdditionalRequestorsOnDPUKey: jsonStr,
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
	tracing.InjectIntoObject(ctx, dpunodemaintenance)
	err = tracing.InSpan(ctx, "dpu.k8s.createDPUNodeMaintenance", func(ctx context.Context) error {
		return client.IgnoreAlreadyExists(k8sClient.Create(ctx, dpunodemaintenance))
	})
	if err != nil {
		return err
	}
	logger.V(3).Info(fmt.Sprintf("Successfully ensured DPUNodeMaintenance (%s/%s) object", dpunodemaintenance.Namespace, dpunodemaintenance.Name))
	return nil
}

// addRequestorAndUpdateForce adds the requestor to the DPUNodeMaintenance CR.
// if dpu.Spec.NodeEffect.Force is true, and dpunodemaintenance.Spec.NodeEffect.Force is false, update dpunodemaintenance.Spec.NodeEffect.Force to true. Because true is stronger than false
// setting force to true is used for critical service update, it means the node effect should be applied immediately.
func addRequestorAndUpdateForce(ctx context.Context, k8sClient client.Client, dpunodemaintenance *provisioningv1.DPUNodeMaintenance, dpuRequestor string, additionalRequestors []string, force bool) error {
	key := client.ObjectKeyFromObject(dpunodemaintenance)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &provisioningv1.DPUNodeMaintenance{}
		if err := k8sClient.Get(ctx, key, current); err != nil {
			return err
		}

		if isDPUInRequestor(current, dpuRequestor) {
			return nil
		}

		original := current.DeepCopy()
		if force && current.Spec.NodeEffect.Force != nil && !*current.Spec.NodeEffect.Force {
			current.Spec.NodeEffect.Force = &force
		}
		current.Spec.Requestor = append(current.Spec.Requestor, dpuRequestor)
		current.Spec.Requestor = append(current.Spec.Requestor, additionalRequestors...)
		current.Spec.Requestor = cutil.RemoveDuplicates(current.Spec.Requestor)
		jsonStr, err := cutil.MarshalJSON(additionalRequestors)
		if err != nil {
			return fmt.Errorf("failed to marshal node maintenance additional requestors: %w", err)
		}
		lastAppliedAdditionalRequestorsOnDPUKey := cutil.GenerateLastAppliedAdditionalRequestorsOnDPUAnnotationKey(dpuRequestor)
		if current.Annotations == nil {
			current.Annotations = map[string]string{}
		}
		current.Annotations[lastAppliedAdditionalRequestorsOnDPUKey] = jsonStr
		// The requestor change reconciles the DPUNodeMaintenance, so it continues this DPU's trace.
		tracing.InjectIntoObject(ctx, current)
		patch := client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})
		if err := k8sClient.Patch(ctx, current, patch); err != nil {
			return fmt.Errorf("failed to patch dpunodemaintenance %s: %w", original.Name, err)
		}
		return nil
	})
}

func isDPUInRequestor(dpunodemaintenance *provisioningv1.DPUNodeMaintenance, dpuName string) bool {
	for _, r := range dpunodemaintenance.Spec.Requestor {
		if r == dpuName {
			return true
		}
	}
	return false
}
