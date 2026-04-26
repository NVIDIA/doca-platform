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

// +kubebuilder:validation:XValidation:rule="!has(self.image) || !has(self.controller) || !has(self.controller.image)",message="only either 'image' (deprecated) or 'controller.image' can be set, but not both"
// +kubebuilder:validation:XValidation:rule="!(has(self.bfCFGTemplateConfigMap) && has(self.enableDynamicBFCFGTemplates) && self.enableDynamicBFCFGTemplates)",message="bfCFGTemplateConfigMap and enableDynamicBFCFGTemplates are mutually exclusive"

type ProvisioningControllerConfiguration struct {
	BaseComponentConfig `json:",inline"`

	// Replicas is the number of replicas for the controller deployment.
	// This is used for High Availability. Leader election is enabled by default.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=3
	// +kubebuilder:default=2
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Image overrides the container image used by the Provisioning controller.
	//
	// Deprecated: This field is deprecated and will be removed with v26.4.0.
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
	//
	// Deprecated: BFCFGTemplateConfigMap is deprecated and will be removed in a future release.
	// Use enableDynamicBFCFGTemplates instead for custom bf.cfg templates.
	// +optional
	BFCFGTemplateConfigMap *string `json:"bfCFGTemplateConfigMap,omitempty"`

	// EnableDynamicBFCFGTemplates enables runtime discovery of bf.cfg templates via ConfigMaps.
	// When enabled, the provisioning controller discovers ConfigMaps by matching labels for BFB
	// name/namespace and DPUCluster name/namespace. Mutually exclusive with bfCFGTemplateConfigMap.
	// +optional
	EnableDynamicBFCFGTemplates bool `json:"enableDynamicBFCFGTemplates,omitempty"`

	// BFBPersistentVolumeClaimName is the name of the PersistentVolumeClaim used by dpf-provisioning-controller
	// If not provided, the controller will use local host storage (hostPath)
	// +optional
	BFBPersistentVolumeClaimName *string `json:"bfbPVCName,omitempty"`

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

	// Registry is the configuration for the BFB Registry
	// +optional
	Registry *RegistryConfiguration `json:"registry,omitempty"`

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

	// OSInstallTimeout is the maximum time allowed for OS installation in zero-trust mode.
	// If the installation exceeds this timeout, the DPU will transition to an error state.
	// +kubebuilder:default="60m"
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern=`^([0-9]+(h|m|s|ms|us|µs|ns))+$`
	// +kubebuilder:validation:Format=duration
	// +optional
	OSInstallTimeout *metav1.Duration `json:"osInstallTimeout,omitempty"`

	// NodeEffectRemovalTimeout is the maximum time allowed for the Node Effect Removal phase.
	// If the DPUNodeMaintenance CR still has requestors after this timeout, the DPU will transition to an error state.
	// When set to "0s" (the default), the timeout is disabled and no time limit is enforced.
	// +kubebuilder:default="0s"
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern=`^([0-9]+(h|m|s|ms|us|µs|ns))+$`
	// +kubebuilder:validation:Format=duration
	// +optional
	NodeEffectRemovalTimeout *metav1.Duration `json:"nodeEffectRemovalTimeout,omitempty"`

	// HostAgentDNSPolicy sets the DNS policy for the hostagent pod.
	// Valid values are 'ClusterFirstWithHostNet', 'ClusterFirst', 'Default' or 'None'.
	// Defaults to 'ClusterFirstWithHostNet'.
	// +kubebuilder:validation:Enum=ClusterFirstWithHostNet;ClusterFirst;Default;None
	// +optional
	HostAgentDNSPolicy *corev1.DNSPolicy `json:"hostAgentDNSPolicy,omitempty"`
}

func (c *ProvisioningControllerConfiguration) Name() string {
	return ProvisioningControllerName.String()
}

// Deprecated: This method is deprecated and will be removed with v26.4.0. Use GetImages instead.
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
// +kubebuilder:validation:XValidation:rule="((has(self.installViaHostAgent) || has(self.installViaGNOI)) && !has(self.installViaRedfish)) || (!has(self.installViaHostAgent) && !has(self.installViaGNOI) && has(self.installViaRedfish))",message="exactly one of installViaHostAgent, installViaGNOI or installViaRedfish must be set"
type ProvisioningInstallInterface struct {
	// InstallViaGNOI is the interface used to install the BFB via GNOI
	//
	// Deprecated: Use InstallViaHostAgent instead.
	// +optional
	InstallViaGNOI *InstallViaGNOI `json:"installViaGNOI,omitempty"`
	// InstallViaHostAgent is the interface used to install the BFB via HostAgent
	// +optional
	InstallViaHostAgent *InstallViaHostAgent `json:"installViaHostAgent,omitempty"`
	// InstallViaRedfish is the interface used to install the BFB via Redfish
	// +optional
	InstallViaRedfish *InstallViaRedfish `json:"installViaRedfish,omitempty"`
}

// InstallViaGNOI is the interface used to install the BFB via GNOI
type InstallViaGNOI struct{}

// InstallViaHostAgent is the interface used to install the BFB
type InstallViaHostAgent struct{}

// InstallViaRedfish is the interface used to install the BFB via Redfish
type InstallViaRedfish struct {
	// BFBRegistryAddress is the address of the BFB Registry
	//
	// Deprecated: Use RegistryConfiguration instead.
	// +kubebuilder:validation:MinLength=1
	BFBRegistryAddress string `json:"bfbRegistryAddress,omitempty"`
	// BFBRegistry is the configuration for the BFB Registry
	//
	// Deprecated: Use RegistryConfiguration instead.
	// +optional
	BFBRegistry *BFBRegistryConfiguration `json:"bfbRegistry,omitempty"`
	// SkipDPUNodeDiscovery is a flag to skip the DPU node discovery.
	// +optional
	// +kubebuilder:default=true
	SkipDPUNodeDiscovery *bool `json:"skipDPUNodeDiscovery,omitempty"`
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

type RegistryConfiguration struct {
	// Address is the address used to access the BFB Registry. The address must start with "http://".
	// By default, the BFB Registry can be accessed via its Service.
	// For non-kubernetes environments, this must be set due to the lack of kubelet on worker nodes.
	// For zero-trust environments, this must be set so that the BFB Registry can be accessed from DPU BMC.
	// +kubebuilder:validation:Pattern="^http://"
	// +optional
	// Deprecated: Address is deprecated and will be removed in a future release.
	Address *string `json:"address,omitempty"`

	// Port is the port on which the registry instances will listen
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	// Deprecated: Address is deprecated and will be removed in a future release.
	Port *int `json:"port,omitempty"`

	// LoadBalancerAddress is the address of the load balancer for the BFB Registry which the hostagent/redfish use to fetch the BFB and generated bf.cfg.
	// To enable the load balancer, you need to deploy your own load balancer controller and configure the LoadBalancerAddress field.
	// Then check the bfb-registry nodeport service and make your load balancer controller to distribute the requests to the bfb-registry nodeport.
	// +kubebuilder:validation:Pattern="^http://"
	// +optional
	LoadBalancerAddress *string `json:"loadBalancerAddress,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="!has(self.image) || !has(self.controller) || !has(self.controller.image)",message="only either 'image' (deprecated) or 'controller.image' can be set, but not both"

type DPUServiceControllerConfiguration struct {
	BaseComponentConfig  `json:",inline"`
	BaseControllerConfig `json:",inline"`

	// Image overrides the container image used by the DPUService controller.
	//
	// Deprecated: This field is deprecated and will be removed with v26.4.0.
	// Use the new field `controller` instead.
	// +optional
	Image Image `json:"image,omitempty"`

	// Controller contains the configuration for the DPU Service controller component.
	// It contains the image for the controller and its resource requirements.
	// +optional
	Controller *DefaultOverridesConfiguration `json:"controller,omitempty"`

	// DisableDPUReadyTaints disables the DPU ready taints feature in the DPU Service Controller.
	// This feature adds taints to the worker nodes when the DPU is not ready.
	// This is useful when the DPU is used for networking and the node should not be scheduled until the DPU is ready.
	// +optional
	DisableDPUReadyTaints *bool `json:"disableDPUReadyTaints,omitempty"`
}

func (c *DPUServiceControllerConfiguration) Name() string {
	return DPUServiceControllerName.String()
}

// Deprecated: This method is deprecated and will be removed with v26.4.0. Use GetImages instead.
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

// +kubebuilder:validation:XValidation:rule="!has(self.image) || !has(self.daemon) || !has(self.daemon.image)",message="only either 'image' (deprecated) or 'daemon.image' can be set, but not both"

type DPUDetectorConfiguration struct {
	BaseComponentConfig `json:",inline"`

	// Image overrides the container image used by the DPUDetector Container.
	//
	// Deprecated: This field is deprecated and will be removed with v26.4.0.
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

// Deprecated: This method is deprecated and will be removed with v26.4.0. Use GetImages instead.
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

// +kubebuilder:validation:XValidation:rule="!has(self.image) || !has(self.controller) || !has(self.controller.image)",message="only either 'image' (deprecated) or 'controller.image' can be set, but not both"

type KamajiClusterManagerConfiguration struct {
	BaseComponentConfig `json:",inline"`

	// Replicas is the number of replicas for the controller deployment.
	// This is used for High Availability. Leader election is enabled by default.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=3
	// +kubebuilder:default=2
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Image overrides the container image used by the Kamaji Cluster Manager.
	//
	// Deprecated: This field is deprecated and will be removed with v26.4.0.
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

// Deprecated: This method is deprecated and will be removed with v26.4.0. Use GetImages instead.
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

// +kubebuilder:validation:XValidation:rule="!has(self.image) || !has(self.controller) || !has(self.controller.image)",message="only either 'image' (deprecated) or 'controller.image' can be set, but not both"

type StaticClusterManagerConfiguration struct {
	BaseComponentConfig  `json:",inline"`
	BaseControllerConfig `json:",inline"`

	// Image overrides the container image used by the Static Cluster Manager.
	//
	// Deprecated: This field is deprecated and will be removed with v26.4.0.
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

// Deprecated: This method is deprecated and will be removed with v26.4.0. Use GetImages instead.
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

// +kubebuilder:validation:XValidation:rule="!has(self.image) || !has(self.controller) || !has(self.controller.image)",message="only either 'image' (deprecated) or 'controller.image' can be set, but not both"

type ServiceSetControllerConfiguration struct {
	BaseComponentConfig  `json:",inline"`
	BaseControllerConfig `json:",inline"`
	HelmComponentConfig  `json:",inline"`

	// Image overrides the container image used by the ServiceChainSet Controller.
	//
	// Deprecated: This field is deprecated and will be removed with v26.4.0.
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

// Deprecated: This method is deprecated and will be removed with v26.4.0. Use GetImages instead.
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
	BaseComponentConfig `json:",inline"`
	HelmComponentConfig `json:",inline"`

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
	// Deprecated: This field is deprecated and will be removed with v26.4.0.
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

// +kubebuilder:validation:XValidation:rule="!has(self.image) || !has(self.cni) || !has(self.cni.image)",message="only either 'image' (deprecated) or 'cni.image' can be set, but not both"

type MultusConfiguration struct {
	BaseComponentConfig `json:",inline"`
	HelmComponentConfig `json:",inline"`

	// Image overrides the container image used by the Multus Container.
	//
	// Deprecated: This field is deprecated and will be removed with v26.4.0.
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

// Deprecated: This method is deprecated and will be removed with v26.4.0. Use GetImages instead.
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

// +kubebuilder:validation:XValidation:rule="!has(self.image) || !has(self.controller) || !has(self.controller.image)",message="only either 'image' (deprecated) or 'controller.image' can be set, but not both"

type NVIPAMConfiguration struct {
	BaseComponentConfig `json:",inline"`
	HelmComponentConfig `json:",inline"`

	// Image overrides the container image used by the NVIPAM controller.
	//
	// Deprecated: This field is deprecated and will be removed with v26.4.0.
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
	ImageComponentConfig    `json:",inline"`
	ResourceComponentConfig `json:",inline"`
}

// Deprecated: This method is deprecated and will be removed with v26.4.0. Use GetImages instead.
func (c *NVIPAMConfiguration) GetImage() *string {
	return c.Image
}

func (c *NVIPAMConfiguration) GetImages() map[ContainerName]*string {
	images := make(map[ContainerName]*string)
	if c.Controller != nil {
		images[NVIPAMContainerController] = c.Controller.GetImage()
	}
	if c.Node != nil {
		images[NVIPAMContainerNode] = c.Node.GetImage()
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
	return NVIPAMControllerName.String()
}

// +kubebuilder:validation:XValidation:rule="!has(self.image) || !has(self.deviceplugin) || !has(self.deviceplugin.image)",message="only either 'image' (deprecated) or 'deviceplugin.image' can be set, but not both"

type SRIOVDevicePluginConfiguration struct {
	BaseComponentConfig `json:",inline"`
	HelmComponentConfig `json:",inline"`

	// Image overrides the container image used by the SRIOV Device Plugin container.
	//
	// Deprecated: This field is deprecated and will be removed with v26.4.0.
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

// Deprecated: This method is deprecated and will be removed with v26.4.0. Use GetImages instead.
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

// +kubebuilder:validation:XValidation:rule="!has(self.image) || !has(self.cni) || !has(self.cni.image)",message="only either 'image' (deprecated) or 'cni.image' can be set, but not both"

type OVSCNIConfiguration struct {
	BaseComponentConfig `json:",inline"`
	HelmComponentConfig `json:",inline"`

	// Image overrides the container image used by the OVS CNI.
	//
	// Deprecated: This field is deprecated and will be removed with v26.4.0.
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

// Deprecated: This method is deprecated and will be removed with v26.4.0. Use GetImages instead.
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

// +kubebuilder:validation:XValidation:rule="!has(self.image) || !has(self.controller) || !has(self.controller.image)",message="only either 'image' (deprecated) or 'controller.image' can be set, but not both"

type SFCControllerConfiguration struct {
	BaseComponentConfig `json:",inline"`
	HelmComponentConfig `json:",inline"`

	// Image overrides the container image used by the SFC controller.
	//
	// Deprecated: This field is deprecated and will be removed with v26.4.0.
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

// Deprecated: This method is deprecated and will be removed with v26.4.0. Use GetImages instead.
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
	BaseComponentConfig `json:",inline"`
	HelmComponentConfig `json:",inline"`

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

// NodeSRIOVDevicePluginSettings contains configuration for the SRIOV device plugin pods
// managed by the NodeSRIOVDevicePlugin controller.
type NodeSRIOVDevicePluginSettings struct {
	// Image overrides the container image for the SRIOV device plugin.
	// +optional
	Image Image `json:"image,omitempty"`

	// InitImage overrides the container image for the init container
	// that generates device plugin configuration.
	// +optional
	InitImage Image `json:"initImage,omitempty"`

	// DefaultResourcePrefix is the default resource prefix for the SRIOV device plugin resources.
	// Defaults to "nvidia.com".
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	// +optional
	DefaultResourcePrefix *string `json:"defaultResourcePrefix,omitempty"`
}

// NodeSRIOVDevicePluginControllerConfiguration is the configuration for the NodeSRIOVDevicePlugin controller.
// This controller manages per-node SRIOV device plugin pods based on DPU configurations.
// The controller is disabled by default.
type NodeSRIOVDevicePluginControllerConfiguration struct {
	BaseComponentConfig  `json:",inline"`
	BaseControllerConfig `json:",inline"`

	// Controller contains the configuration for the NodeSRIOVDevicePlugin controller component.
	// It contains the image for the controller and its resource requirements.
	// +optional
	Controller *DefaultOverridesConfiguration `json:"controller,omitempty"`

	// DevicePlugin contains the configuration for the SRIOV device plugin pods
	// managed by this controller.
	// +optional
	DevicePlugin *NodeSRIOVDevicePluginSettings `json:"devicePlugin,omitempty"`
}

func (c *NodeSRIOVDevicePluginControllerConfiguration) Name() string {
	return NodeSRIOVDevicePluginControllerName.String()
}

// GetImages returns a map of container names to their images
func (c *NodeSRIOVDevicePluginControllerConfiguration) GetImages() map[ContainerName]*string {
	images := make(map[ContainerName]*string)
	if c.Controller != nil {
		images[ControllerManagerContainer] = c.Controller.GetImage()
	}
	return images
}

func (c *NodeSRIOVDevicePluginControllerConfiguration) GetResources() map[ContainerName]*corev1.ResourceRequirements {
	if c.Controller == nil {
		return nil
	}
	return map[ContainerName]*corev1.ResourceRequirements{
		ControllerManagerContainer: c.Controller.GetResource(),
	}
}

type KubeStateMetricsConfiguration struct {
	BaseComponentConfig `json:",inline"`
	HelmComponentConfig `json:",inline"`

	// Daemon contains the configuration for the kube-state-metrics component.
	// It contains the image for kube-state-metrics and its resource requirements.
	// +optional
	Daemon *DefaultOverridesConfiguration `json:"daemon,omitempty"`
}

func (c *KubeStateMetricsConfiguration) Name() string {
	return KubeStateMetricsName.String()
}

// GetImages returns a map of container names to their images
func (c *KubeStateMetricsConfiguration) GetImages() map[ContainerName]*string {
	images := make(map[ContainerName]*string)
	if c.Daemon != nil {
		images[KubeStateMetricsContainer] = c.Daemon.GetImage()
	}
	return images
}

func (c *KubeStateMetricsConfiguration) GetResources() map[ContainerName]*corev1.ResourceRequirements {
	if c.Daemon == nil {
		return nil
	}
	return map[ContainerName]*corev1.ResourceRequirements{
		KubeStateMetricsContainer: c.Daemon.GetResource(),
	}
}

type NodeProblemDetectorConfiguration struct {
	BaseComponentConfig `json:",inline"`
	HelmComponentConfig `json:",inline"`

	// Daemon contains the configuration for the node-problem-detector component.
	// It contains the image for node-problem-detector and its resource requirements.
	// +optional
	Daemon *DefaultOverridesConfiguration `json:"daemon,omitempty"`
}

func (c *NodeProblemDetectorConfiguration) Name() string {
	return NodeProblemDetectorName.String()
}

// GetImages returns a map of container names to their images
func (c *NodeProblemDetectorConfiguration) GetImages() map[ContainerName]*string {
	images := make(map[ContainerName]*string)
	if c.Daemon != nil {
		images[NodeProblemDetectorContainer] = c.Daemon.GetImage()
	}
	return images
}

func (c *NodeProblemDetectorConfiguration) GetResources() map[ContainerName]*corev1.ResourceRequirements {
	if c.Daemon == nil {
		return nil
	}
	return map[ContainerName]*corev1.ResourceRequirements{
		NodeProblemDetectorContainer: c.Daemon.GetResource(),
	}
}

type OpenTelemetryCollectorConfiguration struct {
	BaseComponentConfig `json:",inline"`
	HelmComponentConfig `json:",inline"`

	// Daemon contains the configuration for the opentelemetry-collector component.
	// It contains the image for opentelemetry-collector and its resource requirements.
	// +optional
	Daemon *DefaultOverridesConfiguration `json:"daemon,omitempty"`

	// Logging contains the configuration for the opentelemetry-collector logging component.
	// If not specified, logging will not be streamed.
	// +optional
	Logging *OpenTelemetryCollectorLoggingConfiguration `json:"logging,omitempty"`
}

type OpenTelemetryCollectorLoggingConfiguration struct {
	// Endpoint is the OTLP endpoint where the DPU cluster opentelemetry-collector sends data to.
	// This could be the management cluster's opentelemetry-collector endpoint.
	// If not specified, nothing will be forwarded from DPU clusters.
	// +required
	Endpoint string `json:"endpoint,omitempty"`
}

func (c *OpenTelemetryCollectorConfiguration) Name() string {
	return OpenTelemetryCollectorName.String()
}

// GetImages returns a map of container names to their images
func (c *OpenTelemetryCollectorConfiguration) GetImages() map[ContainerName]*string {
	images := make(map[ContainerName]*string)
	if c.Daemon != nil {
		images[OpenTelemetryCollectorContainer] = c.Daemon.GetImage()
	}
	return images
}

func (c *OpenTelemetryCollectorConfiguration) GetResources() map[ContainerName]*corev1.ResourceRequirements {
	if c.Daemon == nil {
		return nil
	}
	return map[ContainerName]*corev1.ResourceRequirements{
		OpenTelemetryCollectorContainer: c.Daemon.GetResource(),
	}
}
