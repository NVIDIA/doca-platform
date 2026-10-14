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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Storage_Policy", func() {
	BeforeEach(func() {
		By("creating the namespaces")
		snapNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: storage.DefaultNS}}
		Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, snapNS))).To(Succeed())
	})

	AfterEach(func() {
		By("deleting the namespace")
		Expect(k8sClient.Delete(ctx, snapNS)).To(Succeed())
	})

	Context("obj test context", func() {
		ctx := context.Background()

		It("check valid status and destroy", func() {
			By("creating the obj")
			storageClass := createStorageClassObj("low-latency")
			Expect(k8sClient.Create(ctx, storageClass)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, storageClass)

			storageVendor := createStorageVendorObj("excelero", "low-latency", "excelero-plugin")
			Expect(k8sClient.Create(ctx, storageVendor)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, storageVendor)

			storagePolicy := createStoragePolicyObj("excelero", []string{"excelero"})
			Expect(k8sClient.Create(ctx, storagePolicy)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, storagePolicy)

			objFetched := &snapstoragev1.StoragePolicy{}

			By("expecting the Status (Valid)")
			Eventually(func(g Gomega) snapstoragev1.StorageVendorState {
				g.Expect(k8sClient.Get(ctx, getObjKey(storagePolicy.ObjectMeta), objFetched)).To(Succeed())
				return objFetched.Status.State
			}).WithTimeout(10 * time.Second).WithPolling(10 * time.Millisecond).Should(Equal(snapstoragev1.StorageVendorStateValid))
		})

		It("check invalid(empty vendor list) status and destroy", func() {
			By("creating the obj")
			storageClass := createStorageClassObj("low-latency")
			Expect(k8sClient.Create(ctx, storageClass)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, storageClass)

			storageVendor := createStorageVendorObj("excelero", "low-latency", "excelero-plugin")
			Expect(k8sClient.Create(ctx, storageVendor)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, storageVendor)

			storagePolicy := createStoragePolicyObj("excelero", []string{})
			Expect(k8sClient.Create(ctx, storagePolicy)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, storagePolicy)

			objFetched := &snapstoragev1.StoragePolicy{}

			By("expecting the Status (Invalid)")
			Eventually(func(g Gomega) snapstoragev1.StorageVendorState {
				g.Expect(k8sClient.Get(ctx, getObjKey(storagePolicy.ObjectMeta), objFetched)).To(Succeed())
				return objFetched.Status.State
			}).WithTimeout(10 * time.Second).WithPolling(10 * time.Millisecond).Should(Equal(snapstoragev1.StorageVendorStateInvalid))
		})

		It("check invalid status (missing storage class) and destroy", func() {
			By("creating the obj")

			storageVendor := createStorageVendorObj("excelero", "low-latency", "excelero-plugin")
			Expect(k8sClient.Create(ctx, storageVendor)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, storageVendor)

			storagePolicy := createStoragePolicyObj("excelero", []string{"excelero"})
			Expect(k8sClient.Create(ctx, storagePolicy)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, storagePolicy)

			objFetched := &snapstoragev1.StoragePolicy{}

			By("expecting the Status (Invalid)")
			Eventually(func(g Gomega) snapstoragev1.StorageVendorState {
				g.Expect(k8sClient.Get(ctx, getObjKey(storagePolicy.ObjectMeta), objFetched)).To(Succeed())
				return objFetched.Status.State
			}).WithTimeout(10 * time.Second).WithPolling(10 * time.Millisecond).Should(Equal(snapstoragev1.StorageVendorStateInvalid))
		})

		It("check invalid status (missing storage vendor) and destroy", func() {
			By("creating the obj")
			storageClass := createStorageClassObj("low-latency")
			Expect(k8sClient.Create(ctx, storageClass)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, storageClass)

			storagePolicy := createStoragePolicyObj("excelero", []string{"excelero"})
			Expect(k8sClient.Create(ctx, storagePolicy)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, storagePolicy)

			objFetched := &snapstoragev1.StoragePolicy{}

			By("expecting the Status (Invalid)")
			Eventually(func(g Gomega) snapstoragev1.StorageVendorState {
				g.Expect(k8sClient.Get(ctx, getObjKey(storagePolicy.ObjectMeta), objFetched)).To(Succeed())
				return objFetched.Status.State
			}).WithTimeout(10 * time.Second).WithPolling(10 * time.Millisecond).Should(Equal(snapstoragev1.StorageVendorStateInvalid))
		})

		It("check status change from valid to invaild(delete storage class)", func() {
			By("creating the obj")
			storageClass := createStorageClassObj("low-latency")
			Expect(k8sClient.Create(ctx, storageClass)).To(Succeed())

			storageVendor := createStorageVendorObj("excelero", "low-latency", "excelero-plugin")
			Expect(k8sClient.Create(ctx, storageVendor)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, storageVendor)

			storagePolicy := createStoragePolicyObj("excelero", []string{"excelero"})
			Expect(k8sClient.Create(ctx, storagePolicy)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, storagePolicy)

			objFetched := &snapstoragev1.StoragePolicy{}

			By("expecting the Status (Vaild)")
			Eventually(func(g Gomega) snapstoragev1.StorageVendorState {
				g.Expect(k8sClient.Get(ctx, getObjKey(storagePolicy.ObjectMeta), objFetched)).To(Succeed())
				return objFetched.Status.State
			}).WithTimeout(10 * time.Second).WithPolling(10 * time.Millisecond).Should(Equal(snapstoragev1.StorageVendorStateValid))

			By("deleting storage class")
			Expect(k8sClient.Delete(ctx, storageClass)).To(Succeed())

			By("expecting the Status (Invaild)")
			Eventually(func(g Gomega) snapstoragev1.StorageVendorState {
				g.Expect(k8sClient.Get(ctx, getObjKey(storagePolicy.ObjectMeta), objFetched)).To(Succeed())
				return objFetched.Status.State
			}).WithTimeout(10 * time.Second).WithPolling(10 * time.Millisecond).Should(Equal(snapstoragev1.StorageVendorStateInvalid))
		})

		It("check status change from valid to invaild(delete storage vendor)", func() {
			By("creating the obj")
			storageClass := createStorageClassObj("low-latency")
			Expect(k8sClient.Create(ctx, storageClass)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, storageClass)

			storageVendor := createStorageVendorObj("excelero", "low-latency", "excelero-plugin")
			Expect(k8sClient.Create(ctx, storageVendor)).To(Succeed())

			storagePolicy := createStoragePolicyObj("excelero", []string{"excelero"})
			Expect(k8sClient.Create(ctx, storagePolicy)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, storagePolicy)

			objFetched := &snapstoragev1.StoragePolicy{}

			By("expecting the Status (Vaild)")
			Eventually(func(g Gomega) snapstoragev1.StorageVendorState {
				g.Expect(k8sClient.Get(ctx, getObjKey(storagePolicy.ObjectMeta), objFetched)).To(Succeed())
				return objFetched.Status.State
			}).WithTimeout(10 * time.Second).WithPolling(10 * time.Millisecond).Should(Equal(snapstoragev1.StorageVendorStateValid))

			By("deleting storage vendor")
			Expect(k8sClient.Delete(ctx, storageVendor)).To(Succeed())

			By("expecting the Status (Invaild)")
			Eventually(func(g Gomega) snapstoragev1.StorageVendorState {
				g.Expect(k8sClient.Get(ctx, getObjKey(storagePolicy.ObjectMeta), objFetched)).To(Succeed())
				return objFetched.Status.State
			}).WithTimeout(10 * time.Second).WithPolling(10 * time.Millisecond).Should(Equal(snapstoragev1.StorageVendorStateInvalid))
		})

		It("check status change from invaild to vaild(create storage class)", func() {
			By("creating the obj")
			storageVendor := createStorageVendorObj("excelero", "low-latency", "excelero-plugin")
			Expect(k8sClient.Create(ctx, storageVendor)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, storageVendor)

			storagePolicy := createStoragePolicyObj("excelero", []string{"excelero"})
			Expect(k8sClient.Create(ctx, storagePolicy)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, storagePolicy)

			objFetched := &snapstoragev1.StoragePolicy{}

			By("expecting the Status (Invaild)")
			Eventually(func(g Gomega) snapstoragev1.StorageVendorState {
				g.Expect(k8sClient.Get(ctx, getObjKey(storagePolicy.ObjectMeta), objFetched)).To(Succeed())
				return objFetched.Status.State
			}).WithTimeout(10 * time.Second).WithPolling(10 * time.Millisecond).Should(Equal(snapstoragev1.StorageVendorStateInvalid))

			By("creating storage class")
			storageClass := createStorageClassObj("low-latency")
			Expect(k8sClient.Create(ctx, storageClass)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, storageClass)

			By("expecting the Status (Vaild)")
			Eventually(func(g Gomega) snapstoragev1.StorageVendorState {
				g.Expect(k8sClient.Get(ctx, getObjKey(storagePolicy.ObjectMeta), objFetched)).To(Succeed())
				return objFetched.Status.State
			}).WithTimeout(10 * time.Second).WithPolling(10 * time.Millisecond).Should(Equal(snapstoragev1.StorageVendorStateValid))
		})

		It("check status change from invaild to vaild(create storage vendor)", func() {
			By("creating the obj")
			storageClass := createStorageClassObj("low-latency")
			Expect(k8sClient.Create(ctx, storageClass)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, storageClass)

			storagePolicy := createStoragePolicyObj("excelero", []string{"excelero"})
			Expect(k8sClient.Create(ctx, storagePolicy)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, storagePolicy)

			objFetched := &snapstoragev1.StoragePolicy{}

			By("expecting the Status (Invaild)")
			Eventually(func(g Gomega) snapstoragev1.StorageVendorState {
				g.Expect(k8sClient.Get(ctx, getObjKey(storagePolicy.ObjectMeta), objFetched)).To(Succeed())
				return objFetched.Status.State
			}).WithTimeout(10 * time.Second).WithPolling(10 * time.Millisecond).Should(Equal(snapstoragev1.StorageVendorStateInvalid))

			By("creating storage vendor")
			storageVendor := createStorageVendorObj("excelero", "low-latency", "excelero-plugin")
			Expect(k8sClient.Create(ctx, storageVendor)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, storageVendor)

			By("expecting the Status (Vaild)")
			Eventually(func(g Gomega) snapstoragev1.StorageVendorState {
				g.Expect(k8sClient.Get(ctx, getObjKey(storagePolicy.ObjectMeta), objFetched)).To(Succeed())
				return objFetched.Status.State
			}).WithTimeout(10 * time.Second).WithPolling(10 * time.Millisecond).Should(Equal(snapstoragev1.StorageVendorStateValid))
		})
	})
})
