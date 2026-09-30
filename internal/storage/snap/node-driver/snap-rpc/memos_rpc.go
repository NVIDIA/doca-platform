/*
Copyright 2026 NVIDIA

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

// NVMe front end of a CMX key/value volume: the PCI switch and doca_nvme_* RPCs
// ExposeMemosDevice and DestroyMemosDevice drive.
//
// The MEMOS initiator side - the device, the target connection and the volume
// itself - is not here. The nvcache storage vendor DPU plugin owns it and hands
// the volume name back as the attachment's device name, which this side takes as
// the backend of an NVMe KV namespace. The split matches the block path, where
// the plugin attaches the remote bdev and the node-driver only wraps it in an
// emulated function.

import (
	"encoding/json"
	"fmt"
	"time"

	"k8s.io/klog/v2"
)

// snapStatusSuccess is the envelope status SNAP reports for a call it accepted.
const snapStatusSuccess = "SNAP_SUCCESS"

// MemosJSONRPCClient serves the RPCs in this file, which reach a SNAP that
// replies in a different shape than the one the block and filesystem paths talk
// to. It wraps any transport and differs from it only in Call.
//
// The nvme and virtio-fs RPCs address a SNAP4 service, whose reply carries the
// payload directly in the JSON-RPC result member and whose failures arrive in
// the JSON-RPC error member. The CMX path addresses a SNAP5 service, which puts
// {"status": ..., "result": <payload>} in the result member and never sets the
// error member at all, so a rejected call reaches a client as a well-formed
// success that only the envelope status distinguishes.
type MemosJSONRPCClient struct {
	JSONRPCClient
}

// NewMemosJSONRPCClient returns a client that reads SNAP5 replies.
func NewMemosJSONRPCClient(client JSONRPCClient) MemosJSONRPCClient {
	return MemosJSONRPCClient{JSONRPCClient: client}
}

// Call performs an RPC and returns the payload nested inside the reply envelope,
// which is nil for a method that reports only its status.
func (client MemosJSONRPCClient) Call(method string, params map[string]interface{}) (interface{}, error) {
	result, err := client.call(method, params)
	if err != nil {
		return nil, err
	}

	envelope, ok := result.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("%s: unexpected response shape %T, want an object", method, result)
	}

	status, ok := envelope["status"].(string)
	if !ok {
		// doca_nvme_get_emulation_functions (and a few other listings) put the
		// payload in the JSON-RPC result with no SNAP_SUCCESS envelope.
		return envelope, nil
	}
	if status != snapStatusSuccess {
		if message, _ := envelope["message"].(string); message != "" {
			return nil, fmt.Errorf("%s: %s: %s", method, status, message)
		}
		return nil, fmt.Errorf("%s: %s", method, status)
	}

	return envelope["result"], nil
}

// call is the SNAP5 transport. The live SNAP socket is newline-delimited and
// needs a read deadline; the SNAP4 client in snap_rpc.go does neither, so this
// path frames the request itself. Mocks keep using the embedded Call.
func (client MemosJSONRPCClient) call(method string, params map[string]interface{}) (interface{}, error) {
	snap, ok := client.JSONRPCClient.(*JSONRPCSnapClient)
	if !ok {
		return client.JSONRPCClient.Call(method, params)
	}

	klog.V(4).Infof("Sending MEMOS SNAP RPC (method=%s)", method)
	if _, err := snap.Send(method, params); err != nil {
		return nil, err
	}
	if snap.Sock != nil {
		if _, err := snap.Sock.Write([]byte{'\n'}); err != nil {
			return nil, fmt.Errorf("%s: failed to write SNAP RPC newline: %w", method, err)
		}
		if snap.timeout > 0 {
			if err := snap.Sock.SetReadDeadline(time.Now().Add(snap.timeout)); err != nil {
				return nil, fmt.Errorf("%s: failed to set SNAP RPC read deadline: %w", method, err)
			}
			defer func() { _ = snap.Sock.SetReadDeadline(time.Time{}) }()
		}
	}

	response, err := snap.Recv()
	if err != nil {
		return nil, err
	}
	return response["result"], nil
}

/*
 * PCI switch RPCs.
 *
 * The KV controller is exposed through a hotplug PCI switch port rather than a
 * static function, so the port is created and destroyed explicitly. Visibility
 * to the host is the controller hotplug RPC, not a raw port power change:
 *
 *	pci_switch_port_create                      allocate the PCIe function, returns its VUID
 *	doca_nvme_subsystem_controller_hotplug      make the controller visible to the host
 *	doca_nvme_subsystem_controller_hotunplug    retract the controller from the host
 *	pci_switch_port_destroy                     release the function after the controller is gone
 */

// Power states accepted by PCISwitchPortSetPower.
const (
	PCIPowerOn  = "on"
	PCIPowerOff = "off"
)

// Hotplug states reported by PCISwitchShowInfo. They are distinct from the values
// set through PCISwitchPortSetPower.
const (
	PCIPortStatePowerOn          = "power_on"
	PCIPortStatePowerOff         = "power_off"
	PCIPortStatePlugInProgress   = "plug_in_progress"
	PCIPortStateUnplugInProgress = "unplug_in_progress"
)

// PCIPortTypeNVMe is the port type that exposes an NVMe controller.
const PCIPortTypeNVMe = "nvme"

// PCISwitchPort is a hotplug port on a PCI switch.
type PCISwitchPort struct {
	VUID string `json:"vuid"`
	// BDF is absent until the port is powered on.
	BDF   string `json:"bdf"`
	Type  string `json:"type"`
	Power string `json:"power"`
}

// PCISwitch is an entry of pci_switch_show_info.
type PCISwitch struct {
	Name     string          `json:"name"`
	Types    []string        `json:"types"`
	MaxPorts int             `json:"max_ports"`
	Ports    []PCISwitchPort `json:"ports"`
}

// pciSwitchShowInfoResponse wraps the switch array the RPC nests under a key,
// unlike the MEMOS listings which are bare arrays.
type pciSwitchShowInfoResponse struct {
	Switches []PCISwitch `json:"switches"`
}

// pciSwitchPortCreateResponse carries the VUID assigned to the new port.
type pciSwitchPortCreateResponse struct {
	VUID string `json:"vuid"`
}

// PCISwitchShowInfo lists the PCI switches with their supported types and
// existing hotplug ports.
func PCISwitchShowInfo(client JSONRPCClient) ([]PCISwitch, error) {
	result, err := client.Call("pci_switch_show_info", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get PCI switch info: %w", err)
	}
	resultBytes, _ := json.MarshalIndent(result, "", "  ")

	var response pciSwitchShowInfoResponse
	if err := json.Unmarshal(resultBytes, &response); err != nil {
		return nil, fmt.Errorf("failed to decode PCI switch info response: %w", err)
	}

	klog.V(4).Infof("Retrieved PCI switches: %+v", response.Switches)

	return response.Switches, nil
}

// PCISwitchPortCreate creates a hotplug port of portType and returns its VUID.
func PCISwitchPortCreate(client JSONRPCClient, switchName, portType string) (string, error) {
	if switchName == "" || portType == "" {
		return "", fmt.Errorf("PCI switch name and port type are required")
	}

	params := map[string]interface{}{
		"switch": switchName,
		"type":   portType,
	}

	result, err := client.Call("pci_switch_port_create", params)
	if err != nil {
		return "", fmt.Errorf("failed to create PCI switch port: %w", err)
	}
	resultBytes, _ := json.MarshalIndent(result, "", "  ")

	var response pciSwitchPortCreateResponse
	if err := json.Unmarshal(resultBytes, &response); err != nil {
		return "", fmt.Errorf("failed to decode PCI switch port create response: %w", err)
	}
	if response.VUID == "" {
		return "", fmt.Errorf("PCI switch port create returned no VUID")
	}

	klog.V(4).Infof("PCI switch port created successfully: vuid=%s", response.VUID)

	return response.VUID, nil
}

// PCISwitchPortSetPower powers a port on or off. Powering on is what makes the
// controller appear to the host.
func PCISwitchPortSetPower(client JSONRPCClient, switchName, vuid, power string) error {
	if switchName == "" || vuid == "" {
		return fmt.Errorf("PCI switch name and VUID are required")
	}
	if power != PCIPowerOn && power != PCIPowerOff {
		return fmt.Errorf("invalid PCI power state %q, want %q or %q", power, PCIPowerOn, PCIPowerOff)
	}

	params := map[string]interface{}{
		"switch": switchName,
		"vuid":   vuid,
		"power":  power,
	}

	result, err := client.Call("pci_switch_port_set_power", params)
	if err != nil {
		return fmt.Errorf("failed to set PCI switch port power: %w", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.V(4).Infof("PCI switch port powered %s successfully: %s", power, string(resultBytes))

	return nil
}

// PCISwitchPortDestroy destroys a hotplug port by VUID.
func PCISwitchPortDestroy(client JSONRPCClient, switchName, vuid string) error {
	params := map[string]interface{}{
		"switch": switchName,
		"vuid":   vuid,
	}

	result, err := client.Call("pci_switch_port_destroy", params)
	if err != nil {
		return fmt.Errorf("failed to destroy PCI switch port: %w", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.V(4).Infof("PCI switch port destroyed successfully: %s", string(resultBytes))

	return nil
}

// FindPCISwitchPort locates a port by VUID and reports the switch owning it.
func FindPCISwitchPort(switches []PCISwitch, vuid string) (PCISwitchPort, string, bool) {
	for _, sw := range switches {
		for _, port := range sw.Ports {
			if port.VUID == vuid {
				return port, sw.Name, true
			}
		}
	}
	return PCISwitchPort{}, "", false
}

// PCISwitchForType returns the name of a switch that advertises portType and
// still has a free hotplug port. The port type has to be created on a switch
// that advertises it, and a DPU normally has exactly one such switch. Switches
// whose ports are exhausted are skipped so port creation is not attempted on a
// switch that cannot accept it.
func PCISwitchForType(switches []PCISwitch, portType string) (string, error) {
	for _, sw := range switches {
		if sw.MaxPorts == 0 || len(sw.Ports) >= sw.MaxPorts {
			continue
		}
		for _, supported := range sw.Types {
			if supported == portType {
				return sw.Name, nil
			}
		}
	}
	return "", fmt.Errorf("no PCI switch has a free port for type %q", portType)
}

/*
 * NVMe emulation RPCs of the new doca_nvme_* API.
 *
 * These sit alongside the legacy nvme_* wrappers in snap_rpc.go, which drive the
 * block path and are untouched. The CMX flow needs the new API because only it
 * carries the KV command set and the caller-assigned subsystem, namespace and
 * controller handles.
 */

// Command Set Identifiers accepted by DocaNvmeSubsystemNsCreate. The KV command
// set is what makes a namespace reachable by libdoca_kv on the host.
const (
	CSINVM = "nvm"
	CSIKV  = "kv"
)

// maxIOQueues is the ceiling doca_nvme_subsystem_controller_create enforces on
// num_queues.
const maxIOQueues = 31

// maxNvmeControllerID is the ceiling doca_nvme_subsystem_controller_hotplug and
// hotunplug accept for cntl_id (0-0xFFEF).
const maxNvmeControllerID = 0xFFEF

// DocaNvmeSubsystem is an entry of doca_nvme_get_subsystems.
type DocaNvmeSubsystem struct {
	// SubsystemName is the handle every other RPC takes; it is independent of the
	// NQN, which is only what the host sees.
	SubsystemName string `json:"subsystem_name"`
	NQN           string `json:"nqn"`
	SN            string `json:"sn"`
	MN            string `json:"mn"`
	FR            string `json:"fr"`
	NN            int64  `json:"nn"`
	MNAN          int64  `json:"mnan"`
}

// DocaNvmeNamespace is an entry of doca_nvme_get_namespaces.
type DocaNvmeNamespace struct {
	NSID          int    `json:"ns_id"`
	CSI           string `json:"csi"`
	Backend       string `json:"backend"`
	SubsystemName string `json:"subsystem_name"`
	NQN           string `json:"nqn"`
	Inflight      int    `json:"inflight"`
}

// DocaNvmeController is an entry of doca_nvme_get_controllers.
type DocaNvmeController struct {
	CntlID        int    `json:"cntl_id"`
	SubsystemName string `json:"subsystem_name"`
	NQN           string `json:"nqn"`
	VUID          string `json:"vuid"`
	NumIOQueues   int    `json:"num_io_queues"`
	MDTS          int    `json:"mdts"`
}

// Kinds reported by doca_nvme_get_emulation_functions. A static function is
// always present on the DPU; a hotplug function is a PCI switch port.
const (
	FunctionKindStatic  = "static"
	FunctionKindHotplug = "hotplug"
)

// DocaNvmeEmulationFunction is an entry of doca_nvme_get_emulation_functions.
type DocaNvmeEmulationFunction struct {
	VUID string `json:"vuid"`
	// Kind is FunctionKindStatic or FunctionKindHotplug.
	Kind string `json:"kind"`
	BDF  string `json:"bdf"`
	// EmuMgr is the emulation manager (IB device) behind the function.
	EmuMgr string `json:"emu_mgr"`
	// Controller names the controller bound to the function, and is absent while
	// the function is free.
	Controller string `json:"controller"`
	NumVFs     int    `json:"num_vfs"`
	// VFs are the SR-IOV functions nested under a PF.
	VFs []DocaNvmeEmulationFunction `json:"vfs"`
}

// docaNvmeEmulationFunctionsResponse wraps the function array the RPC nests under
// a key, unlike the subsystem, namespace and controller listings which are bare
// arrays.
type docaNvmeEmulationFunctionsResponse struct {
	NvmeFunctions []DocaNvmeEmulationFunction `json:"nvme_functions"`
}

// DocaNvmeManager is an entry of doca_nvme_get_supported_managers.
type DocaNvmeManager struct {
	ManagerName    string `json:"manager_name"`
	InUse          bool   `json:"in_use"`
	MaxIOQueueSize int    `json:"max_io_queue_size"`
}

// DocaNvmeSubsystemCreateRequest describes an NVMe subsystem to create.
type DocaNvmeSubsystemCreateRequest struct {
	SubsystemName string
	NQN           string
	// SN, MN and FR override the SNAP defaults when set.
	SN string
	MN string
	FR string
}

// DocaNvmeNamespaceCreateRequest describes an NVMe namespace to create.
type DocaNvmeNamespaceCreateRequest struct {
	SubsystemName string
	NSID          int
	// BackendName is the backend the namespace is bound to; for CMX this is the
	// MEMOS volume name.
	BackendName string
	// CSI selects the command set; CSIKV for a CMX volume.
	CSI string
	// NGUID is the host-visible namespace identifier, 32 hex characters. It is
	// unrelated to the NGUID of the target namespace behind the MEMOS volume.
	NGUID string
}

// DocaNvmeControllerCreateRequest describes an NVMe controller to create.
type DocaNvmeControllerCreateRequest struct {
	SubsystemName string
	CntlID        int
	// VUID is the emulated function the controller is bound to, which for CMX is
	// the PCI switch port.
	VUID string
	// NumQueues and MDTS leave the SNAP defaults when zero.
	NumQueues int
	MDTS      int
}

// DocaNvmeGetSubsystems lists the NVMe subsystems.
func DocaNvmeGetSubsystems(client JSONRPCClient) ([]DocaNvmeSubsystem, error) {
	result, err := client.Call("doca_nvme_get_subsystems", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get NVMe subsystems: %w", err)
	}
	resultBytes, _ := json.MarshalIndent(result, "", "  ")

	var subsystems []DocaNvmeSubsystem
	if err := json.Unmarshal(resultBytes, &subsystems); err != nil {
		return nil, fmt.Errorf("failed to decode NVMe subsystems response: %w", err)
	}

	klog.V(4).Infof("Retrieved NVMe subsystems: %+v", subsystems)

	return subsystems, nil
}

// DocaNvmeSubsystemCreate creates an NVMe subsystem.
func DocaNvmeSubsystemCreate(client JSONRPCClient, req DocaNvmeSubsystemCreateRequest,
	subsystems []DocaNvmeSubsystem) error {
	if req.SubsystemName == "" || req.NQN == "" {
		return fmt.Errorf("subsystem name and NQN are required")
	}

	if DocaNvmeSubsystemExists(subsystems, req.SubsystemName) {
		klog.V(4).Infof("NVMe subsystem %s already exists, continuing...", req.SubsystemName)
		return nil
	}

	params := map[string]interface{}{
		"subsystem_name": req.SubsystemName,
		"nqn":            req.NQN,
	}
	for key, value := range map[string]string{"sn": req.SN, "mn": req.MN, "fr": req.FR} {
		if value != "" {
			params[key] = value
		}
	}

	result, err := client.Call("doca_nvme_subsystem_create", params)
	if err != nil {
		return fmt.Errorf("failed to create NVMe subsystem: %w", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.V(4).Infof("NVMe subsystem created successfully: %s", string(resultBytes))

	return nil
}

// DocaNvmeSubsystemDestroy destroys an NVMe subsystem.
func DocaNvmeSubsystemDestroy(client JSONRPCClient, subsystemName string) error {
	params := map[string]interface{}{
		"subsystem_name": subsystemName,
	}

	result, err := client.Call("doca_nvme_subsystem_destroy", params)
	if err != nil {
		return fmt.Errorf("failed to destroy NVMe subsystem: %w", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.V(4).Infof("NVMe subsystem destroyed successfully: %s", string(resultBytes))

	return nil
}

// DocaNvmeGetNamespaces lists the NVMe namespaces.
func DocaNvmeGetNamespaces(client JSONRPCClient) ([]DocaNvmeNamespace, error) {
	result, err := client.Call("doca_nvme_get_namespaces", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get NVMe namespaces: %w", err)
	}
	resultBytes, _ := json.MarshalIndent(result, "", "  ")

	var namespaces []DocaNvmeNamespace
	if err := json.Unmarshal(resultBytes, &namespaces); err != nil {
		return nil, fmt.Errorf("failed to decode NVMe namespaces response: %w", err)
	}

	klog.V(4).Infof("Retrieved NVMe namespaces: %+v", namespaces)

	return namespaces, nil
}

// DocaNvmeSubsystemNsCreate creates an NVMe namespace over a backend. For CMX the
// backend is a MEMOS volume, and creating the namespace opens it.
func DocaNvmeSubsystemNsCreate(client JSONRPCClient, req DocaNvmeNamespaceCreateRequest) error {
	if req.SubsystemName == "" || req.BackendName == "" {
		return fmt.Errorf("subsystem name and backend name are required")
	}
	if req.NSID < 1 {
		return fmt.Errorf("namespace ID must be at least 1, got %d", req.NSID)
	}
	if req.NGUID == "" {
		return fmt.Errorf("NGUID is required")
	}

	csi := req.CSI
	if csi == "" {
		csi = CSINVM
	}

	params := map[string]interface{}{
		"subsystem_name": req.SubsystemName,
		"ns_id":          req.NSID,
		"backend_name":   req.BackendName,
		"csi":            csi,
		"nguid":          req.NGUID,
	}

	result, err := client.Call("doca_nvme_subsystem_ns_create", params)
	if err != nil {
		return fmt.Errorf("failed to create NVMe namespace: %w", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.V(4).Infof("NVMe namespace created successfully: %s", string(resultBytes))

	return nil
}

// DocaNvmeSubsystemNsDestroy detaches and destroys an NVMe namespace, releasing
// its reference on the backend.
func DocaNvmeSubsystemNsDestroy(client JSONRPCClient, subsystemName string, nsid int) error {
	params := map[string]interface{}{
		"subsystem_name": subsystemName,
		"ns_id":          nsid,
	}

	result, err := client.Call("doca_nvme_subsystem_ns_destroy", params)
	if err != nil {
		return fmt.Errorf("failed to destroy NVMe namespace: %w", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.V(4).Infof("NVMe namespace destroyed successfully: %s", string(resultBytes))

	return nil
}

// DocaNvmeGetControllers lists the NVMe controllers.
func DocaNvmeGetControllers(client JSONRPCClient) ([]DocaNvmeController, error) {
	result, err := client.Call("doca_nvme_get_controllers", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get NVMe controllers: %w", err)
	}
	resultBytes, _ := json.MarshalIndent(result, "", "  ")

	var controllers []DocaNvmeController
	if err := json.Unmarshal(resultBytes, &controllers); err != nil {
		return nil, fmt.Errorf("failed to decode NVMe controllers response: %w", err)
	}

	klog.V(4).Infof("Retrieved NVMe controllers: %+v", controllers)

	return controllers, nil
}

// DocaNvmeSubsystemControllerCreate creates an NVMe controller bound to an
// emulated function. The controller lifecycle is inferred, so there is nothing
// to start or resume afterwards.
func DocaNvmeSubsystemControllerCreate(client JSONRPCClient, req DocaNvmeControllerCreateRequest) error {
	if req.SubsystemName == "" || req.VUID == "" {
		return fmt.Errorf("subsystem name and VUID are required")
	}
	if req.CntlID < 0 {
		return fmt.Errorf("controller ID must not be negative, got %d", req.CntlID)
	}
	if req.NumQueues > maxIOQueues {
		return fmt.Errorf("num_queues %d exceeds the maximum of %d", req.NumQueues, maxIOQueues)
	}

	params := map[string]interface{}{
		"subsystem_name": req.SubsystemName,
		"cntl_id":        req.CntlID,
		"vuid":           req.VUID,
	}
	if req.NumQueues > 0 {
		params["num_queues"] = req.NumQueues
	}
	if req.MDTS > 0 {
		params["mdts"] = req.MDTS
	}

	result, err := client.Call("doca_nvme_subsystem_controller_create", params)
	if err != nil {
		return fmt.Errorf("failed to create NVMe controller: %w", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.V(4).Infof("NVMe controller created successfully: %s", string(resultBytes))

	return nil
}

// DocaNvmeSubsystemControllerDestroy destroys an NVMe controller.
func DocaNvmeSubsystemControllerDestroy(client JSONRPCClient, subsystemName string, cntlID int) error {
	params := map[string]interface{}{
		"subsystem_name": subsystemName,
		"cntl_id":        cntlID,
	}

	result, err := client.Call("doca_nvme_subsystem_controller_destroy", params)
	if err != nil {
		return fmt.Errorf("failed to destroy NVMe controller: %w", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.V(4).Infof("NVMe controller destroyed successfully: %s", string(resultBytes))

	return nil
}

// DocaNvmeSubsystemControllerHotplug makes a controller visible to the host on
// the PCI bus. It is only valid for a controller bound to a hotplug PCI
// function; a controller on a static function is rejected by the service. The
// call blocks until the PCI function reaches a stable power state.
func DocaNvmeSubsystemControllerHotplug(client JSONRPCClient, subsystemName string, cntlID int) error {
	params, err := docaNvmeControllerPlugParams(subsystemName, cntlID)
	if err != nil {
		return err
	}

	result, err := client.Call("doca_nvme_subsystem_controller_hotplug", params)
	if err != nil {
		return fmt.Errorf("failed to hotplug NVMe controller: %w", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.V(4).Infof("NVMe controller hotplugged successfully: %s", string(resultBytes))

	return nil
}

// DocaNvmeSubsystemControllerHotunplug makes a controller invisible to the host
// on the PCI bus. Same parameters and restrictions as hotplug.
func DocaNvmeSubsystemControllerHotunplug(client JSONRPCClient, subsystemName string, cntlID int) error {
	params, err := docaNvmeControllerPlugParams(subsystemName, cntlID)
	if err != nil {
		return err
	}

	result, err := client.Call("doca_nvme_subsystem_controller_hotunplug", params)
	if err != nil {
		return fmt.Errorf("failed to hotunplug NVMe controller: %w", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.V(4).Infof("NVMe controller hotunplugged successfully: %s", string(resultBytes))

	return nil
}

// docaNvmeControllerPlugParams is the shared argument set of hotplug and
// hotunplug: an existing subsystem and a controller ID in 0-0xFFEF.
func docaNvmeControllerPlugParams(subsystemName string, cntlID int) (map[string]interface{}, error) {
	if subsystemName == "" {
		return nil, fmt.Errorf("subsystem name is required")
	}
	if cntlID < 0 || cntlID > maxNvmeControllerID {
		return nil, fmt.Errorf("controller ID %d is out of range 0-%#x", cntlID, maxNvmeControllerID)
	}

	return map[string]interface{}{
		"subsystem_name": subsystemName,
		"cntl_id":        cntlID,
	}, nil
}

// DocaNvmeGetEmulationFunctions lists the NVMe emulation functions with their
// VUIDs.
func DocaNvmeGetEmulationFunctions(client JSONRPCClient) ([]DocaNvmeEmulationFunction, error) {
	result, err := client.Call("doca_nvme_get_emulation_functions", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get NVMe emulation functions: %w", err)
	}
	resultBytes, _ := json.MarshalIndent(result, "", "  ")

	var response docaNvmeEmulationFunctionsResponse
	if err := json.Unmarshal(resultBytes, &response); err != nil {
		return nil, fmt.Errorf("failed to decode NVMe emulation functions response: %w", err)
	}

	klog.V(4).Infof("Retrieved NVMe emulation functions: %+v", response.NvmeFunctions)

	return response.NvmeFunctions, nil
}

// DocaNvmeGetSupportedManagers lists the emulation managers the NVMe module can
// use, which is the candidate set for the manager list of DocaNvmeSetConfig.
func DocaNvmeGetSupportedManagers(client JSONRPCClient) ([]DocaNvmeManager, error) {
	result, err := client.Call("doca_nvme_get_supported_managers", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get NVMe supported managers: %w", err)
	}
	resultBytes, _ := json.MarshalIndent(result, "", "  ")

	var managers []DocaNvmeManager
	if err := json.Unmarshal(resultBytes, &managers); err != nil {
		return nil, fmt.Errorf("failed to decode NVMe supported managers response: %w", err)
	}

	klog.V(4).Infof("Retrieved NVMe supported managers: %+v", managers)

	return managers, nil
}

// DocaNvmeSetConfig selects the emulation managers and network devices the NVMe
// module uses. Every subsystem depends on this configuration, so it has to run
// before the first subsystem is created.
func DocaNvmeSetConfig(client JSONRPCClient, managersDevList, networkDevList string) error {
	if managersDevList == "" {
		return fmt.Errorf("NVMe manager device list is required")
	}

	params := map[string]interface{}{
		"managers_dev_list": managersDevList,
	}
	if networkDevList != "" {
		params["network_dev_list"] = networkDevList
	}

	result, err := client.Call("doca_nvme_set_config", params)
	if err != nil {
		return fmt.Errorf("failed to set NVMe config: %w", err)
	}

	resultBytes, _ := json.MarshalIndent(result, "", "  ")
	klog.V(4).Infof("NVMe config set successfully: %s", string(resultBytes))

	return nil
}

// DocaNvmeManagerNames returns the manager names, in the comma-separated form
// DocaNvmeSetConfig takes.
func DocaNvmeManagerNames(managers []DocaNvmeManager) []string {
	names := make([]string, 0, len(managers))
	for _, manager := range managers {
		names = append(names, manager.ManagerName)
	}
	return names
}

// DocaNvmeFindFreeStaticFunction returns a static emulation function that has no
// controller bound to it. A static function is the non-hotplug way to expose a
// controller: it already exists on the DPU, so it is claimed rather than created.
func DocaNvmeFindFreeStaticFunction(functions []DocaNvmeEmulationFunction) (DocaNvmeEmulationFunction, bool) {
	for _, function := range functions {
		if function.Kind == FunctionKindStatic && function.Controller == "" {
			return function, true
		}
	}
	return DocaNvmeEmulationFunction{}, false
}

// DocaNvmeFindFunctionByBDF locates an emulation function by PCI BDF, including
// the VFs nested under each PF. This is the listing backfill uses to recover
// funcVUID from pciDeviceAddress.
func DocaNvmeFindFunctionByBDF(functions []DocaNvmeEmulationFunction, pciAddr string) (DocaNvmeEmulationFunction, bool) {
	if pciAddr == "" {
		return DocaNvmeEmulationFunction{}, false
	}
	for _, function := range functions {
		if function.BDF == pciAddr {
			return function, true
		}
		if vf, found := DocaNvmeFindFunctionByBDF(function.VFs, pciAddr); found {
			return vf, true
		}
	}
	return DocaNvmeEmulationFunction{}, false
}

// DocaNvmeFindFunctionByVUID locates an emulation function by VUID, including the
// VFs nested under each PF.
func DocaNvmeFindFunctionByVUID(functions []DocaNvmeEmulationFunction, vuid string) (DocaNvmeEmulationFunction, bool) {
	for _, function := range functions {
		if function.VUID == vuid {
			return function, true
		}
		if vf, found := DocaNvmeFindFunctionByVUID(function.VFs, vuid); found {
			return vf, true
		}
	}
	return DocaNvmeEmulationFunction{}, false
}

// DocaNvmeSubsystemExists reports whether subsystemName is among the listed
// subsystems.
func DocaNvmeSubsystemExists(subsystems []DocaNvmeSubsystem, subsystemName string) bool {
	for _, subsystem := range subsystems {
		if subsystem.SubsystemName == subsystemName {
			return true
		}
	}
	return false
}

// DocaNvmeFindNamespaceByBackend locates the namespace bound to backendName.
func DocaNvmeFindNamespaceByBackend(namespaces []DocaNvmeNamespace, backendName string) (DocaNvmeNamespace, bool) {
	for _, ns := range namespaces {
		if ns.Backend == backendName {
			return ns, true
		}
	}
	return DocaNvmeNamespace{}, false
}

// DocaNvmeFindNamespace locates a namespace by subsystem and namespace ID.
func DocaNvmeFindNamespace(namespaces []DocaNvmeNamespace, subsystemName string, nsid int) (DocaNvmeNamespace, bool) {
	for _, ns := range namespaces {
		if ns.SubsystemName == subsystemName && ns.NSID == nsid {
			return ns, true
		}
	}
	return DocaNvmeNamespace{}, false
}

// DocaNvmeFindControllerByVUID locates the controller bound to an emulated
// function. A function carries at most one controller, so the VUID identifies it.
func DocaNvmeFindControllerByVUID(controllers []DocaNvmeController, vuid string) (DocaNvmeController, bool) {
	for _, ctrl := range controllers {
		if ctrl.VUID == vuid {
			return ctrl, true
		}
	}
	return DocaNvmeController{}, false
}

// DocaNvmeNextControllerID returns the lowest free controller ID in a subsystem.
// Controller IDs are caller-assigned and start at 0.
func DocaNvmeNextControllerID(controllers []DocaNvmeController, subsystemName string) int {
	used := make(map[int]struct{}, len(controllers))
	for _, ctrl := range controllers {
		if ctrl.SubsystemName == subsystemName {
			used[ctrl.CntlID] = struct{}{}
		}
	}

	for cntlID := 0; ; cntlID++ {
		if _, taken := used[cntlID]; !taken {
			return cntlID
		}
	}
}
