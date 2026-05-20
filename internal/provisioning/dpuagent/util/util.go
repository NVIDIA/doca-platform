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

package util

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"
)

var mstPCIAddressRegex = regexp.MustCompile(`domain:bus:dev\.fn=([0-9a-fA-F]{4}:[0-9a-fA-F]{2}:[0-9a-fA-F]{2}\.[0-9]+)`)

// DiscoverNSNIC selects the single NIC that dpu-agent should operate on.
// BF2/BF3 expose one DPU NIC. BF4 may expose multiple NICs; only the N/S NIC
// has the supported BF4 device ID.
func DiscoverNSNIC(sysFSRoot string) (*hostutil.Device, error) {
	devices, err := hostutil.DiscoverDPUs(sysFSRoot)
	if err != nil {
		return nil, err
	}
	if len(devices) == 0 {
		return nil, fmt.Errorf("no N/S NIC found")
	}
	if len(devices) > 1 {
		addrs := make([]string, 0, len(devices))
		for _, dev := range devices {
			addrs = append(addrs, dev.Address)
		}
		return nil, fmt.Errorf("multiple N/S NICs found: %s", strings.Join(addrs, ", "))
	}
	return &devices[0], nil
}

// MFTDevicesForNSNIC lists MST device paths and keeps only those backed by
// PF PCI addresses on the selected N/S NIC.
func MFTDevicesForNSNIC(mstDevicesPath string, nic *hostutil.Device, runBash bash.RunFunc) ([]string, error) {
	if runBash == nil {
		runBash = bash.Run
	}
	// BF4 images may not start MST automatically, so refresh devices before listing /dev/mst.
	if _, stderr, err := runBash("mst start"); err != nil {
		return nil, fmt.Errorf("failed to start mst: %w, stderr: %s", err, stderr.String())
	}

	devices, err := filepath.Glob(filepath.Join(mstDevicesPath, "*"))
	if err != nil {
		return nil, fmt.Errorf("failed to list MST devices: %w", err)
	}
	if len(devices) == 0 {
		return nil, fmt.Errorf("no MST devices found in %s", mstDevicesPath)
	}
	if nic == nil {
		return nil, fmt.Errorf("N/S NIC is not initialized")
	}

	allowed := map[string]struct{}{}
	for _, pci := range nic.PFPCIAddresses() {
		allowed[pci] = struct{}{}
	}

	selected := []string{}
	for _, device := range devices {
		pci, err := pciAddressFromMSTDevice(device)
		if err != nil {
			return nil, err
		}
		if _, ok := allowed[pci]; ok {
			selected = append(selected, device)
		}
	}
	return selected, nil
}

func pciAddressFromMSTDevice(device string) (string, error) {
	content, err := os.ReadFile(device)
	if err != nil {
		return "", fmt.Errorf("failed to read MST device %s: %w", device, err)
	}
	// Example MST device content:
	// /dev/mst/mt41692_pciconf0 - PCI configuration cycles access.
	//                            domain:bus:dev.fn=0000:03:00.0 addr.reg=88 data.reg=92 cr_bar.gw_offset=-1
	matches := mstPCIAddressRegex.FindSubmatch(content)
	if len(matches) != 2 {
		return "", fmt.Errorf("failed to parse PCI address from MST device %s", device)
	}
	return string(matches[1]), nil
}
