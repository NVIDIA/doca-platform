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

package state_test

import (
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("DPU: service readiness", func() {
	// serviceReadinessDPU returns a DPU sitting in Service Readiness referencing the given flavor,
	// with the given agent-reported host OS init status and operational conditions.
	serviceReadinessDPU := func(flavorName string, hostOSInit *provisioningv1.HostOSInitStatus,
		operationalConditions ...metav1.Condition) *provisioningv1.DPU {
		dpu := dpuObj("service-readiness-test")
		dpu.Spec.DPUFlavor = flavorName
		dpu.Status.Phase = provisioningv1.DPUServiceReadiness
		dpu.Status.OperationalConditions = operationalConditions
		if hostOSInit != nil {
			dpu.Status.AgentStatus = &provisioningv1.AgentStatus{HostOSInit: hostOSInit}
		}
		return dpu
	}

	// flavorWithGate builds a DPUFlavor in the DPU namespace, opting into the phase gate when gate
	// is non-empty.
	flavorWithGate := func(name string, gate provisioningv1.ServiceReadinessGate) *provisioningv1.DPUFlavor {
		flavor := dpuFlavorObj(name)
		if gate != "" {
			flavor.Spec.ServiceReadiness = &provisioningv1.ServiceReadiness{Gate: gate}
		}
		return flavor
	}

	// createFlavorWithGate creates a flavor that requests no host hold, so the phase has no reason
	// to wait for the agent, and returns its name.
	createFlavorWithGate := func(name string, gate provisioningv1.ServiceReadinessGate) string {
		flavor := flavorWithGate(name, gate)
		createObject(flavor)
		return flavor.Name
	}

	// createFlavorWithHold creates a flavor that requests the host hold through NVConfig, which is
	// what makes the phase wait for the agent to report a release, and returns its name.
	createFlavorWithHold := func(name string, gate provisioningv1.ServiceReadinessGate) string {
		flavor := flavorWithGate(name, gate)
		flavor.Spec.NVConfig = []provisioningv1.NVConfig{{
			Parameters: []string{"DELAY_HOST_OS_INIT=0x3"},
		}}
		createObject(flavor)
		return flavor.Name
	}

	trueCondition := func(gate provisioningv1.ServiceReadinessGate) metav1.Condition {
		return metav1.Condition{
			Type:               string(gate),
			Status:             metav1.ConditionTrue,
			Reason:             "Ready",
			LastTransitionTime: metav1.Now(),
		}
	}

	succeeded := &provisioningv1.HostOSInitStatus{Succeeded: &provisioningv1.HostOSInitSucceeded{}}

	conditionWithReason := func(status provisioningv1.DPUStatus, reason string) {
		Expect(status.Conditions).To(ContainElement(And(
			HaveField("Type", provisioningv1.DPUCondServiceReadiness.String()),
			HaveField("Reason", reason),
		)))
	}

	Context("with a host hold requested and no gate configured", func() {
		It("advances when the agent reports a succeeded release", func() {
			flavor := createFlavorWithHold("flavor-no-gate-succeeded", "")
			dpu := serviceReadinessDPU(flavor, succeeded)

			status, err := state.ServiceReadiness(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUNodeEffectRemoval))
		})

		It("advances when the agent reports a skipped release", func() {
			flavor := createFlavorWithHold("flavor-no-gate-skipped", "")
			dpu := serviceReadinessDPU(flavor, &provisioningv1.HostOSInitStatus{
				Skipped: &provisioningv1.HostOSInitSkipped{},
			})

			status, err := state.ServiceReadiness(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUNodeEffectRemoval))
		})

		It("does not wait on operational conditions, only on the agent", func() {
			flavor := createFlavorWithHold("flavor-no-gate-agent-pending", "")
			// No operational conditions at all, so a gate wait would block here if one applied.
			dpu := serviceReadinessDPU(flavor, nil)

			status, err := state.ServiceReadiness(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUServiceReadiness))
			conditionWithReason(status, "AwaitingAgent")
		})

		It("waits when HostOSInit is present but not terminal", func() {
			flavor := createFlavorWithHold("flavor-no-gate-non-terminal", "")
			dpu := serviceReadinessDPU(flavor, &provisioningv1.HostOSInitStatus{})

			status, err := state.ServiceReadiness(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUServiceReadiness))
			conditionWithReason(status, "AwaitingAgent")
		})

		// An agent that reported other conditions but no hostOSInit is either mid-release or too
		// old to perform one. Both must read as "not released" rather than dereference the nil.
		It("waits when the agent reported conditions but no hostOSInit", func() {
			flavor := createFlavorWithHold("flavor-no-gate-agent-status-only", "")
			dpu := serviceReadinessDPU(flavor, nil)
			dpu.Status.AgentStatus = &provisioningv1.AgentStatus{
				Conditions: []metav1.Condition{{
					Type:               "KubeletStarted",
					Status:             metav1.ConditionTrue,
					Reason:             "KubeletStarted",
					LastTransitionTime: metav1.Now(),
				}},
			}

			status, err := state.ServiceReadiness(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUServiceReadiness))
			conditionWithReason(status, "AwaitingAgent")
		})
	})

	// A flavor that requests no hold has nothing for the agent to release. Waiting for the agent
	// anyway deadlocks a DPU whose agent completed its one-shot run before this phase existed,
	// which strands the DPU short of Node Effect Removal and leaves its host node drained.
	Context("with no host hold requested", func() {
		It("advances even though the agent never reported a release", func() {
			flavor := createFlavorWithGate("flavor-no-hold-no-agent-report", "")
			dpu := serviceReadinessDPU(flavor, nil)

			status, err := state.ServiceReadiness(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUNodeEffectRemoval))
		})

		// The shape an upgraded DPU actually reports: the agent ran and populated agentStatus, but
		// it predates this phase so it never wrote hostOSInit at all.
		It("advances when the agent reported conditions but no hostOSInit", func() {
			flavor := createFlavorWithGate("flavor-no-hold-agent-status-only", "")
			dpu := serviceReadinessDPU(flavor, nil)
			dpu.Status.AgentStatus = &provisioningv1.AgentStatus{
				Conditions: []metav1.Condition{{
					Type:               "KubeletStarted",
					Status:             metav1.ConditionTrue,
					Reason:             "KubeletStarted",
					LastTransitionTime: metav1.Now(),
				}},
			}

			status, err := state.ServiceReadiness(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUNodeEffectRemoval))
		})

		It("advances once a configured gate is True, without waiting for the agent", func() {
			flavor := createFlavorWithGate("flavor-no-hold-gate-ready", provisioningv1.GateOperationalReady)
			dpu := serviceReadinessDPU(flavor, nil, trueCondition(provisioningv1.GateOperationalReady))

			status, err := state.ServiceReadiness(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUNodeEffectRemoval))
		})

		It("still honors a configured gate before advancing", func() {
			flavor := createFlavorWithGate("flavor-no-hold-gate-pending", provisioningv1.GateOperationalReady)
			dpu := serviceReadinessDPU(flavor, nil)

			status, err := state.ServiceReadiness(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUServiceReadiness))
			conditionWithReason(status, "AwaitingServices")
		})
	})

	Context("with a gate configured", func() {
		It("blocks while the gate is not True, even once the agent has released", func() {
			flavor := createFlavorWithGate("flavor-gate-critical-pending", provisioningv1.GateDPUServiceCriticalPodsReady)
			dpu := serviceReadinessDPU(flavor, succeeded)

			status, err := state.ServiceReadiness(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUServiceReadiness))
			conditionWithReason(status, "AwaitingServices")
		})

		It("advances once the gate is True and the agent has released", func() {
			flavor := createFlavorWithHold("flavor-gate-critical-ready", provisioningv1.GateDPUServiceCriticalPodsReady)
			dpu := serviceReadinessDPU(flavor, succeeded,
				trueCondition(provisioningv1.GateDPUServiceCriticalPodsReady))

			status, err := state.ServiceReadiness(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUNodeEffectRemoval))
		})

		It("is not satisfied by a different operational condition being True", func() {
			flavor := createFlavorWithGate("flavor-gate-operational-wrong-cond", provisioningv1.GateOperationalReady)
			dpu := serviceReadinessDPU(flavor, succeeded,
				trueCondition(provisioningv1.GateDPUServiceCriticalPodsReady))

			status, err := state.ServiceReadiness(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUServiceReadiness))
			conditionWithReason(status, "AwaitingServices")
		})

		It("still waits for the agent after the gate is True", func() {
			flavor := createFlavorWithHold("flavor-gate-ready-agent-pending", provisioningv1.GateOperationalReady)
			dpu := serviceReadinessDPU(flavor, nil, trueCondition(provisioningv1.GateOperationalReady))

			status, err := state.ServiceReadiness(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUServiceReadiness))
			conditionWithReason(status, "AwaitingAgent")
		})
	})

	Context("when the flavor cannot be read", func() {
		// A missing flavor must not be reported as "services not ready", or a mistyped flavor
		// reference would present as an indefinite wait behind a misleading message.
		It("surfaces FlavorNotFound and returns an error rather than waiting silently", func() {
			dpu := serviceReadinessDPU("flavor-that-does-not-exist", succeeded)

			status, err := state.ServiceReadiness(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
			Expect(err).To(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUServiceReadiness))
			conditionWithReason(status, "FlavorNotFound")
		})
	})

	It("transitions to Deleting when the DPU is being deleted", func() {
		flavor := createFlavorWithGate("flavor-deleting", provisioningv1.GateOperationalReady)
		dpu := serviceReadinessDPU(flavor, nil)
		now := metav1.Now()
		dpu.DeletionTimestamp = &now

		status, err := state.ServiceReadiness(ctx, dpu, &dutil.ControllerContext{Client: k8sClient})
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUDeleting))
	})
})
