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
package e2e

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/digest"
	"github.com/nvidia/doca-platform/internal/operator/inventory"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"
	testutils "github.com/nvidia/doca-platform/test/utils"
	"github.com/nvidia/doca-platform/test/utils/netshoot"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	testKubernetesAPIServerVIP  = "192.168.1.1"
	testKubernetesAPIServerPort = 1111
)

// ValidateDPFOperatorBaseConfiguration verifies that DPFOperatorConfiguration ContainerComponentConfiguration options work.
// It changes the images for all system components to arbitrary values, checks that the changes have propagated and then
// changes them back to their default versions.
func ValidateDPFOperatorBaseConfiguration(ctx context.Context, input *systemTestInput) {
	modifiedConfig := &operatorv1.DPFOperatorConfig{}
	Expect(input.client.Get(ctx, client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: configName}, modifiedConfig)).To(Succeed())
	originalConfig := modifiedConfig.DeepCopy()

	dummyRegistryName := "dummy-registry.com"
	imageTemplate := "%s/%s:v1.0"
	dummyResourceRequirements := operatorv1.ResourceComponentConfig{
		Resources: &operatorv1.ResourceRequirements{
			Requests: &operatorv1.Resources{
				CPU:    ptr.To(resource.MustParse("100m")),
				Memory: ptr.To(resource.MustParse("100Mi")),
			},
			Limits: &operatorv1.Resources{
				CPU:    ptr.To(resource.MustParse("200m")),
				Memory: ptr.To(resource.MustParse("200Mi")),
			},
		},
	}
	expectedDummyResources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("100Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("200m"),
			corev1.ResourceMemory: resource.MustParse("200Mi"),
		},
	}

	// For objects which are deployed as DPUServices set the helm chart field in configuration.
	// Excluding flannel which DPF Operator does not allow setting an image for.
	modifiedConfig.Spec.ServiceSetController = &operatorv1.ServiceSetControllerConfiguration{
		Controller: &operatorv1.DefaultOverridesConfiguration{
			ImageComponentConfig: operatorv1.ImageComponentConfig{
				Image: ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.ServiceSetControllerName)),
			},
			ResourceComponentConfig: dummyResourceRequirements,
		},
	}
	modifiedConfig.Spec.Multus = &operatorv1.MultusConfiguration{
		CNI: &operatorv1.DefaultOverridesConfiguration{
			ImageComponentConfig: operatorv1.ImageComponentConfig{
				Image: ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.MultusName)),
			},
			ResourceComponentConfig: dummyResourceRequirements,
		},
	}
	modifiedConfig.Spec.SRIOVDevicePlugin = &operatorv1.SRIOVDevicePluginConfiguration{
		DevicePlugin: &operatorv1.DefaultOverridesConfiguration{
			ImageComponentConfig: operatorv1.ImageComponentConfig{
				Image: ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.SRIOVDevicePluginName)),
			},
			ResourceComponentConfig: dummyResourceRequirements,
		},
	}
	modifiedConfig.Spec.SFCController = &operatorv1.SFCControllerConfiguration{
		Controller: &operatorv1.DefaultOverridesConfiguration{
			ImageComponentConfig: operatorv1.ImageComponentConfig{
				Image: ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.SFCControllerName)),
			},
			ResourceComponentConfig: dummyResourceRequirements,
		},
	}
	modifiedConfig.Spec.NVIPAM = &operatorv1.NVIPAMConfiguration{
		Controller: &operatorv1.NVIPAMController{
			ImageComponentConfig: operatorv1.ImageComponentConfig{
				Image: ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.NVIPAMControllerName)),
			},
			ResourceComponentConfig: dummyResourceRequirements,
		},
		Node: &operatorv1.NVIPAMNode{
			ImageComponentConfig: operatorv1.ImageComponentConfig{
				Image: ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.NVIPAMNodeName)),
			},
			ResourceComponentConfig: dummyResourceRequirements,
		},
	}
	modifiedConfig.Spec.Flannel = &operatorv1.FlannelConfiguration{
		CNI: &operatorv1.FlannelCNI{
			ImageComponentConfig: operatorv1.ImageComponentConfig{
				Image: ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.FlannelName)),
			},
		},
		Daemon: &operatorv1.FlannelDaemon{
			ImageComponentConfig: operatorv1.ImageComponentConfig{
				Image: ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.FlannelName)),
			},
			ResourceComponentConfig: dummyResourceRequirements,
		},
	}
	// Controller overrides.
	modifiedConfig.Spec.ProvisioningController = originalConfig.Spec.ProvisioningController.DeepCopy()
	modifiedConfig.Spec.ProvisioningController.Controller = &operatorv1.DefaultOverridesConfiguration{
		ImageComponentConfig: operatorv1.ImageComponentConfig{
			Image: ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.ProvisioningControllerName)),
		},
		ResourceComponentConfig: dummyResourceRequirements,
	}
	modifiedConfig.Spec.DPUServiceController = &operatorv1.DPUServiceControllerConfiguration{
		Controller: &operatorv1.DefaultOverridesConfiguration{
			ImageComponentConfig: operatorv1.ImageComponentConfig{
				Image: ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.DPUServiceControllerName)),
			},
			ResourceComponentConfig: dummyResourceRequirements,
		},
	}
	if !isGinkgoLabelApplied(Domain.ZeroTrust) {
		modifiedConfig.Spec.NodeSRIOVDevicePluginController = &operatorv1.NodeSRIOVDevicePluginControllerConfiguration{
			Controller: &operatorv1.DefaultOverridesConfiguration{
				ImageComponentConfig: operatorv1.ImageComponentConfig{
					Image: ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.NodeSRIOVDevicePluginControllerName)),
				},
				ResourceComponentConfig: dummyResourceRequirements,
			},
		}
	}
	if modifiedConfig.Spec.Security == nil {
		modifiedConfig.Spec.Security = &operatorv1.SecurityConfiguration{}
	}
	modifiedConfig.Spec.Security.Kata = &operatorv1.KataContainersConfiguration{
		Daemon: &operatorv1.ImageComponentConfig{
			Image: ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.KataContainersName)),
		},
	}

	By("Updating the DPFOperatorConfig with modified images and resources")
	Expect(input.client.Patch(ctx, modifiedConfig, client.MergeFrom(originalConfig))).To(Succeed())

	By("Verifying all components are updated")
	verifyComponentOverrides(ctx, input, dummyRegistryName, expectedDummyResources)

	dummyRegistryName = "overridden-registry.com"
	configCopy := modifiedConfig.DeepCopy()
	modifiedConfig.Spec.ServiceSetController.Controller.Image = nil
	modifiedConfig.Spec.ServiceSetController.Image = ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.ServiceSetControllerName))
	modifiedConfig.Spec.Multus.CNI.Image = nil
	modifiedConfig.Spec.Multus.Image = ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.MultusName))
	modifiedConfig.Spec.SRIOVDevicePlugin.DevicePlugin.Image = nil
	modifiedConfig.Spec.SRIOVDevicePlugin.Image = ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.SRIOVDevicePluginName))
	modifiedConfig.Spec.SFCController.Controller.Image = nil
	modifiedConfig.Spec.SFCController.Image = ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.SFCControllerName))
	modifiedConfig.Spec.NVIPAM.Controller.Image = nil
	modifiedConfig.Spec.NVIPAM.Node.Image = nil
	modifiedConfig.Spec.NVIPAM.Image = ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.NVIPAMControllerName))
	modifiedConfig.Spec.ProvisioningController.Controller.Image = nil
	modifiedConfig.Spec.ProvisioningController.Image = ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.ProvisioningControllerName))
	modifiedConfig.Spec.DPUServiceController.Controller.Image = nil
	modifiedConfig.Spec.DPUServiceController.Image = ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.DPUServiceControllerName))
	modifiedConfig.Spec.Flannel.CNI.Image = nil
	modifiedConfig.Spec.Flannel.Daemon.Image = nil
	modifiedConfig.Spec.Flannel.Images = &operatorv1.FlannelImages{
		KubeFlannel: dummyRegistryName + "/kube-flannel:legacy-test",
		FlannelCNI:  dummyRegistryName + "/flannel-cni:legacy-test",
	}
	if !isGinkgoLabelApplied(Domain.ZeroTrust) {
		modifiedConfig.Spec.NodeSRIOVDevicePluginController.Controller.Image = ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.NodeSRIOVDevicePluginControllerName))
	}
	modifiedConfig.Spec.Security.Kata.Daemon.Image = ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.KataContainersName))
	Expect(input.client.Patch(ctx, modifiedConfig, client.MergeFrom(configCopy))).To(Succeed())

	By("Verifying component overrides")
	verifyComponentOverrides(ctx, input, dummyRegistryName, expectedDummyResources)

	By("Reverting the DPFOperatorConfig to its original setting")
	Eventually(func(g Gomega) {
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(modifiedConfig), modifiedConfig)).To(Succeed())
		resetConfig := modifiedConfig.DeepCopy()
		resetConfig.Spec = originalConfig.Spec
		// Revert the image versions to their previous values.
		g.Expect(input.client.Patch(ctx, resetConfig, client.MergeFrom(modifiedConfig))).To(Succeed())
		// Ensure the changes are reverted before continuing.
	}).Should(Succeed())
}

func verifyComponentOverrides(ctx context.Context, input *systemTestInput, dummyRegistryName string, expectedDummyResources corev1.ResourceRequirements) {
	// Assert the images are set for the system components.
	tracker := NewByTracker()
	Eventually(func(g Gomega) {
		inClusterDeploymentDPUServices := map[operatorv1.ComponentName]bool{
			operatorv1.ServiceSetControllerName: true,
			operatorv1.NVIPAMControllerName:     true,
		}
		daemonSetDPUServices := map[operatorv1.ComponentName]bool{
			operatorv1.SRIOVDevicePluginName: true,
			operatorv1.SFCControllerName:     true,
			operatorv1.NVIPAMNodeName:        true,
			operatorv1.MultusName:            true,
			operatorv1.FlannelName:           true,
			operatorv1.KataContainersName:    true,
		}
		// Components that only support image overrides (no resource overrides).
		imageOnlyDPUServices := map[operatorv1.ComponentName]bool{
			operatorv1.KataContainersName: true,
		}
		controller := map[string]bool{
			inventory.DPFProvisioningControllerName: true,
			inventory.DPUServiceControllerName:      true,
		}

		if !isGinkgoLabelApplied(Domain.ZeroTrust) {
			controller[operatorv1.NodeSRIOVDevicePluginControllerName.String()] = true
		}

		deployValidation := func(g Gomega, c client.Client, clusterName, name string) {
			nameForCluster := fmt.Sprintf("%s-%s", clusterName, name)
			trackingAnnotationValuePrefix := nameForCluster
			if prereqsNamespace != "" {
				trackingAnnotationValuePrefix = fmt.Sprintf("%s_%s", dpfOperatorSystemNamespace, nameForCluster)
			}
			tracker.By(nameForCluster, "verifying overrides for %s", nameForCluster)
			deployments := appsv1.DeploymentList{}
			g.Expect(c.List(ctx, &deployments)).To(Succeed())

			var matchingDeployments []appsv1.Deployment
			for _, deploy := range deployments.Items {
				if strings.HasPrefix(deploy.GetAnnotations()[argoCDTrackingIDAnnotation], trackingAnnotationValuePrefix) {
					matchingDeployments = append(matchingDeployments, deploy)
				}
			}

			g.Expect(matchingDeployments).To(HaveLen(1))
			deployment := matchingDeployments[0]
			for _, container := range deployment.Spec.Template.Spec.Containers {
				g.Expect(container.Image).To(ContainSubstring(dummyRegistryName))
				g.Expect(container.Resources).To(BeEquivalentTo(expectedDummyResources))
			}
		}

		// Verify overrides for inCluster DPUServices
		for name := range inClusterDeploymentDPUServices {
			n := getPerClusterDPUServiceName(name, input.dpuClusters[0].Name, input.dpuClusters[0].Namespace)
			deployValidation(g, input.client, "in-cluster", n)
		}
		// Verify overrides in the DPUClusters
		for name := range daemonSetDPUServices {
			nameForCluster := fmt.Sprintf("%s-%s", input.dpuClusters[0].Name, name)
			trackingAnnotationValuePrefix := nameForCluster
			if prereqsNamespace != "" {
				trackingAnnotationValuePrefix = fmt.Sprintf("%s_%s", dpfOperatorSystemNamespace, nameForCluster)
			}
			tracker.By(nameForCluster, "verifying overrides for %s", nameForCluster)
			daemonSets := appsv1.DaemonSetList{}
			g.Expect(dpuClusterClient[0].List(ctx, &daemonSets)).To(Succeed())

			var matchingDaemonSets []appsv1.DaemonSet
			for _, ds := range daemonSets.Items {
				if strings.HasPrefix(ds.GetAnnotations()[argoCDTrackingIDAnnotation], trackingAnnotationValuePrefix) {
					matchingDaemonSets = append(matchingDaemonSets, ds)
				}
			}

			g.Expect(matchingDaemonSets).To(HaveLen(1))
			daemonSet := matchingDaemonSets[0]
			for _, container := range daemonSet.Spec.Template.Spec.Containers {
				g.Expect(container.Image).To(ContainSubstring(dummyRegistryName))
				if !imageOnlyDPUServices[name] {
					g.Expect(container.Resources).To(BeEquivalentTo(expectedDummyResources))
				}
			}
		}
		// Verify overrides in the controllers
		for name := range controller {
			tracker.By(name, "verifying overrides for %s", name)
			deployments := appsv1.DeploymentList{}
			g.Expect(input.client.List(ctx, &deployments,
				client.MatchingLabels{operatorv1.DPFComponentLabelKey: name})).To(Succeed())
			g.Expect(deployments.Items).To(HaveLen(1))
			deployment := deployments.Items[0]
			for _, container := range deployment.Spec.Template.Spec.Containers {
				// The PLDM unpack container uses the bfb registry image which is not configurable via the DPFOperatorConfig.
				if name == inventory.DPFProvisioningControllerName && container.Name == operatorv1.PLDMUnpackContainerName.String() {
					continue
				}
				g.Expect(container.Image).To(ContainSubstring(dummyRegistryName))
				g.Expect(container.Resources).To(BeEquivalentTo(expectedDummyResources))
			}
		}
	}, 120*time.Second).Should(Succeed())
}

func ValidateDPFOperatorMTUCurrentConfiguration(ctx context.Context, input *systemTestInput) {
	By("Verify flannel configmap for cluster " + input.dpuClusters[0].Name)
	flannelConfigMap := &corev1.ConfigMap{}
	Expect(dpuClusterClient[0].Get(ctx, client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: "kube-flannel-cfg"}, flannelConfigMap)).To(Succeed())
	Expect(flannelConfigMap.Data["net-conf.json"]).To(ContainSubstring("MTU\": 1500,"))
}

func ValidateDPFOperatorMTUConfigurationChange(ctx context.Context, input *systemTestInput) {
	By("Get the operatorConfig")
	modifiedConfig := &operatorv1.DPFOperatorConfig{}
	Expect(input.client.Get(ctx, client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: configName}, modifiedConfig)).To(Succeed())
	By("Update the MTU in the operatorConfig")
	originalConfig := modifiedConfig.DeepCopy()
	if modifiedConfig.Spec.Networking == nil {
		modifiedConfig.Spec.Networking = &operatorv1.Networking{}
	}
	modifiedConfig.Spec.Networking.ControlPlaneMTU = ptr.To(testMTUValue)
	modifiedConfig.Spec.Networking.HighSpeedMTU = ptr.To(9000)
	Eventually(func(g Gomega) {
		g.Expect(input.client.Patch(ctx, modifiedConfig, client.MergeFrom(originalConfig))).To(Succeed())
	}).Should(Succeed())

	By("Verify flannel and multus for cluster " + input.dpuClusters[0].Name)
	Eventually(func(g Gomega) {
		flannelConfigMap := &corev1.ConfigMap{}
		g.Expect(dpuClusterClient[0].Get(ctx, client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: "kube-flannel-cfg"}, flannelConfigMap)).To(Succeed())
		g.Expect(flannelConfigMap.Data["net-conf.json"]).To(ContainSubstring(fmt.Sprintf(`MTU": %d`, testMTUValue)))

		netAttachDef := &unstructured.Unstructured{}
		netAttachDef.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "k8s.cni.cncf.io",
			Version: "v1",
			Kind:    "NetworkAttachmentDefinition",
		})

		g.Expect(dpuClusterClient[0].Get(ctx, client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: "mybrsfc"}, netAttachDef)).To(Succeed())
		netAttachConfig, exists, err := unstructured.NestedString(netAttachDef.Object, "spec", "config")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(exists).To(BeTrue())
		g.Expect(netAttachConfig).To(ContainSubstring("mtu\": 9000,"))

		g.Expect(dpuClusterClient[0].Get(ctx, client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: "mybrhbn"}, netAttachDef)).To(Succeed())
		netAttachConfig, exists, err = unstructured.NestedString(netAttachDef.Object, "spec", "config")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(exists).To(BeTrue())
		g.Expect(netAttachConfig).To(ContainSubstring("mtu\": 9000,"))
	}, time.Second*30).Should(Succeed())

	if input.hasDpuNodes() && !isGinkgoLabelApplied(Domain.ZeroTrust) {
		By("Get configured OOB bridge name from DPFOperatorConfig")
		bridgeName := operatorv1.DefaultDPUNodeOOBBridgeName
		if modifiedConfig.Spec.Networking != nil && modifiedConfig.Spec.Networking.DPUNodeOOBBridgeName != nil {
			bridgeName = *modifiedConfig.Spec.Networking.DPUNodeOOBBridgeName
		}

		By(fmt.Sprintf("Verify host bridge %s MTU on DPU nodes reflects ControlPlaneMTU change", bridgeName))
		Eventually(func(g Gomega) {
			pods := corev1.PodList{}
			g.Expect(input.client.List(ctx, &pods,
				client.MatchingLabels{cutil.ProvisioningComponentLabelKey: "hostagent"})).To(Succeed())
			runningPods := make([]corev1.Pod, 0, len(pods.Items))
			for _, pod := range pods.Items {
				if pod.DeletionTimestamp != nil {
					continue
				}
				g.Expect(pod.Status.Phase).To(Equal(corev1.PodRunning), "hostagent pod %s/%s is not running", pod.Namespace, pod.Name)
				runningPods = append(runningPods, pod)
			}
			g.Expect(runningPods).ToNot(BeEmpty())
			for _, pod := range runningPods {
				stdout, err := netshoot.ExecInContainerOnce(hostClusterRESTClient, input.restConfig,
					pod.Namespace, pod.Name, "hostagent", []string{"cat", fmt.Sprintf("/sys/class/net/%s/mtu", bridgeName)})
				g.Expect(err).NotTo(HaveOccurred(), "exec on pod %s/%s container hostagent: %s", pod.Namespace, pod.Name, stdout)
				g.Expect(strings.TrimSpace(stdout)).To(Equal(fmt.Sprintf("%d", testMTUValue)))
			}
		}, 5*time.Minute).Should(Succeed())
	}

	By("Reverting the DPFOperatorConfig to its original setting")
	Eventually(func(g Gomega) {
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(modifiedConfig), modifiedConfig)).To(Succeed())
		resetConfig := modifiedConfig.DeepCopy()
		resetConfig.Spec = originalConfig.Spec
		// Revert the image versions to their previous values.
		g.Expect(input.client.Patch(ctx, resetConfig, client.MergeFrom(modifiedConfig))).To(Succeed())
		// Ensure the changes are reverted before continuing.
	}).Should(Succeed())
}

func ValidateDPFOperatorOOBBridgeNameChange(ctx context.Context, input *systemTestInput) {
	if !input.hasDpuNodes() {
		Skip("Skip OOB bridge name test as there are no DPU nodes")
	}

	By("Get the operatorConfig")
	modifiedConfig := &operatorv1.DPFOperatorConfig{}
	Expect(input.client.Get(ctx, client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: configName}, modifiedConfig)).To(Succeed())
	originalConfig := modifiedConfig.DeepCopy()

	By("Set a non-existent bridge name in the operatorConfig")
	fakeBridgeName := "br-notexists"
	if modifiedConfig.Spec.Networking == nil {
		modifiedConfig.Spec.Networking = &operatorv1.Networking{}
	}
	modifiedConfig.Spec.Networking.DPUNodeOOBBridgeName = ptr.To(fakeBridgeName)
	Eventually(func(g Gomega) {
		g.Expect(input.client.Patch(ctx, modifiedConfig, client.MergeFrom(originalConfig))).To(Succeed())
	}).Should(Succeed())

	By("Verify DPUNode OOBBridgeConfigured condition becomes False")
	Eventually(func(g Gomega) {
		dpuNodeList := &provisioningv1.DPUNodeList{}
		g.Expect(input.client.List(ctx, dpuNodeList, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
		g.Expect(dpuNodeList.Items).ToNot(BeEmpty())
		for _, dpuNode := range dpuNodeList.Items {
			for _, cond := range dpuNode.Status.Conditions {
				if cond.Type == string(provisioningv1.DPUNodeConditionBridgeConfigured) {
					g.Expect(cond.Status).To(Equal(metav1.ConditionFalse),
						"DPUNode %s OOBBridgeConfigured should be False for non-existent bridge %q", dpuNode.Name, fakeBridgeName)
					g.Expect(cond.Reason).To(Equal("BridgeNotFound"))
				}
			}
		}
	}, 3*time.Minute).Should(Succeed())

	By("Revert the bridge name to its original setting")
	Eventually(func(g Gomega) {
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(modifiedConfig), modifiedConfig)).To(Succeed())
		resetConfig := modifiedConfig.DeepCopy()
		resetConfig.Spec = originalConfig.Spec
		g.Expect(input.client.Patch(ctx, resetConfig, client.MergeFrom(modifiedConfig))).To(Succeed())
	}).Should(Succeed())

	By("Verify DPUNode OOBBridgeConfigured condition recovers to True")
	Eventually(func(g Gomega) {
		dpuNodeList := &provisioningv1.DPUNodeList{}
		g.Expect(input.client.List(ctx, dpuNodeList, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
		g.Expect(dpuNodeList.Items).ToNot(BeEmpty())
		for _, dpuNode := range dpuNodeList.Items {
			for _, cond := range dpuNode.Status.Conditions {
				if cond.Type == string(provisioningv1.DPUNodeConditionBridgeConfigured) {
					g.Expect(cond.Status).To(Equal(metav1.ConditionTrue),
						"DPUNode %s OOBBridgeConfigured should recover to True after reverting bridge name", dpuNode.Name)
				}
			}
		}
	}, 3*time.Minute).Should(Succeed())
}

func ValidateDPFOperatorOOBBridgePostProvisioning(ctx context.Context, input *systemTestInput) {
	if !input.hasDpuNodes() {
		Skip("Skip OOB bridge post-provisioning test as there are no DPU nodes")
	}

	By("Get configured OOB bridge name from DPFOperatorConfig")
	config := &operatorv1.DPFOperatorConfig{}
	Expect(input.client.Get(ctx, client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: configName}, config)).To(Succeed())
	bridgeName := config.Spec.Networking.GetDPUNodeOOBBridgeName()

	By("Get hostagent pods")
	pods := corev1.PodList{}
	Expect(input.client.List(ctx, &pods,
		client.MatchingLabels{cutil.ProvisioningComponentLabelKey: "hostagent"})).To(Succeed())
	runningPods := make([]corev1.Pod, 0, len(pods.Items))
	for _, pod := range pods.Items {
		if pod.DeletionTimestamp != nil {
			continue
		}
		if pod.Status.Phase == corev1.PodRunning {
			runningPods = append(runningPods, pod)
		}
	}
	Expect(runningPods).ToNot(BeEmpty(), "no running hostagent pods found")

	for _, pod := range runningPods {
		By(fmt.Sprintf("Verify VF is attached to bridge %s on pod %s", bridgeName, pod.Name))
		Eventually(func(g Gomega) {
			stdout, err := netshoot.ExecInContainerOnce(hostClusterRESTClient, input.restConfig,
				pod.Namespace, pod.Name, "hostagent",
				[]string{"sh", "-c", fmt.Sprintf("ls /sys/class/net/%s/brif/ 2>/dev/null", bridgeName)})
			g.Expect(err).NotTo(HaveOccurred(), "failed to list bridge members on pod %s: %s", pod.Name, stdout)
			g.Expect(strings.TrimSpace(stdout)).ToNot(BeEmpty(),
				"bridge %s on pod %s should have at least one member interface (VF)", bridgeName, pod.Name)
		}, 2*time.Minute).Should(Succeed())

		By(fmt.Sprintf("Verify netplan file %s exists and references bridge %s", hostutil.BridgeMTUNetplanFile, bridgeName))
		Eventually(func(g Gomega) {
			stdout, err := netshoot.ExecInContainerOnce(hostClusterRESTClient, input.restConfig,
				pod.Namespace, pod.Name, "hostagent",
				[]string{"cat", hostutil.BridgeMTUNetplanFile})
			g.Expect(err).NotTo(HaveOccurred(), "netplan file not found on pod %s: %s", pod.Name, stdout)
			g.Expect(stdout).To(ContainSubstring(bridgeName),
				"netplan file on pod %s should reference bridge %s", pod.Name, bridgeName)
		}, time.Minute).Should(Succeed())

		By(fmt.Sprintf("Verify legacy netplan file %s is removed", hostutil.LegacyBridgeMTUNetplanFile))
		stdout, err := netshoot.ExecInContainerOnce(hostClusterRESTClient, input.restConfig,
			pod.Namespace, pod.Name, "hostagent",
			[]string{"sh", "-c", fmt.Sprintf("test -f %s && echo EXISTS || echo GONE", hostutil.LegacyBridgeMTUNetplanFile)})
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(stdout)).To(Equal("GONE"),
			"legacy netplan file %s should be removed on pod %s", hostutil.LegacyBridgeMTUNetplanFile, pod.Name)
	}
}

func ValidateDPFOperatorFlannelPodCIDRChange(ctx context.Context, input *systemTestInput) {
	By("Get the operatorConfig")
	modifiedConfig := &operatorv1.DPFOperatorConfig{}
	Expect(input.client.Get(ctx, client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: configName}, modifiedConfig)).To(Succeed())
	By("Update the podCIDR in the operatorConfig")
	originalConfig := modifiedConfig.DeepCopy()
	if modifiedConfig.Spec.Flannel == nil {
		modifiedConfig.Spec.Flannel = &operatorv1.FlannelConfiguration{}
	}
	modifiedConfig.Spec.Flannel.PodCIDR = ptr.To("10.255.0.0/14")
	Eventually(func(g Gomega) {
		g.Expect(input.client.Patch(ctx, modifiedConfig, client.MergeFrom(originalConfig))).To(Succeed())
	}).Should(Succeed())

	By("Verify flannel configmap for cluster " + input.dpuClusters[0].Name)
	Eventually(func(g Gomega) {
		flannelConfigMap := &corev1.ConfigMap{}
		g.Expect(dpuClusterClient[0].Get(ctx, client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: "kube-flannel-cfg"}, flannelConfigMap)).To(Succeed())
		g.Expect(flannelConfigMap.Data["net-conf.json"]).To(ContainSubstring("10.255.0.0/14"))
	}, time.Second*30).Should(Succeed())

	By("Reverting the DPFOperatorConfig to its original setting")
	Eventually(func(g Gomega) {
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(modifiedConfig), modifiedConfig)).To(Succeed())
		resetConfig := modifiedConfig.DeepCopy()
		resetConfig.Spec = originalConfig.Spec
		// Revert the image versions to their previous values.
		g.Expect(input.client.Patch(ctx, resetConfig, client.MergeFrom(modifiedConfig))).To(Succeed())
		// Ensure the changes are reverted before continuing.
	}).Should(Succeed())
}

func ValidateDPFOperatorMaxDPUParallelInstallations(ctx context.Context, input *systemTestInput) {
	modifiedConfig := &operatorv1.DPFOperatorConfig{}
	Expect(input.client.Get(ctx, client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: configName}, modifiedConfig)).To(Succeed())
	originalConfig := modifiedConfig.DeepCopy()

	By("Getting the current provisioning controller pod UIDs")
	var originalPodUIDs []types.UID
	Eventually(func(g Gomega) {
		pods := corev1.PodList{}
		g.Expect(input.client.List(ctx, &pods,
			client.MatchingLabels{operatorv1.DPFComponentLabelKey: "dpf-provisioning-controller-manager"})).To(Succeed())
		g.Expect(pods.Items).ToNot(BeEmpty())
		originalPodUIDs = make([]types.UID, 0, len(pods.Items))
		for _, pod := range pods.Items {
			originalPodUIDs = append(originalPodUIDs, pod.UID)
		}
	}).WithTimeout(120 * time.Second).Should(Succeed())

	By("Modifying the DPFOperatorConfig to set MaxDPUParallelInstallations")
	modifiedConfig.Spec.ProvisioningController.MaxDPUParallelInstallations = ptr.To(int32(25))
	Expect(input.client.Patch(ctx, modifiedConfig, client.MergeFrom(originalConfig))).To(Succeed())

	By("Verifying that the provisioning controller pod is restarted")
	var restartedPodUIDs []types.UID
	Eventually(func(g Gomega) {
		pods := corev1.PodList{}
		g.Expect(input.client.List(ctx, &pods,
			client.MatchingLabels{operatorv1.DPFComponentLabelKey: "dpf-provisioning-controller-manager"})).To(Succeed())
		g.Expect(pods.Items).ToNot(BeEmpty())
		// Verify all pods have been restarted
		for _, pod := range pods.Items {
			isOriginalPod := false
			for _, originalUID := range originalPodUIDs {
				if pod.UID == originalUID {
					isOriginalPod = true
					break
				}
			}
			g.Expect(isOriginalPod).To(BeFalse(), "Pod %s should have been restarted", pod.Name)
		}
		// Capture the restarted pod UIDs for later verification
		restartedPodUIDs = make([]types.UID, 0, len(pods.Items))
		for _, pod := range pods.Items {
			restartedPodUIDs = append(restartedPodUIDs, pod.UID)
		}
	}).WithTimeout(120 * time.Second).Should(Succeed())

	By("Reverting the DPFOperatorConfig to its original setting")
	Eventually(func(g Gomega) {
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(modifiedConfig), modifiedConfig)).To(Succeed())
		resetConfig := modifiedConfig.DeepCopy()
		resetConfig.Spec = originalConfig.Spec
		g.Expect(input.client.Patch(ctx, resetConfig, client.MergeFrom(modifiedConfig))).To(Succeed())
	}).WithTimeout(10 * time.Second).Should(Succeed())

	By("Verifying that the provisioning controller pod is restarted again")
	Eventually(func(g Gomega) {
		pods := corev1.PodList{}
		g.Expect(input.client.List(ctx, &pods,
			client.MatchingLabels{operatorv1.DPFComponentLabelKey: "dpf-provisioning-controller-manager"})).To(Succeed())
		g.Expect(pods.Items).ToNot(BeEmpty())
		// Verify all pods have been restarted again
		for _, pod := range pods.Items {
			wasRestartedPod := false
			for _, restartedUID := range restartedPodUIDs {
				if pod.UID == restartedUID {
					wasRestartedPod = true
					break
				}
			}
			g.Expect(wasRestartedPod).To(BeFalse(), "Pod %s should have been restarted again", pod.Name)
		}
	}).WithTimeout(120 * time.Second).Should(Succeed())
}

func ValidateDPFOperatorPathConfiguration(ctx context.Context, input *systemTestInput) {
	modifiedConfig := &operatorv1.DPFOperatorConfig{}
	Expect(input.client.Get(ctx, client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: configName}, modifiedConfig)).To(Succeed())
	originalConfig := modifiedConfig.DeepCopy()

	modifiedOVSRunPath := "/ovsrun"
	modifiedOVSBinPath := "/ovsbin"
	modifiedCNIConfigPath := "/cniconf"
	modifiedCNIBinPath := "/cnibin"
	modifiedOVSharedLibPath := "/ovssharedlib"
	modifiedOVSharedLib64Path := "/ovssharedlib64"
	if modifiedConfig.Spec.Overrides == nil {
		modifiedConfig.Spec.Overrides = &operatorv1.Overrides{}
	}
	modifiedConfig.Spec.Overrides.DPUCNIBinPath = ptr.To(modifiedCNIBinPath)
	modifiedConfig.Spec.Overrides.DPUCNIConfigPath = ptr.To(modifiedCNIConfigPath)
	modifiedConfig.Spec.Overrides.DPUOpenvSwitchRunPath = ptr.To(modifiedOVSRunPath)
	modifiedConfig.Spec.Overrides.DPUOpenvSwitchBinPath = ptr.To(modifiedOVSBinPath)
	modifiedConfig.Spec.Overrides.DPUOpenvSwitchSystemSharedLibPath = ptr.To(modifiedOVSharedLibPath)
	modifiedConfig.Spec.Overrides.DPUOpenvSwitchSystemSharedLib64Path = ptr.To(modifiedOVSharedLib64Path)
	modifiedConfig.Spec.Overrides.FlannelSkipCNIConfigInstallation = ptr.To(false)
	Expect(input.client.Patch(ctx, modifiedConfig, client.MergeFrom(originalConfig))).To(Succeed())

	dpuServiceDaemonSetsWithPathChanges := map[operatorv1.ComponentName]bool{
		operatorv1.SFCControllerName: true,
		operatorv1.NVIPAMNodeName:    true,
		operatorv1.MultusName:        true,
		operatorv1.FlannelName:       true,
	}

	// Assert the images are set for the system components.
	Eventually(func(g Gomega) {
		for name := range dpuServiceDaemonSetsWithPathChanges {
			daemonSets := appsv1.DaemonSetList{}
			nameForCluster := fmt.Sprintf("%s-%s", input.dpuClusters[0].Name, name)
			trackingAnnotationValuePrefix := nameForCluster
			if prereqsNamespace != "" {
				trackingAnnotationValuePrefix = fmt.Sprintf("%s_%s", dpfOperatorSystemNamespace, nameForCluster)
			}
			g.Expect(dpuClusterClient[0].List(ctx, &daemonSets)).To(Succeed())

			var matchingDaemonSets []appsv1.DaemonSet
			for _, ds := range daemonSets.Items {
				if strings.HasPrefix(ds.GetAnnotations()[argoCDTrackingIDAnnotation], trackingAnnotationValuePrefix) {
					matchingDaemonSets = append(matchingDaemonSets, ds)
				}
			}

			g.Expect(matchingDaemonSets).To(HaveLen(1))
			volumes := matchingDaemonSets[0].Spec.Template.Spec.Volumes
			switch name {
			case operatorv1.SFCControllerName:
				g.Expect(volumeNameHasPath("ovs", volumes, filepath.Join(modifiedOVSRunPath))).To(BeTrue())
				g.Expect(volumeNameHasPath("ovs-ofctl", volumes, filepath.Join(modifiedOVSBinPath, "ovs-ofctl"))).To(BeTrue())
				g.Expect(volumeNameHasPath("ovs-vsctl", volumes, filepath.Join(modifiedOVSBinPath, "ovs-vsctl"))).To(BeTrue())
				g.Expect(volumeNameHasPath("lib", volumes, filepath.Join(modifiedOVSharedLibPath))).To(BeTrue())
				g.Expect(volumeNameHasPath("lib64", volumes, filepath.Join(modifiedOVSharedLib64Path))).To(BeTrue())
			case operatorv1.NVIPAMNodeName:
				g.Expect(volumeNameHasPath("cnibin", volumes, filepath.Join(modifiedCNIBinPath))).To(BeTrue())
				g.Expect(volumeNameHasPath("cniconf", volumes, filepath.Join(modifiedCNIConfigPath, "nv-ipam.d"))).To(BeTrue())
			case operatorv1.MultusName:
				g.Expect(volumeNameHasPath("cnibin", volumes, filepath.Join(modifiedCNIBinPath))).To(BeTrue())
				g.Expect(volumeNameHasPath("cni", volumes, filepath.Join(modifiedCNIConfigPath))).To(BeTrue())
			case operatorv1.FlannelName:
				g.Expect(volumeNameHasPath("cni-plugin", volumes, filepath.Join(modifiedCNIBinPath))).To(BeTrue())
				g.Expect(volumeNameHasPath("cni", volumes, filepath.Join(modifiedCNIConfigPath))).To(BeTrue())

				// Validate that skipCNIConfigInstallation is working correctly
				// Since we set modifiedFlannelSkipCNIConfig = false, the install-cni init container should be present
				g.Expect(matchingDaemonSets[0].Spec.Template.Spec.InitContainers).To(HaveLen(2), "Flannel should have 2 init containers when skipCNIConfigInstallation is false")

				// Find the install-cni init container
				var foundInstallCNI bool
				for _, initContainer := range matchingDaemonSets[0].Spec.Template.Spec.InitContainers {
					if initContainer.Name == "install-cni" {
						foundInstallCNI = true
						// Verify it has the expected command
						g.Expect(initContainer.Command).To(ContainElement("cp"))
						g.Expect(initContainer.Args).To(ContainElements("-f", "/etc/kube-flannel/cni-conf.json", "/etc/cni/net.d/10-flannel.conflist"))
						break
					}
				}
				g.Expect(foundInstallCNI).To(BeTrue(), "install-cni init container should be present when skipCNIConfigInstallation is false")
			}
		}
	}).WithTimeout(120 * time.Second).Should(Succeed())

	By("Reverting the DPFOperatorConfig to its original setting")
	Eventually(func(g Gomega) {
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(modifiedConfig), modifiedConfig)).To(Succeed())
		resetConfig := modifiedConfig.DeepCopy()
		resetConfig.Spec = originalConfig.Spec
		// Revert the image versions to their previous values.
		g.Expect(input.client.Patch(ctx, resetConfig, client.MergeFrom(modifiedConfig))).To(Succeed())
		// Ensure the changes are reverted before continuing.
	}).Should(Succeed())
}

func volumeNameHasPath(name string, volumes []corev1.Volume, path string) bool {
	for _, volume := range volumes {
		if volume.Name == name {
			if volume.HostPath.Path == path {
				return true
			}
		}
	}
	return false
}

// ValidateDPFOperatorKubernetesAPIServerVIPAndPort validates that the Kubernetes API Server related variables are
// propagated correctly to the DMS pods.
func ValidateDPFOperatorKubernetesAPIServerVIPAndPort(ctx context.Context, input *systemTestInput) {
	if !input.hasDpuNodes() {
		Skip("Test requires node to trigger provisioning on, skipping")
	}

	modifiedConfig := &operatorv1.DPFOperatorConfig{}
	Expect(input.client.Get(ctx, client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: configName}, modifiedConfig)).To(Succeed())
	originalConfig := modifiedConfig.DeepCopy()

	By("Modifying the DPFOperatorConfig to set the Kubernetes API Server related variables")
	if modifiedConfig.Spec.Overrides == nil {
		modifiedConfig.Spec.Overrides = &operatorv1.Overrides{}
	}
	modifiedConfig.Spec.Overrides.KubernetesAPIServerVIP = ptr.To(testKubernetesAPIServerVIP)
	modifiedConfig.Spec.Overrides.KubernetesAPIServerPort = ptr.To(testKubernetesAPIServerPort)
	Expect(input.client.Patch(ctx, modifiedConfig, client.MergeFrom(originalConfig))).To(Succeed())

	By("Validating that the provisioning controller pod has the correct argument")
	Eventually(func(g Gomega) {
		pods := corev1.PodList{}
		g.Expect(input.client.List(ctx, &pods,
			// TODO: Check if we can align the operatorv1.ProvisioningControllerName with that label in the manifests
			// all the way
			client.MatchingLabels{operatorv1.DPFComponentLabelKey: "dpf-provisioning-controller-manager"})).To(Succeed())
		g.Expect(pods.Items).ToNot(BeEmpty())
		for _, pod := range pods.Items {
			g.Expect(pod.Spec.Containers).To(HaveLen(2))
			g.Expect(pod.Spec.Containers[0].Args).To(ContainElement(fmt.Sprintf("--dms-pod-envs=KUBERNETES_SERVICE_HOST=%s,KUBERNETES_SERVICE_PORT=%d", testKubernetesAPIServerVIP, testKubernetesAPIServerPort)))
		}
	}).WithTimeout(120 * time.Second).Should(Succeed())

	By("Triggering DMS Pod recreation")
	triggerDMSRecreation(ctx, input.client)

	By("Validating that all the DMS containers have the environment variables set correctly")
	Eventually(func(g Gomega) {
		pods := corev1.PodList{}
		g.Expect(input.client.List(ctx, &pods,
			client.MatchingLabels{cutil.ProvisioningComponentLabelKey: "hostagent"})).To(Succeed())
		g.Expect(pods.Items).ToNot(BeEmpty())
		for _, pod := range pods.Items {
			for _, container := range pod.Spec.Containers {
				g.Expect(container.Env).To(ContainElements([]corev1.EnvVar{
					{
						Name:  "KUBERNETES_SERVICE_HOST",
						Value: testKubernetesAPIServerVIP,
					},
					{
						Name:  "KUBERNETES_SERVICE_PORT",
						Value: fmt.Sprintf("%d", testKubernetesAPIServerPort),
					},
				}))
			}
		}
	}).WithTimeout(120 * time.Second).Should(Succeed())

	By("Reverting the DPFOperatorConfig to its original setting")
	Eventually(func(g Gomega) {
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(modifiedConfig), modifiedConfig)).To(Succeed())
		resetConfig := modifiedConfig.DeepCopy()
		resetConfig.Spec = originalConfig.Spec
		g.Expect(input.client.Patch(ctx, resetConfig, client.MergeFrom(modifiedConfig))).To(Succeed())
	}).WithTimeout(10 * time.Second).Should(Succeed())

	By("Validating that the provisioning controller pod has the correct argument")
	Eventually(func(g Gomega) {
		pods := corev1.PodList{}
		g.Expect(input.client.List(ctx, &pods,
			// TODO: Check if we can align the operatorv1.ProvisioningControllerName with that label all the way
			client.MatchingLabels{operatorv1.DPFComponentLabelKey: "dpf-provisioning-controller-manager"})).To(Succeed())
		g.Expect(pods.Items).ToNot(BeEmpty())
		for _, pod := range pods.Items {
			g.Expect(pod.Spec.Containers).To(HaveLen(2))
			g.Expect(pod.Spec.Containers[0].Args).ToNot(ContainElement(fmt.Sprintf("--dms-pod-envs=KUBERNETES_SERVICE_HOST=%s,KUBERNETES_SERVICE_PORT=%d", testKubernetesAPIServerVIP, testKubernetesAPIServerPort)))
		}
	}).WithTimeout(120 * time.Second).Should(Succeed())

	By("Triggering DMS Pod recreation")
	triggerDMSRecreation(ctx, input.client)

	By("Validating that the DMS containers do not have the env variables anymore")
	Eventually(func(g Gomega) {
		pods := corev1.PodList{}
		g.Expect(input.client.List(ctx, &pods,
			client.MatchingLabels{cutil.ProvisioningComponentLabelKey: "hostagent"})).To(Succeed())
		g.Expect(pods.Items).ToNot(BeEmpty())
		for _, pod := range pods.Items {
			containers := slices.Concat(pod.Spec.InitContainers, pod.Spec.Containers)
			for _, container := range containers {
				g.Expect(container.Env).ToNot(ContainElements([]corev1.EnvVar{
					{
						Name:  "KUBERNETES_SERVICE_HOST",
						Value: testKubernetesAPIServerVIP,
					},
					{
						Name:  "KUBERNETES_SERVICE_PORT",
						Value: fmt.Sprintf("%d", testKubernetesAPIServerPort),
					},
				}))
			}
		}
	}).WithTimeout(120 * time.Second).Should(Succeed())
}

// triggerDMSRecreation triggers recreation of the DMS pod. This is based on how the DPUNode controller works.
func triggerDMSRecreation(ctx context.Context, c client.Client) {
	// First delete the existing DMS pods
	Expect(client.IgnoreNotFound(c.DeleteAllOf(ctx,
		&corev1.Pod{},
		client.InNamespace(dpfOperatorSystemNamespace),
		client.MatchingLabels{cutil.ProvisioningComponentLabelKey: "hostagent"}))).To(Succeed())

	// Then trigger reconcile of DPUNode Node Controller by modifying the node objects and expect that a new dms pod is created
	Eventually(func(g Gomega) {
		nodeList := &corev1.NodeList{}
		// We need to match only the nodes we expect to the DPUSet to be targeting, otherwise this test will fail because
		// it will expect DMS on a node that shouldn't have it.
		g.Expect(c.List(ctx, nodeList, client.MatchingLabels{"feature.node.kubernetes.io/dpu-enabled": "true"})).To(Succeed())
		for _, node := range nodeList.Items {
			g.Expect(testutils.ForceObjectReconcileWithAnnotation(ctx, c, &node)).To(Succeed())

			pods := corev1.PodList{}
			g.Expect(c.List(ctx, &pods,
				client.MatchingLabels{cutil.ProvisioningComponentLabelKey: "hostagent"})).To(Succeed())
			g.Expect(pods.Items).ToNot(BeEmpty())
			var found bool
			for _, pod := range pods.Items {
				// check that hostagent pod matches node and is not deleted (i.e a new one was created)
				if pod.Spec.NodeName == node.Name && pod.DeletionTimestamp == nil {
					found = true
				}
			}
			g.Expect(found).To(BeTrue())
		}
	}).WithTimeout(60 * time.Second).Should(Succeed())
}

// ValidateDPFOperatorConfigCleanupPrerequisites this function ensures that the prerequisite objects exist before removing
// the DPFOperatorConfig to ensure that we cover edge cases.
func ValidateDPFOperatorConfigCleanupPrerequisites(ctx context.Context, input *systemTestInput) {
	// Use case, 2 DPUServiceInterfaces, one created by DPUDeployment and a standalone. The DPF Operator should be able
	// to delete those gracefully without stuck finalizers due to sfc-controller missing in the DPU Cluster.
	By("Verify DPUServiceInterface owned by DPUDeployment exists and is not removed by previous tests")
	dpuServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
	Expect(input.client.List(ctx, dpuServiceInterfaceList, client.HasLabels{dpuservicev1.ParentDPUDeploymentNameLabel})).To(Succeed())
	Expect(dpuServiceInterfaceList.Items).ToNot(BeEmpty())
	dpuDeploymentOwnedServiceInterfaceLabels := make([]map[string]string, 0, len(dpuServiceInterfaceList.Items))
	for _, dpuServiceInterface := range dpuServiceInterfaceList.Items {
		dpuDeploymentOwnedServiceInterfaceLabels = append(dpuDeploymentOwnedServiceInterfaceLabels, dpuServiceInterface.Spec.Template.Spec.Template.Labels)
	}

	By("Create DPUServiceInterface and check that it is mirrored to each cluster")
	dpuServiceInterfaceName := "pf0-vf2"
	dpuServiceInterfaceNamespace := "test-dpfoperatorconfig-removal"

	By("Create test namespace")
	testNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: dpuServiceInterfaceNamespace}}
	testNS.SetLabels(CleanupScope.Suite)
	Expect(input.client.Create(ctx, testNS)).To(Succeed())

	By("Create DPUServiceInterface")
	dpuServiceInterface := input.dpuServiceInterface.DeepCopy()
	dpuServiceInterface.SetName(dpuServiceInterfaceName)
	dpuServiceInterface.SetNamespace(dpuServiceInterfaceNamespace)
	dpuServiceInterface.SetLabels(CleanupScope.Suite)
	dpuServiceInterface.Spec.Template.Spec.NodeSelector = nil
	Expect(input.client.Create(ctx, dpuServiceInterface)).To(Succeed())

	if input.hasDpuNodes() {
		By(fmt.Sprintf("Verify ServiceInterface is created in %d nodes", input.totalDPUs()))
		Eventually(func(g Gomega) {
			// Expect ServiceInterface for standalone DPUServiceInterface to be created.
			// ServiceInterface objects are created per K8s node in the DPU cluster, and each DPU device
			// becomes a separate K8s node, so the count equals totalDPUs() (nodes * DPUs per node).
			standaloneServiceInterfaceList := &dpuservicev1.ServiceInterfaceList{}
			g.Expect(dpuClusterClient[0].List(ctx, standaloneServiceInterfaceList, client.InNamespace(dpuServiceInterfaceNamespace))).To(Succeed())
			g.Expect(standaloneServiceInterfaceList.Items).To(HaveLen(input.totalDPUs()))

			// Expect ServiceInterface for DPUDeployment owned DPUServiceInterface to exist
			for _, serviceInterfaceLabels := range dpuDeploymentOwnedServiceInterfaceLabels {
				dpudeploymentOwnedServiceInterfaceList := &dpuservicev1.ServiceInterfaceList{}
				g.Expect(dpuClusterClient[0].List(ctx, dpudeploymentOwnedServiceInterfaceList, client.MatchingLabels(serviceInterfaceLabels))).To(Succeed())
				g.Expect(dpudeploymentOwnedServiceInterfaceList.Items).To(HaveLen(input.totalDPUs()))
			}
		}).WithTimeout(2 * time.Minute).Should(Succeed())
	}
}

func DeleteDPFOperatorConfig(ctx context.Context, testClient client.Client) {
	By("Delete the operatorConfig and ensure it is deleted")
	Eventually(func(g Gomega) {
		key := client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: configName}
		g.Expect(client.IgnoreNotFound(testClient.DeleteAllOf(ctx, &operatorv1.DPFOperatorConfig{}, client.InNamespace(dpfOperatorSystemNamespace)))).To(Succeed())
		g.Expect(apierrors.IsNotFound(testClient.Get(ctx, key, &operatorv1.DPFOperatorConfig{}))).To(BeTrue())
	}).WithTimeout(time.Hour).WithPolling(30 * time.Second).Should(Succeed())
	// TODO: Remove once DPUSets implement foreground deletion
	By("Ensure no leftover resources")
	// Check that all the DPUs are removed. This test in particular is needed because the DPUSet controller does not implement
	// foreground deletion, which might let DPUs lingering in Deleting without being able to delete.
	Eventually(func(g Gomega) {
		dpuList := &provisioningv1.DPUList{}
		g.Expect(testClient.List(ctx, dpuList)).To(Succeed())
		g.Expect(dpuList.Items).To(BeEmpty())
	}).WithTimeout(30 * time.Second).Should(Succeed())

	By("Ensure no leftover DPUNodeMaintenance objects")
	// DPUNodeMaintenance objects are created during provisioning to drain nodes. They must be cleaned up by the
	// DPFOperatorConfig removal, otherwise they linger after the provisioning controller is torn down.
	Eventually(func(g Gomega) {
		dpuNodeMaintenanceList := &provisioningv1.DPUNodeMaintenanceList{}
		g.Expect(testClient.List(ctx, dpuNodeMaintenanceList)).To(Succeed())
		g.Expect(dpuNodeMaintenanceList.Items).To(BeEmpty())
	}).WithTimeout(30 * time.Second).Should(Succeed())
}

// getPerClusterDPUServiceName returns the per-cluster DPUService name for a given component and DPUCluster.
func getPerClusterDPUServiceName(componentName operatorv1.ComponentName, clusterName string, clusterNamespace string) string {
	return fmt.Sprintf("%s-%s", componentName, digest.Short(digest.FromObjects(clusterName, clusterNamespace), 10))
}

// ValidatePrivilegedPodEnforcementToggle verifies the breakglass toggle for
// PrivilegedPodEnforcement in DPFOperatorConfig. Disabling the field must keep
// the ValidatingAdmissionPolicy, its binding, and the allowlist ConfigMap in the
// DPU cluster, switching the binding's validationActions to Audit (so admission
// is logged, not denied) while keeping the allowlist ConfigMap maintained (not
// cleared) so the audit log only flags pods that enforcement would have denied.
// The objects are never deleted, to avoid the Kubernetes VAP paramRef informer
// bug on re-enable (https://github.com/kubernetes/kubernetes/issues/133827).
// Re-enabling must switch the binding back to Deny.
func ValidatePrivilegedPodEnforcementToggle(ctx context.Context, input *systemTestInput) {
	vapName := "dpf-deny-privileged-pods"

	By("Getting the current DPFOperatorConfig")
	originalConfig := &operatorv1.DPFOperatorConfig{}
	Expect(input.client.Get(ctx, client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: configName}, originalConfig)).To(Succeed())

	By("Capturing the allowlist ConfigMap while enforcement is active")
	enforcingAllowlist := map[string]string{}
	Eventually(func(g Gomega) {
		cm := &corev1.ConfigMap{}
		g.Expect(dpuClusterClient[0].Get(ctx, client.ObjectKey{
			Name:      "dpf-deny-privileged-pods-allowlist",
			Namespace: dpfOperatorSystemNamespace,
		}, cm)).To(Succeed())
		enforcingAllowlist = cm.Data
	}).WithTimeout(time.Minute).Should(Succeed())

	By("Disabling PrivilegedPodEnforcement in the operatorConfig")
	configWithDisabled := originalConfig.DeepCopy()
	if configWithDisabled.Spec.Security == nil {
		configWithDisabled.Spec.Security = &operatorv1.SecurityConfiguration{}
	}
	configWithDisabled.Spec.Security.PrivilegedPodEnforcement = ptr.To(false)
	Expect(input.client.Patch(ctx, configWithDisabled, client.MergeFrom(originalConfig))).To(Succeed())

	// bindingValidationActions reads spec.validationActions from the binding.
	bindingValidationActions := func(g Gomega) []admissionregistrationv1.ValidationAction {
		binding := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{}
		g.Expect(dpuClusterClient[0].Get(ctx, client.ObjectKey{Name: vapName}, binding)).To(Succeed())
		return binding.Spec.ValidationActions
	}

	By("Verifying the VAP and binding are retained, the binding switched to Audit, and the allowlist ConfigMap kept maintained")
	Eventually(func(g Gomega) {
		vap := &admissionregistrationv1.ValidatingAdmissionPolicy{}
		g.Expect(dpuClusterClient[0].Get(ctx, client.ObjectKey{Name: vapName}, vap)).To(Succeed(),
			"ValidatingAdmissionPolicy should be retained when enforcement is disabled")

		// The binding is kept but switched to Audit so nothing is denied anymore.
		g.Expect(bindingValidationActions(g)).To(ConsistOf(admissionregistrationv1.Audit),
			"binding should be in Audit mode when enforcement is disabled")

		// The allowlist ConfigMap is never deleted (so the VAP paramRef informer does
		// not hit the Kubernetes informer-list race on re-enable) and is kept
		// maintained rather than cleared: in Audit mode the VAP still evaluates every
		// in-scope pod, so keeping the real allowlist means the audit log only flags
		// privileged pods that enforcement would have denied.
		cm := &corev1.ConfigMap{}
		g.Expect(dpuClusterClient[0].Get(ctx, client.ObjectKey{
			Name:      "dpf-deny-privileged-pods-allowlist",
			Namespace: dpfOperatorSystemNamespace,
		}, cm)).To(Succeed(), "allowlist ConfigMap should still exist when enforcement is disabled")
		g.Expect(cm.Data).To(Equal(enforcingAllowlist), "allowlist ConfigMap data should be maintained (not cleared) when enforcement is disabled")
	}).WithTimeout(5 * time.Minute).WithPolling(time.Second).Should(Succeed())

	By("Re-enabling PrivilegedPodEnforcement by reverting the operatorConfig")
	Eventually(func(g Gomega) {
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(configWithDisabled), configWithDisabled)).To(Succeed())
		resetConfig := configWithDisabled.DeepCopy()
		resetConfig.Spec = originalConfig.Spec
		g.Expect(input.client.Patch(ctx, resetConfig, client.MergeFrom(configWithDisabled))).To(Succeed())
	}).WithTimeout(30 * time.Second).Should(Succeed())

	By("Verifying the VAP and allowlist ConfigMap are present and the binding is back to Deny after re-enable")
	Eventually(func(g Gomega) {
		vap := &admissionregistrationv1.ValidatingAdmissionPolicy{}
		g.Expect(dpuClusterClient[0].Get(ctx, client.ObjectKey{Name: vapName}, vap)).To(Succeed())

		g.Expect(bindingValidationActions(g)).To(ConsistOf(admissionregistrationv1.Deny),
			"binding should be back in Deny mode when enforcement is re-enabled")

		cm := &corev1.ConfigMap{}
		g.Expect(dpuClusterClient[0].Get(ctx, client.ObjectKey{
			Name:      "dpf-deny-privileged-pods-allowlist",
			Namespace: dpfOperatorSystemNamespace,
		}, cm)).To(Succeed())
	}).WithTimeout(5 * time.Minute).WithPolling(time.Second).Should(Succeed())
}
