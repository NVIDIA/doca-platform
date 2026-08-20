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
	"net"
	"slices"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/internal/dpuservicechain/utils/iputils"
	nvipamv1 "github.com/nvidia/doca-platform/third_party/api/nvipam/api/v1alpha1"

	"k8s.io/utils/ptr"
)

// MultiDPUClusterExclusionCalculator computes per-DPUCluster exclusion ranges so that the controller can allocate
// non-overlapping ranges per DPUCluster. It does so by calculating internally the block of IPs that should be assigned
// per node in each DPUCluster and tries to derive consecutive blocks of such blocks per DPUCluster, taking into account
// the user provided exclusions.
type MultiDPUClusterExclusionCalculator struct {
	// allocatableBlocks are the per-node allocation blocks that are allocatable.
	// Blocks fully covered by user excludeRanges and blocks pre-claimed by existing DPUClusters are excluded.
	allocatableBlocks []iputils.IPRange
	// allocatableBlocksPosition is the current position in allocatableBlocks. All blocks before this position are
	// already allocated. Used to track the index when calling Allocate().
	allocatableBlocksPosition int
	// mergedExclusions is the sorted, merged form of the user-provided excludeRanges.
	mergedExclusions []iputils.IPRange
	// targetBlocksPerDPUCluster is the number of per-node allocation blocks each DPUCluster should receive.
	targetBlocksPerDPUCluster int
	// parentRange and blockSizePerNode are kept to validate existing allocations on Allocate.
	parentRange      iputils.IPRange
	blockSizePerNode uint32
	// staticAllocations holds explicit per-node prefix assignments (node name → CIDR). Blocks covered by
	// these entries are carved out of the per-cluster exclusion ranges so that NVIPAM can honor a static
	// allocation even when its prefix falls outside this cluster's normally assigned range.
	staticAllocations map[string]string
}

// NewMultiDPUClusterExclusionCalculatorForIPPool creates a calculator from an IPV4Subnet spec.
func NewMultiDPUClusterExclusionCalculatorForIPPool(
	spec *dpuservicev1.Subnet,
	currentDPUClusterAllocations [][]dpuservicev1.IPRange,
) (*MultiDPUClusterExclusionCalculator, error) {
	return newMultiDPUClusterExclusionCalculator(
		spec.Subnet,
		uint32(spec.PerNodeIPCount),
		spec.BlocksPerDPUCluster,
		// This is always false because we block /31 and /32 subnets in validation, otherwise network and broadcast
		// address could be allocatable. If we ever enable those subnets, we need to take into account not impacting
		// existing allocations when moving from e.g. /31 to /29 as the blocks will be calculated with different offsets.
		false,
		spec.ExcludeRanges,
		currentDPUClusterAllocations,
		nil, // IPV4Subnet has no static per-node allocations
	)
}

// NewMultiDPUClusterExclusionCalculatorForCIDRPool creates a calculator from an IPV4Network spec.
//
//nolint:staticcheck // SA1019: Exclusions is deprecated but still supported
func NewMultiDPUClusterExclusionCalculatorForCIDRPool(
	spec *dpuservicev1.Network,
	currentDPUClusterAllocations [][]dpuservicev1.IPRange,
) (*MultiDPUClusterExclusionCalculator, error) {
	// blockSizePerNode: number of IPs in one /perNodePrefixSize block, e.g. /27 → 32.
	blockSizePerNode := iputils.PrefixSize(int(spec.PrefixSize))

	excludeRanges := make([]dpuservicev1.IPRange, 0, len(spec.Exclusions)+len(spec.ExcludeRanges))
	for _, ip := range spec.Exclusions {
		excludeRanges = append(excludeRanges, dpuservicev1.IPRange{StartIP: ip, EndIP: ip})
	}
	excludeRanges = append(excludeRanges, spec.ExcludeRanges...)

	return newMultiDPUClusterExclusionCalculator(
		spec.Network,
		blockSizePerNode,
		spec.SubnetsPerDPUCluster,
		true,
		excludeRanges,
		currentDPUClusterAllocations,
		spec.Allocations,
	)
}

// newMultiDPUClusterExclusionCalculator is the shared core constructor. Both public constructors normalise their
// spec-specific fields before calling this function.
func newMultiDPUClusterExclusionCalculator(
	network string,
	blockSizePerNode uint32,
	targetBlocksPerDPUCluster *int32,
	includeNetworkAndBroadcast bool,
	excludeRanges []dpuservicev1.IPRange,
	currentDPUClusterAllocations [][]dpuservicev1.IPRange,
	staticAllocations map[string]string,
) (*MultiDPUClusterExclusionCalculator, error) {
	_, parentNet, err := net.ParseCIDR(network)
	if err != nil {
		return nil, fmt.Errorf("invalid network %q: %w", network, err)
	}
	parentPrefixLen, _ := parentNet.Mask.Size()
	parentStartIP := iputils.IPv4ToUint32(parentNet.IP)

	// parentSize: number of IPs in the network, e.g. /22 → 1024.
	parentSize := iputils.PrefixSize(parentPrefixLen)

	var startOffset, endOffset uint32
	// For IPPool, we need to ensure that network and broadcast address is not used in the calculations otherwise we
	// we will provide less IPs to one of the clusters.
	if !includeNetworkAndBroadcast {
		startOffset, endOffset = 1, 1
	}
	effectiveNetworkSize := parentSize - startOffset - endOffset

	// totalBlocks is the number of IP blocks that fit in the allocatable range,
	// e.g. a /22 parent (1024 IPs) with blockSizePerNode=32 (/27) → 1024/32 = 32 blocks.
	totalBlocks := int(effectiveNetworkSize / blockSizePerNode)

	// nil means "give all available blocks to this cluster" — used when no per-cluster partitioning is configured and
	// the whole network should go to a single cluster.
	finalTargetBlocksPerDPUCluster := ptr.Deref(targetBlocksPerDPUCluster, int32(totalBlocks))

	allBlocks := make([]iputils.IPRange, totalBlocks)
	for i := range totalBlocks {
		s := parentStartIP + startOffset + uint32(i)*blockSizePerNode
		allBlocks[i] = iputils.IPRange{Start: s, End: s + blockSizePerNode - 1}
	}

	mergedExcludedRanges := iputils.MergeRanges(parseIPRanges(excludeRanges))

	// excludedBlocks tracks blocks fully covered by user exclusions.
	excludedBlocks := make([]bool, totalBlocks)
	for i, block := range allBlocks {
		excludedBlocks[i] = block.InAny(mergedExcludedRanges)
	}

	// preTaken marks blocks that overlap with existing DPUCluster allocations so the sequential pool never reclaims
	// already-assigned address space.
	preTaken := make([]bool, totalBlocks)
	for _, clusterAllocs := range currentDPUClusterAllocations {
		for _, clusterBlock := range parseIPRanges(clusterAllocs) {
			for i, block := range allBlocks {
				if block.Overlaps(clusterBlock) {
					preTaken[i] = true
				}
			}
		}
	}

	// allocatableBlocks contains only blocks available for allocation.
	allocatableBlocks := make([]iputils.IPRange, 0, totalBlocks)
	for i, b := range allBlocks {
		if !excludedBlocks[i] && !preTaken[i] {
			allocatableBlocks = append(allocatableBlocks, b)
		}
	}

	parentEndIP := parentStartIP + parentSize - 1
	return &MultiDPUClusterExclusionCalculator{
		allocatableBlocks:         allocatableBlocks,
		mergedExclusions:          mergedExcludedRanges,
		targetBlocksPerDPUCluster: int(finalTargetBlocksPerDPUCluster),
		parentRange:               iputils.IPRange{Start: parentStartIP, End: parentEndIP},
		blockSizePerNode:          blockSizePerNode,
		staticAllocations:         staticAllocations,
	}, nil
}

// AllocateClusterBlocks assigns blocks to the cluster and returns the cluster-blocks to persist in status.
// Pass nil for a new cluster. Advances the internal pool cursor; call once per cluster per calculator instance.
// Returns an error when no allocatable blocks remain.
func (m *MultiDPUClusterExclusionCalculator) AllocateClusterBlocks(existingClusterBlocks []dpuservicev1.IPRange) ([]dpuservicev1.IPRange, error) {
	blocks, err := m.computeBlocksForCluster(parseIPRanges(existingClusterBlocks))
	if err != nil {
		return nil, err
	}
	return toDPUServiceIPAMIPRanges(iputils.MergeRanges(blocks)), nil
}

// ComputeExclusions derives the exclusion ranges for the pool from the given cluster-blocks.
// The pool must use the parent Network/CIDR for the exclusions to be correct.
func (m *MultiDPUClusterExclusionCalculator) ComputeExclusions(clusterBlocks []dpuservicev1.IPRange) []nvipamv1.ExcludeRange {
	// Coalesce the raw IP ranges into contiguous cluster-blocks for processing.
	allocs := iputils.MergeRanges(parseIPRanges(clusterBlocks))

	// Derive the partition exclusions: all parent-network space not owned by this cluster
	// (before, after, and between its assigned cluster-blocks).
	partitionExclusions := iputils.ExcludeRanges(m.parentRange, allocs)

	// Carve out any blocks covered by static allocations so that NVIPAM can honor the
	// assignment even when the prefix falls outside this cluster's normally assigned range.
	partitionExclusions = subtractStaticAllocations(partitionExclusions, m.staticAllocations)

	// Merge with user-provided ExcludeRanges. User exclusions always take precedence and
	// are never carved out by static allocations.
	return toNVIPAMExcludeRanges(iputils.MergeRanges(append(partitionExclusions, m.mergedExclusions...)))
}

// subtractStaticAllocations carves out the block covered by each static allocation prefix from the
// exclusion ranges so that NVIPAM honors the assignment regardless of which cluster's range it falls into.
func subtractStaticAllocations(exclusions []iputils.IPRange, staticAllocations map[string]string) []iputils.IPRange {
	for _, prefix := range staticAllocations {
		_, network, err := net.ParseCIDR(prefix)
		if err != nil {
			continue
		}
		prefixLen, _ := network.Mask.Size()
		// e.g. 10.0.0.8/30 → start=10.0.0.8, end=10.0.0.11 (10.0.0.8 + 2^(32-30) - 1 = 10.0.0.8 + 3)
		//      10.0.0.0/27 → start=10.0.0.0, end=10.0.0.31 (10.0.0.0 + 2^(32-27) - 1 = 10.0.0.0 + 31)
		sub := iputils.IPRange{
			Start: iputils.IPv4ToUint32(network.IP),
			End:   iputils.IPv4ToUint32(network.IP) + iputils.PrefixSize(prefixLen) - 1,
		}
		exclusions = iputils.SubtractRange(exclusions, sub)
	}
	return exclusions
}

// areClusterBlocksCompatible reports whether each cluster-block is within the current parent network.
// Returns false only when a cluster-block is outside the parent network entirely (e.g. the
// Network/Subnet field in the spec was changed to a different address space).
func (m *MultiDPUClusterExclusionCalculator) areClusterBlocksCompatible(clusterBlocks []iputils.IPRange) bool {
	for _, clusterBlock := range clusterBlocks {
		if !clusterBlock.In(m.parentRange) {
			return false
		}
	}
	return true
}

// computeBlocksForCluster determines which per-node blocks belong to this DPUCluster and returns them as a flat slice
// (not yet coalesced into cluster-blocks). Two paths:
//
//   - Top-up: existing blocks are within the parent and still contain allocatable IPs; if the count falls below
//     targetBlocksPerDPUCluster, additional blocks are consumed from the sequential pool to close the deficit.
//   - Fresh allocation: no compatible existing blocks; consume targetBlocksPerDPUCluster blocks sequentially from the
//     pool.
func (m *MultiDPUClusterExclusionCalculator) computeBlocksForCluster(existingClusterBlocks []iputils.IPRange) ([]iputils.IPRange, error) {
	// Check for already valid allocations and potential top ups
	if len(existingClusterBlocks) > 0 && m.areClusterBlocksCompatible(existingClusterBlocks) {
		if blocks, ok := m.topUpBlocks(existingClusterBlocks); ok {
			return blocks, nil
		}
	}

	if m.allocatableBlocksPosition >= len(m.allocatableBlocks) {
		return nil, fmt.Errorf("no allocatable blocks remaining")
	}

	return m.consumeBlocks(m.targetBlocksPerDPUCluster), nil
}

// topUpBlocks counts the usable blocks in existingClusterBlocks and tops them up to targetBlocksPerDPUCluster if there is
// a deficit.
// Returns the updated blocks and ok=true when the existing allocation has at least one usable block.
// Returns nil, ok=false when all blocks are excluded, signaling the caller to fall through to a fresh sequential
// allocation.
func (m *MultiDPUClusterExclusionCalculator) topUpBlocks(existingClusterBlocks []iputils.IPRange) ([]iputils.IPRange, bool) {
	allocatable := countAllocatableBlocksInRange(existingClusterBlocks, m.blockSizePerNode, m.mergedExclusions)
	if allocatable == 0 {
		return nil, false
	}

	// Top up if there is deficit, otherwise return the existingClusterBlocks as is.
	if deficit := m.targetBlocksPerDPUCluster - allocatable; deficit > 0 {
		return append(slices.Clone(existingClusterBlocks), iputils.MergeRanges(m.consumeBlocks(deficit))...), true
	}
	return existingClusterBlocks, true
}

// consumeBlocks advances the allocatable pool cursor by up to count positions and returns the consumed blocks. If fewer
// than count blocks remain, all remaining blocks are returned.
func (m *MultiDPUClusterExclusionCalculator) consumeBlocks(count int) []iputils.IPRange {
	first := m.allocatableBlocksPosition
	m.allocatableBlocksPosition = min(first+count, len(m.allocatableBlocks))
	return m.allocatableBlocks[first:m.allocatableBlocksPosition]
}

// countAllocatableBlocksInRange returns the number of per-node allocation blocks within the cluster-blocks that are not
// fully covered by mergedExclusions.
func countAllocatableBlocksInRange(clusterBlocks []iputils.IPRange, blockSizePerNode uint32, mergedExclusions []iputils.IPRange) int {
	total := 0
	for _, clusterBlock := range clusterBlocks {
		for blockStart := clusterBlock.Start; blockStart <= clusterBlock.End; blockStart += blockSizePerNode {
			if !(iputils.IPRange{Start: blockStart, End: blockStart + blockSizePerNode - 1}).InAny(mergedExclusions) {
				total++
			}
		}
	}
	return total
}

// parseIPRanges converts dpuservicev1.IPRanges to the internal IPRange representation.
func parseIPRanges(ranges []dpuservicev1.IPRange) []iputils.IPRange {
	if len(ranges) == 0 {
		return nil
	}
	result := make([]iputils.IPRange, len(ranges))
	for i, r := range ranges {
		result[i] = iputils.IPRange{
			Start: iputils.IPv4ToUint32(net.ParseIP(r.StartIP)),
			End:   iputils.IPv4ToUint32(net.ParseIP(r.EndIP)),
		}
	}
	return result
}

// toDPUServiceIPAMIPRanges converts internal IPRanges to dpuservicev1.IPRange values.
func toDPUServiceIPAMIPRanges(ranges []iputils.IPRange) []dpuservicev1.IPRange {
	if len(ranges) == 0 {
		return nil
	}
	result := make([]dpuservicev1.IPRange, len(ranges))
	for i, r := range ranges {
		result[i] = dpuservicev1.IPRange{
			StartIP: iputils.Uint32ToIPv4Str(r.Start),
			EndIP:   iputils.Uint32ToIPv4Str(r.End),
		}
	}
	return result
}

// toNVIPAMExcludeRanges converts internal IPRange representation to nvipam IP ranges.
func toNVIPAMExcludeRanges(ranges []iputils.IPRange) []nvipamv1.ExcludeRange {
	if len(ranges) == 0 {
		return nil
	}
	result := make([]nvipamv1.ExcludeRange, len(ranges))
	for i, r := range ranges {
		result[i] = nvipamv1.ExcludeRange{
			StartIP: iputils.Uint32ToIPv4Str(r.Start),
			EndIP:   iputils.Uint32ToIPv4Str(r.End),
		}
	}
	return result
}
