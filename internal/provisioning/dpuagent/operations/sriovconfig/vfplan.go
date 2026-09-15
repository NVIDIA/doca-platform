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

package sriovconfig

import (
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	pciutil "github.com/nvidia/doca-platform/internal/provisioning/utils/pci"

	"k8s.io/utils/ptr"
)

// plannedVF is VF indices [index, index+count) from one group on one ECPF.
type plannedVF struct {
	device   string
	netdev   string
	index    int
	count    int
	poolName string
	mac      string
}

// vfPlan is the resolved VFs for this DPU.
type vfPlan struct {
	ports []pciutil.NICPort
	vfs   []plannedVF
	// totals is sriov_numvfs per ECPF. A device only appears here when a group explicitly selected it.
	// count 0  is an explicit "empty this device", absence means never selecting the device at all.
	totals map[string]int
}

func (p *vfPlan) vfsOnDevice(device string) []plannedVF {
	var out []plannedVF
	for _, vf := range p.vfs {
		if pciutil.NormalizeAddress(vf.device) == pciutil.NormalizeAddress(device) {
			out = append(out, vf)
		}
	}
	return out
}

// resolveVFPlan expands the flavor's Virtual Function groups into per-device VF creations
// and calculates the VF count to write on each selected device. Several groups may select the same
// device: their counts add up into that device's single sriov_numvfs value, and each
// group takes a contiguous run of VF indices, numbered sequentially from 0 in
// declaration order.

// Given a spec like below with devices p0("0000:03:00.0") and p1("0000:03:00.1"), it will resolve to:
// ```
// virtualFunctions:
//   - count: 4
//     device: "*"
//     poolName: "bf_vf"
//   - count: 2
//     device: "p1"
//     poolName: "bf_vf_2"
//
// ```
// it will resolve to:
// ```
//
//	plan.totals = {
//	    "0000:03:00.0": 4,
//	    "0000:03:00.1": 6,
//	}
//
// plan.vfs = [
//
//	{device: "0000:03:00.0", netdev: "p0", index: 0, count: 4, poolName: "bf_vf"},
//	{device: "0000:03:00.1", netdev: "p1", index: 0, count: 4, poolName: "bf_vf"},
//	{device: "0000:03:00.1", netdev: "p1", index: 4, count: 2, poolName: "bf_vf_2"},]
//
// ```
func resolveVFPlan(flavor *provisioningv1.DPUFlavor, ports []pciutil.NICPort) (*vfPlan, error) {
	if len(ports) == 0 {
		return nil, fmt.Errorf("target physical port not found")
	}

	cursor := newDeviceCursor()
	plan := &vfPlan{ports: ports, totals: map[string]int{}}
	for i, g := range flavor.Spec.VirtualFunctions {
		count := ptr.Deref(g.Count, 0)
		selected, err := selectGroupPorts(ports, g.Device, count, "virtualFunctions", i)
		if err != nil {
			return nil, err
		}
		for _, port := range selected {
			plan.totals[port.PCIAddress] += int(count)
			if count == 0 {
				continue
			}
			vf := plannedVF{
				device:   port.PCIAddress,
				netdev:   port.Netdev,
				index:    cursor.take(port.PCIAddress, int(count))[0],
				count:    int(count),
				poolName: ptr.Deref(g.PoolName, ""),
			}
			if g.Options != nil && g.Options.MACAddress != nil {
				mac, err := canonicalMAC(*g.Options.MACAddress)
				if err != nil {
					return nil, fmt.Errorf("virtualFunctions[%d]: options.macAddress: %w", i, err)
				}
				vf.mac = mac
			}
			plan.vfs = append(plan.vfs, vf)
		}
	}
	return plan, nil
}
