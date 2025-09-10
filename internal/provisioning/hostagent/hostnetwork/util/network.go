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

func CreateVF(pciAddress string, numOfVFs int) error {
	err := NewPCIHelper(pciAddress).SetNumOfVFs(numOfVFs)
	if err != nil {
		return fmt.Errorf("failed to set number of VFs: %w", err)
	}
	return nil
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

// ConfigurePFNetplan configures the PF network interfaces using netplan
func ConfigurePFNetplan(pciAddress string, portConfigs []PortConfig) error {
	pciHelper := NewPCIHelper(pciAddress)
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
		if portConfig.MTU != nil {
			ethernet.MTU = portConfig.MTU
		}
		if portConfig.DHCP != nil {
			ethernet.DHCP4 = portConfig.DHCP
		}
		config.Network.Ethernets[interfaceName] = ethernet
	}

	// Only create netplan file if we have something to configure
	if len(config.Network.Ethernets) == 0 {
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
