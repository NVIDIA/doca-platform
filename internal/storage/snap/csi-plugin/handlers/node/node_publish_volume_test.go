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

	"github.com/container-storage-interface/spec/lib/go/csi"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	kmount "k8s.io/mount-utils"
)

var _ = Describe("NodePublishVolume", func() {
	var (
		nodeHandler *node
		req         *csi.NodePublishVolumeRequest
		ctx         context.Context
	)

	Context("NVMe emulation mode", func() {
		BeforeEach(func() {
			nodeHandler = &node{commonConfig: config.Common{EmulationMode: config.EmulationModeNVMe}}
			ctx = context.Background()
			req = &csi.NodePublishVolumeRequest{
				VolumeId:          "test-volume-id",
				StagingTargetPath: "/staging/path",
				TargetPath:        "/target/path",
				Readonly:          false,
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

		Context("Validation", func() {
			It("should return error if VolumeId is empty", func() {
				req.VolumeId = ""
				resp, err := nodeHandler.NodePublishVolume(ctx, req)
				Expect(err).To(HaveOccurred())
				common.CheckGRPCErr(err, codes.InvalidArgument, "VolumeID: field is required")
				Expect(resp).To(BeNil())
			})

			It("should return error if StagingTargetPath is empty", func() {
				req.StagingTargetPath = ""
				resp, err := nodeHandler.NodePublishVolume(ctx, req)
				Expect(err).To(HaveOccurred())
				common.CheckGRPCErr(err, codes.InvalidArgument, "StagingTargetPath: field is required")
				Expect(resp).To(BeNil())
			})

			It("should return error if TargetPath is empty", func() {
				req.TargetPath = ""
				resp, err := nodeHandler.NodePublishVolume(ctx, req)
				Expect(err).To(HaveOccurred())
				common.CheckGRPCErr(err, codes.InvalidArgument, "TargetPath: field is required")
				Expect(resp).To(BeNil())
			})

			It("should return error if VolumeCapability is empty", func() {
				req.VolumeCapability = nil
				resp, err := nodeHandler.NodePublishVolume(ctx, req)
				Expect(err).To(HaveOccurred())
				common.CheckGRPCErr(err, codes.InvalidArgument, "VolumeCapability: field is required")
				Expect(resp).To(BeNil())
			})
			It("should return Unimplemented error when readonly", func() {
				req.Readonly = true
				resp, err := nodeHandler.NodePublishVolume(ctx, req)
				common.CheckGRPCErr(err, codes.Unimplemented, "readOnly volumes are not supported")
				Expect(resp).To(BeNil())
			})
		})
		Context("Publish", func() {
			var (
				mountUtils *mountUtilsMockPkg.MockUtils
				testCtrl   *gomock.Controller
			)
			BeforeEach(func() {
				testCtrl = gomock.NewController(GinkgoT())
				mountUtils = mountUtilsMockPkg.NewMockUtils(testCtrl)
				nodeHandler.mount = mountUtils
			})
			AfterEach(func() {
				testCtrl.Finish()
			})

			It("should publish the volume successfully", func() {
				mountUtils.EXPECT().EnsureFileExist("/target/path", os.FileMode(0644)).Return(nil)
				mountUtils.EXPECT().CheckMountExists("/staging/path/test-volume-id", "/target/path").Return(false, kmount.MountInfo{}, nil)
				mountUtils.EXPECT().Mount("/staging/path/test-volume-id", "/target/path", "", []string{"bind"}).Return(nil)
				resp, err := nodeHandler.NodePublishVolume(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).NotTo(BeNil())
			})

			It("already published", func() {
				mountUtils.EXPECT().EnsureFileExist("/target/path", os.FileMode(0644)).Return(nil)
				mountUtils.EXPECT().CheckMountExists("/staging/path/test-volume-id", "/target/path").Return(true, kmount.MountInfo{}, nil)
				resp, err := nodeHandler.NodePublishVolume(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).NotTo(BeNil())
			})

			It("should return error if EnsureFileExist fails", func() {
				mountUtils.EXPECT().EnsureFileExist("/target/path", os.FileMode(0644)).Return(errTest)
				resp, err := nodeHandler.NodePublishVolume(ctx, req)
				common.CheckGRPCErr(err, codes.Internal, "can't create publish path for the volume")
				Expect(resp).To(BeNil())
			})

			It("should return error if CheckMountExists fails", func() {
				mountUtils.EXPECT().EnsureFileExist("/target/path", os.FileMode(0644)).Return(nil)
				mountUtils.EXPECT().CheckMountExists("/staging/path/test-volume-id", "/target/path").Return(false, kmount.MountInfo{}, errTest)
				resp, err := nodeHandler.NodePublishVolume(ctx, req)
				common.CheckGRPCErr(err, codes.Internal, "error occurred while checking if the volume is published")
				Expect(resp).To(BeNil())
			})

			It("should return error if Mount fails", func() {
				mountUtils.EXPECT().EnsureFileExist("/target/path", os.FileMode(0644)).Return(nil)
				mountUtils.EXPECT().CheckMountExists("/staging/path/test-volume-id", "/target/path").Return(false, kmount.MountInfo{}, nil)
				mountUtils.EXPECT().Mount("/staging/path/test-volume-id", "/target/path", "", []string{"bind"}).Return(errTest)
				resp, err := nodeHandler.NodePublishVolume(ctx, req)
				common.CheckGRPCErr(err, codes.Internal, "failed to publish volume, bind mount failed")
				Expect(resp).To(BeNil())
			})
		})
	})
	Context("VirtioFS emulation mode", func() {
		BeforeEach(func() {
			nodeHandler = &node{
				commonConfig: config.Common{EmulationMode: config.EmulationModeVirtiofs},
				nodeConfig:   config.Node{VirtiofsFSTypeName: "virtiofs"},
			}
			ctx = context.Background()
			req = &csi.NodePublishVolumeRequest{
				VolumeId:          "test-volume-id",
				StagingTargetPath: "/staging/path",
				TargetPath:        "/target/path",
				Readonly:          false,
				PublishContext: map[string]string{
					common.PublishCtxVirtioFsTag: "test-tag",
				},
				VolumeCapability: &csi.VolumeCapability{
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
					},
					AccessType: &csi.VolumeCapability_Mount{
						Mount: &csi.VolumeCapability_MountVolume{
							MountFlags: []string{"testoption"},
						},
					},
				},
			}
		})

		Context("Validation", func() {
			It("should return error if VolumeId is empty", func() {
				req.VolumeId = ""
				resp, err := nodeHandler.NodePublishVolume(ctx, req)
				Expect(err).To(HaveOccurred())
				common.CheckGRPCErr(err, codes.InvalidArgument, "VolumeID: field is required")
				Expect(resp).To(BeNil())
			})

			It("should return error if StagingTargetPath is empty", func() {
				req.StagingTargetPath = ""
				resp, err := nodeHandler.NodePublishVolume(ctx, req)
				Expect(err).To(HaveOccurred())
				common.CheckGRPCErr(err, codes.InvalidArgument, "StagingTargetPath: field is required")
				Expect(resp).To(BeNil())
			})

			It("should return error if TargetPath is empty", func() {
				req.TargetPath = ""
				resp, err := nodeHandler.NodePublishVolume(ctx, req)
				Expect(err).To(HaveOccurred())
				common.CheckGRPCErr(err, codes.InvalidArgument, "TargetPath: field is required")
				Expect(resp).To(BeNil())
			})

			It("should return error if VolumeCapability is empty", func() {
				req.VolumeCapability = nil
				resp, err := nodeHandler.NodePublishVolume(ctx, req)
				Expect(err).To(HaveOccurred())
				common.CheckGRPCErr(err, codes.InvalidArgument, "VolumeCapability: field is required")
				Expect(resp).To(BeNil())
			})

			It("should return error if PublishContext.virtioFSTag is not set", func() {
				req.PublishContext[common.PublishCtxVirtioFsTag] = ""
				resp, err := nodeHandler.NodePublishVolume(ctx, req)
				Expect(err).To(HaveOccurred())
				common.CheckGRPCErr(err, codes.InvalidArgument, "PublishContext.virtioFSTag: field is required")
				Expect(resp).To(BeNil())
			})
		})

		Context("Publish", func() {
			var (
				mountUtils *mountUtilsMockPkg.MockUtils
				testCtrl   *gomock.Controller
			)
			BeforeEach(func() {
				testCtrl = gomock.NewController(GinkgoT())
				mountUtils = mountUtilsMockPkg.NewMockUtils(testCtrl)
				nodeHandler.mount = mountUtils
			})
			AfterEach(func() {
				testCtrl.Finish()
			})

			It("should publish the volume successfully", func() {
				mountUtils.EXPECT().EnsureDirExist("/target/path", os.FileMode(0755)).Return(nil)
				mountUtils.EXPECT().CheckMountExists("test-tag", "/target/path").Return(false, kmount.MountInfo{}, nil)
				mountUtils.EXPECT().Mount("test-tag", "/target/path", "virtiofs", []string{"testoption"}).Return(nil)
				resp, err := nodeHandler.NodePublishVolume(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).NotTo(BeNil())
			})

			It("should publish the volume successfully with readonly flag", func() {
				req.Readonly = true
				mountUtils.EXPECT().EnsureDirExist("/target/path", os.FileMode(0755)).Return(nil)
				mountUtils.EXPECT().CheckMountExists("test-tag", "/target/path").Return(false, kmount.MountInfo{}, nil)
				mountUtils.EXPECT().Mount("test-tag", "/target/path", "virtiofs", []string{"ro", "testoption"}).Return(nil)
				resp, err := nodeHandler.NodePublishVolume(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).NotTo(BeNil())
			})

			It("should publish the volume successfully with readonly access mode", func() {
				req.VolumeCapability.AccessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY
				mountUtils.EXPECT().EnsureDirExist("/target/path", os.FileMode(0755)).Return(nil)
				mountUtils.EXPECT().CheckMountExists("test-tag", "/target/path").Return(false, kmount.MountInfo{}, nil)
				mountUtils.EXPECT().Mount("test-tag", "/target/path", "virtiofs", []string{"ro", "testoption"}).Return(nil)
				resp, err := nodeHandler.NodePublishVolume(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).NotTo(BeNil())
			})

			It("already published with same mount options", func() {
				mountUtils.EXPECT().EnsureDirExist("/target/path", os.FileMode(0755)).Return(nil)
				mountUtils.EXPECT().CheckMountExists("test-tag", "/target/path").Return(true, kmount.MountInfo{MountOptions: []string{"testoption"}}, nil)
				resp, err := nodeHandler.NodePublishVolume(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).NotTo(BeNil())
			})

			It("should return error if already published with different mount options", func() {
				mountUtils.EXPECT().EnsureDirExist("/target/path", os.FileMode(0755)).Return(nil)
				mountUtils.EXPECT().CheckMountExists("test-tag", "/target/path").Return(true, kmount.MountInfo{MountOptions: []string{"something"}}, nil)
				resp, err := nodeHandler.NodePublishVolume(ctx, req)
				common.CheckGRPCErr(err, codes.AlreadyExists, "volume already published with different mount options")
				Expect(resp).To(BeNil())
			})

			It("should return error if readonly volume already published without ro flag", func() {
				req.Readonly = true
				mountUtils.EXPECT().EnsureDirExist("/target/path", os.FileMode(0755)).Return(nil)
				mountUtils.EXPECT().CheckMountExists("test-tag", "/target/path").Return(true, kmount.MountInfo{MountOptions: []string{"testoption"}}, nil)
				resp, err := nodeHandler.NodePublishVolume(ctx, req)
				common.CheckGRPCErr(err, codes.AlreadyExists, "volume already published with different mount options")
				Expect(resp).To(BeNil())
			})

			It("should return error if EnsureDirExist fails", func() {
				mountUtils.EXPECT().EnsureDirExist("/target/path", os.FileMode(0755)).Return(errTest)
				resp, err := nodeHandler.NodePublishVolume(ctx, req)
				common.CheckGRPCErr(err, codes.Internal, "can't create publish path for the volume")
				Expect(resp).To(BeNil())
			})

			It("should return error if CheckMountExists fails", func() {
				mountUtils.EXPECT().EnsureDirExist("/target/path", os.FileMode(0755)).Return(nil)
				mountUtils.EXPECT().CheckMountExists("test-tag", "/target/path").Return(false, kmount.MountInfo{}, errTest)
				resp, err := nodeHandler.NodePublishVolume(ctx, req)
				common.CheckGRPCErr(err, codes.Internal, "error occurred while checking if the volume is published")
				Expect(resp).To(BeNil())
			})

			It("should return error if Mount fails", func() {
				mountUtils.EXPECT().EnsureDirExist("/target/path", os.FileMode(0755)).Return(nil)
				mountUtils.EXPECT().CheckMountExists("test-tag", "/target/path").Return(false, kmount.MountInfo{}, nil)
				mountUtils.EXPECT().Mount("test-tag", "/target/path", "virtiofs", []string{"testoption"}).Return(errTest)
				resp, err := nodeHandler.NodePublishVolume(ctx, req)
				common.CheckGRPCErr(err, codes.Internal, "error occurred while trying to mount volume")
				Expect(resp).To(BeNil())
			})
		})
	})
})
