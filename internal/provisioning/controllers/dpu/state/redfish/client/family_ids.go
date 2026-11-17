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

package client

// IsBF4 reports whether this client is talking to a BlueField-4 BMC.
func (c *Client) IsBF4() bool {
	return c != nil && c.family.isBF4()
}

// IsCMX reports whether this client is talking to a CMX BMC.
func (c *Client) IsCMX() bool {
	return c != nil && c.family == familyCMX
}

// bmcFirmwareID is the firmware inventory id for the BMC image on this client's platform.
func (c *Client) bmcFirmwareID() string {
	switch {
	case c.IsCMX():
		return CMXFirmwareBMC
	case c.IsBF4():
		return "BlueField_FW_BMC_0"
	default:
		return "BMC_Firmware"
	}
}

// nicFirmwareID is the firmware inventory id for the NIC image on this client's platform.
func (c *Client) nicFirmwareID() string {
	switch {
	case c.IsCMX():
		return CMXFirmwareCX0
	case c.IsBF4():
		return "BlueField_FW_NIC_0"
	default:
		return "DPU_NIC"
	}
}

// uefiFirmwareID is the firmware inventory id for the UEFI or SBIOS image on this client's platform.
func (c *Client) uefiFirmwareID() string {
	switch {
	case c.IsCMX():
		return CMXFirmwareSBIOS
	case c.IsBF4():
		return "BlueField_FW_CPU_0"
	default:
		return "DPU_UEFI"
	}
}

// identityChassisID is the chassis id that carries the DPU or node identity.
func (c *Client) identityChassisID() string {
	switch {
	case c.IsCMX():
		return CMXIdentityChassis
	case c.IsBF4():
		return "BlueField_0"
	default:
		return "Card1"
	}
}

// erotChassisID is the chassis id of the BMC ERoT.
func (c *Client) erotChassisID() string {
	if c.IsCMX() {
		return CMXERoTChassisID
	}
	return "BlueField_ERoT_BMC_0"
}
