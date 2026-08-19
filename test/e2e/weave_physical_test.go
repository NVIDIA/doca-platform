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

	"github.com/nvidia/doca-platform/test/e2e/cleanup"
	"github.com/nvidia/doca-platform/test/utils/dpuservice"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Weave Physical testcases", Labels{Domain.WeavePhysical, Domain.ZeroTrust}, Ordered, func() {
	var (
		weavePhysicalProvInput ProvisionDPUClustersInput
	)

	BeforeAll(func() {
		weavePhysicalPrerequisiteScope = cleanupTracker.RegisterScope(cleanup.NamedScopeManual("weave-physical-prerequisites"))
		weavePhysicalContextScope = cleanupTracker.RegisterScope(cleanup.NamedScopeManual("weave-physical-tests"))

		for _, label := range CurrentSpecReport().Labels() {
			if label != Domain.RequiresNodes {
				continue
			}

			if !input.hasDpuNodes() {
				Skip("Skip test as there are no DPU nodes")
			}

			weavePhysicalContextScope.CleanupBefore()
			weavePhysicalPrerequisiteScope.CleanupBefore()

			weavePhysicalProvInput = getProvisionDPUClustersInputForWeave(ctx, getProvisionDPUClustersInput(), input.client)
			Expect(weavePhysicalProvInput.dpuClusters).ToNot(BeEmpty(), "no DPU clusters found via config or discovery")

			By("Waiting for DPFOperatorConfig to be ready")
			VerifyDPFOperatorConfigReady(ctx, input.client, 20*time.Minute)

			By("Creating WeavePhysical DPUFlavorTemplate, services, and DPUDeployment")
			PrepareWeavePhysicalProvisioning(ctx, input)

			By("Creating DPU cluster client for verification")
			getDPUClusterClients(ctx, weavePhysicalProvInput)
			Expect(dpuClusterClient).ToNot(BeEmpty(), "no DPU cluster clients were created")

			By("Verifying DPU cluster has ready nodes")
			VerifyDPUClusterWithNodes(ctx, weavePhysicalProvInput)

			By("Waiting for DPU cluster pods to be ready")
			VerifyClusterPods(ctx, dpuClusterClient[0], systemPodsToVerify)

			By("Waiting for WeavePhysical DPUDeployment to be ready")
			dpuservice.WaitForDPUDeploymentReady(ctx, input.client, dpfOperatorSystemNamespace,
				[]string{weavePhysicalDPUDeploymentName}, 50*time.Minute)
		}
	})

	AfterAll(func() {
		weavePhysicalContextScope.CleanupAfter()
		weavePhysicalPrerequisiteScope.CleanupAfter()
	})

	Context("WeavePhysical tests", Labels{Domain.RequiresNodes}, Ordered, func() {
		It("should be ready for WeavePhysical tests", func() {
			// Placeholder for upcoming WeavePhysical.
		})
	})
})
