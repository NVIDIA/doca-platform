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
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/bfcfg"
	"github.com/nvidia/doca-platform/internal/release"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// DPFProvisioningControllerName is the helm value for the Provisioning Controllers component name.
	DPFProvisioningControllerName          = "dpf-provisioning-controller-manager"
	dpfProvisioningControllerContainerName = "manager"
	bfbVolumeName                          = "bfb-volume"
	webhookServiceName                     = "dpf-provisioning-webhook-service"
	customBFConfigFileName                 = "bf.cfg.template"
	customBFConfigVolumeName               = "bf-cfg-template"
)

var _ Component = &provisioningControllerObjects{}

// provisioningControllerObjects contains objects that are used to generate Provisioning Controller manifests.
// provisioningControllerObjects objects should be immutable after Parse()
type provisioningControllerObjects struct {
	data               []byte
	bfbRegistryData    []byte
	objects            []*unstructured.Unstructured
	bfbRegistryObjects []*unstructured.Unstructured
}

func (p *provisioningControllerObjects) Name() string {
	return operatorv1.ProvisioningControllerName
}

// Parse returns typed objects for the Provisioning controller deployment.
func (p *provisioningControllerObjects) Parse() (err error) {
	if p.data == nil {
		return fmt.Errorf("provisioningControllerObjects.data can not be empty")
	} else if p.bfbRegistryData == nil {
		return fmt.Errorf("provisioningControllerObjects.bfbRegistryData can not be empty")
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
	p.bfbRegistryObjects = append(p.bfbRegistryObjects, bfbRegistryObjs...)

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
func (p *provisioningControllerObjects) GenerateManifests(vars Variables, options ...GenerateManifestOption) ([]client.Object, error) {
	ret := []client.Object{}
	if ok := vars.DisableSystemComponents[p.Name()]; ok {
		return []client.Object{}, nil
	}
	opts := &GenerateManifestOptions{}
	for _, option := range options {
		option.Apply(opts)
	}
	// check vars
	if strings.TrimSpace(vars.DPFProvisioningController.BFBPersistentVolumeClaimName) == "" {
		return nil, fmt.Errorf("DPFProvisioningController empty BFBPersistentVolumeClaimName")
	}
	if t := vars.DPFProvisioningController.DMSTimeout; t != nil && *t < 0 {
		return nil, fmt.Errorf("DPFProvisioningController invalid DMSTimeout, must be greater than or equal to 0")
	}

	// make a copy of the objects
	objsCopy := make([]*unstructured.Unstructured, 0, len(p.objects))
	for i := range p.objects {
		objsCopy = append(objsCopy, p.objects[i].DeepCopy())
	}

	labelsToAdd := map[string]string{
		operatorv1.DPFComponentLabelKey: p.Name(),
		release.DPFVersionLabelKey:      release.DPFVersion(),
	}
	applySetID := ApplySetID(vars.Namespace, p)
	// Add the ApplySet to the manifests if this hasn't been disabled.
	if !opts.skipApplySet {
		labelsToAdd[applysetPartOfLabel] = applySetID
	}

	if vars.DPFProvisioningController.InstallInterface != nil &&
		vars.DPFProvisioningController.InstallInterface.InstallViaRedfish != nil {
		rfCfg := vars.DPFProvisioningController.InstallInterface.InstallViaRedfish
		objsCopyRegistry := make([]*unstructured.Unstructured, 0, len(p.bfbRegistryObjects))
		for i := range p.bfbRegistryObjects {
			objsCopyRegistry = append(objsCopyRegistry, p.bfbRegistryObjects[i].DeepCopy())
		}
		image, ok := vars.Images[operatorv1.BFBRegistryName]
		if !ok {
			return nil, fmt.Errorf("image for %q not found in variables", operatorv1.BFBRegistryName)
		}
		edit := NewEdits().AddForAll(NamespaceEdit(vars.Namespace), LabelsEdit(labelsToAdd)).
			AddForKindS(DaemonSetKind, ImageForDaemonSetContainerEdit("nginx", image)).
			AddForKindS(DaemonSetKind, ImagePullSecretsEditForDaemonSetEdit(vars.ImagePullSecrets...)).
			AddForKindS(DaemonSetKind, NodeAffinityEdit(&controlPlaneNodeAffinity)).
			AddForKindS(DaemonSetKind, TolerationsEdit(controlPlaneTolerations))
		if rfCfg.BFBRegistry != nil && rfCfg.BFBRegistry.Port != nil {
			env := []corev1.EnvVar{
				{
					Name:  "NGINX_PORT",
					Value: fmt.Sprintf("%d", *rfCfg.BFBRegistry.Port),
				},
			}
			edit.AddForKindS(DaemonSetKind, EnvForDaemonSetContainerEdit("nginx", env))
		}
		if err := edit.Apply(objsCopyRegistry); err != nil {
			return nil, err
		}
		for i := range objsCopyRegistry {
			ret = append(ret, objsCopyRegistry[i])
		}
	}

	// apply edits
	// TODO: make it generic to not edit every kind one-by-one.
	if err := NewEdits().
		AddForAll(NamespaceEdit(vars.Namespace),
			LabelsEdit(labelsToAdd)).
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

	// Add the ApplySet to the manifests if this hasn't been disabled.
	if !opts.skipApplySet {
		ret = append(ret, applySetParentForComponent(p, applySetID, vars, applySetInventoryString(objsCopy...)))
	}
	return ret, nil
}

func (p *provisioningControllerObjects) dpfProvisioningDeploymentEdit(vars Variables) StructuredEdit {
	return func(obj client.Object) error {
		deployment, ok := obj.(*appsv1.Deployment)
		if !ok {
			return fmt.Errorf("unexpected object %s. expected Deployment", obj.GetObjectKind().GroupVersionKind())
		}

		mods := []func(*appsv1.Deployment, Variables) error{
			p.setBFBPersistentVolumeClaim,
			p.setImagePullSecrets,
			p.setComponentLabel,
			p.setDefaultImageNames,
			p.setDMSTimeout,
			p.addBFCFGConfigMapMountEdit,
			p.setCustomCASecretName,
			p.setInstallInterface,
			p.setKubernetesAPIServerEnvVars,
			p.setMaxDPUParallelInstallations,
			p.setMultiDPUOperationsSyncWaitTime,
			p.setMaxUnavailableDPUNodes,
			p.setResources,
		}
		for _, mod := range mods {
			if err := mod(deployment, vars); err != nil {
				return fmt.Errorf("error while updating Deployment for Provisioning Controller: %w", err)
			}
		}
		return nil
	}
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
func (p *provisioningControllerObjects) setResources(deployment *appsv1.Deployment, vars Variables) error {
	if resources, exists := vars.Resources[operatorv1.ProvisioningControllerName]; exists {
		// Check if resources are set (either requests or limits)
		if len(resources.Requests) > 0 || len(resources.Limits) > 0 {
			container := p.getContainer(deployment)
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
	deployment := &appsv1.Deployment{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.UnstructuredContent(), deployment); err != nil {
		return fmt.Errorf("error while parsing Deployment for Provisioning Controller: %w", err)
	}

	vol := p.getVolume(deployment, "bfb-volume")
	if vol == nil {
		return fmt.Errorf("invalid Provisioning Controller deployment, no bfb volume found")
	}
	c := p.getContainer(deployment)
	if c == nil {
		return fmt.Errorf("container %q not found in Provisioning Controller deployment", dpfProvisioningControllerContainerName)
	}
	return nil
}

func (p *provisioningControllerObjects) getContainer(deploy *appsv1.Deployment) *corev1.Container {
	for i, c := range deploy.Spec.Template.Spec.Containers {
		if c.Name == dpfProvisioningControllerContainerName {
			return &deploy.Spec.Template.Spec.Containers[i]
		}
	}
	return nil
}

func (p *provisioningControllerObjects) getVolume(deploy *appsv1.Deployment, volName string) *corev1.Volume {
	for i, vol := range deploy.Spec.Template.Spec.Volumes {
		if vol.Name == volName && vol.PersistentVolumeClaim != nil {
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
	vol.PersistentVolumeClaim.ClaimName = vars.DPFProvisioningController.BFBPersistentVolumeClaimName
	c := p.getContainer(deploy)
	if c == nil {
		return fmt.Errorf("container %q not found in Provisioning Controller deployment", dpfProvisioningControllerContainerName)
	}
	return p.setFlags(c, fmt.Sprintf("--bfb-pvc=%s", vol.PersistentVolumeClaim.ClaimName))
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
	c := p.getContainer(deploy)
	if c == nil {
		return fmt.Errorf("container %q not found in Provisioning Controller deployment", dpfProvisioningControllerContainerName)
	}
	return p.setFlags(c, fmt.Sprintf("--dms-pod-envs=%s", strings.Join(envVars, ",")))
}

func (p *provisioningControllerObjects) setMaxDPUParallelInstallations(deploy *appsv1.Deployment, vars Variables) error {
	c := p.getContainer(deploy)
	if c == nil {
		return fmt.Errorf("container %q not found in Provisioning Controller deployment", dpfProvisioningControllerContainerName)
	}
	if vars.DPFProvisioningController.MaxDPUParallelInstallations == nil {
		return nil
	}
	return p.setFlags(c, fmt.Sprintf("--max-dpu-parallel-installations=%d", *vars.DPFProvisioningController.MaxDPUParallelInstallations))
}

func (p *provisioningControllerObjects) setImagePullSecrets(deploy *appsv1.Deployment, vars Variables) error {
	c := p.getContainer(deploy)
	if c == nil {
		return fmt.Errorf("container %q not found in Provisioning Controller deployment", dpfProvisioningControllerContainerName)
	}
	if len(vars.ImagePullSecrets) == 0 {
		return nil
	}
	return p.setFlags(c, fmt.Sprintf("--image-pull-secrets=%s", strings.Join(vars.ImagePullSecrets, ",")))
}

func (p *provisioningControllerObjects) setCustomCASecretName(deploy *appsv1.Deployment, vars Variables) error {
	c := p.getContainer(deploy)
	if c == nil {
		return fmt.Errorf("container %q not found in Provisioning Controller deployment", dpfProvisioningControllerContainerName)
	}
	if vars.DPFProvisioningController.CustomCASecretName == nil {
		return nil
	}
	return p.setFlags(c, fmt.Sprintf("--custom-CA-secret=%s", *vars.DPFProvisioningController.CustomCASecretName))
}

func (p *provisioningControllerObjects) setInstallInterface(deploy *appsv1.Deployment, vars Variables) error {
	c := p.getContainer(deploy)
	if c == nil {
		return fmt.Errorf("container %q not found in Provisioning Controller deployment", dpfProvisioningControllerContainerName)
	}
	if vars.DPFProvisioningController.InstallInterface == nil ||
		vars.DPFProvisioningController.InstallInterface.InstallViaGNOI != nil {
		return p.setFlags(c, fmt.Sprintf("--dpu-install-interface=%s", provisioningv1.InstallViaGNOI))
	} else if vars.DPFProvisioningController.InstallInterface.InstallViaRedfish != nil {
		err := p.setFlags(c, fmt.Sprintf("--dpu-install-interface=%s", provisioningv1.InstallViaRedFish))
		if err != nil {
			return err
		}
		return p.setFlags(c, fmt.Sprintf("--bfb-registry=%s", vars.DPFProvisioningController.InstallInterface.InstallViaRedfish.BFBRegistryAddress))
	}
	return fmt.Errorf("provisioning controller install interface not set")
}

func (p *provisioningControllerObjects) setDMSTimeout(deploy *appsv1.Deployment, vars Variables) error {
	t := vars.DPFProvisioningController.DMSTimeout
	if t == nil {
		return nil
	}
	c := p.getContainer(deploy)
	if c == nil {
		return fmt.Errorf("container %q not found in Provisioning Controller deployment", dpfProvisioningControllerContainerName)
	}
	return p.setFlags(c, fmt.Sprintf("--dms-timeout=%d", *vars.DPFProvisioningController.DMSTimeout))
}

func (p *provisioningControllerObjects) setFlags(c *corev1.Container, newFlags ...string) error {
	pending := make(map[string]string)
	for _, f := range newFlags {
		name := p.parseFlagName(f)
		if name == "" {
			return fmt.Errorf("invalid flag %s", f)
		}
		pending[name] = f
	}
	for i, f := range c.Args {
		name := p.parseFlagName(f)
		if name == "" {
			continue
		}
		newFlag, ok := pending[name]
		if !ok {
			continue
		}
		delete(pending, name)
		c.Args[i] = newFlag
	}
	for _, flag := range pending {
		c.Args = append(c.Args, flag)
	}
	return nil
}

func (p *provisioningControllerObjects) parseFlagName(arg string) string {
	if len(arg) < 2 || arg[0] != '-' {
		return ""
	}
	numMinuses := 1
	if arg[1] == '-' {
		numMinuses++
	}
	name := arg[numMinuses:]
	if len(name) == 0 || name[0] == '-' || name[0] == '=' {
		return ""
	}
	for i := 1; i < len(name); i++ {
		if name[i] == '=' {
			return name[:i]
		}
	}
	return name
}

func (p *provisioningControllerObjects) setDefaultImageNames(deployment *appsv1.Deployment, vars Variables) error {
	c := p.getContainer(deployment)
	if c == nil {
		return fmt.Errorf("container %q not found in Provisioning Controller deployment", dpfProvisioningControllerContainerName)
	}
	defaults := release.NewDefaults()
	err := defaults.Parse()
	if err != nil {
		return err
	}
	imageName, ok := vars.Images[p.Name()]
	if !ok {
		return fmt.Errorf("image for %q not found in variables", p.Name())
	}
	c.Image = imageName
	err = p.setFlags(c, fmt.Sprintf("--dms-image=%s", defaults.DMSImage))
	if err != nil {
		return err
	}
	return nil
}

func (p *provisioningControllerObjects) setMultiDPUOperationsSyncWaitTime(deploy *appsv1.Deployment, vars Variables) error {
	c := p.getContainer(deploy)
	if c == nil {
		return fmt.Errorf("container %q not found in Provisioning Controller deployment", dpfProvisioningControllerContainerName)
	}
	return p.setFlags(c, fmt.Sprintf("--multi-dpu-operations-sync-wait-time=%s", vars.DPFProvisioningController.MultiDPUOperationsSyncWaitTime.String()))
}

func (p *provisioningControllerObjects) setMaxUnavailableDPUNodes(deploy *appsv1.Deployment, vars Variables) error {
	c := p.getContainer(deploy)
	if c == nil {
		return fmt.Errorf("container %q not found in Provisioning Controller deployment", dpfProvisioningControllerContainerName)
	}
	if vars.DPFProvisioningController.MaxUnavailableDPUNodes == nil {
		return nil
	}
	return p.setFlags(c, fmt.Sprintf("--max-unavailable-dpu-nodes=%d", *vars.DPFProvisioningController.MaxUnavailableDPUNodes))
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
