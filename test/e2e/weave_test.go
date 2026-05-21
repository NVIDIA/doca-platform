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
	"time"

	"github.com/nvidia/doca-platform/test/e2e/cleanup"
	"github.com/nvidia/doca-platform/test/utils/netshoot"
	"github.com/nvidia/doca-platform/test/utils/vpc"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("Weave testcases", Labels{Domain.Weave}, Ordered, func() {
	var (
		workerNode1, workerNode2                               string
		fcPod1, fcPod2                                         *corev1.Pod
		pfMACP0Node1, pfMACP0Node2, pfMACP1Node1, pfMACP1Node2 string
	)

	BeforeAll(func() {
		weavePrerequisiteScope = cleanupTracker.RegisterScope(cleanup.NamedScopeManual("weave-prerequisites"))
		weaveContextScope = cleanupTracker.RegisterScope(cleanup.NamedScopeManual("weave-tests"))

		for _, label := range CurrentSpecReport().Labels() {
			if label != Domain.RequiresNodes {
				continue
			}

			if !input.hasDpuNodes() {
				Skip("Skip test as there are no DPU nodes")
			}

			weavePrerequisiteScope.CleanupBefore()
			weaveContextScope.CleanupBefore()

			provInput := getProvisionDPUClustersInputForWeave(ctx, getProvisionDPUClustersInput(), input.client)
			Expect(provInput.dpuClusters).ToNot(BeEmpty(), "no DPU clusters found via config or discovery")

			By("Creating DPU cluster client for verification")
			getDPUClusterClients(ctx, provInput)
			Expect(dpuClusterClient).ToNot(BeEmpty(), "no DPU cluster clients were created")

			By("Verifying DPU cluster has ready nodes")
			VerifyDPUClusterWithNodes(ctx, provInput)

			By("Waiting for DPU cluster pods to be ready")
			VerifyClusterPods(ctx, dpuClusterClient[0], systemPodsToVerify)
			By("Waiting for DPFOperatorConfig to be ready")
			VerifyDPFOperatorConfigReady(ctx, input.client, 20*time.Minute)

			By("Waiting for Weave pods on DPU cluster to be ready")
			VerifyClusterPods(ctx, dpuClusterClient[0], weavePodsToVerify)

			By("Getting ready flow controller pods")
			flowControllerPods := netshoot.GetReadyPodsMatchingLabels(ctx, dpuClusterClient[0], dpfOperatorSystemNamespace,
				map[string]string{weaveDPUServiceLabelKey: weaveFlowControllerName})
			Expect(flowControllerPods).To(HaveLen(2), "expected 2 ready %s pods", weaveFlowControllerName)

			By("Getting ready dhcp agent pods")
			dhcpAgentPods := netshoot.GetReadyPodsMatchingLabels(ctx, dpuClusterClient[0], dpfOperatorSystemNamespace,
				map[string]string{weaveDPUServiceLabelKey: weaveDHCPAgentName})
			Expect(dhcpAgentPods).To(HaveLen(2), "expected 2 ready %s pods", weaveDHCPAgentName)

			workerNode1, workerNode2 = getTwoWorkerNodeNames(ctx, input.client)
			By("Getting DPU cluster nodes in order")
			dpuNode1, dpuNode2 := getDPUNodesInOrder(ctx, input.client, dpuClusterClient[0])
			fcPod1 = netshoot.GetPodOnNode(flowControllerPods, dpuNode1.Name)
			fcPod2 = netshoot.GetPodOnNode(flowControllerPods, dpuNode2.Name)
			Expect(fcPod1).ToNot(BeNil(), "no flow-controller pod found on DPU node %s", dpuNode1.Name)
			Expect(fcPod2).ToNot(BeNil(), "no flow-controller pod found on DPU node %s", dpuNode2.Name)

			By("Verifying OVS is responsive on flow-controller pods")
			verifyOVSResponsive(fcPod1)
			verifyOVSResponsive(fcPod2)

			By("Getting PF MAC addresses for p0 and p1 from DPU flow-controller pods")
			pfMACP0Node1 = getPFMACFromFlowControllerByPort(fcPod1, weaveDPUPortP0)
			pfMACP0Node2 = getPFMACFromFlowControllerByPort(fcPod2, weaveDPUPortP0)
			pfMACP1Node1 = getPFMACFromFlowControllerByPort(fcPod1, weaveDPUPortP1)
			pfMACP1Node2 = getPFMACFromFlowControllerByPort(fcPod2, weaveDPUPortP1)
		}
	})

	AfterAll(func() {
		weaveContextScope.CleanupAfter()
		weavePrerequisiteScope.CleanupAfter()
	})

	Context("Pre-requisite services", Labels{Domain.RequiresNodes}, Ordered, func() {
		var dhcpDS *appsv1.DaemonSet

		It("should deploy host DHCP CNI daemon", func() {
			dhcpDS = vpc.DeployDHCPDaemon(ctx, input.client, weaveInput.dhcpDaemonSet, weavePrerequisiteScope.CleanupLabels)
		})

		It("should wait for DHCP daemon pods to be ready", func() {
			vpc.WaitForDHCPDaemonReady(ctx, input.client, dhcpDS)
		})
	})

	Context("Single virtual network, cross-node traffic on both PFs", Labels{Domain.RequiresNodes}, Ordered, func() {
		var (
			overlayIPP0Node1, overlayIPP0Node2 string
			overlayIPP1Node1, overlayIPP1Node2 string
			testPodConfigs                     []*netshoot.TestPodConfig
			contextHasFailed                   bool
			grpcCleanup                        []func()
		)

		const (
			trafficVNetID = "test-vnet-1"
			trafficVNI    = uint32(1001)
			trafficTestNS = "weave-traffic-test"
			podP0Node1    = "weave-p0-pod-1"
			podP0Node2    = "weave-p0-pod-2"
			podP1Node1    = "weave-p1-pod-1"
			podP1Node2    = "weave-p1-pod-2"
		)

		AfterEach(func() {
			if CurrentSpecReport().Failed() {
				contextHasFailed = true
			}
		})

		AfterAll(func() {
			if contextHasFailed {
				By("Weave: Report failure for this spec")
				reportAfterEach(CurrentSpecReport())
			}
			for i := len(grpcCleanup) - 1; i >= 0; i-- {
				grpcCleanup[i]()
			}
			weaveContextScope.CleanupAfter()
		})

		It("should create test namespace", func() {
			vpc.CreateTestNamespace(ctx, input.client, trafficTestNS, weaveContextScope.CleanupLabels)
		})

		It("should create virtual network on both flow-controller pods", func() {
			createVNetOnPod(fcPod1, trafficVNetID, trafficVNI, weaveVNetSubnet)
			grpcCleanup = append(grpcCleanup, func() { deleteVNetOnPod(fcPod1, trafficVNetID) })
			createVNetOnPod(fcPod2, trafficVNetID, trafficVNI, weaveVNetSubnet)
			grpcCleanup = append(grpcCleanup, func() { deleteVNetOnPod(fcPod2, trafficVNetID) })
		})

		It("should create PF attachments for p0 on both nodes", func() {
			var attIDP0Fc1, attIDP0Fc2 string
			attIDP0Fc1, overlayIPP0Node1 = createPFAttachmentAndWaitForHostIP(fcPod1, trafficVNetID, pfMACP0Node1)
			grpcCleanup = append(grpcCleanup, func() { deleteAttachmentOnPod(fcPod1, attIDP0Fc1) })
			attIDP0Fc2, overlayIPP0Node2 = createPFAttachmentAndWaitForHostIP(fcPod2, trafficVNetID, pfMACP0Node2)
			grpcCleanup = append(grpcCleanup, func() { deleteAttachmentOnPod(fcPod2, attIDP0Fc2) })
		})

		It("should create PF attachments for p1 on both nodes", func() {
			var attIDP1Fc1, attIDP1Fc2 string
			attIDP1Fc1, overlayIPP1Node1 = createPFAttachmentAndWaitForHostIP(fcPod1, trafficVNetID, pfMACP1Node1)
			grpcCleanup = append(grpcCleanup, func() { deleteAttachmentOnPod(fcPod1, attIDP1Fc1) })
			attIDP1Fc2, overlayIPP1Node2 = createPFAttachmentAndWaitForHostIP(fcPod2, trafficVNetID, pfMACP1Node2)
			grpcCleanup = append(grpcCleanup, func() { deleteAttachmentOnPod(fcPod2, attIDP1Fc2) })
		})

		It("should verify OVS isolation bridges exist on both DPU nodes", func() {
			verifyIsolationBridgeExists(fcPod1, trafficVNI, weaveDPUPortP0)
			verifyIsolationBridgeExists(fcPod1, trafficVNI, weaveDPUPortP1)
			verifyIsolationBridgeExists(fcPod2, trafficVNI, weaveDPUPortP0)
			verifyIsolationBridgeExists(fcPod2, trafficVNI, weaveDPUPortP1)
		})

		It("should create DHCP NADs and netshoot pods on worker nodes", func() {
			nadP0 := weaveDHCPNADP0
			nadP1 := weaveDHCPNADP1
			vpc.CreateDHCPNetworkAttachmentDefinition(ctx, input.client, trafficTestNS, nadP0, weaveHostPFInterfaceP0, weavePFMTU, weaveContextScope.CleanupLabels)
			vpc.CreateDHCPNetworkAttachmentDefinition(ctx, input.client, trafficTestNS, nadP1, weaveHostPFInterfaceP1, weavePFMTU, weaveContextScope.CleanupLabels)
			testPodConfigs = []*netshoot.TestPodConfig{
				{Namespace: trafficTestNS, Name: podP0Node1, NodeName: workerNode1, NADName: nadP0, Labels: weaveContextScope.CleanupLabels},
				{Namespace: trafficTestNS, Name: podP0Node2, NodeName: workerNode2, NADName: nadP0, Labels: weaveContextScope.CleanupLabels},
				{Namespace: trafficTestNS, Name: podP1Node1, NodeName: workerNode1, NADName: nadP1, Labels: weaveContextScope.CleanupLabels},
				{Namespace: trafficTestNS, Name: podP1Node2, NodeName: workerNode2, NADName: nadP1, Labels: weaveContextScope.CleanupLabels},
			}
			netshoot.CreatePods(ctx, input.client, testPodConfigs)
		})

		It("should verify netshoot pods are running", func() {
			netshoot.WaitForPodsReady(ctx, input.client, testPodConfigs, vpc.LongTimeout)
		})

		It("should verify overlay routes on netshoot pods", func() {
			ensureOverlayRoute(hostClusterRESTClient, input.restConfig, trafficTestNS, podP0Node1, overlayIPP0Node1, weaveVNetSubnet)
			ensureOverlayRoute(hostClusterRESTClient, input.restConfig, trafficTestNS, podP0Node2, overlayIPP0Node2, weaveVNetSubnet)
			ensureOverlayRoute(hostClusterRESTClient, input.restConfig, trafficTestNS, podP1Node1, overlayIPP1Node1, weaveVNetSubnet)
			ensureOverlayRoute(hostClusterRESTClient, input.restConfig, trafficTestNS, podP1Node2, overlayIPP1Node2, weaveVNetSubnet)
		})

		It("should verify cross-node ping succeeds on p0", func() {
			netshoot.AssertPingSuccess(&hostClusterRESTClient, &input.restConfig, trafficTestNS, podP0Node1, overlayIPP0Node2)
			netshoot.AssertPingSuccess(&hostClusterRESTClient, &input.restConfig, trafficTestNS, podP0Node2, overlayIPP0Node1)
		})

		It("should verify cross-node ping succeeds on p1", func() {
			netshoot.AssertPingSuccess(&hostClusterRESTClient, &input.restConfig, trafficTestNS, podP1Node1, overlayIPP1Node2)
			netshoot.AssertPingSuccess(&hostClusterRESTClient, &input.restConfig, trafficTestNS, podP1Node2, overlayIPP1Node1)
		})

		It("should verify performance with iperf cross-node traffic on p0", func() {
			netshoot.RunTrafficTest(&hostClusterRESTClient, &input.restConfig, trafficTestNS, podP0Node1, podP0Node2, overlayIPP0Node2)
		})

		It("should verify performance with iperf cross-node traffic on p1", func() {
			netshoot.RunTrafficTest(&hostClusterRESTClient, &input.restConfig, trafficTestNS, podP1Node1, podP1Node2, overlayIPP1Node2)
		})
	})

	Context("traffic isolation between different VNets", Labels{Domain.RequiresNodes}, Ordered, func() {
		var (
			overlayIP1, overlayIP2 string
			testPodConfigs         []*netshoot.TestPodConfig
			contextHasFailed       bool
			grpcCleanup            []func()
		)

		const (
			isolVNet1ID = "isol-vnet-1"
			isolVNet2ID = "isol-vnet-2"
			isolVNI1    = uint32(2001)
			isolVNI2    = uint32(2002)
			isolTestNS  = "weave-isolation-test"
			isolPod1    = "weave-isol-pod-1"
			isolPod2    = "weave-isol-pod-2"
		)

		AfterEach(func() {
			if CurrentSpecReport().Failed() {
				contextHasFailed = true
			}
		})

		AfterAll(func() {
			if contextHasFailed {
				By("Weave: Report failure for this spec")
				reportAfterEach(CurrentSpecReport())
			}
			for i := len(grpcCleanup) - 1; i >= 0; i-- {
				grpcCleanup[i]()
			}
			weaveContextScope.CleanupAfter()
		})

		It("should create test namespace", func() {
			vpc.CreateTestNamespace(ctx, input.client, isolTestNS, weaveContextScope.CleanupLabels)
		})

		It("should create both isolation virtual networks on both flow-controller pods", func() {
			createVNetOnPod(fcPod1, isolVNet1ID, isolVNI1, weaveVNetSubnet)
			grpcCleanup = append(grpcCleanup, func() { deleteVNetOnPod(fcPod1, isolVNet1ID) })
			createVNetOnPod(fcPod2, isolVNet1ID, isolVNI1, weaveVNetSubnet)
			grpcCleanup = append(grpcCleanup, func() { deleteVNetOnPod(fcPod2, isolVNet1ID) })
			createVNetOnPod(fcPod1, isolVNet2ID, isolVNI2, weaveVNetSubnet)
			grpcCleanup = append(grpcCleanup, func() { deleteVNetOnPod(fcPod1, isolVNet2ID) })
			createVNetOnPod(fcPod2, isolVNet2ID, isolVNI2, weaveVNetSubnet)
			grpcCleanup = append(grpcCleanup, func() { deleteVNetOnPod(fcPod2, isolVNet2ID) })
		})

		It("should attach worker node 1 to vnet-1 and worker node 2 to vnet-2", func() {
			var attIDIsolFc1, attIDIsolFc2 string
			attIDIsolFc1, overlayIP1 = createPFAttachmentAndWaitForHostIP(fcPod1, isolVNet1ID, pfMACP0Node1)
			grpcCleanup = append(grpcCleanup, func() { deleteAttachmentOnPod(fcPod1, attIDIsolFc1) })
			attIDIsolFc2, overlayIP2 = createPFAttachmentAndWaitForHostIP(fcPod2, isolVNet2ID, pfMACP0Node2)
			grpcCleanup = append(grpcCleanup, func() { deleteAttachmentOnPod(fcPod2, attIDIsolFc2) })
		})

		It("should verify OVS isolation bridges exist on each DPU for its PF-attached VNet", func() {
			// br-isol-<vni>-<pci> is created when that flow-controller has a PF attachment for the VNI
			// (bridgemanager), not only when CreateVirtualNetwork succeeded on that pod.
			verifyIsolationBridgeExists(fcPod1, isolVNI1, weaveDPUPortP0)
			verifyIsolationBridgeExists(fcPod2, isolVNI2, weaveDPUPortP0)
		})

		It("should create DHCP NAD and netshoot pods on worker nodes", func() {
			nadName := weaveDHCPNADP0
			vpc.CreateDHCPNetworkAttachmentDefinition(ctx, input.client, isolTestNS, nadName, weaveHostPFInterfaceP0, weavePFMTU, weaveContextScope.CleanupLabels)
			testPodConfigs = []*netshoot.TestPodConfig{
				{Namespace: isolTestNS, Name: isolPod1, NodeName: workerNode1, NADName: nadName, Labels: weaveContextScope.CleanupLabels},
				{Namespace: isolTestNS, Name: isolPod2, NodeName: workerNode2, NADName: nadName, Labels: weaveContextScope.CleanupLabels},
			}
			netshoot.CreatePods(ctx, input.client, testPodConfigs)
		})

		It("should verify netshoot pods are running", func() {
			netshoot.WaitForPodsReady(ctx, input.client, testPodConfigs, vpc.LongTimeout)
		})

		It("should verify overlay routes on netshoot pods", func() {
			ensureOverlayRoute(hostClusterRESTClient, input.restConfig, isolTestNS, isolPod1, overlayIP1, weaveVNetSubnet)
			ensureOverlayRoute(hostClusterRESTClient, input.restConfig, isolTestNS, isolPod2, overlayIP2, weaveVNetSubnet)
		})

		It("should deny ping between worker nodes on different virtual networks", func() {
			netshoot.AssertPingFailure(&hostClusterRESTClient, &input.restConfig, isolTestNS, isolPod1, overlayIP2)
			netshoot.AssertPingFailure(&hostClusterRESTClient, &input.restConfig, isolTestNS, isolPod2, overlayIP1)
		})
	})

	// Runs AFTER the iperf and isolation Contexts so the host PFs are free for dhcpcd to claim.
	Context("RDMA over Weave via host PFs", Labels{Domain.RequiresNodes}, Ordered, func() {
		const (
			rdmaVNetID = "vnet"
			rdmaVNI    = uint32(3001)
			rdmaTestNS = "weave-rdma-test"
			rdmaPod1   = "weave-rdma-w1"
			rdmaPod2   = "weave-rdma-w2"
		)
		var (
			netutilsPod1, netutilsPod2         *corev1.Pod
			overlayIPP0Node1, overlayIPP0Node2 string
			grpcCleanup                        []func()
			contextHasFailed                   bool
		)

		BeforeAll(func() {
			vpc.CreateTestNamespace(ctx, input.client, rdmaTestNS, weaveContextScope.CleanupLabels)
			CopySecretToNamespace(ctx, input.client, dpfPullSecretName, dpfOperatorSystemNamespace, rdmaTestNS, weaveContextScope.CleanupLabels)
			netutilsPod1 = createNetutilsHostPodOnNode(ctx, input.client, rdmaTestNS, rdmaPod1, workerNode1)
			netutilsPod2 = createNetutilsHostPodOnNode(ctx, input.client, rdmaTestNS, rdmaPod2, workerNode2)
		})

		AfterEach(func() {
			if CurrentSpecReport().Failed() {
				contextHasFailed = true
			}
		})

		AfterAll(func() {
			if contextHasFailed {
				By("Weave RDMA: Report failure for this spec")
				reportAfterEach(CurrentSpecReport())
			}
			// Pod preStop hook flushes dhcpcd state when CleanupAfter deletes the netutils pods.
			for i := len(grpcCleanup) - 1; i >= 0; i-- {
				grpcCleanup[i]()
			}
			weaveContextScope.CleanupAfter()
		})

		It("should create vnet on both flow-controller pods", func() {
			createVNetOnPod(fcPod1, rdmaVNetID, rdmaVNI, weaveVNetSubnet)
			grpcCleanup = append(grpcCleanup, func() { deleteVNetOnPod(fcPod1, rdmaVNetID) })
			createVNetOnPod(fcPod2, rdmaVNetID, rdmaVNI, weaveVNetSubnet)
			grpcCleanup = append(grpcCleanup, func() { deleteVNetOnPod(fcPod2, rdmaVNetID) })
		})

		It("should create PF attachments for vnet on p0 of both nodes", func() {
			var attP0Fc1, attP0Fc2 string
			attP0Fc1, overlayIPP0Node1 = createPFAttachmentAndWaitForHostIP(fcPod1, rdmaVNetID, pfMACP0Node1)
			grpcCleanup = append(grpcCleanup, func() { deleteAttachmentOnPod(fcPod1, attP0Fc1) })
			attP0Fc2, overlayIPP0Node2 = createPFAttachmentAndWaitForHostIP(fcPod2, rdmaVNetID, pfMACP0Node2)
			grpcCleanup = append(grpcCleanup, func() { deleteAttachmentOnPod(fcPod2, attP0Fc2) })
		})

		It("should verify OVS isolation bridges for vnet on p0 of both DPUs", func() {
			verifyIsolationBridgeExists(fcPod1, rdmaVNI, weaveDPUPortP0)
			verifyIsolationBridgeExists(fcPod2, rdmaVNI, weaveDPUPortP0)
		})

		It("should plumb overlay IPs onto worker p0 PFs via dhcpcd", func() {
			acquireDHCPLeaseInPod(hostClusterRESTClient, input.restConfig, netutilsPod1, weaveHostPFInterfaceP0, overlayIPP0Node1)
			acquireDHCPLeaseInPod(hostClusterRESTClient, input.restConfig, netutilsPod2, weaveHostPFInterfaceP0, overlayIPP0Node2)
		})

		It("should run ib_write_bw between the two hosts on p0 and meet the BW threshold", func() {
			runIBWriteBWPodToPod(hostClusterRESTClient, input.restConfig, netutilsPod2, netutilsPod1, weaveHostPFRDMADeviceP0, overlayIPP0Node2, weaveIBWriteBWDuration)
		})

		It("should run ib_write_bw between the two hosts on p0 with --reversed and meet the BW threshold", func() {
			// Running with --reversed checks that the RDMA traffic also works in reverse direction for sanity purposes.
			runIBWriteBWPodToPod(hostClusterRESTClient, input.restConfig, netutilsPod2, netutilsPod1, weaveHostPFRDMADeviceP0, overlayIPP0Node2, weaveIBWriteBWDuration, "--reversed")
		})
	})

	Context("traffic isolation between VNets on same node", Labels{Domain.RequiresNodes}, Ordered, func() {
		var (
			overlayIP1, overlayIP2 string
			testPodConfigs         []*netshoot.TestPodConfig
			contextHasFailed       bool
			grpcCleanup            []func()
		)

		const (
			isolVNet1ID  = "isol-vnet-1"
			isolVNet2ID  = "isol-vnet-2"
			isolVNI1     = uint32(100)
			isolVNI2     = uint32(101)
			isolTestNS   = "weave-isolation-samenode-test"
			isolPod1     = "weave-isol-pod-1"
			isolPod2     = "weave-isol-pod-2"
			secondSubnet = "20.0.0.0/8"
		)

		AfterEach(func() {
			if CurrentSpecReport().Failed() {
				contextHasFailed = true
			}
		})

		AfterAll(func() {
			if contextHasFailed {
				By("Weave: Report failure for this spec")
				reportAfterEach(CurrentSpecReport())
			}
			for i := len(grpcCleanup) - 1; i >= 0; i-- {
				grpcCleanup[i]()
			}
			weaveContextScope.CleanupAfter()
		})

		It("should create test namespace", func() {
			vpc.CreateTestNamespace(ctx, input.client, isolTestNS, weaveContextScope.CleanupLabels)
		})

		It("should create both isolation virtual networks on flow-controller pod 1", func() {
			createVNetOnPod(fcPod1, isolVNet1ID, isolVNI1, weaveVNetSubnet)
			grpcCleanup = append(grpcCleanup, func() { deleteVNetOnPod(fcPod1, isolVNet1ID) })
			createVNetOnPod(fcPod1, isolVNet2ID, isolVNI2, secondSubnet)
			grpcCleanup = append(grpcCleanup, func() { deleteVNetOnPod(fcPod1, isolVNet2ID) })
		})

		It("should create two attachments on worker node 1", func() {
			var attIDIsolFc1, attIDIsolFc2 string
			attIDIsolFc1, overlayIP1 = createPFAttachmentAndWaitForHostIP(fcPod1, isolVNet1ID, pfMACP0Node1)
			grpcCleanup = append(grpcCleanup, func() { deleteAttachmentOnPod(fcPod1, attIDIsolFc1) })
			attIDIsolFc2, overlayIP2 = createPFAttachmentAndWaitForHostIP(fcPod1, isolVNet2ID, pfMACP1Node1)
			grpcCleanup = append(grpcCleanup, func() { deleteAttachmentOnPod(fcPod1, attIDIsolFc2) })
		})

		It("should verify OVS isolation bridges for both VNets on DPU node 1", func() {
			// br-isol-<vni>-<pci> is created when that flow-controller has a PF attachment for the VNI
			// (bridgemanager), not only when CreateVirtualNetwork succeeded on that pod.
			verifyIsolationBridgeExists(fcPod1, isolVNI1, weaveDPUPortP0)
			verifyIsolationBridgeExists(fcPod1, isolVNI2, weaveDPUPortP1)
		})

		It("should create DHCP NADs and netshoot pods on worker node 1", func() {
			nadP0 := weaveDHCPNADP0
			nadP1 := weaveDHCPNADP1
			vpc.CreateDHCPNetworkAttachmentDefinition(ctx, input.client, isolTestNS, nadP0, weaveHostPFInterfaceP0, weavePFMTU, weaveContextScope.CleanupLabels)
			vpc.CreateDHCPNetworkAttachmentDefinition(ctx, input.client, isolTestNS, nadP1, weaveHostPFInterfaceP1, weavePFMTU, weaveContextScope.CleanupLabels)
			testPodConfigs = []*netshoot.TestPodConfig{
				{Namespace: isolTestNS, Name: isolPod1, NodeName: workerNode1, NADName: nadP0, Labels: weaveContextScope.CleanupLabels},
				{Namespace: isolTestNS, Name: isolPod2, NodeName: workerNode1, NADName: nadP1, Labels: weaveContextScope.CleanupLabels},
			}
			netshoot.CreatePods(ctx, input.client, testPodConfigs)
		})

		It("should verify netshoot pods are running", func() {
			netshoot.WaitForPodsReady(ctx, input.client, testPodConfigs, vpc.LongTimeout)
		})

		It("should verify overlay routes on netshoot pods", func() {
			ensureOverlayRoute(hostClusterRESTClient, input.restConfig, isolTestNS, isolPod1, overlayIP1, weaveVNetSubnet)
			ensureOverlayRoute(hostClusterRESTClient, input.restConfig, isolTestNS, isolPod2, overlayIP2, secondSubnet)
		})

		It("should add route on netshoot pod 1", func() {
			addRouteOnPodBetweenOverlayAndSubnet(hostClusterRESTClient, input.restConfig, isolTestNS, isolPod1, overlayIP1, secondSubnet)
		})

		It("should add route on netshoot pod 2", func() {
			addRouteOnPodBetweenOverlayAndSubnet(hostClusterRESTClient, input.restConfig, isolTestNS, isolPod2, overlayIP2, weaveVNetSubnet)
		})

		It("should deny ping between pods on different virtual networks on the same node", func() {
			netshoot.AssertPingFailure(&hostClusterRESTClient, &input.restConfig, isolTestNS, isolPod1, overlayIP2)
			netshoot.AssertPingFailure(&hostClusterRESTClient, &input.restConfig, isolTestNS, isolPod2, overlayIP1)
		})
	})
})
