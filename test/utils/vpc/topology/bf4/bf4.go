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

// Package bf4 holds Weave e2e topology and lab wiring for BlueField-4.
package bf4

import "github.com/nvidia/doca-platform/test/utils/vpc/topology"

const (
	// NetutilsContainer is the remote netutils container name.
	NetutilsContainer = "vpc-netutils"
	// HostPasswordEnv is the SSH password env var.
	HostPasswordEnv = "PHYSICAL_PASSWORD"
	// HostUserEnv is the SSH user env var.
	HostUserEnv = "PHYSICAL_USER"
	// ContainerRuntimeEnv is docker or podman.
	ContainerRuntimeEnv = "VPC_CONTAINER_RUNTIME"
	// DefaultHostUser is used when HostUserEnv is unset.
	DefaultHostUser = "depuser"
	// DefaultContainerRuntime is used when ContainerRuntimeEnv is unset.
	DefaultContainerRuntime = "podman"
	// IBWriteBWMTU is the ib_write_bw -m value.
	IBWriteBWMTU = 4096
)

// Topology is the BF4 dual-NIC rail layout (host PFs, DPU PF0 reps, VRFs, PCI).
var Topology = topology.Topology{
	P0: topology.Rail{
		HostPFName:           "r0sw0pf0",
		Uplink:               "A1p0",
		PFRepresentor:        "A1c1pf0",
		PCIAddress:           "0000:01:00.0",
		OverlayDHCPInterface: "r0swp0",
		DHCPNADName:          "dhcp-r0swp0",
		DHCPBridgeName:       "br-dhcp-r0swp0",
		DropBridgeName:       "br-drop-r0swp0",
		VRFName:              "rail0-swp0",
		VRFTable:             100,
	},
	P1: topology.Rail{
		HostPFName:           "r0sw1pf0",
		Uplink:               "A11p0",
		PFRepresentor:        "A11c1pf0",
		PCIAddress:           "0001:01:00.0",
		OverlayDHCPInterface: "r0swp1",
		DHCPNADName:          "dhcp-r0swp1",
		DHCPBridgeName:       "br-dhcp-r0swp1",
		DropBridgeName:       "br-drop-r0swp1",
		VRFName:              "rail0-swp1",
		VRFTable:             101,
	},
	HostPFRDMADevice: "rdma_r0sw0pf0",
	VNetSubnet:       "172.16.0.0/12",
	RDMAMinAvgBWGbit: 300.0,
}
