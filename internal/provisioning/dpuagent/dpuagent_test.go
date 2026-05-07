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
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const testRetryInterval = 1 * time.Millisecond

var _ = Describe("DPUAgent", func() {
	Describe("Run", func() {
		It("should execute operations in order", func() {
			executionOrder := []string{}
			mockOps := []operations.Operation{
				&mockOperation{
					name:          "op1",
					conditionType: "Op1Condition",
					executeFunc: func(execCtx context.Context, optCtx *operations.Context) error {
						executionOrder = append(executionOrder, "op1")
						return nil
					},
				},
				&mockOperation{
					name:          "op2",
					conditionType: "Op2Condition",
					executeFunc: func(execCtx context.Context, optCtx *operations.Context) error {
						executionOrder = append(executionOrder, "op2")
						return nil
					},
				},
				&mockOperation{
					name:          "op3",
					conditionType: "Op3Condition",
					executeFunc: func(execCtx context.Context, optCtx *operations.Context) error {
						executionOrder = append(executionOrder, "op3")
						return nil
					},
				},
			}

			agent := &DPUAgent{
				retryInterval: testRetryInterval,
				optCtx: &operations.Context{
					Client: &mockClient{},
					NSNIC:  &hostutil.Device{Address: "0000:03:00", NumOfPFs: 2},
				},
				operations: mockOps,
			}

			err := agent.Run(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(executionOrder).To(Equal([]string{"op1", "op2", "op3"}))
		})

		It("initializes RebootMethod to Unknown so stale values from a previous session are overwritten", func() {
			var captured *provisioningv1.RebootMethodType
			mockOps := []operations.Operation{
				&mockOperation{
					name:          "op1",
					conditionType: "Op1Condition",
					executeFunc: func(execCtx context.Context, optCtx *operations.Context) error {
						captured = optCtx.Status.RebootMethod
						return nil
					},
				},
			}
			agent := &DPUAgent{
				retryInterval: testRetryInterval,
				optCtx: &operations.Context{
					Client: &mockClient{},
					NSNIC:  &hostutil.Device{Address: "0000:03:00", NumOfPFs: 2},
				},
				operations: mockOps,
			}
			Expect(agent.Run(ctx)).To(Succeed())
			Expect(captured).NotTo(BeNil())
			Expect(*captured).To(Equal(provisioningv1.RebootMethodUnknown))
		})

		It("sets RebootMethodDiscovery on the operation context before operations run", func() {
			var discovery bool
			mockOps := []operations.Operation{
				&mockOperation{
					name:          "op1",
					conditionType: "Op1Condition",
					executeFunc: func(execCtx context.Context, optCtx *operations.Context) error {
						discovery = optCtx.RebootMethodDiscovery
						return nil
					},
				},
			}
			agent := &DPUAgent{
				retryInterval: testRetryInterval,
				optCtx: &operations.Context{
					Client: &mockClient{},
					NSNIC:  &hostutil.Device{Address: "0000:03:00", NumOfPFs: 2},
				},
				operations: mockOps,
			}
			Expect(agent.Run(ctx)).To(Succeed())
			Expect(discovery).To(BeFalse(), "discovery is false when MFT tools are missing or below min version")
		})

		It("sets RebootMethodDiscovery true when rebootMethodDiscoveryFunc returns true", func() {
			var discovery bool
			agent := &DPUAgent{
				retryInterval: testRetryInterval,
				rebootMethodDiscoveryFunc: func(context.Context) bool {
					return true
				},
				optCtx: &operations.Context{
					Client: &mockClient{},
					NSNIC:  &hostutil.Device{Address: "0000:03:00", NumOfPFs: 2},
				},
				operations: []operations.Operation{
					&mockOperation{
						name:          "op1",
						conditionType: "Op1Condition",
						executeFunc: func(execCtx context.Context, optCtx *operations.Context) error {
							discovery = optCtx.RebootMethodDiscovery
							return nil
						},
					},
				},
			}
			Expect(agent.Run(ctx)).To(Succeed())
			Expect(discovery).To(BeTrue())
		})

		It("sets RebootMethodDiscovery false when SkipRebootMethodDiscovery is true", func() {
			var discovery bool
			agent := &DPUAgent{
				retryInterval: testRetryInterval,
				rebootMethodDiscoveryFunc: func(context.Context) bool {
					return true
				},
				optCtx: &operations.Context{
					Client: &mockClient{},
					NSNIC:  &hostutil.Device{Address: "0000:03:00", NumOfPFs: 2},
					Options: opts.Options{
						SkipRebootMethodDiscovery: true,
					},
				},
				operations: []operations.Operation{
					&mockOperation{
						name:          "op1",
						conditionType: "Op1Condition",
						executeFunc: func(execCtx context.Context, optCtx *operations.Context) error {
							discovery = optCtx.RebootMethodDiscovery
							return nil
						},
					},
				},
			}
			Expect(agent.Run(ctx)).To(Succeed())
			Expect(discovery).To(BeFalse())
		})

		It("should skip operations that should be skipped", func() {
			executionOrder := []string{}
			mockOps := []operations.Operation{
				&mockOperation{
					name:          "op1",
					conditionType: "Op1Condition",
					executeFunc: func(execCtx context.Context, optCtx *operations.Context) error {
						executionOrder = append(executionOrder, "op1")
						return nil
					},
				},
				&mockOperation{
					name:          "op2-skipped",
					conditionType: "Op2Condition",
					shouldSkip:    true,
					executeFunc: func(execCtx context.Context, optCtx *operations.Context) error {
						executionOrder = append(executionOrder, "op2-skipped")
						return nil
					},
				},
				&mockOperation{
					name:          "op3",
					conditionType: "Op3Condition",
					executeFunc: func(execCtx context.Context, optCtx *operations.Context) error {
						executionOrder = append(executionOrder, "op3")
						return nil
					},
				},
			}

			agent := &DPUAgent{
				retryInterval: testRetryInterval,
				optCtx: &operations.Context{
					Client: &mockClient{},
					NSNIC:  &hostutil.Device{Address: "0000:03:00", NumOfPFs: 2},
				},
				operations: mockOps,
			}

			err := agent.Run(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(executionOrder).To(Equal([]string{"op1", "op3"}))
		})

		It("should retry failed operations until success", func() {
			attempts := 0
			mockOps := []operations.Operation{
				&mockOperation{
					name:          "flaky-op",
					conditionType: "FlakyOpCondition",
					executeFunc: func(execCtx context.Context, optCtx *operations.Context) error {
						attempts++
						if attempts < 3 {
							return fmt.Errorf("temporary error")
						}
						return nil
					},
				},
			}

			agent := &DPUAgent{
				retryInterval: testRetryInterval,
				optCtx: &operations.Context{
					Client: &mockClient{},
					NSNIC:  &hostutil.Device{Address: "0000:03:00", NumOfPFs: 2},
				},
				operations: mockOps,
			}

			err := agent.Run(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(attempts).To(Equal(3))
		})

		It("uses CondMessage for the success condition message", func() {
			const condType = "Op1Condition"
			agent := &DPUAgent{
				retryInterval: testRetryInterval,
				optCtx: &operations.Context{
					Client: &mockClient{},
					NSNIC:  &hostutil.Device{Address: "0000:03:00", NumOfPFs: 2},
				},
				operations: []operations.Operation{
					&mockOperation{
						name:          "op1",
						conditionType: condType,
						executeFunc: func(execCtx context.Context, optCtx *operations.Context) error {
							optCtx.CondMessage = "custom success message"
							return nil
						},
					},
				},
			}

			Expect(agent.Run(ctx)).To(Succeed())
			cond := meta.FindStatusCondition(agent.optCtx.Status.Conditions, condType)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Message).To(Equal("custom success message"))
		})

		It("clears CondMessage before each retry attempt", func() {
			const condType = "RetryCondition"
			attempts := 0
			seen := []string{}
			agent := &DPUAgent{
				retryInterval: testRetryInterval,
				optCtx: &operations.Context{
					Client: &mockClient{},
					NSNIC:  &hostutil.Device{Address: "0000:03:00", NumOfPFs: 2},
				},
				operations: []operations.Operation{
					&mockOperation{
						name:          "retry-op",
						conditionType: condType,
						executeFunc: func(execCtx context.Context, optCtx *operations.Context) error {
							seen = append(seen, optCtx.CondMessage)
							attempts++
							if attempts == 1 {
								optCtx.CondMessage = "stale message"
								return fmt.Errorf("temporary error")
							}
							optCtx.CondMessage = "fresh message"
							return nil
						},
					},
				},
			}

			Expect(agent.Run(ctx)).To(Succeed())
			Expect(seen).To(Equal([]string{"", ""}))
			cond := meta.FindStatusCondition(agent.optCtx.Status.Conditions, condType)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Message).To(Equal("fresh message"))
		})

		It("does not leak CondMessage to the next operation", func() {
			const firstCond = "FirstCondition"
			const secondCond = "SecondCondition"
			secondSeen := "unset"
			agent := &DPUAgent{
				retryInterval: testRetryInterval,
				optCtx: &operations.Context{
					Client: &mockClient{},
					NSNIC:  &hostutil.Device{Address: "0000:03:00", NumOfPFs: 2},
				},
				operations: []operations.Operation{
					&mockOperation{
						name:          "op1",
						conditionType: firstCond,
						executeFunc: func(execCtx context.Context, optCtx *operations.Context) error {
							optCtx.CondMessage = "first message"
							return nil
						},
					},
					&mockOperation{
						name:          "op2",
						conditionType: secondCond,
						executeFunc: func(execCtx context.Context, optCtx *operations.Context) error {
							secondSeen = optCtx.CondMessage
							return nil
						},
					},
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
			statusUpdateCount := 0
			failingOpAttempts := 0

			mockOps := []operations.Operation{
				&mockOperation{
					name:          "failing-op",
					conditionType: "FailingOpCondition",
					executeFunc: func(execCtx context.Context, optCtx *operations.Context) error {
						defer func() {
							failingOpAttempts++
						}()
						if failingOpAttempts < 3 {
							return fmt.Errorf("temporary error")
						}
						return nil
					},
				},
				&mockOperation{
					name:                             "status-update-op",
					conditionType:                    "StatusUpdateCondition",
					shouldUpdateStatusBeforeContinue: true,
					executeFunc: func(execCtx context.Context, optCtx *operations.Context) error {
						return nil
					},
				},
			}

			agent := &DPUAgent{
				retryInterval: testRetryInterval,
				optCtx: &operations.Context{
					Client: &mockClient{
						updateStatusFunc: func(ctx context.Context, status provisioningv1.AgentStatus) error {
							statusUpdateCount++
							return nil
						},
					},
					NSNIC: &hostutil.Device{Address: "0000:03:00", NumOfPFs: 2},
				},
				operations: mockOps,
			}

			err := agent.Run(ctx)
			Expect(err).NotTo(HaveOccurred())
			// Status updates:
			// - 3 times for failing-op failures (attempts 0, 1 and 2)
			// - 1 time for status-update-op (shouldUpdateStatusBeforeContinue=true)
			// - 1 time at the end of Run
			Expect(statusUpdateCount).To(BeNumerically(">=", 5))
		})

		It("should abort and return error when context is canceled and not execute subsequent operations", func() {
			cancelCtx, cancelFunc := context.WithCancel(ctx)
			attempts := 0
			secondOpExecuted := false

			agent := &DPUAgent{
				retryInterval: testRetryInterval,
				optCtx: &operations.Context{
					Client: &mockClient{},
					NSNIC:  &hostutil.Device{Address: "0000:03:00", NumOfPFs: 2},
				},
				operations: []operations.Operation{
					&mockOperation{
						name:          "blocking-op",
						conditionType: "BlockingOpCondition",
						executeFunc: func(execCtx context.Context, optCtx *operations.Context) error {
							attempts++
							if attempts >= 2 {
								cancelFunc()
							}
							return fmt.Errorf("persistent error")
						},
					},
					&mockOperation{
						name:          "subsequent-op",
						conditionType: "SubsequentOpCondition",
						executeFunc: func(execCtx context.Context, optCtx *operations.Context) error {
							secondOpExecuted = true
							return nil
						},
					},
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

// mockClient is a mock implementation of client.Client for testing
type mockClient struct {
	updateStatusFunc func(ctx context.Context, status provisioningv1.AgentStatus) error
	getObjectFunc    func(ctx context.Context, namespace, name string, obj client.Object) error
	healthCheckFunc  func() error
}

func (m *mockClient) UpdateStatus(ctx context.Context, status provisioningv1.AgentStatus) error {
	if m.updateStatusFunc != nil {
		return m.updateStatusFunc(ctx, status)
	}
	return nil
}

func (m *mockClient) GetObject(ctx context.Context, namespace, name string, obj client.Object) error {
	if m.getObjectFunc != nil {
		return m.getObjectFunc(ctx, namespace, name, obj)
	}
	return nil
}

func (m *mockClient) HealthCheck() error {
	if m.healthCheckFunc != nil {
		return m.healthCheckFunc()
	}
	return nil
}
