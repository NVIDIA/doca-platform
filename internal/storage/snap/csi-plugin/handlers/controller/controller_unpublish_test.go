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

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
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
			DPUClusterClient:  getDPUClusterClient(),
			HostClusterClient: getHostClusterClient(),
		}
		controllerHandler = &controller{
			clusterhelper: clients,
			cfg:           config.Controller{TargetNamespace: "test-namespace"},
		}
		ctx, cancel = context.WithCancel(context.Background())
		DeferCleanup(cancel)
		req = &csi.ControllerUnpublishVolumeRequest{
			VolumeId: "test-volume-id",
			NodeId:   "test-host-node-name",
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
			vol       *storagev1.Volume
			volAttach *storagev1.VolumeAttachment
			dpu       *provisioningv1.DPU
		)

		BeforeEach(func() {
			vol = &storagev1.Volume{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-volume-name",
					Namespace: "test-namespace",
					UID:       types.UID("test-volume-id")},
				Spec: storagev1.VolumeSpec{
					StorageParameters: map[string]string{
						"policy":      "test-policy-name",
						"test-param1": "test-param1-value",
						"test-param2": "test-param2-value",
					},
					Request: storagev1.VolumeRequest{
						CapacityRange: storagev1.CapacityRange{
							Request: *resource.NewQuantity(2147483648, resource.BinarySI),
							Limit:   *resource.NewQuantity(4294967296, resource.BinarySI),
						},
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						VolumeMode:  ptr.To(corev1.PersistentVolumeBlock),
					},
					VolumeSpecDPU: storagev1.VolumeSpecDPU{
						Capacity:                *resource.NewQuantity(2147483648, resource.BinarySI),
						StorageVendorName:       "test-vendor-name",
						StorageVendorPluginName: "test-vendor-plugin-name",
					},
				},
			}
			volAttach = &storagev1.VolumeAttachment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      generateVolumeAttachmentName("test-namespace", "test-dpu-node-name", "test-volume-id"),
					Namespace: "test-namespace",
				},
				Spec: storagev1.VolumeAttachmentSpec{
					NodeName: "test-dpu-node-name",
					Source: storagev1.VolumeSource{VolumeRef: &storagev1.ObjectRef{
						Kind:       storagev1.VolumeKind,
						APIVersion: storagev1.GroupVersion.String(),
						Namespace:  "test-namespace",
						Name:       "test-volume-name",
					}},
					Parameters: map[string]string{
						"test-publish-param": "test-publish-param-value",
					},
				},
				Status: storagev1.VolumeAttachmentStatus{
					StorageAttached: true,
					DPU: storagev1.VolumeAttachmentStatusDPU{
						Attached:         true,
						PCIDeviceAddress: "0000:00:1f.2",
						BdevAttrs: storagev1.BdevAttrs{
							NVMeNsID: 1,
						},
					},
				},
			}
			dpu = &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpu-node-name",
					Namespace: "test-namespace",
				},
				Spec: provisioningv1.DPUSpec{
					DPUNodeName: "test-host-node-name",
				},
			}
		})

		It("detach", func() {
			clients.DPUClusterClient = getDPUClusterClient(vol, volAttach)
			clients.HostClusterClient = getHostClusterClient(dpu)
			ctx, cancel := context.WithTimeout(ctx, time.Second*10)
			defer cancel()
			resp, err := controllerHandler.ControllerUnpublishVolume(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).NotTo(BeNil())
			Expect(clients.DPUClusterClient.Get(ctx, client.ObjectKeyFromObject(volAttach), volAttach)).To(HaveOccurred())
		})
		It("not attached", func() {
			clients.DPUClusterClient = getDPUClusterClient(vol)
			clients.HostClusterClient = getHostClusterClient(dpu)
			ctx, cancel := context.WithTimeout(ctx, time.Second*10)
			defer cancel()
			resp, err := controllerHandler.ControllerUnpublishVolume(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).NotTo(BeNil())
		})
		It("volume not found", func() {
			clients.DPUClusterClient = getDPUClusterClient()
			clients.HostClusterClient = getHostClusterClient(dpu)
			ctx, cancel := context.WithTimeout(ctx, time.Second*10)
			defer cancel()
			resp, err := controllerHandler.ControllerUnpublishVolume(ctx, req)
			common.CheckGRPCErr(err, codes.NotFound, "volume not found")
			Expect(resp).NotTo(BeNil())
		})
		It("dpu not found", func() {
			clients.DPUClusterClient = getDPUClusterClient(vol)
			clients.HostClusterClient = getHostClusterClient()
			ctx, cancel := context.WithTimeout(ctx, time.Second*10)
			defer cancel()
			resp, err := controllerHandler.ControllerUnpublishVolume(ctx, req)
			common.CheckGRPCErr(err, codes.NotFound, "dpu node not found")
			Expect(resp).NotTo(BeNil())
		})
	})
})
