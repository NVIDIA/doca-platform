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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

type ProvisioningControllerConfiguration struct {
	BaseComponentConfig     `json:",inline"`
	ImageComponentConfig    `json:",inline"`
	ResourceComponentConfig `json:",inline"`

	// BFCFGTemplateConfigMap is the name of a configMap containing a template for the BF.cfg file used by the DPU controller.
	// By default the provisioning controller use a hardcoded BF.cfg e.g. https://github.com/NVIDIA/doca-platform/blob/release-v24.10/internal/provisioning/controllers/dpu/bfcfg/bf.cfg.template
	// Note: Replacing the bf.cfg is an advanced use case. The default bf.cfg is designed for most use cases.
	// +optional
	BFCFGTemplateConfigMap *string `json:"bfCFGTemplateConfigMap,omitempty"`

	// BFBPersistentVolumeClaimName is the name of the PersistentVolumeClaim used by dpf-provisioning-controller
	// +kubebuilder:validation:MinLength=1
	BFBPersistentVolumeClaimName string `json:"bfbPVCName"`

	// DMSTimeout is the max time in seconds within which a DMS API must respond, 0 is unlimited
	// +kubebuilder:validation:Minimum=1
	// +optional
	DMSTimeout *int `json:"dmsTimeout,omitempty"`

	// CustomCASecretName indicates the name of the Kubernetes secret object
	// which containing the custom CA certificate
	// +optional
	CustomCASecretName *string `json:"customCASecretName,omitempty"`

	// InstallInterface is the interface through which the BFB is installed
	// +optional
	InstallInterface *ProvisioningInstallInterface `json:"installInterface,omitempty"`

	// MaxDPUParallelInstallations specifies the maximum number of DPUs that can be provisioned concurrently.
	// A DPU is removed from the concurrent provisioning count as soon as it finishes the "OS Installing" phase and
	// enters the "Rebooting" phase of its provisioning lifecycle.
	// +kubebuilder:default=50
	// +kubebuilder:validation:Minimum=1
	// +optional
	MaxDPUParallelInstallations *int32 `json:"maxDPUParallelInstallations,omitempty"`

	// MultiDPUOperationsSyncWaitTime is the wait time between DPUs sync operations on the same node.
	// It would take effect only on DPUNode objects which contain more than one DPU.
	// +kubebuilder:default="30s"
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern=`^([0-9]+(h|m|s|ms|us|µs|ns))+$`
	// +kubebuilder:validation:Format=duration
	// +optional
	MultiDPUOperationsSyncWaitTime *metav1.Duration `json:"multiDPUOperationsSyncWaitTime,omitempty"`

	// MaxUnavailableDPUNodes is the maximum number of DPUNodes that are unavailable during the node effect period.
	// +kubebuilder:default=50
	// +kubebuilder:validation:Minimum=1
	// +optional
	MaxUnavailableDPUNodes *int32 `json:"maxUnavailableDPUNodes,omitempty"`
}

func (c *ProvisioningControllerConfiguration) Name() string {
	return ProvisioningControllerName
}

// ProvisioningInstallInterface is the interface used to install the BFB
// +kubebuilder:validation:XValidation:rule="(has(self.installViaGNOI) && !has(self.installViaRedfish)) || (!has(self.installViaGNOI) && has(self.installViaRedfish))",message="exactly one of installViaGNOI or installViaRedfish must be set"
type ProvisioningInstallInterface struct {
	// InstallViaGNOI is the interface used to install the BFB via GNOI
	// +optional
	InstallViaGNOI *InstallViaGNOI `json:"installViaGNOI,omitempty"`
	// InstallViaRedfish is the interface used to install the BFB via Redfish
	// +optional
	InstallViaRedfish *InstallViaRedfish `json:"installViaRedfish,omitempty"`
}

// InstallViaGNOI is the interface used to install the BFB via GNOI
type InstallViaGNOI struct{}

// InstallViaRedfish is the interface used to install the BFB via Redfish
type InstallViaRedfish struct {
	// BFBRegistryAddress is the address of the BFB Registry
	// +kubebuilder:validation:MinLength=1
	BFBRegistryAddress string `json:"bfbRegistryAddress,omitempty"`
	// BFBRegistry is the configuration for the BFB Registry
	// +optional
	BFBRegistry *BFBRegistryConfiguration `json:"bfbRegistry,omitempty"`
}

type BFBRegistryConfiguration struct {
	// Disable ensures the BFB Registry is not deployed when set to true.
	// +optional
	Disable *bool `json:"disable,omitempty"`

	// Port is the port on which the BFB Registry will listen
	// +optional
	Port *int `json:"port,omitempty"`
}

func (c *BFBRegistryConfiguration) Name() string {
	return BFBRegistryName
}

func (c *BFBRegistryConfiguration) Disabled() bool {
	if c.Disable == nil {
		return false
	}
	return *c.Disable
}

type DPUServiceControllerConfiguration struct {
	BaseComponentConfig     `json:",inline"`
	ImageComponentConfig    `json:",inline"`
	ResourceComponentConfig `json:",inline"`
}

func (c *DPUServiceControllerConfiguration) Name() string {
	return DPUServiceControllerName
}

// DPUDetectorConfiguration is the configuration for the DPUDetector Component.
type DPUDetectorConfiguration struct {
	BaseComponentConfig     `json:",inline"`
	ImageComponentConfig    `json:",inline"`
	ResourceComponentConfig `json:",inline"`
}

func (c *DPUDetectorConfiguration) Name() string {
	return DPUDetectorName
}

type KamajiClusterManagerConfiguration struct {
	BaseComponentConfig     `json:",inline"`
	ImageComponentConfig    `json:",inline"`
	ResourceComponentConfig `json:",inline"`
}

func (c *KamajiClusterManagerConfiguration) Name() string {
	return KamajiClusterManagerName
}

type StaticClusterManagerConfiguration struct {
	BaseComponentConfig     `json:",inline"`
	ImageComponentConfig    `json:",inline"`
	ResourceComponentConfig `json:",inline"`
}

func (c *StaticClusterManagerConfiguration) Name() string {
	return StaticClusterManagerName
}

type ServiceSetControllerConfiguration struct {
	BaseComponentConfig       `json:",inline"`
	ImageComponentConfig      `json:",inline"`
	ResourceComponentConfig   `json:",inline"`
	HelmComponentConfig       `json:",inline"`
	InClusterDeploymentConfig `json:",inline"`
}

func (c *ServiceSetControllerConfiguration) Name() string {
	return ServiceSetControllerName
}

type FlannelCNI struct {
	ImageComponentConfig `json:",inline"`
}

type FlannelDaemon struct {
	ImageComponentConfig    `json:",inline"`
	ResourceComponentConfig `json:",inline"`
}

type FlannelConfiguration struct {
	BaseComponentConfig       `json:",inline"`
	HelmComponentConfig       `json:",inline"`
	InClusterDeploymentConfig `json:",inline"`

	// CNI is the configuration for the Flannel CNI component.
	// It contains the image for the CNI init container.
	// Note: The resources for the CNI container are not configurable.
	// +optional
	CNI *FlannelCNI `json:"cni,omitempty"`

	// Daemon is the configuration for the Flannel Daemon component.
	// It contains the image for the Flannel Daemon container and its resource requirements.
	// +optional
	Daemon *FlannelDaemon `json:"daemon,omitempty"`

	// Images overrides the container images used by flannel
	//
	// Deprecated: This field is deprecated and will be removed with v25.10.
	// Use the new fields `cni` and `daemon` instead.
	//
	// +optional
	Images *FlannelImages `json:"image,omitempty"`

	// PodCIDR is the pod cidr for flannel.
	// +optional
	PodCIDR *string `json:"podCIDR,omitempty"`
}

type FlannelImages struct {
	// FlannelCNI must be set if FlannelImages is set.
	// +kubebuilder:validation:MinLength=1
	// +required
	FlannelCNI string `json:"flannelCNI,omitempty"`
	// KubeFlannel must be set if FlannelImages is set.
	// +kubebuilder:validation:MinLength=1
	// +required
	KubeFlannel string `json:"kubeFlannel,omitempty"`
}

func (c *FlannelConfiguration) Name() string {
	return FlannelName
}

// GetImage returns a comma-delimited list of the Flannel images with a specified order.
// KubeFlannel is first and FlannelCNi is second.
func (c *FlannelConfiguration) GetImage() *string {
	if c.Images == nil {
		return nil
	}
	// If either of fields is nil setting images does not work.
	if c.Images.KubeFlannel == "" || c.Images.FlannelCNI == "" {
		return nil
	}
	return ptr.To(strings.Join([]string{c.Images.KubeFlannel, c.Images.FlannelCNI}, ","))
}

// GetImages returns a map of container names to their images
func (c *FlannelConfiguration) GetImages() map[string]*string {
	images := make(map[string]*string)
	if c.CNI != nil {
		images[FlannelContainerCNIName.String()] = c.CNI.GetImage()
	}
	if c.Daemon != nil {
		images[FlannelContainerDaemonName.String()] = c.Daemon.GetImage()
	}
	return images
}

func (c *FlannelConfiguration) GetResources() map[string]*corev1.ResourceRequirements {
	if c.Daemon == nil {
		return nil
	}
	return map[string]*corev1.ResourceRequirements{
		FlannelContainerDaemonName.String(): c.Daemon.GetResource(),
	}
}

type MultusConfiguration struct {
	BaseComponentConfig       `json:",inline"`
	ImageComponentConfig      `json:",inline"`
	HelmComponentConfig       `json:",inline"`
	InClusterDeploymentConfig `json:",inline"`
	ResourceComponentConfig   `json:",inline"`
}

func (c *MultusConfiguration) Name() string {
	return MultusName
}

type NVIPAMConfiguration struct {
	BaseComponentConfig       `json:",inline"`
	ImageComponentConfig      `json:",inline"`
	HelmComponentConfig       `json:",inline"`
	InClusterDeploymentConfig `json:",inline"`

	// Controller contains the configuration for the NVIPAM controller component.
	// It contains the image for the controller and its resource requirements.
	Controller *NVIPAMController `json:"controller,omitempty"`

	// Node contains the configuration for the NVIPAM node component.
	// It contains the image for the node and its resource requirements.
	Node *NVIPAMNode `json:"node,omitempty"`
}

type NVIPAMController struct {
	ResourceComponentConfig `json:",inline"`
}

type NVIPAMNode struct {
	ResourceComponentConfig `json:",inline"`
}

func (c *NVIPAMConfiguration) GetResources() map[string]*corev1.ResourceRequirements {
	resources := make(map[string]*corev1.ResourceRequirements)
	if c.Controller != nil {
		resources[NVIPAMContainerControllerName.String()] = c.Controller.GetResource()
	}
	if c.Node != nil {
		resources[NVIPAMContainerNodeName.String()] = c.Node.GetResource()
	}
	return resources
}

func (c *NVIPAMConfiguration) Name() string {
	return NVIPAMName
}

type SRIOVDevicePluginConfiguration struct {
	BaseComponentConfig       `json:",inline"`
	ImageComponentConfig      `json:",inline"`
	HelmComponentConfig       `json:",inline"`
	InClusterDeploymentConfig `json:",inline"`
	ResourceComponentConfig   `json:",inline"`
}

func (c *SRIOVDevicePluginConfiguration) Name() string {
	return SRIOVDevicePluginName
}

type OVSCNIConfiguration struct {
	BaseComponentConfig       `json:",inline"`
	ImageComponentConfig      `json:",inline"`
	HelmComponentConfig       `json:",inline"`
	InClusterDeploymentConfig `json:",inline"`
	ResourceComponentConfig   `json:",inline"`
}

func (c *OVSCNIConfiguration) Name() string {
	return OVSCNIName
}

type SFCControllerConfiguration struct {
	BaseComponentConfig       `json:",inline"`
	ImageComponentConfig      `json:",inline"`
	HelmComponentConfig       `json:",inline"`
	InClusterDeploymentConfig `json:",inline"`
	ResourceComponentConfig   `json:",inline"`

	// SecureFlowDeletionTimeout controls the timeout for which the API server is unreachable after which all the flows
	// are deleted to prevent unintended packet leaks. It has effect when is greater than zero.
	// Value must be in units accepted by Go time.ParseDuration https://golang.org/pkg/time/#ParseDuration.
	// +optional
	SecureFlowDeletionTimeout *metav1.Duration `json:"secureFlowDeletionTimeout,omitempty"`
}

func (c *SFCControllerConfiguration) Name() string {
	return SFCControllerName
}
