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
