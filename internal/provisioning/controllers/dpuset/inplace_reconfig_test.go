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
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("in-place reconfig", func() {
	var (
		ctx        context.Context
		reconciler *DPUSetReconciler
		dpuSet     *provisioningv1.DPUSet
		cl         client.Client
	)

	newSet := func(strategy provisioningv1.StrategyType, annotation string) *provisioningv1.DPUSet {
		set := &provisioningv1.DPUSet{
			ObjectMeta: metav1.ObjectMeta{Name: "test-dpuset", Namespace: testNamespace},
			Spec: provisioningv1.DPUSetSpec{
				Strategy: provisioningv1.DPUSetStrategy{Type: strategy},
			},
		}
		if annotation != "" {
			set.Annotations = map[string]string{cutil.DPUSetInPlaceReconfigAnnotation: annotation}
		}
		return set
	}

	readyAt := func(dpu *provisioningv1.DPU, at time.Time) {
		dpu.Status.Phase = provisioningv1.DPUReady
		dpu.Status.Conditions = []metav1.Condition{{
			Type:               provisioningv1.DPUCondReady.String(),
			Status:             metav1.ConditionTrue,
			Reason:             "Ready",
			LastTransitionTime: metav1.NewTime(at),
		}}
	}

	// A zero Ready transition counts as Ready before the round starts.
	markReady := func(dpu *provisioningv1.DPU) {
		readyAt(dpu, time.Time{})
	}

	withVersion := func(dpu *provisioningv1.DPU, version string) {
		if version == "" {
			dpu.Status.AgentStatus = nil
			return
		}
		dpu.Status.AgentStatus = &provisioningv1.AgentStatus{Version: version}
	}

	newDPU := func(name string) *provisioningv1.DPU {
		dpu := &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: testNamespace,
				Labels: map[string]string{
					cutil.DPUSetNameLabel:      "test-dpuset",
					cutil.DPUSetNamespaceLabel: testNamespace,
				},
			},
			Spec: provisioningv1.DPUSpec{DPUDeviceName: name},
		}
		withVersion(dpu, "v0.1.0")
		return dpu
	}

	setup := func(maxUnavailable int32, set *provisioningv1.DPUSet, dpus ...*provisioningv1.DPU) {
		scheme := runtime.NewScheme()
		Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
		objs := make([]client.Object, 0, len(dpus))
		for _, dpu := range dpus {
			objs = append(objs, dpu)
		}
		cl = fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(objs...).
			WithStatusSubresource(&provisioningv1.DPU{}).
			Build()
		reconciler = &DPUSetReconciler{
			Client:  cl,
			Options: DPUSetOptions{MaxUnavailableDPUNodes: maxUnavailable},
		}
		dpuSet = set
		ctx = context.Background()
	}

	reconcile := func(total int) {
		Expect(reconciler.reconcileInPlaceReconfig(ctx, dpuSet, total)).To(Succeed())
	}

	load := func(name string) *provisioningv1.DPU {
		got := &provisioningv1.DPU{}
		Expect(cl.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespace}, got)).To(Succeed())
		return got
	}

	It("starts one round and marks a Ready DPU under OnDelete", func() {
		dpu := newDPU("dpu-a")
		markReady(dpu)
		setup(1, newSet(provisioningv1.OnDeleteStrategyType, inPlaceReconfigTrue), dpu)

		reconcile(1)

		Expect(dpuSet.Status.DPUReconfig.Round).To(Equal(int32(1)))
		Expect(ptr.Deref(dpuSet.Status.DPUReconfig.Active, false)).To(BeTrue())
		Expect(dpuSet.Status.DPUReconfig.StartedAt).NotTo(BeNil())
		Expect(dpuSet.Annotations).To(HaveKeyWithValue(cutil.DPUSetInPlaceReconfigAnnotation, inPlaceReconfigTrue))
		got := load("dpu-a")
		Expect(ptr.Deref(got.Status.DPUReconfig.InProgress, false)).To(BeTrue())
		Expect(got.Status.DPUReconfig.Round).To(Equal(int32(1)))
		Expect(got.Status.DPUReconfig.CompletedRound).To(Equal(int32(0)))
	})

	It("does not bump an active round or mark past the shared budget", func() {
		first := newDPU("dpu-a")
		second := newDPU("dpu-b")
		markReady(first)
		markReady(second)
		setup(1, newSet(provisioningv1.RollingUpdateStrategyType, inPlaceReconfigTrue), first, second)

		reconcile(2)
		started := dpuSet.Status.DPUReconfig.StartedAt.Time
		reconcile(2)

		Expect(dpuSet.Status.DPUReconfig.Round).To(Equal(int32(1)))
		Expect(dpuSet.Status.DPUReconfig.StartedAt.Time).To(Equal(started))
		Expect(ptr.Deref(load("dpu-a").Status.DPUReconfig.InProgress, false)).To(BeTrue())
		Expect(load("dpu-b").Status.DPUReconfig).To(BeNil())
	})

	It("counts a missing DPU against the same budget as a delete", func() {
		dpu := newDPU("dpu-a")
		markReady(dpu)
		setup(1, newSet(provisioningv1.RollingUpdateStrategyType, inPlaceReconfigTrue), dpu)

		reconcile(2)

		Expect(load("dpu-a").Status.DPUReconfig).To(BeNil())
		Expect(ptr.Deref(dpuSet.Status.DPUReconfig.Active, false)).To(BeTrue())
		Expect(dpuSet.Annotations).To(HaveKey(cutil.DPUSetInPlaceReconfigAnnotation))
	})

	It("does not let an excluded device consume the only in-place slot", func() {
		dpu := newDPU("dpu-a")
		markReady(dpu)
		set := newSet(provisioningv1.RollingUpdateStrategyType, inPlaceReconfigTrue)
		set.Spec.DPUTemplate.Spec.AstraEnabled = ptr.To(true)
		devices := map[string]provisioningv1.DPUDevice{
			"bf4": {Status: provisioningv1.DPUDeviceStatus{DPUType: provisioningv1.DPUTypeBlueField4}},
			"bf3": {Status: provisioningv1.DPUDeviceStatus{DPUType: provisioningv1.DPUTypeBlueField3}},
		}
		setup(1, set, dpu)

		reconcile(provisionableDPUCount(set, devices, 0))

		Expect(ptr.Deref(load("dpu-a").Status.DPUReconfig.InProgress, false)).To(BeTrue())
	})

	It("starts the next Ready DPU in the slot freed by one that returned to Ready", func() {
		started := metav1.NewTime(time.Now().Add(-time.Hour))
		finished := newDPU("dpu-a")
		readyAt(finished, time.Now())
		finished.Status.DPUReconfig = &provisioningv1.DPUReconfigStatus{InProgress: ptr.To(true), Round: 1}
		waiting := newDPU("dpu-b")
		markReady(waiting)
		set := newSet(provisioningv1.RollingUpdateStrategyType, inPlaceReconfigTrue)
		set.Status.DPUReconfig = &provisioningv1.DPUSetReconfigStatus{
			Round:     1,
			Active:    ptr.To(true),
			StartedAt: &started,
		}
		setup(1, set, finished, waiting)

		reconcile(2)

		gotFinished := load("dpu-a")
		Expect(ptr.Deref(gotFinished.Status.DPUReconfig.InProgress, true)).To(BeFalse())
		Expect(gotFinished.Status.DPUReconfig.CompletedRound).To(Equal(int32(1)))
		gotWaiting := load("dpu-b")
		Expect(ptr.Deref(gotWaiting.Status.DPUReconfig.InProgress, false)).To(BeTrue())
		Expect(gotWaiting.Status.DPUReconfig.Round).To(Equal(int32(1)))
		Expect(dpuSet.Annotations).To(HaveKey(cutil.DPUSetInPlaceReconfigAnnotation))
	})

	It("holds a Ready DPU while another DPU already occupies the budget", func() {
		waiting := newDPU("dpu-ready")
		markReady(waiting)
		held := newDPU("dpu-install")
		held.Status.Phase = provisioningv1.DPUOSInstalling
		held.Status.Conditions = []metav1.Condition{{
			Type:   provisioningv1.DPUCondReady.String(),
			Status: metav1.ConditionFalse,
			Reason: "Installing",
		}}
		setup(1, newSet(provisioningv1.RollingUpdateStrategyType, inPlaceReconfigTrue), waiting, held)

		reconcile(2)

		Expect(load("dpu-ready").Status.DPUReconfig).To(BeNil())
		Expect(load("dpu-install").Status.DPUReconfig).To(BeNil())
		Expect(dpuSet.Annotations).To(HaveKey(cutil.DPUSetInPlaceReconfigAnnotation))
		Expect(ptr.Deref(dpuSet.Status.DPUReconfig.Active, false)).To(BeTrue())
	})

	DescribeTable("marks a post-config phase without taking another slot",
		func(phase provisioningv1.DPUPhase) {
			installing := newDPU("dpu-install")
			installing.Status.Phase = provisioningv1.DPUOSInstalling
			installing.Status.Conditions = []metav1.Condition{{
				Type:   provisioningv1.DPUCondReady.String(),
				Status: metav1.ConditionFalse,
				Reason: "Installing",
			}}
			joined := newDPU("dpu-joined")
			joined.Status.Phase = phase
			setup(1, newSet(provisioningv1.RollingUpdateStrategyType, inPlaceReconfigTrue), installing, joined)

			reconcile(2)

			got := load("dpu-joined")
			Expect(ptr.Deref(got.Status.DPUReconfig.InProgress, false)).To(BeTrue())
			Expect(got.Status.DPUReconfig.Round).To(Equal(int32(1)))
		},
		Entry("Host Network", provisioningv1.DPUHostNetworkConfiguration),
		Entry("DPU Cluster Config", provisioningv1.DPUClusterConfig),
		Entry("Service Readiness", provisioningv1.DPUServiceReadiness),
		Entry("Node Effect Removal", provisioningv1.DPUNodeEffectRemoval),
		Entry("Node Effect", provisioningv1.DPUNodeEffect),
	)

	DescribeTable("does not mark install, DPU Config, or Rebooting",
		func(phase provisioningv1.DPUPhase) {
			dpu := newDPU("dpu-a")
			dpu.Status.Phase = phase
			setup(1, newSet(provisioningv1.RollingUpdateStrategyType, inPlaceReconfigTrue), dpu)

			reconcile(1)

			Expect(load("dpu-a").Status.DPUReconfig).To(BeNil())
			Expect(dpuSet.Annotations).To(BeEmpty())
			Expect(ptr.Deref(dpuSet.Status.DPUReconfig.Active, false)).To(BeFalse())
			Expect(dpuSet.Status.DPUReconfig.Round).To(Equal(int32(1)))
		},
		Entry("DPU Config", provisioningv1.DPUConfig),
		Entry("Rebooting", provisioningv1.DPURebooting),
		Entry("OS Installing", provisioningv1.DPUOSInstalling),
	)

	It("ignores an annotation value other than true", func() {
		dpu := newDPU("dpu-a")
		markReady(dpu)
		setup(1, newSet(provisioningv1.RollingUpdateStrategyType, "yes"), dpu)

		reconcile(1)

		Expect(dpuSet.Status.DPUReconfig).To(BeNil())
		Expect(dpuSet.Annotations).To(HaveKeyWithValue(cutil.DPUSetInPlaceReconfigAnnotation, "yes"))
		Expect(load("dpu-a").Status.DPUReconfig).To(BeNil())
	})

	It("does not mark an old agent and does not let it block cleanup", func() {
		old := newDPU("dpu-old")
		withVersion(old, "")
		markReady(old)
		done := newDPU("dpu-done")
		markReady(done)
		done.Status.DPUReconfig = &provisioningv1.DPUReconfigStatus{CompletedRound: 1}
		set := newSet(provisioningv1.RollingUpdateStrategyType, inPlaceReconfigTrue)
		set.Status.DPUReconfig = &provisioningv1.DPUSetReconfigStatus{
			Round:     1,
			Active:    ptr.To(true),
			StartedAt: ptr.To(metav1.Now()),
		}
		setup(1, set, old, done)

		reconcile(2)

		Expect(load("dpu-old").Status.DPUReconfig).To(BeNil())
		Expect(load("dpu-done").Status.DPUReconfig.CompletedRound).To(Equal(int32(1)))
		Expect(load("dpu-done").Status.DPUReconfig.InProgress).To(BeNil())
		Expect(dpuSet.Annotations).To(BeEmpty())
		Expect(ptr.Deref(dpuSet.Status.DPUReconfig.Active, true)).To(BeFalse())
		Expect(dpuSet.Status.DPUReconfig.Round).To(Equal(int32(1)))
	})

	It("stamps the first Ready during the round without jumping", func() {
		dpu := newDPU("dpu-a")
		readyAt(dpu, time.Now().Add(time.Hour))
		setup(1, newSet(provisioningv1.RollingUpdateStrategyType, inPlaceReconfigTrue), dpu)

		reconcile(1)

		got := load("dpu-a")
		Expect(got.Status.DPUReconfig.InProgress).To(BeNil())
		Expect(got.Status.DPUReconfig.CompletedRound).To(Equal(int32(1)))
		Expect(dpuSet.Annotations).To(BeEmpty())
		Expect(ptr.Deref(dpuSet.Status.DPUReconfig.Active, true)).To(BeFalse())
	})

	It("stamps a DPU created after the round started when it first becomes Ready", func() {
		started := metav1.NewTime(time.Now().Add(-time.Hour))
		dpu := newDPU("dpu-a")
		dpu.CreationTimestamp = metav1.NewTime(time.Now())
		readyAt(dpu, time.Now())
		set := newSet(provisioningv1.RollingUpdateStrategyType, inPlaceReconfigTrue)
		set.Status.DPUReconfig = &provisioningv1.DPUSetReconfigStatus{
			Round:     2,
			Active:    ptr.To(true),
			StartedAt: &started,
		}
		setup(1, set, dpu)

		reconcile(1)

		got := load("dpu-a")
		Expect(got.Status.DPUReconfig.InProgress).To(BeNil())
		Expect(got.Status.DPUReconfig.CompletedRound).To(Equal(int32(2)))
	})

	It("clears inProgress when a marked DPU is Ready again", func() {
		started := metav1.NewTime(time.Now().Add(-time.Hour))
		dpu := newDPU("dpu-a")
		readyAt(dpu, time.Now())
		dpu.Status.DPUReconfig = &provisioningv1.DPUReconfigStatus{
			InProgress: ptr.To(true),
			Round:      1,
		}
		set := newSet(provisioningv1.RollingUpdateStrategyType, inPlaceReconfigTrue)
		set.Status.DPUReconfig = &provisioningv1.DPUSetReconfigStatus{
			Round:     1,
			Active:    ptr.To(true),
			StartedAt: &started,
		}
		setup(1, set, dpu)

		reconcile(1)

		got := load("dpu-a")
		Expect(ptr.Deref(got.Status.DPUReconfig.InProgress, true)).To(BeFalse())
		Expect(got.Status.DPUReconfig.CompletedRound).To(Equal(int32(1)))
		Expect(dpuSet.Annotations).To(BeEmpty())
		Expect(ptr.Deref(dpuSet.Status.DPUReconfig.Active, true)).To(BeFalse())
	})

	It("keeps inProgress while the original Ready condition is unchanged", func() {
		started := metav1.Now()
		dpu := newDPU("dpu-a")
		readyAt(dpu, time.Now().Add(-time.Hour))
		dpu.Status.DPUReconfig = &provisioningv1.DPUReconfigStatus{
			InProgress: ptr.To(true),
			Round:      1,
		}
		set := newSet(provisioningv1.RollingUpdateStrategyType, inPlaceReconfigTrue)
		set.Status.DPUReconfig = &provisioningv1.DPUSetReconfigStatus{
			Round:     1,
			Active:    ptr.To(true),
			StartedAt: &started,
		}
		setup(1, set, dpu)

		reconcile(1)

		got := load("dpu-a")
		Expect(ptr.Deref(got.Status.DPUReconfig.InProgress, false)).To(BeTrue())
		Expect(got.Status.DPUReconfig.CompletedRound).To(Equal(int32(0)))
		Expect(dpuSet.Annotations).To(HaveKey(cutil.DPUSetInPlaceReconfigAnnotation))
		Expect(ptr.Deref(dpuSet.Status.DPUReconfig.Active, false)).To(BeTrue())
	})

	It("does not start a DPU that already finished this round", func() {
		dpu := newDPU("dpu-a")
		markReady(dpu)
		dpu.Status.DPUReconfig = &provisioningv1.DPUReconfigStatus{CompletedRound: 3}
		set := newSet(provisioningv1.RollingUpdateStrategyType, inPlaceReconfigTrue)
		set.Status.DPUReconfig = &provisioningv1.DPUSetReconfigStatus{
			Round:     3,
			Active:    ptr.To(true),
			StartedAt: ptr.To(metav1.Now()),
		}
		setup(1, set, dpu)

		reconcile(1)

		got := load("dpu-a")
		Expect(got.Status.DPUReconfig.InProgress).To(BeNil())
		Expect(got.Status.DPUReconfig.CompletedRound).To(Equal(int32(3)))
		Expect(dpuSet.Annotations).To(BeEmpty())
		Expect(dpuSet.Status.DPUReconfig.Round).To(Equal(int32(3)))
	})

	It("clears inProgress when disruptive drift wins", func() {
		dpu := newDPU("dpu-a")
		markReady(dpu)
		dpu.Status.DPUReconfig = &provisioningv1.DPUReconfigStatus{
			InProgress:     ptr.To(true),
			Round:          1,
			CompletedRound: 0,
		}
		dpu.Status.Outdated = &provisioningv1.DPUOutdated{
			TimeStamp: metav1.Now(),
			Reason:    "OutdatedDPUFlavor",
			Message:   "flavor changed",
		}
		set := newSet(provisioningv1.RollingUpdateStrategyType, inPlaceReconfigTrue)
		set.Status.DPUReconfig = &provisioningv1.DPUSetReconfigStatus{
			Round:     1,
			Active:    ptr.To(true),
			StartedAt: ptr.To(metav1.Now()),
		}
		setup(1, set, dpu)

		reconcile(1)

		got := load("dpu-a")
		Expect(ptr.Deref(got.Status.DPUReconfig.InProgress, true)).To(BeFalse())
		Expect(got.Status.DPUReconfig.CompletedRound).To(Equal(int32(0)))
		Expect(dpuSet.Annotations).To(BeEmpty())
		Expect(ptr.Deref(dpuSet.Status.DPUReconfig.Active, true)).To(BeFalse())
	})

	It("stops starting DPUs when the annotation is removed and closes the round after in-flight work", func() {
		started := metav1.NewTime(time.Now().Add(-time.Hour))
		inFlight := newDPU("dpu-inflight")
		markReady(inFlight)
		inFlight.Status.DPUReconfig = &provisioningv1.DPUReconfigStatus{InProgress: ptr.To(true), Round: 4}
		waiting := newDPU("dpu-waiting")
		markReady(waiting)
		set := newSet(provisioningv1.OnDeleteStrategyType, "")
		set.Status.DPUReconfig = &provisioningv1.DPUSetReconfigStatus{
			Round:     4,
			Active:    ptr.To(true),
			StartedAt: &started,
		}
		setup(2, set, inFlight, waiting)

		reconcile(2)

		Expect(dpuSet.Status.DPUReconfig.Round).To(Equal(int32(4)))
		Expect(ptr.Deref(load("dpu-inflight").Status.DPUReconfig.InProgress, false)).To(BeTrue())
		Expect(load("dpu-waiting").Status.DPUReconfig).To(BeNil())
		Expect(ptr.Deref(dpuSet.Status.DPUReconfig.Active, false)).To(BeTrue())

		finished := load("dpu-inflight")
		base := finished.DeepCopy()
		readyAt(finished, time.Now())
		Expect(cl.Status().Patch(ctx, finished, client.MergeFrom(base))).To(Succeed())

		reconcile(2)

		Expect(dpuSet.Status.DPUReconfig.Round).To(Equal(int32(4)))
		Expect(ptr.Deref(dpuSet.Status.DPUReconfig.Active, true)).To(BeFalse())
		Expect(load("dpu-waiting").Status.DPUReconfig).To(BeNil())
		got := load("dpu-inflight")
		Expect(ptr.Deref(got.Status.DPUReconfig.InProgress, true)).To(BeFalse())
		Expect(got.Status.DPUReconfig.CompletedRound).To(Equal(int32(4)))
	})

	It("leaves a non-true annotation in place when an active round has nothing left in flight", func() {
		set := newSet(provisioningv1.RollingUpdateStrategyType, "false")
		set.Status.DPUReconfig = &provisioningv1.DPUSetReconfigStatus{
			Round:     2,
			Active:    ptr.To(true),
			StartedAt: ptr.To(metav1.Now()),
		}
		setup(1, set)

		reconcile(0)

		Expect(dpuSet.Annotations).To(HaveKeyWithValue(cutil.DPUSetInPlaceReconfigAnnotation, "false"))
		Expect(ptr.Deref(dpuSet.Status.DPUReconfig.Active, true)).To(BeFalse())
		Expect(dpuSet.Status.DPUReconfig.Round).To(Equal(int32(2)))
	})

	It("bumps the round when the annotation is set again", func() {
		dpu := newDPU("dpu-a")
		markReady(dpu)
		dpu.Status.DPUReconfig = &provisioningv1.DPUReconfigStatus{CompletedRound: 1}
		set := newSet(provisioningv1.RollingUpdateStrategyType, inPlaceReconfigTrue)
		set.Status.DPUReconfig = &provisioningv1.DPUSetReconfigStatus{
			Round:     1,
			Active:    ptr.To(false),
			StartedAt: ptr.To(metav1.NewTime(time.Now().Add(-time.Hour))),
		}
		setup(1, set, dpu)

		reconcile(1)

		Expect(dpuSet.Status.DPUReconfig.Round).To(Equal(int32(2)))
		Expect(ptr.Deref(dpuSet.Status.DPUReconfig.Active, false)).To(BeTrue())
		Expect(ptr.Deref(load("dpu-a").Status.DPUReconfig.InProgress, false)).To(BeTrue())
		Expect(load("dpu-a").Status.DPUReconfig.CompletedRound).To(Equal(int32(1)))
	})
})
