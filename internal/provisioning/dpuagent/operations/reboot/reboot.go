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

package reboot

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"

	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
)

const (
	shutdownDelayInSeconds = 5
	bootIDFile             = "/proc/sys/kernel/random/boot_id"
)

type CheckHostRebootRequired struct {
}

func (c *CheckHostRebootRequired) Name() string {
	return "Check Reboot Required"
}

func (c *CheckHostRebootRequired) ConditionType() string {
	return "CheckHostRebootRequired"
}

func (c *CheckHostRebootRequired) ShouldSkip(ctx *operations.Context) bool {
	return false
}

func (c *CheckHostRebootRequired) ShouldUpdateStatusBeforeContinue(ctx *operations.Context) bool {
	// Must update status before continuing, because the initialBootID is used to check if the host reboot is required.
	return true
}

func (c *CheckHostRebootRequired) Execute(execCtx context.Context, optCtx *operations.Context) error {
	currentRebootID, err := getCurrentRebootID()
	if err != nil {
		return fmt.Errorf("failed to read boot ID file: %w", err)
	}
	if hasBeenBooted(optCtx.LatestDPU, currentRebootID) {
		klog.Infof("Host has already been booted, skip checking for host reboot")
		return nil
	}
	optCtx.Status.HostRebootRequired = ptr.To(true)
	optCtx.Status.InitialBootID = ptr.To(currentRebootID)
	return nil
}

type ShutDownArm struct {
	runBash func(cmd string) (bytes.Buffer, bytes.Buffer, error)
}

func (s *ShutDownArm) Name() string {
	return "Shut Down ARM"
}

func (s *ShutDownArm) ConditionType() string {
	return "ShutDownArm"
}

func (s *ShutDownArm) ShouldSkip(ctx *operations.Context) bool {
	return false
}

func (s *ShutDownArm) ShouldUpdateStatusBeforeContinue(ctx *operations.Context) bool {
	return false
}

func (s *ShutDownArm) Execute(execCtx context.Context, optCtx *operations.Context) error {
	currentRebootID, err := getCurrentRebootID()
	if err != nil {
		return fmt.Errorf("failed to read boot ID file: %w", err)
	}
	if hasBeenBooted(optCtx.LatestDPU, currentRebootID) {
		klog.Infof("Host has already been booted, skip shutting down ARM")
		return nil
	}

	if s.runBash == nil {
		s.runBash = bash.Run
	}
	klog.Infof("Shutting down in %d seconds", shutdownDelayInSeconds)
	cmd := fmt.Sprintf("sleep %d && shutdown -h now", shutdownDelayInSeconds)
	_, stderr, err := s.runBash(cmd)
	if err != nil {
		return fmt.Errorf("failed to shut down host: %w, stderr: %s", err, stderr.String())
	}
	return nil
}

func getCurrentRebootID() (string, error) {
	currentRebootID, err := os.ReadFile(bootIDFile)
	if err != nil {
		return "", fmt.Errorf("failed to read boot ID file: %w", err)
	}
	return strings.TrimSpace(string(currentRebootID)), nil
}

func hasBeenBooted(dpu *provisioningv1.DPU, currentRebootID string) bool {
	return dpu.Status.DPUInternalStatus != nil &&
		dpu.Status.DPUInternalStatus.InitialBootID != nil &&
		*dpu.Status.DPUInternalStatus.InitialBootID != currentRebootID
}
