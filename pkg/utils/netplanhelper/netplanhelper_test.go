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

package netplanhelper

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("NetplanHelper", func() {
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

			err := config.WriteToFile(filePath)
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

			err := config.WriteToFile(filePath)
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

			err := config.WriteToFile(filePath)
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
