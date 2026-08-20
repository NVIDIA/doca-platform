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
	"os"
	"path/filepath"
	"strings"

	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"

	"k8s.io/klog/v2"
)

const (
	defaultScriptPath = "/opt/dpf/ovs.sh"
	// defaultDoneMarker records OVS script completion (with boot ID) under /run so agent
	// restarts can use to detect previous successful runs and skip it until the next reboot clears that directory.
	defaultDoneMarker = "/run/dpu-agent/ovs-script-complete"
)

type RunOVSScript struct {
	scriptPath string
	doneMarker string
}

func (o *RunOVSScript) Name() string {
	return "Run OVS Script"
}

func (o *RunOVSScript) ConditionType() string {
	return "OVSScriptRun"
}

func (o *RunOVSScript) ShouldSkip(ctx *operations.Context) bool {
	if ctx.Options.SkipOVSRawScript || strings.TrimSpace(ctx.DPUFlavor.Spec.OVS.RawConfigScript) == "" {
		return true
	}
	if o.completedThisBoot() {
		klog.Infof("Skipping OVS script; completion marker %s exists", o.markerPath())
		return true
	}
	return false
}

func (o *RunOVSScript) ShouldUpdateStatusBeforeContinue(ctx *operations.Context) bool {
	return false
}

func (o *RunOVSScript) Execute(execCtx context.Context, optCtx *operations.Context) error {
	script := o.script()
	stdout, stderr, err := bash.Run(script)
	if err != nil {
		return fmt.Errorf("failed to run script %s. stdout: %s, stderr: %s, err: %w", script, stdout.String(), stderr.String(), err)
	}
	if err := o.writeDoneMarker(); err != nil {
		return err
	}
	return nil
}

func (o *RunOVSScript) script() string {
	if o.scriptPath != "" {
		return o.scriptPath
	}
	return defaultScriptPath
}

func (o *RunOVSScript) markerPath() string {
	if o.doneMarker != "" {
		return o.doneMarker
	}
	return defaultDoneMarker
}

// completedThisBoot reports whether the OVS script already ran successfully this boot,
// so later agent restarts can skip it until the next reboot.
func (o *RunOVSScript) completedThisBoot() bool {
	_, err := os.Stat(o.markerPath())
	return err == nil
}

func (o *RunOVSScript) writeDoneMarker() error {
	path := o.markerPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create OVS script completion marker directory %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, nil, 0644); err != nil {
		return fmt.Errorf("write OVS script completion marker %s: %w", path, err)
	}
	klog.Infof("OVS script completion marker written to %s", path)
	return nil
}
