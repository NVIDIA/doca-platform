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
	"os"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	rc "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func FirmwareUpdate(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)
	state := dpu.Status.DeepCopy()
	logger.Info("updating BF4 firmware")

	// Check for installation timeout
	if err := checkFirmwareUpdateTimeout(state, ctrlCtx.Options.FirmwareUpdateTimeout); err != nil {
		logger.Error(err, "Firmware update timeout exceeded")
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondFwBundleUpdated), err, "FirmwareUpdateTimeout", err.Error()))
		state.Phase = provisioningv1.DPUError
		return *state, nil
	}

	blueFieldSoftware := &provisioningv1.BlueFieldSoftware{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: ptr.Deref(dpu.Spec.BlueFieldSoftware, "")}, blueFieldSoftware); err != nil {
		if apierrors.IsNotFound(err) {
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFwBundleUpdated.String(), err, "BlueFieldSoftwareNotFound", err.Error()))
			return *state, err
		}
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFwBundleUpdated.String(), err, "FailedToGetBlueFieldSoftware", err.Error()))
		return *state, err
	}

	if blueFieldSoftware.Status.DownloadedComponents.PldmFwBundle == "" {
		logger.Info("no PLDM firmware bundle provided - skipping firmware update")
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFwBundleUpdated.String(), nil, "NoPldmFwBundle", "no PLDM firmware bundle provided - skipping firmware update"))
		state.Phase = provisioningv1.DPUPrepareBFB
		return *state, nil
	}

	dpuDevice := &provisioningv1.DPUDevice{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.DPUDeviceName}, dpuDevice); err != nil {
		if apierrors.IsNotFound(err) {
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFwBundleUpdated.String(), err, "DPUDeviceNotFound", err.Error()))
			return *state, err
		}
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFwBundleUpdated.String(), err, "FailedToGetDPUDevice", err.Error()))
		return *state, err
	}

	client, err := rc.NewTLSClient(ctx, dpuDevice.BMCAddress(), dpu.Namespace, ctrlCtx.Client)
	if err != nil {
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFwBundleUpdated.String(), err, "FailedToCreateClient", err.Error()))
		return *state, err
	}

	// Stale DPU cache between reconcile loops: a later loop can see FwBundleUpdated
	// (reason Updated) from the prior loop while phase is still UpdateFirmware and
	// fall through to PrepareBFB before the required power cycle.
	if firmwareUpdateAwaitingReboot(dpu) {
		logger.Info("firmware update awaiting reboot, moving to Rebooting phase")
		return transitionToFirmwareUpdateReboot(ctx, dpu, state, ctrlCtx)
	}

	cond := cutil.NewCondition(provisioningv1.DPUCondFwBundleUpdated.String(), nil, "Updating", "Updating PLDM Firmware")
	_, existingCond := cutil.GetDPUCondition(&dpu.Status, cond.Type)
	if existingCond == nil || existingCond.Status != metav1.ConditionTrue {
		if checkFirmwareVersions(client, blueFieldSoftware) != nil {
			return updatePldmFwBundle(ctx, dpu, ctrlCtx, blueFieldSoftware.Status.DownloadedComponents.PldmFwBundle)
		} else {
			logger.Info("firmware versions match with PLDM bundle- skipping firmware update")
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFwBundleUpdated.String(), nil, "FirmwareVersionsMatch", "Firmware versions match - skipping firmware update"))
		}
	} else if dpu.Status.PreviousPhase == provisioningv1.DPURebooting {
		if err := checkFirmwareVersions(client, blueFieldSoftware); err != nil {
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFwBundleUpdated.String(), err, "FirmwareVersionsMismatch", err.Error()))
			return *state, err
		} else {
			logger.Info("firmware update completed successfully")
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFwBundleUpdated.String(), nil, "FirmwareUpdated", "Firmware updated successfully"))
		}
	}

	state.Phase = provisioningv1.DPUPrepareBFB
	return *state, nil
}

func checkFirmwareVersions(client *rc.Client, blueFieldSoftware *provisioningv1.BlueFieldSoftware) error {
	if blueFieldSoftware.Status.Versions == nil {
		return fmt.Errorf("BlueFieldSoftware versions are not set")
	}

	if blueFieldSoftware.Status.Versions.BMCVersion == "" {
		return fmt.Errorf("BMC firmware version is not set")
	}

	if blueFieldSoftware.Status.Versions.BMCErotVersion == "" {
		return fmt.Errorf("BMC ERoT firmware version is not set")
	}

	if blueFieldSoftware.Status.Versions.SBIOSVersion == "" {
		return fmt.Errorf("DPU SBIOS firmware version is not set")
	}

	_, bmcFirmwareVersion, err := client.CheckBMCFirmware()
	if err != nil {
		return fmt.Errorf("failed to check BMC firmware: %w", err)
	}

	if bmcFirmwareVersion.Version != blueFieldSoftware.Status.Versions.BMCVersion {
		return fmt.Errorf("BMC firmware version %s is not equal to %s", bmcFirmwareVersion.Version, blueFieldSoftware.Status.Versions.BMCVersion)
	}

	_, bmcEROTFWVersion, err := client.CheckBMCEROTFW()
	if err != nil {
		return fmt.Errorf("failed to check BMC ERoT firmware: %w", err)
	}

	if bmcEROTFWVersion.Version != blueFieldSoftware.Status.Versions.BMCErotVersion {
		return fmt.Errorf("BMC ERoT firmware version %s is not equal to %s", bmcEROTFWVersion.Version, blueFieldSoftware.Status.Versions.BMCErotVersion)
	}

	_, dpuUEFIVersion, err := client.CheckDPUUEFI()
	if err != nil {
		return fmt.Errorf("failed to check DPU UEFI firmware: %w", err)
	}

	if dpuUEFIVersion.Version != blueFieldSoftware.Status.Versions.SBIOSVersion {
		return fmt.Errorf("DPU SBIOS firmware version %s is not equal to %s", dpuUEFIVersion.Version, blueFieldSoftware.Status.Versions.SBIOSVersion)
	}

	return nil
}

func monitorTask(ctx context.Context, client *rc.Client, taskID string) (bool, error) {
	logger := log.FromContext(ctx)

	_, prog, err := client.CheckTaskProgress(taskID)
	if err != nil {
		return false, err
	}

	logger.Info(fmt.Sprintf("taskProgress: %+v", prog))

	// nolint:goconst
	if prog.TaskState == "Exception" {
		return false, fmt.Errorf("task %s is in Exception state: %v", taskID, prog.Messages)
	}

	if prog.PercentComplete < 100 {
		return false, nil
	}

	return true, nil
}

func updatePldmFwBundle(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext, pldmFwBundle string) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)
	state := dpu.Status.DeepCopy()
	logger.Info("updating PLDM firmware")

	dpuDevice := &provisioningv1.DPUDevice{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.DPUDeviceName}, dpuDevice); err != nil {
		if apierrors.IsNotFound(err) {
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFWConfigured.String(), err, "DPUDeviceNotFound", err.Error()))
			return *state, err
		}
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFWConfigured.String(), err, "FailedToGetDPUDevice", err.Error()))
		return *state, err
	}

	client, err := rc.NewTLSClient(ctx, dpuDevice.BMCAddress(), dpu.Namespace, ctrlCtx.Client)
	if err != nil {
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFWConfigured.String(), err, "FailedToCreateClient", err.Error()))
		return *state, err
	}

	cond := cutil.NewCondition(provisioningv1.DPUCondFwBundleSubmitted.String(), nil, "Submitting", "Submitting PLDM Firmware")
	_, existingCond := cutil.GetDPUCondition(&dpu.Status, cond.Type)
	if existingCond == nil || existingCond.Status != metav1.ConditionTrue {
		fwFile, err := os.Open(pldmFwBundle)
		if err != nil {
			err = fmt.Errorf("failed to open %s: %w", pldmFwBundle, err)
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFWConfigured.String(), err, "FailedToOpenComponent", err.Error()))
			return *state, err
		}
		defer func() { _ = fwFile.Close() }()
		resp, taskInfo, err := client.UpdateBluefieldFirmwareMultipart(fwFile, "")
		if err != nil {
			err = fmt.Errorf("failed to update PLDM firmware: %w", err)
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFWConfigured.String(), err, "FailedToUpdatePldmFwBundle", err.Error()))
			return *state, err
		}

		if resp.StatusCode() != http.StatusAccepted {
			err = fmt.Errorf("status code: %d is not Accepted", resp.StatusCode())
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFWConfigured.String(), err, "FailedToUpdateBMCFirmware", err.Error()))
			return *state, err
		}
		state.RedfishTaskID = &taskInfo.ID
		logger.Info(fmt.Sprintf("new pldm firmware update task: %+v", *taskInfo))
		cutil.SetDPUCondition(state, cond)
		return *state, nil
	}

	if state.RedfishTaskID == nil {
		if firmwareUpdateAwaitingReboot(dpu) {
			logger.Info("firmware update awaiting reboot, moving to Rebooting phase")
			return transitionToFirmwareUpdateReboot(ctx, dpu, state, ctrlCtx)
		}
		return *state, nil
	}

	if completed, err := monitorTask(ctx, client, *state.RedfishTaskID); err != nil {
		state.RedfishTaskID = nil
		state.Phase = provisioningv1.DPUError
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFWConfigured.String(), err, "FailedToUpdatePldmFwBundle", err.Error()))
		return *state, fmt.Errorf("failed to update PLDM firmware: %w", err)
	} else if completed {
		resp, err := client.ActivatePendingBundle()
		if err != nil {
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFWConfigured.String(), err, "FailedToActivatePendingBundle", err.Error()))
			return *state, err
		}
		if resp.StatusCode() != http.StatusOK {
			activateErr := fmt.Errorf("unexpected status code from ActivatePendingBundle: %s", resp.Status())
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFWConfigured.String(), activateErr, "FailedToActivatePendingBundle", resp.Status()))
			return *state, activateErr
		}
		logger.Info("successfully activated pending bundle. Moving to Rebooting phase")
		return transitionToFirmwareUpdateReboot(ctx, dpu, state, ctrlCtx)
	} else {
		return *state, nil
	}
}

// firmwareUpdateAwaitingReboot reports whether ActivatePendingBundle completed on a
// prior reconcile loop but the post-update power cycle has not started yet.
func firmwareUpdateAwaitingReboot(dpu *provisioningv1.DPU) bool {
	if dpu.Status.Phase == provisioningv1.DPURebooting || dpu.Status.PreviousPhase == provisioningv1.DPURebooting {
		return false
	}
	_, updatedCond := cutil.GetDPUCondition(&dpu.Status, provisioningv1.DPUCondFwBundleUpdated.String())
	return updatedCond != nil && updatedCond.Status == metav1.ConditionTrue && updatedCond.Reason == "Updated"
}

func transitionToFirmwareUpdateReboot(ctx context.Context, dpu *provisioningv1.DPU, state *provisioningv1.DPUStatus, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	meta.RemoveStatusCondition(&state.Conditions, provisioningv1.DPUCondFwBundleSubmitted.String())
	cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFwBundleUpdated.String(), nil, "Updated", "PLDM Firmware Updated"))
	state.RedfishTaskID = nil
	state.Phase = provisioningv1.DPURebooting
	if err := dutil.InitializeDPURebootStatus(ctx, dpu, state, ctrlCtx, provisioningv1.DPUUpdateFirmware); err != nil {
		return *state, err
	}
	return *state, nil
}

// checkFirmwareUpdateTimeout checks if the BF4 firmware update has exceeded the configured timeout.
// The timer starts when DPUCondFWConfigured becomes true (entry into the Update Firmware phase).
func checkFirmwareUpdateTimeout(state *provisioningv1.DPUStatus, timeout time.Duration) error {
	if timeout <= 0 {
		return nil
	}

	// Anchor on FWConfigured (set when Config FW Parameters completes), not
	// FwBundleUpdated (phase success). Same pattern as OS install timeout vs BFBPrepared.
	_, fwConfiguredCond := cutil.GetDPUCondition(state, string(provisioningv1.DPUCondFWConfigured))
	if fwConfiguredCond == nil {
		return nil
	}

	elapsed := time.Since(fwConfiguredCond.LastTransitionTime.Time)
	if elapsed <= timeout {
		return nil
	}

	return fmt.Errorf("firmware update timeout exceeded: %v > %v", elapsed, timeout)
}
