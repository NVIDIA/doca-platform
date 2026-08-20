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
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("Weave testcases", Labels{Domain.Weave}, Ordered, func() {
	var (
		workerNode1, workerNode2                               string
		dpuNode1Name, dpuNode2Name                             string
		fcPod1, fcPod2                                         *corev1.Pod
		pfMACP0Node1, pfMACP0Node2, pfMACP1Node1, pfMACP1Node2 string
		beforeAllSucceeded                                     bool
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
			dpuNode1Name, dpuNode2Name = dpuNode1.Name, dpuNode2.Name
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

		AfterAll(func() {
			for i := len(grpcCleanup) - 1; i >= 0; i-- {
				grpcCleanup[i]()
			}
			weaveContextScope.CleanupAfter()
		})

		It("should create test namespace", func() {
			vpc.CreateTestNamespace(ctx, input.client, trafficTestNS, weaveContextScope.CleanupLabels)
		})

		It("should create virtual network on both flow-controller pods", func() {
			createWeaveVNetWithCleanup(&grpcCleanup, fcPod1, trafficVNetID, trafficVNI, weaveVNetSubnet)
			createWeaveVNetWithCleanup(&grpcCleanup, fcPod2, trafficVNetID, trafficVNI, weaveVNetSubnet)
		})

		It("should create PF attachments for p0 on both nodes", func() {
			overlayIPP0Node1 = createWeaveAttachmentWithCleanup(&grpcCleanup, fcPod1, trafficVNetID, pfMACP0Node1)
			overlayIPP0Node2 = createWeaveAttachmentWithCleanup(&grpcCleanup, fcPod2, trafficVNetID, pfMACP0Node2)
		})

		It("should create PF attachments for p1 on both nodes", func() {
			overlayIPP1Node1 = createWeaveAttachmentWithCleanup(&grpcCleanup, fcPod1, trafficVNetID, pfMACP1Node1)
			overlayIPP1Node2 = createWeaveAttachmentWithCleanup(&grpcCleanup, fcPod2, trafficVNetID, pfMACP1Node2)
		})

		It("should create DHCP NADs and netshoot pods on worker nodes", func() {
			createWeaveNetshootPods(trafficTestNS, []weaveNetshootEndpoint{
				{name: podP0Node1, nodeName: workerNode1, nadName: weaveDHCPNADP0, hostPF: weaveHostPFInterfaceP0},
				{name: podP0Node2, nodeName: workerNode2, nadName: weaveDHCPNADP0, hostPF: weaveHostPFInterfaceP0},
				{name: podP1Node1, nodeName: workerNode1, nadName: weaveDHCPNADP1, hostPF: weaveHostPFInterfaceP1},
				{name: podP1Node2, nodeName: workerNode2, nadName: weaveDHCPNADP1, hostPF: weaveHostPFInterfaceP1},
			})
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

		AfterAll(func() {
			for i := len(grpcCleanup) - 1; i >= 0; i-- {
				grpcCleanup[i]()
			}
			weaveContextScope.CleanupAfter()
		})

		It("should create test namespace", func() {
			vpc.CreateTestNamespace(ctx, input.client, isolTestNS, weaveContextScope.CleanupLabels)
		})

		It("should create isol-vnet-1 on both flow-controller pods", func() {
			createWeaveVNetWithCleanup(&grpcCleanup, fcPod1, isolVNet1ID, isolVNI1, weaveVNetSubnet)
			createWeaveVNetWithCleanup(&grpcCleanup, fcPod2, isolVNet1ID, isolVNI1, weaveVNetSubnet)
		})

		It("should create isol-vnet-2 on both flow-controller pods", func() {
			createWeaveVNetWithCleanup(&grpcCleanup, fcPod1, isolVNet2ID, isolVNI2, weaveVNetSubnet)
			createWeaveVNetWithCleanup(&grpcCleanup, fcPod2, isolVNet2ID, isolVNI2, weaveVNetSubnet)
		})

		It("should attach worker node 1 to vnet-1", func() {
			overlayIP1 = createWeaveAttachmentWithCleanup(&grpcCleanup, fcPod1, isolVNet1ID, pfMACP0Node1)
		})

		It("should attach worker node 2 to vnet-2", func() {
			overlayIP2 = createWeaveAttachmentWithCleanup(&grpcCleanup, fcPod2, isolVNet2ID, pfMACP0Node2)
		})

		It("should create DHCP NAD and netshoot pods on worker nodes", func() {
			createWeaveNetshootPods(isolTestNS, []weaveNetshootEndpoint{
				{name: isolPod1, nodeName: workerNode1, nadName: weaveDHCPNADP0, hostPF: weaveHostPFInterfaceP0},
				{name: isolPod2, nodeName: workerNode2, nadName: weaveDHCPNADP0, hostPF: weaveHostPFInterfaceP0},
			})
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
		)

		BeforeAll(func() {
			vpc.CreateTestNamespace(ctx, input.client, rdmaTestNS, weaveContextScope.CleanupLabels)
			CopySecretToNamespace(ctx, input.client, dpfPullSecretName, dpfOperatorSystemNamespace, rdmaTestNS, weaveContextScope.CleanupLabels)
			netutilsPod1 = createNetutilsHostPodOnNode(ctx, input.client, rdmaTestNS, rdmaPod1, workerNode1)
			netutilsPod2 = createNetutilsHostPodOnNode(ctx, input.client, rdmaTestNS, rdmaPod2, workerNode2)
		})

		AfterAll(func() {
			// Pod preStop hook flushes dhcpcd state when CleanupAfter deletes the netutils pods.
			for i := len(grpcCleanup) - 1; i >= 0; i-- {
				grpcCleanup[i]()
			}
			weaveContextScope.CleanupAfter()
		})

		It("should create vnet on both flow-controller pods", func() {
			createWeaveVNetWithCleanup(&grpcCleanup, fcPod1, rdmaVNetID, rdmaVNI, weaveVNetSubnet)
			createWeaveVNetWithCleanup(&grpcCleanup, fcPod2, rdmaVNetID, rdmaVNI, weaveVNetSubnet)
		})

		It("should create PF attachments for vnet on p0 of both nodes", func() {
			overlayIPP0Node1 = createWeaveAttachmentWithCleanup(&grpcCleanup, fcPod1, rdmaVNetID, pfMACP0Node1)
			overlayIPP0Node2 = createWeaveAttachmentWithCleanup(&grpcCleanup, fcPod2, rdmaVNetID, pfMACP0Node2)
		})

		It("should plumb overlay IPs onto worker p0 PFs via dhcpcd", func() {
			acquireDHCPLeaseInPod(hostClusterRESTClient, input.restConfig, netutilsPod1, weaveHostPFInterfaceP0, overlayIPP0Node1)
			acquireDHCPLeaseInPod(hostClusterRESTClient, input.restConfig, netutilsPod2, weaveHostPFInterfaceP0, overlayIPP0Node2)
		})

		It("should run ib_write_bw between the two hosts on p0 and meet the BW threshold", func() {
			runIBWriteBWPodToPod(hostClusterRESTClient, input.restConfig, netutilsPod2, netutilsPod1, weaveHostPFRDMADeviceP0, overlayIPP0Node2)
		})

		It("should run ib_write_bw between the two hosts on p0 with --reversed and meet the BW threshold", func() {
			// Running with --reversed checks that the RDMA traffic also works in reverse direction for sanity purposes.
			runIBWriteBWPodToPod(hostClusterRESTClient, input.restConfig, netutilsPod2, netutilsPod1, weaveHostPFRDMADeviceP0, overlayIPP0Node2, "--reversed")
		})
	})

	Context("traffic isolation between VNets on same node", Labels{Domain.RequiresNodes}, Ordered, func() {
		var (
			overlayIP1, overlayIP2 string
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

		AfterAll(func() {
			for i := len(grpcCleanup) - 1; i >= 0; i-- {
				grpcCleanup[i]()
			}
			weaveContextScope.CleanupAfter()
		})

		It("should create test namespace", func() {
			vpc.CreateTestNamespace(ctx, input.client, isolTestNS, weaveContextScope.CleanupLabels)
		})

		It("should create both isolation virtual networks on flow-controller pod 1", func() {
			createWeaveVNetWithCleanup(&grpcCleanup, fcPod1, isolVNet1ID, isolVNI1, weaveVNetSubnet)
			createWeaveVNetWithCleanup(&grpcCleanup, fcPod1, isolVNet2ID, isolVNI2, secondSubnet)
		})

		It("should create two attachments on worker node 1", func() {
			overlayIP1 = createWeaveAttachmentWithCleanup(&grpcCleanup, fcPod1, isolVNet1ID, pfMACP0Node1)
			overlayIP2 = createWeaveAttachmentWithCleanup(&grpcCleanup, fcPod1, isolVNet2ID, pfMACP1Node1)
		})

		It("should create DHCP NADs and netshoot pods on worker node 1", func() {
			createWeaveNetshootPods(isolTestNS, []weaveNetshootEndpoint{
				{name: isolPod1, nodeName: workerNode1, nadName: weaveDHCPNADP0, hostPF: weaveHostPFInterfaceP0},
				{name: isolPod2, nodeName: workerNode1, nadName: weaveDHCPNADP1, hostPF: weaveHostPFInterfaceP1},
			})
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

	// MUST BE LAST among Weave Contexts.
	// device_name is the uplink of PCI function 0, so dual underlays on the same NIC collide on one
	// label. Metrics tests therefore patch weave-flow-controller down to a single underlay and set
	// ENABLE_OVS_METRICS=true before asserting labels and counter deltas. Do not add Contexts after
	// these that still need both underlays.
	Context("flow metrics: labels and cross-node traffic", Labels{Domain.RequiresNodes}, Ordered, func() {
		var (
			overlayIPP0Node1, overlayIPP0Node2 string
			grpcCleanup                        []func()
		)

		const (
			metricsTrafficVNetID = "metrics-vnet"
			metricsTrafficVNI    = uint32(4001)
			metricsTestNS        = "weave-metrics-traffic"
			metricsPod1          = "weave-metrics-pod-1"
			metricsPod2          = "weave-metrics-pod-2"
		)

		BeforeAll(func() {
			previousUIDs := map[string]types.UID{
				dpuNode1Name: fcPod1.UID,
				dpuNode2Name: fcPod2.UID,
			}
			patchFlowControllerForMetrics()
			fcPod1, fcPod2 = waitForFlowControllerPodsRolled(dpuNode1Name, dpuNode2Name, previousUIDs)
		})

		AfterAll(func() {
			for i := len(grpcCleanup) - 1; i >= 0; i-- {
				grpcCleanup[i]()
			}
			weaveContextScope.CleanupAfter()
		})

		It("should create test namespace", func() {
			vpc.CreateTestNamespace(ctx, input.client, metricsTestNS, weaveContextScope.CleanupLabels)
		})

		It("should create virtual network on both flow-controller pods", func() {
			createWeaveVNetWithCleanup(&grpcCleanup, fcPod1, metricsTrafficVNetID, metricsTrafficVNI, weaveVNetSubnet)
			createWeaveVNetWithCleanup(&grpcCleanup, fcPod2, metricsTrafficVNetID, metricsTrafficVNI, weaveVNetSubnet)
		})

		It("should create PF attachments for p0 on both nodes", func() {
			overlayIPP0Node1 = createWeaveAttachmentWithCleanup(&grpcCleanup, fcPod1, metricsTrafficVNetID, pfMACP0Node1)
			overlayIPP0Node2 = createWeaveAttachmentWithCleanup(&grpcCleanup, fcPod2, metricsTrafficVNetID, pfMACP0Node2)
		})

		It("should expose flow metrics with the expected device_name labels", func() {
			verifyMetricDeviceNameLabels(fcPod1, fcPod2, metricsTrafficVNI)
		})

		It("should create netshoot pods on both nodes", func() {
			createWeaveNetshootPods(metricsTestNS, []weaveNetshootEndpoint{
				{name: metricsPod1, nodeName: workerNode1, nadName: weaveDHCPNADP0, hostPF: weaveHostPFInterfaceP0},
				{name: metricsPod2, nodeName: workerNode2, nadName: weaveDHCPNADP0, hostPF: weaveHostPFInterfaceP0},
			})
		})

		It("should verify overlay routes on netshoot pods", func() {
			ensureOverlayRoute(hostClusterRESTClient, input.restConfig, metricsTestNS, metricsPod1, overlayIPP0Node1, weaveVNetSubnet)
			ensureOverlayRoute(hostClusterRESTClient, input.restConfig, metricsTestNS, metricsPod2, overlayIPP0Node2, weaveVNetSubnet)
		})

		It("should verify metrics across nodes under iperf load on p0", func() {
			verifyCrossNodeIperfMetric(fcPod1, fcPod2, metricsTrafficVNI,
				metricsTestNS, metricsPod1, metricsPod2, overlayIPP0Node2)
		})
	})

	Context("flow metrics: VNI-mismatch detection", Labels{Domain.RequiresNodes}, Ordered, func() {
		var (
			overlayIP1, overlayIP2 string
			grpcCleanup            []func()
		)

		const (
			metricsIsolVNet1ID = "metrics-isol-vnet-1"
			metricsIsolVNet2ID = "metrics-isol-vnet-2"
			metricsIsolVNI1    = uint32(4002)
			metricsIsolVNI2    = uint32(4003)
			metricsTestNS      = "weave-metrics-isol"
			metricsPod1        = "weave-metrics-isol-pod-1"
			metricsPod2        = "weave-metrics-isol-pod-2"
		)

		AfterAll(func() {
			for i := len(grpcCleanup) - 1; i >= 0; i-- {
				grpcCleanup[i]()
			}
			weaveContextScope.CleanupAfter()
		})

		It("should create test namespace", func() {
			vpc.CreateTestNamespace(ctx, input.client, metricsTestNS, weaveContextScope.CleanupLabels)
		})

		It("should create isol-vnet-1 on both flow-controller pods", func() {
			createWeaveVNetWithCleanup(&grpcCleanup, fcPod1, metricsIsolVNet1ID, metricsIsolVNI1, weaveVNetSubnet)
			createWeaveVNetWithCleanup(&grpcCleanup, fcPod2, metricsIsolVNet1ID, metricsIsolVNI1, weaveVNetSubnet)
		})

		It("should create isol-vnet-2 on both flow-controller pods", func() {
			createWeaveVNetWithCleanup(&grpcCleanup, fcPod1, metricsIsolVNet2ID, metricsIsolVNI2, weaveVNetSubnet)
			createWeaveVNetWithCleanup(&grpcCleanup, fcPod2, metricsIsolVNet2ID, metricsIsolVNI2, weaveVNetSubnet)
		})

		It("should attach worker node 1 to isol-vnet-1", func() {
			overlayIP1 = createWeaveAttachmentWithCleanup(&grpcCleanup, fcPod1, metricsIsolVNet1ID, pfMACP0Node1)
		})

		It("should attach worker node 2 to isol-vnet-2", func() {
			overlayIP2 = createWeaveAttachmentWithCleanup(&grpcCleanup, fcPod2, metricsIsolVNet2ID, pfMACP0Node2)
		})

		It("should create DHCP NAD and netshoot pods on worker nodes", func() {
			createWeaveNetshootPods(metricsTestNS, []weaveNetshootEndpoint{
				{name: metricsPod1, nodeName: workerNode1, nadName: weaveDHCPNADP0, hostPF: weaveHostPFInterfaceP0},
				{name: metricsPod2, nodeName: workerNode2, nadName: weaveDHCPNADP0, hostPF: weaveHostPFInterfaceP0},
			})
		})

		It("should verify overlay routes on netshoot pods", func() {
			ensureOverlayRoute(hostClusterRESTClient, input.restConfig, metricsTestNS, metricsPod1, overlayIP1, weaveVNetSubnet)
			ensureOverlayRoute(hostClusterRESTClient, input.restConfig, metricsTestNS, metricsPod2, overlayIP2, weaveVNetSubnet)
		})

		// Source ACL must start unlearned; keep this before any deny-ping style traffic.
		It("should verify metrics for VNI-mismatch detection", func() {
			verifyVNIMismatchMetric(fcPod1, fcPod2, metricsIsolVNI1, metricsTestNS, metricsPod1, overlayIP2)
		})
	})

	Context("flow metrics: out-of-subnet drop", Labels{Domain.RequiresNodes}, Ordered, func() {
		var (
			overlayIP   string
			grpcCleanup []func()
		)

		const (
			metricsVNetID        = "metrics-oos-vnet"
			metricsVNI           = uint32(4004)
			metricsTestNS        = "weave-metrics-oos"
			metricsPod1          = "weave-metrics-oos-pod-1"
			metricsOutOfSubnet   = "20.0.0.0/8"
			metricsOutOfSubnetIP = "20.0.0.1"
		)

		AfterAll(func() {
			for i := len(grpcCleanup) - 1; i >= 0; i-- {
				grpcCleanup[i]()
			}
			weaveContextScope.CleanupAfter()
		})

		It("should create test namespace", func() {
			vpc.CreateTestNamespace(ctx, input.client, metricsTestNS, weaveContextScope.CleanupLabels)
		})

		It("should create virtual network on flow-controller pod 1", func() {
			createWeaveVNetWithCleanup(&grpcCleanup, fcPod1, metricsVNetID, metricsVNI, weaveVNetSubnet)
		})

		It("should create a PF attachment for p0 on node 1", func() {
			overlayIP = createWeaveAttachmentWithCleanup(&grpcCleanup, fcPod1, metricsVNetID, pfMACP0Node1)
		})

		It("should create a netshoot pod", func() {
			createWeaveNetshootPods(metricsTestNS, []weaveNetshootEndpoint{
				{name: metricsPod1, nodeName: workerNode1, nadName: weaveDHCPNADP0, hostPF: weaveHostPFInterfaceP0},
			})
		})

		It("should verify overlay routes on netshoot pods", func() {
			ensureOverlayRoute(hostClusterRESTClient, input.restConfig, metricsTestNS, metricsPod1, overlayIP, weaveVNetSubnet)
		})

		It("should add a foreign-subnet route", func() {
			addRouteOnPodBetweenOverlayAndSubnet(hostClusterRESTClient, input.restConfig, metricsTestNS, metricsPod1, overlayIP, metricsOutOfSubnet)
		})

		It("should deny ping to an out-of-subnet destination", func() {
			netshoot.AssertPingFailure(&hostClusterRESTClient, &input.restConfig, metricsTestNS, metricsPod1, metricsOutOfSubnetIP)
		})

		It("should verify metrics for an out-of-subnet destination", func() {
			verifyOutOfSubnetMetric(fcPod1, metricsVNI, metricsTestNS, metricsPod1, metricsOutOfSubnetIP)
		})
	})
})
