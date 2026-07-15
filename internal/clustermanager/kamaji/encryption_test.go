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

package nvidia

import (
	"bytes"
	"encoding/base64"
	"strings"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/kmsplugin/config"
	testutils "github.com/nvidia/doca-platform/test/utils"
	kamajiv1 "github.com/nvidia/doca-platform/third_party/forked/github.com/clastix/kamaji/api/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	apiserverv1 "k8s.io/apiserver/pkg/apis/apiserver/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/yaml"
)

func encVolumeNames(vols []corev1.Volume) []string {
	names := make([]string, 0, len(vols))
	for _, v := range vols {
		names = append(names, v.Name)
	}
	return names
}

var _ = Describe("etcd encryption at rest", func() {
	Describe("encryptionConfigSecretName", func() {
		It("is derived from the cluster name", func() {
			dc := &provisioningv1.DPUCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster-1", Namespace: "ns"}}
			Expect(encryptionConfigSecretName(dc)).To(Equal("etcd-encryption-config-cluster-1"))
		})

		It("shortens names that exceed the DNS subdomain limit", func() {
			dc := &provisioningv1.DPUCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      strings.Repeat("a", validation.DNS1123SubdomainMaxLength),
					Namespace: "ns",
				},
			}

			name := encryptionConfigSecretName(dc)
			Expect(name).To(HavePrefix("etcd-encryption-config-"))
			Expect(len(name)).To(BeNumerically("<", validation.DNS1123SubdomainMaxLength))
			Expect(validation.IsDNS1123Subdomain(name)).To(BeEmpty())
		})

		It("generates different shortened names for different clusters", func() {
			dc1 := &provisioningv1.DPUCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      strings.Repeat("a", validation.DNS1123SubdomainMaxLength),
					Namespace: "ns",
				},
			}
			dc2 := &provisioningv1.DPUCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      strings.Repeat("b", validation.DNS1123SubdomainMaxLength),
					Namespace: "ns",
				},
			}

			Expect(encryptionConfigSecretName(dc1)).NotTo(Equal(encryptionConfigSecretName(dc2)))
		})
	})

	Describe("patchDPUClusterEncryptionMetadata", func() {
		var (
			testNS  *corev1.Namespace
			handler *clusterHandler
			dc      *provisioningv1.DPUCluster
		)

		BeforeEach(func() {
			testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "test-etcd-encryption-metadata-"}}
			Expect(k8sClient.Create(ctx, testNS)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, testNS)

			handler = &clusterHandler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			dc = &provisioningv1.DPUCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: testNS.Name,
				},
				Spec: provisioningv1.DPUClusterSpec{
					Type:     string(provisioningv1.KamajiCluster),
					MaxNodes: 100,
				},
			}
			Expect(k8sClient.Create(ctx, dc)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, k8sClient, dc)
		})

		DescribeTable("records enabled encryption settings",
			func(provider operatorv1.EtcdEncryptionAtRestProvider) {
				secretName := encryptionConfigSecretName(dc)
				Expect(handler.patchDPUClusterEncryptionMetadata(ctx, dc, &encryptionPlan{
					enabled:          true,
					provider:         provider,
					configSecretName: secretName,
				})).To(Succeed())

				got := &provisioningv1.DPUCluster{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: dc.Name, Namespace: dc.Namespace}, got)).To(Succeed())
				Expect(got.Labels).To(HaveKeyWithValue(
					provisioningv1.DPUClusterEtcdEncryptionProviderLabelKey,
					string(provider),
				))
				Expect(got.Annotations).To(HaveKeyWithValue(
					provisioningv1.DPUClusterEtcdEncryptionConfigSecretAnnotationKey,
					secretName,
				))
			},
			Entry("for staticKey", operatorv1.EtcdEncryptionProviderStaticKey),
			Entry("for vaultKMS", operatorv1.EtcdEncryptionProviderVaultKMS),
		)

		It("does not add metadata when encryption is disabled", func() {
			Expect(handler.patchDPUClusterEncryptionMetadata(ctx, dc, &encryptionPlan{enabled: false})).To(Succeed())

			got := &provisioningv1.DPUCluster{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: dc.Name, Namespace: dc.Namespace}, got)).To(Succeed())
			Expect(got.Labels).NotTo(HaveKey(provisioningv1.DPUClusterEtcdEncryptionProviderLabelKey))
			Expect(got.Annotations).NotTo(HaveKey(provisioningv1.DPUClusterEtcdEncryptionConfigSecretAnnotationKey))
		})

		It("does not clean up committed metadata when encryption is disabled", func() {
			plan := &encryptionPlan{
				enabled:          true,
				provider:         operatorv1.EtcdEncryptionProviderVaultKMS,
				configSecretName: encryptionConfigSecretName(dc),
			}
			Expect(handler.patchDPUClusterEncryptionMetadata(ctx, dc, plan)).To(Succeed())
			Expect(handler.patchDPUClusterEncryptionMetadata(ctx, dc, &encryptionPlan{enabled: false})).To(Succeed())

			got := &provisioningv1.DPUCluster{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: dc.Name, Namespace: dc.Namespace}, got)).To(Succeed())
			Expect(got.Labels).To(HaveKeyWithValue(
				provisioningv1.DPUClusterEtcdEncryptionProviderLabelKey,
				string(plan.provider),
			))
			Expect(got.Annotations).To(HaveKeyWithValue(
				provisioningv1.DPUClusterEtcdEncryptionConfigSecretAnnotationKey,
				plan.configSecretName,
			))
		})
	})

	Describe("reconcileEncryptionConfig", func() {
		var (
			testNS  *corev1.Namespace
			handler *clusterHandler
			dc      *provisioningv1.DPUCluster
		)

		createOperatorConfig := func(ear *operatorv1.EtcdEncryptionAtRestConfiguration) *operatorv1.DPFOperatorConfig {
			cfg := &operatorv1.DPFOperatorConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "config",
					Namespace: testNS.Name,
				},
				Spec: operatorv1.DPFOperatorConfigSpec{
					DeploymentMode: operatorv1.DeploymentModeHostTrusted,
					ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
						BFBPersistentVolumeClaimName: ptr.To("pvc"),
					},
					KamajiClusterManager: &operatorv1.KamajiClusterManagerConfiguration{
						EtcdEncryptionAtRest: ear,
					},
				},
			}
			Expect(k8sClient.Create(ctx, cfg)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, k8sClient, cfg)
			return cfg
		}

		createStaticKeySecret := func() {
			key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x2a}, 32))
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "etcd-key",
					Namespace: testNS.Name,
				},
				Data: map[string][]byte{
					"key": []byte(key),
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, k8sClient, secret)
		}

		createExistingEncryptionSecret := func(annotations map[string]string, owner *provisioningv1.DPUCluster) *corev1.Secret {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:        encryptionConfigSecretName(dc),
					Namespace:   testNS.Name,
					Annotations: annotations,
				},
				Data: map[string][]byte{
					encryptionConfigFileName: []byte("existing"),
				},
			}
			if owner != nil {
				Expect(controllerutil.SetOwnerReference(owner, secret, scheme.Scheme)).To(Succeed())
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, k8sClient, secret)
			return secret
		}

		BeforeEach(func() {
			testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "test-etcd-encryption-"}}
			Expect(k8sClient.Create(ctx, testNS)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, testNS)

			handler = &clusterHandler{
				Client: k8sClient,
				Scheme: scheme.Scheme,
			}

			dc = &provisioningv1.DPUCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: testNS.Name,
				},
				Spec: provisioningv1.DPUClusterSpec{
					Type:     string(provisioningv1.KamajiCluster),
					MaxNodes: 100,
				},
			}
			Expect(k8sClient.Create(ctx, dc)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, k8sClient, dc)
		})

		It("creates the encryption config Secret with the provider annotation", func() {
			createStaticKeySecret()
			createOperatorConfig(&operatorv1.EtcdEncryptionAtRestConfiguration{
				Provider: operatorv1.EtcdEncryptionProviderStaticKey,
				StaticKey: &operatorv1.StaticKeyConfiguration{
					KeySecretRef: operatorv1.SecretKeyRef{Name: "etcd-key", Key: "key"},
				},
			})

			plan, err := handler.reconcileEncryptionConfig(ctx, dc)
			Expect(err).NotTo(HaveOccurred())
			Expect(plan).To(Equal(&encryptionPlan{
				enabled:          true,
				provider:         operatorv1.EtcdEncryptionProviderStaticKey,
				configSecretName: encryptionConfigSecretName(dc),
			}))

			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      encryptionConfigSecretName(dc),
				Namespace: testNS.Name,
			}, secret)).To(Succeed())
			Expect(secret.Annotations).To(HaveKeyWithValue(encryptionConfigProviderAnnotation, string(operatorv1.EtcdEncryptionProviderStaticKey)))
			Expect(secret.Data).To(HaveKey(encryptionConfigFileName))
			Expect(hasOwnerReference(secret, dc)).To(BeTrue())
		})

		It("reuses an owned existing Secret as the committed provider decision", func() {
			createExistingEncryptionSecret(map[string]string{
				encryptionConfigProviderAnnotation: string(operatorv1.EtcdEncryptionProviderVaultKMS),
			}, dc)

			plan, err := handler.reconcileEncryptionConfig(ctx, dc)
			Expect(err).NotTo(HaveOccurred())
			Expect(plan).To(Equal(&encryptionPlan{
				enabled:          true,
				provider:         operatorv1.EtcdEncryptionProviderVaultKMS,
				configSecretName: encryptionConfigSecretName(dc),
			}))
		})

		It("records metadata from an existing committed Secret when creating the TenantControlPlane", func() {
			secret := createExistingEncryptionSecret(map[string]string{
				encryptionConfigProviderAnnotation: string(operatorv1.EtcdEncryptionProviderVaultKMS),
			}, dc)

			_, _, _, err := handler.reconcileKamaji(ctx, dc)
			Expect(err).NotTo(HaveOccurred())

			got := &provisioningv1.DPUCluster{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: dc.Name, Namespace: dc.Namespace}, got)).To(Succeed())
			Expect(got.Labels).To(HaveKeyWithValue(
				provisioningv1.DPUClusterEtcdEncryptionProviderLabelKey,
				string(operatorv1.EtcdEncryptionProviderVaultKMS),
			))
			Expect(got.Annotations).To(HaveKeyWithValue(
				provisioningv1.DPUClusterEtcdEncryptionConfigSecretAnnotationKey,
				secret.Name,
			))
		})

		It("rejects an owned existing Secret without a provider annotation", func() {
			createExistingEncryptionSecret(nil, dc)

			_, err := handler.reconcileEncryptionConfig(ctx, dc)
			Expect(err).To(MatchError(ContainSubstring("is missing \"operator.dpu.nvidia.com/etcd-encryption-provider\" annotation")))
		})

		It("rejects an owned existing Secret with an unsupported provider annotation", func() {
			createExistingEncryptionSecret(map[string]string{
				encryptionConfigProviderAnnotation: "unknown",
			}, dc)

			_, err := handler.reconcileEncryptionConfig(ctx, dc)
			Expect(err).To(MatchError(ContainSubstring("has unsupported provider annotation \"unknown\"")))
		})

		It("rejects an existing Secret that is not owned by the DPUCluster", func() {
			createExistingEncryptionSecret(map[string]string{
				encryptionConfigProviderAnnotation: string(operatorv1.EtcdEncryptionProviderStaticKey),
			}, nil)

			_, err := handler.reconcileEncryptionConfig(ctx, dc)
			Expect(err).To(MatchError(ContainSubstring("already exists and is not owned by DPUCluster")))
		})
	})

	Describe("renderStaticKeyEncryptionConfig", func() {
		It("renders an aesgcm provider with the inline key and an identity fallback", func() {
			out, err := renderStaticKeyEncryptionConfig("YWJjZA==")
			Expect(err).NotTo(HaveOccurred())

			ec := &apiserverv1.EncryptionConfiguration{}
			Expect(yaml.Unmarshal(out, ec)).To(Succeed())
			Expect(ec.Kind).To(Equal("EncryptionConfiguration"))
			Expect(ec.APIVersion).To(Equal("apiserver.config.k8s.io/v1"))
			Expect(ec.Resources).To(HaveLen(1))
			Expect(ec.Resources[0].Resources).To(Equal([]string{"secrets", "configmaps"}))
			Expect(ec.Resources[0].Providers).To(HaveLen(2))
			Expect(ec.Resources[0].Providers[0].AESGCM).NotTo(BeNil())
			Expect(ec.Resources[0].Providers[0].AESGCM.Keys).To(ConsistOf(apiserverv1.Key{Name: "key1", Secret: "YWJjZA=="}))
			Expect(ec.Resources[0].Providers[1].Identity).NotTo(BeNil())
		})
	})

	Describe("validateBase64StaticKeyText", func() {
		DescribeTable("accepts base64 key text whose decoded length is a valid AES key size",
			func(rawLen int) {
				base64Key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x2a}, rawLen))

				got, err := validateBase64StaticKeyText([]byte(base64Key))
				Expect(err).NotTo(HaveOccurred())
				Expect(got).To(Equal(base64Key))
			},
			Entry("16 bytes", 16),
			Entry("24 bytes", 24),
			Entry("32 bytes (e.g. openssl rand -base64 32)", 32),
		)

		It("trims surrounding whitespace and returns the normalized base64 string", func() {
			base64Key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x2a}, 32))

			got, err := validateBase64StaticKeyText([]byte("  " + base64Key + "\n"))
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(base64Key))
		})

		It("fails when the value is empty or whitespace only", func() {
			_, err := validateBase64StaticKeyText([]byte("   \n"))
			Expect(err).To(MatchError(ContainSubstring("key is empty")))
		})

		It("fails when the value is not valid base64", func() {
			_, err := validateBase64StaticKeyText([]byte("not-valid-base64!!!"))
			Expect(err).To(MatchError(ContainSubstring("key must be base64-encoded AES key text")))
		})

		It("rejects raw AES key bytes because the Secret value must be base64 text", func() {
			_, err := validateBase64StaticKeyText(bytes.Repeat([]byte{0x2a}, 32))
			Expect(err).To(MatchError(ContainSubstring("key must be base64-encoded AES key text")))
		})

		It("fails when the decoded key length is not 16, 24, or 32 bytes", func() {
			base64Key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x2a}, 10))

			_, err := validateBase64StaticKeyText([]byte(base64Key))
			Expect(err).To(MatchError(ContainSubstring("decoded key length must be 16, 24, or 32 bytes, got 10")))
		})

		It("rejects a value that was base64-encoded twice", func() {
			// openssl rand -base64 32 already yields base64 text; encoding it again decodes to the
			// 44-byte base64 string rather than the 32-byte key.
			doubleEncoded := base64.StdEncoding.EncodeToString(
				[]byte(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x2a}, 32))))

			_, err := validateBase64StaticKeyText([]byte(doubleEncoded))
			Expect(err).To(MatchError(ContainSubstring("decoded key length must be 16, 24, or 32 bytes, got 44")))
		})
	})

	Describe("renderVaultKMSEncryptionConfig", func() {
		It("renders a kms v2 provider pointing at the well-known socket", func() {
			out, err := renderVaultKMSEncryptionConfig()
			Expect(err).NotTo(HaveOccurred())

			ec := &apiserverv1.EncryptionConfiguration{}
			Expect(yaml.Unmarshal(out, ec)).To(Succeed())
			Expect(ec.Resources[0].Resources).To(Equal([]string{"secrets", "configmaps"}))
			Expect(ec.Resources[0].Providers).To(HaveLen(2))
			Expect(ec.Resources[0].Providers[0].KMS).NotTo(BeNil())
			Expect(ec.Resources[0].Providers[0].KMS.APIVersion).To(Equal("v2"))
			Expect(ec.Resources[0].Providers[0].KMS.Name).To(Equal(vaultKMSProviderName))
			Expect(ec.Resources[0].Providers[0].KMS.Endpoint).To(Equal("unix://" + config.DefaultSocketFile))
			Expect(ec.Resources[0].Providers[1].Identity).NotTo(BeNil())
		})
	})

	Describe("applyTCPEncryptionIfEnabled", func() {
		var tcp *kamajiv1.TenantControlPlane

		BeforeEach(func() {
			tcp = &kamajiv1.TenantControlPlane{}
		})

		It("does nothing when the plan is nil or disabled", func() {
			applyTCPEncryptionIfEnabled(tcp, nil)
			applyTCPEncryptionIfEnabled(tcp, &encryptionPlan{enabled: false})
			Expect(tcp.Spec.ControlPlane.Deployment.AdditionalVolumes).To(BeEmpty())
			Expect(tcp.Spec.ControlPlane.Deployment.ExtraArgs).To(BeNil())
		})

		It("mounts the config secret and sets the apiserver flag for staticKey", func() {
			applyTCPEncryptionIfEnabled(tcp, &encryptionPlan{
				enabled:          true,
				provider:         operatorv1.EtcdEncryptionProviderStaticKey,
				configSecretName: "etcd-encryption-config-cluster-1",
			})
			dep := tcp.Spec.ControlPlane.Deployment

			Expect(encVolumeNames(dep.AdditionalVolumes)).To(ConsistOf(encryptionConfigVolumeName))
			cfgVol := dep.AdditionalVolumes[0]
			Expect(cfgVol.Secret).NotTo(BeNil())
			Expect(cfgVol.Secret.SecretName).To(Equal("etcd-encryption-config-cluster-1"))

			Expect(dep.AdditionalVolumeMounts).NotTo(BeNil())
			Expect(dep.AdditionalVolumeMounts.APIServer).To(HaveLen(1))
			Expect(dep.AdditionalVolumeMounts.APIServer[0].MountPath).To(Equal(encryptionConfigMountPath))

			Expect(dep.ExtraArgs.APIServer).To(ContainElement("--encryption-provider-config=/etc/kubernetes/encryption/encryption-config.yaml"))
		})

		It("adds the hostPath socket volume for vaultKMS", func() {
			applyTCPEncryptionIfEnabled(tcp, &encryptionPlan{
				enabled:          true,
				provider:         operatorv1.EtcdEncryptionProviderVaultKMS,
				configSecretName: "etcd-encryption-config-cluster-1",
			})
			dep := tcp.Spec.ControlPlane.Deployment

			Expect(encVolumeNames(dep.AdditionalVolumes)).To(ConsistOf(encryptionConfigVolumeName, vaultKMSSocketVolumeName))

			var socket *corev1.Volume
			for i := range dep.AdditionalVolumes {
				if dep.AdditionalVolumes[i].Name == vaultKMSSocketVolumeName {
					socket = &dep.AdditionalVolumes[i]
				}
			}
			Expect(socket).NotTo(BeNil())
			Expect(socket.HostPath).NotTo(BeNil())
			Expect(socket.HostPath.Path).To(Equal(config.DefaultSocketDir))

			Expect(dep.AdditionalVolumeMounts.APIServer).To(HaveLen(2))
			Expect(dep.AdditionalVolumeMounts.APIServer[1].ReadOnly).To(BeTrue())
			Expect(dep.ExtraArgs.APIServer).To(ContainElement("--encryption-provider-config=/etc/kubernetes/encryption/encryption-config.yaml"))
		})
	})
})
