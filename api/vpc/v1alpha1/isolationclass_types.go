/*
COPYRIGHT 2025 NVIDIA

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

//nolint:dupl
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// IsolationClassKind is the kind of the IsolationClass object.
	IsolationClassKind = "IsolationClass"
	// IsolationClassListKind is the kind of the IsolationClass list object.
	IsolationClassListKind = "IsolationClassList"
)

// IsolationClassGroupVersionKind is the GroupVersionKind of the IsolationClass object.
var IsolationClassGroupVersionKind = GroupVersion.WithKind(IsolationClassKind)

// IsolationClassSpec defines the configuration of IsolationClass
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="IsolationClass spec is immutable"
type IsolationClassSpec struct {
	// Provisioner indicates the type of the provisioner.
	// +required
	Provisioner string `json:"provisioner"`
	// Parameters holds the parameters for the provisioner
	// +optional
	Parameters map[string]string `json:"parameters,omitempty"`
}

// IsolationClassStatus defines the status of IsolationClass
type IsolationClassStatus struct {
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep
// +kubebuilder:resource:scope=Cluster

// IsolationClass is the Schema for the isolationclass API
type IsolationClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IsolationClassSpec   `json:"spec,omitempty"`
	Status IsolationClassStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// IsolationClassList contains a list of IsolationClass
type IsolationClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IsolationClass `json:"items"`
}

func init() {
	SchemeBuilder.Register(&IsolationClass{}, &IsolationClassList{})
}
