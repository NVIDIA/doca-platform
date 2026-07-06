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
	"fmt"
	"strings"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/test/e2e/cleanup"
	"github.com/nvidia/doca-platform/test/utils/dpuservice"
	"github.com/nvidia/doca-platform/test/utils/netshoot"
	vpcutils "github.com/nvidia/doca-platform/test/utils/vpc"
	ovnutils "github.com/nvidia/doca-platform/test/utils/vpc/ovn"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

const nadNamePrefix = "nad-"

func VPCOVNBeforeSuite() {
	By("Setting VPC OVN configs for the test")
	vpcOvnInput.applyVPCOVNConfig(*conf)
}

//nolint:dupl
var _ = Describe("VPC OVN testcases", Labels{Domain.DPFSystem, Domain.DPFVPCOVN}, Ordered, func() {
	var (
		dpuNode1 corev1.Node
		dpuNode2 corev1.Node
	)

	BeforeAll(func() {
		// Register scopes for VPC OVN tests (using global scope variables from vpcovn.go)
		vpcPrerequisiteScope = cleanupTracker.RegisterScope(cleanup.NamedScopeManual("vpc-ovn-prerequisites"))
		vpcOvnContextScope = cleanupTracker.RegisterScope(cleanup.NamedScopeManual("vpc-ovn-tests"))

		for _, label := range CurrentSpecReport().Labels() {
			if label != Domain.RequiresNodes {
				continue
			}

			if !input.HasDpuNodes() {
				Skip("Skip test as there are not multiple nodes")
			}

			// Provisioning is skipped if the test is labels with !Domain.Provisioning
			if !strings.Contains(GinkgoLabelFilter(), "!"+Domain.Provisioning) {
				By("Waiting for provisioning")
				VerifyDPUClusterWithNodes(ctx, GetProvisionDPUClustersInput())
				By("Waiting for DPU cluster pods to be ready")
				VerifyClusterPods(ctx, dpuClusterClient[0], systemPodsToVerify)
				By("Waiting for DPFOperatorConfig to be ready")
				VerifyDPFOperatorConfigReady(ctx, input.Client, 20*time.Minute)
			}

			// Cleanup any VPC-related resources from previous test runs (when tests were run with skip cleanup)
			vpcPrerequisiteScope.CleanupBefore()
			vpcOvnContextScope.CleanupBefore()
			getDPUClusterClients(ctx, GetProvisionDPUClustersInput())
		}
	})

	AfterAll(func() {
		vpcPrerequisiteScope.CleanupAfter()
	})

	// Pods will have two default routes with same priority (metrics),
	// primary network default route ( flannel ) and vpc ovn default route.
	// Delete the primary network default route to avoid having additional default route to vpc ovn for stable testing.
	deleteFlannelDefaultRouteCmd := "ip route del default dev eth0"
	// This is the default interface name for the VFs
	vfDefaultInterfaceName := "net1"

	Context("Pre-requisite services and objects", Labels{Domain.RequiresNodes}, Ordered, func() {
		var dhcpDS *appsv1.DaemonSet

		It("create DPU VPC OVN VTEP DPUServiceIPAM", func() {
			createVtepDPUServiceIPAM(ctx, input)
		})

		It("create DPU VPC OVN gateway DPUServiceIPAM", func() {
			createGatewayDPUServiceIPAM(ctx, input)
		})

		It("create DPU VPC OVN central DPUService", func() {
			createOVNCentralDPUService(ctx, input.Client, dpfOperatorSystemNamespace, vpcOvnInput.dpuServiceOVNCentral)
		})

		It("create DPU OVN controller service", func() {
			createOVNControllerDPUService(ctx, input.Client, dpfOperatorSystemNamespace, vpcOvnInput.dpuServiceOVNController)
		})

		It("create DPU VPC OVN controller service", func() {
			createVPCOVNControllerDPUService(ctx, input.Client, dpfOperatorSystemNamespace, vpcOvnInput.dpuServiceVPCOVNController)
		})

		It("create DPU VPC OVN node service", func() {
			createVPCOVNNodeDPUService(ctx, input.Client, dpfOperatorSystemNamespace, vpcOvnInput.dpuServiceVPCOVNNode)
		})

		It("wait for pre-requisite DPU services to be ready", func() {
			dpuservice.WaitForDPUServices(ctx, input.Client, dpfOperatorSystemNamespace, []string{"ovn-central", "ovn-controller", "vpc-ovn-controller", "vpc-ovn-node"})
		})

		It("create DPU service interfaces", func() {
			createVPCPrerequisiteDPUServiceInterfaces(ctx, input)
		})

		It("wait for DPU service interfaces to be ready", func() {
			dpuServiceInterfaceNames := []string{ovnutils.PhysicalInterface0, ovnutils.OvnExtPatchName}
			dpuservice.WaitForDPUServiceInterfacesReady(ctx, input.Client, dpuClusterClient[0], dpuServiceInterfaceNames, dpfOperatorSystemNamespace)
		})

		It("create DPU service chain", func() {
			createOrUpdateVPCDPUServiceChain(ctx, input, nil)
		})

		It("wait for DPU service chain to be ready", func() {
			dpuservice.WaitForDPUServiceChainsReady(ctx, input.Client, dpuClusterClient[0], []string{ovnutils.VpcOVNServiceChain}, dpfOperatorSystemNamespace, vpcutils.DefaultTimeout)
		})

		It("create dhcp daemon", func() {
			dhcpDS = vpcutils.DeployDHCPDaemon(ctx, input.Client, vpcOvnInput.dhcpDaemonSet, vpcPrerequisiteScope.CleanupLabels)
		})

		It("wait for dhcp daemon pods to be ready", func() {
			vpcutils.WaitForDHCPDaemonReady(ctx, input.Client, dhcpDS)
		})

		It("get DPU nodes", func() {
			By("Getting DPU cluster nodes in order")
			dpuNode1, dpuNode2 = getDPUNodesInOrder(ctx, input.Client, dpuClusterClient[0])
			Expect(dpuNode1.Name).ToNot(BeEmpty())
			Expect(dpuNode2.Name).ToNot(BeEmpty())
		})
	})

	// Common test values
	ovnVPCProvisioner := "ovn.vpc.dpu.nvidia.com"
	defaultTenant := "foo"
	alternateTenant := "bar"
	vpcName := "myvpc"
	testnet1 := "testnet1"
	testnet2 := "testnet2"
	vpcTrafficTestNS := "vpc-traffic-test"
	vnet1DefaultSubnet := "192.178.0.0/16"
	vnet2DefaultSubnet := "192.188.0.0/16"
	podName1 := "netshoot-pod-1"
	podName2 := "netshoot-pod-2"
	podName3 := "netshoot-pod-3"

	// Service interface patterns
	pf0vf2Worker1 := "pf0vf2-worker1"
	pf0vf2Worker2 := "pf0vf2-worker2"
	pf0vf3Worker1 := "pf0vf3-worker1"
	pf0vf3Worker2 := "pf0vf3-worker2"

	// Host device patterns
	hostPf0Vf2 := "enp8s0f0v2"
	hostPf0Vf3 := "enp8s0f0v3"

	Context("Single virtual network, same and cross node traffic", Labels{Domain.RequiresNodes}, Ordered, func() {

		var (
			pf0vf2Worker1MacAddress string
			pf0vf2Worker2MacAddress string
			pf0vf3Worker1MacAddress string
			pod1IP                  string
			pod2IP                  string
			pod3IP                  string
			testPodConfigs          []*netshoot.TestPodConfig
			contextHasFailed        bool
		)

		AfterEach(func() {
			if CurrentSpecReport().Failed() {
				contextHasFailed = true
			}
		})

		AfterAll(func() {
			if contextHasFailed {
				By("VPC OVN: Report failure for this spec")
				reportAfterEach(CurrentSpecReport())
			}
			vpcOvnContextScope.CleanupAfter()
			cleanupDPUClusterNodeLabels(ctx)
		})

		It("label DPU nodes with tenant and tenant-node labels", func() {
			labelDPUNodesWithTenantAndTenantNode(ctx, dpuClusterClient[0], dpuNode1, dpuNode2, defaultTenant, defaultTenant)
		})

		It("create OVNIsolationClass object", func() {
			createOVNIsolationClass(ctx, input.Client, ovnVPCProvisioner, vpcOvnContextScope.CleanupLabels)
		})

		It("create DPUVPC object", func() {
			createDPUVPC(ctx, input.Client, vpcName, defaultTenant, ovnVPCProvisioner, vpcOvnContextScope.CleanupLabels)
		})

		It("create DPUVirtualNetwork object", func() {
			createDPUVirtualNetwork(ctx, input.Client, testnet1, vpcName, defaultTenant, vnet1DefaultSubnet, vpcOvnContextScope.CleanupLabels)
		})

		It("verify DPUVirtualNetwork is ready", func() {
			ovnutils.WaitForDPUServiceVirtualNetworkReady(ctx, input.Client, testnet1, dpfOperatorSystemNamespace)
		})

		It("verify DPUVPC is ready", func() {
			ovnutils.WaitForDPUVPCReady(ctx, input.Client, vpcName, dpfOperatorSystemNamespace)
		})

		It("verify DPUVPC and DPUVirtualNetwork metrics", func() {
			validateVPCMetrics(ctx)
		})

		It("create DPUServiceInterfaces on the nodes, same virtual network", func() {
			pf0vf2Worker1Labels := map[string]string{
				ovnutils.InterfaceLabelKey: pf0vf2Worker1,
			}
			pf0vf2Worker2Labels := map[string]string{
				ovnutils.InterfaceLabelKey: pf0vf2Worker2,
			}
			pf0vf3Worker1Labels := map[string]string{
				ovnutils.InterfaceLabelKey: pf0vf3Worker1,
			}

			createVPCDPUServiceInterface(ctx, input, dpuservice.TestDPUServiceInterfaceConfig{
				Name:           pf0vf2Worker1,
				Namespace:      dpfOperatorSystemNamespace,
				Labels:         cleanup.MergeMaps(vpcOvnContextScope.CleanupLabels, pf0vf2Worker1Labels),
				NodeName:       &dpuNode1.Name,
				Type:           dpuservicev1.InterfaceTypeVF,
				InterfaceName:  pf0vf2Worker1,
				PFIndex:        0,
				VFIndex:        2,
				VirtualNetwork: &testnet1,
			})
			createVPCDPUServiceInterface(ctx, input, dpuservice.TestDPUServiceInterfaceConfig{
				Name:           pf0vf2Worker2,
				Namespace:      dpfOperatorSystemNamespace,
				Labels:         cleanup.MergeMaps(vpcOvnContextScope.CleanupLabels, pf0vf2Worker2Labels),
				NodeName:       &dpuNode2.Name,
				Type:           dpuservicev1.InterfaceTypeVF,
				InterfaceName:  pf0vf2Worker2,
				PFIndex:        0,
				VFIndex:        2,
				VirtualNetwork: &testnet1,
			})

			createVPCDPUServiceInterface(ctx, input, dpuservice.TestDPUServiceInterfaceConfig{
				Name:           pf0vf3Worker1,
				Namespace:      dpfOperatorSystemNamespace,
				Labels:         cleanup.MergeMaps(vpcOvnContextScope.CleanupLabels, pf0vf3Worker1Labels),
				NodeName:       &dpuNode1.Name,
				Type:           dpuservicev1.InterfaceTypeVF,
				InterfaceName:  pf0vf3Worker1,
				PFIndex:        0,
				VFIndex:        3,
				VirtualNetwork: &testnet1,
			})
		})

		It("verify DPUServiceInterfaces are ready", func() {
			dpuServiceInterfaceNames := []string{pf0vf2Worker1, pf0vf2Worker2, pf0vf3Worker1}
			dpuservice.WaitForDPUServiceInterfacesReady(ctx, input.Client, dpuClusterClient[0], dpuServiceInterfaceNames, dpfOperatorSystemNamespace)
		})

		It("get the MAC addresses of the ServiceInterface objects", func() {
			pf0vf2Worker1Labels := map[string]string{
				ovnutils.InterfaceLabelKey: pf0vf2Worker1,
			}
			pf0vf2Worker2Labels := map[string]string{
				ovnutils.InterfaceLabelKey: pf0vf2Worker2,
			}
			pf0vf3Worker1Labels := map[string]string{
				ovnutils.InterfaceLabelKey: pf0vf3Worker1,
			}
			pf0vf2Worker1MacAddressesMap := ovnutils.GetServiceInterfaceMacAddressesMatchingLabels(ctx, dpuClusterClient[0], dpfOperatorSystemNamespace, pf0vf2Worker1Labels)
			pf0vf2Worker2MacAddressesMap := ovnutils.GetServiceInterfaceMacAddressesMatchingLabels(ctx, dpuClusterClient[0], dpfOperatorSystemNamespace, pf0vf2Worker2Labels)
			pf0vf3Worker1MacAddressesMap := ovnutils.GetServiceInterfaceMacAddressesMatchingLabels(ctx, dpuClusterClient[0], dpfOperatorSystemNamespace, pf0vf3Worker1Labels)

			Expect(pf0vf2Worker1MacAddressesMap).To(HaveLen(1))
			pf0vf2Worker1MacAddress = pf0vf2Worker1MacAddressesMap[dpuNode1.Name]
			Expect(pf0vf2Worker2MacAddressesMap).To(HaveLen(1))
			pf0vf2Worker2MacAddress = pf0vf2Worker2MacAddressesMap[dpuNode2.Name]
			Expect(pf0vf3Worker1MacAddressesMap).To(HaveLen(1))
			pf0vf3Worker1MacAddress = pf0vf3Worker1MacAddressesMap[dpuNode1.Name]
		})

		// Note: This is a workaround for testing to avoid rebooting the hosts.
		//  	MAC addresses will be set as part of the BFB, then when the host boots up, the mac address will be set.
		It("set host VF MAC addresses", func() {
			workerNode1, workerNode2 := getTwoWorkerNodeNames(ctx, input.Client)
			workerNode1IP := GetNodeInternalIP(ctx, input.Client, workerNode1)
			workerNode2IP := GetNodeInternalIP(ctx, input.Client, workerNode2)
			vpcutils.SetLinkMacAddress(workerNode1IP, hostPf0Vf2, pf0vf2Worker1MacAddress)
			vpcutils.SetLinkMacAddress(workerNode2IP, hostPf0Vf2, pf0vf2Worker2MacAddress)
			vpcutils.SetLinkMacAddress(workerNode1IP, hostPf0Vf3, pf0vf3Worker1MacAddress)
		})

		It("create netshoot pods and NetworkAttachmentDefinitions", func() {
			vpcutils.CreateTestNamespace(ctx, input.Client, vpcTrafficTestNS, vpcOvnContextScope.CleanupLabels)
			nadName1 := nadNamePrefix + podName1
			nadName2 := nadNamePrefix + podName2
			nadName3 := nadNamePrefix + podName3
			vpcutils.CreateDHCPNetworkAttachmentDefinition(ctx, input.Client, vpcTrafficTestNS, nadName1, hostPf0Vf2, ovnutils.VfsMTU, vpcOvnContextScope.CleanupLabels)
			vpcutils.CreateDHCPNetworkAttachmentDefinition(ctx, input.Client, vpcTrafficTestNS, nadName2, hostPf0Vf2, ovnutils.VfsMTU, vpcOvnContextScope.CleanupLabels)
			vpcutils.CreateDHCPNetworkAttachmentDefinition(ctx, input.Client, vpcTrafficTestNS, nadName3, hostPf0Vf3, ovnutils.VfsMTU, vpcOvnContextScope.CleanupLabels)
			workerNode1, workerNode2 := getTwoWorkerNodeNames(ctx, input.Client)
			testPodConfigs = []*netshoot.TestPodConfig{
				{
					Namespace:   vpcTrafficTestNS,
					Name:        podName1,
					NodeName:    workerNode1,
					NADName:     nadName1,
					Labels:      vpcOvnContextScope.CleanupLabels,
					CommandArgs: []string{deleteFlannelDefaultRouteCmd},
				},
				{
					Namespace:   vpcTrafficTestNS,
					Name:        podName2,
					NodeName:    workerNode2,
					NADName:     nadName2,
					Labels:      vpcOvnContextScope.CleanupLabels,
					CommandArgs: []string{deleteFlannelDefaultRouteCmd},
				},
				{
					Namespace:   vpcTrafficTestNS,
					Name:        podName3,
					NodeName:    workerNode1,
					NADName:     nadName3,
					Labels:      vpcOvnContextScope.CleanupLabels,
					CommandArgs: []string{deleteFlannelDefaultRouteCmd},
				},
			}
			netshoot.CreatePods(ctx, input.Client, testPodConfigs)
		})

		It("verify netshoot pods are running", func() {
			netshoot.WaitForPodsReady(ctx, input.Client, testPodConfigs, vpcutils.LongTimeout)
		})

		It("get pod IP addresses", func() {
			pod1IP = vpcutils.GetPodIPAddressFromNetworkStatus(ctx, input.Client, vpcTrafficTestNS, podName1, vfDefaultInterfaceName)
			pod2IP = vpcutils.GetPodIPAddressFromNetworkStatus(ctx, input.Client, vpcTrafficTestNS, podName2, vfDefaultInterfaceName)
			pod3IP = vpcutils.GetPodIPAddressFromNetworkStatus(ctx, input.Client, vpcTrafficTestNS, podName3, vfDefaultInterfaceName)
			Expect(pod1IP).ToNot(BeEmpty())
			Expect(pod2IP).ToNot(BeEmpty())
			Expect(pod3IP).ToNot(BeEmpty())
		})

		It("verify netshoot pods can ping each other in the same node", func() {
			netshoot.AssertPingSuccess(&hostClusterRESTClient, &input.RestConfig, vpcTrafficTestNS, podName1, pod3IP)
			netshoot.AssertPingSuccess(&hostClusterRESTClient, &input.RestConfig, vpcTrafficTestNS, podName3, pod1IP)
		})

		It("verify netshoot pods can ping each other cross nodes", func() {
			netshoot.AssertPingSuccess(&hostClusterRESTClient, &input.RestConfig, vpcTrafficTestNS, podName1, pod2IP)
			netshoot.AssertPingSuccess(&hostClusterRESTClient, &input.RestConfig, vpcTrafficTestNS, podName2, pod1IP)
			netshoot.AssertPingSuccess(&hostClusterRESTClient, &input.RestConfig, vpcTrafficTestNS, podName3, pod2IP)
			netshoot.AssertPingSuccess(&hostClusterRESTClient, &input.RestConfig, vpcTrafficTestNS, podName2, pod3IP)
		})

		It("verify performance with iperf same node traffic", func() {
			netshoot.RunTrafficTest(&hostClusterRESTClient, &input.RestConfig, vpcTrafficTestNS, podName1, podName3, pod3IP)
		})

		It("verify performance with iperf cross node traffic", func() {
			netshoot.RunTrafficTest(&hostClusterRESTClient, &input.RestConfig, vpcTrafficTestNS, podName1, podName2, pod2IP)
		})
	})

	Context("Two virtual networks, same and cross node traffic", Labels{Domain.RequiresNodes}, Ordered, func() {

		var (
			pf0vf2Worker1MacAddress string
			pf0vf2Worker2MacAddress string
			pf0vf3Worker1MacAddress string
			pod1IP                  string
			pod2IP                  string
			pod3IP                  string
			testPodConfigs          []*netshoot.TestPodConfig
			contextHasFailed        bool
		)

		AfterEach(func() {
			if CurrentSpecReport().Failed() {
				contextHasFailed = true
			}
		})

		AfterAll(func() {
			if contextHasFailed {
				By("VPC OVN: Report failure for this spec")
				reportAfterEach(CurrentSpecReport())
			}
			vpcOvnContextScope.CleanupAfter()
			cleanupDPUClusterNodeLabels(ctx)
		})

		It("label DPU nodes with tenant and tenant-node labels", func() {
			labelDPUNodesWithTenantAndTenantNode(ctx, dpuClusterClient[0], dpuNode1, dpuNode2, defaultTenant, defaultTenant)
		})

		It("create OVNIsolationClass object", func() {
			createOVNIsolationClass(ctx, input.Client, ovnVPCProvisioner, vpcOvnContextScope.CleanupLabels)
		})

		It("create DPUVPC object", func() {
			createDPUVPC(ctx, input.Client, vpcName, defaultTenant, ovnVPCProvisioner, vpcOvnContextScope.CleanupLabels)
		})

		It("create DPUVirtualNetwork objects", func() {
			createDPUVirtualNetwork(ctx, input.Client, testnet1, vpcName, defaultTenant, vnet1DefaultSubnet, vpcOvnContextScope.CleanupLabels)
			createDPUVirtualNetwork(ctx, input.Client, testnet2, vpcName, defaultTenant, vnet2DefaultSubnet, vpcOvnContextScope.CleanupLabels)
		})

		It("verify DPUVirtualNetwork is ready", func() {
			ovnutils.WaitForDPUServiceVirtualNetworkReady(ctx, input.Client, testnet1, dpfOperatorSystemNamespace)
			ovnutils.WaitForDPUServiceVirtualNetworkReady(ctx, input.Client, testnet2, dpfOperatorSystemNamespace)
		})

		It("verify DPUVPC is ready", func() {
			ovnutils.WaitForDPUVPCReady(ctx, input.Client, vpcName, dpfOperatorSystemNamespace)
		})

		It("create DPUServiceInterfaces on the nodes, different virtual networks", func() {
			// Worker1: pf0vf2-worker1 virtual network testnet1, pf0vf3-worker1 virtual network testnet2
			// Worker2: pf0vf2-worker2 virtual network testnet2

			pf0vf2Worker1Labels := map[string]string{
				ovnutils.InterfaceLabelKey: pf0vf2Worker1,
			}
			pf0vf2Worker2Labels := map[string]string{
				ovnutils.InterfaceLabelKey: pf0vf2Worker2,
			}
			pf0vf3Worker1Labels := map[string]string{
				ovnutils.InterfaceLabelKey: pf0vf3Worker1,
			}

			createVPCDPUServiceInterface(ctx, input, dpuservice.TestDPUServiceInterfaceConfig{
				Name:           pf0vf2Worker1,
				Namespace:      dpfOperatorSystemNamespace,
				Labels:         cleanup.MergeMaps(vpcOvnContextScope.CleanupLabels, pf0vf2Worker1Labels),
				NodeName:       &dpuNode1.Name,
				Type:           dpuservicev1.InterfaceTypeVF,
				InterfaceName:  pf0vf2Worker1,
				PFIndex:        0,
				VFIndex:        2,
				VirtualNetwork: &testnet1,
			})

			createVPCDPUServiceInterface(ctx, input, dpuservice.TestDPUServiceInterfaceConfig{
				Name:           pf0vf2Worker2,
				Namespace:      dpfOperatorSystemNamespace,
				Labels:         cleanup.MergeMaps(vpcOvnContextScope.CleanupLabels, pf0vf2Worker2Labels),
				NodeName:       &dpuNode2.Name,
				Type:           dpuservicev1.InterfaceTypeVF,
				InterfaceName:  pf0vf2Worker2,
				PFIndex:        0,
				VFIndex:        2,
				VirtualNetwork: &testnet2,
			})

			createVPCDPUServiceInterface(ctx, input, dpuservice.TestDPUServiceInterfaceConfig{
				Name:           pf0vf3Worker1,
				Namespace:      dpfOperatorSystemNamespace,
				Labels:         cleanup.MergeMaps(vpcOvnContextScope.CleanupLabels, pf0vf3Worker1Labels),
				NodeName:       &dpuNode1.Name,
				Type:           dpuservicev1.InterfaceTypeVF,
				InterfaceName:  pf0vf3Worker1,
				PFIndex:        0,
				VFIndex:        3,
				VirtualNetwork: &testnet2,
			})
		})

		It("verify DPUServiceInterface is ready", func() {
			dpuServiceInterfaceNames := []string{pf0vf2Worker1, pf0vf2Worker2, pf0vf3Worker1}
			dpuservice.WaitForDPUServiceInterfacesReady(ctx, input.Client, dpuClusterClient[0], dpuServiceInterfaceNames, dpfOperatorSystemNamespace)
		})

		It("get the MAC addresses of the ServiceInterface objects", func() {
			pf0vf2Worker1Labels := map[string]string{
				ovnutils.InterfaceLabelKey: pf0vf2Worker1,
			}
			pf0vf2Worker2Labels := map[string]string{
				ovnutils.InterfaceLabelKey: pf0vf2Worker2,
			}
			pf0vf3Worker1Labels := map[string]string{
				ovnutils.InterfaceLabelKey: pf0vf3Worker1,
			}

			pf0vf2Worker1MacAddressesMap := ovnutils.GetServiceInterfaceMacAddressesMatchingLabels(ctx, dpuClusterClient[0], dpfOperatorSystemNamespace, pf0vf2Worker1Labels)
			pf0vf2Worker2MacAddressesMap := ovnutils.GetServiceInterfaceMacAddressesMatchingLabels(ctx, dpuClusterClient[0], dpfOperatorSystemNamespace, pf0vf2Worker2Labels)
			pf0vf3Worker1MacAddressesMap := ovnutils.GetServiceInterfaceMacAddressesMatchingLabels(ctx, dpuClusterClient[0], dpfOperatorSystemNamespace, pf0vf3Worker1Labels)

			Expect(pf0vf2Worker1MacAddressesMap).To(HaveLen(1))
			pf0vf2Worker1MacAddress = pf0vf2Worker1MacAddressesMap[dpuNode1.Name]
			Expect(pf0vf2Worker2MacAddressesMap).To(HaveLen(1))
			pf0vf2Worker2MacAddress = pf0vf2Worker2MacAddressesMap[dpuNode2.Name]
			Expect(pf0vf3Worker1MacAddressesMap).To(HaveLen(1))
			pf0vf3Worker1MacAddress = pf0vf3Worker1MacAddressesMap[dpuNode1.Name]
		})

		// Note: This is a workaround for testing to avoid rebooting the hosts.
		//  	MAC addresses will be set as part of the BFB, then when the host boots up, the mac address will be set.
		It("set host VF MAC addresses", func() {
			workerNode1, workerNode2 := getTwoWorkerNodeNames(ctx, input.Client)
			workerNode1IP := GetNodeInternalIP(ctx, input.Client, workerNode1)
			workerNode2IP := GetNodeInternalIP(ctx, input.Client, workerNode2)
			vpcutils.SetLinkMacAddress(workerNode1IP, hostPf0Vf2, pf0vf2Worker1MacAddress)
			vpcutils.SetLinkMacAddress(workerNode2IP, hostPf0Vf2, pf0vf2Worker2MacAddress)
			vpcutils.SetLinkMacAddress(workerNode1IP, hostPf0Vf3, pf0vf3Worker1MacAddress)
		})

		It("create netshoot pods and NetworkAttachmentDefinitions", func() {
			vpcutils.CreateTestNamespace(ctx, input.Client, vpcTrafficTestNS, vpcOvnContextScope.CleanupLabels)
			nadName1 := nadNamePrefix + podName1
			nadName2 := nadNamePrefix + podName2
			nadName3 := nadNamePrefix + podName3
			vpcutils.CreateDHCPNetworkAttachmentDefinition(ctx, input.Client, vpcTrafficTestNS, nadName1, hostPf0Vf2, ovnutils.VfsMTU, vpcOvnContextScope.CleanupLabels)
			vpcutils.CreateDHCPNetworkAttachmentDefinition(ctx, input.Client, vpcTrafficTestNS, nadName2, hostPf0Vf2, ovnutils.VfsMTU, vpcOvnContextScope.CleanupLabels)
			vpcutils.CreateDHCPNetworkAttachmentDefinition(ctx, input.Client, vpcTrafficTestNS, nadName3, hostPf0Vf3, ovnutils.VfsMTU, vpcOvnContextScope.CleanupLabels)
			workerNode1, workerNode2 := getTwoWorkerNodeNames(ctx, input.Client)
			testPodConfigs = []*netshoot.TestPodConfig{
				{
					Namespace:   vpcTrafficTestNS,
					Name:        podName1,
					NodeName:    workerNode1,
					NADName:     nadName1,
					Labels:      vpcOvnContextScope.CleanupLabels,
					CommandArgs: []string{deleteFlannelDefaultRouteCmd},
				},
				{
					Namespace:   vpcTrafficTestNS,
					Name:        podName2,
					NodeName:    workerNode2,
					NADName:     nadName2,
					Labels:      vpcOvnContextScope.CleanupLabels,
					CommandArgs: []string{deleteFlannelDefaultRouteCmd},
				},
				{
					Namespace:   vpcTrafficTestNS,
					Name:        podName3,
					NodeName:    workerNode1,
					NADName:     nadName3,
					Labels:      vpcOvnContextScope.CleanupLabels,
					CommandArgs: []string{deleteFlannelDefaultRouteCmd},
				},
			}
			netshoot.CreatePods(ctx, input.Client, testPodConfigs)
		})

		It("verify netshoot pods are running", func() {
			netshoot.WaitForPodsReady(ctx, input.Client, testPodConfigs, vpcutils.LongTimeout)
		})

		It("get pod IP addresses", func() {
			pod1IP = vpcutils.GetPodIPAddressFromNetworkStatus(ctx, input.Client, vpcTrafficTestNS, podName1, vfDefaultInterfaceName)
			pod2IP = vpcutils.GetPodIPAddressFromNetworkStatus(ctx, input.Client, vpcTrafficTestNS, podName2, vfDefaultInterfaceName)
			pod3IP = vpcutils.GetPodIPAddressFromNetworkStatus(ctx, input.Client, vpcTrafficTestNS, podName3, vfDefaultInterfaceName)
			Expect(pod1IP).ToNot(BeEmpty())
			Expect(pod2IP).ToNot(BeEmpty())
			Expect(pod3IP).ToNot(BeEmpty())
		})

		It("verify netshoot pods can ping each other in the same node", func() {
			netshoot.AssertPingSuccess(&hostClusterRESTClient, &input.RestConfig, vpcTrafficTestNS, podName1, pod3IP)
			netshoot.AssertPingSuccess(&hostClusterRESTClient, &input.RestConfig, vpcTrafficTestNS, podName3, pod1IP)
		})

		It("verify netshoot pods can ping each other cross nodes", func() {
			netshoot.AssertPingSuccess(&hostClusterRESTClient, &input.RestConfig, vpcTrafficTestNS, podName1, pod2IP)
			netshoot.AssertPingSuccess(&hostClusterRESTClient, &input.RestConfig, vpcTrafficTestNS, podName2, pod1IP)
			netshoot.AssertPingSuccess(&hostClusterRESTClient, &input.RestConfig, vpcTrafficTestNS, podName3, pod2IP)
			netshoot.AssertPingSuccess(&hostClusterRESTClient, &input.RestConfig, vpcTrafficTestNS, podName2, pod3IP)
		})

		It("verify performance with iperf same node traffic", func() {
			netshoot.RunTrafficTest(&hostClusterRESTClient, &input.RestConfig, vpcTrafficTestNS, podName1, podName3, pod3IP)
		})

		It("verify performance with iperf cross node traffic", func() {
			netshoot.RunTrafficTest(&hostClusterRESTClient, &input.RestConfig, vpcTrafficTestNS, podName1, podName2, pod2IP)
		})
	})

	Context("Different VPCs", Labels{Domain.RequiresNodes}, Ordered, func() {

		var (
			pf0vf2Worker1MacAddress string
			pf0vf2Worker2MacAddress string
			pf0vf3Worker2MacAddress string
			pod1IP                  string
			pod2IP                  string
			pod3IP                  string
			testPodConfigs          []*netshoot.TestPodConfig
			contextHasFailed        bool
		)

		vpcName2 := "myvpc-2"

		AfterEach(func() {
			if CurrentSpecReport().Failed() {
				contextHasFailed = true
			}
		})

		AfterAll(func() {
			if contextHasFailed {
				By("VPC OVN: Report failure for this spec")
				reportAfterEach(CurrentSpecReport())
			}
			vpcOvnContextScope.CleanupAfter()
			cleanupDPUClusterNodeLabels(ctx)
		})

		It("label DPU nodes with tenant and tenant-node labels", func() {
			labelDPUNodesWithTenantAndTenantNode(ctx, dpuClusterClient[0], dpuNode1, dpuNode2, defaultTenant, alternateTenant)
		})

		It("create OVNIsolationClass object", func() {
			createOVNIsolationClass(ctx, input.Client, ovnVPCProvisioner, vpcOvnContextScope.CleanupLabels)
		})

		It("create DPUVPC object", func() {
			createDPUVPC(ctx, input.Client, vpcName, defaultTenant, ovnVPCProvisioner, vpcOvnContextScope.CleanupLabels)
			createDPUVPC(ctx, input.Client, vpcName2, alternateTenant, ovnVPCProvisioner, vpcOvnContextScope.CleanupLabels)

		})

		It("create DPUVirtualNetwork objects", func() {
			createDPUVirtualNetwork(ctx, input.Client, testnet1, vpcName, defaultTenant, vnet1DefaultSubnet, vpcOvnContextScope.CleanupLabels)
			createDPUVirtualNetwork(ctx, input.Client, testnet2, vpcName2, alternateTenant, vnet2DefaultSubnet, vpcOvnContextScope.CleanupLabels)
		})

		It("verify DPUVirtualNetwork is ready", func() {
			ovnutils.WaitForDPUServiceVirtualNetworkReady(ctx, input.Client, testnet1, dpfOperatorSystemNamespace)
			ovnutils.WaitForDPUServiceVirtualNetworkReady(ctx, input.Client, testnet2, dpfOperatorSystemNamespace)
		})

		It("verify DPUVPC is ready", func() {
			ovnutils.WaitForDPUVPCReady(ctx, input.Client, vpcName, dpfOperatorSystemNamespace)
			ovnutils.WaitForDPUVPCReady(ctx, input.Client, vpcName2, dpfOperatorSystemNamespace)
		})

		It("create DPUServiceInterfaces on the nodes, different virtual networks, different vpcs", func() {
			pf0vf2Worker1Labels := map[string]string{
				ovnutils.InterfaceLabelKey: pf0vf2Worker1,
			}
			pf0vf2Worker2Labels := map[string]string{
				ovnutils.InterfaceLabelKey: pf0vf2Worker2,
			}
			pf0vf3Worker2Labels := map[string]string{
				ovnutils.InterfaceLabelKey: pf0vf3Worker2,
			}

			createVPCDPUServiceInterface(ctx, input, dpuservice.TestDPUServiceInterfaceConfig{
				Name:           pf0vf2Worker1,
				Namespace:      dpfOperatorSystemNamespace,
				Labels:         cleanup.MergeMaps(vpcOvnContextScope.CleanupLabels, pf0vf2Worker1Labels),
				NodeName:       &dpuNode1.Name,
				Type:           dpuservicev1.InterfaceTypeVF,
				InterfaceName:  pf0vf2Worker1,
				PFIndex:        0,
				VFIndex:        2,
				VirtualNetwork: &testnet1,
			})

			createVPCDPUServiceInterface(ctx, input, dpuservice.TestDPUServiceInterfaceConfig{
				Name:           pf0vf2Worker2,
				Namespace:      dpfOperatorSystemNamespace,
				Labels:         cleanup.MergeMaps(vpcOvnContextScope.CleanupLabels, pf0vf2Worker2Labels),
				NodeName:       &dpuNode2.Name,
				Type:           dpuservicev1.InterfaceTypeVF,
				InterfaceName:  pf0vf2Worker2,
				PFIndex:        0,
				VFIndex:        2,
				VirtualNetwork: &testnet2,
			})

			createVPCDPUServiceInterface(ctx, input, dpuservice.TestDPUServiceInterfaceConfig{
				Name:           pf0vf3Worker2,
				Namespace:      dpfOperatorSystemNamespace,
				Labels:         cleanup.MergeMaps(vpcOvnContextScope.CleanupLabels, pf0vf3Worker2Labels),
				NodeName:       &dpuNode2.Name,
				Type:           dpuservicev1.InterfaceTypeVF,
				InterfaceName:  pf0vf3Worker2,
				PFIndex:        0,
				VFIndex:        3,
				VirtualNetwork: &testnet2,
			})
		})

		It("verify DPUServiceInterface is ready", func() {
			dpuServiceInterfaceNames := []string{pf0vf2Worker1, pf0vf2Worker2, pf0vf3Worker2}
			dpuservice.WaitForDPUServiceInterfacesReady(ctx, input.Client, dpuClusterClient[0], dpuServiceInterfaceNames, dpfOperatorSystemNamespace)
		})

		It("get the MAC addresses of the ServiceInterface objects", func() {
			pf0vf2Worker1Labels := map[string]string{
				ovnutils.InterfaceLabelKey: pf0vf2Worker1,
			}
			pf0vf2Worker2Labels := map[string]string{
				ovnutils.InterfaceLabelKey: pf0vf2Worker2,
			}
			pf0vf3Worker2Labels := map[string]string{
				ovnutils.InterfaceLabelKey: pf0vf3Worker2,
			}

			pf0vf2Worker1MacAddressesMap := ovnutils.GetServiceInterfaceMacAddressesMatchingLabels(ctx, dpuClusterClient[0], dpfOperatorSystemNamespace, pf0vf2Worker1Labels)
			pf0vf2Worker2MacAddressesMap := ovnutils.GetServiceInterfaceMacAddressesMatchingLabels(ctx, dpuClusterClient[0], dpfOperatorSystemNamespace, pf0vf2Worker2Labels)
			pf0vf3Worker2MacAddressesMap := ovnutils.GetServiceInterfaceMacAddressesMatchingLabels(ctx, dpuClusterClient[0], dpfOperatorSystemNamespace, pf0vf3Worker2Labels)

			Expect(pf0vf2Worker1MacAddressesMap).To(HaveLen(1))
			pf0vf2Worker1MacAddress = pf0vf2Worker1MacAddressesMap[dpuNode1.Name]
			Expect(pf0vf2Worker2MacAddressesMap).To(HaveLen(1))
			pf0vf2Worker2MacAddress = pf0vf2Worker2MacAddressesMap[dpuNode2.Name]
			Expect(pf0vf3Worker2MacAddressesMap).To(HaveLen(1))
			pf0vf3Worker2MacAddress = pf0vf3Worker2MacAddressesMap[dpuNode2.Name]

			Expect(pf0vf2Worker1MacAddress).ToNot(BeEmpty())
			Expect(pf0vf2Worker2MacAddress).ToNot(BeEmpty())
			Expect(pf0vf3Worker2MacAddress).ToNot(BeEmpty())
		})

		// Note: This is a workaround for testing to avoid rebooting the hosts.
		//  	MAC addresses will be set as part of the BFB, then when the host boots up, the mac address will be set.
		It("set host VF MAC addresses", func() {
			workerNode1, workerNode2 := getTwoWorkerNodeNames(ctx, input.Client)
			workerNode1IP := GetNodeInternalIP(ctx, input.Client, workerNode1)
			workerNode2IP := GetNodeInternalIP(ctx, input.Client, workerNode2)
			vpcutils.SetLinkMacAddress(workerNode1IP, hostPf0Vf2, pf0vf2Worker1MacAddress)
			vpcutils.SetLinkMacAddress(workerNode2IP, hostPf0Vf2, pf0vf2Worker2MacAddress)
			vpcutils.SetLinkMacAddress(workerNode2IP, hostPf0Vf3, pf0vf3Worker2MacAddress)
		})

		It("create netshoot pods and NetworkAttachmentDefinitions", func() {
			vpcutils.CreateTestNamespace(ctx, input.Client, vpcTrafficTestNS, vpcOvnContextScope.CleanupLabels)
			nadName1 := nadNamePrefix + podName1
			nadName2 := nadNamePrefix + podName2
			nadName3 := nadNamePrefix + podName3
			vpcutils.CreateDHCPNetworkAttachmentDefinition(ctx, input.Client, vpcTrafficTestNS, nadName1, hostPf0Vf2, ovnutils.VfsMTU, vpcOvnContextScope.CleanupLabels)
			vpcutils.CreateDHCPNetworkAttachmentDefinition(ctx, input.Client, vpcTrafficTestNS, nadName2, hostPf0Vf2, ovnutils.VfsMTU, vpcOvnContextScope.CleanupLabels)
			vpcutils.CreateDHCPNetworkAttachmentDefinition(ctx, input.Client, vpcTrafficTestNS, nadName3, hostPf0Vf3, ovnutils.VfsMTU, vpcOvnContextScope.CleanupLabels)
			workerNode1, workerNode2 := getTwoWorkerNodeNames(ctx, input.Client)
			testPodConfigs = []*netshoot.TestPodConfig{
				{
					Namespace:   vpcTrafficTestNS,
					Name:        podName1,
					NodeName:    workerNode1,
					NADName:     nadName1,
					Labels:      vpcOvnContextScope.CleanupLabels,
					CommandArgs: []string{deleteFlannelDefaultRouteCmd},
				},
				{
					Namespace:   vpcTrafficTestNS,
					Name:        podName2,
					NodeName:    workerNode2,
					NADName:     nadName2,
					Labels:      vpcOvnContextScope.CleanupLabels,
					CommandArgs: []string{deleteFlannelDefaultRouteCmd},
				},
				{
					Namespace:   vpcTrafficTestNS,
					Name:        podName3,
					NodeName:    workerNode2,
					NADName:     nadName3,
					Labels:      vpcOvnContextScope.CleanupLabels,
					CommandArgs: []string{deleteFlannelDefaultRouteCmd},
				},
			}
			netshoot.CreatePods(ctx, input.Client, testPodConfigs)
		})

		It("verify netshoot pods are running", func() {
			netshoot.WaitForPodsReady(ctx, input.Client, testPodConfigs, vpcutils.LongTimeout)
		})

		It("get pod IP addresses", func() {
			pod1IP = vpcutils.GetPodIPAddressFromNetworkStatus(ctx, input.Client, vpcTrafficTestNS, podName1, vfDefaultInterfaceName)
			pod2IP = vpcutils.GetPodIPAddressFromNetworkStatus(ctx, input.Client, vpcTrafficTestNS, podName2, vfDefaultInterfaceName)
			pod3IP = vpcutils.GetPodIPAddressFromNetworkStatus(ctx, input.Client, vpcTrafficTestNS, podName3, vfDefaultInterfaceName)
			Expect(pod1IP).ToNot(BeEmpty())
			Expect(pod2IP).ToNot(BeEmpty())
			Expect(pod3IP).ToNot(BeEmpty())
		})

		It("verify netshoot pods within the same VPC can ping each other", func() {
			netshoot.AssertPingSuccess(&hostClusterRESTClient, &input.RestConfig, vpcTrafficTestNS, podName2, pod3IP)
			netshoot.AssertPingSuccess(&hostClusterRESTClient, &input.RestConfig, vpcTrafficTestNS, podName3, pod2IP)
		})

		It("verify netshoot pods different vpcs cannot ping each other", func() {
			netshoot.AssertPingFailure(&hostClusterRESTClient, &input.RestConfig, vpcTrafficTestNS, podName1, pod2IP)
			netshoot.AssertPingFailure(&hostClusterRESTClient, &input.RestConfig, vpcTrafficTestNS, podName2, pod1IP)
			netshoot.AssertPingFailure(&hostClusterRESTClient, &input.RestConfig, vpcTrafficTestNS, podName1, pod3IP)
			netshoot.AssertPingFailure(&hostClusterRESTClient, &input.RestConfig, vpcTrafficTestNS, podName3, pod1IP)
		})
	})

	Context("ServiceInterface type Service", Labels{Domain.RequiresNodes}, Ordered, func() {

		var (
			pf0vf2Worker1MacAddress string
			pf0vf2Worker2MacAddress string
			pod1SFIP                string
			pod2SFIP                string
			testPodConfigs          []*netshoot.TestPodConfig
			sfPods                  []corev1.Pod
			hostWorkerNode1         string
			hostWorkerNode2         string
			contextHasFailed        bool
		)

		const (
			dpfServiceIDLabelKey = "svc.dpu.nvidia.com/service"
			sfName               = "dummyservice-sf"
			sfInterfaceName      = "dummyservice_sf"
			serviceID            = "dummydpuservice"
			brIntNetwork         = "mybrint-vpc"
			sfServiceName        = "dummy-dpu-service"
		)

		BeforeAll(func() {
			hostWorkerNode1, hostWorkerNode2 = getTwoWorkerNodeNames(ctx, input.Client)
		})

		AfterEach(func() {
			if CurrentSpecReport().Failed() {
				contextHasFailed = true
			}
		})

		AfterAll(func() {
			if contextHasFailed {
				By("VPC OVN: Report failure for this spec")
				reportAfterEach(CurrentSpecReport())
			}
			vpcOvnContextScope.CleanupAfter()
			cleanupDPUClusterNodeLabels(ctx)
		})

		It("label DPU nodes with tenant and tenant-node labels", func() {
			labelDPUNodesWithTenantAndTenantNode(ctx, dpuClusterClient[0], dpuNode1, dpuNode2, defaultTenant, defaultTenant)
		})

		It("create OVNIsolationClass object", func() {
			createOVNIsolationClass(ctx, input.Client, ovnVPCProvisioner, vpcOvnContextScope.CleanupLabels)
		})

		It("create DPUVPC object", func() {
			createDPUVPC(ctx, input.Client, vpcName, defaultTenant, ovnVPCProvisioner, vpcOvnContextScope.CleanupLabels)
		})

		It("create DPUVirtualNetwork object", func() {
			createDPUVirtualNetwork(ctx, input.Client, testnet1, vpcName, defaultTenant, vnet1DefaultSubnet, vpcOvnContextScope.CleanupLabels)
		})

		It("verify DPUVirtualNetwork is ready", func() {
			ovnutils.WaitForDPUServiceVirtualNetworkReady(ctx, input.Client, testnet1, dpfOperatorSystemNamespace)
		})

		It("verify DPUVPC is ready", func() {
			ovnutils.WaitForDPUVPCReady(ctx, input.Client, vpcName, dpfOperatorSystemNamespace)
		})

		It("create DPUServiceInterfaces on the nodes, same virtual network", func() {
			sfLabels := map[string]string{
				ovnutils.ServiceInterfaceLabelKey: sfInterfaceName,
				dpfServiceIDLabelKey:              serviceID,
			}
			pf0vf2Worker1Labels := map[string]string{
				ovnutils.InterfaceLabelKey: pf0vf2Worker1,
			}
			pf0vf2Worker2Labels := map[string]string{
				ovnutils.InterfaceLabelKey: pf0vf2Worker2,
			}

			// Create SFs on both nodes.
			createVPCDPUServiceInterface(ctx, input, dpuservice.TestDPUServiceInterfaceConfig{
				Name:           sfName,
				Namespace:      dpfOperatorSystemNamespace,
				Labels:         cleanup.MergeMaps(vpcOvnContextScope.CleanupLabels, sfLabels),
				Type:           dpuservicev1.InterfaceTypeService,
				InterfaceName:  sfInterfaceName,
				ServiceID:      serviceID,
				Network:        fmt.Sprintf("%s/%s", dpfOperatorSystemNamespace, brIntNetwork),
				VirtualNetwork: &testnet1,
			})
			createVPCDPUServiceInterface(ctx, input, dpuservice.TestDPUServiceInterfaceConfig{
				Name:           pf0vf2Worker1,
				Namespace:      dpfOperatorSystemNamespace,
				Labels:         cleanup.MergeMaps(vpcOvnContextScope.CleanupLabels, pf0vf2Worker1Labels),
				NodeName:       &dpuNode1.Name,
				Type:           dpuservicev1.InterfaceTypeVF,
				InterfaceName:  pf0vf2Worker1,
				PFIndex:        0,
				VFIndex:        2,
				VirtualNetwork: &testnet1,
			})
			createVPCDPUServiceInterface(ctx, input, dpuservice.TestDPUServiceInterfaceConfig{
				Name:           pf0vf2Worker2,
				Namespace:      dpfOperatorSystemNamespace,
				Labels:         cleanup.MergeMaps(vpcOvnContextScope.CleanupLabels, pf0vf2Worker2Labels),
				NodeName:       &dpuNode2.Name,
				Type:           dpuservicev1.InterfaceTypeVF,
				InterfaceName:  pf0vf2Worker2,
				PFIndex:        0,
				VFIndex:        2,
				VirtualNetwork: &testnet1,
			})
		})

		It("verify VF DPUServiceInterface is ready", func() {
			dpuservice.WaitForDPUServiceInterfacesReady(ctx, input.Client, dpuClusterClient[0], []string{pf0vf2Worker1, pf0vf2Worker2}, dpfOperatorSystemNamespace)
		})

		It("get the MAC addresses of the ServiceInterface objects", func() {
			pf0vf2Worker1Labels := map[string]string{
				ovnutils.InterfaceLabelKey: pf0vf2Worker1,
			}
			pf0vf2Worker2Labels := map[string]string{
				ovnutils.InterfaceLabelKey: pf0vf2Worker2,
			}

			pf0vf2Worker1MacAddresseMap := ovnutils.GetServiceInterfaceMacAddressesMatchingLabels(ctx, dpuClusterClient[0], dpfOperatorSystemNamespace, pf0vf2Worker1Labels)
			Expect(pf0vf2Worker1MacAddresseMap).To(HaveLen(1))
			pf0vf2Worker1MacAddress = pf0vf2Worker1MacAddresseMap[dpuNode1.Name]
			Expect(pf0vf2Worker1MacAddress).ToNot(BeEmpty())

			pf0vf2Worker2MacAddressesMap := ovnutils.GetServiceInterfaceMacAddressesMatchingLabels(ctx, dpuClusterClient[0], dpfOperatorSystemNamespace, pf0vf2Worker2Labels)
			Expect(pf0vf2Worker2MacAddressesMap).To(HaveLen(1))
			pf0vf2Worker2MacAddress = pf0vf2Worker2MacAddressesMap[dpuNode2.Name]
			Expect(pf0vf2Worker2MacAddress).ToNot(BeEmpty())
		})

		// Note: This is a workaround for testing to avoid rebooting the hosts.
		//  	MAC addresses will be set as part of the BFB, then when the host boots up, the mac address will be set.
		It("set host VF MAC addresses", func() {
			hostWorkerNode1IP := GetNodeInternalIP(ctx, input.Client, hostWorkerNode1)
			hostWorkerNode2IP := GetNodeInternalIP(ctx, input.Client, hostWorkerNode2)
			vpcutils.SetLinkMacAddress(hostWorkerNode1IP, hostPf0Vf2, pf0vf2Worker1MacAddress)
			vpcutils.SetLinkMacAddress(hostWorkerNode2IP, hostPf0Vf2, pf0vf2Worker2MacAddress)
		})

		It("create netshoot pods and NetworkAttachmentDefinition", func() {
			vpcutils.CreateTestNamespace(ctx, input.Client, vpcTrafficTestNS, vpcOvnContextScope.CleanupLabels)
			nadName1 := nadNamePrefix + podName1
			nadName2 := nadNamePrefix + podName2
			vpcutils.CreateDHCPNetworkAttachmentDefinition(ctx, input.Client, vpcTrafficTestNS, nadName1, hostPf0Vf2, ovnutils.VfsMTU, vpcOvnContextScope.CleanupLabels)
			vpcutils.CreateDHCPNetworkAttachmentDefinition(ctx, input.Client, vpcTrafficTestNS, nadName2, hostPf0Vf2, ovnutils.VfsMTU, vpcOvnContextScope.CleanupLabels)
			testPodConfigs = []*netshoot.TestPodConfig{
				{
					Namespace:   vpcTrafficTestNS,
					Name:        podName1,
					NodeName:    hostWorkerNode1,
					NADName:     nadName1,
					Labels:      vpcOvnContextScope.CleanupLabels,
					CommandArgs: []string{deleteFlannelDefaultRouteCmd},
				},
				{
					Namespace:   vpcTrafficTestNS,
					Name:        podName2,
					NodeName:    hostWorkerNode2,
					NADName:     nadName2,
					Labels:      vpcOvnContextScope.CleanupLabels,
					CommandArgs: []string{deleteFlannelDefaultRouteCmd},
				},
			}
			netshoot.CreatePods(ctx, input.Client, testPodConfigs)
		})

		It("verify netshoot pods are running", func() {
			netshoot.WaitForPodsReady(ctx, input.Client, testPodConfigs, vpcutils.LongTimeout)
		})

		It("create DPU NAD for br-int", func() {
			ovnutils.CreateDPUIntergrationBridgeNetworkAttachmentDefinition(ctx, dpuClusterClient[0], dpfOperatorSystemNamespace, vpcOvnContextScope.CleanupLabels)
		})

		It("create dummy service consuming the SF", func() {
			createDummyDPUService(ctx, input.Client, dpfOperatorSystemNamespace, sfServiceName, vpcOvnContextScope.CleanupLabels, nil, serviceID, brIntNetwork, sfInterfaceName)
		})

		It("verify SF DPUServiceInterface is ready", func() {
			// SF ServiceInterfaces will be ready only when we the service pods are deployed.
			dpuservice.WaitForDPUServiceInterfacesReady(ctx, input.Client, dpuClusterClient[0], []string{sfName}, dpfOperatorSystemNamespace)
		})

		It("verify dummy service is ready", func() {
			dpuservice.WaitForDPUServices(ctx, input.Client, dpfOperatorSystemNamespace, []string{sfServiceName})
		})

		It("get SF pods ip addresses", func() {
			sfServiceLabels := map[string]string{
				dpfServiceIDLabelKey: serviceID,
			}

			Eventually(func(g Gomega) {
				sfPods = vpcutils.GetPodsMatchingLabels(ctx, dpuClusterClient[0], dpfOperatorSystemNamespace, sfServiceLabels)
				Expect(sfPods).To(HaveLen(2))
				for _, pod := range sfPods {
					Expect(pod.Spec.NodeName).ToNot(BeEmpty())
				}

				pod1SFIP = vpcutils.GetPodIPAddressFromNetworkStatus(ctx, dpuClusterClient[0], dpfOperatorSystemNamespace, sfPods[0].Name, sfInterfaceName)
				pod2SFIP = vpcutils.GetPodIPAddressFromNetworkStatus(ctx, dpuClusterClient[0], dpfOperatorSystemNamespace, sfPods[1].Name, sfInterfaceName)
				Expect(pod1SFIP).ToNot(BeEmpty())
				Expect(pod2SFIP).ToNot(BeEmpty())
			}, vpcutils.DefaultTimeout).Should(Succeed())
		})

		It("verify netshoot vfs can ping sf pods on same and cross nodes", func() {

			By(fmt.Sprintf("Pinging from pod %s on %s node to Service pod %s on node %s", podName1, hostWorkerNode1, sfPods[0].Name, sfPods[0].Spec.NodeName))
			netshoot.AssertPingSuccess(&hostClusterRESTClient, &input.RestConfig, vpcTrafficTestNS, podName1, pod1SFIP)

			By(fmt.Sprintf("Pinging from pod %s on %s node to Service pod %s on node %s", podName1, hostWorkerNode1, sfPods[1].Name, sfPods[1].Spec.NodeName))
			netshoot.AssertPingSuccess(&hostClusterRESTClient, &input.RestConfig, vpcTrafficTestNS, podName1, pod2SFIP)

			By(fmt.Sprintf("Pinging from pod %s on %s node to Service pod %s on node %s", podName2, hostWorkerNode2, sfPods[0].Name, sfPods[0].Spec.NodeName))
			netshoot.AssertPingSuccess(&hostClusterRESTClient, &input.RestConfig, vpcTrafficTestNS, podName2, pod1SFIP)

			By(fmt.Sprintf("Pinging from pod %s on %s node to Service pod %s on node %s", podName2, hostWorkerNode2, sfPods[1].Name, sfPods[1].Spec.NodeName))
			netshoot.AssertPingSuccess(&hostClusterRESTClient, &input.RestConfig, vpcTrafficTestNS, podName2, pod2SFIP)
		})
	})

	// Note: this Context should always run last as it modifies VPC prerequisites DPUServiceChain
	Context("Virtual network to external network traffic", Labels{Domain.RequiresNodes}, Ordered, func() {

		var (
			pf0vf2Worker1MacAddress string
			pod1IP                  string
			pod2IP                  string
			testPodConfigs          []*netshoot.TestPodConfig
			pf0vf7Worker2Labels     map[string]string
			contextHasFailed        bool
		)

		pf0vf7Worker2 := "pf0vf7-worker2"
		p0ToPf0Vf7Gw := "p0-to-pf0vf7-gw"

		AfterEach(func() {
			if CurrentSpecReport().Failed() {
				contextHasFailed = true
			}
		})

		AfterAll(func() {
			if contextHasFailed {
				By("VPC OVN: Report failure for this spec")
				reportAfterEach(CurrentSpecReport())
			}
			vpcOvnContextScope.CleanupAfter()
			cleanupDPUClusterNodeLabels(ctx)
		})

		It("label DPU nodes with tenant and tenant-node labels", func() {
			labelDPUNodesWithTenantAndTenantNode(ctx, dpuClusterClient[0], dpuNode1, dpuNode2, defaultTenant, "")
		})

		It("create OVNIsolationClass object", func() {
			createOVNIsolationClass(ctx, input.Client, ovnVPCProvisioner, vpcOvnContextScope.CleanupLabels)
		})

		It("create DPUVPC object", func() {
			createDPUVPC(ctx, input.Client, vpcName, defaultTenant, ovnVPCProvisioner, vpcOvnContextScope.CleanupLabels)
		})

		It("create DPUVirtualNetwork object", func() {
			createDPUVirtualNetwork(ctx, input.Client, testnet1, vpcName, defaultTenant, vnet1DefaultSubnet, vpcOvnContextScope.CleanupLabels)
		})

		It("verify DPUVirtualNetwork is ready", func() {
			ovnutils.WaitForDPUServiceVirtualNetworkReady(ctx, input.Client, testnet1, dpfOperatorSystemNamespace)
		})

		It("verify DPUVPC is ready", func() {
			ovnutils.WaitForDPUVPCReady(ctx, input.Client, vpcName, dpfOperatorSystemNamespace)
		})

		It("create DPUServiceInterfaces on the nodes, same virtual network", func() {
			pf0vf2Worker1Labels := map[string]string{
				ovnutils.InterfaceLabelKey: pf0vf2Worker1,
			}
			pf0vf7Worker2Labels = map[string]string{
				ovnutils.InterfaceLabelKey: pf0vf7Worker2,
			}

			createVPCDPUServiceInterface(ctx, input, dpuservice.TestDPUServiceInterfaceConfig{
				Name:           pf0vf2Worker1,
				Namespace:      dpfOperatorSystemNamespace,
				Labels:         cleanup.MergeMaps(vpcOvnContextScope.CleanupLabels, pf0vf2Worker1Labels),
				NodeName:       &dpuNode1.Name,
				Type:           dpuservicev1.InterfaceTypeVF,
				InterfaceName:  pf0vf2Worker1,
				PFIndex:        0,
				VFIndex:        2,
				VirtualNetwork: &testnet1,
			})
			// Creating pf0vf7 on second node that will not be part of the VPC that will simulated the endpoint for external network traffic
			createVPCDPUServiceInterface(ctx, input, dpuservice.TestDPUServiceInterfaceConfig{
				Name:          pf0vf7Worker2,
				Namespace:     dpfOperatorSystemNamespace,
				Labels:        cleanup.MergeMaps(vpcOvnContextScope.CleanupLabels, pf0vf7Worker2Labels),
				NodeName:      &dpuNode2.Name,
				Type:          dpuservicev1.InterfaceTypeVF,
				InterfaceName: pf0vf7Worker2,
				PFIndex:       0,
				VFIndex:       7,
			})
		})

		It("verify DPUServiceInterfaces are ready", func() {
			dpuServiceInterfaceNames := []string{pf0vf2Worker1, pf0vf7Worker2}
			dpuservice.WaitForDPUServiceInterfacesReady(ctx, input.Client, dpuClusterClient[0], dpuServiceInterfaceNames, dpfOperatorSystemNamespace)
		})

		It("create DPU service chain on second worker node for external network traffic", func() {
			createDPUServiceChainP0ToInterfaceMatchingLabels(ctx, input, p0ToPf0Vf7Gw, pf0vf7Worker2Labels, &dpuNode2.Name, vpcOvnContextScope.CleanupLabels)
		})

		It("reconfigure p0 to OVN VTEP external patch port DPU service chain to only exist on first node", func() {
			createOrUpdateVPCDPUServiceChain(ctx, input, &dpuNode1.Name)
		})

		It("wait for DPU service chains to be ready", func() {
			dpuservice.WaitForDPUServiceChainsReady(ctx, input.Client, dpuClusterClient[0], []string{p0ToPf0Vf7Gw, ovnutils.VpcOVNServiceChain}, dpfOperatorSystemNamespace, vpcutils.LongTimeout)
		})

		It("get the MAC addresses of the ServiceInterface objects", func() {
			pf0vf2Worker1Labels := map[string]string{
				ovnutils.InterfaceLabelKey: pf0vf2Worker1,
			}
			pf0vf2Worker1MacAddressesMap := ovnutils.GetServiceInterfaceMacAddressesMatchingLabels(ctx, dpuClusterClient[0], dpfOperatorSystemNamespace, pf0vf2Worker1Labels)

			Expect(pf0vf2Worker1MacAddressesMap).To(HaveLen(1))
			pf0vf2Worker1MacAddress = pf0vf2Worker1MacAddressesMap[dpuNode1.Name]
		})

		// Note: This is a workaround for testing to avoid rebooting the hosts.
		//  	MAC addresses will be set as part of the BFB, then when the host boots up, the mac address will be set.
		It("set host VF MAC addresses", func() {
			workerNode1, _ := getTwoWorkerNodeNames(ctx, input.Client)
			workerNode1IP := GetNodeInternalIP(ctx, input.Client, workerNode1)
			vpcutils.SetLinkMacAddress(workerNode1IP, hostPf0Vf2, pf0vf2Worker1MacAddress)
		})

		It("create netshoot pods and NetworkAttachmentDefinitions", func() {
			vpcutils.CreateTestNamespace(ctx, input.Client, vpcTrafficTestNS, vpcOvnContextScope.CleanupLabels)
			nadName1 := nadNamePrefix + podName1
			vpcutils.CreateDHCPNetworkAttachmentDefinition(ctx, input.Client, vpcTrafficTestNS, nadName1, hostPf0Vf2, ovnutils.VfsMTU, vpcOvnContextScope.CleanupLabels)
			gatewayIPCIDR := fmt.Sprintf("%s/%d", ovnutils.GatewayIPPoolGateway, ovnutils.GatewayMask)
			nadName2 := ovnutils.CreateExternalEndpointPodNetworkAttachmentDefinition(ctx, input.Client, vpcTrafficTestNS, podName2, 7, gatewayIPCIDR, vpcOvnContextScope.CleanupLabels)
			workerNode1, workerNode2 := getTwoWorkerNodeNames(ctx, input.Client)
			testPodConfigs = []*netshoot.TestPodConfig{
				{
					Namespace:   vpcTrafficTestNS,
					Name:        podName1,
					NodeName:    workerNode1,
					NADName:     nadName1,
					Labels:      vpcOvnContextScope.CleanupLabels,
					CommandArgs: []string{deleteFlannelDefaultRouteCmd},
				},
				{
					Namespace:   vpcTrafficTestNS,
					Name:        podName2,
					NodeName:    workerNode2,
					NADName:     nadName2,
					Labels:      vpcOvnContextScope.CleanupLabels,
					CommandArgs: []string{deleteFlannelDefaultRouteCmd},
				},
			}
			netshoot.CreatePods(ctx, input.Client, testPodConfigs)
		})

		It("verify netshoot pods are running", func() {
			netshoot.WaitForPodsReady(ctx, input.Client, testPodConfigs, vpcutils.LongTimeout)
		})

		It("get pod IP addresses", func() {
			pod1IP = vpcutils.GetPodIPAddressFromNetworkStatus(ctx, input.Client, vpcTrafficTestNS, podName1, vfDefaultInterfaceName)
			pod2IP = vpcutils.GetPodIPAddressFromNetworkStatus(ctx, input.Client, vpcTrafficTestNS, podName2, vfDefaultInterfaceName)
			Expect(pod1IP).ToNot(BeEmpty())
			Expect(pod2IP).ToNot(BeEmpty())
		})

		It("verify netshoot vf pod can ping external network pod", func() {
			netshoot.AssertPingSuccess(&hostClusterRESTClient, &input.RestConfig, vpcTrafficTestNS, podName1, pod2IP)
		})

		It("verify performance with iperf to external network traffic", func() {
			netshoot.RunTrafficTest(&hostClusterRESTClient, &input.RestConfig, vpcTrafficTestNS, podName1, podName2, pod2IP)
		})

		It("revert p0 to OVN VTEP external patch port DPU service chain to its original configuration", func() {
			createOrUpdateVPCDPUServiceChain(ctx, input, nil)
			dpuservice.WaitForDPUServiceChainsReady(ctx, input.Client, dpuClusterClient[0], []string{ovnutils.VpcOVNServiceChain}, dpfOperatorSystemNamespace, vpcutils.LongTimeout)
		})
	})
})
