/*
Copyright 2025 NVIDIA

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
	"github.com/nvidia/doca-platform/pkg/conditions"

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

// Status related constants
const (
	// ConditionPreReqsReady is a condition that is set when the prerequisites for the DPUVirtualNetwork are ready.
	ConditionPreReqsReady conditions.ConditionType = "PrerequisitesReady"
	// ConditionDPUVirtualNetworkReconciled is a condition that is set when the DPUVirtualNetwork is reconciled.
	ConditionDPUVirtualNetworkReconciled conditions.ConditionType = "DPUVirtualNetworkReconciled"
)

var (
	// DPUVirtualNetworkConditions are conditions that can be set on a DPUVirtualNetwork object.
	DPUVirtualNetworkConditions = []conditions.ConditionType{
		ConditionPreReqsReady,
		ConditionDPUVirtualNetworkReconciled,
		conditions.TypeReady,
	}
)

var _ conditions.GetSet = &DPUVirtualNetwork{}

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
	IPAM *BridgedNetworkIPAMSpec `json:"ipam,omitempty"`
}

// BridgedNetworkIPAMSpec contains IPAM configuration for bridged network
type BridgedNetworkIPAMSpec struct {
	// IPv4 contains the IPv4 IPAM configuration
	// +optional
	IPv4 *BridgedNetworkIPAMIPv4Spec `json:"ipv4,omitempty"`
}

// BridgedNetworkIPAMIPv4Spec contains IPv4 IPAM configuration for bridged network
type BridgedNetworkIPAMIPv4Spec struct {
	// DHCP if set, enables DHCP for the network
	// +required
	DHCP bool `json:"dhcp"`
	// Subnet is the network subnet in CIDR format to use for DHCP. the first IP in the subnet is the gateway.
	// +required
	Subnet string `json:"subnet"`
	// ExcludeIPs are the IPs to exclude from DHCP allocation.
	// +optional
	ExcludeIPs []ExcludeIPsEntry `json:"excludeIPs,omitempty"`
}

type ExcludeIPsEntry struct {
	// IP is the IP address to exclude from DHCP allocation. must be part for the virtual network subnet.
	// +optional
	IP *string `json:"ip,omitempty"`
	// Range is the range of IP addresses to exclude from DHCP allocation. must be part for the virtual network subnet.
	// +optional
	Range *RangeEntry `json:"range,omitempty"`
}

// RangeEntry contains a range of IP addresses
type RangeEntry struct {
	// Start is the start of the IP address range
	// +required
	Start string `json:"start"`
	// End is the end of the IP address range
	// +required
	End string `json:"end"`
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
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep
// +kubebuilder:selectablefield:JSONPath=`.spec.vpcName`
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

// GetConditions returns the conditions of the DPUVirtualNetwork
func (vn *DPUVirtualNetwork) GetConditions() []metav1.Condition {
	return vn.Status.Conditions
}

// SetConditions sets the conditions of the DPUVirtualNetwork
func (vn *DPUVirtualNetwork) SetConditions(conditions []metav1.Condition) {
	vn.Status.Conditions = conditions
}

// GetIPv4Subnet returns the IPv4 subnet of the virtual network if present, otherwise an empty string is returned
func (vn *DPUVirtualNetwork) GetIPv4Subnet() string {
	if vn.Spec.Type == BridgedVirtualNetworkType && vn.Spec.BridgedNetwork != nil && vn.Spec.BridgedNetwork.IPAM != nil && vn.Spec.BridgedNetwork.IPAM.IPv4 != nil {
		return vn.Spec.BridgedNetwork.IPAM.IPv4.Subnet
	}
	return ""
}
