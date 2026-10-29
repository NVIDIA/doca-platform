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

package agent

import (
	"context"
	"testing"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	"github.com/nvidia/doca-platform/test/mock/dpuagent/pkg/phasedelay"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newTestRunner(t *testing.T, ops []stagedOp, delays *phasedelay.Delays) *Runner {
	t.Helper()
	scheme := runtime.NewScheme()
	utilruntime.Must(provisioningv1.AddToScheme(scheme))
	dpu := &provisioningv1.DPU{ObjectMeta: metav1.ObjectMeta{Name: "dpu", Namespace: "ns", UID: "uid"}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dpu).WithStatusSubresource(&provisioningv1.DPU{}).Build()
	optCtx := &operations.Context{
		Client:  c,
		Options: opts.Options{DPUName: "dpu", DPUNamespace: "ns", DPUUID: "uid"},
	}
	return NewRunner(optCtx, ops, delays)
}

func always(*operations.Context) bool { return true }

func TestRunnerWaitsTheShareOfEachRunOperation(t *testing.T) {
	const total = 3 * time.Second
	delays := phasedelay.New(map[provisioningv1.DPUPhase]phasedelay.Range{
		provisioningv1.DPUConfig: {Min: total, Max: total},
	})
	for name, tc := range map[string]struct {
		skip     func(*operations.Context) bool
		min, max time.Duration
	}{
		"both run":    {never, total, 10 * time.Second},
		"one skipped": {always, total / 2, total - 500*time.Millisecond},
	} {
		t.Run(name, func(t *testing.T) {
			ops := stage(provisioningv1.DPUConfig, phasedelay.StepOperation,
				&op{name: "A", condType: "A", skip: never},
				&op{name: "B", condType: "B", skip: tc.skip},
			)
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			r := newTestRunner(t, ops, delays)
			start := time.Now()
			if err := r.Run(ctx); err != nil {
				t.Fatal(err)
			}
			if elapsed := time.Since(start); elapsed < tc.min || elapsed > tc.max {
				t.Fatalf("run took %v, want between %v and %v", elapsed, tc.min, tc.max)
			}
		})
	}
}

func TestRunnerStopsWaitingWhenCanceled(t *testing.T) {
	delays := phasedelay.New(map[provisioningv1.DPUPhase]phasedelay.Range{
		provisioningv1.DPUConfig: {Min: time.Hour, Max: time.Hour},
	})
	ops := stage(provisioningv1.DPUConfig, phasedelay.StepOperation, &op{name: "A", condType: "A", skip: never})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	if err := newTestRunner(t, ops, delays).Run(ctx); err == nil {
		t.Fatal("canceled run returned nil")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("canceled run returned after %v", elapsed)
	}
}
