/*
Copyright 2026 NVIDIA

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

package netplan

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
	"k8s.io/utils/ptr"
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
			config := &Config{
				Network: Network{
					Version:  2,
					Renderer: "networkd",
					Ethernets: map[string]Ethernet{
						"eth0": {
							MTU:   ptr.To(int32(9000)),
							DHCP4: ptr.To(true),
							DHCP6: ptr.To(false),
							DHCP4Overrides: &DHCPOverrides{
								UseMTU: ptr.To(false),
							},
							DHCP6Overrides: &DHCPOverrides{
								UseMTU: ptr.To(false),
							},
						},
						"eth1": {
							MTU:       ptr.To(int32(1500)),
							DHCP4:     ptr.To(false),
							LinkLocal: ptr.To([]string{}),
							Optional:  ptr.To(true),
						},
						"eth2": {
							DHCP4:     ptr.To(false),
							LinkLocal: ptr.To([]string{"ipv4"}),
						},
						"tmfifo_net0": {
							DHCP4:     ptr.To(false),
							DHCP6:     ptr.To(false),
							LinkLocal: ptr.To([]string{}),
							Addresses: []string{"fe80::2/64"},
						},
					},
					Bridges: map[string]Bridge{
						"br-dpu": {
							Interfaces: []string{"eth0", "eth1"},
							Ethernet: Ethernet{
								MTU:   ptr.To(int32(9216)),
								DHCP4: ptr.To(true),
								DHCP4Overrides: &DHCPOverrides{
									UseMTU: ptr.To(false),
								},
								DHCP6Overrides: &DHCPOverrides{
									UseMTU: ptr.To(false),
								},
							},
						},
					},
				},
			}
			expectedContent := `
network:
  version: 2
  renderer: networkd
  ethernets:
    eth0:
      mtu: 9000
      dhcp4: true
      dhcp6: false
      dhcp4-overrides:
        use-mtu: false
      dhcp6-overrides:
        use-mtu: false
    eth1:
      mtu: 1500
      dhcp4: false
      link-local: []
      optional: true
    eth2:
      dhcp4: false
      link-local:
        - ipv4
    tmfifo_net0:
      dhcp4: false
      dhcp6: false
      link-local: []
      addresses:
        - fe80::2/64
  bridges:
    br-dpu:
      interfaces:
        - eth0
        - eth1
      mtu: 9216
      dhcp4: true
      dhcp4-overrides:
        use-mtu: false
      dhcp6-overrides:
        use-mtu: false
`
			err := config.WriteToFile(filePath)
			Expect(err).NotTo(HaveOccurred())

			info, err := os.Stat(filePath)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Mode().Perm()).To(Equal(os.FileMode(0600)))

			var writenObj, expectedObj map[string]interface{}
			writenBytes, err := os.ReadFile(filePath)
			Expect(err).NotTo(HaveOccurred())
			err = yaml.Unmarshal(writenBytes, &writenObj)
			Expect(err).NotTo(HaveOccurred())
			err = yaml.Unmarshal([]byte(expectedContent), &expectedObj)
			Expect(err).NotTo(HaveOccurred())
			Expect(writenObj).To(Equal(expectedObj))
		})

		It("should create parent directories if they don't exist", func() {
			filePath := filepath.Join(tempDir, "subdir", "nested", "test.yaml")
			config := &Config{
				Network: Network{
					Version: 2,
				},
			}

			err := config.WriteToFile(filePath)
			Expect(err).NotTo(HaveOccurred())

			_, err = os.Stat(filePath)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
