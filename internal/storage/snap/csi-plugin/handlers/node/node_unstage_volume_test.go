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

package node

import (
	"context"

	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/config"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/handlers/common"
	mountUtilsMockPkg "github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/mount/mock"
	utilsNvme "github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/nvme"
	nvmeUtilsMockPkg "github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/nvme/mock"
	pciUtilsMockPkg "github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/pci/mock"

	"github.com/container-storage-interface/spec/lib/go/csi"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
)

var _ = Describe("NodeUnstageVolume", func() {
	var (
		nodeHandler *node
		req         *csi.NodeUnstageVolumeRequest
		ctx         context.Context
	)

	Context("NVMe emulation mode", func() {
		BeforeEach(func() {
			nodeHandler = &node{commonConfig: config.Common{EmulationMode: config.EmulationModeNVMe}}
			ctx = context.Background()
			req = &csi.NodeUnstageVolumeRequest{
				VolumeId:          "test-volume-id",
				StagingTargetPath: "/target/path",
			}
		})

		Context("Validation", func() {
			It("should return error if VolumeId is empty", func() {
				req.VolumeId = ""
				resp, err := nodeHandler.NodeUnstageVolume(ctx, req)
				Expect(err).To(HaveOccurred())
				common.CheckGRPCErr(err, codes.InvalidArgument, "VolumeID: field is required")
				Expect(resp).To(BeNil())
			})

			It("should return error if StagingTargetPath is empty", func() {
				req.StagingTargetPath = ""
				resp, err := nodeHandler.NodeUnstageVolume(ctx, req)
				Expect(err).To(HaveOccurred())
				common.CheckGRPCErr(err, codes.InvalidArgument, "StagingTargetPath: field is required")
				Expect(resp).To(BeNil())
			})
		})

		Context("Unstage", func() {
			var (
				mountUtils *mountUtilsMockPkg.MockUtils
				nvmeUtils  *nvmeUtilsMockPkg.MockUtils
				pciUtils   *pciUtilsMockPkg.MockUtils
				testCtrl   *gomock.Controller
			)
			BeforeEach(func() {
				testCtrl = gomock.NewController(GinkgoT())
				mountUtils = mountUtilsMockPkg.NewMockUtils(testCtrl)
				nvmeUtils = nvmeUtilsMockPkg.NewMockUtils(testCtrl)
				pciUtils = pciUtilsMockPkg.NewMockUtils(testCtrl)
				nodeHandler.mount = mountUtils
				nodeHandler.nvme = nvmeUtils
				nodeHandler.pci = pciUtils
			})
			AfterEach(func() {
				testCtrl.Finish()
			})

			It("should unmount the volume and unbind the NVMe driver", func() {
				gomock.InOrder(
					nvmeUtils.EXPECT().GetPCIAddressForBlockDevicePath("/target/path/test-volume-id").Return("0000:b1:0c.2", nil),
					mountUtils.EXPECT().UnmountAndRemove("/target/path/test-volume-id").Return(nil),
					pciUtils.EXPECT().UnloadDriver("0000:b1:0c.2").Return(nil),
				)
				resp, err := nodeHandler.NodeUnstageVolume(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).NotTo(BeNil())
			})

			It("should unmount the volume without unbinding the NVMe driver if the function lookup fails", func() {
				gomock.InOrder(
					nvmeUtils.EXPECT().GetPCIAddressForBlockDevicePath("/target/path/test-volume-id").Return("", errTest),
					mountUtils.EXPECT().UnmountAndRemove("/target/path/test-volume-id").Return(nil),
				)
				resp, err := nodeHandler.NodeUnstageVolume(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).NotTo(BeNil())
			})

			It("should unstage the volume successfully if unbinding the NVMe driver fails", func() {
				gomock.InOrder(
					nvmeUtils.EXPECT().GetPCIAddressForBlockDevicePath("/target/path/test-volume-id").Return("0000:b1:0c.2", nil),
					mountUtils.EXPECT().UnmountAndRemove("/target/path/test-volume-id").Return(nil),
					pciUtils.EXPECT().UnloadDriver("0000:b1:0c.2").Return(errTest),
				)
				resp, err := nodeHandler.NodeUnstageVolume(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).NotTo(BeNil())
			})

			It("should unstage the volume successfully", func() {
				nvmeUtils.EXPECT().GetPCIAddressForBlockDevicePath("/target/path/test-volume-id").Return("", utilsNvme.ErrBlockDeviceNotFound)
				mountUtils.EXPECT().UnmountAndRemove("/target/path/test-volume-id").Return(nil)
				resp, err := nodeHandler.NodeUnstageVolume(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).NotTo(BeNil())
			})

			It("should return error and not unbind the NVMe driver if unmount fails", func() {
				gomock.InOrder(
					nvmeUtils.EXPECT().GetPCIAddressForBlockDevicePath("/target/path/test-volume-id").Return("0000:b1:0c.2", nil),
					mountUtils.EXPECT().UnmountAndRemove("/target/path/test-volume-id").Return(errTest),
				)
				resp, err := nodeHandler.NodeUnstageVolume(ctx, req)
				common.CheckGRPCErr(err, codes.Internal, "failed to unmount staging path")
				Expect(resp).To(BeNil())
			})
		})
	})

	Context("VirtioFS emulation mode", func() {
		BeforeEach(func() {
			nodeHandler = &node{commonConfig: config.Common{EmulationMode: config.EmulationModeVirtiofs}}
			ctx = context.Background()
			req = &csi.NodeUnstageVolumeRequest{
				VolumeId:          "test-volume-id",
				StagingTargetPath: "/staging/path",
			}
		})

		Context("Validation", func() {
			It("should return error if VolumeId is empty", func() {
				req.VolumeId = ""
				resp, err := nodeHandler.NodeUnstageVolume(ctx, req)
				Expect(err).To(HaveOccurred())
				common.CheckGRPCErr(err, codes.InvalidArgument, "VolumeID: field is required")
				Expect(resp).To(BeNil())
			})

			It("should return error if StagingTargetPath is empty", func() {
				req.StagingTargetPath = ""
				resp, err := nodeHandler.NodeUnstageVolume(ctx, req)
				Expect(err).To(HaveOccurred())
				common.CheckGRPCErr(err, codes.InvalidArgument, "StagingTargetPath: field is required")
				Expect(resp).To(BeNil())
			})
		})

		Context("Unstage", func() {
			It("should unstage the volume successfully", func() {
				// No mount operations expected for VirtioFS mode
				resp, err := nodeHandler.NodeUnstageVolume(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).NotTo(BeNil())
			})
		})
	})
})
