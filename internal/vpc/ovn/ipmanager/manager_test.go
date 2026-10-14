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

package ipmanager_test

import (
	"net"

	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/common"
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/ipmanager"

	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("IPManager", func() {
	var ipm ipmanager.IPManager

	BeforeEach(func() {
		ipm = ipmanager.NewIPManager()
	})

	Context("Basic Operations", func() {
		It("AddVPC()", func() {
			By("Adding VPC")
			ipm.AddVPC("vpc1")
			Expect(ipm.ListVPCs()).To(ContainElement("vpc1"))

			By("Adding same VPC again")
			ipm.AddVPC("vpc1")
			Expect(ipm.ListVPCs()).To(ContainElement("vpc1"))

			By("Adding another VPC")
			ipm.AddVPC("vpc2")
			Expect(ipm.ListVPCs()).To(ConsistOf("vpc1", "vpc2"))
		})

		It("RemoveVPC()", func() {
			By("Removing non-existent VPC")
			ipm.RemoveVPC("vpc1")
			Expect(ipm.ListVPCs()).To(BeEmpty())

			By("Removing existent VPC")
			ipm.AddVPC("vpc1")
			ipm.AddVPC("vpc2")
			ipm.RemoveVPC("vpc1")
			Expect(ipm.ListVPCs()).To(ConsistOf("vpc2"))
			ipm.RemoveVPC("vpc2")
			Expect(ipm.ListVPCs()).To(BeEmpty())
		})

		Context("AddNetwork()", func() {
			It("should fail if VPC does not exist", func() {
				err := ipm.AddNetwork("vpc1", "network1", "10.0.0.0/24")
				Expect(err).To(HaveOccurred())
			})

			It("should fail if network already exists with different cidr", func() {
				ipm.AddVPC("vpc1")
				Expect(ipm.AddNetwork("vpc1", "network1", "10.0.0.0/24")).To(Succeed())
				Expect(ipm.AddNetwork("vpc1", "network1", "10.0.0.0/25")).ToNot(Succeed())
			})

			It("should succeed if network does not exist", func() {
				ipm.AddVPC("vpc1")
				Expect(ipm.AddNetwork("vpc1", "network1", "10.0.0.0/24")).To(Succeed())
				Expect(ipm.ListNetworks("vpc1")).To(ConsistOf("network1"))
			})

			It("should succeed if network exists with same cidr", func() {
				ipm.AddVPC("vpc1")
				Expect(ipm.AddNetwork("vpc1", "network1", "10.0.0.0/24")).To(Succeed())
				Expect(ipm.AddNetwork("vpc1", "network1", "10.0.0.0/24")).To(Succeed())
				Expect(ipm.ListNetworks("vpc1")).To(ConsistOf("network1"))
			})

			It("should succeed to remove network", func() {
				ipm.AddVPC("vpc1")
				Expect(ipm.AddNetwork("vpc1", "network1", "10.0.0.0/24")).To(Succeed())
				Expect(ipm.AddNetwork("vpc1", "network2", "10.100.0.0/24")).To(Succeed())
				ipm.RemoveNetwork("vpc1", "network1")
				Expect(ipm.ListNetworks("vpc1")).To(ConsistOf("network2"))
			})
		})

		Context("GetNetworkIPAllocator()", func() {
			It("should return nil if VPC does not exist", func() {
				ipAllocator := ipm.GetNetworkIPAllocator("vpc1", "network1")
				Expect(ipAllocator).To(BeNil())
			})
			It("should return nil if network does not exist", func() {
				ipm.AddVPC("vpc1")
				ipAllocator := ipm.GetNetworkIPAllocator("vpc1", "network1")
				Expect(ipAllocator).To(BeNil())
			})
			It("should return IPAllocator if network exists", func() {
				ipm.AddVPC("vpc1")
				Expect(ipm.AddNetwork("vpc1", "network1", "10.0.0.0/24")).To(Succeed())
				ipAllocator := ipm.GetNetworkIPAllocator("vpc1", "network1")
				Expect(ipAllocator).NotTo(BeNil())
				_, err := ipAllocator.Allocate("testAllocation", net.ParseIP("10.0.0.1"))
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})

	Context("ResetInitialized()", func() {
		It("should reset initialized state", func() {
			Expect(ipm.Initialized()).To(BeFalse())
			Expect(ipm.Initialize(nil, nil, nil)).To(Succeed())
			Expect(ipm.Initialized()).To(BeTrue())
			ipm.ResetInitialized()
			Expect(ipm.Initialized()).To(BeFalse())
		})
	})

	Context("Initialize", func() {
		It("Successfully initializes ipmanager", func() {
			vpcs := []vpcv1.DPUVPC{
				*getDPUVpc("vpc1", lrpAnnotations("100.64.0.1/16")),
				*getDPUVpc("vpc2", nil),
				*getDPUVpc("vpc3", nil),
			}

			vns := []vpcv1.DPUVirtualNetwork{
				*getDPUVnet("vnet1", "vpc1", lrpAnnotations("100.64.0.2/16")),
				*getDPUVnet("vnet2", "vpc1", lrpAnnotations("100.64.0.3/16")),
				*getDPUVnet("vnet3", "vpc2", nil),
			}

			nodes := []corev1.Node{
				*getNode("node1", vpcLabel(&vpcs[0]), lrpAnnotations("100.64.0.4/16")),
				*getNode("node2", vpcLabel(&vpcs[1]), lrpAnnotations("100.64.0.3/16")),
				*getNode("node3", nil, nil),
			}

			Expect(ipm.Initialized()).To(BeFalse())
			Expect(ipm.Initialize(vpcs, vns, nodes)).To(Succeed())
			Expect(ipm.ListVPCs()).To(HaveLen(3))
			Expect(ipm.ListVPCs()).To(ConsistOf(
				ipmanager.ObjToID(&vpcs[0]),
				ipmanager.ObjToID(&vpcs[1]),
				ipmanager.ObjToID(&vpcs[2])))
			Expect(ipm.Initialized()).To(BeTrue())

			// Check Networks (ipv4 cluster network present)
			for _, vpc := range vpcs {
				Expect(ipm.ListNetworks(ipmanager.ObjToID(&vpc))).To(HaveLen(1))
				Expect(ipm.ListNetworks(ipmanager.ObjToID(&vpc))).To(ConsistOf(ipmanager.VPCClusterNetworkIPV4))
			}

			// Check IP Allocators
			ipa1 := ipm.GetNetworkIPAllocator(ipmanager.ObjToID(&vpcs[0]), ipmanager.VPCClusterNetworkIPV4)
			ipa2 := ipm.GetNetworkIPAllocator(ipmanager.ObjToID(&vpcs[1]), ipmanager.VPCClusterNetworkIPV4)
			ipa3 := ipm.GetNetworkIPAllocator(ipmanager.ObjToID(&vpcs[2]), ipmanager.VPCClusterNetworkIPV4)
			Expect(ipa1).ToNot(BeNil())
			Expect(ipa2).ToNot(BeNil())
			Expect(ipa3).ToNot(BeNil())
			Expect(ipa1.ListAllocationIDs()).To(HaveLen(4))
			Expect(ipa2.ListAllocationIDs()).To(HaveLen(1))
			Expect(ipa3.ListAllocationIDs()).To(BeEmpty())
		})

		It("Fails if vpc has invalid ip address", func() {
			vpcs := []vpcv1.DPUVPC{
				*getDPUVpc("vpc1", lrpAnnotations("invalid")),
			}
			Expect(ipm.Initialize(vpcs, nil, nil)).To(HaveOccurred())
		})

		It("Fails if virtualNetwork has invalid ip address", func() {
			vpcs := []vpcv1.DPUVPC{
				*getDPUVpc("vpc1", lrpAnnotations("100.64.0.1/16")),
			}
			vns := []vpcv1.DPUVirtualNetwork{
				*getDPUVnet("vnet1", "vpc1", lrpAnnotations("invalid")),
			}
			Expect(ipm.Initialize(vpcs, vns, nil)).To(HaveOccurred())
		})

		It("fails if node has invalid address", func() {
			vpcs := []vpcv1.DPUVPC{
				*getDPUVpc("vpc1", lrpAnnotations("100.64.0.1/16")),
			}
			nodes := []corev1.Node{
				*getNode("node1", vpcLabel(&vpcs[0]), lrpAnnotations("invalid")),
			}
			Expect(ipm.Initialize(vpcs, nil, nodes)).To(HaveOccurred())
		})

		It("fails if vpc for virtualNetwork does not exist", func() {
			vpcs := []vpcv1.DPUVPC{
				*getDPUVpc("vpc1", lrpAnnotations("100.64.0.1/16")),
			}
			vns := []vpcv1.DPUVirtualNetwork{
				*getDPUVnet("vnet1", "vpc2", lrpAnnotations("100.64.0.2/16")),
			}
			Expect(ipm.Initialize(vpcs, vns, nil)).To(HaveOccurred())
		})

		It("fails if vpc for node does not exist", func() {
			vpcs := []vpcv1.DPUVPC{
				*getDPUVpc("vpc1", lrpAnnotations("100.64.0.1/16")),
			}
			nodes := []corev1.Node{
				*getNode("node1", map[string]string{common.OVNVPCNodeLabelKey: "foo/vpc2"}, lrpAnnotations("100.64.0.4/16")),
			}
			Expect(ipm.Initialize(vpcs, nil, nodes)).To(HaveOccurred())
		})

		It("ignores second call for Initialization", func() {
			vpcs := []vpcv1.DPUVPC{
				*getDPUVpc("vpc1", nil),
			}
			Expect(ipm.Initialize(vpcs, nil, nil)).To(Succeed())

			otherVPCs := []vpcv1.DPUVPC{
				*getDPUVpc("vpc2", nil),
			}
			Expect(ipm.Initialize(otherVPCs, nil, nil)).To(Succeed())
			Expect(ipm.ListVPCs()).To(Equal([]string{ipmanager.ObjToID(&vpcs[0])}))
		})
	})

	Context("ObjToID", func() {
		It("should return IDs as expected for different objects", func() {
			dpuVPC := getDPUVpc("vpc1", nil)
			dpuVnet := getDPUVnet("vnet1", "vpc1", nil)
			node := getNode("node1", nil, nil)
			Expect(ipmanager.ObjToID(dpuVPC)).To(Equal("v1alpha1.DPUVPC_test-namespace_vpc1"))
			Expect(ipmanager.ObjToID(dpuVnet)).To(Equal("v1alpha1.DPUVirtualNetwork_test-namespace_vnet1"))
			Expect(ipmanager.ObjToID(node)).To(Equal("v1.Node__node1"))
		})
	})
})

func getDPUVpc(name string, annot map[string]string) *vpcv1.DPUVPC {
	return &vpcv1.DPUVPC{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   "test-namespace",
			Annotations: annot,
		},
		Spec: vpcv1.DPUVPCSpec{},
	}
}

func getDPUVnet(name string, vpcname string, annot map[string]string) *vpcv1.DPUVirtualNetwork {
	return &vpcv1.DPUVirtualNetwork{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   "test-namespace",
			Annotations: annot,
		},
		Spec: vpcv1.DPUVirtualNetworkSpec{
			VPCName: vpcname,
		},
	}
}

func getNode(name string, labels map[string]string, annot map[string]string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: annot,
			Labels:      labels,
		},
	}
}

func lrpAnnotations(ip4 string) map[string]string {
	annot := make(map[string]string)
	lrpa := common.LRPAddress{
		IPV4: ip4,
	}
	Expect(common.LRPAddressToAnnotation(lrpa, annot)).ToNot(HaveOccurred())
	return annot
}

func vpcLabel(vpc *vpcv1.DPUVPC) map[string]string {
	lbl := make(map[string]string)
	lbl[common.OVNVPCNodeLabelKey] = common.ObjectToLabelValue(vpc)
	return lbl
}
