/*
COPYRIGHT 2024 NVIDIA

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
	"github.com/nvidia/doca-platform/internal/conditions"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// ServiceInterfaceSetKind is the kind of the ServiceInterfaceSet in string format.
	ServiceInterfaceSetKind = "ServiceInterfaceSet"
	// ServiceInterfaceSetFinalizer is set on a ServiceInterfaceSet when it is first handled by
	// the controller, and removed when this object is deleted.
	ServiceInterfaceSetFinalizer = "dpu.nvidia.com/serviceinterfaceset"
)

var (
	// ServiceInterfaceSetGroupVersionKind is the GroupVersionKind of the ServiceInterfaceSet
	ServiceInterfaceSetGroupVersionKind = GroupVersion.WithKind(ServiceInterfaceSetKind)
)

// Status related variables
const (
	ConditionServiceInterfacesReconciled conditions.ConditionType = "ServiceInterfacesReconciled"
	ConditionServiceInterfacesReady      conditions.ConditionType = "ServiceInterfacesReady"
)

var (
	ServiceInterfaceSetConditions = []conditions.ConditionType{
		conditions.TypeReady,
		ConditionServiceInterfacesReconciled,
		ConditionServiceInterfacesReady,
	}
)

var _ conditions.GetSet = &ServiceInterfaceSet{}

func (c *ServiceInterfaceSet) GetConditions() []metav1.Condition {
	return c.Status.Conditions
}

func (c *ServiceInterfaceSet) SetConditions(conditions []metav1.Condition) {
	c.Status.Conditions = conditions
}

// ServiceInterfaceSetSpec defines the desired state of ServiceInterfaceSet
type ServiceInterfaceSetSpec struct {
	// Select the Nodes with specific labels, ServiceInterface CRs will be
	// created only for these Nodes
	// +optional
	NodeSelector *metav1.LabelSelector `json:"nodeSelector,omitempty"`

	// Template holds the template for the erviceInterfaceSpec
	// +required
	Template ServiceInterfaceSpecTemplate `json:"template"`
}

// GetTemplateSpec returns the ServiceInterfaceSpec from the template
func (s *ServiceInterfaceSetSpec) GetTemplateSpec() *ServiceInterfaceSpec {
	return &s.Template.Spec
}

// ServiceInterfaceSpecTemplate defines the template from which ServiceInterfaceSpecs
// are created
type ServiceInterfaceSpecTemplate struct {
	// ServiceInterfaceSpec is the spec for the ServiceInterfaceSpec
	// +required
	Spec ServiceInterfaceSpec `json:"spec"`
	// ObjectMeta holds metadata like labels and annotations.
	// +optional
	ObjectMeta `json:"metadata,omitempty"`
}

// ServiceInterfaceSetStatus defines the observed state of ServiceInterfaceSet
type ServiceInterfaceSetStatus struct {
	// Conditions reflect the status of the object
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration records the Generation observed on the object the last time it was patched.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// The number of nodes where the service chain is applied and is supposed to be applied.
	NumberApplied int32 `json:"numberApplied,omitempty"`

	// The number of nodes where the service chain is applied and ready.
	NumberReady int32 `json:"numberReady,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep
// +kubebuilder:printcolumn:name="IfType",type=string,JSONPath=`.spec.template.spec.interfaceType`
// +kubebuilder:printcolumn:name="IfName",type=string,JSONPath=`.spec.template.spec.interfaceName`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ServiceInterfaceSet is the Schema for the serviceinterfacesets API
type ServiceInterfaceSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ServiceInterfaceSetSpec   `json:"spec,omitempty"`
	Status ServiceInterfaceSetStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ServiceInterfaceSetList contains a list of ServiceInterfaceSet
type ServiceInterfaceSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ServiceInterfaceSet `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ServiceInterfaceSet{}, &ServiceInterfaceSetList{})
}
