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

// Package sriovdpconfiginit resolves which SR-IOV device-plugin configuration the plugin
// consumes on a DPU node.
//
// With the new flavor based sriov device API, dpu-agent writes the SF and VF pools it created to
// a generated file on the DPU (/var/lib/dpf/sriovdp/config.json). That file is the
// source of truth. A DPU with a BFB which uses an older version of the dpu-agent (i.e. one DPF release behind) runs
// an agent that does not write it, so a default config included in the chart as a fallback for those nodes.
//
// This is deliberately one-shot: the plugin reads its configuration only at
// startup, and dpu-agent writes the generated file before it starts kubelet, so the file is
// already current by the time this runs. Nothing watches the file afterwards.
package sriovdpconfiginit

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"k8s.io/klog/v2"
)

// Source identifies which configuration was selected.
type Source string

const (
	// SourceGenerated is the configuration dpu-agent wrote for this node.
	SourceGenerated Source = "generated"
	// SourceDefault is the fallback shipped in the chart, used when the agent wrote nothing.
	SourceDefault Source = "default"
)

// Options configures a Resolve call. All paths are required.
type Options struct {
	// GeneratedPath is the file dpu-agent writes, exposed through a hostPath mount.
	GeneratedPath string
	// DefaultPath is the fallback config, mounted read-only from the chart's ConfigMap.
	DefaultPath string
	// ActivePath is where the resolved config is written for the plugin to read.
	ActivePath string
}

// configShape defines the basic shape of the configuration file that the device plugin requires.
// This is a very basic check to ensure the file is a valid config. The fields
// within each resource are left to the plugin to interpret.
type configShape struct {
	ResourceList *[]json.RawMessage `json:"resourceList"`
}

// Resolve selects a configuration and writes it to ActivePath, returning the source it chose.
//
// The generated file wins whenever it is present.  If a generated file does not parse it is a hard error
// and the plugin will not start. Default fallback config is reserved when generated file is absent,
//
//	which is the expected state on a BFB with old dpu-agent.
func Resolve(opts Options) (Source, error) {
	if opts.GeneratedPath == "" || opts.DefaultPath == "" || opts.ActivePath == "" {
		return "", errors.New("generated, default and active paths must all be set")
	}

	generated, err := os.ReadFile(opts.GeneratedPath)
	switch {
	case err == nil:
		if err := validateConfig(generated); err != nil {
			return "", fmt.Errorf("generated config %s is not usable: %w", opts.GeneratedPath, err)
		}
		// Valid empty resourceList (no SFs or VFs defined) is substituted with dummy resource.
		// This matches nothing, but keeps the plugin process running.
		active := toActiveConfig(generated)
		if err := write(opts.ActivePath, active); err != nil {
			return "", err
		}
		klog.InfoS("Using the config dpu-agent generated for this node",
			"source", opts.GeneratedPath, "active", opts.ActivePath, "resources", resourceNames(active))
		return SourceGenerated, nil

	case errors.Is(err, fs.ErrNotExist):
		fallback, err := os.ReadFile(opts.DefaultPath)
		if err != nil {
			return "", fmt.Errorf("failed to read default config %s: %w", opts.DefaultPath, err)
		}
		if err := validateConfig(fallback); err != nil {
			return "", fmt.Errorf("default config %s is not usable: %w", opts.DefaultPath, err)
		}
		if err := write(opts.ActivePath, fallback); err != nil {
			return "", err
		}
		klog.InfoS("No config from dpu-agent on this node, using the chart default. Expected on a DPU whose BFB predates new dpu-agent generated config",
			"missing", opts.GeneratedPath, "source", opts.DefaultPath, "active", opts.ActivePath,
			"resources", resourceNames(fallback))
		return SourceDefault, nil

	default:
		return "", fmt.Errorf("failed to read generated config %s: %w", opts.GeneratedPath, err)
	}
}

// dummyResourceConfig is a config with a selector that matches no devices. sriov-network-device-plugin
// (currently v3.11.0) exits if resourceList is empty; this keeps the plugin process running without
// advertising anything to kubelet.
const dummyResourceConfig = `{
    "resourceList": [{
        "resourceName": "dummyResource",
        "resourcePrefix": "nvidia.com",
        "deviceType": "netDevice",
        "selectors": [{
            "vendors": ["ffff"],
            "pfNames": ["dummyPF"]
        }]
    }]
}`

// validateConfig reports whether data is a device-plugin config. An empty resourceList is
// accepted. dpu-agent writes that when no SF or VF group has a poolName or no SFs or VFs are defined.
func validateConfig(data []byte) error {
	var config configShape
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("not valid JSON: %w", err)
	}
	if config.ResourceList == nil {
		return errors.New("no resourceList key")
	}
	return nil
}

// toActiveConfig returns the config bytes to write to ActivePath. An empty resourceList
// is replaced with dummyResourceConfig so the plugin process stays up.
func toActiveConfig(data []byte) []byte {
	var config configShape
	if err := json.Unmarshal(data, &config); err != nil {
		return data
	}
	if config.ResourceList == nil || len(*config.ResourceList) > 0 {
		return data
	}
	klog.InfoS("Config advertises no pools; writing a placeholder so the device plugin stays up")
	return []byte(dummyResourceConfig)
}

// write replaces path atomically so a plugin reading it can never see a partial file.
func write(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create %s: %w", filepath.Dir(path), err)
	}
	tmp := path + ".tmp"
	defer os.Remove(tmp) //nolint: errcheck
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("failed to replace %s: %w", path, err)
	}
	return nil
}

// resourceNames lists the configured resources for logging, so an operator can tell which
// pools a node came up with. Only reached once validateConfig has passed.
func resourceNames(data []byte) []string {
	var config struct {
		ResourceList []struct {
			ResourcePrefix string `json:"resourcePrefix"`
			ResourceName   string `json:"resourceName"`
		} `json:"resourceList"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return nil
	}
	names := make([]string, 0, len(config.ResourceList))
	for _, r := range config.ResourceList {
		if r.ResourcePrefix == "" {
			names = append(names, r.ResourceName)
			continue
		}
		names = append(names, r.ResourcePrefix+"/"+r.ResourceName)
	}
	return names
}
