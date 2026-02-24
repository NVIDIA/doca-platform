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
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/filesystem"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/netplan"

	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
)

const (
	PFMTU              = 9216
	defaultNetplanRoot = "/etc/netplan"

	// tmfifoIPv6 is the fixed IPv6 link-local address for the tmfifo_net0 interface on the DPU.
	// This IP is used to communicate with the host agent running on the host.
	// Using fe80::2 on DPU side, fe80::1 on host side.
	// The DPU agent connects to the host via [fe80::1%tmfifo_net0]:11029.
	tmfifoIPv6 = "fe80::2/64"
)

type CheckNetwork struct {
}

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
	return optCtx.Client.HealthCheck()
}

type listPFFunc func() ([]string, error)

type ConfigureNetwork struct {
	// sysFSRoot is the root directory of the sysfs filesystem.
	sysFSRoot        string
	netplanRoot      string
	applyNetplanFunc func() error
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
		if err := n.setBridgeCommCh(ctx.Options.ControlPlaneMTU); err != nil {
			return fmt.Errorf("failed to create 99-dpf-comm-ch.yaml: %w", err)
		}
	}
	pfs, err := n.listPFsFromSysfs()()
	if err != nil {
		return fmt.Errorf("failed to list PFs: %w", err)
	}
	if err := n.setPFMTU(pfs); err != nil {
		return fmt.Errorf("failed to create 97-pf-mtu.yaml: %w", err)
	}
	klog.Infof("Successfully created all netplan files")
	if n.applyNetplanFunc == nil {
		n.applyNetplanFunc = runNetplanApply
	}
	if err := n.applyNetplanFunc(); err != nil {
		return fmt.Errorf("failed to apply netplan: %w", err)
	}
	return nil
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
		oob.MTU = ptr.To(cpMTU)
	} else {
		oob.DHCP4 = ptr.To(false)
		oob.DHCP6 = ptr.To(false)
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

func (n *ConfigureNetwork) setPFMTU(pfs []string) error {
	name := filepath.Join(n.netplanRoot, "97-pf-mtu.yaml")
	config := &netplan.Config{
		Network: netplan.Network{
			Version:  2,
			Renderer: "networkd",
		},
	}
	for _, pf := range pfs {
		if config.Network.Ethernets == nil {
			config.Network.Ethernets = make(map[string]netplan.Ethernet)
		}
		config.Network.Ethernets[pf] = netplan.Ethernet{
			MTU: ptr.To(int32(PFMTU)),
		}
	}
	return config.WriteToFile(name)
}

func (n *ConfigureNetwork) listPFsFromSysfs() listPFFunc {
	if n.sysFSRoot == "" {
		n.sysFSRoot = hostutil.SysFSRoot
	}
	return func() ([]string, error) {
		pfs := []string{}
		devs, err := hostutil.DiscoverDPUs(n.sysFSRoot)
		if err != nil {
			return nil, fmt.Errorf("failed to discover DPUs: %w", err)
		}
		if len(devs) == 0 {
			return nil, fmt.Errorf("no DPUs found")
		} else if len(devs) > 1 {
			return nil, fmt.Errorf("multiple DPUs found")
		}
		dev := devs[0]
		for i := 0; i < dev.NumOfPFs; i++ {
			pfs = append(pfs, fmt.Sprintf("p%d", i), fmt.Sprintf("pf%dhpf", i))
		}
		return pfs, nil
	}
}

func runNetplanApply() error {
	stdout, stderr, err := bash.Run("netplan apply")
	if err != nil {
		return fmt.Errorf("failed to apply netplan: %w, stdout: %s, stderr: %s", err, stdout.String(), stderr.String())
	}
	return nil
}
