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

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"

	"github.com/container-storage-interface/spec/lib/go/csi"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

var _ = Describe("Helpers", func() {
	Context("generateVolumeAttachmentName", func() {
		It("should produce same name for same inputs", func() {
			Expect(generateVolumeAttachmentName("test", "dpu-node", "vol1")).To(
				Equal(generateVolumeAttachmentName("test", "dpu-node", "vol1")))
		})
		It("should add common prefix", func() {
			Expect(generateVolumeAttachmentName("test", "dpu-node", "vol1")).To(HavePrefix("pva-"))
		})
		It("should produce different names for different inputs", func() {
			Expect(generateVolumeAttachmentName("test1", "dpu-node", "vol1")).NotTo(
				Equal(generateVolumeAttachmentName("test2", "dpu-node", "vol1")))
			Expect(generateVolumeAttachmentName("test", "dpu-node1", "vol1")).NotTo(
				Equal(generateVolumeAttachmentName("test", "dpu-node2", "vol1")))
			Expect(generateVolumeAttachmentName("test", "dpu-node", "vol1")).NotTo(
				Equal(generateVolumeAttachmentName("test", "dpu-node", "vol2")))
			Expect(generateVolumeAttachmentName("test", "dpu-node", "vol1")).NotTo(
				Equal(generateVolumeAttachmentName("dpu-node", "test", "vol1")))
		})
	})
	Context("generateID", func() {
		It("should produce same ID for same inputs", func() {
			Expect(generateID("test", "foo")).To(
				Equal(generateID("test", "foo")))
		})
		It("should produce different IDs for different inputs", func() {
			Expect(generateID("test", "foo")).NotTo(
				Equal(generateID("test", "bar")))
		})
	})
	Context("getVolumeByID", func() {
		var (
			ctx  context.Context
			vol1 *storagev1.Volume
			vol2 *storagev1.Volume
		)
		BeforeEach(func() {
			ctx = context.Background()
			vol1 = &storagev1.Volume{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-volume1",
					UID:  types.UID("test-uid1"),
				},
			}
			vol2 = &storagev1.Volume{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-volume2",
					UID:  types.UID("test-uid2"),
				},
			}
		})
		It("should return the correct volume", func() {
			c := getDPUClusterClient(vol1, vol2)
			v, err := getVolumeByID(ctx, c, "test-uid1")
			Expect(err).NotTo(HaveOccurred())
			Expect(v).NotTo(BeNil())
			Expect(string(v.GetUID())).To(Equal("test-uid1"))
			Expect(v.GetName()).To(Equal("test-volume1"))
		})
		It("not found", func() {
			vol1.SetUID(types.UID("not-match"))
			c := getDPUClusterClient(vol1, vol2)
			v, err := getVolumeByID(ctx, c, "test-uid1")
			Expect(err).NotTo(HaveOccurred())
			Expect(v).To(BeNil())
		})
	})
	Context("getDPUNameByNodeName", func() {
		var (
			ctx  context.Context
			dpu1 *provisioningv1.DPU
			dpu2 *provisioningv1.DPU
			dpu3 *provisioningv1.DPU
		)
		BeforeEach(func() {
			ctx = context.Background()
			dpu1 = &provisioningv1.DPU{ObjectMeta: metav1.ObjectMeta{Name: "dpu1"}, Spec: provisioningv1.DPUSpec{DPUNodeName: "node1"}}
			dpu2 = &provisioningv1.DPU{ObjectMeta: metav1.ObjectMeta{Name: "dpu2"}, Spec: provisioningv1.DPUSpec{DPUNodeName: "node2"}}
			dpu3 = &provisioningv1.DPU{ObjectMeta: metav1.ObjectMeta{Name: "dpu3"}, Spec: provisioningv1.DPUSpec{DPUNodeName: "node1"}}
		})
		It("should return the correct DPU node name", func() {
			c := getHostClusterClient(dpu1, dpu2, dpu3)
			dpuName, err := getDPUNameByNodeName(ctx, c, "node1")
			Expect(err).NotTo(HaveOccurred())
			Expect(dpuName).To(Equal("dpu1"))
		})
		It("not found", func() {
			c := getHostClusterClient(dpu1, dpu2)
			dpuName, err := getDPUNameByNodeName(ctx, c, "node3")
			Expect(err).NotTo(HaveOccurred())
			Expect(dpuName).To(BeEmpty())
		})
	})
	Context("getVolumeCtx", func() {
		It("should build the proper volumeCtx", func() {
			volume := &storagev1.Volume{
				Spec: storagev1.VolumeSpec{
					StorageParameters: map[string]string{
						"policy": "test",
					},
					VolumeSpecDPU: storagev1.VolumeSpecDPU{
						StorageVendorName:       "VendorA",
						StorageVendorPluginName: "PluginX",
						VolumeAttributes: map[string]string{
							"attribute1": "value1",
							"attribute2": "value2",
						},
					},
				},
			}
			volumeCtx := getVolumeCtx(volume)
			Expect(volumeCtx).To(HaveKeyWithValue("storagePolicyName", "test"))
			Expect(volumeCtx).To(HaveKeyWithValue("storageVendorName", "VendorA"))
			Expect(volumeCtx).To(HaveKeyWithValue("storageVendorPluginName", "PluginX"))
			Expect(volumeCtx).To(HaveKeyWithValue("attribute1", "value1"))
			Expect(volumeCtx).To(HaveKeyWithValue("attribute2", "value2"))
		})
	})
	Context("convertCSIVolumeMode", func() {
		It("typeBlock", func() {
			volCaps := []*csi.VolumeCapability{{AccessType: &csi.VolumeCapability_Block{}}}
			result := convertCSIVolumeMode(volCaps)
			Expect(result).To(Equal(ptr.To(corev1.PersistentVolumeBlock)))
		})
		It("typeFS", func() {
			volCaps := []*csi.VolumeCapability{{AccessType: &csi.VolumeCapability_Mount{}}}
			result := convertCSIVolumeMode(volCaps)
			Expect(result).To(Equal(ptr.To(corev1.PersistentVolumeFilesystem)))
		})
		It("typeBlock by default", func() {
			volCaps := []*csi.VolumeCapability{}
			result := convertCSIVolumeMode(volCaps)
			Expect(result).To(Equal(ptr.To(corev1.PersistentVolumeBlock)))
		})
	})
	Context("convertCSIAccessModesToStorageAPIAccessModes", func() {
		It("should convert MULTI_NODE_MULTI_WRITER to ReadWriteMany", func() {
			volCaps := []*csi.VolumeCapability{{AccessMode: &csi.VolumeCapability_AccessMode{
				Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER,
			}}}
			result := convertCSIAccessModesToStorageAPIAccessModes(volCaps)
			Expect(result).To(ContainElement(corev1.ReadWriteMany))
		})
		It("should convert MULTI_NODE_READER_ONLY to ReadOnlyMany", func() {
			volCaps := []*csi.VolumeCapability{{AccessMode: &csi.VolumeCapability_AccessMode{
				Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
			}}}
			result := convertCSIAccessModesToStorageAPIAccessModes(volCaps)
			Expect(result).To(ContainElement(corev1.ReadOnlyMany))
		})
		It("should convert SINGLE_NODE_WRITER to ReadWriteOnce", func() {
			volCaps := []*csi.VolumeCapability{{AccessMode: &csi.VolumeCapability_AccessMode{
				Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
			}}}
			result := convertCSIAccessModesToStorageAPIAccessModes(volCaps)
			Expect(result).To(ContainElement(corev1.ReadWriteOnce))
		})
		It("should convert SINGLE_NODE_SINGLE_WRITER to ReadWriteOncePod", func() {
			volCaps := []*csi.VolumeCapability{{AccessMode: &csi.VolumeCapability_AccessMode{
				Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER,
			}}}
			result := convertCSIAccessModesToStorageAPIAccessModes(volCaps)
			Expect(result).To(ContainElement(corev1.ReadWriteOncePod))
		})
	})
})
