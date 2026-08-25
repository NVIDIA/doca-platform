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

package iputils

import (
	"math/big"
	"net/netip"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("IP ranges", func() {
	DescribeTable("counts addresses in a prefix",
		func(addressBits, prefixLen int, expected *big.Int) {
			Expect(PrefixAddressCount(addressBits, prefixLen)).To(Equal(expected))
		},
		Entry("IPv4", 32, 24, big.NewInt(256)),
		Entry("IPv6", 128, 64, new(big.Int).Lsh(big.NewInt(1), 64)),
		Entry("invalid prefix length", 32, 33, big.NewInt(0)),
	)

	DescribeTable("builds an inclusive range from a prefix",
		func(prefix, start, end string) {
			got, err := RangeFromPrefix(netip.MustParsePrefix(prefix))
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Start).To(Equal(netip.MustParseAddr(start)))
			Expect(got.End).To(Equal(netip.MustParseAddr(end)))
			Expect(got.End.BitLen()).To(Equal(got.Start.BitLen()))
		},
		Entry("IPv4 prefix", "192.0.2.42/24", "192.0.2.0", "192.0.2.255"),
		Entry("IPv6 prefix", "2001:db8::1234/120", "2001:db8::1200", "2001:db8::12ff"),
		Entry("IPv6 prefix ending in mapped-address window", "::/80", "::", "::ffff:ffff:ffff"),
		Entry("IPv6 /127", "2001:db8::/127", "2001:db8::", "2001:db8::1"),
		Entry("IPv6 /128", "2001:db8::1/128", "2001:db8::1", "2001:db8::1"),
	)

	It("rejects an invalid prefix", func() {
		_, err := RangeFromPrefix(netip.Prefix{})
		Expect(err).To(MatchError(ContainSubstring("invalid IP prefix")))
	})

	It("detects address-family boundaries", func() {
		got, ok := Add(netip.MustParseAddr("ffff:ffff:ffff:ffff:ffff:ffff:ffff:fffe"), big.NewInt(1))
		Expect(ok).To(BeTrue())
		Expect(got).To(Equal(netip.MustParseAddr("ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff")))

		_, ok = Add(got, big.NewInt(1))
		Expect(ok).To(BeFalse())
		_, ok = Add(netip.MustParseAddr("255.255.255.255"), big.NewInt(1))
		Expect(ok).To(BeFalse())
		_, ok = Add(netip.MustParseAddr("192.0.2.1"), big.NewInt(-1))
		Expect(ok).To(BeFalse())
	})

	It("preserves IPv6 across the IPv4-mapped address window", func() {
		got, ok := Add(netip.MustParseAddr("::fffe:ffff:ffff"), big.NewInt(1))
		Expect(ok).To(BeTrue())
		Expect(got).To(Equal(netip.MustParseAddr("::ffff:0:0")))
		Expect(got.BitLen()).To(Equal(128))
		Expect(got.Is4In6()).To(BeTrue())
		Expect(got.Is4()).To(BeFalse())

		got, ok = Add(netip.MustParseAddr("::ffff:ffff:ffff"), big.NewInt(1))
		Expect(ok).To(BeTrue())
		Expect(got).To(Equal(netip.MustParseAddr("::1:0:0:0")))
		Expect(got.BitLen()).To(Equal(128))
	})

	It("rejects distances across families and reverse ranges", func() {
		_, ok := Distance(netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("2001:db8::1"))
		Expect(ok).To(BeFalse())
		_, ok = Distance(netip.MustParseAddr("2001:db8::2"), netip.MustParseAddr("2001:db8::1"))
		Expect(ok).To(BeFalse())

		distance, ok := Distance(netip.MustParseAddr("2001:db8::1"), netip.MustParseAddr("2001:db8::ffff"))
		Expect(ok).To(BeTrue())
		Expect(distance.Int64()).To(Equal(int64(65534)))
	})

	It("merges and subtracts IPv6 ranges", func() {
		ranges := []IPRange{
			{Start: netip.MustParseAddr("2001:db8::10"), End: netip.MustParseAddr("2001:db8::1f")},
			{Start: netip.MustParseAddr("2001:db8::20"), End: netip.MustParseAddr("2001:db8::2f")},
			{Start: netip.MustParseAddr("192.0.2.1"), End: netip.MustParseAddr("192.0.2.2")},
		}
		merged := MergeRanges(ranges)
		Expect(merged).To(Equal([]IPRange{
			{Start: netip.MustParseAddr("192.0.2.1"), End: netip.MustParseAddr("192.0.2.2")},
			{Start: netip.MustParseAddr("2001:db8::10"), End: netip.MustParseAddr("2001:db8::2f")},
		}))

		remaining := SubtractRange(merged[1:], IPRange{
			Start: netip.MustParseAddr("2001:db8::18"), End: netip.MustParseAddr("2001:db8::27"),
		})
		Expect(remaining).To(Equal([]IPRange{
			{Start: netip.MustParseAddr("2001:db8::10"), End: netip.MustParseAddr("2001:db8::17")},
			{Start: netip.MustParseAddr("2001:db8::28"), End: netip.MustParseAddr("2001:db8::2f")},
		}))
	})

	It("rejects IPv4-mapped IPv6 input", func() {
		_, err := ParseAddr("::ffff:192.0.2.1")
		Expect(err).To(MatchError(ContainSubstring("IPv4-mapped IPv6")))
	})
})
