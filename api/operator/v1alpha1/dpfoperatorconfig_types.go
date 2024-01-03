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
	"github.com/nvidia/doca-platform/internal/conditions"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ImagePullSecretsReconciledCondition conditions.ConditionType = "ImagePullSecretsReconciled"
	SystemComponentsReconciledCondition conditions.ConditionType = "SystemComponentsReconciled"
	SystemComponentsReadyCondition      conditions.ConditionType = "SystemComponentsReady"
)

var (
	Conditions = []conditions.ConditionType{
		conditions.TypeReady,
		ImagePullSecretsReconciledCondition,
		SystemComponentsReconciledCondition,
		SystemComponentsReadyCondition,
	}
)

var (
	DPFOperatorConfigFinalizer = "dpu.nvidia.com/dpfoperatorconfig"
	// DPFComponentLabelKey is added on all objects created by the DPF Operator.
	DPFComponentLabelKey = "dpu.nvidia.com/component"
)

// Overrides exposes a set of fields which impact the recommended behavior of the DPF Operator.
type Overrides struct {
	// Paused disables all reconciliation of the DPFOperatorConfig when set to true.
	// +optional
	Paused *bool `json:"paused,omitempty"`
}

// Networking defines the networking configuration for the system components.
type Networking struct {
	// ControlPlaneMTU is the MTU value to be set on the management network.
	// The default is 1500.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=9216
	// +kubebuilder:default=1500
	// +optional
	ControlPlaneMTU *int `json:"controlPlaneMTU,omitempty"`
}

// DPFOperatorConfigSpec defines the desired state of DPFOperatorConfig
type DPFOperatorConfigSpec struct {
	// +optional
	Overrides *Overrides `json:"overrides,omitempty"`

	// +kubebuilder:default={controlPlaneMTU: 1500}
	// +optional
	Networking *Networking `json:"networking,omitempty"`

	// List of secret names which are used to pull images for DPF system components and DPUServices.
	// These secrets must be in the same namespace as the DPF Operator Config and should be created before the config is created.
	// System reconciliation will not proceed until these secrets are available.
	// +optional
	ImagePullSecrets []string `json:"imagePullSecrets,omitempty"`

	// DPUServiceController is the configuration for the DPUServiceController
	// +optional
	DPUServiceController *DPUServiceControllerConfiguration `json:"dpuServiceController,omitempty"`
	// ProvisioningController is the configuration for the ProvisioningController
	ProvisioningController ProvisioningControllerConfiguration `json:"provisioningController"`
	// ServiceSetController is the configuration for the ServiceSetController
	// +optional
	ServiceSetController *ServiceSetControllerConfiguration `json:"serviceSetController,omitempty"`
	// DPUDetector is the configuration for the DPUDetector.
	// +optional
	DPUDetector *DPUDetectorConfiguration `json:"dpuDetector,omitempty"`
	// Multus is the configuration for Multus
	// +optional
	Multus *MultusConfiguration `json:"multus,omitempty"`
	// SRIOVDevicePlugin is the configuration for the SRIOVDevicePlugin
	// +optional
	SRIOVDevicePlugin *SRIOVDevicePluginConfiguration `json:"sriovDevicePlugin,omitempty"`
	// Flannel is the configuration for Flannel
	// +optional
	Flannel *FlannelConfiguration `json:"flannel,omitempty"`
	// OVSCNI is the configuration for OVSCNI
	// +optional
	OVSCNI *OVSCNIConfiguration `json:"ovsCNI,omitempty"`
	// NVIPAM is the configuration for NVIPAM
	// +optional
	NVIPAM *NVIPAMConfiguration `json:"nvipam,omitempty"`
	// SFCController is the configuration for the SFCController
	// +optional
	SFCController *SFCControllerConfiguration `json:"sfcController,omitempty"`
	// KamajiClusterManager is the configuration for the kamaji-cluster-manager
	// +optional
	KamajiClusterManager *KamajiClusterManagerConfiguration `json:"kamajiClusterManager,omitempty"`
	// StaticClusterManager is the configuration for the static-cluster-manager
	// +optional
	StaticClusterManager *StaticClusterManagerConfiguration `json:"staticClusterManager,omitempty"`
	// OVSHelper is the configuration for the OVSHelper
	// +optional
	OVSHelper *OVSHelperConfiguration `json:"ovsHelper,omitempty"`
}

// DPFOperatorConfigStatus defines the observed state of DPFOperatorConfig
type DPFOperatorConfigStatus struct {
	// Conditions exposes the current state of the OperatorConfig.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedGeneration records the Generation observed on the object the last time it was patched.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:annotations=helm.sh/resource-policy=keep
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=`.status.conditions[?(@.type=='Ready')].status`
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=`.status.conditions[?(@.type=='Ready')].reason`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// DPFOperatorConfig is the Schema for the dpfoperatorconfigs API
type DPFOperatorConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DPFOperatorConfigSpec   `json:"spec,omitempty"`
	Status DPFOperatorConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DPFOperatorConfigList contains a list of DPFOperatorConfig
type DPFOperatorConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DPFOperatorConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DPFOperatorConfig{}, &DPFOperatorConfigList{})
}

func (c *DPFOperatorConfig) SetConditions(conditions []metav1.Condition) {
	c.Status.Conditions = conditions
}
func (c *DPFOperatorConfig) GetConditions() []metav1.Condition {
	return c.Status.Conditions
}
