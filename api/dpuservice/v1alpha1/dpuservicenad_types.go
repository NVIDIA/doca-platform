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
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	DPUServiceNADKind = "DPUServiceNAD"
	// TrustedSfAnnotationKey is the key of the annotation that may be added to a DPUServiceNAD to
	// indicate that the SFs to be used should be trusted instead of the normal ones.
	TrustedSfAnnotationKey = "dpuservicenad.svc.dpu.nvidia.com/use-trusted-sfs"
)

var DPUServiceNADGroupVersionKind = GroupVersion.WithKind(DPUServiceNADKind)

const (
	ConditionDPUNADObjectReconciled conditions.ConditionType = "DPUNADObjectReconciled"
	ConditionDPUNADObjectReady      conditions.ConditionType = "DPUNADObjectReady"
)

var DPUServiceNADConditions = []conditions.ConditionType{
	conditions.TypeReady,
	ConditionDPUNADObjectReconciled,
	ConditionDPUNADObjectReady,
}

var _ conditions.GetSet = &DPUServiceNAD{}
var _ DPUServiceObject = &DPUServiceNAD{}

func (c *DPUServiceNAD) GetConditions() []metav1.Condition {
	return c.Status.Conditions
}

func (c *DPUServiceNAD) SetConditions(conditions []metav1.Condition) {
	c.Status.Conditions = conditions
}

// GetDPUClusterSelector returns the DPUCluster selector of the DPUServiceNAD
func (c *DPUServiceNAD) GetDPUClusterSelector() *metav1.LabelSelector {
	return c.Spec.DPUClusterSelector
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

// CNIPlugin defines a CNI plugin to be used in a chained CNI configuration.
// When multiple CNI plugins are specified in ChainedCNIs, they are executed in order
// after the base OVS CNI plugin to provide additional network functionality.
type CNIPlugin struct {
	// Type specifies the CNI plugin type to be used in the chain.
	// Currently only "rdma" is supported, which enables RDMA capabilities for the network interface.
	// +kubebuilder:validation:Enum={"rdma"}
	// +required
	Type *string `json:"type"`
	// Config contains optional plugin-specific configuration as raw JSON.
	// The configuration is merged into the CNI plugin configuration.
	// +optional
	Config *runtime.RawExtension `json:"config,omitempty"`
}

// DPUServiceNADSpec defines the desired state of DPUServiceNAD.
type DPUServiceNADSpec struct {
	// DPUClusterSelector determines in which clusters the DPUServiceNAD controller should apply the configuration.
	// +optional
	DPUClusterSelector *metav1.LabelSelector `json:"dpuClusterSelector,omitempty"`
	// ResourceType specifies the type of network resource to allocate for pods using this NAD.
	// - "vf": Virtual Function (SR-IOV VF) from the DPU's physical ports
	// - "sf": Scalable Function from the DPU (maps to nvidia.com/bf_sf or nvidia.com/bf_sf_trusted)
	// - "veth": Virtual Ethernet pair (no device plugin resource required)
	// The resource type determines which SR-IOV device plugin resource will be requested.
	// +kubebuilder:validation:Enum={"vf", "sf", "veth"}
	// +required
	ResourceType string `json:"resourceType"`
	// Bridge specifies the name of the OVS bridge to which the network interface will be connected.
	// This bridge name is used in the CNI configuration for the OVS plugin.
	// +optional
	Bridge string `json:"bridge,omitempty"`
	// ServiceMTU specifies the MTU size in bytes for the network interface.
	// This value is passed to the OVS CNI plugin and determines the maximum packet size.
	// If there is a DPUServiceChain that references an interface that is part of this network,
	// then the MTU that is defined in the DPUServiceChain takes precedence.
	// The default is 1500.
	// +kubebuilder:validation:Minimum=1280
	// +kubebuilder:validation:Maximum=9216
	// +kubebuilder:default=1500
	// +optional
	ServiceMTU int `json:"serviceMTU,omitempty"`
	// IPAM enables IP Address Management for the network interfaces attached to this network
	// When set to true, a DPUServiceChain that references the DPUServiceInterface that has
	// requested this network must be created and include the relevant IPAM information. See
	// DPUServiceChain documentation for more.
	// When set to false, the network interfaces attached to this network will not get an IP
	// +optional
	IPAM bool `json:"ipam,omitempty"`
	// ChainedCNIs specifies additional CNI plugins to be chained after the base OVS plugin.
	// When specified, the NAD will use the CNI chaining format with the OVS plugin as the
	// first plugin, followed by the plugins defined in this list.
	// This allows adding capabilities like RDMA support on top of the base network interface.
	// If empty, the NAD uses a single OVS plugin configuration (backward compatible format).
	// +optional
	ChainedCNIs []CNIPlugin `json:"chainedCNIs,omitempty"`
}

// DPUServiceNADStatus defines the observed state of DPUServiceNAD.
type DPUServiceNADStatus struct {
	// Conditions reflect the status of the object
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
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
