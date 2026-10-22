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

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// triggerKey identifies an object by Go type and name, so objects of different kinds with the
// same name are kept apart.
type triggerKey struct {
	kind string
	types.NamespacedName
}

// triggerKeyFor returns the key obj is recorded under.
func triggerKeyFor(obj client.Object) triggerKey {
	return triggerKey{kind: fmt.Sprintf("%T", obj), NamespacedName: client.ObjectKeyFromObject(obj)}
}

// triggers records, for one reconcile, the object whose change caused each pending write, so the
// write carries that object's trace.
type triggers struct {
	reconciled triggerKey
	byTarget   map[triggerKey]client.Object
	optionsOf  func(trigger client.Object) []ReconcileOption
}

// triggersContextKey is the context key holding the reconcile's triggers.
type triggersContextKey struct{}

// WithTriggers returns ctx carrying an empty trigger record for one reconcile of reconciled. Call
// it once per reconcile, after ReconcileSpan, so concurrent reconciles never share a record.
// optionsOf returns the options the controller of a trigger passes to ReconcileSpan, so a trigger
// without a trace is only followed when its controller exports the workflow it derives; nil means
// no options for any trigger.
func WithTriggers(ctx context.Context, reconciled client.Object, optionsOf func(trigger client.Object) []ReconcileOption) context.Context {
	return context.WithValue(ctx, triggersContextKey{}, &triggers{
		reconciled: triggerKeyFor(reconciled),
		byTarget:   map[triggerKey]client.Object{},
		optionsOf:  optionsOf,
	})
}

// triggersFrom returns the trigger record carried in ctx, or nil when there is none.
func triggersFrom(ctx context.Context) *triggers {
	t, _ := ctx.Value(triggersContextKey{}).(*triggers)
	return t
}

// RecordTrigger records trigger as the cause of the pending write to target. Call it where the
// reconcile decides target must change; target only needs its kind, name and namespace. The
// reconciled object wins over any other trigger, because its own change applies to everything it
// writes; between other triggers the first recorded one is kept. No-op when ctx carries no trigger
// record or trigger is nil.
func RecordTrigger(ctx context.Context, target, trigger client.Object) {
	t := triggersFrom(ctx)
	if t == nil || trigger == nil {
		return
	}
	key := triggerKeyFor(target)
	if _, recorded := t.byTarget[key]; recorded && triggerKeyFor(trigger) != t.reconciled {
		return
	}
	t.byTarget[key] = trigger
}

// HasTrigger reports whether a trigger is recorded for target.
func HasTrigger(ctx context.Context, target client.Object) bool {
	t := triggersFrom(ctx)
	if t == nil {
		return false
	}
	_, recorded := t.byTarget[triggerKeyFor(target)]
	return recorded
}

// InjectTrigger copies the trace of the trigger recorded for target onto target and forgets the
// trigger, so a later write to target in the same reconcile records its own. Call it just before
// the write. When the trigger has no trace to hand on, it removes the trace annotations from target
// instead, so the write does not continue the trace of an earlier change. It reports whether it
// wrote a trace context; without a recorded trigger it leaves target unchanged.
func InjectTrigger(ctx context.Context, target client.Object) bool {
	t := triggersFrom(ctx)
	if t == nil {
		return false
	}
	key := triggerKeyFor(target)
	trigger, recorded := t.byTarget[key]
	if !recorded {
		return false
	}
	delete(t.byTarget, key)
	var opts []ReconcileOption
	if t.optionsOf != nil {
		opts = t.optionsOf(trigger)
	}
	if InjectFromObject(trigger, target, opts...) {
		return true
	}
	RemoveFromObject(target)
	return false
}
