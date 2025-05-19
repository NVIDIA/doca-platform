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
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// SVVolumeAttachmentKind is the kind of the SVVolumeAttachment object
	SVVolumeAttachmentKind = "SVVolumeAttachment"
)

// SVVolumeAttachmentGroupVersionKind is the GroupVersionKind of the SVVolumeAttachment object
var SVVolumeAttachmentGroupVersionKind = GroupVersion.WithKind(SVVolumeAttachmentKind)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep

// SVVolumeAttachment captures the intent to attach/detach the specified Volume to/from the specified node.
type SVVolumeAttachment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              storagev1.VolumeAttachmentSpec   `json:"spec"`
	Status            storagev1.VolumeAttachmentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SVVolumeAttachmentList contains a list of SVVolumeAttachment
type SVVolumeAttachmentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SVVolumeAttachment `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SVVolumeAttachment{}, &SVVolumeAttachmentList{})
}
