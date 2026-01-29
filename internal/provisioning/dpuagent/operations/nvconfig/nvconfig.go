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

package nvconfig

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

const (
	CondNVConfigApplied   = "NVConfigApplied"
	defaultMstDevicesPath = "/dev/mst"
)

type GetLatestDPU struct {
}

func (g *GetLatestDPU) Name() string {
	return "Get Latest DPU"
}

func (g *GetLatestDPU) ConditionType() string {
	return "DPURetrieved"
}

func (g *GetLatestDPU) ShouldSkip(ctx *operations.Context) bool {
	return false
}

func (g *GetLatestDPU) ShouldUpdateStatusBeforeContinue(ctx *operations.Context) bool {
	return false
}

func (g *GetLatestDPU) Execute(execCtx context.Context, optCtx *operations.Context) error {
	dpu := &provisioningv1.DPU{}
	if err := optCtx.Client.GetObject(execCtx, optCtx.Options.DPUNamespace, optCtx.Options.DPUName, dpu); err != nil {
		return err
	}
	optCtx.LatestDPU = dpu
	return nil
}

type ConfigureNVConfig struct {
	mstDevicesPath string
	runBash        func(cmd string) (bytes.Buffer, bytes.Buffer, error)
}

func (n *ConfigureNVConfig) Name() string {
	return "NVConfig"
}

func (n *ConfigureNVConfig) ConditionType() string {
	return CondNVConfigApplied
}

func (n *ConfigureNVConfig) ShouldSkip(ctx *operations.Context) bool {
	if len(ctx.DPUFlavor.Spec.NVConfig) == 0 {
		klog.Infof("NVConfig not specified in DPUFlavor, skip")
		return true
	}
	if ctx.LatestDPU == nil {
		klog.Error("Latest DPU not retrieved, will return error during execution. (this should never happen)")
		return false
	}
	if ctx.LatestDPU.Status.DPUInternalStatus == nil {
		return false
	}
	cond := meta.FindStatusCondition(ctx.LatestDPU.Status.DPUInternalStatus.Conditions, CondNVConfigApplied)
	if cond != nil && cond.Status == metav1.ConditionTrue {
		klog.Infof("NVConfig already configured, skip")
		return true
	}
	return false
}

func (n *ConfigureNVConfig) ShouldUpdateStatusBeforeContinue(ctx *operations.Context) bool {
	// Must update status before continuing, because the condition is used to check if the NVConfig has been configured.
	return true
}

func (n *ConfigureNVConfig) Execute(execCtx context.Context, optCtx *operations.Context) error {
	if optCtx.LatestDPU == nil {
		klog.Error("Latest DPU not retrieved. This should never happen.")
		return fmt.Errorf("latest DPU not retrieved")
	}

	if n.mstDevicesPath == "" {
		n.mstDevicesPath = defaultMstDevicesPath
	}
	// List all MST devices
	devices, err := filepath.Glob(filepath.Join(n.mstDevicesPath, "*"))
	if err != nil {
		return fmt.Errorf("failed to list MST devices: %w", err)
	}
	if len(devices) == 0 {
		return fmt.Errorf("no MST devices found in %s", n.mstDevicesPath)
	}

	if n.runBash == nil {
		n.runBash = bash.Run
	}
	// Reset NVConfig on all devices to defaults
	for _, dev := range devices {
		klog.Infof("Resetting NVConfig on device %s to defaults", dev)
		cmdStr := fmt.Sprintf("mlxconfig -d %s -y reset", dev)
		if _, stderr, err := n.runBash(cmdStr); err != nil {
			return fmt.Errorf("failed to reset NVConfig on device %s: %w, stderr: %s", dev, err, stderr.String())
		}
	}

	// Apply device-specific NVConfig configurations
	for _, nvconfig := range optCtx.DPUFlavor.Spec.NVConfig {
		params := strings.Join(nvconfig.Parameters, " ")
		device := "*"
		if nvconfig.Device != nil {
			device = *nvconfig.Device
		}

		if device == "*" || device == "" {
			// Apply to all devices
			for _, dev := range devices {
				klog.Infof("Setting NVConfig on device %s: %s", dev, params)
				cmdStr := fmt.Sprintf("mlxconfig -d %s -y set %s", dev, params)
				if _, stderr, err := n.runBash(cmdStr); err != nil {
					return fmt.Errorf("failed to set NVConfig on device %s: %w, stderr: %s", dev, err, stderr.String())
				}
			}
		} else {
			klog.Infof("Setting NVConfig on device %s: %s", device, params)
			cmdStr := fmt.Sprintf("mlxconfig -d %s -y set %s", device, params)
			if _, stderr, err := n.runBash(cmdStr); err != nil {
				return fmt.Errorf("failed to set NVConfig on device %s: %w, stderr: %s", device, err, stderr.String())
			}
		}
	}
	return nil
}
