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
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const testRetryInterval = 1 * time.Millisecond

var _ = Describe("DPUAgent", func() {
	Describe("Run", func() {
		It("should return error if context is nil", func() {
			agent := &DPUAgent{
				optCtx:     nil,
				operations: []operations.Operation{},
			}
			err := agent.Run(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("context is nil"))
		})

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
				},
				operations: mockOps,
			}

			err := agent.Run(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(executionOrder).To(Equal([]string{"op1", "op2", "op3"}))
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
				},
				operations: mockOps,
			}

			err := agent.Run(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(attempts).To(Equal(3))
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
