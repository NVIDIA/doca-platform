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

var _ = Describe("NATSet", func() {
	var (
		natSet sets.NATSet
		nat1   *nbdb.NAT
		nat2   *nbdb.NAT
		nat3   *nbdb.NAT
	)

	createTestNAT := func(externalIP, logicalIP, typ string, externalIDs map[string]string) *nbdb.NAT {
		return &nbdb.NAT{
			ExternalIP:  externalIP,
			LogicalIP:   logicalIP,
			Type:        typ,
			ExternalIDs: externalIDs,
		}
	}

	BeforeEach(func() {
		natSet = sets.NewNATSet()
		nat1 = createTestNAT("10.0.0.1", "192.168.0.1", "snat", nil)
		nat2 = createTestNAT("10.0.0.1", "192.168.0.1", "dnat", nil)
		nat3 = createTestNAT("10.0.0.3", "192.168.0.3", "snat", nil)
	})

	Context("Add", func() {
		It("should add NAT elements to the set", func() {
			natSet.Add(nat1, nat2)
			Expect(natSet.Len()).To(Equal(2))
			Expect(natSet.Has(nat1)).To(BeTrue())
			Expect(natSet.Has(nat2)).To(BeTrue())
		})

		It("should ignore nil NAT elements", func() {
			natSet.Add(nil, nat1)
			Expect(natSet.Len()).To(Equal(1))
			Expect(natSet.Has(nat1)).To(BeTrue())
		})

		It("should ignore duplicate NAT elements", func() {
			nat1Dup := createTestNAT("10.0.0.1", "192.168.0.1", "snat", nil)
			natSet.Add(nat1)
			natSet.Add(nat1Dup)
			Expect(natSet.Len()).To(Equal(1))
			Expect(natSet.Has(nat1)).To(BeTrue())
		})

		It("should add cookie to NAT element", func() {
			natSet.Add(nat1)
			Expect(natSet.List()[0].ExternalIDs).To(HaveKey(sets.VPCCookieKey))
		})

		It("should preserve existing external IDs", func() {
			nat1.ExternalIDs = map[string]string{"foo": "bar"}
			natSet.Add(nat1)
			Expect(natSet.List()[0].ExternalIDs).To(HaveKey(sets.VPCCookieKey))
			Expect(natSet.List()[0].ExternalIDs).To(HaveKey("foo"))
			Expect(natSet.List()[0].ExternalIDs["foo"]).To(Equal("bar"))
		})
	})

	Context("Remove", func() {
		BeforeEach(func() {
			natSet.Add(nat1, nat2)
		})

		It("should remove NAT elements from the set", func() {
			natSet.Remove(nat1)
			Expect(natSet.Len()).To(Equal(1))
			Expect(natSet.Has(nat1)).To(BeFalse())
			Expect(natSet.Has(nat2)).To(BeTrue())
		})

		It("should ignore non-existent NAT elements", func() {
			natSet.Remove(nat3)
			Expect(natSet.Len()).To(Equal(2))
		})
	})

	Context("Has", func() {
		BeforeEach(func() {
			natSet.Add(nat1)
		})

		It("should return true for existing NAT element", func() {
			nat1Dup := createTestNAT("10.0.0.1", "192.168.0.1", "snat", nil)
			Expect(natSet.Has(nat1)).To(BeTrue())
			Expect(natSet.Has(nat1Dup)).To(BeTrue())
		})

		It("should return false for non-existent NAT element", func() {
			Expect(natSet.Has(nat2)).To(BeFalse())
		})

		It("should return true for nil NAT element", func() {
			Expect(natSet.Has(nil)).To(BeTrue())
		})

		It("should return false for same NAT element with different external IDs", func() {
			nat1Dup := createTestNAT("10.0.0.1", "192.168.0.1", "snat", map[string]string{"foo": "bar"})
			Expect(natSet.Has(nat1Dup)).To(BeFalse())
		})

		It("should return true for same NAT element with same external IDs", func() {
			nat := createTestNAT("10.0.0.1", "192.168.0.1", "snat", map[string]string{"foo": "bar"})
			natDup := createTestNAT("10.0.0.1", "192.168.0.1", "snat", map[string]string{"foo": "bar"})
			natSet := sets.NewNATSet()
			natSet.Add(nat)
			Expect(natSet.Has(natDup)).To(BeTrue())
		})
	})

	Context("Len", func() {
		It("should return 0 for empty set", func() {
			Expect(natSet.Len()).To(Equal(0))
		})

		It("should return correct length after adding elements", func() {
			natSet.Add(nat1, nat2)
			Expect(natSet.Len()).To(Equal(2))
		})

		It("should return correct length after removing elements", func() {
			natSet.Add(nat1, nat2)
			Expect(natSet.Len()).To(Equal(2))
			natSet.Remove(nat1)
			Expect(natSet.Len()).To(Equal(1))
		})
	})

	Context("In", func() {
		var otherSet sets.NATSet

		BeforeEach(func() {
			otherSet = sets.NewNATSet()
		})

		It("should return true when all elements are in other set", func() {
			natSet.Add(nat1)
			otherSet.Add(nat1, nat2)
			Expect(natSet.In(otherSet)).To(BeTrue())
		})

		It("should return false when some elements are not in other set", func() {
			natSet.Add(nat1, nat3)
			otherSet.Add(nat1, nat2)
			Expect(natSet.In(otherSet)).To(BeFalse())
		})

		It("should return false when sets are disjoint", func() {
			natSet.Add(nat1)
			otherSet.Add(nat2, nat3)
			Expect(natSet.In(otherSet)).To(BeFalse())
		})

		It("should return false when other set is empty", func() {
			natSet.Add(nat1)
			Expect(natSet.In(otherSet)).To(BeFalse())
		})

		It("should return true for empty set", func() {
			Expect(natSet.In(otherSet)).To(BeTrue())
			otherSet.Add(nat1)
			Expect(natSet.In(otherSet)).To(BeTrue())
		})
	})

	Context("Intersect", func() {
		var otherSet sets.NATSet

		BeforeEach(func() {
			otherSet = sets.NewNATSet()
		})

		It("should return empty set when both sets are empty", func() {
			intersection := natSet.Intersect(otherSet)
			Expect(intersection.Len()).To(Equal(0))
		})

		It("should return empty set when one set is empty", func() {
			natSet.Add(nat1)
			intersection := natSet.Intersect(otherSet)
			Expect(intersection.Len()).To(Equal(0))
			intersection = otherSet.Intersect(natSet)
			Expect(intersection.Len()).To(Equal(0))
		})

		It("should return empty set when sets are disjoint", func() {
			natSet.Add(nat1)
			otherSet.Add(nat2)
			intersection := natSet.Intersect(otherSet)
			Expect(intersection.Len()).To(Equal(0))
		})

		It("should return intersection of two sets", func() {
			natSet.Add(nat1, nat3)
			otherSet.Add(nat1, nat2)
			intersection := natSet.Intersect(otherSet)
			Expect(intersection.Len()).To(Equal(1))
			Expect(intersection.Has(nat1)).To(BeTrue())
		})
	})

	Context("Difference", func() {
		var otherSet sets.NATSet

		BeforeEach(func() {
			otherSet = sets.NewNATSet()
		})

		It("should return empty set when both sets are empty", func() {
			difference := natSet.Difference(otherSet)
			Expect(difference.Len()).To(Equal(0))
		})

		It("should return empty set when this set is empty", func() {
			otherSet.Add(nat1)
			difference := natSet.Difference(otherSet)
			Expect(difference.Len()).To(Equal(0))
		})

		It("should return this set when other set is empty", func() {
			natSet.Add(nat1, nat2)
			difference := natSet.Difference(otherSet)
			Expect(difference.Len()).To(Equal(2))
		})

		It("should return this set when other set is disjoint", func() {
			natSet.Add(nat1, nat2)
			otherSet.Add(nat3)
			difference := natSet.Difference(otherSet)
			Expect(difference.Len()).To(Equal(2))
		})

		It("should return difference between two sets", func() {
			natSet.Add(nat1, nat3)
			otherSet.Add(nat1, nat2)
			difference := natSet.Difference(otherSet)
			Expect(difference.Len()).To(Equal(1))
			Expect(difference.Has(nat3)).To(BeTrue())
		})
	})

	Context("SymmetricDifference", func() {
		var otherSet sets.NATSet

		BeforeEach(func() {
			otherSet = sets.NewNATSet()
		})

		It("should return empty set when both sets are empty", func() {
			symDiff := natSet.SymmetricDifference(otherSet)
			Expect(symDiff.Len()).To(Equal(0))
		})

		It("should return empty set when both sets are equal", func() {
			natSet.Add(nat1, nat2)
			otherSet.Add(nat1, nat2)
			symDiff := natSet.SymmetricDifference(otherSet)
			Expect(symDiff.Len()).To(Equal(0))
		})

		It("should return all elements in one of the sets when the other is empty", func() {
			natSet.Add(nat1, nat2)
			symDiff := natSet.SymmetricDifference(otherSet)
			Expect(symDiff.Len()).To(Equal(2))
			Expect(symDiff.Has(nat1)).To(BeTrue())
			Expect(symDiff.Has(nat2)).To(BeTrue())

			symDiff = otherSet.SymmetricDifference(natSet)
			Expect(symDiff.Len()).To(Equal(2))
			Expect(symDiff.Has(nat1)).To(BeTrue())
			Expect(symDiff.Has(nat2)).To(BeTrue())
		})

		It("should return symmetric difference between two sets", func() {
			natSet.Add(nat1, nat3)
			otherSet.Add(nat1, nat2)
			symDiff := natSet.SymmetricDifference(otherSet)
			Expect(symDiff.Len()).To(Equal(2))
			Expect(symDiff.Has(nat2)).To(BeTrue())
			Expect(symDiff.Has(nat3)).To(BeTrue())

			symDiff = otherSet.SymmetricDifference(natSet)
			Expect(symDiff.Len()).To(Equal(2))
			Expect(symDiff.Has(nat2)).To(BeTrue())
			Expect(symDiff.Has(nat3)).To(BeTrue())
		})

		It("should return symmetric difference between two disjoint sets", func() {
			natSet.Add(nat1, nat2)
			otherSet.Add(nat3)
			symDiff := natSet.SymmetricDifference(otherSet)
			Expect(symDiff.Len()).To(Equal(3))
			Expect(symDiff.Has(nat1)).To(BeTrue())
			Expect(symDiff.Has(nat2)).To(BeTrue())
			Expect(symDiff.Has(nat3)).To(BeTrue())
		})
	})

	Context("Union", func() {
		var otherSet sets.NATSet

		BeforeEach(func() {
			otherSet = sets.NewNATSet()
		})

		It("should return empty set when both sets are empty", func() {
			union := natSet.Union(otherSet)
			Expect(union.Len()).To(Equal(0))
		})

		It("should return all elements in one of the sets when the other is empty", func() {
			natSet.Add(nat1, nat2)
			union := natSet.Union(otherSet)
			Expect(union.Len()).To(Equal(2))
			Expect(union.Has(nat1)).To(BeTrue())
			Expect(union.Has(nat2)).To(BeTrue())
			union = otherSet.Union(natSet)
			Expect(union.Len()).To(Equal(2))
			Expect(union.Has(nat1)).To(BeTrue())
			Expect(union.Has(nat2)).To(BeTrue())
		})

		It("should return union of two sets", func() {
			natSet.Add(nat1, nat3)
			otherSet.Add(nat1, nat2)
			union := natSet.Union(otherSet)
			Expect(union.Len()).To(Equal(3))
			Expect(union.Has(nat1)).To(BeTrue())
			Expect(union.Has(nat2)).To(BeTrue())
			Expect(union.Has(nat3)).To(BeTrue())
		})
	})

	Context("Equals", func() {
		var otherSet sets.NATSet

		BeforeEach(func() {
			otherSet = sets.NewNATSet()
		})

		It("should return true for empty sets", func() {
			Expect(natSet.Equals(otherSet)).To(BeTrue())
		})

		It("should return true for equal sets", func() {
			natSet.Add(nat1, nat2)
			otherSet.Add(nat1, nat2)
			Expect(natSet.Equals(otherSet)).To(BeTrue())
		})

		It("should return false for different sets", func() {
			natSet.Add(nat1, nat2)
			otherSet.Add(nat1, nat3)
			Expect(natSet.Equals(otherSet)).To(BeFalse())
		})

		It("should return false for different set sizes", func() {
			natSet.Add(nat1, nat2)
			otherSet.Add(nat1)
			Expect(natSet.Equals(otherSet)).To(BeFalse())
		})

		It("should return false when one of the set is empty", func() {
			natSet.Add(nat1)
			Expect(natSet.Equals(otherSet)).To(BeFalse())
			Expect(otherSet.Equals(natSet)).To(BeFalse())
		})
	})

	Context("List", func() {
		It("should return all elements in the set", func() {
			natSet.Add(nat1, nat2)
			list := natSet.List()
			Expect(list).To(HaveLen(2))
			Expect(list).To(ContainElements(nat1, nat2))
		})

		It("should return empty list when set is empty", func() {
			list := natSet.List()
			Expect(list).To(BeEmpty())
		})
	})

	Context("String", func() {
		It("should return string representation of the set", func() {
			natSet.Add(nat1)
			Expect(natSet.String()).To(ContainSubstring(nat1.ExternalIP))
			Expect(natSet.String()).To(ContainSubstring(nat1.LogicalIP))
			Expect(natSet.String()).To(ContainSubstring(nat1.Type))
		})
	})
})
