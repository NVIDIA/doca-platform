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

package pci

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PCIHelper", func() {
	DescribeTable("NormalizeAddress",
		func(address, expected string) {
			Expect(NormalizeAddress(address)).To(Equal(expected))
		},
		Entry("full address", "0000:03:00.0", "0000:03:00.0"),
		Entry("short address", "03:00.0", "0000:03:00.0"),
		Entry("uppercase and whitespace", " 0000:AB:CD.1\n", "0000:ab:cd.1"),
		Entry("empty", "", ""),
	)

	Context("AddressSet", func() {
		It("should return normalized non-empty addresses", func() {
			got := AddressSet([]string{
				"0000:03:00.0",
				"03:00.1",
				" 0000:AB:CD.2\n",
				"",
			})

			Expect(got).To(Equal(map[string]struct{}{
				"0000:03:00.0": {},
				"0000:03:00.1": {},
				"0000:ab:cd.2": {},
			}))
		})
	})

	Context("NetdevPCI", func() {
		It("should return the normalized PCI address from netdev uevent", func() {
			root := GinkgoT().TempDir()
			setSysfsNetPathForTest(root)

			ueventPath := filepath.Join(root, "p0", "device", "uevent")
			Expect(os.MkdirAll(filepath.Dir(ueventPath), 0755)).To(Succeed())
			Expect(os.WriteFile(ueventPath, []byte("DRIVER=mlx5_core\nPCI_SLOT_NAME=03:00.0\n"), 0644)).To(Succeed())

			pciAddress, err := NetdevPCI("p0")
			Expect(err).NotTo(HaveOccurred())
			Expect(pciAddress).To(Equal("0000:03:00.0"))
		})

		It("should return empty string when netdev uevent does not exist", func() {
			setSysfsNetPathForTest(GinkgoT().TempDir())

			pciAddress, err := NetdevPCI("p0")
			Expect(err).NotTo(HaveOccurred())
			Expect(pciAddress).To(BeEmpty())
		})
	})
})

func setSysfsNetPathForTest(path string) {
	original := sysfsNetPath
	sysfsNetPath = path
	DeferCleanup(func() {
		sysfsNetPath = original
	})
}
