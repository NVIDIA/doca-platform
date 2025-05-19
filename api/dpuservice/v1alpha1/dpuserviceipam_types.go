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

//nolint:dupl
package v1alpha1

import (
	"github.com/nvidia/doca-platform/internal/conditions"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	DPUServiceIPAMFinalizer = "svc.dpu.nvidia.com/dpuserviceipam"
	DPUServiceIPAMKind      = "DPUServiceIPAM"
)

var DPUServiceIPAMGroupVersionKind = GroupVersion.WithKind(DPUServiceIPAMKind)

// Status related variables
const (
	ConditionDPUIPAMObjectReconciled conditions.ConditionType = "DPUIPAMObjectReconciled"
	ConditionDPUIPAMObjectReady      conditions.ConditionType = "DPUIPAMObjectReady"
)

var (
	DPUServiceIPAMConditions = []conditions.ConditionType{
		conditions.TypeReady,
		ConditionDPUIPAMObjectReconciled,
		ConditionDPUIPAMObjectReady,
	}
)

var _ conditions.GetSet = &DPUServiceIPAM{}

func (c *DPUServiceIPAM) GetConditions() []metav1.Condition {
	return c.Status.Conditions
}

func (c *DPUServiceIPAM) SetConditions(conditions []metav1.Condition) {
	c.Status.Conditions = conditions
}

// DPUServiceIPAMSpec defines the desired state of DPUServiceIPAM
type DPUServiceIPAMSpec struct {
	ObjectMeta `json:"metadata,omitempty"`
	// IPV4Network is the configuration related to splitting a network into subnets per node, each with their own gateway.
	IPV4Network *IPV4Network `json:"ipv4Network,omitempty"`
	// IPV4Subnet is the configuration related to splitting a subnet into blocks per node. In this setup, there is a
	// single gateway.
	IPV4Subnet *IPV4Subnet `json:"ipv4Subnet,omitempty"`

	// ClusterSelector determines in which clusters the DPUServiceIPAM controller should apply the configuration.
	// +optional
	ClusterSelector *metav1.LabelSelector `json:"clusterSelector,omitempty"`
	// NodeSelector determines in which DPU nodes the DPUServiceIPAM controller should apply the configuration.
	NodeSelector *corev1.NodeSelector `json:"nodeSelector,omitempty"`
}

// IPV4Network describes the configuration relevant to splitting a network into subnet per node (i.e. different gateway and
// broadcast IP per node).
type IPV4Network struct {
	// Network is the CIDR from which subnets should be allocated per node.
	Network string `json:"network"`
	// GatewayIndex determines which IP in the subnet extracted from the CIDR should be the gateway IP. For point to
	// point networks (/31), one needs to leave this empty to make use of both the IPs.
	GatewayIndex *int32 `json:"gatewayIndex,omitempty"`
	// PrefixSize is the size of the subnet that should be allocated per node.
	PrefixSize int32 `json:"prefixSize"`
	// Exclusions is a list of IPs that should be excluded when splitting the CIDR into subnets per node.
	Exclusions []string `json:"exclusions,omitempty"`
	// Allocations describes the subnets that should be assigned in each DPU node.
	Allocations map[string]string `json:"allocations,omitempty"`
	// DefaultGateway adds gateway as default gateway in the routes list if true.
	DefaultGateway bool `json:"defaultGateway,omitempty"`
	// Routes is the static routes list using the gateway specified in the spec.
	Routes []Route `json:"routes,omitempty"`
}

// IPV4Subnet describes the configuration relevant to splitting a subnet to a subnet block per node (i.e. same gateway
// and broadcast IP across all nodes).
type IPV4Subnet struct {
	// Subnet is the CIDR from which blocks should be allocated per node
	Subnet string `json:"subnet"`
	// Gateway is the IP in the subnet that should be the gateway of the subnet.
	Gateway string `json:"gateway"`
	// PerNodeIPCount is the number of IPs that should be allocated per node.
	PerNodeIPCount int `json:"perNodeIPCount"`
	// if true, add gateway as default gateway in the routes list
	// DefaultGateway adds gateway as default gateway in the routes list if true.
	DefaultGateway bool `json:"defaultGateway,omitempty"`
	// Routes is the static routes list using the gateway specified in the spec.
	Routes []Route `json:"routes,omitempty"`
}

// Route contains static route parameters
type Route struct {
	// The destination of the route, in CIDR notation
	Dst string `json:"dst"`
}

// DPUServiceIPAMStatus defines the observed state of DPUServiceIPAM
type DPUServiceIPAMStatus struct {
	// Conditions reflect the status of the object
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedGeneration records the Generation observed on the object the last time it was patched.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=`.status.conditions[?(@.type=='Ready')].status`
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=`.status.conditions[?(@.type=='Ready')].reason`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// DPUServiceIPAM is the Schema for the dpuserviceipams API
type DPUServiceIPAM struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DPUServiceIPAMSpec   `json:"spec,omitempty"`
	Status DPUServiceIPAMStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DPUServiceIPAMList contains a list of DPUServiceIPAM
type DPUServiceIPAMList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DPUServiceIPAM `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DPUServiceIPAM{}, &DPUServiceIPAMList{})
}
