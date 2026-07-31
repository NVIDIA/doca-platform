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

package sets

import (
	"fmt"
	"hash/fnv"
	"slices"
	"strings"

	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/nbdb"
)

// PolicySet interface defines API for managing router policies as Set
// implementation is not thread safe and modifies the elements passed to it.
type PolicySet interface {
	// Add adds policy element to set
	Add(policies ...*nbdb.LogicalRouterPolicy)
	// Remove removes policy elements from set. if policy a element does not exist, it is ignored
	Remove(policies ...*nbdb.LogicalRouterPolicy)
	// Has returns true if policy element is in the set, else returns false
	Has(policy *nbdb.LogicalRouterPolicy) bool
	// Len returns the number of elements in the set
	Len() int
	// In returns true if every element in this set is an alement of other set. else it returns false
	In(other PolicySet) bool
	// Intersect returns a new PolicySet with elements from both this PolicySet and other PolicySet
	Intersect(other PolicySet) PolicySet
	// Difference returns the difference between this and other PolicySet, that is, elements in this PolicySet
	// and not the other PolicySet
	Difference(other PolicySet) PolicySet
	// SymmetricDifference returns a set of elements which are in either of the sets,
	// but not in their intersection.
	SymmetricDifference(other PolicySet) PolicySet
	// Union returns a new PolicySet with elements from both this PolicySet and other PolicySet
	Union(other PolicySet) PolicySet
	// Equals returns true if this and other PolicySet are equal (have the same elements)
	Equals(other PolicySet) bool
	// List returns the policy elements in PolicySet
	List() []*nbdb.LogicalRouterPolicy
	// String returns a string representation of the PolicySet
	String() string
}

// NewPolicySet creates a new PolicySet
func NewPolicySet() PolicySet {
	return &policySetImpl{
		policies: make(map[string]*nbdb.LogicalRouterPolicy),
	}
}

// policySetImpl is the implementation of PolicySet
type policySetImpl struct {
	policies map[string]*nbdb.LogicalRouterPolicy
}

// Add adds policy elements to set
func (ps *policySetImpl) Add(policies ...*nbdb.LogicalRouterPolicy) {
	for _, p := range policies {
		if p == nil {
			continue
		}

		if !ps.Has(p) {
			ps.policies[p.ExternalIDs[VPCCookieKey]] = p
		}
	}
}

// Remove removes policy elements from set. if policy a element does not exist, it is ignored
func (ps *policySetImpl) Remove(policies ...*nbdb.LogicalRouterPolicy) {
	for _, p := range policies {
		if ps.Has(p) {
			delete(ps.policies, p.ExternalIDs[VPCCookieKey])
		}
	}
}

// Has returns true if policy element is in the set, else returns false
func (ps *policySetImpl) Has(policy *nbdb.LogicalRouterPolicy) bool {
	if policy == nil {
		return true
	}
	addCookieToPolicy(policy)

	if _, ok := ps.policies[policy.ExternalIDs[VPCCookieKey]]; ok {
		return true
	}
	return false
}

// Len returns the number of elements in the set
func (ps *policySetImpl) Len() int {
	return len(ps.policies)
}

// In returns true if every element of this set is in other set. else it returns false
func (ps *policySetImpl) In(other PolicySet) bool {
	if ps.Len() > other.Len() {
		return false
	}

	for _, p := range ps.policies {
		if !other.Has(p) {
			return false
		}
	}
	return true
}

// Intersect returns a new PolicySet with elements from both this PolicySet and other PolicySet
func (ps *policySetImpl) Intersect(other PolicySet) PolicySet {
	nps := NewPolicySet()

	for _, p := range ps.policies {
		if other.Has(p) {
			nps.Add(p)
		}
	}
	return nps
}

// Difference returns the difference between this and other PolicySet, that is, elements in this PolicySet
// and not the other PolicySet
func (ps *policySetImpl) Difference(other PolicySet) PolicySet {
	nps := NewPolicySet()

	for _, p := range ps.policies {
		if !other.Has(p) {
			nps.Add(p)
		}
	}
	return nps
}

// Equals returns true if this and other PolicySet are equal (have the same elements)
func (ps *policySetImpl) Equals(other PolicySet) bool {
	return ps.Len() == other.Len() && ps.In(other)
}

// List returns the policy elements in PolicySet
func (ps *policySetImpl) List() []*nbdb.LogicalRouterPolicy {
	pss := make([]*nbdb.LogicalRouterPolicy, 0, ps.Len())

	for _, p := range ps.policies {
		pss = append(pss, p)
	}
	return pss
}

// Union returns a new PolicySet with elements from both this PolicySet and other PolicySet
func (ps *policySetImpl) Union(other PolicySet) PolicySet {
	nps := NewPolicySet()
	nps.Add(ps.List()...)
	nps.Add(other.List()...)
	return nps
}

// SymmetricDifference returns a set of elements which are in either of the sets,
// but not in their intersection.
func (ps *policySetImpl) SymmetricDifference(other PolicySet) PolicySet {
	return ps.Difference(other).Union(other.Difference(ps))
}

// String returns a string representation of the PolicySet
func (ps *policySetImpl) String() string {
	out := make([]string, 0, ps.Len())
	for _, p := range ps.policies {
		policy := fmt.Sprintf("[match(%s), action(%s), nexthops(%s), externalIDs(%s)]",
			p.Match, p.Action, nexthopsString(p), externalIDsString(p.ExternalIDs))
		out = append(out, policy)
	}
	// sort the list to ensure consistent ordering
	slices.Sort(out)
	return strings.Join(out, " ")
}

// addCookieToPolicy adds cookie to the policy's external ids if not set
func addCookieToPolicy(policy *nbdb.LogicalRouterPolicy) {
	if policy.ExternalIDs == nil {
		policy.ExternalIDs = make(map[string]string)
	}

	if _, ok := policy.ExternalIDs[VPCCookieKey]; !ok {
		policy.ExternalIDs[VPCCookieKey] = calcHashForPolicy(policy)
	}
}

// calcHashForPolicy calculates hash for the policy
func calcHashForPolicy(policy *nbdb.LogicalRouterPolicy) string {
	h := fnv.New64a()
	data := strings.Join([]string{
		fmt.Sprintf("%d", policy.Priority),
		policy.Match,
		policy.Action,
		nexthopsString(policy),
		externalIDsString(policy.ExternalIDs)},
		":")
	_, _ = h.Write([]byte(data))
	return fmt.Sprintf("%x", h.Sum64())
}

// nexthopsString returns a string representation of the nexthops in the policy
func nexthopsString(policy *nbdb.LogicalRouterPolicy) string {
	var nextHops []string

	if policy.Nexthop != nil {
		nextHops = append(nextHops, *policy.Nexthop)
	}
	nextHops = append(nextHops, policy.Nexthops...)
	slices.Sort(nextHops)
	return strings.Join(nextHops, ",")
}
