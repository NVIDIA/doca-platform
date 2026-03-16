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

package ovsscript

import (
	"context"
	"fmt"
	"strings"

	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"
)

const (
	defaultScriptPath = "/opt/dpf/ovs.sh"
)

type RunOVSScript struct {
	scriptPath string
}

func (o *RunOVSScript) Name() string {
	return "Run OVS Script"
}

func (o *RunOVSScript) ConditionType() string {
	return "OVSScriptRun"
}

func (o *RunOVSScript) ShouldSkip(ctx *operations.Context) bool {
	return ctx.Options.SkipOVSRawScript || strings.TrimSpace(ctx.DPUFlavor.Spec.OVS.RawConfigScript) == ""
}

func (o *RunOVSScript) ShouldUpdateStatusBeforeContinue(ctx *operations.Context) bool {
	return false
}

func (o *RunOVSScript) Execute(execCtx context.Context, optCtx *operations.Context) error {
	script := o.scriptPath
	if script == "" {
		script = defaultScriptPath
	}
	stdout, stderr, err := bash.Run(script)
	if err != nil {
		return fmt.Errorf("failed to run script %s. stdout: %s, stderr: %s, err: %w", script, stdout.String(), stderr.String(), err)
	}
	return nil
}
