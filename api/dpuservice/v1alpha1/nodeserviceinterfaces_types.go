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
	"strings"

	"github.com/nvidia/doca-platform/pkg/conditions"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	NodeServiceInterfacesKind = "NodeServiceInterfaces"

	// NSITypeSFC is the NSI type for SFC-backed service interfaces.
	NSITypeSFC = "sfc"
	// NSITypeVPC is the NSI type prefix for VPC-backed service interfaces.
	// The full type may be "vpc" or "vpc-<provisioner>".
	NSITypeVPC = "vpc"

	// NodeServiceInterfacesReconciled indicates that all interface entries
	// have been processed by the managing controller.
	NodeServiceInterfacesReconciled conditions.ConditionType = "NodeServiceInterfacesReconciled"

	// ResourceReleased indicates all required reconcilers have successfully
	// released resources for a terminating entry.
	ResourceReleased conditions.ConditionType = "ResourceReleased"

	// NodeServiceInterfacesFinalizer is set on a NodeServiceInterfaces when it is
	// first handled by the reconciler, and removed when this object is deleted.
	NodeServiceInterfacesFinalizer = "svc.dpu.nvidia.com/nodeserviceinterfaces"
)

var (
	NodeServiceInterfacesGroupVersionKind = GroupVersion.WithKind(NodeServiceInterfacesKind)

	// NodeServiceInterfacesConditions are the conditions set by the reconciler.
	NodeServiceInterfacesConditions = []conditions.ConditionType{
		conditions.TypeReady,
		NodeServiceInterfacesReconciled,
	}
)

var _ conditions.GetSet = &NodeServiceInterfaces{}

func (c *NodeServiceInterfaces) GetConditions() []metav1.Condition {
	return c.Status.Conditions
}

func (c *NodeServiceInterfaces) SetConditions(conditions []metav1.Condition) {
	c.Status.Conditions = conditions
}

// interfaceEntryStatusGetSet wraps an InterfaceEntryStatus and the parent NSI's metadata
// generation, implementing conditions.GetSet so pkg/conditions helpers can be used
// for per-entry condition checks.
type interfaceEntryStatusGetSet struct {
	status     *InterfaceEntryStatus
	generation int64
}

func (a *interfaceEntryStatusGetSet) GetConditions() []metav1.Condition  { return a.status.Conditions }
func (a *interfaceEntryStatusGetSet) SetConditions(c []metav1.Condition) { a.status.Conditions = c }
func (a *interfaceEntryStatusGetSet) GetGeneration() int64               { return a.generation }

// GetEntryStatus returns a conditions.GetSet view of the named entry's status,
// using the NSI object's metadata generation for staleness detection.
// Returns nil if the entry is not found.
func (c *NodeServiceInterfaces) GetEntryStatus(entryName string) conditions.GetSet {
	for i := range c.Status.InterfaceStatuses {
		if c.Status.InterfaceStatuses[i].Name == entryName {
			return &interfaceEntryStatusGetSet{
				status:     &c.Status.InterfaceStatuses[i],
				generation: c.GetGeneration(),
			}
		}
	}
	return nil
}

// NodeServiceInterfacesSpec defines the desired state of NodeServiceInterfaces.
// +kubebuilder:validation:XValidation:rule="self.node == oldSelf.node",message="node is immutable"
// +kubebuilder:validation:XValidation:rule="self.type == oldSelf.type",message="type is immutable"
type NodeServiceInterfacesSpec struct {
	// Node is the name of the DPU node this object represents.
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:MinLength=1
	// +required
	Node string `json:"node"`
	// Type identifies which controller domain owns this NSI shard.
	// Examples: "sfc", "vpc-my-provisioner".
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:MinLength=1
	// +required
	Type string `json:"type"`
	// Interfaces is the list of service interface entries for this node.
	// +kubebuilder:validation:MaxItems=256
	// +listType=map
	// +listMapKey=name
	// +optional
	Interfaces []InterfaceEntry `json:"interfaces,omitempty"`
}

// InterfaceEntry defines a single service interface entry within a NodeServiceInterfaces object.
// +kubebuilder:validation:XValidation:rule="(self.interfaceType == 'vlan' && has(self.vlan)) || (self.interfaceType == 'pf' && has(self.pf)) || (self.interfaceType == 'vf' && has(self.vf)) || (self.interfaceType == 'physical' && has(self.physical)) || (self.interfaceType == 'service' && has(self.service)) || (self.interfaceType == 'ovn') || (self.interfaceType == 'patch' && has(self.patch))",messageExpression="'for interfaceType=' + self.interfaceType + ', ' + self.interfaceType + ' must be set'"
type InterfaceEntry struct {
	// Name uniquely identifies this entry within the NodeServiceInterfaces object.
	// Format: `<namespace>_<service-interface-set-name>`. The underscore separator is collision-free
	// because Kubernetes namespace and resource names follow DNS subdomain rules
	// and can never contain an underscore. Both components are
	// bounded to 63 chars, so the full name is at most 127 chars.
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`
	// Terminating indicates this entry is marked for removal. Cleanup may
	// require one or more reconcilers. ResourceReleased=True is set only
	// after all required cleanup conditions are satisfied in status.
	// +kubebuilder:validation:Default=false
	Terminating bool `json:"terminating,omitempty"`
	// Labels used for ServiceChain matchLabels resolution.
	// +kubebuilder:validation:MaxProperties=50
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// Annotations carry operational metadata for this entry.
	// +kubebuilder:validation:MaxProperties=50
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// InterfaceType is the type of the interface.
	// +kubebuilder:validation:Enum={"vlan", "physical", "pf", "vf", "ovn", "patch", "service"}
	// +required
	InterfaceType string `json:"interfaceType"`
	// Physical is the physical interface definition.
	// +optional
	Physical *Physical `json:"physical,omitempty"`
	// Vlan is the VLAN definition.
	// +optional
	Vlan *VLAN `json:"vlan,omitempty"`
	// VF is the VF definition.
	// +optional
	VF *VF `json:"vf,omitempty"`
	// PF is the PF definition.
	// +optional
	PF *PF `json:"pf,omitempty"`
	// Service is the service definition.
	// +optional
	Service *ServiceDef `json:"service,omitempty"`
	// OVN is the OVN definition.
	// +optional
	OVN *OVN `json:"ovn,omitempty"`
	// Patch is the patch definition.
	// +optional
	Patch *PatchDef `json:"patch,omitempty"`
}

// GetNamespacedName returns the namespace and name of the ServiceInterfaceSet
// that owns this entry, decoded from the entry Name field. Both components are
// guaranteed non-empty for any entry produced by InterfaceEntryName; the guard
// handles programmer errors only.
func (i *InterfaceEntry) GetNamespacedName() (namespace, name string) {
	parts := strings.SplitN(i.Name, "_", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

// NodeServiceInterfacesStatus defines the observed state of NodeServiceInterfaces.
type NodeServiceInterfacesStatus struct {
	// Conditions reflect aggregate status.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedGeneration is the last observed generation of the NodeServiceInterfaces object.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// InterfaceStatuses tracks per-entry reconciliation state. Written by
	// the managing controller. The params map carries controller-specific
	// release information needed during terminating/release handling.
	// +listType=map
	// +listMapKey=name
	// +optional
	InterfaceStatuses []InterfaceEntryStatus `json:"interfaceStatuses,omitempty"`
}

// InterfaceEntryStatus records reconciliation state for a single interface entry.
type InterfaceEntryStatus struct {
	// Name matches the spec entry name.
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`
	// Params carries controller-specific information for handling reconcile/release of the matching InterfaceEntry in spec.
	// Multiple reconcilers may write distinct keys safely as long as they do not share keys.
	// +optional
	Params map[string]string `json:"params,omitempty"`
	// Conditions may be written by multiple reconcilers for the same entry.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep
// +kubebuilder:printcolumn:name="Node",type=string,JSONPath=`.spec.node`
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=`.status.conditions[?(@.type=='Ready')].status`
// +kubebuilder:printcolumn:name="Reason",type="string",JSONPath=`.status.conditions[?(@.type=='Ready')].reason`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// NodeServiceInterfaces is the Schema for the nodeserviceinterfaces API.
type NodeServiceInterfaces struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NodeServiceInterfacesSpec   `json:"spec,omitempty"`
	Status NodeServiceInterfacesStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// NodeServiceInterfacesList contains a list of NodeServiceInterfaces
type NodeServiceInterfacesList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NodeServiceInterfaces `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NodeServiceInterfaces{}, &NodeServiceInterfacesList{})
}
