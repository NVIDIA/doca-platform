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

const (
	// StorageVendorKind is the kind of the StorageVendor object
	StorageVendorKind = "StorageVendor"
)

// StorageVendorGroupVersionKind is the GroupVersionKind of the StorageVendor object
var StorageVendorGroupVersionKind = GroupVersion.WithKind(StorageVendorKind)

// +kubebuilder:object:root=true
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep

// StorageVendor represents a storage vendor.
// Each storage vendor must have exactly one NVIDIA StorageVendor custom resource object.
type StorageVendor struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              StorageVendorSpec `json:"spec"`
}

// StorageVendorSpec defines the desired state of StorageVendor
type StorageVendorSpec struct {
	// Storage vendor class name, deployed on the DPU K8S cluster.
	// +required
	StorageClassName string `json:"storageClassName,omitempty"`
	// Storage vendor DPU plugin name
	// +required
	PluginName string `json:"pluginName,omitempty"`
}

// +kubebuilder:object:root=true

// StorageVendorList contains a list of StorageVendor
type StorageVendorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StorageVendor `json:"items"`
}

func init() {
	SchemeBuilder.Register(&StorageVendor{}, &StorageVendorList{})
}
