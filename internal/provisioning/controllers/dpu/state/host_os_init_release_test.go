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

func hostOSInitDPU(hostOSInit *provisioningv1.HostOSInitStatus) *provisioningv1.DPU {
	return &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{Name: "dpu-1", Namespace: "default"},
		Status: provisioningv1.DPUStatus{
			Phase: provisioningv1.DPUHostOSInitRelease,
			Conditions: []metav1.Condition{{
				Type:   provisioningv1.DPUCondHostOSInitRelease.String(),
				Status: metav1.ConditionUnknown,
				Reason: "AwaitingAgent",
			}},
			AgentStatus: &provisioningv1.AgentStatus{
				HostOSInit: hostOSInit,
			},
		},
	}
}

var _ = Describe("DPU: host OS init release", func() {
	It("advances when the agent reports a succeeded release", func() {
		dpu := hostOSInitDPU(&provisioningv1.HostOSInitStatus{
			Succeeded: &provisioningv1.HostOSInitSucceeded{},
		})
		status, err := state.HostOSInitRelease(ctx, dpu, &dutil.ControllerContext{})
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUNodeEffectRemoval))
	})

	It("advances when the agent reports a skipped release", func() {
		dpu := hostOSInitDPU(&provisioningv1.HostOSInitStatus{
			Skipped: &provisioningv1.HostOSInitSkipped{},
		})
		status, err := state.HostOSInitRelease(ctx, dpu, &dutil.ControllerContext{})
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUNodeEffectRemoval))
	})

	It("waits when the agent has not reported HostOSInit yet", func() {
		dpu := hostOSInitDPU(nil)
		status, err := state.HostOSInitRelease(ctx, dpu, &dutil.ControllerContext{})
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUHostOSInitRelease))
	})

	It("waits when HostOSInit is present but not terminal", func() {
		dpu := hostOSInitDPU(&provisioningv1.HostOSInitStatus{})
		status, err := state.HostOSInitRelease(ctx, dpu, &dutil.ControllerContext{})
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUHostOSInitRelease))
	})

	It("waits when AgentStatus is nil", func() {
		dpu := &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{Name: "dpu-1", Namespace: "default"},
			Status: provisioningv1.DPUStatus{
				Phase: provisioningv1.DPUHostOSInitRelease,
			},
		}
		status, err := state.HostOSInitRelease(ctx, dpu, &dutil.ControllerContext{})
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUHostOSInitRelease))
	})
})
