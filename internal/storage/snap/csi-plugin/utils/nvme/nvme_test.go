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
	"github.com/nvidia/doca-platform/test/utils/fakefs"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

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
				},
				Files: []fakefs.FileEntry{
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.0/nvme/nvme1/nvme0n1/nsid", Data: []byte("1\n")},
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.2/nvme/nvme3/nvme0n3/nsid", Data: []byte("3\n")},
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.2/nvme/nvme3/nvme0n3/size", Data: []byte("4194304\n")},
				},
			})
			dev, err := nvmeUtils.GetBlockDeviceNameForNS("0000:b1:0c.2", int32(3))
			Expect(err).NotTo(HaveOccurred())
			Expect(dev).To(Equal("nvme0n3"))
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
				},
				Files: []fakefs.FileEntry{
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.2/nvme/nvme3/nvme0n3/nsid", Data: []byte("3\n")},
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.2/nvme/nvme3/nvme0n3/size", Data: []byte("0\n")},
				},
			})
			_, err := nvmeUtils.GetBlockDeviceNameForNS("0000:b1:0c.2", int32(3))
			Expect(err).To(MatchError(ErrBlockDeviceIsInvalid))
		})
		It("zero size - no size attribute", func() {
			fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
				Dirs: []fakefs.DirEntry{
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.2/nvme/nvme3/nvme0n3"},
				},
				Files: []fakefs.FileEntry{
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.2/nvme/nvme3/nvme0n3/nsid", Data: []byte("3\n")},
				},
			})
			_, err := nvmeUtils.GetBlockDeviceNameForNS("0000:b1:0c.2", int32(3))
			Expect(err).To(MatchError(ErrBlockDeviceIsInvalid))
		})
		It("zero size - unexpected data", func() {
			fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
				Dirs: []fakefs.DirEntry{
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.2/nvme/nvme3/nvme0n3"},
				},
				Files: []fakefs.FileEntry{
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.2/nvme/nvme3/nvme0n3/nsid", Data: []byte("3\n")},
					{Path: "/sys/bus/pci/drivers/nvme/0000:b1:0c.2/nvme/nvme3/nvme0n3/size", Data: []byte("wrong\n")},
				},
			})
			_, err := nvmeUtils.GetBlockDeviceNameForNS("0000:b1:0c.2", int32(3))
			Expect(err).To(MatchError(ErrBlockDeviceIsInvalid))
		})
	})
})
