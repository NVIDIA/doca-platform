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
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/netplan"

	"gopkg.in/yaml.v3"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
)

const (
	netplanConfigFilePrefix = "/etc/netplan/99-dpu"
	bridgeMTUNetplanFile    = "/etc/netplan/99-br-dpu-interfaces-mtu.yaml"
)

// SystemdNetworkdBackend implements Backend interface using systemd-networkd and netplan.
type SystemdNetworkdBackend struct{}

// NewSystemdNetworkdBackend creates a new systemd-networkd backend.
func NewSystemdNetworkdBackend() Backend {
	return &SystemdNetworkdBackend{}
}

// Name returns the human-readable name of the backend.
func (s *SystemdNetworkdBackend) Name() string {
	return string(BackendTypeSystemdNetworkd)
}

// IsAvailable checks if systemd-networkd is available on the system.
func (s *SystemdNetworkdBackend) IsAvailable() bool {
	return HasSystemdNetworkd()
}

func (s *SystemdNetworkdBackend) Reset() {}

// ConfigurePFInterfaces configures physical function network interfaces using netplan.
// Returns (needsApply, error) where needsApply indicates if changes were made.
func (s *SystemdNetworkdBackend) ConfigurePFInterfaces(pciAddress string, portConfigs []PortConfig) (bool, error) {
	pciHelper := util.NewPCIHelper(pciAddress)
	needApply := false
	config := netplan.Config{
		Network: netplan.Network{
			Version: 2,
		},
	}

	ethernets := make(map[string]netplan.Ethernet)
	for _, portConfig := range portConfigs {
		if portConfig.MTU == nil && portConfig.DHCP == nil {
			continue
		}

		pf := pciHelper.PF(int(portConfig.PortNumber))
		interfaceName, err := pf.InterfaceName()
		if err != nil {
			return false, fmt.Errorf("failed to get PF%d interface name: %w", portConfig.PortNumber, err)
		}

		ethernet := netplan.Ethernet{}
		if portConfig.MTU != nil {
			ethernet.MTU = portConfig.MTU
			ethernet.DHCP4Overrides = &netplan.DHCP4Overrides{UseMTU: ptr.To(false)}
			currentMTU, err := util.GetCurrentMTU(interfaceName)
			if err != nil {
				return false, fmt.Errorf("failed to get current MTU for %s: %w", interfaceName, err)
			}
			if currentMTU != int(*portConfig.MTU) {
				klog.Infof("%s MTU mismatch (current=%d, desired=%d)", interfaceName, currentMTU, *portConfig.MTU)
				needApply = true
			}
		}

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

	netplanFilePath, err := generateNetplanFilePath(pciHelper)
	if err != nil {
		return false, fmt.Errorf("failed to generate netplan file path: %w", err)
	}
	if err = writeNetplanFile(netplanFilePath, &config); err != nil {
		return false, fmt.Errorf("failed to write netplan file: %w", err)
	}

	return needApply, nil
}

// ConfigureBridgeMTU configures the MTU for a bridge and its member interfaces using netplan.
// Returns (needsApply, error) where needsApply indicates if changes were made.
func (s *SystemdNetworkdBackend) ConfigureBridgeMTU(bridgeName string, mtu int) (bool, error) {
	needsApply, err := checkIfBridgeMTUChangeNeeded(bridgeName, mtu)
	if err != nil {
		return false, fmt.Errorf("failed to check bridge MTU state: %w", err)
	}

	// Always write the netplan config file for consistency (idempotent operation)
	if err := writeBridgeMTUConfig(bridgeName, mtu); err != nil {
		return false, fmt.Errorf("failed to write bridge MTU config: %w", err)
	}

	return needsApply, nil
}

// ApplyConfiguration applies pending configuration changes using netplan.
func (s *SystemdNetworkdBackend) ApplyConfiguration() error {
	klog.Infof("Executing 'netplan apply'")
	cmd := exec.Command("netplan", "apply")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("netplan apply failed: %w, output: %s", err, string(output))
	}
	return nil
}

// --- netplan helpers ---

// generateNetplanFilePath creates a unique netplan file path for a DPU device using its serial number.
func generateNetplanFilePath(pciHelper *util.PCIHelper) (string, error) {
	serialNumber, err := pciHelper.SerialNumber()
	if err != nil {
		return "", fmt.Errorf("failed to get device serial number for %s: %w", pciHelper.Path(), err)
	}

	sanitized := strings.ReplaceAll(serialNumber, "/", "-")
	sanitized = strings.ReplaceAll(sanitized, "\\", "-")
	sanitized = strings.ReplaceAll(sanitized, " ", "-")
	sanitized = strings.ReplaceAll(sanitized, ":", "-")

	return fmt.Sprintf("%s-%s.yaml", netplanConfigFilePrefix, sanitized), nil
}

// writeNetplanFile writes the netplan configuration to a file.
func writeNetplanFile(filePath string, config *netplan.Config) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create netplan directory: %w", err)
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal netplan config: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0600); err != nil {
		return fmt.Errorf("failed to write netplan file: %w", err)
	}

	return nil
}

// checkIfBridgeMTUChangeNeeded determines if the bridge and its member interfaces need MTU changes.
func checkIfBridgeMTUChangeNeeded(bridgeName string, desiredMTU int) (bool, error) {
	currentBridgeMTU, err := util.GetCurrentMTU(bridgeName)
	if err != nil {
		return false, fmt.Errorf("failed to get current bridge MTU: %w", err)
	}

	if currentBridgeMTU != desiredMTU {
		klog.Infof("Bridge %s MTU mismatch (current=%d, desired=%d)", bridgeName, currentBridgeMTU, desiredMTU)
		return true, nil
	}

	memberNames, err := util.GetBridgeMembers(bridgeName)
	if err != nil {
		return false, fmt.Errorf("failed to get bridge members for %s: %w", bridgeName, err)
	}

	for _, memberName := range memberNames {
		currentMTU, err := util.GetCurrentMTU(memberName)
		if err != nil {
			return false, fmt.Errorf("failed to get current MTU for bridge member %s: %w", memberName, err)
		}
		if currentMTU != desiredMTU {
			klog.Infof("Bridge member %s MTU mismatch (current=%d, desired=%d)", memberName, currentMTU, desiredMTU)
			return true, nil
		}
	}

	return false, nil
}

// writeBridgeMTUConfig writes the bridge and its member interfaces MTU configuration to netplan.
func writeBridgeMTUConfig(bridgeName string, controlPlaneMTU int) error {
	memberNames, err := util.GetBridgeMembers(bridgeName)
	if err != nil {
		return fmt.Errorf("failed to get bridge members for %s: %w", bridgeName, err)
	}

	mtu := int32(controlPlaneMTU)
	config := netplan.Config{
		Network: netplan.Network{
			Version:   2,
			Bridges:   map[string]netplan.Bridge{bridgeName: {Ethernet: netplan.Ethernet{MTU: &mtu}}},
			Ethernets: make(map[string]netplan.Ethernet, len(memberNames)),
		},
	}

	for _, memberName := range memberNames {
		config.Network.Ethernets[memberName] = netplan.Ethernet{MTU: &mtu}
	}

	return writeNetplanFile(bridgeMTUNetplanFile, &config)
}

// --- systemd-networkd DHCP helpers ---

// networkctlInfo holds parsed information from networkctl status output.
type networkctlInfo struct {
	hasDHCPAddress bool
	networkFile    string
}

// isDHCPConfigured checks if DHCP is configured for the specified interface.
// It uses a two-tier approach:
// 1. First checks runtime state via networkctl (if DHCP obtained an address, it's definitely configured)
// 2. If no address found, checks the configuration file (DHCP may be configured but no address yet)
func isDHCPConfigured(interfaceName string) (bool, error) {
	info, err := getNetworkctlInfo(interfaceName)
	if err != nil {
		return false, err
	}

	if info.hasDHCPAddress {
		return true, nil
	}

	if info.networkFile == "" {
		return false, nil
	}

	return parseDHCPFromNetworkdConfig(info.networkFile)
}

// getNetworkctlInfo runs networkctl once and extracts both DHCP status and network file path.
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
		if idx := strings.IndexAny(line, "#;"); idx != -1 {
			line = line[:idx]
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if strings.EqualFold(trimmed, "[Network]") {
			inNetworkSection = true
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			inNetworkSection = false
			continue
		}
		if inNetworkSection {
			parts := strings.SplitN(trimmed, "=", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				val := strings.TrimSpace(parts[1])
				if strings.EqualFold(key, "DHCP") {
					lval := strings.ToLower(val)
					if lval == "yes" || lval == "ipv4" {
						return true, nil
					}
					return false, nil
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("error reading config file %s: %w", networkFilePath, err)
	}
	return false, nil
}
