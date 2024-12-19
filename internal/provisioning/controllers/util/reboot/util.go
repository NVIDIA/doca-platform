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
)

const (
	PowercycleCmdKey         = "provisioning.dpu.nvidia.com/powercycle-command"
	RebootCmdKey             = "provisioning.dpu.nvidia.com/reboot-command"
	HostPowerCycleRequireKey = "provisioning.dpu.nvidia.com/host-power-cycle-required"
	Cycle                    = "cycle"
	Reset                    = "reset"
	// Skip ensures no power cycle is done on the host.
	// Note: This may cause issues with the BFB firmware installation and configuration.
	Skip = "skip"
)

type RebootType string

const (
	PowerCycle RebootType = "power-cycle"
	WarmReboot RebootType = "warm-reboot"
)

func getHostRebootCmd(annotations map[string]string, key string) (string, error) {
	cmd, ok := annotations[key]
	if !ok {
		if PowercycleCmdKey == key {
			return Cycle, nil
		} else if RebootCmdKey == key {
			return Reset, nil
		}
	}
	supported := []string{Cycle, Reset, Skip}
	for _, s := range supported {
		if cmd == s {
			return cmd, nil
		}
	}
	return "", fmt.Errorf("invalid value %q, supported values: %q", cmd, supported)
}

func powerCycleRequired(annotations map[string]string) bool {
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

func GenerateCmd(nodeAnnotations map[string]string, dpuAnnotations map[string]string) (generateCmd string, rebootType RebootType, err error) {
	var cmd string
	if powerCycleRequired(dpuAnnotations) {
		rebootType = PowerCycle
		if cmd, err = getHostRebootCmd(nodeAnnotations, PowercycleCmdKey); err != nil {
			return generateCmd, rebootType, err
		}
	} else {
		rebootType = WarmReboot
		if cmd, err = getHostRebootCmd(nodeAnnotations, RebootCmdKey); err != nil {
			return generateCmd, rebootType, err
		}
	}

	if rebootType == PowerCycle {
		impicmd := []string{"ipmitool", "chassis", "power"}

		switch cmd {
		case Cycle:
			impicmd = append(impicmd, Cycle)
		case Reset:
			impicmd = append(impicmd, Reset)
		case Skip:
			impicmd = []string{Skip}
		}
		generateCmd = strings.Join(impicmd, " ")
	} else {
		generateCmd = cmd
	}

	return generateCmd, rebootType, nil
}
