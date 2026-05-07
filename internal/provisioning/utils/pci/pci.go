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

package pci

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	pciSlotNameKey = "PCI_SLOT_NAME"
)

var sysfsNetPath = "/sys/class/net"

// NormalizeAddress normalizes a PCI address for comparisons.
func NormalizeAddress(address string) string {
	address = strings.ToLower(strings.TrimSpace(address))
	parts := strings.Split(address, ":")
	if len(parts) == 2 {
		return "0000:" + address
	}
	return address
}

// AddressSet returns a set of normalized PCI addresses.
func AddressSet(addresses []string) map[string]struct{} {
	out := make(map[string]struct{}, len(addresses))
	for _, address := range addresses {
		address = NormalizeAddress(address)
		if address == "" {
			continue
		}
		out[address] = struct{}{}
	}
	return out
}

// NetdevPCI returns the normalized PCI address backing a netdev.
func NetdevPCI(netdev string) (string, error) {
	ueventPath := filepath.Join(sysfsNetPath, netdev, "device", "uevent")
	data, err := os.ReadFile(ueventPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to read uevent for netdev %s: %w", netdev, err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, found := strings.Cut(line, "=")
		if !found || key != pciSlotNameKey {
			continue
		}
		return NormalizeAddress(value), nil
	}
	return "", nil
}
