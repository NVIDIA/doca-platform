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
var _ = Describe("PolicySet", func() {
	var (
		ps      sets.PolicySet
		policy1 *nbdb.LogicalRouterPolicy
		policy2 *nbdb.LogicalRouterPolicy
		policy3 *nbdb.LogicalRouterPolicy
	)

	createTestPolicy := func(priority int, match, action string, nexthop *string, nexthops []string, externalIDs map[string]string) *nbdb.LogicalRouterPolicy {
		return &nbdb.LogicalRouterPolicy{
			Priority:    priority,
			Match:       match,
			Action:      action,
			Nexthop:     nexthop,
			Nexthops:    nexthops,
			ExternalIDs: externalIDs,
		}
	}

	BeforeEach(func() {
		ps = sets.NewPolicySet()
		nexthop1 := "192.168.0.1"
		nexthop2 := "192.168.1.1"
		nexthop3 := "192.168.2.1"
		policy1 = createTestPolicy(100, "ip4.dst == 192.168.0.0/24", "reroute", &nexthop1, nil, nil)
		policy2 = createTestPolicy(200, "ip4.dst == 192.168.1.0/24", "reroute", &nexthop2, nil, nil)
		policy3 = createTestPolicy(300, "ip4.dst == 192.168.2.0/24", "reroute", &nexthop3, nil, nil)
	})

	Context("NewPolicySet", func() {
		It("should create a new empty PolicySet", func() {
			Expect(ps).NotTo(BeNil())
			Expect(ps.Len()).To(Equal(0))
		})
	})

	Context("Add", func() {
		It("should add a single policy", func() {
			ps.Add(policy1)
			Expect(ps.Len()).To(Equal(1))
			Expect(ps.Has(policy1)).To(BeTrue())
		})

		It("should add multiple policies", func() {
			ps.Add(policy1, policy2, policy3)
			Expect(ps.Len()).To(Equal(3))
			Expect(ps.Has(policy1)).To(BeTrue())
			Expect(ps.Has(policy2)).To(BeTrue())
			Expect(ps.Has(policy3)).To(BeTrue())
		})

		It("should ignore nil policies", func() {
			ps.Add(nil)
			Expect(ps.Len()).To(Equal(0))
		})

		It("should ignore duplicate policies", func() {
			ps.Add(policy1)
			ps.Add(policy1)
			Expect(ps.Len()).To(Equal(1))
		})

		It("should add cookie to policy", func() {
			ps.Add(policy1)
			Expect(ps.List()[0].ExternalIDs).To(HaveKey(sets.VPCCookieKey))
		})

		It("should preserve existing external IDs", func() {
			policy1.ExternalIDs = map[string]string{"foo": "bar"}
			ps.Add(policy1)
			Expect(ps.List()[0].ExternalIDs).To(HaveKey(sets.VPCCookieKey))
			Expect(ps.List()[0].ExternalIDs).To(HaveKey("foo"))
			Expect(ps.List()[0].ExternalIDs["foo"]).To(Equal("bar"))
		})
	})

	Context("Remove", func() {
		BeforeEach(func() {
			ps.Add(policy1, policy2, policy3)
		})

		It("should remove a single policy", func() {
			ps.Remove(policy1)
			Expect(ps.Len()).To(Equal(2))
			Expect(ps.Has(policy1)).To(BeFalse())
			Expect(ps.Has(policy2)).To(BeTrue())
			Expect(ps.Has(policy3)).To(BeTrue())
		})

		It("should remove multiple policies", func() {
			ps.Remove(policy1, policy3)
			Expect(ps.Len()).To(Equal(1))
			Expect(ps.Has(policy1)).To(BeFalse())
			Expect(ps.Has(policy2)).To(BeTrue())
			Expect(ps.Has(policy3)).To(BeFalse())
		})

		It("should ignore non-existent policies", func() {
			nexthop := "10.0.0.1"
			nonExistentPolicy := createTestPolicy(400, "ip4.dst == 10.0.0.0/8", "reroute", &nexthop, nil, nil)
			ps.Remove(nonExistentPolicy)
			Expect(ps.Len()).To(Equal(3))
		})
	})

	Context("Has", func() {
		BeforeEach(func() {
			ps.Add(policy1)
			policy2.ExternalIDs = map[string]string{"foo": "bar"}
			ps.Add(policy2)
		})

		It("should return true for existing policies", func() {
			Expect(ps.Has(policy1)).To(BeTrue())
		})

		It("should return false for non-existent policies", func() {
			Expect(ps.Has(policy3)).To(BeFalse())
		})

		It("should return true for nil policies", func() {
			Expect(ps.Has(nil)).To(BeTrue())
		})

		It("should identify policies with the same values", func() {
			nexthop := "192.168.0.1"
			policySameAsPolicy1 := createTestPolicy(100, "ip4.dst == 192.168.0.0/24", "reroute", &nexthop, nil, nil)
			Expect(ps.Has(policySameAsPolicy1)).To(BeTrue())
		})

		It("should identify policies with the same values with nexthops", func() {
			nexthop := "192.168.0.1"
			policyEquvalentToPolicy1 := createTestPolicy(100, "ip4.dst == 192.168.0.0/24", "reroute", nil, []string{nexthop}, nil)
			Expect(ps.Has(policyEquvalentToPolicy1)).To(BeTrue())
		})

		It("should return false if policy has different external ids", func() {
			nexthop := "192.168.0.1"
			policySameAsPolicy1WithExtIDs := createTestPolicy(100, "ip4.dst == 192.168.0.0/24", "reroute", &nexthop, nil, map[string]string{"foo": "bar"})
			Expect(ps.Has(policySameAsPolicy1WithExtIDs)).To(BeFalse())

			policySameAsPolicy2WithExtIDs := createTestPolicy(100, "ip4.dst == 192.168.1.0/24", "reroute", &nexthop, nil, map[string]string{"foo": "baz"})
			Expect(ps.Has(policySameAsPolicy2WithExtIDs)).To(BeFalse())
		})

		It("should return true if policy has same external ids", func() {
			nexthop := "192.168.1.1"
			policy2Copy := createTestPolicy(200, "ip4.dst == 192.168.1.0/24", "reroute", &nexthop, nil, map[string]string{"foo": "bar"})
			Expect(ps.Has(policy2Copy)).To(BeTrue())
		})
	})

	Context("Len", func() {
		It("should return 0 for empty set", func() {
			Expect(ps.Len()).To(Equal(0))
		})

		It("should return correct count after adding policies", func() {
			ps.Add(policy1, policy2)
			Expect(ps.Len()).To(Equal(2))
		})

		It("should return correct count after removing policies", func() {
			ps.Add(policy1, policy2)
			ps.Remove(policy1)
			Expect(ps.Len()).To(Equal(1))
		})
	})

	Context("In", func() {
		var ps2 sets.PolicySet

		BeforeEach(func() {
			ps2 = sets.NewPolicySet()
		})

		It("should return true when both sets are empty", func() {
			Expect(ps.In(ps2)).To(BeTrue())
		})

		It("should return true when one set is a subset of another", func() {
			ps.Add(policy1)
			ps2.Add(policy1, policy2)
			Expect(ps.In(ps2)).To(BeTrue())
		})

		It("should return false when one set is not a subset of another", func() {
			ps.Add(policy1, policy2)
			ps2.Add(policy1)
			Expect(ps.In(ps2)).To(BeFalse())
		})

		It("should return false when sets are disjoint", func() {
			ps.Add(policy1)
			ps2.Add(policy2)
			Expect(ps.In(ps2)).To(BeFalse())
		})
	})

	Context("Intersect", func() {
		var ps2 sets.PolicySet

		BeforeEach(func() {
			ps2 = sets.NewPolicySet()
		})

		It("should return empty set when both sets are empty", func() {
			intersection := ps.Intersect(ps2)
			Expect(intersection.Len()).To(Equal(0))
		})

		It("should return set with common elements", func() {
			ps.Add(policy1, policy2)
			ps2.Add(policy1, policy3)
			intersection := ps.Intersect(ps2)
			Expect(intersection.Len()).To(Equal(1))
			Expect(intersection.Has(policy1)).To(BeTrue())
		})

		It("should return empty set when sets are disjoint", func() {
			ps.Add(policy1)
			ps2.Add(policy2)
			intersection := ps.Intersect(ps2)
			Expect(intersection.Len()).To(Equal(0))
		})
	})

	Context("Difference", func() {
		var ps2 sets.PolicySet

		BeforeEach(func() {
			ps2 = sets.NewPolicySet()
		})

		It("should return empty set when both sets are empty", func() {
			difference := ps.Difference(ps2)
			Expect(difference.Len()).To(Equal(0))
		})

		It("should return set with elements only in first set", func() {
			ps.Add(policy1, policy2)
			ps2.Add(policy1, policy3)
			difference := ps.Difference(ps2)
			Expect(difference.Len()).To(Equal(1))
			Expect(difference.Has(policy2)).To(BeTrue())
		})

		It("should return all elements when sets are disjoint", func() {
			ps.Add(policy1, policy2)
			ps2.Add(policy3)
			difference := ps.Difference(ps2)
			Expect(difference.Len()).To(Equal(2))
			Expect(difference.Has(policy1)).To(BeTrue())
			Expect(difference.Has(policy2)).To(BeTrue())
		})
	})

	Context("Equals", func() {
		var ps2 sets.PolicySet

		BeforeEach(func() {
			ps2 = sets.NewPolicySet()
		})

		It("should return true when both sets are empty", func() {
			Expect(ps.Equals(ps2)).To(BeTrue())
		})

		It("should return true when sets have the same elements", func() {
			ps.Add(policy1, policy2)
			ps2.Add(policy1, policy2)
			Expect(ps.Equals(ps2)).To(BeTrue())
		})

		It("should return false when sets have different elements", func() {
			ps.Add(policy1, policy2)
			ps2.Add(policy1, policy3)
			Expect(ps.Equals(ps2)).To(BeFalse())
		})

		It("should return false when sets have different sizes", func() {
			ps.Add(policy1, policy2)
			ps2.Add(policy1)
			Expect(ps.Equals(ps2)).To(BeFalse())
		})
	})

	Context("List", func() {
		It("should return empty list for empty set", func() {
			list := ps.List()
			Expect(list).To(BeEmpty())
		})

		It("should return all elements in the set", func() {
			ps.Add(policy1, policy2)
			list := ps.List()
			Expect(list).To(HaveLen(2))

			// Check that all policies are in the list
			found1, found2 := false, false
			for _, p := range list {
				if p.Match == "ip4.dst == 192.168.0.0/24" && p.Priority == 100 {
					found1 = true
				}
				if p.Match == "ip4.dst == 192.168.1.0/24" && p.Priority == 200 {
					found2 = true
				}
			}
			Expect(found1).To(BeTrue())
			Expect(found2).To(BeTrue())
		})
	})

	Context("Union", func() {
		var ps2 sets.PolicySet

		BeforeEach(func() {
			ps2 = sets.NewPolicySet()
		})

		It("should return empty set when both sets are empty", func() {
			union := ps.Union(ps2)
			Expect(union.Len()).To(Equal(0))
		})

		It("should return set with all elements when sets have no overlap", func() {
			ps.Add(policy1)
			ps2.Add(policy2)
			union := ps.Union(ps2)
			Expect(union.Len()).To(Equal(2))
			Expect(union.Has(policy1)).To(BeTrue())
			Expect(union.Has(policy2)).To(BeTrue())
		})

		It("should return set with unique elements when sets have overlap", func() {
			ps.Add(policy1, policy2)
			ps2.Add(policy2, policy3)
			union := ps.Union(ps2)
			Expect(union.Len()).To(Equal(3))
			Expect(union.Has(policy1)).To(BeTrue())
			Expect(union.Has(policy2)).To(BeTrue())
			Expect(union.Has(policy3)).To(BeTrue())
		})

		It("should return first set when second set is empty", func() {
			ps.Add(policy1, policy2)
			union := ps.Union(ps2)
			Expect(union.Len()).To(Equal(2))
			Expect(union.Has(policy1)).To(BeTrue())
			Expect(union.Has(policy2)).To(BeTrue())
		})

		It("should return second set when first set is empty", func() {
			ps2.Add(policy1, policy2)
			union := ps.Union(ps2)
			Expect(union.Len()).To(Equal(2))
			Expect(union.Has(policy1)).To(BeTrue())
			Expect(union.Has(policy2)).To(BeTrue())
		})
	})

	Context("SymmetricDifference", func() {
		var ps2 sets.PolicySet

		BeforeEach(func() {
			ps2 = sets.NewPolicySet()
		})

		It("should return empty set when both sets are empty", func() {
			symDiff := ps.SymmetricDifference(ps2)
			Expect(symDiff.Len()).To(Equal(0))
		})

		It("should return all elements when sets have no overlap", func() {
			ps.Add(policy1)
			ps2.Add(policy2)
			symDiff := ps.SymmetricDifference(ps2)
			Expect(symDiff.Len()).To(Equal(2))
			Expect(symDiff.Has(policy1)).To(BeTrue())
			Expect(symDiff.Has(policy2)).To(BeTrue())
		})

		It("should return elements that are in only one set when sets have overlap", func() {
			ps.Add(policy1, policy2)
			ps2.Add(policy2, policy3)
			symDiff := ps.SymmetricDifference(ps2)
			Expect(symDiff.Len()).To(Equal(2))
			Expect(symDiff.Has(policy1)).To(BeTrue())
			Expect(symDiff.Has(policy2)).To(BeFalse())
			Expect(symDiff.Has(policy3)).To(BeTrue())

			symDiff2 := ps2.SymmetricDifference(ps)
			Expect(symDiff2.Equals(symDiff)).To(BeTrue())
		})

		It("should return first set when second set is empty", func() {
			ps.Add(policy1, policy2)
			symDiff := ps.SymmetricDifference(ps2)
			Expect(symDiff.Len()).To(Equal(2))
			Expect(symDiff.Has(policy1)).To(BeTrue())
			Expect(symDiff.Has(policy2)).To(BeTrue())
		})

		It("should return second set when first set is empty", func() {
			ps2.Add(policy1, policy2)
			symDiff := ps.SymmetricDifference(ps2)
			Expect(symDiff.Len()).To(Equal(2))
			Expect(symDiff.Has(policy1)).To(BeTrue())
			Expect(symDiff.Has(policy2)).To(BeTrue())
		})

		It("should return empty set when sets are identical", func() {
			ps.Add(policy1, policy2)
			ps2.Add(policy1, policy2)
			symDiff := ps.SymmetricDifference(ps2)
			Expect(symDiff.Len()).To(Equal(0))
		})
	})

	Context("String", func() {
		It("should return string representation of the set", func() {
			ps.Add(policy1, policy2)
			for _, p := range []*nbdb.LogicalRouterPolicy{policy1, policy2} {
				Expect(ps.String()).To(ContainSubstring(p.Match))
				Expect(ps.String()).To(ContainSubstring(p.Action))
				Expect(ps.String()).To(ContainSubstring(*p.Nexthop))
			}
		})
	})
})
