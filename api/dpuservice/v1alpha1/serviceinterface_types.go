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
	"strings"

	"github.com/nvidia/doca-platform/pkg/conditions"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	ServiceInterfaceKind = "ServiceInterface"

	// InterfaceTypeVLAN is the vlan interface type
	InterfaceTypeVLAN = "vlan"
	// InterfaceTypePhysical is the physical interface type
	InterfaceTypePhysical = "physical"
	// InterfaceTypePF is the pf interface type
	InterfaceTypePF = "pf"
	// InterfaceTypeVF is the vf interface type
	InterfaceTypeVF = "vf"
	// InterfaceTypeOVN is the ovn interface type
	InterfaceTypeOVN = "ovn"
	// InterfaceTypePatch is the patch interface type
	InterfaceTypePatch = "patch"
	// InterfaceTypeService is the service interface type
	InterfaceTypeService = "service"
)

var ServiceInterfaceGroupVersionKind = GroupVersion.WithKind(ServiceInterfaceKind)

// ServiceInterfaceSpec defines the desired state of ServiceInterface
// +kubebuilder:validation:XValidation:rule="(self.interfaceType == 'vlan' && has(self.vlan)) || (self.interfaceType == 'pf' && has(self.pf)) || (self.interfaceType == 'vf' && has(self.vf)) || (self.interfaceType == 'physical' && has(self.physical)) || (self.interfaceType == 'service' && has(self.service)) || (self.interfaceType == 'ovn') || (self.interfaceType == 'patch' && has(self.patch))", message="`for interfaceType=vlan, vlan must be set; for interfaceType=pf, pf must be set; for interfaceType=vf, vf must be set; for interfaceType=physical, physical must be set; for interfaceType=service, service must be set; for interfaceType=patch, patch must be set`"
type ServiceInterfaceSpec struct {
	// Node where this interface exists
	// +optional
	Node *string `json:"node,omitempty"`
	// The interface type ("vlan", "physical", "pf", "vf", "ovn", "patch", "service")
	// +kubebuilder:validation:Enum={"vlan", "physical", "pf", "vf", "ovn", "patch", "service"}
	// +required
	InterfaceType string `json:"interfaceType"`
	// The physical interface definition
	// +optional
	Physical *Physical `json:"physical,omitempty"`
	// The VLAN definition
	// +optional
	Vlan *VLAN `json:"vlan,omitempty"`
	// The VF definition
	// +optional
	VF *VF `json:"vf,omitempty"`
	// The PF definition
	// +optional
	PF *PF `json:"pf,omitempty"`
	// The Service definition
	// +optional
	Service *ServiceDef `json:"service,omitempty"`
	// The OVN definition
	// Deprecated: This field is deprecated and will be removed with v26.10.0.
	// Migrate to interfaceType="patch" with spec.patch.peerBridge and spec.patch.peerPatchName instead.
	// +optional
	OVN *OVN `json:"ovn,omitempty"`
	// The Patch definition
	// +optional
	Patch *PatchDef `json:"patch,omitempty"`
}

const (
	// ServiceInterfaceReconciled is the condition type that indicates that the
	// service interface is reconciled.
	ServiceInterfaceReconciled conditions.ConditionType = "ServiceInterfaceReconciled"
)

var (
	// ServiceInterfaceConditions are the sfc-controller conditions
	ServiceInterfaceConditions = []conditions.ConditionType{
		conditions.TypeReady,
		ServiceInterfaceReconciled,
	}

	// ServiceInterfaceVPCConditions are the vpc-controller conditions
	ServiceInterfaceVPCConditions = []conditions.ConditionType{
		conditions.TypeReady,
		ServiceInterfaceReconciled,
	}
)

var _ conditions.GetSet = &ServiceInterface{}

func (c *ServiceInterface) GetConditions() []metav1.Condition {
	return c.Status.Conditions
}

func (c *ServiceInterface) SetConditions(conditions []metav1.Condition) {
	c.Status.Conditions = conditions
}

// HasVirtualNetwork checks if any component (PF/VF/Service) defines a VirtualNetwork.
func (c *ServiceInterface) HasVirtualNetwork() bool {
	return c.GetVirtualNetworkName() != ""
}

// GetVirtualNetworkName returns the virtual network name from the DPUServiceInterface.
// if it does not have a virtual network, it returns an empty string
func (c *ServiceInterface) GetVirtualNetworkName() string {
	vnn := ""
	switch c.Spec.InterfaceType {
	case InterfaceTypePF:
		if c.Spec.PF != nil &&
			c.Spec.PF.VirtualNetwork != nil {
			vnn = *c.Spec.PF.VirtualNetwork
		}
	case InterfaceTypeVF:
		if c.Spec.VF != nil &&
			c.Spec.VF.VirtualNetwork != nil {
			vnn = *c.Spec.VF.VirtualNetwork
		}
	case InterfaceTypeService:
		if c.Spec.Service != nil &&
			c.Spec.Service.VirtualNetwork != nil {
			vnn = *c.Spec.Service.VirtualNetwork
		}
	}
	return vnn
}

// Physical Identifies a physical interface
type Physical struct {
	// The interface name
	// +required
	InterfaceName string `json:"interfaceName"`
}

// ServiceDef Identifies the service and network for the ServiceInterface
// +kubebuilder:validation:XValidation:rule="(!has(oldSelf.virtualNetwork) && !has(self.virtualNetwork)) || ((has(oldSelf.virtualNetwork) && has(self.virtualNetwork)) && self.virtualNetwork==oldSelf.virtualNetwork)", message="virtualNetwork is immutable"
type ServiceDef struct {
	// ServiceID is the DPU Service Identifier
	// +required
	ServiceID string `json:"serviceID"`
	// Network is the Network Attachment Definition in the form of "namespace/name"
	// or just "name" if the namespace is the same as the ServiceInterface.
	// +required
	Network string `json:"network"`
	// The interface name
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=15
	// +required
	InterfaceName string `json:"interfaceName"`
	// VirtualNetwork is the VirtualNetwork name in the same namespace
	// +optional
	VirtualNetwork *string `json:"virtualNetwork,omitempty"`
}

// GetNetwork returns the namespace and name of the network
func (s *ServiceDef) GetNetwork() (string, string) {
	if s.Network == "" {
		return "", ""
	}

	split := strings.Split(s.Network, string(types.Separator))

	switch len(split) {
	case 1:
		return "", split[0]
	case 2:
		return split[0], split[1]
	}
	return "", ""
}

// VLAN defines the VLAN configuration
type VLAN struct {
	// The VLAN ID
	// +required
	VlanID int `json:"vlanID"`
	// The parent interface reference
	// TODO: Figure out what this field is supposed to be
	// +required
	ParentInterfaceRef string `json:"parentInterfaceRef"`
}

// VF defines the VF configuration
// +kubebuilder:validation:XValidation:rule="(!has(oldSelf.virtualNetwork) && !has(self.virtualNetwork)) || ((has(oldSelf.virtualNetwork) && has(self.virtualNetwork)) && self.virtualNetwork==oldSelf.virtualNetwork)", message="virtualNetwork is immutable"
type VF struct {
	// The VF ID
	// +required
	VFID int `json:"vfID"`
	// The PF ID
	// +required
	PFID int `json:"pfID"`
	// The parent interface reference
	// TODO: Figure out what this field is supposed to be
	// +optional
	ParentInterfaceRef *string `json:"parentInterfaceRef,omitempty"`
	// VirtualNetwork is the VirtualNetwork name in the same namespace
	// +optional
	VirtualNetwork *string `json:"virtualNetwork,omitempty"`
}

// PF defines the PF configuration
// +kubebuilder:validation:XValidation:rule="(!has(oldSelf.virtualNetwork) && !has(self.virtualNetwork)) || ((has(oldSelf.virtualNetwork) && has(self.virtualNetwork)) && self.virtualNetwork==oldSelf.virtualNetwork)", message="virtualNetwork is immutable"
type PF struct {
	// The PF ID
	// +required
	ID int `json:"pfID"`
	// VirtualNetwork is the VirtualNetwork name in the same namespace
	// +optional
	VirtualNetwork *string `json:"virtualNetwork,omitempty"`
}

// OVN defines the configuration for OVN interface type
type OVN struct {
	// ExternalBridge is the name of the OVN bridge
	// +optional
	// +kubebuilder:default="br-ovn"
	ExternalBridge *string `json:"externalBridge,omitempty"`
}

// PatchDef defines the configuration for Patch interface type
type PatchDef struct {
	// PeerBridge is the name of the bridge to which the patch port is connected.
	// This bridge must be created before the ServiceInterface is created.
	// +required
	PeerBridge string `json:"peerBridge"`
	// PeerPatchName is the name of the patch port on the peer bridge.
	// If not set, it is auto-generated in the format: `p_<bridgeA>_to_<bridgeB>_<hash>`
	// where bridge names have hyphens removed and `<hash>` is an 8-character FNV-1a hash
	// derived from the ServiceInterface's namespace/name.
	// Example: p_brovn_to_brsfc_7aea60f7 (for bridges br-ovn and br-sfc).
	// +optional
	PeerPatchName *string `json:"peerPatchName,omitempty"`
	// PeerExternalIDs are the external IDs used to identify the peer patch port.
	// +optional
	PeerExternalIDs map[string]string `json:"peerExternalIDs,omitempty"`
}

// ServiceInterfaceStatus defines the observed state of ServiceInterface
type ServiceInterfaceStatus struct {
	// Conditions reflect the status of the object
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedGeneration records the Generation observed on the object the last time it was patched.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep
// +kubebuilder:printcolumn:name="IfType",type=string,JSONPath=`.spec.interfaceType`
// +kubebuilder:printcolumn:name="IfName",type=string,JSONPath=`.spec.interfaceName`
// +kubebuilder:printcolumn:name="Node",type=string,JSONPath=`.spec.node`
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=`.status.conditions[?(@.type=='Ready')].status`
// +kubebuilder:printcolumn:name="Reason",type="string",JSONPath=`.status.conditions[?(@.type=='Ready')].reason`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ServiceInterface is the Schema for the serviceinterfaces API
type ServiceInterface struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ServiceInterfaceSpec   `json:"spec,omitempty"`
	Status ServiceInterfaceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ServiceInterfaceList contains a list of ServiceInterface
type ServiceInterfaceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ServiceInterface `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ServiceInterface{}, &ServiceInterfaceList{})
}
