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

package reboot

import (
	"fmt"
	"strconv"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
)

const (
	PowercycleCmdKey         = "provisioning.dpu.nvidia.com/powercycle-command"
	HostPowerCycleRequireKey = "provisioning.dpu.nvidia.com/host-power-cycle-required"
	Cycle                    = "cycle"
	Reset                    = "reset"
)

func PowerCycleRequired(annotations map[string]string) bool {
	if annotations != nil {
		if v, ok := annotations[HostPowerCycleRequireKey]; ok {
			if b, err := strconv.ParseBool(v); err == nil {
				return b
			}
		}
	}

	return false
}

func ValidateHostPowerCycleRequire(m map[string]string) error {
	v, ok := m[HostPowerCycleRequireKey]
	if !ok {
		return nil
	}
	if _, err := strconv.ParseBool(v); err != nil {
		return fmt.Errorf("invalid value %q for %q", v, HostPowerCycleRequireKey)
	}

	return nil
}

func PowerCycleCommand(dpuNode *provisioningv1.DPUNode) (string, error) {
	ipmiCmd := []string{"ipmitool", "chassis", "power"}
	cmd, ok := dpuNode.Annotations[PowercycleCmdKey]
	if !ok {
		return strings.Join(append(ipmiCmd, Cycle), " "), nil
	}
	switch cmd {
	case Cycle, Reset:
		return strings.Join(append(ipmiCmd, cmd), " "), nil
	default:
		return "", fmt.Errorf("invalid value %q, supported values: %q", cmd, []string{Cycle, Reset})
	}
}
