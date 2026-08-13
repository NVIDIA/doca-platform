/*
Copyright 2024 NVIDIA

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

//go:generate mockgen -copyright_file ../../../../../../hack/boilerplate.go.txt -destination mock/Utils.go -source pci.go

package pci

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/common"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/vpd"

	"k8s.io/klog/v2"
	kexec "k8s.io/utils/exec"
)

const (
	// defaultPCIDomain is used when a PCI address does not include a domain
	defaultPCIDomain = "0000"
	// path of the modalis file, used to retrieve info about vendor and deviceID
	modaliasPath = "modalias"
	// this path is used to check that device is VF
	vfCheckPath = "physfn"
	// this path is used to check driver for device
	driverPath = "driver"
	// file path to trigger bind to the driver
	driverBindPath = "bind"
	// file path to trigger unbind from the driver
	driverUnbindPath = "unbind"
	// file path to override the device driver
	driverOverridePath = "driver_override"
	// file path to check current number of SRIOV VFs
	sriovNumVfsPath = "sriov_numvfs"
	// file path to check maximum number of SRIOV VFs
	sriovTotalVfsPath = "sriov_totalvfs"
	// file path used to disable driver autoprobe for VFs
	sriovDriversAutoprobePath = "sriov_drivers_autoprobe"
	// path of the VPD file, used to retrieve the function VUID
	vpdPath = "vpd"
	// VPD field containing the function VUID
	vuidVPDField = "VU"
)

// regexp to validate PCI address, expected form is 0000:3b:00.5
// see https://github.com/jaypipes/ghw/pull/373 for details
var pciAddressRegexp = regexp.MustCompile(
	`^((1?[0-9a-f]{0,4}):)?([0-9a-f]{2}):([0-9a-f]{2})\.([0-9a-f]{1})$`,
)

// used in tests to change fs root
var fsRoot = ""

var (
	// ErrDeviceAlreadyBoundToDifferentDriver error indicates that device is already bound to a different driver
	ErrDeviceAlreadyBoundToDifferentDriver = errors.New("device is already bound to a different driver")
)

// New initialize and return a new instance of pci utils
func New(exec kexec.Interface) Utils {
	return &pciUtils{
		exec: exec,
	}
}

// ParsePCIAddress check that PCI address format is in correct format.
// add domain if the address doesn't include it
// address should be in [DOMAIN]BUS:DEVICE.FUNCTION format (0000:3b:00.2)
func ParsePCIAddress(address string) (string, error) {
	domain, busDeviceFunction, err := parsePCIAddress(address)
	if err != nil {
		return "", err
	}
	if domain == "" {
		domain = defaultPCIDomain
	}
	return domain + ":" + busDeviceFunction, nil
}

func parsePCIAddress(address string) (string, string, error) {
	match := pciAddressRegexp.FindStringSubmatch(address)
	if len(match) == 0 {
		return "", "", fmt.Errorf("invalid PCI address format, expected format is BUS:DEVICE.FUNCTION or DOMAIN:BUS:DEVICE.FUNCTION ")
	}
	return match[2], fmt.Sprintf("%s:%s.%s", match[3], match[4], match[5]), nil
}

// DeviceInfo holds information about PCI device
type DeviceInfo struct {
	Address  string
	VendorID string
	DeviceID string
	Driver   string
}

// ErrNotFound error indicates that device with specified address not found
var ErrNotFound = errors.New("device not found")

// Utils is the interface provided by pci utils package
type Utils interface {
	// LoadDriver tries to load driver for provided device, do nothing if device is already has required driver
	LoadDriver(pciAddress, driver string) error
	// UnloadDriver unload driver for the device if it has one
	UnloadDriver(pciAddress string) error
	// GetPFs return PCI physical functions filtered by vendor and device IDs
	GetPFs(vendor string, deviceIDs []string) ([]DeviceInfo, error)
	// ResolvePCIAddressByVUID resolves the PCI domain by the parent PF VUID
	ResolvePCIAddressByVUID(pciAddress, funcVUID string) (string, error)
	// IsSRIOVEnabled returns true if SR-IOV is enabled for the PF
	IsSRIOVEnabled(pciAddress string) (bool, error)
	// GetSRIOVNumVFs returns current number of SRIOV VFs for PF
	GetSRIOVNumVFs(pciAddress string) (int, error)
	// GetSRIOVTotalVFs returns maximum number of SRIOV VFs for PF
	GetSRIOVTotalVFs(pciAddress string) (int, error)
	// InsertKernelModule inserts kernel module
	InsertKernelModule(ctx context.Context, module string) error
	// DisableSriovVfsDriverAutoprobe disable driver autoprobes for the VFs on the PF
	DisableSriovVfsDriverAutoprobe(pciAddress string) error
	// SetSriovNumVfs configure require amount of VFs on the PF identified by the pciAddress
	SetSriovNumVfs(pciAddress string, count int) error
}

type pciUtils struct {
	exec kexec.Interface
}

// LoadDriver is an Utils interface implementation for pciUtils
func (u *pciUtils) LoadDriver(pciAddress, driver string) error {
	curDriver, err := u.getDeviceDriver(pciAddress)
	if err != nil {
		return fmt.Errorf("failed to read driver for device: %v", err)
	}
	if curDriver != "" {
		if curDriver != driver {
			klog.V(2).InfoS("device is already bound to a different driver", "device", pciAddress, "driver", curDriver)
			return ErrDeviceAlreadyBoundToDifferentDriver
		}
		klog.V(2).InfoS("device already bound to required driver", "device", pciAddress, "driver", driver)
		return nil
	}
	// /sys/bus/pci/devices/0000:b1:00.2/driver_override
	if err := os.WriteFile(filepath.Join(fsRoot, common.SysfsPCIDevsPath, pciAddress, driverOverridePath),
		[]byte(driver), os.ModeAppend); err != nil {
		return fmt.Errorf("failed to configure driver override: %v", err)
	}

	klog.V(2).InfoS("try to bind device to the driver", "device", pciAddress, "driver", driver)
	// /sys/bus/pci/drivers/nvme/bind
	if err := os.WriteFile(filepath.Join(fsRoot, common.SysfsPCIDriverPath, driver, driverBindPath),
		[]byte(pciAddress), os.ModeAppend); err != nil {
		return fmt.Errorf("failed to bind device to the driver: %v", err)
	}
	klog.V(2).InfoS("device bind succeed", "device", pciAddress, "driver", driver)
	return nil
}

// UnloadDriver unload driver for the device if it has one
func (u *pciUtils) UnloadDriver(pciAddress string) error {
	curDriver, err := u.getDeviceDriver(pciAddress)
	if err != nil {
		return fmt.Errorf("failed to read driver for device: %v", err)
	}
	if curDriver == "" {
		klog.V(2).InfoS("device has no driver", "device", pciAddress)
		return nil
	}
	klog.V(2).InfoS("device has driver", "device", pciAddress, "driver", curDriver)
	// /sys/bus/pci/drivers/nvme/unbind
	if err := os.WriteFile(filepath.Join(fsRoot, common.SysfsPCIDriverPath, curDriver, driverUnbindPath),
		[]byte(pciAddress), os.ModeAppend); err != nil {
		return fmt.Errorf("failed to unbind device from the driver: %v", err)
	}
	klog.V(2).InfoS("device unbind succeed", "device", pciAddress, "driver", curDriver)
	return nil
}

// GetPFs is an Utils interface implementation for pciUtils
func (u *pciUtils) GetPFs(vendor string, deviceIDs []string) ([]DeviceInfo, error) {
	var pfs []DeviceInfo
	devFolders, err := os.ReadDir(sysFSDevPath())
	if err != nil {
		return nil, fmt.Errorf("failed to read devices from sysfs: %v", err)
	}
	for _, devFolder := range devFolders {
		devInfo, err := u.getDeviceInfo(devFolder.Name())
		if err != nil {
			// if everything work as expected this should never happen
			klog.ErrorS(err, "failed to read device info", "device", devFolder.Name())
			continue
		}
		if devInfo.VendorID == vendor {
			var deviceIDMatch bool
			for _, allowedDevID := range deviceIDs {
				if devInfo.DeviceID == allowedDevID {
					deviceIDMatch = true
					break
				}
			}
			if !deviceIDMatch {
				continue
			}
			isVF, err := u.isVF(devInfo.Address)
			if err != nil {
				klog.ErrorS(err, "failed to check if device is VF", "device", devFolder.Name())
				continue
			}
			if !isVF {
				pfs = append(pfs, devInfo)
			}
		}
	}
	return pfs, nil
}

// ResolvePCIAddressByVUID resolves the PCI address domain by locating the parent
// PF VUID in sysfs. Any domain in pciAddress is ignored. For VFs, pciAddress
// supplies the VF bus, device, and function.
func (u *pciUtils) ResolvePCIAddressByVUID(pciAddress, funcVUID string) (string, error) {
	if funcVUID == "" {
		return "", fmt.Errorf("function VUID is empty")
	}

	_, busDeviceFunction, err := parsePCIAddress(pciAddress)
	if err != nil {
		return "", err
	}

	domain, err := u.getPCIDomainByVUID(busDeviceFunction, funcVUID)
	if err != nil {
		return "", err
	}

	return domain + ":" + busDeviceFunction, nil
}

// getPCIDomainByVUID finds the PCI domain for busDeviceFunction by matching
// funcVUID against the VU field in sysfs VPD. Candidates are first narrowed
// to devices under /sys/bus/pci/devices whose bus:device.function equals
// busDeviceFunction, so VPD is read only for that small set rather than every
// PCI device. For each candidate it reads VPD from the device itself for PFs
// and from physfn/vpd for VFs, and returns the domain when exactly one
// candidate matches. Missing VPD is skipped; other VPD read errors and
// ambiguous or empty matches fail.
func (u *pciUtils) getPCIDomainByVUID(busDeviceFunction, funcVUID string) (string, error) {
	devFolders, err := os.ReadDir(sysFSDevPath())
	if err != nil {
		return "", fmt.Errorf("failed to read devices from sysfs: %w", err)
	}
	matchingDomains := []string{}
	for _, devFolder := range devFolders {
		domain, candidateBusDeviceFunction, err := parsePCIAddress(devFolder.Name())
		if err != nil {
			klog.V(2).InfoS("Failed to parse PCI address from sysfs", "device", devFolder.Name(), "error", err)
			continue
		}
		if candidateBusDeviceFunction != busDeviceFunction {
			continue
		}
		if domain == "" {
			return "", fmt.Errorf("PCI address %s has no domain", devFolder.Name())
		}

		isVF, err := u.isVF(devFolder.Name())
		if err != nil {
			return "", fmt.Errorf("failed to check PCI device %s: %w", devFolder.Name(), err)
		}
		vpdFile := sysFSDevPath(devFolder.Name(), vpdPath)
		if isVF {
			vpdFile = sysFSDevPath(devFolder.Name(), vfCheckPath, vpdPath)
		}

		vpdData, err := os.ReadFile(vpdFile)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return "", fmt.Errorf("failed to read VPD for PCI device %s: %w", devFolder.Name(), err)
			}
			continue
		}
		parsedVPD, err := vpd.Parse(vpdData)
		if err != nil {
			klog.V(2).InfoS("Failed to parse PCI VPD", "device", devFolder.Name(), "error", err)
		}
		// Parse returns fields decoded before malformed data, so the VUID may
		// still be available when an error is returned.
		vuid, found := parsedVPD.Lookup(vuidVPDField)
		if !found || vuid != funcVUID {
			continue
		}
		matchingDomains = append(matchingDomains, domain)
	}
	if len(matchingDomains) == 1 {
		return matchingDomains[0], nil
	}
	if len(matchingDomains) > 1 {
		return "", fmt.Errorf("multiple PCI domains found for PCI function %s with VUID %s: %s",
			busDeviceFunction, funcVUID, strings.Join(matchingDomains, ", "))
	}
	return "", fmt.Errorf("%w: PCI function %s with VUID %s", ErrNotFound, busDeviceFunction, funcVUID)
}

// DisableSriovVfsDriverAutoprobe disable driver autoprobes for the VFs on the PF
func (u *pciUtils) DisableSriovVfsDriverAutoprobe(pciAddress string) error {
	// disable driver autoprobe for VFs
	// e.g /sys/bus/pci/devices/0000:b1:00.2/sriov_drivers_autoprobe
	return os.WriteFile(filepath.Join(fsRoot, common.SysfsPCIDevsPath, pciAddress, sriovDriversAutoprobePath),
		[]byte("0"), os.ModeAppend)
}

// IsSRIOVEnabled returns true if SR-IOV is enabled for the PF
func (u *pciUtils) IsSRIOVEnabled(pciAddress string) (bool, error) {
	totalVfs, err := u.readSriovVFsCount(pciAddress, sriovTotalVfsPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return totalVfs > 0, nil
}

// SetSriovNumVfs configure require amount of VFs on the PF identified by the pciAddress
func (u *pciUtils) SetSriovNumVfs(pciAddress string, count int) error {
	// e.g /sys/bus/pci/devices/0000:b1:00.2/sriov_numvfs
	return os.WriteFile(filepath.Join(fsRoot, common.SysfsPCIDevsPath, pciAddress, sriovNumVfsPath),
		[]byte(strconv.Itoa(count)), os.ModeAppend)
}

// GetSRIOVNumVFs returns current number of SRIOV VFs for PF
func (u *pciUtils) GetSRIOVNumVFs(pciAddress string) (int, error) {
	return u.readSriovVFsCount(pciAddress, sriovNumVfsPath)
}

// GetSRIOVTotalVFs returns maximum number of SRIOV VFs for PF
func (u *pciUtils) GetSRIOVTotalVFs(pciAddress string) (int, error) {
	return u.readSriovVFsCount(pciAddress, sriovTotalVfsPath)
}

func (u *pciUtils) readSriovVFsCount(pciAddress string, subpath string) (int, error) {
	data, err := os.ReadFile(filepath.Join(fsRoot, common.SysfsPCIDevsPath, pciAddress, subpath))
	if err != nil {
		return 0, fmt.Errorf("failed to read %s for device %s: %w", subpath, pciAddress, err)
	}
	val, err := strconv.Atoi(strings.TrimSuffix(string(data), "\n"))
	if err != nil {
		return 0, fmt.Errorf("failed to convert %s for device %s to int: %v", subpath, pciAddress, err)
	}
	return val, nil
}

// InsertKernelModule is an Utils interface implementation for pciUtils
func (u *pciUtils) InsertKernelModule(ctx context.Context, module string) error {
	args := []string{"modprobe", module}
	cmd := u.exec.CommandContext(ctx, args[0], args[1:]...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to load kernel module %s: %v", module, err)
	}
	return nil
}

// check if provided address is PCI virtual function,
// device is virtual function if it has link to the physical function
func (u *pciUtils) isVF(pciAddress string) (bool, error) {
	_, err := os.Stat(sysFSDevPath(pciAddress, vfCheckPath))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("failed to check if PCI dev is VF: %v", err)
	}
	return true, nil
}

func (u *pciUtils) getDeviceInfo(pciAddress string) (DeviceInfo, error) {
	_, err := os.Stat(sysFSDevPath(pciAddress))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DeviceInfo{}, ErrNotFound
		}
		return DeviceInfo{}, fmt.Errorf("failed to check device path in sysfs: %v", err)
	}
	d, err := os.ReadFile(sysFSDevPath(pciAddress, modaliasPath))
	if err != nil {
		return DeviceInfo{}, fmt.Errorf("failed to read modalias file for device")
	}
	data := string(d)
	if len(data) < 23 {
		return DeviceInfo{}, fmt.Errorf("unexpected modalias file format")
	}
	// modalias file example
	// pci:v000015B3d00006001sv00000000sd00000000bc01sc08i02
	vendorID := strings.ToLower(data[9:13])
	productID := strings.ToLower(data[18:22])

	driver, err := u.getDeviceDriver(pciAddress)
	if err != nil {
		return DeviceInfo{}, fmt.Errorf("failed to read driver for device: %v", err)
	}
	return DeviceInfo{
		Address:  pciAddress,
		VendorID: vendorID,
		DeviceID: productID,
		Driver:   driver}, nil
}

func (u *pciUtils) getDeviceDriver(pciAddress string) (string, error) {
	return u.readSysFSLink(pciAddress, driverPath)
}

func (u *pciUtils) readSysFSLink(pciAddress, path string) (string, error) {
	_, err := os.Stat(sysFSDevPath(pciAddress, path))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	link, err := os.Readlink(sysFSDevPath(pciAddress, path))
	if err != nil {
		return "", err
	}
	return filepath.Base(link), nil
}

func sysFSDevPath(paths ...string) string {
	return filepath.Join(fsRoot, common.SysfsPCIDevsPath, filepath.Join(paths...))
}
