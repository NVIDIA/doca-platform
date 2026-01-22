// Copyright (c) 2025 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package netconfig

import (
	"fmt"

	"github.com/godbus/dbus/v5"
	"github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"
	"k8s.io/klog/v2"
)

const (
	// NetworkManager D-Bus service and paths
	nmService       = "org.freedesktop.NetworkManager"
	nmPath          = "/org/freedesktop/NetworkManager"
	nmSettingsPath  = "/org/freedesktop/NetworkManager/Settings"
	nmSettingsIface = "org.freedesktop.NetworkManager.Settings"
	nmConnIface     = "org.freedesktop.NetworkManager.Settings.Connection"
)

// NetworkManagerBackend implements Backend interface using NetworkManager via D-Bus
type NetworkManagerBackend struct {
	conn *dbus.Conn
	// modifiedConnPaths tracks connections modified during configuration
	// so that ApplyConfiguration can activate them regardless of their name
	modifiedConnPaths []dbus.ObjectPath
}

// NewNetworkManagerBackend creates a new NetworkManager backend
func NewNetworkManagerBackend() Backend {
	return &NetworkManagerBackend{}
}

// Name returns the human-readable name of the backend
func (n *NetworkManagerBackend) Name() string {
	return string(BackendTypeNetworkManager)
}

// IsAvailable checks if NetworkManager is available on the system
func (n *NetworkManagerBackend) IsAvailable() bool {
	return HasNetworkManager()
}

// getDBusConn returns a D-Bus system bus connection, creating it if needed
func (n *NetworkManagerBackend) getDBusConn() (*dbus.Conn, error) {
	if n.conn == nil {
		conn, err := dbus.SystemBus()
		if err != nil {
			return nil, fmt.Errorf("failed to connect to system bus: %w", err)
		}
		n.conn = conn
	}
	return n.conn, nil
}

// makeConnectionSettings creates a D-Bus connection settings map
func makeConnectionSettings(connName, connType, interfaceName string) map[string]map[string]dbus.Variant {
	settings := map[string]map[string]dbus.Variant{
		"connection": {
			"id":             dbus.MakeVariant(connName),
			"type":           dbus.MakeVariant(connType),
			"autoconnect":    dbus.MakeVariant(true),
		},
	}

	if interfaceName != "" {
		settings["connection"]["interface-name"] = dbus.MakeVariant(interfaceName)
	}

	// Initialize type-specific settings
	switch connType {
	case "802-3-ethernet":
		settings["802-3-ethernet"] = make(map[string]dbus.Variant)
		settings["ipv4"] = map[string]dbus.Variant{
			"method": dbus.MakeVariant("auto"),
		}
		settings["ipv6"] = map[string]dbus.Variant{
			"method": dbus.MakeVariant("auto"),
		}
	case "bridge":
		settings["bridge"] = make(map[string]dbus.Variant)
		settings["ipv4"] = map[string]dbus.Variant{
			"method": dbus.MakeVariant("disabled"),
		}
		settings["ipv6"] = map[string]dbus.Variant{
			"method": dbus.MakeVariant("disabled"),
		}
	}

	return settings
}

// ConfigurePFInterfaces configures physical function network interfaces using NetworkManager
// Returns (needsApply, error) where needsApply indicates if changes were made
func (n *NetworkManagerBackend) ConfigurePFInterfaces(pciAddress string, portConfigs []PortConfig) (bool, error) {
	pciHelper := util.NewPCIHelper(pciAddress)
	needsApply := false

	for _, portConfig := range portConfigs {
		// Skip if no configuration needed
		if portConfig.MTU == nil && portConfig.DHCP == nil {
			continue
		}

		pf := pciHelper.PF(int(portConfig.PortNumber))
		interfaceName, err := pf.InterfaceName()
		if err != nil {
			return false, fmt.Errorf("failed to get PF%d interface name: %w", portConfig.PortNumber, err)
		}

		// Find or create connection by interface name
		connPath, err := n.getOrCreateConnectionForInterface(interfaceName, "802-3-ethernet")
		if err != nil {
			return false, fmt.Errorf("failed to get/create connection for PF%d (%s): %w", portConfig.PortNumber, interfaceName, err)
		}

		// Build settings with only the properties that need changing.
		// updateConnection will merge these into the full existing settings.
		updateSettings := make(map[string]map[string]dbus.Variant)
		modified := false

		// Configure MTU if specified
		if portConfig.MTU != nil {
			currentMTU, err := util.GetCurrentMTU(interfaceName)
			if err != nil {
				return false, fmt.Errorf("failed to get current MTU for %s: %w", interfaceName, err)
			}

			if currentMTU != int(*portConfig.MTU) {
				klog.Infof("%s MTU mismatch (current=%d, desired=%d)", interfaceName, currentMTU, *portConfig.MTU)
				updateSettings["802-3-ethernet"] = map[string]dbus.Variant{
					"mtu": dbus.MakeVariant(uint32(*portConfig.MTU)),
				}
				modified = true
			}
		}

		// Configure DHCP if specified
		if portConfig.DHCP != nil {
			currentDHCP, err := n.IsDHCPConfigured(interfaceName)
			if err != nil {
				return false, fmt.Errorf("failed to determine DHCP configuration for %s: %w", interfaceName, err)
			}

			if currentDHCP != *portConfig.DHCP {
				klog.Infof("%s DHCP mismatch (current=%v, desired=%v)", interfaceName, currentDHCP, *portConfig.DHCP)
				method := "manual"
				if *portConfig.DHCP {
					method = "auto"
				}
				updateSettings["ipv4"] = map[string]dbus.Variant{
					"method": dbus.MakeVariant(method),
				}
				modified = true
			}
		}

		// Update connection if modified
		if modified {
			if err := n.updateConnection(connPath, updateSettings); err != nil {
				return false, fmt.Errorf("failed to update connection for %s: %w", interfaceName, err)
			}
			n.trackModifiedConnection(connPath)
			needsApply = true
		}
	}

	return needsApply, nil
}

// ConfigureBridgeMTU configures the MTU for a bridge and its member interfaces using NetworkManager.
// The bridge and each member are checked and configured independently -- a member MTU mismatch
// does not trigger bridge reconfiguration and vice versa.
// Returns (needsApply, error) where needsApply indicates if any changes were made
func (n *NetworkManagerBackend) ConfigureBridgeMTU(bridgeName string, mtu int) (bool, error) {
	klog.Infof("ConfigureBridgeMTU: bridge=%s, desiredMTU=%d", bridgeName, mtu)
	needsApply := false

	// Configure bridge MTU if needed
	currentBridgeMTU, err := util.GetCurrentMTU(bridgeName)
	if err != nil {
		return false, fmt.Errorf("failed to get current bridge MTU: %w", err)
	}
	klog.Infof("Bridge %s current MTU: %d", bridgeName, currentBridgeMTU)

	if currentBridgeMTU != mtu {
		klog.Infof("Bridge %s MTU mismatch (current=%d, desired=%d), configuring...", bridgeName, currentBridgeMTU, mtu)

		bridgeConnPath, err := n.getOrCreateConnectionForInterface(bridgeName, "bridge")
		if err != nil {
			return false, fmt.Errorf("failed to get/create connection for bridge %s: %w", bridgeName, err)
		}

		// Build settings with only the properties we need to modify.
		// updateConnection will merge these into the full existing settings.
		bridgeSettings := map[string]map[string]dbus.Variant{
			"bridge": {},
		}

		if err := n.updateConnection(bridgeConnPath, bridgeSettings); err != nil {
			return false, fmt.Errorf("failed to update bridge connection for %s: %w", bridgeName, err)
		}
		n.trackModifiedConnection(bridgeConnPath)
		needsApply = true
		klog.Infof("Bridge %s connection updated successfully", bridgeName)
	} else {
		klog.Infof("Bridge %s MTU already correct (%d), skipping", bridgeName, mtu)
	}

	// Configure each member independently if its MTU doesn't match
	memberNames, err := util.GetBridgeMembers(bridgeName)
	if err != nil {
		return false, fmt.Errorf("failed to get bridge members for %s: %w", bridgeName, err)
	}
	klog.Infof("Bridge %s has %d members: %v", bridgeName, len(memberNames), memberNames)

	for _, memberName := range memberNames {
		currentMTU, err := util.GetCurrentMTU(memberName)
		if err != nil {
			return false, fmt.Errorf("failed to get current MTU for member %s: %w", memberName, err)
		}

		if currentMTU == mtu {
			klog.Infof("Member %s MTU already correct (%d), skipping", memberName, mtu)
			continue
		}

		klog.Infof("Bridge member %s MTU mismatch (current=%d, desired=%d), configuring...", memberName, currentMTU, mtu)

		memberConnPath, err := n.getOrCreateConnectionForInterface(memberName, "802-3-ethernet")
		if err != nil {
			return false, fmt.Errorf("failed to get/create connection for member %s: %w", memberName, err)
		}

		// Build settings with only the properties we need to modify.
		// updateConnection will merge these into the full existing settings.
		memberSettings := map[string]map[string]dbus.Variant{
			"802-3-ethernet": {
				"mtu": dbus.MakeVariant(uint32(mtu)),
			},
			"connection": {
				"master":     dbus.MakeVariant(bridgeName),
				"slave-type": dbus.MakeVariant("bridge"),
			},
		}

		if err := n.updateConnection(memberConnPath, memberSettings); err != nil {
			return false, fmt.Errorf("failed to update member %s: %w", memberName, err)
		}
		n.trackModifiedConnection(memberConnPath)
		needsApply = true
		klog.Infof("Member %s connection updated successfully", memberName)
	}

	klog.Infof("ConfigureBridgeMTU done: bridge=%s, needsApply=%v", bridgeName, needsApply)
	return needsApply, nil
}

// ApplyConfiguration activates connections to apply pending configuration changes
func (n *NetworkManagerBackend) ApplyConfiguration() error {
	klog.Infof("Activating NetworkManager connections")

	for _, connPath := range n.modifiedConnPaths {
		klog.Infof("Activating modified connection %s", connPath)
		if err := n.activateConnection(connPath); err != nil {
			klog.Infof("Failed to activate modified connection %s (may be expected): %v", connPath, err)
		}
	}
	n.modifiedConnPaths = nil

	return nil
}

// GetInterfaceMTU retrieves the current MTU of an interface
func (n *NetworkManagerBackend) GetInterfaceMTU(interfaceName string) (int, error) {
	return util.GetCurrentMTU(interfaceName)
}

// IsDHCPConfigured checks if DHCP is enabled for an interface using NetworkManager
func (n *NetworkManagerBackend) IsDHCPConfigured(interfaceName string) (bool, error) {
	// Try to find a connection for this interface
	connections, err := n.listConnections()
	if err != nil {
		return false, fmt.Errorf("failed to list connections: %w", err)
	}

	conn, err := n.getDBusConn()
	if err != nil {
		return false, err
	}

	// Search for a connection matching this interface
	for _, connPath := range connections {
		connObj := conn.Object(nmService, connPath)

		var settings map[string]map[string]dbus.Variant
		err := connObj.Call(nmConnIface+".GetSettings", 0).Store(&settings)
		if err != nil {
			continue
		}

		// Check if this connection is for our interface
		if connSettings, ok := settings["connection"]; ok {
			if ifnameVariant, ok := connSettings["interface-name"]; ok {
				if ifname, ok := ifnameVariant.Value().(string); ok && ifname == interfaceName {
					// Found the connection, check IPv4 method
					if ipv4Settings, ok := settings["ipv4"]; ok {
						if methodVariant, ok := ipv4Settings["method"]; ok {
							if method, ok := methodVariant.Value().(string); ok {
								return method == "auto", nil
							}
						}
					}
				}
			}
		}
	}

	// No NetworkManager connection found, check if interface has DHCP address
	// This is a fallback for interfaces not managed by NetworkManager
	return false, nil
}

// Helper functions

// getConnectionForInterface finds a connection that manages the given interface.
// It uses a two-tier approach:
// 1. First looks for connections with explicit interface-name property matching (most reliable)
// 2. Falls back to matching by connection ID (name), since some connections (e.g. bridges)
//    may not have interface-name set but are matched by NM via their name
func (n *NetworkManagerBackend) getConnectionForInterface(interfaceName string) (dbus.ObjectPath, error) {
	klog.Infof("Searching for connection managing interface %s", interfaceName)

	connections, err := n.listConnections()
	if err != nil {
		return "", err
	}
	klog.Infof("Found %d total NM connections to search through", len(connections))

	conn, err := n.getDBusConn()
	if err != nil {
		return "", err
	}

	var matchByID dbus.ObjectPath
	matchByIDFound := false

	for _, connPath := range connections {
		connObj := conn.Object(nmService, connPath)

		var settings map[string]map[string]dbus.Variant
		err := connObj.Call(nmConnIface+".GetSettings", 0).Store(&settings)
		if err != nil {
			klog.Infof("  Connection %s: GetSettings failed: %v", connPath, err)
			continue
		}

		connSettings, ok := settings["connection"]
		if !ok {
			klog.Infof("  Connection %s: no 'connection' section in settings", connPath)
			continue
		}

		// Extract connection ID and interface-name for logging
		connID := "<unknown>"
		if idVariant, ok := connSettings["id"]; ok {
			if id, ok := idVariant.Value().(string); ok {
				connID = id
			}
		}
		connIfName := "<not set>"
		if ifnameVariant, ok := connSettings["interface-name"]; ok {
			if ifname, ok := ifnameVariant.Value().(string); ok {
				connIfName = ifname
			}
		}
		klog.Infof("  Connection %s: id=%q, interface-name=%q", connPath, connID, connIfName)

		// Priority 1: match by explicit interface-name property
		if connIfName != "<not set>" && connIfName == interfaceName {
			klog.Infof("  -> Matched by interface-name")
			return connPath, nil
		}

		// Priority 2: remember first match by connection ID (name)
		if !matchByIDFound && connID == interfaceName {
			matchByID = connPath
			matchByIDFound = true
			klog.Infof("  -> Candidate match by connection ID")
		}
	}

	// Return connection matched by ID if no interface-name match was found
	if matchByIDFound {
		klog.Infof("No interface-name match, using ID match at %s for interface %s", matchByID, interfaceName)
		return matchByID, nil
	}

	klog.Infof("No connection found for interface %s (checked %d connections)", interfaceName, len(connections))
	return "", fmt.Errorf("no connection found for interface %s", interfaceName)
}

// getOrCreateConnectionForInterface finds an existing connection for the interface,
// or creates a new one if none exists. Lookups are done by interface name and connection ID.
func (n *NetworkManagerBackend) getOrCreateConnectionForInterface(interfaceName, connType string) (dbus.ObjectPath, error) {
	klog.Infof("getOrCreateConnectionForInterface: interface=%s, type=%s", interfaceName, connType)

	// Try to find existing connection by interface name or connection ID
	connPath, err := n.getConnectionForInterface(interfaceName)
	if err == nil {
		klog.Infof("Found existing connection %s for interface %s", connPath, interfaceName)
		return connPath, nil
	}
	klog.Infof("No existing connection for interface %s: %v", interfaceName, err)

	// No existing connection found, create a new one
	connName := interfaceName
	klog.Infof("Creating new NM connection %q (type=%s) for interface %s", connName, connType, interfaceName)
	if err := n.createConnection(connName, connType, interfaceName); err != nil {
		return "", fmt.Errorf("failed to create connection for interface %s: %w", interfaceName, err)
	}

	// Retrieve the path of the newly created connection
	connPath, err = n.getConnectionForInterface(interfaceName)
	if err != nil {
		return "", fmt.Errorf("created connection for %s but failed to find it: %w", interfaceName, err)
	}
	klog.Infof("Created and found new connection %s for interface %s", connPath, interfaceName)
	return connPath, nil
}

// trackModifiedConnection records a connection path that was modified and needs activation
func (n *NetworkManagerBackend) trackModifiedConnection(connPath dbus.ObjectPath) {
	for _, p := range n.modifiedConnPaths {
		if p == connPath {
			return // already tracked
		}
	}
	n.modifiedConnPaths = append(n.modifiedConnPaths, connPath)
}

// listConnections lists all NetworkManager connections
func (n *NetworkManagerBackend) listConnections() ([]dbus.ObjectPath, error) {
	conn, err := n.getDBusConn()
	if err != nil {
		return nil, err
	}

	obj := conn.Object(nmService, nmSettingsPath)
	var connections []dbus.ObjectPath
	err = obj.Call(nmSettingsIface+".ListConnections", 0).Store(&connections)
	if err != nil {
		return nil, fmt.Errorf("failed to list connections: %w", err)
	}

	return connections, nil
}

// createConnection creates a new connection profile
func (n *NetworkManagerBackend) createConnection(connName, connType, interfaceName string) error {
	conn, err := n.getDBusConn()
	if err != nil {
		return err
	}

	obj := conn.Object(nmService, nmSettingsPath)
	settings := makeConnectionSettings(connName, connType, interfaceName)

	var connPath dbus.ObjectPath
	err = obj.Call(nmSettingsIface+".AddConnection", 0, settings).Store(&connPath)
	if err != nil {
		return fmt.Errorf("failed to add connection: %w", err)
	}

	klog.Infof("Created connection %s at %s", connName, connPath)
	return nil
}

// unsafeRoundtripProps lists per-section properties whose D-Bus types (e.g. struct
// types like a(ayuay) for ipv6.addresses) are not preserved by Go's dbus library
// during deserialization and re-serialization. These deprecated NM properties are
// stripped before sending settings back to NM's Update method. Their modern
// equivalents (address-data, route-data) use map-based D-Bus types (aa{sv}) that
// round-trip correctly through the Go dbus library.
var unsafeRoundtripProps = map[string][]string{
	"ipv4": {"addresses", "routes"},
	"ipv6": {"addresses", "routes"},
}

// updateConnection reads the full existing connection settings, overlays only the
// changed properties from `changes`, and writes back the complete result.
// NM's Update method does a full replacement, so we must pass through ALL existing
// sections (ipv4, ipv6, etc.) to avoid dropping configuration we didn't intend to modify.
// Properties with D-Bus struct types that Go's dbus library cannot round-trip are
// stripped (see unsafeRoundtripProps); their modern equivalents are preserved.
func (n *NetworkManagerBackend) updateConnection(connPath dbus.ObjectPath, changes map[string]map[string]dbus.Variant) error {
	conn, err := n.getDBusConn()
	if err != nil {
		return err
	}

	obj := conn.Object(nmService, connPath)

	// Read ALL existing settings — every section is preserved in the merged result
	var existing map[string]map[string]dbus.Variant
	err = obj.Call(nmConnIface+".GetSettings", 0).Store(&existing)
	if err != nil {
		return fmt.Errorf("failed to read existing settings: %w", err)
	}

	// Start from the full existing settings so unmodified sections (ipv4, ipv6, etc.) are kept
	merged := existing

	// Strip deprecated properties whose D-Bus struct types don't survive the
	// Go dbus Variant round-trip (e.g. ipv6.addresses type a(ayuay) → aav).
	for section, props := range unsafeRoundtripProps {
		if sectionMap, ok := merged[section]; ok {
			for _, prop := range props {
				delete(sectionMap, prop)
			}
		}
	}

	// Overlay only the specific properties we're changing
	for section, props := range changes {
		if merged[section] == nil {
			merged[section] = make(map[string]dbus.Variant)
		}
		for key, val := range props {
			merged[section][key] = val
		}
	}

	klog.Infof("Updating connection %s with sections: %v", connPath, sectionNames(merged))
	err = obj.Call(nmConnIface+".Update", 0, merged).Err
	if err != nil {
		return fmt.Errorf("failed to update connection: %w", err)
	}

	// Reload connections so NetworkManager re-reads the updated profile from disk
	settingsObj := conn.Object(nmService, nmSettingsPath)
	var success bool
	err = settingsObj.Call(nmSettingsIface+".ReloadConnections", 0).Store(&success)
	if err != nil || !success {
		return fmt.Errorf("failed to reload connections after updating %s (success=%t): %w", connPath, success, err)
	}

	return nil
}

// sectionNames returns the keys of a settings map for logging
func sectionNames(settings map[string]map[string]dbus.Variant) []string {
	names := make([]string, 0, len(settings))
	for k := range settings {
		names = append(names, k)
	}
	return names
}

// activateConnection activates a connection
func (n *NetworkManagerBackend) activateConnection(connPath dbus.ObjectPath) error {
	conn, err := n.getDBusConn()
	if err != nil {
		return err
	}

	obj := conn.Object(nmService, nmPath)

	// ActivateConnection(connection, device, specific_object)
	// device and specific_object can be "/" for auto-selection
	var activeConnPath dbus.ObjectPath
	err = obj.Call(nmService+".ActivateConnection", 0, connPath, dbus.ObjectPath("/"), dbus.ObjectPath("/")).Store(&activeConnPath)
	if err != nil {
		return fmt.Errorf("failed to activate connection: %w", err)
	}

	return nil
}
