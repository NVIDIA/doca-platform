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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func VerifyPlainServiceFunctionChain(ctx context.Context, input *systemTestInput) {
	if !input.hasDpuNodes() {
		Skip("Skip test as there are not multiple nodes")
	}

	podName1 := "pod1"
	podName2 := "pod2"

	By("wait for pre-requisite dpu services to be ready")
	Eventually(func(g Gomega) {
		g.Expect(isDPUServiceReady(ctx, g, input.client, "sfc-controller", input.namespace)).To(BeTrue())
	}, 10*time.Minute).Should(Succeed())

	By("create dpu service interfaces")
	createDPUServiceInterface(ctx, "p0", "physical", input.namespace, input.dpuServiceInterfaceTemplate, input.client)
	createDPUServiceInterface(ctx, "pf0vf5", "vf", input.namespace, input.dpuServiceInterfaceTemplate, input.client)

	By("wait for dpu service interfaces to be ready")
	Eventually(func(g Gomega) {
		g.Expect(isDPUServiceInterfaceReady(ctx, g, input.client, "p0", input.namespace)).To(BeTrue())
		g.Expect(isDPUServiceInterfaceReady(ctx, g, input.client, "pf0vf5", input.namespace)).To(BeTrue())
	}, 10*time.Minute).Should(Succeed())

	By("create dpu service chain")
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
	Eventually(func(g Gomega) {
		g.Expect(verifyUnderlyingDPUInterfaceSetsReady(ctx, g, "p0", input.namespace)).To(BeTrue())
		g.Expect(verifyUnderlyingDPUInterfaceSetsReady(ctx, g, "pf0vf5", input.namespace)).To(BeTrue())
		g.Expect(verifyUnderlyingDPUChainSetsReady(ctx, g, "netshoot-to-p0", input.namespace)).To(BeTrue())
	}, 10*time.Minute).Should(Succeed())

	By("create namespace for the test pods and nads")
	hostNamespace := "sfc-plain-test-ns"
	createTestNamespace(ctx, input.client, hostNamespace)

	By("create network attachment definition and netshoot pods")
	pod1IP := "10.1.121.1"
	pod2IP := "10.1.121.2"
	podCIDR := "24"
	addIPCIDR := func(ip string) string {
		return fmt.Sprintf("%s/%s", ip, podCIDR)
	}
	nadName1 := createNetworkAttachmentDefinition(ctx, input.client, hostNamespace, podName1, 5, addIPCIDR(pod1IP), "", "")
	nadName2 := createNetworkAttachmentDefinition(ctx, input.client, hostNamespace, podName2, 5, addIPCIDR(pod2IP), "", "")
	workerNode1, workerNode2 := getTwoWorkerNodeNames(ctx, input.client)
	createNetshootPodWithNad(ctx, input.client, hostNamespace, podName1, workerNode1, nadName1)
	createNetshootPodWithNad(ctx, input.client, hostNamespace, podName2, workerNode2, nadName2)

	By("wait for pods to be running")
	Eventually(func(g Gomega) {
		g.Expect(isPodRunning(ctx, g, input.client, hostNamespace, podName1)).To(BeTrue())
		g.Expect(isPodRunning(ctx, g, input.client, hostNamespace, podName2)).To(BeTrue())
	}, 10*time.Minute).Should(Succeed())

	By("run traffic between pods")
	startIperf3Server(input.restConfig, hostNamespace, podName2)
	netshootOutput := runIperf3Client(input.restConfig, hostNamespace, podName1, pod2IP)
	analyzeIperfResults(netshootOutput, true)
	defer func() {
		stopIperf3Server(input.restConfig, hostNamespace, podName2)
	}()
}

func VerifyHBNOnlyServiceFunctionChain(ctx context.Context, input *systemTestInput) {
	if !input.hasDpuNodes() {
		Skip("Skip test as there are not multiple nodes")
	}

	podName1 := "pod1"
	podName2 := "pod2"

	By("wait for pre-requisite dpu services to be ready")
	Eventually(func(g Gomega) {
		g.Expect(isDPUServiceReady(ctx, g, input.client, "sfc-controller", input.namespace)).To(BeTrue())
	}, 10*time.Minute).Should(Succeed())

	By("create dpu hbn service interfaces")
	createDPUServiceInterface(ctx, "p0", "physical", input.namespace, input.dpuServiceInterfaceTemplate, input.client)
	createDPUServiceInterface(ctx, "p1", "physical", input.namespace, input.dpuServiceInterfaceTemplate, input.client)
	createDPUServiceInterface(ctx, "pf0vf3-rep", "vf", input.namespace, input.dpuServiceInterfaceTemplate, input.client)
	createDPUServiceInterface(ctx, "pf1vf3-rep", "vf", input.namespace, input.dpuServiceInterfaceTemplate, input.client)
	createDPUServiceInterface(ctx, "pf0vf3-sf", "sf", input.namespace, input.dpuServiceInterfaceTemplate, input.client)
	createDPUServiceInterface(ctx, "pf1vf3-sf", "sf", input.namespace, input.dpuServiceInterfaceTemplate, input.client)
	createDPUServiceInterface(ctx, "p0-sf", "sf", input.namespace, input.dpuServiceInterfaceTemplate, input.client)
	createDPUServiceInterface(ctx, "p1-sf", "sf", input.namespace, input.dpuServiceInterfaceTemplate, input.client)

	By("wait for dpu service interfaces to be ready")
	Eventually(func(g Gomega) {
		g.Expect(isDPUServiceInterfaceReady(ctx, g, input.client, "p0", input.namespace)).To(BeTrue())
		g.Expect(isDPUServiceInterfaceReady(ctx, g, input.client, "p1", input.namespace)).To(BeTrue())
		g.Expect(isDPUServiceInterfaceReady(ctx, g, input.client, "pf0vf3-rep", input.namespace)).To(BeTrue())
		g.Expect(isDPUServiceInterfaceReady(ctx, g, input.client, "pf1vf3-rep", input.namespace)).To(BeTrue())
		g.Expect(isDPUServiceInterfaceReady(ctx, g, input.client, "pf0vf3-sf", input.namespace)).To(BeTrue())
		g.Expect(isDPUServiceInterfaceReady(ctx, g, input.client, "pf1vf3-sf", input.namespace)).To(BeTrue())
		g.Expect(isDPUServiceInterfaceReady(ctx, g, input.client, "p0-sf", input.namespace)).To(BeTrue())
		g.Expect(isDPUServiceInterfaceReady(ctx, g, input.client, "p1-sf", input.namespace)).To(BeTrue())
	}, 10*time.Minute).Should(Succeed())

	By("create dpu hbn service chains")
	dpuServiceChain := generateDPUObj("hbn-to-fabric", input.namespace, input.dpuServiceChainTemplate.DeepCopy())
	setHBNChainSwitches(dpuServiceChain, "uplink", "p0", "p1")
	Expect(input.client.Create(ctx, dpuServiceChain)).To(Succeed())

	dpuServiceChain = generateDPUObj("host-to-hbn", input.namespace, input.dpuServiceChainTemplate.DeepCopy())
	setHBNChainSwitches(dpuServiceChain, "vf", "pf0vf3", "pf1vf3")
	Expect(input.client.Create(ctx, dpuServiceChain)).To(Succeed())

	By("verify underlying DPU objects are ready")
	Eventually(func(g Gomega) {
		g.Expect(verifyUnderlyingDPUInterfaceSetsReady(ctx, g, "p0", input.namespace)).To(BeTrue())
		g.Expect(verifyUnderlyingDPUInterfaceSetsReady(ctx, g, "p1", input.namespace)).To(BeTrue())
		g.Expect(verifyUnderlyingDPUInterfaceSetsReady(ctx, g, "pf0vf3-rep", input.namespace)).To(BeTrue())
		g.Expect(verifyUnderlyingDPUInterfaceSetsReady(ctx, g, "pf1vf3-rep", input.namespace)).To(BeTrue())
		g.Expect(verifyUnderlyingDPUInterfaceSetsReady(ctx, g, "pf0vf3-sf", input.namespace)).To(BeTrue())
		g.Expect(verifyUnderlyingDPUInterfaceSetsReady(ctx, g, "pf1vf3-sf", input.namespace)).To(BeTrue())
		g.Expect(verifyUnderlyingDPUInterfaceSetsReady(ctx, g, "p0-sf", input.namespace)).To(BeTrue())
		g.Expect(verifyUnderlyingDPUInterfaceSetsReady(ctx, g, "p1-sf", input.namespace)).To(BeTrue())
		g.Expect(verifyUnderlyingDPUChainSetsReady(ctx, g, "hbn-to-fabric", input.namespace)).To(BeTrue())
		g.Expect(verifyUnderlyingDPUChainSetsReady(ctx, g, "host-to-hbn", input.namespace)).To(BeTrue())
	}, 10*time.Minute).Should(Succeed())

	By("create dpu service ipam")
	dpuServiceIPAM := generateDPUObj("pool1", input.namespace, input.dpuServiceIPAMTemplate.DeepCopy())
	setDPUServiceHBNIPAM(dpuServiceIPAM, "10.0.121.0/24", 2, 29)
	Expect(input.client.Create(ctx, dpuServiceIPAM)).To(Succeed())

	dpuServiceIPAM = generateDPUObj("pool2", input.namespace, input.dpuServiceIPAMTemplate.DeepCopy())
	setDPUServiceHBNIPAM(dpuServiceIPAM, "10.0.122.0/24", 2, 29)
	Expect(input.client.Create(ctx, dpuServiceIPAM)).To(Succeed())

	dpuServiceIPAM = generateDPUObj("loopback", input.namespace, input.dpuServiceIPAMTemplate.DeepCopy())
	setDPUServiceHBNIPAM(dpuServiceIPAM, "11.0.0.0/24", 0, 32)
	Expect(input.client.Create(ctx, dpuServiceIPAM)).To(Succeed())

	By("create dpu hbn service")
	workerNode1, workerNode2 := getTwoWorkerNodeNames(ctx, input.client)
	node1InDPUCluster, node2InDPUCluster := getTwoNodes(ctx, dpuClusterClient)
	createHBNService(ctx, input.client, node1InDPUCluster, node2InDPUCluster, input.namespace, input.dpuServiceHBN)

	By("wait for hbn service to be ready")
	Eventually(func(g Gomega) {
		g.Expect(isDPUServiceReady(ctx, g, input.client, "doca-hbn", input.namespace)).To(BeTrue())
	}, 20*time.Minute).Should(Succeed())

	By("create namespace for the test pods and nads")
	hostNamespace := "sfc-hbn-test-ns"
	createTestNamespace(ctx, input.client, hostNamespace)

	By("create network attachment definition and netshoot pods")
	pod1IP := "10.0.121.1"
	pod1DST := "10.0.121.8"
	pod1GW := "10.0.121.2"
	pod2IP := "10.0.121.9"
	pod2DST := "10.0.121.0"
	pod2GW := "10.0.121.10"
	podCIDR := "29"
	addIPCIDR := func(ip string) string {
		return fmt.Sprintf("%s/%s", ip, podCIDR)
	}

	nadName1 := createNetworkAttachmentDefinition(ctx, input.client, hostNamespace, podName1, 3, addIPCIDR(pod1IP), addIPCIDR(pod1DST), pod1GW)
	nadName2 := createNetworkAttachmentDefinition(ctx, input.client, hostNamespace, podName2, 3, addIPCIDR(pod2IP), addIPCIDR(pod2DST), pod2GW)
	createNetshootPodWithNad(ctx, input.client, hostNamespace, podName1, workerNode1, nadName1)
	createNetshootPodWithNad(ctx, input.client, hostNamespace, podName2, workerNode2, nadName2)
	By("wait for pods to be running")
	Eventually(func(g Gomega) {
		g.Expect(isPodRunning(ctx, g, input.client, hostNamespace, podName1)).To(BeTrue())
		g.Expect(isPodRunning(ctx, g, input.client, hostNamespace, podName2)).To(BeTrue())
	}, 10*time.Minute).Should(Succeed())

	By("run traffic between pods")
	startIperf3Server(input.restConfig, hostNamespace, podName2)
	netshootOutput := runIperf3Client(input.restConfig, hostNamespace, podName1, pod2IP)
	analyzeIperfResults(netshootOutput, true)
	defer func() {
		stopIperf3Server(input.restConfig, hostNamespace, podName2)
	}()
}

func startIperf3Server(restConfig *rest.Config, namespace string, podName string) {
	execCommandWithRetry(testRESTClient, restConfig, namespace, podName, []string{"iperf3", "-s", "-D"}, 10)
}

func stopIperf3Server(restConfig *rest.Config, namespace string, podName string) {
	execCommandWithRetry(testRESTClient, restConfig, namespace, podName, []string{"pkill", "iperf3"}, 10)
}

func runIperf3Client(restConfig *rest.Config, namespace string, podName string, iperf3ServerIP string) string {
	return execCommandWithRetry(testRESTClient, restConfig, namespace, podName, []string{"iperf3", "-c", iperf3ServerIP, "-J"}, 100)
}

// setDPUServiceHBNIPAM sets the IPAM for the DPU service
func setDPUServiceHBNIPAM(dpuServiceIPAM *dpuservicev1.DPUServiceIPAM, network string, gatewayIndex int32, prefixSize int32) {
	dpuServiceIPAM.Spec = dpuservicev1.DPUServiceIPAMSpec{
		IPV4Network: &dpuservicev1.IPV4Network{
			Network:    network,
			PrefixSize: prefixSize,
		},
	}
	if gatewayIndex != 0 {
		dpuServiceIPAM.Spec.IPV4Network.GatewayIndex = &gatewayIndex
	}
}

// setHBNChainSwitches sets the switches for the DPU service chain
func setHBNChainSwitches(dpuServiceChain *dpuservicev1.DPUServiceChain, interfaceType string, firstInterfaceName string, secondInterfaceName string) {
	dpuServiceChain.Spec.Template.Spec.Template.Spec.Switches = []dpuservicev1.Switch{
		{
			Ports: []dpuservicev1.Port{
				{
					ServiceInterface: dpuservicev1.ServiceIfc{
						MatchLabels: map[string]string{interfaceType: firstInterfaceName},
					},
				},
				{
					ServiceInterface: dpuservicev1.ServiceIfc{
						MatchLabels: map[string]string{"svc.dpu.nvidia.com/service": "doca-hbn", "svc.dpu.nvidia.com/interface": fmt.Sprintf("%s_sf", firstInterfaceName)},
					},
				},
			},
		},
		{
			Ports: []dpuservicev1.Port{
				{
					ServiceInterface: dpuservicev1.ServiceIfc{
						MatchLabels: map[string]string{interfaceType: secondInterfaceName},
					},
				},
				{
					ServiceInterface: dpuservicev1.ServiceIfc{
						MatchLabels: map[string]string{"svc.dpu.nvidia.com/service": "doca-hbn", "svc.dpu.nvidia.com/interface": fmt.Sprintf("%s_sf", secondInterfaceName)},
					},
				},
			},
		},
	}
}

// createDPUServiceInterface creates a DPU service interface with the given name, type and namespace
func createDPUServiceInterface(ctx context.Context, interfaceName string, interfaceType string, namespace string, dpuServiceInterfaceTemplate *dpuservicev1.DPUServiceInterface, testClient client.Client) {
	dpuServiceInterface := generateDPUObj(interfaceName, namespace, dpuServiceInterfaceTemplate.DeepCopy())
	switch interfaceType {
	case "physical":
		setDPUServiceInterfacePhysical(dpuServiceInterface, interfaceName)
	case "vf":
		setDPUServiceInterfaceVF(dpuServiceInterface, strings.Split(interfaceName, "-")[0])
	case "sf":
		setDPUServiceInterfaceSF(dpuServiceInterface, strings.Split(interfaceName, "-")[0])
	default:
		Expect(fmt.Errorf("invalid interface type: %s", interfaceType)).To(Succeed())
	}
	Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, dpuServiceInterface))).To(Succeed())
}

// isDPUServiceInterfaceReady checks if the DPU service interface is ready
func isDPUServiceInterfaceReady(ctx context.Context, g Gomega, testClient client.Client, interfaceName string, namespace string) bool {
	dpuServiceInterface := &dpuservicev1.DPUServiceInterface{}
	g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: interfaceName}, dpuServiceInterface)).To(Succeed())
	return conditions.IsTrue(dpuServiceInterface, conditions.TypeReady)
}

// verifyUnderlyingDPUInterfaceSetsReady checks if the service interface set on the DPU is ready
func verifyUnderlyingDPUInterfaceSetsReady(ctx context.Context, g Gomega, name string, namespace string) bool {
	serviceInterfaceSet := &dpuservicev1.ServiceInterfaceSet{}
	g.Expect(dpuClusterClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, serviceInterfaceSet)).To(Succeed())
	return conditions.IsTrue(serviceInterfaceSet, conditions.TypeReady)
}

// verifyUnderlyingDPUChainSetsReady checks if the service chain set on the DPU is ready
func verifyUnderlyingDPUChainSetsReady(ctx context.Context, g Gomega, name string, namespace string) bool {
	serviceChainSet := &dpuservicev1.ServiceChainSet{}
	g.Expect(dpuClusterClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, serviceChainSet)).To(Succeed())
	return conditions.IsTrue(serviceChainSet, conditions.TypeReady)
}

// setDPUServiceInterfacePhysical sets the values for the physical interface of the DPU service interface
func setDPUServiceInterfacePhysical(dpuServiceInterface *dpuservicev1.DPUServiceInterface, interfaceName string) {
	dpuServiceInterface.Spec.Template.Spec.Template.ObjectMeta = dpuservicev1.ObjectMeta{
		Labels: map[string]string{"uplink": interfaceName},
	}
	dpuServiceInterface.Spec.Template.Spec.Template.Spec = dpuservicev1.ServiceInterfaceSpec{
		InterfaceType: dpuservicev1.InterfaceTypePhysical,
		Physical: &dpuservicev1.Physical{
			InterfaceName: interfaceName,
		},
	}
}

// setDPUServiceInterfaceVF sets the values for the VF interface of the DPU service interface
func setDPUServiceInterfaceVF(dpuServiceInterface *dpuservicev1.DPUServiceInterface, interfaceName string) {
	re := regexp.MustCompile(`pf(\d+)vf(\d+)`)
	matches := re.FindStringSubmatch(interfaceName)
	Expect(matches).To(HaveLen(3), "Invalid interface name")
	pfIndex, err := strconv.Atoi(matches[1])
	Expect(err).NotTo(HaveOccurred(), "Invalid pf number")
	vfIndex, err := strconv.Atoi(matches[2])
	Expect(err).NotTo(HaveOccurred(), "Invalid vf number")
	parentInterface := fmt.Sprintf("p%d", pfIndex)
	dpuServiceInterface.Spec.Template.Spec.Template.ObjectMeta = dpuservicev1.ObjectMeta{
		Labels: map[string]string{"vf": interfaceName},
	}
	dpuServiceInterface.Spec.Template.Spec.Template.Spec = dpuservicev1.ServiceInterfaceSpec{
		InterfaceType: dpuservicev1.InterfaceTypeVF,
		VF: &dpuservicev1.VF{
			ParentInterfaceRef: &parentInterface,
			PFID:               pfIndex,
			VFID:               vfIndex,
		},
	}
}

// setDPUServiceInterfaceSF sets the values for the SF interface of the DPU service interface
func setDPUServiceInterfaceSF(dpuServiceInterface *dpuservicev1.DPUServiceInterface, interfaceName string) {
	dpuServiceInterface.Spec.Template.Spec.Template.ObjectMeta = dpuservicev1.ObjectMeta{
		Labels: map[string]string{"svc.dpu.nvidia.com/interface": fmt.Sprintf("%s_sf", interfaceName), "svc.dpu.nvidia.com/service": "doca-hbn"},
	}
	dpuServiceInterface.Spec.Template.Spec.Template.Spec = dpuservicev1.ServiceInterfaceSpec{
		InterfaceType: "service",
		Service: &dpuservicev1.ServiceDef{
			ServiceID:     "doca-hbn",
			Network:       "mybrhbn",
			InterfaceName: fmt.Sprintf("%s_if", interfaceName),
		},
	}
}

// createHBNService creates an HBN DPU service
func createHBNService(ctx context.Context, testClient client.Client, workerNode1 string, workerNode2 string, namespace string, dpuServiceHBNTemplate *dpuservicev1.DPUService) {
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
    bgp_autonomous_system: 65201`, workerNode1, workerNode2)

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

// createNetshootPodWithNad creates a netshoot pod with a network attachment definition
func createNetshootPodWithNad(ctx context.Context, testClient client.Client, namespace string, podName string, nodeName string, nadName string) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
			Annotations: map[string]string{
				"k8s.v1.cni.cncf.io/networks": nadName,
			},
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
			Containers: []corev1.Container{
				{
					Name:    "netshoot",
					Image:   "mirror.gcr.io/nicolaka/netshoot:v0.13",
					Command: []string{"/bin/sh", "-c"},
					Args:    []string{"tail -F /dev/null"},
				},
			},
		},
	}
	pod.SetLabels(afterEachCleanupLabels)
	Expect(testClient.Create(ctx, pod)).To(Succeed())
}

// isPodRunning checks if a pod is running
func isPodRunning(ctx context.Context, g Gomega, testClient client.Client, namespace, podName string) bool {
	pod := &corev1.Pod{}
	g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: podName}, pod)).To(Succeed())
	return pod.Status.Phase == corev1.PodRunning
}

// isDPUServiceReady checks if a DPU service is ready
func isDPUServiceReady(ctx context.Context, g Gomega, testClient client.Client, serviceName string, namespace string) bool {
	svc := &dpuservicev1.DPUService{}
	g.Expect(testClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: serviceName}, svc)).To(Succeed())
	return conditions.IsTrue(svc, conditions.TypeReady)
}

// execCommandWithRetry executes a command on a pod with retries
func execCommandWithRetry(testRESTClient *rest.RESTClient, config *rest.Config, namespace string, podName string, command []string, maxRetries int) string {
	fmt.Printf("Executing command %v on pod '%s' in namespace '%s'\n", command, podName, namespace)
	retryCount := 0
	req := testRESTClient.Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Command: command,
			Stdout:  true,
			Stderr:  true,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	Expect(err).NotTo(HaveOccurred(), "Failed to create executor")

	var stdout, stderr bytes.Buffer
	context, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	for {
		err = exec.StreamWithContext(context, remotecommand.StreamOptions{
			Stdout: &stdout,
			Stderr: &stderr,
		})

		if err == nil {
			break
		}

		retryCount++
		if retryCount >= maxRetries {
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Failed to execute command after retries: %v. Stderr: %s", err, stderr.String()))
		} else {
			fmt.Printf("Failed to execute command. Retrying...\n")
			time.Sleep(5 * time.Second)
			stdout.Reset()
			stderr.Reset()
		}
	}

	return stdout.String()
}

// IperfResult is used to parse the result of an iperf3 command
type IperfResult struct {
	Start struct {
		Connected []struct {
			LocalHost  string `json:"local_host"`
			RemoteHost string `json:"remote_host"`
		} `json:"connected"`
	} `json:"start"`
	Intervals []struct {
		Sum struct {
			BitsPerSecond float64 `json:"bits_per_second"`
		} `json:"sum"`
	} `json:"intervals"`
	End struct {
		SumSent struct {
			BitsPerSecond float64 `json:"bits_per_second"`
		} `json:"sum_sent"`
	} `json:"end"`
}

// analyzeIperfResults parses and validates the result of an iperf3 command
func analyzeIperfResults(output string, checkPerformance bool) {
	// Parse the JSON output
	var result IperfResult
	err := json.Unmarshal([]byte(output), &result)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("failed to parse iperf3 output: %v", err))

	// Extract IP addresses
	Expect(result.Start.Connected).ShouldNot(BeEmpty(), "no connection information found. output: %s", output)
	localIP := result.Start.Connected[0].LocalHost
	remoteIP := result.Start.Connected[0].RemoteHost
	fmt.Printf("Traffic sent from %s to %s\n", localIP, remoteIP)

	// Extract and validate bitrate
	bitrate := result.End.SumSent.BitsPerSecond
	intervalCount := len(result.Intervals)
	fmt.Printf("Bitrate: %.2f Gbit/sec over %d intervals\n", bitrate/1e9, intervalCount)
	if checkPerformance {
		Expect(bitrate).Should(BeNumerically(">", 18e9), "bitrate is below 18 Gbit/sec")
	} else {
		fmt.Println("Skipping performance check")
	}
}

// createNetworkAttachmentDefinition creates a network attachment definition for a pod
func createNetworkAttachmentDefinition(ctx context.Context, testClient client.Client, namespace string, podName string, vfIndex int, ipAddress string, dst string, gw string) string {
	nadName := fmt.Sprintf("nad-%s", podName)
	name := fmt.Sprintf("hostpf0vf%d", vfIndex)
	hostDevice := fmt.Sprintf("enp8s0f0v%d", vfIndex)
	optionalRoutes := ""
	if dst != "" && gw != "" {
		optionalRoutes = fmt.Sprintf(`,"routes": [{"dst": "%s","gw": "%s"}]`, dst, gw)
	}

	nad := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "k8s.cni.cncf.io/v1",
			"kind":       "NetworkAttachmentDefinition",
			"metadata": map[string]interface{}{
				"name":      nadName,
				"namespace": namespace,
				"labels":    afterEachCleanupLabels,
			},
			"spec": map[string]interface{}{
				"config": fmt.Sprintf(`{
					"cniVersion": "0.3.1",
					"name": "%s",
					"type": "host-device",
					"device": "%s",
					"ipam": {
						"type": "static",
						"addresses": [
							{
								"address": "%s"
							}
						]%s
					}
				}`, name, hostDevice, ipAddress, optionalRoutes),
			},
		},
	}

	Expect(testClient.Create(ctx, nad)).To(Succeed())
	return nadName
}
