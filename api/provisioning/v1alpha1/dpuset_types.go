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
	"github.com/nvidia/doca-platform/pkg/conditions"

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

const (
	NodeEffectDrain        = "Drain"
	NodeEffectCustomAction = "CustomAction"
	NodeEffectHold         = "Hold"
	NodeEffectTaint        = "Taint"
	NodeEffectCustomLabel  = "CustomLabel"
	NodeEffectNoEffect     = "NoEffect"
	NodeEffectUnknown      = "Unknown"
)

const (
	// ConditionDPUSetReconciled is the condition type that indicates that the
	// DPUSet is reconciled.
	ConditionDPUSetReconciled conditions.ConditionType = "DPUSetPrereqsReconciled"
)

var (
	DPUSetConditions = []conditions.ConditionType{
		conditions.TypeReady,
		ConditionDPUSetReconciled,
	}
)

var _ conditions.GetSet = &DPUSet{}

func (c *DPUSet) GetConditions() []metav1.Condition {
	return c.Status.Conditions
}

func (c *DPUSet) SetConditions(conditions []metav1.Condition) {
	c.Status.Conditions = conditions
}

// DPUSetGroupVersionKind is the GroupVersionKind of the DPUSet object
var DPUSetGroupVersionKind = GroupVersion.WithKind(DPUSetKind)

// StrategyType describes strategy to use to reprovision existing DPUs.
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
	// +required
	Type StrategyType `json:"type"`

	// Rolling update config params. Present only if StrategyType = RollingUpdate.
	// +optional
	RollingUpdate *RollingUpdateDPU `json:"rollingUpdate,omitempty"`
}

// RollingUpdateDPU is the rolling update strategy for a DPUSet.
type RollingUpdateDPU struct {
	// MaxUnavailable is the maximum number of DPUs that can be unavailable during the update.
	//
	// Deprecated: This field is deprecated and will be removed with v26.4.0.
	// +optional
	MaxUnavailable *intstr.IntOrString `json:"maxUnavailable,omitempty"`
}

// BlueFieldSoftwareReference is a reference to a specific BlueFieldSoftware
type BlueFieldSoftwareReference struct {
	// Specifies name of the BlueFieldSoftware CR to use for this DPU
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name,omitempty"`
}

// BFBReference is a reference to a specific BFB
type BFBReference struct {
	// Specifies name of the bfb CR to use for this DPU
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name,omitempty"`
}

type ClusterSpec struct {
	// NodeLabels specifies the labels to be added to the node.
	// +optional
	NodeLabels map[string]string `json:"nodeLabels,omitempty"`
	// Selector defines the selector of the DPUClusters the produced DPUs should join
	// +optional
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
}

type DPUTemplateSpec struct {
	// Specifies a BFB CR
	BFB BFBReference `json:"bfb,omitempty"`
	// Specifies a BlueFieldSoftware CR
	// +optional
	BlueFieldSoftware *BlueFieldSoftwareReference `json:"blueFieldSoftware,omitempty"`
	// Specifies how changes to the DPU should affect the Node
	// +required
	NodeEffect NodeEffect `json:"nodeEffect"`
	// Specifies details on the K8S cluster to join
	// +optional
	Cluster *ClusterSpec `json:"cluster,omitempty"`
	// DPUFlavor is the name of the DPUFlavor that will be used to deploy the DPU.
	// +kubebuilder:validation:MinLength=1
	// +required
	DPUFlavor string `json:"dpuFlavor"`
	// AstraEnabled indicates whether E/W NIC configuration (Astra) is enabled
	// +optional
	AstraEnabled *bool `json:"astraEnabled,omitempty"`
	// SecureBoot specifies whether UEFI Secure Boot should be enabled.
	// +optional
	SecureBoot *bool `json:"secureBoot,omitempty"`
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
	Action        `json:",inline"`
	UpgradePolicy `json:",inline"`
}

type Action struct {
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

	// Force is the flag to indicate if the node effect should be applied immediately.
	// If true, dpfOperatorConfig.multiDPUOperationsSyncWaitTime and dpfOperatorConfig.maxUnavailableDPUNodes will be ignored when applying node effect for DPUNodeMaintenance CR
	// +kubebuilder:default=false
	// +optional
	Force *bool `json:"force,omitempty"`
}

// UpgradePolicy is the policy for the upgrade of the DPUSet.
type UpgradePolicy struct {
	// Apply node effect when labels change on the DPU object
	// When set to true, label changes in Ready state will trigger node effect logic
	// +optional
	// +kubebuilder:default=false
	ApplyOnLabelChange *bool `json:"applyOnLabelChange,omitempty"`
	// Additional requestors to be added to the NvidiaNodeMaintenance CR when Drain is selected
	// +optional
	NodeMaintenanceAdditionalRequestors []string `json:"nodeMaintenanceAdditionalRequestors,omitempty"`
}

func (n *NodeEffect) String() string {
	if n.IsTaint() {
		return NodeEffectTaint
	}
	if n.IsNoEffect() {
		return NodeEffectNoEffect
	}
	if n.IsCustomLabel() {
		return NodeEffectCustomLabel
	}
	if n.IsDrain() {
		return NodeEffectDrain
	}
	if n.IsCustomAction() {
		return NodeEffectCustomAction
	}
	if n.IsHold() {
		return NodeEffectHold
	}
	return NodeEffectUnknown
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

// +kubebuilder:validation:XValidation:rule="!(has(self.dpuSelector) && has(self.dpuDeviceSelector))", message="only one of dpuSelector or dpuDeviceSelector can be specified"

// DPUSetSpec defines the desired state of DPUSet
type DPUSetSpec struct {
	// The rolling update strategy to use to updating existing DPUs with new ones.
	// +required
	Strategy DPUSetStrategy `json:"strategy"`

	// Select the DPUNodes with specific labels
	// +optional
	DPUNodeSelector *metav1.LabelSelector `json:"dpuNodeSelector,omitempty"`

	// Select the DPU with specific labels
	//
	// Deprecated: This field is deprecated and will be removed with v26.7.0. Use DPUDeviceSelector instead.
	// +optional
	DPUSelector map[string]string `json:"dpuSelector,omitempty"`

	// DPUDeviceSelector defines the selector for DPUDevices that the DPUSet should target and should create a DPU for.
	// +optional
	DPUDeviceSelector *metav1.LabelSelector `json:"dpuDeviceSelector,omitempty"`

	// Object that describes the DPU that will be created if insufficient replicas are detected
	// +required
	DPUTemplate DPUTemplate `json:"dpuTemplate"`
}

// DPUSetStatus defines the observed state of DPUSet
type DPUSetStatus struct {
	// DPUStatistics is a map of DPUPhase to the number of DPUs in that phase.
	// +optional
	DPUStatistics map[DPUPhase]int `json:"dpuStatistics,omitempty"`
	// Conditions reflect the status of the object
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedGeneration records the Generation observed on the object the last time it was patched.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=`.status.conditions[?(@.type=='Ready')].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:validation:XValidation:rule="self.metadata.name.size() <= 63", message="name length can't be bigger than 63 chars"

// DPUSet is the Schema for the dpusets API
type DPUSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DPUSetSpec   `json:"spec,omitempty"`
	Status DPUSetStatus `json:"status,omitempty"`
}

// IsAstraEnabledForNonBlueField4 returns true if Astra is enabled on this DPUSet
// and the target DPUDevice is not a BlueField4.
func (c *DPUSet) IsAstraEnabledForNonBlueField4(dpuDevice DPUDevice) bool {
	return c.Spec.DPUTemplate.Spec.AstraEnabled != nil &&
		*c.Spec.DPUTemplate.Spec.AstraEnabled &&
		dpuDevice.Status.DPUType != DPUTypeBlueField4
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
