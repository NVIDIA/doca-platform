/*
Copyright 2024 NVIDIA

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

package dpu

import "fmt"

type DPU struct {
	Index               int
	DeviceID            string
	PCIAddress          string
	PSID                string
	PF0Name             string
	OOBBridgeConfigured bool
}

func (dpu DPU) String() string {
	return fmt.Sprintf("DPU(index: %d, device id: %s, PS id: %s, PCI address: %s, PF0 name: %s, OOB bridge configured: %t)",
		dpu.Index, dpu.DeviceID, dpu.PSID, dpu.PCIAddress, dpu.PF0Name, dpu.OOBBridgeConfigured)
}
