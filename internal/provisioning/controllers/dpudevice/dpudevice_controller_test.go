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

package dpudevice

import (
	"context"
	"encoding/base64"
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestDPUDeviceController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "DPUDevice Controller Non exported Suite")
}

var _ = Describe("DPUDeviceController Non exported", func() {
	Context("generateCR", func() {
		var (
			reconciler *DPUDeviceReconciler
			dpuDevice  *provisioningv1.DPUDevice
			testCSR    string
		)

		BeforeEach(func() {
			reconciler = &DPUDeviceReconciler{}
			dpuDevice = &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpudevice",
					Namespace: "test-namespace",
					UID:       types.UID("test-uid-123"),
				},
			}
			testCSR = "-----BEGIN CERTIFICATE REQUEST-----\nMIIBVjCB...\n-----END CERTIFICATE REQUEST-----"
		})

		It("should generate a CertificateRequest with correct GVK", func() {
			cr, err := reconciler.generateCR(dpuDevice, testCSR)
			Expect(err).NotTo(HaveOccurred())
			Expect(cr).NotTo(BeNil())

			gvk := cr.GroupVersionKind()
			Expect(gvk.Group).To(Equal("cert-manager.io"))
			Expect(gvk.Version).To(Equal("v1"))
			Expect(gvk.Kind).To(Equal("CertificateRequest"))
		})

		It("should set correct name and namespace", func() {
			cr, err := reconciler.generateCR(dpuDevice, testCSR)
			Expect(err).NotTo(HaveOccurred())

			Expect(cr.GetName()).To(Equal("test-dpudevice"))
			Expect(cr.GetNamespace()).To(Equal("test-namespace"))
		})

		It("should set owner reference to DPUDevice", func() {
			cr, err := reconciler.generateCR(dpuDevice, testCSR)
			Expect(err).NotTo(HaveOccurred())

			ownerRefs := cr.GetOwnerReferences()
			Expect(ownerRefs).To(HaveLen(1))
			Expect(ownerRefs[0].Name).To(Equal("test-dpudevice"))
			Expect(ownerRefs[0].Kind).To(Equal("DPUDevice"))
			Expect(*ownerRefs[0].Controller).To(BeTrue())
		})

		It("should encode CSR in base64", func() {
			cr, err := reconciler.generateCR(dpuDevice, testCSR)
			Expect(err).NotTo(HaveOccurred())

			spec, found, err := unstructured.NestedMap(cr.Object, "spec")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())

			request, found, err := unstructured.NestedString(spec, "request")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())

			// Decode and verify
			decoded, err := base64.StdEncoding.DecodeString(request)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(decoded)).To(Equal(testCSR))
		})

		It("should set isCA to false", func() {
			cr, err := reconciler.generateCR(dpuDevice, testCSR)
			Expect(err).NotTo(HaveOccurred())

			spec, _, _ := unstructured.NestedMap(cr.Object, "spec")
			isCA, found, err := unstructured.NestedBool(spec, "isCA")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(isCA).To(BeFalse())
		})

		It("should set correct usages", func() {
			cr, err := reconciler.generateCR(dpuDevice, testCSR)
			Expect(err).NotTo(HaveOccurred())

			spec, _, _ := unstructured.NestedMap(cr.Object, "spec")
			usages, found, err := unstructured.NestedStringSlice(spec, "usages")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(usages).To(ContainElements("server auth", "key encipherment", "digital signature"))
		})

		It("should set issuerRef", func() {
			cr, err := reconciler.generateCR(dpuDevice, testCSR)
			Expect(err).NotTo(HaveOccurred())

			spec, _, _ := unstructured.NestedMap(cr.Object, "spec")
			issuerRef, found, err := unstructured.NestedMap(spec, "issuerRef")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(issuerRef["kind"]).To(Equal("Issuer"))
			Expect(issuerRef["group"]).To(Equal("cert-manager.io"))
		})
	})

	Context("createCR", func() {
		var (
			reconciler *DPUDeviceReconciler
			dpuDevice  *provisioningv1.DPUDevice
			testCSR    string
			fakeClient client.Client
			ctx        context.Context
		)

		BeforeEach(func() {
			ctx = context.Background()
			scheme := runtime.NewScheme()
			_ = provisioningv1.AddToScheme(scheme)

			dpuDevice = &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpudevice",
					Namespace: "test-namespace",
					UID:       types.UID("test-uid-123"),
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				Build()

			reconciler = &DPUDeviceReconciler{
				Client: fakeClient,
			}

			testCSR = "-----BEGIN CERTIFICATE REQUEST-----\nMIIBVjCB...\n-----END CERTIFICATE REQUEST-----"
		})

		It("should create CertificateRequest successfully", func() {
			err := reconciler.createCR(ctx, dpuDevice, testCSR)
			Expect(err).NotTo(HaveOccurred())

			// Verify the CR was created
			createdCR := &unstructured.Unstructured{}
			createdCR.SetGroupVersionKind(crGVK())
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      "test-dpudevice",
				Namespace: "test-namespace",
			}, createdCR)
			Expect(err).NotTo(HaveOccurred())
			Expect(createdCR.GetName()).To(Equal("test-dpudevice"))
		})

		It("should fail if CR already exists", func() {
			// Create the CR first time
			err := reconciler.createCR(ctx, dpuDevice, testCSR)
			Expect(err).NotTo(HaveOccurred())

			// Try to create again - should fail
			err = reconciler.createCR(ctx, dpuDevice, testCSR)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("initializeDPUDevice", func() {
		var (
			reconciler *DPUDeviceReconciler
			dpuDevice  *provisioningv1.DPUDevice
			fakeClient client.Client
			ctx        context.Context
		)

		BeforeEach(func() {
			ctx = context.Background()
			scheme := runtime.NewScheme()
			_ = provisioningv1.AddToScheme(scheme)

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				Build()

			reconciler = &DPUDeviceReconciler{
				Client: fakeClient,
			}
		})

		It("should return error when BMCIP is nil", func() {
			dpuDevice = &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpudevice",
					Namespace: "test-namespace",
				},
				Status: provisioningv1.DPUDeviceStatus{
					BMCIP: nil, // BMCIP is not set
				},
			}

			err := reconciler.initializeDPUDevice(ctx, dpuDevice)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("BMCIP is required but not set"))

			// Verify condition is set
			conditions := dpuDevice.GetConditions()
			Expect(conditions).NotTo(BeEmpty())

			var foundCondition *metav1.Condition
			for i := range conditions {
				if conditions[i].Type == string(provisioningv1.ConditionDpuDeviceInitialized) {
					foundCondition = &conditions[i]
					break
				}
			}
			Expect(foundCondition).NotTo(BeNil())
			Expect(foundCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(foundCondition.Reason).To(Equal("MissingBMCIP"))
		})

		It("should fail when InitPassword fails", func() {
			bmcIP := "192.168.1.100"
			dpuDevice = &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpudevice",
					Namespace: "test-namespace",
				},
				Status: provisioningv1.DPUDeviceStatus{
					BMCIP: &bmcIP,
				},
			}

			// This will fail because there's no BMC to connect to
			err := reconciler.initializeDPUDevice(ctx, dpuDevice)
			Expect(err).To(HaveOccurred())
			// The error could be about password initialization or connection
			Expect(err.Error()).To(SatisfyAny(
				ContainSubstring("failed to initialize password"),
				ContainSubstring("connection"),
				ContainSubstring("dial"),
			))

			// Verify condition is set with failure
			conditions := dpuDevice.GetConditions()
			Expect(conditions).NotTo(BeEmpty())

			var foundCondition *metav1.Condition
			for i := range conditions {
				if conditions[i].Type == string(provisioningv1.ConditionDpuDeviceInitialized) {
					foundCondition = &conditions[i]
					break
				}
			}
			Expect(foundCondition).NotTo(BeNil())
			Expect(foundCondition.Status).To(Equal(metav1.ConditionFalse))
		})
	})

	Context("discoverDPUDevice", func() {
		It("should fail when TLS client creation fails (no secrets)", func() {
			ctx := context.Background()
			scheme := runtime.NewScheme()
			_ = provisioningv1.AddToScheme(scheme)

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				Build()

			reconciler := &DPUDeviceReconciler{
				Client: fakeClient,
			}

			bmcIP := "192.168.1.100"
			dpuDevice := &provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpudevice",
					Namespace: "test-namespace",
				},
				Status: provisioningv1.DPUDeviceStatus{
					BMCIP: &bmcIP,
				},
			}

			// Will fail because no TLS secrets exist in the fake client
			err := reconciler.discoverDPUDevice(ctx, dpuDevice)
			Expect(err).To(HaveOccurred())
		})
	})
})

// Helper to get CertificateRequest GVK
func crGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   "cert-manager.io",
		Version: "v1",
		Kind:    "CertificateRequest",
	}
}
