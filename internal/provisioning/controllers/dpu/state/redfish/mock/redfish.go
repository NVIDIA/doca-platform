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
)

// RedfishMockServer represents a mock Redfish server for testing
type RedfishMockServer struct {
	server     *httptest.Server
	bmcVersion string
	password   string
}

// NewRedfishMockServer creates a new mock Redfish server
func NewRedfishMockServer(bmcVersion, password string) *RedfishMockServer {
	mock := &RedfishMockServer{
		bmcVersion: bmcVersion,
		password:   password,
	}

	mux := http.NewServeMux()

	// Root service
	mux.HandleFunc("/redfish/v1/", mock.handleRootService)

	// Chassis
	mux.HandleFunc("/redfish/v1/Chassis/Card1", mock.handleGetChassis)

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
		"PartNumber":     "900-9D3B6-00CV-AA0",
		"SerialNumber":   "MT25066004C7",
		"Status": map[string]interface{}{
			"State":  "Enabled",
			"Health": "OK",
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
