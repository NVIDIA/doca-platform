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
	"maps"
	"reflect"
	"slices"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// TraceParentAnnotation carries the W3C parent context across a Kubernetes watch boundary.
	TraceParentAnnotation = "tracing.dpu.nvidia.com/traceparent"
	// TraceStateAnnotation carries optional vendor-specific W3C trace state.
	TraceStateAnnotation = "tracing.dpu.nvidia.com/tracestate"

	tracerName = "github.com/nvidia/doca-platform"
)

// propagator is an empty struct without state, so concurrent reconcile workers can share it.
var propagator = propagation.TraceContext{}

// incomingKey is the Context key holding the SpanContext the reconciled object arrived with, which
// InjectIntoObject copies onto child objects.
type incomingKey struct{}

// annotationCarrier adapts the two W3C trace context annotations to propagation.TextMapCarrier.
type annotationCarrier map[string]string

var _ propagation.TextMapCarrier = annotationCarrier{}

func (c annotationCarrier) Get(key string) string { return c[key] }
func (c annotationCarrier) Set(key, value string) { c[key] = value }
func (c annotationCarrier) Keys() []string        { return slices.Collect(maps.Keys(c)) }

// ContextFromObject returns ctx carrying the W3C trace context found on obj's
// TraceParentAnnotation/TraceStateAnnotation. When the annotation is absent, ctx is returned
// unchanged. ReconcileSpan then falls back to a trace derived from the object itself.
func ContextFromObject(ctx context.Context, obj client.Object) context.Context {
	traceParent := obj.GetAnnotations()[TraceParentAnnotation]
	if traceParent == "" {
		return ctx
	}
	carrier := annotationCarrier{"traceparent": traceParent}
	if traceState := obj.GetAnnotations()[TraceStateAnnotation]; traceState != "" {
		carrier["tracestate"] = traceState
	}
	return propagator.Extract(ctx, carrier)
}

// InjectIntoObject copies the trace context the reconciled object arrived with (as seen by
// ReconcileSpan) onto obj's TraceParentAnnotation/TraceStateAnnotation. Call it before
// Create/Update/Patch on any child object another controller's ReconcileSpan will later read trace
// context from, e.g. a DPUDeployment reconcile creating the DPUServices it owns, so that
// controller's span continues the upstream trace. The incoming value is copied unchanged instead
// of stamping the per-reconcile span, so repeated reconciles produce identical annotations and do
// not cause updates. When the reconciled object carried none, the value derived by ReconcileSpan is
// copied. No-op when ctx did not come from ReconcileSpan.
func InjectIntoObject(ctx context.Context, obj client.Object) {
	incoming, ok := ctx.Value(incomingKey{}).(trace.SpanContext)
	if !ok {
		return
	}
	carrier := annotationCarrier{}
	propagator.Inject(trace.ContextWithRemoteSpanContext(ctx, incoming), carrier)
	// Inject writes nothing for an invalid span context. ReconcileSpan only stores valid ones, so
	// this guards against another writer of the key and keeps the annotations on obj untouched.
	if len(carrier) == 0 {
		return
	}
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string, len(carrier))
	}
	if v, ok := carrier["traceparent"]; ok {
		annotations[TraceParentAnnotation] = v
	}
	if v, ok := carrier["tracestate"]; ok {
		annotations[TraceStateAnnotation] = v
	} else {
		// A client that re-applies the parent with a new traceparent and no tracestate would leave
		// the child with the tracestate of the old request. Drop it.
		delete(annotations, TraceStateAnnotation)
	}
	obj.SetAnnotations(annotations)
}

// newSpanContextForObject returns a deterministic, sampled W3C span context for obj, built from its
// UID and generation. It is the trace DPF starts for an object that arrived without a traceparent
// (the caller is not a tracing-aware client), so that the object and everything created from it
// still share one trace. Being a pure function of UID and generation, repeated reconciles of the
// same generation yield the same value and never change a child's annotation. A spec change bumps
// the generation and so starts a new trace. The span ID is synthetic: no span with that ID is
// exported, so backends show the root as missing. The result is invalid when obj has no UID yet.
func newSpanContextForObject(obj client.Object) trace.SpanContext {
	if obj.GetUID() == "" {
		return trace.SpanContext{}
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s/%d", obj.GetUID(), obj.GetGeneration())))
	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID(sum[:16]),
		SpanID:     trace.SpanID(sum[16:24]),
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
}

// ReconcileSpan starts the span covering one reconcile of obj, using trace context persisted on
// obj when present. When obj carries none, the span continues a trace derived from the object
// itself (see newSpanContextForObject), so DPF still links an object to the children it creates.
// Call it once obj has been fetched and defer the returned function with a pointer to the named
// error return, so the span records the reconcile's error and covers its whole duration:
//
//	ctx, endSpan := tracing.ReconcileSpan(ctx, controllerName, obj)
//	defer endSpan(&reterr)
//
// The function accepts a nil pointer for callers that have no error to report.
func ReconcileSpan(ctx context.Context, controller string, obj client.Object) (context.Context, func(*error)) {
	ctx, span := traceObject(ctx, controller, obj)
	return ctx, func(errp *error) {
		var err error
		if errp != nil {
			err = *errp
		}
		EndSpan(span, err)
	}
}

// traceObject starts the reconcile span and returns it, see ReconcileSpan.
func traceObject(ctx context.Context, controller string, obj client.Object) (context.Context, trace.Span) {
	parentCtx := ContextFromObject(ctx, obj)
	if sc := trace.SpanContextFromContext(parentCtx); sc.IsValid() {
		parentCtx = context.WithValue(parentCtx, incomingKey{}, sc)
	} else if derived := newSpanContextForObject(obj); derived.IsValid() {
		parentCtx = trace.ContextWithRemoteSpanContext(parentCtx, derived)
		parentCtx = context.WithValue(parentCtx, incomingKey{}, derived)
	} else {
		// Objects read from the API server always have a UID, tracing must not fail the reconcile.
		ctrllog.FromContext(ctx).Error(errors.New("object has no UID"), "Unable to derive a trace, reconciling without a parent trace",
			"controller", controller, "name", obj.GetName(), "namespace", obj.GetNamespace())
	}
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
	return otel.Tracer(tracerName).Start(parentCtx, controller+".reconcile",
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(attrs...),
	)
}

// AddAttributes adds controller-specific data to the span currently stored in ctx.
// Callers should avoid secrets and sanitize URLs before adding them.
func AddAttributes(ctx context.Context, attributes ...attribute.KeyValue) {
	trace.SpanFromContext(ctx).SetAttributes(attributes...)
}

// StartSpan starts a child span for a meaningful operation within a reconcile, such as a Redfish
// request. The caller must call EndSpan (or span.End()) on the returned span.
func StartSpan(ctx context.Context, operation string, attributes ...attribute.KeyValue) (context.Context, trace.Span) {
	return otel.Tracer(tracerName).Start(ctx, operation, trace.WithAttributes(attributes...))
}

// EndSpan records an operation error, when present, and ends span.
func EndSpan(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}
