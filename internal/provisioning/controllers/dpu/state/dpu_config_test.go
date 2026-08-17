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
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

var _ = Describe("Phase DPUConfig", func() {
	var defaultDPUName = "dpu-config-test"
	expectDPUConfigCondition := func(status provisioningv1.DPUStatus, wantStatus metav1.ConditionStatus, wantReason, wantMessage string) {
		cond := meta.FindStatusCondition(status.Conditions, provisioningv1.DPUCondDPUConfig.String())
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(wantStatus))
		Expect(cond.Reason).To(Equal(wantReason))
		Expect(cond.Message).To(Equal(wantMessage))
	}

	Context("waiting for agent", func() {
		It("should wait with WaitingForDPUAgent when AgentStatus is nil", func() {
			dpu := dpuObj(defaultDPUName)
			dpu.Status.Phase = provisioningv1.DPUConfig

			status, err := state.DPUConfig(ctx, dpu, &dutil.ControllerContext{})
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUConfig))
			expectDPUConfigCondition(status, metav1.ConditionFalse, "WaitingForDPUAgent", "waiting for DPU agent contact")
		})

		It("should wait with WaitingForDPUAgent when LastStartupTime is nil", func() {
			dpu := dpuObj(defaultDPUName)
			dpu.Status.Phase = provisioningv1.DPUConfig
			dpu.Status.AgentStatus = &provisioningv1.AgentStatus{
				RebootMethod: ptr.To(provisioningv1.RebootMethodUnknown),
			}

			status, err := state.DPUConfig(ctx, dpu, &dutil.ControllerContext{})
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUConfig))
			expectDPUConfigCondition(status, metav1.ConditionFalse, "WaitingForDPUAgent", "waiting for DPU agent contact")
		})

		It("should name a skewed DPU clock as the reason for the wait", func() {
			dpu := dpuObj(defaultDPUName)
			dpu.Status.Phase = provisioningv1.DPUConfig
			hostTime := metav1.Now()
			dpu.Status.AgentStatus = &provisioningv1.AgentStatus{
				Clock: &provisioningv1.ClockStatus{
					DPUTime:  metav1.NewTime(hostTime.Add(-4 * time.Hour)),
					HostTime: hostTime,
				},
			}

			status, err := state.DPUConfig(ctx, dpu, &dutil.ControllerContext{})
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUConfig))
			cond := meta.FindStatusCondition(status.Conditions, provisioningv1.DPUCondDPUConfig.String())
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal(cutil.ReasonDPUClockUnsynchronized))
			Expect(cond.Message).To(ContainSubstring("waiting for DPU agent contact"))
			Expect(cond.Message).To(ContainSubstring("4h0m0s behind the host clock"))
		})

		It("should keep the plain wait when the reported clocks agree", func() {
			dpu := dpuObj(defaultDPUName)
			dpu.Status.Phase = provisioningv1.DPUConfig
			hostTime := metav1.Now()
			dpu.Status.AgentStatus = &provisioningv1.AgentStatus{
				Clock: &provisioningv1.ClockStatus{DPUTime: hostTime, HostTime: hostTime},
			}

			status, err := state.DPUConfig(ctx, dpu, &dutil.ControllerContext{})
			Expect(err).NotTo(HaveOccurred())
			expectDPUConfigCondition(status, metav1.ConditionFalse, "WaitingForDPUAgent", "waiting for DPU agent contact")
		})

		It("should wait with WaitingForRebootMethod when RebootMethod is nil", func() {
			dpu := dpuObj(defaultDPUName)
			dpu.Status.Phase = provisioningv1.DPUConfig
			dpu.Status.AgentStatus = &provisioningv1.AgentStatus{
				LastStartupTime: ptr.To(metav1.Now()),
			}

			status, err := state.DPUConfig(ctx, dpu, &dutil.ControllerContext{})
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUConfig))
			expectDPUConfigCondition(status, metav1.ConditionFalse, "WaitingForRebootMethod", "waiting for DPU agent to report reboot method")
		})

		It("should wait when RebootMethod is Unknown even if LastStartupTime changed", func() {
			oldTime := metav1.NewTime(metav1.Now().Add(-time.Hour))
			dpu := dpuObj(defaultDPUName)
			dpu.Status.Phase = provisioningv1.DPUConfig
			dpu.Status.AgentLastStartupTime = &oldTime
			dpu.Status.AgentStatus = &provisioningv1.AgentStatus{
				LastStartupTime: ptr.To(metav1.Now()),
				RebootMethod:    ptr.To(provisioningv1.RebootMethodUnknown),
			}

			status, err := state.DPUConfig(ctx, dpu, &dutil.ControllerContext{})
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUConfig))
			Expect(status.AgentLastStartupTime).To(Equal(&oldTime), "should not update AgentLastStartupTime while waiting for real RebootMethod")
			expectDPUConfigCondition(status, metav1.ConditionFalse, "WaitingForRebootMethod", "waiting for DPU agent to report reboot method")
		})
	})

	Context("stale RebootMethod guard", func() {
		It("should wait when AgentLastStartupTime equals LastStartupTime", func() {
			now := metav1.Now()
			dpu := dpuObj(defaultDPUName)
			dpu.Status.Phase = provisioningv1.DPUConfig
			dpu.Status.AgentLastStartupTime = &now
			dpu.Status.AgentStatus = &provisioningv1.AgentStatus{
				LastStartupTime: &now,
				RebootMethod:    ptr.To(provisioningv1.RebootMethodNoAction),
			}

			status, err := state.DPUConfig(ctx, dpu, &dutil.ControllerContext{})
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUConfig), "should not advance when startup time has not changed")
			expectDPUConfigCondition(status, metav1.ConditionFalse, "WaitingForFreshRebootMethod", "the RebootMethod in AgentStatus is from the previous reboot, waiting for the DPU agent to report the reboot method for the current reboot")
		})
	})

	Context("NoAction transition", func() {
		It("should move to host network configuration and set DPUConfig condition to true", func() {
			oldTime := metav1.NewTime(metav1.Now().Add(-time.Hour))
			dpu := dpuObj(defaultDPUName)
			dpu.Status.Phase = provisioningv1.DPUConfig
			dpu.Status.AgentLastStartupTime = &oldTime
			dpu.Status.AgentStatus = &provisioningv1.AgentStatus{
				LastStartupTime: ptr.To(metav1.Now()),
				RebootMethod:    ptr.To(provisioningv1.RebootMethodNoAction),
			}

			status, err := state.DPUConfig(ctx, dpu, &dutil.ControllerContext{})
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUHostNetworkConfiguration))
			expectDPUConfigCondition(status, metav1.ConditionTrue, string(provisioningv1.RebootMethodNoAction), "RebootMethod is NoAction; transitioning to Host Network Configuration phase")
		})

		It("should move to DPUClusterConfig in zero-trust mode", func() {
			oldTime := metav1.NewTime(metav1.Now().Add(-time.Hour))
			dpu := dpuObj(defaultDPUName)
			dpu.Status.Phase = provisioningv1.DPUConfig
			dpu.Status.AgentLastStartupTime = &oldTime
			dpu.Status.AgentStatus = &provisioningv1.AgentStatus{
				LastStartupTime: ptr.To(metav1.Now()),
				RebootMethod:    ptr.To(provisioningv1.RebootMethodNoAction),
			}
			ctrlCtx := &dutil.ControllerContext{
				Options: dutil.DPUOptions{DeploymentMode: string(provisioningv1.DeploymentModeZeroTrust)},
			}

			status, err := state.DPUConfig(ctx, dpu, ctrlCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUClusterConfig))
			expectDPUConfigCondition(status, metav1.ConditionTrue, string(provisioningv1.RebootMethodNoAction), "RebootMethod is NoAction; transitioning to DPU Cluster Config phase")
		})
	})

	Context("DPUWarmReboot", func() {
		It("should stay in DPUConfig phase when RebootMethod is DPUWarmReboot", func() {
			oldTime := metav1.NewTime(metav1.Now().Add(-time.Hour))
			dpu := dpuObj(defaultDPUName)
			dpu.Status.Phase = provisioningv1.DPUConfig
			dpu.Status.AgentLastStartupTime = &oldTime
			dpu.Status.AgentStatus = &provisioningv1.AgentStatus{
				LastStartupTime: ptr.To(metav1.Now()),
				RebootMethod:    ptr.To(provisioningv1.RebootMethodDPUWarmReboot),
			}

			status, err := state.DPUConfig(ctx, dpu, &dutil.ControllerContext{})
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUConfig))
			expectDPUConfigCondition(status, metav1.ConditionFalse, string(provisioningv1.RebootMethodDPUWarmReboot), "DPU OS is rebooting, staying in DPUConfig phase")
		})
	})

	Context("transitioning to host reboot", func() {
		It("should clear stale Rebooted condition when entering DPURebooting", func() {
			oldTime := metav1.NewTime(metav1.Now().Add(-time.Hour))
			dpu := dpuObj(defaultDPUName)
			dpu.Status.Phase = provisioningv1.DPUConfig
			dpu.Status.AgentLastStartupTime = &oldTime
			dpu.Status.Conditions = []metav1.Condition{{
				Type:    provisioningv1.DPUCondRebooted.String(),
				Status:  metav1.ConditionTrue,
				Reason:  "Rebooted",
				Message: "stale reboot completion from previous cycle",
			}}
			dpu.Status.AgentStatus = &provisioningv1.AgentStatus{
				LastStartupTime: ptr.To(metav1.Now()),
				RebootMethod:    ptr.To(provisioningv1.RebootMethodSystemLevelReset),
			}

			status, err := state.DPUConfig(ctx, dpu, &dutil.ControllerContext{})
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPURebooting))
			Expect(meta.FindStatusCondition(status.Conditions, provisioningv1.DPUCondRebooted.String())).To(BeNil())
			expectDPUConfigCondition(status, metav1.ConditionTrue, string(provisioningv1.RebootMethodSystemLevelReset), "RebootMethod is SystemLevelReset; transitioning to Rebooting phase")
		})

		It("should transition to DPURebooting for PowerCycle", func() {
			oldTime := metav1.NewTime(metav1.Now().Add(-time.Hour))
			dpu := dpuObj(defaultDPUName)
			dpu.Status.Phase = provisioningv1.DPUConfig
			dpu.Status.AgentLastStartupTime = &oldTime
			dpu.Status.AgentStatus = &provisioningv1.AgentStatus{
				LastStartupTime: ptr.To(metav1.Now()),
				RebootMethod:    ptr.To(provisioningv1.RebootMethodPowerCycle),
			}

			status, err := state.DPUConfig(ctx, dpu, &dutil.ControllerContext{})
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPURebooting))
			Expect(status.RebootStatus).NotTo(BeNil())
			Expect(status.RebootStatus.Method).NotTo(BeNil())
			Expect(*status.RebootStatus.Method).To(Equal(provisioningv1.RebootMethodPowerCycle))
			expectDPUConfigCondition(status, metav1.ConditionTrue, string(provisioningv1.RebootMethodPowerCycle), "RebootMethod is PowerCycle; transitioning to Rebooting phase")
		})
	})

	Context("deletion", func() {
		It("should transition to DPUDeleting when DPU is being deleted", func() {
			dpu := dpuObj(defaultDPUName)
			dpu.Status.Phase = provisioningv1.DPUConfig
			now := metav1.Now()
			dpu.DeletionTimestamp = &now

			status, err := state.DPUConfig(ctx, dpu, &dutil.ControllerContext{})
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUDeleting))
			Expect(meta.FindStatusCondition(status.Conditions, provisioningv1.DPUCondDPUConfig.String())).To(BeNil())
		})
	})
})
