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
	"fmt"
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
		beforeAllSucceeded                                     bool
	)

	BeforeAll(func() {
		weavePrerequisiteScope = CleanupTracker.RegisterScope(cleanup.NamedScopeManual("weave-prerequisites"))
		weaveContextScope = CleanupTracker.RegisterScope(cleanup.NamedScopeManual("weave-tests"))

		for _, label := range CurrentSpecReport().Labels() {
			if label != Domain.RequiresNodes {
				continue
			}

			if !input.HasDpuNodes() {
				Skip("Skip test as there are no DPU nodes")
			}

			weavePrerequisiteScope.CleanupBefore()
			weaveContextScope.CleanupBefore()

			provInput := GetProvisionDPUClustersInputForWeave(Ctx, GetProvisionDPUClustersInput(), input.Client)
			Expect(provInput.DPUClusters).ToNot(BeEmpty(), "no DPU clusters found via config or discovery")

			By("Creating DPU cluster client for verification")
			getDPUClusterClients(Ctx, provInput)
			Expect(DPUClusterClient).ToNot(BeEmpty(), "no DPU cluster clients were created")

			By("Verifying DPU cluster has ready nodes")
			VerifyDPUClusterWithNodes(Ctx, provInput)

			By("Waiting for DPU cluster pods to be ready")
			VerifyClusterPods(Ctx, DPUClusterClient[0], systemPodsToVerify)
			By("Waiting for DPFOperatorConfig to be ready")
			VerifyDPFOperatorConfigReady(Ctx, input.Client, 20*time.Minute)

			By("Waiting for Weave pods on DPU cluster to be ready")
			VerifyClusterPods(Ctx, DPUClusterClient[0], weavePodsToVerify)

			By("Getting ready flow controller pods")
			flowControllerPods := netshoot.GetReadyPodsMatchingLabels(Ctx, DPUClusterClient[0], DPFOperatorSystemNamespace,
				map[string]string{weaveDPUServiceLabelKey: weaveFlowControllerName})
			Expect(flowControllerPods).To(HaveLen(2), "expected 2 ready %s pods", weaveFlowControllerName)

			By("Getting ready dhcp agent pods")
			dhcpAgentPods := netshoot.GetReadyPodsMatchingLabels(Ctx, DPUClusterClient[0], DPFOperatorSystemNamespace,
				map[string]string{weaveDPUServiceLabelKey: weaveDHCPAgentName})
			Expect(dhcpAgentPods).To(HaveLen(2), "expected 2 ready %s pods", weaveDHCPAgentName)

			workerNode1, workerNode2 = getTwoWorkerNodeNames(Ctx, input.Client)
			By("Getting DPU cluster nodes in order")
			dpuNode1, dpuNode2 := getDPUNodesInOrder(Ctx, input.Client, DPUClusterClient[0])
			fcPod1 = netshoot.GetPodOnNode(flowControllerPods, dpuNode1.Name)
			fcPod2 = netshoot.GetPodOnNode(flowControllerPods, dpuNode2.Name)
			Expect(fcPod1).ToNot(BeNil(), "no flow-controller pod found on DPU node %s", dpuNode1.Name)
			Expect(fcPod2).ToNot(BeNil(), "no flow-controller pod found on DPU node %s", dpuNode2.Name)

			By("Verifying OVS is responsive on flow-controller pods")
			VerifyOVSResponsive(fcPod1)
			VerifyOVSResponsive(fcPod2)

			By("Getting PF MAC addresses for p0 and p1 from DPU flow-controller pods")
			pfMACP0Node1 = GetPFMACFromFlowControllerByPort(fcPod1, weaveDPUPortP0)
			pfMACP0Node2 = GetPFMACFromFlowControllerByPort(fcPod2, weaveDPUPortP0)
			pfMACP1Node1 = GetPFMACFromFlowControllerByPort(fcPod1, weaveDPUPortP1)
			pfMACP1Node2 = GetPFMACFromFlowControllerByPort(fcPod2, weaveDPUPortP1)
		}
		beforeAllSucceeded = true
	})

	AfterAll(func() {
		if !beforeAllSucceeded {
			By("Weave: Skipping cleanup, BeforeAll did not complete successfully")
			return
		}
		weaveContextScope.CleanupAfter()
		weavePrerequisiteScope.CleanupAfter()
	})

	Context("Pre-requisite services", Labels{Domain.RequiresNodes}, Ordered, func() {
		var dhcpDS *appsv1.DaemonSet

		It("should deploy host DHCP CNI daemon", func() {
			dhcpDS = vpc.DeployDHCPDaemon(Ctx, input.Client, WeaveInput.DHCPDaemonSet, weavePrerequisiteScope.CleanupLabels)
		})

		It("should wait for DHCP daemon pods to be ready", func() {
			vpc.WaitForDHCPDaemonReady(Ctx, input.Client, dhcpDS)
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
			vpc.CreateTestNamespace(Ctx, input.Client, trafficTestNS, weaveContextScope.CleanupLabels)
		})

		It("should create virtual network on both flow-controller pods", func() {
			CreateVNetOnPod(fcPod1, trafficVNetID, trafficVNI, weaveVNetSubnet)
			grpcCleanup = append(grpcCleanup, func() { DeleteVNetOnPod(fcPod1, trafficVNetID) })
			CreateVNetOnPod(fcPod2, trafficVNetID, trafficVNI, weaveVNetSubnet)
			grpcCleanup = append(grpcCleanup, func() { DeleteVNetOnPod(fcPod2, trafficVNetID) })
		})

		It("should create PF attachments for p0 on both nodes", func() {
			var attIDP0Fc1, attIDP0Fc2 string
			attIDP0Fc1, overlayIPP0Node1 = CreatePFAttachmentAndWaitForHostIP(fcPod1, trafficVNetID, pfMACP0Node1)
			grpcCleanup = append(grpcCleanup, func() { DeleteAttachmentOnPod(fcPod1, attIDP0Fc1) })
			attIDP0Fc2, overlayIPP0Node2 = CreatePFAttachmentAndWaitForHostIP(fcPod2, trafficVNetID, pfMACP0Node2)
			grpcCleanup = append(grpcCleanup, func() { DeleteAttachmentOnPod(fcPod2, attIDP0Fc2) })
		})

		It("should create PF attachments for p1 on both nodes", func() {
			var attIDP1Fc1, attIDP1Fc2 string
			attIDP1Fc1, overlayIPP1Node1 = CreatePFAttachmentAndWaitForHostIP(fcPod1, trafficVNetID, pfMACP1Node1)
			grpcCleanup = append(grpcCleanup, func() { DeleteAttachmentOnPod(fcPod1, attIDP1Fc1) })
			attIDP1Fc2, overlayIPP1Node2 = CreatePFAttachmentAndWaitForHostIP(fcPod2, trafficVNetID, pfMACP1Node2)
			grpcCleanup = append(grpcCleanup, func() { DeleteAttachmentOnPod(fcPod2, attIDP1Fc2) })
		})

		It("should verify OVS isolation bridges exist on both DPU nodes", func() {
			VerifyIsolationBridgeExists(fcPod1, trafficVNI, weaveDPUPortP0)
			VerifyIsolationBridgeExists(fcPod1, trafficVNI, weaveDPUPortP1)
			VerifyIsolationBridgeExists(fcPod2, trafficVNI, weaveDPUPortP0)
			VerifyIsolationBridgeExists(fcPod2, trafficVNI, weaveDPUPortP1)
		})

		It("should create DHCP NADs and netshoot pods on worker nodes", func() {
			nadP0 := weaveDHCPNADP0
			nadP1 := weaveDHCPNADP1
			vpc.CreateDHCPNetworkAttachmentDefinition(Ctx, input.Client, trafficTestNS, nadP0, weaveHostPFInterfaceP0, weavePFMTU, weaveContextScope.CleanupLabels)
			vpc.CreateDHCPNetworkAttachmentDefinition(Ctx, input.Client, trafficTestNS, nadP1, weaveHostPFInterfaceP1, weavePFMTU, weaveContextScope.CleanupLabels)
			testPodConfigs = []*netshoot.TestPodConfig{
				{Namespace: trafficTestNS, Name: podP0Node1, NodeName: workerNode1, NADName: nadP0, Labels: weaveContextScope.CleanupLabels},
				{Namespace: trafficTestNS, Name: podP0Node2, NodeName: workerNode2, NADName: nadP0, Labels: weaveContextScope.CleanupLabels},
				{Namespace: trafficTestNS, Name: podP1Node1, NodeName: workerNode1, NADName: nadP1, Labels: weaveContextScope.CleanupLabels},
				{Namespace: trafficTestNS, Name: podP1Node2, NodeName: workerNode2, NADName: nadP1, Labels: weaveContextScope.CleanupLabels},
			}
			netshoot.CreatePods(Ctx, input.Client, testPodConfigs)
		})

		It("should verify netshoot pods are running", func() {
			netshoot.WaitForPodsReady(Ctx, input.Client, testPodConfigs, vpc.LongTimeout)
		})

		It("should verify overlay routes on netshoot pods", func() {
			EnsureOverlayRoute(HostClusterRESTClient, input.RestConfig, trafficTestNS, podP0Node1, overlayIPP0Node1, weaveVNetSubnet)
			EnsureOverlayRoute(HostClusterRESTClient, input.RestConfig, trafficTestNS, podP0Node2, overlayIPP0Node2, weaveVNetSubnet)
			EnsureOverlayRoute(HostClusterRESTClient, input.RestConfig, trafficTestNS, podP1Node1, overlayIPP1Node1, weaveVNetSubnet)
			EnsureOverlayRoute(HostClusterRESTClient, input.RestConfig, trafficTestNS, podP1Node2, overlayIPP1Node2, weaveVNetSubnet)
		})

		It("should verify cross-node ping succeeds on p0", func() {
			netshoot.AssertPingSuccess(&HostClusterRESTClient, &input.RestConfig, trafficTestNS, podP0Node1, overlayIPP0Node2)
			netshoot.AssertPingSuccess(&HostClusterRESTClient, &input.RestConfig, trafficTestNS, podP0Node2, overlayIPP0Node1)
		})

		It("should verify cross-node ping succeeds on p1", func() {
			netshoot.AssertPingSuccess(&HostClusterRESTClient, &input.RestConfig, trafficTestNS, podP1Node1, overlayIPP1Node2)
			netshoot.AssertPingSuccess(&HostClusterRESTClient, &input.RestConfig, trafficTestNS, podP1Node2, overlayIPP1Node1)
		})

		It("should verify performance with iperf cross-node traffic on p0", func() {
			netshoot.RunTrafficTest(&HostClusterRESTClient, &input.RestConfig, trafficTestNS, podP0Node1, podP0Node2, overlayIPP0Node2)
		})

		It("should verify performance with iperf cross-node traffic on p1", func() {
			netshoot.RunTrafficTest(&HostClusterRESTClient, &input.RestConfig, trafficTestNS, podP1Node1, podP1Node2, overlayIPP1Node2)
		})

		It("should verify metrics across nodes under iperf load on p0", func() {
			bridge := IsolationBridgeName(trafficVNI, weaveDPUPortP0)

			baselineMetricsPod1 := ReadWeaveMetrics(fcPod1)
			baselineMetricsPod2 := ReadWeaveMetrics(fcPod2)

			By("Running iperf cross-node on p0")
			iperfResult := netshoot.RunTrafficTestWithResult(&HostClusterRESTClient, &input.RestConfig, trafficTestNS, podP0Node1, podP0Node2, overlayIPP0Node2)
			forwardBytes := iperfResult.Forward.End.SumSent.Bytes
			Expect(forwardBytes).To(BeNumerically(">", 0), "iperf reported zero forward bytes")
			// iperf3 exposes no packet counter, so derive it from bytes/MSS (segment payload).
			mss := iperfResult.Forward.Start.TCPMSSDefault
			Expect(mss).To(BeNumerically(">", 0), "iperf did not report a TCP MSS")
			minIperfPackets := uint64(forwardBytes) / uint64(mss)

			By("Verifying weave metrics across nodes")
			// Poll until the OVS scrape reflects the burst.
			var currentMetricsPod1, currentMetricsPod2 WeaveMetrics
			Eventually(func(g Gomega) {
				currentMetricsPod1 = ScrapeWeaveMetrics(g, fcPod1)
				currentMetricsPod2 = ScrapeWeaveMetrics(g, fcPod2)

				// Sender encaps (host_tx/tx_sent), receiver decaps (host_rx/rx_decap), neither drop.
				AssertMetricDeltas(g, baselineMetricsPod1, currentMetricsPod1, bridge, MetricDeltaExpect{
					MustRiseBy:   map[string]uint64{weaveMetricHostTx: minIperfPackets, weaveMetricTxSent: minIperfPackets},
					MustStayFlat: []string{weaveMetricTxDropped},
				})
				AssertMetricDeltas(g, baselineMetricsPod2, currentMetricsPod2, bridge, MetricDeltaExpect{
					MustRiseBy:   map[string]uint64{weaveMetricHostRx: minIperfPackets, weaveMetricRxDecap: minIperfPackets},
					MustStayFlat: []string{weaveMetricRxDropped},
				})

				// Cross-DPU: packets encapped out of one DPU equal those decapped at the other, both directions.
				AssertMetricDeltasMatch(g,
					MetricRef{Before: baselineMetricsPod1, After: currentMetricsPod1, Bridge: bridge, Name: weaveMetricTxSent},
					MetricRef{Before: baselineMetricsPod2, After: currentMetricsPod2, Bridge: bridge, Name: weaveMetricRxDecap})
				AssertMetricDeltasMatch(g,
					MetricRef{Before: baselineMetricsPod2, After: currentMetricsPod2, Bridge: bridge, Name: weaveMetricTxSent},
					MetricRef{Before: baselineMetricsPod1, After: currentMetricsPod1, Bridge: bridge, Name: weaveMetricRxDecap})
			}).WithTimeout(weaveOperationTimeout).WithPolling(weaveEventuallyPollInterval).Should(Succeed())
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
			vpc.CreateTestNamespace(Ctx, input.Client, isolTestNS, weaveContextScope.CleanupLabels)
		})

		It("should create both isolation virtual networks on both flow-controller pods", func() {
			CreateVNetOnPod(fcPod1, isolVNet1ID, isolVNI1, weaveVNetSubnet)
			grpcCleanup = append(grpcCleanup, func() { DeleteVNetOnPod(fcPod1, isolVNet1ID) })
			CreateVNetOnPod(fcPod2, isolVNet1ID, isolVNI1, weaveVNetSubnet)
			grpcCleanup = append(grpcCleanup, func() { DeleteVNetOnPod(fcPod2, isolVNet1ID) })
			CreateVNetOnPod(fcPod1, isolVNet2ID, isolVNI2, weaveVNetSubnet)
			grpcCleanup = append(grpcCleanup, func() { DeleteVNetOnPod(fcPod1, isolVNet2ID) })
			CreateVNetOnPod(fcPod2, isolVNet2ID, isolVNI2, weaveVNetSubnet)
			grpcCleanup = append(grpcCleanup, func() { DeleteVNetOnPod(fcPod2, isolVNet2ID) })
		})

		It("should attach worker node 1 to vnet-1 and worker node 2 to vnet-2", func() {
			var attIDIsolFc1, attIDIsolFc2 string
			attIDIsolFc1, overlayIP1 = CreatePFAttachmentAndWaitForHostIP(fcPod1, isolVNet1ID, pfMACP0Node1)
			grpcCleanup = append(grpcCleanup, func() { DeleteAttachmentOnPod(fcPod1, attIDIsolFc1) })
			attIDIsolFc2, overlayIP2 = CreatePFAttachmentAndWaitForHostIP(fcPod2, isolVNet2ID, pfMACP0Node2)
			grpcCleanup = append(grpcCleanup, func() { DeleteAttachmentOnPod(fcPod2, attIDIsolFc2) })
		})

		It("should verify OVS isolation bridges exist on each DPU for its PF-attached VNet", func() {
			// br-isol-<vni>-<pci> is created when that flow-controller has a PF attachment for the VNI
			// (bridgemanager), not only when CreateVirtualNetwork succeeded on that pod.
			VerifyIsolationBridgeExists(fcPod1, isolVNI1, weaveDPUPortP0)
			VerifyIsolationBridgeExists(fcPod2, isolVNI2, weaveDPUPortP0)
		})

		It("should create DHCP NAD and netshoot pods on worker nodes", func() {
			nadName := weaveDHCPNADP0
			vpc.CreateDHCPNetworkAttachmentDefinition(Ctx, input.Client, isolTestNS, nadName, weaveHostPFInterfaceP0, weavePFMTU, weaveContextScope.CleanupLabels)
			testPodConfigs = []*netshoot.TestPodConfig{
				{Namespace: isolTestNS, Name: isolPod1, NodeName: workerNode1, NADName: nadName, Labels: weaveContextScope.CleanupLabels},
				{Namespace: isolTestNS, Name: isolPod2, NodeName: workerNode2, NADName: nadName, Labels: weaveContextScope.CleanupLabels},
			}
			netshoot.CreatePods(Ctx, input.Client, testPodConfigs)
		})

		It("should verify netshoot pods are running", func() {
			netshoot.WaitForPodsReady(Ctx, input.Client, testPodConfigs, vpc.LongTimeout)
		})

		It("should verify overlay routes on netshoot pods", func() {
			EnsureOverlayRoute(HostClusterRESTClient, input.RestConfig, isolTestNS, isolPod1, overlayIP1, weaveVNetSubnet)
			EnsureOverlayRoute(HostClusterRESTClient, input.RestConfig, isolTestNS, isolPod2, overlayIP2, weaveVNetSubnet)
		})

		// Should run before the deny-ping below so the source ACL starts unlearned.
		It("should verify metrics for VNI-mismatch detection", func() {
			srcBridge := IsolationBridgeName(isolVNI1, weaveDPUPortP0)
			dstBridge := fmt.Sprintf("br-drop-%s", dpuPortToDropNIC[weaveDPUPortP0])

			baselineMetricsPod1 := ReadWeaveMetrics(fcPod1)
			baselineMetricsPod2 := ReadWeaveMetrics(fcPod2)

			By("Sending a ping burst across mismatched VNets")
			_, _ = netshoot.PingBurst(HostClusterRESTClient, input.RestConfig, isolTestNS, isolPod1, overlayIP2, weaveMetricBurstCount)

			By("Verifying weave metrics across mismatching VNets")
			var currentMetricsPod1, currentMetricsPod2 WeaveMetrics
			Eventually(func(g Gomega) {
				currentMetricsPod1 = ScrapeWeaveMetrics(g, fcPod1)
				currentMetricsPod2 = ScrapeWeaveMetrics(g, fcPod2)

				// Source: packets enter (host_tx); some leak before the ACL lands (tx_sent), the rest drop after (tx_dropped).
				// tx_dropped omitted because it rides the learned ACL flow and resets to 0 when that flow times out (~30s).
				AssertMetricDeltas(g, baselineMetricsPod1, currentMetricsPod1, srcBridge, MetricDeltaExpect{
					MustRiseBy: map[string]uint64{weaveMetricHostTx: 1, weaveMetricTxSent: 1},
				})

				// Destination: the leaked packets are counted as VNI mismatches on the p0 drop bridge.
				AssertMetricDeltas(g, baselineMetricsPod2, currentMetricsPod2, dstBridge, MetricDeltaExpect{
					MustRiseBy: map[string]uint64{weaveMetricRxVNIMismatch: 1},
				})
			}).WithTimeout(weaveOperationTimeout).WithPolling(weaveEventuallyPollInterval).Should(Succeed())
		})

		It("should deny ping between worker nodes on different virtual networks", func() {
			netshoot.AssertPingFailure(&HostClusterRESTClient, &input.RestConfig, isolTestNS, isolPod1, overlayIP2)
			netshoot.AssertPingFailure(&HostClusterRESTClient, &input.RestConfig, isolTestNS, isolPod2, overlayIP1)
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
			vpc.CreateTestNamespace(Ctx, input.Client, rdmaTestNS, weaveContextScope.CleanupLabels)
			CopySecretToNamespace(Ctx, input.Client, DPFPullSecretName, DPFOperatorSystemNamespace, rdmaTestNS, weaveContextScope.CleanupLabels)
			netutilsPod1 = CreateNetutilsHostPodOnNode(Ctx, input.Client, rdmaTestNS, rdmaPod1, workerNode1)
			netutilsPod2 = CreateNetutilsHostPodOnNode(Ctx, input.Client, rdmaTestNS, rdmaPod2, workerNode2)
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
			CreateVNetOnPod(fcPod1, rdmaVNetID, rdmaVNI, weaveVNetSubnet)
			grpcCleanup = append(grpcCleanup, func() { DeleteVNetOnPod(fcPod1, rdmaVNetID) })
			CreateVNetOnPod(fcPod2, rdmaVNetID, rdmaVNI, weaveVNetSubnet)
			grpcCleanup = append(grpcCleanup, func() { DeleteVNetOnPod(fcPod2, rdmaVNetID) })
		})

		It("should create PF attachments for vnet on p0 of both nodes", func() {
			var attP0Fc1, attP0Fc2 string
			attP0Fc1, overlayIPP0Node1 = CreatePFAttachmentAndWaitForHostIP(fcPod1, rdmaVNetID, pfMACP0Node1)
			grpcCleanup = append(grpcCleanup, func() { DeleteAttachmentOnPod(fcPod1, attP0Fc1) })
			attP0Fc2, overlayIPP0Node2 = CreatePFAttachmentAndWaitForHostIP(fcPod2, rdmaVNetID, pfMACP0Node2)
			grpcCleanup = append(grpcCleanup, func() { DeleteAttachmentOnPod(fcPod2, attP0Fc2) })
		})

		It("should verify OVS isolation bridges for vnet on p0 of both DPUs", func() {
			VerifyIsolationBridgeExists(fcPod1, rdmaVNI, weaveDPUPortP0)
			VerifyIsolationBridgeExists(fcPod2, rdmaVNI, weaveDPUPortP0)
		})

		It("should plumb overlay IPs onto worker p0 PFs via dhcpcd", func() {
			AcquireDHCPLeaseInPod(HostClusterRESTClient, input.RestConfig, netutilsPod1, weaveHostPFInterfaceP0, overlayIPP0Node1)
			AcquireDHCPLeaseInPod(HostClusterRESTClient, input.RestConfig, netutilsPod2, weaveHostPFInterfaceP0, overlayIPP0Node2)
		})

		It("should run ib_write_bw between the two hosts on p0 and meet the BW threshold", func() {
			RunIBWriteBWPodToPod(HostClusterRESTClient, input.RestConfig, netutilsPod2, netutilsPod1, weaveHostPFRDMADeviceP0, overlayIPP0Node2)
		})

		It("should run ib_write_bw between the two hosts on p0 with --reversed and meet the BW threshold", func() {
			// Running with --reversed checks that the RDMA traffic also works in reverse direction for sanity purposes.
			RunIBWriteBWPodToPod(HostClusterRESTClient, input.RestConfig, netutilsPod2, netutilsPod1, weaveHostPFRDMADeviceP0, overlayIPP0Node2, "--reversed")
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
			vpc.CreateTestNamespace(Ctx, input.Client, isolTestNS, weaveContextScope.CleanupLabels)
		})

		It("should create both isolation virtual networks on flow-controller pod 1", func() {
			CreateVNetOnPod(fcPod1, isolVNet1ID, isolVNI1, weaveVNetSubnet)
			grpcCleanup = append(grpcCleanup, func() { DeleteVNetOnPod(fcPod1, isolVNet1ID) })
			CreateVNetOnPod(fcPod1, isolVNet2ID, isolVNI2, secondSubnet)
			grpcCleanup = append(grpcCleanup, func() { DeleteVNetOnPod(fcPod1, isolVNet2ID) })
		})

		It("should create two attachments on worker node 1", func() {
			var attIDIsolFc1, attIDIsolFc2 string
			attIDIsolFc1, overlayIP1 = CreatePFAttachmentAndWaitForHostIP(fcPod1, isolVNet1ID, pfMACP0Node1)
			grpcCleanup = append(grpcCleanup, func() { DeleteAttachmentOnPod(fcPod1, attIDIsolFc1) })
			attIDIsolFc2, overlayIP2 = CreatePFAttachmentAndWaitForHostIP(fcPod1, isolVNet2ID, pfMACP1Node1)
			grpcCleanup = append(grpcCleanup, func() { DeleteAttachmentOnPod(fcPod1, attIDIsolFc2) })
		})

		It("should verify OVS isolation bridges for both VNets on DPU node 1", func() {
			// br-isol-<vni>-<pci> is created when that flow-controller has a PF attachment for the VNI
			// (bridgemanager), not only when CreateVirtualNetwork succeeded on that pod.
			VerifyIsolationBridgeExists(fcPod1, isolVNI1, weaveDPUPortP0)
			VerifyIsolationBridgeExists(fcPod1, isolVNI2, weaveDPUPortP1)
		})

		It("should create DHCP NADs and netshoot pods on worker node 1", func() {
			nadP0 := weaveDHCPNADP0
			nadP1 := weaveDHCPNADP1
			vpc.CreateDHCPNetworkAttachmentDefinition(Ctx, input.Client, isolTestNS, nadP0, weaveHostPFInterfaceP0, weavePFMTU, weaveContextScope.CleanupLabels)
			vpc.CreateDHCPNetworkAttachmentDefinition(Ctx, input.Client, isolTestNS, nadP1, weaveHostPFInterfaceP1, weavePFMTU, weaveContextScope.CleanupLabels)
			testPodConfigs = []*netshoot.TestPodConfig{
				{Namespace: isolTestNS, Name: isolPod1, NodeName: workerNode1, NADName: nadP0, Labels: weaveContextScope.CleanupLabels},
				{Namespace: isolTestNS, Name: isolPod2, NodeName: workerNode1, NADName: nadP1, Labels: weaveContextScope.CleanupLabels},
			}
			netshoot.CreatePods(Ctx, input.Client, testPodConfigs)
		})

		It("should verify netshoot pods are running", func() {
			netshoot.WaitForPodsReady(Ctx, input.Client, testPodConfigs, vpc.LongTimeout)
		})

		It("should verify overlay routes on netshoot pods", func() {
			EnsureOverlayRoute(HostClusterRESTClient, input.RestConfig, isolTestNS, isolPod1, overlayIP1, weaveVNetSubnet)
			EnsureOverlayRoute(HostClusterRESTClient, input.RestConfig, isolTestNS, isolPod2, overlayIP2, secondSubnet)
		})

		It("should add route on netshoot pod 1", func() {
			AddRouteOnPodBetweenOverlayAndSubnet(HostClusterRESTClient, input.RestConfig, isolTestNS, isolPod1, overlayIP1, secondSubnet)
		})

		It("should add route on netshoot pod 2", func() {
			AddRouteOnPodBetweenOverlayAndSubnet(HostClusterRESTClient, input.RestConfig, isolTestNS, isolPod2, overlayIP2, weaveVNetSubnet)
		})

		It("should deny ping between pods on different virtual networks on the same node", func() {
			netshoot.AssertPingFailure(&HostClusterRESTClient, &input.RestConfig, isolTestNS, isolPod1, overlayIP2)
			netshoot.AssertPingFailure(&HostClusterRESTClient, &input.RestConfig, isolTestNS, isolPod2, overlayIP1)
		})

		It("should verify metrics for an out-of-subnet destination", func() {
			bridge := IsolationBridgeName(isolVNI1, weaveDPUPortP0)
			baselineMetrics := ReadWeaveMetrics(fcPod1)

			By("Sending a ping burst to an out-of-subnet destination")
			_, _ = netshoot.PingBurst(HostClusterRESTClient, input.RestConfig, isolTestNS, isolPod1, overlayIP2, weaveMetricBurstCount)

			By("Verifying weave metrics across out-of-subnet destination")
			// Poll until the OVS scrape reflects the burst.
			var currentMetrics WeaveMetrics
			Eventually(func(g Gomega) {
				currentMetrics = ScrapeWeaveMetrics(g, fcPod1)
				// tx_sent stays flat because an out-of-subnet destination is dropped before it is reached.
				AssertMetricDeltas(g, baselineMetrics, currentMetrics, bridge, MetricDeltaExpect{
					MustRiseBy:   map[string]uint64{weaveMetricHostTx: 1, weaveMetricTxDropped: 1},
					MustStayFlat: []string{weaveMetricTxSent},
				})
				AssertTxPacketsAccountedFor(g, baselineMetrics, currentMetrics, bridge)
			}).WithTimeout(weaveOperationTimeout).WithPolling(weaveEventuallyPollInterval).Should(Succeed())
		})
	})
})
