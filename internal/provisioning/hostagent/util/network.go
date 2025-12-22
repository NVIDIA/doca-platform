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
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/vishvananda/netlink"
	"gopkg.in/yaml.v3"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
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
	NetplanConfigFilePrefix = "/etc/netplan/99-dpu"
	BridgeName              = "br-dpu"
	BridgeMTUNetplanFile    = "/etc/netplan/99-br-dpu-interfaces-mtu.yaml"
)

// generateNetplanFilePath creates a unique netplan file path for a DPU device using its serial number
func generateNetplanFilePath(pciHelper *PCIHelper) (string, error) {
	serialNumber, err := pciHelper.SerialNumber()
	if err != nil {
		return "", fmt.Errorf("failed to get device serial number for %s: %w", pciHelper.Path(), err)
	}

	// Sanitize serial number for filename (remove/replace problematic characters)
	sanitized := strings.ReplaceAll(serialNumber, "/", "-")
	sanitized = strings.ReplaceAll(sanitized, "\\", "-")
	sanitized = strings.ReplaceAll(sanitized, " ", "-")
	sanitized = strings.ReplaceAll(sanitized, ":", "-")

	return fmt.Sprintf("%s-%s.yaml", NetplanConfigFilePrefix, sanitized), nil
}

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

// NetplanConfig represents the netplan configuration structure
type NetplanConfig struct {
	Network NetplanNetwork `yaml:"network"`
}

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

// networkctlInfo holds parsed information from networkctl status output
type networkctlInfo struct {
	hasDHCPAddress bool
	networkFile    string
}

// getNetworkctlInfo runs networkctl once and extracts both DHCP status and network file path
func getNetworkctlInfo(interfaceName string) (*networkctlInfo, error) {
	if _, err := exec.LookPath("networkctl"); err != nil {
		return nil, fmt.Errorf("networkctl not available: %w", err)
	}

	cmd := exec.Command("networkctl", "status", interfaceName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to run networkctl status for interface %s: %w", interfaceName, err)
	}

	scanner := bufio.NewScanner(bytes.NewReader(output))
	info := &networkctlInfo{}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// Parse lines with "Key: Value" format
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch key {
		case "Address":
			if strings.Contains(val, "(DHCP4 via") {
				info.hasDHCPAddress = true
			}
		case "DHCP4 Client ID":
			info.hasDHCPAddress = true
		case "Network File":
			if val != "" && val != "n/a" {
				info.networkFile = val
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading networkctl output: %w", err)
	}

	return info, nil
}

// isDHCPConfigured checks if DHCP is configured for the interface.
// It uses a two-tier approach:
// 1. First checks runtime state via networkctl (if DHCP obtained an address, it's definitely configured)
// 2. If no address found, checks the configuration file (DHCP may be configured but no address yet)
func isDHCPConfigured(interfaceName string) (bool, error) {
	// Call networkctl once to get both DHCP status and network file path
	info, err := getNetworkctlInfo(interfaceName)
	if err != nil {
		return false, err
	}

	// If DHCP has obtained an address, definitely configured
	if info.hasDHCPAddress {
		return true, nil
	}

	// If networkctl doesn't provide a network file, systemd-networkd isn't managing this interface
	// This means DHCP is not configured via systemd-networkd
	if info.networkFile == "" {
		return false, nil
	}

	// Parse the network file to check DHCP configuration
	return parseDHCPFromNetworkdConfig(info.networkFile)
}

// parseDHCPFromNetworkdConfig robustly parses DHCP for IPv4 (yes/ipv4) from .network config.
// Handles case-insensitivity and inline comments.
func parseDHCPFromNetworkdConfig(networkFilePath string) (bool, error) {
	f, err := os.Open(networkFilePath)
	if err != nil {
		return false, fmt.Errorf("failed to open systemd-networkd config file %s: %w", networkFilePath, err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			klog.Errorf("failed to close network config file %s: %v", networkFilePath, err)
		}
	}()

	scanner := bufio.NewScanner(f)
	inNetworkSection := false

	for scanner.Scan() {
		line := scanner.Text()
		// Remove inline comments (anything after "#" or ";")
		if idx := strings.IndexAny(line, "#;"); idx != -1 {
			line = line[:idx]
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// Case-insensitive section header compare
		if strings.EqualFold(trimmed, "[Network]") {
			inNetworkSection = true
			continue
		}
		// Leave [Network] if a new section starts
		if strings.HasPrefix(trimmed, "[") {
			inNetworkSection = false
			continue
		}
		// Only process when in [Network] section
		if inNetworkSection {
			parts := strings.SplitN(trimmed, "=", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				val := strings.TrimSpace(parts[1])
				// Case-insensitive key
				if strings.EqualFold(key, "DHCP") {
					// Case-insensitive value logic for "yes" or "ipv4"
					lval := strings.ToLower(val)
					if lval == "yes" || lval == "ipv4" {
						return true, nil
					}
					return false, nil // Found DHCP, but not enabled for IPv4
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("error reading config file %s: %w", networkFilePath, err)
	}
	return false, nil // DHCP setting not found
}

// ConfigureNetplan configures the PF network interfaces and bridge MTU using netplan
func ConfigureNetplan(pciAddress string, portConfigs []PortConfig, controlPlaneMTU int) error {
	// Fail fast if bridge doesn't exist - prevents partial configuration
	if _, err := netlink.LinkByName(BridgeName); err != nil {
		return fmt.Errorf("bridge %s not found - cannot configure network: %w", BridgeName, err)
	}

	// Configure PF network interfaces and check if changes are needed
	pfNeedsApply, err := configurePFs(pciAddress, portConfigs)
	if err != nil {
		return fmt.Errorf("failed to configure PF interfaces: %w", err)
	}

	// Configure bridge MTU using netplan and check if changes are needed
	bridgeNeedsApply, err := configureBridgeMTU(controlPlaneMTU)
	if err != nil {
		return fmt.Errorf("failed to configure bridge MTU: %w", err)
	}

	// Apply netplan configuration if either PF or bridge changes are needed
	if pfNeedsApply || bridgeNeedsApply {
		if err = applyNetplan(); err != nil {
			return fmt.Errorf("failed to apply netplan configuration: %w", err)
		}
	}
	return nil
}

// configurePFs configures the PF network interfaces using netplan
// Returns (needsApply, error) where needsApply indicates if changes are needed
func configurePFs(pciAddress string, portConfigs []PortConfig) (bool, error) {
	pciHelper := NewPCIHelper(pciAddress)
	needApply := false
	config := NetplanConfig{
		Network: NetplanNetwork{
			Version: 2,
		},
	}

	ethernets := make(map[string]NetplanEthernet)
	// Configure each port based on the provided configurations
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

		ethernet := NetplanEthernet{}
		// Check MTU and only configure if different from current state
		if portConfig.MTU != nil {
			ethernet.MTU = portConfig.MTU
			ethernet.DHCP4Overrides = &DHCP4Overrides{UseMTU: ptr.To(false)}
			currentMTU, err := GetCurrentMTU(interfaceName)
			if err != nil {
				return false, fmt.Errorf("failed to get current MTU for %s: %w", interfaceName, err)
			}
			if currentMTU != int(*portConfig.MTU) {
				klog.Infof("%s MTU mismatch (current=%d, desired=%d)", interfaceName, currentMTU, *portConfig.MTU)
				needApply = true
			}
		}

		// Check DHCP and only configure if different from current state
		if portConfig.DHCP != nil {
			ethernet.DHCP4 = portConfig.DHCP
			currentDHCP, err := isDHCPConfigured(interfaceName)
			if err != nil {
				return false, fmt.Errorf("failed to determine DHCP configuration for %s: %w", interfaceName, err)
			}
			if currentDHCP != *portConfig.DHCP {
				klog.Infof("%s DHCP mismatch (current=%v, desired=%v)", interfaceName, currentDHCP, *portConfig.DHCP)
				needApply = true
			}
		}
		ethernets[interfaceName] = ethernet
	}

	if len(ethernets) > 0 {
		config.Network.Ethernets = ethernets
	}

	// Write PF network interfaces netplan configuration file
	netplanFilePath, err := generateNetplanFilePath(pciHelper)
	if err != nil {
		return false, fmt.Errorf("failed to generate netplan file path: %w", err)
	}
	if err = writeNetplanFile(netplanFilePath, &config); err != nil {
		return false, fmt.Errorf("failed to write netplan file: %w", err)
	}

	return needApply, nil
}

// writeNetplanFile writes the netplan configuration to a file
func writeNetplanFile(filePath string, config *NetplanConfig) error {
	// Ensure directory exists
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create netplan directory: %w", err)
	}

	// Marshal to YAML
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal netplan config: %w", err)
	}

	// Write file with correct permissions (netplan requires 0600)
	if err := os.WriteFile(filePath, data, 0600); err != nil {
		return fmt.Errorf("failed to write netplan file: %w", err)
	}

	return nil
}

// applyNetplan applies the netplan configuration
func applyNetplan() error {
	klog.Infof("Executing 'netplan apply'")
	cmd := exec.Command("netplan", "apply")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("netplan apply failed: %w, output: %s", err, string(output))
	}
	return nil
}

// HasNetplan checks if netplan is available on the system
func HasNetplan() bool {
	_, err := exec.LookPath("netplan")
	return err == nil
}

// checkIfBridgeMTUChangeNeeded determines if the bridge and its member interfaces need MTU changes
// Returns (needsChange, error) where needsChange indicates if configuration changes are needed
func checkIfBridgeMTUChangeNeeded(controlPlaneMTU int) (bool, error) {
	// Check current bridge MTU
	currentBridgeMTU, err := GetCurrentMTU(BridgeName)
	if err != nil {
		return false, fmt.Errorf("failed to get current bridge MTU: %w", err)
	}

	// If bridge MTU differs, we need to apply changes
	if currentBridgeMTU != controlPlaneMTU {
		klog.Infof("Bridge %s MTU mismatch (current=%d, desired=%d)", BridgeName, currentBridgeMTU, controlPlaneMTU)
		return true, nil
	}

	// Check all member interface MTUs
	memberNames, err := GetBridgeMembers(BridgeName)
	if err != nil {
		return false, fmt.Errorf("failed to get bridge members for %s: %w", BridgeName, err)
	}

	// Check if any member interface has different MTU
	for _, memberName := range memberNames {
		currentMTU, err := GetCurrentMTU(memberName)
		if err != nil {
			return false, fmt.Errorf("failed to get current MTU for bridge member %s: %w", memberName, err)
		}
		if currentMTU != controlPlaneMTU {
			klog.Infof("Bridge member %s MTU mismatch (current=%d, desired=%d)", memberName, currentMTU, controlPlaneMTU)
			return true, nil
		}
	}

	// No changes needed - all MTUs match desired state
	return false, nil
}

// configureBridgeMTU configures the bridge and its member interfaces MTU using netplan
// Returns (needsApply, error) where needsApply indicates if changes are needed
func configureBridgeMTU(controlPlaneMTU int) (bool, error) {
	// Check if changes are needed first
	needsApply, err := checkIfBridgeMTUChangeNeeded(controlPlaneMTU)
	if err != nil {
		return false, fmt.Errorf("failed to check bridge MTU state: %w", err)
	}

	// Always write the netplan config file for consistency (idempotent operation)
	if err := writeBridgeMTUConfig(controlPlaneMTU); err != nil {
		return false, fmt.Errorf("failed to write bridge MTU config: %w", err)
	}

	return needsApply, nil
}

// writeBridgeMTUConfig writes the bridge and its member interfaces MTU configuration to netplan
func writeBridgeMTUConfig(controlPlaneMTU int) error {
	// Get bridge member interfaces
	memberNames, err := GetBridgeMembers(BridgeName)
	if err != nil {
		return fmt.Errorf("failed to get bridge members for %s: %w", BridgeName, err)
	}

	mtu := int32(controlPlaneMTU)
	config := NetplanConfig{
		Network: NetplanNetwork{
			Version:   2,
			Bridges:   map[string]NetplanEthernet{BridgeName: {MTU: &mtu}},
			Ethernets: make(map[string]NetplanEthernet, len(memberNames)),
		},
	}

	// Configure MTU for all bridge member interfaces
	for _, memberName := range memberNames {
		config.Network.Ethernets[memberName] = NetplanEthernet{MTU: &mtu}
	}

	return writeNetplanFile(BridgeMTUNetplanFile, &config)
}

// EnsureSystemdNetworkdActive validates that systemd-networkd is currently active and available
// Returns nil if systemd-networkd is active, error otherwise
func EnsureSystemdNetworkdActive() error {
	cmd := exec.Command("systemctl", "is-active", "systemd-networkd")
	output, err := cmd.Output()
	if err == nil && strings.TrimSpace(string(output)) == "active" {
		return nil
	}

	if err != nil {
		return fmt.Errorf("failed to check systemd-networkd status: %w", err)
	}

	return fmt.Errorf("systemd-networkd is not active (status: %s)", strings.TrimSpace(string(output)))
}
