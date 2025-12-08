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

		It("should write a valid netplan config file", func() {
			filePath := filepath.Join(tempDir, "test.yaml")
			mtu := int32(9000)
			dhcp := true
			config := &NetplanConfig{
				Network: NetplanNetwork{
					Version: 2,
					Ethernets: map[string]NetplanEthernet{
						"eth0": {
							MTU:   &mtu,
							DHCP4: &dhcp,
						},
					},
				},
			}

			err := writeNetplanFile(filePath, config)
			Expect(err).NotTo(HaveOccurred())

			_, err = os.Stat(filePath)
			Expect(err).NotTo(HaveOccurred())

			info, err := os.Stat(filePath)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Mode().Perm()).To(Equal(os.FileMode(0600)))

			content, err := os.ReadFile(filePath)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(ContainSubstring("version: 2"))
			Expect(string(content)).To(ContainSubstring("eth0"))
			Expect(string(content)).To(ContainSubstring("mtu: 9000"))
		})

		It("should create parent directories if they don't exist", func() {
			filePath := filepath.Join(tempDir, "subdir", "nested", "test.yaml")
			config := &NetplanConfig{
				Network: NetplanNetwork{
					Version: 2,
				},
			}

			err := writeNetplanFile(filePath, config)
			Expect(err).NotTo(HaveOccurred())

			_, err = os.Stat(filePath)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should write bridge configuration", func() {
			filePath := filepath.Join(tempDir, "bridge.yaml")
			mtu := int32(1500)
			config := &NetplanConfig{
				Network: NetplanNetwork{
					Version: 2,
					Bridges: map[string]NetplanEthernet{
						"br-dpu": {
							MTU: &mtu,
						},
					},
				},
			}

			err := writeNetplanFile(filePath, config)
			Expect(err).NotTo(HaveOccurred())

			content, err := os.ReadFile(filePath)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(ContainSubstring("bridges"))
			Expect(string(content)).To(ContainSubstring("br-dpu"))
		})
	})

	Context("NetplanConfig structure", Label("NetplanConfig"), func() {
		It("should have expected fields", func() {
			mtu := int32(9000)
			dhcp := true
			useMTU := false

			config := NetplanConfig{
				Network: NetplanNetwork{
					Version: 2,
					Ethernets: map[string]NetplanEthernet{
						"eth0": {
							MTU:   &mtu,
							DHCP4: &dhcp,
							DHCP4Overrides: &DHCP4Overrides{
								UseMTU: &useMTU,
							},
						},
					},
				},
			}

			Expect(config.Network.Version).To(Equal(2))
			Expect(config.Network.Ethernets).To(HaveLen(1))
			eth := config.Network.Ethernets["eth0"]
			Expect(*eth.MTU).To(Equal(int32(9000)))
			Expect(*eth.DHCP4).To(BeTrue())
			Expect(*eth.DHCP4Overrides.UseMTU).To(BeFalse())
		})
	})
})
