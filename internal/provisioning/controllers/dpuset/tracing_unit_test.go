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

package dpuset

import (
	"context"
	"errors"
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/utils/tracing"

	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

const (
	testTraceparent       = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	testDeviceTraceparent = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
)

// tracedDPUSetContext returns the context of a DPUSet reconcile that arrived with testTraceparent.
func tracedDPUSetContext(dpuSet *provisioningv1.DPUSet) context.Context {
	ctx, endSpan := tracing.ReconcileSpan(context.Background(), DPUSetControllerName, dpuSet)
	endSpan(nil)
	return tracing.WithTriggers(ctx, dpuSet, traceOptionsOf)
}

// TestCreateDPUPropagatesTraceContext checks that a DPU created for a DPUDevice continues the
// device's trace rather than the trace of the DPUSet reconcile that creates it.
func TestCreateDPUPropagatesTraceContext(t *testing.T) {
	t.Cleanup(tracing.EnableForTest())
	g := NewWithT(t)
	scheme := runtime.NewScheme()
	g.Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())

	dpuSet := &provisioningv1.DPUSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "set",
			Namespace:   "ns",
			UID:         "set-uid",
			Annotations: map[string]string{tracing.TraceParentAnnotation: testTraceparent},
		},
		Spec: provisioningv1.DPUSetSpec{
			DPUTemplate: provisioningv1.DPUTemplate{
				Spec: provisioningv1.DPUTemplateSpec{
					DPUFlavor: ptr.To("flavor"),
				},
			},
		},
	}
	device := &provisioningv1.DPUDevice{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "device",
			Namespace:   "ns",
			Labels:      map[string]string{provisioningv1.DPUNodeNameLabel: "node"},
			Annotations: map[string]string{tracing.TraceParentAnnotation: testDeviceTraceparent},
		},
		Spec: provisioningv1.DPUDeviceSpec{SerialNumber: "serial"},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &DPUSetReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(1)}

	_, _, err := r.createMissingDPUs(tracedDPUSetContext(dpuSet), dpuSet, map[string]provisioningv1.DPUDevice{"device": *device}, nil)
	g.Expect(err).NotTo(HaveOccurred())

	got := &provisioningv1.DPU{}
	g.Expect(c.Get(context.Background(), types.NamespacedName{
		Namespace: "ns",
		Name:      cutil.GenerateDPUName("node", "device"),
	}, got)).To(Succeed())
	g.Expect(got.Annotations).To(HaveKeyWithValue(tracing.GeneratedTraceParentAnnotation, testDeviceTraceparent),
		"a new DPU continues its DPUDevice's trace, not the DPUSet's")
}

// TestCreateDPUWritesNoTraceContextWithoutUpstream checks that createDPU writes no trace context
// when neither the DPUDevice nor the DPUSet reconcile carries one.
func TestCreateDPUWritesNoTraceContextWithoutUpstream(t *testing.T) {
	g := NewWithT(t)
	scheme := runtime.NewScheme()
	g.Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())

	dpuSet := &provisioningv1.DPUSet{
		ObjectMeta: metav1.ObjectMeta{Name: "set", Namespace: "ns", UID: "set-uid"},
		Spec: provisioningv1.DPUSetSpec{
			DPUTemplate: provisioningv1.DPUTemplate{
				Spec: provisioningv1.DPUTemplateSpec{DPUFlavor: ptr.To("flavor")},
			},
		},
	}
	device := &provisioningv1.DPUDevice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "device",
			Namespace: "ns",
			Labels:    map[string]string{provisioningv1.DPUNodeNameLabel: "node"},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &DPUSetReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(1)}

	_, _, err := r.createMissingDPUs(tracing.WithTriggers(context.Background(), dpuSet, traceOptionsOf), dpuSet, map[string]provisioningv1.DPUDevice{"device": *device}, nil)
	g.Expect(err).NotTo(HaveOccurred())

	got := &provisioningv1.DPU{}
	g.Expect(c.Get(context.Background(), client.ObjectKey{Namespace: "ns", Name: cutil.GenerateDPUName("node", "device")}, got)).To(Succeed())
	g.Expect(got.Annotations).NotTo(HaveKey(tracing.GeneratedTraceParentAnnotation))
}

// TestTraceOptionsOfDerivesOnlyDeviceWorkflow checks that, with traceOptionsOf, a trigger without a
// trace hands on the workflow derived from it only when it is a DPUDevice, the one provisioning
// object whose controller exports the root span of that workflow.
func TestTraceOptionsOfDerivesOnlyDeviceWorkflow(t *testing.T) {
	dpuSet := &provisioningv1.DPUSet{ObjectMeta: metav1.ObjectMeta{Name: "set", Namespace: "ns", UID: "set-uid", Generation: 1}}
	device := &provisioningv1.DPUDevice{ObjectMeta: metav1.ObjectMeta{Name: "device", Namespace: "ns", UID: "device-uid", Generation: 1}}
	deviceWorkflow := &provisioningv1.DPU{}
	tracing.InjectFromObject(device, deviceWorkflow, tracing.WorkflowRoot())

	for name, tc := range map[string]struct {
		trigger client.Object
		want    map[string]string
	}{
		"DPUDevice": {trigger: device, want: deviceWorkflow.Annotations},
		"DPUSet":    {trigger: dpuSet, want: nil},
	} {
		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := tracing.WithTriggers(context.Background(), dpuSet, traceOptionsOf)
			dpu := &provisioningv1.DPU{ObjectMeta: metav1.ObjectMeta{Name: "dpu", Namespace: "ns"}}
			tracing.RecordTrigger(ctx, dpu, tc.trigger)
			tracing.InjectTrigger(ctx, dpu)
			g.Expect(dpu.Annotations).To(Equal(tc.want))
		})
	}
}

// TestRolloutTraceSource checks that a rollout uses the changed DPUFlavorTemplate's trace for a
// drift caused only by its body, keeps the DPUDevice's trace for a values-only drift, and uses the
// DPUSet's trace for everything else.
func TestRolloutTraceSource(t *testing.T) {
	dpuSet := &provisioningv1.DPUSet{}
	template := &provisioningv1.DPUFlavorTemplate{}
	for name, tc := range map[string]struct {
		reasons []dpuDriftReason
		eval    templateEval
		want    client.Object
	}{
		"BFB":                          {[]dpuDriftReason{driftReasonBFB}, templateEval{}, dpuSet},
		"SecureBoot":                   {[]dpuDriftReason{driftReasonSecureBoot}, templateEval{}, dpuSet},
		"BlueFieldSoftware":            {[]dpuDriftReason{driftReasonBlueFieldSoftware}, templateEval{}, dpuSet},
		"cluster selector":             {[]dpuDriftReason{driftReasonClusterSelector}, templateEval{}, dpuSet},
		"static DPUFlavor or swap":     {[]dpuDriftReason{driftReasonDPUFlavor}, templateEval{disrupt: true}, dpuSet},
		"DPUFlavorTemplate body":       {[]dpuDriftReason{driftReasonDPUFlavor}, templateEval{disrupt: true, changedTemplate: template}, template},
		"DPUDevice values only":        {[]dpuDriftReason{driftReasonDPUFlavor}, templateEval{disrupt: true, valuesOnly: true}, nil},
		"template plus a DPUSet field": {[]dpuDriftReason{driftReasonBFB, driftReasonDPUFlavor}, templateEval{disrupt: true, changedTemplate: template}, dpuSet},
		"values plus a DPUSet field":   {[]dpuDriftReason{driftReasonBFB, driftReasonDPUFlavor}, templateEval{disrupt: true, valuesOnly: true}, dpuSet},
	} {
		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)
			got := rolloutTraceSource(dpuSet, &provisioningv1.DPU{}, nil, dpuDrift{Reasons: tc.reasons}, tc.eval)
			if tc.want == nil {
				g.Expect(got).To(BeNil())
				return
			}
			g.Expect(got).To(BeIdenticalTo(tc.want))
		})
	}
}

// TestClusterSelectorTraceSource checks that cluster selector drift continues the DPUSet's trace
// when the DPUSet's selector changed, and the assigned DPUCluster's trace when its labels changed.
func TestClusterSelectorTraceSource(t *testing.T) {
	oldSelector := &metav1.LabelSelector{MatchLabels: map[string]string{"pool": "old"}}
	newSelector := &metav1.LabelSelector{MatchLabels: map[string]string{"pool": "new"}}
	dpuClusters := []provisioningv1.DPUCluster{
		{ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "ns"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "assigned", Namespace: "ns"}},
	}
	for name, tc := range map[string]struct {
		dpuSelector *metav1.LabelSelector
		dpuHash     string
		clusterName string
		wantName    string
	}{
		"DPUSet selector not yet copied to the DPU": {oldSelector, "current", "assigned", "set"},
		"DPU template hash lags the DPUSet":         {newSelector, "old", "assigned", "set"},
		"assigned DPUCluster labels changed":        {newSelector, "current", "assigned", "assigned"},
		"assigned DPUCluster no longer exists":      {newSelector, "current", "deleted", "set"},
	} {
		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)
			dpuSet := &provisioningv1.DPUSet{
				ObjectMeta: metav1.ObjectMeta{Name: "set", Labels: map[string]string{cutil.DPUSetDPUTemplateSpecHashLabelKey: "current"}},
				Spec: provisioningv1.DPUSetSpec{DPUTemplate: provisioningv1.DPUTemplate{
					Spec: provisioningv1.DPUTemplateSpec{Cluster: &provisioningv1.ClusterSpec{Selector: newSelector}},
				}},
			}
			dpu := &provisioningv1.DPU{ObjectMeta: metav1.ObjectMeta{
				Name:   "dpu",
				Labels: map[string]string{cutil.DPUSetDPUTemplateSpecHashLabelKey: tc.dpuHash},
			}}
			dpu.Spec.Cluster.Name, dpu.Spec.Cluster.Namespace = tc.clusterName, "ns"
			dpu.Spec.Cluster.Selector = tc.dpuSelector

			got := clusterSelectorTraceSource(dpuSet, dpu, dpuClusters)

			g.Expect(got.GetName()).To(Equal(tc.wantName))
		})
	}
}

// TestOnDeleteStampsDPUClusterTraceOnDevice checks that deleting a DPU whose assigned DPUCluster's
// labels stopped matching the selector first copies that DPUCluster's trace onto the DPUDevice.
func TestOnDeleteStampsDPUClusterTraceOnDevice(t *testing.T) {
	t.Cleanup(tracing.EnableForTest())
	g := NewWithT(t)
	scheme := runtime.NewScheme()
	g.Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())

	selector := &metav1.LabelSelector{MatchLabels: map[string]string{"pool": "a"}}
	dpuSet := &provisioningv1.DPUSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "set",
			Namespace:   "ns",
			UID:         "set-uid",
			Labels:      map[string]string{cutil.DPUSetDPUTemplateSpecHashLabelKey: "current"},
			Annotations: map[string]string{tracing.TraceParentAnnotation: testTraceparent},
		},
		Spec: provisioningv1.DPUSetSpec{DPUTemplate: provisioningv1.DPUTemplate{
			Spec: provisioningv1.DPUTemplateSpec{Cluster: &provisioningv1.ClusterSpec{Selector: selector}},
		}},
	}
	clusterTraceparent := "00-11111111111111111111111111111111-2222222222222222-01"
	dpuCluster := provisioningv1.DPUCluster{ObjectMeta: metav1.ObjectMeta{
		Name:        "cluster",
		Namespace:   "ns",
		Labels:      map[string]string{"pool": "b"},
		Annotations: map[string]string{tracing.TraceParentAnnotation: clusterTraceparent},
	}}
	device := &provisioningv1.DPUDevice{ObjectMeta: metav1.ObjectMeta{
		Name:        "device",
		Namespace:   "ns",
		Annotations: map[string]string{tracing.TraceParentAnnotation: testDeviceTraceparent},
	}}
	dpu := &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dpu",
			Namespace: "ns",
			Labels:    map[string]string{cutil.DPUSetDPUTemplateSpecHashLabelKey: "current"},
		},
		Spec: provisioningv1.DPUSpec{DPUDeviceName: "device"},
	}
	dpu.Spec.Cluster.Name, dpu.Spec.Cluster.Namespace = "cluster", "ns"
	dpu.Spec.Cluster.Selector = selector
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(device, dpu).Build()
	r := &DPUSetReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(1)}

	g.Expect(r.onDelete(tracing.WithTriggers(context.Background(), dpuSet, traceOptionsOf), dpuSet,
		map[string]provisioningv1.DPU{"device": *dpu}, []provisioningv1.DPUCluster{dpuCluster})).To(Succeed())

	got := &provisioningv1.DPUDevice{}
	g.Expect(c.Get(context.Background(), client.ObjectKeyFromObject(device), got)).To(Succeed())
	g.Expect(got.Annotations).To(HaveKeyWithValue(tracing.GeneratedTraceParentAnnotation, clusterTraceparent))
	g.Expect(got.Annotations).NotTo(HaveKey(tracing.TraceParentAnnotation), "the stamp replaces the trace a consumer set on the device")
	g.Expect(c.Get(context.Background(), client.ObjectKeyFromObject(dpu), &provisioningv1.DPU{})).NotTo(Succeed())
}

// TestRolloutRollingStampsDPUSetTraceOnDevice checks that a rollout delete for BFB drift first
// copies the DPUSet's trace onto the DPUDevice, so the recreated DPU continues it. The DPUSet's
// trace is either the generated one its DPUDeployment stamped or one a consumer set on it.
func TestRolloutRollingStampsDPUSetTraceOnDevice(t *testing.T) {
	t.Cleanup(tracing.EnableForTest())
	for name, annotation := range map[string]string{
		"stamped by the DPUDeployment": tracing.GeneratedTraceParentAnnotation,
		"set by a consumer":            tracing.TraceParentAnnotation,
	} {
		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)
			scheme := runtime.NewScheme()
			g.Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())

			dpuSet := &provisioningv1.DPUSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "set",
					Namespace:   "ns",
					UID:         "set-uid",
					Annotations: map[string]string{annotation: testTraceparent},
				},
				Spec: provisioningv1.DPUSetSpec{DPUTemplate: provisioningv1.DPUTemplate{
					Spec: provisioningv1.DPUTemplateSpec{BFB: &provisioningv1.BFBReference{Name: "bfb-new"}},
				}},
			}
			device := &provisioningv1.DPUDevice{ObjectMeta: metav1.ObjectMeta{
				Name:        "device",
				Namespace:   "ns",
				Annotations: map[string]string{tracing.TraceParentAnnotation: testDeviceTraceparent},
			}}
			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{Name: "dpu", Namespace: "ns"},
				Spec:       provisioningv1.DPUSpec{DPUDeviceName: "device", BFB: ptr.To("bfb-old")},
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(device, dpu).Build()
			r := &DPUSetReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(1)}

			g.Expect(r.rolloutRolling(tracing.WithTriggers(context.Background(), dpuSet, traceOptionsOf), dpuSet, map[string]provisioningv1.DPU{"device": *dpu}, 1, nil, nil)).To(Succeed())

			got := &provisioningv1.DPUDevice{}
			g.Expect(c.Get(context.Background(), client.ObjectKeyFromObject(device), got)).To(Succeed())
			g.Expect(got.Annotations).To(HaveKeyWithValue(tracing.GeneratedTraceParentAnnotation, testTraceparent))
			g.Expect(got.Annotations).NotTo(HaveKey(tracing.TraceParentAnnotation), "the stamp replaces the trace a consumer set on the device")
			g.Expect(c.Get(context.Background(), client.ObjectKeyFromObject(dpu), &provisioningv1.DPU{})).NotTo(Succeed())
		})
	}
}

// TestRolloutRollingRemovesDeviceTraceForUntracedCause checks that a rollout delete caused by a
// DPUSet without a trace removes the trace the DPUDevice carried, so the recreated DPU does not
// continue an earlier, unrelated change, and that the device's other annotations are kept.
func TestRolloutRollingRemovesDeviceTraceForUntracedCause(t *testing.T) {
	t.Cleanup(tracing.EnableForTest())
	g := NewWithT(t)
	scheme := runtime.NewScheme()
	g.Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())

	dpuSet := &provisioningv1.DPUSet{
		ObjectMeta: metav1.ObjectMeta{Name: "set", Namespace: "ns", UID: "set-uid"},
		Spec: provisioningv1.DPUSetSpec{DPUTemplate: provisioningv1.DPUTemplate{
			Spec: provisioningv1.DPUTemplateSpec{BFB: &provisioningv1.BFBReference{Name: "bfb-new"}},
		}},
	}
	device := &provisioningv1.DPUDevice{ObjectMeta: metav1.ObjectMeta{
		Name:      "device",
		Namespace: "ns",
		Annotations: map[string]string{
			tracing.TraceParentAnnotation:          testDeviceTraceparent,
			tracing.TraceStateAnnotation:           "consumer=1",
			tracing.GeneratedTraceParentAnnotation: testTraceparent,
			tracing.GeneratedTraceStateAnnotation:  "vendor=old",
			"unrelated":                            "keep-me",
		},
	}}
	dpu := &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{Name: "dpu", Namespace: "ns"},
		Spec:       provisioningv1.DPUSpec{DPUDeviceName: "device", BFB: ptr.To("bfb-old")},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(device, dpu).Build()
	r := &DPUSetReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(1)}

	g.Expect(r.rolloutRolling(tracing.WithTriggers(context.Background(), dpuSet, traceOptionsOf), dpuSet, map[string]provisioningv1.DPU{"device": *dpu}, 1, nil, nil)).To(Succeed())

	got := &provisioningv1.DPUDevice{}
	g.Expect(c.Get(context.Background(), client.ObjectKeyFromObject(device), got)).To(Succeed())
	g.Expect(got.Annotations).To(Equal(map[string]string{"unrelated": "keep-me"}))
	g.Expect(c.Get(context.Background(), client.ObjectKeyFromObject(dpu), &provisioningv1.DPU{})).NotTo(Succeed())
}

// TestRolloutRollingStampsReplacementTraceOnDeletedDPU checks that a rollout delete first copies the
// DPUDevice's stamped trace onto the old DPU, so its cleanup lands in the same trace as the DPU
// recreated for that device, and that a DPU already being deleted keeps the trace its cleanup started in.
func TestRolloutRollingStampsReplacementTraceOnDeletedDPU(t *testing.T) {
	t.Cleanup(tracing.EnableForTest())
	const oldTraceparent = "00-33333333333333333333333333333333-4444444444444444-01"
	for name, tc := range map[string]struct {
		dpuSetAnnotations map[string]string
		alreadyDeleting   bool
	}{
		"traced cause":     {dpuSetAnnotations: map[string]string{tracing.TraceParentAnnotation: testTraceparent}},
		"untraced cause":   {},
		"already deleting": {dpuSetAnnotations: map[string]string{tracing.TraceParentAnnotation: testTraceparent}, alreadyDeleting: true},
	} {
		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)
			scheme := runtime.NewScheme()
			g.Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())

			dpuSet := &provisioningv1.DPUSet{
				ObjectMeta: metav1.ObjectMeta{Name: "set", Namespace: "ns", UID: "set-uid", Annotations: tc.dpuSetAnnotations},
				Spec: provisioningv1.DPUSetSpec{DPUTemplate: provisioningv1.DPUTemplate{
					Spec: provisioningv1.DPUTemplateSpec{BFB: &provisioningv1.BFBReference{Name: "bfb-new"}},
				}},
			}
			device := &provisioningv1.DPUDevice{ObjectMeta: metav1.ObjectMeta{
				Name:        "device",
				Namespace:   "ns",
				UID:         "device-uid",
				Generation:  1,
				Annotations: map[string]string{tracing.TraceParentAnnotation: testDeviceTraceparent},
			}}
			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "dpu",
					Namespace:   "ns",
					Finalizers:  []string{provisioningv1.DPUFinalizer},
					Annotations: map[string]string{tracing.GeneratedTraceParentAnnotation: oldTraceparent},
				},
				Spec: provisioningv1.DPUSpec{DPUDeviceName: "device", BFB: ptr.To("bfb-old")},
			}
			if tc.alreadyDeleting {
				dpu.DeletionTimestamp = ptr.To(metav1.Now())
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(device, dpu).Build()
			r := &DPUSetReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(1)}

			g.Expect(r.rolloutRolling(tracing.WithTriggers(context.Background(), dpuSet, traceOptionsOf), dpuSet, map[string]provisioningv1.DPU{"device": *dpu}, 1, nil, nil)).To(Succeed())

			gotDevice := &provisioningv1.DPUDevice{}
			g.Expect(c.Get(context.Background(), client.ObjectKeyFromObject(device), gotDevice)).To(Succeed())
			replacement := &provisioningv1.DPU{}
			g.Expect(tracing.InjectFromObject(gotDevice, replacement, tracing.WorkflowRoot())).To(BeTrue())
			wantTraceparent := replacement.Annotations[tracing.GeneratedTraceParentAnnotation]
			if tc.alreadyDeleting {
				wantTraceparent = oldTraceparent
			}

			got := &provisioningv1.DPU{}
			g.Expect(c.Get(context.Background(), client.ObjectKeyFromObject(dpu), got)).To(Succeed())
			g.Expect(got.DeletionTimestamp.IsZero()).To(BeFalse())
			g.Expect(got.Annotations).To(HaveKeyWithValue(tracing.GeneratedTraceParentAnnotation, wantTraceparent))
		})
	}
}

// TestRolloutRollingDeletesDPUWhenDPUStampFails checks that a rollout still deletes the old DPU when
// copying the trace onto it fails, as that only leaves the cleanup in its old trace.
func TestRolloutRollingDeletesDPUWhenDPUStampFails(t *testing.T) {
	g := NewWithT(t)
	r, dpuSet, _, dpu := newRolloutStampFixture(g, func(ctx context.Context, c client.WithWatch, obj client.Object, p client.Patch, opts ...client.PatchOption) error {
		return c.Patch(ctx, obj, p, opts...)
	})
	r.Client = interceptor.NewClient(r.Client.(client.WithWatch), interceptor.Funcs{
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, p client.Patch, opts ...client.PatchOption) error {
			if _, ok := obj.(*provisioningv1.DPU); ok {
				return apierrors.NewConflict(schema.GroupResource{Resource: "dpus"}, obj.GetName(), errors.New("changed"))
			}
			return c.Patch(ctx, obj, p, opts...)
		},
	})

	g.Expect(r.rolloutRolling(tracing.WithTriggers(context.Background(), dpuSet, traceOptionsOf), dpuSet, map[string]provisioningv1.DPU{"device": *dpu}, 1, nil, nil)).To(Succeed())

	g.Expect(r.Get(context.Background(), client.ObjectKeyFromObject(dpu), &provisioningv1.DPU{})).NotTo(Succeed())
}

// newRolloutStampFixture returns a traced DPUSet whose BFB drifted from its DPU, and a fake client
// holding that DPU and its DPUDevice with a consumer trace, whose DPUDevice patches go through patch.
func newRolloutStampFixture(g *WithT, patch func(context.Context, client.WithWatch, client.Object, client.Patch, ...client.PatchOption) error) (*DPUSetReconciler, *provisioningv1.DPUSet, *provisioningv1.DPUDevice, *provisioningv1.DPU) {
	scheme := runtime.NewScheme()
	g.Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
	dpuSet := &provisioningv1.DPUSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "set",
			Namespace:   "ns",
			UID:         "set-uid",
			Annotations: map[string]string{tracing.TraceParentAnnotation: testTraceparent},
		},
		Spec: provisioningv1.DPUSetSpec{DPUTemplate: provisioningv1.DPUTemplate{
			Spec: provisioningv1.DPUTemplateSpec{BFB: &provisioningv1.BFBReference{Name: "bfb-new"}},
		}},
	}
	device := &provisioningv1.DPUDevice{ObjectMeta: metav1.ObjectMeta{
		Name:        "device",
		Namespace:   "ns",
		Annotations: map[string]string{tracing.TraceParentAnnotation: testDeviceTraceparent},
	}}
	dpu := &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{Name: "dpu", Namespace: "ns"},
		Spec:       provisioningv1.DPUSpec{DPUDeviceName: "device", BFB: ptr.To("bfb-old")},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(device, dpu).
		WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, p client.Patch, opts ...client.PatchOption) error {
				if _, ok := obj.(*provisioningv1.DPUDevice); ok {
					return patch(ctx, c, obj, p, opts...)
				}
				return c.Patch(ctx, obj, p, opts...)
			},
		}).Build()
	return &DPUSetReconciler{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(1)}, dpuSet, device, dpu
}

// TestRolloutRollingRetriesDeviceStampOnConflict checks that a DPUDevice written by another writer
// after the rollout read it gets the stamp on a fresh read instead of losing that write, and that
// the DPU is deleted once the stamp is written.
func TestRolloutRollingRetriesDeviceStampOnConflict(t *testing.T) {
	t.Cleanup(tracing.EnableForTest())
	g := NewWithT(t)
	patches := 0
	r, dpuSet, device, dpu := newRolloutStampFixture(g, func(ctx context.Context, c client.WithWatch, obj client.Object, p client.Patch, opts ...client.PatchOption) error {
		patches++
		if patches == 1 {
			concurrent := &provisioningv1.DPUDevice{}
			g.Expect(c.Get(ctx, client.ObjectKeyFromObject(obj), concurrent)).To(Succeed())
			concurrent.Labels = map[string]string{"concurrent": "write"}
			g.Expect(c.Update(ctx, concurrent)).To(Succeed())
		}
		return c.Patch(ctx, obj, p, opts...)
	})

	g.Expect(r.rolloutRolling(tracing.WithTriggers(context.Background(), dpuSet, traceOptionsOf), dpuSet, map[string]provisioningv1.DPU{"device": *dpu}, 1, nil, nil)).To(Succeed())

	g.Expect(patches).To(Equal(2), "the conflicting patch is retried once")
	got := &provisioningv1.DPUDevice{}
	g.Expect(r.Get(context.Background(), client.ObjectKeyFromObject(device), got)).To(Succeed())
	g.Expect(got.Labels).To(HaveKeyWithValue("concurrent", "write"))
	g.Expect(got.Annotations).To(HaveKeyWithValue(tracing.GeneratedTraceParentAnnotation, testTraceparent))
	g.Expect(got.Annotations).NotTo(HaveKey(tracing.TraceParentAnnotation))
	g.Expect(r.Get(context.Background(), client.ObjectKeyFromObject(dpu), &provisioningv1.DPU{})).NotTo(Succeed())
}

// TestRolloutRollingKeepsDPUWhenDeviceStampFails checks that a rollout whose DPUDevice keeps
// conflicting returns the error and does not delete the DPU, so the next reconcile decides again.
func TestRolloutRollingKeepsDPUWhenDeviceStampFails(t *testing.T) {
	t.Cleanup(tracing.EnableForTest())
	g := NewWithT(t)
	r, dpuSet, _, dpu := newRolloutStampFixture(g, func(_ context.Context, _ client.WithWatch, obj client.Object, _ client.Patch, _ ...client.PatchOption) error {
		return apierrors.NewConflict(schema.GroupResource{Resource: "dpudevices"}, obj.GetName(), errors.New("changed"))
	})

	g.Expect(r.rolloutRolling(tracing.WithTriggers(context.Background(), dpuSet, traceOptionsOf), dpuSet, map[string]provisioningv1.DPU{"device": *dpu}, 1, nil, nil)).NotTo(Succeed())

	g.Expect(r.Get(context.Background(), client.ObjectKeyFromObject(dpu), &provisioningv1.DPU{})).To(Succeed())
}

// TestUpdatePCIAddressContinuesDeviceTrace checks that copying a new PCI address from the DPUDevice
// moves the DPU onto the device's trace.
func TestUpdatePCIAddressContinuesDeviceTrace(t *testing.T) {
	t.Cleanup(tracing.EnableForTest())
	g := NewWithT(t)
	scheme := runtime.NewScheme()
	g.Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())

	device := &provisioningv1.DPUDevice{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "device",
			Namespace:   "ns",
			Annotations: map[string]string{tracing.TraceParentAnnotation: testDeviceTraceparent},
		},
		Status: provisioningv1.DPUDeviceStatus{PCIAddress: ptr.To("0000:03:00")},
	}
	dpu := &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "dpu",
			Namespace:   "ns",
			Annotations: map[string]string{tracing.TraceParentAnnotation: testTraceparent},
		},
		Spec: provisioningv1.DPUSpec{DPUDeviceName: "device"},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dpu).Build()
	r := &DPUSetReconciler{Client: c, Scheme: scheme}

	g.Expect(r.updatePCIAddress(tracing.WithTriggers(context.Background(), &provisioningv1.DPUSet{}, traceOptionsOf), dpu, device)).To(Succeed())

	got := &provisioningv1.DPU{}
	g.Expect(c.Get(context.Background(), client.ObjectKeyFromObject(dpu), got)).To(Succeed())
	g.Expect(got.Annotations).To(HaveKeyWithValue(tracing.GeneratedTraceParentAnnotation, testDeviceTraceparent))
}

// TestUpdateDPUsStampsDPUSetTraceOnTemplateChange checks that an in-place node effect change from
// the DPUSet template moves the DPU onto the DPUSet's trace, or off its earlier trace when the
// DPUSet has none, and that a template hash sync alone leaves the DPU's trace context untouched.
func TestUpdateDPUsStampsDPUSetTraceOnTemplateChange(t *testing.T) {
	t.Cleanup(tracing.EnableForTest())
	const earlierTraceparent = "00-11111111111111111111111111111111-2222222222222222-01"
	drain := provisioningv1.NodeEffect{Action: provisioningv1.Action{Drain: ptr.To(true)}}
	for name, tc := range map[string]struct {
		dpuSetEffect      provisioningv1.NodeEffect
		dpuSetTraceparent string
		wantTraceparent   string
	}{
		"node effect changed": {
			dpuSetEffect:      drain,
			dpuSetTraceparent: testTraceparent,
			wantTraceparent:   testTraceparent,
		},
		"node effect changed by an untraced DPUSet": {
			dpuSetEffect: drain,
		},
		"hash sync only": {
			dpuSetTraceparent: testTraceparent,
			wantTraceparent:   earlierTraceparent,
		},
	} {
		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)
			scheme := runtime.NewScheme()
			g.Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())

			dpuSet := &provisioningv1.DPUSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "set",
					Namespace: "ns",
					UID:       "set-uid",
					Labels:    map[string]string{cutil.DPUSetDPUTemplateSpecHashLabelKey: "new"},
				},
				Spec: provisioningv1.DPUSetSpec{DPUTemplate: provisioningv1.DPUTemplate{
					Spec: provisioningv1.DPUTemplateSpec{NodeEffect: tc.dpuSetEffect},
				}},
			}
			if tc.dpuSetTraceparent != "" {
				dpuSet.Annotations = map[string]string{tracing.TraceParentAnnotation: tc.dpuSetTraceparent}
			}
			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "dpu",
					Namespace:   "ns",
					Labels:      map[string]string{cutil.DPUSetDPUTemplateSpecHashLabelKey: "old"},
					Annotations: map[string]string{tracing.GeneratedTraceParentAnnotation: earlierTraceparent},
				},
				Spec:   provisioningv1.DPUSpec{DPUDeviceName: "device"},
				Status: provisioningv1.DPUStatus{Phase: provisioningv1.DPUReady},
			}
			device := provisioningv1.DPUDevice{ObjectMeta: metav1.ObjectMeta{Name: "device", Namespace: "ns"}}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dpu).Build()
			r := &DPUSetReconciler{Client: c, Scheme: scheme}

			_, err := r.UpdateDPUs(tracedDPUSetContext(dpuSet), dpuSet,
				map[string]provisioningv1.DPU{"device": *dpu}, map[string]provisioningv1.DPUDevice{"device": device})
			g.Expect(err).NotTo(HaveOccurred())

			got := &provisioningv1.DPU{}
			g.Expect(c.Get(context.Background(), client.ObjectKeyFromObject(dpu), got)).To(Succeed())
			if tc.wantTraceparent == "" {
				g.Expect(got.Annotations).NotTo(HaveKey(tracing.GeneratedTraceParentAnnotation))
			} else {
				g.Expect(got.Annotations).To(HaveKeyWithValue(tracing.GeneratedTraceParentAnnotation, tc.wantTraceparent))
			}
		})
	}
}

// TestUpdateDPUsClusterMetadataTraceTrigger checks which object's trace a DPU continues after an
// in-place cluster node metadata change, for changes from the DPUSet template, the DPUDevice, or
// both.
func TestUpdateDPUsClusterMetadataTraceTrigger(t *testing.T) {
	t.Cleanup(tracing.EnableForTest())
	selector := &metav1.LabelSelector{MatchLabels: map[string]string{"pool": "a"}}
	for name, tc := range map[string]struct {
		lastLabels, lastAnnotations string
		dpuHash                     string
		template                    *provisioningv1.ClusterSpec
		device                      *provisioningv1.DPUDeviceClusterSpec
		wantTraceparent             string
	}{
		"unchanged": {
			lastLabels: `{"a":"1"}`, dpuHash: "current",
			template: &provisioningv1.ClusterSpec{NodeLabels: map[string]string{"a": "1"}},
		},
		"template label changed": {
			lastLabels: `{"a":"1"}`, dpuHash: "old",
			template:        &provisioningv1.ClusterSpec{NodeLabels: map[string]string{"a": "2"}},
			wantTraceparent: testTraceparent,
		},
		"device label added": {
			lastLabels: `{"a":"1"}`, dpuHash: "current",
			template:        &provisioningv1.ClusterSpec{NodeLabels: map[string]string{"a": "1"}},
			device:          &provisioningv1.DPUDeviceClusterSpec{NodeLabels: map[string]string{"b": "1"}},
			wantTraceparent: testDeviceTraceparent,
		},
		"device annotation changed": {
			lastAnnotations: `{"note":"x"}`, dpuHash: "current",
			device:          &provisioningv1.DPUDeviceClusterSpec{NodeAnnotations: map[string]string{"note": "y"}},
			wantTraceparent: testDeviceTraceparent,
		},
		"template and device changed": {
			lastLabels: `{"a":"1"}`, dpuHash: "old",
			template:        &provisioningv1.ClusterSpec{NodeLabels: map[string]string{"a": "2"}},
			device:          &provisioningv1.DPUDeviceClusterSpec{NodeLabels: map[string]string{"b": "1"}},
			wantTraceparent: testTraceparent,
		},
		"key removed with current template hash": {
			lastLabels: `{"b":"1"}`, dpuHash: "current",
			wantTraceparent: testDeviceTraceparent,
		},
		"key removed with lagging template hash": {
			lastLabels: `{"a":"1"}`, dpuHash: "old",
			wantTraceparent: testTraceparent,
		},
		"selector changed": {
			dpuHash:         "current",
			template:        &provisioningv1.ClusterSpec{Selector: selector},
			wantTraceparent: testTraceparent,
		},
	} {
		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)
			scheme := runtime.NewScheme()
			g.Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())

			dpuSet := &provisioningv1.DPUSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "set",
					Namespace:   "ns",
					UID:         "set-uid",
					Labels:      map[string]string{cutil.DPUSetDPUTemplateSpecHashLabelKey: "current"},
					Annotations: map[string]string{tracing.TraceParentAnnotation: testTraceparent},
				},
				Spec: provisioningv1.DPUSetSpec{DPUTemplate: provisioningv1.DPUTemplate{
					Spec: provisioningv1.DPUTemplateSpec{Cluster: tc.template},
				}},
			}
			device := provisioningv1.DPUDevice{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "device",
					Namespace:   "ns",
					Annotations: map[string]string{tracing.TraceParentAnnotation: testDeviceTraceparent},
				},
				Spec: provisioningv1.DPUDeviceSpec{Cluster: tc.device},
			}
			dpu := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "dpu",
					Namespace:   "ns",
					Labels:      map[string]string{cutil.DPUSetDPUTemplateSpecHashLabelKey: tc.dpuHash},
					Annotations: map[string]string{},
				},
				Spec: provisioningv1.DPUSpec{DPUDeviceName: "device"},
			}
			if tc.lastLabels != "" {
				dpu.Annotations[cutil.LastAppliedLabelsOnDPUKey] = tc.lastLabels
			}
			if tc.lastAnnotations != "" {
				dpu.Annotations[cutil.LastAppliedAnnotationsOnDPUKey] = tc.lastAnnotations
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dpu).Build()
			r := &DPUSetReconciler{Client: c, Scheme: scheme}

			_, err := r.UpdateDPUs(tracedDPUSetContext(dpuSet), dpuSet,
				map[string]provisioningv1.DPU{"device": *dpu}, map[string]provisioningv1.DPUDevice{"device": device})
			g.Expect(err).NotTo(HaveOccurred())

			got := &provisioningv1.DPU{}
			g.Expect(c.Get(context.Background(), client.ObjectKeyFromObject(dpu), got)).To(Succeed())
			if tc.wantTraceparent == "" {
				g.Expect(got.Annotations).NotTo(HaveKey(tracing.GeneratedTraceParentAnnotation))
			} else {
				g.Expect(got.Annotations).To(HaveKeyWithValue(tracing.GeneratedTraceParentAnnotation, tc.wantTraceparent))
			}
		})
	}
}

// TestUpdateDPUsStampsDeviceTraceOnDeviceLabelChange checks that a node label added on the
// DPUDevice moves the DPU onto the device's trace, not the DPUSet's.
func TestUpdateDPUsStampsDeviceTraceOnDeviceLabelChange(t *testing.T) {
	t.Cleanup(tracing.EnableForTest())
	g := NewWithT(t)
	scheme := runtime.NewScheme()
	g.Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())

	dpuSet := &provisioningv1.DPUSet{ObjectMeta: metav1.ObjectMeta{
		Name:        "set",
		Namespace:   "ns",
		UID:         "set-uid",
		Labels:      map[string]string{cutil.DPUSetDPUTemplateSpecHashLabelKey: "current"},
		Annotations: map[string]string{tracing.TraceParentAnnotation: testTraceparent},
	}}
	dpu := &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dpu",
			Namespace: "ns",
			Labels:    map[string]string{cutil.DPUSetDPUTemplateSpecHashLabelKey: "current"},
		},
		Spec:   provisioningv1.DPUSpec{DPUDeviceName: "device"},
		Status: provisioningv1.DPUStatus{Phase: provisioningv1.DPUReady},
	}
	device := provisioningv1.DPUDevice{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "device",
			Namespace:   "ns",
			Annotations: map[string]string{tracing.TraceParentAnnotation: testDeviceTraceparent},
		},
		Spec: provisioningv1.DPUDeviceSpec{Cluster: &provisioningv1.DPUDeviceClusterSpec{
			NodeLabels: map[string]string{"rack": "r1"},
		}},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dpu).Build()
	r := &DPUSetReconciler{Client: c, Scheme: scheme}

	_, err := r.UpdateDPUs(tracedDPUSetContext(dpuSet), dpuSet,
		map[string]provisioningv1.DPU{"device": *dpu}, map[string]provisioningv1.DPUDevice{"device": device})
	g.Expect(err).NotTo(HaveOccurred())

	got := &provisioningv1.DPU{}
	g.Expect(c.Get(context.Background(), client.ObjectKeyFromObject(dpu), got)).To(Succeed())
	g.Expect(got.Spec.Cluster.NodeLabels).To(HaveKeyWithValue("rack", "r1"))
	g.Expect(got.Annotations).To(HaveKeyWithValue(tracing.GeneratedTraceParentAnnotation, testDeviceTraceparent))
}
