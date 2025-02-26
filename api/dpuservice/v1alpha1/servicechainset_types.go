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

package v1alpha1

import (
	"github.com/nvidia/doca-platform/internal/conditions"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// ServiceChainSetKind is the kind of the ServiceChainSet in string format.
	ServiceChainSetKind = "ServiceChainSet"
	// ServiceChainSetFinalizer is set on a ServiceChainSet when it is first handled by
	// the controller, and removed when this object is deleted.
	ServiceChainSetFinalizer = "dpu.nvidia.com/servicechainset"
)

var (
	// ServiceChainSetGroupVersionKind is the GroupVersionKind of the ServiceChainSet
	ServiceChainSetGroupVersionKind = GroupVersion.WithKind(ServiceChainSetKind)
)

// Status related variables
const (
	ConditionServiceChainsReconciled conditions.ConditionType = "ServiceChainsReconciled"
	ConditionServiceChainsReady      conditions.ConditionType = "ServiceChainsReady"
)

var (
	ServiceChainSetConditions = []conditions.ConditionType{
		conditions.TypeReady,
		ConditionServiceChainsReconciled,
		ConditionServiceChainsReady,
	}
)

var _ conditions.GetSet = &ServiceChainSet{}

func (c *ServiceChainSet) GetConditions() []metav1.Condition {
	return c.Status.Conditions
}

func (c *ServiceChainSet) SetConditions(conditions []metav1.Condition) {
	c.Status.Conditions = conditions
}

// ServiceChainSetSpec defines the desired state of ServiceChainSet
type ServiceChainSetSpec struct {
	// Select the Nodes with specific labels, ServiceChain CRs will be created
	// only for these Nodes
	// +optional
	NodeSelector *metav1.LabelSelector `json:"nodeSelector,omitempty"`
	// ServiceChainSpecTemplate holds the template for the ServiceChainSpec
	// +required
	Template ServiceChainSpecTemplate `json:"template"`
}

// ServiceChainSpecTemplate defines the template from which ServiceChainSpecs
// are created
type ServiceChainSpecTemplate struct {
	// ServiceChainSpec is the spec for the ServiceChainSpec
	// +required
	Spec ServiceChainSpec `json:"spec"`
	// ObjectMeta holds metadata like labels and annotations.
	// +optional
	ObjectMeta `json:"metadata,omitempty"`
}

// ServiceChainSetStatus defines the observed state of ServiceChainSet
type ServiceChainSetStatus struct {
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

// ServiceChainSet is the Schema for the servicechainsets API
type ServiceChainSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ServiceChainSetSpec   `json:"spec,omitempty"`
	Status ServiceChainSetStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ServiceChainSetList contains a list of ServiceChainSet
type ServiceChainSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ServiceChainSet `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ServiceChainSet{}, &ServiceChainSetList{})
}
