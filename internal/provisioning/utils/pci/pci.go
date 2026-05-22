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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"

	"k8s.io/klog/v2"
)

const (
	pciSlotNameKey = "PCI_SLOT_NAME"
	//nolint:misspell // devlink API key uses British spelling "flavour"
	flavourPhysical = "physical"
)

var (
	sysfsNetPath   = "/sys/class/net"
	mstDevicesPath = "/dev/mst"
)

var mstPCIAddressRegex = regexp.MustCompile(`domain:bus:dev\.fn=([0-9a-fA-F]{4}:[0-9a-fA-F]{2}:[0-9a-fA-F]{2}\.[0-9]+)`)

// NormalizeAddress normalizes a PCI address for comparisons.
func NormalizeAddress(address string) string {
	address = strings.ToLower(strings.TrimSpace(address))
	parts := strings.Split(address, ":")
	if len(parts) == 2 {
		return "0000:" + address
	}
	return address
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

// NICPort describes a single physical NIC port discovered on the DPU.
type NICPort struct {
	Netdev     string // network interface name, e.g. "p0", "p1"
	PCIAddress string // full PCI BDF, e.g. "0002:01:00.0"
	MSTDevice  string // MST device path, e.g. "/dev/mst/mt41695_pciconf0"
}

// PortDiscoverer discovers physical NIC ports on the DPU by joining devlink,
// sysfs uevent, and MST device data via PCI address.
type PortDiscoverer struct {
	runBash bash.RunFunc
}

// DefaultPortDiscoverer is a PortDiscoverer with production defaults.
var DefaultPortDiscoverer = &PortDiscoverer{}

func (d *PortDiscoverer) run(cmd string) (string, string, error) {
	runBash := d.runBash
	if runBash == nil {
		runBash = bash.Run
	}
	stdout, stderr, err := runBash(cmd)
	return stdout.String(), stderr.String(), err
}

// DiscoverPorts discovers physical NIC ports by joining devlink, sysfs uevent,
// and MST device data via PCI address, then applies the given filter.
// Pass nil to return all discovered ports without filtering.
func (d *PortDiscoverer) DiscoverPorts(filter func(NICPort) bool) ([]NICPort, error) {
	// Step a: devlink → physical netdevs
	physicalNetdevs, err := d.DevlinkPorts(PhysicalPortFilter)
	if err != nil {
		return nil, fmt.Errorf("discover physical netdevs: %w", err)
	}
	// Step a': netdev → PCI address via uevent
	netdevToPCI := make(map[string]string, len(physicalNetdevs))
	for _, netdev := range physicalNetdevs {
		pci, err := NetdevPCI(netdev)
		if err != nil {
			return nil, fmt.Errorf("get PCI address for netdev %s: %w", netdev, err)
		}
		if pci == "" {
			klog.Infof("Skipping physical netdev %s with no PCI address", netdev)
			continue
		}
		netdevToPCI[netdev] = pci
	}

	// Step b: MST devices → PCI address
	pciToMST, err := d.listMSTFiles()
	if err != nil {
		return nil, fmt.Errorf("discover MST devices: %w", err)
	}

	// Step c: join by PCI address
	ports := make([]NICPort, 0, len(netdevToPCI))
	for netdev, pci := range netdevToPCI {
		mstDev := pciToMST[pci]
		if mstDev == "" {
			klog.Infof("No MST device found for netdev %s (PCI %s), skipping", netdev, pci)
			continue
		}
		port := NICPort{
			Netdev:     netdev,
			PCIAddress: pci,
			MSTDevice:  mstDev,
		}
		if filter != nil && !filter(port) {
			klog.Infof("Filtered out port %s (PCI %s)", netdev, pci)
			continue
		}
		ports = append(ports, port)
	}
	return ports, nil
}

// devlinkPortShowJSON is the structure of "devlink port show -j" output.
type devlinkPortShowJSON struct {
	Port map[string]DevlinkPortEntry `json:"port"`
}

// DevlinkPortEntry is one port entry in devlink port show JSON.
type DevlinkPortEntry struct {
	Netdev string `json:"netdev"`
	//nolint:misspell // devlink API key is British spelling "flavour"
	Flavor string `json:"flavour"`
}

// DevlinkPortFilterFunc is a predicate for filtering devlink port entries.
type DevlinkPortFilterFunc func(DevlinkPortEntry) bool

// PhysicalPortFilter returns true for devlink ports with flavour "physical".
func PhysicalPortFilter(e DevlinkPortEntry) bool {
	return strings.EqualFold(strings.TrimSpace(e.Flavor), flavourPhysical)
}

// DevlinkPorts runs "devlink port show -j" and returns the netdev names of
// ports matching the given filter. Pass nil to return all ports.
func (d *PortDiscoverer) DevlinkPorts(filter DevlinkPortFilterFunc) ([]string, error) {
	stdout, stderr, err := d.run("devlink port show -j")
	if err != nil {
		return nil, fmt.Errorf("devlink port show -j: %w (stderr: %s)", err, stderr)
	}
	var parsed devlinkPortShowJSON
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		return nil, fmt.Errorf("devlink port show: parse JSON: %w", err)
	}
	if parsed.Port == nil {
		return nil, fmt.Errorf("devlink port show: missing \"port\" object")
	}
	netdevs := make([]string, 0, len(parsed.Port))
	for key, entry := range parsed.Port {
		if filter != nil && !filter(entry) {
			continue
		}
		netdev := strings.TrimSpace(entry.Netdev)
		if netdev == "" {
			return nil, fmt.Errorf("devlink port %s has no netdev", key)
		}
		netdevs = append(netdevs, netdev)
	}
	return netdevs, nil
}

func (d *PortDiscoverer) listMSTFiles() (map[string]string, error) {
	_, stderr, err := d.run("mst start")
	if err != nil {
		return nil, fmt.Errorf("failed to start mst: %w, stderr: %s", err, stderr)
	}

	devices, err := filepath.Glob(filepath.Join(mstDevicesPath, "*"))
	if err != nil {
		return nil, fmt.Errorf("failed to list MST devices: %w", err)
	}
	if len(devices) == 0 {
		return nil, fmt.Errorf("no MST devices found in %s", mstDevicesPath)
	}

	pciToMST := make(map[string]string, len(devices))
	for _, device := range devices {
		pci, err := pciAddressFromMSTFile(device)
		if err != nil {
			return nil, err
		}
		pciToMST[pci] = device
	}
	return pciToMST, nil
}

func pciAddressFromMSTFile(mstFile string) (string, error) {
	content, err := os.ReadFile(mstFile)
	if err != nil {
		return "", fmt.Errorf("failed to read MST device %s: %w", mstFile, err)
	}
	// Example MST device content:
	// /dev/mst/mt41692_pciconf0 - PCI configuration cycles access.
	//                            domain:bus:dev.fn=0000:03:00.0 addr.reg=88 data.reg=92 cr_bar.gw_offset=-1
	matches := mstPCIAddressRegex.FindSubmatch(content)
	if len(matches) != 2 {
		return "", fmt.Errorf("failed to parse PCI address from MST device %s", mstFile)
	}
	return string(matches[1]), nil
}
