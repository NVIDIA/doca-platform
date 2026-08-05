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
	"errors"
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

	switch st.classifyError() {
	case retryNow:
		if cleanupErr == nil {
			st.bfs.Status.Phase = provisioningv1.BlueFieldSoftwareDownloading
			conditions.AddFalse(st.bfs, provisioningv1.BlueFieldSoftwareCondDownloaded,
				conditions.ReasonPending, "Retrying download after transient error")
			conditions.AddFalse(st.bfs, provisioningv1.BlueFieldSoftwareCondError,
				conditions.ReasonPending, "Retry initiated")
			return nil
		}
	case terminal:
		// In the terminal state a re-download cannot be forced without deleting the
		// object, so nothing will reuse the downloaded components. Reclaim storage by
		// removing all of them.
		cleanupErr = errors.Join(cleanupErr, cleanupCompletedComponentFiles(st.bfs))
	case retryLater:
	}

	conditions.AddTrue(st.bfs, provisioningv1.BlueFieldSoftwareCondError)
	if cleanupErr != nil {
		return fmt.Errorf("cleanup component artifacts on error: %w", cleanupErr)
	}
	return nil
}

// errorOutcome classifies an Error-phase BlueFieldSoftware into mutually exclusive,
// exhaustive outcomes so the retry and terminal-cleanup decisions come from one place.
type errorOutcome int

const (
	// retryLater: recoverable error, but the retry interval has not elapsed yet. Not
	// retried this reconcile and not terminal - preserve completed downloads and requeue.
	retryLater errorOutcome = iota
	// retryNow: recoverable error, past RetryInterval and still within RetryWindow.
	retryNow
	// terminal: never retried - a hard failure (ReasonFailure) or a recoverable error
	// whose RetryWindow has fully elapsed.
	terminal
)

// classifyError decides how the current Error should be handled, based on:
//   - the failure kind: recoverable (ReasonError) vs terminal (ReasonFailure),
//   - time since the error: RetryInterval (earliest retry) and RetryWindow (giving up).
func (st *blueFieldSoftwareErrorState) classifyError() errorOutcome {
	downloadCond := conditions.Get(st.bfs, provisioningv1.BlueFieldSoftwareCondDownloaded)
	if downloadCond == nil {
		return retryLater
	}
	// Terminal failures require user intervention and are never retried.
	if downloadCond.Reason == string(conditions.ReasonFailure) {
		return terminal
	}
	timeSinceError := time.Since(downloadCond.LastTransitionTime.Time)
	switch {
	case timeSinceError >= RetryWindow:
		return terminal
	case timeSinceError >= RetryInterval:
		return retryNow
	default:
		return retryLater
	}
}
