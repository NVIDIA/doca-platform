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

package controller

import (
	"context"
	"time"

	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/config"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/handlers/common"

	"github.com/container-storage-interface/spec/lib/go/csi"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc/codes"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("ControllerUnpublish", func() {
	var (
		controllerHandler *controller
		clients           *fakeClusterHelper
		ctx               context.Context
		cancel            context.CancelFunc
		req               *csi.ControllerUnpublishVolumeRequest
	)

	BeforeEach(func() {
		clients = &fakeClusterHelper{
			Client: getClusterClient(),
		}
		controllerHandler = &controller{
			clusterhelper:    clients,
			controllerConfig: config.Controller{Namespace: "test-namespace"},
		}
		ctx, cancel = context.WithCancel(context.Background())
		DeferCleanup(cancel)
		req = &csi.ControllerUnpublishVolumeRequest{
			VolumeId: "test-volume-name",
			NodeId:   "test-node-name",
		}
	})

	Context("Validation", func() {
		It("should return error if VolumeId is empty", func() {
			req.VolumeId = ""
			resp, err := controllerHandler.ControllerUnpublishVolume(ctx, req)
			Expect(resp).To(BeNil())
			common.CheckGRPCErr(err, codes.InvalidArgument, "VolumeId: field is required")
		})
		It("should return error if NodeID is empty", func() {
			req.NodeId = ""
			resp, err := controllerHandler.ControllerUnpublishVolume(ctx, req)
			Expect(resp).To(BeNil())
			common.CheckGRPCErr(err, codes.InvalidArgument, "NodeId: field is required")
		})
	})
	Context("Unpublish", func() {
		var (
			vol       *storagev1.DPUVolume
			volAttach *storagev1.DPUVolumeAttachment
		)

		BeforeEach(func() {
			vol = &storagev1.DPUVolume{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-volume-name",
					Namespace: "test-namespace",
					UID:       types.UID("test-volume-id")},
				Spec: storagev1.DPUVolumeSpec{
					DPUStoragePolicyName: "test-policy-name",
					Parameters: map[string]string{
						"test-param1": "test-param1-value",
						"test-param2": "test-param2-value",
					},
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: *resource.NewQuantity(2147483648, resource.BinarySI),
						},
					},
					VolumeMode: ptr.To(corev1.PersistentVolumeBlock),
				},
				Status: storagev1.DPUVolumeStatus{
					Phase: ptr.To(storagev1.DPUVolumePhaseBound),
					State: &storagev1.DPUVolumeState{
						SelectedDPUStorageVendorName: ptr.To("test-vendor-name"),
						StorageVendorPluginName:      ptr.To("test-vendor-plugin-name"),
					},
				},
			}

			volAttach = &storagev1.DPUVolumeAttachment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-node-name-test-volume-name",
					Namespace: "test-namespace",
				},
				Spec: storagev1.DPUVolumeAttachmentSpec{
					DPUNodeName:   "test-node-name",
					DPUVolumeName: "test-volume-name",
					FunctionTypeConfig: storagev1.FunctionTypeConfig{
						FunctionType:    storagev1.FunctionTypeVF,
						HotplugFunction: false,
					},
				},
				Status: storagev1.DPUVolumeAttachmentStatus{
					ControllerAttached: ptr.To(true),
					DPUAttached:        ptr.To(true),
					AttachmentMetadata: map[string]string{
						"test-publish-param": "test-publish-param-value",
					},
					DPU: &storagev1.AttachmentStatusDPU{
						PCIAddress: ptr.To("0000:00:1f.2"),
						NVMEAttrs: &storagev1.NVMEAttrs{
							NamespaceID: ptr.To(int64(1)),
						},
					},
				},
			}
		})

		It("detach", func() {
			clients.Client = getClusterClient(vol, volAttach)
			ctx, cancel := context.WithTimeout(ctx, time.Second*10)
			defer cancel()
			resp, err := controllerHandler.ControllerUnpublishVolume(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).NotTo(BeNil())
			Expect(clients.Client.Get(ctx, client.ObjectKeyFromObject(volAttach), volAttach)).To(HaveOccurred())
		})
		It("not attached", func() {
			clients.Client = getClusterClient(vol)
			ctx, cancel := context.WithTimeout(ctx, time.Second*10)
			defer cancel()
			resp, err := controllerHandler.ControllerUnpublishVolume(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).NotTo(BeNil())
		})
		It("volume not found", func() {
			clients.Client = getClusterClient()
			ctx, cancel := context.WithTimeout(ctx, time.Second*10)
			defer cancel()
			resp, err := controllerHandler.ControllerUnpublishVolume(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).NotTo(BeNil())
		})
	})
})
