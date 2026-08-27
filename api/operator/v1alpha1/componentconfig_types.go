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

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

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
	BaseComponentConfig  `json:",inline"`
	BaseControllerConfig `json:",inline"`

	// Image overrides the container image used by the Provisioning controller.
	//
	// Deprecated: This field is deprecated and will be removed with v26.7.0.
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

	// OSInstallRetries is the maximum number of retryable OS installation attempts in zero-trust mode
	// before the DPU transitions to Error. Attempts are counted in-process and reset on controller restart.
	// When unset, the provisioning controller defaults to 2.
	// +kubebuilder:validation:Minimum=1
	// +optional
	OSInstallRetries int32 `json:"osInstallRetries,omitempty"`

	// OSInstallTimeout is the maximum time allowed for OS installation in zero-trust mode.
	// If the installation exceeds this timeout, the DPU will transition to an error state.
	// When unset, the provisioning controller defaults to 60m.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern=`^([0-9]+(h|m|s|ms|us|µs|ns))+$`
	// +kubebuilder:validation:Format=duration
	// +optional
	OSInstallTimeout *metav1.Duration `json:"osInstallTimeout,omitempty"`

	// FirmwareUpdateTimeout is the maximum time allowed for BF4 firmware update in zero-trust mode.
	// If the update exceeds this timeout, the DPU will transition to an error state.
	// When unset, the provisioning controller defaults to 45m.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern=`^([0-9]+(h|m|s|ms|us|µs|ns))+$`
	// +kubebuilder:validation:Format=duration
	// +optional
	FirmwareUpdateTimeout *metav1.Duration `json:"firmwareUpdateTimeout,omitempty"`

	// PreInstallAgentRegistrationTimeout is how long Initializing waits for the in-band dpu-agent
	// to set preInstall.agentReported on a recreated DPU CR (reprovision). When the timeout elapses,
	// provisioning continues without agent-assisted pre-install for this cycle.
	// +kubebuilder:default="30s"
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern=`^([0-9]+(h|m|s|ms|us|µs|ns))+$`
	// +kubebuilder:validation:Format=duration
	// +optional
	PreInstallAgentRegistrationTimeout *metav1.Duration `json:"preInstallAgentRegistrationTimeout,omitempty"`

	// NodeEffectRemovalTimeout is the maximum time allowed for the Node Effect Removal phase.
	// If the DPUNodeMaintenance CR still has requestors after this timeout, the DPU will transition to an error state.
	// When unset, the provisioning controller defaults to 0s (timeout disabled).
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

	// BMCServerCertRenewBefore is how long before expiry DPF rotates the DPU BMC mTLS
	// server certificate.
	// When unset, the provisioning controller defaults to 720h (30 days).
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern=`^([0-9]+(h|m|s|ms|us|µs|ns))+$`
	// +kubebuilder:validation:Format=duration
	// +optional
	BMCServerCertRenewBefore *metav1.Duration `json:"bmcServerCertRenewBefore,omitempty"`
}

func (c *ProvisioningControllerConfiguration) Name() string {
	return ProvisioningControllerName.String()
}

// Deprecated: This method is deprecated and will be removed with v26.7.0. Use GetImages instead.
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
	// DiscoveredDPUDeviceBMCFactoryResetPolicy is the BMC factory reset policy DPUDiscovery
	// sets on the DPUDevices it creates. It is applied at creation time only: changing it
	// does not affect DPUDevices that already exist, and it is not consulted when a
	// DPUDevice is reconciled. When unset, the discovery controller uses OnInitialization.
	// +kubebuilder:validation:Enum=OnInitialization;Never
	// +optional
	DiscoveredDPUDeviceBMCFactoryResetPolicy provisioningv1.BMCFactoryResetPolicy `json:"discoveredDPUDeviceBMCFactoryResetPolicy,omitempty"`
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
	// Address is the address used to access the BFB Registry. The address must start with "http://" or "https://".
	// By default, the BFB Registry can be accessed via its Service.
	// For non-kubernetes environments, this must be set due to the lack of kubelet on worker nodes.
	// For zero-trust environments, this must be set so that the BFB Registry can be accessed from DPU BMC.
	// +kubebuilder:validation:Pattern="^https?://"
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
	// +kubebuilder:validation:Pattern="^https?://"
	// +optional
	LoadBalancerAddress *string `json:"loadBalancerAddress,omitempty"`
}

// SPIFFETrustBundleFormat is a SPIRE Agent initial trust bundle format.
type SPIFFETrustBundleFormat string

const (
	SPIFFETrustBundleFormatPEM    SPIFFETrustBundleFormat = "pem"
	SPIFFETrustBundleFormatSPIFFE SPIFFETrustBundleFormat = "spiffe"
)

// SPIFFETrustBundleConfigMapReference references a ConfigMap containing the initial SPIRE trust bundle.
type SPIFFETrustBundleConfigMapReference struct {
	// Name is the name of the ConfigMap holding the SPIRE trust bundle.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +required
	Name string `json:"name,omitempty"`

	// Namespace is the namespace of the ConfigMap holding the SPIRE trust bundle.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +required
	Namespace string `json:"namespace,omitempty"`

	// Format selects data["bundle.pem"] or data["bundle.spiffe"] and the matching SPIRE Agent parser.
	// +kubebuilder:validation:Enum=pem;spiffe
	// +kubebuilder:default=pem
	// +optional
	Format SPIFFETrustBundleFormat `json:"format,omitempty"`
}

// DefaultDPUAgentSPIFFEIDTemplate is the default for both DPU Agent identity templates.
// This default is applied during reconciliation because the generated CRD is shipped as a Helm
// template, which would evaluate a kubebuilder default containing Go template delimiters.
const DefaultDPUAgentSPIFFEIDTemplate = "spiffe://{{ .TrustDomain }}/dpu/{{ .SerialNumber }}/process/dpu-agent"

// DefaultDPUServiceSPIFFEIDTemplate is the default for both DPUService identity templates.
// Applied during reconciliation for the same reason as DefaultDPUAgentSPIFFEIDTemplate.
const DefaultDPUServiceSPIFFEIDTemplate = "spiffe://{{ .TrustDomain }}/dpu/{{ .SerialNumber }}/service/{{ .Namespace }}/{{ .ServiceID }}"

// SPIFFEConfiguration is the per-cluster SPIFFE bootstrap parameter set
//
// +kubebuilder:validation:XValidation:rule="self.spireServerAddress.matches('^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?([.][A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)*:[1-9][0-9]{0,4}$')",message="spireServerAddress must be host:port with a valid DNS-1123 host (e.g. spire-server.spire-system.svc:8081)"
// +kubebuilder:validation:XValidation:rule="!self.spireServerAddress.contains(':') || (int(self.spireServerAddress.split(':')[1]) >= 1 && int(self.spireServerAddress.split(':')[1]) <= 65535)",message="spireServerAddress port must be in 1-65535"
type SPIFFEConfiguration struct {
	// SPIREServerAddress is the address of the pre-installed SPIRE Server in host:port form
	// (e.g. "spire-server.spire-system.svc:8081").
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=263
	// +required
	SPIREServerAddress string `json:"spireServerAddress,omitempty"`

	// SPIRETrustDomain is the SPIRE-internal trust domain (e.g. "cs.internal") embedded in the
	// DPU Agent SVID URI.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	// +required
	SPIRETrustDomain string `json:"spireTrustDomain,omitempty"`

	// DPUAgentSPIFFEIDTemplate renders the local SPIFFE workload identity registered with SPIRE.
	// It uses Go text/template syntax and receives TrustDomain, normalized SerialNumber, DPUMeta,
	// DPUSpec, DPUDeviceMeta, and DPUDeviceSpec. Metadata labels and annotations can be accessed
	// with the built-in index function.
	// The rendered identity must use SPIRETrustDomain and depend on the DPU serial.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +optional
	DPUAgentSPIFFEIDTemplate string `json:"dpuAgentSPIFFEIDTemplate,omitempty"`

	// DPUAgentExchangedSPIFFEIDTemplate renders the post-exchange SPIFFE ID subject. It receives
	// the same Go template data as DPUAgentSPIFFEIDTemplate.
	// The rendered identity may use a different trust domain and must depend on the DPU serial.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +optional
	DPUAgentExchangedSPIFFEIDTemplate string `json:"dpuAgentExchangedSPIFFEIDTemplate,omitempty"`

	// DPUServiceSPIFFEIDTemplate renders the identity registered with SPIRE for a DPUService that
	// opts in through its own spec.security.spiffe. It receives TrustDomain, normalized
	// SerialNumber, Namespace and ServiceID, plus DPUMeta, DPUSpec, DPUServiceMeta and
	// DPUServiceSpec.
	// The rendered identity must use SPIRETrustDomain and depend on the namespace, the service ID
	// and the DPU serial, which together identify one DPUService workload. Dropping any of them
	// hands a single SVID to distinct workloads, and nothing detects that later: SPIRE keys
	// entries on the identity, the parent and the selectors, so two DPUServices differing only in
	// namespace produce two entries carrying the same identity. A label cannot stand in for the
	// namespace, since nothing ties one to the other.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +optional
	DPUServiceSPIFFEIDTemplate string `json:"dpuServiceSPIFFEIDTemplate,omitempty"`

	// DPUServiceExchangedSPIFFEIDTemplate renders the post-exchange DPUService subject. It
	// receives the same template data as DPUServiceSPIFFEIDTemplate and may use a different trust
	// domain.
	// DPF renders and validates it so the identity layout is declared in one place, but does not
	// consume it: unlike the DPU Agent, a DPUService identity is never presented back to DPF.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +optional
	DPUServiceExchangedSPIFFEIDTemplate string `json:"dpuServiceExchangedSPIFFEIDTemplate,omitempty"`

	// KubeAPIAudience is the audience claim the DPU Agent's JWT-SVID must carry; it must match an
	// entry in the kube-apiserver AuthenticationConfiguration.audiences[] (owned out-of-band).
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	// +required
	KubeAPIAudience string `json:"kubeAPIAudience,omitempty"`

	// tokenExchangeEndpoint exchanges the SPIRE JWT-SVID before the DSX SPIFFE Helper writes it.
	// When omitted, the DSX SPIFFE Helper writes the SPIRE JWT-SVID directly.
	// The returned token's audience must match kubeAPIAudience, or the kube-apiserver rejects the
	// DPU Agent. Only https: the JWT-SVID is sent here as a bearer credential.
	// +kubebuilder:validation:Pattern=`^https://[^[:space:]"\\]+$`
	// +kubebuilder:validation:MaxLength=2048
	// +optional
	TokenExchangeEndpoint *string `json:"tokenExchangeEndpoint,omitempty"`

	// SPIREOIDCURL is the OIDC discovery (issuer) URL of the pre-installed SPIRE Server.
	// The matching kube-apiserver AuthenticationConfiguration.jwt[].issuer value is applied out-of-band.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +required
	SPIREOIDCURL string `json:"spireOIDCURL,omitempty"`

	// spireControllerManagerClassName selects the SPIRE controller-manager instance that renders
	// DPF ClusterStaticEntries.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +required
	SPIREControllerManagerClassName string `json:"spireControllerManagerClassName,omitempty"`

	// trustBundle references a ConfigMap holding the initial SPIRE trust bundle.
	// +required
	TrustBundle SPIFFETrustBundleConfigMapReference `json:"trustBundle,omitzero"`
}

// +kubebuilder:validation:XValidation:rule="!has(self.image) || !has(self.controller) || !has(self.controller.image)",message="only either 'image' (deprecated) or 'controller.image' can be set, but not both"

type DPUServiceControllerConfiguration struct {
	BaseComponentConfig  `json:",inline"`
	BaseControllerConfig `json:",inline"`

	// Image overrides the container image used by the DPUService controller.
	//
	// Deprecated: This field is deprecated and will be removed with v26.7.0.
	// Use the new field `controller` instead.
	// +optional
	Image Image `json:"image,omitempty"`

	// Controller contains the configuration for the DPU Service controller component.
	// It contains the image for the controller and its resource requirements.
	// +optional
	Controller *DefaultOverridesConfiguration `json:"controller,omitempty"`

	// DisableDPUReadyTaints is a full taint kill-switch for the DPUReady controller.
	// When set to true, no taint managed by this controller (NoSchedule for critical
	// DPUServices, or NoExecute for HostNetworkReady) is added, removed, or otherwise
	// touched on host worker nodes.
	// +optional
	DisableDPUReadyTaints *bool `json:"disableDPUReadyTaints,omitempty"`

	// DisableHostNetworkReadyNoExecuteTaints disables NoExecute taints on host worker nodes
	// based on HostNetworkReady. When unset or true, the feature is disabled (safe default).
	// Set to false to enable NoExecute tainting when HostNetworkReady != True.
	// +optional
	DisableHostNetworkReadyNoExecuteTaints *bool `json:"disableHostNetworkReadyNoExecuteTaints,omitempty"`
}

func (c *DPUServiceControllerConfiguration) Name() string {
	return DPUServiceControllerName.String()
}

// Deprecated: This method is deprecated and will be removed with v26.7.0. Use GetImages instead.
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
	// Deprecated: This field is deprecated and will be removed with v26.7.0.
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

// Deprecated: This method is deprecated and will be removed with v26.7.0. Use GetImages instead.
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
	BaseComponentConfig  `json:",inline"`
	BaseControllerConfig `json:",inline"`

	// Image overrides the container image used by the Kamaji Cluster Manager.
	//
	// Deprecated: This field is deprecated and will be removed with v26.7.0.
	// Use the new field `controller` instead.
	// +optional
	Image Image `json:"image,omitempty"`

	// Controller contains the configuration for the Kamaji Cluster Manager component.
	// It contains the image for the controller and its resource requirements.
	// +optional
	Controller *DefaultOverridesConfiguration `json:"controller,omitempty"`

	// EtcdEncryptionAtRest configures encryption at rest for the etcd datastore of
	// Kamaji-managed DPU clusters. The provider selection is applied only when a
	// Kamaji cluster is first created and is not changed for existing clusters.
	// +optional
	EtcdEncryptionAtRest *EtcdEncryptionAtRestConfiguration `json:"etcdEncryptionAtRest,omitempty"`
}

func (c *KamajiClusterManagerConfiguration) Name() string {
	return KamajiClusterManagerName.String()
}

// Deprecated: This method is deprecated and will be removed with v26.7.0. Use GetImages instead.
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
	// Deprecated: This field is deprecated and will be removed with v26.7.0.
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

// Deprecated: This method is deprecated and will be removed with v26.7.0. Use GetImages instead.
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
	// Deprecated: This field is deprecated and will be removed with v26.7.0.
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

// Deprecated: This method is deprecated and will be removed with v26.7.0. Use GetImages instead.
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
	// Deprecated: This field is deprecated and will be removed with v26.7.0.
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
	// Deprecated: This field is deprecated and will be removed with v26.7.0.
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

// Deprecated: This method is deprecated and will be removed with v26.7.0. Use GetImages instead.
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
	BaseComponentConfig  `json:",inline"`
	HelmComponentConfig  `json:",inline"`
	BaseControllerConfig `json:",inline"`

	// Image overrides the container image used by the NVIPAM controller.
	//
	// Deprecated: This field is deprecated and will be removed with v26.7.0.
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

// Deprecated: This method is deprecated and will be removed with v26.7.0. Use GetImages instead.
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
	// Deprecated: This field is deprecated and will be removed with v26.7.0.
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

// Deprecated: This method is deprecated and will be removed with v26.7.0. Use GetImages instead.
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
	// Deprecated: This field is deprecated and will be removed with v26.7.0.
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

// Deprecated: This method is deprecated and will be removed with v26.7.0. Use GetImages instead.
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

// SFCControllerConfiguration intentionally does not embed BaseControllerConfig:
// HA is achieved via per-node sharding (DaemonSet + node-local cache + per-node
// reconcilers); each pod exclusively owns its node's state, which makes leader
// election unnecessary.
type SFCControllerConfiguration struct {
	BaseComponentConfig `json:",inline"`
	HelmComponentConfig `json:",inline"`

	// Image overrides the container image used by the SFC controller.
	//
	// Deprecated: This field is deprecated and will be removed with v26.7.0.
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

// Deprecated: This method is deprecated and will be removed with v26.7.0. Use GetImages instead.
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

// CoreDNSConfiguration is the configuration for CoreDNS serving DPU clusters.
type CoreDNSConfiguration struct {
	BaseComponentConfig `json:",inline"`
	HelmComponentConfig `json:",inline"`

	// Deployment contains the configuration for the CoreDNS deployment.
	// It contains the image for the CoreDNS container and its resource requirements.
	// +optional
	Deployment *CoreDNSDeployment `json:"deployment,omitempty"`

	// UpstreamNameservers is passed to the CoreDNS forward plugin for names outside the cluster domain.
	// It accepts the same space-separated nameserver or resolv.conf path syntax as the CoreDNS chart.
	// +kubebuilder:validation:MinLength=1
	// +optional
	UpstreamNameservers string `json:"upstreamNameservers,omitempty"`
}

// CoreDNSDeployment contains the configuration for the CoreDNS deployment container.
type CoreDNSDeployment struct {
	ImageComponentConfig    `json:",inline"`
	ResourceComponentConfig `json:",inline"`
}

func (c *CoreDNSConfiguration) Name() string {
	return CoreDNSName.String()
}

// GetImages returns a map of container names to their images.
func (c *CoreDNSConfiguration) GetImages() map[ContainerName]*string {
	if c.Deployment == nil {
		return nil
	}
	return map[ContainerName]*string{
		CoreDNSContainer: c.Deployment.GetImage(),
	}
}

// GetResources returns a map of container names to their resource requirements.
func (c *CoreDNSConfiguration) GetResources() map[ContainerName]*corev1.ResourceRequirements {
	if c.Deployment == nil {
		return nil
	}
	return map[ContainerName]*corev1.ResourceRequirements{
		CoreDNSContainer: c.Deployment.GetResource(),
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

	// Metrics contains the configuration for the opentelemetry-collector metrics component.
	// If not specified, metrics will not be streamed from DPU clusters.
	// +optional
	Metrics *OpenTelemetryCollectorMetricsConfiguration `json:"metrics,omitempty"`
}

// OpenTelemetryCollectorCASecretKey is the default Secret data key that holds the PEM-encoded CA
// certificate bundle referenced by a CASecretRef when the reference does not specify a key.
const OpenTelemetryCollectorCASecretKey = "ca.crt"

// OpenTelemetryCollectorCASecretReference references a Secret that contains the PEM-encoded CA
// certificate bundle used to verify the endpoint's TLS certificate.
type OpenTelemetryCollectorCASecretReference struct {
	// Name is the name of the Secret holding the CA certificate bundle.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +required
	Name string `json:"name,omitempty"`

	// Namespace is the namespace of the Secret holding the CA certificate bundle.
	// If unset, the DPFOperatorConfig namespace is used.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +optional
	Namespace *string `json:"namespace,omitempty"`

	// Key is the Secret data key that holds the PEM-encoded CA certificate bundle.
	// If unset, "ca.crt" is used, matching the key that cert-manager writes.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +optional
	Key *string `json:"key,omitempty"`
}

// OpenTelemetryCollectorTransport is the OTLP transport used to export data to the endpoint.
type OpenTelemetryCollectorTransport string

const (
	// OpenTelemetryCollectorTransportHTTP exports data via OTLP/HTTP.
	OpenTelemetryCollectorTransportHTTP OpenTelemetryCollectorTransport = "http"
	// OpenTelemetryCollectorTransportGRPC exports data via OTLP/gRPC.
	OpenTelemetryCollectorTransportGRPC OpenTelemetryCollectorTransport = "grpc"
)

// OpenTelemetryCollectorLoggingConfiguration configures where and how the DPU cluster
// opentelemetry-collector exports its data.
// +kubebuilder:validation:XValidation:rule="!has(self.caSecretRef) || !self.endpoint.startsWith('http://')",message="caSecretRef cannot be used with a plaintext http:// endpoint"
type OpenTelemetryCollectorLoggingConfiguration struct {
	// Endpoint is the OTLP endpoint where the DPU cluster opentelemetry-collector sends data to.
	// This could be the management cluster's opentelemetry-collector endpoint.
	// If not specified, nothing will be forwarded from DPU clusters.
	// For the http transport the endpoint must include the scheme, e.g. "https://host:4318".
	// For the grpc transport the endpoint is "host:4317", optionally prefixed with a
	// scheme ("https://host:4317") to enforce TLS.
	// +required
	Endpoint string `json:"endpoint,omitempty"`

	// Transport is the OTLP transport used to export data to the endpoint.
	// +kubebuilder:validation:Enum=http;grpc
	// +optional
	Transport *OpenTelemetryCollectorTransport `json:"transport,omitempty"`

	// CASecretRef references a Secret that contains the PEM-encoded CA certificate bundle
	// (under the "ca.crt" key) used to verify the endpoint's TLS certificate. Set it when the
	// endpoint serves a certificate issued by a private CA. The Secret may live in any namespace,
	// for example alongside the endpoint's cert-manager Certificate; if its namespace is empty,
	// the DPFOperatorConfig namespace is used.
	// If not specified, TLS endpoints are verified against the system CA pool.
	// Changes to the Secret content are applied on the next reconciliation of the DPFOperatorConfig.
	// +optional
	CASecretRef *OpenTelemetryCollectorCASecretReference `json:"caSecretRef,omitempty"`
}

// OpenTelemetryCollectorMetricsConfiguration configures where and how the DPU cluster
// opentelemetry-collector exports its metrics.
// +kubebuilder:validation:XValidation:rule="!has(self.caSecretRef) || !self.endpoint.startsWith('http://')",message="caSecretRef cannot be used with a plaintext http:// endpoint"
type OpenTelemetryCollectorMetricsConfiguration struct {
	// Endpoint is the OTLP endpoint where the DPU cluster opentelemetry-collector sends metrics to.
	// This could be the management cluster's opentelemetry-collector endpoint.
	// If not specified, metrics will not be forwarded from DPU clusters.
	// For the http transport the endpoint must include the scheme, e.g. "https://host:4318".
	// For the grpc transport the endpoint is "host:4317", optionally prefixed with a
	// scheme ("https://host:4317") to enforce TLS.
	// +required
	Endpoint string `json:"endpoint,omitempty"`

	// Transport is the OTLP transport used to export data to the endpoint.
	// +kubebuilder:validation:Enum=http;grpc
	// +optional
	Transport *OpenTelemetryCollectorTransport `json:"transport,omitempty"`

	// CASecretRef references a Secret that contains the PEM-encoded CA certificate bundle
	// (under the "ca.crt" key) used to verify the endpoint's TLS certificate. Set it when the
	// endpoint serves a certificate issued by a private CA. The Secret may live in any namespace,
	// for example alongside the endpoint's cert-manager Certificate; if its namespace is empty,
	// the DPFOperatorConfig namespace is used.
	// If not specified, TLS endpoints are verified against the system CA pool.
	// Changes to the Secret content are applied on the next reconciliation of the DPFOperatorConfig.
	// +optional
	CASecretRef *OpenTelemetryCollectorCASecretReference `json:"caSecretRef,omitempty"`
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

// KataShim identifies a Kata hypervisor shim variant.
// Values must match the shim keys in the kata-deploy Helm chart's
// `shims.<name>.enabled` values.
// Only arm64-compatible shims are supported.
// +kubebuilder:validation:Enum=qemu
type KataShim string

const (
	// KataShimQEMU is the QEMU hypervisor shim.
	KataShimQEMU KataShim = "qemu"
)

type KataContainersConfiguration struct {
	BaseComponentConfig `json:",inline"`
	HelmComponentConfig `json:",inline"`

	// Daemon contains the configuration for the kata-deploy component.
	// It contains the image for the kata-deploy container.
	// +optional
	Daemon *ImageComponentConfig `json:"daemon,omitempty"`

	// NodeSelector restricts which nodes kata-deploy runs on.
	// This is passed as the Helm chart's nodeSelector value.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Shims selects which Kata hypervisor shims to enable.
	// Defaults to ["qemu"] if empty.
	// +optional
	// +kubebuilder:validation:items:Enum=qemu
	Shims []KataShim `json:"shims,omitempty"`

	// ContainerdConfigFileName overrides the containerd config file name
	// on the target nodes. Defaults to "config-mlnx.toml".
	// +optional
	ContainerdConfigFileName string `json:"containerdConfigFileName,omitempty"`
}

func (c *KataContainersConfiguration) Name() string {
	return KataContainersName.String()
}

func (c *KataContainersConfiguration) GetImages() map[ContainerName]*string {
	images := make(map[ContainerName]*string)
	if c.Daemon != nil {
		images[KataDeployContainer] = c.Daemon.GetImage()
	}
	return images
}

// SecretKeyRef selects a single key from a Secret living in the same namespace as the DPFOperatorConfig.
type SecretKeyRef struct {
	// Name is the name of the Secret.
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name,omitempty"`

	// Key is the key within the Secret data to select.
	// +kubebuilder:validation:MinLength=1
	// +required
	Key string `json:"key,omitempty"`
}

// ConfigMapKeyRef selects a single key from a ConfigMap living in the same namespace as the DPFOperatorConfig.
type ConfigMapKeyRef struct {
	// Name is the name of the ConfigMap.
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name,omitempty"`

	// Key is the key within the ConfigMap data to select.
	// +kubebuilder:validation:MinLength=1
	// +required
	Key string `json:"key,omitempty"`
}

// EtcdEncryptionAtRestProvider selects the etcd encryption-at-rest provider.
// +kubebuilder:validation:Enum=staticKey;vaultKMS
type EtcdEncryptionAtRestProvider string

const (
	// EtcdEncryptionProviderStaticKey encrypts etcd data with an AES-GCM key rendered inline into the encryption config.
	EtcdEncryptionProviderStaticKey EtcdEncryptionAtRestProvider = "staticKey"
	// EtcdEncryptionProviderVaultKMS encrypts etcd data via the KMS v2 plugin served by the vaultKMS component.
	EtcdEncryptionProviderVaultKMS EtcdEncryptionAtRestProvider = "vaultKMS"
)

// EtcdEncryptionAtRestConfiguration is the per-cluster encryption-at-rest selector for Kamaji clusters.
// +kubebuilder:validation:XValidation:rule="self.provider != 'staticKey' || has(self.staticKey)",message="staticKey is required when provider is staticKey"
// +kubebuilder:validation:XValidation:rule="self.provider != 'vaultKMS' || !has(self.staticKey)",message="staticKey must not be set when provider is vaultKMS"
type EtcdEncryptionAtRestConfiguration struct {
	// Provider selects the encryption-at-rest provider.
	// +required
	Provider EtcdEncryptionAtRestProvider `json:"provider,omitempty"`

	// StaticKey configures the staticKey provider. It is required when provider is staticKey and
	// must not be set otherwise.
	// +optional
	StaticKey *StaticKeyConfiguration `json:"staticKey,omitempty"`
}

// StaticKeyConfiguration configures the staticKey encryption-at-rest provider.
type StaticKeyConfiguration struct {
	// KeySecretRef selects the AES-GCM key from a Secret in the DPFOperatorConfig namespace.
	// The referenced Secret value must be base64-encoded AES key text whose decoded length is 16,
	// 24, or 32 bytes. For Kubernetes manifests, use stringData.key with the output of
	// `openssl rand -base64 32`. For External Secrets, configure the external value or template so
	// the resulting Kubernetes Secret data decodes to that base64 text, not to raw key bytes.
	// The referenced key is used as the desired static key source. Changing the referenced
	// Secret value triggers automatic rotation for existing staticKey-encrypted Kamaji clusters.
	// The per-cluster rendered encryption configuration must be backed up together with the
	// cluster etcd backup because Kubernetes encrypted data references encryption config key names.
	// +required
	KeySecretRef SecretKeyRef `json:"keySecretRef,omitzero"`

	// AutomaticRotationDisabled disables automatic staticKey rotation for existing Kamaji clusters.
	// In-flight rotations stop at the next stable checkpoint; encryption at rest remains enabled.
	// +optional
	AutomaticRotationDisabled *bool `json:"automaticRotationDisabled,omitempty"`
}

// VaultKMSAuthMethod selects the Vault/OpenBao auth method used by the KMS plugin.
// +kubebuilder:validation:Enum=token;approle;userpass;kubernetes;jwt
type VaultKMSAuthMethod string

const (
	// VaultKMSAuthMethodToken authenticates using a Vault token.
	VaultKMSAuthMethodToken VaultKMSAuthMethod = "token"
	// VaultKMSAuthMethodAppRole authenticates using the AppRole auth method.
	VaultKMSAuthMethodAppRole VaultKMSAuthMethod = "approle"
	// VaultKMSAuthMethodUserpass authenticates using the userpass auth method.
	VaultKMSAuthMethodUserpass VaultKMSAuthMethod = "userpass"
	// VaultKMSAuthMethodKubernetes authenticates using the Kubernetes auth method.
	VaultKMSAuthMethodKubernetes VaultKMSAuthMethod = "kubernetes"
	// VaultKMSAuthMethodJWT authenticates using the JWT auth method.
	VaultKMSAuthMethodJWT VaultKMSAuthMethod = "jwt"
)

// VaultKMSConfiguration configures the standalone Vault/OpenBao KMS plugin component.
// The component is deployed as a DaemonSet on control-plane nodes and is disabled by default.
// The plugin is used for encryption at rest for DPUClusters.
type VaultKMSConfiguration struct {
	BaseComponentConfig `json:",inline"`

	// Daemon contains the image and resource overrides for the KMS plugin DaemonSet.
	// +optional
	Daemon *DefaultOverridesConfiguration `json:"daemon,omitempty"`

	// TLS configures TLS settings used to connect to Vault/OpenBao.
	// +optional
	TLS *VaultKMSTLS `json:"tls,omitempty"`

	// Auth configures how the plugin authenticates to Vault/OpenBao.
	// +required
	Auth VaultKMSAuth `json:"auth,omitzero"`

	// TokenCheckIntervalSeconds optionally overrides how often the plugin checks and renews the current Vault token, in seconds.
	// This is an advanced setting. The plugin default should work for most environments.
	// Must be at least 5 seconds.
	// +kubebuilder:validation:Minimum=5
	// +optional
	TokenCheckIntervalSeconds *int32 `json:"tokenCheckIntervalSeconds,omitempty"`

	// LoginTimeoutSeconds optionally overrides the maximum time for one Vault token check cycle, including authentication, in seconds.
	// This is an advanced setting. The plugin default should work for most environments.
	// Must be at least 1 second.
	// +kubebuilder:validation:Minimum=1
	// +optional
	LoginTimeoutSeconds *int32 `json:"loginTimeoutSeconds,omitempty"`

	// Address is the Vault/OpenBao server address.
	// WARNING: Changing this field does not automatically rotate the encryption key or
	// re-encrypt existing DPU cluster secrets. Do not change it while active DPU clusters
	// depend on this KMS plugin unless the new endpoint provides access to the key material
	// used by the previous endpoint. Otherwise, those clusters will be unable to decrypt
	// their existing secrets, causing an outage.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:Pattern=`^https://.+$`
	// +required
	Address string `json:"address,omitempty"`

	// Transit configures the Vault Transit secrets engine used for encrypt/decrypt.
	// WARNING: Changing this field does not automatically rotate the encryption key or
	// re-encrypt existing DPU cluster secrets. Do not change it while active DPU clusters
	// depend on this KMS plugin unless the new Transit configuration provides access to all
	// key material used by the previous configuration. Otherwise, those clusters will be
	// unable to decrypt their existing secrets, causing an outage.
	// +required
	Transit VaultKMSTransit `json:"transit,omitzero"`

	// Namespace optionally configures the Vault/OpenBao namespace used for requests.
	// This is a Vault/OpenBao namespace, not a Kubernetes namespace.
	// WARNING: Changing this field does not automatically rotate the encryption key or
	// re-encrypt existing DPU cluster secrets. Do not change it while active DPU clusters
	// depend on this KMS plugin unless the new namespace provides access to the key material
	// used by the previous namespace. Otherwise, those clusters will be unable to decrypt
	// their existing secrets, causing an outage.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	// +optional
	Namespace *string `json:"namespace,omitempty"`
}

// VaultKMSTLS configures TLS settings for the connection to Vault/OpenBao.
type VaultKMSTLS struct {
	// CACertConfigMapRef selects a CA bundle key from a ConfigMap used to verify the
	// Vault/OpenBao server certificate. It is mounted as a file.
	// +optional
	CACertConfigMapRef *ConfigMapKeyRef `json:"caConfigMapRef,omitempty"`
}

// VaultKMSTransit configures the Vault Transit secrets engine.
type VaultKMSTransit struct {
	// KeyName is the Transit key used for encrypt and decrypt operations.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^\w(([\w-.]+)?\w)?$`
	// +required
	KeyName string `json:"keyName,omitempty"`

	// Mount is the Transit secrets engine mount path. Defaults to "transit".
	// +kubebuilder:default="transit"
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	// +kubebuilder:validation:Pattern=`^/?[^/\s][^\s]*$`
	// +optional
	Mount *string `json:"mount,omitempty"`
}

// VaultKMSAuth configures the Vault/OpenBao auth method. Exactly one auth block matching method must be set.
// +kubebuilder:validation:XValidation:rule="self.method != 'token' || has(self.token)",message="token is required when method is token"
// +kubebuilder:validation:XValidation:rule="self.method == 'token' || !has(self.token)",message="token must only be set when method is token"
// +kubebuilder:validation:XValidation:rule="self.method != 'approle' || has(self.appRole)",message="appRole is required when method is approle"
// +kubebuilder:validation:XValidation:rule="self.method == 'approle' || !has(self.appRole)",message="appRole must only be set when method is approle"
// +kubebuilder:validation:XValidation:rule="self.method != 'userpass' || has(self.userpass)",message="userpass is required when method is userpass"
// +kubebuilder:validation:XValidation:rule="self.method == 'userpass' || !has(self.userpass)",message="userpass must only be set when method is userpass"
// +kubebuilder:validation:XValidation:rule="self.method != 'kubernetes' || has(self.kubernetes)",message="kubernetes is required when method is kubernetes"
// +kubebuilder:validation:XValidation:rule="self.method == 'kubernetes' || !has(self.kubernetes)",message="kubernetes must only be set when method is kubernetes"
// +kubebuilder:validation:XValidation:rule="self.method != 'jwt' || has(self.jwt)",message="jwt is required when method is jwt"
// +kubebuilder:validation:XValidation:rule="self.method == 'jwt' || !has(self.jwt)",message="jwt must only be set when method is jwt"
type VaultKMSAuth struct {
	// Method selects the Vault auth method.
	// +required
	Method VaultKMSAuthMethod `json:"method,omitempty"`

	// Token configures token auth.
	// +optional
	Token *VaultKMSTokenAuth `json:"token,omitempty"`

	// AppRole configures AppRole auth.
	// +optional
	AppRole *VaultKMSAppRoleAuth `json:"appRole,omitempty"`

	// Userpass configures userpass auth.
	// +optional
	Userpass *VaultKMSUserpassAuth `json:"userpass,omitempty"`

	// Kubernetes configures Kubernetes auth.
	// +optional
	Kubernetes *VaultKMSKubernetesAuth `json:"kubernetes,omitempty"`

	// JWT configures JWT auth.
	// +optional
	JWT *VaultKMSJWTAuth `json:"jwt,omitempty"`
}

// VaultKMSTokenAuth configures the token auth method.
type VaultKMSTokenAuth struct {
	// TokenSecretRef selects the Vault token from a Secret in the DPFOperatorConfig namespace.
	// +required
	TokenSecretRef SecretKeyRef `json:"tokenSecretRef,omitzero"`
}

// VaultKMSAppRoleAuth configures the AppRole auth method using a single merged Secret.
type VaultKMSAppRoleAuth struct {
	// SecretName is the name of the Secret holding the AppRole role ID and secret ID.
	// +kubebuilder:validation:MinLength=1
	// +required
	SecretName string `json:"secretName,omitempty"`

	// AuthEngineMountPath optionally overrides the Vault auth engine mount path. It is not the transit mount.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	// +optional
	AuthEngineMountPath *string `json:"authEngineMountPath,omitempty"`

	// RoleIDKey is the Secret data key holding the AppRole role ID.
	// +kubebuilder:validation:MinLength=1
	// +required
	RoleIDKey string `json:"roleIDKey,omitempty"`

	// SecretIDKey is the Secret data key holding the AppRole secret ID.
	// +kubebuilder:validation:MinLength=1
	// +required
	SecretIDKey string `json:"secretIDKey,omitempty"`
}

// VaultKMSUserpassAuth configures the userpass auth method using a single merged Secret.
type VaultKMSUserpassAuth struct {
	// SecretName is the name of the Secret holding the username and password.
	// +kubebuilder:validation:MinLength=1
	// +required
	SecretName string `json:"secretName,omitempty"`

	// AuthEngineMountPath optionally overrides the Vault auth engine mount path. It is not the transit mount.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	// +optional
	AuthEngineMountPath *string `json:"authEngineMountPath,omitempty"`

	// UsernameKey is the Secret data key holding the username.
	// +kubebuilder:validation:MinLength=1
	// +required
	UsernameKey string `json:"usernameKey,omitempty"`

	// PasswordKey is the Secret data key holding the password.
	// +kubebuilder:validation:MinLength=1
	// +required
	PasswordKey string `json:"passwordKey,omitempty"`
}

// VaultKMSKubernetesAuth configures the Kubernetes auth method.
type VaultKMSKubernetesAuth struct {
	// Role is the Vault Kubernetes auth role name (not a Kubernetes RBAC role).
	// +kubebuilder:validation:MinLength=1
	// +required
	Role string `json:"role,omitempty"`

	// Audience optionally sets the audience for the projected Kubernetes service account token.
	// Use this when the Vault Kubernetes auth role is configured with bound audiences.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	// +optional
	Audience *string `json:"audience,omitempty"`

	// AuthEngineMountPath optionally overrides the Vault auth engine mount path. It is not the transit mount.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	// +optional
	AuthEngineMountPath *string `json:"authEngineMountPath,omitempty"`
}

// VaultKMSJWTAuth configures the JWT auth method.
type VaultKMSJWTAuth struct {
	// Role is the Vault JWT auth role name.
	// +kubebuilder:validation:MinLength=1
	// +required
	Role string `json:"role,omitempty"`

	// JWTSecretRef selects the JWT presented to Vault from a Secret in the DPFOperatorConfig namespace.
	// +required
	JWTSecretRef SecretKeyRef `json:"jwtSecretRef,omitzero"`

	// AuthEngineMountPath optionally overrides the Vault auth engine mount path. It is not the transit mount.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	// +optional
	AuthEngineMountPath *string `json:"authEngineMountPath,omitempty"`
}

func (c *VaultKMSConfiguration) Name() string {
	return VaultKMSName.String()
}

// GetImages returns a map of container names to their images.
func (c *VaultKMSConfiguration) GetImages() map[ContainerName]*string {
	images := make(map[ContainerName]*string)
	if c.Daemon != nil {
		images[VaultKMSContainer] = c.Daemon.GetImage()
	}
	return images
}

// GetResources returns a map of container names to their resource requirements.
func (c *VaultKMSConfiguration) GetResources() map[ContainerName]*corev1.ResourceRequirements {
	if c.Daemon == nil {
		return nil
	}
	return map[ContainerName]*corev1.ResourceRequirements{
		VaultKMSContainer: c.Daemon.GetResource(),
	}
}
