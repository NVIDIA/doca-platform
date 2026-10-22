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
	"github.com/nvidia/doca-platform/internal/utils/tracing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
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

	It("does not re-add the finalizer while the object is being deleted", func() {
		now := metav1.Now()
		obj := &provisioningv1.DPUNodeMaintenance{
			ObjectMeta: metav1.ObjectMeta{
				Name:              objName,
				Namespace:         namespace,
				DeletionTimestamp: &now,
				// A placeholder finalizer keeps the object in the fake API after the protection
				// finalizer has been stripped, as the operator does on DPFOperatorConfig teardown.
				Finalizers: []string{"test.nvidia.com/keep"},
			},
			Spec: provisioningv1.DPUNodeMaintenanceSpec{
				DPUNodeName: dpuNodeName,
				NodeEffect: &provisioningv1.NodeEffect{
					Action: provisioningv1.Action{
						NoEffect: ptr.To(true),
					},
				},
			},
		}
		reconciler, fakeClient := newReconciler(obj)

		_, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: client.ObjectKeyFromObject(obj),
		})
		Expect(err).NotTo(HaveOccurred())

		got := &provisioningv1.DPUNodeMaintenance{}
		Expect(fakeClient.Get(ctx, client.ObjectKeyFromObject(obj), got)).To(Succeed())
		Expect(got.Finalizers).NotTo(ContainElement(provisioningv1.DPUNodeMaintenanceFinalizer))
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

var _ = Describe("Node writes continue the DPUNodeMaintenance trace", func() {
	const maintenanceTraceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	taint := corev1.Taint{Key: "dpu-maintenance", Effect: corev1.TaintEffectNoSchedule}
	var (
		ctx         context.Context
		k8sClient   client.Client
		maintenance *provisioningv1.DPUNodeMaintenance
	)

	// storedNode creates node in the fake client and returns it as read back, so later writes have
	// a resourceVersion to work with.
	storedNode := func(node *corev1.Node) *corev1.Node {
		Expect(k8sClient.Create(context.Background(), node)).To(Succeed())
		got := &corev1.Node{}
		Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(node), got)).To(Succeed())
		return got
	}
	// nodeTraceparent returns the generated traceparent annotation on the stored Node named name.
	nodeTraceparent := func(name string) string {
		got := &corev1.Node{}
		Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: name}, got)).To(Succeed())
		return got.Annotations[tracing.GeneratedTraceParentAnnotation]
	}

	BeforeEach(func() {
		DeferCleanup(tracing.EnableForTest())
		scheme := runtime.NewScheme()
		Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		k8sClient = fake.NewClientBuilder().WithScheme(scheme).Build()
		maintenance = &provisioningv1.DPUNodeMaintenance{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "maintenance",
				Namespace:   "ns",
				UID:         "maintenance-uid",
				Annotations: map[string]string{tracing.TraceParentAnnotation: maintenanceTraceparent},
			},
			Spec: provisioningv1.DPUNodeMaintenanceSpec{
				DPUNodeName: "dpu-node",
				NodeEffect:  &provisioningv1.NodeEffect{Action: provisioningv1.Action{Taint: &taint}},
			},
		}
		var endSpan func(*error)
		ctx, endSpan = tracing.ReconcileSpan(context.Background(), DPUNodeMaintenanceControllerName, maintenance)
		endSpan(nil)
	})

	It("stamps the Node when it adds the taint", func() {
		node := storedNode(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node"}})
		r := &DPUNodeMaintenanceReconciler{Client: k8sClient}

		Expect(r.reconcileTaint(ctx, maintenance, node)).To(Succeed())

		Expect(nodeTraceparent("node")).To(Equal(maintenanceTraceparent))
	})

	It("stamps the Node when it removes the taint", func() {
		node := storedNode(&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "node"},
			Spec:       corev1.NodeSpec{Taints: []corev1.Taint{taint}},
		})

		Expect(removeNodeEffectTaint(ctx, k8sClient, node, maintenance.Spec.NodeEffect)).To(Succeed())

		Expect(nodeTraceparent("node")).To(Equal(maintenanceTraceparent))
	})

	It("does not stamp the Node when there is no taint to remove", func() {
		node := storedNode(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node"}})

		Expect(removeNodeEffectTaint(ctx, k8sClient, node, maintenance.Spec.NodeEffect)).To(Succeed())

		Expect(nodeTraceparent("node")).To(BeEmpty())
	})

	DescribeTable("removing custom labels stamps the Node and DPUNode only when a label is removed",
		func(nodeLabels map[string]string, wantTraceparent string) {
			node := storedNode(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node", Labels: nodeLabels}})
			nodeEffect := &provisioningv1.NodeEffect{Action: provisioningv1.Action{CustomLabel: map[string]string{"maintenance": "true"}}}
			dpuNode := &provisioningv1.DPUNode{ObjectMeta: metav1.ObjectMeta{Name: "dpu-node", Namespace: "ns", Labels: nodeLabels}}
			Expect(k8sClient.Create(context.Background(), dpuNode)).To(Succeed())

			Expect(removeNodeEffectCustomLabel(ctx, k8sClient, node, dpuNode, nodeEffect)).To(Succeed())

			Expect(nodeTraceparent("node")).To(Equal(wantTraceparent))
			gotDPUNode := &provisioningv1.DPUNode{}
			Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(dpuNode), gotDPUNode)).To(Succeed())
			Expect(gotDPUNode.Annotations[tracing.GeneratedTraceParentAnnotation]).To(Equal(wantTraceparent))
		},
		Entry("label present", map[string]string{"maintenance": "true"}, maintenanceTraceparent),
		Entry("label absent", map[string]string{"other": "kept"}, ""),
	)
})
