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
	"bytes"
	"errors"
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

func TestMFTDevicesForNSNIC(t *testing.T) {
	t.Run("starts mst and selects devices for NIC PFs", func(t *testing.T) {
		g := gomega.NewWithT(t)
		mstDevicesPath := filepath.Join(t.TempDir(), "mst")
		g.Expect(os.MkdirAll(mstDevicesPath, 0755)).To(gomega.Succeed())
		dev1 := filepath.Join(mstDevicesPath, "mt41692_pciconf0")
		dev2 := filepath.Join(mstDevicesPath, "mt41692_pciconf1")
		devOther := filepath.Join(mstDevicesPath, "mt41692_pciconf2")
		writeMSTDevice(g, dev1, "0000:03:00.0")
		writeMSTDevice(g, dev2, "0000:03:00.1")
		writeMSTDevice(g, devOther, "0000:04:00.0")
		var commands []string
		runBash := func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
			commands = append(commands, cmd)
			return bytes.Buffer{}, bytes.Buffer{}, nil
		}

		devices, err := MFTDevicesForNSNIC(mstDevicesPath, &hostutil.Device{Address: "0000:03:00", NumOfPFs: 2}, runBash)

		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(commands).To(gomega.Equal([]string{"mst start"}))
		g.Expect(devices).To(gomega.Equal([]string{dev1, dev2}))
	})

	t.Run("returns mst start stderr on failure", func(t *testing.T) {
		g := gomega.NewWithT(t)
		stderr := bytes.NewBufferString("mst: command not found")
		runBash := func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
			g.Expect(cmd).To(gomega.Equal("mst start"))
			return bytes.Buffer{}, *stderr, errors.New("exit status 127")
		}

		_, err := MFTDevicesForNSNIC(filepath.Join(t.TempDir(), "mst"), &hostutil.Device{Address: "0000:03:00", NumOfPFs: 1}, runBash)

		g.Expect(err).To(gomega.HaveOccurred())
		g.Expect(err.Error()).To(gomega.ContainSubstring("failed to start mst"))
		g.Expect(err.Error()).To(gomega.ContainSubstring("mst: command not found"))
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

func writeMSTDevice(g *gomega.WithT, devicePath, pciAddress string) {
	g.Expect(os.WriteFile(devicePath, []byte("domain:bus:dev.fn="+pciAddress+" addr.reg=88 data.reg=92"), 0644)).To(gomega.Succeed())
}
