//go:build linux

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

package sriov

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("DefaultOps", func() {
	var (
		ops           DefaultOps
		origSysBusPci string
		sysBusPci     string
	)

	BeforeEach(func() {
		ops = DefaultOps{}
		origSysBusPci = SysBusPci
		sysBusPci = GinkgoT().TempDir()
		SysBusPci = sysBusPci
	})

	AfterEach(func() {
		SysBusPci = origSysBusPci
	})

	Describe("IsOvsHardwareOffloadEnabled", func() {
		It("returns true when device id is set", func() {
			Expect(ops.IsOvsHardwareOffloadEnabled("0000:03:00.0")).To(BeTrue())
		})

		It("returns false when device id is empty", func() {
			Expect(ops.IsOvsHardwareOffloadEnabled("")).To(BeFalse())
		})
	})

	Describe("IsPCIDeviceName", func() {
		DescribeTable("matches PCI device names",
			func(deviceID string, expected bool) {
				Expect(ops.IsPCIDeviceName(deviceID)).To(Equal(expected))
			},
			Entry("valid PCI address", "0000:03:00.0", true),
			Entry("valid PCI address with function 7", "abcd:ef:1f.7", true),
			Entry("missing domain", "03:00.0", false),
			Entry("invalid function", "0000:03:00.8", false),
			Entry("auxiliary device", "mlx5_core.sf.1", false),
		)
	})

	Describe("GetVFLinkName", func() {
		const pciAddr = "0000:03:00.0"

		It("returns the VF netdevice name from sysfs", func() {
			Expect(os.MkdirAll(filepath.Join(sysBusPci, pciAddr, "net", "pf0vf0"), 0700)).To(Succeed())

			got, err := ops.GetVFLinkName(pciAddr)

			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal("pf0vf0"))
		})

		It("returns error when the VF net directory is missing", func() {
			_, err := ops.GetVFLinkName(pciAddr)

			Expect(err).To(HaveOccurred())
		})

		It("returns error when the VF net directory is empty", func() {
			Expect(os.MkdirAll(filepath.Join(sysBusPci, pciAddr, "net"), 0700)).To(Succeed())

			_, err := ops.GetVFLinkName(pciAddr)

			Expect(err).To(MatchError(ContainSubstring("has no entries")))
		})
	})

	Describe("HasUserspaceDriver", func() {
		const pciAddr = "0000:03:00.0"

		It("returns true for userspace drivers", func() {
			driverPath := filepath.Join(sysBusPci, "drivers", "vfio-pci")
			devicePath := filepath.Join(sysBusPci, pciAddr)
			Expect(os.MkdirAll(driverPath, 0700)).To(Succeed())
			Expect(os.MkdirAll(devicePath, 0700)).To(Succeed())
			Expect(os.Symlink(driverPath, filepath.Join(devicePath, "driver"))).To(Succeed())

			got, err := ops.HasUserspaceDriver(pciAddr)

			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeTrue())
		})

		It("returns false for non-userspace drivers", func() {
			driverPath := filepath.Join(sysBusPci, "drivers", "mlx5_core")
			devicePath := filepath.Join(sysBusPci, pciAddr)
			Expect(os.MkdirAll(driverPath, 0700)).To(Succeed())
			Expect(os.MkdirAll(devicePath, 0700)).To(Succeed())
			Expect(os.Symlink(driverPath, filepath.Join(devicePath, "driver"))).To(Succeed())

			got, err := ops.HasUserspaceDriver(pciAddr)

			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeFalse())
		})

		It("returns error when the driver link is missing", func() {
			_, err := ops.HasUserspaceDriver(pciAddr)

			Expect(err).To(HaveOccurred())
		})
	})
})
