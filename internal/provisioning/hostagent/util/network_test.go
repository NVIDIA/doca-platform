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
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Network", func() {
	Context("Constants", Label("Constants"), func() {
		It("should have correct NetplanConfigFilePrefix", func() {
			Expect(NetplanConfigFilePrefix).To(Equal("/etc/netplan/99-dpu"))
		})

		It("should have correct BridgeName", func() {
			Expect(BridgeName).To(Equal("br-dpu"))
		})

		It("should have correct BridgeMTUNetplanFile", func() {
			Expect(BridgeMTUNetplanFile).To(Equal("/etc/netplan/99-br-dpu-interfaces-mtu.yaml"))
		})
	})

	Context("parseDHCPFromNetworkdConfig", Label("parseDHCPFromNetworkdConfig"), func() {
		var tempDir string

		BeforeEach(func() {
			var err error
			tempDir, err = os.MkdirTemp("", "network-test-*")
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			Expect(os.RemoveAll(tempDir)).To(Succeed())
		})

		It("should return true when DHCP=yes in [Network] section", func() {
			configPath := filepath.Join(tempDir, "test.network")
			content := `[Match]
Name=eth0

[Network]
DHCP=yes
`
			err := os.WriteFile(configPath, []byte(content), 0644)
			Expect(err).NotTo(HaveOccurred())

			result, err := parseDHCPFromNetworkdConfig(configPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())
		})

		It("should return true when DHCP=ipv4 in [Network] section", func() {
			configPath := filepath.Join(tempDir, "test.network")
			content := `[Network]
DHCP=ipv4
`
			err := os.WriteFile(configPath, []byte(content), 0644)
			Expect(err).NotTo(HaveOccurred())

			result, err := parseDHCPFromNetworkdConfig(configPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())
		})

		It("should handle case-insensitive DHCP values", func() {
			configPath := filepath.Join(tempDir, "test.network")
			content := `[Network]
DHCP=YES
`
			err := os.WriteFile(configPath, []byte(content), 0644)
			Expect(err).NotTo(HaveOccurred())

			result, err := parseDHCPFromNetworkdConfig(configPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())
		})

		It("should handle case-insensitive section headers", func() {
			configPath := filepath.Join(tempDir, "test.network")
			content := `[network]
DHCP=yes
`
			err := os.WriteFile(configPath, []byte(content), 0644)
			Expect(err).NotTo(HaveOccurred())

			result, err := parseDHCPFromNetworkdConfig(configPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())
		})

		It("should return false when DHCP=no", func() {
			configPath := filepath.Join(tempDir, "test.network")
			content := `[Network]
DHCP=no
`
			err := os.WriteFile(configPath, []byte(content), 0644)
			Expect(err).NotTo(HaveOccurred())

			result, err := parseDHCPFromNetworkdConfig(configPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
		})

		It("should return false when DHCP is not set", func() {
			configPath := filepath.Join(tempDir, "test.network")
			content := `[Network]
Address=192.168.1.100/24
`
			err := os.WriteFile(configPath, []byte(content), 0644)
			Expect(err).NotTo(HaveOccurred())

			result, err := parseDHCPFromNetworkdConfig(configPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
		})

		It("should ignore DHCP outside [Network] section", func() {
			configPath := filepath.Join(tempDir, "test.network")
			content := `[Match]
DHCP=yes

[Network]
Address=192.168.1.100/24
`
			err := os.WriteFile(configPath, []byte(content), 0644)
			Expect(err).NotTo(HaveOccurred())

			result, err := parseDHCPFromNetworkdConfig(configPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
		})

		It("should handle inline comments with hash", func() {
			configPath := filepath.Join(tempDir, "test.network")
			content := `[Network]
DHCP=yes # Enable DHCP
`
			err := os.WriteFile(configPath, []byte(content), 0644)
			Expect(err).NotTo(HaveOccurred())

			result, err := parseDHCPFromNetworkdConfig(configPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())
		})

		It("should handle inline comments with semicolon", func() {
			configPath := filepath.Join(tempDir, "test.network")
			content := `[Network]
DHCP=yes ; Enable DHCP
`
			err := os.WriteFile(configPath, []byte(content), 0644)
			Expect(err).NotTo(HaveOccurred())

			result, err := parseDHCPFromNetworkdConfig(configPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())
		})

		It("should stop parsing DHCP when new section starts", func() {
			configPath := filepath.Join(tempDir, "test.network")
			content := `[Network]
Address=192.168.1.100/24

[Route]
DHCP=yes
`
			err := os.WriteFile(configPath, []byte(content), 0644)
			Expect(err).NotTo(HaveOccurred())

			result, err := parseDHCPFromNetworkdConfig(configPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
		})

		It("should return error for non-existent file", func() {
			_, err := parseDHCPFromNetworkdConfig("/nonexistent/path/file.network")
			Expect(err).To(HaveOccurred())
		})

		It("should handle empty file", func() {
			configPath := filepath.Join(tempDir, "empty.network")
			err := os.WriteFile(configPath, []byte(""), 0644)
			Expect(err).NotTo(HaveOccurred())

			result, err := parseDHCPFromNetworkdConfig(configPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
		})

		It("should handle whitespace around values", func() {
			configPath := filepath.Join(tempDir, "test.network")
			content := `[Network]
DHCP = yes 
`
			err := os.WriteFile(configPath, []byte(content), 0644)
			Expect(err).NotTo(HaveOccurred())

			result, err := parseDHCPFromNetworkdConfig(configPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())
		})
	})

	Context("writeNetplanFile", Label("writeNetplanFile"), func() {
		var tempDir string

		BeforeEach(func() {
			var err error
			tempDir, err = os.MkdirTemp("", "netplan-test-*")
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			Expect(os.RemoveAll(tempDir)).To(Succeed())
		})
	})

	// Netlink-based function tests - error paths (no root required)
	Context("GetCurrentMTU", Label("GetCurrentMTU"), func() {
		It("should return error for non-existent interface", func() {
			_, err := GetCurrentMTU("nonexistent-iface-12345")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get link"))
		})
	})

	Context("GetBridgeMembers", Label("GetBridgeMembers"), func() {
		It("should return error for non-existent bridge", func() {
			_, err := GetBridgeMembers("nonexistent-bridge-12345")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get bridge"))
		})
	})

	Context("SetLinkMTU", Label("SetLinkMTU"), func() {
		It("should return error for non-existent interface", func() {
			err := SetLinkMTU("nonexistent-iface-12345", 1500)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get link"))
		})
	})

	Context("AddVFToBridge", Label("AddVFToBridge"), func() {
		It("should return error for non-existent bridge", func() {
			err := AddVFToBridge("eth0", "nonexistent-bridge-12345")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get bridge link"))
		})
	})

	Context("RemoveVFFromBridge", Label("RemoveVFFromBridge"), func() {
		It("should return nil for non-existent VF (graceful handling)", func() {
			err := RemoveVFFromBridge("nonexistent-vf-12345")
			// RemoveVFFromBridge returns nil for LinkNotFoundError
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("generateNetplanFilePath", Label("generateNetplanFilePath"), func() {
		It("should return error when serial number cannot be read", func() {
			pciHelper := NewPCIHelper("9999:99:99.0") // Non-existent device
			_, err := generateNetplanFilePath(pciHelper)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get device serial number"))
		})

		It("should generate correct path with serial number", func() {
			// Create mock sysfs with VPD data
			vpdData := createVPDDataWithSerialNumber("MT2334XZ0L")
			mock := createMockSysfs("0000:b1:00.0", "", "", vpdData, "")
			defer mock.Cleanup()

			pciHelper := NewPCIHelper("0000:b1:00.0").SetSysFS(mock.TempSysfsDir())
			path, err := generateNetplanFilePath(pciHelper)
			Expect(err).NotTo(HaveOccurred())
			Expect(path).To(Equal("/etc/netplan/99-dpu-MT2334XZ0L.yaml"))
		})

		It("should sanitize special characters in serial number", func() {
			// Create mock with serial number containing special chars
			vpdData := createVPDDataWithSerialNumber("MT/23:34")
			mock := createMockSysfs("0000:b1:00.0", "", "", vpdData, "")
			defer mock.Cleanup()

			pciHelper := NewPCIHelper("0000:b1:00.0").SetSysFS(mock.TempSysfsDir())
			path, err := generateNetplanFilePath(pciHelper)
			Expect(err).NotTo(HaveOccurred())
			// Special chars should be replaced with dashes
			Expect(path).To(Equal("/etc/netplan/99-dpu-MT-23-34.yaml"))
		})
	})

})
