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
	"strconv"
	"strings"

	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/common"

	"k8s.io/klog/v2"
)

var (
	// ErrBlockDeviceNotFound is returned when block device is not found
	ErrBlockDeviceNotFound = fmt.Errorf("block device not found")
	// ErrBlockDeviceIsInvalid is returned when block device is invalid, for example, if it has zero size
	ErrBlockDeviceIsInvalid = fmt.Errorf("block device is invalid")
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
		deviceName = d.Name()
		deviceSizeData, err := os.ReadFile(filepath.Join(devicePath, "size"))
		if err != nil {
			klog.V(3).InfoS("can't read size for block device, return zero size", "block_device", deviceName)
			deviceSize = 0
			break
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
