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

// Package tracing provides shared OpenTelemetry helpers for DPF controllers: extracting W3C
// trace context persisted on Kubernetes objects, starting/ending spans, and bootstrapping the
// OTLP exporter each controller binary registers as its global TracerProvider (see provider.go).
package tracing

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"time"

	"github.com/nvidia/doca-platform/internal/digest"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
	"go.opentelemetry.io/otel/trace"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// TraceParentAnnotation carries the W3C parent context a user or consumer sets on an object.
	// DPF never writes it, and it wins over the generated one.
	TraceParentAnnotation = "tracing.dpu.nvidia.com/traceparent"
	// TraceStateAnnotation carries optional vendor-specific W3C trace state of the consumer.
	TraceStateAnnotation = "tracing.dpu.nvidia.com/tracestate"
	// GeneratedTraceParentAnnotation carries the W3C parent context DPF passes on to the objects it
	// creates, across a Kubernetes watch boundary.
	GeneratedTraceParentAnnotation = "tracing.dpu.nvidia.com/generated-traceparent"
	// GeneratedTraceStateAnnotation carries the trace state that goes with the generated parent.
	GeneratedTraceStateAnnotation = "tracing.dpu.nvidia.com/generated-tracestate"

	tracerName = "github.com/nvidia/doca-platform"

	// attrWorkflowID is the attribute holding the trace ID of the workflow a span belongs to.
	attrWorkflowID = "workflow.id"
	// traceParentKey and traceStateKey are the W3C header names the carrier uses.
	traceParentKey = "traceparent"
	traceStateKey  = "tracestate"
)

// propagator is an empty struct without state, so concurrent reconcile workers can share it.
var propagator = propagation.TraceContext{}

// incomingKey is the Context key holding the incoming workflow of the reconciled object, which
// InjectIntoObject copies onto child objects.
type incomingKey struct{}

// childSpansKey is the Context key holding the counter of the child spans a reconcile started with
// StartSpan.
type childSpansKey struct{}

// countedSpanKey is the Context key holding the countedSpan StartSpan returned the context for.
type countedSpanKey struct{}

// countedSpan is a child span of a reconcile that is counted in childSpans, until MarkUnchanged
// takes it back.
type countedSpan struct {
	span       trace.Span
	childSpans *atomic.Int64
	marked     atomic.Bool
}

// incoming is the workflow a reconcile belongs to.
type incoming struct {
	// spanContext carries the workflow ID as its trace ID and the span the reconciles are children of.
	spanContext trace.SpanContext
	// derived is whether DPF derived the workflow from the object, as it carried no trace context.
	derived bool
	// revision is the revision of the object's inputs the workflow was derived with, see Revision.
	revision string
	// started is when the reconcile started, which is when a rollout seen by it started.
	started time.Time
}

// objectState summarizes the parts of an object a reconcile can change, to tell afterwards what it did.
type objectState struct {
	generation int64
	metadata   string
	status     string
}

func newObjectState(obj client.Object) objectState {
	state := objectState{
		generation: obj.GetGeneration(),
		metadata:   digest.Short(digest.FromObjects(obj.GetLabels(), obj.GetAnnotations(), obj.GetFinalizers()), 12),
	}
	if fields, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj); err == nil {
		state.status = digest.Short(digest.FromObjects(fields["status"]), 12)
	}
	return state
}

// changes returns what a reconcile changed on its object, from the state it started with to after:
// spec when the generation moved, metadata for labels, annotations or finalizers, and status. The
// parts are joined with a plus, or none when nothing changed.
func (s objectState) changes(after objectState) string {
	var parts []string
	if s.generation != after.generation {
		parts = append(parts, "spec")
	}
	if s.metadata != after.metadata {
		parts = append(parts, "metadata")
	}
	if s.status != after.status {
		parts = append(parts, "status")
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, "+")
}

// ReconcileOption configures ReconcileSpan.
type ReconcileOption func(*reconcileOptions)

type reconcileOptions struct {
	workflowRoot bool
	revision     string
	// idle is set by ReconcileSpan: the reconcile starts at the state the last successful one left behind.
	idle bool
}

// Revision identifies the inputs of the object that can change without a new generation of the
// object itself, such as the generations of the objects it references. A new revision starts a new
// workflow, so a change of an input is traced like a change of the object. It only applies to a
// workflow derived by DPF.
func Revision(revision string) ReconcileOption {
	return func(o *reconcileOptions) { o.revision = revision }
}

// WorkflowRoot marks the controller of an object whose change starts a workflow, such as a
// DPUDeployment. The reconciles of the objects it creates become children of the workflow's root
// span, which the controller exports with ObserveRollout. Without it, an object that carries no trace
// context starts a trace of its own with every reconcile, as nothing would export the root span of a
// workflow derived from it.
func WorkflowRoot() ReconcileOption {
	return func(o *reconcileOptions) { o.workflowRoot = true }
}

// ContextFromObject returns ctx carrying the W3C trace context found on the trace annotations of
// obj, or ctx unchanged when there is none. The annotations a consumer set win over the generated ones.
func ContextFromObject(ctx context.Context, obj client.Object) context.Context {
	annotations := obj.GetAnnotations()
	traceParent, traceState := annotations[TraceParentAnnotation], annotations[TraceStateAnnotation]
	if traceParent == "" {
		traceParent, traceState = annotations[GeneratedTraceParentAnnotation], annotations[GeneratedTraceStateAnnotation]
	}
	if traceParent == "" {
		return ctx
	}
	carrier := propagation.MapCarrier{traceParentKey: traceParent}
	if traceState != "" {
		carrier[traceStateKey] = traceState
	}
	return propagator.Extract(ctx, carrier)
}

// InjectIntoObject copies the trace context the reconciled object arrived with onto the generated
// trace annotations of obj, and leaves the ones a consumer set alone. Call it before Create, Update
// or Patch of a child that another controller reconciles, so its span joins the same trace. The
// incoming value is copied unchanged, not the per-reconcile span, so repeated reconciles cause no
// updates. A derived workflow is only copied by a WorkflowRoot controller. No-op when ctx did not
// come from ReconcileSpan. With tracing off it writes nothing and removes the generated annotations
// from obj, so the write that follows also clears them from an object that carried them before.
func InjectIntoObject(ctx context.Context, obj client.Object) {
	if !enabled() {
		removeGeneratedAnnotations(obj)
		return
	}
	in, ok := ctx.Value(incomingKey{}).(incoming)
	if !ok {
		return
	}
	carrier := propagation.MapCarrier{}
	propagator.Inject(trace.ContextWithRemoteSpanContext(ctx, in.spanContext), carrier)
	// Inject writes nothing for an invalid span context, then obj stays untouched.
	if len(carrier) == 0 {
		return
	}
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string, len(carrier))
	}
	annotations[GeneratedTraceParentAnnotation] = carrier[traceParentKey]
	if v, ok := carrier[traceStateKey]; ok {
		annotations[GeneratedTraceStateAnnotation] = v
	} else {
		// A client that re-applies the parent with a new traceparent and no tracestate would leave
		// the child with the tracestate of the old request. Drop it.
		delete(annotations, GeneratedTraceStateAnnotation)
	}
	obj.SetAnnotations(annotations)
}

// enabled reports whether tracing is set up in this process. Without it nothing is traced and
// nothing is written to the objects.
func enabled() bool {
	return reconciles.Load() != nil
}

// removeGeneratedAnnotations removes the annotations DPF generated from obj. The ones a consumer set stay.
func removeGeneratedAnnotations(obj client.Object) {
	annotations := obj.GetAnnotations()
	_, hasParent := annotations[GeneratedTraceParentAnnotation]
	_, hasState := annotations[GeneratedTraceStateAnnotation]
	if !hasParent && !hasState {
		return
	}
	delete(annotations, GeneratedTraceParentAnnotation)
	delete(annotations, GeneratedTraceStateAnnotation)
	obj.SetAnnotations(annotations)
}

// newSpanContextForObject returns the span context DPF assigns to an object that arrived without a
// traceparent. It is derived from the UID, generation and revision, so repeated reconciles of the
// same change yield the same value and a new generation or revision starts a new workflow. Its span
// ID is the one of the root span, see ObserveRollout. It is invalid without a UID.
func newSpanContextForObject(obj client.Object, revision string) trace.SpanContext {
	if obj.GetUID() == "" {
		return trace.SpanContext{}
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s/%d/%s", obj.GetUID(), obj.GetGeneration(), revision)))
	traceID := trace.TraceID(sum[:16])
	// The sampled flag carries the decision of the sampling ratio, so every controller that reads the
	// context decides alike, and a trace of an external owner keeps the decision of that owner.
	var flags trace.TraceFlags
	if reconciles.Load().keeps(traceID) {
		flags = trace.FlagsSampled
	}
	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     trace.SpanID(sum[16:24]),
		TraceFlags: flags,
		Remote:     true,
	})
}

// ReconcileSpan starts the span covering one reconcile of obj, as a child of the span in the trace
// context on obj (set by an upstream caller or InjectIntoObject). The trace is flat, as DPF cannot
// tell which reconcile caused another. Without that context, a WorkflowRoot controller joins the
// workflow derived from obj and any other controller starts a trace of its own.
// A reconcile that starts at the resourceVersion and revision the last successful one left behind
// is idle and is not recorded, so requeues of a stable object stay out of the traces.
// Call it once obj has been fetched and defer the returned function with a pointer to the named
// error return, so the span records the error and covers the whole reconcile:
//
//	ctx, endSpan := tracing.ReconcileSpan(ctx, controllerName, obj)
//	defer endSpan(&reterr)
//
// The function accepts a nil pointer for callers that have no error to report. With tracing off
// it returns at once, the reconcile is not touched.
func ReconcileSpan(ctx context.Context, controller string, obj client.Object, opts ...ReconcileOption) (context.Context, func(*error)) {
	if !enabled() {
		return ctx, func(*error) {}
	}
	var o reconcileOptions
	for _, opt := range opts {
		opt(&o)
	}
	startResourceVersion, start := obj.GetResourceVersion(), time.Now()
	// Decided once, as a span that is not recording may also have been dropped by sampling.
	o.idle = reconciles.Load().idle(controller, obj.GetUID(), reconcileState(obj, o.revision))
	ctx, span := traceObject(ctx, controller, obj, o)
	ctx = withTraceLogger(ctx, span)
	var before objectState
	if span.IsRecording() {
		before = newObjectState(obj)
	}
	var childSpans atomic.Int64
	ctx = context.WithValue(ctx, childSpansKey{}, &childSpans)
	return ctx, func(errp *error) {
		var err error
		if errp != nil {
			err = *errp
		}
		if span.IsRecording() {
			changes := before.changes(newObjectState(obj))
			span.SetAttributes(attribute.String("reconcile.changes", changes))
			// A reconcile that changed nothing, did not fail and started no child span only says that it ran.
			if changes == "none" && err == nil && childSpans.Load() == 0 && reconciles.Load().skipUnchanged() {
				span.SetAttributes(attribute.Bool(dropAttribute, true))
			}
		}
		EndSpan(span, err)
		// An idle reconcile is not recorded, unless it failed or changed obj after all.
		if o.idle && (err != nil || obj.GetResourceVersion() != startResourceVersion) {
			recordIdleReconcile(ctx, controller, obj, o.revision, start, err)
		}
		reconciles.Load().finish(controller, obj.GetUID(), reconcileState(obj, o.revision), err == nil)
	}
}

// reconcileState identifies what a reconcile of obj starts from, for the idle check.
func reconcileState(obj client.Object, revision string) string {
	return obj.GetResourceVersion() + "/" + revision
}

// withTraceLogger returns ctx with a logger that adds the trace and span ID of span to every line,
// so a log line leads to its trace. The traceID key is the one the Loki derived field matches. A
// span that is not recording, as with tracing disabled, leaves ctx unchanged.
func withTraceLogger(ctx context.Context, span trace.Span) context.Context {
	if !span.IsRecording() {
		return ctx
	}
	sc := span.SpanContext()
	logger := ctrllog.FromContext(ctx).WithValues("traceID", sc.TraceID().String(), "spanID", sc.SpanID().String())
	return ctrllog.IntoContext(ctx, logger)
}

// traceObject starts the reconcile span and returns it, see ReconcileSpan.
func traceObject(ctx context.Context, controller string, obj client.Object, opts reconcileOptions) (context.Context, trace.Span) {
	in := incomingContext(ctx, obj, opts.revision)
	// A span in ctx must not become the parent, the workflow decides it.
	rootCtx := trace.ContextWithSpanContext(ctx, trace.SpanContext{})
	attrs := make([]attribute.KeyValue, 0, 8)
	var links []trace.Link
	if in.spanContext.IsValid() {
		attrs = append(attrs, attribute.String(attrWorkflowID, in.spanContext.TraceID().String()))
		// The root span of a derived workflow is only exported by a WorkflowRoot controller.
		joinWorkflow := !in.derived || opts.workflowRoot
		if joinWorkflow {
			in.started = reconciles.Load().clock()
			rootCtx = context.WithValue(rootCtx, incomingKey{}, in)
		}
		switch {
		case opts.idle:
			// An unsampled parent keeps the reconcile span and the spans started below it from being recorded.
			rootCtx = trace.ContextWithRemoteSpanContext(rootCtx, in.spanContext.WithTraceFlags(0))
		case joinWorkflow:
			rootCtx, links = attachToWorkflow(rootCtx, in)
		}
	} else {
		// Objects read from the API server always have a UID, tracing must not fail the reconcile.
		ctrllog.FromContext(ctx).Error(errors.New("object has no UID"), "Unable to derive a workflow ID, reconciling without one",
			"controller", controller, "name", obj.GetName(), "namespace", obj.GetNamespace())
	}
	return otel.Tracer(tracerName).Start(rootCtx, controller+".reconcile",
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(append(attrs, objectAttributes(controller, obj)...)...),
		trace.WithLinks(links...),
	)
}

// recordIdleReconcile exports the span of a reconcile that was thought to be idle when it started
// but failed or changed obj. The spans started below it are lost.
func recordIdleReconcile(ctx context.Context, controller string, obj client.Object, revision string, start time.Time, err error) {
	attrs := append(objectAttributes(controller, obj), attribute.Bool("reconcile.idle_at_start", true))
	parentCtx := trace.ContextWithSpanContext(ctx, trace.SpanContext{})
	if workflow := incomingContext(ctx, obj, revision).spanContext; workflow.IsValid() {
		attrs = append(attrs, attribute.String(attrWorkflowID, workflow.TraceID().String()))
	}
	var links []trace.Link
	if in, ok := ctx.Value(incomingKey{}).(incoming); ok {
		parentCtx, links = attachToWorkflow(parentCtx, in)
	}
	_, span := otel.Tracer(tracerName).Start(parentCtx, controller+".reconcile",
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithTimestamp(start),
		trace.WithAttributes(attrs...),
		trace.WithLinks(links...),
	)
	EndSpan(span, err)
}

// attachToWorkflow returns how a reconcile joins the workflow in: as a child of its span or, once the
// workflow has collected reconciles for long enough, as the start of a trace of its own with a link to
// it, so no trace grows without bound.
func attachToWorkflow(ctx context.Context, in incoming) (context.Context, []trace.Link) {
	if reconciles.Load().expired(in.spanContext.TraceID()) {
		return ctx, []trace.Link{{SpanContext: in.spanContext}}
	}
	return trace.ContextWithRemoteSpanContext(ctx, in.spanContext), nil
}

// incomingContext returns the workflow of obj, the one on obj or else the one derived from it. Its
// span context is invalid when obj has no UID yet.
func incomingContext(ctx context.Context, obj client.Object, revision string) incoming {
	// A span in ctx, such as the reconcile span, is not the workflow of obj.
	noSpanCtx := trace.ContextWithSpanContext(ctx, trace.SpanContext{})
	if sc := trace.SpanContextFromContext(ContextFromObject(noSpanCtx, obj)); sc.IsValid() {
		return incoming{spanContext: sc, revision: revision}
	}
	return incoming{spanContext: newSpanContextForObject(obj, revision), derived: true, revision: revision}
}

// objectAttributes returns the attributes that identify obj on the spans of controller.
func objectAttributes(controller string, obj client.Object) []attribute.KeyValue {
	// Typed objects from the client usually have empty TypeMeta.
	kind := obj.GetObjectKind().GroupVersionKind().Kind
	if kind == "" {
		kind = reflect.TypeOf(obj).Elem().Name()
	}
	attrs := []attribute.KeyValue{
		attribute.String("controller", controller),
		attribute.String("k8s.resource.kind", kind),
		attribute.String("k8s.namespace.name", obj.GetNamespace()),
		attribute.String("k8s.object.name", obj.GetName()),
		attribute.String("k8s.object.uid", string(obj.GetUID())),
		attribute.Int64("k8s.object.generation", obj.GetGeneration()),
	}
	// The controller owner lets spans be queried by the object that created this one, and the chain of owners be followed.
	if owner := metav1.GetControllerOf(obj); owner != nil {
		attrs = append(attrs,
			attribute.String("k8s.owner.kind", owner.Kind),
			attribute.String("k8s.owner.name", owner.Name),
		)
	}
	return attrs
}

// AddAttributes adds controller-specific data to the span currently stored in ctx.
// Callers should avoid secrets and sanitize URLs before adding them.
func AddAttributes(ctx context.Context, attributes ...attribute.KeyValue) {
	trace.SpanFromContext(ctx).SetAttributes(attributes...)
}

// StartSpan starts a child span for a meaningful operation within a reconcile, such as a Redfish
// request. The caller must call EndSpan (or span.End()) on the returned span.
func StartSpan(ctx context.Context, operation string, attributes ...attribute.KeyValue) (context.Context, trace.Span) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, operation, trace.WithAttributes(attributes...))
	if childSpans, ok := ctx.Value(childSpansKey{}).(*atomic.Int64); ok {
		childSpans.Add(1)
		ctx = context.WithValue(ctx, countedSpanKey{}, &countedSpan{span: span, childSpans: childSpans})
	}
	return ctx, span
}

// EndSpan records an operation error, when present, and ends span. It follows the OpenTelemetry
// convention for recording errors: the error status and the error.type attribute.
func EndSpan(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetAttributes(semconv.ErrorType(err))
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}

// MarkUnchanged tells that the operation of the span in ctx changed nothing, such as a read-only
// check or an apply that found the object up to date. While unchanged reconciles are skipped, the span
// is not exported and no longer keeps its reconcile alive, so a reconcile that only ran such operations
// is dropped like one without child spans. Call it with the context StartSpan returned, for an
// operation that did not fail. It does nothing for any other context.
func MarkUnchanged(ctx context.Context) {
	counted, ok := ctx.Value(countedSpanKey{}).(*countedSpan)
	if !ok || !reconciles.Load().skipUnchanged() || !counted.marked.CompareAndSwap(false, true) {
		return
	}
	counted.span.SetAttributes(attribute.Bool(dropAttribute, true))
	counted.childSpans.Add(-1)
}
