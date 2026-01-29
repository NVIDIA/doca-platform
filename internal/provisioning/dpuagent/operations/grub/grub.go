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
	defaultGrubConfigDir = "/etc/default/grub.d"
	grubConfigFileName   = "99-dpf.cfg"
)

type ConfigureKernelCmdLine struct {
	grubConfigDir string
	runBash       func(cmd string) (bytes.Buffer, bytes.Buffer, error)
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

	// Join all kernel parameters with space
	params := strings.TrimSpace(strings.Join(optCtx.DPUFlavor.Spec.Grub.KernelParameters, " "))

	// Write grub config file
	configPath := filepath.Join(g.grubConfigDir, grubConfigFileName)
	content := fmt.Sprintf("GRUB_CMDLINE_LINUX=\"%s\"\n", params)

	if err := os.MkdirAll(g.grubConfigDir, 0755); err != nil {
		return fmt.Errorf("failed to create grub config directory %s: %w", g.grubConfigDir, err)
	}

	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
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
	return nil
}
