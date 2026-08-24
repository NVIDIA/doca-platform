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

package redfish

import (
	"context"
	"fmt"
	"net/http"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	rc "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	dpuOSRunningState              string = "OsIsRunning"
	hostlessSOCForceResetTriggered string = "RedfishSOCForceResetTriggered"
	hostlessRebootWaiting          string = "WaitingForDPUOSRunning"
	hostlessRebootWaitingForAgent  string = "WaitingForDPUAgentRestarted"
	// hostlessRebootTriggeredLegacy reasons are kept so in-flight hostless reboots
	// that already stamped an older trigger continue to wait for OS/agent.
	hostlessRebootTriggeredLegacy       string = "RedfishGracefulRestartTriggered"
	hostlessForceRestartTriggeredLegacy string = "RedfishForceRestartTriggered"

	// armShutdownWaitTimeout bounds how long we wait for the DPU Arm to report a
	// powered-off state after a System Level Reset shutdown. The DPU agent's graceful
	// shutdown completes within seconds; if the BMC has not reported an off state by
	// this deadline we proceed with the host reboot anyway rather than block
	// provisioning indefinitely.
	armShutdownWaitTimeout = 5 * time.Minute
)

// isDPUArmPoweredOff reports whether the Redfish system state indicates the DPU Arm has
// completed its graceful shutdown. On BF4 a shut-down Arm reports PowerState "Paused"
// ("Off" is accepted too); Status.State "StandbyOffline" is the purpose-built offline
// signal and is checked in addition.
func isDPUArmPoweredOff(system *rc.SystemInfo) bool {
	if system == nil {
		return false
	}
	switch system.PowerState {
	case "Paused", "Off":
		return true
	}
	return system.Status.State == "StandbyOffline"
}

func Rebooting(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)

	state, dpuNode, done, err := dutil.StartRebooting(ctx, dpu, ctrlCtx)
	if done || err != nil {
		return *state, err
	}

	if dpu.Status.Hostless {
		return reconcileHostlessReboot(ctx, dpu, state, ctrlCtx)
	}

	skipHW, err := cutil.ShouldSkipHWProvisioning(ctx, ctrlCtx.Client, dpu)
	if err != nil {
		logger.V(3).Info("Failed to check skip-hw-provisioning label, assuming real hardware", "error", err)
	}
	if skipHW {
		logger.Info("skip-hw-provisioning label set - skipping power cycle")
		cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondRebooted, "", ""))
		state.Phase = provisioningv1.DPUClusterConfig
		return *state, nil
	}

	switch {
	case dpuNode.Spec.NodeRebootMethod == nil:
		err := fmt.Errorf("DPUNode %s has no node reboot method", dpuNode.Name)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondRebooted.String(), err, "NodeRebootMethodNotProvided", err.Error()))
		state.Phase = provisioningv1.DPUError
		return *state, nil
	case dpuNode.Spec.NodeRebootMethod.GNOI != nil || dpuNode.Spec.NodeRebootMethod.HostAgent != nil: //nolint:staticcheck // GNOI is deprecated but invalid for Redfish.
		err := fmt.Errorf("DPUNode %s uses a host-agent reboot method, which is not supported by the Redfish reboot handler", dpuNode.Name)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondRebooted.String(), err, "HostAgentRebootMethodNotSupported", err.Error()))
		state.Phase = provisioningv1.DPUError
		return *state, nil
	case dpuNode.Spec.NodeRebootMethod.External != nil || dpuNode.Spec.NodeRebootMethod.Script != nil:
		// For a System Level Reset the DPU agent shuts the Arm down itself. Hold the host
		// reboot until the Arm has powered off so the External/Script reboot does not power cycle the host.
		if state.RebootStatus != nil && state.RebootStatus.Phase == provisioningv1.RebootStatusWaitForShutdown {
			return reconcileWaitForArmShutdown(ctx, dpu, state, ctrlCtx)
		}
		return dutil.CompleteRebooting(ctx, dpu, state, ctrlCtx.Options.ZeroTrustProvisioningFlow()), nil
	case dpuNode.Spec.NodeRebootMethod.None != nil:
		err := fmt.Errorf("DPUNode %s uses nodeRebootMethod none, but DPU %s is not marked hostless", dpuNode.Name, dpu.Name)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondRebooted.String(), err, "HostlessStatusNotSet", err.Error()))
		state.Phase = provisioningv1.DPUError
		return *state, nil
	default:
		err := fmt.Errorf("DPUNode %s has an unsupported node reboot method", dpuNode.Name)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondRebooted.String(), err, "UnsupportedNodeRebootMethod", err.Error()))
		state.Phase = provisioningv1.DPUError
		return *state, nil
	}
}

func reconcileHostlessReboot(ctx context.Context, dpu *provisioningv1.DPU, state *provisioningv1.DPUStatus, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	client, err := redfishClientForDPU(ctx, dpu, ctrlCtx)
	if err != nil {
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondRebooted.String(), err, "FailedToCreateClient", err.Error()))
		return *state, err
	}

	if state.RebootStatus == nil {
		now := metav1.Now()
		method := provisioningv1.RebootMethodPowerCycle
		state.RebootStatus = &provisioningv1.RebootStatus{
			Phase:              provisioningv1.RebootStatusPending,
			Method:             &method,
			Reason:             "RebootRequested",
			Message:            "hostless DPU reboot requested",
			LastTransitionTime: &now,
		}
	}

	if state.RebootStatus.Phase != provisioningv1.RebootStatusPending {
		return dutil.CompleteRebooting(ctx, dpu, state, ctrlCtx.Options.ZeroTrustProvisioningFlow()), nil
	}

	if !hostlessRebootStarted(state.RebootStatus.Reason) {
		if _, err := client.ForceResetSOC(); err != nil {
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondRebooted.String(), err, "FailedToTriggerRedfishSOCForceReset", err.Error()))
			return *state, err
		}
		dutil.UpdateRebootStatus(state, provisioningv1.RebootStatusPending, hostlessSOCForceResetTriggered,
			"Redfish SOC.ForceReset triggered for hostless DPU")
		setHostlessRebootPendingCondition(state, state.RebootStatus.Reason, state.RebootStatus.Message)
		return *state, nil
	}

	resp, system, err := client.GetSystem()
	if err != nil {
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondRebooted.String(), err, "FailedToGetRedfishSystem", err.Error()))
		return *state, err
	}
	if resp.StatusCode() != http.StatusOK {
		err := fmt.Errorf("failed to get Redfish system status: %s", resp.Status())
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondRebooted.String(), err, "UnexpectedRedfishSystemStatus", err.Error()))
		return *state, err
	}
	if system.BootProgress.OemLastState != dpuOSRunningState {
		dutil.UpdateRebootStatus(state, provisioningv1.RebootStatusPending, hostlessRebootWaiting,
			fmt.Sprintf("waiting for DPU OS to report %q, current state is %q", dpuOSRunningState, system.BootProgress.OemLastState))
		setHostlessRebootPendingCondition(state, hostlessRebootWaiting, state.RebootStatus.Message)
		return *state, nil
	}
	if !hasFreshDPUAgentStartup(dpu) {
		dutil.UpdateRebootStatus(state, provisioningv1.RebootStatusPending, hostlessRebootWaitingForAgent,
			"waiting for DPU agent to report a fresh startup after Redfish reboot")
		setHostlessRebootPendingCondition(state, hostlessRebootWaitingForAgent, state.RebootStatus.Message)
		return *state, nil
	}

	dutil.UpdateRebootStatus(state, provisioningv1.RebootStatusSucceeded, "", "")
	cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondRebooted, "", ""))
	return dutil.CompleteRebooting(ctx, dpu, state, ctrlCtx.Options.ZeroTrustProvisioningFlow()), nil
}

// reconcileWaitForArmShutdown holds the External/Script host reboot until the DPU Arm
// has completed its graceful (soft) shutdown. It polls the DPU-BMC over Redfish and
// releases the gate (RebootStatus -> Pending) once the ComputerSystem reports the Arm is
// off, using either PowerState (e.g. "Paused") or Status.State == "StandbyOffline". The
// DPUNode controller only triggers the host reboot once the status is Pending, so this
// prevents power cycling the host while a System Level Reset shutdown is still in flight.
// As a safety net, if the Arm has not reported a powered-off state within
// armShutdownWaitTimeout, the gate is released anyway so provisioning is not blocked
// indefinitely.
func reconcileWaitForArmShutdown(ctx context.Context, dpu *provisioningv1.DPU, state *provisioningv1.DPUStatus, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)

	client, err := redfishClientForDPU(ctx, dpu, ctrlCtx)
	if err != nil {
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondRebooted.String(), err, "FailedToCreateClient", err.Error()))
		return *state, err
	}

	resp, system, err := client.GetSystem()
	if err != nil {
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondRebooted.String(), err, "FailedToGetRedfishSystem", err.Error()))
		return *state, err
	}
	if resp.StatusCode() != http.StatusOK {
		err := fmt.Errorf("failed to get Redfish system status: %s", resp.Status())
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondRebooted.String(), err, "UnexpectedRedfishSystemStatus", err.Error()))
		return *state, err
	}

	if !isDPUArmPoweredOff(system) {
		logger.V(3).Info("waiting for DPU Arm to power off", "dpu", dpu.Name,
			"powerState", system.PowerState, "statusState", system.Status.State)
		// Time out relative to when we entered the WaitForShutdown phase, using
		// RebootStatus.LastTransitionTime as the anchor.
		timedOut := state.RebootStatus != nil && state.RebootStatus.LastTransitionTime != nil &&
			time.Since(state.RebootStatus.LastTransitionTime.Time) > armShutdownWaitTimeout
		if timedOut {
			logger.Info("timed out waiting for DPU Arm to report a powered-off state; proceeding with host reboot",
				"severity", "warning", "dpu", dpu.Name, "powerState", system.PowerState,
				"statusState", system.Status.State, "timeout", armShutdownWaitTimeout.String())
			dutil.UpdateRebootStatus(state, provisioningv1.RebootStatusPending, "DPUArmShutdownWaitTimeout",
				fmt.Sprintf("timed out after %s waiting for DPU Arm to power off (last PowerState %q, Status.State %q); proceeding with host reboot",
					armShutdownWaitTimeout, system.PowerState, system.Status.State))
			return *state, nil
		}
		// Keep the message stable (do not embed the changing PowerState) so
		// RebootStatus.LastTransitionTime stays anchored for the timeout check.
		waitErr := fmt.Errorf("waiting for DPU Arm to power off after System Level Reset shutdown")
		dutil.UpdateRebootStatus(state, provisioningv1.RebootStatusWaitForShutdown, "WaitingForDPUArmShutdown", waitErr.Error())
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondRebooted.String(), waitErr, "WaitingForDPUArmShutdown", ""))
		return *state, nil
	}

	// Arm is off: release the gate to Pending. The DPUNode controller then triggers the
	// External/Script host reboot, and CompleteRebooting derives DPUCondRebooted from the
	// Pending phase on the next reconcile.
	logger.Info("DPU Arm reported powered off; releasing host reboot gate", "dpu", dpu.Name,
		"powerState", system.PowerState, "statusState", system.Status.State)
	dutil.UpdateRebootStatus(state, provisioningv1.RebootStatusPending, "DPUArmPoweredOff",
		"DPU Arm reported powered off; releasing host reboot")
	return *state, nil
}

func hostlessRebootStarted(reason string) bool {
	return reason == hostlessSOCForceResetTriggered ||
		reason == hostlessForceRestartTriggeredLegacy ||
		reason == hostlessRebootTriggeredLegacy ||
		reason == hostlessRebootWaiting ||
		reason == hostlessRebootWaitingForAgent
}

func setHostlessRebootPendingCondition(state *provisioningv1.DPUStatus, reason, message string) {
	cutil.SetDPUCondition(state, &metav1.Condition{
		Type:    provisioningv1.DPUCondRebooted.String(),
		Status:  metav1.ConditionFalse,
		Reason:  reason,
		Message: message,
	})
}

func hasFreshDPUAgentStartup(dpu *provisioningv1.DPU) bool {
	if dpu.Status.AgentLastStartupTime == nil ||
		dpu.Status.AgentStatus == nil ||
		dpu.Status.AgentStatus.LastStartupTime == nil {
		return false
	}
	return !dpu.Status.AgentStatus.LastStartupTime.Equal(dpu.Status.AgentLastStartupTime)
}

func redfishClientForDPU(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (*rc.Client, error) {
	dpuDevice := &provisioningv1.DPUDevice{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.DPUDeviceName}, dpuDevice); err != nil {
		return nil, err
	}
	return rc.NewTLSClient(ctx, dpuDevice.BMCAddress(), dpu.Namespace, ctrlCtx.Client)
}
