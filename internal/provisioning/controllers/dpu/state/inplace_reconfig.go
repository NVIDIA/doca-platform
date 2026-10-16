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
	"strconv"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

const (
	// reconfigPendingReason is set on Ready=False and on DPUReconfiguring=True
	// while an in-place jump is in progress.
	reconfigPendingReason = "ReconfigPending"
	// reconfigCompletedReason is set on DPUReconfiguring=False once the DPU is
	// Ready again for the round recorded in the condition message.
	reconfigCompletedReason = "ReconfigCompleted"
)

// StartInPlaceReconfig leaves a jumpable phase for Node Effect when DPUSet has
// set dpuReconfig.inProgress. The returned status is used instead of the phase
// handler. Node Effect itself is not redirected here; that handler keeps the
// phase and only sets postProvisioningDPUConfig.
//
// DPUReconfiguring stays True, with this round in its message, from the jump
// until Ready. Phases after DPU Config still have inProgress set, and must run
// instead of jumping back to Node Effect. A later reconcile of Ready records
// the round on DPUReconfiguring=False so it is not jumped again either.
func StartInPlaceReconfig(dpu *provisioningv1.DPU) (provisioningv1.DPUStatus, bool) {
	if !dpu.DeletionTimestamp.IsZero() || !reconfigInProgress(dpu) {
		return provisioningv1.DPUStatus{}, false
	}
	if !phaseJumpsForInPlace(dpu.Status.Phase) {
		return provisioningv1.DPUStatus{}, false
	}
	round := reconfigRound(dpu)
	if inPlaceAlreadyReturned(&dpu.Status, round) || inPlaceRoundInFlight(&dpu.Status, round) {
		return provisioningv1.DPUStatus{}, false
	}
	state := dpu.Status.DeepCopy()
	applyInPlaceJump(state)
	updateFalseDPUCondReady(state, reconfigPendingReason, "in-place reconfig")
	cutil.SetDPUCondition(state, &metav1.Condition{
		Type:    provisioningv1.DPUCondReconfiguring.String(),
		Status:  metav1.ConditionTrue,
		Reason:  reconfigPendingReason,
		Message: strconv.FormatInt(int64(round), 10),
	})
	return *state, true
}

// applyInPlaceJump moves the DPU to Node Effect for an in-place reconfig.
// postProvisioningNodeEffect is cleared so Node Effect exits to DPU Config.
func applyInPlaceJump(state *provisioningv1.DPUStatus) {
	state.PostProvisioningDPUConfig = ptr.To(true)
	state.PostProvisioningNodeEffect = nil
	state.Phase = provisioningv1.DPUNodeEffect
}

// armInPlaceNodeEffect records an in-place exit on a DPU that is already in
// Node Effect. The phase stays Node Effect. The caller writes DPUReconfiguring.
func armInPlaceNodeEffect(state *provisioningv1.DPUStatus) {
	state.PostProvisioningDPUConfig = ptr.To(true)
	state.PostProvisioningNodeEffect = nil
}

// reconfigInProgress reports whether DPUSet has set dpuReconfig.inProgress.
// A nil status or a nil pointer is not in progress.
func reconfigInProgress(dpu *provisioningv1.DPU) bool {
	status := dpu.Status.DPUReconfig
	return status != nil && status.InProgress != nil && *status.InProgress
}

// reconfigRound returns the DPUSet round stored on this DPU, or 0 when none is set.
func reconfigRound(dpu *provisioningv1.DPU) int32 {
	if dpu.Status.DPUReconfig == nil {
		return 0
	}
	return dpu.Status.DPUReconfig.Round
}

// phaseJumpsForInPlace reports phases that leave for Node Effect when an
// in-place round is marked and has not already started.
func phaseJumpsForInPlace(phase provisioningv1.DPUPhase) bool {
	switch phase {
	case provisioningv1.DPUReady,
		provisioningv1.DPUHostNetworkConfiguration,
		provisioningv1.DPUClusterConfig,
		provisioningv1.DPUServiceReadiness,
		provisioningv1.DPUNodeEffectRemoval:
		return true
	default:
		return false
	}
}

// inPlaceRoundInFlight reports that this round already jumped to Node Effect.
// The condition stays True through DPU Config and the phases that follow it.
func inPlaceRoundInFlight(state *provisioningv1.DPUStatus, round int32) bool {
	_, cond := cutil.GetDPUCondition(state, provisioningv1.DPUCondReconfiguring.String())
	if cond == nil || cond.Status != metav1.ConditionTrue {
		return false
	}
	return cond.Message == strconv.FormatInt(int64(round), 10)
}

// inPlaceAlreadyReturned reports that this round already came back to Ready.
// The condition message is the round number recorded at the jump.
func inPlaceAlreadyReturned(state *provisioningv1.DPUStatus, round int32) bool {
	_, cond := cutil.GetDPUCondition(state, provisioningv1.DPUCondReconfiguring.String())
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != reconfigCompletedReason {
		return false
	}
	return cond.Message == strconv.FormatInt(int64(round), 10)
}
