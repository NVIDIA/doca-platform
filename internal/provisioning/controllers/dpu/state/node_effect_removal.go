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

package state

import (
	"context"
	"fmt"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func NodeEffectRemoval(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)
	state := dpu.Status.DeepCopy()
	// Check deletion condition
	if !dpu.DeletionTimestamp.IsZero() {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	node, err := cutil.GetNodeFromDPUCluster(ctx, ctrlCtx.Client, dpu)
	if err != nil {
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondNodeEffectRemoved.String(), err, "GetNodeFromDPUClusterError", err.Error()))
		return *state, err
	}

	ann := dpu.Spec.Cluster.NodeAnnotations
	if ann == nil {
		ann = map[string]string{}
	}
	needUpdateLabels, err := cutil.NeedUpdateLabelsOnNodeInDPUCluster(node, dpu.Spec.Cluster.NodeLabels)
	if err != nil {
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondNodeEffectRemoved.String(), err, "UpdateLabelsOnNodeInDPUClusterError", err.Error()))
		return *state, err
	}
	needUpdateAnn, err := cutil.NeedUpdateAnnotationsOnNodeInDPUCluster(node, ann)
	if err != nil {
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondNodeEffectRemoved.String(), err, "UpdateAnnotationsOnNodeInDPUClusterError", err.Error()))
		return *state, err
	}
	if needUpdateLabels || needUpdateAnn {
		// Reset the NodeEffectRemoved condition so the removal timeout timer starts fresh
		// when we re-enter NodeEffectRemoval after the label update in ClusterConfig.
		meta.RemoveStatusCondition(&state.Conditions, provisioningv1.DPUCondNodeEffectRemoved.String())
		state.Phase = provisioningv1.DPUClusterConfig
		logger.V(3).Info(fmt.Sprintf("node %s needs to update cluster node labels or annotations", node.Name))
		return *state, nil
	}

	dpunodemaintenanceName, err := cutil.GenerateDPUNodeMaintenanceObjectName(dpu.Spec.DPUNodeName, dpu.Spec.NodeEffect)
	if err != nil {
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondNodeEffectRemoved.String(), err, "FailedGetGenerateDPUNodeMaintenanceObjectName", err.Error()))
		return *state, err
	}

	dpunodemaintenance := &provisioningv1.DPUNodeMaintenance{}
	key := types.NamespacedName{Namespace: dpu.Namespace, Name: dpunodemaintenanceName}
	if err := ctrlCtx.Client.Get(ctx, key, dpunodemaintenance); err != nil {
		if apierrors.IsNotFound(err) {
			cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondNodeEffectRemoved, "", ""))
			state.Phase = provisioningv1.DPUReady
			return *state, nil
		} else {
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondNodeEffectRemoved.String(), err, "FailedGetDPUNodeMaintenanceObject", err.Error()))
			return *state, err
		}
	} else {
		if err := RemoveRequestorFromDPUNodeMaintenance(ctx, dpu, ctrlCtx); err != nil {
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondNodeEffectRemoved.String(), err, "FailedRemoveRequestor", err.Error()))
			return *state, err
		}

		// Set InProgress condition before the timeout check so that LastTransitionTime
		// is reset when entering a new removal cycle (Status changes from True to False),
		// preventing stale timestamps from previous cycles from causing false timeouts.
		inProgressErr := fmt.Errorf("node effect removal is in progress")
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondNodeEffectRemoved.String(), inProgressErr, "NodeEffectRemovalInProgress", inProgressErr.Error()))

		// Check timeout only when the DPUNodeMaintenance still has requestors (not in deletion).
		// If it is in deletion phase, the normal flow will either succeed or fail on its own.
		if dpunodemaintenance.DeletionTimestamp.IsZero() {
			if err := checkNodeEffectRemovalTimeout(state, ctrlCtx.Options.NodeEffectRemovalTimeout, logger); err != nil {
				cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondNodeEffectRemoved.String(), err, "NodeEffectRemovalTimeout", err.Error()))
				state.Phase = provisioningv1.DPUError
				return *state, nil
			}
		}
	}
	return *state, nil
}

// checkNodeEffectRemovalTimeout checks if the Node Effect Removal phase has exceeded the configured timeout.
// Returns an error if timeout is exceeded, nil otherwise.
func checkNodeEffectRemovalTimeout(state *provisioningv1.DPUStatus, timeout time.Duration, logger logr.Logger) error {
	if timeout <= 0 {
		return nil
	}

	_, cond := cutil.GetDPUCondition(state, provisioningv1.DPUCondNodeEffectRemoved.String())
	if cond == nil {
		return nil
	}

	elapsed := time.Since(cond.LastTransitionTime.Time)
	if elapsed <= timeout {
		return nil
	}

	logger.Info("Node effect removal timeout exceeded", "elapsed", elapsed, "timeout", timeout)
	return fmt.Errorf("node effect removal timeout exceeded: %v > %v", elapsed, timeout)
}

func RemoveRequestorFromDPUNodeMaintenance(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) error {
	dpunodemaintenanceName, err := cutil.GenerateDPUNodeMaintenanceObjectName(dpu.Spec.DPUNodeName, dpu.Spec.NodeEffect)
	if err != nil {
		return err
	}

	dpunodemaintenance := &provisioningv1.DPUNodeMaintenance{}
	key := types.NamespacedName{Namespace: dpu.Namespace, Name: dpunodemaintenanceName}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := ctrlCtx.Client.Get(ctx, key, dpunodemaintenance); err != nil {
			if !apierrors.IsNotFound(err) {
				return err
			}
			// If DPUNodeMaintenance object doesn't exist, return nil
			return nil
		}
		// mutate obj.Spec.Requestor
		newRequestors := []string{}
		for _, r := range dpunodemaintenance.Spec.Requestor {
			if r != dpu.Name {
				newRequestors = append(newRequestors, r)
			}
		}
		if len(newRequestors) != len(dpunodemaintenance.Spec.Requestor) {
			dpunodemaintenance.Spec.Requestor = newRequestors
			return ctrlCtx.Client.Update(ctx, dpunodemaintenance)
		}
		return nil
	})
}
