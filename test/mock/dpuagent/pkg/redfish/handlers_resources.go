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
	"time"

	rfclient "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
)

const erotChassisID = "BlueField_ERoT_BMC_0"

func (s *Server) handleRoot(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"@odata.context": "/redfish/v1/$metadata#ServiceRoot.ServiceRoot",
		"@odata.id":      "/redfish/v1",
		"@odata.type":    "#ServiceRoot.v1_15_0.ServiceRoot",
		"Id":             "RootService",
		"Name":           "Root Service",
		"RedfishVersion": "1.15.0",
		"Product":        s.state.Personality().Product,
		"Systems":        odata("/redfish/v1/Systems"),
		"Chassis":        odata("/redfish/v1/Chassis"),
		"Managers":       odata("/redfish/v1/Managers"),
		"UpdateService":  odata("/redfish/v1/UpdateService"),
		"TaskService":    odata("/redfish/v1/TaskService"),
		"AccountService": odata("/redfish/v1/AccountService"),
	})
}

func (s *Server) handleSystems(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"@odata.id":           "/redfish/v1/Systems",
		"@odata.type":         "#ComputerSystemCollection.ComputerSystemCollection",
		"Name":                "Computer System Collection",
		"Members":             []map[string]interface{}{odata("/redfish/v1/Systems/" + s.state.Personality().SystemID)},
		"Members@odata.count": 1,
	})
}

func (s *Server) handleManagers(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"@odata.id":           "/redfish/v1/Managers",
		"@odata.type":         "#ManagerCollection.ManagerCollection",
		"Name":                "Manager Collection",
		"Members":             []map[string]interface{}{odata("/redfish/v1/Managers/" + ManagerID)},
		"Members@odata.count": 1,
	})
}

func (s *Server) handleManager(w http.ResponseWriter, r *http.Request) {
	bmcVersion, _ := s.state.FirmwareVersion(s.state.Personality().InvBMC)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"@odata.id":       "/redfish/v1/Managers/" + r.PathValue("manager"),
		"@odata.type":     "#Manager.v1_14_0.Manager",
		"Id":              r.PathValue("manager"),
		"Name":            "OpenBmc Manager",
		"ManagerType":     "BMC",
		"DateTime":        time.Now().UTC().Format(time.RFC3339),
		"FirmwareVersion": bmcVersion,
		"Status":          map[string]interface{}{"State": "Enabled", "Health": "OK"},
	})
}

func (s *Server) handleChassis(w http.ResponseWriter, r *http.Request) {
	p := s.state.Personality()
	id := r.PathValue("chassis")
	switch id {
	case p.ChassisID:
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"@odata.context": "/redfish/v1/$metadata#Chassis.Chassis",
			"@odata.id":      "/redfish/v1/Chassis/" + id,
			"@odata.type":    "#Chassis.v1_20_0.Chassis",
			"Id":             id,
			"Name":           "BlueField DPU Card",
			"ChassisType":    "Card",
			"Manufacturer":   "Nvidia",
			"Model":          p.ChassisModel,
			"PartNumber":     p.PartNumber,
			"SerialNumber":   s.state.SerialNumber(),
			"AssetTag":       rfclient.ChassisAssetTagUnavailable,
			"Status":         map[string]interface{}{"State": "Enabled", "Health": "OK"},
		})
	case erotChassisID:
		if !p.IsBF4() {
			break
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"@odata.context": "/redfish/v1/$metadata#Chassis.Chassis",
			"@odata.id":      "/redfish/v1/Chassis/" + erotChassisID,
			"@odata.type":    "#Chassis.v1_20_0.Chassis",
			"Id":             erotChassisID,
			"Name":           "BlueField ERoT BMC",
			"Oem":            map[string]interface{}{"Nvidia": map[string]interface{}{"BackgroundCopyStatus": "Completed"}},
		})
		return
	}
	if id != p.ChassisID {
		writeError(w, http.StatusNotFound, "Base.1.18.1.ResourceMissingAtURI", "The resource at the URI /redfish/v1/Chassis/"+id+" was not found.")
	}
}

func (s *Server) handleSystem(w http.ResponseWriter, r *http.Request) {
	p := s.state.Personality()
	id := r.PathValue("system")
	if id != p.SystemID {
		writeError(w, http.StatusNotFound, "Base.1.18.1.ResourceMissingAtURI", "The resource at the URI /redfish/v1/Systems/"+id+" was not found.")
		return
	}
	powerState, statusState, oemLastState := s.state.Power()
	assetTag := rfclient.ChassisAssetTagUnavailable
	if p.IsBF4() {
		assetTag = s.state.PSID()
	}
	bootOverride, bootEnabled := s.state.BootOverride()
	base := "/redfish/v1/Systems/" + id
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"@odata.id":    base,
		"@odata.type":  "#ComputerSystem.v1_22_0.ComputerSystem",
		"Id":           id,
		"Name":         id,
		"Description":  "This ComputerSystem resource represents the SoC that is part of the DPU found in " + p.ChassisID,
		"SystemType":   "Physical",
		"Manufacturer": "Nvidia",
		"Model":        p.ChassisModel + " SmartNIC Main Card",
		"SerialNumber": s.state.SerialNumber(),
		"AssetTag":     assetTag,
		"PowerState":   powerState,
		"Status":       map[string]interface{}{"State": statusState, "Health": "OK", "Conditions": []interface{}{}},
		"BootProgress": map[string]interface{}{
			"LastState":     "OEM",
			"LastStateTime": time.Now().UTC().Format(time.RFC3339),
			"OemLastState":  oemLastState,
		},
		"Boot": map[string]interface{}{
			"BootSourceOverrideEnabled": bootEnabled,
			"BootSourceOverrideMode":    "UEFI",
			"BootSourceOverrideTarget":  bootOverride,
		},
		"Bios":       odata(base + "/Bios"),
		"SecureBoot": odata(base + "/SecureBoot"),
		"Links": map[string]interface{}{
			"Chassis":   []map[string]interface{}{odata("/redfish/v1/Chassis/" + p.ChassisID)},
			"ManagedBy": []map[string]interface{}{odata("/redfish/v1/Managers/" + ManagerID)},
		},
		"Actions": map[string]interface{}{
			"#ComputerSystem.Reset": map[string]interface{}{"target": base + "/Actions/ComputerSystem.Reset"},
		},
	})
}

func (s *Server) handleProductDescription(w http.ResponseWriter, r *http.Request) {
	p := s.state.Personality()
	base := "/redfish/v1/Systems/" + r.PathValue("system") + "/Oem/Nvidia"
	body := map[string]interface{}{
		"@odata.id":   base,
		"@odata.type": "#NvidiaComputerSystem.v1_0_0.NvidiaComputerSystem",
		"Actions": map[string]interface{}{
			"#SOC.ForceReset": map[string]interface{}{"target": base + "/SOC.ForceReset"},
		},
	}
	if !p.IsBF4() {
		// BF4 BMCs expose neither Description nor Mode on this resource.
		body["Id"] = "NvidiaComputerSystem"
		body["Name"] = "Nvidia Computer System"
		body["Mode"] = s.state.DPUMode()
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) handleBios(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"@odata.id":   "/redfish/v1/Systems/" + r.PathValue("system") + "/Bios",
		"@odata.type": "#Bios.v1_2_0.Bios",
		"Id":          "Bios",
		"Name":        "BIOS Configuration",
		"Attributes": map[string]interface{}{
			"NicMode":            s.state.DPUMode(),
			"HostPrivilegeLevel": s.state.HostPrivilegeMode(),
			"InternalCPUModel":   "Privileged",
		},
	})
}

func (s *Server) handleBiosSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, odata("/redfish/v1/Systems/"+r.PathValue("system")+"/Bios/Settings"))
}

func (s *Server) handleModeSet(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode string `json:"Mode"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Base.1.18.1.MalformedJSON", err.Error())
		return
	}
	if body.Mode != "" {
		s.state.SetDPUMode(body.Mode)
	}
	writeJSON(w, http.StatusOK, odata(r.URL.Path))
}

func (s *Server) handleSecureBoot(w http.ResponseWriter, r *http.Request) {
	base := "/redfish/v1/Systems/" + r.PathValue("system") + "/SecureBoot"
	if r.Method == http.MethodPatch {
		var body struct {
			SecureBootEnable *bool `json:"SecureBootEnable"`
		}
		if err := decodeBody(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "Base.1.18.1.MalformedJSON", err.Error())
			return
		}
		if body.SecureBootEnable != nil {
			s.state.SetSecureBootEnable(*body.SecureBootEnable)
		}
		writeJSON(w, http.StatusOK, odata(base))
		return
	}
	enable, current := s.state.SecureBoot()
	currentBoot := "Disabled"
	if current {
		currentBoot = "Enabled"
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"@odata.id":             base,
		"@odata.type":           "#SecureBoot.v1_1_0.SecureBoot",
		"Id":                    "SecureBoot",
		"Name":                  "UEFI Secure Boot",
		"SecureBootCurrentBoot": currentBoot,
		"SecureBootEnable":      enable,
		"SecureBootMode":        "UserMode",
	})
}

func (s *Server) handleSELEntries(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"@odata.id":           r.URL.Path,
		"@odata.type":         "#LogEntryCollection.LogEntryCollection",
		"Name":                "System Event Log Entries",
		"Members":             []interface{}{},
		"Members@odata.count": 0,
	})
}

func (s *Server) handleSystemSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPatch {
		var body struct {
			Boot struct {
				BootSourceOverrideTarget  string `json:"BootSourceOverrideTarget"`
				BootSourceOverrideEnabled string `json:"BootSourceOverrideEnabled"`
			} `json:"Boot"`
		}
		if err := decodeBody(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "Base.1.18.1.MalformedJSON", err.Error())
			return
		}
		s.state.SetBootOverride(body.Boot.BootSourceOverrideTarget, body.Boot.BootSourceOverrideEnabled)
		writeJSON(w, http.StatusOK, odata(r.URL.Path))
		return
	}
	target, enabled := s.state.BootOverride()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"@odata.id": r.URL.Path,
		"Boot": map[string]interface{}{
			"BootSourceOverrideTarget":  target,
			"BootSourceOverrideMode":    "UEFI",
			"BootSourceOverrideEnabled": enabled,
		},
	})
}

func (s *Server) handleNetworkDeviceFunction(w http.ResponseWriter, r *http.Request) {
	p := s.state.Personality()
	pf := r.PathValue("pf")
	if pf != p.PF0ID {
		writeError(w, http.StatusNotFound, "Base.1.18.1.ResourceMissingAtURI", "The resource at the URI "+r.URL.Path+" was not found.")
		return
	}
	ethernet := map[string]interface{}{"MTUSize": 1500}
	if p.IsBF4() {
		ethernet["PermanentMACAddress"] = s.state.PF0MAC()
	} else {
		ethernet["MACAddress"] = s.state.PF0MAC()
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"@odata.id":      p.NetworkAdapterPath + "/NetworkDeviceFunctions/" + pf,
		"@odata.type":    "#NetworkDeviceFunction.v1_9_0.NetworkDeviceFunction",
		"Id":             pf,
		"NetDevFuncType": "Ethernet",
		"Ethernet":       ethernet,
	})
}
