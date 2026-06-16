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
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
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
		DiscoverPorts: func() ([]pciutil.NICPort, error) {
			return []pciutil.NICPort{
				{Netdev: "p0", PCIAddress: "0000:03:00.0", MSTDevice: "/dev/mst/mt41692_pciconf0"},
				{Netdev: "p1", PCIAddress: "0000:03:00.1", MSTDevice: "/dev/mst/mt41692_pciconf0.1"},
			}, nil
		},
	}
}

var _ = Describe("DPUAgent", func() {
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

		It("should include package installation after static file verification", func() {
			agent := NewDPUAgent(&operations.Context{})
			names := make([]string, 0, len(agent.operations))
			for _, op := range agent.operations {
				names = append(names, op.Name())
			}

			Expect(names).To(ContainElement("Verify Static Files"))
			Expect(names).To(ContainElement("Install Packages"))
			Expect(names).To(ContainElement("Handle Reboot"))
			Expect(names).To(ContainElement("Start Kubelet"))
			Expect(names).To(ContainElement("Report Node Labels"))
			Expect(indexOf(names, "Verify Static Files")).To(BeNumerically("<", indexOf(names, "Install Packages")))
			Expect(indexOf(names, "Install Packages")).To(BeNumerically("<", indexOf(names, "Handle Reboot")))
			Expect(indexOf(names, "Start Kubelet")).To(BeNumerically("<", indexOf(names, "Report Node Labels")))
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
						captured = optCtx.Status.RebootMethod
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

		It("sets RebootMethodDiscovery false when SkipRebootMethodDiscovery is true", func() {
			dpu := newTestDPU()
			fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).WithStatusSubresource(dpu).Build()

			var discovery bool
			optCtx := newTestOptCtx(fakeClient)
			optCtx.Options.SkipRebootMethodDiscovery = true
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
			cond := meta.FindStatusCondition(agent.optCtx.Status.Conditions, condType)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Message).To(Equal("custom success message"))
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
			cond := meta.FindStatusCondition(agent.optCtx.Status.Conditions, condType)
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
			first := meta.FindStatusCondition(agent.optCtx.Status.Conditions, firstCond)
			Expect(first).NotTo(BeNil())
			Expect(first.Message).To(Equal("first message"))
			second := meta.FindStatusCondition(agent.optCtx.Status.Conditions, secondCond)
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
