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

package cmd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleTemplate = `apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavorTemplate
metadata:
  name: sample
spec:
  template: |
    spec:
      bfcfgParameters:
      - "MTU={{ .mtu }}"
`

const sampleDevice = `apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUDevice
metadata:
  name: dev0
spec:
  values:
    mtu: 9000
`

const sampleDeviceWithoutValues = `apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUDevice
metadata:
  name: dev0
spec: {}
`

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func resetRenderOpts() {
	ftRenderOpts = flavorTemplateRenderOptions{}
}

func TestLoadTemplateSpecFromFile(t *testing.T) {
	resetRenderOpts()
	defer resetRenderOpts()
	ftRenderOpts.templateFile = writeTemp(t, "tmpl.yaml", sampleTemplate)

	spec, err := loadTemplateSpec()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(spec.Template, "{{ .mtu }}") {
		t.Fatalf("template not loaded, got: %q", spec.Template)
	}
}

func TestLoadTemplateSpecRequiresFile(t *testing.T) {
	resetRenderOpts()
	defer resetRenderOpts()

	_, err := loadTemplateSpec()
	if err == nil || !strings.Contains(err.Error(), "a DPUFlavorTemplate file is required") {
		t.Fatalf("expected missing-file error, got: %v", err)
	}
}

func TestLoadTemplateSpecUnreadableFile(t *testing.T) {
	resetRenderOpts()
	defer resetRenderOpts()
	ftRenderOpts.templateFile = filepath.Join(t.TempDir(), "does-not-exist.yaml")

	_, err := loadTemplateSpec()
	if err == nil || !strings.Contains(err.Error(), "failed to read template file") {
		t.Fatalf("expected read error, got: %v", err)
	}
}

func TestLoadTemplateSpecMalformedFile(t *testing.T) {
	resetRenderOpts()
	defer resetRenderOpts()
	ftRenderOpts.templateFile = writeTemp(t, "bad.yaml", "spec: [not: a: template")

	_, err := loadTemplateSpec()
	if err == nil || !strings.Contains(err.Error(), "failed to parse DPUFlavorTemplate") {
		t.Fatalf("expected parse error, got: %v", err)
	}
}

func TestLoadDeviceValuesFromFile(t *testing.T) {
	resetRenderOpts()
	defer resetRenderOpts()
	ftRenderOpts.dpuDeviceFile = writeTemp(t, "device.yaml", sampleDevice)

	values, err := loadDeviceValues()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if values == nil || !strings.Contains(string(values.Raw), "9000") {
		t.Fatalf("device values not loaded, got: %v", values)
	}
}

func TestLoadDeviceValuesRequiresFile(t *testing.T) {
	resetRenderOpts()
	defer resetRenderOpts()

	_, err := loadDeviceValues()
	if err == nil || !strings.Contains(err.Error(), "a DPUDevice file is required") {
		t.Fatalf("expected missing-file error, got: %v", err)
	}
}

func TestLoadDeviceValuesUnreadableFile(t *testing.T) {
	resetRenderOpts()
	defer resetRenderOpts()
	ftRenderOpts.dpuDeviceFile = filepath.Join(t.TempDir(), "does-not-exist.yaml")

	_, err := loadDeviceValues()
	if err == nil || !strings.Contains(err.Error(), "failed to read DPUDevice file") {
		t.Fatalf("expected read error, got: %v", err)
	}
}

func TestLoadDeviceValuesMalformedFile(t *testing.T) {
	resetRenderOpts()
	defer resetRenderOpts()
	ftRenderOpts.dpuDeviceFile = writeTemp(t, "bad-device.yaml", "spec: [not: valid")

	_, err := loadDeviceValues()
	if err == nil || !strings.Contains(err.Error(), "failed to parse DPUDevice") {
		t.Fatalf("expected parse error, got: %v", err)
	}
}

func TestRunFlavorTemplateRenderPropagatesRenderError(t *testing.T) {
	resetRenderOpts()
	defer resetRenderOpts()
	// Template references .mtu but the device provides no values, so render must fail.
	ftRenderOpts.templateFile = writeTemp(t, "tmpl.yaml", sampleTemplate)
	ftRenderOpts.dpuDeviceFile = writeTemp(t, "device.yaml", sampleDeviceWithoutValues)

	err := runFlavorTemplateRender()
	if err == nil || !strings.Contains(err.Error(), "render failed") {
		t.Fatalf("expected render failure to propagate, got: %v", err)
	}
}

func TestRunFlavorTemplateRenderFromFiles(t *testing.T) {
	resetRenderOpts()
	defer resetRenderOpts()
	ftRenderOpts.templateFile = writeTemp(t, "tmpl.yaml", sampleTemplate)
	ftRenderOpts.dpuDeviceFile = writeTemp(t, "device.yaml", sampleDevice)

	out := captureStdout(t, runFlavorTemplateRender)
	if !strings.Contains(out, "MTU=9000") {
		t.Fatalf("rendered output missing substituted value, got:\n%s", out)
	}
	if !strings.Contains(out, "kind: DPUFlavor") {
		t.Fatalf("rendered output missing DPUFlavor kind, got:\n%s", out)
	}
}

// captureStdout runs fn while capturing everything written to os.Stdout.
func captureStdout(t *testing.T, fn func() error) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	runErr := fn()
	_ = w.Close()
	os.Stdout = orig

	data, _ := io.ReadAll(r)
	if runErr != nil {
		t.Fatalf("run returned error: %v", runErr)
	}
	return string(data)
}
