/*
Copyright 2024 NVIDIA

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
	"path/filepath"
	"strings"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/operator/utils"
	"github.com/nvidia/doca-platform/internal/release"
	"github.com/nvidia/doca-platform/pkg/bfcfg"
	"github.com/nvidia/doca-platform/pkg/certmanager"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// DPFProvisioningControllerName is the helm value for the Provisioning Controllers component name.
	DPFProvisioningControllerName        = "dpf-provisioning-controller-manager"
	bfbVolumeName                        = "bfb-volume"
	bfbHostPathPath                      = "/var/lib/nvidia/dpf/bfb"
	prepareLocalStorageInitContainerName = "prepare-local-storage"
	webhookServiceName                   = "dpf-provisioning-webhook-service"
	customBFConfigFileName               = "bf.cfg.template"
	customBFConfigVolumeName             = "bf-cfg-template"
	errManagerContainerNotFoundFmt       = "container %q not found in Provisioning Controller deployment"
	// legacyProvisioningCAObjectCount is how many objects isLegacyProvisioningCAObject matches: the
	// self-signed issuer, the CA certificate under it, and the issuer over that certificate.
	legacyProvisioningCAObjectCount = 3
)

var _ Component = &provisioningControllerObjects{}

// provisioningControllerObjects contains objects that are used to generate Provisioning Controller manifests.
// provisioningControllerObjects objects should be immutable after Parse()
type provisioningControllerObjects struct {
	data                   []byte
	bfbRegistryData        []byte
	platformCAData         []byte
	objects                []*unstructured.Unstructured
	bfbRegistryObjects     []*unstructured.Unstructured
	platformCAObjects      []*unstructured.Unstructured
	bfbRegistryServiceName string
	bfbRegistryServicePort int
}

func (p *provisioningControllerObjects) Name() operatorv1.ComponentName {
	return operatorv1.ProvisioningControllerName
}

func (p *provisioningControllerObjects) ImageName() string {
	return operatorv1.ProvisioningControllerName.WithContainer(operatorv1.ControllerManagerContainer)
}

// Parse returns typed objects for the Provisioning controller deployment.
func (p *provisioningControllerObjects) Parse() (err error) {
	if p.data == nil {
		return fmt.Errorf("provisioningControllerObjects.data can not be empty")
	} else if p.bfbRegistryData == nil {
		return fmt.Errorf("provisioningControllerObjects.bfbRegistryData can not be empty")
	} else if p.platformCAData == nil {
		return fmt.Errorf("provisioningControllerObjects.platformCAData can not be empty")
	}

	objs, err := utils.BytesToUnstructured(p.data)
	if err != nil {
		return fmt.Errorf("error while converting DPU Provisioning Controller manifests to objects: %w", err)
	} else if len(objs) == 0 {
		return fmt.Errorf("no objects found in DPU Provisioning Controller manifests")
	}
	bfbRegistryObjs, err := utils.BytesToUnstructured(p.bfbRegistryData)
	if err != nil {
		return fmt.Errorf("error while converting BFB Registry manifests to objects: %w", err)
	} else if len(bfbRegistryObjs) == 0 {
		return fmt.Errorf("no objects found in BFB Registry manifests")
	}
	platformCAObjs, err := utils.BytesToUnstructured(p.platformCAData)
	if err != nil {
		return fmt.Errorf("error while converting platform CA manifests to objects: %w", err)
	} else if len(platformCAObjs) == 0 {
		return fmt.Errorf("no objects found in platform CA manifests")
	}
	p.platformCAObjects = append(p.platformCAObjects, platformCAObjs...)
	p.bfbRegistryObjects = append(p.bfbRegistryObjects, bfbRegistryObjs...)
	for _, obj := range bfbRegistryObjs {
		if ObjectKind(obj.GetKind()) != ServiceKind {
			continue
		}
		service := &corev1.Service{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), service); err != nil {
			return fmt.Errorf("error while parsing Service for BFB Registry: %w", err)
		}
		if len(service.Spec.Ports) == 0 {
			return fmt.Errorf("service %s has no ports", obj.GetName())
		}
		p.bfbRegistryServicePort = int(service.Spec.Ports[0].Port)
		p.bfbRegistryServiceName = obj.GetName()
	}

	if p.bfbRegistryServiceName == "" {
		p.bfbRegistryServiceName = "bfb-registry"
	}
	if p.bfbRegistryServicePort == 0 {
		p.bfbRegistryServicePort = 8082
	}

	deploymentFound := false
	for _, obj := range objs {
		switch ObjectKind(obj.GetKind()) {
		// Namespace and CustomResourceDefinition can not be part of the manifests.
		case NamespaceKind, CustomResourceDefinitionKind:
			return fmt.Errorf("can not parse manifest %s: %s not allowed ", obj.GetName(), obj.GetKind())
		// If the object is the dpf-provisioning-controller-manager Deployment validate it
		case DeploymentKind:
			if obj.GetName() == DPFProvisioningControllerName {
				deploymentFound = true
				err = p.validateDeployment(obj)
				if err != nil {
					return err
				}
			}
		}
		p.objects = append(p.objects, obj)
	}
	if !deploymentFound {
		return fmt.Errorf("error while converting Provisioning Controller manifests to objects: Deployment not found")
	}
	return nil
}

// GenerateManifests applies edits and returns objects
func (p *provisioningControllerObjects) GenerateManifests(_ context.Context, vars Variables) ([]client.Object, error) {
	ret := []client.Object{}
	if ok := vars.DisableSystemComponents[p.Name()]; ok {
		return []client.Object{}, nil
	}
	// check vars
	// BFBPersistentVolumeClaimName is now optional - if not provided, will use hostPath
	if t := vars.DPFProvisioningController.DMSTimeout; t != nil && *t < 0 {
		return nil, fmt.Errorf("DPFProvisioningController invalid DMSTimeout, must be greater than or equal to 0")
	}
	if vars.PlatformCAIssuerRef.Name == "" {
		return nil, fmt.Errorf("PlatformCAIssuerRef must have a name, it anchors the platform intermediate CA")
	}

	// make a copy of the objects
	objsCopy := make([]*unstructured.Unstructured, 0, len(p.objects))
	for i := range p.objects {
		if !vars.RetainLegacyProvisioningCA && isLegacyProvisioningCAObject(p.objects[i]) {
			continue
		}
		objsCopy = append(objsCopy, p.objects[i].DeepCopy())
	}
	// Added before the edits below so they pick up the namespace and the component labels like every
	// other object read from an embedded manifest, and so the issuerRef edit reaches the intermediate.
	for i := range p.platformCAObjects {
		objsCopy = append(objsCopy, p.platformCAObjects[i].DeepCopy())
	}

	labelsToAdd := map[string]string{
		operatorv1.DPFComponentLabelKey: p.Name().String(),
		release.DPFVersionLabelKey:      release.DPFVersion(),
		applysetPartOfLabel:             ApplySetID(vars.Namespace, p),
	}

	bfbRegistryObjects, err := p.editRegistryObjs(vars, labelsToAdd)
	if err != nil {
		return nil, err
	}
	for i := range bfbRegistryObjects {
		ret = append(ret, bfbRegistryObjects[i])
	}

	// apply edits
	// TODO: make it generic to not edit every kind one-by-one.
	if err := NewEdits().
		AddForAll(NamespaceEdit(vars.Namespace),
			LabelsEdit(labelsToAdd)).
		AddForKind(CertificateKind, p.setPlatformCAEdit(vars)).
		AddForKind(CertificateKind, p.holdLeafIssuerEdit(vars)).
		AddForKind(IssuerKind, p.setLegacyProvisioningIssuerCAEdit(vars)).
		AddForKindS(DeploymentKind, ImagePullSecretsEditForDeploymentEdit(vars.ImagePullSecrets...)).
		AddForKindS(DeploymentKind, p.dpfProvisioningDeploymentEdit(vars)).
		AddForKindS(DeploymentKind, NodeAffinityEdit(&controlPlaneNodeAffinity)).
		AddForKindS(StatefulSetKind, NodeAffinityEdit(&controlPlaneNodeAffinity)).
		AddForKindS(DeploymentKind, TolerationsEdit(controlPlaneTolerations)).
		AddForKindS(StatefulSetKind, TolerationsEdit(controlPlaneTolerations)).
		AddForKindS(DaemonSetKind, TolerationsEdit(controlPlaneTolerations)).
		AddForKind(ServiceKind, fixupWebhookServiceEdit).
		Apply(objsCopy); err != nil {
		return nil, err
	}
	for i := range objsCopy {
		ret = append(ret, objsCopy[i])
	}

	return ret, nil
}

// isLegacyProvisioningCAObject reports whether obj belongs to the provisioning CA chain that the
// platform intermediate CA takes over from.
//
// The chain is rendered from the kustomize overlay like everything else, rather than kept in a
// manifest of its own, so that what is applied while the migration runs is exactly what the
// releases that created these objects applied. An apply that differed in any field would have
// cert-manager reissue the CA, generating a new keypair and changing the identity the migration is
// carrying over. Which of them are emitted is decided here instead.
func isLegacyProvisioningCAObject(obj *unstructured.Unstructured) bool {
	switch ObjectKind(obj.GetKind()) {
	case CertificateKind:
		return obj.GetName() == operatorv1.LegacyProvisioningCACertificateName
	case IssuerKind:
		return obj.GetName() == operatorv1.LegacyProvisioningIssuerName ||
			obj.GetName() == operatorv1.LegacyProvisioningSelfSignedIssuerName
	default:
		return false
	}
}

// setLegacyProvisioningIssuerCAEdit keeps the retained legacy provisioning Issuer signing with the
// Secret a CA rotation has it pinned to.
//
// The pin is what the rotation of that CA is driven by, and the apply forces ownership of every
// field it sets, so emitting the Issuer with the Secret of its own chain would take the pin away
// from a rotation in flight. The migration refuses to run while one is, since what is in that
// Secret during a rotation is not necessarily what signed the certificates the peers hold, and it
// reads the pin from this Issuer: erasing it here would have the migration proceed on the next
// reconcile, in the middle of the rotation it just declined to migrate under.
//
// It does nothing on a cluster not in the middle of one, which is where the manifest's own Secret
// is already the right answer, and nothing at all once the chain is retired and the Issuer with it.
func (p *provisioningControllerObjects) setLegacyProvisioningIssuerCAEdit(vars Variables) UnstructuredEdit {
	return func(obj *unstructured.Unstructured) error {
		if obj.GetName() != operatorv1.LegacyProvisioningIssuerName || vars.LegacyProvisioningIssuerCASecretName == "" {
			return nil
		}
		return unstructured.SetNestedField(obj.Object,
			vars.LegacyProvisioningIssuerCASecretName, "spec", "ca", "secretName")
	}
}

// setPlatformCAEdit points the platform intermediate CA at whatever anchors it, and requests it
// under the identity it carries.
//
// The provisioning certificates reference the issuer over that CA and never the CA that signs it,
// which makes this issuerRef the single place where the choice of certificate authority takes
// effect: the self-signed root the dpf-operator chart creates, or the authority its
// certificateAuthority.issuerRef value names. Everything below the intermediate is identical in
// both modes, so this is the only object the mode edit touches.
//
// The subject is replaced only on a cluster the upgrade migration carried a provisioning CA into.
// A CA is a subject as much as a key: requesting the one whose keypair was adopted is what lets the
// intermediate validate the certificates that CA signed, and requesting the subject of the manifest
// over an adopted key would produce an authority that can validate none of them.
func (p *provisioningControllerObjects) setPlatformCAEdit(vars Variables) UnstructuredEdit {
	return func(obj *unstructured.Unstructured) error {
		if obj.GetName() != operatorv1.PlatformIntermediateCAName {
			return nil
		}

		if err := unstructured.SetNestedStringMap(obj.Object, map[string]string{
			"name":  vars.PlatformCAIssuerRef.Name,
			"kind":  vars.PlatformCAIssuerRef.Kind,
			"group": vars.PlatformCAIssuerRef.Group,
		}, "spec", "issuerRef"); err != nil {
			return err
		}

		if vars.PlatformCACommonName == "" {
			return nil
		}
		return unstructured.SetNestedField(obj.Object, vars.PlatformCACommonName, "spec", "commonName")
	}
}

// holdLeafIssuerEdit signs the leaf certificates that reference the platform issuer with the legacy
// provisioning issuer while the upgrade migration carries the legacy CA into the platform
// intermediate CA. Until the intermediate holds that identity, cert-manager can issue it under the
// subject of the manifest, and a leaf signed then names an issuer no peer is guaranteed to trust.
func (p *provisioningControllerObjects) holdLeafIssuerEdit(vars Variables) UnstructuredEdit {
	return func(obj *unstructured.Unstructured) error {
		if !vars.HoldLeavesOnLegacyProvisioningIssuer {
			return nil
		}
		name, _, err := unstructured.NestedString(obj.Object, "spec", "issuerRef", "name")
		if err != nil || name != operatorv1.PlatformIssuerName {
			return err
		}
		return unstructured.SetNestedField(obj.Object, operatorv1.LegacyProvisioningIssuerName, "spec", "issuerRef", "name")
	}
}

// leafIssuerRef is the issuer the provisioning leaf certificates are signed by: the platform issuer,
// or the legacy provisioning issuer while holdLeafIssuerEdit holds them on it.
func leafIssuerRef(vars Variables) certmanager.IssuerReference {
	ref := certmanager.PlatformLeafIssuerRef()
	if vars.HoldLeavesOnLegacyProvisioningIssuer {
		ref.Name = operatorv1.LegacyProvisioningIssuerName
	}
	return ref
}

func (p *provisioningControllerObjects) dpfProvisioningDeploymentEdit(vars Variables) StructuredEdit {
	return func(obj client.Object) error {
		deployment, ok := obj.(*appsv1.Deployment)
		if !ok {
			return fmt.Errorf("unexpected object %s. expected Deployment", obj.GetObjectKind().GroupVersionKind())
		}

		mods := []func(*appsv1.Deployment, Variables) error{
			p.setBFBPersistentVolumeClaim,
			p.setNodeNameAndIPEnv,
			p.setBFBRegistryImageEnv,
			p.setImagePullSecrets,
			p.setComponentLabel,
			p.setDefaultImageNames,
			p.setDMSTimeout,
			p.addBFCFGConfigMapMountEdit,
			p.setCustomCASecretName,
			p.setInstallInterface,
			p.setDeploymentMode,
			p.setKubernetesAPIServerEnvVars,
			p.setMaxDPUParallelInstallations,
			p.setDPUMaxConcurrentReconciles,
			p.setMultiDPUOperationsSyncWaitTime,
			p.setMaxUnavailableDPUNodes,
			p.setOSInstallTimeout,
			p.setNodeJoinTokenTTL,
			p.setOSInstallRetries,
			p.setFirmwareUpdateTimeout,
			p.setPreInstallAgentRegistrationTimeout,
			p.setNodeEffectRemovalTimeout,
			p.setHostAgentDNSPolicy,
			p.setResources,
			p.setBFBRegistryLoadBalancerAddress,
			p.setReplicas,
			p.setCAIssuer,
			p.setCATrustBundleConfigMapName,
			tracingFlagsFor(operatorv1.ProvisioningControllerName),
		}
		for _, mod := range mods {
			if err := mod(deployment, vars); err != nil {
				return fmt.Errorf("error while updating Deployment for Provisioning Controller: %w", err)
			}
		}
		return nil
	}
}

func (p *provisioningControllerObjects) editRegistryObjs(vars Variables, labelsToAdd map[string]string) ([]*unstructured.Unstructured, error) {
	objs := make([]*unstructured.Unstructured, 0, len(p.bfbRegistryObjects))
	for i := range p.bfbRegistryObjects {
		objs = append(objs, p.bfbRegistryObjects[i].DeepCopy())
	}
	edit := NewEdits().AddForAll(NamespaceEdit(vars.Namespace), LabelsEdit(labelsToAdd))
	if err := edit.Apply(objs); err != nil {
		return nil, err
	}
	return objs, nil
}

// Set the component label for the deployment.
func (p *provisioningControllerObjects) setComponentLabel(deployment *appsv1.Deployment, _ Variables) error {
	labels := deployment.Spec.Template.ObjectMeta.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	labels[operatorv1.DPFComponentLabelKey] = DPFProvisioningControllerName
	deployment.Spec.Template.ObjectMeta.Labels = labels
	return nil
}

func (p *provisioningControllerObjects) addBFCFGConfigMapMountEdit(deployment *appsv1.Deployment, vars Variables) error {
	if vars.DPFProvisioningController.BFCFGTemplateConfig == nil {
		return nil
	}
	if deployment.Spec.Template.Spec.Volumes == nil {
		deployment.Spec.Template.Spec.Volumes = []corev1.Volume{}
	}

	bfbCFGConfigMapVolume := corev1.Volume{
		Name: customBFConfigVolumeName,
		VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: *vars.DPFProvisioningController.BFCFGTemplateConfig},
			Items: []corev1.KeyToPath{
				{
					Key:  bfcfg.ConfigMapDataKey,
					Path: customBFConfigFileName,
				},
			},
		}},
	}
	deployment.Spec.Template.Spec.Volumes = append(deployment.Spec.Template.Spec.Volumes, bfbCFGConfigMapVolume)

	if deployment.Spec.Template.Spec.Containers[0].VolumeMounts == nil {
		deployment.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{}
	}
	mount := corev1.VolumeMount{
		Name:      customBFConfigVolumeName,
		MountPath: "/bfb-config",
	}
	deployment.Spec.Template.Spec.Containers[0].VolumeMounts = append(deployment.Spec.Template.Spec.Containers[0].VolumeMounts, mount)

	arg := fmt.Sprintf("--bf-cfg-template-file=%s", filepath.Join(mount.MountPath, customBFConfigFileName))
	deployment.Spec.Template.Spec.Containers[0].Args = append(deployment.Spec.Template.Spec.Containers[0].Args, arg)

	return nil
}

// Set Resources for the deployment.
func (p *provisioningControllerObjects) setResources(deploy *appsv1.Deployment, vars Variables) error {
	if resources, exists := vars.Resources[p.ImageName()]; exists {
		// Check if resources are set (either requests or limits)
		if len(resources.Requests) > 0 || len(resources.Limits) > 0 {
			container := getManagerContainer(deploy)
			if container != nil {
				container.Resources = resources
			}
		}
	}
	return nil
}

// Add a component label selector to the webhook service.
func fixupWebhookServiceEdit(obj *unstructured.Unstructured) error {
	if obj.GetName() != webhookServiceName {
		return nil
	}
	// do the conversion to ensure we're dealing with the correct type, but deal with unstructured for the patch.
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), &corev1.Service{})
	if err != nil {
		return fmt.Errorf("error while converting DPU Provisioning Controller Service to objects: %w", err)
	}
	selector, found, err := unstructured.NestedMap(obj.UnstructuredContent(), "spec", "selector")
	if err != nil {
		return fmt.Errorf("error while converting DPU Provisioning Controller Service to objects: %w", err)
	}
	if !found {
		return fmt.Errorf("DPU Provisioning Controller webhook secret does not have a selector")
	}
	selector[operatorv1.DPFComponentLabelKey] = DPFProvisioningControllerName
	err = unstructured.SetNestedMap(obj.UnstructuredContent(), selector, "spec", "selector")
	if err != nil {
		return fmt.Errorf("error while converting DPU Provisioning Controller Service to objects: %w", err)
	}
	return nil
}

func (p *provisioningControllerObjects) validateDeployment(obj *unstructured.Unstructured) error {
	deploy := &appsv1.Deployment{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), deploy); err != nil {
		return fmt.Errorf("error while parsing Deployment for Provisioning Controller: %w", err)
	}
	vol := p.getVolume(deploy, bfbVolumeName)
	if vol == nil {
		return fmt.Errorf("invalid Provisioning Controller deployment, no bfb volume found")
	}
	c := getManagerContainer(deploy)
	if c == nil {
		return fmt.Errorf(errManagerContainerNotFoundFmt, managerContainerName)
	}
	return nil
}

func (p *provisioningControllerObjects) getVolume(deploy *appsv1.Deployment, volName string) *corev1.Volume {
	for i, vol := range deploy.Spec.Template.Spec.Volumes {
		if vol.Name == volName {
			return &deploy.Spec.Template.Spec.Volumes[i]
		}
	}
	return nil
}

func (p *provisioningControllerObjects) setBFBPersistentVolumeClaim(deploy *appsv1.Deployment, vars Variables) error {
	vol := p.getVolume(deploy, bfbVolumeName)
	if vol == nil {
		return fmt.Errorf("error while generating Deployment for Provisioning Controller: no bfb volume found")
	}

	c := getManagerContainer(deploy)
	if c == nil {
		return fmt.Errorf(errManagerContainerNotFoundFmt, managerContainerName)
	}

	// If PVC name is provided, use the persistent volume.
	if vars.DPFProvisioningController.BFBPersistentVolumeClaimName != nil &&
		*vars.DPFProvisioningController.BFBPersistentVolumeClaimName != "" {
		vol.VolumeSource = corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: *vars.DPFProvisioningController.BFBPersistentVolumeClaimName,
			},
		}
		vol.HostPath = nil
		// Ensure no init container while using persistent volume.
		deploy.Spec.Template.Spec.InitContainers = filterOutInitContainer(deploy.Spec.Template.Spec.InitContainers, prepareLocalStorageInitContainerName)
		return setFlags(c, fmt.Sprintf("--bfb-pvc=%s", *vars.DPFProvisioningController.BFBPersistentVolumeClaimName))
	}
	// Use hostPath and add init-container to prepare the directory.
	hostPathType := corev1.HostPathDirectoryOrCreate
	vol.VolumeSource = corev1.VolumeSource{
		HostPath: &corev1.HostPathVolumeSource{
			Path: bfbHostPathPath,
			Type: &hostPathType,
		},
	}
	vol.PersistentVolumeClaim = nil
	addBFBHostPathInitContainer(deploy)
	return setFlags(c, "--bfb-pvc=")
}

func addBFBHostPathInitContainer(deploy *appsv1.Deployment) {
	for i := range deploy.Spec.Template.Spec.InitContainers {
		if deploy.Spec.Template.Spec.InitContainers[i].Name == prepareLocalStorageInitContainerName {
			return // already present
		}
	}
	initContainer := corev1.Container{
		Name:    prepareLocalStorageInitContainerName,
		Image:   "busybox:1.36",
		Command: []string{"sh", "-c", "mkdir -p /bfb && chown -R 65532:65532 /bfb"},
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:  ptr.To(int64(0)),
			Privileged: ptr.To(true),
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: bfbVolumeName, MountPath: "/bfb"},
		},
	}
	deploy.Spec.Template.Spec.InitContainers = append(deploy.Spec.Template.Spec.InitContainers, initContainer)
}

func filterOutInitContainer(containers []corev1.Container, name string) []corev1.Container {
	out := make([]corev1.Container, 0, len(containers))
	for _, c := range containers {
		if c.Name != name {
			out = append(out, c)
		}
	}
	return out
}

func (p *provisioningControllerObjects) setNodeNameAndIPEnv(deploy *appsv1.Deployment, _ Variables) error {
	c := getManagerContainer(deploy)
	if c == nil {
		return fmt.Errorf("container %q not found in Provisioning Controller deployment", managerContainerName)
	}
	envVars := []corev1.EnvVar{
		{
			Name: "NODE_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
			},
		},
		{
			Name: "NODE_IP",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.hostIP"},
			},
		},
		{
			Name: "POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
			},
		},
		{
			Name: "POD_NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
			},
		},
	}
	c.Env = append(c.Env, envVars...)
	return nil
}

func (p *provisioningControllerObjects) setBFBRegistryImageEnv(deploy *appsv1.Deployment, vars Variables) error {
	c := getManagerContainer(deploy)
	if c == nil {
		return fmt.Errorf("container %q not found in Provisioning Controller deployment", managerContainerName)
	}
	image, ok := vars.Images[operatorv1.BFBRegistryName.String()]
	if !ok {
		return nil
	}
	c.Env = append(c.Env, corev1.EnvVar{Name: "BFB_REGISTRY_IMAGE", Value: image})
	return nil
}

func (p *provisioningControllerObjects) setKubernetesAPIServerEnvVars(deploy *appsv1.Deployment, vars Variables) error {
	envVars := []string{}

	if vars.KubernetesAPIServerVIP != nil {
		envVars = append(envVars, fmt.Sprintf("KUBERNETES_SERVICE_HOST=%s", *vars.KubernetesAPIServerVIP))
	}
	if vars.KubernetesAPIServerPort != nil {
		envVars = append(envVars, fmt.Sprintf("KUBERNETES_SERVICE_PORT=%d", *vars.KubernetesAPIServerPort))
	}

	if len(envVars) == 0 {
		return nil
	}
	c := getManagerContainer(deploy)
	if c == nil {
		return fmt.Errorf(errManagerContainerNotFoundFmt, managerContainerName)
	}
	return setFlags(c, fmt.Sprintf("--dms-pod-envs=%s", strings.Join(envVars, ",")))
}

func (p *provisioningControllerObjects) setMaxDPUParallelInstallations(deploy *appsv1.Deployment, vars Variables) error {
	c := getManagerContainer(deploy)
	if c == nil {
		return fmt.Errorf(errManagerContainerNotFoundFmt, managerContainerName)
	}
	if vars.DPFProvisioningController.MaxDPUParallelInstallations == nil {
		return nil
	}
	return setFlags(c, fmt.Sprintf("--max-dpu-parallel-installations=%d", *vars.DPFProvisioningController.MaxDPUParallelInstallations))
}

// setDPUMaxConcurrentReconciles sets --dpu-max-concurrent-reconciles on the
// provisioning controller when DPFOperatorConfig specifies a non-zero value.
func (p *provisioningControllerObjects) setDPUMaxConcurrentReconciles(deploy *appsv1.Deployment, vars Variables) error {
	if vars.DPFProvisioningController.DPUMaxConcurrentReconciles == 0 {
		return nil
	}
	c := getManagerContainer(deploy)
	if c == nil {
		return fmt.Errorf(errManagerContainerNotFoundFmt, managerContainerName)
	}
	return setFlags(c, fmt.Sprintf("--dpu-max-concurrent-reconciles=%d", vars.DPFProvisioningController.DPUMaxConcurrentReconciles))
}

func (p *provisioningControllerObjects) setOSInstallRetries(deploy *appsv1.Deployment, vars Variables) error {
	if vars.DPFProvisioningController.OSInstallRetries == 0 {
		return nil
	}
	c := getManagerContainer(deploy)
	if c == nil {
		return fmt.Errorf(errManagerContainerNotFoundFmt, managerContainerName)
	}
	return setFlags(c, fmt.Sprintf("--os-install-retries=%d", vars.DPFProvisioningController.OSInstallRetries))
}

func (p *provisioningControllerObjects) setImagePullSecrets(deploy *appsv1.Deployment, vars Variables) error {
	c := getManagerContainer(deploy)
	if c == nil {
		return fmt.Errorf(errManagerContainerNotFoundFmt, managerContainerName)
	}
	if len(vars.ImagePullSecrets) == 0 {
		return nil
	}
	return setFlags(c, fmt.Sprintf("--image-pull-secrets=%s", strings.Join(vars.ImagePullSecrets, ",")))
}

func (p *provisioningControllerObjects) setCustomCASecretName(deploy *appsv1.Deployment, vars Variables) error {
	c := getManagerContainer(deploy)
	if c == nil {
		return fmt.Errorf(errManagerContainerNotFoundFmt, managerContainerName)
	}
	if vars.DPFProvisioningController.CustomCASecretName == nil {
		return nil
	}
	return setFlags(c, fmt.Sprintf("--custom-CA-secret=%s", *vars.DPFProvisioningController.CustomCASecretName))
}

func (p *provisioningControllerObjects) setInstallInterface(deploy *appsv1.Deployment, vars Variables) error {
	c := getManagerContainer(deploy)
	if c == nil {
		return fmt.Errorf(errManagerContainerNotFoundFmt, managerContainerName)
	}
	if vars.DPFProvisioningController.InstallInterface == nil ||
		vars.DPFProvisioningController.InstallInterface.InstallViaGNOI != nil || //nolint:staticcheck
		vars.DPFProvisioningController.InstallInterface.InstallViaHostAgent != nil {
		return setFlags(c, fmt.Sprintf("--dpu-install-interface=%s", provisioningv1.InstallViaHostAgent))
	} else if vars.DPFProvisioningController.InstallInterface.InstallViaRedfish != nil {
		err := setFlags(c, fmt.Sprintf("--dpu-install-interface=%s", provisioningv1.InstallViaRedFish))
		if err != nil {
			return err
		}
		return nil
	}
	return fmt.Errorf("provisioning controller install interface not set")
}

func (p *provisioningControllerObjects) setDeploymentMode(deploy *appsv1.Deployment, vars Variables) error {
	if vars.DPFProvisioningController.DeploymentMode == "" {
		return fmt.Errorf("deploymentMode must be set on DPFOperatorConfig.spec.deploymentMode")
	}
	c := getManagerContainer(deploy)
	if c == nil {
		return fmt.Errorf(errManagerContainerNotFoundFmt, managerContainerName)
	}
	return setFlags(c, fmt.Sprintf("--deployment-mode=%s", string(vars.DPFProvisioningController.DeploymentMode)))
}

func (p *provisioningControllerObjects) setBFBRegistryLoadBalancerAddress(deploy *appsv1.Deployment, vars Variables) error {
	c := getManagerContainer(deploy)
	if c == nil {
		return fmt.Errorf(errManagerContainerNotFoundFmt, managerContainerName)
	}
	if vars.DPFProvisioningController.Registry != nil && vars.DPFProvisioningController.Registry.LoadBalancerAddress != nil {
		return setFlags(c, fmt.Sprintf("--bfb-registry-load-balancer-address=%s", *vars.DPFProvisioningController.Registry.LoadBalancerAddress))
	}
	return setFlags(c, "--bfb-registry-load-balancer-address=")
}

func (p *provisioningControllerObjects) setDMSTimeout(deploy *appsv1.Deployment, vars Variables) error {
	t := vars.DPFProvisioningController.DMSTimeout
	if t == nil {
		return nil
	}
	c := getManagerContainer(deploy)
	if c == nil {
		return fmt.Errorf(errManagerContainerNotFoundFmt, managerContainerName)
	}
	return setFlags(c, fmt.Sprintf("--dms-timeout=%d", *vars.DPFProvisioningController.DMSTimeout))
}

func (p *provisioningControllerObjects) setDefaultImageNames(deploy *appsv1.Deployment, vars Variables) error {
	c := getManagerContainer(deploy)
	if c == nil {
		return fmt.Errorf(errManagerContainerNotFoundFmt, managerContainerName)
	}
	defaults := release.NewDefaults()
	err := defaults.Parse()
	if err != nil {
		return err
	}
	imageName, ok := vars.Images[p.ImageName()]
	if !ok {
		return fmt.Errorf("image for %q not found in variables", p.Name())
	}
	c.Image = imageName
	err = setFlags(c, fmt.Sprintf("--dms-image=%s", defaults.DMSImage))
	if err != nil {
		return err
	}

	if sc := getContainerByName(deploy, operatorv1.PLDMUnpackContainerName.String()); sc != nil {
		if bfbImage, ok := vars.Images[operatorv1.BFBRegistryName.String()]; ok {
			sc.Image = bfbImage
		}
	}
	return nil
}

func (p *provisioningControllerObjects) setMultiDPUOperationsSyncWaitTime(deploy *appsv1.Deployment, vars Variables) error {
	c := getManagerContainer(deploy)
	if c == nil {
		return fmt.Errorf(errManagerContainerNotFoundFmt, managerContainerName)
	}
	return setFlags(c, fmt.Sprintf("--multi-dpu-operations-sync-wait-time=%s", vars.DPFProvisioningController.MultiDPUOperationsSyncWaitTime.String()))
}

func (p *provisioningControllerObjects) setMaxUnavailableDPUNodes(deploy *appsv1.Deployment, vars Variables) error {
	c := getManagerContainer(deploy)
	if c == nil {
		return fmt.Errorf(errManagerContainerNotFoundFmt, managerContainerName)
	}
	if vars.DPFProvisioningController.MaxUnavailableDPUNodes == nil {
		return nil
	}
	return setFlags(c, fmt.Sprintf("--max-unavailable-dpu-nodes=%d", *vars.DPFProvisioningController.MaxUnavailableDPUNodes))
}

func (p *provisioningControllerObjects) setReplicas(deploy *appsv1.Deployment, vars Variables) error {
	// Default to 2 replicas if not specified
	replicas := ptr.To[int32](2)
	if vars.DPFProvisioningController.Replicas != nil {
		replicas = vars.DPFProvisioningController.Replicas
	}
	deploy.Spec.Replicas = replicas
	return nil
}

func (p *provisioningControllerObjects) setOSInstallTimeout(deploy *appsv1.Deployment, vars Variables) error {
	if vars.DPFProvisioningController.OSInstallTimeout == nil {
		return nil
	}
	c := getManagerContainer(deploy)
	if c == nil {
		return fmt.Errorf(errManagerContainerNotFoundFmt, managerContainerName)
	}
	return setFlags(c, fmt.Sprintf("--os-install-timeout=%s", vars.DPFProvisioningController.OSInstallTimeout.Duration.String()))
}

// setNodeJoinTokenTTL configures the kubeadm bootstrap token lifetime when specified.
func (p *provisioningControllerObjects) setNodeJoinTokenTTL(deploy *appsv1.Deployment, vars Variables) error {
	if vars.DPFProvisioningController.NodeJoinTokenTTL == nil {
		return nil
	}
	c := getManagerContainer(deploy)
	if c == nil {
		return fmt.Errorf(errManagerContainerNotFoundFmt, managerContainerName)
	}
	return setFlags(c, fmt.Sprintf("--node-join-token-ttl=%s", vars.DPFProvisioningController.NodeJoinTokenTTL.Duration.String()))
}

func (p *provisioningControllerObjects) setFirmwareUpdateTimeout(deploy *appsv1.Deployment, vars Variables) error {
	if vars.DPFProvisioningController.FirmwareUpdateTimeout == nil {
		return nil
	}
	c := getManagerContainer(deploy)
	if c == nil {
		return fmt.Errorf(errManagerContainerNotFoundFmt, managerContainerName)
	}
	return setFlags(c, fmt.Sprintf("--firmware-update-timeout=%s", vars.DPFProvisioningController.FirmwareUpdateTimeout.Duration.String()))
}

func (p *provisioningControllerObjects) setPreInstallAgentRegistrationTimeout(deploy *appsv1.Deployment, vars Variables) error {
	c := getManagerContainer(deploy)
	if c == nil {
		return fmt.Errorf(errManagerContainerNotFoundFmt, managerContainerName)
	}
	if vars.DPFProvisioningController.PreInstallAgentRegistrationTimeout == nil {
		return nil
	}
	return setFlags(c, fmt.Sprintf("--pre-install-agent-registration-timeout=%s",
		vars.DPFProvisioningController.PreInstallAgentRegistrationTimeout.Duration.String()))
}

func (p *provisioningControllerObjects) setNodeEffectRemovalTimeout(deploy *appsv1.Deployment, vars Variables) error {
	c := getManagerContainer(deploy)
	if c == nil {
		return fmt.Errorf(errManagerContainerNotFoundFmt, managerContainerName)
	}
	if vars.DPFProvisioningController.NodeEffectRemovalTimeout == nil {
		return nil
	}
	return setFlags(c, fmt.Sprintf("--node-effect-removal-timeout=%s", vars.DPFProvisioningController.NodeEffectRemovalTimeout.Duration.String()))
}

// setCAIssuer tells the controller which issuer to sign the certificates it mints at runtime with:
// the DPU BMC server certificate and the bfb-registry server certificate. Both are created outside
// the manifest, so the issuer reaches them here rather than through a leaf Certificate object.
//
// The value is the platform issuer in both CA modes, since only the anchor above the intermediate CA
// differs between them. It is templated all the same so that renaming the issuer rolls the
// controller out with the new name instead of requiring a matching change in its binary, and so
// that the upgrade migration can hold these certificates on the legacy issuer like the others.
func (p *provisioningControllerObjects) setCAIssuer(deploy *appsv1.Deployment, vars Variables) error {
	c := getManagerContainer(deploy)
	if c == nil {
		return fmt.Errorf(errManagerContainerNotFoundFmt, managerContainerName)
	}
	issuerRef := leafIssuerRef(vars)
	return setFlags(c,
		fmt.Sprintf("--ca-issuer-name=%s", issuerRef.Name),
		fmt.Sprintf("--ca-issuer-kind=%s", issuerRef.Kind),
		fmt.Sprintf("--ca-issuer-group=%s", issuerRef.Group),
	)
}

// setCATrustBundleConfigMapName passes the effective trust bundle name down to the HostAgent, which
// validates the bfb-registry server certificate against it. The controller defaults the flag to the
// same name, so this only matters once an external issuer lets the operator name the bundle
// themselves: without it the HostAgent would keep reading a ConfigMap nobody maintains.
func (p *provisioningControllerObjects) setCATrustBundleConfigMapName(deploy *appsv1.Deployment, vars Variables) error {
	c := getManagerContainer(deploy)
	if c == nil {
		return fmt.Errorf(errManagerContainerNotFoundFmt, managerContainerName)
	}
	name := vars.CATrustBundleConfigMapName
	if name == "" {
		name = operatorv1.DefaultCATrustBundleConfigMapName
	}
	return setFlags(c, fmt.Sprintf("--ca-trust-bundle-configmap-name=%s", name))
}

func (p *provisioningControllerObjects) setHostAgentDNSPolicy(deploy *appsv1.Deployment, vars Variables) error {
	if vars.DPFProvisioningController.HostAgentDNSPolicy == nil {
		return nil
	}
	c := getManagerContainer(deploy)
	if c == nil {
		return fmt.Errorf("container %q not found in Provisioning Controller deployment", managerContainerName)
	}
	return setFlags(c, fmt.Sprintf("--hostagent-dns-policy=%s", *vars.DPFProvisioningController.HostAgentDNSPolicy))
}

// IsReadyForUpgrade reports the readiness of the provisioning controller objects. It returns an error when the number of Replicas in
// the single provisioning controller deployment is true.
func (p *provisioningControllerObjects) IsReadyForUpgrade(ctx context.Context, c client.Client, config *operatorv1.DPFOperatorConfig) error {
	return deploymentReadyCheck(ctx, c, config.GetNamespace(), p.objects, false)
}

// IsReady reports the readiness of the provisioning controller objects as well as the version state.
// It returns an error when the number of Replicas in the single provisioning controller deployment is true.
func (p *provisioningControllerObjects) IsReady(ctx context.Context, c client.Client, namespace string) error {
	return deploymentReadyCheck(ctx, c, namespace, p.objects, true)
}
