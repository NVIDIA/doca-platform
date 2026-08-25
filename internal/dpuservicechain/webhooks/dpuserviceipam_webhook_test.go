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

const ipv6NetworkCIDR = "2001:db8::/64"

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
		Entry("bad exclusion - IPv4-mapped IPv6 IP", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Subnet = nil
			//nolint:staticcheck // SA1019: Exclusions is deprecated but still supported
			ipam.Spec.IPV4Network.Exclusions[0] = "::ffff:192.168.0.10"
			return ipam
		}(), true),
		Entry("bad exclude range - IPv4-mapped IPv6 startIP", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Subnet = nil
			ipam.Spec.IPV4Network.ExcludeRanges = []dpuservicev1.IPRange{
				{StartIP: "::ffff:192.168.0.40", EndIP: "192.168.0.50"},
			}
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
		Entry("gatewayIndex is optional without dependent settings", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Subnet = nil
			ipam.Spec.IPV4Network.GatewayIndex = nil
			ipam.Spec.IPV4Network.DefaultGateway = false
			ipam.Spec.IPV4Network.Routes = nil
			return ipam
		}(), false),
		Entry("default gateway without a gatewayIndex", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Subnet = nil
			ipam.Spec.IPV4Network.GatewayIndex = nil
			ipam.Spec.IPV4Network.DefaultGateway = true
			ipam.Spec.IPV4Network.Routes = nil
			return ipam
		}(), false),
		Entry("routes without a gatewayIndex", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Subnet = nil
			ipam.Spec.IPV4Network.GatewayIndex = nil
			ipam.Spec.IPV4Network.DefaultGateway = false
			return ipam
		}(), false),
		Entry("bad route - dest not a valid cidr", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Subnet = nil
			ipam.Spec.IPV4Network.Routes[0].Dst = "not-a-cidr"
			return ipam
		}(), true),
		Entry("bad route - IPv4-mapped IPv6 CIDR", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Subnet = nil
			ipam.Spec.IPV4Network.Routes[0].Dst = "::ffff:192.0.2.0/120"
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
		Entry("prefixSize is 0 - out of range", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Subnet = nil
			ipam.Spec.IPV4Network.PrefixSize = 0
			return ipam
		}(), true),
		Entry("prefixSize is 33 - out of range", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Subnet = nil
			ipam.Spec.IPV4Network.PrefixSize = 33
			return ipam
		}(), true),
		Entry("prefixSize is 32 - valid", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Subnet = nil
			ipam.Spec.IPV4Network.Network = "192.168.0.0/20"
			ipam.Spec.IPV4Network.PrefixSize = 32
			ipam.Spec.IPV4Network.GatewayIndex = nil
			ipam.Spec.IPV4Network.Allocations = nil
			ipam.Spec.IPV4Network.DefaultGateway = false
			ipam.Spec.IPV4Network.Routes = nil
			return ipam
		}(), false),
		Entry("gatewayIndex out of range for prefix", func() *dpuservicev1.DPUServiceIPAM {
			// /24 prefix has 256 IPs (indices 0–255); gatewayIndex 256 is out of range.
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Subnet = nil
			ipam.Spec.IPV4Network.GatewayIndex = ptr.To[int32](256)
			return ipam
		}(), true),
		Entry("gatewayIndex at upper bound for prefix - valid", func() *dpuservicev1.DPUServiceIPAM {
			// /24 prefix has 256 IPs (indices 0–255); gatewayIndex 255 is the last valid index.
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Subnet = nil
			ipam.Spec.IPV4Network.GatewayIndex = ptr.To[int32](255)
			return ipam
		}(), false),
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
		Entry("bad gateway - IPv4-mapped IPv6 IP", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Network = nil
			ipam.Spec.IPV4Subnet.Gateway = "::ffff:192.168.0.1"
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
		Entry("gateway is required", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Network = nil
			ipam.Spec.IPV4Subnet.Gateway = ""
			ipam.Spec.IPV4Subnet.DefaultGateway = false
			ipam.Spec.IPV4Subnet.Routes = nil
			return ipam
		}(), true),
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
		Entry("perNodeIPCount is 0", func() *dpuservicev1.DPUServiceIPAM {
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Network = nil
			ipam.Spec.IPV4Subnet.PerNodeIPCount = 0
			return ipam
		}(), true),
		Entry("perNodeIPCount exceeds allocatable IPs in subnet", func() *dpuservicev1.DPUServiceIPAM {
			// 192.168.0.0/20 has 4094 allocatable IPs; perNodeIPCount 4095 must fail.
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Network = nil
			ipam.Spec.IPV4Subnet.PerNodeIPCount = 4095
			return ipam
		}(), true),
		Entry("perNodeIPCount equals allocatable IPs in subnet - valid", func() *dpuservicev1.DPUServiceIPAM {
			// 192.168.0.0/20 has 4094 allocatable IPs; perNodeIPCount 4094 must pass (1 full block).
			ipam := getFullyPopulatedDPUServiceIPAM()
			ipam.Spec.IPV4Network = nil
			ipam.Spec.IPV4Subnet.PerNodeIPCount = 4094
			return ipam
		}(), false),
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

	Describe("new allocation fields", func() {
		It("preserves the existing IPv4 network validation behavior", func() {
			legacy := ipamWithIPV4Network.DeepCopy()
			legacy.Spec.IPV4Network.Network = "192.0.2.0/24"
			legacy.Spec.IPV4Network.PrefixSize = 24
			legacy.Spec.IPV4Network.GatewayIndex = ptr.To[int32](255)
			legacy.Spec.IPV4Network.Routes = []dpuservicev1.Route{{Dst: "198.51.100.0/24"}}

			current := ipamWithNetwork.DeepCopy()
			current.Spec.Network.Network = legacy.Spec.IPV4Network.Network
			current.Spec.Network.PrefixSize = legacy.Spec.IPV4Network.PrefixSize
			current.Spec.Network.GatewayIndex = legacy.Spec.IPV4Network.GatewayIndex
			current.Spec.Network.Routes = legacy.Spec.IPV4Network.Routes

			_, legacyErr := webhook.ValidateCreate(context.Background(), legacy)
			_, currentErr := webhook.ValidateCreate(context.Background(), current)
			Expect(legacyErr).NotTo(HaveOccurred())
			Expect(currentErr).NotTo(HaveOccurred())
		})

		It("preserves the existing IPv4 subnet validation behavior", func() {
			legacy := ipamWithIPV4Subnet.DeepCopy()
			legacy.Spec.IPV4Subnet.Subnet = "192.0.2.0/24"
			legacy.Spec.IPV4Subnet.Gateway = "192.0.2.0"
			legacy.Spec.IPV4Subnet.PerNodeIPCount = 254

			current := ipamWithSubnet.DeepCopy()
			current.Spec.Subnet.Subnet = legacy.Spec.IPV4Subnet.Subnet
			current.Spec.Subnet.Gateway = legacy.Spec.IPV4Subnet.Gateway
			current.Spec.Subnet.PerNodeIPCount = legacy.Spec.IPV4Subnet.PerNodeIPCount

			_, legacyErr := webhook.ValidateCreate(context.Background(), legacy)
			_, currentErr := webhook.ValidateCreate(context.Background(), current)
			Expect(legacyErr).NotTo(HaveOccurred())
			Expect(currentErr).NotTo(HaveOccurred())
		})

		DescribeTable("validates IPv6 network configuration",
			func(network *dpuservicev1.Network, expectError bool) {
				ipam := ipamWithNetwork.DeepCopy()
				ipam.Spec.Network = network
				_, err := webhook.ValidateCreate(context.Background(), ipam)
				if expectError {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).NotTo(HaveOccurred())
				}
			},
			Entry("valid configuration", &dpuservicev1.Network{
				Network:      ipv6NetworkCIDR,
				PrefixSize:   80,
				GatewayIndex: ptr.To[int32](1),
				ExcludeRanges: []dpuservicev1.IPRange{
					{StartIP: "2001:db8::10", EndIP: "2001:db8::20"},
				},
				Allocations: map[string]string{"dpu-node-1": "2001:db8:0:0:1::/80"},
				Routes:      []dpuservicev1.Route{{Dst: "2001:db8:ffff::/64"}},
			}, false),
			Entry("prefix larger than IPv6 address size", &dpuservicev1.Network{
				Network: ipv6NetworkCIDR, PrefixSize: 129,
			}, true),
			Entry("negative gateway index", &dpuservicev1.Network{
				Network: ipv6NetworkCIDR, PrefixSize: 80, GatewayIndex: ptr.To[int32](-1),
			}, true),
			Entry("allocation from another address family", &dpuservicev1.Network{
				Network: ipv6NetworkCIDR, PrefixSize: 80,
				Allocations: map[string]string{"dpu-node-1": "192.0.2.0/24"},
			}, true),
			Entry("IPv4-mapped IPv6 allocation", &dpuservicev1.Network{
				Network: "::/0", PrefixSize: 120,
				Allocations: map[string]string{"dpu-node-1": "::ffff:192.0.2.0/120"},
			}, true),
			Entry("IPv4-mapped IPv6 network", &dpuservicev1.Network{
				Network: "::ffff:192.0.2.0/120", PrefixSize: 124,
			}, true),
		)

		DescribeTable("validates IPv6 subnet configuration",
			func(subnet *dpuservicev1.Subnet, expectError bool) {
				ipam := ipamWithSubnet.DeepCopy()
				ipam.Spec.Subnet = subnet
				_, err := webhook.ValidateCreate(context.Background(), ipam)
				if expectError {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).NotTo(HaveOccurred())
				}
			},
			Entry("ordinary subnet", &dpuservicev1.Subnet{
				Subnet: "2001:db8::/120", Gateway: "2001:db8::1", PerNodeIPCount: 8,
				ExcludeRanges: []dpuservicev1.IPRange{
					{StartIP: "2001:db8::10", EndIP: "2001:db8::20"},
				},
				Routes: []dpuservicev1.Route{{Dst: "2001:db8:ffff::/64"}},
			}, false),
			Entry("/127 subnet is rejected", &dpuservicev1.Subnet{
				Subnet: "2001:db8::/127", Gateway: "2001:db8::1", PerNodeIPCount: 2,
			}, true),
			Entry("/128 subnet is rejected", &dpuservicev1.Subnet{
				Subnet: "2001:db8::1/128", Gateway: "2001:db8::1", PerNodeIPCount: 1,
			}, true),
			Entry("per-node count exceeds the allocatable IPs", &dpuservicev1.Subnet{
				Subnet: "2001:db8::/126", Gateway: "2001:db8::1", PerNodeIPCount: 4,
			}, true),
			Entry("route from another address family", &dpuservicev1.Subnet{
				Subnet: "2001:db8::/120", Gateway: "2001:db8::1", PerNodeIPCount: 8,
				Routes: []dpuservicev1.Route{{Dst: "192.0.2.0/24"}},
			}, true),
			Entry("IPv4 subnet with IPv4-mapped IPv6 gateway", &dpuservicev1.Subnet{
				Subnet: "192.0.2.0/24", Gateway: "::ffff:192.0.2.1", PerNodeIPCount: 8,
			}, true),
		)

		DescribeTable("allows migration from a deprecated field to its replacement",
			func(oldIPAM, newIPAM *dpuservicev1.DPUServiceIPAM) {
				_, err := webhook.ValidateUpdate(context.Background(), oldIPAM, newIPAM)
				Expect(err).NotTo(HaveOccurred())
			},
			Entry("network", ipamWithIPV4Network.DeepCopy(), ipamWithNetwork.DeepCopy()),
			Entry("subnet", ipamWithIPV4Subnet.DeepCopy(), ipamWithSubnet.DeepCopy()),
		)

		DescribeTable("allows IPv6 migration from a deprecated field to its replacement",
			func(oldIPAM, newIPAM *dpuservicev1.DPUServiceIPAM) {
				_, err := webhook.ValidateUpdate(context.Background(), oldIPAM, newIPAM)
				Expect(err).NotTo(HaveOccurred())
			},
			Entry("network", func() *dpuservicev1.DPUServiceIPAM {
				ipam := ipamWithIPV4Network.DeepCopy()
				ipam.Spec.IPV4Network.Network = ipv6NetworkCIDR
				ipam.Spec.IPV4Network.PrefixSize = 80
				return ipam
			}(), func() *dpuservicev1.DPUServiceIPAM {
				ipam := ipamWithNetwork.DeepCopy()
				ipam.Spec.Network.Network = ipv6NetworkCIDR
				ipam.Spec.Network.PrefixSize = 80
				return ipam
			}()),
			Entry("subnet", func() *dpuservicev1.DPUServiceIPAM {
				ipam := ipamWithIPV4Subnet.DeepCopy()
				ipam.Spec.IPV4Subnet.Subnet = "2001:db8::/120"
				ipam.Spec.IPV4Subnet.Gateway = "2001:db8::1"
				ipam.Spec.IPV4Subnet.PerNodeIPCount = 8
				return ipam
			}(), func() *dpuservicev1.DPUServiceIPAM {
				ipam := ipamWithSubnet.DeepCopy()
				ipam.Spec.Subnet.Subnet = "2001:db8::/120"
				ipam.Spec.Subnet.Gateway = "2001:db8::1"
				ipam.Spec.Subnet.PerNodeIPCount = 8
				return ipam
			}()),
		)

		It("rejects changing the address family", func() {
			oldIPAM := ipamWithNetwork.DeepCopy()
			newIPAM := ipamWithNetwork.DeepCopy()
			newIPAM.Spec.Network.Network = ipv6NetworkCIDR
			newIPAM.Spec.Network.PrefixSize = 80

			_, err := webhook.ValidateUpdate(context.Background(), oldIPAM, newIPAM)
			Expect(err).To(MatchError(ContainSubstring("transitioning between address families is not supported")))
		})

		DescribeTable("reports the narrowest supported subnet per address family",
			func(subnet, gateway, expectedMessage string) {
				ipam := ipamWithSubnet.DeepCopy()
				ipam.Spec.Subnet.Subnet = subnet
				ipam.Spec.Subnet.Gateway = gateway
				ipam.Spec.Subnet.PerNodeIPCount = 1

				_, err := webhook.ValidateCreate(context.Background(), ipam)
				Expect(err).To(MatchError(ContainSubstring(expectedMessage)))
			},
			Entry("IPv4 /31", "192.0.2.0/31", "192.0.2.1",
				"subnet 192.0.2.0/31 must be larger than /30 — /31 and /32 are not supported"),
			Entry("IPv4 /32", "192.0.2.1/32", "192.0.2.1",
				"subnet 192.0.2.1/32 must be larger than /30 — /31 and /32 are not supported"),
			Entry("IPv6 /127", "2001:db8::/127", "2001:db8::1",
				"subnet 2001:db8::/127 must be larger than /126 — /127 and /128 are not supported"),
			Entry("IPv6 /128", "2001:db8::1/128", "2001:db8::1",
				"subnet 2001:db8::1/128 must be larger than /126 — /127 and /128 are not supported"),
		)
	})

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
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Network: &dpuservicev1.Network{Network: "10.0.0.0/20", PrefixSize: 24}}},
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Network: &dpuservicev1.Network{Network: "10.0.0.0/20", PrefixSize: 24}}},
			false, ""),
		Entry("both set same value - no change",
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Network: &dpuservicev1.Network{Network: "10.0.0.0/20", PrefixSize: 24, SubnetsPerDPUCluster: ptr.To[int32](2)}}},
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Network: &dpuservicev1.Network{Network: "10.0.0.0/20", PrefixSize: 24, SubnetsPerDPUCluster: ptr.To[int32](2)}}},
			false, ""),
		Entry("both set different value - value change is allowed",
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Network: &dpuservicev1.Network{Network: "10.0.0.0/20", PrefixSize: 24, SubnetsPerDPUCluster: ptr.To[int32](2)}}},
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Network: &dpuservicev1.Network{Network: "10.0.0.0/20", PrefixSize: 24, SubnetsPerDPUCluster: ptr.To[int32](4)}}},
			false, ""),
		Entry("unset to set - not allowed",
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Network: &dpuservicev1.Network{Network: "10.0.0.0/20", PrefixSize: 24}}},
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Network: &dpuservicev1.Network{Network: "10.0.0.0/20", PrefixSize: 24, SubnetsPerDPUCluster: ptr.To[int32](2)}}},
			true, "subnetsPerDPUCluster cannot be toggled between set and unset"),
		Entry("set to unset - not allowed",
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Network: &dpuservicev1.Network{Network: "10.0.0.0/20", PrefixSize: 24, SubnetsPerDPUCluster: ptr.To[int32](2)}}},
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Network: &dpuservicev1.Network{Network: "10.0.0.0/20", PrefixSize: 24}}},
			true, "subnetsPerDPUCluster cannot be toggled between set and unset"),
		Entry("grow - allowed",
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Network: &dpuservicev1.Network{Network: "10.0.0.0/20", PrefixSize: 24, SubnetsPerDPUCluster: ptr.To[int32](2)}}},
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Network: &dpuservicev1.Network{Network: "10.0.0.0/20", PrefixSize: 24, SubnetsPerDPUCluster: ptr.To[int32](4)}}},
			false, ""),
		Entry("shrink - not allowed",
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Network: &dpuservicev1.Network{Network: "10.0.0.0/20", PrefixSize: 24, SubnetsPerDPUCluster: ptr.To[int32](4)}}},
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Network: &dpuservicev1.Network{Network: "10.0.0.0/20", PrefixSize: 24, SubnetsPerDPUCluster: ptr.To[int32](2)}}},
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
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Subnet: &dpuservicev1.Subnet{Subnet: "10.0.0.0/24", Gateway: "10.0.0.1", PerNodeIPCount: 10}}},
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Subnet: &dpuservicev1.Subnet{Subnet: "10.0.0.0/24", Gateway: "10.0.0.1", PerNodeIPCount: 10}}},
			false, ""),
		Entry("both set same value - no change",
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Subnet: &dpuservicev1.Subnet{Subnet: "10.0.0.0/24", Gateway: "10.0.0.1", PerNodeIPCount: 10, BlocksPerDPUCluster: ptr.To[int32](2)}}},
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Subnet: &dpuservicev1.Subnet{Subnet: "10.0.0.0/24", Gateway: "10.0.0.1", PerNodeIPCount: 10, BlocksPerDPUCluster: ptr.To[int32](2)}}},
			false, ""),
		Entry("both set different value - value change is allowed",
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Subnet: &dpuservicev1.Subnet{Subnet: "10.0.0.0/24", Gateway: "10.0.0.1", PerNodeIPCount: 10, BlocksPerDPUCluster: ptr.To[int32](2)}}},
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Subnet: &dpuservicev1.Subnet{Subnet: "10.0.0.0/24", Gateway: "10.0.0.1", PerNodeIPCount: 10, BlocksPerDPUCluster: ptr.To[int32](4)}}},
			false, ""),
		Entry("unset to set - not allowed",
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Subnet: &dpuservicev1.Subnet{Subnet: "10.0.0.0/24", Gateway: "10.0.0.1", PerNodeIPCount: 10}}},
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Subnet: &dpuservicev1.Subnet{Subnet: "10.0.0.0/24", Gateway: "10.0.0.1", PerNodeIPCount: 10, BlocksPerDPUCluster: ptr.To[int32](2)}}},
			true, "blocksPerDPUCluster cannot be toggled between set and unset"),
		Entry("set to unset - not allowed",
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Subnet: &dpuservicev1.Subnet{Subnet: "10.0.0.0/24", Gateway: "10.0.0.1", PerNodeIPCount: 10, BlocksPerDPUCluster: ptr.To[int32](2)}}},
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Subnet: &dpuservicev1.Subnet{Subnet: "10.0.0.0/24", Gateway: "10.0.0.1", PerNodeIPCount: 10}}},
			true, "blocksPerDPUCluster cannot be toggled between set and unset"),
		Entry("grow - allowed",
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Subnet: &dpuservicev1.Subnet{Subnet: "10.0.0.0/24", Gateway: "10.0.0.1", PerNodeIPCount: 10, BlocksPerDPUCluster: ptr.To[int32](2)}}},
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Subnet: &dpuservicev1.Subnet{Subnet: "10.0.0.0/24", Gateway: "10.0.0.1", PerNodeIPCount: 10, BlocksPerDPUCluster: ptr.To[int32](4)}}},
			false, ""),
		Entry("shrink - not allowed",
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Subnet: &dpuservicev1.Subnet{Subnet: "10.0.0.0/24", Gateway: "10.0.0.1", PerNodeIPCount: 10, BlocksPerDPUCluster: ptr.To[int32](4)}}},
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Subnet: &dpuservicev1.Subnet{Subnet: "10.0.0.0/24", Gateway: "10.0.0.1", PerNodeIPCount: 10, BlocksPerDPUCluster: ptr.To[int32](2)}}},
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
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Network: &dpuservicev1.Network{Network: "10.0.0.0/20", PrefixSize: 24}}},
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Network: &dpuservicev1.Network{Network: "10.0.0.0/20", PrefixSize: 24}}},
			false, ""),
		Entry("change - not allowed",
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Network: &dpuservicev1.Network{Network: "10.0.0.0/20", PrefixSize: 24}}},
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Network: &dpuservicev1.Network{Network: "10.0.0.0/20", PrefixSize: 25}}},
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
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Subnet: &dpuservicev1.Subnet{Subnet: "10.0.0.0/24", Gateway: "10.0.0.1", PerNodeIPCount: 10}}},
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Subnet: &dpuservicev1.Subnet{Subnet: "10.0.0.0/24", Gateway: "10.0.0.1", PerNodeIPCount: 10}}},
			false, ""),
		Entry("change - not allowed",
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Subnet: &dpuservicev1.Subnet{Subnet: "10.0.0.0/24", Gateway: "10.0.0.1", PerNodeIPCount: 10}}},
			dpuservicev1.DPUServiceIPAM{ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system"}, Spec: dpuservicev1.DPUServiceIPAMSpec{IPV4Subnet: &dpuservicev1.Subnet{Subnet: "10.0.0.0/24", Gateway: "10.0.0.1", PerNodeIPCount: 20}}},
			true, "perNodeIPCount is immutable"),
	)
})

var ipamWithIPV4Subnet = &dpuservicev1.DPUServiceIPAM{
	ObjectMeta: metav1.ObjectMeta{
		Namespace: "dpf-operator-system",
	},
	Spec: dpuservicev1.DPUServiceIPAMSpec{
		IPV4Subnet: &dpuservicev1.Subnet{
			Subnet:         "10.0.0.0/24",
			Gateway:        "10.0.0.1",
			PerNodeIPCount: 10,
		},
	},
}
var ipamWithIPV4Network = &dpuservicev1.DPUServiceIPAM{
	ObjectMeta: metav1.ObjectMeta{
		Namespace: "dpf-operator-system",
	},
	Spec: dpuservicev1.DPUServiceIPAMSpec{
		IPV4Network: &dpuservicev1.Network{
			Network:    "10.0.0.0/24",
			PrefixSize: 30,
		},
	},
}

var ipamWithNetwork = &dpuservicev1.DPUServiceIPAM{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "some-object",
		Namespace: "dpf-operator-system",
	},
	Spec: dpuservicev1.DPUServiceIPAMSpec{
		Network: &dpuservicev1.Network{
			Network:    "10.0.0.0/24",
			PrefixSize: 30,
		},
	},
}

var ipamWithSubnet = &dpuservicev1.DPUServiceIPAM{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "some-object",
		Namespace: "dpf-operator-system",
	},
	Spec: dpuservicev1.DPUServiceIPAMSpec{
		Subnet: &dpuservicev1.Subnet{
			Subnet:         "10.0.0.0/24",
			Gateway:        "10.0.0.1",
			PerNodeIPCount: 10,
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
			IPV4Network: &dpuservicev1.Network{
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
			IPV4Subnet: &dpuservicev1.Subnet{
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
