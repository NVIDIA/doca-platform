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

package reboot

import (
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"
)

var _ = Describe("reboot state", func() {
	It("replaces the entry of the same boot, appends a new boot and keeps the newest maxRebootRequests", func() {
		var state rebootState
		state.record(rebootRequest{BootID: "boot-1", Method: provisioningv1.RebootMethodPowerCycle})
		state.record(rebootRequest{BootID: "boot-1", Method: provisioningv1.RebootMethodSystemLevelReset})
		Expect(state.Requests).To(Equal([]rebootRequest{{BootID: "boot-1", Method: provisioningv1.RebootMethodSystemLevelReset}}))

		for i := 2; i <= maxRebootRequests+2; i++ {
			state.record(rebootRequest{BootID: fmt.Sprintf("boot-%d", i), Method: provisioningv1.RebootMethodNoAction})
		}
		Expect(state.Requests).To(HaveLen(maxRebootRequests))
		Expect(state.Requests[0].BootID).To(Equal("boot-3"))
		Expect(state.Requests[maxRebootRequests-1].BootID).To(Equal(fmt.Sprintf("boot-%d", maxRebootRequests+2)))
	})

	DescribeTable("sequenceCount counts trailing requests that are not NoAction",
		func(methods []provisioningv1.RebootMethodType, want int32) {
			var state rebootState
			for i, m := range methods {
				state.record(rebootRequest{BootID: fmt.Sprintf("boot-%d", i), Method: m})
			}
			Expect(state.sequenceCount()).To(Equal(want))
		},
		Entry("no requests", nil, int32(0)),
		Entry("only NoAction", []provisioningv1.RebootMethodType{provisioningv1.RebootMethodNoAction}, int32(0)),
		Entry("requests after a NoAction", []provisioningv1.RebootMethodType{
			provisioningv1.RebootMethodPowerCycle, provisioningv1.RebootMethodNoAction,
			provisioningv1.RebootMethodSystemLevelReset, provisioningv1.RebootMethodDPUWarmReboot,
		}, int32(2)),
		Entry("ending with NoAction", []provisioningv1.RebootMethodType{
			provisioningv1.RebootMethodPowerCycle, provisioningv1.RebootMethodNoAction,
		}, int32(0)),
	)

	It("never returns an observation of the current boot and rotates only on a new boot", func() {
		var p pendingNVConfigState
		Expect(p.fromPreviousBoot("boot-1")).To(BeNil())

		p = p.commit(pendingObservation{BootID: "boot-1"})
		Expect(p.fromPreviousBoot("boot-1")).To(BeNil())
		Expect(p.fromPreviousBoot("boot-2")).To(Equal(&pendingObservation{BootID: "boot-1"}))

		p = p.commit(pendingObservation{BootID: "boot-2"})
		p = p.commit(pendingObservation{BootID: "boot-2", Devices: []pendingDevice{{Device: "d"}}})
		Expect(p.Previous).To(Equal(&pendingObservation{BootID: "boot-1"}))
		Expect(p.Latest).To(Equal(&pendingObservation{BootID: "boot-2", Devices: []pendingDevice{{Device: "d"}}}))
		Expect(p.fromPreviousBoot("boot-2")).To(Equal(&pendingObservation{BootID: "boot-1"}))
		Expect(p.fromPreviousBoot("boot-3")).To(Equal(p.Latest))
	})

	It("starts a new state on the current boot and fails on a corrupt file", func() {
		optCtx := &operations.Context{StateStore: newTestStateStore(), CurrentBootID: "boot-1"}
		state, err := loadState(optCtx.StateStore, optCtx.CurrentBootID)
		Expect(err).NotTo(HaveOccurred())
		Expect(state).To(Equal(rebootState{InitialBootID: "boot-1"}))

		Expect(optCtx.StateStore.Save(stateFileName, "not an object")).To(Succeed())
		_, err = loadState(optCtx.StateStore, optCtx.CurrentBootID)
		Expect(err).To(MatchError(ContainSubstring("load reboot state")))
	})

	It("ResetState removes the file so the next run starts over", func() {
		optCtx := &operations.Context{
			StateStore: newTestStateStoreWith(rebootState{
				InitialBootID: "boot-1",
				Requests:      requestsOnBoots("boot", provisioningv1.RebootMethodPowerCycle, 3),
				Pending:       pendingNVConfigState{Latest: &pendingObservation{BootID: "boot-3"}},
			}),
			CurrentBootID: "boot-4",
		}
		Expect(ResetState(optCtx.StateStore)).To(Succeed())
		Expect(ResetState(optCtx.StateStore)).To(Succeed())
		expectNoSavedState(optCtx)
		Expect(reportedSequenceCount(optCtx)).To(BeNil())
		state, err := loadState(optCtx.StateStore, optCtx.CurrentBootID)
		Expect(err).NotTo(HaveOccurred())
		Expect(state).To(Equal(rebootState{InitialBootID: "boot-4"}))
	})

	Describe("SequenceCountSource", func() {
		It("leaves the count unset without a file or without a recorded request", func() {
			optCtx := &operations.Context{StateStore: newTestStateStore()}
			Expect(reportedSequenceCount(optCtx)).To(BeNil())

			optCtx.StateStore = newTestStateStoreWith(rebootState{
				InitialBootID: "boot-1",
				Pending:       pendingNVConfigState{Latest: &pendingObservation{BootID: "boot-1"}},
			})
			Expect(reportedSequenceCount(optCtx)).To(BeNil())
		})

		It("reports 0 after NoAction and the trailing count otherwise", func() {
			optCtx := &operations.Context{StateStore: newTestStateStoreWith(rebootState{
				Requests: requestsOnBoots("boot", provisioningv1.RebootMethodNoAction, 1),
			})}
			Expect(reportedSequenceCount(optCtx)).To(Equal(ptr.To(int32(0))))

			optCtx.StateStore = newTestStateStoreWith(rebootState{
				Requests: requestsOnBoots("boot", provisioningv1.RebootMethodPowerCycle, 2),
			})
			Expect(reportedSequenceCount(optCtx)).To(Equal(ptr.To(int32(2))))
		})

		It("fails on a corrupt file", func() {
			store := newTestStateStore()
			Expect(store.Save(stateFileName, "not an object")).To(Succeed())
			Expect(SequenceCountSource(store)(&provisioningv1.AgentStatus{})).To(MatchError(ContainSubstring("load reboot state")))
		})
	})
})
