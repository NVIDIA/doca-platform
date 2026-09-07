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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	snapstoragev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"

	"github.com/google/uuid"
	"k8s.io/klog/v2"
)

/* TODOs:
 * 1. Ensure that the RPC client is thread-safe and maintains connections.
 */

const NVMeProtocol = "NVME"

// subsystemNQNPrefix marks the subsystems this driver creates and owns. Teardown
// only destroys subsystems carrying it, so the subsystem declared in
// snapRpcInitConf is never removed even though volumes no longer live in it.
const subsystemNQNPrefix = "nqn.2022-10.io.nvda.nvme:dpf-"

// volumeNSID is the namespace ID given to every volume. Each volume owns a
// subsystem of its own, so namespace IDs no longer have to be unique across
// volumes and there is nothing to allocate.
const volumeNSID = 1

// maxNQNLength is the NVMe limit on the length of a qualified name.
const maxNQNLength = 223

// errSubsystemNQNRequired is returned by the subsystem-scoped RPCs when they are
// reached without an NQN. Every one of them addresses a single volume's
// subsystem, so there is nothing sensible to fall back to.
var errSubsystemNQNRequired = errors.New("subsystem NQN is required")

// SubsystemNQNForDevice returns the NQN of the subsystem that holds deviceName's
// namespace.
//
// It is a pure function of the device name so that a replay after a SNAP restart
// addresses the same subsystem without the NQN having to be persisted, and so
// that a subsystem orphaned by a failed teardown can still be found.
func SubsystemNQNForDevice(deviceName string) string {
	// The digest keeps the NQN unique even when sanitizing or truncating the
	// label below maps two different device names onto the same text.
	digest := sha256.Sum256([]byte(deviceName))
	suffix := hex.EncodeToString(digest[:4])

	label := sanitizeNQNLabel(deviceName)
	if limit := maxNQNLength - len(subsystemNQNPrefix) - len(suffix) - 1; len(label) > limit {
		label = label[:limit]
	}

	return subsystemNQNPrefix + label + "-" + suffix
}

// sanitizeNQNLabel keeps the device name readable inside the NQN while replacing
// characters that are not safe to embed in a qualified name.
func sanitizeNQNLabel(deviceName string) string {
	var label strings.Builder
	for _, r := range deviceName {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-':
			label.WriteRune(r)
		default:
			label.WriteRune('-')
		}
	}
	return label.String()
}

// isDriverOwnedSubsystem reports whether the driver created this subsystem and is
// therefore allowed to destroy it.
func isDriverOwnedSubsystem(nqn string) bool {
	return strings.HasPrefix(nqn, subsystemNQNPrefix)
}

// subsystemExists reports whether nqn is present in a subsystem listing.
func subsystemExists(subsystems NvmeSubsystemListResponse, nqn string) bool {
	for _, subsystem := range subsystems {
		if subsystem.NQN == nqn {
			return true
		}
	}
	return false
}

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

// NvmeSubsystemCreate creates the subsystem that will hold a single volume's
// namespace.
//
// A subsystem that is already there is not an error: expose is replayed on every
// reconcile and after a SNAP restart, and has to converge on whatever state it
// finds.
func NvmeSubsystemCreate(client JSONRPCClient, nqn string, subsystems NvmeSubsystemListResponse) error {
	if nqn == "" {
		return errSubsystemNQNRequired
	}

	if subsystemExists(subsystems, nqn) {
		klog.Infof("NVMe subsystem %s already exists, continuing...", nqn)
		return nil
	}

	// Only nqn is passed, leaving the rest at SNAP's defaults. In particular the
	// maximal namespace ID, nn, defaults to 0xFFFFFFFE, which is what lets a
	// namespace recorded under the previous shared-subsystem model be recreated
	// here at its original NSID rather than being forced down to 1.
	params := map[string]interface{}{
		"nqn": nqn,
	}

	result, err := client.Call("nvme_subsystem_create", params)
	if err != nil {
		// The listing can already be stale by the time the create runs, for
		// instance for a subsystem carried over by a SNAP live update. Asking
		// SNAP again decides whether that happened, which keeps the outcome
		// independent of how SNAP words an already-exists error.
		live, listErr := NvmeSubsystemList(client)
		if listErr == nil && subsystemExists(live, nqn) {
			klog.Infof("NVMe subsystem %s exists despite %v, continuing...", nqn, err)
			return nil
		}
		return fmt.Errorf("failed to create subsystem %s: %v", nqn, err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.Infof("Subsystem %s created successfully: %s", nqn, string(resultBytes))

	return nil
}

// NvmeSubsystemDestroy removes a volume's subsystem.
//
// force is deliberately not set. A subsystem that still holds a controller or a
// namespace means teardown ran out of order, and cascading the delete would hide
// that instead of surfacing it.
func NvmeSubsystemDestroy(client JSONRPCClient, nqn string) error {
	if nqn == "" {
		return errSubsystemNQNRequired
	}

	params := map[string]interface{}{
		"nqn": nqn,
	}

	result, err := client.Call("nvme_subsystem_destroy", params)
	if err != nil {
		return fmt.Errorf("failed to destroy subsystem %s: %v", nqn, err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.Infof("Subsystem %s destroyed successfully: %s", nqn, string(resultBytes))

	return nil
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

// NvmeNamespaceCreate creates a new NVMe namespace for a given device name inside
// the subsystem identified by nqn.
//
// The namespace ID is a constant: the subsystem holds this volume alone, so there
// is no shared ID space to pick a free slot out of. A namespace recorded by an
// earlier attach keeps its ID and UUID so the host sees the same namespace across
// a re-attach, including one recorded under the previous shared-subsystem model.
func NvmeNamespaceCreate(client JSONRPCClient, crdDeviceName string, nqn string,
	dpuStatus snapstoragev1.VolumeAttachmentStatusDPU) (int, string, error) {
	if nqn == "" {
		return 0, "", errSubsystemNQNRequired
	}

	nsid := volumeNSID
	var uuidStr string
	if dpuStatus.BdevAttrs.NVMeNsID > 0 && dpuStatus.BdevAttrs.NVMeUUID != "" {
		nsid = int(dpuStatus.BdevAttrs.NVMeNsID)
		uuidStr = dpuStatus.BdevAttrs.NVMeUUID
	} else {
		uuidStr = uuid.Must(uuid.NewRandom()).String()
	}

	params := map[string]interface{}{
		"bdev_type": "spdk",
		"nqn":       nqn,
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

// NvmeControllerCreate creates a new NVMe controller in the subsystem identified
// by nqn, on the PCIe function selected from pf_id and vf_id.
func NvmeControllerCreate(client JSONRPCClient, nqn string, emulationFunctions EmulationFunctionListResponse,
	dpuStatus snapstoragev1.VolumeAttachmentStatusDPU, parameters map[string]string, functionType string) (string, string, error) {
	if nqn == "" {
		return "", "", errSubsystemNQNRequired
	}

	pciBDF, err := getPCI(emulationFunctions, dpuStatus, parameters, functionType)
	if err != nil {
		return "", "", err
	}

	params := getControllerParams(nqn, pciBDF, parameters)
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

// NvmeFunctionCreate creates (attaches/plugs) an NVMe device in POWER_OFF state (not visible to host yet)
func NvmeFunctionCreate(client JSONRPCClient) (string, error) {
	result, err := client.Call("nvme_function_create", nil)
	if err != nil {
		return "", fmt.Errorf("failed to create NVMe function: %v", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.Infof("NVMe function created successfully: %s", string(resultBytes))

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("unexpected response format")
	}

	vuid, ok := resultMap["vuid"].(string)
	if !ok {
		return "", fmt.Errorf("vuid not found or not a string in response")
	}

	return vuid, nil
}

// NvmeControllerHotplug hot-plugs an NVMe controller to make it visible to the host PCI subsystem
func NvmeControllerHotplug(client JSONRPCClient, ctrlID string) error {
	params := map[string]interface{}{
		"ctrl_id":       ctrlID,
		"wait_for_done": true,
	}

	result, err := client.Call("nvme_controller_hotplug", params)
	if err != nil {
		return fmt.Errorf("failed to hotplug controller: %v", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.Infof("Controller hotplugged successfully: %s", string(resultBytes))

	return nil
}

// NvmeControllerHotunplug hot-unplugs an NVMe controller to make it not visible to the host PCI subsystem
func NvmeControllerHotunplug(client JSONRPCClient, ctrlID string) error {
	params := map[string]interface{}{
		"ctrl_id":       ctrlID,
		"wait_for_done": true,
	}

	result, err := client.Call("nvme_controller_hotunplug", params)
	if err != nil {
		return fmt.Errorf("failed to hotunplug controller: %v", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.Infof("Controller hotunplugged successfully: %s", string(resultBytes))

	return nil
}

// NvmeFunctionDestroy destroys (detaches/unplugs) an NVMe device
func NvmeFunctionDestroy(client JSONRPCClient, vuid string) error {
	params := map[string]interface{}{
		"vuid": vuid,
	}

	result, err := client.Call("nvme_function_destroy", params)
	if err != nil {
		return fmt.Errorf("failed to destroy NVMe function: %v", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.Infof("NVMe function destroyed successfully: %s", string(resultBytes))

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

// NvmeNamespaceDestroy destroys an NVMe namespace in the subsystem identified by
// nqn.
func NvmeNamespaceDestroy(client JSONRPCClient, nqn string, nsid int) error {
	if nqn == "" {
		return errSubsystemNQNRequired
	}

	params := map[string]interface{}{
		"nqn":  nqn,
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

// getNamespaceByDeviceName retrieves the namespace ID (NSID) and UUID associated with a given block device name.
// Returns an NSID of -1 when no namespace matches the device.
// The owning NQN is returned alongside the namespace so callers can address it
// without assuming which subsystem it lives in. A volume attached before the
// driver moved to a subsystem per volume is still found in the shared subsystem.
func getNamespaceByDeviceName(deviceName string, subsystems NvmeSubsystemListResponse) (string, int, string) {
	for _, subsystem := range subsystems {
		for _, ns := range subsystem.Namespaces {
			if ns.Bdev == deviceName {
				klog.Infof("Namespace found for device %s: NQN=%s, NSID=%d, UUID=%s",
					deviceName, subsystem.NQN, ns.NSID, ns.UUID)
				return subsystem.NQN, ns.NSID, ns.UUID
			}
		}
	}

	klog.Infof("No namespace found for device %s", deviceName)
	return "", -1, ""
}

// checkNamespaceAttached checks if the namespace exists and if it is attached to the specified controller.
// Returns: namespaceExists (true if nsid is found in the nqn subsystem), attachedToCtrl (true if that namespace is attached to ctrlID).
//
// The search is scoped to a single subsystem: every volume now owns a subsystem
// and its namespace sits at the same ID, so an NSID on its own no longer
// identifies a namespace.
func checkNamespaceAttached(nqn string, nsid int, ctrlID string, subsystems NvmeSubsystemListResponse) (namespaceExists bool, attachedToCtrl bool) {
	namespaceExists = false
	attachedToCtrl = false

	for _, subsystem := range subsystems {
		if subsystem.NQN != nqn {
			continue
		}
		for _, ns := range subsystem.Namespaces {
			if ns.NSID != nsid {
				continue
			}
			namespaceExists = true

			// Found matching namespace, now check if it's attached to the controller
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
					attachedToCtrl = true
					return namespaceExists, attachedToCtrl
				}
			}
			return namespaceExists, attachedToCtrl
		}
	}

	return namespaceExists, attachedToCtrl
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

// isControllerAttachedToNamespace checks if a controller is already attached to a namespace.
//
// Like checkNamespaceAttached, the namespace is identified by subsystem and NSID
// together, because the same NSID now appears in every volume's subsystem.
func isControllerAttachedToNamespace(ctrlID string, nqn string, nsid int, subsystems NvmeSubsystemListResponse) bool {
	for _, subsystem := range subsystems {
		if subsystem.NQN != nqn {
			continue
		}
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

// controllerSubsystemNQN returns the NQN of the subsystem holding ctrlID, or an
// empty string when no subsystem in the listing claims it.
func controllerSubsystemNQN(ctrlID string, subsystems NvmeSubsystemListResponse) string {
	for _, subsystem := range subsystems {
		for _, ctrl := range subsystem.Controllers {
			ctrlMap, ok := ctrl.(map[string]interface{})
			if !ok {
				continue
			}

			subsystemCtrlID, exists := ctrlMap["ctrl_id"].(string)
			if !exists {
				continue
			}

			if subsystemCtrlID == ctrlID {
				return subsystem.NQN
			}
		}
	}
	return ""
}

// resolveOwnedController returns the controller to tear down for deviceName,
// along with the PCI address it actually sits on.
func resolveOwnedController(deviceName string, nqn string, pciAddr string, subsystems NvmeSubsystemListResponse,
	emulationFunctions EmulationFunctionListResponse) (ctrlID string, ctrlPCIAddr string) {
	ctrlID = getNvmeControllerByPciAddr(pciAddr, emulationFunctions)

	// Only proven foreign ownership makes the recorded controller untouchable. An
	// empty ctrlID means the function carries no controller and so belongs to
	// nobody, and an owner the listing does not report leaves the address as
	// trustworthy as it was before.
	owner := controllerSubsystemNQN(ctrlID, subsystems)
	if ctrlID == "" || owner == "" || owner == nqn {
		return ctrlID, pciAddr
	}

	klog.Infof("Controller %s at recorded PCI address %s belongs to subsystem %s, not %s, leaving it to its owner",
		ctrlID, pciAddr, owner, nqn)

	ctrlID = getCtrlByDeviceName(deviceName, subsystems)
	if ctrlID == "" {
		return "", ""
	}

	ownedPCIAddr, err := getPciAddrByCtrlID(ctrlID, emulationFunctions, false)
	if err != nil {
		klog.Errorf("No PCI address for controller %s of device %s: %v", ctrlID, deviceName, err)
		return ctrlID, ""
	}

	return ctrlID, ownedPCIAddr
}

// getVUIDByCtrlID retrieves the emulated function VUID for a controller ID.
// For VFs this returns the parent PF VUID.
func getVUIDByCtrlID(ctrlID string, emulationFunctions EmulationFunctionListResponse, hotplug bool) (string, error) {
	pciBDF, err := getPciAddrByCtrlID(ctrlID, emulationFunctions, hotplug)
	if err != nil {
		return "", err
	}
	return getFunctionVUIDByPCIAddress(pciBDF, emulationFunctions)
}

// getFunctionVUIDByPCIAddress retrieves the function VUID for a PCI address.
// PFs (including hotplugged PFs) return their own VUID. VFs return their parent PF VUID.
func getFunctionVUIDByPCIAddress(pciAddress string, emulationFunctions EmulationFunctionListResponse) (string, error) {
	for _, emFunc := range emulationFunctions {
		if emFunc.EmulationType != NVMeProtocol {
			continue
		}
		if emFunc.PCIBDF == pciAddress {
			if emFunc.VUID == "" {
				return "", fmt.Errorf("VUID is empty for NVMe PF at PCI address %s", pciAddress)
			}
			return emFunc.VUID, nil
		}
		for _, vf := range emFunc.VFs {
			if vf.PCIBDF == pciAddress {
				if emFunc.VUID == "" {
					return "", fmt.Errorf("parent PF VUID is empty for NVMe VF at PCI address %s", pciAddress)
				}
				return emFunc.VUID, nil
			}
		}
	}
	return "", fmt.Errorf("no NVMe function found for PCI address %s", pciAddress)
}

// getHotplugVUIDByPCIAddress retrieves the VUID of a hotplugged NVMe device by PCI address
func getHotplugVUIDByPCIAddress(pciAddress string, emulationFunctions EmulationFunctionListResponse) string {
	for _, emFunc := range emulationFunctions {
		if emFunc.EmulationType == NVMeProtocol && emFunc.Hotplugged && emFunc.PCIBDF == pciAddress {
			return emFunc.VUID
		}
	}
	return ""
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
