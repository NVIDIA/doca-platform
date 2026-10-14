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

	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/handlers/common"
	mountUtilsMockPkg "github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/mount/mock"

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

	BeforeEach(func() {
		nodeHandler = &node{}
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
	Context("NodeUnstageVolume", func() {
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

		It("should unpublish the volume successfully", func() {
			mountUtils.EXPECT().UnmountAndRemove("/target/path/test-volume-id").Return(nil)
			resp, err := nodeHandler.NodeUnstageVolume(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).NotTo(BeNil())
		})

		It("should return error if unmount fails", func() {
			mountUtils.EXPECT().UnmountAndRemove("/target/path/test-volume-id").Return(errTest)
			resp, err := nodeHandler.NodeUnstageVolume(ctx, req)
			common.CheckGRPCErr(err, codes.Internal, "failed to unmount staging path")
			Expect(resp).To(BeNil())
		})
	})
})
