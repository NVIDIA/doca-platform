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

func ProvisioningBeforeSuite() {
	By("Setting Provisioning configs for the test")
	// No additional config needed - input.applyConfig(*conf) already called in SetInput()
}

//nolint:dupl
var _ = Describe("DPF System tests - Provisioning", Labels{Domain.Provisioning}, Ordered, func() {
	BeforeAll(func() {
		BeforeProvisioning(ctx, input)
	})

	AfterAll(func() {
		By("Cleaning up test suite resources")
		if cleanupFlags.SkipCleanup {
			By("Skip cleanup")
			return
		}
	})

	It("create DPU cluster and BFB", func() {
		CreateProvisioningDPUCluster(ctx, input)
	})

	It("create DPUSet and provision DPUs", func() {
		CreateProvisioningDPUSet(ctx, input)
	})

	It("verify provisioning is complete", func() {
		VerifyProvisioning(ctx, input)
	})

	It("change the OOB bridge name in the operatorConfig and verify DPUNode condition updates", Labels{Domain.RequiresNodes}, func() {
		ValidateDPFOperatorOOBBridgeNameChange(ctx, input)
	})

	It("verify OOB bridge VF attachment and netplan after provisioning", Labels{Domain.RequiresNodes}, func() {
		ValidateDPFOperatorOOBBridgePostProvisioning(ctx, input)
	})

	It("verify DPUDevice and DPUSet cluster node label and annotation changes are reflected on tenant Nodes", func() {
		ValidateDPUSetClusterNodeLabelsPropagation(ctx, input)
		ValidateDPUDeviceClusterNodeLabelsPropagation(ctx, input)
	})

	It("delete all provisioning resources", func() {
		if cleanupFlags.SkipCleanup {
			Skip("Skipping deprovisioning tests because skipCleanup is enabled")
		}
		DeleteProvisioning(ctx, input)
	})
})
