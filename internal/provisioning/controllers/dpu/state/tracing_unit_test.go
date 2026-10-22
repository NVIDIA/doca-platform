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
	"github.com/nvidia/doca-platform/internal/utils/tracing"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	testTraceparent        = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	testCreatorTraceparent = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
)

// tracedDPUContext returns the context of a DPU reconcile that arrived with testTraceparent.
func tracedDPUContext(dpu *provisioningv1.DPU) context.Context {
	ctx, endSpan := tracing.ReconcileSpan(context.Background(), "dpu", dpu)
	endSpan(nil)
	return ctx
}

// TestCreateDPUNodeMaintenancePropagatesTraceContext checks that the DPUNodeMaintenance a DPU
// creates continues that DPU's trace.
func TestCreateDPUNodeMaintenancePropagatesTraceContext(t *testing.T) {
	t.Cleanup(tracing.EnableForTest())
	g := NewWithT(t)
	scheme := runtime.NewScheme()
	g.Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	dpu := &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "dpu",
			Namespace:   "ns",
			UID:         "dpu-uid",
			Annotations: map[string]string{tracing.TraceParentAnnotation: testTraceparent},
		},
		Spec: provisioningv1.DPUSpec{DPUNodeName: "node"},
	}

	g.Expect(createDPUNodeMaintenance(tracedDPUContext(dpu), c, "maintenance", dpu)).To(Succeed())

	got := &provisioningv1.DPUNodeMaintenance{}
	g.Expect(c.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: "maintenance"}, got)).To(Succeed())
	g.Expect(got.Annotations).To(HaveKeyWithValue(tracing.GeneratedTraceParentAnnotation, testTraceparent))
}

// TestCreateDPUNodeMaintenanceWritesNoTraceContextWithoutUpstream checks that the
// DPUNodeMaintenance gets no trace context when the DPU reconcile carries none.
func TestCreateDPUNodeMaintenanceWritesNoTraceContextWithoutUpstream(t *testing.T) {
	g := NewWithT(t)
	scheme := runtime.NewScheme()
	g.Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	dpu := &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{Name: "dpu", Namespace: "ns", UID: "dpu-uid"},
		Spec:       provisioningv1.DPUSpec{DPUNodeName: "node"},
	}

	g.Expect(createDPUNodeMaintenance(context.Background(), c, "maintenance", dpu)).To(Succeed())

	got := &provisioningv1.DPUNodeMaintenance{}
	g.Expect(c.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: "maintenance"}, got)).To(Succeed())
	g.Expect(got.Annotations).NotTo(HaveKey(tracing.GeneratedTraceParentAnnotation))
}

// TestAddRequestorStampsJoiningDPUTrace checks that a DPU joining an existing DPUNodeMaintenance
// replaces the creator's trace with its own, so the reconcile its join causes continues it.
func TestAddRequestorStampsJoiningDPUTrace(t *testing.T) {
	t.Cleanup(tracing.EnableForTest())
	g := NewWithT(t)
	scheme := runtime.NewScheme()
	g.Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
	maintenance := &provisioningv1.DPUNodeMaintenance{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "maintenance",
			Namespace:   "ns",
			Annotations: map[string]string{tracing.GeneratedTraceParentAnnotation: testCreatorTraceparent},
		},
		Spec: provisioningv1.DPUNodeMaintenanceSpec{
			Requestor:  []string{"dpu-a"},
			NodeEffect: &provisioningv1.NodeEffect{},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(maintenance).Build()
	dpu := &provisioningv1.DPU{ObjectMeta: metav1.ObjectMeta{
		Name:        "dpu-b",
		Namespace:   "ns",
		UID:         "dpu-b-uid",
		Annotations: map[string]string{tracing.TraceParentAnnotation: testTraceparent},
	}}

	g.Expect(addRequestorAndUpdateForce(tracedDPUContext(dpu), c, maintenance, "dpu-b", nil, false)).To(Succeed())

	got := &provisioningv1.DPUNodeMaintenance{}
	g.Expect(c.Get(context.Background(), client.ObjectKeyFromObject(maintenance), got)).To(Succeed())
	g.Expect(got.Spec.Requestor).To(ContainElement("dpu-b"))
	g.Expect(got.Annotations).To(HaveKeyWithValue(tracing.GeneratedTraceParentAnnotation, testTraceparent))
}

// TestRemoveRequestorStampsLeavingDPUTrace checks that a DPU removing itself as a requestor stamps
// its trace, so the reconcile that may delete the DPUNodeMaintenance continues it.
func TestRemoveRequestorStampsLeavingDPUTrace(t *testing.T) {
	t.Cleanup(tracing.EnableForTest())
	g := NewWithT(t)
	scheme := runtime.NewScheme()
	g.Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
	nodeEffect := provisioningv1.NodeEffect{Action: provisioningv1.Action{Drain: ptr.To(true)}}
	name, err := cutil.GenerateDPUNodeMaintenanceObjectName("node", nodeEffect)
	g.Expect(err).NotTo(HaveOccurred())
	maintenance := &provisioningv1.DPUNodeMaintenance{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   "ns",
			Annotations: map[string]string{tracing.GeneratedTraceParentAnnotation: testCreatorTraceparent},
		},
		Spec: provisioningv1.DPUNodeMaintenanceSpec{Requestor: []string{"dpu-a", "dpu-b"}},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(maintenance).Build()
	dpu := &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "dpu-b",
			Namespace:   "ns",
			UID:         "dpu-b-uid",
			Annotations: map[string]string{tracing.TraceParentAnnotation: testTraceparent},
		},
		Spec: provisioningv1.DPUSpec{DPUNodeName: "node", NodeEffect: nodeEffect},
	}

	g.Expect(RemoveRequestorFromDPUNodeMaintenance(tracedDPUContext(dpu), dpu, &dutil.ControllerContext{Client: c})).To(Succeed())

	got := &provisioningv1.DPUNodeMaintenance{}
	g.Expect(c.Get(context.Background(), client.ObjectKeyFromObject(maintenance), got)).To(Succeed())
	g.Expect(got.Spec.Requestor).To(Equal([]string{"dpu-a"}))
	g.Expect(got.Annotations).To(HaveKeyWithValue(tracing.GeneratedTraceParentAnnotation, testTraceparent))
}
