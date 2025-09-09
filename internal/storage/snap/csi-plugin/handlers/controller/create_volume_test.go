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

var _ = Describe("CreateVolume", func() {
	var (
		controllerHandler *controller
		clients           *fakeClusterHelper
		ctx               context.Context
		cancel            context.CancelFunc
		req               *csi.CreateVolumeRequest
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
		req = &csi.CreateVolumeRequest{
			Name: "test-volume-name",
			CapacityRange: &csi.CapacityRange{
				RequiredBytes: 2147483648, // 2 GiB
				LimitBytes:    4294967296, // 4 GiB
			},
			VolumeCapabilities: []*csi.VolumeCapability{
				{
					AccessType: &csi.VolumeCapability_Block{
						Block: &csi.VolumeCapability_BlockVolume{},
					},
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
					},
				},
			},
			Parameters: map[string]string{
				"policy":      "test-policy-name",
				"test-param1": "test-param1-value",
				"test-param2": "test-param2-value",
			},
		}
	})

	Context("Validation", func() {
		It("should return error if Name is empty", func() {
			req.Name = ""
			resp, err := controllerHandler.CreateVolume(ctx, req)
			Expect(resp).To(BeNil())
			common.CheckGRPCErr(err, codes.InvalidArgument, "Name: field is required")
		})
		It("should return error if VolumeCapability is empty", func() {
			req.VolumeCapabilities = nil
			resp, err := controllerHandler.CreateVolume(ctx, req)
			Expect(resp).To(BeNil())
			common.CheckGRPCErr(err, codes.InvalidArgument, "VolumeCapabilities: field is required")
		})
		It("should return error if mount volume requested", func() {
			req.VolumeCapabilities[0].AccessType = &csi.VolumeCapability_Mount{
				Mount: &csi.VolumeCapability_MountVolume{},
			}
			resp, err := controllerHandler.CreateVolume(ctx, req)
			Expect(resp).To(BeNil())
			common.CheckGRPCErr(err, codes.Unimplemented, "VolumeCapabilities[0].Mount is not supported")
		})
		It("should return error if VolumeContentSource set", func() {
			req.VolumeContentSource = &csi.VolumeContentSource{}
			resp, err := controllerHandler.CreateVolume(ctx, req)
			Expect(resp).To(BeNil())
			common.CheckGRPCErr(err, codes.InvalidArgument, "volume content source is not supported")
		})
		It("should return error if policy parameter in not set", func() {
			delete(req.Parameters, common.StorageClassPolicyKey)
			resp, err := controllerHandler.CreateVolume(ctx, req)
			Expect(resp).To(BeNil())
			common.CheckGRPCErr(err, codes.InvalidArgument, "Parameters.policy: field is required")
		})
	})

	Context("Create", func() {
		var (
			vol *storagev1.DPUVolume
		)
		sharedCreateTests := func(emulationMode string) {
			Context("Common tests for "+emulationMode, func() {
				BeforeEach(func() {
					controllerHandler.commonConfig.EmulationMode = emulationMode
				})
				It("already exist", func() {
					clients.Client = getClusterClient(vol)
					resp, err := controllerHandler.CreateVolume(ctx, req)
					Expect(err).NotTo(HaveOccurred())
					Expect(resp).NotTo(BeNil())
					Expect(resp.Volume).NotTo(BeNil())
					Expect(resp.Volume.VolumeId).To(Equal("test-volume-name"))
				})
				It("exist with different request", func() {
					vol.Spec.Resources.Requests[corev1.ResourceStorage] = *resource.NewQuantity(1073741824, resource.BinarySI)
					clients.Client = getClusterClient(vol)
					resp, err := controllerHandler.CreateVolume(ctx, req)
					common.CheckGRPCErr(err, codes.AlreadyExists, "volume already exist but with different parameters")
					Expect(resp).To(BeNil())
				})
				It("exist with different parameters", func() {
					vol.Spec.Parameters["test-param3"] = "test-param3-value"
					clients.Client = getClusterClient(vol)
					resp, err := controllerHandler.CreateVolume(ctx, req)
					common.CheckGRPCErr(err, codes.AlreadyExists, "volume already exist but with different parameters")
					Expect(resp).To(BeNil())
				})
				It("timeout", func() {
					var volCreateCalled atomic.Bool
					clients.Client = getClusterClientBuilder().WithInterceptorFuncs(interceptor.Funcs{Create: func(
						ctx context.Context, client client.WithWatch,
						obj client.Object, opts ...client.CreateOption) error {
						// Mark that DPUVolume creation was attempted
						if _, ok := obj.(*storagev1.DPUVolume); ok {
							volCreateCalled.Store(true)
						}
						return client.Create(ctx, obj, opts...)
					}}).Build()
					go func() {
						// Wait until the volume creation is observed, then cancel context
						Eventually(func(g Gomega) {
							g.Expect(volCreateCalled.Load()).To(BeTrue())
						}).WithPolling(time.Millisecond * 100).Should(Succeed())
						cancel()
					}()
					resp, err := controllerHandler.CreateVolume(ctx, req)
					// Expect a timeout error because volume never becomes "ready"
					common.CheckGRPCErr(err, codes.DeadlineExceeded, "timeout occurred while waiting for volume creation")
					Expect(resp).To(BeNil())
				})
				It("failed to get client", func() {
					clients.Error = errTest
					resp, err := controllerHandler.CreateVolume(ctx, req)
					common.CheckGRPCErr(err, codes.Internal, "failed to get kubernetes client for target cluster")
					Expect(resp).To(BeNil())
				})
				It("failed to create", func() {
					clients.Client = getClusterClientBuilder().WithInterceptorFuncs(interceptor.Funcs{Create: func(
						ctx context.Context, client client.WithWatch,
						obj client.Object, opts ...client.CreateOption) error {
						return errTest
					}}).Build()
					resp, err := controllerHandler.CreateVolume(ctx, req)
					common.CheckGRPCErr(err, codes.Internal, "failed to create volume")
					Expect(resp).To(BeNil())
				})
				It("failed to get after already exist", func() {
					clients.Client = getClusterClientBuilder(vol).WithInterceptorFuncs(interceptor.Funcs{Get: func(
						ctx context.Context, client client.WithWatch,
						key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						return errTest
					}}).Build()
					resp, err := controllerHandler.CreateVolume(ctx, req)
					common.CheckGRPCErr(err, codes.Internal, "failed to read volume info")
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
						Limits: corev1.ResourceList{
							corev1.ResourceStorage: *resource.NewQuantity(4294967296, resource.BinarySI),
						},
					},
					VolumeMode: ptr.To(corev1.PersistentVolumeBlock),
				},
				Status: storagev1.DPUVolumeStatus{
					Phase: ptr.To(storagev1.DPUVolumePhaseBound),
					State: &storagev1.DPUVolumeState{
						SelectedDPUStorageVendorName: ptr.To("test-vendor-name"),
						StorageVendorPluginName:      ptr.To("test-vendor-plugin-name"),
						VolumeInfo: &storagev1.VolumeInfo{
							Capacity: corev1.ResourceList{
								corev1.ResourceStorage: *resource.NewQuantity(2147483648, resource.BinarySI),
							},
							AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
							VolumeMode:  ptr.To(corev1.PersistentVolumeBlock),
						},
					},
				},
			}
		})
		Context("NVMe emulation mode", func() {
			BeforeEach(func() {
				controllerHandler.commonConfig.EmulationMode = config.EmulationModeNVMe
			})
			sharedCreateTests(config.EmulationModeNVMe)
			It("should create volume with block mode", func() {
				stop := make(chan struct{})
				go func() {
					defer GinkgoRecover()
					defer close(stop)
					c, _ := controllerHandler.clusterhelper.GetClient(ctx)
					Eventually(func(g Gomega) {
						volToUpdate := &storagev1.DPUVolume{}
						g.Expect(c.Get(ctx, client.ObjectKeyFromObject(vol), volToUpdate)).NotTo(HaveOccurred())
						volToUpdate.Status = vol.Status
						g.Expect(c.Status().Update(ctx, volToUpdate)).NotTo(HaveOccurred())
					}).WithTimeout(time.Second * 10).Should(Succeed())
				}()
				resp, err := controllerHandler.CreateVolume(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).NotTo(BeNil())
				Expect(resp.Volume).NotTo(BeNil())
				Expect(resp.Volume.VolumeId).To(Equal("test-volume-name"))
				Expect(resp.Volume.CapacityBytes).To(Equal(int64(2147483648)))
				Expect(resp.Volume.VolumeContext).To(HaveKeyWithValue("storagePolicyName", "test-policy-name"))
				Expect(resp.Volume.VolumeContext).To(HaveKeyWithValue("storageVendorName", "test-vendor-name"))
				Expect(resp.Volume.VolumeContext).To(HaveKeyWithValue("storageVendorPluginName", "test-vendor-plugin-name"))
				Eventually(stop).WithTimeout(time.Second * 15).Should(BeClosed())
			})
			It("exist with different volume mode", func() {
				vol.Spec.VolumeMode = ptr.To(corev1.PersistentVolumeFilesystem)
				clients.Client = getClusterClient(vol)
				resp, err := controllerHandler.CreateVolume(ctx, req)
				common.CheckGRPCErr(err, codes.AlreadyExists, "volume already exist but with different parameters")
				Expect(resp).To(BeNil())
			})
		})
		Context("VirtioFS emulation mode", func() {
			BeforeEach(func() {
				controllerHandler.commonConfig.EmulationMode = config.EmulationModeVirtiofs
				vol.Spec.VolumeMode = ptr.To(corev1.PersistentVolumeFilesystem)
				req.Parameters["functionType"] = "pf"
				req.Parameters["hotplugFunction"] = "true"
				req.VolumeCapabilities[0].AccessType = &csi.VolumeCapability_Mount{
					Mount: &csi.VolumeCapability_MountVolume{},
				}
			})
			sharedCreateTests(config.EmulationModeVirtiofs)
			It("should create volume with filesystem mode", func() {
				stop := make(chan struct{})
				go func() {
					defer GinkgoRecover()
					defer close(stop)
					c, _ := controllerHandler.clusterhelper.GetClient(ctx)
					Eventually(func(g Gomega) {
						volToUpdate := &storagev1.DPUVolume{}
						g.Expect(c.Get(ctx, client.ObjectKeyFromObject(vol), volToUpdate)).NotTo(HaveOccurred())
						volToUpdate.Status = vol.Status
						g.Expect(c.Status().Update(ctx, volToUpdate)).NotTo(HaveOccurred())
					}).WithTimeout(time.Second * 10).Should(Succeed())
				}()
				resp, err := controllerHandler.CreateVolume(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).NotTo(BeNil())
				Expect(resp.Volume).NotTo(BeNil())
				Expect(resp.Volume.VolumeId).To(Equal("test-volume-name"))
				Expect(resp.Volume.CapacityBytes).To(Equal(int64(2147483648)))
				Expect(resp.Volume.VolumeContext).To(HaveKeyWithValue("storagePolicyName", "test-policy-name"))
				Expect(resp.Volume.VolumeContext).To(HaveKeyWithValue("storageVendorName", "test-vendor-name"))
				Expect(resp.Volume.VolumeContext).To(HaveKeyWithValue("storageVendorPluginName", "test-vendor-plugin-name"))
				Eventually(stop).WithTimeout(time.Second * 15).Should(BeClosed())
			})
			It("exist with different volume mode", func() {
				vol.Spec.VolumeMode = ptr.To(corev1.PersistentVolumeBlock)
				clients.Client = getClusterClient(vol)
				resp, err := controllerHandler.CreateVolume(ctx, req)
				common.CheckGRPCErr(err, codes.AlreadyExists, "volume already exist but with different parameters")
				Expect(resp).To(BeNil())
			})
		})
	})
})
