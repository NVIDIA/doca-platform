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
	"fmt"

	"github.com/nvidia/doca-platform/pkg/conditions"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// DPUNodeKind is the kind of the DPUNode object
	DPUNodeKind = "DPUNode"

	DPUNodeFinalizer = "provisioning.dpu.nvidia.com/dpunode-protection"
)

// DPUNodeGroupVersionKind is the GroupVersionKind of the DPUNode object
var DPUNodeGroupVersionKind = GroupVersion.WithKind(DPUNodeKind)

type DPUNodeInstallInterfaceType string

// List of valid Install Interface types
const (
	DPUNodeInstallInterfaceGNOI      DPUNodeInstallInterfaceType = "gNOI"
	DPUNodeInstallInterfaceHostAgent DPUNodeInstallInterfaceType = "hostAgent"
	DPUNodeInstallIntrefaceRedfish   DPUNodeInstallInterfaceType = "redfish"
)

type DPUNodeConditionType string

// List of valid condition types
const (
	// DPUNodeConditionReady means the DPU is ready.
	DPUNodeConditionReady DPUNodeConditionType = "Ready"
	// DPUNodeConditionInvalidDPUDetails means the DPU details provided are invalid.
	DPUNodeConditionInvalidDPUDetails DPUNodeConditionType = "InvalidDPUDetails"
	// DPUNodeConditionRebootInProgress means the DPUNode is in the process of rebooting.
	DPUNodeConditionRebootInProgress DPUNodeConditionType = "DPUNodeRebootInProgress"
	// DPUNodeConditionDPUUpdateInProgress means the DPU is in the process of being updated.
	DPUNodeConditionDPUUpdateInProgress DPUNodeConditionType = "DPUUpdateInProgress"
	// DPUNodeConditionNeedDMSUpgrade means the DMS needs to be upgraded.
	DPUNodeConditionNeedHostAgentUpgrade DPUNodeConditionType = "NeedHostAgentUpgrade"
	// DPUNodeConditionBridgeConfigured means the bridge br-dpu is configured.
	DPUNodeConditionBridgeConfigured DPUNodeConditionType = "OOBBridgeConfigured"
	// DPUNodeConditionRshimAvailable means the rshim is available.
	DPUNodeConditionRshimAvailable DPUNodeConditionType = "RshimAvailable"
	// DPUNodeConditionNodeEffectInProgress means the node effect is processing on the node.
	DPUNodeConditionNodeEffectInProgress DPUNodeConditionType = "DPUNodeNodeEffectInProgress"
)

const (
	DPUNodeExternalRebootRequiredAnnotation = "provisioning.dpu.nvidia.com/dpunode-external-reboot-required"
	// DPUNodeScriptConfigMapVersionAnnotation stores the ResourceVersion of the ConfigMap
	// used to create the last script reboot job. Used to detect ConfigMap changes for auto-retry.
	DPUNodeScriptConfigMapVersionAnnotation = "provisioning.dpu.nvidia.com/dpunode-script-configmap-version"
	// DPUNodeNameLabel is the label added to the DPU Kubernetes Node that indicates the name of
	// the DPUNode that this DPU belongs to.
	DPUNodeNameLabel = "provisioning.dpu.nvidia.com/dpunode-name"
	// DPUNodeNamespaceLabel is the label added to the DPU Kubernetes Node that indicates
	// the namespace of the DPUNode that this DPU belongs to.
	DPUNodeNamespaceLabel = "provisioning.dpu.nvidia.com/dpunode-namespace"
)

func (ct DPUNodeConditionType) String() string {
	return string(ct)
}

var _ conditions.GetSet = &DPUNode{}

func (c *DPUNode) GetConditions() []metav1.Condition {
	return c.Status.Conditions
}

func (c *DPUNode) SetConditions(conditions []metav1.Condition) {
	c.Status.Conditions = conditions
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep
// TODO: Add e2e test when we add scenarios that include creating our own DPUNode and DPUDevice objects
// +kubebuilder:validation:XValidation:rule="self.metadata.name.size() <= 48", message="name length can't be bigger than 48 chars"

// DPUNode is the Schema for the dpunodes API
type DPUNode struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DPUNodeSpec   `json:"spec,omitempty"`
	Status DPUNodeStatus `json:"status,omitempty"`
}

type GNOI struct{}

type HostAgent struct{}

type External struct{}

type Script struct {
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`
}

// NodeRebootMethod defines the desired reboot method
// +kubebuilder:validation:XValidation:rule="(((has(self.hostAgent) || has(self.gNOI)) && !has(self.external) && !has(self.script)) || (has(self.external) && !has(self.hostAgent) && !has(self.gNOI) && !has(self.script)) || (has(self.script) && !has(self.external) && !has(self.hostAgent) && !has(self.gNOI)))", message="only one of hostAgent, external, script can be set"
type NodeRebootMethod struct {
	// Use the DPU's DMS interface to reboot the host.
	//
	// Deprecated: Use HostAgent instead.
	// +optional
	GNOI *GNOI `json:"gNOI,omitempty"`
	// Use the HostAgent to reboot the host.
	// +optional
	HostAgent *HostAgent `json:"hostAgent,omitempty"`
	// Reboot the host via an external means, not controlled by the DPU controller.
	// +optional
	External *External `json:"external,omitempty"`
	// Reboot the host by executing a custom script. This field defined which ConfigMap store the custom script.
	// The ConfigMap should include a pod template of Job object under the `pod-template` key.
	// That pod template will be put in a Job object to be executed.
	// +optional
	Script *Script `json:"script,omitempty"`
}

type DPURef struct {
	// Name of the DPU device.
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`
}

// DPUNodeSpec defines the desired state of DPUNode
type DPUNodeSpec struct {
	// Defines the method for rebooting the host.
	// One of the following options can be chosen for this field:
	//    - "external": Reboot the host via an external means, not controlled by the
	//      DPU controller.
	//    - "script": Reboot the host by executing a custom script.
	//    - "hostAgent": Use the host agent to reboot the host.
	// "hostAgent" is the default value.
	// +kubebuilder:default={hostAgent:{}}
	// +optional
	NodeRebootMethod *NodeRebootMethod `json:"nodeRebootMethod,omitempty"`

	// The IP address and port where the DMS is exposed. Only applicable if dpuInstallInterface is set to gNOI.
	//
	// Deprecated: this field is no longer used.
	// +optional
	NodeDMSAddress *DMSAddress `json:"nodeDMSAddress,omitempty"`

	// A map containing names of each DPUDevice attached to the node.
	// +optional
	DPUs []DPURef `json:"dpus,omitempty"`
}

// DMSAddress represents the IP and Port configuration for DMS.
type DMSAddress struct {
	// IP address in IPv4 format.
	// +kubebuilder:validation:Format=ipv4
	IP string `json:"ip"`

	// Port number.
	// +kubebuilder:validation:Minimum=1
	Port uint16 `json:"port"`
}

func (d *DMSAddress) String() string {
	if d == nil {
		return ""
	}
	return fmt.Sprintf("%s:%d", d.IP, d.Port)
}

// DPUNodeStatus defines the observed state of DPUNode
type DPUNodeStatus struct {
	// Conditions represent the latest available observations of an object's state.
	// +kubebuilder:validation:Type=array
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// The name of the interface which will be used to install the bfb image, can be one of hostAgent,redfish
	// +kubebuilder:validation:Enum=gNOI;hostAgent;redfish
	// +optional
	DPUInstallInterface *string `json:"dpuInstallInterface,omitempty"`
	// The name of the Kubernetes Node object that this DPUNode represents.
	// This field is optional and only relevant if the x86 host is part of the DPF Kubernetes cluster.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="KubeNodeRef is immutable"
	// +optional
	KubeNodeRef *string `json:"kubeNodeRef,omitempty"`
	// RebootInProgress indicates if the node is in the process of rebooting.
	// +optional
	RebootInProgress *bool `json:"rebootInProgress,omitempty"`
}

// +kubebuilder:object:root=true

// DPUNodeList contains a list of DPUNode
type DPUNodeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DPUNode `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DPUNode{}, &DPUNodeList{})
}
