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

// Package topology describes per-hardware Weave rail layout used by e2e tests.
package topology

import (
	"fmt"
	"strings"
)

// Rail is one NIC rail: host PF, DPU-side names, and PCI used by OVS bridges/metrics.
type Rail struct {
	// HostPFName is the host-side PF netdev (e.g. enp8s0f0np0, ens6np0).
	HostPFName string
	// Uplink is the DPU netdev used for PF MAC lookup (e.g. p0, A1p0).
	Uplink string
	// PFRepresentor is the host PF representor (e.g. pf0hpf on BF3, A1c1pf0 on BF4).
	PFRepresentor string
	// PCIAddress is the canonical PCI BDF.
	PCIAddress string
	// OverlayDHCPInterface is the DPU overlay DHCP netdev (e.g. nic0, r0swp0).
	OverlayDHCPInterface string
	// DHCPNADName is the DHCP NetworkAttachmentDefinition name (e.g. dhcp-nic-n0, dhcp-r0swp0).
	DHCPNADName string
	// DHCPBridgeName is the OVS DHCP bridge for this rail (e.g. br-dhcp-n0, br-dhcp-r0swp0).
	DHCPBridgeName string
	// DropBridgeName is the OVS drop bridge for this rail (e.g. br-drop-n0, br-drop-r0swp0).
	DropBridgeName string
	// VRFName is the host VRF for this rail (BF4 ZT), empty when unused.
	VRFName string
	// VRFTable is the routing table id for VRFName, 0 when unused.
	VRFTable int
}

// IsolationBridgeName returns br-isol-<vni>-<pci> with :/. in PCIAddress replaced by _.
func (r Rail) IsolationBridgeName(vni uint32) string {
	pci := strings.NewReplacer(":", "_", ".", "_").Replace(r.PCIAddress)
	return fmt.Sprintf("br-isol-%d-%s", vni, pci)
}

// Topology is the fixed lab wiring for one BlueField generation.
type Topology struct {
	// P0 and P1 are the two NIC rails/sw planes.
	P0, P1 Rail
	// HostPFRDMADevice is the ibv device for P0 RDMA tests (e.g. mlx5_2).
	HostPFRDMADevice string
	// VNetSubnet is the overlay IPv4 CIDR for Weave virtual networks.
	VNetSubnet string
	// RDMAMinAvgBWGbit is the min ib_write_bw average Gbit/sec with HW offload.
	RDMAMinAvgBWGbit float32
}
