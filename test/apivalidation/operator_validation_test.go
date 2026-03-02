/*
Copyright 2025 NVIDIA

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

//nolint:staticcheck
package apivalidation_test

import (
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Operator API Validation", func() {
	var testNs *corev1.Namespace
	var cleanupObjs []client.Object

	BeforeEach(func() {
		cleanupObjs = []client.Object{}
		testNs = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "test-api-validation-"}}
		Expect(testClient.Create(ctx, testNs)).To(Succeed())
	})

	AfterEach(func() {
		Expect(testutils.CleanupAndWait(ctx, testClient, cleanupObjs...)).To(Succeed())
		Expect(testClient.Delete(ctx, testNs)).To(Succeed())
	})

	Context("DPFOperatorConfig", func() {
		Context("validate image configuration mutual exclusivity", func() {
			DescribeTable("ProvisioningControllerConfiguration validation",
				func(legacyImage string, controllerImage string, expectError bool, errorMessage string) {
					config := getMinimalDPFOperatorConfig(testNs.Name)
					setProvisioningControllerImages(config, legacyImage, controllerImage)
					validateConfigCreation(config, expectError, errorMessage, &cleanupObjs)
				},
				Entry("valid - only legacy image", "legacy-image:latest", "", false, ""),
				Entry("valid - only controller.image", "", "controller-image:latest", false, ""),
				Entry("valid - no image set", "", "", false, ""),
				Entry("invalid - both images set", "legacy-image:latest", "controller-image:latest", true, "only either 'image' (deprecated) or 'controller.image' can be set, but not both"),
			)

			DescribeTable("DPUServiceControllerConfiguration validation",
				func(legacyImage string, controllerImage string, expectError bool, errorMessage string) {
					config := getMinimalDPFOperatorConfig(testNs.Name)
					setDPUServiceControllerImages(config, legacyImage, controllerImage)
					validateConfigCreation(config, expectError, errorMessage, &cleanupObjs)
				},
				Entry("valid - only legacy image", "legacy-image:latest", "", false, ""),
				Entry("valid - only controller.image", "", "controller-image:latest", false, ""),
				Entry("valid - no image set", "", "", false, ""),
				Entry("invalid - both images set", "legacy-image:latest", "controller-image:latest", true, "only either 'image' (deprecated) or 'controller.image' can be set, but not both"),
			)

			DescribeTable("DPUDetectorConfiguration validation",
				func(legacyImage string, daemonImage string, expectError bool, errorMessage string) {
					config := getMinimalDPFOperatorConfig(testNs.Name)
					setDPUDetectorImages(config, legacyImage, daemonImage)
					validateConfigCreation(config, expectError, errorMessage, &cleanupObjs)
				},
				Entry("valid - only legacy image", "legacy-image:latest", "", false, ""),
				Entry("valid - only daemon.image", "", "daemon-image:latest", false, ""),
				Entry("valid - no image set", "", "", false, ""),
				Entry("invalid - both images set", "legacy-image:latest", "daemon-image:latest", true, "only either 'image' (deprecated) or 'daemon.image' can be set, but not both"),
			)

			DescribeTable("KamajiClusterManagerConfiguration validation",
				func(legacyImage string, controllerImage string, expectError bool, errorMessage string) {
					config := getMinimalDPFOperatorConfig(testNs.Name)
					setKamajiClusterManagerImages(config, legacyImage, controllerImage)
					validateConfigCreation(config, expectError, errorMessage, &cleanupObjs)
				},
				Entry("valid - only legacy image", "legacy-image:latest", "", false, ""),
				Entry("valid - only controller.image", "", "controller-image:latest", false, ""),
				Entry("valid - no image set", "", "", false, ""),
				Entry("invalid - both images set", "legacy-image:latest", "controller-image:latest", true, "only either 'image' (deprecated) or 'controller.image' can be set, but not both"),
			)

			DescribeTable("StaticClusterManagerConfiguration validation",
				func(legacyImage string, controllerImage string, expectError bool, errorMessage string) {
					config := getMinimalDPFOperatorConfig(testNs.Name)
					setStaticClusterManagerImages(config, legacyImage, controllerImage)
					validateConfigCreation(config, expectError, errorMessage, &cleanupObjs)
				},
				Entry("valid - only legacy image", "legacy-image:latest", "", false, ""),
				Entry("valid - only controller.image", "", "controller-image:latest", false, ""),
				Entry("valid - no image set", "", "", false, ""),
				Entry("invalid - both images set", "legacy-image:latest", "controller-image:latest", true, "only either 'image' (deprecated) or 'controller.image' can be set, but not both"),
			)

			DescribeTable("ServiceSetControllerConfiguration validation",
				func(legacyImage string, controllerImage string, expectError bool, errorMessage string) {
					config := getMinimalDPFOperatorConfig(testNs.Name)
					setServiceSetControllerImages(config, legacyImage, controllerImage)
					validateConfigCreation(config, expectError, errorMessage, &cleanupObjs)
				},
				Entry("valid - only legacy image", "legacy-image:latest", "", false, ""),
				Entry("valid - only controller.image", "", "controller-image:latest", false, ""),
				Entry("valid - no image set", "", "", false, ""),
				Entry("invalid - both images set", "legacy-image:latest", "controller-image:latest", true, "only either 'image' (deprecated) or 'controller.image' can be set, but not both"),
			)

			DescribeTable("MultusConfiguration validation",
				func(legacyImage string, cniImage string, expectError bool, errorMessage string) {
					config := getMinimalDPFOperatorConfig(testNs.Name)
					setMultusImages(config, legacyImage, cniImage)
					validateConfigCreation(config, expectError, errorMessage, &cleanupObjs)
				},
				Entry("valid - only legacy image", "legacy-image:latest", "", false, ""),
				Entry("valid - only cni.image", "", "cni-image:latest", false, ""),
				Entry("valid - no image set", "", "", false, ""),
				Entry("invalid - both images set", "legacy-image:latest", "cni-image:latest", true, "only either 'image' (deprecated) or 'cni.image' can be set, but not both"),
			)

			DescribeTable("NVIPAMConfiguration validation",
				func(legacyImage string, controllerImage string, expectError bool, errorMessage string) {
					config := getMinimalDPFOperatorConfig(testNs.Name)
					setNVIPAMImages(config, legacyImage, controllerImage)
					validateConfigCreation(config, expectError, errorMessage, &cleanupObjs)
				},
				Entry("valid - only legacy image", "legacy-image:latest", "", false, ""),
				Entry("valid - only controller.image", "", "controller-image:latest", false, ""),
				Entry("valid - no image set", "", "", false, ""),
				Entry("invalid - both images set", "legacy-image:latest", "controller-image:latest", true, "only either 'image' (deprecated) or 'controller.image' can be set, but not both"),
			)

			DescribeTable("SRIOVDevicePluginConfiguration validation",
				func(legacyImage string, devicePluginImage string, expectError bool, errorMessage string) {
					config := getMinimalDPFOperatorConfig(testNs.Name)
					setSRIOVDevicePluginImages(config, legacyImage, devicePluginImage)
					validateConfigCreation(config, expectError, errorMessage, &cleanupObjs)
				},
				Entry("valid - only legacy image", "legacy-image:latest", "", false, ""),
				Entry("valid - only deviceplugin.image", "", "deviceplugin-image:latest", false, ""),
				Entry("valid - no image set", "", "", false, ""),
				Entry("invalid - both images set", "legacy-image:latest", "deviceplugin-image:latest", true, "only either 'image' (deprecated) or 'deviceplugin.image' can be set, but not both"),
			)

			DescribeTable("OVSCNIConfiguration validation",
				func(legacyImage string, cniImage string, expectError bool, errorMessage string) {
					config := getMinimalDPFOperatorConfig(testNs.Name)
					setOVSCNIImages(config, legacyImage, cniImage)
					validateConfigCreation(config, expectError, errorMessage, &cleanupObjs)
				},
				Entry("valid - only legacy image", "legacy-image:latest", "", false, ""),
				Entry("valid - only cni.image", "", "cni-image:latest", false, ""),
				Entry("valid - no image set", "", "", false, ""),
				Entry("invalid - both images set", "legacy-image:latest", "cni-image:latest", true, "only either 'image' (deprecated) or 'cni.image' can be set, but not both"),
			)

			DescribeTable("SFCControllerConfiguration validation",
				func(legacyImage string, controllerImage string, expectError bool, errorMessage string) {
					config := getMinimalDPFOperatorConfig(testNs.Name)
					setSFCControllerImages(config, legacyImage, controllerImage)
					validateConfigCreation(config, expectError, errorMessage, &cleanupObjs)
				},
				Entry("valid - only legacy image", "legacy-image:latest", "", false, ""),
				Entry("valid - only controller.image", "", "controller-image:latest", false, ""),
				Entry("valid - no image set", "", "", false, ""),
				Entry("invalid - both images set", "legacy-image:latest", "controller-image:latest", true, "only either 'image' (deprecated) or 'controller.image' can be set, but not both"),
			)
		})

		Context("validate bfCFGTemplateConfigMap and  mutual exclusivity", func() {
			DescribeTable("ProvisioningControllerConfiguration bfcfg template validation",
				func(bfCFGTemplateConfigMap *string, enableDynamic bool, expectError bool, errorMessage string) {
					config := getMinimalDPFOperatorConfig(testNs.Name)
					config.Spec.ProvisioningController.BFCFGTemplateConfigMap = bfCFGTemplateConfigMap
					config.Spec.ProvisioningController.EnableDynamicBFCFGTemplates = enableDynamic
					validateConfigCreation(config, expectError, errorMessage, &cleanupObjs)
				},
				Entry("valid - only bfCFGTemplateConfigMap set", ptr.To("custom-bfb-cfg"), false, false, ""),
				Entry("valid - only enableDynamicBFCFGTemplates set", nil, true, false, ""),
				Entry("valid - neither set", nil, false, false, ""),
				Entry("invalid - both set", ptr.To("custom-bfb-cfg"), true, true, "bfCFGTemplateConfigMap and enableDynamicBFCFGTemplates are mutually exclusive"),
			)
		})

		Context("validate replicas configuration", func() {
			DescribeTable("ProvisioningControllerConfiguration replicas validation",
				func(replicas *int32, expectError bool, errorMessage string) {
					config := getMinimalDPFOperatorConfig(testNs.Name)
					config.Spec.ProvisioningController.Replicas = replicas
					validateConfigCreation(config, expectError, errorMessage, &cleanupObjs)
				},
				Entry("valid - replicas is nil (uses default)", (*int32)(nil), false, ""),
				Entry("valid - replicas is 1", ptr.To(int32(1)), false, ""),
				Entry("valid - replicas is 2", ptr.To(int32(2)), false, ""),
				Entry("valid - replicas is 3", ptr.To(int32(3)), false, ""),
				Entry("invalid - replicas is 0", ptr.To(int32(0)), true, "should be greater than or equal to 1"),
				Entry("invalid - replicas is 4", ptr.To(int32(4)), true, "should be less than or equal to 3"),
				Entry("invalid - replicas is negative", ptr.To(int32(-1)), true, "should be greater than or equal to 1"),
			)

			DescribeTable("KamajiClusterManagerConfiguration replicas validation",
				func(replicas *int32, expectError bool, errorMessage string) {
					config := getMinimalDPFOperatorConfig(testNs.Name)
					if config.Spec.KamajiClusterManager == nil {
						config.Spec.KamajiClusterManager = &operatorv1.KamajiClusterManagerConfiguration{}
					}
					config.Spec.KamajiClusterManager.Replicas = replicas
					validateConfigCreation(config, expectError, errorMessage, &cleanupObjs)
				},
				Entry("valid - replicas is nil (uses default)", (*int32)(nil), false, ""),
				Entry("valid - replicas is 1", ptr.To(int32(1)), false, ""),
				Entry("valid - replicas is 2", ptr.To(int32(2)), false, ""),
				Entry("valid - replicas is 3", ptr.To(int32(3)), false, ""),
				Entry("invalid - replicas is 0", ptr.To(int32(0)), true, "should be greater than or equal to 1"),
				Entry("invalid - replicas is 4", ptr.To(int32(4)), true, "should be less than or equal to 3"),
				Entry("invalid - replicas is negative", ptr.To(int32(-1)), true, "should be greater than or equal to 1"),
			)
		})
	})
})

// Helper functions for setting image configurations
func setProvisioningControllerImages(config *operatorv1.DPFOperatorConfig, legacyImage, controllerImage string) {
	if legacyImage != "" {
		config.Spec.ProvisioningController.Image = &legacyImage
	}
	if controllerImage != "" {
		config.Spec.ProvisioningController.Controller = &operatorv1.DefaultOverridesConfiguration{
			ImageComponentConfig: operatorv1.ImageComponentConfig{
				Image: &controllerImage,
			},
		}
	}
}

func setDPUServiceControllerImages(config *operatorv1.DPFOperatorConfig, legacyImage, controllerImage string) {
	config.Spec.DPUServiceController = &operatorv1.DPUServiceControllerConfiguration{}
	if legacyImage != "" {
		config.Spec.DPUServiceController.Image = &legacyImage
	}
	if controllerImage != "" {
		config.Spec.DPUServiceController.Controller = &operatorv1.DefaultOverridesConfiguration{
			ImageComponentConfig: operatorv1.ImageComponentConfig{
				Image: &controllerImage,
			},
		}
	}
}

func setDPUDetectorImages(config *operatorv1.DPFOperatorConfig, legacyImage, daemonImage string) {
	config.Spec.DPUDetector = &operatorv1.DPUDetectorConfiguration{}
	if legacyImage != "" {
		config.Spec.DPUDetector.Image = &legacyImage
	}
	if daemonImage != "" {
		config.Spec.DPUDetector.Daemon = &operatorv1.DefaultOverridesConfiguration{
			ImageComponentConfig: operatorv1.ImageComponentConfig{
				Image: &daemonImage,
			},
		}
	}
}

func setKamajiClusterManagerImages(config *operatorv1.DPFOperatorConfig, legacyImage, controllerImage string) {
	config.Spec.KamajiClusterManager = &operatorv1.KamajiClusterManagerConfiguration{}
	if legacyImage != "" {
		config.Spec.KamajiClusterManager.Image = &legacyImage
	}
	if controllerImage != "" {
		config.Spec.KamajiClusterManager.Controller = &operatorv1.DefaultOverridesConfiguration{
			ImageComponentConfig: operatorv1.ImageComponentConfig{
				Image: &controllerImage,
			},
		}
	}
}

func setStaticClusterManagerImages(config *operatorv1.DPFOperatorConfig, legacyImage, controllerImage string) {
	config.Spec.StaticClusterManager = &operatorv1.StaticClusterManagerConfiguration{}
	if legacyImage != "" {
		config.Spec.StaticClusterManager.Image = &legacyImage
	}
	if controllerImage != "" {
		config.Spec.StaticClusterManager.Controller = &operatorv1.DefaultOverridesConfiguration{
			ImageComponentConfig: operatorv1.ImageComponentConfig{
				Image: &controllerImage,
			},
		}
	}
}

func setServiceSetControllerImages(config *operatorv1.DPFOperatorConfig, legacyImage, controllerImage string) {
	config.Spec.ServiceSetController = &operatorv1.ServiceSetControllerConfiguration{}
	if legacyImage != "" {
		config.Spec.ServiceSetController.Image = &legacyImage
	}
	if controllerImage != "" {
		config.Spec.ServiceSetController.Controller = &operatorv1.DefaultOverridesConfiguration{
			ImageComponentConfig: operatorv1.ImageComponentConfig{
				Image: &controllerImage,
			},
		}
	}
}

func setMultusImages(config *operatorv1.DPFOperatorConfig, legacyImage, cniImage string) {
	config.Spec.Multus = &operatorv1.MultusConfiguration{}
	if legacyImage != "" {
		config.Spec.Multus.Image = &legacyImage
	}
	if cniImage != "" {
		config.Spec.Multus.CNI = &operatorv1.DefaultOverridesConfiguration{
			ImageComponentConfig: operatorv1.ImageComponentConfig{
				Image: &cniImage,
			},
		}
	}
}

func setNVIPAMImages(config *operatorv1.DPFOperatorConfig, legacyImage, controllerImage string) {
	config.Spec.NVIPAM = &operatorv1.NVIPAMConfiguration{}
	if legacyImage != "" {
		config.Spec.NVIPAM.Image = &legacyImage
	}
	if controllerImage != "" {
		config.Spec.NVIPAM.Controller = &operatorv1.NVIPAMController{
			ImageComponentConfig: operatorv1.ImageComponentConfig{
				Image: &controllerImage,
			},
		}
	}
}

func setSRIOVDevicePluginImages(config *operatorv1.DPFOperatorConfig, legacyImage, devicePluginImage string) {
	config.Spec.SRIOVDevicePlugin = &operatorv1.SRIOVDevicePluginConfiguration{}
	if legacyImage != "" {
		config.Spec.SRIOVDevicePlugin.Image = &legacyImage
	}
	if devicePluginImage != "" {
		config.Spec.SRIOVDevicePlugin.DevicePlugin = &operatorv1.DefaultOverridesConfiguration{
			ImageComponentConfig: operatorv1.ImageComponentConfig{
				Image: &devicePluginImage,
			},
		}
	}
}

func setOVSCNIImages(config *operatorv1.DPFOperatorConfig, legacyImage, cniImage string) {
	config.Spec.OVSCNI = &operatorv1.OVSCNIConfiguration{}
	if legacyImage != "" {
		config.Spec.OVSCNI.Image = &legacyImage
	}
	if cniImage != "" {
		config.Spec.OVSCNI.CNI = &operatorv1.DefaultOverridesConfiguration{
			ImageComponentConfig: operatorv1.ImageComponentConfig{
				Image: &cniImage,
			},
		}
	}
}

func setSFCControllerImages(config *operatorv1.DPFOperatorConfig, legacyImage, controllerImage string) {
	config.Spec.SFCController = &operatorv1.SFCControllerConfiguration{}
	if legacyImage != "" {
		config.Spec.SFCController.Image = &legacyImage
	}
	if controllerImage != "" {
		config.Spec.SFCController.Controller = &operatorv1.DefaultOverridesConfiguration{
			ImageComponentConfig: operatorv1.ImageComponentConfig{
				Image: &controllerImage,
			},
		}
	}
}

// Common validation helper
func validateConfigCreation(config *operatorv1.DPFOperatorConfig, expectError bool, errorMessage string, cleanupObjs *[]client.Object) {
	err := testClient.Create(ctx, config)
	if expectError {
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(errorMessage))
	} else {
		Expect(err).ToNot(HaveOccurred())
		*cleanupObjs = append(*cleanupObjs, config)
	}
}

func getMinimalDPFOperatorConfig(namespace string) *operatorv1.DPFOperatorConfig {
	return &operatorv1.DPFOperatorConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-config",
			Namespace: namespace,
		},
		Spec: operatorv1.DPFOperatorConfigSpec{
			ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
				BFBPersistentVolumeClaimName: ptr.To("test-bfb-pvc"),
			},
		},
	}
}
