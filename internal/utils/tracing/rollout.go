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
	"slices"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
	"go.opentelemetry.io/otel/trace"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ObserveRollout reports a change of obj and how long it took to become ready. Call it from each
// reconcile of a WorkflowRoot controller's object, once its status is computed, with ready set when
// the object is ready for its current generation. ctx must come from ReconcileSpan.
//
// The rollout starts when the process first sees a generation, or a new Revision of it. When obj
// carries no trace context, an instant <controller>.change span is exported then as a root of the
// trace while the change is in progress. One <controller>.rollout span with the real duration is
// exported when obj becomes ready, after the rollout timeout or when a newer generation replaces it.
// It is the other root and the parent of the reconciles of obj and of what it creates, or a child
// of the span in the trace context of obj. The timeout is only checked when obj is reconciled. An
// object that is ready when first seen is not a rollout, as its start is unknown, so its change span
// is the root of the trace.
func ObserveRollout(ctx context.Context, controller string, obj client.Object, ready bool) {
	in, ok := ctx.Value(incomingKey{}).(incoming)
	if !ok {
		return
	}
	started, finished := reconciles.Load().observeRollout(controller, obj.GetUID(), obj.GetGeneration(), in, ready)
	if started && in.derived {
		// No rollout span will take the span ID the reconciles point at, so the change span takes it.
		noRollout := ready && !slices.ContainsFunc(finished, func(r finishedRollout) bool {
			return r.in.spanContext.SpanID() == in.spanContext.SpanID()
		})
		startChange(ctx, controller, obj, in, noRollout)
	}
	for _, rollout := range finished {
		endRollout(ctx, controller, obj, rollout)
	}
}

// startChange exports an instant <controller>.change span as a root of the trace of the change, so
// the trace has a root while the change is in progress. The rollout span, exported when it ends, is
// the other root and has the real duration. Without a rollout, the change span is the only root and
// takes the span ID of the workflow, which the reconciles point at.
func startChange(ctx context.Context, controller string, obj client.Object, in incoming, noRollout bool) {
	marker := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    in.spanContext.TraceID(),
		SpanID:     newRandomSpanID(),
		TraceFlags: trace.FlagsSampled,
	})
	if noRollout {
		marker = in.spanContext
	}
	rootCtx := context.WithValue(trace.ContextWithSpanContext(ctx, trace.SpanContext{}), forcedIDsKey{}, marker)
	_, span := otel.Tracer(tracerName).Start(rootCtx, controller+".change",
		trace.WithTimestamp(in.started),
		trace.WithAttributes(append(objectAttributes(controller, obj), attribute.String(attrWorkflowID, in.spanContext.TraceID().String()))...),
	)
	span.End()
}

// endRollout exports the span of a rollout that ended. For a workflow DPF derived, it is a root and
// takes the span ID the reconciles of the workflow point at. Otherwise it is a child of the span of
// the upstream caller.
func endRollout(ctx context.Context, controller string, obj client.Object, rollout finishedRollout) {
	attrs := append(objectAttributes(controller, obj),
		attribute.String(attrWorkflowID, rollout.in.spanContext.TraceID().String()),
		attribute.Int64("k8s.object.generation", rollout.generation),
		attribute.String("rollout.result", rollout.outcome),
	)
	rolloutCtx := trace.ContextWithRemoteSpanContext(ctx, rollout.in.spanContext)
	if rollout.in.derived {
		rolloutCtx = context.WithValue(trace.ContextWithSpanContext(ctx, trace.SpanContext{}), forcedIDsKey{}, rollout.in.spanContext)
	}
	_, span := otel.Tracer(tracerName).Start(rolloutCtx, controller+".rollout",
		trace.WithTimestamp(rollout.in.started),
		trace.WithAttributes(attrs...),
	)
	if rollout.outcome == rolloutTimedOut {
		span.SetAttributes(semconv.ErrorTypeKey.String(rolloutTimedOut))
		span.SetStatus(codes.Error, fmt.Sprintf("not ready after %s", reconciles.Load().rolloutTimeout()))
	}
	span.End(trace.WithTimestamp(rollout.end))
}
