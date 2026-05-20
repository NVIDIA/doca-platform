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

package dpumode

import (
	"context"
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	dpuagentutil "github.com/nvidia/doca-platform/internal/provisioning/dpuagent/util"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"

	"k8s.io/klog/v2"
)

const (
	defaultMstDevicesPath = "/dev/mst"
)

type EnsureMode struct {
	mstDevicesPath string
	runBash        bash.RunFunc
}

func (d *EnsureMode) Name() string {
	return "Ensure DPU Mode"
}

func (d *EnsureMode) ConditionType() string {
	return "DpuModeEnsured"
}

func (d *EnsureMode) ShouldSkip(ctx *operations.Context) bool {
	return false
}

func (d *EnsureMode) ShouldUpdateStatusBeforeContinue(ctx *operations.Context) bool {
	return false
}

func (d *EnsureMode) Execute(execCtx context.Context, optCtx *operations.Context) error {
	devices, err := d.targetMFTDevices(optCtx)
	if err != nil {
		return fmt.Errorf("failed to discover MFT target devices: %w", err)
	}

	if len(devices) == 0 {
		klog.Warningf("No MFT target devices found")
		return nil
	}

	if optCtx.LatestDPU == nil {
		return fmt.Errorf("latest DPU is required to resolve deployment mode")
	}
	deploymentMode := optCtx.LatestDPU.Status.DeploymentMode
	if deploymentMode == "" {
		return fmt.Errorf("dpu.status.deploymentMode is empty")
	}

	for _, dev := range devices {
		if err := d.setDeploymentMode(dev, deploymentMode); err != nil {
			return fmt.Errorf("failed to set DPU mode for device %s: %w", dev, err)
		}
	}
	return nil
}

func (d *EnsureMode) targetMFTDevices(optCtx *operations.Context) ([]string, error) {
	if d.mstDevicesPath == "" {
		d.mstDevicesPath = defaultMstDevicesPath
	}
	return dpuagentutil.MFTDevicesForNSNIC(d.mstDevicesPath, optCtx.NSNIC, d.runBash)
}

func (d *EnsureMode) setDeploymentMode(dev string, deploymentMode provisioningv1.DeploymentMode) error {
	var cmd string
	switch deploymentMode {
	case provisioningv1.DeploymentModeZeroTrust:
		klog.Infof("Setting DPU to zero-trust mode for device %s", dev)
		cmd = fmt.Sprintf("mlxprivhost -d %s r --disable_rshim --disable_tracer --disable_counter_rd --disable_port_owner", dev)
	case provisioningv1.DeploymentModeHostTrusted:
		klog.Infof("Setting DPU to DPU mode for device %s", dev)
		cmd = fmt.Sprintf("mlxprivhost -d %s p", dev)
	default:
		return fmt.Errorf("invalid deployment mode: %s", deploymentMode)
	}

	if d.runBash == nil {
		d.runBash = bash.Run
	}
	stdout, stderr, err := d.runBash(cmd)
	if err != nil {
		return fmt.Errorf("failed to run mlxprivhost: stdout=%s, stderr=%s, err=%w", stdout.String(), stderr.String(), err)
	}

	klog.Infof("Successfully set DPU mode for device %s", dev)
	return nil
}
