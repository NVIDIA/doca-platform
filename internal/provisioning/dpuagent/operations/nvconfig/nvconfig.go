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
	"sort"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"
	pciutil "github.com/nvidia/doca-platform/internal/provisioning/utils/pci"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

const (
	CondNVConfigApplied = "NVConfigApplied"

	flavourPhysical = "physical"
)

type ConfigureNVConfig struct {
	runBash        func(cmd string) (bytes.Buffer, bytes.Buffer, error)
	getDevlinkPort func() (string, error)
	getNetdevPCI   func(netdev string) (string, error)
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
	pciToNetdev, err := n.pciToNetdevMap(optCtx.NSNIC)
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
	// false: reset all target PCI devices, then set per PCI. true: --with_default set only.
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
	for _, pair := range ordered {
		pci, params := pair.pci, pair.params
		klog.Infof("Setting NVConfig params on device %s: %s", pci, params)
		if params == "" {
			// Avoid mlxconfig reset (which always forces a device reset). Use --with_default set
			// with a parameter that equals its default (e.g. BOOT_DBG_LOG=0), so the config is
			// unchanged but no reset is triggered when current configuration equal to default.
			if err := n.runMlxconfig(pci, "--with_default set", "BOOT_DBG_LOG=0"); err != nil {
				return err
			}
		} else {
			if err := n.runMlxconfig(pci, "--with_default set", params); err != nil {
				return err
			}
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

func (n *ConfigureNVConfig) pciToNetdevMap(target *hostutil.Device) (map[string]string, error) {
	if n.getDevlinkPort == nil {
		n.getDevlinkPort = getDevlinkPort
	}
	if target == nil {
		return nil, fmt.Errorf("N/S NIC is not initialized")
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
	if n.getNetdevPCI == nil {
		n.getNetdevPCI = pciutil.NetdevPCI
	}
	allowedPCI := pciutil.AddressSet(target.PFPCIAddresses())

	// 2. Physical uplinks (p0/p1) are auxiliary devlink entries. Resolve each
	// physical netdev back to its PF PCI address through sysfs uevent.
	pciToNetdev := make(map[string]string)
	for key, entry := range out.Port {
		if !strings.EqualFold(strings.TrimSpace(entry.Flavor), flavourPhysical) {
			continue
		}
		netdev := strings.TrimSpace(entry.Netdev)
		if netdev == "" {
			return nil, fmt.Errorf("physical devlink port %s has no netdev", key)
		}
		pci, err := n.getNetdevPCI(netdev)
		if err != nil {
			return nil, fmt.Errorf("get PCI address for physical netdev %s: %w", netdev, err)
		}
		pci = pciutil.NormalizeAddress(pci)
		if pci == "" {
			klog.Infof("Skipping physical devlink port with no PCI address, netdev=%s", netdev)
			continue
		}
		if _, ok := allowedPCI[pci]; !ok {
			klog.Infof("Skipping physical devlink port outside target NIC, netdev=%s, pciAddress=%s, targetPCIAddresses=%v", netdev, pci, target.PFPCIAddresses())
			continue
		}
		if existing, ok := pciToNetdev[pci]; ok && existing != netdev {
			return nil, fmt.Errorf("multiple physical netdevs for PCI %s: %s, %s", pci, existing, netdev)
		}
		pciToNetdev[pci] = netdev
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

func getDevlinkPort() (string, error) {
	const cmd = "devlink port show -j"
	stdout, stderr, err := bash.Run(cmd)
	if err != nil {
		return "", fmt.Errorf("%s: %w (stderr: %s)", cmd, err, stderr.String())
	}
	return stdout.String(), nil
}
