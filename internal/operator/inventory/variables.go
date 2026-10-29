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
	"github.com/nvidia/doca-platform/internal/release"
	"github.com/nvidia/doca-platform/pkg/dpucluster"

	corev1 "k8s.io/api/core/v1"
)

func newDefaultVariables(defaults *release.Defaults) Variables {
	return Variables{
		DPUClusters:                      []*dpucluster.Config{},
		DPUCNIBinPath:                    "/opt/cni/bin",
		DPUCNIConfPath:                   "/etc/cni/net.d/",
		DPUOpenvSwitchRunPath:            "/var/run/openvswitch/",
		DPUOpenvSwitchBinPath:            "/usr/bin/",
		DPUOpenvSwitchSharedLibPath:      "/lib",
		DPUOpenvSwitchSharedLib64Path:    nil, // Default to nil - only mount when explicitly configured
		FlannelSkipCNIConfigInstallation: true,
		FlannelPodCIDR:                   "10.244.0.0/14",
		DisableSystemComponents: map[string]bool{
			operatorv1.ProvisioningControllerName: false,
			operatorv1.DPUServiceControllerName:   false,
			operatorv1.ServiceSetControllerName:   false,
			operatorv1.FlannelName:                false,
			operatorv1.MultusName:                 false,
			operatorv1.SRIOVDevicePluginName:      false,
			operatorv1.OVSCNIName:                 false,
			operatorv1.NVIPAMName:                 false,
			operatorv1.SFCControllerName:          false,
			operatorv1.DPUDetectorName:            false,
			operatorv1.KamajiClusterManagerName:   false,

			// Static cluster manager is disabled by default.
			operatorv1.StaticClusterManagerName: true,
		},
		Images: map[string]string{
			// Images built as part of the DPF Operator release.
			operatorv1.ProvisioningControllerName: defaults.DPFSystemImage,
			operatorv1.DPUServiceControllerName:   defaults.DPFSystemImage,
			operatorv1.StaticClusterManagerName:   defaults.DPFSystemImage,
			operatorv1.KamajiClusterManagerName:   defaults.DPFSystemImage,
			operatorv1.ServiceSetControllerName:   defaults.DPFSystemImage,
			operatorv1.OVSCNIName:                 defaults.OVSCNIImage,
			operatorv1.SFCControllerName:          defaults.DPFSystemImage,
			operatorv1.DPUDetectorName:            defaults.DPFSystemImage,
			operatorv1.BFBRegistryName:            defaults.BFBRegistryImage,
		},
		HelmCharts: map[string]string{
			operatorv1.FlannelName:              defaults.DPUNetworkingHelmChart,
			operatorv1.MultusName:               defaults.DPUNetworkingHelmChart,
			operatorv1.SRIOVDevicePluginName:    defaults.DPUNetworkingHelmChart,
			operatorv1.NVIPAMName:               defaults.DPUNetworkingHelmChart,
			operatorv1.OVSCNIName:               defaults.DPUNetworkingHelmChart,
			operatorv1.SFCControllerName:        defaults.DPUNetworkingHelmChart,
			operatorv1.ServiceSetControllerName: defaults.DPUNetworkingHelmChart,
		},
		DeployInCluster: map[string]bool{
			operatorv1.FlannelName:              false,
			operatorv1.MultusName:               false,
			operatorv1.SRIOVDevicePluginName:    false,
			operatorv1.NVIPAMName:               false,
			operatorv1.OVSCNIName:               false,
			operatorv1.SFCControllerName:        false,
			operatorv1.ServiceSetControllerName: true,
		},
		SFCController: SFCControllerVariables{
			SecureFlowDeletionTimeout: 0 * time.Second,
		},
		Resources: map[string]corev1.ResourceRequirements{},
	}
}

// Variables contains information required to generate manifests from the inventory.
type Variables struct {
	Namespace                        string
	DPUCNIBinPath                    string
	DPUCNIConfPath                   string
	DPUOpenvSwitchRunPath            string
	DPUOpenvSwitchBinPath            string
	DPUOpenvSwitchSharedLibPath      string
	DPUOpenvSwitchSharedLib64Path    *string
	FlannelSkipCNIConfigInstallation bool
	FlannelPodCIDR                   string
	DPUClusters                      []*dpucluster.Config
	DPFProvisioningController        DPFProvisioningVariables
	SFCController                    SFCControllerVariables
	Networking                       Networking
	DisableSystemComponents          map[string]bool
	ImagePullSecrets                 []string
	Images                           map[string]string
	HelmCharts                       map[string]string
	DeployInCluster                  map[string]bool
	KubernetesAPIServerVIP           *string
	KubernetesAPIServerPort          *int
	Resources                        map[string]corev1.ResourceRequirements
}

type DPFProvisioningVariables struct {
	BFBPersistentVolumeClaimName   string
	DMSTimeout                     *int
	BFCFGTemplateConfig            *string
	CustomCASecretName             *string
	InstallInterface               *operatorv1.ProvisioningInstallInterface
	MaxDPUParallelInstallations    *int32
	MultiDPUOperationsSyncWaitTime time.Duration
	MaxUnavailableDPUNodes         *int32
}

type SFCControllerVariables struct {
	SecureFlowDeletionTimeout time.Duration
}

type Networking struct {
	ControlPlaneMTU int
	HighSpeedMTU    int
}

func VariablesFromDPFOperatorConfig(defaults *release.Defaults, config *operatorv1.DPFOperatorConfig, dpuClusters []*dpucluster.Config) Variables {
	variables := newDefaultVariables(defaults)
	variables = extractComponentConfigs(variables, config)
	variables = setBasicConfig(variables, config)
	variables = setNetworkingConfig(variables, config)
	variables = setOverrideConfigs(variables, config)
	variables = setAdditionalConfigs(variables, config)
	variables.DPUClusters = append(variables.DPUClusters, dpuClusters...)
	return variables
}

// extractComponentConfigs extracts component-specific configurations from the DPFOperatorConfig
func extractComponentConfigs(variables Variables, config *operatorv1.DPFOperatorConfig) Variables {
	disableComponents := variables.DisableSystemComponents
	images := variables.Images
	helmCharts := variables.HelmCharts
	deployInCluster := variables.DeployInCluster
	resources := variables.Resources

	for _, componentConfig := range config.ComponentConfigs() {
		if componentConfig == nil {
			continue
		}

		componentName := componentConfig.Name()
		disableComponents[componentName] = componentConfig.Disabled()

		// Extract helm chart configuration
		if helmConfig, ok := componentConfig.(operatorv1.HelmComponentConfigurable); ok && helmConfig.GetHelmChart() != nil {
			helmCharts[componentName] = *helmConfig.GetHelmChart()
		}

		// Extract DPU service configuration
		if dpuServiceConfig, ok := componentConfig.(operatorv1.InClusterDeploymentConfigurable); ok && dpuServiceConfig != nil {
			if dpuServiceConfig.InClusterDeployment() {
				deployInCluster[componentName] = true
			}
		}
		extraImageConfigs(componentConfig, images, componentName)
		extraResourceConfigs(componentConfig, resources, componentName)
	}

	variables.DisableSystemComponents = disableComponents
	variables.Images = images
	variables.HelmCharts = helmCharts
	variables.DeployInCluster = deployInCluster
	return variables
}

func extraImageConfigs(componentConfig operatorv1.ComponentConfigurable, images map[string]string, componentName string) {
	// nolint:staticcheck
	if imageConfig, ok := componentConfig.(operatorv1.DeprecatedImageComponentConfigurable); ok && imageConfig.GetImage() != nil {
		images[componentName] = *imageConfig.GetImage()
	}

	if multiImageConfig, ok := componentConfig.(operatorv1.ImageComponentConfigurable); ok {
		containerImages := multiImageConfig.GetImages()
		for containerName, img := range containerImages {
			// TODO: this is a temporary workaround for flannel if the deprecated image override is already in-use.
			if _, ok := images[componentName]; ok && componentName == operatorv1.FlannelName {
				break
			}
			if img != nil {
				images[componentName+multiSplitChar+containerName] = *img
			}
		}
	}
}

func extraResourceConfigs(componentConfig operatorv1.ComponentConfigurable, resources map[string]corev1.ResourceRequirements, componentName string) {
	// nolint:staticcheck
	if resourceConfig, ok := componentConfig.(operatorv1.DeprecatedResourceComponentConfig); ok {
		resourceRequests := resourceConfig.GetResource()
		if resourceRequests != nil {
			resources[componentName] = *resourceRequests
		}
	}

	if multiResourceConfig, ok := componentConfig.(operatorv1.ResourcesComponentConfigurable); ok {
		containerResources := multiResourceConfig.GetResources()
		for containerName, resourceMap := range containerResources {
			if resourceMap == nil {
				continue
			}
			resources[componentName+multiSplitChar+containerName] = *resourceMap
		}
	}
}

// setBasicConfig sets the basic configuration values
func setBasicConfig(variables Variables, config *operatorv1.DPFOperatorConfig) Variables {
	variables.Namespace = config.Namespace
	variables.DPFProvisioningController = DPFProvisioningVariables{
		BFBPersistentVolumeClaimName: config.Spec.ProvisioningController.BFBPersistentVolumeClaimName,
		DMSTimeout:                   config.Spec.ProvisioningController.DMSTimeout,
		BFCFGTemplateConfig:          config.Spec.ProvisioningController.BFCFGTemplateConfigMap,
		CustomCASecretName:           config.Spec.ProvisioningController.CustomCASecretName,
		InstallInterface:             config.Spec.ProvisioningController.InstallInterface,
		MaxDPUParallelInstallations:  config.Spec.ProvisioningController.MaxDPUParallelInstallations,
		MaxUnavailableDPUNodes:       config.Spec.ProvisioningController.MaxUnavailableDPUNodes,
	}
	if config.Spec.ProvisioningController.MultiDPUOperationsSyncWaitTime != nil {
		variables.DPFProvisioningController.MultiDPUOperationsSyncWaitTime = config.Spec.ProvisioningController.MultiDPUOperationsSyncWaitTime.Duration
	}
	variables.ImagePullSecrets = config.Spec.ImagePullSecrets
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
	if config.Spec.Overrides.DPUOpenvSwitchRunPath != nil {
		variables.DPUOpenvSwitchRunPath = *config.Spec.Overrides.DPUOpenvSwitchRunPath
	}
	if config.Spec.Overrides.KubernetesAPIServerVIP != nil {
		variables.KubernetesAPIServerVIP = config.Spec.Overrides.KubernetesAPIServerVIP
	}
	if config.Spec.Overrides.KubernetesAPIServerPort != nil {
		variables.KubernetesAPIServerPort = config.Spec.Overrides.KubernetesAPIServerPort
	}
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
	return variables
}
