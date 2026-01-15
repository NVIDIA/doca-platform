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
	"fmt"

	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/filesystem"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/netplan"

	"k8s.io/utils/ptr"
)

type NetplanOperation struct {
}

func (n *NetplanOperation) Name() string {
	return "Setup netplan"
}

func (n *NetplanOperation) Type() operations.OperationType {
	return operations.RunOnce
}

func (n *NetplanOperation) Execute(ctx operations.Context) error {
	if err := createOOBTmFifoNetplanFile(ctx.InstallConfig.Mode, ctx.InstallConfig.ControlPlaneMTU); err != nil {
		return fmt.Errorf("failed to create oo98-oob-tmfifo.yaml: %w", err)
	}
	if err := createBrCommChNetplanFile(ctx.InstallConfig.ControlPlaneMTU); err != nil {
		return fmt.Errorf("failed to create 99-dpf-comm-ch.yaml: %w", err)
	}
	if err := remove60Mlx(); err != nil {
		return fmt.Errorf("failed to remove 60-mlnx.yaml: %w", err)
	}
	return nil
}

func remove60Mlx() error {
	name := "/etc/netplan/60-mlnx.yaml"
	return filesystem.Remove(name)
}

func createOOBTmFifoNetplanFile(mode operations.InstallMode, cpMTU int32) error {
	name := "/etc/netplan/98-oob-tmfifo.yaml"
	oob := netplan.Ethernet{}
	if mode == operations.ZeroTrustMode {
		oob.DHCP4 = ptr.To(true)
		oob.MTU = ptr.To(cpMTU)
	} else {
		oob.DHCP4 = ptr.To(false)
	}

	tmFifo := netplan.Ethernet{
		DHCP4:     ptr.To(false),
		LinkLocal: ptr.To([]string{"ipv4"}),
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

func createBrCommChNetplanFile(cpMTU int32) error {
	name := "/etc/netplan/99-dpf-comm-ch.yaml"
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
