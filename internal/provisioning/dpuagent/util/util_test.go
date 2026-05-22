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

package util

import (
	"os"
	"path/filepath"

	pciutil "github.com/nvidia/doca-platform/internal/provisioning/utils/pci"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func writePCIDeviceID(root, bdf, deviceID string) {
	dir := filepath.Join(root, bdf)
	ExpectWithOffset(1, os.MkdirAll(dir, 0755)).To(Succeed())
	ExpectWithOffset(1, os.WriteFile(filepath.Join(dir, "device"), []byte(deviceID+"\n"), 0644)).To(Succeed())
}

func overrideSysfsPCIDevicesDir(path string) {
	original := sysfsPCIDevicesDir
	sysfsPCIDevicesDir = path
	DeferCleanup(func() { sysfsPCIDevicesDir = original })
}

var _ = Describe("NSPortFilter", func() {
	It("should return true for known N/S NIC device IDs (BF2, BF3, BF4)", func() {
		root := GinkgoT().TempDir()
		writePCIDeviceID(root, "0000:03:00.0", "0xa2d6") // BF2
		writePCIDeviceID(root, "0000:04:00.0", "0xa2dc") // BF3
		writePCIDeviceID(root, "0002:01:00.0", "0xa2df") // BF4

		overrideSysfsPCIDevicesDir(root)

		Expect(NSPortFilter(pciutil.NICPort{Netdev: "p0", PCIAddress: "0000:03:00.0"})).To(BeTrue())
		Expect(NSPortFilter(pciutil.NICPort{Netdev: "p0", PCIAddress: "0000:04:00.0"})).To(BeTrue())
		Expect(NSPortFilter(pciutil.NICPort{Netdev: "p0", PCIAddress: "0002:01:00.0"})).To(BeTrue())
	})

	It("should return false for unknown device IDs", func() {
		root := GinkgoT().TempDir()
		writePCIDeviceID(root, "000a:01:00.0", "0xffff")

		overrideSysfsPCIDevicesDir(root)

		Expect(NSPortFilter(pciutil.NICPort{Netdev: "ew0", PCIAddress: "000a:01:00.0"})).To(BeFalse())
	})

	It("should return false when device file is missing", func() {
		overrideSysfsPCIDevicesDir(GinkgoT().TempDir())

		Expect(NSPortFilter(pciutil.NICPort{Netdev: "p0", PCIAddress: "0000:99:00.0"})).To(BeFalse())
	})
})
