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

package iprange_test

import (
	"net"

	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/topology/iprange"

	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"
)

var _ = Describe("IPRange", func() {
	Describe("String", func() {
		DescribeTable("should return correct string representation",
			func(r iprange.IPRange, expected string) {
				Expect(r.String()).To(Equal(expected))
			},
			Entry("Empty IPRange", iprange.IPRange{}, ""),
			Entry("Single IP",
				iprange.IPRange{IP: net.ParseIP("192.168.1.1")},
				"192.168.1.1"),
			Entry("IPRange with Start and End",
				iprange.IPRange{Start: net.ParseIP("192.168.1.1"), End: net.ParseIP("192.168.1.10")},
				"192.168.1.1..192.168.1.10"),
			Entry("IPRange with Start and End equal",
				iprange.IPRange{Start: net.ParseIP("192.168.1.1"), End: net.ParseIP("192.168.1.1")},
				"192.168.1.1"),
			Entry("IPRange with both single IP and range",
				iprange.IPRange{
					IP:    net.ParseIP("192.168.1.1"),
					Start: net.ParseIP("192.168.2.1"),
					End:   net.ParseIP("192.168.2.10"),
				},
				"192.168.1.1 192.168.2.1..192.168.2.10"),
		)
	})

	Describe("IPRangesString", func() {
		DescribeTable("should return correct string representation for multiple IP ranges",
			func(ranges []iprange.IPRange, expected string) {
				Expect(iprange.IPRangesString(ranges)).To(Equal(expected))
			},
			Entry("Empty IPRanges", []iprange.IPRange{}, ""),
			Entry("Single IPRange",
				[]iprange.IPRange{{IP: net.ParseIP("192.168.1.1")}},
				"192.168.1.1"),
			Entry("Multiple IPRanges",
				[]iprange.IPRange{
					{IP: net.ParseIP("192.168.1.1")},
					{Start: net.ParseIP("192.168.2.1"), End: net.ParseIP("192.168.2.10")},
				},
				"192.168.1.1 192.168.2.1..192.168.2.10"),
			Entry("Multiple IPRanges with empty range",
				[]iprange.IPRange{
					{IP: net.ParseIP("192.168.1.1")},
					{},
					{Start: net.ParseIP("192.168.2.1"), End: net.ParseIP("192.168.2.10")},
				},
				"192.168.1.1 192.168.2.1..192.168.2.10"),
			Entry("Multiple IPRanges with same start and end",
				[]iprange.IPRange{
					{IP: net.ParseIP("192.168.1.1")},
					{Start: net.ParseIP("192.168.2.1"), End: net.ParseIP("192.168.2.1")},
				},
				"192.168.1.1 192.168.2.1"),
		)
	})

	Describe("IPRangeFromExcludeIPsSpec", func() {
		var subnet *net.IPNet

		BeforeEach(func() {
			_, subnet, _ = net.ParseCIDR("192.168.1.0/24")
		})

		Context("when input is valid", func() {
			It("should handle empty ExcludeIPs", func() {
				result, err := iprange.IPRangeFromExcludeIPsSpec([]vpcv1.ExcludeIPsEntry{}, subnet)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(BeNil())
			})

			It("should handle valid single IP", func() {
				excludeIPs := []vpcv1.ExcludeIPsEntry{
					{IP: ptr.To("192.168.1.1")},
				}
				result, err := iprange.IPRangeFromExcludeIPsSpec(excludeIPs, subnet)

				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(HaveLen(1))
				Expect(result[0].IP.String()).To(Equal("192.168.1.1"))
				Expect(result[0].Start).To(BeNil())
				Expect(result[0].End).To(BeNil())
			})

			It("should handle valid IP range", func() {
				excludeIPs := []vpcv1.ExcludeIPsEntry{
					{Range: &vpcv1.RangeEntry{Start: "192.168.1.1", End: "192.168.1.10"}},
				}
				result, err := iprange.IPRangeFromExcludeIPsSpec(excludeIPs, subnet)

				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(HaveLen(1))
				Expect(result[0].IP).To(BeNil())
				Expect(result[0].Start.String()).To(Equal("192.168.1.1"))
				Expect(result[0].End.String()).To(Equal("192.168.1.10"))
			})

			It("should handle multiple valid entries", func() {
				excludeIPs := []vpcv1.ExcludeIPsEntry{
					{IP: ptr.To("192.168.1.1")},
					{Range: &vpcv1.RangeEntry{Start: "192.168.1.5", End: "192.168.1.10"}},
				}
				result, err := iprange.IPRangeFromExcludeIPsSpec(excludeIPs, subnet)

				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(HaveLen(2))

				Expect(result[0].IP.String()).To(Equal("192.168.1.1"))
				Expect(result[0].Start).To(BeNil())
				Expect(result[0].End).To(BeNil())

				Expect(result[1].IP).To(BeNil())
				Expect(result[1].Start.String()).To(Equal("192.168.1.5"))
				Expect(result[1].End.String()).To(Equal("192.168.1.10"))
			})
		})

		Context("when input is invalid", func() {
			It("should return error for nil subnet", func() {
				excludeIPs := []vpcv1.ExcludeIPsEntry{
					{IP: ptr.To("192.168.1.1")},
				}
				_, err := iprange.IPRangeFromExcludeIPsSpec(excludeIPs, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("subnet is nil"))
			})

			It("should return error for invalid IP", func() {
				excludeIPs := []vpcv1.ExcludeIPsEntry{
					{IP: ptr.To("invalid-ip")},
				}
				_, err := iprange.IPRangeFromExcludeIPsSpec(excludeIPs, subnet)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to parse IP address"))
			})

			It("should return error for IPv6 address", func() {
				excludeIPs := []vpcv1.ExcludeIPsEntry{
					{IP: ptr.To("2001:db8::1")},
				}
				_, err := iprange.IPRangeFromExcludeIPsSpec(excludeIPs, subnet)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("not ipv4"))
			})

			It("should return error for IP outside subnet", func() {
				excludeIPs := []vpcv1.ExcludeIPsEntry{
					{IP: ptr.To("10.0.0.1")},
				}
				_, err := iprange.IPRangeFromExcludeIPsSpec(excludeIPs, subnet)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("not part of the subnet"))
			})

			It("should return error for invalid IP range start", func() {
				excludeIPs := []vpcv1.ExcludeIPsEntry{
					{Range: &vpcv1.RangeEntry{Start: "invalid-ip", End: "192.168.1.10"}},
				}
				_, err := iprange.IPRangeFromExcludeIPsSpec(excludeIPs, subnet)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to parse IP adresses"))
			})

			It("should return error for invalid IP range end", func() {
				excludeIPs := []vpcv1.ExcludeIPsEntry{
					{Range: &vpcv1.RangeEntry{Start: "192.168.1.1", End: "invalid-ip"}},
				}
				_, err := iprange.IPRangeFromExcludeIPsSpec(excludeIPs, subnet)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to parse IP adresses"))
			})

			It("should return error for IP range start outside subnet", func() {
				excludeIPs := []vpcv1.ExcludeIPsEntry{
					{Range: &vpcv1.RangeEntry{Start: "10.0.0.1", End: "192.168.1.10"}},
				}
				_, err := iprange.IPRangeFromExcludeIPsSpec(excludeIPs, subnet)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("not part of the subnet"))
			})

			It("should return error for IP range end outside subnet", func() {
				excludeIPs := []vpcv1.ExcludeIPsEntry{
					{Range: &vpcv1.RangeEntry{Start: "192.168.1.1", End: "10.0.0.1"}},
				}
				_, err := iprange.IPRangeFromExcludeIPsSpec(excludeIPs, subnet)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("not part of the subnet"))
			})

			It("should return error for IP range start IPv6 address", func() {
				excludeIPs := []vpcv1.ExcludeIPsEntry{
					{Range: &vpcv1.RangeEntry{Start: "2001:db8::1", End: "192.168.1.10"}},
				}
				_, err := iprange.IPRangeFromExcludeIPsSpec(excludeIPs, subnet)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("not ipv4"))
			})

			It("should return error for IP range start IPv6 address", func() {
				excludeIPs := []vpcv1.ExcludeIPsEntry{
					{Range: &vpcv1.RangeEntry{Start: "192.168.1.1", End: "2001:db8::10"}},
				}
				_, err := iprange.IPRangeFromExcludeIPsSpec(excludeIPs, subnet)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("not ipv4"))
			})

			It("should return error for IP range start greater than end", func() {
				excludeIPs := []vpcv1.ExcludeIPsEntry{
					{Range: &vpcv1.RangeEntry{Start: "192.168.1.5", End: "192.168.1.1"}},
				}
				_, err := iprange.IPRangeFromExcludeIPsSpec(excludeIPs, subnet)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("greater than end"))
			})
		})
	})

	Describe("IPRangeFromIP", func() {
		It("should create IPRange from valid IP", func() {
			ip := net.ParseIP("192.168.1.1")
			result := iprange.IPRangeFromIP(ip)

			Expect(result.IP.String()).To(Equal("192.168.1.1"))
			Expect(result.Start).To(BeNil())
			Expect(result.End).To(BeNil())
		})

		It("should handle nil IP", func() {
			result := iprange.IPRangeFromIP(nil)

			Expect(result.IP).To(BeNil())
			Expect(result.Start).To(BeNil())
			Expect(result.End).To(BeNil())
		})
	})
})
