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
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// --- test helpers ---

func devlinkJSON(ports map[string]DevlinkPortEntry) string {
	b, _ := json.Marshal(devlinkPortShowJSON{Port: ports})
	return string(b)
}

func writeUevent(root, netdev, pciSlotName string) {
	dir := filepath.Join(root, netdev, "device")
	ExpectWithOffset(1, os.MkdirAll(dir, 0755)).To(Succeed())
	ExpectWithOffset(1, os.WriteFile(filepath.Join(dir, "uevent"),
		[]byte(fmt.Sprintf("DRIVER=mlx5_core\nPCI_SLOT_NAME=%s\n", pciSlotName)), 0644)).To(Succeed())
}

func writePCIDeviceID(root, bdf, deviceID string) {
	dir := filepath.Join(root, bdf)
	ExpectWithOffset(1, os.MkdirAll(dir, 0755)).To(Succeed())
	ExpectWithOffset(1, os.WriteFile(filepath.Join(dir, "device"), []byte(deviceID+"\n"), 0644)).To(Succeed())
}

func mockRunBash(devlinkResponse string) func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
	return func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
		switch cmd {
		case "devlink port show -j":
			var buf bytes.Buffer
			buf.WriteString(devlinkResponse)
			return buf, bytes.Buffer{}, nil
		default:
			return bytes.Buffer{}, bytes.Buffer{}, fmt.Errorf("unexpected command: %s", cmd)
		}
	}
}

func setSysfsNetPathForTest(path string) {
	original := sysfsNetPath
	sysfsNetPath = path
	DeferCleanup(func() { sysfsNetPath = original })
}

func setSysfsPCIDevicesPathForTest(path string) {
	original := sysfsPCIDevicesPath
	sysfsPCIDevicesPath = path
	DeferCleanup(func() { sysfsPCIDevicesPath = original })
}

func overridePathVars(netRoot string) {
	origNet := sysfsNetPath
	origPCIDevices := sysfsPCIDevicesPath
	sysfsNetPath = netRoot
	sysfsPCIDevicesPath = filepath.Join(filepath.Dir(filepath.Dir(netRoot)), "bus", "pci", "devices")
	DeferCleanup(func() {
		sysfsNetPath = origNet
		sysfsPCIDevicesPath = origPCIDevices
	})
}

func excludePCI(addrs ...string) func(*NICPort) bool {
	blocked := map[string]struct{}{}
	for _, a := range addrs {
		blocked[a] = struct{}{}
	}
	return func(port *NICPort) bool {
		_, ok := blocked[port.PCIAddress]
		return !ok
	}
}

// --- tests ---

var _ = Describe("NormalizeAddress", func() {
	DescribeTable("normalizes PCI addresses",
		func(address, expected string) {
			Expect(NormalizeAddress(address)).To(Equal(expected))
		},
		Entry("full address", "0000:03:00.0", "0000:03:00.0"),
		Entry("short address", "03:00.0", "0000:03:00.0"),
		Entry("uppercase and whitespace", " 0000:AB:CD.1\n", "0000:ab:cd.1"),
		Entry("empty", "", ""),
	)
})

var _ = Describe("NetdevPCI", func() {
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

var _ = Describe("NSPortFilter", func() {
	It("should return true for known N/S NIC device IDs (BF2, BF3, BF4)", func() {
		Expect(NSPortFilter(&NICPort{DeviceID: bluefield2DeviceID})).To(BeTrue())
		Expect(NSPortFilter(&NICPort{DeviceID: bluefield3DeviceID})).To(BeTrue())
		Expect(NSPortFilter(&NICPort{DeviceID: bluefield4DeviceID})).To(BeTrue())
	})

	It("should return false for unknown device IDs", func() {
		Expect(NSPortFilter(&NICPort{DeviceID: "0xffff"})).To(BeFalse())
		Expect(NSPortFilter(&NICPort{})).To(BeFalse())
	})
})

var _ = Describe("DiscoverNSPFRepresentors", func() {
	It("should discover PF representors from N/S ECPF sysfs devices and devlink", func() {
		root := GinkgoT().TempDir()
		pciDevicesRoot := filepath.Join(root, "bus", "pci", "devices")
		setSysfsPCIDevicesPathForTest(pciDevicesRoot)
		writePCIDeviceID(pciDevicesRoot, "0002:01:00.0", "0xa2df")
		writePCIDeviceID(pciDevicesRoot, "0002:01:00.1", "0xa2df")
		writePCIDeviceID(pciDevicesRoot, "0006:01:00.0", "0xa2df")
		writePCIDeviceID(pciDevicesRoot, "0006:01:00.1", "0xa2df")
		writePCIDeviceID(pciDevicesRoot, "0001:03:00.0", "0xffff")

		d := &PortDiscoverer{
			runBash: mockRunBash(devlinkJSON(map[string]DevlinkPortEntry{
				"pci/0001:03:00.0/262144":             {Netdev: "eth46", Flavor: "pcipf"},
				"pci/0002:01:00.0/327680":             {Netdev: "B21c1pf0", Flavor: "pcipf"},
				"pci/0002:01:00.0/360448":             {Netdev: "B21pf0sf0", Flavor: "pcisf"},
				"pci/0002:01:00.1/393216":             {Netdev: "B21c2pf0", Flavor: "pcipf"},
				"auxiliary/mlx5_core.eth.12/393215":   {Netdev: "p0", Flavor: "physical"},
				"pci/0006:01:00.0/458752":             {Netdev: "B61c1pf1", Flavor: "pcipf"},
				"pci/0006:01:00.0/491520":             {Netdev: "B61c4pf0sf0", Flavor: "pcisf"},
				"pci/0006:01:00.1/524288":             {Netdev: "B61c2pf1", Flavor: "pcipf"},
				"auxiliary/mlx5_core.eth.30/524287":   {Netdev: "p1", Flavor: "physical"},
				"auxiliary/mlx5_core.eth.40/13238272": {Netdev: "enP2p1s0f0S8", Flavor: "virtual"},
			})),
		}

		pfReps, err := d.DiscoverNSPFRepresentors()
		Expect(err).NotTo(HaveOccurred())
		Expect(pfReps).To(ConsistOf("B21c1pf0", "B61c1pf1", "B21c2pf0", "B61c2pf1"))
	})
})

var _ = Describe("DiscoverPorts", func() {
	It("should join devlink and sysfs data and apply filter", func() {
		root := GinkgoT().TempDir()

		netRoot := filepath.Join(root, "sys", "class", "net")
		// BF3-style: same domain
		writeUevent(netRoot, "p0", "0000:03:00.0")
		writeUevent(netRoot, "p1", "0000:03:00.1")
		// BF4-style: cross domain
		writeUevent(netRoot, "p3", "0002:01:00.0")
		writeUevent(netRoot, "p4", "0006:01:00.0")

		pciDevicesRoot := filepath.Join(root, "sys", "bus", "pci", "devices")
		writePCIDeviceID(pciDevicesRoot, "0000:03:00.0", bluefield3DeviceID)
		writePCIDeviceID(pciDevicesRoot, "0000:03:00.1", bluefield3DeviceID)
		writePCIDeviceID(pciDevicesRoot, "0002:01:00.0", bluefield4DeviceID)
		writePCIDeviceID(pciDevicesRoot, "0006:01:00.0", bluefield4DeviceID)
		overridePathVars(netRoot)

		d := &PortDiscoverer{
			runBash: mockRunBash(devlinkJSON(map[string]DevlinkPortEntry{
				"auxiliary/mlx5_core.eth.0/262143": {Netdev: "p0", Flavor: "physical"},
				"auxiliary/mlx5_core.eth.1/327679": {Netdev: "p1", Flavor: "physical"},
				"auxiliary/mlx5_core.eth.2/393215": {Netdev: "p3", Flavor: "physical"},
				"auxiliary/mlx5_core.eth.3/458751": {Netdev: "p4", Flavor: "physical"},
				"pci/0000:03:00.0/196608":          {Netdev: "custompf0", Flavor: "pcipf"},
				"pci/0002:01:00.0/458752":          {Netdev: "B21c1pf7", Flavor: "pcipf"},
				"pci/0000:03:00.0/229377":          {Netdev: "en3f0pf0sf1", Flavor: "pcisf"},
				"pci/0002:01:00.0/458754":          {Flavor: "virtual"},
			})),
		}

		// Filter excludes p1 and p4
		ports, err := d.DiscoverPhysicalPort(excludePCI("0000:03:00.1", "0006:01:00.0"))
		Expect(err).NotTo(HaveOccurred())
		Expect(ports).To(HaveLen(2))

		portMap := map[string]NICPort{}
		for _, p := range ports {
			portMap[p.Netdev] = p
		}
		Expect(portMap).To(HaveKey("p0"))
		Expect(portMap["p0"].PCIAddress).To(Equal("0000:03:00.0"))
		Expect(portMap).To(HaveKey("p3"))
		Expect(portMap["p3"].PCIAddress).To(Equal("0002:01:00.0"))
	})

	It("should discover N/S ports", func() {
		root := GinkgoT().TempDir()

		netRoot := filepath.Join(root, "sys", "class", "net")
		writeUevent(netRoot, "p0", "0000:03:00.0")
		writeUevent(netRoot, "p1", "0002:01:00.0")
		writeUevent(netRoot, "eth0", "000a:01:00.0")

		pciDevicesRoot := filepath.Join(root, "sys", "bus", "pci", "devices")
		writePCIDeviceID(pciDevicesRoot, "0000:03:00.0", "0xa2dc")
		writePCIDeviceID(pciDevicesRoot, "0002:01:00.0", "0xa2df")
		writePCIDeviceID(pciDevicesRoot, "000a:01:00.0", "0xffff")
		overridePathVars(netRoot)

		d := &PortDiscoverer{
			runBash: mockRunBash(devlinkJSON(map[string]DevlinkPortEntry{
				"auxiliary/mlx5_core.eth.0/262143": {Netdev: "p0", Flavor: "physical"},
				"auxiliary/mlx5_core.eth.1/327679": {Netdev: "p1", Flavor: "physical"},
				"auxiliary/mlx5_core.eth.2/393215": {Netdev: "eth0", Flavor: "physical"},
				"pci/0000:03:00.0/196608":          {Netdev: "pf0hpf", Flavor: "pcipf"},
				"pci/0002:01:00.0/458752":          {Netdev: "B21c1pf0", Flavor: "pcipf"},
			})),
		}

		ports, err := d.DiscoverPhysicalPort(NSPortFilter)
		Expect(err).NotTo(HaveOccurred())
		Expect(ports).To(HaveLen(2))

		portMap := map[string]NICPort{}
		for _, p := range ports {
			portMap[p.Netdev] = p
		}
		Expect(portMap["p0"].DeviceID).To(Equal(bluefield3DeviceID))
		Expect(portMap["p1"].DeviceID).To(Equal(bluefield4DeviceID))
		Expect(portMap).NotTo(HaveKey("eth0"))
	})

})
