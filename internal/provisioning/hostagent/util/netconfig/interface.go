// Copyright (c) 2025 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package netconfig

import (
	"github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"
)

// BackendType represents the type of network configuration backend
type BackendType string

const (
	// BackendTypeNetworkManager represents NetworkManager backend (used on RHEL/OpenShift)
	BackendTypeNetworkManager BackendType = "NetworkManager"
	// BackendTypeSystemdNetworkd represents systemd-networkd backend (used on Ubuntu)
	BackendTypeSystemdNetworkd BackendType = "systemd-networkd"
)

// PortConfig is an alias to util.PortConfig for convenience
type PortConfig = util.PortConfig

// Backend defines the interface for network configuration backends
// Implementations include NetworkManager (RHEL/OpenShift) and systemd-networkd/netplan (Ubuntu)
type Backend interface {
	// Name returns the human-readable name of the backend
	Name() string

	// IsAvailable checks if the backend is available on the system
	IsAvailable() bool

	// ConfigurePFInterfaces configures physical function network interfaces
	// Returns (needsApply, error) where needsApply indicates if changes were made
	ConfigurePFInterfaces(pciAddress string, portConfigs []PortConfig) (bool, error)

	// ConfigureBridgeMTU configures the MTU for a bridge interface
	// Returns (needsApply, error) where needsApply indicates if changes were made
	ConfigureBridgeMTU(bridgeName string, mtu int) (bool, error)

	// ApplyConfiguration applies pending configuration changes
	// For NetworkManager, this activates connections
	// For systemd-networkd, this runs netplan apply
	ApplyConfiguration() error

	// GetInterfaceMTU retrieves the current MTU of an interface
	GetInterfaceMTU(interfaceName string) (int, error)

	// IsDHCPConfigured checks if DHCP is enabled for an interface
	IsDHCPConfigured(interfaceName string) (bool, error)
}
