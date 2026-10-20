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

package tracing

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	corev1 "k8s.io/api/core/v1"
)

// observe runs one reconcile of obj by a WorkflowRoot controller that reports its rollout.
func observe(obj *corev1.ConfigMap, ready bool) {
	observeRevision(obj, "", ready)
}

// observeRevision is observe for an object whose inputs are at revision.
func observeRevision(obj *corev1.ConfigMap, revision string, ready bool) {
	ctx, endSpan := ReconcileSpan(context.Background(), "dpudeployment", obj, WorkflowRoot(), Revision(revision))
	ObserveRollout(ctx, "dpudeployment", obj, ready)
	endSpan(nil)
}

// spansNamed returns the exported spans with name.
func spansNamed(exporter *tracetest.InMemoryExporter, name string) tracetest.SpanStubs {
	var named tracetest.SpanStubs
	for _, s := range exporter.GetSpans() {
		if s.Name == name {
			named = append(named, s)
		}
	}
	return named
}

func TestObserveRollout_ChangeIsARootWhileTheChangeIsInProgress(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)
	useTracker(t)
	obj := newTestObject(nil)

	observe(obj, false)
	observe(obj, false)

	changes := spansNamed(exporter, "dpudeployment.change")
	require.Len(t, changes, 1, "the marker is exported once")
	derived := newSpanContextForObject(obj, "")
	require.False(t, changes[0].Parent.IsValid())
	require.Equal(t, derived.TraceID(), changes[0].SpanContext.TraceID())
	require.NotEqual(t, derived.SpanID(), changes[0].SpanContext.SpanID(), "the reconciles do not point at the marker")
	require.Equal(t, derived.TraceID().String(), attrValue(t, changes[0].Attributes, "workflow.id").AsString())

	for _, reconcile := range spansNamed(exporter, "dpudeployment.reconcile") {
		require.Equal(t, derived.SpanID(), reconcile.Parent.SpanID(), "they point at the span ID the rollout takes")
	}
}

func TestObserveRollout_NoChangeSpanWhenObjectCarriesTraceContext(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)
	useTracker(t)
	obj := newTestObject(map[string]string{
		TraceParentAnnotation: fmt.Sprintf("00-%s-%s-01", testTraceID, testSpanID),
	})

	observe(obj, false)
	observe(obj, true)

	require.Empty(t, spansNamed(exporter, "dpudeployment.change"), "the upstream caller owns the root")
	rollouts := spansNamed(exporter, "dpudeployment.rollout")
	require.Len(t, rollouts, 1)
	require.Equal(t, testTraceID, rollouts[0].SpanContext.TraceID().String())
	require.Equal(t, testSpanID, rollouts[0].Parent.SpanID().String(), "the rollout is a child of the upstream span")
}

func TestObserveRollout_ReadyEndsRollout(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)
	now := useTracker(t)
	obj := newTestObject(nil)

	observe(obj, false)
	require.Empty(t, spansNamed(exporter, "dpudeployment.rollout"))

	start := *now
	*now = now.Add(4 * time.Minute)
	observe(obj, true)
	observe(obj, true)

	rollouts := spansNamed(exporter, "dpudeployment.rollout")
	require.Len(t, rollouts, 1, "a rollout is reported once")
	got := rollouts[0]
	derived := newSpanContextForObject(obj, "")
	require.Equal(t, derived.TraceID(), got.SpanContext.TraceID())
	require.False(t, got.Parent.IsValid(), "the rollout is a root")
	require.Equal(t, derived.SpanID(), got.SpanContext.SpanID(), "and the parent of the reconciles")
	require.Equal(t, start.UnixNano(), got.StartTime.UnixNano())
	require.Equal(t, 4*time.Minute, got.EndTime.Sub(got.StartTime))
	require.Equal(t, "ready", attrValue(t, got.Attributes, "rollout.result").AsString())
	require.Equal(t, derived.TraceID().String(), attrValue(t, got.Attributes, "workflow.id").AsString())
	require.Equal(t, int64(3), attrValue(t, got.Attributes, "k8s.object.generation").AsInt64())
	require.Equal(t, codes.Unset, got.Status.Code)
}

func TestObserveRollout_StartsWithTheReconcileThatSawTheChange(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)
	now := useTracker(t)
	obj := newTestObject(nil)

	start := *now
	ctx, endSpan := ReconcileSpan(context.Background(), "dpudeployment", obj, WorkflowRoot())
	*now = now.Add(time.Minute) // the first reconcile takes a while before it reports the rollout
	ObserveRollout(ctx, "dpudeployment", obj, false)
	endSpan(nil)
	*now = now.Add(time.Minute)
	observe(obj, true)

	changes := spansNamed(exporter, "dpudeployment.change")
	rollouts := spansNamed(exporter, "dpudeployment.rollout")
	require.Len(t, changes, 1)
	require.Len(t, rollouts, 1)
	require.Equal(t, start.UnixNano(), changes[0].StartTime.UnixNano())
	require.Equal(t, start.UnixNano(), rollouts[0].StartTime.UnixNano(), "the rollout covers the reconcile that saw the change")
	require.Equal(t, 2*time.Minute, rollouts[0].EndTime.Sub(rollouts[0].StartTime))
}

func TestObserveRollout_TimeoutEndsRollout(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)
	now := useTracker(t)
	obj := newTestObject(nil)

	observe(obj, false)
	*now = now.Add(DefaultRolloutTimeout - time.Second)
	observe(obj, false)
	require.Empty(t, spansNamed(exporter, "dpudeployment.rollout"))

	*now = now.Add(time.Second)
	observe(obj, false)
	observe(obj, true)

	rollouts := spansNamed(exporter, "dpudeployment.rollout")
	require.Len(t, rollouts, 1)
	require.Equal(t, "timeout", attrValue(t, rollouts[0].Attributes, "rollout.result").AsString())
	require.Equal(t, "timeout", attrValue(t, rollouts[0].Attributes, "error.type").AsString())
	require.Equal(t, codes.Error, rollouts[0].Status.Code)
}

func TestObserveRollout_TimeoutIsConfigurable(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)
	now := useTrackerWith(t, FlagOptions{RolloutTimeout: 10 * time.Second})
	obj := newTestObject(nil)

	observe(obj, false)
	*now = now.Add(10 * time.Second)
	observe(obj, false)

	rollouts := spansNamed(exporter, "dpudeployment.rollout")
	require.Len(t, rollouts, 1)
	require.Equal(t, "timeout", attrValue(t, rollouts[0].Attributes, "rollout.result").AsString())
	require.Equal(t, "not ready after 10s", rollouts[0].Status.Description)
}

func TestObserveRollout_LateReconcileEndsRolloutAtItsDeadline(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)
	now := useTracker(t)
	obj := newTestObject(nil)

	observe(obj, false)
	start := *now
	*now = now.Add(5 * time.Hour)
	observe(obj, false)

	rollouts := spansNamed(exporter, "dpudeployment.rollout")
	require.Len(t, rollouts, 1)
	require.Equal(t, "timeout", attrValue(t, rollouts[0].Attributes, "rollout.result").AsString())
	require.Equal(t, start.Add(DefaultRolloutTimeout), rollouts[0].EndTime, "the rollout ends at its deadline, not when the reconcile noticed")
}

func TestObserveRollout_NewGenerationAfterTheDeadlineTimedOutTheOldRollout(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)
	now := useTracker(t)
	obj := newTestObject(nil)

	observe(obj, false)
	start := *now
	*now = now.Add(5 * time.Hour)
	next := newTestObject(nil)
	next.Generation = obj.Generation + 1
	observe(next, false)

	rollouts := spansNamed(exporter, "dpudeployment.rollout")
	require.Len(t, rollouts, 1)
	require.Equal(t, "timeout", attrValue(t, rollouts[0].Attributes, "rollout.result").AsString())
	require.Equal(t, start.Add(DefaultRolloutTimeout), rollouts[0].EndTime)
}

func TestObserveRollout_ReadyWhenNoticedLateEndsWhenNoticed(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)
	now := useTracker(t)
	obj := newTestObject(nil)

	observe(obj, false)
	*now = now.Add(5 * time.Hour)
	observe(obj, true)

	rollouts := spansNamed(exporter, "dpudeployment.rollout")
	require.Len(t, rollouts, 1)
	require.Equal(t, "ready", attrValue(t, rollouts[0].Attributes, "rollout.result").AsString(), "a status change triggered this reconcile")
	require.Equal(t, *now, rollouts[0].EndTime)
}

func TestObserveRollout_NewGenerationSupersedesRollout(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)
	useTracker(t)
	obj := newTestObject(nil)

	observe(obj, false)
	next := newTestObject(nil)
	next.Generation = obj.Generation + 1
	observe(next, false)
	observe(next, true)

	require.Len(t, spansNamed(exporter, "dpudeployment.change"), 2, "each generation has its own trace")
	rollouts := spansNamed(exporter, "dpudeployment.rollout")
	require.Len(t, rollouts, 2)
	require.Equal(t, "superseded", attrValue(t, rollouts[0].Attributes, "rollout.result").AsString())
	require.Equal(t, obj.Generation, attrValue(t, rollouts[0].Attributes, "k8s.object.generation").AsInt64())
	require.Equal(t, newSpanContextForObject(obj, "").TraceID(), rollouts[0].SpanContext.TraceID())
	require.Equal(t, "ready", attrValue(t, rollouts[1].Attributes, "rollout.result").AsString())
	require.Equal(t, next.Generation, attrValue(t, rollouts[1].Attributes, "k8s.object.generation").AsInt64())
	require.Equal(t, newSpanContextForObject(next, "").TraceID(), rollouts[1].SpanContext.TraceID())
}

func TestObserveRollout_NewRevisionIsANewChange(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)
	useTracker(t)
	obj := newTestObject(nil)

	observeRevision(obj, "template-1", false)
	observeRevision(obj, "template-2", false)
	observeRevision(obj, "template-2", true)

	changes := spansNamed(exporter, "dpudeployment.change")
	require.Len(t, changes, 2, "a change of an input of the same generation has its own root")
	require.Equal(t, newSpanContextForObject(obj, "template-1").TraceID(), changes[0].SpanContext.TraceID())
	require.Equal(t, newSpanContextForObject(obj, "template-2").TraceID(), changes[1].SpanContext.TraceID())
	require.NotEqual(t, changes[0].SpanContext.TraceID(), changes[1].SpanContext.TraceID())

	rollouts := spansNamed(exporter, "dpudeployment.rollout")
	require.Len(t, rollouts, 2)
	require.Equal(t, "superseded", attrValue(t, rollouts[0].Attributes, "rollout.result").AsString())
	require.Equal(t, "ready", attrValue(t, rollouts[1].Attributes, "rollout.result").AsString())
}

func TestObserveRollout_ReadyWhenFirstSeenIsNoRollout(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)
	useTracker(t)
	obj := newTestObject(nil)

	observe(obj, true)
	observe(obj, true)
	changes := spansNamed(exporter, "dpudeployment.change")
	require.Len(t, changes, 1, "the reconciles still need a root")
	require.Equal(t, newSpanContextForObject(obj, "").SpanID(), changes[0].SpanContext.SpanID(),
		"the change span takes the span ID the reconciles point at, as no rollout span will")
	require.Empty(t, spansNamed(exporter, "dpudeployment.rollout"), "the start of a rollout that was ready on first sight is unknown")

	next := newTestObject(nil)
	next.Generation = obj.Generation + 1
	observe(next, true)
	rollouts := spansNamed(exporter, "dpudeployment.rollout")
	require.Len(t, rollouts, 1, "a new generation that is ready at once is still a rollout")
	require.Equal(t, newSpanContextForObject(next, "").SpanID(), rollouts[0].SpanContext.SpanID())
	changes = spansNamed(exporter, "dpudeployment.change")
	require.Len(t, changes, 2)
	require.NotEqual(t, rollouts[0].SpanContext.SpanID(), changes[1].SpanContext.SpanID(),
		"with a rollout span, the change span has an ID of its own")
}

func TestObserveRollout_NoTrackerNoSpan(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)

	observe(newTestObject(nil), true)
	require.Empty(t, spansNamed(exporter, "dpudeployment.change"))
	require.Empty(t, spansNamed(exporter, "dpudeployment.rollout"))
}

func TestObserveRollout_NoReconcileContextNoSpan(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)
	useTracker(t)

	ObserveRollout(context.Background(), "dpudeployment", newTestObject(nil), true)
	require.Empty(t, exporter.GetSpans())
}

func TestObserveRollout_NoUIDNoSpan(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)
	useTracker(t)

	obj := newTestObject(nil)
	obj.UID = ""
	observe(obj, true)
	require.Empty(t, spansNamed(exporter, "dpudeployment.change"))
	require.Empty(t, spansNamed(exporter, "dpudeployment.rollout"))
}
