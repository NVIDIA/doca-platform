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

package netplan

import (
	"fmt"
	"path/filepath"

	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/filesystem"

	"gopkg.in/yaml.v3"
)

type Network struct {
	Version   int                 `yaml:"version"`
	Renderer  string              `yaml:"renderer,omitempty"`
	Ethernets map[string]Ethernet `yaml:"ethernets,omitempty"`
	Bridges   map[string]Bridge   `yaml:"bridges,omitempty"`
}

type Ethernet struct {
	DHCP4 *bool `yaml:"dhcp4,omitempty"`
	DHCP6 *bool `yaml:"dhcp6,omitempty"`
	// AcceptRA controls whether to accept IPv6 Router Advertisements (SLAAC).
	// When false, the interface will not autoconfigure IPv6 addresses or routes from RAs.
	AcceptRA       *bool          `yaml:"accept-ra,omitempty"`
	MTU            *int32         `yaml:"mtu,omitempty"`
	Addresses      []string       `yaml:"addresses,omitempty"`
	DHCP4Overrides *DHCPOverrides `yaml:"dhcp4-overrides,omitempty"`
	DHCP6Overrides *DHCPOverrides `yaml:"dhcp6-overrides,omitempty"`
	// LinkLocal configures the link-local addresses to bring up.
	// A pointer is used to distinguish between nil (not specified) and empty slice
	// Quote from netplan documentation:
	// * If this field is not defined, the default is to enable only IPv6 link-local addresses
	// * If the field is defined but configured as an empty set, IPv6 link-local addresses are disabled as well as IPv4 link- local addresses
	LinkLocal *[]string `yaml:"link-local,omitempty"`
	Optional  *bool     `yaml:"optional,omitempty"`
}

type Bridge struct {
	Ethernet   `yaml:",inline"`
	Interfaces []string `yaml:"interfaces,omitempty"`
}

type DHCPOverrides struct {
	UseMTU *bool `yaml:"use-mtu,omitempty"`
}

// Config represents the netplan configuration structure
type Config struct {
	Network Network `yaml:"network"`
}

func (n Config) WriteToFile(filePath string) error {
	// Ensure directory exists
	dir := filepath.Dir(filePath)
	if err := filesystem.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create netplan directory: %w", err)
	}

	// Marshal to YAML
	data, err := yaml.Marshal(n)
	if err != nil {
		return fmt.Errorf("failed to marshal netplan config: %w", err)
	}

	// Write file with correct permissions (netplan requires 0600)
	if err := filesystem.AtomicWrite(filePath, data, 0600); err != nil {
		return fmt.Errorf("failed to write netplan file: %w", err)
	}
	return nil
}

func Apply() error {
	stdout, stderr, err := bash.Run("netplan apply")
	if err != nil {
		return fmt.Errorf("failed to run netplan apply. stdout: %s, stderr: %s, err: %w", stdout.String(), stderr.String(), err)
	}
	return nil
}
