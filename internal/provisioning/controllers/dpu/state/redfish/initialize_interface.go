/*
Copyright 2025 NVIDIA

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
	dpustate "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state"
	rfclient "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/pkg/conditions"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// secureBootRequiredRestarts is the number of ARM restarts required for
	// Secure Boot configuration to take effect. Per BlueField BMC documentation,
	// the first restart applies the BIOS setting and the second validates it.
	secureBootRequiredRestarts = 2

	// secureBootVerificationTimeout is the maximum time after all restarts completed
	// to wait for the BMC to reflect the new SecureBootCurrentBoot value.
	// Anchored to ArmForceRestarted condition's LastTransitionTime, which is set
	// when PerformArmForceRestart detects AllRestartsDone (regardless of OS state).
	secureBootVerificationTimeout = 4 * time.Minute
)

func InitializeInterface(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	state := dpu.Status.DeepCopy()
	log := log.FromContext(ctx)

	if !dpu.DeletionTimestamp.IsZero() {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	device := &provisioningv1.DPUDevice{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.DPUDeviceName}, device); err != nil {
		return *state, err
	}

	// Check if DPUDevice is ready before proceeding
	if err := checkDPUDeviceReady(device); err != nil {
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), err, "DPUDeviceNotReady", err.Error()))
		return *state, err
	}

	tlsClient, err := rfclient.NewTLSClient(ctx, device.BMCAddress(), dpu.Namespace, ctrlCtx.Client)
	if err != nil {
		log.Error(err, fmt.Sprintf("Failed to create TLS client for DPU %s", device.BMCAddress()))
		return *state, err
	}

	descr, err := getProductDescription(tlsClient)
	if err != nil {
		log.Error(err, fmt.Sprintf("Failed to get product description for DPU %s", device.BMCAddress()))
		return *state, err
	}

	// Redfish returns DPU is in NIC mode - reqesting to change to DpuMode
	if descr.Mode != nil && *descr.Mode == rfclient.NicMode {
		log.Info(fmt.Sprintf("DPU %s is in NicMode. Setting DPU mode to DpuMode", device.BMCAddress()))
		_, err := tlsClient.SetDpuMode(provisioningv1.DpuMode)
		if err != nil {
			err = fmt.Errorf("failed to request mode change: %w", err)
			cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), err, "FailedToRequestModeChange", err.Error()))
			return *state, err
		}

		log.Info(fmt.Sprintf("Host power cycle is required for DPU %s to transition from NicMode to DpuMode", device.BMCAddress()))
		// Align with hostagent: use InterfaceInitialized + ModeUpdate as the mode-change reboot signal.
		cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondInterfaceInitialized, "", string(provisioningv1.DPUCondMessageModeUpdate)))

		state.Phase = provisioningv1.DPURebooting
		if err := dpustate.InitializeDPURebootStatus(ctx, dpu, state, ctrlCtx, provisioningv1.DPUInitializeInterface); err != nil {
			return *state, err
		}
		return *state, nil
	}

	// DPU mode was in NIC mode, updating to DpuMode
	if dpu.Status.DPUMode != provisioningv1.DpuMode {
		log.Info(fmt.Sprintf("DPU %s successfully set to DpuMode", device.BMCAddress()))
		state.DPUMode = provisioningv1.DpuMode
	}

	// Configure Secure Boot if requested, may trigger phase transition
	done, err := reconcileSecureBoot(ctx, dpu, state, tlsClient)
	if err != nil {
		return *state, err
	}
	if done {
		return *state, nil
	}

	// In NIC mode, DPU type was unknown, now it's in DPU mode - updating to the value from the Redfish
	if dpu.Status.DPUType == provisioningv1.DPUTypeUnknown {
		_, chassisInfo, err := tlsClient.GetChassis()
		if err != nil {
			log.Error(err, fmt.Sprintf("Failed to get chassis info for DPU %s", device.BMCAddress()))
			cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), err, "FailedToGetChassisInfo", err.Error()))
			return *state, err
		}

		state.DPUType = chassisInfo.GetBlueFieldVersion()
		if state.DPUType == provisioningv1.DPUTypeUnknown {
			err = fmt.Errorf("unknown DPU type")
			log.Error(err, fmt.Sprintf("Failed to get DPU type for DPU %s", device.BMCAddress()))
			cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), err, "FailedToGetDPUType", err.Error()))
			return *state, err
		}
	}

	result, err := checkCapacity(ctx, dpu, device, ctrlCtx, tlsClient)
	if err != nil {
		err = fmt.Errorf("failed to check capacity: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), err, "FailedToCheckCapacity", err.Error()))
		return *state, err
	}
	switch result {
	case dutil.CapacityInsufficient:
		err = fmt.Errorf("not enough resources for the given DPUFlavor")
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), err, "FailedToCheckResources", err.Error()))
		state.Phase = provisioningv1.DPUError
		return *state, nil
	case dutil.CapacitySatisfied:
		state.Phase = provisioningv1.DPUConfigFWParameters
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), nil, "", ""))
		return *state, nil
	default:
		// send a warning in the condition message, but continue the flow
		state.Phase = provisioningv1.DPUConfigFWParameters
		cond := cutil.NewCondition(
			string(provisioningv1.DPUCondInterfaceInitialized), nil, "UnableToCheckResources",
			fmt.Sprintf("WARNING: unable to check DPU CPU/Memory capacity, the DPUFlavor may be unfit for the DPU, err: %v", err))
		cutil.SetDPUCondition(state, cond)
		return *state, err
	}
}

// reconcileSecureBoot handles Secure Boot configuration during InitializeInterface.
// Returns (true, nil) when a phase transition was set and the caller should return early.
// Returns (false, nil) when no action was needed and the caller should continue.
// Returns (false, err) on retryable errors.
func reconcileSecureBoot(ctx context.Context, dpu *provisioningv1.DPU, state *provisioningv1.DPUStatus, tlsClient *rfclient.Client) (bool, error) {
	tracker, err := dutil.LoadArmRestartTracker(dpu)
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to load ArmRestartTracker")
		return false, err
	}

	if tracker != nil {
		if tracker.AllRestartsDone() {
			return verifySecureBootAfterRestarts(ctx, dpu, state, tlsClient, tracker)
		}
		// Tracker present: verify after restarts or re-assert phase. Re-assert recovers when
		// fluxcd patcher wrote metadata (tracker) but status (phase) patch failed (e.g. 409).
		state.Phase = provisioningv1.DPUPerformArmForceRestart
		return true, nil
	}

	// No spec - skip Secure Boot and continue to capacity check
	if dpu.Spec.SecureBoot == nil {
		return false, nil
	}

	// Initial detection: check if configuration is needed
	return detectAndStageSecureBoot(ctx, dpu, state, tlsClient)
}

// verifySecureBootAfterRestarts verifies that the BMC applied the Secure Boot configuration
// after all ARM restarts completed. On success the tracker is cleared; on terminal error the
// tracker is intentionally preserved to avoid a SerialPatcher race (metadata patch triggering
// a new reconcile before the status patch lands) and to retain forensic state.
//
// Returns (true, nil) to stay in current phase (retry or terminal error),
// (false, nil) on success, or (false, err) on retryable BMC failure.
func verifySecureBootAfterRestarts(ctx context.Context, dpu *provisioningv1.DPU, state *provisioningv1.DPUStatus, tlsClient *rfclient.Client, tracker *dutil.ArmRestartTracker) (bool, error) {
	log := log.FromContext(ctx)

	// Spec.SecureBoot removed mid-flow: clean up orphaned tracker and continue.
	if dpu.Spec.SecureBoot == nil {
		dutil.ClearArmRestartTracker(dpu)
		return false, nil
	}

	_, sbInfo, err := tlsClient.GetSecureBoot()
	if err != nil {
		cutil.SetDPUCondition(state, cutil.NewCondition(
			string(provisioningv1.DPUCondInterfaceInitialized),
			err, "FailedToGetSecureBootStatus", err.Error()))
		return false, err // Retryable - BMC may be temporarily unreachable
	}

	desiredEnabled := *dpu.Spec.SecureBoot
	actualEnabled := sbInfo != nil && sbInfo.IsCurrentlyActive()

	if desiredEnabled == actualEnabled {
		dutil.ClearArmRestartTracker(dpu)
		state.SecureBoot = &provisioningv1.SecureBootStatus{Enabled: &actualEnabled}
		log.Info("Secure Boot verification passed after ARM restarts",
			"dpu", dpu.Name, "enabled", actualEnabled)
		return false, nil
	}

	// Mismatch -- retry while within the verification window.
	// Primary anchor: ArmForceRestarted.LastTransitionTime (marks all restarts completed).
	// Fallback: tracker.IsStale() guards against infinite retry if the condition
	// is permanently absent (e.g., corrupted state after controller restart).
	_, armCond := cutil.GetDPUCondition(state, provisioningv1.DPUCondArmForceRestarted.String())
	withinRetryWindow := false
	if armCond != nil && armCond.Status == metav1.ConditionTrue {
		withinRetryWindow = time.Since(armCond.LastTransitionTime.Time) < secureBootVerificationTimeout
	} else {
		withinRetryWindow = !tracker.IsStale()
	}
	if withinRetryWindow {
		return true, nil
	}

	// Timeout exceeded (verification window or stale tracker)
	state.SecureBoot = &provisioningv1.SecureBootStatus{Enabled: &actualEnabled}
	err = fmt.Errorf("secure boot verification failed after %d restarts: desired=%v, actual=%v",
		tracker.MaxAttempts, desiredEnabled, actualEnabled)
	cutil.SetDPUCondition(state, cutil.NewCondition(
		string(provisioningv1.DPUCondInterfaceInitialized),
		err, "SecureBootConfigurationFailed", err.Error()))
	state.Phase = provisioningv1.DPUError
	return true, nil
}

// detectAndStageSecureBoot checks whether the current hardware Secure Boot state matches
// the desired spec. If a mismatch is found, stages the configuration via BMC and creates
// a tracker to initiate ARM restarts.
// Returns (true, nil) on phase transition, (false, nil) when already in desired state,
// or (false, err) on retryable failure.
func detectAndStageSecureBoot(ctx context.Context, dpu *provisioningv1.DPU, state *provisioningv1.DPUStatus, tlsClient *rfclient.Client) (bool, error) {
	log := log.FromContext(ctx)

	_, sbInfo, err := tlsClient.GetSecureBoot()
	if err != nil {
		cutil.SetDPUCondition(state, cutil.NewCondition(
			string(provisioningv1.DPUCondInterfaceInitialized),
			err, "FailedToGetSecureBootStatus", err.Error()))
		return false, err // Retryable - BMC may be temporarily unreachable
	}

	desiredEnabled := *dpu.Spec.SecureBoot
	currentEnabled := sbInfo != nil && sbInfo.IsCurrentlyActive()

	// Already in desired state - sync status and continue
	if desiredEnabled == currentEnabled {
		state.SecureBoot = &provisioningv1.SecureBootStatus{Enabled: &currentEnabled}
		return false, nil
	}

	// Configuration mismatch - stage change via BMC Redfish
	log.Info("Secure Boot configuration mismatch, staging change",
		"dpu", dpu.Name, "desired", desiredEnabled, "current", currentEnabled)

	if desiredEnabled {
		_, err = tlsClient.EnableSecureBoot()
	} else {
		_, err = tlsClient.DisableSecureBoot()
	}
	if err != nil {
		err = fmt.Errorf("failed to stage Secure Boot: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(
			string(provisioningv1.DPUCondInterfaceInitialized),
			err, "FailedToStageSecureBoot", err.Error()))
		return false, err // Retryable - BMC may be temporarily unreachable
	}

	// Create tracker for restart phase.
	// Per BlueField BMC documentation, the first restart applies the setting
	// and the second restart validates it.
	newTracker := &dutil.ArmRestartTracker{
		InitialGeneration: dpu.Generation,
		MaxAttempts:       secureBootRequiredRestarts,
	}
	if err := dutil.SaveArmRestartTracker(dpu, newTracker); err != nil {
		log.Error(err, "Failed to save ArmRestartTracker")
		return false, err
	}

	state.Phase = provisioningv1.DPUPerformArmForceRestart
	cutil.SetDPUCondition(state, cutil.NewCondition(
		string(provisioningv1.DPUCondInterfaceInitialized),
		nil, "SecureBootConfigurationStaged",
		fmt.Sprintf("Secure Boot staged (current=%v, desired=%v), ARM restarts required", currentEnabled, desiredEnabled)))
	return true, nil // Phase transition - caller should return
}

func checkDPUDeviceReady(dpuDevice *provisioningv1.DPUDevice) error {
	if !conditions.IsTrue(dpuDevice, provisioningv1.ConditionDpuDeviceReady) {
		return fmt.Errorf("DPUDevice %q is not ready", dpuDevice.Name)
	}

	if dpuDevice.Spec.BMCIP == nil {
		return fmt.Errorf("DPUDevice %q has no BMCIP set", dpuDevice.Name)
	}

	return nil
}

func getProductDescription(tlsClient *rfclient.Client) (*rfclient.ProductSpecInfo, error) {
	resp, desc, err := tlsClient.GetProductDescription()
	if err != nil || resp == nil || resp.StatusCode() != http.StatusOK || desc == nil || desc.Mode == nil {
		return nil, fmt.Errorf("failed to get description, err: %v, resp: %+v, desc: %+v", err, resp, desc)
	}
	return desc, nil
}

// checkCapacity checks if the DPU has sufficient resources for the flavor.
func checkCapacity(ctx context.Context, dpu *provisioningv1.DPU, device *provisioningv1.DPUDevice, ctrlCtx *dutil.ControllerContext, tlsClient *rfclient.Client) (dutil.CapacityResult, error) {
	log := log.FromContext(ctx)
	flavor := &provisioningv1.DPUFlavor{}
	if err := ctrlCtx.Client.Get(ctx, types.NamespacedName{Name: dpu.Spec.DPUFlavor, Namespace: dpu.Namespace}, flavor); err != nil {
		return dutil.CapacityUnknown, err
	}
	// check capacity by description
	productSpecInfo, err := getProductDescription(tlsClient)
	if err != nil {
		log.Error(err, fmt.Sprintf("Failed to get product description for DPU %s", device.BMCAddress()))
		return dutil.CapacityUnknown, err
	}

	check := func(data string, parseFunc func(string) *dutil.BlueFieldSpecs) dutil.CapacityResult {
		bfSpecs := parseFunc(data)
		if bfSpecs == nil {
			return dutil.CapacityUnknown
		}
		log.Info("retrieved DPU specs", "bfSpecs", bfSpecs)
		return bfSpecs.CanSatisfy(flavor.Spec.DPUResources)
	}

	// check capacity by part number
	resp, pn, err := tlsClient.GetChassis()
	if err != nil || resp == nil || resp.StatusCode() != http.StatusOK {
		var status string
		if resp != nil {
			status = resp.Status()
		}
		err = fmt.Errorf("failed to get part number, status code: %s, err: %v", status, err)
		return dutil.CapacityUnknown, err
	}
	if result := check(pn.PartNumber, dutil.LookUpPartNumber); result != dutil.CapacityUnknown {
		return result, nil
	}

	if productSpecInfo.Description != nil && *productSpecInfo.Description != "" {
		return check(*productSpecInfo.Description, dutil.ParseDescription), nil
	}
	return dutil.CapacityUnknown, nil
}
