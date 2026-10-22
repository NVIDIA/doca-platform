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

package dpudevice

import (
	"context"
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuflavortemplate"
	"github.com/nvidia/doca-platform/internal/utils/tracing"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

const testTraceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

// tracedDeviceContext returns the context of a DPUDevice reconcile of device, started the way the
// DPUDevice controller starts it.
func tracedDeviceContext(device *provisioningv1.DPUDevice) context.Context {
	ctx, endSpan := tracing.ReconcileSpan(context.Background(), DPUDeviceControllerName, device, tracing.WorkflowRoot())
	endSpan(nil)
	return ctx
}

// TestEnsureHostlessDPUNodeContinuesDerivedWorkflow checks that a DPUDevice without a trace starts
// a workflow of its own, and that its hostless DPUNode joins the same workflow its DPU does.
func TestEnsureHostlessDPUNodeContinuesDerivedWorkflow(t *testing.T) {
	t.Cleanup(tracing.EnableForTest())
	g := NewWithT(t)
	scheme := runtime.NewScheme()
	g.Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	device := &provisioningv1.DPUDevice{
		ObjectMeta: metav1.ObjectMeta{Name: "device", Namespace: "ns", UID: "device-uid", Generation: 1},
	}
	r := &DPUDeviceReconciler{Client: c}

	name, err := r.ensureHostlessDPUNode(tracedDeviceContext(device), device)
	g.Expect(err).NotTo(HaveOccurred())

	got := &provisioningv1.DPUNode{}
	g.Expect(c.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: name}, got)).To(Succeed())
	dpu := &provisioningv1.DPU{}
	tracing.InjectFromObject(device, dpu, tracing.WorkflowRoot())
	g.Expect(got.Annotations).To(HaveKey(tracing.GeneratedTraceParentAnnotation))
	g.Expect(got.Annotations[tracing.GeneratedTraceParentAnnotation]).To(Equal(dpu.Annotations[tracing.GeneratedTraceParentAnnotation]))
}

// TestDPUReadyPhasePredicate checks that only DPU updates entering or leaving the Ready phase, or
// applying new device values to a Ready DPU, enqueue the bound DPUDevice for its tracing rollout.
func TestDPUReadyPhasePredicate(t *testing.T) {
	dpuIn := func(phase provisioningv1.DPUPhase, valuesHash string) *provisioningv1.DPU {
		dpu := &provisioningv1.DPU{Status: provisioningv1.DPUStatus{Phase: phase}}
		if valuesHash != "" {
			dpu.Labels = map[string]string{cutil.DPUDeviceValuesHashLabel: valuesHash}
		}
		return dpu
	}
	for name, tc := range map[string]struct {
		oldPhase, newPhase   provisioningv1.DPUPhase
		oldValues, newValues string
		want                 bool
	}{
		"enters Ready":                  {oldPhase: provisioningv1.DPUOSInstalling, newPhase: provisioningv1.DPUReady, want: true},
		"leaves Ready":                  {oldPhase: provisioningv1.DPUReady, newPhase: provisioningv1.DPUError, want: true},
		"stays Ready":                   {oldPhase: provisioningv1.DPUReady, newPhase: provisioningv1.DPUReady, want: false},
		"other phase moves":             {oldPhase: provisioningv1.DPUInitializing, newPhase: provisioningv1.DPUOSInstalling, want: false},
		"Ready DPU gets new values":     {oldPhase: provisioningv1.DPUReady, newPhase: provisioningv1.DPUReady, oldValues: "old", newValues: "new", want: true},
		"Ready DPU keeps its values":    {oldPhase: provisioningv1.DPUReady, newPhase: provisioningv1.DPUReady, oldValues: "same", newValues: "same", want: false},
		"not Ready DPU gets new values": {oldPhase: provisioningv1.DPUOSInstalling, newPhase: provisioningv1.DPUOSInstalling, oldValues: "old", newValues: "new", want: false},
	} {
		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)
			p := dpuReadyPhasePredicate()
			g.Expect(p.Update(event.UpdateEvent{ObjectOld: dpuIn(tc.oldPhase, tc.oldValues), ObjectNew: dpuIn(tc.newPhase, tc.newValues)})).To(Equal(tc.want))
			g.Expect(p.Create(event.CreateEvent{Object: dpuIn(tc.newPhase, tc.newValues)})).To(BeFalse())
		})
	}
}

// TestDPUReadyForDevice checks that a Ready DPU rendered from a DPUFlavorTemplate only ends the
// rollout of its DPUDevice once it was rendered from the device's current values, and that any
// other Ready DPU ends it.
func TestDPUReadyForDevice(t *testing.T) {
	g := NewWithT(t)
	device := &provisioningv1.DPUDevice{Spec: provisioningv1.DPUDeviceSpec{
		Values: &runtime.RawExtension{Raw: []byte(`{"mtu":9000}`)},
	}}
	currentValues, err := dpuflavortemplate.ValuesHash(device.Spec.Values)
	g.Expect(err).NotTo(HaveOccurred())

	for name, tc := range map[string]struct {
		phase  provisioningv1.DPUPhase
		labels map[string]string
		want   bool
	}{
		"Ready without a template":          {phase: provisioningv1.DPUReady, want: true},
		"Ready from the current values":     {phase: provisioningv1.DPUReady, labels: map[string]string{cutil.DPUDeviceValuesHashLabel: currentValues}, want: true},
		"Ready from the previous values":    {phase: provisioningv1.DPUReady, labels: map[string]string{cutil.DPUDeviceValuesHashLabel: "previous"}, want: false},
		"not Ready from the current values": {phase: provisioningv1.DPUOSInstalling, labels: map[string]string{cutil.DPUDeviceValuesHashLabel: currentValues}, want: false},
	} {
		t.Run(name, func(t *testing.T) {
			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{Labels: tc.labels},
				Status:     provisioningv1.DPUStatus{Phase: tc.phase},
			}
			NewWithT(t).Expect(dpuReadyForDevice(dpu, device)).To(Equal(tc.want))
		})
	}
}

// TestEnsureHostlessDPUNodePropagatesTraceContext checks that the hostless DPUNode created for a
// DPUDevice continues the device's trace.
func TestEnsureHostlessDPUNodePropagatesTraceContext(t *testing.T) {
	t.Cleanup(tracing.EnableForTest())
	g := NewWithT(t)
	scheme := runtime.NewScheme()
	g.Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	device := &provisioningv1.DPUDevice{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "device",
			Namespace:   "ns",
			UID:         "device-uid",
			Annotations: map[string]string{tracing.TraceParentAnnotation: testTraceparent},
		},
	}
	r := &DPUDeviceReconciler{Client: c}

	name, err := r.ensureHostlessDPUNode(tracedDeviceContext(device), device)
	g.Expect(err).NotTo(HaveOccurred())

	got := &provisioningv1.DPUNode{}
	g.Expect(c.Get(context.Background(), types.NamespacedName{Namespace: "ns", Name: name}, got)).To(Succeed())
	g.Expect(got.Annotations).To(HaveKeyWithValue(tracing.GeneratedTraceParentAnnotation, testTraceparent))
}
