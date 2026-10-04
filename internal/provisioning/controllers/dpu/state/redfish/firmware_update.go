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
	"fmt"
	"maps"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	rc "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/diag"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	"github.com/go-resty/resty/v2"
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

	if len(blueFieldSoftware.Spec.PldmFwBundle) == 0 {
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

	logger = logger.WithValues("bmc", dpuDevice.BMCAddress())
	ctx = log.IntoContext(ctx, logger)

	psid := dpuDevice.Status.PSID
	if psid == nil || *psid == "" {
		err := fmt.Errorf("PSID is not set for DPUDevice %s/%s", dpuDevice.Namespace, dpuDevice.Name)
		logger.Info(err.Error())
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFwBundleUpdated.String(), err, "WaitingForPSID", err.Error()))
		return *state, err
	}

	if _, ok := lookupByPSID(blueFieldSoftware.Spec.PldmFwBundle, *psid); !ok {
		msg := fmt.Sprintf("no PLDM firmware bundle configured for PSID %s - skipping firmware update", *psid)
		logger.Info(msg)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFwBundleUpdated.String(), nil, "NoPldmFwBundleForPSID", msg))
		state.Phase = provisioningv1.DPUPrepareBFB
		return *state, nil
	}
	pldmFwBundlePath, _ := lookupByPSID(blueFieldSoftware.Status.DownloadedComponents.PldmFwBundle, *psid)
	if pldmFwBundlePath == "" {
		err := fmt.Errorf("waiting for PLDM firmware bundle download (BlueFieldSoftware phase %s)", blueFieldSoftware.Status.Phase)
		logger.Info(err.Error())
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFwBundleUpdated.String(), err, "WaitingForPldmFwBundle", err.Error()))
		return *state, err
	}

	readCtx, cancelRead := rc.ReadContext(ctx)
	defer cancelRead()
	client, err := rc.NewTLSClient(readCtx, dpuDevice.BMCAddress(), dpu.Namespace, ctrlCtx.Client)
	if err != nil {
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFwBundleUpdated.String(), err, "FailedToCreateClient", err.Error()))
		return *state, err
	}
	defer client.CloseIdleConnections()

	// Stale DPU cache between reconcile loops: a later loop can see FwBundleUpdated
	// (reason Updated) from the prior loop while phase is still UpdateFirmware and
	// fall through to PrepareBFB before the required power cycle.
	if firmwareUpdateAwaitingReboot(dpu) {
		logger.Info("firmware update awaiting reboot, moving to Rebooting phase")
		return transitionToFirmwareUpdateReboot(ctx, dpu, state, ctrlCtx)
	}

	cond := cutil.NewCondition(provisioningv1.DPUCondFwBundleUpdated.String(), nil, "Updating", "Updating PLDM Firmware")
	_, existingCond := cutil.GetDPUCondition(&dpu.Status, cond.Type)
	forceUpdate := forceFwUpdateRequested(dpu)
	if existingCond == nil || existingCond.Status != metav1.ConditionTrue {
		if forceUpdate {
			return updatePldmFwBundle(ctx, dpu, ctrlCtx, pldmFwBundlePath, forceUpdate)
		}
		switch err := checkFirmwareVersions(readCtx, client, blueFieldSoftware, *psid); {
		case errors.Is(err, errVersionMismatch):
			logger.Info("firmware version mismatch with PLDM bundle - updating firmware", "reason", err.Error())
			return updatePldmFwBundle(ctx, dpu, ctrlCtx, pldmFwBundlePath, false)
		case err != nil:
			// Read/verification/I/O error: propagate instead of blindly updating firmware.
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFwBundleUpdated.String(), err, "FirmwareVersionCheckFailed", err.Error()))
			return *state, err
		default:
			logger.Info("firmware versions match with PLDM bundle - skipping firmware update")
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFwBundleUpdated.String(), nil, "FirmwareVersionsMatch", "Firmware versions match - skipping firmware update"))
		}
	} else if dpu.Status.PreviousPhase == provisioningv1.DPURebooting {
		switch err := checkFirmwareVersions(readCtx, client, blueFieldSoftware, *psid); {
		case errors.Is(err, errVersionMismatch):
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFwBundleUpdated.String(), err, "FirmwareVersionsMismatch", err.Error()))
			return *state, err
		case err != nil:
			logger.Info("post-reboot firmware verification inconclusive, retrying", "reason", err.Error())
			return *state, err
		default:
			logger.Info("firmware update completed successfully")
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFwBundleUpdated.String(), nil, "FirmwareUpdated", "Firmware updated successfully"))
		}
	}

	state.Phase = provisioningv1.DPUPrepareBFB
	return *state, nil
}

// errVersionMismatch marks an installed firmware version differing from the target.
// Read/verification/I/O errors are returned unwrapped so callers can distinguish
// "needs update" from "could not determine".
var errVersionMismatch = errors.New("firmware version mismatch")

// forceFwUpdateRequested reports whether the DPU is annotated to force the firmware update.
// Forcing bypasses the ERoT comparison stamp check, which otherwise aborts the update task
// when the bundle carries an older stamp than the installed firmware.
func forceFwUpdateRequested(dpu *provisioningv1.DPU) bool {
	force, err := strconv.ParseBool(dpu.Annotations[cutil.DPUForceFwUpdateAnnotation])
	if err != nil {
		return false
	}
	return force
}

// lookupByPSID returns the map value for psid. Exact match wins; otherwise the
// lowest-sorting key that matches case-insensitively is used. Spec keys and
// BMC-reported PSIDs can differ only in case (e.g. mt_0000001774 vs MT_0000001774).
//
// Keys are sorted rather than ranged over directly so that a spec holding two keys
// differing only in case resolves to the same bundle on every reconcile, instead of
// flashing a different one as Go's map iteration order changes.
func lookupByPSID[V any](m map[string]V, psid string) (V, bool) {
	var zero V
	if v, ok := m[psid]; ok {
		return v, true
	}
	for _, k := range slices.Sorted(maps.Keys(m)) {
		if strings.EqualFold(k, psid) {
			return m[k], true
		}
	}
	return zero, false
}

// bundleVersions returns the firmware versions BlueFieldSoftware unpacked from the bundle for
// psid, rejecting a set that is missing any component the firmware update compares.
func bundleVersions(blueFieldSoftware *provisioningv1.BlueFieldSoftware, psid string) (provisioningv1.BluefieldDeviceVersions, error) {
	var versions provisioningv1.BluefieldDeviceVersions
	if blueFieldSoftware.Status.Versions == nil {
		return versions, fmt.Errorf("BlueFieldSoftware versions are not set")
	}

	versions, ok := lookupByPSID(blueFieldSoftware.Status.Versions.BluefieldSoftwareVersions, psid)
	if !ok {
		return versions, fmt.Errorf("firmware versions are not set for PSID %s", psid)
	}

	if versions.BMCVersion == "" {
		return versions, fmt.Errorf("BMC firmware version is not set for PSID %s", psid)
	}

	if versions.BMCErotVersion == "" {
		return versions, fmt.Errorf("BMC ERoT firmware version is not set for PSID %s", psid)
	}

	if versions.SBIOSVersion == "" {
		return versions, fmt.Errorf("DPU SBIOS firmware version is not set for PSID %s", psid)
	}

	if versions.BFNicFwVersion == "" {
		return versions, fmt.Errorf("BF NIC firmware version is not set for PSID %s", psid)
	}

	return versions, nil
}

func checkFirmwareVersions(ctx context.Context, client *rc.Client, blueFieldSoftware *provisioningv1.BlueFieldSoftware, psid string) error {
	ctx, cancel := rc.ReadContext(ctx)
	defer cancel()
	versions, err := bundleVersions(blueFieldSoftware, psid)
	if err != nil {
		return err
	}

	installed, err := componentVersion(client.CheckBMCFirmware(ctx))
	if err != nil {
		return fmt.Errorf("failed to check BMC firmware: %w", err)
	}
	if installed != versions.BMCVersion {
		return fmt.Errorf("BMC firmware version %s is not equal to %s: %w", installed, versions.BMCVersion, errVersionMismatch)
	}

	installed, err = componentVersion(client.CheckBMCEROTFW(ctx))
	if err != nil {
		return fmt.Errorf("failed to check BMC ERoT firmware: %w", err)
	}
	if installed != versions.BMCErotVersion {
		return fmt.Errorf("BMC ERoT firmware version %s is not equal to %s: %w", installed, versions.BMCErotVersion, errVersionMismatch)
	}

	installed, err = componentVersion(client.CheckDPUUEFI(ctx))
	if err != nil {
		return fmt.Errorf("failed to check DPU UEFI firmware: %w", err)
	}
	if installed != versions.SBIOSVersion {
		return fmt.Errorf("DPU SBIOS firmware version %s is not equal to %s: %w", installed, versions.SBIOSVersion, errVersionMismatch)
	}

	installed, err = componentVersion(client.CheckDPUNIC(ctx))
	if err != nil {
		return fmt.Errorf("failed to check CX9 NIC firmware: %w", err)
	}
	if installed != versions.BFNicFwVersion {
		return fmt.Errorf("BF NIC firmware version %s is not equal to %s: %w", installed, versions.BFNicFwVersion, errVersionMismatch)
	}

	return nil
}

// componentVersion validates one Redfish firmware-inventory read and returns the Version.
func componentVersion(resp *resty.Response, info *rc.VersionInfo, err error) (string, error) {
	if err != nil {
		return "", err
	}
	if resp == nil || info == nil {
		return "", errors.New("empty Redfish response")
	}
	if resp.StatusCode() != http.StatusOK {
		return "", fmt.Errorf("unexpected status code %d", resp.StatusCode())
	}
	if info.Version == "" {
		return "", errors.New("firmware inventory entry has no version")
	}
	return info.Version, nil
}

// pldmTaskExceptionError is a Redfish firmware-update task that finished in Exception.
type pldmTaskExceptionError struct {
	taskID   string
	messages []map[string]interface{}
}

func (e pldmTaskExceptionError) Error() string {
	if cause := diag.JoinCriticalMessages(e.messages); cause != "" {
		return fmt.Sprintf("task %s is in Exception state: %s", e.taskID, cause)
	}
	return fmt.Sprintf("task %s is in Exception state", e.taskID)
}

// pldmTaskNotFoundError means the BMC no longer has the task record. This can happen after a
// BMC restart even when the multipart upload completed, so callers must verify the staged
// firmware inventory before deciding whether to continue or resubmit.
type pldmTaskNotFoundError struct {
	taskID string
}

// Error returns a description of the missing firmware update task.
func (e pldmTaskNotFoundError) Error() string {
	return fmt.Sprintf("firmware update task %s was not found", e.taskID)
}

func monitorTask(ctx context.Context, client *rc.Client, taskID string) (bool, error) {
	logger := log.FromContext(ctx)

	_, prog, err := client.CheckTaskProgress(ctx, taskID)
	if rc.HasHTTPStatus(err, http.StatusNotFound) {
		return false, pldmTaskNotFoundError{taskID: taskID}
	}
	if err != nil {
		return false, err
	}

	logger.Info(msgTaskProgress, taskProgressFields(opPLDMFirmware, taskID, prog)...)

	// nolint:goconst
	if prog.TaskState == "Exception" {
		return false, pldmTaskExceptionError{taskID: taskID, messages: prog.Messages}
	}

	if prog.PercentComplete < 100 {
		return false, nil
	}

	return true, nil
}

// checkStagedFirmwareVersions is the fallback for a firmware update whose Redfish task is gone:
// the task record does not survive a BMC restart, the staged firmware does. It compares the
// FirmwareInventory _pending members against the versions BlueFieldSoftware unpacked from the bundle.
func checkStagedFirmwareVersions(ctx context.Context, ctrlCtx *dutil.ControllerContext, client *rc.Client, dpu *provisioningv1.DPU, dpuDevice *provisioningv1.DPUDevice) error {
	blueFieldSoftware := &provisioningv1.BlueFieldSoftware{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: ptr.Deref(dpu.Spec.BlueFieldSoftware, "")}, blueFieldSoftware); err != nil {
		return fmt.Errorf("failed to get BlueFieldSoftware: %w", err)
	}
	versions, err := bundleVersions(blueFieldSoftware, ptr.Deref(dpuDevice.Status.PSID, ""))
	if err != nil {
		return err
	}

	ctx, cancel := rc.ReadContext(ctx)
	defer cancel()
	staged, err := componentVersion(client.CheckPendingBMCFirmware(ctx))
	if err != nil {
		return fmt.Errorf("failed to check pending BMC firmware: %w", err)
	}
	if staged != versions.BMCVersion {
		return fmt.Errorf("pending BMC firmware version %s is not equal to %s: %w", staged, versions.BMCVersion, errVersionMismatch)
	}

	staged, err = componentVersion(client.CheckPendingBMCEROTFW(ctx))
	if err != nil {
		return fmt.Errorf("failed to check pending BMC ERoT firmware: %w", err)
	}
	if staged != versions.BMCErotVersion {
		return fmt.Errorf("pending BMC ERoT firmware version %s is not equal to %s: %w", staged, versions.BMCErotVersion, errVersionMismatch)
	}

	staged, err = componentVersion(client.CheckPendingDPUUEFI(ctx))
	if err != nil {
		return fmt.Errorf("failed to check pending DPU UEFI firmware: %w", err)
	}
	if staged != versions.SBIOSVersion {
		return fmt.Errorf("pending DPU SBIOS firmware version %s is not equal to %s: %w", staged, versions.SBIOSVersion, errVersionMismatch)
	}

	staged, err = componentVersion(client.CheckPendingDPUNIC(ctx))
	if err != nil {
		return fmt.Errorf("failed to check pending CX9 NIC firmware: %w", err)
	}
	if staged != versions.BFNicFwVersion {
		return fmt.Errorf("pending BF NIC firmware version %s is not equal to %s: %w", staged, versions.BFNicFwVersion, errVersionMismatch)
	}

	return nil
}

func submitPldmFirmwareUpdate(ctx context.Context, state *provisioningv1.DPUStatus, client *rc.Client, pldmFwBundle string, force bool, cond *metav1.Condition) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)

	_, erotChassis, err := client.GetErotChassis(ctx)
	if err != nil {
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFWConfigured.String(), err, "FailedToGetErotChassis", err.Error()))
		return *state, err
	}

	nvidiaRaw, ok := erotChassis.Oem["Nvidia"]
	if !ok || nvidiaRaw == nil {
		err := errors.New("ERoT chassis is not found")
		logger.Error(err, "ERoT chassis is not found")
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFWConfigured.String(), err, "ERoTChassisNotFound", err.Error()))
		return *state, err
	}
	nvidiaOem, ok := nvidiaRaw.(map[string]interface{})
	if !ok {
		err := errors.New("ERoT chassis Nvidia OEM format is invalid")
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFWConfigured.String(), err, "ERoTChassisInvalidFormat", err.Error()))
		return *state, err
	}
	statusRaw, ok := nvidiaOem["BackgroundCopyStatus"]
	if !ok || statusRaw == nil {
		err := errors.New("ERoT background copy status is not found")
		logger.Error(err, "ERoT background copy status is not found")
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFWConfigured.String(), err, "ERoTBackgroundCopyStatusNotFound", err.Error()))
		return *state, err
	}
	backgroundCopyStatus, ok := statusRaw.(string)
	if !ok || backgroundCopyStatus == "" {
		err := errors.New("ERoT background copy status format is invalid")
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFWConfigured.String(), err, "ERoTBackgroundCopyStatusInvalidFormat", err.Error()))
		return *state, err
	}
	if backgroundCopyStatus != "Completed" {
		logger.Info("ERoT background copy is not completed, waiting for it to complete")
		return *state, nil
	}

	fwFile, err := os.Open(pldmFwBundle)
	if err != nil {
		err = fmt.Errorf("failed to open %s: %w", pldmFwBundle, err)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFWConfigured.String(), err, "FailedToOpenComponent", err.Error()))
		return *state, err
	}
	defer func() { _ = fwFile.Close() }()
	resp, taskInfo, err := client.UpdateBluefieldFirmwareMultipart(fwFile, force)
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
	logger.Info(msgTaskSubmitted, taskSubmittedFields(opPLDMFirmware, taskInfo)...)
	cutil.SetDPUCondition(state, cond)
	return *state, nil
}

func updatePldmFwBundle(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext, pldmFwBundle string, force bool) (provisioningv1.DPUStatus, error) {
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

	readCtx, cancelRead := rc.ReadContext(ctx)
	defer cancelRead()
	client, err := rc.NewTLSClient(readCtx, dpuDevice.BMCAddress(), dpu.Namespace, ctrlCtx.Client)
	if err != nil {
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFWConfigured.String(), err, "FailedToCreateClient", err.Error()))
		return *state, err
	}
	defer client.CloseIdleConnections()

	cond := cutil.NewCondition(provisioningv1.DPUCondFwBundleSubmitted.String(), nil, "Submitting", "Submitting PLDM Firmware")
	_, existingCond := cutil.GetDPUCondition(&dpu.Status, cond.Type)
	if existingCond == nil || existingCond.Status != metav1.ConditionTrue {
		return submitPldmFirmwareUpdate(ctx, state, client, pldmFwBundle, force, cond)
	}

	if state.RedfishTaskID == nil {
		if firmwareUpdateAwaitingReboot(dpu) {
			logger.Info("firmware update awaiting reboot, moving to Rebooting phase")
			return transitionToFirmwareUpdateReboot(ctx, dpu, state, ctrlCtx)
		}
		return *state, nil
	}

	completed, err := monitorTask(readCtx, client, *state.RedfishTaskID)
	var lostTask pldmTaskNotFoundError
	if errors.As(err, &lostTask) {
		// A BMC restart drops task records even when the upload itself finished, so a task that
		// is gone says nothing on its own. What the BMC has staged does: firmware matching the
		// bundle means the upload completed and activation can go ahead.
		if stagedErr := checkStagedFirmwareVersions(ctx, ctrlCtx, client, dpu, dpuDevice); stagedErr != nil {
			err = fmt.Errorf("%w: %w", lostTask, stagedErr)
			if errors.Is(stagedErr, errVersionMismatch) {
				// Staged firmware is not the submitted bundle. Mark submit incomplete so the
				// next reconcile POSTs update-multipart again.
				state.RedfishTaskID = nil
				cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFwBundleSubmitted.String(), err, "FailedToUpdatePldmFwBundle", err.Error()))
			}
		} else {
			logger.Info("firmware update task is gone but the bundle is staged, continuing", "taskID", lostTask.taskID)
			completed, err = true, nil
		}
	}

	if err != nil {
		var taskException pldmTaskExceptionError
		if errors.As(err, &taskException) {
			// BMC task got exception. Mark submit incomplete so the next reconcile
			// POSTs update-multipart again.
			state.RedfishTaskID = nil
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFwBundleSubmitted.String(), err, "FailedToUpdatePldmFwBundle", err.Error()))
		}
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFWConfigured.String(), err, "FailedToUpdatePldmFwBundle", err.Error()))
		return *state, fmt.Errorf("failed to update PLDM firmware: %w", err)
	} else if completed {
		_, system, err := client.GetSystem(ctx)
		if err != nil {
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFWConfigured.String(), err, "FailedToGetSystem", err.Error()))
			return *state, err
		}
		if !isDPUArmPoweredOff(system) {
			armShutdownCondition := cutil.NewCondition(provisioningv1.DPUCondFwBundleArmShutdown.String(), nil, "ArmShutdown", "Arming DPU for shutdown")
			_, existingCond := cutil.GetDPUCondition(&dpu.Status, armShutdownCondition.Type)
			if existingCond == nil || existingCond.Status != metav1.ConditionTrue {
				_, err := client.ArmShutdown()
				if err != nil {
					cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFWConfigured.String(), err, "FailedToArmShutdown", err.Error()))
					return *state, err
				}
				cutil.SetDPUCondition(state, armShutdownCondition)
			}

			logger.Info("Waiting for DPU Arm to shutdown")
			return *state, nil
		}

		continueCondition := cutil.NewCondition(provisioningv1.DPUCondFwBundleActivated.String(), nil, "Activated", "Activated Pending Bundle")
		_, existingCond := cutil.GetDPUCondition(&dpu.Status, continueCondition.Type)
		if existingCond == nil || existingCond.Status != metav1.ConditionTrue {
			resp, err := client.ActivatePendingBundle()
			if err != nil {
				cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFwBundleActivated.String(), err, "FailedToActivatePendingBundle", err.Error()))
				return *state, err
			}
			if resp.StatusCode() != http.StatusOK {
				activateErr := fmt.Errorf("unexpected status code from ActivatePendingBundle: %s", resp.Status())
				cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFwBundleActivated.String(), activateErr, "FailedToActivatePendingBundle", resp.Status()))
				return *state, activateErr
			}

			logger.Info("successfully activated pending bundle. Moving to Rebooting phase")
			cutil.SetDPUCondition(state, continueCondition)
		} else {
			logger.Info("Pending bundle already activated")
		}

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
	for _, cond := range []provisioningv1.DPUConditionType{
		provisioningv1.DPUCondFwBundleSubmitted,
		provisioningv1.DPUCondFwBundleArmShutdown,
		provisioningv1.DPUCondFwBundleActivated,
	} {
		meta.RemoveStatusCondition(&state.Conditions, cond.String())
	}
	cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondFwBundleUpdated.String(), nil, "Updated", "PLDM Firmware Updated"))
	state.RedfishTaskID = nil
	state.Phase = provisioningv1.DPURebooting
	if err := dutil.InitializeDPURebootStatus(ctx, dpu, state, ctrlCtx, provisioningv1.DPUUpdateFirmware); err != nil {
		return *state, err
	}
	return *state, nil
}

// checkFirmwareUpdateTimeout checks if the BF4 firmware update has exceeded the configured timeout.
//
// The timer is anchored on InterfaceInitialized, the newest condition that no handler of this
// phase writes. It must not be anchored on a condition this phase writes: updatePldmFwBundle
// reuses FWConfigured to report in-phase PLDM failures, and SetDPUCondition rewrites
// LastTransitionTime whenever Status changes, so a BMC task Exception flips FWConfigured
// true -> false and restarts the timer on every BMC retry.
func checkFirmwareUpdateTimeout(state *provisioningv1.DPUStatus, timeout time.Duration) error {
	if timeout <= 0 {
		return nil
	}

	_, startCond := cutil.GetDPUCondition(state, string(provisioningv1.DPUCondInterfaceInitialized))
	if startCond == nil {
		return nil
	}

	elapsed := time.Since(startCond.LastTransitionTime.Time)
	if elapsed <= timeout {
		return nil
	}

	return fmt.Errorf("firmware update timeout exceeded: %v > %v", elapsed, timeout)
}
