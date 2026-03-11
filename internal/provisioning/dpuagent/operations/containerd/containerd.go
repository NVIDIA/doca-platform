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

	"github.com/BurntSushi/toml"
	"github.com/Masterminds/semver/v3"
	"k8s.io/klog/v2"
)

const (
	defaultRootFS       = "/"
	containerdConfigDir = "/etc/containerd"
)

type ConfigureContainerd struct {
	rootFS               string
	getContainerdVersion func() (string, error)
	runBash              func(cmd string) (bytes.Buffer, bytes.Buffer, error)
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
		klog.Info("No registry endpoint configured, skipping containerd configuration")
	} else {
		if err := c.configureRegistryMirror(endpoint); err != nil {
			return err
		}
	}

	if _, stderr, err := c.runBash("systemctl enable --now containerd"); err != nil {
		return fmt.Errorf("failed to enable and start containerd: %w, stderr: %s", err, stderr.String())
	}
	klog.Info("containerd enabled and started")
	return nil
}

func (c *ConfigureContainerd) configureRegistryMirror(endpoint string) error {
	configPath, err := c.resolveContainerdConfigPath()
	if err != nil {
		return err
	}

	var config map[string]interface{}
	if _, err := toml.DecodeFile(configPath, &config); err != nil {
		return fmt.Errorf("failed to parse containerd config: %w", err)
	}

	pluginPath := "io.containerd.grpc.v1.cri"
	version, err := c.containerdVersion()
	if err != nil {
		return fmt.Errorf("failed to get containerd version: %w", err)
	}
	if version.GreaterThanEqual(semver.MustParse("2.0.0")) {
		pluginPath = "io.containerd.cri.v1.images"
	}

	plugins, ok := config["plugins"].(map[string]interface{})
	if !ok {
		plugins = make(map[string]interface{})
		config["plugins"] = plugins
	}

	criPlugin, ok := plugins[pluginPath].(map[string]interface{})
	if !ok {
		criPlugin = make(map[string]interface{})
		plugins[pluginPath] = criPlugin
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

	nvcrConfig, ok := configs["nvcr.io"].(map[string]interface{})
	if !ok {
		nvcrConfig = make(map[string]interface{})
		configs["nvcr.io"] = nvcrConfig
	}

	tls, ok := nvcrConfig["tls"].(map[string]interface{})
	if !ok {
		tls = make(map[string]interface{})
		nvcrConfig["tls"] = tls
	}
	tls["insecure_skip_verify"] = true

	mirrors, ok := registry["mirrors"].(map[string]interface{})
	if !ok {
		mirrors = make(map[string]interface{})
		registry["mirrors"] = mirrors
	}

	nvcrMirror, ok := mirrors["nvcr.io"].(map[string]interface{})
	if !ok {
		nvcrMirror = make(map[string]interface{})
		mirrors["nvcr.io"] = nvcrMirror
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

	if err := os.WriteFile(configPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write containerd config: %w", err)
	}

	klog.Infof("Successfully updated containerd configuration at %s", configPath)
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
