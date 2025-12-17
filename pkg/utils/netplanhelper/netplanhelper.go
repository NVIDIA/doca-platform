/*
Copyright 2025 NVIDIA

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

package netplanhelper

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type NetplanNetwork struct {
	Version   int                        `yaml:"version"`
	Ethernets map[string]NetplanEthernet `yaml:"ethernets,omitempty"`
	Bridges   map[string]NetplanEthernet `yaml:"bridges,omitempty"`
}

type NetplanEthernet struct {
	DHCP4          *bool           `yaml:"dhcp4,omitempty"`
	MTU            *int32          `yaml:"mtu,omitempty"`
	DHCP4Overrides *DHCP4Overrides `yaml:"dhcp4-overrides,omitempty"`
}

type DHCP4Overrides struct {
	UseMTU *bool `yaml:"use-mtu,omitempty"`
}

// NetplanConfig represents the netplan configuration structure
type NetplanConfig struct {
	Network NetplanNetwork `yaml:"network"`
}

func (n NetplanConfig) WriteToFile(filePath string) error {
	// Ensure directory exists
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create netplan directory: %w", err)
	}

	// Marshal to YAML
	data, err := yaml.Marshal(n)
	if err != nil {
		return fmt.Errorf("failed to marshal netplan config: %w", err)
	}

	// Write file with correct permissions (netplan requires 0600)
	if err := os.WriteFile(filePath, data, 0600); err != nil {
		return fmt.Errorf("failed to write netplan file: %w", err)
	}
	return nil
}
