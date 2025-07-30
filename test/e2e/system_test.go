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
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	"github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var input *systemTestInput

func SetInput() {
	By("Setting operatorConfig for the test")
	dpfOperatorConfig := &operatorv1.DPFOperatorConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configName,
			Namespace: dpfOperatorSystemNamespace,
			Labels:    afterAllCleanupLabels,
		},
		Spec: operatorv1.DPFOperatorConfigSpec{
			ProvisioningController: operatorv1.ProvisioningControllerConfiguration{
				BFBPersistentVolumeClaimName: "bfb-pvc",
			},
			StaticClusterManager: &operatorv1.StaticClusterManagerConfiguration{
				Disable: ptr.To(false),
			},
			// Disable the Kamaji cluster manager so only one cluster manager is running.
			// TODO: Enable Kamaji by default in the e2e tests.
			KamajiClusterManager: &operatorv1.KamajiClusterManagerConfiguration{
				Disable: ptr.To(true),
			},
			ImagePullSecrets: []string{"dpf-pull-secret", "pull-secret-extra"},
		},
	}

	input = &systemTestInput{
		namespace:       dpfOperatorSystemNamespace,
		config:          dpfOperatorConfig,
		pullSecretNames: dpfOperatorConfig.Spec.ImagePullSecrets,
		client:          testClient,
		restConfig:      restConfig,
		skipCleanup:     skipCleanup,
		bfbImageURL:     bfbImageURL,
	}
	input.applyConfig(*conf)
}

func SystemSetupBeforeSuite() {
	if Label(scaleLabel).MatchesLabelFilter(GinkgoLabelFilter()) {
		CreateDPUWorkerNodes(ctx, input.numberOfDPUNodes)
	}

	AnnotateAndLabelNodes(ctx, input.client, input.useExternalNodeReboot)

	By("Deploy DPF System components")
	DeployDPFSystemComponents(ctx, DeployDPFSystemComponentsInput{
		systemNamespace:           input.namespace,
		operatorConfig:            input.config,
		ImagePullSecrets:          input.pullSecretNames,
		ProvisioningControllerPVC: input.pvc,
		client:                    input.client,
	})
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
var _ = Describe("DPF System tests", Labels{dpfSystemLabel}, func() {

	BeforeEach(func() {
		if !input.hasDpuNodes() {
			return
		}
		for _, label := range CurrentSpecReport().Labels() {
			if label != requiresNodesLabel {
				continue
			}

			By("Waiting for provisioning")
			VerifyDPUClusterWithNodes(ctx, getProvisionDPUClustersInput())
			By("Waiting for DPU cluster pods to be ready")
			VerifyDPUClusterPods(ctx, systemPodsToVerify)
		}
	})

	AfterEach(func() {
		By("cleaning up objects created during the test", func() {
			Expect(utils.CleanupWithLabelAndWait(ctx, testClient, labels.SelectorFromSet(afterEachCleanupLabels), resourcesToDelete...)).To(Succeed())
		})
	})

	Context("DPU Deployment", func() {
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

	Context("KSM Metrics Collection", func() {
		It("validate DPF metrics services are accessible", func() {
			VerifyKSMMetricsCollection(ctx)
		})
	})

	Context("DPU Service IPAM", func() {
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

	Context("DPU Service Chain", func() {
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

	Context("DPU Service Credential Request", func() {
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
		It("verify DPUService and DPUServiceInterface metrics", func() {
			ValidateDPUServiceMetrics(ctx, input)
		})
		It("delete the DPUServices and check that the applications are cleaned up", func() {
			ValidateDPUServiceDeletion(ctx, input)
		})
		It("verify that the ImagePullSecrets have been synced correctly and cleaned up", func() {
			ValidateImagePullSecretsSync(ctx, input)
		})
	})

	Context("DPU Service Template", func() {
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

	Context("Validate General DPF Metrics", func() {
		It("should validate general DPF Metrics ", func() {
			ValidateGeneralDPFMetrics(ctx, input)
		})
	})

	Context("DPU Service Config Ports", Labels{requiresNodesLabel}, Serial, func() {
		It("expose ConfigPorts via DPUService and test reachability", func() {
			ValidateDPUServiceConfigPorts(ctx, input)
		})
	})

	Context("Validate DPU Operator Config", Serial, Ordered, func() {
		It("verify ImageConfiguration from DPUServices", func() {
			ValidateDPFOperatorImageConfiguration(ctx, input)
		})
		It("verify that the current MTU in the DPU clusters flannel configmap is 1500", func() {
			ValidateDPFOperatorMTUCurrentConfiguration(ctx, input)
		})
		It("change the MTUs in the operatorConfig and verify that DPU Clusters are updated", func() {
			ValidateDPFOperatorMTUConfigurationChange(ctx, input)
		})
		It("verify overrides path setting for system DPUServices", func() {
			ValidateDPFOperatorPathConfiguration(ctx, input)
		})
		It("Change the MaxDPUParallelInstallations in the operatorConfig and verify that the provisioning controller is restarted", func() {
			ValidateDPFOperatorMaxDPUParallelInstallations(ctx, input)
		})
		It("Change the flannel podCIDR in the operatorConfig and check that it is set", func() {
			ValidateDPFOperatorFlannelPodCIDRChange(ctx, input)
		})

		// This test triggers reprovisioning, which might disrupt other tests relying on provisioned nodes.
		// Added BeforeEach wait for the nodes to be provisioned for the test with requiresNodesLabel
		It("verify Kubernetes API related variables are propagated correctly", func() {
			ValidateDPFOperatorKubernetesAPIServerVIPAndPort(ctx, input)
		})
	})

	Context("Validate DPF Operator Cleanup", Serial, func() {
		It("should validate DPU Operator and underlying objects creation", func() {
			ValidateOperatorFullCreation(ctx, input)
		})
	})
})

func getProvisionDPUClustersInput() ProvisionDPUClustersInput {
	return ProvisionDPUClustersInput{
		numberOfNodesPerCluster: input.numberOfDPUNodes,
		dpuClusterPrerequisites: input.additionalProvisioningObjects,
		dpuCluster:              input.dpuCluster,
		dpuSet:                  input.dpuSet,
		bfb:                     input.bfb,
		dpuFlavor:               input.dpuFlavor,
		// This server override enables running the e2e tests using Docker Desktop on MacOS. The port must match the port contained
		// in the nodeport defined in the nodePortService in the dpuClusterPrerequisites.
		dpuClusterClientOptions: []dpucluster.ClientOption{
			dpucluster.ClientOptionConfigHost{Host: conf.DPUClusterKubernetesAPIOverride},
			dpucluster.ClientOptionSkipVerifyTLS{SkipVerifyTLS: conf.DPUClusterSkipVerifyTLS},
		},
		client:      input.client,
		bfbImageURL: input.bfbImageURL,
	}
}
