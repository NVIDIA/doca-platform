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
	"strconv"
	"time"

	rfclient "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
)

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
	ids := s.state.Personality().managerIDs()
	members := make([]map[string]interface{}, 0, len(ids))
	for _, id := range ids {
		members = append(members, odata("/redfish/v1/Managers/"+id))
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"@odata.id":           "/redfish/v1/Managers",
		"@odata.type":         "#ManagerCollection.ManagerCollection",
		"Name":                "Manager Collection",
		"Members":             members,
		"Members@odata.count": len(members),
	})
}

func (s *Server) handleManager(w http.ResponseWriter, r *http.Request) {
	if s.state.BMCDown() {
		writeError(w, http.StatusServiceUnavailable, "Base.1.18.1.ServiceTemporarilyUnavailable", "The BMC is rebooting")
		return
	}
	bmcVersion, _ := s.state.FirmwareVersion(s.state.Personality().InvBMC)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"@odata.id":       "/redfish/v1/Managers/" + r.PathValue("manager"),
		"@odata.type":     "#Manager.v1_14_0.Manager",
		"Id":              r.PathValue("manager"),
		"Name":            "OpenBmc Manager",
		"ManagerType":     "BMC",
		"DateTime":        time.Now().UTC().Format(time.RFC3339),
		"LastResetTime":   s.state.LastResetTime().Format(time.RFC3339),
		"FirmwareVersion": bmcVersion,
		"Status":          map[string]interface{}{"State": "Enabled", "Health": "OK"},
	})
}

// handleChassisCollection lists the chassis this personality serves.
func (s *Server) handleChassisCollection(w http.ResponseWriter, _ *http.Request) {
	p := s.state.Personality()
	ids := p.chassisIDs()
	members := make([]map[string]interface{}, 0, len(ids))
	for _, id := range ids {
		members = append(members, odata("/redfish/v1/Chassis/"+id))
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"@odata.id":           "/redfish/v1/Chassis",
		"@odata.type":         "#ChassisCollection.ChassisCollection",
		"Name":                "Chassis Collection",
		"Members":             members,
		"Members@odata.count": len(members),
	})
}

func (s *Server) handleChassis(w http.ResponseWriter, r *http.Request) {
	p := s.state.Personality()
	id := r.PathValue("chassis")
	switch {
	case id == p.ChassisID:
		s.writeIdentityChassis(w, p, id)
	case id != "" && id == p.ERoTChassisID:
		name := "BlueField ERoT BMC"
		if p.IsCMX() {
			name = "ERoT BMC"
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"@odata.context": "/redfish/v1/$metadata#Chassis.Chassis",
			"@odata.id":      "/redfish/v1/Chassis/" + id,
			"@odata.type":    "#Chassis.v1_20_0.Chassis",
			"Id":             id,
			"Name":           name,
			"Oem":            map[string]interface{}{"Nvidia": map[string]interface{}{"BackgroundCopyStatus": "Completed"}},
		})
	case id != "" && id == p.BMCChassisID:
		base := "/redfish/v1/Chassis/" + id
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"@odata.id":    base,
			"@odata.type":  "#Chassis.v1_20_0.Chassis",
			"Id":           id,
			"Name":         "BMC",
			"ChassisType":  "Component",
			"Manufacturer": "Nvidia",
			"Status":       map[string]interface{}{"State": "Enabled", "Health": "OK"},
			"Actions": map[string]interface{}{
				"Oem": map[string]interface{}{
					"#NvidiaChassis.AuxPowerReset": map[string]interface{}{
						"target": base + "/Actions/Oem/NvidiaChassis.AuxPowerReset",
					},
				},
			},
		})
	default:
		for _, nic := range p.NICs {
			if nic.ChassisID == id && id != p.ChassisID {
				base := "/redfish/v1/Chassis/" + id
				body := map[string]interface{}{
					"@odata.id":       base,
					"@odata.type":     "#Chassis.v1_20_0.Chassis",
					"Id":              id,
					"Name":            id,
					"ChassisType":     "Module",
					"Manufacturer":    "Nvidia",
					"Model":           nic.Model,
					"PartNumber":      p.PartNumber,
					"SerialNumber":    s.state.SerialNumber(),
					"NetworkAdapters": odata(base + "/NetworkAdapters"),
					"Status":          map[string]interface{}{"State": "Enabled", "Health": "OK"},
				}
				if nic.ServiceLabel != "" {
					body["Location"] = map[string]interface{}{
						"PartLocation": map[string]interface{}{
							"LocationType":         "Embedded",
							"LocationOrdinalValue": nic.LocationOrdinal,
							"ServiceLabel":         nic.ServiceLabel,
						},
						"PartLocationContext": "N1",
					}
				}
				writeJSON(w, http.StatusOK, body)
				return
			}
		}
		if p.hasChassis(id) {
			s.writeComponentChassis(w, p, id)
			return
		}
		writeError(w, http.StatusNotFound, "Base.1.18.1.ResourceMissingAtURI", "The resource at the URI /redfish/v1/Chassis/"+id+" was not found.")
	}
}

// writeComponentChassis writes a CMX chassis that is not the node, the BMC, the ERoT or a CX9.
func (s *Server) writeComponentChassis(w http.ResponseWriter, p Personality, id string) {
	base := "/redfish/v1/Chassis/" + id
	body := map[string]interface{}{
		"@odata.id":    base,
		"@odata.type":  "#Chassis.v1_20_0.Chassis",
		"Id":           id,
		"Name":         id,
		"ChassisType":  "Component",
		"Manufacturer": "Nvidia",
		"Model":        p.ChassisModel,
		"Status":       map[string]interface{}{"State": "Enabled", "Health": "OK"},
	}
	if id == "OSFP_Board_0" {
		body["Model"] = "CMX OSFP Board"
		body["Links"] = map[string]interface{}{
			"Contains": []map[string]interface{}{
				odata("/redfish/v1/Chassis/MCU_OSFP_0"),
				odata("/redfish/v1/Chassis/CX_0"),
				odata("/redfish/v1/Chassis/CX_1"),
			},
		}
	}
	writeJSON(w, http.StatusOK, body)
}

// writeIdentityChassis writes the chassis the controller reads for serial number, model and PSID.
func (s *Server) writeIdentityChassis(w http.ResponseWriter, p Personality, id string) {
	name := "BlueField DPU Card"
	chassisType := "Card"
	assetTag := rfclient.ChassisAssetTagUnavailable
	body := map[string]interface{}{
		"@odata.context": "/redfish/v1/$metadata#Chassis.Chassis",
		"@odata.id":      "/redfish/v1/Chassis/" + id,
		"@odata.type":    "#Chassis.v1_20_0.Chassis",
		"Id":             id,
		"Manufacturer":   "Nvidia",
		"Model":          p.ChassisModel,
		"PartNumber":     p.PartNumber,
		"SerialNumber":   s.state.SerialNumber(),
		"Status":         map[string]interface{}{"State": "Enabled", "Health": "OK"},
	}
	if p.IsCMX() {
		name = "CMX"
		chassisType = "Zone"
		assetTag = s.state.PSID()
		body["Location"] = map[string]interface{}{
			"PartLocation": map[string]interface{}{
				"LocationType":         "Slot",
				"LocationOrdinalValue": 0,
				"ServiceLabel":         "N1",
			},
			"PartLocationContext": "Node_0",
		}
	}
	body["Name"] = name
	body["ChassisType"] = chassisType
	body["AssetTag"] = assetTag
	writeJSON(w, http.StatusOK, body)
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
	if p.IsBF4() || p.IsCMX() {
		assetTag = s.state.PSID()
	}
	description := "This ComputerSystem resource represents the SoC that is part of the DPU found in " + p.ChassisID
	model := p.ChassisModel + " SmartNIC Main Card"
	if p.IsCMX() {
		description = "CMX node"
		model = "CMX"
	}
	bootOverride, bootEnabled := s.state.BootOverride()
	base := "/redfish/v1/Systems/" + id
	bootProgress := map[string]interface{}{
		"LastState":     "OEM",
		"LastStateTime": time.Now().UTC().Format(time.RFC3339),
		"OemLastState":  oemLastState,
	}
	if p.IsCMX() {
		bootProgress = cmxBootProgress(oemLastState)
	}
	chassisLinkID := p.ChassisID
	if p.IsCMX() {
		chassisLinkID = p.BMCChassisID
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"@odata.id":    base,
		"@odata.type":  "#ComputerSystem.v1_22_0.ComputerSystem",
		"Id":           id,
		"Name":         id,
		"Description":  description,
		"SystemType":   "Physical",
		"Manufacturer": "Nvidia",
		"Model":        model,
		"SerialNumber": s.state.SerialNumber(),
		"AssetTag":     assetTag,
		"PowerState":   powerState,
		"Status":       map[string]interface{}{"State": statusState, "Health": "OK", "Conditions": []interface{}{}},
		"BootProgress": bootProgress,
		"Boot": map[string]interface{}{
			"BootSourceOverrideEnabled": bootEnabled,
			"BootSourceOverrideMode":    "UEFI",
			"BootSourceOverrideTarget":  bootOverride,
		},
		"Bios":       odata(base + "/Bios"),
		"SecureBoot": odata(base + "/SecureBoot"),
		"Links": map[string]interface{}{
			"Chassis":   []map[string]interface{}{odata("/redfish/v1/Chassis/" + chassisLinkID)},
			"ManagedBy": []map[string]interface{}{odata("/redfish/v1/Managers/" + p.ManagerID)},
		},
		"Actions": map[string]interface{}{
			"#ComputerSystem.Reset": map[string]interface{}{"target": base + "/Actions/ComputerSystem.Reset"},
		},
	})
}

// cmxBootProgress is the CMX BootProgress. The Vera BMC names the milestone in the standard
// LastState (OSRunning once the OS is up) and has no OemLastState, unlike the BF3 BMC. oemLastState
// is the mock's internal boot state.
func cmxBootProgress(oemLastState string) map[string]interface{} {
	lastState := "OSBootStarted"
	switch oemLastState {
	case OemLastStateOSUp:
		lastState = "OSRunning"
	case OemLastStateOff:
		lastState = "None"
	}
	return map[string]interface{}{
		"LastState":     lastState,
		"LastStateTime": time.Now().UTC().Format(time.RFC3339),
	}
}

func (s *Server) handleProductDescription(w http.ResponseWriter, r *http.Request) {
	p := s.state.Personality()
	if p.IsCMX() {
		writeError(w, http.StatusNotFound, "Base.1.18.1.ResourceMissingAtURI", "The resource at the URI "+r.URL.Path+" was not found.")
		return
	}
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
	if s.state.Personality().IsCMX() {
		writeError(w, http.StatusNotFound, "Base.1.18.1.ResourceMissingAtURI", "The resource at the URI "+r.URL.Path+" was not found.")
		return
	}
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

// handleNetworkAdapterCollection lists the ConnectX adapters on a chassis.
func (s *Server) handleNetworkAdapterCollection(w http.ResponseWriter, r *http.Request) {
	p := s.state.Personality()
	chassis := r.PathValue("chassis")
	members := make([]map[string]interface{}, 0)
	for _, nic := range p.NICs {
		if nic.ChassisID == chassis {
			members = append(members, odata(r.URL.Path+"/"+nic.AdapterID))
		}
	}
	if len(members) == 0 && !p.hasChassis(chassis) {
		writeError(w, http.StatusNotFound, "Base.1.18.1.ResourceMissingAtURI", "The resource at the URI "+r.URL.Path+" was not found.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"@odata.id":           r.URL.Path,
		"@odata.type":         "#NetworkAdapterCollection.NetworkAdapterCollection",
		"Name":                "Network Adapter Collection",
		"Members":             members,
		"Members@odata.count": len(members),
	})
}

// handleNetworkAdapter writes one ConnectX NetworkAdapter. CMX adapters have no
// NetworkDeviceFunctions link and no MAC address.
func (s *Server) handleNetworkAdapter(w http.ResponseWriter, r *http.Request) {
	p := s.state.Personality()
	chassis := r.PathValue("chassis")
	adapter := r.PathValue("adapter")
	for _, nic := range p.NICs {
		if nic.ChassisID != chassis || nic.AdapterID != adapter {
			continue
		}
		base := "/redfish/v1/Chassis/" + chassis + "/NetworkAdapters/" + adapter
		body := map[string]interface{}{
			"@odata.id":    base,
			"@odata.type":  "#NetworkAdapter.v1_11_0.NetworkAdapter",
			"Id":           adapter,
			"Name":         adapter,
			"Manufacturer": "Nvidia",
			"Model":        nic.Model,
			"PartNumber":   p.PartNumber,
			"SerialNumber": s.state.SerialNumber(),
			"Controllers":  []interface{}{},
			"Ports":        odata(base + "/Ports"),
			"Status":       map[string]interface{}{"State": "Enabled", "Conditions": []interface{}{}},
		}
		if !p.IsCMX() {
			body["NetworkDeviceFunctions"] = odata(base + "/NetworkDeviceFunctions")
		}
		writeJSON(w, http.StatusOK, body)
		return
	}
	writeError(w, http.StatusNotFound, "Base.1.18.1.ResourceMissingAtURI", "The resource at the URI "+r.URL.Path+" was not found.")
}

// handleNetworkPorts lists the ports of a ConnectX adapter. CMX CX9 adapters have Port_0 and Port_1
// and neither port carries a MAC address.
func (s *Server) handleNetworkPorts(w http.ResponseWriter, r *http.Request) {
	if !s.hasAdapter(r.PathValue("chassis"), r.PathValue("adapter")) {
		writeError(w, http.StatusNotFound, "Base.1.18.1.ResourceMissingAtURI", "The resource at the URI "+r.URL.Path+" was not found.")
		return
	}
	count := 1
	if s.state.Personality().IsCMX() {
		count = 2
	}
	members := make([]map[string]interface{}, 0, count)
	for i := 0; i < count; i++ {
		members = append(members, odata(r.URL.Path+"/Port_"+strconv.Itoa(i)))
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"@odata.id":           r.URL.Path,
		"@odata.type":         "#PortCollection.PortCollection",
		"Name":                "Port Collection",
		"Members":             members,
		"Members@odata.count": len(members),
	})
}

// handleNetworkPort writes one adapter port. CMX ports have no MACAddress.
func (s *Server) handleNetworkPort(w http.ResponseWriter, r *http.Request) {
	if !s.hasAdapter(r.PathValue("chassis"), r.PathValue("adapter")) {
		writeError(w, http.StatusNotFound, "Base.1.18.1.ResourceMissingAtURI", "The resource at the URI "+r.URL.Path+" was not found.")
		return
	}
	id := r.PathValue("port")
	if id != "Port_0" && (!s.state.Personality().IsCMX() || id != "Port_1") {
		writeError(w, http.StatusNotFound, "Base.1.18.1.ResourceMissingAtURI", "The resource at the URI "+r.URL.Path+" was not found.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"@odata.id":    r.URL.Path,
		"@odata.type":  "#Port.v1_8_0.Port",
		"Id":           id,
		"Name":         "Port",
		"PortProtocol": "Ethernet",
		"LinkStatus":   "LinkDown",
		"Status":       map[string]interface{}{"State": "Disabled", "Health": "OK"},
	})
}

// hasAdapter reports whether a NIC with this chassis and adapter exists.
func (s *Server) hasAdapter(chassis, adapter string) bool {
	for _, nic := range s.state.Personality().NICs {
		if nic.ChassisID == chassis && nic.AdapterID == adapter {
			return true
		}
	}
	return false
}

// handleDrive writes a drive named by a firmware RelatedItem. CMX drive firmware points at these.
func (s *Server) handleDrive(w http.ResponseWriter, r *http.Request) {
	if !s.relatedTarget(r.URL.Path) {
		writeError(w, http.StatusNotFound, "Base.1.18.1.ResourceMissingAtURI", "The resource at the URI "+r.URL.Path+" was not found.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"@odata.id":   r.URL.Path,
		"@odata.type": "#Drive.v1_22_0.Drive",
		"Id":          r.PathValue("drive"),
		"Name":        r.PathValue("drive"),
		"Status":      map[string]interface{}{"State": "Enabled", "Health": "OK"},
	})
}

// handleProcessor writes the Vera CPU that SBIOS firmware points at.
func (s *Server) handleProcessor(w http.ResponseWriter, r *http.Request) {
	p := s.state.Personality()
	if !p.IsCMX() || r.PathValue("system") != p.SystemID || r.PathValue("processor") != "CPU_0" {
		writeError(w, http.StatusNotFound, "Base.1.18.1.ResourceMissingAtURI", "The resource at the URI "+r.URL.Path+" was not found.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"@odata.id":     r.URL.Path,
		"@odata.type":   "#Processor.v1_18_0.Processor",
		"Id":            "CPU_0",
		"Name":          "Processor",
		"ProcessorType": "CPU",
		"Status":        map[string]interface{}{"State": "Enabled", "Health": "OK"},
	})
}

// relatedTarget reports whether path is a RelatedItem of a firmware inventory member.
func (s *Server) relatedTarget(path string) bool {
	for _, member := range s.state.Personality().Firmware {
		for _, related := range member.Related {
			if related == path {
				return true
			}
		}
	}
	return false
}

func (s *Server) handleNetworkDeviceFunction(w http.ResponseWriter, r *http.Request) {
	p := s.state.Personality()
	if p.IsCMX() {
		writeError(w, http.StatusNotFound, "Base.1.18.1.ResourceMissingAtURI", "The resource at the URI "+r.URL.Path+" was not found.")
		return
	}
	pf := r.PathValue("pf")
	if pf != p.PF0ID {
		writeError(w, http.StatusNotFound, "Base.1.18.1.ResourceMissingAtURI", "The resource at the URI "+r.URL.Path+" was not found.")
		return
	}
	ethernet := map[string]interface{}{"MTUSize": 1500}
	if p.IsBF4() || p.IsCMX() {
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
