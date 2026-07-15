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
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/digest"
	"github.com/nvidia/doca-platform/internal/kmsplugin/config"
	"github.com/nvidia/doca-platform/internal/utils"
	kamajiv1 "github.com/nvidia/doca-platform/third_party/forked/github.com/clastix/kamaji/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	apiserverv1 "k8s.io/apiserver/pkg/apis/apiserver/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/yaml"
)

const (
	// encryptionConfigMountPath is the in-pod directory holding the rendered EncryptionConfiguration.
	encryptionConfigMountPath = "/etc/kubernetes/encryption"
	// encryptionConfigFileName is the file name of the rendered EncryptionConfiguration.
	encryptionConfigFileName = "encryption-config.yaml"
	// encryptionConfigVolumeName is the apiserver volume holding the rendered EncryptionConfiguration.
	encryptionConfigVolumeName = "etcd-encryption-config"
	// encryptionConfigSecretNamePrefix is prepended to the DPUCluster name for the Secret holding
	// the rendered EncryptionConfiguration.
	encryptionConfigSecretNamePrefix = "etcd-encryption-config"
	// encryptionConfigProviderAnnotation records the provider used to render the immutable
	// per-cluster EncryptionConfiguration.
	encryptionConfigProviderAnnotation = "operator.dpu.nvidia.com/etcd-encryption-provider"
	// encryptionProviderConfigFlagPrefix is the kube-apiserver flag enabling encryption at rest.
	encryptionProviderConfigFlagPrefix = "--encryption-provider-config="
	// vaultKMSProviderName is the KMS v2 provider name referenced by the EncryptionConfiguration.
	vaultKMSProviderName = "vault-kms-plugin"
	// vaultKMSSocketVolumeName is the apiserver volume mounting the host path KMS socket directory.
	vaultKMSSocketVolumeName = "vault-kms-socket"
)

// encryptionPlan captures the encryption-at-rest decision taken when a Kamaji cluster is created.
type encryptionPlan struct {
	enabled          bool
	provider         operatorv1.EtcdEncryptionAtRestProvider
	configSecretName string
}

// encryptionConfigSecretName returns the deterministic per-cluster name of the Secret holding the
// rendered EncryptionConfiguration.
func encryptionConfigSecretName(dc *provisioningv1.DPUCluster) string {
	tcpName := kamajiTCPName(dc).Name
	name := fmt.Sprintf("%s-%s", encryptionConfigSecretNamePrefix, tcpName)
	if len(name) > validation.DNS1123SubdomainMaxLength {
		name = fmt.Sprintf("%s-%s", encryptionConfigSecretNamePrefix, digest.Short(digest.FromObjects(tcpName), 64))
	}
	return name
}

// reconcileEncryptionConfig builds the encryption-at-rest plan for a new Kamaji cluster. If a
// per-cluster Secret already exists, its provider annotation is treated as the committed
// creation-time decision. Otherwise, the current DPFOperatorConfig is used to render and create the
// Secret.
func (cm *clusterHandler) reconcileEncryptionConfig(ctx context.Context, dc *provisioningv1.DPUCluster) (*encryptionPlan, error) {
	nn := kamajiTCPName(dc)
	secretName := encryptionConfigSecretName(dc)

	existing := &corev1.Secret{}
	if err := cm.Client.Get(ctx, types.NamespacedName{Name: secretName, Namespace: nn.Namespace}, existing); err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("get encryption config secret: %w", err)
		}
	} else {
		return encryptionPlanFromExistingSecret(existing, dc)
	}

	cfg, err := utils.GetDPFOperatorConfig(ctx, cm.Client)
	if err != nil {
		return nil, fmt.Errorf("get DPFOperatorConfig for etcd encryption at rest: %w", err)
	}

	if cfg.Spec.KamajiClusterManager == nil || cfg.Spec.KamajiClusterManager.EtcdEncryptionAtRest == nil {
		return &encryptionPlan{enabled: false}, nil
	}

	ear := cfg.Spec.KamajiClusterManager.EtcdEncryptionAtRest

	var rendered []byte
	switch ear.Provider {
	case operatorv1.EtcdEncryptionProviderStaticKey:
		if ear.StaticKey == nil {
			return nil, fmt.Errorf("etcd encryption provider staticKey requires staticKey configuration")
		}
		keyBytes, err := cm.readSecretKey(ctx, cfg.Namespace, ear.StaticKey.KeySecretRef)
		if err != nil {
			return nil, err
		}
		base64Key, err := validateBase64StaticKeyText(keyBytes)
		if err != nil {
			return nil, fmt.Errorf("invalid static key in secret %s/%s for etcd encryption: %w",
				cfg.Namespace, ear.StaticKey.KeySecretRef.Name, err)
		}
		rendered, err = renderStaticKeyEncryptionConfig(base64Key)
		if err != nil {
			return nil, fmt.Errorf("failed to render static key encryption configuration: %w", err)
		}
	case operatorv1.EtcdEncryptionProviderVaultKMS:
		var err error
		rendered, err = renderVaultKMSEncryptionConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to render vault-kms encryption configuration: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported etcd encryption provider %q", ear.Provider)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: nn.Namespace,
			Annotations: map[string]string{
				encryptionConfigProviderAnnotation: string(ear.Provider),
			},
			Labels: map[string]string{
				"tenant.clastix.io":                   nn.Name,
				provisioningv1.DPUClusterNameLabelKey: dc.Name,
			},
		},
		Data: map[string][]byte{
			encryptionConfigFileName: rendered,
		},
	}
	if err := controllerutil.SetOwnerReference(dc, secret, cm.Scheme); err != nil {
		return nil, fmt.Errorf("failed to set owner reference on encryption config secret: %w", err)
	}

	if err := cm.Client.Create(ctx, secret); err != nil {
		return nil, fmt.Errorf("failed to create encryption config secret: %w", err)
	}

	return &encryptionPlan{enabled: true, provider: ear.Provider, configSecretName: secretName}, nil
}

// patchDPUClusterEncryptionMetadata records the encryption settings committed at cluster creation.
func (cm *clusterHandler) patchDPUClusterEncryptionMetadata(ctx context.Context, dc *provisioningv1.DPUCluster, plan *encryptionPlan) error {
	if plan == nil || !plan.enabled {
		return nil
	}

	provider := string(plan.provider)
	if dc.Labels[provisioningv1.DPUClusterEtcdEncryptionProviderLabelKey] == provider &&
		dc.Annotations[provisioningv1.DPUClusterEtcdEncryptionConfigSecretAnnotationKey] == plan.configSecretName {
		return nil
	}

	original := dc.DeepCopy()
	if dc.Labels == nil {
		dc.Labels = map[string]string{}
	}
	if dc.Annotations == nil {
		dc.Annotations = map[string]string{}
	}
	dc.Labels[provisioningv1.DPUClusterEtcdEncryptionProviderLabelKey] = provider
	dc.Annotations[provisioningv1.DPUClusterEtcdEncryptionConfigSecretAnnotationKey] = plan.configSecretName

	if err := cm.Client.Patch(ctx, dc, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("patch DPUCluster encryption metadata: %w", err)
	}
	return nil
}

func encryptionPlanFromExistingSecret(secret *corev1.Secret, dc *provisioningv1.DPUCluster) (*encryptionPlan, error) {
	if !hasOwnerReference(secret, dc) {
		return nil, fmt.Errorf("encryption config secret %s/%s already exists and is not owned by DPUCluster %s/%s",
			secret.Namespace, secret.Name, dc.Namespace, dc.Name)
	}

	provider, ok := secret.Annotations[encryptionConfigProviderAnnotation]
	if !ok || provider == "" {
		return nil, fmt.Errorf("encryption config secret %s/%s is missing %q annotation",
			secret.Namespace, secret.Name, encryptionConfigProviderAnnotation)
	}

	switch operatorv1.EtcdEncryptionAtRestProvider(provider) {
	case operatorv1.EtcdEncryptionProviderStaticKey, operatorv1.EtcdEncryptionProviderVaultKMS:
		return &encryptionPlan{
			enabled:          true,
			provider:         operatorv1.EtcdEncryptionAtRestProvider(provider),
			configSecretName: secret.Name,
		}, nil
	default:
		return nil, fmt.Errorf("encryption config secret %s/%s has unsupported provider annotation %q",
			secret.Namespace, secret.Name, provider)
	}
}

func hasOwnerReference(obj metav1.Object, owner metav1.Object) bool {
	for _, ref := range obj.GetOwnerReferences() {
		if ref.UID == owner.GetUID() {
			return true
		}
	}
	return false
}

// readSecretKey reads a single key from a Secret in the given namespace.
func (cm *clusterHandler) readSecretKey(ctx context.Context, namespace string, ref operatorv1.SecretKeyRef) ([]byte, error) {
	secret := &corev1.Secret{}
	if err := cm.Client.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: namespace}, secret); err != nil {
		return nil, fmt.Errorf("failed to get secret %s/%s for etcd encryption: %w", namespace, ref.Name, err)
	}
	val, ok := secret.Data[ref.Key]
	if !ok {
		return nil, fmt.Errorf("key %q not found in secret %s/%s for etcd encryption", ref.Key, namespace, ref.Name)
	}
	return val, nil
}

// validateBase64StaticKeyText treats the Secret value as base64 key text, decodes it only to
// validate that it is a well-formed AES key whose length is 16, 24, or 32 bytes, and returns the
// normalized base64 string to render into the EncryptionConfiguration. The value is not re-encoded.
func validateBase64StaticKeyText(value []byte) (string, error) {
	base64Key := strings.TrimSpace(string(value))
	if base64Key == "" {
		return "", fmt.Errorf("key is empty")
	}
	decoded, err := base64.StdEncoding.DecodeString(base64Key)
	if err != nil {
		return "", fmt.Errorf("key must be base64-encoded AES key text")
	}
	switch len(decoded) {
	case 16, 24, 32:
	default:
		return "", fmt.Errorf("decoded key length must be 16, 24, or 32 bytes, got %d", len(decoded))
	}
	return base64Key, nil
}

// applyTCPEncryptionIfEnabled wires the rendered EncryptionConfiguration into the
// TenantControlPlane apiserver when the plan has encryption enabled.
func applyTCPEncryptionIfEnabled(tcp *kamajiv1.TenantControlPlane, plan *encryptionPlan) {
	if plan == nil || !plan.enabled {
		return
	}

	dep := &tcp.Spec.ControlPlane.Deployment

	dep.AdditionalVolumes = append(dep.AdditionalVolumes, corev1.Volume{
		Name: encryptionConfigVolumeName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: plan.configSecretName,
			},
		},
	})

	if dep.AdditionalVolumeMounts == nil {
		dep.AdditionalVolumeMounts = &kamajiv1.AdditionalVolumeMounts{}
	}
	dep.AdditionalVolumeMounts.APIServer = append(dep.AdditionalVolumeMounts.APIServer, corev1.VolumeMount{
		Name:      encryptionConfigVolumeName,
		MountPath: encryptionConfigMountPath,
		ReadOnly:  true,
	})

	if plan.provider == operatorv1.EtcdEncryptionProviderVaultKMS {
		dep.AdditionalVolumes = append(dep.AdditionalVolumes, corev1.Volume{
			Name: vaultKMSSocketVolumeName,
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: config.DefaultSocketDir,
					Type: ptr.To(corev1.HostPathDirectoryOrCreate),
				},
			},
		})
		dep.AdditionalVolumeMounts.APIServer = append(dep.AdditionalVolumeMounts.APIServer, corev1.VolumeMount{
			Name:      vaultKMSSocketVolumeName,
			MountPath: config.DefaultSocketDir,
			ReadOnly:  true,
		})
	}

	if dep.ExtraArgs == nil {
		dep.ExtraArgs = &kamajiv1.ControlPlaneExtraArgs{}
	}
	dep.ExtraArgs.APIServer = append(dep.ExtraArgs.APIServer,
		fmt.Sprintf("%s%s/%s", encryptionProviderConfigFlagPrefix, encryptionConfigMountPath, encryptionConfigFileName))
}

// renderStaticKeyEncryptionConfig renders an EncryptionConfiguration with an inline AES-GCM key.
func renderStaticKeyEncryptionConfig(base64Key string) ([]byte, error) {
	return yaml.Marshal(newEncryptionConfiguration(apiserverv1.ProviderConfiguration{
		AESGCM: &apiserverv1.AESConfiguration{
			Keys: []apiserverv1.Key{{Name: "key1", Secret: base64Key}},
		},
	}))
}

// renderVaultKMSEncryptionConfig renders an EncryptionConfiguration pointing at the KMS v2 socket
// served by the vault-kms DaemonSet.
func renderVaultKMSEncryptionConfig() ([]byte, error) {
	return yaml.Marshal(newEncryptionConfiguration(apiserverv1.ProviderConfiguration{
		KMS: &apiserverv1.KMSConfiguration{
			APIVersion: "v2",
			Name:       vaultKMSProviderName,
			Endpoint:   "unix://" + config.DefaultSocketFile,
		},
	}))
}

// newEncryptionConfiguration builds a typed EncryptionConfiguration that encrypts the core
// secrets and configmaps resources with the supplied provider, always appending the identity
// provider so previously written, unencrypted data remains readable.
func newEncryptionConfiguration(provider apiserverv1.ProviderConfiguration) *apiserverv1.EncryptionConfiguration {
	return &apiserverv1.EncryptionConfiguration{
		TypeMeta: metav1.TypeMeta{
			APIVersion: apiserverv1.SchemeGroupVersion.String(),
			Kind:       "EncryptionConfiguration",
		},
		Resources: []apiserverv1.ResourceConfiguration{
			{
				Resources: []string{"secrets", "configmaps"},
				Providers: []apiserverv1.ProviderConfiguration{
					provider,
					{Identity: &apiserverv1.IdentityConfiguration{}},
				},
			},
		},
	}
}
