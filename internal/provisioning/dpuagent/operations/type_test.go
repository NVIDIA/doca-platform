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

package operations

import (
	"context"
	"testing"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// TestDPUFlavorSnapshot verifies that changing a snapshot does not change the shared flavor.
func TestDPUFlavorSnapshot(t *testing.T) {
	ctx := &Context{DPUFlavor: provisioningv1.DPUFlavor{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"test": "original"}},
		Spec:       provisioningv1.DPUFlavorSpec{EWNicConfigurations: []provisioningv1.NicConfiguration{{NumVfs: 1}}},
	}}
	snapshot := ctx.DPUFlavorSnapshot()
	snapshot.Labels["test"] = "modified"
	snapshot.Spec.EWNicConfigurations[0].NumVfs = 8
	require.Equal(t, "original", ctx.DPUFlavor.Labels["test"])
	require.Equal(t, 1, ctx.DPUFlavor.Spec.FirstEWNicConfiguration().NumVfs)
}

// TestDPUFlavorSnapshotDuringReload verifies that a blocked API GET does not block snapshot readers.
func TestDPUFlavorSnapshotDuringReload(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	fetchStarted := make(chan struct{})
	c := fake.NewClientBuilder().WithInterceptorFuncs(interceptor.Funcs{
		Get: func(ctx context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
			close(fetchStarted)
			<-ctx.Done()
			return ctx.Err()
		},
	}).Build()
	shared := &Context{
		Client: c,
		DPUFlavor: provisioningv1.DPUFlavor{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "flavor",
				Generation: 1,
			},
		},
	}
	reloadDone := make(chan error, 1)
	go func() { reloadDone <- shared.ReloadDPUFlavor(ctx) }()
	select {
	case <-fetchStarted:
	case <-time.After(time.Second):
		t.Fatal("reload did not reach the API")
	}
	snapshotDone := make(chan *provisioningv1.DPUFlavor, 1)
	go func() { snapshotDone <- shared.DPUFlavorSnapshot() }()
	select {
	case snapshot := <-snapshotDone:
		require.EqualValues(t, 1, snapshot.Generation)
	case <-time.After(time.Second):
		t.Fatal("snapshot blocked behind flavor GET")
	}
	cancel()
	select {
	case err := <-reloadDone:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("reload did not stop after cancellation")
	}
}
