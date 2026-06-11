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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// DPUFlavorTemplateKind is the kind of the DPUFlavorTemplate object
	DPUFlavorTemplateKind = "DPUFlavorTemplate"
)

// DPUFlavorTemplateGroupVersionKind is the GroupVersionKind of the DPUFlavorTemplate object
var DPUFlavorTemplateGroupVersionKind = GroupVersion.WithKind(DPUFlavorTemplateKind)

// DPUFlavorTemplateSpec defines the content of a DPUFlavorTemplate. The template body is
// rendered per-DPU against DPUDevice.spec.values to produce a concrete DPUFlavor.
type DPUFlavorTemplateSpec struct {
	// Template is the DPUFlavor body as a YAML/JSON string with Go template actions
	// (delimited by double curly braces). It is rendered against DPUDevice.spec.values and the result is
	// unmarshalled into a typed DPUFlavor and validated by DPUFlavor admission when
	// the generated flavor is created. It must NOT contain dpuResources or
	// systemReservedResources; those are provided by the structured fields below.
	// +kubebuilder:validation:MinLength=1
	// +required
	Template string `json:"template"`

	// DPUResources is resource-fitting metadata mirrored from DPUFlavor. It is NOT
	// templated: when set it is stamped onto every generated DPUFlavor and takes
	// precedence over anything in the rendered body.
	// +optional
	DPUResources corev1.ResourceList `json:"dpuResources,omitempty"`

	// SystemReservedResources is resource-fitting metadata mirrored from DPUFlavor. It is
	// NOT templated: when set it is stamped onto every generated DPUFlavor and takes
	// precedence over anything in the rendered body.
	// +optional
	SystemReservedResources corev1.ResourceList `json:"systemReservedResources,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep

// DPUFlavorTemplate is the Schema for the dpuflavortemplates API
type DPUFlavorTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec DPUFlavorTemplateSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// DPUFlavorTemplateList contains a list of DPUFlavorTemplate
type DPUFlavorTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DPUFlavorTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DPUFlavorTemplate{}, &DPUFlavorTemplateList{})
}
