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
	"os"
	"path/filepath"
	"testing"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	butil "github.com/nvidia/doca-platform/internal/provisioning/controllers/bluefieldsoftware/util"
	"github.com/nvidia/doca-platform/pkg/conditions"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	testPldmFwBundleURL  = "https://example.com/fw.fwpkg"
	testPldmFwBundlePSID = "MT_0000000001"
)

// writeCompletedFwBundle creates a fully-downloaded DPU PLDM bundle file on disk and
// records its path in status, mirroring a component that finished before a sibling download failed.
// It returns the on-disk path.
func writeCompletedFwBundle(t *testing.T, bfs *provisioningv1.BlueFieldSoftware) string {
	t.Helper()
	unit := componentInfo{
		URL:           testPldmFwBundleURL,
		ComponentType: butil.ComponentTypeFwBundle,
		Key:           testPldmFwBundlePSID,
	}
	destPath := componentDestinationPath(unit.ComponentType, componentFileName(bfs, unit))
	require.NoError(t, os.MkdirAll(filepath.Dir(destPath), 0755))
	require.NoError(t, os.WriteFile(destPath, []byte("completed fw bundle"), 0644))
	setDownloadedComponentPath(bfs, unit.ComponentType, unit.Key, destPath)
	return destPath
}

// bfsInErrorWithDownloadedCondition builds a BlueFieldSoftware in the Error phase whose
// Downloaded condition carries the given reason and transitioned at the given time.
func bfsInErrorWithDownloadedCondition(reason conditions.ConditionReason, transitioned time.Time) *provisioningv1.BlueFieldSoftware {
	return &provisioningv1.BlueFieldSoftware{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bf4-software",
			Namespace: "dpf-operator-system",
		},
		Spec: provisioningv1.BlueFieldSpec{
			OsIso: testBF4OsIsoURL,
		},
		Status: provisioningv1.BlueFieldSoftwareStatus{
			Phase: provisioningv1.BlueFieldSoftwareError,
			Conditions: []metav1.Condition{
				{
					Type:               string(provisioningv1.BlueFieldSoftwareCondDownloaded),
					Status:             metav1.ConditionFalse,
					Reason:             string(reason),
					LastTransitionTime: metav1.NewTime(transitioned),
				},
			},
		},
	}
}

func TestBlueFieldSoftwareErrorState_RetriesRecoverableError(t *testing.T) {
	defer withTestBFBBaseDir(t)()

	bfs := bfsInErrorWithDownloadedCondition(conditions.ReasonError, time.Now().Add(-(RetryInterval + time.Minute)))

	st := &blueFieldSoftwareErrorState{bfs: bfs}
	require.NoError(t, st.Handle(context.Background(), nil))

	assert.Equal(t, provisioningv1.BlueFieldSoftwareDownloading, bfs.Status.Phase)

	downloaded := conditions.Get(bfs, provisioningv1.BlueFieldSoftwareCondDownloaded)
	require.NotNil(t, downloaded)
	assert.Equal(t, metav1.ConditionFalse, downloaded.Status)
	assert.Equal(t, string(conditions.ReasonPending), downloaded.Reason)
}

func TestBlueFieldSoftwareErrorState_NoRetryOnTerminalFailure(t *testing.T) {
	defer withTestBFBBaseDir(t)()

	bfs := bfsInErrorWithDownloadedCondition(conditions.ReasonFailure, time.Now().Add(-(RetryInterval + time.Minute)))

	st := &blueFieldSoftwareErrorState{bfs: bfs}
	require.NoError(t, st.Handle(context.Background(), nil))

	assert.Equal(t, provisioningv1.BlueFieldSoftwareError, bfs.Status.Phase)

	downloaded := conditions.Get(bfs, provisioningv1.BlueFieldSoftwareCondDownloaded)
	require.NotNil(t, downloaded)
	assert.Equal(t, string(conditions.ReasonFailure), downloaded.Reason, "the terminal failure stays on the milestone")
}

// TestBlueFieldSoftwareErrorState_ExtractFailureIsTerminal checks that the unpack tool's
// rejection of a bundle (Extracted Failure) is terminal even though the download succeeded and
// the retry interval has passed: re-downloading the same bundle cannot help, so the Error state
// stays put and reclaims the files.
func TestBlueFieldSoftwareErrorState_ExtractFailureIsTerminal(t *testing.T) {
	t.Cleanup(withTestBFBBaseDir(t))

	bfs := bfsInErrorWithDownloadedCondition(conditions.ReasonSuccess, time.Now().Add(-(RetryInterval + time.Minute)))
	bfs.Status.Conditions[0].Status = metav1.ConditionTrue
	conditions.AddFalse(bfs, provisioningv1.BlueFieldSoftwareCondExtracted,
		conditions.ReasonFailure, "Extract PLDM firmware bundle failed: bad package")
	bfs.Spec.PldmFwBundle = map[string]string{testPldmFwBundlePSID: testPldmFwBundleURL}
	completedPath := writeCompletedFwBundle(t, bfs)

	st := &blueFieldSoftwareErrorState{bfs: bfs}
	require.NoError(t, st.Handle(context.Background(), nil))

	assert.Equal(t, provisioningv1.BlueFieldSoftwareError, bfs.Status.Phase)
	_, err := os.Stat(completedPath)
	assert.True(t, os.IsNotExist(err), "terminal error reclaims the downloaded bundle")
	extracted := conditions.Get(bfs, provisioningv1.BlueFieldSoftwareCondExtracted)
	require.NotNil(t, extracted)
	assert.Equal(t, string(conditions.ReasonFailure), extracted.Reason, "the failure stays visible")
}

// TestBlueFieldSoftwareErrorState_ExtractErrorRetriesFromCachedFiles checks that a recoverable
// extraction error (unpack sidecar unreachable) is retried after the interval through the
// Downloading phase, which keeps the cached bundle and leads back to Extracting.
func TestBlueFieldSoftwareErrorState_ExtractErrorRetriesFromCachedFiles(t *testing.T) {
	t.Cleanup(withTestBFBBaseDir(t))

	bfs := bfsInErrorWithDownloadedCondition(conditions.ReasonSuccess, time.Now().Add(-(RetryInterval + time.Minute)))
	bfs.Status.Conditions[0].Status = metav1.ConditionTrue
	conditions.AddFalse(bfs, provisioningv1.BlueFieldSoftwareCondExtracted,
		conditions.ReasonError, "call unpack service: dial unix: connection refused")
	bfs.Spec.PldmFwBundle = map[string]string{testPldmFwBundlePSID: testPldmFwBundleURL}
	completedPath := writeCompletedFwBundle(t, bfs)

	st := &blueFieldSoftwareErrorState{bfs: bfs}
	require.NoError(t, st.Handle(context.Background(), nil))

	assert.Equal(t, provisioningv1.BlueFieldSoftwareDownloading, bfs.Status.Phase)
	_, err := os.Stat(completedPath)
	require.NoError(t, err, "the cached bundle is kept for re-extraction")
	downloaded := conditions.Get(bfs, provisioningv1.BlueFieldSoftwareCondDownloaded)
	require.NotNil(t, downloaded)
	assert.Equal(t, string(conditions.ReasonPending), downloaded.Reason, "the retry goes back through Downloading")
}

// TestBlueFieldSoftwareErrorState_DeletionReopensMilestones checks that a deletion seen in the
// Error phase moves to Deleting and reports Downloaded and Extracted as AwaitingDeletion.
func TestBlueFieldSoftwareErrorState_DeletionReopensMilestones(t *testing.T) {
	t.Cleanup(withTestBFBBaseDir(t))

	bfs := bfsInErrorWithDownloadedCondition(conditions.ReasonError, time.Now())
	now := metav1.Now()
	bfs.DeletionTimestamp = &now
	bfs.Finalizers = []string{provisioningv1.BlueFieldSoftwareFinalizer}

	st := &blueFieldSoftwareErrorState{bfs: bfs}
	require.NoError(t, st.Handle(context.Background(), nil))

	assert.Equal(t, provisioningv1.BlueFieldSoftwareDeleting, bfs.Status.Phase)
	for _, typ := range []conditions.ConditionType{
		provisioningv1.BlueFieldSoftwareCondDownloaded,
		provisioningv1.BlueFieldSoftwareCondExtracted,
	} {
		cond := conditions.Get(bfs, typ)
		require.NotNil(t, cond, typ)
		assert.Equal(t, metav1.ConditionFalse, cond.Status, typ)
		assert.Equal(t, string(conditions.ReasonAwaitingDeletion), cond.Reason, typ)
	}
}

func TestBlueFieldSoftwareErrorState_NoRetryWithinInterval(t *testing.T) {
	defer withTestBFBBaseDir(t)()

	bfs := bfsInErrorWithDownloadedCondition(conditions.ReasonError, time.Now().Add(-(RetryInterval / 2)))

	st := &blueFieldSoftwareErrorState{bfs: bfs}
	require.NoError(t, st.Handle(context.Background(), nil))

	assert.Equal(t, provisioningv1.BlueFieldSoftwareError, bfs.Status.Phase)
	downloaded := conditions.Get(bfs, provisioningv1.BlueFieldSoftwareCondDownloaded)
	require.NotNil(t, downloaded)
	assert.Equal(t, string(conditions.ReasonError), downloaded.Reason, "the recoverable error stays until the next retry")
}

func TestBlueFieldSoftwareErrorState_NoRetryAfterWindow(t *testing.T) {
	defer withTestBFBBaseDir(t)()

	bfs := bfsInErrorWithDownloadedCondition(conditions.ReasonError, time.Now().Add(-(RetryWindow + time.Hour)))

	st := &blueFieldSoftwareErrorState{bfs: bfs}
	require.NoError(t, st.Handle(context.Background(), nil))

	assert.Equal(t, provisioningv1.BlueFieldSoftwareError, bfs.Status.Phase)
	downloaded := conditions.Get(bfs, provisioningv1.BlueFieldSoftwareCondDownloaded)
	require.NotNil(t, downloaded)
	assert.Equal(t, metav1.ConditionFalse, downloaded.Status)
	assert.Equal(t, string(conditions.ReasonFailure), downloaded.Reason,
		"the controller gave up, so the recoverable Error becomes Failure")
	assert.Contains(t, downloaded.Message, "Retry window")
}

// TestBlueFieldSoftwareErrorState_ExtractErrorIsRetriedAfterRetryWindow checks that a recoverable
// extraction error is not given up on when the download finished longer ago than RetryWindow, as
// after a controller outage: it is retried, the cached bundle is kept and Extracted stays Error.
func TestBlueFieldSoftwareErrorState_ExtractErrorIsRetriedAfterRetryWindow(t *testing.T) {
	t.Cleanup(withTestBFBBaseDir(t))

	bfs := bfsInErrorWithDownloadedCondition(conditions.ReasonSuccess, time.Now().Add(-(RetryWindow + time.Hour)))
	bfs.Status.Conditions[0].Status = metav1.ConditionTrue
	conditions.AddFalse(bfs, provisioningv1.BlueFieldSoftwareCondExtracted,
		conditions.ReasonError, "unpack sidecar unreachable")
	bfs.Spec.PldmFwBundle = map[string]string{testPldmFwBundlePSID: testPldmFwBundleURL}
	completedPath := writeCompletedFwBundle(t, bfs)

	st := &blueFieldSoftwareErrorState{
		bfs: bfs,
	}
	require.NoError(t, st.Handle(context.Background(), nil))

	assert.Equal(t, provisioningv1.BlueFieldSoftwareDownloading, bfs.Status.Phase, "the retry goes back through Downloading")
	assert.FileExists(t, completedPath, "the cached bundle is kept for re-extraction")
	extracted := conditions.Get(bfs, provisioningv1.BlueFieldSoftwareCondExtracted)
	require.NotNil(t, extracted)
	assert.Equal(t, string(conditions.ReasonError), extracted.Reason, "a recoverable extraction error never becomes Failure")
	assert.Contains(t, extracted.Message, "unpack sidecar unreachable", "the original error stays visible")
}

func TestBlueFieldSoftwareErrorState_TerminalFailureRemovesCompletedSibling(t *testing.T) {
	defer withTestBFBBaseDir(t)()

	// osIso download failed terminally, but the sibling DPU PLDM bundle finished before it.
	bfs := bfsInErrorWithDownloadedCondition(conditions.ReasonFailure, time.Now())
	bfs.Spec.PldmFwBundle = map[string]string{testPldmFwBundlePSID: testPldmFwBundleURL}
	completedPath := writeCompletedFwBundle(t, bfs)

	st := &blueFieldSoftwareErrorState{bfs: bfs}
	require.NoError(t, st.Handle(context.Background(), nil))

	assert.Equal(t, provisioningv1.BlueFieldSoftwareError, bfs.Status.Phase)
	_, err := os.Stat(completedPath)
	assert.True(t, os.IsNotExist(err), "completed sibling should be removed on terminal error")
}

func TestBlueFieldSoftwareErrorState_TerminalAfterWindowRemovesCompletedSibling(t *testing.T) {
	defer withTestBFBBaseDir(t)()

	// Recoverable error whose retry window has elapsed is terminal: clean up siblings.
	bfs := bfsInErrorWithDownloadedCondition(conditions.ReasonError, time.Now().Add(-(RetryWindow + time.Hour)))
	bfs.Spec.PldmFwBundle = map[string]string{testPldmFwBundlePSID: testPldmFwBundleURL}
	completedPath := writeCompletedFwBundle(t, bfs)

	st := &blueFieldSoftwareErrorState{bfs: bfs}
	require.NoError(t, st.Handle(context.Background(), nil))

	_, err := os.Stat(completedPath)
	assert.True(t, os.IsNotExist(err), "completed sibling should be removed once the retry window expired")
}

func TestBlueFieldSoftwareErrorState_TransientErrorPreservesCompletedSibling(t *testing.T) {
	defer withTestBFBBaseDir(t)()

	// Recoverable error, still too soon to retry (within RetryInterval): not terminal, so a
	// completed sibling must be preserved for the imminent retry to reuse.
	bfs := bfsInErrorWithDownloadedCondition(conditions.ReasonError, time.Now().Add(-(RetryInterval / 2)))
	bfs.Spec.PldmFwBundle = map[string]string{testPldmFwBundlePSID: testPldmFwBundleURL}
	completedPath := writeCompletedFwBundle(t, bfs)

	st := &blueFieldSoftwareErrorState{bfs: bfs}
	require.NoError(t, st.Handle(context.Background(), nil))

	assert.FileExists(t, completedPath, "completed sibling must be preserved during the transient retry window")
}

func TestBlueFieldSoftwareErrorState_NoRetryWithoutDownloadedCondition(t *testing.T) {
	defer withTestBFBBaseDir(t)()

	bfs := &provisioningv1.BlueFieldSoftware{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bf4-software",
			Namespace: "dpf-operator-system",
		},
		Spec: provisioningv1.BlueFieldSpec{
			OsIso: testBF4OsIsoURL,
		},
		Status: provisioningv1.BlueFieldSoftwareStatus{
			Phase: provisioningv1.BlueFieldSoftwareError,
		},
	}

	st := &blueFieldSoftwareErrorState{bfs: bfs}
	require.NoError(t, st.Handle(context.Background(), nil))

	assert.Equal(t, provisioningv1.BlueFieldSoftwareError, bfs.Status.Phase)
	assert.Empty(t, bfs.Status.Conditions, "nothing is written without a Downloaded condition to act on")
}
