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
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	testutils "github.com/nvidia/doca-platform/test/utils"

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

	By("Setting operatorConfig for the test")
	dpfOperatorConfig := &operatorv1.DPFOperatorConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configName,
			Namespace: dpfOperatorSystemNamespace,
			Labels:    testutils.AfterAllCleanupLabels,
		},
		Spec: operatorv1.DPFOperatorConfigSpec{
			ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
				BFBPersistentVolumeClaimName: "bfb-pvc",
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
			ImagePullSecrets: []string{dpfPullSecretName, "pull-secret-extra"},
		},
	}
	if isGinkgoLabelApplied(zeroTrustLabel) {
		dpfOperatorConfig.Spec.StaticClusterManager.BaseComponentConfig.Disable = ptr.To(true)
		dpfOperatorConfig.Spec.KamajiClusterManager.BaseComponentConfig.Disable = ptr.To(false)
		By("Get control-plane IP")
		trustedHostIP := getClusterControlPlaneIP(ctx, testClient)
		By(fmt.Sprintf("Zero trust mode is applied to operatorConfig with trusted host IP %s", trustedHostIP))
		dpfOperatorConfig.Spec.ProvisioningController.InstallInterface = &operatorv1.ProvisioningInstallInterface{
			InstallViaRedfish: &operatorv1.InstallViaRedfish{
				// Use NodePort service port 30080
				BFBRegistryAddress:   fmt.Sprintf("%s:30080", trustedHostIP),
				SkipDPUNodeDiscovery: ptr.To(false),
			},
		}
		dpfOperatorConfig.Spec.DPUDetector = &operatorv1.DPUDetectorConfiguration{
			BaseComponentConfig: operatorv1.BaseComponentConfig{
				Disable: ptr.To(true),
			},
		}
	}

	input = &systemTestInput{
		namespace:        dpfOperatorSystemNamespace,
		config:           dpfOperatorConfig,
		pullSecretNames:  dpfOperatorConfig.Spec.ImagePullSecrets,
		client:           testClient,
		restConfig:       restConfig,
		skipCleanup:      skipCleanup,
		bfbImageURL:      bfbImageURL,
		HostRebootScript: HostRebootScript,
	}
	input.applyConfig(*conf)
}

func SystemSetupBeforeSuite() {
	if Label(scaleLabel).MatchesLabelFilter(GinkgoLabelFilter()) {
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

	labels := maps.Clone(testutils.AfterAllCleanupLabels)
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
var _ = Describe("DPF System tests - Core", SpecPriority(CoreTestPriority), Labels{dpfSystemLabel}, func() {

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
			VerifyClusterPods(ctx, dpuClusterClient[0], systemPodsToVerify)

			By("Waiting for DPFOperatorConfig to be ready")
			VerifyDPFOperatorConfigReady(ctx, input.client, 20*time.Minute)
		}
	})

	Context("DPU Deployment", Labels{zeroTrustLabel}, func() {
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

	Context("KSM Metrics Collection", Labels{zeroTrustLabel}, func() {
		It("validate DPF metrics services are accessible", func() {
			VerifyKSMMetricsCollection(ctx)
		})
	})

	Context("DPU Service IPAM", Labels{zeroTrustLabel}, func() {
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

	Context("DPU Service Chain", Labels{zeroTrustLabel}, func() {
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

	Context("DPU Service Credential Request", Labels{zeroTrustLabel}, func() {
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
		It("verify DPUService and DPUServiceInterface metrics", Labels{zeroTrustLabel}, func() {
			ValidateDPUServiceMetrics(ctx, input)
		})
		It("delete the DPUServices and check that the applications are cleaned up", Labels{zeroTrustLabel}, func() {
			ValidateDPUServiceDeletion(ctx, input)
		})
		// Skipped for ZeroTrust due to the bug #4835281
		It("verify that the ImagePullSecrets have been synced correctly and cleaned up", func() {
			ValidateImagePullSecretsSync(ctx, input)
		})
	})

	Context("DPU Service Template", Labels{zeroTrustLabel}, func() {
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

	Context("Validate General DPF Metrics", Labels{zeroTrustLabel}, func() {
		It("should validate general DPF Metrics ", func() {
			ValidateGeneralDPFMetrics(ctx, input)
		})
	})
	// Config Ports check is not valid for ZeroTrust
	Context("DPU Service Config Ports", Labels{requiresNodesLabel}, Serial, func() {
		It("expose ConfigPorts via DPUService and test reachability", func() {
			ValidateDPUServiceConfigPorts(ctx, input)
		})
	})

	Context("Validate DPU Operator Config", Serial, Ordered, func() {
		It("verify system component overrides", Labels{zeroTrustLabel}, func() {
			ValidateDPFOperatorBaseConfiguration(ctx, input)
		})
		It("verify that the current MTU in the DPU clusters flannel configmap is 1500", Labels{zeroTrustLabel}, func() {
			ValidateDPFOperatorMTUCurrentConfiguration(ctx, input)
		})
		It("change the MTUs in the operatorConfig and verify that DPU Clusters are updated", Labels{zeroTrustLabel}, func() {
			ValidateDPFOperatorMTUConfigurationChange(ctx, input)
		})
		It("verify overrides path setting for system DPUServices", Labels{zeroTrustLabel}, func() {
			ValidateDPFOperatorPathConfiguration(ctx, input)
		})
		// This test triggers reprovisioning, which might disrupt other tests relying on provisioned nodes. Skip for ZeroTrust due to bug #4835281
		It("Change the MaxDPUParallelInstallations in the operatorConfig and verify that the provisioning controller is restarted", func() {
			ValidateDPFOperatorMaxDPUParallelInstallations(ctx, input)
		})
		It("Change the flannel podCIDR in the operatorConfig and check that it is set", Labels{zeroTrustLabel}, func() {
			ValidateDPFOperatorFlannelPodCIDRChange(ctx, input)
		})

		// This test triggers reprovisioning, which might disrupt other tests relying on provisioned nodes.
		// Added BeforeEach wait for the nodes to be provisioned for the test with requiresNodesLabel
		// DMS check is not valid for ZeroTrust
		It("verify Kubernetes API related variables are propagated correctly", Labels{requiresNodesLabel}, func() {
			ValidateDPFOperatorKubernetesAPIServerVIPAndPort(ctx, input)
		})
	})

	// These tests delete the existing DPUSet created in the beginning of the testing suite, and create a DPUDeployment
	// instead. The DPUDeployment should not be removed until all the tests in the e2e suite are run as the DPUs will be
	// deleted.
	Context("Validate DPUDeployment full creation", Serial, Ordered, func() {
		BeforeAll(func() {
			By("should validate DPUDeployment and underlying objects creation")
			ValidateDPUDeploymentFullCreation(ctx, input)
		})
		// TODO: remove requiresNodesLabel when the bug #4835281 is fixed
		It("should validate DPUDeployment becomes ready", Labels{requiresNodesLabel, zeroTrustLabel}, func() {
			VerifyDPUDeploymentIsReady(ctx, input)
		})
		// Disruptive upgrade checks are not valid for ZeroTrust due to the bug #4835281
		It("should validate DPUDeployment disruptive upgrade of standard DPUServices", func() {
			ValidateDPUDeploymentDPUServiceDisruptiveUpgrade(ctx, input)
		})
		It("should validate DPUDeployment disruptive upgrade of in-cluster DPUServices", func() {
			ValidateDPUDeploymentInClusterDPUServiceDisruptiveUpgrade(ctx, input)
		})
		It("should validate DPUDeployment disruptive upgrade of DPUServiceChain", func() {
			ValidateDPUDeploymentDPUServiceChainDisruptiveUpgrade(ctx, input)
		})
	})

	// The actual DPFOperatorConfig removal happens in AfterSuite but we need to ensure some resources exist before we
	// proceed with the removal.
	Context("Validate DPFOperatorConfig deletion", Serial, Labels{zeroTrustLabel}, func() {
		It("should validate the expected objects exist before leaving the Container node", func() {
			ValidateDPFOperatorConfigCleanupPrerequisites(ctx, input)
		})
	})
})

func getProvisionDPUClustersInput() ProvisionDPUClustersInput {
	return ProvisionDPUClustersInput{
		numberOfNodesPerCluster: input.numberOfDPUNodes,
		dpuClusterPrerequisites: input.additionalProvisioningObjects,
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
