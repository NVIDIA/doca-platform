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

package predicates

import (
	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"

	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// NSIPredicate fires only on NSI changes the ServiceInterfaceSet
// reconciler needs to act on, avoiding spurious reconciles for unrelated changes.
//
// It fires on UPDATE when:
//   - spec changed (entries added, removed, or modified by any manager)
//   - any status.interfaceStatuses entry Ready condition changed
//     (downstream reconcilers update SIS readiness from NSI entry status)
//   - any status.interfaceStatuses entry just transitioned to ResourceReleased=True
//     (the termination handshake completed; the producer must now remove the entry)
//
// It always fires on Create and Delete.
type NSIPredicate struct {
	predicate.Funcs
}

func (NSIPredicate) Update(e event.UpdateEvent) bool {
	oldNSI, ok := e.ObjectOld.(*dpuservicev1.NodeServiceInterfaces)
	if !ok {
		return true
	}
	newNSI, ok := e.ObjectNew.(*dpuservicev1.NodeServiceInterfaces)
	if !ok {
		return true
	}

	if oldNSI.GetGeneration() != newNSI.GetGeneration() {
		return true
	}

	for _, newStatus := range newNSI.Status.InterfaceStatuses {
		old := findInterfaceEntryStatus(oldNSI, newStatus.Name)
		if nsiEntryReadyChanged(old, &newStatus) {
			return true
		}
		if !meta.IsStatusConditionTrue(newStatus.Conditions, string(dpuservicev1.ResourceReleased)) {
			continue
		}
		if old == nil || !meta.IsStatusConditionTrue(old.Conditions, string(dpuservicev1.ResourceReleased)) {
			return true
		}
	}

	return false
}

func nsiEntryReadyChanged(old, new *dpuservicev1.InterfaceEntryStatus) bool {
	oldReady := old != nil && meta.IsStatusConditionTrue(old.Conditions, string(conditions.TypeReady))
	newReady := meta.IsStatusConditionTrue(new.Conditions, string(conditions.TypeReady))
	return oldReady != newReady
}

func findInterfaceEntryStatus(nsi *dpuservicev1.NodeServiceInterfaces, name string) *dpuservicev1.InterfaceEntryStatus {
	for _, status := range nsi.Status.InterfaceStatuses {
		if status.Name == name {
			return &status
		}
	}
	return nil
}
