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
	// NodeSRIOVDevicePluginConfigKind is the kind of the NodeSRIOVDevicePluginConfig in string format.
	NodeSRIOVDevicePluginConfigKind = "NodeSRIOVDevicePluginConfig"
)

var (
	// NodeSRIOVDevicePluginConfigGroupVersionKind is the GroupVersionKind of the NodeSRIOVDevicePluginConfig object
	NodeSRIOVDevicePluginConfigGroupVersionKind = GroupVersion.WithKind(NodeSRIOVDevicePluginConfigKind)
)

// Status related constants
const (
	// ConditionNodeSRIOVDevicePluginConfigReady indicates that the config is ready (reconciled and validated).
	ConditionNodeSRIOVDevicePluginConfigReady conditions.ConditionType = "ConfigReady"
)

var (
	// NodeSRIOVDevicePluginConfigConditions is the list of conditions for NodeSRIOVDevicePluginConfig.
	NodeSRIOVDevicePluginConfigConditions = []conditions.ConditionType{
		conditions.TypeReady,
		ConditionNodeSRIOVDevicePluginConfigReady,
	}
)

var _ conditions.GetSet = &NodeSRIOVDevicePluginConfig{}

// DevicePluginResourceType specifies the type of the device plugin resource.
// +kubebuilder:validation:Enum=vf
type DevicePluginResourceType string

const (
	// DevicePluginResourceTypeVF represents a Virtual Function resource.
	DevicePluginResourceTypeVF DevicePluginResourceType = "vf"
)

// NodeSRIOVDevicePluginConfigSpec defines the desired state of NodeSRIOVDevicePluginConfig
type NodeSRIOVDevicePluginConfigSpec struct {
	// DevicePluginResources is the list of device plugin resource configurations.
	// +kubebuilder:validation:MinItems=1
	// +required
	DevicePluginResources []DevicePluginResource `json:"devicePluginResources"`
}

// DevicePluginResource defines a single device plugin resource configuration.
type DevicePluginResource struct {
	// Name is the endpoint resource name for the device plugin.
	// Should contain only alphanumeric characters, underscores and hyphens.
	// The full extended resource name will be constructed as resource-prefix/name.
	// Example: pods_vf, ovnk_mgmt_vf
	// +kubebuilder:validation:Pattern=`^[a-zA-Z0-9_-]+$`
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`

	// ResourcePrefix is the resource prefix used by the device plugin to prefix the resource name.
	// If not set, the default resource prefix will be used.
	// +kubebuilder:validation:Pattern=`^[a-zA-Z0-9][a-zA-Z0-9-]*(\.[a-zA-Z0-9][a-zA-Z0-9-]*)+$`
	// +optional
	ResourcePrefix *string `json:"resourcePrefix,omitempty"`

	// Type specifies the type of the device plugin resource.
	// +required
	Type DevicePluginResourceType `json:"type"`

	// Options contains additional options for the device plugin resource.
	// +optional
	Options *DevicePluginResourceOptions `json:"options,omitempty"`

	// Ranges specifies the VF ranges on PFs to be included in this resource.
	// +kubebuilder:validation:MinItems=1
	// +required
	Ranges []VFRange `json:"ranges"`
}

// DevicePluginResourceOptions contains additional options for a device plugin resource.
type DevicePluginResourceOptions struct {
	// IsRdma indicates whether RDMA is enabled for this resource.
	// +optional
	IsRdma *bool `json:"isRdma,omitempty"`
}

// VFRange defines a range of Virtual Functions on a Physical Function.
// +kubebuilder:validation:XValidation:rule="!has(self.start) || !has(self.end) || self.start <= self.end",message="start must be less than or equal to end"
type VFRange struct {
	// PFIndex is the index of the Physical Function.
	// +kubebuilder:validation:Minimum=0
	// +required
	PFIndex int32 `json:"pfIndex"`

	// Start is the starting VF index (inclusive).
	// If not set, the range starts from VF 0.
	// +kubebuilder:validation:Minimum=0
	// +optional
	Start *int32 `json:"start,omitempty"`

	// End is the ending VF index (inclusive).
	// If not set, the range extends to the last VF on the PF.
	// +kubebuilder:validation:Minimum=0
	// +optional
	End *int32 `json:"end,omitempty"`
}

// NodeSRIOVDevicePluginConfigStatus defines the observed state of NodeSRIOVDevicePluginConfig
type NodeSRIOVDevicePluginConfigStatus struct {
	// Conditions exposes the current state of the NodeSRIOVDevicePluginConfig.
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration records the Generation observed on the object the last time it was patched.
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

// GetConditions returns the conditions of the NodeSRIOVDevicePluginConfig.
func (c *NodeSRIOVDevicePluginConfig) GetConditions() []metav1.Condition {
	return c.Status.Conditions
}

// SetConditions sets the conditions of the NodeSRIOVDevicePluginConfig.
func (c *NodeSRIOVDevicePluginConfig) SetConditions(conditions []metav1.Condition) {
	c.Status.Conditions = conditions
}
