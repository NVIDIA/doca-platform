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

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="CIDR",type="string",JSONPath=`.spec.cidr`
// +kubebuilder:printcolumn:name="Gateway index",type="string",JSONPath=`.spec.gatewayIndex`
// +kubebuilder:printcolumn:name="Per Node Network Prefix",type="integer",JSONPath=`.spec.perNodeNetworkPrefix`

// CIDRPool contains configuration for CIDR pool
type CIDRPool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              CIDRPoolSpec   `json:"spec"`
	Status            CIDRPoolStatus `json:"status,omitempty"`
}

// CIDRPoolSpec contains configuration for CIDR pool
type CIDRPoolSpec struct {
	// pool CIDR block which will be split to smaller prefixes(size is define in perNodeNetworkPrefix)
	// and distributed between matching nodes
	CIDR string `json:"cidr"`
	// use IP with this index from the host prefix as a gateway, skip gateway configuration if the value not set
	GatewayIndex *int32 `json:"gatewayIndex,omitempty"`
	// size of the network prefix for each host, the network defined in "cidr" field will be split to multiple networks
	// with this size.
	PerNodeNetworkPrefix int32 `json:"perNodeNetworkPrefix"`
	// contains reserved IP addresses that should not be allocated by nv-ipam
	Exclusions []ExcludeRange `json:"exclusions,omitempty"`
	// static allocations for the pool
	StaticAllocations []CIDRPoolStaticAllocation `json:"staticAllocations,omitempty"`
	// selector for nodes, if empty match all nodes
	NodeSelector *corev1.NodeSelector `json:"nodeSelector,omitempty"`
	// if true, add gateway as default gateway in the routes list
	DefaultGateway bool `json:"defaultGateway,omitempty"`
	// static routes list. The gateway used will according to the node allocation.
	Routes []Route `json:"routes,omitempty"`
}

// CIDRPoolStatus contains the IP prefixes allocated to nodes
type CIDRPoolStatus struct {
	// prefixes allocations for Nodes
	Allocations []CIDRPoolAllocation `json:"allocations"`
}

// CIDRPoolStaticAllocation contains static allocation for a CIDR pool
type CIDRPoolStaticAllocation struct {
	// name of the node for static allocation, can be empty in case if the prefix
	// should be preallocated without assigning it for a specific node
	NodeName string `json:"nodeName,omitempty"`
	// gateway for the node
	Gateway string `json:"gateway,omitempty"`
	// statically allocated prefix
	Prefix string `json:"prefix"`
}

// CIDRPoolAllocation contains prefix allocated for a specific Node
type CIDRPoolAllocation struct {
	// name of the node which owns this allocation
	NodeName string `json:"nodeName"`
	// gateway for the node
	Gateway string `json:"gateway,omitempty"`
	// allocated prefix
	Prefix string `json:"prefix"`
}

// +kubebuilder:object:root=true

// CIDRPoolList contains a list of CIDRPool
type CIDRPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CIDRPool `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CIDRPool{}, &CIDRPoolList{})
}
