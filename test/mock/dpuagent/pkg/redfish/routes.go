/*
Copyright 2026 NVIDIA

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

package redfish

import (
	"net/http"

	rfclient "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"

	"k8s.io/klog/v2"
)

// route is one entry of the Redfish route table. Name is the operation name users refer to in
// bmc.responseDelayOverrides; several routes share a name when they are the same resource
// reached with different methods or spellings.
type route struct {
	name    string
	method  string
	path    string
	handler func(*Server, http.ResponseWriter, *http.Request)
}

// pattern is the ServeMux pattern the route is registered under.
func (rt route) pattern() string {
	return rt.method + " " + rt.path
}

// routes is the route table. Paths follow the client package constants; {system}, {chassis}
// and {manager} are accepted for any value because the controller resolves the IDs from the
// collections this server returns. Redfish actions are named after the action, other resources
// after the resource; the README lists the table.
var routes = []route{
	// Service root and collections.
	{"ServiceRoot", http.MethodGet, "/" + rfclient.APIRootService, (*Server).handleRoot},
	{"ServiceRoot", http.MethodGet, "/" + rfclient.APIRootService + "/{$}", (*Server).handleRoot},
	{"Systems", http.MethodGet, "/" + rfclient.APIGetSystems, (*Server).handleSystems},
	{"Managers", http.MethodGet, "/" + rfclient.APIGetManagers, (*Server).handleManagers},
	{"Manager", http.MethodGet, "/redfish/v1/Managers/{manager}", (*Server).handleManager},

	// Chassis, system and NIC resources.
	{"Chassis", http.MethodGet, "/redfish/v1/Chassis/{chassis}", (*Server).handleChassis},
	{"System", http.MethodGet, "/redfish/v1/Systems/{system}", (*Server).handleSystem},
	{"SystemOem", http.MethodGet, "/redfish/v1/Systems/{system}/Oem/Nvidia", (*Server).handleProductDescription},
	{"Bios", http.MethodGet, "/redfish/v1/Systems/{system}/Bios", (*Server).handleBios},
	{"BiosSettings", http.MethodPatch, "/redfish/v1/Systems/{system}/Bios/Settings", (*Server).handleBiosSettings},
	{"Mode.Set", http.MethodPost, "/redfish/v1/Systems/{system}/Oem/Nvidia/Actions/Mode.Set", (*Server).handleModeSet},
	{"SecureBoot", http.MethodGet, "/redfish/v1/Systems/{system}/SecureBoot", (*Server).handleSecureBoot},
	{"SecureBoot", http.MethodPatch, "/redfish/v1/Systems/{system}/SecureBoot", (*Server).handleSecureBoot},
	{"SELEntries", http.MethodGet, "/redfish/v1/Systems/{system}/LogServices/SEL/Entries", (*Server).handleSELEntries},
	{"SystemSettings", http.MethodGet, "/redfish/v1/Systems/{system}/Settings", (*Server).handleSystemSettings},
	{"SystemSettings", http.MethodPatch, "/redfish/v1/Systems/{system}/Settings", (*Server).handleSystemSettings},
	{"NetworkDeviceFunction", http.MethodGet, "/redfish/v1/Chassis/{chassis}/NetworkAdapters/{adapter}/NetworkDeviceFunctions/{pf}", (*Server).handleNetworkDeviceFunction},

	// Power and reset.
	{"ComputerSystem.Reset", http.MethodPost, "/redfish/v1/Systems/{system}/Actions/ComputerSystem.Reset", (*Server).handleSystemReset},
	{"SOC.ForceReset", http.MethodPost, "/redfish/v1/Systems/{system}/Oem/Nvidia/SOC.ForceReset", (*Server).handleSOCForceReset},
	{"NvidiaChassis.Reset", http.MethodPost, "/redfish/v1/Chassis/{chassis}/Actions/Oem/NvidiaChassis.Reset", (*Server).handleChassisReset},

	// Firmware parameters.
	{"HostRshim.Set", http.MethodPost, "/redfish/v1/Systems/{system}/Oem/Nvidia/Actions/HostRshim.Set", (*Server).handleHostRshimSet},
	{"BMCRshim", http.MethodGet, "/" + rfclient.APIEnableBMCRshim, (*Server).handleBMCRShimOem},
	{"BMCRshim", http.MethodPatch, "/" + rfclient.APIEnableBMCRshim, (*Server).handleBMCRShimOem},
	{"HostPrivilegeConfig", http.MethodPatch, "/" + rfclient.APIHostPrivilegeConfigSettings, (*Server).handleHostPrivilegeConfig},

	// Accounts, factory reset, BMC reset, mTLS.
	{"Account", http.MethodPatch, "/redfish/v1/AccountService/Accounts/{user}", (*Server).handleAccountPatch},
	{"AccountService", http.MethodPatch, "/" + rfclient.APIEnableMTLS, (*Server).handleEnableMTLS},
	{"Manager.ResetToDefaults", http.MethodPost, "/redfish/v1/Managers/{manager}/Actions/Manager.ResetToDefaults", (*Server).handleFactoryReset},
	{"Manager.Reset", http.MethodPost, "/redfish/v1/Managers/{manager}/Actions/Manager.Reset", (*Server).handleResetBMC},

	// Certificates.
	{"CertificateService.GenerateCSR", http.MethodPost, "/" + rfclient.APIGenerateCSR, (*Server).handleGenerateCSR},
	{"CertificateService.ReplaceCertificate", http.MethodPost, "/" + rfclient.APIReplaceCert, (*Server).handleReplaceCertificate},
	{"ServerCertificate", http.MethodGet, "/redfish/v1/Managers/{manager}/NetworkProtocol/HTTPS/Certificates/1", (*Server).handleServerCert},
	{"TruststoreCertificates", http.MethodGet, "/redfish/v1/Managers/{manager}/Truststore/Certificates", (*Server).handleTruststoreCollection},
	{"TruststoreCertificates", http.MethodPost, "/redfish/v1/Managers/{manager}/Truststore/Certificates", (*Server).handleTruststoreCollection},
	{"TruststoreCertificates", http.MethodGet, "/redfish/v1/Managers/{manager}/Truststore/Certificates/{id}", (*Server).handleTruststoreCert},
	{"TruststoreCertificates", http.MethodDelete, "/redfish/v1/Managers/{manager}/Truststore/Certificates/{id}", (*Server).handleTruststoreCert},

	// Update service and tasks.
	{"UpdateService", http.MethodGet, "/" + rfclient.APIUpdateFW, (*Server).handleUpdateServiceGet},
	{"UpdateService", http.MethodPost, "/" + rfclient.APIUpdateFW, (*Server).handleBMCFirmwarePush},
	{"UpdateService.SimpleUpdate", http.MethodPost, "/" + rfclient.APIInstallBFB, (*Server).handleSimpleUpdate},
	{"FirmwareInventory", http.MethodGet, "/redfish/v1/UpdateService/FirmwareInventory/{id}", (*Server).handleFirmwareInventory},
	{"UpdateMultipart", http.MethodPost, "/" + rfclient.APIUpdateBluefieldFWMultipart, (*Server).handleUpdateMultipart},
	{"UpdateService.Activate", http.MethodPost, "/" + rfclient.APIActivatePendingBundle, (*Server).handleActivatePendingBundle},
	{"Task", http.MethodGet, "/redfish/v1/TaskService/Tasks/{id}", (*Server).handleTask},

	// Virtual media.
	{"VirtualMedia", http.MethodGet, "/redfish/v1/Managers/{manager}/VirtualMedia/{media}", (*Server).handleVirtualMediaGet},
	{"VirtualMedia", http.MethodPost, "/redfish/v1/Managers/{manager}/VirtualMedia/{media}/Actions/{action}", (*Server).handleVirtualMediaAction},
}

// handler builds the ServeMux from the route table. Every route and the catch-all 404 wait the
// response delay that applies to them before the handler runs.
func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	for _, rt := range routes {
		mux.HandleFunc(rt.pattern(), s.delayed(s.delayFor(rt.pattern()), func(w http.ResponseWriter, r *http.Request) {
			rt.handler(s, w, r)
		}))
	}
	mux.HandleFunc("/", s.delayed(s.defaultDelay, func(w http.ResponseWriter, r *http.Request) {
		klog.V(2).InfoS("unhandled Redfish request", "method", r.Method, "path", r.URL.Path)
		writeError(w, http.StatusNotFound, "Base.1.18.1.ResourceMissingAtURI",
			"The resource at the URI "+r.URL.Path+" was not found.")
	}))
	return s.logRequests(mux)
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		klog.V(3).InfoS("Redfish request", "method", r.Method, "path", r.URL.Path, "remote", r.RemoteAddr)
		next.ServeHTTP(w, r)
	})
}
