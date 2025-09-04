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
)

var _ = Describe("ValidateVolumeCapabilities", func() {
	var (
		clients           *fakeClusterHelper
		controllerHandler *controller
		ctx               context.Context
		req               *csi.ValidateVolumeCapabilitiesRequest
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
		ctx = context.Background()
		req = &csi.ValidateVolumeCapabilitiesRequest{
			VolumeId: "test-volume-name",
			VolumeContext: map[string]string{
				"storagePolicyName":       "test-policy-name",
				"storageVendorName":       "test-vendor-name",
				"storageVendorPluginName": "test-vendor-plugin-name",
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
				"test-param1": "test-param1-value",
				"test-param2": "test-param2-value",
			},
		}
	})

	Context("Validation", func() {
		It("should return error if VolumeId is empty", func() {
			req.VolumeId = ""
			resp, err := controllerHandler.ValidateVolumeCapabilities(ctx, req)
			Expect(resp).To(BeNil())
			common.CheckGRPCErr(err, codes.InvalidArgument, "VolumeId: field is required")
		})
		It("should return error if VolumeCapability is empty", func() {
			req.VolumeCapabilities = nil
			resp, err := controllerHandler.ValidateVolumeCapabilities(ctx, req)
			Expect(resp).To(BeNil())
			common.CheckGRPCErr(err, codes.InvalidArgument, "VolumeCapabilities: field is required")
		})
	})

	Context("Check", func() {
		var (
			vol *storagev1.DPUVolume
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
		It("valid", func() {
			clients.Client = getClusterClient(vol)
			resp, err := controllerHandler.ValidateVolumeCapabilities(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).NotTo(BeNil())
			Expect(resp.Confirmed).NotTo(BeNil())
			Expect(resp.Confirmed.VolumeContext).To(Equal(req.VolumeContext))
			Expect(resp.Confirmed.VolumeCapabilities).To(Equal(req.VolumeCapabilities))
			Expect(resp.Confirmed.Parameters).To(Equal(req.Parameters))
		})
		It("parameters mismatch", func() {
			vol.Spec.Parameters["new-opt"] = "new-opt-value"
			clients.Client = getClusterClient(vol)
			resp, err := controllerHandler.ValidateVolumeCapabilities(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).NotTo(BeNil())
			Expect(resp.Confirmed).To(BeNil())
			Expect(resp.Message).To(ContainSubstring("parameters mismatch"))
		})
		It("context mismatch", func() {
			vol.Spec.DPUStoragePolicyName = "new-policy"
			clients.Client = getClusterClient(vol)
			resp, err := controllerHandler.ValidateVolumeCapabilities(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).NotTo(BeNil())
			Expect(resp.Confirmed).To(BeNil())
			Expect(resp.Message).To(ContainSubstring("volumeCtx mismatch"))
		})
		It("capabilities mismatch", func() {
			vol.Spec.VolumeMode = ptr.To(corev1.PersistentVolumeFilesystem)
			clients.Client = getClusterClient(vol)
			resp, err := controllerHandler.ValidateVolumeCapabilities(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).NotTo(BeNil())
			Expect(resp.Confirmed).To(BeNil())
			Expect(resp.Message).To(ContainSubstring("volumeMode mismatch"))
		})
	})
})
