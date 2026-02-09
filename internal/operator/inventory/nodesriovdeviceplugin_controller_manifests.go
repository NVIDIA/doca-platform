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
	"fmt"
	"strings"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ Component = &nodeSRIOVDevicePluginControllerObjects{}

type nodeSRIOVDevicePluginControllerObjects struct {
	simpleDeploymentObjects
}

func newNodeSRIOVDevicePluginControllerObjects(data []byte) *nodeSRIOVDevicePluginControllerObjects {
	m := &nodeSRIOVDevicePluginControllerObjects{}
	m.name = operatorv1.NodeSRIOVDevicePluginControllerName
	m.data = data
	m.isDisabled = func(disableComponents map[operatorv1.ComponentName]bool) bool {
		return disableComponents[operatorv1.NodeSRIOVDevicePluginControllerName]
	}
	imageName := operatorv1.NodeSRIOVDevicePluginControllerName.WithContainer(operatorv1.ControllerManagerContainer)
	m.edit = func(objs []*unstructured.Unstructured, vars Variables, labelsToAdd map[string]string) error {
		ownerConfigMapName, err := getOwnerConfigMapName(objs)
		if err != nil {
			return err
		}

		edits := NewEdits().
			AddForAll(NamespaceEdit(vars.Namespace), LabelsEdit(labelsToAdd)).
			AddForKindS(DeploymentKind, ImagePullSecretsEditForDeploymentEdit(vars.ImagePullSecrets...)).
			AddForKindS(DeploymentKind, TolerationsEdit(controlPlaneTolerations)).
			AddForKindS(DeploymentKind, NodeAffinityEdit(&controlPlaneNodeAffinity)).
			AddForKindS(DeploymentKind, ImageForDeploymentContainerEdit(operatorv1.ControllerManagerContainer.String(), vars.Images[imageName])).
			AddForKindS(DeploymentKind, nodeSRIOVDevicePluginControllerArgsEdit(vars, ownerConfigMapName))

		if resources, exists := vars.Resources[imageName]; exists {
			if len(resources.Requests) > 0 || len(resources.Limits) > 0 {
				edits = edits.AddForKindS(DeploymentKind, ResourcesEditForDeployment(managerContainerName, resources))
			}
		}

		if replicas, exists := vars.Replicas[operatorv1.NodeSRIOVDevicePluginControllerName]; exists && replicas != nil {
			edits = edits.AddForKindS(DeploymentKind, ReplicasEditForDeployment(replicas))
		}

		return edits.Apply(objs)
	}
	return m
}

// getOwnerConfigMapName returns the name of the first ConfigMap in the given objects.
func getOwnerConfigMapName(objs []*unstructured.Unstructured) (string, error) {
	for _, obj := range objs {
		if obj.GetKind() != "ConfigMap" {
			continue
		}
		return obj.GetName(), nil
	}
	return "", fmt.Errorf("no ConfigMap found in %s manifests", operatorv1.NodeSRIOVDevicePluginControllerName)
}

// nodeSRIOVDevicePluginControllerArgsEdit returns an edit function that adds device plugin config args
// to the controller deployment.
func nodeSRIOVDevicePluginControllerArgsEdit(vars Variables, ownerConfigMapName string) StructuredEdit {
	return func(obj client.Object) error {
		deployment, ok := obj.(*appsv1.Deployment)
		if !ok {
			return fmt.Errorf("unexpected object %s. expected Deployment", obj.GetObjectKind().GroupVersionKind())
		}

		c := getManagerContainer(deployment)
		if c == nil {
			return fmt.Errorf("container %q not found in NodeSRIOVDevicePluginController deployment",
				managerContainerName)
		}

		// Set namespace flag.
		if err := setFlags(c, fmt.Sprintf("--namespace=%s", vars.Namespace)); err != nil {
			return err
		}

		// Set device plugin configuration flags from Variables.
		dpConfig := vars.NodeSRIOVDevicePluginController
		if dpConfig.DevicePluginImage != "" {
			if err := setFlags(c, fmt.Sprintf("--device-plugin-image=%s", dpConfig.DevicePluginImage)); err != nil {
				return err
			}
		}
		if dpConfig.DevicePluginInitImage != "" {
			if err := setFlags(c, fmt.Sprintf("--device-plugin-init-image=%s", dpConfig.DevicePluginInitImage)); err != nil {
				return err
			}
		}
		if dpConfig.DefaultResourcePrefix != "" {
			if err := setFlags(c, fmt.Sprintf("--default-resource-prefix=%s", dpConfig.DefaultResourcePrefix)); err != nil {
				return err
			}
		}
		if len(vars.ImagePullSecrets) > 0 {
			if err := setFlags(c, fmt.Sprintf("--image-pull-secrets=%s",
				strings.Join(vars.ImagePullSecrets, ","))); err != nil {
				return err
			}
		}
		if err := setFlags(c, fmt.Sprintf("--owner-configmap-name=%s", ownerConfigMapName)); err != nil {
			return err
		}

		return nil
	}
}
