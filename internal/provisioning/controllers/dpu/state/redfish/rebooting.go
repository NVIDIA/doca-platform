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

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	rc "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	dpuOSRunningState             string = "OsIsRunning"
	hostlessRebootTriggered       string = "RedfishGracefulRestartTriggered"
	hostlessRebootWaiting         string = "WaitingForDPUOSRunning"
	hostlessRebootWaitingForAgent string = "WaitingForDPUAgentRestarted"
)

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
		method := provisioningv1.RebootMethodHostlessDPUReboot
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
		if _, err := client.GracefulRestartDPUArm(); err != nil {
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondRebooted.String(), err, "FailedToTriggerRedfishGracefulRestart", err.Error()))
			return *state, err
		}
		dutil.UpdateRebootStatus(state, provisioningv1.RebootStatusPending, hostlessRebootTriggered, "Redfish GracefulRestart triggered for hostless DPU")
		setHostlessRebootPendingCondition(state, hostlessRebootTriggered, state.RebootStatus.Message)
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

func hostlessRebootStarted(reason string) bool {
	return reason == hostlessRebootTriggered ||
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
