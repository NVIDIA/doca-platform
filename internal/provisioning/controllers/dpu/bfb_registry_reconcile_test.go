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
	"time"

	"github.com/nvidia/doca-platform/internal/provisioning/bfbregistry"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestReconcileRequeuesForRecentlyFailedBFBRegistryPod checks that a recently failed
// bfb-registry Pod is kept and the reconcile asks to retry once its backoff has passed.
func TestReconcileRequeuesForRecentlyFailedBFBRegistryPod(t *testing.T) {
	g := NewWithT(t)
	const ns = "dpf-operator-system"
	t.Setenv("POD_NAME", "leader-pod")
	t.Setenv("NODE_NAME", "node-1")
	t.Setenv("NODE_IP", "10.0.0.1")
	t.Setenv("BFB_REGISTRY_IMAGE", "registry:test")

	leader := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "leader-pod",
			Namespace: ns,
			UID:       "leader-uid",
		},
	}
	failed := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bfbregistry.PodName,
			Namespace: ns,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "v1",
				Kind:       "Pod",
				Name:       leader.Name,
				UID:        leader.UID,
				Controller: ptr.To(true),
			}},
			CreationTimestamp: metav1.Time{Time: time.Now().Add(-5 * time.Second)},
		},
		Status: corev1.PodStatus{Phase: corev1.PodFailed},
	}
	s := runtime.NewScheme()
	g.Expect(corev1.AddToScheme(s)).To(Succeed())
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(leader, failed).Build()
	r := &DPUReconciler{ctrlCtx: &util.ControllerContext{Client: c, Scheme: s}}

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: ns, Name: bfbregistry.PodName},
	})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result.RequeueAfter).To(BeNumerically(">", 0))
	g.Expect(result.RequeueAfter).To(BeNumerically("<=", 30*time.Second))

	got := &corev1.Pod{}
	g.Expect(c.Get(context.Background(), client.ObjectKey{Namespace: ns, Name: bfbregistry.PodName}, got)).To(Succeed())
	g.Expect(got.Status.Phase).To(Equal(corev1.PodFailed))
}
