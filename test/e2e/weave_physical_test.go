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
	"k8s.io/apimachinery/pkg/types"
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

	Context("Shared v4 virtual network, E/W traffic on both swPlanes", Labels{Domain.RequiresNodes}, Ordered, func() {
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
			weavePhysicalCreateVNet(fcPod1, sharedVNetID, vni1001, bf4.Topology.VNetSubnet, remotehost.IPFamilyV4, &grpcCleanup)
			weavePhysicalCreateVNet(fcPod2, sharedVNetID, vni1001, bf4.Topology.VNetSubnet, remotehost.IPFamilyV4, &grpcCleanup)
		})

		It("should create PF attachments on both DPU nodes for each sw plain", func() {
			for i, swp := range swPlanes {
				hostIPWorker1[i] = weavePhysicalCreatePFAttachment(fcPod1, sharedVNetID, swp.PFRepresentor, remotehost.IPFamilyV4, &grpcCleanup)
				hostIPWorker2[i] = weavePhysicalCreatePFAttachment(fcPod2, sharedVNetID, swp.PFRepresentor, remotehost.IPFamilyV4, &grpcCleanup)
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
			vpc.VerifyAttachmentsDHCPRoutes(pfSwPlanes, workerOverlayIPs, weaveUnderlayBitsIPv4)
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
			weavePhysicalCreateVNet(fcPod1, vniIsolVNetID, vni1001, bf4.Topology.VNetSubnet, remotehost.IPFamilyV4, &grpcCleanup)
			weavePhysicalCreateVNet(fcPod2, vniIsolVNetID, vni1002, bf4.Topology.VNetSubnet, remotehost.IPFamilyV4, &grpcCleanup)
		})

		It("should create PF attachments on both DPU nodes for each sw plain", func() {
			for i, swp := range swPlanes {
				hostIPWorker1[i] = weavePhysicalCreatePFAttachment(fcPod1, vniIsolVNetID, swp.PFRepresentor, remotehost.IPFamilyV4, &grpcCleanup)
				hostIPWorker2[i] = weavePhysicalCreatePFAttachment(fcPod2, vniIsolVNetID, swp.PFRepresentor, remotehost.IPFamilyV4, &grpcCleanup)
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
			vpc.VerifyAttachmentsDHCPRoutes(pfSwPlanes, workerOverlayIPs, weaveUnderlayBitsIPv4)
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
			weavePhysicalCreateVNet(fcPod1, subnetIsolVNetID, vni1001, bf4.Topology.VNetSubnet, remotehost.IPFamilyV4, &grpcCleanup)
			weavePhysicalCreateVNet(fcPod2, subnetIsolVNetID, vni1001, altSubnet, remotehost.IPFamilyV4, &grpcCleanup)
		})

		It("should create PF attachments on both DPU nodes for each sw plain", func() {
			for i, swp := range swPlanes {
				hostIPWorker1[i] = weavePhysicalCreatePFAttachment(fcPod1, subnetIsolVNetID, swp.PFRepresentor, remotehost.IPFamilyV4, &grpcCleanup)
				hostIPWorker2[i] = weavePhysicalCreatePFAttachment(fcPod2, subnetIsolVNetID, swp.PFRepresentor, remotehost.IPFamilyV4, &grpcCleanup)
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
			vpc.VerifyAttachmentsDHCPRoutes(pfSwPlanes, workerOverlayIPs, weaveUnderlayBitsIPv4)
		})

		// Each worker only learns routes inside its own subnet, so without these the ping below
		// fails locally and never reaches the DPU that is meant to drop it.
		It("should route the peer subnet out the overlay PF on both workers", func() {
			for i, link := range pfSwPlanes {
				remoteCleanup = append(remoteCleanup, func() {
					remoteWorker1.DeleteVRFRoute(link, altSubnet)
					remoteWorker2.DeleteVRFRoute(link, bf4.Topology.VNetSubnet)
				})
				remoteWorker1.ReplaceVRFRoute(link, altSubnet, overlayGatewayIPv4(hostIPWorker1[i]))
				remoteWorker2.ReplaceVRFRoute(link, bf4.Topology.VNetSubnet, overlayGatewayIPv4(hostIPWorker2[i]))
			}
		})

		for i, swp := range swPlanes {
			It(fmt.Sprintf("should deny cross-node ping on %s across different subnets", swp.VRFName), func() {
				remoteWorker1.AssertPingFailure(remotehost.Net{VRF: swp.VRFName, Destination: hostIPWorker2[i]})
				remoteWorker2.AssertPingFailure(remotehost.Net{VRF: swp.VRFName, Destination: hostIPWorker1[i]})
			})
		}
	})

	// The flow metrics Contexts below keep both underlay interfaces: BF4 rails sit on distinct PCI
	// devices, so the device_name label is unambiguous per bridge. They only attach rail0-swp0.
	Context("flow metrics: labels and cross-node traffic", Labels{Domain.RequiresNodes}, Ordered, func() {
		var (
			hostIPWorker1, hostIPWorker2 string
			swPlane                      = bf4.Topology.P0
			pfSwPlanes                   []remotehost.VRFLink
			remoteWorkers                []remotehost.Host
			workerOverlayIPs             []vpc.WorkerOverlayIPs
			contextHasFailed             bool
			grpcCleanup                  []func()
			remoteCleanup                []func()
		)

		const (
			metricsTrafficVNetID = "metrics-vnet"
			metricsTrafficVNI    = uint32(4001)
		)

		BeforeAll(func() {
			previousUIDs := map[string]types.UID{
				fcPod1.Spec.NodeName: fcPod1.UID,
				fcPod2.Spec.NodeName: fcPod2.UID,
			}
			patchFlowControllerConfiguration(ctx, input.client, weaveServiceFlowController, flowControllerPatch{
				Env: map[string]string{
					weaveEnableOVSMetricsEnv: weaveEnableOVSMetricsEnvValue,
				},
			})
			fcPod1, fcPod2 = waitForFlowControllerPodsRolled(ctx, fcPod1.Spec.NodeName, fcPod2.Spec.NodeName, previousUIDs,
				weaveServiceFlowController, map[string]string{
					weaveEnableOVSMetricsEnv: weaveEnableOVSMetricsEnvValue,
				})
		})

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
			weavePhysicalCreateVNet(fcPod1, metricsTrafficVNetID, metricsTrafficVNI, bf4.Topology.VNetSubnet, remotehost.IPFamilyV4, &grpcCleanup)
			weavePhysicalCreateVNet(fcPod2, metricsTrafficVNetID, metricsTrafficVNI, bf4.Topology.VNetSubnet, remotehost.IPFamilyV4, &grpcCleanup)
		})

		It("should create a PF attachment on both DPU nodes for rail0-swp0", func() {
			hostIPWorker1 = weavePhysicalCreatePFAttachment(fcPod1, metricsTrafficVNetID, swPlane.PFRepresentor, remotehost.IPFamilyV4, &grpcCleanup)
			hostIPWorker2 = weavePhysicalCreatePFAttachment(fcPod2, metricsTrafficVNetID, swPlane.PFRepresentor, remotehost.IPFamilyV4, &grpcCleanup)
		})

		It("should expose flow metrics with the expected device_name labels", func() {
			verifyMetricDeviceNameLabels(fcPod1, fcPod2, metricsTrafficVNI)
		})

		It("should start remote netutils host network container on both hosts", func() {
			remoteWorkers = []remotehost.Host{remoteWorker1, remoteWorker2}
			remotehost.SpinUpAll(remoteWorkers, fmt.Sprintf("%s:%s", netutilsImage, tag))
			remoteCleanup = append(remoteCleanup, func() {
				ifaces := []string{swPlane.HostPFName}
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
			pfSwPlanes = []remotehost.VRFLink{
				{
					Iface: swPlane.HostPFName,
					VRF:   swPlane.VRFName,
					Table: swPlane.VRFTable,
				},
			}
			workerOverlayIPs = []vpc.WorkerOverlayIPs{
				{
					Worker: remoteWorker1,
					IPs:    []string{hostIPWorker1 + "/31"},
					Subnet: bf4.Topology.VNetSubnet,
				},
				{
					Worker: remoteWorker2,
					IPs:    []string{hostIPWorker2 + "/31"},
					Subnet: bf4.Topology.VNetSubnet,
				},
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
			vpc.VerifyAttachmentsDHCPRoutes(pfSwPlanes, workerOverlayIPs, weaveUnderlayBitsIPv4)
		})

		It("should verify cross-node ping succeeds on rail0-swp0", func() {
			remoteWorker1.Ping(remotehost.Net{
				VRF:         swPlane.VRFName,
				Destination: hostIPWorker2,
			})
			remoteWorker2.Ping(remotehost.Net{
				VRF:         swPlane.VRFName,
				Destination: hostIPWorker1,
			})
		})

		It("should verify metrics across nodes under iperf load on rail0-swp0", func() {
			verifyCrossNodeIperfMetric(fcPod1, fcPod2, metricsTrafficVNI, func() netshoot.IperfResult {
				return remotehost.RunTrafficTestWithResult(remoteWorker1, remoteWorker2, remotehost.Net{
					VRF:          swPlane.VRFName,
					Destination:  hostIPWorker2,
					ClientBindIP: hostIPWorker1,
				}).Forward
			})
		})
	})

	Context("flow metrics: VNI-mismatch detection", Labels{Domain.RequiresNodes}, Ordered, func() {
		var (
			hostIPWorker1, hostIPWorker2 string
			swPlane                      = bf4.Topology.P0
			pfSwPlanes                   []remotehost.VRFLink
			remoteWorkers                []remotehost.Host
			workerOverlayIPs             []vpc.WorkerOverlayIPs
			contextHasFailed             bool
			grpcCleanup                  []func()
			remoteCleanup                []func()
		)

		const (
			metricsIsolVNet1ID = "metrics-isol-vnet-1"
			metricsIsolVNet2ID = "metrics-isol-vnet-2"
			metricsIsolVNI1    = uint32(4002)
			metricsIsolVNI2    = uint32(4003)
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

		It("should create a virtual network with a different VNI on each flow-controller pod", func() {
			weavePhysicalCreateVNet(fcPod1, metricsIsolVNet1ID, metricsIsolVNI1, bf4.Topology.VNetSubnet, remotehost.IPFamilyV4, &grpcCleanup)
			weavePhysicalCreateVNet(fcPod2, metricsIsolVNet2ID, metricsIsolVNI2, bf4.Topology.VNetSubnet, remotehost.IPFamilyV4, &grpcCleanup)
		})

		It("should create a PF attachment on both DPU nodes for rail0-swp0", func() {
			hostIPWorker1 = weavePhysicalCreatePFAttachment(fcPod1, metricsIsolVNet1ID, swPlane.PFRepresentor, remotehost.IPFamilyV4, &grpcCleanup)
			hostIPWorker2 = weavePhysicalCreatePFAttachment(fcPod2, metricsIsolVNet2ID, swPlane.PFRepresentor, remotehost.IPFamilyV4, &grpcCleanup)
		})

		It("should start remote netutils host network container on both hosts", func() {
			remoteWorkers = []remotehost.Host{remoteWorker1, remoteWorker2}
			remotehost.SpinUpAll(remoteWorkers, fmt.Sprintf("%s:%s", netutilsImage, tag))
			remoteCleanup = append(remoteCleanup, func() {
				ifaces := []string{swPlane.HostPFName}
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
			pfSwPlanes = []remotehost.VRFLink{
				{
					Iface: swPlane.HostPFName,
					VRF:   swPlane.VRFName,
					Table: swPlane.VRFTable,
				},
			}
			workerOverlayIPs = []vpc.WorkerOverlayIPs{
				{
					Worker: remoteWorker1,
					IPs:    []string{hostIPWorker1 + "/31"},
					Subnet: bf4.Topology.VNetSubnet,
				},
				{
					Worker: remoteWorker2,
					IPs:    []string{hostIPWorker2 + "/31"},
					Subnet: bf4.Topology.VNetSubnet,
				},
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
			vpc.VerifyAttachmentsDHCPRoutes(pfSwPlanes, workerOverlayIPs, weaveUnderlayBitsIPv4)
		})

		// Source ACL must start unlearned, keep this before any deny-ping style traffic.
		It("should count a VNI mismatch on the destination drop bridge", func() {
			verifyVNIMismatchMetric(fcPod1, fcPod2, metricsIsolVNI1, func() {
				remoteWorker1.PingBurst(remotehost.Net{
					VRF:         swPlane.VRFName,
					Destination: hostIPWorker2,
				}, weaveMetricBurstCount)
			})
		})
	})

	Context("flow metrics: out-of-subnet drop", Labels{Domain.RequiresNodes}, Ordered, func() {
		var (
			hostIPWorker1    string
			swPlane          = bf4.Topology.P0
			pfSwPlanes       []remotehost.VRFLink
			remoteWorkers    []remotehost.Host
			workerOverlayIPs []vpc.WorkerOverlayIPs
			contextHasFailed bool
			grpcCleanup      []func()
			remoteCleanup    []func()
		)

		const (
			metricsVNetID        = "metrics-oos-vnet"
			metricsVNI           = uint32(4004)
			metricsOutOfSubnet   = "20.0.0.0/8"
			metricsOutOfSubnetIP = "20.0.0.1"
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

		It("should create virtual network on the first flow-controller pod", func() {
			weavePhysicalCreateVNet(fcPod1, metricsVNetID, metricsVNI, bf4.Topology.VNetSubnet, remotehost.IPFamilyV4, &grpcCleanup)
		})

		It("should create a PF attachment on the first DPU node for rail0-swp0", func() {
			hostIPWorker1 = weavePhysicalCreatePFAttachment(fcPod1, metricsVNetID, swPlane.PFRepresentor, remotehost.IPFamilyV4, &grpcCleanup)
		})

		It("should start remote netutils host network container on the first host", func() {
			remoteWorkers = []remotehost.Host{remoteWorker1}
			remotehost.SpinUpAll(remoteWorkers, fmt.Sprintf("%s:%s", netutilsImage, tag))
			remoteCleanup = append(remoteCleanup, func() {
				ifaces := []string{swPlane.HostPFName}
				if len(workerOverlayIPs) > 0 {
					vpc.CleanupHosts(workerOverlayIPs, ifaces)
					return
				}
				for _, w := range remoteWorkers {
					w.Remove()
				}
			})
		})

		It("should configure the remote worker PF via systemd-networkd DHCP", func() {
			pfSwPlanes = []remotehost.VRFLink{
				{
					Iface: swPlane.HostPFName,
					VRF:   swPlane.VRFName,
					Table: swPlane.VRFTable,
				},
			}
			workerOverlayIPs = []vpc.WorkerOverlayIPs{
				{
					Worker: remoteWorker1,
					IPs:    []string{hostIPWorker1 + "/31"},
					Subnet: bf4.Topology.VNetSubnet,
				},
			}

			remoteCleanup = append(remoteCleanup, func() {
				remotehost.RemoveVRFNetworkd(pfSwPlanes, remoteWorkers)
			})
			remotehost.ConfigureVRFDHCP(weavePFMTU, pfSwPlanes, remoteWorkers)
		})

		It("should have a DHCP /31 address on the PF", func() {
			vpc.VerifyAttachmentsDHCPAddresses(pfSwPlanes, workerOverlayIPs)
		})

		It("should route an out-of-subnet destination out the overlay PF", func() {
			remoteCleanup = append(remoteCleanup, func() {
				remoteWorker1.DeleteVRFRoute(pfSwPlanes[0], metricsOutOfSubnet)
			})
			remoteWorker1.ReplaceVRFRoute(pfSwPlanes[0], metricsOutOfSubnet, overlayGatewayIPv4(hostIPWorker1))
		})

		It("should deny ping to an out-of-subnet destination", func() {
			remoteWorker1.AssertPingFailure(remotehost.Net{
				VRF:         swPlane.VRFName,
				Destination: metricsOutOfSubnetIP,
			})
		})

		It("should count out-of-subnet drops on the isolation bridge", func() {
			verifyOutOfSubnetMetric(fcPod1, metricsVNI, func() {
				remoteWorker1.PingBurst(remotehost.Net{
					VRF:         swPlane.VRFName,
					Destination: metricsOutOfSubnetIP,
				}, weaveMetricBurstCount)
			})
		})
	})

	// MUST BE LAST among WeavePhysical Contexts that need IPv4 underlay.
	Context("Switch to ipv6 underlay IP family", Labels{Domain.RequiresNodes}, Ordered, func() {
		BeforeAll(func() {
			previousUIDs := map[string]types.UID{
				fcPod1.Spec.NodeName: fcPod1.UID,
				fcPod2.Spec.NodeName: fcPod2.UID,
			}
			patchFlowControllerConfiguration(ctx, input.client, weaveServiceFlowController, flowControllerPatch{
				Env: map[string]string{
					weaveUnderlayIPFamilyEnv: weaveUnderlayIPFamilyIPv6,
				},
				PrefixBits: &weaveUnderlayBitsIPv6,
			})
			fcPod1, fcPod2 = waitForFlowControllerPodsRolled(ctx, fcPod1.Spec.NodeName, fcPod2.Spec.NodeName, previousUIDs,
				weaveServiceFlowController, map[string]string{
					weaveUnderlayIPFamilyEnv: weaveUnderlayIPFamilyIPv6,
				})
		})

		Context("Shared v6 virtual network, E/W traffic on both swPlanes", Ordered, func() {
			var (
				hostIPWorker1, hostIPWorker2 [2]string
				swPlanes                     = []topology.Rail{bf4.Topology.P0, bf4.Topology.P1}
				pfSwPlanes                   []remotehost.VRFLink
				remoteWorkers                []remotehost.Host
				workerOverlayIPs             []vpc.WorkerOverlayIPs
				contextHasFailed             bool
				failedSpec                   SpecReport
				grpcCleanup                  []func()
				remoteCleanup                []func()
			)

			const (
				ipv6VNetID = "ipv6-vnet"
				vni1001    = uint32(1001)
			)

			AfterEach(func() {
				if spec := CurrentSpecReport(); spec.Failed() {
					contextHasFailed = true
					failedSpec = spec
				}
			})

			AfterAll(func() {
				if contextHasFailed {
					By("WeavePhysical: Report failure for this spec")
					reportAfterEach(failedSpec)
				}
				runCleanups(contextHasFailed, grpcCleanup, remoteCleanup)
				weavePhysicalContextScope.CleanupAfter()
			})

			It("should create virtual network on both flow-controller pods", func() {
				weavePhysicalCreateVNet(fcPod1, ipv6VNetID, vni1001, bf4.Topology.VNetSubnetIPv6, remotehost.IPFamilyV6, &grpcCleanup)
				weavePhysicalCreateVNet(fcPod2, ipv6VNetID, vni1001, bf4.Topology.VNetSubnetIPv6, remotehost.IPFamilyV6, &grpcCleanup)
			})

			It("should create PF attachments on both DPU nodes for each sw plain", func() {
				for _, swp := range swPlanes {
					weavePhysicalCreatePFAttachment(fcPod1, ipv6VNetID, swp.PFRepresentor, remotehost.IPFamilyV6, &grpcCleanup)
					weavePhysicalCreatePFAttachment(fcPod2, ipv6VNetID, swp.PFRepresentor, remotehost.IPFamilyV6, &grpcCleanup)
				}
			})

			It("should start remote netutils workers on both hosts", func() {
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

			It("should configure remote worker PFs via systemd-networkd SLAAC", func() {
				pfSwPlanes = make([]remotehost.VRFLink, len(swPlanes))
				for i, swp := range swPlanes {
					pfSwPlanes[i] = remotehost.VRFLink{
						Iface:  swp.HostPFName,
						VRF:    swp.VRFName,
						Table:  swp.VRFTable,
						Family: remotehost.IPFamilyV6,
					}
				}
				workerOverlayIPs = []vpc.WorkerOverlayIPs{
					{
						Worker: remoteWorker1,
						Subnet: bf4.Topology.VNetSubnetIPv6,
						Family: remotehost.IPFamilyV6,
					},
					{
						Worker: remoteWorker2,
						Subnet: bf4.Topology.VNetSubnetIPv6,
						Family: remotehost.IPFamilyV6,
					},
				}

				remoteCleanup = append(remoteCleanup, func() {
					remotehost.RemoveVRFNetworkd(pfSwPlanes, remoteWorkers)
				})
				remotehost.ConfigureVRFDHCP(weavePFMTU, pfSwPlanes, remoteWorkers)
			})

			It("should have SLAAC overlay addresses on each PF", func() {
				cidrsWorker1 := make([]string, len(pfSwPlanes))
				cidrsWorker2 := make([]string, len(pfSwPlanes))
				for i, link := range pfSwPlanes {
					hostIPWorker1[i], cidrsWorker1[i] = vpc.WaitGlobalOverlayIP(remoteWorker1, link.Iface, bf4.Topology.VNetSubnetIPv6)
					hostIPWorker2[i], cidrsWorker2[i] = vpc.WaitGlobalOverlayIP(remoteWorker2, link.Iface, bf4.Topology.VNetSubnetIPv6)
				}
				workerOverlayIPs[0].IPs = cidrsWorker1
				workerOverlayIPs[1].IPs = cidrsWorker2
			})

			It("should have RA overlay routes in each VRF table", func() {
				vpc.VerifyAttachmentsDHCPRoutes(pfSwPlanes, workerOverlayIPs, weaveUnderlayBitsIPv6)
			})

			for i, swp := range swPlanes {
				It(fmt.Sprintf("should verify cross-node ping succeeds on %s", swp.VRFName), func() {
					remoteWorker1.Ping(remotehost.Net{
						VRF:         swp.VRFName,
						Destination: hostIPWorker2[i],
						Family:      remotehost.IPFamilyV6,
					})
					remoteWorker2.Ping(remotehost.Net{
						VRF:         swp.VRFName,
						Destination: hostIPWorker1[i],
						Family:      remotehost.IPFamilyV6,
					})
				})

				It(fmt.Sprintf("should verify iperf cross-node traffic on %s", swp.VRFName), func() {
					remotehost.RunTrafficTest(remoteWorker1, remoteWorker2, remotehost.Net{
						VRF:          swp.VRFName,
						Destination:  hostIPWorker2[i],
						ClientBindIP: hostIPWorker1[i],
						Family:       remotehost.IPFamilyV6,
					})
				})
			}

			It("should run ib_write_bw on rail0 and meet the BW threshold", func() {
				remotehost.RunIBWriteBW(
					remoteWorker1, remoteWorker2,
					remotehost.Net{
						VRF:         bf4.Topology.P0.VRFName,
						Destination: hostIPWorker2[0],
						RDMADev:     bf4.Topology.HostPFRDMADevice,
						Family:      remotehost.IPFamilyV6,
					},
					weaveIBWriteBWDuration, bf4.Topology.RDMAMinAvgBWGbit, bf4.IBWriteBWMTU,
				)
			})

			It("should run ib_write_bw on rail0 with --reversed and meet the BW threshold", func() {
				remotehost.RunIBWriteBW(
					remoteWorker1, remoteWorker2,
					remotehost.Net{
						VRF:         bf4.Topology.P0.VRFName,
						Destination: hostIPWorker2[0],
						RDMADev:     bf4.Topology.HostPFRDMADevice,
						Family:      remotehost.IPFamilyV6,
					},
					weaveIBWriteBWDuration, bf4.Topology.RDMAMinAvgBWGbit, bf4.IBWriteBWMTU,
					"--reversed",
				)
			})
		})

		Context("Two virtual networks, same v6 subnet different VNIs", Ordered, func() {
			var (
				hostIPWorker1, hostIPWorker2 [2]string
				swPlanes                     = []topology.Rail{bf4.Topology.P0, bf4.Topology.P1}
				pfSwPlanes                   []remotehost.VRFLink
				remoteWorkers                []remotehost.Host
				workerOverlayIPs             []vpc.WorkerOverlayIPs
				contextHasFailed             bool
				failedSpec                   SpecReport
				grpcCleanup                  []func()
				remoteCleanup                []func()
			)

			const (
				vniIsolVNetID = "vni-isol-vnet-v6"
				vni1001       = uint32(1001)
				vni1002       = uint32(1002)
			)

			AfterEach(func() {
				if spec := CurrentSpecReport(); spec.Failed() {
					contextHasFailed = true
					failedSpec = spec
				}
			})

			AfterAll(func() {
				if contextHasFailed {
					By("WeavePhysical: Report failure for this spec")
					reportAfterEach(failedSpec)
				}
				runCleanups(contextHasFailed, grpcCleanup, remoteCleanup)
				weavePhysicalContextScope.CleanupAfter()
			})

			It("should create virtual network on both flow-controller pods", func() {
				weavePhysicalCreateVNet(fcPod1, vniIsolVNetID, vni1001, bf4.Topology.VNetSubnetIPv6, remotehost.IPFamilyV6, &grpcCleanup)
				weavePhysicalCreateVNet(fcPod2, vniIsolVNetID, vni1002, bf4.Topology.VNetSubnetIPv6, remotehost.IPFamilyV6, &grpcCleanup)
			})

			It("should create PF attachments on both DPU nodes for each sw plain", func() {
				for _, swp := range swPlanes {
					weavePhysicalCreatePFAttachment(fcPod1, vniIsolVNetID, swp.PFRepresentor, remotehost.IPFamilyV6, &grpcCleanup)
					weavePhysicalCreatePFAttachment(fcPod2, vniIsolVNetID, swp.PFRepresentor, remotehost.IPFamilyV6, &grpcCleanup)
				}
			})

			It("should start remote netutils workers on both hosts", func() {
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

			It("should configure remote worker PFs via systemd-networkd SLAAC", func() {
				pfSwPlanes = make([]remotehost.VRFLink, len(swPlanes))
				for i, swp := range swPlanes {
					pfSwPlanes[i] = remotehost.VRFLink{
						Iface:  swp.HostPFName,
						VRF:    swp.VRFName,
						Table:  swp.VRFTable,
						Family: remotehost.IPFamilyV6,
					}
				}
				workerOverlayIPs = []vpc.WorkerOverlayIPs{
					{
						Worker: remoteWorker1,
						Subnet: bf4.Topology.VNetSubnetIPv6,
						Family: remotehost.IPFamilyV6,
					},
					{
						Worker: remoteWorker2,
						Subnet: bf4.Topology.VNetSubnetIPv6,
						Family: remotehost.IPFamilyV6,
					},
				}

				remoteCleanup = append(remoteCleanup, func() {
					remotehost.RemoveVRFNetworkd(pfSwPlanes, remoteWorkers)
				})
				remotehost.ConfigureVRFDHCP(weavePFMTU, pfSwPlanes, remoteWorkers)
			})

			It("should have SLAAC overlay addresses on each PF", func() {
				cidrsWorker1 := make([]string, len(pfSwPlanes))
				cidrsWorker2 := make([]string, len(pfSwPlanes))
				for i, link := range pfSwPlanes {
					hostIPWorker1[i], cidrsWorker1[i] = vpc.WaitGlobalOverlayIP(remoteWorker1, link.Iface, bf4.Topology.VNetSubnetIPv6)
					hostIPWorker2[i], cidrsWorker2[i] = vpc.WaitGlobalOverlayIP(remoteWorker2, link.Iface, bf4.Topology.VNetSubnetIPv6)
				}
				workerOverlayIPs[0].IPs = cidrsWorker1
				workerOverlayIPs[1].IPs = cidrsWorker2
			})

			It("should have RA overlay routes in each VRF table", func() {
				vpc.VerifyAttachmentsDHCPRoutes(pfSwPlanes, workerOverlayIPs, weaveUnderlayBitsIPv6)
			})

			for i, swp := range swPlanes {
				It(fmt.Sprintf("should deny cross-node ping on %s across different VNIs", swp.VRFName), func() {
					remoteWorker1.AssertPingFailure(remotehost.Net{
						VRF:         swp.VRFName,
						Destination: hostIPWorker2[i],
						Family:      remotehost.IPFamilyV6,
					})
					remoteWorker2.AssertPingFailure(remotehost.Net{
						VRF:         swp.VRFName,
						Destination: hostIPWorker1[i],
						Family:      remotehost.IPFamilyV6,
					})
				})
			}
		})

		Context("Two virtual networks, same VNI different v6 subnet", Ordered, func() {
			var (
				hostIPWorker1, hostIPWorker2 [2]string
				swPlanes                     = []topology.Rail{bf4.Topology.P0, bf4.Topology.P1}
				pfSwPlanes                   []remotehost.VRFLink
				remoteWorkers                []remotehost.Host
				workerOverlayIPs             []vpc.WorkerOverlayIPs
				contextHasFailed             bool
				failedSpec                   SpecReport
				grpcCleanup                  []func()
				remoteCleanup                []func()
			)

			const (
				subnetIsolVNetID = "subnet-isol-vnet-v6"
				vni1001          = uint32(1001)
				// altSubnetIPv6 matches VNetSubnetIPv6 in prefix length, only the overlay network differs.
				altSubnetIPv6 = "f002::/24"
			)

			AfterEach(func() {
				if spec := CurrentSpecReport(); spec.Failed() {
					contextHasFailed = true
					failedSpec = spec
				}
			})

			AfterAll(func() {
				if contextHasFailed {
					By("WeavePhysical: Report failure for this spec")
					reportAfterEach(failedSpec)
				}
				runCleanups(contextHasFailed, grpcCleanup, remoteCleanup)
				weavePhysicalContextScope.CleanupAfter()
			})

			It("should create virtual network on both flow-controller pods", func() {
				weavePhysicalCreateVNet(fcPod1, subnetIsolVNetID, vni1001, bf4.Topology.VNetSubnetIPv6, remotehost.IPFamilyV6, &grpcCleanup)
				weavePhysicalCreateVNet(fcPod2, subnetIsolVNetID, vni1001, altSubnetIPv6, remotehost.IPFamilyV6, &grpcCleanup)
			})

			It("should create PF attachments on both DPU nodes for each sw plain", func() {
				for _, swp := range swPlanes {
					weavePhysicalCreatePFAttachment(fcPod1, subnetIsolVNetID, swp.PFRepresentor, remotehost.IPFamilyV6, &grpcCleanup)
					weavePhysicalCreatePFAttachment(fcPod2, subnetIsolVNetID, swp.PFRepresentor, remotehost.IPFamilyV6, &grpcCleanup)
				}
			})

			It("should start remote netutils workers on both hosts", func() {
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

			It("should configure remote worker PFs via systemd-networkd SLAAC", func() {
				pfSwPlanes = make([]remotehost.VRFLink, len(swPlanes))
				for i, swp := range swPlanes {
					pfSwPlanes[i] = remotehost.VRFLink{
						Iface:  swp.HostPFName,
						VRF:    swp.VRFName,
						Table:  swp.VRFTable,
						Family: remotehost.IPFamilyV6,
					}
				}
				workerOverlayIPs = []vpc.WorkerOverlayIPs{
					{
						Worker: remoteWorker1,
						Subnet: bf4.Topology.VNetSubnetIPv6,
						Family: remotehost.IPFamilyV6,
					},
					{
						Worker: remoteWorker2,
						Subnet: altSubnetIPv6,
						Family: remotehost.IPFamilyV6,
					},
				}

				remoteCleanup = append(remoteCleanup, func() {
					remotehost.RemoveVRFNetworkd(pfSwPlanes, remoteWorkers)
				})
				remotehost.ConfigureVRFDHCP(weavePFMTU, pfSwPlanes, remoteWorkers)
			})

			It("should have SLAAC overlay addresses on each PF", func() {
				cidrsWorker1 := make([]string, len(pfSwPlanes))
				cidrsWorker2 := make([]string, len(pfSwPlanes))
				for i, link := range pfSwPlanes {
					hostIPWorker1[i], cidrsWorker1[i] = vpc.WaitGlobalOverlayIP(remoteWorker1, link.Iface, bf4.Topology.VNetSubnetIPv6)
					hostIPWorker2[i], cidrsWorker2[i] = vpc.WaitGlobalOverlayIP(remoteWorker2, link.Iface, altSubnetIPv6)
				}
				workerOverlayIPs[0].IPs = cidrsWorker1
				workerOverlayIPs[1].IPs = cidrsWorker2
			})

			It("should have RA overlay routes in each VRF table", func() {
				vpc.VerifyAttachmentsDHCPRoutes(pfSwPlanes, workerOverlayIPs, weaveUnderlayBitsIPv6)
			})

			// Each worker only learns routes inside its own subnet, so without these the ping below
			// fails locally and never reaches the DPU that is meant to drop it.
			It("should route the peer subnet out the overlay PF on both workers", func() {
				for _, link := range pfSwPlanes {
					gateway1 := remoteWorker1.VRFRouteNextHop(link)
					gateway2 := remoteWorker2.VRFRouteNextHop(link)
					remoteCleanup = append(remoteCleanup, func() {
						remoteWorker1.DeleteVRFRoute(link, altSubnetIPv6)
						remoteWorker2.DeleteVRFRoute(link, bf4.Topology.VNetSubnetIPv6)
					})
					remoteWorker1.ReplaceVRFRoute(link, altSubnetIPv6, gateway1)
					remoteWorker2.ReplaceVRFRoute(link, bf4.Topology.VNetSubnetIPv6, gateway2)
				}
			})

			for i, swp := range swPlanes {
				It(fmt.Sprintf("should deny cross-node ping on %s across different subnets", swp.VRFName), func() {
					remoteWorker1.AssertPingFailure(remotehost.Net{
						VRF:         swp.VRFName,
						Destination: hostIPWorker2[i],
						Family:      remotehost.IPFamilyV6,
					})
					remoteWorker2.AssertPingFailure(remotehost.Net{
						VRF:         swp.VRFName,
						Destination: hostIPWorker1[i],
						Family:      remotehost.IPFamilyV6,
					})
				})
			}
		})
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
