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
	// DPUVPCKind is the kind of the DPUVPC object.
	DPUVPCKind = "DPUVPC"
	// DPUVPCListKind is the kind of the DPUVPC list object.
	DPUVPCListKind = "DPUVPCList"
)

// DPUVPCGroupVersionKind is the GroupVersionKind of the DPUVPC object.
var DPUVPCGroupVersionKind = GroupVersion.WithKind(DPUVPCKind)

var (
	// DPUVPCConditions are conditions that can be set on a DPUVPC object.
	DPUVPCConditions = []conditions.ConditionType{
		conditions.TypeReady,
	}
)

var _ conditions.GetSet = &DPUVPC{}

func (c *DPUVPC) GetConditions() []metav1.Condition {
	return c.Status.Conditions
}

func (c *DPUVPC) SetConditions(conditions []metav1.Condition) {
	c.Status.Conditions = conditions
}

// DPUVPCSpec defines the desired state of DPUVPCSpec
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="DPUVPC spec is immutable"
type DPUVPCSpec struct {
	// Tenant which owns the VPC.
	// +required
	Tenant string `json:"tenant"`
	// NodeSelector Selects the DPU Nodes with specific labels which belong to this VPC.
	// +optional
	NodeSelector *metav1.LabelSelector `json:"nodeSelector,omitempty"`
	// IsolationClassName is the name of the isolation class to use for the VPC
	// +required
	IsolationClassName string `json:"isolationClassName"`
	// InterNetworkAccess defines if virtual networks within the VPC are routed or not.
	// if set to false, communication between virtual networks is not allowed.
	// +required
	InterNetworkAccess bool `json:"interNetworkAccess"`
}

// DPUVPCStatus defines the observed state of DPUVPC
type DPUVPCStatus struct {
	// VirtualNetworks contains the virtual networks that belong to this VPC
	// +optional
	VirtualNetworks []VirtualNetworkStatus `json:"virtualNetworks,omitempty"`
	// Conditions reflect the status of the object
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedGeneration records the Generation observed on the object the last time it was patched.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// VirtualNetworkStatus is the status of a virtual network
type VirtualNetworkStatus struct {
	// the name of the virtual network
	// +required
	Name string `json:"name"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=`.status.conditions[?(@.type=='Ready')].status`
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=`.status.conditions[?(@.type=='Ready')].reason`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// DPUVPC is the Schema for the dpuvpc API
type DPUVPC struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DPUVPCSpec   `json:"spec,omitempty"`
	Status DPUVPCStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DPUVPCList contains a list of DPUVPC
type DPUVPCList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DPUVPC `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DPUVPC{}, &DPUVPCList{})
}
