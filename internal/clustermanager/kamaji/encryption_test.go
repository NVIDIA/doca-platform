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
	"github.com/nvidia/doca-platform/internal/clustermanager/kamaji/encryptionconfig"
	"github.com/nvidia/doca-platform/internal/kmsplugin/config"
	testutils "github.com/nvidia/doca-platform/test/utils"
	kamajiv1 "github.com/nvidia/doca-platform/third_party/forked/github.com/clastix/kamaji/api/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
)

// testConfig is a minimal Config implementation for TCP wiring tests.
type testConfig struct {
	provider operatorv1.EtcdEncryptionAtRestProvider
	name     types.NamespacedName
}

// Provider returns the fake provider used by TCP wiring tests.
func (c testConfig) Provider() operatorv1.EtcdEncryptionAtRestProvider { return c.provider }

// NamespacedName returns the fake config Secret key used by TCP wiring tests.
func (c testConfig) NamespacedName() types.NamespacedName { return c.name }

// testStaticKeyConfig is a minimal StaticKey implementation for orchestration tests.
type testStaticKeyConfig struct {
	testConfig
	activeRef provisioningv1.ObservedSecretKeyRef
	phase     encryptionconfig.Phase
	hash      string
}

// Phase returns the fake persisted staticKey phase.
func (c testStaticKeyConfig) Phase() encryptionconfig.Phase {
	if c.phase == "" {
		return encryptionconfig.PhaseIdle
	}
	return c.phase
}

// ConfigHash returns the fake config hash used by reload tests.
func (c testStaticKeyConfig) ConfigHash() string {
	if c.hash == "" {
		return "sha256:test"
	}
	return c.hash
}

// ActiveKeyRef returns the fake active source Secret metadata.
func (c testStaticKeyConfig) ActiveKeyRef() provisioningv1.ObservedSecretKeyRef {
	return c.activeRef
}

// TransitionToPrepared returns the fake config unchanged.
func (c testStaticKeyConfig) TransitionToPrepared(encryptionconfig.SourceKey) (encryptionconfig.StaticKey, error) {
	return c, nil
}

// TransitionToPromoted returns the fake config unchanged.
func (c testStaticKeyConfig) TransitionToPromoted() (encryptionconfig.StaticKey, error) {
	return c, nil
}

// RotationID returns a stable fake rotation ID.
func (c testStaticKeyConfig) RotationID() (string, error) { return "rotation", nil }

// TransitionToFinalized returns the fake config unchanged.
func (c testStaticKeyConfig) TransitionToFinalized() (encryptionconfig.StaticKey, error) {
	return c, nil
}

// TransitionToIdle returns the fake config unchanged.
func (c testStaticKeyConfig) TransitionToIdle() (encryptionconfig.StaticKey, error) {
	return c, nil
}

// encVolumeNames extracts volume names for concise assertions.
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
			Expect(encryptionConfigSecretName(dc)).To(Equal(types.NamespacedName{Name: "etcd-encryption-config-cluster-1", Namespace: "ns"}))
		})

		It("shortens names that exceed the DNS subdomain limit", func() {
			dc := &provisioningv1.DPUCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      strings.Repeat("a", validation.DNS1123SubdomainMaxLength),
					Namespace: "ns",
				},
			}

			name := encryptionConfigSecretName(dc)
			Expect(name.Name).To(HavePrefix("etcd-encryption-config-"))
			Expect(len(name.Name)).To(BeNumerically("<", validation.DNS1123SubdomainMaxLength))
			Expect(validation.IsDNS1123Subdomain(name.Name)).To(BeEmpty())
		})

		It("generates different shortened names for different clusters", func() {
			dc1 := &provisioningv1.DPUCluster{ObjectMeta: metav1.ObjectMeta{Name: strings.Repeat("a", validation.DNS1123SubdomainMaxLength), Namespace: "ns"}}
			dc2 := &provisioningv1.DPUCluster{ObjectMeta: metav1.ObjectMeta{Name: strings.Repeat("b", validation.DNS1123SubdomainMaxLength), Namespace: "ns"}}

			Expect(encryptionConfigSecretName(dc1).Name).NotTo(Equal(encryptionConfigSecretName(dc2).Name))
		})
	})

	Describe("patchDPUClusterEncryptionMetadata", func() {
		It("records provider and staticKey active metadata", func() {
			activeRef := provisioningv1.ObservedSecretKeyRef{Name: "etcd-key", Key: "key", Namespace: "ns", UID: "uid", ResourceVersion: "1"}
			dc := &provisioningv1.DPUCluster{}

			(&clusterHandler{}).patchDPUClusterEncryptionMetadata(dc, &encryptionPlan{
				enabled: true,
				config: testStaticKeyConfig{
					testConfig: testConfig{
						provider: operatorv1.EtcdEncryptionProviderStaticKey,
						name:     types.NamespacedName{Name: "config", Namespace: "ns"},
					},
					activeRef: activeRef,
				},
			})

			Expect(dc.Status.EtcdEncryptionAtRest.Provider).To(Equal(string(operatorv1.EtcdEncryptionProviderStaticKey)))
			Expect(dc.Status.EtcdEncryptionAtRest.StaticKey.ActiveKeyRef).To(Equal(&activeRef))
		})

		It("does not add status when encryption is disabled", func() {
			dc := &provisioningv1.DPUCluster{}

			(&clusterHandler{}).patchDPUClusterEncryptionMetadata(dc, &encryptionPlan{enabled: false})

			Expect(dc.Status.EtcdEncryptionAtRest).To(BeNil())
		})
	})

	Describe("reconcileEncryptionConfig", func() {
		var (
			testNS  *corev1.Namespace
			handler *clusterHandler
			dc      *provisioningv1.DPUCluster
		)

		createOperatorConfig := func(ear *operatorv1.EtcdEncryptionAtRestConfiguration) {
			cfg := &operatorv1.DPFOperatorConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "config", Namespace: testNS.Name},
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
		}

		createStaticKeySecret := func() *corev1.Secret {
			key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x2a}, 32))
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "etcd-key", Namespace: testNS.Name},
				Data:       map[string][]byte{"key": []byte(key)},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, k8sClient, secret)
			return secret
		}

		BeforeEach(func() {
			testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "test-etcd-encryption-"}}
			Expect(k8sClient.Create(ctx, testNS)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, testNS)

			handler = &clusterHandler{Client: k8sClient, Scheme: scheme.Scheme}
			dc = &provisioningv1.DPUCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: testNS.Name},
				Spec:       provisioningv1.DPUClusterSpec{Type: string(provisioningv1.KamajiCluster), MaxNodes: 100},
			}
			Expect(k8sClient.Create(ctx, dc)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, k8sClient, dc)
		})

		It("creates a staticKey encryption config through the wrapper store", func() {
			sourceSecret := createStaticKeySecret()
			createOperatorConfig(&operatorv1.EtcdEncryptionAtRestConfiguration{
				Provider: operatorv1.EtcdEncryptionProviderStaticKey,
				StaticKey: &operatorv1.StaticKeyConfiguration{
					KeySecretRef: operatorv1.SecretKeyRef{Name: "etcd-key", Key: "key"},
				},
			})

			plan, err := handler.reconcileEncryptionConfig(ctx, dc)
			Expect(err).NotTo(HaveOccurred())
			Expect(plan.enabled).To(BeTrue())
			Expect(plan.config.Provider()).To(Equal(operatorv1.EtcdEncryptionProviderStaticKey))
			staticKey := plan.config.(encryptionconfig.StaticKey)
			Expect(staticKey.ActiveKeyRef()).To(Equal(observedSecretKeyRef(sourceSecret, "key")))
		})

		It("reuses an owned existing Secret as the committed provider decision", func() {
			store := encryptionconfig.NewStore(k8sClient, scheme.Scheme)
			_, err := store.InitializeVaultKMS(ctx, encryptionConfigSecretName(dc), dc, nil)
			Expect(err).NotTo(HaveOccurred())

			plan, err := handler.reconcileEncryptionConfig(ctx, dc)

			Expect(err).NotTo(HaveOccurred())
			Expect(plan.config.Provider()).To(Equal(operatorv1.EtcdEncryptionProviderVaultKMS))
		})

		It("restores activeKeyRef from an existing staticKey Secret when creating the TenantControlPlane", func() {
			sourceSecret := createStaticKeySecret()
			store := encryptionconfig.NewStore(k8sClient, scheme.Scheme)
			keyBytes := sourceSecret.Data["key"]
			_, err := store.InitializeStaticKey(ctx, encryptionConfigSecretName(dc), dc, nil, encryptionconfig.SourceKey{
				Key: keyBytes,
				Ref: observedSecretKeyRef(sourceSecret, "key"),
			})
			Expect(err).NotTo(HaveOccurred())

			_, _, _, err = handler.reconcileKamaji(ctx, dc)
			Expect(err).NotTo(HaveOccurred())

			Expect(dc.Status.EtcdEncryptionAtRest.Provider).To(Equal(string(operatorv1.EtcdEncryptionProviderStaticKey)))
			Expect(dc.Status.EtcdEncryptionAtRest.StaticKey.ActiveKeyRef).To(Equal(ptr.To(observedSecretKeyRef(sourceSecret, "key"))))
		})

		It("rejects an existing Secret that is not owned by the DPUCluster", func() {
			secretKey := encryptionConfigSecretName(dc)
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:        secretKey.Name,
					Namespace:   secretKey.Namespace,
					Annotations: map[string]string{encryptionconfig.ProviderAnnotation: string(operatorv1.EtcdEncryptionProviderVaultKMS)},
				},
				Data: map[string][]byte{encryptionconfig.ConfigFileName: []byte("invalid")},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, k8sClient, secret)

			_, err := handler.reconcileEncryptionConfig(ctx, dc)

			Expect(err).To(MatchError(ContainSubstring("is not owned by DPUCluster")))
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
				enabled: true,
				config: testStaticKeyConfig{testConfig: testConfig{
					provider: operatorv1.EtcdEncryptionProviderStaticKey,
					name:     types.NamespacedName{Name: "etcd-encryption-config-cluster-1", Namespace: "ns"},
				}},
			})
			dep := tcp.Spec.ControlPlane.Deployment

			Expect(encVolumeNames(dep.AdditionalVolumes)).To(ConsistOf(encryptionConfigVolumeName))
			Expect(dep.AdditionalVolumes[0].Secret.SecretName).To(Equal("etcd-encryption-config-cluster-1"))
			Expect(dep.AdditionalVolumeMounts.APIServer[0].MountPath).To(Equal(encryptionConfigMountPath))
			Expect(dep.ExtraArgs.APIServer).To(ContainElement("--encryption-provider-config=/etc/kubernetes/encryption/encryption-config.yaml"))
			Expect(dep.ExtraArgs.APIServer).To(ContainElement(encryptionProviderConfigAutomaticReloadFlag))
			Expect(dep.ExtraArgs.APIServer).To(ContainElement(storageVersionMigrationRuntimeConfigFlag))
			Expect(dep.ExtraArgs.APIServer).To(ContainElement(storageVersionMigratorFeatureGateFlag))
			Expect(dep.ExtraArgs.ControllerManager).To(ContainElement(storageVersionMigratorFeatureGateFlag))
		})

		It("adds the hostPath socket volume for a validated vaultKMS wrapper", func() {
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "test-vault-kms-"}}
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, ns)
			dc := &provisioningv1.DPUCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: ns.Name},
				Spec:       provisioningv1.DPUClusterSpec{Type: string(provisioningv1.KamajiCluster), MaxNodes: 100},
			}
			Expect(k8sClient.Create(ctx, dc)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, k8sClient, dc)
			store := encryptionconfig.NewStore(k8sClient, scheme.Scheme)
			vault, err := store.InitializeVaultKMS(ctx, types.NamespacedName{Name: "vault-config", Namespace: ns.Name}, dc, nil)
			Expect(err).NotTo(HaveOccurred())

			applyTCPEncryptionIfEnabled(tcp, &encryptionPlan{enabled: true, config: vault})
			dep := tcp.Spec.ControlPlane.Deployment

			Expect(encVolumeNames(dep.AdditionalVolumes)).To(ConsistOf(encryptionConfigVolumeName, vaultKMSSocketVolumeName))
			Expect(dep.AdditionalVolumes[1].HostPath.Path).To(Equal(config.DefaultSocketDir))
			Expect(dep.ExtraArgs.APIServer).To(ContainElement("--encryption-provider-config=/etc/kubernetes/encryption/encryption-config.yaml"))
		})
	})
})
