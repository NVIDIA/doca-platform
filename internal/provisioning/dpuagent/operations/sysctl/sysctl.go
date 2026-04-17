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
	"sort"
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
	mandatoryParams = map[string]string{
		"net.ipv4.ip_forward":                 "1",
		"net.bridge.bridge-nf-call-iptables":  "1",
		"net.bridge.bridge-nf-call-ip6tables": "1",
		// Required by kubelet: with ProtectKernelDefaults=true, kubelet validates these sysctl values and may fail to start if they are unexpected.
		"kernel.panic_on_oops": "1",
		"kernel.panic":         "10",
		"vm.overcommit_memory": "1",
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
	effectiveFlavorParams, err := getEffectiveSysctlParams(optCtx.DPUFlavor.Spec.Sysctl.Parameters)
	if err != nil {
		return fmt.Errorf("failed to calculate effective flavor sysctl parameters: %w", err)
	}
	if err := validateNoConflicts(effectiveFlavorParams); err != nil {
		return err
	}

	if s.kernelParametersDirectory == "" {
		s.kernelParametersDirectory = defaultKernelParametersDirectory
	}
	if err := s.checkParams("Mandatory", mandatoryParams); err != nil {
		return err
	}
	if err := s.checkParams("DPUFlavor", effectiveFlavorParams); err != nil {
		return err
	}
	return nil
}

func (s *CheckParams) checkParams(name string, params map[string]string) error {
	mismatches, err := isRuntimeSysctlParamsMatching(s.kernelParametersDirectory, params)
	if err != nil {
		return fmt.Errorf("failed to check if %s sysctl parameters match expected values: %w", name, err)
	}
	if len(mismatches) > 0 {
		msg := []string{}
		for _, mismatch := range mismatches {
			msg = append(msg, mismatch.String())
		}
		return fmt.Errorf("%s sysctl parameters mismatch. %s", name, strings.Join(msg, "; "))
	}
	return nil
}

type SetParams struct {
	etcDirectory              string
	kernelParametersDirectory string
	applyParams               func() error
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
	effectiveFlavorParams, err := getEffectiveSysctlParams(optCtx.DPUFlavor.Spec.Sysctl.Parameters)
	if err != nil {
		return fmt.Errorf("failed to calculate effective flavor sysctl parameters: %w", err)
	}
	if err := validateNoConflicts(effectiveFlavorParams); err != nil {
		return err
	}

	if s.etcDirectory == "" {
		s.etcDirectory = defaultEtcDirectory
	}
	if s.kernelParametersDirectory == "" {
		s.kernelParametersDirectory = defaultKernelParametersDirectory
	}

	mandatoryUpdated, err := s.appendMandatoryParamsToConf()
	if err != nil {
		return fmt.Errorf("failed to append mandatory params to sysctl.conf: %w", err)
	}
	userUpdated, err := s.writeUserParams(effectiveFlavorParams)
	if err != nil {
		return fmt.Errorf("failed to write user params: %w", err)
	}
	// Conflicts were already rejected above, so mandatory and flavor params do
	// not overwrite each other here. This merge just builds the final expected
	// runtime params.
	expectedRuntimeParams := make(map[string]string, len(mandatoryParams)+len(effectiveFlavorParams))
	for key, value := range mandatoryParams {
		expectedRuntimeParams[key] = value
	}
	for key, value := range effectiveFlavorParams {
		expectedRuntimeParams[key] = value
	}
	mismatches, err := isRuntimeSysctlParamsMatching(s.kernelParametersDirectory, expectedRuntimeParams)
	if err != nil {
		return fmt.Errorf("failed to compare running sysctl parameters: %w", err)
	}
	// dpu-agent is a long-running service and systemd may restart it after an unexpected exit.
	// In that case the managed files may already be correct while the live sysctl state still
	// needs to be restored, so we must check the current runtime values before skipping apply.
	needApply := mandatoryUpdated || userUpdated || len(mismatches) > 0
	if !needApply {
		return nil
	}
	klog.Infof("Need to apply sysctl parameters: mandatoryUpdated=%v, userUpdated=%v, runtimeMatches=%v", mandatoryUpdated, userUpdated, len(mismatches) == 0)
	if s.applyParams == nil {
		s.applyParams = applyParams
	}
	return s.applyParams()
}

func (s *SetParams) appendMandatoryParamsToConf() (bool, error) {
	sysctlConfPath := filepath.Join(s.etcDirectory, "sysctl.conf")

	existingContent, err := os.ReadFile(sysctlConfPath)
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("failed to read %s: %w", sysctlConfPath, err)
	}
	existingParams := make(map[string]string)
	for _, line := range strings.Split(string(existingContent), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		key, value, err := parseSysctlParameter(line)
		if err != nil {
			return false, fmt.Errorf("failed to parse existing sysctl parameter %s: %w", line, err)
		}
		existingParams[key] = value
	}

	f, err := os.OpenFile(sysctlConfPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return false, err
	}
	defer f.Close() //nolint:errcheck

	modified := false

	for key, value := range mandatoryParams {
		param := fmt.Sprintf("%s=%s", key, value)
		if existingValue, exists := existingParams[key]; exists && existingValue == value {
			klog.Infof("Skipping %s, already exists in %s", param, sysctlConfPath)
			continue
		}
		if _, err := fmt.Fprintf(f, "%s\n", param); err != nil {
			return modified, err
		}
		modified = true
		klog.Infof("Appended %s to %s", param, sysctlConfPath)
	}
	return modified, nil
}

func (s *SetParams) writeUserParams(params map[string]string) (bool, error) {
	if len(params) == 0 {
		return false, nil
	}

	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("%s=%s", key, params[key]))
	}
	expectedContent := strings.Join(lines, "\n") + "\n"

	sysctlConfPath := filepath.Join(s.etcDirectory, "sysctl.d", "99-dpf.conf")
	existingContent, err := os.ReadFile(sysctlConfPath)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if string(existingContent) == expectedContent {
		klog.Infof("Skipping %s, content already matches expected params", sysctlConfPath)
		return false, nil
	}

	f, err := os.OpenFile(sysctlConfPath, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return false, err
	}
	defer f.Close() //nolint:errcheck

	if _, err := f.WriteString(expectedContent); err != nil {
		return false, err
	}

	klog.Infof("Wrote %d params to %s", len(params), sysctlConfPath)
	return true, nil
}

func parseSysctlParameter(param string) (string, string, error) {
	parts := strings.SplitN(param, "=", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid sysctl parameter format: %s", param)
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), nil
}

func getEffectiveSysctlParams(params []string) (map[string]string, error) {
	effectiveParams := make(map[string]string, len(params))
	for _, param := range params {
		key, value, err := parseSysctlParameter(param)
		if err != nil {
			return nil, fmt.Errorf("failed to parse sysctl parameter %s: %w", param, err)
		}
		effectiveParams[key] = value
	}
	return effectiveParams, nil
}

func validateNoConflicts(flavorParams map[string]string) error {
	conflicts := []string{}
	for key, flavorValue := range flavorParams {
		if mandatoryValue, ok := mandatoryParams[key]; ok && mandatoryValue != flavorValue {
			conflicts = append(conflicts, fmt.Sprintf("%s: flavor requires %s, mandatory requires %s", key, flavorValue, mandatoryValue))
		}
	}
	if len(conflicts) == 0 {
		return nil
	}
	return fmt.Errorf("flavor sysctl parameters conflict with mandatory parameters: %s", strings.Join(conflicts, "; "))
}

type MismatchParam struct {
	Key           string
	ExpectedValue string
	CurrentValue  string
}

func (m MismatchParam) String() string {
	return fmt.Sprintf("%s: expected %s, current %s", m.Key, m.ExpectedValue, m.CurrentValue)
}

func isRuntimeSysctlParamsMatching(kernelParametersDirectory string, params map[string]string) ([]MismatchParam, error) {
	var mismatch []MismatchParam
	for key, expectedValue := range params {
		currentValue, err := getCurrentValue(kernelParametersDirectory, key)
		if err != nil {
			return mismatch, err
		}
		if currentValue != expectedValue {
			mismatch = append(mismatch, MismatchParam{
				Key:           key,
				ExpectedValue: expectedValue,
				CurrentValue:  currentValue,
			})
		}
	}
	return mismatch, nil
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

func applyParams() error {
	stdout, stderr, err := bash.Run("sysctl --system")
	if err != nil {
		return fmt.Errorf("failed to apply sysctl parameters: %w, stdout: %s, stderr: %s", err, stdout.String(), stderr.String())
	}
	return nil
}
