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

// Package redfish is the trimmed BlueField BMC Redfish server of mock-dpuagent. It answers every
// request the provisioning controllers make during zero-trust provisioning of one DPU, streams the
// artifacts they point it at and drives the simulated dpu-agent through the Supervisor.
package redfish

import (
	"fmt"

	rfclient "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
	"github.com/nvidia/doca-platform/test/mock/dpuagent/pkg/config"
)

// ManagerID is the BMC manager resource name on BlueField-3 and BlueField-4; the controller picks
// the member whose @odata.id contains "bmc". BF4-CMX uses BMC_0, which still matches that check.
const ManagerID = "Bluefield_BMC"

// NICSlot is one ConnectX the BMC exposes: its chassis, firmware inventory member and network adapter.
type NICSlot struct {
	ChassisID  string
	FirmwareID string
	AdapterID  string
	Model      string
	// ServiceLabel is the silk label, such as "NIC 0". Empty on BlueField, where the NIC is the card.
	ServiceLabel string
	// LocationOrdinal is the 0-based slot of a CMX CX9. Ignored when ServiceLabel is empty.
	LocationOrdinal int
}

// FirmwareMember is one UpdateService/FirmwareInventory member.
type FirmwareMember struct {
	ID string
	// Related are RelatedItem @odata.id values. Written only when ReportRelated is set, so an
	// empty slice is an empty RelatedItem array rather than an omitted property.
	Related       []string
	ReportRelated bool
	// DualSlot members always expose Oem.Nvidia ActiveFirmwareSlot and InactiveFirmwareSlot.
	// CMX CX9, MCU, CPLD and drive firmware do not.
	DualSlot bool
	// HostReset reports whether ComputerSystem.Reset promotes a staged image. A Node AC cycle
	// promotes every staged member.
	HostReset bool
}

// Personality holds the per-generation constants the controller uses to recognize the DPU type
// and to address its resources.
type Personality struct {
	Type config.DPUType
	// Product is returned by GET /redfish/v1; the controller treats anything containing "B4" or
	// "BLUEFIELD-4" as BlueField-4. "CMX" matches neither, which is how a Vera BMC identifies itself.
	Product      string
	User         string
	ManagerID    string
	SystemID     string
	ChassisID    string
	ChassisModel string
	PartNumber   string
	// ManagerIDs lists Managers collection members. Empty means ManagerID alone.
	ManagerIDs []string
	// ExtraChassis lists chassis members besides the identity chassis, the BMC chassis, the ERoT
	// and the NIC chassis.
	ExtraChassis []string
	// BMCChassisID is the chassis that owns NvidiaChassis.AuxPowerReset. Empty when the generation
	// has no Node AC cycle.
	BMCChassisID string
	// ERoTChassisID is the ERoT chassis. Empty when the generation has none.
	ERoTChassisID string
	// PowerOffState is the ComputerSystem.PowerState of a shut down host.
	PowerOffState string
	// Firmware inventory member IDs. Empty when the generation has no such member.
	InvBMC           string
	InvERoT          string
	InvUEFI          string
	InvNIC           string
	InvBSP           string
	InvOS            string
	InvBoard         string
	InvOSImage       string
	InvOSConfig      string
	InvPendingBundle string
	// Firmware is the CMX inventory. Empty on BlueField, which uses the Inv* member IDs.
	Firmware []FirmwareMember
	// NICs are the ConnectX cards. BlueField-3 and BlueField-4 have one integrated NIC.
	// BF4-CMX has two CX9s. A PLDM bundle carries one NIC version, staged for both cards.
	// CX9 inventory members have no inactive firmware slot.
	NICs []NICSlot
	// VirtualMedia lists the slots that accept InsertMedia. Empty means every slot id is accepted,
	// which is what the BlueField install flow relies on (IMAGE and CONFIG).
	VirtualMedia []string
	// VirtualMediaReadOnly lists slots that exist but have no InsertMedia action. CMX Slot_0 is one.
	VirtualMediaReadOnly []string
	// SeedMedia is the slot that carries the per-DPU cidata seed ISO the dpu-agent identity is
	// taken from. Empty when the generation delivers it through SimpleUpdate.
	SeedMedia string
	// NetworkAdapterPath is the path of the PF0 network adapter.
	NetworkAdapterPath string
	PF0ID              string
}

// ForType returns the personality for a DPU generation. An unknown type is BlueField-3.
func ForType(t config.DPUType) Personality {
	switch t {
	case config.DPUTypeBF4:
		return Personality{
			Type:               config.DPUTypeBF4,
			Product:            "BLUEFIELD-4",
			User:               rfclient.BF4BMCUser,
			ManagerID:          ManagerID,
			SystemID:           "BlueField_0",
			ChassisID:          "BlueField_0",
			ChassisModel:       "BlueField-4",
			PartNumber:         "900-9D3B4-00SV-EA0",
			ERoTChassisID:      "BlueField_ERoT_BMC_0",
			PowerOffState:      "Paused",
			InvBMC:             "BlueField_FW_BMC_0",
			InvERoT:            "BlueField_FW_ERoT_BMC_0",
			InvUEFI:            "BlueField_FW_CPU_0",
			InvNIC:             "BlueField_FW_NIC_0",
			InvOSImage:         "BlueField_OS_Image_CPU_0",
			InvOSConfig:        "BlueField_OS_Config_CPU_0",
			InvPendingBundle:   "Pending_Bundle",
			NICs:               []NICSlot{{ChassisID: "BlueField_0", FirmwareID: "BlueField_FW_NIC_0", AdapterID: "BlueField_NIC_0", Model: "BlueField-4"}},
			NetworkAdapterPath: "/redfish/v1/Chassis/BlueField_0/NetworkAdapters/BlueField_NIC_0",
			PF0ID:              "0",
		}
	case config.DPUTypeBF4CMX:
		return cmxPersonality()
	default:
		return Personality{
			Type:               config.DPUTypeBF3,
			Product:            "BlueField",
			User:               rfclient.BF3BMCUser,
			ManagerID:          ManagerID,
			SystemID:           "Bluefield",
			ChassisID:          "Card1",
			ChassisModel:       "BlueField-3",
			PartNumber:         "900-9D3B6-00CC-EA0",
			PowerOffState:      "Off",
			InvBMC:             "BMC_Firmware",
			InvUEFI:            "DPU_UEFI",
			InvNIC:             "DPU_NIC",
			InvBSP:             "DPU_BSP",
			InvOS:              "DPU_OS",
			InvBoard:           "DPU_BOARD",
			NICs:               []NICSlot{{ChassisID: "Card1", FirmwareID: "DPU_NIC", AdapterID: "NvidiaNetworkAdapter", Model: "BlueField-3"}},
			NetworkAdapterPath: "/redfish/v1/Chassis/Card1/NetworkAdapters/NvidiaNetworkAdapter",
			PF0ID:              "eth0f0",
		}
	}
}

// IsBF4 reports whether this is the BlueField-4 personality.
func (p Personality) IsBF4() bool {
	return p.Type == config.DPUTypeBF4
}

// IsCMX reports whether this is the BF4-CMX personality.
func (p Personality) IsCMX() bool {
	return p.Type == config.DPUTypeBF4CMX
}

// cmxPersonality is the Vera BMC tree for one CMX node, following the v0.2 north-star mockup.
// Resource ids stay BMC_0 and System_0 on every node. N2 is the same tree with a different Location.
func cmxPersonality() Personality {
	cpuRelated := []string{
		"/redfish/v1/Chassis/CPU_0",
		"/redfish/v1/Systems/System_0/Processors/CPU_0",
	}
	member := func(id string, related []string, dual, hostReset bool) FirmwareMember {
		return FirmwareMember{ID: id, Related: related, ReportRelated: true, DualSlot: dual, HostReset: hostReset}
	}
	chassis := func(id string) []string { return []string{"/redfish/v1/Chassis/" + id} }
	fw := []FirmwareMember{
		member("FW_BMC_0", chassis("BMC_0"), true, false),
		member("FW_ERoT_BMC_0", chassis("ERoT_BMC_0"), true, false),
		member("SBIOS_FW_0", cpuRelated, true, true),
		member("SBIOS_FMC_0", cpuRelated, true, true),
		member("FW_CX_0", chassis("CX_0"), false, true),
		member("FW_CX_1", chassis("CX_1"), false, true),
		member("FW_CPLD_0", chassis("CPLD_0"), false, false),
		member("FW_MCU_0", chassis("MCU_0"), false, false),
		member("FW_MCU_1", chassis("MCU_1"), false, false),
		member("FW_MCU_HPM_0", chassis("MCU_HPM_0"), false, false),
		member("FW_MCU_OSFP_0", chassis("MCU_OSFP_0"), false, false),
		member("FW_ProcessorModule_SMA_0", chassis("ProcessorModule_SMA_0"), true, false),
		member("FW_StorageBackplane_SMA_0", nil, true, false),
		member("FW_NVMe_M2_0", []string{"/redfish/v1/Chassis/ProcessorModule_0/Drives/NVMe_M2_0"}, false, false),
	}
	for i := 0; i < 12; i++ {
		drive := fmt.Sprintf("NVMe_E3S_SSD%d", i)
		fw = append(fw, member("FW_"+drive, []string{"/redfish/v1/Chassis/StorageBackplane_0/Drives/" + drive}, false, false))
	}
	return Personality{
		Type:         config.DPUTypeBF4CMX,
		Product:      "CMX",
		User:         rfclient.BF3BMCUser,
		ManagerID:    "BMC_0",
		ManagerIDs:   []string{"BMC_0", "SMM_0", "StorageBackplane_0"},
		SystemID:     "System_0",
		ChassisID:    "Chassis_0",
		ChassisModel: "CMX",
		PartNumber:   "900-00000-0000-000",
		ExtraChassis: []string{
			"CPLD_0", "CPU_0", "MCU_0", "MCU_1", "MCU_HPM_0", "MCU_OSFP_0", "OSFP_Board_0",
			"ProcessorModule_0", "ProcessorModule_SMA_0", "SMM_0", "StorageBackplane_0", "StorageBackplane_SMA_0",
		},
		BMCChassisID:  "BMC_0",
		ERoTChassisID: "ERoT_BMC_0",
		PowerOffState: "Off",
		InvBMC:        "FW_BMC_0",
		InvERoT:       "FW_ERoT_BMC_0",
		InvUEFI:       "SBIOS_FW_0",
		InvNIC:        "FW_CX_0",
		Firmware:      fw,
		NICs: []NICSlot{
			{ChassisID: "CX_0", FirmwareID: "FW_CX_0", AdapterID: "CX_NIC_0", Model: "ConnectX-9 800GE 2P NIC", ServiceLabel: "NIC 0"},
			{ChassisID: "CX_1", FirmwareID: "FW_CX_1", AdapterID: "CX_NIC_1", Model: "ConnectX-9 800GE 2P NIC", ServiceLabel: "NIC 1", LocationOrdinal: 1},
		},
		VirtualMedia:         []string{"USB1", "USB2"},
		VirtualMediaReadOnly: []string{"Slot_0"},
		SeedMedia:            "USB2",
		NetworkAdapterPath:   "/redfish/v1/Chassis/CX_0/NetworkAdapters/CX_NIC_0",
		PF0ID:                "0",
	}
}

// allowsVirtualMedia reports whether slot accepts InsertMedia.
func (p Personality) allowsVirtualMedia(slot string) bool {
	if len(p.VirtualMedia) == 0 && len(p.VirtualMediaReadOnly) == 0 {
		return true
	}
	for _, id := range p.VirtualMedia {
		if id == slot {
			return true
		}
	}
	return false
}

// virtualMediaExists reports whether slot is a VirtualMedia member.
func (p Personality) virtualMediaExists(slot string) bool {
	if p.allowsVirtualMedia(slot) && len(p.VirtualMedia) == 0 && len(p.VirtualMediaReadOnly) == 0 {
		return true
	}
	if p.allowsVirtualMedia(slot) {
		return true
	}
	for _, id := range p.VirtualMediaReadOnly {
		if id == slot {
			return true
		}
	}
	return false
}

// virtualMediaIDs lists VirtualMedia members. Empty means the generation does not publish a fixed set.
func (p Personality) virtualMediaIDs() []string {
	if len(p.VirtualMedia) == 0 && len(p.VirtualMediaReadOnly) == 0 {
		return nil
	}
	ids := make([]string, 0, len(p.VirtualMedia)+len(p.VirtualMediaReadOnly))
	ids = append(ids, p.VirtualMedia...)
	ids = append(ids, p.VirtualMediaReadOnly...)
	return ids
}

// managerIDs lists Managers collection members.
func (p Personality) managerIDs() []string {
	if len(p.ManagerIDs) > 0 {
		return p.ManagerIDs
	}
	return []string{p.ManagerID}
}

// firmwareMember returns the inventory description for id.
func (p Personality) firmwareMember(id string) (FirmwareMember, bool) {
	for _, member := range p.Firmware {
		if member.ID == id {
			return member, true
		}
	}
	return FirmwareMember{}, false
}

// hostResetFirmwareIDs lists inventory members a ComputerSystem.Reset activates.
func (p Personality) hostResetFirmwareIDs() []string {
	var ids []string
	for _, member := range p.Firmware {
		if member.HostReset {
			ids = append(ids, member.ID)
		}
	}
	return ids
}

// chassisIDs lists the chassis members this personality serves, without duplicates.
func (p Personality) chassisIDs() []string {
	seen := map[string]struct{}{}
	var ids []string
	add := func(id string) {
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	add(p.ChassisID)
	add(p.BMCChassisID)
	add(p.ERoTChassisID)
	for _, nic := range p.NICs {
		add(nic.ChassisID)
	}
	for _, id := range p.ExtraChassis {
		add(id)
	}
	return ids
}

// hasChassis reports whether id is a chassis member.
func (p Personality) hasChassis(id string) bool {
	for _, chassisID := range p.chassisIDs() {
		if chassisID == id {
			return true
		}
	}
	return false
}

// nicSlot returns the ConnectX whose firmware inventory member is firmwareID.
func (p Personality) nicSlot(firmwareID string) (NICSlot, bool) {
	for _, nic := range p.NICs {
		if nic.FirmwareID == firmwareID {
			return nic, true
		}
	}
	return NICSlot{}, false
}
