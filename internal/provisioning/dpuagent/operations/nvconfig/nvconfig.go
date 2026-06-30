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

package nvconfig

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"
	pciutil "github.com/nvidia/doca-platform/internal/provisioning/utils/pci"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

const CondNVConfigApplied = "NVConfigApplied"

type nvParam struct {
	name  string
	value string
}

type ConfigureNVConfig struct {
	runBash func(cmd string) (bytes.Buffer, bytes.Buffer, error)
}

func (n *ConfigureNVConfig) Name() string {
	return "NVConfig"
}

func (n *ConfigureNVConfig) ConditionType() string {
	return CondNVConfigApplied
}

func (n *ConfigureNVConfig) ShouldSkip(ctx *operations.Context) bool {
	if ctx.LatestDPU == nil {
		klog.Error("Latest DPU not retrieved, will return error during execution. (this should never happen)")
		return false
	}
	if ctx.LatestDPU.Status.AgentStatus == nil {
		return false
	}
	// Re-run NV config when device-query reboot path is active;
	// do not short-circuit on a prior NVConfigApplied.
	if !ctx.RebootMethodDiscovery {
		cond := meta.FindStatusCondition(ctx.LatestDPU.Status.AgentStatus.Conditions, CondNVConfigApplied)
		if cond != nil && cond.Status == metav1.ConditionTrue {
			klog.Infof("NVConfig already configured, skip")
			return true
		}
	}
	return false
}

func (n *ConfigureNVConfig) ShouldUpdateStatusBeforeContinue(ctx *operations.Context) bool {
	// Must update status before continuing, because the condition is used to check if the NVConfig has been configured.
	return true
}

func (n *ConfigureNVConfig) Execute(execCtx context.Context, optCtx *operations.Context) error {
	if optCtx.LatestDPU == nil {
		klog.Error("Latest DPU not retrieved. This should never happen.")
		return fmt.Errorf("latest DPU not retrieved")
	}

	if n.runBash == nil {
		n.runBash = bash.Run
	}

	// 1.Get PCI -> netdev map.
	pciToNetdev, err := n.pciToNetdevMap(optCtx)
	if err != nil {
		return fmt.Errorf("get devices from devlink: %w", err)
	}
	if len(pciToNetdev) == 0 {
		return fmt.Errorf("no physical ports from devlink (p0/p1)")
	}

	// 2. Build PCI -> NVConfig parameters map.
	nvconfigs := optCtx.DPUFlavor.Spec.NVConfig
	pciToParams := pciToNVConfig(nvconfigs, pciToNetdev)

	// 3. Build a deterministic PCI order and group reset-only devices first.
	// In legacy flow, we reset all devices first, then apply set per device.
	type pciParams struct{ pci, params string }
	ordered := make([]pciParams, 0, len(pciToParams))
	pciKeys := make([]string, 0, len(pciToParams))
	for pci := range pciToParams {
		pciKeys = append(pciKeys, pci)
	}
	sort.Strings(pciKeys)
	for _, pci := range pciKeys {
		params := pciToParams[pci]
		if params == "" {
			ordered = append(ordered, pciParams{pci: pci, params: params})
		}
	}
	for _, pci := range pciKeys {
		params := pciToParams[pci]
		if params != "" {
			ordered = append(ordered, pciParams{pci: pci, params: params})
		}
	}

	// 4. Execute mlxconfig.
	// RebootMethodDiscovery is set at agent start (see dpuagent MFT version probe logs).
	// false (legacy): reset all target PCI devices, then set the full flavor list per PCI.
	// true (device-query): --with_default set only; filterParamsForSet queries mlxconfig before each set.
	if !optCtx.RebootMethodDiscovery {
		// Reset all target PCI devices first, then set per PCI.
		for _, pair := range ordered {
			pci := pair.pci
			if err := n.runMlxconfig(pci, "reset", ""); err != nil {
				return err
			}
		}
		for _, pair := range ordered {
			pci, params := pair.pci, pair.params
			if params != "" {
				klog.Infof("Setting NVConfig params on device %s: %s", pci, params)
				if err := n.runMlxconfig(pci, "set", params); err != nil {
					return err
				}
			}
		}
		return nil
	}
	optCtx.DeferredNVConfigParams = nil
	for _, pair := range ordered {
		pci, params := pair.pci, pair.params
		klog.Infof("Passed NVConfig params on device %s: %s", pci, params)
		if params == "" {
			// Avoid mlxconfig reset (which always forces a device reset). Use --with_default set
			// with a parameter that equals its default (e.g. BOOT_DBG_LOG=0), so the config is
			// unchanged but no reset is triggered when current configuration equal to default.
			if err := n.runMlxconfig(pci, "--with_default set", "BOOT_DBG_LOG=0"); err != nil {
				return err
			}
			continue
		}
		resolved, resolveErr := n.filterParamsForSet(optCtx, pci, params)
		if resolveErr != nil {
			return resolveErr
		}
		klog.Infof("Setting NVConfig params on device %s: %s", pci, resolved)
		if resolved == "" {
			if err := n.runMlxconfig(pci, "--with_default set", "BOOT_DBG_LOG=0"); err != nil {
				return err
			}
			continue
		}
		if err := n.runMlxconfig(pci, "--with_default set", resolved); err != nil {
			return err
		}
	}
	return nil
}

// Possible nvconfigs input (per DPUFlavor CRD validation):
//   - Empty: [] — no config; every PCI gets "" (reset-only).
//   - Single wildcard: [{ Device: "*", Parameters: [...] }] or [{ Parameters: [...] }] — same params for all devices.
//   - Single port: [{ Device: "p0", ... }] or [{ Device: "p1", ... }] — only that port gets params; others get "".
//   - Both ports: [{ Device: "p0", ... }, { Device: "p1", ... }] — each gets its own params (max 2 entries).
//
// Note:
// 1. Wildcard and per-device cannot be mixed.
// 2. Device values are unique and case-insensitive (e.g. p0, P0, p1).
// See DPUFlavor CRD validation for more details.
func pciToNVConfig(nvconfigs []provisioningv1.NVConfig, pciToNetdev map[string]string) map[string]string {
	out := make(map[string]string, len(pciToNetdev))
	for pci, netdev := range pciToNetdev {
		params := ""
		for _, nc := range nvconfigs {
			device := "*"
			if nc.Device != nil {
				device = strings.ToLower(strings.TrimSpace(*nc.Device))
			}
			joined := strings.Join(nc.Parameters, " ")
			if strings.EqualFold(device, netdev) {
				params = joined
				break
			}
			if device == "*" {
				params = joined
			}
		}
		out[pci] = params
	}
	return out
}

func (n *ConfigureNVConfig) pciToNetdevMap(optCtx *operations.Context) (map[string]string, error) {
	ports, err := optCtx.NSPorts()
	if err != nil {
		return nil, err
	}
	pciToNetdev := make(map[string]string, len(ports))
	for _, port := range ports {
		pciToNetdev[pciutil.NormalizeAddress(port.PCIAddress)] = port.Netdev
	}
	return pciToNetdev, nil
}

// runMlxconfig runs an mlxconfig command via bash and returns a wrapped error on failure.
func (n *ConfigureNVConfig) runMlxconfig(dev, op, args string) error {
	cmd := strings.TrimSpace(fmt.Sprintf("mlxconfig -d %s -y %s %s", dev, op, args))
	_, stderr, err := n.runBash(cmd)
	if err != nil {
		return fmt.Errorf("%s: %w (stderr: %s)", cmd, err, stderr.String())
	}
	return nil
}

func (n *ConfigureNVConfig) queryMlxconfig(dev string) (string, error) {
	cmd := fmt.Sprintf("mlxconfig -d %s q", dev)
	stdout, stderr, err := n.runBash(cmd)
	if err != nil {
		return "", fmt.Errorf("%s: %w (stderr: %s)", cmd, err, stderr.String())
	}
	return stdout.String(), nil
}

// filterParamsForSet queries mlxconfig and returns only parameters to apply on this pass.
// Used only when RebootMethodDiscovery is true. Parameters not listed in mlxconfig q output
// are deferred (partial apply succeeds) and recorded in optCtx.CondMessage.
func (n *ConfigureNVConfig) filterParamsForSet(optCtx *operations.Context, dev, params string) (string, error) {
	if strings.TrimSpace(params) == "" {
		return params, nil
	}
	entries := parseParamEntries(params)
	if len(entries) == 0 {
		return "", nil
	}
	queryOut, err := n.queryMlxconfig(dev)
	if err != nil {
		return "", err
	}
	available := parseMlxconfigQuery(queryOut)
	toSet, deferred := planParamApply(entries, available)
	if len(deferred) > 0 {
		deferredParams := joinParamEntries(deferred)
		optCtx.DeferredNVConfigParams = append(optCtx.DeferredNVConfigParams, operations.DeferredNVConfigParam{
			Device: dev,
			Params: deferredParams,
		})
		msg := fmt.Sprintf(
			"device=%s deferred NVConfig params (not exposed by mlxconfig q on this pass): [%s]",
			dev,
			deferredParams,
		)
		klog.Info(msg)
		if optCtx.CondMessage != "" {
			optCtx.CondMessage += " "
		}
		optCtx.CondMessage += msg
	}
	return joinParamEntries(toSet), nil
}

func planParamApply(desired []nvParam, available map[string]struct{}) (toSet, deferred []nvParam) {
	for _, entry := range desired {
		if _, ok := available[entry.name]; !ok {
			deferred = append(deferred, entry)
			continue
		}
		toSet = append(toSet, entry)
	}
	return toSet, deferred
}

func parseMlxconfigQuery(output string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "-E-") || strings.HasPrefix(line, "Configurations:") {
			continue
		}
		name, ok := parseMlxconfigQueryLine(line)
		if !ok {
			continue
		}
		out[strings.ToUpper(name)] = struct{}{}
	}
	return out
}

func parseMlxconfigQueryLine(line string) (name string, ok bool) {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	if i >= len(line) || line[i] < 'A' || line[i] > 'Z' {
		return "", false
	}
	start := i
	for i < len(line) {
		c := line[i]
		if c != '_' && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			break
		}
		i++
	}
	if i == start {
		return "", false
	}
	name = line[start:i]
	if strings.TrimSpace(line[i:]) == "" {
		return "", false
	}
	return name, true
}

func parseParamEntries(params string) []nvParam {
	parts := strings.Fields(strings.TrimSpace(params))
	out := make([]nvParam, 0, len(parts))
	for _, part := range parts {
		name, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		out = append(out, nvParam{
			name:  strings.ToUpper(strings.TrimSpace(name)),
			value: strings.TrimSpace(value),
		})
	}
	return out
}

func joinParamEntries(entries []nvParam) string {
	if len(entries) == 0 {
		return ""
	}
	parts := make([]string, len(entries))
	for i, entry := range entries {
		parts[i] = entry.name + "=" + entry.value
	}
	return strings.Join(parts, " ")
}
