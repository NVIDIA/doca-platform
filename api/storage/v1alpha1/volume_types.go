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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VolumeState represents the state of volume
// +kubebuilder:validation:Enum=InProgress;Available
type VolumeState string

const (
	// VolumeKind is the kind of the Volume object
	VolumeKind = "Volume"
	// InProgress means the some of related resource is still in progress
	VolumeStateInProgress VolumeState = "InProgress"
	// Available means that all related resources are created
	VolumeStateAvailable VolumeState = "Available"
)

// VolumeGroupVersionKind is the GroupVersionKind of the Volume object
var VolumeGroupVersionKind = GroupVersion.WithKind(VolumeKind)

// VolumeRequest represents the volume's requirements
type VolumeRequest struct {
	// The capacity of the required storage space in bytes
	// +optional
	CapacityRange CapacityRange `json:"capacityRange,omitempty"`
	// Contains the types of access modes required
	// +optional
	AccessModes []corev1.PersistentVolumeAccessMode `json:"accessModes,omitempty"`
	// volumeMode defines what type of volume is required by the claim.
	// Value of Filesystem is implied when not included in claim spec.
	// +optional
	VolumeMode *corev1.PersistentVolumeMode `json:"volumeMode,omitempty"`
}

// CapacityRange represents the capacity of the required storage space in bytes
type CapacityRange struct {
	Request resource.Quantity `json:"request,omitempty"`
	Limit   resource.Quantity `json:"limit,omitempty"`
}

// ObjectRef reference to the object
type ObjectRef struct {
	Kind       string `json:"kind,omitempty"`
	APIVersion string `json:"apiVersion,omitempty"`
	Name       string `json:"name,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
}

// CSIReference reference to CSI object
type CSIReference struct {
	CSIDriverName    string     `json:"csiDriverName,omitempty"`
	StorageClassName string     `json:"storageClassName,omitempty"`
	PVCRef           *ObjectRef `json:"pvcRef,omitempty"`
}

// VolumeSpecDPU describe volume information in DPU cluster
type VolumeSpecDPU struct {
	ID          string                              `json:"id,omitempty"`
	Capacity    resource.Quantity                   `json:"capacity,omitempty"`
	AccessModes []corev1.PersistentVolumeAccessMode `json:"accessModes,omitempty"`
	// +kubebuilder:validation:Enum=Delete;Retain
	ReclaimPolicy           corev1.PersistentVolumeReclaimPolicy `json:"reclaimPolicy,omitempty"`
	StorageVendorName       string                               `json:"storageVendorName,omitempty"`
	StorageVendorPluginName string                               `json:"storageVendorPluginName,omitempty"`
	VolumeAttributes        map[string]string                    `json:"volumeAttributes,omitempty"`
	CSIReference            CSIReference                         `json:"csiReference,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep

// Volume represents a persistent volume on the DPU cluster.
// It maps between the tenant K8S persistent volume (PV) object on the tenant cluster into the actual volume on the DPU cluster.
// Volume is an internal API, it is not intended to be used by users.
type Volume struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              VolumeSpec   `json:"spec"`
	Status            VolumeStatus `json:"status,omitempty"`
}

// VolumeSpec defines the desired state of Volume
type VolumeSpec struct {
	// List of storage parameters supported by the policy, values are string only
	// +optional
	StorageParameters map[string]string `json:"storageParameters,omitempty"`
	// The capacity of the required storage space in bytes
	// +required
	Request VolumeRequest `json:"request,omitempty"`
	// Reference to the StoragePolicy object
	// +optional
	StoragePolicyRef *ObjectRef `json:"storagePolicyRef,omitempty"`
	// List of storage parameters supported by the policy, values are string only
	// +optional
	StoragePolicyParameters map[string]string `json:"storagePolicyParameters,omitempty"`
	// Describe volume information in DPU cluster
	// +optional
	VolumeSpecDPU VolumeSpecDPU `json:"volume,omitempty"`
}

// VolumeStatus defines the observed state of Volume
type VolumeStatus struct {
	// The state of a Volume object
	// +optional
	State VolumeState `json:"state,omitempty"`
}

// +kubebuilder:object:root=true

// VolumeList contains a list of Volume
type VolumeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Volume `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Volume{}, &VolumeList{})
}
