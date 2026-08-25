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
	"fmt"
	"math/big"
	"net/netip"
	"slices"
)

// IPRange is the internal representation of a contiguous IPv4 or IPv6 address range.
// Start and End must always belong to the same address family.
type IPRange struct {
	Start netip.Addr
	End   netip.Addr
}

// ParseAddr parses an IPv4 or IPv6 address. IPv4-mapped IPv6 representations are rejected so callers
// cannot accidentally change address families through netip.Addr.Unmap.
func ParseAddr(s string) (netip.Addr, error) {
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}, err
	}
	if addr.Is4In6() {
		return netip.Addr{}, fmt.Errorf("IPv4-mapped IPv6 address %q is not supported", s)
	}
	return addr, nil
}

// RangeFromPrefix returns the inclusive address range covered by prefix.
func RangeFromPrefix(prefix netip.Prefix) (IPRange, error) {
	if !prefix.IsValid() {
		return IPRange{}, fmt.Errorf("invalid IP prefix")
	}
	prefix = prefix.Masked()
	size := PrefixAddressCount(prefix.Addr().BitLen(), prefix.Bits())
	end, ok := Add(prefix.Addr(), new(big.Int).Sub(size, big.NewInt(1)))
	if !ok {
		return IPRange{}, fmt.Errorf("prefix %s overflows its address family", prefix)
	}
	return IPRange{Start: prefix.Addr(), End: end}, nil
}

// PrefixAddressCount returns the number of IP addresses in a prefix.
func PrefixAddressCount(addressBits, prefixLen int) *big.Int {
	if prefixLen < 0 || prefixLen > addressBits {
		return big.NewInt(0)
	}
	return new(big.Int).Lsh(big.NewInt(1), uint(addressBits-prefixLen))
}

// Add adds a non-negative offset to an address. ok is false on invalid input or family overflow.
func Add(addr netip.Addr, offset *big.Int) (result netip.Addr, ok bool) {
	if !addr.IsValid() || offset == nil || offset.Sign() < 0 {
		return netip.Addr{}, false
	}
	bits := addr.BitLen()
	value := addrToInt(addr)
	value.Add(value, offset)
	limit := new(big.Int).Lsh(big.NewInt(1), uint(bits))
	if value.Cmp(limit) >= 0 {
		return netip.Addr{}, false
	}
	return intToAddr(value, bits)
}

// Distance returns end-start. ok is false for mixed families or end before start.
func Distance(start, end netip.Addr) (distance *big.Int, ok bool) {
	if !start.IsValid() || !end.IsValid() || start.BitLen() != end.BitLen() || start.Compare(end) > 0 {
		return nil, false
	}
	return new(big.Int).Sub(addrToInt(end), addrToInt(start)), true
}

func addrToInt(addr netip.Addr) *big.Int {
	if addr.Is4() {
		v := addr.As4()
		return new(big.Int).SetBytes(v[:])
	}
	v := addr.As16()
	return new(big.Int).SetBytes(v[:])
}

func intToAddr(value *big.Int, bits int) (netip.Addr, bool) {
	byteLen := bits / 8
	raw := value.Bytes()
	if len(raw) > byteLen {
		return netip.Addr{}, false
	}
	switch bits {
	case 32:
		var bytes [4]byte
		copy(bytes[len(bytes)-len(raw):], raw)
		return netip.AddrFrom4(bytes), true
	case 128:
		var bytes [16]byte
		copy(bytes[len(bytes)-len(raw):], raw)
		return netip.AddrFrom16(bytes), true
	default:
		return netip.Addr{}, false
	}
}

// In reports whether r is fully contained within other.
func (r IPRange) In(other IPRange) bool {
	return sameFamily(r, other) && r.Start.Compare(other.Start) >= 0 && r.End.Compare(other.End) <= 0
}

// Overlaps reports whether r shares at least one IP with other.
func (r IPRange) Overlaps(other IPRange) bool {
	return sameFamily(r, other) && r.Start.Compare(other.End) <= 0 && r.End.Compare(other.Start) >= 0
}

func sameFamily(a, b IPRange) bool {
	return a.Start.IsValid() && a.End.IsValid() && b.Start.IsValid() && b.End.IsValid() &&
		a.Start.BitLen() == a.End.BitLen() && b.Start.BitLen() == b.End.BitLen() &&
		a.Start.BitLen() == b.Start.BitLen()
}

// SortRanges returns a copy of the ranges sorted by family, start address, then end address.
func SortRanges(ranges []IPRange) []IPRange {
	sorted := slices.Clone(ranges)
	slices.SortFunc(sorted, func(a, b IPRange) int {
		if a.Start.BitLen() != b.Start.BitLen() {
			return a.Start.BitLen() - b.Start.BitLen()
		}
		if c := a.Start.Compare(b.Start); c != 0 {
			return c
		}
		return a.End.Compare(b.End)
	})
	return sorted
}

// InAny reports whether r is fully contained within any of the given ranges.
func (r IPRange) InAny(ranges []IPRange) bool {
	return slices.ContainsFunc(ranges, r.In)
}

// MergeRanges merges overlapping/adjacent IP ranges into a minimal set. Mixed families remain separate.
func MergeRanges(ranges []IPRange) []IPRange {
	if len(ranges) == 0 {
		return nil
	}
	sorted := SortRanges(ranges)
	merged := []IPRange{sorted[0]}
	for _, r := range sorted[1:] {
		last := &merged[len(merged)-1]
		adjacent := false
		if sameFamily(*last, r) {
			next := last.End.Next()
			adjacent = next.IsValid() && r.Start.Compare(next) <= 0
		}
		if last.Overlaps(r) || adjacent {
			if r.End.Compare(last.End) > 0 {
				last.End = r.End
			}
		} else {
			merged = append(merged, r)
		}
	}
	return merged
}

// ExcludeRanges returns all sub-ranges of parent that are not covered by any range in allocated.
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
func SubtractRange(ranges []IPRange, sub IPRange) []IPRange {
	result := make([]IPRange, 0, len(ranges)+1)
	for _, existing := range ranges {
		if !existing.Overlaps(sub) {
			result = append(result, existing)
			continue
		}
		if sub.Start.Compare(existing.Start) > 0 {
			end := sub.Start.Prev()
			if end.IsValid() {
				result = append(result, IPRange{Start: existing.Start, End: end})
			}
		}
		if sub.End.Compare(existing.End) < 0 {
			start := sub.End.Next()
			if start.IsValid() {
				result = append(result, IPRange{Start: start, End: existing.End})
			}
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
