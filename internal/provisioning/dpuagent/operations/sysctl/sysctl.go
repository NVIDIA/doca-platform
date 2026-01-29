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

package sysctl

import (
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
	defaultKernelParametersDirectory = "/proc/sys"
	defaultEtcDirectory              = "/etc"
)

var (
	mandatoryParams = []string{
		"net.ipv4.ip_forward=1",
		"net.bridge.bridge-nf-call-iptables=1",
		"net.bridge.bridge-nf-call-ip6tables=1",
	}
)

type CheckParams struct {
	kernelParametersDirectory string
}

func (s *CheckParams) Name() string {
	return "Check Sysctl Parameters"
}

func (s *CheckParams) ConditionType() string {
	return "SysctlParametersChecked"
}

func (s *CheckParams) ShouldSkip(ctx *operations.Context) bool {
	return false
}

func (s *CheckParams) ShouldUpdateStatusBeforeContinue(ctx *operations.Context) bool {
	return false
}

func (s *CheckParams) Execute(execCtx context.Context, optCtx *operations.Context) error {
	if s.kernelParametersDirectory == "" {
		s.kernelParametersDirectory = defaultKernelParametersDirectory
	}
	allParams := append(mandatoryParams, optCtx.DPUFlavor.Spec.Sysctl.Parameters...)
	needUpdate, err := isUpdateRequired(s.kernelParametersDirectory, allParams)
	if err != nil {
		return fmt.Errorf("failed to check if sysctl parameters need update: %w", err)
	}
	if len(needUpdate) == 0 {
		return nil
	}
	return fmt.Errorf("sysctl parameters mismatch. Current: %s", strings.Join(needUpdate, ", "))
}

type SetParams struct {
	etcDirectory string
	applyParams  func() error
}

func (s *SetParams) Name() string {
	return "Set Sysctl"
}

func (s *SetParams) ConditionType() string {
	return "SysctlParametersSet"
}

func (s *SetParams) ShouldSkip(ctx *operations.Context) bool {
	return ctx.Options.SkipSysctl
}

func (s *SetParams) ShouldUpdateStatusBeforeContinue(ctx *operations.Context) bool {
	return false
}

func (s *SetParams) Execute(execCtx context.Context, optCtx *operations.Context) error {
	if s.etcDirectory == "" {
		s.etcDirectory = defaultEtcDirectory
	}
	// To maintain consistent behavior with old cloud-init versions,
	// append mandatory parameters to sysctl.conf regardless of whether they match the current values.
	if err := s.appendMandatoryParamsToConf(mandatoryParams); err != nil {
		return fmt.Errorf("failed to append mandatory params to sysctl.conf: %w", err)
	}
	if err := s.writeUserParams(optCtx.DPUFlavor.Spec.Sysctl.Parameters); err != nil {
		return fmt.Errorf("failed to write user params: %w", err)
	}
	if s.applyParams == nil {
		s.applyParams = applyParams
	}
	return s.applyParams()
}

func (s *SetParams) appendMandatoryParamsToConf(params []string) error {
	sysctlConfPath := filepath.Join(s.etcDirectory, "sysctl.conf")

	// Read existing content to check for duplicates
	existingContent, err := os.ReadFile(sysctlConfPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read %s: %w", sysctlConfPath, err)
	}
	existingLines := make(map[string]bool)
	for _, line := range strings.Split(string(existingContent), "\n") {
		existingLines[strings.TrimSpace(line)] = true
	}

	f, err := os.OpenFile(sysctlConfPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck

	for _, param := range params {
		if existingLines[strings.TrimSpace(param)] {
			klog.Infof("Skipping %s, already exists in %s", param, sysctlConfPath)
			continue
		}
		if _, err := fmt.Fprintf(f, "%s\n", param); err != nil {
			return err
		}
		klog.Infof("Appended %s to %s", param, sysctlConfPath)
	}
	return nil
}

func (s *SetParams) writeUserParams(params []string) error {
	sysctlConfPath := filepath.Join(s.etcDirectory, "sysctl.d", "99-dpf.conf")
	// Use O_TRUNC to overwrite the file if it exists, or O_CREATE to create it
	f, err := os.OpenFile(sysctlConfPath, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck

	for _, param := range params {
		if _, err := fmt.Fprintf(f, "%s\n", param); err != nil {
			return err
		}
	}
	klog.Infof("Wrote %d params to %s", len(params), sysctlConfPath)
	return nil
}

func parseSysctlParameter(param string) (string, string, error) {
	parts := strings.SplitN(param, "=", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid sysctl parameter format: %s", param)
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), nil
}

func getCurrentValue(kernelParametersDirectory string, key string) (string, error) {
	filename := strings.ReplaceAll(key, ".", "/")
	path := filepath.Join(kernelParametersDirectory, filename)
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read sysctl parameter %s: %w", key, err)
	}
	return strings.TrimSpace(string(content)), nil
}

func isUpdateRequired(kernelParametersDirectory string, params []string) ([]string, error) {
	if kernelParametersDirectory == "" {
		kernelParametersDirectory = defaultKernelParametersDirectory
	}
	needUpdate := []string{}
	allParams := append(mandatoryParams, params...)
	for _, param := range allParams {
		key, expectedValue, err := parseSysctlParameter(param)
		if err != nil {
			return nil, fmt.Errorf("failed to parse sysctl parameter %s: %w", param, err)
		}
		currentValue, err := getCurrentValue(kernelParametersDirectory, key)
		if err != nil {
			klog.Warningf("failed to get current value of sysctl parameter %s: %v", key, err)
			continue
		}
		if currentValue != expectedValue {
			needUpdate = append(needUpdate, fmt.Sprintf("%s=%s", key, currentValue))
		}
	}
	return needUpdate, nil
}

func applyParams() error {
	stdout, stderr, err := bash.Run("sysctl --system")
	if err != nil {
		return fmt.Errorf("failed to apply sysctl parameters: %w, stdout: %s, stderr: %s", err, stdout.String(), stderr.String())
	}
	return nil
}
