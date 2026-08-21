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

package e2e

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"os"
	"strings"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

const (
	staticKeyEncryptionKeySize = 32

	vaultKMSOpenBaoAddress          = "https://openbao.openbao.svc.cluster.local:8200"
	vaultKMSTransitMount            = "transit"
	vaultKMSTransitKeyName          = "dpf-e2e-etcd"
	vaultKMSRoleName                = "dpf-e2e-vault-kms"
	vaultKMSServiceAccountName      = "dpf-kms-plugin"
	vaultKMSKubernetesAuthMountPath = "kubernetes"
)

// newStaticKeyValue generates and returns base64-encoded AES-256 key material.
func newStaticKeyValue() []byte {
	keyMaterial := make([]byte, staticKeyEncryptionKeySize)
	_, err := rand.Read(keyMaterial)
	Expect(err).NotTo(HaveOccurred())
	return []byte(base64.StdEncoding.EncodeToString(keyMaterial))
}

// etcdEncryptionAtRestConfigurationFromFile loads and returns an optional
// encryption-at-rest configuration.
func etcdEncryptionAtRestConfigurationFromFile(path *string) *operatorv1.EtcdEncryptionAtRestConfiguration {
	if path == nil {
		return nil
	}

	data, err := os.ReadFile(*path)
	Expect(err).NotTo(HaveOccurred(), "reading etcd encryption-at-rest configuration %s", *path)

	config := &operatorv1.EtcdEncryptionAtRestConfiguration{}
	Expect(yaml.UnmarshalStrict(data, config)).To(Succeed(), "parsing etcd encryption-at-rest configuration %s", *path)
	return config
}

// etcdEncryptionAtRestConfiguration returns the desired Kamaji encryption-at-rest configuration.
func etcdEncryptionAtRestConfiguration(config *operatorv1.DPFOperatorConfig) *operatorv1.EtcdEncryptionAtRestConfiguration {
	if config == nil || config.Spec.KamajiClusterManager == nil {
		return nil
	}
	return config.Spec.KamajiClusterManager.EtcdEncryptionAtRest
}

// configureGeneratedDPFOperatorConfigVaultKMS enables the Vault KMS component
// when generated Kamaji settings select the vaultKMS provider.
func configureGeneratedDPFOperatorConfigVaultKMS(config *operatorv1.DPFOperatorConfig) {
	encryptionAtRest := etcdEncryptionAtRestConfiguration(config)
	if encryptionAtRest == nil ||
		encryptionAtRest.Provider != operatorv1.EtcdEncryptionProviderVaultKMS {
		return
	}

	if config.Spec.Security == nil {
		config.Spec.Security = &operatorv1.SecurityConfiguration{}
	}
	config.Spec.Security.VaultKMS = &operatorv1.VaultKMSConfiguration{
		BaseComponentConfig: operatorv1.BaseComponentConfig{Disable: ptr.To(false)},
		Address:             vaultKMSOpenBaoAddress,
		TLS: &operatorv1.VaultKMSTLS{
			CACertConfigMapRef: &operatorv1.ConfigMapKeyRef{
				Name: openBaoCAConfigMapName,
				Key:  openBaoCAConfigMapKey,
			},
		},
		Transit: operatorv1.VaultKMSTransit{
			Mount:   ptr.To(vaultKMSTransitMount),
			KeyName: vaultKMSTransitKeyName,
		},
		Auth: operatorv1.VaultKMSAuth{
			Method: operatorv1.VaultKMSAuthMethodKubernetes,
			Kubernetes: &operatorv1.VaultKMSKubernetesAuth{
				Role:                vaultKMSRoleName,
				AuthEngineMountPath: ptr.To(vaultKMSKubernetesAuthMountPath),
			},
		},
	}
}

// createEtcdEncryptionAtRestPrerequisites creates provider prerequisites before
// Kamaji DPUClusters are created.
func createEtcdEncryptionAtRestPrerequisites(
	ctx context.Context,
	c client.Client,
	restClient *rest.RESTClient,
	restConfig *rest.Config,
	config *operatorv1.DPFOperatorConfig,
) {
	encryptionAtRest := etcdEncryptionAtRestConfiguration(config)
	if encryptionAtRest == nil {
		return
	}

	switch encryptionAtRest.Provider {
	case operatorv1.EtcdEncryptionProviderStaticKey:
		createStaticKeyEncryptionSecret(ctx, c, config, encryptionAtRest)
	case operatorv1.EtcdEncryptionProviderVaultKMS:
		createVaultKMSOpenBaoPrerequisites(ctx, c, restClient, restConfig, config)
	}
}

// createStaticKeyEncryptionSecret creates the source Secret for static key encryption.
func createStaticKeyEncryptionSecret(
	ctx context.Context,
	c client.Client,
	config *operatorv1.DPFOperatorConfig,
	encryptionAtRest *operatorv1.EtcdEncryptionAtRestConfiguration,
) {
	staticKey := encryptionAtRest.StaticKey
	Expect(staticKey).NotTo(BeNil())

	By("Creating the static key Secret for Kamaji encryption at rest")
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      staticKey.KeySecretRef.Name,
			Namespace: config.Namespace,
			Labels:    CleanupScope.Suite,
		},
		Data: map[string][]byte{
			staticKey.KeySecretRef.Key: newStaticKeyValue(),
		},
	}
	Expect(client.IgnoreAlreadyExists(c.Create(ctx, secret))).To(Succeed())
}

// createVaultKMSOpenBaoPrerequisites configures the Helmfile-provided OpenBao
// instance for the generated Vault KMS settings.
func createVaultKMSOpenBaoPrerequisites(
	ctx context.Context,
	c client.Client,
	restClient *rest.RESTClient,
	restConfig *rest.Config,
	config *operatorv1.DPFOperatorConfig,
) {
	Expect(config.Spec.Security).NotTo(BeNil())
	vaultKMS := config.Spec.Security.VaultKMS
	Expect(vaultKMS).NotTo(BeNil())
	Expect(strings.TrimRight(vaultKMS.Address, "/")).To(
		Equal(vaultKMSOpenBaoAddress),
		"Vault KMS E2E setup requires the Helmfile-provided OpenBao address",
	)
	Expect(vaultKMS.Auth.Method).To(
		Equal(operatorv1.VaultKMSAuthMethodKubernetes),
		"Vault KMS E2E setup requires Kubernetes authentication",
	)
	Expect(vaultKMS.Auth.Kubernetes).NotTo(BeNil())

	transitMount := strings.Trim(ptr.Deref(vaultKMS.Transit.Mount, vaultKMSTransitMount), "/")
	Expect(transitMount).NotTo(BeEmpty())
	Expect(vaultKMS.Transit.KeyName).NotTo(BeEmpty())
	Expect(vaultKMS.Auth.Kubernetes.Role).NotTo(BeEmpty())

	authMount := strings.Trim(
		ptr.Deref(vaultKMS.Auth.Kubernetes.AuthEngineMountPath, vaultKMSKubernetesAuthMountPath),
		"/",
	)
	Expect(authMount).To(Equal(vaultKMSKubernetesAuthMountPath))

	var openBao *openBaoClient
	Eventually(func(g Gomega) {
		var err error
		openBao, err = newOpenBaoClient(ctx, c, restClient, restConfig)
		g.Expect(err).NotTo(HaveOccurred())
	}).WithTimeout(openBaoOperationTimeout).
		WithPolling(time.Second).
		Should(Succeed())

	expectOpenBaoOperation(func() error {
		return openBao.enableTransit(transitMount)
	})
	expectOpenBaoOperation(func() error {
		return openBao.createTransitKey(transitMount, vaultKMS.Transit.KeyName)
	})
	expectOpenBaoOperation(func() error {
		return openBao.writeVaultKMSPolicy(vaultKMSRoleName, transitMount, vaultKMS.Transit.KeyName)
	})
	expectOpenBaoOperation(func() error {
		return openBao.writeKubernetesRole(
			vaultKMS.Auth.Kubernetes.Role,
			vaultKMSServiceAccountName,
			config.Namespace,
			vaultKMSRoleName,
		)
	})
}

// updateStaticKeySecretIfConfigured changes the desired static key to trigger rotation.
func updateStaticKeySecretIfConfigured(ctx context.Context, c client.Client, config *operatorv1.DPFOperatorConfig) {
	encryptionAtRest := etcdEncryptionAtRestConfiguration(config)
	if encryptionAtRest == nil ||
		encryptionAtRest.Provider != operatorv1.EtcdEncryptionProviderStaticKey {
		return
	}

	staticKey := encryptionAtRest.StaticKey
	Expect(staticKey).NotTo(BeNil())
	newKey := newStaticKeyValue()

	By("Updating the static key Secret to trigger rotation")
	Eventually(func(g Gomega) {
		secret := &corev1.Secret{}
		g.Expect(c.Get(ctx, client.ObjectKey{
			Namespace: config.Namespace,
			Name:      staticKey.KeySecretRef.Name,
		}, secret)).To(Succeed())
		if bytes.Equal(secret.Data[staticKey.KeySecretRef.Key], newKey) {
			return
		}
		original := secret.DeepCopy()
		if secret.Data == nil {
			secret.Data = map[string][]byte{}
		}
		secret.Data[staticKey.KeySecretRef.Key] = newKey
		g.Expect(c.Patch(ctx, secret, client.MergeFrom(original))).To(Succeed())
	}).WithTimeout(time.Minute).WithPolling(time.Second).Should(Succeed())
}

// expectDPUClusterEncryptionAtRest verifies the encryption configuration
// committed to a Kamaji DPUCluster when encryption at rest is configured.
func expectDPUClusterEncryptionAtRest(
	ctx context.Context,
	c client.Client,
	dpuCluster *provisioningv1.DPUCluster,
	config *operatorv1.DPFOperatorConfig,
	timeout time.Duration,
) {
	expected := etcdEncryptionAtRestConfiguration(config)
	Expect(dpuCluster.Spec.Type).To(Equal(string(provisioningv1.KamajiCluster)))
	if expected == nil {
		return
	}

	Eventually(func(g Gomega) {
		current := &provisioningv1.DPUCluster{}
		g.Expect(c.Get(ctx, client.ObjectKeyFromObject(dpuCluster), current)).To(Succeed())
		g.Expect(current.Status.EtcdEncryptionAtRest).NotTo(BeNil())
		g.Expect(current.Status.EtcdEncryptionAtRest.Provider).To(Equal(string(expected.Provider)))

		if expected.Provider != operatorv1.EtcdEncryptionProviderStaticKey {
			return
		}

		g.Expect(expected.StaticKey).NotTo(BeNil())
		sourceSecret := &corev1.Secret{}
		g.Expect(c.Get(ctx, client.ObjectKey{
			Namespace: config.Namespace,
			Name:      expected.StaticKey.KeySecretRef.Name,
		}, sourceSecret)).To(Succeed())

		g.Expect(current.Status.EtcdEncryptionAtRest.StaticKey).NotTo(BeNil())
		g.Expect(current.Status.EtcdEncryptionAtRest.StaticKey.ActiveKeyRef).To(Equal(
			&provisioningv1.ObservedSecretKeyRef{
				Name:            sourceSecret.Name,
				Key:             expected.StaticKey.KeySecretRef.Key,
				Namespace:       sourceSecret.Namespace,
				UID:             string(sourceSecret.UID),
				ResourceVersion: sourceSecret.ResourceVersion,
			},
		))

		inProgress := meta.FindStatusCondition(
			current.Status.Conditions,
			string(provisioningv1.ConditionEtcdEncryptionRotationInProgress),
		)
		g.Expect(inProgress).NotTo(BeNil())
		g.Expect(inProgress.Status).To(Equal(metav1.ConditionFalse))
		g.Expect(inProgress.Reason).To(Equal("Idle"))

		blocked := meta.FindStatusCondition(
			current.Status.Conditions,
			string(provisioningv1.ConditionEtcdEncryptionRotationBlocked),
		)
		g.Expect(blocked).NotTo(BeNil())
		g.Expect(blocked.Status).To(Equal(metav1.ConditionFalse))
	}).WithTimeout(timeout).WithPolling(time.Second).Should(Succeed())
}
