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

var _ = Describe("DeleteVolume", func() {
	var (
		controllerHandler *controller
		clients           *fakeClusterHelper
		ctx               context.Context
		cancel            context.CancelFunc
		req               *csi.DeleteVolumeRequest
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
		req = &csi.DeleteVolumeRequest{
			VolumeId: "test-volume-id",
		}
	})

	Context("Validation", func() {
		It("should return error if VolumeId is empty", func() {
			req.VolumeId = ""
			resp, err := controllerHandler.DeleteVolume(ctx, req)
			Expect(resp).To(BeNil())
			common.CheckGRPCErr(err, codes.InvalidArgument, "VolumeID: field is required")
		})
	})
	Context("Delete", func() {
		It("should delete volume", func() {
			vol := &storagev1.Volume{
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
				Status: storagev1.VolumeStatus{
					State: storagev1.VolumeStateAvailable,
				},
			}
			clients.DPUClusterClient = getDPUClusterClient(vol)
			Expect(clients.DPUClusterClient.Get(ctx, client.ObjectKeyFromObject(vol), vol)).NotTo(HaveOccurred())
			ctx, cancel := context.WithTimeout(ctx, time.Second*10)
			defer cancel()
			resp, err := controllerHandler.DeleteVolume(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).NotTo(BeNil())
			Expect(clients.DPUClusterClient.Get(ctx, client.ObjectKeyFromObject(vol), vol)).To(HaveOccurred())
		})
	})
})
