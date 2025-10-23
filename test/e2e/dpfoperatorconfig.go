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
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/operator/inventory"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	testutils "github.com/nvidia/doca-platform/test/utils"

	"github.com/fluxcd/pkg/runtime/patch"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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
	modifiedConfig.Spec.OVSCNI = &operatorv1.OVSCNIConfiguration{
		CNI: &operatorv1.DefaultOverridesConfiguration{
			ImageComponentConfig: operatorv1.ImageComponentConfig{
				Image: ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.OVSCNIName)),
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
				Image: ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.NVIPAMName)),
			},
			ResourceComponentConfig: dummyResourceRequirements,
		},
		Node: &operatorv1.NVIPAMNode{
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

	By("updating the DPFOperatorConfig with modified images and resources")
	Expect(input.client.Patch(ctx, modifiedConfig, client.MergeFrom(originalConfig))).To(Succeed())

	By("verifying all components are updated")
	verifyComponentOverrides(ctx, input, dummyRegistryName, expectedDummyResources)

	dummyRegistryName = "overridden-registry.com"
	configCopy := modifiedConfig.DeepCopy()
	modifiedConfig.Spec.ServiceSetController.Controller.Image = nil
	modifiedConfig.Spec.ServiceSetController.Image = ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.ServiceSetControllerName))
	modifiedConfig.Spec.Multus.CNI.Image = nil
	modifiedConfig.Spec.Multus.Image = ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.MultusName))
	modifiedConfig.Spec.SRIOVDevicePlugin.DevicePlugin.Image = nil
	modifiedConfig.Spec.SRIOVDevicePlugin.Image = ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.SRIOVDevicePluginName))
	modifiedConfig.Spec.OVSCNI.CNI.Image = nil
	modifiedConfig.Spec.OVSCNI.Image = ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.OVSCNIName))
	modifiedConfig.Spec.SFCController.Controller.Image = nil
	modifiedConfig.Spec.SFCController.Image = ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.SFCControllerName))
	modifiedConfig.Spec.NVIPAM.Controller.Image = nil
	modifiedConfig.Spec.NVIPAM.Image = ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.NVIPAMName))
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
	Expect(input.client.Patch(ctx, modifiedConfig, client.MergeFrom(configCopy))).To(Succeed())

	By("verifying legacy component overrides")
	verifyComponentOverrides(ctx, input, dummyRegistryName, expectedDummyResources)

	By("reverting the DPFOperatorConfig to its original setting.")
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
		}
		deploymentDPUservices := map[operatorv1.ComponentName]bool{
			operatorv1.NVIPAMName: true,
		}
		daemonSetDPUServices := map[operatorv1.ComponentName]bool{
			operatorv1.SRIOVDevicePluginName: true,
			operatorv1.SFCControllerName:     true,
			operatorv1.OVSCNIName:            true,
			operatorv1.NVIPAMName:            true,
			operatorv1.MultusName:            true,
			operatorv1.FlannelName:           true,
		}
		controller := map[string]bool{
			inventory.DPFProvisioningControllerName: true,
			inventory.DPUServiceControllerName:      true,
		}

		deployValidation := func(c client.Client, clusterName, name string) {
			nameForCluster := fmt.Sprintf("%s-%s", clusterName, name)
			tracker.By(nameForCluster, "verifying overrides for %s", nameForCluster)
			deployments := appsv1.DeploymentList{}
			g.Expect(c.List(ctx, &deployments,
				client.MatchingLabels{argoCDInstanceLabel: nameForCluster})).To(Succeed())
			deployment := deployments.Items[0]
			for _, container := range deployment.Spec.Template.Spec.Containers {
				g.Expect(container.Image).To(ContainSubstring(dummyRegistryName))
				g.Expect(container.Resources).To(BeEquivalentTo(expectedDummyResources))
			}
		}

		// Verify overrides for inCluster DPUServices
		for name := range inClusterDeploymentDPUServices {
			deployValidation(input.client, "in-cluster", name.String())
		}
		// Verify overrides in the DPUClusters
		for name := range deploymentDPUservices {
			deployValidation(dpuClusterClient, input.dpuCluster.Name, name.String())
		}
		for name := range daemonSetDPUServices {
			nameForCluster := fmt.Sprintf("%s-%s", input.dpuCluster.Name, name)
			tracker.By(nameForCluster, "verifying overrides for %s", nameForCluster)
			daemonSets := appsv1.DaemonSetList{}
			g.Expect(dpuClusterClient.List(ctx, &daemonSets,
				client.MatchingLabels{argoCDInstanceLabel: nameForCluster})).To(Succeed())
			g.Expect(daemonSets.Items).To(HaveLen(1))
			daemonSet := daemonSets.Items[0]
			for _, container := range daemonSet.Spec.Template.Spec.Containers {
				g.Expect(container.Image).To(ContainSubstring(dummyRegistryName))
				g.Expect(container.Resources).To(BeEquivalentTo(expectedDummyResources))
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
				g.Expect(container.Image).To(ContainSubstring(dummyRegistryName))
				g.Expect(container.Resources).To(BeEquivalentTo(expectedDummyResources))
			}
		}
	}, 120*time.Second).Should(Succeed())
}

func ValidateDPFOperatorMTUCurrentConfiguration(ctx context.Context, input *systemTestInput) {
	By("verify flannel configmap for cluster " + input.dpuCluster.Name)
	flannelConfigMap := &corev1.ConfigMap{}
	Expect(dpuClusterClient.Get(ctx, client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: "kube-flannel-cfg"}, flannelConfigMap)).To(Succeed())
	Expect(flannelConfigMap.Data["net-conf.json"]).To(ContainSubstring("MTU\": 1500,"))
}

func ValidateDPFOperatorMTUConfigurationChange(ctx context.Context, input *systemTestInput) {
	By("get the operatorConfig")
	modifiedConfig := &operatorv1.DPFOperatorConfig{}
	Expect(input.client.Get(ctx, client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: configName}, modifiedConfig)).To(Succeed())
	By("update the MTU in the operatorConfig")
	originalConfig := modifiedConfig.DeepCopy()
	if modifiedConfig.Spec.Networking == nil {
		modifiedConfig.Spec.Networking = &operatorv1.Networking{}
	}
	modifiedConfig.Spec.Networking.ControlPlaneMTU = ptr.To(1300)
	modifiedConfig.Spec.Networking.HighSpeedMTU = ptr.To(9000)
	Eventually(func(g Gomega) {
		g.Expect(input.client.Patch(ctx, modifiedConfig, client.MergeFrom(originalConfig))).To(Succeed())
	}).Should(Succeed())

	By("verify flannel and multus for cluster " + input.dpuCluster.Name)
	Eventually(func(g Gomega) {
		flannelConfigMap := &corev1.ConfigMap{}
		g.Expect(dpuClusterClient.Get(ctx, client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: "kube-flannel-cfg"}, flannelConfigMap)).To(Succeed())
		g.Expect(flannelConfigMap.Data["net-conf.json"]).To(ContainSubstring("MTU\": 1300,"))

		netAttachDef := &unstructured.Unstructured{}
		netAttachDef.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "k8s.cni.cncf.io",
			Version: "v1",
			Kind:    "NetworkAttachmentDefinition",
		})

		g.Expect(dpuClusterClient.Get(ctx, client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: "mybrsfc"}, netAttachDef)).To(Succeed())
		netAttachConfig, exists, err := unstructured.NestedString(netAttachDef.Object, "spec", "config")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(exists).To(BeTrue())
		g.Expect(netAttachConfig).To(ContainSubstring("mtu\": 9000,"))

		g.Expect(dpuClusterClient.Get(ctx, client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: "mybrhbn"}, netAttachDef)).To(Succeed())
		netAttachConfig, exists, err = unstructured.NestedString(netAttachDef.Object, "spec", "config")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(exists).To(BeTrue())
		g.Expect(netAttachConfig).To(ContainSubstring("mtu\": 9000,"))
	}, time.Second*30).Should(Succeed())

	By("reverting the DPFOperatorConfig to its original setting.")
	Eventually(func(g Gomega) {
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(modifiedConfig), modifiedConfig)).To(Succeed())
		resetConfig := modifiedConfig.DeepCopy()
		resetConfig.Spec = originalConfig.Spec
		// Revert the image versions to their previous values.
		g.Expect(input.client.Patch(ctx, resetConfig, client.MergeFrom(modifiedConfig))).To(Succeed())
		// Ensure the changes are reverted before continuing.
	}).Should(Succeed())
}

func ValidateDPFOperatorFlannelPodCIDRChange(ctx context.Context, input *systemTestInput) {
	By("get the operatorConfig")
	modifiedConfig := &operatorv1.DPFOperatorConfig{}
	Expect(input.client.Get(ctx, client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: configName}, modifiedConfig)).To(Succeed())
	By("update the podCIDR in the operatorConfig")
	originalConfig := modifiedConfig.DeepCopy()
	if modifiedConfig.Spec.Flannel == nil {
		modifiedConfig.Spec.Flannel = &operatorv1.FlannelConfiguration{}
	}
	modifiedConfig.Spec.Flannel.PodCIDR = ptr.To("10.255.0.0/14")
	Eventually(func(g Gomega) {
		g.Expect(input.client.Patch(ctx, modifiedConfig, client.MergeFrom(originalConfig))).To(Succeed())
	}).Should(Succeed())

	By("verify flannel configmap for cluster " + input.dpuCluster.Name)
	Eventually(func(g Gomega) {
		flannelConfigMap := &corev1.ConfigMap{}
		g.Expect(dpuClusterClient.Get(ctx, client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: "kube-flannel-cfg"}, flannelConfigMap)).To(Succeed())
		g.Expect(flannelConfigMap.Data["net-conf.json"]).To(ContainSubstring("10.255.0.0/14"))
	}, time.Second*30).Should(Succeed())

	By("reverting the DPFOperatorConfig to its original setting.")
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
	var newPodUID types.UID

	By("getting the current provisioning controller pod UID")
	var originalPodUID types.UID
	Eventually(func(g Gomega) {
		pods := corev1.PodList{}
		g.Expect(input.client.List(ctx, &pods,
			client.MatchingLabels{operatorv1.DPFComponentLabelKey: "dpf-provisioning-controller-manager"})).To(Succeed())
		g.Expect(pods.Items).To(HaveLen(1))
		originalPodUID = pods.Items[0].UID
	}).WithTimeout(120 * time.Second).Should(Succeed())

	By("modifying the DPFOperatorConfig to set MaxDPUParallelInstallations")
	modifiedConfig.Spec.ProvisioningController.MaxDPUParallelInstallations = ptr.To(int32(25))
	Expect(input.client.Patch(ctx, modifiedConfig, client.MergeFrom(originalConfig))).To(Succeed())

	By("verifying that the provisioning controller pod is restarted")
	Eventually(func(g Gomega) {
		pods := corev1.PodList{}
		g.Expect(input.client.List(ctx, &pods,
			client.MatchingLabels{operatorv1.DPFComponentLabelKey: "dpf-provisioning-controller-manager"})).To(Succeed())
		g.Expect(pods.Items).To(HaveLen(1))
		newPodUID = pods.Items[0].UID
		g.Expect(newPodUID).ToNot(Equal(originalPodUID))
	}).WithTimeout(120 * time.Second).Should(Succeed())

	By("reverting the DPFOperatorConfig to its original setting")
	Eventually(func(g Gomega) {
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(modifiedConfig), modifiedConfig)).To(Succeed())
		resetConfig := modifiedConfig.DeepCopy()
		resetConfig.Spec = originalConfig.Spec
		g.Expect(input.client.Patch(ctx, resetConfig, client.MergeFrom(modifiedConfig))).To(Succeed())
	}).WithTimeout(10 * time.Second).Should(Succeed())

	By("verifying that the provisioning controller pod is restarted again")
	Eventually(func(g Gomega) {
		pods := corev1.PodList{}
		g.Expect(input.client.List(ctx, &pods,
			client.MatchingLabels{operatorv1.DPFComponentLabelKey: "dpf-provisioning-controller-manager"})).To(Succeed())
		g.Expect(pods.Items).To(HaveLen(1))
		g.Expect(pods.Items[0].UID).ToNot(Equal(newPodUID))
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
	modifiedConfig.Spec.Overrides = &operatorv1.Overrides{
		DPUCNIBinPath:                       ptr.To(modifiedCNIBinPath),
		DPUCNIConfigPath:                    ptr.To(modifiedCNIConfigPath),
		DPUOpenvSwitchRunPath:               ptr.To(modifiedOVSRunPath),
		DPUOpenvSwitchBinPath:               ptr.To(modifiedOVSBinPath),
		DPUOpenvSwitchSystemSharedLibPath:   ptr.To(modifiedOVSharedLibPath),
		DPUOpenvSwitchSystemSharedLib64Path: ptr.To(modifiedOVSharedLib64Path),
		FlannelSkipCNIConfigInstallation:    ptr.To(false),
	}
	Expect(input.client.Patch(ctx, modifiedConfig, client.MergeFrom(originalConfig))).To(Succeed())

	dpuServiceDaemonSetsWithPathChanges := map[operatorv1.ComponentName]bool{
		operatorv1.SFCControllerName: true,
		operatorv1.OVSCNIName:        true,
		operatorv1.NVIPAMName:        true,
		operatorv1.MultusName:        true,
		operatorv1.FlannelName:       true,
	}

	// Assert the images are set for the system components.
	Eventually(func(g Gomega) {
		for name := range dpuServiceDaemonSetsWithPathChanges {
			daemonSets := appsv1.DaemonSetList{}
			nameForCluster := fmt.Sprintf("%s-%s", input.dpuCluster.Name, name)
			g.Expect(dpuClusterClient.List(ctx, &daemonSets,
				client.MatchingLabels{argoCDInstanceLabel: nameForCluster})).To(Succeed())
			g.Expect(daemonSets.Items).To(HaveLen(1))
			volumes := daemonSets.Items[0].Spec.Template.Spec.Volumes
			switch name {
			case operatorv1.SFCControllerName:
				g.Expect(volumeNameHasPath("ovs", volumes, filepath.Join(modifiedOVSRunPath))).To(BeTrue())
				g.Expect(volumeNameHasPath("ovs-ofctl", volumes, filepath.Join(modifiedOVSBinPath, "ovs-ofctl"))).To(BeTrue())
				g.Expect(volumeNameHasPath("ovs-vsctl", volumes, filepath.Join(modifiedOVSBinPath, "ovs-vsctl"))).To(BeTrue())
				g.Expect(volumeNameHasPath("lib", volumes, filepath.Join(modifiedOVSharedLibPath))).To(BeTrue())
				g.Expect(volumeNameHasPath("lib64", volumes, filepath.Join(modifiedOVSharedLib64Path))).To(BeTrue())
			case operatorv1.OVSCNIName:
				g.Expect(volumeNameHasPath("cnibin", volumes, filepath.Join(modifiedCNIBinPath))).To(BeTrue())
				g.Expect(volumeNameHasPath("ovs-var-run", volumes, filepath.Join(modifiedOVSRunPath))).To(BeTrue())
			case operatorv1.NVIPAMName:
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
				g.Expect(daemonSets.Items[0].Spec.Template.Spec.InitContainers).To(HaveLen(2), "Flannel should have 2 init containers when skipCNIConfigInstallation is false")

				// Find the install-cni init container
				var foundInstallCNI bool
				for _, initContainer := range daemonSets.Items[0].Spec.Template.Spec.InitContainers {
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

	By("reverting the DPFOperatorConfig to its original setting.")
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
		Skip("test requires node to trigger provisioning on, skipping")
	}

	modifiedConfig := &operatorv1.DPFOperatorConfig{}
	Expect(input.client.Get(ctx, client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: configName}, modifiedConfig)).To(Succeed())
	originalConfig := modifiedConfig.DeepCopy()

	By("modifying the DPFOperatorConfig to set the Kubernetes API Server related variables")
	modifiedConfig.Spec.Overrides = &operatorv1.Overrides{
		KubernetesAPIServerVIP:  ptr.To("192.168.1.1"),
		KubernetesAPIServerPort: ptr.To(1111),
	}
	Expect(input.client.Patch(ctx, modifiedConfig, client.MergeFrom(originalConfig))).To(Succeed())

	By("validating that the provisioning controller pod has the correct argument")
	Eventually(func(g Gomega) {
		pods := corev1.PodList{}
		g.Expect(input.client.List(ctx, &pods,
			// TODO: Check if we can align the operatorv1.ProvisioningControllerName with that label in the manifests
			// all the way
			client.MatchingLabels{operatorv1.DPFComponentLabelKey: "dpf-provisioning-controller-manager"})).To(Succeed())
		g.Expect(pods.Items).To(HaveLen(1))
		pod := pods.Items[0]
		g.Expect(pod.Spec.Containers).To(HaveLen(1))
		g.Expect(pod.Spec.Containers[0].Args).To(ContainElement("--dms-pod-envs=KUBERNETES_SERVICE_HOST=192.168.1.1,KUBERNETES_SERVICE_PORT=1111"))
	}).WithTimeout(120 * time.Second).Should(Succeed())

	By("Triggering recreation DMS Pod")
	triggerDMSRecreation(ctx, input.client)

	By("validating that all the dms containers have the environment variables set correctly")
	Eventually(func(g Gomega) {
		pods := corev1.PodList{}
		g.Expect(input.client.List(ctx, &pods,
			client.MatchingLabels{cutil.ProvisioningComponentLabelKey: "hostagent"})).To(Succeed())
		g.Expect(pods.Items).ToNot(BeEmpty())
		for _, pod := range pods.Items {
			containers := slices.Concat(pod.Spec.InitContainers, pod.Spec.Containers)
			for _, container := range containers {
				g.Expect(container.Env).To(ContainElements([]corev1.EnvVar{
					{
						Name:  "KUBERNETES_SERVICE_HOST",
						Value: "192.168.1.1",
					},
					{
						Name:  "KUBERNETES_SERVICE_PORT",
						Value: "1111",
					},
				}))
			}
		}
	}).WithTimeout(120 * time.Second).Should(Succeed())

	By("reverting the DPFOperatorConfig to its original setting")
	Eventually(func(g Gomega) {
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(modifiedConfig), modifiedConfig)).To(Succeed())
		resetConfig := modifiedConfig.DeepCopy()
		resetConfig.Spec = originalConfig.Spec
		g.Expect(input.client.Patch(ctx, resetConfig, client.MergeFrom(modifiedConfig))).To(Succeed())
	}).WithTimeout(10 * time.Second).Should(Succeed())

	By("validating that the provisioning controller pod has the correct argument")
	Eventually(func(g Gomega) {
		pods := corev1.PodList{}
		g.Expect(input.client.List(ctx, &pods,
			// TODO: Check if we can align the operatorv1.ProvisioningControllerName with that label all the way
			client.MatchingLabels{operatorv1.DPFComponentLabelKey: "dpf-provisioning-controller-manager"})).To(Succeed())
		g.Expect(pods.Items).To(HaveLen(1))
		pod := pods.Items[0]
		g.Expect(pod.Spec.Containers).To(HaveLen(1))
		g.Expect(pod.Spec.Containers[0].Args).ToNot(ContainElement("--dms-pod-envs=KUBERNETES_SERVICE_HOST=192.168.1.1,KUBERNETES_SERVICE_PORT=1111"))
	}).WithTimeout(120 * time.Second).Should(Succeed())

	By("Triggering recreation DMS Pod")
	triggerDMSRecreation(ctx, input.client)

	By("validating that the dms containers do not have the env variables anymore")
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
						Value: "192.168.1.1",
					},
					{
						Name:  "KUBERNETES_SERVICE_PORT",
						Value: "1111",
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

	// Then remove the existing DPUNodes
	Expect(client.IgnoreNotFound(c.DeleteAllOf(ctx, &provisioningv1.DPUNode{}, client.InNamespace(dpfOperatorSystemNamespace)))).To(Succeed())

	// TODO: Remove this block once DPUNode finalizer from the DPU controller is no longer a requirement
	Eventually(func(g Gomega) {
		dpuNodeList := &provisioningv1.DPUNodeList{}
		g.Expect(c.List(ctx, dpuNodeList)).To(Succeed())
		for _, dpuNode := range dpuNodeList.Items {
			g.Expect(dpuNode.DeletionTimestamp).ToNot(BeNil())
			patcher := patch.NewSerialPatcher(&dpuNode, c)
			controllerutil.RemoveFinalizer(&dpuNode, provisioningv1.DPUNodeFinalizer)
			g.Expect(patcher.Patch(ctx, &dpuNode)).To(Succeed())
		}
	}).WithTimeout(120 * time.Second).Should(Succeed())

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
				if pod.Spec.NodeName == node.Name {
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
	By("verify DPUServiceInterface owned by DPUDeployment exists and is not removed by previous tests")
	dpuServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
	Expect(input.client.List(ctx, dpuServiceInterfaceList, client.HasLabels{dpuservicev1.ParentDPUDeploymentNameLabel})).To(Succeed())
	Expect(dpuServiceInterfaceList.Items).ToNot(BeEmpty())
	dpuDeploymentOwnedServiceInterfaceLabels := make([]map[string]string, 0, len(dpuServiceInterfaceList.Items))
	for _, dpuServiceInterface := range dpuServiceInterfaceList.Items {
		dpuDeploymentOwnedServiceInterfaceLabels = append(dpuDeploymentOwnedServiceInterfaceLabels, dpuServiceInterface.Spec.Template.Spec.Template.Labels)
	}

	By("create DPUServiceInterface and check that it is mirrored to each cluster")
	dpuServiceInterfaceName := "pf0-vf2"
	dpuServiceInterfaceNamespace := "test-dpfoperatorconfig-removal"

	By("create test namespace")
	testNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: dpuServiceInterfaceNamespace}}
	testNS.SetLabels(afterAllCleanupLabels)
	Expect(input.client.Create(ctx, testNS)).To(Succeed())

	By("create DPUServiceInterface")
	dpuServiceInterface := input.dpuServiceInterface.DeepCopy()
	dpuServiceInterface.SetName(dpuServiceInterfaceName)
	dpuServiceInterface.SetNamespace(dpuServiceInterfaceNamespace)
	dpuServiceInterface.SetLabels(afterAllCleanupLabels)
	dpuServiceInterface.Spec.Template.Spec.NodeSelector = nil
	Expect(input.client.Create(ctx, dpuServiceInterface)).To(Succeed())

	if input.hasDpuNodes() {
		By(fmt.Sprintf("verify ServiceInterface is created in %d nodes", input.numberOfDPUNodes))
		Eventually(func(g Gomega) {
			// Expect ServiceInterface for standalone DPUServiceInterface to be created
			standaloneServiceInterfaceList := &dpuservicev1.ServiceInterfaceList{}
			g.Expect(dpuClusterClient.List(ctx, standaloneServiceInterfaceList, client.InNamespace(dpuServiceInterfaceNamespace))).To(Succeed())
			g.Expect(standaloneServiceInterfaceList.Items).To(HaveLen(input.numberOfDPUNodes))

			// Expect ServiceInterface for DPUDeployment owned DPUServiceInterface to exist
			for _, serviceInterfaceLabels := range dpuDeploymentOwnedServiceInterfaceLabels {
				dpudeploymentOwnedServiceInterfaceList := &dpuservicev1.ServiceInterfaceList{}
				g.Expect(dpuClusterClient.List(ctx, dpudeploymentOwnedServiceInterfaceList, client.MatchingLabels(serviceInterfaceLabels))).To(Succeed())
				g.Expect(dpudeploymentOwnedServiceInterfaceList.Items).To(HaveLen(input.numberOfDPUNodes))
			}
		}).WithTimeout(2 * time.Minute).Should(Succeed())
	}
}

func DeleteDPFOperatorConfig(ctx context.Context, testClient client.Client) {
	By("delete the operatorConfig and ensure it is deleted")
	Eventually(func(g Gomega) {
		key := client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: configName}
		g.Expect(client.IgnoreNotFound(testClient.DeleteAllOf(ctx, &operatorv1.DPFOperatorConfig{}, client.InNamespace(dpfOperatorSystemNamespace)))).To(Succeed())
		g.Expect(apierrors.IsNotFound(testClient.Get(ctx, key, &operatorv1.DPFOperatorConfig{}))).To(BeTrue())
	}).WithTimeout(time.Hour).WithPolling(30 * time.Second).Should(Succeed())
	// TODO: Remove once DPUSets implement foreground deletion
	By("ensure no leftover resources")
	// Check that all the DPUs are removed. This test in particular is needed because the DPUSet controller does not implement
	// foreground deletion, which might let DPUs lingering in Deleting without being able to delete.
	Eventually(func(g Gomega) {
		dpuList := &provisioningv1.DPUList{}
		g.Expect(testClient.List(ctx, dpuList)).To(Succeed())
		g.Expect(dpuList.Items).To(BeEmpty())
	}).WithTimeout(30 * time.Second).Should(Succeed())
}
