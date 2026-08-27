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

package inventory

import (
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/release"
	"github.com/nvidia/doca-platform/pkg/dpucluster"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// DefaultFlannelPodCIDR is the range DPU cluster Pods get their addresses from. Flannel allocates
// out of it and the DPU cluster control plane hands node podCIDRs out of it, so the two have to
// agree, which is why the cluster manager builds TenantControlPlanes with this same range.
const DefaultFlannelPodCIDR = "10.244.0.0/14"

func newDefaultVariables(defaults *release.Defaults) Variables {
	return Variables{
		DPUClusters:                            []*dpucluster.Config{},
		DPUCNIBinPath:                          "/opt/cni/bin",
		DPUCNIConfPath:                         "/etc/cni/net.d/",
		DPUOpenvSwitchRunPath:                  "/var/run/openvswitch/",
		DPUOpenvSwitchBinPath:                  "/usr/bin/",
		DPUOpenvSwitchSharedLibPath:            "/lib",
		DPUOpenvSwitchSharedLib64Path:          nil, // Default to nil - only mount when explicitly configured
		FlannelSkipCNIConfigInstallation:       true,
		FlannelPodCIDR:                         DefaultFlannelPodCIDR,
		DisableHostNetworkReadyNoExecuteTaints: true, // opt-in: disabled until explicitly set to false
		DisableSystemComponents: map[operatorv1.ComponentName]bool{
			operatorv1.ProvisioningControllerName: false,
			operatorv1.DPUServiceControllerName:   false,
			operatorv1.ServiceSetControllerName:   false,
			operatorv1.FlannelName:                false,
			operatorv1.MultusName:                 false,
			operatorv1.SRIOVDevicePluginName:      false,
			operatorv1.NVIPAMControllerName:       false,
			operatorv1.SFCControllerName:          false,
			operatorv1.DPUDetectorName:            false,
			operatorv1.KamajiClusterManagerName:   false,
			operatorv1.CNIInstallerName:           false,
			operatorv1.KubeStateMetricsName:       false,
			operatorv1.DPUMonitoringName:          false,
			operatorv1.NodeProblemDetectorName:    false,
			operatorv1.OpenTelemetryCollectorName: true, // Disabled by default, requires endpoint configuration
			operatorv1.SPIFFECSIDriverName:        true, // Disabled by default (opt-in).

			// Static cluster manager is disabled by default.
			operatorv1.StaticClusterManagerName: true,
			// NodeSRIOVDevicePluginController is disabled by default.
			operatorv1.NodeSRIOVDevicePluginControllerName: true,
			// KataContainers is disabled by default (opt-in).
			operatorv1.KataContainersName: true,
			// VaultKMS is disabled by default (opt-in).
			operatorv1.VaultKMSName: true,
			// SpireAgentRBAC is disabled unless the DPFOperatorConfig opts into SPIFFE.
			// setAdditionalConfigs is the only thing that flips it, there is no component
			// config for it, so it cannot be toggled independently of the SPIFFE gate.
			operatorv1.SpireAgentRBACName: true,
			// CoreDNS replaces the Kamaji CoreDNS addon, so it is enabled by default. It is only
			// generated for DPUClusters that can reach it, see IsDPUClusterServedByHostDNS.
			operatorv1.CoreDNSName: false,
		},
		Images: map[string]string{
			// Images built as part of the DPF Operator release.
			operatorv1.ProvisioningControllerName.WithContainer(operatorv1.ControllerManagerContainer):          defaults.DPFSystemImage,
			operatorv1.DPUServiceControllerName.WithContainer(operatorv1.ControllerManagerContainer):            defaults.DPFSystemImage,
			operatorv1.StaticClusterManagerName.WithContainer(operatorv1.ControllerManagerContainer):            defaults.DPFSystemImage,
			operatorv1.KamajiClusterManagerName.WithContainer(operatorv1.ControllerManagerContainer):            defaults.DPFSystemImage,
			operatorv1.ServiceSetControllerName.WithContainer(operatorv1.ControllerManagerContainer):            defaults.DPFSystemImage,
			operatorv1.SFCControllerName.WithContainer(operatorv1.ControllerManagerContainer):                   defaults.DPFSystemImage,
			operatorv1.DPUDetectorName.WithContainer(operatorv1.DPUDetectorContainer):                           defaults.DPFSystemImage,
			operatorv1.CNIInstallerName.WithContainer(operatorv1.CNIInstallerContainer):                         defaults.CNIInstallerImage,
			operatorv1.NodeSRIOVDevicePluginControllerName.WithContainer(operatorv1.ControllerManagerContainer): defaults.DPFSystemImage,
			operatorv1.KataContainersName.WithContainer(operatorv1.KataDeployContainer):                         defaults.KataDeployImage,
			operatorv1.VaultKMSName.WithContainer(operatorv1.VaultKMSContainer):                                 defaults.DPFSystemImage,
			// BFBRegistry is not configurable via the DPFOperatorConfig, thus it does not need to have the container name included.
			operatorv1.BFBRegistryName.String(): defaults.BFBRegistryImage,
		},
		HelmCharts: map[operatorv1.ComponentName]string{
			operatorv1.FlannelName:                defaults.DPUNetworkingHelmChart,
			operatorv1.MultusName:                 defaults.DPUNetworkingHelmChart,
			operatorv1.SRIOVDevicePluginName:      defaults.DPUNetworkingHelmChart,
			operatorv1.NVIPAMControllerName:       defaults.DPUNetworkingHelmChart,
			operatorv1.SFCControllerName:          defaults.DPUNetworkingHelmChart,
			operatorv1.ServiceSetControllerName:   defaults.DPUNetworkingHelmChart,
			operatorv1.CNIInstallerName:           defaults.DPUNetworkingHelmChart,
			operatorv1.KubeStateMetricsName:       defaults.DPUNetworkingHelmChart,
			operatorv1.DPUMonitoringName:          defaults.DPUNetworkingHelmChart,
			operatorv1.NodeProblemDetectorName:    defaults.DPUNetworkingHelmChart,
			operatorv1.OpenTelemetryCollectorName: defaults.DPUNetworkingHelmChart,
			operatorv1.KataContainersName:         defaults.DPUNetworkingHelmChart,
			operatorv1.SpireAgentRBACName:         defaults.DPUNetworkingHelmChart,
			operatorv1.CoreDNSName:                defaults.DPUNetworkingHelmChart,
			operatorv1.SPIFFECSIDriverName:        defaults.DPUNetworkingHelmChart,
		},
		SFCController: SFCControllerVariables{
			SecureFlowDeletionTimeout: 0 * time.Second,
		},
		NodeSRIOVDevicePluginController: NodeSRIOVDevicePluginControllerVariables{
			DevicePluginImage:     defaults.NodeSRIOVDevicePluginImage,
			DevicePluginInitImage: defaults.DPFSystemImage,
			DefaultResourcePrefix: "nvidia.com",
		},
		DPFProvisioningController: DPFProvisioningVariables{
			DeploymentMode: operatorv1.DeploymentModeHostTrusted,
		},
		Resources: map[string]corev1.ResourceRequirements{},
		Replicas: map[operatorv1.ComponentName]*int32{
			operatorv1.ProvisioningControllerName: ptr.To[int32](2),
			operatorv1.KamajiClusterManagerName:   ptr.To[int32](2),
		},
	}
}

// Variables contains information required to generate manifests from the inventory.
type Variables struct {
	Namespace                              string
	DPUCNIBinPath                          string
	DPUCNIConfPath                         string
	DPUOpenvSwitchRunPath                  string
	DPUOpenvSwitchBinPath                  string
	DPUOpenvSwitchSharedLibPath            string
	DPUOpenvSwitchSharedLib64Path          *string
	DPULinkerCachePath                     *string
	DPUOptLibraryPath                      *string
	FlannelSkipCNIConfigInstallation       bool
	DisableDPUReadyTaints                  bool
	DisableHostNetworkReadyNoExecuteTaints bool
	FlannelPodCIDR                         string
	DPUClusters                            []*dpucluster.Config
	DPFProvisioningController              DPFProvisioningVariables
	SFCController                          SFCControllerVariables
	NodeSRIOVDevicePluginController        NodeSRIOVDevicePluginControllerVariables
	OpenTelemetryCollector                 OpenTelemetryCollectorVariables
	CoreDNS                                CoreDNSVariables
	Networking                             Networking
	KataContainers                         KataContainersVariables
	DisableSystemComponents                map[operatorv1.ComponentName]bool
	ImagePullSecrets                       []string
	Images                                 map[string]string
	HelmCharts                             map[operatorv1.ComponentName]string
	KubernetesAPIServerVIP                 *string
	KubernetesAPIServerPort                *int
	Resources                              map[string]corev1.ResourceRequirements
	Replicas                               map[operatorv1.ComponentName]*int32
	ArgoCDNamespace                        string
	VaultKMS                               *operatorv1.VaultKMSConfiguration
	SpiffeEnabled                          bool
}

type DPFProvisioningVariables struct {
	BFBPersistentVolumeClaimName       *string
	DMSTimeout                         *int
	BFCFGTemplateConfig                *string
	CustomCASecretName                 *string
	ProvisioningIssuerCASecretName     *string
	InstallInterface                   *operatorv1.ProvisioningInstallInterface
	MaxDPUParallelInstallations        *int32
	MultiDPUOperationsSyncWaitTime     time.Duration
	MaxUnavailableDPUNodes             *int32
	Registry                           *operatorv1.RegistryConfiguration
	Replicas                           *int32
	OSInstallRetries                   int32
	OSInstallTimeout                   *metav1.Duration
	FirmwareUpdateTimeout              *metav1.Duration
	PreInstallAgentRegistrationTimeout *metav1.Duration
	NodeEffectRemovalTimeout           *metav1.Duration
	HostAgentDNSPolicy                 *corev1.DNSPolicy
	DeploymentMode                     operatorv1.DeploymentMode
}

type SFCControllerVariables struct {
	SecureFlowDeletionTimeout time.Duration
}

type Networking struct {
	ControlPlaneMTU int
	HighSpeedMTU    int
}

// CoreDNSVariables contains values passed to the CoreDNS chart.
type CoreDNSVariables struct {
	UpstreamNameservers string
}

// NodeSRIOVDevicePluginControllerVariables holds variables for the NodeSRIOVDevicePlugin controller.
type NodeSRIOVDevicePluginControllerVariables struct {
	// DevicePluginImage is the container image for the SRIOV device plugin.
	DevicePluginImage string
	// DevicePluginInitImage is the container image for the init container.
	DevicePluginInitImage string
	// DefaultResourcePrefix is the default resource prefix for device plugin resources.
	DefaultResourcePrefix string
}

type OpenTelemetryCollectorVariables struct {
	LoggingEndpoint string
	MetricsEndpoint string
}

// KataContainersVariables holds variables specific to the Kata Containers component.
type KataContainersVariables struct {
	Shims                    []string
	ContainerdConfigFileName string
	NodeSelector             map[string]string
}

func VariablesFromDPFOperatorConfig(defaults *release.Defaults, config *operatorv1.DPFOperatorConfig, dpuClusters []*dpucluster.Config) Variables {
	variables := newDefaultVariables(defaults)
	variables = extractComponentConfigs(variables, config)
	variables = setBasicConfig(variables, config)
	variables = setNetworkingConfig(variables, config)
	variables = setCoreDNSConfig(variables, config)
	variables = setOverrideConfigs(variables, config)
	variables = setAdditionalConfigs(variables, config)
	variables = setMonitoringConfigs(variables, config)
	variables.DPUClusters = append(variables.DPUClusters, dpuClusters...)
	return variables
}

// extractComponentConfigs extracts component-specific configurations from the DPFOperatorConfig
func extractComponentConfigs(variables Variables, config *operatorv1.DPFOperatorConfig) Variables {
	disableComponents := variables.DisableSystemComponents
	images := variables.Images
	helmCharts := variables.HelmCharts
	resources := variables.Resources

	for _, componentConfig := range config.ComponentConfigs() {
		if componentConfig == nil {
			continue
		}

		componentName := operatorv1.ComponentName(componentConfig.Name())
		disableComponents[componentName] = componentConfig.Disabled()

		// Extract helm chart configuration
		if helmConfig, ok := componentConfig.(operatorv1.HelmComponentConfigurable); ok && helmConfig.GetHelmChart() != nil {
			helmCharts[componentName] = *helmConfig.GetHelmChart()
		}

		extraImageConfigs(componentConfig, images, componentName)
		extraResourceConfigs(componentConfig, resources, componentName)
	}

	variables.DisableSystemComponents = disableComponents
	variables.Images = images
	variables.HelmCharts = helmCharts
	return variables
}

func extraImageConfigs(componentConfig operatorv1.ComponentConfigurable, images map[string]string, componentName operatorv1.ComponentName) {
	// nolint:staticcheck
	if imageConfig, ok := componentConfig.(operatorv1.DeprecatedImageComponentConfigurable); ok && imageConfig.GetImage() != nil {
		containerName := getContainerNameFromComponent(componentName)
		if containerName != "" {
			images[componentName.WithContainer(containerName)] = *imageConfig.GetImage()
			return
		}
		// TODO: Remove this special case after the deprecated single image config is removed.
		// Flannel is the only component using the deprecated single image config with multiple containers.
		if componentName == operatorv1.FlannelName {
			images[componentName.String()] = *imageConfig.GetImage()
		}
	}

	if multiImageConfig, ok := componentConfig.(operatorv1.ImageComponentConfigurable); ok {
		containerImages := multiImageConfig.GetImages()
		for containerName, img := range containerImages {
			if img != nil {
				images[componentName.WithContainer(containerName)] = *img
			}
		}
	}
}

func extraResourceConfigs(componentConfig operatorv1.ComponentConfigurable, resources map[string]corev1.ResourceRequirements, componentName operatorv1.ComponentName) {
	if multiResourceConfig, ok := componentConfig.(operatorv1.ResourcesComponentConfigurable); ok {
		containerResources := multiResourceConfig.GetResources()
		for containerName, resourceMap := range containerResources {
			if resourceMap == nil {
				continue
			}
			resources[componentName.WithContainer(containerName)] = *resourceMap
		}
	}
}

// setBasicConfig sets the basic configuration values
func setBasicConfig(variables Variables, config *operatorv1.DPFOperatorConfig) Variables {
	variables.Namespace = config.Namespace
	variables.DPFProvisioningController = DPFProvisioningVariables{
		BFBPersistentVolumeClaimName: config.Spec.ProvisioningController.BFBPersistentVolumeClaimName,
		DMSTimeout:                   config.Spec.ProvisioningController.DMSTimeout,
		//nolint:staticcheck // Intentionally using deprecated field for backward compatibility
		BFCFGTemplateConfig:                config.Spec.ProvisioningController.BFCFGTemplateConfigMap,
		CustomCASecretName:                 config.Spec.ProvisioningController.CustomCASecretName,
		ProvisioningIssuerCASecretName:     nil,
		InstallInterface:                   config.Spec.ProvisioningController.InstallInterface,
		MaxDPUParallelInstallations:        config.Spec.ProvisioningController.MaxDPUParallelInstallations,
		MaxUnavailableDPUNodes:             config.Spec.ProvisioningController.MaxUnavailableDPUNodes,
		Registry:                           config.Spec.ProvisioningController.Registry,
		Replicas:                           config.Spec.ProvisioningController.Replicas,
		OSInstallTimeout:                   config.Spec.ProvisioningController.OSInstallTimeout,
		OSInstallRetries:                   config.Spec.ProvisioningController.OSInstallRetries,
		FirmwareUpdateTimeout:              config.Spec.ProvisioningController.FirmwareUpdateTimeout,
		PreInstallAgentRegistrationTimeout: config.Spec.ProvisioningController.PreInstallAgentRegistrationTimeout,
		NodeEffectRemovalTimeout:           config.Spec.ProvisioningController.NodeEffectRemovalTimeout,
		HostAgentDNSPolicy:                 config.Spec.ProvisioningController.HostAgentDNSPolicy,
		DeploymentMode:                     config.Spec.DeploymentMode,
	}
	if config.Spec.Overrides != nil {
		variables.DPFProvisioningController.ProvisioningIssuerCASecretName = config.Spec.Overrides.ProvisioningIssuerCASecretName
	}
	if config.Spec.ProvisioningController.MultiDPUOperationsSyncWaitTime != nil {
		variables.DPFProvisioningController.MultiDPUOperationsSyncWaitTime = config.Spec.ProvisioningController.MultiDPUOperationsSyncWaitTime.Duration
	}
	variables.ImagePullSecrets = config.Spec.ImagePullSecrets
	variables.ArgoCDNamespace = config.Namespace
	return variables
}

// setNetworkingConfig sets the networking configuration values
func setNetworkingConfig(variables Variables, config *operatorv1.DPFOperatorConfig) Variables {
	if config.Spec.Networking == nil {
		return variables
	}

	if config.Spec.Networking.ControlPlaneMTU != nil {
		variables.Networking.ControlPlaneMTU = *config.Spec.Networking.ControlPlaneMTU
	}
	if config.Spec.Networking.HighSpeedMTU != nil {
		variables.Networking.HighSpeedMTU = *config.Spec.Networking.HighSpeedMTU
	}
	return variables
}

func setCoreDNSConfig(variables Variables, config *operatorv1.DPFOperatorConfig) Variables {
	if config.Spec.CoreDNS != nil {
		variables.CoreDNS.UpstreamNameservers = config.Spec.CoreDNS.UpstreamNameservers
	}
	return variables
}

// setOverrideConfigs sets the override configuration values
func setOverrideConfigs(variables Variables, config *operatorv1.DPFOperatorConfig) Variables {
	if config.Spec.Overrides == nil {
		return variables
	}

	if config.Spec.Overrides.DPUCNIConfigPath != nil {
		variables.DPUCNIConfPath = *config.Spec.Overrides.DPUCNIConfigPath
	}
	if config.Spec.Overrides.DPUCNIBinPath != nil {
		variables.DPUCNIBinPath = *config.Spec.Overrides.DPUCNIBinPath
	}
	if config.Spec.Overrides.DPUOpenvSwitchBinPath != nil {
		variables.DPUOpenvSwitchBinPath = *config.Spec.Overrides.DPUOpenvSwitchBinPath
	}
	if config.Spec.Overrides.DPUOpenvSwitchSystemSharedLibPath != nil {
		variables.DPUOpenvSwitchSharedLibPath = *config.Spec.Overrides.DPUOpenvSwitchSystemSharedLibPath
	}
	if config.Spec.Overrides.FlannelSkipCNIConfigInstallation != nil {
		variables.FlannelSkipCNIConfigInstallation = *config.Spec.Overrides.FlannelSkipCNIConfigInstallation
	}
	if v := config.Spec.Overrides.DPUOpenvSwitchSystemSharedLib64Path; v != nil && *v != "" {
		variables.DPUOpenvSwitchSharedLib64Path = v
	}
	if v := config.Spec.Overrides.DPULinkerCachePath; v != nil && *v != "" {
		variables.DPULinkerCachePath = v
	}
	if v := config.Spec.Overrides.DPUOptLibraryPath; v != nil && *v != "" {
		variables.DPUOptLibraryPath = v
	}
	if config.Spec.Overrides.DPUOpenvSwitchRunPath != nil {
		variables.DPUOpenvSwitchRunPath = *config.Spec.Overrides.DPUOpenvSwitchRunPath
	}
	if config.Spec.Overrides.KubernetesAPIServerVIP != nil {
		variables.KubernetesAPIServerVIP = config.Spec.Overrides.KubernetesAPIServerVIP
	}
	if config.Spec.Overrides.KubernetesAPIServerPort != nil {
		variables.KubernetesAPIServerPort = config.Spec.Overrides.KubernetesAPIServerPort
	}
	variables.ArgoCDNamespace = config.GetArgoCDNamespace()

	return variables
}

// setAdditionalConfigs sets additional configuration values
func setAdditionalConfigs(variables Variables, config *operatorv1.DPFOperatorConfig) Variables {
	if config.Spec.Flannel != nil && config.Spec.Flannel.PodCIDR != nil {
		variables.FlannelPodCIDR = *config.Spec.Flannel.PodCIDR
	}
	if config.Spec.SFCController != nil {
		if config.Spec.SFCController.SecureFlowDeletionTimeout != nil {
			variables.SFCController.SecureFlowDeletionTimeout = config.Spec.SFCController.SecureFlowDeletionTimeout.Duration
		}
	}

	if dsc := config.Spec.DPUServiceController; dsc != nil {
		if dsc.DisableDPUReadyTaints != nil {
			variables.DisableDPUReadyTaints = *dsc.DisableDPUReadyTaints
		}
		if dsc.DisableHostNetworkReadyNoExecuteTaints != nil {
			variables.DisableHostNetworkReadyNoExecuteTaints = *dsc.DisableHostNetworkReadyNoExecuteTaints
		}
		if dsc.Replicas != nil {
			variables.Replicas[operatorv1.DPUServiceControllerName] = dsc.Replicas
		}
	}

	// The SPIRE agent's Kubernetes workload attestor needs nodes/pods in the DPU cluster.
	// Enablement follows the SPIFFE gate rather than a component config, so that opting
	// into SPIFFE cannot leave workload attestation broken by a missing grant.
	spiffeEnabled := util.SpiffeEnabled(config)
	variables.DisableSystemComponents[operatorv1.SpireAgentRBACName] = !spiffeEnabled
	variables.DisableSystemComponents[operatorv1.SPIFFECSIDriverName] = !spiffeEnabled
	variables.SpiffeEnabled = spiffeEnabled

	// Extract replicas for every controller that exposes a Replicas field in the CRD.
	// This is the single propagation point from DPFOperatorConfig.Spec.<Component>.Replicas
	// into the deployment manifests and helm chart values.
	if config.Spec.KamajiClusterManager != nil && config.Spec.KamajiClusterManager.Replicas != nil {
		variables.Replicas[operatorv1.KamajiClusterManagerName] = config.Spec.KamajiClusterManager.Replicas
	}
	if config.Spec.NodeSRIOVDevicePluginController != nil && config.Spec.NodeSRIOVDevicePluginController.Replicas != nil {
		variables.Replicas[operatorv1.NodeSRIOVDevicePluginControllerName] = config.Spec.NodeSRIOVDevicePluginController.Replicas
	}
	if config.Spec.StaticClusterManager != nil && config.Spec.StaticClusterManager.Replicas != nil {
		variables.Replicas[operatorv1.StaticClusterManagerName] = config.Spec.StaticClusterManager.Replicas
	}
	if config.Spec.ServiceSetController != nil && config.Spec.ServiceSetController.Replicas != nil {
		variables.Replicas[operatorv1.ServiceSetControllerName] = config.Spec.ServiceSetController.Replicas
	}
	if config.Spec.NVIPAM != nil && config.Spec.NVIPAM.Replicas != nil {
		variables.Replicas[operatorv1.NVIPAMControllerName] = config.Spec.NVIPAM.Replicas
	}
	// SFCController achieves HA via per-node sharding (DaemonSet + node-local cache):
	// each pod exclusively owns its node's state, which makes leader election unnecessary.

	// Extract NodeSRIOVDevicePluginController configuration
	if config.Spec.NodeSRIOVDevicePluginController != nil && config.Spec.NodeSRIOVDevicePluginController.DevicePlugin != nil {
		dp := config.Spec.NodeSRIOVDevicePluginController.DevicePlugin
		if dp.Image != nil {
			variables.NodeSRIOVDevicePluginController.DevicePluginImage = *dp.Image
		}
		if dp.InitImage != nil {
			variables.NodeSRIOVDevicePluginController.DevicePluginInitImage = *dp.InitImage
		}
		if dp.DefaultResourcePrefix != nil {
			variables.NodeSRIOVDevicePluginController.DefaultResourcePrefix = *dp.DefaultResourcePrefix
		}
	}

	if config.Spec.Security != nil && config.Spec.Security.Kata != nil {
		kata := config.Spec.Security.Kata
		for _, shim := range kata.Shims {
			variables.KataContainers.Shims = append(variables.KataContainers.Shims, string(shim))
		}
		variables.KataContainers.ContainerdConfigFileName = kata.ContainerdConfigFileName
		variables.KataContainers.NodeSelector = kata.NodeSelector
	}

	if config.Spec.Security != nil && config.Spec.Security.VaultKMS != nil {
		variables.VaultKMS = config.Spec.Security.VaultKMS
	}
	return variables
}

func setMonitoringConfigs(variables Variables, config *operatorv1.DPFOperatorConfig) Variables {
	if !config.MonitoringEnabled() {
		variables.DisableSystemComponents[operatorv1.NodeProblemDetectorName] = true
		variables.DisableSystemComponents[operatorv1.KubeStateMetricsName] = true
		variables.DisableSystemComponents[operatorv1.OpenTelemetryCollectorName] = true
		variables.DisableSystemComponents[operatorv1.DPUMonitoringName] = true
		return variables
	}

	// Enable monitoring components by default when monitoring is enabled
	variables.DisableSystemComponents[operatorv1.NodeProblemDetectorName] = false
	variables.DisableSystemComponents[operatorv1.KubeStateMetricsName] = false
	// The DPU cluster control plane metrics credentials follow the ServiceMonitors
	// created by the cluster manager, which are also gated on MonitoringEnabled.
	variables.DisableSystemComponents[operatorv1.DPUMonitoringName] = false
	// OpenTelemetry Collector remains disabled by default (requires endpoint configuration)

	// No component-specific configuration provided, use defaults
	if config.Spec.Monitoring == nil {
		return variables
	}

	// Apply kube-state-metrics specific configuration
	if ksmConfig := config.Spec.Monitoring.KubeStateMetrics; ksmConfig != nil && ksmConfig.Disabled() {
		variables.DisableSystemComponents[operatorv1.KubeStateMetricsName] = true
	}

	// Apply node-problem-detector specific configuration
	if npdConfig := config.Spec.Monitoring.NodeProblemDetector; npdConfig != nil && npdConfig.Disabled() {
		variables.DisableSystemComponents[operatorv1.NodeProblemDetectorName] = true
	}

	// Apply opentelemetry-collector specific configuration
	// OpenTelemetry Collector is disabled by default and requires explicit configuration
	if otelConfig := config.Spec.Monitoring.OpenTelemetryCollector; otelConfig != nil {
		if otelConfig.Disabled() {
			variables.DisableSystemComponents[operatorv1.OpenTelemetryCollectorName] = true
		} else if otelConfig.Logging != nil || otelConfig.Metrics != nil {
			// Only enable if at least one endpoint is explicitly provided
			variables.DisableSystemComponents[operatorv1.OpenTelemetryCollectorName] = false
			if otelConfig.Logging != nil {
				variables.OpenTelemetryCollector.LoggingEndpoint = otelConfig.Logging.Endpoint
			}
			if otelConfig.Metrics != nil {
				variables.OpenTelemetryCollector.MetricsEndpoint = otelConfig.Metrics.Endpoint
			}
		}
		// If enabled but no endpoint provided, it remains disabled
	}

	return variables
}

// getContainerNameFromComponent returns the container name associated with the given component configuration.
// This is used for components that have a single container and use the deprecated single image configuration.
// TODO: Remove this function after the deprecated single image config is removed.
// Deprecated: Use multi-container image configuration instead.
func getContainerNameFromComponent(componentName operatorv1.ComponentName) operatorv1.ContainerName {
	switch componentName {
	case operatorv1.ProvisioningControllerName,
		operatorv1.DPUServiceControllerName,
		operatorv1.SFCControllerName,
		operatorv1.ServiceSetControllerName,
		operatorv1.KamajiClusterManagerName,
		operatorv1.StaticClusterManagerName:
		return operatorv1.ControllerManagerContainer
	case operatorv1.DPUDetectorName:
		return operatorv1.DPUDetectorContainer
	case operatorv1.MultusName:
		return operatorv1.MultusContainer
	case operatorv1.SRIOVDevicePluginName:
		return operatorv1.SRIOVDevicePluginContainer
	case operatorv1.NVIPAMControllerName:
		return operatorv1.NVIPAMContainerController
	}
	return ""
}
