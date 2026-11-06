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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const nodeEffectRemovalTimeoutReason = "NodeEffectRemovalTimeout"

func Error(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	state := dpu.Status.DeepCopy()
	if !dpu.DeletionTimestamp.IsZero() {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	ctrlCtx.DPUInProvisioningMap.Remove(dutil.DPUID(dpu.UID))

	if err := RemoveRequestorFromDPUNodeMaintenance(ctx, dpu, ctrlCtx); err != nil {
		err = fmt.Errorf("failed to remove requestor from dpunodemaintenance: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondError.String(), err, "RemoveRequestorFromDPUNodeMaintenanceError", err.Error()))
		return *state, err
	}

	resume, err := canResumeNodeEffectRemoval(ctx, dpu, ctrlCtx)
	if err != nil {
		return *state, fmt.Errorf("failed to check whether node effect removal can resume: %w", err)
	}
	if resume {
		meta.RemoveStatusCondition(&state.Conditions, provisioningv1.DPUCondNodeEffectRemoved.String())
		state.Phase = provisioningv1.DPUNodeEffectRemoval
	}

	return *state, nil
}

func canResumeNodeEffectRemoval(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (bool, error) {
	if dpu.Status.PreviousPhase != provisioningv1.DPUNodeEffectRemoval {
		return false, nil
	}

	_, condition := cutil.GetDPUCondition(&dpu.Status, provisioningv1.DPUCondNodeEffectRemoved.String())
	if condition == nil || condition.Status != metav1.ConditionFalse || condition.Reason != nodeEffectRemovalTimeoutReason {
		return false, nil
	}

	maintenanceName, err := cutil.GenerateDPUNodeMaintenanceObjectName(dpu.Spec.DPUNodeName, dpu.Spec.NodeEffect)
	if err != nil {
		return false, err
	}
	maintenance := &provisioningv1.DPUNodeMaintenance{}
	if err := ctrlCtx.Client.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: maintenanceName}, maintenance); err != nil {
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	}
	if !maintenance.DeletionTimestamp.IsZero() {
		return true, nil
	}

	// Resume only after the other maintenance requestors have released the node.
	return len(maintenance.Spec.Requestor) == 0, nil
}
