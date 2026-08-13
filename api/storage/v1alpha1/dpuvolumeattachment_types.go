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
	// DPUVolumeAttachmentKind is the kind of the DPUVolumeAttachment object
	DPUVolumeAttachmentKind = "DPUVolumeAttachment"
	// DPUVolumeAttachmentFinalizer is the finalizer for the DPUVolumeAttachment object
	DPUVolumeAttachmentFinalizer = "storage.dpu.nvidia.com/dpuvolumeattachment"
)

// DPUVolumeAttachmentGroupVersionKind is the GroupVersionKind of the DPUVolumeAttachment object
var DPUVolumeAttachmentGroupVersionKind = GroupVersion.WithKind(DPUVolumeAttachmentKind)

// Condition types
const (
	// ConditionDPUVolumeAttachmentReconciled is the condition type that indicates that the
	// DPUVolumeAttachment is reconciled.
	ConditionDPUVolumeAttachmentReconciled conditions.ConditionType = "DPUVolumeAttachmentReconciled"
	// ConditionDPUVolumeAttachmentControllerAttached is the condition type that indicates that
	// the Volume was attached by the vendor CSI driver
	ConditionDPUVolumeAttachmentControllerAttached conditions.ConditionType = "ControllerAttached"
	// ConditionDPUVolumeAttachmentDPUAttached is the condition type that indicates that
	// the Volume was attached by the DPU
	ConditionDPUVolumeAttachmentDPUAttached conditions.ConditionType = "DPUAttached"
)

type FunctionType string

const (
	// FunctionTypePF is the PF function type
	FunctionTypePF FunctionType = "pf"
	// FunctionTypeVF is the VF function type
	FunctionTypeVF FunctionType = "vf"
)

var (
	// DPUVolumeAttachmentConditions is the list of conditions that the DPUVolumeAttachment can have.
	DPUVolumeAttachmentConditions = []conditions.ConditionType{
		conditions.TypeReady,
		ConditionDPUVolumeAttachmentReconciled,
		ConditionDPUVolumeAttachmentControllerAttached,
		ConditionDPUVolumeAttachmentDPUAttached,
	}
)

var _ conditions.GetSet = &DPUVolumeAttachment{}

// GetConditions returns the conditions of the DPUVolumeAttachment.
func (v *DPUVolumeAttachment) GetConditions() []metav1.Condition {
	return v.Status.Conditions
}

// SetConditions sets the conditions of the DPUVolumeAttachment.
func (v *DPUVolumeAttachment) SetConditions(conditions []metav1.Condition) {
	v.Status.Conditions = conditions
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep
// +kubebuilder:printcolumn:name="DPUVolumeName",type=string,JSONPath=`.spec.dpuVolumeName`
// +kubebuilder:printcolumn:name="DPUNodeName",type=string,JSONPath=`.spec.dpuNodeName`
// +kubebuilder:printcolumn:name="FunctionType",type=string,JSONPath=`.spec.functionType`
// +kubebuilder:printcolumn:name="HotplugFunction",type=boolean,JSONPath=`.spec.hotplugFunction`
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=`.status.conditions[?(@.type=='Ready')].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// DPUVolumeAttachment represents a Volume CR on the DPU cluster.
type DPUVolumeAttachment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              DPUVolumeAttachmentSpec   `json:"spec"`
	Status            DPUVolumeAttachmentStatus `json:"status,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="!(self.hotplugFunction == true && self.functionType != 'pf')", message="hotplugFunction can only be true when functionType is 'pf'"
// +kubebuilder:validation:XValidation:rule="self.dpuNodeName == oldSelf.dpuNodeName", message="dpuNode field is immutable"
// +kubebuilder:validation:XValidation:rule="self.dpuVolumeName == oldSelf.dpuVolumeName", message="dpuVolume field is immutable"
// +kubebuilder:validation:XValidation:rule="self.functionType == oldSelf.functionType", message="functionType field is immutable"
// +kubebuilder:validation:XValidation:rule="self.hotplugFunction == oldSelf.hotplugFunction", message="hotplugFunction field is immutable"

// DPUVolumeAttachmentSpec defines the desired state of DPUVolumeAttachment
type DPUVolumeAttachmentSpec struct {
	// DPUNodeName is the name of DPUNode object that represents the node to which the volume should
	// be attached
	// +kubebuilder:validation:MinLength=1
	// +required
	DPUNodeName string `json:"dpuNodeName"`
	// DPUVolumeName is the name of DPUVolume object that represents the volume to be attached
	// +kubebuilder:validation:MinLength=1
	// +required
	DPUVolumeName string `json:"dpuVolumeName"`
	// FunctionTypeConfig is the configuration for the emulated function that should be used to attach the volume
	FunctionTypeConfig `json:",inline"`
}

// FunctionTypeConfig is the configuration for the emulated function that should be used to attach the volume
type FunctionTypeConfig struct {
	// FunctionType is the type of the emulated function that should be used to attach the volume
	// +kubebuilder:validation:Enum=pf;vf
	// +required
	FunctionType FunctionType `json:"functionType"`
	// HotplugFunction is a boolean flag that indicates if the emulated function should be hotplugged
	// +required
	HotplugFunction bool `json:"hotplugFunction"`
}

// DPUVolumeAttachmentStatus defines the observed state of DPUVolumeAttachment
type DPUVolumeAttachmentStatus struct {
	// Indicates the volume is successfully attached to by the Vendor CSI driver
	ControllerAttached *bool `json:"controllerAttached,omitempty"`
	// Indicates the volume is successfully attached to the node by DPU
	// +optional
	DPUAttached *bool `json:"dpuAttached,omitempty"`
	// AttachmentMetadata contains the metadata of the volume attachment returned by the Vendor CSI driver
	// +optional
	AttachmentMetadata map[string]string `json:"attachmentMetadata,omitempty"`
	// Details about the DPU attachment
	// +optional
	DPU *AttachmentStatusDPU `json:"dpu,omitempty"`
	// The last error encountered during the attach operation, if any
	// +optional
	Message *string `json:"message,omitempty"`
	// Conditions defines current service state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedGeneration records the Generation observed on the object the last time it was patched.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true

// DPUVolumeAttachmentList contains a list of DPUVolumeAttachment
type DPUVolumeAttachmentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DPUVolumeAttachment `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DPUVolumeAttachment{}, &DPUVolumeAttachmentList{})
}

// AttachmentStatusDPU describe the information of DPU volume
type AttachmentStatusDPU struct {
	// PCI device address in the following format: (bus:device.function)
	// +optional
	PCIAddress *string `json:"pciAddress,omitempty"`
	// The VUID of the emulated function. For a VF, this is the parent PF VUID.
	// +optional
	FuncVUID *string `json:"funcVUID,omitempty"`
	// The name of the device that was created by the storage vendor plugin
	// +optional
	DeviceName *string `json:"deviceName,omitempty"`
	// The attributes of the emulated NVME function
	// +optional
	NVMEAttrs *NVMEAttrs `json:"nvmeAttrs,omitempty"`
	// The attributes of the emulated VirtioFS function
	// +optional
	VirtioFSAttrs *VirtioFSAttrs `json:"virtioFSAttrs,omitempty"`
}

// VirtioFSAttrs represents the attributes of the VirtioFS emulated function
type VirtioFSAttrs struct {
	// Filesystem tag identified by SNAP on the host (used for the mount). Relevant for volume of type filesystem
	// +optional
	FilesystemTag *string `json:"filesystemTag,omitempty"`
}

// NVMEAttrs represents the attributes of the NVME emulated function
type NVMEAttrs struct {
	// The namespace ID within the NVME controller
	// +optional
	NamespaceID *int64 `json:"namespaceID,omitempty"`
	// The NVMe namespace UUID
	// +optional
	NamespaceUUID *string `json:"namespaceUUID,omitempty"`
}
