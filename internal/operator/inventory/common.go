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
func deploymentReadyCheck(ctx context.Context, c client.Client, namespace string, objects []*unstructured.Unstructured) error {
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
	name       string
	data       []byte
	objects    []*unstructured.Unstructured
	isDisabled func(map[string]bool) bool
	edit       func([]*unstructured.Unstructured, Variables, map[string]string) error
}

func (p *simpleDeploymentObjects) Name() string {
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

func (p *simpleDeploymentObjects) GenerateManifests(vars Variables, options ...GenerateManifestOption) ([]client.Object, error) {
	if p.isDisabled != nil && p.isDisabled(vars.DisableSystemComponents) {
		return []client.Object{}, nil
	}

	opts := &GenerateManifestOptions{}
	for _, option := range options {
		option.Apply(opts)
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
	if p.edit != nil {
		if err := p.edit(objsCopy, vars, labelsToAdd); err != nil {
			return nil, err
		}
	}

	// return as Objects
	ret := []client.Object{}
	if !opts.skipApplySet {
		ret = append(ret, applySetParentForComponent(p, applySetID, vars, applySetInventoryString(objsCopy...)))
	}

	for i := range objsCopy {
		ret = append(ret, objsCopy[i])
	}

	return ret, nil
}

func (p *simpleDeploymentObjects) IsReady(ctx context.Context, c client.Client, namespace string) error {
	return deploymentReadyCheck(ctx, c, namespace, p.objects)
}

// nolint
// daemonsetReadyCheck can be used to check for readiness of an inventory Component that has exactly one Kubernetes DaemonSet of interest.
// The Component returns and error when the number of Ready is zero or below the current number of desiredNumberScheduled.
func daemonsetReadyCheck(ctx context.Context, c client.Client, namespace string, objects []*unstructured.Unstructured) error {
	deamonset := &appsv1.DaemonSet{}
	found := false
	for _, obj := range objects {
		if obj.GetKind() == string(DaemonsetKind) {
			err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: obj.GetName()}, deamonset)
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

	// Consider the deployment not ready if it has no replicas.
	if deamonset.Status.NumberReady == 0 {
		return fmt.Errorf("Daemonset %s/%s has no ready replicas", deamonset.GetNamespace(), deamonset.GetName())
	}
	if deamonset.Status.NumberReady != deamonset.Status.DesiredNumberScheduled {
		return fmt.Errorf("Daemonset %s/%s has %d ready, want %d",
			deamonset.GetNamespace(), deamonset.GetName(), deamonset.Status.NumberReady, deamonset.Status.DesiredNumberScheduled)
	}
	return nil
}
