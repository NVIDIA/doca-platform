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

// mockSysfsSetup holds paths for mock sysfs directories and handles
// automatic reassignment of package-level sysfs path variables.
//
// Usage pattern in tests (simple - just defer cleanup):
//
//	mock := createMockSysfs("0000:b1:00.0", "0xa2dc\n", "eth0", nil, "")
//	defer mock.Cleanup()  // Removes temp files
//	// ... test code using NewPCIHelper, IsDPU, GetMTU, etc. ...
//
// How it works:
//   - createMockSysfs creates a temporary directory as the root of a mock sysfs
//   - Cleanup() removes the temp directory and all its contents
//   - This ensures test isolation without manual save/restore boilerplate
type mockSysfsSetup struct {
	// tempSysfsDir is the temporary directory created for the mock sysfs.
	tempSysfsDir string
}

// createMockSysfs creates a mock sysfs structure for testing PCI functions.
//
// Parameters:
//   - pciAddress: PCI address like "0000:b1:00.0" to create device directory for
//   - deviceID: Content for "device" file (e.g., "0xa2dc\n" for BlueField-3), empty to skip
//   - interfaceName: Network interface name to create in net/ dir (e.g., "eth0"), empty to skip
//   - vpdData: Binary VPD data for "vpd" file, nil to skip
//   - mtu: MTU value string for /sys/class/net/<iface>/mtu (e.g., "9000"), empty to skip
//
// Returns mockSysfsSetup - call cleanup() via defer to restore original paths and remove temp files.
func createMockSysfs(pciAddress string, deviceID string, interfaceName string, vpdData []byte, mtu string) *mockSysfsSetup {
	tempDir, err := os.MkdirTemp("", "mock-sysfs-*")
	Expect(err).NotTo(HaveOccurred())

	pciDevicesDir := filepath.Join(tempDir, sysPCIDevicesDir)
	classNetDir := filepath.Join(tempDir, sysClassNetDir)

	devicePath := filepath.Join(pciDevicesDir, pciAddress)
	err = os.MkdirAll(devicePath, 0755)
	Expect(err).NotTo(HaveOccurred())

	// Create device file (for IsDPU)
	if deviceID != "" {
		err = os.WriteFile(filepath.Join(devicePath, "device"), []byte(deviceID), 0644)
		Expect(err).NotTo(HaveOccurred())
	}

	// Create net directory with interface (for InterfaceName)
	if interfaceName != "" {
		netPath := filepath.Join(devicePath, "net", interfaceName)
		err = os.MkdirAll(netPath, 0755)
		Expect(err).NotTo(HaveOccurred())
	}

	// Create vpd file (for SerialNumber)
	if vpdData != nil {
		err = os.WriteFile(filepath.Join(devicePath, "vpd"), vpdData, 0644)
		Expect(err).NotTo(HaveOccurred())
	}

	// Create MTU file in /sys/class/net/<interface>/mtu (for GetMTU)
	if interfaceName != "" && mtu != "" {
		ifacePath := filepath.Join(classNetDir, interfaceName)
		err = os.MkdirAll(ifacePath, 0755)
		Expect(err).NotTo(HaveOccurred())
		err = os.WriteFile(filepath.Join(ifacePath, "mtu"), []byte(mtu), 0644)
		Expect(err).NotTo(HaveOccurred())
	}

	return &mockSysfsSetup{
		tempSysfsDir: tempDir,
	}
}

func (m *mockSysfsSetup) TempSysfsDir() string {
	return m.tempSysfsDir
}

func (m *mockSysfsSetup) PCIDevicesDir() string {
	return filepath.Join(m.tempSysfsDir, sysPCIDevicesDir)
}

func (m *mockSysfsSetup) ClassNetDir() string {
	return filepath.Join(m.tempSysfsDir, sysClassNetDir)
}

func (m *mockSysfsSetup) Cleanup() {
	// Remove temp directory and all contents
	_ = os.RemoveAll(m.tempSysfsDir) // Best effort cleanup, ignore error
}

// createVPDDataWithSerialNumber creates VPD data containing a serial number
func createVPDDataWithSerialNumber(serialNumber string) []byte {
	// VPD format: Large resource tag (0x90), length (little endian), then fields
	// Field format: 2-byte identifier, 1-byte length, data
	snBytes := []byte(serialNumber)
	fieldLen := len(snBytes)
	totalLen := 2 + 1 + fieldLen // SN identifier (2) + length (1) + data

	vpdData := []byte{
		0x90,                 // Large resource tag for VPD-R
		byte(totalLen), 0x00, // Length (little endian)
		'S', 'N', // Serial Number field identifier
		byte(fieldLen), // Field length
	}
	vpdData = append(vpdData, snBytes...)
	return vpdData
}

var _ = Describe("PCI", func() {
	Context("PCIHelper.IsDPU", Label("PCIHelper", "IsDPU"), func() {
		It("should return error when device file does not exist", func() {
			helper := NewPCIHelper("9999:99:99.0") // Non-existent PCI address
			_, err := helper.IsDPU()
			Expect(err).To(HaveOccurred())
		})

		It("should return true for known DPU device ID 0xa2dc (BlueField-3)", func() {
			mock := createMockSysfs("0000:b1:00.0", "0xa2dc\n", "", nil, "")
			defer mock.Cleanup()

			helper := NewPCIHelper("0000:b1:00.0").SetSysFS(mock.TempSysfsDir())
			isDPU, err := helper.IsDPU()
			Expect(err).NotTo(HaveOccurred())
			Expect(isDPU).To(BeTrue())
		})

		It("should return true for known DPU device ID 0xa2d6 (BlueField-2)", func() {
			mock := createMockSysfs("0000:03:00.0", "0xa2d6\n", "", nil, "")
			defer mock.Cleanup()

			helper := NewPCIHelper("0000:03:00.0").SetSysFS(mock.TempSysfsDir())
			isDPU, err := helper.IsDPU()
			Expect(err).NotTo(HaveOccurred())
			Expect(isDPU).To(BeTrue())
		})

		It("should return false for non-DPU device ID", func() {
			mock := createMockSysfs("0000:00:00.0", "0x1234\n", "", nil, "")
			defer mock.Cleanup()

			helper := NewPCIHelper("0000:00:00.0").SetSysFS(mock.TempSysfsDir())
			isDPU, err := helper.IsDPU()
			Expect(err).NotTo(HaveOccurred())
			Expect(isDPU).To(BeFalse())
		})
	})

	Context("PCIHelper.InterfaceName", Label("PCIHelper", "InterfaceName"), func() {
		It("should return error when net directory does not exist", func() {
			helper := NewPCIHelper("9999:99:99.0") // Non-existent PCI address
			_, err := helper.InterfaceName()
			Expect(err).To(HaveOccurred())
		})

		It("should return interface name from net directory", func() {
			mock := createMockSysfs("0000:b1:00.0", "", "enp177s0f0np0", nil, "")
			defer mock.Cleanup()

			helper := NewPCIHelper("0000:b1:00.0").SetSysFS(mock.TempSysfsDir())
			name, err := helper.InterfaceName()
			Expect(err).NotTo(HaveOccurred())
			Expect(name).To(Equal("enp177s0f0np0"))
		})

		It("should return error when net directory is empty", func() {
			mock := createMockSysfs("0000:b1:00.0", "", "", nil, "")
			defer mock.Cleanup()

			// Create empty net directory
			netPath := filepath.Join(mock.PCIDevicesDir(), "0000:b1:00.0", "net")
			err := os.MkdirAll(netPath, 0755)
			Expect(err).NotTo(HaveOccurred())

			helper := NewPCIHelper("0000:b1:00.0").SetSysFS(mock.TempSysfsDir())
			_, err = helper.InterfaceName()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no network interface found"))
		})
	})

	Context("PCIHelper.SerialNumber", Label("PCIHelper", "SerialNumber"), func() {
		It("should return error when vpd file does not exist", func() {
			helper := NewPCIHelper("9999:99:99.0") // Non-existent PCI address
			_, err := helper.SerialNumber()
			Expect(err).To(HaveOccurred())
		})

		It("should return serial number from vpd file", func() {
			vpdData := createVPDDataWithSerialNumber("MT2334XZ0L")
			mock := createMockSysfs("0000:b1:00.0", "", "", vpdData, "")
			defer mock.Cleanup()

			helper := NewPCIHelper("0000:b1:00.0").SetSysFS(mock.TempSysfsDir())
			sn, err := helper.SerialNumber()
			Expect(err).NotTo(HaveOccurred())
			Expect(sn).To(Equal("MT2334XZ0L"))
		})

		It("should return empty string when vpd has no serial number", func() {
			// VPD data without serial number field
			vpdData := []byte{
				0x90, 0x08, 0x00, // Large resource tag
				'P', 'N', // Part Number field (not SN)
				0x05,
				'P', 'A', 'R', 'T', '1',
			}
			mock := createMockSysfs("0000:b1:00.0", "", "", vpdData, "")
			defer mock.Cleanup()

			helper := NewPCIHelper("0000:b1:00.0").SetSysFS(mock.TempSysfsDir())
			sn, err := helper.SerialNumber()
			Expect(err).NotTo(HaveOccurred())
			Expect(sn).To(Equal(""))
		})
	})

	Context("PCIHelper.GetMTU", Label("PCIHelper", "GetMTU"), func() {
		It("should return error when interface does not exist", func() {
			helper := NewPCIHelper("9999:99:99.0") // Non-existent PCI address
			_, err := helper.GetMTU()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get interface name"))
		})

		It("should return MTU value for interface", func() {
			mock := createMockSysfs("0000:b1:00.0", "", "enp177s0f0np0", nil, "9000")
			defer mock.Cleanup()

			helper := NewPCIHelper("0000:b1:00.0").SetSysFS(mock.TempSysfsDir())
			mtu, err := helper.GetMTU()
			Expect(err).NotTo(HaveOccurred())
			Expect(mtu).To(Equal(9000))
		})

		It("should return error when MTU file cannot be read", func() {
			mock := createMockSysfs("0000:b1:00.0", "", "enp177s0f0np0", nil, "")
			defer mock.Cleanup()

			helper := NewPCIHelper("0000:b1:00.0").SetSysFS(mock.TempSysfsDir())
			_, err := helper.GetMTU()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to read MTU"))
		})

		It("should return error when MTU value is not a valid integer", func() {
			mock := createMockSysfs("0000:b1:00.0", "", "enp177s0f0np0", nil, "invalid")
			defer mock.Cleanup()

			helper := NewPCIHelper("0000:b1:00.0").SetSysFS(mock.TempSysfsDir())
			_, err := helper.GetMTU()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to parse MTU"))
		})
	})

	Context("ReadDeviceSerialNumberFromVPD", Label("ReadDeviceSerialNumberFromVPD"), func() {
		It("should return error when vpd file does not exist", func() {
			_, err := ReadDeviceSerialNumberFromVPD(SysFSRoot, "9999:99:99.0") // Non-existent PCI address
			Expect(err).To(HaveOccurred())
		})

		It("should return serial number from vpd file", func() {
			vpdData := createVPDDataWithSerialNumber("MT2334XZ0L")
			mock := createMockSysfs("0000:b1:00.0", "", "", vpdData, "")
			defer mock.Cleanup()

			sn, err := ReadDeviceSerialNumberFromVPD(mock.TempSysfsDir(), "0000:b1:00.0")
			Expect(err).NotTo(HaveOccurred())
			Expect(sn).To(Equal("MT2334XZ0L"))
		})

		It("should return empty string when vpd has no serial number", func() {
			// VPD data without serial number
			vpdData := []byte{0x90, 0x05, 0x00, 'P', 'N', 0x02, 'A', 'B'}
			mock := createMockSysfs("0000:b1:00.0", "", "", vpdData, "")
			defer mock.Cleanup()

			sn, err := ReadDeviceSerialNumberFromVPD(mock.TempSysfsDir(), "0000:b1:00.0")
			Expect(err).NotTo(HaveOccurred())
			Expect(sn).To(Equal(""))
		})
	})

	Context("NewPCIHelper", Label("NewPCIHelper"), func() {
		It("should create a PCIHelper with full address", func() {
			helper := NewPCIHelper("0000:4d:00.0")
			Expect(helper).NotTo(BeNil())
			Expect(helper.pciAddress).To(Equal("0000:4d:00.0"))
			Expect(helper.pfIndex).To(BeNil())
			Expect(helper.vfIndex).To(BeNil())
		})

		It("should convert dash-separated address to colon-separated", func() {
			helper := NewPCIHelper("0000-4d-00")
			Expect(helper.pciAddress).To(Equal("0000:4d:00"))
		})

		It("should prepend domain when address has only two parts", func() {
			helper := NewPCIHelper("4d:00")
			Expect(helper.pciAddress).To(Equal("0000:4d:00"))
		})

		It("should handle address without function number", func() {
			helper := NewPCIHelper("0000:4d:00")
			Expect(helper.pciAddress).To(Equal("0000:4d:00"))
		})
	})

	Context("PCIHelper.PF", Label("PCIHelper", "PF"), func() {
		It("should set PF index", func() {
			helper := NewPCIHelper("0000:4d:00").PF(0)
			Expect(helper.pfIndex).NotTo(BeNil())
			Expect(*helper.pfIndex).To(Equal(0))
		})

		It("should return self for chaining", func() {
			helper := NewPCIHelper("0000:4d:00")
			result := helper.PF(1)
			Expect(result).To(Equal(helper))
		})

		It("should allow setting different PF indices", func() {
			helper := NewPCIHelper("0000:4d:00").PF(2)
			Expect(*helper.pfIndex).To(Equal(2))
		})
	})

	Context("PCIHelper.VF", Label("PCIHelper", "VF"), func() {
		It("should set VF index", func() {
			helper := NewPCIHelper("0000:4d:00").VF(0)
			Expect(helper.vfIndex).NotTo(BeNil())
			Expect(*helper.vfIndex).To(Equal(0))
		})

		It("should return self for chaining", func() {
			helper := NewPCIHelper("0000:4d:00")
			result := helper.VF(1)
			Expect(result).To(Equal(helper))
		})

		It("should allow chaining PF and VF", func() {
			helper := NewPCIHelper("0000:4d:00").PF(0).VF(3)
			Expect(*helper.pfIndex).To(Equal(0))
			Expect(*helper.vfIndex).To(Equal(3))
		})
	})

	Context("PCIHelper.Path", Label("PCIHelper", "Path"), func() {
		It("should return correct path for address with function number", func() {
			helper := NewPCIHelper("0000:4d:00.0")
			path := helper.Path()
			Expect(path).To(Equal(filepath.Join(SysFSRoot, sysPCIDevicesDir, "0000:4d:00.0")))
		})

		It("should return correct path for address without function number", func() {
			helper := NewPCIHelper("0000:4d:00")
			path := helper.Path()
			Expect(path).To(Equal(filepath.Join(SysFSRoot, sysPCIDevicesDir, "0000:4d:00.0")))
		})

		It("should use PF index when set", func() {
			helper := NewPCIHelper("0000:4d:00.0").PF(1)
			path := helper.Path()
			Expect(path).To(Equal(filepath.Join(SysFSRoot, sysPCIDevicesDir, "0000:4d:00.1")))
		})

		It("should append VF path when VF index is set", func() {
			helper := NewPCIHelper("0000:4d:00.0").VF(5)
			path := helper.Path()
			Expect(path).To(Equal(filepath.Join(SysFSRoot, sysPCIDevicesDir, "0000:4d:00.0", "virtfn5")))
		})

		It("should combine PF and VF paths correctly", func() {
			helper := NewPCIHelper("0000:4d:00").PF(1).VF(3)
			path := helper.Path()
			Expect(path).To(Equal(filepath.Join(SysFSRoot, sysPCIDevicesDir, "0000:4d:00.1", "virtfn3")))
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

	Context("PCIHelper.SetNumOfVFs", Label("PCIHelper", "SetNumOfVFs"), func() {
		It("should return error when sriov_numvfs file does not exist", func() {
			mock := createMockSysfs("0000:b1:00.0", "0xa2dc\n", "", nil, "")
			defer mock.Cleanup()

			helper := NewPCIHelper("0000:b1:00.0").SetSysFS(mock.TempSysfsDir())
			err := helper.SetNumOfVFs(4)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to stat sriov_numvfs path"))
		})

		It("should successfully write number of VFs", func() {
			mock := createMockSysfs("0000:b1:00.0", "0xa2dc\n", "", nil, "")
			defer mock.Cleanup()

			// Create sriov_numvfs file
			numvfsPath := filepath.Join(mock.PCIDevicesDir(), "0000:b1:00.0", "sriov_numvfs")
			err := os.WriteFile(numvfsPath, []byte("0"), 0644)
			Expect(err).NotTo(HaveOccurred())

			helper := NewPCIHelper("0000:b1:00.0").SetSysFS(mock.TempSysfsDir())
			err = helper.SetNumOfVFs(4)
			Expect(err).NotTo(HaveOccurred())

			// Verify the file was written correctly
			content, err := os.ReadFile(numvfsPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal("4"))
		})

		It("should write zero VFs", func() {
			mock := createMockSysfs("0000:b1:00.0", "0xa2dc\n", "", nil, "")
			defer mock.Cleanup()

			numvfsPath := filepath.Join(mock.PCIDevicesDir(), "0000:b1:00.0", "sriov_numvfs")
			err := os.WriteFile(numvfsPath, []byte("8"), 0644)
			Expect(err).NotTo(HaveOccurred())

			helper := NewPCIHelper("0000:b1:00.0").SetSysFS(mock.TempSysfsDir())
			err = helper.SetNumOfVFs(0)
			Expect(err).NotTo(HaveOccurred())

			content, err := os.ReadFile(numvfsPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal("0"))
		})
	})

	Context("DiscoverDPUs", Label("DiscoverDPUs"), func() {
		It("should return error when PCI devices directory does not exist", func() {
			// Create mock to get cleanup() for path restoration, then point to non-existent path
			mock := createMockSysfs("0000:00:00.0", "0x1234\n", "", nil, "")
			defer mock.Cleanup()

			_, err := DiscoverDPUs("/nonexistent/path/to/pci/devices")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to read PCI sys directory"))
		})

		It("should return empty list when no devices exist", func() {
			// Create mock with a device, then remove it to get empty directory
			mock := createMockSysfs("0000:00:00.0", "0x1234\n", "", nil, "")
			defer mock.Cleanup()

			// Remove the device to have an empty PCI devices directory
			err := os.RemoveAll(filepath.Join(mock.PCIDevicesDir(), "0000:00:00.0"))
			Expect(err).NotTo(HaveOccurred())

			devices, err := DiscoverDPUs(mock.TempSysfsDir())
			Expect(err).NotTo(HaveOccurred())
			Expect(devices).To(BeEmpty())
		})

		It("should return empty list when no DPU devices found", func() {
			mock := createMockSysfs("0000:00:00.0", "0x1234\n", "", nil, "")
			defer mock.Cleanup()

			devices, err := DiscoverDPUs(mock.TempSysfsDir())
			Expect(err).NotTo(HaveOccurred())
			Expect(devices).To(BeEmpty())
		})

		It("should discover a single DPU device", func() {
			vpdData := createVPDDataWithSerialNumber("MT2334XZ0L")
			mock := createMockSysfs("0000:b1:00.0", "0xa2dc\n", "", vpdData, "")
			defer mock.Cleanup()

			devices, err := DiscoverDPUs(mock.TempSysfsDir())
			Expect(err).NotTo(HaveOccurred())
			Expect(devices).To(HaveLen(1))
			Expect(devices[0].Address).To(Equal("0000:b1:00"))
			Expect(devices[0].SerialNumber).To(Equal("MT2334XZ0L"))
			Expect(devices[0].NumOfPFs).To(Equal(1))
		})

		It("should aggregate multiple PFs from the same device", func() {
			// Create first mock for PF0
			vpdData := createVPDDataWithSerialNumber("MT2334XZ0L")
			mock := createMockSysfs("0000:b1:00.0", "0xa2dc\n", "", vpdData, "")
			defer mock.Cleanup()

			// Add PF1 to the same mock directory
			pf1Path := filepath.Join(mock.PCIDevicesDir(), "0000:b1:00.1")
			err := os.MkdirAll(pf1Path, 0755)
			Expect(err).NotTo(HaveOccurred())
			err = os.WriteFile(filepath.Join(pf1Path, "device"), []byte("0xa2dc\n"), 0644)
			Expect(err).NotTo(HaveOccurred())
			err = os.WriteFile(filepath.Join(pf1Path, "vpd"), vpdData, 0644)
			Expect(err).NotTo(HaveOccurred())

			devices, err := DiscoverDPUs(mock.TempSysfsDir())
			Expect(err).NotTo(HaveOccurred())
			Expect(devices).To(HaveLen(1))
			Expect(devices[0].Address).To(Equal("0000:b1:00"))
			Expect(devices[0].SerialNumber).To(Equal("MT2334XZ0L"))
			Expect(devices[0].NumOfPFs).To(Equal(2))
		})

		It("should discover multiple different DPU devices", func() {
			vpdData1 := createVPDDataWithSerialNumber("MT2334XZ0L")
			mock := createMockSysfs("0000:b1:00.0", "0xa2dc\n", "", vpdData1, "")
			defer mock.Cleanup()

			// Add a second DPU device with different address
			vpdData2 := createVPDDataWithSerialNumber("MT9876YZ0K")
			dev2Path := filepath.Join(mock.PCIDevicesDir(), "0000:3c:00.0")
			err := os.MkdirAll(dev2Path, 0755)
			Expect(err).NotTo(HaveOccurred())
			err = os.WriteFile(filepath.Join(dev2Path, "device"), []byte("0xa2d6\n"), 0644)
			Expect(err).NotTo(HaveOccurred())
			err = os.WriteFile(filepath.Join(dev2Path, "vpd"), vpdData2, 0644)
			Expect(err).NotTo(HaveOccurred())

			devices, err := DiscoverDPUs(mock.TempSysfsDir())
			Expect(err).NotTo(HaveOccurred())
			Expect(devices).To(HaveLen(2))

			// Find devices by address since map iteration order is not guaranteed
			deviceMap := make(map[string]Device)
			for _, d := range devices {
				deviceMap[d.Address] = d
			}

			Expect(deviceMap).To(HaveKey("0000:b1:00"))
			Expect(deviceMap["0000:b1:00"].SerialNumber).To(Equal("MT2334XZ0L"))
			Expect(deviceMap["0000:b1:00"].NumOfPFs).To(Equal(1))

			Expect(deviceMap).To(HaveKey("0000:3c:00"))
			Expect(deviceMap["0000:3c:00"].SerialNumber).To(Equal("MT9876YZ0K"))
			Expect(deviceMap["0000:3c:00"].NumOfPFs).To(Equal(1))
		})

		It("should continue when IsDPU check fails for a device", func() {
			vpdData := createVPDDataWithSerialNumber("MT2334XZ0L")
			mock := createMockSysfs("0000:b1:00.0", "0xa2dc\n", "", vpdData, "")
			defer mock.Cleanup()

			// Add a device directory without device file (will cause IsDPU to fail)
			brokenDevPath := filepath.Join(mock.PCIDevicesDir(), "0000:ff:00.0")
			err := os.MkdirAll(brokenDevPath, 0755)
			Expect(err).NotTo(HaveOccurred())
			// Note: not creating "device" file, so IsDPU will fail

			devices, err := DiscoverDPUs(mock.TempSysfsDir())
			Expect(err).NotTo(HaveOccurred())
			// Should still discover the valid device
			Expect(devices).To(HaveLen(1))
			Expect(devices[0].Address).To(Equal("0000:b1:00"))
		})

		It("should continue when SerialNumber read fails for a device", func() {
			mock := createMockSysfs("0000:b1:00.0", "0xa2dc\n", "", nil, "")
			defer mock.Cleanup()
			// Note: vpd file not created, so SerialNumber will fail

			// Add another valid DPU device
			vpdData := createVPDDataWithSerialNumber("MT9876YZ0K")
			dev2Path := filepath.Join(mock.PCIDevicesDir(), "0000:3c:00.0")
			err := os.MkdirAll(dev2Path, 0755)
			Expect(err).NotTo(HaveOccurred())
			err = os.WriteFile(filepath.Join(dev2Path, "device"), []byte("0xa2dc\n"), 0644)
			Expect(err).NotTo(HaveOccurred())
			err = os.WriteFile(filepath.Join(dev2Path, "vpd"), vpdData, 0644)
			Expect(err).NotTo(HaveOccurred())

			devices, err := DiscoverDPUs(mock.TempSysfsDir())
			Expect(err).NotTo(HaveOccurred())
			// Should only discover the second device (first one has no vpd)
			Expect(devices).To(HaveLen(1))
			Expect(devices[0].Address).To(Equal("0000:3c:00"))
			Expect(devices[0].SerialNumber).To(Equal("MT9876YZ0K"))
		})

		It("should handle mix of DPU and non-DPU devices", func() {
			vpdData := createVPDDataWithSerialNumber("MT2334XZ0L")
			mock := createMockSysfs("0000:b1:00.0", "0xa2dc\n", "", vpdData, "")
			defer mock.Cleanup()

			// Add a non-DPU device
			nonDpuPath := filepath.Join(mock.PCIDevicesDir(), "0000:00:1f.0")
			err := os.MkdirAll(nonDpuPath, 0755)
			Expect(err).NotTo(HaveOccurred())
			err = os.WriteFile(filepath.Join(nonDpuPath, "device"), []byte("0x8086\n"), 0644)
			Expect(err).NotTo(HaveOccurred())

			devices, err := DiscoverDPUs(mock.TempSysfsDir())
			Expect(err).NotTo(HaveOccurred())
			Expect(devices).To(HaveLen(1))
			Expect(devices[0].Address).To(Equal("0000:b1:00"))
		})
	})

})
