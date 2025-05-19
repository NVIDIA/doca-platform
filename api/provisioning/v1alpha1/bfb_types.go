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
	// BFBKind is the kind of the BFB object
	BFBKind = "BFB"
)

// BFBGroupVersionKind is the GroupVersionKind of the BFB object
var BFBGroupVersionKind = GroupVersion.WithKind(BFBKind)

// BFBPhase describes current state of BFB CR.
// Only one of the following state may be specified.
// Default is Initializing.
// +kubebuilder:validation:Enum=Initializing;Downloading;Ready;Deleting;Error
type BFBPhase string

// These are the valid statuses of BFB.
const (
	BFBFinalizer = "provisioning.dpu.nvidia.com/bfb-protection"

	// BFB CR is created
	BFBInitializing BFBPhase = "Initializing"
	// Downloading BFB file
	BFBDownloading BFBPhase = "Downloading"
	// Finished downloading BFB file, ready for DPU to use
	BFBReady BFBPhase = "Ready"
	// Delete BFB
	BFBDeleting BFBPhase = "Deleting"
	// Error happens during BFB downloading
	BFBError BFBPhase = "Error"
)

// BFBSpec defines the content of the BFB
type BFBSpec struct {
	// Specifies the file name where the BFB is downloaded on the volume.
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9\_\-\.]+\.bfb$`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf", message="Value is immutable"
	// +optional
	FileName *string `json:"fileName,omitempty"`

	// The url of the bfb image to download.
	// +kubebuilder:validation:Pattern=`^(http|https)://.+\.bfb$`
	// +required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf", message="Value is immutable"
	URL string `json:"url"`
}

// BFBVersions represents the version information for BFB components.
type BFBVersions struct {
	// BSP (Board Support Package) version.
	// This field stores the version of the BSP, which provides essential
	// support and drivers for the hardware platform.
	// +optional
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="BSP version is immutable"
	BSP string `json:"bsp,omitempty"`

	// DOCA version
	// Specifies the version of NVIDIA's Data Center-on-a-Chip Architecture (DOCA),
	// a platform for developing applications on DPUs
	// +optional
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="DOCA version is immutable"
	DOCA string `json:"doca,omitempty"`

	// UEFI (Unified Extensible Firmware Interface) version.
	// Indicates the UEFI firmware version, which is responsible for booting
	// the operating system and initializing hardware components
	// +optional
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="UEFI version is immutable"
	UEFI string `json:"uefi,omitempty"`

	// ATF (Arm Trusted Firmware) version.
	// Contains the version of ATF, which provides a secure runtime environment
	// +optional
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="ATF version is immutable"
	ATF string `json:"atf,omitempty"`
}

// BFBStatus defines the observed state of BFB
type BFBStatus struct {
	// Filename is the name of the file where the BFB can be accessed on its volume.
	// This is the same as `.spec.Filename` if set.
	FileName string `json:"fileName,omitempty"`
	// The current state of BFB.
	// +kubebuilder:default=Initializing
	// +required
	Phase BFBPhase `json:"phase"`
	// BFB versions - BSP, DOCA, UEFI and ATF
	// Holds detailed version information for each component within the BFB
	// +optional
	Versions BFBVersions `json:"versions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep

// BFB is the Schema for the bfbs API
type BFB struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec BFBSpec `json:"spec,omitempty"`

	// +kubebuilder:default={phase: Initializing}
	// +optional
	Status BFBStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// BFBList contains a list of BFB
type BFBList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BFB `json:"items"`
}

func init() {
	SchemeBuilder.Register(&BFB{}, &BFBList{})
}
