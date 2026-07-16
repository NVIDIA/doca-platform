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
	"net/http"
	"os"
	"path/filepath"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
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
			dpu.Spec.BlueFieldSoftware = ptr.To("bfs-1")
			flavor := &provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					ConfigFiles: []provisioningv1.ConfigFile{
						{
							Type: ptr.To(provisioningv1.ConfigFileTypeAgentApplied),
							ContentFrom: &provisioningv1.ConfigFileContentSource{
								ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "doca-profile"},
									Key:                  "profile.conf",
								},
							},
						},
					},
				},
			}

			err := CreateDPUAgentRole(ctx, fakeClient, scheme, dpu, flavor)
			Expect(err).NotTo(HaveOccurred())

			role := &rbacv1.Role{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      "da-dpu-01",
				Namespace: "dpf-operator-system",
			}, role)).To(Succeed())

			Expect(role.Rules).To(HaveLen(5))
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
			Expect(role.Rules[4].APIGroups).To(Equal([]string{""}))
			Expect(role.Rules[4].Resources).To(Equal([]string{"configmaps"}))
			Expect(role.Rules[4].ResourceNames).To(Equal([]string{"doca-profile"}))
			Expect(role.Rules[4].Verbs).To(Equal([]string{"get"}))

			Expect(role.OwnerReferences).To(HaveLen(1))
			Expect(role.OwnerReferences[0].Name).To(Equal("dpu-01"))
			Expect(role.OwnerReferences[0].UID).To(Equal(dpu.UID))
		})

		It("should not fail when role already exists", func() {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			Expect(CreateDPUAgentRole(ctx, fakeClient, scheme, dpu, nil)).To(Succeed())
			Expect(CreateDPUAgentRole(ctx, fakeClient, scheme, dpu, nil)).To(Succeed())
		})

		It("should update existing role when configmap references change", func() {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			Expect(CreateDPUAgentRole(ctx, fakeClient, scheme, dpu, nil)).To(Succeed())
			flavor := &provisioningv1.DPUFlavor{
				Spec: provisioningv1.DPUFlavorSpec{
					ConfigFiles: []provisioningv1.ConfigFile{
						{
							Type: ptr.To(provisioningv1.ConfigFileTypeAgentApplied),
							ContentFrom: &provisioningv1.ConfigFileContentSource{
								ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "cfg-a"},
									Key:                  "k",
								},
							},
						},
						{
							Type: ptr.To(provisioningv1.ConfigFileTypeAgentApplied),
							ContentFrom: &provisioningv1.ConfigFileContentSource{
								ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "cfg-b"},
									Key:                  "k",
								},
							},
						},
					},
				},
			}
			Expect(CreateDPUAgentRole(ctx, fakeClient, scheme, dpu, flavor)).To(Succeed())

			role := &rbacv1.Role{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      "da-dpu-01",
				Namespace: "dpf-operator-system",
			}, role)).To(Succeed())
			Expect(role.Rules).To(HaveLen(5))
			Expect(role.Rules[4].Resources).To(Equal([]string{"configmaps"}))
			Expect(role.Rules[4].ResourceNames).To(Equal([]string{"cfg-a", "cfg-b"}))
		})
	})

	Describe("CreateDPUAgentRoleBinding", func() {
		It("should create a role binding with correct subject and owner reference", func() {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			err := CreateDPUAgentRoleBinding(ctx, fakeClient, scheme, dpu, "da-dpu-01")
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

		It("should create a role binding with a SPIFFE URI subject", func() {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
			spiffeURI := "spiffe://example.trust.domain/ns/dpf-operator-system/dpu/serial123"

			err := CreateDPUAgentRoleBinding(ctx, fakeClient, scheme, dpu, spiffeURI)
			Expect(err).NotTo(HaveOccurred())

			rb := &rbacv1.RoleBinding{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      "da-dpu-01",
				Namespace: "dpf-operator-system",
			}, rb)).To(Succeed())

			Expect(rb.Subjects).To(HaveLen(1))
			Expect(rb.Subjects[0].Kind).To(Equal(rbacv1.UserKind))
			Expect(rb.Subjects[0].Name).To(Equal(spiffeURI))
			Expect(rb.Subjects[0].APIGroup).To(Equal(rbacv1.GroupName))
		})

		It("should not fail when role binding already exists", func() {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			Expect(CreateDPUAgentRoleBinding(ctx, fakeClient, scheme, dpu, "da-dpu-01")).To(Succeed())
			Expect(CreateDPUAgentRoleBinding(ctx, fakeClient, scheme, dpu, "da-dpu-01")).To(Succeed())
		})

		It("should not fail, and should leave the existing binding untouched, when a stale cache causes Create to race an existing role binding", func() {
			existing := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{
				Name: "da-dpu-01", Namespace: "dpf-operator-system",
			}}
			// Simulate a stale-cache miss on the first Get only; later Gets see real state.
			staleGetDone := false
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).
				WithObjects(existing).
				WithInterceptorFuncs(interceptor.Funcs{
					Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if _, ok := obj.(*rbacv1.RoleBinding); ok && !staleGetDone {
							staleGetDone = true
							return apierrors.NewNotFound(rbacv1.Resource("rolebindings"), key.Name)
						}
						return c.Get(ctx, key, obj, opts...)
					},
				}).Build()

			Expect(CreateDPUAgentRoleBinding(ctx, fakeClient, scheme, dpu, "da-dpu-01")).To(Succeed())

			// The existing binding is left untouched by this call.
			rb := &rbacv1.RoleBinding{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      "da-dpu-01",
				Namespace: "dpf-operator-system",
			}, rb)).To(Succeed())
			Expect(rb.Subjects).To(BeEmpty())
		})

		It("reconciles a stale subject on an existing binding while leaving RoleRef untouched", func() {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			// First create with the bootstrap-token subject, then re-run with the SPIFFE URI
			// subject to simulate an identity-mode subject swap on an already-existing binding.
			Expect(CreateDPUAgentRoleBinding(ctx, fakeClient, scheme, dpu, "da-dpu-01")).To(Succeed())
			spiffeURI := "spiffe://example.trust.domain/ns/dpf-operator-system/dpu/serial123"
			Expect(CreateDPUAgentRoleBinding(ctx, fakeClient, scheme, dpu, spiffeURI)).To(Succeed())

			rb := &rbacv1.RoleBinding{}
			Expect(fakeClient.Get(ctx, types.NamespacedName{
				Name:      "da-dpu-01",
				Namespace: "dpf-operator-system",
			}, rb)).To(Succeed())

			Expect(rb.Subjects).To(HaveLen(1))
			Expect(rb.Subjects[0].Name).To(Equal(spiffeURI), "stale subject must be reconciled")
			// RoleRef is immutable and set on create only; it must remain the original value.
			Expect(rb.RoleRef.Kind).To(Equal("Role"))
			Expect(rb.RoleRef.Name).To(Equal("da-dpu-01"))
			Expect(rb.RoleRef.APIGroup).To(Equal(rbacv1.GroupName))
			Expect(rb.OwnerReferences).To(HaveLen(1))
			Expect(rb.OwnerReferences[0].UID).To(Equal(dpu.UID))
		})
	})

	Describe("CreateDPUAgentBootstrapToken", func() {
		It("should create a bootstrap token", func() {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			token, err := CreateDPUAgentBootstrapToken(ctx, fakeClient, dpu)
			Expect(err).NotTo(HaveOccurred())
			Expect(token).NotTo(BeEmpty())

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

			token, err := CreateDPUAgentBootstrapToken(ctx, fakeClient, dpu)
			Expect(err).NotTo(HaveOccurred())
			Expect(token).To(Equal("abc123.1234567890abcdef"))

			secretList := &corev1.SecretList{}
			Expect(fakeClient.List(ctx, secretList, client.InNamespace("kube-system"))).To(Succeed())
			Expect(secretList.Items).To(HaveLen(1))
		})
	})

	Describe("GenerateBootstrapKubeconfig", func() {
		It("should generate a valid bootstrap kubeconfig", func() {
			data, err := GenerateBootstrapKubeconfig("https://10.0.0.1:6443", "abc123.token", []byte("fake-ca-data"), "")
			Expect(err).NotTo(HaveOccurred())

			tmpKubeconfig, err := os.CreateTemp("", "kubeconfig-test-*")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.Remove(tmpKubeconfig.Name()) }()
			Expect(os.WriteFile(tmpKubeconfig.Name(), data, 0600)).To(Succeed())

			cfg, err := clientcmd.BuildConfigFromFlags("", tmpKubeconfig.Name())
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.Host).To(Equal("https://10.0.0.1:6443"))
			Expect(cfg.BearerToken).To(Equal("abc123.token"))
			Expect(cfg.CAData).To(Equal([]byte("fake-ca-data")))
		})

		It("should include proxy-url when specified", func() {
			proxyURL := "http://[fe80::1%25tmfifo_net0]:11030"
			data, err := GenerateBootstrapKubeconfig("https://kubernetes.default.svc:6443", "abc123.token", []byte("fake-ca-data"), proxyURL)
			Expect(err).NotTo(HaveOccurred())

			tmpKubeconfig, err := os.CreateTemp("", "kubeconfig-test-*")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.Remove(tmpKubeconfig.Name()) }()
			Expect(os.WriteFile(tmpKubeconfig.Name(), data, 0600)).To(Succeed())

			cfg, err := clientcmd.BuildConfigFromFlags("", tmpKubeconfig.Name())
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.Host).To(Equal("https://kubernetes.default.svc:6443"))
			Expect(cfg.Proxy).NotTo(BeNil())
		})
	})

	Describe("GenerateSpiffeKubeconfig", func() {
		It("should generate a kubeconfig with BearerTokenFile", func() {
			tokenPath := filepath.Join(GinkgoT().TempDir(), "token")
			Expect(os.WriteFile(tokenPath, []byte("jwt"), 0600)).To(Succeed())

			data, err := GenerateSpiffeKubeconfig("https://10.0.0.1:6443", tokenPath, []byte("fake-ca-data"), "")
			Expect(err).NotTo(HaveOccurred())

			tmpKubeconfig, err := os.CreateTemp("", "spiffe-kubeconfig-test-*")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.Remove(tmpKubeconfig.Name()) }()
			Expect(os.WriteFile(tmpKubeconfig.Name(), data, 0600)).To(Succeed())

			cfg, err := clientcmd.BuildConfigFromFlags("", tmpKubeconfig.Name())
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.Host).To(Equal("https://10.0.0.1:6443"))
			Expect(cfg.BearerTokenFile).To(Equal(tokenPath))
			Expect(cfg.CAData).To(Equal([]byte("fake-ca-data")))
		})

		It("writes proxy-url into the cluster stanza when specified", func() {
			tokenPath := filepath.Join(GinkgoT().TempDir(), "token")
			Expect(os.WriteFile(tokenPath, []byte("jwt"), 0600)).To(Succeed())

			data, err := GenerateSpiffeKubeconfig("https://10.0.0.1:6443", tokenPath, []byte("fake-ca-data"), "http://proxy.example.com:3128")
			Expect(err).NotTo(HaveOccurred())

			tmpKubeconfig, err := os.CreateTemp("", "spiffe-kubeconfig-test-*")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.Remove(tmpKubeconfig.Name()) }()
			Expect(os.WriteFile(tmpKubeconfig.Name(), data, 0600)).To(Succeed())

			cfg, err := clientcmd.BuildConfigFromFlags("", tmpKubeconfig.Name())
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.Proxy).NotTo(BeNil())

			req, err := http.NewRequest(http.MethodGet, "https://10.0.0.1:6443", nil)
			Expect(err).NotTo(HaveOccurred())
			proxyURL, err := cfg.Proxy(req)
			Expect(err).NotTo(HaveOccurred())
			Expect(proxyURL).NotTo(BeNil())
			Expect(proxyURL.Host).To(Equal("proxy.example.com:3128"))
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
