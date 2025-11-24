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
	"github.com/nvidia/doca-platform/test/utils"
	"github.com/nvidia/doca-platform/test/utils/dpuservice"
	"github.com/nvidia/doca-platform/test/utils/netshoot"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// mtuTestConfig holds configuration values for MTU tests
type mtuTestConfig struct {
	service1ID              string
	service2ID              string
	chainName               string
	ipInterface             string
	podCount                int
	physicalInterfacePrefix string
	subnetPoolName          string
	nadName                 string
}

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

func VerifyServiceMTUOnDPUPods(ctx context.Context, input *systemTestInput) {
	if input.numberOfDPUNodes != 2 {
		// Test assumes that there are exactly 2 host nodes to match the DPU cluster
		Skip("Skip test as there are not exactly 2 nodes")
	}

	mtuTestLowerMTU := 1300
	mtuTestDefaultMTU := 1500

	cfg := &mtuTestConfig{
		service1ID:              "service1",
		service2ID:              "service2",
		chainName:               "echo-server-to-p0",
		ipInterface:             "pf0sf5_if",
		podCount:                2,
		physicalInterfacePrefix: "p0",
		subnetPoolName:          "subnet-pool1",
		nadName:                 "test-sfc-nad",
	}

	setupMTUServiceFunctionChain(ctx, input, mtuTestDefaultMTU, cfg)

	By("waiting for pods to be ready and capturing initial pod names")
	var initialPod1Names, initialPod2Names []string
	Eventually(func(g Gomega) {
		serviceConfigs := []struct {
			serviceID string
			podNames  *[]string
		}{
			{cfg.service1ID, &initialPod1Names},
			{cfg.service2ID, &initialPod2Names},
		}

		for _, svc := range serviceConfigs {
			pods := getActiveServicePods(ctx, g, input.namespace, svc.serviceID)
			*svc.podNames = make([]string, cfg.podCount)
			for i, pod := range pods {
				g.Expect(pod.Status.Phase).To(Equal(corev1.PodRunning), "Pod %s should be running", pod.Name)
				(*svc.podNames)[i] = pod.Name
			}
		}
	}).WithTimeout(10 * time.Minute).Should(Succeed())

	By(fmt.Sprintf("testing inter-node (pod1->pod2) and intra-node (pod1->pod1) ping connectivity with MTU %d", mtuTestDefaultMTU))
	testPingBetweenPods(ctx, input.namespace, mtuTestDefaultMTU, cfg)

	By("testing serviceMTU change triggers pod recreation")
	updatedPod1Names, updatedPod2Names := updateServiceMTUAndValidatePodRestart(ctx, input, input.namespace, initialPod1Names, initialPod2Names, mtuTestLowerMTU, cfg)

	By("waiting for tunnel connection to stabilize after pod recreation")
	time.Sleep(15 * time.Second)

	By(fmt.Sprintf("testing inter-node (pod1->pod2) and intra-node (pod1->pod1) ping connectivity with MTU %d", mtuTestLowerMTU))
	testPingBetweenPods(ctx, input.namespace, mtuTestLowerMTU, cfg)

	By("Reverting the serviceMTU to the original value, and validating ping connectivity")
	updateServiceMTUAndValidatePodRestart(ctx, input, input.namespace, updatedPod1Names, updatedPod2Names, mtuTestDefaultMTU, cfg)

	By("waiting for tunnel connection to stabilize after pod recreation")
	time.Sleep(15 * time.Second)

	By(fmt.Sprintf("testing inter-node (pod1->pod2) and intra-node (pod1->pod1) ping connectivity with MTU %d", mtuTestDefaultMTU))
	testPingBetweenPods(ctx, input.namespace, mtuTestDefaultMTU, cfg)
}

// updateServiceMTUAndValidatePodRestart tests that changing the serviceMTU triggers pod recreation
func updateServiceMTUAndValidatePodRestart(ctx context.Context, input *systemTestInput, namespace string, initialPod1Names, initialPod2Names []string, newMTU int, cfg *mtuTestConfig) ([]string, []string) {
	By("updating serviceMTU in DPUServiceChain")
	// Use Patch instead of Update to avoid conflicts with concurrent controller updates
	dpuServiceChain := &dpuservicev1.DPUServiceChain{}
	Expect(input.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: cfg.chainName}, dpuServiceChain)).To(Succeed())
	originalDpuServiceChain := dpuServiceChain.DeepCopy()
	dpuServiceChain.Spec.Template.Spec.Template.Spec.Switches[0].ServiceMTU = ptr.To(newMTU)
	Expect(input.client.Patch(ctx, dpuServiceChain, client.MergeFrom(originalDpuServiceChain))).To(Succeed())

	By("waiting for pods to be recreated with new names and be running")
	var newPod1Names, newPod2Names []string
	Eventually(func(g Gomega) {
		serviceConfigs := []struct {
			serviceID       string
			podNames        *[]string
			initialPodNames []string
		}{
			{cfg.service1ID, &newPod1Names, initialPod1Names},
			{cfg.service2ID, &newPod2Names, initialPod2Names},
		}

		for _, svc := range serviceConfigs {
			pods := getActiveServicePods(ctx, g, namespace, svc.serviceID)
			*svc.podNames = make([]string, cfg.podCount)
			for i, pod := range pods {
				g.Expect(pod.Status.Phase).To(Equal(corev1.PodRunning), "Pod %s should be running", pod.Name)
				// Verify pod name is different from initial names
				g.Expect(svc.initialPodNames).NotTo(ContainElement(pod.Name), "Pod %s should have been recreated with a new name", pod.Name)
				(*svc.podNames)[i] = pod.Name
			}
		}
	}).WithTimeout(10 * time.Minute).Should(Succeed())

	return newPod1Names, newPod2Names
}

// getActiveServicePods gets and filters active (non-terminating) pods for a service
func getActiveServicePods(ctx context.Context, g Gomega, namespace, serviceID string) []corev1.Pod {
	podList := &corev1.PodList{}
	g.Expect(dpuClusterClient.List(ctx, podList, client.MatchingLabels{"svc.dpu.nvidia.com/service": serviceID}, client.InNamespace(namespace))).ToNot(HaveOccurred())

	var activePods []corev1.Pod
	for _, pod := range podList.Items {
		if pod.DeletionTimestamp == nil {
			activePods = append(activePods, pod)
		}
	}
	g.Expect(activePods).To(HaveLen(2), "Expected exactly 2 active pods for service %s, but found %d (total including terminating: %d)", serviceID, len(activePods), len(podList.Items))
	return activePods
}

// testPingBetweenPods tests ping connectivity between pod1 on different nodes
func testPingBetweenPods(ctx context.Context, namespace string, mtu int, cfg *mtuTestConfig) {
	// Get pods for both services and find correctly positioned pods for testing
	// Retry until exactly 2 active pods exist for each service and they're positioned correctly
	var pod1Node1, pod1Node2, pod2Node1 *corev1.Pod
	Eventually(func(g Gomega) {
		service1Pods := getActiveServicePods(ctx, g, namespace, cfg.service1ID)
		service2Pods := getActiveServicePods(ctx, g, namespace, cfg.service2ID)

		pod1Node1 = &service1Pods[0]
		for i := 0; i < 2; i++ {
			if service1Pods[i].Spec.NodeName != pod1Node1.Spec.NodeName {
				pod1Node2 = &service1Pods[i]
			}
			if service2Pods[i].Spec.NodeName == pod1Node1.Spec.NodeName {
				pod2Node1 = &service2Pods[i]
			}
		}
		g.Expect(pod1Node2).NotTo(BeNil(), "Should find service1 pod on a different node")
		g.Expect(pod2Node1).NotTo(BeNil(), "Should find service2 pod on the same node as pod1Node1")
	}, 2*time.Minute, 5*time.Second).Should(Succeed())

	pod1Node1IP := getPodIPForInterface(*pod1Node1, cfg.ipInterface)
	pod1Node2IP := getPodIPForInterface(*pod1Node2, cfg.ipInterface)
	pod2Node1IP := getPodIPForInterface(*pod2Node1, cfg.ipInterface)

	// Test successful ping with valid MTU
	By(fmt.Sprintf("Testing ping from %s (%s) to %s (%s) with MTU %d on the same node", pod1Node1.Name, pod1Node1IP, pod2Node1.Name, pod2Node1IP, mtu))
	netshoot.AssertPingSuccessWithMTU(&dpuClusterRestClient, &dpuClusterRestConfig, namespace, pod1Node1.Name, pod2Node1IP, mtu)

	// Test failed ping with MTU too large
	By(fmt.Sprintf("Testing ping fails from %s (%s) to %s (%s) with MTU %d on the same node", pod1Node1.Name, pod1Node1IP, pod2Node1.Name, pod2Node1IP, mtu+1))
	netshoot.AssertPingFailureWithMTU(&dpuClusterRestClient, &dpuClusterRestConfig, namespace, pod1Node1.Name, pod2Node1IP, mtu+1, mtu)

	By(fmt.Sprintf("Testing ping from %s (%s) to %s (%s) with MTU %d on different nodes", pod1Node1.Name, pod1Node1IP, pod1Node2.Name, pod1Node2IP, mtu))
	netshoot.AssertPingSuccessWithMTU(&dpuClusterRestClient, &dpuClusterRestConfig, namespace, pod1Node1.Name, pod1Node2IP, mtu)

	By(fmt.Sprintf("Testing ping from %s (%s) to %s (%s) with MTU %d on different nodes", pod1Node2.Name, pod1Node2IP, pod1Node1.Name, pod1Node1IP, mtu))
	netshoot.AssertPingSuccessWithMTU(&dpuClusterRestClient, &dpuClusterRestConfig, namespace, pod1Node2.Name, pod1Node1IP, mtu)

	By(fmt.Sprintf("Testing ping fails from %s (%s) to %s (%s) with MTU %d on different nodes", pod1Node1.Name, pod1Node1IP, pod1Node2.Name, pod1Node2IP, mtu+1))
	netshoot.AssertPingFailureWithMTU(&dpuClusterRestClient, &dpuClusterRestConfig, namespace, pod1Node1.Name, pod1Node2IP, mtu+1, mtu)
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
	dpuServiceChain := utils.GenerateDPUObj("netshoot-to-p0", input.namespace, input.dpuServiceChainTemplate.DeepCopy())
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
	dpuServiceHBN := utils.GenerateDPUObj("doca-hbn", namespace, dpuServiceHBNTemplate.DeepCopy())
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
	dpuServiceInterface := utils.GenerateDPUObj(interfaceConfig.Name, interfaceConfig.Namespace, dpuServiceInterfaceTemplate.DeepCopy())
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
	fabricChain := utils.GenerateDPUObj("hbn-to-fabric", namespace, dpuServiceChainTemplate.DeepCopy())
	dpuservice.SetHBNChainSwitches(fabricChain, "uplink", "p0", "p1")
	Expect(client.Create(ctx, fabricChain)).To(Succeed())

	// Create host chain
	hostChain := utils.GenerateDPUObj("host-to-hbn", namespace, dpuServiceChainTemplate.DeepCopy())
	dpuservice.SetHBNChainSwitches(hostChain, "vf", fmt.Sprintf("pf0vf%d", vfIndex), fmt.Sprintf("pf1vf%d", vfIndex))
	Expect(client.Create(ctx, hostChain)).To(Succeed())
}

// createHBNIPAMs creates the IPAM configurations for HBN
func createHBNIPAMs(ctx context.Context, client client.Client, namespace string, dpuServiceIPAMTemplate *dpuservicev1.DPUServiceIPAM, IPAMConfigs []dpuservice.TestIPAMConfig) {
	node1InDPUCluster, node2InDPUCluster := getDPUClusterNodesInOrder(ctx, client, dpuClusterClient)
	for _, config := range IPAMConfigs {
		DPUServiceIPAM := utils.GenerateDPUObj(config.Name, namespace, dpuServiceIPAMTemplate.DeepCopy())
		dpuservice.SetDPUServiceHBNIPAM(DPUServiceIPAM, config, node1InDPUCluster, node2InDPUCluster)
		Expect(client.Create(ctx, DPUServiceIPAM)).To(Succeed())
	}
}

func setupMTUServiceFunctionChain(ctx context.Context, input *systemTestInput, mtu int, cfg *mtuTestConfig) {
	By("wait for prerequisite services")
	dpuservice.WaitForDPUServices(ctx, input.client, input.namespace, []string{"sfc-controller"})

	By("create dpu service interfaces for service function")
	interfaceConfigs := []dpuservice.TestDPUServiceInterfaceConfig{
		{
			Name:          cfg.physicalInterfacePrefix,
			Type:          "physical",
			Namespace:     input.namespace,
			InterfaceName: cfg.physicalInterfacePrefix,
			Labels: map[string]string{
				"uplink": cfg.physicalInterfacePrefix,
			},
		},
	}

	for _, serviceID := range []string{cfg.service1ID, cfg.service2ID} {
		interfaceConfigs = append(interfaceConfigs, dpuservice.TestDPUServiceInterfaceConfig{
			Name:          fmt.Sprintf("%s-%s", cfg.physicalInterfacePrefix+"-sf", serviceID),
			Type:          "sf",
			Namespace:     input.namespace,
			InterfaceName: cfg.ipInterface,
			Network:       cfg.nadName,
			ServiceID:     serviceID,
			Labels: map[string]string{
				"svc.dpu.nvidia.com/service":   serviceID,
				"svc.dpu.nvidia.com/interface": "p0_sf",
			},
		})
	}

	By("create and wait for dpu service interfaces")
	createAndWaitForInterfaces(ctx, input.client, input.dpuServiceInterfaceTemplate, interfaceConfigs)

	By("create ipam for service function")
	dpuServiceIPAM := utils.GenerateDPUObj(cfg.subnetPoolName, input.namespace, input.dpuServiceIPAMTemplate.DeepCopy())
	dpuServiceIPAM.Spec.IPV4Subnet = &dpuservicev1.IPV4Subnet{
		Subnet:         "10.44.44.0/24",
		Gateway:        "10.44.44.1",
		PerNodeIPCount: 50,
	}
	dpuServiceIPAM.Spec.ObjectMeta.Labels = map[string]string{
		"svc.dpu.nvidia.com/pool": cfg.subnetPoolName,
	}
	Expect(input.client.Create(ctx, dpuServiceIPAM)).To(Succeed())

	By("create DPUServiceNAD for automatic resource injection")
	dpuServiceNAD := utils.GenerateDPUObj(cfg.nadName, input.namespace, input.dpuServiceNAD.DeepCopy())
	dpuServiceNAD.Spec.ServiceMTU = mtu
	Expect(input.client.Create(ctx, dpuServiceNAD)).To(Succeed())

	By("create netshoot DPU services")
	for _, serviceID := range []string{cfg.service1ID, cfg.service2ID} {
		dpuService := utils.GenerateDPUObj(serviceID, input.namespace, input.dpuService.DeepCopy())
		configureNetshootDPUService(dpuService, serviceID)
		Expect(input.client.Create(ctx, dpuService)).To(Succeed())
	}

	By("create dpu service chain for service function")
	dpuServiceChain := utils.GenerateDPUObj(cfg.chainName, input.namespace, input.dpuServiceChainTemplate.DeepCopy())
	dpuServiceChain.Spec.Template.Spec.Template.Spec.Switches = []dpuservicev1.Switch{
		{
			Ports: []dpuservicev1.Port{
				{
					ServiceInterface: dpuservicev1.ServiceIfc{
						MatchLabels: map[string]string{
							"uplink": cfg.physicalInterfacePrefix,
						},
					},
				},
				{
					ServiceInterface: dpuservicev1.ServiceIfc{
						IPAM: &dpuservicev1.IPAM{
							MatchLabels: map[string]string{
								"svc.dpu.nvidia.com/pool": cfg.subnetPoolName,
							},
						},
						MatchLabels: map[string]string{
							"svc.dpu.nvidia.com/interface": cfg.physicalInterfacePrefix + "_sf",
							"svc.dpu.nvidia.com/service":   cfg.service1ID,
						},
					},
				},
				{
					ServiceInterface: dpuservicev1.ServiceIfc{
						IPAM: &dpuservicev1.IPAM{
							MatchLabels: map[string]string{
								"svc.dpu.nvidia.com/pool": cfg.subnetPoolName,
							},
						},
						MatchLabels: map[string]string{
							"svc.dpu.nvidia.com/interface": cfg.physicalInterfacePrefix + "_sf",
							"svc.dpu.nvidia.com/service":   cfg.service2ID,
						},
					},
				},
			},
			ServiceMTU: ptr.To(mtu),
		},
	}
	Expect(input.client.Create(ctx, dpuServiceChain)).To(Succeed())

	By("verify underlying DPU objects are ready")
	dpuservice.VerifyUnderlyingDPUObjectsReady(ctx, dpuClusterClient, input.namespace, interfaceConfigs, []string{cfg.chainName})
}

// configureNetshootDPUService configures a DPUService for netshoot using the dummydpuservice chart
func configureNetshootDPUService(dpuService *dpuservicev1.DPUService, serviceID string) {
	dpuService.Spec.ServiceID = &serviceID

	dpuService.Spec.Interfaces = []string{fmt.Sprintf("%s-%s", "p0-sf", serviceID)}

	dpuService.Spec.HelmChart.Source = dpuservicev1.ApplicationSource{
		Chart:   "dummydpuservice-chart",
		Version: tag,
		RepoURL: helmRegistry,
	}

	values := make(map[string]any)
	values["imagePullSecrets"] = []map[string]string{{"name": dpfPullSecretName}}
	values["image"] = map[string]string{"repository": netutilsImage}
	rawValues, err := json.Marshal(values)
	Expect(err).NotTo(HaveOccurred())
	dpuService.Spec.HelmChart.Values.Raw = rawValues
}
