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

	"k8s.io/klog/v2"
)

const (
	defaultRootFS = "/"
	// defaultDevice is the PCI address of device for BF3.
	// Note:
	// For BF3, the address is always 0000:03:00.0.
	// For BF4, we have no clear answer yet.
	defaultDevice = "0000:03:00.0"
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

	pfTotalSF := getPFTotalSFFromFlavor(&optCtx.DPUFlavor)
	trustedSF := getTrustedSFFromFlavor(&optCtx.DPUFlavor)

	// Create SFs on P0 for SFC
	// System SF(index 0) has been removed, so DPF will create SF from index 0
	for i := 0; i < pfTotalSF-trustedSF; i++ {
		// Create SFs with random mac, kernel will allocate random MAC for SF netdev
		cmd := fmt.Sprintf("/sbin/mlnx-sf --action create --device %s --sfnum %d", defaultDevice, i)
		stdout, stderr, err := s.runBash(cmd)
		if err != nil {
			// Continue on error (like "|| true" in bash)
			klog.Warningf("Failed to create SF %d: stdout=%s, stderr=%s, err=%v", i, stdout.String(), stderr.String(), err)
		}
	}

	// Create trusted SFs starting from index 101
	for i := 101; i <= 100+trustedSF; i++ {
		cmd := fmt.Sprintf("/sbin/mlnx-sf --action create --device %s --sfnum %d -t", defaultDevice, i)
		stdout, stderr, err := s.runBash(cmd)
		if err != nil {
			// Continue on error (like "|| true" in bash)
			klog.Warningf("Failed to create trusted SF %d: stdout=%s, stderr=%s, err=%v", i, stdout.String(), stderr.String(), err)
		}
	}

	// Set GUID for SF
	if err := s.setGUIDForSF(); err != nil {
		return fmt.Errorf("failed to set GUID for SF: %w", err)
	}
	return nil
}

// SFInfo represents the JSON structure returned by mlnx-sf -a show -j
type SFInfo struct {
	SFNetdev string `json:"sf_netdev"`
	AuxDev   string `json:"aux_dev"`
}

func (s *CreateSF) setGUIDForSF() error {
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
