/*
SPDX-FileCopyrightText: Copyright (c) 2025 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
SPDX-License-Identifier: LicenseRef-NvidiaProprietary

NVIDIA CORPORATION, its affiliates and licensors retain all intellectual
property and proprietary rights in and to this material, related
documentation and any modifications thereto. Any use, reproduction,
disclosure or distribution of this material and related documentation
 without an express license agreement from NVIDIA CORPORATION or
its affiliates is strictly prohibited.
*/

package topology_test

import (
	"fmt"
	"net"
	"strings"

	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/common"
	ipallocator "gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/ip"
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/ipmanager"
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/nbdb"
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/ovnlib"
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/topology"
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/topology/sets"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8ssets "k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//nolint:goconst
var _ = Describe("Topology Manager", func() {
	var (
		tm topology.Manager
	)

	BeforeEach(func() {
		tm = topology.NewManager(ovnClient)
	})

	AfterEach(func() {
		Expect(ovnClient.ClearAll(ctx)).To(Succeed())
	})

	Context("ApplyTopology - Bridged Network", func() {
		It("should apply the logical topology to OVN for the VPC and virtual networks - no network", func() {
			testTopology := NewBridgedTestTopology("vpc", false)
			Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, nil)).To(Succeed())
			testTopology.GetExpectedOvnTopology().validate()
		})

		It("should apply the logical topology to OVN for the VPC and virtual networks - single network", func() {
			testTopology := NewBridgedTestTopology("vpc", false).AddVirtualNetworks(1, true, false, false)
			Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, nil)).To(Succeed())
			testTopology.GetExpectedOvnTopology().validate()
		})

		It("should apply the logical topology to OVN for the VPC and virtual networks - no access between networks", func() {
			testTopology := NewBridgedTestTopology("vpc", false).AddVirtualNetworks(3, true, false, false)
			Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, nil)).To(Succeed())
			testTopology.GetExpectedOvnTopology().validate()
		})

		It("should apply the logical topology to OVN for the VPC and virtual networks - with access between networks", func() {
			testTopology := NewBridgedTestTopology("vpc", true).AddVirtualNetworks(3, true, false, false)
			Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, nil)).To(Succeed())
			testTopology.GetExpectedOvnTopology().validate()
		})

		It("should update the logical topology in OVN for the VPC and virtual networks - add virtual network", func() {
			testTopology := NewBridgedTestTopology("vpc", true).AddVirtualNetworks(2, true, false, false)
			Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, nil)).To(Succeed())
			testTopology.AddVirtualNetworks(1, true, false, false)
			Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, nil)).To(Succeed())
			testTopology.GetExpectedOvnTopology().validate()
		})

		It("should update the logical topology in OVN for the VPC and virtual networks - remove virtual network", func() {
			testTopology := NewBridgedTestTopology("vpc", true).AddVirtualNetworks(3, true, false, false)
			Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, nil)).To(Succeed())
			testTopology = NewBridgedTestTopology("vpc", true).AddVirtualNetworks(2, true, false, false)
			Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, nil)).To(Succeed())
			testTopology.GetExpectedOvnTopology().validate()
		})

		It("should update the logical topology in OVN for the VPC and virtual networks - change interNetworkAccess", func() {
			testTopology := NewBridgedTestTopology("vpc", false).AddVirtualNetworks(3, true, false, false)
			Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, nil)).To(Succeed())
			testTopology = NewBridgedTestTopology("vpc", true).AddVirtualNetworks(3, true, false, false)
			Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, nil)).To(Succeed())
			testTopology.GetExpectedOvnTopology().validate()
		})

		It("should update the logical topology in OVN for the VPC and virtual networks - remove all networks", func() {
			testTopology := NewBridgedTestTopology("vpc", false).AddVirtualNetworks(3, true, false, false)
			Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, nil)).To(Succeed())
			testTopology = NewBridgedTestTopology("vpc", false)
			Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, nil)).To(Succeed())
			testTopology.GetExpectedOvnTopology().validate()
		})

		It("should update the logical topology in OVN for the VPC and virtual networks - add multiple networks", func() {
			testTopology := NewBridgedTestTopology("vpc", false)
			Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, nil)).To(Succeed())
			testTopology.AddVirtualNetworks(3, true, false, false)
			Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, nil)).To(Succeed())
			testTopology.GetExpectedOvnTopology().validate()
		})

		It("should apply the logical topology to OVN for the VPC and virtual networks - DHCP disabled", func() {
			testTopology := NewBridgedTestTopology("vpc", false).AddVirtualNetworks(2, false, false, false)
			Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, nil)).To(Succeed())
			testTopology.GetExpectedOvnTopology().validate()
		})

		It("should apply the logical toplogy to OVN for multiple VPCs", func() {
			testTopology1 := NewBridgedTestTopology("vpc1", false).AddVirtualNetworks(2, false, false, false)
			testTopology2 := NewBridgedTestTopology("vpc2", true).AddVirtualNetworks(2, true, false, false)
			Expect(tm.ApplyTopology(ctx, testTopology1.vpc, testTopology1.vns, nil)).To(Succeed())
			Expect(tm.ApplyTopology(ctx, testTopology2.vpc, testTopology2.vns, nil)).To(Succeed())
			testTopology1.GetExpectedOvnTopology().validate()
			testTopology2.GetExpectedOvnTopology().validate()
		})

		Context("External connectivity", func() {
			It("should apply the logical topology to OVN with external connectivity", func() {
				testTopology := NewBridgedTestTopology("vpc", true).AddVirtualNetworks(2, true, true, false).AddNodes(2)
				Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, testTopology.nodes)).To(Succeed())
				testTopology.GetExpectedOvnTopology().validate()
			})

			It("should apply the logical topology to OVN with external connectivity and NAT", func() {
				testTopology := NewBridgedTestTopology("vpc", true).AddVirtualNetworks(2, true, true, true).AddNodes(2)
				Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, testTopology.nodes)).To(Succeed())
				testTopology.GetExpectedOvnTopology().validate()
			})

			It("should apply the logical topology to OVN some with external connectivity and NAT some without", func() {
				testTopology := NewBridgedTestTopology("vpc", true).
					AddVirtualNetworks(2, true, true, true).
					AddVirtualNetworks(2, true, true, false).
					AddNodes(2)
				Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, testTopology.nodes)).To(Succeed())
				testTopology.GetExpectedOvnTopology().validate()
			})
		})

		Context("Error flows", func() {
			It("should fail if vpc is missing LRP address annotation", func() {
				testTopology := NewBridgedTestTopology("vpc", false).AddVirtualNetworks(1, true, false, false)
				delete(testTopology.vpc.Annotations, common.LRPAddressesAnnotationKey)
				Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, nil)).NotTo(Succeed())
			})

			It("should fail if vpc LRP address annotation is invalid", func() {
				testTopology := NewBridgedTestTopology("vpc", false).AddVirtualNetworks(1, true, false, false)
				testTopology.vpc.Annotations[common.LRPAddressesAnnotationKey] = "garbage"
				Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, nil)).NotTo(Succeed())
			})

			It("should fail if virtual network is missing LRP address annotation", func() {
				testTopology := NewBridgedTestTopology("vpc", false).AddVirtualNetworks(1, true, false, false)
				delete(testTopology.vns[0].Annotations, common.LRPAddressesAnnotationKey)
				Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, nil)).NotTo(Succeed())
			})

			It("should fail if virtual network LRP address annotation is invalid", func() {
				testTopology := NewBridgedTestTopology("vpc", false).AddVirtualNetworks(1, true, false, false)
				testTopology.vns[0].Annotations[common.LRPAddressesAnnotationKey] = "garbage"
				Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, nil)).NotTo(Succeed())
			})

			It("should fail if node is missing LRP address annotation", func() {
				testTopology := NewBridgedTestTopology("vpc", false).AddVirtualNetworks(1, true, true, true).AddNodes(1)
				delete(testTopology.nodes[0].Annotations, common.LRPAddressesAnnotationKey)
				Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, testTopology.nodes)).NotTo(Succeed())
			})

			It("should fail if node LRP address annotation is invalid", func() {
				testTopology := NewBridgedTestTopology("vpc", false).AddVirtualNetworks(1, true, true, true).AddNodes(1)
				testTopology.nodes[0].Annotations[common.LRPAddressesAnnotationKey] = "garbage"
				Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, testTopology.nodes)).NotTo(Succeed())
			})

			It("should fail if node is missing ovn chassis id annotation", func() {
				testTopology := NewBridgedTestTopology("vpc", false).AddVirtualNetworks(1, true, true, true).AddNodes(1)
				delete(testTopology.nodes[0].Annotations, common.OVNChassisIDAnnotationKey)
				Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, testTopology.nodes)).NotTo(Succeed())
			})

			It("should fail if node is missing ovn gateway config annotation", func() {
				testTopology := NewBridgedTestTopology("vpc", false).AddVirtualNetworks(1, true, true, true).AddNodes(1)
				delete(testTopology.nodes[0].Annotations, common.OVNGatewayConfigAnnotationKey)
				Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, testTopology.nodes)).NotTo(Succeed())
			})

			It("should fail if node ovn gateway config annotation is invalid", func() {
				testTopology := NewBridgedTestTopology("vpc", false).AddVirtualNetworks(1, true, true, true).AddNodes(1)
				testTopology.nodes[0].Annotations[common.OVNGatewayConfigAnnotationKey] = "invalid"
				Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, testTopology.nodes)).NotTo(Succeed())
			})
		})
	})

	Context("RemoveTopology", func() {
		It("should remove OVN topology for the VPC when deleted", func() {
			testTopology := NewBridgedTestTopology("vpc", true).AddVirtualNetworks(3, true, false, false)
			Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, nil)).To(Succeed())
			emptyExpected := newExpectedOvnTopology()
			Expect(tm.RemoveTopology(ctx, testTopology.vpc)).To(Succeed())
			emptyExpected.validate()
		})

		It("should remove OVN topology for the VPC when deleted - with external connectivity", func() {
			testTopology := NewBridgedTestTopology("vpc", true).AddVirtualNetworks(3, true, true, true).AddNodes(2)
			Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, nil)).To(Succeed())
			emptyExpected := newExpectedOvnTopology()
			Expect(tm.RemoveTopology(ctx, testTopology.vpc)).To(Succeed())
			emptyExpected.validate()
		})

		It("should succeed if VPC OVN topology does not exist", func() {
			testTopology := NewBridgedTestTopology("vpc", true)
			Expect(tm.RemoveTopology(ctx, testTopology.vpc)).To(Succeed())
		})
	})

	Context("PlugServiceInterface", func() {
		It("Should plug service interface to VPC - DHCP enabled", func() {
			testTopology := NewBridgedTestTopology("vpc", false).AddVirtualNetworks(1, true, false, false)
			Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, nil)).To(Succeed())
			serviceIfcs := getTestServiceInterfaces(1, testTopology.vns[0].Name, false)
			Expect(tm.PlugServiceInterface(ctx, testTopology.vpc, testTopology.vns[0], nil, serviceIfcs[0])).To(Succeed())
			validateServiceInterfacePlugged(testTopology.vpc, serviceIfcs[0], true, false)
		})

		It("Should plug service interface to VPC - DHCP disabled", func() {
			testTopology := NewBridgedTestTopology("vpc", false).AddVirtualNetworks(1, false, false, false)
			Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, nil)).To(Succeed())
			serviceIfcs := getTestServiceInterfaces(1, testTopology.vns[0].Name, false)
			Expect(tm.PlugServiceInterface(ctx, testTopology.vpc, testTopology.vns[0], nil, serviceIfcs[0])).To(Succeed())
			validateServiceInterfacePlugged(testTopology.vpc, serviceIfcs[0], false, false)
		})

		It("Should plug service interface to VPC - unknown MAC, DHCP enabled", func() {
			testTopology := NewBridgedTestTopology("vpc", false).AddVirtualNetworks(1, true, false, false)
			Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, nil)).To(Succeed())
			serviceIfcs := getTestServiceInterfaces(1, testTopology.vns[0].Name, true)
			serviceIfc := serviceIfcs[0]
			Expect(tm.PlugServiceInterface(ctx, testTopology.vpc, testTopology.vns[0], nil, serviceIfc)).To(Succeed())

			//validate
			p, err := ovnClient.GetLogicalSwitchPort(ctx, &nbdb.LogicalSwitchPortGetParams{Name: common.ServiceInterfacePortName(serviceIfc)})
			Expect(err).ToNot(HaveOccurred())
			Expect(p.Addresses).ToNot(BeNil())
			Expect(p.Addresses[0]).To(Equal("unknown"))
			Expect(p.DynamicAddresses).To(BeNil())
		})

		It("Should plug service interface to VPC - unknown MAC, DHCP disabled", func() {
			testTopology := NewBridgedTestTopology("vpc", false).AddVirtualNetworks(1, false, false, false)
			Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, nil)).To(Succeed())
			serviceIfcs := getTestServiceInterfaces(1, testTopology.vns[0].Name, true)
			serviceIfc := serviceIfcs[0]
			Expect(tm.PlugServiceInterface(ctx, testTopology.vpc, testTopology.vns[0], nil, serviceIfc)).To(Succeed())

			//validate
			p, err := ovnClient.GetLogicalSwitchPort(ctx, &nbdb.LogicalSwitchPortGetParams{Name: common.ServiceInterfacePortName(serviceIfc)})
			Expect(err).ToNot(HaveOccurred())
			Expect(p.Addresses).ToNot(BeNil())
			Expect(p.Addresses[0]).To(Equal("unknown"))
			Expect(p.DynamicAddresses).To(BeNil())
		})

		It("Should plug service interface to VPC - external connectivity, DHCP enabled", func() {
			testTopology := NewBridgedTestTopology("vpc", false).AddVirtualNetworks(1, true, true, true).AddNodes(1)
			Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, nil)).To(Succeed())
			serviceIfcs := getTestServiceInterfaces(1, testTopology.vns[0].Name, false)
			Expect(tm.PlugServiceInterface(ctx, testTopology.vpc, testTopology.vns[0], testTopology.nodes[0], serviceIfcs[0])).To(Succeed())
			validateServiceInterfacePlugged(testTopology.vpc, serviceIfcs[0], true, true)
		})

		It("Should plug service interface to VPC - external connectivity, DHCP disabled", func() {
			testTopology := NewBridgedTestTopology("vpc", false).AddVirtualNetworks(1, false, true, true).AddNodes(1)
			Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, nil)).To(Succeed())
			serviceIfcs := getTestServiceInterfaces(1, testTopology.vns[0].Name, false)
			Expect(tm.PlugServiceInterface(ctx, testTopology.vpc, testTopology.vns[0], testTopology.nodes[0], serviceIfcs[0])).To(Succeed())
			validateServiceInterfacePlugged(testTopology.vpc, serviceIfcs[0], false, true)
		})

		It("Should fail if mac address annotation is missing", func() {
			testTopology := NewBridgedTestTopology("vpc", false).AddVirtualNetworks(1, true, false, false)
			Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, nil)).To(Succeed())
			serviceIfcs := getTestServiceInterfaces(1, testTopology.vns[0].Name, false)
			serviceIfc := serviceIfcs[0]
			delete(serviceIfc.Annotations, common.LSPMACAddressAnnotationKey)
			Expect(tm.PlugServiceInterface(ctx, testTopology.vpc, testTopology.vns[0], nil, serviceIfc)).NotTo(Succeed())
		})

		It("Should fail if mac address annotation is empty", func() {
			testTopology := NewBridgedTestTopology("vpc", false).AddVirtualNetworks(1, true, false, false)
			Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, nil)).To(Succeed())
			serviceIfcs := getTestServiceInterfaces(1, testTopology.vns[0].Name, false)
			serviceIfc := serviceIfcs[0]
			serviceIfc.Annotations[common.LSPMACAddressAnnotationKey] = ""
			Expect(tm.PlugServiceInterface(ctx, testTopology.vpc, testTopology.vns[0], nil, serviceIfc)).NotTo(Succeed())
		})

		It("Should fail if mac address annotation is invalid", func() {
			testTopology := NewBridgedTestTopology("vpc", false).AddVirtualNetworks(1, true, false, false)
			Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, nil)).To(Succeed())
			serviceIfcs := getTestServiceInterfaces(1, testTopology.vns[0].Name, false)
			serviceIfc := serviceIfcs[0]
			serviceIfc.Annotations[common.LSPMACAddressAnnotationKey] = "garbage"
			Expect(tm.PlugServiceInterface(ctx, testTopology.vpc, testTopology.vns[0], nil, serviceIfc)).NotTo(Succeed())
		})

		It("Should fail if invalid or missing LRP address annotation if externally routed set on virtual network", func() {
			testTopology := NewBridgedTestTopology("vpc", false).AddVirtualNetworks(1, true, true, true).AddNodes(1)
			Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, nil)).To(Succeed())
			serviceIfcs := getTestServiceInterfaces(1, testTopology.vns[0].Name, false)
			testTopology.nodes[0].Annotations[common.LRPAddressesAnnotationKey] = "invalid"
			Expect(tm.PlugServiceInterface(ctx, testTopology.vpc, testTopology.vns[0], testTopology.nodes[0], serviceIfcs[0])).NotTo(Succeed())
			delete(testTopology.nodes[0].Annotations, common.LRPAddressesAnnotationKey)
			Expect(tm.PlugServiceInterface(ctx, testTopology.vpc, testTopology.vns[0], testTopology.nodes[0], serviceIfcs[0])).NotTo(Succeed())
		})
	})

	Context("UnplugServiceInterface", func() {
		It("Should Remove Service Interface from VPC", func() {
			testTopology := NewBridgedTestTopology("vpc", false).AddVirtualNetworks(1, true, false, false)
			Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, nil)).To(Succeed())
			serviceIfcs := getTestServiceInterfaces(1, testTopology.vns[0].Name, false)
			serviceIfc := serviceIfcs[0]
			Expect(tm.PlugServiceInterface(ctx, testTopology.vpc, testTopology.vns[0], nil, serviceIfc)).To(Succeed())
			Expect(tm.UnplugServiceInterface(ctx, testTopology.vns[0], serviceIfc)).To(Succeed())
			validateServiceInterfaceUnplugged(testTopology.vpc, serviceIfc)
		})

		It("Should Remove Service Interface from VPC with external connectivity", func() {
			testTopology := NewBridgedTestTopology("vpc", false).AddVirtualNetworks(1, true, true, true).AddNodes(1)
			Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, nil)).To(Succeed())
			serviceIfcs := getTestServiceInterfaces(1, testTopology.vns[0].Name, false)
			serviceIfc := serviceIfcs[0]
			Expect(tm.PlugServiceInterface(ctx, testTopology.vpc, testTopology.vns[0], testTopology.nodes[0], serviceIfc)).To(Succeed())
			Expect(tm.UnplugServiceInterface(ctx, testTopology.vns[0], serviceIfc)).To(Succeed())
			validateServiceInterfaceUnplugged(testTopology.vpc, serviceIfc)
		})

		It("Should Succeed when Service Interface is not plugged to ovn", func() {
			testTopology := NewBridgedTestTopology("vpc", false).AddVirtualNetworks(1, true, false, false)
			Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, nil)).To(Succeed())
			serviceIfcs := getTestServiceInterfaces(1, testTopology.vns[0].Name, false)
			Expect(tm.UnplugServiceInterface(ctx, testTopology.vns[0], serviceIfcs[0])).To(Succeed())
		})
	})

	Context("ListVPCs", func() {
		It("should list all VPCs in OVN", func() {
			testTopology1 := NewBridgedTestTopology("vpc1", false).AddVirtualNetworks(3, true, false, false)
			testTopology2 := NewBridgedTestTopology("vpc2", false).AddVirtualNetworks(3, true, false, false)
			Expect(tm.ApplyTopology(ctx, testTopology1.vpc, testTopology1.vns, nil)).To(Succeed())
			Expect(tm.ApplyTopology(ctx, testTopology2.vpc, testTopology2.vns, nil)).To(Succeed())
			Expect(tm.ListVPCs(ctx)).To(And(
				HaveLen(2),
				ContainElements(
					client.ObjectKeyFromObject(testTopology1.vpc),
					client.ObjectKeyFromObject(testTopology2.vpc))))
		})
	})

	Context("ListServiceInterfaces", func() {
		It("should list all service interfaces in OVN", func() {
			testTopology := NewBridgedTestTopology("vpc", false).AddVirtualNetworks(1, true, false, false)
			Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, nil)).To(Succeed())

			// create and plug service interfaces
			serviceIfcs := getTestServiceInterfaces(2, testTopology.vns[0].Name, true)
			for _, serviceIfc := range serviceIfcs {
				Expect(tm.PlugServiceInterface(ctx, testTopology.vpc, testTopology.vns[0], nil, serviceIfc)).To(Succeed())
			}

			Expect(tm.ListServiceInterfaces(ctx)).To(And(
				HaveLen(2),
				ContainElements(
					topology.ServiceInterfacRef{
						ServiceInterface: client.ObjectKeyFromObject(serviceIfcs[0]),
						VirtualNetwork:   client.ObjectKeyFromObject(testTopology.vns[0]),
						VPC:              client.ObjectKeyFromObject(testTopology.vpc),
					},
					topology.ServiceInterfacRef{
						ServiceInterface: client.ObjectKeyFromObject(serviceIfcs[1]),
						VirtualNetwork:   client.ObjectKeyFromObject(testTopology.vns[0]),
						VPC:              client.ObjectKeyFromObject(testTopology.vpc),
					})))
		})
	})

	Context("\"Scale\" testing", func() {
		It("should create N vpcs with K networks and L nodes - Bridged topology", func() {
			// number of VPCs in the topology
			numVPCs := 3
			// number of virtual networks. Cannot exceed 250 because of createBridgedTestTopology implementation
			numNetworks := 10
			// number of nodes per VPC
			numNodesPerVPC := 10

			// NOTE(adrianc): going much higher for a single VPC is slow since we create MAX(O(K^2),O(K*L)) policy rules

			var testTopologies []*testTopology

			// construct test topologies
			for i := range numVPCs {
				testTopology := NewBridgedTestTopology(fmt.Sprintf("vpc%d", i), true).
					AddVirtualNetworks(numNetworks, true, true, true).
					AddNodes(numNodesPerVPC)
				testTopologies = append(testTopologies, testTopology)
			}

			// apply test topologies
			for _, testTopology := range testTopologies {
				Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, testTopology.nodes)).To(Succeed())
			}

			// validate expected ovn topology
			for _, testTopology := range testTopologies {
				testTopology.GetExpectedOvnTopology().validate()
			}
		})

		It("should plug N service interfaces", func() {
			// number of VPCs in the topology
			numVPCs := 1
			// number of interfaces per VPC
			numInterfacesPerVPC := 10
			// num interface per vn. Cannot exceed 250 because of createBridgedTestTopology implementation
			numInterfacePerVn := 10
			// number of VNs in each VPC
			numVnsPerVPC := (numInterfacesPerVPC / numInterfacePerVn) + 1

			// create test topology
			var testTopologies []*testTopology
			for i := range numVPCs {
				testTopology := NewBridgedTestTopology(fmt.Sprintf("vpc%d", i), true).
					AddVirtualNetworks(numVnsPerVPC, true, true, true).
					AddNodes(1)
				Expect(tm.ApplyTopology(ctx, testTopology.vpc, testTopology.vns, testTopology.nodes)).To(Succeed())
				testTopologies = append(testTopologies, testTopology)
			}

			for i := range numVPCs {
				// create service interface
				serviceIfcs := make([]*dpuservicev1.ServiceInterface, 0, numInterfacesPerVPC)
				for _, vn := range testTopologies[i].vns {
					serviceIfcs = append(serviceIfcs, getTestServiceInterfaces(numInterfacePerVn, vn.Name, false)...)
				}

				// plug and validate
				for j, serviceIfc := range serviceIfcs {
					Expect(tm.PlugServiceInterface(ctx, testTopologies[i].vpc, testTopologies[i].vns[j/numInterfacePerVn], testTopologies[i].nodes[0], serviceIfc)).To(Succeed())
					validateServiceInterfacePlugged(testTopologies[i].vpc, serviceIfc, true, true)
				}
			}
		})
	})
})

type testTopology struct {
	vpc   *vpcv1.DPUVPC
	vns   []*vpcv1.DPUVirtualNetwork
	nodes []*corev1.Node

	vpcIPAllocator  ipallocator.IPAllocator
	nodeIPAllocator ipallocator.IPAllocator
}

// NewBridgedTestTopology creates a new test topology for a bridged virtual network
func NewBridgedTestTopology(vpcName string, interNetworkAccess bool) *testTopology {
	GinkgoHelper()

	// initialize vpc IP allocator
	_, ipn, err := net.ParseCIDR("10.64.0.0/16")
	Expect(err).ToNot(HaveOccurred())
	rs := &ipallocator.RangeSet{
		ipallocator.Range{Subnet: *ipn, Gateway: ipallocator.NextIP(ipn.IP)},
	}
	Expect(rs.Canonicalize()).ToNot(HaveOccurred())
	vpcIPAllocator := ipallocator.NewIPAllocator(rs, nil)

	// initialize node IP allocator
	_, nodeIpn, err := net.ParseCIDR("20.20.0.0/16")
	Expect(err).ToNot(HaveOccurred())
	nrs := &ipallocator.RangeSet{
		ipallocator.Range{Subnet: *nodeIpn, Gateway: ipallocator.NextIP(nodeIpn.IP)},
	}
	Expect(nrs.Canonicalize()).ToNot(HaveOccurred())
	nodeIPAllocator := ipallocator.NewIPAllocator(nrs, nil)

	vpc := &vpcv1.DPUVPC{
		TypeMeta: metav1.TypeMeta{
			Kind: vpcv1.DPUVPCGroupVersionKind.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        vpcName,
			Namespace:   "default",
			Annotations: map[string]string{},
		},
		Spec: vpcv1.DPUVPCSpec{
			InterNetworkAccess: interNetworkAccess,
		},
	}

	ipa, err := vpcIPAllocator.AllocateGateway(ipmanager.ObjToID(vpc))
	Expect(err).ToNot(HaveOccurred())

	vpc.Annotations[common.LRPAddressesAnnotationKey] = fmt.Sprintf(`{"ipv4": "%s"}`, ipa.Address.String())

	return &testTopology{
		vpc: vpc,

		vpcIPAllocator:  vpcIPAllocator,
		nodeIPAllocator: nodeIPAllocator,
	}
}

func (t *testTopology) AddVirtualNetworks(numNetworks int, dhcpEnabled bool, externallyRouted bool, masquerade bool) *testTopology {
	GinkgoHelper()

	if masquerade && !externallyRouted {
		GinkgoT().Fatal("masquerade is only supported for externally routed virtual networks")
	}

	if len(t.vns)+numNetworks > 254 {
		GinkgoT().Fatal("total number of networks must be less than 255")
	}

	currNumVns := len(t.vns)
	for i := range numNetworks {
		vn := &vpcv1.DPUVirtualNetwork{
			TypeMeta: metav1.TypeMeta{
				Kind: vpcv1.DPUVirtualNetworkGroupVersionKind.Kind,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        fmt.Sprintf("%s-vn%d", t.vpc.Name, currNumVns+i),
				Namespace:   "default",
				Annotations: map[string]string{},
			},
			Spec: vpcv1.DPUVirtualNetworkSpec{
				Type:             vpcv1.BridgedVirtualNetworkType,
				ExternallyRouted: externallyRouted,
				Masquerade:       ptr.To(masquerade),
				VPCName:          t.vpc.Name,
				BridgedNetwork: &vpcv1.BridgedNetworkSpec{
					IPAM: &vpcv1.BridgedNetworkIPAMSpec{
						IPv4: &vpcv1.BridgedNetworkIPAMIPv4Spec{
							DHCP:   dhcpEnabled,
							Subnet: fmt.Sprintf("192.%d.0.1/16", currNumVns+i),
							ExcludeIPs: []vpcv1.ExcludeIPsEntry{
								{
									IP: ptr.To(fmt.Sprintf("192.%d.0.2", currNumVns+i)),
									Range: &vpcv1.RangeEntry{
										Start: fmt.Sprintf("192.%d.0.10", currNumVns+i),
										End:   fmt.Sprintf("192.%d.0.20", currNumVns+i),
									},
								},
							},
						},
					},
				},
			},
		}
		ipa, err := t.vpcIPAllocator.Allocate(ipmanager.ObjToID(vn), nil)
		Expect(err).ToNot(HaveOccurred())
		vn.Annotations[common.LRPAddressesAnnotationKey] = fmt.Sprintf(`{"ipv4": "%s"}`, ipa.Address.String())
		t.vns = append(t.vns, vn)
	}

	return t
}

func (t *testTopology) AddNodes(numNodes int) *testTopology {
	GinkgoHelper()

	if numNodes > 254 {
		GinkgoT().Fatal("total number of nodes must be less than 255")
	}

	for i := range numNodes {
		node := &corev1.Node{
			TypeMeta: metav1.TypeMeta{
				Kind: "Node",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        fmt.Sprintf("%s-node-%d", t.vpc.Name, i),
				Namespace:   "default",
				Annotations: map[string]string{},
			},
		}
		// allocate vpc ip
		ipa, err := t.vpcIPAllocator.Allocate(ipmanager.ObjToID(node), nil)
		Expect(err).ToNot(HaveOccurred())
		node.Annotations[common.LRPAddressesAnnotationKey] = fmt.Sprintf(`{"ipv4": "%s"}`, ipa.Address.String())

		// set gateway info
		ipa, err = t.nodeIPAllocator.Allocate(ipmanager.ObjToID(node), nil)
		Expect(err).ToNot(HaveOccurred())
		gatewayConfig := common.GatewayConfig{
			MAC: common.IPtoMAC(ipa.Address.IP).String(),
			IP: common.IPNetConfiguration{
				IPv4: ipa.Address.String(),
			},
			NextHop: common.IPConfiguration{
				IPv4: ipa.Gateway.String(),
			},
		}
		Expect(common.GatewayConfigToAnnotation(gatewayConfig, node.Annotations)).To(Succeed())

		// set chassis ID
		node.Annotations[common.OVNChassisIDAnnotationKey] = node.Name

		t.nodes = append(t.nodes, node)
	}

	return t
}

func (t *testTopology) GetExpectedOvnTopology() *expectedOvnTopology {
	GinkgoHelper()
	expected := newExpectedOvnTopology().withVPCRef(fmt.Sprintf("%s/%s", t.vpc.Namespace, t.vpc.Name))

	// Cluster Router and Switch and interconnecting ports
	expected.
		withLR(common.VPCRouterName(t.vpc), nil).
		withLS(common.VPCSwitchName(t.vpc), nil).
		withLSP(common.SwitchToRouterPortName(common.VPCSwitchName(t.vpc), common.VPCRouterName(t.vpc))).
		withLRP(common.RouterToSwitchPortName(common.VPCRouterName(t.vpc), common.VPCSwitchName(t.vpc)))

	// Virtual Network Routers and Switches and interconnecting ports
	for i, vn := range t.vns {
		var si *switchInfo
		if vn.Spec.BridgedNetwork.IPAM.IPv4.DHCP {
			si = &switchInfo{
				otherConfig: map[string]string{
					"subnet":      vn.GetIPv4Subnet(),
					"exclude_ips": fmt.Sprintf("192.%d.0.1 192.%d.0.2 192.%d.0.10..192.%d.0.20", i, i, i, i),
				},
			}
		}

		expected.
			withLR(common.VirtualNetworkRouterName(vn), nil).
			withLS(common.VirtualNetworkSwitchName(vn), si).
			withLSP(common.SwitchToRouterPortName(common.VirtualNetworkSwitchName(vn), common.VirtualNetworkRouterName(vn))).
			withLRP(common.RouterToSwitchPortName(common.VirtualNetworkRouterName(vn), common.VirtualNetworkSwitchName(vn)))
	}

	// ports connecting virtual network router to/from cluster switch
	for _, vn := range t.vns {
		expected.
			withLSP(common.SwitchToRouterPortName(common.VPCSwitchName(t.vpc), common.VirtualNetworkRouterName(vn))).
			withLRP(common.RouterToSwitchPortName(common.VirtualNetworkRouterName(vn), common.VPCSwitchName(t.vpc)))
	}

	// gateway routers and switches
	for _, node := range t.nodes {
		routerOpts := map[string]string{
			topology.GRChassisKey:                   node.Name,
			topology.GRMACBindingAgeThresholdKey:    topology.GRMACBindingAgeThreshold,
			topology.GRDynamicNeighRoutersKey:       "true",
			topology.GRAlwaysLearnFromARPRequestKey: "false",
		}
		expected.
			withLR(common.GatewayRouterName(t.vpc, node), &routerInfo{options: routerOpts}).
			withLS(common.GatewaySwitchName(t.vpc, node), nil).
			withLSP(common.SwitchToRouterPortName(common.GatewaySwitchName(t.vpc, node), common.GatewayRouterName(t.vpc, node))).
			withLRP(common.RouterToSwitchPortName(common.GatewayRouterName(t.vpc, node), common.GatewaySwitchName(t.vpc, node))).
			// GR to VPC switch ports
			withLSP(common.SwitchToRouterPortName(common.VPCSwitchName(t.vpc), common.GatewayRouterName(t.vpc, node))).
			withLRP(common.RouterToSwitchPortName(common.GatewayRouterName(t.vpc, node), common.VPCSwitchName(t.vpc))).
			// GS localnet port
			withLSP(common.GatewaySwitchLocalnetPortName(common.GatewaySwitchName(t.vpc, node)))
	}

	// DHCP Options
	for _, vn := range t.vns {
		if !vn.Spec.BridgedNetwork.IPAM.IPv4.DHCP {
			continue
		}
		_, ipn, err := net.ParseCIDR(vn.GetIPv4Subnet())
		Expect(err).ToNot(HaveOccurred())
		expected.withDhcpOptionsCidr(ipn.String())
	}

	// Static Routes:

	// routes for vpc router:
	// dummy route
	expected.withRouteForRouter(common.VPCRouterName(t.vpc), &nbdb.LogicalRouterStaticRoute{
		IPPrefix:    "0.0.0.0/0",
		Nexthop:     "10.64.255.254",
		ExternalIDs: topology.NewExternalIDs().WithVPCRef(t.vpc).WithOwnerRef(t.vpc),
	})
	// routes for each virtual network router in vpc router
	for _, vn := range t.vns {
		vnIPA := t.vpcIPAllocator.GetAllocation(ipmanager.ObjToID(vn))
		Expect(vnIPA).ToNot(BeNil())

		expected.withRouteForRouter(common.VPCRouterName(t.vpc), &nbdb.LogicalRouterStaticRoute{
			IPPrefix:    vn.GetIPv4Subnet(),
			Nexthop:     vnIPA.Address.IP.String(),
			ExternalIDs: topology.NewExternalIDs().WithVPCRef(t.vpc).WithOwnerRef(vn),
		})
	}

	// routes for network routers, default via cluster router
	vpcIPA := t.vpcIPAllocator.GetAllocation(ipmanager.ObjToID(t.vpc))
	Expect(vpcIPA).ToNot(BeNil())
	for _, vn := range t.vns {
		expected.withRouteForRouter(common.VirtualNetworkRouterName(vn), &nbdb.LogicalRouterStaticRoute{
			IPPrefix:    "0.0.0.0/0",
			Nexthop:     vpcIPA.Address.IP.String(),
			ExternalIDs: topology.NewExternalIDs().WithVPCRef(t.vpc).WithOwnerRef(vn),
		})
	}

	// routes for gateway routers
	vpcRouterIPA := t.vpcIPAllocator.GetAllocation(ipmanager.ObjToID(t.vpc))
	Expect(vpcRouterIPA).ToNot(BeNil())

	for _, node := range t.nodes {
		for _, vn := range t.vns {
			expected.withRouteForRouter(common.GatewayRouterName(t.vpc, node), &nbdb.LogicalRouterStaticRoute{
				IPPrefix:    vn.GetIPv4Subnet(),
				Nexthop:     vpcRouterIPA.Address.IP.String(),
				ExternalIDs: topology.NewExternalIDs().WithVPCRef(t.vpc).WithOwnerRef(vn),
			})
		}

		extIPA := t.nodeIPAllocator.GetAllocation(ipmanager.ObjToID(node))
		Expect(extIPA).ToNot(BeNil())
		expected.withRouteForRouter(common.GatewayRouterName(t.vpc, node), &nbdb.LogicalRouterStaticRoute{
			IPPrefix:    "0.0.0.0/0",
			Nexthop:     extIPA.Gateway.String(),
			ExternalIDs: topology.NewExternalIDs().WithVPCRef(t.vpc).WithOwnerRef(node),
		})
	}

	// Router Policies:

	// vpc router policies:

	// default drop rule
	expected.
		withPolicyForRouter(common.VPCRouterName(t.vpc), &nbdb.LogicalRouterPolicy{
			Priority:    10,
			Match:       "1",
			Action:      "drop",
			ExternalIDs: topology.NewExternalIDs().WithVPCRef(t.vpc).WithOwnerRef(t.vpc),
		})

	// drop or allow for each virtual network to other virtual networks
	action := "drop"
	if t.vpc.Spec.InterNetworkAccess {
		action = "allow"
	}
	for _, srcVN := range t.vns {
		for _, dstVN := range t.vns {
			if srcVN.Name == dstVN.Name {
				continue
			}

			expected.withPolicyForRouter(common.VPCRouterName(t.vpc), &nbdb.LogicalRouterPolicy{
				Priority: 200,
				Match: fmt.Sprintf("ip4.src == %s && ip4.dst == %s",
					srcVN.GetIPv4Subnet(), dstVN.GetIPv4Subnet()),
				Action:      action,
				ExternalIDs: topology.NewExternalIDs().WithVPCRef(t.vpc).WithOwnerRef(srcVN),
			})
		}
	}

	// drop or allow external traffic to virtual networks
	for _, vn := range t.vns {
		if !vn.Spec.ExternallyRouted {
			continue
		}

		expected.withPolicyForRouter(common.VPCRouterName(t.vpc), &nbdb.LogicalRouterPolicy{
			Priority:    100,
			Match:       fmt.Sprintf("ip4.dst == %s", vn.GetIPv4Subnet()),
			Action:      nbdb.LogicalRouterPolicyActionAllow,
			ExternalIDs: topology.NewExternalIDs().WithVPCRef(t.vpc).WithOwnerRef(vn),
		})
	}

	// Gateway router NAT rules
	for _, node := range t.nodes {
		for _, vn := range t.vns {
			if !vn.Spec.ExternallyRouted || vn.Spec.Masquerade == nil || !*vn.Spec.Masquerade {
				continue
			}

			extIPA := t.nodeIPAllocator.GetAllocation(ipmanager.ObjToID(node))
			Expect(extIPA).ToNot(BeNil())

			expected.withNATForRouter(common.GatewayRouterName(t.vpc, node), &nbdb.NAT{
				ExternalIDs: topology.NewExternalIDs().WithVPCRef(t.vpc).WithOwnerRef(vn),
				Type:        nbdb.NATTypeSNAT,
				LogicalIP:   vn.GetIPv4Subnet(),
				ExternalIP:  extIPA.Address.IP.String(),
			})
		}
	}

	return expected
}

type switchInfo struct {
	otherConfig map[string]string
}

func (si *switchInfo) validate(ls *nbdb.LogicalSwitch) {
	if si.otherConfig != nil {
		Expect(ls.OtherConfig).To(Equal(si.otherConfig), "unexpected other config for logical switch %s", ls.Name)
	}
}

type routerInfo struct {
	options map[string]string
}

func (ri *routerInfo) validate(lr *nbdb.LogicalRouter) {
	if ri.options != nil {
		Expect(lr.Options).To(Equal(ri.options), "unexpected options for logical router %s", lr.Name)
	}
}

type expectedOvnTopology struct {
	vpcRef                     string
	ExpectedLogicalSwitches    map[string]*switchInfo
	ExpectedLogicalRouters     map[string]*routerInfo
	ExpectedLogicalSwitchPorts k8ssets.Set[string]
	ExpectedLogicalRouterPorts k8ssets.Set[string]
	ExpectedRoutesForRouter    map[string]sets.RouteSet
	ExpectedPoliciesForRouter  map[string]sets.PolicySet
	ExpectedDhcpOptionsCidrs   k8ssets.Set[string]
	ExpectedNATForRouter       map[string]sets.NATSet
}

func newExpectedOvnTopology() *expectedOvnTopology {
	return &expectedOvnTopology{
		ExpectedLogicalSwitches:    make(map[string]*switchInfo),
		ExpectedLogicalRouters:     make(map[string]*routerInfo),
		ExpectedLogicalSwitchPorts: k8ssets.New[string](),
		ExpectedLogicalRouterPorts: k8ssets.New[string](),
		ExpectedRoutesForRouter:    make(map[string]sets.RouteSet),
		ExpectedPoliciesForRouter:  make(map[string]sets.PolicySet),
		ExpectedDhcpOptionsCidrs:   k8ssets.New[string](),
		ExpectedNATForRouter:       make(map[string]sets.NATSet),
	}
}

func (e *expectedOvnTopology) withLS(name string, si *switchInfo) *expectedOvnTopology {
	e.ExpectedLogicalSwitches[name] = si
	return e
}

func (e *expectedOvnTopology) withVPCRef(ref string) *expectedOvnTopology {
	e.vpcRef = ref
	return e
}

func (e *expectedOvnTopology) withLR(name string, ri *routerInfo) *expectedOvnTopology {
	e.ExpectedLogicalRouters[name] = ri
	return e
}

func (e *expectedOvnTopology) withLSP(name string) *expectedOvnTopology {
	e.ExpectedLogicalSwitchPorts.Insert(name)
	return e
}

//nolint:unparam
func (e *expectedOvnTopology) withLRP(name string) *expectedOvnTopology {
	e.ExpectedLogicalRouterPorts.Insert(name)
	return e
}

//nolint:unparam
func (e *expectedOvnTopology) withRouteForRouter(routerName string, route *nbdb.LogicalRouterStaticRoute) *expectedOvnTopology {
	if _, ok := e.ExpectedRoutesForRouter[routerName]; !ok {
		e.ExpectedRoutesForRouter[routerName] = sets.NewRouteSet()
	}
	e.ExpectedRoutesForRouter[routerName].Add(route)
	return e
}

//nolint:unparam
func (e *expectedOvnTopology) withPolicyForRouter(routerName string, policy *nbdb.LogicalRouterPolicy) *expectedOvnTopology {
	if _, ok := e.ExpectedPoliciesForRouter[routerName]; !ok {
		e.ExpectedPoliciesForRouter[routerName] = sets.NewPolicySet()
	}
	e.ExpectedPoliciesForRouter[routerName].Add(policy)
	return e
}

//nolint:unparam
func (e *expectedOvnTopology) withNATForRouter(routerName string, nat *nbdb.NAT) *expectedOvnTopology {
	if _, ok := e.ExpectedNATForRouter[routerName]; !ok {
		e.ExpectedNATForRouter[routerName] = sets.NewNATSet()
	}
	e.ExpectedNATForRouter[routerName].Add(nat)
	return e
}

func (e *expectedOvnTopology) withDhcpOptionsCidr(cidr string) *expectedOvnTopology {
	e.ExpectedDhcpOptionsCidrs.Insert(cidr)
	return e
}

func (e *expectedOvnTopology) validate() {
	GinkgoHelper()

	var extIDs map[string]string
	if e.vpcRef != "" {
		extIDs = make(map[string]string)
		extIDs[topology.VPCRefKey] = e.vpcRef
	}

	// validate logical routers
	lrs, err := ovnClient.ListLogicalRouter(ctx, &nbdb.LogicalRouterListParams{ExternalIDs: extIDs})
	Expect(err).ToNot(HaveOccurred(), "failed to list logical routers")
	e.validateLogicalRouters(lrs)

	// validate logical switches
	lss, err := ovnClient.ListLogicalSwitch(ctx, &nbdb.LogicalSwitchListParams{ExternalIDs: extIDs})
	Expect(err).ToNot(HaveOccurred(), "failed to list logical switches")
	e.validateLogicalSwitches(lss)

	// validate logical switch ports
	lsps, err := ovnClient.ListLogicalSwitchPort(ctx, &nbdb.LogicalSwitchPortListParams{ExternalIDs: extIDs})
	Expect(err).ToNot(HaveOccurred(), "failed to list logical switch ports")
	currentSwitchPorts := k8ssets.New[string]()
	for _, lsp := range lsps {
		currentSwitchPorts.Insert(lsp.Name)
	}
	Expect(e.ExpectedLogicalSwitchPorts.Equal(currentSwitchPorts)).To(
		BeTrue(),
		"expected switch ports: %v\ngot: %v\ndifference: %v\n",
		e.ExpectedLogicalSwitchPorts,
		currentSwitchPorts,
		e.ExpectedLogicalSwitchPorts.SymmetricDifference(currentSwitchPorts))

	// validate logical router ports
	lrps, err := ovnClient.ListLogicalRouterPort(ctx, &nbdb.LogicalRouterPortListParams{ExternalIDs: extIDs})
	Expect(err).ToNot(HaveOccurred(), "failed to list logical router ports")
	currentRouterPorts := k8ssets.New[string]()
	for _, lrp := range lrps {
		currentRouterPorts.Insert(lrp.Name)
	}
	Expect(e.ExpectedLogicalRouterPorts.Equal(currentRouterPorts)).To(
		BeTrue(),
		"expected router ports: %v\ngot: %v\ndifference: %v\n",
		e.ExpectedLogicalRouterPorts,
		currentRouterPorts,
		e.ExpectedLogicalRouterPorts.SymmetricDifference(currentRouterPorts))

	// validate dhcp options cidrs
	dhcpOptions, err := ovnClient.ListDhcpOptions(ctx, &nbdb.DHCPOptionsListParams{ExternalIDs: extIDs})
	Expect(err).ToNot(HaveOccurred(), "failed to list dhcp options")
	currentOpts := k8ssets.New[string]()
	for _, opt := range dhcpOptions {
		currentOpts.Insert(opt.Cidr)
	}
	Expect(e.ExpectedDhcpOptionsCidrs.Equal(currentOpts)).To(
		BeTrue(),
		"expected dhcp options: %v\ngot: %v\ndifference: %v\n",
		e.ExpectedDhcpOptionsCidrs,
		currentOpts,
		e.ExpectedDhcpOptionsCidrs.SymmetricDifference(currentOpts))

	// validate routes
	for routerName, expectedRoutes := range e.ExpectedRoutesForRouter {
		currRoutes := e.routeSetForRouter(routerName)
		Expect(expectedRoutes.Equals(currRoutes)).To(
			BeTrue(),
			"expected routes for router %s: %v\ngot: %v\ndifference: %v\n",
			routerName,
			expectedRoutes,
			currRoutes, expectedRoutes.SymmetricDifference(currRoutes))
	}

	// validate policies
	for routerName, expectedPolicies := range e.ExpectedPoliciesForRouter {
		currPolicies := policySetForRouter(routerName)
		Expect(expectedPolicies.Equals(currPolicies)).To(
			BeTrue(),
			"expected policies for router %s: %v\ngot: %v\ndifference: %v\n",
			routerName,
			expectedPolicies,
			currPolicies,
			expectedPolicies.SymmetricDifference(currPolicies))
	}

	// validate NAT for routers
	for routerName, expectedNATs := range e.ExpectedNATForRouter {
		e.validateNATforRouter(routerName, expectedNATs)
	}
}

func (e *expectedOvnTopology) validateNATforRouter(routerName string, expectedNATs sets.NATSet) {
	router, err := ovnClient.GetLogicalRouter(ctx, &nbdb.LogicalRouterGetParams{Name: routerName})
	Expect(err).ToNot(HaveOccurred(), "failed to get logical router %s", routerName)

	natEntries := make([]*nbdb.NAT, 0, len(router.Nat))
	for _, uid := range router.Nat {
		nat, err := ovnClient.GetLogicalRouterNat(ctx, &nbdb.NatGetParams{UUID: uid})
		Expect(err).ToNot(HaveOccurred())
		natEntries = append(natEntries, nat)
	}

	currNATs := sets.NewNATSet()
	currNATs.Add(natEntries...)

	Expect(expectedNATs.Equals(currNATs)).To(
		BeTrue(),
		"expected NAT for router %s: %v\ngot: %v\ndifference: %v\n",
		routerName,
		expectedNATs,
		currNATs,
		expectedNATs.SymmetricDifference(currNATs))
}

func (e *expectedOvnTopology) validateLogicalSwitches(lss []*nbdb.LogicalSwitch) {
	// validate that we have the expected number of switches
	lsSet := k8ssets.New[string]()
	for _, ls := range lss {
		lsSet.Insert(ls.Name)
	}

	siSet := k8ssets.New[string]()
	for switchName := range e.ExpectedLogicalSwitches {
		siSet.Insert(switchName)
	}

	Expect(siSet.Equal(lsSet)).To(
		BeTrue(),
		"expected logical switches: %v\ngot: %v\ndifference: %v\n",
		siSet,
		lsSet,
		siSet.SymmetricDifference(lsSet))

	// validate each switch info
	for _, ls := range lss {
		si, ok := e.ExpectedLogicalSwitches[ls.Name]
		Expect(ok).To(BeTrue(), "unexpected logical switch %s", ls.Name)
		if si != nil {
			si.validate(ls)
		}
	}
}

func (e *expectedOvnTopology) validateLogicalRouters(lrs []*nbdb.LogicalRouter) {
	// validate that we have the expected number of routers
	lrSet := k8ssets.New[string]()
	for _, lr := range lrs {
		lrSet.Insert(lr.Name)
	}

	riSet := k8ssets.New[string]()
	for routerName := range e.ExpectedLogicalRouters {
		riSet.Insert(routerName)
	}

	Expect(riSet.Equal(lrSet)).To(
		BeTrue(),
		"expected logical routers: %v\ngot: %v\ndifference: %v\n",
		riSet,
		lrSet,
		riSet.SymmetricDifference(lrSet))

	// validate each router info
	for _, lr := range lrs {
		ri, ok := e.ExpectedLogicalRouters[lr.Name]
		Expect(ok).To(BeTrue(), "unexpected logical router %s", lr.Name)
		if ri != nil {
			ri.validate(lr)
		}
	}
}

func (e *expectedOvnTopology) routeSetForRouter(routerName string) sets.RouteSet {
	router, err := ovnClient.GetLogicalRouter(ctx, &nbdb.LogicalRouterGetParams{Name: routerName})
	Expect(err).ToNot(HaveOccurred(), "failed to get logical router %s", routerName)

	current := sets.NewRouteSet()
	for _, uid := range router.StaticRoutes {
		sr, err := ovnClient.GetLogicalRouterStaticRoute(ctx, &nbdb.LogicalRouterStaticRouteGetParams{UUID: uid})
		Expect(err).ToNot(HaveOccurred(), "failed to get logical router static route %s", uid)
		current.Add(sr)
	}
	return current
}

func policySetForRouter(routerName string) sets.PolicySet {
	router, err := ovnClient.GetLogicalRouter(ctx, &nbdb.LogicalRouterGetParams{Name: routerName})
	Expect(err).ToNot(HaveOccurred(), "failed to get logical router %s", routerName)

	current := sets.NewPolicySet()
	for _, uid := range router.Policies {
		sr, err := ovnClient.GetLogicalRouterPolicy(ctx, &nbdb.LogicalRouterPolicyGetParams{UUID: uid})
		Expect(err).ToNot(HaveOccurred(), "failed to get logical router policy %s", uid)
		current.Add(sr)
	}

	return current
}

func getTestServiceInterfaces(numInterfaces int, vnName string, unknownMac bool) []*dpuservicev1.ServiceInterface {
	if numInterfaces > 255 || numInterfaces < 1 {
		GinkgoT().Fatal("numInterfaces must be 1 <= N <= 255")
	}

	sis := make([]*dpuservicev1.ServiceInterface, 0, numInterfaces)
	for i := range numInterfaces {
		annot := make(map[string]string)
		if unknownMac {
			annot[common.LSPMACAddressAnnotationKey] = "unknown"
		} else {
			annot[common.LSPMACAddressAnnotationKey] = fmt.Sprintf("00:%02x:aa:bb:de:ad", i)
		}

		si := &dpuservicev1.ServiceInterface{
			TypeMeta: metav1.TypeMeta{
				Kind: dpuservicev1.ServiceInterfaceGroupVersionKind.Kind,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        fmt.Sprintf("%s-si%d", vnName, i),
				Namespace:   "default",
				Annotations: annot,
			},
			Spec: dpuservicev1.ServiceInterfaceSpec{
				InterfaceType: dpuservicev1.InterfaceTypePF,
				PF: &dpuservicev1.PF{
					ID:             0,
					VirtualNetwork: &vnName,
				},
			},
		}
		sis = append(sis, si)
	}
	return sis
}

func validateServiceInterfacePlugged(vpc *vpcv1.DPUVPC, serviceIfc *dpuservicev1.ServiceInterface, dhcpEnabled bool, externalConnectivity bool) {
	GinkgoHelper()

	p, err := ovnClient.GetLogicalSwitchPort(ctx, &nbdb.LogicalSwitchPortGetParams{Name: common.ServiceInterfacePortName(serviceIfc)})
	Expect(err).ToNot(HaveOccurred())
	Expect(p.Addresses).ToNot(BeNil())

	if dhcpEnabled {
		portAddresses := strings.Split(p.Addresses[0], " ")
		Expect(len(portAddresses)).To(BeNumerically(">", 1))
		Expect(portAddresses[0]).To(Equal(serviceIfc.Annotations[common.LSPMACAddressAnnotationKey]))
		Expect(portAddresses[1]).To(Equal("dynamic"))
		Expect(p.DynamicAddresses).ToNot(BeNil())
		dynamicPortAddresses := strings.Split(*p.DynamicAddresses, " ")
		Expect(dynamicPortAddresses).To(HaveLen(2))
		Expect(dynamicPortAddresses[0]).To(Equal(serviceIfc.Annotations[common.LSPMACAddressAnnotationKey]))
		Expect(net.ParseIP(dynamicPortAddresses[1])).ToNot(BeNil(), "expected valid IP address, got %s", dynamicPortAddresses[1])
	} else {
		Expect(p.Addresses).To(HaveLen(1))
		Expect(p.Addresses[0]).To(Equal(serviceIfc.Annotations[common.LSPMACAddressAnnotationKey]))
	}

	// get router policies for service interface from VPC router
	ps := policySetForRouter(common.VPCRouterName(vpc))
	var policiesForServiceInterface []*nbdb.LogicalRouterPolicy
	for _, p := range ps.List() {
		if p.ExternalIDs[topology.VPCOwnerRefKey] == topology.NewExternalIDs().WithOwnerRef(serviceIfc)[topology.VPCOwnerRefKey] {
			policiesForServiceInterface = append(policiesForServiceInterface, p)
		}
	}

	if dhcpEnabled && externalConnectivity {
		// reroute policy exists
		Expect(policiesForServiceInterface).To(HaveLen(1), "expected 1 route policy for service interface %s", serviceIfc.Name)
		Expect(policiesForServiceInterface[0].Action).To(Equal(nbdb.LogicalRouterPolicyActionReroute))
	} else {
		Expect(policiesForServiceInterface).To(BeEmpty(), "expected no route policies for service interface %s", serviceIfc.Name)
	}
}

func validateServiceInterfaceUnplugged(vpc *vpcv1.DPUVPC, serviceIfc *dpuservicev1.ServiceInterface) {
	GinkgoHelper()

	// ensure service interface LSP does not exist
	_, err := ovnClient.GetLogicalSwitchPort(ctx, &nbdb.LogicalSwitchPortGetParams{Name: common.ServiceInterfacePortName(serviceIfc)})
	Expect(err).To(HaveOccurred())
	Expect(ovnlib.GetOvnErrorCodeFromError(err)).To(Equal(ovnlib.ErrNotFound))

	// ensure no route policies exist for service interface in vpc router
	ps := policySetForRouter(common.VPCRouterName(vpc))
	var policiesForServiceInterface []*nbdb.LogicalRouterPolicy
	for _, p := range ps.List() {
		if p.ExternalIDs[topology.VPCOwnerRefKey] == topology.NewExternalIDs().WithOwnerRef(serviceIfc)[topology.VPCOwnerRefKey] {
			policiesForServiceInterface = append(policiesForServiceInterface, p)
		}
	}
	Expect(policiesForServiceInterface).To(BeEmpty(), "expected no route policies for service interface %s", serviceIfc.Name)
}
