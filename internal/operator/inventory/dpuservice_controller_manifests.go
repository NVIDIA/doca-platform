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
	"fmt"
	"maps"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	argocd "github.com/nvidia/doca-platform/internal/argocd"
	"github.com/nvidia/doca-platform/internal/operator/utils"
	"github.com/nvidia/doca-platform/internal/release"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	argov1 "github.com/nvidia/doca-platform/third_party/forked/argoproj/argo-cd/pkg/apis/application/v1alpha1"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ Component = &dpuServiceControllerObjects{}

const (
	// DPUServiceControllerName is the helm value for the DPUService Controllers component name.
	DPUServiceControllerName = "dpuservice-controller-manager"
)

// dpuServiceControllerObjects contains Kubernetes objects to be created by the DPUService controller.
type dpuServiceControllerObjects struct {
	data    []byte
	objects []*unstructured.Unstructured
}

func (d *dpuServiceControllerObjects) Name() operatorv1.ComponentName {
	return operatorv1.DPUServiceControllerName
}

func (d *dpuServiceControllerObjects) ImageName() string {
	return operatorv1.DPUServiceControllerName.WithContainer(operatorv1.ControllerManagerContainer)
}

// Parse returns typed objects for the DPUService controller deployment.
func (d *dpuServiceControllerObjects) Parse() error {
	if d.data == nil {
		return fmt.Errorf("dpuServiceControllerObjects.data can not be empty")
	}
	var err error
	objects, err := utils.BytesToUnstructured(d.data)
	if err != nil {
		return fmt.Errorf("error while converting DPUService controller manifests to objects: %w", err)
	}

	for i, obj := range objects {
		switch ObjectKind(obj.GetKind()) {
		// Namespace and CustomResourceDefinition can not be part of the manifests.
		case NamespaceKind, CustomResourceDefinitionKind:
			return fmt.Errorf("can not parse manifest %s: %s not allowed ", obj.GetName(), obj.GetKind())
		}
		d.objects = append(d.objects, objects[i])
	}
	return nil
}

// GenerateManifests returns all objects as a list.
func (d *dpuServiceControllerObjects) GenerateManifests(ctx context.Context, vars Variables) ([]client.Object, error) {
	if ok := vars.DisableSystemComponents[d.Name()]; ok {
		return []client.Object{}, nil
	}
	// make a copy of the objects
	objsCopy := make([]*unstructured.Unstructured, 0, len(d.objects))
	for i := range d.objects {
		objsCopy = append(objsCopy, d.objects[i].DeepCopy())
	}

	labelsToAdd := map[string]string{
		operatorv1.DPFComponentLabelKey: d.Name().String(),
		release.DPFVersionLabelKey:      release.DPFVersion(),
		applysetPartOfLabel:             ApplySetID(vars.Namespace, d),
	}

	containerImage, ok := vars.Images[d.ImageName()]
	if !ok {
		return nil, fmt.Errorf("could not find image for %s in variables", d.Name())
	}
	// apply edits
	// TODO: make it generic to not edit every kind one-by-one.
	edits := NewEdits().
		AddForAll(NamespaceEdit(vars.Namespace),
			LabelsEdit(labelsToAdd)).
		AddForKindS(DeploymentKind, ImagePullSecretsEditForDeploymentEdit(vars.ImagePullSecrets...)).
		AddForKindS(DeploymentKind, d.deploymentEdit(vars)).
		AddForKindS(DeploymentKind, NodeAffinityEdit(&controlPlaneNodeAffinity)).
		AddForKindS(StatefulSetKind, NodeAffinityEdit(&controlPlaneNodeAffinity)).
		AddForKindS(DeploymentKind, TolerationsEdit(controlPlaneTolerations)).
		AddForKindS(StatefulSetKind, TolerationsEdit(controlPlaneTolerations)).
		AddForKindS(DaemonSetKind, TolerationsEdit(controlPlaneTolerations)).
		AddForKindS(DeploymentKind, ImageForDeploymentContainerEdit(operatorv1.ControllerManagerContainer.String(), containerImage))

	// Add component-specific labels, annotations, and resources
	if resources, exists := vars.Resources[d.ImageName()]; exists {
		// Check if resources are set (either requests or limits)
		if len(resources.Requests) > 0 || len(resources.Limits) > 0 {
			edits = edits.AddForKindS(DeploymentKind, ResourcesEditForDeployment(operatorv1.ControllerManagerContainer.String(), resources))
		}
	}

	if err := edits.Apply(objsCopy); err != nil {
		return nil, err
	}

	ret := []client.Object{}
	for _, obj := range objsCopy {
		ret = append(ret, obj)
	}

	// Add ArgoCD AppProject manifests (one for the host cluster and one for all DPU clusters).
	argoCDProjects := d.generateArgoCDProjects(vars.ArgoCDNamespace, labelsToAdd, vars.DPUClusters, vars.Namespace)
	ret = append(ret, argoCDProjects...)

	// Add ArgoCD cluster secrets for all DPU clusters.
	argoCDSecrets, err := d.generateArgoCDClusterSecrets(ctx, vars.ArgoCDNamespace, labelsToAdd, vars.DPUClusters)
	if err != nil {
		return nil, fmt.Errorf("generating ArgoCD cluster secrets: %w", err)
	}
	ret = append(ret, argoCDSecrets...)

	return ret, nil
}

func (d *dpuServiceControllerObjects) generateArgoCDProjects(argoCDNamespace string, labelsToAdd map[string]string, dpuClusters []*dpucluster.Config, applicationNamespace string) []client.Object {
	appProjects := []client.Object{}
	// Generate AppProject manifests for DPU and host clusters.
	clusterKeys := make([]types.NamespacedName, 0, len(dpuClusters))
	for _, dpuClusterConfig := range dpuClusters {
		clusterKeys = append(clusterKeys, types.NamespacedName{
			Namespace: dpuClusterConfig.Cluster.Namespace,
			Name:      dpuClusterConfig.Cluster.Name,
		})
	}

	for _, appProject := range []*argov1.AppProject{
		argocd.NewAppProject(argoCDNamespace, argocd.AppProjectNameDPU, clusterKeys, applicationNamespace),
		argocd.NewAppProject(argoCDNamespace, argocd.AppProjectNameHost, []types.NamespacedName{{Namespace: "*", Name: "in-cluster"}}, applicationNamespace),
	} {
		labels := appProject.GetLabels()
		maps.Copy(labels, labelsToAdd)
		appProject.SetLabels(labelsToAdd)
		appProjects = append(appProjects, appProject)
	}

	return appProjects
}

func (d *dpuServiceControllerObjects) generateArgoCDClusterSecrets(ctx context.Context, argoCDNamespace string, labelsToAdd map[string]string, dpuClusters []*dpucluster.Config) ([]client.Object, error) {
	var errs []error
	argoCDClusterSecrets := []client.Object{}
	for _, dpuClusterConfig := range dpuClusters {
		// Get the control plane kubeconfig
		adminConfig, err := dpuClusterConfig.Kubeconfig(ctx)
		if err != nil {
			// Skip in case the kubeconfig does not exist yet.
			if apierrors.IsNotFound(err) {
				continue
			}

			errs = append(errs, err)
			continue
		}

		// Template an argoCDClusterSecret using information from the control plane secret.
		argoCDClusterSecret, err := argocd.NewArgoCDClusterSecret(adminConfig, argoCDNamespace, dpuClusterConfig)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		// Add labels to the secret.
		if argoCDClusterSecret.Labels == nil {
			argoCDClusterSecret.Labels = map[string]string{}
		}
		for k, v := range labelsToAdd {
			argoCDClusterSecret.Labels[k] = v
		}

		argoCDClusterSecrets = append(argoCDClusterSecrets, argoCDClusterSecret)
	}

	return argoCDClusterSecrets, kerrors.NewAggregate(errs)
}

func (d *dpuServiceControllerObjects) deploymentEdit(vars Variables) StructuredEdit {
	return func(obj client.Object) error {
		deployment, ok := obj.(*appsv1.Deployment)
		if !ok {
			return fmt.Errorf("unexpected object %s. expected Deployment", obj.GetObjectKind().GroupVersionKind())
		}

		mods := []func(*appsv1.Deployment, Variables) error{
			d.setDPUReadyController,
		}
		for _, mod := range mods {
			if err := mod(deployment, vars); err != nil {
				return fmt.Errorf("error while updating Deployment for DPUService Controller: %w", err)
			}
		}
		return nil
	}
}

func (d *dpuServiceControllerObjects) setDPUReadyController(deploy *appsv1.Deployment, vars Variables) error {
	c := getManagerContainer(deploy)
	if c == nil {
		return fmt.Errorf("container %q not found in DPUService Controller deployment", managerContainerName)
	}
	return setFlags(c, fmt.Sprintf("--disable-dpu-ready-taints=%t", vars.DisableDPUReadyTaints))
}

// IsReadyForUpgrade reports the readiness of the dpuservice controller objects. It returns an error when the number of Replicas in
// the single provisioning controller deployment is true.
func (d *dpuServiceControllerObjects) IsReadyForUpgrade(ctx context.Context, c client.Client, config *operatorv1.DPFOperatorConfig) error {
	return deploymentReadyCheck(ctx, c, config.GetNamespace(), d.objects, false)
}

// IsReady reports the readiness of the dpuservice controller objects as well as the version state.
// It returns an error when the number of Replicas in the single provisioning controller deployment is true.
func (d *dpuServiceControllerObjects) IsReady(ctx context.Context, c client.Client, namespace string) error {
	return deploymentReadyCheck(ctx, c, namespace, d.objects, true)
}
