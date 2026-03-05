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

const (
	// VPCCookieKey is the key for storing cookies in the external IDs
	VPCCookieKey = "vpc-cookie"
)

// RouteSet interface defines API for managing routes as Set
// implementation is not thread safe and modifies the elements passed to it.
type RouteSet interface {
	// Add adds route element to set
	Add(routes ...*nbdb.LogicalRouterStaticRoute)
	// Remove removes route elements from set. if a route element does not exist, it is ignored
	Remove(routes ...*nbdb.LogicalRouterStaticRoute)
	// Has returns true if route element is in the set, else returns false
	Has(route *nbdb.LogicalRouterStaticRoute) bool
	// Len returns the number of elements in the set
	Len() int
	// In returns true if every element in this set is an alement of other set. else it returns false
	In(other RouteSet) bool
	// Intersect returns a new RouteSet with elements from both this RouteSet and other RouteSet
	Intersect(other RouteSet) RouteSet
	// Difference returns the difference between this and other RouteSet, that is, elements in this RouteSet
	// and not the other RouteSet
	Difference(other RouteSet) RouteSet
	// SymmetricDifference returns a set of elements which are in either of the sets,
	// but not in their intersection.
	SymmetricDifference(other RouteSet) RouteSet
	// Union returns a new RouteSet with elements from both this RouteSet and other RouteSet
	Union(other RouteSet) RouteSet
	// Equals returns true if this and other RouteSet are equal (have the same elements)
	Equals(other RouteSet) bool
	// List returns the route elements in RouteSet
	List() []*nbdb.LogicalRouterStaticRoute
	// String returns a string representation of the RouteSet
	String() string
}

// NewRouteSet creates a new RouteSet
func NewRouteSet() RouteSet {
	return &routeSetImpl{
		routes: make(map[string]*nbdb.LogicalRouterStaticRoute),
	}
}

// routeSetImpl is the implementation of RouteSet
type routeSetImpl struct {
	routes map[string]*nbdb.LogicalRouterStaticRoute
}

// Add adds route elements to set
func (rs *routeSetImpl) Add(routes ...*nbdb.LogicalRouterStaticRoute) {
	for _, r := range routes {
		if r == nil {
			continue
		}

		if !rs.Has(r) {
			rs.routes[r.ExternalIDs[VPCCookieKey]] = r
		}
	}
}

// Remove removes route elements from set. if a route element does not exist, it is ignored
func (rs *routeSetImpl) Remove(routes ...*nbdb.LogicalRouterStaticRoute) {
	for _, r := range routes {
		if rs.Has(r) {
			delete(rs.routes, r.ExternalIDs[VPCCookieKey])
		}
	}
}

// Has returns true if route element is in the set, else returns false
func (rs *routeSetImpl) Has(route *nbdb.LogicalRouterStaticRoute) bool {
	if route == nil {
		return true
	}
	addCookieToRoute(route)

	if _, ok := rs.routes[route.ExternalIDs[VPCCookieKey]]; ok {
		return true
	}
	return false
}

// Len returns the number of elements in the set
func (rs *routeSetImpl) Len() int {
	return len(rs.routes)
}

// In returns true if every element of this set is in other set. else it returns false
func (rs *routeSetImpl) In(other RouteSet) bool {
	if rs.Len() > other.Len() {
		return false
	}

	for _, r := range rs.routes {
		if !other.Has(r) {
			return false
		}
	}
	return true
}

// Intersect returns a new RouteSet with elements from both this RouteSet and other RouteSet
func (rs *routeSetImpl) Intersect(other RouteSet) RouteSet {
	nrs := NewRouteSet()

	for _, r := range rs.routes {
		if other.Has(r) {
			nrs.Add(r)
		}
	}
	return nrs
}

// Difference returns the difference between this and other RouteSet, that is, elements in this RouteSet
// and not the other RouteSet
func (rs *routeSetImpl) Difference(other RouteSet) RouteSet {
	nrs := NewRouteSet()

	for _, r := range rs.routes {
		if !other.Has(r) {
			nrs.Add(r)
		}
	}
	return nrs
}

// Equals returns true if this and other RouteSet are equal (have the same elements)
func (rs *routeSetImpl) Equals(other RouteSet) bool {
	return rs.Len() == other.Len() && rs.In(other)
}

// List returns the route elements in RouteSet
func (rs *routeSetImpl) List() []*nbdb.LogicalRouterStaticRoute {
	rss := make([]*nbdb.LogicalRouterStaticRoute, 0, rs.Len())

	for _, r := range rs.routes {
		rss = append(rss, r)
	}
	return rss
}

// Union returns a new RouteSet with elements from both this RouteSet and other RouteSet
func (rs *routeSetImpl) Union(other RouteSet) RouteSet {
	nrs := NewRouteSet()
	nrs.Add(rs.List()...)
	nrs.Add(other.List()...)
	return nrs
}

// SymmetricDifference returns a set of elements which are in either of the sets,
// but not in their intersection.
func (rs *routeSetImpl) SymmetricDifference(other RouteSet) RouteSet {
	return rs.Difference(other).Union(other.Difference(rs))
}

// String returns a string representation of the RouteSet
func (rs *routeSetImpl) String() string {
	out := make([]string, 0, rs.Len())
	for _, r := range rs.routes {
		route := fmt.Sprintf("[prefix(%s) -> nexthop(%s), externalIDs(%s)]", r.IPPrefix, r.Nexthop, externalIDsString(r.ExternalIDs))
		out = append(out, route)
	}
	// sort the list to ensure consistent ordering
	slices.Sort(out)
	return strings.Join(out, " ")
}

// addCookieToRoute adds cookie to the route's external ids if not set
func addCookieToRoute(route *nbdb.LogicalRouterStaticRoute) {
	if route.ExternalIDs == nil {
		route.ExternalIDs = make(map[string]string)
	}

	if _, ok := route.ExternalIDs[VPCCookieKey]; !ok {
		route.ExternalIDs[VPCCookieKey] = calcHashForRoute(route)
	}
}

// calcHashForRoute calculates hash for the route
func calcHashForRoute(route *nbdb.LogicalRouterStaticRoute) string {
	h := fnv.New64a()
	data := strings.Join([]string{
		route.IPPrefix,
		route.Nexthop,
		externalIDsString(route.ExternalIDs)},
		":")
	_, _ = h.Write([]byte(data))
	return fmt.Sprintf("%x", h.Sum64())
}

// externalIDsString returns a string representation of the external IDs in a stable manner (map order does not matter)
func externalIDsString(externalIDs map[string]string) string {
	if externalIDs == nil {
		return ""
	}
	// store as list of key=value pairs, skip VPCCookieKey
	out := make([]string, 0, len(externalIDs))
	for k, v := range externalIDs {
		if k == VPCCookieKey {
			continue
		}
		out = append(out, fmt.Sprintf("%s=%s", k, v))
	}
	// sort the list to ensure consistent ordering
	slices.Sort(out)
	return strings.Join(out, ",")
}
