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
	"github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/labels"
)

func SDNBeforeSuite() {
	By("Setting SDN configs for the test")
	input.applySDNConfig(*conf)
}

//nolint:dupl
var _ = Describe("DPF System tests", Labels{dpfSystemLabel, sdnLabel}, Ordered, func() {

	BeforeEach(func() {
		for _, label := range CurrentSpecReport().Labels() {
			if label == requiresNodesLabel {
				By("Waiting for provisioning")
				VerifyDPUClusterWithNodes(ctx, getProvisionDPUClustersInput())
				By("Waiting for DPU cluster pods to be ready")
				VerifyDPUClusterPods(ctx, systemPodsToVerify)
			}
		}
	})
	AfterEach(func() {
		By("cleaning up objects created during the SDN test", func() {
			Expect(utils.CleanupWithLabelAndWait(ctx, testClient, labels.SelectorFromSet(afterEachCleanupLabels), resourcesToDelete...)).To(Succeed())
		})
	})

	Context("DPU Service Function Chain", Labels{requiresNodesLabel, l2ConnectivityLabel}, func() {
		It("create plain dpu chain and verify performance", func() {
			VerifyPlainServiceFunctionChain(ctx, input)
		})
		It("create HBN only dpu chain and verify performance", func() {
			VerifyHBNOnlyServiceFunctionChain(ctx, input)
		})
	})
})
