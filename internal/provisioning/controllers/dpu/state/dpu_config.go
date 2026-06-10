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
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func DPUConfig(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)
	state := dpu.Status.DeepCopy()

	if !dpu.DeletionTimestamp.IsZero() {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	// Apply the agent-startup timeout only while handling the initial Installing ->
	// DPUConfig handoff. If the controller restarts after that handoff, the agent may
	// already have reported LastStartupTime, so do not treat the old OSInstalled
	// transition as an agent-startup timeout.
	if state.PreviousPhase == provisioningv1.DPUOSInstalling &&
		(state.AgentStatus == nil || state.AgentStatus.LastStartupTime == nil) {
		_, cond := cutil.GetDPUCondition(state, provisioningv1.DPUCondOSInstalled.String())
		if cond != nil && time.Since(cond.LastTransitionTime.Time) > 20*time.Minute {
			err := fmt.Errorf("DPU agent did not report startup within 20 minutes after OS installation completed")
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondDPUConfig.String(), err, "DPUAgentNotStarted", err.Error()))
			state.Phase = provisioningv1.DPUError
			return *state, nil
		}
	}

	if dpu.Status.AgentStatus == nil || dpu.Status.AgentStatus.RebootMethod == nil || *dpu.Status.AgentStatus.RebootMethod == provisioningv1.RebootMethodUnknown {
		logger.Info("Waiting for DPU agent to report reboot method")
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondDPUConfig.String(),
			fmt.Errorf("waiting for DPU agent to report reboot method"), "WaitingForRebootMethod", ""))
		return *state, nil
	}

	// The DPU may go through multiple reboots. Compare the last recorded startup time
	// with the current one to ensure the RebootMethod we see is freshly reported by the
	// DPU Agent after its latest reboot, not a stale value from a previous cycle.
	if state.AgentLastStartupTime != nil && state.AgentLastStartupTime.Equal(dpu.Status.AgentStatus.LastStartupTime) {
		logger.Info("The RebootMethod in AgentStatus is from the previous reboot, waiting for the DPU agent to report the reboot method for the current reboot")
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondDPUConfig.String(),
			fmt.Errorf("the RebootMethod in AgentStatus is from the previous reboot, waiting for the DPU agent to report the reboot method for the current reboot"), "WaitingForFreshRebootMethod", ""))
		return *state, nil
	}

	state.AgentLastStartupTime = dpu.Status.AgentStatus.LastStartupTime
	rm := *dpu.Status.AgentStatus.RebootMethod
	logger.Info("DPUConfig: agent reboot method", "dpu", dpu.Name, "namespace", dpu.Namespace, "rebootMethod", rm)
	switch rm {
	case provisioningv1.RebootMethodNoAction:
		if ctrlCtx.Options.ZeroTrustProvisioningFlow() {
			state.Phase = provisioningv1.DPUClusterConfig
		} else {
			state.Phase = provisioningv1.DPUHostNetworkConfiguration
		}
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondDPUConfig.String(), nil,
			string(rm), fmt.Sprintf("RebootMethod is %s; transitioning to %s phase", rm, state.Phase)))
	case provisioningv1.RebootMethodFirmwareReset, provisioningv1.RebootMethodDPUWarmReboot:
		logger.Info("DPU OS is rebooting, staying in DPUConfig phase")
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondDPUConfig.String(),
			fmt.Errorf("DPU OS is rebooting, staying in DPUConfig phase"), string(rm), ""))
	default:
		// Enter each host reboot cycle with a fresh condition so Rebooting does not
		// accidentally treat a previous reboot as already completed.
		meta.RemoveStatusCondition(&state.Conditions, provisioningv1.DPUCondRebooted.String())
		state.Phase = provisioningv1.DPURebooting
		if err := dutil.InitializeDPURebootStatus(ctx, dpu, state, ctrlCtx, provisioningv1.DPUConfig); err != nil {
			state.Phase = provisioningv1.DPUConfig
			err = fmt.Errorf("failed to initialize reboot status: %w", err)
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondDPUConfig.String(), err, "FailedToInitializeRebootStatus", err.Error()))
			return *state, nil
		}
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondDPUConfig.String(), nil,
			string(rm), fmt.Sprintf("RebootMethod is %s; transitioning to %s phase", rm, state.Phase)))
	}

	return *state, nil
}
