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
	"errors"
	"fmt"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/internal/operator/utils"
	"github.com/nvidia/doca-platform/internal/release"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ObjectKind represents different Kind of Kubernetes Objects
type ObjectKind string

func (o ObjectKind) String() string {
	return string(o)
}

const (
	NamespaceKind                      ObjectKind = "Namespace"
	CustomResourceDefinitionKind       ObjectKind = "CustomResourceDefinition"
	ClusterRoleKind                    ObjectKind = "ClusterRole"
	RoleBindingKind                    ObjectKind = "RoleBinding"
	ClusterRoleBindingKind             ObjectKind = "ClusterRoleBinding"
	MutatingWebhookConfigurationKind   ObjectKind = "MutatingWebhookConfiguration"
	ValidatingWebhookConfigurationKind ObjectKind = "ValidatingWebhookConfiguration"
	DeploymentKind                     ObjectKind = "Deployment"
	StatefulSetKind                    ObjectKind = "StatefulSet"
	DaemonSetKind                      ObjectKind = "DaemonSet"
	CertificateKind                    ObjectKind = "Certificate"
	IssuerKind                         ObjectKind = "Issuer"
	RoleKind                           ObjectKind = "Role"
	ServiceAccountKind                 ObjectKind = "ServiceAccount"
	ServiceKind                        ObjectKind = "Service"
	DPUServiceKind                     ObjectKind = "DPUService"
	DaemonsetKind                      ObjectKind = "DaemonSet"

	kubernetesNodeRoleMaster       = "node-role.kubernetes.io/master"
	kubernetesNodeRoleControlPlane = "node-role.kubernetes.io/control-plane"
	nodeNotReadyTaint              = "node.kubernetes.io/not-ready"

	// managerContainerName is the name of the CR controller manager container.
	managerContainerName = "manager"
)

var (
	controlPlaneNodeAffinity = corev1.NodeAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
			NodeSelectorTerms: []corev1.NodeSelectorTerm{
				{
					MatchExpressions: []corev1.NodeSelectorRequirement{
						{
							Key:      kubernetesNodeRoleMaster,
							Operator: corev1.NodeSelectorOpExists,
						},
					},
				},
				{
					MatchExpressions: []corev1.NodeSelectorRequirement{
						{
							Key:      kubernetesNodeRoleControlPlane,
							Operator: corev1.NodeSelectorOpExists,
						},
					},
				},
			},
		},
	}
	controlPlaneTolerations = []corev1.Toleration{
		{
			Key:      kubernetesNodeRoleMaster,
			Operator: corev1.TolerationOpExists,
			Effect:   corev1.TaintEffectNoSchedule,
		},
		{
			Key:      kubernetesNodeRoleControlPlane,
			Operator: corev1.TolerationOpExists,
			Effect:   corev1.TaintEffectNoSchedule,
		},
	}
	nodeNotReadyTolerations = []corev1.Toleration{
		{
			Key:      nodeNotReadyTaint,
			Operator: corev1.TolerationOpExists,
			Effect:   corev1.TaintEffectNoSchedule,
		},
	}
)

func localObjRefsFromStrings(names ...string) []corev1.LocalObjectReference {
	localObjectRefs := make([]corev1.LocalObjectReference, 0, len(names))
	for _, name := range names {
		localObjectRefs = append(localObjectRefs, corev1.LocalObjectReference{Name: name})
	}
	return localObjectRefs
}

// nolint
// deploymentReadyCheck can be used to check for readiness of an inventory Component that has exactly one Kubernetes Deployment of interest.
// The Component returns and error when the number of ReadyReplicas is zero or below the current number of Replicas.
func deploymentReadyCheck(ctx context.Context, c client.Client, namespace string, objects []*unstructured.Unstructured, versionValidation bool) error {
	deployment := &appsv1.Deployment{}
	found := false
	for _, obj := range objects {
		if obj.GetKind() == string(DeploymentKind) {
			err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: obj.GetName()}, deployment)
			if err != nil {
				return err
			}
			found = true
			break
		}
	}
	if !found {
		return errors.New("deployment not found in objects")
	}

	if versionValidation {
		if deployment.GetLabels()[release.DPFVersionLabelKey] != "" && deployment.GetLabels()[release.DPFVersionLabelKey] != release.DPFVersion() {
			return fmt.Errorf("Deployment %s/%s has version %s, want %s",
				deployment.GetNamespace(), deployment.GetName(), deployment.GetLabels()[release.DPFVersionLabelKey], release.DPFVersion())
		}
	}
	if deployment.GetGeneration() != deployment.Status.ObservedGeneration {
		return fmt.Errorf("Deployment %s/%s is not ready: generation is not equal to observed generation", deployment.Namespace, deployment.Name)
	}
	// Consider the deployment not ready if it has no replicas.
	if deployment.Status.Replicas == 0 {
		return fmt.Errorf("Deployment %s/%s has no replicas", deployment.GetNamespace(), deployment.GetName())
	}
	if deployment.Status.ReadyReplicas != deployment.Status.Replicas {
		return fmt.Errorf("Deployment %s/%s has %d readyReplicas, want %d",
			deployment.GetNamespace(), deployment.GetName(), deployment.Status.ReadyReplicas, deployment.Status.Replicas)
	}
	return nil
}

func isClusterScoped(kind string) bool {
	clusterScopedKinds := map[string]interface{}{
		string(ClusterRoleKind):                    nil,
		string(ClusterRoleBindingKind):             nil,
		string(MutatingWebhookConfigurationKind):   nil,
		string(ValidatingWebhookConfigurationKind): nil,
	}
	_, ok := clusterScopedKinds[kind]
	return ok
}

var _ Component = &simpleDeploymentObjects{}

type simpleDeploymentObjects struct {
	name       operatorv1.ComponentName
	data       []byte
	objects    []*unstructured.Unstructured
	isDisabled func(map[operatorv1.ComponentName]bool) bool
	edit       func([]*unstructured.Unstructured, Variables, map[string]string) error
}

func (p *simpleDeploymentObjects) Name() operatorv1.ComponentName {
	return p.name
}

func (p *simpleDeploymentObjects) Parse() (err error) {
	if p.data == nil {
		return fmt.Errorf("%s.data can not be empty", p.Name())
	}
	objs, err := utils.BytesToUnstructured(p.data)
	if err != nil {
		return fmt.Errorf("error while converting %s manifests to objects: %w", p.Name(), err)
	} else if len(objs) == 0 {
		return fmt.Errorf("no objects found in %s manifests", p.Name())
	}

	deploymentFound := false
	for _, obj := range objs {
		// Exclude Namespace and CustomResourceDefinition as the operator should not deploy these resources.
		if obj.GetKind() == string(NamespaceKind) || obj.GetKind() == string(CustomResourceDefinitionKind) {
			continue
		}
		// If the object is the dpf-provisioning-controller-manager Deployment validate it
		if obj.GetKind() == string(DeploymentKind) {
			deploymentFound = true
		}
		p.objects = append(p.objects, obj)
	}
	if !deploymentFound {
		return fmt.Errorf("error while converting %s manifests to objects: Deployment not found", p.Name())
	}
	return nil
}

func (p *simpleDeploymentObjects) GenerateManifests(_ context.Context, vars Variables) ([]client.Object, error) {
	if p.isDisabled != nil && p.isDisabled(vars.DisableSystemComponents) {
		return []client.Object{}, nil
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

	// apply edits
	if p.edit != nil {
		if err := p.edit(objsCopy, vars, labelsToAdd); err != nil {
			return nil, err
		}
	}

	// return as Objects
	ret := []client.Object{}
	for i := range objsCopy {
		ret = append(ret, objsCopy[i])
	}

	return ret, nil
}

func (p *simpleDeploymentObjects) IsReadyForUpgrade(ctx context.Context, c client.Client, config *operatorv1.DPFOperatorConfig) error {
	return deploymentReadyCheck(ctx, c, config.GetNamespace(), p.objects, false)
}

func (p *simpleDeploymentObjects) IsReady(ctx context.Context, c client.Client, namespace string) error {
	return deploymentReadyCheck(ctx, c, namespace, p.objects, true)
}

// nolint
// daemonSetReadyCheck can be used to check for readiness of an inventory Component that has exactly one Kubernetes DaemonSet of interest.
// The Component returns and error when the number of Ready is zero or below the current number of desiredNumberScheduled.
func daemonSetReadyCheck(ctx context.Context, c client.Client, namespace string, objects []*unstructured.Unstructured, versionValidation bool) error {
	daemonset := &appsv1.DaemonSet{}
	found := false
	for _, obj := range objects {
		if obj.GetKind() == string(DaemonsetKind) {
			err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: obj.GetName()}, daemonset)
			if err != nil {
				return err
			}
			found = true
			break
		}
	}
	if !found {
		return errors.New("daemonset not found in objects")
	}

	if versionValidation {
		if daemonset.GetLabels()[release.DPFVersionLabelKey] != "" && daemonset.GetLabels()[release.DPFVersionLabelKey] != release.DPFVersion() {
			return fmt.Errorf("DaemonSet %s/%s has version %s, want %s",
				daemonset.GetNamespace(), daemonset.GetName(), daemonset.GetLabels()[release.DPFVersionLabelKey], release.DPFVersion())
		}
	}
	if daemonset.GetGeneration() != daemonset.Status.ObservedGeneration {
		return fmt.Errorf("DaemonSet %s/%s is not ready: generation is not equal to observed generation", daemonset.Namespace, daemonset.Name)
	}
	if daemonset.Status.UpdatedNumberScheduled != daemonset.Status.DesiredNumberScheduled ||
		daemonset.Status.NumberAvailable != daemonset.Status.DesiredNumberScheduled {
		return fmt.Errorf("DaemonSet %s/%s has %d available and %d up-to-date, want %d",
			daemonset.GetNamespace(), daemonset.GetName(), daemonset.Status.NumberAvailable, daemonset.Status.UpdatedNumberScheduled, daemonset.Status.DesiredNumberScheduled)
	}
	return nil
}

func getManagerContainer(deploy *appsv1.Deployment) *corev1.Container {
	return getContainerByName(deploy, managerContainerName)
}

func getContainerByName(deploy *appsv1.Deployment, name string) *corev1.Container {
	for i, c := range deploy.Spec.Template.Spec.Containers {
		if c.Name == name {
			return &deploy.Spec.Template.Spec.Containers[i]
		}
	}
	return nil
}

// setFlags sets or updates command line flags in a container's Args slice.
func setFlags(c *corev1.Container, newFlags ...string) error {
	pending := make(map[string]string)
	for _, f := range newFlags {
		name := parseFlagName(f)
		if name == "" {
			return fmt.Errorf("invalid flag %s", f)
		}
		pending[name] = f
	}
	for i, f := range c.Args {
		name := parseFlagName(f)
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

// parseFlagName extracts the name of a command line flag from its string representation.
// It returns an empty string if the argument is not a valid flag.
func parseFlagName(arg string) string {
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
