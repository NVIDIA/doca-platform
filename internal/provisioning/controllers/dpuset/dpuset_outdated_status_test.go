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
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// outdatedFixture is a small helper to set up a DPUSet + DPU pair backed by a
// fake client with status subresource support, then invoke
// reconcileDPUOutdatedStatus and reload the DPU for assertions.
type outdatedFixture struct {
	ctx        context.Context
	reconciler *DPUSetReconciler
	dpuSet     *provisioningv1.DPUSet
	dpus       []*provisioningv1.DPU
}

func newOutdatedFixture(strategy provisioningv1.StrategyType, dpus ...*provisioningv1.DPU) *outdatedFixture {
	scheme := runtime.NewScheme()
	Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())

	dpuSet := &provisioningv1.DPUSet{
		ObjectMeta: metav1.ObjectMeta{Name: "test-dpuset", Namespace: testNamespace},
		Spec: provisioningv1.DPUSetSpec{
			Strategy: provisioningv1.DPUSetStrategy{Type: strategy},
			DPUTemplate: provisioningv1.DPUTemplate{
				Spec: provisioningv1.DPUTemplateSpec{
					BFB:        &provisioningv1.BFBReference{Name: "bfb-v2"},
					DPUFlavor:  "flavor-a",
					SecureBoot: nil,
				},
			},
		},
	}

	objs := []client.Object{dpuSet}
	for _, dpu := range dpus {
		objs = append(objs, dpu)
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&provisioningv1.DPU{}).
		Build()

	return &outdatedFixture{
		ctx: context.Background(),
		reconciler: &DPUSetReconciler{
			Client:   fakeClient,
			Scheme:   scheme,
			Recorder: record.NewFakeRecorder(10),
		},
		dpuSet: dpuSet,
		dpus:   dpus,
	}
}

func (f *outdatedFixture) dpuMap() map[string]provisioningv1.DPU {
	m := make(map[string]provisioningv1.DPU, len(f.dpus))
	for _, d := range f.dpus {
		m[d.Spec.DPUDeviceName] = *d
	}
	return m
}

func (f *outdatedFixture) reloadDPU(name string) *provisioningv1.DPU {
	dpu := &provisioningv1.DPU{}
	Expect(f.reconciler.Get(f.ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, dpu)).To(Succeed())
	return dpu
}

func newOutdatedTestDPU(name, deviceName, bfb, flavor string) *provisioningv1.DPU {
	return &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Spec: provisioningv1.DPUSpec{
			DPUNodeName:   name + "-node",
			DPUDeviceName: deviceName,
			BFB:           ptr.To(bfb),
			DPUFlavor:     flavor,
		},
	}
}

var _ = Describe("DPUSetReconciler reconcileDPUOutdatedStatus", func() {

	Context("under OnDelete strategy", func() {
		It("sets dpu.status.outdated when the BFB diverges", func() {
			dpu := newOutdatedTestDPU("dpu-1", "dev-1", "bfb-v1", "flavor-a")
			f := newOutdatedFixture(provisioningv1.OnDeleteStrategyType, dpu)

			Expect(f.reconciler.reconcileDPUOutdatedStatus(f.ctx, f.dpuSet, f.dpuMap(), nil)).To(Succeed())

			got := f.reloadDPU("dpu-1")
			Expect(got.Status.Outdated).ToNot(BeNil())
			Expect(got.Status.Outdated.Reason).To(Equal(provisioningv1.DPUOutdatedReasonBFB))
			Expect(got.Status.Outdated.Message).To(ContainSubstring("BFB: bfb-v1 -> bfb-v2"))
			Expect(got.Status.Outdated.TimeStamp.IsZero()).To(BeFalse())
		})

		It("clears dpu.status.outdated once the DPU matches the template again", func() {
			dpu := newOutdatedTestDPU("dpu-2", "dev-2", "bfb-v2", "flavor-a")
			// Pre-populate a stale outdated struct.
			dpu.Status.Outdated = &provisioningv1.DPUOutdated{
				TimeStamp: metav1.Now(),
				Reason:    provisioningv1.DPUOutdatedReasonBFB,
				Message:   "stale message",
			}
			f := newOutdatedFixture(provisioningv1.OnDeleteStrategyType, dpu)
			Expect(f.reconciler.Status().Update(f.ctx, dpu)).To(Succeed())

			Expect(f.reconciler.reconcileDPUOutdatedStatus(f.ctx, f.dpuSet, f.dpuMap(), nil)).To(Succeed())

			got := f.reloadDPU("dpu-2")
			Expect(got.Status.Outdated).To(BeNil())
		})

		It("does not modify the DPU when nothing has changed and the struct is absent", func() {
			dpu := newOutdatedTestDPU("dpu-3", "dev-3", "bfb-v2", "flavor-a")
			f := newOutdatedFixture(provisioningv1.OnDeleteStrategyType, dpu)
			initial := f.reloadDPU("dpu-3")

			Expect(f.reconciler.reconcileDPUOutdatedStatus(f.ctx, f.dpuSet, f.dpuMap(), nil)).To(Succeed())

			got := f.reloadDPU("dpu-3")
			Expect(got.ResourceVersion).To(Equal(initial.ResourceVersion), "DPU must not be patched when no change is needed")
			Expect(got.Status.Outdated).To(BeNil())
		})

		It("skips DPUs that are being deleted", func() {
			now := metav1.Now()
			dpu := newOutdatedTestDPU("dpu-4", "dev-4", "bfb-v1", "flavor-a")
			dpu.DeletionTimestamp = &now
			dpu.Finalizers = []string{"test-finalizer"}
			f := newOutdatedFixture(provisioningv1.OnDeleteStrategyType, dpu)

			Expect(f.reconciler.reconcileDPUOutdatedStatus(f.ctx, f.dpuSet, f.dpuMap(), nil)).To(Succeed())

			got := f.reloadDPU("dpu-4")
			Expect(got.Status.Outdated).To(BeNil())
		})
	})

	Context("under RollingUpdate strategy", func() {
		It("sets dpu.status.outdated when BFB diverges", func() {
			dpu := newOutdatedTestDPU("dpu-5", "dev-5", "bfb-v1", "flavor-a")
			f := newOutdatedFixture(provisioningv1.RollingUpdateStrategyType, dpu)

			Expect(f.reconciler.reconcileDPUOutdatedStatus(f.ctx, f.dpuSet, f.dpuMap(), nil)).To(Succeed())

			got := f.reloadDPU("dpu-5")
			Expect(got.Status.Outdated).ToNot(BeNil())
			Expect(got.Status.Outdated.Reason).To(Equal(provisioningv1.DPUOutdatedReasonBFB))
			Expect(got.Status.Outdated.Message).To(ContainSubstring("BFB: bfb-v1 -> bfb-v2"))
		})
	})

	Context("multi-field drift", func() {
		It("reports BFB reason and lists BFB and DPUFlavor diffs in the message", func() {
			dpu := newOutdatedTestDPU("dpu-7", "dev-7", "bfb-v1", "flavor-old")
			f := newOutdatedFixture(provisioningv1.OnDeleteStrategyType, dpu)
			// Template currently has bfb-v2 + flavor-a, so DPU diverges on both.

			Expect(f.reconciler.reconcileDPUOutdatedStatus(f.ctx, f.dpuSet, f.dpuMap(), nil)).To(Succeed())

			got := f.reloadDPU("dpu-7")
			Expect(got.Status.Outdated).ToNot(BeNil())
			Expect(got.Status.Outdated.Reason).To(Equal(provisioningv1.DPUOutdatedReasonBFB))
			Expect(got.Status.Outdated.Message).To(ContainSubstring("BFB: bfb-v1 -> bfb-v2"))
			Expect(got.Status.Outdated.Message).To(ContainSubstring("DPUFlavor: flavor-old -> flavor-a"))
		})
	})

	Context("conditions slice field-owner boundary", func() {
		It("preserves DPU-controller-owned conditions when patching status.outdated", func() {
			dpu := newOutdatedTestDPU("dpu-8", "dev-8", "bfb-v1", "flavor-a")
			// Pre-existing condition owned by the DPU controller (e.g., Ready).
			dpu.Status.Conditions = []metav1.Condition{
				{
					Type:               string(provisioningv1.DPUCondReady),
					Status:             metav1.ConditionTrue,
					Reason:             "DPUReady",
					LastTransitionTime: metav1.Now(),
				},
			}
			f := newOutdatedFixture(provisioningv1.OnDeleteStrategyType, dpu)
			Expect(f.reconciler.Status().Update(f.ctx, dpu)).To(Succeed())

			Expect(f.reconciler.reconcileDPUOutdatedStatus(f.ctx, f.dpuSet, f.dpuMap(), nil)).To(Succeed())

			got := f.reloadDPU("dpu-8")
			Expect(got.Status.Outdated).ToNot(BeNil())
			Expect(got.Status.Outdated.Reason).To(Equal(provisioningv1.DPUOutdatedReasonBFB))
			// DPU-controller-owned conditions slice must survive the status.outdated patch.
			Expect(got.Status.Conditions).To(HaveLen(1))
			Expect(got.Status.Conditions[0].Type).To(Equal(string(provisioningv1.DPUCondReady)))
			Expect(got.Status.Conditions[0].Status).To(Equal(metav1.ConditionTrue))
		})
	})

	Context("multiple DPUs in the same map", func() {
		It("processes each DPU independently", func() {
			outdated := newOutdatedTestDPU("dpu-9a", "dev-9a", "bfb-v1", "flavor-a") // BFB diverges
			matching := newOutdatedTestDPU("dpu-9b", "dev-9b", "bfb-v2", "flavor-a") // matches template

			f := newOutdatedFixture(provisioningv1.OnDeleteStrategyType, outdated, matching)

			Expect(f.reconciler.reconcileDPUOutdatedStatus(f.ctx, f.dpuSet, f.dpuMap(), nil)).To(Succeed())

			gotOutdated := f.reloadDPU("dpu-9a")
			Expect(gotOutdated.Status.Outdated).ToNot(BeNil())
			Expect(gotOutdated.Status.Outdated.Reason).To(Equal(provisioningv1.DPUOutdatedReasonBFB))

			gotMatching := f.reloadDPU("dpu-9b")
			Expect(gotMatching.Status.Outdated).To(BeNil())
		})
	})

	Context("stale dpuMap snapshot", func() {
		It("tolerates NotFound for DPUs deleted earlier in the same reconcile and keeps processing the rest", func() {
			// Simulates onDelete / rolloutRolling deleting one DPU mid-reconcile:
			// the dpuMap snapshot still has the entry, but the object no longer
			// exists on the API server.
			deleted := newOutdatedTestDPU("dpu-stale", "dev-stale", "bfb-v1", "flavor-a")  // BFB diverges
			survivor := newOutdatedTestDPU("dpu-alive", "dev-alive", "bfb-v1", "flavor-a") // BFB diverges

			f := newOutdatedFixture(provisioningv1.OnDeleteStrategyType, deleted, survivor)
			snapshot := f.dpuMap()
			// Remove the DPU from the API server after capturing the snapshot.
			Expect(f.reconciler.Delete(f.ctx, deleted)).To(Succeed())

			Expect(f.reconciler.reconcileDPUOutdatedStatus(f.ctx, f.dpuSet, snapshot, nil)).To(Succeed())

			gotSurvivor := f.reloadDPU("dpu-alive")
			Expect(gotSurvivor.Status.Outdated).ToNot(BeNil())
			Expect(gotSurvivor.Status.Outdated.Reason).To(Equal(provisioningv1.DPUOutdatedReasonBFB))

			err := f.reconciler.Get(f.ctx, types.NamespacedName{Name: "dpu-stale", Namespace: testNamespace}, &provisioningv1.DPU{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue(), "deleted DPU must remain absent")
		})
	})

	Context("with SecureBoot drift under OnDelete", func() {
		It("sets dpu.status.outdated with OutdatedSecureBoot reason", func() {
			dpu := newOutdatedTestDPU("dpu-10", "dev-10", "bfb-v2", "flavor-a")
			f := newOutdatedFixture(provisioningv1.OnDeleteStrategyType, dpu)
			f.dpuSet.Spec.DPUTemplate.Spec.SecureBoot = ptr.To(true)

			Expect(f.reconciler.reconcileDPUOutdatedStatus(f.ctx, f.dpuSet, f.dpuMap(), nil)).To(Succeed())

			got := f.reloadDPU("dpu-10")
			Expect(got.Status.Outdated).ToNot(BeNil())
			Expect(got.Status.Outdated.Reason).To(Equal(provisioningv1.DPUOutdatedReasonSecureBoot))
			Expect(got.Status.Outdated.Message).To(ContainSubstring("SecureBoot: <nil> -> true"))
		})
	})

	Context("TimeStamp semantics", func() {
		It("preserves TimeStamp across reconciles when Reason is unchanged", func() {
			// Pre-populate the same outdated struct that the next reconcile will produce
			// (BFB drift), so outdatedNeedsUpdate returns false and the patch is skipped.
			// Use second-level precision because metav1.Time serializes via RFC3339.
			dpu := newOutdatedTestDPU("dpu-11", "dev-11", "bfb-v1", "flavor-a")
			original := metav1.NewTime(time.Now().Add(-time.Hour).Truncate(time.Second))
			dpu.Status.Outdated = &provisioningv1.DPUOutdated{
				TimeStamp: original,
				Reason:    provisioningv1.DPUOutdatedReasonBFB,
				Message:   "DPU template has changed (BFB: bfb-v1 -> bfb-v2).",
			}
			f := newOutdatedFixture(provisioningv1.OnDeleteStrategyType, dpu)
			Expect(f.reconciler.Status().Update(f.ctx, dpu)).To(Succeed())

			Expect(f.reconciler.reconcileDPUOutdatedStatus(f.ctx, f.dpuSet, f.dpuMap(), nil)).To(Succeed())

			got := f.reloadDPU("dpu-11")
			Expect(got.Status.Outdated).ToNot(BeNil())
			Expect(got.Status.Outdated.Reason).To(Equal(provisioningv1.DPUOutdatedReasonBFB))
			Expect(got.Status.Outdated.TimeStamp.Equal(&original)).To(BeTrue(), "TimeStamp should be preserved when Reason is unchanged")
		})

		It("refreshes TimeStamp when Reason transitions to a different drifted field", func() {
			// DPU initially marked outdated due to DPUFlavor, but the live drift is now BFB.
			// Use an offset larger than 1s so the comparison survives second-level rounding.
			dpu := newOutdatedTestDPU("dpu-12", "dev-12", "bfb-v1", "flavor-a")
			original := metav1.NewTime(time.Now().Add(-time.Hour).Truncate(time.Second))
			dpu.Status.Outdated = &provisioningv1.DPUOutdated{
				TimeStamp: original,
				Reason:    provisioningv1.DPUOutdatedReasonDPUFlavor,
				Message:   "old message",
			}
			f := newOutdatedFixture(provisioningv1.OnDeleteStrategyType, dpu)
			Expect(f.reconciler.Status().Update(f.ctx, dpu)).To(Succeed())

			Expect(f.reconciler.reconcileDPUOutdatedStatus(f.ctx, f.dpuSet, f.dpuMap(), nil)).To(Succeed())

			got := f.reloadDPU("dpu-12")
			Expect(got.Status.Outdated).ToNot(BeNil())
			Expect(got.Status.Outdated.Reason).To(Equal(provisioningv1.DPUOutdatedReasonBFB))
			Expect(got.Status.Outdated.TimeStamp.After(original.Time)).To(BeTrue(), "TimeStamp should advance when Reason changes")
		})
	})
})
