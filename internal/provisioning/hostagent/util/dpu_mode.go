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

package util

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
)

func GetDPUMode(ctx context.Context, pciAddress string) (provisioningv1.DpuModeType, error) {
	return getDPUMode(pciAddress, RunBash)
}

func getDPUMode(pciAddress string, runBash func(string) (bytes.Buffer, bytes.Buffer, error)) (provisioningv1.DpuModeType, error) {
	cmd := fmt.Sprintf("/opt/mellanox/doca/services/dms/dmsc --insecure --address 127.0.0.1:9339 --target %s get --path /nvidia/mode/state/mode", pciAddress)
	stdout, stderr, err := runBash(cmd)
	if err != nil {
		return "", fmt.Errorf("failed to run cmd: %s, err: %w, stdout: %s, stderr: %s", cmd, err, stdout.String(), stderr.String())
	}

	// dmsc outputs the mode in a pretty weird format:
	//[
	//	{
	//	  "source": "127.0.0.1:9339",
	//	  "timestamp": 1761796906478936518,
	//	  "time": "2025-10-30T04:01:46.478936518Z",
	//	  "target": "c9:00.0",
	//	  "updates": [
	//		{
	//		  "Path": "nvidia/mode/state/mode",
	//		  "values": {
	//			"nvidia/mode/state/mode": "DPU"
	//		  }
	//		}
	//	  ]
	//	}
	//]

	pattern := `"nvidia/mode/state/mode"\s*:\s*"([^"]+)"`
	re := regexp.MustCompile(pattern)
	matches := re.FindStringSubmatch(stdout.String())
	if len(matches) <= 1 {
		return "", fmt.Errorf("failed to parse DPU mode from: %s", stdout.String())
	}

	switch strings.ToLower(matches[1]) {
	case string(provisioningv1.DpuMode):
		return provisioningv1.DpuMode, nil
	case string(provisioningv1.NicMode):
		return provisioningv1.NicMode, nil
	default:
		return "", fmt.Errorf("unsupported DPU mode %q", matches[1])
	}
}

func SetDPUMode(pciAddress string) error {
	// DMS will use the PCI address without the "0000:" prefix to determine if the device is BlueField3.
	pciAddress = strings.TrimPrefix(pciAddress, "0000:")
	cmd := fmt.Sprintf("/opt/mellanox/doca/services/dms/dmsc --insecure --address 127.0.0.1:9339 --target %s set --update /nvidia/mode/config/mode:::string:::DPU", pciAddress)
	if stdout, stderr, err := RunBash(cmd); err != nil {
		return fmt.Errorf("failed to run cmd: %s, err: %w, stdout: %s, stderr: %s", cmd, err, stdout.String(), stderr.String())
	}
	return nil
}
