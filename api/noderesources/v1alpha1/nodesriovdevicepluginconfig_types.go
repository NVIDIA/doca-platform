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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// NodeSRIOVDevicePluginConfigKind is the kind of the NodeSRIOVDevicePluginConfig in string format.
	NodeSRIOVDevicePluginConfigKind = "NodeSRIOVDevicePluginConfig"
)

var (
	// NodeSRIOVDevicePluginConfigGroupVersionKind is the GroupVersionKind of the NodeSRIOVDevicePluginConfig object
	NodeSRIOVDevicePluginConfigGroupVersionKind = GroupVersion.WithKind(NodeSRIOVDevicePluginConfigKind)
)

// DevicePluginResourceType specifies the type of the device plugin resource.
// +kubebuilder:validation:Enum=vf;sf
type DevicePluginResourceType string

const (
	// DevicePluginResourceTypeVF represents a Virtual Function resource.
	DevicePluginResourceTypeVF DevicePluginResourceType = "vf"
	// DevicePluginResourceTypeSF represents a Scalable Function resource.
	DevicePluginResourceTypeSF DevicePluginResourceType = "sf"
)

// IsSupported reports whether t is a recognized device plugin resource type.
func (t DevicePluginResourceType) IsSupported() bool {
	switch t {
	case DevicePluginResourceTypeVF, DevicePluginResourceTypeSF:
		return true
	default:
		return false
	}
}

// NodeSRIOVDevicePluginConfigSpec defines the desired state of NodeSRIOVDevicePluginConfig
type NodeSRIOVDevicePluginConfigSpec struct {
	// DevicePluginResources is the list of device plugin resource configurations.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=64
	// +required
	DevicePluginResources []DevicePluginResource `json:"devicePluginResources"`
}

// DevicePluginResource defines a single device plugin resource configuration.
// +kubebuilder:validation:XValidation:rule="self.type != 'sf' || self.ranges.all(r, has(r.start) && has(r.end))",message="start and end must be set when type is sf"
type DevicePluginResource struct {
	// Name is the endpoint resource name for the device plugin.
	// Should contain only alphanumeric characters, underscores and hyphens.
	// The full extended resource name will be constructed as resource-prefix/name.
	// Example: pods_vf, ovnk_mgmt_vf, pods_sf
	// +kubebuilder:validation:Pattern=`^[a-zA-Z0-9_-]+$`
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`

	// ResourcePrefix is the resource prefix used by the device plugin to prefix the resource name.
	// If not set, the default resource prefix will be used.
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	// +optional
	ResourcePrefix *string `json:"resourcePrefix,omitempty"`

	// Type specifies the type of the device plugin resource.
	// +required
	Type DevicePluginResourceType `json:"type"`

	// Options contains additional options for the device plugin resource.
	// +optional
	Options *DevicePluginResourceOptions `json:"options,omitempty"`

	// Ranges specifies the function ranges on PFs to be included in this resource.
	// For type sf, each range must set both start and end.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=1024
	// +required
	Ranges []FunctionRange `json:"ranges"`
}

// DevicePluginResourceOptions contains additional options for a device plugin resource.
type DevicePluginResourceOptions struct {
	// IsRdma indicates whether RDMA is enabled for this resource.
	// +optional
	IsRdma *bool `json:"isRdma,omitempty"`

	// NeedVhostNet mounts /dev/vhost-net and /dev/net/tun with the allocated devices.
	// Defaults to false.
	// +optional
	NeedVhostNet *bool `json:"needVhostNet,omitempty"`
}

// FunctionRange defines a range of functions (Virtual Functions or Scalable Functions)
// on a Physical Function.
// +kubebuilder:validation:XValidation:rule="!has(self.start) || !has(self.end) || self.start <= self.end",message="start must be less than or equal to end"
type FunctionRange struct {
	// PFIndex is the index of the Physical Function.
	// +kubebuilder:validation:Minimum=0
	// +required
	PFIndex int32 `json:"pfIndex"`

	// Start is the starting function index (inclusive).
	// If not set, the range starts from index 0.
	// Required when type is sf. SF ranges are contiguous: every index from
	// start to end (inclusive) must exist on the PF.
	// +kubebuilder:validation:Minimum=0
	// +optional
	Start *int32 `json:"start,omitempty"`

	// End is the ending function index (inclusive).
	// If not set, the range extends to the last function of this type on the PF.
	// Required when type is sf. SF ranges are contiguous: every index from
	// start to end (inclusive) must exist on the PF.
	// +kubebuilder:validation:Minimum=0
	// +optional
	End *int32 `json:"end,omitempty"`
}

// NodeSRIOVDevicePluginConfigStatus defines the observed state of NodeSRIOVDevicePluginConfig
type NodeSRIOVDevicePluginConfigStatus struct {
	// Conditions exposes the current state of the NodeSRIOVDevicePluginConfig.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration records the Generation observed on the object the last time it was patched.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=`.status.conditions[?(@.type=='Ready')].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// NodeSRIOVDevicePluginConfig is the Schema for the nodesriovdevicepluginconfigs API
type NodeSRIOVDevicePluginConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NodeSRIOVDevicePluginConfigSpec   `json:"spec,omitempty"`
	Status NodeSRIOVDevicePluginConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// NodeSRIOVDevicePluginConfigList contains a list of NodeSRIOVDevicePluginConfig
type NodeSRIOVDevicePluginConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NodeSRIOVDevicePluginConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NodeSRIOVDevicePluginConfig{}, &NodeSRIOVDevicePluginConfigList{})
}
