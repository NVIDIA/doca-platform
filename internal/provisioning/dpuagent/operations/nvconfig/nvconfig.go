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
	"encoding/json"
	"fmt"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"
	"github.com/nvidia/doca-platform/pkg/utils/networkhelper"

	"github.com/Masterminds/semver/v3"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

const (
	CondNVConfigApplied = "NVConfigApplied"
	minMftVersion       = "4.35.1"

	pciPrefix       = "pci/"
	flavourPhysical = "physical"
)

type GetLatestDPU struct {
}

func (g *GetLatestDPU) Name() string {
	return "Get Latest DPU"
}

func (g *GetLatestDPU) ConditionType() string {
	return "DPURetrieved"
}

func (g *GetLatestDPU) ShouldSkip(ctx *operations.Context) bool {
	return false
}

func (g *GetLatestDPU) ShouldUpdateStatusBeforeContinue(ctx *operations.Context) bool {
	return false
}

func (g *GetLatestDPU) Execute(execCtx context.Context, optCtx *operations.Context) error {
	dpu := &provisioningv1.DPU{}
	if err := optCtx.Client.GetObject(execCtx, optCtx.Options.DPUNamespace, optCtx.Options.DPUName, dpu); err != nil {
		return err
	}
	optCtx.LatestDPU = dpu
	return nil
}

type ConfigureNVConfig struct {
	runBash             func(cmd string) (bytes.Buffer, bytes.Buffer, error)
	getMlxconfigVersion func() (string, error)
	getDevlinkPort      func() (string, error)
	getUplinkName       func(pci string) (string, error)
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
	if ctx.LatestDPU.Status.DPUInternalStatus == nil {
		return false
	}
	cond := meta.FindStatusCondition(ctx.LatestDPU.Status.DPUInternalStatus.Conditions, CondNVConfigApplied)
	if cond != nil && cond.Status == metav1.ConditionTrue {
		klog.Infof("NVConfig already configured, skip")
		return true
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
	pciToNetdev, err := n.pciToNetdevMap()
	if err != nil {
		return fmt.Errorf("get devices from devlink: %w", err)
	}
	if len(pciToNetdev) == 0 {
		return fmt.Errorf("no physical ports from devlink (p0/p1)")
	}

	// 2. Build PCI -> NVConfig parameters map.
	nvconfigs := optCtx.DPUFlavor.Spec.NVConfig
	pciToParams := pciToNVConfig(nvconfigs, pciToNetdev)

	// 3. Order PCIs to move reset-only (empty params) on top of the list to run first
	// because mlxconfig reset can impact previously set config.
	type pciParams struct{ pci, params string }
	ordered := make([]pciParams, 0, len(pciToParams))
	for pci, params := range pciToParams {
		if params == "" {
			ordered = append(ordered, pciParams{pci: pci, params: params})
		}
	}
	for pci, params := range pciToParams {
		if params != "" {
			ordered = append(ordered, pciParams{pci: pci, params: params})
		}
	}

	// 4. Execute mlxconfig.
	// Get version and decide flow next to usage:
	// legacy = reset+set, new = set --with_default [params].
	version, err := n.mlxconfigVersion()
	if err != nil {
		return fmt.Errorf("failed to get mlxconfig (MFT) version: %w", err)
	}
	klog.Infof("mlxconfig (MFT) version: %s", version.String())
	minVer, err := semver.NewVersion(minMftVersion)
	if err != nil {
		return fmt.Errorf("invalid min MFT version constant %q: %w", minMftVersion, err)
	}
	legacyFlow := version.LessThan(minVer)
	for _, pair := range ordered {
		pci, params := pair.pci, pair.params
		klog.Infof("Setting NVConfig params on device %s: %s", pci, params)
		if legacyFlow {
			if err := n.runMlxconfig(pci, "reset", ""); err != nil {
				return err
			}
			if params != "" {
				if err := n.runMlxconfig(pci, "set", params); err != nil {
					return err
				}
			}
		} else {
			if params == "" {
				if err := n.runMlxconfig(pci, "reset", ""); err != nil {
					return err
				}
			} else {
				if err := n.runMlxconfig(pci, "--with_default set", params); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (n *ConfigureNVConfig) mlxconfigVersion() (*semver.Version, error) {
	if n.getMlxconfigVersion == nil {
		n.getMlxconfigVersion = getMlxconfigVersion
	}
	output, err := n.getMlxconfigVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to get mlxconfig version: %w", err)
	}
	// Output format: "mlxconfig, mft 4.30.1-8, built on Nov 28 2024..."
	var ver *semver.Version
	for _, field := range strings.Fields(output) {
		field = strings.TrimRight(field, ".,;")
		ver, err = semver.NewVersion(field)
		if err == nil && strings.Count(field, ".") >= 2 {
			return ver, nil
		}
	}
	return nil, fmt.Errorf("failed to extract version from output: %s", output)
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

// devlinkPortShowJSON is the structure of "devlink port show -j" output.
type devlinkPortShowJSON struct {
	Port map[string]devlinkPortEntry `json:"port"`
}

// devlinkPortEntry is one port entry in devlink port show JSON.
type devlinkPortEntry struct {
	Type   string `json:"type"`
	Netdev string `json:"netdev"`
	//nolint:misspell // devlink API key is British spelling "flavour"
	Flavor string `json:"flavour"`
	Port   *int   `json:"port,omitempty"`
}

func (n *ConfigureNVConfig) pciToNetdevMap() (map[string]string, error) {
	if n.getDevlinkPort == nil {
		n.getDevlinkPort = getDevlinkPort
	}
	// 1. Get devlink port show JSON.
	output, err := n.getDevlinkPort()
	if err != nil {
		return nil, err
	}
	var out devlinkPortShowJSON
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		return nil, fmt.Errorf("devlink port show: parse JSON: %w", err)
	}
	if out.Port == nil {
		return nil, fmt.Errorf("devlink port show: missing \"port\" object")
	}
	// 2. Collect unique PCI keys from all pci/... entries.
	pciToNetdev := make(map[string]string)
	for key := range out.Port {
		if !strings.HasPrefix(key, pciPrefix) {
			continue
		}
		rest := strings.TrimPrefix(key, pciPrefix)
		parts := strings.SplitN(rest, "/", 2)
		if len(parts) < 2 {
			continue
		}
		pci := parts[0]
		pciToNetdev[pci] = ""
	}
	// 3. Fill value for each PCI using getUplinkName (returns p0/p1 on DPU).
	if n.getUplinkName == nil {
		n.getUplinkName = func(pci string) (string, error) { return networkhelper.New().GetUplinkRepresentor(pci) }
	}
	for pci := range pciToNetdev {
		portName, err := n.getUplinkName(pci)
		if err != nil {
			return nil, fmt.Errorf("get uplink representor for %s: %w", pci, err)
		}
		pciToNetdev[pci] = portName
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

func getMlxconfigVersion() (string, error) {
	const cmd = "mlxconfig -v"
	stdout, stderr, err := bash.Run(cmd)
	if err != nil {
		return "", fmt.Errorf("%s: %w (stdout: %s, stderr: %s)", cmd, err, stdout.String(), stderr.String())
	}
	return stdout.String(), nil
}

func getDevlinkPort() (string, error) {
	const cmd = "devlink port show -j"
	stdout, stderr, err := bash.Run(cmd)
	if err != nil {
		return "", fmt.Errorf("%s: %w (stderr: %s)", cmd, err, stderr.String())
	}
	return stdout.String(), nil
}
