/*
COPYRIGHT 2024 NVIDIA

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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	ServiceInterfaceKind = "ServiceChain"

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
	// InterfaceTypeService is the service interface type
	InterfaceTypeService = "service"
)

var ServiceInterfaceGroupVersionKind = GroupVersion.WithKind(ServiceChainKind)

// ServiceInterfaceSpec defines the desired state of ServiceInterface
// +kubebuilder:validation:XValidation:rule="(self.interfaceType == 'vlan' && has(self.vlan)) || (self.interfaceType == 'pf' && has(self.pf)) || (self.interfaceType == 'vf' && has(self.vf)) || (self.interfaceType == 'physical' && has(self.physical)) || (self.interfaceType == 'service' && has(self.service)) || (self.interfaceType == 'ovn')", message="`for interfaceType=vlan, vlan must be set; for interfaceType=pf, pf must be set; for interfaceType=vf, vf must be set; for interfaceType=physical, physical must be set; for interfaceType=service, service must be set`"
type ServiceInterfaceSpec struct {
	// Node where this interface exists
	// +optional
	Node *string `json:"node,omitempty"`
	// The interface type ("vlan", "physical", "pf", "vf", "ovn", "service")
	// +kubebuilder:validation:Enum={"vlan", "physical", "pf", "vf", "ovn", "service"}
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
}

// Physical Identifies a physical interface
type Physical struct {
	// The interface name
	// +required
	InterfaceName string `json:"interfaceName"`
}

// ServiceDef Identifes the service and network for the ServiceInterface
type ServiceDef struct {
	// ServiceID is the DPU Service Identifier
	// +required
	ServiceID string `json:"serviceID"`
	// Network is the Network Attachment Definition in the form of "namespace/name"
	// or just "name" if the namespace is the same as the ServiceInterface.
	// +required
	Network string `json:"network"`
	// The interface name
	// +required
	InterfaceName string `json:"interfaceName"`
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
type VF struct {
	// The VF ID
	// +required
	VFID int `json:"vfID"`
	// The PF ID
	// +required
	PFID int `json:"pfID"`
	// The parent interface reference
	// TODO: Figure out what this field is supposed to be
	// +required
	ParentInterfaceRef string `json:"parentInterfaceRef"`
}

// PF defines the PF configuration
type PF struct {
	// The PF ID
	// +required
	ID int `json:"pfID"`
}

// ServiceInterfaceStatus defines the observed state of ServiceInterface
type ServiceInterfaceStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep
// +kubebuilder:printcolumn:name="IfType",type=string,JSONPath=`.spec.interfaceType`
// +kubebuilder:printcolumn:name="IfName",type=string,JSONPath=`.spec.interfaceName`
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
