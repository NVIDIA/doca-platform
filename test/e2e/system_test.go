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
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

//nolint:dupl
var _ = Describe("DPF System tests - Core", SpecPriority(CoreTestPriority), Labels{Domain.DPFSystem}, func() {

	BeforeEach(func() {
		if !input.HasDpuNodes() {
			return
		}
		for _, label := range CurrentSpecReport().Labels() {
			if label != Domain.RequiresNodes {
				continue
			}

			By("Waiting for provisioning")
			VerifyDPUClusterWithNodes(Ctx, GetProvisionDPUClustersInput())

			By("Waiting for DPU cluster pods to be ready")
			VerifyClusterPods(Ctx, DPUClusterClient[0], systemPodsToVerify)

			By("Waiting for DPFOperatorConfig to be ready")
			VerifyDPFOperatorConfigReady(Ctx, input.Client, 20*time.Minute)
		}
	})

	Context("DPU Deployment", Labels{Domain.ZeroTrust}, func() {
		It("create a DPUDeployment with its dependencies and ensure that the underlying objects are created", func() {
			ValidateDPUDeploymentCreation(Ctx, input)
		})
		It("verify DPUDeployment and DPUServiceInterface metrics", func() {
			ValidateDPUDeploymentMetrics(Ctx, input)
		})
		It("verify deletion on a disruptive upgrade with bad parameters so that the up to date DPUService never becomes ready", func() {
			ValidateDPUDeploymentDeletionWhileDisruptiveUpgradeInProgress(Ctx, input)
		})
	})

	Context("DPU Service IPAM", Labels{Domain.ZeroTrust}, func() {
		It("create an invalid DPUServiceIPAM and ensure that the webhook rejects the request", func() {
			ValidateDPUServiceIPAMCreationInvalid(Ctx, input)
		})
		It("create a DPUServiceIPAM with subnet split per node configuration and check NVIPAM IPPool is created to each cluster", func() {
			ValidateDPUServiceIPAMCreationSubnetSplit(Ctx, input)
		})
		It("verify DPUServiceIPAM metrics", func() {
			ValidateDPUServiceIPAMMetrics(Ctx, input)
		})
		It("delete the DPUServiceIPAM with subnet split per node configuration and check NVIPAM IPPool is deleted in each cluster", func() {
			ValidateDPUServiceIPAMMetricsDeletion(Ctx, input)
		})
		It("create a DPUServiceIPAM with cidr split in subnet per node configuration and check NVIPAM CIDRPool is created to each cluster", func() {
			ValidateDPUServiceIPAMCreationCidrSplit(Ctx, input)
		})
		It("delete the DPUServiceIPAM with cidr split in subnet per node configuration and check NVIPAM CIDRPool is deleted in each cluster", func() {
			ValidateDPUServiceIPAMDeletionCidrSplit(Ctx, input)
		})
	})

	Context("DPU Service Chain", Labels{Domain.ZeroTrust}, func() {
		It("create DPUServiceInterface and check that it is mirrored to each cluster", func() {
			ValidateDPUServiceInterfaceCreation(Ctx, input)
		})
		It("create DPUServiceChain and check that it is mirrored to each cluster", func() {
			ValidateDPUServiceChainCreation(Ctx, input)
		})
		It("verify DPUServiceChain metrics", func() {
			ValidateDPUServiceChainMetrics(Ctx, input)
		})
		It("delete the DPUServiceChain & DPUServiceInterface and check that the Sets are cleaned up", func() {
			ValidateDPUServiceChainDeletion(Ctx, input)
		})
	})

	Context("DPU Service Credential Request", Labels{Domain.ZeroTrust}, func() {
		It("create a DPUServiceCredentialRequest and check that the credentials are created", func() {
			ValidateDPUServiceCredentialRequestCreation(Ctx, input)
		})
		It("verify DPUServiceCredentialRequest metrics", func() {
			ValidateDPUServiceCredentialRequestMetrics(Ctx, input)
		})
		It("delete the DPUServiceCredentialRequest and check that the credentials are deleted", func() {
			ValidateDPUServiceCredentialRequestDeletion(Ctx, input)
		})
	})

	Context("DPU Service", func() {
		It("verify DPUService and DPUServiceInterface metrics", Labels{Domain.ZeroTrust}, func() {
			ValidateDPUServiceMetrics(Ctx, input)
		})
		It("delete the DPUServices and check that the applications are cleaned up", Labels{Domain.ZeroTrust}, func() {
			ValidateDPUServiceDeletion(Ctx, input)
		})
		It("verify that the ImagePullSecrets have been synced correctly and cleaned up", Labels{Domain.ZeroTrust, Domain.ImagePullSecretsSync}, func() {
			ValidateImagePullSecretsSync(Ctx, input)
		})
	})

	Context("DPU Service Template", Labels{Domain.ZeroTrust}, func() {
		It("create a DPUServiceTemplate with a chart that doesn't include annotations and expect no versions in status", func() {
			ValidateDPUServiceTemplateCreationNoAnnotations(Ctx, input)
		})
		It("create a DPUServiceTemplate with a chart that includes annotations and expect versions in status", func() {
			VerifyDPUServiceTemplateCreationWithAnnotations(Ctx, input)
		})
		It("verify DPUServiceTemplate metrics", func() {
			VerifyDPUServiceTemplateMetrics(Ctx, input)
		})
	})

	Context("Validate General DPF Metrics", Labels{Domain.ZeroTrust}, func() {
		It("should validate general DPF Metrics ", func() {
			ValidateGeneralDPFMetrics(Ctx, input)
		})
	})

	Context("VAP Deprecation Warnings", Labels{Domain.ZeroTrust}, func() {
		It("verify VAP emits a warning when a deprecated field is set", func() {
			ValidateVAPDeprecationWarnings(Ctx, input)
		})
	})

	Context("Observability", Labels{Domain.Observability, Domain.ZeroTrust}, func() {
		Context("Monitoring", func() {
			Context("KSM Metrics Collection", Labels{Domain.ZeroTrust}, func() {
				It("validate host cluster kube-state-metrics is accessible", func() {
					VerifyHostKSMMetricsCollection(Ctx)
				})
				It("validate DPU cluster kube-state-metrics is accessible", func() {
					By("Waiting for DPU cluster kube-state-metrics to be ready")
					VerifyClusterPods(Ctx, input.Client, []string{"in-cluster-kube-state-metrics"})
					By("Validating DPU cluster kube-state-metrics accessibility")
					VerifyDPUKSMMetricsCollection(Ctx, input)
				})
			})

			Context("Node Problem Detector", Labels{Domain.ZeroTrust, Domain.RequiresNodes}, func() {
				It("validate node-problem-detector is reporting DPU-specific node conditions", func() {
					if !input.HasDpuNodes() {
						Skip("Skip Node Problem Detector test as there are no DPU nodes")
					}
					By("Waiting for node-problem-detector to be ready")
					VerifyClusterPods(Ctx, DPUClusterClient[0], []string{"node-problem-detector"})
					By("Validating node-problem-detector conditions for DPU nodes")
					VerifyNodeProblemDetectorConditions(Ctx, input)
				})
			})
		})
		Context("Logging Infrastructure", func() {
			Context("Component Deployment", func() {
				It("should verify OpenTelemetry Collector DaemonSets running in host cluster", func() {
					By("Running in host cluster")
					VerifyClusterPods(Ctx, input.Client, []string{"opentelemetry-collector"})
				})
				It("should verify OpenTelemetry Collector DaemonSets running in DPU cluster", Labels{Domain.RequiresNodes}, func() {
					if !input.HasDpuNodes() {
						Skip("Skip test as there are no DPU nodes")
					}
					By("Running in DPUCluster")
					VerifyClusterPods(Ctx, DPUClusterClient[0], []string{"opentelemetry-collector"})
				})
			})

			Context("Configuration", func() {
				It("should verify DPU cluster collector configuration", Labels{Domain.RequiresNodes}, func() {
					if !input.HasDpuNodes() {
						Skip("Skip test as there are no DPU nodes")
					}
					ValidateDPUClusterOpenTelemetryConfiguration(Ctx, input)
				})
			})

			Context("Log Flow", func() {
				It("should collect and forward logs from management cluster to Loki", func() {
					ValidateManagementClusterLogFlow(Ctx, input)
				})

				It("should collect and forward logs from DPU cluster to Loki", Labels{Domain.RequiresNodes}, func() {
					if !input.HasDpuNodes() {
						Skip("Skip test as there are no DPU nodes")
					}
					ValidateDPUClusterLogFlow(Ctx, input)
				})
			})
		})
	})
	Context("DPU Agent", Labels{Domain.ZeroTrust, Domain.RequiresNodes}, func() {
		It("validate DPU agent has reported status on all provisioned DPUs", func() {
			ValidateDPUAgentStatus(Ctx, input, provisioningv1.AgentStatus{
				RebootMethod:        ptr.To(provisioningv1.RebootMethodNoAction),
				RebootSequenceCount: ptr.To(int32(0)),
				Conditions: []metav1.Condition{
					{Type: "KernelModuleLoaded", Status: metav1.ConditionTrue},
					{Type: "NetworkConfigured", Status: metav1.ConditionTrue},
					{Type: "NetworkChecked", Status: metav1.ConditionTrue},
					{Type: "LastStartupTimeReported", Status: metav1.ConditionTrue},
					{Type: "DPURetrieved", Status: metav1.ConditionTrue},
					{Type: "DNSConfigured", Status: metav1.ConditionTrue},
					{Type: "StaticFilesVerified", Status: metav1.ConditionTrue},
					{Type: "BuiltinKubeletRemoved", Status: metav1.ConditionTrue},
					{Type: "SysctlParametersSet", Status: metav1.ConditionTrue},
					{Type: "SysctlParametersChecked", Status: metav1.ConditionTrue},
					{Type: "KernelCmdLineConfigured", Status: metav1.ConditionTrue},
					{Type: "ContainerdConfigured", Status: metav1.ConditionTrue},
					{Type: "DpuModeEnsured", Status: metav1.ConditionTrue},
					{Type: "NVConfigApplied", Status: metav1.ConditionTrue},
					{Type: "RebootHandled", Status: metav1.ConditionTrue},
					{Type: "KernelCmdLineChecked", Status: metav1.ConditionTrue},
					{Type: "SFCreated", Status: metav1.ConditionTrue},
					{Type: "VFMacSet", Status: metav1.ConditionTrue},
					{Type: "OVSScriptRun", Status: metav1.ConditionTrue},
					{Type: "KubeletConfigured", Status: metav1.ConditionTrue},
					{Type: "KubeletStarted", Status: metav1.ConditionTrue},
				},
			})
		})
	})

	Context("DPU Service Kata Containers", Labels{Domain.RequiresNodes}, func() {
		It("deploy a DPUService pod with kata-qemu RuntimeClass and an SF", func() {
			ValidateDPUServiceKataRuntimeClass(Ctx, input)
		})
	})

	// Config Ports check is not valid for ZeroTrust
	Context("DPU Service Config Ports", Labels{Domain.RequiresNodes}, Serial, func() {
		It("expose ConfigPorts via DPUService and test reachability", func() {
			ValidateDPUServiceConfigPorts(Ctx, input)
		})
	})

	Context("NodeSRIOVDevicePluginController", func() {
		It("verify the webhook rejects invalid NodeSRIOVDevicePluginConfig", func() {
			ValidateNodeSRIOVDevicePluginWebhookRejectsInvalid(Ctx, input)
		})
		It("verify a valid NodeSRIOVDevicePluginConfig is accepted and can be deleted", func() {
			ValidateNodeSRIOVDevicePluginConfigValidCreate(Ctx, input)
		})
	})

	Context("NodeSRIOVDevicePluginController Managed Pods", Labels{Domain.RequiresNodes}, Serial, Ordered, func() {
		It("verify node SRIOV device plugin management", func() {
			ValidateNodeSRIOVDevicePluginManagement(Ctx, input)
		})
	})

	Context("Validate DPU Operator Config", Serial, Ordered, func() {
		It("verify system component overrides", Labels{Domain.ZeroTrust}, func() {
			ValidateDPFOperatorBaseConfiguration(Ctx, input)
		})
		It("verify that the current MTU in the DPU clusters flannel configmap is 1500", Labels{Domain.ZeroTrust}, func() {
			ValidateDPFOperatorMTUCurrentConfiguration(Ctx, input)
		})
		It("change the MTUs in the operatorConfig and verify that DPU Clusters are updated", Labels{Domain.ZeroTrust}, func() {
			ValidateDPFOperatorMTUConfigurationChange(Ctx, input)
		})
		It("verify overrides path setting for system DPUServices", Labels{Domain.ZeroTrust}, func() {
			ValidateDPFOperatorPathConfiguration(Ctx, input)
		})
		It("change the MaxDPUParallelInstallations in the operatorConfig and verify that the provisioning controller is restarted", Labels{Domain.ZeroTrust}, func() {
			ValidateDPFOperatorMaxDPUParallelInstallations(Ctx, input)
		})
		It("change the flannel podCIDR in the operatorConfig and check that it is set", Labels{Domain.ZeroTrust}, func() {
			ValidateDPFOperatorFlannelPodCIDRChange(Ctx, input)
		})

		// This test triggers reprovisioning, which might disrupt other tests relying on provisioned nodes.
		// Added BeforeEach wait for the nodes to be provisioned for the test with Domain.RequiresNodes
		// DMS check is not valid for ZeroTrust
		It("verify Kubernetes API related variables are propagated correctly", Labels{Domain.RequiresNodes}, func() {
			ValidateDPFOperatorKubernetesAPIServerVIPAndPort(Ctx, input)
		})
	})

	// These tests delete the existing DPUSet created in the beginning of the testing suite, and create a DPUDeployment
	// instead. The DPUDeployment should not be removed until all the tests in the e2e suite are run as the DPUs will be
	// deleted.
	Context("Validate DPUDeployment full creation", Serial, Ordered, func() {
		BeforeAll(func() {
			By("Should validate DPUDeployment and underlying objects creation")
			ValidateDPUDeploymentFullCreation(Ctx, input)
		})
		It("should validate DPUDeployment becomes ready", Labels{Domain.ZeroTrust}, func() {
			VerifyDPUDeploymentIsReady(Ctx, input)
		})
		It("should validate DPUDeployment disruptive upgrade of standard DPUServices", Labels{Domain.ZeroTrust}, func() {
			if isGinkgoLabelApplied(Domain.ZeroTrust) {
				ValidateDPUDeploymentDPUServiceDisruptiveUpgradeHold(Ctx, input)
			} else {
				ValidateDPUDeploymentDPUServiceDisruptiveUpgradeDrain(Ctx, input)
			}
		})
		It("should validate DPUDeployment disruptive upgrade of standard DPUServices with bad configuration", func() {
			ValidateDPUDeploymentDPUServiceDisruptiveUpgradeBadConfigurationAndBack(Ctx, input)
		})
		It("should validate DPUDeployment disruptive upgrade of in-cluster DPUServices", func() {
			ValidateDPUDeploymentInClusterDPUServiceDisruptiveUpgrade(Ctx, input)
		})
		It("should validate DPUDeployment disruptive upgrade of DPUServiceChain", Labels{Domain.ZeroTrust}, func() {
			if isGinkgoLabelApplied(Domain.ZeroTrust) {
				ValidateDPUDeploymentDPUServiceChainDisruptiveUpgradeHold(Ctx, input)
			} else {
				ValidateDPUDeploymentDPUServiceChainDisruptiveUpgradeDrain(Ctx, input)
			}
		})
		It("should validate DPUDeployment disruptive upgrade of DPUServiceChain with bad configuration", func() {
			ValidateDPUDeploymentDPUServiceChainDisruptiveUpgradeBadConfigurationAndBack(Ctx, input)
		})
	})

	// The actual DPFOperatorConfig removal happens in AfterSuite but we need to ensure some resources exist before we
	// proceed with the removal.
	Context("Validate DPFOperatorConfig deletion", Serial, Labels{Domain.ZeroTrust}, func() {
		It("should validate the expected objects exist before leaving the Container node", func() {
			ValidateDPFOperatorConfigCleanupPrerequisites(Ctx, input)
		})
	})
})
