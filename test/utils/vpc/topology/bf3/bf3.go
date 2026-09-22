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

// Package bf3 holds Weave e2e topology for BlueField-3 lab hardware.
package bf3

import "github.com/nvidia/doca-platform/test/utils/vpc/topology"

// Topology is the BF3 NIC Cloud / Weave port layout.
var Topology = topology.Topology{
	P0: topology.Rail{
		HostPFName:           "enp8s0f0np0",
		Uplink:               "p0",
		PFRepresentor:        "pf0hpf",
		PCIAddress:           "0000:03:00.0",
		OverlayDHCPInterface: "nic0",
		DHCPNADName:          "dhcp-nic-n0",
		DHCPBridgeName:       "br-dhcp-n0",
		DropBridgeName:       "br-drop-n0",
	},
	P1: topology.Rail{
		HostPFName:           "enp8s0f1np1",
		Uplink:               "p1",
		PFRepresentor:        "pf1hpf",
		PCIAddress:           "0000:03:00.1",
		OverlayDHCPInterface: "nic1",
		DHCPNADName:          "dhcp-nic-n1",
		DHCPBridgeName:       "br-dhcp-n1",
		DropBridgeName:       "br-drop-n1",
	},
	HostPFRDMADevice: "mlx5_0",
	VNetSubnet:       "10.0.0.0/8",
	// With HW offload, BF3 sustain >60 Gbit/sec.
	RDMAMinAvgBWGbit: 60.0,
}
