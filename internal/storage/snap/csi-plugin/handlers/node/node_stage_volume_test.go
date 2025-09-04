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
	"os"

	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/config"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/handlers/common"
	mountUtilsMockPkg "github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/mount/mock"
	utilsNvme "github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/nvme"
	nvmeUtilsMockPkg "github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/nvme/mock"
	mountPciMockPkg "github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/pci/mock"

	"github.com/container-storage-interface/spec/lib/go/csi"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	kmount "k8s.io/mount-utils"
)

var _ = Describe("NodeStageVolume", func() {
	var (
		ctrl        *gomock.Controller
		pciUtils    *mountPciMockPkg.MockUtils
		mountUtils  *mountUtilsMockPkg.MockUtils
		nvmeUtils   *nvmeUtilsMockPkg.MockUtils
		nodeHandler *node
		ctx         context.Context
		cancel      context.CancelFunc
		req         *csi.NodeStageVolumeRequest
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		pciUtils = mountPciMockPkg.NewMockUtils(ctrl)
		mountUtils = mountUtilsMockPkg.NewMockUtils(ctrl)
		nvmeUtils = nvmeUtilsMockPkg.NewMockUtils(ctrl)
		nodeHandler = &node{
			commonConfig: config.Common{EmulationMode: config.EmulationModeNVMe},
			pci:          pciUtils,
			mount:        mountUtils,
			nvme:         nvmeUtils,
		}
		ctx, cancel = context.WithCancel(context.Background())
		req = &csi.NodeStageVolumeRequest{
			VolumeId:          "volume-id",
			StagingTargetPath: "/staging/path",
			PublishContext: map[string]string{
				common.PublishCtxDevicePciAddress: "0000:00:1f.2",
				common.PublishCtxNvmeNsID:         "1",
			},
			VolumeCapability: &csi.VolumeCapability{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
				AccessType: &csi.VolumeCapability_Block{
					Block: &csi.VolumeCapability_BlockVolume{},
				},
			},
		}
	})

	AfterEach(func() {
		cancel()
		ctrl.Finish()
	})

	Context("Validation", func() {
		It("should return error if VolumeId is empty", func() {
			req.VolumeId = ""
			resp, err := nodeHandler.NodeStageVolume(ctx, req)
			Expect(resp).To(BeNil())
			common.CheckGRPCErr(err, codes.InvalidArgument, "VolumeID: field is required")
		})
		It("should return error if StagingTargetPath is empty", func() {
			req.StagingTargetPath = ""
			resp, err := nodeHandler.NodeStageVolume(ctx, req)
			Expect(resp).To(BeNil())
			common.CheckGRPCErr(err, codes.InvalidArgument, "StagingTargetPath: field is required")
		})

		It("should return error if VolumeCapability is invalid", func() {
			req.VolumeCapability = nil
			resp, err := nodeHandler.NodeStageVolume(ctx, req)
			Expect(resp).To(BeNil())
			Expect(err).To(HaveOccurred())
		})
		It("should return error if PublishContext.pciDeviceAddress is not set", func() {
			req.PublishContext[common.PublishCtxDevicePciAddress] = ""
			resp, err := nodeHandler.NodeStageVolume(ctx, req)
			Expect(resp).To(BeNil())
			common.CheckGRPCErr(err, codes.InvalidArgument, "PublishContext.pciDeviceAddress: field is required")
		})

		It("should return error if PublishContext.pciDeviceAddress is invalid", func() {
			req.PublishContext[common.PublishCtxDevicePciAddress] = "invalid-address"
			resp, err := nodeHandler.NodeStageVolume(ctx, req)
			Expect(resp).To(BeNil())
			common.CheckGRPCErr(err, codes.InvalidArgument, "PublishContext.pciDeviceAddress contains invalid PCI address")
		})

		It("should return error if PublishContext.nvmeNsID is not set", func() {
			req.PublishContext[common.PublishCtxNvmeNsID] = ""
			resp, err := nodeHandler.NodeStageVolume(ctx, req)
			Expect(resp).To(BeNil())
			common.CheckGRPCErr(err, codes.InvalidArgument, "PublishContext.nvmeNsID: field is required")
		})

		It("should return error if PublishContext.nvmeNsID is invalid", func() {
			req.PublishContext[common.PublishCtxNvmeNsID] = "invalid-nsid"
			resp, err := nodeHandler.NodeStageVolume(ctx, req)
			Expect(resp).To(BeNil())
			common.CheckGRPCErr(err, codes.InvalidArgument, "PublishContext.nvmeNsID contains invalid NVME NS ID")
		})
	})

	Context("Stage", func() {
		It("should stage the volume successfully", func() {
			nvmeUtils.EXPECT().GetBlockDeviceNameForNS("0000:00:1f.2", int32(1)).Return("", utilsNvme.ErrBlockDeviceNotFound).Times(1)
			nvmeUtils.EXPECT().GetBlockDeviceNameForNS("0000:00:1f.2", int32(1)).Return("", utilsNvme.ErrBlockDeviceIsInvalid).Times(1)
			pciUtils.EXPECT().UnloadDriver("0000:00:1f.2").Return(nil)
			pciUtils.EXPECT().LoadDriver("0000:00:1f.2", "nvme").Return(nil)
			nvmeUtils.EXPECT().GetBlockDeviceNameForNS("0000:00:1f.2", int32(1)).Return("nvme0n1", nil).Times(1)
			mountUtils.EXPECT().EnsureFileExist("/staging/path/volume-id", os.FileMode(0644)).Return(nil)
			mountUtils.EXPECT().CheckMountExists("/dev/nvme0n1", "/staging/path/volume-id").Return(false, kmount.MountInfo{}, nil)
			mountUtils.EXPECT().Mount("/dev/nvme0n1", "/staging/path/volume-id", "", []string{"bind"}).Return(nil)

			resp, err := nodeHandler.NodeStageVolume(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).ToNot(BeNil())
		})
		It("already staged", func() {
			nvmeUtils.EXPECT().GetBlockDeviceNameForNS("0000:00:1f.2", int32(1)).Return("nvme0n1", nil)
			mountUtils.EXPECT().EnsureFileExist("/staging/path/volume-id", os.FileMode(0644)).Return(nil)
			mountUtils.EXPECT().CheckMountExists("/dev/nvme0n1", "/staging/path/volume-id").Return(true, kmount.MountInfo{}, nil)

			resp, err := nodeHandler.NodeStageVolume(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).ToNot(BeNil())
		})
		It("should return error if GetBlockDeviceNameForNS fails", func() {
			nvmeUtils.EXPECT().GetBlockDeviceNameForNS("0000:00:1f.2", int32(1)).Return("", errTest)

			resp, err := nodeHandler.NodeStageVolume(ctx, req)
			common.CheckGRPCErr(err, codes.Internal, "error occurred while trying to find block device for the volume")
			Expect(resp).To(BeNil())
		})
		It("should return error if UnloadDriver fails", func() {
			nvmeUtils.EXPECT().GetBlockDeviceNameForNS("0000:00:1f.2", int32(1)).Return("", utilsNvme.ErrBlockDeviceNotFound).Times(1)
			pciUtils.EXPECT().UnloadDriver("0000:00:1f.2").Return(errTest)

			resp, err := nodeHandler.NodeStageVolume(ctx, req)
			common.CheckGRPCErr(err, codes.Internal, "error occurred while trying to unload NVME driver for the volume device")
			Expect(resp).To(BeNil())
		})
		It("should return error if LoadDriver fails", func() {
			nvmeUtils.EXPECT().GetBlockDeviceNameForNS("0000:00:1f.2", int32(1)).Return("", utilsNvme.ErrBlockDeviceNotFound).Times(1)
			pciUtils.EXPECT().UnloadDriver("0000:00:1f.2").Return(nil)
			pciUtils.EXPECT().LoadDriver("0000:00:1f.2", "nvme").Return(errTest)

			resp, err := nodeHandler.NodeStageVolume(ctx, req)
			common.CheckGRPCErr(err, codes.Internal, "error occurred while trying to load NVME driver for the volume device")
			Expect(resp).To(BeNil())
		})
		It("should return error if GetBlockDeviceNameForNS fails in polling loop", func() {
			nvmeUtils.EXPECT().GetBlockDeviceNameForNS("0000:00:1f.2", int32(1)).Return("", utilsNvme.ErrBlockDeviceNotFound).Times(1)
			pciUtils.EXPECT().UnloadDriver("0000:00:1f.2").Return(nil)
			pciUtils.EXPECT().LoadDriver("0000:00:1f.2", "nvme").Return(nil)
			nvmeUtils.EXPECT().GetBlockDeviceNameForNS("0000:00:1f.2", int32(1)).Return("", errTest).Times(1)

			resp, err := nodeHandler.NodeStageVolume(ctx, req)
			common.CheckGRPCErr(err, codes.Internal, "failed to discover block device for the volume")
			Expect(resp).To(BeNil())
		})
		It("should return error if GetBlockDeviceNameForNS fails by timeout", func() {
			nvmeUtils.EXPECT().GetBlockDeviceNameForNS("0000:00:1f.2", int32(1)).Return("", utilsNvme.ErrBlockDeviceNotFound).Times(1)
			pciUtils.EXPECT().UnloadDriver("0000:00:1f.2").Return(nil)
			pciUtils.EXPECT().LoadDriver("0000:00:1f.2", "nvme").Return(nil)
			nvmeUtils.EXPECT().GetBlockDeviceNameForNS("0000:00:1f.2", int32(1)).Do(func(_ string, _ int32) {
				cancel()
			}).Return("", utilsNvme.ErrBlockDeviceNotFound).Times(1)

			resp, err := nodeHandler.NodeStageVolume(ctx, req)
			common.CheckGRPCErr(err, codes.DeadlineExceeded, "timeout occurred while waiting for block device for the volume")
			Expect(resp).To(BeNil())
		})
		It("should return error if EnsureFileExist fails", func() {
			nvmeUtils.EXPECT().GetBlockDeviceNameForNS("0000:00:1f.2", int32(1)).Return("", utilsNvme.ErrBlockDeviceNotFound).Times(1)
			pciUtils.EXPECT().UnloadDriver("0000:00:1f.2").Return(nil)
			pciUtils.EXPECT().LoadDriver("0000:00:1f.2", "nvme").Return(nil)
			nvmeUtils.EXPECT().GetBlockDeviceNameForNS("0000:00:1f.2", int32(1)).Return("nvme0n1", nil).Times(1)
			mountUtils.EXPECT().EnsureFileExist("/staging/path/volume-id", os.FileMode(0644)).Return(errTest)

			resp, err := nodeHandler.NodeStageVolume(ctx, req)
			common.CheckGRPCErr(err, codes.Internal, "can't create staging path for the volume")
			Expect(resp).To(BeNil())
		})
		It("should return error if CheckMountExists fails", func() {
			nvmeUtils.EXPECT().GetBlockDeviceNameForNS("0000:00:1f.2", int32(1)).Return("", utilsNvme.ErrBlockDeviceNotFound).Times(2)
			pciUtils.EXPECT().UnloadDriver("0000:00:1f.2").Return(nil)
			pciUtils.EXPECT().LoadDriver("0000:00:1f.2", "nvme").Return(nil)
			nvmeUtils.EXPECT().GetBlockDeviceNameForNS("0000:00:1f.2", int32(1)).Return("nvme0n1", nil).Times(1)
			mountUtils.EXPECT().EnsureFileExist("/staging/path/volume-id", os.FileMode(0644)).Return(nil)
			mountUtils.EXPECT().CheckMountExists("/dev/nvme0n1", "/staging/path/volume-id").Return(false, kmount.MountInfo{}, errTest)

			resp, err := nodeHandler.NodeStageVolume(ctx, req)
			common.CheckGRPCErr(err, codes.Internal, "error occurred while checking if the volume is staged")
			Expect(resp).To(BeNil())
		})
		It("should return error if Mount fails", func() {
			nvmeUtils.EXPECT().GetBlockDeviceNameForNS("0000:00:1f.2", int32(1)).Return("", utilsNvme.ErrBlockDeviceNotFound).Times(2)
			pciUtils.EXPECT().UnloadDriver("0000:00:1f.2").Return(nil)
			pciUtils.EXPECT().LoadDriver("0000:00:1f.2", "nvme").Return(nil)
			nvmeUtils.EXPECT().GetBlockDeviceNameForNS("0000:00:1f.2", int32(1)).Return("nvme0n1", nil).Times(1)
			mountUtils.EXPECT().EnsureFileExist("/staging/path/volume-id", os.FileMode(0644)).Return(nil)
			mountUtils.EXPECT().CheckMountExists("/dev/nvme0n1", "/staging/path/volume-id").Return(false, kmount.MountInfo{}, nil)
			mountUtils.EXPECT().Mount("/dev/nvme0n1", "/staging/path/volume-id", "", []string{"bind"}).Return(errTest)

			resp, err := nodeHandler.NodeStageVolume(ctx, req)
			common.CheckGRPCErr(err, codes.Internal, "failed to stage volume, bind mount failed")
			Expect(resp).To(BeNil())
		})
	})
})
