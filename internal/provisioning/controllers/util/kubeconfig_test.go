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

package util

import (
	"context"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("GenerateKubeconfig", func() {
	Context("positive", func() {
		It("should generate a kubeconfig parseable by clientcmd", func() {
			tokenSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      DPUAgentTokenSecretName,
					Namespace: "test-ns",
				},
				Data: map[string][]byte{
					"token":  []byte("test-token-value"),
					"ca.crt": []byte("fake-ca-data"),
				},
			}
			fakeClient := fake.NewClientBuilder().WithObjects(tokenSecret).Build()

			data, err := GenerateKubeconfig(context.Background(), fakeClient, "https://10.0.0.1:6443", "test-ns")
			Expect(err).NotTo(HaveOccurred())
			Expect(data).NotTo(BeEmpty())

			tmpFile, err := os.CreateTemp("", "kubeconfig-test-*")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.Remove(tmpFile.Name()) }()
			Expect(os.WriteFile(tmpFile.Name(), data, 0600)).To(Succeed())

			cfg, err := clientcmd.BuildConfigFromFlags("", tmpFile.Name())
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.Host).To(Equal("https://10.0.0.1:6443"))
			Expect(cfg.BearerToken).To(Equal("test-token-value"))
			Expect(cfg.CAData).To(Equal([]byte("fake-ca-data")))
		})
	})

	Context("negative", func() {
		It("should return error when token secret does not exist", func() {
			fakeClient := fake.NewClientBuilder().Build()

			_, err := GenerateKubeconfig(context.Background(), fakeClient, "https://10.0.0.1:6443", "test-ns")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("getting token secret"))
		})

		It("should return error when ca.crt is missing from secret", func() {
			tokenSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      DPUAgentTokenSecretName,
					Namespace: "test-ns",
				},
				Data: map[string][]byte{
					"token": []byte("test-token-value"),
				},
			}
			fakeClient := fake.NewClientBuilder().WithObjects(tokenSecret).Build()

			_, err := GenerateKubeconfig(context.Background(), fakeClient, "https://10.0.0.1:6443", "test-ns")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("ca.crt not found"))
		})

		It("should return error when token key is missing from secret", func() {
			tokenSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      DPUAgentTokenSecretName,
					Namespace: "test-ns",
				},
				Data: map[string][]byte{},
			}
			fakeClient := fake.NewClientBuilder().WithObjects(tokenSecret).Build()

			_, err := GenerateKubeconfig(context.Background(), fakeClient, "https://10.0.0.1:6443", "test-ns")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("token not found"))
		})
	})
})
