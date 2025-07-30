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
	"github.com/nvidia/doca-platform/pkg/conditions"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ServiceChainKind = "ServiceChain"
)

var ServiceChainGroupVersionKind = GroupVersion.WithKind(ServiceChainKind)

// ServiceChainSpec defines the desired state of ServiceChain
type ServiceChainSpec struct {
	// Node where this ServiceChain applies to
	// +optional
	Node *string `json:"node,omitempty"`
	// The switches of the ServiceChain, order is significant
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=50
	// +required
	Switches []Switch `json:"switches"`
}

// Switch defines the switch configuration
type Switch struct {
	// Ports of the switch
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=50
	// +required
	Ports []Port `json:"ports"`

	// ServiceMTU of the switch
	// The default is 1500.
	// +kubebuilder:validation:Minimum=1000
	// +kubebuilder:validation:Maximum=9216
	// +kubebuilder:default=1500
	// +optional
	ServiceMTU *int `json:"serviceMTU,omitempty"`
}

// Port defines the port configuration
type Port struct {
	// +required
	ServiceInterface ServiceIfc `json:"serviceInterface"`
}

// ServiceIfc defines the service interface configuration
type ServiceIfc struct {
	// Labels matching service interface
	// +kubebuilder:validation:MinProperties=1
	// +kubebuilder:validation:MaxProperties=50
	// +required
	MatchLabels map[string]string `json:"matchLabels"`
	// IPAM defines the IPAM configuration when referencing a serviceInterface of type 'service'
	// +optional
	IPAM *IPAM `json:"ipam,omitempty"`
}

// IPAM defines the IPAM configuration
type IPAM struct {
	// Labels matching service IPAM
	// +kubebuilder:validation:MinProperties=1
	// +kubebuilder:validation:MaxProperties=50
	// +required
	MatchLabels map[string]string `json:"matchLabels"`
	// DefaultGateway adds gateway as default gateway in the routes list if true.
	// +optional
	DefaultGateway *bool `json:"defaultGateway,omitempty"`
	// SetDefaultRoute adds a default route to the routing table if true.
	// +optional
	SetDefaultRoute *bool `json:"setDefaultRoute,omitempty"`
}

const (
	// ServiceChainReconciled is the condition type that indicates that the
	// service chain is reconciled.
	ServiceChainReconciled conditions.ConditionType = "ServiceChainReconciled"
)

var (
	ServiceChainConditions = []conditions.ConditionType{
		conditions.TypeReady,
		ServiceChainReconciled,
	}
)

var _ conditions.GetSet = &ServiceChain{}

func (c *ServiceChain) GetConditions() []metav1.Condition {
	return c.Status.Conditions
}

func (c *ServiceChain) SetConditions(conditions []metav1.Condition) {
	c.Status.Conditions = conditions
}

// ServiceChainStatus defines the observed state of ServiceChain
type ServiceChainStatus struct {
	// Conditions reflect the status of the object
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedGeneration records the Generation observed on the object the last time it was patched.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep
// +kubebuilder:printcolumn:name="Node",type=string,JSONPath=`.spec.node`
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=`.status.conditions[?(@.type=='Ready')].status`
// +kubebuilder:printcolumn:name="Reason",type="string",JSONPath=`.status.conditions[?(@.type=='Ready')].reason`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ServiceChain is the Schema for the servicechains API
type ServiceChain struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ServiceChainSpec   `json:"spec,omitempty"`
	Status ServiceChainStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ServiceChainList contains a list of ServiceChain
type ServiceChainList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ServiceChain `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ServiceChain{}, &ServiceChainList{})
}
