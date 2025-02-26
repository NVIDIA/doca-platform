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

import "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

var _ Component = &clusterManagerObjects{}

type clusterManagerObjects struct {
	simpleDeploymentObjects
}

func newClusterManagerObjects(name string, data []byte) *clusterManagerObjects {
	m := &clusterManagerObjects{}
	m.name = name
	m.data = data
	// disabled if the provisioning-controller is disabled
	m.isDisabled = func(disableComponents map[string]bool) bool {
		return disableComponents[name]
	}
	m.edit = func(objs []*unstructured.Unstructured, vars Variables, labelsToAdd map[string]string) error {
		return NewEdits().
			AddForAll(NamespaceEdit(vars.Namespace), LabelsEdit(labelsToAdd)).
			AddForKindS(DeploymentKind, ImagePullSecretsEditForDeploymentEdit(vars.ImagePullSecrets...)).
			AddForKindS(DeploymentKind, TolerationsEdit(controlPlaneTolerations)).
			AddForKindS(DeploymentKind, NodeAffinityEdit(&controlPlaneNodeAffinity)).
			AddForKindS(DeploymentKind, ImageForDeploymentContainerEdit("manager", vars.Images[name])).
			Apply(objs)
	}
	return m
}
