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
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// useTracker installs a tracker with the default rollout timeout, whose clock is the returned
// pointer for the duration of the test.
func useTracker(t *testing.T) *time.Time {
	t.Helper()
	return useTrackerWith(t, FlagOptions{RolloutTimeout: DefaultRolloutTimeout})
}

// useTrackerWith is useTracker for a tracker that follows opts. It fills in the default AddFlags
// sets for an unset sampling percent. The rollout timeout is not defaulted, as zero turns it off.
func useTrackerWith(t *testing.T, opts FlagOptions) *time.Time {
	t.Helper()
	if opts.SamplingPercent == 0 {
		opts.SamplingPercent = DefaultSamplingPercent
	}
	now := time.Now()
	tr := newTracker(opts)
	tr.now = func() time.Time { return now }
	reconciles.Store(tr)
	t.Cleanup(func() { reconciles.Store(nil) })
	return &now
}

// reconcileAt runs one reconcile of the test object at resourceVersion.
func reconcileAt(resourceVersion string, err error) {
	obj := newTestObject(nil)
	obj.ResourceVersion = resourceVersion
	ctx, endSpan := ReconcileSpan(context.Background(), "dpuset", obj)
	_, child := StartSpan(ctx, "child")
	child.End()
	endSpan(&err)
}

func spanNames(exporter *tracetest.InMemoryExporter) []string {
	spans := exporter.GetSpans()
	names := make([]string, 0, len(spans))
	for _, s := range spans {
		names = append(names, s.Name)
	}
	return names
}

func TestReconcileSpan_IdleReconcileIsNotRecorded(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)
	useTracker(t)

	reconcileAt("1", nil)
	require.Equal(t, []string{"child", "dpuset.reconcile"}, spanNames(exporter))

	exporter.Reset()
	reconcileAt("1", nil)
	require.Empty(t, exporter.GetSpans(), "same resourceVersion after a success is idle, with its child spans")
}

func TestReconcileSpan_NoIdleRecordsRetryAtSameResourceVersion(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)
	useTracker(t)

	reconcile := func() {
		obj := newTestObject(nil)
		obj.ResourceVersion = "1"
		ctx, endSpan := ReconcileSpan(context.Background(), "dpuset", obj, NoIdle())
		_, child := StartSpan(ctx, "child")
		child.End()
		endSpan(nil)
	}
	reconcile()
	exporter.Reset()
	reconcile()
	require.Equal(t, []string{"child", "dpuset.reconcile"}, spanNames(exporter))
}

// TestReconcileSpan_JoinWorkflowOf checks which workflow a reconcile joins: the object's own, else
// the first candidate that has one, else a trace of its own.
func TestReconcileSpan_JoinWorkflowOf(t *testing.T) {
	chain := newTestObject(map[string]string{
		TraceParentAnnotation: fmt.Sprintf("00-%s-%s-01", testTraceID, testSpanID),
	})
	plain := newTestObject(nil)

	tests := []struct {
		name       string
		obj        *corev1.ConfigMap
		candidates []client.Object
		wantJoined bool
	}{
		{
			name:       "joins the first candidate with a context",
			obj:        newTestObject(nil),
			candidates: []client.Object{plain, chain},
			wantJoined: true,
		},
		{
			name: "keeps the context of the object",
			obj: newTestObject(map[string]string{
				TraceParentAnnotation: fmt.Sprintf("00-%s-%s-01", "11111111111111111111111111111111", testSpanID),
			}),
			candidates: []client.Object{chain},
		},
		{
			name:       "starts its own trace without a candidate with a context",
			obj:        newTestObject(nil),
			candidates: []client.Object{plain},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exporter := useInMemoryTracerProvider(t)

			_, endSpan := ReconcileSpan(context.Background(), "podipam", tt.obj, JoinWorkflowOf(func() []client.Object { return tt.candidates }))
			endSpan(nil)

			spans := exporter.GetSpans()
			require.Len(t, spans, 1)
			require.Equal(t, tt.wantJoined, spans[0].SpanContext.TraceID().String() == testTraceID)
		})
	}
}

// TestReconcileSpan_JoinWorkflowOfLooksUpLazily checks that the candidates are only looked up when
// tracing is on and the object carries no trace context.
func TestReconcileSpan_JoinWorkflowOfLooksUpLazily(t *testing.T) {
	lookups := 0
	candidates := func() []client.Object {
		lookups++
		return nil
	}

	_, endSpan := ReconcileSpan(context.Background(), "podipam", newTestObject(nil), JoinWorkflowOf(candidates))
	endSpan(nil)
	require.Zero(t, lookups, "tracing is off")

	useInMemoryTracerProvider(t)
	withContext := newTestObject(map[string]string{
		TraceParentAnnotation: fmt.Sprintf("00-%s-%s-01", testTraceID, testSpanID),
	})
	_, endSpan = ReconcileSpan(context.Background(), "podipam", withContext, JoinWorkflowOf(candidates))
	endSpan(nil)
	require.Zero(t, lookups, "the object carries a context")

	withoutContext := newTestObject(nil)
	withoutContext.UID = "other-uid"
	_, endSpan = ReconcileSpan(context.Background(), "podipam", withoutContext, JoinWorkflowOf(candidates))
	endSpan(nil)
	require.Equal(t, 1, lookups)
}

// TestReconcileSpan_JoinWorkflowOfSkipsLookupOfIdleReconcile checks that the candidates are not looked
// up for an idle reconcile, which is not recorded, unless it fails after all.
func TestReconcileSpan_JoinWorkflowOfSkipsLookupOfIdleReconcile(t *testing.T) {
	useInMemoryTracerProvider(t)
	chain := newTestObject(map[string]string{
		TraceParentAnnotation: fmt.Sprintf("00-%s-%s-01", testTraceID, testSpanID),
	})
	lookups := 0
	candidates := func() []client.Object {
		lookups++
		return []client.Object{chain}
	}
	reconcile := func(err error) {
		obj := newTestObject(nil)
		obj.ResourceVersion = "1"
		_, endSpan := ReconcileSpan(context.Background(), "podipam", obj, JoinWorkflowOf(candidates))
		endSpan(&err)
	}

	reconcile(nil)
	require.Equal(t, 1, lookups)
	reconcile(nil)
	require.Equal(t, 1, lookups, "the idle reconcile does not look up the candidates")
	reconcile(errors.New("boom"))
	require.Equal(t, 2, lookups, "an idle reconcile that fails is recorded with its workflow")
}

// TestReconcileSpan_JoinWorkflowOfKeepsWorkflowOfLateIdleReconcile checks that an idle reconcile that
// fails after all is exported with the workflow it joined, not with one derived from its object.
func TestReconcileSpan_JoinWorkflowOfKeepsWorkflowOfLateIdleReconcile(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)
	chain := newTestObject(map[string]string{
		TraceParentAnnotation: fmt.Sprintf("00-%s-%s-01", testTraceID, testSpanID),
	})
	candidates := func() []client.Object { return []client.Object{chain} }

	reconcile := func(err error) {
		obj := newTestObject(nil)
		obj.ResourceVersion = "1"
		_, endSpan := ReconcileSpan(context.Background(), "podipam", obj, JoinWorkflowOf(candidates))
		endSpan(&err)
	}
	reconcile(nil)
	exporter.Reset()
	reconcile(errors.New("boom"))

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	require.True(t, attrValue(t, spans[0].Attributes, "reconcile.idle_at_start").AsBool())
	require.Equal(t, testTraceID, attrValue(t, spans[0].Attributes, "workflow.id").AsString())
}

func TestReconcileSpan_FailMarksSpanAndKeepsRetryOutOfIdle(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)
	useTracker(t)

	reconcile := func() {
		obj := newTestObject(nil)
		obj.ResourceVersion = "1"
		ctx, endSpan := ReconcileSpan(context.Background(), "dpuset", obj)
		_, child := StartSpan(ctx, "child")
		child.End()
		Fail(ctx, errors.New("boom"))
		var err error
		endSpan(&err)
	}
	reconcile()
	exporter.Reset()
	reconcile()

	spans := exporter.GetSpans()
	require.Len(t, spans, 2, "a reconcile that failed is not idle, the retry keeps its child span")
	require.Equal(t, codes.Error, spans[1].Status.Code)
}

func TestReconcileSpan_IdleReconcileOfWorkflowIsNotRecorded(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)
	useTracker(t)

	obj := newTestObject(nil)
	obj.ResourceVersion = "1"
	for range 2 {
		ctx, endSpan := ReconcileSpan(context.Background(), "dpudeployment", obj, WorkflowRoot())
		_, child := StartSpan(ctx, "child")
		child.End()
		endSpan(nil)
	}
	require.Equal(t, []string{"child", "dpudeployment.reconcile"}, spanNames(exporter))
}

func TestReconcileSpan_NewRevisionIsNotIdle(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)
	useTracker(t)

	obj := newTestObject(nil)
	obj.ResourceVersion = "1"
	run := func(revision string) {
		ctx, endSpan := ReconcileSpan(context.Background(), "dpudeployment", obj, WorkflowRoot(), Revision(revision))
		_, child := StartSpan(ctx, "child")
		child.End()
		endSpan(nil)
	}

	run("a")
	exporter.Reset()
	run("a")
	require.Empty(t, exporter.GetSpans(), "same resourceVersion and revision is idle")

	run("b")
	require.Equal(t, []string{"child", "dpudeployment.reconcile"}, spanNames(exporter), "an input changed, the object did not")
}

func TestReconcileSpan_ChangedObjectIsRecorded(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)
	useTracker(t)

	reconcileAt("1", nil)
	exporter.Reset()
	reconcileAt("2", nil)
	require.Equal(t, []string{"child", "dpuset.reconcile"}, spanNames(exporter))
}

func TestReconcileSpan_RetryAfterErrorIsRecorded(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)
	useTracker(t)

	reconcileAt("1", errors.New("boom"))
	exporter.Reset()
	reconcileAt("1", nil)
	require.Equal(t, []string{"child", "dpuset.reconcile"}, spanNames(exporter))
}

func TestReconcileSpan_IdleReconcileThatFailsIsRecorded(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)
	useTracker(t)

	reconcileAt("1", nil)
	exporter.Reset()
	reconcileAt("1", errors.New("boom"))

	spans := exporter.GetSpans()
	require.Len(t, spans, 1, "the child span of an idle reconcile is lost")
	require.Equal(t, "dpuset.reconcile", spans[0].Name)
	require.Equal(t, codes.Error, spans[0].Status.Code)
	require.True(t, attrValue(t, spans[0].Attributes, "reconcile.idle_at_start").AsBool())
	require.Equal(t, newSpanContextForObject(newTestObject(nil), "").TraceID().String(), attrValue(t, spans[0].Attributes, "workflow.id").AsString())
}

func TestReconcileSpan_IdleReconcileOfWorkflowThatFailsJoinsTrace(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)
	useTracker(t)

	obj := newTestObject(nil)
	obj.ResourceVersion = "1"
	_, endSpan := ReconcileSpan(context.Background(), "dpudeployment", obj, WorkflowRoot())
	endSpan(nil)
	exporter.Reset()

	err := errors.New("boom")
	_, endSpan = ReconcileSpan(context.Background(), "dpudeployment", obj, WorkflowRoot())
	endSpan(&err)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	derived := newSpanContextForObject(obj, "")
	require.Equal(t, derived.TraceID(), spans[0].SpanContext.TraceID())
	require.Equal(t, derived.SpanID(), spans[0].Parent.SpanID())
}

func TestReconcileSpan_IdleReconcileThatChangesObjectIsRecorded(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)
	useTracker(t)

	reconcileAt("1", nil)
	exporter.Reset()

	obj := newTestObject(nil)
	obj.ResourceVersion = "1"
	_, endSpan := ReconcileSpan(context.Background(), "dpuset", obj)
	obj.ResourceVersion = "2"
	endSpan(nil)
	require.Equal(t, []string{"dpuset.reconcile"}, spanNames(exporter))

	exporter.Reset()
	reconcileAt("2", nil)
	require.Empty(t, exporter.GetSpans(), "the version the reconcile left behind is idle")
}

func TestReconcileSpan_IdleReconcileIsRecordedWhenConfigured(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)
	useTrackerWith(t, FlagOptions{RecordIdleReconciles: true})

	reconcileAt("1", nil)
	exporter.Reset()
	reconcileAt("1", nil)
	require.Equal(t, []string{"child", "dpuset.reconcile"}, spanNames(exporter))
}

// dropped reports whether the span is marked to not be exported.
func dropped(s tracetest.SpanStub) bool {
	for _, kv := range s.Attributes {
		if string(kv.Key) == dropAttribute {
			return kv.Value.AsBool()
		}
	}
	return false
}

func TestReconcileSpan_UnchangedReconcileWithoutChildSpansIsDropped(t *testing.T) {
	tests := []struct {
		name        string
		opts        FlagOptions
		childSpan   bool
		err         error
		mutate      func(*corev1.ConfigMap)
		wantDropped bool
	}{
		{name: "nothing happened", wantDropped: true},
		{name: "recording unchanged reconciles", opts: FlagOptions{RecordUnchangedReconciles: true}},
		{name: "it has a child span", childSpan: true},
		{name: "it failed", err: errors.New("boom")},
		{name: "it changed its object", mutate: func(c *corev1.ConfigMap) { c.Annotations = map[string]string{"a": "b"} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exporter := useInMemoryTracerProvider(t)
			useTrackerWith(t, tt.opts)

			obj := newTestObject(nil)
			ctx, endSpan := ReconcileSpan(context.Background(), "dpuset", obj)
			if tt.childSpan {
				_, child := StartSpan(ctx, "child")
				child.End()
			}
			if tt.mutate != nil {
				tt.mutate(obj)
			}
			endSpan(&tt.err)

			for _, s := range exporter.GetSpans() {
				if s.Name == "dpuset.reconcile" {
					require.Equal(t, tt.wantDropped, dropped(s))
				}
			}
		})
	}
}

func TestMarkUnchanged(t *testing.T) {
	// run starts a reconcile of an unchanged object with two child spans, the second marked unchanged
	// when markSecond is set, and returns the exported spans by name.
	run := func(t *testing.T, opts FlagOptions, markSecond bool) map[string]tracetest.SpanStub {
		exporter := useInMemoryTracerProvider(t)
		useTrackerWith(t, opts)

		ctx, endSpan := ReconcileSpan(context.Background(), "dpuset", newTestObject(nil))
		_, first := StartSpan(ctx, "first")
		first.End()
		secondCtx, second := StartSpan(ctx, "second")
		if markSecond {
			MarkUnchanged(secondCtx)
		}
		second.End()
		endSpan(nil)

		spans := map[string]tracetest.SpanStub{}
		for _, s := range exporter.GetSpans() {
			spans[s.Name] = s
		}
		return spans
	}

	t.Run("an unchanged span does not keep its reconcile alive", func(t *testing.T) {
		spans := run(t, FlagOptions{}, true)
		require.False(t, dropped(spans["first"]))
		require.True(t, dropped(spans["second"]))
		require.False(t, dropped(spans["dpuset.reconcile"]), "the first span still counts")
	})

	t.Run("a reconcile of only unchanged spans is dropped", func(t *testing.T) {
		exporter := useInMemoryTracerProvider(t)
		useTracker(t)

		ctx, endSpan := ReconcileSpan(context.Background(), "dpuset", newTestObject(nil))
		childCtx, child := StartSpan(ctx, "child")
		MarkUnchanged(childCtx)
		child.End()
		endSpan(nil)

		for _, s := range exporter.GetSpans() {
			require.True(t, dropped(s), s.Name)
		}
	})

	t.Run("a context that is not a child span's does nothing", func(t *testing.T) {
		exporter := useInMemoryTracerProvider(t)
		useTracker(t)

		ctx, endSpan := ReconcileSpan(context.Background(), "dpuset", newTestObject(nil))
		_, child := StartSpan(ctx, "child")
		child.End()
		MarkUnchanged(ctx)
		endSpan(nil)

		for _, s := range exporter.GetSpans() {
			require.False(t, dropped(s), s.Name)
		}
	})

	t.Run("marking a span twice takes it back once", func(t *testing.T) {
		exporter := useInMemoryTracerProvider(t)
		useTracker(t)

		ctx, endSpan := ReconcileSpan(context.Background(), "dpuset", newTestObject(nil))
		firstCtx, first := StartSpan(ctx, "first")
		MarkUnchanged(firstCtx)
		MarkUnchanged(firstCtx)
		first.End()
		_, second := StartSpan(ctx, "second")
		second.End()
		endSpan(nil)

		for _, s := range exporter.GetSpans() {
			require.Equal(t, s.Name == "first", dropped(s), s.Name)
		}
	})

	t.Run("nothing is dropped when unchanged reconciles are recorded", func(t *testing.T) {
		for _, s := range run(t, FlagOptions{RecordUnchangedReconciles: true}, true) {
			require.False(t, dropped(s), s.Name)
		}
	})
}

// dropFirstSampler drops the first trace it sees and keeps the rest, which tells a reconcile that
// starts a new trace from one that does not.
type dropFirstSampler struct {
	calls atomic.Int32
}

func (s *dropFirstSampler) ShouldSample(p sdktrace.SamplingParameters) sdktrace.SamplingResult {
	if s.calls.Add(1) == 1 {
		return sdktrace.NeverSample().ShouldSample(p)
	}
	return sdktrace.AlwaysSample().ShouldSample(p)
}

func (s *dropFirstSampler) Description() string { return "dropFirstSampler" }

func TestReconcileSpan_SampledOutReconcileThatFailsStaysDropped(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
		sdktrace.WithIDGenerator(idGenerator{}),
		sdktrace.WithSampler(sdktrace.ParentBased(&dropFirstSampler{})),
	))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	useTracker(t)

	reconcileAt("1", errors.New("boom"))
	require.Empty(t, exporter.GetSpans(), "a reconcile dropped by sampling is not an idle reconcile and gets no second chance")
}

// TestReconcileSpan_ZeroTimeoutKeepsReconcilesInTheWorkflow checks that a zero rollout timeout also
// turns off the window a trace collects reconciles in, so a late reconcile rejoins the workflow.
func TestReconcileSpan_ZeroTimeoutKeepsReconcilesInTheWorkflow(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)
	now := useTrackerWith(t, FlagOptions{RolloutTimeout: 0})

	obj := newTestObject(nil)
	obj.ResourceVersion = "1"
	for i, wait := range []time.Duration{0, 100 * time.Hour} {
		*now = now.Add(wait)
		var err error
		if i == 1 {
			err = errors.New("boom")
		}
		_, endSpan := ReconcileSpan(context.Background(), "dpudeployment", obj, WorkflowRoot())
		endSpan(&err)
	}

	workflow := newSpanContextForObject(obj, "")
	spans := exporter.GetSpans()
	require.Len(t, spans, 2)
	for _, s := range spans {
		require.Equal(t, workflow.TraceID(), s.SpanContext.TraceID())
	}
}

func TestReconcileSpan_LateIdleReconcileThatFailsGetsLinkedTrace(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)
	now := useTrackerWith(t, FlagOptions{RolloutTimeout: time.Minute})

	obj := newTestObject(nil)
	obj.ResourceVersion = "1"
	for i, wait := range []time.Duration{0, 2 * time.Minute} {
		*now = now.Add(wait)
		var err error
		if i == 1 {
			err = errors.New("boom")
		}
		_, endSpan := ReconcileSpan(context.Background(), "dpudeployment", obj, WorkflowRoot())
		endSpan(&err)
	}

	workflow := newSpanContextForObject(obj, "")
	spans := exporter.GetSpans()
	require.Len(t, spans, 2)
	late := spans[1]
	require.True(t, attrValue(t, late.Attributes, "reconcile.idle_at_start").AsBool())
	require.NotEqual(t, workflow.TraceID(), late.SpanContext.TraceID(), "a late reconcile does not rejoin the workflow")
	require.False(t, late.Parent.IsValid())
	require.Len(t, late.Links, 1)
	require.Equal(t, workflow.TraceID(), late.Links[0].SpanContext.TraceID())
}

func TestReconcileSpan_IdleIsPerController(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)
	useTracker(t)

	obj := newTestObject(nil)
	obj.ResourceVersion = "1"
	for _, controller := range []string{"a", "a", "b"} {
		_, endSpan := ReconcileSpan(context.Background(), controller, obj)
		endSpan(nil)
	}

	require.Equal(t, []string{"a.reconcile", "b.reconcile"}, spanNames(exporter),
		"the second reconcile of a is idle, a success of a does not make b idle")
}

func TestNewSpanContextForObject_FlagFollowsTheSamplingRatio(t *testing.T) {
	obj := newTestObject(nil)
	require.True(t, newSpanContextForObject(obj, "").IsSampled(), "without a tracker every trace is kept")

	for percent, want := range map[int]bool{100: true, 0: false} {
		reconciles.Store(newTracker(FlagOptions{SamplingPercent: percent}))
		require.Equal(t, want, newSpanContextForObject(obj, "").IsSampled(), "%d percent", percent)
	}
	t.Cleanup(func() { reconciles.Store(nil) })
}

func TestReconcileSpan_ExternalTraceKeepsTheDecisionOfItsOwner(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
		sdktrace.WithIDGenerator(idGenerator{}),
		sdktrace.WithSampler(newSampler(0)),
	))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	reconciles.Store(newTracker(FlagOptions{SamplingPercent: 0}))
	t.Cleanup(func() { reconciles.Store(nil) })

	sampled := newTestObject(map[string]string{TraceParentAnnotation: "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"})
	unsampled := newTestObject(map[string]string{TraceParentAnnotation: "00-1af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-00"})
	derived := newTestObject(nil)
	for _, obj := range []*corev1.ConfigMap{sampled, unsampled, derived} {
		_, endSpan := ReconcileSpan(context.Background(), "dpuservice", obj, WorkflowRoot())
		endSpan(nil)
	}

	spans := exporter.GetSpans()
	require.Len(t, spans, 1, "at 0 percent only the trace its owner sampled is kept")
	require.Equal(t, "0af7651916cd43dd8448eb211c80319c", spans[0].SpanContext.TraceID().String())
}

func TestReconcileSpan_ReconcileAfterTheTimeoutStartsLinkedTrace(t *testing.T) {
	upstream := "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	tests := []struct {
		name        string
		controller  string
		annotations map[string]string
		opts        []ReconcileOption
		wantParent  func() trace.SpanContext
	}{
		{
			name:       "derived workflow",
			controller: "dpudeployment",
			opts:       []ReconcileOption{WorkflowRoot()},
			wantParent: func() trace.SpanContext { return newSpanContextForObject(newTestObject(nil), "") },
		},
		{
			name:        "upstream traceparent",
			controller:  "dpuservice",
			annotations: map[string]string{TraceParentAnnotation: upstream},
			wantParent: func() trace.SpanContext {
				return trace.SpanContextFromContext(ContextFromObject(context.Background(), newTestObject(map[string]string{TraceParentAnnotation: upstream})))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exporter := useInMemoryTracerProvider(t)
			now := useTrackerWith(t, FlagOptions{RolloutTimeout: time.Minute})

			var lateCtx context.Context
			for i, wait := range []time.Duration{0, 30 * time.Second, time.Minute} {
				*now = now.Add(wait)
				obj := newTestObject(tt.annotations)
				obj.ResourceVersion = string(rune('1' + i))
				ctx, endSpan := ReconcileSpan(context.Background(), tt.controller, obj, tt.opts...)
				lateCtx = ctx
				endSpan(nil)
			}

			workflow := tt.wantParent()
			spans := exporter.GetSpans()
			require.Len(t, spans, 3)
			for _, s := range spans[:2] {
				require.Equal(t, workflow.TraceID(), s.SpanContext.TraceID(), "within the timeout the reconcile joins the workflow")
				require.Equal(t, workflow.SpanID(), s.Parent.SpanID())
				require.Empty(t, s.Links)
			}
			late := spans[2]
			require.NotEqual(t, workflow.TraceID(), late.SpanContext.TraceID(), "after the timeout the reconcile starts a trace of its own")
			require.False(t, late.Parent.IsValid())
			require.Len(t, late.Links, 1)
			require.Equal(t, workflow.TraceID(), late.Links[0].SpanContext.TraceID())
			require.Equal(t, workflow.SpanID(), late.Links[0].SpanContext.SpanID())
			require.Equal(t, workflow.TraceID().String(), attrValue(t, late.Attributes, "workflow.id").AsString(), "the workflow ID still finds it")
			_, ok := lateCtx.Value(incomingKey{}).(incoming)
			require.True(t, ok, "the late reconcile still belongs to the workflow, for ObserveRollout and InjectIntoObject")
		})
	}
}
