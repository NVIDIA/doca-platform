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

func writeMSTDevice(path, pciAddress string) {
	ExpectWithOffset(1, os.WriteFile(path, []byte(fmt.Sprintf("domain:bus:dev.fn=%s addr.reg=88 data.reg=92", pciAddress)), 0644)).To(Succeed())
}

func writeUevent(root, netdev, pciSlotName string) {
	dir := filepath.Join(root, netdev, "device")
	ExpectWithOffset(1, os.MkdirAll(dir, 0755)).To(Succeed())
	ExpectWithOffset(1, os.WriteFile(filepath.Join(dir, "uevent"),
		[]byte(fmt.Sprintf("DRIVER=mlx5_core\nPCI_SLOT_NAME=%s\n", pciSlotName)), 0644)).To(Succeed())
}

func mockRunBash(devlinkResponse string) func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
	return func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
		switch cmd {
		case "devlink port show -j":
			var buf bytes.Buffer
			buf.WriteString(devlinkResponse)
			return buf, bytes.Buffer{}, nil
		case "mst start":
			return bytes.Buffer{}, bytes.Buffer{}, nil
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

func overridePathVars(netRoot, mstDir string) {
	origNet := sysfsNetPath
	origMST := mstDevicesPath
	sysfsNetPath = netRoot
	mstDevicesPath = mstDir
	DeferCleanup(func() {
		sysfsNetPath = origNet
		mstDevicesPath = origMST
	})
}

func excludePCI(addrs ...string) func(NICPort) bool {
	blocked := map[string]struct{}{}
	for _, a := range addrs {
		blocked[a] = struct{}{}
	}
	return func(port NICPort) bool {
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

var _ = Describe("DiscoverPorts", func() {
	It("should join devlink, sysfs, and MST data and apply filter", func() {
		root := GinkgoT().TempDir()

		netRoot := filepath.Join(root, "sys", "class", "net")
		// BF3-style: same domain
		writeUevent(netRoot, "p0", "0000:03:00.0")
		writeUevent(netRoot, "p1", "0000:03:00.1")
		// BF4-style: cross domain
		writeUevent(netRoot, "p3", "0002:01:00.0")
		writeUevent(netRoot, "p4", "0006:01:00.0")

		mstDir := filepath.Join(root, "dev", "mst")
		Expect(os.MkdirAll(mstDir, 0755)).To(Succeed())
		writeMSTDevice(filepath.Join(mstDir, "mt41692_pciconf0"), "0000:03:00.0")
		writeMSTDevice(filepath.Join(mstDir, "mt41692_pciconf0.1"), "0000:03:00.1")
		writeMSTDevice(filepath.Join(mstDir, "mt41695_pciconf0"), "0002:01:00.0")
		writeMSTDevice(filepath.Join(mstDir, "mt41695_pciconf1"), "0006:01:00.0")

		overridePathVars(netRoot, mstDir)

		d := &PortDiscoverer{
			runBash: mockRunBash(devlinkJSON(map[string]DevlinkPortEntry{
				"auxiliary/mlx5_core.eth.0/262143": {Netdev: "p0", Flavor: "physical"},
				"auxiliary/mlx5_core.eth.1/327679": {Netdev: "p1", Flavor: "physical"},
				"auxiliary/mlx5_core.eth.2/393215": {Netdev: "p3", Flavor: "physical"},
				"auxiliary/mlx5_core.eth.3/458751": {Netdev: "p4", Flavor: "physical"},
				"pci/0000:03:00.0/229377":          {Netdev: "en3f0pf0sf1", Flavor: "pcisf"},
			})),
		}

		// Filter excludes p1 and p4
		ports, err := d.DiscoverPorts(excludePCI("0000:03:00.1", "0006:01:00.0"))
		Expect(err).NotTo(HaveOccurred())
		Expect(ports).To(HaveLen(2))

		portMap := map[string]NICPort{}
		for _, p := range ports {
			portMap[p.Netdev] = p
		}
		Expect(portMap).To(HaveKey("p0"))
		Expect(portMap["p0"].PCIAddress).To(Equal("0000:03:00.0"))
		Expect(portMap["p0"].MSTDevice).To(Equal(filepath.Join(mstDir, "mt41692_pciconf0")))
		Expect(portMap).To(HaveKey("p3"))
		Expect(portMap["p3"].PCIAddress).To(Equal("0002:01:00.0"))
		Expect(portMap["p3"].MSTDevice).To(Equal(filepath.Join(mstDir, "mt41695_pciconf0")))
	})
})

var _ = Describe("pciAddressFromMSTDevice", func() {
	It("should parse PCI address from MST device content", func() {
		path := filepath.Join(GinkgoT().TempDir(), "mt41695_pciconf0")
		writeMSTDevice(path, "0002:01:00.0")

		pci, err := pciAddressFromMSTFile(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(pci).To(Equal("0002:01:00.0"))
	})

	It("should return error for unparseable content", func() {
		path := filepath.Join(GinkgoT().TempDir(), "bad_device")
		Expect(os.WriteFile(path, []byte("garbage"), 0644)).To(Succeed())

		_, err := pciAddressFromMSTFile(path)
		Expect(err).To(HaveOccurred())
	})
})
