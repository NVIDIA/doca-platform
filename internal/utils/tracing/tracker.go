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
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/lru"
)

const (
	// trackerSize is how many objects a tracker remembers. The oldest ones are forgotten first, which
	// costs one more recorded idle reconcile and a rollout that is never reported.
	trackerSize = 32768

	// Outcomes of a rollout, reported as the rollout.result attribute.
	rolloutReady      = "ready"
	rolloutTimedOut   = "timeout"
	rolloutSuperseded = "superseded"
)

// reconciles is the process-wide tracker used by ReconcileSpan and ObserveRollout. It stays nil,
// and nothing is tracked, until SetupTracerProviderFromFlags enables tracing.
var reconciles atomic.Pointer[tracker]

// objectKey identifies an object as one controller reconciles it, as several controllers may
// reconcile the same object.
type objectKey struct {
	controller string
	uid        types.UID
}

// tracker remembers what the reconciles of an object did, to keep the idle requeues of a stable
// object out of the traces and to report how long a generation took to become ready.
type tracker struct {
	mu sync.Mutex
	// succeeded maps an objectKey to the resourceVersion its last reconcile succeeded at.
	succeeded *lru.Cache
	// rollouts maps an objectKey to its current rolloutState.
	rollouts *lru.Cache
	// traces maps the trace ID of a workflow to when this process first saw it.
	traces *lru.Cache
	// now returns the current time, replaced in tests.
	now func() time.Time
	// opts are the settings of the process, see FlagOptions.
	opts FlagOptions
}

// rolloutState is the rollout of one generation of an object, as far as this process saw it.
type rolloutState struct {
	// change identifies the generation and revision the rollout is of.
	change     string
	generation int64
	// in is the workflow of the rollout, and in.started is when the rollout started.
	in    incoming
	ended bool
}

// finishedRollout is a rollout that ended and has to be reported.
type finishedRollout struct {
	generation int64
	in         incoming
	end        time.Time
	outcome    string
}

// newTracker returns an empty tracker that follows the settings in opts.
func newTracker(opts FlagOptions) *tracker {
	return &tracker{
		succeeded: lru.New(trackerSize),
		rollouts:  lru.New(trackerSize),
		traces:    lru.New(trackerSize),
		now:       time.Now,
		opts:      opts,
	}
}

// keeps reports whether the sampling ratio keeps the trace with traceID. It decides the sampled flag
// of a trace DPF derives itself. A nil tracker keeps every trace.
func (t *tracker) keeps(traceID trace.TraceID) bool {
	if t == nil {
		return true
	}
	params := sdktrace.SamplingParameters{TraceID: traceID}
	return newRatioSampler(t.opts.SamplingPercent).ShouldSample(params).Decision == sdktrace.RecordAndSample
}

// skipUnchanged reports whether the spans of reconciles that changed nothing on their object, did
// not fail and have no child spans are dropped. A nil tracker drops nothing.
func (t *tracker) skipUnchanged() bool {
	return t != nil && !t.opts.RecordUnchangedReconciles
}

// rolloutTimeout returns how long a change may take to become ready. A nil tracker, or one without
// a setting, uses DefaultRolloutTimeout.
func (t *tracker) rolloutTimeout() time.Duration {
	if t == nil || t.opts.RolloutTimeout <= 0 {
		return DefaultRolloutTimeout
	}
	return t.opts.RolloutTimeout
}

// clock returns the current time. A nil tracker uses the system clock.
func (t *tracker) clock() time.Time {
	if t == nil {
		return time.Now()
	}
	return t.now()
}

// idle reports whether a reconcile of the object by controller that starts at resourceVersion is
// idle, as the last one of that controller succeeded at the same resourceVersion and nothing changed
// the object since. A nil tracker, or one that records idle reconciles, reports no idle reconcile.
func (t *tracker) idle(controller string, uid types.UID, resourceVersion string) bool {
	if t == nil || t.opts.RecordIdleReconciles || uid == "" || resourceVersion == "" {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	last, ok := t.succeeded.Get(objectKey{controller, uid})
	return ok && last.(string) == resourceVersion
}

// expired reports whether the process has collected reconciles into the workflow with traceID for
// longer than the rollout timeout, so a trace stays short. It starts the clock the first time it
// sees a workflow. A nil tracker never expires one.
func (t *tracker) expired(traceID trace.TraceID) bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	first, ok := t.traces.Get(traceID)
	if !ok {
		t.traces.Add(traceID, now)
		return false
	}
	return now.Sub(first.(time.Time)) >= t.rolloutTimeout()
}

// finish records the outcome of a reconcile by controller that started at resourceVersion. It does
// nothing on a nil tracker.
func (t *tracker) finish(controller string, uid types.UID, resourceVersion string, succeeded bool) {
	if t == nil || uid == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if succeeded {
		t.succeeded.Add(objectKey{controller, uid}, resourceVersion)
		return
	}
	t.succeeded.Remove(objectKey{controller, uid})
}

// observeRollout is called by each reconcile of the object at generation, with the workflow in it
// belongs to. It reports whether the process sees the change for the first time and returns the
// rollouts that ended with this reconcile. A rollout starts at in.started when the process first
// sees a change and ends when the object is ready, after the rollout timeout, or when a newer change
// replaces it. An object that is ready the first time it is seen is not a rollout, as its start is
// unknown. A nil tracker reports nothing.
func (t *tracker) observeRollout(controller string, uid types.UID, generation int64, in incoming, ready bool) (started bool, finished []finishedRollout) {
	if t == nil || uid == "" {
		return false, nil
	}
	key := objectKey{controller, uid}
	change := fmt.Sprintf("%d/%s", generation, in.revision)
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	var state rolloutState
	prev, known := t.rollouts.Get(key)
	if known {
		state = prev.(rolloutState)
	}
	// A reconcile can come long after the timeout. A rollout that was not ready by its deadline timed
	// out then, not when the reconcile noticed. A ready object does not wait for a reconcile.
	end := func(outcome string) finishedRollout {
		at := now
		if deadline := state.in.started.Add(t.rolloutTimeout()); outcome != rolloutReady && !now.Before(deadline) {
			outcome, at = rolloutTimedOut, deadline
		}
		return finishedRollout{state.generation, state.in, at, outcome}
	}
	if known && state.change != change {
		if !state.ended {
			finished = append(finished, end(rolloutSuperseded))
		}
		state = rolloutState{change: change, generation: generation, in: in}
		started = true
	} else if !known {
		state = rolloutState{change: change, generation: generation, in: in, ended: ready}
		started = true
	}
	switch {
	case state.ended:
	case ready:
		state.ended = true
		finished = append(finished, end(rolloutReady))
	case now.Sub(state.in.started) >= t.rolloutTimeout():
		state.ended = true
		finished = append(finished, end(rolloutTimedOut))
	}
	t.rollouts.Add(key, state)
	return started, finished
}
