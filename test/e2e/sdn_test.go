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
	input.applySDNConfig(*conf)
}

//nolint:dupl
var _ = Describe("DPF System tests - SDN", Labels{dpfSystemLabel, sdnLabel}, Ordered, func() {

	BeforeEach(func() {
		for _, label := range CurrentSpecReport().Labels() {
			if label == requiresNodesLabel {
				By("Waiting for provisioning")
				VerifyDPUClusterWithNodes(ctx, getProvisionDPUClustersInput())
				By("Waiting for DPU cluster pods to be ready")
				VerifyClusterPods(ctx, dpuClusterClient, systemPodsToVerify)
				By("Waiting for DPFOperatorConfig to be ready")
				VerifyDPFOperatorConfigReady(ctx, input.client, 20*time.Minute)
			}
		}
	})

	Context("DPU Service Function Chain", Labels{requiresNodesLabel, l2ConnectivityLabel}, func() {
		It("create plain dpu chain and verify performance", func() {
			VerifyPlainServiceFunctionChain(ctx, input)
		})
		It("create HBN only dpu chain and verify performance", func() {
			VerifyHBNOnlyServiceFunctionChain(ctx, input)
		})
		It("create HBN only dpu chain and verify performance after killing HBN", Labels{l2ConnectivityLabel}, func() {
			VerifyHBNOnlyBadFlowRecovery(ctx, input)
		})
		It("create Pods running in the DPUCluster via DPUService and verify RDMA traffic between them", func() {
			VerifyDPUPodToPodRDMATraffic(ctx, input)
		})
	})

	Context("Validate DPU Service NAD", Labels{dpfSystemLabel, requiresNodesLabel}, func() {
		It("create a pod consuming a DPUServiceNAD with all dependencies and check that it is created successfully", func() {
			ValidateDPUServiceNADConsumedByPod(ctx, input)
		})
	})
})
