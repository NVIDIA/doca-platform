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
	"fmt"
	"net"

	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/ip"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	testID = "ID"
)

type AllocatorTestCase struct {
	subnets      []string
	preallocIps  map[string]string
	expectResult string
	lastIP       string
}

func mkAlloc() ip.IPAllocator {
	p := ip.RangeSet{
		ip.Range{Subnet: mustSubnet("192.168.1.0/29"), Gateway: net.ParseIP("192.168.1.1")},
	}
	Expect(p.Canonicalize()).NotTo(HaveOccurred())
	return ip.NewIPAllocator(&p, nil)
}

func newAllocatorWithMultiRanges() ip.IPAllocator {
	p := ip.RangeSet{
		ip.Range{RangeStart: net.IP{192, 168, 1, 0}, RangeEnd: net.IP{192, 168, 1, 3}, Subnet: mustSubnet("192.168.1.0/30")},
		ip.Range{RangeStart: net.IP{192, 168, 2, 0}, RangeEnd: net.IP{192, 168, 2, 3}, Subnet: mustSubnet("192.168.2.0/30")},
	}
	Expect(p.Canonicalize()).NotTo(HaveOccurred())
	return ip.NewIPAllocator(&p, nil)
}

func (t AllocatorTestCase) run(idx int) (*ip.IPAllocation, error) {
	_, _ = fmt.Fprintln(GinkgoWriter, "Index:", idx)
	p := ip.RangeSet{}
	for _, s := range t.subnets {
		ipaddr, subnet, err := net.ParseCIDR(s)
		if err != nil {
			return nil, err
		}
		subnet.IP = ipaddr

		p = append(p, ip.Range{Subnet: *subnet, Gateway: ip.NextIP(subnet.IP)})
	}
	Expect(p.Canonicalize()).To(Succeed())
	alloc := ip.NewIPAllocator(&p, nil)

	prealloc := make(map[string]net.IP)
	for id, addr := range t.preallocIps {
		prealloc[id] = net.ParseIP(addr)
	}
	Expect(alloc.Preallocate(prealloc)).To(Succeed())
	alloc.SetLastReservedIP(net.ParseIP(t.lastIP))

	return alloc.Allocate(testID, nil)
}

func checkAlloc(a ip.IPAllocator, id string, expectedIP net.IP) {
	ipa, err := a.Allocate(id, nil)
	if expectedIP == nil {
		ExpectWithOffset(1, err).To(HaveOccurred())
		return
	}
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, ipa.Address.IP).To(Equal(expectedIP))
}

var _ = Describe("allocator", func() {
	Context("RangeIter", func() {
		It("should loop correctly from the beginning", func() {
			a := mkAlloc()
			checkAlloc(a, "1", net.IP{192, 168, 1, 2})
			checkAlloc(a, "2", net.IP{192, 168, 1, 3})
			checkAlloc(a, "3", net.IP{192, 168, 1, 4})
			checkAlloc(a, "4", net.IP{192, 168, 1, 5})
			checkAlloc(a, "5", net.IP{192, 168, 1, 6})
			checkAlloc(a, "6", nil)
		})

		It("should loop correctly from the end", func() {
			a := mkAlloc()
			a.SetLastReservedIP(net.IP{192, 168, 1, 6})
			checkAlloc(a, "1", net.IP{192, 168, 1, 2})
			checkAlloc(a, "2", net.IP{192, 168, 1, 3})
		})
		It("should loop correctly from the middle", func() {
			a := mkAlloc()
			a.SetLastReservedIP(net.IP{192, 168, 1, 3})
			checkAlloc(a, "0", net.IP{192, 168, 1, 4})
			checkAlloc(a, "1", net.IP{192, 168, 1, 5})
			checkAlloc(a, "2", net.IP{192, 168, 1, 6})
			checkAlloc(a, "3", net.IP{192, 168, 1, 2})
			checkAlloc(a, "4", net.IP{192, 168, 1, 3})
			checkAlloc(a, "5", nil)
		})
	})

	Context("when has free ip", func() {
		It("should allocate ips in round robin", func() {
			testCases := []AllocatorTestCase{
				// fresh start
				{
					subnets:      []string{"10.0.0.0/29"},
					preallocIps:  map[string]string{},
					expectResult: "10.0.0.2",
					lastIP:       "",
				},
				{
					subnets:      []string{"2001:db8:1::0/64"},
					preallocIps:  map[string]string{},
					expectResult: "2001:db8:1::2",
					lastIP:       "",
				},
				{
					subnets:      []string{"10.0.0.0/30"},
					preallocIps:  map[string]string{},
					expectResult: "10.0.0.2",
					lastIP:       "",
				},
				{
					subnets: []string{"10.0.0.0/29"},
					preallocIps: map[string]string{
						"foo": "10.0.0.2",
					},
					expectResult: "10.0.0.3",
					lastIP:       "",
				},
				// next ip of last reserved ip
				{
					subnets:      []string{"10.0.0.0/29"},
					preallocIps:  map[string]string{},
					expectResult: "10.0.0.6",
					lastIP:       "10.0.0.5",
				},
				// next ip of last reserved ip with pre-allocations
				{
					subnets: []string{"10.0.0.0/29"},
					preallocIps: map[string]string{
						"foo": "10.0.0.4",
						"bar": "10.0.0.5",
					},
					expectResult: "10.0.0.6",
					lastIP:       "10.0.0.3",
				},
				// round-robin to the beginning
				{
					subnets: []string{"10.0.0.0/29"},
					preallocIps: map[string]string{
						"foo": "10.0.0.6",
					},
					expectResult: "10.0.0.2",
					lastIP:       "10.0.0.5",
				},
				// lastIP is out of range
				{
					subnets: []string{"10.0.0.0/29"},
					preallocIps: map[string]string{
						"foo": "10.0.0.2",
					},
					expectResult: "10.0.0.3",
					lastIP:       "10.0.0.128",
				},
				// subnet is completely full except for lastip
				// wrap around and reserve lastIP
				{
					subnets: []string{"10.0.0.0/29"},
					preallocIps: map[string]string{
						"foo": "10.0.0.2",
						"bar": "10.0.0.4",
						"baz": "10.0.0.5",
						"gaz": "10.0.0.6",
					},
					expectResult: "10.0.0.3",
					lastIP:       "10.0.0.3",
				},
				// allocate from multiple subnets
				{
					subnets:      []string{"10.0.0.0/30", "10.0.1.0/30"},
					expectResult: "10.0.0.2",
					preallocIps:  map[string]string{},
				},
				// advance to next subnet
				{
					subnets:      []string{"10.0.0.0/30", "10.0.1.0/30"},
					lastIP:       "10.0.0.2",
					expectResult: "10.0.1.2",
					preallocIps:  map[string]string{},
				},
				// Roll to start subnet
				{
					subnets:      []string{"10.0.0.0/30", "10.0.1.0/30", "10.0.2.0/30"},
					lastIP:       "10.0.2.2",
					expectResult: "10.0.0.2",
					preallocIps:  map[string]string{},
				},
				// Already allocated
				{
					subnets:      []string{"10.0.2.0/30"},
					lastIP:       "",
					expectResult: "10.0.2.1",
					preallocIps: map[string]string{
						testID: "10.0.2.1",
					},
				},
				// lastIP out of range
				{
					subnets:      []string{"10.0.2.0/30"},
					lastIP:       "10.33.33.1",
					expectResult: "10.0.2.2",
					preallocIps: map[string]string{
						"foo": "10.0.2.1",
					},
				},
				// IP overflow
				{
					subnets: []string{"255.255.255.0/24"},
					lastIP:  "255.255.255.255",
					// skip GW ip
					expectResult: "255.255.255.2",
					preallocIps:  map[string]string{},
				},
			}

			for idx, tc := range testCases {
				res, err := tc.run(idx)
				Expect(err).ToNot(HaveOccurred())
				Expect(res.Address.IP.String()).To(Equal(tc.expectResult))
			}
		})

		It("should not allocate the broadcast address", func() {
			alloc := mkAlloc()
			for i := 2; i < 7; i++ {
				res, err := alloc.Allocate(fmt.Sprintf("ID%d", i), nil)
				Expect(err).ToNot(HaveOccurred())
				s := fmt.Sprintf("192.168.1.%d/29", i)
				Expect(s).To(Equal(res.Address.String()))
				_, _ = fmt.Fprintln(GinkgoWriter, "got ip", res.Address.String())
			}

			x, err := alloc.Allocate("ID8", nil)
			_, _ = fmt.Fprintln(GinkgoWriter, "got ip", x)
			Expect(err).To(HaveOccurred())
		})

		It("should allocate in a round-robin fashion", func() {
			alloc := mkAlloc()
			res, err := alloc.Allocate(testID, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Address.String()).To(Equal("192.168.1.2/29"))
			alloc.Deallocate(testID)

			res, err = alloc.Allocate(testID, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(res.Address.String()).To(Equal("192.168.1.3/29"))
		})

		It("should return same IP allocation for the a given ID if called twice", func() {
			alloc := mkAlloc()
			res, err := alloc.Allocate(testID, nil)
			Expect(err).ToNot(HaveOccurred())
			res2, err := alloc.Allocate(testID, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(res2).To(Equal(res))
		})
	})
	Context("when out of ips", func() {
		It("returns a meaningful error", func() {
			testCases := []AllocatorTestCase{
				{
					subnets: []string{"10.0.0.0/30"},
					preallocIps: map[string]string{
						"foo": "10.0.0.2",
					},
				},
				{
					subnets: []string{"10.0.0.0/29"},
					preallocIps: map[string]string{
						"foo": "10.0.0.2",
						"bar": "10.0.0.3",
						"baz": "10.0.0.4",
						"one": "10.0.0.5",
						"two": "10.0.0.6",
					},
				},
				{
					subnets: []string{"10.0.0.0/30", "10.0.1.0/30"},
					preallocIps: map[string]string{
						"foo": "10.0.0.2",
						"bar": "10.0.1.2",
					},
				},
			}
			for idx, tc := range testCases {
				_, err := tc.run(idx)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(HavePrefix("no free addresses in the allocated range"))
			}
		})
	})

	Context("when lastReservedIP is at the end of one of multi ranges", func() {
		It("should use the first IP of next range as startIP after Next", func() {
			a := newAllocatorWithMultiRanges()
			a.SetLastReservedIP(net.IP{192, 168, 1, 3})
			// check that IP from the next range is used
			checkAlloc(a, "0", net.IP{192, 168, 2, 0})
		})
	})

	Context("when no lastReservedIP", func() {
		It("should use the first IP of the first range as startIP after Next", func() {
			a := newAllocatorWithMultiRanges()
			// get range iterator and do the first Next
			checkAlloc(a, "0", net.IP{192, 168, 1, 0})
		})
	})
	Context("no gateway", func() {
		It("should use the first IP of the range", func() {
			p := ip.RangeSet{
				ip.Range{Subnet: mustSubnet("192.168.1.0/30")},
				ip.Range{Subnet: mustSubnet("192.168.1.4/30")},
			}
			Expect(p.Canonicalize()).NotTo(HaveOccurred())
			a := ip.NewIPAllocator(&p, nil)
			// get range iterator and do the first Next
			checkAlloc(a, "0", net.IP{192, 168, 1, 1})
			checkAlloc(a, "1", net.IP{192, 168, 1, 2})
			checkAlloc(a, "2", net.IP{192, 168, 1, 5})
		})
	})
	Context("point to point ranges", func() {
		It("should allocate two IPs", func() {
			p := ip.RangeSet{
				ip.Range{Subnet: mustSubnet("192.168.1.0/31")},
			}
			Expect(p.Canonicalize()).NotTo(HaveOccurred())
			a := ip.NewIPAllocator(&p, nil)
			// get range iterator and do the first Next
			checkAlloc(a, "0", net.IP{192, 168, 1, 0})
			checkAlloc(a, "1", net.IP{192, 168, 1, 1})
		})
	})
	Context("single ip ranges", func() {
		It("/32 network", func() {
			p := ip.RangeSet{
				ip.Range{Subnet: mustSubnet("192.168.1.10/32")},
			}
			Expect(p.Canonicalize()).NotTo(HaveOccurred())
			a := ip.NewIPAllocator(&p, nil)
			// get range iterator and do the first Next
			checkAlloc(a, "0", net.IP{192, 168, 1, 10})
			_, err := a.Allocate("1", nil)
			Expect(err).To(MatchError(ContainSubstring("no free addresses in the allocated range")))
		})
		It("/24 network", func() {
			p := ip.RangeSet{
				ip.Range{
					Subnet:     mustSubnet("192.168.1.0/24"),
					RangeStart: net.ParseIP("192.168.1.100"),
					RangeEnd:   net.ParseIP("192.168.1.100"),
				},
			}
			Expect(p.Canonicalize()).NotTo(HaveOccurred())
			a := ip.NewIPAllocator(&p, nil)
			// get range iterator and do the first Next
			checkAlloc(a, "0", net.IP{192, 168, 1, 100})
			_, err := a.Allocate("1", nil)
			Expect(err).To(MatchError(ContainSubstring("no free addresses in the allocated range")))
		})
	})
	Context("IP address exclusion", func() {
		It("should exclude IPs", func() {
			p := ip.RangeSet{
				ip.Range{Subnet: mustSubnet("192.168.0.0/29")},
			}
			Expect(p.Canonicalize()).NotTo(HaveOccurred())
			e := ip.RangeSet{
				ip.Range{Subnet: mustSubnet("192.168.0.0/29"),
					RangeStart: net.ParseIP("192.168.0.2"), RangeEnd: net.ParseIP("192.168.0.3")},
				ip.Range{Subnet: mustSubnet("192.168.0.0/29"),
					RangeStart: net.ParseIP("192.168.0.5"), RangeEnd: net.ParseIP("192.168.0.5")},
			}
			Expect(e.Canonicalize()).NotTo(HaveOccurred())
			a := ip.NewIPAllocator(&p, &e)
			// get range iterator and do the first Next
			checkAlloc(a, "0", net.IP{192, 168, 0, 1})
			checkAlloc(a, "1", net.IP{192, 168, 0, 4})
			checkAlloc(a, "2", net.IP{192, 168, 0, 6})
		})
	})
	Context("Static IP allocation", func() {
		It("should allocate IP", func() {
			p := ip.RangeSet{
				ip.Range{Subnet: mustSubnet("192.168.0.0/24"), Gateway: net.ParseIP("192.168.0.10")},
			}
			// should ignore exclusions while allocating static IPs
			e := ip.RangeSet{
				ip.Range{Subnet: mustSubnet("192.168.0.0/24"),
					RangeStart: net.ParseIP("192.168.0.33"), RangeEnd: net.ParseIP("192.168.0.33")},
			}
			staticIP := net.ParseIP("192.168.0.33")
			Expect(p.Canonicalize()).NotTo(HaveOccurred())
			Expect(e.Canonicalize()).NotTo(HaveOccurred())
			a := ip.NewIPAllocator(&p, &e)
			alloc, err := a.Allocate("0", staticIP)
			Expect(err).NotTo(HaveOccurred())
			Expect(alloc.Address.IP).To(Equal(staticIP))
			Expect(alloc.Address.Mask).To(Equal(p[0].Subnet.Mask))
			Expect(alloc.Gateway).To(Equal(p[0].Gateway))
			// second allocation for the same ID with the same IP should succeed
			alloc, err = a.Allocate("0", staticIP)
			Expect(err).NotTo(HaveOccurred())
			Expect(alloc.Address.IP).To(Equal(staticIP))
			Expect(alloc.Address.Mask).To(Equal(p[0].Subnet.Mask))
			Expect(alloc.Gateway).To(Equal(p[0].Gateway))
		})
		It("should fail if static IP is not in the allocator's subnet", func() {
			p := ip.RangeSet{
				ip.Range{Subnet: mustSubnet("192.168.0.0/24"), Gateway: net.ParseIP("192.168.0.10")},
			}
			Expect(p.Canonicalize()).NotTo(HaveOccurred())
			staticIP := net.ParseIP("10.10.10.10")
			a := ip.NewIPAllocator(&p, nil)
			_, err := a.Allocate("0", staticIP)
			Expect(err).To(MatchError(ContainSubstring("can't find IP range")))
		})
		It("should fail if static IP is already allocated to someone else", func() {
			p := ip.RangeSet{
				ip.Range{Subnet: mustSubnet("192.168.0.0/24"), Gateway: net.ParseIP("192.168.0.10")},
			}
			Expect(p.Canonicalize()).NotTo(HaveOccurred())
			staticIP := net.ParseIP("192.168.0.44")
			a := ip.NewIPAllocator(&p, nil)
			_, err := a.Allocate("0", staticIP)
			Expect(err).NotTo(HaveOccurred())

			_, err = a.Allocate("1", staticIP)
			Expect(err).To(MatchError(ContainSubstring("is already allocated for different ID")))
		})
		It("should fail if a different static IP is already allocated for the same ID", func() {
			p := ip.RangeSet{
				ip.Range{Subnet: mustSubnet("192.168.0.0/24"), Gateway: net.ParseIP("192.168.0.10")},
			}
			Expect(p.Canonicalize()).NotTo(HaveOccurred())
			staticIP := net.ParseIP("192.168.0.44")
			a := ip.NewIPAllocator(&p, nil)
			_, err := a.Allocate("0", staticIP)
			Expect(err).NotTo(HaveOccurred())

			staticIP2 := net.ParseIP("192.168.0.55")
			_, err = a.Allocate("0", staticIP2)
			Expect(err).To(MatchError(ContainSubstring("is already allocated for ID")))
		})
	})
	Context("get allocation", func() {
		It("when exists", func() {
			ipalloc := mkAlloc()
			ipa, err := ipalloc.Allocate("foo", nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(ipalloc.GetAllocation("foo")).To(Equal(ipa))
		})
		It("when does not exists", func() {
			ipalloc := mkAlloc()
			Expect(ipalloc.GetAllocation("bar")).To(BeNil())
		})
	})
	Context("allocate gateway", func() {
		var alloc ip.IPAllocator
		BeforeEach(func() {
			alloc = mkAlloc()
		})
		It("should allocate gateway IP", func() {
			ipa, err := alloc.AllocateGateway("0")
			Expect(err).NotTo(HaveOccurred())
			Expect(ipa.Address.IP).To(Equal(net.IP{192, 168, 1, 1}))
		})

		It("should return gateway IP if already allocated to same id", func() {
			_, err := alloc.AllocateGateway("0")
			Expect(err).NotTo(HaveOccurred())
			ipa, err := alloc.AllocateGateway("0")
			Expect(err).NotTo(HaveOccurred())
			Expect(ipa.Address.IP).To(Equal(net.IP{192, 168, 1, 1}))
		})

		It("should fail if gateway IP if already allocated to different id", func() {
			_, err := alloc.AllocateGateway("0")
			Expect(err).NotTo(HaveOccurred())
			_, err = alloc.AllocateGateway("1")
			Expect(err).To(HaveOccurred())
		})

		It("should fail if no gateway is availale in rangeset", func() {
			p := ip.RangeSet{
				ip.Range{Subnet: mustSubnet("192.168.1.0/29"), Gateway: nil},
			}
			Expect(p.Canonicalize()).NotTo(HaveOccurred())
			alloc := ip.NewIPAllocator(&p, nil)
			_, err := alloc.AllocateGateway("0")
			Expect(err).To(HaveOccurred())
		})
	})
	Context("ListAllocationIDs", func() {
		var alloc ip.IPAllocator
		BeforeEach(func() {
			alloc = mkAlloc()
		})

		It("should return an empty list if no allocations", func() {
			ids := alloc.ListAllocationIDs()
			Expect(ids).To(BeEmpty())
		})
		It("should return all allocation IDs", func() {
			_, _ = alloc.Allocate("foo", nil)
			_, _ = alloc.Allocate("bar", nil)
			_, _ = alloc.Allocate("baz", nil)
			ids := alloc.ListAllocationIDs()
			Expect(ids).To(ConsistOf("foo", "bar", "baz"))

			alloc.Deallocate("bar")
			ids = alloc.ListAllocationIDs()
			Expect(ids).To(ConsistOf("foo", "baz"))

			alloc.Deallocate("foo")
			alloc.Deallocate("baz")
			ids = alloc.ListAllocationIDs()
			Expect(ids).To(BeEmpty())
		})
	})
})
