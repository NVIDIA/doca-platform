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

package state

import (
	"context"
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestErrorResumesTimedOutNodeEffectRemovalWhenUnblocked(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := provisioningv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	dpu := timedOutNodeEffectRemovalDPU()
	maintenance := maintenanceForDPU(t, dpu, []string{dpu.Name})
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(maintenance).Build()
	ctrlCtx := &dutil.ControllerContext{
		Client:               client,
		DPUInProvisioningMap: dutil.NewDPUInProvisioningMap(1),
	}

	status, err := Error(context.Background(), dpu, ctrlCtx)
	if err != nil {
		t.Fatalf("Error returned an unexpected error: %v", err)
	}
	if status.Phase != provisioningv1.DPUNodeEffectRemoval {
		t.Fatalf("expected phase %q, got %q", provisioningv1.DPUNodeEffectRemoval, status.Phase)
	}
	if meta.FindStatusCondition(status.Conditions, provisioningv1.DPUCondNodeEffectRemoved.String()) != nil {
		t.Fatal("expected timeout condition to be cleared before retry")
	}

	updated := &provisioningv1.DPUNodeMaintenance{}
	if err := client.Get(context.Background(), types.NamespacedName{Namespace: dpu.Namespace, Name: maintenance.Name}, updated); err != nil {
		t.Fatal(err)
	}
	if len(updated.Spec.Requestor) != 0 {
		t.Fatalf("expected DPU requestor to be removed, got %v", updated.Spec.Requestor)
	}
}

func TestErrorKeepsDPUInErrorWhileOtherMaintenanceRequestorsRemain(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := provisioningv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	dpu := timedOutNodeEffectRemovalDPU()
	maintenance := maintenanceForDPU(t, dpu, []string{dpu.Name, "service-requestor"})
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(maintenance).Build()
	ctrlCtx := &dutil.ControllerContext{
		Client:               client,
		DPUInProvisioningMap: dutil.NewDPUInProvisioningMap(1),
	}

	status, err := Error(context.Background(), dpu, ctrlCtx)
	if err != nil {
		t.Fatalf("Error returned an unexpected error: %v", err)
	}
	if status.Phase != provisioningv1.DPUError {
		t.Fatalf("expected phase %q while another requestor remains, got %q", provisioningv1.DPUError, status.Phase)
	}
}

func TestErrorDoesNotRetryUnrelatedDPUFailures(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := provisioningv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	dpu := timedOutNodeEffectRemovalDPU()
	dpu.Status.PreviousPhase = provisioningv1.DPUConfig
	ctrlCtx := &dutil.ControllerContext{
		Client:               fake.NewClientBuilder().WithScheme(scheme).Build(),
		DPUInProvisioningMap: dutil.NewDPUInProvisioningMap(1),
	}

	status, err := Error(context.Background(), dpu, ctrlCtx)
	if err != nil {
		t.Fatalf("Error returned an unexpected error: %v", err)
	}
	if status.Phase != provisioningv1.DPUError {
		t.Fatalf("expected unrelated failure to remain in %q, got %q", provisioningv1.DPUError, status.Phase)
	}
}

func timedOutNodeEffectRemovalDPU() *provisioningv1.DPU {
	return &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{Namespace: "dpf-operator-system", Name: "dpu-a"},
		Spec: provisioningv1.DPUSpec{
			DPUNodeName: "dpu-node-a",
			NodeEffect: provisioningv1.NodeEffect{
				Action: provisioningv1.Action{Drain: ptr.To(true)},
			},
		},
		Status: provisioningv1.DPUStatus{
			Phase:         provisioningv1.DPUError,
			PreviousPhase: provisioningv1.DPUNodeEffectRemoval,
			Conditions: []metav1.Condition{{
				Type:   provisioningv1.DPUCondNodeEffectRemoved.String(),
				Status: metav1.ConditionFalse,
				Reason: nodeEffectRemovalTimeoutReason,
			}},
		},
	}
}

func maintenanceForDPU(t *testing.T, dpu *provisioningv1.DPU, requestors []string) *provisioningv1.DPUNodeMaintenance {
	t.Helper()
	name, err := cutil.GenerateDPUNodeMaintenanceObjectName(dpu.Spec.DPUNodeName, dpu.Spec.NodeEffect)
	if err != nil {
		t.Fatal(err)
	}
	return &provisioningv1.DPUNodeMaintenance{
		ObjectMeta: metav1.ObjectMeta{Namespace: dpu.Namespace, Name: name},
		Spec: provisioningv1.DPUNodeMaintenanceSpec{
			DPUNodeName: dpu.Spec.DPUNodeName,
			Requestor:   requestors,
		},
	}
}
