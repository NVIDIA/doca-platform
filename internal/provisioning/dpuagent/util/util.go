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
	"os"
	"path/filepath"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	pciutil "github.com/nvidia/doca-platform/internal/provisioning/utils/pci"

	"k8s.io/klog/v2"
)

// IsBlueField4 reports whether dpu is a BlueField-4 DPU.
func IsBlueField4(dpu *provisioningv1.DPU) bool {
	return dpu != nil && dpu.Status.DPUType == provisioningv1.DPUTypeBlueField4
}

var sysfsPCIDevicesDir = "/sys/bus/pci/devices"

// nsNICDeviceIDs lists PCI device IDs for known DPU N/S NICs.
var nsNICDeviceIDs = map[string]struct{}{
	"0xa2d6": {}, // BlueField-2
	"0xa2dc": {}, // BlueField-3
	"0xa2df": {}, // BlueField-4
}

// NSPortFilter returns true for ports backed by a known N/S NIC device ID.
func NSPortFilter(port pciutil.NICPort) bool {
	devicePath := filepath.Join(sysfsPCIDevicesDir, port.PCIAddress, "device")
	data, err := os.ReadFile(devicePath)
	if err != nil {
		klog.Errorf("Failed to read PCI device ID for %s (PCI %s): %v", port.Netdev, port.PCIAddress, err)
		return false
	}
	id := strings.ToLower(strings.TrimSpace(string(data)))
	if id != "" && !strings.HasPrefix(id, "0x") {
		id = "0x" + id
	}
	_, ok := nsNICDeviceIDs[id]
	return ok
}
