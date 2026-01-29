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

package dns

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	"k8s.io/klog/v2"
)

const (
	defaultRootFS = "/"
)

type ConfigureDNS struct {
	rootFS string
}

func (d *ConfigureDNS) Name() string {
	return "Configure DNS"
}

func (d *ConfigureDNS) ConditionType() string {
	return "DNSConfigured"
}

func (d *ConfigureDNS) ShouldSkip(ctx *operations.Context) bool {
	return ctx.Options.SkipDNSConfig
}

func (d *ConfigureDNS) ShouldUpdateStatusBeforeContinue(ctx *operations.Context) bool {
	return false
}

func (d *ConfigureDNS) Execute(execCtx context.Context, optCtx *operations.Context) error {
	files := []struct {
		path    string
		content string
	}{
		{
			path: "/etc/systemd/resolved.conf.d/01-dpf.conf",
			content: `[Resolve]
DNSStubListener=no
`,
		},
		{
			path: "/etc/NetworkManager/conf.d/90-dpf.conf",
			content: `[main]
dns=none
`,
		},
	}

	if d.rootFS == "" {
		d.rootFS = defaultRootFS
	}
	for _, f := range files {
		if err := ensureFile(filepath.Join(d.rootFS, f.path), f.content); err != nil {
			return fmt.Errorf("failed to ensure file %s: %w", f.path, err)
		}
	}
	return nil
}

func ensureFile(path string, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", path, err)
	}
	klog.Infof("Successfully created %s", path)
	return nil
}
