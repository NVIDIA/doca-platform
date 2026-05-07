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

// Manage virtual function (VF) MAC addresses.
// It maintains persistent MAC address mappings in a TOML config file (/etc/mellanox/dpf-vf-mac-mapping.toml)
// and handles MAC address assignment for VFs for discovered ECPFs (e.g p0/p1).

// The module provides functions to:
// - Query maximum VF count from /sys/class/net/<uplink>/smart_nic
// - Read and write VF MAC addresses from/to sysfs (/sys/class/net/<uplink>/smart_nic/<vf>/mac)
// - Load and save MAC address mappings from/to config (/etc/mellanox/dpf-vf-mac-mapping.toml)
// - Process VFs to either generate random MAC addresses or assign existing MAC addresses
//   if already present from the config file.

package vfmac

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nvidia/doca-platform/pkg/utils/networkhelper"

	"github.com/BurntSushi/toml"
	"github.com/go-logr/logr"
	"github.com/vishvananda/netlink/nl"
)

const (
	sysfsNetPath = "/sys/class/net"
)

// FileSystem abstracts file and OS operations for testability.
type FileSystem interface {
	Stat(name string) (os.FileInfo, error)
	ReadFile(name string) ([]byte, error)
	WriteFile(name string, data []byte, perm os.FileMode) error
	Open(name string) (*os.File, error)
	MkdirAll(path string, perm os.FileMode) error
	ReadDir(name string) ([]os.DirEntry, error)
}

// OSFileSystem is the default implementation of FileSystem using the os package.
type OSFileSystem struct{}

func (OSFileSystem) Stat(name string) (os.FileInfo, error) { return os.Stat(name) }
func (OSFileSystem) ReadFile(name string) ([]byte, error)  { return os.ReadFile(name) }
func (OSFileSystem) WriteFile(name string, data []byte, perm os.FileMode) error {
	return os.WriteFile(name, data, perm)
}
func (OSFileSystem) Open(name string) (*os.File, error)           { return os.Open(name) }
func (OSFileSystem) MkdirAll(path string, perm os.FileMode) error { return os.MkdirAll(path, perm) }
func (OSFileSystem) ReadDir(name string) ([]os.DirEntry, error)   { return os.ReadDir(name) }

// getEnv returns the value of the environment variable or fallback if not set.
func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// VFMAC manages virtual function (VF) MAC addresses.
type VFMAC struct {
	fs         FileSystem
	configDir  string
	configFile string
	ecpfs      []string
	log        logr.Logger
}

// NewVFMAC creates a new VFMAC instance with the given configuration.
// allowedPCIAddresses restricts discovery to physical devlink ports owned by
// those PCI devices. An empty allow-list allows all ports.
func NewVFMAC(fs FileSystem, networkhelper networkhelper.NetworkHelper, log logr.Logger, configDir, configFile string, allowedPCIAddresses []string) (*VFMAC, error) {
	if fs == nil {
		fs = OSFileSystem{}
	}
	if configDir == "" {
		configDir = getEnv("VFMAC_CONFIG_DIR", "/etc/mellanox")
	}
	if configFile == "" {
		configFile = getEnv("VFMAC_CONFIG_FILE", "dpf-vf-mac-mapping.toml")
	}

	// get ecpfs
	ecpfs, err := discoverECPFs(networkhelper, fs, log, allowedPCIAddresses)
	if err != nil {
		return nil, fmt.Errorf("failed to discover ECPFs: %w", err)
	}

	return &VFMAC{
		fs:         fs,
		configDir:  configDir,
		configFile: configFile,
		ecpfs:      ecpfs,
		log:        log,
	}, nil
}

// discoverECPFs discovers all ECPFs in the system. returns a list of netdevs of those ECPFs.
func discoverECPFs(networkhelper networkhelper.NetworkHelper, fs FileSystem, log logr.Logger, allowedPCIAddresses []string) ([]string, error) {
	ports, err := networkhelper.DevlinkPortList()
	if err != nil {
		return nil, fmt.Errorf("failed to list devlink ports: %w", err)
	}
	allowed := allowedPCIAddressSet(allowedPCIAddresses)
	if len(allowed) > 0 {
		log.Info("Filtering ECPFs by PCI allowlist", "allowedPCIAddresses", allowedPCIAddresses)
	}

	//nolint:prealloc
	var ecpfs []string
	for _, port := range ports {
		if port.PortFlavour != nl.DEVLINK_PORT_FLAVOUR_PHYSICAL {
			continue
		}
		if len(allowed) > 0 {
			pciAddress, ok, err := isAllowed(fs, port.NetdeviceName, allowed)
			if err != nil {
				return nil, err
			}
			if !ok {
				log.Info("Skipping ECPF candidate outside PCI allowlist", "netdev", port.NetdeviceName, "pciAddress", pciAddress)
				continue
			}
			log.Info("ECPF candidate matched PCI allowlist", "netdev", port.NetdeviceName, "pciAddress", pciAddress)
		}

		// check if smart_nic dir exists. ATM we don't have a better way to check if this device is an ECPF.
		smartNicPath := filepath.Join(sysfsNetPath, port.NetdeviceName, "smart_nic")
		if _, err := fs.Stat(smartNicPath); err != nil {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("failed to stat smart_nic directory (%s): %w", smartNicPath, err)
			}
			log.Info("Skipping ECPF candidate without smart_nic directory", "netdev", port.NetdeviceName, "path", smartNicPath)
			continue
		}

		ecpfs = append(ecpfs, port.NetdeviceName)
	}
	log.Info("Discovered ECPFs", "ecpfs", ecpfs)

	return ecpfs, nil
}

func isAllowed(fs FileSystem, netdev string, allowed map[string]struct{}) (string, bool, error) {
	pciAddress, err := netdevPCI(fs, netdev)
	if err != nil {
		return "", false, err
	}
	if pciAddress == "" {
		return "", false, nil
	}
	_, ok := allowed[pciAddress]
	return pciAddress, ok, nil
}

func netdevPCI(fs FileSystem, netdev string) (string, error) {
	ueventPath := filepath.Join(sysfsNetPath, netdev, "device", "uevent")
	data, err := fs.ReadFile(ueventPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to read uevent for netdev %s: %w", netdev, err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, found := strings.Cut(line, "=")
		if !found || key != "PCI_SLOT_NAME" {
			continue
		}
		return normalizePCIAddress(value), nil
	}
	return "", nil
}

func allowedPCIAddressSet(addresses []string) map[string]struct{} {
	if len(addresses) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(addresses))
	for _, address := range addresses {
		address = normalizePCIAddress(address)
		if address == "" {
			continue
		}
		out[address] = struct{}{}
	}
	return out
}

func normalizePCIAddress(address string) string {
	address = strings.ToLower(strings.TrimSpace(address))
	parts := strings.Split(address, ":")
	if len(parts) == 2 {
		return "0000:" + address
	}
	return address
}

// getMaxVFs queries the maximum number of VFs from /sys/class/net/<uplink>/smart_nic.
func (v *VFMAC) getMaxVFs(ecpf string) (int, error) {
	v.log.Info("Getting max number of VFs from", "path", fmt.Sprintf("%s/%s/smart_nic", sysfsNetPath, ecpf))
	count, err := v.countVFFolders(ecpf)
	if err != nil {
		return 0, fmt.Errorf("failed to count VF folders: %w", err)
	}
	v.log.Info("Max number of VFs", "count", count)
	return count, nil
}

// VFConfig holds the MAC address for a VF.
type VFConfig struct {
	MAC string `toml:"mac"`
}

// ECPFConfig holds VFConfig for VFs and PF of a specific ECPF keyed by VF+index/PF e.g "vf0", "vf1", "pf".
type ECPFConfig map[string]VFConfig

// VFMapping holds ECPFConfigs keyed by ECPF netdev name.
type VFMapping map[string]ECPFConfig

// getVFConfig reads the VF config from sysfs for a given ECPF and VF.
func (v *VFMAC) getVFConfig(ecpf, vf string) (VFConfig, error) {
	macAddr, err := v.getIfaceMACConfig(ecpf, vf)
	if err != nil {
		return VFConfig{}, fmt.Errorf("failed to get MAC address for %s/%s: %w", ecpf, vf, err)
	}
	v.log.Info("MAC address for", "ecpf", ecpf, "vf", vf, "mac", macAddr)
	return VFConfig{MAC: macAddr}, nil
}

// isValidMAC validates a MAC address string.
func isValidMAC(mac string) bool {
	_, err := net.ParseMAC(mac)
	return err == nil
}

// setVFMAC sets the MAC address for a VF in sysfs.
func (v *VFMAC) setVFMAC(ecpf, vf, mac string) error {
	v.log.Info("Setting MAC for", "ecpf", ecpf, "vf", vf, "mac", mac)

	// Validate VF name format
	if !strings.HasPrefix(vf, "vf") {
		return fmt.Errorf("invalid VF name: %s (must start with 'vf')", vf)
	}
	if _, err := strconv.Atoi(strings.TrimPrefix(vf, "vf")); err != nil {
		return fmt.Errorf("invalid VF name: %s (must be vf followed by a number)", vf)
	}

	if mac != "Random" && !isValidMAC(mac) {
		return fmt.Errorf("invalid MAC address: %s", mac)
	}

	// Check if VF directory exists
	vfPath := filepath.Join(sysfsNetPath, ecpf, "smart_nic", vf)
	if _, err := v.fs.Stat(vfPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("sysfs directory for VF %s not found: %s", vf, vfPath)
		}
		return fmt.Errorf("failed to check VF directory: %w", err)
	}

	macPath := filepath.Join(vfPath, "mac")
	if err := v.fs.WriteFile(macPath, []byte(mac), 0644); err != nil {
		return fmt.Errorf("failed to write MAC address: %w", err)
	}

	return nil
}

// loadConfig loads the VF MAC mapping from the config file.
func (v *VFMAC) loadConfig() (VFMapping, error) {
	v.log.Info("Loading config from", "path", filepath.Join(v.configDir, v.configFile))
	data, err := v.fs.ReadFile(filepath.Join(v.configDir, v.configFile))
	if err != nil {
		if os.IsNotExist(err) {
			v.log.Info("Config file does not exist, creating new mapping")
			mapping := make(VFMapping)
			// Create the config file with empty mappings
			if err := v.saveConfig(mapping); err != nil {
				return nil, fmt.Errorf("failed to create initial config file: %w", err)
			}
			return mapping, nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var mapping VFMapping
	if err := toml.Unmarshal(data, &mapping); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Validate MAC addresses in the loaded config
	for ecpf, ecpfConfig := range mapping {
		for vf, vfConfig := range ecpfConfig {
			if vfConfig.MAC != "" && !isValidMAC(vfConfig.MAC) {
				return nil, fmt.Errorf("invalid MAC address in config for %s/%s: %s", ecpf, vf, vfConfig.MAC)
			}
		}
	}

	v.log.Info("Loaded config")
	for ecpf, ecpfConfig := range mapping {
		v.log.Info("Loaded config for ECPF", "ecpf", ecpf, "VF count", len(ecpfConfig))
	}
	return mapping, nil
}

// getIfaceMACConfig reads the interface config from sysfs for a given vf or pf of an ECPF.
func (v *VFMAC) getIfaceMACConfig(ecpf, iface string) (string, error) {
	configPath := filepath.Join(sysfsNetPath, ecpf, "smart_nic", iface, "config")
	data, err := v.fs.ReadFile(configPath)
	if err != nil {
		return "", fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	macFound := false
	macAddr := ""

	for scanner.Scan() {
		line := scanner.Text()
		// Split only on the first colon to preserve colons in MAC address
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		if key == "MAC" {
			macFound = true
			if value == "" {
				return "", fmt.Errorf("empty MAC found for %s/%s", ecpf, iface)
			}
			if !isValidMAC(value) {
				return "", fmt.Errorf("invalid MAC format for %s/%s: %s", ecpf, iface, value)
			}
			macAddr = value
			v.log.Info("Found MAC for", "ecpf", ecpf, "iface", iface, "mac", value)
			break // We only need the MAC address
		}
	}

	if !macFound {
		return "", fmt.Errorf("no MAC found for %s/%s", ecpf, iface)
	}

	return macAddr, nil
}

// LoadIfaceMACAddressMapping walks sysfs to build a ECPF/VF → MAC mapping.
func (v *VFMAC) LoadIfaceMACAddressMapping() (VFMapping, error) {
	v.log.Info("Loading mac address mapping from", "path", filepath.Join(sysfsNetPath))
	mapping := make(VFMapping)

	// Process MAC addresses
	for _, ecpf := range v.ecpfs {
		mapping[ecpf] = make(ECPFConfig)
		// Process PF MAC address so we have it in mapping (even though when storing the vfmac config file we dont store the PF MAC address).
		if err := v.processMACAddress(mapping, ecpf, "pf"); err != nil {
			return nil, fmt.Errorf("failed to process PF MAC address for %s: %w", ecpf, err)
		}

		// Process VF MAC addresses
		maxVFs, err := v.getMaxVFs(ecpf)
		if err != nil {
			return nil, fmt.Errorf("failed to get max VFs: %w", err)
		}

		for i := range maxVFs {
			vf := fmt.Sprintf("vf%d", i)
			if err := v.processMACAddress(mapping, ecpf, vf); err != nil {
				return nil, err
			}
		}
	}

	for _, ecpf := range v.ecpfs {
		v.log.Info("Loaded mac address mapping for ECPF", "ecpf", ecpf, "VF count", len(mapping[ecpf]))
	}
	return mapping, nil
}

// processMACAddress handles MAC address retrieval and assignment for both PF and VF interfaces
func (v *VFMAC) processMACAddress(mapping VFMapping, ecpf, iface string) error {
	macAddr, err := v.getIfaceMACConfig(ecpf, iface)
	if err != nil {
		return fmt.Errorf("failed to get MAC address for %s/%s: %w", ecpf, iface, err)
	}

	v.log.Info("MAC address for", "ecpf", ecpf, "iface", iface, "mac", macAddr)
	mapping[ecpf][iface] = VFConfig{MAC: macAddr}

	return nil
}

// saveConfig saves the VF MAC mapping to the config file.
func (v *VFMAC) saveConfig(mapping VFMapping) error {
	v.log.Info("Saving config to", "path", filepath.Join(v.configDir, v.configFile))
	// Ensure directory exists
	if err := v.fs.MkdirAll(v.configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	configPath := filepath.Join(v.configDir, v.configFile)
	var buf strings.Builder
	encoder := toml.NewEncoder(&buf)
	if err := encoder.Encode(mapping); err != nil {
		return fmt.Errorf("failed to encode TOML: %w", err)
	}

	v.log.Info("Config saved successfully")
	return v.fs.WriteFile(configPath, []byte(buf.String()), 0644)
}

// ProcessVFs processes all virtual functions (VFs) for configured physical interfaces.
func (v *VFMAC) ProcessVFs() error {
	v.log.Info("Starting VF processing for ECPFS", "ecpfs", v.ecpfs)

	mapping, err := v.loadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	for _, ecpf := range v.ecpfs {
		v.log.Info("Processing ecpf interface", "ecpf", ecpf)

		if mapping[ecpf] == nil {
			mapping[ecpf] = make(ECPFConfig)
		}

		maxVFs, err := v.getMaxVFs(ecpf)
		if err != nil {
			return fmt.Errorf("failed to get max VFs for ecpf %s: %w", ecpf, err)
		}

		vfMap := mapping[ecpf]
		for i := range maxVFs {
			vf := fmt.Sprintf("vf%d", i)
			vfPath := filepath.Join(sysfsNetPath, ecpf, "smart_nic", vf)

			// Check if VF exists
			if _, err := v.fs.Stat(vfPath); err != nil {
				if os.IsNotExist(err) {
					v.log.Info("VF does not exist, skipping", "ecpf", ecpf, "vf", vf)
					continue
				}
				return fmt.Errorf("failed to stat %s: %w", vfPath, err)
			}

			v.log.Info("Processing VF", "ecpf", ecpf, "vf", vf)
			vfConfig, exists := vfMap[vf]

			if !exists {
				v.log.Info("No existing MAC found, generating random MAC", "ecpf", ecpf, "vf", vf)
				// Generate random MAC
				if err := v.setVFMAC(ecpf, vf, "Random"); err != nil {
					return fmt.Errorf("failed to set random MAC for %s/%s: %w", ecpf, vf, err)
				}

				// Read the assigned MAC
				time.Sleep(1 * time.Second)
				vfConfig, err = v.getVFConfig(ecpf, vf)
				if err != nil {
					return fmt.Errorf("failed to get VF config for %s/%s: %w", ecpf, vf, err)
				}

				vfMap[vf] = vfConfig
				v.log.Info("Stored new MAC", "ecpf", ecpf, "vf", vf, "mac", vfConfig.MAC)
			} else {
				v.log.Info("Setting existing MAC", "ecpf", ecpf, "vf", vf, "mac", vfConfig.MAC)
				// Set the stored MAC
				if err := v.setVFMAC(ecpf, vf, vfConfig.MAC); err != nil {
					return fmt.Errorf("failed to set MAC for %s/%s: %w", ecpf, vf, err)
				}
			}
		}
	}

	// remove ecpfs from mapping that no longer exist. this can happen if a NIC/DPU changed firmware configuration
	// and now exposes less ECPFs.
	for ecpf := range mapping {
		if !slices.Contains(v.ecpfs, ecpf) {
			v.log.Info("ECPF no longer exists, removing stale entry from mapping", "ecpf", ecpf)
			delete(mapping, ecpf)
		}
	}

	if err := v.saveConfig(mapping); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	v.log.Info("VF mac processing completed successfully")
	return nil
}

// countVFFolders counts the number of VF folders in the specified smart_nic path.
func (v *VFMAC) countVFFolders(ecpf string) (int, error) {
	smartNicPath := filepath.Join(sysfsNetPath, ecpf, "smart_nic")

	// Read directory entries
	entries, err := v.fs.ReadDir(smartNicPath)
	if err != nil {
		return 0, fmt.Errorf("failed to read directory entries: %w", err)
	}

	// Count VF folders
	count := 0
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "vf") {
			count++
		}
	}

	return count, nil
}

// GetVFMacAddress retrieves the MAC address from an ECPF (specified by its netdev name) of a VF interface from the VF MAC mapping.
func (vfMapping VFMapping) GetVFMacAddress(ecpfName string, vfID int) (string, error) {
	if vfID < 0 {
		return "", fmt.Errorf("invalid VF ID: %d (must be >= 0)", vfID)
	}

	// vf key is in the format "vf%d" i.e "vf0", "vf1", "vf2", etc.
	vfKey := fmt.Sprintf("vf%d", vfID)

	ecpfConfig, ok := vfMapping[ecpfName]
	if !ok {
		return "", fmt.Errorf("ECPF %s not found in VF MAC address mapping", ecpfName)
	}

	vfConfig, ok := ecpfConfig[vfKey]
	if !ok {
		return "", fmt.Errorf("VF %s not found in VF MAC address mapping for ecpf %s", vfKey, ecpfName)
	}

	return vfConfig.MAC, nil
}

// GetPFMacAddress retrieves the MAC address from an ECPF (specified by its netdev name) of a PF interface from the VF MAC mapping.
func (vfMapping VFMapping) GetPFMacAddress(ecpfName string) (string, error) {
	// pf key is hardcoded to "pf"
	pfKey := "pf"

	ecpfConfig, ok := vfMapping[ecpfName]
	if !ok {
		return "", fmt.Errorf("ECPF %s not found in VF MAC address mapping", ecpfName)
	}

	vfConfig, ok := ecpfConfig[pfKey]
	if !ok {
		return "", fmt.Errorf("PF not found in VF MAC address mapping for ecpf %s", ecpfName)
	}

	return vfConfig.MAC, nil
}
