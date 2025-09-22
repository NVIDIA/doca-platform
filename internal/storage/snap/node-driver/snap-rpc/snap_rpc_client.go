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

package rpcclient

import (
	"fmt"
	"time"

	snapstoragev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"

	"k8s.io/klog/v2"
)

// hotplugCreationDelay is the duration to wait after attaching (plugging) an NVMe device
const hotplugAttachDelay = 5 * time.Second

// Client defines the interface for managing device operations
type Client interface {
	// ExposeBlockDevice exposes a block device on the SNAP controller
	ExposeBlockDevice(deviceName string, dpuStatus snapstoragev1.VolumeAttachmentStatusDPU,
		parameters map[string]string, hotplug bool) (int, string, string, error)
	// ExposeFSDevice exposes a filesystem device on the SNAP controller
	ExposeFSDevice(deviceName string, dpuStatus snapstoragev1.VolumeAttachmentStatusDPU,
		parameters map[string]string) (string, string, error)
	// DestroyBlockDevice destroys a block device on the SNAP controller
	DestroyBlockDevice(nsid int, pciAddr string, hotplug bool) error
	// DestroyFSDevice destroys a filesystem device on the SNAP controller
	DestroyFSDevice(deviceName string, pciAddr string) error
	// Close closes the underlying RPC connection
	Close() error
}

// NewClient returns a new Client instance
// Note: client is not thread-safe, so it should be used in a single thread
func NewClient(rpcClient JSONRPCClient) Client {
	return &client{
		rpcClient: rpcClient,
	}
}

// client is default implementation of the Client interface
type client struct {
	rpcClient JSONRPCClient
}

// ExposeBlockDevice exposes a block device on the SNAP controller
func (c *client) ExposeBlockDevice(deviceName string, dpuStatus snapstoragev1.VolumeAttachmentStatusDPU,
	parameters map[string]string, hotplug bool) (int, string, string, error) {
	if hotplug && dpuStatus.PCIDeviceAddress == "" {
		vuid, err := NvmeEmulationDeviceAttach(c.rpcClient)
		if err != nil {
			return 0, "", "", fmt.Errorf("failed to attach NVMe emulation device: %v", err)
		}

		if parameters == nil {
			parameters = make(map[string]string)
		}
		parameters["vuid"] = vuid
	}

	subsystems, err := NvmeSubsystemList(c.rpcClient)
	if err != nil {
		return 0, "", "", fmt.Errorf("failed to retrieve NVMe subsystems: %v", err)
	}

	nsid := getNamespaceByDeviceName(deviceName, subsystems)

	var currUUID string
	if nsid == -1 {
		nsid, currUUID, err = NvmeNamespaceCreate(c.rpcClient, deviceName, subsystems, dpuStatus)
		if err != nil {
			return 0, "", "", fmt.Errorf("failed to create namespace: %v", err)
		}
		klog.Infof("Created new namespace: NSID=%d, UUID=%s", nsid, currUUID)
	} else {
		klog.Infof("Namespace already exists: NSID=%d", nsid)
	}

	ctrlID := getCtrlByDeviceName(deviceName, subsystems)
	if err != nil {
		klog.Errorf("Error retrieving NVMe controllers: %v", err)
		return nsid, "", currUUID, err
	}

	emulationFunctions, err := EmulationFunctionList(c.rpcClient)
	if err != nil {
		klog.Errorf("Failed to retrieve emulation functions: %v", err)
		return 0, "", "", fmt.Errorf("failed to retrieve emulation functions: %v", err)
	}

	var pciBDF string
	if ctrlID != "" {
		klog.Infof("Controller already exists: ID=%s", ctrlID)
		pciBDF, err = getPciAddrByCtrlID(ctrlID, emulationFunctions, hotplug)
		if err != nil {
			return nsid, "", currUUID, fmt.Errorf("failed to get PCI BDF for controller: %v", err)
		}
	} else {
		ctrlID, pciBDF, err = NvmeControllerCreate(c.rpcClient, subsystems, emulationFunctions, dpuStatus, parameters)
		if err != nil {
			return nsid, "", currUUID, fmt.Errorf("failed to create controller: %v", err)
		}

		isAttached := isControllerAttachedToNamespace(ctrlID, nsid, subsystems)
		if isAttached {
			klog.Infof("Controller %s is already attached to namespace %d, skipping attachment", ctrlID, nsid)
		} else {
			err = NvmeControllerAttachNs(c.rpcClient, ctrlID, nsid)
			if err != nil {
				return nsid, pciBDF, currUUID, fmt.Errorf("failed to attach namespace to controller: %v", err)
			}
		}

		err = NvmeControllerResume(c.rpcClient, ctrlID)
		if err != nil {
			return nsid, pciBDF, currUUID, fmt.Errorf("failed to resume controller: %v", err)
		}

		klog.Infof("Created new controller: ID=%s, PCI BDF=%s", ctrlID, pciBDF)
	}

	attempts := 0
	maxAttempts := 10
	for pciBDF == "00:00.0" && attempts < maxAttempts {
		time.Sleep(hotplugAttachDelay)
		klog.Infof("Waiting for PCI BDF to be available")
		emulationFunctions, err := EmulationFunctionList(c.rpcClient)
		if err != nil {
			klog.Errorf("Failed to retrieve emulation functions: %v", err)
			return 0, "", "", fmt.Errorf("failed to retrieve emulation functions: %v", err)
		}
		pciBDF, err = getPciAddrByCtrlID(ctrlID, emulationFunctions, hotplug)
		if err != nil {
			return nsid, "", currUUID, fmt.Errorf("failed to get PCI BDF for controller: %v", err)
		}
		attempts++
	}

	klog.Infof("Final Device State -> NSID=%d, PCI BDF=%s", nsid, pciBDF)
	return nsid, pciBDF, currUUID, nil
}

// ExposeFSDevice exposes a filesystem device on the SNAP controller
func (c *client) ExposeFSDevice(deviceName string, dpuStatus snapstoragev1.VolumeAttachmentStatusDPU,
	parameters map[string]string) (string, string, error) {
	transports, err := VirtioFSGetTransports(c.rpcClient)
	if err != nil {
		return "", "", fmt.Errorf("failed to get virtio-fs transports: %w", err)
	}
	klog.Infof("Virtio-fs transports: %v", transports)

	if err := VirtioFSTransportCreate(c.rpcClient, transports); err != nil {
		return "", "", fmt.Errorf("failed to create virtio-fs transport: %w", err)
	}

	possibleManagers, err := VirtioFSGetPossibleManagers(c.rpcClient)
	if err != nil {
		return "", "", fmt.Errorf("failed to get possible DOCA managers: %w", err)
	}
	if len(possibleManagers) == 0 {
		return "", "", fmt.Errorf("no possible DOCA managers found")
	}

	managerName := possibleManagers[0].Name
	klog.Infof("Using DOCA manager: %s", managerName)

	managers, err := VirtioFSDOCAGetManagers(c.rpcClient)
	if err != nil {
		return "", "", fmt.Errorf("failed to get DOCA managers: %w", err)
	}
	klog.Infof("DOCA managers: %v", managers)

	if err := VirtioFSDOCAManagerCreate(c.rpcClient, managerName, managers); err != nil {
		return "", "", fmt.Errorf("failed to create DOCA manager: %w", err)
	}

	transports, err = VirtioFSGetTransports(c.rpcClient)
	if err != nil {
		return "", "", fmt.Errorf("failed to get virtio-fs transports: %w", err)
	}
	klog.Infof("Virtio-fs transports: %v", transports)

	if err := VirtioFSTransportStart(c.rpcClient, transports); err != nil {
		return "", "", fmt.Errorf("failed to start virtio-fs transport: %w", err)
	}

	functionLists, err := VirtioFSDOCAGetFunctions(c.rpcClient)
	if err != nil {
		return "", "", fmt.Errorf("failed to get DOCA functions: %w", err)
	}

	var vuid string
	var pciAddr string
	if dpuStatus.PCIDeviceAddress != "" {
		pciAddr = dpuStatus.PCIDeviceAddress
		vuid, err = GetVUIDByPCIAddress(functionLists, pciAddr)
		if err != nil {
			return "", "", fmt.Errorf("failed to get VUID for PCI address: %w", err)
		}
	} else {
		vuid, err = VirtioFSDOCAFunctionCreate(c.rpcClient, managerName)
		if err != nil {
			return "", "", fmt.Errorf("failed to create DOCA function: %w", err)
		}
	}

	devices, err := VirtioFSGetDevices(c.rpcClient)
	if err != nil {
		return "", "", fmt.Errorf("failed to get virtio-fs devices: %w", err)
	}
	klog.Infof("Virtio-fs devices: %v", devices)

	if VirtioFSDeviceExists(devices, deviceName) {
		klog.Infof("Virtio-fs device %s already exists. Skipping create.", deviceName)
		return deviceName + "tag", pciAddr, nil
	}

	if err := VirtioFSDeviceCreate(c.rpcClient, deviceName, parameters); err != nil {
		return "", "", fmt.Errorf("failed to create virtio-fs device: %w", err)
	}

	if err := VirtioFSDOCADeviceModify(c.rpcClient, managerName, deviceName, vuid); err != nil {
		return deviceName + "tag", pciAddr, fmt.Errorf("failed to modify DOCA device: %w", err)
	}

	devices, err = VirtioFSGetDevices(c.rpcClient)
	if err != nil {
		return "", "", fmt.Errorf("failed to get virtio-fs devices: %w", err)
	}
	klog.Infof("Virtio-fs devices: %v", devices)

	if err := VirtioFSDeviceStart(c.rpcClient, deviceName, devices); err != nil {
		return deviceName + "tag", pciAddr, fmt.Errorf("failed to start virtio-fs device: %w", err)
	}

	if dpuStatus.PCIDeviceAddress == "" {
		if err := VirtioFSDOCADeviceHotplug(c.rpcClient, deviceName); err != nil {
			return deviceName + "tag", pciAddr, fmt.Errorf("failed to hotplug virtio-fs device: %w", err)
		}
	}

	functionLists, err = VirtioFSDOCAGetFunctions(c.rpcClient)
	if err != nil {
		return "", "", fmt.Errorf("failed to get DOCA functions: %w", err)
	}

	pciAddr, err = GetPCIAddressByVUID(functionLists, vuid)
	if err != nil {
		return "", "", fmt.Errorf("failed to get PCI address: %w", err)
	}
	klog.Infof("PCI address: %s", pciAddr)

	return deviceName + "tag", pciAddr, nil
}

// DestroyBlockDevice destroys a block device on the SNAP controller
func (c *client) DestroyBlockDevice(nsid int, pciAddr string, hotplug bool) error {
	emulationFunctions, err := EmulationFunctionList(c.rpcClient)
	if err != nil {
		klog.Errorf("Failed to get emulation functions list: %v", err)
		return fmt.Errorf("failed to retrieve emulation functions: %v", err)
	}

	subsystems, err := NvmeSubsystemList(c.rpcClient)
	if err != nil {
		return fmt.Errorf("failed to retrieve NVMe subsystems: %v", err)
	}

	var ctrlID string
	var vuid string

	if hotplug {
		ctrlID, vuid, err = getNvmeHotplugByPciAddr(pciAddr, emulationFunctions)
		if err != nil {
			return fmt.Errorf("failed to get hotplugged NVMe device: %v", err)
		}
		err = NvmeEmulationDeviceDetachPrepare(c.rpcClient, vuid)
		if err != nil {
			return fmt.Errorf("failed to prepare NVMe emulation device detach: %v", err)
		}
	} else {
		ctrlID = getNvmeControllerByPciAddr(pciAddr, emulationFunctions)
	}

	namespaceExists := checkNamespaceAttached(nsid, ctrlID, subsystems)
	if err != nil {
		klog.Errorf("Failed to check namespace existence: %v", err)
		return err
	}

	if ctrlID == "" && !namespaceExists {
		klog.Infof("NVMe Controller and namespace not found for PCI Address %s. Skipping detach and destroy.", pciAddr)
		return nil
	}

	// Detach the namespace only if both exist
	if ctrlID == "" || !namespaceExists {
		klog.Infof("Namespace/Controller does not exist. Skipping detach.")
	} else {
		err = NvmeControllerDetachNs(c.rpcClient, ctrlID, nsid)
		if err != nil {
			klog.Errorf("Failed to detach namespace: %v", err)
			return fmt.Errorf("failed to detach namespace: %v", err)
		}
		klog.Infof("Successfully detached namespace ID %d", nsid)
	}

	if ctrlID == "" {
		klog.Infof("Controller ID does not exist. Skipping destroy.")
	} else {
		err = NvmeControllerDestroy(c.rpcClient, ctrlID)
		if err != nil {
			klog.Errorf("Failed to destroy controller: %v", err)
			return fmt.Errorf("failed to destroy controller: %v", err)
		}
		klog.Infof("Successfully destroyed controller ID %s", ctrlID)
	}

	if !namespaceExists {
		klog.Infof("Namespace ID %d does not exist. Skipping detach.", nsid)
	} else {
		err = NvmeNamespaceDestroy(c.rpcClient, nsid, subsystems)
		if err != nil {
			klog.Errorf("Failed to destroy namespace ID %d: %v", nsid, err)
			return fmt.Errorf("failed to destroy namespace: %v", err)
		}
		klog.Infof("Successfully destroyed namespace ID %d", nsid)
	}

	if hotplug {
		err = NvmeEmulationDeviceDetach(c.rpcClient, vuid)
		if err != nil {
			return fmt.Errorf("failed to detach NVMe emulation device: %v", err)
		}
	}

	return nil
}

// DestroyFSDevice destroys a filesystem device on the SNAP controller
func (c *client) DestroyFSDevice(deviceName string, pciAddr string) error {
	devices, err := VirtioFSGetDevices(c.rpcClient)
	if err != nil {
		return fmt.Errorf("failed to get virtio-fs devices: %w", err)
	}
	klog.Infof("Virtio-fs devices: %v", devices)

	if !VirtioFSDeviceExists(devices, deviceName) {
		klog.Infof("Virtio-fs device %s does not exist. Skipping destroy.", deviceName)
		return nil
	}
	possibleManagers, err := VirtioFSGetPossibleManagers(c.rpcClient)
	if err != nil {
		return fmt.Errorf("failed to get possible DOCA managers: %w", err)
	}
	if len(possibleManagers) == 0 {
		return fmt.Errorf("no possible DOCA managers found")
	}

	managerName := possibleManagers[0].Name
	klog.Infof("Using DOCA manager: %s", managerName)

	functionLists, err := VirtioFSDOCAGetFunctions(c.rpcClient)
	if err != nil {
		return fmt.Errorf("failed to get DOCA functions: %w", err)
	}

	if err := VirtioFSDOCADeviceHotunplug(c.rpcClient, deviceName); err != nil {
		return fmt.Errorf("failed to unplug virtio-fs device: %w", err)
	}

	time.Sleep(2 * time.Second)

	if err := VirtioFSDeviceStop(c.rpcClient, deviceName); err != nil {
		return fmt.Errorf("failed to stop virtio-fs device: %w", err)
	}

	if err := VirtioFSDeviceDestroy(c.rpcClient, deviceName); err != nil {
		return fmt.Errorf("failed to destroy virtio-fs device: %w", err)
	}

	var vuid string
	if pciAddr != "" {
		vuid, err = GetVUIDByPCIAddress(functionLists, pciAddr)
		if err != nil {
			return fmt.Errorf("failed to get VUID for PCI address: %w", err)
		}
	}
	if err := VirtioFSDOCAFunctionDestroy(c.rpcClient, managerName, vuid); err != nil {
		return fmt.Errorf("failed to destroy DOCA function: %w", err)
	}

	devices, err = VirtioFSGetDevices(c.rpcClient)
	if err != nil {
		return fmt.Errorf("failed to get virtio-fs devices: %w", err)
	}
	if len(devices) > 0 {
		klog.Infof("some virtio-fs device still exists, skipping destroy transport")
		return nil
	}

	if err := VirtioFSTransportStop(c.rpcClient); err != nil {
		return fmt.Errorf("failed to stop virtio-fs transport: %w", err)
	}

	if err := VirtioFSDOCAManagerDestroy(c.rpcClient, managerName); err != nil {
		return fmt.Errorf("failed to destroy DOCA manager: %w", err)
	}

	if err := VirtioFSTransportDestroy(c.rpcClient); err != nil {
		return fmt.Errorf("failed to destroy virtio-fs transport: %w", err)
	}

	return nil
}

// Close closes the underlying RPC connection
func (c *client) Close() error {
	return c.rpcClient.Close()
}
