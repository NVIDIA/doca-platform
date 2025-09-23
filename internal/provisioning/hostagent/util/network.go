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
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/vishvananda/netlink"
	"gopkg.in/yaml.v3"
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
	Ethernets map[string]NetplanEthernet `yaml:"ethernets"`
}

type NetplanEthernet struct {
	DHCP4 *bool  `yaml:"dhcp4,omitempty"`
	MTU   *int32 `yaml:"mtu,omitempty"`
}

// getCurrentMTU returns the current MTU of the specified interface
func getCurrentMTU(interfaceName string) (int, error) {
	link, err := netlink.LinkByName(interfaceName)
	if err != nil {
		return 0, fmt.Errorf("failed to get link %s: %w", interfaceName, err)
	}
	return link.Attrs().MTU, nil
}

// isDHCPEnabled checks if DHCP4 is currently enabled on the specified interface
// Returns: (dhcpEnabled, error)
// - (true, nil): DHCP4 is enabled
// - (false, nil): DHCP4 is disabled
// - (false, error): Could not determine DHCP4 status
func isDHCPEnabled(interfaceName string) (bool, error) {
	// Check if networkctl is available
	if _, err := exec.LookPath("networkctl"); err != nil {
		return false, fmt.Errorf("networkctl not available: %w", err)
	}

	cmd := exec.Command("networkctl", "status", interfaceName)
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("failed to run networkctl status for interface %s: %w", interfaceName, err)
	}

	outputStr := string(output)
	if len(strings.TrimSpace(outputStr)) == 0 {
		return false, fmt.Errorf("networkctl returned empty output for interface %s", interfaceName)
	}

	// Look for DHCP4 indicators in networkctl output
	foundInterfaceInfo := false

	for line := range strings.SplitSeq(outputStr, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// Check if we found interface-specific information
		if strings.Contains(trimmed, interfaceName) || strings.Contains(trimmed, "State:") {
			foundInterfaceInfo = true
		}

		// Check for address obtained via DHCP4: "Address: x.x.x.x (DHCP4 via y.y.y.y)"
		if strings.Contains(trimmed, "Address:") && strings.Contains(trimmed, "(DHCP4 via") {
			return true, nil
		}
		// Check for DHCP4 client configuration: "DHCP4 Client ID: xx:xx:xx:xx:xx:xx"
		if strings.Contains(trimmed, "DHCP4 Client ID:") {
			return true, nil
		}
	}

	if !foundInterfaceInfo {
		return false, fmt.Errorf("networkctl output does not contain information for interface %s", interfaceName)
	}

	// If we reach here, we found interface info but no DHCP indicators
	return false, nil
}

// ConfigurePFNetplan configures the PF network interfaces using netplan
func ConfigurePFNetplan(pciAddress string, portConfigs []PortConfig) error {
	pciHelper := NewPCIHelper(pciAddress)
	hasChanges := false
	config := NetplanConfig{
		Network: NetplanNetwork{
			Version:   2,
			Ethernets: make(map[string]NetplanEthernet),
		},
	}

	// Configure each port based on the provided configurations
	for _, portConfig := range portConfigs {
		// Skip if no configuration needed
		if portConfig.MTU == nil && portConfig.DHCP == nil {
			continue
		}

		pf := pciHelper.PF(int(portConfig.PortNumber))
		interfaceName, err := pf.InterfaceName()
		if err != nil {
			return fmt.Errorf("failed to get PF%d interface name: %w", portConfig.PortNumber, err)
		}

		ethernet := NetplanEthernet{}
		needsUpdate := false

		// Check MTU and only configure if different from current state
		if portConfig.MTU != nil {
			currentMTU, err := getCurrentMTU(interfaceName)
			if err != nil {
				// If we can't get current MTU, log warning but proceed with configuration
				// to ensure desired state is applied
				log.Printf("Warning: failed to get current MTU for %s, will apply configuration: %v", interfaceName, err)
				ethernet.MTU = portConfig.MTU
				needsUpdate = true
			} else if currentMTU != int(*portConfig.MTU) {
				ethernet.MTU = portConfig.MTU
				needsUpdate = true
			}
		}

		// Check DHCP and only configure if different from current state
		if portConfig.DHCP != nil {
			currentDHCP, err := isDHCPEnabled(interfaceName)
			if err != nil {
				// If we can't determine current DHCP state, log warning but proceed with configuration
				// to ensure desired state is applied
				log.Printf("Warning: failed to determine DHCP state for %s, will apply configuration: %v", interfaceName, err)
				ethernet.DHCP4 = portConfig.DHCP
				needsUpdate = true
			} else if currentDHCP != *portConfig.DHCP {
				ethernet.DHCP4 = portConfig.DHCP
				needsUpdate = true
			}
		}

		// Only add to config if changes are needed
		if needsUpdate {
			config.Network.Ethernets[interfaceName] = ethernet
			hasChanges = true
		}
	}

	// Only create netplan file and apply if we have changes to make
	if !hasChanges {
		return nil
	}

	// Write netplan configuration file
	netplanFilePath, err := generateNetplanFilePath(pciHelper)
	if err != nil {
		return fmt.Errorf("failed to generate netplan file path: %w", err)
	}
	if err = writeNetplanFile(netplanFilePath, &config); err != nil {
		return fmt.Errorf("failed to write netplan file: %w", err)
	}

	// Apply netplan configuration
	if err = applyNetplan(); err != nil {
		return fmt.Errorf("failed to apply netplan configuration: %w", err)
	}

	return nil
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
	// Check if netplan command exists
	if _, err := exec.LookPath("netplan"); err != nil {
		return fmt.Errorf("netplan command not found: %w", err)
	}

	cmd := exec.Command("netplan", "apply")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("netplan apply failed: %w, output: %s", err, string(output))
	}
	return nil
}

// GetOSType detects the operating system type by reading /etc/os-release
func GetOSType() (string, error) {
	osReleaseFilePath := "/etc/os-release"
	osReleaseFile, err := os.Open(osReleaseFilePath)
	if err != nil {
		return "", fmt.Errorf("failed to open %s: %w", osReleaseFilePath, err)
	}
	defer func() {
		_ = osReleaseFile.Close()
	}()

	scanner := bufio.NewScanner(osReleaseFile)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "ID=") {
			// Extract the value after ID=, removing quotes if present
			osID := strings.TrimPrefix(line, "ID=")
			osID = strings.Trim(osID, `"'`)
			return strings.ToLower(osID), nil
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to scan %s: %w", osReleaseFilePath, err)
	}

	return "", fmt.Errorf("ID field not found in %s", osReleaseFilePath)
}
