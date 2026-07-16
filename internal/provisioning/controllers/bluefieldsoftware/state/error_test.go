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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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

	errCond := conditions.Get(bfs, provisioningv1.BlueFieldSoftwareCondError)
	require.NotNil(t, errCond)
	assert.Equal(t, metav1.ConditionFalse, errCond.Status)
	assert.Equal(t, string(conditions.ReasonPending), errCond.Reason)
}

func TestBlueFieldSoftwareErrorState_NoRetryOnTerminalFailure(t *testing.T) {
	defer withTestBFBBaseDir(t)()

	bfs := bfsInErrorWithDownloadedCondition(conditions.ReasonFailure, time.Now().Add(-(RetryInterval + time.Minute)))

	st := &blueFieldSoftwareErrorState{bfs: bfs}
	require.NoError(t, st.Handle(context.Background(), nil))

	assert.Equal(t, provisioningv1.BlueFieldSoftwareError, bfs.Status.Phase)

	errCond := conditions.Get(bfs, provisioningv1.BlueFieldSoftwareCondError)
	require.NotNil(t, errCond)
	assert.Equal(t, metav1.ConditionTrue, errCond.Status)
}

func TestBlueFieldSoftwareErrorState_NoRetryWithinInterval(t *testing.T) {
	defer withTestBFBBaseDir(t)()

	bfs := bfsInErrorWithDownloadedCondition(conditions.ReasonError, time.Now().Add(-(RetryInterval / 2)))

	st := &blueFieldSoftwareErrorState{bfs: bfs}
	require.NoError(t, st.Handle(context.Background(), nil))

	assert.Equal(t, provisioningv1.BlueFieldSoftwareError, bfs.Status.Phase)
	errCond := conditions.Get(bfs, provisioningv1.BlueFieldSoftwareCondError)
	require.NotNil(t, errCond)
	assert.Equal(t, metav1.ConditionTrue, errCond.Status)
}

func TestBlueFieldSoftwareErrorState_NoRetryAfterWindow(t *testing.T) {
	defer withTestBFBBaseDir(t)()

	bfs := bfsInErrorWithDownloadedCondition(conditions.ReasonError, time.Now().Add(-(RetryWindow + time.Hour)))

	st := &blueFieldSoftwareErrorState{bfs: bfs}
	require.NoError(t, st.Handle(context.Background(), nil))

	assert.Equal(t, provisioningv1.BlueFieldSoftwareError, bfs.Status.Phase)
	errCond := conditions.Get(bfs, provisioningv1.BlueFieldSoftwareCondError)
	require.NotNil(t, errCond)
	assert.Equal(t, metav1.ConditionTrue, errCond.Status)
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
	errCond := conditions.Get(bfs, provisioningv1.BlueFieldSoftwareCondError)
	require.NotNil(t, errCond)
	assert.Equal(t, metav1.ConditionTrue, errCond.Status)
}
