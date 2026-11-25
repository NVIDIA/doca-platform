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

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

// failedRun returns an install function whose run failed and asks for a restart. The working state
// keeps the task id it was given, as the Redfish install helpers do.
func failedRun(taskID *string) installBFOSFn {
	return func(_ context.Context, dpu *provisioningv1.DPU, _ *dutil.ControllerContext, _ *provisioningv1.DPUDevice) (provisioningv1.DPUStatus, error) {
		state := dpu.Status.DeepCopy()
		state.RedfishTaskID = taskID
		return *state, newRestartOSInstallError(errors.New("task is in Exception state"))
	}
}

// retriesSpent returns how many failed runs were counted for uid.
func retriesSpent(uid types.UID) int {
	if v, ok := installRetryCounter.Load(uid); ok {
		return v.(int)
	}
	return 0
}

// TestFailedReleaseDoesNotSpendARetry verifies a failed run whose task cannot be released costs no
// retry. Counting it would let the next reconcile count the same failed task again and reach Error
// before any replacement task is submitted.
func TestFailedReleaseDoesNotSpendARetry(t *testing.T) {
	uid := types.UID("uid-release-fails")
	defer clearInstallState(uid)
	stored := newTaskDPU("dpu-release-fails", ptr.To("iso-1"))
	stored.UID = uid
	env := newTaskEnv(t, stored, errors.New("api unavailable"))
	env.ctrlCtx.Options.OSInstallRetries = 3
	dpu := stored.DeepCopy()

	state, err := installWithRetry(context.Background(), dpu, env.ctrlCtx, nil, failedRun(ptr.To("iso-1")))
	if err == nil || isRestartOSInstallError(err) {
		t.Fatalf("want the release error, not a restart error, got %v", err)
	}
	if n := retriesSpent(uid); n != 0 {
		t.Fatalf("a failed release spent %d retries, want 0", n)
	}
	if state.Phase != provisioningv1.DPUOSInstalling {
		t.Fatalf("phase = %s, want OS Installing", state.Phase)
	}
	if state.RedfishTaskID == nil || *state.RedfishTaskID != "iso-1" {
		t.Fatalf("state must keep the task id for the next release attempt, got %v", state.RedfishTaskID)
	}
}

// TestSameFailureIsCountedOnce verifies a second reconcile that still sees the same failed task,
// because its cached DPU is stale, does not spend a second retry.
func TestSameFailureIsCountedOnce(t *testing.T) {
	uid := types.UID("uid-count-once")
	defer clearInstallState(uid)
	stored := newTaskDPU("dpu-count-once", ptr.To("iso-1"))
	stored.UID = uid
	env := newTaskEnv(t, stored, nil)
	env.ctrlCtx.Options.OSInstallRetries = 3
	dpu := stored.DeepCopy()

	state, err := installWithRetry(context.Background(), dpu, env.ctrlCtx, nil, failedRun(ptr.To("iso-1")))
	if !isRestartOSInstallError(err) || state.RedfishTaskID != nil {
		t.Fatalf("first failure: err=%v taskID=%v, want a restart error and a released task", err, state.RedfishTaskID)
	}
	if n := retriesSpent(uid); n != 1 {
		t.Fatalf("first failure spent %d retries, want 1", n)
	}

	// The cached DPU still holds the failed task; the API already dropped it.
	state, err = installWithRetry(context.Background(), dpu, env.ctrlCtx, nil, failedRun(ptr.To("iso-1")))
	if !isRestartOSInstallError(err) {
		t.Fatalf("second observation must still requeue, got %v", err)
	}
	if n := retriesSpent(uid); n != 1 {
		t.Fatalf("the same failure spent %d retries, want 1", n)
	}
	if state.Phase != provisioningv1.DPUOSInstalling {
		t.Fatalf("phase = %s, want OS Installing", state.Phase)
	}
}

// TestFailedRunWithoutTaskStillCounts verifies a run that failed before any task existed, for
// example when the BMC refused the submit, still costs a retry.
func TestFailedRunWithoutTaskStillCounts(t *testing.T) {
	uid := types.UID("uid-no-task")
	defer clearInstallState(uid)
	stored := newTaskDPU("dpu-no-task", nil)
	stored.UID = uid
	env := newTaskEnv(t, stored, nil)
	env.ctrlCtx.Options.OSInstallRetries = 3

	if _, err := installWithRetry(context.Background(), stored.DeepCopy(), env.ctrlCtx, nil, failedRun(nil)); !isRestartOSInstallError(err) {
		t.Fatalf("want a restart error, got %v", err)
	}
	if n := retriesSpent(uid); n != 1 {
		t.Fatalf("spent %d retries, want 1", n)
	}
}

// TestLastFailureReleasesTheTask verifies the run that exhausts the retries still releases its
// failed task, so a DPU in Error does not keep a task id.
func TestLastFailureReleasesTheTask(t *testing.T) {
	uid := types.UID("uid-last-failure")
	defer clearInstallState(uid)
	stored := newTaskDPU("dpu-last-failure", ptr.To("iso-1"))
	stored.UID = uid
	env := newTaskEnv(t, stored, nil)
	env.ctrlCtx.Options.OSInstallRetries = 1
	dpu := stored.DeepCopy()

	state, err := installWithRetry(context.Background(), dpu, env.ctrlCtx, nil, failedRun(ptr.To("iso-1")))
	if err != nil {
		t.Fatalf("exhausted retries must end in Error without an error, got %v", err)
	}
	if state.Phase != provisioningv1.DPUError || state.RedfishTaskID != nil {
		t.Fatalf("state = phase %s taskID %v, want Error and no task id", state.Phase, state.RedfishTaskID)
	}
	if got := env.get(t, dpu); got.Status.RedfishTaskID != nil {
		t.Fatalf("API must drop the failed task id, got %v", *got.Status.RedfishTaskID)
	}
}
