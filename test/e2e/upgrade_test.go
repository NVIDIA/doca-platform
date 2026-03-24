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
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	operatorutils "github.com/nvidia/doca-platform/internal/operator/utils"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/pkg/conditions"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//nolint:dupl
var _ = Describe("DPF Upgrade tests", Labels{Domain.DPFUpgrade}, func() {
	Context("Should pass", Labels{Domain.RequiresNodes}, Serial, Ordered, func() {
		It("create DPFOperatorConfig", func() {
			SystemSetupBeforeSuite()
			By("Pre provisioning DPU cluster setup")
			provInput := getProvisionDPUClustersInput()
			ProvisionDPUClusters(ctx, provInput)
			ProvisionBFBAndDPUFlavor(ctx, provInput)
		})

		It("create DPUDeployments dependencies", func() {
			dpuServiceTemplate := generateDPUServiceTemplate(input, "")
			useDummyDPUServiceChart(dpuServiceTemplate)
			Expect(input.client.Create(ctx, dpuServiceTemplate)).To(Succeed())

			dpuServiceConfiguration := generateServiceConfiguration(input, "")
			Expect(input.client.Create(ctx, dpuServiceConfiguration)).To(Succeed())

			dpuServiceTemplate2 := generateDPUServiceTemplate(input, "2")
			useDummyDPUServiceChart(dpuServiceTemplate2)
			Expect(input.client.Create(ctx, dpuServiceTemplate2)).To(Succeed())

			dpuServiceConfiguration2 := generateServiceConfiguration(input, "2")
			dpuServiceConfiguration2.Spec.Interfaces = []dpuservicev1.ServiceInterfaceTemplate{{Name: "net2", Network: "mybrsfc"}}
			Expect(input.client.Create(ctx, dpuServiceConfiguration2)).To(Succeed())

			dpuServiceIPAM := input.ipPoolDPUServiceIPAM.DeepCopy()
			dpuServiceIPAM.SetLabels(CleanupScope.Suite)
			dpuServiceIPAM.SetName("dpudeployment-ipam-pool1")
			dpuServiceIPAM.SetNamespace(dpfOperatorSystemNamespace)
			// Remove selectors so it applies to all nodes
			dpuServiceIPAM.Spec.NodeSelector = nil
			Expect(input.client.Create(ctx, dpuServiceIPAM)).To(Succeed())
		})

		It("create DPUDeployment objects", func() {
			By("Get worker nodes")
			nodes := &corev1.NodeList{}
			Expect(input.client.List(ctx, nodes, client.MatchingLabels{"node-role.kubernetes.io/worker": ""})).To(Succeed())

			By("Creating the DPUDeployment objects")
			for i := 0; i < input.numberOfDPUNodes; i++ {
				node := &nodes.Items[i]
				dpuDeployment := generateDPUDeployment(input, "")
				dpuDeployment.SetName(node.GetName())
				// Intentionally using deprecated field, e2e tests will be updated once we have removed the deprecated field. Unit
				// tests cover the new field, e2e tests cover the old field since there is no more unit test coverage for the deprecated field.
				// This particular test must use the deprecated field since it's using the old CRDs on DPUDeployment creation.
				//nolint:staticcheck
				dpuDeployment.Spec.DPUs.DPUSets[0].NodeSelector = &metav1.LabelSelector{
					MatchLabels: map[string]string{"kubernetes.io/hostname": node.GetName()},
				}
				dpuDeployment.Spec.Services["example-2"] = dpuservicev1.DPUDeploymentServiceConfiguration{
					ServiceTemplate:      "example-2",
					ServiceConfiguration: "example-2",
				}
				dpuDeployment.Spec.ServiceChains.Switches[0] = dpuservicev1.DPUDeploymentSwitch{
					Ports: []dpuservicev1.DPUDeploymentPort{
						{
							Service: &dpuservicev1.DPUDeploymentService{
								InterfaceName: "net1",
								Name:          "example",
								IPAM: &dpuservicev1.IPAM{
									MatchLabels: map[string]string{
										"svc.dpu.nvidia.com/pool": "pool1",
									},
								},
							},
						},
						{
							Service: &dpuservicev1.DPUDeploymentService{
								InterfaceName: "net2",
								Name:          "example-2",
								IPAM: &dpuservicev1.IPAM{
									MatchLabels: map[string]string{
										"svc.dpu.nvidia.com/pool": "pool1",
									},
								},
							},
						},
					},
				}
				Expect(input.client.Create(ctx, dpuDeployment)).To(Succeed())
			}
		})

		It("get DPUCluster client", func() {
			By("Creating a client for the DPUCluster")
			getDPUClusterClients(ctx, getProvisionDPUClustersInput())
		})

		It("wait for DPUs to be provisioned", func() {
			By("Waiting for provisioning")
			VerifyDPUClusterWithNodes(ctx, getProvisionDPUClustersInput())
			By("Waiting for system components to be ready")
			verifySystemReady()
		})

		It("capture DPU and DPUService generations before upgrade", func() {
			collectGenerations("upgrade-test-generations-before")
		})
	})
})

var _ = Describe("DPF Upgrade validation", Labels{Domain.DPFUpgradeValidation}, func() {
	Context("Should pass", Labels{Domain.RequiresNodes}, Serial, Ordered, func() {
		It("validate rollout is done and pre-upgrade validation successful", func() {
			By("Validating pre-upgrade conditions")
			validatePreUpgradeConditions(ctx, input)
		})

		It("get DPUCluster client", func() {
			By("Creating a client for the DPUCluster")
			getDPUClusterClients(ctx, getProvisionDPUClustersInput())
		})

		It("validate DPUCluster", func() {
			By("Validating DPUCluster")
			VerifyDPUClusterWithNodes(ctx, getProvisionDPUClustersInput())
			By("Waiting for system components to be ready")
			verifySystemReady()
		})

		It("validate the DPF version is upgraded", func() {
			By("Validating the DPF version is upgraded")
			validateDPFVersionUpgrade()
		})

		It("validate that DMS Pods are upgraded", func() {
			By("Validating that DMS Pods have the correct same image tag")
			VerifyHostAgentPodsImageTag(ctx, input, tag)
		})

		It("waiting for controllers to reconcile", func() {
			const reconciliationWait = 30 * time.Second
			By(fmt.Sprintf("Waiting %s for controllers to reconcile", reconciliationWait))
			time.Sleep(reconciliationWait)
		})

		It("validate DPU and DPUService generations after upgrade", func() {
			validateGenerationsAfterUpgrade()
		})

		It("perform DPU and DPUService rollout test", func() {
			By("Performing DPU and DPUService rollout test")
			rolloutDPU(ctx, input)
			rolloutDPUService(ctx, input)
			By("Waiting for provisioning")
			VerifyDPUClusterWithNodes(ctx, getProvisionDPUClustersInput())
			By("Waiting for system components to be ready")
			verifySystemReady()
		})
	})
})

func VerifyHostAgentPodsImageTag(ctx context.Context, input *systemTestInput, expectedTag string) {
	By("Verifying HostAgent Pods have the same image tag as the operator")
	Eventually(func(g Gomega) {
		dmsPods := &corev1.PodList{}
		g.Expect(input.client.List(ctx, dmsPods,
			client.InNamespace(dpfOperatorSystemNamespace),
			client.MatchingLabels{util.ProvisioningComponentLabelKey: "hostagent"},
		)).To(Succeed())

		for _, pod := range dmsPods.Items {
			g.Expect(pod.Spec.Containers).ToNot(BeEmpty(), "DMS Pod should have containers")
			for _, container := range pod.Spec.Containers {
				g.Expect(container.Image).To(ContainSubstring(expectedTag),
					fmt.Sprintf("DMS Pod %s should have image tag %s", pod.Name, expectedTag))
			}
		}
	}).WithTimeout(3*time.Minute).WithPolling(1*time.Second).Should(Succeed(),
		"DMS Pods should have the same image tag as the operator")
}

func validatePreUpgradeConditions(ctx context.Context, input *systemTestInput) {
	By("Validating pre-upgrade conditions of dpfoperatorconfig with stability verification")

	// Helper function to check if condition is ready
	checkConditionReady := func(g Gomega) {
		dpfOperatorConfig := &operatorv1.DPFOperatorConfig{}
		g.Expect(input.client.Get(ctx, client.ObjectKey{Name: configName, Namespace: dpfOperatorSystemNamespace}, dpfOperatorConfig)).To(Succeed())
		g.Expect(dpfOperatorConfig.Status.ObservedGeneration).To(Equal(dpfOperatorConfig.GetGeneration()))
		g.Expect(dpfOperatorConfig.Status.Conditions).NotTo(BeEmpty())
		g.Expect(conditions.IsTrue(dpfOperatorConfig, operatorv1.PreUpgradeValidationReadyCondition)).To(BeTrue())
	}

	// Function to check both readiness and stability
	checkReadinessAndStability := func(g Gomega) {
		// Step 1: Wait for condition to become True
		g.Eventually(checkConditionReady, 10*time.Second, 1*time.Second).Should(Succeed())

		// Step 2: Verify stability - if this fails, the outer Eventually will retry
		g.Consistently(checkConditionReady, 10*time.Second, 1*time.Second).Should(Succeed())
	}

	// Main retry loop: check readiness, then stability, retry if either fails
	By("Waiting for PreUpgradeValidationReady condition to become True and stable")
	Eventually(checkReadinessAndStability, 10*time.Minute, 20*time.Second).Should(Succeed(),
		"PreUpgradeValidationReady condition should be ready and stable")
}

func validateGenerationsAfterUpgrade() {
	collectGenerations("upgrade-test-generations-after")
	allGenerationsBefore := getGenerationsFromConfigMap("upgrade-test-generations-before")
	allGenerationsAfter := getGenerationsFromConfigMap("upgrade-test-generations-after")

	By("Comparing generations before and after upgrade")
	Expect(allGenerationsAfter).To(HaveLen(len(allGenerationsBefore)),
		"Number of objects should remain the same after upgrade")

	sort.Slice(allGenerationsBefore, func(i, j int) bool {
		return fmt.Sprintf("%v", allGenerationsBefore[i]) <
			fmt.Sprintf("%v", allGenerationsBefore[j])
	})
	sort.Slice(allGenerationsAfter, func(i, j int) bool {
		return fmt.Sprintf("%v", allGenerationsAfter[i]) <
			fmt.Sprintf("%v", allGenerationsAfter[j])
	})
	Expect(allGenerationsAfter).To(BeComparableTo(allGenerationsBefore),
		"Generation data (ignoring order) should remain identical after upgrade")
}

func collectGenerations(configMapName string) {
	allGenerations := make([]map[string]interface{}, 0)

	By("Capturing DPU generations")
	dpuList := &provisioningv1.DPUList{}
	Expect(input.client.List(ctx, dpuList, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
	allGenerations = append(allGenerations, extractGenerationInfo(ToClientObjectSlice(dpuList.Items))...)

	By("Capturing DPUService generations with owned-by-dpudeployment label")
	dpuServiceList := &dpuservicev1.DPUServiceList{}
	Expect(input.client.List(ctx, dpuServiceList,
		client.InNamespace(dpfOperatorSystemNamespace),
		client.HasLabels{dpuservicev1.ParentDPUDeploymentNameLabel})).To(Succeed())
	allGenerations = append(allGenerations, extractGenerationInfo(ToClientObjectSlice(dpuServiceList.Items))...)

	genData, err := json.MarshalIndent(allGenerations, "", "  ")
	Expect(err).ToNot(HaveOccurred())

	By("Storing generations in ConfigMap")
	configMap := &corev1.ConfigMap{}
	configMap.SetName(configMapName)
	configMap.SetNamespace(dpfOperatorSystemNamespace)
	configMap.SetLabels(CleanupScope.Suite)
	configMap.Data = map[string]string{
		"generations.json": string(genData),
	}
	Expect(input.client.Create(ctx, configMap)).To(Succeed())
}

func getGenerationsFromConfigMap(configMapName string) []map[string]interface{} {
	By("Reading generations before upgrade from ConfigMap")
	configMapBefore := &corev1.ConfigMap{}
	Expect(input.client.Get(ctx, client.ObjectKey{
		Name:      configMapName,
		Namespace: dpfOperatorSystemNamespace,
	}, configMapBefore)).To(Succeed())

	genData := configMapBefore.Data["generations.json"]
	var allGenerations []map[string]interface{}
	Expect(json.Unmarshal([]byte(genData), &allGenerations)).To(Succeed())
	return allGenerations
}

// ToClientObjectSlice converts a slice of concrete Kubernetes objects to []client.Object
// T is the value type (e.g., DPU), but *T must implement client.Object
func ToClientObjectSlice[T any](in []T) []client.Object {
	out := make([]client.Object, len(in))
	for i := range in {
		out[i] = any(&in[i]).(client.Object)
	}
	return out
}

// extractGenerationInfo extracts generation info from Kubernetes objects
func extractGenerationInfo(objects []client.Object) []map[string]interface{} {
	generations := []map[string]interface{}{}

	for _, obj := range objects {
		// Extract type name from the struct type
		typeName := reflect.TypeOf(obj).Elem().Name()

		generations = append(generations, map[string]interface{}{
			"type":       typeName,
			"name":       obj.GetName(),
			"namespace":  obj.GetNamespace(),
			"generation": obj.GetGeneration(),
		})
	}

	return generations
}

// verifySystemReady checks if the DPF system components are ready.
// This is not a complete list of all system pods, but it includes the most important ones.
func verifySystemReady() {
	VerifyClusterPods(ctx, dpuClusterClient[0], []string{
		// Kubernetes system pods
		"kube-flannel-ds", "coredns", "kube-proxy",
		// DPF system components
		"nvidia-k8s-ipam", "sfc-controller",
		// DPUDeployment pods
		"example",
	})

	dpfOperatorConfig := &operatorv1.DPFOperatorConfig{}
	objectKey := client.ObjectKey{Name: configName, Namespace: dpfOperatorSystemNamespace}
	Expect(input.client.Get(ctx, objectKey, dpfOperatorConfig)).To(Succeed())
	Expect(dpfOperatorConfig.Status.Version).NotTo(BeNil())
	isCurrentVersionLastReleasedGA := operatorutils.IsUpgradeFromLastReleasedGA(*dpfOperatorConfig.Status.Version)

	dpuServiceNames := []string{
		"flannel", "multus", "sriov-device-plugin", "ovs-cni", "sfc-controller", "servicechainset-rbac-and-crds",
		"cni-installer",
	}
	// TODO: Figure out a more clear way to do that and adjust the relevant fields when we bump to the next release
	if isCurrentVersionLastReleasedGA {
		// Previous release: single nvidia-k8s-ipam service and single servicechainset-controller service.
		dpuServiceNames = append(dpuServiceNames, "nvidia-k8s-ipam", "servicechainset-controller")
	} else {
		// Current version: nvidia-k8s-ipam is split into a per-cluster controller service and a node service,
		// servicechainset-controller is split into a per-cluster controller service, and
		// kube-state-metrics is split into a per-cluster controller service and an RBAC service.
		perClusterNVIPAMControllerServiceName := getPerClusterDPUServiceName("nvidia-k8s-ipam", input.dpuClusters[0].Name, input.dpuClusters[0].Namespace)
		perClusterServiceChainSetControllerServiceName := getPerClusterDPUServiceName("servicechainset-controller", input.dpuClusters[0].Name, input.dpuClusters[0].Namespace)
		perClusterKubeStateMetricsServiceName := getPerClusterDPUServiceName("kube-state-metrics", input.dpuClusters[0].Name, input.dpuClusters[0].Namespace)
		dpuServiceNames = append(dpuServiceNames,
			perClusterNVIPAMControllerServiceName, "nvidia-k8s-ipam-node",
			perClusterServiceChainSetControllerServiceName,
			perClusterKubeStateMetricsServiceName, "kube-state-metrics-rbac",
		)
	}
	verifyDPUServicesReady(ctx, input, dpfOperatorSystemNamespace, dpuServiceNames)
}

func validateDPFVersionUpgrade() {
	Eventually(func(g Gomega) {
		dpfOperatorConfig := &operatorv1.DPFOperatorConfig{}
		g.Expect(input.client.Get(ctx, client.ObjectKey{
			Name:      configName,
			Namespace: dpfOperatorSystemNamespace,
		}, dpfOperatorConfig)).To(Succeed())
		g.Expect(dpfOperatorConfig.Status.Version).NotTo(BeNil(),
			"DPFOperatorConfig.Status.Version must be set before comparing")
		g.Expect(*dpfOperatorConfig.Status.Version).To(Equal(tag))

	}).WithTimeout(1*time.Minute).WithPolling(1*time.Second).Should(Succeed(),
		"DPF version should be upgraded to the specified tag")
}

func rolloutDPU(ctx context.Context, input *systemTestInput) {
	By("Selecting one DPU for deletion")
	dpuList := &provisioningv1.DPUList{}
	Expect(input.client.List(ctx, dpuList, client.InNamespace(dpfOperatorSystemNamespace))).To(Succeed())
	Expect(dpuList.Items).NotTo(BeEmpty(), "No DPUs found for rollout test")
	selectedDPU := &dpuList.Items[0]
	uuid := selectedDPU.GetUID()
	By(fmt.Sprintf("Selected DPU: %s", selectedDPU.GetName()))
	dpuDeviceNameLabel := selectedDPU.GetLabels()[util.DPUDeviceNameLabel]
	Expect(dpuDeviceNameLabel).ToNot(BeEmpty(), "DPU should have a device name label")

	By("Deleting selected DPU")
	Expect(client.IgnoreNotFound(input.client.Delete(ctx, selectedDPU))).To(Succeed())

	By("Waiting for DPU to be recreated")
	Eventually(func(g Gomega) {
		updatedDPUs := &provisioningv1.DPUList{}
		g.Expect(input.client.List(ctx, updatedDPUs,
			client.InNamespace(dpfOperatorSystemNamespace),
			client.MatchingLabels{
				util.DPUDeviceNameLabel: dpuDeviceNameLabel,
			})).To(Succeed())
		g.Expect(updatedDPUs.Items).To(HaveLen(1), "DPU should be recreated")
		g.Expect(updatedDPUs.Items[0].GetUID()).ToNot(Equal(uuid), "DPU should be recreated with a new UID")
		dpuVersion := updatedDPUs.Items[0].Status.DPFVersion
		g.Expect(dpuVersion).ToNot(BeNil())
		g.Expect(*dpuVersion).To(Equal(tag), "DPU should be recreated with the new DPF version")
	}).WithTimeout(20 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
}

func rolloutDPUService(ctx context.Context, input *systemTestInput) {
	By("Selecting system component DPUService flannel for deletion")
	selectedDPUService := &dpuservicev1.DPUService{}
	selectedDPUService.SetName(operatorv1.FlannelName.String())
	selectedDPUService.SetNamespace(dpfOperatorSystemNamespace)
	Expect(input.client.Get(ctx, client.ObjectKeyFromObject(selectedDPUService), selectedDPUService)).To(Succeed())
	uuid := selectedDPUService.GetUID()

	By("Deleting system component DPUService flannel")
	Expect(input.client.Delete(ctx, selectedDPUService)).To(Succeed())

	By("Waiting for DPUService to be recreated")
	Eventually(func(g Gomega) {
		updatedDPUService := &dpuservicev1.DPUService{}
		g.Expect(input.client.Get(ctx, client.ObjectKeyFromObject(selectedDPUService), updatedDPUService)).To(Succeed())
		g.Expect(updatedDPUService.GetUID()).ToNot(Equal(uuid), "DPUService should be recreated with a new UID")
	}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
}
