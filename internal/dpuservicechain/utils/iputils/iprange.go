/*
Copyright 2026 NVIDIA

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package iputils

import (
	"encoding/binary"
	"net"
	"slices"
	"sort"
)

// IPRange is the internal representation of a contiguous IPv4 address range.
type IPRange struct {
	Start uint32
	End   uint32
}

// In reports whether r is fully contained within other.
func (r IPRange) In(other IPRange) bool {
	return r.Start >= other.Start && r.End <= other.End
}

// Overlaps reports whether r shares at least one IP with other.
func (r IPRange) Overlaps(other IPRange) bool {
	return r.Start <= other.End && r.End >= other.Start
}

// SortRanges returns a copy of the ranges sorted by start address.
func SortRanges(ranges []IPRange) []IPRange {
	sorted := make([]IPRange, len(ranges))
	copy(sorted, ranges)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Start < sorted[j].Start
	})
	return sorted
}

// PrefixSize returns the number of IPv4 addresses in a prefix of the given length (e.g. /24 → 256).
func PrefixSize(prefixLen int) uint32 {
	return 1 << uint(32-prefixLen)
}

// IPv4ToUint32 converts a net.IP to a uint32.
func IPv4ToUint32(ip net.IP) uint32 {
	return binary.BigEndian.Uint32(ip.To4())
}

// Uint32ToIPv4Str converts a uint32 to an IPv4 address string.
func Uint32ToIPv4Str(n uint32) string {
	b := make(net.IP, 4)
	binary.BigEndian.PutUint32(b, n)
	return b.String()
}

// InAny reports whether r is fully contained within any of the given ranges.
func (r IPRange) InAny(ranges []IPRange) bool {
	return slices.ContainsFunc(ranges, r.In)
}

// MergeRanges merges overlapping/adjacent IP ranges into a minimal set.
func MergeRanges(ranges []IPRange) []IPRange {
	if len(ranges) == 0 {
		return nil
	}
	sorted := SortRanges(ranges)
	merged := []IPRange{sorted[0]}
	for _, r := range sorted[1:] {
		last := &merged[len(merged)-1]
		if r.Start <= last.End+1 {
			if r.End > last.End {
				last.End = r.End
			}
		} else {
			merged = append(merged, r)
		}
	}
	return merged
}

// ExcludeRanges returns all sub-ranges of parent that are not covered by any range in allocated.
// The result is sorted and non-overlapping. Returns nil when parent is fully covered.
func ExcludeRanges(parent IPRange, allocated []IPRange) []IPRange {
	remaining := []IPRange{parent}
	for _, a := range allocated {
		remaining = SubtractRange(remaining, a)
		if len(remaining) == 0 {
			return nil
		}
	}
	return remaining
}

// SubtractRange returns ranges with sub removed, splitting any overlapping range around it.
// Ranges with no overlap are kept unchanged. Returns nil when the result is empty.
func SubtractRange(ranges []IPRange, sub IPRange) []IPRange {
	result := make([]IPRange, 0, len(ranges)+1)
	for _, e := range ranges {
		if !e.Overlaps(sub) {
			result = append(result, e)
			continue
		}
		if sub.Start > e.Start {
			result = append(result, IPRange{Start: e.Start, End: sub.Start - 1})
		}
		if sub.End < e.End {
			result = append(result, IPRange{Start: sub.End + 1, End: e.End})
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
