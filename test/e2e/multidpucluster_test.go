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
		if input.numberOfDPUNodes != 2 {
			Skip("Skip test as exactly 2 nodes are required for multi DPUCluster testing")
		}
	})
	Context("Setup system", Ordered, func() {
		It("create DPFOperatorConfig", func() {
			SystemSetupBeforeSuite()
		})
		It("create DPUClusters", func() {
			ProvisionDPUClusters(ctx, getProvisionDPUClustersInput())
		})
		It("create BFB and DPUFlavor", func() {
			ProvisionBFBAndDPUFlavor(ctx, getProvisionDPUClustersInput())
		})
		It("create a DPUDeployment with each of DPUs joining a different cluster", func() {
			ProvisionDPUDeploymentWithEachDPUJoiningADifferentDPUCluster(ctx, input)
		})
	})

	Context("Validate system behavior", Ordered, func() {
		BeforeAll(func() {
			By("Waiting for DPU cluster 0 pods to be ready")
			VerifyClusterPods(ctx, dpuClusterClient[0], systemPodsToVerify)
			By("Waiting for DPU cluster 1 pods to be ready")
			VerifyClusterPods(ctx, dpuClusterClient[1], systemPodsToVerify)
			By("Waiting for DPFOperatorConfig to be ready")
			VerifyDPFOperatorConfigReady(ctx, input.client, 20*time.Minute)
		})
		It("Create DPUServiceIPAM in L2 mode and validate workload", func() {
			ValidateDPUServiceIPAMInL2ModeForMultiDPUCluster(ctx, input)
		})
		It("Create DPUServiceIPAM in L3 mode and validate workload", func() {
			ValidateDPUServiceIPAMInL3ModeForMultiDPUCluster(ctx, input)
		})
	})
})
