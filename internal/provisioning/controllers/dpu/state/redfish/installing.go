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
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	rc "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/diag"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	"github.com/go-logr/logr"
	"github.com/go-resty/resty/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	exceptionTaskState = "Exception"

	// osNotRunningReason marks the OSInstalled condition while the DPU is waiting for the OS and
	// dpu-agent. Its message doubles as the store for the last BootProgress read from the BMC.
	osNotRunningReason = "OSNotRunning"

	// bootProgressProbeInterval throttles the BMC BootProgress read. Reconciles run every
	// cutil.RequeueInterval (5s) for as long as the install takes.
	bootProgressProbeInterval = time.Minute
)

func Installing(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)
	state := dpu.Status.DeepCopy()

	cutil.ConfirmDPUCondition(state, provisioningv1.DPUCondBFBPrepared)

	// Check if DPU deletion is requested during OS installation
	if !dpu.DeletionTimestamp.IsZero() {
		logger.Info("DPU deletion requested while in Installing state, cannot delete DPU during OS installation")
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondOSInstalled), nil, "CannotDeleteWhileInstalling", "Cannot delete DPU during OS installation. Wait for completion or timeout."))
	}

	// Check for installation timeout
	if err := dutil.CheckInstallationTimeout(state, ctrlCtx.Options.OSInstallTimeout); err != nil {
		logger.Info("OS installation timeout exceeded", "timeout", ctrlCtx.Options.OSInstallTimeout, "error", err)
		// Best-effort: append a SEL rail hint so an operator can spot
		// ATX/PCIe power drops. No client is constructed yet here, so pass
		// nil and let the helper build one. Hint is folded into err because
		// cutil.NewCondition uses err.Error() as the condition message.
		if hint := bestEffortRailHint(ctx, dpu, ctrlCtx, logger, nil); hint != "" {
			err = fmt.Errorf("%s. %s", err.Error(), hint)
		}
		if last := lastReportedWaitMessage(state); last != "" {
			err = fmt.Errorf("%s. Last reported: %s", err.Error(), last)
		}
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondOSInstalled), err, "InstallationTimeout", err.Error()))
		clearInstallState(dpu.UID)
		state.Phase = provisioningv1.DPUError
		return *state, nil
	}

	device := &provisioningv1.DPUDevice{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.DPUDeviceName}, device); err != nil {
		logger.Error(err, "Failed to get DPUDevice")
		return *state, err
	}

	if device.Labels[provisioningv1.DPUDeviceLabelSkipHWProvisioning] == "true" {
		logger.Info("skip-hw-provisioning label set - skipping BFB installation")
		cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondBFBTransferred, "", ""))
		cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondOSInstalled, "", ""))
		ctrlCtx.DPUInProvisioningMap.Remove(dutil.DPUID(dpu.UID))
		state.Phase = provisioningv1.DPURebooting
		return *state, nil
	}

	if dpu.Status.AgentStatus != nil && dpu.Status.AgentStatus.LastStartupTime != nil {
		if dpu.Status.DPUType != provisioningv1.DPUTypeBlueField4 {
			// The dpu-agent only starts once the BFB is installed and the new OS has booted, so the
			// install task must already have completed. The BMC may have rebooted after the task
			// finished and dropped it before we observed the final state, so mark the transfer as
			// done here and stop tracking the task.
			if dpu.Status.RedfishTaskID != nil {
				logger.Info("dpu-agent reported startup while a BFB install task was still being tracked; treating the task as completed", "taskID", *dpu.Status.RedfishTaskID)
			}
			cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondBFBTransferred, "", ""))
			state.RedfishTaskID = nil
		}

		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondOSInstalled), nil, "OsInstalled", "OS installed, waiting for the DPU agent to start"))
		clearInstallState(dpu.UID)
		ctrlCtx.DPUInProvisioningMap.Remove(dutil.DPUID(dpu.UID))
		state.Phase = provisioningv1.DPUConfig
		logger.Info("installation finished")
		return *state, nil
	}

	msg := "Waiting for DPU OS to finish booting and start dpu-agent"
	if dpu.Status.DPUType == provisioningv1.DPUTypeBlueField4 {
		_, cond := cutil.GetDPUCondition(state, string(provisioningv1.DPUCondChangeBootTarget))
		if cond == nil || cond.Status != metav1.ConditionTrue {
			return installWithRetry(ctx, dpu, ctrlCtx, device, installOsBf4)
		}
	} else {
		_, cond := cutil.GetDPUCondition(state, string(provisioningv1.DPUCondBFBTransferred))
		if cond == nil || cond.Status != metav1.ConditionTrue {
			return installWithRetry(ctx, dpu, ctrlCtx, device, submitAndMonitorBfbInstallTask)
		}
	}

	if bootProgress := bootProgressState(ctx, dpu, ctrlCtx, logger); bootProgress != "" {
		msg = fmt.Sprintf("%s (BootProgress %s)", msg, bootProgress)
	} else if last := lastReportedWaitMessage(state); last != "" {
		// Throttled or unreadable this time round: keep reporting what was last read rather than
		// dropping back to the bare message, which would rewrite the condition on every reconcile.
		msg = last
	}

	logger.Info(msg)
	cond := cutil.NewCondition(string(provisioningv1.DPUCondOSInstalled), nil, osNotRunningReason, msg)
	cond.Status = metav1.ConditionFalse
	cutil.SetDPUCondition(state, cond)
	return *state, nil
}

// lastReportedWaitMessage returns the OSInstalled message previously recorded while waiting, or ""
// when the DPU has not been through the wait yet.
func lastReportedWaitMessage(state *provisioningv1.DPUStatus) string {
	_, cond := cutil.GetDPUCondition(state, string(provisioningv1.DPUCondOSInstalled))
	if cond == nil || cond.Reason != osNotRunningReason {
		return ""
	}
	return cond.Message
}

// bootProgressState renders the DPU Arm boot progress the BMC reports as the Redfish fields it
// came from, or "" when nothing was read — either because the probe is still throttled or because
// the BMC could not be reached. Callers treat the two the same: they fall back to the reading
// already recorded in the OSInstalled condition, so a BMC hiccup costs only the extra detail and
// never changes the condition the DPU would otherwise report.
//
// Both fields are reported because which one carries the answer varies, and not only by platform:
//
//	BF4 (Systems/BlueField_0) names the milestone in LastState ("OSRunning" once the OS is up),
//	    omitting OemLastState — except for codes it has no name for, where LastState is "OEM" and
//	    the raw 9-byte postcode lands in OemLastState instead.
//	BF3 (Systems/Bluefield) names its states in OemLastState ("OsIsRunning", "OsStarting", "UEFI",
//	    "BootRom", the crash-dump states, ...).
//
// Printing whichever are set keeps that split out of the control flow, and both arrive in the same
// GET, so the second field is free.
func bootProgressState(
	ctx context.Context,
	dpu *provisioningv1.DPU,
	ctrlCtx *dutil.ControllerContext,
	logger logr.Logger,
) string {
	if !checkInstallProgressState(dpu.UID) {
		return ""
	}
	dpuDevice := &provisioningv1.DPUDevice{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.DPUDeviceName}, dpuDevice); err != nil {
		logger.V(1).Info("boot progress probe: failed to fetch DPUDevice", "err", err)
		return ""
	}
	client, err := rc.NewTLSClient(ctx, dpuDevice.BMCAddress(), dpu.Namespace, ctrlCtx.Client)
	if err != nil {
		logger.V(1).Info("boot progress probe: failed to construct client", "err", err)
		return ""
	}
	_, system, err := client.GetSystem()
	if err != nil || system == nil {
		logger.V(1).Info("boot progress probe: failed to get Redfish system", "err", err)
		return ""
	}
	var fields []string
	if s := system.BootProgress.LastState; s != "" {
		fields = append(fields, fmt.Sprintf("LastState=%q", s))
	}
	if s := system.BootProgress.OemLastState; s != "" {
		fields = append(fields, fmt.Sprintf("OemLastState=%q", s))
	}
	return strings.Join(fields, " ")
}

type installBFOSFn func(context.Context, *provisioningv1.DPU, *dutil.ControllerContext, *provisioningv1.DPUDevice) (provisioningv1.DPUStatus, error)

// installWithRetry runs one OS install attempt per reconcile. Failures wrapped
// as restartOSInstallError stay in OS Installing (controller RequeueAfter
// retries later) until Options.OSInstallRetries is exhausted, then transition
// to Error. Other errors are returned as-is. The retry counter is not cleared
// on nil: install helpers also return nil while in progress (task polls,
// waiting on another update). It is cleared when OSInstallRetries is exhausted,
// or when installation finishes successfully.
func installWithRetry(
	ctx context.Context,
	dpu *provisioningv1.DPU,
	ctrlCtx *dutil.ControllerContext,
	dpuDevice *provisioningv1.DPUDevice,
	fn installBFOSFn,
) (provisioningv1.DPUStatus, error) {
	state, err := fn(ctx, dpu, ctrlCtx, dpuDevice)
	if err == nil {
		return state, nil
	}
	if !isRestartOSInstallError(err) {
		return state, err
	}

	maxRuns := osInstallRetries(ctrlCtx.Options)
	// Start a new OS install run up to maxRuns times.
	count := incrementInstallRetryCounter(dpu.UID)
	logger := log.FromContext(ctx)
	if count >= maxRuns {
		logger.Info("max number of OS installation runs reached", "maxRuns", maxRuns, "lastError", err)
		clearInstallState(dpu.UID)
		state.Phase = provisioningv1.DPUError
		return state, nil
	}

	logger.Info("installation failed, will retry", "attempt", count, "maxRuns", maxRuns, "error", err)
	state.Phase = provisioningv1.DPUOSInstalling
	state.RedfishTaskID = nil
	return state, err
}

func osInstallRetries(opts dutil.DPUOptions) int {
	if opts.OSInstallRetries > 0 {
		return int(opts.OSInstallRetries)
	}
	return int(dutil.DefaultOSInstallRetries)
}

func installOsBf4(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext, dpuDevice *provisioningv1.DPUDevice) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)
	state := dpu.Status.DeepCopy()
	logger.Info("installing OS for BlueField 4")

	bfbRegistryAddr, err := getBFBRegistryAddress(ctx, ctrlCtx)
	if err != nil {
		err = fmt.Errorf("failed to get bfb-registry address: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondOSInstalled), err, "FailToGetBFBRegistryAddress", err.Error()))
		return *state, err
	}

	bluefieldSoftware := &provisioningv1.BlueFieldSoftware{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: ptr.Deref(dpu.Spec.BlueFieldSoftware, "")}, bluefieldSoftware); err != nil {
		err = fmt.Errorf("failed to get bluefield software: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondOSInstalled), err, "FailToGetBlueFieldSoftware", err.Error()))
		return *state, err
	}

	if bluefieldSoftware.Status.Phase != provisioningv1.BlueFieldSoftwareReady {
		err = fmt.Errorf("bluefield software is not ready")
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondOSInstalled), err, "BlueFieldSoftwareIsNotReady", err.Error()))
		return *state, err
	}

	schemes := []string{"http://", "https://"}
	for _, prefix := range schemes {
		bfbRegistryAddr = strings.TrimPrefix(bfbRegistryAddr, prefix)
	}

	client, err := rc.NewTLSClient(ctx, dpuDevice.BMCAddress(), dpu.Namespace, ctrlCtx.Client)
	if err != nil {
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondOSInstalled), err, "FailedToCreateClient", err.Error()))
		return *state, err
	}

	if _, err := client.CheckOSImage(); err != nil {
		err = fmt.Errorf("failed to check OS image: %w", err)
		logger.Error(err, "Failed to check OS image")
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondOSInstalled), err, "FailToCheckOSImage", err.Error()))
		return *state, err
	}

	imageURI := filepath.Join(bfbRegistryAddr, bluefieldSoftware.Status.DownloadedComponents.OsIso)
	if err := reconcileBf4ArmTransfer(logger, dpu, state, client, provisioningv1.DPUCondIsoTransferred, imageURI, client.InstallBluefieldArmImage, "ISO"); err != nil {
		logger.Error(err, "Failed to install ISO", "error", err)
		return *state, err
	}
	if _, c := cutil.GetDPUCondition(state, string(provisioningv1.DPUCondIsoTransferred)); c == nil || c.Status != metav1.ConditionTrue {
		return *state, nil
	}

	osImage, err := client.CheckOSImage()
	if err != nil {
		err = fmt.Errorf("failed to check OS image: %w", err)
		logger.Error(err, "Failed to check OS image")
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondOSInstalled), err, "FailToCheckOSImage", err.Error()))
		return *state, err
	}
	logger.Info("ISO transferred, starting to install config", "osImage", osImage.Version)

	if _, err := client.CheckConfigImage(); err != nil {
		err = fmt.Errorf("failed to check config image: %w", err)
		logger.Error(err, "Failed to check config image")
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondOSInstalled), err, "FailToCheckConfigImage", err.Error()))
		return *state, err
	}

	configURI := filepath.Join(bfbRegistryAddr, dpu.Status.BFCFGFile)
	if err := reconcileBf4ArmTransfer(logger, dpu, state, client, provisioningv1.DPUCondConfigTransferred, configURI, client.InstallBluefieldArmConfig, "config"); err != nil {
		logger.Error(err, "Failed to install config", "error", err)
		return *state, err
	}
	if _, c := cutil.GetDPUCondition(state, string(provisioningv1.DPUCondConfigTransferred)); c == nil || c.Status != metav1.ConditionTrue {
		return *state, nil
	}

	configImage, err := client.CheckConfigImage()
	if err != nil {
		err = fmt.Errorf("failed to check config image: %w", err)
		logger.Error(err, "Failed to check config image")
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondOSInstalled), err, "FailToCheckConfigImage", err.Error()))
		return *state, err
	}

	logger.Info("Config transferred, starting to insert virtual media", "configImage", configImage.Version)

	_, err = client.InsertVirtualMediaImage()
	if err != nil {
		err = fmt.Errorf("failed to insert virtual media image: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondVirtualMediaInserted), err, "FailToInsertVirtualMediaImage", "Failed to insert virtual media image"))
		return *state, newRestartOSInstallError(err)
	}

	_, err = client.InsertVirtualMediaConfig()
	if err != nil {
		err = fmt.Errorf("failed to insert virtual media config: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondVirtualMediaInserted), err, "FailToInsertVirtualMediaConfig", "Failed to insert virtual media config"))
		return *state, newRestartOSInstallError(err)
	}

	_, err = client.SetBootTarget("None", false)
	if err != nil {
		err = fmt.Errorf("failed to set boot target: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondChangeBootTarget), err, "FailToSetBootTarget", "Failed to set boot target to None"))
		return *state, newRestartOSInstallError(err)
	}

	_, settings, err := client.GetSettings()
	if err != nil {
		err = fmt.Errorf("failed to get settings: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondChangeBootTarget), err, "FailToGetSettings", "Failed to get settings"))
		return *state, newRestartOSInstallError(err)
	}

	if settings.Boot.BootSourceOverrideTarget != "None" {
		err = fmt.Errorf("boot source override target is not None")
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondChangeBootTarget), err, "BootTargetNotApplied", fmt.Sprintf("Boot source override target is not None: %s", settings.Boot.BootSourceOverrideTarget)))
		return *state, newRestartOSInstallError(err)
	}

	_, err = client.SetBootTarget("Usb", true)
	if err != nil {
		err = fmt.Errorf("failed to set boot target: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondChangeBootTarget), err, "FailToSetBootTarget", "Failed to set boot target to USB"))
		return *state, newRestartOSInstallError(err)
	}

	_, settings, err = client.GetSettings()
	if err != nil {
		err = fmt.Errorf("failed to get settings: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondChangeBootTarget), err, "FailToGetSettings", "Failed to get settings"))
		return *state, newRestartOSInstallError(err)
	}

	if settings.Boot.BootSourceOverrideTarget != "Usb" {
		err = fmt.Errorf("boot source override target is not Usb")
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondChangeBootTarget), err, "BootTargetNotApplied", fmt.Sprintf("Boot source override target is not Usb: %s", settings.Boot.BootSourceOverrideTarget)))
		return *state, newRestartOSInstallError(err)
	}

	_, err = client.ChassisReset()
	if err != nil {
		err = fmt.Errorf("failed to reset chassis: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondChassisReset), err, "FailToResetChassis", "Failed to reset chassis"))
		return *state, newRestartOSInstallError(err)
	}

	logger.Info("Chassis reset, waiting for the DPU agent to start")

	state.RedfishTaskID = nil

	vmCond := cutil.NewCondition(string(provisioningv1.DPUCondVirtualMediaInserted), nil, "", "Virtual media inserted")
	vmCond.Status = metav1.ConditionTrue
	cutil.SetDPUCondition(state, vmCond)

	bootCond := cutil.NewCondition(string(provisioningv1.DPUCondChangeBootTarget), nil, "", "Boot target changed to USB, chassis reset.")
	bootCond.Status = metav1.ConditionTrue
	cutil.SetDPUCondition(state, bootCond)

	return *state, nil
}

// reconcileBf4ArmTransfer submits or polls a single BlueField ARM install Redfish task (ISO or config)
// and updates state. It uses state.RedfishTaskID (not dpu.Status) so a completed ISO step can start
// the config step in the same reconcile without reusing the prior task id from the API object.
func reconcileBf4ArmTransfer(
	logger logr.Logger,
	dpu *provisioningv1.DPU,
	state *provisioningv1.DPUStatus,
	client *rc.Client,
	condType provisioningv1.DPUConditionType,
	uri string,
	install func(string) (*resty.Response, *rc.TaskInfo, error),
	installDesc string,
) error {
	_, cond := cutil.GetDPUCondition(state, string(condType))
	if cond != nil && cond.Status == metav1.ConditionTrue {
		return nil
	}

	condKey := string(condType)

	if state.RedfishTaskID == nil {
		resp, taskInfo, err := install(uri)
		if err != nil {
			err = fmt.Errorf("failed to install %s: %w", installDesc, err)
			cutil.SetDPUCondition(state, cutil.NewCondition(condKey, err, "FailToInstall", err.Error()))
			return newRestartOSInstallError(err)
		}
		if resp.StatusCode() == http.StatusBadRequest && strings.Contains(resp.String(), "Another update is in progress") {
			logger.Info("another update is in progress, waiting for it to finish", "dpuName", dpu.Name)
			return nil
		}
		if resp.StatusCode() != http.StatusAccepted {
			err = fmt.Errorf("get status: %s", resp.Status())
			logger.Error(err, "Failed to install component", "component", installDesc, "status", resp.Status(), "body", resp.String())
			cutil.SetDPUCondition(state, cutil.NewCondition(condKey, err, "FailToInstall", resp.String()))
			return newRestartOSInstallError(fmt.Errorf("%w: %s", err, resp.String()))
		}
		state.RedfishTaskID = &taskInfo.ID
		logger.Info(fmt.Sprintf("new install task: %+v", *taskInfo))
		return nil
	}

	resp, prog, err := client.CheckTaskProgress(*state.RedfishTaskID)
	if err != nil {
		err = fmt.Errorf("failed to check task progress: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(condKey, err, "FailToCheckProgress", err.Error()))
		return err
	}
	if resp.StatusCode() != http.StatusOK {
		err = fmt.Errorf("get status: %s", resp.Status())
		logger.Error(err, "Failed to check task progress", "status", resp.Status(), "body", resp.String())
		cutil.SetDPUCondition(state, cutil.NewCondition(condKey, err, "FailToCheckProgress", resp.String()))
		return fmt.Errorf("%w: %s", err, resp.String())
	}
	if prog.TaskState == exceptionTaskState {
		taskErr := fmt.Errorf("task %s is in Exception state: %v", *state.RedfishTaskID, prog.Messages)
		cutil.SetDPUCondition(state, cutil.NewCondition(condKey, taskErr, "FailToInstall", fmt.Sprintf("Task %s is in Exception state: %v", *state.RedfishTaskID, prog.Messages)))
		return newRestartOSInstallError(taskErr)
	}
	logger.Info(fmt.Sprintf("taskProgress: %+v", prog), "component", installDesc)
	if prog.PercentComplete < 100 {
		taskProgress := fmt.Sprintf("install task %d%% complete", prog.PercentComplete)
		c := cutil.NewCondition(condKey, nil, "TaskProgress", taskProgress)
		c.Status = metav1.ConditionFalse
		cutil.SetDPUCondition(state, c)
		return nil
	}

	state.RedfishTaskID = nil
	done := cutil.NewCondition(condKey, nil, "", "")
	done.Status = metav1.ConditionTrue
	cutil.SetDPUCondition(state, done)
	return nil
}

func submitAndMonitorBfbInstallTask(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext, dpuDevice *provisioningv1.DPUDevice) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)
	state := dpu.Status.DeepCopy()

	client, err := rc.NewTLSClient(ctx, dpuDevice.BMCAddress(), dpu.Namespace, ctrlCtx.Client)
	if err != nil {
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondBFBTransferred), err, "FailedToCreateClient", err.Error()))
		return *state, err
	}

	if dpu.Status.RedfishTaskID == nil {
		bfbRegistryAddr, err := getBFBRegistryAddress(ctx, ctrlCtx)
		if err != nil {
			err = fmt.Errorf("failed to get bfb-registry address: %w", err)
			cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondBFBTransferred), err, "FailToGetBFBRegistryAddress", err.Error()))
			return *state, err
		}
		logger.Info("submit BFB install task", "will use bfbRegistry", bfbRegistryAddr, "bfbFile", dpu.Status.BFBFile, "bfcfgFile", dpu.Status.BFCFGFile)
		resp, taskInfo, err := client.InstallBFB(concatBFBAndBFCFGPath(bfbRegistryAddr, dpu.Status.BFBFile, dpu.Status.BFCFGFile))
		if err != nil {
			err = fmt.Errorf("failed to install BFB: %w", err)
			cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondBFBTransferred), err, "FailToInstall", err.Error()))
			return *state, newRestartOSInstallError(err)
		} else if resp.StatusCode() == http.StatusBadRequest && strings.Contains(resp.String(), "Another update is in progress") {
			logger.Info("another update is in progress, waiting for it to finish", "dpuName", dpu.Name)
			return *state, nil
		} else if resp.StatusCode() != http.StatusAccepted {
			err = buildInstallBFBError(logger, resp.Status(), resp.String())
			cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondBFBTransferred), err, "FailToInstall", err.Error()))
			return *state, newRestartOSInstallError(err)
		}
		// Update the state with the task ID so it's reflected in the returned status
		state.RedfishTaskID = &taskInfo.ID

		logger.Info(fmt.Sprintf("new install task: %+v", *taskInfo))
		return *state, nil
	}

	// check progress
	resp, prog, err := client.CheckTaskProgress(*dpu.Status.RedfishTaskID)
	if err != nil {
		err = fmt.Errorf("failed to check task progress: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondBFBTransferred), err, "FailToCheckProgress", err.Error()))
		return *state, err
	} else if resp.StatusCode() != http.StatusOK {
		enrichedErr := buildNon200ProgressError(ctx, dpu, ctrlCtx, logger, client, resp)
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondBFBTransferred), enrichedErr, "FailToCheckProgress", enrichedErr.Error()))
		return *state, enrichedErr
	}
	if prog.TaskState == exceptionTaskState {
		taskErr := buildExceptionTaskError(ctx, dpu, ctrlCtx, logger, client, resp, prog)
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondBFBTransferred), taskErr, "FailToInstall", taskErr.Error()))
		return *state, newRestartOSInstallError(taskErr)
	}

	logger.Info(fmt.Sprintf("taskProgress: %+v", prog))
	if prog.PercentComplete < 100 {
		taskProgress := fmt.Sprintf("install task %d%% complete", prog.PercentComplete)
		cond := cutil.NewCondition(string(provisioningv1.DPUCondBFBTransferred), nil, "TaskProgress", taskProgress)
		cond.Status = metav1.ConditionFalse
		cutil.SetDPUCondition(state, cond)
		return *state, nil
	}

	state.RedfishTaskID = nil
	cond := cutil.NewCondition(string(provisioningv1.DPUCondBFBTransferred), nil, "", "")
	cond.Status = metav1.ConditionTrue
	cutil.SetDPUCondition(state, cond)
	return *state, nil
}

// getBFBRegistryAddress returns the full bfb-registry address
func getBFBRegistryAddress(ctx context.Context, ctrlCtx *dutil.ControllerContext) (string, error) {
	if ctrlCtx.Options.BFBRegistryLoadBalancer != "" {
		return ctrlCtx.Options.BFBRegistryLoadBalancer, nil
	}
	return cutil.GetBFBRegistryAddressWithPort(ctx, ctrlCtx.Client, os.Getenv("POD_NAMESPACE"), ctrlCtx.Options.BFBRegistry)
}

// concatBFBAndBFCFGPath returns the bfb-registry path of concatenated bfbFile and bfcfgFile
// Given bfbFile is /bfb/file.bfb and bfcfgFile is /bfb/bfcfg/file.cfg,
// it returns /bfb/??file.bfb,bfcfg/file.cfg?/bfb-to-install
func concatBFBAndBFCFGPath(bfbRegistry string, bfbFile string, bfcfgFile string) string {
	schemes := []string{"http://", "https://"}
	for _, prefix := range schemes {
		bfbRegistry = strings.TrimPrefix(bfbRegistry, prefix)
	}
	bfCfg := strings.TrimPrefix(bfcfgFile, "/"+cutil.BFBBaseDir+"/")
	return filepath.Join(bfbRegistry, cutil.BFBBaseDir, fmt.Sprintf("??%s,%s?", filepath.Base(bfbFile), bfCfg), "bfb-to-install")
}

// responseBodyMaxChars trims a Redfish response body so we don't blow up the
// condition message field. ~1 KiB is enough to capture MessageId + a small
// JSON snippet; the full body is always logged at Error level for forensics.
const responseBodyMaxChars = 1024

// truncateForCondition trims a Redfish response body to responseBodyMaxChars.
func truncateForCondition(body string) string {
	if len(body) <= responseBodyMaxChars {
		return body
	}
	return body[:responseBodyMaxChars] + "...(truncated)"
}

// buildInstallBFBError formats a non-202 response to the BFB submit
// (UpdateService.SimpleUpdate) into a condition error. It keeps the legacy
// "get status: %s" prefix and folds in the Redfish error message(s), falling
// back to the raw (truncated) body when the response is not a parseable
// Redfish error envelope.
func buildInstallBFBError(logger logr.Logger, status, body string) error {
	parts := []string{fmt.Sprintf("get status: %s", status)}
	if msgs := rc.ErrorMessages(body); len(msgs) > 0 {
		parts = append(parts, strings.Join(msgs, "; "))
	} else if body != "" {
		parts = append(parts, "body: "+truncateForCondition(body))
	}
	err := fmt.Errorf("%s", strings.Join(parts, ". "))
	logger.Error(err, "Failed to install BFB", "status", status, "body", body)
	return err
}

// buildNon200ProgressError formats the enriched error for a non-200 response
// from CheckTaskProgress. Preserves the legacy "get status: ... is not OK"
// prefix for tools grepping on it; appends taskID, classified hint, optional
// rail hint (only on power-related statuses), and a truncated body. Also
// emits the full forensic detail to the logger.
func buildNon200ProgressError(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext, logger logr.Logger, client *rc.Client, resp *resty.Response) error {
	status := resp.Status()
	body := resp.String()
	parts := []string{
		fmt.Sprintf("get status: %s is not OK", status),
		fmt.Sprintf("taskID=%s", *dpu.Status.RedfishTaskID),
	}
	if hint := diag.ClassifyHTTPStatus(resp.StatusCode(), body); hint != "" {
		parts = append(parts, "Hint: "+hint)
	}
	if diag.ShouldProbeRails(resp.StatusCode()) {
		if rh := bestEffortRailHint(ctx, dpu, ctrlCtx, logger, client); rh != "" {
			parts = append(parts, rh)
		}
	}
	if body != "" {
		parts = append(parts, "body: "+truncateForCondition(body))
	}
	enrichedErr := fmt.Errorf("%s", strings.Join(parts, ". "))
	logger.Error(enrichedErr, "CheckTaskProgress returned non-OK status",
		"taskID", *dpu.Status.RedfishTaskID,
		"status", status,
		"body", body)
	return enrichedErr
}

// buildExceptionTaskError formats the enriched error for a Redfish task that
// ended in TaskState=Exception. Preserves the legacy "Task X is in Exception
// state" prefix; appends a translated MessageId hint (with BMC Resolution
// when present) or the raw messages dump for unknown MessageIds, and a
// best-effort SEL rail hint (ATX-off can land in any MessageId). Also emits
// the full forensic detail to the logger.
func buildExceptionTaskError(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext, logger logr.Logger, client *rc.Client, resp *resty.Response, prog *rc.TaskProgress) error {
	baseMsg := fmt.Sprintf("Task %s is in Exception state", *dpu.Status.RedfishTaskID)
	var trailers []string
	if translated, found := diag.TranslateTaskMessages(prog.Messages); found {
		trailers = append(trailers, fmt.Sprintf("Hint: %s (MessageId=%s)", translated.Hint, translated.MessageID))
		if translated.Resolution != "" {
			trailers = append(trailers, "BMC Resolution: "+translated.Resolution)
		}
	} else {
		// Unknown MessageId - keep raw-messages shape so we never regress
		// for messages we have not cataloged yet.
		trailers = append(trailers, fmt.Sprintf("messages=%v", prog.Messages))
	}
	if rh := bestEffortRailHint(ctx, dpu, ctrlCtx, logger, client); rh != "" {
		trailers = append(trailers, rh)
	}
	taskErr := fmt.Errorf("%s: %s", baseMsg, strings.Join(trailers, ". "))
	logger.Error(taskErr, "Failed to install BFB",
		"taskID", *dpu.Status.RedfishTaskID,
		"messages", prog.Messages,
		"body", resp.String())
	return taskErr
}

// railHintProbeTimeout caps the best-effort BMC SEL probe so a slow/unreachable
// BMC cannot delay the failure path. Propagated into the HTTP layer via the
// request context, so a blackholed BMC cannot keep the call alive past this
// budget.
const railHintProbeTimeout = 2 * time.Second

// railHintMaxEntries caps how many of the most recent SEL entries we scan for
// a low-rail event. Sensor threshold events are emitted only on crossings, so
// the relevant 12V_ATX / 12V_PCIe event for the current install attempt is
// almost always within the last few entries; 20 is a conservative window.
const railHintMaxEntries = 20

// bestEffortRailHint queries the BMC SEL for a recent low-rail event on the
// monitored power rails (12V_ATX, 12V_PCIe) and returns a short operator hint
// if one is found. All errors (DPUDevice fetch, client construction, Redfish
// call, missing fields) are silently swallowed - this is supplemental
// diagnostic context, never a hard requirement.
//
// If existingClient is non-nil, it is reused as-is to avoid the extra
// GetProductDescription verification round-trip that NewTLSClient performs.
// Callers in branches that already have a validated client (non-200,
// Exception) should pass it; the timeout branch passes nil.
func bestEffortRailHint(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext, logger logr.Logger, existingClient *rc.Client) string {
	cl := existingClient
	if cl == nil {
		// NewTLSClient (incl. its GetProductDescription verification probe) is
		// bounded by the parent reconcile context, NOT by railHintProbeTimeout.
		// We accept this gap because the reconcile context is the natural upper
		// bound and tightening it would mask real TLS setup failures as opaque
		// "context deadline exceeded" entries in the V(1) log.
		device := &provisioningv1.DPUDevice{}
		if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.DPUDeviceName}, device); err != nil {
			logger.V(1).Info("rail hint probe: failed to fetch DPUDevice", "err", err)
			return ""
		}
		var err error
		cl, err = rc.NewTLSClient(ctx, device.BMCAddress(), dpu.Namespace, ctrlCtx.Client)
		if err != nil {
			logger.V(1).Info("rail hint probe: failed to construct client", "err", err)
			return ""
		}
	}
	probeCtx, cancel := context.WithTimeout(ctx, railHintProbeTimeout)
	defer cancel()
	return probeRailHint(probeCtx, cl, logger)
}

// probeRailHint executes the SEL fetch + scan against an already-constructed
// Redfish client. Split out from bestEffortRailHint so tests can inject a
// mock client directly without going through the controller-runtime cache.
// Bounded by ctx (propagated into the HTTP layer via SetContext).
func probeRailHint(ctx context.Context, client *rc.Client, logger logr.Logger) string {
	resp, entries, err := client.GetSELEntries(ctx)
	if err != nil || entries == nil {
		var status string
		if resp != nil {
			status = resp.Status()
		}
		logger.V(1).Info("rail hint probe: GetSELEntries failed", "err", err, "status", status)
		return ""
	}

	// Walk entries newest-last (BMC returns oldest-first); cap scan window so
	// a long-lived BMC with thousands of entries cannot stall us.
	members := entries.Members
	start := 0
	if len(members) > railHintMaxEntries {
		start = len(members) - railHintMaxEntries
	}
	for i := len(members) - 1; i >= start; i-- {
		e := members[i]
		if e.MessageID != diag.SensorThresholdLowMessageID {
			continue
		}
		if len(e.MessageArgs) == 0 {
			continue
		}
		rail := e.MessageArgs[0]
		if _, ok := diag.PowerRailHints[rail]; !ok {
			continue
		}
		var reading, threshold string
		if len(e.MessageArgs) > 1 {
			reading = e.MessageArgs[1]
		}
		if len(e.MessageArgs) > 2 {
			threshold = e.MessageArgs[2]
		}
		return diag.FormatRailHint(rail, reading, threshold)
	}
	return ""
}

// installProgressProbedAt records when BootProgress was last read from the BMC, per DPU UID, so the
// probe can be throttled across reconciles.
var installProgressProbedAt sync.Map

// checkInstallProgressState reports whether this DPU is due a BMC read of its install progress.
func checkInstallProgressState(dpuUID types.UID) bool {
	if v, ok := installProgressProbedAt.Load(dpuUID); ok && time.Since(v.(time.Time)) < bootProgressProbeInterval {
		return false
	}
	installProgressProbedAt.Store(dpuUID, time.Now())
	return true
}

// installRetryCounter tracks failed retryable OS install attempts per DPU UID across reconciles
var installRetryCounter sync.Map

func incrementInstallRetryCounter(dpuUID types.UID) int {
	for {
		v, loaded := installRetryCounter.Load(dpuUID)
		if !loaded {
			if _, existed := installRetryCounter.LoadOrStore(dpuUID, 1); !existed {
				return 1
			}
			continue
		}
		count := v.(int) + 1
		if installRetryCounter.CompareAndSwap(dpuUID, v, count) {
			return count
		}
	}
}

// clearInstallState drops every piece of per-DPU install state this file keeps across reconciles.
func clearInstallState(dpuUID types.UID) {
	installRetryCounter.Delete(dpuUID)
	installProgressProbedAt.Delete(dpuUID)
}

// restartOSInstallError marks a failed OS install run that should count toward
// OSInstallRetries and start a new install (clear RedfishTaskID) on the next
// reconcile. Other errors still requeue while the phase is OS Installing; they
// just do not start a new run.
type restartOSInstallError struct {
	err error
}

func (e *restartOSInstallError) Error() string {
	if e == nil || e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *restartOSInstallError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

// newRestartOSInstallError wraps err as a failed OS install run.
func newRestartOSInstallError(err error) error {
	if err == nil {
		return nil
	}
	return &restartOSInstallError{err: err}
}

// isRestartOSInstallError reports whether err is a failed OS install run that
// Installing should count toward OSInstallRetries.
func isRestartOSInstallError(err error) bool {
	var ie *restartOSInstallError
	return errors.As(err, &ie)
}
