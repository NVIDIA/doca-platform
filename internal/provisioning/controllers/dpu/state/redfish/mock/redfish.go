/*
Copyright 2025 NVIDIA

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

package mock

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"

	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"

	"k8s.io/klog/v2"
)

// Constants for repeated strings
const (
	defaultBMCPassword = "0penBmc"
	DpuSerialNumber    = "MT25066004C7"
	DpuOPN             = "900-9D3B4-00SV-EA0"
)

// RedfishMockServer represents a mock Redfish server for testing
type RedfishMockServer struct {
	server                        *httptest.Server
	bmcVersion                    string
	password                      string
	dpuMode                       string                   // Current DPU mode: "NicMode" or "DpuMode"
	secureBootEnable              bool                     // Configured/desired Secure Boot state (for next boot)
	secureBootCurrentBoot         bool                     // Actual Secure Boot state of current boot session
	secureBootError               bool                     // Simulate Secure Boot endpoint error for testing
	secureBootPatchError          bool                     // Simulate Secure Boot PATCH-only error for testing
	systemError                   bool                     // Simulate GetSystem endpoint error for testing
	resetSystemError              bool                     // Simulate ResetSystem endpoint error for testing
	productDescriptionError       bool                     // Simulate GetProductDescription endpoint error for testing
	taskProgressError             bool                     // Simulate CheckTaskProgress endpoint error for testing
	chassisError                  bool                     // Simulate GetChassis endpoint error for testing
	oemLastState                  string                   // Current ARM OS boot state: "OsIsRunning", "OsStarting", etc.
	bootLastState                 string                   // BootProgress.LastState for GET System (e.g. OSRunning on BF4)
	dpuVersion                    DpuVersion               // Current DPU version
	model                         string                   // DPU model string (optional override)
	taskState                     string                   // Current task state: "Completed", "Exception", etc.
	taskMessages                  []map[string]interface{} // Task messages for Exception state
	concurrentUpdateBusyRemaining int                      // Number of InstallBFB calls that return HTTP 400 "Another update is in progress"
	concurrentUpdateBusyServed    int                      // How many 400 "Another update" responses were actually sent
	taskHTTPStatus                int                      // Override HTTP status returned by GET task; 0 means default 200
	taskHTTPBody                  string                   // Override raw body when taskHTTPStatus != 0
	selEntries                    []client.SELEntry        // System Event Log entries returned by GET SEL/Entries
	hostPrivilegeError            bool                     // Simulate HostPrivilegeConfig endpoint error for testing
	hostPrivilegeMode             string                   // Current host privilege mode: "Privileged" or "Restricted"
}

type DpuVersion int

const (
	BF3 DpuVersion = 3
	BF4 DpuVersion = 4
)

// NewRedfishMockServer creates a new mock Redfish server
func NewRedfishMockServer(bmcVersion, password string) *RedfishMockServer {
	mock := &RedfishMockServer{
		bmcVersion:            bmcVersion,
		password:              password,
		dpuMode:               "DpuMode",                  // Default to DpuMode
		dpuVersion:            BF3,                        // Default to BF3
		hostPrivilegeMode:     "Privileged",               // Default to Privileged
		secureBootEnable:      true,                       // Default configured state: enabled
		secureBootCurrentBoot: true,                       // Default current boot state: enabled
		oemLastState:          "OsIsRunning",              // Default to OS running
		taskState:             "Completed",                // Default task state
		taskMessages:          []map[string]interface{}{}, // Default empty messages
	}

	mux := http.NewServeMux()

	// Systems collection (must be registered before the /redfish/v1/ prefix handler)
	mux.HandleFunc("/redfish/v1/Systems", mock.handleGetSystems)

	// Root service
	mux.HandleFunc("/redfish/v1/", mock.handleRootService)

	// Chassis
	mux.HandleFunc("/redfish/v1/Chassis/Card1", mock.handleGetChassis)

	// UpdateService
	mux.HandleFunc("/"+client.APIUpdateFW, mock.handleUpdateService)
	mux.HandleFunc("/redfish/v1/UpdateService/Actions/UpdateService.SimpleUpdate", mock.handleInstallBFB)

	// FirmwareInventory
	mux.HandleFunc("/"+client.APICheckBMCFW, mock.handleCheckBMCFirmware)

	// TaskService
	mux.HandleFunc("/redfish/v1/TaskService/Tasks/", mock.handleGetTask)

	// Managers
	mux.HandleFunc("/redfish/v1/Managers", mock.handleGetManagers)

	// ResetBMC
	mux.HandleFunc("/redfish/v1/Managers/{manager_id}/Actions/Manager.Reset", mock.handleResetBMC)

	// NetworkDeviceFunctions
	mux.HandleFunc("/redfish/v1/Chassis/Card1/NetworkAdapters/NvidiaNetworkAdapter/NetworkDeviceFunctions/eth0f0", mock.handleGetNetworkDeviceFunction)

	// System endpoints
	mux.HandleFunc("/redfish/v1/Systems/Bluefield", mock.handleGetSystem)

	// BIOS endpoints
	mux.HandleFunc("/redfish/v1/Systems/Bluefield/Bios", mock.handleGetBios)
	mux.HandleFunc("/redfish/v1/Systems/Bluefield/Bios/Settings", mock.handleSetBiosSettings)
	mux.HandleFunc("/redfish/v1/Systems/Bluefield/Oem/Nvidia/Actions/Mode.Set", mock.handleSetMode)
	mux.HandleFunc("/redfish/v1/Systems/Bluefield/Oem/Nvidia", mock.handleGetProductDescription)

	// Secure Boot endpoints
	mux.HandleFunc("/redfish/v1/Systems/Bluefield/SecureBoot", mock.handleSecureBoot)
	mux.HandleFunc("/redfish/v1/Systems/Bluefield/Actions/ComputerSystem.Reset", mock.handleResetSystem)

	// System Event Log entries
	mux.HandleFunc("/"+client.APIGetSELEntries, mock.handleGetSELEntries)

	// Host Privilege Config
	mux.HandleFunc("/"+client.APIHostPrivilegeConfigSettings, mock.handleHostPrivilegeConfigSettings)

	// BlueField 4 OS install (virtual media, boot settings, chassis reset)
	mux.HandleFunc("/redfish/v1/Managers/Bluefield_BMC/VirtualMedia/", mock.handleVirtualMedia)
	mux.HandleFunc("/redfish/v1/Systems/Bluefield/Settings", mock.handleBluefieldSystemSettings)
	mux.HandleFunc("/redfish/v1/Chassis/BlueField_0/Actions/Oem/NvidiaChassis.Reset", mock.handleChassisReset)

	mock.server = httptest.NewUnstartedServer(mux)
	return mock
}

// Start starts the mock server
func (r *RedfishMockServer) Start() {
	r.server.StartTLS()
}

// Stop stops the mock server
func (r *RedfishMockServer) Stop() {
	r.server.Close()
}

// URL returns the server URL
func (r *RedfishMockServer) URL() string {
	return r.server.URL
}

// Listener returns the server listener
func (r *RedfishMockServer) Listener() net.Listener {
	return r.server.Listener
}

// GetIPAddress returns the IP address of the mock server
func (r *RedfishMockServer) GetIPAddress() string {
	if r.server == nil || r.server.Listener == nil {
		return ""
	}
	addr := r.server.Listener.Addr()
	if tcpAddr, ok := addr.(*net.TCPAddr); ok {
		return tcpAddr.IP.String()
	}
	return ""
}

// GetPort returns the port of the mock server
func (r *RedfishMockServer) GetPort() int {
	if r.server == nil || r.server.Listener == nil {
		return 0
	}
	addr := r.server.Listener.Addr()
	if tcpAddr, ok := addr.(*net.TCPAddr); ok {
		return tcpAddr.Port
	}
	return 0
}

// GetAddress returns the full address (IP:port) of the mock server
func (r *RedfishMockServer) GetAddress() string {
	if r.server == nil || r.server.Listener == nil {
		return ""
	}
	addr := r.server.Listener.Addr()
	if tcpAddr, ok := addr.(*net.TCPAddr); ok {
		return tcpAddr.String()
	}
	return ""
}

// GetClient returns a Redfish client configured to connect to this mock server
func (r *RedfishMockServer) GetClient() (*client.Client, error) {
	// Create a client that skips TLS verification for testing
	c, err := client.NewRawClient(r.server.URL)
	if err != nil {
		return nil, err
	}

	// Set basic auth
	if r.dpuVersion == BF3 {
		c.SetBasicAuth("root", r.password)
		c.IsBF4 = false
	} else {
		c.SetBasicAuth("admin", r.password)
		c.IsBF4 = true
	}

	return c, nil
}

// writeJSONResponse writes a JSON response to the HTTP writer with error handling
func writeJSONResponse(w http.ResponseWriter, response interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

// handleRootService handles the root service endpoint
func (r *RedfishMockServer) handleRootService(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	product := "BlueField-3 DPU"
	if r.dpuVersion == BF4 {
		product = "B4240"
	}

	response := map[string]interface{}{
		"@odata.context": "/redfish/v1/$metadata#ServiceRoot.ServiceRoot",
		"@odata.id":      "/redfish/v1",
		"@odata.type":    "#ServiceRoot.v1_15_0.ServiceRoot",
		"Id":             "RootService",
		"Name":           "Root Service",
		"RedfishVersion": "1.15.0",
		"UUID":           "12345678-1234-1234-1234-123456789abc",
		"Product":        product,
		"Systems": map[string]interface{}{
			"@odata.id": "/redfish/v1/Systems",
		},
		"Managers": map[string]interface{}{
			"@odata.id": "/redfish/v1/Managers",
		},
		"UpdateService": map[string]interface{}{
			"@odata.id": client.APIUpdateFW,
		},
		"TaskService": map[string]interface{}{
			"@odata.id": "/redfish/v1/TaskService",
		},
	}

	writeJSONResponse(w, response)
}

// handleGetSystems handles GET requests to /redfish/v1/Systems
func (r *RedfishMockServer) handleGetSystems(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	response := map[string]interface{}{
		"@odata.id": "/redfish/v1/Systems",
		"Members": []map[string]interface{}{
			{
				"@odata.id": "/redfish/v1/Systems/Bluefield",
			},
		},
	}

	writeJSONResponse(w, response)
}

// handleGetChassis handles chassis information requests
func (r *RedfishMockServer) handleGetChassis(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if r.chassisError {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("BMC chassis endpoint unavailable"))
		return
	}

	// Use custom model if set, otherwise default to BlueField-3
	model := r.model
	if model == "" {
		model = "BlueField-3 DPU"
	}

	response := map[string]interface{}{
		"@odata.context": "/redfish/v1/$metadata#Chassis.Chassis",
		"@odata.id":      "/redfish/v1/Chassis/Card1",
		"@odata.type":    "#Chassis.v1_20_0.Chassis",
		"Id":             "Card1",
		"Name":           "BlueField DPU Card",
		"Model":          model,
		"PartNumber":     DpuOPN,
		"SerialNumber":   DpuSerialNumber,
		"AssetTag":       "N/A",
		"Status": map[string]interface{}{
			"State":  "Enabled",
			"Health": "OK",
		},
	}

	writeJSONResponse(w, response)
}

// handleInstallBFB handles BFB installation requests
func (r *RedfishMockServer) handleInstallBFB(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if r.concurrentUpdateBusyRemaining > 0 {
		r.concurrentUpdateBusyRemaining--
		r.concurrentUpdateBusyServed++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		writeJSONResponse(w, map[string]interface{}{
			"error": map[string]interface{}{
				"message": "Another update is in progress",
			},
		})
		return
	}

	r.bmcVersion = "24.10-17"

	taskInfo := map[string]interface{}{
		"@odata.context": "/redfish/v1/$metadata#Task.Task",
		"@odata.id":      "/redfish/v1/TaskService/Tasks/0",
		"@odata.type":    "#Task.v1_10_0.Task",
		"Id":             "0",
		"Name":           "BFB Install Task",
	}

	w.WriteHeader(http.StatusAccepted)
	writeJSONResponse(w, taskInfo)
}

// handleUpdateService handles GET (info) and POST (firmware push) requests
func (r *RedfishMockServer) handleUpdateService(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		response := map[string]interface{}{
			"@odata.context": "/redfish/v1/$metadata#UpdateService.UpdateService",
			"@odata.id":      client.APIUpdateFW,
			"@odata.type":    "#UpdateService.v1_10_0.UpdateService",
			"Id":             "UpdateService",
			"Name":           "Update Service",
		}
		writeJSONResponse(w, response)
	case http.MethodPost:
		w.WriteHeader(http.StatusAccepted)
		writeJSONResponse(w, map[string]interface{}{
			"@odata.id": "/redfish/v1/TaskService/Tasks/0",
			"Id":        "0",
		})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleCheckBMCFirmware handles BMC firmware version requests
func (r *RedfishMockServer) handleCheckBMCFirmware(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSONResponse(w, map[string]interface{}{
		"@odata.id": "/" + client.APICheckBMCFW,
		"Version":   r.bmcVersion,
	})
}

// handleGetTask handles task information requests
func (r *RedfishMockServer) handleGetTask(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Allow tests to inject a non-200 response (e.g., 404 / 500) plus an
	// arbitrary body to exercise the error-classification paths.
	if r.taskHTTPStatus != 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(r.taskHTTPStatus)
		if r.taskHTTPBody != "" {
			_, _ = w.Write([]byte(r.taskHTTPBody))
		}
		return
	}

	if r.taskProgressError {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		writeJSONResponse(w, map[string]interface{}{"error": "BMC task endpoint unavailable"})
		return
	}

	taskInfo := map[string]interface{}{
		"@odata.context":  "/redfish/v1/$metadata#Task.Task",
		"@odata.id":       "/redfish/v1/TaskService/Tasks/0",
		"@odata.type":     "#Task.v1_10_0.Task",
		"Id":              "0",
		"Name":            "Update Service",
		"TaskState":       r.taskState,
		"PercentComplete": 100,
	}

	// Add messages if taskState is Exception
	if r.taskState == "Exception" && len(r.taskMessages) > 0 {
		taskInfo["Messages"] = r.taskMessages
	}

	writeJSONResponse(w, taskInfo)
}

// handleGetSELEntries handles GET requests to LogServices/SEL/Entries.
// Returns an empty Members[] when the test hasn't injected entries.
func (r *RedfishMockServer) handleGetSELEntries(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	members := r.selEntries
	if members == nil {
		members = []client.SELEntry{}
	}
	writeJSONResponse(w, map[string]interface{}{
		"@odata.id":           "/" + client.APIGetSELEntries,
		"@odata.type":         "#LogEntryCollection.LogEntryCollection",
		"Description":         "Collection of System Event Log Entries",
		"Members":             members,
		"Members@odata.count": len(members),
		"Name":                "System Event Log Entries",
	})
}

func (r *RedfishMockServer) handleGetManagers(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	response := map[string]interface{}{
		"@odata.context": "/redfish/v1/$metadata#Managers.Managers",
		"@odata.id":      "/redfish/v1/Managers",
		"@odata.type":    "#Managers.v1_10_0.Managers",
		"Id":             "Managers",
		"Name":           "Managers",
		"Members": []map[string]interface{}{
			{
				"@odata.id": "/redfish/v1/Managers/Bluefield_BMC",
			},
		},
	}

	writeJSONResponse(w, response)
}

func (r *RedfishMockServer) handleResetBMC(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	response := map[string]interface{}{
		"@odata.id": "/redfish/v1/Managers/BMC/Actions/Manager.Reset",
	}
	json.NewEncoder(w).Encode(response) //nolint: errcheck
}

func (r *RedfishMockServer) handleGetNetworkDeviceFunction(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	response := map[string]interface{}{
		"@odata.id":      "/redfish/v1/Chassis/Card1/NetworkAdapters/NvidiaNetworkAdapter/NetworkDeviceFunctions/eth0f0",
		"Id":             "eth0f0",
		"NetDevFuncType": "Ethernet",
		"Ethernet": map[string]interface{}{
			"MACAddress": "00:1B:21:C0:8F:32",
			"MTUSize":    1500,
		},
	}
	json.NewEncoder(w).Encode(response) //nolint: errcheck
}

func (r *RedfishMockServer) handleGetProductDescription(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.productDescriptionError {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		writeJSONResponse(w, map[string]interface{}{"error": "BMC unreachable"})
		return
	}
	response := map[string]interface{}{
		"@odata.id":   "/redfish/v1/Systems/Bluefield/Oem/Nvidia",
		"@odata.type": "#NvidiaComputerSystem.v1_0_0.NvidiaComputerSystem",
		"Id":          "NvidiaComputerSystem",
		"Name":        "Nvidia Computer System",
		"Mode":        r.dpuMode,
	}
	writeJSONResponse(w, response)
}

func (r *RedfishMockServer) handleGetBios(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	response := map[string]interface{}{
		"@odata.context": "/redfish/v1/$metadata#Bios.Bios",
		"@odata.id":      "/redfish/v1/Systems/Bluefield/Bios",
		"@odata.type":    "#Bios.v1_2_0.Bios",
		"Id":             "Bios",
		"Name":           "BIOS Configuration",
		"Attributes": map[string]interface{}{
			"NicMode":            r.dpuMode,
			"HostPrivilegeLevel": "Privileged",
		},
	}

	writeJSONResponse(w, response)
}

func (r *RedfishMockServer) handleSetBiosSettings(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPatch {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.WriteHeader(http.StatusOK)
	response := map[string]interface{}{
		"@odata.id": "/redfish/v1/Systems/Bluefield/Bios/Settings",
	}
	writeJSONResponse(w, response)
}

func (r *RedfishMockServer) handleSetMode(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body map[string]interface{}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if mode, ok := body["Mode"].(string); ok {
		r.dpuMode = mode
	}

	w.WriteHeader(http.StatusOK)
	response := map[string]interface{}{
		"@odata.id": "/redfish/v1/Systems/Bluefield/Oem/Nvidia/Actions/Mode.Set",
	}
	writeJSONResponse(w, response)
}

// SetNicMode sets the current NIC mode for the mock server
func (r *RedfishMockServer) SetNicMode(mode string) {
	r.dpuMode = mode
}

// GetNicMode returns the current NIC mode
func (r *RedfishMockServer) GetNicMode() string {
	return r.dpuMode
}

// SetSecureBootEnable sets the configured Secure Boot state (for next boot)
func (r *RedfishMockServer) SetSecureBootEnable(enabled bool) {
	r.secureBootEnable = enabled
}

// GetSecureBootEnable returns the configured Secure Boot state
func (r *RedfishMockServer) GetSecureBootEnable() bool {
	return r.secureBootEnable
}

// SetSecureBootCurrentBoot sets the current boot session's Secure Boot state
func (r *RedfishMockServer) SetSecureBootCurrentBoot(enabled bool) {
	r.secureBootCurrentBoot = enabled
}

// GetSecureBootCurrentBoot returns the current boot session's Secure Boot state
func (r *RedfishMockServer) GetSecureBootCurrentBoot() bool {
	return r.secureBootCurrentBoot
}

// ApplySecureBootAfterReboot simulates the second reboot that applies Secure Boot configuration.
// Test code should call this explicitly after simulating two reboots:
//  1. First reboot: client.ForceRestartDPUArm() - applies BIOS config
//  2. Second reboot: client.ForceRestartDPUArm() + mock.ApplySecureBootAfterReboot() - activates Secure Boot
func (r *RedfishMockServer) ApplySecureBootAfterReboot() {
	r.secureBootCurrentBoot = r.secureBootEnable
}

// SetSecureBootError enables or disables Secure Boot endpoint error simulation for testing
func (r *RedfishMockServer) SetSecureBootError(simulateError bool) {
	r.secureBootError = simulateError
}

// SetSecureBootPatchError enables or disables Secure Boot PATCH-only error simulation.
// GET requests still succeed, allowing detection to pass while staging fails.
func (r *RedfishMockServer) SetSecureBootPatchError(simulateError bool) {
	r.secureBootPatchError = simulateError
}

// SetSystemError enables or disables GetSystem endpoint error simulation for testing
func (r *RedfishMockServer) SetSystemError(simulateError bool) {
	r.systemError = simulateError
}

// SetResetSystemError enables or disables ResetSystem endpoint error simulation for testing
func (r *RedfishMockServer) SetResetSystemError(simulateError bool) {
	r.resetSystemError = simulateError
}

// SetProductDescriptionError enables or disables GetProductDescription endpoint error simulation.
// Since NewTLSClient calls GetProductDescription as a connectivity check, this effectively
// simulates BMC unreachable at the TLS client creation level.
func (r *RedfishMockServer) SetProductDescriptionError(simulateError bool) {
	r.productDescriptionError = simulateError
}

// SetTaskProgressError enables or disables CheckTaskProgress endpoint error simulation for testing
func (r *RedfishMockServer) SetTaskProgressError(simulateError bool) {
	r.taskProgressError = simulateError
}

// SetChassisError enables or disables GetChassis endpoint error simulation for testing
func (r *RedfishMockServer) SetChassisError(simulateError bool) {
	r.chassisError = simulateError
}

// SetOemLastState sets the ARM OS boot state for the mock server
func (r *RedfishMockServer) SetOemLastState(state string) {
	r.oemLastState = state
}

// SetBootLastState sets BootProgress.LastState returned by GET System (used by BF4 installing).
func (r *RedfishMockServer) SetBootLastState(state string) {
	r.bootLastState = state
}

// GetOemLastState returns the current ARM OS boot state
func (r *RedfishMockServer) GetOemLastState() string {
	return r.oemLastState
}

// SetDpuVersion sets the DPU version (BF3 or BF4), which affects the root service product string
func (r *RedfishMockServer) SetDpuVersion(version DpuVersion) {
	r.dpuVersion = version
}

// SetModel sets the DPU model string
func (r *RedfishMockServer) SetModel(model string) {
	r.model = model
}

// SetBMCVersion sets the BMC firmware version returned by the mock server
func (r *RedfishMockServer) SetBMCVersion(version string) {
	r.bmcVersion = version
}

// SetTaskState sets the task state for the mock server
func (r *RedfishMockServer) SetTaskState(state string) {
	r.taskState = state
}

// SetTaskMessages sets the task messages for the mock server
func (r *RedfishMockServer) SetTaskMessages(messages []map[string]interface{}) {
	r.taskMessages = messages
}

// SetConcurrentUpdateBusy configures the mock server to return HTTP 400
// "Another update is in progress" for the next N InstallBFB calls.
// After N calls the server returns the normal HTTP 202 Accepted response.
// The served counter is reset to 0 each time this is called.
func (r *RedfishMockServer) SetConcurrentUpdateBusy(count int) {
	r.concurrentUpdateBusyRemaining = count
	r.concurrentUpdateBusyServed = 0
}

// GetConcurrentUpdateBusyServed returns how many HTTP 400 "Another update
// is in progress" responses the mock server has actually sent.
func (r *RedfishMockServer) GetConcurrentUpdateBusyServed() int {
	return r.concurrentUpdateBusyServed
}

// SetTaskHTTPResponse forces handleGetTask to return the given HTTP status
// and raw body, bypassing the default JSON. Pass status=0 to restore default.
func (r *RedfishMockServer) SetTaskHTTPResponse(status int, body string) {
	r.taskHTTPStatus = status
	r.taskHTTPBody = body
}

// SetSELEntries sets the entries returned by GET LogServices/SEL/Entries.
func (r *RedfishMockServer) SetSELEntries(entries []client.SELEntry) {
	r.selEntries = entries
}

// SetHostPrivilegeError enables or disables HostPrivilegeConfig endpoint error simulation for testing
func (r *RedfishMockServer) SetHostPrivilegeError(simulateError bool) {
	r.hostPrivilegeError = simulateError
}

// GetHostPrivilegeMode returns the current host privilege mode
func (r *RedfishMockServer) GetHostPrivilegeMode() string {
	return r.hostPrivilegeMode
}

// handleHostPrivilegeConfigSettings handles PATCH requests to the HostPrivilegeConfig/Settings endpoint
func (r *RedfishMockServer) handleHostPrivilegeConfigSettings(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPatch {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if r.hostPrivilegeError {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		writeJSONResponse(w, map[string]interface{}{"error": "HostPrivilegeConfig endpoint unavailable"})
		return
	}

	var body map[string]interface{}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if mode, ok := body["PrivilegeMode"].(string); ok {
		r.hostPrivilegeMode = mode
	}

	w.WriteHeader(http.StatusOK)
	writeJSONResponse(w, map[string]interface{}{
		"@odata.id": req.URL.Path,
	})
}

// GetCertificate returns the server's TLS certificate in PEM format
func (r *RedfishMockServer) GetCertificate() []byte {
	if r.server == nil || r.server.Certificate() == nil {
		return nil
	}
	return r.server.Certificate().Raw
}

func (r *RedfishMockServer) bootProgressLastState() string {
	if r.bootLastState != "" {
		return r.bootLastState
	}
	return "OEM"
}

// handleVirtualMedia handles VirtualMedia.InsertMedia and VirtualMedia.EjectMedia for BF4 OS install.
func (r *RedfishMockServer) handleVirtualMedia(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusOK)
	writeJSONResponse(w, map[string]interface{}{
		"@odata.id": req.URL.Path,
	})
}

// handleBluefieldSystemSettings handles PATCH boot settings for BF4 OS install.
func (r *RedfishMockServer) handleBluefieldSystemSettings(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPatch {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusOK)
	writeJSONResponse(w, map[string]interface{}{
		"@odata.id": req.URL.Path,
	})
}

// handleChassisReset handles BF4 chassis ARM reset during OS install.
func (r *RedfishMockServer) handleChassisReset(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusOK)
	writeJSONResponse(w, map[string]interface{}{
		"@odata.id": req.URL.Path,
	})
}

// handleGetSystem handles GET requests to /redfish/v1/Systems/Bluefield
func (r *RedfishMockServer) handleGetSystem(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if r.systemError {
		// Return valid JSON with non-200 status to test the status code check path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "System endpoint unavailable"}) //nolint:errcheck
		return
	}

	response := map[string]interface{}{
		"@odata.id":    "/redfish/v1/Systems/Bluefield",
		"@odata.type":  "#ComputerSystem.v1_22_0.ComputerSystem",
		"Id":           "Bluefield",
		"Name":         "Bluefield",
		"Description":  "This ComputerSystem resource represents the SoC that is part of the DPU found in Card1",
		"SystemType":   "Physical",
		"Manufacturer": "Nvidia",
		"Model":        "Bluefield 3 SmartNIC Main Card",
		"SerialNumber": DpuSerialNumber,
		"PowerState":   "On",
		"Status": map[string]interface{}{
			"State":      "Enabled",
			"Health":     "OK",
			"Conditions": []interface{}{},
		},
		"BootProgress": map[string]interface{}{
			"LastState":     r.bootProgressLastState(),
			"LastStateTime": "1970-01-21T11:09:24.575484+00:00",
			"OemLastState":  r.oemLastState,
		},
		"Boot": map[string]interface{}{
			"BootSourceOverrideEnabled": "Disabled",
			"BootSourceOverrideMode":    "UEFI",
			"BootSourceOverrideTarget":  "None",
		},
		"Bios": map[string]interface{}{
			"@odata.id": "/redfish/v1/Systems/Bluefield/Bios",
		},
		"SecureBoot": map[string]interface{}{
			"@odata.id": "/redfish/v1/Systems/Bluefield/SecureBoot",
		},
		"Links": map[string]interface{}{
			"Chassis": []map[string]interface{}{
				{"@odata.id": "/redfish/v1/Chassis/Card1"},
			},
			"ManagedBy": []map[string]interface{}{
				{"@odata.id": "/redfish/v1/Managers/Bluefield_BMC"},
			},
		},
		"Actions": map[string]interface{}{
			"#ComputerSystem.Reset": map[string]interface{}{
				"target":              "/redfish/v1/Systems/Bluefield/Actions/ComputerSystem.Reset",
				"@Redfish.ActionInfo": "/redfish/v1/Systems/Bluefield/ResetActionInfo",
			},
		},
	}
	writeJSONResponse(w, response)
}

// handleSecureBoot handles GET and PATCH requests to /redfish/v1/Systems/Bluefield/SecureBoot
func (r *RedfishMockServer) handleSecureBoot(w http.ResponseWriter, req *http.Request) {
	// Simulate error if flag is set
	if r.secureBootError {
		http.Error(w, "Secure Boot endpoint unavailable", http.StatusInternalServerError)
		return
	}

	switch req.Method {
	case http.MethodGet:
		// Return Secure Boot state - current boot vs configured state can differ
		currentBoot := "Disabled"
		if r.secureBootCurrentBoot {
			currentBoot = "Enabled"
		}

		response := map[string]interface{}{
			"@odata.id":             "/redfish/v1/Systems/Bluefield/SecureBoot",
			"@odata.type":           "#SecureBoot.v1_1_0.SecureBoot",
			"Id":                    "SecureBoot",
			"Name":                  "UEFI Secure Boot",
			"SecureBootCurrentBoot": currentBoot,        // What current boot session used
			"SecureBootEnable":      r.secureBootEnable, // What's configured for next boot
			"SecureBootMode":        "UserMode",
		}
		writeJSONResponse(w, response)

	case http.MethodPatch:
		if r.secureBootPatchError {
			http.Error(w, "Secure Boot PATCH endpoint unavailable", http.StatusInternalServerError)
			return
		}
		// Update Secure Boot setting (only changes SecureBootEnable, not SecureBootCurrentBoot)
		// SecureBootCurrentBoot will only update after system reboot
		var body map[string]interface{}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if enabled, ok := body["SecureBootEnable"].(bool); ok {
			r.secureBootEnable = enabled
			// Note: secureBootCurrentBoot is NOT changed here - only updated on reboot
		}

		w.WriteHeader(http.StatusOK)
		response := map[string]interface{}{
			"@odata.id": "/redfish/v1/Systems/Bluefield/SecureBoot",
		}
		writeJSONResponse(w, response)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleResetSystem handles POST requests to /redfish/v1/Systems/Bluefield/Actions/ComputerSystem.Reset
func (r *RedfishMockServer) handleResetSystem(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if r.resetSystemError {
		http.Error(w, "Reset system endpoint unavailable", http.StatusInternalServerError)
		return
	}

	var body map[string]interface{}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate reset type
	resetType, ok := body["ResetType"].(string)
	if !ok || (resetType != "ForceRestart" && resetType != "GracefulRestart" && resetType != "PowerCycle") {
		http.Error(w, "Invalid reset type", http.StatusBadRequest)
		return
	}

	// Note: Secure Boot requires TWO reboots to take effect
	// - First reboot: Apply BIOS configuration
	// - Second reboot: Boot with new Secure Boot state
	// Test code must explicitly call ApplySecureBootAfterReboot() at the right time

	// Return success message like real API
	w.WriteHeader(http.StatusOK)
	response := map[string]interface{}{
		"@Message.ExtendedInfo": []map[string]interface{}{
			{
				"@odata.type":     "#Message.v1_1_1.Message",
				"Message":         "The request completed successfully.",
				"MessageId":       "Base.1.18.1.Success",
				"MessageSeverity": "OK",
				"Resolution":      "None.",
			},
		},
	}
	writeJSONResponse(w, response)
}

// CreateMockRedfishServer creates and starts a mock Redfish server for testing
func CreateMockRedfishServer(bmcVersion, password string) (*RedfishMockServer, error) {
	if password == "" {
		password = defaultBMCPassword
	}

	server := NewRedfishMockServer(bmcVersion, password)
	server.Start()

	klog.Infof("Mock Redfish server started at %s", server.URL())
	return server, nil
}
