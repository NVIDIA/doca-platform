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
	"net/http"
	"net/http/httptest"
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	rc "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"

	"github.com/go-logr/logr"
	"github.com/go-resty/resty/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// TestConfigStepReleasesLeftoverISOTaskBeforePolling verifies that when the ISO task id is still in
// the slot while the config step runs, the config step drops it without polling. A failed poll
// would otherwise write a FailToCheckProgress condition for the config step, and a later
// successful poll would then count the ISO task as the config transfer.
func TestConfigStepReleasesLeftoverISOTaskBeforePolling(t *testing.T) {
	polls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		polls++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	c, err := rc.NewRawClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseIdleConnections()

	stored := newTaskDPU("dpu-leftover-iso", ptr.To("iso-1"),
		taskCond(provisioningv1.DPUCondIsoTransferred, metav1.ConditionTrue, string(provisioningv1.DPUCondIsoTransferred)))
	env := newTaskEnv(t, stored, nil)
	dpu := stored.DeepCopy()
	state := dpu.Status.DeepCopy()
	install := func(context.Context, string) (*resty.Response, *rc.TaskInfo, error) {
		return nil, nil, errors.New("must not submit in the same reconcile")
	}

	err = reconcileBf4ArmTransfer(context.Background(), logr.Discard(), dpu, state, c, env.ctrlCtx,
		provisioningv1.DPUCondConfigTransferred, "config", install, "config")
	if err != nil {
		t.Fatal(err)
	}
	if polls != 0 {
		t.Fatalf("config step polled the ISO task %d times", polls)
	}
	if state.RedfishTaskID != nil {
		t.Fatalf("state must drop the leftover task id, got %v", *state.RedfishTaskID)
	}
	if got := env.get(t, dpu); got.Status.RedfishTaskID != nil {
		t.Fatalf("API must drop the leftover task id, got %v", *got.Status.RedfishTaskID)
	}
	if cc := condition(state, provisioningv1.DPUCondConfigTransferred); cc != nil {
		t.Fatalf("config step must not carry a condition for the ISO task, got %+v", cc)
	}
}

// TestReleaseTaskKeepsStateWhenSlotChanged verifies a skipped release leaves state alone, so the
// deferred status patch cannot erase the task id another reconcile recorded in the meantime.
func TestReleaseTaskKeepsStateWhenSlotChanged(t *testing.T) {
	stored := newTaskDPU("dpu-release-state", ptr.To("pldm-2"))
	env := newTaskEnv(t, stored, nil)
	dpu := newTaskDPU(stored.Name, ptr.To("pldm-1"))
	state := dpu.Status.DeepCopy()
	failed := taskCond(provisioningv1.DPUCondFwBundleSubmitted, metav1.ConditionFalse, "FailedToUpdatePldmFwBundle")

	if err := releaseTask(context.Background(), env.ctrlCtx, dpu, state, "pldm-1", &failed); err != nil {
		t.Fatal(err)
	}
	if state.RedfishTaskID == nil || *state.RedfishTaskID != "pldm-1" {
		t.Fatalf("state task id = %v, want pldm-1 untouched", state.RedfishTaskID)
	}
	if c := condition(state, provisioningv1.DPUCondFwBundleSubmitted); c != nil {
		t.Fatalf("state must not get the failure condition on a skipped release, got %+v", c)
	}
}

// TestTaskSlotFreeRejectsPhaseChange verifies a cached reconcile cannot submit a task after the API
// DPU left the phase it runs for, for example after a move to Error.
func TestTaskSlotFreeRejectsPhaseChange(t *testing.T) {
	stored := newTaskDPU("dpu-phase-moved", nil)
	stored.Status.Phase = provisioningv1.DPUError
	env := newTaskEnv(t, stored, nil)
	cached := newTaskDPU(stored.Name, nil) // cached copy still says OS Installing

	free, err := isTaskSlotFree(context.Background(), env.ctrlCtx.UncachedReader(), cached, conditionTrue(provisioningv1.DPUCondConfigTransferred))
	if err != nil {
		t.Fatal(err)
	}
	if free {
		t.Fatal("isTaskSlotFree must reject a submit once the API DPU left the cached phase")
	}
}

// TestReplacedDPUIsNotTouched verifies a reconcile for a deleted DPU can neither pass the slot check
// nor write a task id into a new DPU that has the same name and a different UID.
func TestReplacedDPUIsNotTouched(t *testing.T) {
	stored := newTaskDPU("dpu-replaced", nil)
	stored.UID = "uid-new"
	env := newTaskEnv(t, stored, nil)
	old := stored.DeepCopy()
	old.UID = "uid-old"

	free, err := isTaskSlotFree(context.Background(), env.ctrlCtx.UncachedReader(), old, conditionTrue(provisioningv1.DPUCondConfigTransferred))
	if err == nil || free {
		t.Fatalf("slot check for a replaced DPU: free=%v err=%v, want an error", free, err)
	}

	state := old.Status.DeepCopy()
	cond := inFlightCondition(provisioningv1.DPUCondConfigTransferred, "cfg-1")
	if err := recordTaskSubmitted(context.Background(), env.ctrlCtx, old, state, cond, "cfg-1"); err == nil {
		t.Fatal("recording a task for a replaced DPU must fail")
	}
	if got := env.get(t, stored); got.Status.RedfishTaskID != nil {
		t.Fatalf("the new DPU must not receive the old DPU's task id, got %v", *got.Status.RedfishTaskID)
	}
}
