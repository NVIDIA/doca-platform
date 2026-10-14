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

// This package is based on https://github.com/Mellanox/nvidia-k8s-ipam/blob/v0.3.5//pkg/ipam-node/allocator/range_set.go
// modified to fit vpc needs.

import (
	"fmt"
	"net"
	"strings"
)

// RangeSet is an alias type for list or Range objects
type RangeSet []Range

// Contains returns true if any range in this set contains an IP
func (s *RangeSet) Contains(addr net.IP) bool {
	r, _ := s.RangeFor(addr)
	return r != nil
}

// RangeFor finds the range that contains an IP, or nil if not found
func (s *RangeSet) RangeFor(addr net.IP) (*Range, error) {
	if err := CanonicalizeIP(&addr); err != nil {
		return nil, err
	}

	for _, r := range *s {
		if r.Contains(addr) {
			return &r, nil
		}
	}

	return nil, fmt.Errorf("%s not in range set %s", addr.String(), s.String())
}

// Overlaps returns true if any ranges in any set overlap with this one
func (s *RangeSet) Overlaps(p1 *RangeSet) bool {
	for _, r := range *s {
		for _, r1 := range *p1 {
			if r.Overlaps(&r1) {
				return true
			}
		}
	}
	return false
}

// Canonicalize ensures the RangeSet is in a standard form, and detects any
// invalid input. Call Range.Canonicalize() on every Range in the set
func (s *RangeSet) Canonicalize() error {
	if len(*s) == 0 {
		return fmt.Errorf("empty range set")
	}

	fam := 0
	for i := range *s {
		if err := (*s)[i].Canonicalize(); err != nil {
			return err
		}
		if i == 0 {
			fam = len((*s)[i].RangeStart)
		} else if fam != len((*s)[i].RangeStart) {
			return fmt.Errorf("mixed address families")
		}
	}

	// Make sure none of the ranges in the set overlap
	l := len(*s)
	for i, r1 := range (*s)[:l-1] {
		for _, r2 := range (*s)[i+1:] {
			if r1.Overlaps(&r2) {
				return fmt.Errorf("subnets %s and %s overlap", r1.String(), r2.String())
			}
		}
	}

	return nil
}

// String returns string representation of the RangeSet
func (s *RangeSet) String() string {
	out := []string{}
	for _, r := range *s {
		out = append(out, r.String())
	}

	return strings.Join(out, ",")
}
