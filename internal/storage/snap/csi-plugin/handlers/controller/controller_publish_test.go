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
	"sync/atomic"
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
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
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
			Client: getClusterClient(),
		}
		controllerHandler = &controller{
			commonConfig:     config.Common{EmulationMode: config.EmulationModeNVMe},
			controllerConfig: config.Controller{Namespace: "test-namespace"},
			clusterhelper:    clients,
		}
		ctx, cancel = context.WithTimeout(context.Background(), callTimeout)
		DeferCleanup(cancel)
		req = &csi.ControllerPublishVolumeRequest{
			VolumeId: "test-volume-name",
			NodeId:   "test-node-name",
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
			vol       *storagev1.DPUVolume
			volAttach *storagev1.DPUVolumeAttachment
		)
		sharedPublishTests := func(emulationMode string) {
			Context("Common tests for "+emulationMode, func() {
				BeforeEach(func() {
					controllerHandler.commonConfig.EmulationMode = emulationMode
				})
				It("attached", func() {
					stop := make(chan struct{})
					clients.Client = getClusterClient(vol)

					go func() {
						defer GinkgoRecover()
						defer close(stop)
						Eventually(func(g Gomega) {
							volAttachToUpdate := &storagev1.DPUVolumeAttachment{}
							g.Expect(clients.Client.Get(ctx, client.ObjectKeyFromObject(volAttach), volAttachToUpdate)).NotTo(HaveOccurred())
							volAttachToUpdate.Status = volAttach.Status
							g.Expect(clients.Client.Status().Update(ctx, volAttachToUpdate)).NotTo(HaveOccurred())
						}).WithTimeout(time.Second * 10).Should(Succeed())
					}()
					resp, err := controllerHandler.ControllerPublishVolume(ctx, req)
					Expect(err).NotTo(HaveOccurred())
					Expect(resp).NotTo(BeNil())
					Eventually(stop).WithTimeout(time.Second * 15).Should(BeClosed())
				})
				It("includes function VUID when available", func() {
					volAttachWithVUID := volAttach.DeepCopy()
					volAttachWithVUID.Status.DPU.FuncVUID = ptr.To("test-function-vuid")
					clients.Client = getClusterClient(vol, volAttachWithVUID)

					resp, err := controllerHandler.ControllerPublishVolume(ctx, req)
					Expect(err).NotTo(HaveOccurred())
					Expect(resp).NotTo(BeNil())
					Expect(resp.PublishContext).To(HaveKeyWithValue(common.PublishCtxFuncVUID, "test-function-vuid"))
				})
				It("volume not found", func() {
					resp, err := controllerHandler.ControllerPublishVolume(ctx, req)
					common.CheckGRPCErr(err, codes.NotFound, "volume not found")
					Expect(resp).To(BeNil())
				})
				It("attached with wrong source", func() {
					wrongVolAttach := volAttach.DeepCopy()
					wrongVolAttach.Spec.DPUVolumeName = "test-wrong-volume-name"
					clients.Client = getClusterClient(vol, wrongVolAttach)
					resp, err := controllerHandler.ControllerPublishVolume(ctx, req)
					common.CheckGRPCErr(err, codes.AlreadyExists, "DPUVolumeAttachment already exist but contains different parameters")
					Expect(resp).To(BeNil())
				})
				It("attached with wrong nodeName", func() {
					wrongVolAttach := volAttach.DeepCopy()
					wrongVolAttach.Spec.DPUNodeName = "test-wrong-dpu-node-name"
					clients.Client = getClusterClient(vol, wrongVolAttach)
					resp, err := controllerHandler.ControllerPublishVolume(ctx, req)
					common.CheckGRPCErr(err, codes.AlreadyExists, "DPUVolumeAttachment already exist but contains different parameters")
					Expect(resp).To(BeNil())
				})
				It("timeout", func() {
					var volAttachCalled atomic.Bool
					clients.Client = getClusterClientBuilder(vol).WithInterceptorFuncs(interceptor.Funcs{Create: func(
						ctx context.Context, client client.WithWatch,
						obj client.Object, opts ...client.CreateOption) error {
						// Mark that DPUVolumeAttachment creation was attempted
						if _, ok := obj.(*storagev1.DPUVolumeAttachment); ok {
							volAttachCalled.Store(true)
						}
						return client.Create(ctx, obj, opts...)
					}}).Build()
					go func() {
						// Wait until the attachment creation is observed, then cancel context
						Eventually(func(g Gomega) {
							g.Expect(volAttachCalled.Load()).To(BeTrue())
						}).WithPolling(time.Millisecond * 100).Should(Succeed())
						cancel()
					}()
					resp, err := controllerHandler.ControllerPublishVolume(ctx, req)
					// Expect a timeout error because attachment never becomes "attached"
					common.CheckGRPCErr(err, codes.DeadlineExceeded, "timeout occurred while waiting for volume attachment")
					Expect(resp).To(BeNil())
				})
				It("failed to get client", func() {
					clients.Error = errTest
					resp, err := controllerHandler.ControllerPublishVolume(ctx, req)
					common.CheckGRPCErr(err, codes.Internal, "failed to get kubernetes client for target cluster")
					Expect(resp).To(BeNil())
				})
				It("failed to read volume info", func() {
					clients.Client = getClusterClientBuilder().WithInterceptorFuncs(interceptor.Funcs{Get: func(
						ctx context.Context, client client.WithWatch,
						key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						return errTest
					}}).Build()
					resp, err := controllerHandler.ControllerPublishVolume(ctx, req)
					common.CheckGRPCErr(err, codes.Internal, "failed to read volume info")
					Expect(resp).To(BeNil())
				})
				It("failed to create volume attachment", func() {
					clients.Client = getClusterClientBuilder(vol).WithInterceptorFuncs(interceptor.Funcs{Create: func(
						ctx context.Context, client client.WithWatch,
						obj client.Object, opts ...client.CreateOption) error {
						return errTest
					}}).Build()
					resp, err := controllerHandler.ControllerPublishVolume(ctx, req)
					common.CheckGRPCErr(err, codes.Internal, "failed to create DPUVolumeAttachment")
					Expect(resp).To(BeNil())
				})
				It("failed to get volume attachment", func() {
					clients.Client = getClusterClientBuilder(vol, volAttach).WithInterceptorFuncs(interceptor.Funcs{Get: func(
						ctx context.Context, client client.WithWatch,
						key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if _, ok := obj.(*storagev1.DPUVolumeAttachment); ok {
							return errTest
						}
						return client.Get(ctx, key, obj, opts...)
					}}).Build()
					resp, err := controllerHandler.ControllerPublishVolume(ctx, req)
					common.CheckGRPCErr(err, codes.Internal, "failed to read DPUVolumeAttachment info")
					Expect(resp).To(BeNil())
				})
				It("called with invalid function type config", func() {
					req.VolumeContext["functionType"] = "invalid"
					clients.Client = getClusterClient(vol)
					resp, err := controllerHandler.ControllerPublishVolume(ctx, req)
					common.CheckGRPCErr(err, codes.InvalidArgument, "invalid function type config")
					Expect(resp).To(BeNil())
				})
				It("should return error when DPUVolumeAttachment status.dpu is nil", func() {
					invalidVolAttach := volAttach.DeepCopy()
					invalidVolAttach.Status.DPU = nil
					clients.Client = getClusterClient(vol, invalidVolAttach)
					resp, err := controllerHandler.ControllerPublishVolume(ctx, req)
					common.CheckGRPCErr(err, codes.Internal, "DPUVolumeAttachment is ready but status.dpu is missing")
					Expect(resp).To(BeNil())
				})
				It("should return error when DPUVolumeAttachment status.dpu.pciAddress is nil", func() {
					invalidVolAttach := volAttach.DeepCopy()
					invalidVolAttach.Status.DPU.PCIAddress = nil
					clients.Client = getClusterClient(vol, invalidVolAttach)
					resp, err := controllerHandler.ControllerPublishVolume(ctx, req)
					common.CheckGRPCErr(err, codes.Internal, "DPUVolumeAttachment is ready but status.dpu.pciAddress is missing")
					Expect(resp).To(BeNil())
				})

			})
		}

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

		Context("NVMe emulation mode", func() {
			BeforeEach(func() {
				controllerHandler.commonConfig.EmulationMode = config.EmulationModeNVMe
			})
			sharedPublishTests(config.EmulationModeNVMe)
			It("already attached", func() {
				clients.Client = getClusterClient(vol, volAttach)
				resp, err := controllerHandler.ControllerPublishVolume(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).NotTo(BeNil())
				Expect(resp.PublishContext).To(Equal(
					map[string]string{
						"nv-volumeName":           "test-volume-name",
						"nv-volumeAttachmentName": "test-node-name-test-volume-name",
						"nv-pciDeviceAddress":     "0000:00:1f.2",
						"nv-nvmeNsID":             "1",
						"test-publish-param":      "test-publish-param-value",
					},
				))
			})
			It("should return error when DPUVolumeAttachment status.dpu.nvmeAttrs is nil", func() {
				invalidVolAttach := volAttach.DeepCopy()
				invalidVolAttach.Status.DPU.NVMEAttrs = nil
				clients.Client = getClusterClient(vol, invalidVolAttach)
				resp, err := controllerHandler.ControllerPublishVolume(ctx, req)
				common.CheckGRPCErr(err, codes.Internal, "DPUVolumeAttachment is ready but status.dpu.nvmeAttrs is missing")
				Expect(resp).To(BeNil())
			})
			It("should return error when DPUVolumeAttachment status.dpu.nvmeAttrs.namespaceID is nil", func() {
				invalidVolAttach := volAttach.DeepCopy()
				invalidVolAttach.Status.DPU.NVMEAttrs.NamespaceID = nil
				clients.Client = getClusterClient(vol, invalidVolAttach)
				resp, err := controllerHandler.ControllerPublishVolume(ctx, req)
				common.CheckGRPCErr(err, codes.Internal, "DPUVolumeAttachment is ready but status.dpu.nvmeAttrs.namespaceID is missing")
				Expect(resp).To(BeNil())
			})
		})
		Context("VirtioFS emulation mode", func() {
			BeforeEach(func() {
				controllerHandler.commonConfig.EmulationMode = config.EmulationModeVirtiofs
				vol.Spec.VolumeMode = ptr.To(corev1.PersistentVolumeFilesystem)
				volAttach.Spec.FunctionTypeConfig.FunctionType = storagev1.FunctionTypePF
				volAttach.Spec.FunctionTypeConfig.HotplugFunction = true
				volAttach.Status.DPU.NVMEAttrs = nil
				volAttach.Status.DPU.VirtioFSAttrs = &storagev1.VirtioFSAttrs{
					FilesystemTag: ptr.To("test-virtiofs-tag"),
				}
				req.VolumeCapability = &csi.VolumeCapability{
					AccessType: &csi.VolumeCapability_Mount{
						Mount: &csi.VolumeCapability_MountVolume{},
					},
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
					},
				}
				req.VolumeContext["functionType"] = "pf"
				req.VolumeContext["hotplugFunction"] = "true"
			})
			sharedPublishTests(config.EmulationModeVirtiofs)
			It("already attached", func() {
				clients.Client = getClusterClient(vol, volAttach)
				resp, err := controllerHandler.ControllerPublishVolume(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).NotTo(BeNil())
				Expect(resp.PublishContext).To(Equal(
					map[string]string{
						"nv-volumeName":           "test-volume-name",
						"nv-volumeAttachmentName": "test-node-name-test-volume-name",
						"nv-pciDeviceAddress":     "0000:00:1f.2",
						"nv-virtioFsTag":          "test-virtiofs-tag",
						"test-publish-param":      "test-publish-param-value",
					},
				))
			})
			It("should return error when DPUVolumeAttachment status.dpu.virtioFSAttrs is nil", func() {
				invalidVolAttach := volAttach.DeepCopy()
				invalidVolAttach.Status.DPU.VirtioFSAttrs = nil
				clients.Client = getClusterClient(vol, invalidVolAttach)
				resp, err := controllerHandler.ControllerPublishVolume(ctx, req)
				common.CheckGRPCErr(err, codes.Internal, "DPUVolumeAttachment is ready but status.dpu.virtioFSAttrs is missing")
				Expect(resp).To(BeNil())
			})
			It("should return error when FilesystemTag is nil in VirtioFS mode", func() {
				invalidVolAttach := volAttach.DeepCopy()
				invalidVolAttach.Status.DPU.VirtioFSAttrs = &storagev1.VirtioFSAttrs{
					FilesystemTag: nil,
				}
				clients.Client = getClusterClient(vol, invalidVolAttach)
				resp, err := controllerHandler.ControllerPublishVolume(ctx, req)
				common.CheckGRPCErr(err, codes.Internal, "DPUVolumeAttachment is ready but status.dpu.virtioFSAttrs.filesystemTag is missing")
				Expect(resp).To(BeNil())
			})
		})
	})
})
