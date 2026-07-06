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

package e2e

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
)

//nolint:dupl
var _ = Describe("DPF System tests - Multi DPUCluster", Labels{Domain.MultiDPUCluster}, Ordered, func() {
	BeforeAll(func() {
		if input.NumberOfDPUNodes != 2 {
			Skip("Skip test as exactly 2 nodes are required for multi DPUCluster testing")
		}
	})
	Context("Setup system", Ordered, func() {
		It("create DPFOperatorConfig", func() {
			SystemSetupBeforeSuite(false)
		})
		It("create DPUClusters", func() {
			ProvisionDPUClusters(Ctx, GetProvisionDPUClustersInput())
		})
		It("create BFB and DPUFlavor", func() {
			ProvisionBFBOrBlueFieldSoftwareAndDPUFlavor(Ctx, GetProvisionDPUClustersInput())
		})
		It("create a DPUDeployment with each of DPUs joining a different cluster", func() {
			ProvisionDPUDeploymentWithEachDPUJoiningADifferentDPUCluster(Ctx, input)
		})
	})

	Context("Validate system behavior", Ordered, func() {
		BeforeAll(func() {
			By("Waiting for DPU cluster 0 pods to be ready")
			VerifyClusterPods(Ctx, DPUClusterClient[0], systemPodsToVerify)
			By("Waiting for DPU cluster 1 pods to be ready")
			VerifyClusterPods(Ctx, DPUClusterClient[1], systemPodsToVerify)
			By("Waiting for DPFOperatorConfig to be ready")
			VerifyDPFOperatorConfigReady(Ctx, input.Client, 20*time.Minute)
		})
		It("create single DPUServiceIPAM in L2 mode spanning both DPUClusters and validate workload", func() {
			ValidateDPUServiceIPAMInL2ModeSharedAcrossDPUClusters(Ctx, input)
		})
		It("create single DPUServiceIPAM in L3 mode spanning both DPUClusters and validate workload", func() {
			ValidateDPUServiceIPAMInL3ModeSharedAcrossDPUClusters(Ctx, input)
		})
		It("create single DPUServiceIPAM in L3 mode spanning both DPUClusters with static allocations and validate workload", func() {
			ValidateDPUServiceIPAMInL3ModeSharedAcrossDPUClustersWithStaticAllocations(Ctx, input)
		})
		It("create single DPUServiceIPAM in L2 mode spanning both DPUClusters with single IP per node and validate workload", func() {
			ValidateDPUServiceIPAMInL2ModeSharedAcrossDPUClustersWithSingleIPPerNode(Ctx, input)
		})
		It("create single DPUServiceIPAM in L3 mode spanning both DPUClusters with single IP per node (/32) and validate workload", func() {
			ValidateDPUServiceIPAMInL3ModeSharedAcrossDPUClustersWithSingleIPPerNode(Ctx, input)
		})
		It("create per-DPUCluster DPUServiceIPAM in L2 mode and validate workload", func() {
			ValidateDPUServiceIPAMInL2ModePerDPUCluster(Ctx, input)
		})
		It("create per-DPUCluster DPUServiceIPAM in L3 mode and validate workload", func() {
			ValidateDPUServiceIPAMInL3ModePerDPUCluster(Ctx, input)
		})
	})

	Context("Validate DPUCluster operations", Ordered, func() {
		BeforeAll(func() {
			By("Waiting for DPU cluster 0 pods to be ready")
			VerifyClusterPods(Ctx, DPUClusterClient[0], systemPodsToVerify)
			By("Waiting for DPU cluster 1 pods to be ready")
			VerifyClusterPods(Ctx, DPUClusterClient[1], systemPodsToVerify)
			By("Waiting for DPFOperatorConfig to be ready")
			VerifyDPFOperatorConfigReady(Ctx, input.Client, 20*time.Minute)
		})
		It("Delete one of the DPUClusters and validate resource readiness", func() {
			ValidateDPUClusterDeletion(Ctx, input)
		})
	})
})
