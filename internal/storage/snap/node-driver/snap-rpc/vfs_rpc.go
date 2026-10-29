/*
COPYRIGHT 2025 NVIDIA

Licensed under the Apache License, Version 2.0 (the License);
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an AS IS BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package rpcclient

import (
	"encoding/json"
	"fmt"
	"strconv"

	"k8s.io/klog/v2"
)

// VirtioFSTransportCreate creates a virtio-fs transport
func VirtioFSTransportCreate(client JSONRPCClient, transports []Transport) error {
	for _, transport := range transports {
		if transport.Name == "DOCA" {
			klog.Infof("DOCA transport already exists, continuing...")
			return nil
		}
	}

	params := map[string]interface{}{
		"transport_name": "DOCA",
	}

	result, err := client.Call("virtio_fs_transport_create", params)
	if err != nil {
		return fmt.Errorf("failed to create virtio-fs transport: %w", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.Infof("Transport created successfully: %s", string(resultBytes))

	return nil
}

// VirtioFSGetPossibleManagers gets the list of possible DOCA managers
func VirtioFSGetPossibleManagers(client JSONRPCClient) ([]DOCAManager, error) {
	result, err := client.Call("virtio_fs_doca_get_possible_managers", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get possible managers: %w", err)
	}
	resultBytes, _ := json.MarshalIndent(result, "", "  ")

	var managers []DOCAManager
	if err := json.Unmarshal(resultBytes, &managers); err != nil {
		return nil, fmt.Errorf("failed to decode managers response: %w", err)
	}

	klog.V(4).Infof("Retrieved possible managers: %+v", managers)

	return managers, nil
}

// VirtioFSDOCAManagerCreate creates a DOCA virtio-fs emulation manager
func VirtioFSDOCAManagerCreate(client JSONRPCClient, managerName string, managers []DOCAManager) error {
	if managerName == "" {
		return fmt.Errorf("manager name is required")
	}

	for _, manager := range managers {
		if manager.Name == managerName {
			klog.Infof("DOCA manager already exists, continuing...")
			return nil
		}
	}

	params := map[string]interface{}{
		"manager": managerName,
	}

	result, err := client.Call("virtio_fs_doca_manager_create", params)
	if err != nil {
		return fmt.Errorf("failed to create DOCA manager: %w", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.Infof("DOCA manager created successfully: %s", string(resultBytes))

	return nil
}

// VirtioFSTransportStart starts a virtio-fs transport
func VirtioFSTransportStart(client JSONRPCClient, transports []Transport) error {
	transportExists := false
	for _, transport := range transports {
		if transport.Name == "DOCA" {
			transportExists = true
			if transport.State == "started" {
				klog.Infof("DOCA transport already started")
				return nil
			}
		}
	}

	if !transportExists {
		return fmt.Errorf("DOCA transport does not exist")
	}

	params := map[string]interface{}{
		"transport_name": "DOCA",
	}

	result, err := client.Call("virtio_fs_transport_start", params)
	if err != nil {
		return fmt.Errorf("failed to start virtio-fs transport: %w", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.Infof("Transport started successfully: %s", string(resultBytes))

	return nil
}

// DOCAFunction represents a single DOCA function
type DOCAFunction struct {
	HotPluggable string `json:"hot pluggable"`
	PCIAddress   string `json:"pci_address"`
	VUID         string `json:"vuid"`
	FunctionType string `json:"function_type"`
}

// DOCAFunctionList represents a group of DOCA functions under a manager
type DOCAFunctionList struct {
	Manager      string         `json:"manager"`
	FunctionList []DOCAFunction `json:"Function List"`
}

// VirtioFSDOCAGetFunctions gets the list of DOCA functions
func VirtioFSDOCAGetFunctions(client JSONRPCClient) ([]DOCAFunctionList, error) {
	result, err := client.Call("virtio_fs_doca_get_functions", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get DOCA functions: %w", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	var functionLists []DOCAFunctionList
	if err := json.Unmarshal(resultBytes, &functionLists); err != nil {
		return nil, fmt.Errorf("failed to decode DOCA functions response: %w", err)
	}

	klog.V(4).Infof("Retrieved DOCA functions: %+v", functionLists)

	return functionLists, nil
}

// VirtioFSDOCAFunctionCreate creates a DOCA virtio-fs function
func VirtioFSDOCAFunctionCreate(client JSONRPCClient, managerName string) (string, error) {
	if managerName == "" {
		return "", fmt.Errorf("manager name is required")
	}

	params := map[string]interface{}{
		"manager": managerName,
	}

	klog.Infof("Creating DOCA function for manager: %s", managerName)
	result, err := client.Call("virtio_fs_doca_function_create", params)
	if err != nil {
		return "", fmt.Errorf("failed to create DOCA function: %w", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	var vuid string
	if err := json.Unmarshal(resultBytes, &vuid); err != nil {
		return "", fmt.Errorf("failed to decode DOCA function create response: %w", err)
	}

	klog.V(4).Infof("Created DOCA function with VUID: %s", vuid)

	return vuid, nil
}

// VirtioFSDOCAFunctionDestroy destroys a DOCA virtio-fs function
func VirtioFSDOCAFunctionDestroy(client JSONRPCClient, managerName string, vuid string) error {
	if managerName == "" || vuid == "" {
		return fmt.Errorf("manager name and VUID are required")
	}

	params := map[string]interface{}{
		"manager": managerName,
		"vuid":    vuid,
	}

	klog.Infof("Destroying DOCA function with VUID: %s for manager: %s", vuid, managerName)
	result, err := client.Call("virtio_fs_doca_function_destroy", params)
	if err != nil {
		return fmt.Errorf("failed to destroy DOCA function: %w", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.Infof("DOCA function destroyed successfully: %s", string(resultBytes))

	return nil
}

// VirtioFSDeviceCreate creates a virtio-fs device
func VirtioFSDeviceCreate(client JSONRPCClient, deviceName string, parameters map[string]string) error {
	if deviceName == "" {
		return fmt.Errorf("device name is required")
	}

	numQueues := 8 // default value
	if queueStr, exists := parameters["num_request_queues"]; exists {
		if q, err := strconv.Atoi(queueStr); err == nil {
			numQueues = q
		}
	}

	params := map[string]interface{}{
		"transport_name":     "DOCA",
		"dev_name":           "dev_" + deviceName,
		"tag":                deviceName + "tag",
		"fsdev":              deviceName,
		"num_request_queues": numQueues,
		"queue_size":         256,
		"driver_platform":    "x86_64",
	}

	klog.Infof("Creating virtio-fs device with params: %+v", params)
	result, err := client.Call("virtio_fs_device_create", params)
	if err != nil {
		return fmt.Errorf("failed to create virtio-fs device: %w", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.Infof("Device created successfully: %s", string(resultBytes))

	return nil
}

// VirtioFSDOCADeviceModify modifies a DOCA virtio-fs device
func VirtioFSDOCADeviceModify(client JSONRPCClient, managerName string, deviceName string, vuid string) error {
	if managerName == "" || deviceName == "" || vuid == "" {
		return fmt.Errorf("manager name, device name and VUID are required")
	}

	params := map[string]interface{}{
		"dev_name": "dev_" + deviceName,
		"manager":  managerName,
		"vuid":     vuid,
	}

	klog.Infof("Modifying DOCA device with params: %+v", params)
	result, err := client.Call("virtio_fs_doca_device_modify", params)
	if err != nil {
		return fmt.Errorf("failed to modify DOCA device: %w", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.Infof("Device modified successfully: %s", string(resultBytes))

	return nil
}

// VirtioFSDeviceStart starts a virtio-fs device
func VirtioFSDeviceStart(client JSONRPCClient, deviceName string, devices []FSDevice) error {
	deviceExists := false
	devName := "dev_" + deviceName
	for _, device := range devices {
		if device.Name == devName {
			deviceExists = true
			if device.State == "running" {
				klog.Infof("virtio-fs device already started")
				return nil
			}
		}
	}

	if !deviceExists {
		return fmt.Errorf("virtio-fs device does not exist")
	}

	params := map[string]interface{}{
		"dev_name": devName,
	}

	result, err := client.Call("virtio_fs_device_start", params)
	if err != nil {
		return fmt.Errorf("failed to start virtio-fs device: %w", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.Infof("Device started successfully: %s", string(resultBytes))

	return nil
}

// VirtioFSDeviceStop stops a virtio-fs device
func VirtioFSDeviceStop(client JSONRPCClient, deviceName string) error {
	devName := "dev_" + deviceName
	params := map[string]interface{}{
		"dev_name": devName,
	}

	klog.Infof("Stopping virtio-fs device: %s", deviceName)
	result, err := client.Call("virtio_fs_device_stop", params)
	if err != nil {
		return fmt.Errorf("failed to stop virtio-fs device: %w", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.Infof("Device stopped successfully: %s", string(resultBytes))

	return nil
}

// VirtioFSDeviceDestroy destroys a virtio-fs device
func VirtioFSDeviceDestroy(client JSONRPCClient, deviceName string) error {
	devName := "dev_" + deviceName
	params := map[string]interface{}{
		"dev_name": devName,
	}

	klog.Infof("Destroying virtio-fs device: %s", devName)
	result, err := client.Call("virtio_fs_device_destroy", params)
	if err != nil {
		return fmt.Errorf("failed to destroy virtio-fs device: %w", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.Infof("Device destroyed successfully: %s", string(resultBytes))

	return nil
}

// VirtioFSTransportStop stops a virtio-fs transport
func VirtioFSTransportStop(client JSONRPCClient) error {
	params := map[string]interface{}{
		"transport_name": "DOCA",
	}

	klog.Info("Stopping virtio-fs transport")
	result, err := client.Call("virtio_fs_transport_stop", params)
	if err != nil {
		return fmt.Errorf("failed to stop virtio-fs transport: %w", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.Infof("Transport stopped successfully: %s", string(resultBytes))

	return nil
}

// VirtioFSDOCAManagerDestroy destroys a DOCA virtio-fs emulation manager
func VirtioFSDOCAManagerDestroy(client JSONRPCClient, managerName string) error {
	if managerName == "" {
		return fmt.Errorf("manager name is required")
	}

	params := map[string]interface{}{
		"manager": managerName,
	}

	klog.Info("Destroying DOCA manager")
	result, err := client.Call("virtio_fs_doca_manager_destroy", params)
	if err != nil {
		return fmt.Errorf("failed to destroy DOCA manager: %w", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.Infof("DOCA manager destroyed successfully: %s", string(resultBytes))

	return nil
}

// VirtioFSTransportDestroy destroys a virtio-fs transport
func VirtioFSTransportDestroy(client JSONRPCClient) error {
	params := map[string]interface{}{
		"transport_name": "DOCA",
	}

	klog.Info("Destroying virtio-fs transport")
	result, err := client.Call("virtio_fs_transport_destroy", params)
	if err != nil {
		return fmt.Errorf("failed to destroy virtio-fs transport: %w", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.Infof("Transport destroyed successfully: %s", string(resultBytes))

	return nil
}

// VirtioFSDOCADeviceHotplug hotplugs a DOCA virtio-fs device
func VirtioFSDOCADeviceHotplug(client JSONRPCClient, deviceName string) error {
	devName := "dev_" + deviceName
	params := map[string]interface{}{
		"dev_name":      devName,
		"wait_for_done": true,
	}

	klog.Infof("Hotplugging DOCA device: %s", devName)
	result, err := client.Call("virtio_fs_doca_device_hotplug", params)
	if err != nil {
		return fmt.Errorf("failed to hotplug DOCA device: %w", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.Infof("Device hotplugged successfully: %s", string(resultBytes))

	return nil
}

// VirtioFSDOCADeviceHotunplug hotunplugs a DOCA virtio-fs device
func VirtioFSDOCADeviceHotunplug(client JSONRPCClient, deviceName string) error {
	devName := "dev_" + deviceName
	params := map[string]interface{}{
		"dev_name":      devName,
		"wait_for_done": true,
	}

	klog.Infof("Hotunplugging DOCA device: %s", devName)
	result, err := client.Call("virtio_fs_doca_device_hotunplug", params)
	if err != nil {
		return fmt.Errorf("failed to hotunplug DOCA device: %w", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.Infof("Device hotunplugged successfully: %s", string(resultBytes))

	return nil
}

// Transport represents a virtio-fs transport
type Transport struct {
	Name     string        `json:"name"`
	State    string        `json:"state"`
	Managers []DOCAManager `json:"managers"`
}

// VirtioFSGetTransports returns the list of registered virtio-fs transports
func VirtioFSGetTransports(client JSONRPCClient) ([]Transport, error) {
	klog.Infof("Getting virtio-fs transports")
	result, err := client.Call("virtio_fs_get_transports", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get virtio-fs transports: %w", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")

	var transports []Transport
	if err := json.Unmarshal(resultBytes, &transports); err != nil {
		return nil, fmt.Errorf("failed to decode transport response: %w", err)
	}

	klog.V(4).Infof("Retrieved transports: %+v", transports)
	return transports, nil
}

// DOCAManager represents a DOCA manager name
type DOCAManager struct {
	Name string `json:"name"`
}

// VirtioFSDOCAGetManagers returns the list of existing DOCA transport managers
func VirtioFSDOCAGetManagers(client JSONRPCClient) ([]DOCAManager, error) {
	klog.Info("Getting DOCA transport managers")
	result, err := client.Call("virtio_fs_doca_get_managers", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get DOCA managers: %w", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")

	var managers []DOCAManager
	if err := json.Unmarshal(resultBytes, &managers); err != nil {
		return nil, fmt.Errorf("failed to decode managers response: %w", err)
	}

	klog.V(4).Infof("Retrieved DOCA managers: %+v", managers)
	return managers, nil
}

// FSDevice represents a virtio-fs device
type FSDevice struct {
	Name             string `json:"name"`
	TransportName    string `json:"transport_name"`
	State            string `json:"state"`
	Fsdev            string `json:"fsdev"`
	Tag              string `json:"tag"`
	QueueSize        int    `json:"queue_size"`
	NumRequestQueues int    `json:"num_request_queues"`
}

// VirtioFSGetDevices returns the list of virtio-fs devices
func VirtioFSGetDevices(client JSONRPCClient) ([]FSDevice, error) {
	klog.Infof("Getting virtio-fs devices")
	result, err := client.Call("virtio_fs_get_devices", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get virtio-fs devices: %w", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")

	var devices []FSDevice
	if err := json.Unmarshal(resultBytes, &devices); err != nil {
		return nil, fmt.Errorf("failed to decode devices response: %w", err)
	}

	klog.V(4).Infof("Retrieved devices: %+v", devices)
	return devices, nil
}

func VirtioFSDeviceExists(devices []FSDevice, deviceName string) bool {
	devName := "dev_" + deviceName
	for _, device := range devices {
		if device.Name == devName {
			return true
		}
	}
	return false
}

// GetPCIAddressByVUID retrieves the PCI address associated with a given VUID
func GetPCIAddressByVUID(functionLists []DOCAFunctionList, vuid string) (string, error) {
	for _, list := range functionLists {
		for _, function := range list.FunctionList {
			if function.VUID == vuid {
				klog.V(4).Infof("Found PCI address %s for VUID %s", function.PCIAddress, vuid)
				return function.PCIAddress, nil
			}
		}
	}

	return "", fmt.Errorf("no PCI address found for VUID: %s", vuid)
}

// GetVUIDByPCIAddress retrieves the VUID associated with a given PCI address
func GetVUIDByPCIAddress(functionLists []DOCAFunctionList, pciAddress string) (string, error) {
	for _, list := range functionLists {
		for _, function := range list.FunctionList {
			if function.PCIAddress == pciAddress {
				klog.V(4).Infof("Found VUID %s for PCI address %s", function.VUID, pciAddress)
				return function.VUID, nil
			}
		}
	}

	return "", fmt.Errorf("no VUID found for PCI address: %s", pciAddress)
}
