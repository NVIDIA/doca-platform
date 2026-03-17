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
	// MaxSafetyLimit is the absolute maximum restarts allowed (defense-in-depth)
	MaxSafetyLimit = 10
	// MinRestartInterval is the minimum time between restart triggers (BMC protection)
	MinRestartInterval = 1 * time.Minute
	// OSRunningTimeout is how long to wait for OS to boot before failing
	OSRunningTimeout = 5 * time.Minute
	// bootProgressOsRunning is the BMC OemLastState value indicating the ARM OS is fully booted
	bootProgressOsRunning = "OsIsRunning"
)

// PerformArmForceRestart handles the ARM ForceRestart phase for DPU configuration.
// It executes a multi-reboot cycle as specified by the ArmRestartTracker, then
// returns to InitializeInterface for validation.
//
// The caller (e.g., InitializeInterface) creates the tracker with the required
// number of restarts and transitions to this phase. This handler is agnostic to
// why restarts are needed — it simply executes the requested count.
//
// State flow:
//
//	InitializeInterface (create tracker) → PerformArmForceRestart (execute restarts)
//	  → InitializeInterface (validate result) → ConfigFWParameters
//
// Error convention:
//   - Returning nil error = terminal (no requeue) or transition to another phase
//   - Returning non-nil error = retryable (controller will requeue)
func PerformArmForceRestart(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	log := log.FromContext(ctx)
	state := dpu.Status.DeepCopy()

	// Handle deletion - abort and cleanup
	if !dpu.DeletionTimestamp.IsZero() {
		dutil.ClearArmRestartTracker(dpu)
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	tracker, err := validateTracker(dpu, state)
	if tracker == nil {
		return *state, err
	}

	// Set initial condition (in progress; Status False until step is done).
	_, cond := cutil.GetDPUCondition(state, provisioningv1.DPUCondArmForceRestarted.String())
	if cond == nil {
		setArmCondition(state, "InProgress",
			fmt.Sprintf("ARM restart flow started (0/%d)", tracker.MaxAttempts))
	}

	// Get DPUDevice and create Redfish client
	client, err := createRedfishClientForDPU(ctx, dpu, ctrlCtx, state)
	if err != nil {
		return *state, err // Retryable
	}

	// Check ARM OS boot state via BMC
	resp, sysInfo, err := client.GetSystem()
	if err != nil {
		log.Error(err, "Failed to get system info from BMC")
		setArmCondition(state, "FailedToGetSystemState", err.Error())
		return *state, err // Retryable
	}
	if sysInfo == nil || resp.StatusCode() != http.StatusOK {
		err = fmt.Errorf("unexpected response from BMC: status=%d", resp.StatusCode())
		log.Error(err, "Failed to get system boot state")
		setArmCondition(state, "FailedToGetSystemState", err.Error())
		return *state, err // Retryable
	}
	osRunning := sysInfo.BootProgress.OemLastState == bootProgressOsRunning

	return executeRestartStateMachine(ctx, dpu, state, tracker, client, osRunning)
}

// validateTracker loads and validates the ARM restart tracker.
// Returns (nil, nil) with phase set if the tracker is invalid and we should exit early.
// Returns (nil, err) if loading failed.
// Returns (tracker, nil) if valid.
func validateTracker(dpu *provisioningv1.DPU, state *provisioningv1.DPUStatus) (*dutil.ArmRestartTracker, error) {
	tracker, err := dutil.LoadArmRestartTracker(dpu)
	if err != nil {
		return nil, err
	}

	// No tracker or stale tracker - return to Init for re-detection
	if tracker == nil || tracker.IsStale() {
		dutil.ClearArmRestartTracker(dpu)
		state.Phase = provisioningv1.DPUInitializeInterface
		return nil, nil
	}

	// Detect spec changes during flow - abort if generation changed
	if tracker.InitialGeneration != dpu.Generation {
		dutil.ClearArmRestartTracker(dpu)
		state.Phase = provisioningv1.DPUInitializeInterface
		return nil, nil
	}

	// Safety check - prevent infinite loops
	if tracker.Attempt >= MaxSafetyLimit {
		state.Phase = provisioningv1.DPUError
		err := fmt.Errorf("exceeded maximum safety limit of %d restart attempts", MaxSafetyLimit)
		setArmCondition(state, "MaxSafetyLimitExceeded", err.Error())
		return nil, nil // Terminal error - don't retry
	}

	return tracker, nil
}

// createRedfishClientForDPU fetches the DPUDevice and creates a Redfish TLS client.
func createRedfishClientForDPU(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext, state *provisioningv1.DPUStatus) (*rc.Client, error) {
	dpuDevice := &provisioningv1.DPUDevice{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.DPUDeviceName}, dpuDevice); err != nil {
		setArmCondition(state, "FailedToGetDPUDevice", err.Error())
		return nil, err // Retryable
	}

	client, err := rc.NewTLSClient(ctx, dpuDevice.BMCAddress(), dpu.Namespace, ctrlCtx.Client)
	if err != nil {
		setArmCondition(state, "FailedToCreateRedfishClient", err.Error())
		return nil, err // Retryable
	}

	return client, nil
}

// executeRestartStateMachine runs the core restart logic once tracker and client are ready.
func executeRestartStateMachine(ctx context.Context, dpu *provisioningv1.DPU, state *provisioningv1.DPUStatus, tracker *dutil.ArmRestartTracker, client *rc.Client, osRunning bool) (provisioningv1.DPUStatus, error) {
	log := log.FromContext(ctx)

	// All restarts done — return to InitializeInterface for verification.
	if tracker.AllRestartsDone() {
		log.Info("All ARM restarts complete, returning to InitializeInterface",
			"attempts", tracker.Attempt, "maxAttempts", tracker.MaxAttempts)
		cutil.SetDPUCondition(state, cutil.DPUCondition(
			provisioningv1.DPUCondArmForceRestarted, "", ""))
		state.Phase = provisioningv1.DPUInitializeInterface
		return *state, nil
	}

	// OS is rebooting after a triggered intermediate restart — wait for it to come
	// back before triggering the next one (ForceRestart requires the system to be up).
	if !osRunning && tracker.Attempt > 0 {
		return handleWaitingForBoot(state, tracker)
	}

	// More restarts needed - trigger next one
	return triggerArmRestart(ctx, dpu, state, tracker, client)
}

// handleWaitingForBoot handles the case when OS is not yet running after a restart.
func handleWaitingForBoot(state *provisioningv1.DPUStatus, tracker *dutil.ArmRestartTracker) (provisioningv1.DPUStatus, error) {
	if !tracker.LastRestartTime.IsZero() && time.Since(tracker.LastRestartTime) > OSRunningTimeout {
		state.Phase = provisioningv1.DPUError
		err := fmt.Errorf("OS boot timeout after %s", OSRunningTimeout)
		setArmCondition(state, "OSBootTimeout", err.Error())
		return *state, nil // Terminal error - don't retry
	}

	setArmCondition(state, "WaitingForBoot", "Waiting for ARM OS to reach running state")
	return *state, nil // Requeue via default reconcile interval
}

// triggerArmRestart triggers a single ARM ForceRestart and updates the tracker.
func triggerArmRestart(ctx context.Context, dpu *provisioningv1.DPU, state *provisioningv1.DPUStatus, tracker *dutil.ArmRestartTracker, client *rc.Client) (provisioningv1.DPUStatus, error) {
	log := log.FromContext(ctx)

	// Enforce minimum interval between restarts (BMC protection)
	if !tracker.LastRestartTime.IsZero() && time.Since(tracker.LastRestartTime) < MinRestartInterval {
		return *state, nil // Requeue
	}

	// ForceRestartDPUArm returns error for both connection failures and non-200/204 responses
	if _, err := client.ForceRestartDPUArm(); err != nil {
		log.Error(err, "Failed to trigger ARM ForceRestart")
		setArmCondition(state, "FailedToRebootDPUArm", err.Error())
		return *state, err // Retryable
	}

	tracker.IncrementAttempt()
	if err := dutil.SaveArmRestartTracker(dpu, tracker); err != nil {
		log.Error(err, "Failed to save ArmRestartTracker")
		return *state, err // Retryable
	}

	setArmCondition(state, "RestartTriggered",
		fmt.Sprintf("ARM restart %d/%d triggered", tracker.Attempt, tracker.MaxAttempts))
	return *state, nil
}

// setArmCondition sets ArmForceRestarted to False with reason and message.
// Use for in-progress sub-states and errors; True is set only when the step is done (phase transition).
func setArmCondition(state *provisioningv1.DPUStatus, reason, message string) {
	cutil.SetDPUCondition(state, &metav1.Condition{
		Type:    provisioningv1.DPUCondArmForceRestarted.String(),
		Status:  metav1.ConditionFalse,
		Reason:  reason,
		Message: message,
	})
}
