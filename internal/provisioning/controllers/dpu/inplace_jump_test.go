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
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// TestPatchInPlaceJumpWritesPhaseAndConditionsInOnePatch checks that the jump
// sends one status PATCH that stores Node Effect, Ready=False and
// DPUReconfiguring=True for the round together.
func TestPatchInPlaceJumpWritesPhaseAndConditionsInOnePatch(t *testing.T) {
	ctx := context.Background()
	g := NewWithT(t)
	c, patches := inPlaceJumpClient(g, readyInPlaceDPU())

	dpu := &provisioningv1.DPU{}
	g.Expect(c.Get(ctx, client.ObjectKeyFromObject(readyInPlaceDPU()), dpu)).To(Succeed())
	base := dpu.DeepCopy()
	jumped, ok := state.StartInPlaceReconfig(dpu)
	g.Expect(ok).To(BeTrue())
	UpdateDPUStatus(dpu, jumped)

	r := &DPUReconciler{ctrlCtx: &util.ControllerContext{Client: c}}
	g.Expect(r.patchInPlaceJump(ctx, base, dpu)).To(Succeed())

	g.Expect(*patches).To(Equal(1))
	stored := &provisioningv1.DPU{}
	g.Expect(c.Get(ctx, client.ObjectKeyFromObject(dpu), stored)).To(Succeed())
	g.Expect(stored.Status.Phase).To(Equal(provisioningv1.DPUNodeEffect))
	g.Expect(stored.Status.PreviousPhase).To(Equal(provisioningv1.DPUReady))
	_, ready := cutil.GetDPUCondition(&stored.Status, provisioningv1.DPUCondReady.String())
	g.Expect(ready).NotTo(BeNil())
	g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
	_, reconfiguring := cutil.GetDPUCondition(&stored.Status, provisioningv1.DPUCondReconfiguring.String())
	g.Expect(reconfiguring).NotTo(BeNil())
	g.Expect(reconfiguring.Status).To(Equal(metav1.ConditionTrue))
	g.Expect(reconfiguring.Message).To(Equal("3"))
}

// TestPatchInPlaceJumpConflictKeepsTheReadStatus checks that a jump based on a
// stale DPU fails with a conflict, leaves the stored DPU in Ready, and resets the
// in-memory status to the one that was read.
func TestPatchInPlaceJumpConflictKeepsTheReadStatus(t *testing.T) {
	ctx := context.Background()
	g := NewWithT(t)
	c, _ := inPlaceJumpClient(g, readyInPlaceDPU())

	dpu := &provisioningv1.DPU{}
	g.Expect(c.Get(ctx, client.ObjectKeyFromObject(readyInPlaceDPU()), dpu)).To(Succeed())
	base := dpu.DeepCopy()

	newer := dpu.DeepCopy()
	newer.Status.ObservedGeneration = 7
	g.Expect(c.Status().Patch(ctx, newer, client.MergeFrom(base))).To(Succeed())

	jumped, ok := state.StartInPlaceReconfig(dpu)
	g.Expect(ok).To(BeTrue())
	UpdateDPUStatus(dpu, jumped)

	r := &DPUReconciler{ctrlCtx: &util.ControllerContext{Client: c}}
	err := r.patchInPlaceJump(ctx, base, dpu)
	g.Expect(apierrors.IsConflict(err)).To(BeTrue())
	g.Expect(dpu.Status).To(Equal(base.Status))

	stored := &provisioningv1.DPU{}
	g.Expect(c.Get(ctx, client.ObjectKeyFromObject(dpu), stored)).To(Succeed())
	g.Expect(stored.Status.Phase).To(Equal(provisioningv1.DPUReady))
}

// inPlaceJumpClient returns a fake client holding dpu and a counter of the
// status PATCHes sent through it.
func inPlaceJumpClient(g *WithT, dpu *provisioningv1.DPU) (client.Client, *int) {
	scheme := runtime.NewScheme()
	g.Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
	patches := 0
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(dpu).
		WithStatusSubresource(&provisioningv1.DPU{}).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourcePatch: func(ctx context.Context, c client.Client, subResourceName string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
				patches++
				return c.SubResource(subResourceName).Patch(ctx, obj, patch, opts...)
			},
		}).
		Build()
	return c, &patches
}

// readyInPlaceDPU returns a Ready DPU that DPUSet has marked for round 3.
func readyInPlaceDPU() *provisioningv1.DPU {
	return &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "worker1",
			Namespace: "dpf-operator-system",
		},
		Status: provisioningv1.DPUStatus{
			Phase: provisioningv1.DPUReady,
			Conditions: []metav1.Condition{
				{
					Type:               provisioningv1.DPUCondReady.String(),
					Status:             metav1.ConditionTrue,
					Reason:             "DPUReady",
					LastTransitionTime: metav1.Now(),
				},
			},
			DPUReconfig: &provisioningv1.DPUReconfigStatus{
				InProgress: ptr.To(true),
				Round:      3,
			},
		},
	}
}
