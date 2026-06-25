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

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
)

const (
	pciSlotNameKey = "PCI_SLOT_NAME"
	//nolint:misspell // devlink API key uses British spelling "flavour"
	flavourPhysical = "physical"
	//nolint:misspell // devlink API key uses British spelling "flavour"
	flavourPCIPF = "pcipf"

	// Supported BlueField N/S NIC PCI device IDs as advertised in https://admin.pci-ids.ucw.cz/read/PC/15b3
	bluefield2DeviceID = "0xa2d6"
	bluefield3DeviceID = "0xa2dc"
	bluefield4DeviceID = "0xa2df"
)

var (
	sysfsNetPath        = "/sys/class/net"
	sysfsPCIDevicesPath = "/sys/bus/pci/devices"
	mstDevicesPath      = "/dev/mst"
)

var mstPCIAddressRegex = regexp.MustCompile(`domain:bus:dev\.fn=([0-9a-fA-F]{4}:[0-9a-fA-F]{2}:[0-9a-fA-F]{2}\.[0-9]+)`)

var nsNICDeviceIDs = sets.New(bluefield2DeviceID, bluefield3DeviceID, bluefield4DeviceID)

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

// NSPortFilter returns true for ports backed by a known N/S NIC device ID.
func NSPortFilter(port *NICPort) bool {
	return nsNICDeviceIDs.Has(port.DeviceID)
}

func pciDeviceID(pciAddress string) (string, error) {
	devicePath := filepath.Join(sysfsPCIDevicesPath, NormalizeAddress(pciAddress), "device")
	data, err := os.ReadFile(devicePath)
	if err != nil {
		return "", fmt.Errorf("failed to read PCI device ID for PCI %s: %w", pciAddress, err)
	}
	id := strings.ToLower(strings.TrimSpace(string(data)))
	if id != "" && !strings.HasPrefix(id, "0x") {
		id = "0x" + id
	}
	return id, nil
}

// NICPort describes a single physical NIC port discovered on the DPU.
type NICPort struct {
	// Netdev is the physical port network interface name, e.g. "p0", "p1".
	Netdev string
	// PCIAddress is the full ECPF PCI BDF, e.g. "0002:01:00.0".
	PCIAddress string
	// DeviceID is the ECPF PCI device ID, e.g. "0xa2df".
	DeviceID string
	// MSTDevice is the MST device path, e.g. "/dev/mst/mt41695_pciconf0".
	MSTDevice string
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

// DiscoverPhysicalPort discovers physical ports by joining devlink physical entries,
// sysfs PCI uevent data, and MST device data via PCI address. Pass nil to return
// all discovered physical ports.
func (d *PortDiscoverer) DiscoverPhysicalPort(filter func(*NICPort) bool) ([]NICPort, error) {
	// Step a: devlink → physical netdevs
	devlinkPorts, err := d.DevlinkPortEntries()
	if err != nil {
		return nil, err
	}
	// Step a': netdev → PCI address via uevent
	netdevToPCI := make(map[string]string, len(devlinkPorts))
	for key, entry := range devlinkPorts {
		if !PhysicalPortFilter(entry) {
			continue
		}
		netdev := strings.TrimSpace(entry.Netdev)
		if netdev == "" {
			return nil, fmt.Errorf("discover physical netdevs: devlink port %s has no netdev", key)
		}
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
		deviceID, err := pciDeviceID(port.PCIAddress)
		if err != nil {
			return nil, err
		}
		port.DeviceID = deviceID
		if filter != nil && !filter(&port) {
			klog.Infof("Filtered out port %s (PCI %s)", netdev, pci)
			continue
		}
		ports = append(ports, port)
	}
	return ports, nil
}

// DiscoverNSPFRepresentors discovers host PF representor netdevs for all N/S ECPFs.
func (d *PortDiscoverer) DiscoverNSPFRepresentors() ([]string, error) {
	devlinkPorts, err := d.DevlinkPortEntries()
	if err != nil {
		return nil, err
	}

	pfReps := make([]string, 0, len(devlinkPorts))
	for key, entry := range devlinkPorts {
		if !strings.EqualFold(strings.TrimSpace(entry.Flavor), flavourPCIPF) {
			continue
		}
		addressWithPortID, ok := strings.CutPrefix(key, "pci/")
		if !ok {
			continue
		}
		pciAddress, _, ok := strings.Cut(addressWithPortID, "/")
		if !ok {
			continue
		}
		deviceID, err := pciDeviceID(pciAddress)
		if err != nil {
			return nil, err
		}
		if !nsNICDeviceIDs.Has(deviceID) {
			continue
		}
		netdev := strings.TrimSpace(entry.Netdev)
		if netdev == "" {
			klog.Warningf("Skipping pcipf devlink port %s with no netdev", key)
			continue
		}
		// Each ECPF has exactly one host PF representor.
		pfReps = append(pfReps, netdev)
	}
	return pfReps, nil
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

// PhysicalPortFilter returns true for devlink ports with flavour "physical".
func PhysicalPortFilter(e DevlinkPortEntry) bool {
	return strings.EqualFold(strings.TrimSpace(e.Flavor), flavourPhysical)
}

// DevlinkPortEntries runs "devlink port show -j" and returns the parsed devlink port map.
func (d *PortDiscoverer) DevlinkPortEntries() (map[string]DevlinkPortEntry, error) {
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
	return parsed.Port, nil
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
