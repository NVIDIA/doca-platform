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
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// maxFailedEWNICDevicesInMessage caps how many failed E/W NIC devices a condition
// message lists.
const maxFailedEWNICDevicesInMessage = 5

func DPUConfig(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)
	state := dpu.Status.DeepCopy()

	if !dpu.DeletionTimestamp.IsZero() {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	if dpu.Status.AgentStatus == nil || dpu.Status.AgentStatus.LastStartupTime == nil {
		logger.Info("Waiting for DPU agent contact")
		// A skewed DPU clock stalls the agent here, before it can obtain an identity, so name that
		// as the reason rather than leaving the wait unexplained.
		reason, waitErr := "WaitingForDPUAgent", fmt.Errorf("waiting for DPU agent contact")
		if skew := cutil.DPUClockSkewMessage(dpu.Status.AgentStatus); skew != "" {
			reason = cutil.ReasonDPUClockUnsynchronized
			waitErr = fmt.Errorf("waiting for DPU agent contact: %s", skew)
		}
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondDPUConfig.String(),
			waitErr, reason, ""))
		return *state, nil
	}
	if dpu.Status.AgentStatus.RebootMethod == nil || *dpu.Status.AgentStatus.RebootMethod == provisioningv1.RebootMethodUnknown {
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

	rm := *dpu.Status.AgentStatus.RebootMethod
	logger.Info("DPUConfig: agent reboot method", "dpu", dpu.Name, "namespace", dpu.Namespace, "rebootMethod", rm)

	// Astra E/W NIC runtime config runs after the rest of the agent pipeline. Do not
	// leave DPU Config on NoAction until the agent reports at least one configured E/W NIC.
	// Host-reboot methods must not wait: runtime config has not started yet.
	if rm == provisioningv1.RebootMethodNoAction {
		if ready, reason, message := ewnicRuntimeConfigReady(dpu.Status.AgentStatus.EWNICRuntimeConfig); !ready {
			logger.Info("Waiting for E/W NIC runtime configuration", "dpu", dpu.Name, "namespace", dpu.Namespace, "reason", reason)
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondDPUConfig.String(),
				fmt.Errorf("%s", message), reason, ""))
			return *state, nil
		}
	}

	state.AgentLastStartupTime = dpu.Status.AgentStatus.LastStartupTime
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

// ewnicRuntimeConfigReady reports whether E/W NIC runtime configuration is healthy
// enough for DPU Config (NoAction) to advance, and for Ready to keep DPUCondReady True.
// It is ready once at least one E/W NIC device is configured. Until then, DPU Config
// stays put, and Ready keeps the phase but clears DPUCondReady (e.g. every device
// failed a CC-termination-triggered reapply after Ready).
func ewnicRuntimeConfigReady(status *provisioningv1.EWNICRuntimeConfigStatus) (ready bool, reason, message string) {
	// The agent reports EWNICRuntimeConfig only when it runs NIC provisioning, so
	// unset means this DPU has no E/W NIC runtime config to wait for.
	if status == nil {
		return true, "", ""
	}
	configured := ptr.Deref(status.ConfiguredDevices, 0)
	discovered := ptr.Deref(status.DiscoveredDevices, 0)
	if configured >= 1 {
		return true, "", ""
	}
	summary := fmt.Sprintf("%d/%d E/W NIC devices configured", configured, discovered)
	if configured+int32(len(status.FailedDevices)) < discovered {
		return false, "WaitingForEWNICRuntimeConfig", summary
	}
	return false, "NoEWNICDeviceConfigured", summary + ": " + failedEWNICDevicesSummary(status.FailedDevices)
}

// failedEWNICDevicesSummary lists failed devices as "PCI (Reason)", capped at
// maxFailedEWNICDevicesInMessage entries.
func failedEWNICDevicesSummary(failed []provisioningv1.EWNICDeviceFailure) string {
	entries := make([]string, 0, min(len(failed), maxFailedEWNICDevicesInMessage))
	for i, device := range failed {
		if i == maxFailedEWNICDevicesInMessage {
			entries = append(entries, "...")
			break
		}
		entries = append(entries, fmt.Sprintf("%s (%s)", ptr.Deref(device.PCIAddress, ""), ptr.Deref(device.Reason, "")))
	}
	return strings.Join(entries, ", ")
}
