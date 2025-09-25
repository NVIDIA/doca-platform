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
	. "github.com/onsi/ginkgo/v2"
)

//nolint:dupl
var _ = Describe("External DPF tests", Labels{externalTestLabel}, func() {
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

	Context("External DFP tests", Labels{requiresNodesLabel}, func() {
		It("Placeholder for the external test command - to be replaced with an env var", func() {
			By("DPF system is configure, provisioned and ready for external testing")
		})
	})
})
