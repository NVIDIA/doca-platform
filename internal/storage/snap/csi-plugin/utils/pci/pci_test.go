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

package pci

import (
	"context"
	"fmt"

	"github.com/nvidia/doca-platform/test/utils/fakefs"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	kexec "k8s.io/utils/exec"
	kexecTesting "k8s.io/utils/exec/testing"
)

var _ = Describe("Pci Utils package test", func() {
	Describe("helpers", func() {
		Context("ValidatePCIAddress", func() {
			It("valid", func() {
				add, err := ParsePCIAddress("0000:3b:00.2")
				Expect(err).NotTo(HaveOccurred())
				Expect(add).To(Equal("0000:3b:00.2"))
			})
			It("Valid - no leading zeroes", func() {
				add, err := ParsePCIAddress("3b:00.2")
				Expect(err).NotTo(HaveOccurred())
				Expect(add).To(Equal("0000:3b:00.2"))
			})
			It("invalid - empty", func() {
				_, err := ParsePCIAddress("")
				Expect(err).To(MatchError(ContainSubstring("invalid PCI address format")))
			})
			It("invalid - invalid format", func() {
				_, err := ParsePCIAddress("foobar")
				Expect(err).To(MatchError(ContainSubstring("invalid PCI address format")))
			})
		})
	})
	Describe("utils", func() {
		var (
			pciUtils Utils
			testCtrl *gomock.Controller
			fakeExec *kexecTesting.FakeExec
			ctx      context.Context
		)
		BeforeEach(func() {
			fakeExec = &kexecTesting.FakeExec{}
			testCtrl = gomock.NewController(GinkgoT())
			pciUtils = New("/host", fakeExec)
			ctx = context.Background()
		})
		AfterEach(func() {
			testCtrl.Finish()
		})
		Context("LoadDriver", func() {
			It("loaded", func() {
				f := fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
					Dirs: []fakefs.DirEntry{
						{Path: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2"},
						{Path: "/sys/bus/pci/devices"},
						{Path: "/sys/bus/pci/drivers/nvme"},
					},
					Files: []fakefs.FileEntry{
						{Path: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2/driver_override"},
						{Path: "/sys/bus/pci/drivers/nvme/bind"},
					},
					Symlinks: []fakefs.SymlinkEntry{
						{OldPath: "../../../devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2", NewPath: "/sys/bus/pci/devices/0000:b1:00.2"},
					},
				})
				Expect(pciUtils.LoadDriver("0000:b1:00.2", "nvme")).NotTo(HaveOccurred())
				fakefs.GinkgoFakeFsFileContent(f, "/sys/bus/pci/devices/0000:b1:00.2/driver_override").To(Equal("nvme"))
				fakefs.GinkgoFakeFsFileContent(f, "/sys/bus/pci/drivers/nvme/bind").To(Equal("0000:b1:00.2"))
			})
			It("already loaded", func() {
				fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
					Dirs: []fakefs.DirEntry{
						{Path: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2"},
						{Path: "/sys/bus/pci/devices"},
						{Path: "/sys/bus/pci/drivers/nvme"},
					},
					Symlinks: []fakefs.SymlinkEntry{
						{OldPath: "../../../devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2", NewPath: "/sys/bus/pci/devices/0000:b1:00.2"},
						{OldPath: "../../../../bus/pci/drivers/nvme", NewPath: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2/driver"},
					},
				})
				Expect(pciUtils.LoadDriver("0000:b1:00.2", "nvme")).NotTo(HaveOccurred())
			})
			It("failed to configure driver_override", func() {
				fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
					Dirs: []fakefs.DirEntry{
						{Path: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2"},
						{Path: "/sys/bus/pci/devices"},
						{Path: "/sys/bus/pci/drivers/nvme"},
						// use dir instead of the file, this will cause EISDIR error on file read/write
						{Path: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2/driver_override"},
					},
					Symlinks: []fakefs.SymlinkEntry{
						{OldPath: "../../../devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2", NewPath: "/sys/bus/pci/devices/0000:b1:00.2"},
					},
				})
				Expect(pciUtils.LoadDriver("0000:b1:00.2", "nvme")).To(MatchError(ContainSubstring("failed to configure driver override")))
			})
			It("failed to bind driver", func() {
				f := fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
					Dirs: []fakefs.DirEntry{
						{Path: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2"},
						{Path: "/sys/bus/pci/devices"},
						{Path: "/sys/bus/pci/drivers/nvme"},
						// use dir instead of the file, this will cause EISDIR error on file read/write
						{Path: "/sys/bus/pci/drivers/nvme/bind"},
					},
					Files: []fakefs.FileEntry{
						{Path: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2/driver_override"},
					},
					Symlinks: []fakefs.SymlinkEntry{
						{OldPath: "../../../devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2", NewPath: "/sys/bus/pci/devices/0000:b1:00.2"},
					},
				})
				Expect(pciUtils.LoadDriver("0000:b1:00.2", "nvme")).To(MatchError(ContainSubstring("failed to bind device to the driver")))
				fakefs.GinkgoFakeFsFileContent(f, "/sys/bus/pci/devices/0000:b1:00.2/driver_override").To(Equal("nvme"))
			})
			It("wrong driver", func() {
				fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
					Dirs: []fakefs.DirEntry{
						{Path: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2"},
						{Path: "/sys/bus/pci/devices"},
						{Path: "/sys/bus/pci/drivers/testdriver"},
					},
					Symlinks: []fakefs.SymlinkEntry{
						{OldPath: "../../../devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2", NewPath: "/sys/bus/pci/devices/0000:b1:00.2"},
						{OldPath: "../../../../bus/pci/drivers/testdriver", NewPath: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2/driver"},
					},
				})
				Expect(pciUtils.LoadDriver("0000:b1:00.2", "nvme")).To(
					MatchError(ContainSubstring("device is already binded to a different driver")))
			})
		})
		Context("UnloadDriver", func() {
			It("loaded", func() {
				f := fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
					Dirs: []fakefs.DirEntry{
						{Path: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2"},
						{Path: "/sys/bus/pci/devices"},
						{Path: "/sys/bus/pci/drivers/nvme"},
					},
					Files: []fakefs.FileEntry{
						{Path: "/sys/bus/pci/drivers/nvme/unbind"},
					},
					Symlinks: []fakefs.SymlinkEntry{
						{OldPath: "../../../devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2", NewPath: "/sys/bus/pci/devices/0000:b1:00.2"},
						{OldPath: "../../../../bus/pci/drivers/nvme", NewPath: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2/driver"},
					},
				})
				Expect(pciUtils.UnloadDriver("0000:b1:00.2")).NotTo(HaveOccurred())
				fakefs.GinkgoFakeFsFileContent(f, "/sys/bus/pci/drivers/nvme/unbind").To(Equal("0000:b1:00.2"))
			})
			It("no driver", func() {
				fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
					Dirs: []fakefs.DirEntry{
						{Path: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2"},
						{Path: "/sys/bus/pci/devices"},
					},
					Symlinks: []fakefs.SymlinkEntry{
						{OldPath: "../../../devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2", NewPath: "/sys/bus/pci/devices/0000:b1:00.2"},
					},
				})
				Expect(pciUtils.UnloadDriver("0000:b1:00.2")).NotTo(HaveOccurred())
			})
			It("failed to unbind driver", func() {
				fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
					Dirs: []fakefs.DirEntry{
						{Path: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2"},
						{Path: "/sys/bus/pci/devices"},
						{Path: "/sys/bus/pci/drivers/nvme"},
						// use dir instead of the file, this will cause EISDIR error on file read/write
						{Path: "/sys/bus/pci/drivers/nvme/unbind"},
					},
					Symlinks: []fakefs.SymlinkEntry{
						{OldPath: "../../../devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2", NewPath: "/sys/bus/pci/devices/0000:b1:00.2"},
						{OldPath: "../../../../bus/pci/drivers/nvme", NewPath: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2/driver"},
					},
				})
				Expect(pciUtils.UnloadDriver("0000:b1:00.2")).To(MatchError(ContainSubstring("failed to unbind device from the driver")))
			})
		})
		Context("GetPFs", func() {
			It("return PF", func() {
				fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
					Dirs: []fakefs.DirEntry{
						{Path: "/sys/bus/pci/devices"},
						{Path: "/sys/bus/pci/drivers/nvme"},
						// pf
						{Path: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2"},
						// vf
						{Path: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:0c.0"},
					},
					Files: []fakefs.FileEntry{
						// pf
						{Path: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2/modalias",
							Data: []byte("pci:v000015B3d00006001sv00000000sd00000000bc01sc08i02")},
						// vf
						{Path: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:0c.0/modalias",
							Data: []byte("pci:v000015B3d00006001sv000015B3sd00002009bc01sc08i02")},
					},
					Symlinks: []fakefs.SymlinkEntry{
						// pf
						{OldPath: "../../../devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2", NewPath: "/sys/bus/pci/devices/0000:b1:00.2"},
						{OldPath: "../../../../bus/pci/drivers/nvme", NewPath: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2/driver"},
						// vf
						{OldPath: "../../../devices/pci0000:b0/0000:b0:04.0/0000:b1:0c.0", NewPath: "/sys/bus/pci/devices/0000:b1:0c.0"},
						{OldPath: "../../../../bus/pci/drivers/nvme", NewPath: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:0c.0/driver"},
						{OldPath: "../0000:b1:00.2", NewPath: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:0c.0/physfn"},
					},
				})
				dev, err := pciUtils.GetPFs("15b3", []string{"6001"})
				Expect(err).NotTo(HaveOccurred())
				Expect(dev).To(HaveLen(1))
				Expect(dev[0].Address).To(Equal("0000:b1:00.2"))
				Expect(dev[0].VendorID).To(Equal("15b3"))
				Expect(dev[0].DeviceID).To(Equal("6001"))
				Expect(dev[0].Driver).To(Equal("nvme"))
			})
			It("not found", func() {
				fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
					Dirs: []fakefs.DirEntry{
						{Path: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2"},
						{Path: "/sys/bus/pci/devices"},
						{Path: "/sys/bus/pci/drivers/nvme"},
					},
					Files: []fakefs.FileEntry{
						{Path: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2/modalias",
							Data: []byte("pci:v000015B3d00006001sv00000000sd00000000bc01sc08i02")},
					},
					Symlinks: []fakefs.SymlinkEntry{
						{OldPath: "../../../devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2", NewPath: "/sys/bus/pci/devices/0000:b1:00.2"},
						{OldPath: "../../../../bus/pci/drivers/nvme", NewPath: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2/driver"},
					},
				})
				dev, err := pciUtils.GetPFs("15b3", []string{"5555"})
				Expect(err).NotTo(HaveOccurred())
				Expect(dev).To(BeEmpty())
			})
			It("not devices folder in sysfs", func() {
				fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{})
				_, err := pciUtils.GetPFs("15b3", []string{"6001"})
				Expect(err).To(MatchError(ContainSubstring("failed to read devices from sysfs")))
			})
		})
		Context("DisableSriovVfsDriverAutoprobe", func() {
			It("succeed", func() {
				f := fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
					Dirs: []fakefs.DirEntry{
						{Path: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2"},
						{Path: "/sys/bus/pci/devices"},
					},
					Files: []fakefs.FileEntry{
						{Path: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2/sriov_drivers_autoprobe",
							Data: []byte("1")},
					},
					Symlinks: []fakefs.SymlinkEntry{
						{OldPath: "../../../devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2", NewPath: "/sys/bus/pci/devices/0000:b1:00.2"},
					},
				})
				Expect(pciUtils.DisableSriovVfsDriverAutoprobe("0000:b1:00.2")).NotTo(HaveOccurred())
				fakefs.GinkgoFakeFsFileContent(f, "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2/sriov_drivers_autoprobe").To(Equal("0"))
			})
			It("failed", func() {
				fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
					Dirs: []fakefs.DirEntry{
						{Path: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2"},
						{Path: "/sys/bus/pci/devices"},
						// use dir instead of the file, this will cause EISDIR error on file read/write
						{Path: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2/sriov_drivers_autoprobe"},
					},
					Symlinks: []fakefs.SymlinkEntry{
						{OldPath: "../../../devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2", NewPath: "/sys/bus/pci/devices/0000:b1:00.2"},
					},
				})
				Expect(pciUtils.DisableSriovVfsDriverAutoprobe("0000:b1:00.2")).To(MatchError(ContainSubstring("sriov_drivers_autoprobe")))
			})
		})
		Context("SetSriovNumVfs", func() {
			It("succeed", func() {
				f := fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
					Dirs: []fakefs.DirEntry{
						{Path: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2"},
						{Path: "/sys/bus/pci/devices"},
					},
					Files: []fakefs.FileEntry{
						{Path: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2/sriov_numvfs",
							Data: []byte("0")},
					},
					Symlinks: []fakefs.SymlinkEntry{
						{OldPath: "../../../devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2", NewPath: "/sys/bus/pci/devices/0000:b1:00.2"},
					},
				})
				Expect(pciUtils.SetSriovNumVfs("0000:b1:00.2", 125)).NotTo(HaveOccurred())
				fakefs.GinkgoFakeFsFileContent(f, "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2/sriov_numvfs").To(Equal("125"))
			})
			It("failed", func() {
				fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
					Dirs: []fakefs.DirEntry{
						{Path: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2"},
						{Path: "/sys/bus/pci/devices"},
						// use dir instead of the file, this will cause EISDIR error on file read/write
						{Path: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2/sriov_numvfs"},
					},
					Symlinks: []fakefs.SymlinkEntry{
						{OldPath: "../../../devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2", NewPath: "/sys/bus/pci/devices/0000:b1:00.2"},
					},
				})
				Expect(pciUtils.SetSriovNumVfs("0000:b1:00.2", 125)).To(MatchError(ContainSubstring("sriov_numvfs")))
			})
		})
		Context("GetSRIOVNumVFs", func() {
			It("succeed", func() {
				fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
					Dirs: []fakefs.DirEntry{
						{Path: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2"},
						{Path: "/sys/bus/pci/devices"},
					},
					Files: []fakefs.FileEntry{
						{Path: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2/sriov_numvfs",
							Data: []byte("3")},
					},
					Symlinks: []fakefs.SymlinkEntry{
						{OldPath: "../../../devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2", NewPath: "/sys/bus/pci/devices/0000:b1:00.2"},
					},
				})
				vfCount, err := pciUtils.GetSRIOVNumVFs("0000:b1:00.2")
				Expect(err).NotTo(HaveOccurred())
				Expect(vfCount).To(Equal(3))
			})
			It("failed", func() {
				fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
					Dirs: []fakefs.DirEntry{
						{Path: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2"},
						{Path: "/sys/bus/pci/devices"},
						// use dir instead of the file, this will cause EISDIR error on file read/write
						{Path: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2/sriov_numvfs"},
					},
					Symlinks: []fakefs.SymlinkEntry{
						{OldPath: "../../../devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2", NewPath: "/sys/bus/pci/devices/0000:b1:00.2"},
					},
				})
				_, err := pciUtils.GetSRIOVNumVFs("0000:b1:00.2")
				Expect(err).To(MatchError(ContainSubstring("sriov_numvfs")))
			})
		})
		Context("GetSRIOVTotalVFs", func() {
			It("succeed", func() {
				fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
					Dirs: []fakefs.DirEntry{
						{Path: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2"},
						{Path: "/sys/bus/pci/devices"},
					},
					Files: []fakefs.FileEntry{
						{Path: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2/sriov_totalvfs",
							Data: []byte("3")},
					},
					Symlinks: []fakefs.SymlinkEntry{
						{OldPath: "../../../devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2", NewPath: "/sys/bus/pci/devices/0000:b1:00.2"},
					},
				})
				vfCount, err := pciUtils.GetSRIOVTotalVFs("0000:b1:00.2")
				Expect(err).NotTo(HaveOccurred())
				Expect(vfCount).To(Equal(3))
			})
			It("failed", func() {
				fakefs.GinkgoConfigureFakeFS(&fsRoot, fakefs.Config{
					Dirs: []fakefs.DirEntry{
						{Path: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2"},
						{Path: "/sys/bus/pci/devices"},
						// use dir instead of the file, this will cause EISDIR error on file read/write
						{Path: "/sys/devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2/sriov_totalvfs"},
					},
					Symlinks: []fakefs.SymlinkEntry{
						{OldPath: "../../../devices/pci0000:b0/0000:b0:04.0/0000:b1:00.2", NewPath: "/sys/bus/pci/devices/0000:b1:00.2"},
					},
				})
				_, err := pciUtils.GetSRIOVTotalVFs("0000:b1:00.2")
				Expect(err).To(MatchError(ContainSubstring("failed to read sriov_totalvfs for device 0000:b1:00.2")))
			})
		})
		Context("InsertKernelModule", func() {
			var (
				fakeCmd *kexecTesting.FakeCmd
			)
			BeforeEach(func() {
				fakeCmd = &kexecTesting.FakeCmd{}
				fakeExec.CommandScript = []kexecTesting.FakeCommandAction{
					func(cmd string, args ...string) kexec.Cmd {
						return kexecTesting.InitFakeCmd(fakeCmd, cmd, args...)
					}}
			})
			It("no chroot - succeed", func() {
				fakeCmd.RunScript = []kexecTesting.FakeAction{func() ([]byte, []byte, error) {
					return nil, nil, nil
				}}
				pciUtils = New("", fakeExec)
				Expect(pciUtils.InsertKernelModule(ctx, "nvme")).NotTo(HaveOccurred())
				Expect(fakeCmd.RunCalls).To(Equal(1))
				Expect(fakeCmd.RunLog[0]).To(Equal([]string{"modprobe", "nvme"}))
			})
			It("no chroot - failed", func() {
				fakeCmd.RunScript = []kexecTesting.FakeAction{func() ([]byte, []byte, error) {
					return nil, nil, fmt.Errorf("test error")
				}}
				pciUtils = New("", fakeExec)
				Expect(pciUtils.InsertKernelModule(ctx, "nvme")).To(MatchError(ContainSubstring("test error")))
				Expect(fakeCmd.RunCalls).To(Equal(1))
				Expect(fakeCmd.RunLog[0]).To(Equal([]string{"modprobe", "nvme"}))
			})
			It("chroot - succeed", func() {
				fakeCmd.RunScript = []kexecTesting.FakeAction{func() ([]byte, []byte, error) {
					return nil, nil, nil
				}}
				pciUtils = New("/host", fakeExec)
				Expect(pciUtils.InsertKernelModule(ctx, "nvme")).NotTo(HaveOccurred())
				Expect(fakeCmd.RunCalls).To(Equal(1))
				Expect(fakeCmd.RunLog[0]).To(Equal([]string{"chroot", "/host", "modprobe", "nvme"}))
			})
			It("chroot - failed", func() {
				fakeCmd.RunScript = []kexecTesting.FakeAction{func() ([]byte, []byte, error) {
					return nil, nil, fmt.Errorf("test error")
				}}
				pciUtils = New("/host", fakeExec)
				Expect(pciUtils.InsertKernelModule(ctx, "nvme")).To(MatchError(ContainSubstring("test error")))
				Expect(fakeCmd.RunCalls).To(Equal(1))
				Expect(fakeCmd.RunLog[0]).To(Equal([]string{"chroot", "/host", "modprobe", "nvme"}))
			})
		})
	})
})
