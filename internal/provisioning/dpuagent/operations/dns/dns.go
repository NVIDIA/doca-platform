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
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

const (
	CondDNSConfigured = "DNSConfigured"

	defaultRootFS       = "/"
	resolvConfPath      = "/etc/resolv.conf"
	resolvConfBakPath   = "/etc/resolv.conf.bak"
	resolvConfTarget    = "/var/run/systemd/resolve/resolv.conf"
	resolvedConfPath    = "/etc/systemd/resolved.conf.d/01-dpf.conf"
	nmConfPath          = "/etc/NetworkManager/conf.d/90-dpf.conf"
	resolvedConfContent = `[Resolve]
DNSStubListener=no
`
	nmConfContent = `[main]
dns=none
`
)

type ConfigureDNS struct {
	rootFS  string
	runBash func(cmd string) (bytes.Buffer, bytes.Buffer, error)
}

func (d *ConfigureDNS) Name() string {
	return "Configure DNS"
}

func (d *ConfigureDNS) ConditionType() string {
	return CondDNSConfigured
}

func (d *ConfigureDNS) ShouldSkip(ctx *operations.Context) bool {
	if ctx.Options.SkipDNSConfig {
		return true
	}

	if ctx.LatestDPU == nil {
		klog.Error("Latest DPU not retrieved, will return error during execution. (this should never happen)")
		return false
	}
	if ctx.LatestDPU.Status.AgentStatus == nil {
		return false
	}
	cond := meta.FindStatusCondition(ctx.LatestDPU.Status.AgentStatus.Conditions, CondDNSConfigured)
	if cond != nil && cond.Status == metav1.ConditionTrue {
		klog.Infof("DNS already configured, skip")
		return true
	}
	return false
}

func (d *ConfigureDNS) ShouldUpdateStatusBeforeContinue(ctx *operations.Context) bool {
	return true
}

func (d *ConfigureDNS) Execute(execCtx context.Context, optCtx *operations.Context) error {
	if optCtx.LatestDPU == nil {
		return fmt.Errorf("latest DPU not retrieved")
	}

	if d.rootFS == "" {
		d.rootFS = defaultRootFS
	}
	if d.runBash == nil {
		d.runBash = bash.Run
	}

	// Step 1: Create /etc/systemd/resolved.conf.d/01-dpf.conf - Disables DNSStubListener
	if err := ensureFile(filepath.Join(d.rootFS, resolvedConfPath), resolvedConfContent); err != nil {
		return fmt.Errorf("failed to ensure file %s: %w", resolvedConfPath, err)
	}

	// Step 2: Create /etc/NetworkManager/conf.d/90-dpf.conf - Disables NetworkManager DNS
	if err := ensureFile(filepath.Join(d.rootFS, nmConfPath), nmConfContent); err != nil {
		return fmt.Errorf("failed to ensure file %s: %w", nmConfPath, err)
	}

	// Step 3: Rename /etc/resolv.conf to /etc/resolv.conf.bak
	resolvConf := filepath.Join(d.rootFS, resolvConfPath)
	resolvConfBak := filepath.Join(d.rootFS, resolvConfBakPath)
	if err := os.Rename(resolvConf, resolvConfBak); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to backup %s to %s: %w", resolvConfPath, resolvConfBakPath, err)
	}
	klog.Infof("Backed up %s to %s", resolvConfPath, resolvConfBakPath)

	// Step 4: ln -s /var/run/systemd/resolve/resolv.conf /etc/resolv.conf
	if err := os.Symlink(resolvConfTarget, resolvConf); err != nil {
		return fmt.Errorf("failed to create symlink %s -> %s: %w", resolvConfPath, resolvConfTarget, err)
	}
	klog.Infof("Created symlink %s -> %s", resolvConfPath, resolvConfTarget)

	// Step 5: systemctl mask dnsmasq
	if stdout, stderr, err := d.runBash("systemctl mask dnsmasq"); err != nil {
		return fmt.Errorf("failed to mask dnsmasq: %w, stdout: %s, stderr: %s", err, stdout.String(), stderr.String())
	}
	klog.Infof("Successfully masked dnsmasq service")

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
