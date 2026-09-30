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
	"strconv"
	"strings"
	"time"

	snapstoragev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"

	"github.com/google/uuid"
	"k8s.io/klog/v2"
)

// Client defines the interface for managing device operations
type Client interface {
	// ExposeBlockDevice exposes a block device on the SNAP controller
	ExposeBlockDevice(dpuStatus snapstoragev1.VolumeAttachmentStatusDPU, spec snapstoragev1.VolumeAttachmentSpec, parameters map[string]string) (int, string, string, string, error)
	// ExposeFSDevice exposes a filesystem device on the SNAP controller
	ExposeFSDevice(deviceName string, dpuStatus snapstoragev1.VolumeAttachmentStatusDPU,
		parameters map[string]string) (string, string, string, error)
	// ExposeMemosDevice exposes a CMX key/value volume as an NVMe KV namespace on
	// the SNAP controller
	ExposeMemosDevice(dpuStatus snapstoragev1.VolumeAttachmentStatusDPU, spec snapstoragev1.VolumeAttachmentSpec,
		parameters map[string]string) (int, string, string, string, error)
	// DestroyBlockDevice destroys a block device on the SNAP controller.
	DestroyBlockDevice(deviceName string, nsid int, pciAddr string, hotplug bool) error
	// DestroyMemosDevice destroys the NVMe front end of a CMX key/value volume on
	// the SNAP controller. deviceName is the MEMOS volume, whose name is also the
	// handle of the dedicated subsystem holding the volume's namespace.
	DestroyMemosDevice(funcVUID string, nsid int, pciAddr string, hotplug bool, deviceName string) error
	// DestroyFSDevice destroys a filesystem device on the SNAP controller
	DestroyFSDevice(deviceName string, pciAddr string) error
	// GetBlockFuncVUID returns the emulated NVMe function VUID exposed at pciAddr
	GetBlockFuncVUID(pciAddr string) (string, error)
	// GetMemosFuncVUID returns the emulated NVMe function VUID exposed at pciAddr
	// on the SNAP5 MEMOS path (doca_nvme_get_emulation_functions).
	GetMemosFuncVUID(pciAddr string) (string, error)
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

// memosRPC returns the transport the CMX key/value path uses. It is the same
// connection the block path runs on, read as SNAP5 replies rather than SNAP4
// ones - see MemosJSONRPCClient.
func (c *client) memosRPC() MemosJSONRPCClient {
	return NewMemosJSONRPCClient(c.rpcClient)
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

	// An existing namespace keeps whichever subsystem it is already in, which is
	// what makes a volume attached before this driver moved to a subsystem per
	// volume keep working: its namespace is still in the shared init subsystem.
	nqn, nsid, currUUID := getNamespaceByDeviceName(deviceName, subsystems)

	// namespaceCreated records that the subsystem listing above predates this
	// namespace, so the namespace must not be looked up in it further down.
	namespaceCreated := false
	if nsid == -1 {
		nqn = SubsystemNQNForDevice(deviceName)
		if err := NvmeSubsystemCreate(c.rpcClient, nqn, subsystems); err != nil {
			return 0, "", "", funcVUID, fmt.Errorf("failed to create subsystem: %v", err)
		}

		nsid, currUUID, err = NvmeNamespaceCreate(c.rpcClient, deviceName, nqn, dpuStatus)
		if err != nil {
			return 0, "", "", funcVUID, fmt.Errorf("failed to create namespace: %v", err)
		}
		namespaceCreated = true
		klog.Infof("Created new namespace: NQN=%s, NSID=%d, UUID=%s", nqn, nsid, currUUID)
	} else {
		klog.Infof("Namespace already exists: NQN=%s, NSID=%d, UUID=%s", nqn, nsid, currUUID)
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
		ctrlID, pciBDF, err = NvmeControllerCreate(c.rpcClient, nqn, emulationFunctions, dpuStatus, parameters, functionType)
		if err != nil {
			return nsid, "", currUUID, funcVUID, fmt.Errorf("failed to create controller: %v", err)
		}

		isAttached := !namespaceCreated && isControllerAttachedToNamespace(ctrlID, nqn, nsid, subsystems)
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

// GetMemosFuncVUID returns the SNAP5 emulated NVMe function VUID at pciAddr.
// That is the same vuid field of doca_nvme_get_emulation_functions (static PF
// or hotplug). A hotplug BDF that has not yet landed on the emulation list is
// resolved from pci_switch_show_info.
func (c *client) GetMemosFuncVUID(pciAddr string) (string, error) {
	if pciAddr == "" {
		return "", fmt.Errorf("PCI address is empty")
	}

	functions, err := DocaNvmeGetEmulationFunctions(c.memosRPC())
	if err != nil {
		return "", err
	}
	if function, found := DocaNvmeFindFunctionByBDF(functions, pciAddr); found {
		return function.VUID, nil
	}

	switches, err := PCISwitchShowInfo(c.memosRPC())
	if err != nil {
		return "", err
	}
	if vuid, _, found := findPCISwitchPortByBDF(switches, pciAddr); found {
		return vuid, nil
	}

	return "", fmt.Errorf("no NVMe emulation function found for PCI address %s", pciAddr)
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
func (c *client) DestroyBlockDevice(deviceName string, nsid int, pciAddr string, hotplug bool) error {
	emulationFunctions, err := EmulationFunctionList(c.rpcClient)
	if err != nil {
		klog.Errorf("Failed to get emulation functions list: %v", err)
		return fmt.Errorf("failed to retrieve emulation functions: %v", err)
	}

	subsystems, err := NvmeSubsystemList(c.rpcClient)
	if err != nil {
		return fmt.Errorf("failed to retrieve NVMe subsystems: %v", err)
	}

	// Live state decides which subsystem the namespace is in, so a volume
	// attached under the previous shared-subsystem model is torn down from the
	// subsystem it actually lives in rather than a derived one. The derived NQN
	// is only a fallback for cleaning up a subsystem left behind by a teardown
	// that failed after the namespace was already gone.
	nqn, liveNSID, _ := getNamespaceByDeviceName(deviceName, subsystems)
	if liveNSID != -1 {
		nsid = liveNSID
	}
	if nqn == "" {
		nqn = SubsystemNQNForDevice(deviceName)
	}

	ctrlID, ctrlPCIAddr := resolveOwnedController(deviceName, nqn, pciAddr, subsystems, emulationFunctions)
	if ctrlID == "" {
		klog.Errorf("No controller of device %s found at PCI address: %s", deviceName, pciAddr)
	} else if hotplug {
		err = NvmeControllerHotunplug(c.rpcClient, ctrlID)
		if err != nil {
			return fmt.Errorf("failed to hotunplug controller: %v", err)
		}
		klog.Infof("Successfully hotunplugged controller ID %s", ctrlID)
	}

	namespaceExists, attachedToCtrl := checkNamespaceAttached(nqn, nsid, ctrlID, subsystems)

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
		err = NvmeNamespaceDestroy(c.rpcClient, nqn, nsid)
		if err != nil {
			klog.Errorf("Failed to destroy namespace ID %d: %v", nsid, err)
			return fmt.Errorf("failed to destroy namespace: %v", err)
		}
		klog.Infof("Successfully destroyed namespace ID %d", nsid)
	}

	// The function is addressed through the controller that was proven to be this
	// volume's, so a stale pciAddr can no longer reach another volume's function.
	if hotplug && ctrlPCIAddr != "" {
		vuid := getHotplugVUIDByPCIAddress(ctrlPCIAddr, emulationFunctions)
		if vuid != "" {
			err = NvmeFunctionDestroy(c.rpcClient, vuid)
			if err != nil {
				return fmt.Errorf("failed to destroy NVMe function: %v", err)
			}
			klog.Infof("Successfully destroyed NVMe function ID %s", vuid)
		}
	}

	// The prefix check keeps the subsystem declared in snapRpcInitConf in place:
	// it still hosts the admin-only PF controller in the nvme-vf-on-static-pf
	// scenario, and legacy namespaces until they are reattached.
	if isDriverOwnedSubsystem(nqn) && subsystemExists(subsystems, nqn) {
		if err := NvmeSubsystemDestroy(c.rpcClient, nqn); err != nil {
			klog.Errorf("Failed to destroy subsystem %s, leaving it behind: %v", nqn, err)
		} else {
			klog.Infof("Successfully destroyed subsystem %s", nqn)
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

/*
 * CMX key/value path.
 *
 * The block path exposes a local backend behind an emulated NVMe function. The
 * CMX path serves the same host-visible shape - one NVMe namespace on one
 * controller - but the namespace is backed by a MEMOS volume composed of
 * namespaces on a remote CMX target, and the controller sits on a hotplug PCI
 * switch port instead of a static or SR-IOV function.
 */

// Storage parameter keys configuring the NVMe front end of the CMX path. The
// target side is not configured here: the plugin connects the MEMOS target and
// creates the volume, and hands the volume name back as the device name.
const (
	ParamMemosSubsystemNQN = "memos_subsystem_nqn"
	ParamMemosNumQueues    = "memos_num_queues"
	ParamMemosPCISwitch    = "memos_pci_switch"
	// ParamMemosNvmeManagers and ParamMemosNvmeNetworkDevs configure the NVMe
	// module, which has to be configured before the first subsystem is created.
	ParamMemosNvmeManagers    = "memos_nvme_managers"
	ParamMemosNvmeNetworkDevs = "memos_nvme_network_devs"
)

// memosConfig is the resolved NVMe front-end configuration for one attachment.
type memosConfig struct {
	subsystemNQN    string
	numQueues       int
	pciSwitch       string
	nvmeManagers    string
	nvmeNetworkDevs string
}

// parseMemosConfig resolves the NVMe front-end configuration from the storage
// parameters.
func parseMemosConfig(parameters map[string]string) (memosConfig, error) {
	if parameters == nil {
		parameters = make(map[string]string)
	}

	cfg := memosConfig{
		subsystemNQN:    parameters[ParamMemosSubsystemNQN],
		pciSwitch:       parameters[ParamMemosPCISwitch],
		nvmeManagers:    parameters[ParamMemosNvmeManagers],
		nvmeNetworkDevs: parameters[ParamMemosNvmeNetworkDevs],
	}

	if value := parameters[ParamMemosNumQueues]; value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return memosConfig{}, fmt.Errorf("invalid %s %q: %w", ParamMemosNumQueues, value, err)
		}
		cfg.numQueues = parsed
	}

	// The NQN is the base for each volume's dedicated subsystem, so it is the one
	// parameter that must be present.
	if cfg.subsystemNQN == "" {
		return memosConfig{}, fmt.Errorf("no NVMe subsystem NQN: %s is required", ParamMemosSubsystemNQN)
	}

	return cfg, nil
}

// memosSubsystemNamePrefix keeps the NVMe subsystem handle distinct from the
// MEMOS volume it fronts. SNAP keeps its emulation objects - including the
// plugin's MEMOS volume, which is named after the same volume - in one name
// registry, so using the volume name verbatim as the subsystem handle collides
// with it and fails with SNAP_ERROR_IN_USE.
const memosSubsystemNamePrefix = "nvme_subsys_"

// memosSubsystemName returns the handle of the NVMe subsystem dedicated to
// volumeName. It is a pure function of the volume name so teardown can address
// the subsystem from deviceName alone, and it is prefixed so it never collides
// with the MEMOS volume of the same name.
func memosSubsystemName(volumeName string) string {
	return memosSubsystemNamePrefix + volumeName
}

// memosVolumeSubsystem derives the dedicated NVMe subsystem for one CMX volume:
// its handle (used by the doca_nvme RPCs) and its NQN (what the host enumerates).
//
// Each volume gets a subsystem of its own so that its namespace is visible only
// to that volume's controller, and therefore only to that volume's PCI function.
// A single subsystem shared across volumes would instead expose every namespace
// to every controller in it, letting one host reach another volume's data.
func memosVolumeSubsystem(baseNQN, volumeName string) (name, nqn string) {
	return memosSubsystemName(volumeName), baseNQN + ":" + volumeName
}

// ExposeMemosDevice exposes a CMX key/value volume as an NVMe KV namespace.
//
// The MEMOS side is the plugin's: it connects the target and creates the volume,
// and dpuStatus.DeviceName is that volume name. This builds the NVMe front end
// over it - subsystem, KV namespace bound to the volume, and a controller on an
// emulated function - mirroring how ExposeBlockDevice wraps a bdev. Every step
// checks the live state first, so a retried attachment converges instead of
// creating duplicates.
//
// It returns the namespace ID, the PCI address, the host-visible NGUID and the
// VUID of the emulated function, matching ExposeBlockDevice.
func (c *client) ExposeMemosDevice(dpuStatus snapstoragev1.VolumeAttachmentStatusDPU,
	spec snapstoragev1.VolumeAttachmentSpec, parameters map[string]string) (int, string, string, string, error) {
	// volumeName is the MEMOS volume the plugin created, which the KV namespace
	// takes as its backend.
	volumeName := dpuStatus.DeviceName
	funcVUID := dpuStatus.FuncVUID
	// The namespace listing does not report the NGUID, so the value recorded on
	// the CR is the only source once the namespace exists.
	hostNGUID := dpuStatus.BdevAttrs.NVMeUUID

	// The MEMOS path exposes the KV namespace over a static NVMe function or a
	// hotplug switch port, neither of which selects a VF. Reject vf rather than
	// silently exposing a PF and diverging from the requested function type.
	if spec.FunctionTypeConfig.FunctionType == snapstoragev1.FunctionTypeVF {
		return 0, "", hostNGUID, funcVUID, fmt.Errorf("function type %q is not supported for MEMOS/CMX attachments", snapstoragev1.FunctionTypeVF)
	}

	cfg, err := parseMemosConfig(parameters)
	if err != nil {
		return 0, "", hostNGUID, funcVUID, err
	}

	if volumeName == "" {
		return 0, "", hostNGUID, funcVUID, fmt.Errorf("no device name on the attachment: the plugin has not created the MEMOS volume yet")
	}

	subsystems, err := DocaNvmeGetSubsystems(c.memosRPC())
	if err != nil {
		return 0, "", hostNGUID, funcVUID, err
	}

	if err := c.ensureNvmeConfig(cfg, subsystems); err != nil {
		return 0, "", hostNGUID, funcVUID, err
	}

	namespaces, err := DocaNvmeGetNamespaces(c.memosRPC())
	if err != nil {
		return 0, "", hostNGUID, funcVUID, err
	}

	subsystemName, subsystemNQN := memosVolumeSubsystem(cfg.subsystemNQN, volumeName)
	nsid := 0
	if ns, exists := DocaNvmeFindNamespaceByBackend(namespaces, volumeName); exists {
		nsid = ns.NSID
		subsystemName = ns.SubsystemName
		klog.Infof("NVMe KV namespace already exists: NSID=%d, subsystem=%s", nsid, subsystemName)
	} else {
		err = DocaNvmeSubsystemCreate(c.memosRPC(), DocaNvmeSubsystemCreateRequest{
			SubsystemName: subsystemName,
			NQN:           subsystemNQN,
		}, subsystems)
		if err != nil {
			return 0, "", hostNGUID, funcVUID, err
		}

		nsid = int(dpuStatus.BdevAttrs.NVMeNsID)
		if nsid < 1 {
			nsid = 1
		}
		if hostNGUID == "" {
			hostNGUID = newHostNGUID()
		}

		err = DocaNvmeSubsystemNsCreate(c.memosRPC(), DocaNvmeNamespaceCreateRequest{
			SubsystemName: subsystemName,
			NSID:          nsid,
			BackendName:   volumeName,
			CSI:           CSIKV,
			NGUID:         hostNGUID,
		})
		if err != nil {
			return 0, "", hostNGUID, funcVUID, err
		}
		klog.Infof("Created NVMe KV namespace: NSID=%d, NGUID=%s, backend=%s", nsid, hostNGUID, volumeName)
	}

	// The controller binds to an emulated function. A hotplug attachment gets its
	// own PCI switch port; otherwise it claims a static function, which already
	// exists on the DPU and is neither created nor powered.
	hotplug := spec.FunctionTypeConfig.HotplugFunction

	var pciBDF string
	if hotplug {
		funcVUID, _, err = c.ensureMemosSwitchPort(cfg, funcVUID)
	} else {
		funcVUID, pciBDF, err = c.resolveMemosStaticFunction(funcVUID)
	}
	if err != nil {
		return nsid, "", hostNGUID, funcVUID, err
	}

	controllers, err := DocaNvmeGetControllers(c.memosRPC())
	if err != nil {
		return nsid, "", hostNGUID, funcVUID, err
	}

	// A function carries at most one controller, so the port decides whether a
	// controller already exists for this attachment.
	var cntlID int
	if ctrl, exists := DocaNvmeFindControllerByVUID(controllers, funcVUID); exists {
		klog.Infof("NVMe controller already exists: subsystem=%s cntlID=%d", ctrl.SubsystemName, ctrl.CntlID)
		subsystemName = ctrl.SubsystemName
		cntlID = ctrl.CntlID
	} else {
		cntlID = DocaNvmeNextControllerID(controllers, subsystemName)
		err = DocaNvmeSubsystemControllerCreate(c.memosRPC(), DocaNvmeControllerCreateRequest{
			SubsystemName: subsystemName,
			CntlID:        cntlID,
			VUID:          funcVUID,
			NumQueues:     cfg.numQueues,
		})
		if err != nil {
			return nsid, "", hostNGUID, funcVUID, err
		}
	}

	if hotplug {
		pciBDF, err = c.hotplugMemosController(subsystemName, cntlID, funcVUID)
		if err != nil {
			return nsid, "", hostNGUID, funcVUID, err
		}
	}

	klog.Infof("Final CMX Device State -> subsystem=%s, NSID=%d, PCI BDF=%s, Function UUID (vuid)=%s, volume=%s",
		subsystemName, nsid, pciBDF, funcVUID, volumeName)

	return nsid, pciBDF, hostNGUID, funcVUID, nil
}

// ensureNvmeConfig selects the emulation managers and network devices the NVMe
// module uses.
//
// Every subsystem depends on this configuration, so it has to be set before the
// first one is created; an existing subsystem means it is already set.
func (c *client) ensureNvmeConfig(cfg memosConfig, subsystems []DocaNvmeSubsystem) error {
	if len(subsystems) > 0 {
		klog.Infof("NVMe subsystems already exist, leaving the existing configuration in place")
		return nil
	}

	managerList := cfg.nvmeManagers
	if managerList == "" {
		managers, err := DocaNvmeGetSupportedManagers(c.memosRPC())
		if err != nil {
			return err
		}
		if len(managers) == 0 {
			return fmt.Errorf("no NVMe emulation managers reported by the SNAP service")
		}
		managerList = strings.Join(DocaNvmeManagerNames(managers), ",")
		klog.Infof("No NVMe manager list configured, using every supported manager: %s", managerList)
	}

	// The network list defaults to the devices the MEMOS data path uses, since
	// that is where the CMX traffic goes.
	return DocaNvmeSetConfig(c.memosRPC(), managerList, cfg.nvmeNetworkDevs)
}

// ensureMemosSwitchPort returns the hotplug PCI switch port the controller binds
// to, creating one when this attachment does not have a live port yet.
func (c *client) ensureMemosSwitchPort(cfg memosConfig, funcVUID string) (string, string, error) {
	switches, err := PCISwitchShowInfo(c.memosRPC())
	if err != nil {
		return funcVUID, "", err
	}

	// A persisted VUID with a live port means the port already exists; creating
	// another would leak a PCIe function on every retry.
	if funcVUID != "" {
		if _, owningSwitch, found := FindPCISwitchPort(switches, funcVUID); found {
			return funcVUID, owningSwitch, nil
		}
		klog.Infof("Persisted VUID %s has no PCI switch port, creating a new one", funcVUID)
	}

	switchName := cfg.pciSwitch
	if switchName == "" {
		switchName, err = PCISwitchForType(switches, PCIPortTypeNVMe)
		if err != nil {
			return funcVUID, "", err
		}
	}

	funcVUID, err = PCISwitchPortCreate(c.memosRPC(), switchName, PCIPortTypeNVMe)
	if err != nil {
		return "", switchName, err
	}

	return funcVUID, switchName, nil
}

// resolveMemosStaticFunction claims the static emulation function the controller
// is exposed through and returns its VUID and PCI address. A static function is
// always present on the DPU, so it is claimed rather than created.
func (c *client) resolveMemosStaticFunction(funcVUID string) (string, string, error) {
	functions, err := DocaNvmeGetEmulationFunctions(c.memosRPC())
	if err != nil {
		return funcVUID, "", err
	}

	if funcVUID != "" {
		function, found := DocaNvmeFindFunctionByVUID(functions, funcVUID)
		if !found {
			return funcVUID, "", fmt.Errorf("no NVMe emulation function with VUID %s", funcVUID)
		}
		return function.VUID, function.BDF, nil
	}

	function, found := DocaNvmeFindFreeStaticFunction(functions)
	if !found {
		return "", "", fmt.Errorf("no free static NVMe emulation function available")
	}

	klog.Infof("Claiming static NVMe emulation function %s at %s", function.VUID, function.BDF)

	return function.VUID, function.BDF, nil
}

// hotplugMemosController publishes a hotplug-bound controller to the host and
// returns the PCI address the function is assigned once it is visible. A
// controller on a static function must not take this path: the service rejects
// it, because that function is always present.
func (c *client) hotplugMemosController(subsystemName string, cntlID int, vuid string) (string, error) {
	switches, err := PCISwitchShowInfo(c.memosRPC())
	if err != nil {
		return "", err
	}

	port, _, _ := FindPCISwitchPort(switches, vuid)
	if port.Power != PCIPortStatePowerOn {
		if err := DocaNvmeSubsystemControllerHotplug(c.memosRPC(), subsystemName, cntlID); err != nil {
			return "", err
		}

		refreshed, err := PCISwitchShowInfo(c.memosRPC())
		if err != nil {
			return "", err
		}
		port, _, _ = FindPCISwitchPort(refreshed, vuid)
	}

	if port.BDF != "" {
		return port.BDF, nil
	}

	// The BDF is also reported on the emulation function once the PCI function is
	// stable, which covers a listing that has not yet filled the switch port.
	functions, err := DocaNvmeGetEmulationFunctions(c.memosRPC())
	if err != nil {
		return "", err
	}
	if function, found := DocaNvmeFindFunctionByVUID(functions, vuid); found && function.BDF != "" {
		return function.BDF, nil
	}

	return "", fmt.Errorf("no PCI address reported for hotplugged controller %d in subsystem %s", cntlID, subsystemName)
}

// newHostNGUID generates a host-visible namespace identifier: 32 hex characters,
// which is a UUID without its dashes.
func newHostNGUID() string {
	return strings.ReplaceAll(uuid.Must(uuid.NewRandom()).String(), "-", "")
}

// DestroyMemosDevice tears down the NVMe front end of a CMX key/value volume.
//
// The MEMOS volume itself is the plugin's to destroy, so this stops at releasing
// the namespace that references it. Teardown runs front to back - controller
// hotunplug, controller, namespace, subsystem, then the port - and each step
// resolves its target from live state and is skipped when the object is already
// gone, so a retried detachment converges.
func (c *client) DestroyMemosDevice(funcVUID string, nsid int, pciAddr string, hotplug bool, deviceName string) error {
	controllers, err := DocaNvmeGetControllers(c.memosRPC())
	if err != nil {
		return err
	}

	// The emulated function is what ties the SNAP resources to this attachment,
	// and it is either a hotplug switch port or a static function.
	vuid, switchName, err := c.resolveMemosFunction(funcVUID, pciAddr, hotplug)
	if err != nil {
		return err
	}
	if vuid == "" {
		klog.Errorf("No NVMe emulation function found for funcVUID %q / PCI address %q", funcVUID, pciAddr)
	}

	var subsystemName string
	ctrl, hasCtrl := DocaNvmeFindControllerByVUID(controllers, vuid)
	if hasCtrl {
		subsystemName = ctrl.SubsystemName
	} else if vuid != "" {
		klog.Errorf("No NVMe controller found for VUID: %s", vuid)
	}

	// With the controller already gone, fall back to the volume's dedicated
	// subsystem, whose handle is derived from the volume name, so the namespace
	// and subsystem are still released on retry.
	if subsystemName == "" {
		subsystemName = memosSubsystemName(deviceName)
	}

	// Hotunplug retracts the namespace from the host before anything backing it
	// is torn down. A static function is always present and cannot be unplugged.
	if hasCtrl && hotplug {
		if err := DocaNvmeSubsystemControllerHotunplug(c.memosRPC(), subsystemName, ctrl.CntlID); err != nil {
			return err
		}
	}

	if hasCtrl {
		if err := DocaNvmeSubsystemControllerDestroy(c.memosRPC(), subsystemName, ctrl.CntlID); err != nil {
			return err
		}
		klog.Infof("Successfully destroyed NVMe controller %d in subsystem %s", ctrl.CntlID, subsystemName)
	}

	namespaces, err := DocaNvmeGetNamespaces(c.memosRPC())
	if err != nil {
		return err
	}

	if ns, exists := DocaNvmeFindNamespace(namespaces, subsystemName, nsid); exists {
		if err := DocaNvmeSubsystemNsDestroy(c.memosRPC(), subsystemName, nsid); err != nil {
			return err
		}
		klog.Infof("Successfully destroyed NVMe namespace ID %d in subsystem %s, releasing backend %s",
			nsid, subsystemName, ns.Backend)
	}

	if err := c.destroyIdleNvmeSubsystem(subsystemName); err != nil {
		return err
	}

	// Only a hotplug port is ours to destroy; a static function is permanent.
	if hotplug && vuid != "" && switchName != "" {
		if err := PCISwitchPortDestroy(c.memosRPC(), switchName, vuid); err != nil {
			return err
		}
		klog.Infof("Successfully destroyed PCI switch port %s", vuid)
	}

	return nil
}

// resolveMemosFunction returns the VUID of this attachment's emulated function
// and, for a hotplug function, the switch owning its port.
//
// It prefers the persisted funcVUID: a hotunplug clears the port's BDF, so the
// VUID is the only identity that survives a teardown retried after the
// hotunplug. The BDF is used only when no funcVUID was ever recorded (legacy
// attachments), reproducing the previous behavior.
func (c *client) resolveMemosFunction(funcVUID, pciAddr string, hotplug bool) (string, string, error) {
	if funcVUID == "" {
		return c.resolveMemosFunctionByBDF(pciAddr, hotplug)
	}

	// A static function has no switch port; the VUID alone drives teardown.
	if !hotplug {
		return funcVUID, "", nil
	}

	// The switch owning the port is resolved by VUID, which still matches after a
	// hotunplug even though the port's BDF has been cleared. A missing port means
	// it was already destroyed, so the later port-destroy step is a no-op.
	switches, err := PCISwitchShowInfo(c.memosRPC())
	if err != nil {
		return "", "", err
	}
	if _, switchName, found := FindPCISwitchPort(switches, funcVUID); found {
		return funcVUID, switchName, nil
	}

	return funcVUID, "", nil
}

// resolveMemosFunctionByBDF returns the VUID of the emulated function at pciAddr,
// and the switch owning it when it is a hotplug port.
func (c *client) resolveMemosFunctionByBDF(pciAddr string, hotplug bool) (string, string, error) {
	if pciAddr == "" {
		return "", "", nil
	}

	if hotplug {
		switches, err := PCISwitchShowInfo(c.memosRPC())
		if err != nil {
			return "", "", err
		}
		vuid, switchName, _ := findPCISwitchPortByBDF(switches, pciAddr)
		return vuid, switchName, nil
	}

	functions, err := DocaNvmeGetEmulationFunctions(c.memosRPC())
	if err != nil {
		return "", "", err
	}
	if function, found := DocaNvmeFindFunctionByBDF(functions, pciAddr); found {
		return function.VUID, "", nil
	}

	return "", "", nil
}

// destroyIdleNvmeSubsystem removes the subsystem once it holds no namespaces or
// controllers. It is shared by every CMX volume on the node, so it outlives an
// individual attachment.
func (c *client) destroyIdleNvmeSubsystem(subsystemName string) error {
	if subsystemName == "" {
		return nil
	}

	namespaces, err := DocaNvmeGetNamespaces(c.memosRPC())
	if err != nil {
		return err
	}
	for _, ns := range namespaces {
		if ns.SubsystemName == subsystemName {
			klog.Infof("NVMe subsystem %s still has namespaces, leaving it in place", subsystemName)
			return nil
		}
	}

	controllers, err := DocaNvmeGetControllers(c.memosRPC())
	if err != nil {
		return err
	}
	for _, ctrl := range controllers {
		if ctrl.SubsystemName == subsystemName {
			klog.Infof("NVMe subsystem %s still has controllers, leaving it in place", subsystemName)
			return nil
		}
	}

	subsystems, err := DocaNvmeGetSubsystems(c.memosRPC())
	if err != nil {
		return err
	}
	if !DocaNvmeSubsystemExists(subsystems, subsystemName) {
		return nil
	}

	return DocaNvmeSubsystemDestroy(c.memosRPC(), subsystemName)
}

// findPCISwitchPortByBDF locates the port exposed at pciAddr and reports its VUID
// and switch.
func findPCISwitchPortByBDF(switches []PCISwitch, pciAddr string) (string, string, bool) {
	if pciAddr == "" {
		return "", "", false
	}

	for _, sw := range switches {
		for _, port := range sw.Ports {
			if port.BDF == pciAddr {
				return port.VUID, sw.Name, true
			}
		}
	}

	return "", "", false
}

// Close closes the underlying RPC connection
func (c *client) Close() error {
	return c.rpcClient.Close()
}
