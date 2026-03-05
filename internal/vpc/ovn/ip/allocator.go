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

package ip

// This package is based on https://github.com/Mellanox/nvidia-k8s-ipam/blob/v0.3.5/pkg/ipam-node/allocator/allocator.go
// modified to fit vpc needs.

import (
	"errors"
	"fmt"
	"net"
	"sync"
)

var (
	// ErrNoFreeAddresses is returned is there is no free IP addresses left in the pool
	ErrNoFreeAddresses = errors.New("no free addresses in the allocated range")
)

// IPAllocation is an allocation for a given IP address
type IPAllocation struct {
	// Address is the IP address allocated
	Address net.IPNet
	// Gateway is the gateway for the network
	Gateway net.IP
}

// IPAllocator is an interface to allocate IPs for a given range
type IPAllocator interface {
	// Allocate allocates IP address from a range for a given (unique) ID. if staticIP is provided, the allocator will attempt to allocate that IP if free.
	// if the saticIP is already allocated for the given ID then no error will be returned and the same IP will be returned.
	// only a single IP can be allocated for a given ID.
	Allocate(id string, staticIP net.IP) (*IPAllocation, error)
	// AllocateGateway allocates the gateway IP address for a given ID.
	// if id already has an allocation other than the gateway, error is returned. else the gateway IP is allocated and returned.
	AllocateGateway(id string) (*IPAllocation, error)
	// Deallocate deallocates IP address for a given ID. if id is not found(no IP allocated for the ID) no error is returned
	Deallocate(id string)
	// Preallocate, preallocates a set of IPs for a given set of IDs. This is useful when we want to initialize allocator with a set of preallocated IPs.
	Preallocate(map[string]net.IP) error
	// GetAllocation returns the IPAllocation for a given ID. if no allocation for the given ID, nil is returned.
	GetAllocation(id string) *IPAllocation

	// SetLastReservedIP sets the last reserved IP by the allocator. This is useful for testing and generally should not be used.
	SetLastReservedIP(ip net.IP)
	// GetLastReservedIP returns the last reserved IP by the allocator. This is useful for testing and generally should not be used.
	GetLastReservedIP() net.IP
	// ListAllocationIDs returns the list of IDs that have allocations
	ListAllocationIDs() []string
}

// allocator implements IPAllocator
type allocator struct {
	mu sync.Mutex

	// rangeSet is the set of ranges that the allocator can allocate from
	rangeSet *RangeSet
	// exclusions is the set of IPs that are excluded from the allocation
	exclusions *RangeSet
	// lastReservedIP is the last IP that was reserved by the allocator
	lastReservedIP net.IP
	// allocations is a map of ID to IP. it contains alls the IPs that are allocated by the allocator
	allocations map[string]net.IP
}

// NewIPAllocator create and initialize a new instance of IPAllocator
func NewIPAllocator(s *RangeSet, exclusions *RangeSet) IPAllocator {
	return &allocator{
		mu:             sync.Mutex{},
		rangeSet:       s,
		exclusions:     exclusions,
		lastReservedIP: nil,
		allocations:    make(map[string]net.IP),
	}
}

// GetAllocation returns the IPAllocation for a given ID.
func (a *allocator) GetAllocation(id string) *IPAllocation {
	a.mu.Lock()
	defer a.mu.Unlock()

	ip, ok := a.allocations[id]
	if !ok {
		return nil
	}

	for _, r := range *a.rangeSet {
		if r.Contains(ip) {
			return &IPAllocation{
				Address: net.IPNet{IP: ip, Mask: r.Subnet.Mask},
				Gateway: r.Gateway,
			}
		}
	}

	// This should never happen
	panic(fmt.Sprintf("IP %s allocated for ID %s is not in any of the ranges %s", ip.String(), id, a.rangeSet.String()))
}

// Deallocate deallocates IP address
func (a *allocator) Deallocate(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	delete(a.allocations, id)
}

// Preallocate, preallocates a set of IPs for a given set of IDs.
func (a *allocator) Preallocate(ips map[string]net.IP) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	for id, ip := range ips {
		_, err := a.allocateStaticIP(id, ip)
		if err != nil {
			return fmt.Errorf("failed to preallocate IPs. %w", err)
		}
	}
	return nil
}

// AllocateGateway allocates the gateway IP address for a given ID.
func (a *allocator) AllocateGateway(id string) (*IPAllocation, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// find gateway
	var gw net.IP
	var mask net.IPMask
	for _, r := range *a.rangeSet {
		if r.Gateway != nil {
			gw = r.Gateway
			mask = r.Subnet.Mask
			break
		}
	}

	if gw == nil {
		return nil, fmt.Errorf("no gateway found in the range set")
	}

	// check allocations for id
	if addr, ok := a.allocations[id]; ok {
		if Cmp(addr, gw) != 0 {
			return nil, fmt.Errorf("a different IP %s is already allocated for ID %s", addr.String(), id)
		}
		// gateway addr already allocated for id
		return &IPAllocation{
			Address: net.IPNet{IP: addr, Mask: mask},
			Gateway: gw,
		}, nil
	}

	// no allocation for id
	if a.isIPAllocated(gw) {
		return nil, fmt.Errorf("gateway IP %s is already allocated for different ID", gw.String())
	}

	// reserve GW address for id and return
	a.allocations[id] = gw
	return &IPAllocation{
		Address: net.IPNet{IP: gw, Mask: mask},
		Gateway: gw,
	}, nil
}

// Allocate allocates an IP
func (a *allocator) Allocate(id string, staticIP net.IP) (*IPAllocation, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	var reservedIP *net.IPNet
	var gw net.IP

	if staticIP != nil {
		return a.allocateStaticIP(id, staticIP)
	}

	if addr, ok := a.allocations[id]; ok {
		for _, r := range *a.rangeSet {
			if r.Contains(addr) {
				return &IPAllocation{
					Address: net.IPNet{IP: addr, Mask: r.Subnet.Mask},
					Gateway: r.Gateway,
				}, nil
			}
		}
		return nil, fmt.Errorf("unexpected Error: IP %s allocated for ID %s is not in any of the ranges", addr.String(), id)
	}

	iter := a.getIter()
	for {
		reservedIP, gw = iter.Next()
		if reservedIP == nil {
			return nil, ErrNoFreeAddresses
		}
		if a.exclusions != nil && a.exclusions.Contains(reservedIP.IP) {
			continue
		}

		if a.isIPAllocated(reservedIP.IP) {
			continue
		}

		a.lastReservedIP = reservedIP.IP
		a.allocations[id] = reservedIP.IP
		break
	}

	return &IPAllocation{
		Address: *reservedIP,
		Gateway: gw,
	}, nil
}

// allocateStaticIP allocates a predefined IP for a given ID
func (a *allocator) allocateStaticIP(id string, staticIP net.IP) (*IPAllocation, error) {
	for _, r := range *a.rangeSet {
		rangeSubnet := r.Subnet
		if !rangeSubnet.Contains(staticIP) {
			continue
		}

		if allocIP, ok := a.allocations[id]; ok {
			if Cmp(allocIP, staticIP) != 0 {
				// a different IP already allocated for the ID
				return nil, fmt.Errorf("a different static IP %s is already allocated for ID %s", allocIP.String(), id)
			}
		} else {
			if a.isIPAllocated(staticIP) {
				return nil, fmt.Errorf("static IP %s is already allocated for different ID %s", staticIP.String(), id)
			}
			a.allocations[id] = staticIP
		}

		return &IPAllocation{
			Address: net.IPNet{IP: staticIP, Mask: r.Subnet.Mask},
			Gateway: r.Gateway,
		}, nil
	}
	return nil, fmt.Errorf("can't find IP range in the allocator for static IP: %s", staticIP.String())
}

// RangeIter implements iterator over the RangeSet
type RangeIter struct {
	// The set of ranges to iterate over
	rangeSet *RangeSet
	// The current range id
	rangeIdx int
	// Our current position
	cur net.IP
	// The IP where we started iterating; if we hit this again, we're done.
	startIP net.IP
}

// getIter encapsulates the strategy for this allocator.
// We use a round-robin strategy, attempting to evenly use the whole set.
func (a *allocator) getIter() *RangeIter {
	iter := RangeIter{
		rangeSet: a.rangeSet,
	}

	// Round-robin by trying to allocate from the last reserved IP + 1
	startFromLastReservedIP := false

	// We might get a last reserved IP that is wrong if the range indexes changed.
	// This is not critical, we just lose round-robin this one time.
	if a.lastReservedIP != nil {
		startFromLastReservedIP = a.rangeSet.Contains(a.lastReservedIP)
	}

	// Find the range in the set with this IP
	if startFromLastReservedIP {
		for i, r := range *a.rangeSet {
			if r.Contains(a.lastReservedIP) {
				iter.rangeIdx = i

				// We advance the cursor on every Next(), so the first call
				// to next() will return lastReservedIP + 1
				iter.cur = a.lastReservedIP
				break
			}
		}
	} else {
		iter.rangeIdx = 0
		iter.startIP = (*a.rangeSet)[0].RangeStart
	}
	return &iter
}

// isIPAllocated returns true if the IP is already allocated
func (a *allocator) isIPAllocated(ip net.IP) bool {
	for _, allocatedIP := range a.allocations {
		if Cmp(allocatedIP, ip) == 0 {
			return true
		}
	}
	return false
}

// SetLastReservedIP sets the last reserved IP by the allocator
func (a *allocator) SetLastReservedIP(ip net.IP) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.lastReservedIP = ip
}

// GetLastReservedIP returns the last reserved IP by the allocator
func (a *allocator) GetLastReservedIP() net.IP {
	a.mu.Lock()
	defer a.mu.Unlock()

	lastReservedCopy := make(net.IP, len(a.lastReservedIP))
	copy(lastReservedCopy, a.lastReservedIP)
	return lastReservedCopy
}

// ListAllocationIDs lists the IDs that have allocations
func (a *allocator) ListAllocationIDs() []string {
	a.mu.Lock()
	defer a.mu.Unlock()

	ids := make([]string, 0, len(a.allocations))
	for id := range a.allocations {
		ids = append(ids, id)
	}
	return ids
}

// Next returns the next IP, its mask, and its gateway. Returns nil if the iterator has been exhausted
func (i *RangeIter) Next() (*net.IPNet, net.IP) {
	r := (*i.rangeSet)[i.rangeIdx]

	// If this is the first time iterating, and we're not starting in the middle
	// of the range, then start at rangeStart, which is inclusive
	if i.cur == nil {
		i.cur = r.RangeStart
		i.startIP = i.cur
		if r.Gateway != nil && i.cur.Equal(r.Gateway) {
			return i.Next()
		}
		return &net.IPNet{IP: i.cur, Mask: r.Subnet.Mask}, r.Gateway
	}

	nextIP := NextIP(i.cur)
	// If we've reached the end of this range, we need to advance the range
	// RangeEnd is inclusive as well
	if i.cur.Equal(r.RangeEnd) || nextIP == nil {
		i.rangeIdx++
		i.rangeIdx %= len(*i.rangeSet)
		r = (*i.rangeSet)[i.rangeIdx]

		i.cur = r.RangeStart
	} else {
		i.cur = nextIP
	}

	if i.startIP == nil {
		i.startIP = i.cur
	} else if i.cur.Equal(i.startIP) {
		// IF we've looped back to where we started, give up
		return nil, nil
	}

	if r.Gateway != nil && i.cur.Equal(r.Gateway) {
		return i.Next()
	}

	return &net.IPNet{IP: i.cur, Mask: r.Subnet.Mask}, r.Gateway
}

// StartIP returns start IP of the current range
func (i *RangeIter) StartIP() net.IP {
	return i.startIP
}
