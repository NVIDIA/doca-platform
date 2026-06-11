/*
Copyright 2024 NVIDIA

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

package webhooks

import (
	"context"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("DPUServiceIPAM Validating Webhook", func() {
	var webhook *DPUServiceIPAMValidator

	BeforeEach(func() {
		s := scheme.Scheme
		Expect(dpuservicev1.AddToScheme(s)).To(Succeed())
		fakeclient := fake.NewClientBuilder().WithScheme(s).Build()
		webhook = &DPUServiceIPAMValidator{
			Client: fakeclient,
		}
	})

	It("Errors out when namespace is different than dpf-operator-system", func() {
		ipam := getFullyPopulatedDPUServiceIPAM()
		ipam.Spec.IPV4Network = nil
		ipam.Namespace = "some-other-namespace"
		_, err := webhook.ValidateCreate(context.Background(), ipam)
		Expect(err).To(HaveOccurred())
	})

	It("Errors out when both .spec.ipv4Network and .spec.ipv4Subnet are specified", func() {
		_, err := webhook.ValidateCreate(context.Background(), getFullyPopulatedDPUServiceIPAM())
		Expect(err).To(HaveOccurred())
	})

	It("Errors out when neither .spec.ipv4Network nor .spec.ipv4Subnet are specified", func() {
		ipam := getFullyPopulatedDPUServiceIPAM()
		ipam.Spec.IPV4Network = nil
		ipam.Spec.IPV4Subnet = nil
		_, err := webhook.ValidateCreate(context.Background(), ipam)
		Expect(err).To(HaveOccurred())
	})

	DescribeTable("Validates the .spec.ipv4Network correctly", func(ipam *dpuservicev1.DPUServiceIPAM, expectError bool) {
		_, err := webhook.ValidateCreate(context.Background(), ipam)
		if expectError {
			Expect(err).To(HaveOccurred())
		} else {
			Expect(err).ToNot(HaveOccurred())
		}
	},
		Entry("bad network", func() *dpuservicev1.DPUServiceIPAM {
			return getFullyPopulatedDPUServiceIPAM()
		}(), true),

		Entry("bad network", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Subnet = nil
			ipam.Spec.IPV4Network.Network = "bad-network"
			return ipam
		}(), true),
		Entry("bad prefixSize", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Subnet = nil
			ipam.Spec.IPV4Network.PrefixSize = 10
			return ipam
		}(), true),
		Entry("bad exclusion - invalid IP", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Subnet = nil
			//nolint:staticcheck // SA1019: Exclusions is deprecated but still supported
			ipam.Spec.IPV4Network.Exclusions[0] = "bad-ip"
			return ipam
		}(), true),
		Entry("bad exclusion - IP not part of the network", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Subnet = nil
			//nolint:staticcheck // SA1019: Exclusions is deprecated but still supported
			ipam.Spec.IPV4Network.Exclusions[0] = "10.0.0.0"
			return ipam
		}(), true),
		Entry("bad allocation - invalid subnet", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Subnet = nil
			ipam.Spec.IPV4Network.Allocations["dpu-node-1"] = "bad-subnet"
			return ipam
		}(), true),
		Entry("bad allocation - subnet not part of the network due to IP", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Subnet = nil
			ipam.Spec.IPV4Network.Allocations["dpu-node-1"] = "192.168.0.0/20"
			return ipam
		}(), true),
		Entry("bad allocation - subnet not part of the network due to mask size", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Subnet = nil
			ipam.Spec.IPV4Network.Allocations["dpu-node-1"] = "192.168.1.0/10"
			return ipam
		}(), true),
		Entry("valid config", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Subnet = nil
			return ipam
		}(), false),
		Entry("bad route - dest not a valid cidr", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Subnet = nil
			ipam.Spec.IPV4Network.Routes[0].Dst = "not-a-cidr"
			return ipam
		}(), true),
		Entry("invalid route - default gateway true", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Subnet = nil
			ipam.Spec.IPV4Network.Routes[0].Dst = ipv4DefaultRoute
			return ipam
		}(), true),
		Entry("invalid route - not same family", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Subnet = nil
			ipam.Spec.IPV4Network.Routes[0].Dst = "2001:db8:3333:4444::0/64"
			return ipam
		}(), true),
		Entry("bad exclude range - invalid startIP", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Subnet = nil
			ipam.Spec.IPV4Network.ExcludeRanges = []dpuservicev1.IPRange{
				{StartIP: "bad-startIP", EndIP: "192.168.0.50"},
			}
			return ipam
		}(), true),
		Entry("bad exclude range - invalid endIP", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Subnet = nil
			ipam.Spec.IPV4Network.ExcludeRanges = []dpuservicev1.IPRange{
				{StartIP: "192.168.0.40", EndIP: "bad-endIP"},
			}
			return ipam
		}(), true),
		Entry("bad exclude range - startIP is not part of the network", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Subnet = nil
			ipam.Spec.IPV4Network.ExcludeRanges = []dpuservicev1.IPRange{
				{StartIP: "192.178.0.40", EndIP: "192.168.0.50"},
			}
			return ipam
		}(), true),
		Entry("bad exclude range - endIP is not part of the network", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Subnet = nil
			ipam.Spec.IPV4Network.ExcludeRanges = []dpuservicev1.IPRange{
				{StartIP: "192.168.0.40", EndIP: "192.178.0.50"},
			}
			return ipam
		}(), true),
		Entry("bad exclude range - endIP is smaller than startIP", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Subnet = nil
			ipam.Spec.IPV4Network.ExcludeRanges = []dpuservicev1.IPRange{
				{StartIP: "192.178.0.50", EndIP: "192.178.0.40"},
			}
			return ipam
		}(), true),
		Entry("bad exclude range - endIP is smaller than startIP ipv6", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Subnet = nil
			ipam.Spec.IPV4Network.ExcludeRanges = []dpuservicev1.IPRange{
				{StartIP: "2001:db8::40:1", EndIP: "2001:db8::1"},
			}
			return ipam
		}(), true),
		Entry("subnetsPerDPUCluster is 0", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Subnet = nil
			ipam.Spec.IPV4Network.SubnetsPerDPUCluster = ptr.To[int32](0)
			return ipam
		}(), true),
		Entry("subnetsPerDPUCluster is 1", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Subnet = nil
			ipam.Spec.IPV4Network.SubnetsPerDPUCluster = ptr.To[int32](1)
			return ipam
		}(), false),
		Entry("subnetsPerDPUCluster exceeds total available subnets in network", func() *dpuservicev1.DPUServiceIPAM {
			// 192.168.0.0/20 with prefixSize 24 gives 16 /24 subnets; requesting 17 must fail.
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Subnet = nil
			ipam.Spec.IPV4Network.SubnetsPerDPUCluster = ptr.To[int32](17)
			return ipam
		}(), true),
		Entry("subnetsPerDPUCluster equals total available subnets in network", func() *dpuservicev1.DPUServiceIPAM {
			// 192.168.0.0/20 with prefixSize 24 gives exactly 16 /24 subnets; requesting 16 must pass.
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Subnet = nil
			ipam.Spec.IPV4Network.SubnetsPerDPUCluster = ptr.To[int32](16)
			return ipam
		}(), false),
	)

	DescribeTable("Validates the .spec.ipv4Subnet correctly", func(ipam *dpuservicev1.DPUServiceIPAM, expectError bool) {
		_, err := webhook.ValidateCreate(context.Background(), ipam)
		if expectError {
			Expect(err).To(HaveOccurred())
		} else {
			Expect(err).ToNot(HaveOccurred())
		}
	},
		Entry("bad subnet", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Network = nil
			ipam.Spec.IPV4Subnet.Subnet = "bad-subnet"
			return ipam
		}(), true),
		Entry("/31 subnet is rejected", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Network = nil
			ipam.Spec.IPV4Subnet.Subnet = "192.168.0.0/31"
			ipam.Spec.IPV4Subnet.Gateway = "192.168.0.1"
			return ipam
		}(), true),
		Entry("/32 subnet is rejected", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Network = nil
			ipam.Spec.IPV4Subnet.Subnet = "192.168.0.1/32"
			ipam.Spec.IPV4Subnet.Gateway = "192.168.0.1"
			return ipam
		}(), true),
		Entry("bad gateway - invalid IP ", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Network = nil
			ipam.Spec.IPV4Subnet.Gateway = "bad-gateway"
			return ipam
		}(), true),
		Entry("bad gateway - IP not part of subnet", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Network = nil
			ipam.Spec.IPV4Subnet.Gateway = "10.0.0.0"
			return ipam
		}(), true),
		Entry("valid config", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Network = nil
			return ipam
		}(), false),
		Entry("bad route - dest not a valid cidr", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Network = nil
			ipam.Spec.IPV4Subnet.Routes[0].Dst = "not-a-cidr"
			return ipam
		}(), true),
		Entry("invalid route - default gateway true", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Network = nil
			ipam.Spec.IPV4Subnet.Routes[0].Dst = ipv4DefaultRoute
			return ipam
		}(), true),
		Entry("invalid route - not same family", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Network = nil
			ipam.Spec.IPV4Subnet.Routes[0].Dst = "2011:db8:3333:4444::0/64"
			return ipam
		}(), true),
		Entry("bad exclude range - invalid startIP", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Network = nil
			ipam.Spec.IPV4Subnet.ExcludeRanges = []dpuservicev1.IPRange{
				{StartIP: "bad-startIP", EndIP: "192.168.0.50"},
			}
			return ipam
		}(), true),
		Entry("bad exclude range - invalid endIP", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Network = nil
			ipam.Spec.IPV4Subnet.ExcludeRanges = []dpuservicev1.IPRange{
				{StartIP: "192.168.0.40", EndIP: "bad-endIP"},
			}
			return ipam
		}(), true),
		Entry("bad exclude range - startIP is not part of the network", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Network = nil
			ipam.Spec.IPV4Subnet.ExcludeRanges = []dpuservicev1.IPRange{
				{StartIP: "192.178.0.40", EndIP: "192.168.0.50"},
			}
			return ipam
		}(), true),
		Entry("bad exclude range - endIP is not part of the network", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Network = nil
			ipam.Spec.IPV4Subnet.ExcludeRanges = []dpuservicev1.IPRange{
				{StartIP: "192.168.0.40", EndIP: "192.178.0.50"},
			}
			return ipam
		}(), true),
		Entry("bad exclude range - endIP is smaller than startIP", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Network = nil
			ipam.Spec.IPV4Subnet.ExcludeRanges = []dpuservicev1.IPRange{
				{StartIP: "192.178.0.50", EndIP: "192.178.0.40"},
			}
			return ipam
		}(), true),
		Entry("bad exclude range - endIP is smaller than startIP ipv6", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Network = nil
			ipam.Spec.IPV4Subnet.ExcludeRanges = []dpuservicev1.IPRange{
				{StartIP: "2001:db8::40:1", EndIP: "2001:db8::1"},
			}
			return ipam
		}(), true),
		Entry("blocksPerDPUCluster is 0", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Network = nil
			ipam.Spec.IPV4Subnet.BlocksPerDPUCluster = ptr.To[int32](0)
			return ipam
		}(), true),
		Entry("blocksPerDPUCluster is 1", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Network = nil
			ipam.Spec.IPV4Subnet.BlocksPerDPUCluster = ptr.To[int32](1)
			return ipam
		}(), false),
		Entry("blocksPerDPUCluster exceeds total available blocks in subnet", func() *dpuservicev1.DPUServiceIPAM {
			// 192.168.0.0/20 with perNodeIPCount 256: (4096-2)/256 = 15 blocks; requesting 16 must fail.
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Network = nil
			ipam.Spec.IPV4Subnet.BlocksPerDPUCluster = ptr.To[int32](16)
			return ipam
		}(), true),
		Entry("blocksPerDPUCluster equals total available blocks in subnet", func() *dpuservicev1.DPUServiceIPAM {
			// 192.168.0.0/20 with perNodeIPCount 256: (4096-2)/256 = 15 blocks; requesting 15 must pass.
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Network = nil
			ipam.Spec.IPV4Subnet.BlocksPerDPUCluster = ptr.To[int32](15)
			return ipam
		}(), false),
	)

	DescribeTable("type switch validation",
		func(oldIpamObj, newIpamObj dpuservicev1.DPUServiceIPAM, expectedError bool, expectedErrorMessage string) {
			_, err := webhook.ValidateUpdate(context.Background(), &oldIpamObj, &newIpamObj)
			if expectedError {
				Expect(err).To(HaveOccurred())
				if expectedErrorMessage != "" {
					Expect(err.Error()).To(ContainSubstring(expectedErrorMessage))
				}
			} else {
				Expect(err).ToNot(HaveOccurred())
			}
		},
		Entry("same subnet interface", *ipamWithIPV4Subnet, *ipamWithIPV4Subnet, false, ""),
		Entry("same network interface", *ipamWithIPV4Network, *ipamWithIPV4Network, false, ""),
		Entry("switch from subnet to network", *ipamWithIPV4Subnet, *ipamWithIPV4Network, true, "transitioning from ipv4subnet to ipv4network and vice versa is currently not supported"),
		Entry("switch from network to subnet", *ipamWithIPV4Network, *ipamWithIPV4Subnet, true, "transitioning from ipv4subnet to ipv4network and vice versa is currently not supported"),
	)

	DescribeTable("validateIpRangeNotShrinking",
		func(oldSubnet, newSubnet string, isIPAMWithSubnet bool, expectedError bool, expectedErrorMessage string) {
			oldIpam := getFullyPopulatedDPUServiceIPAM()
			if isIPAMWithSubnet {
				oldIpam.Spec.IPV4Network = nil
				oldIpam.Spec.IPV4Subnet.Subnet = oldSubnet
			} else {
				oldIpam.Spec.IPV4Subnet = nil
				oldIpam.Spec.IPV4Network.Network = oldSubnet
			}

			newIpam := getFullyPopulatedDPUServiceIPAM()
			if isIPAMWithSubnet {
				newIpam.Spec.IPV4Network = nil
				newIpam.Spec.IPV4Subnet.Subnet = newSubnet
			} else {
				newIpam.Spec.IPV4Subnet = nil
				newIpam.Spec.IPV4Network.Network = newSubnet
			}

			_, err := webhook.ValidateUpdate(context.Background(), oldIpam, newIpam)
			if expectedError {
				Expect(err).To(HaveOccurred())
				if expectedErrorMessage != "" {
					Expect(err.Error()).To(ContainSubstring(expectedErrorMessage))
				}
			} else {
				Expect(err).ToNot(HaveOccurred())
			}
		},
		Entry("ipv4network - same subnet", "192.168.0.0/20", "192.168.0.0/20", true, false, ""),
		Entry("ipv4network - new subnet is a superset of the old subnet", "192.168.0.0/20", "192.168.0.0/19", true, false, ""),
		Entry("ipv4network - new subnet is a subset of the old subnet", "192.168.0.0/20", "192.168.0.128/25", true, true, "you cannot shrink the ip subnet"),
		Entry("ipv4network - new subnet does not contain the old subnet's IP", "192.168.0.0/20", "192.169.0.0/20", true, true, "you cannot shrink the ip subnet"),
		Entry("ipv4network - old subnet is invalid", "invalid", "192.168.0.0/20", true, true, ""),
		Entry("ipv4network - new subnet is invalid", "192.168.0.0/20", "invalid", true, true, ""),
		Entry("ipv4subnet - same subnet", "192.168.0.0/20", "192.168.0.0/20", false, false, ""),
		Entry("ipv4subnet - new subnet is a superset of the old subnet", "192.168.0.0/20", "192.168.0.0/19", false, false, ""),
		Entry("ipv4subnet - new subnet is a subset of the old subnet", "192.168.0.0/20", "192.168.0.128/25", false, true, "you cannot shrink the ip subnet"),
		Entry("ipv4subnet - new subnet does not contain the old subnet's IP", "192.168.0.0/20", "192.169.0.0/20", false, true, "you cannot shrink the ip subnet"),
		Entry("ipv4subnet - old subnet is invalid", "invalid", "192.168.0.0/20", false, true, ""),
		Entry("ipv4subnet - new subnet is invalid", "192.168.0.0/20", "invalid", false, true, ""),
	)

	DescribeTable("subnetsPerDPUCluster immutability on update",
		func(oldIpamObj, newIpamObj dpuservicev1.DPUServiceIPAM, expectedError bool, expectedErrorMessage string) {
			_, err := webhook.ValidateUpdate(context.Background(), &oldIpamObj, &newIpamObj)
			if expectedError {
				Expect(err).To(HaveOccurred())
				if expectedErrorMessage != "" {
					Expect(err.Error()).To(ContainSubstring(expectedErrorMessage))
				}
			} else {
				Expect(err).ToNot(HaveOccurred())
			}
		},
		Entry("both unset - no change",
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Network: &dpuservicev1.IPV4Network{Network: "10.0.0.0/20", PrefixSize: 24}}},
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Network: &dpuservicev1.IPV4Network{Network: "10.0.0.0/20", PrefixSize: 24}}},
			false, ""),
		Entry("both set same value - no change",
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Network: &dpuservicev1.IPV4Network{Network: "10.0.0.0/20", PrefixSize: 24, SubnetsPerDPUCluster: ptr.To[int32](2)}}},
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Network: &dpuservicev1.IPV4Network{Network: "10.0.0.0/20", PrefixSize: 24, SubnetsPerDPUCluster: ptr.To[int32](2)}}},
			false, ""),
		Entry("both set different value - value change is allowed",
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Network: &dpuservicev1.IPV4Network{Network: "10.0.0.0/20", PrefixSize: 24, SubnetsPerDPUCluster: ptr.To[int32](2)}}},
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Network: &dpuservicev1.IPV4Network{Network: "10.0.0.0/20", PrefixSize: 24, SubnetsPerDPUCluster: ptr.To[int32](4)}}},
			false, ""),
		Entry("unset to set - not allowed",
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Network: &dpuservicev1.IPV4Network{Network: "10.0.0.0/20", PrefixSize: 24}}},
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Network: &dpuservicev1.IPV4Network{Network: "10.0.0.0/20", PrefixSize: 24, SubnetsPerDPUCluster: ptr.To[int32](2)}}},
			true, "subnetsPerDPUCluster cannot be toggled between set and unset"),
		Entry("set to unset - not allowed",
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Network: &dpuservicev1.IPV4Network{Network: "10.0.0.0/20", PrefixSize: 24, SubnetsPerDPUCluster: ptr.To[int32](2)}}},
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Network: &dpuservicev1.IPV4Network{Network: "10.0.0.0/20", PrefixSize: 24}}},
			true, "subnetsPerDPUCluster cannot be toggled between set and unset"),
		Entry("grow - allowed",
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Network: &dpuservicev1.IPV4Network{Network: "10.0.0.0/20", PrefixSize: 24, SubnetsPerDPUCluster: ptr.To[int32](2)}}},
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Network: &dpuservicev1.IPV4Network{Network: "10.0.0.0/20", PrefixSize: 24, SubnetsPerDPUCluster: ptr.To[int32](4)}}},
			false, ""),
		Entry("shrink - not allowed",
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Network: &dpuservicev1.IPV4Network{Network: "10.0.0.0/20", PrefixSize: 24, SubnetsPerDPUCluster: ptr.To[int32](4)}}},
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Network: &dpuservicev1.IPV4Network{Network: "10.0.0.0/20", PrefixSize: 24, SubnetsPerDPUCluster: ptr.To[int32](2)}}},
			true, "subnetsPerDPUCluster cannot be decreased"),
	)

	DescribeTable("blocksPerDPUCluster immutability on update",
		func(oldIpamObj, newIpamObj dpuservicev1.DPUServiceIPAM, expectedError bool, expectedErrorMessage string) {
			_, err := webhook.ValidateUpdate(context.Background(), &oldIpamObj, &newIpamObj)
			if expectedError {
				Expect(err).To(HaveOccurred())
				if expectedErrorMessage != "" {
					Expect(err.Error()).To(ContainSubstring(expectedErrorMessage))
				}
			} else {
				Expect(err).ToNot(HaveOccurred())
			}
		},
		Entry("both unset - no change",
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Subnet: &dpuservicev1.IPV4Subnet{Subnet: "10.0.0.0/24", Gateway: "10.0.0.1", PerNodeIPCount: 10}}},
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Subnet: &dpuservicev1.IPV4Subnet{Subnet: "10.0.0.0/24", Gateway: "10.0.0.1", PerNodeIPCount: 10}}},
			false, ""),
		Entry("both set same value - no change",
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Subnet: &dpuservicev1.IPV4Subnet{Subnet: "10.0.0.0/24", Gateway: "10.0.0.1", PerNodeIPCount: 10, BlocksPerDPUCluster: ptr.To[int32](2)}}},
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Subnet: &dpuservicev1.IPV4Subnet{Subnet: "10.0.0.0/24", Gateway: "10.0.0.1", PerNodeIPCount: 10, BlocksPerDPUCluster: ptr.To[int32](2)}}},
			false, ""),
		Entry("both set different value - value change is allowed",
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Subnet: &dpuservicev1.IPV4Subnet{Subnet: "10.0.0.0/24", Gateway: "10.0.0.1", PerNodeIPCount: 10, BlocksPerDPUCluster: ptr.To[int32](2)}}},
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Subnet: &dpuservicev1.IPV4Subnet{Subnet: "10.0.0.0/24", Gateway: "10.0.0.1", PerNodeIPCount: 10, BlocksPerDPUCluster: ptr.To[int32](4)}}},
			false, ""),
		Entry("unset to set - not allowed",
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Subnet: &dpuservicev1.IPV4Subnet{Subnet: "10.0.0.0/24", Gateway: "10.0.0.1", PerNodeIPCount: 10}}},
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Subnet: &dpuservicev1.IPV4Subnet{Subnet: "10.0.0.0/24", Gateway: "10.0.0.1", PerNodeIPCount: 10, BlocksPerDPUCluster: ptr.To[int32](2)}}},
			true, "blocksPerDPUCluster cannot be toggled between set and unset"),
		Entry("set to unset - not allowed",
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Subnet: &dpuservicev1.IPV4Subnet{Subnet: "10.0.0.0/24", Gateway: "10.0.0.1", PerNodeIPCount: 10, BlocksPerDPUCluster: ptr.To[int32](2)}}},
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Subnet: &dpuservicev1.IPV4Subnet{Subnet: "10.0.0.0/24", Gateway: "10.0.0.1", PerNodeIPCount: 10}}},
			true, "blocksPerDPUCluster cannot be toggled between set and unset"),
		Entry("grow - allowed",
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Subnet: &dpuservicev1.IPV4Subnet{Subnet: "10.0.0.0/24", Gateway: "10.0.0.1", PerNodeIPCount: 10, BlocksPerDPUCluster: ptr.To[int32](2)}}},
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Subnet: &dpuservicev1.IPV4Subnet{Subnet: "10.0.0.0/24", Gateway: "10.0.0.1", PerNodeIPCount: 10, BlocksPerDPUCluster: ptr.To[int32](4)}}},
			false, ""),
		Entry("shrink - not allowed",
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Subnet: &dpuservicev1.IPV4Subnet{Subnet: "10.0.0.0/24", Gateway: "10.0.0.1", PerNodeIPCount: 10, BlocksPerDPUCluster: ptr.To[int32](4)}}},
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Subnet: &dpuservicev1.IPV4Subnet{Subnet: "10.0.0.0/24", Gateway: "10.0.0.1", PerNodeIPCount: 10, BlocksPerDPUCluster: ptr.To[int32](2)}}},
			true, "blocksPerDPUCluster cannot be decreased"),
	)

	DescribeTable("prefixSize immutability on update",
		func(oldIpamObj, newIpamObj dpuservicev1.DPUServiceIPAM, expectedError bool, expectedErrorMessage string) {
			_, err := webhook.ValidateUpdate(context.Background(), &oldIpamObj, &newIpamObj)
			if expectedError {
				Expect(err).To(HaveOccurred())
				if expectedErrorMessage != "" {
					Expect(err.Error()).To(ContainSubstring(expectedErrorMessage))
				}
			} else {
				Expect(err).ToNot(HaveOccurred())
			}
		},
		Entry("same value - no change",
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Network: &dpuservicev1.IPV4Network{Network: "10.0.0.0/20", PrefixSize: 24}}},
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Network: &dpuservicev1.IPV4Network{Network: "10.0.0.0/20", PrefixSize: 24}}},
			false, ""),
		Entry("change - not allowed",
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Network: &dpuservicev1.IPV4Network{Network: "10.0.0.0/20", PrefixSize: 24}}},
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Network: &dpuservicev1.IPV4Network{Network: "10.0.0.0/20", PrefixSize: 25}}},
			true, "prefixSize is immutable"),
	)

	DescribeTable("perNodeIPCount immutability on update",
		func(oldIpamObj, newIpamObj dpuservicev1.DPUServiceIPAM, expectedError bool, expectedErrorMessage string) {
			_, err := webhook.ValidateUpdate(context.Background(), &oldIpamObj, &newIpamObj)
			if expectedError {
				Expect(err).To(HaveOccurred())
				if expectedErrorMessage != "" {
					Expect(err.Error()).To(ContainSubstring(expectedErrorMessage))
				}
			} else {
				Expect(err).ToNot(HaveOccurred())
			}
		},
		Entry("same value - no change",
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Subnet: &dpuservicev1.IPV4Subnet{Subnet: "10.0.0.0/24", Gateway: "10.0.0.1", PerNodeIPCount: 10}}},
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Subnet: &dpuservicev1.IPV4Subnet{Subnet: "10.0.0.0/24", Gateway: "10.0.0.1", PerNodeIPCount: 10}}},
			false, ""),
		Entry("change - not allowed",
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Subnet: &dpuservicev1.IPV4Subnet{Subnet: "10.0.0.0/24", Gateway: "10.0.0.1", PerNodeIPCount: 10}}},
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Subnet: &dpuservicev1.IPV4Subnet{Subnet: "10.0.0.0/24", Gateway: "10.0.0.1", PerNodeIPCount: 20}}},
			true, "perNodeIPCount is immutable"),
	)
})

var ipamWithIPV4Subnet = &dpuservicev1.DPUServiceIPAM{
	ObjectMeta: metav1.ObjectMeta{
		Namespace: "dpf-operator-system",
	},
	Spec: dpuservicev1.DPUServiceIPAMSpec{
		IPV4Subnet: &dpuservicev1.IPV4Subnet{
			Subnet:  "10.0.0.0/24",
			Gateway: "10.0.0.1",
		},
	},
}
var ipamWithIPV4Network = &dpuservicev1.DPUServiceIPAM{
	ObjectMeta: metav1.ObjectMeta{
		Namespace: "dpf-operator-system",
	},
	Spec: dpuservicev1.DPUServiceIPAMSpec{
		IPV4Network: &dpuservicev1.IPV4Network{
			Network:    "10.0.0.0/24",
			PrefixSize: 30,
		},
	},
}

// getFullyPopulatedDPUServiceIPAM returns an invalid but fully populated (for the validation context) DPUServiceIPAM
func getFullyPopulatedDPUServiceIPAM() *dpuservicev1.DPUServiceIPAM {
	return &dpuservicev1.DPUServiceIPAM{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "some-object",
			Namespace: "dpf-operator-system",
		},
		Spec: dpuservicev1.DPUServiceIPAMSpec{
			IPV4Network: &dpuservicev1.IPV4Network{
				Network:      "192.168.0.0/20",
				GatewayIndex: ptr.To[int32](1),
				PrefixSize:   24,
				Exclusions: []string{
					"192.168.0.10",
					"192.168.2.30",
				},
				ExcludeRanges: []dpuservicev1.IPRange{
					{StartIP: "192.168.0.40", EndIP: "192.168.0.50"},
					{StartIP: "192.168.0.60", EndIP: "192.168.0.70"},
				},
				Allocations: map[string]string{
					"dpu-node-1": "192.168.1.0/24",
					"dpu-node-2": "192.168.2.0/24",
				},
				DefaultGateway: true,
				Routes:         []dpuservicev1.Route{{Dst: "5.5.5.0/16"}},
			},
			IPV4Subnet: &dpuservicev1.IPV4Subnet{
				Subnet:         "192.168.0.0/20",
				Gateway:        "192.168.0.1",
				PerNodeIPCount: 256,
				ExcludeRanges: []dpuservicev1.IPRange{
					{StartIP: "192.168.0.40", EndIP: "192.168.0.50"},
					{StartIP: "192.168.0.60", EndIP: "192.168.0.70"},
				},
				DefaultGateway: true,
				Routes:         []dpuservicev1.Route{{Dst: "5.5.5.0/16"}},
			},
		},
	}
}
