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
	"fmt"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/clustermanager/kamaji/encryptionconfig"
	"github.com/nvidia/doca-platform/internal/digest"
	"github.com/nvidia/doca-platform/internal/kmsplugin/config"
	"github.com/nvidia/doca-platform/internal/utils"
	kamajiv1 "github.com/nvidia/doca-platform/third_party/forked/github.com/clastix/kamaji/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/utils/ptr"
)

const (
	// encryptionConfigMountPath is the in-pod directory holding the rendered EncryptionConfiguration.
	encryptionConfigMountPath = "/etc/kubernetes/encryption"
	// encryptionConfigVolumeName is the apiserver volume holding the rendered EncryptionConfiguration.
	encryptionConfigVolumeName = "etcd-encryption-config"
	// encryptionConfigSecretNamePrefix is prepended to the DPUCluster name for the Secret holding
	// the rendered EncryptionConfiguration.
	encryptionConfigSecretNamePrefix = "etcd-encryption-config"
	// encryptionProviderConfigFlagPrefix is the kube-apiserver flag enabling encryption at rest.
	encryptionProviderConfigFlagPrefix = "--encryption-provider-config="
	// encryptionProviderConfigAutomaticReloadFlag enables kube-apiserver encryption config reload polling.
	encryptionProviderConfigAutomaticReloadFlag = "--encryption-provider-config-automatic-reload=true"
	// storageVersionMigrationRuntimeConfigFlag enables the StorageVersionMigration API in the tenant API server.
	storageVersionMigrationRuntimeConfigFlag = "--runtime-config=storagemigration.k8s.io/v1beta1=true"
	// storageVersionMigratorFeatureGateFlag enables in-tree storage version migration support.
	storageVersionMigratorFeatureGateFlag = "--feature-gates=StorageVersionMigrator=true"
	// vaultKMSSocketVolumeName is the apiserver volume mounting the host path KMS socket directory.
	vaultKMSSocketVolumeName = "vault-kms-socket"
)

// encryptionPlan captures the encryption-at-rest decision taken when a Kamaji cluster is created.
type encryptionPlan struct {
	enabled bool
	config  encryptionconfig.Config
}

// encryptionConfigSecretName returns the per-cluster Secret key holding the rendered EncryptionConfiguration.
func encryptionConfigSecretName(dc *provisioningv1.DPUCluster) types.NamespacedName {
	nn := kamajiTCPName(dc)
	tcpName := nn.Name
	name := fmt.Sprintf("%s-%s", encryptionConfigSecretNamePrefix, tcpName)
	if len(name) > validation.DNS1123SubdomainMaxLength {
		name = fmt.Sprintf("%s-%s", encryptionConfigSecretNamePrefix, digest.Short(digest.FromObjects(tcpName), 64))
	}
	return types.NamespacedName{Name: name, Namespace: nn.Namespace}
}

// reconcileEncryptionConfig builds the encryption-at-rest plan for a new Kamaji cluster. If a
// per-cluster Secret already exists, its rendered config is treated as the committed creation-time
// decision. Otherwise, the current DPFOperatorConfig is used to render and create the Secret.
func (cm *clusterHandler) reconcileEncryptionConfig(ctx context.Context, dc *provisioningv1.DPUCluster) (*encryptionPlan, error) {
	nn := kamajiTCPName(dc)
	secretKey := encryptionConfigSecretName(dc)
	encryptionConfigStore := encryptionconfig.NewStore(cm.Client, cm.Scheme)

	existing, err := encryptionConfigStore.Load(ctx, secretKey, dc)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("get encryption config secret: %w", err)
		}
	} else {
		return &encryptionPlan{enabled: true, config: existing}, nil
	}

	cfg, err := utils.GetDPFOperatorConfig(ctx, cm.Client)
	if err != nil {
		return nil, fmt.Errorf("get DPFOperatorConfig for etcd encryption at rest: %w", err)
	}

	if cfg.Spec.KamajiClusterManager == nil || cfg.Spec.KamajiClusterManager.EtcdEncryptionAtRest == nil {
		return &encryptionPlan{enabled: false}, nil
	}

	ear := cfg.Spec.KamajiClusterManager.EtcdEncryptionAtRest

	var config encryptionconfig.Config
	labels := map[string]string{
		"tenant.clastix.io":                   nn.Name,
		provisioningv1.DPUClusterNameLabelKey: dc.Name,
	}
	switch ear.Provider {
	case operatorv1.EtcdEncryptionProviderStaticKey:
		if ear.StaticKey == nil {
			return nil, fmt.Errorf("etcd encryption provider staticKey requires staticKey configuration")
		}
		sourceSecret, keyBytes, err := cm.readSecretKey(ctx, cfg.Namespace, ear.StaticKey.KeySecretRef)
		if err != nil {
			return nil, err
		}
		config, err = encryptionConfigStore.InitializeStaticKey(ctx, secretKey, dc, labels, encryptionconfig.SourceKey{
			Key: keyBytes,
			Ref: observedSecretKeyRef(sourceSecret, ear.StaticKey.KeySecretRef.Key),
		})
		if err != nil {
			return nil, fmt.Errorf("initialize staticKey encryption config: %w", err)
		}
	case operatorv1.EtcdEncryptionProviderVaultKMS:
		config, err = encryptionConfigStore.InitializeVaultKMS(ctx, secretKey, dc, labels)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported etcd encryption provider %q", ear.Provider)
	}

	return &encryptionPlan{enabled: true, config: config}, nil
}

// patchDPUClusterEncryptionMetadata records the encryption settings committed at cluster creation.
func (cm *clusterHandler) patchDPUClusterEncryptionMetadata(dc *provisioningv1.DPUCluster, plan *encryptionPlan) {
	if plan == nil || !plan.enabled {
		return
	}

	dc.Status.EtcdEncryptionAtRest = &provisioningv1.DPUClusterEtcdEncryptionAtRestStatus{
		Provider: string(plan.config.Provider()),
	}
	if staticKey, ok := plan.config.(encryptionconfig.StaticKey); ok {
		activeRef := staticKey.ActiveKeyRef()
		dc.Status.EtcdEncryptionAtRest.StaticKey = &provisioningv1.DPUClusterStaticKeyEncryptionStatus{
			ActiveKeyRef: &activeRef,
		}
	}
}

// readSecretKey reads a single key from a Secret in the given namespace.
func (cm *clusterHandler) readSecretKey(ctx context.Context, namespace string, ref operatorv1.SecretKeyRef) (*corev1.Secret, []byte, error) {
	secret := &corev1.Secret{}
	if err := cm.Client.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: namespace}, secret); err != nil {
		return nil, nil, fmt.Errorf("failed to get secret %s/%s for etcd encryption: %w", namespace, ref.Name, err)
	}
	val, ok := secret.Data[ref.Key]
	if !ok {
		return nil, nil, &secretKeyNotFoundError{namespace: namespace, name: ref.Name, key: ref.Key}
	}
	return secret, val, nil
}

// secretKeyNotFoundError reports a configured Secret that exists but misses the requested data key.
type secretKeyNotFoundError struct {
	namespace string
	name      string
	key       string
}

// Error returns the missing Secret key message.
func (e *secretKeyNotFoundError) Error() string {
	return fmt.Sprintf("key %q not found in secret %s/%s for etcd encryption", e.key, e.namespace, e.name)
}

// observedSecretKeyRef captures the observed source Secret version for status and rotation metadata.
func observedSecretKeyRef(secret *corev1.Secret, key string) provisioningv1.ObservedSecretKeyRef {
	if secret == nil {
		return provisioningv1.ObservedSecretKeyRef{}
	}
	return provisioningv1.ObservedSecretKeyRef{
		Name:            secret.Name,
		Key:             key,
		Namespace:       secret.Namespace,
		UID:             string(secret.UID),
		ResourceVersion: secret.ResourceVersion,
	}
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
				SecretName: plan.config.NamespacedName().Name,
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

	if plan.config.Provider() == operatorv1.EtcdEncryptionProviderVaultKMS {
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
		fmt.Sprintf("%s%s/%s", encryptionProviderConfigFlagPrefix, encryptionConfigMountPath, encryptionconfig.ConfigFileName))
	if plan.config.Provider() == operatorv1.EtcdEncryptionProviderStaticKey {
		dep.ExtraArgs.APIServer = append(dep.ExtraArgs.APIServer,
			encryptionProviderConfigAutomaticReloadFlag,
			storageVersionMigrationRuntimeConfigFlag,
			storageVersionMigratorFeatureGateFlag)
		dep.ExtraArgs.ControllerManager = append(dep.ExtraArgs.ControllerManager, storageVersionMigratorFeatureGateFlag)
	}
}
