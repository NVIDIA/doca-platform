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
	"github.com/nvidia/doca-platform/test/utils/dpuservice"
	"github.com/nvidia/doca-platform/test/utils/netshoot"
	"github.com/nvidia/doca-platform/test/utils/remotehost"
	"github.com/nvidia/doca-platform/test/utils/vpc"
	"github.com/nvidia/doca-platform/test/utils/vpc/topology"
	"github.com/nvidia/doca-platform/test/utils/vpc/topology/bf4"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("Weave Physical testcases", Labels{Domain.WeavePhysical, Domain.ZeroTrust}, Ordered, func() {
	var (
		weavePhysicalProvInput       ProvisionDPUClustersInput
		remoteWorker1, remoteWorker2 remotehost.Host
		fcPod1, fcPod2               *corev1.Pod
	)

	BeforeAll(func() {
		weaveHW = bf4.Topology
		weavePhysicalPrerequisiteScope = cleanupTracker.RegisterScope(cleanup.NamedScopeManual("weave-physical-prerequisites"))
		weavePhysicalContextScope = cleanupTracker.RegisterScope(cleanup.NamedScopeManual("weave-physical-tests"))

		for _, label := range CurrentSpecReport().Labels() {
			if label != Domain.RequiresNodes {
				continue
			}

			if !input.hasDpuNodes() {
				Skip("Skip test as there are no DPU nodes")
			}

			weavePhysicalContextScope.CleanupBefore()
			weavePhysicalPrerequisiteScope.CleanupBefore()

			weavePhysicalProvInput = getProvisionDPUClustersInputForWeave(ctx, getProvisionDPUClustersInput(), input.client)
			Expect(weavePhysicalProvInput.dpuClusters).ToNot(BeEmpty(), "no DPU clusters found via config or discovery")

			By("Waiting for DPFOperatorConfig to be ready")
			VerifyDPFOperatorConfigReady(ctx, input.client, 20*time.Minute)

			By("Creating WeavePhysical DPUFlavorTemplate, services, and DPUDeployment")
			PrepareWeavePhysicalProvisioning(ctx, input)

			By("Creating DPU cluster client for verification")
			getDPUClusterClients(ctx, weavePhysicalProvInput)
			Expect(dpuClusterClient).ToNot(BeEmpty(), "no DPU cluster clients were created")

			By("Verifying DPU cluster has ready nodes")
			VerifyDPUClusterWithNodes(ctx, weavePhysicalProvInput)

			By("Waiting for DPU cluster pods to be ready")
			VerifyClusterPods(ctx, dpuClusterClient[0], systemPodsToVerify)

			By("Waiting for WeavePhysical DPUDeployment to be ready")
			dpuservice.WaitForDPUDeploymentReady(ctx, input.client, dpfOperatorSystemNamespace,
				[]string{weavePhysicalDPUDeploymentName}, 50*time.Minute)

			By("Waiting for WeavePhysical pods on DPU cluster to be ready")
			VerifyClusterPods(ctx, dpuClusterClient[0], weavePhysicalPodsToVerify)

			By("Getting ready flow controller pods")
			flowControllerPods := netshoot.GetReadyPodsMatchingLabels(ctx, dpuClusterClient[0], dpfOperatorSystemNamespace,
				map[string]string{weaveDPUServiceLabelKey: weaveServiceFlowController}, 2)

			By("Verifying OVS is responsive on flow-controller pods")
			verifyOVSResponsive(flowControllerPods[0])
			verifyOVSResponsive(flowControllerPods[1])

			remoteWorker1, remoteWorker2, fcPod1, fcPod2 = pairRemoteWorkersToFlowControllers(
				ctx, input.client, dpuClusterClient[0], flowControllerPods,
			)
		}
	})

	AfterAll(func() {
		weavePhysicalContextScope.CleanupAfter()
		weavePhysicalPrerequisiteScope.CleanupAfter()
	})

	Context("Shared virtual network, E/W traffic on both swPlanes", Labels{Domain.RequiresNodes}, Ordered, func() {
		var (
			// hostIPWorker1/hostIPWorker1 is attachment host IPv4.
			hostIPWorker1, hostIPWorker2 [2]string
			swPlanes                     = []topology.Rail{bf4.Topology.P0, bf4.Topology.P1}
			pfSwPlanes                   []remotehost.VRFLink
			remoteWorkers                []remotehost.Host
			workerOverlayIPs             []vpc.WorkerOverlayIPs
			contextHasFailed             bool
			grpcCleanup                  []func()
			remoteCleanup                []func()
		)

		const (
			sharedVNetID = "shared-vnet"
			vni1001      = uint32(1001)
		)

		AfterEach(func() {
			if CurrentSpecReport().Failed() {
				contextHasFailed = true
			}
		})

		AfterAll(func() {
			runCleanups(contextHasFailed, grpcCleanup, remoteCleanup)
			weavePhysicalContextScope.CleanupAfter()
		})

		It("should create virtual network on both flow-controller pods", func() {
			weavePhysicalCreateVNet(fcPod1, sharedVNetID, vni1001, bf4.Topology.VNetSubnet, &grpcCleanup)
			weavePhysicalCreateVNet(fcPod2, sharedVNetID, vni1001, bf4.Topology.VNetSubnet, &grpcCleanup)
		})

		It("should create PF attachments on both DPU nodes for each sw plain", func() {
			for i, swp := range swPlanes {
				hostIPWorker1[i] = weavePhysicalCreatePFAttachment(fcPod1, sharedVNetID, swp.PFRepresentor, &grpcCleanup)
				hostIPWorker2[i] = weavePhysicalCreatePFAttachment(fcPod2, sharedVNetID, swp.PFRepresentor, &grpcCleanup)
			}
		})

		It("should start remote netutils host network container on both hosts", func() {
			remoteWorkers = []remotehost.Host{remoteWorker1, remoteWorker2}
			remotehost.SpinUpAll(remoteWorkers, fmt.Sprintf("%s:%s", netutilsImage, tag))
			remoteCleanup = append(remoteCleanup, func() {
				ifaces := []string{bf4.Topology.P0.HostPFName, bf4.Topology.P1.HostPFName}
				if len(workerOverlayIPs) > 0 {
					vpc.CleanupHosts(workerOverlayIPs, ifaces)
					return
				}
				for _, w := range remoteWorkers {
					w.Remove()
				}
			})
		})

		It("should configure remote worker PFs via systemd-networkd DHCP", func() {
			pfSwPlanes = make([]remotehost.VRFLink, len(swPlanes))
			for i, swp := range swPlanes {
				pfSwPlanes[i] = remotehost.VRFLink{Iface: swp.HostPFName, VRF: swp.VRFName, Table: swp.VRFTable}
			}
			workerOverlayIPs = []vpc.WorkerOverlayIPs{
				{Worker: remoteWorker1, IPs: []string{hostIPWorker1[0] + "/31", hostIPWorker1[1] + "/31"}, Subnet: bf4.Topology.VNetSubnet},
				{Worker: remoteWorker2, IPs: []string{hostIPWorker2[0] + "/31", hostIPWorker2[1] + "/31"}, Subnet: bf4.Topology.VNetSubnet},
			}

			remoteCleanup = append(remoteCleanup, func() {
				remotehost.RemoveVRFNetworkd(pfSwPlanes, remoteWorkers)
			})
			remotehost.ConfigureVRFDHCP(weavePFMTU, pfSwPlanes, remoteWorkers)
		})

		It("should have DHCP /31 addresses on each PF", func() {
			vpc.VerifyAttachmentsDHCPAddresses(pfSwPlanes, workerOverlayIPs)
		})

		It("should have DHCP overlay routes in each VRF table", func() {
			vpc.VerifyAttachmentsDHCPRoutes(pfSwPlanes, workerOverlayIPs, weaveUnderlayBits)
		})

		for i, swp := range swPlanes {
			It(fmt.Sprintf("should verify cross-node ping succeeds on %s", swp.VRFName), func() {
				remoteWorker1.Ping(remotehost.Net{VRF: swp.VRFName, Destination: hostIPWorker2[i]})
				remoteWorker2.Ping(remotehost.Net{VRF: swp.VRFName, Destination: hostIPWorker1[i]})
			})

			It(fmt.Sprintf("should verify iperf cross-node traffic on %s", swp.VRFName), func() {
				remotehost.RunTrafficTest(remoteWorker1, remoteWorker2, remotehost.Net{
					VRF: swp.VRFName, Destination: hostIPWorker2[i], ClientBindIP: hostIPWorker1[i],
				})
			})
		}

		It("should run ib_write_bw on rail0-swp0 and meet the BW threshold", func() {
			remotehost.RunIBWriteBW(
				remoteWorker1, remoteWorker2,
				remotehost.Net{
					VRF: bf4.Topology.P0.VRFName, Destination: hostIPWorker2[0], RDMADev: bf4.Topology.HostPFRDMADevice,
				},
				weaveIBWriteBWDuration, bf4.Topology.RDMAMinAvgBWGbit, bf4.IBWriteBWMTU,
			)
		})

		It("should run ib_write_bw on rail0-swp0 with --reversed and meet the BW threshold", func() {
			remotehost.RunIBWriteBW(
				remoteWorker1, remoteWorker2,
				remotehost.Net{
					VRF: bf4.Topology.P0.VRFName, Destination: hostIPWorker2[0], RDMADev: bf4.Topology.HostPFRDMADevice,
				},
				weaveIBWriteBWDuration, bf4.Topology.RDMAMinAvgBWGbit, bf4.IBWriteBWMTU,
				"--reversed",
			)
		})
	})

	Context("Two virtual networks, same v4 subnet different VNIs", Labels{Domain.RequiresNodes}, Ordered, func() {
		var (
			hostIPWorker1, hostIPWorker2 [2]string
			swPlanes                     = []topology.Rail{bf4.Topology.P0, bf4.Topology.P1}
			pfSwPlanes                   []remotehost.VRFLink
			remoteWorkers                []remotehost.Host
			workerOverlayIPs             []vpc.WorkerOverlayIPs
			contextHasFailed             bool
			grpcCleanup                  []func()
			remoteCleanup                []func()
		)

		const (
			vniIsolVNetID = "vni-isol-vnet"
			vni1001       = uint32(1001)
			vni1002       = uint32(1002)
		)

		AfterEach(func() {
			if CurrentSpecReport().Failed() {
				contextHasFailed = true
			}
		})

		AfterAll(func() {
			runCleanups(contextHasFailed, grpcCleanup, remoteCleanup)
			weavePhysicalContextScope.CleanupAfter()
		})

		It("should create virtual network on both flow-controller pods", func() {
			weavePhysicalCreateVNet(fcPod1, vniIsolVNetID, vni1001, bf4.Topology.VNetSubnet, &grpcCleanup)
			weavePhysicalCreateVNet(fcPod2, vniIsolVNetID, vni1002, bf4.Topology.VNetSubnet, &grpcCleanup)
		})

		It("should create PF attachments on both DPU nodes for each sw plain", func() {
			for i, swp := range swPlanes {
				hostIPWorker1[i] = weavePhysicalCreatePFAttachment(fcPod1, vniIsolVNetID, swp.PFRepresentor, &grpcCleanup)
				hostIPWorker2[i] = weavePhysicalCreatePFAttachment(fcPod2, vniIsolVNetID, swp.PFRepresentor, &grpcCleanup)
			}
		})

		It("should start remote netutils host network container on both hosts", func() {
			remoteWorkers = []remotehost.Host{remoteWorker1, remoteWorker2}
			remotehost.SpinUpAll(remoteWorkers, fmt.Sprintf("%s:%s", netutilsImage, tag))
			remoteCleanup = append(remoteCleanup, func() {
				ifaces := []string{bf4.Topology.P0.HostPFName, bf4.Topology.P1.HostPFName}
				if len(workerOverlayIPs) > 0 {
					vpc.CleanupHosts(workerOverlayIPs, ifaces)
					return
				}
				for _, w := range remoteWorkers {
					w.Remove()
				}
			})
		})

		It("should configure remote worker PFs via systemd-networkd DHCP", func() {
			pfSwPlanes = make([]remotehost.VRFLink, len(swPlanes))
			for i, swp := range swPlanes {
				pfSwPlanes[i] = remotehost.VRFLink{Iface: swp.HostPFName, VRF: swp.VRFName, Table: swp.VRFTable}
			}
			workerOverlayIPs = []vpc.WorkerOverlayIPs{
				{Worker: remoteWorker1, IPs: []string{hostIPWorker1[0] + "/31", hostIPWorker1[1] + "/31"}, Subnet: bf4.Topology.VNetSubnet},
				{Worker: remoteWorker2, IPs: []string{hostIPWorker2[0] + "/31", hostIPWorker2[1] + "/31"}, Subnet: bf4.Topology.VNetSubnet},
			}

			remoteCleanup = append(remoteCleanup, func() {
				remotehost.RemoveVRFNetworkd(pfSwPlanes, remoteWorkers)
			})
			remotehost.ConfigureVRFDHCP(weavePFMTU, pfSwPlanes, remoteWorkers)
		})

		It("should have DHCP /31 addresses on each PF", func() {
			vpc.VerifyAttachmentsDHCPAddresses(pfSwPlanes, workerOverlayIPs)
		})

		It("should have DHCP overlay routes in each VRF table", func() {
			vpc.VerifyAttachmentsDHCPRoutes(pfSwPlanes, workerOverlayIPs, weaveUnderlayBits)
		})

		for i, swp := range swPlanes {
			It(fmt.Sprintf("should deny cross-node ping on %s across different VNIs", swp.VRFName), func() {
				remoteWorker1.AssertPingFailure(remotehost.Net{VRF: swp.VRFName, Destination: hostIPWorker2[i]})
				remoteWorker2.AssertPingFailure(remotehost.Net{VRF: swp.VRFName, Destination: hostIPWorker1[i]})
			})
		}
	})

	Context("Two virtual networks, same VNI different v4 subnet", Labels{Domain.RequiresNodes}, Ordered, func() {
		var (
			hostIPWorker1, hostIPWorker2 [2]string
			swPlanes                     = []topology.Rail{bf4.Topology.P0, bf4.Topology.P1}
			pfSwPlanes                   []remotehost.VRFLink
			remoteWorkers                []remotehost.Host
			workerOverlayIPs             []vpc.WorkerOverlayIPs
			contextHasFailed             bool
			grpcCleanup                  []func()
			remoteCleanup                []func()
		)

		const (
			subnetIsolVNetID = "subnet-isol-vnet"
			vni1001          = uint32(1001)
			altSubnet        = "192.16.0.0/12"
		)

		AfterEach(func() {
			if CurrentSpecReport().Failed() {
				contextHasFailed = true
			}
		})

		AfterAll(func() {
			runCleanups(contextHasFailed, grpcCleanup, remoteCleanup)
			weavePhysicalContextScope.CleanupAfter()
		})

		It("should create virtual network on both flow-controller pods", func() {
			weavePhysicalCreateVNet(fcPod1, subnetIsolVNetID, vni1001, bf4.Topology.VNetSubnet, &grpcCleanup)
			weavePhysicalCreateVNet(fcPod2, subnetIsolVNetID, vni1001, altSubnet, &grpcCleanup)
		})

		It("should create PF attachments on both DPU nodes for each sw plain", func() {
			for i, swp := range swPlanes {
				hostIPWorker1[i] = weavePhysicalCreatePFAttachment(fcPod1, subnetIsolVNetID, swp.PFRepresentor, &grpcCleanup)
				hostIPWorker2[i] = weavePhysicalCreatePFAttachment(fcPod2, subnetIsolVNetID, swp.PFRepresentor, &grpcCleanup)
			}
		})

		It("should start remote netutils host network container on both hosts", func() {
			remoteWorkers = []remotehost.Host{remoteWorker1, remoteWorker2}
			remotehost.SpinUpAll(remoteWorkers, fmt.Sprintf("%s:%s", netutilsImage, tag))
			remoteCleanup = append(remoteCleanup, func() {
				ifaces := []string{bf4.Topology.P0.HostPFName, bf4.Topology.P1.HostPFName}
				if len(workerOverlayIPs) > 0 {
					vpc.CleanupHosts(workerOverlayIPs, ifaces)
					return
				}
				for _, w := range remoteWorkers {
					w.Remove()
				}
			})
		})

		It("should configure remote worker PFs via systemd-networkd DHCP", func() {
			pfSwPlanes = make([]remotehost.VRFLink, len(swPlanes))
			for i, swp := range swPlanes {
				pfSwPlanes[i] = remotehost.VRFLink{Iface: swp.HostPFName, VRF: swp.VRFName, Table: swp.VRFTable}
			}
			workerOverlayIPs = []vpc.WorkerOverlayIPs{
				{Worker: remoteWorker1, IPs: []string{hostIPWorker1[0] + "/31", hostIPWorker1[1] + "/31"}, Subnet: bf4.Topology.VNetSubnet},
				{Worker: remoteWorker2, IPs: []string{hostIPWorker2[0] + "/31", hostIPWorker2[1] + "/31"}, Subnet: altSubnet},
			}

			remoteCleanup = append(remoteCleanup, func() {
				remotehost.RemoveVRFNetworkd(pfSwPlanes, remoteWorkers)
			})
			remotehost.ConfigureVRFDHCP(weavePFMTU, pfSwPlanes, remoteWorkers)
		})

		It("should have DHCP /31 addresses on each PF", func() {
			vpc.VerifyAttachmentsDHCPAddresses(pfSwPlanes, workerOverlayIPs)
		})

		It("should have DHCP overlay routes in each VRF table", func() {
			vpc.VerifyAttachmentsDHCPRoutes(pfSwPlanes, workerOverlayIPs, weaveUnderlayBits)
		})

		for i, swp := range swPlanes {
			It(fmt.Sprintf("should deny cross-node ping on %s across different subnets", swp.VRFName), func() {
				remoteWorker1.AssertPingFailure(remotehost.Net{VRF: swp.VRFName, Destination: hostIPWorker2[i]})
				remoteWorker2.AssertPingFailure(remotehost.Net{VRF: swp.VRFName, Destination: hostIPWorker1[i]})
			})
		}
	})
})

// skipWeavePhysicalGRPCCleanup is true when skip-cleanup flags say to keep weave gRPC objects.
func skipWeavePhysicalGRPCCleanup(contextFailed bool) bool {
	if cleanupFlags == nil {
		return false
	}
	if cleanupFlags.SkipCleanup {
		return true
	}
	return cleanupFlags.SkipCleanupOnFailure && contextFailed
}

// runCleanupList runs fns last-in first-out.
func runCleanupList(fns []func()) {
	for i := len(fns) - 1; i >= 0; i-- {
		fns[i]()
	}
}

// runCleanups always tears down remote hosts, and skips vpcctl cleanup when skip-cleanup flags are set.
func runCleanups(contextFailed bool, grpcCleanup, remoteCleanup []func()) {
	if skipWeavePhysicalGRPCCleanup(contextFailed) {
		By("Skipping WeavePhysical vpcctl cleanup")
	} else {
		runCleanupList(grpcCleanup)
	}
	runCleanupList(remoteCleanup)
}
