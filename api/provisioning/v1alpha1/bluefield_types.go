/*
Copyright 2026 NVIDIA

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
	// BlueFieldKind is the kind of the BlueField object
	BlueFieldKind = "BlueFieldSoftware"
)

// BlueFieldGroupVersionKind is the GroupVersionKind of the BlueField object
var BlueFieldGroupVersionKind = GroupVersion.WithKind(BlueFieldKind)

// BlueFieldSoftwarePhase describes current state of BlueFieldSoftware CR.
// Only one of the following state may be specified.
// Default is Initializing.
// +kubebuilder:validation:Enum=Initializing;Downloading;Extracting;Ready;Deleting;Error
type BlueFieldSoftwarePhase string

// These are the valid statuses of BlueFieldSoftware.
const (
	// BlueFieldSoftwareFinalizerPrefix is the prefix for per-DPUSet protection finalizers
	// placed on a BlueFieldSoftware while that DPUSet references it.
	// Full format: provisioning.dpu.nvidia.com/bfs-dpuset-<dpuSetName>
	BlueFieldSoftwareFinalizerPrefix = DPUProvisioningPrefix + "bfs-dpuset-"

	// BlueFieldSoftwareFinalizer is retained for compatibility with older objects that still
	// carry the shared protection finalizer.
	BlueFieldSoftwareFinalizer = "provisioning.dpu.nvidia.com/bluefieldsoftware-protection"

	// BlueFieldSoftware CR is created
	BlueFieldSoftwareInitializing BlueFieldSoftwarePhase = "Initializing"
	// Downloading BlueFieldSoftware components
	BlueFieldSoftwareDownloading BlueFieldSoftwarePhase = "Downloading"
	// Extracting BlueFieldSoftware components from downloaded bundle
	BlueFieldSoftwareExtracting BlueFieldSoftwarePhase = "Extracting"
	// Finished downloading BlueFieldSoftware components, ready for DPU to use
	BlueFieldSoftwareReady BlueFieldSoftwarePhase = "Ready"
	// Delete BlueFieldSoftware
	BlueFieldSoftwareDeleting BlueFieldSoftwarePhase = "Deleting"
	// Error happens during BlueFieldSoftware downloading
	BlueFieldSoftwareError BlueFieldSoftwarePhase = "Error"
)

const (
	// BlueFieldSoftwareCondInitialized indicates the BlueFieldSoftware has been initialized
	BlueFieldSoftwareCondInitialized conditions.ConditionType = "Initialized"
	// BlueFieldSoftwareCondDownloaded indicates the BlueFieldSoftware components have been downloaded
	BlueFieldSoftwareCondDownloaded conditions.ConditionType = "Downloaded"
	// BlueFieldSoftwareCondReady indicates the BlueFieldSoftware is ready for use
	BlueFieldSoftwareCondReady conditions.ConditionType = conditions.TypeReady
	// BlueFieldSoftwareCondError indicates the BlueFieldSoftware is in error state
	BlueFieldSoftwareCondError conditions.ConditionType = "Error"
	// BlueFieldSoftwareCondDeleted indicates the BlueFieldSoftware has been deleted
	BlueFieldSoftwareCondDeleted conditions.ConditionType = "Deleted"
)

var (
	// BlueFieldSoftwareConditions are conditions that can be set on a BlueFieldSoftware object.
	BlueFieldSoftwareConditions = []conditions.ConditionType{
		BlueFieldSoftwareCondInitialized,
		BlueFieldSoftwareCondDownloaded,
		BlueFieldSoftwareCondReady,
		BlueFieldSoftwareCondError,
		BlueFieldSoftwareCondDeleted,
	}
)

// BlueFieldSpec defines the desired state of BlueFieldSoftware.
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="BlueFieldSpec is immutable"
// +kubebuilder:validation:XValidation:rule="!(has(self.nicFw) && has(self.platformPldmFwBundle))",message="nicFw and platformPldmFwBundle are mutually exclusive; set only one"
type BlueFieldSpec struct {
	// OS ISO points to the OS ISO used by DPU OS installation flow.
	// +required
	// +kubebuilder:validation:MinLength=1
	OsIso string `json:"osIso"`

	// PldmFwBundle maps each PSID to the PLDM firmware bundle URL used for that DPU model's
	// baseline firmware updates. Each PSID's bundle is a complete PLDM package for that device.
	// Keys are matched case-insensitively against the BMC-reported DPUDevice status PSID.
	// +optional
	// +kubebuilder:validation:MinProperties=1
	// +kubebuilder:validation:XValidation:rule="self.all(k, size(k) > 0 && size(self[k]) > 0)",message="pldmFwBundle entries must have non-empty PSID keys and non-empty bundle URLs"
	PldmFwBundle map[string]string `json:"pldmFwBundle,omitempty"`

	// PlatformPldmFwBundle points to the platform PLDM firmware bundle used for E/W NIC
	// firmware updates.
	// +optional
	// +kubebuilder:validation:MinLength=1
	PlatformPldmFwBundle *string `json:"platformPldmFwBundle,omitempty"`

	// NicFw points to the NIC firmware binary used for E/W NIC firmware updates.
	// Use this when a specific NIC firmware binary is required and is not included in the
	// platform PLDM firmware bundle.
	// In production, prefer using PlatformPldmFwBundle.
	// +optional
	// +kubebuilder:validation:MinLength=1
	NicFw *string `json:"nicFw,omitempty"`
}

// BlueFieldSoftwareStatus defines the observed state of BlueFieldSoftware
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.versions) || has(self.versions)",message="versions cannot be removed once set"
type BlueFieldSoftwareStatus struct {
	// The current state of BlueFieldSoftware.
	// +kubebuilder:default=Initializing
	// +optional
	Phase BlueFieldSoftwarePhase `json:"phase,omitempty"`
	// Versions tracks the versions of the components
	// +optional
	Versions *BluefieldSoftwareVersions `json:"versions,omitempty"`
	// DownloadedComponents tracks which components have been successfully downloaded
	// +optional
	DownloadedComponents DownloadedComponents `json:"downloadedComponents,omitempty"`
	// ObservedGeneration records the Generation observed on the object the last time it was patched.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions represent the latest available observations of BlueFieldSoftware state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// BluefieldSoftwareVersions defines the versions of various software components for a Bluefield device.
type BluefieldSoftwareVersions struct {
	// DOCA is the formatted, user-facing DOCA version derived from the OS ISO.
	// +optional
	DOCA string `json:"doca,omitempty"`

	// OSISOVersion is the raw DOCA version for the OS ISO, taken from the ISO filename
	// +optional
	OSISOVersion string `json:"osISOVersion,omitempty"`

	// EWNicFwVersion is the E/W NIC firmware version taken from the platform PLDM bundle.
	// It is not keyed by PSID because a BlueFieldSoftware carries a single platform bundle.
	EWNicFwVersion string `json:"ewNicFwVersion,omitempty"`

	// BluefieldSoftwareVersions maps each PSID to the firmware versions from that device's
	// PLDM bundle. Keys are matched case-insensitively against the BMC-reported DPUDevice PSID.
	// +optional
	BluefieldSoftwareVersions map[string]BluefieldDeviceVersions `json:"bluefieldSoftwareVersions,omitempty"`
}

// BluefieldDeviceVersions holds firmware versions extracted from a single PSID's PLDM bundle.
type BluefieldDeviceVersions struct {
	// BMCVersion is the DPU BMC firmware version shipped in the bundle.
	// +optional
	// +kubebuilder:validation:MinLength=1
	BMCVersion string `json:"bmcVersion,omitempty"`

	// BMCErotVersion is the BMC ERoT (External Root of Trust) firmware version shipped in the bundle.
	// +optional
	// +kubebuilder:validation:MinLength=1
	BMCErotVersion string `json:"bmcErotVersion,omitempty"`

	// SBIOSVersion is the DPU SBIOS firmware version shipped in the bundle.
	// +optional
	// +kubebuilder:validation:MinLength=1
	SBIOSVersion string `json:"sbiosVersion,omitempty"`

	// BFNicFwVersion is the BlueField NIC firmware version shipped in the bundle.
	// +optional
	// +kubebuilder:validation:MinLength=1
	BFNicFwVersion string `json:"bfNicFwVersion,omitempty"`
}

// DownloadedComponents tracks which components have been downloaded
type DownloadedComponents struct {
	// PldmFwBundle maps each PSID to the local path of its downloaded PLDM firmware bundle.
	// Keys are matched case-insensitively against the BMC-reported DPUDevice PSID.
	PldmFwBundle map[string]string `json:"pldmFwBundle,omitempty"`

	// PlatformPldmFwBundle is the local path of the downloaded platform PLDM firmware bundle.
	PlatformPldmFwBundle string `json:"platformPldmFwBundle,omitempty"`

	// OsIso is the local path of the downloaded OS ISO.
	OsIso string `json:"osIso,omitempty"`

	// NicFw is the local path of the E/W NIC firmware binary, either downloaded from
	// spec.nicFw or unpacked from the platform PLDM firmware bundle.
	NicFw string `json:"nicFw,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep
// +kubebuilder:validation:XValidation:rule="self.metadata.name.size() <= 187", message="name length can't be bigger than 187 chars"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="phase of the bluefieldsoftware"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// BlueFieldSoftware is the Schema for the bluefieldsoftware API
type BlueFieldSoftware struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec BlueFieldSpec `json:"spec,omitempty"`

	// +kubebuilder:default={phase: Initializing}
	// +optional
	Status BlueFieldSoftwareStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// BlueFieldSoftwareList contains a list of BlueFieldSoftware
type BlueFieldSoftwareList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BlueFieldSoftware `json:"items"`
}

// Implement conditions.GetSet interface
var _ conditions.GetSet = &BlueFieldSoftware{}

// GetConditions returns the conditions of the BlueFieldSoftware
func (b *BlueFieldSoftware) GetConditions() []metav1.Condition {
	return b.Status.Conditions
}

// SetConditions sets the conditions of the BlueFieldSoftware
func (b *BlueFieldSoftware) SetConditions(conditions []metav1.Condition) {
	b.Status.Conditions = conditions
}

// BlueFieldSoftwareFinalizerForDPUSet returns the protection finalizer a DPUSet
// places on a BlueFieldSoftware it references.
func BlueFieldSoftwareFinalizerForDPUSet(dpuSetName string) string {
	return BlueFieldSoftwareFinalizerPrefix + dpuSetName
}

func init() {
	SchemeBuilder.Register(&BlueFieldSoftware{}, &BlueFieldSoftwareList{})
}
