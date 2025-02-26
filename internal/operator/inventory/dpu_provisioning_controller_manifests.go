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
	"strings"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/internal/operator/utils"
	"github.com/nvidia/doca-platform/internal/release"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	dpfProvisioningControllerName          = "dpf-provisioning-controller-manager"
	dpfProvisioningControllerContainerName = "manager"
	bfbVolumeName                          = "bfb-volume"
	webhookServiceName                     = "dpf-provisioning-webhook-service"
)

var _ Component = &provisioningControllerObjects{}

// provisioningControllerObjects contains objects that are used to generate Provisioning Controller manifests.
// provisioningControllerObjects objects should be immutable after Parse()
type provisioningControllerObjects struct {
	data    []byte
	objects []*unstructured.Unstructured
}

func (p *provisioningControllerObjects) Name() string {
	return operatorv1.ProvisioningControllerName
}

// Parse returns typed objects for the Provisioning controller deployment.
func (p *provisioningControllerObjects) Parse() (err error) {
	if p.data == nil {
		return fmt.Errorf("provisioningControllerObjects.data can not be empty")
	}
	objs, err := utils.BytesToUnstructured(p.data)
	if err != nil {
		return fmt.Errorf("error while converting DPU Provisioning Controller manifests to objects: %w", err)
	} else if len(objs) == 0 {
		return fmt.Errorf("no objects found in DPU Provisioning Controller manifests")
	}

	deploymentFound := false
	for _, obj := range objs {
		// Exclude Namespace and CustomResourceDefinition as the operator should not deploy these resources.
		if obj.GetKind() == string(NamespaceKind) || obj.GetKind() == string(CustomResourceDefinitionKind) {
			continue
		}
		// If the object is the dpf-provisioning-controller-manager Deployment validate it
		if obj.GetKind() == string(DeploymentKind) && obj.GetName() == dpfProvisioningControllerName {
			deploymentFound = true
			err = p.validateDeployment(obj)
			if err != nil {
				return err
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

	labelsToAdd := map[string]string{operatorv1.DPFComponentLabelKey: p.Name()}
	applySetID := ApplySetID(vars.Namespace, p)
	// Add the ApplySet to the manifests if this hasn't been disabled.
	if !opts.skipApplySet {
		labelsToAdd[applysetPartOfLabel] = applySetID
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

	// Add the ApplySet to the manifests if this hasn't been disabled.
	if !opts.skipApplySet {
		ret = append(ret, applySetParentForComponent(p, applySetID, vars, applySetInventoryString(objsCopy...)))
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
			p.setImagePullSecrets,
			p.setComponentLabel,
			p.setDefaultImageNames,
			p.setDMSTimeout,
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
	labels[operatorv1.DPFComponentLabelKey] = dpfProvisioningControllerName
	deployment.Spec.Template.ObjectMeta.Labels = labels
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
	selector[operatorv1.DPFComponentLabelKey] = dpfProvisioningControllerName
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
	err = p.setFlags(c, fmt.Sprintf("--hostnetwork-image=%s", defaults.HostNetworkSetupImage))
	if err != nil {
		return err
	}
	return nil
}

// IsReady reports the readiness of the provisioning controller objects. It returns an error when the number of Replicas in
// the single provisioning controller deployment is true.
func (p *provisioningControllerObjects) IsReady(ctx context.Context, c client.Client, namespace string) error {
	return deploymentReadyCheck(ctx, c, namespace, p.objects)
}
