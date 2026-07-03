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
)

var _ Component = &provisioningControllerObjects{}

// provisioningControllerObjects contains objects that are used to generate Provisioning Controller manifests.
// provisioningControllerObjects objects should be immutable after Parse()
type provisioningControllerObjects struct {
	data                   []byte
	bfbRegistryData        []byte
	objects                []*unstructured.Unstructured
	bfbRegistryObjects     []*unstructured.Unstructured
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

	// make a copy of the objects
	objsCopy := make([]*unstructured.Unstructured, 0, len(p.objects))
	for i := range p.objects {
		objsCopy = append(objsCopy, p.objects[i].DeepCopy())
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
			p.setKubernetesAPIServerEnvVars,
			p.setMaxDPUParallelInstallations,
			p.setMultiDPUOperationsSyncWaitTime,
			p.setMaxUnavailableDPUNodes,
			p.setOSInstallTimeout,
			p.setNodeEffectRemovalTimeout,
			p.setHostAgentDNSPolicy,
			p.setResources,
			p.setBFBRegistryLoadBalancerAddress,
			p.setReplicas,
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
	c := getManagerContainer(deploy)
	if c == nil {
		return fmt.Errorf(errManagerContainerNotFoundFmt, managerContainerName)
	}
	timeout := operatorv1.DefaultOSInstallTimeout
	if vars.DPFProvisioningController.OSInstallTimeout != nil {
		timeout = vars.DPFProvisioningController.OSInstallTimeout.Duration
	}
	return setFlags(c, fmt.Sprintf("--os-install-timeout=%s", timeout.String()))
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
