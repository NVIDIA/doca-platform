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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"

	"github.com/Masterminds/semver/v3"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
)

const (
	shutdownDelayInSeconds        = 5
	bootIDFile                    = "/proc/sys/kernel/random/boot_id"
	defaultMstDevicesPath         = "/dev/mst"
	defaultPostResetBlockDuration = 10 * time.Minute

	// maxRebootSequenceCount caps RebootSequenceCount (non-NoAction RebootMethod runs in a row)
	// before the agent refuses further host reboot until NoAction resets the counter.
	maxRebootSequenceCount int32 = 5

	// maxConditionMessageLen bounds Condition.Message (mlxfwreset JSON can be large).
	maxConditionMessageLen = 8192

	// MinRebootDiscoveryMFTVersion is the minimum MFT (mlxconfig / mlxfwreset) version
	// required to use device-query path (RebootMethodDiscovery=true).
	MinRebootDiscoveryMFTVersion = "4.36.0-95"
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
	if m == nil {
		return fmt.Errorf("reboot method is nil")
	}
	if err := checkRebootSequenceCount(optCtx, m); err != nil {
		return err
	}

	// Keep track of the current boot ID on Client side.
	currentRebootID, err := getCurrentRebootID()
	if err != nil {
		return fmt.Errorf("failed to read boot ID file: %w", err)
	}
	optCtx.Status.InitialBootID = ptr.To(currentRebootID)

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
		optCtx.Status.InitialBootID = nil
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

// getMSTDevices lists all device paths under mstDevicesPath.
func (h *HandleReboot) getMSTDevices() ([]string, error) {
	mstPath := h.mstDevicesPath
	if mstPath == "" {
		mstPath = defaultMstDevicesPath
	}
	devices, err := filepath.Glob(filepath.Join(mstPath, "*"))
	if err != nil {
		return nil, fmt.Errorf("failed to list MST devices: %w", err)
	}
	if len(devices) == 0 {
		return nil, fmt.Errorf("no MST devices found in %s", mstPath)
	}
	sort.Strings(devices)
	return devices, nil
}

func (h *HandleReboot) execFirmwareReset(execCtx context.Context, optCtx *operations.Context) error {
	devices, err := h.getMSTDevices()
	if err != nil {
		return err
	}

	optCtx.Status.RebootMethod = ptr.To(provisioningv1.RebootMethodFirmwareReset)

	// Update status until success.
	optCtx.UpdateStatusUntilSuccess(execCtx)

	if h.runBash == nil {
		h.runBash = bash.Run
	}
	for _, device := range devices {
		cmd := fmt.Sprintf("mlxfwreset -d %s -y reset", device)
		_, stderr, err := h.runBash(cmd)
		if err != nil {
			return fmt.Errorf("%s: %w (stderr: %s)", cmd, err, stderr.String())
		}
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
	if !optCtx.RebootMethodDiscovery {
		return getRebootMethodBootID(optCtx)
	}
	return h.getRebootMethodDeviceQuery(optCtx)
}

// getRebootMethodBootID is used when RebootMethodDiscovery is false (Boot-ID based).
func getRebootMethodBootID(optCtx *operations.Context) (*provisioningv1.RebootMethodType, error) {
	currentRebootID, err := getCurrentRebootID()
	if err != nil {
		return nil, fmt.Errorf("failed to read boot ID file: %w", err)
	}

	if hasBeenBooted(optCtx.LatestDPU, currentRebootID) {
		klog.Infof("Host has already been booted, no reboot action")
		return ptr.To(provisioningv1.RebootMethodNoAction), nil
	}
	return ptr.To(provisioningv1.RebootMethodSystemLevelReset), nil
}

// mlxfwresetStatusJSON is the subset of `mlxfwreset -d <mst_device> s --json` output we need for reboot decisions.
// Other fields (e.g. reasons) remain in the raw JSON string passed to the discovery condition Message.
type mlxfwresetStatusJSON struct {
	ResetNeeded *bool `json:"reset_needed"`
}

// getRebootMethodDeviceQuery is used when RebootMethodDiscovery is true (Device Query based).
func (h *HandleReboot) getRebootMethodDeviceQuery(optCtx *operations.Context) (*provisioningv1.RebootMethodType, error) {
	// Mark device-query mode early so status reflects it even if listing devices or mlxfwreset fails later.
	// Reason stays NoAction with empty Message until a device reports reset_needed, then we update to SystemLevelReset + JSON.
	setRebootMethodDiscoveryCondition(optCtx, provisioningv1.RebootMethodNoAction, "")

	devices, err := h.getMSTDevices()
	if err != nil {
		return nil, err
	}
	if h.runBash == nil {
		h.runBash = bash.Run
	}
	for _, device := range devices {
		cmd := fmt.Sprintf("mlxfwreset -d %s s --json", device)
		stdout, stderr, err := h.runBash(cmd)
		if err != nil {
			return nil, fmt.Errorf("%s: %w (stderr: %s)", cmd, err, stderr.String())
		}
		raw := strings.TrimSpace(stdout.String())
		if raw == "" {
			raw = strings.TrimSpace(stderr.String())
		}
		var out mlxfwresetStatusJSON
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			return nil, fmt.Errorf("mlxfwreset status for %s: parse JSON: %w", device, err)
		}
		if ptr.Deref(out.ResetNeeded, false) {
			setRebootMethodDiscoveryCondition(optCtx, provisioningv1.RebootMethodSystemLevelReset, raw)
			klog.Infof("MST device %s requires reset, using SystemLevelReset", device)
			return ptr.To(provisioningv1.RebootMethodSystemLevelReset), nil
		}
	}
	klog.Infof("No MST device requires reset, using NoAction")
	return ptr.To(provisioningv1.RebootMethodNoAction), nil
}

// checkRebootSequenceCount enforces maxRebootSequenceCount for non-NoAction RebootMethod
func checkRebootSequenceCount(optCtx *operations.Context, method *provisioningv1.RebootMethodType) error {
	var prev int32
	if optCtx.LatestDPU != nil && optCtx.LatestDPU.Status.AgentStatus != nil &&
		optCtx.LatestDPU.Status.AgentStatus.RebootSequenceCount != nil {
		prev = *optCtx.LatestDPU.Status.AgentStatus.RebootSequenceCount
	}
	if *method == provisioningv1.RebootMethodNoAction {
		optCtx.Status.RebootSequenceCount = ptr.To(int32(0))
		return nil
	}
	if prev >= maxRebootSequenceCount {
		return fmt.Errorf("rebootSequenceCount limit exceeded (%d >= %d) without an intervening NoAction RebootMethod; refusing further host reboot",
			prev, maxRebootSequenceCount)
	}
	optCtx.Status.RebootSequenceCount = ptr.To(prev + 1)
	return nil
}

// setRebootMethodDiscoveryCondition sets or updates the device-query discovery condition.
func setRebootMethodDiscoveryCondition(optCtx *operations.Context, method provisioningv1.RebootMethodType, msg string) {
	if len(msg) > maxConditionMessageLen {
		msg = msg[:maxConditionMessageLen-1] + "…"
	}
	meta.SetStatusCondition(&optCtx.Status.Conditions, metav1.Condition{
		Type:               cutil.AgentCondRebootMethodDiscovery,
		Status:             metav1.ConditionTrue,
		Reason:             string(method),
		Message:            msg,
		LastTransitionTime: metav1.Now(),
	})
}

func getCurrentRebootID() (string, error) {
	currentRebootID, err := os.ReadFile(bootIDFile)
	if err != nil {
		return "", fmt.Errorf("failed to read boot ID file: %w", err)
	}
	return strings.TrimSpace(string(currentRebootID)), nil
}

func hasBeenBooted(dpu *provisioningv1.DPU, currentRebootID string) bool {
	// Legacy flow: compare stored InitialBootID to current host boot_id.
	return dpu != nil &&
		dpu.Status.AgentStatus != nil &&
		dpu.Status.AgentStatus.InitialBootID != nil &&
		*dpu.Status.AgentStatus.InitialBootID != currentRebootID
}

// ResolveRebootMethodDiscovery runs mlxfwreset and mlxconfig version checks.
// It returns true when both report MFT at least MinRebootDiscoveryMFTVersion.
func ResolveRebootMethodDiscovery(run func(string) (bytes.Buffer, bytes.Buffer, error)) bool {
	return resolveRebootMethodDiscovery(run)
}

func resolveRebootMethodDiscovery(run func(string) (bytes.Buffer, bytes.Buffer, error)) bool {
	minVer, err := semver.NewVersion(MinRebootDiscoveryMFTVersion)
	if err != nil {
		klog.Errorf("invalid MinRebootDiscoveryMFTVersion constant %q: %v", MinRebootDiscoveryMFTVersion, err)
		return false
	}

	fwOut, err := getMlxfwresetVersionOutputForDiscovery(run)
	if err != nil {
		klog.Infof("RebootMethodDiscovery=false: %v", err)
		return false
	}
	fwVer, err := parseMlxfwresetVersionOutput(fwOut)
	if err != nil {
		klog.Infof("RebootMethodDiscovery=false: mlxfwreset: %v", err)
		return false
	}
	if !mftVersionMeetsMinimum(fwVer, minVer) {
		klog.Infof("RebootMethodDiscovery=false: mlxfwreset version %s is below required %s", fwVer, minVer)
		return false
	}

	cfgOut, err := getMlxconfigVersionOutputForDiscovery(run)
	if err != nil {
		klog.Infof("RebootMethodDiscovery=false: %v", err)
		return false
	}
	cfgVer, err := parseMlxconfigVersionOutput(cfgOut)
	if err != nil {
		klog.Infof("RebootMethodDiscovery=false: mlxconfig: %v", err)
		return false
	}
	if !mftVersionMeetsMinimum(cfgVer, minVer) {
		klog.Infof("RebootMethodDiscovery=false: mlxconfig version %s is below required %s (must be >= %s like mlxfwreset)", cfgVer, minVer, minVer)
		return false
	}

	klog.Infof("RebootMethodDiscovery=true: mlxfwreset=%s mlxconfig=%s (min %s)", fwVer, cfgVer, minVer)
	return true
}

func getMlxfwresetVersionOutputForDiscovery(run func(string) (bytes.Buffer, bytes.Buffer, error)) (string, error) {
	const cmd = "mlxfwreset --version"
	stdout, stderr, err := run(cmd)
	combined := strings.TrimSpace(stdout.String() + stderr.String())
	if err != nil {
		return "", fmt.Errorf("mlxfwreset version output: %s: %w", cmd, err)
	}
	return combined, nil
}

func getMlxconfigVersionOutputForDiscovery(run func(string) (bytes.Buffer, bytes.Buffer, error)) (string, error) {
	const cmd = "mlxconfig --version"
	stdout, stderr, err := run(cmd)
	combined := strings.TrimSpace(stdout.String() + stderr.String())
	if err != nil {
		return "", fmt.Errorf("mlxconfig version output: %s: %w", cmd, err)
	}
	return combined, nil
}

func parseMlxfwresetVersionOutput(output string) (*semver.Version, error) {
	const tag = "mft"
	lower := strings.ToLower(output)
	search := 0
	for search < len(lower) {
		i := strings.Index(lower[search:], tag)
		if i < 0 {
			break
		}
		i += search
		if i > 0 {
			prev := lower[i-1]
			if (prev >= 'a' && prev <= 'z') || (prev >= '0' && prev <= '9') {
				search = i + len(tag)
				continue
			}
		}
		j := i + len(tag)
		for j < len(output) && (output[j] == ' ' || output[j] == '\t' || output[j] == ',' || output[j] == ':') {
			j++
		}
		rest := output[j:]
		for _, field := range strings.Fields(rest) {
			field = strings.TrimRight(field, ".,;")
			ver, err := semver.NewVersion(field)
			if err == nil && strings.Count(field, ".") >= 2 {
				return ver, nil
			}
		}
		search = i + len(tag)
	}
	return nil, fmt.Errorf("failed to extract mlxfwreset MFT version from output: %s", strings.TrimSpace(output))
}

func parseMlxconfigVersionOutput(output string) (*semver.Version, error) {
	for _, field := range strings.Fields(output) {
		field = strings.TrimRight(field, ".,;")
		ver, err := semver.NewVersion(field)
		if err == nil && strings.Count(field, ".") >= 2 {
			return ver, nil
		}
	}
	return nil, fmt.Errorf("failed to extract version from mlxconfig output: %s", strings.TrimSpace(output))
}

func mftVersionMeetsMinimum(ver *semver.Version, min *semver.Version) bool {
	return !ver.LessThan(min)
}
