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

package util

import (
	"context"
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// StartRebooting performs checks common to all DPURebooting implementations.
// If done is true, the returned status is complete and callers should return it immediately.
func StartRebooting(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *ControllerContext) (*provisioningv1.DPUStatus, *provisioningv1.DPUNode, bool, error) {
	state := dpu.Status.DeepCopy()
	if !dpu.DeletionTimestamp.IsZero() {
		state.Phase = provisioningv1.DPUDeleting
		return state, nil, true, nil
	}

	dpuNode := &provisioningv1.DPUNode{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.DPUNodeName}, dpuNode); err != nil {
		if apierrors.IsNotFound(err) {
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondRebooted.String(), err, "DPUNodeNotFound", err.Error()))
			return state, nil, true, err
		}
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondRebooted.String(), err, "GetDPUNodeError", err.Error()))
		return state, nil, true, err
	}

	_, cond := cutil.GetDPUCondition(state, provisioningv1.DPUCondInterfaceInitialized.String())
	if (dpu.Status.DPUMode != provisioningv1.NicMode) && (cond == nil || cond.Status != metav1.ConditionTrue) {
		err := fmt.Errorf("trying to reboot the host before %s", provisioningv1.DPUCondInterfaceInitialized.String())
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondRebooted.String(), err, "InvalidState", err.Error()))
		return state, dpuNode, true, err
	}

	return state, dpuNode, false, nil
}

// UpdateRebootStatus updates the in-memory DPU reboot status and transition time.
func UpdateRebootStatus(state *provisioningv1.DPUStatus, phase provisioningv1.RebootStatusPhase, reason, message string) {
	changed := state.RebootStatus == nil ||
		state.RebootStatus.Phase != phase ||
		state.RebootStatus.Reason != reason ||
		state.RebootStatus.Message != message
	if state.RebootStatus == nil {
		state.RebootStatus = &provisioningv1.RebootStatus{}
	}
	state.RebootStatus.Phase = phase
	state.RebootStatus.Reason = reason
	state.RebootStatus.Message = message
	if changed {
		now := metav1.Now()
		state.RebootStatus.LastTransitionTime = &now
	}
}

// CompleteRebooting runs when the DPU is in DPURebooting and reboot is complete.
func CompleteRebooting(ctx context.Context, dpu *provisioningv1.DPU, state *provisioningv1.DPUStatus, zeroTrustMode bool) provisioningv1.DPUStatus {
	logger := log.FromContext(ctx)

	// Update the Rebooted condition based on status.rebootStatus.
	// Pending / Failed: copy Reason and Message onto DPUCondRebooted (False). Succeeded: True via DPUCondition.
	_, rebootCondition := cutil.GetDPUCondition(state, provisioningv1.DPUCondRebooted.String())
	if (rebootCondition == nil || rebootCondition.Status != metav1.ConditionTrue) && dpu.Status.RebootStatus != nil {
		switch dpu.Status.RebootStatus.Phase {
		case provisioningv1.RebootStatusSucceeded:
			rs := dpu.Status.RebootStatus
			cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondRebooted, rs.Reason, rs.Message))
			_, rebootCondition = cutil.GetDPUCondition(state, provisioningv1.DPUCondRebooted.String())
		case provisioningv1.RebootStatusPending, provisioningv1.RebootStatusFailed:
			rs := dpu.Status.RebootStatus
			reason := rs.Reason
			if reason == "" {
				reason = string(rs.Phase)
			}
			cutil.SetDPUCondition(state, &metav1.Condition{
				Type:    provisioningv1.DPUCondRebooted.String(),
				Status:  metav1.ConditionFalse,
				Reason:  reason,
				Message: rs.Message,
			})
			_, rebootCondition = cutil.GetDPUCondition(state, provisioningv1.DPUCondRebooted.String())
		}
	}

	// Wait until Rebooted is True before choosing the next provisioning phase.
	if rebootCondition == nil || rebootCondition.Status != metav1.ConditionTrue {
		return *state
	}

	var discoveryCond *metav1.Condition
	if dpu.Status.AgentStatus != nil {
		discoveryCond = meta.FindStatusCondition(dpu.Status.AgentStatus.Conditions, cutil.AgentCondRebootMethodDiscovery)
	}
	// Next provisioning phase after host reboot completes (cases 1-4 in order; first match wins).
	// 1. DPUInitializeInterface: in case the reboot was forced from DPUInitializeInterface.
	// 2. DPUFirmwareUpdate: in case the reboot was forced from DPUFirmwareUpdate.
	// 3. DPUConfig: in case the reboot was forced from DPUConfig and the reboot method is based on device query.
	// 4. DPUClusterConfig: in case the reboot method is based on boot ID and Zero Trusted mode.
	// 5. DPUHostNetworkConfiguration: in case the reboot method is based on boot ID and Host Trusted mode.
	switch {
	case dpu.Status.PreviousPhase == provisioningv1.DPUInitializeInterface:
		meta.RemoveStatusCondition(&state.Conditions, provisioningv1.DPUCondInterfaceInitialized.String())
		state.RequiredReset = nil
		state.Phase = provisioningv1.DPUInitializeInterface
	case dpu.Status.PreviousPhase == provisioningv1.DPUUpdateFirmware:
		state.Phase = provisioningv1.DPUUpdateFirmware
	case dpu.Status.PreviousPhase == provisioningv1.DPUConfig &&
		discoveryCond != nil && discoveryCond.Status == metav1.ConditionTrue:
		state.Phase = provisioningv1.DPUConfig
	case zeroTrustMode:
		state.Phase = provisioningv1.DPUClusterConfig
	default:
		state.Phase = provisioningv1.DPUHostNetworkConfiguration
	}

	logger.Info("reboot reported complete, advancing provisioning phase",
		"dpu", dpu.Name,
		"phase", state.Phase)
	return *state
}

// InitializeDPURebootStatus initializes status.rebootStatus when entering DPURebooting,
// always refreshing it to avoid carrying stale state between reboot cycles. The
// DPUConfig branch consumes the agent-reported method as-is; the host-power-cycle-required
// annotation is a Trusted Host execution-time escalation (see internal/provisioning/hostagent/phase/reboot/sync.go)
// and intentionally does not propagate into RebootStatus.Method.
func InitializeDPURebootStatus(ctx context.Context, dpu *provisioningv1.DPU, state *provisioningv1.DPUStatus, ctrlCtx *ControllerContext, sourcePhase provisioningv1.DPUPhase) error {
	// Ensure each reboot cycle starts from a clean rebooted condition state.
	meta.RemoveStatusCondition(&state.Conditions, provisioningv1.DPUCondRebooted.String())

	now := metav1.Now()
	reason := "RebootRequested"
	message := "reboot requested and pending execution"
	var method *provisioningv1.RebootMethodType

	switch {
	case sourcePhase == provisioningv1.DPUInitializeInterface:
		// Mode transition reboot always requires a host power cycle.
		powerCycle := provisioningv1.RebootMethodPowerCycle
		method = &powerCycle
		reason = "ModeUpdateRequiresPowerCycle"
		message = "host power cycle required to apply DPU mode transition"
	case sourcePhase == provisioningv1.DPUConfig && dpu.Status.AgentStatus != nil:
		method = dpu.Status.AgentStatus.RebootMethod
		if agentCond := meta.FindStatusCondition(dpu.Status.AgentStatus.Conditions, cutil.AgentCondRebootMethodDiscovery); agentCond != nil {
			if agentCond.Reason != "" {
				reason = agentCond.Reason
			}
			if agentCond.Message != "" {
				message = agentCond.Message
			}
		}
	}

	// RebootStatus must always carry a method. For DPUConfig, method is expected from agent discovery;
	// for mode update from InitializeInterface, PowerCycle is explicitly set above.
	if method == nil || *method == provisioningv1.RebootMethodUnknown {
		err := fmt.Errorf("failed to initialize reboot status: reboot method is unresolved (sourcePhase=%s)", sourcePhase)
		log.FromContext(ctx).Info(
			"Cannot initialize RebootStatus: unresolved reboot method",
			"severity", "warning",
			"dpu", dpu.Name,
			"namespace", dpu.Namespace,
			"previousPhase", sourcePhase,
			"agentStatus", dpu.Status.AgentStatus,
			"error", err.Error(),
		)
		return err
	}

	state.RebootStatus = &provisioningv1.RebootStatus{
		Phase:              provisioningv1.RebootStatusPending,
		Method:             method,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: &now,
	}
	return nil
}
