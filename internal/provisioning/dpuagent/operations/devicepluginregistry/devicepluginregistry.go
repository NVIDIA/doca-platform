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

package devicepluginregistry

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	"k8s.io/klog/v2"
)

const (
	defaultPluginRegistryDir = "/var/lib/kubelet/plugins_registry"
	// defaultDoneMarker records stale-socket cleanup (under /run) so agent restarts can
	// skip it until the next reboot clears that directory.
	defaultDoneMarker = "/run/dpu-agent/kubelet-registry-cleaned"
)

// CleanPluginRegistry removes stale device-plugin registration sockets left over from
// a previous boot. A stale socket races kubelet's plugin-watcher on start and can
// disconnect the live client (kubernetes/kubernetes#128043). It runs as its own
// operation, independent of kubelet.StartKubelet, so skipping StartKubelet (e.g. for
// debugging) doesn't also skip this cleanup.
type CleanPluginRegistry struct {
	pluginRegistryDir string
	doneMarker        string
}

func (c *CleanPluginRegistry) Name() string {
	return "Clean Kubelet Plugin Registry"
}

func (c *CleanPluginRegistry) ConditionType() string {
	return "KubeletPluginRegistryCleaned"
}

func (c *CleanPluginRegistry) ShouldSkip(ctx *operations.Context) bool {
	return ctx.Options.SkipCleanPluginRegistry
}

func (c *CleanPluginRegistry) ShouldUpdateStatusBeforeContinue(ctx *operations.Context) bool {
	return false
}

func (c *CleanPluginRegistry) Execute(_ context.Context, _ *operations.Context) error {
	// Only do this once per actual boot: dpu-agent.service can restart on failure
	// without a reboot, and by then the device plugin's socket is live.
	if c.completedThisBoot() {
		return nil
	}
	removed, err := removeStalePluginRegistrySockets(c.pluginRegistry())
	if err != nil {
		return fmt.Errorf("failed to remove stale device plugin registration sockets: %w", err)
	}
	if removed > 0 {
		klog.Infof("Removed %d stale device plugin registration socket(s)", removed)
	}
	return c.writeDoneMarker()
}

func (c *CleanPluginRegistry) pluginRegistry() string {
	if c.pluginRegistryDir != "" {
		return c.pluginRegistryDir
	}
	return defaultPluginRegistryDir
}

func (c *CleanPluginRegistry) markerPath() string {
	if c.doneMarker != "" {
		return c.doneMarker
	}
	return defaultDoneMarker
}

// completedThisBoot reports whether the stale-socket cleanup already ran this boot,
// so later agent restarts can skip it until the next reboot.
func (c *CleanPluginRegistry) completedThisBoot() bool {
	_, err := os.Stat(c.markerPath())
	return err == nil
}

func (c *CleanPluginRegistry) writeDoneMarker() error {
	path := c.markerPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create kubelet registry cleanup marker directory %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, nil, 0644); err != nil {
		return fmt.Errorf("write kubelet registry cleanup marker %s: %w", path, err)
	}
	klog.V(2).Infof("Kubelet registry cleanup marker written to %s", path)
	return nil
}

// removeStalePluginRegistrySockets removes every socket under dir and returns how many were removed.
func removeStalePluginRegistrySockets(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return 0, nil
	} else if err != nil {
		return 0, fmt.Errorf("failed to read directory %s: %w", dir, err)
	}
	removed := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sock" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return removed, fmt.Errorf("failed to remove stale socket %s: %w", path, err)
		}
		klog.V(2).Infof("Removed stale device plugin registration socket %s", path)
		removed++
	}
	return removed, nil
}
