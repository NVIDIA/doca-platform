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

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

func DPUConfig(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)
	state := dpu.Status.DeepCopy()

	if !dpu.DeletionTimestamp.IsZero() {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	if dpu.Status.AgentStatus == nil || dpu.Status.AgentStatus.RebootMethod == nil || *dpu.Status.AgentStatus.RebootMethod == provisioningv1.RebootMethodUnknown {
		logger.Info("Waiting for DPU agent to report reboot method")
		return *state, nil
	}

	// The DPU may go through multiple reboots. Compare the last recorded startup time
	// with the current one to ensure the RebootMethod we see is freshly reported by the
	// DPU Agent after its latest reboot, not a stale value from a previous cycle.
	if state.AgentLastStartupTime != nil && state.AgentLastStartupTime.Equal(dpu.Status.AgentStatus.LastStartupTime) {
		logger.Info("The RebootMethod in AgentStatus is from the previous reboot, waiting for the DPU agent to report the reboot method for the current reboot")
		return *state, nil
	}

	state.AgentLastStartupTime = dpu.Status.AgentStatus.LastStartupTime
	switch *dpu.Status.AgentStatus.RebootMethod {
	case provisioningv1.RebootMethodNoAction:
		if ctrlCtx.Options.DPUInstallInterface == string(provisioningv1.InstallViaRedFish) {
			state.Phase = provisioningv1.DPUClusterConfig
		} else {
			state.Phase = provisioningv1.DPUHostNetworkConfiguration
		}
	default:
		state.Phase = provisioningv1.DPURebooting
	}

	return *state, nil
}
