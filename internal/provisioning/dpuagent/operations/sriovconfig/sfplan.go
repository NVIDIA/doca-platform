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
	"regexp"
	"strconv"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	pciutil "github.com/nvidia/doca-platform/internal/provisioning/utils/pci"

	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
)

// hostControllerNumber is the controller a hostDevice group is created on.
const hostControllerNumber = 1

// legacyTrustedSFNumStart is the historical trusted-SF numbering (device-plugin chart range).
const legacyTrustedSFNumStart = 101

// plannedSF is one SF to create.
type plannedSF struct {
	device        string // ECPF PCI address
	netdev        string // ECPF netdev, e.g. p0; device-plugin pfNames
	sfNum         int
	poolName      string // empty: hardware only
	trusted       bool
	controller    *int32
	cpuList       string
	mac           string
	disableRoCE   bool
	disableNetdev bool
}

// sfPlan is the resolved SFs for this DPU.
type sfPlan struct {
	ports []pciutil.NICPort
	sfs   []plannedSF
}

func (p *sfPlan) sfsOnDevice(device string) []plannedSF {
	var out []plannedSF
	for _, sf := range p.sfs {
		if pciutil.NormalizeAddress(sf.device) == pciutil.NormalizeAddress(device) {
			out = append(out, sf)
		}
	}
	return out
}

// sfGroup is a ScalableFunction, including groups synthesized by compatibilityGroups.
type sfGroup struct {
	provisioningv1.ScalableFunction
}

// resolveSFPlan expands flavor groups into per-device SFs. dmaTargetDevice is only
// used by the compatibility path, to leave a PF_TOTAL_SF slot for the DMA SF.
func resolveSFPlan(flavor *provisioningv1.DPUFlavor, ports []pciutil.NICPort, isBF4 bool, dmaTargetDevice string) (*sfPlan, error) {
	if len(ports) == 0 {
		return nil, fmt.Errorf("target physical port not found")
	}

	groups := make([]sfGroup, 0, len(flavor.Spec.ScalableFunctions))
	for _, sf := range flavor.Spec.ScalableFunctions {
		groups = append(groups, sfGroup{ScalableFunction: sf})
	}
	if len(flavor.Spec.ScalableFunctions) == 0 && len(flavor.Spec.VirtualFunctions) == 0 {
		compat, err := compatibilityGroups(flavor, ports, isBF4, dmaTargetDevice)
		if err != nil {
			return nil, err
		}
		groups = compat
	}

	sfs, err := resolveSFs(groups, ports)
	if err != nil {
		return nil, err
	}
	return &sfPlan{ports: ports, sfs: sfs}, nil
}

// resolveSFs assigns sfnums: pinned sfNumStart runs first, then sequential from 0.
func resolveSFs(groups []sfGroup, ports []pciutil.NICPort) ([]plannedSF, error) {
	targets := make([][]pciutil.NICPort, len(groups))
	cursor := newDeviceCursor()
	for i, g := range groups {
		count := ptr.Deref(g.Count, 0)
		selected, err := selectGroupPorts(ports, g.Device, count, "scalableFunctions", i)
		if err != nil {
			return nil, err
		}
		targets[i] = selected

		for _, num := range pinnedSFNums(g) {
			for _, port := range selected {
				if !cursor.reserve(port.PCIAddress, num) {
					return nil, fmt.Errorf("scalableFunctions[%d]: sfnum %d is already taken on device %s", i, num, port.Netdev)
				}
			}
		}
	}

	var out []plannedSF
	for i, g := range groups {
		for _, port := range targets[i] {
			nums := pinnedSFNums(g)
			if nums == nil {
				nums = cursor.take(port.PCIAddress, int(ptr.Deref(g.Count, 0)))
			}
			for _, num := range nums {
				sf, err := newPlannedSF(g, port, num)
				if err != nil {
					return nil, fmt.Errorf("scalableFunctions[%d]: %w", i, err)
				}
				out = append(out, sf)
			}
		}
	}
	return out, nil
}

// pinnedSFNums is the sfNumStart run of length count, or nil if the agent numbers the group.
func pinnedSFNums(g sfGroup) []int {
	count := ptr.Deref(g.Count, 0)
	if count <= 0 || g.Options == nil || g.Options.SFNumStart == nil {
		return nil
	}
	start := int(*g.Options.SFNumStart)
	nums := make([]int, 0, count)
	for i := range int(count) {
		nums = append(nums, start+i)
	}
	return nums
}

func newPlannedSF(g sfGroup, port pciutil.NICPort, sfNum int) (plannedSF, error) {
	sf := plannedSF{
		device:   port.PCIAddress,
		netdev:   port.Netdev,
		sfNum:    sfNum,
		poolName: ptr.Deref(g.PoolName, ""),
	}
	if ptr.Deref(g.HostDevice, false) {
		sf.controller = ptr.To(int32(hostControllerNumber))
	}
	if g.Options == nil {
		return sf, nil
	}
	sf.trusted = ptr.Deref(g.Options.Trusted, false)
	sf.cpuList = ptr.Deref(g.Options.CPUList, "")
	sf.disableRoCE = ptr.Deref(g.Options.DisableRoCE, false)
	sf.disableNetdev = ptr.Deref(g.Options.DisableNetdev, false)
	if g.Options.Controller != nil {
		sf.controller = g.Options.Controller
	}
	if g.Options.MACAddress != nil {
		mac, err := canonicalMAC(*g.Options.MACAddress)
		if err != nil {
			return plannedSF{}, fmt.Errorf("options.macAddress: %w", err)
		}
		sf.mac = mac
	}
	return sf, nil
}

// compatibilityGroups synthesizes the pre-API groups: PF_TOTAL_SF workload SFs minus
// trusted and DMA, plus trusted SFs from the annotation, on every port (BF4) or p0
// (earlier). This is the only place counts are derived from PF_TOTAL_SF.
//
//nolint:staticcheck // SA1019: TrustedSFCount remains supported for compatibility.
func compatibilityGroups(flavor *provisioningv1.DPUFlavor, ports []pciutil.NICPort, isBF4 bool, dmaTargetDevice string) ([]sfGroup, error) {
	pfTotalSF := pfTotalSFFromFlavor(flavor)
	if pfTotalSF <= 0 {
		return nil, nil
	}
	trusted := trustedSFCountFromFlavor(flavor)
	if trusted > 0 {
		klog.Warningf("annotation %s is deprecated and will be ignored in a future release in v27.x: declare a spec.scalableFunctions group with options.trusted: true and poolName: %s instead",
			cutil.TrustedSFCount, compatibilityTrustedSFPool)
	}
	dmaSFs := 0
	if dmaTargetDevice != "" {
		dmaSFs = 1
	}
	if pfTotalSF-trusted-dmaSFs < 0 {
		return nil, fmt.Errorf("insufficient SF capacity: PF_TOTAL_SF=%d cannot fit %d trusted SF(s) and %d DMA SF(s)",
			pfTotalSF, trusted, dmaSFs)
	}

	devices := []*string{ptr.To("*")}
	if !isBF4 {
		devices = []*string{ptr.To("p0")}
	}
	if dmaTargetDevice != "" {
		// DMA occupies one slot on its ECPF only; express that as per-port groups.
		devices = nil
		for _, port := range ports {
			if isBF4 || port.Netdev == "p0" {
				devices = append(devices, ptr.To(port.PCIAddress))
			}
		}
	}

	var groups []sfGroup
	for _, device := range devices {
		workload := pfTotalSF - trusted
		if dmaTargetDevice != "" && pciutil.NormalizeAddress(*device) == pciutil.NormalizeAddress(dmaTargetDevice) {
			workload--
		}
		if workload > 0 {
			groups = append(groups, sfGroup{ScalableFunction: provisioningv1.ScalableFunction{
				Count:    ptr.To(int32(workload)),
				Device:   device,
				PoolName: ptr.To(compatibilitySFPool),
			}})
		}
		if trusted > 0 {
			groups = append(groups, sfGroup{ScalableFunction: provisioningv1.ScalableFunction{
				Count:    ptr.To(int32(trusted)),
				Device:   device,
				PoolName: ptr.To(compatibilityTrustedSFPool),
				Options: &provisioningv1.ScalableFunctionOptions{
					Trusted:    ptr.To(true),
					SFNumStart: ptr.To(int32(legacyTrustedSFNumStart)),
				},
			}})
		}
	}
	return groups, nil
}

var pfTotalSFRegex = regexp.MustCompile(`^PF_TOTAL_SF=([0-9]+)`)

func pfTotalSFFromFlavor(flavor *provisioningv1.DPUFlavor) int {
	for _, nvconfig := range flavor.Spec.NVConfig {
		for _, parameter := range nvconfig.Parameters {
			matches := pfTotalSFRegex.FindStringSubmatch(parameter)
			if len(matches) == 2 {
				if num, err := strconv.Atoi(matches[1]); err == nil {
					return num
				}
			}
		}
	}
	return 0
}

//nolint:staticcheck // SA1019: TrustedSFCount remains supported for compatibility.
func trustedSFCountFromFlavor(flavor *provisioningv1.DPUFlavor) int {
	count, err := strconv.Atoi(flavor.Annotations[cutil.TrustedSFCount])
	if err != nil || count <= 0 || count > MaxTrustedSfs {
		return 0
	}
	return count
}
