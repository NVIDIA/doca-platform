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
	"context"
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	mockconfig "github.com/nvidia/doca-platform/test/mock/dpuagent/pkg/config"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// mockBMCBelowMinimum is a BF3 BMC firmware version below dpudevice.BMCMinSupportedVersion, so
	// every BF3 run goes through the BMC firmware upgrade and Manager.Reset.
	mockBMCBelowMinimum = "BF-24.07-10"
	// mockRebootMethods makes the agent report SystemLevelReset on its first run, PowerCycle on the
	// second and NoAction afterwards, so one run exercises both external reboot variants.
	mockRebootMethods = "SLR,PowerCycle,NoAction"
)

// Zero-trust provisioning of simulated DPUs. `make test-e2e-mock-dpuagent` builds and deploys the
// mock (Deployment and ConfigMap), creates the second kind cluster that plays the DPU cluster
// (kubeconfig in MOCK_DPU_CLUSTER_KUBECONFIG) and runs this suite with
// -ginkgo.label-filter="MockDPU && !SDN && !DPFVPCOVN && !Weave" -e2e.config=./config-mock-dpuagent.yaml
var _ = Describe("Zero-trust provisioning with mock DPUs", Labels{Domain.MockDPU}, Ordered, Serial, func() {
	var env *mockDPUEnv

	BeforeAll(func() {
		env = setupMockDPUEnvironment(ctx, testClient)
		deprovisionMockDPUs(ctx, env)
	})

	AfterAll(func() {
		if env == nil || cleanupFlags.SkipCleanup {
			return
		}
		deprovisionMockDPUs(ctx, env)
	})

	AfterEach(func() {
		if env != nil && !CurrentSpecReport().Failed() {
			deprovisionMockDPUs(ctx, env)
		}
	})

	// Each case covers as many phases as the DPU type allows and passes when every DPU is Ready.
	It("provisions BF3 DPUs through BMC upgrade and external reboots, then reprovisions a deleted one", func() {
		provisionMockDPUs(ctx, env, mockDPUConfig{dpuType: mockconfig.DPUTypeBF3, bmcVersion: mockBMCBelowMinimum, rebootMethod: mockRebootMethods})
		reprovisionDeletedMockDPU(ctx, env, provisioningv1.DPUTypeBlueField3)
	})

	It("provisions BF4 DPUs through a PLDM bundle update and external reboots, then reprovisions a deleted one", func() {
		provisionMockDPUs(ctx, env, mockDPUConfig{dpuType: mockconfig.DPUTypeBF4, rebootMethod: mockRebootMethods})
		reprovisionDeletedMockDPU(ctx, env, provisioningv1.DPUTypeBlueField4)
	})
})

// reprovisionDeletedMockDPU deletes one Ready DPU, waits for the DPUSet to recreate it and for the
// mock to go through the whole flow again with a fresh agent identity.
func reprovisionDeletedMockDPU(ctx context.Context, env *mockDPUEnv, dpuType provisioningv1.DPUType) {
	dpus := &provisioningv1.DPUList{}
	Expect(env.client.List(ctx, dpus, client.InNamespace(env.namespace))).To(Succeed())
	Expect(dpus.Items).NotTo(BeEmpty())
	victim := dpus.Items[0]
	oldUID := victim.UID
	oldStartup := victim.Status.AgentStatus.LastStartupTime

	By(fmt.Sprintf("Deleting DPU %s and waiting for the DPUSet to recreate it", victim.Name))
	Expect(env.client.Delete(ctx, &victim)).To(Succeed())
	Eventually(func(g Gomega) {
		dpu := &provisioningv1.DPU{}
		g.Expect(env.client.Get(ctx, client.ObjectKeyFromObject(&victim), dpu)).To(Succeed())
		g.Expect(dpu.UID).NotTo(Equal(oldUID), "DPU %s was not recreated yet", victim.Name)
		g.Expect(dpu.Status.Phase).NotTo(Equal(provisioningv1.DPUError), "DPU %s entered Error: %s", dpu.Name, dpuConditionsSummary(dpu))
		g.Expect(dpu.Status.Phase).To(Equal(provisioningv1.DPUReady))
		g.Expect(dpu.Status.AgentStatus).NotTo(BeNil())
		g.Expect(dpu.Status.AgentStatus.LastStartupTime).NotTo(BeNil())
		g.Expect(dpu.Status.AgentStatus.LastStartupTime.Time).To(BeTemporally(">", oldStartup.Time), "the agent must have run again with the new identity")
	}).WithTimeout(mockProvisioningTimeout).WithPolling(mockPollInterval).Should(Succeed())
	// The recreated DPU keeps the DPUDevice and therefore the mock BMC, on which Secure Boot is
	// already enabled, so the controller skips Perform ARM Force Restart this time.
	verifyMockDPUsProvisioned(ctx, env, dpuType, false)
}
