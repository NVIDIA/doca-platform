/*
Copyright 2026 NVIDIA

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

package client

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	testNamespace           = "test-ns"
	testPerDeviceSecretName = "my-dpu-bmc-v1"
)

func TestRedfishClient(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Redfish Client Suite")
}

var _ = Describe("ResolveBMCCredential", func() {
	var (
		ctx    context.Context
		scheme *runtime.Scheme
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		_ = corev1.AddToScheme(scheme)
	})

	It("should use per-device secret when bmcCredentialSecretName is set", func() {
		perDeviceSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testPerDeviceSecretName,
				Namespace: testNamespace,
			},
			Data: map[string][]byte{
				"password": []byte("per-device-password"),
			},
		}

		k8sClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(perDeviceSecret).
			Build()

		result, err := ResolveBMCCredential(ctx, testNamespace, ptr.To(testPerDeviceSecretName), k8sClient)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Password).To(Equal("per-device-password"))
		Expect(result.SecretName).To(Equal(testPerDeviceSecretName))
	})

	It("should fall back to shared secret when bmcCredentialSecretName is nil", func() {
		sharedSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      BMCPasswordSecret,
				Namespace: testNamespace,
			},
			Data: map[string][]byte{
				"password": []byte("shared-password"),
			},
		}

		k8sClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(sharedSecret).
			Build()

		result, err := ResolveBMCCredential(ctx, testNamespace, nil, k8sClient)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Password).To(Equal("shared-password"))
		Expect(result.SecretName).To(Equal(BMCPasswordSecret))
	})

	It("should fall back to shared secret when bmcCredentialSecretName is empty", func() {
		sharedSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      BMCPasswordSecret,
				Namespace: testNamespace,
			},
			Data: map[string][]byte{
				"password": []byte("shared-password"),
			},
		}

		k8sClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(sharedSecret).
			Build()

		result, err := ResolveBMCCredential(ctx, testNamespace, ptr.To(""), k8sClient)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Password).To(Equal("shared-password"))
		Expect(result.SecretName).To(Equal(BMCPasswordSecret))
	})

	It("should return error when referenced per-device secret does not exist", func() {
		k8sClient := fake.NewClientBuilder().
			WithScheme(scheme).
			Build()

		_, err := ResolveBMCCredential(ctx, testNamespace, ptr.To("nonexistent-secret"), k8sClient)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not found"))
	})

	It("should return error when per-device secret has empty password", func() {
		perDeviceSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testPerDeviceSecretName,
				Namespace: testNamespace,
			},
			Data: map[string][]byte{
				"password": []byte(""),
			},
		}

		k8sClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(perDeviceSecret).
			Build()

		_, err := ResolveBMCCredential(ctx, testNamespace, ptr.To(testPerDeviceSecretName), k8sClient)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("empty or missing"))
	})

	It("should return error when per-device secret is missing password key", func() {
		perDeviceSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testPerDeviceSecretName,
				Namespace: testNamespace,
			},
			Data: map[string][]byte{
				"wrong-key": []byte("some-value"),
			},
		}

		k8sClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(perDeviceSecret).
			Build()

		_, err := ResolveBMCCredential(ctx, testNamespace, ptr.To(testPerDeviceSecretName), k8sClient)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("empty or missing"))
	})

	It("should return error when shared secret is missing", func() {
		k8sClient := fake.NewClientBuilder().
			WithScheme(scheme).
			Build()

		_, err := ResolveBMCCredential(ctx, testNamespace, nil, k8sClient)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not found"))
	})

	It("should return error when shared secret has empty password", func() {
		sharedSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      BMCPasswordSecret,
				Namespace: testNamespace,
			},
			Data: map[string][]byte{
				"password": []byte(""),
			},
		}

		k8sClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(sharedSecret).
			Build()

		_, err := ResolveBMCCredential(ctx, testNamespace, nil, k8sClient)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("empty or missing"))
	})

	It("should resolve distinct secrets for multiple DPUDevices", func() {
		secret1 := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "dpu1-bmc", Namespace: testNamespace},
			Data:       map[string][]byte{"password": []byte("password1")},
		}
		secret2 := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "dpu2-bmc", Namespace: testNamespace},
			Data:       map[string][]byte{"password": []byte("password2")},
		}

		k8sClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(secret1, secret2).
			Build()

		result1, err := ResolveBMCCredential(ctx, testNamespace, ptr.To("dpu1-bmc"), k8sClient)
		Expect(err).NotTo(HaveOccurred())
		Expect(result1.Password).To(Equal("password1"))

		result2, err := ResolveBMCCredential(ctx, testNamespace, ptr.To("dpu2-bmc"), k8sClient)
		Expect(err).NotTo(HaveOccurred())
		Expect(result2.Password).To(Equal("password2"))
	})
})
