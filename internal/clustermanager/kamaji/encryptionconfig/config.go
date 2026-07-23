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

package encryptionconfig

import (
	"crypto/sha256"
	"fmt"
	"slices"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	apiserverv1 "k8s.io/apiserver/pkg/apis/apiserver/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

// encryptedResources lists the resources covered by the rendered EncryptionConfiguration.
var encryptedResources = []string{corev1.ResourceSecrets.String(), corev1.ResourceConfigMaps.String()}

// EncryptedResources returns the resources covered by the rendered EncryptionConfiguration.
func EncryptedResources() []string {
	return slices.Clone(encryptedResources)
}

// baseConfig provides the shared provider-neutral Config implementation.
type baseConfig struct {
	secret *corev1.Secret
}

// Provider returns the provider declared by the Secret annotation.
func (c *baseConfig) Provider() operatorv1.EtcdEncryptionAtRestProvider {
	if c == nil || c.secret == nil {
		return ""
	}
	return operatorv1.EtcdEncryptionAtRestProvider(c.secret.Annotations[ProviderAnnotation])
}

// NamespacedName returns the wrapped Secret's key.
func (c *baseConfig) NamespacedName() types.NamespacedName {
	if c == nil || c.secret == nil {
		return types.NamespacedName{}
	}
	return client.ObjectKeyFromObject(c.secret)
}

// parse dispatches a Secret to the provider-specific parser using the provider annotation.
func parse(secret *corev1.Secret) (Config, error) {
	if secret == nil {
		return nil, validationErrorf("encryption config Secret is nil")
	}
	provider := operatorv1.EtcdEncryptionAtRestProvider(secret.Annotations[ProviderAnnotation])
	switch provider {
	case operatorv1.EtcdEncryptionProviderStaticKey:
		return parseStaticKey(secret)
	case operatorv1.EtcdEncryptionProviderVaultKMS:
		return parseVaultKMS(secret)
	case "":
		return nil, validationErrorf("encryption config secret %s/%s is missing provider annotation %q",
			secret.Namespace, secret.Name, ProviderAnnotation)
	default:
		return nil, validationErrorf("encryption config secret %s/%s has unsupported provider annotation %q",
			secret.Namespace, secret.Name, provider)
	}
}

// parseEncryptionConfiguration validates and returns the canonical Kubernetes encryption config.
func parseEncryptionConfiguration(secret *corev1.Secret) (*apiserverv1.EncryptionConfiguration, []byte, error) {
	raw, ok := secret.Data[ConfigFileName]
	if !ok || len(raw) == 0 {
		return nil, nil, validationErrorf("encryption config secret %s/%s is missing %q", secret.Namespace, secret.Name, ConfigFileName)
	}
	cfg := &apiserverv1.EncryptionConfiguration{}
	if err := yaml.Unmarshal(raw, cfg); err != nil {
		return nil, nil, validationErrorf("parse encryption config secret %s/%s: %v", secret.Namespace, secret.Name, err)
	}
	if cfg.APIVersion != apiserverv1.SchemeGroupVersion.String() {
		return nil, nil, validationErrorf("encryption config secret %s/%s has unsupported apiVersion %q",
			secret.Namespace, secret.Name, cfg.APIVersion)
	}
	if cfg.Kind != "EncryptionConfiguration" {
		return nil, nil, validationErrorf("encryption config secret %s/%s has unsupported kind %q",
			secret.Namespace, secret.Name, cfg.Kind)
	}
	if len(cfg.Resources) != 1 {
		return nil, nil, validationErrorf("expected one resource configuration, got %d", len(cfg.Resources))
	}
	resourceConfig := cfg.Resources[0]
	if !slices.Equal(resourceConfig.Resources, encryptedResources) {
		return nil, nil, validationErrorf("expected encrypted resources %v, got %v", encryptedResources, resourceConfig.Resources)
	}
	if len(resourceConfig.Providers) != 2 {
		return nil, nil, validationErrorf("expected exactly two providers, got %d", len(resourceConfig.Providers))
	}
	if count := configuredProviderCount(resourceConfig.Providers[0]); count != 1 {
		return nil, nil, validationErrorf("expected primary provider to configure exactly one provider kind, got %d", count)
	}
	if resourceConfig.Providers[1].Identity == nil || configuredProviderCount(resourceConfig.Providers[1]) != 1 {
		return nil, nil, validationErrorf("expected identity fallback provider")
	}
	return cfg, raw, nil
}

// configuredProviderCount returns the number of provider kinds configured in one entry.
func configuredProviderCount(provider apiserverv1.ProviderConfiguration) int {
	count := 0
	if provider.AESGCM != nil {
		count++
	}
	if provider.AESCBC != nil {
		count++
	}
	if provider.Secretbox != nil {
		count++
	}
	if provider.Identity != nil {
		count++
	}
	if provider.KMS != nil {
		count++
	}
	return count
}

// renderEncryptionConfiguration renders the canonical provider plus identity fallback config.
func renderEncryptionConfiguration(provider apiserverv1.ProviderConfiguration) ([]byte, error) {
	return yaml.Marshal(&apiserverv1.EncryptionConfiguration{
		TypeMeta: metav1.TypeMeta{
			APIVersion: apiserverv1.SchemeGroupVersion.String(),
			Kind:       "EncryptionConfiguration",
		},
		Resources: []apiserverv1.ResourceConfiguration{
			{
				Resources: encryptedResources,
				Providers: []apiserverv1.ProviderConfiguration{
					provider,
					{Identity: &apiserverv1.IdentityConfiguration{}},
				},
			},
		},
	})
}

// configHash returns the kube-apiserver hash label value for the rendered config bytes.
func configHash(raw []byte) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256(raw))
}
