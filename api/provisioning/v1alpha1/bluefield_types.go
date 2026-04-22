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

type BlueFieldSpec struct {
	// +optional
	PldmFwBundle string `json:"pldmFwBundle,omitempty"`

	// +optional
	OsIso string `json:"osIso,omitempty"`

	// +optional
	TmpFwComponents *TmpFwComponents `json:"tmpFwComponents,omitempty"`
}

type TmpFwComponents struct {
	BmcErot    string `json:"bmcErot,omitempty"`
	BmcFw      string `json:"bmcFw,omitempty"`
	AstraNicFw string `json:"astraNicFw,omitempty"`
	GraceErot  string `json:"graceErot,omitempty"`
	GraceFw    string `json:"graceFw,omitempty"`
}

type TmpFwComponentsVersions struct {
	BmcErotVersion    string `json:"bmcErotVersion,omitempty"`
	BmcFwVersion      string `json:"bmcFwVersion,omitempty"`
	AstraNicFwVersion string `json:"astraNicFwVersion,omitempty"`
	GraceErotVersion  string `json:"graceErotVersion,omitempty"`
	GraceFwVersion    string `json:"graceFwVersion,omitempty"`
}

// BlueFieldSoftwareStatus defines the observed state of BlueFieldSoftware
type BlueFieldSoftwareStatus struct {
	// The current state of BlueFieldSoftware.
	// +kubebuilder:default=Initializing
	// +required
	Phase BlueFieldSoftwarePhase `json:"phase"`
	// Versions tracks the versions of the components
	// +optional
	Versions BluefieldSoftwareVersions `json:"versions,omitempty"`
	// DownloadedComponents tracks which components have been successfully downloaded
	// +optional
	DownloadedComponents DownloadedComponents `json:"downloadedComponents,omitempty"`
	// ObservedGeneration records the Generation observed on the object the last time it was patched.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions represent the latest available observations of BlueFieldSoftware state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// BluefieldSoftwareVersions defines the versions of various software components for a Bluefield device.
// +kubebuilder:validation:XValidation:rule="self.fwBundleVersion == oldSelf.fwBundleVersion",message="fwBundleVersion is immutable"
// +kubebuilder:validation:XValidation:rule="self.osISOVersion == oldSelf.osISOVersion",message="osISOVersion is immutable"
// +kubebuilder:validation:XValidation:rule="self.tmpFwComponentsVersions == oldSelf.tmpFwComponentsVersions",message="tmpFwComponentsVersions is immutable"
type BluefieldSoftwareVersions struct {
	FwBundleVersion string `json:"fwBundleVersion,omitempty"`

	OSISOVersion string `json:"osISOVersion,omitempty"`

	TmpFwComponentsVersions TmpFwComponentsVersions `json:"tmpFwComponentsVersions"`
}

// DownloadedComponents tracks which components have been downloaded
type DownloadedComponents struct {
	PldmFwBundle string `json:"pldmFwBundle,omitempty"`
	OsIso        string `json:"osIso,omitempty"`
	BmcErot      string `json:"bmcErot,omitempty"`
	BmcFw        string `json:"bmcFW,omitempty"`
	AstraNicFw   string `json:"astraNicFw,omitempty"`
	GraceErot    string `json:"graceErot,omitempty"`
	GraceFw      string `json:"graceFw,omitempty"`
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
	Status BlueFieldSoftwareStatus `json:"status,omitempty"`
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

func init() {
	SchemeBuilder.Register(&BlueFieldSoftware{}, &BlueFieldSoftwareList{})
}
