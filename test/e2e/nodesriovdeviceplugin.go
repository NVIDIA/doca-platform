/*
Copyright 2026 NVIDIA

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
	"time"

	noderesourcesv1 "github.com/nvidia/doca-platform/api/noderesources/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	nodesriovctrl "github.com/nvidia/doca-platform/internal/nodesriovdeviceplugin/controllers"
	"github.com/nvidia/doca-platform/pkg/conditions"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ValidateNodeSRIOVDevicePluginWebhookRejectsInvalid creates
// NodeSRIOVDevicePluginConfigs that violate cross-field validation rules and
// verifies that the validating webhook rejects them.

//nolint:dupl
func ValidateNodeSRIOVDevicePluginWebhookRejectsInvalid(ctx context.Context, input *SystemTestInput) {
	By("Creating a NodeSRIOVDevicePluginConfig with overlapping VF ranges")
	Eventually(func(g Gomega) {
		invalidConfig := &noderesourcesv1.NodeSRIOVDevicePluginConfig{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "e2e-invalid-config-",
				Namespace:    dpfOperatorSystemNamespace,
				Labels:       CleanupScope.It,
			},
			Spec: noderesourcesv1.NodeSRIOVDevicePluginConfigSpec{
				DevicePluginResources: []noderesourcesv1.DevicePluginResource{
					{
						Name: "res_a",
						Type: noderesourcesv1.DevicePluginResourceTypeVF,
						Ranges: []noderesourcesv1.VFRange{
							{PFIndex: 0, Start: ptr.To[int32](0), End: ptr.To[int32](7)},
						},
					},
					{
						Name: "res_b",
						Type: noderesourcesv1.DevicePluginResourceTypeVF,
						Ranges: []noderesourcesv1.VFRange{
							{PFIndex: 0, Start: ptr.To[int32](4), End: ptr.To[int32](10)},
						},
					},
				},
			},
		}
		err := input.Client.Create(ctx, invalidConfig)
		g.Expect(err).To(HaveOccurred(),
			"webhook should reject overlapping VF ranges")
		g.Expect(apierrors.IsForbidden(err) || apierrors.IsInvalid(err)).To(
			BeTrue(), fmt.Sprintf("expected Forbidden or Invalid, got: %v", err))
	}).WithTimeout(time.Minute).Should(Succeed())

	By("Creating a NodeSRIOVDevicePluginConfig with duplicate name+prefix")
	Eventually(func(g Gomega) {
		duplicateConfig := &noderesourcesv1.NodeSRIOVDevicePluginConfig{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "e2e-duplicate-config-",
				Namespace:    dpfOperatorSystemNamespace,
				Labels:       CleanupScope.It,
			},
			Spec: noderesourcesv1.NodeSRIOVDevicePluginConfigSpec{
				DevicePluginResources: []noderesourcesv1.DevicePluginResource{
					{
						Name: "dup_res",
						Type: noderesourcesv1.DevicePluginResourceTypeVF,
						Ranges: []noderesourcesv1.VFRange{
							{PFIndex: 0, Start: ptr.To[int32](0), End: ptr.To[int32](3)},
						},
					},
					{
						Name: "dup_res",
						Type: noderesourcesv1.DevicePluginResourceTypeVF,
						Ranges: []noderesourcesv1.VFRange{
							{PFIndex: 1, Start: ptr.To[int32](0), End: ptr.To[int32](3)},
						},
					},
				},
			},
		}
		err := input.Client.Create(ctx, duplicateConfig)
		g.Expect(err).To(HaveOccurred(),
			"webhook should reject duplicate resource names")
		g.Expect(apierrors.IsForbidden(err) || apierrors.IsInvalid(err)).To(
			BeTrue(), fmt.Sprintf("expected Forbidden or Invalid, got: %v", err))
	}).WithTimeout(time.Minute).Should(Succeed())
}

// ValidateNodeSRIOVDevicePluginConfigValidCreate creates a valid
// NodeSRIOVDevicePluginConfig and verifies it is accepted.
func ValidateNodeSRIOVDevicePluginConfigValidCreate(ctx context.Context, input *SystemTestInput) {
	By("Creating a valid NodeSRIOVDevicePluginConfig")
	Eventually(func(g Gomega) {
		validConfig := &noderesourcesv1.NodeSRIOVDevicePluginConfig{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "e2e-valid-config-",
				Namespace:    dpfOperatorSystemNamespace,
				Labels:       CleanupScope.It,
			},
			Spec: noderesourcesv1.NodeSRIOVDevicePluginConfigSpec{
				DevicePluginResources: []noderesourcesv1.DevicePluginResource{
					{
						Name: "e2e_vf_res",
						Type: noderesourcesv1.DevicePluginResourceTypeVF,
						Ranges: []noderesourcesv1.VFRange{
							{PFIndex: 0, Start: ptr.To[int32](0), End: ptr.To[int32](3)},
						},
					},
				},
			},
		}
		g.Expect(input.Client.Create(ctx, validConfig)).To(Succeed())

		By("Verifying the NodeSRIOVDevicePluginConfig exists")
		got := &noderesourcesv1.NodeSRIOVDevicePluginConfig{}
		g.Expect(input.Client.Get(ctx, client.ObjectKeyFromObject(validConfig), got)).To(Succeed())
		g.Expect(got.Spec.DevicePluginResources).To(HaveLen(1))
	}).WithTimeout(time.Minute).Should(Succeed())
}

func ValidateNodeSRIOVDevicePluginManagement(ctx context.Context, input *SystemTestInput) {
	if !input.HasDpuNodes() {
		Skip("No DPUs in test config, skipping managed pod test")
	}

	const invalidDevicePluginImage = "invalid.repo/sriov-device-plugin:not-found"
	const invalidInitImage = "invalid.repo/nodesriov-init:not-found"

	var dpuName, kubeNodeName, serialNumber string
	Eventually(func(g Gomega) {
		dpuName, kubeNodeName, serialNumber = findTargetDPUAndNode(g, ctx, input.Client)
		g.Expect(dpuName).NotTo(BeEmpty(), "expected a DPU with HostNetworkReady=True")
		g.Expect(kubeNodeName).NotTo(BeEmpty())
		g.Expect(serialNumber).NotTo(BeEmpty())
	}).WithTimeout(240 * time.Second).WithPolling(5 * time.Second).Should(Succeed())
	By(fmt.Sprintf("Using DPU %s on node %s", dpuName, kubeNodeName))

	config1Name := "e2e-nodesriov-config1"
	config2Name := "e2e-nodesriov-config2"

	config1NoPrefixResource := "e2e_vf_no_prefix_1"
	config1ExplicitResource := "e2e_vf_explicit_1"
	config2NoPrefixResource := "e2e_vf_no_prefix_2"
	config2ExplicitResource := "e2e_vf_explicit_2"
	explicitPrefix := "e2e.explicit.io"
	alternateDefaultPrefix := "e2e.default.io"

	defaultControllerConfig := &operatorv1.NodeSRIOVDevicePluginControllerConfiguration{
		BaseComponentConfig: operatorv1.BaseComponentConfig{
			Disable: ptr.To(false),
		},
	}

	By("Ensuring NodeSRIOVDevicePluginController is enabled with default settings")
	removeDPUConfigAnnotation(ctx, input.Client, dpuName)
	patchDPFOperatorConfigAndWait(ctx, input.Client, defaultControllerConfig)

	By("Creating config1 and config2")
	config1 := buildNodeSRIOVConfigWithResources(config1Name, []noderesourcesv1.DevicePluginResource{
		{
			Name: config1NoPrefixResource,
			Type: noderesourcesv1.DevicePluginResourceTypeVF,
			Ranges: []noderesourcesv1.VFRange{
				{PFIndex: 0, Start: ptr.To[int32](0), End: ptr.To[int32](3)}, // 4 VFs
			},
		},
		{
			Name:           config1ExplicitResource,
			ResourcePrefix: ptr.To(explicitPrefix),
			Type:           noderesourcesv1.DevicePluginResourceTypeVF,
			Ranges: []noderesourcesv1.VFRange{
				{PFIndex: 0, Start: ptr.To[int32](4), End: ptr.To[int32](7)}, // 4 VFs
			},
		},
	})
	Expect(input.Client.Create(ctx, config1)).To(Succeed())

	config2 := buildNodeSRIOVConfigWithResources(config2Name, []noderesourcesv1.DevicePluginResource{
		{
			Name: config2NoPrefixResource,
			Type: noderesourcesv1.DevicePluginResourceTypeVF,
			Ranges: []noderesourcesv1.VFRange{
				{PFIndex: 0, Start: ptr.To[int32](0), End: ptr.To[int32](1)}, // 2 VFs
			},
		},
		{
			Name:           config2ExplicitResource,
			ResourcePrefix: ptr.To(explicitPrefix),
			Type:           noderesourcesv1.DevicePluginResourceTypeVF,
			Ranges: []noderesourcesv1.VFRange{
				{PFIndex: 0, Start: ptr.To[int32](2), End: ptr.To[int32](4)}, // 3 VFs
			},
		},
	})
	Expect(input.Client.Create(ctx, config2)).To(Succeed())

	By("Marking DPU with config1")
	setDPUConfigAnnotation(ctx, input.Client, dpuName, config1Name)

	By("Waiting for managed pod to start and validating config + resources (config1)")
	Eventually(func(g Gomega) {
		pod := getManagedPodForNode(ctx, g, input.Client, kubeNodeName)
		g.Expect(pod).NotTo(BeNil())
		expectPodRunning(g, pod)
		raw := pod.Annotations[nodesriovctrl.PodInputAnnotationKey]
		g.Expect(raw).To(ContainSubstring(serialNumber))
		g.Expect(raw).To(ContainSubstring(config1NoPrefixResource))
		g.Expect(raw).To(ContainSubstring(config1ExplicitResource))
	}).WithTimeout(240 * time.Second).Should(Succeed())
	waitForNodeResource(ctx, input.Client, kubeNodeName,
		fmt.Sprintf("%s/%s", nodesriovctrl.DefaultResourcePrefix, config1NoPrefixResource), 4)
	waitForNodeResource(ctx, input.Client, kubeNodeName,
		fmt.Sprintf("%s/%s", explicitPrefix, config1ExplicitResource), 4)

	By("Updating to fake images and waiting for managed pod to update")
	patchDPFOperatorConfigAndWait(ctx, input.Client, &operatorv1.NodeSRIOVDevicePluginControllerConfiguration{
		BaseComponentConfig: operatorv1.BaseComponentConfig{
			Disable: ptr.To(false),
		},
		DevicePlugin: &operatorv1.NodeSRIOVDevicePluginSettings{
			Image:     ptr.To(invalidDevicePluginImage),
			InitImage: ptr.To(invalidInitImage),
		},
	})
	Eventually(func(g Gomega) {
		pod := getManagedPodForNode(ctx, g, input.Client, kubeNodeName)
		g.Expect(pod).NotTo(BeNil())
		g.Expect(getContainerImageByName(pod.Spec.Containers, "sriov-device-plugin")).To(Equal(invalidDevicePluginImage))
		g.Expect(getContainerImageByName(pod.Spec.InitContainers, "dpf-device-plugin-init")).To(Equal(invalidInitImage))
	}).WithTimeout(240 * time.Second).Should(Succeed())

	By("Reverting fake images and waiting for managed pod to recover")
	patchDPFOperatorConfigAndWait(ctx, input.Client, defaultControllerConfig)
	Eventually(func(g Gomega) {
		pod := getManagedPodForNode(ctx, g, input.Client, kubeNodeName)
		g.Expect(pod).NotTo(BeNil())
		expectPodRunning(g, pod)
	}).WithTimeout(300 * time.Second).Should(Succeed())

	By("Updating default resource prefix and verifying non-explicit resources are updated")
	patchDPFOperatorConfigAndWait(ctx, input.Client, &operatorv1.NodeSRIOVDevicePluginControllerConfiguration{
		BaseComponentConfig: operatorv1.BaseComponentConfig{
			Disable: ptr.To(false),
		},
		DevicePlugin: &operatorv1.NodeSRIOVDevicePluginSettings{
			DefaultResourcePrefix: ptr.To(alternateDefaultPrefix),
		},
	})
	Eventually(func(g Gomega) {
		node := &corev1.Node{}
		g.Expect(input.Client.Get(ctx, types.NamespacedName{Name: kubeNodeName}, node)).To(Succeed())

		newKey := fmt.Sprintf("%s/%s", alternateDefaultPrefix, config1NoPrefixResource)
		oldKey := fmt.Sprintf("%s/%s", nodesriovctrl.DefaultResourcePrefix, config1NoPrefixResource)

		newQty := node.Status.Allocatable[corev1.ResourceName(newKey)]
		g.Expect(newQty.Value()).To(Equal(int64(4)))

		oldQty := node.Status.Allocatable[corev1.ResourceName(oldKey)]
		g.Expect(oldQty.Value()).To(Equal(int64(0)))

		explicitKey := fmt.Sprintf("%s/%s", explicitPrefix, config1ExplicitResource)
		explicitQty, explicitExists := node.Status.Allocatable[corev1.ResourceName(explicitKey)]
		g.Expect(explicitExists).To(BeTrue())
		g.Expect(explicitQty.Value()).To(Equal(int64(4)))
	}).WithTimeout(300 * time.Second).Should(Succeed())

	By("Reverting default prefix and switching DPU to config2")
	patchDPFOperatorConfigAndWait(ctx, input.Client, defaultControllerConfig)
	setDPUConfigAnnotation(ctx, input.Client, dpuName, config2Name)

	By("Validating managed pod started and node exposes correct resources (config2)")
	Eventually(func(g Gomega) {
		pod := getManagedPodForNode(ctx, g, input.Client, kubeNodeName)
		g.Expect(pod).NotTo(BeNil())
		expectPodRunning(g, pod)
		raw := pod.Annotations[nodesriovctrl.PodInputAnnotationKey]
		g.Expect(raw).To(ContainSubstring(serialNumber))
		g.Expect(raw).To(ContainSubstring(config2NoPrefixResource))
		g.Expect(raw).To(ContainSubstring(config2ExplicitResource))
	}).WithTimeout(300 * time.Second).Should(Succeed())
	waitForNodeResource(ctx, input.Client, kubeNodeName,
		fmt.Sprintf("%s/%s", nodesriovctrl.DefaultResourcePrefix, config2NoPrefixResource), 2)
	waitForNodeResource(ctx, input.Client, kubeNodeName,
		fmt.Sprintf("%s/%s", explicitPrefix, config2ExplicitResource), 3)
}

func findTargetDPUAndNode(g Gomega, ctx context.Context, c client.Client) (dpuName, kubeNodeName, serialNumber string) {
	dpuList := &provisioningv1.DPUList{}
	g.Expect(c.List(ctx, dpuList, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
	for i := range dpuList.Items {
		dpu := &dpuList.Items[i]
		if dpu.Spec.SerialNumber == "" || !dpu.DeletionTimestamp.IsZero() {
			continue
		}
		if !isDPUHostNetworkReady(dpu) {
			continue
		}
		dpuNode := &provisioningv1.DPUNode{}
		if err := c.Get(ctx, types.NamespacedName{
			Namespace: dpfOperatorSystemNamespace,
			Name:      dpu.Spec.DPUNodeName,
		}, dpuNode); err != nil {
			continue
		}
		if dpuNode.Status.KubeNodeRef == nil || *dpuNode.Status.KubeNodeRef == "" {
			continue
		}
		return dpu.Name, *dpuNode.Status.KubeNodeRef, dpu.Spec.SerialNumber
	}
	return "", "", ""
}

// Check if the DPU is host network ready.
func isDPUHostNetworkReady(dpu *provisioningv1.DPU) bool {
	for _, cond := range dpu.Status.Conditions {
		if cond.Type == string(provisioningv1.DPUCondHostNetworkReady) &&
			cond.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

// Get the managed pod for the given node.
func getManagedPodForNode(ctx context.Context, g Gomega, c client.Client, nodeName string) *corev1.Pod {
	podList := &corev1.PodList{}
	g.Expect(c.List(ctx, podList,
		client.InNamespace(dpfOperatorSystemNamespace),
		client.MatchingLabels{nodesriovctrl.ManagedByLabelKey: nodesriovctrl.ManagedByLabelValue},
	)).To(Succeed())
	for i := range podList.Items {
		pod := &podList.Items[i]
		if pod.Spec.Affinity == nil ||
			pod.Spec.Affinity.NodeAffinity == nil ||
			pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
			continue
		}
		for _, term := range pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
			for _, field := range term.MatchFields {
				if field.Key == metav1.ObjectNameField &&
					len(field.Values) > 0 &&
					field.Values[0] == nodeName {
					return pod
				}
			}
		}
	}
	return nil
}

// Set the DPU config annotation on the given DPU.
func setDPUConfigAnnotation(ctx context.Context, c client.Client, dpuName string, configName string) {
	Eventually(func(g Gomega) {
		dpu := &provisioningv1.DPU{}
		g.Expect(c.Get(ctx, types.NamespacedName{
			Namespace: dpfOperatorSystemNamespace,
			Name:      dpuName,
		}, dpu)).To(Succeed())
		original := dpu.DeepCopy()
		if dpu.Annotations == nil {
			dpu.Annotations = map[string]string{}
		}
		dpu.Annotations[nodesriovctrl.DPUDevicePluginConfigAnnotationKey] = configName
		g.Expect(c.Patch(ctx, dpu, client.MergeFrom(original))).To(Succeed())
	}).WithTimeout(60 * time.Second).Should(Succeed())
}

// Expect the pod to be running and have the expected annotations.
func expectPodRunning(g Gomega, pod *corev1.Pod) {
	g.Expect(pod.Status.Phase).To(Equal(corev1.PodRunning))
	g.Expect(pod.Status.Conditions).To(ContainElement(
		And(
			HaveField("Type", Equal(corev1.PodReady)),
			HaveField("Status", Equal(corev1.ConditionTrue)),
		),
	))
	g.Expect(pod.Annotations[nodesriovctrl.PodObjectHashAnnotationKey]).NotTo(BeEmpty())
	g.Expect(pod.Annotations[nodesriovctrl.PodInputAnnotationKey]).NotTo(BeEmpty())
}

// Wait for the given node resource to be present and have the expected count.
func waitForNodeResource(ctx context.Context, c client.Client, nodeName string, resourceKey string, expectedCount int64) {
	Eventually(func(g Gomega) {
		node := &corev1.Node{}
		g.Expect(c.Get(ctx, types.NamespacedName{Name: nodeName}, node)).To(Succeed())
		quantity, exists := node.Status.Allocatable[corev1.ResourceName(resourceKey)]
		g.Expect(exists).To(BeTrue())
		g.Expect(quantity.Value()).To(Equal(expectedCount))
	}).WithTimeout(240 * time.Second).Should(Succeed())
}

// Patch the DPFOperatorConfig with the given NodeSRIOVDevicePluginControllerConfiguration and wait for the deployment to be ready.
func patchDPFOperatorConfigAndWait(ctx context.Context, c client.Client, config *operatorv1.NodeSRIOVDevicePluginControllerConfiguration) {
	Eventually(func(g Gomega) {
		cfg := &operatorv1.DPFOperatorConfig{}
		g.Expect(c.Get(ctx, client.ObjectKey{
			Namespace: dpfOperatorSystemNamespace,
			Name:      configName,
		}, cfg)).To(Succeed())
		original := cfg.DeepCopy()
		cfg.Spec.NodeSRIOVDevicePluginController = config.DeepCopy()
		g.Expect(c.Patch(ctx, cfg, client.MergeFrom(original))).To(Succeed())
	}).WithTimeout(60 * time.Second).Should(Succeed())

	Eventually(func(g Gomega) {
		cfg := &operatorv1.DPFOperatorConfig{}
		g.Expect(c.Get(ctx, client.ObjectKey{
			Namespace: dpfOperatorSystemNamespace,
			Name:      configName,
		}, cfg)).To(Succeed())
		g.Expect(cfg.Status.ObservedGeneration).To(Equal(cfg.GetGeneration()))
		g.Expect(conditions.IsTrue(cfg, conditions.TypeReady)).To(BeTrue())
	}).WithTimeout(120 * time.Second).WithPolling(1 * time.Second).Should(Succeed())

	Eventually(func(g Gomega) {
		deployment := &appsv1.Deployment{}
		g.Expect(c.Get(ctx, client.ObjectKey{
			Namespace: dpfOperatorSystemNamespace,
			Name:      "dpf-nodesriovdeviceplugin-controller",
		}, deployment)).To(Succeed())
		g.Expect(deployment.Spec.Replicas).NotTo(BeNil())
		g.Expect(deployment.Status.ObservedGeneration).To(Equal(deployment.GetGeneration()))
		g.Expect(deployment.Status.Replicas).NotTo(BeZero())
		g.Expect(deployment.Status.ReadyReplicas).To(Equal(*deployment.Spec.Replicas))
	}).WithTimeout(240 * time.Second).WithPolling(1 * time.Second).Should(Succeed())
}

// Build the NodeSRIOVDevicePluginConfig with the given name and resources.
func buildNodeSRIOVConfigWithResources(
	name string,
	resources []noderesourcesv1.DevicePluginResource,
) *noderesourcesv1.NodeSRIOVDevicePluginConfig {
	return &noderesourcesv1.NodeSRIOVDevicePluginConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: dpfOperatorSystemNamespace,
			Labels:    CleanupScope.It,
		},
		Spec: noderesourcesv1.NodeSRIOVDevicePluginConfigSpec{
			DevicePluginResources: resources,
		},
	}
}

// Remove the DPU config annotation from the given DPU.
func removeDPUConfigAnnotation(ctx context.Context, c client.Client, dpuName string) {
	Eventually(func(g Gomega) {
		dpu := &provisioningv1.DPU{}
		g.Expect(c.Get(ctx, types.NamespacedName{
			Namespace: dpfOperatorSystemNamespace,
			Name:      dpuName,
		}, dpu)).To(Succeed())
		original := dpu.DeepCopy()
		if dpu.Annotations != nil {
			delete(dpu.Annotations, nodesriovctrl.DPUDevicePluginConfigAnnotationKey)
		}
		g.Expect(c.Patch(ctx, dpu, client.MergeFrom(original))).To(Succeed())
	}).WithTimeout(60 * time.Second).Should(Succeed())
}

// Get the container image by the container name from the given containers.
func getContainerImageByName(containers []corev1.Container, name string) string {
	for _, c := range containers {
		if c.Name == name {
			return c.Image
		}
	}
	return ""
}
