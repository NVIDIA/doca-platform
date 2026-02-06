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

package initcontainer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nvidia/doca-platform/internal/nodesriovdeviceplugin/common"

	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
)

// Options contains the configuration options for the init container.
type Options struct {
	// InputPath is the path to the input config file (downward API mounted).
	InputPath string
	// OutputPath is the path to the output config directory.
	OutputPath string
	// DefaultResourcePrefix is the default resource prefix for resources that don't specify one.
	DefaultResourcePrefix string
	// DevicesReadinessTimeout is the timeout for discovering DPUs and waiting for VFs to be ready.
	DevicesReadinessTimeout time.Duration
	// DevicesReadinessPollInterval is the interval for polling for DPUs and VFs to be ready.
	DevicesReadinessPollInterval time.Duration

	// SysFSRoot is the path to the sysfs root directory (default: /sys).
	SysFSRoot string
	// Clock is the clock to use for time-based operations (default: clock.RealClock{}).
	Clock clock.WithTicker
}

// Run executes the init container logic. It reads the input config, discovers
// DPUs on the node, and waits for required PFs to have at least one VF created
// (virtfn0 exists). Once ready, it generates the upstream device plugin config
// using rootDevices with range syntax and writes it to disk.
func Run(ctx context.Context, opts Options) error {
	if opts.SysFSRoot == "" {
		opts.SysFSRoot = "/sys"
	}
	if opts.Clock == nil {
		opts.Clock = clock.RealClock{}
	}

	klog.InfoS("Starting init container",
		"inputPath", opts.InputPath,
		"outputPath", opts.OutputPath,
		"sysFSRoot", opts.SysFSRoot,
		"defaultResourcePrefix", opts.DefaultResourcePrefix)

	inputConfig, err := readInputConfig(opts.DefaultResourcePrefix, opts.InputPath)
	if err != nil {
		return fmt.Errorf("failed to read input config: %w", err)
	}

	klog.InfoS("Read input config", "dpuCount", len(inputConfig))

	// Discover DPUs and wait for required PFs to have VFs ready.
	// Only PFs mentioned in the config are waited for.
	dpuInfoList, err := discoverDPUsAndWaitForReadiness(ctx,
		opts.Clock,
		opts.SysFSRoot,
		inputConfig,
		opts.DevicesReadinessTimeout,
		opts.DevicesReadinessPollInterval)
	if err != nil {
		return fmt.Errorf("failed to discover DPUs and wait for VFs to be ready: %w", err)
	}

	config, err := buildDevicePluginConfig(opts.DefaultResourcePrefix, dpuInfoList)
	if err != nil {
		return fmt.Errorf("failed to build device plugin config: %w", err)
	}

	if err := writeConfig(opts.OutputPath, config); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	klog.InfoS("Init container completed successfully",
		"resourceCount", len(config.ResourceList))

	return nil
}

// readInputConfig reads, parses, and validates the JSON input config from the
// file. It returns an error if the config is empty or invalid.
func readInputConfig(defaultResourcePrefix string, inputPath string) (common.NodeInputConfig, error) {
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read input file %s: %w", inputPath, err)
	}

	var inputConfig common.NodeInputConfig
	if err := json.Unmarshal(data, &inputConfig); err != nil {
		return nil, fmt.Errorf("failed to parse input config JSON: %w", err)
	}
	if len(inputConfig) == 0 {
		// this should never happen, the controller should not schedule the pod in this case
		return nil, fmt.Errorf("input config is empty")
	}

	for serialNumber, resources := range inputConfig {
		if errList := common.ValidateDevicePluginResources(field.NewPath(serialNumber), defaultResourcePrefix, resources); len(errList) > 0 {
			return nil, fmt.Errorf("validation of input config for DPU %s failed: %w", serialNumber, errList.ToAggregate())
		}
	}
	return inputConfig, nil
}

// writeConfig writes the device plugin config to the output directory
func writeConfig(outputPath string, config *DevicePluginConfig) error {
	if err := os.MkdirAll(outputPath, 0755); err != nil {
		return fmt.Errorf("failed to create output directory %s: %w", outputPath, err)
	}
	configPath := filepath.Join(outputPath, "config.json")
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file %s: %w", configPath, err)
	}
	klog.InfoS("Wrote config file", "path", configPath, "config", config)
	return nil
}
