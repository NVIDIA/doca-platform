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
	"errors"
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// newTaskScheme returns a scheme with the provisioning types registered.
func newTaskScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := provisioningv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return scheme
}

// taskCond returns a condition of the given type, status and reason.
func taskCond(step provisioningv1.DPUConditionType, status metav1.ConditionStatus, reason string) metav1.Condition {
	return metav1.Condition{
		Type:               string(step),
		Status:             status,
		Reason:             reason,
		LastTransitionTime: metav1.Now(),
	}
}

// newTaskDPU returns a DPU in OS Installing with the given task id and conditions.
func newTaskDPU(name string, taskID *string, conds ...metav1.Condition) *provisioningv1.DPU {
	return &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Status: provisioningv1.DPUStatus{
			Phase:         provisioningv1.DPUOSInstalling,
			RedfishTaskID: taskID,
			Conditions:    conds,
		},
	}
}

// taskEnv is a fake API server holding one DPU, with a count of status writes.
type taskEnv struct {
	ctrlCtx *dutil.ControllerContext
	client  client.Client
	patches *int
}

// newTaskEnv stores stored on a fake API server. failPatch, when set, fails every status write.
func newTaskEnv(t *testing.T, stored *provisioningv1.DPU, failPatch error) taskEnv {
	t.Helper()
	patches := 0
	c := fake.NewClientBuilder().WithScheme(newTaskScheme(t)).WithStatusSubresource(stored).WithObjects(stored.DeepCopy()).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourcePatch: func(ctx context.Context, c client.Client, subResourceName string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
				patches++
				if failPatch != nil {
					return failPatch
				}
				return c.SubResource(subResourceName).Patch(ctx, obj, patch, opts...)
			},
		}).Build()
	return taskEnv{
		ctrlCtx: &dutil.ControllerContext{Client: c},
		client:  c,
		patches: &patches,
	}
}

// get returns the DPU as stored on the fake API server.
func (e taskEnv) get(t *testing.T, dpu *provisioningv1.DPU) *provisioningv1.DPU {
	t.Helper()
	got := &provisioningv1.DPU{}
	if err := e.client.Get(context.Background(), client.ObjectKeyFromObject(dpu), got); err != nil {
		t.Fatal(err)
	}
	return got
}

// condition returns the condition of type step on status, or nil.
func condition(status *provisioningv1.DPUStatus, step provisioningv1.DPUConditionType) *metav1.Condition {
	_, c := cutil.GetDPUCondition(status, string(step))
	return c
}

// doneCondition returns step's done condition.
func doneCondition(step provisioningv1.DPUConditionType) *metav1.Condition {
	c := cutil.NewCondition(string(step), nil, "", "")
	c.Status = metav1.ConditionTrue
	return c
}

// TestOwnsTask verifies a step owns the task id only while its condition marks the task in flight,
// and that a step without a condition owns it only when it is the first step in the slot.
func TestOwnsTask(t *testing.T) {
	config := provisioningv1.DPUCondConfigTransferred
	for _, tc := range []struct {
		name string
		step provisioningv1.DPUConditionType
		cond *metav1.Condition
		want bool
	}{
		{
			name: "config without condition follows ISO",
			step: config,
			cond: nil,
			want: false,
		},
		{
			name: "ISO without condition is first in the slot",
			step: provisioningv1.DPUCondIsoTransferred,
			cond: nil,
			want: true,
		},
		{
			name: "BFB without condition is first in the slot",
			step: provisioningv1.DPUCondBFBTransferred,
			cond: nil,
			want: true,
		},
		{
			name: "BFB owns the id whatever reason its condition carries",
			step: provisioningv1.DPUCondBFBTransferred,
			cond: ptr.To(taskCond(provisioningv1.DPUCondBFBTransferred, metav1.ConditionFalse, "FailedToCreateClient")),
			want: true,
		},
		{
			name: "submitted",
			step: config,
			cond: ptr.To(taskCond(config, metav1.ConditionFalse, reasonTaskSubmitted)),
			want: true,
		},
		{
			name: "progress",
			step: config,
			cond: ptr.To(taskCond(config, metav1.ConditionFalse, reasonTaskProgress)),
			want: true,
		},
		{
			name: "progress read failed",
			step: config,
			cond: ptr.To(taskCond(config, metav1.ConditionFalse, reasonFailToCheckProgress)),
			want: true,
		},
		{
			name: "record failed",
			step: config,
			cond: ptr.To(taskCond(config, metav1.ConditionFalse, reasonFailToPersistTaskID)),
			want: true,
		},
		{
			name: "completion write failed",
			step: config,
			cond: ptr.To(taskCond(config, metav1.ConditionFalse, reasonFailToPersistCompletion)),
			want: true,
		},
		{
			name: "install failed",
			step: provisioningv1.DPUCondIsoTransferred,
			cond: ptr.To(taskCond(provisioningv1.DPUCondIsoTransferred, metav1.ConditionFalse, "FailToInstall")),
			want: false,
		},
		{
			name: "done",
			step: provisioningv1.DPUCondIsoTransferred,
			cond: ptr.To(taskCond(provisioningv1.DPUCondIsoTransferred, metav1.ConditionTrue, "IsoTransferred")),
			want: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status := &provisioningv1.DPUStatus{}
			if tc.cond != nil {
				status.Conditions = []metav1.Condition{*tc.cond}
			}
			if got := ownsTask(status, tc.step); got != tc.want {
				t.Fatalf("ownsTask = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestTaskSlotFreeReadsTheAPI verifies the submit check uses the API, not the cached DPU the
// handler holds: a task another reconcile recorded, or a finished step, blocks a new submit.
func TestTaskSlotFreeReadsTheAPI(t *testing.T) {
	step := provisioningv1.DPUCondConfigTransferred
	for _, tc := range []struct {
		name   string
		stored *provisioningv1.DPU
		want   bool
	}{
		{
			name:   "slot empty, step pending",
			stored: newTaskDPU("dpu-free", nil),
			want:   true,
		},
		{
			name:   "task already recorded",
			stored: newTaskDPU("dpu-recorded", ptr.To("cfg-2"), taskCond(step, metav1.ConditionFalse, reasonTaskSubmitted)),
			want:   false,
		},
		{
			name:   "step already done",
			stored: newTaskDPU("dpu-done", nil, taskCond(step, metav1.ConditionTrue, string(step))),
			want:   false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newTaskEnv(t, tc.stored, nil)
			cached := newTaskDPU(tc.stored.Name, nil)
			free, err := isTaskSlotFree(context.Background(), env.ctrlCtx.UncachedReader(), cached, conditionTrue(step))
			if err != nil {
				t.Fatal(err)
			}
			if free != tc.want {
				t.Fatalf("isTaskSlotFree = %v, want %v", free, tc.want)
			}
		})
	}
}

// TestRecordTaskSubmittedWritesIDAndOwnerTogether verifies an accepted task is saved with the
// condition marking its step in flight, in one write, without touching the reconciler's DPU.
func TestRecordTaskSubmittedWritesIDAndOwnerTogether(t *testing.T) {
	step := provisioningv1.DPUCondConfigTransferred
	stored := newTaskDPU("dpu-record", nil)
	env := newTaskEnv(t, stored, nil)
	dpu := stored.DeepCopy()
	state := dpu.Status.DeepCopy()

	if err := recordTaskSubmitted(context.Background(), env.ctrlCtx, dpu, state, inFlightCondition(step, "cfg-2"), "cfg-2"); err != nil {
		t.Fatal(err)
	}
	if *env.patches != 1 {
		t.Fatalf("recording must be one status write, got %d", *env.patches)
	}
	got := env.get(t, dpu)
	if got.Status.RedfishTaskID == nil || *got.Status.RedfishTaskID != "cfg-2" {
		t.Fatalf("API task id = %v, want cfg-2", got.Status.RedfishTaskID)
	}
	if !ownsTask(&got.Status, step) {
		t.Fatalf("API must mark %s as owning the task, got %v", step, condition(&got.Status, step))
	}
	if state.RedfishTaskID == nil || *state.RedfishTaskID != "cfg-2" || !ownsTask(state, step) {
		t.Fatalf("state must carry the task id and its owner, got %+v", state)
	}
	if dpu.Status.RedfishTaskID != nil {
		t.Fatal("recording must not mutate the reconciler DPU")
	}
}

// TestRecordTaskSubmittedRefusesOccupiedSlot verifies a task is not recorded over a different task
// already in the slot.
func TestRecordTaskSubmittedRefusesOccupiedSlot(t *testing.T) {
	step := provisioningv1.DPUCondConfigTransferred
	stored := newTaskDPU("dpu-occupied", ptr.To("iso-1"), taskCond(provisioningv1.DPUCondIsoTransferred, metav1.ConditionFalse, reasonTaskProgress))
	env := newTaskEnv(t, stored, nil)
	dpu := newTaskDPU(stored.Name, nil)
	state := dpu.Status.DeepCopy()

	if err := recordTaskSubmitted(context.Background(), env.ctrlCtx, dpu, state, inFlightCondition(step, "cfg-2"), "cfg-2"); err == nil {
		t.Fatal("expected an error when the slot holds another task")
	}
	if got := env.get(t, dpu); got.Status.RedfishTaskID == nil || *got.Status.RedfishTaskID != "iso-1" {
		t.Fatalf("API task id = %v, want iso-1 untouched", got.Status.RedfishTaskID)
	}
	if c := condition(state, step); c == nil || c.Reason != reasonFailToPersistTaskID {
		t.Fatalf("state must record the failed recording, got %v", c)
	}
}

// TestRecordTaskSubmittedFailureKeepsTaskOnState verifies a failed write keeps the accepted task id
// on state, still owned by its step, so the deferred status patch can save it.
func TestRecordTaskSubmittedFailureKeepsTaskOnState(t *testing.T) {
	step := provisioningv1.DPUCondBFBTransferred
	stored := newTaskDPU("dpu-record-fail", nil)
	env := newTaskEnv(t, stored, errors.New("injected status patch failure"))
	dpu := stored.DeepCopy()
	state := dpu.Status.DeepCopy()

	if err := recordTaskSubmitted(context.Background(), env.ctrlCtx, dpu, state, inFlightCondition(step, "bfb-1"), "bfb-1"); err == nil {
		t.Fatal("expected an error when the write fails")
	}
	if state.RedfishTaskID == nil || *state.RedfishTaskID != "bfb-1" {
		t.Fatalf("state must keep the accepted task id, got %v", state.RedfishTaskID)
	}
	if c := condition(state, step); c == nil || c.Reason != reasonFailToPersistTaskID || !ownsTask(state, step) {
		t.Fatalf("state must keep the step owning the task, got %v", c)
	}
}

// TestCompleteTaskStepWritesClearAndDoneTogether verifies a finished task clears the slot and marks
// its step done in one write.
func TestCompleteTaskStepWritesClearAndDoneTogether(t *testing.T) {
	step := provisioningv1.DPUCondIsoTransferred
	stored := newTaskDPU("dpu-complete", ptr.To("iso-1"), taskCond(step, metav1.ConditionFalse, reasonTaskProgress))
	env := newTaskEnv(t, stored, nil)
	dpu := stored.DeepCopy()
	state := dpu.Status.DeepCopy()

	completed, err := completeTaskStep(context.Background(), env.ctrlCtx, dpu, state, "iso-1", doneCondition(step))
	if err != nil {
		t.Fatal(err)
	}
	if !completed || *env.patches != 1 {
		t.Fatalf("completed=%v writes=%d, want true and one write", completed, *env.patches)
	}
	got := env.get(t, dpu)
	if got.Status.RedfishTaskID != nil {
		t.Fatalf("API must not keep the finished task id, got %v", *got.Status.RedfishTaskID)
	}
	if c := condition(&got.Status, step); c == nil || c.Status != metav1.ConditionTrue {
		t.Fatalf("API must have %s=True, got %v", step, c)
	}
	if state.RedfishTaskID != nil {
		t.Fatalf("state must drop the task id, got %v", *state.RedfishTaskID)
	}
	if c := condition(state, step); c == nil || c.Status != metav1.ConditionTrue {
		t.Fatalf("state must have %s=True, got %v", step, c)
	}
}

// TestCompleteTaskStepSkipsWhenAPINoLongerHoldsTask verifies a stale cached DPU cannot complete a
// step: if the API no longer holds the polled task, nothing is written and the step stays pending.
func TestCompleteTaskStepSkipsWhenAPINoLongerHoldsTask(t *testing.T) {
	step := provisioningv1.DPUCondIsoTransferred
	stored := newTaskDPU("dpu-stale", ptr.To("cfg-2"),
		taskCond(step, metav1.ConditionTrue, string(step)),
		taskCond(provisioningv1.DPUCondConfigTransferred, metav1.ConditionFalse, reasonTaskSubmitted))
	env := newTaskEnv(t, stored, nil)
	cached := newTaskDPU(stored.Name, ptr.To("iso-1"), taskCond(step, metav1.ConditionFalse, reasonTaskProgress))
	state := cached.Status.DeepCopy()

	completed, err := completeTaskStep(context.Background(), env.ctrlCtx, cached, state, "iso-1", doneCondition(step))
	if err != nil {
		t.Fatal(err)
	}
	if completed || *env.patches != 0 {
		t.Fatalf("completed=%v writes=%d, want false and no write", completed, *env.patches)
	}
	if got := env.get(t, cached); got.Status.RedfishTaskID == nil || *got.Status.RedfishTaskID != "cfg-2" {
		t.Fatalf("API task id = %v, want cfg-2 untouched", got.Status.RedfishTaskID)
	}
}

// TestCompleteTaskStepClearsTaskOwnedByAnotherStep reproduces the CI failure: the API holds a
// finished ISO task while ISO is done, and the config step polls it. The config step must not be
// marked done; the leftover id is cleared so config is submitted next.
func TestCompleteTaskStepClearsTaskOwnedByAnotherStep(t *testing.T) {
	stored := newTaskDPU("dpu-foreign", ptr.To("iso-1"),
		taskCond(provisioningv1.DPUCondIsoTransferred, metav1.ConditionTrue, string(provisioningv1.DPUCondIsoTransferred)))
	env := newTaskEnv(t, stored, nil)
	dpu := stored.DeepCopy()
	state := dpu.Status.DeepCopy()

	completed, err := completeTaskStep(context.Background(), env.ctrlCtx, dpu, state, "iso-1", doneCondition(provisioningv1.DPUCondConfigTransferred))
	if err != nil {
		t.Fatal(err)
	}
	if completed {
		t.Fatal("a task of another step must not complete the config step")
	}
	got := env.get(t, dpu)
	if got.Status.RedfishTaskID != nil {
		t.Fatalf("API must drop the leftover task id, got %v", *got.Status.RedfishTaskID)
	}
	if c := condition(&got.Status, provisioningv1.DPUCondConfigTransferred); c != nil && c.Status == metav1.ConditionTrue {
		t.Fatal("API must not mark config transferred")
	}
	if state.RedfishTaskID != nil {
		t.Fatalf("state must drop the leftover task id, got %v", *state.RedfishTaskID)
	}
	if c := condition(state, provisioningv1.DPUCondConfigTransferred); c != nil && c.Status == metav1.ConditionTrue {
		t.Fatal("state must not mark config transferred")
	}
}

// TestCompleteTaskStepKeepsConditionsWrittenByOthers verifies conditions written to the API after
// the reconciler read the DPU survive the completion write.
func TestCompleteTaskStepKeepsConditionsWrittenByOthers(t *testing.T) {
	step := provisioningv1.DPUCondIsoTransferred
	stored := newTaskDPU("dpu-others", ptr.To("iso-1"),
		taskCond(step, metav1.ConditionFalse, reasonTaskProgress),
		taskCond("WrittenByOthers", metav1.ConditionTrue, "Other"))
	env := newTaskEnv(t, stored, nil)
	cached := newTaskDPU(stored.Name, ptr.To("iso-1"), taskCond(step, metav1.ConditionFalse, reasonTaskProgress))
	state := cached.Status.DeepCopy()

	if _, err := completeTaskStep(context.Background(), env.ctrlCtx, cached, state, "iso-1", doneCondition(step)); err != nil {
		t.Fatal(err)
	}
	if condition(&env.get(t, cached).Status, "WrittenByOthers") == nil {
		t.Fatal("completion write dropped a condition written by another writer")
	}
}

// TestCompleteTaskStepRetriesOnConflict verifies a version conflict re-reads the DPU and retries.
func TestCompleteTaskStepRetriesOnConflict(t *testing.T) {
	step := provisioningv1.DPUCondIsoTransferred
	stored := newTaskDPU("dpu-conflict", ptr.To("iso-1"), taskCond(step, metav1.ConditionFalse, reasonTaskProgress))
	patches := 0
	c := fake.NewClientBuilder().WithScheme(newTaskScheme(t)).WithStatusSubresource(stored).WithObjects(stored.DeepCopy()).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourcePatch: func(ctx context.Context, c client.Client, subResourceName string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
				patches++
				if patches == 1 {
					return apierrors.NewConflict(schema.GroupResource{Group: provisioningv1.GroupVersion.Group, Resource: "dpus"}, obj.GetName(), errors.New("injected conflict"))
				}
				return c.SubResource(subResourceName).Patch(ctx, obj, patch, opts...)
			},
		}).Build()
	dpu := stored.DeepCopy()
	state := dpu.Status.DeepCopy()

	completed, err := completeTaskStep(context.Background(), &dutil.ControllerContext{Client: c}, dpu, state, "iso-1", doneCondition(step))
	if err != nil {
		t.Fatal(err)
	}
	if !completed || patches != 2 {
		t.Fatalf("completed=%v writes=%d, want true after one retry", completed, patches)
	}
}

// TestCompleteTaskStepFailureKeepsStepPending verifies a failed write leaves the step not done and
// still owning its task, so the next reconcile re-polls the finished task and retries.
func TestCompleteTaskStepFailureKeepsStepPending(t *testing.T) {
	step := provisioningv1.DPUCondIsoTransferred
	stored := newTaskDPU("dpu-complete-fail", ptr.To("iso-1"), taskCond(step, metav1.ConditionFalse, reasonTaskProgress))
	env := newTaskEnv(t, stored, errors.New("injected status patch failure"))
	dpu := stored.DeepCopy()
	state := dpu.Status.DeepCopy()

	completed, err := completeTaskStep(context.Background(), env.ctrlCtx, dpu, state, "iso-1", doneCondition(step))
	if err == nil || completed {
		t.Fatalf("completed=%v err=%v, want false and an error", completed, err)
	}
	if state.RedfishTaskID == nil || *state.RedfishTaskID != "iso-1" {
		t.Fatalf("state must keep the task id, got %v", state.RedfishTaskID)
	}
	if c := condition(state, step); c == nil || c.Reason != reasonFailToPersistCompletion || !ownsTask(state, step) {
		t.Fatalf("state must keep the step owning its task, got %v", c)
	}
}

// TestReleaseTaskWritesClearAndConditionTogether verifies releasing a task clears the slot and
// sets the failure condition in one write.
func TestReleaseTaskWritesClearAndConditionTogether(t *testing.T) {
	step := provisioningv1.DPUCondFwBundleSubmitted
	stored := newTaskDPU("dpu-release", ptr.To("pldm-1"), taskCond(step, metav1.ConditionTrue, "Submitting"))
	env := newTaskEnv(t, stored, nil)
	dpu := stored.DeepCopy()
	state := dpu.Status.DeepCopy()
	failed := cutil.NewCondition(string(step), errors.New("exception"), "FailedToUpdatePldmFwBundle", "exception")

	if err := releaseTask(context.Background(), env.ctrlCtx, dpu, state, "pldm-1", failed); err != nil {
		t.Fatal(err)
	}
	if *env.patches != 1 {
		t.Fatalf("release must be one status write, got %d", *env.patches)
	}
	got := env.get(t, dpu)
	if got.Status.RedfishTaskID != nil {
		t.Fatalf("API must drop the released task id, got %v", *got.Status.RedfishTaskID)
	}
	if c := condition(&got.Status, step); c == nil || c.Status != metav1.ConditionFalse {
		t.Fatalf("API must carry the failure condition, got %v", c)
	}
	if state.RedfishTaskID != nil {
		t.Fatalf("state must drop the released task id, got %v", *state.RedfishTaskID)
	}
}

// TestReleaseTaskSkipsWhenSlotChanged verifies release does not clear a different task that took
// the slot in the meantime.
func TestReleaseTaskSkipsWhenSlotChanged(t *testing.T) {
	stored := newTaskDPU("dpu-release-changed", ptr.To("pldm-2"), taskCond(provisioningv1.DPUCondFwBundleSubmitted, metav1.ConditionTrue, "Submitting"))
	env := newTaskEnv(t, stored, nil)
	dpu := newTaskDPU(stored.Name, ptr.To("pldm-1"))
	state := dpu.Status.DeepCopy()

	if err := releaseTask(context.Background(), env.ctrlCtx, dpu, state, "pldm-1", nil); err != nil {
		t.Fatal(err)
	}
	if *env.patches != 0 {
		t.Fatalf("release must not write when the slot changed, got %d writes", *env.patches)
	}
	if got := env.get(t, dpu); got.Status.RedfishTaskID == nil || *got.Status.RedfishTaskID != "pldm-2" {
		t.Fatalf("API task id = %v, want pldm-2 untouched", got.Status.RedfishTaskID)
	}
}
