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
	"os"
	"path/filepath"

	"github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SystemdNetworkdBackend", func() {
	var backend *SystemdNetworkdBackend

	BeforeEach(func() {
		backend = &SystemdNetworkdBackend{}
	})

	Context("Name", func() {
		It("should return systemd-networkd", func() {
			Expect(backend.Name()).To(Equal("systemd-networkd"))
		})
	})

	Context("IsAvailable", func() {
		It("should check systemd-networkd availability", func() {
			result := backend.IsAvailable()
			Expect(result).To(BeAssignableToTypeOf(false))
		})
	})
})

var _ = Describe("Netplan helpers", func() {
	Context("parseDHCPFromNetworkdConfig", func() {
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
			Expect(os.WriteFile(configPath, []byte(content), 0644)).To(Succeed())
			result, err := parseDHCPFromNetworkdConfig(configPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())
		})

		It("should return true when DHCP=ipv4 in [Network] section", func() {
			configPath := filepath.Join(tempDir, "test.network")
			content := `[Network]
DHCP=ipv4
`
			Expect(os.WriteFile(configPath, []byte(content), 0644)).To(Succeed())
			result, err := parseDHCPFromNetworkdConfig(configPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())
		})

		It("should handle case-insensitive DHCP values", func() {
			configPath := filepath.Join(tempDir, "test.network")
			content := `[Network]
DHCP=YES
`
			Expect(os.WriteFile(configPath, []byte(content), 0644)).To(Succeed())
			result, err := parseDHCPFromNetworkdConfig(configPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())
		})

		It("should handle case-insensitive section headers", func() {
			configPath := filepath.Join(tempDir, "test.network")
			content := `[network]
DHCP=yes
`
			Expect(os.WriteFile(configPath, []byte(content), 0644)).To(Succeed())
			result, err := parseDHCPFromNetworkdConfig(configPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())
		})

		It("should return false when DHCP=no", func() {
			configPath := filepath.Join(tempDir, "test.network")
			content := `[Network]
DHCP=no
`
			Expect(os.WriteFile(configPath, []byte(content), 0644)).To(Succeed())
			result, err := parseDHCPFromNetworkdConfig(configPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
		})

		It("should return false when DHCP is not set", func() {
			configPath := filepath.Join(tempDir, "test.network")
			content := `[Network]
Address=192.168.1.100/24
`
			Expect(os.WriteFile(configPath, []byte(content), 0644)).To(Succeed())
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
			Expect(os.WriteFile(configPath, []byte(content), 0644)).To(Succeed())
			result, err := parseDHCPFromNetworkdConfig(configPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
		})

		It("should handle inline comments with hash", func() {
			configPath := filepath.Join(tempDir, "test.network")
			content := `[Network]
DHCP=yes # Enable DHCP
`
			Expect(os.WriteFile(configPath, []byte(content), 0644)).To(Succeed())
			result, err := parseDHCPFromNetworkdConfig(configPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())
		})

		It("should handle inline comments with semicolon", func() {
			configPath := filepath.Join(tempDir, "test.network")
			content := `[Network]
DHCP=yes ; Enable DHCP
`
			Expect(os.WriteFile(configPath, []byte(content), 0644)).To(Succeed())
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
			Expect(os.WriteFile(configPath, []byte(content), 0644)).To(Succeed())
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
			Expect(os.WriteFile(configPath, []byte(""), 0644)).To(Succeed())
			result, err := parseDHCPFromNetworkdConfig(configPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
		})

		It("should handle whitespace around values", func() {
			configPath := filepath.Join(tempDir, "test.network")
			content := `[Network]
DHCP = yes 
`
			Expect(os.WriteFile(configPath, []byte(content), 0644)).To(Succeed())
			result, err := parseDHCPFromNetworkdConfig(configPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())
		})
	})

	Context("generateNetplanFilePath", func() {
		It("should return error when serial number cannot be read", func() {
			pciHelper := util.NewPCIHelper("9999:99:99.0")
			_, err := generateNetplanFilePath(pciHelper)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get device serial number"))
		})
	})
})
