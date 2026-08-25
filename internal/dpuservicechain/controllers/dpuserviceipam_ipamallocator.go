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

package controllers

import (
	"fmt"
	"math/big"
	"net/netip"
	"slices"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/internal/dpuservicechain/utils/iputils"
	nvipamv1 "github.com/nvidia/doca-platform/third_party/api/nvipam/api/v1alpha1"
)

type blockIndexRange struct {
	Start *big.Int
	End   *big.Int
}

func newBlockIndexRange(start, end *big.Int) blockIndexRange {
	return blockIndexRange{Start: new(big.Int).Set(start), End: new(big.Int).Set(end)}
}

// MultiDPUClusterExclusionCalculator computes per-DPUCluster exclusion ranges without enumerating the address space.
type MultiDPUClusterExclusionCalculator struct {
	availableBlockRanges       []blockIndexRange
	excludedBlockRanges        []blockIndexRange
	mergedExclusions           []iputils.IPRange
	targetBlocksPerDPUCluster  int
	allocateAllRemainingBlocks bool
	parentRange                iputils.IPRange
	effectiveStart             netip.Addr
	effectiveEnd               netip.Addr
	blockSizePerNode           *big.Int
	totalBlocks                *big.Int
	staticAllocations          map[string]string
}

// NewMultiDPUClusterExclusionCalculatorForIPPool creates a calculator from the deprecated IPV4Subnet type alias.
func NewMultiDPUClusterExclusionCalculatorForIPPool(
	spec *dpuservicev1.Subnet,
	currentDPUClusterAllocations [][]dpuservicev1.IPRange,
) (*MultiDPUClusterExclusionCalculator, error) {
	return NewMultiDPUClusterExclusionCalculatorForSubnet(spec, currentDPUClusterAllocations)
}

// NewMultiDPUClusterExclusionCalculatorForSubnet creates a calculator from a shared-subnet spec.
func NewMultiDPUClusterExclusionCalculatorForSubnet(
	spec *dpuservicev1.Subnet,
	currentDPUClusterAllocations [][]dpuservicev1.IPRange,
) (*MultiDPUClusterExclusionCalculator, error) {
	return newMultiDPUClusterExclusionCalculator(
		spec.Subnet, big.NewInt(int64(spec.PerNodeIPCount)), spec.BlocksPerDPUCluster, false,
		spec.ExcludeRanges, currentDPUClusterAllocations, nil,
	)
}

// NewMultiDPUClusterExclusionCalculatorForCIDRPool creates a calculator from the deprecated IPV4Network type alias.
func NewMultiDPUClusterExclusionCalculatorForCIDRPool(
	spec *dpuservicev1.Network,
	currentDPUClusterAllocations [][]dpuservicev1.IPRange,
) (*MultiDPUClusterExclusionCalculator, error) {
	return NewMultiDPUClusterExclusionCalculatorForNetwork(spec, currentDPUClusterAllocations)
}

// NewMultiDPUClusterExclusionCalculatorForNetwork creates a calculator from a network-per-node spec.
//
//nolint:staticcheck // SA1019: Exclusions remains supported for backward compatibility.
func NewMultiDPUClusterExclusionCalculatorForNetwork(
	spec *dpuservicev1.Network,
	currentDPUClusterAllocations [][]dpuservicev1.IPRange,
) (*MultiDPUClusterExclusionCalculator, error) {
	prefix, err := netip.ParsePrefix(spec.Network)
	if err != nil {
		return nil, fmt.Errorf("invalid network %q: %w", spec.Network, err)
	}
	if prefix.Addr().Is4In6() {
		return nil, fmt.Errorf("IPv4-mapped IPv6 network %q is not supported", spec.Network)
	}
	excludeRanges := make([]dpuservicev1.IPRange, 0, len(spec.Exclusions)+len(spec.ExcludeRanges))
	for _, address := range spec.Exclusions {
		excludeRanges = append(excludeRanges, dpuservicev1.IPRange{StartIP: address, EndIP: address})
	}
	excludeRanges = append(excludeRanges, spec.ExcludeRanges...)
	return newMultiDPUClusterExclusionCalculator(
		spec.Network, iputils.PrefixAddressCount(prefix.Addr().BitLen(), int(spec.PrefixSize)),
		spec.SubnetsPerDPUCluster, true, excludeRanges, currentDPUClusterAllocations, spec.Allocations,
	)
}

func newMultiDPUClusterExclusionCalculator(
	network string,
	blockSizePerNode *big.Int,
	targetBlocksPerDPUCluster *int32,
	isCIDRPool bool,
	excludeRanges []dpuservicev1.IPRange,
	currentDPUClusterAllocations [][]dpuservicev1.IPRange,
	staticAllocations map[string]string,
) (*MultiDPUClusterExclusionCalculator, error) {
	prefix, err := netip.ParsePrefix(network)
	if err != nil {
		return nil, fmt.Errorf("invalid network %q: %w", network, err)
	}
	if prefix.Addr().Is4In6() {
		return nil, fmt.Errorf("IPv4-mapped IPv6 network %q is not supported", network)
	}
	prefix = prefix.Masked()
	parentRange, err := iputils.RangeFromPrefix(prefix)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate address range for network %s: %w", prefix, err)
	}
	parentSize := iputils.PrefixAddressCount(prefix.Addr().BitLen(), prefix.Bits())
	if blockSizePerNode == nil || blockSizePerNode.Sign() <= 0 {
		return nil, fmt.Errorf("per-node block size must be positive")
	}

	startOffset, endOffset := poolBoundaryOffsets(prefix, isCIDRPool)
	effectiveSize := new(big.Int).Sub(parentSize, new(big.Int).Add(startOffset, endOffset))
	if effectiveSize.Sign() <= 0 {
		return nil, fmt.Errorf("network %s contains no allocatable addresses", prefix)
	}
	totalBlocks := new(big.Int).Quo(effectiveSize, blockSizePerNode)
	if totalBlocks.Sign() <= 0 {
		return nil, fmt.Errorf("network %s cannot fit a per-node block of %s addresses", prefix, blockSizePerNode)
	}

	effectiveStart, ok := iputils.Add(parentRange.Start, startOffset)
	if !ok {
		return nil, fmt.Errorf("failed to calculate first allocatable address in %s", prefix)
	}
	effectiveEndDistance := new(big.Int).Sub(new(big.Int).Mul(totalBlocks, blockSizePerNode), big.NewInt(1))
	effectiveEnd, ok := iputils.Add(effectiveStart, effectiveEndDistance)
	if !ok {
		return nil, fmt.Errorf("failed to calculate last allocatable address in %s", prefix)
	}

	mergedExclusions, err := parseAndMergeIPRanges(excludeRanges)
	if err != nil {
		return nil, err
	}
	excludedBlockRanges := blockRangesFullyCoveredBy(mergedExclusions, effectiveStart, effectiveEnd, blockSizePerNode, totalBlocks)

	var existingRanges []iputils.IPRange
	for _, allocations := range currentDPUClusterAllocations {
		parsed, err := parseIPRanges(allocations)
		if err != nil {
			return nil, err
		}
		existingRanges = append(existingRanges, parsed...)
	}
	preTaken := blockRangesOverlapping(existingRanges, effectiveStart, effectiveEnd, blockSizePerNode, totalBlocks)
	unavailable := mergeBlockIndexRanges(append(slices.Clone(excludedBlockRanges), preTaken...))
	available := subtractBlockIndexRanges(
		[]blockIndexRange{newBlockIndexRange(big.NewInt(0), new(big.Int).Sub(totalBlocks, big.NewInt(1)))},
		unavailable,
	)

	calculator := &MultiDPUClusterExclusionCalculator{
		availableBlockRanges:       available,
		excludedBlockRanges:        excludedBlockRanges,
		mergedExclusions:           mergedExclusions,
		allocateAllRemainingBlocks: targetBlocksPerDPUCluster == nil,
		parentRange:                parentRange,
		effectiveStart:             effectiveStart,
		effectiveEnd:               effectiveEnd,
		blockSizePerNode:           new(big.Int).Set(blockSizePerNode),
		totalBlocks:                new(big.Int).Set(totalBlocks),
		staticAllocations:          staticAllocations,
	}
	if targetBlocksPerDPUCluster != nil {
		calculator.targetBlocksPerDPUCluster = int(*targetBlocksPerDPUCluster)
	}
	return calculator, nil
}

func poolBoundaryOffsets(prefix netip.Prefix, isCIDRPool bool) (*big.Int, *big.Int) {
	if isCIDRPool || prefix.Bits() >= prefix.Addr().BitLen()-1 {
		return big.NewInt(0), big.NewInt(0)
	}
	if prefix.Addr().Is4() {
		// Preserve the historical IPv4 shared-subnet behavior: ordinary prefixes exclude both the network and
		// broadcast addresses. Changing these offsets would move every subsequently calculated per-cluster block.
		return big.NewInt(1), big.NewInt(1)
	}
	// NVIPAM reserves the IPv6 subnet address for ordinary prefixes but has no IPv6 broadcast address.
	// The early return above matches how upstream treats /127 and /128 as exceptions, but validation
	// rejects those prefixes in shared-subnet mode because their offsets differ from every wider prefix.
	// Keep this aligned with upstream api/v1alpha1/helpers.go:GetPossibleIPCount and pkg/ip/cidr.go:IsBroadcast.
	return big.NewInt(1), big.NewInt(0)
}

// AllocateClusterBlocks assigns up to the configured number of complete blocks, or every remaining block when no
// per-cluster quota is configured. Returning the final partial quota preserves the existing IPv4 allocation semantics.
func (m *MultiDPUClusterExclusionCalculator) AllocateClusterBlocks(existingClusterBlocks []dpuservicev1.IPRange) ([]dpuservicev1.IPRange, error) {
	parsed, err := parseIPRanges(existingClusterBlocks)
	if err != nil {
		return nil, err
	}
	blocks, err := m.computeBlocksForCluster(parsed)
	if err != nil {
		return nil, err
	}
	return toDPUServiceIPAMIPRanges(iputils.MergeRanges(blocks)), nil
}

// ComputeExclusions derives all addresses in the parent network not owned by this cluster, plus user exclusions.
func (m *MultiDPUClusterExclusionCalculator) ComputeExclusions(clusterBlocks []dpuservicev1.IPRange) ([]nvipamv1.ExcludeRange, error) {
	allocs, err := parseIPRanges(clusterBlocks)
	if err != nil {
		return nil, fmt.Errorf("failed to parse DPUCluster allocation: %w", err)
	}
	partitionExclusions := iputils.ExcludeRanges(m.parentRange, iputils.MergeRanges(allocs))
	partitionExclusions, err = subtractStaticAllocations(partitionExclusions, m.staticAllocations)
	if err != nil {
		return nil, err
	}
	return toNVIPAMExcludeRanges(iputils.MergeRanges(append(partitionExclusions, m.mergedExclusions...))), nil
}

func subtractStaticAllocations(exclusions []iputils.IPRange, staticAllocations map[string]string) ([]iputils.IPRange, error) {
	for _, prefixString := range staticAllocations {
		prefix, err := netip.ParsePrefix(prefixString)
		if err != nil {
			continue
		}
		if prefix.Addr().Is4In6() {
			return nil, fmt.Errorf("IPv4-mapped IPv6 static allocation %q is not supported", prefixString)
		}
		allocationRange, err := iputils.RangeFromPrefix(prefix)
		if err != nil {
			return nil, fmt.Errorf("failed to calculate static allocation range %q: %w", prefixString, err)
		}
		exclusions = iputils.SubtractRange(exclusions, allocationRange)
	}
	return exclusions, nil
}

func (m *MultiDPUClusterExclusionCalculator) areClusterBlocksCompatible(clusterBlocks []iputils.IPRange) bool {
	return !slices.ContainsFunc(clusterBlocks, func(block iputils.IPRange) bool { return !block.In(m.parentRange) })
}

func (m *MultiDPUClusterExclusionCalculator) computeBlocksForCluster(existing []iputils.IPRange) ([]iputils.IPRange, error) {
	if len(existing) > 0 && m.areClusterBlocksCompatible(existing) {
		count := m.countAllocatableBlocks(existing)
		if count.Sign() > 0 {
			if m.allocateAllRemainingBlocks {
				additional, err := m.consumeAllBlocks()
				if err != nil {
					return existing, nil
				}
				return append(slices.Clone(existing), additional...), nil
			}
			target := big.NewInt(int64(m.targetBlocksPerDPUCluster))
			if count.Cmp(target) >= 0 {
				return existing, nil
			}
			deficit := new(big.Int).Sub(target, count)
			additional, err := m.consumeBlocks(int(deficit.Int64()))
			if err != nil {
				// Preserve an existing allocation if no additional capacity remains. This matches the historical IPv4
				// update behavior and avoids invalidating already-rendered pools during upgrade.
				return existing, nil
			}
			return append(slices.Clone(existing), additional...), nil
		}
	}

	if m.allocateAllRemainingBlocks {
		return m.consumeAllBlocks()
	}
	return m.consumeBlocks(m.targetBlocksPerDPUCluster)
}

// countAllocatableBlocks counts complete per-node blocks relative to each persisted allocation range. A parent subnet
// expansion can shift the current block grid, so counting the new grid blocks touched by an old allocation can
// overstate its capacity. The current grid is still used when reserving address space and assigning new blocks.
func (m *MultiDPUClusterExclusionCalculator) countAllocatableBlocks(ranges []iputils.IPRange) *big.Int {
	total := big.NewInt(0)
	for _, allocated := range iputils.MergeRanges(ranges) {
		distance, ok := iputils.Distance(allocated.Start, allocated.End)
		if !ok {
			continue
		}
		addressCount := new(big.Int).Add(distance, big.NewInt(1))
		blockCount := new(big.Int).Quo(addressCount, m.blockSizePerNode)
		if blockCount.Sign() == 0 {
			continue
		}

		completeEndOffset := new(big.Int).Sub(new(big.Int).Mul(blockCount, m.blockSizePerNode), big.NewInt(1))
		completeEnd, ok := iputils.Add(allocated.Start, completeEndOffset)
		if !ok {
			continue
		}
		excluded := blockRangesFullyCoveredBy(
			m.mergedExclusions,
			allocated.Start,
			completeEnd,
			m.blockSizePerNode,
			blockCount,
		)
		total.Add(total, blockCount)
		total.Sub(total, blockIndexRangeCount(excluded))
	}
	return total
}

func (m *MultiDPUClusterExclusionCalculator) consumeBlocks(count int) ([]iputils.IPRange, error) {
	if count < 1 {
		return nil, fmt.Errorf("target blocks per DPUCluster must be at least 1")
	}
	selected, remaining := takeBlockIndices(m.availableBlockRanges, big.NewInt(int64(count)))
	selectedCount := blockIndexRangeCount(selected)
	if selectedCount.Sign() == 0 {
		return nil, fmt.Errorf("no allocatable blocks remaining")
	}
	m.availableBlockRanges = remaining
	return m.blockIndicesToIPRanges(selected)
}

func (m *MultiDPUClusterExclusionCalculator) consumeAllBlocks() ([]iputils.IPRange, error) {
	if len(m.availableBlockRanges) == 0 {
		return nil, fmt.Errorf("no allocatable blocks remaining")
	}
	selected := m.availableBlockRanges
	m.availableBlockRanges = nil
	return m.blockIndicesToIPRanges(selected)
}

func (m *MultiDPUClusterExclusionCalculator) blockIndicesToIPRanges(indices []blockIndexRange) ([]iputils.IPRange, error) {
	result := make([]iputils.IPRange, 0, len(indices))
	for _, r := range indices {
		startOffset := new(big.Int).Mul(r.Start, m.blockSizePerNode)
		endOffset := new(big.Int).Sub(new(big.Int).Mul(new(big.Int).Add(r.End, big.NewInt(1)), m.blockSizePerNode), big.NewInt(1))
		start, startOK := iputils.Add(m.effectiveStart, startOffset)
		end, endOK := iputils.Add(m.effectiveStart, endOffset)
		if !startOK || !endOK {
			return nil, fmt.Errorf("block index range %s-%s overflows the allocatable address range", r.Start, r.End)
		}
		result = append(result, iputils.IPRange{Start: start, End: end})
	}
	return result, nil
}

func parseIPRanges(ranges []dpuservicev1.IPRange) ([]iputils.IPRange, error) {
	if len(ranges) == 0 {
		return nil, nil
	}
	result := make([]iputils.IPRange, 0, len(ranges))
	for _, r := range ranges {
		start, err := iputils.ParseAddr(r.StartIP)
		if err != nil {
			return nil, fmt.Errorf("invalid range start %q: %w", r.StartIP, err)
		}
		end, err := iputils.ParseAddr(r.EndIP)
		if err != nil {
			return nil, fmt.Errorf("invalid range end %q: %w", r.EndIP, err)
		}
		if start.BitLen() != end.BitLen() || start.Compare(end) > 0 {
			return nil, fmt.Errorf("invalid range %s-%s", start, end)
		}
		result = append(result, iputils.IPRange{Start: start, End: end})
	}
	return result, nil
}

func parseAndMergeIPRanges(ranges []dpuservicev1.IPRange) ([]iputils.IPRange, error) {
	parsed, err := parseIPRanges(ranges)
	if err != nil {
		return nil, err
	}
	return iputils.MergeRanges(parsed), nil
}

func toDPUServiceIPAMIPRanges(ranges []iputils.IPRange) []dpuservicev1.IPRange {
	result := make([]dpuservicev1.IPRange, 0, len(ranges))
	for _, r := range ranges {
		result = append(result, dpuservicev1.IPRange{StartIP: r.Start.String(), EndIP: r.End.String()})
	}
	return result
}

func toNVIPAMExcludeRanges(ranges []iputils.IPRange) []nvipamv1.ExcludeRange {
	if len(ranges) == 0 {
		return nil
	}
	// Render absolute address ranges rather than NV-IPAM perNodeExclusions. The latter uses int32 indexes and cannot
	// represent offsets beyond math.MaxInt32 within a large IPv6 allocation.
	result := make([]nvipamv1.ExcludeRange, 0, len(ranges))
	for _, r := range ranges {
		result = append(result, nvipamv1.ExcludeRange{StartIP: r.Start.String(), EndIP: r.End.String()})
	}
	return result
}

func blockRangesFullyCoveredBy(ranges []iputils.IPRange, effectiveStart, effectiveEnd netip.Addr, blockSize, totalBlocks *big.Int) []blockIndexRange {
	result := make([]blockIndexRange, 0, len(ranges))
	for _, r := range ranges {
		if r.Start.BitLen() != effectiveStart.BitLen() || r.End.Compare(effectiveStart) < 0 || r.Start.Compare(effectiveEnd) > 0 {
			continue
		}
		start := r.Start
		if start.Compare(effectiveStart) < 0 {
			start = effectiveStart
		}
		end := r.End
		if end.Compare(effectiveEnd) > 0 {
			end = effectiveEnd
		}
		startDistance, _ := iputils.Distance(effectiveStart, start)
		endDistance, _ := iputils.Distance(effectiveStart, end)
		first := ceilDiv(startDistance, blockSize)
		if new(big.Int).Add(endDistance, big.NewInt(1)).Cmp(blockSize) < 0 {
			continue
		}
		last := new(big.Int).Quo(new(big.Int).Sub(new(big.Int).Add(endDistance, big.NewInt(1)), blockSize), blockSize)
		if first.Cmp(last) <= 0 && first.Cmp(totalBlocks) < 0 {
			if last.Cmp(totalBlocks) >= 0 {
				last.Sub(totalBlocks, big.NewInt(1))
			}
			result = append(result, newBlockIndexRange(first, last))
		}
	}
	return mergeBlockIndexRanges(result)
}

func blockRangesOverlapping(ranges []iputils.IPRange, effectiveStart, effectiveEnd netip.Addr, blockSize, totalBlocks *big.Int) []blockIndexRange {
	result := make([]blockIndexRange, 0, len(ranges))
	for _, r := range ranges {
		if r.Start.BitLen() != effectiveStart.BitLen() || r.End.Compare(effectiveStart) < 0 || r.Start.Compare(effectiveEnd) > 0 {
			continue
		}
		start := r.Start
		if start.Compare(effectiveStart) < 0 {
			start = effectiveStart
		}
		end := r.End
		if end.Compare(effectiveEnd) > 0 {
			end = effectiveEnd
		}
		startDistance, _ := iputils.Distance(effectiveStart, start)
		endDistance, _ := iputils.Distance(effectiveStart, end)
		first := new(big.Int).Quo(startDistance, blockSize)
		last := new(big.Int).Quo(endDistance, blockSize)
		if first.Cmp(totalBlocks) < 0 {
			if last.Cmp(totalBlocks) >= 0 {
				last.Sub(totalBlocks, big.NewInt(1))
			}
			result = append(result, newBlockIndexRange(first, last))
		}
	}
	return mergeBlockIndexRanges(result)
}

func ceilDiv(value, divisor *big.Int) *big.Int {
	if value.Sign() == 0 {
		return big.NewInt(0)
	}
	return new(big.Int).Quo(new(big.Int).Add(value, new(big.Int).Sub(divisor, big.NewInt(1))), divisor)
}

func mergeBlockIndexRanges(ranges []blockIndexRange) []blockIndexRange {
	if len(ranges) == 0 {
		return nil
	}
	slices.SortFunc(ranges, func(a, b blockIndexRange) int { return a.Start.Cmp(b.Start) })
	merged := []blockIndexRange{newBlockIndexRange(ranges[0].Start, ranges[0].End)}
	for _, r := range ranges[1:] {
		last := &merged[len(merged)-1]
		if r.Start.Cmp(new(big.Int).Add(last.End, big.NewInt(1))) <= 0 {
			if r.End.Cmp(last.End) > 0 {
				last.End.Set(r.End)
			}
		} else {
			merged = append(merged, newBlockIndexRange(r.Start, r.End))
		}
	}
	return merged
}

func subtractBlockIndexRanges(ranges, subtract []blockIndexRange) []blockIndexRange {
	result := mergeBlockIndexRanges(ranges)
	for _, sub := range mergeBlockIndexRanges(subtract) {
		next := make([]blockIndexRange, 0, len(result)+1)
		for _, current := range result {
			if sub.End.Cmp(current.Start) < 0 || sub.Start.Cmp(current.End) > 0 {
				next = append(next, newBlockIndexRange(current.Start, current.End))
				continue
			}
			if sub.Start.Cmp(current.Start) > 0 {
				next = append(next, newBlockIndexRange(current.Start, new(big.Int).Sub(sub.Start, big.NewInt(1))))
			}
			if sub.End.Cmp(current.End) < 0 {
				next = append(next, newBlockIndexRange(new(big.Int).Add(sub.End, big.NewInt(1)), current.End))
			}
		}
		result = next
	}
	return result
}

func takeBlockIndices(ranges []blockIndexRange, count *big.Int) (selected, remaining []blockIndexRange) {
	left := new(big.Int).Set(count)
	for _, r := range ranges {
		if left.Sign() == 0 {
			remaining = append(remaining, newBlockIndexRange(r.Start, r.End))
			continue
		}
		rangeCount := new(big.Int).Add(new(big.Int).Sub(r.End, r.Start), big.NewInt(1))
		if rangeCount.Cmp(left) <= 0 {
			selected = append(selected, newBlockIndexRange(r.Start, r.End))
			left.Sub(left, rangeCount)
			continue
		}
		selectedEnd := new(big.Int).Sub(new(big.Int).Add(r.Start, left), big.NewInt(1))
		selected = append(selected, newBlockIndexRange(r.Start, selectedEnd))
		remaining = append(remaining, newBlockIndexRange(new(big.Int).Add(selectedEnd, big.NewInt(1)), r.End))
		left.SetInt64(0)
	}
	return selected, remaining
}

func blockIndexRangeCount(ranges []blockIndexRange) *big.Int {
	total := big.NewInt(0)
	for _, r := range ranges {
		total.Add(total, new(big.Int).Add(new(big.Int).Sub(r.End, r.Start), big.NewInt(1)))
	}
	return total
}
