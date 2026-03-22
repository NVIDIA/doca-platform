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

package bfbregistry

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestBFBRegistryCreator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "BFB Registry Creator Suite")
}

var _ = Describe("EnsureBFBRegistry", func() {
	const testNamespace = "bfb-registry-test"

	var (
		ctx    context.Context
		scheme *runtime.Scheme
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
	})

	It("returns error when leader pod does not exist", func() {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		err := EnsureBFBRegistry(ctx, c, testNamespace, "leader-pod", "node-1", "img", "", nil)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("get leader pod"))
	})

	It("creates bfb-registry pod and service when both not exist", func() {
		leaderPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "leader-pod",
				Namespace: testNamespace,
				UID:       "leader-uid",
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(leaderPod).Build()

		err := EnsureBFBRegistry(ctx, c, testNamespace, "leader-pod", "node-1", "registry:8082", "", nil)
		Expect(err).NotTo(HaveOccurred())

		pod := &corev1.Pod{}
		Expect(c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: PodName}, pod)).To(Succeed())
		Expect(pod.Spec.NodeName).To(Equal("node-1"))
		Expect(pod.Labels[LabelDPUComponent]).To(Equal(LabelValue))

		svc := &corev1.Service{}
		Expect(c.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: PodName}, svc)).To(Succeed())
		Expect(svc.Spec.Type).To(Equal(corev1.ServiceTypeNodePort))
		Expect(svc.Spec.Ports).To(HaveLen(1))
	})

	It("succeeds when pod and service already exist (no duplicate create)", func() {
		leaderPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "leader-pod",
				Namespace: testNamespace,
				UID:       "leader-uid",
			},
		}
		existingPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      PodName,
				Namespace: testNamespace,
			},
			Spec: corev1.PodSpec{NodeName: "node-1"},
		}
		existingSvc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      PodName,
				Namespace: testNamespace,
			},
			Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(leaderPod, existingPod, existingSvc).
			Build()

		err := EnsureBFBRegistry(ctx, c, testNamespace, "leader-pod", "node-1", "registry:8082", "", nil)
		Expect(err).NotTo(HaveOccurred())

		// Should still have exactly one pod and one service (no duplicates)
		podList := &corev1.PodList{}
		Expect(c.List(ctx, podList, client.InNamespace(testNamespace))).To(Succeed())
		Expect(podList.Items).To(HaveLen(2)) // leader + bfb-registry
		svcList := &corev1.ServiceList{}
		Expect(c.List(ctx, svcList, client.InNamespace(testNamespace))).To(Succeed())
		Expect(svcList.Items).To(HaveLen(1))
	})
})
