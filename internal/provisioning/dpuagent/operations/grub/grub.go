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

package grub

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"

	"k8s.io/klog/v2"
)

const (
	defaultGrubConfigDir   = "/etc/default/grub.d"
	defaultProcCmdlinePath = "/proc/cmdline"
	grubConfigFileName     = "99-dpf.cfg"
)

type ConfigureKernelCmdLine struct {
	grubConfigDir   string
	procCmdlinePath string
	runBash         func(cmd string) (bytes.Buffer, bytes.Buffer, error)
}

func (g *ConfigureKernelCmdLine) Name() string {
	return "Configure Kernel Cmd Line"
}

func (g *ConfigureKernelCmdLine) ConditionType() string {
	return "KernelCmdLineConfigured"
}

func (g *ConfigureKernelCmdLine) ShouldSkip(ctx *operations.Context) bool {
	if ctx.Options.SkipKernelCmdLine {
		return true
	}
	return len(ctx.DPUFlavor.Spec.Grub.KernelParameters) == 0
}

func (g *ConfigureKernelCmdLine) ShouldUpdateStatusBeforeContinue(ctx *operations.Context) bool {
	return false
}

func (g *ConfigureKernelCmdLine) Execute(execCtx context.Context, optCtx *operations.Context) error {
	if g.grubConfigDir == "" {
		g.grubConfigDir = defaultGrubConfigDir
	}
	if g.procCmdlinePath == "" {
		g.procCmdlinePath = defaultProcCmdlinePath
	}

	desiredParams := optCtx.DPUFlavor.Spec.Grub.KernelParameters

	// Join all kernel parameters with space
	params := strings.TrimSpace(strings.Join(desiredParams, " "))

	// Skip if grub config file already exists and all parameters are active in /proc/cmdline
	configPath := filepath.Join(g.grubConfigDir, grubConfigFileName)
	if fileExists(configPath) && allParamsPresent(g.procCmdlinePath, desiredParams) {
		klog.Infof("Grub config already up-to-date and all kernel parameters active, skipping")
		return nil
	}

	// Write grub config file
	if err := os.MkdirAll(g.grubConfigDir, 0755); err != nil {
		return fmt.Errorf("failed to create grub config directory %s: %w", g.grubConfigDir, err)
	}

	expectedContent := fmt.Sprintf("GRUB_CMDLINE_LINUX=\"%s\"\n", params)
	if err := os.WriteFile(configPath, []byte(expectedContent), 0644); err != nil {
		return fmt.Errorf("failed to write grub config file %s: %w", configPath, err)
	}
	klog.Infof("Wrote kernel parameters to %s: %s", configPath, params)

	// Update grub configuration
	if g.runBash == nil {
		g.runBash = bash.Run
	}
	if stdout, stderr, err := g.runBash("update-grub"); err != nil {
		return fmt.Errorf("failed to update grub: %w, stdout: %s, stderr: %s", err, stdout.String(), stderr.String())
	}
	// Flush filesystem buffers to ensure grub config is persisted to disk before reboot.
	// Without this, kernel parameters may not take effect after reset (observed in e2e tests).
	if stdout, stderr, err := g.runBash("sync"); err != nil {
		return fmt.Errorf("failed to sync filesystem after grub update: %w, stdout: %s, stderr: %s", err, stdout.String(), stderr.String())
	}
	optCtx.GrubConfigChanged = true
	return nil
}

// CheckKernelCmdLine verifies that the desired kernel parameters are active in /proc/cmdline.
// This should be run after a reboot to confirm the grub configuration took effect.
type CheckKernelCmdLine struct {
	procCmdlinePath string
}

func (c *CheckKernelCmdLine) Name() string {
	return "Check Kernel Cmd Line"
}

func (c *CheckKernelCmdLine) ConditionType() string {
	return "KernelCmdLineChecked"
}

func (c *CheckKernelCmdLine) ShouldSkip(ctx *operations.Context) bool {
	if ctx.Options.SkipKernelCmdLine {
		return true
	}
	return len(ctx.DPUFlavor.Spec.Grub.KernelParameters) == 0
}

func (c *CheckKernelCmdLine) ShouldUpdateStatusBeforeContinue(ctx *operations.Context) bool {
	return false
}

func (c *CheckKernelCmdLine) Execute(execCtx context.Context, optCtx *operations.Context) error {
	if c.procCmdlinePath == "" {
		c.procCmdlinePath = defaultProcCmdlinePath
	}

	desiredParams := optCtx.DPUFlavor.Spec.Grub.KernelParameters
	if allParamsPresent(c.procCmdlinePath, desiredParams) {
		klog.Infof("All desired kernel parameters are active in %s", c.procCmdlinePath)
		return nil
	}
	actual, _ := os.ReadFile(c.procCmdlinePath)
	return fmt.Errorf("not all desired kernel parameters are active in %s, desired: %v, actual: %q", c.procCmdlinePath, desiredParams, strings.TrimSpace(string(actual)))
}

// fileExists checks if a file exists at the given path.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// allParamsPresent reads /proc/cmdline and checks if all desired parameters are already present
// using exact token matching. DPUFlavor declares the full desired parameter (e.g.
// "cgroup_no_v1=net_prio,net_cls"), so we require the exact same token in /proc/cmdline;
// a superset like "cgroup_no_v1=net_prio,net_cls,memory" is treated as a mismatch.
func allParamsPresent(procCmdlinePath string, desiredParams []string) bool {
	data, err := os.ReadFile(procCmdlinePath)
	if err != nil {
		klog.Warningf("Failed to read %s: %v, will proceed with grub update", procCmdlinePath, err)
		return false
	}
	currentCmdline := strings.TrimSpace(string(data))
	currentParams := strings.Fields(currentCmdline)
	currentSet := make(map[string]struct{}, len(currentParams))
	for _, p := range currentParams {
		currentSet[p] = struct{}{}
	}
	for _, desired := range desiredParams {
		if _, found := currentSet[desired]; !found {
			klog.Infof("Kernel parameter %q not found in current cmdline", desired)
			return false
		}
	}
	return true
}
