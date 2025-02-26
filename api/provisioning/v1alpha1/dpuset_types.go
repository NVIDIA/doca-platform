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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	// DPUSetKind is the kind of the DPUSet object
	DPUSetKind = "DPUSet"
)

// DPUSetGroupVersionKind is the GroupVersionKind of the DPUSet object
var DPUSetGroupVersionKind = GroupVersion.WithKind(DPUSetKind)

// StrategyType describes strategy to use to reprovision existing DPUs.
// Default is "Recreate".
// +kubebuilder:validation:Enum=Recreate;RollingUpdate
type StrategyType string

const (
	DPUSetFinalizer = "provisioning.dpu.nvidia.com/dpuset-protection"

	// Delete all the existing DPUs before creating new ones.
	RecreateStrategyType StrategyType = "Recreate"

	// Gradually scale down the old DPUs and scale up the new one.
	RollingUpdateStrategyType StrategyType = "RollingUpdate"
)

type DPUSetStrategy struct {
	// Can be "Recreate" or "RollingUpdate".
	// +kubebuilder:default=Recreate
	// +optional
	Type StrategyType `json:"type,omitempty"`

	// Rolling update config params. Present only if StrategyType = RollingUpdate.
	// +optional
	RollingUpdate *RollingUpdateDPU `json:"rollingUpdate,omitempty"`
}

// RollingUpdateDPU is the rolling update strategy for a DPUSet.
type RollingUpdateDPU struct {
	// MaxUnavailable is the maximum number of DPUs that can be unavailable during the update.
	// +optional
	MaxUnavailable *intstr.IntOrString `json:"maxUnavailable,omitempty"`
}

// BFBReference is a reference to a specific BFB
type BFBReference struct {
	// Specifies name of the bfb CR to use for this DPU
	Name string `json:"name,omitempty"`
}

type ClusterSpec struct {
	// NodeLabels specifies the labels to be added to the node.
	// +optional
	NodeLabels map[string]string `json:"nodeLabels,omitempty"`
}

type DPUTemplateSpec struct {
	// Specifies a BFB CR
	BFB BFBReference `json:"bfb,omitempty"`
	// Specifies how changes to the DPU should affect the Node
	// +kubebuilder:default={drain: {automaticNodeReboot: true}}
	// +optional
	NodeEffect *NodeEffect `json:"nodeEffect,omitempty"`
	// Specifies details on the K8S cluster to join
	// +optional
	Cluster *ClusterSpec `json:"cluster,omitempty"`
	// DPUFlavor is the name of the DPUFlavor that will be used to deploy the DPU.
	// +optional
	DPUFlavor string `json:"dpuFlavor"`
	// Specifies if the DPU controller should automatically reboot the node on upgrades,
	// this field is intended for advanced cases that don’t use draining but want to reboot the host based with custom logic
	// +kubebuilder:default=true
	// +optional
	AutomaticNodeReboot *bool `json:"automaticNodeReboot,omitempty"`
}

// DPUTemplate is a template for DPU
type DPUTemplate struct {
	// Annotations specifies annotations which are added to the DPU.
	Annotations map[string]string `json:"annotations,omitempty"`
	// Spec specifies the DPU specification.
	Spec DPUTemplateSpec `json:"spec,omitempty"`
}

type NodeEffect struct {
	// Add specify taint on the DPU node
	// +optional
	Taint *corev1.Taint `json:"taint,omitempty"`
	// Do not do any action on the DPU node
	// +optional
	NoEffect bool `json:"noEffect,omitempty"`
	// Add specify labels on the DPU node
	// +optional
	CustomLabel map[string]string `json:"customLabel,omitempty"`
	// Drain the K8s host node by NodeMaintenance operator
	// +optional
	Drain *Drain `json:"drain,omitempty"`
}

// Drain the K8s host node by NodeMaintenance operator
type Drain struct {
	// Specifies if the DPU controller should automatically reboot the node on upgrades
	// +kubebuilder:default=true
	// +optional
	AutomaticNodeReboot bool `json:"automaticNodeReboot,omitempty"`
}

// DPUSetSpec defines the desired state of DPUSet
type DPUSetSpec struct {
	// The rolling update strategy to use to updating existing DPUs with new ones.
	// +optional
	Strategy *DPUSetStrategy `json:"strategy,omitempty"`

	// Select the Nodes with specific labels
	// +optional
	NodeSelector *metav1.LabelSelector `json:"nodeSelector,omitempty"`

	// Select the DPU with specific labels
	// +optional
	DPUSelector map[string]string `json:"dpuSelector,omitempty"`

	// Object that describes the DPU that will be created if insufficient replicas are detected
	// +optional
	DPUTemplate DPUTemplate `json:"dpuTemplate,omitempty"`
}

// DPUSetStatus defines the observed state of DPUSet
type DPUSetStatus struct {
	// DPUStatistics is a map of DPUPhase to the number of DPUs in that phase.
	// +optional
	DPUStatistics map[DPUPhase]int `json:"dpuStatistics,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep

// DPUSet is the Schema for the dpusets API
type DPUSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DPUSetSpec   `json:"spec,omitempty"`
	Status DPUSetStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DPUSetList contains a list of DPUSet
type DPUSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DPUSet `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DPUSet{}, &DPUSetList{})
}
