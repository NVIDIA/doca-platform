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
	// scriptHeader is the header cloud-init used to prepend to /opt/dpf/ovs.sh.
	// The agent now writes the file itself and keeps the same header, so flavor
	// scripts still run with bash and stop on the first error.
	scriptHeader = "#! /bin/bash\nset -e\n"
	// DoneMarkerFileName is the OVS completion marker under the agent run directory.
	// An in-place reconfig Run() deletes it so the script runs again in the same boot.
	// A process restart keeps the marker and skips until reboot.
	DoneMarkerFileName = "ovs-script-complete"
	// defaultDoneMarker records OVS script completion under /run so an agent restart
	// after a crash can skip until the next reboot clears that directory.
	defaultDoneMarker = "/run/dpu-agent/" + DoneMarkerFileName
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
	// Write the script from the flavor on every execution, so the op runs the
	// current flavor even when a reconfig Run() picked up an in-place edit.
	if err := o.writeScript(optCtx.DPUFlavor.Spec.OVS.RawConfigScript); err != nil {
		return err
	}
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

// writeScript atomically replaces the OVS script with the flavor's raw script,
// so a stale script written for an earlier flavor is never executed.
func (o *RunOVSScript) writeScript(raw string) error {
	path := o.script()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create OVS script directory %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".ovs-script-*")
	if err != nil {
		return fmt.Errorf("create temporary OVS script in %s: %w", dir, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	content := scriptHeader + raw
	if !strings.HasSuffix(raw, "\n") {
		content += "\n"
	}
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary OVS script %s: %w", tmp.Name(), err)
	}
	if err := tmp.Chmod(0700); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temporary OVS script %s: %w", tmp.Name(), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary OVS script %s: %w", tmp.Name(), err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replace OVS script %s: %w", path, err)
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
// so later agent restarts can skip it until the next reboot. In-place reconfig
// removes the marker before Run() so this returns false for that Run().
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
