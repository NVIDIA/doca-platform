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

	"k8s.io/apimachinery/pkg/api/meta"
)

const (
	hostOSInitReleaseReasonAwaitingAgent = "AwaitingAgent"
)

func HostOSInitRelease(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	state := dpu.Status.DeepCopy()

	if !dpu.DeletionTimestamp.IsZero() {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	if !hasHostOSInitReleaseCondition(state) {
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondHostOSInitRelease.String(),
			fmt.Errorf("waiting for DPU agent host OS init release"), hostOSInitReleaseReasonAwaitingAgent, ""))
	}

	hostOSInit := agentHostOSInitStatus(dpu)
	if hostOSInit != nil && (hostOSInit.Skipped != nil || hostOSInit.Succeeded != nil) {
		cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondHostOSInitRelease, "", "host OS init release completed"))
		state.Phase = provisioningv1.DPUNodeEffectRemoval
		return *state, nil
	}

	cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondHostOSInitRelease.String(),
		fmt.Errorf("waiting for DPU agent host OS init release"), hostOSInitReleaseReasonAwaitingAgent, ""))
	return *state, nil
}

func agentHostOSInitStatus(dpu *provisioningv1.DPU) *provisioningv1.HostOSInitStatus {
	if dpu.Status.AgentStatus == nil {
		return nil
	}
	return dpu.Status.AgentStatus.HostOSInit
}

func hasHostOSInitReleaseCondition(state *provisioningv1.DPUStatus) bool {
	return meta.FindStatusCondition(state.Conditions, provisioningv1.DPUCondHostOSInitRelease.String()) != nil
}
