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
	"fmt"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/internal/release"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ Component = &clusterManagerObjects{}

type clusterManagerObjects struct {
	simpleDeploymentObjects
}

// newClusterManagerBase creates the base cluster manager object with common fields.
func newClusterManagerBase(name operatorv1.ComponentName, data []byte) *clusterManagerObjects {
	m := &clusterManagerObjects{}
	m.name = name
	m.data = data
	m.isDisabled = func(disableComponents map[operatorv1.ComponentName]bool) bool {
		return disableComponents[name]
	}
	return m
}

// buildClusterManagerEdits builds the common Edits chain for cluster managers.
func buildClusterManagerEdits(component operatorv1.ComponentName, vars Variables, labelsToAdd map[string]string) *Edits {
	imageName := component.WithContainer(operatorv1.ControllerManagerContainer)
	edits := NewEdits().
		AddForAll(NamespaceEdit(vars.Namespace), LabelsEdit(labelsToAdd)).
		AddForKindS(DeploymentKind, ImagePullSecretsEditForDeploymentEdit(vars.ImagePullSecrets...)).
		AddForKindS(DeploymentKind, TolerationsEdit(controlPlaneTolerations)).
		AddForKindS(DeploymentKind, NodeAffinityEdit(&controlPlaneNodeAffinity)).
		AddForKindS(DeploymentKind, ImageForDeploymentContainerEdit("manager", vars.Images[imageName]))

	if resources, exists := vars.Resources[imageName]; exists {
		// Check if resources are set (either requests or limits)
		if len(resources.Requests) > 0 || len(resources.Limits) > 0 {
			edits = edits.AddForKindS(DeploymentKind, ResourcesEditForDeployment(managerContainerName, resources))
		}
	}

	// Apply replicas if specified
	if replicas, exists := vars.Replicas[component]; exists && replicas != nil {
		edits = edits.AddForKindS(DeploymentKind, ReplicasEditForDeployment(replicas))
	}

	return edits
}

// newStaticClusterManagerObjects creates Static cluster manager objects.
func newStaticClusterManagerObjects(data []byte) *clusterManagerObjects {
	m := newClusterManagerBase(operatorv1.StaticClusterManagerName, data)
	m.edit = func(objs []*unstructured.Unstructured, vars Variables, labelsToAdd map[string]string) error {
		edits := buildClusterManagerEdits(m.name, vars, labelsToAdd)
		return edits.Apply(objs)
	}
	return m
}

// newKamajiClusterManagerObjects creates Kamaji cluster manager objects with keepalived image injection.
func newKamajiClusterManagerObjects(data []byte) *clusterManagerObjects {
	m := newClusterManagerBase(operatorv1.KamajiClusterManagerName, data)
	m.edit = func(objs []*unstructured.Unstructured, vars Variables, labelsToAdd map[string]string) error {
		edits := buildClusterManagerEdits(m.name, vars, labelsToAdd).
			AddForKindS(DeploymentKind, injectKeepalivedImageEdit()) // Kamaji-specific
		return edits.Apply(objs)
	}
	return m
}

// injectKeepalivedImageEdit injects the keepalived image as a command-line argument to the kamaji cluster manager.
func injectKeepalivedImageEdit() StructuredEdit {
	return func(obj client.Object) error {
		deploy, ok := obj.(*appsv1.Deployment)
		if !ok {
			return fmt.Errorf("unexpected object type, expected Deployment")
		}

		// Read keepalived image from defaults
		defaults := release.NewDefaults()
		if err := defaults.Parse(); err != nil {
			return fmt.Errorf("failed to parse defaults: %w", err)
		}

		// Get the manager container
		c := getManagerContainer(deploy)
		if c == nil {
			return fmt.Errorf("container %q not found in deployment", managerContainerName)
		}

		// Add --keepalived-image flag to container args
		return setFlags(c, fmt.Sprintf("--keepalived-image=%s", defaults.KeepalivedImage))
	}
}
