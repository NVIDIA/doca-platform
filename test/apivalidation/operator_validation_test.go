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
	"strings"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
		Context("Validate image configuration mutual exclusivity", func() {
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

		Context("Validate bfCFGTemplateConfigMap and mutual exclusivity", func() {
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

		Context("Validate replicas configuration", func() {
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

		Context("Validate deployment mode and install interface compatibility", func() {
			DescribeTable("DPFOperatorConfig deploymentMode validation",
				func(deploymentMode operatorv1.DeploymentMode, installInterface *operatorv1.ProvisioningInstallInterface, expectError bool, errorMessage string) {
					config := getMinimalDPFOperatorConfig(testNs.Name)
					config.Spec.DeploymentMode = deploymentMode
					config.Spec.ProvisioningController.InstallInterface = installInterface
					validateConfigCreation(config, expectError, errorMessage, &cleanupObjs)
				},
				Entry("valid - host-trusted without install interface", operatorv1.DeploymentModeHostTrusted, nil, false, ""),
				Entry("valid - host-trusted with installViaHostAgent", operatorv1.DeploymentModeHostTrusted, &operatorv1.ProvisioningInstallInterface{
					InstallViaHostAgent: &operatorv1.InstallViaHostAgent{},
				}, false, ""),
				Entry("valid - host-trusted with installViaGNOI", operatorv1.DeploymentModeHostTrusted, &operatorv1.ProvisioningInstallInterface{
					InstallViaGNOI: &operatorv1.InstallViaGNOI{},
				}, false, ""),
				Entry("invalid - host-trusted with installViaRedfish", operatorv1.DeploymentModeHostTrusted, &operatorv1.ProvisioningInstallInterface{
					InstallViaRedfish: &operatorv1.InstallViaRedfish{},
				}, true, "deploymentMode host-trusted does not support provisioningController.installInterface.installViaRedfish"),
				Entry("invalid - zero-trust without install interface", operatorv1.DeploymentModeZeroTrust, nil, true, "deploymentMode zero-trust requires provisioningController.installInterface.installViaRedfish"),
				Entry("invalid - zero-trust with installViaHostAgent", operatorv1.DeploymentModeZeroTrust, &operatorv1.ProvisioningInstallInterface{
					InstallViaHostAgent: &operatorv1.InstallViaHostAgent{},
				}, true, "deploymentMode zero-trust requires provisioningController.installInterface.installViaRedfish"),
				Entry("valid - zero-trust with installViaRedfish", operatorv1.DeploymentModeZeroTrust, &operatorv1.ProvisioningInstallInterface{
					InstallViaRedfish: &operatorv1.InstallViaRedfish{},
				}, false, ""),
			)
		})

		Context("Validate OOB bridge name", func() {
			DescribeTable("DPFOperatorConfig dpuNodeOOBBridgeName validation",
				func(deploymentMode operatorv1.DeploymentMode, bridgeName *string, expectError bool, errorMessage string) {
					config := getMinimalDPFOperatorConfig(testNs.Name)
					config.Spec.DeploymentMode = deploymentMode
					config.Spec.Networking = &operatorv1.Networking{
						ControlPlaneMTU:      ptr.To(1500),
						DPUNodeOOBBridgeName: bridgeName,
					}
					if deploymentMode == operatorv1.DeploymentModeZeroTrust {
						config.Spec.ProvisioningController.InstallInterface = &operatorv1.ProvisioningInstallInterface{
							InstallViaRedfish: &operatorv1.InstallViaRedfish{},
						}
					}
					validateConfigCreation(config, expectError, errorMessage, &cleanupObjs)
				},
				Entry("valid - host-trusted with default bridge name omitted", operatorv1.DeploymentModeHostTrusted, nil, false, ""),
				Entry("valid - host-trusted with br-ex", operatorv1.DeploymentModeHostTrusted, ptr.To("br-ex"), false, ""),
				Entry("valid - host-trusted with mgmt-br", operatorv1.DeploymentModeHostTrusted, ptr.To("mgmt-br"), false, ""),
				Entry("valid - zero-trust with default br-dpu", operatorv1.DeploymentModeZeroTrust, ptr.To("br-dpu"), false, ""),
				Entry("valid - zero-trust with bridge name omitted", operatorv1.DeploymentModeZeroTrust, nil, false, ""),
				Entry("invalid - zero-trust with custom bridge name", operatorv1.DeploymentModeZeroTrust, ptr.To("br-ex"), true, "dpuNodeOOBBridgeName is only configurable in host-trusted mode"),
				Entry("invalid - uppercase bridge name", operatorv1.DeploymentModeHostTrusted, ptr.To("BR-DPU"), true, "Invalid value"),
				Entry("invalid - bridge name starts with digit", operatorv1.DeploymentModeHostTrusted, ptr.To("1br"), true, "Invalid value"),
				Entry("invalid - bridge name too long", operatorv1.DeploymentModeHostTrusted, ptr.To("abcdefghijklmnop"), true, "Invalid value"),
			)

			It("accepts update from br-dpu to br-ex in host-trusted mode", func() {
				config := getMinimalDPFOperatorConfig(testNs.Name)
				config.Spec.Networking = &operatorv1.Networking{
					ControlPlaneMTU:      ptr.To(1500),
					DPUNodeOOBBridgeName: ptr.To("br-dpu"),
				}
				Expect(testClient.Create(ctx, config)).To(Succeed())
				cleanupObjs = append(cleanupObjs, config)

				updated := config.DeepCopy()
				updated.Spec.Networking.DPUNodeOOBBridgeName = ptr.To("br-ex")
				Expect(testClient.Update(ctx, updated)).To(Succeed())
			})
		})

		Context("Validate control plane MTU in zero-trust mode", func() {
			DescribeTable("DPFOperatorConfig controlPlaneMTU validation",
				func(controlPlaneMTU *int, expectError bool, errorMessage string) {
					config := getMinimalDPFOperatorConfig(testNs.Name)
					config.Spec.DeploymentMode = operatorv1.DeploymentModeZeroTrust
					config.Spec.ProvisioningController.InstallInterface = &operatorv1.ProvisioningInstallInterface{
						InstallViaRedfish: &operatorv1.InstallViaRedfish{},
					}
					config.Spec.Networking = &operatorv1.Networking{
						ControlPlaneMTU: controlPlaneMTU,
					}
					validateConfigCreation(config, expectError, errorMessage, &cleanupObjs)
				},
				Entry("valid - zero-trust with default MTU omitted", nil, false, ""),
				Entry("valid - zero-trust with MTU 1500", ptr.To(1500), false, ""),
				Entry("valid - zero-trust with MTU 1280", ptr.To(1280), false, ""),
				Entry("invalid - zero-trust with MTU 9000", ptr.To(9000), true, "controlPlaneMTU must not exceed 1500 in zero-trust mode because DPU OOB interfaces do not support jumbo frames"),
			)

			It("accepts controlPlaneMTU 9000 in host-trusted mode", func() {
				config := getMinimalDPFOperatorConfig(testNs.Name)
				config.Spec.Networking = &operatorv1.Networking{
					ControlPlaneMTU: ptr.To(9000),
				}
				Expect(testClient.Create(ctx, config)).To(Succeed())
				cleanupObjs = append(cleanupObjs, config)
			})
		})

		Context("Validate SPIFFE configuration", func() {
			DescribeTable("SPIFFE deploymentMode gate (creation)",
				func(zeroTrust bool, withSPIFFE bool, expectError bool, errorMessage string) {
					var config *operatorv1.DPFOperatorConfig
					if zeroTrust {
						config = getZeroTrustDPFOperatorConfig(testNs.Name)
					} else {
						config = getMinimalDPFOperatorConfig(testNs.Name)
					}
					if withSPIFFE {
						setSPIFFEConfig(config, getValidSPIFFEConfiguration())
					}
					validateConfigCreation(config, expectError, errorMessage, &cleanupObjs)
				},
				Entry("valid - zero-trust with spiffe", true, true, false, ""),
				Entry("valid - host-trusted without spiffe", false, false, false, ""),
				Entry("valid - zero-trust without spiffe", true, false, false, ""),
				Entry("invalid - host-trusted with spiffe", false, true, true, "spiffe configuration requires deploymentMode=zero-trust"),
			)

			DescribeTable("SPIFFEConfiguration field validation (zero-trust base)",
				func(mutate func(*operatorv1.SPIFFEConfiguration), expectError bool, errorMessage string) {
					config := getZeroTrustDPFOperatorConfig(testNs.Name)
					spiffe := getValidSPIFFEConfiguration()
					mutate(spiffe)
					setSPIFFEConfig(config, spiffe)
					validateConfigCreation(config, expectError, errorMessage, &cleanupObjs)
				},
				Entry("valid - full configuration", func(s *operatorv1.SPIFFEConfiguration) {}, false, ""),
				Entry("invalid - address missing port", func(s *operatorv1.SPIFFEConfiguration) {
					s.SPIREServerAddress = "spire-server.spire-system.svc"
				}, true, "spireServerAddress must be host:port"),
				Entry("invalid - address port out of range", func(s *operatorv1.SPIFFEConfiguration) {
					s.SPIREServerAddress = "spire-server.spire-system.svc:70000"
				}, true, "spireServerAddress port must be in 1-65535"),
				Entry("invalid - empty trust domain", func(s *operatorv1.SPIFFEConfiguration) {
					s.SPIRETrustDomain = ""
				}, true, "spireTrustDomain"),
				Entry("invalid - whitespace trust domain", func(s *operatorv1.SPIFFEConfiguration) {
					s.SPIRETrustDomain = " "
				}, true, "spireTrustDomain"),
				Entry("invalid - slash in trust domain", func(s *operatorv1.SPIFFEConfiguration) {
					s.SPIRETrustDomain = "cs.internal/extra"
				}, true, "spireTrustDomain"),
				// Pattern-valid but exceeds MaxLength=253: closes the admission/runtime skew where
				// an overlength all-lowercase domain passed admission yet failed spireTrustDomain().
				Entry("invalid - overlength trust domain", func(s *operatorv1.SPIFFEConfiguration) {
					s.SPIRETrustDomain = strings.Repeat("a", 254)
				}, true, "spireTrustDomain"),
				Entry("invalid - empty trust bundle name", func(s *operatorv1.SPIFFEConfiguration) {
					s.TrustBundle.Name = ""
				}, true, "trustBundle.name"),
			)

			DescribeTable("SPIFFE no-downgrade transition (K2)",
				func(setup func(*operatorv1.DPFOperatorConfig), endFn func(*operatorv1.DPFOperatorConfig), expectError bool, errorMessage string) {
					config := getZeroTrustDPFOperatorConfig(testNs.Name)
					setup(config)
					Expect(testClient.Create(ctx, config)).To(Succeed())
					cleanupObjs = append(cleanupObjs, config)

					endFn(config)
					err := testClient.Update(ctx, config)
					if expectError {
						Expect(err).To(HaveOccurred())
						Expect(err.Error()).To(ContainSubstring(errorMessage))
					} else {
						Expect(err).ToNot(HaveOccurred())
					}
				},
				Entry("ok - nil to nil", func(c *operatorv1.DPFOperatorConfig) {}, func(c *operatorv1.DPFOperatorConfig) {}, false, ""),
				Entry("ok - nil to set (add allowed)", func(c *operatorv1.DPFOperatorConfig) {}, func(c *operatorv1.DPFOperatorConfig) {
					setSPIFFEConfig(c, getValidSPIFFEConfiguration())
				}, false, ""),
				Entry("ok - empty security to set (add allowed)", func(c *operatorv1.DPFOperatorConfig) {
					c.Spec.Security = &operatorv1.SecurityConfiguration{}
				}, func(c *operatorv1.DPFOperatorConfig) {
					setSPIFFEConfig(c, getValidSPIFFEConfiguration())
				}, false, ""),
				Entry("ok - set to set", func(c *operatorv1.DPFOperatorConfig) {
					setSPIFFEConfig(c, getValidSPIFFEConfiguration())
				}, func(c *operatorv1.DPFOperatorConfig) {
					setSPIFFEConfig(c, getValidSPIFFEConfiguration())
				}, false, ""),
				Entry("reject - remove security block (downgrade)", func(c *operatorv1.DPFOperatorConfig) {
					setSPIFFEConfig(c, getValidSPIFFEConfiguration())
				}, func(c *operatorv1.DPFOperatorConfig) {
					c.Spec.Security = nil
				}, true, "spec.security.spiffe cannot be removed once set"),
				Entry("reject - clear spiffe under security (downgrade)", func(c *operatorv1.DPFOperatorConfig) {
					setSPIFFEConfig(c, getValidSPIFFEConfiguration())
				}, func(c *operatorv1.DPFOperatorConfig) {
					c.Spec.Security.SPIFFE = nil
				}, true, "spec.security.spiffe cannot be removed once set"),
			)
		})

		Context("Validate etcd encryption at rest", func() {
			It("accepts provider staticKey with a staticKey ref", func() {
				config := getMinimalDPFOperatorConfig(testNs.Name)
				config.Spec.KamajiClusterManager = &operatorv1.KamajiClusterManagerConfiguration{
					EtcdEncryptionAtRest: &operatorv1.EtcdEncryptionAtRestConfiguration{
						Provider:  operatorv1.EtcdEncryptionProviderStaticKey,
						StaticKey: &operatorv1.StaticKeyConfiguration{KeySecretRef: operatorv1.SecretKeyRef{Name: "etcd-key", Key: "key"}},
					},
				}
				validateConfigCreation(config, false, "", &cleanupObjs)
			})

			It("rejects provider staticKey without a staticKey ref", func() {
				config := getMinimalDPFOperatorConfig(testNs.Name)
				config.Spec.KamajiClusterManager = &operatorv1.KamajiClusterManagerConfiguration{
					EtcdEncryptionAtRest: &operatorv1.EtcdEncryptionAtRestConfiguration{Provider: operatorv1.EtcdEncryptionProviderStaticKey},
				}
				validateConfigCreation(config, true, "staticKey is required when provider is staticKey", &cleanupObjs)
			})

			It("rejects provider vaultKMS when spec.security.vaultKMS is absent", func() {
				config := getMinimalDPFOperatorConfig(testNs.Name)
				config.Spec.KamajiClusterManager = &operatorv1.KamajiClusterManagerConfiguration{
					EtcdEncryptionAtRest: &operatorv1.EtcdEncryptionAtRestConfiguration{Provider: operatorv1.EtcdEncryptionProviderVaultKMS},
				}
				validateConfigCreation(config, true, "requires spec.security.vaultKMS to be enabled", &cleanupObjs)
			})

			It("rejects provider vaultKMS when spec.security.vaultKMS is disabled", func() {
				config := getMinimalDPFOperatorConfig(testNs.Name)
				v := enabledVaultKMS()
				v.Disable = ptr.To(true)
				setVaultKMSConfig(config, v)
				config.Spec.KamajiClusterManager = &operatorv1.KamajiClusterManagerConfiguration{
					EtcdEncryptionAtRest: &operatorv1.EtcdEncryptionAtRestConfiguration{Provider: operatorv1.EtcdEncryptionProviderVaultKMS},
				}
				validateConfigCreation(config, true, "requires spec.security.vaultKMS to be enabled", &cleanupObjs)
			})

			It("accepts provider vaultKMS when spec.security.vaultKMS is enabled", func() {
				config := getMinimalDPFOperatorConfig(testNs.Name)
				setVaultKMSConfig(config, enabledVaultKMS())
				config.Spec.KamajiClusterManager = &operatorv1.KamajiClusterManagerConfiguration{
					EtcdEncryptionAtRest: &operatorv1.EtcdEncryptionAtRestConfiguration{Provider: operatorv1.EtcdEncryptionProviderVaultKMS},
				}
				validateConfigCreation(config, false, "", &cleanupObjs)
			})

			It("accepts provider vaultKMS when spec.security.vaultKMS disable is omitted", func() {
				config := getMinimalDPFOperatorConfig(testNs.Name)
				v := enabledVaultKMS()
				v.Disable = nil
				setVaultKMSConfig(config, v)
				config.Spec.KamajiClusterManager = &operatorv1.KamajiClusterManagerConfiguration{
					EtcdEncryptionAtRest: &operatorv1.EtcdEncryptionAtRestConfiguration{Provider: operatorv1.EtcdEncryptionProviderVaultKMS},
				}
				validateConfigCreation(config, false, "", &cleanupObjs)
			})

			It("rejects staticKey set together with provider vaultKMS", func() {
				config := getMinimalDPFOperatorConfig(testNs.Name)
				setVaultKMSConfig(config, enabledVaultKMS())
				config.Spec.KamajiClusterManager = &operatorv1.KamajiClusterManagerConfiguration{
					EtcdEncryptionAtRest: &operatorv1.EtcdEncryptionAtRestConfiguration{
						Provider:  operatorv1.EtcdEncryptionProviderVaultKMS,
						StaticKey: &operatorv1.StaticKeyConfiguration{KeySecretRef: operatorv1.SecretKeyRef{Name: "etcd-key", Key: "key"}},
					},
				}
				validateConfigCreation(config, true, "staticKey must not be set when provider is vaultKMS", &cleanupObjs)
			})
		})

		Context("Validate vaultKMS auth", func() {
			It("accepts an enabled vaultKMS with token auth", func() {
				config := getMinimalDPFOperatorConfig(testNs.Name)
				setVaultKMSConfig(config, enabledVaultKMS())
				validateConfigCreation(config, false, "", &cleanupObjs)
			})

			It("rejects token method without a token block", func() {
				config := getMinimalDPFOperatorConfig(testNs.Name)
				v := enabledVaultKMS()
				v.Auth = operatorv1.VaultKMSAuth{Method: operatorv1.VaultKMSAuthMethodToken}
				setVaultKMSConfig(config, v)
				validateConfigCreation(config, true, "token is required when method is token", &cleanupObjs)
			})

			It("rejects an auth block that does not match the method", func() {
				config := getMinimalDPFOperatorConfig(testNs.Name)
				v := enabledVaultKMS()
				v.Auth.AppRole = &operatorv1.VaultKMSAppRoleAuth{
					SecretName:  "approle",
					RoleIDKey:   "role_id",
					SecretIDKey: "secret_id",
				}
				setVaultKMSConfig(config, v)
				validateConfigCreation(config, true, "appRole must only be set when method is approle", &cleanupObjs)
			})

			It("accepts kubernetes auth", func() {
				config := getMinimalDPFOperatorConfig(testNs.Name)
				v := enabledVaultKMS()
				v.Auth = operatorv1.VaultKMSAuth{
					Method:     operatorv1.VaultKMSAuthMethodKubernetes,
					Kubernetes: &operatorv1.VaultKMSKubernetesAuth{Role: "dpf-kms", Audience: ptr.To("vault")},
				}
				setVaultKMSConfig(config, v)
				validateConfigCreation(config, false, "", &cleanupObjs)
			})

			It("accepts userpass auth", func() {
				config := getMinimalDPFOperatorConfig(testNs.Name)
				v := enabledVaultKMS()
				v.Auth = operatorv1.VaultKMSAuth{
					Method: operatorv1.VaultKMSAuthMethodUserpass,
					Userpass: &operatorv1.VaultKMSUserpassAuth{
						SecretName:  "vault-userpass",
						UsernameKey: "username",
						PasswordKey: "password",
					},
				}
				setVaultKMSConfig(config, v)
				validateConfigCreation(config, false, "", &cleanupObjs)
			})

			It("accepts optional token manager timing", func() {
				config := getMinimalDPFOperatorConfig(testNs.Name)
				v := enabledVaultKMS()
				v.TokenCheckIntervalSeconds = ptr.To[int32](30)
				v.LoginTimeoutSeconds = ptr.To[int32](10)
				setVaultKMSConfig(config, v)
				validateConfigCreation(config, false, "", &cleanupObjs)
			})

			It("accepts namespace", func() {
				config := getMinimalDPFOperatorConfig(testNs.Name)
				v := enabledVaultKMS()
				v.Namespace = ptr.To("platform/kubernetes")
				setVaultKMSConfig(config, v)
				validateConfigCreation(config, false, "", &cleanupObjs)
			})

			It("rejects tokenCheckIntervalSeconds below 5", func() {
				config := getMinimalDPFOperatorConfig(testNs.Name)
				v := enabledVaultKMS()
				v.TokenCheckIntervalSeconds = ptr.To[int32](4)
				setVaultKMSConfig(config, v)
				validateConfigCreation(config, true, "tokenCheckIntervalSeconds", &cleanupObjs)
			})

			It("rejects loginTimeoutSeconds below 1", func() {
				config := getMinimalDPFOperatorConfig(testNs.Name)
				v := enabledVaultKMS()
				v.LoginTimeoutSeconds = ptr.To[int32](0)
				setVaultKMSConfig(config, v)
				validateConfigCreation(config, true, "loginTimeoutSeconds", &cleanupObjs)
			})

			DescribeTable("rejects invalid hardened vaultKMS fields",
				func(mutate func(*operatorv1.VaultKMSConfiguration), errorMessage string) {
					config := getMinimalDPFOperatorConfig(testNs.Name)
					v := enabledVaultKMS()
					mutate(v)
					setVaultKMSConfig(config, v)
					validateConfigCreation(config, true, errorMessage, &cleanupObjs)
				},
				Entry("address without http scheme", func(v *operatorv1.VaultKMSConfiguration) {
					v.Address = "vault.example:8200"
				}, "Invalid value"),
				Entry("address with plaintext http scheme", func(v *operatorv1.VaultKMSConfiguration) {
					v.Address = "http://vault.example:8200"
				}, "Invalid value"),
				Entry("transit key contains slash", func(v *operatorv1.VaultKMSConfiguration) {
					v.Transit.KeyName = "k8s/etcd"
				}, "Invalid value"),
				Entry("transit mount is whitespace", func(v *operatorv1.VaultKMSConfiguration) {
					v.Transit.Mount = ptr.To("   ")
				}, "Invalid value"),
				Entry("transit mount is slash-only", func(v *operatorv1.VaultKMSConfiguration) {
					v.Transit.Mount = ptr.To("///")
				}, "Invalid value"),
				Entry("kubernetes audience is too long", func(v *operatorv1.VaultKMSConfiguration) {
					v.Auth = operatorv1.VaultKMSAuth{
						Method: operatorv1.VaultKMSAuthMethodKubernetes,
						Kubernetes: &operatorv1.VaultKMSKubernetesAuth{
							Role:     "dpf-kms",
							Audience: ptr.To(strings.Repeat("a", 513)),
						},
					}
				}, "Too long"),
			)

			It("rejects an explicit empty transit mount", func() {
				config := &unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": operatorv1.GroupVersion.String(),
						"kind":       operatorv1.DPFOperatorConfigKind,
						"metadata": map[string]interface{}{
							"name":      "test-config",
							"namespace": testNs.Name,
						},
						"spec": map[string]interface{}{
							"deploymentMode": "host-trusted",
							"provisioningController": map[string]interface{}{
								"bfbPVCName": "test-bfb-pvc",
							},
							"security": map[string]interface{}{
								"vaultKMS": map[string]interface{}{
									"disable": false,
									"address": "https://vault.example:8200",
									"transit": map[string]interface{}{
										"keyName": "k8s-etcd",
										"mount":   "",
									},
									"auth": map[string]interface{}{
										"method": "token",
										"token": map[string]interface{}{
											"tokenSecretRef": map[string]interface{}{
												"name": "vault-token",
												"key":  "token",
											},
										},
									},
								},
							},
						},
					},
				}

				err := testClient.Create(ctx, config)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("Invalid value"))
			})

			It("accepts jwt auth", func() {
				config := getMinimalDPFOperatorConfig(testNs.Name)
				v := enabledVaultKMS()
				v.Auth = operatorv1.VaultKMSAuth{
					Method: operatorv1.VaultKMSAuthMethodJWT,
					JWT:    &operatorv1.VaultKMSJWTAuth{Role: "dpf-kms", JWTSecretRef: operatorv1.SecretKeyRef{Name: "dpf-jwt", Key: "jwt"}},
				}
				setVaultKMSConfig(config, v)
				validateConfigCreation(config, false, "", &cleanupObjs)
			})

			It("rejects jwt method without a jwt block", func() {
				config := getMinimalDPFOperatorConfig(testNs.Name)
				v := enabledVaultKMS()
				v.Auth = operatorv1.VaultKMSAuth{Method: operatorv1.VaultKMSAuthMethodJWT}
				setVaultKMSConfig(config, v)
				validateConfigCreation(config, true, "jwt is required when method is jwt", &cleanupObjs)
			})

			It("rejects userpass method without a userpass block", func() {
				config := getMinimalDPFOperatorConfig(testNs.Name)
				v := enabledVaultKMS()
				v.Auth = operatorv1.VaultKMSAuth{Method: operatorv1.VaultKMSAuthMethodUserpass}
				setVaultKMSConfig(config, v)
				validateConfigCreation(config, true, "userpass is required when method is userpass", &cleanupObjs)
			})

			It("rejects a secret ref with an empty key", func() {
				config := getMinimalDPFOperatorConfig(testNs.Name)
				v := enabledVaultKMS()
				v.Auth.Token.TokenSecretRef.Key = ""
				setVaultKMSConfig(config, v)
				validateConfigCreation(config, true, "", &cleanupObjs)
			})
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
			DeploymentMode: operatorv1.DeploymentModeHostTrusted,
			ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
				BFBPersistentVolumeClaimName: ptr.To("test-bfb-pvc"),
			},
		},
	}
}

// getZeroTrustDPFOperatorConfig returns a minimal config switched to zero-trust mode with
// installViaRedfish, which is the pre-existing CEL prerequisite for any SPIFFE configuration.
func getZeroTrustDPFOperatorConfig(namespace string) *operatorv1.DPFOperatorConfig {
	config := getMinimalDPFOperatorConfig(namespace)
	config.Spec.DeploymentMode = operatorv1.DeploymentModeZeroTrust
	config.Spec.ProvisioningController.InstallInterface = &operatorv1.ProvisioningInstallInterface{
		InstallViaRedfish: &operatorv1.InstallViaRedfish{},
	}
	return config
}

func setSPIFFEConfig(config *operatorv1.DPFOperatorConfig, spiffe *operatorv1.SPIFFEConfiguration) {
	if spiffe == nil {
		if config.Spec.Security != nil {
			config.Spec.Security.SPIFFE = nil
		}
		return
	}
	if config.Spec.Security == nil {
		config.Spec.Security = &operatorv1.SecurityConfiguration{}
	}
	config.Spec.Security.SPIFFE = spiffe
}

func setVaultKMSConfig(config *operatorv1.DPFOperatorConfig, vaultKMS *operatorv1.VaultKMSConfiguration) {
	if config.Spec.Security == nil {
		config.Spec.Security = &operatorv1.SecurityConfiguration{}
	}
	config.Spec.Security.VaultKMS = vaultKMS
}

func getValidSPIFFEConfiguration() *operatorv1.SPIFFEConfiguration {
	return &operatorv1.SPIFFEConfiguration{
		SPIREServerAddress: "spire-server.spire-system.svc:8081",
		SPIRETrustDomain:   "cs.internal",
		KubeAPIAudience:    "https://kubernetes.default.svc",
		SPIREOIDCURL:       "https://spire-oidc.spire-system.svc",
		TrustBundle: operatorv1.SPIFFETrustBundleConfigMapReference{
			Name:      "spire-bundle",
			Namespace: "spire-system",
		},
	}
}

// enabledVaultKMS returns a minimal, valid, enabled VaultKMS configuration using token auth.
func enabledVaultKMS() *operatorv1.VaultKMSConfiguration {
	return &operatorv1.VaultKMSConfiguration{
		BaseComponentConfig: operatorv1.BaseComponentConfig{Disable: ptr.To(false)},
		Address:             "https://vault.example:8200",
		Transit:             operatorv1.VaultKMSTransit{KeyName: "k8s-etcd"},
		Auth: operatorv1.VaultKMSAuth{
			Method: operatorv1.VaultKMSAuthMethodToken,
			Token:  &operatorv1.VaultKMSTokenAuth{TokenSecretRef: operatorv1.SecretKeyRef{Name: "vault-token", Key: "token"}},
		},
	}
}
