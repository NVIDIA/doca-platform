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
	"slices"

	"k8s.io/apimachinery/pkg/api/equality"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// IgnoreTraceOnlyUpdates returns a predicate that drops update events whose only change is to the
// trace annotations, so a patch that only hands a trace on to an object does not enqueue a
// reconcile. Create, delete and generic events, and resyncs, pass.
func IgnoreTraceOnlyUpdates() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			return !traceOnlyUpdate(e.ObjectOld, e.ObjectNew)
		},
	}
}

// traceOnlyUpdate reports whether newObj differs from oldObj in its trace annotations and in
// nothing else but the resourceVersion and managedFields the write itself changed.
func traceOnlyUpdate(oldObj, newObj client.Object) bool {
	if oldObj == nil || newObj == nil {
		return false
	}
	oldAnnotations, newAnnotations := oldObj.GetAnnotations(), newObj.GetAnnotations()
	if !slices.ContainsFunc(traceAnnotations, func(key string) bool {
		oldValue, oldOK := oldAnnotations[key]
		newValue, newOK := newAnnotations[key]
		return oldOK != newOK || oldValue != newValue
	}) {
		return false
	}
	oldStripped, newStripped := withoutTrace(oldObj), withoutTrace(newObj)
	return oldStripped != nil && newStripped != nil && equality.Semantic.DeepEqual(oldStripped, newStripped)
}

// withoutTrace returns a copy of obj without its trace annotations, resourceVersion and
// managedFields, or nil when obj cannot be copied as a client.Object.
func withoutTrace(obj client.Object) client.Object {
	stripped, ok := obj.DeepCopyObject().(client.Object)
	if !ok {
		return nil
	}
	RemoveFromObject(stripped)
	stripped.SetResourceVersion("")
	stripped.SetManagedFields(nil)
	return stripped
}
