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

package redfish

import (
	"context"
	"errors"
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// TestPersistRedfishTaskIDWritesStatus verifies a successful status patch stores the task id on
// the API without changing the reconciler object's ResourceVersion.
func TestPersistRedfishTaskIDWritesStatus(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := provisioningv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	dpu := &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "dpu-task",
			Namespace:       "default",
			ResourceVersion: "1",
		},
		Status: provisioningv1.DPUStatus{
			Phase: provisioningv1.DPUOSInstalling,
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(dpu).WithObjects(dpu).Build()

	beforeRV := dpu.ResourceVersion
	if err := persistRedfishTaskID(context.Background(), c, dpu, "task-42"); err != nil {
		t.Fatal(err)
	}
	got := &provisioningv1.DPU{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(dpu), got); err != nil {
		t.Fatal(err)
	}
	if got.Status.RedfishTaskID == nil || *got.Status.RedfishTaskID != "task-42" {
		t.Fatalf("stored redfishTaskId = %v, want task-42", got.Status.RedfishTaskID)
	}
	// Reconciler object must keep its start-of-reconcile RV so SerialPatcher is not locked.
	if dpu.ResourceVersion != beforeRV {
		t.Fatalf("in-memory ResourceVersion changed from %q to %q; deferred SerialPatcher must not see checkpoint RV", beforeRV, dpu.ResourceVersion)
	}
	if dpu.Status.RedfishTaskID != nil {
		t.Fatalf("persist must not mutate dpu.Status; UpdateDPUStatus applies the handler state, got %v", dpu.Status.RedfishTaskID)
	}
}

// TestPersistRedfishTaskIDReplacesPriorTaskID verifies a new accepted id replaces a prior durable
// id on the API (e.g. config after ISO).
func TestPersistRedfishTaskIDReplacesPriorTaskID(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := provisioningv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	dpu := &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "dpu-stale",
			Namespace:       "default",
			ResourceVersion: "3",
		},
		Status: provisioningv1.DPUStatus{
			Phase:         provisioningv1.DPUOSInstalling,
			RedfishTaskID: ptr.To("iso-done"),
		},
	}
	beforeRV := dpu.ResourceVersion
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(dpu).WithObjects(dpu.DeepCopy()).Build()

	if err := persistRedfishTaskID(context.Background(), c, dpu, "config-new"); err != nil {
		t.Fatal(err)
	}
	got := &provisioningv1.DPU{}
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(dpu), got); err != nil {
		t.Fatal(err)
	}
	if got.Status.RedfishTaskID == nil || *got.Status.RedfishTaskID != "config-new" {
		t.Fatalf("API must store config task id, got %v", got.Status.RedfishTaskID)
	}
	if dpu.ResourceVersion != beforeRV {
		t.Fatalf("in-memory ResourceVersion must stay %q, got %q", beforeRV, dpu.ResourceVersion)
	}
}

// TestCheckpointRedfishTaskIDReturnsErrorOnPatchFailure verifies a failed status patch returns
// an error while keeping the accepted task id on the working state for the deferred write.
func TestCheckpointRedfishTaskIDReturnsErrorOnPatchFailure(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := provisioningv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	dpu := &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dpu-fail",
			Namespace: "default",
		},
		Status: provisioningv1.DPUStatus{
			Phase: provisioningv1.DPUOSInstalling,
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(dpu).WithObjects(dpu).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourcePatch: func(ctx context.Context, c client.Client, subResourceName string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
				return errors.New("injected status patch failure")
			},
		}).Build()

	state := dpu.Status.DeepCopy()
	err := checkpointRedfishTaskID(context.Background(), c, dpu, state, "accepted-but-unpatched")
	if err == nil {
		t.Fatal("expected checkpoint error when status patch fails")
	}
	if state.RedfishTaskID == nil || *state.RedfishTaskID != "accepted-but-unpatched" {
		t.Fatalf("state must keep task id after failed immediate patch, got %v", state.RedfishTaskID)
	}
	got := &provisioningv1.DPU{}
	if getErr := c.Get(context.Background(), client.ObjectKeyFromObject(dpu), got); getErr != nil {
		t.Fatal(getErr)
	}
	if got.Status.RedfishTaskID != nil {
		t.Fatalf("API object must not have task id after failed patch, got %v", *got.Status.RedfishTaskID)
	}
}

// TestCheckpointRedfishTaskIDRequiresClientAndDPU verifies missing client or DPU is an error while
// still recording the accepted id on the working state for the deferred write.
func TestCheckpointRedfishTaskIDRequiresClientAndDPU(t *testing.T) {
	state := &provisioningv1.DPUStatus{
		Phase: provisioningv1.DPUOSInstalling,
	}
	if err := checkpointRedfishTaskID(context.Background(), nil, nil, state, "local-only"); err == nil {
		t.Fatal("expected error when client/dpu are nil")
	}
	if state.RedfishTaskID == nil || *state.RedfishTaskID != "local-only" {
		t.Fatalf("state must still hold accepted id, got %v", state.RedfishTaskID)
	}
}

// TestCheckpointRedfishTaskIDRejectsEmptyTaskID verifies an empty task id is rejected before any
// present-but-empty value is stored on state.
func TestCheckpointRedfishTaskIDRejectsEmptyTaskID(t *testing.T) {
	state := &provisioningv1.DPUStatus{
		Phase: provisioningv1.DPUOSInstalling,
	}
	dpu := &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dpu-empty",
			Namespace: "default",
		},
	}
	err := checkpointRedfishTaskID(context.Background(), fake.NewClientBuilder().Build(), dpu, state, "")
	if err == nil {
		t.Fatal("expected error for empty task id")
	}
	if state.RedfishTaskID != nil {
		t.Fatalf("empty task id must not be stored on state, got %v", state.RedfishTaskID)
	}
}
