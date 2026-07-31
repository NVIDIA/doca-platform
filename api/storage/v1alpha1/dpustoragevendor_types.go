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

package v1alpha1

import (
	"github.com/nvidia/doca-platform/pkg/conditions"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// DPUStorageVendorKind is the kind of the DPUStorageVendor object
	DPUStorageVendorKind = "DPUStorageVendor"
	// DPUStorageVendorFinalizer is the finalizer for the DPUStorageVendor object
	DPUStorageVendorFinalizer = "storage.dpu.nvidia.com/dpustoragevendor"
)

// DPUStorageVendorGroupVersionKind is the GroupVersionKind of the DPUStorageVendor object
var DPUStorageVendorGroupVersionKind = GroupVersion.WithKind(DPUStorageVendorKind)

// Condition types
const (
	// ConditionDPUStorageVendorReconciled is the condition type that indicates that the
	// DPUStorageVendor is reconciled.
	ConditionDPUStorageVendorReconciled conditions.ConditionType = "DPUStorageVendorReconciled"
)

var (
	// DPUStorageVendorConditions is the list of conditions that the DPUStorageVendor can have.
	DPUStorageVendorConditions = []conditions.ConditionType{
		conditions.TypeReady,
		ConditionDPUStorageVendorReconciled,
	}
)

var _ conditions.GetSet = &DPUStorageVendor{}

// GetConditions returns the conditions of the DPUStorageVendor.
func (v *DPUStorageVendor) GetConditions() []metav1.Condition {
	return v.Status.Conditions
}

// SetConditions sets the conditions of the DPUStorageVendor.
func (v *DPUStorageVendor) SetConditions(conditions []metav1.Condition) {
	v.Status.Conditions = conditions
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep
// +kubebuilder:printcolumn:name="StorageClassName",type=string,JSONPath=`.spec.storageClassName`
// +kubebuilder:printcolumn:name="PluginName",type=string,JSONPath=`.spec.pluginName`
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=`.status.conditions[?(@.type=='Ready')].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// DPUStorageVendor represents a StorageVendor CR on the DPU cluster.
type DPUStorageVendor struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              DPUStorageVendorSpec   `json:"spec"`
	Status            DPUStorageVendorStatus `json:"status,omitempty"`
}

// DPUStorageVendorSpec defines the desired state of DPUStorageVendor
type DPUStorageVendorSpec struct {
	// Storage vendor class name, deployed on the DPU K8S cluster.
	// +kubebuilder:validation:MinLength=1
	// +required
	StorageClassName string `json:"storageClassName"`
	// Storage vendor DPU plugin name
	// +kubebuilder:validation:MinLength=1
	// +required
	PluginName string `json:"pluginName"`
}

// DPUStorageVendorStatus defines the observed state of DPUStorageVendor
type DPUStorageVendorStatus struct {
	// Conditions defines current service state.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedGeneration records the Generation observed on the object the last time it was patched.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true

// DPUStorageVendorList contains a list of DPUStorageVendor
type DPUStorageVendorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DPUStorageVendor `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DPUStorageVendor{}, &DPUStorageVendorList{})
}
