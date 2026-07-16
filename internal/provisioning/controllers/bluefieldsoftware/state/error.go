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
	"fmt"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// RetryWindow is the maximum time to keep retrying after the first error.
	// After this window expires, the BlueFieldSoftware stays in permanent Error state.
	RetryWindow = 30 * time.Minute
	// RetryInterval is the minimum time to wait before retrying a failed download.
	RetryInterval = time.Minute
)

type blueFieldSoftwareErrorState struct {
	bfs *provisioningv1.BlueFieldSoftware
}

func (st *blueFieldSoftwareErrorState) Handle(ctx context.Context, _ client.Client) error {
	cleanupErr := cleanupInFlightComponentArtifacts(st.bfs)
	if isDeleting(st.bfs) {
		if cleanupErr != nil {
			log.FromContext(ctx).Error(cleanupErr, "failed to cleanup in-flight component artifacts during deletion")
		}
		st.bfs.Status.Phase = provisioningv1.BlueFieldSoftwareDeleting
		conditions.AddFalse(st.bfs, provisioningv1.BlueFieldSoftwareCondError,
			conditions.ReasonAwaitingDeletion, "BlueFieldSoftware is being deleted")
		return nil
	}

	// Recover from transient failures instead of staying permanently in Error. This lets
	// BlueFieldSoftware heal after transient storage or network issues, such as /bfb
	// being wiped by a control-plane node OS revert. Only attempt this when cleanup of
	// in-flight artifacts succeeded, so a retry starts from a clean slate.
	if cleanupErr == nil && st.shouldRetry() {
		st.bfs.Status.Phase = provisioningv1.BlueFieldSoftwareDownloading
		conditions.AddFalse(st.bfs, provisioningv1.BlueFieldSoftwareCondDownloaded,
			conditions.ReasonPending, "Retrying download after transient error")
		conditions.AddFalse(st.bfs, provisioningv1.BlueFieldSoftwareCondError,
			conditions.ReasonPending, "Retry initiated")
		return nil
	}

	conditions.AddTrue(st.bfs, provisioningv1.BlueFieldSoftwareCondError)
	if cleanupErr != nil {
		return fmt.Errorf("cleanup in-flight component artifacts: %w", cleanupErr)
	}
	return nil
}

// shouldRetry determines whether the download should be retried based on:
//   - the failure must be recoverable (ReasonError, not the terminal ReasonFailure),
//   - enough time has passed since the last error (RetryInterval),
//   - we are still within the retry window (RetryWindow from the first error).
//
// Terminal failures (ReasonFailure) require user intervention and are never retried.
func (st *blueFieldSoftwareErrorState) shouldRetry() bool {
	downloadCond := conditions.Get(st.bfs, provisioningv1.BlueFieldSoftwareCondDownloaded)
	if downloadCond == nil {
		return false
	}

	// Only retry recoverable errors (ReasonError) - transient network/storage issues.
	// Terminal failures (ReasonFailure) are left untouched for user intervention.
	if downloadCond.Reason != string(conditions.ReasonError) {
		return false
	}

	timeSinceError := time.Since(downloadCond.LastTransitionTime.Time)
	return timeSinceError >= RetryInterval && timeSinceError < RetryWindow
}
