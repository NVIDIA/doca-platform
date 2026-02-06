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

package state

import (
	"context"
	"testing"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBFBState(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "BFB State Suite")
}

// setConditionWithTime sets the BFBCondDownloaded condition with a specific LastTransitionTime
func setConditionWithTime(bfb *provisioningv1.BFB, reason conditions.ConditionReason, message string, transitionTime time.Time) {
	// First add the condition
	conditions.AddFalse(bfb, provisioningv1.BFBCondDownloaded, reason, conditions.ConditionMessage(message))
	// Then update the LastTransitionTime directly in the slice
	for i := range bfb.Status.Conditions {
		if bfb.Status.Conditions[i].Type == string(provisioningv1.BFBCondDownloaded) {
			bfb.Status.Conditions[i].LastTransitionTime = metav1.NewTime(transitionTime)
			break
		}
	}
}

var _ = Describe("BFB Error State Retry Logic", func() {
	var (
		ctx context.Context
		bfb *provisioningv1.BFB
	)

	BeforeEach(func() {
		ctx = context.Background()
		bfb = &provisioningv1.BFB{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-bfb",
				Namespace: "default",
			},
			Spec: provisioningv1.BFBSpec{
				URL: "http://example.com/test.bfb",
			},
			Status: provisioningv1.BFBStatus{
				Phase:    provisioningv1.BFBError,
				FileName: "test.bfb",
			},
		}
	})

	Describe("shouldRetry", func() {
		Context("when Downloaded condition has ReasonError (recoverable)", func() {
			It("should NOT retry if RetryInterval has not passed", func() {
				// Set error condition with recent timestamp (less than RetryInterval ago)
				setConditionWithTime(bfb,
					conditions.ReasonError, "connection timeout", time.Now().Add(-5*time.Minute))

				st := &bfbErrorState{bfb: bfb}
				Expect(st.shouldRetry()).To(BeFalse(),
					"should not retry before RetryInterval (10m) has passed")
			})

			It("should retry after RetryInterval has passed but within RetryWindow", func() {
				// Set error condition with timestamp > RetryInterval but < RetryWindow ago
				setConditionWithTime(bfb,
					conditions.ReasonError, "connection timeout", time.Now().Add(-15*time.Minute))

				st := &bfbErrorState{bfb: bfb}
				Expect(st.shouldRetry()).To(BeTrue(),
					"should retry after RetryInterval (10m) but within RetryWindow (6h)")
			})

			It("should NOT retry after RetryWindow has expired", func() {
				// Set error condition with timestamp > RetryWindow ago
				setConditionWithTime(bfb,
					conditions.ReasonError, "connection timeout", time.Now().Add(-7*time.Hour))

				st := &bfbErrorState{bfb: bfb}
				Expect(st.shouldRetry()).To(BeFalse(),
					"should not retry after RetryWindow (6h) has expired")
			})

			It("should retry at exactly RetryInterval boundary", func() {
				setConditionWithTime(bfb,
					conditions.ReasonError, "connection timeout", time.Now().Add(-RetryInterval))

				st := &bfbErrorState{bfb: bfb}
				Expect(st.shouldRetry()).To(BeTrue(),
					"should retry at exactly RetryInterval boundary")
			})

			It("should NOT retry at exactly RetryWindow boundary", func() {
				setConditionWithTime(bfb,
					conditions.ReasonError, "connection timeout", time.Now().Add(-RetryWindow))

				st := &bfbErrorState{bfb: bfb}
				Expect(st.shouldRetry()).To(BeFalse(),
					"should not retry at exactly RetryWindow boundary (>= check)")
			})
		})

		Context("when Downloaded condition has ReasonFailure (terminal)", func() {
			It("should NOT retry for terminal failures regardless of timing", func() {
				// ReasonFailure indicates user intervention required (e.g., invalid BFB file)
				setConditionWithTime(bfb,
					conditions.ReasonFailure, "invalid BFB file format", time.Now().Add(-30*time.Minute))

				st := &bfbErrorState{bfb: bfb}
				Expect(st.shouldRetry()).To(BeFalse(),
					"should never retry terminal failures (ReasonFailure)")
			})
		})

		Context("when Downloaded condition is missing", func() {
			It("should NOT retry if condition is nil", func() {
				// No conditions set
				bfb.Status.Conditions = nil

				st := &bfbErrorState{bfb: bfb}
				Expect(st.shouldRetry()).To(BeFalse(),
					"should not retry when Downloaded condition is missing")
			})
		})

		Context("with other Reason values", func() {
			It("should NOT retry for ReasonPending", func() {
				setConditionWithTime(bfb,
					conditions.ReasonPending, "download pending", time.Now().Add(-30*time.Minute))

				st := &bfbErrorState{bfb: bfb}
				Expect(st.shouldRetry()).To(BeFalse(),
					"should not retry for non-Error reasons")
			})
		})
	})

	Describe("Handle", func() {
		Context("when retry conditions are met", func() {
			It("should transition from Error to Downloading", func() {
				bfb.Status.Phase = provisioningv1.BFBError
				setConditionWithTime(bfb,
					conditions.ReasonError, "temporary network error", time.Now().Add(-15*time.Minute))

				st := &bfbErrorState{bfb: bfb}
				err := st.Handle(ctx, nil)

				Expect(err).NotTo(HaveOccurred())
				Expect(bfb.Status.Phase).To(Equal(provisioningv1.BFBDownloading),
					"should transition to Downloading phase for retry")

				// Verify conditions are updated for retry
				downloadedCond := conditions.Get(bfb, provisioningv1.BFBCondDownloaded)
				Expect(downloadedCond.Status).To(Equal(metav1.ConditionFalse))
				Expect(downloadedCond.Reason).To(Equal(string(conditions.ReasonPending)))
				Expect(downloadedCond.Message).To(ContainSubstring("Retrying"))

				errorCond := conditions.Get(bfb, provisioningv1.BFBCondError)
				Expect(errorCond.Status).To(Equal(metav1.ConditionFalse))
				Expect(errorCond.Reason).To(Equal(string(conditions.ReasonPending)))
			})
		})

		Context("when retry conditions are NOT met", func() {
			It("should stay in Error state with Error condition True", func() {
				bfb.Status.Phase = provisioningv1.BFBError
				setConditionWithTime(bfb,
					conditions.ReasonFailure, "invalid BFB file", time.Now().Add(-15*time.Minute))

				st := &bfbErrorState{bfb: bfb}
				err := st.Handle(ctx, nil)

				Expect(err).NotTo(HaveOccurred())
				Expect(bfb.Status.Phase).To(Equal(provisioningv1.BFBError),
					"should remain in Error phase for terminal failures")

				errorCond := conditions.Get(bfb, provisioningv1.BFBCondError)
				Expect(errorCond.Status).To(Equal(metav1.ConditionTrue),
					"Error condition should be True for permanent error")
			})

			It("should stay in Error state when RetryWindow expired", func() {
				bfb.Status.Phase = provisioningv1.BFBError
				setConditionWithTime(bfb,
					conditions.ReasonError, "network error", time.Now().Add(-7*time.Hour))

				st := &bfbErrorState{bfb: bfb}
				err := st.Handle(ctx, nil)

				Expect(err).NotTo(HaveOccurred())
				Expect(bfb.Status.Phase).To(Equal(provisioningv1.BFBError),
					"should remain in Error phase after RetryWindow expires")

				errorCond := conditions.Get(bfb, provisioningv1.BFBCondError)
				Expect(errorCond.Status).To(Equal(metav1.ConditionTrue))
			})
		})

		Context("when BFB is being deleted", func() {
			It("should transition to Deleting state regardless of retry status", func() {
				bfb.Status.Phase = provisioningv1.BFBError
				bfb.DeletionTimestamp = &metav1.Time{Time: time.Now()}
				setConditionWithTime(bfb,
					conditions.ReasonError, "network error", time.Now().Add(-15*time.Minute))

				st := &bfbErrorState{bfb: bfb}
				err := st.Handle(ctx, nil)

				Expect(err).NotTo(HaveOccurred())
				Expect(bfb.Status.Phase).To(Equal(provisioningv1.BFBDeleting),
					"should transition to Deleting when deletion requested")
			})
		})
	})

	Describe("Retry Constants", func() {
		It("should have expected retry window of 6 hours", func() {
			Expect(RetryWindow).To(Equal(6 * time.Hour))
		})

		It("should have expected retry interval of 10 minutes", func() {
			Expect(RetryInterval).To(Equal(10 * time.Minute))
		})
	})
})

var _ = Describe("BFB Error State - Full Lifecycle Scenarios", func() {
	var (
		ctx context.Context
		bfb *provisioningv1.BFB
	)

	BeforeEach(func() {
		ctx = context.Background()
		bfb = &provisioningv1.BFB{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-bfb",
				Namespace: "default",
			},
			Spec: provisioningv1.BFBSpec{
				URL: "http://example.com/test.bfb",
			},
			Status: provisioningv1.BFBStatus{
				Phase:    provisioningv1.BFBError,
				FileName: "test.bfb",
			},
		}
	})

	Describe("Scenario: Downloading -> Error -> Downloading (retry) -> Ready", func() {
		It("should allow recovery through retry mechanism", func() {
			// Step 1: Start in Error state with recoverable error
			bfb.Status.Phase = provisioningv1.BFBError
			setConditionWithTime(bfb,
				conditions.ReasonError, "temporary server unavailable", time.Now().Add(-12*time.Minute))

			// Step 2: Handle should trigger retry
			st := &bfbErrorState{bfb: bfb}
			err := st.Handle(ctx, nil)

			Expect(err).NotTo(HaveOccurred())
			Expect(bfb.Status.Phase).To(Equal(provisioningv1.BFBDownloading),
				"should transition to Downloading for retry")

			// Verify retry message
			downloadedCond := conditions.Get(bfb, provisioningv1.BFBCondDownloaded)
			Expect(downloadedCond.Message).To(ContainSubstring("Retrying download"))
		})
	})

	Describe("Scenario: Multiple retry attempts within window", func() {
		It("should allow multiple retries as long as within RetryWindow", func() {
			// Simulate multiple retry cycles
			retryTimes := []time.Duration{
				-15 * time.Minute,             // 1st retry at 15 min
				-30 * time.Minute,             // 2nd retry at 30 min
				-1 * time.Hour,                // 3rd retry at 1 hour
				-3 * time.Hour,                // 4th retry at 3 hours
				-5*time.Hour - 50*time.Minute, // 5th retry at 5h50m (still within 6h window)
			}

			for i, elapsed := range retryTimes {
				// Reset BFB state for each iteration
				bfb.Status.Phase = provisioningv1.BFBError
				bfb.Status.Conditions = nil
				setConditionWithTime(bfb,
					conditions.ReasonError, "network timeout", time.Now().Add(elapsed))

				st := &bfbErrorState{bfb: bfb}
				Expect(st.shouldRetry()).To(BeTrue(),
					"retry #%d at %v should be allowed within RetryWindow", i+1, elapsed)
			}
		})
	})

	Describe("Scenario: Permanent error after RetryWindow expires", func() {
		It("should stop retrying after 6 hour window", func() {
			bfb.Status.Phase = provisioningv1.BFBError
			setConditionWithTime(bfb,
				conditions.ReasonError, "server unreachable", time.Now().Add(-6*time.Hour-1*time.Minute))

			st := &bfbErrorState{bfb: bfb}
			err := st.Handle(ctx, nil)

			Expect(err).NotTo(HaveOccurred())
			Expect(bfb.Status.Phase).To(Equal(provisioningv1.BFBError),
				"should remain in Error state after RetryWindow expires")

			// Error condition should be permanently set
			errorCond := conditions.Get(bfb, provisioningv1.BFBCondError)
			Expect(errorCond.Status).To(Equal(metav1.ConditionTrue),
				"Error condition should be permanently True")
		})
	})

	Describe("Scenario: Terminal failure - no retry", func() {
		It("should never retry for filesystem errors (ReasonFailure)", func() {
			bfb.Status.Phase = provisioningv1.BFBError
			setConditionWithTime(bfb,
				conditions.ReasonFailure, "permission denied: /bfb-cache/test.bfb", time.Now().Add(-15*time.Minute))

			st := &bfbErrorState{bfb: bfb}
			Expect(st.shouldRetry()).To(BeFalse(),
				"should never retry terminal failures requiring user intervention")

			err := st.Handle(ctx, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(bfb.Status.Phase).To(Equal(provisioningv1.BFBError))
		})

		It("should never retry for invalid BFB file (ReasonFailure)", func() {
			bfb.Status.Phase = provisioningv1.BFBError
			setConditionWithTime(bfb,
				conditions.ReasonFailure, "failed to parse BFB version: invalid format", time.Now().Add(-2*time.Hour))

			st := &bfbErrorState{bfb: bfb}
			Expect(st.shouldRetry()).To(BeFalse(),
				"invalid BFB file requires user to provide correct URL")
		})
	})

	Describe("Scenario: Distinguishing recoverable vs terminal errors", func() {
		DescribeTable("error classification",
			func(reason conditions.ConditionReason, message string, shouldRetryExpected bool) {
				bfb.Status.Phase = provisioningv1.BFBError
				bfb.Status.Conditions = nil
				setConditionWithTime(bfb, reason, message, time.Now().Add(-20*time.Minute))

				st := &bfbErrorState{bfb: bfb}
				Expect(st.shouldRetry()).To(Equal(shouldRetryExpected))
			},
			// Recoverable errors (ReasonError) - should retry
			Entry("network timeout", conditions.ReasonError, "connection timeout", true),
			Entry("server 500", conditions.ReasonError, "HTTP 500 Internal Server Error", true),
			Entry("DNS failure", conditions.ReasonError, "DNS lookup failed", true),
			Entry("connection refused", conditions.ReasonError, "connection refused", true),
			Entry("download interrupted", conditions.ReasonError, "download interrupted: connection reset", true),

			// Terminal failures (ReasonFailure) - should NOT retry
			Entry("filesystem permission", conditions.ReasonFailure, "permission denied", false),
			Entry("disk full", conditions.ReasonFailure, "no space left on device", false),
			Entry("invalid BFB format", conditions.ReasonFailure, "invalid BFB file format", false),
			Entry("version parse error", conditions.ReasonFailure, "failed to parse BFB version", false),
		)
	})
})
