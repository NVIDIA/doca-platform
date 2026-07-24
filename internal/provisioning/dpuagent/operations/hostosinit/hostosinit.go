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

package hostosinit

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/nvconfig"
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"
	pciutil "github.com/nvidia/doca-platform/internal/provisioning/utils/pci"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	condReleaseHostOSInit = "ReleaseHostOSInit"

	mlxregRegisterName = "HOST_OS_INIT_CTRL"
	mlxregClearField   = "delay_host_os_init_clr"
	mlxregDelayField   = "delay_host_os_init"
	mlxregModeField    = "host_os_init_mode"
)

type ReleaseHostOSInit struct {
	runBash func(cmd string) (bytes.Buffer, bytes.Buffer, error)
}

func (r *ReleaseHostOSInit) Name() string {
	return "Release Host OS Init"
}

func (r *ReleaseHostOSInit) ConditionType() string {
	return condReleaseHostOSInit
}

func (r *ReleaseHostOSInit) ShouldSkip(_ *operations.Context) bool {
	return false
}

func (r *ReleaseHostOSInit) ShouldUpdateStatusBeforeContinue(_ *operations.Context) bool {
	return true
}

func (r *ReleaseHostOSInit) Execute(execCtx context.Context, optCtx *operations.Context) error {
	if r.runBash == nil {
		r.runBash = bash.Run
	}
	optCtx.ClearHostOSInit = true
	optCtx.Status.HostOSInit = nil
	if err := r.persistStatus(execCtx, optCtx); err != nil {
		return err
	}
	optCtx.ClearHostOSInit = false

	resolved, err := nvconfig.EnsureResolved(optCtx)
	if err != nil {
		return err
	}
	if !resolved.HostOSInitRequired {
		return r.patchSkipped(execCtx, optCtx, "ReleaseNotRequired", "DELAY_HOST_OS_INIT is not set to ENABLE_USER (0x3) in flavor nvconfig")
	}

	if mismatch := pendingHostOSInitMismatch(optCtx, resolved.HostOSInitPCIs); mismatch != "" {
		return fmt.Errorf("host OS init release blocked: %s", mismatch)
	}

	states := make([]hostOSInitRegisterState, len(resolved.HostOSInitPCIs))
	allReleased := true
	for i, pci := range resolved.HostOSInitPCIs {
		state, err := r.readHoldRegister(pci)
		if err != nil {
			return err
		}
		states[i] = state
		allReleased = allReleased && state == hostOSInitReleased
	}
	if allReleased {
		return r.patchSucceeded(execCtx, optCtx, effectiveReleaseAfter(optCtx))
	}

	gate := effectiveReleaseAfter(optCtx)
	if err := r.ensureGateReady(execCtx, optCtx, gate); err != nil {
		return err
	}
	for i, pci := range resolved.HostOSInitPCIs {
		if states[i] == hostOSInitReleased {
			continue
		}
		if err := r.setHoldRegister(pci); err != nil {
			return err
		}
		state, err := r.readHoldRegister(pci)
		if err != nil {
			return err
		}
		if state != hostOSInitReleased {
			return fmt.Errorf("host OS init remains held on PCI %s after mlxreg release", pci)
		}
	}
	return r.patchSucceeded(execCtx, optCtx, gate)
}

func effectiveReleaseAfter(optCtx *operations.Context) *provisioningv1.HostOSInitReleaseAfter {
	if optCtx.DPUFlavor.Spec.HostOSInit != nil && optCtx.DPUFlavor.Spec.HostOSInit.ReleaseAfter != nil {
		return optCtx.DPUFlavor.Spec.HostOSInit.ReleaseAfter.DeepCopy()
	}
	return &provisioningv1.HostOSInitReleaseAfter{
		DPUServiceCriticalPodsReady: &provisioningv1.HostOSInitGate{},
	}
}

type hostOSInitRegisterState uint8

const (
	hostOSInitHeld hostOSInitRegisterState = iota
	hostOSInitReleased
)

func (r *ReleaseHostOSInit) readHoldRegister(pci string) (hostOSInitRegisterState, error) {
	out, err := r.mlxregGet(pci)
	if err != nil {
		return 0, err
	}
	values := map[string]uint64{}
	for _, field := range []string{mlxregClearField, mlxregDelayField, mlxregModeField} {
		value, ok := parseMlxregField(out, field)
		if !ok {
			return 0, fmt.Errorf("mlxreg --get output for PCI %s missing %s", pci, field)
		}
		values[field], err = parseMlxregUint(value)
		if err != nil {
			return 0, fmt.Errorf("unexpected %s value %q from mlxreg --get for PCI %s", field, value, pci)
		}
	}
	clear, delay, mode := values[mlxregClearField], values[mlxregDelayField], values[mlxregModeField]
	if clear != 0 || mode != 3 || delay > 1 {
		return 0, fmt.Errorf("unexpected HOST_OS_INIT_CTRL state on PCI %s: clear=%d delay=%d mode=%d", pci, clear, delay, mode)
	}
	if delay == 0 {
		return hostOSInitReleased, nil
	}
	return hostOSInitHeld, nil
}

func pendingHostOSInitMismatch(optCtx *operations.Context, pcis []string) string {
	pending := optCtx.Status.LastObservedPendingNVConfig
	if pending == nil || pending.BootID == "" || pending.BootID != optCtx.CurrentBootID {
		return ""
	}
	targets := make(map[string]struct{}, len(pcis))
	for _, pci := range pcis {
		targets[pciutil.NormalizeAddress(pci)] = struct{}{}
	}
	for _, device := range pending.Devices {
		if _, ok := targets[pciutil.NormalizeAddress(device.Device)]; !ok {
			continue
		}
		for _, entry := range device.Entries {
			if !strings.EqualFold(strings.TrimSpace(entry.Name), "DELAY_HOST_OS_INIT") {
				continue
			}
			current, currentOK := parseObservedNVConfigMode(entry.Current)
			next, nextOK := parseObservedNVConfigMode(entry.NextBoot)
			if currentOK && nextOK && current < 3 && next == 3 {
				return fmt.Sprintf("DELAY_HOST_OS_INIT did not activate on PCI %s after reboot (current=%s, next=%s)",
					device.Device, entry.Current, entry.NextBoot)
			}
		}
	}
	return ""
}

func parseObservedNVConfigMode(value string) (uint64, bool) {
	value = strings.ToUpper(strings.TrimSpace(value))
	switch value {
	case "DEVICE_DEFAULT(0)":
		return 0, true
	case "ENABLE_USER(3)":
		return 3, true
	}
	mode, err := parseMlxregUint(value)
	return mode, err == nil && mode <= 3
}

func (r *ReleaseHostOSInit) mlxregGet(pci string) (string, error) {
	cmd := fmt.Sprintf("mlxreg -d %s --reg_name %s --get", pci, mlxregRegisterName)
	stdout, stderr, err := r.runBash(cmd)
	combined := strings.TrimSpace(stdout.String() + "\n" + stderr.String())
	if err != nil {
		return "", fmt.Errorf("%s: %w (output: %s)", cmd, err, combined)
	}
	return combined, nil
}

// parseMlxregField reads a field from mlxreg --get table output ("name | 0x00000000")
// or the compact "name=value" form.
func parseMlxregField(output, field string) (string, bool) {
	target := strings.ToLower(strings.TrimSpace(field))
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "=") {
			continue
		}
		var name, value string
		var ok bool
		switch {
		case strings.Contains(line, "|"):
			name, value, ok = strings.Cut(line, "|")
		default:
			name, value, ok = strings.Cut(line, "=")
		}
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(name), target) {
			return strings.TrimSpace(value), true
		}
	}
	return "", false
}

func parseMlxregUint(value string) (uint64, error) {
	v := strings.TrimSpace(strings.ToLower(value))
	base := 10
	if strings.HasPrefix(v, "0x") {
		v = strings.TrimPrefix(v, "0x")
		base = 16
	}
	return strconv.ParseUint(v, base, 64)
}

func (r *ReleaseHostOSInit) ensureGateReady(ctx context.Context, optCtx *operations.Context, gate *provisioningv1.HostOSInitReleaseAfter) error {
	// Do not use cached LatestDPU: it is filled once early in bootstrap and is stale for
	// operationalConditions. Use a local Get so we do not mutate shared optCtx.LatestDPU.
	dpu := optCtx.LatestDPU
	if optCtx.Client != nil {
		fresh := &provisioningv1.DPU{}
		key := client.ObjectKey{Namespace: optCtx.Options.DPUNamespace, Name: optCtx.Options.DPUName}
		if err := optCtx.Client.Get(ctx, key, fresh); err != nil {
			return err
		}
		dpu = fresh
	}
	gateType := gateConditionType(gate)
	if dpu == nil || !gateConditionTrue(dpu, gateType) {
		return fmt.Errorf("waiting for %s", gateType)
	}
	return nil
}

func gateConditionType(gate *provisioningv1.HostOSInitReleaseAfter) provisioningv1.DPUOperationalConditionType {
	if gate != nil && gate.OperationalReady != nil {
		return provisioningv1.DPUOperationalCondReady
	}
	return provisioningv1.DPUOperationalCondDPUServiceCriticalPodsReady
}

func gateConditionTrue(dpu *provisioningv1.DPU, gateType provisioningv1.DPUOperationalConditionType) bool {
	cond := meta.FindStatusCondition(dpu.Status.OperationalConditions, string(gateType))
	return cond != nil && cond.Status == metav1.ConditionTrue
}

func (r *ReleaseHostOSInit) setHoldRegister(pci string) error {
	cmd := fmt.Sprintf("mlxreg -d %s --reg_name %s --yes --set %q", pci, mlxregRegisterName, mlxregClearField+"=0x1")
	_, stderr, err := r.runBash(cmd)
	if err == nil {
		return nil
	}
	return fmt.Errorf("failed to release host OS init: mlxreg command failed: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
}

func (r *ReleaseHostOSInit) persistStatus(ctx context.Context, optCtx *operations.Context) error {
	if optCtx.UpdateStatusUntilSuccess == nil {
		return nil
	}
	return optCtx.UpdateStatusUntilSuccess(ctx)
}

func (r *ReleaseHostOSInit) patchSkipped(ctx context.Context, optCtx *operations.Context, reason, message string) error {
	optCtx.Status.HostOSInit = &provisioningv1.HostOSInitStatus{
		Skipped: &provisioningv1.HostOSInitSkipped{Reason: &reason, Message: &message},
	}
	hostutil.NewCondition(condReleaseHostOSInit).Success(message).Set(&optCtx.Status.Conditions)
	return r.persistStatus(ctx, optCtx)
}

func (r *ReleaseHostOSInit) patchSucceeded(ctx context.Context, optCtx *operations.Context, gate *provisioningv1.HostOSInitReleaseAfter) error {
	optCtx.Status.HostOSInit = &provisioningv1.HostOSInitStatus{
		Succeeded: &provisioningv1.HostOSInitSucceeded{ReleaseAfter: gate.DeepCopy()},
	}
	hostutil.NewCondition(condReleaseHostOSInit).Success("").Set(&optCtx.Status.Conditions)
	return r.persistStatus(ctx, optCtx)
}
