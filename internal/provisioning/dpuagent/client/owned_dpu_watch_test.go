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

	err := RunDPUWatch(ctx, wc, "dpf-operator-system", "worker1-mt2323xz09fx", func(event *provisioningv1.DPU) {
		require.Nil(t, event)
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

	err := RunDPUWatch(context.Background(), wc, "dpf-operator-system", "worker1-mt2323xz09fx", func(*provisioningv1.DPU) {})
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
		done <- RunDPUWatch(ctx, wc, "dpf-operator-system", "target-dpu", func(*provisioningv1.DPU) {
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

func TestRunDPUWatchPassesEventDPU(t *testing.T) {
	t.Parallel()

	fw := watch.NewFake()
	defer fw.Stop()

	watchReady := make(chan struct{})
	baseClient := fake.NewClientBuilder().Build()
	wc := &watchClient{
		Client: baseClient,
		watchFn: func(_ context.Context, _ crclient.ObjectList, _ ...crclient.ListOption) (watch.Interface, error) {
			select {
			case <-watchReady:
			default:
				close(watchReady)
			}
			return fw, nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	got := make(chan *provisioningv1.DPU, 2)
	done := make(chan error, 1)
	go func() {
		done <- RunDPUWatch(ctx, wc, "dpf-operator-system", "target-dpu", func(event *provisioningv1.DPU) {
			got <- event
			if event != nil {
				cancel()
			}
		})
	}()

	require.Eventually(t, func() bool {
		select {
		case event := <-got:
			return event == nil
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond, "pre-watch reconcile should pass a nil DPU")
	require.Eventually(t, func() bool {
		select {
		case <-watchReady:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)

	eventDPU := &provisioningv1.DPU{}
	eventDPU.Name = "target-dpu"
	eventDPU.Namespace = "dpf-operator-system"
	eventDPU.Status.Phase = provisioningv1.DPUNodeEffect
	go fw.Modify(eventDPU)

	var observed *provisioningv1.DPU
	require.Eventually(t, func() bool {
		select {
		case observed = <-got:
			return observed != nil
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond, "watch event should pass the event DPU")
	require.Equal(t, provisioningv1.DPUNodeEffect, observed.Status.Phase)
	require.ErrorIs(t, <-done, context.Canceled)
}
