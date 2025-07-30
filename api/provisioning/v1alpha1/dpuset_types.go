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
	// DPUSetListKind is the kind of the DPUSetList object
	DPUSetListKind = "DPUSetList"
)

// DPUSetGroupVersionKind is the GroupVersionKind of the DPUSet object
var DPUSetGroupVersionKind = GroupVersion.WithKind(DPUSetKind)

// StrategyType describes strategy to use to reprovision existing DPUs.
// Default is "OnDelete".
// +kubebuilder:validation:Enum=OnDelete;RollingUpdate
type StrategyType string

const (
	DPUSetFinalizer = "provisioning.dpu.nvidia.com/dpuset-protection"

	// New DPU CR will only be created when you manually delete old DPU CR.
	OnDeleteStrategyType StrategyType = "OnDelete"

	// Gradually scale down the old DPUs and scale up the new one.
	RollingUpdateStrategyType StrategyType = "RollingUpdate"
)

type DPUSetStrategy struct {
	// Can be "OnDelete" or "RollingUpdate".
	// +kubebuilder:default=OnDelete
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
	// +kubebuilder:default={drain: true}
	// +optional
	NodeEffect *NodeEffect `json:"nodeEffect,omitempty"`
	// Specifies details on the K8S cluster to join
	// +optional
	Cluster *ClusterSpec `json:"cluster,omitempty"`
	// DPUFlavor is the name of the DPUFlavor that will be used to deploy the DPU.
	// +optional
	DPUFlavor string `json:"dpuFlavor"`
}

// DPUTemplate is a template for DPU
type DPUTemplate struct {
	// Annotations specifies annotations which are added to the DPU.
	Annotations map[string]string `json:"annotations,omitempty"`
	// Spec specifies the DPU specification.
	Spec DPUTemplateSpec `json:"spec,omitempty"`
}

// NodeEffect is the effect the DPU has on Nodes during provisioning.
// Only one of Taint, NoEffect, CustomLabel, Drain, CustomAction, Hold can be set.
// +kubebuilder:validation:XValidation:rule="(has(self.taint) ? 1 : 0) + (has(self.noEffect) ? 1 : 0) + (has(self.customLabel) ? 1 : 0) + (has(self.drain) ? 1 : 0) + (has(self.customAction) ? 1 : 0) + (has(self.hold) ? 1 : 0) == 1", message="only one of taint, noEffect, drain, customLabel, customAction, hold can be set"
type NodeEffect struct {
	// Add specify taint on the DPU node
	// +optional
	Taint *corev1.Taint `json:"taint,omitempty"`
	// Do not do any action on the DPU node
	// +optional
	NoEffect *bool `json:"noEffect,omitempty"`
	// Add specify labels on the DPU node
	// +optional
	CustomLabel map[string]string `json:"customLabel,omitempty"`
	// Drain the K8s host node by NodeMaintenance operator
	// +optional
	Drain *bool `json:"drain,omitempty"`
	// Name of a config map which contains a pod yaml definition to run which will apply the nodeEffect.
	// The pod is expected to exit when node effect is done, if pod terminates with error then DPU would move to an error phase.
	// The DPUNode's name will be exported as an environment variable, named as DPUNODE_NAME, to each container and init container in the pod.
	// The labels and annotations of DPUNode will be exported in `/etc/dpu/dpf-pod-info/labels` and `/etc/dpu/dpf-pod-info/annotations` accordingly; the volume name `dpf-pod-info` is used to mount the labels and annotations.
	// If any name confliction for env or volume, the controller will not export the name or labels/annotations of DPUNode accordingly.
	// +optional
	CustomAction *string `json:"customAction,omitempty"`
	// Places annotation `wait-for-external-nodeeffect` and waits for it to be removed
	// this is the default behavior in a non K8S environment
	// +optional
	Hold *bool `json:"hold,omitempty"`
}

func (n *NodeEffect) String() string {
	if n.IsTaint() {
		return "Taint"
	}
	if n.IsNoEffect() {
		return "NoEffect"
	}
	if n.IsCustomLabel() {
		return "CustomLabel"
	}
	if n.IsDrain() {
		return "Drain"
	}
	if n.IsCustomAction() {
		return "CustomAction"
	}
	if n.IsHold() {
		return "Hold"
	}
	return "Unknown"
}

func (n *NodeEffect) IsHold() bool {
	if n.Hold == nil {
		return false
	}
	return *n.Hold
}

func (n *NodeEffect) IsCustomAction() bool {
	return n.CustomAction != nil && len(*n.CustomAction) > 0
}

func (n *NodeEffect) IsCustomLabel() bool {
	return len(n.CustomLabel) != 0
}

func (n *NodeEffect) IsTaint() bool {
	return n.Taint != nil
}

func (n *NodeEffect) IsDrain() bool {
	if n.Drain == nil {
		return false
	}
	return *n.Drain
}

func (n *NodeEffect) IsNoEffect() bool {
	if n.NoEffect == nil {
		return false
	}
	return *n.NoEffect
}

// DPUSetSpec defines the desired state of DPUSet
type DPUSetSpec struct {
	// The rolling update strategy to use to updating existing DPUs with new ones.
	// +optional
	Strategy *DPUSetStrategy `json:"strategy,omitempty"`

	// Select the DPUNodes with specific labels
	// +optional
	DPUNodeSelector *metav1.LabelSelector `json:"dpuNodeSelector,omitempty"`

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
// +kubebuilder:validation:XValidation:rule="self.metadata.name.size() <= 63", message="name length can't be bigger than 63 chars"

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
