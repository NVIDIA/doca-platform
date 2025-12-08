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

package webhooks

import (
	"context"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

var _ = Describe("BFB", func() {

	const (
		DefaultObjName = "obj-bfb"
		DefaultURL     = "http://example.com/dummy.bfb"
	)

	var getObjKey = func(obj *provisioningv1.BFB) types.NamespacedName {
		return types.NamespacedName{
			Name:      obj.Name,
			Namespace: obj.Namespace,
		}
	}

	var createObj = func(name string) *provisioningv1.BFB {
		return &provisioningv1.BFB{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "default",
			},
			Spec:   provisioningv1.BFBSpec{},
			Status: provisioningv1.BFBStatus{},
		}
	}

	BeforeEach(func() {
		// Add any setup steps that needs to be executed before each test
	})

	AfterEach(func() {
		// Add any teardown steps that needs to be executed after each test
	})

	Context("obj test context", func() {
		ctx := context.Background()

		It("create and get object", func() {
			obj := createObj(DefaultObjName)
			obj.Spec.URL = DefaultURL
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.BFB{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched).To(Equal(obj))
		})

		It("delete object", func() {
			obj := createObj(DefaultObjName)
			obj.Spec.URL = DefaultURL
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Delete(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, getObjKey(obj), obj)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		It("update object", func() {
			obj := createObj(DefaultObjName)
			obj.Spec.URL = DefaultURL
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			err = k8sClient.Update(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			objFetched := &provisioningv1.BFB{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched).To(Equal(obj))
		})

		It("spec.url is mandatory", func() {
			obj := createObj(DefaultObjName)
			err := k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
		})

		It("spec.url validation", func() {
			obj := createObj("obj-0")
			obj.Spec.URL = "http://"
			err := k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())

			obj = createObj("obj-1")
			obj.Spec.URL = "http://8.8.8.8/dummy.bfb"
			err = k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			obj = createObj("obj-2")
			obj.Spec.URL = "https://example.com/dummy.bfb"
			err = k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			obj = createObj("obj-3")
			obj.Spec.URL = "https://8.8.8.8/dummy.bfb"
			err = k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			obj = createObj("obj-4")
			obj.Spec.URL = "example.com/dummy.bfb"
			err = k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
		})

		It("spec.url is immutable", func() {
			refValue := DefaultURL

			obj := createObj(DefaultObjName)
			obj.Spec.URL = refValue
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			obj.Spec.URL = "http://example.com/dummy_clone.bfb"
			err = k8sClient.Update(ctx, obj)
			Expect(err).To(HaveOccurred())

			objFetched := &provisioningv1.BFB{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched.Spec.URL).To(Equal(refValue))
		})

		It("spec.fileName is validation", func() {
			obj := createObj(DefaultObjName)
			obj.Spec.FileName = ptr.To("dummy_NAME-1.2.3.bfb")
			obj.Spec.URL = DefaultURL
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			obj = createObj(DefaultObjName)
			obj.Spec.FileName = ptr.To("dummy.tar")
			obj.Spec.URL = DefaultURL
			err = k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())

			obj = createObj(DefaultObjName)
			obj.Spec.FileName = ptr.To(" dummy.bfb")
			obj.Spec.URL = DefaultURL
			err = k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())

			obj = createObj(DefaultObjName)
			obj.Spec.FileName = ptr.To("/dummy.bfb")
			obj.Spec.URL = DefaultURL
			err = k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())

			obj = createObj(DefaultObjName)
			obj.Spec.FileName = ptr.To("dummy with spaces.bfb")
			obj.Spec.URL = DefaultURL
			err = k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
		})

		It("spec.fileName is immutable", func() {
			refValue := "dummy.bfb"

			obj := createObj(DefaultObjName)
			obj.Spec.FileName = ptr.To(refValue)
			obj.Spec.URL = DefaultURL
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			obj.Spec.FileName = ptr.To("dummy_clone.bfb")
			err = k8sClient.Update(ctx, obj)
			Expect(err).To(HaveOccurred())

			objFetched := &provisioningv1.BFB{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(*objFetched.Spec.FileName).To(Equal(refValue))
		})

		It("status.phase default", func() {
			obj := createObj(DefaultObjName)
			obj.Spec.URL = DefaultURL
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			Expect(obj.Status.Phase).To(BeEquivalentTo(provisioningv1.BFBInitializing))
			DeferCleanup(k8sClient.Delete, ctx, obj)

			objFetched := &provisioningv1.BFB{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched.Status.Phase).To(BeEquivalentTo(provisioningv1.BFBInitializing))
		})

		It("spec.fileName must be globally unique", func() {
			fileName := "unique_test_file.bfb"

			// Create the first BFB with this fileName
			obj1 := createObj("bfb-unique-1")
			obj1.Spec.FileName = ptr.To(fileName)
			obj1.Spec.URL = DefaultURL
			By("creating the first BFB with fileName")
			err := k8sClient.Create(ctx, obj1)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(k8sClient.Delete, ctx, obj1)

			// Attempt to create a second BFB with the same fileName
			obj2 := createObj("bfb-unique-2")
			obj2.Spec.FileName = ptr.To(fileName)
			obj2.Spec.URL = DefaultURL
			By("creating the second BFB with the same fileName")
			err = k8sClient.Create(ctx, obj2)
			Expect(err).To(HaveOccurred())

			// Create a third BFB with a different fileName should succeed
			obj3 := createObj("bfb-unique-3")
			obj3.Spec.FileName = ptr.To("another_unique_file.bfb")
			obj3.Spec.URL = DefaultURL
			By("creating a third BFB with a different fileName")
			err = k8sClient.Create(ctx, obj3)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(k8sClient.Delete, ctx, obj3)
		})

		It("spec.fileName uniqueness is enforced on update", func() {
			fileName1 := "update_unique_1.bfb"
			fileName2 := "update_unique_2.bfb"

			// Create the first BFB with fileName1
			obj1 := createObj("bfb-update-1")
			obj1.Spec.FileName = ptr.To(fileName1)
			obj1.Spec.URL = DefaultURL
			err := k8sClient.Create(ctx, obj1)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(k8sClient.Delete, ctx, obj1)

			// Create the second BFB with fileName2
			obj2 := createObj("bfb-update-2")
			obj2.Spec.FileName = ptr.To(fileName2)
			obj2.Spec.URL = DefaultURL
			err = k8sClient.Create(ctx, obj2)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(k8sClient.Delete, ctx, obj2)

			// Simulate an update: try to change obj2's fileName to fileName1 (should fail uniqueness)
			obj2.Spec.FileName = ptr.To(fileName1)
			err = k8sClient.Update(ctx, obj2)
			Expect(err).To(HaveOccurred())
		})

		It("should allow deletion of BFB not referenced by any DPU or DPUSet", func() {
			obj := createObj("bfb-unreferenced")
			obj.Spec.URL = DefaultURL
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Delete(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should block deletion of BFB referenced by a DPU", func() {
			bfbName := "bfb-referenced-by-dpu"
			obj := createObj(bfbName)
			obj.Spec.URL = DefaultURL
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dpu-1",
					Namespace: "default",
				},
				Spec: provisioningv1.DPUSpec{
					BFB:           bfbName,
					DPUDeviceName: "dpudevice-1",
					SerialNumber:  "test-serial-123",
				},
			}
			err = k8sClient.Create(ctx, dpu)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(k8sClient.Delete, ctx, dpu)

			err = k8sClient.Delete(ctx, obj)
			Expect(apierrors.IsInvalid(err) || apierrors.IsForbidden(err)).To(BeTrue())
		})

		It("should block deletion of BFB referenced by a DPUSet", func() {
			bfbName := "bfb-referenced-by-dpuset"
			obj := createObj(bfbName)
			obj.Spec.URL = DefaultURL
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(k8sClient.Delete, ctx, obj)

			dpuSet := &provisioningv1.DPUSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dpuset-1",
					Namespace: "default",
				},
				Spec: provisioningv1.DPUSetSpec{
					DPUTemplate: provisioningv1.DPUTemplate{
						Spec: provisioningv1.DPUTemplateSpec{
							BFB: provisioningv1.BFBReference{
								Name: bfbName,
							},
							DPUFlavor: "dummy-flavor",
						},
					},
				},
			}
			err = k8sClient.Create(ctx, dpuSet)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(k8sClient.Delete, ctx, dpuSet)

			err = k8sClient.Delete(ctx, obj)
			Expect(apierrors.IsInvalid(err) || apierrors.IsForbidden(err)).To(BeTrue())
		})
	})

	// Unit tests - direct webhook method calls
	Context("webhook unit tests", func() {
		ctx := context.Background()

		It("ValidateDelete should return nil when no DPUSet references BFB", func() {
			webhook := &BFB{}
			warnings, err := webhook.ValidateDelete(ctx, &provisioningv1.BFB{ObjectMeta: metav1.ObjectMeta{Name: "non-existent-bfb", Namespace: "default"}})
			Expect(warnings).To(BeNil())
			Expect(err).ToNot(HaveOccurred())
		})

		// Tests for type assertion error handling (!ok branches)
		It("ValidateCreate should return error for invalid object type", func() {
			webhook := &BFB{}
			_, err := webhook.ValidateCreate(ctx, &provisioningv1.DPU{}) // Wrong type
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid object type"))
		})

		It("ValidateUpdate should return error for invalid object type", func() {
			webhook := &BFB{}
			_, err := webhook.ValidateUpdate(ctx, &provisioningv1.BFB{}, &provisioningv1.DPU{}) // Wrong newObj type
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid new object type"))
		})

		It("ValidateDelete should return error for invalid object type", func() {
			webhook := &BFB{}
			_, err := webhook.ValidateDelete(ctx, &provisioningv1.DPU{}) // Wrong type
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid object type"))
		})
	})
})
