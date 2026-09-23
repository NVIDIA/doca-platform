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
	rfclient "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
	"github.com/nvidia/doca-platform/test/mock/dpuagent/pkg/config"
)

// ManagerID is the BMC manager resource name on both DPU generations; the controller picks the
// member whose @odata.id contains "bmc".
const ManagerID = "Bluefield_BMC"

// Personality holds the per-generation constants the controller uses to recognize the DPU type
// and to address its resources.
type Personality struct {
	Type config.DPUType
	// Product is returned by GET /redfish/v1; the controller treats anything containing "B4" or
	// "BLUEFIELD-4" as BlueField-4.
	Product      string
	User         string
	SystemID     string
	ChassisID    string
	ChassisModel string
	PartNumber   string
	// PowerOffState is the ComputerSystem.PowerState of a shut down Arm.
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
	// NetworkAdapterPath is the Chassis-relative path of the NIC network adapter.
	NetworkAdapterPath string
	PF0ID              string
}

// ForType returns the personality for a DPU generation.
func ForType(t config.DPUType) Personality {
	if t == config.DPUTypeBF4 {
		return Personality{
			Type:               config.DPUTypeBF4,
			Product:            "BLUEFIELD-4",
			User:               rfclient.BF4BMCUser,
			SystemID:           "BlueField_0",
			ChassisID:          "BlueField_0",
			ChassisModel:       "BlueField-4",
			PartNumber:         "900-9D3B4-00SV-EA0",
			PowerOffState:      "Paused",
			InvBMC:             "BlueField_FW_BMC_0",
			InvERoT:            "BlueField_FW_ERoT_BMC_0",
			InvUEFI:            "BlueField_FW_CPU_0",
			InvNIC:             "BlueField_FW_NIC_0",
			InvOSImage:         "BlueField_OS_Image_CPU_0",
			InvOSConfig:        "BlueField_OS_Config_CPU_0",
			InvPendingBundle:   "Pending_Bundle",
			NetworkAdapterPath: "/redfish/v1/Chassis/BlueField_0/NetworkAdapters/BlueField_NIC_0",
			PF0ID:              "0",
		}
	}
	return Personality{
		Type:               config.DPUTypeBF3,
		Product:            "BlueField",
		User:               rfclient.BF3BMCUser,
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
		NetworkAdapterPath: "/redfish/v1/Chassis/Card1/NetworkAdapters/NvidiaNetworkAdapter",
		PF0ID:              "eth0f0",
	}
}

// IsBF4 reports whether this is the BlueField-4 personality.
func (p Personality) IsBF4() bool {
	return p.Type == config.DPUTypeBF4
}
