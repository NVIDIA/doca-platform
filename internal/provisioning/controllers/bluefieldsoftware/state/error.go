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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// RetryWindow is the maximum time to keep retrying a download after its first error.
	// After this window expires, the BlueFieldSoftware stays in permanent Error state and
	// Downloaded is marked Failure. A recoverable extraction error is not bound by it.
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
		conditions.AddFalse(st.bfs, provisioningv1.BlueFieldSoftwareCondDownloaded,
			conditions.ReasonAwaitingDeletion, "BlueFieldSoftware is being deleted")
		conditions.AddFalse(st.bfs, provisioningv1.BlueFieldSoftwareCondExtracted,
			conditions.ReasonAwaitingDeletion, "BlueFieldSoftware is being deleted")
		return nil
	}

	switch st.classifyError() {
	case retryNow:
		if cleanupErr == nil {
			st.bfs.Status.Phase = provisioningv1.BlueFieldSoftwareDownloading
			conditions.AddFalse(st.bfs, provisioningv1.BlueFieldSoftwareCondDownloaded,
				conditions.ReasonPending, "Retrying download after transient error")
			return nil
		}
	case terminal:
		// The controller has given up on the download, so Downloaded is marked Failure and the
		// Ready summary reports Failure instead of Pending. An extraction Failure is already on
		// Extracted.
		if msg, expired := st.expiredDownloadMessage(); expired {
			conditions.AddFalse(st.bfs, provisioningv1.BlueFieldSoftwareCondDownloaded,
				conditions.ReasonFailure, msg)
		}
		// In the terminal state a re-download cannot be forced without deleting the
		// object, so nothing will reuse the downloaded components. Reclaim storage by
		// removing all of them.
		cleanupErr = errors.Join(cleanupErr, cleanupCompletedComponentFiles(st.bfs))
	case retryLater:
	}

	if cleanupErr != nil {
		return fmt.Errorf("cleanup component artifacts on error: %w", cleanupErr)
	}
	return nil
}

// expiredDownloadMessage returns the Failure message for a download whose retry window has
// expired. It reports false when Downloaded is True or already carries Failure, which need no
// write.
func (st *blueFieldSoftwareErrorState) expiredDownloadMessage() (conditions.ConditionMessage, bool) {
	cond := conditions.Get(st.bfs, provisioningv1.BlueFieldSoftwareCondDownloaded)
	if cond == nil || cond.Status == metav1.ConditionTrue || cond.Reason == string(conditions.ReasonFailure) {
		return "", false
	}
	msg := fmt.Sprintf("Retry window of %s expired", RetryWindow)
	if cond.Message != "" {
		msg = fmt.Sprintf("%s: %s", msg, cond.Message)
	}
	return conditions.ConditionMessage(msg), true
}

// errorOutcome classifies an Error-phase BlueFieldSoftware into mutually exclusive,
// exhaustive outcomes so the retry and terminal-cleanup decisions come from one place.
type errorOutcome int

const (
	// retryLater: recoverable error, but the retry interval has not elapsed yet. Not
	// retried this reconcile and not terminal - preserve completed downloads and requeue.
	retryLater errorOutcome = iota
	// retryNow: recoverable error, past RetryInterval and, for a download, still within
	// RetryWindow.
	retryNow
	// terminal: never retried - a hard failure (ReasonFailure) or a recoverable download error
	// whose RetryWindow has fully elapsed.
	terminal
)

// classifyError decides how the current Error should be handled, based on:
//   - an extraction Failure, which is terminal: the unpack tool rejected the bundle and a
//     re-download cannot fix it,
//   - a recoverable extraction error, which is retried every RetryInterval through Downloading,
//     which finds the cached files and re-extracts,
//   - a download failure: recoverable (ReasonError) vs terminal (ReasonFailure), with
//     RetryInterval as the earliest retry and RetryWindow as the point of giving up.
func (st *blueFieldSoftwareErrorState) classifyError() errorOutcome {
	if extractCond := conditions.Get(st.bfs, provisioningv1.BlueFieldSoftwareCondExtracted); extractCond != nil &&
		extractCond.Reason == string(conditions.ReasonFailure) {
		return terminal
	}
	downloadCond := conditions.Get(st.bfs, provisioningv1.BlueFieldSoftwareCondDownloaded)
	if downloadCond == nil {
		return retryLater
	}
	// Terminal failures require user intervention and are never retried.
	if downloadCond.Reason == string(conditions.ReasonFailure) {
		return terminal
	}
	timeSinceError := time.Since(downloadCond.LastTransitionTime.Time)
	// With the download complete, what failed is the extraction, and only a recoverable error
	// gets this far. It is retried for as long as it lasts: the files are cached, so a retry
	// costs one unpack call. RetryWindow does not apply, because Downloaded turns True on every
	// attempt, so its transition time says when the download finished and not when the
	// extraction first failed.
	downloadComplete := downloadCond.Status == metav1.ConditionTrue
	switch {
	case !downloadComplete && timeSinceError >= RetryWindow:
		return terminal
	case timeSinceError >= RetryInterval:
		return retryNow
	default:
		return retryLater
	}
}
