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
	"testing"

	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"

	"github.com/onsi/gomega"
)

func TestDiscoverNSNIC(t *testing.T) {
	t.Run("selects BF4 N/S NIC by device ID", func(t *testing.T) {
		g := gomega.NewWithT(t)
		root := createPCIDevice(g, "0000:b1:00.0", hostutil.DeviceIDBlueField4)
		defer func() { _ = os.RemoveAll(root) }()

		dev, err := DiscoverNSNIC(root)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(dev.Address).To(gomega.Equal("0000:b1:00"))
		g.Expect(dev.NumOfPFs).To(gomega.Equal(1))
		g.Expect(dev.PFPCIAddresses()).To(gomega.Equal([]string{"0000:b1:00.0"}))
	})

	t.Run("aggregates PFs and ignores unsupported BF4 E/W NICs", func(t *testing.T) {
		g := gomega.NewWithT(t)
		root := createPCIDevice(g, "0000:b1:00.0", hostutil.DeviceIDBlueField4)
		defer func() { _ = os.RemoveAll(root) }()
		writePCIDevice(g, root, "0000:b1:00.1", hostutil.DeviceIDBlueField4)
		writePCIDevice(g, root, "0000:c1:00.0", "0xffff")

		dev, err := DiscoverNSNIC(root)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(dev.Address).To(gomega.Equal("0000:b1:00"))
		g.Expect(dev.NumOfPFs).To(gomega.Equal(2))
		g.Expect(dev.PFPCIAddresses()).To(gomega.Equal([]string{"0000:b1:00.0", "0000:b1:00.1"}))
	})

	t.Run("returns error for multiple target NICs", func(t *testing.T) {
		g := gomega.NewWithT(t)
		root := createPCIDevice(g, "0000:b1:00.0", hostutil.DeviceIDBlueField4)
		defer func() { _ = os.RemoveAll(root) }()
		writePCIDevice(g, root, "0000:c1:00.0", hostutil.DeviceIDBlueField3)

		_, err := DiscoverNSNIC(root)
		g.Expect(err).To(gomega.HaveOccurred())
		g.Expect(err.Error()).To(gomega.ContainSubstring("multiple N/S NICs found"))
	})
}

func createPCIDevice(g *gomega.WithT, pciAddress, deviceID string) string {
	root, err := os.MkdirTemp("", "dpuagent-util-sysfs-*")
	g.Expect(err).NotTo(gomega.HaveOccurred())
	writePCIDevice(g, root, pciAddress, deviceID)
	return root
}

func writePCIDevice(g *gomega.WithT, root, pciAddress, deviceID string) {
	devicePath := filepath.Join(root, "bus/pci/devices", pciAddress)
	g.Expect(os.MkdirAll(devicePath, 0755)).To(gomega.Succeed())
	g.Expect(os.WriteFile(filepath.Join(devicePath, "device"), []byte(deviceID+"\n"), 0644)).To(gomega.Succeed())
	g.Expect(os.WriteFile(filepath.Join(devicePath, "vpd"), vpdWithSerialNumber("MT2334XZ0L"), 0644)).To(gomega.Succeed())
}

func vpdWithSerialNumber(serialNumber string) []byte {
	fieldData := []byte("SN")
	fieldData = append(fieldData, byte(len(serialNumber)))
	fieldData = append(fieldData, []byte(serialNumber)...)
	return append([]byte{0x90, byte(len(fieldData)), 0x00}, fieldData...)
}
