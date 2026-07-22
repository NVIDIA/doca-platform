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
	"encoding/pem"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"

	"k8s.io/klog/v2"
)

// Constants for repeated strings
const (
	defaultBMCPassword = "0penBmc"
	DpuSerialNumber    = "MT25066004C7"
	DpuPSIDBF4         = "MT_0000001774"
	DpuOPN             = "900-9D3B4-00SV-EA0"
)

// RedfishMockServer represents a mock Redfish server for testing
type RedfishMockServer struct {
	server                          *httptest.Server
	bmcVersion                      string
	bmcErotVersion                  string
	sbiosVersion                    string
	nicVersion                      string
	password                        string
	dpuMode                         string                   // Current DPU mode: "NicMode" or "DpuMode"
	secureBootEnable                bool                     // Configured/desired Secure Boot state (for next boot)
	secureBootCurrentBoot           bool                     // Actual Secure Boot state of current boot session
	secureBootError                 bool                     // Simulate Secure Boot endpoint error for testing
	secureBootPatchError            bool                     // Simulate Secure Boot PATCH-only error for testing
	systemError                     bool                     // Simulate GetSystem endpoint error for testing
	resetSystemError                bool                     // Simulate ResetSystem endpoint error for testing
	productDescriptionError         bool                     // Simulate GetProductDescription endpoint error for testing
	taskProgressError               bool                     // Simulate CheckTaskProgress endpoint error for testing
	chassisError                    bool                     // Simulate GetChassis endpoint error for testing
	erotChassisError                bool                     // Simulate GetErotChassis endpoint error for testing
	erotChassisOemPresent           bool                     // Include Oem.Nvidia in ERoT chassis response
	erotBackgroundCopyStatus        string                   // BackgroundCopyStatus value in ERoT chassis Oem.Nvidia
	erotBackgroundCopyStatusPresent bool                     // Include BackgroundCopyStatus in ERoT chassis Oem.Nvidia
	oemLastState                    string                   // Current ARM OS boot state: "OsIsRunning", "OsStarting", etc.
	bootLastState                   string                   // BootProgress.LastState for GET System (e.g. OSRunning on BF4)
	dpuVersion                      DpuVersion               // Current DPU version
	model                           string                   // DPU model string (optional override)
	assetTag                        string                   // Chassis AssetTag (PSID) returned by GetChassis
	taskState                       string                   // Current task state: "Completed", "Exception", etc.
	taskMessages                    []map[string]interface{} // Task messages for Exception state
	concurrentUpdateBusyRemaining   int                      // Number of InstallBFB calls that return HTTP 400 "Another update is in progress"
	concurrentUpdateBusyServed      int                      // How many 400 "Another update" responses were actually sent
	installBFBStatus                int                      // Override HTTP status returned by InstallBFB; 0 means default 202
	installBFBBody                  string                   // Override raw body when installBFBStatus != 0
	taskHTTPStatus                  int                      // Override HTTP status returned by GET task; 0 means default 200
	taskHTTPBody                    string                   // Override raw body when taskHTTPStatus != 0
	selEntries                      []client.SELEntry        // System Event Log entries returned by GET SEL/Entries
	hostPrivilegeError              bool                     // Simulate HostPrivilegeConfig endpoint error for testing
	replaceCertError                bool                     // Simulate CertificateService.ReplaceCertificate returning 500 (BMC key mismatch)
	hostPrivilegeMode               string                   // Current host privilege mode: "Privileged" or "Restricted"
	bootSourceOverrideTarget        string                   // BootSourceOverrideTarget returned by GET Settings
	bootSourceOverrideEnabled       string                   // BootSourceOverrideEnabled returned by GET Settings
	virtualMediaInserted            map[string]bool          // Inserted state per VirtualMedia ID (IMAGE, CONFIG)
}

type DpuVersion int

const (
	BF3 DpuVersion = 3
	BF4 DpuVersion = 4
)

// NewRedfishMockServer creates a new mock Redfish server
func NewRedfishMockServer(bmcVersion, password string) *RedfishMockServer {
	mock := &RedfishMockServer{
		bmcVersion:                      bmcVersion,
		password:                        password,
		dpuMode:                         "DpuMode",                         // Default to DpuMode
		dpuVersion:                      BF3,                               // Default to BF3
		hostPrivilegeMode:               "Privileged",                      // Default to Privileged
		secureBootEnable:                true,                              // Default configured state: enabled
		secureBootCurrentBoot:           true,                              // Default current boot state: enabled
		oemLastState:                    "OsIsRunning",                     // Default to OS running
		assetTag:                        client.ChassisAssetTagUnavailable, // Default AssetTag when unset on BMC
		erotChassisOemPresent:           true,
		erotBackgroundCopyStatus:        "Completed",
		erotBackgroundCopyStatusPresent: true,
		taskState:                       "Completed",                // Default task state
		taskMessages:                    []map[string]interface{}{}, // Default empty messages
		bootSourceOverrideTarget:        "None",                     // Default boot target
		bootSourceOverrideEnabled:       "Disabled",                 // Default boot override state
		virtualMediaInserted:            map[string]bool{},          // Default: nothing inserted
	}

	mux := http.NewServeMux()

	// Systems collection (must be registered before the /redfish/v1/ prefix handler)
	mux.HandleFunc("/redfish/v1/Systems", mock.handleGetSystems)

	// Root service
	mux.HandleFunc("/redfish/v1/", mock.handleRootService)

	// Chassis
	mux.HandleFunc("/redfish/v1/Chassis/Card1", mock.handleGetChassis)
	mux.HandleFunc("/redfish/v1/Chassis/BlueField_0", mock.handleGetChassis)
	mux.HandleFunc("/redfish/v1/Chassis/BlueField_ERoT_BMC_0", mock.handleGetErotChassis)

	// UpdateService
	mux.HandleFunc("/"+client.APIUpdateFW, mock.handleUpdateService)
	mux.HandleFunc("/redfish/v1/UpdateService/Actions/UpdateService.SimpleUpdate", mock.handleInstallBFB)

	// FirmwareInventory (prefix handler avoids Go ServeMux wildcard conflicts between inventory IDs)
	mux.HandleFunc("/redfish/v1/UpdateService/FirmwareInventory/", mock.handleCheckFirmwareInventory)
	mux.HandleFunc("/"+client.APIUpdateBluefieldFWMultipart, mock.handleUpdateBluefieldFirmwareMultipart)
	mux.HandleFunc("/"+client.APIActivatePendingBundle, mock.handleActivatePendingBundle)

	// TaskService
	mux.HandleFunc("/redfish/v1/TaskService/Tasks/", mock.handleGetTask)

	// Managers
	mux.HandleFunc("/redfish/v1/Managers", mock.handleGetManagers)

	// ResetBMC
	mux.HandleFunc("/redfish/v1/Managers/{manager_id}/Actions/Manager.Reset", mock.handleResetBMC)

	// Server certificate management (mTLS server cert rotation)
	mux.HandleFunc("/redfish/v1/Managers/Bluefield_BMC/NetworkProtocol/HTTPS/Certificates/1", mock.handleGetServerCert)
	mux.HandleFunc("/redfish/v1/Managers/Bluefield_BMC/Truststore/Certificates", mock.handleInstallTruststoreCert)
	mux.HandleFunc("/"+client.APIReplaceCert, mock.handleReplaceCert)
	mux.HandleFunc("/"+client.APIEnableMTLS, mock.handleEnableMTLS)

	// NetworkDeviceFunctions
	mux.HandleFunc("/redfish/v1/Chassis/Card1/NetworkAdapters/NvidiaNetworkAdapter/NetworkDeviceFunctions/eth0f0", mock.handleGetNetworkDeviceFunction)
	mux.HandleFunc("/redfish/v1/Chassis/BlueField_0/NetworkAdapters/BlueField_NIC_0/NetworkDeviceFunctions/0", mock.handleGetNetworkDeviceFunctionBF4)

	// System endpoints (BF3 uses Bluefield; BF4 uses BlueField_0)
	for _, systemID := range []string{"Bluefield", "BlueField_0"} {
		mux.HandleFunc("/redfish/v1/Systems/"+systemID, mock.handleGetSystem)
		mux.HandleFunc("/redfish/v1/Systems/"+systemID+"/Bios", mock.handleGetBios)
		mux.HandleFunc("/redfish/v1/Systems/"+systemID+"/Bios/Settings", mock.handleSetBiosSettings)
		mux.HandleFunc("/redfish/v1/Systems/"+systemID+"/Oem/Nvidia/Actions/Mode.Set", mock.handleSetMode)
		mux.HandleFunc("/redfish/v1/Systems/"+systemID+"/Oem/Nvidia", mock.handleGetProductDescription)
		mux.HandleFunc("/redfish/v1/Systems/"+systemID+"/SecureBoot", mock.handleSecureBoot)
		mux.HandleFunc("/redfish/v1/Systems/"+systemID+"/Actions/ComputerSystem.Reset", mock.handleResetSystem)
		mux.HandleFunc("/redfish/v1/Systems/"+systemID+"/LogServices/SEL/Entries", mock.handleGetSELEntries)
		mux.HandleFunc("/redfish/v1/Systems/"+systemID+"/Settings", mock.handleBluefieldSystemSettings)
	}

	// Host Privilege Config
	mux.HandleFunc("/"+client.APIHostPrivilegeConfigSettings, mock.handleHostPrivilegeConfigSettings)

	// BlueField 4 OS install (virtual media, boot settings, chassis reset)
	mux.HandleFunc("/redfish/v1/Managers/Bluefield_BMC/VirtualMedia/", mock.handleVirtualMedia)
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

// GetServerCertPEM returns the PEM-encoded leaf certificate the mock TLS server is serving.
// Tests use it as the trust anchor (it carries the 127.0.0.1 IP SAN) so the verified mTLS client
// can validate the mock without InsecureSkipVerify. Returns nil before the server is started.
func (r *RedfishMockServer) GetServerCertPEM() []byte {
	if r.server == nil {
		return nil
	}
	cert := r.server.Certificate()
	if cert == nil {
		return nil
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
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

func getSystemID(r *RedfishMockServer) string {
	if r.dpuVersion == BF4 {
		return "BlueField_0"
	}
	return "Bluefield"
}

// handleGetSystems handles GET requests to /redfish/v1/Systems
func (r *RedfishMockServer) handleGetSystems(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	systemID := getSystemID(r)

	response := map[string]interface{}{
		"@odata.id": "/redfish/v1/Systems",
		"Members": []map[string]interface{}{
			{
				"@odata.id": "/redfish/v1/Systems/" + systemID,
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

	chassisID := "Card1"
	defaultModel := "BlueField-3 DPU"
	if r.dpuVersion == BF4 {
		chassisID = "BlueField_0"
		defaultModel = "BlueField-4"
	}

	model := r.model
	if model == "" {
		model = defaultModel
	}

	response := map[string]interface{}{
		"@odata.context": "/redfish/v1/$metadata#Chassis.Chassis",
		"@odata.id":      "/redfish/v1/Chassis/" + chassisID,
		"@odata.type":    "#Chassis.v1_20_0.Chassis",
		"Id":             chassisID,
		"Name":           "BlueField DPU Card",
		"Model":          model,
		"PartNumber":     DpuOPN,
		"SerialNumber":   DpuSerialNumber,
		"AssetTag":       r.chassisAssetTag(),
		"Status": map[string]interface{}{
			"State":  "Enabled",
			"Health": "OK",
		},
	}

	writeJSONResponse(w, response)
}

// handleGetErotChassis handles ERoT chassis information requests.
func (r *RedfishMockServer) handleGetErotChassis(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if r.erotChassisError {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("ERoT chassis endpoint unavailable"))
		return
	}

	response := map[string]interface{}{
		"@odata.context": "/redfish/v1/$metadata#Chassis.Chassis",
		"@odata.id":      "/redfish/v1/Chassis/BlueField_ERoT_BMC_0",
		"@odata.type":    "#Chassis.v1_20_0.Chassis",
		"Id":             "BlueField_ERoT_BMC_0",
		"Name":           "BlueField ERoT BMC",
	}

	if r.erotChassisOemPresent {
		nvidiaOem := map[string]interface{}{}
		if r.erotBackgroundCopyStatusPresent {
			status := r.erotBackgroundCopyStatus
			if status == "" {
				status = "Completed"
			}
			nvidiaOem["BackgroundCopyStatus"] = status
		}
		response["Oem"] = map[string]interface{}{"Nvidia": nvidiaOem}
	}

	writeJSONResponse(w, response)
}

// handleInstallBFB handles BFB installation requests
func (r *RedfishMockServer) handleInstallBFB(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if r.installBFBStatus != 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(r.installBFBStatus)
		if r.installBFBBody != "" {
			_, _ = w.Write([]byte(r.installBFBBody))
		}
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

// handleCheckFirmwareInventory handles firmware inventory version requests.
func (r *RedfishMockServer) handleCheckFirmwareInventory(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSONResponse(w, map[string]interface{}{
		"@odata.id": req.URL.Path,
		"Version":   r.firmwareVersionForPath(req.URL.Path),
	})
}

func (r *RedfishMockServer) firmwareVersionForPath(path string) string {
	switch {
	case strings.HasSuffix(path, "BlueField_FW_ERoT_BMC_0"):
		return r.bmcErotVersionOrDefault()
	case strings.HasSuffix(path, "BlueField_FW_CPU_0"), strings.HasSuffix(path, "DPU_UEFI"):
		return r.sbiosVersionOrDefault()
	case strings.HasSuffix(path, "BlueField_FW_NIC_0"), strings.HasSuffix(path, "DPU_NIC"):
		return r.nicVersionOrDefault()
	case strings.HasSuffix(path, "BlueField_FW_BMC_0"), strings.HasSuffix(path, "BMC_Firmware"):
		return r.bmcVersion
	default:
		return r.bmcVersion
	}
}

func (r *RedfishMockServer) bmcErotVersionOrDefault() string {
	if r.bmcErotVersion != "" {
		return r.bmcErotVersion
	}
	return r.bmcVersion
}

func (r *RedfishMockServer) sbiosVersionOrDefault() string {
	if r.sbiosVersion != "" {
		return r.sbiosVersion
	}
	return r.bmcVersion
}

func (r *RedfishMockServer) nicVersionOrDefault() string {
	if r.nicVersion != "" {
		return r.nicVersion
	}
	return r.bmcVersion
}

// handleUpdateBluefieldFirmwareMultipart handles PLDM firmware multipart upload requests.
func (r *RedfishMockServer) handleUpdateBluefieldFirmwareMultipart(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	taskInfo := map[string]interface{}{
		"@odata.context": "/redfish/v1/$metadata#Task.Task",
		"@odata.id":      "/redfish/v1/TaskService/Tasks/0",
		"@odata.type":    "#Task.v1_10_0.Task",
		"Id":             "0",
		"Name":           "PLDM Firmware Update Task",
	}

	w.WriteHeader(http.StatusAccepted)
	writeJSONResponse(w, taskInfo)
}

// handleActivatePendingBundle handles pending bundle activation requests.
func (r *RedfishMockServer) handleActivatePendingBundle(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.WriteHeader(http.StatusOK)
	writeJSONResponse(w, map[string]interface{}{
		"@odata.id": "/" + client.APIActivatePendingBundle,
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

	systemID := "Bluefield"
	url := strings.ReplaceAll(client.APIGetSELEntries, "{SYSTEM_ID}", systemID)
	writeJSONResponse(w, map[string]interface{}{
		"@odata.id":           "/" + url,
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

// handleGetServerCert returns the certificate the mock BMC is "serving" on its HTTPS endpoint.
// It echoes the mock TLS server's own leaf certificate so cold-start expiry backfill in rotation
// tests parses a real, far-future NotAfter.
func (r *RedfishMockServer) handleGetServerCert(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSONResponse(w, map[string]interface{}{
		"@odata.id":         "/redfish/v1/Managers/Bluefield_BMC/NetworkProtocol/HTTPS/Certificates/1",
		"CertificateString": string(r.GetServerCertPEM()),
		"CertificateType":   "PEM",
	})
}

// handleInstallTruststoreCert acknowledges a CA truststore certificate install (setUpMTLS step 1).
func (r *RedfishMockServer) handleInstallTruststoreCert(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSONResponse(w, map[string]interface{}{})
}

// handleEnableMTLS acknowledges enabling mTLS on the BMC (setUpMTLS step 3).
func (r *RedfishMockServer) handleEnableMTLS(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPatch {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSONResponse(w, map[string]interface{}{})
}

// handleReplaceCert acknowledges a server-certificate replacement request used by rotation tests.
// When replaceCertError is set it returns 500 to emulate a BMC rejecting the certificate (e.g. the
// issued cert no longer matches the BMC's current key pair).
func (r *RedfishMockServer) handleReplaceCert(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.replaceCertError {
		// Mirror a real BMC: a JSON error body with a 500 status (e.g. issued cert/key mismatch).
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"code":    "Base.1.18.1.InternalError",
				"message": "The request failed due to an internal service error.",
			},
		})
		return
	}
	writeJSONResponse(w, map[string]interface{}{})
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

func (r *RedfishMockServer) handleGetNetworkDeviceFunctionBF4(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	response := map[string]interface{}{
		"@odata.id":      "/redfish/v1/Chassis/BlueField_0/NetworkAdapters/BlueField_NIC_0/NetworkDeviceFunctions/0",
		"Id":             "0",
		"NetDevFuncType": "Ethernet",
		"Ethernet": map[string]interface{}{
			"PermanentMACAddress": "00:1B:21:C0:8F:32",
			"MTUSize":             1500,
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
	systemID := getSystemID(r)

	response := map[string]interface{}{
		"@odata.id":   "/redfish/v1/Systems/" + systemID + "/Oem/Nvidia",
		"@odata.type": "#NvidiaComputerSystem.v1_0_0.NvidiaComputerSystem",
	}
	if r.dpuVersion == BF4 {
		// BF4 BMC does not expose Description or Mode on this resource.
		response["Actions"] = map[string]interface{}{
			"#SOC.ForceReset": map[string]interface{}{
				"target": "/redfish/v1/Systems/" + systemID + "/Oem/Nvidia/SOC.ForceReset",
			},
		}
	} else {
		response["Id"] = "NvidiaComputerSystem"
		response["Name"] = "Nvidia Computer System"
		response["Mode"] = r.dpuMode
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

// SetReplaceCertError enables or disables CertificateService.ReplaceCertificate error simulation,
// emulating a BMC that rejects the issued server certificate (e.g. key mismatch after a reboot).
func (r *RedfishMockServer) SetReplaceCertError(simulateError bool) {
	r.replaceCertError = simulateError
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

// SetErotChassisError enables or disables GetErotChassis endpoint error simulation for testing.
func (r *RedfishMockServer) SetErotChassisError(simulateError bool) {
	r.erotChassisError = simulateError
}

// SetErotBackgroundCopyStatus sets the BackgroundCopyStatus value returned by GetErotChassis.
func (r *RedfishMockServer) SetErotBackgroundCopyStatus(status string) {
	r.erotBackgroundCopyStatus = status
}

// SetErotChassisOemPresent controls whether Oem.Nvidia is included in the GetErotChassis response.
func (r *RedfishMockServer) SetErotChassisOemPresent(present bool) {
	r.erotChassisOemPresent = present
}

// SetErotBackgroundCopyStatusPresent controls whether BackgroundCopyStatus is included in Oem.Nvidia.
func (r *RedfishMockServer) SetErotBackgroundCopyStatusPresent(present bool) {
	r.erotBackgroundCopyStatusPresent = present
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

func (r *RedfishMockServer) chassisAssetTag() string {
	if r.dpuVersion == BF4 {
		return DpuPSIDBF4
	}
	return r.assetTag
}

// SetModel sets the DPU model string
func (r *RedfishMockServer) SetModel(model string) {
	r.model = model
}

// SetBMCVersion sets the BMC firmware version returned by the mock server
func (r *RedfishMockServer) SetBMCVersion(version string) {
	r.bmcVersion = version
}

// SetFirmwareVersions sets the firmware versions returned by firmware inventory endpoints.
func (r *RedfishMockServer) SetFirmwareVersions(bmc, bmcErot, sbios, nic string) {
	r.bmcVersion = bmc
	r.bmcErotVersion = bmcErot
	r.sbiosVersion = sbios
	r.nicVersion = nic
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

// SetInstallBFBResponse forces handleInstallBFB to return the given HTTP status
// and raw body, bypassing the default HTTP 202 Accepted. Pass status=0 to
// restore the default. Used to simulate BMC errors such as a 404 when the BMC
// does not own rshim.
func (r *RedfishMockServer) SetInstallBFBResponse(status int, body string) {
	r.installBFBStatus = status
	r.installBFBBody = body
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

// handleVirtualMedia handles GET VirtualMedia and InsertMedia/EjectMedia for BF4 OS install.
func (r *RedfishMockServer) handleVirtualMedia(w http.ResponseWriter, req *http.Request) {
	const prefix = "/redfish/v1/Managers/Bluefield_BMC/VirtualMedia/"
	rest := strings.TrimPrefix(req.URL.Path, prefix)
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	mediaID := parts[0]

	switch req.Method {
	case http.MethodGet:
		if len(parts) != 1 {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		writeJSONResponse(w, map[string]interface{}{
			"@odata.id":  req.URL.Path,
			"Id":         mediaID,
			"Inserted":   r.virtualMediaInserted[mediaID],
			"Image":      "",
			"MediaTypes": []string{"CD", "USBStick"},
		})
	case http.MethodPost:
		if len(parts) != 3 || parts[1] != "Actions" {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		switch parts[2] {
		case "VirtualMedia.InsertMedia":
			r.virtualMediaInserted[mediaID] = true
		case "VirtualMedia.EjectMedia":
			r.virtualMediaInserted[mediaID] = false
		default:
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		writeJSONResponse(w, map[string]interface{}{
			"@odata.id": req.URL.Path,
		})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleBluefieldSystemSettings handles GET/PATCH boot settings for BF4 OS install.
func (r *RedfishMockServer) handleBluefieldSystemSettings(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		w.WriteHeader(http.StatusOK)
		writeJSONResponse(w, map[string]interface{}{
			"@odata.id": req.URL.Path,
			"Boot": map[string]interface{}{
				"BootSourceOverrideTarget":  r.bootSourceOverrideTarget,
				"BootSourceOverrideMode":    "UEFI",
				"BootSourceOverrideEnabled": r.bootSourceOverrideEnabled,
			},
		})
	case http.MethodPatch:
		var body struct {
			Boot struct {
				BootSourceOverrideTarget  string `json:"BootSourceOverrideTarget"`
				BootSourceOverrideEnabled string `json:"BootSourceOverrideEnabled"`
			} `json:"Boot"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		if body.Boot.BootSourceOverrideTarget != "" {
			r.bootSourceOverrideTarget = body.Boot.BootSourceOverrideTarget
		}
		if body.Boot.BootSourceOverrideEnabled != "" {
			r.bootSourceOverrideEnabled = body.Boot.BootSourceOverrideEnabled
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
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
