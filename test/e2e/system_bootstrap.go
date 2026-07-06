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

func validateFlags() {
	if !IsGinkgoLabelApplied(Domain.ZeroTrust) {
		return
	}

	if Conf.NodeRebootConfigMap == "" {
		panic("ZeroTrust requires `nodeRebootConfigMap` to be set in the e2e config file")
	}
	if Conf.NodeRebootConfigMapPath == "" {
		panic("ZeroTrust requires `nodeRebootConfigMapPath` to be set in the e2e config file")
	}
	if bmcPassword == "" {
		panic("ZeroTrust requires E2E_ZT_BMC_PASSWORD env var (BMC root password used by the in-cluster reboot script)")
	}
	if bmcInventoryPath == "" {
		panic("ZeroTrust requires E2E_ZT_BMC_INVENTORY_PATH env var (path to the lab DPU-serial -> BMC IP inventory YAML)")
	}

	if IsGinkgoLabelApplied(Domain.ExternalTest) {
		if len(externalTest) == 0 {
			panic("This script must be provided when External label is present")
		}
	}
}

func SetInput() *SystemTestInput {
	By("Validating the input")
	validateFlags()

	By("Get control plane IP")
	controlPlaneIP := getClusterControlPlaneIP(Ctx, TestClient)

	By("Setting operatorConfig for the test")
	var bfbPVCName *string
	if Conf.ProvisioningControllerPVCPath != nil {
		bfbPVCName = ptr.To("bfb-pvc")
	}
	dpfOperatorConfig := &operatorv1.DPFOperatorConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ConfigName,
			Namespace: DPFOperatorSystemNamespace,
			Labels:    CleanupScope.Suite,
		},
		Spec: operatorv1.DPFOperatorConfigSpec{
			DeploymentMode: operatorv1.DeploymentModeHostTrusted,
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
			KataContainers: &operatorv1.KataContainersConfiguration{
				BaseComponentConfig: operatorv1.BaseComponentConfig{
					Disable: ptr.To(false),
				},
			},
			ImagePullSecrets: []string{DPFPullSecretName, "pull-secret-extra"},
		},
	}
	if IsGinkgoLabelApplied(Domain.ZeroTrust) {
		dpfOperatorConfig.Spec.DeploymentMode = operatorv1.DeploymentModeZeroTrust
		dpfOperatorConfig.Spec.StaticClusterManager.BaseComponentConfig.Disable = ptr.To(true)
		dpfOperatorConfig.Spec.KamajiClusterManager.BaseComponentConfig.Disable = ptr.To(false)
		dpfOperatorConfig.Spec.NodeSRIOVDevicePluginController.BaseComponentConfig.Disable = ptr.To(true)
		dpfOperatorConfig.Spec.ProvisioningController.InstallInterface = &operatorv1.ProvisioningInstallInterface{
			InstallViaRedfish: &operatorv1.InstallViaRedfish{
				SkipDPUNodeDiscovery: ptr.To(false),
			},
		}
		dpfOperatorConfig.Spec.DPUDetector = &operatorv1.DPUDetectorConfiguration{
			BaseComponentConfig: operatorv1.BaseComponentConfig{
				Disable: ptr.To(true),
			},
		}
		apiServerPort := 443
		if u, err := url.Parse(RestConfig.Host); err == nil {
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

	if IsGinkgoLabelApplied(Domain.Scale) {
		// For scale environments, the nodes are fake, therefore we can't have DPUDetector running
		dpfOperatorConfig.Spec.DPUDetector = &operatorv1.DPUDetectorConfiguration{
			BaseComponentConfig: operatorv1.BaseComponentConfig{
				Disable: ptr.To(true),
			},
		}
	}

	// CI runs the host control-plane controllers at a single replica to save
	// resources on control-plane nodes and keep logs easy to read.
	if dpfOperatorConfig.Spec.DPUServiceController == nil {
		dpfOperatorConfig.Spec.DPUServiceController = &operatorv1.DPUServiceControllerConfiguration{}
	}
	dpfOperatorConfig.Spec.ProvisioningController.Replicas = ptr.To[int32](1)
	dpfOperatorConfig.Spec.DPUServiceController.Replicas = ptr.To[int32](1)
	dpfOperatorConfig.Spec.KamajiClusterManager.Replicas = ptr.To[int32](1)
	dpfOperatorConfig.Spec.StaticClusterManager.Replicas = ptr.To[int32](1)
	dpfOperatorConfig.Spec.NodeSRIOVDevicePluginController.Replicas = ptr.To[int32](1)

	if prereqsNamespace != "" {
		if dpfOperatorConfig.Spec.Overrides == nil {
			dpfOperatorConfig.Spec.Overrides = &operatorv1.Overrides{}
		}

		dpfOperatorConfig.Spec.Overrides.ArgoCDNamespace = ptr.To(prereqsNamespace)
	}

	if IsGinkgoLabelApplied(Domain.Performance) {
		apiServerHost := controlPlaneIP
		apiServerPort := DefaultAPIServerPort
		if targetClusterAPIServerHost != "" {
			apiServerHost = targetClusterAPIServerHost
		} else if u, err := url.Parse(RestConfig.Host); err == nil {
			if h := u.Hostname(); h != "" {
				apiServerHost = h
			}
			if p := u.Port(); p != "" {
				if parsed, err := strconv.Atoi(p); err == nil {
					apiServerPort = parsed
				}
			}
		}
		if dpfOperatorConfig.Spec.Overrides == nil {
			dpfOperatorConfig.Spec.Overrides = &operatorv1.Overrides{}
		}
		dpfOperatorConfig.Spec.Overrides.KubernetesAPIServerVIP = ptr.To(apiServerHost)
		dpfOperatorConfig.Spec.Overrides.KubernetesAPIServerPort = ptr.To(apiServerPort)
		dpfOperatorConfig.Spec.ProvisioningController.DMSTimeout = ptr.To(15 * 60)
		dpfOperatorConfig.Spec.Networking = &operatorv1.Networking{
			ControlPlaneMTU: ptr.To(PerformanceMTU),
			HighSpeedMTU:    ptr.To(PerformanceMTU),
		}
	}

	input = &SystemTestInput{
		Namespace:          DPFOperatorSystemNamespace,
		Config:             dpfOperatorConfig,
		PullSecretNames:    dpfOperatorConfig.Spec.ImagePullSecrets,
		Client:             TestClient,
		RestConfig:         RestConfig,
		CleanupFlags:       CleanupFlags,
		BFBImageURL:        bfbImageURL,
		BFSOsIsoURL:        bfsOsIsoURL,
		BFSPldmFwBundleURL: bfsPldmFwBundleURL,
	}
	input.ApplyConfig(*Conf)
	return input
}

// SystemSetupBeforeSuite sets up the system components for the e2e tests.
// If skipSystemComponentValidation is true, it skips the validation of system components after deployment.
func SystemSetupBeforeSuite(skipSystemComponentValidation bool) {
	if Label(Domain.Scale).MatchesLabelFilter(GinkgoLabelFilter()) {
		CreateDPUWorkerNodes(Ctx, input.NumberOfDPUNodes)
	}

	AnnotateAndLabelNodes(Ctx, input.Client, input.UseExternalNodeReboot)

	if ngcAPIKey != "" {
		createNGCImagePullSecret(Ctx, input.Client)
	}

	By("Deploy DPF System components")
	DeployDPFSystemComponents(Ctx, DeployDPFSystemComponentsInput{
		SystemNamespace:               input.Namespace,
		OperatorConfig:                input.Config,
		ImagePullSecrets:              input.PullSecretNames,
		ProvisioningControllerPVC:     input.PVC,
		DPUDiscovery:                  input.DPUDiscovery,
		Client:                        input.Client,
		NumberOfDPUNodes:              input.NumberOfDPUNodes,
		SkipSystemComponentValidation: skipSystemComponentValidation,
	})

	if IsGinkgoLabelApplied(Domain.ZeroTrust) {
		// In ZeroTrust mode, build a DPUNode-to-host BMC IP map from the lab inventory file
		// for the script-based reboot path (nodeRebootMethod.script).
		input.DPUNodeBMCs = GetDPUNodeToBMCIPs(
			Ctx, input.Client, input.NumberOfDPUNodes)

		// Ensure ConfigMap and DPUNode BMC IP labels are set ahead of any DPU reaching the reboot state,
		// so the controller can drive in-cluster Redfish reboots through the named ConfigMap.
		ApplyNodeRebootConfigMap(Ctx, input.Client, input.NodeRebootConfigMapPath)
		PatchDPUNodesForScriptReboot(Ctx, input.Client, input.NumberOfDPUNodes,
			input.NodeRebootConfigMap, input.DPUNodeBMCs)
	}

	if IsGinkgoLabelApplied(Domain.Performance) {
		vip := *input.Config.Spec.Overrides.KubernetesAPIServerVIP
		port := *input.Config.Spec.Overrides.KubernetesAPIServerPort
		PatchNFDWorkerForVIP(Ctx, input.Client, input.Namespace, vip, port)
	}
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
			Name:      NGCPullSecretName,
			Namespace: DPFOperatorSystemNamespace,
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

// AnnotateAndLabelNodes stamps host-cluster Nodes with reboot-related labels
// consumed by the host agent. When useExternalNodeReboot is true (NIC cloud
// e2e tests), the labels make the host agent delegate host reboots to lab
// infrastructure (e.g. NIC cloud's `nic-cloud-reset`). Independent from
// ZeroTrust's in-cluster script reboot path (`nodeRebootMethod.script` set
// per-DPUNode by the e2e suite).
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

func GetProvisionDPUClustersInput() ProvisionDPUClustersInput {
	return ProvisionDPUClustersInput{
		NumberOfDPUNodes:        input.NumberOfDPUNodes,
		NumberOfDPUsPerNode:     input.NumberOfDPUsPerNode,
		DPUClusterPrerequisites: input.DPUClusterPrerequisites,
		DPUClusters:             input.DPUClusters,
		DPUSet:                  input.DPUSet,
		BFB:                     input.BFB,
		BlueFieldSoftware:       input.BlueFieldSoftware,
		DPUFlavor:               input.DPUFlavor,
		Client:                  input.Client,
		BFBImageURL:             input.BFBImageURL,
		BFSOsIsoURL:             input.BFSOsIsoURL,
		BFSPldmFwBundleURL:      input.BFSPldmFwBundleURL,
		RestConfig:              RestConfig,
		NodeRebootConfigMap:     input.NodeRebootConfigMap,
		DPUNodeBMCs:             input.DPUNodeBMCs,
	}
}
