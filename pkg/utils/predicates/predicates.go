/*
Copyright 2025 NVIDIA

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

package predicates

import (
	"github.com/nvidia/doca-platform/pkg/conditions"

	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// TypedResourceIsChanged returns a predicate that returns true only if the resource
// has changed. This predicate allows to drop resync events on additionally watched objects.
func TypedResourceIsChanged[T client.Object]() predicate.TypedFuncs[T] {
	return predicate.TypedFuncs[T]{
		UpdateFunc: func(e event.TypedUpdateEvent[T]) bool {
			// We only want to trigger a reconciliation if the resource version has changed.
			// ResourceVersion is a string that is updated every time the object is modified.
			// It is used to detect changes in the object.
			return e.ObjectOld.GetResourceVersion() != e.ObjectNew.GetResourceVersion()
		},
		CreateFunc:  func(event.TypedCreateEvent[T]) bool { return true },
		DeleteFunc:  func(event.TypedDeleteEvent[T]) bool { return true },
		GenericFunc: func(event.TypedGenericEvent[T]) bool { return true },
	}
}

// ReadyConditionChanged returns a predicate that triggers only on Ready condition changes.
// This is useful for filtering out status updates that don't affect the Ready state.
// The object must implement conditions.GetSet interface.
func ReadyConditionChanged() predicate.Funcs {
	return ConditionChanged(conditions.TypeReady)
}

// ConditionChanged returns a predicate that triggers only when the status of the given
// condition type changes. This is useful for filtering out status updates that don't
// affect the condition being watched. The object must implement conditions.GetSet interface.
func ConditionChanged(conditionType conditions.ConditionType) predicate.Funcs {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			// Trigger when an object is marked for deletion (DeletionTimestamp set).
			// With finalizers the object is not removed immediately, so this arrives
			// as an Update, not a Delete.
			if e.ObjectOld.GetDeletionTimestamp().IsZero() && !e.ObjectNew.GetDeletionTimestamp().IsZero() {
				return true
			}

			oldObj, oldOk := e.ObjectOld.(conditions.GetSet)
			newObj, newOk := e.ObjectNew.(conditions.GetSet)
			if !oldOk || !newOk {
				// If objects don't implement GetSet, skip this event
				return false
			}

			oldStatus := meta.IsStatusConditionTrue(oldObj.GetConditions(), string(conditionType))
			newStatus := meta.IsStatusConditionTrue(newObj.GetConditions(), string(conditionType))
			return oldStatus != newStatus
		},
		CreateFunc:  func(event.CreateEvent) bool { return true }, // Trigger on creation to get the status already
		DeleteFunc:  func(event.DeleteEvent) bool { return true }, // Trigger on deletion as it will affect readiness
		GenericFunc: func(event.GenericEvent) bool { return false },
	}
}

// PredicateFuncsByEventTypes returns a predicate that returns true for the provided event types
func PredicateFuncsByEventTypes(events ...any) predicate.Funcs {
	var created, updated, deleted, generic bool
	for _, e := range events {
		switch e.(type) {
		case event.CreateEvent:
			created = true
		case event.UpdateEvent:
			updated = true
		case event.DeleteEvent:
			deleted = true
		case event.GenericEvent:
			generic = true
		}
	}

	return predicate.Funcs{
		CreateFunc:  func(event.CreateEvent) bool { return created },
		UpdateFunc:  func(event.UpdateEvent) bool { return updated },
		DeleteFunc:  func(event.DeleteEvent) bool { return deleted },
		GenericFunc: func(event.GenericEvent) bool { return generic },
	}
}
