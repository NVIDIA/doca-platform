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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// DPUServiceTemplateKind is the kind of the DPUServiceTemplate object
	DPUServiceTemplateKind = "DPUServiceTemplate"
)

// DPUServiceTemplateGroupVersionKind is the GroupVersionKind of the DPUServiceTemplate object
var DPUServiceTemplateGroupVersionKind = GroupVersion.WithKind(DPUServiceTemplateKind)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep

// DPUServiceTemplate is the Schema for the DPUServiceTemplate API. This object is intended to be used in
// conjunction with a DPUDeployment object. This object is the template from which the DPUService will be created. It
// contains configuration options related to resources required by the service to be deployed. The rest of the
// configuration options must be defined in a DPUServiceConfiguration object.
type DPUServiceTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DPUServiceTemplateSpec   `json:"spec,omitempty"`
	Status DPUServiceTemplateStatus `json:"status,omitempty"`
}

// DPUServiceTemplateSpec defines the desired state of DPUServiceTemplate
type DPUServiceTemplateSpec struct {
	// DeploymentServiceName is the name of the DPU service this configuration refers to. It must match
	// .spec.deploymentServiceName of a DPUServiceConfiguration object and one of the keys in .spec.services of a
	// DPUDeployment object.
	// +required
	DeploymentServiceName string `json:"deploymentServiceName"`
	// HelmChart reflects the Helm related configuration. The user is supposed to configure the values that are static
	// across any DPUServiceConfiguration used with this DPUServiceTemplate in a DPUDeployment. These values act as a
	// baseline and are merged with values specified in the DPUServiceConfiguration. In case of conflict, the
	// DPUServiceConfiguration values take precedence.
	// +required
	HelmChart HelmChart `json:"helmChart"`
	// ResourceRequirements contains the overall resources required by this particular service to run on a single node
	// +optional
	ResourceRequirements corev1.ResourceList `json:"resourceRequirements,omitempty"`
}

// DPUServiceTemplateStatus defines the observed state of DPUServiceTemplate
type DPUServiceTemplateStatus struct{}

// +kubebuilder:object:root=true

// DPUServiceTemplateList contains a list of DPUServiceTemplate
type DPUServiceTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DPUServiceTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DPUServiceTemplate{}, &DPUServiceTemplateList{})
}
