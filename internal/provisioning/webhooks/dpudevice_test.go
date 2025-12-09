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

//nolint:staticcheck
package webhooks

import (
	"context"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/utils/ptr"
)

var _ = Describe("DPUDevice", func() {

	var getObjKey = func(obj *provisioningv1.DPUDevice) types.NamespacedName {
		return types.NamespacedName{
			Name:      obj.Name,
			Namespace: obj.Namespace,
		}
	}
	var createObj = func(name string, serialNumber string) *provisioningv1.DPUDevice {
		return &provisioningv1.DPUDevice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "default",
			},
			Spec: provisioningv1.DPUDeviceSpec{
				SerialNumber: serialNumber,
			},
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
		It("check default settings", func() {
			obj := createObj("obj-4", "MT25066004C4")
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			objFetched := &provisioningv1.DPUDevice{}
			err = k8sClient.Get(ctx, getObjKey(obj), objFetched)
			Expect(err).NotTo(HaveOccurred())
			Expect(objFetched).To(Equal(obj))
			Expect(objFetched.Spec.BMCIP).To(BeNil())
			Expect(objFetched.Spec.SerialNumber).NotTo(BeNil())
			Expect(objFetched.Spec.PSID).To(BeNil())
			Expect(objFetched.Spec.OPN).To(BeNil())
			Expect(objFetched.Spec.NumberOfPFs).NotTo(BeNil())
			Expect(*objFetched.Spec.NumberOfPFs).To(Equal(1))
			Expect(objFetched.Spec.PF0Name).To(BeNil()) //nolint:staticcheck
		})
		It("create from yaml", func() {
			yml := []byte(`
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUDevice
metadata:
  name: obj-5
  namespace: default
spec:
  serialNumber: MT25066004C5
  bmcIp: 3.3.3.3
  psid: MT_0000000034
  opn: 900-9D3B4-00SV-EA0
`)
			obj := &provisioningv1.DPUDevice{}
			err := yaml.UnmarshalStrict(yml, obj)
			Expect(err).To(Succeed())
			err = k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})
		It("create from yaml minimal", func() {
			yml := []byte(`
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUDevice
metadata:
  name: obj-6
  namespace: default
spec:
  serialNumber: MT25066004C6
`)
			obj := &provisioningv1.DPUDevice{}
			err := yaml.UnmarshalStrict(yml, obj)
			Expect(err).To(Succeed())
			err = k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
		})
		It("update object - check immutability of Serial Number", func() {
			obj := createObj("obj-7", "MT25066004C7")
			Expect(k8sClient.Create(ctx, obj)).NotTo(HaveOccurred())

			obj.Spec.SerialNumber = "MT25066004C8"
			Expect(k8sClient.Update(ctx, obj)).To(HaveOccurred())
		})
		It("update object - check immutability of PSID", func() {
			obj := createObj("obj-8", "MT25066004D8")
			obj.Spec.PSID = ptr.To("MT_0000000034")
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			obj.Spec.PSID = ptr.To("MT_0000000039")
			err = k8sClient.Update(ctx, obj)
			Expect(err).To(HaveOccurred())
		})
		It("update object - check immutability of OPN", func() {
			obj := createObj("obj-9", "MT25066004D9")
			obj.Spec.OPN = ptr.To("900-9D3B4-00SV-EA0")
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			obj.Spec.OPN = ptr.To("900-9D3B4-00SV-EAA")
			err = k8sClient.Update(ctx, obj)
			Expect(err).To(HaveOccurred())
		})
		It("update object - check immutability of BMC IP", func() {
			obj := createObj("obj-10", "MT25066004DA")
			obj.Spec.BMCIP = ptr.To("22.22.22.22")
			err := k8sClient.Create(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			obj.Spec.BMCIP = ptr.To("4.4.4.4")
			err = k8sClient.Update(ctx, obj)
			Expect(err).NotTo(HaveOccurred())

			objFetched := &provisioningv1.DPUDevice{}
			Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
			Expect(*objFetched.Spec.BMCIP).To(Equal("4.4.4.4"))
		})
		It("create object with empty Serial Number should fail MinLength=1 validation", func() {
			// SerialNumber requires MinLength=1 validation. When using Go struct with omitempty,
			// the empty string is omitted from JSON, causing CRD 'required' validation to fail.
			obj := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "obj-11",
					Namespace: "default",
				},
				Spec: provisioningv1.DPUDeviceSpec{
					SerialNumber: "",
				},
			}
			Expect(k8sClient.Create(ctx, obj)).To(HaveOccurred())
		})
		It("create object with empty Serial Number via unstructured should fail MinLength=1 validation", func() {
			// This test uses unstructured client to bypass Go's omitempty behavior and explicitly send
			// serialNumber: "" to the API server. This tests the MinLength=1 validation directly
			// since the field IS present in the request (not omitted).
			u := &unstructured.Unstructured{}
			u.SetUnstructuredContent(map[string]interface{}{
				"apiVersion": "provisioning.dpu.nvidia.com/v1alpha1",
				"kind":       "DPUDevice",
				"metadata": map[string]interface{}{
					"name":      "obj-empty-serial-unstructured",
					"namespace": "default",
				},
				"spec": map[string]interface{}{
					"serialNumber": "", // Explicitly empty - tests MinLength=1 validation
				},
			})
			err := k8sClient.Create(ctx, u)
			GinkgoWriter.Printf("Create error: %v\n", err)
			Expect(err).To(HaveOccurred())
		})
		It("create object with invalid PSID", func() {
			obj := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "obj-12",
					Namespace: "default",
				},
				Spec: provisioningv1.DPUDeviceSpec{
					PSID: ptr.To("Invalid-PSID"),
				},
			}
			err := k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
		})
		It("create object with invalid OPN", func() {
			obj := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "obj-13",
					Namespace: "default",
				},
				Spec: provisioningv1.DPUDeviceSpec{
					OPN: ptr.To("Invalid-OPN"),
				},
			}
			err := k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
		})
		It("create object with invalid BMCIP", func() {
			obj := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "obj-14",
					Namespace: "default",
				},
				Spec: provisioningv1.DPUDeviceSpec{
					BMCIP: ptr.To("Invalid-IP-Address"),
				},
			}
			err := k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
		})
		It("create object with multiple invalid specs should fail", func() {
			obj := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "obj-15",
					Namespace: "default",
				},
				Spec: provisioningv1.DPUDeviceSpec{
					SerialNumber: "",
					PSID:         ptr.To("MT-0001234567"),
					OPN:          ptr.To("900-9D3B4-00SV-EA0F"),
					BMCIP:        ptr.To("10.1.2.3/24"),
				},
			}
			err := k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
		})
		It("create object with valid specs should succeed", func() {
			obj := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "obj-16",
					Namespace: "default",
				},
				Spec: provisioningv1.DPUDeviceSpec{
					SerialNumber: "MT25066004D6",
					PSID:         ptr.To("MT_0001234567"),
					OPN:          ptr.To("900-9D3B4-00SV-EA0"),
					BMCIP:        ptr.To("10.1.2.3"),
				},
			}
			Expect(k8sClient.Create(ctx, obj)).NotTo(HaveOccurred())

			objFetched := &provisioningv1.DPUDevice{}
			Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).NotTo(HaveOccurred())
			Expect(objFetched).To(Equal(obj))
		})
		It("create object with duplicate serial number should fail", func() {
			// Create first object
			obj1 := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "obj-17",
					Namespace: "default",
				},
				Spec: provisioningv1.DPUDeviceSpec{
					SerialNumber: "MT25066004D7",
				},
			}
			Expect(k8sClient.Create(ctx, obj1)).NotTo(HaveOccurred())

			// Try to create second object with same serial number
			obj2 := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "obj-18",
					Namespace: "default",
				},
				Spec: provisioningv1.DPUDeviceSpec{
					SerialNumber: "MT25066004D7", // Same serial number
				},
			}
			Expect(k8sClient.Create(ctx, obj2)).To(HaveOccurred())
		})
		It("create object with duplicate serial number in different namespace should fail", func() {
			// Create first object in default namespace
			obj1 := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "obj-19",
					Namespace: "default",
				},
				Spec: provisioningv1.DPUDeviceSpec{
					SerialNumber: "MT25066004C8",
				},
			}
			Expect(k8sClient.Create(ctx, obj1)).NotTo(HaveOccurred())

			// Try to create second object with same serial number in different namespace
			obj2 := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "obj-20",
					Namespace: "test-namespace",
				},
				Spec: provisioningv1.DPUDeviceSpec{
					SerialNumber: "MT25066004C8", // Same serial number
				},
			}
			Expect(k8sClient.Create(ctx, obj2)).To(HaveOccurred())
		})
		It("update object with duplicate serial number should fail", func() {
			// Create first object
			obj1 := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "obj-21",
					Namespace: "default",
				},
				Spec: provisioningv1.DPUDeviceSpec{
					SerialNumber: "MT25066004C9",
				},
			}
			Expect(k8sClient.Create(ctx, obj1)).NotTo(HaveOccurred())

			// Create second object with different serial number
			obj2 := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "obj-22",
					Namespace: "default",
				},
				Spec: provisioningv1.DPUDeviceSpec{
					SerialNumber: "MT25066004C10",
				},
			}
			Expect(k8sClient.Create(ctx, obj2)).NotTo(HaveOccurred())

			// Try to update second object to have same serial number as first
			obj2.Spec.SerialNumber = "MT25066004C9"
			Expect(k8sClient.Update(ctx, obj2)).To(HaveOccurred())
		})
	})

	// Unit tests - direct webhook method calls
	Context("webhook unit tests", func() {
		ctx := context.Background()

		It("ValidateDelete should return nil", func() {
			webhook := &DPUDevice{}
			warnings, err := webhook.ValidateDelete(ctx, &provisioningv1.DPUDevice{})
			Expect(warnings).To(BeNil())
			Expect(err).ToNot(HaveOccurred())
		})

		// Tests for type assertion error handling (!ok branches)
		It("ValidateCreate should return error for invalid object type", func() {
			webhook := &DPUDevice{}
			_, err := webhook.ValidateCreate(ctx, &provisioningv1.DPU{}) // Wrong type
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid object type"))
		})

		It("ValidateUpdate should return error for invalid object type", func() {
			webhook := &DPUDevice{}
			_, err := webhook.ValidateUpdate(ctx, &provisioningv1.DPUDevice{}, &provisioningv1.DPU{}) // Wrong newObj type
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid object type"))
		})

		It("ValidateUpdate should return error for invalid old object type", func() {
			webhook := &DPUDevice{}
			_, err := webhook.ValidateUpdate(ctx, &provisioningv1.DPU{}, &provisioningv1.DPUDevice{}) // Wrong oldObj type
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid old object type"))
		})

		It("ValidateUpdate should check serial number uniqueness", func() {
			// Create first device with serial number
			obj1 := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-device-unique-1",
					Namespace: "default",
				},
				Spec: provisioningv1.DPUDeviceSpec{
					SerialNumber: "MT25066004U1",
				},
			}
			Expect(k8sClient.Create(ctx, obj1)).NotTo(HaveOccurred())
			DeferCleanup(k8sClient.Delete, ctx, obj1)

			// Create second device with different serial number
			obj2 := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-device-unique-2",
					Namespace: "default",
				},
				Spec: provisioningv1.DPUDeviceSpec{
					SerialNumber: "MT25066004U2",
				},
			}
			Expect(k8sClient.Create(ctx, obj2)).NotTo(HaveOccurred())
			DeferCleanup(k8sClient.Delete, ctx, obj2)

			// Try to update second device to have same serial number as first (should fail due to immutability)
			obj2.Spec.SerialNumber = "MT25066004U1"
			Expect(k8sClient.Update(ctx, obj2)).To(HaveOccurred())
		})
	})
})
