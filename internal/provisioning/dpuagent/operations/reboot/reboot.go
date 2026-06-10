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
	"sort"
	"strconv"
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
	defaultPostResetBlockDuration = 10 * time.Minute

	// maxRebootSequenceCount caps RebootSequenceCount (non-NoAction RebootMethod runs in a row)
	// before the agent refuses further host reboot until NoAction resets the counter.
	maxRebootSequenceCount int32 = 5

	// maxConditionMessageLen bounds Condition.Message (mlxfwreset JSON can be large).
	maxConditionMessageLen = 8192

	// MinRebootDiscoveryMFTVersion is the minimum MFT (mlxconfig / mlxfwreset) version
	// required to use device-query path (RebootMethodDiscovery=true).
	MinRebootDiscoveryMFTVersion = "4.36.0-95"

	// systemLevelResetCommandRequiredText is the canonical mlxfwreset `command_required` value that
	// maps to RebootMethodSystemLevelReset (trim + case-insensitive match on the JSON field).
	systemLevelResetCommandRequiredText = "Reboot external host is required"

	// powerCycleCommandRequiredText is the canonical mlxfwreset `command_required` text pattern that
	// maps to RebootMethodPowerCycle when present in the field (trim + case-insensitive substring match).
	powerCycleCommandRequiredText = "power cycle"

	pendingNVConfigReason = "Pending NVCONFIG parameter change"
)

// powerCyclePendingNvconfigNames lists pending NVCONFIG parameter names that require
// RebootMethodPowerCycle (highest merge priority among MST discovery methods).
var powerCyclePendingNvconfigNames = map[string]struct{}{
	"DELAY_HOST_OS_INIT":           {},
	"INT_CPU_AUTO_SHUTDOWN":        {},
	"INTERNAL_CPU_OFFLOAD_ENGINE":  {},
	"INTERNAL_CPU_IB_VPORT0":       {},
	"INTERNAL_CPU_ESWITCH_MANAGER": {},
	"INTERNAL_CPU_PAGE_SUPPLIER":   {},
	"INTERNAL_CPU_MODEL":           {},
}

// firmwareResetPerDevice is one command line for execFirmwareReset, in MST glob sort order.
type firmwareResetPerDevice struct {
	DevicePath string
	Cmd        string
}

type HandleReboot struct {
	runBash   bash.RunFunc
	skipBlock bool
	// allowFirmwareReset is set for the duration of getRebootMethodDeviceQuery from LatestDPU annotations
	// (AgentAnnotationAllowFirmwareResetReboot). When false or h is nil, rebootMethodFromMlxfwresetStatus does not select FirmwareReset.
	allowFirmwareReset bool
	// perDeviceFirmwareResetCmds is the ordered mlxfwreset command line per MST device from discovery
	// when the final reboot method is FirmwareReset. Cleared when consumed by execFirmwareReset.
	perDeviceFirmwareResetCmds []firmwareResetPerDevice
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
	if isHostlessDPU(optCtx) {
		optCtx.Status.RebootMethod = ptr.To(provisioningv1.RebootMethodHostlessDPUReboot)
		return nil
	}

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
	optCtx.Status.InitialBootID = ptr.To(optCtx.CurrentBootID)

	optCtx.Status.RebootMethod = nil
	switch *m {
	case provisioningv1.RebootMethodPowerCycle:
		return h.execPowerCycle(execCtx, optCtx)
	case provisioningv1.RebootMethodSystemReboot:
		return h.execSystemReboot(optCtx)
	case provisioningv1.RebootMethodSystemLevelReset:
		return h.execSystemLevelReset(execCtx, optCtx)
	case provisioningv1.RebootMethodFirmwareReset:
		return h.execFirmwareReset(execCtx, optCtx)
	case provisioningv1.RebootMethodDPUWarmReboot:
		return h.execWarmReboot(execCtx, optCtx)
	case provisioningv1.RebootMethodHostlessDPUReboot:
		optCtx.Status.RebootMethod = ptr.To(provisioningv1.RebootMethodHostlessDPUReboot)
		return nil
	case provisioningv1.RebootMethodNoAction:
		optCtx.Status.InitialBootID = nil
		optCtx.Status.RebootMethod = ptr.To(provisioningv1.RebootMethodNoAction)
		return nil
	}
	return fmt.Errorf("unsupported reboot method: %s", *m)
}

func (h *HandleReboot) execPowerCycle(execCtx context.Context, optCtx *operations.Context) error {
	optCtx.Status.RebootMethod = ptr.To(provisioningv1.RebootMethodPowerCycle)
	optCtx.UpdateStatusUntilSuccess(execCtx)
	return h.blockUntilReset()
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

func isHostlessDPU(optCtx *operations.Context) bool {
	return optCtx != nil && optCtx.LatestDPU != nil && optCtx.LatestDPU.Status.Hostless
}

// getMSTDevices returns the selected target devices for MFT commands.
func (h *HandleReboot) getMSTDevices(optCtx *operations.Context) ([]string, error) {
	ports, err := optCtx.NSPorts()
	if err != nil {
		return nil, err
	}
	var devices []string
	for _, p := range ports {
		if p.MSTDevice != "" {
			devices = append(devices, p.MSTDevice)
		}
	}
	return devices, nil
}

func (h *HandleReboot) execFirmwareReset(execCtx context.Context, optCtx *operations.Context) error {
	if h.runBash == nil {
		h.runBash = bash.Run
	}
	perDeviceCmds := h.perDeviceFirmwareResetCmds
	h.perDeviceFirmwareResetCmds = nil
	if len(perDeviceCmds) == 0 {
		return fmt.Errorf("per-device firmware reset commands are empty; expected device-query discovery to populate them")
	}

	optCtx.Status.RebootMethod = ptr.To(provisioningv1.RebootMethodFirmwareReset)
	optCtx.UpdateStatusUntilSuccess(execCtx)

	for _, e := range perDeviceCmds {
		_, stderr, err := h.runBash(e.Cmd)
		if err != nil {
			return fmt.Errorf("%s: %w (stderr: %s)", e.Cmd, err, stderr.String())
		}
	}
	return h.blockUntilReset()
}

// execWarmReboot reboots the DPU OS.
func (h *HandleReboot) execWarmReboot(execCtx context.Context, optCtx *operations.Context) error {
	optCtx.Status.RebootMethod = ptr.To(provisioningv1.RebootMethodDPUWarmReboot)
	optCtx.UpdateStatusUntilSuccess(execCtx)

	if h.runBash == nil {
		h.runBash = bash.Run
	}
	klog.Infof("Rebooting DPU OS in %d seconds", shutdownDelayInSeconds)
	cmd := fmt.Sprintf("sleep %d && reboot", shutdownDelayInSeconds)
	_, stderr, err := h.runBash(cmd)
	if err != nil {
		return fmt.Errorf("failed to reboot host for grub changes: %w, stderr: %s", err, stderr.String())
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
	if hasBeenBooted(optCtx.LatestDPU, optCtx.CurrentBootID) {
		if optCtx.GrubConfigChanged {
			return ptr.To(provisioningv1.RebootMethodDPUWarmReboot), nil
		}
		klog.Infof("Host has already been booted, no reboot action")
		return ptr.To(provisioningv1.RebootMethodNoAction), nil
	}
	return ptr.To(provisioningv1.RebootMethodSystemLevelReset), nil
}

type pendingParamList []provisioningv1.PendingNVConfigEntry

func (p *pendingParamList) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '[' {
		*p = pendingParamList{}
		return nil
	}
	out := []provisioningv1.PendingNVConfigEntry{}
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	*p = pendingParamList(out)
	return nil
}

// mlxfwresetStatusJSON is the subset of `mlxfwreset -d <mst_device> s --json` output we need for reboot decisions.
// Other fields remain in the raw JSON string passed to the discovery condition Message.
type mlxfwresetStatusJSON struct {
	ResetNeeded *bool `json:"reset_needed"`
	// PendingNvconfigParameters may be an array of provisioningv1.PendingNVConfigEntry
	// or a string (e.g. "N/A"); non-array value is parsed as an empty list.
	PendingNvconfigParameters pendingParamList `json:"pending_nvconfig_parameters"`
	CommandRequired           string           `json:"command_required"`
	Reasons                   []string         `json:"reasons"`
}

// checkRebootMethodPowerCycle reports whether a power-cycle reboot is indicated by command_required
// or, as a fallback, by pending_nvconfig_parameters listing any name in powerCyclePendingNvconfigNames.
func checkRebootMethodPowerCycle(_ *HandleReboot, out *mlxfwresetStatusJSON) bool {
	cmd := strings.TrimSpace(out.CommandRequired)
	if cmd != "" && strings.Contains(strings.ToLower(cmd), powerCycleCommandRequiredText) {
		return true
	}
	// Workaround should be removed once [1] is fixed.
	// [1] Feature Request #4846233: [DPF][BF4] Server Reboot Reduction - Mark specific TLVs for blocking FW reset
	if len(out.PendingNvconfigParameters) == 0 {
		return false
	}
	// Detect Power Cycle reboot request using pending_nvconfig_parameters predefined names.
	for _, e := range out.PendingNvconfigParameters {
		if _, ok := powerCyclePendingNvconfigNames[e.Name]; ok {
			return true
		}
	}
	return false
}

// checkRebootMethodSystemLevelReset reports whether command_required has
// predefined text for SystemLevelReset.
func checkRebootMethodSystemLevelReset(_ *HandleReboot, out *mlxfwresetStatusJSON) bool {
	cmd := strings.TrimSpace(out.CommandRequired)
	return cmd != "" && strings.EqualFold(cmd, systemLevelResetCommandRequiredText)
}

// checkRebootMethodFirmwareReset reports non-empty command_required as the firmware-reset path.
// When h is non-nil, appends CommandRequired to h.perDeviceFirmwareResetCmds.
func checkRebootMethodFirmwareReset(h *HandleReboot, devicePath string, out *mlxfwresetStatusJSON) bool {
	if out.CommandRequired == "" {
		return false
	}
	if h != nil {
		h.perDeviceFirmwareResetCmds = append(h.perDeviceFirmwareResetCmds, firmwareResetPerDevice{
			DevicePath: devicePath,
			Cmd:        out.CommandRequired,
		})
	}
	return true
}

// rebootMethodTakesPrecedenceOver reports whether a should replace the merged method b when both come from MST discovery.
func rebootMethodTakesPrecedenceOver(a, b provisioningv1.RebootMethodType) bool {
	return cutil.GetRebootMethodPriority(a) < cutil.GetRebootMethodPriority(b)
}

// rebootMethodFromMlxfwresetStatus maps one device's JSON to a reboot method.
// Use checking reboot method according to the priority in rebootMethodMergePriority.
func rebootMethodFromMlxfwresetStatus(h *HandleReboot, device string, out *mlxfwresetStatusJSON) provisioningv1.RebootMethodType {
	if checkRebootMethodPowerCycle(nil, out) {
		return provisioningv1.RebootMethodPowerCycle
	}
	if checkRebootMethodSystemLevelReset(nil, out) {
		return provisioningv1.RebootMethodSystemLevelReset
	}
	if h != nil && h.allowFirmwareReset && checkRebootMethodFirmwareReset(h, device, out) {
		return provisioningv1.RebootMethodFirmwareReset
	}
	// Fallback to SystemLevelReset in case 'reset_needed' is true but no other reboot method is detected.
	// Full tool output is available on the AgentCondRebootMethodDiscovery condition Message.
	klog.Infof("MST device %s: no reboot method matched from tool output; falling back to SystemLevelReset. See AgentCondRebootMethodDiscovery condition Message for full tool output.", device)
	return provisioningv1.RebootMethodSystemLevelReset
}

func lastPendingState(dpu *provisioningv1.DPU) *provisioningv1.PendingNVConfigState {
	if dpu == nil || dpu.Status.AgentStatus == nil {
		return nil
	}
	return dpu.Status.AgentStatus.LastObservedPendingNVConfig
}

func recordPending(optCtx *operations.Context, device string, entries pendingParamList) {
	if optCtx.Status.LastObservedPendingNVConfig == nil {
		optCtx.Status.LastObservedPendingNVConfig = &provisioningv1.PendingNVConfigState{}
	}
	optCtx.Status.LastObservedPendingNVConfig.BootID = optCtx.CurrentBootID
	optCtx.Status.LastObservedPendingNVConfig.Devices = append(optCtx.Status.LastObservedPendingNVConfig.Devices, provisioningv1.PendingNVConfigDevice{
		Device:  device,
		Entries: []provisioningv1.PendingNVConfigEntry(entries),
	})
}

// removeForeverPending filters parameters that mlxfwreset keeps reporting as
// pending across boots and ignores them when they are the only reset reason.
// TODO: Remove this workaround once the MFT tool is fixed.
func removeForeverPending(
	optCtx *operations.Context,
	device string,
	mlxfwresetOutput mlxfwresetStatusJSON,
) (mlxfwresetStatusJSON, bool) {
	curPending := mlxfwresetOutput.PendingNvconfigParameters
	if len(curPending) == 0 {
		return mlxfwresetOutput, false
	}

	lastObserved := lastPendingState(optCtx.LatestDPU)
	if lastObserved == nil {
		return mlxfwresetOutput, false
	}
	// This usually means provisioning already finished and dpu-agent restarted
	// while the DPU OS did not reboot. In that case we must avoid shutting
	// down the DPU OS again. This can also happen if the agent flow is interrupted
	// unexpectedly during provisioning, but that path is considered extremely
	// unlikely and is not handled separately. If it happens, reprovisioning resolves the issue.
	if lastObserved.BootID == optCtx.CurrentBootID {
		return mlxfwresetOutput, true
	}
	lastPending := []provisioningv1.PendingNVConfigEntry{}
	for _, d := range lastObserved.Devices {
		if d.Device == device {
			lastPending = d.Entries
			break
		}
	}
	if len(lastPending) == 0 {
		return mlxfwresetOutput, false
	}

	// filter out pending parameters that are the same as the last pending parameters
	effectivePending := []provisioningv1.PendingNVConfigEntry{}
	removed := []string{}
	for _, cur := range curPending {
		found := false
		for _, lastP := range lastPending {
			if lastP.Name == cur.Name && lastP.Current == cur.Current {
				found = true
				removed = append(removed, fmt.Sprintf("%s(default=%s,current=%s,next=%s)", cur.Name, cur.Default, cur.Current, cur.NextBoot))
				break
			}
		}
		if !found {
			effectivePending = append(effectivePending, cur)
		}
	}
	effective := mlxfwresetOutput
	effective.PendingNvconfigParameters = pendingParamList(effectivePending)
	hasOtherReasons := false
	for _, reason := range mlxfwresetOutput.Reasons {
		if !strings.EqualFold(strings.TrimSpace(reason), pendingNVConfigReason) {
			hasOtherReasons = true
			break
		}
	}
	shouldIgnore := len(effectivePending) == 0 && !hasOtherReasons

	// format the message for the condition
	sort.Strings(removed)
	msg := fmt.Sprintf(
		"device=%s pending NVCONFIG params did not take effect after reboot: [%s]",
		device,
		strings.Join(removed, ","),
	)
	if shouldIgnore {
		msg += "; reset ignored because no other reset reasons remain."
	} else {
		msg += "; reset still required because other reset reasons remain."
	}
	optCtx.CondMessage += msg
	return effective, shouldIgnore
}

// agentAnnotationAllowsFirmwareResetReboot reports whether AgentAnnotationAllowFirmwareResetReboot is true on LatestDPU.
// Default off when omitted or not "true".
func agentAnnotationAllowsFirmwareResetReboot(optCtx *operations.Context) bool {
	if optCtx == nil || optCtx.LatestDPU == nil {
		return false
	}
	v, ok := optCtx.LatestDPU.Annotations[cutil.AgentAnnotationAllowFirmwareResetReboot]
	if !ok {
		return false
	}
	allowed, err := strconv.ParseBool(strings.TrimSpace(v))
	if err != nil {
		return false
	}
	return allowed
}

// getRebootMethodDeviceQuery is used when RebootMethodDiscovery is true (Device Query based).
func (h *HandleReboot) getRebootMethodDeviceQuery(optCtx *operations.Context) (*provisioningv1.RebootMethodType, error) {
	// Mark device-query mode early so status reflects it even if listing devices or mlxfwreset fails later.
	// Reason stays NoAction with empty Message until a device reports reset_needed, then we update to the derived method + JSON.
	setRebootMethodDiscoveryCondition(optCtx, provisioningv1.RebootMethodNoAction, "")
	h.perDeviceFirmwareResetCmds = nil
	h.allowFirmwareReset = agentAnnotationAllowsFirmwareResetReboot(optCtx)
	defer func() { h.allowFirmwareReset = false }()

	devices, err := h.getMSTDevices(optCtx)
	if err != nil {
		return nil, err
	}
	if h.runBash == nil {
		h.runBash = bash.Run
	}

	finalRebootMethod := provisioningv1.RebootMethodNoAction
	rawParts := make([]string, 0, len(devices))
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
		recordPending(optCtx, device, out.PendingNvconfigParameters)
		if !ptr.Deref(out.ResetNeeded, false) {
			continue
		}
		// Some NVConfig parameters depend on other parameters. When those
		// dependencies are not satisfied, the parameter values still do not change
		// after reboot, so mlxfwreset keeps reporting them as pending. As a
		// workaround, removeForeverPending filters parameters that remained
		// unchanged across boots. Provisioning continues only when all remaining
		// pending_nvconfig_parameters are filtered out and "Pending NVCONFIG
		// parameter change" is the only reason for reset.
		effective, shouldIgnore := removeForeverPending(optCtx, device, out)
		if shouldIgnore {
			klog.Infof("MST device %s: ignoring repeated pending NVCONFIG parameters after boot change", device)
			continue
		}
		rawParts = append(rawParts, raw)
		m := rebootMethodFromMlxfwresetStatus(h, device, &effective)
		if rebootMethodTakesPrecedenceOver(m, finalRebootMethod) {
			finalRebootMethod = m
		}
		klog.Infof("MST device %s requires reboot method %s, (current selected method: %s)", device, m, finalRebootMethod)
	}

	if optCtx.NICFirmwareRebootRequired && rebootMethodTakesPrecedenceOver(provisioningv1.RebootMethodSystemReboot, finalRebootMethod) {
		nicFirmwareRebootMsg := "NIC provisioning requires host reboot, using SystemReboot"
		klog.Info(nicFirmwareRebootMsg)
		rawParts = append(rawParts, nicFirmwareRebootMsg)
		finalRebootMethod = provisioningv1.RebootMethodSystemReboot
	}

	// If a higher-priority method wins (e.g. PowerCycle), drop those cmds—they only apply when finalRebootMethod is FirmwareReset.
	if finalRebootMethod != provisioningv1.RebootMethodFirmwareReset {
		h.perDeviceFirmwareResetCmds = nil
	}

	if finalRebootMethod != provisioningv1.RebootMethodNoAction {
		klog.Infof("MST device-query final reboot method: %s across %d device(s)", finalRebootMethod, len(rawParts))
		setRebootMethodDiscoveryCondition(optCtx, finalRebootMethod, strings.Join(rawParts, "\n---\n"))
		return ptr.To(finalRebootMethod), nil
	}

	if optCtx.GrubConfigChanged {
		klog.Infof("No MST device requires reset but grub config changed, using DPUWarmReboot")
		setRebootMethodDiscoveryCondition(optCtx, provisioningv1.RebootMethodDPUWarmReboot, "No MST device requires reset but grub config changed, using DPUWarmReboot")
		return ptr.To(provisioningv1.RebootMethodDPUWarmReboot), nil
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
func ResolveRebootMethodDiscovery(run bash.RunFunc) bool {
	return resolveRebootMethodDiscovery(run)
}

func resolveRebootMethodDiscovery(run bash.RunFunc) bool {
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

func getMlxfwresetVersionOutputForDiscovery(run bash.RunFunc) (string, error) {
	const cmd = "mlxfwreset --version"
	stdout, stderr, err := run(cmd)
	combined := strings.TrimSpace(stdout.String() + stderr.String())
	if err != nil {
		return "", fmt.Errorf("mlxfwreset version output: %s: %w", cmd, err)
	}
	return combined, nil
}

func getMlxconfigVersionOutputForDiscovery(run bash.RunFunc) (string, error) {
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
