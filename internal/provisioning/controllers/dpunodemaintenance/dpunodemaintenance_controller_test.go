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

package dpunodemaintenance

import (
	"context"
	"testing"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestDPUNodeMaintenanceController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "DPUNodeMaintenance Controller Suite")
}

var _ = Describe("DPUNodeMaintenanceReconciler", func() {
	const (
		namespace   = "dpf-operator-system"
		dpuNodeName = "dpu-node-mt2403xz04tt"
		objName     = "dpu-node-mt2403xz04tt-noeffect"
		requestor   = "dpu-node-mt2403xz04tt-mt2403xz04tt"
	)

	var (
		ctx context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
	})

	orphanedNoEffectDPUNodeMaintenance := func() *provisioningv1.DPUNodeMaintenance {
		now := metav1.Now()
		return &provisioningv1.DPUNodeMaintenance{
			ObjectMeta: metav1.ObjectMeta{
				Name:              objName,
				Namespace:         namespace,
				DeletionTimestamp: &now,
				Finalizers:        []string{provisioningv1.DPUNodeMaintenanceFinalizer},
				Generation:        2,
			},
			Spec: provisioningv1.DPUNodeMaintenanceSpec{
				DPUNodeName: dpuNodeName,
				NodeEffect: &provisioningv1.NodeEffect{
					Action: provisioningv1.Action{
						NoEffect: ptr.To(true),
					},
				},
				Requestor: []string{requestor},
			},
			Status: provisioningv1.DPUNodeMaintenanceStatus{
				Conditions: []metav1.Condition{
					{
						Type:               string(provisioningv1.ConditionNodeEffectApplied),
						Status:             metav1.ConditionTrue,
						Reason:             "Success",
						ObservedGeneration: 1,
						LastTransitionTime: now,
					},
				},
				MaxUnavailableDPUNodes:         ptr.To(int32(50)),
				MultiDPUOperationsSyncWaitTime: &metav1.Duration{Duration: 30 * time.Second},
			},
		}
	}

	newReconciler := func(objects ...client.Object) (*DPUNodeMaintenanceReconciler, client.Client) {
		scheme := runtime.NewScheme()
		Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(objects...).
			WithStatusSubresource(&provisioningv1.DPUNodeMaintenance{}).
			Build()

		return &DPUNodeMaintenanceReconciler{
			Client: fakeClient,
			Options: DPUNodeMaintenanceOptions{
				MultiDPUOperationsSyncWaitTime: 30 * time.Second,
				MaxUnavailableDPUNodes:         50,
			},
		}, fakeClient
	}

	It("finalizes orphaned NoEffect DPUNodeMaintenance when DPUNode was deleted out-of-band", func() {
		obj := orphanedNoEffectDPUNodeMaintenance()
		reconciler, fakeClient := newReconciler(obj)

		_, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: client.ObjectKeyFromObject(obj),
		})
		Expect(err).NotTo(HaveOccurred())

		got := &provisioningv1.DPUNodeMaintenance{}
		err = fakeClient.Get(ctx, client.ObjectKeyFromObject(obj), got)
		if apierrors.IsNotFound(err) {
			// Object was finalized and removed from the API.
		} else {
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Finalizers).To(BeEmpty())
			Expect(got.Spec.Requestor).To(BeEmpty())
		}
	})

	It("keeps finalizer when requestor is set and DPUNode still exists", func() {
		obj := orphanedNoEffectDPUNodeMaintenance()
		dpuNode := &provisioningv1.DPUNode{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dpuNodeName,
				Namespace: namespace,
			},
		}
		reconciler, fakeClient := newReconciler(obj, dpuNode)

		_, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: client.ObjectKeyFromObject(obj),
		})
		Expect(err).NotTo(HaveOccurred())

		got := &provisioningv1.DPUNodeMaintenance{}
		Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(obj), got)).To(Succeed())
		Expect(got.Finalizers).To(ContainElement(provisioningv1.DPUNodeMaintenanceFinalizer))
		Expect(got.Spec.Requestor).To(Equal([]string{requestor}))
	})
})
