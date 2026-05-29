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
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = rbacv1.AddToScheme(s)
	_ = provisioningv1.AddToScheme(s)
	return s
}

func newTestDPU(name, namespace string) *provisioningv1.DPU {
	return &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       types.UID("test-uid-12345"),
		},
	}
}

var _ = Describe("ZT Bootstrap", func() {
	var (
		ctx    context.Context
		scheme *runtime.Scheme
		dpu    *provisioningv1.DPU
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = newTestScheme()
		dpu = newTestDPU("dpu-01", "dpf-operator-system")
	})

	Describe("CreateDPUAgentRole", func() {
		It("should create a role with correct rules and owner reference", func() {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			dpu.Spec.BlueFieldSoftware = "bfs-1"

			err := CreateDPUAgentRole(ctx, fakeClient, scheme, dpu)
			Expect(err).NotTo(HaveOccurred())

			role := &rbacv1.Role{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      "da-dpu-01",
				Namespace: "dpf-operator-system",
			}, role)).To(Succeed())

			Expect(role.Rules).To(HaveLen(4))
			Expect(role.Rules[0].APIGroups).To(Equal([]string{"provisioning.dpu.nvidia.com"}))
			Expect(role.Rules[0].Resources).To(Equal([]string{"dpus"}))
			Expect(role.Rules[0].ResourceNames).To(Equal([]string{"dpu-01"}))
			Expect(role.Rules[0].Verbs).To(Equal([]string{"get"}))

			Expect(role.Rules[1].Resources).To(Equal([]string{"dpus/status"}))
			Expect(role.Rules[1].ResourceNames).To(Equal([]string{"dpu-01"}))
			Expect(role.Rules[1].Verbs).To(Equal([]string{"patch"}))

			Expect(role.Rules[2].APIGroups).To(Equal([]string{""}))
			Expect(role.Rules[2].Resources).To(Equal([]string{"secrets"}))
			Expect(role.Rules[2].ResourceNames).To(Equal([]string{"dpu-01-kubeadm-join"}))
			Expect(role.Rules[2].Verbs).To(Equal([]string{"get"}))

			Expect(role.Rules[3].APIGroups).To(Equal([]string{"provisioning.dpu.nvidia.com"}))
			Expect(role.Rules[3].Resources).To(Equal([]string{"bluefieldsoftwares"}))
			Expect(role.Rules[3].ResourceNames).To(Equal([]string{"bfs-1"}))
			Expect(role.Rules[3].Verbs).To(Equal([]string{"get"}))

			Expect(role.OwnerReferences).To(HaveLen(1))
			Expect(role.OwnerReferences[0].Name).To(Equal("dpu-01"))
			Expect(role.OwnerReferences[0].UID).To(Equal(dpu.UID))
		})

		It("should not fail when role already exists", func() {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			Expect(CreateDPUAgentRole(ctx, fakeClient, scheme, dpu)).To(Succeed())
			Expect(CreateDPUAgentRole(ctx, fakeClient, scheme, dpu)).To(Succeed())
		})
	})

	Describe("CreateDPUAgentRoleBinding", func() {
		It("should create a role binding with correct subject and owner reference", func() {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			err := CreateDPUAgentRoleBinding(ctx, fakeClient, scheme, dpu)
			Expect(err).NotTo(HaveOccurred())

			rb := &rbacv1.RoleBinding{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      "da-dpu-01",
				Namespace: "dpf-operator-system",
			}, rb)).To(Succeed())

			Expect(rb.Subjects).To(HaveLen(1))
			Expect(rb.Subjects[0].Kind).To(Equal(rbacv1.UserKind))
			Expect(rb.Subjects[0].Name).To(Equal("da-dpu-01"))
			Expect(rb.Subjects[0].APIGroup).To(Equal(rbacv1.GroupName))

			Expect(rb.RoleRef.Kind).To(Equal("Role"))
			Expect(rb.RoleRef.Name).To(Equal("da-dpu-01"))
			Expect(rb.RoleRef.APIGroup).To(Equal(rbacv1.GroupName))

			Expect(rb.OwnerReferences).To(HaveLen(1))
			Expect(rb.OwnerReferences[0].Name).To(Equal("dpu-01"))
			Expect(rb.OwnerReferences[0].UID).To(Equal(dpu.UID))
		})

		It("should not fail when role binding already exists", func() {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			Expect(CreateDPUAgentRoleBinding(ctx, fakeClient, scheme, dpu)).To(Succeed())
			Expect(CreateDPUAgentRoleBinding(ctx, fakeClient, scheme, dpu)).To(Succeed())
		})
	})

	Describe("CreateDPUAgentBootstrapKubeconfig", func() {
		It("should create a bootstrap token and return a valid kubeconfig", func() {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			tmpCA, err := os.CreateTemp("", "ca-test-*")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.Remove(tmpCA.Name()) }()
			Expect(os.WriteFile(tmpCA.Name(), []byte("fake-ca-data"), 0600)).To(Succeed())

			kubeconfigData, err := CreateDPUAgentBootstrapKubeconfig(ctx, fakeClient, dpu, "https://10.0.0.1:6443", tmpCA.Name(), "")
			Expect(err).NotTo(HaveOccurred())
			Expect(kubeconfigData).NotTo(BeEmpty())

			secretList := &corev1.SecretList{}
			Expect(fakeClient.List(ctx, secretList, client.InNamespace("kube-system"))).To(Succeed())
			Expect(secretList.Items).To(HaveLen(1))

			s := secretList.Items[0]
			Expect(s.Name).To(HavePrefix("bootstrap-token-"))
			Expect(s.Namespace).To(Equal("kube-system"))
			Expect(s.Type).To(Equal(corev1.SecretTypeBootstrapToken))
			Expect(s.Labels[LabelDPUName]).To(Equal("dpu-01"))
			Expect(s.Labels[LabelDPUNamespace]).To(Equal("dpf-operator-system"))
			Expect(s.StringData["usage-bootstrap-authentication"]).To(Equal("true"))
			Expect(s.StringData["auth-extra-groups"]).To(Equal(DPUAgentBootstrapGroup))

			tmpKubeconfig, err := os.CreateTemp("", "kubeconfig-test-*")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.Remove(tmpKubeconfig.Name()) }()
			Expect(os.WriteFile(tmpKubeconfig.Name(), kubeconfigData, 0600)).To(Succeed())

			cfg, err := clientcmd.BuildConfigFromFlags("", tmpKubeconfig.Name())
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.Host).To(Equal("https://10.0.0.1:6443"))
			Expect(cfg.BearerToken).NotTo(BeEmpty())
			Expect(cfg.CAData).To(Equal([]byte("fake-ca-data")))
		})

		It("should include proxy-url in kubeconfig when specified", func() {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			tmpCA, err := os.CreateTemp("", "ca-test-*")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.Remove(tmpCA.Name()) }()
			Expect(os.WriteFile(tmpCA.Name(), []byte("fake-ca-data"), 0600)).To(Succeed())

			proxyURL := "http://[fe80::1%25tmfifo_net0]:11030"
			kubeconfigData, err := CreateDPUAgentBootstrapKubeconfig(ctx, fakeClient, dpu, "https://kubernetes.default.svc:6443", tmpCA.Name(), proxyURL)
			Expect(err).NotTo(HaveOccurred())

			tmpKubeconfig, err := os.CreateTemp("", "kubeconfig-test-*")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.Remove(tmpKubeconfig.Name()) }()
			Expect(os.WriteFile(tmpKubeconfig.Name(), kubeconfigData, 0600)).To(Succeed())

			cfg, err := clientcmd.BuildConfigFromFlags("", tmpKubeconfig.Name())
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.Host).To(Equal("https://kubernetes.default.svc:6443"))
			Expect(cfg.Proxy).NotTo(BeNil())
		})

		It("should reuse existing token when one already exists", func() {
			existingSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bootstrap-token-abc123",
					Namespace: "kube-system",
					Labels: map[string]string{
						LabelDPUName:      "dpu-01",
						LabelDPUNamespace: "dpf-operator-system",
					},
				},
				Type: corev1.SecretTypeBootstrapToken,
				Data: map[string][]byte{
					"token-id":     []byte("abc123"),
					"token-secret": []byte("1234567890abcdef"),
					"expiration":   []byte(time.Now().Add(1 * time.Hour).Format(time.RFC3339)),
				},
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existingSecret).Build()

			tmpCA, err := os.CreateTemp("", "ca-test-*")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.Remove(tmpCA.Name()) }()
			Expect(os.WriteFile(tmpCA.Name(), []byte("fake-ca-data"), 0600)).To(Succeed())

			kubeconfigData, err := CreateDPUAgentBootstrapKubeconfig(ctx, fakeClient, dpu, "https://10.0.0.1:6443", tmpCA.Name(), "")
			Expect(err).NotTo(HaveOccurred())

			tmpKubeconfig, err := os.CreateTemp("", "kubeconfig-test-*")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.Remove(tmpKubeconfig.Name()) }()
			Expect(os.WriteFile(tmpKubeconfig.Name(), kubeconfigData, 0600)).To(Succeed())

			cfg, err := clientcmd.BuildConfigFromFlags("", tmpKubeconfig.Name())
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.BearerToken).To(Equal("abc123.1234567890abcdef"))

			secretList := &corev1.SecretList{}
			Expect(fakeClient.List(ctx, secretList, client.InNamespace("kube-system"))).To(Succeed())
			Expect(secretList.Items).To(HaveLen(1))
		})
	})

	Describe("DeleteDPUAgentBootstrapTokens", func() {
		It("should delete bootstrap tokens matching the DPU labels", func() {
			matchingSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bootstrap-token-abc123",
					Namespace: "kube-system",
					Labels: map[string]string{
						LabelDPUName:      "dpu-01",
						LabelDPUNamespace: "dpf-operator-system",
					},
				},
				Type: corev1.SecretTypeBootstrapToken,
			}
			otherSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bootstrap-token-xyz789",
					Namespace: "kube-system",
					Labels: map[string]string{
						LabelDPUName:      "dpu-02",
						LabelDPUNamespace: "dpf-operator-system",
					},
				},
				Type: corev1.SecretTypeBootstrapToken,
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(matchingSecret, otherSecret).Build()

			err := DeleteDPUAgentBootstrapTokens(ctx, fakeClient, "dpu-01", "dpf-operator-system")
			Expect(err).NotTo(HaveOccurred())

			secretList := &corev1.SecretList{}
			Expect(fakeClient.List(ctx, secretList, client.InNamespace("kube-system"))).To(Succeed())
			Expect(secretList.Items).To(HaveLen(1))
			Expect(secretList.Items[0].Name).To(Equal("bootstrap-token-xyz789"))
		})

		It("should not fail when no tokens exist", func() {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			err := DeleteDPUAgentBootstrapTokens(ctx, fakeClient, "dpu-01", "dpf-operator-system")
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("ResolveAPIServerAddress", func() {
		const testVIP = "10.0.0.1"

		It("zero trust requires VIP and Port", func() {
			_, _, err := ResolveAPIServerAddress(nil, true)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("KubernetesAPIServerVIP"))
		})

		It("zero trust returns address without proxy", func() {
			vip := testVIP
			port := 6443
			overrides := &operatorv1.Overrides{
				KubernetesAPIServerVIP:  &vip,
				KubernetesAPIServerPort: &port,
			}
			addr, proxyURL, err := ResolveAPIServerAddress(overrides, true)
			Expect(err).NotTo(HaveOccurred())
			Expect(addr).To(Equal("https://10.0.0.1:6443"))
			Expect(proxyURL).To(BeEmpty())
		})

		It("trusted host with VIP and Port configured", func() {
			vip := testVIP
			port := 6443
			overrides := &operatorv1.Overrides{
				KubernetesAPIServerVIP:  &vip,
				KubernetesAPIServerPort: &port,
			}
			addr, proxyURL, err := ResolveAPIServerAddress(overrides, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(addr).To(Equal("https://10.0.0.1:6443"))
			Expect(proxyURL).To(Equal("http://[fe80::1%25tmfifo_net0]:11030"))
		})

		It("trusted host with only VIP configured uses KUBERNETES_SERVICE_PORT", func() {
			GinkgoT().Setenv("KUBERNETES_SERVICE_PORT", "6443")
			vip := testVIP
			overrides := &operatorv1.Overrides{
				KubernetesAPIServerVIP: &vip,
			}
			addr, proxyURL, err := ResolveAPIServerAddress(overrides, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(addr).To(Equal("https://10.0.0.1:6443"))
			Expect(proxyURL).To(Equal("http://[fe80::1%25tmfifo_net0]:11030"))
		})

		It("trusted host with only Port configured uses kubernetes.default.svc", func() {
			port := 6443
			overrides := &operatorv1.Overrides{
				KubernetesAPIServerPort: &port,
			}
			addr, proxyURL, err := ResolveAPIServerAddress(overrides, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(addr).To(Equal("https://kubernetes.default.svc:6443"))
			Expect(proxyURL).To(Equal("http://[fe80::1%25tmfifo_net0]:11030"))
		})

		It("trusted host with no overrides uses defaults", func() {
			GinkgoT().Setenv("KUBERNETES_SERVICE_PORT", "443")
			addr, proxyURL, err := ResolveAPIServerAddress(nil, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(addr).To(Equal("https://kubernetes.default.svc:443"))
			Expect(proxyURL).To(Equal("http://[fe80::1%25tmfifo_net0]:11030"))
		})

		It("trusted host with no port anywhere falls back to 443", func() {
			GinkgoT().Setenv("KUBERNETES_SERVICE_PORT", "")
			addr, proxyURL, err := ResolveAPIServerAddress(nil, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(addr).To(Equal("https://kubernetes.default.svc:443"))
			Expect(proxyURL).To(Equal("http://[fe80::1%25tmfifo_net0]:11030"))
		})
	})

})
