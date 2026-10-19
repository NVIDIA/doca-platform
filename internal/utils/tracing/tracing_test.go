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
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	testTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	testSpanID  = "00f067aa0ba902b7"
)

func newTestObject(annotations map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-obj",
			Namespace:   "test-ns",
			UID:         types.UID("test-uid"),
			Generation:  3,
			Annotations: annotations,
		},
	}
}

// useInMemoryTracerProvider installs an in-memory exporter as the global TracerProvider for the
// duration of the test and returns it so assertions can inspect the exported spans.
func useInMemoryTracerProvider(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	return exporter
}

func attrValue(t *testing.T, attrs []attribute.KeyValue, key string) attribute.Value {
	t.Helper()
	set := attribute.NewSet(attrs...)
	v, ok := set.Value(attribute.Key(key))
	require.True(t, ok, "expected attribute %q to be present", key)
	return v
}

func TestReconcileSpan(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)

	obj := newTestObject(nil)
	ctx, span := traceObject(context.Background(), "dpuset", obj)
	require.True(t, span.SpanContext().IsValid())
	require.NotNil(t, trace.SpanFromContext(ctx))
	span.End()

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	got := spans[0]
	require.Equal(t, "dpuset.reconcile", got.Name)
	require.Equal(t, trace.SpanKindConsumer, got.SpanKind)
	require.True(t, got.Parent.IsValid(), "expected the derived trace when the object carries no trace context annotation")

	require.Equal(t, "dpuset", attrValue(t, got.Attributes, "controller").AsString())
	require.Equal(t, "ConfigMap", attrValue(t, got.Attributes, "k8s.resource.kind").AsString())
	require.Equal(t, "test-ns", attrValue(t, got.Attributes, "k8s.namespace.name").AsString())
	require.Equal(t, "test-obj", attrValue(t, got.Attributes, "k8s.object.name").AsString())
	require.Equal(t, "test-uid", attrValue(t, got.Attributes, "k8s.object.uid").AsString())
	require.Equal(t, int64(3), attrValue(t, got.Attributes, "k8s.object.generation").AsInt64())
}

func TestReconcileSpan_RecordsReturnedError(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)

	reconcile := func() (reterr error) {
		_, endSpan := ReconcileSpan(context.Background(), "dpuset", newTestObject(nil))
		defer endSpan(&reterr)
		return errors.New("boom")
	}
	require.Error(t, reconcile())

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Error, spans[0].Status.Code)
	require.Equal(t, "boom", spans[0].Status.Description)
}

func TestReconcileSpan_NilErrorPointer(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)

	_, endSpan := ReconcileSpan(context.Background(), "dpuset", newTestObject(nil))
	endSpan(nil)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Unset, spans[0].Status.Code)
}

func TestTraceObject_ControllerOwnerBecomesAttributes(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)

	isController := true
	obj := newTestObject(nil)
	obj.OwnerReferences = []metav1.OwnerReference{
		{Kind: "ConfigMapSet", Name: "not-the-controller"},
		{Kind: "DPUSet", Name: "foo-a", Controller: &isController},
	}
	_, span := traceObject(context.Background(), "dpu", obj)
	span.End()

	attrs := exporter.GetSpans()[0].Attributes
	require.Equal(t, "DPUSet", attrValue(t, attrs, "k8s.owner.kind").AsString())
	require.Equal(t, "foo-a", attrValue(t, attrs, "k8s.owner.name").AsString())
}

func TestTraceObject_NoOwnerAttributesWithoutControllerOwner(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)

	obj := newTestObject(nil)
	obj.OwnerReferences = []metav1.OwnerReference{{Kind: "DPUSet", Name: "foo-a"}}
	_, span := traceObject(context.Background(), "dpu", obj)
	span.End()

	set := attribute.NewSet(exporter.GetSpans()[0].Attributes...)
	_, hasKind := set.Value("k8s.owner.kind")
	_, hasName := set.Value("k8s.owner.name")
	require.False(t, hasKind || hasName, "an owner that is not the controller must not be reported")
}

func TestTraceObject_KindFallsBackToGoType(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)

	obj := newTestObject(nil)
	obj.TypeMeta = metav1.TypeMeta{}
	_, span := traceObject(context.Background(), "dpuset", obj)
	span.End()

	require.Equal(t, "ConfigMap", attrValue(t, exporter.GetSpans()[0].Attributes, "k8s.resource.kind").AsString())
}

func TestTraceObject_UsesParentFromAnnotation(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)

	obj := newTestObject(map[string]string{
		TraceParentAnnotation: fmt.Sprintf("00-%s-%s-01", testTraceID, testSpanID),
	})

	_, span := traceObject(context.Background(), "dpuset", obj)
	span.End()

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	require.Equal(t, testTraceID, spans[0].Parent.TraceID().String())
	require.Equal(t, testSpanID, spans[0].Parent.SpanID().String())
	require.Equal(t, testTraceID, spans[0].SpanContext.TraceID().String(), "child span should share the parent's trace ID")
}

func TestContextFromObject_ExtractsTraceparent(t *testing.T) {
	obj := newTestObject(map[string]string{
		TraceParentAnnotation: fmt.Sprintf("00-%s-%s-01", testTraceID, testSpanID),
	})

	ctx := ContextFromObject(context.Background(), obj)
	sc := trace.SpanContextFromContext(ctx)
	require.True(t, sc.IsValid())
	require.True(t, sc.IsRemote())
	require.Equal(t, testTraceID, sc.TraceID().String())
	require.Equal(t, testSpanID, sc.SpanID().String())
}

func TestContextFromObject_NoAnnotation(t *testing.T) {
	ctx := ContextFromObject(context.Background(), newTestObject(nil))
	require.False(t, trace.SpanContextFromContext(ctx).IsValid())
}

func TestInjectIntoObject_RoundTripsWithContextFromObject(t *testing.T) {
	useInMemoryTracerProvider(t)

	traceparent := fmt.Sprintf("00-%s-%s-01", testTraceID, testSpanID)
	parent := newTestObject(map[string]string{TraceParentAnnotation: traceparent})
	ctx, span := traceObject(context.Background(), "dpudeployment", parent)
	defer span.End()

	child := newTestObject(nil)
	InjectIntoObject(ctx, child)
	// The incoming value is copied unchanged, not replaced by the reconcile span.
	require.Equal(t, traceparent, child.Annotations[TraceParentAnnotation])

	// A controller reconciling child later should pick up this trace as its parent.
	sc := trace.SpanContextFromContext(ContextFromObject(context.Background(), child))
	require.True(t, sc.IsValid())
	require.Equal(t, testTraceID, sc.TraceID().String())
	require.Equal(t, testSpanID, sc.SpanID().String())
}

func TestInjectIntoObject_StableAcrossReconciles(t *testing.T) {
	useInMemoryTracerProvider(t)

	parent := newTestObject(map[string]string{
		TraceParentAnnotation: fmt.Sprintf("00-%s-%s-01", testTraceID, testSpanID),
	})
	got := make([]string, 0, 2)
	for range 2 {
		ctx, span := traceObject(context.Background(), "dpudeployment", parent)
		child := newTestObject(nil)
		InjectIntoObject(ctx, child)
		span.End()
		got = append(got, child.Annotations[TraceParentAnnotation])
	}
	require.Equal(t, got[0], got[1], "annotation must not change between reconciles")
}

func TestTraceObject_DerivesTraceWithoutAnnotation(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)

	obj := newTestObject(nil)
	for range 2 {
		_, span := traceObject(context.Background(), "dpuset", obj)
		span.End()
	}

	spans := exporter.GetSpans()
	require.Len(t, spans, 2)
	require.True(t, spans[0].Parent.IsValid())
	require.Equal(t, spans[0].Parent, spans[1].Parent, "reconciles of one generation must share the derived parent")
	require.Equal(t, spans[0].SpanContext.TraceID(), spans[1].SpanContext.TraceID())
	require.NotEqual(t, spans[0].SpanContext.SpanID(), spans[1].SpanContext.SpanID())
}

func TestTraceObject_DerivedTraceFollowsUIDAndGeneration(t *testing.T) {
	traceID := func(obj *corev1.ConfigMap) trace.TraceID {
		sc := newSpanContextForObject(obj)
		require.True(t, sc.IsValid())
		return sc.TraceID()
	}

	base := newTestObject(nil)
	require.Equal(t, traceID(base), traceID(newTestObject(nil)), "same UID and generation must derive the same trace")

	otherGeneration := newTestObject(nil)
	otherGeneration.Generation = base.Generation + 1
	require.NotEqual(t, traceID(base), traceID(otherGeneration), "a new generation starts a new trace")

	otherUID := newTestObject(nil)
	otherUID.UID = types.UID("other-uid")
	require.NotEqual(t, traceID(base), traceID(otherUID))
}

func TestTraceObject_NoDerivedTraceWithoutUID(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)

	obj := newTestObject(nil)
	obj.UID = ""
	_, span := traceObject(context.Background(), "dpuset", obj)
	span.End()

	require.False(t, exporter.GetSpans()[0].Parent.IsValid())
}

func TestTraceObject_AnnotationWinsOverDerivedTrace(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)

	obj := newTestObject(map[string]string{
		TraceParentAnnotation: fmt.Sprintf("00-%s-%s-01", testTraceID, testSpanID),
	})
	_, span := traceObject(context.Background(), "dpuset", obj)
	span.End()

	require.Equal(t, testTraceID, exporter.GetSpans()[0].SpanContext.TraceID().String())
}

func TestInjectIntoObject_CopiesDerivedTraceWithoutAnnotation(t *testing.T) {
	useInMemoryTracerProvider(t)

	parent := newTestObject(nil)
	derived := newSpanContextForObject(parent)
	require.True(t, derived.IsValid())

	got := make([]string, 0, 2)
	for range 2 {
		ctx, span := traceObject(context.Background(), "dpudeployment", parent)
		child := newTestObject(nil)
		InjectIntoObject(ctx, child)
		span.End()
		got = append(got, child.Annotations[TraceParentAnnotation])
	}

	require.Equal(t, got[0], got[1], "derived annotation must not change between reconciles")
	sc := trace.SpanContextFromContext(ContextFromObject(context.Background(), &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{TraceParentAnnotation: got[0]}},
	}))
	require.Equal(t, derived.TraceID(), sc.TraceID())
	require.Equal(t, derived.SpanID(), sc.SpanID())
}

func TestInjectIntoObject_PreservesExistingAnnotations(t *testing.T) {
	useInMemoryTracerProvider(t)

	parent := newTestObject(map[string]string{
		TraceParentAnnotation: fmt.Sprintf("00-%s-%s-01", testTraceID, testSpanID),
	})
	ctx, span := traceObject(context.Background(), "dpudeployment", parent)
	defer span.End()

	child := newTestObject(map[string]string{"unrelated": "keep-me"})
	InjectIntoObject(ctx, child)
	require.Equal(t, "keep-me", child.Annotations["unrelated"])
	require.NotEmpty(t, child.Annotations[TraceParentAnnotation])
}

func TestInjectIntoObject_ClearsStaleTracestate(t *testing.T) {
	useInMemoryTracerProvider(t)

	parent := newTestObject(map[string]string{
		TraceParentAnnotation: fmt.Sprintf("00-%s-%s-01", testTraceID, testSpanID),
	})
	ctx, span := traceObject(context.Background(), "dpudeployment", parent)
	defer span.End()

	child := newTestObject(map[string]string{TraceStateAnnotation: "vendor=stale"})
	InjectIntoObject(ctx, child)
	require.NotContains(t, child.Annotations, TraceStateAnnotation)
}

func TestInjectIntoObject_NoopWithoutActiveSpan(t *testing.T) {
	obj := newTestObject(nil)
	InjectIntoObject(context.Background(), obj)
	require.Empty(t, obj.Annotations[TraceParentAnnotation], "no active span in ctx should produce no annotation")
}

func TestStartSpanEndSpan_RecordsError(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)

	_, span := StartSpan(context.Background(), "redfish.get")
	EndSpan(span, fmt.Errorf("boom"))

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	require.Equal(t, "redfish.get", spans[0].Name)
	require.Equal(t, codes.Error, spans[0].Status.Code)
	require.Equal(t, "boom", spans[0].Status.Description)
	require.Len(t, spans[0].Events, 1)
	require.Equal(t, "exception", spans[0].Events[0].Name)
}

func TestEndSpan_NoError(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)

	_, span := StartSpan(context.Background(), "op")
	EndSpan(span, nil)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	require.Equal(t, codes.Unset, spans[0].Status.Code)
}

func TestAddAttributes(t *testing.T) {
	exporter := useInMemoryTracerProvider(t)

	ctx, span := StartSpan(context.Background(), "op")
	AddAttributes(ctx, attribute.String("foo", "bar"))
	span.End()

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	require.Equal(t, "bar", attrValue(t, spans[0].Attributes, "foo").AsString())
}

func TestNewTracerProvider_EmptyEndpointIsError(t *testing.T) {
	_, err := NewTracerProvider(context.Background(), "svc", "", TransportHTTP, "")
	require.Error(t, err)
}

func TestNewTracerProvider_DoesNotRegisterGlobally(t *testing.T) {
	prev := otel.GetTracerProvider()
	tp, err := NewTracerProvider(context.Background(), "svc", "collector:4317", TransportHTTP, "")
	require.NoError(t, err)
	require.NoError(t, tp.Shutdown(context.Background()))
	require.Same(t, prev, otel.GetTracerProvider())
}

func TestNewTracerProvider_UnsupportedTransport(t *testing.T) {
	_, err := NewTracerProvider(context.Background(), "svc", "collector:4317", Transport("carrier-pigeon"), "")
	require.Error(t, err)
}

func TestParseEndpoint(t *testing.T) {
	cases := []struct {
		name       string
		endpoint   string
		wantTarget string
		wantSecure bool
		wantErr    bool
	}{
		{name: "empty", endpoint: "", wantErr: true},
		{name: "plain", endpoint: "collector:4317", wantTarget: "collector:4317", wantSecure: false},
		{name: "http scheme", endpoint: "http://collector:4318", wantTarget: "collector:4318", wantSecure: false},
		{name: "https scheme", endpoint: "https://collector:4317", wantTarget: "collector:4317", wantSecure: true},
		{name: "trailing slash", endpoint: "https://collector:4318/", wantTarget: "collector:4318", wantSecure: true},
		{name: "plain ipv6", endpoint: "[::1]:4317", wantTarget: "[::1]:4317", wantSecure: false},
		{name: "https ipv6", endpoint: "https://[::1]:4317", wantTarget: "[::1]:4317", wantSecure: true},
		{name: "path", endpoint: "http://collector:4318/v1/traces", wantErr: true},
		{name: "unsupported scheme", endpoint: "ftp://collector:21", wantErr: true},
		{name: "no host", endpoint: "http://", wantErr: true},
		{name: "invalid", endpoint: "http://collector:port", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target, secure, err := parseEndpoint(tc.endpoint)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantTarget, target)
			require.Equal(t, tc.wantSecure, secure)
		})
	}
}
