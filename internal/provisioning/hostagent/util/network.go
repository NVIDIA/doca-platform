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

package util

import (
	"fmt"

	"github.com/vishvananda/netlink"
)

// PortConfig holds the configuration for a specific network port
type PortConfig struct {
	// PortNumber identifies the port (0 for P0, 1 for P1, etc.)
	PortNumber int32 `json:"portNumber"`
	// MTU is the MTU value for the port. nil means "no configuration requested, keep current"
	MTU *int32 `json:"mtu,omitempty"`
	// DHCP configuration for the port. nil when no configuration requested
	DHCP *bool `json:"dhcp,omitempty"`
}

const (
	BridgeName = "br-dpu"
)

func AddVFToBridge(vfName, bridgeName string) error {
	bridge, err := netlink.LinkByName(bridgeName)
	if err != nil {
		return fmt.Errorf("failed to get bridge link: %w", err)
	}
	vf, err := netlink.LinkByName(vfName)
	if err != nil {
		return fmt.Errorf("failed to get VF link: %w", err)
	}
	if err := netlink.LinkSetMaster(vf, bridge); err != nil {
		return fmt.Errorf("failed to set VF link master: %w", err)
	}
	if err := netlink.LinkSetUp(vf); err != nil {
		return fmt.Errorf("failed to set VF link up: %w", err)
	}
	return nil
}

func SetLinkMTU(linkName string, mtu int) error {
	link, err := netlink.LinkByName(linkName)
	if err != nil {
		return fmt.Errorf("failed to get link %s: %w", linkName, err)
	}
	if err := netlink.LinkSetMTU(link, mtu); err != nil {
		return fmt.Errorf("failed to set link %s MTU: %w", linkName, err)
	}
	return nil
}

func CreateBridgeIfNotExists(bridgeName string) (netlink.Link, error) {
	bridge, err := netlink.LinkByName(bridgeName)
	if err == nil {
		return bridge, nil
	}

	if _, ok := err.(netlink.LinkNotFoundError); !ok {
		return nil, fmt.Errorf("failed to get bridge: %w", err)
	}

	if err := netlink.LinkAdd(&netlink.Bridge{
		LinkAttrs: netlink.LinkAttrs{
			Name: bridgeName,
		},
	}); err != nil {
		return nil, fmt.Errorf("failed to create bridge: %w", err)
	}
	return netlink.LinkByName(bridgeName)
}

func RemoveVFFromBridge(vfName string) error {
	vf, err := netlink.LinkByName(vfName)
	if err != nil {
		if _, ok := err.(netlink.LinkNotFoundError); ok {
			return nil
		}
		return fmt.Errorf("failed to get VF link: %w", err)
	}
	return netlink.LinkSetNoMaster(vf)
}

// GetCurrentMTU returns the current MTU of the specified interface
func GetCurrentMTU(interfaceName string) (int, error) {
	link, err := netlink.LinkByName(interfaceName)
	if err != nil {
		return 0, fmt.Errorf("failed to get link %s: %w", interfaceName, err)
	}
	return link.Attrs().MTU, nil
}

// GetBridgeMembers returns the names of all member interfaces of the specified bridge
func GetBridgeMembers(bridgeName string) ([]string, error) {
	bridge, err := netlink.LinkByName(bridgeName)
	if err != nil {
		return nil, fmt.Errorf("failed to get bridge %s: %w", bridgeName, err)
	}

	// Get all links
	links, err := netlink.LinkList()
	if err != nil {
		return nil, fmt.Errorf("failed to list network links: %w", err)
	}

	var members []string
	bridgeIndex := bridge.Attrs().Index

	// Find links that have this bridge as their master
	for _, link := range links {
		if link.Attrs().MasterIndex == bridgeIndex {
			members = append(members, link.Attrs().Name)
		}
	}

	return members, nil
}

// NetworkBackend is an interface for network configuration backends.
// This avoids circular dependency with netconfig package.
type NetworkBackend interface {
	Reset()
	ConfigurePFInterfaces(pciAddress string, portConfigs []PortConfig) (bool, error)
	ConfigureBridgeMTU(bridgeName string, mtu int) (bool, error)
	ApplyConfiguration() error
}

// ConfigureNetwork configures the PF network interfaces and bridge MTU using the provided backend.
// This is the backend-agnostic version that works with both nmstate and systemd-networkd.
func ConfigureNetwork(backend NetworkBackend, pciAddress string, portConfigs []PortConfig, controlPlaneMTU int) error {
	// Reset backend state to prevent stale data from a previous failed cycle
	backend.Reset()

	// Fail fast if bridge doesn't exist - prevents partial configuration
	if _, err := netlink.LinkByName(BridgeName); err != nil {
		return fmt.Errorf("bridge %s not found - cannot configure network: %w", BridgeName, err)
	}

	// Configure PF network interfaces and check if changes are needed
	pfNeedsApply, err := backend.ConfigurePFInterfaces(pciAddress, portConfigs)
	if err != nil {
		return fmt.Errorf("failed to configure PF interfaces: %w", err)
	}

	// Configure bridge MTU and check if changes are needed
	bridgeNeedsApply, err := backend.ConfigureBridgeMTU(BridgeName, controlPlaneMTU)
	if err != nil {
		return fmt.Errorf("failed to configure bridge MTU: %w", err)
	}

	// Apply configuration if either PF or bridge changes are needed
	if pfNeedsApply || bridgeNeedsApply {
		if err = backend.ApplyConfiguration(); err != nil {
			return fmt.Errorf("failed to apply network configuration: %w", err)
		}
	}
	return nil
}
