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
	"encoding/json"
	"fmt"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
)

func GetDPUMode(ctx context.Context, pciAddress string) (provisioningv1.DpuModeType, error) {
	return getDPUMode(pciAddress, RunBash)
}

func getDPUMode(pciAddress string, runBash func(string) (bytes.Buffer, bytes.Buffer, error)) (provisioningv1.DpuModeType, error) {
	// dmsc reports /nvidia/mode/state/mode as DPU on a DPU running in NIC mode, so the mode
	// is read through dms-cli, which reports it correctly. The dms-cli target needs both the
	// PCI domain and the function.
	// See: #5301165
	cmd := fmt.Sprintf("/opt/mellanox/doca/services/dms/dms-cli --target pci/%s --json /nvidia/mode/operating-mode", pciAddress)
	stdout, stderr, err := runBash(cmd)
	if err != nil {
		return "", fmt.Errorf("failed to run cmd: %s, err: %w, stdout: %s, stderr: %s", cmd, err, stdout.String(), stderr.String())
	}

	// dms-cli --json returns the leaf as a single-key object:
	// { "operating-mode": "dpu" }
	var resp struct {
		OperatingMode string `json:"operating-mode"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		return "", fmt.Errorf("failed to parse DPU mode from: %s, err: %w", stdout.String(), err)
	}
	if resp.OperatingMode == "" {
		return "", fmt.Errorf("failed to parse DPU mode from: %s", stdout.String())
	}

	switch strings.ToLower(resp.OperatingMode) {
	case string(provisioningv1.DpuMode):
		return provisioningv1.DpuMode, nil
	case string(provisioningv1.NicMode):
		return provisioningv1.NicMode, nil
	default:
		return "", fmt.Errorf("unsupported DPU mode %q", resp.OperatingMode)
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
