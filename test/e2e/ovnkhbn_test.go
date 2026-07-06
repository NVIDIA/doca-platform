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
var _ = Describe("DPF System tests - OVNK HBN", Labels{Domain.OVNKHBN, Domain.RequiresNodes}, func() {
	BeforeEach(func() {
		By("Wait for OVNK HBN deployment to be ready")
		WaitForOVNKHBNDeploymentReady(Ctx, input)

		By("Waiting for multus pods to be ready")
		VerifyClusterPods(Ctx, input.Client, []string{"kube-multus-ds"})
	})

	Context("OVNK HBN", func() {
		It("verify performance of pod to pod same node", func() {
			VerifyPerformancePodToPodSameNode(Ctx, input, "ovnkhbn")
		})
		It("verify performance of pod to pod different nodes", func() {
			VerifyPerformancePodToPodDifferentNode(Ctx, input, "ovnkhbn")
		})
	})
})
