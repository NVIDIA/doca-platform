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

//go:generate mockgen -copyright_file ../../../../../../hack/boilerplate.go.txt -destination mock/Utils.go -source nvme.go

package nvme

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/common"

	"golang.org/x/sys/unix"
	"k8s.io/klog/v2"
)

const (
	sysfsBlockPath = "/sys/class/block"
	// sysfsDevBlockPath contains a <major>:<minor> symlink for every block device
	sysfsDevBlockPath = "/sys/dev/block"
)

var (
	// ErrBlockDeviceNotFound is returned when block device is not found
	ErrBlockDeviceNotFound = fmt.Errorf("block device not found")
	// ErrBlockDeviceIsInvalid is returned when block device is invalid, for example, if it has zero size
	ErrBlockDeviceIsInvalid = fmt.Errorf("block device is invalid")
	// Native NVMe multipath names a hidden path nvme<subsystem-instance>c<controller-instance>n<head-instance>
	// and its usable namespace head nvme<subsystem-instance>n<head-instance>.
	nativeMultipathPathRegexp = regexp.MustCompile(`^nvme([0-9]+)c[0-9]+n([0-9]+)$`)
)

// used in tests to change fs root
var fsRoot = ""

// Utils is the interface provided by nvme utils package
type Utils interface {
	// GetBlockDeviceNameForNS return block device name for NVME namespace
	// address - PCI device address,
	// namespace - NVME namespace id
	// return ErrBlockDeviceNotFound error if device not found
	GetBlockDeviceNameForNS(address string, namespace int32) (string, error)
	// GetPCIAddressForBlockDevicePath returns the PCI address of the NVME function that backs the block device
	// node available at the path, for example a bind mounted /dev/nvme0n1
	// return ErrBlockDeviceNotFound error if the path is not an NVME block device of a PCI function
	GetPCIAddressForBlockDevicePath(path string) (string, error)
}

// New initialize and return a new instance of nvme utils
func New() Utils {
	return &nvmeUtils{}
}

type nvmeUtils struct{}

// GetBlockDeviceNameForNS is an Utils interface implementation for nvmeUtils
func (n *nvmeUtils) GetBlockDeviceNameForNS(deviceID string, namespace int32) (string, error) {
	devName, deviceSize, err := n.getBlockDeviceInfo(deviceID, namespace)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("failed to get block device name for dev %s ns %d: %v", deviceID, namespace, err)
	}
	if err != nil || devName == "" {
		klog.V(3).InfoS("block device not found", "deviceID", deviceID, "namespace", namespace)
		return "", ErrBlockDeviceNotFound
	}
	if deviceSize == 0 {
		klog.V(3).InfoS("block device has zero size", "deviceID", deviceID, "namespace", namespace, "block_device", devName)
		return "", ErrBlockDeviceIsInvalid
	}
	klog.V(2).InfoS("found block device name for NVME namespace", "deviceID", deviceID, "namespace",
		namespace, "block_device", devName, "size_sectors", deviceSize)
	return devName, nil
}

// GetPCIAddressForBlockDevicePath is an Utils interface implementation for nvmeUtils
func (n *nvmeUtils) GetPCIAddressForBlockDevicePath(path string) (string, error) {
	var st unix.Stat_t
	if err := unix.Stat(path, &st); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", ErrBlockDeviceNotFound
		}
		return "", fmt.Errorf("failed to stat %s: %w", path, err)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFBLK {
		return "", ErrBlockDeviceNotFound
	}
	// Rdev is uint64 on Linux but int32 on macOS, the conversion keeps the package building on both
	rdev := uint64(st.Rdev) //nolint:unconvert
	return n.getPCIAddressForDevNum(unix.Major(rdev), unix.Minor(rdev))
}

// getPCIAddressForDevNum returns the PCI address of the NVME function that owns the block device with the device number
func (n *nvmeUtils) getPCIAddressForDevNum(major, minor uint32) (string, error) {
	// e.g. /sys/dev/block/259:1 -> ../../devices/pci0000:b0/0000:b0:02.0/0000:b1:0c.2/nvme/nvme3/nvme3n1
	link, err := os.Readlink(filepath.Join(fsRoot, sysfsDevBlockPath, fmt.Sprintf("%d:%d", major, minor)))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", ErrBlockDeviceNotFound
		}
		return "", fmt.Errorf("failed to resolve block device %d:%d: %w", major, minor, err)
	}
	return n.getPCIAddressForBlockDevice(filepath.Base(link))
}

// getPCIAddressForBlockDevice returns the PCI address of the NVME function that owns the block device
func (n *nvmeUtils) getPCIAddressForBlockDevice(deviceName string) (string, error) {
	// e.g. /sys/bus/pci/drivers/nvme/0000:3b:00.6/nvme/nvme3/nvme2n1
	devices, err := filepath.Glob(filepath.Join(fsRoot, common.SysfsPCIDriverPath, common.NVMEDriver, "*", common.NVMEDriver, "*", "*"))
	if err != nil {
		return "", fmt.Errorf("failed to list NVME block devices: %w", err)
	}
	deviceName = normalizeBlockDeviceName(deviceName)
	for _, device := range devices {
		if normalizeBlockDeviceName(filepath.Base(device)) != deviceName {
			continue
		}
		// the device is a child of <pci address>/nvme/<controller>
		return filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(device)))), nil
	}
	return "", ErrBlockDeviceNotFound
}

// normalizeBlockDeviceName returns the namespace head name for a native multipath path device name,
// other names are returned as is
func normalizeBlockDeviceName(name string) string {
	if matches := nativeMultipathPathRegexp.FindStringSubmatch(name); matches != nil {
		return fmt.Sprintf("nvme%sn%s", matches[1], matches[2])
	}
	return name
}

// getBlockDeviceInfo returns block device name and device size in sectors
func (n *nvmeUtils) getBlockDeviceInfo(deviceID string, namespace int32) (string, uint64, error) {
	// e.g. /sys/bus/pci/drivers/nvme/0000:3b:00.6/nvme
	devDriverPath := filepath.Join(fsRoot, common.SysfsPCIDriverPath, common.NVMEDriver, deviceID, common.NVMEDriver)
	ctrlDirs, err := os.ReadDir(devDriverPath)
	if err != nil {
		return "", 0, fmt.Errorf("failed to read device info: %w", err)
	}
	if len(ctrlDirs) == 0 {
		return "", 0, fmt.Errorf("can't find NVME controller dir")
	}
	// e.g. /sys/bus/pci/drivers/nvme/0000:3b:00.6/nvme/nvme3
	// assumption is that each VF will have only one NVME controller
	ctrlDirPath := filepath.Join(devDriverPath, ctrlDirs[0].Name())
	dirs, err := os.ReadDir(ctrlDirPath)
	if err != nil {
		return "", 0, err
	}
	var deviceName string
	var deviceSize uint64
	for _, d := range dirs {
		// e.g. /sys/bus/pci/drivers/nvme/0000:3b:00.6/nvme/nvme3/nvme2n1
		devicePath := filepath.Join(ctrlDirPath, d.Name())
		nsIDData, err := os.ReadFile(filepath.Join(devicePath, "nsid"))
		if err != nil {
			continue
		}
		nsid, err := strconv.ParseInt(strings.TrimSuffix(string(nsIDData), "\n"), 10, 32)
		if err != nil {
			continue
		}
		if int32(nsid) != namespace {
			continue
		}
		deviceName = normalizeBlockDeviceName(d.Name())
		devicePath = filepath.Join(fsRoot, sysfsBlockPath, deviceName)
		deviceSizeData, err := os.ReadFile(filepath.Join(devicePath, "size"))
		if err != nil {
			return "", 0, fmt.Errorf("failed to read block device %s size: %w", deviceName, err)
		}
		deviceSize, err = strconv.ParseUint(strings.TrimSuffix(string(deviceSizeData), "\n"), 10, 64)
		if err != nil {
			klog.V(3).InfoS("size parameter for block device has unexpected format, return zero size", "block_device", deviceName)
			deviceSize = 0
			break
		}
	}
	return deviceName, deviceSize, nil
}
