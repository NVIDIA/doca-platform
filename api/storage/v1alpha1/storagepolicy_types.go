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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// StorageSelectionAlgType represents the type of storage selection algorithm
// +kubebuilder:validation:Enum=Random;LocalNVolumes
type StorageSelectionAlgType string

// StorageVendorState represents the state of storage vendor
// +kubebuilder:validation:Enum=Valid;Invalid
type StorageVendorState string

const (
	// StoragePolicyKind is the kind of the StoragePolicy object
	StoragePolicyKind = "StoragePolicy"
	// Random selection across the vendors defined in the StoragePolicy list.
	Random StorageSelectionAlgType = "Random"
	// Load-balancing on the number of volumes belonging to the StoragePolicy.
	// The vendor (in the StoragePolicy list) with the minimal number of volumes should be selected.
	LocalNVolumes StorageSelectionAlgType = "LocalNVolumes"
	// Vaild means all provided storage vendors (storageVendors)
	// have a StorageVendor object with a valid storage class object
	StorageVendorStateValid StorageVendorState = "Valid"
	// Invalid means one or more provided storage vendors (storageVendors) do not
	// have a StorageVendor object with a valid storage class object
	StorageVendorStateInvalid StorageVendorState = "Invalid"
)

// StoragePolicyGroupVersionKind is the GroupVersionKind of the StoragePolicy object
var StoragePolicyGroupVersionKind = GroupVersion.WithKind(StoragePolicyKind)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep

// StoragePolicy represents a storage policy which maps between policy into a list of storage vendors.
type StoragePolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              StoragePolicySpec   `json:"spec"`
	Status            StoragePolicyStatus `json:"status,omitempty"`
}

// StoragePolicySpec defines the desired state of StoragePolicy
type StoragePolicySpec struct {
	// List of storage vendors
	StorageVendors []string `json:"storageVendors,omitempty"`
	// List of storage parameters supported by the policy, values are string only
	// +optional
	StorageParameters map[string]string `json:"storageParameters,omitempty"`
	// Algorithm used to select the storage vendor. Default: LocalNVolumes
	// +kubebuilder:default=LocalNVolumes
	// +optional
	StorageSelectionAlg StorageSelectionAlgType `json:"storageSelectionAlg,omitempty"`
}

// StoragePolicyStatus defines the observed state of StoragePolicy
type StoragePolicyStatus struct {
	// A storage policy is valid if all provided storage vendors have a StorageVendor object with a valid storage class object
	// +optional
	State StorageVendorState `json:"state,omitempty"`
	// Informative message when the state is invalid
	// +optional
	Message string `json:"message,omitempty"`
}

// +kubebuilder:object:root=true

// StoragePolicyList contains a list of StoragePolicy
type StoragePolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StoragePolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&StoragePolicy{}, &StoragePolicyList{})
}
