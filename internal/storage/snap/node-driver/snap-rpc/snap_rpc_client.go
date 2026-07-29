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

// Client defines the interface for managing device operations
type Client interface {
	// ExposeBlockDevice exposes a block device on the SNAP controller
	ExposeBlockDevice(dpuStatus snapstoragev1.VolumeAttachmentStatusDPU, spec snapstoragev1.VolumeAttachmentSpec, parameters map[string]string) (int, string, string, string, error)
	// ExposeFSDevice exposes a filesystem device on the SNAP controller
	ExposeFSDevice(deviceName string, dpuStatus snapstoragev1.VolumeAttachmentStatusDPU,
		parameters map[string]string) (string, string, string, error)
	// DestroyBlockDevice destroys a block device on the SNAP controller
	DestroyBlockDevice(nsid int, pciAddr string, hotplug bool) error
	// DestroyFSDevice destroys a filesystem device on the SNAP controller
	DestroyFSDevice(deviceName string, pciAddr string) error
	// GetBlockFuncVUID returns the emulated NVMe function VUID exposed at pciAddr
	GetBlockFuncVUID(pciAddr string) (string, error)
	// GetFSFuncVUID returns the emulated VirtioFS function VUID exposed at pciAddr
	GetFSFuncVUID(pciAddr string) (string, error)
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
func (c *client) ExposeBlockDevice(dpuStatus snapstoragev1.VolumeAttachmentStatusDPU, spec snapstoragev1.VolumeAttachmentSpec, parameters map[string]string) (int, string, string, string, error) {
	deviceName := dpuStatus.DeviceName
	hotplug := spec.FunctionTypeConfig.HotplugFunction
	functionType := string(spec.FunctionTypeConfig.FunctionType)
	funcVUID := dpuStatus.FuncVUID // emulated function VUID (for VF: parent PF VUID)
	var err error

	if parameters == nil {
		parameters = make(map[string]string)
	}

	subsystems, err := NvmeSubsystemList(c.rpcClient)
	if err != nil {
		return 0, "", "", funcVUID, fmt.Errorf("failed to retrieve NVMe subsystems: %v", err)
	}

	nsid, currUUID := getNamespaceByDeviceName(deviceName, subsystems)

	if nsid == -1 {
		nsid, currUUID, err = NvmeNamespaceCreate(c.rpcClient, deviceName, subsystems, dpuStatus)
		if err != nil {
			return 0, "", "", funcVUID, fmt.Errorf("failed to create namespace: %v", err)
		}
		klog.Infof("Created new namespace: NSID=%d, UUID=%s", nsid, currUUID)
	} else {
		klog.Infof("Namespace already exists: NSID=%d, UUID=%s", nsid, currUUID)
	}

	ctrlID := getCtrlByDeviceName(deviceName, subsystems)

	emulationFunctions, err := EmulationFunctionList(c.rpcClient)
	if err != nil {
		klog.Errorf("Failed to retrieve emulation functions: %v", err)
		return 0, "", currUUID, funcVUID, fmt.Errorf("failed to retrieve emulation functions: %v", err)
	}

	// For hotplug with unknown PCI, discover existing SNAP state or reuse a
	// persisted FuncVUID before creating a new emulated function.
	if hotplug && dpuStatus.PCIDeviceAddress == "" {
		var created bool
		funcVUID, created, err = resolveHotplugNVMeFuncVUID(c.rpcClient, ctrlID, funcVUID, emulationFunctions)
		if err != nil {
			return nsid, "", currUUID, funcVUID, err
		}
		parameters["vuid"] = funcVUID

		// A function created just now is absent from the list fetched above, and
		// controller creation resolves the PCI address through that list.
		if created {
			emulationFunctions, err = EmulationFunctionList(c.rpcClient)
			if err != nil {
				return nsid, "", currUUID, funcVUID, fmt.Errorf("failed to retrieve emulation functions: %v", err)
			}
		}
	}

	var pciBDF string
	if ctrlID != "" {
		klog.Infof("Controller already exists: ID=%s", ctrlID)
		pciBDF, err = getPciAddrByCtrlID(ctrlID, emulationFunctions, hotplug)
		if err != nil {
			return nsid, "", currUUID, funcVUID, fmt.Errorf("failed to get PCI BDF for controller: %v", err)
		}
	} else {
		ctrlID, pciBDF, err = NvmeControllerCreate(c.rpcClient, subsystems, emulationFunctions, dpuStatus, parameters, functionType)
		if err != nil {
			return nsid, "", currUUID, funcVUID, fmt.Errorf("failed to create controller: %v", err)
		}

		isAttached := isControllerAttachedToNamespace(ctrlID, nsid, subsystems)
		if isAttached {
			klog.Infof("Controller %s is already attached to namespace %d, skipping attachment", ctrlID, nsid)
		} else {
			err = NvmeControllerAttachNs(c.rpcClient, ctrlID, nsid)
			if err != nil {
				return nsid, pciBDF, currUUID, funcVUID, fmt.Errorf("failed to attach namespace to controller: %v", err)
			}
		}

		err = NvmeControllerResume(c.rpcClient, ctrlID)
		if err != nil {
			return nsid, pciBDF, currUUID, funcVUID, fmt.Errorf("failed to resume controller: %v", err)
		}
	}

	if hotplug {
		err = NvmeControllerHotplug(c.rpcClient, ctrlID)
		if err != nil {
			return nsid, pciBDF, currUUID, funcVUID, fmt.Errorf("failed to hotplug controller: %v", err)
		}

		emulationFunctions, err = EmulationFunctionList(c.rpcClient)
		if err != nil {
			klog.Errorf("Failed to retrieve emulation functions: %v", err)
			return nsid, pciBDF, currUUID, funcVUID, fmt.Errorf("failed to retrieve emulation functions: %v", err)
		}

		pciBDF, err = getPciAddrByCtrlID(ctrlID, emulationFunctions, hotplug)
		if err != nil {
			return nsid, "", currUUID, funcVUID, fmt.Errorf("failed to get PCI BDF for controller: %v", err)
		}
	}

	if funcVUID == "" {
		funcVUID, err = getFunctionVUIDByPCIAddress(pciBDF, emulationFunctions)
		if err != nil {
			return nsid, pciBDF, currUUID, funcVUID, fmt.Errorf("failed to get function VUID: %v", err)
		}
	}

	klog.Infof("Final Device State -> CTRL ID=%s, NSID=%d, PCI BDF=%s, Function UUID (vuid)=%s", ctrlID, nsid, pciBDF, funcVUID)
	return nsid, pciBDF, currUUID, funcVUID, nil
}

// resolveHotplugNVMeFuncVUID picks the hotplug function VUID without creating a
// duplicate when SNAP already has a controller or the CR already persisted one.
// The boolean reports whether a new emulated function was created.
func resolveHotplugNVMeFuncVUID(rpcClient JSONRPCClient, ctrlID, persistedFuncVUID string,
	emulationFunctions EmulationFunctionListResponse) (string, bool, error) {
	if ctrlID != "" {
		discoveredVUID, err := getVUIDByCtrlID(ctrlID, emulationFunctions, true)
		if err != nil {
			return persistedFuncVUID, false, fmt.Errorf("failed to discover VUID for existing controller %s: %v", ctrlID, err)
		}
		if persistedFuncVUID != "" && persistedFuncVUID != discoveredVUID {
			return persistedFuncVUID, false, fmt.Errorf("persisted funcVUID %s does not match existing controller function VUID %s",
				persistedFuncVUID, discoveredVUID)
		}
		klog.Infof("Reusing existing hotplug NVMe function VUID %s from controller %s", discoveredVUID, ctrlID)
		return discoveredVUID, false, nil
	}

	if persistedFuncVUID != "" {
		klog.Infof("Reusing persisted hotplug NVMe function VUID %s", persistedFuncVUID)
		return persistedFuncVUID, false, nil
	}

	funcVUID, err := NvmeFunctionCreate(rpcClient)
	if err != nil {
		return "", false, fmt.Errorf("failed to create hotplug NVMe function: %v", err)
	}
	klog.Infof("Created hotplug NVMe function VUID %s", funcVUID)
	return funcVUID, true, nil
}

// ExposeFSDevice exposes a filesystem device on the SNAP controller
func (c *client) ExposeFSDevice(deviceName string, dpuStatus snapstoragev1.VolumeAttachmentStatusDPU,
	parameters map[string]string) (string, string, string, error) {
	transports, err := VirtioFSGetTransports(c.rpcClient)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to get virtio-fs transports: %w", err)
	}
	klog.Infof("Virtio-fs transports: %v", transports)

	if err := VirtioFSTransportCreate(c.rpcClient, transports); err != nil {
		return "", "", "", fmt.Errorf("failed to create virtio-fs transport: %w", err)
	}

	possibleManagers, err := VirtioFSGetPossibleManagers(c.rpcClient)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to get possible DOCA managers: %w", err)
	}
	if len(possibleManagers) == 0 {
		return "", "", "", fmt.Errorf("no possible DOCA managers found")
	}

	managerName := possibleManagers[0].Name
	klog.Infof("Using DOCA manager: %s", managerName)

	managers, err := VirtioFSDOCAGetManagers(c.rpcClient)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to get DOCA managers: %w", err)
	}
	klog.Infof("DOCA managers: %v", managers)

	if err := VirtioFSDOCAManagerCreate(c.rpcClient, managerName, managers); err != nil {
		return "", "", "", fmt.Errorf("failed to create DOCA manager: %w", err)
	}

	transports, err = VirtioFSGetTransports(c.rpcClient)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to get virtio-fs transports: %w", err)
	}
	klog.Infof("Virtio-fs transports: %v", transports)

	if err := VirtioFSTransportStart(c.rpcClient, transports); err != nil {
		return "", "", "", fmt.Errorf("failed to start virtio-fs transport: %w", err)
	}

	functionLists, err := VirtioFSDOCAGetFunctions(c.rpcClient)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to get DOCA functions: %w", err)
	}

	devices, err := VirtioFSGetDevices(c.rpcClient)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to get virtio-fs devices: %w", err)
	}
	klog.Infof("Virtio-fs devices: %v", devices)

	vuid, pciAddr, err := resolveVirtioFSFuncVUID(c.rpcClient, managerName, deviceName, dpuStatus, functionLists, devices)
	if err != nil {
		return "", "", "", err
	}

	if VirtioFSDeviceExists(devices, deviceName) {
		klog.Infof("Virtio-fs device %s already exists. Skipping create.", deviceName)
		pciAddr, err = ensureVirtioFSDevicePCI(c.rpcClient, deviceName, vuid, pciAddr, functionLists)
		if err != nil {
			return deviceName + "tag", pciAddr, vuid, err
		}
		return deviceName + "tag", pciAddr, vuid, nil
	}

	if err := VirtioFSDeviceCreate(c.rpcClient, deviceName, parameters); err != nil {
		return "", "", vuid, fmt.Errorf("failed to create virtio-fs device: %w", err)
	}

	if err := VirtioFSDOCADeviceModify(c.rpcClient, managerName, deviceName, vuid); err != nil {
		return deviceName + "tag", pciAddr, vuid, fmt.Errorf("failed to modify DOCA device: %w", err)
	}

	devices, err = VirtioFSGetDevices(c.rpcClient)
	if err != nil {
		return "", "", vuid, fmt.Errorf("failed to get virtio-fs devices: %w", err)
	}
	klog.Infof("Virtio-fs devices: %v", devices)

	if err := VirtioFSDeviceStart(c.rpcClient, deviceName, devices); err != nil {
		return deviceName + "tag", pciAddr, vuid, fmt.Errorf("failed to start virtio-fs device: %w", err)
	}

	if dpuStatus.PCIDeviceAddress == "" {
		if err := VirtioFSDOCADeviceHotplug(c.rpcClient, deviceName); err != nil {
			return deviceName + "tag", pciAddr, vuid, fmt.Errorf("failed to hotplug virtio-fs device: %w", err)
		}
	}

	functionLists, err = VirtioFSDOCAGetFunctions(c.rpcClient)
	if err != nil {
		return "", "", vuid, fmt.Errorf("failed to get DOCA functions: %w", err)
	}

	pciAddr, err = GetPCIAddressByVUID(functionLists, vuid)
	if err != nil {
		return "", "", vuid, fmt.Errorf("failed to get PCI address: %w", err)
	}
	klog.Infof("PCI address: %s", pciAddr)

	return deviceName + "tag", pciAddr, vuid, nil
}

// resolveVirtioFSFuncVUID picks the VirtioFS function VUID without creating a
// duplicate when SNAP already has the device or the CR already persisted one.
func resolveVirtioFSFuncVUID(rpcClient JSONRPCClient, managerName, deviceName string,
	dpuStatus snapstoragev1.VolumeAttachmentStatusDPU, functionLists []DOCAFunctionList, devices []FSDevice) (string, string, error) {
	if dpuStatus.PCIDeviceAddress != "" {
		pciAddr := dpuStatus.PCIDeviceAddress
		vuid, err := GetVUIDByPCIAddress(functionLists, pciAddr)
		if err != nil {
			return "", "", fmt.Errorf("failed to get VUID for PCI address: %w", err)
		}
		return vuid, pciAddr, nil
	}

	if dpuStatus.FuncVUID != "" {
		vuid := dpuStatus.FuncVUID
		klog.Infof("Reusing persisted VirtioFS function VUID %s", vuid)
		pciAddr, err := GetPCIAddressByVUID(functionLists, vuid)
		if err != nil {
			// Function may exist but not be hotplugged yet; PCI is filled later.
			klog.Infof("PCI address not yet known for persisted VUID %s: %v", vuid, err)
			return vuid, "", nil
		}
		return vuid, pciAddr, nil
	}

	if VirtioFSDeviceExists(devices, deviceName) {
		return "", "", fmt.Errorf("virtio-fs device %s already exists but funcVUID and pciDeviceAddress are unknown; refusing to create a new function", deviceName)
	}

	vuid, err := VirtioFSDOCAFunctionCreate(rpcClient, managerName)
	if err != nil {
		return "", "", fmt.Errorf("failed to create DOCA function: %w", err)
	}
	klog.Infof("Created VirtioFS function VUID %s", vuid)
	return vuid, "", nil
}

// ensureVirtioFSDevicePCI returns a PCI address for an already-created VirtioFS
// device, hotplugging when necessary.
func ensureVirtioFSDevicePCI(rpcClient JSONRPCClient, deviceName, vuid, pciAddr string, functionLists []DOCAFunctionList) (string, error) {
	if pciAddr != "" {
		return pciAddr, nil
	}
	if p, err := GetPCIAddressByVUID(functionLists, vuid); err == nil && p != "" {
		return p, nil
	}
	if err := VirtioFSDOCADeviceHotplug(rpcClient, deviceName); err != nil {
		return "", fmt.Errorf("failed to hotplug existing virtio-fs device: %w", err)
	}
	functionLists, err := VirtioFSDOCAGetFunctions(rpcClient)
	if err != nil {
		return "", fmt.Errorf("failed to get DOCA functions: %w", err)
	}
	pciAddr, err = GetPCIAddressByVUID(functionLists, vuid)
	if err != nil {
		return "", fmt.Errorf("failed to get PCI address: %w", err)
	}
	return pciAddr, nil
}

// GetBlockFuncVUID returns the emulated NVMe function VUID exposed at pciAddr.
// For a VF this is the parent PF VUID.
func (c *client) GetBlockFuncVUID(pciAddr string) (string, error) {
	if pciAddr == "" {
		return "", fmt.Errorf("PCI address is empty")
	}

	emulationFunctions, err := EmulationFunctionList(c.rpcClient)
	if err != nil {
		return "", fmt.Errorf("failed to retrieve emulation functions: %v", err)
	}

	return getFunctionVUIDByPCIAddress(pciAddr, emulationFunctions)
}

// GetFSFuncVUID returns the emulated VirtioFS function VUID exposed at pciAddr.
func (c *client) GetFSFuncVUID(pciAddr string) (string, error) {
	if pciAddr == "" {
		return "", fmt.Errorf("PCI address is empty")
	}

	functionLists, err := VirtioFSDOCAGetFunctions(c.rpcClient)
	if err != nil {
		return "", fmt.Errorf("failed to get DOCA functions: %w", err)
	}

	return GetVUIDByPCIAddress(functionLists, pciAddr)
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

	ctrlID := getNvmeControllerByPciAddr(pciAddr, emulationFunctions)
	if ctrlID == "" {
		klog.Errorf("No controller found for PCI address: %s", pciAddr)
	} else if hotplug {
		err = NvmeControllerHotunplug(c.rpcClient, ctrlID)
		if err != nil {
			return fmt.Errorf("failed to hotunplug controller: %v", err)
		}
		klog.Infof("Successfully hotunplugged controller ID %s", ctrlID)
	}

	namespaceExists, attachedToCtrl := checkNamespaceAttached(nsid, ctrlID, subsystems)

	// Detach the namespace only if it exists and is attached to this controller
	if attachedToCtrl {
		err = NvmeControllerDetachNs(c.rpcClient, ctrlID, nsid)
		if err != nil {
			klog.Errorf("Failed to detach namespace: %v", err)
			return fmt.Errorf("failed to detach namespace: %v", err)
		}
		klog.Infof("Successfully detached namespace ID %d", nsid)
	}

	if ctrlID != "" {
		err = NvmeControllerDestroy(c.rpcClient, ctrlID)
		if err != nil {
			klog.Errorf("Failed to destroy controller: %v", err)
			return fmt.Errorf("failed to destroy controller: %v", err)
		}
		klog.Infof("Successfully destroyed controller ID %s", ctrlID)
	}

	if namespaceExists {
		err = NvmeNamespaceDestroy(c.rpcClient, nsid, subsystems)
		if err != nil {
			klog.Errorf("Failed to destroy namespace ID %d: %v", nsid, err)
			return fmt.Errorf("failed to destroy namespace: %v", err)
		}
		klog.Infof("Successfully destroyed namespace ID %d", nsid)
	}

	if hotplug {
		vuid := getHotplugVUIDByPCIAddress(pciAddr, emulationFunctions)
		if vuid != "" {
			err = NvmeFunctionDestroy(c.rpcClient, vuid)
			if err != nil {
				return fmt.Errorf("failed to destroy NVMe function: %v", err)
			}
			klog.Infof("Successfully destroyed NVMe function ID %s", vuid)
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
