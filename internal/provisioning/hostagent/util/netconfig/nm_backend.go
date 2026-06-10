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
	"errors"
	"fmt"

	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"

	"github.com/godbus/dbus/v5"
	"k8s.io/klog/v2"
)

var errConnectionNotFound = errors.New("no NM connection found")

const (
	nmConnTypeEthernet  = "802-3-ethernet"
	nmConnTypeBridge    = "bridge"
	nmSectionEthernet   = "802-3-ethernet"
	nmPropInterfaceName = "interface-name"
)

// unsafeRoundtripProps lists per-section D-Bus properties that use legacy
// structured array types (e.g. a(ayuay) for ipv6.addresses) which godbus
// deserializes into incompatible Go types, causing Update calls to fail.
// They are stripped before writing settings back to NM's Update method.
// The modern map-based equivalents (address-data, route-data) round-trip
// correctly. See: https://networkmanager.dev/docs/api/latest/nm-dbus-types.html
var unsafeRoundtripProps = map[string][]string{
	"ipv4": {"addresses", "routes"},
	"ipv6": {"addresses", "routes"},
}

// System-level functions, overridable in tests.
var (
	getCurrentMTUFunc    = hostutil.GetCurrentMTU
	getBridgeMembersFunc = hostutil.GetBridgeMembers
	getInterfaceNameFunc = func(pciAddress string, portNumber int) (string, error) {
		return hostutil.NewPCIHelper(pciAddress).PF(portNumber).InterfaceName()
	}
	isVFFunc       = hostutil.IsVF
	setLinkMTUFunc = hostutil.SetLinkMTU
)

// NetworkManagerBackend implements Backend using NetworkManager via D-Bus.
// This type is NOT goroutine-safe; the caller (networkmanager.NetworkManager)
// serializes access via its own mutex.
type NetworkManagerBackend struct {
	client NMClient
	// modifiedConnPaths tracks connections that were changed and need activation.
	modifiedConnPaths []ConnectionPath
}

// NewNetworkManagerBackend creates a new NetworkManager backend with the given client.
func NewNetworkManagerBackend(client NMClient) Backend {
	return &NetworkManagerBackend{client: client}
}

func (n *NetworkManagerBackend) Name() string {
	return "NetworkManager"
}

func (n *NetworkManagerBackend) ResetPendingChanges() {
	n.modifiedConnPaths = nil
}

func (n *NetworkManagerBackend) EnsureVFsUnmanaged() error {
	return ensureNMUnmanagedUdevRule()
}

// ConfigurePFInterfaces configures physical function network interfaces via NM D-Bus.
func (n *NetworkManagerBackend) ConfigurePFInterfaces(pciAddress string, portConfigs []hostutil.PortConfig) (bool, error) {
	needsApply := false
	for _, portConfig := range portConfigs {
		changed, err := n.configureSinglePF(pciAddress, portConfig)
		if err != nil {
			return false, err
		}
		needsApply = needsApply || changed
	}
	return needsApply, nil
}

func (n *NetworkManagerBackend) configureSinglePF(pciAddress string, portConfig hostutil.PortConfig) (bool, error) {
	if portConfig.MTU == nil && portConfig.DHCP == nil {
		klog.V(3).Infof("PF%d: no MTU or DHCP configuration requested, skipping", portConfig.PortNumber)
		return false, nil
	}

	interfaceName, err := getInterfaceNameFunc(pciAddress, int(portConfig.PortNumber))
	if err != nil {
		return false, fmt.Errorf("failed to get PF%d interface name: %w", portConfig.PortNumber, err)
	}

	connPath, err := n.getOrCreateConnectionForInterface(interfaceName, nmConnTypeEthernet)
	if err != nil {
		return false, fmt.Errorf("failed to get/create connection for PF%d (%s): %w", portConfig.PortNumber, interfaceName, err)
	}

	updateSettings := make(ConnectionSettings)

	if err := n.collectMTUDiff(connPath, interfaceName, portConfig.MTU, updateSettings); err != nil {
		return false, err
	}
	if err := n.collectDHCPDiff(interfaceName, portConfig.DHCP, updateSettings); err != nil {
		return false, err
	}

	if len(updateSettings) == 0 {
		return false, nil
	}

	if err := n.mergeAndUpdateConnection(connPath, updateSettings); err != nil {
		return false, fmt.Errorf("failed to update connection for %s: %w", interfaceName, err)
	}
	n.trackModifiedConnection(connPath)
	return true, nil
}

func (n *NetworkManagerBackend) collectMTUDiff(connPath ConnectionPath, interfaceName string, desiredMTU *int32, out ConnectionSettings) error {
	if desiredMTU == nil {
		return nil
	}

	// Check the NM profile MTU first. If the profile already has the desired
	// value, skip — re-activating the connection would bounce the interface
	// and temporarily reset the link MTU to the driver default.
	if profileMTU, err := n.getProfileMTU(connPath); err == nil && profileMTU == uint32(*desiredMTU) {
		klog.V(3).Infof("%s NM profile MTU already %d, skipping", interfaceName, profileMTU)
		return nil
	}

	currentMTU, err := getCurrentMTUFunc(interfaceName)
	if err != nil {
		return fmt.Errorf("failed to get current MTU for %s: %w", interfaceName, err)
	}
	if currentMTU == int(*desiredMTU) {
		return nil
	}
	klog.Infof("%s MTU mismatch (current=%d, desired=%d)", interfaceName, currentMTU, *desiredMTU)
	out[nmSectionEthernet] = map[string]dbus.Variant{
		"mtu": dbus.MakeVariant(uint32(*desiredMTU)),
	}
	return nil
}

func (n *NetworkManagerBackend) getProfileMTU(connPath ConnectionPath) (uint32, error) {
	settings, err := n.client.GetConnectionSettings(connPath)
	if err != nil {
		return 0, err
	}
	ethSection, ok := settings[nmSectionEthernet]
	if !ok {
		return 0, fmt.Errorf("no %s section", nmSectionEthernet)
	}
	mtuVariant, ok := ethSection["mtu"]
	if !ok {
		return 0, fmt.Errorf("no mtu property")
	}
	mtu, ok := mtuVariant.Value().(uint32)
	if !ok {
		return 0, fmt.Errorf("mtu is not uint32")
	}
	return mtu, nil
}

func (n *NetworkManagerBackend) collectDHCPDiff(interfaceName string, desiredDHCP *bool, out ConnectionSettings) error {
	if desiredDHCP == nil {
		return nil
	}
	currentDHCP, err := n.IsDHCPConfigured(interfaceName)
	if err != nil {
		return fmt.Errorf("failed to determine DHCP configuration for %s: %w", interfaceName, err)
	}
	if currentDHCP == *desiredDHCP {
		return nil
	}
	klog.Infof("%s DHCP mismatch (current=%v, desired=%v)", interfaceName, currentDHCP, *desiredDHCP)
	method := "manual"
	if *desiredDHCP {
		method = "auto"
	}
	out["ipv4"] = map[string]dbus.Variant{
		"method": dbus.MakeVariant(method),
	}
	return nil
}

// ConfigureBridgeMTU configures the MTU for a bridge and its member interfaces via NM.
func (n *NetworkManagerBackend) ConfigureBridgeMTU(bridgeName string, mtu int) (bool, error) {
	klog.V(3).Infof("ConfigureBridgeMTU: bridge=%s, desiredMTU=%d", bridgeName, mtu)

	bridgeChanged, err := n.configureBridgeInterfaceMTU(bridgeName, mtu)
	if err != nil {
		return false, err
	}

	membersChanged, err := n.configureBridgeMembersMTU(bridgeName, mtu)
	if err != nil {
		return false, err
	}

	return bridgeChanged || membersChanged, nil
}

func (n *NetworkManagerBackend) configureBridgeInterfaceMTU(bridgeName string, mtu int) (bool, error) {
	currentMTU, err := getCurrentMTUFunc(bridgeName)
	if err != nil {
		return false, fmt.Errorf("failed to get current bridge MTU: %w", err)
	}
	if currentMTU == mtu {
		return false, nil
	}

	klog.Infof("Bridge %s MTU mismatch (current=%d, desired=%d)", bridgeName, currentMTU, mtu)

	connPath, err := n.getOrCreateConnectionForInterface(bridgeName, nmConnTypeBridge)
	if err != nil {
		return false, fmt.Errorf("failed to get/create connection for bridge %s: %w", bridgeName, err)
	}

	settings := ConnectionSettings{
		nmSectionEthernet: map[string]dbus.Variant{
			"mtu": dbus.MakeVariant(uint32(mtu)),
		},
	}
	if err := n.mergeAndUpdateConnection(connPath, settings); err != nil {
		return false, fmt.Errorf("failed to update bridge connection for %s: %w", bridgeName, err)
	}
	n.trackModifiedConnection(connPath)
	return true, nil
}

func (n *NetworkManagerBackend) configureBridgeMembersMTU(bridgeName string, mtu int) (bool, error) {
	memberNames, err := getBridgeMembersFunc(bridgeName)
	if err != nil {
		return false, fmt.Errorf("failed to get bridge members for %s: %w", bridgeName, err)
	}

	needsApply := false
	for _, memberName := range memberNames {
		currentMTU, err := getCurrentMTUFunc(memberName)
		if err != nil {
			return false, fmt.Errorf("failed to get current MTU for member %s: %w", memberName, err)
		}
		if currentMTU == mtu {
			continue
		}

		klog.Infof("Bridge member %s MTU mismatch (current=%d, desired=%d)", memberName, currentMTU, mtu)

		if isVFFunc(memberName) {
			klog.Infof("Member %s is a VF, setting MTU via netlink", memberName)
			if err := setLinkMTUFunc(memberName, mtu); err != nil {
				return false, fmt.Errorf("failed to set MTU for VF %s via netlink: %w", memberName, err)
			}
			continue
		}

		connPath, err := n.getOrCreateConnectionForInterface(memberName, nmConnTypeEthernet)
		if err != nil {
			return false, fmt.Errorf("failed to get/create connection for member %s: %w", memberName, err)
		}

		settings := ConnectionSettings{
			nmSectionEthernet: map[string]dbus.Variant{
				"mtu": dbus.MakeVariant(uint32(mtu)),
			},
			"connection": map[string]dbus.Variant{
				"master":            dbus.MakeVariant(bridgeName),
				"slave-type":        dbus.MakeVariant("bridge"),
				nmPropInterfaceName: dbus.MakeVariant(memberName),
			},
		}
		if err := n.mergeAndUpdateConnection(connPath, settings); err != nil {
			return false, fmt.Errorf("failed to update member %s: %w", memberName, err)
		}
		n.trackModifiedConnection(connPath)
		needsApply = true
	}
	return needsApply, nil
}

// ApplyConfiguration activates all modified connections.
func (n *NetworkManagerBackend) ApplyConfiguration() error {
	klog.Infof("Activating %d modified NetworkManager connections", len(n.modifiedConnPaths))

	var errs []error
	for _, connPath := range n.modifiedConnPaths {
		if err := n.client.ActivateConnection(connPath); err != nil {
			errs = append(errs, fmt.Errorf("failed to activate connection %s: %w", connPath, err))
		}
	}
	n.modifiedConnPaths = nil

	return errors.Join(errs...)
}

// IsDHCPConfigured checks if DHCP is enabled for an interface by inspecting
// its NM connection profile.
func (n *NetworkManagerBackend) IsDHCPConfigured(interfaceName string) (bool, error) {
	settings, err := n.findConnectionSettingsByInterface(interfaceName)
	if err != nil {
		return false, err
	}
	if settings == nil {
		return false, nil
	}
	return extractStringProp(settings["ipv4"], "method") == "auto", nil
}

// findConnectionSettingsByInterface returns the full settings for the first
// connection whose interface-name matches, or nil if none is found.
func (n *NetworkManagerBackend) findConnectionSettingsByInterface(interfaceName string) (ConnectionSettings, error) {
	connections, err := n.client.ListConnections()
	if err != nil {
		return nil, fmt.Errorf("failed to list connections: %w", err)
	}
	for _, connPath := range connections {
		settings, err := n.client.GetConnectionSettings(connPath)
		if err != nil {
			klog.V(3).Infof("Skipping connection %s: GetSettings failed: %v", connPath, err)
			continue
		}
		connSettings, ok := settings["connection"]
		if !ok {
			continue
		}
		if extractStringProp(connSettings, nmPropInterfaceName) == interfaceName {
			return settings, nil
		}
	}
	return nil, nil
}

// getConnectionForInterface finds a connection that manages the given interface.
// Matches by explicit interface-name first, then falls back to connection ID.
func (n *NetworkManagerBackend) getConnectionForInterface(interfaceName string) (ConnectionPath, error) {
	connections, err := n.client.ListConnections()
	if err != nil {
		return "", err
	}

	var matchByID ConnectionPath
	matchByIDFound := false

	for _, connPath := range connections {
		settings, err := n.client.GetConnectionSettings(connPath)
		if err != nil {
			klog.V(3).Infof("Connection %s: GetSettings failed: %v", connPath, err)
			continue
		}

		connSettings, ok := settings["connection"]
		if !ok {
			continue
		}

		connID := extractStringProp(connSettings, "id")
		connIfName := extractStringProp(connSettings, nmPropInterfaceName)

		klog.V(3).Infof("Connection %s: id=%q, interface-name=%q", connPath, connID, connIfName)

		if connIfName == interfaceName {
			return connPath, nil
		}

		if !matchByIDFound && connID == interfaceName {
			matchByID = connPath
			matchByIDFound = true
		}
	}

	if matchByIDFound {
		klog.V(3).Infof("No interface-name match, using ID match at %s for %s", matchByID, interfaceName)
		return matchByID, nil
	}

	return "", fmt.Errorf("%w for interface %s", errConnectionNotFound, interfaceName)
}

// getOrCreateConnectionForInterface finds an existing connection or creates a new one.
func (n *NetworkManagerBackend) getOrCreateConnectionForInterface(interfaceName, connType string) (ConnectionPath, error) {
	connPath, err := n.getConnectionForInterface(interfaceName)
	if err == nil {
		return connPath, nil
	}
	if !errors.Is(err, errConnectionNotFound) {
		return "", fmt.Errorf("failed to look up connection for %s: %w", interfaceName, err)
	}

	klog.Infof("Creating new NM connection %q (type=%s) for interface %s", interfaceName, connType, interfaceName)
	settings := makeConnectionSettings(interfaceName, connType, interfaceName)
	newPath, err := n.client.AddConnection(settings)
	if err != nil {
		return "", fmt.Errorf("failed to create connection for interface %s: %w", interfaceName, err)
	}
	return newPath, nil
}

// mergeAndUpdateConnection reads existing settings, overlays changes, strips
// unsafe round-trip properties, and writes back the merged result.
func (n *NetworkManagerBackend) mergeAndUpdateConnection(connPath ConnectionPath, changes ConnectionSettings) error {
	existing, err := n.client.GetConnectionSettings(connPath)
	if err != nil {
		return fmt.Errorf("failed to read existing settings: %w", err)
	}

	merged := deepCopySettings(existing)
	stripUnsafeRoundtripProps(merged)
	applySettingsOverlay(merged, changes)

	if err := n.client.UpdateConnection(connPath, merged); err != nil {
		return fmt.Errorf("failed to update connection: %w", err)
	}
	return nil
}

func deepCopySettings(src ConnectionSettings) ConnectionSettings {
	dst := make(ConnectionSettings, len(src))
	for section, sectionMap := range src {
		cp := make(map[string]dbus.Variant, len(sectionMap))
		for k, v := range sectionMap {
			cp[k] = v
		}
		dst[section] = cp
	}
	return dst
}

func stripUnsafeRoundtripProps(settings ConnectionSettings) {
	for section, props := range unsafeRoundtripProps {
		sectionMap, ok := settings[section]
		if !ok {
			continue
		}
		for _, prop := range props {
			delete(sectionMap, prop)
		}
	}
}

func applySettingsOverlay(base, overlay ConnectionSettings) {
	for section, props := range overlay {
		if base[section] == nil {
			base[section] = make(map[string]dbus.Variant)
		}
		for key, val := range props {
			base[section][key] = val
		}
	}
}

func (n *NetworkManagerBackend) trackModifiedConnection(connPath ConnectionPath) {
	for _, p := range n.modifiedConnPaths {
		if p == connPath {
			return
		}
	}
	n.modifiedConnPaths = append(n.modifiedConnPaths, connPath)
}

func makeConnectionSettings(connName, connType, interfaceName string) ConnectionSettings {
	settings := ConnectionSettings{
		"connection": {
			"id":          dbus.MakeVariant(connName),
			"type":        dbus.MakeVariant(connType),
			"autoconnect": dbus.MakeVariant(true),
		},
	}

	if interfaceName != "" {
		settings["connection"][nmPropInterfaceName] = dbus.MakeVariant(interfaceName)
	}

	switch connType {
	case nmConnTypeEthernet:
		settings[nmSectionEthernet] = map[string]dbus.Variant{}
		settings["ipv4"] = map[string]dbus.Variant{"method": dbus.MakeVariant("auto")}
		settings["ipv6"] = map[string]dbus.Variant{"method": dbus.MakeVariant("auto")}
	case "bridge":
		settings["bridge"] = map[string]dbus.Variant{}
		settings["ipv4"] = map[string]dbus.Variant{"method": dbus.MakeVariant("disabled")}
		settings["ipv6"] = map[string]dbus.Variant{"method": dbus.MakeVariant("disabled")}
	}

	return settings
}

func extractStringProp(section map[string]dbus.Variant, key string) string {
	v, ok := section[key]
	if !ok {
		return ""
	}
	s, ok := v.Value().(string)
	if !ok {
		return ""
	}
	return s
}
