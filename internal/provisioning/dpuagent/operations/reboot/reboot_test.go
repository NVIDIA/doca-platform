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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	opts "github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/nvconfig"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/statestore"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/statusmanager"
	pciutil "github.com/nvidia/doca-platform/internal/provisioning/utils/pci"

	"github.com/Masterminds/semver/v3"
	nicconfigurationv1alpha1 "github.com/Mellanox/nic-configuration-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	testPCIAddress0 = "0000:03:00.0"
	testPCIAddress1 = "0000:03:00.1"
)

const (
	statusDPUNamespace = "ns"
	statusDPUName      = "dpu"
)

// newTestStatusClient returns a fake client holding the DPU that test status managers push to.
func newTestStatusClient() client.Client {
	scheme := runtime.NewScheme()
	Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
	dpu := &provisioningv1.DPU{ObjectMeta: metav1.ObjectMeta{Namespace: statusDPUNamespace, Name: statusDPUName}}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(dpu).WithStatusSubresource(dpu).Build()
}

// newTestStatusFor returns a started status manager that pushes to c. It stops when the spec ends.
func newTestStatusFor(c client.Client) *statusmanager.Manager {
	m := statusmanager.New(c, statusDPUNamespace, statusDPUName, "")
	ctx, cancel := context.WithCancel(context.Background())
	DeferCleanup(cancel)
	m.Start(ctx)
	return m
}

// newTestStatus returns a started status manager holding initial, if given, that pushes to a
// fake client of its own.
func newTestStatus(initial ...provisioningv1.AgentStatus) *statusmanager.Manager {
	m := newTestStatusFor(newTestStatusClient())
	for _, status := range initial {
		m.UpdateLocal(func(s *provisioningv1.AgentStatus) { *s = *status.DeepCopy() })
	}
	return m
}

// newTestStateStore returns an empty state store that is removed when the spec ends.
func newTestStateStore() *statestore.Store {
	return statestore.New(GinkgoT().TempDir(), "test-uid")
}

// newTestStateStoreWith returns a state store holding state as the HandleReboot file.
func newTestStateStoreWith(state rebootState) *statestore.Store {
	store := newTestStateStore()
	Expect(store.Save(stateFileName, state)).To(Succeed())
	return store
}

// savedState returns the HandleReboot state saved in optCtx.StateStore.
func savedState(optCtx *operations.Context) rebootState {
	var state rebootState
	Expect(optCtx.StateStore.Load(stateFileName, &state)).To(Succeed())
	return state
}

// expectNoSavedState asserts that HandleReboot has not saved any state.
func expectNoSavedState(optCtx *operations.Context) {
	var state rebootState
	Expect(errors.Is(optCtx.StateStore.Load(stateFileName, &state), os.ErrNotExist)).To(BeTrue())
}

// reportedSequenceCount returns what SequenceCountSource reports for optCtx.StateStore.
func reportedSequenceCount(optCtx *operations.Context) *int32 {
	s := &provisioningv1.AgentStatus{}
	Expect(SequenceCountSource(optCtx.StateStore)(s)).To(Succeed())
	return s.RebootSequenceCount
}

// requestsOnBoots returns one request with method per boot, oldest first, for boots named
// "<prefix>-1", "<prefix>-2", ...
func requestsOnBoots(prefix string, method provisioningv1.RebootMethodType, n int) []rebootRequest {
	requests := make([]rebootRequest, 0, n)
	for i := 1; i <= n; i++ {
		requests = append(requests, rebootRequest{BootID: fmt.Sprintf("%s-%d", prefix, i), Method: method})
	}
	return requests
}

// methods returns the method of every recorded request, oldest first.
func methods(state rebootState) []provisioningv1.RebootMethodType {
	out := make([]provisioningv1.RebootMethodType, 0, len(state.Requests))
	for _, r := range state.Requests {
		out = append(out, r.Method)
	}
	return out
}

// getRebootMethod runs h.getRebootMethod on the state saved in optCtx.StateStore, as Execute does.
func getRebootMethod(h *HandleReboot, optCtx *operations.Context) (*provisioningv1.RebootMethodType, error) {
	state, err := loadState(optCtx.StateStore, optCtx.CurrentBootID)
	Expect(err).NotTo(HaveOccurred())
	return h.getRebootMethod(optCtx, &state)
}

// pushedAgentStatus returns the AgentStatus pushed to c.
func pushedAgentStatus(c client.Client) *provisioningv1.AgentStatus {
	dpu := &provisioningv1.DPU{}
	Expect(c.Get(context.Background(), client.ObjectKey{Namespace: statusDPUNamespace, Name: statusDPUName}, dpu)).To(Succeed())
	Expect(dpu.Status.AgentStatus).NotTo(BeNil())
	return dpu.Status.AgentStatus
}

var _ = Describe("Reboot", func() {
	It("should not skip when SkipHWProvisioning is false", func() {
		h := &HandleReboot{}
		Expect(h.ShouldSkip(&operations.Context{})).To(BeFalse())
	})

	It("should not skip when SkipReboot is true", func() {
		h := &HandleReboot{}
		Expect(h.ShouldSkip(&operations.Context{Options: opts.Options{SkipReboot: true}})).To(BeFalse())
	})

	It("should report NoAction when SkipReboot is true without saved state", func() {
		h := &HandleReboot{}
		optCtx := &operations.Context{
			Options:    opts.Options{SkipReboot: true},
			StateStore: newTestStateStore(),
			Status: newTestStatus(provisioningv1.AgentStatus{
				RebootMethod: ptr.To(provisioningv1.RebootMethodUnknown),
			}),
		}

		err := h.Execute(context.Background(), optCtx)
		Expect(err).NotTo(HaveOccurred())
		Expect(optCtx.Status.GetLocal().RebootMethod).NotTo(BeNil())
		Expect(*optCtx.Status.GetLocal().RebootMethod).To(Equal(provisioningv1.RebootMethodNoAction))
		expectNoSavedState(optCtx)
	})

	Describe("RebootMethodDiscovery false (boot-ID based)", func() {
		Context("HandleReboot", func() {
			It("should reboot the host if DPU ARM has not been booted", func() {
				dpu := &provisioningv1.DPU{}
				bootID, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
				Expect(err).NotTo(HaveOccurred())
				bootIDStr := strings.TrimSpace(string(bootID))
				optCtx := &operations.Context{
					Status:                newTestStatus(),
					StateStore:            newTestStateStore(),
					LatestDPU:             dpu,
					RebootMethodDiscovery: false,
					CurrentBootID:         bootIDStr,
				}
				reboot := &HandleReboot{
					skipBlock: true,
					runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
						Expect(cmd).To(Equal(fmt.Sprintf("sleep %d && shutdown -h now", shutdownDelayInSeconds)))
						return bytes.Buffer{}, bytes.Buffer{}, nil
					},
				}
				Expect(reboot.Execute(context.Background(), optCtx)).To(Succeed())
				state := savedState(optCtx)
				Expect(state.InitialBootID).To(Equal(bootIDStr))
				Expect(methods(state)).To(Equal([]provisioningv1.RebootMethodType{provisioningv1.RebootMethodSystemLevelReset}))
				Expect(reportedSequenceCount(optCtx)).To(Equal(ptr.To(int32(1))))
				cond := meta.FindStatusCondition(optCtx.Status.GetLocal().Conditions, cutil.AgentCondRebootMethodDiscovery)
				Expect(cond).To(BeNil(), "legacy boot-ID path omits RebootMethodDiscovery condition")
			})

			It("should request a power cycle instead of a system-level reset on BlueField-4 with Astra enabled", func() {
				optCtx := &operations.Context{
					Status:     newTestStatus(),
					StateStore: newTestStateStore(),
					Options:    opts.Options{AstraEnabled: true},
					LatestDPU: &provisioningv1.DPU{
						Status: provisioningv1.DPUStatus{
							DPUType: provisioningv1.DPUTypeBlueField4,
						},
					},
					RebootMethodDiscovery: false,
					CurrentBootID:         "boot-id",
				}
				reboot := &HandleReboot{
					skipBlock: true,
					runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
						Fail("a power cycle runs no command on the DPU, got: " + cmd)
						return bytes.Buffer{}, bytes.Buffer{}, nil
					},
				}
				Expect(reboot.Execute(context.Background(), optCtx)).To(Succeed())
				Expect(optCtx.Status.GetLocal().RebootMethod).NotTo(BeNil())
				Expect(*optCtx.Status.GetLocal().RebootMethod).To(Equal(provisioningv1.RebootMethodPowerCycle))
				Expect(reportedSequenceCount(optCtx)).To(Equal(ptr.To(int32(1))))
				cond := meta.FindStatusCondition(optCtx.Status.GetLocal().Conditions, cutil.AgentCondRebootMethodDiscovery)
				Expect(cond).To(BeNil(), "legacy boot-ID path omits RebootMethodDiscovery condition")
			})

			It("should not reboot the host if DPU ARM has already been booted", func() {
				bootID, err := os.ReadFile(bootIDFile)
				Expect(err).NotTo(HaveOccurred())
				currentBootIDStr := strings.TrimSpace(string(bootID))
				aDifferentBootID := currentBootIDStr + "-other"

				optCtx := &operations.Context{
					Status: newTestStatus(),
					StateStore: newTestStateStoreWith(rebootState{
						InitialBootID: aDifferentBootID,
						Requests:      []rebootRequest{{BootID: aDifferentBootID, Method: provisioningv1.RebootMethodSystemLevelReset}},
					}),
					LatestDPU:             &provisioningv1.DPU{},
					RebootMethodDiscovery: false,
					CurrentBootID:         currentBootIDStr,
				}
				reboot := &HandleReboot{}
				Expect(reboot.Execute(context.Background(), optCtx)).To(Succeed())
				state := savedState(optCtx)
				Expect(state.InitialBootID).To(Equal(aDifferentBootID))
				Expect(methods(state)).To(Equal([]provisioningv1.RebootMethodType{
					provisioningv1.RebootMethodSystemLevelReset, provisioningv1.RebootMethodNoAction,
				}))
				Expect(reportedSequenceCount(optCtx)).To(Equal(ptr.To(int32(0))))
				cond := meta.FindStatusCondition(optCtx.Status.GetLocal().Conditions, cutil.AgentCondRebootMethodDiscovery)
				Expect(cond).To(BeNil(), "legacy boot-ID path omits RebootMethodDiscovery condition")
			})

			It("reboots once: same-boot retries repeat the request, later boots and restarts report NoAction", func() {
				store := newTestStateStore()
				run := func(bootID string) provisioningv1.RebootMethodType {
					optCtx := &operations.Context{
						Status:                newTestStatus(),
						StateStore:            store,
						LatestDPU:             &provisioningv1.DPU{},
						RebootMethodDiscovery: false,
						CurrentBootID:         bootID,
					}
					h := &HandleReboot{
						skipBlock: true,
						runBash: func(string) (bytes.Buffer, bytes.Buffer, error) {
							return bytes.Buffer{}, bytes.Buffer{}, nil
						},
					}
					Expect(h.Execute(context.Background(), optCtx)).To(Succeed())
					return *optCtx.Status.GetLocal().RebootMethod
				}

				Expect(run("boot-1")).To(Equal(provisioningv1.RebootMethodSystemLevelReset))
				Expect(run("boot-1")).To(Equal(provisioningv1.RebootMethodSystemLevelReset))
				optCtx := &operations.Context{StateStore: store}
				Expect(reportedSequenceCount(optCtx)).To(Equal(ptr.To(int32(1))))

				Expect(run("boot-2")).To(Equal(provisioningv1.RebootMethodNoAction))
				Expect(run("boot-2")).To(Equal(provisioningv1.RebootMethodNoAction))
				Expect(run("boot-3")).To(Equal(provisioningv1.RebootMethodNoAction))

				state := savedState(optCtx)
				Expect(state.InitialBootID).To(Equal("boot-1"))
				Expect(methods(state)).To(Equal([]provisioningv1.RebootMethodType{
					provisioningv1.RebootMethodSystemLevelReset, provisioningv1.RebootMethodNoAction, provisioningv1.RebootMethodNoAction,
				}))
				Expect(reportedSequenceCount(optCtx)).To(Equal(ptr.To(int32(0))))
			})

			It("reboots once more on an in-place reconfig in the legacy flow", func() {
				store := newTestStateStore()
				run := func(bootID string) provisioningv1.RebootMethodType {
					optCtx := &operations.Context{
						Status:                newTestStatus(),
						StateStore:            store,
						LatestDPU:             &provisioningv1.DPU{},
						RebootMethodDiscovery: false,
						CurrentBootID:         bootID,
					}
					h := &HandleReboot{
						skipBlock: true,
						runBash: func(string) (bytes.Buffer, bytes.Buffer, error) {
							return bytes.Buffer{}, bytes.Buffer{}, nil
						},
					}
					Expect(h.Execute(context.Background(), optCtx)).To(Succeed())
					return *optCtx.Status.GetLocal().RebootMethod
				}

				Expect(run("boot-1")).To(Equal(provisioningv1.RebootMethodSystemLevelReset))
				Expect(run("boot-2")).To(Equal(provisioningv1.RebootMethodNoAction))

				Expect(ResetState(store, "boot-2")).To(Succeed())
				Expect(run("boot-2")).To(Equal(provisioningv1.RebootMethodSystemLevelReset))
				Expect(run("boot-3")).To(Equal(provisioningv1.RebootMethodNoAction))

				state := savedState(&operations.Context{StateStore: store})
				Expect(state.InitialBootID).To(Equal("boot-2"))
				Expect(methods(state)).To(Equal([]provisioningv1.RebootMethodType{
					provisioningv1.RebootMethodSystemLevelReset, provisioningv1.RebootMethodNoAction,
				}))
			})

			It("returns error when rebootSequenceCount limit is reached and records nothing", func() {
				initial := rebootState{
					InitialBootID: "boot-id",
					Requests:      requestsOnBoots("prev", provisioningv1.RebootMethodSystemLevelReset, 5),
				}
				optCtx := &operations.Context{
					Status:                newTestStatus(),
					StateStore:            newTestStateStoreWith(initial),
					LatestDPU:             &provisioningv1.DPU{},
					RebootMethodDiscovery: false,
					CurrentBootID:         "boot-id",
				}
				reboot := &HandleReboot{skipBlock: true}
				err := reboot.Execute(context.Background(), optCtx)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("rebootSequenceCount limit exceeded (5 >= 5)"))
				Expect(savedState(optCtx)).To(Equal(initial))
			})

			It("does not refuse a retry on the boot that reached the limit", func() {
				requests := requestsOnBoots("prev", provisioningv1.RebootMethodSystemLevelReset, 4)
				requests = append(requests, rebootRequest{BootID: "boot-id", Method: provisioningv1.RebootMethodSystemLevelReset})
				optCtx := &operations.Context{
					Status:                newTestStatus(),
					StateStore:            newTestStateStoreWith(rebootState{InitialBootID: "boot-id", Requests: requests}),
					LatestDPU:             &provisioningv1.DPU{},
					RebootMethodDiscovery: false,
					CurrentBootID:         "boot-id",
				}
				reboot := &HandleReboot{
					skipBlock: true,
					runBash: func(string) (bytes.Buffer, bytes.Buffer, error) {
						return bytes.Buffer{}, bytes.Buffer{}, nil
					},
				}
				Expect(reboot.Execute(context.Background(), optCtx)).To(Succeed())
				Expect(savedState(optCtx).Requests).To(HaveLen(5))
				Expect(reportedSequenceCount(optCtx)).To(Equal(ptr.To(int32(5))))
			})

			It("keeps counting when a same-boot NoAction is replaced by a later request", func() {
				requests := requestsOnBoots("prev", provisioningv1.RebootMethodSystemLevelReset, 3)
				requests = append(requests, rebootRequest{BootID: "boot-id", Method: provisioningv1.RebootMethodNoAction})
				optCtx := &operations.Context{
					Status:                newTestStatus(),
					StateStore:            newTestStateStoreWith(rebootState{InitialBootID: "boot-id", Requests: requests}),
					LatestDPU:             &provisioningv1.DPU{},
					RebootMethodDiscovery: false,
					CurrentBootID:         "boot-id",
				}
				reboot := &HandleReboot{
					skipBlock: true,
					runBash: func(string) (bytes.Buffer, bytes.Buffer, error) {
						return bytes.Buffer{}, bytes.Buffer{}, nil
					},
				}
				Expect(reboot.Execute(context.Background(), optCtx)).To(Succeed())
				Expect(savedState(optCtx).Requests).To(HaveLen(4))
				Expect(reportedSequenceCount(optCtx)).To(Equal(ptr.To(int32(4))))
			})

			// WORKAROUND(mlxfwreset-no-reset-level): hostless-only. PowerCycle from that mlxfwreset
			// error may become NoAction at the reboot sequence limit; other PowerCycle causes must not.
			It("downgrades PowerCycle from no-reset-level to NoAction when rebootSequenceCount limit is reached", func() {
				device := testPCIAddress0
				dpu := &provisioningv1.DPU{
					Status: provisioningv1.DPUStatus{
						Hostless: true,
					},
				}
				optCtx := &operations.Context{
					Status: newTestStatus(),
					StateStore: newTestStateStoreWith(rebootState{
						InitialBootID: "prev-1",
						Requests:      requestsOnBoots("prev", provisioningv1.RebootMethodPowerCycle, 5),
					}),
					LatestDPU:             dpu,
					RebootMethodDiscovery: true,
					CurrentBootID:         "boot-id",
					DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
						return []pciutil.NICPort{{Netdev: "p0", PCIAddress: device}}, nil
					},
				}
				reboot := &HandleReboot{
					skipBlock: true,
					runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
						var stderr bytes.Buffer
						_, _ = stderr.WriteString("-E- No reset level is supported.")
						return bytes.Buffer{}, stderr, fmt.Errorf("exit status 1")
					},
				}
				Expect(reboot.Execute(context.Background(), optCtx)).To(Succeed())
				Expect(optCtx.Status.GetLocal().RebootMethod).NotTo(BeNil())
				Expect(*optCtx.Status.GetLocal().RebootMethod).To(Equal(provisioningv1.RebootMethodNoAction))
				state := savedState(optCtx)
				Expect(state.Requests).To(HaveLen(6))
				Expect(state.Requests[5].BootID).To(Equal("boot-id"))
				Expect(state.Requests[5].Method).To(Equal(provisioningv1.RebootMethodNoAction))
				Expect(reportedSequenceCount(optCtx)).To(Equal(ptr.To(int32(0))))
				cond := meta.FindStatusCondition(optCtx.Status.GetLocal().Conditions, cutil.AgentCondRebootMethodDiscovery)
				Expect(cond).NotTo(BeNil())
				Expect(cond.Reason).To(Equal(string(provisioningv1.RebootMethodNoAction)))
				Expect(cond.Message).To(ContainSubstring(noResetLevelSupportedText))
			})

			It("still returns PowerCycle for no-reset-level before the rebootSequenceCount limit", func() {
				device := testPCIAddress0
				dpu := &provisioningv1.DPU{
					Status: provisioningv1.DPUStatus{
						Hostless: true,
					},
				}
				optCtx := &operations.Context{
					Status: newTestStatus(),
					StateStore: newTestStateStoreWith(rebootState{
						InitialBootID: "prev-1",
						Requests:      requestsOnBoots("prev", provisioningv1.RebootMethodPowerCycle, 2),
					}),
					LatestDPU:             dpu,
					RebootMethodDiscovery: true,
					CurrentBootID:         "boot-id",
					DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
						return []pciutil.NICPort{{Netdev: "p0", PCIAddress: device}}, nil
					},
				}
				reboot := &HandleReboot{
					skipBlock: true,
					runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
						var stderr bytes.Buffer
						_, _ = stderr.WriteString("-E- No reset level is supported.")
						return bytes.Buffer{}, stderr, fmt.Errorf("exit status 1")
					},
				}
				Expect(reboot.Execute(context.Background(), optCtx)).To(Succeed())
				Expect(optCtx.Status.GetLocal().RebootMethod).NotTo(BeNil())
				Expect(*optCtx.Status.GetLocal().RebootMethod).To(Equal(provisioningv1.RebootMethodPowerCycle))
				Expect(reportedSequenceCount(optCtx)).To(Equal(ptr.To(int32(3))))
			})

			It("returns error for no-reset-level on non-hostless DPUs when rebootSequenceCount limit is reached", func() {
				device := testPCIAddress0
				dpu := &provisioningv1.DPU{
					Status: provisioningv1.DPUStatus{
						Hostless: false,
					},
				}
				optCtx := &operations.Context{
					Status: newTestStatus(),
					StateStore: newTestStateStoreWith(rebootState{
						InitialBootID: "prev-1",
						Requests:      requestsOnBoots("prev", provisioningv1.RebootMethodPowerCycle, 5),
					}),
					LatestDPU:             dpu,
					RebootMethodDiscovery: true,
					CurrentBootID:         "boot-id",
					DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
						return []pciutil.NICPort{{Netdev: "p0", PCIAddress: device}}, nil
					},
				}
				reboot := &HandleReboot{
					skipBlock: true,
					runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
						var stderr bytes.Buffer
						_, _ = stderr.WriteString("-E- No reset level is supported.")
						return bytes.Buffer{}, stderr, fmt.Errorf("exit status 1")
					},
				}
				err := reboot.Execute(context.Background(), optCtx)
				Expect(err).To(HaveOccurred())
				// PowerCycle from the non-hostless fallback is not downgraded to NoAction.
				Expect(err.Error()).To(ContainSubstring("rebootSequenceCount limit exceeded"))
				Expect(savedState(optCtx).Requests).To(HaveLen(5))
			})

			It("surfaces NVConfigApplied=False when the reboot limit is reached with deferred NVConfig params", func() {
				optCtx := &operations.Context{
					Status: newTestStatus(),
					StateStore: newTestStateStoreWith(rebootState{
						InitialBootID: "boot-id",
						Requests:      requestsOnBoots("prev", provisioningv1.RebootMethodSystemLevelReset, 5),
					}),
					LatestDPU:             &provisioningv1.DPU{},
					RebootMethodDiscovery: false,
					CurrentBootID:         "boot-id",
					DeferredNVConfigParams: []operations.DeferredNVConfigParam{
						{Device: testPCIAddress0, Params: "XYZ=228"},
					},
				}
				reboot := &HandleReboot{skipBlock: true}
				err := reboot.Execute(context.Background(), optCtx)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("rebootSequenceCount limit exceeded"))
				Expect(err.Error()).To(ContainSubstring("XYZ=228"))
				cond := meta.FindStatusCondition(optCtx.Status.GetLocal().Conditions, nvconfig.CondNVConfigApplied)
				Expect(cond).NotTo(BeNil())
				Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			})

			It("increments RebootSequenceCount on each non-NoAction reboot", func() {
				optCtx := &operations.Context{
					Status: newTestStatus(),
					StateStore: newTestStateStoreWith(rebootState{
						InitialBootID: "boot-id",
						Requests:      requestsOnBoots("prev", provisioningv1.RebootMethodSystemLevelReset, 2),
					}),
					LatestDPU:             &provisioningv1.DPU{},
					RebootMethodDiscovery: false,
					CurrentBootID:         "boot-id",
				}
				reboot := &HandleReboot{
					skipBlock: true,
					runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
						return bytes.Buffer{}, bytes.Buffer{}, nil
					},
				}
				Expect(reboot.Execute(context.Background(), optCtx)).To(Succeed())
				Expect(reportedSequenceCount(optCtx)).To(Equal(ptr.To(int32(3))))
			})
		})

		Context("DPUWarmReboot when GrubConfigChanged", func() {
			It("should trigger DPUWarmReboot via boot-ID path when GrubConfigChanged is true", func() {
				bootID, err := os.ReadFile(bootIDFile)
				Expect(err).NotTo(HaveOccurred())
				currentBootIDStr := strings.TrimSpace(string(bootID))
				aDifferentBootID := currentBootIDStr + "-other"

				var rebootCmd string
				statusClient := newTestStatusClient()
				optCtx := &operations.Context{
					Status:                newTestStatusFor(statusClient),
					StateStore:            newTestStateStoreWith(rebootState{InitialBootID: aDifferentBootID}),
					LatestDPU:             &provisioningv1.DPU{},
					RebootMethodDiscovery: false,
					CurrentBootID:         currentBootIDStr,
					GrubConfigChanged:     true,
				}
				h := &HandleReboot{
					skipBlock: true,
					runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
						rebootCmd = cmd
						return bytes.Buffer{}, bytes.Buffer{}, nil
					},
				}
				Expect(h.Execute(context.Background(), optCtx)).To(Succeed())
				Expect(rebootCmd).To(Equal(fmt.Sprintf("sleep %d && reboot", shutdownDelayInSeconds)))
				Expect(pushedAgentStatus(statusClient).RebootMethod).To(Equal(ptr.To(provisioningv1.RebootMethodDPUWarmReboot)))
				Expect(optCtx.Status.GetLocal().RebootMethod).NotTo(BeNil())
				Expect(*optCtx.Status.GetLocal().RebootMethod).To(Equal(provisioningv1.RebootMethodDPUWarmReboot))
			})

			It("should trigger DPUWarmReboot via device-query path when GrubConfigChanged is true", func() {
				var rebootCmd string
				statusClient := newTestStatusClient()
				optCtx := &operations.Context{
					Status:                newTestStatusFor(statusClient),
					StateStore:            newTestStateStore(),
					RebootMethodDiscovery: true,
					CurrentBootID:         "boot-id",
					DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
						return []pciutil.NICPort{{Netdev: "p0", PCIAddress: testPCIAddress0}}, nil
					},
					GrubConfigChanged: true,
				}
				h := &HandleReboot{
					skipBlock: true,
					runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
						if strings.Contains(cmd, "mlxfwreset") {
							var b bytes.Buffer
							_, _ = b.WriteString(`{"reset_needed":false}`)
							return b, bytes.Buffer{}, nil
						}
						rebootCmd = cmd
						return bytes.Buffer{}, bytes.Buffer{}, nil
					},
				}
				Expect(h.Execute(context.Background(), optCtx)).To(Succeed())
				Expect(rebootCmd).To(Equal(fmt.Sprintf("sleep %d && reboot", shutdownDelayInSeconds)))
				Expect(pushedAgentStatus(statusClient).RebootMethod).To(Equal(ptr.To(provisioningv1.RebootMethodDPUWarmReboot)), "status should be pushed before reboot")
				Expect(optCtx.Status.GetLocal().RebootMethod).NotTo(BeNil())
				Expect(*optCtx.Status.GetLocal().RebootMethod).To(Equal(provisioningv1.RebootMethodDPUWarmReboot))
			})

		})

		It("getRebootMethod returns PowerCycle immediately when mlxfwreset reports no reset level on hostless", func() {
			device := testPCIAddress0
			optCtx := &operations.Context{
				Status:     newTestStatus(),
				StateStore: newTestStateStore(),
				LatestDPU: &provisioningv1.DPU{
					Status: provisioningv1.DPUStatus{Hostless: true},
				},
				RebootMethodDiscovery: true,
				CurrentBootID:         "boot-id",
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{{Netdev: "p0", PCIAddress: device}}, nil
				},
			}
			h := &HandleReboot{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var stderr bytes.Buffer
					_, _ = stderr.WriteString("-E- No reset level is supported.")
					return bytes.Buffer{}, stderr, fmt.Errorf("exit status 1")
				},
			}
			m, err := getRebootMethod(h, optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(*m).To(Equal(provisioningv1.RebootMethodPowerCycle))
			cond := meta.FindStatusCondition(optCtx.Status.GetLocal().Conditions, cutil.AgentCondRebootMethodDiscovery)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal(string(provisioningv1.RebootMethodPowerCycle)))
			Expect(cond.Message).To(ContainSubstring(noResetLevelSupportedText))
		})

		It("getRebootMethod returns PowerCycle and reports the failure when mlxfwreset reports no reset level on non-hostless", func() {
			device := testPCIAddress0
			optCtx := &operations.Context{
				Status:     newTestStatus(),
				StateStore: newTestStateStore(),
				LatestDPU: &provisioningv1.DPU{
					Status: provisioningv1.DPUStatus{Hostless: false},
				},
				RebootMethodDiscovery: true,
				CurrentBootID:         "boot-id",
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{{Netdev: "p0", PCIAddress: device}}, nil
				},
			}
			h := &HandleReboot{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var stderr bytes.Buffer
					_, _ = stderr.WriteString("-E- No reset level is supported.")
					return bytes.Buffer{}, stderr, fmt.Errorf("exit status 1")
				},
			}
			m, err := getRebootMethod(h, optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(*m).To(Equal(provisioningv1.RebootMethodPowerCycle))
			cond := meta.FindStatusCondition(optCtx.Status.GetLocal().Conditions, cutil.AgentCondRebootMethodDiscovery)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal(string(provisioningv1.RebootMethodPowerCycle)))
			// The reboot method alone does not explain the power cycle.
			Expect(cond.Message).To(ContainSubstring("exit status 1"))
			Expect(cond.Message).To(ContainSubstring(noResetLevelSupportedText))
		})

		It("getRebootMethod does not query further PCI devices after no-reset-level PowerCycle", func() {
			queried := []string{}
			optCtx := &operations.Context{
				Status:     newTestStatus(),
				StateStore: newTestStateStore(),
				LatestDPU: &provisioningv1.DPU{
					Status: provisioningv1.DPUStatus{Hostless: true},
				},
				RebootMethodDiscovery: true,
				CurrentBootID:         "boot-id",
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{
						{Netdev: "p0", PCIAddress: testPCIAddress0},
						{Netdev: "p1", PCIAddress: testPCIAddress1},
					}, nil
				},
			}
			h := &HandleReboot{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					queried = append(queried, cmd)
					var stderr bytes.Buffer
					_, _ = stderr.WriteString("-E- No reset level is supported.")
					return bytes.Buffer{}, stderr, fmt.Errorf("exit status 1")
				},
			}
			m, err := getRebootMethod(h, optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(*m).To(Equal(provisioningv1.RebootMethodPowerCycle))
			Expect(queried).To(Equal([]string{fmt.Sprintf("mlxfwreset -d %s status --json", testPCIAddress0)}))
		})

		It("getRebootMethod fails on other mlxfwreset status errors", func() {
			device := testPCIAddress0
			optCtx := &operations.Context{
				Status:                newTestStatus(),
				StateStore:            newTestStateStore(),
				RebootMethodDiscovery: true,
				CurrentBootID:         "boot-id",
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{{Netdev: "p0", PCIAddress: device}}, nil
				},
			}
			h := &HandleReboot{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var stderr bytes.Buffer
					_, _ = stderr.WriteString("-E- Device not found")
					return bytes.Buffer{}, stderr, fmt.Errorf("exit status 1")
				},
			}
			m, err := getRebootMethod(h, optCtx)
			Expect(err).To(HaveOccurred())
			Expect(m).To(BeNil())
			Expect(err.Error()).To(ContainSubstring(fmt.Sprintf("mlxfwreset -d %s status --json", device)))
			Expect(err.Error()).To(ContainSubstring("Device not found"))
		})

		Context("getRebootMethod", func() {
			It("returns RebootMethodSystemLevelReset or RebootMethodNoAction", func() {
				currentBootID, err := os.ReadFile(bootIDFile)
				Expect(err).NotTo(HaveOccurred())
				currentBootIDStr := strings.TrimSpace(string(currentBootID))
				differentBootID := currentBootIDStr + "x"

				allowedMethods := map[provisioningv1.RebootMethodType]bool{
					provisioningv1.RebootMethodSystemLevelReset: true,
					provisioningv1.RebootMethodNoAction:         true,
				}

				h := &HandleReboot{}

				optCtxNotBooted := &operations.Context{LatestDPU: &provisioningv1.DPU{}, StateStore: newTestStateStore(), RebootMethodDiscovery: false, CurrentBootID: currentBootIDStr}
				m, err := getRebootMethod(h, optCtxNotBooted)
				Expect(err).NotTo(HaveOccurred())
				Expect(m).NotTo(BeNil())
				Expect(allowedMethods[*m]).To(BeTrue(), "getRebootMethod got %s", *m)
				Expect(*m).To(Equal(provisioningv1.RebootMethodSystemLevelReset))

				optCtxBooted := &operations.Context{
					RebootMethodDiscovery: false,
					CurrentBootID:         currentBootIDStr,
					LatestDPU:             &provisioningv1.DPU{},
					StateStore:            newTestStateStoreWith(rebootState{InitialBootID: differentBootID}),
				}
				m, err = getRebootMethod(h, optCtxBooted)
				Expect(err).NotTo(HaveOccurred())
				Expect(m).NotTo(BeNil())
				Expect(allowedMethods[*m]).To(BeTrue(), "getRebootMethod got %s", *m)
				Expect(*m).To(Equal(provisioningv1.RebootMethodNoAction))
			})
		})
	})

	Describe("RebootMethodDiscovery true (Device Query based)", func() {
		It("getRebootMethod returns SystemLevelReset when reset_needed is true", func() {
			device := testPCIAddress0

			optCtx := &operations.Context{
				Status:                newTestStatus(),
				StateStore:            newTestStateStore(),
				RebootMethodDiscovery: true,
				CurrentBootID:         "boot-id",
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{{Netdev: "p0", PCIAddress: device}}, nil
				},
			}
			ran := []string{}
			h := &HandleReboot{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					ran = append(ran, cmd)
					var b bytes.Buffer
					_, _ = b.WriteString(`{"reset_needed":true}`)
					return b, bytes.Buffer{}, nil
				},
			}
			m, err := getRebootMethod(h, optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(ran).To(Equal([]string{
				fmt.Sprintf("mlxfwreset -d %s status --json", device),
				fmt.Sprintf("mlxfwreset -d %s query", device),
			}))
			Expect(m).NotTo(BeNil())
			Expect(*m).To(Equal(provisioningv1.RebootMethodSystemLevelReset))
		})

		It("getRebootMethod returns SystemLevelReset when at least one device has reset_needed true", func() {
			devA := testPCIAddress0
			devB := testPCIAddress1

			optCtx := &operations.Context{
				Status:                newTestStatus(),
				StateStore:            newTestStateStore(),
				RebootMethodDiscovery: true,
				CurrentBootID:         "boot-id",
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{
						{Netdev: "p0", PCIAddress: devA},
						{Netdev: "p1", PCIAddress: devB},
					}, nil
				},
			}
			h := &HandleReboot{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var b bytes.Buffer
					switch {
					case strings.Contains(cmd, devA):
						_, _ = b.WriteString(`{"reset_needed":false}`)
					case strings.Contains(cmd, devB):
						_, _ = b.WriteString(`{"reset_needed":true}`)
					default:
						Fail("unexpected cmd: " + cmd)
					}
					return b, bytes.Buffer{}, nil
				},
			}
			m, err := getRebootMethod(h, optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(m).NotTo(BeNil())
			Expect(*m).To(Equal(provisioningv1.RebootMethodSystemLevelReset))
		})

		It("getRebootMethod returns NoAction when reset_needed is false", func() {
			device := testPCIAddress0

			optCtx := &operations.Context{
				Status:                newTestStatus(),
				StateStore:            newTestStateStore(),
				RebootMethodDiscovery: true,
				CurrentBootID:         "boot-id",
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{{Netdev: "p0", PCIAddress: device}}, nil
				},
			}
			h := &HandleReboot{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					Expect(cmd).To(Equal(fmt.Sprintf("mlxfwreset -d %s status --json", device)))
					var b bytes.Buffer
					_, _ = b.WriteString(`{"reset_needed":false}`)
					return b, bytes.Buffer{}, nil
				},
			}
			m, err := getRebootMethod(h, optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(m).NotTo(BeNil())
			Expect(*m).To(Equal(provisioningv1.RebootMethodNoAction))
			cond := meta.FindStatusCondition(optCtx.Status.GetLocal().Conditions, cutil.AgentCondRebootMethodDiscovery)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal(string(provisioningv1.RebootMethodNoAction)))
			Expect(cond.Message).To(BeEmpty())
		})

		It("getRebootMethod queries only PCI devices for the target NIC", func() {
			target0 := testPCIAddress0
			target1 := testPCIAddress1

			optCtx := &operations.Context{
				Status:                newTestStatus(),
				StateStore:            newTestStateStore(),
				RebootMethodDiscovery: true,
				CurrentBootID:         "boot-id",
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{
						{Netdev: "p0", PCIAddress: target0},
						{Netdev: "p1", PCIAddress: target1},
					}, nil
				},
			}
			ran := []string{}
			h := &HandleReboot{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					ran = append(ran, cmd)
					var b bytes.Buffer
					_, _ = b.WriteString(`{"reset_needed":false}`)
					return b, bytes.Buffer{}, nil
				},
			}
			m, err := getRebootMethod(h, optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(m).NotTo(BeNil())
			Expect(*m).To(Equal(provisioningv1.RebootMethodNoAction))
			Expect(ran).To(Equal([]string{
				fmt.Sprintf("mlxfwreset -d %s status --json", target0),
				fmt.Sprintf("mlxfwreset -d %s status --json", target1),
			}))
		})

		It("getRebootMethod returns NoAction when reset_needed is omitted from mlxfwreset JSON", func() {
			device := testPCIAddress0

			optCtx := &operations.Context{
				Status:                newTestStatus(),
				StateStore:            newTestStateStore(),
				RebootMethodDiscovery: true,
				CurrentBootID:         "boot-id",
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{{Netdev: "p0", PCIAddress: device}}, nil
				},
			}
			h := &HandleReboot{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					Expect(cmd).To(Equal(fmt.Sprintf("mlxfwreset -d %s status --json", device)))
					var b bytes.Buffer
					// Omitted reset_needed unmarshals to *bool nil; must not be treated as reset required.
					_, _ = b.WriteString(`{"fw_version":"1.2.3"}`)
					return b, bytes.Buffer{}, nil
				},
			}
			m, err := getRebootMethod(h, optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(m).NotTo(BeNil())
			Expect(*m).To(Equal(provisioningv1.RebootMethodNoAction))
		})

		It("Execute fails when deferred NVConfig params remain and reboot method is NoAction", func() {
			device := testPCIAddress0
			mlxfwresetJSON := strings.TrimSpace(`
{
  "reset_needed": false,
  "pending_nvconfig_parameters": "N/A",
  "reasons": []
}
`)
			optCtx := &operations.Context{
				RebootMethodDiscovery: true,
				CurrentBootID:         "current-boot-id",
				DeferredNVConfigParams: []operations.DeferredNVConfigParam{
					{Device: device, Params: "INVALID_PARAM=1"},
				},
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{{Netdev: "p0", PCIAddress: device}}, nil
				},
				Status: newTestStatus(provisioningv1.AgentStatus{
					Conditions: []metav1.Condition{},
				}),
				StateStore: newTestStateStore(),
			}
			h := &HandleReboot{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var b bytes.Buffer
					_, _ = b.WriteString(mlxfwresetJSON)
					return b, bytes.Buffer{}, nil
				},
			}
			err := h.Execute(context.Background(), optCtx)
			expectedMsg := fmt.Sprintf(
				"check DPUFlavor NVConfig: device=%s unapplied NVConfig params not exposed by mlxconfig q: [INVALID_PARAM=1]",
				device,
			)
			Expect(err).To(MatchError(expectedMsg))
			nvCond := meta.FindStatusCondition(optCtx.Status.GetLocal().Conditions, nvconfig.CondNVConfigApplied)
			Expect(nvCond).NotTo(BeNil())
			Expect(nvCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(nvCond.Reason).To(Equal(nvconfig.CondNVConfigApplied))
			Expect(nvCond.Message).To(Equal(expectedMsg))
		})

		It("Execute succeeds when deferred NVConfig params remain but reboot is required", func() {
			device := testPCIAddress0
			mlxfwresetJSON := strings.TrimSpace(`
{
  "reset_needed": true,
  "command_required": "Reboot external host is required",
  "reasons": ["Pending NVCONFIG parameter change"]
}
`)
			optCtx := &operations.Context{
				Status:                newTestStatus(),
				StateStore:            newTestStateStore(),
				RebootMethodDiscovery: true,
				CurrentBootID:         "current-boot-id",
				DeferredNVConfigParams: []operations.DeferredNVConfigParam{
					{Device: device, Params: "INTERNAL_CPU_MODEL=1"},
				},
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{{Netdev: "p0", PCIAddress: device}}, nil
				},
			}
			h := &HandleReboot{
				skipBlock: true,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					if strings.Contains(cmd, "mlxfwreset") {
						var b bytes.Buffer
						_, _ = b.WriteString(mlxfwresetJSON)
						return b, bytes.Buffer{}, nil
					}
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
			}
			err := h.Execute(context.Background(), optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(optCtx.Status.GetLocal().RebootMethod).To(Equal(ptr.To(provisioningv1.RebootMethodSystemLevelReset)))
		})

		Context("removeForeverPending workaround", func() {
			// observation returns a pending NVConfig observation of one device on bootID.
			observation := func(bootID, device string, entries ...pendingEntry) *pendingObservation {
				return &pendingObservation{BootID: bootID, Devices: []pendingDevice{{Device: device, Entries: entries}}}
			}
			// latestObservation returns the newest saved observation without its timestamp.
			latestObservation := func(optCtx *operations.Context) *pendingObservation {
				latest := savedState(optCtx).Pending.Latest
				Expect(latest).NotTo(BeNil())
				Expect(latest.ObservedAt.IsZero()).To(BeFalse())
				latest.ObservedAt = time.Time{}
				return latest
			}

			It("stores current pending NVConfig entries in the state store", func() {
				device := testPCIAddress0
				currentBootID, err := getCurrentRebootID()
				Expect(err).NotTo(HaveOccurred())

				mlxfwresetJSON := strings.TrimSpace(`
{
  "reset_needed": true,
  "pending_nvconfig_parameters": [
    {"name": "PARAM_A", "current": "0", "next_boot": "1"},
    {"name": "PARAM_B", "current": "x", "next_boot": "y"}
  ],
  "reasons": ["Pending NVCONFIG parameter change"]
}
`)
				optCtx := &operations.Context{
					Status:                newTestStatus(),
					StateStore:            newTestStateStore(),
					RebootMethodDiscovery: true,
					CurrentBootID:         currentBootID,
					DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
						return []pciutil.NICPort{{Netdev: "p0", PCIAddress: device}}, nil
					},
				}
				h := &HandleReboot{
					runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
						var b bytes.Buffer
						_, _ = b.WriteString(mlxfwresetJSON)
						return b, bytes.Buffer{}, nil
					},
				}
				_, err = getRebootMethod(h, optCtx)
				Expect(err).NotTo(HaveOccurred())
				Expect(latestObservation(optCtx)).To(Equal(observation(currentBootID, device,
					pendingEntry{Name: "PARAM_A", Current: "0", NextBoot: "1"},
					pendingEntry{Name: "PARAM_B", Current: "x", NextBoot: "y"},
				)))
				Expect(savedState(optCtx).Pending.Previous).To(BeNil())
			})

			It("stores pending NVConfig state when reset_needed is false", func() {
				device := testPCIAddress0

				mlxfwresetJSON := strings.TrimSpace(`
{
  "reset_needed": false,
  "pending_nvconfig_parameters": "N/A (No pending NVCONFIG parameters)",
  "reasons": []
}
`)
				optCtx := &operations.Context{
					Status:                newTestStatus(),
					StateStore:            newTestStateStore(),
					RebootMethodDiscovery: true,
					CurrentBootID:         "boot-id",
					DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
						return []pciutil.NICPort{{Netdev: "p0", PCIAddress: device}}, nil
					},
				}
				h := &HandleReboot{
					runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
						var b bytes.Buffer
						_, _ = b.WriteString(mlxfwresetJSON)
						return b, bytes.Buffer{}, nil
					},
				}
				_, err := getRebootMethod(h, optCtx)
				Expect(err).NotTo(HaveOccurred())
				Expect(latestObservation(optCtx)).To(Equal(&pendingObservation{
					BootID:  "boot-id",
					Devices: []pendingDevice{{Device: device, Entries: []pendingEntry{}}},
				}))
			})

			It("ignores repeated pending NVConfig entries after boot change when they are the only reason", func() {
				device := testPCIAddress0
				currentBootID, err := getCurrentRebootID()
				Expect(err).NotTo(HaveOccurred())
				cmd := fmt.Sprintf("mlxfwreset -d %s reset --level 3", device)
				stuck := pendingEntry{Name: "PARAM_A", Default: "2", Current: "0", NextBoot: "1"}

				mlxfwresetJSON := fmt.Sprintf(`{
  "reset_needed": true,
  "pending_nvconfig_parameters": [
    {"name": "PARAM_A", "default": "2", "current": "0", "next_boot": "1"}
  ],
  "command_required": %q,
  "reasons": ["Pending NVCONFIG parameter change"]
}`, cmd)
				previous := observation("previous-boot", device, stuck)
				optCtx := &operations.Context{
					Status: newTestStatus(),
					StateStore: newTestStateStoreWith(rebootState{
						InitialBootID: "previous-boot",
						Pending:       pendingNVConfigState{Latest: previous},
					}),
					RebootMethodDiscovery: true,
					DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
						return []pciutil.NICPort{{Netdev: "p0", PCIAddress: device}}, nil
					},
					CurrentBootID: currentBootID,
					LatestDPU:     &provisioningv1.DPU{},
				}
				h := &HandleReboot{
					runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
						var b bytes.Buffer
						_, _ = b.WriteString(mlxfwresetJSON)
						return b, bytes.Buffer{}, nil
					},
				}
				m, err := getRebootMethod(h, optCtx)
				Expect(err).NotTo(HaveOccurred())
				Expect(*m).To(Equal(provisioningv1.RebootMethodNoAction))
				Expect(optCtx.CondMessage).To(Equal(fmt.Sprintf(
					"device=%s pending NVCONFIG params did not take effect after reboot: [PARAM_A(default=2,current=0,next=1)]; reset ignored because stuck pending NVCONFIG params did not progress after reboot.",
					device,
				)))
				Expect(latestObservation(optCtx)).To(Equal(observation(currentBootID, device, stuck)))
				Expect(savedState(optCtx).Pending.Previous).To(Equal(previous), "a new boot moves the latest observation to previous")
			})

			It("identifies desired N/S NVConfig parameters", func() {
				resolved := operations.ResolvedNVConfig{
					PCIToParams: map[string]operations.NVConfigParams{
						testPCIAddress0: {Params: "INVALID param_a=v1 PARAM_B=v2"},
					},
				}
				optCtx := &operations.Context{Status: newTestStatus()}
				optCtx.SetResolvedNVConfig(&resolved)

				desired, err := getDesiredNVConfigParameters(optCtx, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(desired.contains(testPCIAddress0, "PARAM_A")).To(BeTrue())
				Expect(desired.contains(testPCIAddress0, "PARAM_B")).To(BeTrue())
				Expect(desired.contains(testPCIAddress0, "INVALID")).To(BeFalse())
				Expect(desired.contains(testPCIAddress1, "PARAM_A")).To(BeFalse())
			})

			It("identifies desired E/W NVConfig parameters", func() {
				optCtx := &operations.Context{
					Status:     newTestStatus(),
					StateStore: newTestStateStore(),
					Options:    opts.Options{AstraEnabled: true},
					LatestDPU: &provisioningv1.DPU{
						Status: provisioningv1.DPUStatus{DPUType: provisioningv1.DPUTypeBlueField4},
					},
					DPUFlavor: provisioningv1.DPUFlavor{
						Spec: provisioningv1.DPUFlavorSpec{
							EWNicConfigurations: []provisioningv1.NicConfiguration{
								{
									RawNvConfig: []nicconfigurationv1alpha1.NvConfigParam{
										{Name: "param_a"},
										{Name: "PARAM_B"},
									},
								},
							},
						},
					},
				}
				optCtx.SetResolvedNVConfig(&operations.ResolvedNVConfig{})
				devices := []pciutil.NICPort{
					{PCIAddress: testPCIAddress1, DeviceID: "0x1025"},
				}

				desired, err := getDesiredNVConfigParameters(optCtx, devices)
				Expect(err).NotTo(HaveOccurred())
				Expect(desired.contains(testPCIAddress1, "PARAM_A")).To(BeTrue())
				Expect(desired.contains(testPCIAddress1, "PARAM_B")).To(BeTrue())
				Expect(desired.contains(testPCIAddress0, "PARAM_A")).To(BeFalse())
			})

			verifyDesiredFiltering := func(isEW bool) {
				device := testPCIAddress0
				mlxfwresetOutput := pendingParamList{
					{Name: "PARAM_A", Current: "a", NextBoot: "next-a"},
					{Name: "PARAM_B", Current: "b", NextBoot: "next-b"},
				}
				optCtx := &operations.Context{
					Status:        newTestStatus(),
					CurrentBootID: "current-boot",
					LatestDPU: &provisioningv1.DPU{
						Status: provisioningv1.DPUStatus{
							DPUType: provisioningv1.DPUTypeBlueField4,
						},
					},
				}
				baseline := observation("previous-boot", device,
					pendingEntry{Name: "PARAM_A", Current: "a", NextBoot: "next-a"},
					pendingEntry{Name: "PARAM_B", Current: "b", NextBoot: "next-b"},
				)
				resolved := operations.ResolvedNVConfig{
					PCIToParams: map[string]operations.NVConfigParams{
						device: {Params: "PARAM_A=desired"},
					},
				}
				devices := []pciutil.NICPort{
					{PCIAddress: device, DeviceID: pciutil.BlueField4DeviceID},
				}
				if isEW {
					optCtx.Options.AstraEnabled = true
					optCtx.DPUFlavor.Spec.EWNicConfigurations = []provisioningv1.NicConfiguration{
						{
							RawNvConfig: []nicconfigurationv1alpha1.NvConfigParam{
								{Name: "PARAM_A", Value: "desired"},
							},
						},
					}
					resolved = operations.ResolvedNVConfig{}
					devices[0].DeviceID = "0x1025"
				}
				optCtx.SetResolvedNVConfig(&resolved)
				desiredParameters, err := getDesiredNVConfigParameters(optCtx, devices)
				Expect(err).NotTo(HaveOccurred())

				effective, shouldIgnore, _ := removeForeverPending(baseline, desiredParameters, device, mlxfwresetStatusJSON{
					PendingNvconfigParameters: mlxfwresetOutput,
				})
				Expect(shouldIgnore).To(BeFalse())
				Expect(effective.PendingNvconfigParameters).To(Equal(pendingParamList{mlxfwresetOutput[0]}))
			}

			It("keeps desired N/S entries and filters only undesired stuck entries", func() {
				verifyDesiredFiltering(false)
			})

			It("keeps desired E/W entries and filters only undesired stuck entries", func() {
				verifyDesiredFiltering(true)
			})

			It("filters stuck pending NVConfig entries per entry before selecting reboot method", func() {
				device := testPCIAddress0
				cmd := fmt.Sprintf("mlxfwreset -d %s reset --level 3", device)

				mlxfwresetJSON := fmt.Sprintf(`{
  "reset_needed": true,
  "pending_nvconfig_parameters": [
    {"name": "INTERNAL_CPU_MODEL", "default": "model-default", "current": "x", "next_boot": "y"},
    {"name": "ROCE_CONTROL", "default": "ROCE_ENABLE(2)", "current": "DEVICE_DEFAULT(0)", "next_boot": "ROCE_DISABLE(1)"}
  ],
  "command_required": %q,
  "reasons": ["Pending NVCONFIG parameter change"]
}`, cmd)
				optCtx := &operations.Context{
					Status: newTestStatus(),
					StateStore: newTestStateStoreWith(rebootState{Pending: pendingNVConfigState{
						Latest: observation("previous-boot", device,
							pendingEntry{Name: "INTERNAL_CPU_MODEL", Default: "model-default", Current: "x", NextBoot: "y"}),
					}}),
					RebootMethodDiscovery: true,
					DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
						return []pciutil.NICPort{{Netdev: "p0", PCIAddress: device}}, nil
					},
					CurrentBootID: "current-boot-id",
					LatestDPU: &provisioningv1.DPU{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								cutil.AgentAnnotationAllowFirmwareResetReboot: "true",
							},
						},
					},
				}
				h := &HandleReboot{
					runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
						var b bytes.Buffer
						_, _ = b.WriteString(mlxfwresetJSON)
						return b, bytes.Buffer{}, nil
					},
				}
				m, err := getRebootMethod(h, optCtx)
				Expect(err).NotTo(HaveOccurred())
				Expect(*m).To(Equal(provisioningv1.RebootMethodFirmwareReset))
				Expect(optCtx.CondMessage).To(Equal(fmt.Sprintf(
					"device=%s pending NVCONFIG params did not take effect after reboot: [INTERNAL_CPU_MODEL(default=model-default,current=x,next=y)]; reset still required because unapplied pending NVCONFIG parameters remain.",
					device,
				)))
			})

			It("ignores reset when stuck pending NVCONFIG remains despite extra mlxfwreset reasons", func() {
				device := testPCIAddress0
				currentBootID := "current-boot-id"
				stuck := pendingEntry{Name: "PARAM_A", Default: "2", Current: "0", NextBoot: "1"}

				mlxfwresetJSON := `{
  "reset_needed": true,
  "pending_nvconfig_parameters": [
    {"name": "PARAM_A", "default": "2", "current": "0", "next_boot": "1"}
  ],
  "command_required": "Reboot external host is required",
  "reasons": ["Pending NVCONFIG parameter change", "PCI rescan is required"]
}`
				optCtx := &operations.Context{
					Status: newTestStatus(),
					StateStore: newTestStateStoreWith(rebootState{Pending: pendingNVConfigState{
						Latest: observation("previous-boot", device, stuck),
					}}),
					RebootMethodDiscovery: true,
					DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
						return []pciutil.NICPort{{Netdev: "p0", PCIAddress: device}}, nil
					},
					CurrentBootID: currentBootID,
					LatestDPU:     &provisioningv1.DPU{},
				}
				h := &HandleReboot{
					runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
						var b bytes.Buffer
						_, _ = b.WriteString(mlxfwresetJSON)
						return b, bytes.Buffer{}, nil
					},
				}

				m, err := getRebootMethod(h, optCtx)
				Expect(err).NotTo(HaveOccurred())
				Expect(*m).To(Equal(provisioningv1.RebootMethodNoAction))
				Expect(optCtx.CondMessage).To(Equal(fmt.Sprintf(
					"device=%s pending NVCONFIG params did not take effect after reboot: [PARAM_A(default=2,current=0,next=1)]; reset ignored because stuck pending NVCONFIG params did not progress after reboot.",
					device,
				)))
				Expect(latestObservation(optCtx)).To(Equal(observation(currentBootID, device, stuck)))
			})

			It("compares a discovery repeated on the same boot against the previous boot", func() {
				device := testPCIAddress0
				stuck := pendingEntry{Name: "INTERNAL_CPU_MODEL", Current: "x", NextBoot: "y"}
				mlxfwresetJSON := fmt.Sprintf(`{
  "reset_needed": true,
  "pending_nvconfig_parameters": [
    {"name": "INTERNAL_CPU_MODEL", "current": "x", "next_boot": "y"}
  ],
  "command_required": %q,
  "reasons": ["Pending NVCONFIG parameter change"]
}`, fmt.Sprintf("mlxfwreset -d %s reset --level 3", device))
				newCtx := func(store *statestore.Store) *operations.Context {
					return &operations.Context{
						Status:                newTestStatus(),
						StateStore:            store,
						RebootMethodDiscovery: true,
						DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
							return []pciutil.NICPort{{Netdev: "p0", PCIAddress: device}}, nil
						},
						CurrentBootID: "boot-2",
						LatestDPU:     &provisioningv1.DPU{},
					}
				}
				h := &HandleReboot{
					runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
						var b bytes.Buffer
						_, _ = b.WriteString(mlxfwresetJSON)
						return b, bytes.Buffer{}, nil
					},
				}

				By("a boot with a previous-boot baseline ignores the stuck entry, also when discovery runs again")
				store := newTestStateStoreWith(rebootState{Pending: pendingNVConfigState{Latest: observation("boot-1", device, stuck)}})
				for range 2 {
					m, err := getRebootMethod(h, newCtx(store))
					Expect(err).NotTo(HaveOccurred())
					Expect(*m).To(Equal(provisioningv1.RebootMethodNoAction))
				}

				By("an observation of the current boot is never used as the baseline")
				store = newTestStateStoreWith(rebootState{Pending: pendingNVConfigState{Latest: observation("boot-2", device, stuck)}})
				m, err := getRebootMethod(h, newCtx(store))
				Expect(err).NotTo(HaveOccurred())
				Expect(*m).To(Equal(provisioningv1.RebootMethodPowerCycle))
			})

			Context("in-place reconfig on a DPU that has a forever-pending NVConfig parameter", func() {
				const device = testPCIAddress0
				stuck := pendingEntry{Name: "VIRTIO_NET_EMULATION_NUM_MSIX", Default: "2", Current: "0", NextBoot: "2"}
				var (
					store *statestore.Store
					// provisioning is the observation of boot-1, the baseline of a reconfig on boot-2.
					provisioning *pendingObservation
				)

				// The DPU set PARAM_K1 from v0 to v1 on boot-1 and rebooted, then ignored the stuck
				// parameter on boot-2 and became Ready. No flavor is set, so no parameter counts as
				// desired and only the comparison against the baseline decides.
				BeforeEach(func() {
					provisioning = observation("boot-1", device, pendingEntry{Name: "PARAM_K1", Current: "v0", NextBoot: "v1"}, stuck)
					store = newTestStateStoreWith(rebootState{
						InitialBootID: "boot-1",
						Requests: []rebootRequest{
							{BootID: "boot-1", Method: provisioningv1.RebootMethodSystemLevelReset},
							{BootID: "boot-2", Method: provisioningv1.RebootMethodNoAction},
						},
						Pending: pendingNVConfigState{
							Previous: provisioning,
							Latest:   observation("boot-2", device, stuck),
						},
					})
				})

				// handleReboot runs HandleReboot on bootID while mlxfwreset reports pending, and
				// returns the reboot method it concluded.
				handleReboot := func(bootID string, pending ...pendingEntry) (provisioningv1.RebootMethodType, *operations.Context) {
					optCtx := &operations.Context{
						Status:                newTestStatus(),
						StateStore:            store,
						RebootMethodDiscovery: true,
						DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
							return []pciutil.NICPort{{Netdev: "p0", PCIAddress: device}}, nil
						},
						CurrentBootID: bootID,
						LatestDPU:     &provisioningv1.DPU{},
					}
					h := &HandleReboot{
						skipBlock: true,
						runBash: func(string) (bytes.Buffer, bytes.Buffer, error) {
							params, err := json.Marshal(pending)
							Expect(err).NotTo(HaveOccurred())
							var b bytes.Buffer
							_, _ = fmt.Fprintf(&b, `{"reset_needed": true, "pending_nvconfig_parameters": %s, "command_required": %q}`,
								params, systemLevelResetCommandRequiredText)
							return b, bytes.Buffer{}, nil
						},
					}
					Expect(h.Execute(context.Background(), optCtx)).To(Succeed())
					return *optCtx.Status.GetLocal().RebootMethod, optCtx
				}
				// reconfig is the in-place reconfig Run on boot-2: it resets the state, then handles the reboot.
				reconfig := func(pending ...pendingEntry) (provisioningv1.RebootMethodType, *operations.Context) {
					Expect(ResetState(store, "boot-2")).To(Succeed())
					return handleReboot("boot-2", pending...)
				}
				// expectRebootThenNoAction checks a reconfig that makes changed pending next to the
				// stuck parameter: it reboots once, and the next boot reports NoAction.
				expectRebootThenNoAction := func(changed pendingEntry) {
					By("the reconfig on boot-2 keeps the changed parameter and filters only the stuck one")
					method, optCtx := reconfig(changed, stuck)
					Expect(method).To(Equal(provisioningv1.RebootMethodSystemLevelReset))
					Expect(optCtx.CondMessage).To(ContainSubstring(
						"[VIRTIO_NET_EMULATION_NUM_MSIX(default=2,current=0,next=2)]; reset still required"))
					Expect(savedState(optCtx).Pending.Previous).To(Equal(provisioning))
					Expect(latestObservation(optCtx)).To(Equal(observation("boot-2", device, changed, stuck)))

					By("boot-3 compares against the reconfig observation and ignores the stuck parameter")
					method, optCtx = handleReboot("boot-3", stuck)
					Expect(method).To(Equal(provisioningv1.RebootMethodNoAction))
					state := savedState(optCtx)
					Expect(state.InitialBootID).To(Equal("boot-2"))
					Expect(methods(state)).To(Equal([]provisioningv1.RebootMethodType{
						provisioningv1.RebootMethodSystemLevelReset, provisioningv1.RebootMethodNoAction,
					}))
					Expect(state.Pending.Previous).NotTo(BeNil())
					state.Pending.Previous.ObservedAt = time.Time{}
					Expect(state.Pending.Previous).To(Equal(observation("boot-2", device, changed, stuck)))
				}

				It("does not reboot when the reconfig leaves NVConfig unchanged", func() {
					method, optCtx := reconfig(stuck)
					Expect(method).To(Equal(provisioningv1.RebootMethodNoAction))
					Expect(optCtx.CondMessage).To(ContainSubstring(
						"[VIRTIO_NET_EMULATION_NUM_MSIX(default=2,current=0,next=2)]; reset ignored"))
					state := savedState(optCtx)
					Expect(state.InitialBootID).To(Equal("boot-2"))
					Expect(methods(state)).To(Equal([]provisioningv1.RebootMethodType{provisioningv1.RebootMethodNoAction}))
					Expect(state.Pending.Previous).To(Equal(provisioning), "the baseline stays for the next reconfig on this boot")
					Expect(latestObservation(optCtx)).To(Equal(observation("boot-2", device, stuck)))
				})

				It("reboots once when the reconfig changes the value of an NVConfig parameter that is already set", func() {
					expectRebootThenNoAction(pendingEntry{Name: "PARAM_K1", Current: "v1", NextBoot: "v2"})
				})

				It("reboots once when the reconfig adds a new NVConfig parameter", func() {
					expectRebootThenNoAction(pendingEntry{Name: "PARAM_K2", Current: "v0", NextBoot: "v2"})
				})
			})

			It("keeps a stuck pending parameter that the live flavor still requests", func() {
				device := testPCIAddress0
				baseline := observation("boot-1", device, pendingEntry{Name: "PARAM_A", Current: "0", NextBoot: "1"})
				desired := desiredNVConfigParameters{device: []string{"PARAM_A"}}
				out := mlxfwresetStatusJSON{
					ResetNeeded: ptr.To(true),
					PendingNvconfigParameters: pendingParamList{{
						Name: "PARAM_A", Current: "0", NextBoot: "1",
					}},
				}
				filtered, ignore, _ := removeForeverPending(baseline, desired, device, out)
				Expect(ignore).To(BeFalse())
				Expect(filtered.PendingNvconfigParameters).To(ConsistOf(pendingEntry{
					Name: "PARAM_A", Current: "0", NextBoot: "1",
				}))
			})

			It("does not save the observation when a device query fails", func() {
				device := testPCIAddress0
				stuck := pendingEntry{Name: "PARAM_A", Current: "0", NextBoot: "1"}
				initial := rebootState{Pending: pendingNVConfigState{Latest: observation("boot-1", device, stuck)}}
				optCtx := &operations.Context{
					Status:                newTestStatus(),
					StateStore:            newTestStateStoreWith(initial),
					RebootMethodDiscovery: true,
					DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
						return []pciutil.NICPort{{Netdev: "p0", PCIAddress: device}, {Netdev: "p1", PCIAddress: testPCIAddress1}}, nil
					},
					CurrentBootID: "boot-2",
					LatestDPU:     &provisioningv1.DPU{},
				}
				h := &HandleReboot{
					runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
						if strings.Contains(cmd, testPCIAddress1) {
							return bytes.Buffer{}, bytes.Buffer{}, fmt.Errorf("exit status 1")
						}
						var b bytes.Buffer
						_, _ = b.WriteString(`{"reset_needed":true,"pending_nvconfig_parameters":[{"name":"PARAM_A","current":"0","next_boot":"1"}]}`)
						return b, bytes.Buffer{}, nil
					},
				}
				_, err := getRebootMethod(h, optCtx)
				Expect(err).To(HaveOccurred())
				Expect(savedState(optCtx)).To(Equal(initial))
			})

			It("fails when the observation cannot be saved", func() {
				device := testPCIAddress0
				notADir := filepath.Join(GinkgoT().TempDir(), "file")
				Expect(os.WriteFile(notADir, nil, 0o600)).To(Succeed())
				optCtx := &operations.Context{
					Status:                newTestStatus(),
					StateStore:            statestore.New(notADir, "test-uid"),
					RebootMethodDiscovery: true,
					DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
						return []pciutil.NICPort{{Netdev: "p0", PCIAddress: device}}, nil
					},
					CurrentBootID: "boot-2",
					LatestDPU:     &provisioningv1.DPU{},
				}
				state := rebootState{Pending: pendingNVConfigState{
					Latest: observation("boot-1", device, pendingEntry{Name: "PARAM_A", Current: "0", NextBoot: "1"}),
				}}
				h := &HandleReboot{
					runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
						var b bytes.Buffer
						_, _ = b.WriteString(`{"reset_needed":true,"pending_nvconfig_parameters":[{"name":"PARAM_A","current":"0","next_boot":"1"}]}`)
						return b, bytes.Buffer{}, nil
					},
				}
				_, err := h.getRebootMethod(optCtx, &state)
				Expect(err).To(MatchError(ContainSubstring("save reboot state")))
			})
		})

		It("getRebootMethod returns error when no PCI devices are found", func() {
			optCtx := &operations.Context{
				Status:                newTestStatus(),
				StateStore:            newTestStateStore(),
				RebootMethodDiscovery: true,
				CurrentBootID:         "boot-id",
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return nil, fmt.Errorf("no PCI devices found")
				},
			}
			h := &HandleReboot{}
			_, err := getRebootMethod(h, optCtx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no PCI devices found"))
		})

		It("HandleReboot sets RebootMethodDiscovery True when device-query path", func() {
			mlxfwresetJSON := strings.TrimSpace(`
{
  "reset_needed": true
}
`)

			optCtx := &operations.Context{
				Status:                newTestStatus(),
				StateStore:            newTestStateStore(),
				RebootMethodDiscovery: true,
				CurrentBootID:         "boot-id",
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{{Netdev: "p0", PCIAddress: testPCIAddress0}}, nil
				},
			}
			h := &HandleReboot{
				skipBlock: true,
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					if strings.Contains(cmd, "mlxfwreset -d") && strings.Contains(cmd, "s --json") {
						var b bytes.Buffer
						_, _ = b.WriteString(mlxfwresetJSON)
						return b, bytes.Buffer{}, nil
					}
					if strings.HasSuffix(cmd, " query") {
						return bytes.Buffer{}, bytes.Buffer{}, nil
					}
					Expect(cmd).To(Equal(fmt.Sprintf("sleep %d && shutdown -h now", shutdownDelayInSeconds)))
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
			}
			Expect(h.Execute(context.Background(), optCtx)).To(Succeed())
			cond := meta.FindStatusCondition(optCtx.Status.GetLocal().Conditions, cutil.AgentCondRebootMethodDiscovery)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			Expect(cond.Reason).To(Equal(string(provisioningv1.RebootMethodSystemLevelReset)))
			Expect(cond.Message).To(Equal(mlxfwresetJSON))
		})

		It("RebootMethodDiscovery condition uses Reason=SystemLevelReset and Message=mlxfwreset JSON when reset_needed", func() {
			mlxfwresetJSON := strings.TrimSpace(`
{
  "reset_needed": true,
  "reasons": [
    "Pending FW update",
    "Pending NVCONFIG parameter change"
  ]
}
`)

			optCtx := &operations.Context{
				Status:                newTestStatus(),
				StateStore:            newTestStateStore(),
				RebootMethodDiscovery: true,
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{{Netdev: "p0", PCIAddress: testPCIAddress0}}, nil
				},
				CurrentBootID: "boot-id",
			}
			h := &HandleReboot{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var b bytes.Buffer
					_, _ = b.WriteString(mlxfwresetJSON)
					return b, bytes.Buffer{}, nil
				},
			}
			m, err := getRebootMethod(h, optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(*m).To(Equal(provisioningv1.RebootMethodSystemLevelReset))
			cond := meta.FindStatusCondition(optCtx.Status.GetLocal().Conditions, cutil.AgentCondRebootMethodDiscovery)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal(string(provisioningv1.RebootMethodSystemLevelReset)))
			Expect(cond.Message).To(Equal(mlxfwresetJSON))
		})

		It("getRebootMethod returns PowerCycle instead of SystemLevelReset on BlueField-4 with Astra enabled and records the reason", func() {
			mlxfwresetJSON := `{"reset_needed":true,"command_required":"Reboot external host is required"}`

			optCtx := &operations.Context{
				Status:                newTestStatus(),
				StateStore:            newTestStateStore(),
				RebootMethodDiscovery: true,
				CurrentBootID:         "boot-id",
				Options:               opts.Options{AstraEnabled: true},
				LatestDPU: &provisioningv1.DPU{
					Status: provisioningv1.DPUStatus{DPUType: provisioningv1.DPUTypeBlueField4},
				},
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{{Netdev: "p0", PCIAddress: testPCIAddress0, DeviceID: pciutil.BlueField4DeviceID}}, nil
				},
			}
			h := &HandleReboot{
				resolveTarget: func(p pciutil.NICPort) (string, error) { return p.PCIAddress, nil },
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var b bytes.Buffer
					if strings.HasSuffix(cmd, "status --json") {
						_, _ = b.WriteString(mlxfwresetJSON)
					}
					return b, bytes.Buffer{}, nil
				},
			}
			m, err := getRebootMethod(h, optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(m).NotTo(BeNil())
			Expect(*m).To(Equal(provisioningv1.RebootMethodPowerCycle))
			cond := meta.FindStatusCondition(optCtx.Status.GetLocal().Conditions, cutil.AgentCondRebootMethodDiscovery)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal(string(provisioningv1.RebootMethodPowerCycle)))
			Expect(cond.Message).To(HavePrefix(astraSystemLevelResetUnsupportedText))
			Expect(cond.Message).To(ContainSubstring(mlxfwresetJSON))
		})

		It("stores full mlxfwreset JSON in condition Message (device, FW versions, nvconfig, reasons)", func() {
			device := testPCIAddress0

			// Representative `mlxfwreset -d <pci> status --json` output (embedded fixture, same style as dpuflavor webhook YAML tests).
			mlxfwresetFullJSON := strings.TrimSpace(`
{
  "device": "0000:03:00.0",
  "pending_fw_version": "22.49.0264",
  "running_fw_version": "22.33.1048",
  "pending_nvconfig_parameters": [
    {
      "name": "ROCE_CONTROL",
      "default": "ROCE_ENABLE(2)",
      "current": "DEVICE_DEFAULT(0)",
      "next_boot": "ROCE_DISABLE(1)"
    }
  ],
  "reset_needed": true,
  "description_action": "Driver restart and PCI reset (Level 3)",
  "command_required": "mlxfwreset -d 0000:03:00.0 reset --level 3 --type 0 --sync 0 --method 0",
  "reasons": [
    "Pending FW update",
    "Pending NVCONFIG parameter change"
  ]
}
`)

			optCtx := &operations.Context{
				Status:                newTestStatus(),
				StateStore:            newTestStateStore(),
				RebootMethodDiscovery: true,
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{{Netdev: "p0", PCIAddress: device}}, nil
				},
				CurrentBootID: "boot-id",
				LatestDPU: &provisioningv1.DPU{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							cutil.AgentAnnotationAllowFirmwareResetReboot: "true",
						},
					},
				},
			}
			h := &HandleReboot{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					if strings.HasSuffix(cmd, " query") {
						return bytes.Buffer{}, bytes.Buffer{}, nil
					}
					Expect(cmd).To(Equal(fmt.Sprintf("mlxfwreset -d %s status --json", device)))
					var b bytes.Buffer
					_, _ = b.WriteString(mlxfwresetFullJSON)
					return b, bytes.Buffer{}, nil
				},
			}
			m, err := getRebootMethod(h, optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(*m).To(Equal(provisioningv1.RebootMethodFirmwareReset))
			cond := meta.FindStatusCondition(optCtx.Status.GetLocal().Conditions, cutil.AgentCondRebootMethodDiscovery)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal(string(provisioningv1.RebootMethodFirmwareReset)))
			Expect(cond.Message).To(Equal(mlxfwresetFullJSON))
		})

		It("getRebootMethod maps firmware-style command_required to SystemLevelReset when allow-firmware-reset-reboot annotation is absent", func() {
			mlxfwresetFullJSON := strings.TrimSpace(`
{
  "reset_needed": true,
  "command_required": "mlxfwreset -d 0000:03:00.0 reset --level 3 --type 0 --sync 0 --method 0"
}
`)
			optCtx := &operations.Context{
				Status:                newTestStatus(),
				StateStore:            newTestStateStore(),
				RebootMethodDiscovery: true,
				CurrentBootID:         "boot-id",
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{{Netdev: "p0", PCIAddress: testPCIAddress0}}, nil
				},
			}
			h := &HandleReboot{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var b bytes.Buffer
					_, _ = b.WriteString(mlxfwresetFullJSON)
					return b, bytes.Buffer{}, nil
				},
			}
			m, err := getRebootMethod(h, optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(*m).To(Equal(provisioningv1.RebootMethodSystemLevelReset))
		})

		It("getRebootMethod returns PowerCycle when pending_nvconfig includes a power-cycle-only parameter", func() {
			mlxfwresetJSON := strings.TrimSpace(`
{
  "reset_needed": true,
  "pending_nvconfig_parameters": [
    {"name": "INTERNAL_CPU_MODEL", "current": "x", "next_boot": "y"}
  ],
  "command_required": "mlxfwreset -d 0000:03:00.0 reset --level 3"
}
`)
			optCtx := &operations.Context{
				Status:                newTestStatus(),
				StateStore:            newTestStateStore(),
				RebootMethodDiscovery: true,
				CurrentBootID:         "boot-id",
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{{Netdev: "p0", PCIAddress: testPCIAddress0}}, nil
				},
			}
			h := &HandleReboot{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var b bytes.Buffer
					_, _ = b.WriteString(mlxfwresetJSON)
					return b, bytes.Buffer{}, nil
				},
			}
			m, err := getRebootMethod(h, optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(*m).To(Equal(provisioningv1.RebootMethodPowerCycle))
			cond := meta.FindStatusCondition(optCtx.Status.GetLocal().Conditions, cutil.AgentCondRebootMethodDiscovery)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal(string(provisioningv1.RebootMethodPowerCycle)))
		})

		It("getRebootMethod returns PowerCycle when command_required contains power cycle (case-insensitive substring)", func() {
			mlxfwresetJSON := strings.TrimSpace(`
{
  "reset_needed": true,
  "pending_nvconfig_parameters": "N/A (No pending NVCONFIG parameters)",
  "command_required": "Host POWER CYCLE is required before applying configuration"
}
`)
			optCtx := &operations.Context{
				Status:                newTestStatus(),
				StateStore:            newTestStateStore(),
				RebootMethodDiscovery: true,
				CurrentBootID:         "boot-id",
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{{Netdev: "p0", PCIAddress: testPCIAddress0}}, nil
				},
			}
			h := &HandleReboot{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var b bytes.Buffer
					_, _ = b.WriteString(mlxfwresetJSON)
					return b, bytes.Buffer{}, nil
				},
			}
			m, err := getRebootMethod(h, optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(*m).To(Equal(provisioningv1.RebootMethodPowerCycle))
			cond := meta.FindStatusCondition(optCtx.Status.GetLocal().Conditions, cutil.AgentCondRebootMethodDiscovery)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal(string(provisioningv1.RebootMethodPowerCycle)))
		})

		It("getRebootMethod returns PowerCycle when description_action reports power cycle and command_required is absent", func() {
			mlxfwresetJSON := strings.TrimSpace(`
{
  "reset_needed": true,
  "pending_nvconfig_parameters": "N/A (No pending NVCONFIG parameters)",
  "description_action": "Full power cycle is required",
  "reasons": ["Pending FW update"]
}
`)
			optCtx := &operations.Context{
				Status:                newTestStatus(),
				StateStore:            newTestStateStore(),
				RebootMethodDiscovery: true,
				CurrentBootID:         "boot-id",
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{{Netdev: "p0", PCIAddress: testPCIAddress0}}, nil
				},
			}
			h := &HandleReboot{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var b bytes.Buffer
					_, _ = b.WriteString(mlxfwresetJSON)
					return b, bytes.Buffer{}, nil
				},
			}
			m, err := getRebootMethod(h, optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(*m).To(Equal(provisioningv1.RebootMethodPowerCycle))
			cond := meta.FindStatusCondition(optCtx.Status.GetLocal().Conditions, cutil.AgentCondRebootMethodDiscovery)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal(string(provisioningv1.RebootMethodPowerCycle)))
		})

		It("getRebootMethod prefers PowerCycle from description_action over SystemLevelReset from command_required", func() {
			mlxfwresetJSON := strings.TrimSpace(`
{
  "reset_needed": true,
  "pending_nvconfig_parameters": "N/A (No pending NVCONFIG parameters)",
  "description_action": "Full power cycle is required",
  "command_required": "Reboot external host is required"
}
`)
			optCtx := &operations.Context{
				Status:                newTestStatus(),
				StateStore:            newTestStateStore(),
				RebootMethodDiscovery: true,
				CurrentBootID:         "boot-id",
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{{Netdev: "p0", PCIAddress: testPCIAddress0}}, nil
				},
			}
			h := &HandleReboot{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var b bytes.Buffer
					_, _ = b.WriteString(mlxfwresetJSON)
					return b, bytes.Buffer{}, nil
				},
			}
			m, err := getRebootMethod(h, optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(*m).To(Equal(provisioningv1.RebootMethodPowerCycle))
			cond := meta.FindStatusCondition(optCtx.Status.GetLocal().Conditions, cutil.AgentCondRebootMethodDiscovery)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal(string(provisioningv1.RebootMethodPowerCycle)))
		})

		It("getRebootMethod returns PowerCycle when mlxfwreset query reports no supported reset level", func() {
			// MFT 4.36/4.37 shape: the required power cycle is dropped and command_required
			// carries the external host reboot message instead.
			mlxfwresetJSON := strings.TrimSpace(`
{
  "reset_needed": true,
  "pending_nvconfig_parameters": "N/A (No pending NVCONFIG parameters)",
  "command_required": "Reboot external host is required",
  "reasons": ["PCI rescan is required"]
}
`)
			optCtx := &operations.Context{
				Status:                newTestStatus(),
				StateStore:            newTestStateStore(),
				RebootMethodDiscovery: true,
				CurrentBootID:         "boot-id",
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{{Netdev: "p0", PCIAddress: testPCIAddress0}}, nil
				},
			}
			h := &HandleReboot{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var b bytes.Buffer
					if strings.HasSuffix(cmd, " query") {
						_, _ = b.WriteString("-E- " + noResetLevelSupportedText1 + ".")
						return bytes.Buffer{}, b, fmt.Errorf("exit status 1")
					}
					_, _ = b.WriteString(mlxfwresetJSON)
					return b, bytes.Buffer{}, nil
				},
			}
			m, err := getRebootMethod(h, optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(*m).To(Equal(provisioningv1.RebootMethodPowerCycle))
			cond := meta.FindStatusCondition(optCtx.Status.GetLocal().Conditions, cutil.AgentCondRebootMethodDiscovery)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal(string(provisioningv1.RebootMethodPowerCycle)))
		})

		It("getRebootMethod returns SystemLevelReset when command_required is external host reboot message", func() {
			mlxfwresetJSON := strings.TrimSpace(`
{
  "reset_needed": true,
  "pending_nvconfig_parameters": "N/A (No pending NVCONFIG parameters)",
  "command_required": "Reboot external host is required"
}
`)
			optCtx := &operations.Context{
				Status:                newTestStatus(),
				StateStore:            newTestStateStore(),
				RebootMethodDiscovery: true,
				CurrentBootID:         "boot-id",
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{{Netdev: "p0", PCIAddress: testPCIAddress0}}, nil
				},
			}
			h := &HandleReboot{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var b bytes.Buffer
					_, _ = b.WriteString(mlxfwresetJSON)
					return b, bytes.Buffer{}, nil
				},
			}
			m, err := getRebootMethod(h, optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(*m).To(Equal(provisioningv1.RebootMethodSystemLevelReset))
			cond := meta.FindStatusCondition(optCtx.Status.GetLocal().Conditions, cutil.AgentCondRebootMethodDiscovery)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal(string(provisioningv1.RebootMethodSystemLevelReset)))
		})

		It("getRebootMethod matches external host message with trim and case-insensitive command_required", func() {
			mlxfwresetJSON := `{"reset_needed":true,"command_required":"  reboot EXTERNAL host is required  "}`
			optCtx := &operations.Context{
				Status:                newTestStatus(),
				StateStore:            newTestStateStore(),
				RebootMethodDiscovery: true,
				CurrentBootID:         "boot-id",
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{{Netdev: "p0", PCIAddress: testPCIAddress0}}, nil
				},
			}
			h := &HandleReboot{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var b bytes.Buffer
					_, _ = b.WriteString(mlxfwresetJSON)
					return b, bytes.Buffer{}, nil
				},
			}
			m, err := getRebootMethod(h, optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(*m).To(Equal(provisioningv1.RebootMethodSystemLevelReset))
		})

		It("merges multiple PCI devices: PowerCycle wins over FirmwareReset", func() {
			devA := testPCIAddress0
			devB := testPCIAddress1

			jsonFR := `{"reset_needed":true,"command_required":"mlxfwreset -d 0000:03:00.0 reset --level 3"}`
			jsonPC := `{"reset_needed":true,"pending_nvconfig_parameters":[{"name":"INTERNAL_CPU_MODEL"}]}`
			optCtx := &operations.Context{
				Status:                newTestStatus(),
				StateStore:            newTestStateStore(),
				RebootMethodDiscovery: true,
				CurrentBootID:         "boot-id",
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{
						{Netdev: "p0", PCIAddress: devA},
						{Netdev: "p1", PCIAddress: devB},
					}, nil
				},
			}
			h := &HandleReboot{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var b bytes.Buffer
					switch {
					case strings.Contains(cmd, devA):
						_, _ = b.WriteString(jsonFR)
					case strings.Contains(cmd, devB):
						_, _ = b.WriteString(jsonPC)
					default:
						Fail("unexpected cmd: " + cmd)
					}
					return b, bytes.Buffer{}, nil
				},
			}
			m, err := getRebootMethod(h, optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(*m).To(Equal(provisioningv1.RebootMethodPowerCycle))
			Expect(h.perDeviceFirmwareResetCmds).To(BeNil())
			cond := meta.FindStatusCondition(optCtx.Status.GetLocal().Conditions, cutil.AgentCondRebootMethodDiscovery)
			Expect(cond.Message).To(Equal(jsonFR + "\n---\n" + jsonPC))
		})

		It("merges multiple PCI devices: SystemLevelReset wins over FirmwareReset", func() {
			devA := testPCIAddress0
			devB := testPCIAddress1

			jsonFR := `{"reset_needed":true,"command_required":"mlxfwreset -d 0000:03:00.0 reset --level 3"}`
			jsonSLR := `{"reset_needed":true,"command_required":"Reboot external host is required"}`
			optCtx := &operations.Context{
				Status:                newTestStatus(),
				StateStore:            newTestStateStore(),
				RebootMethodDiscovery: true,
				CurrentBootID:         "boot-id",
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{
						{Netdev: "p0", PCIAddress: devA},
						{Netdev: "p1", PCIAddress: devB},
					}, nil
				},
			}
			h := &HandleReboot{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var b bytes.Buffer
					switch {
					case strings.Contains(cmd, devA):
						_, _ = b.WriteString(jsonFR)
					case strings.Contains(cmd, devB):
						_, _ = b.WriteString(jsonSLR)
					default:
						Fail("unexpected cmd: " + cmd)
					}
					return b, bytes.Buffer{}, nil
				},
			}
			m, err := getRebootMethod(h, optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(*m).To(Equal(provisioningv1.RebootMethodSystemLevelReset))
			Expect(h.perDeviceFirmwareResetCmds).To(BeNil())
			cond := meta.FindStatusCondition(optCtx.Status.GetLocal().Conditions, cutil.AgentCondRebootMethodDiscovery)
			Expect(cond.Message).To(Equal(jsonFR + "\n---\n" + jsonSLR))
		})

		It("joins mlxfwreset JSON from each reset-needed device in discovery condition Message", func() {
			devA := testPCIAddress0
			devB := testPCIAddress1
			jsonA := `{"reset_needed":true,"tag":"a"}`
			jsonB := `{"reset_needed":true,"tag":"b"}`
			optCtx := &operations.Context{
				Status:                newTestStatus(),
				StateStore:            newTestStateStore(),
				RebootMethodDiscovery: true,
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{
						{Netdev: "p0", PCIAddress: devA},
						{Netdev: "p1", PCIAddress: devB},
					}, nil
				},
			}
			h := &HandleReboot{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var b bytes.Buffer
					if strings.Contains(cmd, devA) {
						_, _ = b.WriteString(jsonA)
					} else {
						_, _ = b.WriteString(jsonB)
					}
					return b, bytes.Buffer{}, nil
				},
			}
			_, err := getRebootMethod(h, optCtx)
			Expect(err).NotTo(HaveOccurred())
			cond := meta.FindStatusCondition(optCtx.Status.GetLocal().Conditions, cutil.AgentCondRebootMethodDiscovery)
			Expect(cond.Message).To(Equal(jsonA + "\n---\n" + jsonB))
		})

		It("populates per-device firmware reset commands when merged method is FirmwareReset", func() {
			devA := testPCIAddress0
			devB := testPCIAddress1
			cmdA := "mlxfwreset -d 0000:03:00.0 reset --level 3 --type 0"
			cmdB := "mlxfwreset -d 0000:03:00.1 reset --level 1"
			jsonA := `{"reset_needed":true,"command_required":"` + cmdA + `"}`
			jsonB := `{"reset_needed":true,"command_required":"` + cmdB + `"}`
			optCtx := &operations.Context{
				Status:                newTestStatus(),
				StateStore:            newTestStateStore(),
				RebootMethodDiscovery: true,
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{
						{Netdev: "p0", PCIAddress: devA},
						{Netdev: "p1", PCIAddress: devB},
					}, nil
				},
				CurrentBootID: "boot-id",
				LatestDPU: &provisioningv1.DPU{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							cutil.AgentAnnotationAllowFirmwareResetReboot: "true",
						},
					},
				},
			}
			h := &HandleReboot{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var b bytes.Buffer
					if strings.Contains(cmd, devA) {
						_, _ = b.WriteString(jsonA)
					} else {
						_, _ = b.WriteString(jsonB)
					}
					return b, bytes.Buffer{}, nil
				},
			}
			m, err := getRebootMethod(h, optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(*m).To(Equal(provisioningv1.RebootMethodFirmwareReset))
			Expect(h.perDeviceFirmwareResetCmds).To(Equal([]firmwareResetPerDevice{
				{Device: devA, Cmd: cmdA},
				{Device: devB, Cmd: cmdB},
			}))
		})

		It("uses the fwctl device as mlxfwreset -d target on BF4 while status and condition keep the PCI address", func() {
			const target = "/dev/fwctl/fwctl0"
			device := testPCIAddress0
			resetCmd := "mlxfwreset -d " + target + " reset --level 3 --type 0"
			mlxfwresetJSON := `{"reset_needed":true,"pending_nvconfig_parameters":[{"name":"PARAM_A","current":"0","next_boot":"1"}],"command_required":"` + resetCmd + `"}`
			optCtx := &operations.Context{
				Status:                newTestStatus(),
				StateStore:            newTestStateStore(),
				RebootMethodDiscovery: true,
				CurrentBootID:         "boot-id",
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{{Netdev: "p0", PCIAddress: device, DeviceID: pciutil.BlueField4DeviceID}}, nil
				},
				LatestDPU: &provisioningv1.DPU{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							cutil.AgentAnnotationAllowFirmwareResetReboot: "true",
						},
					},
				},
			}
			var resolvedPorts []pciutil.NICPort
			var cmds []string
			h := &HandleReboot{
				resolveTarget: func(port pciutil.NICPort) (string, error) {
					resolvedPorts = append(resolvedPorts, port)
					return target, nil
				},
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					cmds = append(cmds, cmd)
					var b bytes.Buffer
					_, _ = b.WriteString(mlxfwresetJSON)
					return b, bytes.Buffer{}, nil
				},
			}
			m, err := getRebootMethod(h, optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(*m).To(Equal(provisioningv1.RebootMethodFirmwareReset))
			Expect(resolvedPorts).To(Equal([]pciutil.NICPort{{Netdev: "p0", PCIAddress: device, DeviceID: pciutil.BlueField4DeviceID}}))
			// Every mlxfwreset invocation (status and the no-reset-level query workaround) targets
			// the fwctl device, never the PCI address.
			Expect(cmds).To(Equal([]string{
				"mlxfwreset -d " + target + " status --json",
				"mlxfwreset -d " + target + " query",
			}))
			Expect(h.perDeviceFirmwareResetCmds).To(Equal([]firmwareResetPerDevice{{Device: target, Cmd: resetCmd}}))
			// The pending observation and condition keep identifying the port by PCI address.
			latest := savedState(optCtx).Pending.Latest
			Expect(latest).NotTo(BeNil())
			Expect(latest.Devices).To(Equal([]pendingDevice{{
				Device:  device,
				Entries: []pendingEntry{{Name: "PARAM_A", Current: "0", NextBoot: "1"}},
			}}))
			cond := meta.FindStatusCondition(optCtx.Status.GetLocal().Conditions, cutil.AgentCondRebootMethodDiscovery)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Reason).To(Equal(string(provisioningv1.RebootMethodFirmwareReset)))
			Expect(cond.Message).To(Equal(mlxfwresetJSON))
		})

		It("runs mlxfwreset query against the fwctl device on BF4 when checking for no supported reset level", func() {
			const target = "/dev/fwctl/fwctl0"
			optCtx := &operations.Context{
				Status:                newTestStatus(),
				StateStore:            newTestStateStore(),
				RebootMethodDiscovery: true,
				CurrentBootID:         "boot-id",
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{{Netdev: "p0", PCIAddress: testPCIAddress0, DeviceID: pciutil.BlueField4DeviceID}}, nil
				},
			}
			var cmds []string
			h := &HandleReboot{
				resolveTarget: func(pciutil.NICPort) (string, error) { return target, nil },
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					cmds = append(cmds, cmd)
					var b bytes.Buffer
					if strings.HasSuffix(cmd, " query") {
						_, _ = b.WriteString(noResetLevelSupportedText)
						return b, bytes.Buffer{}, nil
					}
					_, _ = b.WriteString(`{"reset_needed":true,"command_required":"mlxfwreset -d x reset --level 3"}`)
					return b, bytes.Buffer{}, nil
				},
			}
			m, err := getRebootMethod(h, optCtx)
			Expect(err).NotTo(HaveOccurred())
			Expect(*m).To(Equal(provisioningv1.RebootMethodPowerCycle))
			Expect(cmds).To(Equal([]string{
				"mlxfwreset -d " + target + " status --json",
				"mlxfwreset -d " + target + " query",
			}))
		})

		It("getRebootMethod returns an error naming the PCI device and runs no mlxfwreset when the fwctl device cannot be resolved", func() {
			optCtx := &operations.Context{
				Status:                newTestStatus(),
				StateStore:            newTestStateStore(),
				RebootMethodDiscovery: true,
				CurrentBootID:         "boot-id",
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{{Netdev: "p0", PCIAddress: testPCIAddress0, DeviceID: pciutil.BlueField4DeviceID}}, nil
				},
			}
			h := &HandleReboot{
				resolveTarget: func(port pciutil.NICPort) (string, error) {
					return "", fmt.Errorf("no fwctl device found for PCI %s", port.PCIAddress)
				},
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					Fail("mlxfwreset must not run when the fwctl device cannot be resolved: " + cmd)
					return bytes.Buffer{}, bytes.Buffer{}, nil
				},
			}
			_, err := getRebootMethod(h, optCtx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("resolve mlxfwreset device for PCI device " + testPCIAddress0))
			Expect(err.Error()).To(ContainSubstring("no fwctl device found"))
			expectNoSavedState(optCtx)
		})

		It("getRebootMethod returns error when mlxfwreset JSON is invalid", func() {
			optCtx := &operations.Context{
				Status:                newTestStatus(),
				StateStore:            newTestStateStore(),
				RebootMethodDiscovery: true,
				CurrentBootID:         "boot-id",
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{{Netdev: "p0", PCIAddress: testPCIAddress0}}, nil
				},
			}
			h := &HandleReboot{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var b bytes.Buffer
					_, _ = b.WriteString(`not json`)
					return b, bytes.Buffer{}, nil
				},
			}
			_, err := getRebootMethod(h, optCtx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("parse JSON:"))
		})

		It("getRebootMethod surfaces mlxfwreset stdout when the command fails", func() {
			// Use a diagnostic that is not the hostless-only noResetLevelSupportedText workaround.
			const mftDiagnostic = "-E- Failed to open device"
			optCtx := &operations.Context{
				Status:                newTestStatus(),
				StateStore:            newTestStateStore(),
				RebootMethodDiscovery: true,
				CurrentBootID:         "boot-id",
				DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
					return []pciutil.NICPort{{Netdev: "p0", PCIAddress: testPCIAddress0}}, nil
				},
			}
			h := &HandleReboot{
				runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
					var stdout bytes.Buffer
					_, _ = stdout.WriteString(mftDiagnostic)
					return stdout, bytes.Buffer{}, fmt.Errorf("exit status 1")
				},
			}
			_, err := getRebootMethod(h, optCtx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(mftDiagnostic))
			Expect(err.Error()).To(ContainSubstring("stdout:"))
		})
	})

	Describe("device-query reboot method helpers", func() {
		DescribeTable("rebootMethodTakesPrecedenceOver",
			func(a, b provisioningv1.RebootMethodType, want bool) {
				Expect(rebootMethodTakesPrecedenceOver(a, b)).To(Equal(want))
			},
			Entry("PowerCycle beats FirmwareReset",
				provisioningv1.RebootMethodPowerCycle, provisioningv1.RebootMethodFirmwareReset, true),
			Entry("FirmwareReset does not beat PowerCycle",
				provisioningv1.RebootMethodFirmwareReset, provisioningv1.RebootMethodPowerCycle, false),
			Entry("PowerCycle beats SystemLevelReset",
				provisioningv1.RebootMethodPowerCycle, provisioningv1.RebootMethodSystemLevelReset, true),
			Entry("SystemLevelReset beats FirmwareReset",
				provisioningv1.RebootMethodSystemLevelReset, provisioningv1.RebootMethodFirmwareReset, true),
			Entry("FirmwareReset does not beat SystemLevelReset",
				provisioningv1.RebootMethodFirmwareReset, provisioningv1.RebootMethodSystemLevelReset, false),
			Entry("SystemReboot beats FirmwareReset",
				provisioningv1.RebootMethodSystemReboot, provisioningv1.RebootMethodFirmwareReset, true),
			Entry("FirmwareReset does not beat SystemReboot",
				provisioningv1.RebootMethodFirmwareReset, provisioningv1.RebootMethodSystemReboot, false),
			Entry("same method — no replacement",
				provisioningv1.RebootMethodFirmwareReset, provisioningv1.RebootMethodFirmwareReset, false),
			Entry("FirmwareReset beats NoAction",
				provisioningv1.RebootMethodFirmwareReset, provisioningv1.RebootMethodNoAction, true),
		)

		It("rebootMethodFromMlxfwresetStatus follows PowerCycle > SystemLevelReset > FirmwareReset > SystemLevelReset fallback", func() {
			dev := testPCIAddress0
			h := &HandleReboot{}

			var outPC mlxfwresetStatusJSON
			Expect(json.Unmarshal([]byte(`{"reset_needed":true,"pending_nvconfig_parameters":[{"name":"INTERNAL_CPU_MODEL"}],"command_required":"mlxfwreset -d x reset"}`), &outPC)).To(Succeed())
			Expect(rebootMethodFromMlxfwresetStatus(h, dev, &outPC)).To(Equal(provisioningv1.RebootMethodPowerCycle))

			var outPCCmd mlxfwresetStatusJSON
			Expect(json.Unmarshal([]byte(`{"reset_needed":true,"command_required":"Please perform a power cycle on the host"}`), &outPCCmd)).To(Succeed())
			Expect(rebootMethodFromMlxfwresetStatus(h, dev, &outPCCmd)).To(Equal(provisioningv1.RebootMethodPowerCycle))

			var outSLR mlxfwresetStatusJSON
			Expect(json.Unmarshal([]byte(`{"reset_needed":true,"command_required":"Reboot external host is required"}`), &outSLR)).To(Succeed())
			Expect(rebootMethodFromMlxfwresetStatus(h, dev, &outSLR)).To(Equal(provisioningv1.RebootMethodSystemLevelReset))

			var outFR mlxfwresetStatusJSON
			Expect(json.Unmarshal([]byte(`{"reset_needed":true,"command_required":"mlxfwreset -d x reset --level 3"}`), &outFR)).To(Succeed())
			Expect(rebootMethodFromMlxfwresetStatus(h, dev, &outFR)).To(Equal(provisioningv1.RebootMethodSystemLevelReset))
			h.allowFirmwareReset = true
			Expect(rebootMethodFromMlxfwresetStatus(h, dev, &outFR)).To(Equal(provisioningv1.RebootMethodFirmwareReset))

			var outFallback mlxfwresetStatusJSON
			Expect(json.Unmarshal([]byte(`{"reset_needed":true}`), &outFallback)).To(Succeed())
			Expect(rebootMethodFromMlxfwresetStatus(nil, dev, &outFallback)).To(Equal(provisioningv1.RebootMethodSystemLevelReset))
		})
	})

	// exec* tests call HandleReboot helpers directly; they are not tied to RebootMethodDiscovery or Execute().
	Describe("HandleReboot exec helpers", func() {
		Context("execPowerCycle", func() {
			It("sets RebootMethod PowerCycle and updates status before blocking", func() {
				statusClient := newTestStatusClient()
				optCtx := &operations.Context{Status: newTestStatusFor(statusClient)}
				h := &HandleReboot{skipBlock: true}
				Expect(h.execPowerCycle(optCtx)).To(Succeed())
				Expect(pushedAgentStatus(statusClient).RebootMethod).To(Equal(ptr.To(provisioningv1.RebootMethodPowerCycle)))
				Expect(optCtx.Status.GetLocal().RebootMethod).NotTo(BeNil())
				Expect(*optCtx.Status.GetLocal().RebootMethod).To(Equal(provisioningv1.RebootMethodPowerCycle))
			})
		})

		Context("execSystemReboot", func() {
			It("sets RebootMethod SystemReboot", func() {
				optCtx := &operations.Context{Status: newTestStatus()}
				h := &HandleReboot{}
				Expect(h.execSystemReboot(optCtx)).To(Succeed())
				Expect(optCtx.Status.GetLocal().RebootMethod).NotTo(BeNil())
				Expect(*optCtx.Status.GetLocal().RebootMethod).To(Equal(provisioningv1.RebootMethodSystemReboot))
			})
		})

		Context("execSystemLevelReset", func() {
			It("sets status and runs shutdown command", func() {
				optCtx := &operations.Context{Status: newTestStatus()}
				var shutdownCmd string
				h := &HandleReboot{
					skipBlock: true,
					runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
						shutdownCmd = cmd
						return bytes.Buffer{}, bytes.Buffer{}, nil
					},
				}
				Expect(h.execSystemLevelReset(optCtx)).To(Succeed())
				Expect(optCtx.Status.GetLocal().RebootMethod).NotTo(BeNil())
				Expect(*optCtx.Status.GetLocal().RebootMethod).To(Equal(provisioningv1.RebootMethodSystemLevelReset))
				Expect(shutdownCmd).To(Equal(fmt.Sprintf("sleep %d && shutdown -h now", shutdownDelayInSeconds)))
			})
		})

		Context("execFirmwareReset", func() {
			It("sets status and runs mlxfwreset reset for each PCI device", func() {
				device := testPCIAddress0
				cmd := fmt.Sprintf("mlxfwreset -d %s -y reset", device)
				optCtx := &operations.Context{Status: newTestStatus()}
				var fwResetCmds []string
				h := &HandleReboot{
					skipBlock: true,
					perDeviceFirmwareResetCmds: []firmwareResetPerDevice{
						{Device: device, Cmd: cmd},
					},
					runBash: func(c string) (bytes.Buffer, bytes.Buffer, error) {
						fwResetCmds = append(fwResetCmds, c)
						return bytes.Buffer{}, bytes.Buffer{}, nil
					},
				}
				Expect(h.execFirmwareReset(optCtx)).To(Succeed())
				Expect(optCtx.Status.GetLocal().RebootMethod).NotTo(BeNil())
				Expect(*optCtx.Status.GetLocal().RebootMethod).To(Equal(provisioningv1.RebootMethodFirmwareReset))
				Expect(fwResetCmds).To(Equal([]string{cmd}))
			})

			It("runs discovered per-device firmware reset commands without listing devices", func() {
				optCtx := &operations.Context{Status: newTestStatus()}
				var ran []string
				h := &HandleReboot{
					skipBlock: true,
					perDeviceFirmwareResetCmds: []firmwareResetPerDevice{
						{Device: testPCIAddress0, Cmd: "custom-a"},
						{Device: testPCIAddress1, Cmd: "custom-b"},
					},
					runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
						ran = append(ran, cmd)
						return bytes.Buffer{}, bytes.Buffer{}, nil
					},
				}
				Expect(h.execFirmwareReset(optCtx)).To(Succeed())
				Expect(ran).To(Equal([]string{"custom-a", "custom-b"}))
				Expect(h.perDeviceFirmwareResetCmds).To(BeNil())
			})

			It("runs mlxfwreset reset for every planned device command", func() {
				devices := []string{testPCIAddress0, testPCIAddress1}
				optCtx := &operations.Context{Status: newTestStatus()}
				var ran []string
				h := &HandleReboot{
					skipBlock: true,
					perDeviceFirmwareResetCmds: []firmwareResetPerDevice{
						{Device: devices[0], Cmd: fmt.Sprintf("mlxfwreset -d %s -y reset", devices[0])},
						{Device: devices[1], Cmd: fmt.Sprintf("mlxfwreset -d %s -y reset", devices[1])},
					},
					runBash: func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
						ran = append(ran, cmd)
						return bytes.Buffer{}, bytes.Buffer{}, nil
					},
				}
				Expect(h.execFirmwareReset(optCtx)).To(Succeed())
				Expect(ran).To(HaveLen(2))
				Expect(ran).To(Equal([]string{
					fmt.Sprintf("mlxfwreset -d %s -y reset", devices[0]),
					fmt.Sprintf("mlxfwreset -d %s -y reset", devices[1]),
				}))
			})

			It("fails when per-device firmware reset commands are empty", func() {
				optCtx := &operations.Context{Status: newTestStatus()}
				h := &HandleReboot{skipBlock: true}
				execErr := h.execFirmwareReset(optCtx)
				Expect(execErr).To(HaveOccurred())
				Expect(execErr.Error()).To(ContainSubstring("per-device firmware reset commands are empty"))
			})
		})
	})
})

var _ = Describe("MFT tool version parsing and resolveRebootMethodDiscovery", func() {
	It("parseMlxfwresetVersionOutput skips prefix before mft then parses like mlxconfig", func() {
		out := "mlxfwreset 1.0.0, mft 4.36.0-93, Git SHA Hash: 2565c9618"
		version, err := parseMlxfwresetVersionOutput(out)
		Expect(err).NotTo(HaveOccurred())
		Expect(version.Equal(semver.MustParse("4.36.0-93"))).To(BeTrue())
	})

	It("parseMlxfwresetVersionOutput fails when version cannot be extracted", func() {
		_, err := parseMlxfwresetVersionOutput("no version here")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to extract mlxfwreset MFT version"))
	})

	It("parseMlxconfigVersionOutput extracts version like former nvconfig mlxconfigVersion", func() {
		testCases := []struct {
			output          string
			expectedVersion *semver.Version
		}{
			{
				output:          "mlxconfig, mft 4.30.1-8, built on Nov 28 2024",
				expectedVersion: semver.MustParse("4.30.1-8"),
			},
			{
				output:          "mlxconfig 4.29.0",
				expectedVersion: semver.MustParse("4.29.0"),
			},
			{
				output:          "mlxconfig, mft 4.36.0-86. Git SHA Hash: e44fa1501",
				expectedVersion: semver.MustParse("4.36.0-86"),
			},
		}
		for _, tc := range testCases {
			version, err := parseMlxconfigVersionOutput(tc.output)
			Expect(err).NotTo(HaveOccurred())
			Expect(version.Equal(tc.expectedVersion)).To(BeTrue(), "output %q => got %s, expected %s", tc.output, version.String(), tc.expectedVersion.String())
		}
	})

	It("parseMlxconfigVersionOutput fails when version cannot be extracted", func() {
		_, err := parseMlxconfigVersionOutput("no version here")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to extract version from mlxconfig output"))
	})

	It("parseMlxconfigVersionOutput fails for short form (e.g. 4.30)", func() {
		_, err := parseMlxconfigVersionOutput("mlxconfig 4.30")
		Expect(err).To(HaveOccurred())
	})

	It("resolveRebootMethodDiscovery returns true when both tools meet minimum", func() {
		run := func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
			var b bytes.Buffer
			switch {
			case strings.HasPrefix(cmd, "mlxfwreset"):
				b.WriteString("mlxfwreset 1.0.0, mft 4.36.0-95, Git SHA Hash: abc\n")
			case strings.HasPrefix(cmd, "mlxconfig"):
				b.WriteString("mlxconfig, mft 4.36.0-95. Git SHA Hash: abc\n")
			}
			return b, bytes.Buffer{}, nil
		}
		Expect(resolveRebootMethodDiscovery(run)).To(BeTrue())
	})

	It("resolveRebootMethodDiscovery returns false when mlxfwreset is below minimum", func() {
		run := func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
			var b bytes.Buffer
			if strings.HasPrefix(cmd, "mlxfwreset") {
				b.WriteString("mlxfwreset 1.0.0, mft 4.36.0-86, Git SHA Hash: x\n")
				return b, bytes.Buffer{}, nil
			}
			b.WriteString("mlxconfig, mft 4.36.0-95\n")
			return b, bytes.Buffer{}, nil
		}
		Expect(resolveRebootMethodDiscovery(run)).To(BeFalse())
	})

	It("resolveRebootMethodDiscovery returns false when mlxconfig is below minimum", func() {
		run := func(cmd string) (bytes.Buffer, bytes.Buffer, error) {
			var b bytes.Buffer
			if strings.HasPrefix(cmd, "mlxfwreset") {
				b.WriteString("mlxfwreset 1.0.0, mft 4.36.0-95, Git SHA Hash: x\n")
				return b, bytes.Buffer{}, nil
			}
			b.WriteString("mlxconfig, mft 4.36.0-86\n")
			return b, bytes.Buffer{}, nil
		}
		Expect(resolveRebootMethodDiscovery(run)).To(BeFalse())
	})

	It("semver prerelease ordering for min version 4.36.0-95", func() {
		minVer := semver.MustParse(MinRebootDiscoveryMFTVersion)
		v94 := semver.MustParse("4.36.0-94")
		v95 := semver.MustParse("4.36.0-95")
		Expect(mftVersionMeetsMinimum(v94, minVer)).To(BeFalse())
		Expect(mftVersionMeetsMinimum(v95, minVer)).To(BeTrue())
	})
})
