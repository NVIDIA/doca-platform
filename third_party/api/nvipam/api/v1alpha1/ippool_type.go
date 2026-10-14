/*
 Copyright 2023, NVIDIA CORPORATION & AFFILIATES
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
// +kubebuilder:printcolumn:name="Subnet",type="string",JSONPath=`.spec.subnet`
// +kubebuilder:printcolumn:name="Gateway",type="string",JSONPath=`.spec.gateway`
// +kubebuilder:printcolumn:name="Block Size",type="integer",JSONPath=`.spec.perNodeBlockSize`

// IPPool contains configuration for IPAM controller
type IPPool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              IPPoolSpec   `json:"spec"`
	Status            IPPoolStatus `json:"status,omitempty"`
}

// IPPoolSpec contains configuration for IP pool
type IPPoolSpec struct {
	// subnet of the pool
	Subnet string `json:"subnet"`
	// amount of IPs to allocate for each node,
	// must be less than amount of available IPs in the subnet
	PerNodeBlockSize int `json:"perNodeBlockSize"`
	// contains reserved IP addresses that should not be allocated by nv-ipam
	Exclusions []ExcludeRange `json:"exclusions,omitempty"`
	// gateway for the pool
	Gateway string `json:"gateway,omitempty"`
	// selector for nodes, if empty match all nodes
	NodeSelector *corev1.NodeSelector `json:"nodeSelector,omitempty"`
	// if true, add gateway as default gateway in the routes list
	DefaultGateway bool `json:"defaultGateway,omitempty"`
	// static routes list using the gateway specified in the spec.
	Routes []Route `json:"routes,omitempty"`
}

// IPPoolStatus contains the IP ranges allocated to nodes
type IPPoolStatus struct {
	// IP allocations for Nodes
	Allocations []Allocation `json:"allocations"`
}

// Allocation contains IP Allocation for a specific Node
type Allocation struct {
	NodeName string `json:"nodeName"`
	StartIP  string `json:"startIP"`
	EndIP    string `json:"endIP"`
}

// +kubebuilder:object:root=true

// IPPoolList contains a list of IPPool
type IPPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IPPool `json:"items"`
}

func init() {
	SchemeBuilder.Register(&IPPool{}, &IPPoolList{})
}
