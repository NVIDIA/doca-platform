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

var _ = Describe("ControllerPublish", func() {
	var (
		controllerHandler *controller
		clients           *fakeClusterHelper
		ctx               context.Context
		cancel            context.CancelFunc
		req               *csi.ControllerPublishVolumeRequest
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
		req = &csi.ControllerPublishVolumeRequest{
			VolumeId: "test-volume-id",
			NodeId:   "test-host-node-name",
			VolumeCapability: &csi.VolumeCapability{
				AccessType: &csi.VolumeCapability_Block{
					Block: &csi.VolumeCapability_BlockVolume{},
				},
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
			},
			VolumeContext: map[string]string{
				"storagePolicyName":       "test-policy-name",
				"storageVendorName":       "test-vendor-name",
				"storageVendorPluginName": "test-vendor-plugin-name",
			},
		}
	})

	Context("Validation", func() {
		It("should return error if VolumeId is empty", func() {
			req.VolumeId = ""
			resp, err := controllerHandler.ControllerPublishVolume(ctx, req)
			Expect(resp).To(BeNil())
			common.CheckGRPCErr(err, codes.InvalidArgument, "VolumeId: field is required")
		})
		It("should return error if NodeID is empty", func() {
			req.NodeId = ""
			resp, err := controllerHandler.ControllerPublishVolume(ctx, req)
			Expect(resp).To(BeNil())
			common.CheckGRPCErr(err, codes.InvalidArgument, "NodeId: field is required")
		})
		It("should return error if VolumeCapability is empty", func() {
			req.VolumeCapability = nil
			resp, err := controllerHandler.ControllerPublishVolume(ctx, req)
			Expect(resp).To(BeNil())
			common.CheckGRPCErr(err, codes.InvalidArgument, "VolumeCapability: field is required")
		})
	})
	Context("Publish", func() {
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

		It("attached", func() {
			stop := make(chan struct{})
			clients.DPUClusterClient = getDPUClusterClient(vol)
			clients.HostClusterClient = getHostClusterClient(dpu)

			go func() {
				defer GinkgoRecover()
				defer close(stop)
				Eventually(func(g Gomega) {
					volAttachToUpdate := &storagev1.VolumeAttachment{}
					g.Expect(clients.DPUClusterClient.Get(ctx, client.ObjectKeyFromObject(volAttach), volAttachToUpdate)).NotTo(HaveOccurred())
					volAttachToUpdate.Status = volAttach.Status
					g.Expect(clients.DPUClusterClient.Status().Update(ctx, volAttachToUpdate)).NotTo(HaveOccurred())
				}).WithTimeout(time.Second * 10).Should(Succeed())
			}()

			ctx, cancel := context.WithTimeout(ctx, time.Second*10)
			defer cancel()
			resp, err := controllerHandler.ControllerPublishVolume(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).NotTo(BeNil())
			Eventually(stop).WithTimeout(time.Second * 15).Should(BeClosed())
		})
		It("already attached", func() {
			clients.DPUClusterClient = getDPUClusterClient(vol, volAttach)
			clients.HostClusterClient = getHostClusterClient(dpu)
			ctx, cancel := context.WithTimeout(ctx, time.Second*10)
			defer cancel()
			resp, err := controllerHandler.ControllerPublishVolume(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).NotTo(BeNil())
			Expect(resp.PublishContext).To(Equal(
				map[string]string{
					"nv-volumeName":           "test-volume-name",
					"nv-volumeAttachmentName": generateVolumeAttachmentName("test-namespace", "test-dpu-node-name", "test-volume-id"),
					"nv-pciDeviceAddress":     "0000:00:1f.2",
					"nv-nvmeNsID":             "1",
					"test-publish-param":      "test-publish-param-value",
				},
			))
		})
		It("dpu not found", func() {
			clients.DPUClusterClient = getDPUClusterClient(vol, volAttach)
			ctx, cancel := context.WithTimeout(ctx, time.Second*10)
			defer cancel()
			resp, err := controllerHandler.ControllerPublishVolume(ctx, req)
			common.CheckGRPCErr(err, codes.NotFound, "dpu node not found")
			Expect(resp).To(BeNil())
		})
		It("attached with wrong source", func() {
			volAttach.Spec.Source.VolumeRef.Name = "test-wrong-volume-name"
			clients.DPUClusterClient = getDPUClusterClient(vol, volAttach)
			clients.HostClusterClient = getHostClusterClient(dpu)
			ctx, cancel := context.WithTimeout(ctx, time.Second*10)
			defer cancel()
			resp, err := controllerHandler.ControllerPublishVolume(ctx, req)
			common.CheckGRPCErr(err, codes.AlreadyExists, "VolumeAttachment already exist but with different source")
			Expect(resp).To(BeNil())
		})
		It("attached with wrong nodeName", func() {
			volAttach.Spec.NodeName = "test-wrong-dpu-node-name"
			clients.DPUClusterClient = getDPUClusterClient(vol, volAttach)
			clients.HostClusterClient = getHostClusterClient(dpu)
			ctx, cancel := context.WithTimeout(ctx, time.Second*10)
			defer cancel()
			resp, err := controllerHandler.ControllerPublishVolume(ctx, req)
			common.CheckGRPCErr(err, codes.AlreadyExists, "VolumeAttachment already exist but contains different nodeName")
			Expect(resp).To(BeNil())
		})
	})
})
