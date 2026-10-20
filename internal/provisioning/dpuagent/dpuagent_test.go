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

package dpuagent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/ovsscript"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/statusmanager"
	dpuutil "github.com/nvidia/doca-platform/internal/provisioning/dpuagent/util"
	pciutil "github.com/nvidia/doca-platform/internal/provisioning/utils/pci"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const testRetryInterval = 1 * time.Millisecond

func newTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(provisioningv1.AddToScheme(s))
	return s
}

func newTestDPU() *provisioningv1.DPU {
	return &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-dpu",
			Namespace: "test-ns",
			UID:       "test-uid",
		},
		Spec: provisioningv1.DPUSpec{
			DPUNodeName:   "node-1",
			DPUDeviceName: "dev-1",
			BFB:           ptr.To("bfb-1"),
			SerialNumber:  "sn-1",
			DPUFlavor:     "flavor-1",
			NodeEffect:    provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
		},
	}
}

func newTestOptCtx(fakeClient client.Client) *operations.Context {
	return &operations.Context{
		Client: fakeClient,
		Options: opts.Options{
			DPUName:      "test-dpu",
			DPUNamespace: "test-ns",
			DPUUID:       "test-uid",
		},
		Status: statusmanager.New(fakeClient, "test-ns", "test-dpu", "test-uid"),
		DiscoverPorts: func(_ pciutil.PortScope) ([]pciutil.NICPort, error) {
			return []pciutil.NICPort{
				{Netdev: "p0", PCIAddress: "0000:03:00.0"},
				{Netdev: "p1", PCIAddress: "0000:03:00.1"},
			}, nil
		},
	}
}

var _ = Describe("DPUAgent", func() {
	Describe("UID adoption logic", func() {
		It("keeps in-memory status when adopting a recreated DPU", func() {
			existingRebootMethod := provisioningv1.RebootMethodNoAction
			existingCond := metav1.Condition{
				Type:               "ExistingCondition",
				Status:             metav1.ConditionTrue,
				Reason:             "Kept",
				Message:            "must stay",
				LastTransitionTime: metav1.NewTime(time.Unix(99, 0)),
			}
			existingPending := &provisioningv1.PendingNVConfigState{
				BootID: "old-boot",
				Devices: []provisioningv1.PendingNVConfigDevice{
					{
						Device: "0000:03:00.0",
						Entries: []provisioningv1.PendingNVConfigEntry{
							{Name: "VIRTIO_NET_EMULATION_NUM_MSIX", Current: "0", NextBoot: "2"},
						},
					},
				},
			}
			existingStatus := provisioningv1.AgentStatus{
				RebootMethod:                &existingRebootMethod,
				LastObservedPendingNVConfig: existingPending,
				Conditions:                  []metav1.Condition{existingCond},
				PreInstall: &provisioningv1.AgentPreInstallStatus{
					AgentReported: ptr.To(metav1.NewTime(time.Unix(98, 0))),
					Conditions: []metav1.Condition{
						{
							Type:               provisioningv1.DPUAgentConditionNVConfigApplied,
							Status:             metav1.ConditionTrue,
							Reason:             "Old",
							Message:            "must be cleared",
							LastTransitionTime: metav1.NewTime(time.Unix(98, 0)),
						},
					},
				},
			}
			optCtx := &operations.Context{
				Options: opts.Options{DPUUID: "old-uid"},
				Status:  statusmanager.New(nil, "", "", ""),
			}
			optCtx.Status.UpdateLocal(func(s *provisioningv1.AgentStatus) { *s = *existingStatus.DeepCopy() })
			dpu := &provisioningv1.DPU{ObjectMeta: metav1.ObjectMeta{UID: "new-uid"}}

			changed := dpuUIDChanged(optCtx, dpu)

			Expect(changed).To(BeTrue())
			Expect(optCtx.Options.DPUUID).To(Equal("old-uid"))
			Expect(optCtx.Status.GetLocal()).To(Equal(existingStatus))
		})

		It("does not clear LastObservedPendingNVConfig when UID does not change", func() {
			existing := &provisioningv1.PendingNVConfigState{
				BootID: "boot-1",
				Devices: []provisioningv1.PendingNVConfigDevice{
					{Device: "0000:03:00.0"},
				},
			}
			optCtx := &operations.Context{
				Options: opts.Options{DPUUID: "same-uid"},
				Status:  statusmanager.New(nil, "", "", ""),
			}
			optCtx.Status.UpdateLocal(func(s *provisioningv1.AgentStatus) { s.LastObservedPendingNVConfig = existing.DeepCopy() })
			dpu := &provisioningv1.DPU{ObjectMeta: metav1.ObjectMeta{UID: "same-uid"}}

			changed := dpuUIDChanged(optCtx, dpu)

			Expect(changed).To(BeFalse())
			Expect(optCtx.Status.GetLocal().LastObservedPendingNVConfig).To(Equal(existing))
		})
	})

	Describe("reconcileOwnedDPU", func() {
		It("reports pre-install status to a recreated DPU without the old OS status", func() {
			dpu := newTestDPU()
			dpu.UID = "new-uid"
			existingStartup := metav1.NewTime(time.Unix(100, 0))
			existingRebootMethod := provisioningv1.RebootMethodNoAction
			dpu.Status.AgentStatus = &provisioningv1.AgentStatus{
				LastStartupTime: &existingStartup,
				RebootMethod:    &existingRebootMethod,
				Conditions: []metav1.Condition{
					{
						Type:               "RegularCondition",
						Status:             metav1.ConditionTrue,
						Reason:             "Kept",
						Message:            "keep regular status",
						LastTransitionTime: metav1.NewTime(time.Unix(101, 0)),
					},
				},
			}
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()

			agent := &DPUAgent{optCtx: newTestOptCtx(fakeClient)}
			preInstallStatus := statusmanager.New(fakeClient, "test-ns", "test-dpu", "")
			statusCtx, cancelStatus := context.WithCancel(ctx)
			defer cancelStatus()
			preInstallStatus.Start(statusCtx)
			// Status of the old OS, which must not reach the recreated DPU.
			newStartup := metav1.NewTime(time.Unix(200, 0))
			agent.optCtx.Status.UpdateLocal(func(s *provisioningv1.AgentStatus) {
				s.LastStartupTime = &newStartup
				s.RebootMethod = ptr.To(provisioningv1.RebootMethodPowerCycle)
				meta.SetStatusCondition(&s.Conditions, metav1.Condition{Type: "OldOSCondition", Status: metav1.ConditionTrue, Reason: "Old"})
			})

			Expect(agent.reconcileOwnedDPU(ctx, preInstallStatus, nil)).To(Succeed())

			latestDPU := &provisioningv1.DPU{}
			Expect(fakeClient.Get(ctx, client.ObjectKey{Namespace: "test-ns", Name: "test-dpu"}, latestDPU)).To(Succeed())
			Expect(latestDPU.Status.AgentStatus).NotTo(BeNil())
			Expect(latestDPU.Status.AgentStatus.LastStartupTime).NotTo(BeNil())
			Expect(latestDPU.Status.AgentStatus.LastStartupTime.Unix()).To(Equal(existingStartup.Unix()))
			Expect(latestDPU.Status.AgentStatus.RebootMethod).To(Equal(&existingRebootMethod))
			Expect(latestDPU.Status.AgentStatus.Conditions).To(HaveLen(1))
			Expect(latestDPU.Status.AgentStatus.PreInstall).NotTo(BeNil())
			Expect(latestDPU.Status.AgentStatus.PreInstall.AgentReported).NotTo(BeNil())
			reported := preInstallStatus.GetLocal().PreInstall.AgentReported
			Expect(latestDPU.Status.AgentStatus.PreInstall.AgentReported.Unix()).To(Equal(reported.Unix()))
		})

		It("reports a pre-install operation result to preInstall conditions", func() {
			dpu := newTestDPU()
			dpu.UID = "new-uid"
			reportedAt := metav1.NewTime(time.Unix(201, 0))
			dpu.Status.AgentStatus = &provisioningv1.AgentStatus{
				PreInstall: &provisioningv1.AgentPreInstallStatus{AgentReported: &reportedAt},
			}
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()

			agent := &DPUAgent{optCtx: newTestOptCtx(fakeClient)}
			preInstallStatus := statusmanager.New(fakeClient, "test-ns", "test-dpu", "")
			statusCtx, cancelStatus := context.WithCancel(ctx)
			defer cancelStatus()
			preInstallStatus.Start(statusCtx)
			localCtx := agent.snapshotPreInstallCtx(dpu, preInstallStatus)

			op := &mockOperation{
				name:                             "pre-install-op",
				conditionType:                    provisioningv1.DPUAgentConditionNVConfigApplied,
				shouldUpdateStatusBeforeContinue: true,
				executeFunc: func(_ context.Context, optCtx *operations.Context) error {
					optCtx.CondMessage = "pre-install NVConfig done"
					return nil
				},
			}
			Expect(agent.runPreInstallOperationOnce(ctx, op, &localCtx)).To(Succeed())

			latestDPU := &provisioningv1.DPU{}
			Expect(fakeClient.Get(ctx, client.ObjectKey{Namespace: "test-ns", Name: "test-dpu"}, latestDPU)).To(Succeed())
			preInstall := latestDPU.Status.AgentStatus.PreInstall
			Expect(preInstall.AgentReported.Unix()).To(Equal(reportedAt.Unix()))
			cond := meta.FindStatusCondition(preInstall.Conditions, provisioningv1.DPUAgentConditionNVConfigApplied)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			Expect(cond.Message).To(Equal("pre-install NVConfig done"))
			Expect(latestDPU.Status.AgentStatus.Conditions).To(BeEmpty())
		})
	})

	Describe("Done marker", func() {
		It("should write the done marker file after all operations complete successfully", func() {
			dpu := newTestDPU()
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()
			markerCalled := false

			agent := &DPUAgent{
				retryInterval:       testRetryInterval,
				writeDoneMarkerFunc: func(_ string) error { markerCalled = true; return nil },
				optCtx:              newTestOptCtx(fakeClient),
				operations: []operations.Operation{
					&mockOperation{name: "op1", conditionType: "Op1Condition", executeFunc: func(_ context.Context, _ *operations.Context) error { return nil }},
				},
			}
			Expect(agent.Run(ctx)).To(Succeed())
			Expect(markerCalled).To(BeTrue())
		})

		It("aborts bootstrap when a status push finds the DPU recreated", func() {
			dpu := newTestDPU()
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()

			agent := &DPUAgent{
				retryInterval:       testRetryInterval,
				writeDoneMarkerFunc: func(_ string) error { return nil },
				optCtx:              newTestOptCtx(fakeClient),
				operations: []operations.Operation{
					&mockOperation{name: "recreate-op", conditionType: "RecreateOpCondition", shouldUpdateStatusBeforeContinue: true,
						executeFunc: func(execCtx context.Context, _ *operations.Context) error {
							Expect(fakeClient.Delete(execCtx, newTestDPU())).To(Succeed())
							recreated := newTestDPU()
							recreated.UID = "new-uid"
							Expect(fakeClient.Create(execCtx, recreated)).To(Succeed())
							return nil
						}},
				},
			}
			err := agent.Run(ctx)
			Expect(IsBootstrapAbortErr(err)).To(BeTrue(), "got %v", err)

			latestDPU := &provisioningv1.DPU{}
			Expect(fakeClient.Get(ctx, client.ObjectKey{Namespace: "test-ns", Name: "test-dpu"}, latestDPU)).To(Succeed())
			Expect(latestDPU.Status.AgentStatus).To(BeNil(), "no status may reach the recreated DPU")
		})

		It("should not write the done marker when the run is aborted", func() {
			dpu := newTestDPU()
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()
			cancelCtx, cancelFunc := context.WithCancel(ctx)
			markerCalled := false

			agent := &DPUAgent{
				retryInterval:       testRetryInterval,
				writeDoneMarkerFunc: func(_ string) error { markerCalled = true; return nil },
				optCtx:              newTestOptCtx(fakeClient),
				operations: []operations.Operation{
					&mockOperation{name: "cancel-op", conditionType: "CancelOpCondition", executeFunc: func(_ context.Context, _ *operations.Context) error {
						cancelFunc()
						return fmt.Errorf("error that triggers context check")
					}},
				},
			}
			err := agent.Run(cancelCtx)
			Expect(err).To(HaveOccurred())
			Expect(markerCalled).To(BeFalse())
		})

		It("should remove a stale done marker at startup before operations run", func() {
			dpu := newTestDPU()
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()
			removeCalled := false
			opExecuted := false

			agent := &DPUAgent{
				retryInterval:        testRetryInterval,
				removeDoneMarkerFunc: func(_ string) error { removeCalled = true; return nil },
				writeDoneMarkerFunc:  func(_ string) error { return nil },
				optCtx:               newTestOptCtx(fakeClient),
				operations: []operations.Operation{
					&mockOperation{name: "op1", conditionType: "Op1Condition", executeFunc: func(_ context.Context, _ *operations.Context) error {
						Expect(removeCalled).To(BeTrue(), "stale marker should be removed before operations run")
						opExecuted = true
						return nil
					}},
				},
			}
			Expect(agent.Run(ctx)).To(Succeed())
			Expect(opExecuted).To(BeTrue())
		})

		It("should return error when removing the stale done marker fails", func() {
			dpu := newTestDPU()
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()

			agent := &DPUAgent{
				retryInterval:        testRetryInterval,
				removeDoneMarkerFunc: func(_ string) error { return fmt.Errorf("permission denied") },
				writeDoneMarkerFunc:  func(_ string) error { return nil },
				optCtx:               newTestOptCtx(fakeClient),
				operations: []operations.Operation{
					&mockOperation{name: "op1", conditionType: "Op1Condition", executeFunc: func(_ context.Context, _ *operations.Context) error { return nil }},
				},
			}
			err := agent.Run(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("stale done marker"))
		})

		It("should return error when writing the done marker fails", func() {
			dpu := newTestDPU()
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()

			agent := &DPUAgent{
				retryInterval:       testRetryInterval,
				writeDoneMarkerFunc: func(_ string) error { return fmt.Errorf("disk full") },
				optCtx:              newTestOptCtx(fakeClient),
				operations: []operations.Operation{
					&mockOperation{name: "op1", conditionType: "Op1Condition", executeFunc: func(_ context.Context, _ *operations.Context) error { return nil }},
				},
			}
			err := agent.Run(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("done marker"))
		})
	})

	Describe("Run", func() {
		noopMarker := func(_ string) error { return nil }

		It("reloads the DPUFlavor and restarts the pipeline when a failed operation observes a spec change", func() {
			dpu := newTestDPU()
			flavor := &provisioningv1.DPUFlavor{
				ObjectMeta: metav1.ObjectMeta{Name: "flavor-1", Namespace: "test-ns"},
				Spec: provisioningv1.DPUFlavorSpec{
					Sysctl: provisioningv1.DPUFLavorSysctl{Parameters: []string{"bad"}},
				},
			}
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu, flavor).WithStatusSubresource(dpu).Build()
			stored := &provisioningv1.DPUFlavor{}
			Expect(fakeClient.Get(ctx, client.ObjectKey{Namespace: "test-ns", Name: "flavor-1"}, stored)).To(Succeed())

			var attempts []string
			agent := &DPUAgent{
				retryInterval:       testRetryInterval,
				writeDoneMarkerFunc: noopMarker,
				optCtx:              newTestOptCtx(fakeClient),
				operations: []operations.Operation{
					&mockOperation{
						name:          "nvconfig",
						conditionType: "NVConfigApplied",
						executeFunc: func(execCtx context.Context, optCtx *operations.Context) error {
							param := ""
							if len(optCtx.DPUFlavor.Spec.Sysctl.Parameters) > 0 {
								param = optCtx.DPUFlavor.Spec.Sysctl.Parameters[0]
							}
							attempts = append(attempts, param)
							if param == "bad" {
								optCtx.SetResolvedNVConfig(&operations.ResolvedNVConfig{})
								optCtx.DeferredNVConfigParams = []operations.DeferredNVConfigParam{{Device: "0000:03:00.0", Params: "FOO=1"}}
								updated := stored.DeepCopy()
								updated.Spec.Sysctl.Parameters = []string{"good"}
								// The fake client does not bump generation. The apiserver does on this spec edit.
								updated.Generation = stored.Generation + 1
								Expect(fakeClient.Patch(execCtx, updated, client.MergeFrom(stored))).To(Succeed())
								return fmt.Errorf("invalid nvconfig")
							}
							// The pipeline restarted: ResetPerRunState cleared the
							// per-run state built from the old flavor, and this run
							// sees the fixed spec.
							Expect(optCtx.GetResolvedNVConfig()).To(BeNil())
							Expect(optCtx.DeferredNVConfigParams).To(BeEmpty())
							// The leftover-success skip (e.g. NVConfigApplied on the
							// legacy boot-ID path) is disabled for the restarted
							// pipeline.
							Expect(optCtx.FlavorUpdated).To(BeTrue())
							return nil
						},
					},
				},
			}
			agent.optCtx.DPUFlavor = *stored
			Expect(agent.Run(ctx)).To(Succeed())
			Expect(attempts).To(Equal([]string{"bad", "good"}))
		})

		It("restarts from the first operation when the flavor changes during a retry", func() {
			dpu := newTestDPU()
			flavor := &provisioningv1.DPUFlavor{
				ObjectMeta: metav1.ObjectMeta{Name: "flavor-1", Namespace: "test-ns"},
				Spec: provisioningv1.DPUFlavorSpec{
					Sysctl: provisioningv1.DPUFLavorSysctl{Parameters: []string{"bad"}},
				},
			}
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu, flavor).WithStatusSubresource(dpu).Build()
			stored := &provisioningv1.DPUFlavor{}
			Expect(fakeClient.Get(ctx, client.ObjectKey{Namespace: "test-ns", Name: "flavor-1"}, stored)).To(Succeed())

			var executionOrder []string
			agent := &DPUAgent{
				retryInterval:       testRetryInterval,
				writeDoneMarkerFunc: noopMarker,
				optCtx:              newTestOptCtx(fakeClient),
				operations: []operations.Operation{
					&mockOperation{
						name:          "first",
						conditionType: "First",
						executeFunc: func(_ context.Context, _ *operations.Context) error {
							executionOrder = append(executionOrder, "first")
							return nil
						},
					},
					&mockOperation{
						name:          "second",
						conditionType: "Second",
						executeFunc: func(execCtx context.Context, optCtx *operations.Context) error {
							param := ""
							if len(optCtx.DPUFlavor.Spec.Sysctl.Parameters) > 0 {
								param = optCtx.DPUFlavor.Spec.Sysctl.Parameters[0]
							}
							executionOrder = append(executionOrder, "second:"+param)
							if param == "bad" {
								updated := stored.DeepCopy()
								updated.Spec.Sysctl.Parameters = []string{"good"}
								updated.Generation = stored.Generation + 1
								Expect(fakeClient.Patch(execCtx, updated, client.MergeFrom(stored))).To(Succeed())
								return fmt.Errorf("invalid config")
							}
							return nil
						},
					},
				},
			}
			agent.optCtx.DPUFlavor = *stored
			Expect(agent.Run(ctx)).To(Succeed())
			Expect(executionOrder).To(Equal([]string{"first", "second:bad", "first", "second:good"}))
		})

		It("keeps retrying the same operation when only flavor metadata changes", func() {
			dpu := newTestDPU()
			flavor := &provisioningv1.DPUFlavor{
				ObjectMeta: metav1.ObjectMeta{Name: "flavor-1", Namespace: "test-ns"},
				Spec: provisioningv1.DPUFlavorSpec{
					Sysctl: provisioningv1.DPUFLavorSysctl{Parameters: []string{"bad"}},
				},
			}
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu, flavor).WithStatusSubresource(dpu).Build()
			stored := &provisioningv1.DPUFlavor{}
			Expect(fakeClient.Get(ctx, client.ObjectKey{Namespace: "test-ns", Name: "flavor-1"}, stored)).To(Succeed())

			var executionOrder []string
			secondRuns := 0
			agent := &DPUAgent{
				retryInterval:       testRetryInterval,
				writeDoneMarkerFunc: noopMarker,
				optCtx:              newTestOptCtx(fakeClient),
				operations: []operations.Operation{
					&mockOperation{
						name:          "first",
						conditionType: "First",
						executeFunc: func(_ context.Context, _ *operations.Context) error {
							executionOrder = append(executionOrder, "first")
							return nil
						},
					},
					&mockOperation{
						name:          "second",
						conditionType: "Second",
						executeFunc: func(execCtx context.Context, optCtx *operations.Context) error {
							param := ""
							if len(optCtx.DPUFlavor.Spec.Sysctl.Parameters) > 0 {
								param = optCtx.DPUFlavor.Spec.Sysctl.Parameters[0]
							}
							executionOrder = append(executionOrder, "second:"+param)
							secondRuns++
							switch secondRuns {
							case 1:
								// Metadata-only write: generation is unchanged, so the
								// retry keeps working on this operation instead of
								// restarting the pipeline.
								updated := stored.DeepCopy()
								updated.Labels = map[string]string{"touched": "true"}
								Expect(fakeClient.Patch(execCtx, updated, client.MergeFrom(stored))).To(Succeed())
								return fmt.Errorf("invalid config")
							case 2:
								updated := stored.DeepCopy()
								updated.Spec.Sysctl.Parameters = []string{"good"}
								updated.Generation = stored.Generation + 1
								Expect(fakeClient.Patch(execCtx, updated, client.MergeFrom(stored))).To(Succeed())
								return fmt.Errorf("invalid config")
							}
							return nil
						},
					},
				},
			}
			agent.optCtx.DPUFlavor = *stored
			Expect(agent.Run(ctx)).To(Succeed())
			Expect(executionOrder).To(Equal([]string{"first", "second:bad", "second:bad", "first", "second:good"}))
		})

		It("removes the once-per-boot OVS script marker on a flavor-change restart without in-place reconfig", func() {
			dpu := newTestDPU()
			flavor := &provisioningv1.DPUFlavor{
				ObjectMeta: metav1.ObjectMeta{Name: "flavor-1", Namespace: "test-ns"},
				Spec: provisioningv1.DPUFlavorSpec{
					Sysctl: provisioningv1.DPUFLavorSysctl{Parameters: []string{"bad"}},
				},
			}
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu, flavor).WithStatusSubresource(dpu).Build()
			stored := &provisioningv1.DPUFlavor{}
			Expect(fakeClient.Get(ctx, client.ObjectKey{Namespace: "test-ns", Name: "flavor-1"}, stored)).To(Succeed())

			runDir, err := os.MkdirTemp("", "dpu-agent-run")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { Expect(os.RemoveAll(runDir)).To(Succeed()) })
			markerPath := filepath.Join(runDir, ovsscript.DoneMarkerFileName)
			Expect(os.WriteFile(markerPath, nil, 0644)).To(Succeed())

			attempts := 0
			agent := &DPUAgent{
				retryInterval:       testRetryInterval,
				writeDoneMarkerFunc: noopMarker,
				optCtx:              newTestOptCtx(fakeClient),
				runDir:              runDir,
				operations: []operations.Operation{
					&mockOperation{
						name:          "nvconfig",
						conditionType: "NVConfigApplied",
						executeFunc: func(execCtx context.Context, _ *operations.Context) error {
							attempts++
							if attempts == 1 {
								updated := stored.DeepCopy()
								updated.Spec.Sysctl.Parameters = []string{"good"}
								updated.Generation = stored.Generation + 1
								Expect(fakeClient.Patch(execCtx, updated, client.MergeFrom(stored))).To(Succeed())
								return fmt.Errorf("invalid nvconfig")
							}
							return nil
						},
					},
				},
			}
			agent.optCtx.DPUFlavor = *stored
			// Initial provisioning: not an in-place reconfig.
			Expect(agent.optCtx.ReapplyConfig).To(BeFalse())
			Expect(agent.Run(ctx)).To(Succeed())
			Expect(attempts).To(Equal(2))
			Expect(agent.optCtx.FlavorUpdated).To(BeTrue())
			Expect(markerPath).NotTo(BeAnExistingFile())
		})

		It("keeps the flavor loaded at the start of the Run while operations succeed", func() {
			dpu := newTestDPU()
			flavor := &provisioningv1.DPUFlavor{
				ObjectMeta: metav1.ObjectMeta{Name: "flavor-1", Namespace: "test-ns"},
				Spec: provisioningv1.DPUFlavorSpec{
					Sysctl: provisioningv1.DPUFLavorSysctl{Parameters: []string{"original"}},
				},
			}
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu, flavor).WithStatusSubresource(dpu).Build()
			stored := &provisioningv1.DPUFlavor{}
			Expect(fakeClient.Get(ctx, client.ObjectKey{Namespace: "test-ns", Name: "flavor-1"}, stored)).To(Succeed())

			var seen []string
			readParam := func(optCtx *operations.Context) string {
				if len(optCtx.DPUFlavor.Spec.Sysctl.Parameters) == 0 {
					return ""
				}
				return optCtx.DPUFlavor.Spec.Sysctl.Parameters[0]
			}
			agent := &DPUAgent{
				retryInterval:       testRetryInterval,
				writeDoneMarkerFunc: noopMarker,
				optCtx:              newTestOptCtx(fakeClient),
				operations: []operations.Operation{
					&mockOperation{
						name:          "first",
						conditionType: "First",
						executeFunc: func(execCtx context.Context, optCtx *operations.Context) error {
							seen = append(seen, readParam(optCtx))
							updated := stored.DeepCopy()
							updated.Spec.Sysctl.Parameters = []string{"patched"}
							Expect(fakeClient.Update(execCtx, updated)).To(Succeed())
							return nil
						},
					},
					&mockOperation{
						name:          "second",
						conditionType: "Second",
						executeFunc: func(_ context.Context, optCtx *operations.Context) error {
							seen = append(seen, readParam(optCtx))
							return nil
						},
					},
				},
			}
			agent.optCtx.DPUFlavor = *stored
			Expect(agent.Run(ctx)).To(Succeed())
			Expect(seen).To(Equal([]string{"original", "original"}))
		})

		It("should include package installation after static file verification", func() {
			agent := NewDPUAgent(&operations.Context{})
			names := make([]string, 0, len(agent.operations))
			for _, op := range agent.operations {
				names = append(names, op.Name())
			}

			Expect(names).To(ContainElement("Get Latest DPU"))
			Expect(names).To(ContainElement("Verify Static Files"))
			Expect(names).To(ContainElement("Install Packages"))
			Expect(names).To(ContainElement("Handle Reboot"))
			Expect(names).To(ContainElement("Start Kubelet"))
			Expect(names).To(ContainElement("Report Node Labels"))
			Expect(names).To(ContainElement("Configure Network"))
			Expect(names).To(ContainElement("Run OVS Script"))
			Expect(names).To(ContainElement("Set Netplan Underlay MTU"))
			Expect(indexOf(names, "Get Latest DPU")).To(BeNumerically("<", indexOf(names, "Verify Static Files")))
			Expect(indexOf(names, "Verify Static Files")).To(BeNumerically("<", indexOf(names, "Install Packages")))
			Expect(indexOf(names, "Install Packages")).To(BeNumerically("<", indexOf(names, "Handle Reboot")))
			Expect(indexOf(names, "Start Kubelet")).To(BeNumerically("<", indexOf(names, "Report Node Labels")))
			Expect(indexOf(names, "Configure Network")).To(BeNumerically("<", indexOf(names, "Run OVS Script")))
			Expect(indexOf(names, "Run OVS Script")).To(BeNumerically("<", indexOf(names, "Set Netplan Underlay MTU")))
		})

		It("should execute operations in order", func() {
			dpu := newTestDPU()
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()

			executionOrder := []string{}
			mockOps := []operations.Operation{
				&mockOperation{name: "op1", conditionType: "Op1Condition", executeFunc: func(_ context.Context, _ *operations.Context) error {
					executionOrder = append(executionOrder, "op1")
					return nil
				}},
				&mockOperation{name: "op2", conditionType: "Op2Condition", executeFunc: func(_ context.Context, _ *operations.Context) error {
					executionOrder = append(executionOrder, "op2")
					return nil
				}},
				&mockOperation{name: "op3", conditionType: "Op3Condition", executeFunc: func(_ context.Context, _ *operations.Context) error {
					executionOrder = append(executionOrder, "op3")
					return nil
				}},
			}

			agent := &DPUAgent{retryInterval: testRetryInterval, writeDoneMarkerFunc: noopMarker, optCtx: newTestOptCtx(fakeClient), operations: mockOps}
			Expect(agent.Run(ctx)).To(Succeed())
			Expect(executionOrder).To(Equal([]string{"op1", "op2", "op3"}))
		})

		It("initializes RebootMethod to Unknown so stale values from a previous session are overwritten", func() {
			dpu := newTestDPU()
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()

			var captured *provisioningv1.RebootMethodType
			agent := &DPUAgent{
				retryInterval:       testRetryInterval,
				writeDoneMarkerFunc: noopMarker,
				optCtx:              newTestOptCtx(fakeClient),
				operations: []operations.Operation{
					&mockOperation{name: "op1", conditionType: "Op1Condition", executeFunc: func(_ context.Context, optCtx *operations.Context) error {
						captured = optCtx.Status.GetLocal().RebootMethod
						return nil
					}},
				},
			}
			Expect(agent.Run(ctx)).To(Succeed())
			Expect(captured).NotTo(BeNil())
			Expect(*captured).To(Equal(provisioningv1.RebootMethodUnknown))
		})

		It("sets RebootMethodDiscovery on the operation context before operations run", func() {
			dpu := newTestDPU()
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()

			var discovery bool
			agent := &DPUAgent{
				retryInterval:       testRetryInterval,
				writeDoneMarkerFunc: noopMarker,
				optCtx:              newTestOptCtx(fakeClient),
				operations: []operations.Operation{
					&mockOperation{name: "op1", conditionType: "Op1Condition", executeFunc: func(_ context.Context, optCtx *operations.Context) error {
						discovery = optCtx.RebootMethodDiscovery
						return nil
					}},
				},
			}
			Expect(agent.Run(ctx)).To(Succeed())
			Expect(discovery).To(BeFalse(), "discovery is false when MFT tools are missing or below min version")
		})

		It("sets RebootMethodDiscovery true when rebootMethodDiscoveryFunc returns true", func() {
			dpu := newTestDPU()
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()

			var discovery bool
			agent := &DPUAgent{
				retryInterval:             testRetryInterval,
				writeDoneMarkerFunc:       noopMarker,
				rebootMethodDiscoveryFunc: func(context.Context) bool { return true },
				optCtx:                    newTestOptCtx(fakeClient),
				operations: []operations.Operation{
					&mockOperation{name: "op1", conditionType: "Op1Condition", executeFunc: func(_ context.Context, optCtx *operations.Context) error {
						discovery = optCtx.RebootMethodDiscovery
						return nil
					}},
				},
			}
			Expect(agent.Run(ctx)).To(Succeed())
			Expect(discovery).To(BeTrue())
		})

		It("sets RebootMethodDiscovery true for BlueField4 without probing MFT versions", func() {
			dpu := newTestDPU()
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()

			var discovery bool
			optCtx := newTestOptCtx(fakeClient)
			optCtx.Options.DPUType = string(provisioningv1.DPUTypeBlueField4)
			agent := &DPUAgent{
				retryInterval:       testRetryInterval,
				writeDoneMarkerFunc: noopMarker,
				rebootMethodDiscoveryFunc: func(context.Context) bool {
					Fail("MFT version discovery should not run for BlueField4")
					return false
				},
				optCtx: optCtx,
				operations: []operations.Operation{
					&mockOperation{name: "op1", conditionType: "Op1Condition", executeFunc: func(_ context.Context, optCtx *operations.Context) error {
						discovery = optCtx.RebootMethodDiscovery
						return nil
					}},
				},
			}
			Expect(agent.Run(ctx)).To(Succeed())
			Expect(discovery).To(BeTrue())
		})

		It("sets RebootMethodDiscovery false when SkipRebootMethodDiscovery is true", func() {
			dpu := newTestDPU()
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()

			var discovery bool
			optCtx := newTestOptCtx(fakeClient)
			optCtx.Options.SkipRebootMethodDiscovery = true
			optCtx.Options.DPUType = string(provisioningv1.DPUTypeBlueField4)
			agent := &DPUAgent{
				retryInterval:             testRetryInterval,
				writeDoneMarkerFunc:       noopMarker,
				rebootMethodDiscoveryFunc: func(context.Context) bool { return true },
				optCtx:                    optCtx,
				operations: []operations.Operation{
					&mockOperation{name: "op1", conditionType: "Op1Condition", executeFunc: func(_ context.Context, optCtx *operations.Context) error {
						discovery = optCtx.RebootMethodDiscovery
						return nil
					}},
				},
			}
			Expect(agent.Run(ctx)).To(Succeed())
			Expect(discovery).To(BeFalse())
		})

		It("should skip operations that should be skipped", func() {
			dpu := newTestDPU()
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()

			executionOrder := []string{}
			agent := &DPUAgent{
				retryInterval:       testRetryInterval,
				writeDoneMarkerFunc: noopMarker,
				optCtx:              newTestOptCtx(fakeClient),
				operations: []operations.Operation{
					&mockOperation{name: "op1", conditionType: "Op1Condition", executeFunc: func(_ context.Context, _ *operations.Context) error {
						executionOrder = append(executionOrder, "op1")
						return nil
					}},
					&mockOperation{name: "op2-skipped", conditionType: "Op2Condition", shouldSkip: true, executeFunc: func(_ context.Context, _ *operations.Context) error {
						executionOrder = append(executionOrder, "op2-skipped")
						return nil
					}},
					&mockOperation{name: "op3", conditionType: "Op3Condition", executeFunc: func(_ context.Context, _ *operations.Context) error {
						executionOrder = append(executionOrder, "op3")
						return nil
					}},
				},
			}
			Expect(agent.Run(ctx)).To(Succeed())
			Expect(executionOrder).To(Equal([]string{"op1", "op3"}))
		})

		It("should retry failed operations until success", func() {
			dpu := newTestDPU()
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()

			attempts := 0
			agent := &DPUAgent{
				retryInterval:       testRetryInterval,
				writeDoneMarkerFunc: noopMarker,
				optCtx:              newTestOptCtx(fakeClient),
				operations: []operations.Operation{
					&mockOperation{name: "flaky-op", conditionType: "FlakyOpCondition", executeFunc: func(_ context.Context, _ *operations.Context) error {
						attempts++
						if attempts < 3 {
							return fmt.Errorf("temporary error")
						}
						return nil
					}},
				},
			}
			Expect(agent.Run(ctx)).To(Succeed())
			Expect(attempts).To(Equal(3))
		})

		It("uses CondMessage for the success condition message", func() {
			dpu := newTestDPU()
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()
			const condType = "Op1Condition"

			agent := &DPUAgent{
				retryInterval:       testRetryInterval,
				writeDoneMarkerFunc: noopMarker,
				optCtx:              newTestOptCtx(fakeClient),
				operations: []operations.Operation{
					&mockOperation{name: "op1", conditionType: condType, executeFunc: func(_ context.Context, optCtx *operations.Context) error {
						optCtx.CondMessage = "custom success message"
						return nil
					}},
				},
			}
			Expect(agent.Run(ctx)).To(Succeed())
			cond := meta.FindStatusCondition(agent.optCtx.Status.GetLocal().Conditions, condType)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Message).To(Equal("custom success message"))
		})

		It("truncates CondMessage before writing the success condition", func() {
			dpu := newTestDPU()
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()
			const condType = "LongMessageCondition"
			longMessage := strings.Repeat("a", 9000)

			agent := &DPUAgent{
				retryInterval:       testRetryInterval,
				writeDoneMarkerFunc: noopMarker,
				optCtx:              newTestOptCtx(fakeClient),
				operations: []operations.Operation{
					&mockOperation{name: "op1", conditionType: condType, executeFunc: func(_ context.Context, optCtx *operations.Context) error {
						optCtx.CondMessage = longMessage
						return nil
					}},
				},
			}
			Expect(agent.Run(ctx)).To(Succeed())
			cond := meta.FindStatusCondition(agent.optCtx.Status.GetLocal().Conditions, condType)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Message).To(Equal(dpuutil.TruncateConditionMessage(longMessage)))
		})

		It("clears CondMessage before each retry attempt", func() {
			dpu := newTestDPU()
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()
			const condType = "RetryCondition"
			attempts := 0
			seen := []string{}

			agent := &DPUAgent{
				retryInterval:       testRetryInterval,
				writeDoneMarkerFunc: noopMarker,
				optCtx:              newTestOptCtx(fakeClient),
				operations: []operations.Operation{
					&mockOperation{name: "retry-op", conditionType: condType, executeFunc: func(_ context.Context, optCtx *operations.Context) error {
						seen = append(seen, optCtx.CondMessage)
						attempts++
						if attempts == 1 {
							optCtx.CondMessage = "stale message"
							return fmt.Errorf("temporary error")
						}
						optCtx.CondMessage = "fresh message"
						return nil
					}},
				},
			}
			Expect(agent.Run(ctx)).To(Succeed())
			Expect(seen).To(Equal([]string{"", ""}))
			cond := meta.FindStatusCondition(agent.optCtx.Status.GetLocal().Conditions, condType)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Message).To(Equal("fresh message"))
		})

		It("does not leak CondMessage to the next operation", func() {
			dpu := newTestDPU()
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()
			const firstCond = "FirstCondition"
			const secondCond = "SecondCondition"
			secondSeen := "unset"

			agent := &DPUAgent{
				retryInterval:       testRetryInterval,
				writeDoneMarkerFunc: noopMarker,
				optCtx:              newTestOptCtx(fakeClient),
				operations: []operations.Operation{
					&mockOperation{name: "op1", conditionType: firstCond, executeFunc: func(_ context.Context, optCtx *operations.Context) error {
						optCtx.CondMessage = "first message"
						return nil
					}},
					&mockOperation{name: "op2", conditionType: secondCond, executeFunc: func(_ context.Context, optCtx *operations.Context) error { secondSeen = optCtx.CondMessage; return nil }},
				},
			}
			Expect(agent.Run(ctx)).To(Succeed())
			Expect(secondSeen).To(BeEmpty())
			first := meta.FindStatusCondition(agent.optCtx.Status.GetLocal().Conditions, firstCond)
			Expect(first).NotTo(BeNil())
			Expect(first.Message).To(Equal("first message"))
			second := meta.FindStatusCondition(agent.optCtx.Status.GetLocal().Conditions, secondCond)
			Expect(second).NotTo(BeNil())
			Expect(second.Message).To(BeEmpty())
		})

		It("should update status when operation fails or requires update before continue", func() {
			dpu := newTestDPU()
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()
			failingOpAttempts := 0

			agent := &DPUAgent{
				retryInterval:       testRetryInterval,
				writeDoneMarkerFunc: noopMarker,
				optCtx:              newTestOptCtx(fakeClient),
				operations: []operations.Operation{
					&mockOperation{name: "failing-op", conditionType: "FailingOpCondition", executeFunc: func(_ context.Context, _ *operations.Context) error {
						defer func() { failingOpAttempts++ }()
						if failingOpAttempts < 3 {
							return fmt.Errorf("temporary error")
						}
						return nil
					}},
					&mockOperation{name: "status-update-op", conditionType: "StatusUpdateCondition", shouldUpdateStatusBeforeContinue: true, executeFunc: func(_ context.Context, _ *operations.Context) error { return nil }},
				},
			}
			Expect(agent.Run(ctx)).To(Succeed())

			latestDPU := &provisioningv1.DPU{}
			Expect(fakeClient.Get(ctx, client.ObjectKey{Namespace: "test-ns", Name: "test-dpu"}, latestDPU)).To(Succeed())
			Expect(latestDPU.Status.AgentStatus).NotTo(BeNil())
			Expect(latestDPU.Status.AgentStatus.Conditions).NotTo(BeEmpty())
		})

		It("should abort and return error when context is canceled and not execute subsequent operations", func() {
			dpu := newTestDPU()
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()
			cancelCtx, cancelFunc := context.WithCancel(ctx)
			attempts := 0
			secondOpExecuted := false

			agent := &DPUAgent{
				retryInterval:       testRetryInterval,
				writeDoneMarkerFunc: noopMarker,
				optCtx:              newTestOptCtx(fakeClient),
				operations: []operations.Operation{
					&mockOperation{name: "blocking-op", conditionType: "BlockingOpCondition", executeFunc: func(_ context.Context, _ *operations.Context) error {
						attempts++
						if attempts >= 2 {
							cancelFunc()
						}
						return fmt.Errorf("persistent error")
					}},
					&mockOperation{name: "subsequent-op", conditionType: "SubsequentOpCondition", executeFunc: func(_ context.Context, _ *operations.Context) error { secondOpExecuted = true; return nil }},
				},
			}
			err := agent.Run(cancelCtx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("context canceled"))
			Expect(secondOpExecuted).To(BeFalse(), "subsequent operation should not be executed after context is canceled")
		})
	})

	Describe("in-place reconfig Run", func() {
		noopMarker := func(_ string) error { return nil }

		reconcile := func(agent *DPUAgent, fakeClient client.Client, event ...*provisioningv1.DPU) error {
			preInstallStatus := statusmanager.New(fakeClient, "test-ns", "test-dpu", "")
			statusCtx, cancelStatus := context.WithCancel(ctx)
			defer cancelStatus()
			preInstallStatus.Start(statusCtx)
			var observed *provisioningv1.DPU
			if len(event) > 0 {
				observed = event[0]
			}
			return agent.reconcileOwnedDPU(ctx, preInstallStatus, observed)
		}

		setDPUConfigPhase := func(c client.Client) {
			dpu := &provisioningv1.DPU{}
			Expect(c.Get(ctx, client.ObjectKey{Namespace: "test-ns", Name: "test-dpu"}, dpu)).To(Succeed())
			patch := client.MergeFrom(dpu.DeepCopy())
			dpu.Status.Phase = provisioningv1.DPUConfig
			Expect(c.Status().Patch(ctx, dpu, patch)).To(Succeed())
		}

		newAgent := func(fakeClient client.Client, executeFunc func(context.Context, *operations.Context) error) *DPUAgent {
			return &DPUAgent{
				retryInterval:        testRetryInterval,
				writeDoneMarkerFunc:  noopMarker,
				removeDoneMarkerFunc: noopMarker,
				optCtx:               newTestOptCtx(fakeClient),
				operations: []operations.Operation{
					&mockOperation{name: "reconfig-op", conditionType: "ReconfigOp", executeFunc: executeFunc},
				},
			}
		}

		DescribeTable("phaseLeftDPUConfig",
			func(phase provisioningv1.DPUPhase, want bool) {
				Expect(phaseLeftDPUConfig(phase)).To(Equal(want))
			},
			Entry("Ready", provisioningv1.DPUReady, true),
			Entry("Host Network Configuration", provisioningv1.DPUHostNetworkConfiguration, true),
			Entry("DPU Cluster Config", provisioningv1.DPUClusterConfig, true),
			Entry("Service Readiness", provisioningv1.DPUServiceReadiness, true),
			Entry("Node Effect Removal", provisioningv1.DPUNodeEffectRemoval, true),
			Entry("Node Effect", provisioningv1.DPUNodeEffect, true),
			Entry("DPU Config", provisioningv1.DPUConfig, false),
			Entry("Rebooting", provisioningv1.DPURebooting, false),
			Entry("Initializing", provisioningv1.DPUInitializing, false),
			Entry("Error", provisioningv1.DPUError, false),
			Entry("Deleting", provisioningv1.DPUDeleting, false),
		)

		It("keeps the OVS script marker on a process-start Run", func() {
			dpu := newTestDPU()
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()
			runDir, err := os.MkdirTemp("", "dpu-agent-run-*")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { Expect(os.RemoveAll(runDir)).To(Succeed()) })
			Expect(os.WriteFile(filepath.Join(runDir, doneMarkerFileName), []byte("old\n"), 0644)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(runDir, ovsscript.DoneMarkerFileName), nil, 0644)).To(Succeed())

			agent := &DPUAgent{
				retryInterval:       testRetryInterval,
				runDir:              runDir,
				writeDoneMarkerFunc: noopMarker,
				optCtx:              newTestOptCtx(fakeClient),
				operations: []operations.Operation{
					&mockOperation{name: "op1", conditionType: "Op1Condition", executeFunc: func(_ context.Context, _ *operations.Context) error {
						_, statErr := os.Stat(filepath.Join(runDir, ovsscript.DoneMarkerFileName))
						Expect(statErr).NotTo(HaveOccurred())
						return nil
					}},
				},
			}
			Expect(agent.Run(ctx)).To(Succeed())
			_, err = os.Stat(filepath.Join(runDir, doneMarkerFileName))
			Expect(os.IsNotExist(err)).To(BeTrue())
		})

		It("removes the OVS script marker on an in-place reconfig Run", func() {
			dpu := newTestDPU()
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()
			runDir, err := os.MkdirTemp("", "dpu-agent-run-*")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { Expect(os.RemoveAll(runDir)).To(Succeed()) })
			Expect(os.WriteFile(filepath.Join(runDir, ovsscript.DoneMarkerFileName), nil, 0644)).To(Succeed())
			optCtx := newTestOptCtx(fakeClient)
			optCtx.ReapplyConfig = true

			agent := &DPUAgent{
				retryInterval:       testRetryInterval,
				runDir:              runDir,
				writeDoneMarkerFunc: noopMarker,
				optCtx:              optCtx,
				operations: []operations.Operation{
					&mockOperation{name: "op1", conditionType: "Op1Condition", executeFunc: func(_ context.Context, _ *operations.Context) error {
						_, statErr := os.Stat(filepath.Join(runDir, ovsscript.DoneMarkerFileName))
						Expect(os.IsNotExist(statErr)).To(BeTrue())
						return nil
					}},
				},
			}
			Expect(agent.Run(ctx)).To(Succeed())
		})

		It("clears per-run caches so an in-process reconfig uses the live flavor", func() {
			dpu := newTestDPU()
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()
			optCtx := newTestOptCtx(fakeClient)
			optCtx.SetResolvedNVConfig(&operations.ResolvedNVConfig{HostOSInitRequired: true})
			optCtx.GrubConfigChanged = true
			optCtx.DeferredNVConfigParams = []operations.DeferredNVConfigParam{{Device: "0000:03:00.0", Params: "FOO=1"}}
			optCtx.SRIOVState = "stale"
			optCtx.CondMessage = "stale"

			agent := &DPUAgent{
				retryInterval:       testRetryInterval,
				writeDoneMarkerFunc: noopMarker,
				optCtx:              optCtx,
				operations: []operations.Operation{
					&mockOperation{name: "op1", conditionType: "Op1Condition", executeFunc: func(_ context.Context, got *operations.Context) error {
						Expect(got.GetResolvedNVConfig()).To(BeNil())
						Expect(got.GrubConfigChanged).To(BeFalse())
						Expect(got.DeferredNVConfigParams).To(BeNil())
						Expect(got.SRIOVState).To(BeNil())
						Expect(got.CondMessage).To(BeEmpty())
						return nil
					}},
				},
			}
			Expect(agent.Run(ctx)).To(Succeed())
		})

		It("preserves CA trust bundle and SPIRE condition on in-process reconfig", func() {
			dpu := newTestDPU()
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()
			hash := "bundle-hash"
			now := metav1.Now()
			optCtx := newTestOptCtx(fakeClient)
			optCtx.ReapplyConfig = true
			optCtx.Status.UpdateLocal(func(s *provisioningv1.AgentStatus) {
				s.TrustBundleHash = &hash
				s.TrustBundleLastUpdateTime = &now
				s.Conditions = []metav1.Condition{{
					Type:               provisioningv1.DPUAgentConditionSPIREWorkloadAttestorEnabled,
					Status:             metav1.ConditionTrue,
					Reason:             provisioningv1.DPUAgentConditionSPIREWorkloadAttestorEnabled,
					Message:            "SPIRE Kubernetes workload attestation is enabled",
					LastTransitionTime: now,
				}}
			})

			agent := &DPUAgent{
				retryInterval:       testRetryInterval,
				writeDoneMarkerFunc: noopMarker,
				optCtx:              optCtx,
				operations: []operations.Operation{
					&mockOperation{name: "op1", conditionType: "Op1Condition", executeFunc: func(_ context.Context, got *operations.Context) error {
						local := got.Status.GetLocal()
						Expect(local.TrustBundleHash).NotTo(BeNil())
						Expect(*local.TrustBundleHash).To(Equal(hash))
						Expect(local.TrustBundleLastUpdateTime).NotTo(BeNil())
						cond := meta.FindStatusCondition(local.Conditions, provisioningv1.DPUAgentConditionSPIREWorkloadAttestorEnabled)
						Expect(cond).NotTo(BeNil())
						Expect(cond.Status).To(Equal(metav1.ConditionTrue))
						Expect(local.Version).To(Equal(agentStatusVersion()))
						return nil
					}},
				},
			}
			Expect(agent.Run(ctx)).To(Succeed())
		})

		It("reports an injected DPF version", func() {
			orig := version
			version = "v30.0.0"
			DeferCleanup(func() { version = orig })

			dpu := newTestDPU()
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()
			agent := &DPUAgent{
				retryInterval:        testRetryInterval,
				removeDoneMarkerFunc: func(string) error { return nil },
				writeDoneMarkerFunc:  func(string) error { return nil },
				optCtx:               newTestOptCtx(fakeClient),
				operations: []operations.Operation{
					&mockOperation{name: "op1", conditionType: "Op1Condition"},
				},
			}
			Expect(agent.Run(ctx)).To(Succeed())
			Expect(agent.optCtx.Status.GetLocal().Version).To(Equal("v30.0.0"))
		})

		It("writes version on the first status push and the flavor generation only after success", func() {
			dpu := newTestDPU()
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()
			optCtx := newTestOptCtx(fakeClient)
			dpuFlavorCopy := optCtx.DPUFlavor
			dpuFlavorCopy.Generation = 42
			optCtx.DPUFlavor = dpuFlavorCopy
			agent := &DPUAgent{
				retryInterval:        testRetryInterval,
				removeDoneMarkerFunc: noopMarker,
				writeDoneMarkerFunc:  func(string) error { return fmt.Errorf("disk full") },
				optCtx:               optCtx,
				operations: []operations.Operation{
					&mockOperation{name: "push", conditionType: "PushCondition", shouldUpdateStatusBeforeContinue: true, executeFunc: func(_ context.Context, _ *operations.Context) error {
						return nil
					}},
				},
			}
			err := agent.Run(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("done marker"))

			latestDPU := &provisioningv1.DPU{}
			Expect(fakeClient.Get(ctx, client.ObjectKey{Namespace: "test-ns", Name: "test-dpu"}, latestDPU)).To(Succeed())
			Expect(latestDPU.Status.AgentStatus).NotTo(BeNil())
			Expect(latestDPU.Status.AgentStatus.Version).To(Equal(agentStatusVersion()))
			Expect(latestDPU.Status.AgentStatus.AppliedFlavorGeneration).To(BeZero())
			Expect(agent.optCtx.Status.GetLocal().AppliedFlavorGeneration).To(BeZero())

			agent.writeDoneMarkerFunc = noopMarker
			Expect(agent.Run(ctx)).To(Succeed())
			Expect(fakeClient.Get(ctx, client.ObjectKey{Namespace: "test-ns", Name: "test-dpu"}, latestDPU)).To(Succeed())
			Expect(latestDPU.Status.AgentStatus.Version).To(Equal(agentStatusVersion()))
			Expect(latestDPU.Status.AgentStatus.AppliedFlavorGeneration).To(Equal(int64(42)))
		})

		It("does not start a second Run while still in DPU Config", func() {
			dpu := newTestDPU()
			dpu.Status.Phase = provisioningv1.DPUConfig
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()
			runs := 0
			agent := newAgent(fakeClient, func(_ context.Context, _ *operations.Context) error {
				runs++
				return nil
			})
			Expect(reconcile(agent, fakeClient)).To(Succeed())
			Expect(runs).To(Equal(0))
		})

		It("starts one Run when phase returns to DPU Config after Ready", func() {
			dpu := newTestDPU()
			dpu.Status.Phase = provisioningv1.DPUReady
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()
			runs := 0
			agent := newAgent(fakeClient, func(_ context.Context, optCtx *operations.Context) error {
				runs++
				Expect(optCtx.ReapplyConfig).To(BeTrue())
				return nil
			})
			dpuFlavorCopy := agent.optCtx.DPUFlavor
			dpuFlavorCopy.Generation = 99
			agent.optCtx.DPUFlavor = dpuFlavorCopy
			Expect(reconcile(agent, fakeClient)).To(Succeed())
			Expect(runs).To(Equal(0))

			setDPUConfigPhase(fakeClient)
			Expect(reconcile(agent, fakeClient)).To(Succeed())
			Expect(runs).To(Equal(1))
			Expect(agent.optCtx.ReapplyConfig).To(BeFalse())
			Expect(agent.reconfig).To(Equal(reconfigIdle))

			latestDPU := &provisioningv1.DPU{}
			Expect(fakeClient.Get(ctx, client.ObjectKey{Namespace: "test-ns", Name: "test-dpu"}, latestDPU)).To(Succeed())
			Expect(latestDPU.Status.AgentStatus.Version).To(Equal(agentStatusVersion()))
			Expect(latestDPU.Status.AgentStatus.AppliedFlavorGeneration).To(Equal(int64(99)))
		})

		It("starts one Run when phase returns to DPU Config after Host Network Configuration", func() {
			dpu := newTestDPU()
			dpu.Status.Phase = provisioningv1.DPUHostNetworkConfiguration
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()
			runs := 0
			agent := newAgent(fakeClient, func(_ context.Context, _ *operations.Context) error {
				runs++
				return nil
			})
			Expect(reconcile(agent, fakeClient)).To(Succeed())
			setDPUConfigPhase(fakeClient)
			Expect(reconcile(agent, fakeClient)).To(Succeed())
			Expect(runs).To(Equal(1))
		})

		It("starts one Run when the watch event left DPU Config and the live object is already DPU Config", func() {
			dpu := newTestDPU()
			dpu.ResourceVersion = "12"
			dpu.Status.Phase = provisioningv1.DPUConfig
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()
			runs := 0
			agent := newAgent(fakeClient, func(_ context.Context, _ *operations.Context) error {
				runs++
				return nil
			})
			event := dpu.DeepCopy()
			event.ResourceVersion = "10"
			event.Status.Phase = provisioningv1.DPUNodeEffect
			Expect(reconcile(agent, fakeClient, event)).To(Succeed())
			Expect(runs).To(Equal(1))
			Expect(agent.reconfig).To(Equal(reconfigIdle))
		})

		It("does not start another Run for a queued post-config event from the same return", func() {
			dpu := newTestDPU()
			dpu.ResourceVersion = "12"
			dpu.Status.Phase = provisioningv1.DPUConfig
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()
			runs := 0
			agent := newAgent(fakeClient, func(_ context.Context, _ *operations.Context) error {
				runs++
				return nil
			})
			first := dpu.DeepCopy()
			first.ResourceVersion = "10"
			first.Status.Phase = provisioningv1.DPUNodeEffect
			Expect(reconcile(agent, fakeClient, first)).To(Succeed())
			Expect(runs).To(Equal(1))

			queued := dpu.DeepCopy()
			queued.ResourceVersion = "11"
			queued.Status.Phase = provisioningv1.DPUNodeEffect
			Expect(reconcile(agent, fakeClient, queued)).To(Succeed())
			Expect(runs).To(Equal(1))
			Expect(agent.reconfig).To(Equal(reconfigIdle))

			next := dpu.DeepCopy()
			next.ResourceVersion = "1000"
			next.Status.Phase = provisioningv1.DPUNodeEffect
			Expect(reconcile(agent, fakeClient, next)).To(Succeed())
			Expect(runs).To(Equal(2))
		})

		It("does not start a Run when the watch event is Rebooting and the live object is DPU Config", func() {
			dpu := newTestDPU()
			dpu.Status.Phase = provisioningv1.DPUConfig
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()
			runs := 0
			agent := newAgent(fakeClient, func(_ context.Context, _ *operations.Context) error {
				runs++
				return nil
			})
			event := dpu.DeepCopy()
			event.Status.Phase = provisioningv1.DPURebooting
			Expect(reconcile(agent, fakeClient, event)).To(Succeed())
			Expect(runs).To(Equal(0))
			Expect(agent.reconfig).To(Equal(reconfigIdle))
		})

		It("does not arm from a watch event for a different DPU UID", func() {
			dpu := newTestDPU()
			dpu.Status.Phase = provisioningv1.DPUConfig
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()
			runs := 0
			agent := newAgent(fakeClient, func(_ context.Context, _ *operations.Context) error {
				runs++
				return nil
			})
			event := dpu.DeepCopy()
			event.UID = "other-uid"
			event.Status.Phase = provisioningv1.DPUNodeEffect
			Expect(reconcile(agent, fakeClient, event)).To(Succeed())
			Expect(runs).To(Equal(0))
			Expect(agent.reconfig).To(Equal(reconfigIdle))
		})

		It("starts one Run when phase returns to DPU Config after Node Effect", func() {
			dpu := newTestDPU()
			dpu.Status.Phase = provisioningv1.DPUNodeEffect
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()
			runs := 0
			agent := newAgent(fakeClient, func(_ context.Context, _ *operations.Context) error {
				runs++
				return nil
			})
			Expect(reconcile(agent, fakeClient)).To(Succeed())
			setDPUConfigPhase(fakeClient)
			Expect(reconcile(agent, fakeClient)).To(Succeed())
			Expect(runs).To(Equal(1))
		})

		It("does not start a Run when Rebooting returns to DPU Config", func() {
			dpu := newTestDPU()
			dpu.Status.Phase = provisioningv1.DPURebooting
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()
			runs := 0
			agent := newAgent(fakeClient, func(_ context.Context, _ *operations.Context) error {
				runs++
				return nil
			})
			Expect(reconcile(agent, fakeClient)).To(Succeed())
			setDPUConfigPhase(fakeClient)
			Expect(reconcile(agent, fakeClient)).To(Succeed())
			Expect(runs).To(Equal(0))
		})

		It("does not start a third Run while still in DPU Config after a successful reconfig", func() {
			dpu := newTestDPU()
			dpu.Status.Phase = provisioningv1.DPUReady
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()
			runs := 0
			agent := newAgent(fakeClient, func(_ context.Context, _ *operations.Context) error {
				runs++
				return nil
			})
			Expect(reconcile(agent, fakeClient)).To(Succeed())
			setDPUConfigPhase(fakeClient)
			Expect(reconcile(agent, fakeClient)).To(Succeed())
			Expect(runs).To(Equal(1))
			Expect(reconcile(agent, fakeClient)).To(Succeed())
			Expect(runs).To(Equal(1))
		})

		It("retries after a failed reconfig Run", func() {
			dpu := newTestDPU()
			dpu.Status.Phase = provisioningv1.DPUReady
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()
			runs := 0
			agent := newAgent(fakeClient, func(_ context.Context, _ *operations.Context) error {
				runs++
				return nil
			})
			agent.writeDoneMarkerFunc = func(string) error { return fmt.Errorf("disk full") }
			Expect(reconcile(agent, fakeClient)).To(Succeed())
			setDPUConfigPhase(fakeClient)
			err := reconcile(agent, fakeClient)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("in-place reconfig Run"))
			Expect(runs).To(Equal(1))
			Expect(agent.reconfig).To(Equal(reconfigArmed))

			agent.writeDoneMarkerFunc = noopMarker
			Expect(reconcile(agent, fakeClient)).To(Succeed())
			Expect(runs).To(Equal(2))
		})

		It("does not start an in-place Run on the same reconcile as a UID change", func() {
			dpu := newTestDPU()
			dpu.Status.Phase = provisioningv1.DPUConfig
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()
			runs := 0
			agent := newAgent(fakeClient, func(_ context.Context, _ *operations.Context) error {
				runs++
				return nil
			})
			agent.reconfig = reconfigArmed
			agent.seenResourceVersion = 50
			agent.optCtx.Options.DPUUID = "old-uid"
			Expect(reconcile(agent, fakeClient)).To(Succeed())
			Expect(runs).To(Equal(0))
			Expect(agent.reconfig).To(Equal(reconfigArmed))
			Expect(agent.seenResourceVersion).To(Equal(uint64(50)))
		})

		It("does not clear an in-flight reconfig when the UID changes", func() {
			dpu := newTestDPU()
			dpu.Status.Phase = provisioningv1.DPUConfig
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()
			runs := 0
			agent := newAgent(fakeClient, func(_ context.Context, _ *operations.Context) error {
				runs++
				return nil
			})
			agent.reconfig = reconfigRunning
			agent.seenResourceVersion = 50
			agent.optCtx.ReapplyConfig = true
			agent.optCtx.Options.DPUUID = "old-uid"
			Expect(reconcile(agent, fakeClient)).To(Succeed())
			Expect(runs).To(Equal(0))
			Expect(agent.reconfig).To(Equal(reconfigRunning))
			Expect(agent.optCtx.ReapplyConfig).To(BeTrue())
			Expect(agent.seenResourceVersion).To(Equal(uint64(50)))
		})

		It("skips a concurrent reconcile while a reconfig Run is in flight", func() {
			dpu := newTestDPU()
			dpu.Status.Phase = provisioningv1.DPUReady
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()
			var runs atomic.Int32
			started := make(chan struct{})
			release := make(chan struct{})
			agent := newAgent(fakeClient, func(_ context.Context, _ *operations.Context) error {
				runs.Add(1)
				close(started)
				<-release
				return nil
			})
			Expect(reconcile(agent, fakeClient)).To(Succeed())
			setDPUConfigPhase(fakeClient)

			done := make(chan struct{})
			go func() {
				defer GinkgoRecover()
				Expect(reconcile(agent, fakeClient)).To(Succeed())
				close(done)
			}()
			Eventually(started).Should(BeClosed())
			Expect(reconcile(agent, fakeClient)).To(Succeed())
			Expect(runs.Load()).To(Equal(int32(1)))
			close(release)
			Eventually(done).Should(BeClosed())
		})
	})
})

// mockOperation is a mock implementation of operations.Operation for testing
type mockOperation struct {
	name                             string
	conditionType                    string
	shouldSkip                       bool
	shouldUpdateStatusBeforeContinue bool
	executeFunc                      func(execCtx context.Context, optCtx *operations.Context) error
}

func (m *mockOperation) Name() string {
	return m.name
}

func (m *mockOperation) ConditionType() string {
	return m.conditionType
}

func (m *mockOperation) ShouldSkip(optCtx *operations.Context) bool {
	return m.shouldSkip
}

func (m *mockOperation) ShouldUpdateStatusBeforeContinue(optCtx *operations.Context) bool {
	return m.shouldUpdateStatusBeforeContinue
}

func (m *mockOperation) Execute(execCtx context.Context, optCtx *operations.Context) error {
	if m.executeFunc != nil {
		return m.executeFunc(execCtx, optCtx)
	}
	return nil
}

func indexOf(values []string, target string) int {
	for i, value := range values {
		if value == target {
			return i
		}
	}
	return -1
}
