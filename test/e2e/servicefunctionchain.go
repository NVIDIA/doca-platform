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
	"strings"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/test/utils/dpuservice"
	"github.com/nvidia/doca-platform/test/utils/netshoot"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func VerifyPlainServiceFunctionChain(ctx context.Context, input *systemTestInput) {
	if !input.hasDpuNodes() {
		Skip("Skip test as there are not multiple nodes")
	}

	hostNamespace := "sfc-plain-test-ns"
	createTestNamespace(ctx, input.client, hostNamespace)

	vfIndex := 5
	setupPlainChainTest(ctx, input, vfIndex)

	By("creating test pods")
	pod1Config, pod2Config := getPlainChainTestPodConfigs(ctx, input, hostNamespace, vfIndex)
	netshoot.CreateNadsFromConfig(ctx, input.client, []*netshoot.TestPodConfig{&pod1Config, &pod2Config})
	netshoot.CreateAndWaitForPods(ctx, input.client, []*netshoot.TestPodConfig{&pod1Config, &pod2Config})

	By("running traffic test between pods")
	netshoot.RunTrafficTest(&hostClusterRESTClient, &input.restConfig, hostNamespace, pod1Config.Name, pod2Config.Name, pod2Config.IP)
}

func VerifyHBNOnlyServiceFunctionChain(ctx context.Context, input *systemTestInput) {
	if input.numberOfDPUNodes != 2 {
		// Test assumes that there are exactly 2 host nodes to match the DPU cluster
		Skip("Skip test as there are not exactly 2 nodes")
	}

	hostNamespace := "sfc-hbn-test-ns"
	createTestNamespace(ctx, input.client, hostNamespace)

	vfIndex := 3
	setupHBNOnlyTest(ctx, input, vfIndex)

	By("creating test pods")
	pod1Config, pod2Config := getHBNOnlyTestPodConfigs(ctx, input, hostNamespace, vfIndex)
	netshoot.CreateNadsFromConfig(ctx, input.client, []*netshoot.TestPodConfig{&pod1Config, &pod2Config})
	netshoot.CreateAndWaitForPods(ctx, input.client, []*netshoot.TestPodConfig{&pod1Config, &pod2Config})

	By("running traffic test between pods")
	netshoot.RunTrafficTest(&hostClusterRESTClient, &input.restConfig, hostNamespace, pod1Config.Name, pod2Config.Name, pod2Config.IP)
}

func VerifyHBNOnlyBadFlowRecovery(ctx context.Context, input *systemTestInput) {
	if input.numberOfDPUNodes != 2 {
		// Test assumes that there are exactly 2 host nodes to match the DPU cluster
		Skip("Skip test as there are not exactly 2 nodes")
	}

	hostNamespace := "sfc-hbn-recovery-test-ns"
	createTestNamespace(ctx, input.client, hostNamespace)

	vfIndex := 3
	setupHBNOnlyTest(ctx, input, vfIndex)

	By("creating test pods")
	pod1Config, pod2Config := getHBNOnlyTestPodConfigs(ctx, input, hostNamespace, vfIndex)
	netshoot.CreateNadsFromConfig(ctx, input.client, []*netshoot.TestPodConfig{&pod1Config, &pod2Config})
	netshoot.CreateAndWaitForPods(ctx, input.client, []*netshoot.TestPodConfig{&pod1Config, &pod2Config})

	By("running initial traffic test")
	netshoot.RunTrafficTest(&hostClusterRESTClient, &input.restConfig, hostNamespace, pod1Config.Name, pod2Config.Name, pod2Config.IP)

	By("killing hbn pod to test recovery")
	deleteFirstFoundPodOnDpuCluser(ctx, "doca-hbn", input.namespace)

	By("waiting for hbn service to recover")
	dpuservice.WaitForDPUServices(ctx, input.client, input.namespace, []string{"doca-hbn"})

	By("running traffic test after recovery")
	netshoot.RunTrafficTest(&hostClusterRESTClient, &input.restConfig, hostNamespace, pod1Config.Name, pod2Config.Name, pod2Config.IP)
}

func getPlainChainTestPodConfigs(ctx context.Context, input *systemTestInput, namespace string, vfIndex int) (netshoot.TestPodConfig, netshoot.TestPodConfig) {
	workerNode1, workerNode2 := getTwoWorkerNodeNames(ctx, input.client)

	pod1Config := netshoot.TestPodConfig{
		Name:      "pod1",
		Namespace: namespace,
		IP:        "10.1.121.1",
		NodeName:  workerNode1,
		VFIndex:   vfIndex,
		CIDR:      "24",
	}
	pod2Config := netshoot.TestPodConfig{
		Name:      "pod2",
		Namespace: namespace,
		IP:        "10.1.121.2",
		NodeName:  workerNode2,
		VFIndex:   vfIndex,
		CIDR:      "24",
	}

	return pod1Config, pod2Config
}

func getHBNOnlyTestPodConfigs(ctx context.Context, input *systemTestInput, namespace string, vfIndex int) (netshoot.TestPodConfig, netshoot.TestPodConfig) {
	workerNode1, workerNode2 := getTwoWorkerNodeNames(ctx, input.client)

	pod1Config := netshoot.TestPodConfig{
		Name:      "pod1",
		Namespace: namespace,
		IP:        "10.0.121.1",
		DST:       "10.0.121.8",
		GW:        "10.0.121.2",
		NodeName:  workerNode1,
		VFIndex:   vfIndex,
		CIDR:      "29",
	}
	pod2Config := netshoot.TestPodConfig{
		Name:      "pod2",
		Namespace: namespace,
		IP:        "10.0.121.9",
		DST:       "10.0.121.0",
		GW:        "10.0.121.10",
		NodeName:  workerNode2,
		VFIndex:   vfIndex,
		CIDR:      "29",
	}

	return pod1Config, pod2Config
}

// setupPlainChainTest creates a test environment for a plain service function chain
func setupPlainChainTest(ctx context.Context, input *systemTestInput, vfIndex int) {
	interfaceConfigs := []dpuservice.TestDPUServiceInterfaceConfig{
		{
			Name:          "p0",
			Type:          "physical",
			Namespace:     input.namespace,
			InterfaceName: "p0",
			Labels: map[string]string{
				"uplink": "p0",
			},
			Annotations: map[string]string{
				"svc.dpu.nvidia.com/noop-physical-removal": "",
			},
		},
		{
			Name:          fmt.Sprintf("pf0vf%d", vfIndex),
			Type:          "vf",
			Namespace:     input.namespace,
			InterfaceName: fmt.Sprintf("pf0vf%d", vfIndex),
			PFIndex:       0,
			VFIndex:       vfIndex,
			Labels: map[string]string{
				"vf": fmt.Sprintf("pf0vf%d", vfIndex),
			},
		},
	}

	By("wait for prerequisite services")
	dpuservice.WaitForDPUServices(ctx, input.client, input.namespace, []string{"sfc-controller"})

	By("create and wait for dpu service interfaces")
	createAndWaitForInterfaces(ctx, input.client, input.dpuServiceInterfaceTemplate, interfaceConfigs)

	By("create plain dpu service chain")
	dpuServiceChain := generateDPUObj("netshoot-to-p0", input.namespace, input.dpuServiceChainTemplate.DeepCopy())
	dpuServiceChain.Spec.Template.Spec.NodeSelector = &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      "kubernetes.io/os",
				Operator: metav1.LabelSelectorOpIn,
				Values:   []string{"linux"},
			},
		},
	}
	Expect(input.client.Create(ctx, dpuServiceChain)).To(Succeed())

	By("verify underlying DPU objects are ready")
	dpuservice.VerifyUnderlyingDPUObjectsReady(ctx, dpuClusterClient, input.namespace, interfaceConfigs, []string{"netshoot-to-p0"})
}

// setupHBNOnlyTest creates a test environment for a HBN only service function chain
func setupHBNOnlyTest(ctx context.Context, input *systemTestInput, vfIndex int) {
	hbnServiceID := "doca-hbn"
	hbnNetwork := "mybrhbn"
	interfaceConfigs := []dpuservice.TestDPUServiceInterfaceConfig{
		{
			Name:          "p0",
			Namespace:     input.namespace,
			Type:          "physical",
			InterfaceName: "p0",
			Labels: map[string]string{
				"uplink": "p0",
			},
			Annotations: map[string]string{
				"svc.dpu.nvidia.com/noop-physical-removal": "",
			},
		},
		{
			Name:          "p1",
			Namespace:     input.namespace,
			Type:          "physical",
			InterfaceName: "p1",
			Labels: map[string]string{
				"uplink": "p1",
			},
			Annotations: map[string]string{
				"svc.dpu.nvidia.com/noop-physical-removal": "",
			},
		},
		{
			Name:          fmt.Sprintf("pf0vf%d-rep", vfIndex),
			Namespace:     input.namespace,
			Type:          "vf",
			InterfaceName: fmt.Sprintf("pf0vf%d", vfIndex),
			PFIndex:       0,
			VFIndex:       vfIndex,
			Labels: map[string]string{
				"vf": fmt.Sprintf("pf0vf%d", vfIndex),
			},
		},
		{
			Name:          fmt.Sprintf("pf1vf%d-rep", vfIndex),
			Namespace:     input.namespace,
			Type:          "vf",
			InterfaceName: fmt.Sprintf("pf1vf%d", vfIndex),
			PFIndex:       1,
			VFIndex:       vfIndex,
			Labels: map[string]string{
				"vf": fmt.Sprintf("pf1vf%d", vfIndex),
			},
		},
		{
			Name:          fmt.Sprintf("pf0vf%d-sf", vfIndex),
			Namespace:     input.namespace,
			Type:          "sf",
			InterfaceName: fmt.Sprintf("pf0vf%d_if", vfIndex),
			ServiceID:     hbnServiceID,
			Network:       hbnNetwork,
			Labels: map[string]string{
				dpuservicev1.DPFServiceIDLabelKey: hbnServiceID,
				"svc.dpu.nvidia.com/interface":    fmt.Sprintf("pf0vf%d_sf", vfIndex),
			},
		},
		{
			Name:          fmt.Sprintf("pf1vf%d-sf", vfIndex),
			Namespace:     input.namespace,
			Type:          "sf",
			InterfaceName: fmt.Sprintf("pf1vf%d_if", vfIndex),
			ServiceID:     hbnServiceID,
			Network:       hbnNetwork,
			Labels: map[string]string{
				dpuservicev1.DPFServiceIDLabelKey: hbnServiceID,
				"svc.dpu.nvidia.com/interface":    fmt.Sprintf("pf1vf%d_sf", vfIndex),
			},
		},
		{
			Name:          "p0-sf",
			Namespace:     input.namespace,
			Type:          "sf",
			InterfaceName: "p0_if",
			ServiceID:     hbnServiceID,
			Network:       hbnNetwork,
			Labels: map[string]string{
				dpuservicev1.DPFServiceIDLabelKey: hbnServiceID,
				"svc.dpu.nvidia.com/interface":    "p0_sf",
			},
		},
		{
			Name:          "p1-sf",
			Namespace:     input.namespace,
			Type:          "sf",
			InterfaceName: "p1_if",
			ServiceID:     hbnServiceID,
			Network:       hbnNetwork,
			Labels: map[string]string{
				dpuservicev1.DPFServiceIDLabelKey: hbnServiceID,
				"svc.dpu.nvidia.com/interface":    "p1_sf",
			},
		},
	}

	ipamConfigs := []dpuservice.TestIPAMConfig{
		{
			Name:         "pool1",
			Network:      "10.0.121.0/24",
			GatewayIndex: 2,
			PrefixSize:   29,
			DPU1Subnet:   "10.0.121.0/29",
			DPU2Subnet:   "10.0.121.8/29",
		},
		{
			Name:         "pool2",
			Network:      "10.0.122.0/24",
			GatewayIndex: 2,
			PrefixSize:   29,
			DPU1Subnet:   "10.0.122.0/29",
			DPU2Subnet:   "10.0.122.8/29",
		},
		{
			Name:       "loopback",
			Network:    "11.0.0.0/24",
			PrefixSize: 32,
		},
	}

	By("wait for prerequisite services")
	dpuservice.WaitForDPUServices(ctx, input.client, input.namespace, []string{"sfc-controller"})

	By("create and wait for dpu service interfaces")
	createAndWaitForInterfaces(ctx, input.client, input.dpuServiceInterfaceTemplate, interfaceConfigs)

	By("create hbn only service chains")
	createHBNServiceChains(ctx, input.client, input.namespace, vfIndex, input.dpuServiceChainTemplate)

	By("create hbn ipams")
	createHBNIPAMs(ctx, input.client, input.namespace, input.dpuServiceIPAMTemplate, ipamConfigs)

	By("create and wait for hbn service")
	node1InDPUCluster, node2InDPUCluster := getDPUClusterNodesInOrder(ctx, input.client, dpuClusterClient)
	createHBNService(ctx, input.client, node1InDPUCluster, node2InDPUCluster, input.namespace, input.dpuServiceHBN)
	dpuservice.WaitForDPUServices(ctx, input.client, input.namespace, []string{"doca-hbn"})

	By("verify underlying ServiceChain and ServiceInterface objects are ready")
	dpuservice.VerifyUnderlyingDPUObjectsReady(ctx, dpuClusterClient, input.namespace, interfaceConfigs, []string{"hbn-to-fabric", "host-to-hbn"})
}

// createHBNService deploys the HBN service
func createHBNService(ctx context.Context, testClient client.Client, node1InDPUCluster string, node2InDPUCluster string, namespace string, dpuServiceHBNTemplate *dpuservicev1.DPUService) {
	dpuServiceHBN := generateDPUObj("doca-hbn", namespace, dpuServiceHBNTemplate.DeepCopy())
	perDPUValuesYAML := fmt.Sprintf(`- hostnamePattern: "*"
  values:
    bgp_peer_group: hbn
    vrf1: RED
    vrf2: BLUE
    l3vni1: 100001
    l3vni2: 100002
- hostnamePattern: "%s"
  values:
    bgp_autonomous_system: 65101
- hostnamePattern: "%s"
  values:
    bgp_autonomous_system: 65201`, node1InDPUCluster, node2InDPUCluster)

	existingData := make(map[string]interface{})
	Expect(json.Unmarshal(dpuServiceHBN.Spec.HelmChart.Values.Raw, &existingData)).To(Succeed())

	config, exists := existingData["configuration"].(map[string]interface{})
	Expect(exists).To(BeTrue())
	config["perDPUValuesYAML"] = perDPUValuesYAML

	mergedRaw, err := json.Marshal(existingData)
	Expect(err).NotTo(HaveOccurred())
	dpuServiceHBN.Spec.HelmChart.Values.Raw = mergedRaw

	Expect(testClient.Create(ctx, dpuServiceHBN)).To(Succeed())
}

func getDPUClusterNodesInOrder(ctx context.Context, client client.Client, dpuClusterClient client.Client) (string, string) {
	worker1, _ := getTwoWorkerNodeNames(ctx, client)
	dpuNode1, dpuNode2 := getTwoNodes(ctx, dpuClusterClient)
	if dpuNode1.ObjectMeta.Labels["provisioning.dpu.nvidia.com/host"] == worker1 {
		return dpuNode1.Name, dpuNode2.Name
	} else {
		return dpuNode2.Name, dpuNode1.Name
	}
}

// deleteFirstFoundPodOnDpuCluser deletes the first pod that matches the substring from the DPU cluster
func deleteFirstFoundPodOnDpuCluser(ctx context.Context, podSubstrNameToDelete string, namespace string) {
	pods := &corev1.PodList{}
	deletedPodName := ""
	Expect(dpuClusterClient.List(ctx, pods, client.InNamespace(namespace))).To(Succeed())
	for _, pod := range pods.Items {
		if strings.Contains(pod.Name, podSubstrNameToDelete) {
			deletedPodName = pod.Name
			Expect(dpuClusterClient.Delete(ctx, &pod)).To(Succeed())
			break
		}
	}
	Expect(deletedPodName).NotTo(BeEmpty())
	Eventually(func(g Gomega) {
		g.Expect(dpuClusterClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: deletedPodName}, &corev1.Pod{})).To(MatchError(ContainSubstring("not found")))
	}, 10*time.Minute).Should(Succeed())
}

// createAndWaitForInterfaces creates the DPU service interfaces and waits for them to be ready
func createAndWaitForInterfaces(ctx context.Context, client client.Client, dpuServiceInterfaceTemplate *dpuservicev1.DPUServiceInterface, interfaceConfigs []dpuservice.TestDPUServiceInterfaceConfig) {
	for _, interfaceConfig := range interfaceConfigs {
		createDPUServiceInterface(ctx, interfaceConfig, dpuServiceInterfaceTemplate, client)
	}

	Eventually(func(g Gomega) {
		for _, interfaceConfig := range interfaceConfigs {
			g.Expect(dpuservice.IsDPUServiceInterfaceReady(ctx, g, client, interfaceConfig.Name, interfaceConfig.Namespace)).To(BeTrue())
		}
	}, 10*time.Minute).Should(Succeed())
}

// createDPUServiceInterface creates a DPU service interface with the given name, type and namespace
func createDPUServiceInterface(ctx context.Context, interfaceConfig dpuservice.TestDPUServiceInterfaceConfig, dpuServiceInterfaceTemplate *dpuservicev1.DPUServiceInterface, testClient client.Client) {
	dpuServiceInterface := generateDPUObj(interfaceConfig.Name, interfaceConfig.Namespace, dpuServiceInterfaceTemplate.DeepCopy())
	switch interfaceConfig.Type {
	case "physical":
		dpuservice.SetDPUServiceInterfacePhysical(dpuServiceInterface, interfaceConfig)
	case "vf":
		dpuservice.SetDPUServiceInterfaceVF(dpuServiceInterface, interfaceConfig)
	case "sf":
		dpuservice.SetDPUServiceInterfaceSF(dpuServiceInterface, interfaceConfig)
	default:
		Expect(fmt.Errorf("invalid interface type: %s", interfaceConfig.Type)).To(Succeed())
	}
	Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, dpuServiceInterface))).To(Succeed())
}

// createHBNServiceChains creates the necessary service chains for HBN
func createHBNServiceChains(ctx context.Context, client client.Client, namespace string, vfIndex int, dpuServiceChainTemplate *dpuservicev1.DPUServiceChain) {
	// Create fabric chain
	fabricChain := generateDPUObj("hbn-to-fabric", namespace, dpuServiceChainTemplate.DeepCopy())
	dpuservice.SetHBNChainSwitches(fabricChain, "uplink", "p0", "p1")
	Expect(client.Create(ctx, fabricChain)).To(Succeed())

	// Create host chain
	hostChain := generateDPUObj("host-to-hbn", namespace, dpuServiceChainTemplate.DeepCopy())
	dpuservice.SetHBNChainSwitches(hostChain, "vf", fmt.Sprintf("pf0vf%d", vfIndex), fmt.Sprintf("pf1vf%d", vfIndex))
	Expect(client.Create(ctx, hostChain)).To(Succeed())
}

// createHBNIPAMs creates the IPAM configurations for HBN
func createHBNIPAMs(ctx context.Context, client client.Client, namespace string, dpuServiceIPAMTemplate *dpuservicev1.DPUServiceIPAM, IPAMConfigs []dpuservice.TestIPAMConfig) {
	node1InDPUCluster, node2InDPUCluster := getDPUClusterNodesInOrder(ctx, client, dpuClusterClient)
	for _, config := range IPAMConfigs {
		DPUServiceIPAM := generateDPUObj(config.Name, namespace, dpuServiceIPAMTemplate.DeepCopy())
		dpuservice.SetDPUServiceHBNIPAM(DPUServiceIPAM, config, node1InDPUCluster, node2InDPUCluster)
		Expect(client.Create(ctx, DPUServiceIPAM)).To(Succeed())
	}
}
