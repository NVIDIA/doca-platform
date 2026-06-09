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

package netconfig

import (
	"fmt"

	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"

	"github.com/vishvananda/netlink"
)

// Backend defines the interface for network configuration backends.
// Implementations include NetworkManager (RHEL/OpenShift) and systemd-networkd/netplan (Ubuntu).
type Backend interface {
	// Name returns the human-readable name of the backend.
	Name() string

	// ConfigurePFInterfaces configures physical function network interfaces.
	// Returns (needsApply, error) where needsApply indicates if changes were made.
	ConfigurePFInterfaces(pciAddress string, portConfigs []hostutil.PortConfig) (bool, error)

	// ConfigureBridgeMTU configures the MTU for a bridge and its member interfaces.
	// Returns (needsApply, error) where needsApply indicates if changes were made.
	ConfigureBridgeMTU(bridgeName string, mtu int) (bool, error)

	// ApplyConfiguration applies pending configuration changes.
	ApplyConfiguration() error

	// ResetPendingChanges discards any tracked-but-unapplied state from a
	// previous cycle. Called at the start of each ConfigureNetwork invocation.
	ResetPendingChanges()

	// IsDHCPConfigured checks if DHCP is enabled for an interface.
	IsDHCPConfigured(interfaceName string) (bool, error)

	// EnsureVFsUnmanaged ensures that VF interfaces will not be managed by the
	// network configuration backend. For NetworkManager this writes a udev rule;
	// other backends may no-op.
	EnsureVFsUnmanaged() error
}

// ConfigureNetwork orchestrates PF interface and bridge MTU configuration
// using the provided backend. It is the backend-agnostic replacement for
// the former ConfigureNetplan function.
func ConfigureNetwork(backend Backend, pciAddress string, portConfigs []hostutil.PortConfig, controlPlaneMTU int) error {
	backend.ResetPendingChanges()

	if _, err := netlink.LinkByName(hostutil.BridgeName); err != nil {
		return fmt.Errorf("bridge %s not found - cannot configure network: %w", hostutil.BridgeName, err)
	}

	pfNeedsApply, err := backend.ConfigurePFInterfaces(pciAddress, portConfigs)
	if err != nil {
		return fmt.Errorf("failed to configure PF interfaces: %w", err)
	}

	bridgeNeedsApply, err := backend.ConfigureBridgeMTU(hostutil.BridgeName, controlPlaneMTU)
	if err != nil {
		return fmt.Errorf("failed to configure bridge MTU: %w", err)
	}

	if pfNeedsApply || bridgeNeedsApply {
		if err = backend.ApplyConfiguration(); err != nil {
			return fmt.Errorf("failed to apply network configuration: %w", err)
		}
	}
	return nil
}
