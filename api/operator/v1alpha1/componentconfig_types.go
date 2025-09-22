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

type DefaultOverridesConfiguration struct {
	ImageComponentConfig    `json:",inline"`
	ResourceComponentConfig `json:",inline"`
}

type ProvisioningControllerConfiguration struct {
	BaseComponentConfig `json:",inline"`

	// Image overrides the container image used by the Provisioning controller.
	//
	// Deprecated: This field is deprecated and will be removed with v26.1.0.
	// Use the new field `controller` instead.
	// +optional
	Image Image `json:"image,omitempty"`

	// Controller contains the configuration for the Provisioning controller component.
	// It contains the image for the controller and its resource requirements.
	// +optional
	Controller *DefaultOverridesConfiguration `json:"controller,omitempty"`

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
	return ProvisioningControllerName.String()
}

// Deprecated: This method is deprecated and will be removed with v26.1.0. Use GetImages instead.
func (c *ProvisioningControllerConfiguration) GetImage() *string {
	return c.Image
}

// GetImages returns a map of container names to their images
func (c *ProvisioningControllerConfiguration) GetImages() map[ContainerName]*string {
	images := make(map[ContainerName]*string)
	if c.Controller != nil {
		images[ControllerManagerContainer] = c.Controller.GetImage()
	}
	return images
}

func (c *ProvisioningControllerConfiguration) GetResources() map[ContainerName]*corev1.ResourceRequirements {
	if c.Controller == nil {
		return nil
	}
	return map[ContainerName]*corev1.ResourceRequirements{
		ControllerManagerContainer: c.Controller.GetResource(),
	}
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
	// SkipDpuNodeDiscovery is a flag to skip the DPU node discovery.
	// +optional
	// +kubebuilder:default=true
	SkipDpuNodeDiscovery *bool `json:"skipDpuNodeDiscovery,omitempty"`
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
	return BFBRegistryName.String()
}

func (c *BFBRegistryConfiguration) Disabled() bool {
	if c.Disable == nil {
		return false
	}
	return *c.Disable
}

type DPUServiceControllerConfiguration struct {
	BaseComponentConfig `json:",inline"`

	// Image overrides the container image used by the DPUService controller.
	//
	// Deprecated: This field is deprecated and will be removed with v26.1.0.
	// Use the new field `controller` instead.
	// +optional
	Image Image `json:"image,omitempty"`

	// Controller contains the configuration for the DPU Service controller component.
	// It contains the image for the controller and its resource requirements.
	// +optional
	Controller *DefaultOverridesConfiguration `json:"controller,omitempty"`

	// DisableDPUReady disables the DPU Ready Controller functionality in the DPU Service Controller.
	// The DPU Ready Controller adds taints to the worker nodes when the DPU is not ready.
	// This is useful when the DPU is used for networking and the node should not be scheduled until the DPU is ready.
	// +optional
	DisableDPUReadyCheck *bool `json:"disableDPUReadyCheck,omitempty"`
}

func (c *DPUServiceControllerConfiguration) Name() string {
	return DPUServiceControllerName.String()
}

// Deprecated: This method is deprecated and will be removed with v26.1.0. Use GetImages instead.
func (c *DPUServiceControllerConfiguration) GetImage() *string {
	return c.Image
}

// GetImages returns a map of container names to their images
func (c *DPUServiceControllerConfiguration) GetImages() map[ContainerName]*string {
	images := make(map[ContainerName]*string)
	if c.Controller != nil {
		images[ControllerManagerContainer] = c.Controller.GetImage()
	}
	return images
}

func (c *DPUServiceControllerConfiguration) GetResources() map[ContainerName]*corev1.ResourceRequirements {
	if c.Controller == nil {
		return nil
	}
	return map[ContainerName]*corev1.ResourceRequirements{
		ControllerManagerContainer: c.Controller.GetResource(),
	}
}

// DPUDetectorConfiguration is the configuration for the DPUDetectorContainer Component.
type DPUDetectorConfiguration struct {
	BaseComponentConfig `json:",inline"`

	// Image overrides the container image used by the DPUDetector Container.
	//
	// Deprecated: This field is deprecated and will be removed with v26.1.0.
	// Use the new field `daemon` instead.
	// +optional
	Image Image `json:"image,omitempty"`

	// Daemon contains the configuration for the DPU Detector component.
	// It contains the image for the controller and its resource requirements.
	// +optional
	Daemon *DefaultOverridesConfiguration `json:"daemon,omitempty"`
}

func (c *DPUDetectorConfiguration) Name() string {
	return DPUDetectorName.String()
}

// Deprecated: This method is deprecated and will be removed with v26.1.0. Use GetImages instead.
func (c *DPUDetectorConfiguration) GetImage() *string {
	return c.Image
}

// GetImages returns a map of container names to their images
func (c *DPUDetectorConfiguration) GetImages() map[ContainerName]*string {
	images := make(map[ContainerName]*string)
	if c.Daemon != nil {
		images[DPUDetectorContainer] = c.Daemon.GetImage()
	}
	return images
}

func (c *DPUDetectorConfiguration) GetResources() map[ContainerName]*corev1.ResourceRequirements {
	if c.Daemon == nil {
		return nil
	}
	return map[ContainerName]*corev1.ResourceRequirements{
		DPUDetectorContainer: c.Daemon.GetResource(),
	}
}

type KamajiClusterManagerConfiguration struct {
	BaseComponentConfig `json:",inline"`

	// Image overrides the container image used by the Kamaji Cluster Manager.
	//
	// Deprecated: This field is deprecated and will be removed with v26.1.0.
	// Use the new field `controller` instead.
	// +optional
	Image Image `json:"image,omitempty"`

	// Controller contains the configuration for the Kamaji Cluster Manager component.
	// It contains the image for the controller and its resource requirements.
	// +optional
	Controller *DefaultOverridesConfiguration `json:"controller,omitempty"`
}

func (c *KamajiClusterManagerConfiguration) Name() string {
	return KamajiClusterManagerName.String()
}

// Deprecated: This method is deprecated and will be removed with v26.1.0. Use GetImages instead.
func (c *KamajiClusterManagerConfiguration) GetImage() *string {
	return c.Image
}

// GetImages returns a map of container names to their images
func (c *KamajiClusterManagerConfiguration) GetImages() map[ContainerName]*string {
	images := make(map[ContainerName]*string)
	if c.Controller != nil {
		images[ControllerManagerContainer] = c.Controller.GetImage()
	}
	return images
}

func (c *KamajiClusterManagerConfiguration) GetResources() map[ContainerName]*corev1.ResourceRequirements {
	if c.Controller == nil {
		return nil
	}
	return map[ContainerName]*corev1.ResourceRequirements{
		ControllerManagerContainer: c.Controller.GetResource(),
	}
}

type StaticClusterManagerConfiguration struct {
	BaseComponentConfig `json:",inline"`

	// Image overrides the container image used by the Static Cluster Manager.
	//
	// Deprecated: This field is deprecated and will be removed with v26.1.0.
	// Use the new field `controller` instead.
	// +optional
	Image Image `json:"image,omitempty"`

	// Controller contains the configuration for the Static Cluster Manager controller component.
	// It contains the image for the controller and its resource requirements.
	// +optional
	Controller *DefaultOverridesConfiguration `json:"controller,omitempty"`
}

func (c *StaticClusterManagerConfiguration) Name() string {
	return StaticClusterManagerName.String()
}

// Deprecated: This method is deprecated and will be removed with v26.1.0. Use GetImages instead.
func (c *StaticClusterManagerConfiguration) GetImage() *string {
	return c.Image
}

// GetImages returns a map of container names to their images
func (c *StaticClusterManagerConfiguration) GetImages() map[ContainerName]*string {
	images := make(map[ContainerName]*string)
	if c.Controller != nil {
		images[ControllerManagerContainer] = c.Controller.GetImage()
	}
	return images
}

func (c *StaticClusterManagerConfiguration) GetResources() map[ContainerName]*corev1.ResourceRequirements {
	if c.Controller == nil {
		return nil
	}
	return map[ContainerName]*corev1.ResourceRequirements{
		ControllerManagerContainer: c.Controller.GetResource(),
	}
}

type ServiceSetControllerConfiguration struct {
	BaseComponentConfig       `json:",inline"`
	HelmComponentConfig       `json:",inline"`
	InClusterDeploymentConfig `json:",inline"`

	// Image overrides the container image used by the ServiceChainSet Controller.
	//
	// Deprecated: This field is deprecated and will be removed with v26.1.0.
	// Use the new field `controller` instead.
	// +optional
	Image Image `json:"image,omitempty"`

	// Controller contains the configuration for the ServiceChainSet controller component.
	// It contains the image for the controller and its resource requirements.
	// +optional
	Controller *DefaultOverridesConfiguration `json:"controller,omitempty"`
}

func (c *ServiceSetControllerConfiguration) Name() string {
	return ServiceSetControllerName.String()
}

// Deprecated: This method is deprecated and will be removed with v26.1.0. Use GetImages instead.
func (c *ServiceSetControllerConfiguration) GetImage() *string {
	return c.Image
}

// GetImages returns a map of container names to their images
func (c *ServiceSetControllerConfiguration) GetImages() map[ContainerName]*string {
	images := make(map[ContainerName]*string)
	if c.Controller != nil {
		images[ControllerManagerContainer] = c.Controller.GetImage()
	}
	return images
}

func (c *ServiceSetControllerConfiguration) GetResources() map[ContainerName]*corev1.ResourceRequirements {
	if c.Controller == nil {
		return nil
	}
	return map[ContainerName]*corev1.ResourceRequirements{
		ControllerManagerContainer: c.Controller.GetResource(),
	}
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
	// Deprecated: This field is deprecated and will be removed with v26.1.0.
	// Use the new fields `cni` and `daemon` instead.
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
	return FlannelName.String()
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
func (c *FlannelConfiguration) GetImages() map[ContainerName]*string {
	images := make(map[ContainerName]*string)
	if c.CNI != nil {
		images[FlannelContainerCNI] = c.CNI.GetImage()
	}
	if c.Daemon != nil {
		images[FlannelContainerDaemon] = c.Daemon.GetImage()
	}
	return images
}

func (c *FlannelConfiguration) GetResources() map[ContainerName]*corev1.ResourceRequirements {
	if c.Daemon == nil {
		return nil
	}
	return map[ContainerName]*corev1.ResourceRequirements{
		FlannelContainerDaemon: c.Daemon.GetResource(),
	}
}

type MultusConfiguration struct {
	BaseComponentConfig       `json:",inline"`
	HelmComponentConfig       `json:",inline"`
	InClusterDeploymentConfig `json:",inline"`

	// Image overrides the container image used by the Multus Container.
	//
	// Deprecated: This field is deprecated and will be removed with v26.1.0.
	// Use the new field `cni` instead.
	// +optional
	Image Image `json:"image,omitempty"`

	// CNI contains the configuration for the Multus CNI component.
	// It contains the image for the controller and its resource requirements.
	// +optional
	CNI *DefaultOverridesConfiguration `json:"cni,omitempty"`
}

func (c *MultusConfiguration) Name() string {
	return MultusName.String()
}

// Deprecated: This method is deprecated and will be removed with v26.1.0. Use GetImages instead.
func (c *MultusConfiguration) GetImage() *string {
	return c.Image
}

// GetImages returns a map of container names to their images
func (c *MultusConfiguration) GetImages() map[ContainerName]*string {
	images := make(map[ContainerName]*string)
	if c.CNI != nil {
		images[MultusContainer] = c.CNI.GetImage()
	}
	return images
}

func (c *MultusConfiguration) GetResources() map[ContainerName]*corev1.ResourceRequirements {
	if c.CNI == nil {
		return nil
	}
	return map[ContainerName]*corev1.ResourceRequirements{
		MultusContainer: c.CNI.GetResource(),
	}
}

type NVIPAMConfiguration struct {
	BaseComponentConfig       `json:",inline"`
	HelmComponentConfig       `json:",inline"`
	InClusterDeploymentConfig `json:",inline"`

	// Image overrides the container image used by the NVIPAM controller.
	//
	// Deprecated: This field is deprecated and will be removed with v26.1.0.
	// Use the new field `controller` instead.
	// +optional
	Image Image `json:"image,omitempty"`

	// Controller contains the configuration for the NVIPAM controller component.
	// It contains the image for the controller and its resource requirements.
	// +optional
	Controller *NVIPAMController `json:"controller,omitempty"`

	// Node contains the configuration for the NVIPAM node component.
	// It contains the image for the node and its resource requirements.
	Node *NVIPAMNode `json:"node,omitempty"`
}

type NVIPAMController struct {
	ImageComponentConfig    `json:",inline"`
	ResourceComponentConfig `json:",inline"`
}

type NVIPAMNode struct {
	ResourceComponentConfig `json:",inline"`
}

// Deprecated: This method is deprecated and will be removed with v26.1.0. Use GetImages instead.
func (c *NVIPAMConfiguration) GetImage() *string {
	return c.Image
}

func (c *NVIPAMConfiguration) GetImages() map[ContainerName]*string {
	images := make(map[ContainerName]*string)
	if c.Controller != nil {
		images[NVIPAMContainerController] = c.Controller.GetImage()
	}
	return images
}

func (c *NVIPAMConfiguration) GetResources() map[ContainerName]*corev1.ResourceRequirements {
	resources := make(map[ContainerName]*corev1.ResourceRequirements)
	if c.Controller != nil {
		resources[NVIPAMContainerController] = c.Controller.GetResource()
	}
	if c.Node != nil {
		resources[NVIPAMContainerNode] = c.Node.GetResource()
	}
	return resources
}

func (c *NVIPAMConfiguration) Name() string {
	return NVIPAMName.String()
}

type SRIOVDevicePluginConfiguration struct {
	BaseComponentConfig       `json:",inline"`
	HelmComponentConfig       `json:",inline"`
	InClusterDeploymentConfig `json:",inline"`

	// Image overrides the container image used by the SRIOV Device Plugin container.
	//
	// Deprecated: This field is deprecated and will be removed with v26.1.0.
	// Use the new field `deviceplugin` instead.
	// +optional
	Image Image `json:"image,omitempty"`

	// DevicePlugin contains the configuration for the SRIOV Device Plugin component.
	// It contains the image for the controller and its resource requirements.
	// +optional
	DevicePlugin *DefaultOverridesConfiguration `json:"deviceplugin,omitempty"`
}

func (c *SRIOVDevicePluginConfiguration) Name() string {
	return SRIOVDevicePluginName.String()
}

// Deprecated: This method is deprecated and will be removed with v26.1.0. Use GetImages instead.
func (c *SRIOVDevicePluginConfiguration) GetImage() *string {
	return c.Image
}

// GetImages returns a map of container names to their images
func (c *SRIOVDevicePluginConfiguration) GetImages() map[ContainerName]*string {
	images := make(map[ContainerName]*string)
	if c.DevicePlugin != nil {
		images[SRIOVDevicePluginContainer] = c.DevicePlugin.GetImage()
	}
	return images
}

func (c *SRIOVDevicePluginConfiguration) GetResources() map[ContainerName]*corev1.ResourceRequirements {
	if c.DevicePlugin == nil {
		return nil
	}
	return map[ContainerName]*corev1.ResourceRequirements{
		SRIOVDevicePluginContainer: c.DevicePlugin.GetResource(),
	}
}

type OVSCNIConfiguration struct {
	BaseComponentConfig       `json:",inline"`
	HelmComponentConfig       `json:",inline"`
	InClusterDeploymentConfig `json:",inline"`

	// Image overrides the container image used by the OVS CNI.
	//
	// Deprecated: This field is deprecated and will be removed with v26.1.0.
	// Use the new field `cni` instead.
	// +optional
	Image Image `json:"image,omitempty"`

	// CNI contains the configuration for the OVS CNI component.
	// It contains the image for the controller and its resource requirements.
	// +optional
	CNI *DefaultOverridesConfiguration `json:"cni,omitempty"`
}

func (c *OVSCNIConfiguration) Name() string {
	return OVSCNIName.String()
}

// Deprecated: This method is deprecated and will be removed with v26.1.0. Use GetImages instead.
func (c *OVSCNIConfiguration) GetImage() *string {
	return c.Image
}

// GetImages returns a map of container names to their images
func (c *OVSCNIConfiguration) GetImages() map[ContainerName]*string {
	images := make(map[ContainerName]*string)
	if c.CNI != nil {
		images[OVSCNI] = c.CNI.GetImage()
	}
	return images
}

func (c *OVSCNIConfiguration) GetResources() map[ContainerName]*corev1.ResourceRequirements {
	if c.CNI == nil {
		return nil
	}
	return map[ContainerName]*corev1.ResourceRequirements{
		OVSCNI: c.CNI.GetResource(),
	}
}

type SFCControllerConfiguration struct {
	BaseComponentConfig       `json:",inline"`
	HelmComponentConfig       `json:",inline"`
	InClusterDeploymentConfig `json:",inline"`

	// Image overrides the container image used by the SFC controller.
	//
	// Deprecated: This field is deprecated and will be removed with v26.1.0.
	// Use the new field `controller` instead.
	// +optional
	Image Image `json:"image,omitempty"`

	// Controller contains the configuration for the SFC controller component.
	// It contains the image for the controller and its resource requirements.
	// +optional
	Controller *DefaultOverridesConfiguration `json:"controller,omitempty"`

	// SecureFlowDeletionTimeout controls the timeout for which the API server is unreachable after which all the flows
	// are deleted to prevent unintended packet leaks. It has effect when is greater than zero.
	// Value must be in units accepted by Go time.ParseDuration https://golang.org/pkg/time/#ParseDuration.
	// +optional
	SecureFlowDeletionTimeout *metav1.Duration `json:"secureFlowDeletionTimeout,omitempty"`
}

func (c *SFCControllerConfiguration) Name() string {
	return SFCControllerName.String()
}

// Deprecated: This method is deprecated and will be removed with v26.1.0. Use GetImages instead.
func (c *SFCControllerConfiguration) GetImage() *string {
	return c.Image
}

// GetImages returns a map of container names to their images
func (c *SFCControllerConfiguration) GetImages() map[ContainerName]*string {
	images := make(map[ContainerName]*string)
	if c.Controller != nil {
		images[ControllerManagerContainer] = c.Controller.GetImage()
	}
	return images
}

func (c *SFCControllerConfiguration) GetResources() map[ContainerName]*corev1.ResourceRequirements {
	if c.Controller == nil {
		return nil
	}
	return map[ContainerName]*corev1.ResourceRequirements{
		ControllerManagerContainer: c.Controller.GetResource(),
	}
}

type CNIInstallerConfiguration struct {
	BaseComponentConfig       `json:",inline"`
	HelmComponentConfig       `json:",inline"`
	InClusterDeploymentConfig `json:",inline"`

	// Installer contains the configuration for the CNI-Installer component.
	// It contains the image for the controller and its resource requirements.
	// +optional
	Installer *DefaultOverridesConfiguration `json:"installer,omitempty"`
}

func (c *CNIInstallerConfiguration) Name() string {
	return CNIInstallerName.String()
}

// GetImages returns a map of container names to their images
func (c *CNIInstallerConfiguration) GetImages() map[ContainerName]*string {
	images := make(map[ContainerName]*string)
	if c.Installer != nil {
		images[CNIInstallerContainer] = c.Installer.GetImage()
	}
	return images
}

func (c *CNIInstallerConfiguration) GetResources() map[ContainerName]*corev1.ResourceRequirements {
	if c.Installer == nil {
		return nil
	}
	return map[ContainerName]*corev1.ResourceRequirements{
		CNIInstallerContainer: c.Installer.GetResource(),
	}
}
