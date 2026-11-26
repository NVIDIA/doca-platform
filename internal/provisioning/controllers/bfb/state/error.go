/*
Copyright 2024 NVIDIA

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
	"fmt"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// RetryWindow is the maximum time to keep retrying after the first error.
	// After this window expires, the BFB stays in permanent Error state and the
	// Downloaded milestone is promoted from Error to Failure.
	RetryWindow = 6 * time.Hour
	// RetryInterval is the minimum time to wait before retrying a failed download.
	RetryInterval = 10 * time.Minute
)

type bfbErrorState struct {
	bfb *provisioningv1.BFB
}

func (st *bfbErrorState) Handle(context.Context, client.Client) error {
	if isDeleting(st.bfb) {
		st.bfb.Status.Phase = provisioningv1.BFBDeleting
		conditions.AddFalse(st.bfb, provisioningv1.BFBCondDownloaded,
			conditions.ReasonAwaitingDeletion, "BFB is being deleted")
		return nil
	}

	// The Error phase implies a failed download. If the patch that recorded the failure was lost,
	// neither the retry nor the promotion would ever act and the BFB would stay in Error for good,
	// so the failure is re-asserted as recoverable and the next attempt records the real outcome.
	if !st.downloadFailureRecorded() {
		conditions.AddFalse(st.bfb, provisioningv1.BFBCondDownloaded,
			conditions.ReasonError, "Download failed and the failure detail was lost, retrying")
	}

	// Check if we should retry the download
	if st.shouldRetry() {
		// Transition back to Downloading to retry
		st.bfb.Status.Phase = provisioningv1.BFBDownloading
		conditions.AddFalse(st.bfb, provisioningv1.BFBCondDownloaded,
			conditions.ReasonPending, "Retrying download after transient error")
		return nil
	}

	// The controller no longer retries once RetryWindow has passed, so the recoverable Error is
	// promoted to a terminal Failure and the Ready summary reports Failure instead of Pending.
	if lastError, expired := st.expiredError(); expired {
		msg := fmt.Sprintf("Retry window of %s expired: %s", RetryWindow, lastError)
		conditions.AddFalse(st.bfb, provisioningv1.BFBCondDownloaded,
			conditions.ReasonFailure, conditions.ConditionMessage(msg))
	}
	return nil
}

// downloadFailureRecorded reports whether the Downloaded milestone carries the failed download,
// as a recoverable Error or as a terminal Failure.
func (st *bfbErrorState) downloadFailureRecorded() bool {
	downloadCond := conditions.Get(st.bfb, provisioningv1.BFBCondDownloaded)
	return downloadCond != nil && (downloadCond.Reason == string(conditions.ReasonError) ||
		downloadCond.Reason == string(conditions.ReasonFailure))
}

// expiredError returns the message of a recoverable Error on the Downloaded milestone that has
// been standing for RetryWindow or longer.
func (st *bfbErrorState) expiredError() (string, bool) {
	downloadCond := conditions.Get(st.bfb, provisioningv1.BFBCondDownloaded)
	if downloadCond == nil || downloadCond.Reason != string(conditions.ReasonError) ||
		time.Since(downloadCond.LastTransitionTime.Time) < RetryWindow {
		return "", false
	}
	return downloadCond.Message, true
}

// shouldRetry determines if the download should be retried based on:
// - The error must be recoverable (ReasonError, not ReasonFailure)
// - Enough time has passed since the last error (RetryInterval)
// - We're still within the retry window (RetryWindow from first error)
func (st *bfbErrorState) shouldRetry() bool {
	downloadCond := conditions.Get(st.bfb, provisioningv1.BFBCondDownloaded)
	if downloadCond == nil {
		return false
	}

	// Only retry for recoverable errors (ReasonError) - transient network/server issues
	// Don't retry for terminal failures (ReasonFailure) - user intervention required
	// (e.g., filesystem errors, invalid BFB file at URL)
	if downloadCond.Reason != string(conditions.ReasonError) {
		return false
	}

	timeSinceError := time.Since(downloadCond.LastTransitionTime.Time)

	// Check if we're still within the retry window and enough time has passed for next retry
	return timeSinceError >= RetryInterval && timeSinceError < RetryWindow
}
