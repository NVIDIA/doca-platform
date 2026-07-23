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
	"strings"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/internal/kmsplugin/config"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	apiserverv1 "k8s.io/apiserver/pkg/apis/apiserver/v1"
)

const (
	// vaultKMSAPIVersion is the KMS plugin API version rendered into the config.
	vaultKMSAPIVersion = "v2"
	// vaultKMSProviderName is the KMS v2 provider name rendered into the config.
	vaultKMSProviderName = "vault-kms-plugin"
	// vaultKMSEndpoint is the KMS v2 Unix domain socket rendered into the config.
	vaultKMSEndpoint = "unix://" + config.DefaultSocketFile
)

// vaultKMSConfig wraps a validated VaultKMS encryption config Secret.
type vaultKMSConfig struct {
	baseConfig
}

// newVaultKMSConfig builds the initial VaultKMS config Secret wrapper.
func newVaultKMSConfig(key types.NamespacedName, labels map[string]string) (*vaultKMSConfig, error) {
	rendered, err := renderVaultKMSEncryptionConfig()
	if err != nil {
		return nil, err
	}
	secret := baseSecret(key, labels, operatorv1.EtcdEncryptionProviderVaultKMS)
	secret.Data[ConfigFileName] = rendered
	return parseVaultKMS(secret)
}

// parseVaultKMS validates a Secret as a VaultKMS config and returns its wrapper.
func parseVaultKMS(secret *corev1.Secret) (*vaultKMSConfig, error) {
	cfg, _, err := parseEncryptionConfiguration(secret)
	if err != nil {
		return nil, err
	}
	if hasStaticKeyMetadata(secret) {
		return nil, validationErrorf("vaultKMS encryption config secret %s/%s must not contain staticKey metadata",
			secret.Namespace, secret.Name)
	}
	kms := cfg.Resources[0].Providers[0].KMS
	if kms == nil {
		return nil, validationErrorf("provider annotation %q declares vaultKMS but first provider is not kms", ProviderAnnotation)
	}
	if kms.APIVersion != vaultKMSAPIVersion {
		return nil, validationErrorf("vaultKMS provider must use apiVersion %s, got %q", vaultKMSAPIVersion, kms.APIVersion)
	}
	if kms.Name != vaultKMSProviderName {
		return nil, validationErrorf("vaultKMS provider name must be %q, got %q", vaultKMSProviderName, kms.Name)
	}
	if kms.Endpoint != vaultKMSEndpoint {
		return nil, validationErrorf("vaultKMS provider endpoint must be %q, got %q", vaultKMSEndpoint, kms.Endpoint)
	}
	return &vaultKMSConfig{baseConfig: baseConfig{secret: secret.DeepCopy()}}, nil
}

// isVaultKMS seals the VaultKMS interface to this package.
func (c *vaultKMSConfig) isVaultKMS() {}

// renderVaultKMSEncryptionConfig renders the canonical KMS v2 provider config.
func renderVaultKMSEncryptionConfig() ([]byte, error) {
	return renderEncryptionConfiguration(apiserverv1.ProviderConfiguration{
		KMS: &apiserverv1.KMSConfiguration{
			APIVersion: vaultKMSAPIVersion,
			Name:       vaultKMSProviderName,
			Endpoint:   vaultKMSEndpoint,
		},
	})
}

// hasStaticKeyMetadata reports whether a Secret carries staticKey-only annotations.
func hasStaticKeyMetadata(secret *corev1.Secret) bool {
	for key := range secret.Annotations {
		if strings.HasPrefix(key, staticKeyAnnotationPrefix) {
			return true
		}
	}
	return false
}
