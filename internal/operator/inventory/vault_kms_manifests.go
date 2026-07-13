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

package inventory

import (
	"context"
	_ "embed"
	"fmt"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/internal/kmsplugin/config"
	"github.com/nvidia/doca-platform/internal/operator/utils"
	"github.com/nvidia/doca-platform/internal/release"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ Component = &vaultKMSObjects{}

const (
	vaultKMSContainerName = "vault-kms"
	vaultKMSDaemonSetName = "dpf-vault-kms"
	// vaultKMSCredentialDir is the in-pod directory under which selected credential keys are mounted.
	vaultKMSCredentialDir = "/etc/dpf/kms"
	// vaultKMSServiceAccountTokenDir is the in-pod directory holding the projected service account
	// token used by the Vault Kubernetes auth method.
	vaultKMSServiceAccountTokenDir = "/var/run/secrets/vault"
	// vaultKMSServiceAccountTokenExpiration is the requested lifetime of the projected token.
	vaultKMSServiceAccountTokenExpiration int64 = 3600
)

// vaultKMSObjects contains the objects used to generate the standalone Vault KMS plugin DaemonSet.
// vaultKMSObjects objects should be immutable after Parse().
type vaultKMSObjects struct {
	data    []byte
	objects []*unstructured.Unstructured
}

func (p *vaultKMSObjects) Name() operatorv1.ComponentName {
	return operatorv1.VaultKMSName
}

func (p *vaultKMSObjects) ImageName() string {
	return operatorv1.VaultKMSName.WithContainer(operatorv1.VaultKMSContainer)
}

// Parse returns typed objects for the Vault KMS plugin DaemonSet.
func (p *vaultKMSObjects) Parse() (err error) {
	if p.data == nil {
		return fmt.Errorf("vaultKMSObjects.data can not be empty")
	}
	objs, err := utils.BytesToUnstructured(p.data)
	if err != nil {
		return fmt.Errorf("error while converting Vault KMS manifests to objects: %w", err)
	} else if len(objs) == 0 {
		return fmt.Errorf("no objects found in Vault KMS manifests")
	}

	daemonsetFound := false
	for _, obj := range objs {
		if obj.GetKind() == string(NamespaceKind) || obj.GetKind() == string(CustomResourceDefinitionKind) {
			continue
		}
		if obj.GetKind() == string(DaemonSetKind) && obj.GetName() == vaultKMSDaemonSetName {
			daemonsetFound = true
		}
		p.objects = append(p.objects, obj)
	}

	if !daemonsetFound {
		return fmt.Errorf("error while converting Vault KMS manifests to objects: DaemonSet not found")
	}

	return nil
}

// GenerateManifests applies edits and returns objects.
func (p *vaultKMSObjects) GenerateManifests(_ context.Context, vars Variables) ([]client.Object, error) {
	if disabled := vars.DisableSystemComponents[p.Name()]; disabled {
		return []client.Object{}, nil
	}
	// The component can only be enabled through spec.security.vaultKMS, which also populates vars.VaultKMS.
	// If the configuration is absent there is nothing valid to deploy, so render nothing.
	if vars.VaultKMS == nil {
		return []client.Object{}, nil
	}

	labelsToAdd := map[string]string{
		operatorv1.DPFComponentLabelKey: p.Name().String(),
		release.DPFVersionLabelKey:      release.DPFVersion(),
		applysetPartOfLabel:             ApplySetID(vars.Namespace, p),
	}

	objsCopy := make([]*unstructured.Unstructured, 0, len(p.objects))
	for i := range p.objects {
		objsCopy = append(objsCopy, p.objects[i].DeepCopy())
	}

	containerImage, ok := vars.Images[p.ImageName()]
	if !ok {
		return nil, fmt.Errorf("could not find image for %s in variables", p.ImageName())
	}

	edits := NewEdits().
		AddForAll(NamespaceEdit(vars.Namespace)).
		AddForKindS(DaemonSetKind, ImagePullSecretsEditForDaemonSetEdit(vars.ImagePullSecrets...)).
		AddForKindS(DaemonSetKind, ImageForDaemonSetContainerEdit(vaultKMSContainerName, containerImage)).
		AddForKindS(DaemonSetKind, NodeAffinityEdit(&controlPlaneNodeAffinity)).
		AddForKindS(DaemonSetKind, TolerationsEdit(controlPlaneTolerations)).
		AddForKindS(DaemonSetKind, vaultKMSDaemonSetEdit(vars.VaultKMS)).
		AddForAll(LabelsEdit(labelsToAdd))

	if resources, exists := vars.Resources[p.ImageName()]; exists {
		if len(resources.Requests) > 0 || len(resources.Limits) > 0 {
			edits = edits.AddForKindS(DaemonSetKind, ResourcesEditForDaemonSet(vaultKMSContainerName, resources))
		}
	}

	if err := edits.Apply(objsCopy); err != nil {
		return nil, err
	}

	ret := []client.Object{}
	for i := range objsCopy {
		ret = append(ret, objsCopy[i])
	}

	return ret, nil
}

func (p *vaultKMSObjects) IsReadyForUpgrade(ctx context.Context, c client.Client, config *operatorv1.DPFOperatorConfig) error {
	shouldSkip, err := ShouldSkipUpgradeCheck(p.Name(), *config.Status.Version)
	if err != nil {
		return fmt.Errorf("determine if component %s should skip upgrade check: %w", p.Name(), err)
	}
	if shouldSkip {
		return nil
	}
	// Vault KMS provides a node-local KMS socket for control-plane API servers, so zero
	// scheduled pods means there is no socket provider anywhere.
	return daemonSetReadyCheckWithScheduledPods(ctx, c, config.GetNamespace(), p.objects, false)
}

func (p *vaultKMSObjects) IsReady(ctx context.Context, c client.Client, namespace string) error {
	// Vault KMS provides a node-local KMS socket for control-plane API servers, so zero
	// scheduled pods means there is no socket provider anywhere.
	return daemonSetReadyCheckWithScheduledPods(ctx, c, namespace, p.objects, true)
}

// vaultKMSDaemonSetEdit injects the command-line flags, credential volumes and mounts derived from
// the VaultKMSConfiguration into the plugin DaemonSet.
func vaultKMSDaemonSetEdit(cfg *operatorv1.VaultKMSConfiguration) StructuredEdit {
	return func(obj client.Object) error {
		ds, ok := obj.(*appsv1.DaemonSet)
		if !ok {
			return fmt.Errorf("unexpected object %s. expected DaemonSet", obj.GetObjectKind().GroupVersionKind())
		}

		c, err := vaultKMSContainer(ds)
		if err != nil {
			return err
		}

		builder := vaultKMSDaemonSetBuilder{cfg: cfg}
		if err := builder.build(); err != nil {
			return err
		}

		c.Args = append(c.Args, builder.args...)
		ds.Spec.Template.Spec.Volumes = append(ds.Spec.Template.Spec.Volumes, builder.volumes...)
		c.VolumeMounts = append(c.VolumeMounts, builder.mounts...)

		return nil
	}
}

type vaultKMSDaemonSetBuilder struct {
	cfg     *operatorv1.VaultKMSConfiguration
	args    []string
	volumes []corev1.Volume
	mounts  []corev1.VolumeMount
}

func (b *vaultKMSDaemonSetBuilder) build() error {
	if err := b.addBaseFlags(); err != nil {
		return err
	}
	b.addTLS()
	return b.addAuth()
}

func (b *vaultKMSDaemonSetBuilder) addBaseFlags() error {
	// Transit.Mount is defaulted by the CRD; do not re-default here. Error if it is unexpectedly empty.
	if b.cfg.Transit.Mount == nil || *b.cfg.Transit.Mount == "" {
		return fmt.Errorf("vault-kms transit.mount must be set")
	}

	b.args = append(b.args,
		vaultKMSFlag(config.FlagVaultAddress, b.cfg.Address),
		vaultKMSFlag(config.FlagSocketPath, config.DefaultSocketFile),
		vaultKMSFlag(config.FlagKeyName, b.cfg.Transit.KeyName),
		vaultKMSFlag(config.FlagTransitMount, *b.cfg.Transit.Mount),
		vaultKMSFlag(config.FlagAuthMethod, string(b.cfg.Auth.Method)),
	)
	if b.cfg.TokenCheckIntervalSeconds != nil {
		b.args = append(b.args, vaultKMSFlag(config.FlagTokenCheckInterval, fmt.Sprintf("%ds", *b.cfg.TokenCheckIntervalSeconds)))
	}
	if b.cfg.LoginTimeoutSeconds != nil {
		b.args = append(b.args, vaultKMSFlag(config.FlagLoginTimeout, fmt.Sprintf("%ds", *b.cfg.LoginTimeoutSeconds)))
	}
	if b.cfg.Namespace != nil && *b.cfg.Namespace != "" {
		b.args = append(b.args, vaultKMSFlag(config.FlagVaultNamespace, *b.cfg.Namespace))
	}

	return nil
}

func (b *vaultKMSDaemonSetBuilder) addTLS() {
	if b.cfg.TLS == nil || b.cfg.TLS.CACertConfigMapRef == nil {
		return
	}

	const caVolume = "vault-kms-ca"
	caDir := vaultKMSCredentialDir + "/tls"
	caRef := b.cfg.TLS.CACertConfigMapRef

	b.addConfigMapItems(caVolume, caRef.Name, caDir, corev1.KeyToPath{Key: caRef.Key, Path: "ca.crt"})
	b.args = append(b.args, vaultKMSFlag(config.FlagVaultCACert, caDir+"/ca.crt"))
}

func (b *vaultKMSDaemonSetBuilder) addAuth() error {
	switch b.cfg.Auth.Method {
	case operatorv1.VaultKMSAuthMethodToken:
		return b.addTokenAuth()
	case operatorv1.VaultKMSAuthMethodAppRole:
		return b.addAppRoleAuth()
	case operatorv1.VaultKMSAuthMethodUserpass:
		return b.addUserpassAuth()
	case operatorv1.VaultKMSAuthMethodKubernetes:
		return b.addKubernetesAuth()
	case operatorv1.VaultKMSAuthMethodJWT:
		return b.addJWTAuth()
	default:
		return fmt.Errorf("unsupported vault-kms auth method %q", b.cfg.Auth.Method)
	}
}

func (b *vaultKMSDaemonSetBuilder) addTokenAuth() error {
	if b.cfg.Auth.Token == nil {
		return fmt.Errorf("vault-kms token auth selected but token configuration is missing")
	}

	const tokenVolume = "vault-kms-token"
	tokenDir := vaultKMSCredentialDir + "/token"
	tokenRef := b.cfg.Auth.Token.TokenSecretRef

	b.addSecretItems(tokenVolume, tokenRef.Name, tokenDir, corev1.KeyToPath{Key: tokenRef.Key, Path: "token"})
	b.args = append(b.args, vaultKMSFlag(config.FlagTokenFile, tokenDir+"/token"))
	return nil
}

func (b *vaultKMSDaemonSetBuilder) addAppRoleAuth() error {
	if b.cfg.Auth.AppRole == nil {
		return fmt.Errorf("vault-kms approle auth selected but appRole configuration is missing")
	}

	auth := b.cfg.Auth.AppRole
	if auth.RoleIDKey == "" || auth.SecretIDKey == "" {
		return fmt.Errorf("vault-kms approle roleIDKey and secretIDKey must be set")
	}

	const approleVolume = "vault-kms-approle"
	approleDir := vaultKMSCredentialDir + "/approle"

	b.addSecretItems(approleVolume, auth.SecretName, approleDir,
		corev1.KeyToPath{Key: auth.RoleIDKey, Path: "role_id"},
		corev1.KeyToPath{Key: auth.SecretIDKey, Path: "secret_id"})
	b.args = append(b.args,
		vaultKMSFlag(config.FlagAppRoleRoleIDFile, approleDir+"/role_id"),
		vaultKMSFlag(config.FlagAppRoleSecretIDFile, approleDir+"/secret_id"))
	b.addAuthMountFlag(auth.AuthEngineMountPath)
	return nil
}

func (b *vaultKMSDaemonSetBuilder) addUserpassAuth() error {
	if b.cfg.Auth.Userpass == nil {
		return fmt.Errorf("vault-kms userpass auth selected but userpass configuration is missing")
	}

	auth := b.cfg.Auth.Userpass
	if auth.UsernameKey == "" || auth.PasswordKey == "" {
		return fmt.Errorf("vault-kms userpass usernameKey and passwordKey must be set")
	}

	const userpassVolume = "vault-kms-userpass"
	userpassDir := vaultKMSCredentialDir + "/userpass"

	b.addSecretItems(userpassVolume, auth.SecretName, userpassDir,
		corev1.KeyToPath{Key: auth.UsernameKey, Path: "username"},
		corev1.KeyToPath{Key: auth.PasswordKey, Path: "password"})
	b.args = append(b.args,
		vaultKMSFlag(config.FlagUserpassUsernameFile, userpassDir+"/username"),
		vaultKMSFlag(config.FlagUserpassPasswordFile, userpassDir+"/password"))
	b.addAuthMountFlag(auth.AuthEngineMountPath)
	return nil
}

func (b *vaultKMSDaemonSetBuilder) addKubernetesAuth() error {
	if b.cfg.Auth.Kubernetes == nil {
		return fmt.Errorf("vault-kms kubernetes auth selected but kubernetes configuration is missing")
	}

	auth := b.cfg.Auth.Kubernetes
	const saTokenVolume = "vault-kms-sa-token"

	tokenProjection := &corev1.ServiceAccountTokenProjection{
		Path:              "token",
		ExpirationSeconds: ptr.To(vaultKMSServiceAccountTokenExpiration),
	}
	if auth.Audience != nil && *auth.Audience != "" {
		tokenProjection.Audience = *auth.Audience
	}

	b.volumes = append(b.volumes, corev1.Volume{
		Name: saTokenVolume,
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				Sources: []corev1.VolumeProjection{
					{
						ServiceAccountToken: tokenProjection,
					},
				},
			},
		},
	})
	b.mounts = append(b.mounts, corev1.VolumeMount{Name: saTokenVolume, MountPath: vaultKMSServiceAccountTokenDir, ReadOnly: true})
	b.args = append(b.args,
		vaultKMSFlag(config.FlagKubernetesRole, auth.Role),
		vaultKMSFlag(config.FlagKubernetesJWTFile, vaultKMSServiceAccountTokenDir+"/token"))
	b.addAuthMountFlag(auth.AuthEngineMountPath)
	return nil
}

func (b *vaultKMSDaemonSetBuilder) addJWTAuth() error {
	if b.cfg.Auth.JWT == nil {
		return fmt.Errorf("vault-kms jwt auth selected but jwt configuration is missing")
	}

	const jwtVolume = "vault-kms-jwt"
	jwtDir := vaultKMSCredentialDir + "/jwt"
	auth := b.cfg.Auth.JWT

	b.addSecretItems(jwtVolume, auth.JWTSecretRef.Name, jwtDir, corev1.KeyToPath{Key: auth.JWTSecretRef.Key, Path: "jwt"})
	b.args = append(b.args,
		vaultKMSFlag(config.FlagJWTRole, auth.Role),
		vaultKMSFlag(config.FlagJWTFile, jwtDir+"/jwt"))
	b.addAuthMountFlag(auth.AuthEngineMountPath)
	return nil
}

func (b *vaultKMSDaemonSetBuilder) addAuthMountFlag(authEngineMountPath *string) {
	if authEngineMountPath != nil && *authEngineMountPath != "" {
		b.args = append(b.args, vaultKMSFlag(config.FlagAuthMount, *authEngineMountPath))
	}
}

func (b *vaultKMSDaemonSetBuilder) addSecretItems(volumeName, secretName, mountPath string, items ...corev1.KeyToPath) {
	b.volumes = append(b.volumes, secretItemsVolume(volumeName, secretName, items...))
	b.mounts = append(b.mounts, corev1.VolumeMount{Name: volumeName, MountPath: mountPath, ReadOnly: true})
}

func (b *vaultKMSDaemonSetBuilder) addConfigMapItems(volumeName, configMapName, mountPath string, items ...corev1.KeyToPath) {
	b.volumes = append(b.volumes, corev1.Volume{
		Name: volumeName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: configMapName},
				Items:                items,
			},
		},
	})
	b.mounts = append(b.mounts, corev1.VolumeMount{Name: volumeName, MountPath: mountPath, ReadOnly: true})
}

func vaultKMSContainer(ds *appsv1.DaemonSet) (*corev1.Container, error) {
	for i := range ds.Spec.Template.Spec.Containers {
		if ds.Spec.Template.Spec.Containers[i].Name == vaultKMSContainerName {
			return &ds.Spec.Template.Spec.Containers[i], nil
		}
	}
	return nil, fmt.Errorf("container %s not found in daemonset", vaultKMSContainerName)
}

// secretItemsVolume builds a Secret-backed volume that projects only the selected keys.
func secretItemsVolume(volumeName, secretName string, items ...corev1.KeyToPath) corev1.Volume {
	return corev1.Volume{
		Name: volumeName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: secretName,
				Items:      items,
			},
		},
	}
}

// vaultKMSFlag formats a "--name=value" command-line flag for the plugin container.
func vaultKMSFlag(name, value string) string {
	return fmt.Sprintf("--%s=%s", name, value)
}
