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
	"encoding/binary"
	"math/rand/v2"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// forcedIDsKey is the Context key holding the span context whose trace and span ID the next root
// span takes instead of random ones.
type forcedIDsKey struct{}

// idGenerator generates random IDs, except for the root span of a derived trace. That span takes
// the IDs newSpanContextForObject derived, which the traceparent of the children points at.
type idGenerator struct{}

var _ sdktrace.IDGenerator = idGenerator{}

// NewIDs returns the IDs for a new root span.
func (idGenerator) NewIDs(ctx context.Context) (trace.TraceID, trace.SpanID) {
	if sc, ok := ctx.Value(forcedIDsKey{}).(trace.SpanContext); ok {
		return sc.TraceID(), sc.SpanID()
	}
	var traceID trace.TraceID
	for !traceID.IsValid() {
		binary.LittleEndian.PutUint64(traceID[:8], rand.Uint64())
		binary.LittleEndian.PutUint64(traceID[8:], rand.Uint64())
	}
	return traceID, newRandomSpanID()
}

// NewSpanID returns the ID for a new span in an existing trace.
func (idGenerator) NewSpanID(context.Context, trace.TraceID) trace.SpanID {
	return newRandomSpanID()
}

// newRandomSpanID returns a random, non-zero span ID.
func newRandomSpanID() trace.SpanID {
	var spanID trace.SpanID
	for !spanID.IsValid() {
		binary.LittleEndian.PutUint64(spanID[:], rand.Uint64())
	}
	return spanID
}
