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

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
)

// bfsInReadyPhase builds a BlueFieldSoftware in the Ready phase with one DPU PLDM bundle in
// its spec and every milestone already True, as the earlier phases leave it.
func bfsInReadyPhase() *provisioningv1.BlueFieldSoftware {
	bfs := &provisioningv1.BlueFieldSoftware{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "bf4-software",
			Namespace:  "dpf-operator-system",
			Generation: 2,
		},
		Spec: provisioningv1.BlueFieldSpec{
			PldmFwBundle: map[string]string{testPldmFwBundlePSID: testPldmFwBundleURL},
		},
		Status: provisioningv1.BlueFieldSoftwareStatus{
			Phase: provisioningv1.BlueFieldSoftwareReady,
		},
	}
	conditions.AddTrue(bfs, provisioningv1.BlueFieldSoftwareCondInitialized)
	conditions.AddTrue(bfs, provisioningv1.BlueFieldSoftwareCondDownloaded)
	conditions.AddTrue(bfs, provisioningv1.BlueFieldSoftwareCondExtracted)
	return bfs
}

// TestBlueFieldSoftwareReadyState_ReassertsMilestonesWhileFilesCached checks that the Ready phase
// restores a milestone that fell behind the phase (a conditions patch lost to a conflict) while
// the cached files exist, and never writes Ready itself.
func TestBlueFieldSoftwareReadyState_ReassertsMilestonesWhileFilesCached(t *testing.T) {
	t.Cleanup(withTestBFBBaseDir(t))

	bfs := bfsInReadyPhase()
	writeCompletedFwBundle(t, bfs)
	// Simulate a conditions patch lost to a conflict: the stored milestone is behind the phase.
	conditions.AddFalse(bfs, provisioningv1.BlueFieldSoftwareCondExtracted, conditions.ReasonPending, "stale")

	st := &blueFieldSoftwareReadyState{
		bfs:      bfs,
		recorder: record.NewFakeRecorder(5),
	}
	require.NoError(t, st.Handle(context.Background(), nil))

	assert.Equal(t, provisioningv1.BlueFieldSoftwareReady, bfs.Status.Phase)
	assert.True(t, conditions.IsTrue(bfs, provisioningv1.BlueFieldSoftwareCondInitialized))
	assert.True(t, conditions.IsTrue(bfs, provisioningv1.BlueFieldSoftwareCondDownloaded))
	assert.True(t, conditions.IsTrue(bfs, provisioningv1.BlueFieldSoftwareCondExtracted))
	assert.Nil(t, conditions.Get(bfs, provisioningv1.BlueFieldSoftwareCondReady), "Ready is the controller's summary")
}

// TestBlueFieldSoftwareReadyState_MissingFileReopensDownloadAndExtract checks that a missing
// component file sends the object back to Downloading with Downloaded Unknown/Pending and
// Extracted False/Pending, since both phases run again. Downloaded must not turn False here,
// because that would start the retry window before any download attempt has failed.
func TestBlueFieldSoftwareReadyState_MissingFileReopensDownloadAndExtract(t *testing.T) {
	t.Cleanup(withTestBFBBaseDir(t))

	// The spec names a bundle that is not on disk.
	bfs := bfsInReadyPhase()

	st := &blueFieldSoftwareReadyState{
		bfs:      bfs,
		recorder: record.NewFakeRecorder(5),
	}
	require.NoError(t, st.Handle(context.Background(), nil))

	assert.Equal(t, provisioningv1.BlueFieldSoftwareDownloading, bfs.Status.Phase)
	downloaded := conditions.Get(bfs, provisioningv1.BlueFieldSoftwareCondDownloaded)
	require.NotNil(t, downloaded)
	assert.Equal(t, metav1.ConditionUnknown, downloaded.Status)
	assert.Equal(t, string(conditions.ReasonPending), downloaded.Reason)
	assert.Contains(t, downloaded.Message, "missing")
	extracted := conditions.Get(bfs, provisioningv1.BlueFieldSoftwareCondExtracted)
	require.NotNil(t, extracted)
	assert.Equal(t, metav1.ConditionFalse, extracted.Status)
	assert.Equal(t, string(conditions.ReasonPending), extracted.Reason)
	assert.True(t, conditions.IsTrue(bfs, provisioningv1.BlueFieldSoftwareCondInitialized))
	assert.Nil(t, conditions.Get(bfs, provisioningv1.BlueFieldSoftwareCondReady))
}

// TestBlueFieldSoftwareReadyState_DeletionReopensMilestones checks that a deletion seen in the
// Ready phase moves to Deleting and reports Downloaded and Extracted as AwaitingDeletion.
func TestBlueFieldSoftwareReadyState_DeletionReopensMilestones(t *testing.T) {
	t.Cleanup(withTestBFBBaseDir(t))

	bfs := bfsInReadyPhase()
	now := metav1.Now()
	bfs.DeletionTimestamp = &now
	bfs.Finalizers = []string{provisioningv1.BlueFieldSoftwareFinalizer}

	st := &blueFieldSoftwareReadyState{
		bfs:      bfs,
		recorder: record.NewFakeRecorder(5),
	}
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
	assert.True(t, conditions.IsTrue(bfs, provisioningv1.BlueFieldSoftwareCondInitialized))
}
