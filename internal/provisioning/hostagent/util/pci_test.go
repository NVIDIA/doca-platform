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
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PCI", func() {
	Context("NewPCIHelper", Label("NewPCIHelper"), func() {
		It("should create a PCIHelper with full address", func() {
			helper := NewPCIHelper("0000:4d:00.0")
			Expect(helper).NotTo(BeNil())
			Expect(helper.PCIAddress).To(Equal("0000:4d:00.0"))
			Expect(helper.PFIndex).To(BeNil())
			Expect(helper.VFIndex).To(BeNil())
		})

		It("should convert dash-separated address to colon-separated", func() {
			helper := NewPCIHelper("0000-4d-00")
			Expect(helper.PCIAddress).To(Equal("0000:4d:00"))
		})

		It("should prepend domain when address has only two parts", func() {
			helper := NewPCIHelper("4d:00")
			Expect(helper.PCIAddress).To(Equal("0000:4d:00"))
		})

		It("should handle address without function number", func() {
			helper := NewPCIHelper("0000:4d:00")
			Expect(helper.PCIAddress).To(Equal("0000:4d:00"))
		})
	})

	Context("PCIHelper.PF", Label("PCIHelper", "PF"), func() {
		It("should set PF index", func() {
			helper := NewPCIHelper("0000:4d:00").PF(0)
			Expect(helper.PFIndex).NotTo(BeNil())
			Expect(*helper.PFIndex).To(Equal(0))
		})

		It("should return self for chaining", func() {
			helper := NewPCIHelper("0000:4d:00")
			result := helper.PF(1)
			Expect(result).To(Equal(helper))
		})

		It("should allow setting different PF indices", func() {
			helper := NewPCIHelper("0000:4d:00").PF(2)
			Expect(*helper.PFIndex).To(Equal(2))
		})
	})

	Context("PCIHelper.VF", Label("PCIHelper", "VF"), func() {
		It("should set VF index", func() {
			helper := NewPCIHelper("0000:4d:00").VF(0)
			Expect(helper.VFIndex).NotTo(BeNil())
			Expect(*helper.VFIndex).To(Equal(0))
		})

		It("should return self for chaining", func() {
			helper := NewPCIHelper("0000:4d:00")
			result := helper.VF(1)
			Expect(result).To(Equal(helper))
		})

		It("should allow chaining PF and VF", func() {
			helper := NewPCIHelper("0000:4d:00").PF(0).VF(3)
			Expect(*helper.PFIndex).To(Equal(0))
			Expect(*helper.VFIndex).To(Equal(3))
		})
	})

	Context("PCIHelper.Path", Label("PCIHelper", "Path"), func() {
		It("should return correct path for address with function number", func() {
			helper := NewPCIHelper("0000:4d:00.0")
			path := helper.Path()
			Expect(path).To(Equal(filepath.Join(SysPCIDevicesDir, "0000:4d:00.0")))
		})

		It("should return correct path for address without function number", func() {
			helper := NewPCIHelper("0000:4d:00")
			path := helper.Path()
			Expect(path).To(Equal(filepath.Join(SysPCIDevicesDir, "0000:4d:00.0")))
		})

		It("should use PF index when set", func() {
			helper := NewPCIHelper("0000:4d:00.0").PF(1)
			path := helper.Path()
			Expect(path).To(Equal(filepath.Join(SysPCIDevicesDir, "0000:4d:00.1")))
		})

		It("should append VF path when VF index is set", func() {
			helper := NewPCIHelper("0000:4d:00.0").VF(5)
			path := helper.Path()
			Expect(path).To(Equal(filepath.Join(SysPCIDevicesDir, "0000:4d:00.0", "virtfn5")))
		})

		It("should combine PF and VF paths correctly", func() {
			helper := NewPCIHelper("0000:4d:00").PF(1).VF(3)
			path := helper.Path()
			Expect(path).To(Equal(filepath.Join(SysPCIDevicesDir, "0000:4d:00.1", "virtfn3")))
		})
	})

	Context("parseVPDSerialNumber", Label("parseVPDSerialNumber"), func() {
		It("should return empty string for empty data", func() {
			result := parseVPDSerialNumber([]byte{})
			Expect(result).To(Equal(""))
		})

		It("should return empty string for invalid VPD data", func() {
			result := parseVPDSerialNumber([]byte{0x00, 0x01, 0x02})
			Expect(result).To(Equal(""))
		})

		It("should return empty string when end tag is encountered", func() {
			result := parseVPDSerialNumber([]byte{0x78}) // 0x0f << 3 = 0x78
			Expect(result).To(Equal(""))
		})

		It("should parse serial number from VPD read-only section", func() {
			vpdData := []byte{
				0x90, 0x08, 0x00, // Large resource tag 0x90, length 8 (little endian)
				'S', 'N', // Field identifier
				0x05, // Field length (5 bytes)
				'T', 'E', 'S', 'T', '1',
			}
			result := parseVPDSerialNumber(vpdData)
			Expect(result).To(Equal("TEST1"))
		})

		It("should handle VPD data with trailing whitespace in serial number", func() {
			vpdData := []byte{
				0x90, 0x0a, 0x00, // Large resource tag 0x90, length 10
				'S', 'N', // Field identifier
				0x07, // Field length
				'A', 'B', 'C', ' ', ' ', ' ', ' ',
			}
			result := parseVPDSerialNumber(vpdData)
			Expect(result).To(Equal("ABC"))
		})

		It("should return empty string when no serial number field found", func() {
			vpdData := []byte{
				0x90, 0x08, 0x00, // Large resource tag 0x90, length 8
				'P', 'N', // Part Number field
				0x05, // Field length
				'P', 'A', 'R', 'T', '1',
			}
			result := parseVPDSerialNumber(vpdData)
			Expect(result).To(Equal(""))
		})
	})

	Context("truncateFunctionNumber", Label("truncateFunctionNumber"), func() {
		It("should truncate function number from address", func() {
			result := truncateFunctionNumber("0000:4d:00.0")
			Expect(result).To(Equal("0000:4d:00"))
		})

		It("should handle address without function number", func() {
			result := truncateFunctionNumber("0000:4d:00")
			Expect(result).To(Equal("0000:4d:00"))
		})

		It("should truncate function number 1", func() {
			result := truncateFunctionNumber("0000:4d:00.1")
			Expect(result).To(Equal("0000:4d:00"))
		})

		It("should handle different PCI domains", func() {
			result := truncateFunctionNumber("0001:3c:00.2")
			Expect(result).To(Equal("0001:3c:00"))
		})
	})
})
