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

package dpu

import (
	"context"
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func TestDPUNodeMaintenanceToDPU(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := provisioningv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	dpu := &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "dpf-operator-system",
			Name:      "dpu-a",
			Labels: map[string]string{
				provisioningv1.DPUNodeNameLabel: "dpu-node-a",
			},
		},
	}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dpu).Build()
	reconciler := &DPUReconciler{ctrlCtx: &util.ControllerContext{Client: client}}
	maintenance := &provisioningv1.DPUNodeMaintenance{
		ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system", Name: "maintenance-a"},
		Spec:       provisioningv1.DPUNodeMaintenanceSpec{DPUNodeName: "dpu-node-a"},
	}

	requests := reconciler.dpuNodeMaintenanceToDPU(context.Background(), maintenance)
	if len(requests) != 1 || requests[0].NamespacedName != (types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Name}) {
		t.Fatalf("unexpected reconcile requests: %#v", requests)
	}
}

func TestDPUNodeMaintenancePredicate(t *testing.T) {
	predicate := dpuNodeMaintenancePredicate()
	oldMaintenance := &provisioningv1.DPUNodeMaintenance{
		ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system", Generation: 1},
		Status: provisioningv1.DPUNodeMaintenanceStatus{Conditions: []metav1.Condition{{
			Type:               string(provisioningv1.ConditionNodeEffectApplied),
			Status:             metav1.ConditionFalse,
			ObservedGeneration: 1,
		}}},
	}
	newMaintenance := oldMaintenance.DeepCopy()
	newMaintenance.Status.Conditions[0].Status = metav1.ConditionTrue

	if !predicate.Update(event.UpdateEvent{ObjectOld: oldMaintenance, ObjectNew: newMaintenance}) {
		t.Fatal("expected NodeEffectApplied transition to enqueue reconciliation")
	}

	newMaintenance = oldMaintenance.DeepCopy()
	newMaintenance.Generation++
	if !predicate.Update(event.UpdateEvent{ObjectOld: oldMaintenance, ObjectNew: newMaintenance}) {
		t.Fatal("expected DPUNodeMaintenance spec change to enqueue reconciliation")
	}
}

func TestDPUNodeMaintenancePredicateIgnoresUnrelatedStatusUpdate(t *testing.T) {
	predicate := dpuNodeMaintenancePredicate()
	oldMaintenance := &provisioningv1.DPUNodeMaintenance{
		ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system", Generation: 1},
		Status: provisioningv1.DPUNodeMaintenanceStatus{Conditions: []metav1.Condition{{
			Type:               string(provisioningv1.ConditionNodeEffectApplied),
			Status:             metav1.ConditionFalse,
			ObservedGeneration: 1,
		}}},
	}
	newMaintenance := oldMaintenance.DeepCopy()
	newMaintenance.Status.Conditions[0].Message = "still pending"

	if predicate.Update(event.UpdateEvent{ObjectOld: oldMaintenance, ObjectNew: newMaintenance}) {
		t.Fatal("did not expect unrelated status update to enqueue reconciliation")
	}
}
