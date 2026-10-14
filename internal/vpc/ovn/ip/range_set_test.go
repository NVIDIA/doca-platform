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

package ip_test

import (
	"net"

	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/ip"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("range sets", func() {
	It("should detect set membership correctly", func() {
		p := ip.RangeSet{
			ip.Range{Subnet: mustSubnet("192.168.0.0/24")},
			ip.Range{Subnet: mustSubnet("172.16.1.0/24")},
		}

		err := p.Canonicalize()
		Expect(err).NotTo(HaveOccurred())

		Expect(p.Contains(net.IP{192, 168, 0, 55})).To(BeTrue())

		r, err := p.RangeFor(net.IP{192, 168, 0, 55})
		Expect(err).NotTo(HaveOccurred())
		Expect(r).To(Equal(&p[0]))

		r, err = p.RangeFor(net.IP{192, 168, 99, 99})
		Expect(r).To(BeNil())
		Expect(err).To(MatchError("192.168.99.99 not in range set 192.168.0.1-192.168.0.254,172.16.1.1-172.16.1.254"))
	})

	It("should discover overlaps within a set", func() {
		p := ip.RangeSet{
			ip.Range{Subnet: mustSubnet("192.168.0.0/20")},
			ip.Range{Subnet: mustSubnet("192.168.2.0/24")},
		}

		err := p.Canonicalize()
		Expect(err).To(MatchError("subnets 192.168.0.1-192.168.15.254 and 192.168.2.1-192.168.2.254 overlap"))
	})

	It("should discover overlaps outside a set", func() {
		p1 := ip.RangeSet{
			ip.Range{Subnet: mustSubnet("192.168.0.0/20")},
		}
		p2 := ip.RangeSet{
			ip.Range{Subnet: mustSubnet("192.168.2.0/24")},
		}

		Expect(p1.Canonicalize()).NotTo(HaveOccurred())
		Expect(p2.Canonicalize()).NotTo(HaveOccurred())

		Expect(p1.Overlaps(&p2)).To(BeTrue())
		Expect(p2.Overlaps(&p1)).To(BeTrue())
	})
})
