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
	// VolumeAttachmentKind is the kind of the VolumeAttachment object
	VolumeAttachmentKind = "VolumeAttachment"
)

// VolumeAttachmentGroupVersionKind is the GroupVersionKind of the VolumeAttachment object
var VolumeAttachmentGroupVersionKind = GroupVersion.WithKind(VolumeAttachmentKind)

// VolumeSource references to the NV-Volume object
type VolumeSource struct {
	// Reference to the NV-Volume object
	VolumeRef *ObjectRef `json:"volumeRef,omitempty"`
}

// VolumeAttachmentStatusDPU describe the information of DPU volume
// +kubebuilder:validation:cel:rule="(has(self.bdevAttrs) && !has(self.fsdevAttrs)) || (!has(self.bdevAttrs) && has(self.fsdevAttrs))"
type VolumeAttachmentStatusDPU struct {
	// Indicates the volume is successfully attached to the DPU node
	Attached bool `json:"attached,omitempty"`
	// PCI device address in the following format: (bus:device.function)
	PCIDeviceAddress string `json:"pciDeviceAddress,omitempty"`
	// The name of the device that was created by the storage vendor plugin
	DeviceName string `json:"deviceName,omitempty"`
	// The attributes of the underlying block device
	// +optional
	BdevAttrs BdevAttrs `json:"bdevAttrs,omitempty"`
	// The attributes of the underlying filesystem device
	// +optional
	FSdevAttrs FSdevAttrs `json:"fsdevAttrs,omitempty"`
}

// FSdevAttrs represents the attributes of the underlying filesystem device
type FSdevAttrs struct {
	// Filesystem tag identified by SNAP on the host (used for the mount). Relevant for volume of type filesystem
	FilesystemTag string `json:"filesystemTag,omitempty"`
}

// BdevAttrs represents the attributes of the underlying block device
type BdevAttrs struct {
	// The namespace ID within the NVME controller
	NVMeNsID int64 `json:"nvmeNsID,omitempty"`
	// The nvme namespace UUID
	NVMeUUID string `json:"nvmeUUID,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep

// VolumeAttachment captures the intent to attach/detach the specified NV-Volume to/from the specified node.
type VolumeAttachment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              VolumeAttachmentSpec   `json:"spec"`
	Status            VolumeAttachmentStatus `json:"status,omitempty"`
}

// VolumeAttachmentSpec defines the desired state of VolumeAttachment
type VolumeAttachmentSpec struct {
	// The name of the node that the volume should be attached to
	NodeName string `json:"nodeName,omitempty"`
	// Reference to the NV-Volume object
	Source VolumeSource `json:"source,omitempty"`
	// Reference to the SV-VolumeAttachment object
	VolumeAttachmentRef *ObjectRef `json:"volumeAttachmentRef,omitempty"`
	// Opaque static publish properties of the volume returned by the plugin
	Parameters map[string]string `json:"parameters,omitempty"`
}

// VolumeAttachmentStatus defines the observed state of VolumeAttachment
type VolumeAttachmentStatus struct {
	// Indicates the volume is successfully attached to the target storage system
	StorageAttached bool `json:"storageAttached,omitempty"`
	// The last error encountered during the attach operation, if any
	Message string `json:"message,omitempty"`
	// Details about the DPU attachment
	DPU VolumeAttachmentStatusDPU `json:"dpu,omitempty"`
}

// +kubebuilder:object:root=true

// VolumeAttachmentList contains a list of VolumeAttachment
type VolumeAttachmentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VolumeAttachment `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VolumeAttachment{}, &VolumeAttachmentList{})
}
