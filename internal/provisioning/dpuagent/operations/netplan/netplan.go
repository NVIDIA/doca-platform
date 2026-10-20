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

package netplan

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/filesystem"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/netplan"

	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
)

const (
	defaultNetplanRoot        = "/etc/netplan"
	defaultSystemdNetworkRoot = "/etc/systemd/network"
	defaultSysctlRoot         = "/etc/sysctl.d"

	oobNetworkOverrideFile = "02-oob.network"
	oobARPConfigFile       = "99-z-dpf-oob-arp.conf"

	oobNetworkOverride = `[Match]
Name=oob_net0

[Network]
DHCP=no
LinkLocalAddressing=no
IPv6AcceptRA=no
`

	oobARPConfig = `net.ipv4.conf.all.arp_ignore=1
net.ipv4.conf.all.arp_announce=2
`

	// tmfifoIPv6 is the fixed IPv6 link-local address for the tmfifo_net0 interface on the DPU.
	// This IP is used to communicate with the host agent running on the host.
	// Using fe80::2 on DPU side, fe80::1 on host side.
	// The DPU agent connects to the host via [fe80::1%tmfifo_net0]:11029.
	tmfifoIPv6 = "fe80::2/64"
)

type CheckNetwork struct{}

func (c *CheckNetwork) Name() string {
	return "Check Network"
}

func (c *CheckNetwork) ConditionType() string {
	return "NetworkChecked"
}

func (c *CheckNetwork) ShouldSkip(ctx *operations.Context) bool {
	return false
}

func (c *CheckNetwork) ShouldUpdateStatusBeforeContinue(ctx *operations.Context) bool {
	return false
}

func (c *CheckNetwork) Execute(execCtx context.Context, optCtx *operations.Context) error {
	_, err := optCtx.K8sClient.Discovery().ServerVersion()
	return err
}

type ConfigureNetwork struct {
	netplanRoot        string
	systemdNetworkRoot string
	sysctlRoot         string
	applyNetplanFunc   func() error
	applyOOBARPFunc    func(string) error
	reconfigureOOBFunc func() error
}

func (n *ConfigureNetwork) Name() string {
	return "Configure Network"
}

func (n *ConfigureNetwork) ConditionType() string {
	return "NetworkConfigured"
}

func (n *ConfigureNetwork) ShouldSkip(ctx *operations.Context) bool {
	return ctx.Options.SkipNetworkConfig
}

func (n *ConfigureNetwork) ShouldUpdateStatusBeforeContinue(ctx *operations.Context) bool {
	return false
}

func (n *ConfigureNetwork) Execute(execCtx context.Context, optCtx *operations.Context) error {
	return n.configNetplan(optCtx)
}

func (n *ConfigureNetwork) configNetplan(ctx *operations.Context) error {
	if n.netplanRoot == "" {
		n.netplanRoot = defaultNetplanRoot
	}
	if err := n.remove60Mlx(); err != nil {
		return fmt.Errorf("failed to remove 60-mlnx.yaml: %w", err)
	}
	if err := n.setOOBAndRshimInterface(ctx.Options.ZeroTrustMode, ctx.Options.ControlPlaneMTU); err != nil {
		return fmt.Errorf("failed to create 98-oob-tmfifo.yaml: %w", err)
	}
	if !ctx.Options.ZeroTrustMode {
		if err := n.setHostTrustedOOBConfig(); err != nil {
			return fmt.Errorf("failed to configure host-trusted OOB interface: %w", err)
		}
		if err := n.setBridgeCommCh(ctx.Options.ControlPlaneMTU); err != nil {
			return fmt.Errorf("failed to create 99-dpf-comm-ch.yaml: %w", err)
		}
	}
	klog.Infof("Successfully created all netplan files")
	if n.applyNetplanFunc == nil {
		n.applyNetplanFunc = runNetplanApply
	}
	if err := n.applyNetplanFunc(); err != nil {
		return fmt.Errorf("failed to apply netplan: %w", err)
	}
	if !ctx.Options.ZeroTrustMode {
		arpConfigPath := filepath.Join(n.sysctlRoot, oobARPConfigFile)
		if n.applyOOBARPFunc == nil {
			n.applyOOBARPFunc = runApplyOOBARP
		}
		if err := n.applyOOBARPFunc(arpConfigPath); err != nil {
			return fmt.Errorf("failed to apply OOB ARP settings: %w", err)
		}
		if n.reconfigureOOBFunc == nil {
			n.reconfigureOOBFunc = runReconfigureOOB
		}
		if err := n.reconfigureOOBFunc(); err != nil {
			return fmt.Errorf("failed to reconfigure oob_net0: %w", err)
		}
	}
	return nil
}

func (n *ConfigureNetwork) setHostTrustedOOBConfig() error {
	if n.systemdNetworkRoot == "" {
		n.systemdNetworkRoot = defaultSystemdNetworkRoot
	}
	if n.sysctlRoot == "" {
		n.sysctlRoot = defaultSysctlRoot
	}
	if err := filesystem.MkdirAll(n.systemdNetworkRoot, 0755); err != nil {
		return err
	}
	if err := filesystem.AtomicWrite(filepath.Join(n.systemdNetworkRoot, oobNetworkOverrideFile), []byte(oobNetworkOverride), 0644); err != nil {
		return err
	}
	if err := filesystem.MkdirAll(n.sysctlRoot, 0755); err != nil {
		return err
	}
	return filesystem.AtomicWrite(filepath.Join(n.sysctlRoot, oobARPConfigFile), []byte(oobARPConfig), 0644)
}

func (n *ConfigureNetwork) remove60Mlx() error {
	name := filepath.Join(n.netplanRoot, "60-mlnx.yaml")
	if err := filesystem.Remove(name); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return nil
}

func (n *ConfigureNetwork) setOOBAndRshimInterface(zeroTrustMode bool, cpMTU int32) error {
	name := filepath.Join(n.netplanRoot, "98-oob-tmfifo.yaml")
	oob := netplan.Ethernet{}
	if zeroTrustMode {
		oob.DHCP4 = ptr.To(true)
		oob.DHCP6 = ptr.To(false)
		oob.AcceptRA = ptr.To(false)
		oob.MTU = ptr.To(cpMTU)
	} else {
		oob.DHCP4 = ptr.To(false)
		oob.DHCP6 = ptr.To(false)
		oob.AcceptRA = ptr.To(false)
		oob.LinkLocal = ptr.To([]string{})
		oob.Optional = ptr.To(true)
	}

	// Configure tmfifo_net0 with a fixed IPv6 link-local address.
	// Using fe80::2/64 on DPU side to communicate with host's fe80::1.
	// IPv6 link-local addresses are interface-scoped, so no routing conflicts occur.
	tmFifo := netplan.Ethernet{
		DHCP4:     ptr.To(false),
		DHCP6:     ptr.To(false),
		LinkLocal: ptr.To([]string{}),
		Addresses: []string{tmfifoIPv6},
	}

	config := &netplan.Config{
		Network: netplan.Network{
			Version:  2,
			Renderer: "networkd",
			Ethernets: map[string]netplan.Ethernet{
				"oob_net0":    oob,
				"tmfifo_net0": tmFifo,
			},
		},
	}
	return config.WriteToFile(name)
}

// setBridgeCommCh configures the communication bridge using pf0vf0, which is
// only valid for BF3.
func (n *ConfigureNetwork) setBridgeCommCh(cpMTU int32) error {
	name := filepath.Join(n.netplanRoot, "99-dpf-comm-ch.yaml")
	config := &netplan.Config{
		Network: netplan.Network{
			Version:  2,
			Renderer: "networkd",
			Ethernets: map[string]netplan.Ethernet{
				"pf0vf0": {
					MTU: ptr.To(cpMTU),
				},
			},
			Bridges: map[string]netplan.Bridge{
				"br-comm-ch": {
					Ethernet: netplan.Ethernet{
						DHCP4: ptr.To(true),
						MTU:   ptr.To(cpMTU),
					},
					Interfaces: []string{"pf0vf0"},
				},
			},
		},
	}
	return config.WriteToFile(name)
}

func runNetplanApply() error {
	stdout, stderr, err := bash.Run("netplan apply")
	if err != nil {
		return fmt.Errorf("failed to apply netplan: %w, stdout: %s, stderr: %s", err, stdout.String(), stderr.String())
	}
	return nil
}

func runApplyOOBARP(configPath string) error {
	stdout, stderr, err := bash.Run(fmt.Sprintf("sysctl --load %q", configPath))
	if err != nil {
		return fmt.Errorf("failed to load %s: %w, stdout: %s, stderr: %s", configPath, err, stdout.String(), stderr.String())
	}
	return nil
}

func runReconfigureOOB() error {
	stdout, stderr, err := bash.Run("networkctl reload && networkctl reconfigure oob_net0")
	if err != nil {
		return fmt.Errorf("failed to reload networkd and reconfigure oob_net0: %w, stdout: %s, stderr: %s", err, stdout.String(), stderr.String())
	}
	return nil
}
