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
	"github.com/nvidia/doca-platform/internal/conditions"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// DPUVirtualNetworkKind is the kind of the DPUVirtualNetwork object.
	DPUVirtualNetworkKind = "DPUVirtualNetwork"
	// DPUVirtualNetworkListKind is the kind of the DPUVirtualNetwork list object.
	DPUVirtualNetworkListKind = "DPUVirtualNetworkList"
)

// DPUVirtualNetworkGroupVersionKind is the GroupVersionKind of the DPUVirtualNetwork object.
var DPUVirtualNetworkGroupVersionKind = GroupVersion.WithKind(DPUVirtualNetworkKind)

var (
	// DPUVirtualNetworkConditions are conditions that can be set on a DPUVirtualNetwork object.
	DPUVirtualNetworkConditions = []conditions.ConditionType{
		conditions.TypeReady,
	}
)

var _ conditions.GetSet = &DPUVirtualNetwork{}

func (c *DPUVirtualNetwork) GetConditions() []metav1.Condition {
	return c.Status.Conditions
}

func (c *DPUVirtualNetwork) SetConditions(conditions []metav1.Condition) {
	c.Status.Conditions = conditions
}

// NetworkType represents the type of the virtual network
// +kubebuilder:validation:Enum=Bridged
type NetworkType string

const (
	// BridgedVirtualNetworkType represents a bridged virtual network
	BridgedVirtualNetworkType NetworkType = "Bridged"
)

// DPUVirtualNetworkSpec defines the desired state of DPUVirtualNetworkSpec
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="DPUVirtualNetwork spec is immutable"
// +kubebuilder:validation:XValidation:rule="(self.type == 'Bridged' && has(self.bridgedNetwork))", message="for type=Bridged, bridgedNetwork must be set"
type DPUVirtualNetworkSpec struct {
	// NodeSelector Selects the DPU Nodes with specific labels which can belong to the virtual network.
	// +optional
	NodeSelector *metav1.LabelSelector `json:"nodeSelector,omitempty"`
	// vpcName is the name of the DPUVPC the virtual network belongs within the same namespace.
	// +required
	VPCName string `json:"vpcName"`
	// Type of the virtual network
	// +required
	Type NetworkType `json:"type"`
	// ExternallyRouted defines if the virtual network can be routed externally
	// +required
	ExternallyRouted bool `json:"externallyRouted"`
	// Masquerade defines if the virtual network should masquerade the traffic before egressing to external networks.
	// valid only if ExternallyRouted is true
	// +optional
	// +kubebuilder:default=true
	Masquerade *bool `json:"masquerade,omitempty"`
	// BridgedNetwork contains the bridged network configuration
	// +optional
	BridgedNetwork *BridgedNetworkSpec `json:"bridgedNetwork,omitempty"`
}

// BridgedNetworkSpec contains configuration for bridged network
type BridgedNetworkSpec struct {
	// IPAM contains the IPAM configuration for the bridged network
	// +optional
	IPAM *BridgedNetworkIPAMSpec `json:"ipam"`
}

// BridgedNetworkIPAMSpec contains IPAM configuration for bridged network
type BridgedNetworkIPAMSpec struct {
	// IPv4 contains the IPv4 IPAM configuration
	// +optional
	IPv4 *BridgedNetworkIPAMIPv4Spec `json:"ipv4"`
}

// BridgedNetworkIPAMIPv4Spec contains IPv4 IPAM configuration for bridged network
type BridgedNetworkIPAMIPv4Spec struct {
	// DHCP if set, enables DHCP for the network
	// +required
	DHCP bool `json:"dhcp"`
	// Subnet is the network subnet in CIDR format to use for DHCP. the first IP in the subnet is the gateway.
	// +required
	Subnet string `json:"subnet"`
}

// DPUVirtualNetworkStatus defines the observed state of DPUVirtualNetwork
type DPUVirtualNetworkStatus struct {
	// Conditions reflect the status of the object
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedGeneration records the Generation observed on the object the last time it was patched.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=`.status.conditions[?(@.type=='Ready')].status`
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=`.status.conditions[?(@.type=='Ready')].reason`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// DPUVirtualNetwork is the Schema for the dpuvirtualnetwork API
type DPUVirtualNetwork struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DPUVirtualNetworkSpec   `json:"spec,omitempty"`
	Status DPUVirtualNetworkStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DPUVirtualNetworkList contains a list of DPUVirtualNetwork
type DPUVirtualNetworkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DPUVirtualNetwork `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DPUVirtualNetwork{}, &DPUVirtualNetworkList{})
}
