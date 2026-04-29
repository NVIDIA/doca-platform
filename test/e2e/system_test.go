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

package e2e

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"strconv"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var input *systemTestInput
var vpcOvnInput = &vpcOvnTestInput{}

func SetInput() {
	By("Validating the input")
	validateFlags()

	By("Get control plane IP")
	controlPlaneIP := getClusterControlPlaneIP(ctx, testClient)

	By("Setting operatorConfig for the test")
	var bfbPVCName *string
	if conf.ProvisioningControllerPVCPath != nil {
		bfbPVCName = ptr.To("bfb-pvc")
	}
	dpfOperatorConfig := &operatorv1.DPFOperatorConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configName,
			Namespace: dpfOperatorSystemNamespace,
			Labels:    CleanupScope.Suite,
		},
		Spec: operatorv1.DPFOperatorConfigSpec{
			DeploymentMode: operatorv1.DeploymentModeTrustedHost,
			ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
				BFBPersistentVolumeClaimName: bfbPVCName,
			},
			StaticClusterManager: &operatorv1.StaticClusterManagerConfiguration{
				BaseComponentConfig: operatorv1.BaseComponentConfig{
					Disable: ptr.To(false),
				},
			},
			// Disable the Kamaji cluster manager so only one cluster manager is running.
			// TODO: Enable Kamaji by default in the e2e tests.
			KamajiClusterManager: &operatorv1.KamajiClusterManagerConfiguration{
				BaseComponentConfig: operatorv1.BaseComponentConfig{
					Disable: ptr.To(false),
				},
			},
			Monitoring: &operatorv1.MonitoringConfiguration{
				Disable: ptr.To(false),
				OpenTelemetryCollector: &operatorv1.OpenTelemetryCollectorConfiguration{
					Logging: &operatorv1.OpenTelemetryCollectorLoggingConfiguration{
						Endpoint: fmt.Sprintf("%s%s:%d", otelEndpointSchema, controlPlaneIP, otelNodePort),
					},
				},
			},
			NodeSRIOVDevicePluginController: &operatorv1.NodeSRIOVDevicePluginControllerConfiguration{
				BaseComponentConfig: operatorv1.BaseComponentConfig{
					Disable: ptr.To(false),
				},
			},
			ImagePullSecrets: []string{dpfPullSecretName, "pull-secret-extra"},
		},
	}
	if isGinkgoLabelApplied(Domain.ZeroTrust) {
		dpfOperatorConfig.Spec.DeploymentMode = operatorv1.DeploymentModeZeroTrust
		dpfOperatorConfig.Spec.StaticClusterManager.BaseComponentConfig.Disable = ptr.To(true)
		dpfOperatorConfig.Spec.KamajiClusterManager.BaseComponentConfig.Disable = ptr.To(false)
		dpfOperatorConfig.Spec.NodeSRIOVDevicePluginController.BaseComponentConfig.Disable = ptr.To(true)
		By(fmt.Sprintf("Zero trust mode is applied to operatorConfig with trusted host IP %s", controlPlaneIP))
		dpfOperatorConfig.Spec.ProvisioningController.InstallInterface = &operatorv1.ProvisioningInstallInterface{
			InstallViaRedfish: &operatorv1.InstallViaRedfish{
				BFBRegistryAddress:   fmt.Sprintf("%s:%d", controlPlaneIP, bfbRegistryNodePort),
				SkipDPUNodeDiscovery: ptr.To(false),
			},
		}
		dpfOperatorConfig.Spec.DPUDetector = &operatorv1.DPUDetectorConfiguration{
			BaseComponentConfig: operatorv1.BaseComponentConfig{
				Disable: ptr.To(true),
			},
		}
		apiServerPort := 443
		if u, err := url.Parse(restConfig.Host); err == nil {
			if p := u.Port(); p != "" {
				if parsed, err := strconv.Atoi(p); err == nil {
					apiServerPort = parsed
				}
			}
		}
		By(fmt.Sprintf("Using API server VIP %s:%d for zero-trust kubeconfig", controlPlaneIP, apiServerPort))
		if dpfOperatorConfig.Spec.Overrides == nil {
			dpfOperatorConfig.Spec.Overrides = &operatorv1.Overrides{}
		}
		dpfOperatorConfig.Spec.Overrides.KubernetesAPIServerVIP = ptr.To(controlPlaneIP)
		dpfOperatorConfig.Spec.Overrides.KubernetesAPIServerPort = ptr.To(apiServerPort)
	}

	if isGinkgoLabelApplied(Domain.Scale) {
		// For scale environments, the nodes are fake, therefore we can't have DPUDetector running
		dpfOperatorConfig.Spec.DPUDetector = &operatorv1.DPUDetectorConfiguration{
			BaseComponentConfig: operatorv1.BaseComponentConfig{
				Disable: ptr.To(true),
			},
		}
	}

	if prereqsNamespace != "" {
		if dpfOperatorConfig.Spec.Overrides == nil {
			dpfOperatorConfig.Spec.Overrides = &operatorv1.Overrides{}
		}

		dpfOperatorConfig.Spec.Overrides.ArgoCDNamespace = ptr.To(prereqsNamespace)
	}

	input = &systemTestInput{
		namespace:        dpfOperatorSystemNamespace,
		config:           dpfOperatorConfig,
		pullSecretNames:  dpfOperatorConfig.Spec.ImagePullSecrets,
		client:           testClient,
		restConfig:       restConfig,
		cleanupFlags:     cleanupFlags,
		bfbImageURL:      bfbImageURL,
		HostRebootScript: HostRebootScript,
	}
	input.applyConfig(*conf)
}

func SystemSetupBeforeSuite() {
	if Label(Domain.Scale).MatchesLabelFilter(GinkgoLabelFilter()) {
		CreateDPUWorkerNodes(ctx, input.numberOfDPUNodes)
	}

	AnnotateAndLabelNodes(ctx, input.client, input.useExternalNodeReboot)

	if ngcAPIKey != "" {
		createNGCImagePullSecret(ctx, input.client)
	}

	By("Deploy DPF System components")
	DeployDPFSystemComponents(ctx, DeployDPFSystemComponentsInput{
		systemNamespace:           input.namespace,
		operatorConfig:            input.config,
		ImagePullSecrets:          input.pullSecretNames,
		ProvisioningControllerPVC: input.pvc,
		dpuDiscovery:              input.dpuDiscovery,
		client:                    input.client,
		numberOfDPUNodes:          input.numberOfDPUNodes,
	})
}

// createNGCImagePullSecret creates a secret to be able to pull images from NGC, this secret can be used by DPUservices and should not be used for core components.
func createNGCImagePullSecret(ctx context.Context, testClient client.Client) {
	// Docker registry credentials
	registry := "nvcr.io"
	username := "$oauthtoken"
	password := ngcAPIKey

	// Create the auth string
	auth := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", username, password)))

	// Build the config.json structure
	dockerConfig := map[string]interface{}{
		"auths": map[string]interface{}{
			registry: map[string]string{
				"auth": auth,
			},
		},
	}

	dockerConfigJSON, err := json.Marshal(dockerConfig)
	Expect(err).ToNot(HaveOccurred())

	labels := maps.Clone(CleanupScope.Suite)
	labels["dpu.nvidia.com/image-pull-secret"] = ""

	// Create the Secret object
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ngcPullSecretName,
			Namespace: dpfOperatorSystemNamespace,
			Labels:    labels,
		},
		Type: corev1.SecretTypeDockerConfigJson,
		Data: map[string][]byte{
			".dockerconfigjson": dockerConfigJSON,
		},
	}

	// Create the secret
	Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, secret))).NotTo(HaveOccurred())
}

func AnnotateAndLabelNodes(ctx context.Context, c client.Client, useExternalNodeReboot bool) {
	nodeAnnotations := make(map[string]string)
	nodeLabels := make(map[string]string)

	if useExternalNodeReboot {
		nodeLabels["provisioning.dpu.nvidia.com/reboot-method"] = "external"
		nodeLabels["provisioning.dpu.nvidia.com/dpu-reboot-after-install"] = ""
	}

	if len(nodeAnnotations) == 0 && len(nodeLabels) == 0 {
		return
	}

	By("Annotate and Label nodes in the main cluster")
	Eventually(func(g Gomega) {
		nodes := &corev1.NodeList{}
		g.Expect(c.List(ctx, nodes)).To(Succeed())
		for _, node := range nodes.Items {
			original := node.DeepCopy()
			annotations := node.GetAnnotations()
			if annotations == nil {
				annotations = map[string]string{}
			}
			for k, v := range nodeAnnotations {
				annotations[k] = v
			}
			node.SetAnnotations(annotations)

			labels := node.GetLabels()
			if labels == nil {
				labels = map[string]string{}
			}
			for k, v := range nodeLabels {
				labels[k] = v
			}
			node.SetLabels(labels)

			g.Expect(c.Patch(ctx, &node, client.MergeFrom(original))).To(Succeed())
		}
	}).WithTimeout(10 * time.Second).Should(Succeed())
}

//nolint:dupl
var _ = Describe("DPF System tests - Core", SpecPriority(CoreTestPriority), Labels{Domain.DPFSystem}, func() {

	BeforeEach(func() {
		if !input.hasDpuNodes() {
			return
		}
		for _, label := range CurrentSpecReport().Labels() {
			if label != Domain.RequiresNodes {
				continue
			}

			By("Waiting for provisioning")
			VerifyDPUClusterWithNodes(ctx, getProvisionDPUClustersInput())

			By("Waiting for DPU cluster pods to be ready")
			VerifyClusterPods(ctx, dpuClusterClient[0], systemPodsToVerify)

			By("Waiting for DPFOperatorConfig to be ready")
			VerifyDPFOperatorConfigReady(ctx, input.client, 20*time.Minute)
		}
	})

	Context("DPU Deployment", Labels{Domain.ZeroTrust}, func() {
		It("create a DPUDeployment with its dependencies and ensure that the underlying objects are created", func() {
			ValidateDPUDeploymentCreation(ctx, input)
		})
		It("verify DPUDeployment and DPUServiceInterface metrics", func() {
			ValidateDPUDeploymentMetrics(ctx, input)
		})
		It("verify deletion on a disruptive upgrade with bad parameters so that the up to date DPUService never becomes ready", func() {
			ValidateDPUDeploymentDeletionWhileDisruptiveUpgradeInProgress(ctx, input)
		})
	})

	Context("DPU Service IPAM", Labels{Domain.ZeroTrust}, func() {
		It("create an invalid DPUServiceIPAM and ensure that the webhook rejects the request", func() {
			ValidateDPUServiceIPAMCreationInvalid(ctx, input)
		})
		It("create a DPUServiceIPAM with subnet split per node configuration and check NVIPAM IPPool is created to each cluster", func() {
			ValidateDPUServiceIPAMCreationSubnetSplit(ctx, input)
		})
		It("verify DPUServiceIPAM metrics", func() {
			ValidateDPUServiceIPAMMetrics(ctx, input)
		})
		It("delete the DPUServiceIPAM with subnet split per node configuration and check NVIPAM IPPool is deleted in each cluster", func() {
			ValidateDPUServiceIPAMMetricsDeletion(ctx, input)
		})
		It("create a DPUServiceIPAM with cidr split in subnet per node configuration and check NVIPAM CIDRPool is created to each cluster", func() {
			ValidateDPUServiceIPAMCreationCidrSplit(ctx, input)
		})
		It("delete the DPUServiceIPAM with cidr split in subnet per node configuration and check NVIPAM CIDRPool is deleted in each cluster", func() {
			ValidateDPUServiceIPAMDeletionCidrSplit(ctx, input)
		})
	})

	Context("DPU Service Chain", Labels{Domain.ZeroTrust}, func() {
		It("create DPUServiceInterface and check that it is mirrored to each cluster", func() {
			ValidateDPUServiceInterfaceCreation(ctx, input)
		})
		It("create DPUServiceChain and check that it is mirrored to each cluster", func() {
			ValidateDPUServiceChainCreation(ctx, input)
		})
		It("verify DPUServiceChain metrics", func() {
			ValidateDPUServiceChainMetrics(ctx, input)
		})
		It("delete the DPUServiceChain & DPUServiceInterface and check that the Sets are cleaned up", func() {
			ValidateDPUServiceChainDeletion(ctx, input)
		})
	})

	Context("DPU Service Credential Request", Labels{Domain.ZeroTrust}, func() {
		It("create a DPUServiceCredentialRequest and check that the credentials are created", func() {
			ValidateDPUServiceCredentialRequestCreation(ctx, input)
		})
		It("verify DPUServiceCredentialRequest metrics", func() {
			ValidateDPUServiceCredentialRequestMetrics(ctx, input)
		})
		It("delete the DPUServiceCredentialRequest and check that the credentials are deleted", func() {
			ValidateDPUServiceCredentialRequestDeletion(ctx, input)
		})
	})

	Context("DPU Service", func() {
		It("verify DPUService and DPUServiceInterface metrics", Labels{Domain.ZeroTrust}, func() {
			ValidateDPUServiceMetrics(ctx, input)
		})
		It("delete the DPUServices and check that the applications are cleaned up", Labels{Domain.ZeroTrust}, func() {
			ValidateDPUServiceDeletion(ctx, input)
		})
		It("verify that the ImagePullSecrets have been synced correctly and cleaned up", Labels{Domain.ZeroTrust, Domain.ImagePullSecretsSync}, func() {
			ValidateImagePullSecretsSync(ctx, input)
		})
	})

	Context("DPU Service Template", Labels{Domain.ZeroTrust}, func() {
		It("create a DPUServiceTemplate with a chart that doesn't include annotations and expect no versions in status", func() {
			ValidateDPUServiceTemplateCreationNoAnnotations(ctx, input)
		})
		It("create a DPUServiceTemplate with a chart that includes annotations and expect versions in status", func() {
			VerifyDPUServiceTemplateCreationWithAnnotations(ctx, input)
		})
		It("verify DPUServiceTemplate metrics", func() {
			VerifyDPUServiceTemplateMetrics(ctx, input)
		})
	})

	Context("Validate General DPF Metrics", Labels{Domain.ZeroTrust}, func() {
		It("should validate general DPF Metrics ", func() {
			ValidateGeneralDPFMetrics(ctx, input)
		})
	})

	Context("Observability", Labels{Domain.Observability, Domain.ZeroTrust}, func() {
		Context("Monitoring", func() {
			Context("KSM Metrics Collection", Labels{Domain.ZeroTrust}, func() {
				It("validate host cluster kube-state-metrics is accessible", func() {
					VerifyHostKSMMetricsCollection(ctx)
				})
				It("validate DPU cluster kube-state-metrics is accessible", func() {
					By("Waiting for DPU cluster kube-state-metrics to be ready")
					VerifyClusterPods(ctx, input.client, []string{"in-cluster-kube-state-metrics"})
					By("Validating DPU cluster kube-state-metrics accessibility")
					VerifyDPUKSMMetricsCollection(ctx, input)
				})
			})

			Context("Node Problem Detector", Labels{Domain.ZeroTrust, Domain.RequiresNodes}, func() {
				It("validate node-problem-detector is reporting DPU-specific node conditions", func() {
					if !input.hasDpuNodes() {
						Skip("Skip Node Problem Detector test as there are no DPU nodes")
					}
					By("Waiting for node-problem-detector to be ready")
					VerifyClusterPods(ctx, dpuClusterClient[0], []string{"node-problem-detector"})
					By("Validating node-problem-detector conditions for DPU nodes")
					VerifyNodeProblemDetectorConditions(ctx, input)
				})
			})
		})
		Context("Logging Infrastructure", func() {
			Context("Component Deployment", func() {
				It("should verify OpenTelemetry Collector DaemonSets running in host cluster", func() {
					By("Running in host cluster")
					VerifyClusterPods(ctx, input.client, []string{"opentelemetry-collector"})
				})
				It("should verify OpenTelemetry Collector DaemonSets running in DPU cluster", Labels{Domain.RequiresNodes}, func() {
					if !input.hasDpuNodes() {
						Skip("Skip test as there are no DPU nodes")
					}
					By("Running in DPUCluster")
					VerifyClusterPods(ctx, dpuClusterClient[0], []string{"opentelemetry-collector"})
				})
			})

			Context("Configuration", func() {
				It("should verify DPU cluster collector configuration", Labels{Domain.RequiresNodes}, func() {
					if !input.hasDpuNodes() {
						Skip("Skip test as there are no DPU nodes")
					}
					ValidateDPUClusterOpenTelemetryConfiguration(ctx, input)
				})
			})

			Context("Log Flow", func() {
				It("should collect and forward logs from management cluster to Loki", func() {
					ValidateManagementClusterLogFlow(ctx, input)
				})

				It("should collect and forward logs from DPU cluster to Loki", Labels{Domain.RequiresNodes}, func() {
					if !input.hasDpuNodes() {
						Skip("Skip test as there are no DPU nodes")
					}
					ValidateDPUClusterLogFlow(ctx, input)
				})
			})
		})
	})

	// Config Ports check is not valid for ZeroTrust
	Context("DPU Service Config Ports", Labels{Domain.RequiresNodes}, Serial, func() {
		It("expose ConfigPorts via DPUService and test reachability", func() {
			ValidateDPUServiceConfigPorts(ctx, input)
		})
	})

	Context("NodeSRIOVDevicePluginController", func() {
		It("verify the webhook rejects invalid NodeSRIOVDevicePluginConfig", func() {
			ValidateNodeSRIOVDevicePluginWebhookRejectsInvalid(ctx, input)
		})
		It("verify a valid NodeSRIOVDevicePluginConfig is accepted and can be deleted", func() {
			ValidateNodeSRIOVDevicePluginConfigValidCreate(ctx, input)
		})
	})

	Context("NodeSRIOVDevicePluginController Managed Pods", Labels{Domain.RequiresNodes}, Serial, Ordered, func() {
		It("verify node SRIOV device plugin management", func() {
			ValidateNodeSRIOVDevicePluginManagement(ctx, input)
		})
	})

	Context("Validate DPU Operator Config", Serial, Ordered, func() {
		It("verify system component overrides", Labels{Domain.ZeroTrust}, func() {
			ValidateDPFOperatorBaseConfiguration(ctx, input)
		})
		It("verify that the current MTU in the DPU clusters flannel configmap is 1500", Labels{Domain.ZeroTrust}, func() {
			ValidateDPFOperatorMTUCurrentConfiguration(ctx, input)
		})
		It("change the MTUs in the operatorConfig and verify that DPU Clusters are updated", Labels{Domain.ZeroTrust}, func() {
			ValidateDPFOperatorMTUConfigurationChange(ctx, input)
		})
		It("verify overrides path setting for system DPUServices", Labels{Domain.ZeroTrust}, func() {
			ValidateDPFOperatorPathConfiguration(ctx, input)
		})
		It("change the MaxDPUParallelInstallations in the operatorConfig and verify that the provisioning controller is restarted", Labels{Domain.ZeroTrust}, func() {
			ValidateDPFOperatorMaxDPUParallelInstallations(ctx, input)
		})
		It("change the flannel podCIDR in the operatorConfig and check that it is set", Labels{Domain.ZeroTrust}, func() {
			ValidateDPFOperatorFlannelPodCIDRChange(ctx, input)
		})

		// This test triggers reprovisioning, which might disrupt other tests relying on provisioned nodes.
		// Added BeforeEach wait for the nodes to be provisioned for the test with Domain.RequiresNodes
		// DMS check is not valid for ZeroTrust
		It("verify Kubernetes API related variables are propagated correctly", Labels{Domain.RequiresNodes}, func() {
			ValidateDPFOperatorKubernetesAPIServerVIPAndPort(ctx, input)
		})
	})

	// These tests delete the existing DPUSet created in the beginning of the testing suite, and create a DPUDeployment
	// instead. The DPUDeployment should not be removed until all the tests in the e2e suite are run as the DPUs will be
	// deleted.
	Context("Validate DPUDeployment full creation", Serial, Ordered, func() {
		BeforeAll(func() {
			By("Should validate DPUDeployment and underlying objects creation")
			ValidateDPUDeploymentFullCreation(ctx, input)
		})
		It("should validate DPUDeployment becomes ready", Labels{Domain.ZeroTrust}, func() {
			VerifyDPUDeploymentIsReady(ctx, input)
		})
		It("should validate DPUDeployment disruptive upgrade of standard DPUServices", Labels{Domain.ZeroTrust}, func() {
			if isGinkgoLabelApplied(Domain.ZeroTrust) {
				ValidateDPUDeploymentDPUServiceDisruptiveUpgradeHold(ctx, input)
			} else {
				ValidateDPUDeploymentDPUServiceDisruptiveUpgradeDrain(ctx, input)
			}
		})
		It("should validate DPUDeployment disruptive upgrade of standard DPUServices with bad configuration", func() {
			ValidateDPUDeploymentDPUServiceDisruptiveUpgradeBadConfigurationAndBack(ctx, input)
		})
		It("should validate DPUDeployment disruptive upgrade of in-cluster DPUServices", func() {
			ValidateDPUDeploymentInClusterDPUServiceDisruptiveUpgrade(ctx, input)
		})
		It("should validate DPUDeployment disruptive upgrade of DPUServiceChain", Labels{Domain.ZeroTrust}, func() {
			if isGinkgoLabelApplied(Domain.ZeroTrust) {
				ValidateDPUDeploymentDPUServiceChainDisruptiveUpgradeHold(ctx, input)
			} else {
				ValidateDPUDeploymentDPUServiceChainDisruptiveUpgradeDrain(ctx, input)
			}
		})
		It("should validate DPUDeployment disruptive upgrade of DPUServiceChain with bad configuration", func() {
			ValidateDPUDeploymentDPUServiceChainDisruptiveUpgradeBadConfigurationAndBack(ctx, input)
		})
	})

	// The actual DPFOperatorConfig removal happens in AfterSuite but we need to ensure some resources exist before we
	// proceed with the removal.
	Context("Validate DPFOperatorConfig deletion", Serial, Labels{Domain.ZeroTrust}, func() {
		It("should validate the expected objects exist before leaving the Container node", func() {
			ValidateDPFOperatorConfigCleanupPrerequisites(ctx, input)
		})
	})
})

func getProvisionDPUClustersInput() ProvisionDPUClustersInput {
	return ProvisionDPUClustersInput{
		numberOfDPUNodes:        input.numberOfDPUNodes,
		numberOfDPUsPerNode:     input.numberOfDPUsPerNode,
		dpuClusterPrerequisites: input.dpuClusterPrerequisites,
		dpuClusters:             input.dpuClusters,
		dpuSet:                  input.dpuSet,
		bfb:                     input.bfb,
		dpuFlavor:               input.dpuFlavor,
		client:                  input.client,
		bfbImageURL:             input.bfbImageURL,
		restConfig:              restConfig,
		HostRebootScript:        input.HostRebootScript,
	}
}
