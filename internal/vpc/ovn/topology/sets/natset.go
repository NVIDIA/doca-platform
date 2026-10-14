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

// NATSet interface defines API for managing NAT rules as Set
// implementation is not thread safe and modifies the elements passed to it.
type NATSet interface {
	// Add adds NAT element to set
	Add(nats ...*nbdb.NAT)
	// Remove removes NAT elements from set. if NAT element does not exist, it is ignored
	Remove(nats ...*nbdb.NAT)
	// Has returns true if NAT element is in the set, else returns false
	Has(nat *nbdb.NAT) bool
	// Len returns the number of elements in the set
	Len() int
	// In returns true if every element in this set is an element of other set. else it returns false
	In(other NATSet) bool
	// Intersect returns a new NATSet with elements from both this NATSet and other NATSet
	Intersect(other NATSet) NATSet
	// Difference returns the difference between this and other NATSet, that is, elements in this NATSet
	// and not the other NATSet
	Difference(other NATSet) NATSet
	// SymmetricDifference returns a set of elements which are in either of the sets,
	// but not in their intersection.
	SymmetricDifference(other NATSet) NATSet
	// Union returns a new NATSet with elements from both this NATSet and other NATSet
	Union(other NATSet) NATSet
	// Equals returns true if this and other NATSet are equal (have the same elements)
	Equals(other NATSet) bool
	// List returns the NAT elements in NATSet
	List() []*nbdb.NAT
	// String returns a string representation of the NATSet
	String() string
}

// NewNATSet creates a new NATSet
func NewNATSet() NATSet {
	return &natSetImpl{
		nats: make(map[string]*nbdb.NAT),
	}
}

// natSetImpl is the implementation of NATSet
type natSetImpl struct {
	nats map[string]*nbdb.NAT
}

// Add adds NAT elements to set
func (ns *natSetImpl) Add(nats ...*nbdb.NAT) {
	for _, n := range nats {
		if n == nil {
			continue
		}

		if !ns.Has(n) {
			ns.nats[n.ExternalIDs[VPCCookieKey]] = n
		}
	}
}

// Remove removes NAT elements from set. if NAT element does not exist, it is ignored
func (ns *natSetImpl) Remove(nats ...*nbdb.NAT) {
	for _, n := range nats {
		if ns.Has(n) {
			delete(ns.nats, n.ExternalIDs[VPCCookieKey])
		}
	}
}

// Has returns true if NAT element is in the set, else returns false
func (ns *natSetImpl) Has(nat *nbdb.NAT) bool {
	if nat == nil {
		return true
	}
	addCookieToNAT(nat)

	if _, ok := ns.nats[nat.ExternalIDs[VPCCookieKey]]; ok {
		return true
	}
	return false
}

// Len returns the number of elements in the set
func (ns *natSetImpl) Len() int {
	return len(ns.nats)
}

// In returns true if every element of this set is in other set. else it returns false
func (ns *natSetImpl) In(other NATSet) bool {
	if ns.Len() > other.Len() {
		return false
	}

	for _, n := range ns.nats {
		if !other.Has(n) {
			return false
		}
	}
	return true
}

// Intersect returns a new NATSet with elements from both this NATSet and other NATSet
func (ns *natSetImpl) Intersect(other NATSet) NATSet {
	nns := NewNATSet()

	for _, n := range ns.nats {
		if other.Has(n) {
			nns.Add(n)
		}
	}
	return nns
}

// Difference returns the difference between this and other NATSet, that is, elements in this NATSet
// and not the other NATSet
func (ns *natSetImpl) Difference(other NATSet) NATSet {
	nns := NewNATSet()

	for _, n := range ns.nats {
		if !other.Has(n) {
			nns.Add(n)
		}
	}
	return nns
}

// Equals returns true if this and other NATSet are equal (have the same elements)
func (ns *natSetImpl) Equals(other NATSet) bool {
	return ns.Len() == other.Len() && ns.In(other)
}

// List returns the NAT elements in NATSet
func (ns *natSetImpl) List() []*nbdb.NAT {
	nats := make([]*nbdb.NAT, 0, ns.Len())

	for _, n := range ns.nats {
		nats = append(nats, n)
	}
	return nats
}

// Union returns a new NATSet with elements from both this NATSet and other NATSet
func (ns *natSetImpl) Union(other NATSet) NATSet {
	nns := NewNATSet()
	nns.Add(ns.List()...)
	nns.Add(other.List()...)
	return nns
}

// SymmetricDifference returns a set of elements which are in either of the sets,
// but not in their intersection.
func (ns *natSetImpl) SymmetricDifference(other NATSet) NATSet {
	return ns.Difference(other).Union(other.Difference(ns))
}

// String returns a string representation of the NATSet
func (ns *natSetImpl) String() string {
	out := make([]string, 0, ns.Len())
	for _, n := range ns.nats {
		nat := fmt.Sprintf("[type(%s), externalIP(%s), logicalIP(%s), externalIDs(%s)]",
			n.Type, n.ExternalIP, n.LogicalIP, externalIDsString(n.ExternalIDs))
		out = append(out, nat)
	}
	// sort the list to ensure consistent ordering
	slices.Sort(out)
	return strings.Join(out, " ")
}

// addCookieToNAT adds cookie to the NAT's external ids if not set
func addCookieToNAT(nat *nbdb.NAT) {
	if nat.ExternalIDs == nil {
		nat.ExternalIDs = make(map[string]string)
	}

	if _, ok := nat.ExternalIDs[VPCCookieKey]; !ok {
		nat.ExternalIDs[VPCCookieKey] = calcHashForNAT(nat)
	}
}

// calcHashForNAT calculates hash for the NAT
func calcHashForNAT(nat *nbdb.NAT) string {
	h := fnv.New64a()
	data := strings.Join([]string{
		nat.Type,
		nat.ExternalIP,
		nat.LogicalIP,
		externalIDsString(nat.ExternalIDs)},
		":")
	_, _ = h.Write([]byte(data))
	return fmt.Sprintf("%x", h.Sum64())
}
