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
	"net"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/internal/dpuservicechain/utils/iputils"
	nvipamv1 "github.com/nvidia/doca-platform/third_party/api/nvipam/api/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gmeasure"
)

var _ = Describe("MultiDPUClusterExclusionCalculator", func() {
	Context("core logic", func() {
		type perClusterSetting struct {
			existingAllocations []dpuservicev1.IPRange
			expectedAllocations []dpuservicev1.IPRange
			expectedExclusions  []nvipamv1.ExcludeRange
			wantErr             bool
		}

		Context("IPPool", func() {
			type testCaseIPPool struct {
				spec                     *dpuservicev1.IPV4Subnet
				numOfBlocksPerDPUCluster int32
				perClusterSettings       []perClusterSetting
			}

			runTestCaseIPPool := func(tc testCaseIPPool) {
				existingAllocations := make([][]dpuservicev1.IPRange, len(tc.perClusterSettings))
				for i, s := range tc.perClusterSettings {
					existingAllocations[i] = s.existingAllocations
				}

				tc.spec.BlocksPerDPUCluster = &tc.numOfBlocksPerDPUCluster
				calc, err := NewMultiDPUClusterExclusionCalculatorForIPPool(tc.spec, existingAllocations)
				Expect(err).NotTo(HaveOccurred())

				type result struct {
					allocations []dpuservicev1.IPRange
					exclusions  []nvipamv1.ExcludeRange
					err         error
				}
				results := make([]result, len(tc.perClusterSettings))
				for i, s := range tc.perClusterSettings {
					allocs, callErr := calc.AllocateClusterBlocks(s.existingAllocations)
					var excl []nvipamv1.ExcludeRange
					if callErr == nil {
						excl = calc.ComputeExclusions(allocs)
					}
					results[i] = result{allocs, excl, callErr}
				}

				for i, r := range results {
					s := tc.perClusterSettings[i]
					if s.wantErr {
						Expect(r.err).To(HaveOccurred(), "cluster %d should error", i)
						continue
					}
					Expect(r.err).NotTo(HaveOccurred(), "cluster %d", i)
					Expect(r.allocations).To(BeComparableTo(s.expectedAllocations), "cluster %d allocations", i)
					Expect(r.exclusions).To(BeComparableTo(s.expectedExclusions), "cluster %d exclusions", i)
				}
			}

			Context("new DPUServiceIPAM", func() {
				DescribeTable("Allocate returns correct results with 2 DPUClusters",
					runTestCaseIPPool,
					Entry("network with buffer to accommodate more DPUClusters, no exclusions",
						// /24 (256 IPs) gives 25 full blocks (250 IPs, starting at .1); tail 10.0.0.251–10.0.0.255 is unused.
						// Cluster 0 gets blocks 0,1,2 → 10.0.0.1–10.0.0.30; cluster 1 gets blocks 3,4,5 →
						// 10.0.0.31–10.0.0.60. The upper IPs remain as buffer.
						testCaseIPPool{
							spec:                     &dpuservicev1.IPV4Subnet{Subnet: "10.0.0.0/24", PerNodeIPCount: 10},
							numOfBlocksPerDPUCluster: 3,

							perClusterSettings: []perClusterSetting{
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.0"},
										{StartIP: "10.0.0.31", EndIP: "10.0.0.255"},
									},
								},
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.31", EndIP: "10.0.0.60"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.30"},
										{StartIP: "10.0.0.61", EndIP: "10.0.0.255"},
									},
								},
							},
						},
					),
					Entry("network with buffer to accommodate more DPUClusters, exclusions matching subset of block per node",
						// ExcludeRange {10.0.0.0, 10.0.0.4} covers part of block 0 (10.0.0.1–10.0.0.10) so the
						// block is still allocatable. Cluster 0 gets blocks 0,1,2 → 10.0.0.1–10.0.0.30; cluster 1
						// gets blocks 3,4,5 → 10.0.0.31–10.0.0.60. The partial exclusion is propagated.
						testCaseIPPool{
							spec: &dpuservicev1.IPV4Subnet{
								Subnet:         "10.0.0.0/24",
								PerNodeIPCount: 10,
								ExcludeRanges:  []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.4"}},
							},
							numOfBlocksPerDPUCluster: 3,

							perClusterSettings: []perClusterSetting{
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.4"},
										{StartIP: "10.0.0.31", EndIP: "10.0.0.255"},
									},
								},
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.31", EndIP: "10.0.0.60"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.30"},
										{StartIP: "10.0.0.61", EndIP: "10.0.0.255"},
									},
								},
							},
						},
					),
					Entry("network with buffer to accommodate more DPUClusters, exclusions matching block per node",
						// ExcludeRange {10.0.0.1, 10.0.0.10} fully covers block 0 (10.0.0.1–10.0.0.10) so the
						// block is skipped. Cluster 0 gets blocks 1,2,3 → 10.0.0.11–10.0.0.40; cluster 1
						// gets blocks 4,5,6 → 10.0.0.41–10.0.0.70. Buffer space keeps both well within the /24.
						testCaseIPPool{
							spec: &dpuservicev1.IPV4Subnet{
								Subnet:         "10.0.0.0/24",
								PerNodeIPCount: 10,
								ExcludeRanges:  []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.10"}},
							},
							numOfBlocksPerDPUCluster: 3,

							perClusterSettings: []perClusterSetting{
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.11", EndIP: "10.0.0.40"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.10"},
										{StartIP: "10.0.0.41", EndIP: "10.0.0.255"},
									},
								},
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.41", EndIP: "10.0.0.70"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.40"},
										{StartIP: "10.0.0.71", EndIP: "10.0.0.255"},
									},
								},
							},
						},
					),
					Entry("network with buffer to accommodate more DPUClusters, exclusions matching block per cluster",
						// ExcludeRange {10.0.0.0, 10.0.0.30} fully covers blocks 0,1,2 (10.0.0.1–10.0.0.30).
						// Cluster 0 starts at block 3 → 10.0.0.31–10.0.0.60;
						// cluster 1 gets blocks 6,7,8 → 10.0.0.61–10.0.0.90. Ample buffer remains in the /24.
						testCaseIPPool{
							spec: &dpuservicev1.IPV4Subnet{
								Subnet:         "10.0.0.0/24",
								PerNodeIPCount: 10,
								ExcludeRanges:  []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.30"}},
							},
							numOfBlocksPerDPUCluster: 3,

							perClusterSettings: []perClusterSetting{
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.31", EndIP: "10.0.0.60"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.30"},
										{StartIP: "10.0.0.61", EndIP: "10.0.0.255"},
									},
								},
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.61", EndIP: "10.0.0.90"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.60"},
										{StartIP: "10.0.0.91", EndIP: "10.0.0.255"},
									},
								},
							},
						},
					),
					Entry("exact network size to accommodate DPUClusters, no exclusions",
						// /26 (64 IPs) gives 6 full blocks (60 IPs, starting at .1); tail 10.0.0.61–10.0.0.63 is unused.
						// Cluster 0 gets blocks 0,1,2 → 10.0.0.1–10.0.0.30; cluster 1 gets blocks 3,4,5 →
						// 10.0.0.31–10.0.0.60. No buffer remains after both allocations.
						testCaseIPPool{
							spec:                     &dpuservicev1.IPV4Subnet{Subnet: "10.0.0.0/26", PerNodeIPCount: 10},
							numOfBlocksPerDPUCluster: 3,

							perClusterSettings: []perClusterSetting{
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.0"},
										{StartIP: "10.0.0.31", EndIP: "10.0.0.63"},
									},
								},
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.31", EndIP: "10.0.0.60"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.30"},
										{StartIP: "10.0.0.61", EndIP: "10.0.0.63"},
									},
								},
							},
						},
					),
					Entry("exact network size to accommodate DPUClusters, exclusions matching subset of block per node",
						// ExcludeRange {10.0.0.0, 10.0.0.4} partially covers block 0 (10.0.0.1–10.0.0.10);
						// block remains allocatable. Both clusters fit within the /26 with no buffer left over.
						testCaseIPPool{
							spec: &dpuservicev1.IPV4Subnet{
								Subnet:         "10.0.0.0/26",
								PerNodeIPCount: 10,
								ExcludeRanges:  []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.4"}},
							},
							numOfBlocksPerDPUCluster: 3,

							perClusterSettings: []perClusterSetting{
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.4"},
										{StartIP: "10.0.0.31", EndIP: "10.0.0.63"},
									},
								},
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.31", EndIP: "10.0.0.60"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.30"},
										{StartIP: "10.0.0.61", EndIP: "10.0.0.63"},
									},
								},
							},
						},
					),
					Entry("exact network size to accommodate DPUClusters, exclusions matching block per node",
						// ExcludeRange {10.0.0.1, 10.0.0.10} fully covers block 0 (10.0.0.1–10.0.0.10) so
						// block 0 is skipped. Cluster 0 gets blocks 1,2,3 → 10.0.0.11–10.0.0.40.
						// Only blocks 4,5 remain for cluster 1 → partial allocation 10.0.0.41–10.0.0.60.
						testCaseIPPool{
							spec: &dpuservicev1.IPV4Subnet{
								Subnet:         "10.0.0.0/26",
								PerNodeIPCount: 10,
								ExcludeRanges:  []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.10"}},
							},
							numOfBlocksPerDPUCluster: 3,

							perClusterSettings: []perClusterSetting{
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.11", EndIP: "10.0.0.40"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.10"},
										{StartIP: "10.0.0.41", EndIP: "10.0.0.63"},
									},
								},
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.41", EndIP: "10.0.0.60"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.40"},
										{StartIP: "10.0.0.61", EndIP: "10.0.0.63"},
									},
								},
							},
						},
					),
					Entry("exact network size to accommodate DPUClusters, exclusions matching block per cluster",
						// ExcludeRange {10.0.0.0, 10.0.0.30} fully covers blocks 0,1,2 (10.0.0.1–10.0.0.30).
						// Cluster 0 gets blocks 3,4,5 → 10.0.0.31–10.0.0.60.
						// No blocks remain for cluster 1 → error.
						testCaseIPPool{
							spec: &dpuservicev1.IPV4Subnet{
								Subnet:         "10.0.0.0/26",
								PerNodeIPCount: 10,
								ExcludeRanges:  []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.30"}},
							},
							numOfBlocksPerDPUCluster: 3,

							perClusterSettings: []perClusterSetting{
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.31", EndIP: "10.0.0.60"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.30"},
										{StartIP: "10.0.0.61", EndIP: "10.0.0.63"},
									},
								},
								{wantErr: true},
							},
						},
					),
					Entry("/30 subnet, 1 IP per node, 1 block per DPUCluster",
						// /30 (4 IPs): network .0 and broadcast .3 are reserved, leaving 2 usable IPs (.1–.2).
						// Each DPUCluster receives one single-IP block.
						testCaseIPPool{
							spec:                     &dpuservicev1.IPV4Subnet{Subnet: "10.0.0.0/30", PerNodeIPCount: 1},
							numOfBlocksPerDPUCluster: 1,

							perClusterSettings: []perClusterSetting{
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.1"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.0"},
										{StartIP: "10.0.0.2", EndIP: "10.0.0.3"},
									},
								},
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.2", EndIP: "10.0.0.2"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.1"},
										{StartIP: "10.0.0.3", EndIP: "10.0.0.3"},
									},
								},
							},
						},
					),
					Entry("/29 subnet, 2 IPs per node, 1 block per DPUCluster",
						// /29 (8 IPs): network .0 and broadcast .7 reserved, leaving 6 usable IPs (.1-.5)
						// Each DPUCluster receives one two-IP block.
						testCaseIPPool{
							spec:                     &dpuservicev1.IPV4Subnet{Subnet: "10.0.0.0/29", PerNodeIPCount: 2},
							numOfBlocksPerDPUCluster: 1,

							perClusterSettings: []perClusterSetting{
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.2"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.0"},
										{StartIP: "10.0.0.3", EndIP: "10.0.0.7"},
									},
								},
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.3", EndIP: "10.0.0.4"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.2"},
										{StartIP: "10.0.0.5", EndIP: "10.0.0.7"},
									},
								},
							},
						},
					),
					Entry("small network that don't fit the given DPUClusters, no exclusions",
						// /27 (32 IPs) gives only 3 full blocks (30 IPs, starting at .1); tail 10.0.0.31 is unused.
						// Cluster 0 claims all 3 blocks → 10.0.0.1–10.0.0.30; cluster 1 finds the pool
						// empty and errors.
						testCaseIPPool{
							spec:                     &dpuservicev1.IPV4Subnet{Subnet: "10.0.0.0/27", PerNodeIPCount: 10},
							numOfBlocksPerDPUCluster: 3,

							perClusterSettings: []perClusterSetting{
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.0"},
										{StartIP: "10.0.0.31", EndIP: "10.0.0.31"},
									},
								},
								{wantErr: true},
							},
						},
					),
				)
			})

			Context("single DPUCluster", func() {
				runTestCaseIPPoolSingleCluster := func(tc testCaseIPPool) {
					existingAllocations := make([][]dpuservicev1.IPRange, len(tc.perClusterSettings))
					for i, s := range tc.perClusterSettings {
						existingAllocations[i] = s.existingAllocations
					}
					tc.spec.BlocksPerDPUCluster = nil
					calc, err := NewMultiDPUClusterExclusionCalculatorForIPPool(tc.spec, existingAllocations)
					Expect(err).NotTo(HaveOccurred())
					for i, s := range tc.perClusterSettings {
						allocs, callErr := calc.AllocateClusterBlocks(s.existingAllocations)
						excl := calc.ComputeExclusions(allocs)
						Expect(callErr).NotTo(HaveOccurred(), "cluster %d", i)
						Expect(allocs).To(BeComparableTo(s.expectedAllocations), "cluster %d allocations", i)
						Expect(excl).To(BeComparableTo(s.expectedExclusions), "cluster %d exclusions", i)
					}
				}

				DescribeTable("Allocate returns correct results with 1 DPUCluster",
					runTestCaseIPPoolSingleCluster,
					Entry("no exclusions, no existing allocations — tail excluded",
						// /24 gives 25 full 10-IP blocks (250 IPs, starting at .1); the 4-IP tail
						// (.251–.255) is excluded because it does not fill a complete block.
						testCaseIPPool{
							spec: &dpuservicev1.IPV4Subnet{Subnet: "10.0.0.0/24", PerNodeIPCount: 10},
							perClusterSettings: []perClusterSetting{
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.250"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.0"},
										{StartIP: "10.0.0.251", EndIP: "10.0.0.255"},
									},
								},
							},
						},
					),
					Entry("/30 subnet, 1 IP per node — no tail",
						// /30 has 2 usable IPs (.1–.2). Single cluster gets both; only
						// network (.0) and broadcast (.3) are excluded.
						testCaseIPPool{
							spec: &dpuservicev1.IPV4Subnet{Subnet: "10.0.0.0/30", PerNodeIPCount: 1},
							perClusterSettings: []perClusterSetting{
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.2"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.0"},
										{StartIP: "10.0.0.3", EndIP: "10.0.0.3"},
									},
								},
							},
						},
					),
					Entry("/29 subnet, 2 IPs per node — no tail",
						// /29 has 6 usable IPs (.1–.6). 3 blocks of 2; only network (.0)
						// and broadcast (.7) are excluded.
						testCaseIPPool{
							spec: &dpuservicev1.IPV4Subnet{Subnet: "10.0.0.0/29", PerNodeIPCount: 2},
							perClusterSettings: []perClusterSetting{
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.6"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.0"},
										{StartIP: "10.0.0.7", EndIP: "10.0.0.7"},
									},
								},
							},
						},
					),
					Entry("user exclusion partially covering a block",
						// ExcludeRange {10.0.0.0, 10.0.0.4} partially covers block 0 (.1–.10)
						// so the block remains allocatable. The exclusion is merged with the
						// network-address exclusion in the output.
						testCaseIPPool{
							spec: &dpuservicev1.IPV4Subnet{
								Subnet:         "10.0.0.0/24",
								PerNodeIPCount: 10,
								ExcludeRanges:  []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.4"}},
							},
							perClusterSettings: []perClusterSetting{
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.250"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.4"},
										{StartIP: "10.0.0.251", EndIP: "10.0.0.255"},
									},
								},
							},
						},
					),
					Entry("idempotent — existing allocation is preserved",
						// Cluster already owns the full allocation. The calculator returns the
						// same cluster-block without consuming new pool space.
						testCaseIPPool{
							spec: &dpuservicev1.IPV4Subnet{Subnet: "10.0.0.0/24", PerNodeIPCount: 10},
							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.250"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.250"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.0"},
										{StartIP: "10.0.0.251", EndIP: "10.0.0.255"},
									},
								},
							},
						},
					),
				)
			})

			Context("existing DPUServiceIPAM - DPUCluster operations", func() {
				DescribeTable("Allocate returns correct results with 1 DPUCluster that has already allocations when 1 DPUCluster is added",
					runTestCaseIPPool,
					Entry("network with buffer, no exclusions",
						// /24, cluster 0 already owns blocks 0,1,2 → 10.0.0.1–10.0.0.30. No deficit.
						// New cluster 1 gets the next 3 blocks → 10.0.0.31–10.0.0.60.
						testCaseIPPool{
							spec:                     &dpuservicev1.IPV4Subnet{Subnet: "10.0.0.0/24", PerNodeIPCount: 10},
							numOfBlocksPerDPUCluster: 3,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.0"},
										{StartIP: "10.0.0.31", EndIP: "10.0.0.255"},
									},
								},
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.31", EndIP: "10.0.0.60"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.30"},
										{StartIP: "10.0.0.61", EndIP: "10.0.0.255"},
									},
								},
							},
						},
					),
					Entry("network with buffer, exclusions matching subset of block per node",
						// Partial exclusion {0.0,0.4}. Cluster 0 existing; block 0 not fully covered →
						// no deficit. New cluster 1 gets blocks 3,4,5 from buffer.
						testCaseIPPool{
							spec: &dpuservicev1.IPV4Subnet{
								Subnet:         "10.0.0.0/24",
								PerNodeIPCount: 10,
								ExcludeRanges:  []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.4"}},
							},
							numOfBlocksPerDPUCluster: 3,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.4"},
										{StartIP: "10.0.0.31", EndIP: "10.0.0.255"},
									},
								},
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.31", EndIP: "10.0.0.60"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.30"},
										{StartIP: "10.0.0.61", EndIP: "10.0.0.255"},
									},
								},
							},
						},
					),
					Entry("network with buffer, exclusions matching block per node",
						// Exclusion {0.1,0.10} fully covers block 0 (10.0.0.1–10.0.0.10), so block 0
						// is skipped. Cluster 0 existing [{0.1,0.30}] has 2 allocatable blocks →
						// deficit=1; topped up with block 3 → [{0.1,0.40}]. New cluster 1 gets blocks 4,5,6 → 10.0.0.41–10.0.0.70.
						testCaseIPPool{
							spec: &dpuservicev1.IPV4Subnet{
								Subnet:         "10.0.0.0/24",
								PerNodeIPCount: 10,
								ExcludeRanges:  []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.10"}},
							},
							numOfBlocksPerDPUCluster: 3,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.40"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.10"},
										{StartIP: "10.0.0.41", EndIP: "10.0.0.255"},
									},
								},
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.41", EndIP: "10.0.0.70"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.40"},
										{StartIP: "10.0.0.71", EndIP: "10.0.0.255"},
									},
								},
							},
						},
					),
					Entry("network with buffer, exclusions matching block per cluster",
						// Exclusion {0.0,0.30} fully covers blocks 0,1,2 (10.0.0.1–10.0.0.30).
						// Cluster 0 existing [{0.1,0.30}] has 0 allocatable blocks → fresh alloc → blocks 3,4,5
						// = [{0.31,0.60}]. New cluster 1 gets blocks 6,7,8 → 10.0.0.61–10.0.0.90.
						testCaseIPPool{
							spec: &dpuservicev1.IPV4Subnet{
								Subnet:         "10.0.0.0/24",
								PerNodeIPCount: 10,
								ExcludeRanges:  []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.30"}},
							},
							numOfBlocksPerDPUCluster: 3,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.31", EndIP: "10.0.0.60"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.30"},
										{StartIP: "10.0.0.61", EndIP: "10.0.0.255"},
									},
								},
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.61", EndIP: "10.0.0.90"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.60"},
										{StartIP: "10.0.0.91", EndIP: "10.0.0.255"},
									},
								},
							},
						},
					),
					Entry("exact network size to accommodate DPUClusters, no exclusions",
						// /26, cluster 0 existing with blocks 0,1,2 → 10.0.0.1–10.0.0.30. New cluster 1
						// gets blocks 3,4,5 → 10.0.0.31–10.0.0.60. No buffer remains after both allocations.
						testCaseIPPool{
							spec:                     &dpuservicev1.IPV4Subnet{Subnet: "10.0.0.0/26", PerNodeIPCount: 10},
							numOfBlocksPerDPUCluster: 3,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.0"},
										{StartIP: "10.0.0.31", EndIP: "10.0.0.63"},
									},
								},
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.31", EndIP: "10.0.0.60"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.30"},
										{StartIP: "10.0.0.61", EndIP: "10.0.0.63"},
									},
								},
							},
						},
					),
					Entry("exact network size to accommodate DPUClusters, exclusions matching subset of block per node",
						// /26, partial exclusion {0.0,0.4}. Cluster 0 existing [{0.1,0.30}]; block 0
						// partially covered → no deficit. New cluster 1 gets blocks 3,4,5. No buffer.
						testCaseIPPool{
							spec: &dpuservicev1.IPV4Subnet{
								Subnet:         "10.0.0.0/26",
								PerNodeIPCount: 10,
								ExcludeRanges:  []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.4"}},
							},
							numOfBlocksPerDPUCluster: 3,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.4"},
										{StartIP: "10.0.0.31", EndIP: "10.0.0.63"},
									},
								},
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.31", EndIP: "10.0.0.60"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.30"},
										{StartIP: "10.0.0.61", EndIP: "10.0.0.63"},
									},
								},
							},
						},
					),
					Entry("exact network size to accommodate DPUClusters, exclusions matching block per node",
						// Exclusion {0.1,0.10} fully covers block 0 (10.0.0.1–10.0.0.10), so block 0
						// is skipped. Cluster 0 existing [{0.1,0.30}] has 2 allocatable blocks →
						// deficit=1; topped up with block 3 → [{0.1,0.40}]. Only blocks 4,5 remain for
						// cluster 1 → partial allocation 10.0.0.41–10.0.0.60.
						testCaseIPPool{
							spec: &dpuservicev1.IPV4Subnet{
								Subnet:         "10.0.0.0/26",
								PerNodeIPCount: 10,
								ExcludeRanges:  []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.10"}},
							},
							numOfBlocksPerDPUCluster: 3,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.40"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.10"},
										{StartIP: "10.0.0.41", EndIP: "10.0.0.63"},
									},
								},
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.41", EndIP: "10.0.0.60"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.40"},
										{StartIP: "10.0.0.61", EndIP: "10.0.0.63"},
									},
								},
							},
						},
					),
					Entry("exact network size to accommodate DPUClusters, exclusions matching block per cluster",
						// Exclusion {0.0,0.30} fully covers blocks 0,1,2 (10.0.0.1–10.0.0.30).
						// Cluster 0 existing [{0.1,0.30}] has 0 allocatable blocks → fresh alloc → blocks 3,4,5
						// = [{0.31,0.60}]. No blocks remain for new cluster 1 → error.
						testCaseIPPool{
							spec: &dpuservicev1.IPV4Subnet{
								Subnet:         "10.0.0.0/26",
								PerNodeIPCount: 10,
								ExcludeRanges:  []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.30"}},
							},
							numOfBlocksPerDPUCluster: 3,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.31", EndIP: "10.0.0.60"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.30"},
										{StartIP: "10.0.0.61", EndIP: "10.0.0.63"},
									},
								},
								{wantErr: true},
							},
						},
					),
					Entry("small network that don't fit the given DPUClusters, no exclusions",
						// /27, cluster 0 already owns all 3 blocks → preserved. New cluster 1 errors.
						testCaseIPPool{
							spec:                     &dpuservicev1.IPV4Subnet{Subnet: "10.0.0.0/27", PerNodeIPCount: 10},
							numOfBlocksPerDPUCluster: 3,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.0"},
										{StartIP: "10.0.0.31", EndIP: "10.0.0.31"},
									},
								},
								{wantErr: true},
							},
						},
					),
					Entry("/30 subnet, 1 IP per node, 1 block per DPUCluster",
						// /30 (4 IPs): network .0 and broadcast .3 reserved, leaving exactly 2 usable IPs.
						// Both clusters have existing allocations that exhaust the pool; adding a 3rd errors.
						testCaseIPPool{
							spec:                     &dpuservicev1.IPV4Subnet{Subnet: "10.0.0.0/30", PerNodeIPCount: 1},
							numOfBlocksPerDPUCluster: 1,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.1"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.1"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.0"},
										{StartIP: "10.0.0.2", EndIP: "10.0.0.3"},
									},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.2", EndIP: "10.0.0.2"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.2", EndIP: "10.0.0.2"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.1"},
										{StartIP: "10.0.0.3", EndIP: "10.0.0.3"},
									},
								},
								{wantErr: true},
							},
						},
					),
				)
				DescribeTable("Allocate returns correct results with 3 DPUClusters that have already allocations when the middle DPUCluster is removed",
					runTestCaseIPPool,
					Entry("network with buffer, no exclusions",
						// /24, clusters 0 and 2 retain their allocations after cluster 1 is deleted.
						// Freed blocks 3,4,5 return to the pool (unused here).
						testCaseIPPool{
							spec:                     &dpuservicev1.IPV4Subnet{Subnet: "10.0.0.0/24", PerNodeIPCount: 10},
							numOfBlocksPerDPUCluster: 3,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.0"},
										{StartIP: "10.0.0.31", EndIP: "10.0.0.255"},
									},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.61", EndIP: "10.0.0.90"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.61", EndIP: "10.0.0.90"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.60"},
										{StartIP: "10.0.0.91", EndIP: "10.0.0.255"},
									},
								},
							},
						},
					),
					Entry("network with buffer, exclusions matching subset of block per node",
						// /24, partial exclusion {0.0,0.4}. Both surviving clusters preserved; the
						// partial exclusion is propagated into cluster 0's IPPool.
						testCaseIPPool{
							spec: &dpuservicev1.IPV4Subnet{
								Subnet:         "10.0.0.0/24",
								PerNodeIPCount: 10,
								ExcludeRanges:  []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.4"}},
							},
							numOfBlocksPerDPUCluster: 3,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.4"},
										{StartIP: "10.0.0.31", EndIP: "10.0.0.255"},
									},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.61", EndIP: "10.0.0.90"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.61", EndIP: "10.0.0.90"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.60"},
										{StartIP: "10.0.0.91", EndIP: "10.0.0.255"},
									},
								},
							},
						},
					),
					Entry("network with buffer, exclusions matching block per node",
						// Exclusion {0.1,0.10} fully covers block 0 (10.0.0.1–10.0.0.10).
						// Cluster 0 existing [{0.1,0.30}] has 2 allocatable blocks → deficit=1; topped up
						// with block 3 → [{0.1,0.40}]. Cluster 2 existing [{0.61,0.90}] preserved.
						testCaseIPPool{
							spec: &dpuservicev1.IPV4Subnet{
								Subnet:         "10.0.0.0/24",
								PerNodeIPCount: 10,
								ExcludeRanges:  []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.10"}},
							},
							numOfBlocksPerDPUCluster: 3,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.40"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.10"},
										{StartIP: "10.0.0.41", EndIP: "10.0.0.255"},
									},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.61", EndIP: "10.0.0.90"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.61", EndIP: "10.0.0.90"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.60"},
										{StartIP: "10.0.0.91", EndIP: "10.0.0.255"},
									},
								},
							},
						},
					),
					Entry("network with buffer, exclusions matching block per cluster",
						// Exclusion {0.0,0.30} fully covers blocks 0,1,2 (one full cluster).
						// Cluster 0 existing [{0.1,0.30}] has 0 allocatable blocks → fresh alloc → blocks 3,4,5
						// = [{0.31,0.60}]. Cluster 2 existing [{0.61,0.90}] preserved.
						testCaseIPPool{
							spec: &dpuservicev1.IPV4Subnet{
								Subnet:         "10.0.0.0/24",
								PerNodeIPCount: 10,
								ExcludeRanges:  []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.30"}},
							},
							numOfBlocksPerDPUCluster: 3,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.31", EndIP: "10.0.0.60"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.30"},
										{StartIP: "10.0.0.61", EndIP: "10.0.0.255"},
									},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.61", EndIP: "10.0.0.90"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.61", EndIP: "10.0.0.90"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.60"},
										{StartIP: "10.0.0.91", EndIP: "10.0.0.255"},
									},
								},
							},
						},
					),
					Entry("exact network size, no exclusions",
						// /25 (128 IPs, 12 blocks, tail 10.0.0.120–10.0.0.127). Clusters 0 and 2 preserved.
						// Freed blocks 3,4,5 return to pool (unused).
						testCaseIPPool{
							spec:                     &dpuservicev1.IPV4Subnet{Subnet: "10.0.0.0/25", PerNodeIPCount: 10},
							numOfBlocksPerDPUCluster: 3,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.0"},
										{StartIP: "10.0.0.31", EndIP: "10.0.0.127"},
									},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.61", EndIP: "10.0.0.90"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.61", EndIP: "10.0.0.90"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.60"},
										{StartIP: "10.0.0.91", EndIP: "10.0.0.127"},
									},
								},
							},
						},
					),
					Entry("exact network size, exclusions matching subset of block per node",
						// /25, partial exclusion {0.0,0.4}. Both surviving clusters preserved; exclusion
						// propagated to cluster 0's IPPool.
						testCaseIPPool{
							spec: &dpuservicev1.IPV4Subnet{
								Subnet:         "10.0.0.0/25",
								PerNodeIPCount: 10,
								ExcludeRanges:  []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.4"}},
							},
							numOfBlocksPerDPUCluster: 3,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.4"},
										{StartIP: "10.0.0.31", EndIP: "10.0.0.127"},
									},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.61", EndIP: "10.0.0.90"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.61", EndIP: "10.0.0.90"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.60"},
										{StartIP: "10.0.0.91", EndIP: "10.0.0.127"},
									},
								},
							},
						},
					),
					Entry("exact network size, exclusions matching block per node",
						// Exclusion {0.1,0.10} fully covers block 0 (10.0.0.1–10.0.0.10).
						// Cluster 0 existing [{0.1,0.30}] has 2 allocatable blocks → deficit=1; topped up
						// with block 3 → [{0.1,0.40}]. Cluster 2 existing [{0.61,0.90}] preserved.
						testCaseIPPool{
							spec: &dpuservicev1.IPV4Subnet{
								Subnet:         "10.0.0.0/25",
								PerNodeIPCount: 10,
								ExcludeRanges:  []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.10"}},
							},
							numOfBlocksPerDPUCluster: 3,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.40"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.10"},
										{StartIP: "10.0.0.41", EndIP: "10.0.0.127"},
									},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.61", EndIP: "10.0.0.90"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.61", EndIP: "10.0.0.90"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.60"},
										{StartIP: "10.0.0.91", EndIP: "10.0.0.127"},
									},
								},
							},
						},
					),
					Entry("exact network size, exclusions matching block per cluster",
						// Exclusion {0.0,0.30} fully covers blocks 0,1,2 (one full cluster).
						// Cluster 0 existing [{0.1,0.30}] has 0 allocatable blocks → fresh alloc → blocks 3,4,5
						// = [{0.31,0.60}]. Cluster 2 existing [{0.61,0.90}] preserved.
						testCaseIPPool{
							spec: &dpuservicev1.IPV4Subnet{
								Subnet:         "10.0.0.0/25",
								PerNodeIPCount: 10,
								ExcludeRanges:  []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.30"}},
							},
							numOfBlocksPerDPUCluster: 3,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.31", EndIP: "10.0.0.60"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.30"},
										{StartIP: "10.0.0.61", EndIP: "10.0.0.127"},
									},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.61", EndIP: "10.0.0.90"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.61", EndIP: "10.0.0.90"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.60"},
										{StartIP: "10.0.0.91", EndIP: "10.0.0.127"},
									},
								},
							},
						},
					),
				)
			})

			Context("existing DPUServiceIPAM - existing DPUClusters - updates on DPUServiceIPAM", func() {
				DescribeTable("Allocate returns correct results with 2 DPUClusters that have already allocations and DPUServiceIPAM settings are changed",
					runTestCaseIPPool,
					Entry("subnet changes",
						// Subnet changes to a completely different address space. Existing allocations
						// in 10.0.0.0/24 are outside the new 192.168.0.0/24 parent → both clusters
						// fall through to fresh allocations in the new subnet.
						testCaseIPPool{
							spec:                     &dpuservicev1.IPV4Subnet{Subnet: "192.168.0.0/24", PerNodeIPCount: 10},
							numOfBlocksPerDPUCluster: 3,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "192.168.0.1", EndIP: "192.168.0.30"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "192.168.0.0", EndIP: "192.168.0.0"},
										{StartIP: "192.168.0.31", EndIP: "192.168.0.255"},
									},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.31", EndIP: "10.0.0.60"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "192.168.0.31", EndIP: "192.168.0.60"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "192.168.0.0", EndIP: "192.168.0.30"},
										{StartIP: "192.168.0.61", EndIP: "192.168.0.255"},
									},
								},
							},
						},
					),
					Entry("subnet grows",
						// /24 grows to /23 (512 IPs, 51 blocks, tail 10.0.1.250–10.0.1.255). Existing
						// allocations in 10.0.0.0/24 are within the new parent and block-aligned →
						// preserved unchanged. The upper /23 half becomes spare buffer.
						testCaseIPPool{
							spec:                     &dpuservicev1.IPV4Subnet{Subnet: "10.0.0.0/23", PerNodeIPCount: 10},
							numOfBlocksPerDPUCluster: 3,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.0"},
										{StartIP: "10.0.0.31", EndIP: "10.0.1.255"},
									},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.31", EndIP: "10.0.0.60"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.31", EndIP: "10.0.0.60"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.30"},
										{StartIP: "10.0.0.61", EndIP: "10.0.1.255"},
									},
								},
							},
						},
					),
					Entry("PerNodeIPCount grows, existing allocations align to new block grid",
						// PerNodeIPCount grows from 10 to 30; numOfBlocksPerDPUCluster stays 3. New blocks
						// start at .1 with size 30: block 0 = .1-.30, block 1 = .31-.60. Each cluster's
						// existing allocation covers exactly 1 block → deficit=2 each; topped up from pool.
						// Resulting allocations are non-contiguous.
						testCaseIPPool{
							spec:                     &dpuservicev1.IPV4Subnet{Subnet: "10.0.0.0/24", PerNodeIPCount: 30},
							numOfBlocksPerDPUCluster: 3,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedAllocations: []dpuservicev1.IPRange{
										{StartIP: "10.0.0.1", EndIP: "10.0.0.30"},
										{StartIP: "10.0.0.61", EndIP: "10.0.0.120"},
									},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.0"},
										{StartIP: "10.0.0.31", EndIP: "10.0.0.60"},
										{StartIP: "10.0.0.121", EndIP: "10.0.0.255"},
									},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.31", EndIP: "10.0.0.60"}},
									expectedAllocations: []dpuservicev1.IPRange{
										{StartIP: "10.0.0.31", EndIP: "10.0.0.60"},
										{StartIP: "10.0.0.121", EndIP: "10.0.0.180"},
									},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.30"},
										{StartIP: "10.0.0.61", EndIP: "10.0.0.120"},
										{StartIP: "10.0.0.181", EndIP: "10.0.0.255"},
									},
								},
							},
						},
					),
					Entry("numOfBlocksPerDPUCluster grows",
						// numOfBlocksPerDPUCluster doubles from 3 to 6 (PerNodeIPCount stays 10).
						// targetBlocksPerCluster grows from 3 to 6. Each cluster has 3 existing blocks →
						// deficit=3 each; topped up from pool. Resulting allocations are non-contiguous.
						testCaseIPPool{
							spec:                     &dpuservicev1.IPV4Subnet{Subnet: "10.0.0.0/24", PerNodeIPCount: 10},
							numOfBlocksPerDPUCluster: 6,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedAllocations: []dpuservicev1.IPRange{
										{StartIP: "10.0.0.1", EndIP: "10.0.0.30"},
										{StartIP: "10.0.0.61", EndIP: "10.0.0.90"},
									},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.0"},
										{StartIP: "10.0.0.31", EndIP: "10.0.0.60"},
										{StartIP: "10.0.0.91", EndIP: "10.0.0.255"},
									},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.31", EndIP: "10.0.0.60"}},
									expectedAllocations: []dpuservicev1.IPRange{
										{StartIP: "10.0.0.31", EndIP: "10.0.0.60"},
										{StartIP: "10.0.0.91", EndIP: "10.0.0.120"},
									},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.30"},
										{StartIP: "10.0.0.61", EndIP: "10.0.0.90"},
										{StartIP: "10.0.0.121", EndIP: "10.0.0.255"},
									},
								},
							},
						},
					),
					Entry("network with buffer to accommodate more DPUClusters, exclusions matching subset of block per node",
						// Partial exclusion added to /24 after initial alloc. All 3 blocks in each
						// cluster's existing range remain allocatable → no deficit; both preserved.
						// Exclusion propagated to IPPools.
						testCaseIPPool{
							spec: &dpuservicev1.IPV4Subnet{
								Subnet:         "10.0.0.0/24",
								PerNodeIPCount: 10,
								ExcludeRanges:  []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.4"}},
							},
							numOfBlocksPerDPUCluster: 3,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.4"},
										{StartIP: "10.0.0.31", EndIP: "10.0.0.255"},
									},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.31", EndIP: "10.0.0.60"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.31", EndIP: "10.0.0.60"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.30"},
										{StartIP: "10.0.0.61", EndIP: "10.0.0.255"},
									},
								},
							},
						},
					),
					Entry("network with buffer to accommodate more DPUClusters, exclusions matching block per node",
						// Exclusion {0.1,0.10} fully covers block 0 (10.0.0.1–10.0.0.10) → block 0 skipped.
						// Cluster 0 existing [{0.1,0.30}] has 2 allocatable blocks → deficit=1; topped up
						// with block 6 → [{0.1,0.30},{0.61,0.70}]. Cluster 1 existing [{0.31,0.60}]
						// has 3 allocatable blocks → no deficit.
						testCaseIPPool{
							spec: &dpuservicev1.IPV4Subnet{
								Subnet:         "10.0.0.0/24",
								PerNodeIPCount: 10,
								ExcludeRanges:  []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.10"}},
							},
							numOfBlocksPerDPUCluster: 3,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedAllocations: []dpuservicev1.IPRange{
										{StartIP: "10.0.0.1", EndIP: "10.0.0.30"},
										{StartIP: "10.0.0.61", EndIP: "10.0.0.70"},
									},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.10"},
										{StartIP: "10.0.0.31", EndIP: "10.0.0.60"},
										{StartIP: "10.0.0.71", EndIP: "10.0.0.255"},
									},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.31", EndIP: "10.0.0.60"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.31", EndIP: "10.0.0.60"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.30"},
										{StartIP: "10.0.0.61", EndIP: "10.0.0.255"},
									},
								},
							},
						},
					),
					Entry("network with buffer to accommodate more DPUClusters, exclusions matching block per cluster",
						// Exclusion {0.0,0.30} fully covers blocks 0,1,2 (10.0.0.1–10.0.0.30).
						// Cluster 0 existing [{0.1,0.30}] has 0 allocatable blocks → fresh alloc → blocks 3,4,5
						// = [{0.31,0.60}]. But cluster 1 existing [{0.31,0.60}] takes those; cluster 0 gets
						// blocks 6,7,8 → [{0.61,0.90}]. Cluster 1 existing [{0.31,0.60}] preserved.
						testCaseIPPool{
							spec: &dpuservicev1.IPV4Subnet{
								Subnet:         "10.0.0.0/24",
								PerNodeIPCount: 10,
								ExcludeRanges:  []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.30"}},
							},
							numOfBlocksPerDPUCluster: 3,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.61", EndIP: "10.0.0.90"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.60"},
										{StartIP: "10.0.0.91", EndIP: "10.0.0.255"},
									},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.31", EndIP: "10.0.0.60"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.31", EndIP: "10.0.0.60"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.30"},
										{StartIP: "10.0.0.61", EndIP: "10.0.0.255"},
									},
								},
							},
						},
					),
					Entry("exact network size to accommodate DPUClusters, exclusions matching subset of block per node",
						// Partial exclusion added to /26. No full blocks excluded → no deficit; preserved.
						testCaseIPPool{
							spec: &dpuservicev1.IPV4Subnet{
								Subnet:         "10.0.0.0/26",
								PerNodeIPCount: 10,
								ExcludeRanges:  []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.4"}},
							},
							numOfBlocksPerDPUCluster: 3,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.4"},
										{StartIP: "10.0.0.31", EndIP: "10.0.0.63"},
									},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.31", EndIP: "10.0.0.60"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.31", EndIP: "10.0.0.60"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.30"},
										{StartIP: "10.0.0.61", EndIP: "10.0.0.63"},
									},
								},
							},
						},
					),
					Entry("exact network size to accommodate DPUClusters, exclusions matching block per node",
						// Block 0 (10.0.0.1–10.0.0.10) fully excluded in exact /26. Cluster 0 existing
						// [{0.1,0.30}] has 2 allocatable blocks → deficit=1; pool is empty (all 6 blocks
						// pre-taken) → no top-up; cluster 0 keeps its existing allocation. Cluster 1 preserved.
						testCaseIPPool{
							spec: &dpuservicev1.IPV4Subnet{
								Subnet:         "10.0.0.0/26",
								PerNodeIPCount: 10,
								ExcludeRanges:  []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.10"}},
							},
							numOfBlocksPerDPUCluster: 3,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.10"},
										{StartIP: "10.0.0.31", EndIP: "10.0.0.63"},
									},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.31", EndIP: "10.0.0.60"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.31", EndIP: "10.0.0.60"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.30"},
										{StartIP: "10.0.0.61", EndIP: "10.0.0.63"},
									},
								},
							},
						},
					),
					Entry("exact network size to accommodate DPUClusters, exclusions matching block per cluster",
						// Blocks 0,1,2 fully excluded in exact /26 (exclusion covers .0–.30 = all 3 blocks).
						// Cluster 0 existing [{0.1,0.30}] → fresh; pool empty → error.
						// Cluster 1 existing [{0.31,0.60}] has 3 allocatable blocks → no deficit.
						testCaseIPPool{
							spec: &dpuservicev1.IPV4Subnet{
								Subnet:         "10.0.0.0/26",
								PerNodeIPCount: 10,
								ExcludeRanges:  []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.30"}},
							},
							numOfBlocksPerDPUCluster: 3,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.30"}},
									wantErr:             true,
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.31", EndIP: "10.0.0.60"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.31", EndIP: "10.0.0.60"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.30"},
										{StartIP: "10.0.0.61", EndIP: "10.0.0.63"},
									},
								},
							},
						},
					),
				)
			})
		})
		Context("CIDRPool", func() {
			type testCaseCIDRPool struct {
				spec                      *dpuservicev1.IPV4Network
				numOfSubnetsPerDPUCluster int32
				perClusterSettings        []perClusterSetting
			}

			runTestCaseCIDRPool := func(tc testCaseCIDRPool) {
				existingAllocations := make([][]dpuservicev1.IPRange, len(tc.perClusterSettings))
				for i, s := range tc.perClusterSettings {
					existingAllocations[i] = s.existingAllocations
				}

				tc.spec.SubnetsPerDPUCluster = &tc.numOfSubnetsPerDPUCluster
				calc, err := NewMultiDPUClusterExclusionCalculatorForCIDRPool(tc.spec, existingAllocations)
				Expect(err).NotTo(HaveOccurred())

				type result struct {
					allocations []dpuservicev1.IPRange
					exclusions  []nvipamv1.ExcludeRange
					err         error
				}
				results := make([]result, len(tc.perClusterSettings))
				for i, s := range tc.perClusterSettings {
					allocs, callErr := calc.AllocateClusterBlocks(s.existingAllocations)
					var excl []nvipamv1.ExcludeRange
					if callErr == nil {
						excl = calc.ComputeExclusions(allocs)
					}
					results[i] = result{allocs, excl, callErr}
				}

				for i, r := range results {
					s := tc.perClusterSettings[i]
					if s.wantErr {
						Expect(r.err).To(HaveOccurred(), "cluster %d should error", i)
						continue
					}
					Expect(r.err).NotTo(HaveOccurred(), "cluster %d", i)
					Expect(r.allocations).To(BeComparableTo(s.expectedAllocations), "cluster %d allocations", i)
					Expect(r.exclusions).To(BeComparableTo(s.expectedExclusions), "cluster %d exclusions", i)
				}
			}

			runTestCaseCIDRPoolSingleCluster := func(tc testCaseCIDRPool) {
				existingAllocations := make([][]dpuservicev1.IPRange, len(tc.perClusterSettings))
				for i, s := range tc.perClusterSettings {
					existingAllocations[i] = s.existingAllocations
				}
				tc.spec.SubnetsPerDPUCluster = nil
				calc, err := NewMultiDPUClusterExclusionCalculatorForCIDRPool(tc.spec, existingAllocations)
				Expect(err).NotTo(HaveOccurred())
				for i, s := range tc.perClusterSettings {
					allocs, callErr := calc.AllocateClusterBlocks(s.existingAllocations)
					excl := calc.ComputeExclusions(allocs)
					Expect(callErr).NotTo(HaveOccurred(), "cluster %d", i)
					Expect(allocs).To(BeComparableTo(s.expectedAllocations), "cluster %d allocations", i)
					Expect(excl).To(BeComparableTo(s.expectedExclusions), "cluster %d exclusions", i)
				}
			}

			Context("new DPUServiceIPAM", func() {
				DescribeTable("Allocate returns correct results with 2 DPUClusters",
					runTestCaseCIDRPool,
					Entry("network with buffer to accommodate more DPUClusters, no exclusions",
						// /21 holds 4 /23 sub-networks; only 2 DPUClusters are created so the upper
						// half ({10.0.4.0, 10.0.7.255}) remains unused as buffer.
						testCaseCIDRPool{
							spec:                      &dpuservicev1.IPV4Network{Network: "10.0.0.0/21", PrefixSize: 27},
							numOfSubnetsPerDPUCluster: 16,

							perClusterSettings: []perClusterSetting{
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.2.0", EndIP: "10.0.7.255"}},
								},
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.3.255"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.1.255"},
										{StartIP: "10.0.4.0", EndIP: "10.0.7.255"},
									},
								},
							},
						},
					),
					Entry("network with buffer to accommodate more DPUClusters, exclusions matching subset of block per node",
						// Same partial exclusion on a /21 parent. Both DPUClusters still get their
						// /23; the user range is absorbed into the leading exclusion of the second
						// DPUCluster, and the /21 upper half ({10.0.4.0, 10.0.7.255}) remains unused.
						testCaseCIDRPool{
							spec: &dpuservicev1.IPV4Network{
								Network:       "10.0.0.0/21",
								PrefixSize:    27,
								ExcludeRanges: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.15"}},
							},
							numOfSubnetsPerDPUCluster: 16,

							perClusterSettings: []perClusterSetting{
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.15"}, // user excluded range
										{StartIP: "10.0.2.0", EndIP: "10.0.7.255"},
									},
								},
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.3.255"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.1.255"},
										{StartIP: "10.0.4.0", EndIP: "10.0.7.255"},
									},
								},
							},
						},
					),
					Entry("network with buffer to accommodate more DPUClusters, exclusions matching block per node",
						// Block 0 (10.0.0.0/27 = one per-node block) is excluded. The first DPUCluster
						// borrows block 16 from the second /23; the second DPUCluster takes the next 16
						// contiguous blocks. The /21 buffer means no cluster runs out.
						testCaseCIDRPool{
							spec: &dpuservicev1.IPV4Network{
								Network:       "10.0.0.0/21",
								PrefixSize:    27,
								ExcludeRanges: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.31"}},
							},
							numOfSubnetsPerDPUCluster: 16,

							perClusterSettings: []perClusterSetting{
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.32", EndIP: "10.0.2.31"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.31"}, // user excluded range
										{StartIP: "10.0.2.32", EndIP: "10.0.7.255"},
									},
								},
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.32", EndIP: "10.0.4.31"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.2.31"},
										{StartIP: "10.0.4.32", EndIP: "10.0.7.255"},
									},
								},
							},
						},
					),
					Entry("network with buffer to accommodate more DPUClusters, exclusions matching block per cluster",
						// The first /23 (10.0.0.0/23 = one per-cluster sub-network) is excluded from the /21.
						// Both DPUClusters still fit using the remaining /23 slots; the upper /21 half
						// ({10.0.4.0, 10.0.7.255}) serves as buffer.
						testCaseCIDRPool{
							spec: &dpuservicev1.IPV4Network{
								Network:       "10.0.0.0/21",
								PrefixSize:    27,
								ExcludeRanges: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
							},
							numOfSubnetsPerDPUCluster: 16,

							perClusterSettings: []perClusterSetting{
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.3.255"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.1.255"},
										{StartIP: "10.0.4.0", EndIP: "10.0.7.255"},
									},
								},
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.4.0", EndIP: "10.0.5.255"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.3.255"},
										{StartIP: "10.0.6.0", EndIP: "10.0.7.255"},
									},
								},
							},
						},
					),
					Entry("exact network size to accommodate DPUClusters, no exclusions",
						// /22 holds exactly 2 /23 sub-networks — no buffer remains after allocating
						// both DPUClusters.
						testCaseCIDRPool{
							spec:                      &dpuservicev1.IPV4Network{Network: "10.0.0.0/22", PrefixSize: 27},
							numOfSubnetsPerDPUCluster: 16,

							perClusterSettings: []perClusterSetting{
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.2.0", EndIP: "10.0.3.255"}},
								},
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.3.255"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
								},
							},
						},
					),
					Entry("exact network size to accommodate DPUClusters, exclusions matching subset of block per node",
						// ExcludeRange {10.0.0.0, 10.0.0.15} covers only half of block 0 (/27) so
						// the block remains allocatable. Both DPUClusters get a clean /23 split;
						// the partial exclusion is propagated into each CIDRPool's exclusions.
						testCaseCIDRPool{
							spec: &dpuservicev1.IPV4Network{
								Network:       "10.0.0.0/22",
								PrefixSize:    27,
								ExcludeRanges: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.15"}},
							},
							numOfSubnetsPerDPUCluster: 16,

							perClusterSettings: []perClusterSetting{
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.15"}, // user excluded range
										{StartIP: "10.0.2.0", EndIP: "10.0.3.255"},
									},
								},
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.3.255"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
								},
							},
						},
					),
					Entry("exact network size to accommodate DPUClusters, exclusions matching block per node",
						// Block 0 (10.0.0.0/27 = one per-node block) is fully excluded. The first
						// DPUCluster borrows block 16 (10.0.2.0/27) from the second /23. The last
						// DPUCluster receives 15 blocks.
						testCaseCIDRPool{
							spec: &dpuservicev1.IPV4Network{
								Network:       "10.0.0.0/22",
								PrefixSize:    27,
								ExcludeRanges: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.31"}},
							},
							numOfSubnetsPerDPUCluster: 16,

							perClusterSettings: []perClusterSetting{
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.32", EndIP: "10.0.2.31"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.31"}, // user excluded range
										{StartIP: "10.0.2.32", EndIP: "10.0.3.255"},
									},
								},
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.32", EndIP: "10.0.3.255"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.0.0", EndIP: "10.0.2.31"}},
								},
							},
						},
					),
					Entry("exact network size to accommodate DPUClusters, exclusions matching block per cluster",
						// The first /23 (10.0.0.0/23 = one per-cluster sub-network) is fully excluded.
						// Only the second /23 remains allocatable: the first DPUCluster gets it and
						// the second errors because no blocks remain.
						testCaseCIDRPool{
							spec: &dpuservicev1.IPV4Network{
								Network:       "10.0.0.0/22",
								PrefixSize:    27,
								ExcludeRanges: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
							},
							numOfSubnetsPerDPUCluster: 16,

							perClusterSettings: []perClusterSetting{
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.3.255"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
								},
								{wantErr: true},
							},
						},
					),
					Entry("/30 network, /31 per node (2 IPs), 1 block per DPUCluster",
						// /30 parent (4 IPs) with PrefixSize /31 (2 IPs per node) and numOfSubnetsPerDPUCluster=1
						// (1 block per DPUCluster). 2 blocks fit; each DPUCluster receives one /31 block.
						testCaseCIDRPool{
							spec:                      &dpuservicev1.IPV4Network{Network: "10.0.0.0/30", PrefixSize: 31},
							numOfSubnetsPerDPUCluster: 1,

							perClusterSettings: []perClusterSetting{
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.1"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.0.2", EndIP: "10.0.0.3"}},
								},
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.2", EndIP: "10.0.0.3"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.1"}},
								},
							},
						},
					),
					Entry("/31 network, /32 per node (1 IP), 1 block per DPUCluster",
						// /31 parent (2 IPs) with PrefixSize /32 (1 IP per node) and numOfSubnetsPerDPUCluster=1
						// (1 block per DPUCluster). 2 single-IP blocks fit; each DPUCluster receives one IP.
						testCaseCIDRPool{
							spec:                      &dpuservicev1.IPV4Network{Network: "10.0.0.0/31", PrefixSize: 32},
							numOfSubnetsPerDPUCluster: 1,

							perClusterSettings: []perClusterSetting{
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.0"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.1"}},
								},
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.1"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.0"}},
								},
							},
						},
					),
					Entry("small network that don't fit the given DPUClusters, no exclusions",
						// /24 = exactly 1 /24 per-cluster sub-network. The first DPUCluster claims
						// all 8 /27 blocks; the second Allocate call finds nothing left and errors.
						testCaseCIDRPool{
							spec:                      &dpuservicev1.IPV4Network{Network: "10.0.0.0/24", PrefixSize: 27},
							numOfSubnetsPerDPUCluster: 8,

							perClusterSettings: []perClusterSetting{
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.255"}},
									expectedExclusions:  nil,
								},
								{wantErr: true},
							},
						},
					),
					Entry("user-excluded block also targeted by static allocation — user exclusion takes priority over static allocation",
						// /28 parent, /30 per node, 2 blocks per cluster. User excludes .4/30.
						// node-a → .4/30 (user-excluded): the carve-out removes the .4-.7 partition gap
						// but the user ExcludeRange is merged back in, so .4-.7 remains excluded.
						// node-b → .8/30 (cluster 0's sequential block): carved from cluster 1's
						// partition exclusion {.0,.11}.
						// Cluster 0 sequential (excluded .4/30 skipped): {.0,.3} + {.8,.11}.
						// Cluster 0 exclusions: [{.4,.7}] user + [{.12,.15}] partition.
						// Cluster 1 sequential (only remaining block): {.12,.15}.
						// Cluster 1 exclusions: [{.0,.7}] — partition {.0,.3} and user {.4,.7} are
						// adjacent and merge; {.8,.11} is carved by node-b's static allocation.
						testCaseCIDRPool{
							spec: &dpuservicev1.IPV4Network{
								Network:       "10.0.0.0/28",
								PrefixSize:    30,
								ExcludeRanges: []dpuservicev1.IPRange{{StartIP: "10.0.0.4", EndIP: "10.0.0.7"}},
								Allocations:   map[string]string{"node-a": "10.0.0.4/30", "node-b": "10.0.0.8/30"},
							},
							numOfSubnetsPerDPUCluster: 2,

							perClusterSettings: []perClusterSetting{
								{
									expectedAllocations: []dpuservicev1.IPRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.3"},
										{StartIP: "10.0.0.8", EndIP: "10.0.0.11"},
									},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.4", EndIP: "10.0.0.7"},
										{StartIP: "10.0.0.12", EndIP: "10.0.0.15"},
									},
								},
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.12", EndIP: "10.0.0.15"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.7"}},
								},
							},
						},
					),
				)
				DescribeTable("Allocate returns correct results with 1 DPUCluster",
					runTestCaseCIDRPoolSingleCluster,
					Entry("no exclusions, no existing allocations — fills parent exactly, no exclusions",
						// /22 splits cleanly into 4 /24 subnets. Single cluster gets all 4;
						// first and last blocks coincide with the parent boundary so no exclusions are produced.
						testCaseCIDRPool{
							spec: &dpuservicev1.IPV4Network{Network: "10.0.0.0/22", PrefixSize: 24},
							perClusterSettings: []perClusterSetting{
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.3.255"}},
									expectedExclusions:  nil,
								},
							},
						},
					),
					Entry("larger network, no exclusions — fills parent exactly, no exclusions",
						// /21 splits cleanly into 64 /27 blocks. Single cluster gets all;
						// no exclusions produced.
						testCaseCIDRPool{
							spec: &dpuservicev1.IPV4Network{Network: "10.0.0.0/21", PrefixSize: 27},
							perClusterSettings: []perClusterSetting{
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.7.255"}},
									expectedExclusions:  nil,
								},
							},
						},
					),
					Entry("user exclusion partially covering a block",
						// ExcludeRange {10.0.0.0, 10.0.0.15} partially covers block 0 (/27 = .0–.31)
						// so the block remains allocatable. The user exclusion appears in the output;
						// no before/after exclusions since the cluster fills the full parent.
						testCaseCIDRPool{
							spec: &dpuservicev1.IPV4Network{
								Network:       "10.0.0.0/21",
								PrefixSize:    27,
								ExcludeRanges: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.15"}},
							},
							perClusterSettings: []perClusterSetting{
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.7.255"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.15"}},
								},
							},
						},
					),
					Entry("idempotent — existing allocation is preserved",
						// Cluster already owns the full /22. The calculator returns the same
						// cluster-block without consuming new pool space.
						testCaseCIDRPool{
							spec: &dpuservicev1.IPV4Network{Network: "10.0.0.0/22", PrefixSize: 24},
							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.3.255"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.3.255"}},
									expectedExclusions:  nil,
								},
							},
						},
					),
					Entry("static allocation pointing to a user-excluded block — user exclusion takes priority",
						// /28 parent, /30 per node, single cluster owns all available blocks.
						// The user excludes .4/30 via ExcludeRanges, so sequential allocation
						// skips that block (result: {.0,.3} + {.8,.15}). A static allocation
						// also pins "node-a" to that same block, but user-provided ExcludeRanges
						// have higher priority than static allocations: the carve-out only applies
						// to per-cluster gap exclusions, not to user exclusions. The .4-.7 range
						// therefore remains excluded in the output.
						testCaseCIDRPool{
							spec: &dpuservicev1.IPV4Network{
								Network:       "10.0.0.0/28",
								PrefixSize:    30,
								ExcludeRanges: []dpuservicev1.IPRange{{StartIP: "10.0.0.4", EndIP: "10.0.0.7"}},
								Allocations:   map[string]string{"node-a": "10.0.0.4/30"},
							},
							perClusterSettings: []perClusterSetting{
								{
									expectedAllocations: []dpuservicev1.IPRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.3"},
										{StartIP: "10.0.0.8", EndIP: "10.0.0.15"},
									},
									expectedExclusions: []nvipamv1.ExcludeRange{{StartIP: "10.0.0.4", EndIP: "10.0.0.7"}},
								},
							},
						},
					),
				)
			})

			Context("existing DPUServiceIPAM - DPUCluster operations", func() {
				DescribeTable("Allocate returns correct results with 1 DPUCluster that has already allocations when 1 DPUCluster is added",
					runTestCaseCIDRPool,
					Entry("network with buffer to accommodate more DPUClusters, no exclusions",
						// /21 holds 4 /23 slots; cluster 0 already owns the first one.
						// New cluster gets the second; the upper half remains unused as buffer.
						testCaseCIDRPool{
							spec:                      &dpuservicev1.IPV4Network{Network: "10.0.0.0/21", PrefixSize: 27},
							numOfSubnetsPerDPUCluster: 16,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.2.0", EndIP: "10.0.7.255"}},
								},
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.3.255"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.1.255"},
										{StartIP: "10.0.4.0", EndIP: "10.0.7.255"},
									},
								},
							},
						},
					),
					Entry("network with buffer to accommodate more DPUClusters, exclusions matching subset of block per node",
						// /21, /23, /27. Cluster 0 existing; partial exclusion in its range.
						// Block 0 partially excluded → all 16 blocks still allocatable → no deficit.
						// New cluster gets next /23 from buffer space.
						testCaseCIDRPool{
							spec: &dpuservicev1.IPV4Network{
								Network: "10.0.0.0/21", PrefixSize: 27,
								ExcludeRanges: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.15"}},
							},
							numOfSubnetsPerDPUCluster: 16,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.15"},
										{StartIP: "10.0.2.0", EndIP: "10.0.7.255"},
									},
								},
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.3.255"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.1.255"},
										{StartIP: "10.0.4.0", EndIP: "10.0.7.255"},
									},
								},
							},
						},
					),
					Entry("network with buffer to accommodate more DPUClusters, exclusions matching block per node",
						// /21, /23, /27. Cluster 0 was originally allocated with block 0 excluded;
						// it received {10.0.0.32, 10.0.2.31}. No deficit on reconcile. New cluster
						// gets the next 16 blocks from buffer space.
						testCaseCIDRPool{
							spec: &dpuservicev1.IPV4Network{
								Network: "10.0.0.0/21", PrefixSize: 27,
								ExcludeRanges: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.31"}},
							},
							numOfSubnetsPerDPUCluster: 16,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.32", EndIP: "10.0.2.31"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.32", EndIP: "10.0.2.31"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.31"},
										{StartIP: "10.0.2.32", EndIP: "10.0.7.255"},
									},
								},
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.32", EndIP: "10.0.4.31"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.2.31"},
										{StartIP: "10.0.4.32", EndIP: "10.0.7.255"},
									},
								},
							},
						},
					),
					Entry("network with buffer to accommodate more DPUClusters, exclusions matching block per cluster",
						// /21, /23, /27. Cluster 0 was originally allocated with the first /23 excluded;
						// it received {10.0.2.0, 10.0.3.255}. Exclusion still present → preserved.
						testCaseCIDRPool{
							spec: &dpuservicev1.IPV4Network{
								Network: "10.0.0.0/21", PrefixSize: 27,
								ExcludeRanges: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
							},
							numOfSubnetsPerDPUCluster: 16,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.3.255"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.3.255"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.1.255"},
										{StartIP: "10.0.4.0", EndIP: "10.0.7.255"},
									},
								},
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.4.0", EndIP: "10.0.5.255"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.3.255"},
										{StartIP: "10.0.6.0", EndIP: "10.0.7.255"},
									},
								},
							},
						},
					),
					Entry("exact network size to accommodate DPUClusters, no exclusions",
						// /22, /23, /27. Cluster 0 existing, no exclusions. New cluster gets the second /23.
						testCaseCIDRPool{
							spec:                      &dpuservicev1.IPV4Network{Network: "10.0.0.0/22", PrefixSize: 27},
							numOfSubnetsPerDPUCluster: 16,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.2.0", EndIP: "10.0.3.255"}},
								},
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.3.255"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
								},
							},
						},
					),
					Entry("exact network size to accommodate DPUClusters, exclusions matching subset of block per node",
						// /22, /23, /27. Cluster 0 existing; partial exclusion in its range.
						testCaseCIDRPool{
							spec: &dpuservicev1.IPV4Network{
								Network: "10.0.0.0/22", PrefixSize: 27,
								ExcludeRanges: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.15"}},
							},
							numOfSubnetsPerDPUCluster: 16,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.15"},
										{StartIP: "10.0.2.0", EndIP: "10.0.3.255"},
									},
								},
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.3.255"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
								},
							},
						},
					),
					Entry("exact network size to accommodate DPUClusters, exclusions matching block per node",
						// /22, /23, /27. Cluster 0 was originally allocated with block 0 excluded;
						// it received {10.0.0.32, 10.0.2.31}. No deficit. New cluster gets remaining 15 blocks.
						testCaseCIDRPool{
							spec: &dpuservicev1.IPV4Network{
								Network: "10.0.0.0/22", PrefixSize: 27,
								ExcludeRanges: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.31"}},
							},
							numOfSubnetsPerDPUCluster: 16,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.32", EndIP: "10.0.2.31"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.32", EndIP: "10.0.2.31"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.31"},
										{StartIP: "10.0.2.32", EndIP: "10.0.3.255"},
									},
								},
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.32", EndIP: "10.0.3.255"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.0.0", EndIP: "10.0.2.31"}},
								},
							},
						},
					),
					Entry("exact network size to accommodate DPUClusters, exclusions matching block per cluster",
						// /22, /23, /27. Cluster 0 was originally allocated with the first /23 excluded;
						// it received {10.0.2.0, 10.0.3.255}. Preserved. New cluster errors (pool empty).
						testCaseCIDRPool{
							spec: &dpuservicev1.IPV4Network{
								Network: "10.0.0.0/22", PrefixSize: 27,
								ExcludeRanges: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
							},
							numOfSubnetsPerDPUCluster: 16,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.3.255"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.3.255"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
								},
								{wantErr: true},
							},
						},
					),
					Entry("small network that don't fit the given DPUClusters, no exclusions",
						// Cluster 0 already owns the entire /24 (all 8 blocks). New cluster errors.
						testCaseCIDRPool{
							spec:                      &dpuservicev1.IPV4Network{Network: "10.0.0.0/24", PrefixSize: 27},
							numOfSubnetsPerDPUCluster: 8,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.255"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.255"}},
									expectedExclusions:  nil,
								},
								{wantErr: true},
							},
						},
					),
					Entry("/31 network, /32 per node, 1 block per DPUCluster",
						// /31 parent (2 IPs), /32 per node (1 IP per block), 1 block per DPUCluster.
						// Both clusters have existing allocations that exhaust the 2 available blocks;
						// adding a 3rd errors.
						testCaseCIDRPool{
							spec:                      &dpuservicev1.IPV4Network{Network: "10.0.0.0/31", PrefixSize: 32},
							numOfSubnetsPerDPUCluster: 1,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.0"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.0"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.1"}},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.1"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.1", EndIP: "10.0.0.1"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.0"}},
								},
								{wantErr: true},
							},
						},
					),
					Entry("/30 network, /31 per node, 1 block per DPUCluster",
						// /30 parent (4 IPs), /31 per node (2 IPs per block), 1 block per DPUCluster.
						// Both clusters have existing allocations that exhaust the 2 available blocks;
						// adding a 3rd errors.
						testCaseCIDRPool{
							spec:                      &dpuservicev1.IPV4Network{Network: "10.0.0.0/30", PrefixSize: 31},
							numOfSubnetsPerDPUCluster: 1,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.1"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.1"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.0.2", EndIP: "10.0.0.3"}},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.2", EndIP: "10.0.0.3"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.2", EndIP: "10.0.0.3"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.1"}},
								},
								{wantErr: true},
							},
						},
					),
					Entry("static allocation within first cluster's existing range — carved from second cluster's exclusions",
						// /28 parent, /30 per node, 2 blocks per cluster. Cluster 0 already owns
						// .0-.7; a new cluster is added and receives .8-.15. Static alloc pins
						// "node-a" to .4/30, which sits inside cluster 0's range.
						// Cluster 0's exclusion [{.8,.15}] is unaffected — .4/30 is not in it.
						// Cluster 1's exclusion [{.0,.7}] is split: .4-.7 is carved out, leaving [{.0,.3}].
						testCaseCIDRPool{
							spec: &dpuservicev1.IPV4Network{
								Network:     "10.0.0.0/28",
								PrefixSize:  30,
								Allocations: map[string]string{"node-a": "10.0.0.4/30"},
							},
							numOfSubnetsPerDPUCluster: 2,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.7"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.7"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.0.8", EndIP: "10.0.0.15"}},
								},
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.8", EndIP: "10.0.0.15"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.3"}},
								},
							},
						},
					),
					Entry("static allocation pointing into second cluster's range — carved from first cluster's exclusions",
						// /28 parent, /30 per node, 2 blocks per cluster. Cluster 0 already owns
						// .0-.7; a new cluster is added and receives .8-.15. Static alloc pins
						// "node-a" to .8/30, which sits inside cluster 1's range.
						// Cluster 0's exclusion [{.8,.15}] is split: .8-.11 is carved out, leaving [{.12,.15}].
						// Cluster 1's exclusion [{.0,.7}] is unaffected — .8/30 is not in it.
						testCaseCIDRPool{
							spec: &dpuservicev1.IPV4Network{
								Network:     "10.0.0.0/28",
								PrefixSize:  30,
								Allocations: map[string]string{"node-a": "10.0.0.8/30"},
							},
							numOfSubnetsPerDPUCluster: 2,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.7"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.7"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.0.12", EndIP: "10.0.0.15"}},
								},
								{
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.8", EndIP: "10.0.0.15"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.7"}},
								},
							},
						},
					),
				)
				DescribeTable("Allocate returns correct results with 3 DPUCluster that have already allocations when the middle DPUCluster is removed",
					runTestCaseCIDRPool,
					Entry("network with buffer to accommodate more DPUClusters, no exclusions",
						// /21, /24, /27. Clusters 0 and 2 retain their /24 allocations after the
						// middle cluster (slot 1) is deleted. Freed slot 1 goes to pool (unused here).
						testCaseCIDRPool{
							spec:                      &dpuservicev1.IPV4Network{Network: "10.0.0.0/21", PrefixSize: 27},
							numOfSubnetsPerDPUCluster: 8,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.255"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.255"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.1.0", EndIP: "10.0.7.255"}},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.2.255"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.2.255"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.1.255"},
										{StartIP: "10.0.3.0", EndIP: "10.0.7.255"},
									},
								},
							},
						},
					),
					Entry("network with buffer to accommodate more DPUClusters, exclusions matching subset of block per node",
						// Partial exclusion in slot 0 range; initial alloc unchanged. Both surviving
						// clusters preserved; partial exclusion propagated to CIDRPool.
						testCaseCIDRPool{
							spec: &dpuservicev1.IPV4Network{
								Network: "10.0.0.0/21", PrefixSize: 27,
								ExcludeRanges: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.15"}},
							},
							numOfSubnetsPerDPUCluster: 8,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.255"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.255"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.15"},
										{StartIP: "10.0.1.0", EndIP: "10.0.7.255"},
									},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.2.255"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.2.255"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.1.255"},
										{StartIP: "10.0.3.0", EndIP: "10.0.7.255"},
									},
								},
							},
						},
					),
					Entry("network with buffer to accommodate more DPUClusters, exclusions matching block per node",
						// Block 0 excluded during initial alloc; all 3 clusters shifted by 1 block.
						// Surviving clusters 0 and 2 retain their shifted allocations.
						testCaseCIDRPool{
							spec: &dpuservicev1.IPV4Network{
								Network: "10.0.0.0/21", PrefixSize: 27,
								ExcludeRanges: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.31"}},
							},
							numOfSubnetsPerDPUCluster: 8,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.32", EndIP: "10.0.1.31"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.32", EndIP: "10.0.1.31"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.31"},
										{StartIP: "10.0.1.32", EndIP: "10.0.7.255"},
									},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.32", EndIP: "10.0.3.31"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.32", EndIP: "10.0.3.31"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.2.31"},
										{StartIP: "10.0.3.32", EndIP: "10.0.7.255"},
									},
								},
							},
						},
					),
					Entry("network with buffer to accommodate more DPUClusters, exclusions matching block per cluster",
						// First /24 excluded; all 3 clusters skipped slot 0 initially. Surviving
						// clusters 0 and 2 retain their allocations in slots 1 and 3.
						testCaseCIDRPool{
							spec: &dpuservicev1.IPV4Network{
								Network: "10.0.0.0/21", PrefixSize: 27,
								ExcludeRanges: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.255"}},
							},
							numOfSubnetsPerDPUCluster: 8,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.1.0", EndIP: "10.0.1.255"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.1.0", EndIP: "10.0.1.255"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.255"},
										{StartIP: "10.0.2.0", EndIP: "10.0.7.255"},
									},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.3.0", EndIP: "10.0.3.255"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.3.0", EndIP: "10.0.3.255"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.2.255"},
										{StartIP: "10.0.4.0", EndIP: "10.0.7.255"},
									},
								},
							},
						},
					),
					Entry("exact network size to accommodate DPUClusters, no exclusions",
						// /22, /24, /27. Slots 0,1,2 used (slot 3 spare). After middle deleted: clusters
						// 0 and 2 preserved; freed slot 1 and spare slot 3 go to pool (unused here).
						testCaseCIDRPool{
							spec:                      &dpuservicev1.IPV4Network{Network: "10.0.0.0/22", PrefixSize: 27},
							numOfSubnetsPerDPUCluster: 8,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.255"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.255"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.1.0", EndIP: "10.0.3.255"}},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.2.255"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.2.255"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.1.255"},
										{StartIP: "10.0.3.0", EndIP: "10.0.3.255"},
									},
								},
							},
						},
					),
					Entry("exact network size to accommodate DPUClusters, exclusions matching subset of block per node",
						// Partial exclusion in slot 0 range; initial alloc unchanged. Both surviving
						// clusters preserved; partial exclusion propagated to each CIDRPool.
						testCaseCIDRPool{
							spec: &dpuservicev1.IPV4Network{
								Network: "10.0.0.0/22", PrefixSize: 27,
								ExcludeRanges: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.15"}},
							},
							numOfSubnetsPerDPUCluster: 8,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.255"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.255"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.15"},
										{StartIP: "10.0.1.0", EndIP: "10.0.3.255"},
									},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.2.255"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.2.255"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.1.255"},
										{StartIP: "10.0.3.0", EndIP: "10.0.3.255"},
									},
								},
							},
						},
					),
					Entry("exact network size to accommodate DPUClusters, exclusions matching block per node",
						// Block 0 excluded during initial alloc; all 3 clusters shifted by 1 block.
						// Surviving clusters 0 and 2 retain their shifted allocations.
						testCaseCIDRPool{
							spec: &dpuservicev1.IPV4Network{
								Network: "10.0.0.0/22", PrefixSize: 27,
								ExcludeRanges: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.31"}},
							},
							numOfSubnetsPerDPUCluster: 8,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.32", EndIP: "10.0.1.31"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.32", EndIP: "10.0.1.31"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.31"},
										{StartIP: "10.0.1.32", EndIP: "10.0.3.255"},
									},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.32", EndIP: "10.0.3.31"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.32", EndIP: "10.0.3.31"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.2.31"},
										{StartIP: "10.0.3.32", EndIP: "10.0.3.255"},
									},
								},
							},
						},
					),
					Entry("static allocation within the first surviving cluster's range — carved from second surviving cluster's exclusions",
						// /27 parent, /30 per node, 2 blocks per cluster. Clusters 0 and 2 survive
						// after the middle cluster is removed. A static alloc pins "node-a" to .4/30
						// (inside cluster 0's {.0,.7} range).
						// Cluster 0's exclusion [{.8,.31}] is unaffected — .4/30 is not in it.
						// Cluster 2's exclusion [{.0,.15},{.24,.31}] is split: .4-.7 is carved from
						// the tail of {.0,.15}, leaving {.0,.3} + {.8,.15}.
						testCaseCIDRPool{
							spec: &dpuservicev1.IPV4Network{
								Network:     "10.0.0.0/27",
								PrefixSize:  30,
								Allocations: map[string]string{"node-a": "10.0.0.4/30"},
							},
							numOfSubnetsPerDPUCluster: 2,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.7"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.7"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.0.8", EndIP: "10.0.0.31"}},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.16", EndIP: "10.0.0.23"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.16", EndIP: "10.0.0.23"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.3"},
										{StartIP: "10.0.0.8", EndIP: "10.0.0.15"},
										{StartIP: "10.0.0.24", EndIP: "10.0.0.31"},
									},
								},
							},
						},
					),
					Entry("static allocation pointing into the removed cluster's range — carved from both surviving clusters' exclusions",
						// /27 parent, /30 per node, 2 blocks per cluster. Clusters 0 and 2 survive.
						// A static alloc pins "node-b" to .8/30, which was inside the removed cluster's
						// {.8,.15} range.
						// Cluster 0's exclusion [{.8,.31}] has .8-.11 carved from the head → [{.12,.31}].
						// Cluster 2's exclusion [{.0,.15},{.24,.31}] has .8-.11 carved from the middle
						// of {.0,.15} → {.0,.7} + {.12,.15}.
						testCaseCIDRPool{
							spec: &dpuservicev1.IPV4Network{
								Network:     "10.0.0.0/27",
								PrefixSize:  30,
								Allocations: map[string]string{"node-b": "10.0.0.8/30"},
							},
							numOfSubnetsPerDPUCluster: 2,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.7"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.7"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.0.12", EndIP: "10.0.0.31"}},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.16", EndIP: "10.0.0.23"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.16", EndIP: "10.0.0.23"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.7"},
										{StartIP: "10.0.0.12", EndIP: "10.0.0.15"},
										{StartIP: "10.0.0.24", EndIP: "10.0.0.31"},
									},
								},
							},
						},
					),
					Entry("exact network size to accommodate DPUClusters, exclusions matching block per cluster",
						// First /24 excluded; all 3 clusters skipped slot 0 initially. Surviving
						// clusters 0 and 2 retain their allocations in slots 1 and 3.
						testCaseCIDRPool{
							spec: &dpuservicev1.IPV4Network{
								Network: "10.0.0.0/22", PrefixSize: 27,
								ExcludeRanges: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.255"}},
							},
							numOfSubnetsPerDPUCluster: 8,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.1.0", EndIP: "10.0.1.255"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.1.0", EndIP: "10.0.1.255"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.255"},
										{StartIP: "10.0.2.0", EndIP: "10.0.3.255"},
									},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.3.0", EndIP: "10.0.3.255"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.3.0", EndIP: "10.0.3.255"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.0.0", EndIP: "10.0.2.255"}},
								},
							},
						},
					),
				)
			})
			Context("existing DPUServiceIPAM - existing DPUClusters - updates on DPUServiceIPAM", func() {
				DescribeTable("Allocate returns correct results with 2 DPUClusters that have already allocations and DPUServiceIPAM settings are changed",
					runTestCaseCIDRPool,
					Entry("network changes",
						// The Network field changes to a completely different address space. Existing
						// allocations in 10.0.0.0/22 are outside the new 192.168.0.0/22 parent →
						// clusterBlocksCompatible returns false → both clusters fall through to fresh
						// allocations in the new network.
						testCaseCIDRPool{
							spec:                      &dpuservicev1.IPV4Network{Network: "192.168.0.0/22", PrefixSize: 27},
							numOfSubnetsPerDPUCluster: 16,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "192.168.0.0", EndIP: "192.168.1.255"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "192.168.2.0", EndIP: "192.168.3.255"}},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.3.255"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "192.168.2.0", EndIP: "192.168.3.255"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "192.168.0.0", EndIP: "192.168.1.255"}},
								},
							},
						},
					),
					Entry("network grows",
						// /22 grows to /21; existing /22 allocations are within the new parent and
						// grid-aligned → preserved unchanged. The upper /21 half becomes spare buffer.
						testCaseCIDRPool{
							spec:                      &dpuservicev1.IPV4Network{Network: "10.0.0.0/21", PrefixSize: 27},
							numOfSubnetsPerDPUCluster: 16,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.2.0", EndIP: "10.0.7.255"}},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.3.255"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.3.255"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.1.255"},
										{StartIP: "10.0.4.0", EndIP: "10.0.7.255"},
									},
								},
							},
						},
					),
					Entry("prefixSize grows",
						// Per-node network size grows: prefix length /27 → /26 (64 IPs per node instead of 32).
						// Both /23 cluster-blocks span exactly 8 new /26 blocks (512/64=8=target) → no deficit, preserved.
						testCaseCIDRPool{
							spec:                      &dpuservicev1.IPV4Network{Network: "10.0.0.0/22", PrefixSize: 26},
							numOfSubnetsPerDPUCluster: 8,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.2.0", EndIP: "10.0.3.255"}},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.3.255"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.3.255"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
								},
							},
						},
					),
					Entry("numOfSubnetsPerDPUCluster grows",
						// numOfSubnetsPerDPUCluster grows from 16 to 32 — each DPUCluster now receives 32 /27 blocks.
						// Each cluster now needs 32 /27 blocks; deficit=16 → topped up from upper /21 half.
						// Resulting allocations are non-contiguous (original /23 + top-up /23).
						testCaseCIDRPool{
							spec:                      &dpuservicev1.IPV4Network{Network: "10.0.0.0/21", PrefixSize: 27},
							numOfSubnetsPerDPUCluster: 32,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
									expectedAllocations: []dpuservicev1.IPRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.1.255"},
										{StartIP: "10.0.4.0", EndIP: "10.0.5.255"},
									},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.2.0", EndIP: "10.0.3.255"},
										{StartIP: "10.0.6.0", EndIP: "10.0.7.255"},
									},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.3.255"}},
									expectedAllocations: []dpuservicev1.IPRange{
										{StartIP: "10.0.2.0", EndIP: "10.0.3.255"},
										{StartIP: "10.0.6.0", EndIP: "10.0.7.255"},
									},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.1.255"},
										{StartIP: "10.0.4.0", EndIP: "10.0.5.255"},
									},
								},
							},
						},
					),
					Entry("network with buffer to accommodate more DPUClusters, exclusions matching subset of block per node",
						// Partial exclusion added to /21. All 16 blocks in each cluster's range remain
						// allocatable → no deficit; both preserved. Exclusion propagated to CIDRPools.
						testCaseCIDRPool{
							spec: &dpuservicev1.IPV4Network{
								Network: "10.0.0.0/21", PrefixSize: 27,
								ExcludeRanges: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.15"}},
							},
							numOfSubnetsPerDPUCluster: 16,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.15"},
										{StartIP: "10.0.2.0", EndIP: "10.0.7.255"},
									},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.3.255"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.3.255"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.1.255"},
										{StartIP: "10.0.4.0", EndIP: "10.0.7.255"},
									},
								},
							},
						},
					),
					Entry("network with buffer to accommodate more DPUClusters, exclusions matching block per node",
						// Block 0 excluded in /21 after initial alloc. Deficit=1 for cluster 0; topped up
						// with block 32 from spare /21 upper half. Non-contiguous allocation results.
						testCaseCIDRPool{
							spec: &dpuservicev1.IPV4Network{
								Network: "10.0.0.0/21", PrefixSize: 27,
								ExcludeRanges: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.31"}},
							},
							numOfSubnetsPerDPUCluster: 16,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
									expectedAllocations: []dpuservicev1.IPRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.1.255"},
										{StartIP: "10.0.4.0", EndIP: "10.0.4.31"},
									},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.31"},
										{StartIP: "10.0.2.0", EndIP: "10.0.3.255"},
										{StartIP: "10.0.4.32", EndIP: "10.0.7.255"},
									},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.3.255"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.3.255"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.1.255"},
										{StartIP: "10.0.4.0", EndIP: "10.0.7.255"},
									},
								},
							},
						},
					),
					Entry("network with buffer to accommodate more DPUClusters, exclusions matching block per cluster",
						// Full /23 excluded in /21 after initial alloc. Cluster 0's entire range is now
						// fully excluded → falls through to spare blocks in the upper /21 half.
						testCaseCIDRPool{
							spec: &dpuservicev1.IPV4Network{
								Network: "10.0.0.0/21", PrefixSize: 27,
								ExcludeRanges: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
							},
							numOfSubnetsPerDPUCluster: 16,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.4.0", EndIP: "10.0.5.255"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.3.255"},
										{StartIP: "10.0.6.0", EndIP: "10.0.7.255"},
									},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.3.255"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.3.255"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.1.255"},
										{StartIP: "10.0.4.0", EndIP: "10.0.7.255"},
									},
								},
							},
						},
					),
					Entry("exact network size to accommodate DPUClusters, exclusions matching subset of block per node",
						// Partial exclusion added to /22. No full blocks excluded → no deficit; preserved.
						testCaseCIDRPool{
							spec: &dpuservicev1.IPV4Network{
								Network: "10.0.0.0/22", PrefixSize: 27,
								ExcludeRanges: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.15"}},
							},
							numOfSubnetsPerDPUCluster: 16,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.15"},
										{StartIP: "10.0.2.0", EndIP: "10.0.3.255"},
									},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.3.255"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.3.255"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
								},
							},
						},
					),
					Entry("exact network size to accommodate DPUClusters, exclusions matching block per node",
						// Block 0 excluded in exact /22. Deficit=1 for cluster 0; pool empty
						// (both clusters pre-take all blocks) → no top-up; 15 usable blocks remain.
						testCaseCIDRPool{
							spec: &dpuservicev1.IPV4Network{
								Network: "10.0.0.0/22", PrefixSize: 27,
								ExcludeRanges: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.31"}},
							},
							numOfSubnetsPerDPUCluster: 16,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
									expectedExclusions: []nvipamv1.ExcludeRange{
										{StartIP: "10.0.0.0", EndIP: "10.0.0.31"},
										{StartIP: "10.0.2.0", EndIP: "10.0.3.255"},
									},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.3.255"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.3.255"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
								},
							},
						},
					),
					Entry("static allocation added within first cluster's own range — second cluster's exclusion carved",
						// /28 parent, /30 per node, 2 blocks per cluster. Both clusters already have
						// their allocations. A static alloc for "node-a" → .4/30 is added to the spec
						// (within cluster 0's .0-.7 range). Both clusters preserve existing allocations
						// via the top-up path (no deficit). Cluster 0's exclusion [{.8,.15}] is
						// unaffected; cluster 1's exclusion [{.0,.7}] is split at the static block:
						// .4-.7 is carved from the tail, leaving [{.0,.3}].
						testCaseCIDRPool{
							spec: &dpuservicev1.IPV4Network{
								Network:     "10.0.0.0/28",
								PrefixSize:  30,
								Allocations: map[string]string{"node-a": "10.0.0.4/30"},
							},
							numOfSubnetsPerDPUCluster: 2,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.7"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.7"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.0.8", EndIP: "10.0.0.15"}},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.8", EndIP: "10.0.0.15"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.8", EndIP: "10.0.0.15"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.3"}},
								},
							},
						},
					),
					Entry("static allocation added pointing into second cluster's range — first cluster's exclusion carved",
						// /28 parent, /30 per node, 2 blocks per cluster. Both clusters already have
						// their allocations. A static alloc for "node-a" → .8/30 is added to the spec
						// (within cluster 1's .8-.15 range). Both clusters preserve existing allocations
						// via the top-up path. Cluster 0's exclusion [{.8,.15}] is split: .8-.11 is
						// carved from the head, leaving [{.12,.15}]. Cluster 1's exclusion [{.0,.7}]
						// is unaffected — .8/30 is not in it.
						testCaseCIDRPool{
							spec: &dpuservicev1.IPV4Network{
								Network:     "10.0.0.0/28",
								PrefixSize:  30,
								Allocations: map[string]string{"node-a": "10.0.0.8/30"},
							},
							numOfSubnetsPerDPUCluster: 2,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.7"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.7"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.0.12", EndIP: "10.0.0.15"}},
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.8", EndIP: "10.0.0.15"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.8", EndIP: "10.0.0.15"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.0.0", EndIP: "10.0.0.7"}},
								},
							},
						},
					),
					Entry("exact network size to accommodate DPUClusters, exclusions matching block per cluster",
						// Full /23 excluded in exact /22 after initial alloc. Cluster 0's entire range is
						// fully excluded → falls through; pool is empty (cluster 1 pre-takes remaining blocks) → error.
						testCaseCIDRPool{
							spec: &dpuservicev1.IPV4Network{
								Network: "10.0.0.0/22", PrefixSize: 27,
								ExcludeRanges: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
							},
							numOfSubnetsPerDPUCluster: 16,

							perClusterSettings: []perClusterSetting{
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
									wantErr:             true,
								},
								{
									existingAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.3.255"}},
									expectedAllocations: []dpuservicev1.IPRange{{StartIP: "10.0.2.0", EndIP: "10.0.3.255"}},
									expectedExclusions:  []nvipamv1.ExcludeRange{{StartIP: "10.0.0.0", EndIP: "10.0.1.255"}},
								},
							},
						},
					),
				)
			})
		})
	})

	Context("helpers", func() {
		r := func(s, e string) iputils.IPRange {
			return iputils.IPRange{Start: iputils.IPv4ToUint32(net.ParseIP(s)), End: iputils.IPv4ToUint32(net.ParseIP(e))}
		}

		Context("mergeRanges", func() {
			DescribeTable("merges exclusion ranges correctly",
				func(input []iputils.IPRange, want []iputils.IPRange) {
					Expect(iputils.MergeRanges(input)).To(Equal(want))
				},
				Entry("empty input returns nil",
					nil,
					nil,
				),
				Entry("single range is returned unchanged",
					[]iputils.IPRange{r("10.0.0.0", "10.0.0.31")},
					[]iputils.IPRange{r("10.0.0.0", "10.0.0.31")},
				),
				Entry("non-overlapping ranges are preserved",
					[]iputils.IPRange{r("10.0.0.0", "10.0.0.31"), r("10.0.0.64", "10.0.0.95")},
					[]iputils.IPRange{r("10.0.0.0", "10.0.0.31"), r("10.0.0.64", "10.0.0.95")},
				),
				Entry("one free IP between two ranges prevents merging",
					[]iputils.IPRange{r("10.0.0.0", "10.0.0.30"), r("10.0.0.32", "10.0.0.63")},
					[]iputils.IPRange{r("10.0.0.0", "10.0.0.30"), r("10.0.0.32", "10.0.0.63")},
				),
				Entry("overlapping ranges are merged",
					[]iputils.IPRange{r("10.0.0.0", "10.0.0.63"), r("10.0.0.32", "10.0.0.95")},
					[]iputils.IPRange{r("10.0.0.0", "10.0.0.95")},
				),
				Entry("adjacent ranges are merged",
					[]iputils.IPRange{r("10.0.0.0", "10.0.0.31"), r("10.0.0.32", "10.0.0.63")},
					[]iputils.IPRange{r("10.0.0.0", "10.0.0.63")},
				),
				Entry("out-of-order input is sorted before merging",
					[]iputils.IPRange{r("10.0.0.64", "10.0.0.95"), r("10.0.0.0", "10.0.0.31"), r("10.0.0.32", "10.0.0.63")},
					[]iputils.IPRange{r("10.0.0.0", "10.0.0.95")},
				),
				Entry("contained range does not extend the merged end",
					[]iputils.IPRange{r("10.0.0.0", "10.0.0.95"), r("10.0.0.32", "10.0.0.63")},
					[]iputils.IPRange{r("10.0.0.0", "10.0.0.95")},
				),
				Entry("three disjoint ranges remain separate",
					[]iputils.IPRange{r("10.0.0.0", "10.0.0.31"), r("10.0.0.64", "10.0.0.95"), r("10.0.0.128", "10.0.0.159")},
					[]iputils.IPRange{r("10.0.0.0", "10.0.0.31"), r("10.0.0.64", "10.0.0.95"), r("10.0.0.128", "10.0.0.159")},
				),
			)
		})

		Context("subtractRange", func() {
			DescribeTable("removes sub from the exclusion list, splitting overlapping ranges as needed",
				func(exclusions []iputils.IPRange, sub iputils.IPRange, want []iputils.IPRange) {
					Expect(iputils.SubtractRange(exclusions, sub)).To(Equal(want))
				},
				Entry("empty exclusions returns nil",
					nil,
					r("10.0.0.0", "10.0.0.31"),
					nil,
				),
				Entry("sub entirely before all exclusions — all kept unchanged",
					[]iputils.IPRange{r("10.0.0.64", "10.0.0.95")},
					r("10.0.0.0", "10.0.0.31"),
					[]iputils.IPRange{r("10.0.0.64", "10.0.0.95")},
				),
				Entry("sub entirely after all exclusions — all kept unchanged",
					[]iputils.IPRange{r("10.0.0.0", "10.0.0.31")},
					r("10.0.0.64", "10.0.0.95"),
					[]iputils.IPRange{r("10.0.0.0", "10.0.0.31")},
				),
				Entry("sub exactly matches exclusion — exclusion is removed",
					[]iputils.IPRange{r("10.0.0.0", "10.0.0.31")},
					r("10.0.0.0", "10.0.0.31"),
					nil,
				),
				Entry("sub fully covers exclusion — exclusion is removed",
					[]iputils.IPRange{r("10.0.0.8", "10.0.0.23")},
					r("10.0.0.0", "10.0.0.31"),
					nil,
				),
				Entry("sub covers left part of exclusion — right tail kept",
					[]iputils.IPRange{r("10.0.0.0", "10.0.0.31")},
					r("10.0.0.0", "10.0.0.15"),
					[]iputils.IPRange{r("10.0.0.16", "10.0.0.31")},
				),
				Entry("sub covers right part of exclusion — left tail kept",
					[]iputils.IPRange{r("10.0.0.0", "10.0.0.31")},
					r("10.0.0.16", "10.0.0.31"),
					[]iputils.IPRange{r("10.0.0.0", "10.0.0.15")},
				),
				Entry("sub is in the middle of exclusion — split into two",
					[]iputils.IPRange{r("10.0.0.0", "10.0.0.31")},
					r("10.0.0.8", "10.0.0.23"),
					[]iputils.IPRange{r("10.0.0.0", "10.0.0.7"), r("10.0.0.24", "10.0.0.31")},
				),
				Entry("multiple exclusions — only overlapping one is split, others unchanged",
					[]iputils.IPRange{r("10.0.0.0", "10.0.0.31"), r("10.0.0.64", "10.0.0.95"), r("10.0.0.128", "10.0.0.159")},
					r("10.0.0.16", "10.0.0.79"),
					[]iputils.IPRange{r("10.0.0.0", "10.0.0.15"), r("10.0.0.80", "10.0.0.95"), r("10.0.0.128", "10.0.0.159")},
				),
				Entry("sub spans multiple exclusions — all covered ones removed",
					[]iputils.IPRange{r("10.0.0.0", "10.0.0.31"), r("10.0.0.32", "10.0.0.63"), r("10.0.0.64", "10.0.0.95")},
					r("10.0.0.0", "10.0.0.95"),
					nil,
				),
			)
		})

		Context("isIPv4RangeFullyCovered", func() {
			ip := func(s string) uint32 { return iputils.IPv4ToUint32(net.ParseIP(s)) }
			DescribeTable("reports whether [start, end] is fully covered by one of the merged ranges",
				func(start, end uint32, merged []iputils.IPRange, want bool) {
					Expect((iputils.IPRange{Start: start, End: end}).InAny(merged)).To(Equal(want))
				},
				Entry("empty merged returns false",
					ip("10.0.0.0"), ip("10.0.0.31"), nil, false,
				),
				Entry("exact match returns true",
					ip("10.0.0.0"), ip("10.0.0.31"), []iputils.IPRange{r("10.0.0.0", "10.0.0.31")}, true,
				),
				Entry("query fully inside a larger range returns true",
					ip("10.0.0.8"), ip("10.0.0.23"), []iputils.IPRange{r("10.0.0.0", "10.0.0.31")}, true,
				),
				Entry("covering range is one of many returns true",
					ip("10.0.0.64"), ip("10.0.0.95"),
					[]iputils.IPRange{r("10.0.0.0", "10.0.0.31"), r("10.0.0.64", "10.0.0.127")},
					true,
				),
				Entry("query partially overlaps range returns false",
					ip("10.0.0.16"), ip("10.0.0.47"), []iputils.IPRange{r("10.0.0.0", "10.0.0.31")}, false,
				),
				Entry("query extends beyond range end returns false",
					ip("10.0.0.0"), ip("10.0.0.63"), []iputils.IPRange{r("10.0.0.0", "10.0.0.31")}, false,
				),
				Entry("query starts before range start returns false",
					ip("9.255.255.224"), ip("10.0.0.31"), []iputils.IPRange{r("10.0.0.0", "10.0.0.31")}, false,
				),
				Entry("query entirely before all ranges returns false",
					ip("10.0.0.0"), ip("10.0.0.31"), []iputils.IPRange{r("10.0.0.64", "10.0.0.95")}, false,
				),
				Entry("query entirely after all ranges returns false",
					ip("10.0.0.128"), ip("10.0.0.159"), []iputils.IPRange{r("10.0.0.0", "10.0.0.63")}, false,
				),
			)
		})
	})

	Context("benchmarks", func() {
		It("/8 network, 1-IP blocks, 1k target per DPUCluster", func() {
			experiment := gmeasure.NewExperiment("Construct state and Allocate")
			AddReportEntry(experiment.Name, experiment)

			blocksPerCluster := int32(1000)
			experiment.SampleDuration("Construct without exclusions", func(_ int) {
				_, err := newMultiDPUClusterExclusionCalculator(
					"10.0.0.0/8",
					1,                 // blockSizePerNode: 1 IP — 16M blocks in a /8
					&blocksPerCluster, // targetBlocksPerDPUCluster: 1k
					true,
					nil,
					nil,
					nil,
				)
				Expect(err).NotTo(HaveOccurred())
			}, gmeasure.SamplingConfig{N: 10})

			// 5 non-consecutive /24 exclusions spread across the /8 address space.
			mixedExclusions := []dpuservicev1.IPRange{
				{StartIP: "10.10.0.0", EndIP: "10.10.0.255"},
				{StartIP: "10.50.0.0", EndIP: "10.50.0.255"},
				{StartIP: "10.100.0.0", EndIP: "10.100.0.255"},
				{StartIP: "10.150.0.0", EndIP: "10.150.0.255"},
				{StartIP: "10.200.0.0", EndIP: "10.200.0.255"},
			}
			experiment.SampleDuration("Construct with 5 non-consecutive /24 exclusions", func(_ int) {
				_, err := newMultiDPUClusterExclusionCalculator(
					"10.0.0.0/8",
					1,
					&blocksPerCluster,
					true,
					mixedExclusions,
					nil,
					nil,
				)
				Expect(err).NotTo(HaveOccurred())
			}, gmeasure.SamplingConfig{N: 10})

			// 3 existing cluster allocations: 1000 IPs each, non-overlapping.
			clusterAllocs := [][]dpuservicev1.IPRange{
				{{StartIP: "10.0.0.0", EndIP: "10.0.3.231"}},    // cluster 0: IPs 0–999
				{{StartIP: "10.0.3.232", EndIP: "10.0.7.207"}},  // cluster 1: IPs 1000–1999
				{{StartIP: "10.0.7.208", EndIP: "10.0.11.183"}}, // cluster 2: IPs 2000–2999
			}
			experiment.SampleDuration("Construct with 3 existing cluster allocations", func(_ int) {
				_, err := newMultiDPUClusterExclusionCalculator(
					"10.0.0.0/8",
					1,
					&blocksPerCluster,
					true,
					nil,
					clusterAllocs,
					nil,
				)
				Expect(err).NotTo(HaveOccurred())
			}, gmeasure.SamplingConfig{N: 10})

			experiment.SampleDuration("Construct with 5 non-consecutive /24 exclusions and 3 existing cluster allocations", func(_ int) {
				_, err := newMultiDPUClusterExclusionCalculator(
					"10.0.0.0/8",
					1,
					&blocksPerCluster,
					true,
					mixedExclusions,
					clusterAllocs,
					nil,
				)
				Expect(err).NotTo(HaveOccurred())
			}, gmeasure.SamplingConfig{N: 10})

			// Pre-computed allocation: 1000 single-IP blocks starting at 10.0.0.0 coalesce to one cluster-block.
			existingAlloc := []dpuservicev1.IPRange{{StartIP: "10.0.0.0", EndIP: "10.0.3.231"}}

			// Allocate() for a cluster whose full allocation is already stored — top-up path with
			// zero deficit; the pool cursor does not advance so the calculator can be reused.
			calcExisting, err := newMultiDPUClusterExclusionCalculator(
				"10.0.0.0/8", 1, &blocksPerCluster, true, nil,
				[][]dpuservicev1.IPRange{existingAlloc}, nil,
			)
			Expect(err).NotTo(HaveOccurred())
			experiment.SampleDuration("AllocateClusterBlocks existing cluster", func(_ int) {
				_, err := calcExisting.AllocateClusterBlocks(existingAlloc)
				Expect(err).NotTo(HaveOccurred())
			}, gmeasure.SamplingConfig{N: 100})

			// AllocateClusterBlocks for a brand-new cluster — fresh path consuming 1k blocks from the pool.
			// The cursor is reset between samples to isolate just the AllocateClusterBlocks cost.
			calcFresh, err := newMultiDPUClusterExclusionCalculator(
				"10.0.0.0/8", 1, &blocksPerCluster, true, nil, nil, nil,
			)
			Expect(err).NotTo(HaveOccurred())
			experiment.SampleDuration("AllocateClusterBlocks new cluster", func(_ int) {
				calcFresh.allocatableBlocksPosition = 0
				_, err := calcFresh.AllocateClusterBlocks(nil)
				Expect(err).NotTo(HaveOccurred())
			}, gmeasure.SamplingConfig{N: 100})
		})
	})
})
