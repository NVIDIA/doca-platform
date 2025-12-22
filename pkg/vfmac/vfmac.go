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
// and handles MAC address assignment for VFs on physical interfaces p0 and p1.

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
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nvidia/doca-platform/pkg/utils/networkhelper"

	"github.com/BurntSushi/toml"
)

const (
	sysfsNetPath = "/sys/class/net"

	// p0PCIAddress is the PCI address of the first PF of the DPU
	p0PCIAddress = "0000:03:00.0"
	// p1PCIAddress is the PCI address of the second PF of the DPU
	p1PCIAddress = "0000:03:00.1"
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
	uplinks    []string
	maxVFs     int
}

// NewVFMAC creates a new VFMAC instance with the given configuration.
func NewVFMAC(fs FileSystem, networkhelper networkhelper.NetworkHelper, configDir, configFile string) (*VFMAC, error) {
	if fs == nil {
		fs = OSFileSystem{}
	}
	if configDir == "" {
		configDir = getEnv("VFMAC_CONFIG_DIR", "/etc/mellanox")
	}
	if configFile == "" {
		configFile = getEnv("VFMAC_CONFIG_FILE", "dpf-vf-mac-mapping.toml")
	}

	// get uplinks
	uplinks := []string{}
	p0uplink, err := networkhelper.GetUplinkRepresentor(p0PCIAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to get uplink representor for p0: %w", err)
	}
	uplinks = append(uplinks, p0uplink)

	p1uplink, err := networkhelper.GetUplinkRepresentor(p1PCIAddress)
	if err == nil {
		// p1 might not exist for single port DPUs.
		uplinks = append(uplinks, p1uplink)
	}

	return &VFMAC{
		fs:         fs,
		configDir:  configDir,
		configFile: configFile,
		uplinks:    uplinks,
	}, nil
}

// getMaxVFs queries the maximum number of VFs from /sys/class/net/<uplink>/smart_nic.
func (v *VFMAC) getMaxVFs(pf string) (int, error) {
	log.Printf("[INFO] Getting max number of VFs from path %s/%s/smart_nic", sysfsNetPath, pf)
	count, err := v.countVFFolders(pf)
	if err != nil {
		return 0, fmt.Errorf("failed to count VF folders: %w", err)
	}
	log.Printf("[INFO] Max number of VFs: %d", count)
	return count, nil
}

// VFConfig holds the MAC address for a VF.
type VFConfig struct {
	MAC string `toml:"mac"`
}

// VFMapping holds the mapping of VFs to MAC addresses for both uplinks.
type VFMapping struct {
	P0 map[string]VFConfig `toml:"p0"`
	P1 map[string]VFConfig `toml:"p1"`
}

// getVFConfig reads the VF config from sysfs for a given PF and VF.
func (v *VFMAC) getVFConfig(pf, vf string) (VFConfig, error) {
	macAddr, err := v.getIfaceMACConfig(pf, vf)
	if err != nil {
		return VFConfig{}, fmt.Errorf("failed to get MAC address for %s/%s: %w", pf, vf, err)
	}
	log.Printf("[INFO] MAC address for %s/%s: %s", pf, vf, macAddr)
	return VFConfig{MAC: macAddr}, nil
}

// isValidMAC validates a MAC address string.
func isValidMAC(mac string) bool {
	_, err := net.ParseMAC(mac)
	return err == nil
}

// setVFMAC sets the MAC address for a VF in sysfs.
func (v *VFMAC) setVFMAC(pf, vf, mac string) error {
	log.Printf("[INFO] Setting MAC for %s/%s to %s", pf, vf, mac)

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
	vfPath := filepath.Join(sysfsNetPath, pf, "smart_nic", vf)
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
func (v *VFMAC) loadConfig() (*VFMapping, error) {
	log.Printf("[INFO] Loading config from %s", filepath.Join(v.configDir, v.configFile))
	data, err := v.fs.ReadFile(filepath.Join(v.configDir, v.configFile))
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("[INFO] Config file does not exist, creating new mapping")
			mapping := &VFMapping{
				P0: make(map[string]VFConfig),
				P1: make(map[string]VFConfig),
			}
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

	// Initialize maps if they are nil
	if mapping.P0 == nil {
		mapping.P0 = make(map[string]VFConfig)
	}
	if mapping.P1 == nil {
		mapping.P1 = make(map[string]VFConfig)
	}

	// Validate MAC addresses in the loaded config
	for pf, vfs := range map[string]map[string]VFConfig{"P0": mapping.P0, "P1": mapping.P1} {
		for vf, config := range vfs {
			if config.MAC != "" && !isValidMAC(config.MAC) {
				return nil, fmt.Errorf("invalid MAC address in config for %s/%s: %s", pf, vf, config.MAC)
			}
		}
	}

	log.Printf("[INFO] Loaded config: P0 has %d VFs, P1 has %d VFs", len(mapping.P0), len(mapping.P1))
	return &mapping, nil
}

// getIfaceMACConfig reads the interface config from sysfs for a given vf of pf.
func (v *VFMAC) getIfaceMACConfig(pf, iface string) (string, error) {
	configPath := filepath.Join(sysfsNetPath, pf, "smart_nic", iface, "config")
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
				return "", fmt.Errorf("empty MAC found for %s/%s", pf, iface)
			}
			if !isValidMAC(value) {
				return "", fmt.Errorf("invalid MAC format for %s/%s: %s", pf, iface, value)
			}
			macAddr = value
			log.Printf("[INFO] Found MAC for %s/%s: %s", pf, iface, value)
			break // We only need the MAC address
		}
	}

	if !macFound {
		return "", fmt.Errorf("no MAC found for %s/%s", pf, iface)
	}

	return macAddr, nil
}

// LoadIfaceMACAddressMapping walks sysfs to build a PF/VF → MAC mapping.
func (v *VFMAC) LoadIfaceMACAddressMapping() (*VFMapping, error) {
	log.Printf("[INFO] Loading mac address mapping from %s", filepath.Join(sysfsNetPath))

	mapping := VFMapping{
		P0: make(map[string]VFConfig),
		P1: make(map[string]VFConfig),
	}

	for _, pf := range v.uplinks {
		// Process PF MAC address
		if _, err := v.fs.Stat(filepath.Join(sysfsNetPath, pf, "smart_nic", "pf", "config")); err == nil {
			if err := v.processMACAddress(&mapping, pf, "pf"); err != nil {
				return nil, err
			}
		}

		// Process VF MAC addresses
		maxVFs, err := v.getMaxVFs(pf)
		if err != nil {
			return nil, fmt.Errorf("failed to get max VFs: %w", err)
		}

		for i := range maxVFs {
			vf := fmt.Sprintf("vf%d", i)
			if err := v.processMACAddress(&mapping, pf, vf); err != nil {
				return nil, err
			}
		}
	}

	log.Printf("[INFO] Loaded mac address mapping: P0 has %d Entries, P1 has %d Entries", len(mapping.P0), len(mapping.P1))
	return &mapping, nil
}

// processMACAddress handles MAC address retrieval and assignment for both PF and VF interfaces
func (v *VFMAC) processMACAddress(mapping *VFMapping, pf, iface string) error {
	macAddr, err := v.getIfaceMACConfig(pf, iface)
	if err != nil {
		return fmt.Errorf("failed to get MAC address for %s/%s: %w", pf, iface, err)
	}

	log.Printf("[INFO] MAC address for %s/%s: %s", pf, iface, macAddr)
	if err := v.assignMACToMapping(mapping, pf, iface, macAddr); err != nil {
		return fmt.Errorf("failed to assign MAC address to mapping: %w", err)
	}
	return nil
}

// assignMACToMapping assigns a MAC address to the appropriate PF mapping. returns an error if the pf is not supported.
func (v *VFMAC) assignMACToMapping(mapping *VFMapping, pf, iface, macAddr string) error {
	config := VFConfig{MAC: macAddr}
	idx := v.pfToIndex(pf)
	switch idx {
	case 0:
		mapping.P0[iface] = config
	case 1:
		mapping.P1[iface] = config
	default:
		return fmt.Errorf("unsupported PF(%s): got index %d", pf, idx)
	}
	return nil
}

// saveConfig saves the VF MAC mapping to the config file.
func (v *VFMAC) saveConfig(mapping *VFMapping) error {
	log.Printf("[INFO] Saving config to %s/%s", v.configDir, v.configFile)
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

	log.Printf("[INFO] Config saved successfully")
	return v.fs.WriteFile(configPath, []byte(buf.String()), 0644)
}

// ProcessVFs processes all virtual functions (VFs) for configured physical interfaces.

func (v *VFMAC) ProcessVFs() error {
	vfs, err := v.getMaxVFs(v.uplinks[0])
	if err != nil {
		return fmt.Errorf("failed to get max VFs: %w", err)
	}
	v.maxVFs = vfs

	log.Printf("[INFO] Starting VF processing")

	mapping, err := v.loadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	for idx, pf := range v.uplinks {
		log.Printf("[INFO] Processing physical interface: %s", pf)
		var vfMap map[string]VFConfig
		switch idx {
		case 0:
			vfMap = mapping.P0
		case 1:
			vfMap = mapping.P1
		default:
			continue
		}

		for i := range v.maxVFs {
			vf := fmt.Sprintf("vf%d", i)
			vfPath := filepath.Join(sysfsNetPath, pf, "smart_nic", vf)

			// Check if VF exists
			if _, err := v.fs.Stat(vfPath); err != nil {
				if os.IsNotExist(err) {
					log.Printf("[INFO] VF %s/%s does not exist, skipping", pf, vf)
					continue
				}
				return fmt.Errorf("failed to stat %s: %w", vfPath, err)
			}

			log.Printf("[INFO] Processing VF: %s/%s", pf, vf)
			vfConfig, exists := vfMap[vf]

			if !exists {
				log.Printf("[INFO] No existing MAC found for %s/%s, generating random MAC", pf, vf)
				// Generate random MAC
				if err := v.setVFMAC(pf, vf, "Random"); err != nil {
					return fmt.Errorf("failed to set random MAC for %s/%s: %w", pf, vf, err)
				}

				// Read the assigned MAC
				time.Sleep(1 * time.Second)
				vfConfig, err = v.getVFConfig(pf, vf)
				if err != nil {
					return fmt.Errorf("failed to get VF config for %s/%s: %w", pf, vf, err)
				}

				vfMap[vf] = vfConfig
				log.Printf("[INFO] Stored new MAC %s for %s/%s", vfConfig.MAC, pf, vf)
			} else {
				log.Printf("[INFO] Setting existing MAC %s for %s/%s", vfConfig.MAC, pf, vf)
				// Set the stored MAC
				if err := v.setVFMAC(pf, vf, vfConfig.MAC); err != nil {
					return fmt.Errorf("failed to set MAC for %s/%s: %w", pf, vf, err)
				}
			}
		}
	}

	if err := v.saveConfig(mapping); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	log.Printf("[INFO] VF mac processing completed successfully")
	return nil
}

// countVFFolders counts the number of VF folders in the specified smart_nic path.
func (v *VFMAC) countVFFolders(pf string) (int, error) {
	smartNicPath := filepath.Join(sysfsNetPath, pf, "smart_nic")

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

// pfToIndex returns the index of the provided pf in VFMAC uplink slice
// if not found, -1 is returned.
func (v *VFMAC) pfToIndex(pf string) int {
	for idx, u := range v.uplinks {
		if u == pf {
			return idx
		}
	}
	return -1
}

// GetVFMacAddressFromVFMapping retrieves the MAC address for a VF interface from the VF MAC mapping.
func (vfmapping *VFMapping) GetVFMacAddressFromVFMapping(pfID, vfID int) (string, error) {
	var macAddrMapping map[string]VFConfig
	switch pfID {
	case 0:
		macAddrMapping = vfmapping.P0
	case 1:
		macAddrMapping = vfmapping.P1
	default:
		return "", fmt.Errorf("unsupported PF ID: %d", pfID)
	}

	vfIDStr := fmt.Sprintf("vf%d", vfID)
	if macAddr, ok := macAddrMapping[vfIDStr]; ok {
		return macAddr.MAC, nil
	} else {
		return "", fmt.Errorf("PF ID %d, VF ID %d not found in VF MAC address mapping", pfID, vfID)
	}
}

// GetPFMacAddressFromVFMapping retrieves the MAC address for a PF interface from the MAC address mapping.
func (vfmapping *VFMapping) GetPFMacAddressFromVFMapping(pfID int) (string, error) {
	var macAddrMapping map[string]VFConfig
	switch pfID {
	case 0:
		macAddrMapping = vfmapping.P0
	case 1:
		macAddrMapping = vfmapping.P1
	default:
		return "", fmt.Errorf("unsupported PF ID: %d", pfID)
	}

	if macAddr, ok := macAddrMapping["pf"]; ok {
		return macAddr.MAC, nil
	} else {
		return "", fmt.Errorf("PF ID %d not found in VF MAC address mapping", pfID)
	}
}
