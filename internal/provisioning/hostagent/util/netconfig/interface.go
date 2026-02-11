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

package netconfig

import (
	"github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"
)

// BackendType represents the type of network configuration backend
type BackendType string

const (
	// BackendTypeNmstate represents nmstate backend (used on RHEL/OpenShift)
	BackendTypeNmstate BackendType = "nmstate"
	// BackendTypeSystemdNetworkd represents systemd-networkd backend (used on Ubuntu)
	BackendTypeSystemdNetworkd BackendType = "systemd-networkd"
)

// PortConfig is an alias to util.PortConfig for convenience
type PortConfig = util.PortConfig

// Backend defines the interface for network configuration backends
// Implementations include nmstate (RHEL/OpenShift) and systemd-networkd/netplan (Ubuntu)
type Backend interface {
	// Name returns the human-readable name of the backend
	Name() string

	// IsAvailable checks if the backend is available on the system
	IsAvailable() bool

	// Reset discards any internal state from a previous configuration cycle.
	Reset()

	// ConfigurePFInterfaces configures physical function network interfaces
	// Returns (needsApply, error) where needsApply indicates if changes were made
	ConfigurePFInterfaces(pciAddress string, portConfigs []PortConfig) (bool, error)

	// ConfigureBridgeMTU configures the MTU for a bridge interface
	// Returns (needsApply, error) where needsApply indicates if changes were made
	ConfigureBridgeMTU(bridgeName string, mtu int) (bool, error)

	// ApplyConfiguration applies pending configuration changes
	// For nmstate, this applies the desired network state via nmstatectl
	// For systemd-networkd, this runs netplan apply
	ApplyConfiguration() error
}
