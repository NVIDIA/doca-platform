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

var DPUServiceNADGroupVersionKind = GroupVersion.WithKind(DPUServiceNADKind)

// Status related variables
const (
	DPUServiceNADKind                                        = "DPUServiceNAD"
	ConditionDPUNADObjectReconciled conditions.ConditionType = "DPUNADObjectReconciled"
	ConditionDPUNADObjectReady      conditions.ConditionType = "DPUNADObjectReady"
	// TrustedSfAnnotationKey is the key of the annotation that may be added to a DPUServiceNAD to
	// indicate that the SFs to be used should be trusted instead of the normal ones.
	TrustedSfAnnotationKey = "dpuservicenad.svc.dpu.nvidia.com/use-trusted-sfs"
)

// status conditions on DPUServiceNAD crd object
var (
	DPUServiceNADConditions = []conditions.ConditionType{
		conditions.TypeReady,
		ConditionDPUNADObjectReconciled,
		ConditionDPUNADObjectReady,
	}
)

var _ conditions.GetSet = &DPUServiceNAD{}

func (c *DPUServiceNAD) GetConditions() []metav1.Condition {
	return c.Status.Conditions
}

func (c *DPUServiceNAD) SetConditions(conditions []metav1.Condition) {
	c.Status.Conditions = conditions
}

// GetResourceName returns the resource name associated with the resource type specified in the DPUServiceNAD.
func (c *DPUServiceNAD) GetResourceName() string {
	switch c.Spec.ResourceType {
	case "sf":
		if _, exists := c.Annotations[TrustedSfAnnotationKey]; exists {
			return "nvidia.com/bf_sf_trusted"
		}
		return "nvidia.com/bf_sf"
	case "vf":
		return "nvidia.com/bf_vf"
	case "veth":
		return ""
	default:
		return "nvidia.com/bf_sf"
	}
}

// DPUServiceNADSpec defines the desired state of DPUServiceNAD.
type DPUServiceNADSpec struct {
	ObjectMeta `json:"metadata,omitempty"`
	// +kubebuilder:validation:Enum={"vf", "sf", "veth"}
	// +required
	ResourceType string `json:"resourceType"`
	// +optional
	Bridge string `json:"bridge,omitempty"`
	// +optional
	ServiceMTU int `json:"serviceMTU,omitempty"`
	// +optional
	IPAM bool `json:"ipam,omitempty"`
}

// DPUServiceNADStatus defines the observed state of DPUServiceNAD.
type DPUServiceNADStatus struct {
	// Conditions reflect the status of the object
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep
// +kubebuilder:validation:XValidation:rule="self.metadata.name.size() <= 63", message="name length can't be bigger than 63 chars"

// DPUServiceNAD is the Schema for the dpuservicenads API.
type DPUServiceNAD struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DPUServiceNADSpec   `json:"spec,omitempty"`
	Status DPUServiceNADStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DPUServiceNADList contains a list of DPUServiceNAD.
type DPUServiceNADList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DPUServiceNAD `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DPUServiceNAD{}, &DPUServiceNADList{})
}
