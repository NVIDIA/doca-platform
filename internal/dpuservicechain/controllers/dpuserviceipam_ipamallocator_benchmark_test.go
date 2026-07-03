//go:build benchmark

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

// This file holds gmeasure micro-benchmarks for the IPAM exclusion calculator. It is guarded by
// the "benchmark" build tag so it only compiles when that tag is set, keeping it out of the
// default test build.
package controllers

import (
	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gmeasure"
)

var _ = Describe("MultiDPUClusterExclusionCalculator", func() {
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
