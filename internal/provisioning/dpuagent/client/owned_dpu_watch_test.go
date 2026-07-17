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

package client

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type watchClient struct {
	crclient.Client
	watchFn func(ctx context.Context, list crclient.ObjectList, opts ...crclient.ListOption) (watch.Interface, error)
}

func (w *watchClient) Watch(ctx context.Context, list crclient.ObjectList, opts ...crclient.ListOption) (watch.Interface, error) {
	return w.watchFn(ctx, list, opts...)
}

func TestRunDPUWatchUsesNameScopedWatch(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, provisioningv1.AddToScheme(scheme))

	var observedNamespace string
	var observedFieldSelector string
	reconcileCalls := 0

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	baseClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	wc := &watchClient{
		Client: baseClient,
		watchFn: func(_ context.Context, _ crclient.ObjectList, opts ...crclient.ListOption) (watch.Interface, error) {
			listOpts := &crclient.ListOptions{}
			for _, opt := range opts {
				opt.ApplyToList(listOpts)
			}
			observedNamespace = listOpts.Namespace
			if listOpts.FieldSelector != nil {
				observedFieldSelector = listOpts.FieldSelector.String()
			}
			cancel()
			return watch.NewFake(), nil
		},
	}

	err := RunDPUWatch(ctx, wc, "dpf-operator-system", "worker1-mt2323xz09fx", func() {
		reconcileCalls++
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, reconcileCalls, "onReconcile should be called once before watch loop")
	require.Equal(t, "dpf-operator-system", observedNamespace)
	require.Equal(t, "metadata.name=worker1-mt2323xz09fx", observedFieldSelector)
}

func TestRunDPUWatchReturnsWatchSetupError(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, provisioningv1.AddToScheme(scheme))

	wantErr := errors.New("watch denied")
	baseClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	wc := &watchClient{
		Client: baseClient,
		watchFn: func(_ context.Context, _ crclient.ObjectList, _ ...crclient.ListOption) (watch.Interface, error) {
			return nil, wantErr
		},
	}

	err := RunDPUWatch(context.Background(), wc, "dpf-operator-system", "worker1-mt2323xz09fx", func() {})
	require.ErrorContains(t, err, "watch DPUs in namespace dpf-operator-system")
	require.ErrorIs(t, err, wantErr)
}

func TestRunDPUWatchReopensOnChannelClose(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, provisioningv1.AddToScheme(scheme))

	first := watch.NewFake()
	second := watch.NewFake()
	defer second.Stop()

	var watchCalls atomic.Int32
	var reconcileCalls atomic.Int32
	secondOpened := make(chan struct{})

	baseClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	wc := &watchClient{
		Client: baseClient,
		watchFn: func(_ context.Context, _ crclient.ObjectList, _ ...crclient.ListOption) (watch.Interface, error) {
			if watchCalls.Add(1) == 1 {
				return first, nil
			}
			select {
			case <-secondOpened:
			default:
				close(secondOpened)
			}
			return second, nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- RunDPUWatch(ctx, wc, "dpf-operator-system", "target-dpu", func() {
			reconcileCalls.Add(1)
		})
	}()

	first.Stop()
	require.Eventually(t, func() bool {
		select {
		case <-secondOpened:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond, "watch should be reopened after channel close")

	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	require.GreaterOrEqual(t, watchCalls.Load(), int32(2))
	require.GreaterOrEqual(t, reconcileCalls.Load(), int32(2), "reconcile should trigger after watch reopen")
}
