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
	"fmt"
	"path/filepath"
	"slices"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	testutils "github.com/nvidia/doca-platform/test/utils"

	"github.com/fluxcd/pkg/runtime/patch"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// ValidateDPFOperatorImageConfiguration verifies that DPFOperatorConfiguration Image options work.
// It changes the images for all system components to arbitrary values, checks that the changes have propagated
// and then changes them back to their default versions.
// This function tests DPUService image setting as it is complex and requires e2e testing.
func ValidateDPFOperatorImageConfiguration(ctx context.Context, input *systemTestInput) {
	modifiedConfig := &operatorv1.DPFOperatorConfig{}
	Expect(input.client.Get(ctx, client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: configName}, modifiedConfig)).To(Succeed())
	originalConfig := modifiedConfig.DeepCopy()

	dummyRegistryName := "dummy-registry.com"
	imageTemplate := "%s/%s:v1.0"
	// Update the config with a new image and tag.

	// For objects which are deployed as DPUServices set the helm chart field in configuration.
	// Excluding flannel which DPF Operator does not allow setting an image for.
	modifiedConfig.Spec.ServiceSetController = &operatorv1.ServiceSetControllerConfiguration{
		Image: ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.ServiceSetControllerName)),
	}
	modifiedConfig.Spec.Multus = &operatorv1.MultusConfiguration{
		Image: ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.MultusName)),
	}
	modifiedConfig.Spec.SRIOVDevicePlugin = &operatorv1.SRIOVDevicePluginConfiguration{
		Image: ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.SRIOVDevicePluginName)),
	}
	modifiedConfig.Spec.OVSCNI = &operatorv1.OVSCNIConfiguration{
		Image: ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.OVSCNIName)),
	}
	modifiedConfig.Spec.NVIPAM = &operatorv1.NVIPAMConfiguration{
		Image: ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.NVIPAMName)),
	}
	modifiedConfig.Spec.SFCController = &operatorv1.SFCControllerConfiguration{
		Image: ptr.To(fmt.Sprintf(imageTemplate, dummyRegistryName, operatorv1.SFCControllerName)),
	}
	Expect(input.client.Patch(ctx, modifiedConfig, client.MergeFrom(originalConfig))).To(Succeed())

	// Assert the images are set for the system components.
	Eventually(func(g Gomega) {
		inClusterDeploymentDPUServices := map[string]bool{
			operatorv1.ServiceSetControllerName: true,
		}
		deploymentDPUservices := map[string]bool{
			operatorv1.NVIPAMName: true,
		}
		daemonSetDPUServices := map[string]bool{
			operatorv1.SRIOVDevicePluginName: true,
			operatorv1.SFCControllerName:     true,
			operatorv1.OVSCNIName:            true,
			operatorv1.NVIPAMName:            true,
			operatorv1.MultusName:            true,
		}

		// Verify images for inCluster DPUServices
		for name := range inClusterDeploymentDPUServices {
			deployments := appsv1.DeploymentList{}
			nameForCluster := fmt.Sprintf("%s-%s", "in-cluster", name)
			g.Expect(input.client.List(ctx, &deployments,
				client.MatchingLabels{argoCDInstanceLabel: nameForCluster})).To(Succeed())
			g.Expect(deployments.Items).To(HaveLen(1))
			deployment := deployments.Items[0]
			g.Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
			g.Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(ContainSubstring(dummyRegistryName))
		}
		// Verify images in the DPUClusters
		for name := range deploymentDPUservices {
			deployments := appsv1.DeploymentList{}
			nameForCluster := fmt.Sprintf("%s-%s", input.dpuCluster.Name, name)
			g.Expect(dpuClusterClient.List(ctx, &deployments,
				client.MatchingLabels{argoCDInstanceLabel: nameForCluster})).To(Succeed())
			g.Expect(deployments.Items).To(HaveLen(1))
			deployment := deployments.Items[0]
			g.Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
			g.Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(ContainSubstring(dummyRegistryName))
		}
		for name := range daemonSetDPUServices {
			daemonSets := appsv1.DaemonSetList{}
			nameForCluster := fmt.Sprintf("%s-%s", input.dpuCluster.Name, name)
			g.Expect(dpuClusterClient.List(ctx, &daemonSets,
				client.MatchingLabels{argoCDInstanceLabel: nameForCluster})).To(Succeed())
			g.Expect(daemonSets.Items).To(HaveLen(1))
			daemonSet := daemonSets.Items[0]
			g.Expect(daemonSet.Spec.Template.Spec.Containers[0].Image).To(ContainSubstring(dummyRegistryName))
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
	modifiedConfig.Spec.Networking.ControlPlaneMTU = ptr.To(1200)
	modifiedConfig.Spec.Networking.HighSpeedMTU = ptr.To(9000)
	Eventually(func(g Gomega) {
		g.Expect(input.client.Patch(ctx, modifiedConfig, client.MergeFrom(originalConfig))).To(Succeed())
	}).Should(Succeed())

	By("verify flannel and multus for cluster " + input.dpuCluster.Name)
	Eventually(func(g Gomega) {
		flannelConfigMap := &corev1.ConfigMap{}
		g.Expect(dpuClusterClient.Get(ctx, client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: "kube-flannel-cfg"}, flannelConfigMap)).To(Succeed())
		g.Expect(flannelConfigMap.Data["net-conf.json"]).To(ContainSubstring("MTU\": 1200,"))

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

	dpuServiceDaemonSetsWithPathChanges := map[string]bool{
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
			client.MatchingLabels{cutil.ProvisioningComponentLabelKey: "dms"})).To(Succeed())
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
			client.MatchingLabels{cutil.ProvisioningComponentLabelKey: "dms"})).To(Succeed())
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
		client.MatchingLabels{cutil.ProvisioningComponentLabelKey: "dms"}))).To(Succeed())

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
				client.MatchingLabels{cutil.ProvisioningComponentLabelKey: "dms"})).To(Succeed())
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

func ValidateOperatorFullCreation(ctx context.Context, input *systemTestInput) {
	By("delete DPUs and DPUSets and ensure they are deleted for a clean test condition")
	if input.skipCleanup {
		Skip("Skip cleanup resources")
	}
	Eventually(func(g Gomega) {
		dpuSetList := &provisioningv1.DPUSetList{}
		dpuList := &provisioningv1.DPUList{}
		g.Expect(client.IgnoreNotFound(input.client.DeleteAllOf(ctx, &provisioningv1.DPUSet{}, client.InNamespace(dpfOperatorSystemNamespace)))).To(Succeed())
		g.Expect(input.client.List(ctx, dpuSetList)).To(Succeed())
		g.Expect(dpuSetList.Items).To(BeEmpty())

		// Expect all DPUs to have been deleted.
		g.Expect(input.client.List(ctx, dpuList)).To(Succeed())
		g.Expect(dpuList.Items).To(BeEmpty())

		nodes := &corev1.NodeList{}
		g.Expect(dpuClusterClient.List(ctx, nodes)).To(Succeed())
		By(fmt.Sprintf("Expected number of nodes %d to equal %d", len(nodes.Items), 0))
		g.Expect(nodes.Items).To(BeEmpty())
	}).WithTimeout(10 * time.Minute).Should(Succeed())

	By("create a DPUDeployment with its dependencies and ensure that the underlying objects are created")
	dpuServiceTemplate := input.dpuServiceTemplate.DeepCopy()
	dpuServiceTemplate.SetLabels(afterAllCleanupLabels)
	Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, dpuServiceTemplate))).To(Succeed())

	dpuServiceConfiguration := input.dpuServiceConfiguration.DeepCopy()
	dpuServiceConfiguration.SetLabels(afterAllCleanupLabels)
	Expect(client.IgnoreAlreadyExists(input.client.Create(ctx, dpuServiceConfiguration))).To(Succeed())

	dpuDeployment := generateDPUObj("dpf-cleanup", input.dpuDeployment.DeepCopy().Namespace, input.dpuDeployment.DeepCopy(), afterAllCleanupLabels)
	dpuDeployment.Spec.DPUs.DPUSets[0].NodeSelector = &metav1.LabelSelector{
		MatchLabels: map[string]string{"feature.node.kubernetes.io/dpu-enabled": "true"},
	}
	Expect(input.client.Create(ctx, dpuDeployment)).To(Succeed())

	Eventually(func(g Gomega) {
		g.Expect(VerifyDeploymentUnderlyingObjectsCreated(ctx, g, input.client, dpuDeployment)).To(BeTrue())
	}).WithTimeout(180 * time.Second).Should(Succeed())

	tracker := NewByTracker()
	Eventually(func(g Gomega) {
		if !input.hasDpuNodes() {
			return
		}
		nodes := &corev1.NodeList{}
		g.Expect(dpuClusterClient.List(ctx, nodes)).To(Succeed())
		nodeKey := fmt.Sprintf("%d/%d", len(nodes.Items), input.numberOfDPUNodes)
		tracker.By(nodeKey, "Checking that the number of nodes %d is equal to %d", len(nodes.Items), input.numberOfDPUNodes)
		g.Expect(nodes.Items).To(HaveLen(input.numberOfDPUNodes))
	}).WithTimeout(30 * time.Minute).WithPolling(1 * time.Second).Should(Succeed())

	By("create DPUServiceInterface and check that it is mirrored to each cluster")
	dpuServiceInterfaceName := "pf0-vf2"
	dpuServiceInterfaceNamespace := "test-dpudeployment"
	By("create test namespace")
	testNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: dpuServiceInterfaceNamespace}}
	testNS.SetLabels(afterAllCleanupLabels)
	Expect(input.client.Create(ctx, testNS)).To(Succeed())
	By("create DPUServiceInterface")
	dpuServiceInterface := input.dpuServiceInterface.DeepCopy()
	dpuServiceInterface.SetName(dpuServiceInterfaceName)
	dpuServiceInterface.SetNamespace(dpuServiceInterfaceNamespace)
	dpuServiceInterface.SetLabels(afterAllCleanupLabels)
	Expect(input.client.Create(ctx, dpuServiceInterface)).To(Succeed())

	serviceInterfaceLabels := map[string]string{}
	By("verify ServiceInterfaceSet is created in DPF clusters")
	Eventually(func(g Gomega) {
		//get the DPUServiceInterface owned by DPUDeployment
		dpuServiceInterfaceList := &dpuservicev1.DPUServiceInterfaceList{}
		g.Expect(input.client.List(ctx, dpuServiceInterfaceList,
			client.MatchingLabels{
				"svc.dpu.nvidia.com/owned-by-dpudeployment": fmt.Sprintf("%s_%s", dpuDeployment.GetNamespace(), dpuDeployment.GetName())})).
			To(Succeed())
		g.Expect(dpuServiceInterfaceList.Items).To(HaveLen(1))
		// getting labels for ServiceInterface check
		serviceInterfaceLabels = dpuServiceInterfaceList.Items[0].Spec.Template.Spec.Template.Labels

		// verify ServiceInterfaceSet is created in namespace
		serviceInterfaceSetListInNamespace := &dpuservicev1.ServiceInterfaceSetList{}
		g.Expect(dpuClusterClient.List(ctx, serviceInterfaceSetListInNamespace,
			client.InNamespace(dpuServiceInterfaceNamespace),
		)).To(Succeed())
		g.Expect(serviceInterfaceSetListInNamespace.Items).To(HaveLen(1))

		// verify ServiceInterfaceSet is reconciled
		serviceInterfaceSetReconciled := &dpuservicev1.ServiceInterfaceSetList{}
		g.Expect(dpuClusterClient.List(ctx, serviceInterfaceSetReconciled,
			client.InNamespace(dpuDeployment.Namespace),
		)).To(Succeed())
		g.Expect(serviceInterfaceSetReconciled.Items).NotTo(BeEmpty())
	}, time.Second*300, time.Millisecond*250).Should(Succeed())

	if input.hasDpuNodes() {
		By(fmt.Sprintf("verify ServiceInterface is created in %d nodes", input.numberOfDPUNodes))
		Eventually(func(g Gomega) {
			serviceInterfaceList := &dpuservicev1.ServiceInterfaceList{}
			g.Expect(dpuClusterClient.List(ctx, serviceInterfaceList, client.MatchingLabels(serviceInterfaceLabels))).To(Succeed())
			g.Expect(serviceInterfaceList.Items).To(Not(BeEmpty()))
		}).WithTimeout(30 * time.Minute).WithPolling(120 * time.Second).Should(Succeed())
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
