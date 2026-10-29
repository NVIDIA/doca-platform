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
	// DPUNodeMaintenanceKind is the kind of the DPUNodeMaintenance object
	DPUNodeMaintenanceKind = "DPUNodeMaintenance"
	// DPUNodeMaintenanceListKind is the kind of the DPUNodeMaintenanceList object
	DPUNodeMaintenanceListKind = "DPUNodeMaintenanceList"
	// DPUNodeMaintenanceFinalizer is the finalizer for the DPUNodeMaintenance object
	DPUNodeMaintenanceFinalizer = "provisioning.dpu.nvidia.com/dpunodemaintenance-protection"
)

// DPUNodeMaintenanceGroupVersionKind is the GroupVersionKind of the DPUNodeMaintenance object
var DPUNodeMaintenanceGroupVersionKind = GroupVersion.WithKind(DPUNodeMaintenanceKind)

// Status related variables
const (
	ConditionNodeEffectApplied conditions.ConditionType = "NodeEffectApplied"
	ConditionNodeEffectRemoved conditions.ConditionType = "NodeEffectRemoved"
)

var _ conditions.GetSet = &DPUNodeMaintenance{}

func (c *DPUNodeMaintenance) GetConditions() []metav1.Condition {
	return c.Status.Conditions
}

func (c *DPUNodeMaintenance) SetConditions(conditions []metav1.Condition) {
	c.Status.Conditions = conditions
}

// DPUNodeMaintenanceSpec is the specification of the DPUNodeMaintenance object
type DPUNodeMaintenanceSpec struct {
	// DPUNodeName is the name of the DPUNode that is being maintained.
	// +required
	DPUNodeName string `json:"dpuNodeName"`
	// NodeEffect is the effect to be applied to the node.
	// +optional
	NodeEffect *NodeEffect `json:"nodeEffect,omitempty"`
	// Requestor is the list of consumers for the maintenance.
	// +optional
	Requestor []string `json:"requestor,omitempty"`
}

// DPUNodeMaintenanceStatus defines the observed state of DPUNodeMaintenance
type DPUNodeMaintenanceStatus struct {
	// Conditions reflect the status of the object
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// NodeEffectSyncStartTime is the time when the node effect sync started.
	// +optional
	NodeEffectSyncStartTime *metav1.Time `json:"nodeEffectSyncStartTime,omitempty"`
	// MultiDPUOperationsSyncWaitTime  is the wait time between DPUs on the same node.
	// +optional
	MultiDPUOperationsSyncWaitTime *metav1.Duration `json:"multiDPUOperationsSyncWaitTime,omitempty"`
	// MaxUnavailableDPUNodes is the maximum number of DPUNodes that are unavailable during the node effect period.
	// +kubebuilder:validation:Minimum=1
	// +optional
	MaxUnavailableDPUNodes *int32 `json:"maxUnavailableDPUNodes,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep
// +kubebuilder:validation:XValidation:rule="self.metadata.name.size() <= 63", message="name length can't be bigger than 63 chars"

// DPUNodeMaintenance is the Schema for the dpunodemaintenances API
type DPUNodeMaintenance struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DPUNodeMaintenanceSpec   `json:"spec,omitempty"`
	Status DPUNodeMaintenanceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DPUNodeMaintenanceList contains a list of DPUNodeMaintenance
type DPUNodeMaintenanceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DPUNodeMaintenance `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DPUNodeMaintenance{}, &DPUNodeMaintenanceList{})
}
