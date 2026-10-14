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

// This package is based on https://github.com/Mellanox/nvidia-k8s-ipam/blob/v0.3.5//pkg/ipam-node/allocator/range.go
// modified to fit vpc needs.

import (
	"fmt"
	"net"
)

// Range contains IP range configuration
type Range struct {
	RangeStart net.IP    // The first ip, inclusive
	RangeEnd   net.IP    // The last ip, inclusive
	Subnet     net.IPNet // The subnet of the range
	Gateway    net.IP    // The gateway of the range
}

// Canonicalize takes a given range and ensures that all information is consistent,
// filling out Start, End, and Gateway with sane values if missing
func (r *Range) Canonicalize() error {
	if err := CanonicalizeIP(&r.Subnet.IP); err != nil {
		return err
	}

	if len(r.Subnet.IP) != len(r.Subnet.Mask) {
		return fmt.Errorf("IPNet IP and Mask version mismatch")
	}

	// Ensure Subnet IP is the network address, not some other address
	networkIP := r.Subnet.IP.Mask(r.Subnet.Mask)
	if !r.Subnet.IP.Equal(networkIP) {
		return fmt.Errorf("network has host bits set. "+
			"Expected subnet address is %s", networkIP.String())
	}

	// validate Gateway only if set
	if r.Gateway != nil {
		if err := CanonicalizeIP(&r.Gateway); err != nil {
			return err
		}
	}

	// RangeStart: If specified, make sure it's sane (inside the subnet),
	// otherwise use the first free IP (i.e. .1) - this will conflict with the
	// gateway but we skip it in the iterator
	if r.RangeStart != nil {
		if err := CanonicalizeIP(&r.RangeStart); err != nil {
			return err
		}

		if !r.Contains(r.RangeStart) {
			return fmt.Errorf("RangeStart %s not in network %s", r.RangeStart.String(), r.Subnet.String())
		}
	} else {
		if IsPointToPointSubnet(&r.Subnet) || IsSingleIPSubnet(&r.Subnet) {
			r.RangeStart = r.Subnet.IP
		} else {
			r.RangeStart = NextIP(r.Subnet.IP)
			if r.RangeStart == nil {
				return fmt.Errorf("computed RangeStart is not a valid IP")
			}
		}
	}

	// RangeEnd: If specified, verify sanity. Otherwise, add a sensible default
	// (e.g. for a /24: .254 if IPv4, ::255 if IPv6)
	if r.RangeEnd != nil {
		if err := CanonicalizeIP(&r.RangeEnd); err != nil {
			return err
		}

		if !r.Contains(r.RangeEnd) {
			return fmt.Errorf("RangeEnd %s not in network %s", r.RangeEnd.String(), r.Subnet.String())
		}
	} else {
		r.RangeEnd = LastIP(&r.Subnet)
	}

	return nil
}

// Contains checks if a given ip is a valid, allocatable address in a given Range
func (r *Range) Contains(addr net.IP) bool {
	if err := CanonicalizeIP(&addr); err != nil {
		return false
	}

	subnet := r.Subnet

	// Not the same address family
	if len(addr) != len(r.Subnet.IP) {
		return false
	}

	// Not in network
	if !subnet.Contains(addr) {
		return false
	}

	if Cmp(addr, r.RangeStart) < 0 {
		// Before the range start
		return false
	}

	if Cmp(addr, r.RangeEnd) > 0 {
		// After the  range end
		return false
	}

	return true
}

// Overlaps returns true if there is any overlap between ranges
func (r *Range) Overlaps(r1 *Range) bool {
	// different families
	if len(r.RangeStart) != len(r1.RangeStart) {
		return false
	}

	return r.Contains(r1.RangeStart) ||
		r.Contains(r1.RangeEnd) ||
		r1.Contains(r.RangeStart) ||
		r1.Contains(r.RangeEnd)
}

// String returns string representation of the Range
func (r *Range) String() string {
	return fmt.Sprintf("%s-%s", r.RangeStart.String(), r.RangeEnd.String())
}

// CanonicalizeIP makes sure a provided ip is in standard form
func CanonicalizeIP(addr *net.IP) error {
	if addr == nil {
		return fmt.Errorf("IP can't be nil")
	}
	normalizedIP := NormalizeIP(*addr)
	if normalizedIP == nil {
		return fmt.Errorf("IP %s not v4 nor v6", *addr)
	}
	*addr = normalizedIP
	return nil
}
