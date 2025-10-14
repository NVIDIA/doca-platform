/*
Copyright 2025 NVIDIA

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
	"strconv"
	"strings"

	"k8s.io/klog/v2"
)

const (
	SysPCIDevicesDir = "/sys/bus/pci/devices"
)

var (
	knownDPUDeviceID = map[string]struct{}{
		"0xa2dc": {}, // BlueField-3
		"0xa2d6": {}, // BlueField-2
	}
)

type PCIHelper struct {
	// PCIAddress is the PCI address of the device with optional function number,
	// e.g. "0000:4d:00.0" or "0000:4d:00".
	PCIAddress string
	// PFIndex is the index of the PF, e.g. 0 for PF0, 1 for PF1, etc. If PFIndex is not set,
	// the helper use the function number in PCIAddress to determine the PF.
	PFIndex *int
	// VFIndex is the index of the VF, e.g. 0 for VF0, 1 for VF1, etc.
	VFIndex *int
}

func NewPCIHelper(pciAddress string) *PCIHelper {
	// PCI address in vf-config is in the format of "0000-4d-00", convert it to "0000:4d:00"
	pciAddress = strings.ReplaceAll(pciAddress, "-", ":")
	parts := strings.Split(pciAddress, ":")
	if len(parts) == 2 {
		pciAddress = "0000:" + pciAddress
	}
	h := &PCIHelper{
		PCIAddress: pciAddress,
	}
	return h
}

func (h *PCIHelper) PF(pf int) *PCIHelper {
	h.PFIndex = &pf
	return h
}

func (h *PCIHelper) VF(vf int) *PCIHelper {
	h.VFIndex = &vf
	return h
}

// Path returns the full path to the PCI device.
func (h *PCIHelper) Path() string {
	parts := strings.Split(h.PCIAddress, ".")
	base := parts[0]
	suffix := "0"
	if len(parts) == 2 {
		suffix = parts[1]
	}
	if h.PFIndex != nil {
		suffix = fmt.Sprintf("%d", *h.PFIndex)
	}
	p := filepath.Join(SysPCIDevicesDir, fmt.Sprintf("%s.%s", base, suffix))
	if h.VFIndex != nil {
		p = filepath.Join(p, fmt.Sprintf("virtfn%d", *h.VFIndex))
	}
	return p
}

// IsDPU checks if the device is a DPU.
func (h *PCIHelper) IsDPU() (bool, error) {
	deviceID, err := os.ReadFile(filepath.Join(h.Path(), "device"))
	if err != nil {
		return false, err
	}
	_, ok := knownDPUDeviceID[strings.TrimSpace(string(deviceID))]
	return ok, nil
}

// InterfaceName returns the name of the network interface of the device.
func (h *PCIHelper) InterfaceName() (string, error) {
	p := filepath.Join(h.Path(), "net")
	entries, err := os.ReadDir(p)
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "", fmt.Errorf("no network interface found in %s", p)
	}
	return entries[0].Name(), nil
}

func (h *PCIHelper) SerialNumber() (string, error) {
	vpdData, err := os.ReadFile(filepath.Join(h.Path(), "vpd"))
	if err != nil {
		return "", err
	}
	return parseVPDSerialNumber(vpdData), nil
}

func (h *PCIHelper) SetNumOfVFs(num int) error {
	numvfsPath := filepath.Join(h.Path(), "sriov_numvfs")
	if _, err := os.Stat(numvfsPath); err != nil {
		return fmt.Errorf("failed to stat sriov_numvfs path: %w", err)
	}
	return os.WriteFile(numvfsPath, []byte(fmt.Sprintf("%d", num)), 0644)
}

// GetMTU returns the current MTU of the PF interface
func (h *PCIHelper) GetMTU() (int, error) {
	interfaceName, err := h.InterfaceName()
	if err != nil {
		return 0, fmt.Errorf("failed to get interface name: %w", err)
	}

	mtuPath := filepath.Join("/sys/class/net", interfaceName, "mtu")
	mtuBytes, err := os.ReadFile(mtuPath)
	if err != nil {
		return 0, fmt.Errorf("failed to read MTU for interface %s: %w", interfaceName, err)
	}
	mtuStr := strings.TrimSpace(string(mtuBytes))
	mtu, err := strconv.Atoi(mtuStr)
	if err != nil {
		return 0, fmt.Errorf("failed to parse MTU %s for interface %s: %w", mtuStr, interfaceName, err)
	}
	return mtu, nil
}

// ReadDeviceSerialNumberFromVPD reads the serial number from a PCI device's VPD data
// via sysfs configuration space. The pciAddress should be in format like "0000:4d:00.0"
func ReadDeviceSerialNumberFromVPD(pciAddress string) (string, error) {
	vpdPath := filepath.Join(SysPCIDevicesDir, pciAddress, "vpd")
	vpdData, err := os.ReadFile(vpdPath)
	if err != nil {
		return "", err
	}
	return parseVPDSerialNumber(vpdData), nil
}

// parseVPDSerialNumber extracts the serial number from VPD data.
// This function is inspired by cap_vpd() from lspci, see https://github.com/pciutils/pciutils/blob/master/ls-vpd.c#L97
func parseVPDSerialNumber(vpdData []byte) string {
	i := 0
	for i < len(vpdData) {
		tag := vpdData[i]
		var resLen int
		if tag&0x80 > 0 {
			// large resource type (the most significant bit is 1):
			// byte 0: large item name
			// byte 1: length of data items bits[7:0] (lsb)
			// byte 2: length of data items bits[15:8] (msb)
			// byte 3 to n: actual data items
			if i > len(vpdData)-3 {
				return ""
			}
			resLen = int(vpdData[i+1]) | (int(vpdData[i+2]) << 8)
			i += 3
		} else {
			// small resource type (the most significant bit is 0):
			// byte 0: bits[0:3] for length, bits[4:7] for small item name
			// byte 1 to n: actual information
			resLen = int(tag & 7)
			tag >>= 3
			i++
		}
		if resLen > len(vpdData)-i {
			return ""
		}
		switch tag {
		case 0x0f:
			// End of VPD
			return ""
		case 0x90, 0x91:
			// iterate through the read-only and read-write lists to search serial number
			partPos := 0
			for partPos+3 <= resLen {
				id := string(vpdData[i+partPos : i+partPos+2])
				partLen := int(vpdData[i+partPos+2])
				partPos += 3
				if partLen > resLen-partPos {
					break
				}
				switch id {
				case "SN":
					return strings.TrimSpace(string(vpdData[i+partPos : i+partPos+partLen]))
				}
				partPos += partLen
			}
		}
		i += resLen
	}
	return ""
}

type Device struct {
	// Address is the PCI address of the device after truncating the function number, e.g. "0000:4d:00"
	Address string
	// SerialNumber is the serial number of the device
	SerialNumber string
	// NumOfPFs is the number of PFs of the device
	NumOfPFs int
}

func DiscoverDPUs() ([]Device, error) {
	ret := []Device{}
	devices := make(map[string]*Device)
	deviceEntries, err := os.ReadDir(SysPCIDevicesDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read PCI sys directory: %w", err)
	}

	for _, entry := range deviceEntries {
		pciAddr := entry.Name()
		isDPU, err := NewPCIHelper(pciAddr).IsDPU()
		if err != nil {
			klog.Errorf("failed to check if %s is a DPU: %v", pciAddr, err)
			continue
		} else if !isDPU {
			continue
		}
		sn, err := NewPCIHelper(pciAddr).SerialNumber()
		if err != nil {
			klog.Errorf("failed to read device serial number from VPD %s: %v", pciAddr, err)
			continue
		}
		truncatedAddr := truncateFunctionNumber(pciAddr)
		dev, ok := devices[truncatedAddr]
		if ok {
			dev.NumOfPFs++
		} else {
			dev = &Device{
				Address:      truncatedAddr,
				SerialNumber: sn,
				NumOfPFs:     1,
			}
			devices[dev.Address] = dev
		}
	}
	for _, dev := range devices {
		ret = append(ret, *dev)
	}
	return ret, nil
}

func truncateFunctionNumber(pciAddr string) string {
	i := strings.LastIndex(pciAddr, ".")
	if i != -1 {
		return pciAddr[:i]
	}
	return pciAddr
}
