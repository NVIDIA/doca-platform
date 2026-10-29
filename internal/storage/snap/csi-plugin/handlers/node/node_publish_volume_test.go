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

	BeforeEach(func() {
		nodeHandler = &node{}
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
			common.CheckGRPCErr(err, codes.Internal, "can't create staging path for the volume")
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
