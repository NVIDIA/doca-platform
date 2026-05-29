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

package sfconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"
	pciutil "github.com/nvidia/doca-platform/internal/provisioning/utils/pci"

	"k8s.io/klog/v2"
)

const (
	defaultRootFS = "/"
	MaxTrustedSfs = 10
)

type CreateSF struct {
	// sysClassNet string
	rootFS  string
	runBash func(cmd string) (bytes.Buffer, bytes.Buffer, error)
}

func (s *CreateSF) Name() string {
	return "Configure SF"
}

func (s *CreateSF) ConditionType() string {
	return "SFCreated"
}

func (s *CreateSF) ShouldSkip(ctx *operations.Context) bool {
	return ctx.Options.SkipSFConfig
}

func (s *CreateSF) ShouldUpdateStatusBeforeContinue(ctx *operations.Context) bool {
	return false
}

func (s *CreateSF) Execute(execCtx context.Context, optCtx *operations.Context) error {
	if s.runBash == nil {
		s.runBash = bash.Run
	}
	devices, err := s.targetDevices(optCtx)
	if err != nil {
		return err
	}

	pfTotalSF := getPFTotalSFFromFlavor(&optCtx.DPUFlavor)
	trustedSF := getTrustedSFFromFlavor(&optCtx.DPUFlavor)

	for _, device := range devices {
		if err := s.configureSFsOnDevice(device, pfTotalSF, trustedSF); err != nil {
			return fmt.Errorf("failed to configure SFs on device %s: %w", device, err)
		}
	}
	return nil
}

func (s *CreateSF) configureSFsOnDevice(device string, pfTotalSF, trustedSF int) error {
	createErrBySF := map[int]error{}
	expectedSFNums := make([]int, 0, pfTotalSF)

	// System SF(index 0) has been removed, so DPF will create SF from index 0
	for i := 0; i < pfTotalSF-trustedSF; i++ {
		expectedSFNums = append(expectedSFNums, i)
		// Create SFs with random mac, kernel will allocate random MAC for SF netdev
		cmd := fmt.Sprintf("/sbin/mlnx-sf --action create --device %s --sfnum %d", device, i)
		stdout, stderr, err := s.runBash(cmd)
		if err != nil {
			// Continue on error (like "|| true" in bash)
			klog.Warningf("Failed to create SF %d on device %s: stdout=%s, stderr=%s, err=%v", i, device, stdout.String(), stderr.String(), err)
			createErrBySF[i] = fmt.Errorf("failed to create SF %d on device %s: stdout=%s, stderr=%s, err=%w", i, device, stdout.String(), stderr.String(), err)
		}
	}

	// Create trusted SFs starting from index 101
	for i := 101; i <= 100+trustedSF; i++ {
		expectedSFNums = append(expectedSFNums, i)
		cmd := fmt.Sprintf("/sbin/mlnx-sf --action create --device %s --sfnum %d -t", device, i)
		stdout, stderr, err := s.runBash(cmd)
		if err != nil {
			// Continue on error (like "|| true" in bash)
			klog.Warningf("Failed to create trusted SF %d on device %s: stdout=%s, stderr=%s, err=%v", i, device, stdout.String(), stderr.String(), err)
			createErrBySF[i] = fmt.Errorf("failed to create trusted SF %d on device %s: stdout=%s, stderr=%s, err=%w", i, device, stdout.String(), stderr.String(), err)
		}
	}
	if err := s.verifyExpectedSFs(device, expectedSFNums, createErrBySF); err != nil {
		return err
	}

	// Set GUID for SF
	if err := s.setGUIDForSF(device); err != nil {
		return fmt.Errorf("failed to set GUID for SF: %w", err)
	}
	return nil
}

func (s *CreateSF) targetDevices(ctx *operations.Context) ([]string, error) {
	ports, err := ctx.NSPorts()
	if err != nil {
		return nil, err
	}

	devices := []string{}
	if isBlueField4(ctx) {
		for _, p := range ports {
			devices = append(devices, p.PCIAddress)
		}
	} else {
		// SFs are created on p0 for non-BF4 BlueField generations.
		for _, p := range ports {
			if p.Netdev == "p0" {
				devices = append(devices, p.PCIAddress)
				break
			}
		}
	}
	if len(devices) == 0 {
		return nil, fmt.Errorf("target physical port not found")
	}
	return devices, nil
}

func isBlueField4(ctx *operations.Context) bool {
	return ctx.LatestDPU != nil && ctx.LatestDPU.Status.DPUType == provisioningv1.DPUTypeBlueField4
}

// SFInfo represents fields parsed from mlnx-sf -a show -j.
// Example output:
//
//	{
//	    "pci/0000:03:00.0/229376": {
//	        "type": "eth",
//	        "netdev": "en3f0pf0sf0",
//	        "flavour": "pcisf",
//	        "controller": 0,
//	        "pfnum": 0,
//	        "sfnum": 0,
//	        "splittable": false,
//	        "function": {
//	            "hw_addr": "42:f9:a8:cf:b6:1e",
//	            "state": "active",
//	            "opstate": "attached",
//	            "roce": "enable",
//	            "trust": "off",
//	            "max_uc_macs": 128,
//	            "max_io_eqs": 8,
//	            "eswitch": "NA"
//	        },
//	        "device": "0000:03:00.0",
//	        "sfindex": "pci/0000:03:00.0/229376",
//	        "aux_dev": "mlx5_core.sf.2",
//	        "sf_netdev": "enp3s0f0s0",
//	        "rdma_dev": "mlx5_0"
//	    }
//	}
//
//nolint:misspell // Keep original field names from mlnx-sf output example.
type SFInfo struct {
	SFNetdev string `json:"sf_netdev"`
	AuxDev   string `json:"aux_dev"`
	Device   string `json:"device"`
	SFNum    int    `json:"sfnum"`
}

func (s *CreateSF) verifyExpectedSFs(device string, expectedSFNums []int, createErrBySF map[int]error) error {
	stdout, stderr, err := s.runBash("mlnx-sf -a show -j")
	if err != nil {
		return fmt.Errorf("failed to run mlnx-sf for SF verification: stdout=%s, stderr=%s, err=%w", stdout.String(), stderr.String(), err)
	}

	var sfMap map[string]SFInfo
	if err := json.Unmarshal(stdout.Bytes(), &sfMap); err != nil {
		return fmt.Errorf("failed to parse mlnx-sf output during SF verification: %w", err)
	}

	existingSF := map[int]struct{}{}
	for _, info := range sfMap {
		if pciutil.NormalizeAddress(info.Device) == pciutil.NormalizeAddress(device) {
			existingSF[info.SFNum] = struct{}{}
		}
	}

	for _, sfnum := range expectedSFNums {
		if _, found := existingSF[sfnum]; found {
			continue
		}
		if createErr, hasCreateErr := createErrBySF[sfnum]; hasCreateErr {
			return createErr
		}
		return fmt.Errorf("sf %d was not found on device %s after creation", sfnum, device)
	}

	return nil
}

func (s *CreateSF) setGUIDForSF(device string) error {
	// Get JSON output from mlnx-sf
	stdout, stderr, err := s.runBash("mlnx-sf -a show -j")
	if err != nil {
		return fmt.Errorf("failed to run mlnx-sf: stdout=%s, stderr=%s, err=%w", stdout.String(), stderr.String(), err)
	}

	// Parse JSON
	var sfMap map[string]SFInfo
	if err := json.Unmarshal(stdout.Bytes(), &sfMap); err != nil {
		return fmt.Errorf("failed to parse mlnx-sf output: %w", err)
	}

	if s.rootFS == "" {
		s.rootFS = defaultRootFS
	}
	// Iterate over each SF
	for key, info := range sfMap {
		if pciutil.NormalizeAddress(info.Device) != pciutil.NormalizeAddress(device) {
			continue
		}
		// Read the MAC address from the system file
		macPath := filepath.Join(s.rootFS, "sys/class/net", info.SFNetdev, "address")
		macBytes, err := os.ReadFile(macPath)
		if err != nil {
			klog.Warningf("Failed to read MAC address for %s: %v", info.SFNetdev, err)
			continue
		}
		macAddress := strings.TrimSpace(string(macBytes))

		// Update the MAC address using mlxdevm
		cmd := fmt.Sprintf("/opt/mellanox/iproute2/sbin/mlxdevm port function set %s hw_addr %s", key, macAddress)
		stdout, stderr, err := s.runBash(cmd)
		if err != nil {
			klog.Warningf("Failed to set hw_addr for %s: stdout=%s, stderr=%s, err=%v", key, stdout.String(), stderr.String(), err)
			continue
		}

		// Unbind the auxiliary device
		unbindPath := filepath.Join(s.rootFS, "sys/bus/auxiliary/devices", info.AuxDev, "driver/unbind")
		if err := os.WriteFile(unbindPath, []byte(info.AuxDev), 0644); err != nil {
			return fmt.Errorf("failed to unbind aux device %s: %w", info.AuxDev, err)
		}

		// Bind the auxiliary device
		bindPath := filepath.Join(s.rootFS, "sys/bus/auxiliary/drivers/mlx5_core.sf/bind")
		if err := os.WriteFile(bindPath, []byte(info.AuxDev), 0644); err != nil {
			return fmt.Errorf("failed to bind aux device %s: %w", info.AuxDev, err)
		}

		klog.Infof("Successfully set GUID for SF %s", key)
	}
	return nil
}

func getPFTotalSFFromFlavor(flavor *provisioningv1.DPUFlavor) int {
	regex := regexp.MustCompile(`^PF_TOTAL_SF=([0-9]+)`)
	for _, nvconfig := range flavor.Spec.NVConfig {
		for _, parmeter := range nvconfig.Parameters {
			matches := regex.FindStringSubmatch(parmeter)
			if len(matches) == 2 {
				if num, err := strconv.Atoi(matches[1]); err == nil {
					return num
				}
			}
		}
	}
	return 0
}

func getTrustedSFFromFlavor(flavor *provisioningv1.DPUFlavor) int {
	if flavor.Annotations != nil {
		trustedSFCountFromAnnotation, found := flavor.Annotations[cutil.TrustedSFCount]
		if found {
			trustedSFCount, err := strconv.Atoi(trustedSFCountFromAnnotation)
			if err == nil && trustedSFCount > 0 && trustedSFCount <= MaxTrustedSfs {
				return trustedSFCount
			}

		}
	}
	return 0
}
