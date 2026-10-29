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

package sets_test

import (
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/nbdb"
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/topology/sets"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

//nolint:goconst
var _ = Describe("RouteSet", func() {
	var (
		rs     sets.RouteSet
		route1 *nbdb.LogicalRouterStaticRoute
		route2 *nbdb.LogicalRouterStaticRoute
		route3 *nbdb.LogicalRouterStaticRoute
	)

	createTestRoute := func(ipPrefix, nexthop string) *nbdb.LogicalRouterStaticRoute {
		return &nbdb.LogicalRouterStaticRoute{
			IPPrefix:    ipPrefix,
			Nexthop:     nexthop,
			ExternalIDs: nil,
		}
	}

	BeforeEach(func() {
		rs = sets.NewRouteSet()
		route1 = createTestRoute("192.168.1.0/24", "192.168.1.1")
		route2 = createTestRoute("192.168.2.0/24", "192.168.2.1")
		route3 = createTestRoute("192.168.3.0/24", "192.168.3.1")
	})

	Context("NewRouteSet", func() {
		It("should create a new empty RouteSet", func() {
			Expect(rs).NotTo(BeNil())
			Expect(rs.Len()).To(Equal(0))
		})
	})

	Context("Add", func() {
		It("should add a single route", func() {
			rs.Add(route1)
			Expect(rs.Len()).To(Equal(1))
			Expect(rs.Has(route1)).To(BeTrue())
		})

		It("should add multiple routes", func() {
			rs.Add(route1, route2, route3)
			Expect(rs.Len()).To(Equal(3))
			Expect(rs.Has(route1)).To(BeTrue())
			Expect(rs.Has(route2)).To(BeTrue())
			Expect(rs.Has(route3)).To(BeTrue())
		})

		It("should ignore nil routes", func() {
			rs.Add(nil, nil)
			Expect(rs.Len()).To(Equal(0))
		})

		It("should ignore no routes", func() {
			rs.Add()
			Expect(rs.Len()).To(Equal(0))
		})

		It("should ignore duplicate routes", func() {
			rs.Add(route1)
			rs.Add(route1)
			// clear external ids and add again
			route1.ExternalIDs = nil
			rs.Add(route1)
			Expect(rs.Len()).To(Equal(1))
		})

		It("should preserve external IDs when adding route to set", func() {
			route1.ExternalIDs = map[string]string{"foo": "bar"}
			rs.Add(route1)
			Expect(rs.List()[0].ExternalIDs).To(HaveKey("foo"))
			Expect(rs.List()[0].ExternalIDs["foo"]).To(Equal("bar"))
		})

		It("should add a cookie in external IDs when adding route to set", func() {
			rs.Add(route1)
			Expect(rs.List()[0].ExternalIDs).To(HaveKey(sets.VPCCookieKey))
			Expect(rs.List()[0].ExternalIDs[sets.VPCCookieKey]).ToNot(BeEmpty())
		})
	})

	Context("Remove", func() {
		BeforeEach(func() {
			rs.Add(route1, route2, route3)
		})

		It("should remove a single route", func() {
			rs.Remove(route1)
			Expect(rs.Len()).To(Equal(2))
			Expect(rs.Has(route1)).To(BeFalse())
			Expect(rs.Has(route2)).To(BeTrue())
			Expect(rs.Has(route3)).To(BeTrue())
		})

		It("should remove multiple routes", func() {
			rs.Remove(route1, route3)
			Expect(rs.Len()).To(Equal(1))
			Expect(rs.Has(route1)).To(BeFalse())
			Expect(rs.Has(route2)).To(BeTrue())
			Expect(rs.Has(route3)).To(BeFalse())
		})

		It("should ignore non-existent routes", func() {
			nonExistentRoute := createTestRoute("10.0.0.0/8", "10.0.0.1")
			rs.Remove(nonExistentRoute)
			Expect(rs.Len()).To(Equal(3))
		})
	})

	Context("Has", func() {
		BeforeEach(func() {
			rs.Add(route1)
		})

		It("should return true for existing routes", func() {
			Expect(rs.Has(route1)).To(BeTrue())
		})

		It("should return false for non-existent routes", func() {
			Expect(rs.Has(route2)).To(BeFalse())
		})

		It("should return true for nil routes", func() {
			Expect(rs.Has(nil)).To(BeTrue())
		})

		It("should identify routes with the same values", func() {
			routeSameAsRoute1 := createTestRoute("192.168.1.0/24", "192.168.1.1")
			Expect(rs.Has(routeSameAsRoute1)).To(BeTrue())
		})
	})

	Context("Len", func() {
		It("should return 0 for empty set", func() {
			Expect(rs.Len()).To(Equal(0))
		})

		It("should return correct count after adding routes", func() {
			rs.Add(route1, route2)
			Expect(rs.Len()).To(Equal(2))
		})

		It("should return correct count after removing routes", func() {
			rs.Add(route1, route2)
			rs.Remove(route1)
			Expect(rs.Len()).To(Equal(1))
		})
	})

	Context("In", func() {
		var rs2 sets.RouteSet

		BeforeEach(func() {
			rs2 = sets.NewRouteSet()
		})

		It("should return true when both sets are empty", func() {
			Expect(rs.In(rs2)).To(BeTrue())
		})

		It("should return true when one set is a subset of another", func() {
			rs.Add(route1)
			rs2.Add(route1, route2)
			Expect(rs.In(rs2)).To(BeTrue())
		})

		It("should return false when one set is not a subset of another", func() {
			rs.Add(route1, route2)
			rs2.Add(route1)
			Expect(rs.In(rs2)).To(BeFalse())
		})

		It("should return false when sets are disjoint", func() {
			rs.Add(route1)
			rs2.Add(route2)
			Expect(rs.In(rs2)).To(BeFalse())
		})
	})

	Context("Intersect", func() {
		var rs2 sets.RouteSet

		BeforeEach(func() {
			rs2 = sets.NewRouteSet()
		})

		It("should return empty set when both sets are empty", func() {
			intersection := rs.Intersect(rs2)
			Expect(intersection.Len()).To(Equal(0))
		})

		It("should return set with common elements", func() {
			rs.Add(route1, route2)
			rs2.Add(route1, route3)
			intersection := rs.Intersect(rs2)
			Expect(intersection.Len()).To(Equal(1))
			Expect(intersection.Has(route1)).To(BeTrue())
		})

		It("should return empty set when sets are disjoint", func() {
			rs.Add(route1)
			rs2.Add(route2)
			intersection := rs.Intersect(rs2)
			Expect(intersection.Len()).To(Equal(0))
		})
	})

	Context("Difference", func() {
		var rs2 sets.RouteSet

		BeforeEach(func() {
			rs2 = sets.NewRouteSet()
		})

		It("should return empty set when both sets are empty", func() {
			difference := rs.Difference(rs2)
			Expect(difference.Len()).To(Equal(0))
		})

		It("should return set with elements only in first set", func() {
			rs.Add(route1, route2)
			rs2.Add(route1, route3)
			difference := rs.Difference(rs2)
			Expect(difference.Len()).To(Equal(1))
			Expect(difference.Has(route2)).To(BeTrue())
		})

		It("should return all elements when sets are disjoint", func() {
			rs.Add(route1, route2)
			rs2.Add(route3)
			difference := rs.Difference(rs2)
			Expect(difference.Len()).To(Equal(2))
			Expect(difference.Has(route1)).To(BeTrue())
			Expect(difference.Has(route2)).To(BeTrue())
		})
	})

	Context("Equals", func() {
		var rs2 sets.RouteSet

		BeforeEach(func() {
			rs2 = sets.NewRouteSet()
		})

		It("should return true when both sets are empty", func() {
			Expect(rs.Equals(rs2)).To(BeTrue())
		})

		It("should return true when sets have the same elements", func() {
			rs.Add(route1, route2)
			rs2.Add(route1, route2)
			Expect(rs.Equals(rs2)).To(BeTrue())
		})

		It("should return false when sets have different elements", func() {
			rs.Add(route1, route2)
			rs2.Add(route1, route3)
			Expect(rs.Equals(rs2)).To(BeFalse())
		})

		It("should return false when sets have different sizes", func() {
			rs.Add(route1, route2)
			rs2.Add(route1)
			Expect(rs.Equals(rs2)).To(BeFalse())
		})
	})

	Context("List", func() {
		It("should return empty list for empty set", func() {
			list := rs.List()
			Expect(list).To(BeEmpty())
		})

		It("should return all elements in the set", func() {
			rs.Add(route1, route2)
			list := rs.List()
			Expect(list).To(HaveLen(2))

			// Check that all routes are in the list
			found1, found2 := false, false
			for _, r := range list {
				if r.IPPrefix == "192.168.1.0/24" && r.Nexthop == "192.168.1.1" {
					found1 = true
				}
				if r.IPPrefix == "192.168.2.0/24" && r.Nexthop == "192.168.2.1" {
					found2 = true
				}
			}
			Expect(found1).To(BeTrue())
			Expect(found2).To(BeTrue())
		})
	})

	Context("Union", func() {
		var rs2 sets.RouteSet

		BeforeEach(func() {
			rs2 = sets.NewRouteSet()
		})

		It("should return empty set when both sets are empty", func() {
			union := rs.Union(rs2)
			Expect(union.Len()).To(Equal(0))
		})

		It("should return set with all elements when one set is empty", func() {
			rs.Add(route1, route2)
			union := rs.Union(rs2)
			Expect(union.Len()).To(Equal(2))
			Expect(union.Has(route1)).To(BeTrue())
			Expect(union.Has(route2)).To(BeTrue())
		})

		It("should return set with all unique elements from both sets", func() {
			rs.Add(route1, route2)
			rs2.Add(route2, route3)
			union := rs.Union(rs2)
			Expect(union.Len()).To(Equal(3))
			Expect(union.Has(route1)).To(BeTrue())
			Expect(union.Has(route2)).To(BeTrue())
			Expect(union.Has(route3)).To(BeTrue())
		})

		It("should handle duplicate elements correctly", func() {
			rs.Add(route1, route2)
			rs2.Add(route1, route2)
			union := rs.Union(rs2)
			Expect(union.Len()).To(Equal(2))
			Expect(union.Has(route1)).To(BeTrue())
			Expect(union.Has(route2)).To(BeTrue())
		})
	})

	Context("SymmetricDifference", func() {
		var rs2 sets.RouteSet

		BeforeEach(func() {
			rs2 = sets.NewRouteSet()
		})

		It("should return empty set when both sets are empty", func() {
			symDiff := rs.SymmetricDifference(rs2)
			Expect(symDiff.Len()).To(Equal(0))
		})

		It("should return all elements of non-empty set when other set is empty", func() {
			rs.Add(route1, route2)
			symDiff := rs.SymmetricDifference(rs2)
			Expect(symDiff.Len()).To(Equal(2))
			Expect(symDiff.Has(route1)).To(BeTrue())
			Expect(symDiff.Has(route2)).To(BeTrue())
		})

		It("should return elements that are in either set but not in both", func() {
			rs.Add(route1, route2)
			rs2.Add(route2, route3)
			symDiff := rs.SymmetricDifference(rs2)
			Expect(symDiff.Len()).To(Equal(2))
			Expect(symDiff.Has(route1)).To(BeTrue())
			Expect(symDiff.Has(route2)).To(BeFalse()) // In both sets, so not in symmetric difference
			Expect(symDiff.Has(route3)).To(BeTrue())

			symDiff2 := rs2.SymmetricDifference(rs)
			Expect(symDiff2.Equals(symDiff)).To(BeTrue())
		})

		It("should return all elements when sets are disjoint", func() {
			rs.Add(route1)
			rs2.Add(route2, route3)
			symDiff := rs.SymmetricDifference(rs2)
			Expect(symDiff.Len()).To(Equal(3))
			Expect(symDiff.Has(route1)).To(BeTrue())
			Expect(symDiff.Has(route2)).To(BeTrue())
			Expect(symDiff.Has(route3)).To(BeTrue())
		})

		It("should return empty set when sets are identical", func() {
			rs.Add(route1, route2)
			rs2.Add(route1, route2)
			symDiff := rs.SymmetricDifference(rs2)
			Expect(symDiff.Len()).To(Equal(0))
		})
	})

	Context("String", func() {
		It("should return string representation of the set", func() {
			rs.Add(route1, route2)
			for _, r := range []*nbdb.LogicalRouterStaticRoute{route1, route2} {
				Expect(rs.String()).To(ContainSubstring(r.IPPrefix))
				Expect(rs.String()).To(ContainSubstring(r.Nexthop))
			}
		})
	})
})
