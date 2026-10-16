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
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

var _ = Describe("in-place reconfig jump", func() {
	jumpPhases := []provisioningv1.DPUPhase{
		provisioningv1.DPUReady,
		provisioningv1.DPUHostNetworkConfiguration,
		provisioningv1.DPUClusterConfig,
		provisioningv1.DPUServiceReadiness,
		provisioningv1.DPUNodeEffectRemoval,
	}

	It("moves a jumpable phase to Node Effect and sets the DPU Config exit", func() {
		for _, phase := range jumpPhases {
			dpu := inPlaceDPU(phase, true, 3)
			dpu.Status.PostProvisioningNodeEffect = ptr.To(true)

			status, jumped := state.StartInPlaceReconfig(dpu)

			Expect(jumped).To(BeTrue(), string(phase))
			Expect(status.Phase).To(Equal(provisioningv1.DPUNodeEffect))
			Expect(status.PostProvisioningDPUConfig).To(Equal(ptr.To(true)))
			Expect(status.PostProvisioningNodeEffect).To(BeNil())
			Expect(status.DPUReconfig).To(Equal(dpu.Status.DPUReconfig))
			Expect(status.Conditions).To(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondReady.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "ReconfigPending"),
				),
				And(
					HaveField("Type", provisioningv1.DPUCondReconfiguring.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", "ReconfigPending"),
					HaveField("Message", "3"),
				),
			))
		}
	})

	DescribeTable("does not jump",
		func(dpu *provisioningv1.DPU) {
			status, jumped := state.StartInPlaceReconfig(dpu)
			Expect(jumped).To(BeFalse())
			Expect(status).To(Equal(provisioningv1.DPUStatus{}))
		},
		Entry("without inProgress", inPlaceDPU(provisioningv1.DPUReady, false, 0)),
		Entry("in DPU Config", inPlaceDPU(provisioningv1.DPUConfig, true, 1)),
		Entry("in Rebooting", inPlaceDPU(provisioningv1.DPURebooting, true, 1)),
		Entry("in Node Effect", inPlaceDPU(provisioningv1.DPUNodeEffect, true, 1)),
		Entry("while deleting", deletingInPlaceDPU()),
		Entry("after this round already returned to Ready", returnedInPlaceDPU(3, "3")),
	)

	It("lets the phases after DPU Config run while this round is in flight", func() {
		phases := []provisioningv1.DPUPhase{
			provisioningv1.DPUHostNetworkConfiguration,
			provisioningv1.DPUClusterConfig,
			provisioningv1.DPUServiceReadiness,
			provisioningv1.DPUNodeEffectRemoval,
			provisioningv1.DPUReady,
		}
		for _, phase := range phases {
			dpu := inPlaceDPU(phase, true, 3)
			cutil.SetDPUCondition(&dpu.Status, &metav1.Condition{
				Type:    provisioningv1.DPUCondReconfiguring.String(),
				Status:  metav1.ConditionTrue,
				Reason:  "ReconfigPending",
				Message: "3",
			})

			status, jumped := state.StartInPlaceReconfig(dpu)

			Expect(jumped).To(BeFalse(), string(phase))
			Expect(status).To(Equal(provisioningv1.DPUStatus{}))
		}
	})

	It("jumps again when a later round is marked", func() {
		dpu := returnedInPlaceDPU(4, "3")

		status, jumped := state.StartInPlaceReconfig(dpu)

		Expect(jumped).To(BeTrue())
		Expect(status.Phase).To(Equal(provisioningv1.DPUNodeEffect))
		Expect(status.Conditions).To(ContainElement(
			And(
				HaveField("Type", provisioningv1.DPUCondReconfiguring.String()),
				HaveField("Status", metav1.ConditionTrue),
				HaveField("Message", "4"),
			),
		))
	})
})

func inPlaceDPU(phase provisioningv1.DPUPhase, inProgress bool, round int32) *provisioningv1.DPU {
	dpu := &provisioningv1.DPU{}
	dpu.Status.Phase = phase
	if inProgress {
		dpu.Status.DPUReconfig = &provisioningv1.DPUReconfigStatus{
			InProgress: ptr.To(true),
			Round:      round,
		}
	}
	return dpu
}

func deletingInPlaceDPU() *provisioningv1.DPU {
	dpu := inPlaceDPU(provisioningv1.DPUReady, true, 1)
	now := metav1.Now()
	dpu.DeletionTimestamp = &now
	return dpu
}

func returnedInPlaceDPU(round int32, completedMessage string) *provisioningv1.DPU {
	dpu := inPlaceDPU(provisioningv1.DPUReady, true, round)
	cutil.SetDPUCondition(&dpu.Status, &metav1.Condition{
		Type:    provisioningv1.DPUCondReconfiguring.String(),
		Status:  metav1.ConditionFalse,
		Reason:  "ReconfigCompleted",
		Message: completedMessage,
	})
	return dpu
}
