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
	// DPUStoragePolicyKind is the kind of the DPUStoragePolicy object
	DPUStoragePolicyKind = "DPUStoragePolicy"
	// DPUStoragePolicyFinalizer is the finalizer for the DPUStoragePolicy object
	DPUStoragePolicyFinalizer = "storage.dpu.nvidia.com/dpustoragepolicy"
)

// SelectionAlgorithm represents the storage selection algorithm type
type SelectionAlgorithm string

const (
	// Random selection across the vendors defined in the StoragePolicy list.
	SelectionAlgorithmRandom SelectionAlgorithm = "Random"
	// Load-balancing on the number of volumes belonging to the StoragePolicy.
	// The vendor (in the DPUStoragePolicy list) with the minimal number of volumes should be selected.
	SelectionAlgorithmNumberVolumes SelectionAlgorithm = "NumberVolumes"
)

// DPUStoragePolicyGroupVersionKind is the GroupVersionKind of the DPUStoragePolicy object
var DPUStoragePolicyGroupVersionKind = GroupVersion.WithKind(DPUStoragePolicyKind)

// Condition types
const (
	// ConditionDPUStoragePolicyReconciled is the condition type that indicates that the
	// DPUStoragePolicy is reconciled.
	ConditionDPUStoragePolicyReconciled conditions.ConditionType = "DPUStoragePolicyReconciled"
	// ConditionDPUStoragePolicyValid is the condition type that indicates that
	// all the provided DPUStorageVendors are valid
	ConditionDPUStoragePolicyValid conditions.ConditionType = "Valid"
)

var (
	// DPUStoragePolicyConditions is the list of conditions that the DPUStoragePolicy can have.
	DPUStoragePolicyConditions = []conditions.ConditionType{
		conditions.TypeReady,
		ConditionDPUStoragePolicyReconciled,
		ConditionDPUStoragePolicyValid,
	}
)

var _ conditions.GetSet = &DPUStoragePolicy{}

// GetConditions returns the conditions of the DPUStoragePolicy.
func (v *DPUStoragePolicy) GetConditions() []metav1.Condition {
	return v.Status.Conditions
}

// SetConditions sets the conditions of the DPUStoragePolicy.
func (v *DPUStoragePolicy) SetConditions(conditions []metav1.Condition) {
	v.Status.Conditions = conditions
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep
// +kubebuilder:printcolumn:name="SelectionAlgorithm",type="string",JSONPath=`.spec.selectionAlgorithm`
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=`.status.conditions[?(@.type=='Ready')].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// DPUStoragePolicy represents a DPUStoragePolicy CR
type DPUStoragePolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              DPUStoragePolicySpec   `json:"spec"`
	Status            DPUStoragePolicyStatus `json:"status,omitempty"`
}

// DPUStoragePolicySpec defines the desired state of DPUStoragePolicy
type DPUStoragePolicySpec struct {
	// List of storage vendors
	// +kubebuilder:validation:MinItems=1
	// +required
	DPUStorageVendors []string `json:"dpuStorageVendors"`
	// Parameters supported by the policy
	// +kubebuilder:default={}
	// +optional
	Parameters map[string]string `json:"parameters,omitempty"`
	// Selection algorithm used to select DPUStorageVendor
	// +kubebuilder:validation:Enum=Random;NumberVolumes
	// +kubebuilder:default=NumberVolumes
	// +optional
	SelectionAlgorithm *SelectionAlgorithm `json:"selectionAlgorithm,omitempty"`
}

// DPUStoragePolicyStatus defines the observed state of DPUStoragePolicy
type DPUStoragePolicyStatus struct {
	// Current service state conditions
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedGeneration records the Generation observed on the object the last time it was patched.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true

// DPUStoragePolicyList contains a list of DPUStoragePolicy objects
type DPUStoragePolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DPUStoragePolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DPUStoragePolicy{}, &DPUStoragePolicyList{})
}
