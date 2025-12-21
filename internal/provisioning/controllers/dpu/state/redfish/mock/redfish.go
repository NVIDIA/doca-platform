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
	server     *httptest.Server
	bmcVersion string
	password   string
	dpuMode    string // Current DPU mode: "NicMode" or "DpuMode"
}

// NewRedfishMockServer creates a new mock Redfish server
func NewRedfishMockServer(bmcVersion, password string) *RedfishMockServer {
	mock := &RedfishMockServer{
		bmcVersion: bmcVersion,
		password:   password,
		dpuMode:    "DpuMode", // Default to DpuMode
	}

	mux := http.NewServeMux()

	// Root service
	mux.HandleFunc("/redfish/v1/", mock.handleRootService)

	// Chassis
	mux.HandleFunc("/redfish/v1/Chassis/Card1", mock.handleGetChassis)

	// UpdateService
	mux.HandleFunc("/redfish/v1/UpdateService", mock.handleUpdateService)

	// TaskService
	mux.HandleFunc("/redfish/v1/TaskService/Tasks/{task_id}", mock.handleGetTask)

	// Managers
	mux.HandleFunc("/redfish/v1/Managers", mock.handleGetManagers)

	// ResetBMC
	mux.HandleFunc("/redfish/v1/Managers/{manager_id}/Actions/Manager.Reset", mock.handleResetBMC)

	// NetworkDeviceFunctions
	mux.HandleFunc("/redfish/v1/Chassis/Card1/NetworkAdapters/NvidiaNetworkAdapter/NetworkDeviceFunctions/eth0f0", mock.handleGetNetworkDeviceFunction)

	// BIOS endpoints
	mux.HandleFunc("/redfish/v1/Systems/Bluefield/Bios", mock.handleGetBios)
	mux.HandleFunc("/redfish/v1/Systems/Bluefield/Bios/Settings", mock.handleSetBiosSettings)
	mux.HandleFunc("/redfish/v1/Systems/Bluefield/Oem/Nvidia/Actions/Mode.Set", mock.handleSetMode)
	mux.HandleFunc("/redfish/v1/Systems/Bluefield/Oem/Nvidia", mock.handleGetProductDescription)

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
	c.SetBasicAuth("root", r.password)

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

	response := map[string]interface{}{
		"@odata.context": "/redfish/v1/$metadata#ServiceRoot.ServiceRoot",
		"@odata.id":      "/redfish/v1",
		"@odata.type":    "#ServiceRoot.v1_15_0.ServiceRoot",
		"Id":             "RootService",
		"Name":           "Root Service",
		"RedfishVersion": "1.15.0",
		"UUID":           "12345678-1234-1234-1234-123456789abc",
		"Systems": map[string]interface{}{
			"@odata.id": "/redfish/v1/Systems",
		},
		"Managers": map[string]interface{}{
			"@odata.id": "/redfish/v1/Managers",
		},
		"UpdateService": map[string]interface{}{
			"@odata.id": "/redfish/v1/UpdateService",
		},
		"TaskService": map[string]interface{}{
			"@odata.id": "/redfish/v1/TaskService",
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

	response := map[string]interface{}{
		"@odata.context": "/redfish/v1/$metadata#Chassis.Chassis",
		"@odata.id":      "/redfish/v1/Chassis/Card1",
		"@odata.type":    "#Chassis.v1_20_0.Chassis",
		"Id":             "Card1",
		"Name":           "BlueField DPU Card",
		"Model":          "BlueField-3 B3220",
		"PartNumber":     DpuOPN,
		"SerialNumber":   DpuSerialNumber,
		"Status": map[string]interface{}{
			"State":  "Enabled",
			"Health": "OK",
		},
	}

	writeJSONResponse(w, response)
}

// handleUpdateService handles update service information requests
func (r *RedfishMockServer) handleUpdateService(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.bmcVersion = "24.10-17"

	taskInfo := map[string]interface{}{
		"@odata.context": "/redfish/v1/$metadata#Task.Task",
		"@odata.id":      "/redfish/v1/TaskService/Tasks/0",
		"@odata.type":    "#Task.v1_10_0.Task",
		"Id":             "0",
		"Name":           "Update Service",
	}

	w.WriteHeader(http.StatusAccepted)
	writeJSONResponse(w, taskInfo)
}

// handleGetTask handles task information requests
func (r *RedfishMockServer) handleGetTask(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	taskInfo := map[string]interface{}{
		"@odata.context":  "/redfish/v1/$metadata#Task.Task",
		"@odata.id":       "/redfish/v1/TaskService/Tasks/0",
		"@odata.type":     "#Task.v1_10_0.Task",
		"Id":              "0",
		"Name":            "Update Service",
		"TaskState":       "Completed",
		"PercentComplete": 100,
	}

	writeJSONResponse(w, taskInfo)
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

// GetCertificate returns the server's TLS certificate in PEM format
func (r *RedfishMockServer) GetCertificate() []byte {
	if r.server == nil || r.server.Certificate() == nil {
		return nil
	}
	return r.server.Certificate().Raw
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
