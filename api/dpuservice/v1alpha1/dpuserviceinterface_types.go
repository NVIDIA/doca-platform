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
	DPUServiceInterfaceFinalizer = "svc.dpu.nvidia.com/dpuserviceinterface"
	DPUServiceInterfaceKind      = "DPUServiceInterface"
	DPUServiceInterfaceListKind  = "DPUServiceInterfaceList"
)

var DPUServiceInterfaceGroupVersionKind = GroupVersion.WithKind(DPUServiceInterfaceKind)

// Status related variables
const (
	ConditionServiceInterfaceSetReconciled conditions.ConditionType = "ServiceInterfaceSetReconciled"
	ConditionServiceInterfaceSetReady      conditions.ConditionType = "ServiceInterfaceSetReady"
)

var (
	DPUServiceInterfaceConditions = []conditions.ConditionType{
		conditions.TypeReady,
		ConditionServiceInterfaceSetReconciled,
		ConditionServiceInterfaceSetReady,
	}
)

var _ conditions.GetSet = &DPUServiceInterface{}

func (c *DPUServiceInterface) GetConditions() []metav1.Condition {
	return c.Status.Conditions
}

func (c *DPUServiceInterface) SetConditions(conditions []metav1.Condition) {
	c.Status.Conditions = conditions
}

// DPUServiceInterfaceSpec defines the desired state of DPUServiceInterfaceSpec
type DPUServiceInterfaceSpec struct {
	// Select the Clusters with specific labels, ServiceInterfaceSet CRs will be created only for these Clusters
	// +optional
	ClusterSelector *metav1.LabelSelector `json:"clusterSelector,omitempty"`
	// Template describes the ServiceInterfaceSet that will be created for each selected Cluster.
	Template ServiceInterfaceSetSpecTemplate `json:"template"`
}

// GetTemplateSpec returns the spec of the template
func (s *DPUServiceInterfaceSpec) GetTemplateSpec() *ServiceInterfaceSetSpec {
	return &s.Template.Spec
}

// ServiceInterfaceSetSpecTemplate describes the data a ServiceInterfaceSet should have when created from a template.
type ServiceInterfaceSetSpecTemplate struct {
	Spec       ServiceInterfaceSetSpec `json:"spec"`
	ObjectMeta `json:"metadata,omitempty"`
}

// DPUServiceInterfaceStatus defines the observed state of DPUServiceInterface
type DPUServiceInterfaceStatus struct {
	// Conditions defines current service state.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedGeneration records the Generation observed on the object the last time it was patched.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=`.status.conditions[?(@.type=='Ready')].status`
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=`.status.conditions[?(@.type=='Ready')].reason`
// +kubebuilder:printcolumn:name="IfType",type=string,JSONPath=`.spec.template.spec.template.spec.interfaceType`
// +kubebuilder:printcolumn:name="IfName",type=string,JSONPath=`.spec.template.spec.template.spec.interfaceName`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// DPUServiceInterface is the Schema for the DPUServiceInterface API
type DPUServiceInterface struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DPUServiceInterfaceSpec   `json:"spec,omitempty"`
	Status DPUServiceInterfaceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DPUServiceInterfaceList contains a list of DPUServiceInterface
type DPUServiceInterfaceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DPUServiceInterface `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DPUServiceInterface{}, &DPUServiceInterfaceList{})
}
