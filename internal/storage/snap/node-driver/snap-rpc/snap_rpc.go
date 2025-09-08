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
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	snapstoragev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"

	"github.com/google/uuid"
	"k8s.io/klog/v2"
)

/* TODOs:
 * 1. Ensure that the RPC client is thread-safe and maintains connections.
 */

const NVMeProtocol = "NVME"

// JSONRPCClient defines the interface for JSON-RPC client operations
type JSONRPCClient interface {
	Send(method string, params map[string]interface{}) (int, error)
	Recv() (map[string]interface{}, error)
	Call(method string, params map[string]interface{}) (interface{}, error)
	Close() error
}

// JSONRPCSnapClient represents the client for JSON-RPC communication
type JSONRPCSnapClient struct {
	Sock      net.Conn
	requestID int
	timeout   time.Duration
}

// Namespace represents a single namespace entry
type Namespace struct {
	Bdev                  string        `json:"bdev"`
	Controllers           []interface{} `json:"controllers"`
	MaxInflightsPerWeight int           `json:"max inflights per weight"`
	NQN                   string        `json:"nqn"`
	NSID                  int           `json:"nsid"`
	Ready                 string        `json:"ready"`
	UUID                  string        `json:"uuid"`
}

// Subsystem represents a single subsystem entry
type Subsystem struct {
	Controllers []interface{} `json:"controllers"`
	MN          string        `json:"mn"`
	MNAN        int           `json:"mnan"`
	Namespaces  []Namespace   `json:"namespaces"`
	NN          int           `json:"nn"`
	NQN         string        `json:"nqn"`
	SN          string        `json:"sn"`
}

// NvmeSubsystemListResponse represents the entire response which is a list of subsystems
type NvmeSubsystemListResponse []Subsystem

// VF represents a single virtual-function entry
type VF struct {
	Hotplugged    bool   `json:"hotplugged"`
	EmulationType string `json:"emulation_type"`
	PFIndex       int    `json:"pf_index"`
	VFIndex       int    `json:"vf_index"`
	PCIBDF        string `json:"pci_bdf"`
	VHCAID        int    `json:"vhca_id"`
	VUID          string `json:"vuid"`
	CtrlID        string `json:"ctrl_id,omitempty"`
}

// EmulationFunction represents the structure of each emulation function in the response
type EmulationFunction struct {
	Hotplugged    bool   `json:"hotplugged"`
	EmulationType string `json:"emulation_type"`
	PFIndex       int    `json:"pf_index"`
	PCIBDF        string `json:"pci_bdf"`
	VHCAID        int    `json:"vhca_id"`
	VUID          string `json:"vuid"`
	CtrlID        string `json:"ctrl_id"`
	VFs           []VF   `json:"vfs"`
}

// EmulationFunctionListResponse represents the response, which is a list of emulation functions
type EmulationFunctionListResponse []EmulationFunction

// NewJSONRPCSnapClient initializes a new JSON-RPC Snap client
func NewJSONRPCSnapClient(sockPath string, timeout time.Duration) (*JSONRPCSnapClient, error) {
	client := &JSONRPCSnapClient{
		timeout: timeout,
	}

	if err := client.connect(sockPath); err != nil {
		return nil, err
	}

	return client, nil
}

// Close closes the underlying connection of the JSONRPCSnapClient.
func (client *JSONRPCSnapClient) Close() error {
	if client.Sock != nil {
		err := client.Sock.Close()
		client.Sock = nil
		return err
	}
	return nil
}

// connect establishes a connection to the server (TCP or Unix socket)
func (client *JSONRPCSnapClient) connect(uri string) error {
	var err error

	if uri[0] == '/' || uri[:5] == "unix:" {
		path := uri
		if uri[:5] == "unix:" {
			path = uri[5:]
		}
		client.Sock, err = net.Dial("unix", path)
	} else if uri[:6] == "tcp://" {
		client.Sock, err = net.DialTimeout("tcp", uri[6:], client.timeout)
	} else {
		return errors.New("unsupported socket address")
	}

	if err != nil {
		klog.Errorf("Error while connecting to %s: %v", uri, err)
		return fmt.Errorf("error while connecting to %s: %v", uri, err)
	}

	return nil
}

// Send sends a JSON-RPC request with the given method and parameters
func (client *JSONRPCSnapClient) Send(method string, params map[string]interface{}) (int, error) {
	client.requestID++
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"id":      client.requestID,
	}
	if params != nil {
		req["params"] = params
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		klog.Errorf("Failed to marshal request: %v", err)
		return 0, fmt.Errorf("failed to marshal request: %v", err)
	}

	_, err = client.Sock.Write(reqBytes)
	if err != nil {
		klog.Errorf("Failed to send request: %v", err)
		return 0, fmt.Errorf("failed to send request: %v", err)
	}

	return client.requestID, nil
}

// Recv reads a JSON-RPC response from the socket, decodes it, and returns the result
func (client *JSONRPCSnapClient) Recv() (map[string]interface{}, error) {
	var response map[string]interface{}

	reader := bufio.NewReader(client.Sock)
	decoder := json.NewDecoder(reader)

	if err := decoder.Decode(&response); err != nil {
		klog.Errorf("Failed to decode response: %v", err)
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	if errField, ok := response["error"]; ok {
		klog.Errorf("RPC error: %v", errField)
		return nil, fmt.Errorf("RPC error: %v", errField)
	}

	return response, nil
}

// Call combines Send and Recv for a single RPC call
func (client *JSONRPCSnapClient) Call(method string, params map[string]interface{}) (interface{}, error) {
	_, err := client.Send(method, params)

	if err != nil {
		return nil, err
	}

	response, err := client.Recv()

	if err != nil {
		return nil, err
	}

	return response["result"], nil
}

// NvmeSubsystemList retrieves the list of NVMe subsystems
func NvmeSubsystemList(client JSONRPCClient) (NvmeSubsystemListResponse, error) {
	result, err := client.Call("nvme_subsystem_list", nil)

	if err != nil {
		return nil, err
	}

	if result == nil {
		klog.Error("Received empty response from nvme_subsystem_list RPC call")
		return nil, fmt.Errorf("received empty response from RPC call")
	}

	resultBytes, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		klog.Errorf("Failed to marshal response for debugging: %v", err)
		return nil, fmt.Errorf("failed to marshal response for debugging: %v", err)
	}

	var subsystems NvmeSubsystemListResponse
	if err := json.Unmarshal(resultBytes, &subsystems); err != nil {
		klog.Errorf("Failed to unmarshal response: %v", err)
		return nil, fmt.Errorf("failed to unmarshal response: %v", err)
	}

	return subsystems, nil
}

// EmulationFunctionList retrieves and prints the list of emulation functions
func EmulationFunctionList(client JSONRPCClient) (EmulationFunctionListResponse, error) {
	params := map[string]interface{}{
		"all": true,
	}

	result, err := client.Call("emulation_function_list", params)
	if err != nil {
		return nil, err
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")

	var emulationFunctions EmulationFunctionListResponse
	if err := json.Unmarshal(resultBytes, &emulationFunctions); err != nil {
		klog.Errorf("Failed to unmarshal response: %v", err)
		return nil, fmt.Errorf("failed to unmarshal response: %v", err)
	}

	return emulationFunctions, nil
}

// NvmeNamespaceCreate creates a new NVMe namespace for a given device name
func NvmeNamespaceCreate(client JSONRPCClient, crdDeviceName string, subsystems NvmeSubsystemListResponse,
	dpuStatus snapstoragev1.VolumeAttachmentStatusDPU) (int, string, error) {
	if len(subsystems) == 0 {
		klog.Error("No subsystems found")
		return 0, "", fmt.Errorf("no subsystems found")
	}

	targetSubsystem := &subsystems[0]
	var nsid int
	var uuidStr string
	if dpuStatus.BdevAttrs.NVMeNsID > 0 && dpuStatus.BdevAttrs.NVMeUUID != "" {
		nsid = int(dpuStatus.BdevAttrs.NVMeNsID)
		uuidStr = dpuStatus.BdevAttrs.NVMeUUID
	} else {
		if len(targetSubsystem.Namespaces) == 0 {
			nsid = 1
		} else {
			nsid = targetSubsystem.Namespaces[0].NSID + 1
		}
		uuidStr = uuid.Must(uuid.NewRandom()).String()
	}

	params := map[string]interface{}{
		"bdev_type": "spdk",
		"nqn":       targetSubsystem.NQN,
		"nsid":      nsid,
		"uuid":      uuidStr,
		"bdev_name": crdDeviceName,
	}

	result, err := client.Call("nvme_namespace_create", params)

	if err != nil {
		return 0, "", fmt.Errorf("RPC call failed: %v", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.Infof("Namespace created successfully: %s", string(resultBytes))

	return nsid, uuidStr, nil
}

// NvmeControllerCreate creates a new NVMe controller with only nqn, pf_id, and vf_id
func NvmeControllerCreate(client JSONRPCClient, subsystems NvmeSubsystemListResponse, emulationFunctions EmulationFunctionListResponse,
	dpuStatus snapstoragev1.VolumeAttachmentStatusDPU, parameters map[string]string, functionType string) (string, string, error) {
	if len(subsystems) == 0 {
		klog.Error("No subsystems found")
		return "", "", fmt.Errorf("no subsystems found")
	}
	targetSubsystem := &subsystems[0]

	pciBDF, err := getPCI(emulationFunctions, dpuStatus, parameters, functionType)
	if err != nil {
		return "", "", err
	}

	params := getControllerParams(targetSubsystem.NQN, pciBDF, parameters)
	klog.Infof("Creating controller with params: %v", params)

	result, err := client.Call("nvme_controller_create", params)
	if err != nil {
		return "", "", fmt.Errorf("failed to create controller: %v", err)
	}

	ctrlID, ok := result.(map[string]interface{})["ctrl_id"].(string)
	if !ok {
		klog.Errorf("ctrl_id not found or not a string in response")
		return "", "", fmt.Errorf("ctrl_id not found or not a string in response")
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.Infof("Controller created successfully: %s", string(resultBytes))

	return ctrlID, pciBDF, nil
}

// NvmeControllerAttachNs attaches a namespace to a controller
func NvmeControllerAttachNs(client JSONRPCClient, ctrlID string, nsid int) error {
	params := map[string]interface{}{
		"nsid":    nsid,
		"ctrl_id": ctrlID,
	}

	result, err := client.Call("nvme_controller_attach_ns", params)
	if err != nil {
		return fmt.Errorf("failed to attach namespace: %v", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.Infof("Namespace attached successfully: %s", string(resultBytes))

	return nil
}

// NvmeControllerResume resumes a controller
func NvmeControllerResume(client JSONRPCClient, ctrlID string) error {
	// Prepare parameters
	params := map[string]interface{}{
		"ctrl_id": ctrlID,
	}

	result, err := client.Call("nvme_controller_resume", params)
	if err != nil {
		return fmt.Errorf("failed to resume controller: %v", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.Infof("Controller resumed successfully: %s", string(resultBytes))

	return nil
}

// NvmeControllerDestroy destroys an NVMe controller
func NvmeControllerDestroy(client JSONRPCClient, ctrlID string) error {
	params := map[string]interface{}{
		"ctrl_id": ctrlID,
		"force":   true,
	}

	result, err := client.Call("nvme_controller_destroy", params)
	if err != nil {
		return fmt.Errorf("failed to destroy controller: %v", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.Infof("Controller destroyed successfully: %s", string(resultBytes))

	return nil
}

// NvmeNamespaceDestroy destroys an NVMe namespace
func NvmeNamespaceDestroy(client JSONRPCClient, nsid int, subsystems NvmeSubsystemListResponse) error {
	if len(subsystems) == 0 {
		klog.Error("No subsystems found")
		return fmt.Errorf("no subsystems found")
	}
	targetSubsystem := &subsystems[0]

	params := map[string]interface{}{
		"nqn":  targetSubsystem.NQN,
		"nsid": nsid,
	}

	result, err := client.Call("nvme_namespace_destroy", params)
	if err != nil {
		return fmt.Errorf("failed to destroy namespace: %v", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.Infof("Namespace destroyed successfully: %s", string(resultBytes))

	return nil
}

// NvmeControllerDetachNs detaches a namespace to a controller
func NvmeControllerDetachNs(client JSONRPCClient, ctrlID string, nsid int) error {
	params := map[string]interface{}{
		"nsid":    nsid,
		"ctrl_id": ctrlID,
	}

	result, err := client.Call("nvme_controller_detach_ns", params)
	if err != nil {
		return fmt.Errorf("failed to detach namespace: %v", err)
	}

	resultBytes, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		klog.Errorf("Failed to marshal response: %v", err)
		return fmt.Errorf("failed to marshal response: %v", err)
	}

	klog.Infof("Namespace detached successfully: %s", string(resultBytes))

	return nil
}

// NvmeEmulationDeviceAttach attaches (plugs) an NVMe device to the host
func NvmeEmulationDeviceAttach(client JSONRPCClient) (string, error) {
	params := map[string]interface{}{
		"protocol": "nvme",
	}

	result, err := client.Call("nvme_emulation_device_attach", params)
	if err != nil {
		return "", fmt.Errorf("failed to attach NVMe emulation device: %v", err)
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("unexpected response format")
	}

	vuid, ok := resultMap["vuid"].(string)
	if !ok {
		return "", fmt.Errorf("vuid not found or not a string in response")
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.Infof("NVMe emulation device attached successfully: %s", string(resultBytes))

	return vuid, nil
}

// NvmeEmulationDeviceDetachPrepare prepares to detach (unplug) an NVMe device using its VUID
func NvmeEmulationDeviceDetachPrepare(client JSONRPCClient, vuid string) error {
	params := map[string]interface{}{
		"vuid": vuid,
	}

	result, err := client.Call("emulation_device_detach_prepare", params)
	if err != nil {
		return fmt.Errorf("failed to prepare NVMe emulation device detach: %v", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.Infof("NVMe emulation device detach preparation successful: %s", string(resultBytes))

	return nil
}

// NvmeEmulationDeviceDetach detaches (unplugs) an NVMe device from the host using its VUID
func NvmeEmulationDeviceDetach(client JSONRPCClient, vuid string) error {
	params := map[string]interface{}{
		"vuid":  vuid,
		"force": true,
	}

	result, err := client.Call("emulation_device_detach", params)
	if err != nil {
		return fmt.Errorf("failed to detach NVMe emulation device: %v", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.Infof("NVMe emulation device detached successfully: %s", string(resultBytes))

	return nil
}

// getNvmeControllerByPciAddr returns the controller ID for a given PCI BDF.
// It first checks Physical Functions, then scans all Virtual Functions belonging
// to each PF. Returns empty string if no match is found.
func getNvmeControllerByPciAddr(pciAddr string, emulationFunctions EmulationFunctionListResponse) string {
	for _, emFunc := range emulationFunctions {
		if emFunc.EmulationType != NVMeProtocol {
			continue
		}
		if emFunc.PCIBDF == pciAddr {
			return emFunc.CtrlID
		}
		for _, vf := range emFunc.VFs {
			if vf.PCIBDF == pciAddr {
				return vf.CtrlID
			}
		}
	}

	return ""
}

// getPciAddrByCtrlID retrieves the PCI address associated with a given NVMe controller ID
// If hotplug is true, only hotplugged PFs are considered valid; VFs are never treated as hotplugged.
func getPciAddrByCtrlID(ctrlID string, emulationFunctions EmulationFunctionListResponse, hotplug bool) (string, error) {
	for _, emFunc := range emulationFunctions {
		if emFunc.EmulationType != NVMeProtocol {
			continue
		}
		if emFunc.CtrlID == ctrlID {
			if hotplug && !emFunc.Hotplugged {
				return "", fmt.Errorf("found non-hotplugged function with ctrl ID %s while searching for hotplugged device", ctrlID)
			}
			return emFunc.PCIBDF, nil
		}
		for _, vf := range emFunc.VFs {
			if vf.CtrlID != ctrlID {
				continue
			}
			return vf.PCIBDF, nil
		}
	}

	return "", fmt.Errorf("no PCI address found for NVMe controller ID %s", ctrlID)
}

// getNamespaceByDeviceName retrieves the namespace ID (NSID) associated with a given block device name
func getNamespaceByDeviceName(deviceName string, subsystems NvmeSubsystemListResponse) int {
	for _, subsystem := range subsystems {
		for _, ns := range subsystem.Namespaces {
			if ns.Bdev == deviceName {
				klog.Infof("Namespace found for device %s: NSID=%d", deviceName, ns.NSID)
				return ns.NSID
			}
		}
	}

	klog.Infof("No namespace found for device %s", deviceName)
	return -1
}

// checkNamespaceAttached checks if the namespace exists and is attached to the specified controller
func checkNamespaceAttached(nsid int, ctrlID string, subsystems NvmeSubsystemListResponse) bool {
	for _, subsystem := range subsystems {
		for _, ns := range subsystem.Namespaces {
			if ns.NSID != nsid {
				continue
			}

			// Found matching namespace, now check if it's attached to the controller
			for _, ctrl := range ns.Controllers {
				ctrlMap, ok := ctrl.(map[string]interface{})
				if !ok {
					continue
				}

				attachedCtrlID := ctrlMap["ctrl_id"].(string)
				if attachedCtrlID == ctrlID {
					return true
				}
			}
			return false
		}
	}

	return false
}

// getCtrlByDeviceName retrieves the controller ID associated with a given block device name
func getCtrlByDeviceName(deviceName string, subsystems NvmeSubsystemListResponse) string {
	for _, subsystem := range subsystems {
		for _, ns := range subsystem.Namespaces {
			if ns.Bdev != deviceName {
				continue
			}
			for _, ctrl := range ns.Controllers {
				ctrlMap, ok := ctrl.(map[string]interface{})
				if !ok {
					continue
				}
				ctrlID, exists := ctrlMap["ctrl_id"].(string)
				if !exists {
					continue
				}
				klog.Infof("Controller found for device %s: CtrlID=%s", deviceName, ctrlID)
				return ctrlID
			}
		}
	}

	klog.Infof("No controller found for device %s", deviceName)
	return ""
}

// isControllerAttachedToNamespace checks if a controller is already attached to a namespace
func isControllerAttachedToNamespace(ctrlID string, nsid int, subsystems NvmeSubsystemListResponse) bool {
	for _, subsystem := range subsystems {
		for _, ns := range subsystem.Namespaces {
			if ns.NSID != nsid {
				continue
			}

			// Found the namespace, now check if the controller is attached
			for _, ctrl := range ns.Controllers {
				ctrlMap, ok := ctrl.(map[string]interface{})
				if !ok {
					continue
				}

				attachedCtrlID, exists := ctrlMap["ctrl_id"].(string)
				if !exists {
					continue
				}

				if attachedCtrlID == ctrlID {
					return true
				}
			}
			return false
		}
	}
	return false
}

// getNvmeHotplugByPciAddr retrieves the VUID and ctrl_id of a hotplugged NVMe device by PCI address
func getNvmeHotplugByPciAddr(pciAddr string, emulationFunctions EmulationFunctionListResponse) (string, string, error) {
	for _, emFunc := range emulationFunctions {
		if emFunc.EmulationType == NVMeProtocol && emFunc.Hotplugged && emFunc.PCIBDF == pciAddr {
			return emFunc.CtrlID, emFunc.VUID, nil
		}
	}

	return "", "", fmt.Errorf("no hotplugged NVMe device found for PCI address %s", pciAddr)
}

// getPCIByVUID finds PCI BDF for a hotplugged NVMe device by its VUID
func getPCIByVUID(emulationFunctions EmulationFunctionListResponse, vuid string) (string, error) {
	for _, emFunc := range emulationFunctions {
		if emFunc.EmulationType == NVMeProtocol && emFunc.Hotplugged && emFunc.VUID == vuid {
			return emFunc.PCIBDF, nil
		}
	}
	return "", fmt.Errorf("no device found with VUID %s", vuid)
}

// getPCIForStaticPF finds first available NVMe Physical Function without a controller
func getPCIForStaticPF(emulationFunctions EmulationFunctionListResponse) (string, error) {
	for _, emFunc := range emulationFunctions {
		if emFunc.EmulationType == NVMeProtocol && !emFunc.Hotplugged && emFunc.CtrlID == "" {
			return emFunc.PCIBDF, nil
		}
	}
	return "", fmt.Errorf("no available pci bdf found for pf")
}

// getPCIForVF finds first available NVMe Virtual Function without a controller
func getPCIForVF(emulationFunctions EmulationFunctionListResponse) (string, error) {
	for _, emFunc := range emulationFunctions {
		if emFunc.EmulationType != NVMeProtocol {
			continue
		}
		for _, vf := range emFunc.VFs {
			if vf.CtrlID == "" {
				return vf.PCIBDF, nil
			}
		}
	}
	return "", fmt.Errorf("no available pci bdf found for vf")
}

// getPCI selects the PCI BDF based on the following priority:
// 1. Use existing PCI address from DPU status if available
// 2. Find PCI address by VUID for hotplugged devices (fails if VUID provided but not found)
// 3. For PF: find first available NVMe PF without a controller
// 4. For VF: find first available NVMe VF without a controller
func getPCI(emulationFunctions EmulationFunctionListResponse, dpuStatus snapstoragev1.VolumeAttachmentStatusDPU,
	parameters map[string]string, functionType string) (string, error) {
	if dpuStatus.PCIDeviceAddress != "" {
		return dpuStatus.PCIDeviceAddress, nil
	}

	if vuid := parameters["vuid"]; vuid != "" {
		pciBDF, err := getPCIByVUID(emulationFunctions, vuid)
		return pciBDF, err
	}

	if functionType == "pf" {
		return getPCIForStaticPF(emulationFunctions)
	}

	// Handle VF case
	return getPCIForVF(emulationFunctions)
}

// convertStringMapToInterfaceMap converts a map[string]string to map[string]interface{}
func convertStringMapToInterfaceMap(input map[string]string) map[string]interface{} {
	result := make(map[string]interface{})
	for key, value := range input {
		if intVal, err := strconv.Atoi(value); err == nil {
			result[key] = intVal
			continue
		}
		if boolVal, err := strconv.ParseBool(value); err == nil {
			result[key] = boolVal
			continue
		}
		result[key] = value
	}
	return result
}

// getControllerParams builds the parameters for a controller creation request
func getControllerParams(nqn string, pciBDF string, parameters map[string]string) map[string]interface{} {
	params := convertStringMapToInterfaceMap(parameters)

	params["nqn"] = nqn
	params["suspended"] = true

	if pciBDF != "00:00.0" {
		params["pci_bdf"] = pciBDF
	} else if parameters["vuid"] != "" {
		params["vuid"] = parameters["vuid"]
	}

	return params
}
