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

package indexers

import (
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Indexers", func() {
	It("should index DPU by spec.dpuNodeName", func() {
		dpuNodeName := "test-dpu-node"
		dpu := &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{Name: "test-dpu", Namespace: "default"},
			Spec: provisioningv1.DPUSpec{
				DPUNodeName:   dpuNodeName,
				SerialNumber:  "MT25066004C7",
				DPUDeviceName: "test-device",
				BFB:           "test-bfb",
				DPUFlavor:     "test-flavor",
			},
		}
		Expect(testClient.Create(ctx, dpu)).To(Succeed())
		Eventually(func(g Gomega) {
			var dpuList provisioningv1.DPUList
			g.Expect(testClient.List(ctx, &dpuList,
				client.MatchingFields{DPUSpecDPUNodeName: dpuNodeName})).To(Succeed())
			g.Expect(dpuList.Items).To(HaveLen(1))
			g.Expect(dpuList.Items[0].Name).To(Equal(dpu.Name))
		}).Should(Succeed())
		Expect(testClient.Delete(ctx, dpu)).To(Succeed())
	})
	It("should index DPUVolumeAttachment by spec.dpuVolumeName", func() {
		dpuVolumeName := "test-dpu-volume"
		attachment := &storagev1.DPUVolumeAttachment{
			ObjectMeta: metav1.ObjectMeta{Name: "test-attachment", Namespace: "default"},
			Spec: storagev1.DPUVolumeAttachmentSpec{
				DPUVolumeName:      dpuVolumeName,
				DPUNodeName:        "test-node",
				FunctionTypeConfig: storagev1.FunctionTypeConfig{FunctionType: "vf", HotplugFunction: false},
			},
		}
		Expect(testClient.Create(ctx, attachment)).To(Succeed())
		Eventually(func(g Gomega) {
			var list storagev1.DPUVolumeAttachmentList
			g.Expect(testClient.List(ctx, &list,
				client.MatchingFields{DPUVolumeAttachmentSpecDPUVolumeName: dpuVolumeName})).To(Succeed())
			g.Expect(list.Items).To(HaveLen(1))
			g.Expect(list.Items[0].Name).To(Equal(attachment.Name))
		}).Should(Succeed())
		Expect(testClient.Delete(ctx, attachment)).To(Succeed())
	})
	It("should index DPUVolumeAttachment by spec.dpuNodeName", func() {
		dpuNodeName := "test-attachment-node"
		attachment := &storagev1.DPUVolumeAttachment{
			ObjectMeta: metav1.ObjectMeta{Name: "test-attachment-2", Namespace: "default"},
			Spec: storagev1.DPUVolumeAttachmentSpec{
				DPUVolumeName:      "test-vol",
				DPUNodeName:        dpuNodeName,
				FunctionTypeConfig: storagev1.FunctionTypeConfig{FunctionType: "vf", HotplugFunction: false},
			},
		}
		Expect(testClient.Create(ctx, attachment)).To(Succeed())
		Eventually(func(g Gomega) {
			var list storagev1.DPUVolumeAttachmentList
			g.Expect(testClient.List(ctx, &list,
				client.MatchingFields{DPUVolumeAttachmentSpecDPUNodeName: dpuNodeName})).To(Succeed())
			g.Expect(list.Items).To(HaveLen(1))
			g.Expect(list.Items[0].Name).To(Equal(attachment.Name))
		}).Should(Succeed())
		Expect(testClient.Delete(ctx, attachment)).To(Succeed())
	})
	It("should index DPUStorageVendor by spec.storageClassName", func() {
		storageClassName := "test-storage-class"
		vendor := &storagev1.DPUStorageVendor{
			ObjectMeta: metav1.ObjectMeta{Name: "test-vendor", Namespace: "default"},
			Spec: storagev1.DPUStorageVendorSpec{
				StorageClassName: storageClassName,
				PluginName:       "test-plugin",
			},
		}
		Expect(testClient.Create(ctx, vendor)).To(Succeed())
		Eventually(func(g Gomega) {
			var list storagev1.DPUStorageVendorList
			g.Expect(testClient.List(ctx, &list,
				client.MatchingFields{DPUStorageVendorSpecStorageClassName: storageClassName})).To(Succeed())
			g.Expect(list.Items).To(HaveLen(1))
			g.Expect(list.Items[0].Name).To(Equal(vendor.Name))
		}).Should(Succeed())
		Expect(testClient.Delete(ctx, vendor)).To(Succeed())
	})
	It("should index DPUStoragePolicy by spec.dpuStorageVendors", func() {
		vendorName := "test-policy-vendor"
		policy := &storagev1.DPUStoragePolicy{
			ObjectMeta: metav1.ObjectMeta{Name: "test-policy", Namespace: "default"},
			Spec:       storagev1.DPUStoragePolicySpec{DPUStorageVendors: []string{vendorName, "vendor2"}},
		}
		Expect(testClient.Create(ctx, policy)).To(Succeed())
		Eventually(func(g Gomega) {
			var list storagev1.DPUStoragePolicyList
			g.Expect(testClient.List(ctx, &list,
				client.MatchingFields{DPUStoragePolicySpecDPUStorageVendors: vendorName})).To(Succeed())
			g.Expect(list.Items).To(HaveLen(1))
			g.Expect(list.Items[0].Name).To(Equal(policy.Name))
		}).Should(Succeed())
		Expect(testClient.Delete(ctx, policy)).To(Succeed())
	})
	It("should index DPUVolume by spec.dpuStoragePolicyName", func() {
		policyName := "test-dpu-policy"
		volume := &storagev1.DPUVolume{
			ObjectMeta: metav1.ObjectMeta{Name: "test-dpu-volume", Namespace: "default"},
			Spec: storagev1.DPUVolumeSpec{
				DPUStoragePolicyName: policyName,
				AccessModes:          []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")}},
			},
		}
		Expect(testClient.Create(ctx, volume)).To(Succeed())
		Eventually(func(g Gomega) {
			var list storagev1.DPUVolumeList
			g.Expect(testClient.List(ctx, &list,
				client.MatchingFields{DPUVolumeSpecDPUStoragePolicyName: policyName})).To(Succeed())
			g.Expect(list.Items).To(HaveLen(1))
			g.Expect(list.Items[0].Name).To(Equal(volume.Name))
		}).Should(Succeed())
		Expect(testClient.Delete(ctx, volume)).To(Succeed())
	})
	It("should index DPUVolume by status.state.selectedDPUStorageVendorName", func() {
		vendorName := "test-selected-vendor"
		volume := &storagev1.DPUVolume{
			ObjectMeta: metav1.ObjectMeta{Name: "test-dpu-volume-2", Namespace: "default"},
			Spec: storagev1.DPUVolumeSpec{
				DPUStoragePolicyName: "policy",
				AccessModes:          []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")}},
			},
		}
		Expect(testClient.Create(ctx, volume)).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(volume), volume)).To(Succeed())
			volume.Status.State = &storagev1.DPUVolumeState{SelectedDPUStorageVendorName: &vendorName}
			g.Expect(testClient.Status().Update(ctx, volume)).To(Succeed())
		}).Should(Succeed())
		Eventually(func(g Gomega) {
			var list storagev1.DPUVolumeList
			g.Expect(testClient.List(ctx, &list, client.MatchingFields{
				DPUVolumeStatusStateSelectedDPUStorageVendorName: vendorName})).To(Succeed())
			g.Expect(list.Items).To(HaveLen(1))
			g.Expect(list.Items[0].Name).To(Equal(volume.Name))
		}).Should(Succeed())
		Expect(testClient.Delete(ctx, volume)).To(Succeed())
	})
	It("should index DPUVolume by status.state.volumeInfo.volumeName", func() {
		pvName := "test-pv-name"
		volume := &storagev1.DPUVolume{
			ObjectMeta: metav1.ObjectMeta{Name: "test-dpu-volume-3", Namespace: "default"},
			Spec: storagev1.DPUVolumeSpec{
				DPUStoragePolicyName: "policy",
				AccessModes:          []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")}},
			},
		}
		Expect(testClient.Create(ctx, volume)).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(volume), volume)).To(Succeed())
			volume.Status.State = &storagev1.DPUVolumeState{VolumeInfo: &storagev1.VolumeInfo{VolumeName: &pvName}}
			g.Expect(testClient.Status().Update(ctx, volume)).To(Succeed())
		}).Should(Succeed())
		Eventually(func(g Gomega) {
			var list storagev1.DPUVolumeList
			g.Expect(testClient.List(ctx, &list, client.MatchingFields{
				DPUVolumeStatusStateVolumeInfoVolumeName: pvName})).To(Succeed())
			g.Expect(list.Items).To(HaveLen(1))
			g.Expect(list.Items[0].Name).To(Equal(volume.Name))
		}).Should(Succeed())
		Expect(testClient.Delete(ctx, volume)).To(Succeed())
	})
})
