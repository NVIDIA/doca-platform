/*
Copyright 2025 NVIDIA

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

func SDNBeforeSuite() {
	By("Setting SDN configs for the test")
	input.ApplySDNConfig(*conf)
}

//nolint:dupl
var _ = Describe("DPF System tests - SDN", SpecPriority(SDNTestPriority), Labels{Domain.DPFSystem, Domain.SDN}, Ordered, func() {

	BeforeEach(func() {
		for _, label := range CurrentSpecReport().Labels() {
			if label == Domain.RequiresNodes {
				By("Waiting for provisioning")
				VerifyDPUClusterWithNodes(ctx, GetProvisionDPUClustersInput())
				By("Waiting for DPU cluster pods to be ready")
				VerifyClusterPods(ctx, dpuClusterClient[0], systemPodsToVerify)
				By("Waiting for DPFOperatorConfig to be ready")
				VerifyDPFOperatorConfigReady(ctx, input.Client, 20*time.Minute)
			}
		}
	})

	Context("DPU Service Function Chain", Labels{Domain.RequiresNodes, Domain.L2Connectivity}, func() {
		It("create plain DPU chain and verify performance", func() {
			VerifyPlainServiceFunctionChain(ctx, input)
		})
		It("create HBN only DPU chain and verify performance", func() {
			VerifyHBNOnlyServiceFunctionChain(ctx, input)
		})
		It("create HBN only DPU chain and verify performance after killing HBN", Labels{Domain.L2Connectivity}, func() {
			VerifyHBNOnlyBadFlowRecovery(ctx, input)
		})
		It("create simple chain and validate serviceMTU changes", func() {
			VerifyServiceMTUOnDPUPods(ctx, input)
		})
		It("create Pods running in the DPUCluster via DPUService and verify RDMA traffic between them", func() {
			VerifyDPUPodToPodRDMATraffic(ctx, input)
		})
	})

	Context("Validate DPU Service NAD", Labels{Domain.DPFSystem, Domain.RequiresNodes}, func() {
		It("create a pod consuming a DPUServiceNAD with all dependencies and check that it is created successfully", func() {
			ValidateDPUServiceNADConsumedByPod(ctx, input)
		})
		It("verify DPUServiceNAD metrics", func() {
			ValidateDPUServiceNADMetrics(ctx)
		})
	})
})
