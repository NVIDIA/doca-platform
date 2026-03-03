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

package reboot

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"

	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
)

const (
	shutdownDelayInSeconds        = 5
	bootIDFile                    = "/proc/sys/kernel/random/boot_id"
	defaultMstDevicesPath         = "/dev/mst"
	defaultPostResetBlockDuration = 10 * time.Minute
)

type HandleReboot struct {
	runBash        func(string) (bytes.Buffer, bytes.Buffer, error)
	mstDevicesPath string
	skipBlock      bool
}

func (h *HandleReboot) Name() string {
	return "Handle Reboot"
}

func (h *HandleReboot) ConditionType() string {
	return "RebootHandled"
}

func (h *HandleReboot) ShouldSkip(ctx *operations.Context) bool {
	return false
}

func (h *HandleReboot) ShouldUpdateStatusBeforeContinue(ctx *operations.Context) bool {
	// Must update status before continuing, because the initialBootID is used to check if the host reboot is required.
	return true
}

func (h *HandleReboot) Execute(execCtx context.Context, optCtx *operations.Context) error {
	m, err := h.getRebootMethod(optCtx)
	if err != nil {
		return err
	}

	optCtx.Status.RebootMethod = nil
	switch *m {
	case provisioningv1.RebootMethodPowerCycle:
		return h.execPowerCycle(optCtx)
	case provisioningv1.RebootMethodSystemReboot:
		return h.execSystemReboot(optCtx)
	case provisioningv1.RebootMethodSystemLevelReset:
		return h.execSystemLevelReset(execCtx, optCtx)
	case provisioningv1.RebootMethodFirmwareReset:
		return h.execFirmwareReset(execCtx, optCtx)
	case provisioningv1.RebootMethodNoAction:
		optCtx.Status.RebootMethod = ptr.To(provisioningv1.RebootMethodNoAction)
		return nil
	}
	return fmt.Errorf("unsupported reboot method: %s", *m)
}

func (h *HandleReboot) execPowerCycle(optCtx *operations.Context) error {
	optCtx.Status.RebootMethod = ptr.To(provisioningv1.RebootMethodPowerCycle)
	return nil
}

func (h *HandleReboot) execSystemReboot(optCtx *operations.Context) error {
	optCtx.Status.RebootMethod = ptr.To(provisioningv1.RebootMethodSystemReboot)
	return nil
}

func (h *HandleReboot) execSystemLevelReset(execCtx context.Context, optCtx *operations.Context) error {
	currentRebootID, err := getCurrentRebootID()
	if err != nil {
		return fmt.Errorf("failed to read boot ID file: %w", err)
	}
	optCtx.Status.InitialBootID = ptr.To(currentRebootID)
	optCtx.Status.RebootMethod = ptr.To(provisioningv1.RebootMethodSystemLevelReset)

	// Update status until success.
	optCtx.UpdateStatusUntilSuccess(execCtx)

	// Run the shutdown command.
	if h.runBash == nil {
		h.runBash = bash.Run
	}
	klog.Infof("Shutting down in %d seconds", shutdownDelayInSeconds)
	cmd := fmt.Sprintf("sleep %d && shutdown -h now", shutdownDelayInSeconds)
	_, stderr, err := h.runBash(cmd)
	if err != nil {
		return fmt.Errorf("failed to shut down host: %w, stderr: %s", err, stderr.String())
	}
	return h.blockUntilReset()
}

func (h *HandleReboot) execFirmwareReset(execCtx context.Context, optCtx *operations.Context) error {
	currentRebootID, err := getCurrentRebootID()
	if err != nil {
		return fmt.Errorf("failed to read boot ID file: %w", err)
	}
	optCtx.Status.InitialBootID = ptr.To(currentRebootID)

	// Find the first MST device.
	mstPath := h.mstDevicesPath
	if mstPath == "" {
		mstPath = defaultMstDevicesPath
	}
	devices, err := filepath.Glob(filepath.Join(mstPath, "*"))
	if err != nil {
		return fmt.Errorf("failed to list MST devices: %w", err)
	}
	if len(devices) == 0 {
		return fmt.Errorf("no MST devices found in %s", mstPath)
	}
	device := devices[0]

	optCtx.Status.RebootMethod = ptr.To(provisioningv1.RebootMethodFirmwareReset)

	// Update status until success.
	optCtx.UpdateStatusUntilSuccess(execCtx)

	// Run the firmware reset command.
	if h.runBash == nil {
		h.runBash = bash.Run
	}
	cmd := fmt.Sprintf("mlxfwreset -d %s -y reset", device)
	_, stderr, err := h.runBash(cmd)
	if err != nil {
		return fmt.Errorf("%s: %w (stderr: %s)", cmd, err, stderr.String())
	}
	return h.blockUntilReset()
}

// blockUntilReset blocks until the system resets or the timeout expires.
// The reset/shutdown command returns immediately; without this block the
// agent would continue to the next operation before the machine goes down.
func (h *HandleReboot) blockUntilReset() error {
	if h.skipBlock {
		return nil
	}
	klog.Infof("Reset initiated, waiting up to %v for system to go down...", defaultPostResetBlockDuration)
	time.Sleep(defaultPostResetBlockDuration)
	return fmt.Errorf("system did not reset within %v", defaultPostResetBlockDuration)
}

// getRebootMethod returns the reboot method for this run.
func (h *HandleReboot) getRebootMethod(optCtx *operations.Context) (*provisioningv1.RebootMethodType, error) {
	currentRebootID, err := getCurrentRebootID()
	if err != nil {
		return nil, fmt.Errorf("failed to read boot ID file: %w", err)
	}

	// Hardcode SystemLevelReset for now
	// Represent the legacy flow based on the initial boot ID.
	var reboot *provisioningv1.RebootMethodType
	if hasBeenBooted(optCtx.LatestDPU, currentRebootID) {
		klog.Infof("Host has already been booted, no reboot action")
		reboot = ptr.To(provisioningv1.RebootMethodNoAction)
	} else {
		reboot = ptr.To(provisioningv1.RebootMethodSystemLevelReset)
	}

	// Sanity check for protection against double reboot.
	// means reboot was done physically (stored ID is old boot, current is new).
	// If the logic still returns non-NoAction, we must not trigger again — error instead.
	if *reboot != provisioningv1.RebootMethodNoAction && optCtx.LatestDPU != nil &&
		optCtx.LatestDPU.Status.AgentStatus != nil &&
		optCtx.LatestDPU.Status.AgentStatus.InitialBootID != nil &&
		*optCtx.LatestDPU.Status.AgentStatus.InitialBootID != currentRebootID {
		return nil, fmt.Errorf("reboot already done (InitialBootID %s != current boot ID %s) but logic returned %s; refusing to avoid double reboot",
			*optCtx.LatestDPU.Status.AgentStatus.InitialBootID, currentRebootID, *reboot)
	}
	return reboot, nil
}

func getCurrentRebootID() (string, error) {
	currentRebootID, err := os.ReadFile(bootIDFile)
	if err != nil {
		return "", fmt.Errorf("failed to read boot ID file: %w", err)
	}
	return strings.TrimSpace(string(currentRebootID)), nil
}

func hasBeenBooted(dpu *provisioningv1.DPU, currentRebootID string) bool {
	// Hardcode SystemLevelReset for now
	// Represent the legacy flow based on the initial boot ID.
	return dpu.Status.AgentStatus != nil &&
		dpu.Status.AgentStatus.InitialBootID != nil &&
		*dpu.Status.AgentStatus.InitialBootID != currentRebootID
}
