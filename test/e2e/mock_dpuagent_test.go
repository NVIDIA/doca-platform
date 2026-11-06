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
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/release"
	"github.com/nvidia/doca-platform/pkg/conditions"
	mockconfig "github.com/nvidia/doca-platform/test/mock/dpuagent/pkg/config"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
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
// -ginkgo.label-filter="MockDPU && !SDN && !DPFVPCOVN && !WeaveBF3" -e2e.config=./config-mock-dpuagent.yaml
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

	// An external system (e.g. NICo) rolls out disruptive DPUServices together with DPF and only
	// releases the hold node effect once the DPF upgrade completed (#5389689). The controllers of the
	// version under test already run when the upgrade is simulated, so this covers upgrades from a
	// version that holds DPUs in Node Effect, not from one that predates that hold.
	It("upgrades DPF while provisioned DPUs are held in Node Effect, then releases them", func() {
		provisionMockDPUs(ctx, env, mockDPUConfig{dpuType: mockconfig.DPUTypeBF3})
		holdMockDPUsInNodeEffect(ctx, env)
		simulateDPFUpgrade(ctx, env)
		releaseMockDPUHolds(ctx, env)
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

// mockHoldNodeLabel is the DPU cluster node label whose change sends the Ready DPUs back to Node Effect.
const mockHoldNodeLabel = "e2e.test.io/mock-dpu-hold"

// holdMockDPUsInNodeEffect switches the DPUSet to a hold node effect applied on label change and
// changes the node labels, so every Ready DPU goes back to Node Effect and waits there for the hold
// to be released.
func holdMockDPUsInNodeEffect(ctx context.Context, env *mockDPUEnv) {
	By("Switching the DPUSet to a hold node effect and changing the node labels")
	dpuSet := &provisioningv1.DPUSet{}
	Expect(env.client.Get(ctx, types.NamespacedName{Namespace: env.namespace, Name: mockDPUSetName}, dpuSet)).To(Succeed())
	original := dpuSet.DeepCopy()
	dpuSet.Spec.DPUTemplate.Spec.NodeEffect = provisioningv1.NodeEffect{
		// Force holds every DPU at once instead of one DPUNode after the other.
		Action: provisioningv1.Action{
			Hold:  ptr.To(true),
			Force: ptr.To(true),
		},
		UpgradePolicy: provisioningv1.UpgradePolicy{
			ApplyOnLabelChange: ptr.To(true),
		},
	}
	dpuSet.Spec.DPUTemplate.Spec.Cluster.NodeLabels[mockHoldNodeLabel] = "true"
	Expect(env.client.Patch(ctx, dpuSet, client.MergeFrom(original))).To(Succeed())

	By("Waiting for every DPU to be held in Node Effect")
	Eventually(func(g Gomega) {
		dpus := &provisioningv1.DPUList{}
		g.Expect(env.client.List(ctx, dpus, client.InNamespace(env.namespace))).To(Succeed())
		g.Expect(dpus.Items).To(HaveLen(env.replicas))
		for _, dpu := range dpus.Items {
			g.Expect(dpu.Status.Phase).To(Equal(provisioningv1.DPUNodeEffect), "DPU %s is %s", dpu.Name, dpu.Status.Phase)
			g.Expect(ptr.Deref(dpu.Status.PostProvisioningNodeEffect, false)).To(BeTrue(), "DPU %s is not in Node Effect after provisioning", dpu.Name)
			name, err := cutil.GenerateDPUNodeMaintenanceObjectName(dpu.Spec.DPUNodeName, dpu.Spec.NodeEffect)
			g.Expect(err).NotTo(HaveOccurred())
			maintenance := &provisioningv1.DPUNodeMaintenance{}
			g.Expect(env.client.Get(ctx, types.NamespacedName{Namespace: env.namespace, Name: name}, maintenance)).To(Succeed())
			g.Expect(isDPUNodeMaintenanceOnHold(maintenance)).To(BeTrue(), "DPUNodeMaintenance %s is not on hold", maintenance.Name)
		}
	}).WithTimeout(5 * time.Minute).WithPolling(mockPollInterval).Should(Succeed())
}

// simulateDPFUpgrade sets status.version of the DPFOperatorConfig back to the last GA version. The
// DPF Operator then sees status.targetVersion differ from it, runs the pre-upgrade validation and
// rolls out its components as for an upgrade from that version, and records its own version again
// once done. It must not wait for the DPUs held in Node Effect, which are only released after the
// upgrade. Only the version is simulated: the DPU controller holding the DPUs is the one under test.
func simulateDPFUpgrade(ctx context.Context, env *mockDPUEnv) {
	key := client.ObjectKey{Namespace: dpfOperatorSystemNamespace, Name: configName}
	config := &operatorv1.DPFOperatorConfig{}
	Expect(env.client.Get(ctx, key, config)).To(Succeed())
	targetVersion := ptr.Deref(config.Status.TargetVersion, "")
	Expect(targetVersion).NotTo(BeEmpty())
	if targetVersion == release.LastReleasedDPFGAVersion {
		Skip("an upgrade can only be simulated from a version other than the one under test")
	}

	By(fmt.Sprintf("Simulating a DPF upgrade from %s to %s", release.LastReleasedDPFGAVersion, targetVersion))
	original := config.DeepCopy()
	config.Status.Version = ptr.To(release.LastReleasedDPFGAVersion)
	Expect(env.client.Status().Patch(ctx, config, client.MergeFrom(original))).To(Succeed())
	// Do not leave the cluster in an upgrade if the spec fails before the upgrade completed: new
	// DPUs would be held in Pending and the DPF Operator would keep validating.
	DeferCleanup(func(ctx SpecContext) {
		current := &operatorv1.DPFOperatorConfig{}
		Expect(env.client.Get(ctx, key, current)).To(Succeed())
		if ptr.Deref(current.Status.Version, "") == targetVersion {
			return
		}
		restored := current.DeepCopy()
		restored.Status.Version = ptr.To(targetVersion)
		Expect(env.client.Status().Patch(ctx, restored, client.MergeFrom(current))).To(Succeed())
	})

	By("Waiting for the DPF Operator to complete the upgrade while the DPU controller reports the DPUs as held")
	reported := map[string]struct{}{}
	Eventually(func(g Gomega) {
		dpus := &provisioningv1.DPUList{}
		g.Expect(env.client.List(ctx, dpus, client.InNamespace(env.namespace))).To(Succeed())
		for i := range dpus.Items {
			if cutil.IsDPUHeldForUpgrade(&dpus.Items[i].Status) {
				reported[dpus.Items[i].Name] = struct{}{}
			}
		}
		g.Expect(env.client.Get(ctx, key, config)).To(Succeed())
		cond := conditions.Get(config, operatorv1.PreUpgradeValidationReadyCondition)
		g.Expect(cond).NotTo(BeNil())
		g.Expect(cond.Status).To(Equal(metav1.ConditionTrue), "pre-upgrade validation: %s", cond.Message)
		g.Expect(ptr.Deref(config.Status.Version, "")).To(Equal(targetVersion), "the upgrade did not complete yet")
	}).WithTimeout(5 * time.Minute).WithPolling(mockPollInterval).Should(Succeed())
	VerifyDPFOperatorConfigReady(ctx, env.client, 5*time.Minute)

	By("Verifying the DPUs were reported as held and are still in Node Effect")
	dpus := &provisioningv1.DPUList{}
	Expect(env.client.List(ctx, dpus, client.InNamespace(env.namespace))).To(Succeed())
	for _, dpu := range dpus.Items {
		Expect(reported).To(HaveKey(dpu.Name), "DPU %s was never reported with reason %s", dpu.Name, cutil.ReasonDPFOperatorUpgradeInProgress)
		Expect(dpu.Status.Phase).To(Equal(provisioningv1.DPUNodeEffect), "DPU %s left Node Effect before its hold was released", dpu.Name)
	}
}

// releaseMockDPUHolds releases the hold of every DPUNodeMaintenance, as the external system does once
// the DPF upgrade completed, and waits for the DPUs to apply the new node labels and be Ready again.
func releaseMockDPUHolds(ctx context.Context, env *mockDPUEnv) {
	By("Releasing the hold node effect of every DPU")
	maintenances := &provisioningv1.DPUNodeMaintenanceList{}
	Expect(env.client.List(ctx, maintenances, client.InNamespace(env.namespace))).To(Succeed())
	for i := range maintenances.Items {
		if isDPUNodeMaintenanceOnHold(&maintenances.Items[i]) {
			Expect(releaseDPUNodeMaintenanceHold(ctx, env.client, &maintenances.Items[i])).To(Succeed())
		}
	}

	waitForMockDPUsReady(ctx, env)
	dpus := &provisioningv1.DPUList{}
	Expect(env.client.List(ctx, dpus, client.InNamespace(env.namespace))).To(Succeed())
	for _, dpu := range dpus.Items {
		node := &corev1.Node{}
		Expect(env.dpuClusterClient.Get(ctx, types.NamespacedName{Name: dpu.Name}, node)).To(Succeed())
		Expect(node.Labels).To(HaveKeyWithValue(mockHoldNodeLabel, "true"), "Node %s did not get the new labels", node.Name)
	}
}
