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
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/internal/release"
)

func newDefaultVariables(defaults *release.Defaults) Variables {
	return Variables{
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
			operatorv1.OVSHelperName:              false,
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
			operatorv1.DPUDetectorName:            defaults.DMSImage,
			operatorv1.OVSHelperName:              defaults.DPFSystemImage,

			// External images of components which are deployed by the DPF Operator.
			operatorv1.MultusName:            defaults.MultusImage,
			operatorv1.SRIOVDevicePluginName: defaults.SRIOVDPImage,
			operatorv1.NVIPAMName:            defaults.NVIPAMImage,
		},
		HelmCharts: map[string]string{
			operatorv1.FlannelName:              defaults.DPUNetworkingHelmChart,
			operatorv1.MultusName:               defaults.DPUNetworkingHelmChart,
			operatorv1.SRIOVDevicePluginName:    defaults.DPUNetworkingHelmChart,
			operatorv1.NVIPAMName:               defaults.DPUNetworkingHelmChart,
			operatorv1.OVSCNIName:               defaults.DPUNetworkingHelmChart,
			operatorv1.SFCControllerName:        defaults.DPUNetworkingHelmChart,
			operatorv1.ServiceSetControllerName: defaults.DPUNetworkingHelmChart,
			operatorv1.OVSHelperName:            defaults.DPUNetworkingHelmChart,
		},
		DPUDetectorCollectors: map[string]bool{},
	}
}

// Variables contains information required to generate manifests from the inventory.
type Variables struct {
	Namespace                 string
	DPFProvisioningController DPFProvisioningVariables
	Networking                Networking
	DisableSystemComponents   map[string]bool
	ImagePullSecrets          []string
	Images                    map[string]string
	HelmCharts                map[string]string
	DPUDetectorCollectors     map[string]bool
}

type DPFProvisioningVariables struct {
	BFBPersistentVolumeClaimName string
	DMSTimeout                   *int
}

type Networking struct {
	ControlPlaneMTU int
	HighSpeedMTU    int
}

func VariablesFromDPFOperatorConfig(defaults *release.Defaults, config *operatorv1.DPFOperatorConfig) Variables {
	variables := newDefaultVariables(defaults)
	disableComponents := variables.DisableSystemComponents
	images := variables.Images
	helmCharts := variables.HelmCharts
	collectors := variables.DPUDetectorCollectors
	for _, componentConfig := range config.ComponentConfigs() {
		if componentConfig != nil {
			disableComponents[componentConfig.Name()] = componentConfig.Disabled()

			// If the component is an imageComponent override the image.
			imageConfig, ok := componentConfig.(operatorv1.ImageComponentConfig)
			if ok && imageConfig.GetImage() != nil {
				images[componentConfig.Name()] = *imageConfig.GetImage()
			}

			// If the component is a helmComponent override the helm chart.
			helmConfig, ok := componentConfig.(operatorv1.HelmComponentConfig)
			if ok && helmConfig.GetHelmChart() != nil {
				helmCharts[componentConfig.Name()] = *helmConfig.GetHelmChart()
			}

			dpuDetector, ok := componentConfig.(*operatorv1.DPUDetectorConfiguration)
			if ok && dpuDetector != nil && dpuDetector.Collectors != nil {
				if dpuDetector.Collectors.PSID != nil {
					collectors[psIDCollectorName] = *dpuDetector.Collectors.PSID
				}
			}
		}
	}

	variables.Namespace = config.Namespace
	variables.DPFProvisioningController = DPFProvisioningVariables{
		BFBPersistentVolumeClaimName: config.Spec.ProvisioningController.BFBPersistentVolumeClaimName,
		DMSTimeout:                   config.Spec.ProvisioningController.DMSTimeout,
	}
	variables.ImagePullSecrets = config.Spec.ImagePullSecrets
	variables.DisableSystemComponents = disableComponents
	variables.Images = images
	variables.HelmCharts = helmCharts
	variables.DPUDetectorCollectors = collectors

	// This value should never be nil in a running system as it defaulted by the OpenAPI spec. This check guards against
	// tests etc. that may create a DPFOperatorConfig without setting this value.
	if config.Spec.Networking != nil {
		if config.Spec.Networking.ControlPlaneMTU != nil {
			variables.Networking.ControlPlaneMTU = *config.Spec.Networking.ControlPlaneMTU
		}
		if config.Spec.Networking.HighSpeedMTU != nil {
			variables.Networking.HighSpeedMTU = *config.Spec.Networking.HighSpeedMTU
		}
	}
	return variables
}
