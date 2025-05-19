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
		p = New(config.Node{SnapControllerDeviceID: "6001"}, runtimeConfig, pciUtils)
		go func() {
			defer GinkgoRecover()
			defer cancel()
			defer close(stop)
			_ = p.Wait(ctx)
		}()
	})
	AfterEach(func() {
		cancel()
		testCtrl.Finish()
		Eventually(stop).WithTimeout(time.Second * 5).Should(BeClosed())
	})
	It("Create VFs", func() {
		pciUtils.EXPECT().InsertKernelModule(ctx, "nvme").Return(nil)
		pciUtils.EXPECT().GetPFs("15b3", []string{"6001"}).Return([]pci.DeviceInfo{
			{Address: "0000:b1:00.3"}, {Address: "0000:b1:00.4"}}, nil)
		pciUtils.EXPECT().LoadDriver("0000:b1:00.3", "nvme").Return(nil)
		pciUtils.EXPECT().GetSRIOVTotalVFs("0000:b1:00.3").Return(125, nil)
		pciUtils.EXPECT().GetSRIOVNumVFs("0000:b1:00.3").Return(0, nil)
		pciUtils.EXPECT().DisableSriovVfsDriverAutoprobe("0000:b1:00.3").Return(nil)
		pciUtils.EXPECT().SetSriovNumVfs("0000:b1:00.3", 125).Return(nil)
		Expect(p.Run(ctx)).NotTo(HaveOccurred())
		Expect(runtimeConfig.GetMaxVolumesPerNode()).To(Equal(int64(125)))
	})
	It("Already configured", func() {
		pciUtils.EXPECT().InsertKernelModule(ctx, "nvme").Return(nil)
		pciUtils.EXPECT().GetPFs("15b3", []string{"6001"}).Return([]pci.DeviceInfo{{Address: "0000:b1:00.3"}}, nil)
		pciUtils.EXPECT().LoadDriver("0000:b1:00.3", "nvme").Return(nil)
		pciUtils.EXPECT().GetSRIOVTotalVFs("0000:b1:00.3").Return(125, nil)
		pciUtils.EXPECT().GetSRIOVNumVFs("0000:b1:00.3").Return(125, nil)
		Expect(p.Run(ctx)).NotTo(HaveOccurred())
		Expect(runtimeConfig.GetMaxVolumesPerNode()).To(Equal(int64(125)))
	})
	It("Already configured - less VFs", func() {
		pciUtils.EXPECT().InsertKernelModule(ctx, "nvme").Return(nil)
		pciUtils.EXPECT().GetPFs("15b3", []string{"6001"}).Return([]pci.DeviceInfo{{Address: "0000:b1:00.3"}}, nil)
		pciUtils.EXPECT().LoadDriver("0000:b1:00.3", "nvme").Return(nil)
		pciUtils.EXPECT().GetSRIOVTotalVFs("0000:b1:00.3").Return(125, nil)
		pciUtils.EXPECT().GetSRIOVNumVFs("0000:b1:00.3").Return(25, nil)
		Expect(p.Run(ctx)).NotTo(HaveOccurred())
		Expect(runtimeConfig.GetMaxVolumesPerNode()).To(Equal(int64(25)))
	})
	It("SRIOV disabled", func() {
		pciUtils.EXPECT().InsertKernelModule(ctx, "nvme").Return(nil)
		pciUtils.EXPECT().GetPFs("15b3", []string{"6001"}).Return([]pci.DeviceInfo{{Address: "0000:b1:00.3"}}, nil)
		pciUtils.EXPECT().LoadDriver("0000:b1:00.3", "nvme").Return(nil)
		pciUtils.EXPECT().GetSRIOVTotalVFs("0000:b1:00.3").Return(0, nil)
		Expect(p.Run(ctx)).To(MatchError(ContainSubstring("sriov_totalvfs is 0 for the device")))
	})
	It("DisableSriovVfsDriverAutoprobe is not supported", func() {
		pciUtils.EXPECT().InsertKernelModule(ctx, "nvme").Return(nil)
		pciUtils.EXPECT().GetPFs("15b3", []string{"6001"}).Return([]pci.DeviceInfo{{Address: "0000:b1:00.3"}}, nil)
		pciUtils.EXPECT().LoadDriver("0000:b1:00.3", "nvme").Return(nil)
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
	It("Storage PF not found", func() {
		pciUtils.EXPECT().InsertKernelModule(ctx, "nvme").Return(nil)
		pciUtils.EXPECT().GetPFs("15b3", []string{"6001"}).Return([]pci.DeviceInfo{}, nil)
		Expect(p.Run(ctx)).To(MatchError(ContainSubstring("storage PF not found")))
	})
	It("Failed to load driver for PF", func() {
		pciUtils.EXPECT().InsertKernelModule(ctx, "nvme").Return(nil)
		pciUtils.EXPECT().GetPFs("15b3", []string{"6001"}).Return([]pci.DeviceInfo{{Address: "0000:b1:00.3"}}, nil)
		pciUtils.EXPECT().LoadDriver("0000:b1:00.3", "nvme").Return(errTest)
		Expect(p.Run(ctx)).To(MatchError(errTest))
	})
	It("Failed to read total VFs", func() {
		pciUtils.EXPECT().InsertKernelModule(ctx, "nvme").Return(nil)
		pciUtils.EXPECT().GetPFs("15b3", []string{"6001"}).Return([]pci.DeviceInfo{{Address: "0000:b1:00.3"}}, nil)
		pciUtils.EXPECT().LoadDriver("0000:b1:00.3", "nvme").Return(nil)
		pciUtils.EXPECT().GetSRIOVTotalVFs("0000:b1:00.3").Return(0, errTest)
		Expect(p.Run(ctx)).To(MatchError(errTest))
	})
	It("Failed to read num VFs", func() {
		pciUtils.EXPECT().InsertKernelModule(ctx, "nvme").Return(nil)
		pciUtils.EXPECT().GetPFs("15b3", []string{"6001"}).Return([]pci.DeviceInfo{{Address: "0000:b1:00.3"}}, nil)
		pciUtils.EXPECT().LoadDriver("0000:b1:00.3", "nvme").Return(nil)
		pciUtils.EXPECT().GetSRIOVTotalVFs("0000:b1:00.3").Return(125, nil)
		pciUtils.EXPECT().GetSRIOVNumVFs("0000:b1:00.3").Return(0, errTest)
		Expect(p.Run(ctx)).To(MatchError(errTest))
	})
	It("Failed to set num VFs", func() {
		pciUtils.EXPECT().InsertKernelModule(ctx, "nvme").Return(nil)
		pciUtils.EXPECT().GetPFs("15b3", []string{"6001"}).Return([]pci.DeviceInfo{{Address: "0000:b1:00.3"}}, nil)
		pciUtils.EXPECT().LoadDriver("0000:b1:00.3", "nvme").Return(nil)
		pciUtils.EXPECT().GetSRIOVTotalVFs("0000:b1:00.3").Return(125, nil)
		pciUtils.EXPECT().GetSRIOVNumVFs("0000:b1:00.3").Return(0, nil)
		pciUtils.EXPECT().DisableSriovVfsDriverAutoprobe("0000:b1:00.3").Return(nil)
		pciUtils.EXPECT().SetSriovNumVfs("0000:b1:00.3", 125).Return(errTest)
		Expect(p.Run(ctx)).To(MatchError(errTest))
	})
})
