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

// Package nvconfig holds NVConfig parameter parsing shared by the provisioning controllers and
// the DPU agent. Both must read DELAY_HOST_OS_INIT identically: the controller rejects the hold
// early in the Pending phase, the agent guards it again before writing it with mlxconfig.
package nvconfig

import (
	"strconv"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
)

// DelayHostOSInitParam is the NVConfig parameter that holds the host at UEFI until the DPU
// releases it.
const DelayHostOSInitParam = "DELAY_HOST_OS_INIT"

// delayHostOSInitUserMode is the DELAY_HOST_OS_INIT value that holds the host until the DPU
// releases it.
const delayHostOSInitUserMode = 3

// FlavorRequestsHostOSInitHold reports whether any NVConfig entry in a DPUFlavor holds the host
// at UEFI.
func FlavorRequestsHostOSInitHold(nvconfigs []provisioningv1.NVConfig) bool {
	for _, nc := range nvconfigs {
		if ParamsRequestHostOSInitHold(nc.Parameters) {
			return true
		}
	}
	return false
}

// ParamsRequestHostOSInitHold reports whether a single NVConfig entry's parameters hold the host
// at UEFI.
func ParamsRequestHostOSInitHold(params []string) bool {
	for _, param := range params {
		name, value, ok := strings.Cut(param, "=")
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(name), DelayHostOSInitParam) && isDelayHostOSInitUserMode(value) {
			return true
		}
	}
	return false
}

// isDelayHostOSInitUserMode reports whether the flavor value holds the host at UEFI. It compares
// numerically because mlxconfig gets the value verbatim.
// Values it cannot interpret are not holds: mlxconfig rejects them at the write.
func isDelayHostOSInitUserMode(rhs string) bool {
	value := strings.ToUpper(strings.TrimSpace(rhs))
	if value == "ENABLE_USER" {
		return true
	}
	// Base 0 accepts the 0x, 0o and bare-decimal forms mlxconfig takes.
	parsed, err := strconv.ParseUint(value, 0, 32)
	return err == nil && parsed == delayHostOSInitUserMode
}
