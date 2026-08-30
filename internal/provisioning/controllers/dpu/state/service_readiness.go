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

package state

import (
	"context"
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	nvconfigutil "github.com/nvidia/doca-platform/internal/provisioning/utils/nvconfig"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
)

const (
	serviceReadinessReasonAwaitingAgent    = "AwaitingAgent"
	serviceReadinessReasonAwaitingServices = "AwaitingServices"
	serviceReadinessReasonFlavorNotFound   = "FlavorNotFound"
	serviceReadinessReasonFlavorError      = "GetDPUFlavorError"
)

func ServiceReadiness(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	state := dpu.Status.DeepCopy()

	if !dpu.DeletionTimestamp.IsZero() {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	flavor := &provisioningv1.DPUFlavor{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.DPUFlavor}, flavor); err != nil {
		reason := serviceReadinessReasonFlavorError
		if apierrors.IsNotFound(err) {
			reason = serviceReadinessReasonFlavorNotFound
		}
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondServiceReadiness.String(), err, reason, err.Error()))
		return *state, err
	}

	if gate := flavor.ConfiguredGate(); gate != "" && !meta.IsStatusConditionTrue(state.OperationalConditions, string(gate)) {
		err := fmt.Errorf("waiting for operational condition %s to become True", gate)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondServiceReadiness.String(),
			err, serviceReadinessReasonAwaitingServices, err.Error()))
		return *state, nil
	}

	// A nil hostOSInit is ambiguous: the agent clears it on every attempt while it holds the host.
	// Only the flavor says whether there is a hold to wait for.
	if nvconfigutil.FlavorRequestsHostOSInitHold(flavor.Spec.NVConfig) && !hostOSInitReleased(dpu) {
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondServiceReadiness.String(),
			fmt.Errorf("waiting for DPU agent host OS init release"), serviceReadinessReasonAwaitingAgent, ""))
		return *state, nil
	}

	cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondServiceReadiness, "", "service readiness completed"))
	state.Phase = provisioningv1.DPUNodeEffectRemoval
	return *state, nil
}

// hostOSInitReleased reports whether the agent recorded a terminal outcome for the host release.
func hostOSInitReleased(dpu *provisioningv1.DPU) bool {
	if dpu.Status.AgentStatus == nil || dpu.Status.AgentStatus.HostOSInit == nil {
		return false
	}
	hostOSInit := dpu.Status.AgentStatus.HostOSInit
	return hostOSInit.Skipped != nil || hostOSInit.Succeeded != nil
}
