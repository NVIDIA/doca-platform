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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// DPUVolumeKind is the kind of the DPUVolume object
	DPUVolumeKind = "DPUVolume"
	// DPUVolumeFinalizer is the finalizer for the DPUVolume object
	DPUVolumeFinalizer = "storage.dpu.nvidia.com/dpuvolume"
)

// DPUVolumeGroupVersionKind is the GroupVersionKind of the DPUVolume object
var DPUVolumeGroupVersionKind = GroupVersion.WithKind(DPUVolumeKind)

// Condition types
const (
	// ConditionDPUVolumeReconciled is the condition type that indicates that the
	// DPUVolume is reconciled.
	ConditionDPUVolumeReconciled conditions.ConditionType = "DPUVolumeReconciled"
	// ConditionDPUVolumeScheduled is the condition type that indicates that the
	// DPUVolume is scheduled.
	ConditionDPUVolumeScheduled conditions.ConditionType = "Scheduled"
	// ConditionDPUVolumeBound is the condition type that indicates that the
	// DPUVolume is bound to a volume in the DPU cluster
	ConditionDPUVolumeBound conditions.ConditionType = "Bound"
)

type DPUVolumePhase string

const (
	// used for DPUVolume that are not yet bound to a volume in the DPU cluster
	DPUVolumePhasePending DPUVolumePhase = "Pending"
	// used for DPUVolume that are bound to a volume in the DPU cluster
	DPUVolumePhaseBound DPUVolumePhase = "Bound"
)

var (
	// DPUVolumeConditions is the list of conditions that the DPUVolume can have.
	DPUVolumeConditions = []conditions.ConditionType{
		conditions.TypeReady,
		ConditionDPUVolumeReconciled,
		ConditionDPUVolumeScheduled,
		ConditionDPUVolumeBound,
	}
)

var _ conditions.GetSet = &DPUVolume{}

// GetConditions returns the conditions of the DPUVolume.
func (v *DPUVolume) GetConditions() []metav1.Condition {
	return v.Status.Conditions
}

// SetConditions sets the conditions of the DPUVolume.
func (v *DPUVolume) SetConditions(conditions []metav1.Condition) {
	v.Status.Conditions = conditions
}

// ObjectReference represents a reference to a Kubernetes object.
type ObjectReference struct {
	// Name specifies the name of the referenced object
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`
	// Namespace specifies the namespace where the referenced object exists
	// +kubebuilder:validation:MinLength=1
	// +required
	Namespace string `json:"namespace"`
}

// String returns the string representation of the ObjectReference.
func (o *ObjectReference) String() string {
	if o == nil {
		return ""
	}
	return o.Namespace + "/" + o.Name
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep
// +kubebuilder:printcolumn:name="DPUStoragePolicyName",type=string,JSONPath=`.spec.dpuStoragePolicyName`
// +kubebuilder:printcolumn:name="VolumeMode",type="string",JSONPath=`.spec.volumeMode`
// +kubebuilder:printcolumn:name="Size",type="string",JSONPath=`.spec.resources.requests.storage`
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=`.status.conditions[?(@.type=='Ready')].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// DPUVolume represents a DPUVolume CR.
type DPUVolume struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              DPUVolumeSpec   `json:"spec"`
	Status            DPUVolumeStatus `json:"status,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="self.dpuStoragePolicyName == oldSelf.dpuStoragePolicyName", message="dpuStoragePolicy field is immutable"
// +kubebuilder:validation:XValidation:rule="self.parameters == oldSelf.parameters", message="parameters field is immutable"
// +kubebuilder:validation:XValidation:rule="self.accessModes == oldSelf.accessModes", message="accessModes field is immutable"
// +kubebuilder:validation:XValidation:rule="self.resources == oldSelf.resources", message="resources field is immutable"
// +kubebuilder:validation:XValidation:rule="self.volumeMode == oldSelf.volumeMode", message="volumeMode field is immutable"

// DPUVolumeSpec defines the desired state of DPUVolume
type DPUVolumeSpec struct {
	// Name of the DPUStoragePolicyName object that will be used to create the volume.
	// +kubebuilder:validation:MinLength=1
	// +required
	DPUStoragePolicyName string `json:"dpuStoragePolicyName"`
	// Additional parameters for the volume, these parameters are merged with the values from the DPUStoragePolicy object.
	// +kubebuilder:default={}
	// +optional
	Parameters map[string]string `json:"parameters,omitempty"`
	// Access modes define how the volume can be mounted. These modes are directly passed to the
	// PersistentVolumeClaim created for the Vendor CSI Plugin selected by the DPUStoragePolicy.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=3
	// +kubebuilder:validation:XValidation:rule="self.all(mode, mode in ['ReadWriteOnce', 'ReadOnlyMany', 'ReadWriteMany'])", message="accessModes must contain only valid PersistentVolumeAccessMode values: ReadWriteOnce, ReadOnlyMany, ReadWriteMany"
	// +required
	AccessModes []corev1.PersistentVolumeAccessMode `json:"accessModes"`
	// Resources represents the storage resources requested for the volume. These resource requirements
	// are directly passed to the PersistentVolumeClaim created for the Vendor CSI Plugin selected
	// by the DPUStoragePolicy. Since volume resizing is not supported, modifications to the resource request are prohibited.
	// +kubebuilder:validation:XValidation:rule="has(self.requests) && has(self.requests.storage)", message="resource requirements must contain storage resource"
	// +required
	Resources corev1.VolumeResourceRequirements `json:"resources"`
	// Volume mode defines how the volume should be mounted and used. This value is directly passed to the
	// PersistentVolumeClaim created for the Vendor CSI Plugin selected by the DPUStoragePolicy.
	// +kubebuilder:validation:Enum=Filesystem;Block
	// +kubebuilder:default=Filesystem
	// +optional
	VolumeMode *corev1.PersistentVolumeMode `json:"volumeMode,omitempty"`
}

// DPUVolumeStatus defines the observed state of DPUVolume
type DPUVolumeStatus struct {
	// Phase of the volume
	// +kubebuilder:validation:Enum=Pending;Bound
	// +optional
	Phase *DPUVolumePhase `json:"phase,omitempty"`
	// State of the volume. This field is managed by the controller. User usually do not need to set fields from this struct.
	// +optional
	State *DPUVolumeState `json:"state,omitempty"`
	// Conditions defines current service state.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedGeneration records the Generation observed on the object the last time it was patched.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// DPUVolumeState defines the state of the volume.
type DPUVolumeState struct {
	// DPUCluster contains the reference to the DPUCluster object that was selected for volume creation.
	// +optional
	DPUCluster *ObjectReference `json:"dpuCluster,omitempty"`
	// Parameters contains the final set of parameters for volume creation, computed by merging
	// the parameters from the DPUStoragePolicy object with user-provided parameters.
	// +optional
	Parameters map[string]string `json:"parameters,omitempty"`
	// SelectedDPUStorageVendorName contains the name of the DPUStorageVendor object that was selected for volume creation.
	// +optional
	SelectedDPUStorageVendorName *string `json:"selectedDPUStorageVendorName,omitempty"`
	// StorageVendorPluginName contains the name of the storage vendor plugin deployed on the DPU cluster that was selected for volume creation.
	// +optional
	StorageVendorPluginName *string `json:"storageVendorPluginName,omitempty"`
	// StorageClassName contains the name of the storage class in the DPU cluster that was selected for volume creation.
	// +optional
	StorageClassName *string `json:"storageClassName,omitempty"`
	// CSIDriverName contains the name of the CSI driver in the DPU cluster that was selected for volume creation.
	// +optional
	CSIDriverName *string `json:"csiDriverName,omitempty"`
	// PersistentVolumeClaimRef contains the reference to the PersistentVolumeClaim object in the DPU cluster that was created for the volume.
	// +optional
	PersistentVolumeClaimRef *ObjectReference `json:"persistentVolumeClaimRef,omitempty"`
	// VolumeInfo contains a subset of fields from the PersistentVolume object created in the DPU cluster
	// +optional
	VolumeInfo *VolumeInfo `json:"volumeInfo,omitempty"`
}

// VolumeInfo represents a subset of fields from the PersistentVolume object that was created in the DPU cluster.
// This struct is used to track and expose key volume information without carrying the full PersistentVolume object.
type VolumeInfo struct {
	// Actual capacity of the volume in the DPU cluster
	// +optional
	Capacity corev1.ResourceList `json:"capacity,omitempty"`
	// Actual access modes of the volume in the DPU cluster
	// +optional
	AccessModes []corev1.PersistentVolumeAccessMode `json:"accessModes,omitempty"`
	// Actual volume mode of the volume in the DPU cluster
	// +optional
	VolumeMode *corev1.PersistentVolumeMode `json:"volumeMode,omitempty"`
	// VolumeAttributes from the PersistentVolume object in the DPU cluster
	// This field usually contains parameters returned by the Vendor CSI plugin on volume creation.
	// +optional
	VolumeAttributes map[string]string `json:"volumeAttributes,omitempty"`
}

// +kubebuilder:object:root=true

// DPUVolumeList contains a list of DPUVolume
type DPUVolumeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DPUVolume `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DPUVolume{}, &DPUVolumeList{})
}
