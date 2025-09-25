/*
Copyright 2024 NVIDIA

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

package controllers

import (
	"context"
	"time"

	snapstoragev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/internal/storage"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("NV_Volume_Attachment", func() {
	BeforeEach(func() {
		By("creating the namespaces")
		snapNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: storage.DefaultNS}}
		tenantNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: TenantNSName}}
		Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, snapNS))).To(Succeed())
		Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, tenantNS))).To(Succeed())
	})

	AfterEach(func() {
		By("deleting the namespace")
		Expect(k8sClient.Delete(ctx, snapNS)).To(Succeed())
		Expect(k8sClient.Delete(ctx, tenantNS)).To(Succeed())
	})

	Context("obj test context", func() {
		ctx := context.Background()

		It("check storageAttached state", func() {
			By("creating the obj")
			csiDriver := createCSIDrivertObj(Provisioner)
			Expect(k8sClient.Create(ctx, csiDriver)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, csiDriver)

			storageClass := createStorageClassObj("low-latency")
			Expect(k8sClient.Create(ctx, storageClass)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, storageClass)

			storageVendor := createStorageVendorObj("excelero", "low-latency", "excelero-plugin")
			Expect(k8sClient.Create(ctx, storageVendor)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, storageVendor)

			storagePolicy := createStoragePolicyObj("excelero", []string{"excelero"})
			Expect(k8sClient.Create(ctx, storagePolicy)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, storagePolicy)

			request, _ := resource.ParseQuantity("5Gi")
			nvVolume := createNVVolumeObj("nv-volume", "excelero", request)
			Expect(k8sClient.Create(ctx, nvVolume)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, nvVolume)

			nvVolumeAttachment := createNVVolumeAttachmentObj("nv-volume-attachment", "nv-volume")
			Expect(k8sClient.Create(ctx, nvVolumeAttachment)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, nvVolumeAttachment)

			objFetched := &snapstoragev1.VolumeAttachment{}

			By("expecting finalizer added and InProgress state")
			Eventually(func(g Gomega) bool {
				g.Expect(k8sClient.Get(ctx, getObjKey(nvVolumeAttachment.ObjectMeta), objFetched)).To(Succeed())
				return objFetched.Status.StorageAttached
			}).WithTimeout(10 * time.Second).WithPolling(10 * time.Millisecond).Should(BeTrue())
		})
	})
})

// TODO: add more tests
