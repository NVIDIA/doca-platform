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

package controllers

import (
	"maps"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// newDPUServiceIDLabelPredicate creates a predicate that filters Pod events with the DPFServiceIDLabelKey label.
func newDPUServiceIDLabelPredicate() predicate.Predicate {
	return predicate.NewPredicateFuncs(func(obj client.Object) bool {
		pod := obj.(*corev1.Pod)
		_, hasServiceID := pod.Labels[dpuservicev1.DPFServiceIDLabelKey]
		return hasServiceID
	})
}

// newPodReadinessPredicate creates a predicate that filters Pod events to only trigger on pod readiness changes.
func newPodReadinessPredicate() predicate.Funcs {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			// Accept create events if the pod is already ready.
			pod := e.Object.(*corev1.Pod)
			return getPodReadyCondition(pod) == corev1.ConditionTrue
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldPod := e.ObjectOld.(*corev1.Pod)
			newPod := e.ObjectNew.(*corev1.Pod)

			// Get readiness status for both old and new pod
			oldReady := getPodReadyCondition(oldPod)
			newReady := getPodReadyCondition(newPod)

			// Only trigger reconciliation if:
			// 1. Pod readiness changed (transition to/from ready state)
			if oldReady != newReady {
				return true
			}

			// 2. Pod deletion timestamp set (pod is being deleted)
			if oldPod.DeletionTimestamp == nil && newPod.DeletionTimestamp != nil {
				return true
			}

			// Ignore all other updates
			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// accept all delete events
			return true
		},
		GenericFunc: func(e event.GenericEvent) bool {
			// we are not interested in generic events
			return false
		},
	}
}

// getPodReadyCondition returns the status of the PodReady condition
func getPodReadyCondition(pod *corev1.Pod) corev1.ConditionStatus {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady {
			return cond.Status
		}
	}
	return corev1.ConditionUnknown
}

// newNodeConditionPredicate creates a predicate that filters node events to only trigger on condition changes
func newNodeConditionPredicate() predicate.Funcs {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			// Accept create events for nodes that have conditions
			node := e.Object.(*corev1.Node)
			return len(node.Status.Conditions) > 0
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldNode := e.ObjectOld.(*corev1.Node)
			newNode := e.ObjectNew.(*corev1.Node)

			// Only trigger reconciliation if node conditions have changed
			return !nodeConditionsEqual(oldNode.Status.Conditions, newNode.Status.Conditions)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return true
		},
		GenericFunc: func(e event.GenericEvent) bool {
			// we are not interested in generic events
			return false
		},
	}
}

// newServiceChainReadyPredicate creates a predicate that filters ServiceChain events
// to only trigger on Ready condition changes, avoiding reconciliation bursts on startup
func newServiceChainReadyPredicate() predicate.Funcs {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			// Skip create events to avoid reconciliation burst during initial cache sync
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldSC := e.ObjectOld.(*dpuservicev1.ServiceChain)
			newSC := e.ObjectNew.(*dpuservicev1.ServiceChain)

			// Only trigger if Ready condition changed
			oldReady := meta.IsStatusConditionTrue(oldSC.Status.Conditions, string(conditions.TypeReady))
			newReady := meta.IsStatusConditionTrue(newSC.Status.Conditions, string(conditions.TypeReady))
			return oldReady != newReady
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Reconcile on delete - a chain going away matters
			return true
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
	}
}

// newServiceInterfaceReadyPredicate creates a predicate that filters ServiceInterface events
// to only trigger on Ready condition changes, avoiding reconciliation bursts on startup
func newServiceInterfaceReadyPredicate() predicate.Funcs {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			// Skip create events to avoid reconciliation burst during initial cache sync
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldSI := e.ObjectOld.(*dpuservicev1.ServiceInterface)
			newSI := e.ObjectNew.(*dpuservicev1.ServiceInterface)

			// Only trigger if Ready condition changed
			oldReady := meta.IsStatusConditionTrue(oldSI.Status.Conditions, string(conditions.TypeReady))
			newReady := meta.IsStatusConditionTrue(newSI.Status.Conditions, string(conditions.TypeReady))
			return oldReady != newReady
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Reconcile on delete - an interface going away matters
			return true
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
	}
}

// newNodeServiceInterfacesReadyPredicate creates a predicate that filters NodeServiceInterfaces
// events to only trigger when a per-entry Ready condition changes, avoiding reconciliation bursts on startup.
func newNodeServiceInterfacesReadyPredicate() predicate.Funcs {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			// Skip create events to avoid reconciliation burst during initial cache sync
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldNSI := e.ObjectOld.(*dpuservicev1.NodeServiceInterfaces)
			newNSI := e.ObjectNew.(*dpuservicev1.NodeServiceInterfaces)
			return nsiInterfaceReadinessChanged(oldNSI.Status.InterfaceStatuses, newNSI.Status.InterfaceStatuses)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return true
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
	}
}

// nsiInterfaceReadinessChanged returns true if the Ready condition of any entry changed
// between the old and new InterfaceStatuses slices.
func nsiInterfaceReadinessChanged(oldStatuses, newStatuses []dpuservicev1.InterfaceEntryStatus) bool {
	readiness := func(statuses []dpuservicev1.InterfaceEntryStatus) map[string]bool {
		m := make(map[string]bool, len(statuses))
		for _, s := range statuses {
			m[s.Name] = meta.IsStatusConditionTrue(s.Conditions, string(conditions.TypeReady))
		}
		return m
	}
	return !maps.Equal(readiness(oldStatuses), readiness(newStatuses))
}

// privilegedPodEnforcementChangedPredicate triggers a DPUService reconcile only
// when the DPFOperatorConfig's resolved privileged-pod enforcement state
// changes. Create/Delete/Generic events and unrelated spec edits are ignored:
// DPUServices reconcile their own lifecycle (and pick up the current state via
// the periodic resync), so the watch exists purely to make a breakglass toggle
// converge promptly without re-reconciling every DPUService on every config edit.
func privilegedPodEnforcementChangedPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc:  func(event.CreateEvent) bool { return false },
		DeleteFunc:  func(event.DeleteEvent) bool { return false },
		GenericFunc: func(event.GenericEvent) bool { return false },
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldConfig, ok := e.ObjectOld.(*operatorv1.DPFOperatorConfig)
			if !ok {
				return false
			}
			newConfig, ok := e.ObjectNew.(*operatorv1.DPFOperatorConfig)
			if !ok {
				return false
			}
			return oldConfig.Spec.Security.PrivilegedPodEnforcementEnabled() != newConfig.Spec.Security.PrivilegedPodEnforcementEnabled()
		},
	}
}

// nodeOVNEncapIPPredicate filters node events to only trigger on OVN node encapsulation IP changes.
func nodeOVNEncapIPPredicate() predicate.Funcs {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return e.Object.GetAnnotations()[ovnNodeEncapIPsAnnotation] != ""
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldAnno := e.ObjectOld.GetAnnotations()
			newAnno := e.ObjectNew.GetAnnotations()
			return oldAnno[ovnNodeEncapIPsAnnotation] != newAnno[ovnNodeEncapIPsAnnotation]
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return e.Object.GetAnnotations()[ovnNodeEncapIPsAnnotation] != ""
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
	}
}

// nodeAddressPredicate filters node events to only trigger on node address changes.
func nodeAddressPredicate() predicate.Funcs {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return true
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldNode := e.ObjectOld.(*corev1.Node)
			newNode := e.ObjectNew.(*corev1.Node)

			// We only care about transitions to/from the node addresses.
			oldAddresses := oldNode.Status.Addresses
			newAddresses := newNode.Status.Addresses

			// trigger if the internal addresses have changed
			if !equality.Semantic.DeepEqual(oldAddresses, newAddresses) {
				return true
			}

			// Ignore all other updates
			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// accept all delete events
			return true
		},
		GenericFunc: func(e event.GenericEvent) bool {
			// we are not interested in generic events
			return false
		},
	}
}
