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

package preconfigure

import (
	"context"
	"fmt"
	"time"

	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/config"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/pci"
	pciUtilsMockPkg "github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/pci/mock"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

var errTest = fmt.Errorf("test error")

var _ = Describe("Preconfigure", func() {
	var (
		pciUtils      *pciUtilsMockPkg.MockUtils
		testCtrl      *gomock.Controller
		ctx           context.Context
		cancel        context.CancelFunc
		stop          chan struct{}
		p             Preconfigure
		runtimeConfig *config.NodeRuntime
	)
	BeforeEach(func() {
		stop = make(chan struct{})
		testCtrl = gomock.NewController(GinkgoT())
		pciUtils = pciUtilsMockPkg.NewMockUtils(testCtrl)
		ctx, cancel = context.WithCancel(context.Background())
		runtimeConfig = config.NewNodeRuntime()
	})
	AfterEach(func() {
		testCtrl.Finish()
		Eventually(stop).WithTimeout(time.Second * 5).Should(BeClosed())
	})

	Context("NVMe emulation mode - default configuration", func() {
		BeforeEach(func() {
			p = New(config.Common{
				EmulationMode: config.EmulationModeNVMe,
			}, config.Node{
				SnapControllerDeviceID: "6001",
				NVMeLoadDriver:         true,
				NVMeCreateVFs:          true,
			},
				runtimeConfig, pciUtils)
			go func() {
				defer GinkgoRecover()
				defer cancel()
				defer close(stop)
				_ = p.Wait(ctx)
			}()
		})
		It("Create VFs", func() {
			pciUtils.EXPECT().InsertKernelModule(ctx, "nvme").Return(nil)
			pciUtils.EXPECT().GetPFs("15b3", []string{"6001"}).Return([]pci.DeviceInfo{
				{Address: "0000:b1:00.3"}, {Address: "0000:b1:00.4"}}, nil)
			pciUtils.EXPECT().LoadDriver("0000:b1:00.3", "nvme").Return(nil)
			pciUtils.EXPECT().IsSRIOVEnabled("0000:b1:00.3").Return(true, nil)
			pciUtils.EXPECT().GetSRIOVTotalVFs("0000:b1:00.3").Return(125, nil)
			pciUtils.EXPECT().GetSRIOVNumVFs("0000:b1:00.3").Return(0, nil)
			pciUtils.EXPECT().DisableSriovVfsDriverAutoprobe("0000:b1:00.3").Return(nil)
			pciUtils.EXPECT().SetSriovNumVfs("0000:b1:00.3", 125).Return(nil)
			pciUtils.EXPECT().LoadDriver("0000:b1:00.4", "nvme").Return(nil)
			pciUtils.EXPECT().IsSRIOVEnabled("0000:b1:00.4").Return(true, nil)
			pciUtils.EXPECT().GetSRIOVTotalVFs("0000:b1:00.4").Return(125, nil)
			pciUtils.EXPECT().GetSRIOVNumVFs("0000:b1:00.4").Return(0, nil)
			pciUtils.EXPECT().DisableSriovVfsDriverAutoprobe("0000:b1:00.4").Return(nil)
			pciUtils.EXPECT().SetSriovNumVfs("0000:b1:00.4", 125).Return(nil)
			Expect(p.Run(ctx)).NotTo(HaveOccurred())
			Expect(runtimeConfig.GetMaxVolumesPerNode()).To(Equal(int64(250)))
		})
		It("Already configured", func() {
			pciUtils.EXPECT().InsertKernelModule(ctx, "nvme").Return(nil)
			pciUtils.EXPECT().GetPFs("15b3", []string{"6001"}).Return([]pci.DeviceInfo{{Address: "0000:b1:00.3"}}, nil)
			pciUtils.EXPECT().LoadDriver("0000:b1:00.3", "nvme").Return(nil)
			pciUtils.EXPECT().IsSRIOVEnabled("0000:b1:00.3").Return(true, nil)
			pciUtils.EXPECT().GetSRIOVTotalVFs("0000:b1:00.3").Return(125, nil)
			pciUtils.EXPECT().GetSRIOVNumVFs("0000:b1:00.3").Return(125, nil)
			Expect(p.Run(ctx)).NotTo(HaveOccurred())
			Expect(runtimeConfig.GetMaxVolumesPerNode()).To(Equal(int64(125)))
		})
		It("Already configured - less VFs", func() {
			pciUtils.EXPECT().InsertKernelModule(ctx, "nvme").Return(nil)
			pciUtils.EXPECT().GetPFs("15b3", []string{"6001"}).Return([]pci.DeviceInfo{{Address: "0000:b1:00.3"}}, nil)
			pciUtils.EXPECT().LoadDriver("0000:b1:00.3", "nvme").Return(nil)
			pciUtils.EXPECT().IsSRIOVEnabled("0000:b1:00.3").Return(true, nil)
			pciUtils.EXPECT().GetSRIOVTotalVFs("0000:b1:00.3").Return(125, nil)
			pciUtils.EXPECT().GetSRIOVNumVFs("0000:b1:00.3").Return(25, nil)
			Expect(p.Run(ctx)).NotTo(HaveOccurred())
			Expect(runtimeConfig.GetMaxVolumesPerNode()).To(Equal(int64(25)))
		})
		It("SRIOV disabled", func() {
			pciUtils.EXPECT().InsertKernelModule(ctx, "nvme").Return(nil)
			pciUtils.EXPECT().GetPFs("15b3", []string{"6001"}).Return([]pci.DeviceInfo{{Address: "0000:b1:00.3"}}, nil)
			pciUtils.EXPECT().IsSRIOVEnabled("0000:b1:00.3").Return(false, nil)
			Expect(p.Run(ctx)).NotTo(HaveOccurred())
			Expect(runtimeConfig.GetMaxVolumesPerNode()).To(Equal(int64(0)))
		})
		It("IsSRIOVEnabled failed", func() {
			pciUtils.EXPECT().InsertKernelModule(ctx, "nvme").Return(nil)
			pciUtils.EXPECT().GetPFs("15b3", []string{"6001"}).Return([]pci.DeviceInfo{{Address: "0000:b1:00.3"}}, nil)
			pciUtils.EXPECT().IsSRIOVEnabled("0000:b1:00.3").Return(false, errTest)
			Expect(p.Run(ctx)).To(MatchError(errTest))
		})
		It("DisableSriovVfsDriverAutoprobe is not supported", func() {
			pciUtils.EXPECT().InsertKernelModule(ctx, "nvme").Return(nil)
			pciUtils.EXPECT().GetPFs("15b3", []string{"6001"}).Return([]pci.DeviceInfo{{Address: "0000:b1:00.3"}}, nil)
			pciUtils.EXPECT().LoadDriver("0000:b1:00.3", "nvme").Return(nil)
			pciUtils.EXPECT().IsSRIOVEnabled("0000:b1:00.3").Return(true, nil)
			pciUtils.EXPECT().GetSRIOVTotalVFs("0000:b1:00.3").Return(125, nil)
			pciUtils.EXPECT().GetSRIOVNumVFs("0000:b1:00.3").Return(0, nil)
			pciUtils.EXPECT().DisableSriovVfsDriverAutoprobe("0000:b1:00.3").Return(errTest)
			pciUtils.EXPECT().SetSriovNumVfs("0000:b1:00.3", 125).Return(nil)
			Expect(p.Run(ctx)).NotTo(HaveOccurred())
		})
		It("Failed to load nvme driver", func() {
			pciUtils.EXPECT().InsertKernelModule(ctx, "nvme").Return(errTest)
			Expect(p.Run(ctx)).To(MatchError(errTest))
		})
		It("Failed to list PFs", func() {
			pciUtils.EXPECT().InsertKernelModule(ctx, "nvme").Return(nil)
			pciUtils.EXPECT().GetPFs("15b3", []string{"6001"}).Return([]pci.DeviceInfo{}, errTest)
			Expect(p.Run(ctx)).To(MatchError(errTest))
		})
		It("Failed to load driver for PF", func() {
			pciUtils.EXPECT().InsertKernelModule(ctx, "nvme").Return(nil)
			pciUtils.EXPECT().GetPFs("15b3", []string{"6001"}).Return([]pci.DeviceInfo{{Address: "0000:b1:00.3"}}, nil)
			pciUtils.EXPECT().IsSRIOVEnabled("0000:b1:00.3").Return(true, nil)
			pciUtils.EXPECT().LoadDriver("0000:b1:00.3", "nvme").Return(errTest)
			Expect(p.Run(ctx)).To(MatchError(errTest))
		})
		It("Failed to read total VFs", func() {
			pciUtils.EXPECT().InsertKernelModule(ctx, "nvme").Return(nil)
			pciUtils.EXPECT().GetPFs("15b3", []string{"6001"}).Return([]pci.DeviceInfo{{Address: "0000:b1:00.3"}}, nil)
			pciUtils.EXPECT().LoadDriver("0000:b1:00.3", "nvme").Return(nil)
			pciUtils.EXPECT().IsSRIOVEnabled("0000:b1:00.3").Return(true, nil)
			pciUtils.EXPECT().GetSRIOVTotalVFs("0000:b1:00.3").Return(0, errTest)
			Expect(p.Run(ctx)).To(MatchError(errTest))
		})
		It("Failed to read num VFs", func() {
			pciUtils.EXPECT().InsertKernelModule(ctx, "nvme").Return(nil)
			pciUtils.EXPECT().GetPFs("15b3", []string{"6001"}).Return([]pci.DeviceInfo{{Address: "0000:b1:00.3"}}, nil)
			pciUtils.EXPECT().LoadDriver("0000:b1:00.3", "nvme").Return(nil)
			pciUtils.EXPECT().IsSRIOVEnabled("0000:b1:00.3").Return(true, nil)
			pciUtils.EXPECT().GetSRIOVTotalVFs("0000:b1:00.3").Return(125, nil)
			pciUtils.EXPECT().GetSRIOVNumVFs("0000:b1:00.3").Return(0, errTest)
			Expect(p.Run(ctx)).To(MatchError(errTest))
		})
		It("Failed to set num VFs", func() {
			pciUtils.EXPECT().InsertKernelModule(ctx, "nvme").Return(nil)
			pciUtils.EXPECT().GetPFs("15b3", []string{"6001"}).Return([]pci.DeviceInfo{{Address: "0000:b1:00.3"}}, nil)
			pciUtils.EXPECT().LoadDriver("0000:b1:00.3", "nvme").Return(nil)
			pciUtils.EXPECT().IsSRIOVEnabled("0000:b1:00.3").Return(true, nil)
			pciUtils.EXPECT().GetSRIOVTotalVFs("0000:b1:00.3").Return(125, nil)
			pciUtils.EXPECT().GetSRIOVNumVFs("0000:b1:00.3").Return(0, nil)
			pciUtils.EXPECT().DisableSriovVfsDriverAutoprobe("0000:b1:00.3").Return(nil)
			pciUtils.EXPECT().SetSriovNumVfs("0000:b1:00.3", 125).Return(errTest)
			Expect(p.Run(ctx)).To(MatchError(errTest))
		})
	})

	Context("NVMe emulation mode - skip driver loading", func() {
		It("Should skip driver loading", func() {
			p = New(config.Common{
				EmulationMode: config.EmulationModeNVMe,
			}, config.Node{
				SnapControllerDeviceID: "6001",
				NVMeLoadDriver:         false,
				NVMeCreateVFs:          true,
			}, runtimeConfig, pciUtils)
			go func() {
				defer GinkgoRecover()
				defer cancel()
				defer close(stop)
				_ = p.Wait(ctx)
			}()
			pciUtils.EXPECT().GetPFs("15b3", []string{"6001"}).Return([]pci.DeviceInfo{
				{Address: "0000:b1:00.3"}, {Address: "0000:b1:00.4"}}, nil)
			pciUtils.EXPECT().LoadDriver("0000:b1:00.3", "nvme").Return(nil)
			pciUtils.EXPECT().IsSRIOVEnabled("0000:b1:00.3").Return(true, nil)
			pciUtils.EXPECT().GetSRIOVTotalVFs("0000:b1:00.3").Return(125, nil)
			pciUtils.EXPECT().GetSRIOVNumVFs("0000:b1:00.3").Return(0, nil)
			pciUtils.EXPECT().DisableSriovVfsDriverAutoprobe("0000:b1:00.3").Return(nil)
			pciUtils.EXPECT().SetSriovNumVfs("0000:b1:00.3", 125).Return(nil)
			pciUtils.EXPECT().LoadDriver("0000:b1:00.4", "nvme").Return(nil)
			pciUtils.EXPECT().IsSRIOVEnabled("0000:b1:00.4").Return(true, nil)
			pciUtils.EXPECT().GetSRIOVTotalVFs("0000:b1:00.4").Return(125, nil)
			pciUtils.EXPECT().GetSRIOVNumVFs("0000:b1:00.4").Return(0, nil)
			pciUtils.EXPECT().DisableSriovVfsDriverAutoprobe("0000:b1:00.4").Return(nil)
			pciUtils.EXPECT().SetSriovNumVfs("0000:b1:00.4", 125).Return(nil)
			Expect(p.Run(ctx)).NotTo(HaveOccurred())
			Expect(runtimeConfig.GetMaxVolumesPerNode()).To(Equal(int64(250)))
		})
	})

	Context("NVMe emulation mode - skip VFs creation", func() {
		It("Should skip VFs creation", func() {
			p = New(config.Common{
				EmulationMode: config.EmulationModeNVMe,
			}, config.Node{
				SnapControllerDeviceID: "6001",
				NVMeLoadDriver:         true,
				NVMeCreateVFs:          false,
			}, runtimeConfig, pciUtils)
			go func() {
				defer GinkgoRecover()
				defer cancel()
				defer close(stop)
				_ = p.Wait(ctx)
			}()
			pciUtils.EXPECT().InsertKernelModule(ctx, "nvme").Return(nil)
			Expect(p.Run(ctx)).NotTo(HaveOccurred())
			Expect(runtimeConfig.GetMaxVolumesPerNode()).To(Equal(int64(0)))
		})
	})
	Context("Virtiofs emulation mode - default configuration", func() {
		BeforeEach(func() {
			p = New(config.Common{
				EmulationMode: config.EmulationModeVirtiofs,
			}, config.Node{
				VirtiofsLoadDriver: true,
			}, runtimeConfig, pciUtils)
			go func() {
				defer GinkgoRecover()
				defer cancel()
				defer close(stop)
				_ = p.Wait(ctx)
			}()
		})

		It("Load virtio-pci driver successfully", func() {
			pciUtils.EXPECT().InsertKernelModule(ctx, "virtio-pci").Return(nil)
			Expect(p.Run(ctx)).NotTo(HaveOccurred())
		})

		It("Failed to load virtio-pci driver", func() {
			pciUtils.EXPECT().InsertKernelModule(ctx, "virtio-pci").Return(errTest)
			Expect(p.Run(ctx)).To(MatchError(errTest))
		})
	})
	Context("Virtiofs emulation mode - skip driver loading", func() {
		It("Should skip driver loading", func() {
			p = New(config.Common{
				EmulationMode: config.EmulationModeVirtiofs,
			}, config.Node{
				VirtiofsLoadDriver: false,
			}, runtimeConfig, pciUtils)
			go func() {
				defer GinkgoRecover()
				defer cancel()
				defer close(stop)
				_ = p.Wait(ctx)
			}()
			Expect(p.Run(ctx)).NotTo(HaveOccurred())
		})
	})
})
