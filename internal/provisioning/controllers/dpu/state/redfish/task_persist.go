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

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// status.redfishTaskId is one slot shared by every task-backed step (PLDM update, BF4 ISO and
// config transfer, BF3 BFB install). Two things make it easy to misread:
//   - Handlers read the DPU from the informer cache, which can lag behind status writes made
//     moments earlier.
//   - The deferred SerialPatcher writes conditions and the rest of status in separate calls, so a
//     failure in between can persist a step's condition without its task id change.
//
// The helpers below keep the slot consistent: every change of the task id is one version-locked
// status write on a DPU read straight from the API, and it carries the condition that says which
// step the id belongs to.

// Reasons a step's condition carries while that step's Redfish task is in flight. A step owns
// status.redfishTaskId only while its condition carries one of them.
const (
	reasonTaskSubmitted           = "TaskSubmitted"
	reasonTaskProgress            = "TaskProgress"
	reasonFailToCheckProgress     = "FailToCheckProgress"
	reasonFailToPersistTaskID     = "FailToPersistTaskID"
	reasonFailToPersistCompletion = "FailToPersistCompletion"
)

// reasonFailToInstall marks a step whose task failed. It is not an in-flight reason: the failed
// task must be released, not polled again.
const reasonFailToInstall = "FailToInstall"

// ownsTask reports whether step's condition marks its Redfish task as in flight.
//
// The BFB install is the only task-backed step of its phase, and entering the phase clears the
// slot, so it always owns the id. Any condition it carries, for example FailedToCreateClient,
// must not make a finished install look foreign.
//
// A step with no condition yet owns the task only if it is the first step to use the slot in its
// phase: entering the phase clears the slot, so nothing else can have put an id there. This keeps
// a task submitted by an older controller, which did not mark the step, from being resubmitted.
// The config transfer follows the ISO transfer in the same slot, so it needs an explicit marker.
func ownsTask(status *provisioningv1.DPUStatus, step provisioningv1.DPUConditionType) bool {
	if step == provisioningv1.DPUCondBFBTransferred {
		return true
	}
	_, cond := cutil.GetDPUCondition(status, string(step))
	if cond == nil {
		return step != provisioningv1.DPUCondConfigTransferred
	}
	if cond.Status == metav1.ConditionTrue {
		return false
	}
	switch cond.Reason {
	case reasonTaskSubmitted, reasonTaskProgress, reasonFailToCheckProgress, reasonFailToPersistTaskID, reasonFailToPersistCompletion:
		return true
	}
	return false
}

// inFlightCondition returns step's condition for a task the BMC just accepted.
func inFlightCondition(step provisioningv1.DPUConditionType, taskID string) *metav1.Condition {
	c := cutil.NewCondition(string(step), nil, reasonTaskSubmitted, fmt.Sprintf("Redfish task %s accepted", taskID))
	c.Status = metav1.ConditionFalse
	return c
}

// isTaskSlotFree re-reads the DPU from the API and reports whether step may submit a new Redfish
// task: the DPU is still in the phase the caller runs for, the slot is empty and step is not done.
// The done func reports whether step already finished on the API copy; the caller's cached copy can
// still show it as not done.
// Callers check it right before the BMC call, because the cached DPU they hold may predate a task
// another reconcile already recorded, or a phase change such as a move to Error.
func isTaskSlotFree(ctx context.Context, reader client.Reader, dpu *provisioningv1.DPU, done func(*provisioningv1.DPUStatus) bool) (bool, error) {
	latest := &provisioningv1.DPU{}
	if err := reader.Get(ctx, client.ObjectKeyFromObject(dpu), latest); err != nil {
		return false, fmt.Errorf("re-read DPU before submitting a Redfish task: %w", err)
	}
	if err := checkSameDPU(latest, dpu); err != nil {
		return false, err
	}
	if latest.Status.Phase != dpu.Status.Phase {
		log.FromContext(ctx).Info("cached DPU phase is behind the API; not submitting a Redfish task this reconcile",
			"cachedPhase", dpu.Status.Phase, "apiPhase", latest.Status.Phase)
		return false, nil
	}
	if latest.Status.RedfishTaskID != nil || done(&latest.Status) {
		log.FromContext(ctx).Info("cached DPU is behind the API; not submitting a Redfish task this reconcile",
			"apiTaskID", latest.Status.RedfishTaskID)
		return false, nil
	}
	return true, nil
}

// checkSameDPU fails when the DPU read from the API is not the object the reconcile started with.
// DPU names derive from the DPUDevice name and the node label, so a forced delete can leave a new
// DPU with the same name and a different UID. Its task slot must not take over the old object's
// task state. A cached DPU without a UID (tests) is not checked.
func checkSameDPU(latest, dpu *provisioningv1.DPU) error {
	if dpu.UID != "" && latest.UID != dpu.UID {
		return fmt.Errorf("DPU %s/%s was replaced (UID %s, expected %s)", dpu.Namespace, dpu.Name, latest.UID, dpu.UID)
	}
	return nil
}

// conditionTrue returns a done check for conditions that are True once their step finished.
func conditionTrue(step provisioningv1.DPUConditionType) func(*provisioningv1.DPUStatus) bool {
	return func(s *provisioningv1.DPUStatus) bool {
		_, c := cutil.GetDPUCondition(s, string(step))
		return c != nil && c.Status == metav1.ConditionTrue
	}
}

// patchLatestStatus re-reads the DPU from the API, lets mutate change it, and writes the change
// with an optimistic lock, retrying on conflict. mutate returns false to skip the write. The
// reconciler's dpu is only used for its key, so its ResourceVersion and the deferred status patch
// built on it are unaffected.
func patchLatestStatus(ctx context.Context, ctrlCtx *dutil.ControllerContext, dpu *provisioningv1.DPU, mutate func(latest *provisioningv1.DPU) (bool, error)) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		latest := &provisioningv1.DPU{}
		if err := ctrlCtx.UncachedReader().Get(ctx, client.ObjectKeyFromObject(dpu), latest); err != nil {
			return err
		}
		if err := checkSameDPU(latest, dpu); err != nil {
			return err
		}
		base := latest.DeepCopy()
		apply, err := mutate(latest)
		if err != nil || !apply {
			return err
		}
		return ctrlCtx.Status().Patch(ctx, latest, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
	})
}

// recordTaskSubmitted persists a task the BMC just accepted: redfishTaskId and cond, which marks
// the owning step in flight, in one write, and only while the slot is empty. state gets both
// either way. If the write fails, cond's step is marked FailToPersistTaskID (still in flight) and
// the error is returned, so the deferred status patch can still save the id.
func recordTaskSubmitted(ctx context.Context, ctrlCtx *dutil.ControllerContext, dpu *provisioningv1.DPU, state *provisioningv1.DPUStatus, cond *metav1.Condition, taskID string) error {
	if taskID == "" {
		return fmt.Errorf("record Redfish task: empty task id")
	}
	id := taskID
	state.RedfishTaskID = &id
	cutil.SetDPUCondition(state, cond.DeepCopy())

	err := patchLatestStatus(ctx, ctrlCtx, dpu, func(latest *provisioningv1.DPU) (bool, error) {
		if cur := latest.Status.RedfishTaskID; cur != nil && *cur != taskID {
			return false, fmt.Errorf("task slot already holds Redfish task %s", *cur)
		}
		latest.Status.RedfishTaskID = &id
		cutil.SetDPUCondition(&latest.Status, cond.DeepCopy())
		return true, nil
	})
	if err != nil {
		err = fmt.Errorf("record Redfish task %s: %w", taskID, err)
		log.FromContext(ctx).Error(err, "BMC accepted the task but recording it failed; keeping it for the deferred status patch")
		failed := cutil.NewCondition(cond.Type, err, reasonFailToPersistTaskID, err.Error())
		cutil.SetDPUCondition(state, failed)
		return err
	}
	return nil
}

// completeTaskStep persists the end of step's task taskID: redfishTaskId cleared and done set, in
// one write, and only while the slot still holds taskID and step owns it. It reports whether the
// step was marked done.
//
//   - Slot no longer holds taskID: the cached DPU was stale. Nothing is written; the next
//     reconcile works from fresher data.
//   - Slot holds taskID but step does not own it: the id is a leftover from another step (for
//     example a finished ISO task seen by the config step). It is cleared, never counted as this
//     step's result.
//   - Write fails: step stays not done, keeps the task id, and records FailToPersistCompletion, so
//     the next reconcile re-polls the finished task and retries.
func completeTaskStep(ctx context.Context, ctrlCtx *dutil.ControllerContext, dpu *provisioningv1.DPU, state *provisioningv1.DPUStatus, taskID string, done *metav1.Condition) (bool, error) {
	step := provisioningv1.DPUConditionType(done.Type)
	const (
		outcomeStale = iota
		outcomeForeign
		outcomeDone
	)
	outcome := outcomeStale
	err := patchLatestStatus(ctx, ctrlCtx, dpu, func(latest *provisioningv1.DPU) (bool, error) {
		cur := latest.Status.RedfishTaskID
		if cur == nil || *cur != taskID {
			outcome = outcomeStale
			return false, nil
		}
		latest.Status.RedfishTaskID = nil
		if !ownsTask(&latest.Status, step) {
			outcome = outcomeForeign
			return true, nil
		}
		outcome = outcomeDone
		cutil.SetDPUCondition(&latest.Status, done.DeepCopy())
		return true, nil
	})
	logger := log.FromContext(ctx)
	if err != nil {
		err = fmt.Errorf("persist %s completion of Redfish task %s: %w", done.Type, taskID, err)
		logger.Error(err, "Redfish task finished but its completion could not be persisted; will re-poll the task")
		cutil.SetDPUCondition(state, cutil.NewCondition(done.Type, err, reasonFailToPersistCompletion, err.Error()))
		return false, err
	}
	switch outcome {
	case outcomeStale:
		logger.Info("Redfish task finished but the API no longer holds it; waiting for the cache to catch up", "taskID", taskID, "step", step)
		return false, nil
	case outcomeForeign:
		logger.Info("Redfish task id does not belong to this step; cleared it instead of completing the step", "taskID", taskID, "step", step)
		state.RedfishTaskID = nil
		return false, nil
	}
	state.RedfishTaskID = nil
	cutil.SetDPUCondition(state, done.DeepCopy())
	return true, nil
}

// releaseTask persists giving up task taskID (task failed, or must be submitted again):
// redfishTaskId cleared and, when cond is set, the failure condition, in one write, and only while
// the slot still holds taskID. state changes only after that write landed. If the write is skipped
// (the slot holds another task or none) or fails, state keeps its task id, so the deferred status
// patch cannot erase a newer task id, and the next reconcile works from fresher data.
func releaseTask(ctx context.Context, ctrlCtx *dutil.ControllerContext, dpu *provisioningv1.DPU, state *provisioningv1.DPUStatus, taskID string, cond *metav1.Condition) error {
	released := false
	err := patchLatestStatus(ctx, ctrlCtx, dpu, func(latest *provisioningv1.DPU) (bool, error) {
		released = false
		if cur := latest.Status.RedfishTaskID; cur == nil || *cur != taskID {
			return false, nil
		}
		latest.Status.RedfishTaskID = nil
		if cond != nil {
			cutil.SetDPUCondition(&latest.Status, cond.DeepCopy())
		}
		released = true
		return true, nil
	})
	if err != nil {
		err = fmt.Errorf("release Redfish task %s: %w", taskID, err)
		log.FromContext(ctx).Error(err, "Could not persist releasing the Redfish task; keeping it and retrying next reconcile")
		return err
	}
	if released {
		state.RedfishTaskID = nil
		if cond != nil {
			cutil.SetDPUCondition(state, cond.DeepCopy())
		}
	}
	return nil
}
