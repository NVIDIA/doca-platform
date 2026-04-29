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
	"github.com/nvidia/doca-platform/internal/release"
	"github.com/nvidia/doca-platform/pkg/conditions"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	PreUpgradeValidationReadyCondition  conditions.ConditionType = "PreUpgradeValidationReady"
	ImagePullSecretsReconciledCondition conditions.ConditionType = "ImagePullSecretsReconciled"
	SystemComponentsReconciledCondition conditions.ConditionType = "SystemComponentsReconciled"
	SystemComponentsReadyCondition      conditions.ConditionType = "SystemComponentsReady"
)

var (
	Conditions = []conditions.ConditionType{
		conditions.TypeReady,
		PreUpgradeValidationReadyCondition,
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
// These fields should only be set for advanced use cases. The fields here have no stability guarantees.
type Overrides struct {
	// Paused disables all reconciliation of the DPFOperatorConfig when set to true.
	// +optional
	Paused *bool `json:"paused,omitempty"`

	// DPUCNIBinPath is the path at which the CNI binaries will be installed to on the DPU.
	// This is /opt/cni/bin by default.
	// This setting does not change where kubelet is configured to use the CNI from.
	// +optional
	DPUCNIBinPath *string `json:"dpuCNIBinPath,omitempty"`

	// DPUCNIConfigPath is the path to which the CNI config files will be installed on the DPU.
	// This is /etc/cni/net.d by default.
	// This setting does not change where kubelet is configured to read the CNI config from.
	// +optional
	DPUCNIConfigPath *string `json:"dpuCNIPath,omitempty"`

	// DPUOpenvSwitchPath is the path at which the openvSwitch run directory can be found on the DPU.
	// This is /var/run/openvswitch by default.
	// This setting does not change where components are installed. Installation location fixed in the BFB.
	// +optional
	DPUOpenvSwitchRunPath *string `json:"dpuOpenvSwitchRunPath,omitempty"`

	// DPUOpenvSwitchBinPath is the path at which the openvSwitch bin directory can be found on the DPU node.
	// This is /usr/bin/ by default.
	// This setting does not change where components are installed. Installation location fixed in the BFB.
	// +optional
	DPUOpenvSwitchBinPath *string `json:"dpuOpenvSwitchBinPath,omitempty"`

	// DPUOpenvSwitchSystemSharedLibPath is the path at which the system lib used by OVS components can be found on the DPU.
	// This is /lib by default.
	// This setting does not change where components are installed. Installation location fixed in the BFB.
	// +optional
	DPUOpenvSwitchSystemSharedLibPath *string `json:"dpuOpenvSwitchSystemSharedPath,omitempty"`

	// FlannelSkipCNIConfigInstallation controls whether Flannel should skip CNI config installation.
	// This is true by default, meaning Flannel does not manage its own CNI configuration.
	// Set to false if you want Flannel to install a CNI configuration.
	// +optional
	FlannelSkipCNIConfigInstallation *bool `json:"flannelSkipCNIConfigInstallation,omitempty"`

	// DPUOpenvSwitchSystemSharedLib64Path is the path at which the system lib64 used by OVS components can be found on the DPU.
	// If this field is not set, no lib64 volume mount will be configured in the SFC Controller component.
	// This setting does not change where components are installed. Installation location fixed in the BFB.
	// +optional
	// +kubebuilder:validation:MinLength=1
	DPUOpenvSwitchSystemSharedLib64Path *string `json:"dpuOpenvSwitchSystemSharedLib64Path,omitempty"`

	// KubernetesAPIServerVIP is the VIP the Kubernetes API server is accessible at.
	// This setting enables specific underlying components deployed directly or indirectly by the DPF Operator to reach
	// the Kubernetes API Server when the ClusterIP Kubernetes Service is not functional.
	// If set, it should be set to an IP to ensure that components work even if DNS is not available in the cluster.
	// +optional
	KubernetesAPIServerVIP *string `json:"kubernetesAPIServerVIP,omitempty"`

	// KubernetesAPIServerPort is the port the Kubernetes API server is accessible at.
	// This setting is usually used together with the kubernetesAPIServerVIP setting. It enables specific underlying
	// components deployed directly or indirectly by the DPF Operator to reach the Kubernetes API Server when the
	// ClusterIP Kubernetes Service is not functional.
	// +optional
	KubernetesAPIServerPort *int `json:"kubernetesAPIServerPort,omitempty"`

	// ArgoCDNamespace is the namespace where ArgoCD is deployed.
	// AppProjects and cluster secrets required by DPF will be created in this namespace.
	// Defaults to the namespace of the DPFOperatorConfig.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	ArgoCDNamespace *string `json:"argoCDNamespace,omitempty"`
}

// Networking defines the networking configuration for the system components.
type Networking struct {
	// ControlPlaneMTU is the MTU value to be set on the management network.
	// The default is 1500.
	// +kubebuilder:validation:Minimum=1280
	// +kubebuilder:validation:Maximum=9216
	// +kubebuilder:default=1500
	// +optional
	ControlPlaneMTU *int `json:"controlPlaneMTU,omitempty"`

	// HighSpeedMTU is the MTU value to be set on the high-speed interface.
	// The default is 1500.
	// +kubebuilder:validation:Minimum=1280
	// +kubebuilder:validation:Maximum=9216
	// +kubebuilder:default=1500
	// +optional
	HighSpeedMTU *int `json:"highSpeedMTU,omitempty"`
}

// DeploymentMode describes the cluster deployment model for DPU provisioning (zero-trust vs host-trusted).
// +kubebuilder:validation:Enum=zero-trust;host-trusted
type DeploymentMode string

const (
	// DeploymentModeZeroTrust requires provisioningController.installInterface.installViaRedfish
	DeploymentModeZeroTrust DeploymentMode = "zero-trust"
	// DeploymentModeHostTrusted allows provisioningController.installInterface.installViaHostAgent, or installViaGNOI
	DeploymentModeHostTrusted DeploymentMode = "host-trusted"
)

// DPFOperatorConfigSpec defines the desired state of DPFOperatorConfig
// +kubebuilder:validation:XValidation:rule="self.deploymentMode != 'zero-trust' || (has(self.provisioningController.installInterface) && has(self.provisioningController.installInterface.installViaRedfish))",message="deploymentMode zero-trust requires provisioningController.installInterface.installViaRedfish"
// +kubebuilder:validation:XValidation:rule="self.deploymentMode != 'host-trusted' || !has(self.provisioningController.installInterface) || !has(self.provisioningController.installInterface.installViaRedfish)",message="deploymentMode host-trusted does not support provisioningController.installInterface.installViaRedfish"
type DPFOperatorConfigSpec struct {
	// +optional
	Overrides *Overrides `json:"overrides,omitempty"`

	// +kubebuilder:default={controlPlaneMTU: 1500}
	// +optional
	Networking *Networking `json:"networking,omitempty"`
	// Monitoring is the configuration for monitoring resources.
	// +optional
	Monitoring *MonitoringConfiguration `json:"monitoring,omitempty"`

	// List of secret names which are used to pull images for DPF system components and DPUServices.
	// These secrets must be in the same namespace as the DPF Operator Config and should be created before the config is created.
	// System reconciliation will not proceed until these secrets are available.
	// +optional
	ImagePullSecrets []string `json:"imagePullSecrets,omitempty"`

	// DeploymentMode selects zero-trust vs host-trusted deployment alignment.
	// Required: operators must set this explicitly; provisioning controllers propagate this to DPU.status.deploymentMode.
	// +required
	DeploymentMode DeploymentMode `json:"deploymentMode"`

	// DPUServiceController is the configuration for the DPUServiceController
	// +optional
	DPUServiceController *DPUServiceControllerConfiguration `json:"dpuServiceController,omitempty"`
	// ProvisioningController is the configuration for the ProvisioningController
	ProvisioningController *ProvisioningControllerConfiguration `json:"provisioningController"`
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
	// CNIInstaller is the configuration for the cni-installer
	// +optional
	CNIInstaller *CNIInstallerConfiguration `json:"cniInstaller,omitempty"`
	// SFCController is the configuration for the SFCController
	// +optional
	SFCController *SFCControllerConfiguration `json:"sfcController,omitempty"`
	// KamajiClusterManager is the configuration for the kamaji-cluster-manager
	// +optional
	KamajiClusterManager *KamajiClusterManagerConfiguration `json:"kamajiClusterManager,omitempty"`
	// StaticClusterManager is the configuration for the static-cluster-manager
	// +optional
	StaticClusterManager *StaticClusterManagerConfiguration `json:"staticClusterManager,omitempty"`
	// NodeSRIOVDevicePluginController is the configuration for the NodeSRIOVDevicePlugin controller.
	// This controller manages per-node SRIOV device plugin pods based on DPU configurations.
	// The controller is disabled by default.
	// +optional
	NodeSRIOVDevicePluginController *NodeSRIOVDevicePluginControllerConfiguration `json:"nodeSRIOVDevicePluginController,omitempty"`
}

// MonitoringConfiguration defines the configuration for monitoring resources.
type MonitoringConfiguration struct {
	// Disable controls whether monitoring resources are installed.
	// When enabled (default), the controller:
	// - Creates ServiceMonitors for Kamaji clusters to scrape control-plane metrics.
	// - Deploys kube-state-metrics as a DPUService to expose metrics for custom resources.
	// - Deploys node-problem-detector as a DaemonSet on DPU nodes to detect and report node-level problems.
	// - Deploys opentelemetry-collector as a DaemonSet on DPU nodes to collect and forward logs.
	// +optional
	Disable *bool `json:"disable,omitempty"`

	// KubeStateMetrics is the configuration for kube-state-metrics
	// +optional
	KubeStateMetrics *KubeStateMetricsConfiguration `json:"kubeStateMetrics,omitempty"`

	// NodeProblemDetector is the configuration for node-problem-detector
	// +optional
	NodeProblemDetector *NodeProblemDetectorConfiguration `json:"nodeProblemDetector,omitempty"`

	// OpenTelemetryCollector is the configuration for opentelemetry-collector
	// +optional
	OpenTelemetryCollector *OpenTelemetryCollectorConfiguration `json:"openTelemetryCollector,omitempty"`
}

// DPFOperatorConfigStatus defines the observed state of DPFOperatorConfig
type DPFOperatorConfigStatus struct {
	// Conditions exposes the current state of the OperatorConfig.
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration records the Generation observed on the object the last time it was patched.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Version is the version of the DPF Operator that is currently deployed.
	Version *string `json:"version,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
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

func (c *DPFOperatorConfig) UpgradeInProgress() bool {
	return c.Status.Version != nil && *c.Status.Version != release.DPFVersion()
}

func (c *DPFOperatorConfig) IsNewConfig() bool {
	return c.Status.ObservedGeneration == 0
}

// GetArgoCDNamespace returns the namespace where ArgoCD is deployed.
// Falls back to the DPFOperatorConfig's own namespace if not explicitly configured.
func (c *DPFOperatorConfig) GetArgoCDNamespace() string {
	if c.Spec.Overrides != nil && c.Spec.Overrides.ArgoCDNamespace != nil && *c.Spec.Overrides.ArgoCDNamespace != "" {
		return *c.Spec.Overrides.ArgoCDNamespace
	}
	return c.GetNamespace()
}

func (c *DPFOperatorConfig) MonitoringEnabled() bool {
	return c.Spec.Monitoring == nil || c.Spec.Monitoring.Disable == nil || !*c.Spec.Monitoring.Disable
}
