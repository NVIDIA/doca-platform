/*
Copyright 2024 NVIDIA

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
package nvme

import (
	"os"
	"path/filepath"

	"github.com/nvidia/doca-platform/test/utils/fakefs"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// concreteUtils returns the implementation behind Utils to reach its unexported methods
func concreteUtils(u Utils) *nvmeUtils { return u.(*nvmeUtils) }

var _ = Describe("Nvme utils tests", func() {
	var (
		nvmeUtils Utils
	)

	BeforeEach(func() {
		nvmeUtils = New()
	})
	Context("GetBlockDeviceNameForNS", func() {
		It("found", func() {
			fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
				Dirs: []fakefs.DirEntry{
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.0/nvme/nvme1/nvme0n1"},
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.1/nvme"},
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.2/nvme/nvme3/nvme0n3"},
					{Path: "/sys/class/block/nvme0n3"},
				},
				Files: []fakefs.FileEntry{
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.0/nvme/nvme1/nvme0n1/nsid", Data: []byte("1\n")},
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.2/nvme/nvme3/nvme0n3/nsid", Data: []byte("3\n")},
					{Path: "/sys/class/block/nvme0n3/size", Data: []byte("4194304\n")},
				},
			})
			dev, err := nvmeUtils.GetBlockDeviceNameForNS("0000:b1:0c.2", int32(3))
			Expect(err).NotTo(HaveOccurred())
			Expect(dev).To(Equal("nvme0n3"))
		})
		It("finds the namespace head for a native multipath device", func() {
			fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
				Dirs: []fakefs.DirEntry{
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.2/nvme/nvme3/nvme0c3n1"},
					{Path: "/sys/class/block/nvme0n1"},
				},
				Files: []fakefs.FileEntry{
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.2/nvme/nvme3/nvme0c3n1/nsid", Data: []byte("3\n")},
					{Path: "/sys/class/block/nvme0n1/size", Data: []byte("4194304\n")},
				},
			})
			dev, err := nvmeUtils.GetBlockDeviceNameForNS("0000:b1:0c.2", int32(3))
			Expect(err).NotTo(HaveOccurred())
			Expect(dev).To(Equal("nvme0n1"))
		})
		It("uses the namespace head instance from a native multipath device name", func() {
			fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
				Dirs: []fakefs.DirEntry{
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.2/nvme/nvme34/nvme12c34n56"},
					{Path: "/sys/class/block/nvme12n56"},
				},
				Files: []fakefs.FileEntry{
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.2/nvme/nvme34/nvme12c34n56/nsid", Data: []byte("3\n")},
					{Path: "/sys/class/block/nvme12n56/size", Data: []byte("4194304\n")},
				},
			})
			dev, err := nvmeUtils.GetBlockDeviceNameForNS("0000:b1:0c.2", int32(3))
			Expect(err).NotTo(HaveOccurred())
			Expect(dev).To(Equal("nvme12n56"))
		})
		It("returns not found while the native multipath namespace head is missing", func() {
			fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
				Dirs: []fakefs.DirEntry{
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.2/nvme/nvme3/nvme0c3n1"},
				},
				Files: []fakefs.FileEntry{
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.2/nvme/nvme3/nvme0c3n1/nsid", Data: []byte("3\n")},
				},
			})
			_, err := nvmeUtils.GetBlockDeviceNameForNS("0000:b1:0c.2", int32(3))
			Expect(err).To(MatchError(ErrBlockDeviceNotFound))
		})
		It("returns invalid for a zero-size native multipath namespace head", func() {
			fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
				Dirs: []fakefs.DirEntry{
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.2/nvme/nvme3/nvme0c3n1"},
					{Path: "/sys/class/block/nvme0n1"},
				},
				Files: []fakefs.FileEntry{
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.2/nvme/nvme3/nvme0c3n1/nsid", Data: []byte("3\n")},
					{Path: "/sys/class/block/nvme0n1/size", Data: []byte("0\n")},
				},
			})
			_, err := nvmeUtils.GetBlockDeviceNameForNS("0000:b1:0c.2", int32(3))
			Expect(err).To(MatchError(ErrBlockDeviceIsInvalid))
		})
		It("returns invalid for a native multipath namespace head with an unexpected size", func() {
			fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
				Dirs: []fakefs.DirEntry{
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.2/nvme/nvme3/nvme0c3n1"},
					{Path: "/sys/class/block/nvme0n1"},
				},
				Files: []fakefs.FileEntry{
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.2/nvme/nvme3/nvme0c3n1/nsid", Data: []byte("3\n")},
					{Path: "/sys/class/block/nvme0n1/size", Data: []byte("wrong\n")},
				},
			})
			_, err := nvmeUtils.GetBlockDeviceNameForNS("0000:b1:0c.2", int32(3))
			Expect(err).To(MatchError(ErrBlockDeviceIsInvalid))
		})
		It("pci device not found", func() {
			fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
				Dirs: []fakefs.DirEntry{
					{Path: "/sys/bus/pci/drivers/nvme"},
				},
			})
			_, err := nvmeUtils.GetBlockDeviceNameForNS("0000:b1:0c.2", int32(3))
			Expect(err).To(MatchError(ErrBlockDeviceNotFound))
		})
		It("nsid not match", func() {
			fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
				Dirs: []fakefs.DirEntry{
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.2/nvme/nvme3/nvme0n3"},
				},
				Files: []fakefs.FileEntry{
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.2/nvme/nvme3/nvme0n3/nsid", Data: []byte("3\n")},
				},
			})
			_, err := nvmeUtils.GetBlockDeviceNameForNS("0000:b1:0c.2", int32(10))
			Expect(err).To(MatchError(ErrBlockDeviceNotFound))
		})
		It("zero size", func() {
			fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
				Dirs: []fakefs.DirEntry{
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.2/nvme/nvme3/nvme0n3"},
					{Path: "/sys/class/block/nvme0n3"},
				},
				Files: []fakefs.FileEntry{
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.2/nvme/nvme3/nvme0n3/nsid", Data: []byte("3\n")},
					{Path: "/sys/class/block/nvme0n3/size", Data: []byte("0\n")},
				},
			})
			_, err := nvmeUtils.GetBlockDeviceNameForNS("0000:b1:0c.2", int32(3))
			Expect(err).To(MatchError(ErrBlockDeviceIsInvalid))
		})
		It("block device not found - no size attribute", func() {
			fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
				Dirs: []fakefs.DirEntry{
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.2/nvme/nvme3/nvme0n3"},
				},
				Files: []fakefs.FileEntry{
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.2/nvme/nvme3/nvme0n3/nsid", Data: []byte("3\n")},
				},
			})
			_, err := nvmeUtils.GetBlockDeviceNameForNS("0000:b1:0c.2", int32(3))
			Expect(err).To(MatchError(ErrBlockDeviceNotFound))
		})
		It("zero size - unexpected data", func() {
			fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
				Dirs: []fakefs.DirEntry{
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.2/nvme/nvme3/nvme0n3"},
					{Path: "/sys/class/block/nvme0n3"},
				},
				Files: []fakefs.FileEntry{
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.2/nvme/nvme3/nvme0n3/nsid", Data: []byte("3\n")},
					{Path: "/sys/class/block/nvme0n3/size", Data: []byte("wrong\n")},
				},
			})
			_, err := nvmeUtils.GetBlockDeviceNameForNS("0000:b1:0c.2", int32(3))
			Expect(err).To(MatchError(ErrBlockDeviceIsInvalid))
		})
	})
	Context("GetPCIAddressForBlockDevicePath", func() {
		It("finds the function of a block device", func() {
			fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
				Dirs: []fakefs.DirEntry{
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.0/nvme/nvme1/nvme1n1"},
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.2/nvme/nvme3/nvme3n1"},
				},
			})
			addr, err := concreteUtils(nvmeUtils).getPCIAddressForBlockDevice("nvme3n1")
			Expect(err).NotTo(HaveOccurred())
			Expect(addr).To(Equal("0000:b1:0c.2"))
		})
		It("finds the function of a native multipath namespace head", func() {
			fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
				Dirs: []fakefs.DirEntry{
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.2/nvme/nvme3/nvme0c3n1"},
				},
			})
			addr, err := concreteUtils(nvmeUtils).getPCIAddressForBlockDevice("nvme0n1")
			Expect(err).NotTo(HaveOccurred())
			Expect(addr).To(Equal("0000:b1:0c.2"))
		})
		It("finds the function if the requested name is a native multipath path device", func() {
			fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
				Dirs: []fakefs.DirEntry{
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.2/nvme/nvme3/nvme2c3n1"},
				},
			})
			addr, err := concreteUtils(nvmeUtils).getPCIAddressForBlockDevice("nvme2c3n1")
			Expect(err).NotTo(HaveOccurred())
			Expect(addr).To(Equal("0000:b1:0c.2"))
		})
		It("returns not found if no function owns the block device", func() {
			fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
				Dirs: []fakefs.DirEntry{
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.2/nvme/nvme3/nvme3n1"},
				},
			})
			_, err := concreteUtils(nvmeUtils).getPCIAddressForBlockDevice("nvme9n1")
			Expect(err).To(MatchError(ErrBlockDeviceNotFound))
		})
		It("returns not found if the driver has no functions", func() {
			fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
				Dirs: []fakefs.DirEntry{{Path: "/sys/bus/pci/drivers/nvme"}},
			})
			_, err := concreteUtils(nvmeUtils).getPCIAddressForBlockDevice("nvme3n1")
			Expect(err).To(MatchError(ErrBlockDeviceNotFound))
		})
		It("returns not found if the path does not exist", func() {
			_, err := nvmeUtils.GetPCIAddressForBlockDevicePath(filepath.Join(GinkgoT().TempDir(), "missing"))
			Expect(err).To(MatchError(ErrBlockDeviceNotFound))
		})
		It("returns not found if the path is not a block device", func() {
			path := filepath.Join(GinkgoT().TempDir(), "file")
			Expect(os.WriteFile(path, []byte("data"), 0o644)).To(Succeed())
			_, err := nvmeUtils.GetPCIAddressForBlockDevicePath(path)
			Expect(err).To(MatchError(ErrBlockDeviceNotFound))
		})
		It("finds the function through the major:minor link of the block device", func() {
			fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
				Dirs: []fakefs.DirEntry{
					{Path: "/sys/dev/block"},
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.0/nvme/nvme1/nvme1n1"},
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.2/nvme/nvme3/nvme3n1"},
				},
				Symlinks: []fakefs.SymlinkEntry{
					{OldPath: "../../devices/pci0000:b0/0000:b0:02.0/0000:b1:0c.2/nvme/nvme3/nvme3n1", NewPath: "/sys/dev/block/259:1"},
				},
			})
			addr, err := concreteUtils(nvmeUtils).getPCIAddressForDevNum(259, 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(addr).To(Equal("0000:b1:0c.2"))
		})
		It("finds the function if the major:minor link points to the namespace head of a native multipath device", func() {
			fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
				Dirs: []fakefs.DirEntry{
					{Path: "/sys/dev/block"},
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.2/nvme/nvme3/nvme2c3n1"},
				},
				Symlinks: []fakefs.SymlinkEntry{
					{OldPath: "../../devices/virtual/nvme-subsystem/nvme-subsys2/nvme2n1", NewPath: "/sys/dev/block/259:1"},
				},
			})
			addr, err := concreteUtils(nvmeUtils).getPCIAddressForDevNum(259, 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(addr).To(Equal("0000:b1:0c.2"))
		})
		It("returns not found if the block device has no major:minor link", func() {
			fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
				Dirs: []fakefs.DirEntry{{Path: "/sys/dev/block"}},
			})
			_, err := concreteUtils(nvmeUtils).getPCIAddressForDevNum(259, 1)
			Expect(err).To(MatchError(ErrBlockDeviceNotFound))
		})
	})
})
