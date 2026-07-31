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

package containerd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/filesystem"

	"github.com/BurntSushi/toml"
	"github.com/Masterminds/semver/v3"
	"k8s.io/klog/v2"
)

const (
	defaultRootFS       = "/"
	containerdConfigDir = "/etc/containerd"

	containerdSystemdDropInDir = "/etc/systemd/system/containerd.service.d"
	containerdTLSDropInFile    = "99-dpf.conf"
	containerdRestartMarker    = "/run/dpu-agent/containerd-restart-required"

	// mirrorRegistryHost is the upstream registry that is mirrored.
	mirrorRegistryHost = "nvcr.io"

	// defaultRegistryConfigPath is the containerd registry host-config directory
	// used when the existing containerd config does not already declare one.
	defaultRegistryConfigPath = "/etc/containerd/certs.d"

	// criImagesPluginV2 is the containerd 2.x CRI images plugin that owns the
	// registry configuration; criPluginV1 is the containerd 1.x CRI plugin.
	criImagesPluginV2 = "io.containerd.cri.v1.images"
	criPluginV1       = "io.containerd.grpc.v1.cri"
)

const containerdTLSDropInContent = `# Managed by dpu-agent. Do not edit.
[Service]
Environment="GODEBUG=tlsmlkem=0"
`

type ConfigureContainerd struct {
	rootFS               string
	getContainerdVersion func() (string, error)
	runBash              func(cmd string) (bytes.Buffer, bytes.Buffer, error)
	atomicWrite          func(name string, data []byte, perm os.FileMode) error
}

func (c *ConfigureContainerd) Name() string {
	return "Configure Containerd"
}

func (c *ConfigureContainerd) ConditionType() string {
	return "ContainerdConfigured"
}

func (c *ConfigureContainerd) ShouldSkip(ctx *operations.Context) bool {
	return ctx.Options.SkipContainerdConfigration
}

func (c *ConfigureContainerd) ShouldUpdateStatusBeforeContinue(ctx *operations.Context) bool {
	return false
}

func (c *ConfigureContainerd) Execute(execCtx context.Context, optCtx *operations.Context) error {
	if c.rootFS == "" {
		c.rootFS = defaultRootFS
	}

	if c.runBash == nil {
		c.runBash = bash.Run
	}

	endpoint := optCtx.DPUFlavor.Spec.ContainerdConfig.RegistryEndpoint
	if endpoint == "" {
		klog.Info("No registry endpoint configured, skipping registry mirror configuration")
	} else {
		if err := c.configureRegistryMirror(endpoint); err != nil {
			return err
		}
	}

	if _, err := c.ensureTLSCompatibilityDropIn(); err != nil {
		return fmt.Errorf("failed to configure containerd TLS compatibility: %w", err)
	}

	if err := c.reconcileContainerdService(); err != nil {
		return err
	}
	klog.Info("containerd enabled and configured with TLS compatibility mode")
	return nil
}

func (c *ConfigureContainerd) configureRegistryMirror(endpoint string) error {
	version, err := c.containerdVersion()
	if err != nil {
		return fmt.Errorf("failed to get containerd version: %w", err)
	}
	// containerd 2.x removed support for the inline registry.mirrors config.
	// Mirrors must be configured via host-config files (hosts.toml) referenced
	// by registry.config_path, so the two versions take different paths.
	if version.GreaterThanEqual(semver.MustParse("2.0.0")) {
		return c.configureRegistryMirrorV2(endpoint)
	}
	return c.configureRegistryMirrorV1(endpoint)
}

// configureRegistryMirrorV1 configures the registry mirror for containerd 1.x
// using the inline registry.mirrors format under the CRI plugin.
func (c *ConfigureContainerd) configureRegistryMirrorV1(endpoint string) error {
	configPath, err := c.resolveContainerdConfigPath()
	if err != nil {
		return err
	}

	var config map[string]interface{}
	if _, err := toml.DecodeFile(configPath, &config); err != nil {
		return fmt.Errorf("failed to parse containerd config: %w", err)
	}

	plugins, ok := config["plugins"].(map[string]interface{})
	if !ok {
		plugins = make(map[string]interface{})
		config["plugins"] = plugins
	}

	criPlugin, ok := plugins[criPluginV1].(map[string]interface{})
	if !ok {
		criPlugin = make(map[string]interface{})
		plugins[criPluginV1] = criPlugin
	}

	registry, ok := criPlugin["registry"].(map[string]interface{})
	if !ok {
		registry = make(map[string]interface{})
		criPlugin["registry"] = registry
	}

	configs, ok := registry["configs"].(map[string]interface{})
	if !ok {
		configs = make(map[string]interface{})
		registry["configs"] = configs
	}

	nvcrConfig, ok := configs[mirrorRegistryHost].(map[string]interface{})
	if !ok {
		nvcrConfig = make(map[string]interface{})
		configs[mirrorRegistryHost] = nvcrConfig
	}

	tls, ok := nvcrConfig["tls"].(map[string]interface{})
	if !ok {
		tls = make(map[string]interface{})
		nvcrConfig["tls"] = tls
	}
	tls["insecure_skip_verify"] = false

	mirrors, ok := registry["mirrors"].(map[string]interface{})
	if !ok {
		mirrors = make(map[string]interface{})
		registry["mirrors"] = mirrors
	}

	nvcrMirror, ok := mirrors[mirrorRegistryHost].(map[string]interface{})
	if !ok {
		nvcrMirror = make(map[string]interface{})
		mirrors[mirrorRegistryHost] = nvcrMirror
	}
	nvcrMirror["endpoint"] = []string{endpoint}
	// registry.mirrors will have conflict with config_path
	if _, hasConfigPath := registry["config_path"]; hasConfigPath {
		klog.Infof("Removing config_path from containerd registry config to avoid conflict with registry.mirrors")
		delete(registry, "config_path")
	}

	buf := new(bytes.Buffer)
	if err := toml.NewEncoder(buf).Encode(config); err != nil {
		return fmt.Errorf("failed to encode containerd config: %w", err)
	}

	if _, err := c.writeRestartSensitiveFile(configPath, buf.Bytes()); err != nil {
		return fmt.Errorf("failed to write containerd config: %w", err)
	}

	klog.Infof("Successfully updated containerd configuration at %s", configPath)
	return nil
}

// configureRegistryMirrorV2 configures the registry mirror for containerd 2.x.
// The inline registry.mirrors block is ignored by containerd 2.x, so the mirror
// is configured through a host-config file (hosts.toml) located under the
// registry host-config directory referenced by registry.config_path.
func (c *ConfigureContainerd) configureRegistryMirrorV2(endpoint string) error {
	configPath, err := c.resolveContainerdConfigPath()
	if err != nil {
		return err
	}

	var config map[string]interface{}
	if _, err := toml.DecodeFile(configPath, &config); err != nil {
		return fmt.Errorf("failed to parse containerd config: %w", err)
	}

	// containerd 2.x only reads registry.config_path from the CRI images plugin
	// (io.containerd.cri.v1.images). If it is already declared there, reuse it as
	// is. Otherwise reuse a path declared under the legacy v1 plugin (to keep a
	// single host-config directory on migrated systems) or fall back to the
	// default, and record it under the images plugin so containerd 2.x honors it.
	registryConfigPath, found := lookupRegistryConfigPath(config, criImagesPluginV2)
	if !found {
		if v1Path, v1Found := lookupRegistryConfigPath(config, criPluginV1); v1Found {
			registryConfigPath = v1Path
		} else {
			registryConfigPath = defaultRegistryConfigPath
		}
		setRegistryConfigPath(config, registryConfigPath)

		buf := new(bytes.Buffer)
		if err := toml.NewEncoder(buf).Encode(config); err != nil {
			return fmt.Errorf("failed to encode containerd config: %w", err)
		}
		if _, err := c.writeRestartSensitiveFile(configPath, buf.Bytes()); err != nil {
			return fmt.Errorf("failed to write containerd config: %w", err)
		}
		klog.Infof("Set containerd registry config_path to %s in %s", registryConfigPath, configPath)
	}

	if err := c.writeRegistryHostsConfig(registryConfigPath, endpoint); err != nil {
		return err
	}

	klog.Infof("Successfully configured containerd registry mirror for %s via %s", mirrorRegistryHost, endpoint)
	return nil
}

// lookupRegistryConfigPath returns the registry.config_path declared under the
// given CRI plugin, if present and non-empty.
func lookupRegistryConfigPath(config map[string]interface{}, plugin string) (string, bool) {
	plugins, ok := config["plugins"].(map[string]interface{})
	if !ok {
		return "", false
	}
	criPlugin, ok := plugins[plugin].(map[string]interface{})
	if !ok {
		return "", false
	}
	registry, ok := criPlugin["registry"].(map[string]interface{})
	if !ok {
		return "", false
	}
	if cp, ok := registry["config_path"].(string); ok && cp != "" {
		return cp, true
	}
	return "", false
}

// setRegistryConfigPath records registry.config_path under the containerd 2.x
// CRI images plugin, creating intermediate tables as needed.
func setRegistryConfigPath(config map[string]interface{}, path string) {
	plugins, ok := config["plugins"].(map[string]interface{})
	if !ok {
		plugins = make(map[string]interface{})
		config["plugins"] = plugins
	}
	criPlugin, ok := plugins[criImagesPluginV2].(map[string]interface{})
	if !ok {
		criPlugin = make(map[string]interface{})
		plugins[criImagesPluginV2] = criPlugin
	}
	registry, ok := criPlugin["registry"].(map[string]interface{})
	if !ok {
		registry = make(map[string]interface{})
		criPlugin["registry"] = registry
	}
	registry["config_path"] = path
}

// hostsConfig models a containerd registry hosts.toml host-config file.
type hostsConfig struct {
	Server string               `toml:"server"`
	Host   map[string]hostEntry `toml:"host"`
}

// hostEntry models a single mirror host entry within a hosts.toml file.
type hostEntry struct {
	Capabilities []string `toml:"capabilities"`
	SkipVerify   bool     `toml:"skip_verify"`
}

// writeRegistryHostsConfig writes a containerd hosts.toml host-config file that
// mirrors the upstream registry through the provided endpoint.
func (c *ConfigureContainerd) writeRegistryHostsConfig(registryConfigPath, endpoint string) error {
	hostsDir := filepath.Join(c.rootFS, registryConfigPath, mirrorRegistryHost)
	if err := os.MkdirAll(hostsDir, 0755); err != nil {
		return fmt.Errorf("failed to create registry host config dir %s: %w", hostsDir, err)
	}

	hostEndpoint := endpoint
	if !strings.Contains(hostEndpoint, "://") {
		hostEndpoint = "https://" + hostEndpoint
	}

	cfg := hostsConfig{
		Server: "https://" + mirrorRegistryHost,
		Host: map[string]hostEntry{
			hostEndpoint: {
				Capabilities: []string{"pull", "resolve"},
				SkipVerify:   false,
			},
		},
	}

	buf := new(bytes.Buffer)
	if err := toml.NewEncoder(buf).Encode(cfg); err != nil {
		return fmt.Errorf("failed to encode registry host config: %w", err)
	}

	hostsFile := filepath.Join(hostsDir, "hosts.toml")
	if err := os.WriteFile(hostsFile, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write registry host config %s: %w", hostsFile, err)
	}

	klog.Infof("Wrote containerd registry host config %s", hostsFile)
	return nil
}

func (c *ConfigureContainerd) ensureTLSCompatibilityDropIn() (bool, error) {
	dropInDir := filepath.Join(c.rootFS, containerdSystemdDropInDir)
	if err := os.MkdirAll(dropInDir, 0755); err != nil {
		return false, fmt.Errorf("failed to create containerd systemd drop-in directory %s: %w", dropInDir, err)
	}

	dropInPath := filepath.Join(dropInDir, containerdTLSDropInFile)
	changed, err := c.writeRestartSensitiveFile(dropInPath, []byte(containerdTLSDropInContent))
	if err != nil {
		return false, fmt.Errorf("failed to write containerd TLS compatibility drop-in %s: %w", dropInPath, err)
	}
	if changed {
		klog.Infof("Configured containerd TLS compatibility drop-in at %s; this manages the complete GODEBUG value", dropInPath)
	}
	return changed, nil
}

func (c *ConfigureContainerd) writeRestartSensitiveFile(path string, content []byte) (bool, error) {
	existingContent, err := os.ReadFile(path)
	if err == nil && bytes.Equal(existingContent, content) {
		return false, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("failed to read restart-sensitive file %s: %w", path, err)
	}

	if err := c.createRestartMarker(); err != nil {
		return false, err
	}
	if err := c.writeFileAtomically(path, content, 0644); err != nil {
		return false, fmt.Errorf("failed to atomically write restart-sensitive file %s: %w", path, err)
	}
	return true, nil
}

func (c *ConfigureContainerd) writeFileAtomically(name string, data []byte, perm os.FileMode) error {
	if c.atomicWrite != nil {
		return c.atomicWrite(name, data, perm)
	}
	return filesystem.AtomicWrite(name, data, perm)
}

func (c *ConfigureContainerd) createRestartMarker() error {
	markerPath := filepath.Join(c.rootFS, containerdRestartMarker)
	if _, err := os.Stat(markerPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to check containerd restart marker %s: %w", markerPath, err)
	}

	markerDir := filepath.Dir(markerPath)
	if err := os.MkdirAll(markerDir, 0755); err != nil {
		return fmt.Errorf("failed to create containerd restart marker directory %s: %w", markerDir, err)
	}
	if err := c.writeFileAtomically(markerPath, nil, 0644); err != nil {
		return fmt.Errorf("failed to create containerd restart marker %s: %w", markerPath, err)
	}
	return nil
}

func (c *ConfigureContainerd) restartMarkerExists() (bool, error) {
	markerPath := filepath.Join(c.rootFS, containerdRestartMarker)
	if _, err := os.Stat(markerPath); err == nil {
		return true, nil
	} else if os.IsNotExist(err) {
		return false, nil
	} else {
		return false, fmt.Errorf("failed to check containerd restart marker %s: %w", markerPath, err)
	}
}

func (c *ConfigureContainerd) removeRestartMarker() error {
	markerPath := filepath.Join(c.rootFS, containerdRestartMarker)
	if err := os.Remove(markerPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove containerd restart marker %s: %w", markerPath, err)
	}
	return nil
}

func (c *ConfigureContainerd) reconcileContainerdService() error {
	restartRequired, err := c.restartMarkerExists()
	if err != nil {
		return err
	}

	if restartRequired {
		if _, stderr, err := c.runBash("systemctl daemon-reload"); err != nil {
			return fmt.Errorf("failed to reload systemd configuration: %w, stderr: %s", err, stderr.String())
		}
		if _, stderr, err := c.runBash("systemctl stop containerd"); err != nil {
			return fmt.Errorf("failed to stop containerd: %w, stderr: %s", err, stderr.String())
		}
	}

	if _, stderr, err := c.runBash("systemctl enable --now containerd"); err != nil {
		return fmt.Errorf("failed to enable and start containerd: %w, stderr: %s", err, stderr.String())
	}

	if restartRequired {
		if err := c.removeRestartMarker(); err != nil {
			return err
		}
	}
	return nil
}

func (c *ConfigureContainerd) resolveContainerdConfigPath() (string, error) {
	if c.rootFS == "" {
		c.rootFS = defaultRootFS
	}

	configDir := filepath.Join(c.rootFS, containerdConfigDir)
	entries, err := os.ReadDir(configDir)
	if err != nil {
		return "", fmt.Errorf("failed to read containerd config dir %s: %w", configDir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".toml" {
			continue
		}
		return filepath.Join(configDir, entry.Name()), nil
	}

	return "", fmt.Errorf("no containerd config file found in %s", configDir)
}

func (c *ConfigureContainerd) containerdVersion() (*semver.Version, error) {
	if c.getContainerdVersion == nil {
		c.getContainerdVersion = getContainerdVersion
	}
	output, err := c.getContainerdVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to get containerd version: %w", err)
	}
	// Output format:
	// 1. containerd github.com/containerd/containerd v1.7.20 8fc6bcff51318944179630522a095cc9dbf9f353
	// 2. containerd github.com/containerd/containerd 1.7.27
	var ver *semver.Version
	for _, field := range strings.Fields(output) {
		ver, err = semver.NewVersion(field)
		if err == nil {
			return ver, nil
		}
	}
	return nil, fmt.Errorf("failed to extract version from output: %s", output)
}

func getContainerdVersion() (string, error) {
	stdout, stderr, err := bash.Run("containerd --version")
	if err != nil {
		return "", fmt.Errorf("failed to get containerd version: %w, stdout: %s, stderr: %s", err, stdout.String(), stderr.String())
	}
	return stdout.String(), nil
}
