// Copyright (c) 2025 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package netconfig

import (
	"github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"
)

// SystemdNetworkdBackend implements Backend interface using systemd-networkd and netplan
type SystemdNetworkdBackend struct{}

// NewSystemdNetworkdBackend creates a new systemd-networkd backend
func NewSystemdNetworkdBackend() Backend {
	return &SystemdNetworkdBackend{}
}

// Name returns the human-readable name of the backend
func (s *SystemdNetworkdBackend) Name() string {
	return string(BackendTypeSystemdNetworkd)
}

// IsAvailable checks if systemd-networkd is available on the system
func (s *SystemdNetworkdBackend) IsAvailable() bool {
	return HasSystemdNetworkd()
}

// ConfigurePFInterfaces configures physical function network interfaces using netplan
// Returns (needsApply, error) where needsApply indicates if changes were made
func (s *SystemdNetworkdBackend) ConfigurePFInterfaces(pciAddress string, portConfigs []PortConfig) (bool, error) {
	// Delegate to existing util function (will be exported in next step)
	return util.ConfigurePFs(pciAddress, portConfigs)
}

// ConfigureBridgeMTU configures the MTU for a bridge interface using netplan
// Returns (needsApply, error) where needsApply indicates if changes were made
func (s *SystemdNetworkdBackend) ConfigureBridgeMTU(bridgeName string, mtu int) (bool, error) {
	// Delegate to existing util function (will be exported in next step)
	// Note: Current implementation hardcodes bridge name to BridgeName constant
	// The bridgeName parameter is kept for interface compatibility
	return util.ConfigureBridgeMTU(mtu)
}

// ApplyConfiguration applies pending configuration changes using netplan
func (s *SystemdNetworkdBackend) ApplyConfiguration() error {
	// Delegate to existing util function (will be exported in next step)
	return util.ApplyNetplan()
}

// GetInterfaceMTU retrieves the current MTU of an interface
func (s *SystemdNetworkdBackend) GetInterfaceMTU(interfaceName string) (int, error) {
	// Already exported in util package
	return util.GetCurrentMTU(interfaceName)
}

// IsDHCPConfigured checks if DHCP is enabled for an interface
func (s *SystemdNetworkdBackend) IsDHCPConfigured(interfaceName string) (bool, error) {
	// Delegate to existing util function (will be exported in next step)
	return util.IsDHCPConfigured(interfaceName)
}
