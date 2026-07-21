/*
Copyright 2025 NVIDIA

Licensed under the Apache License, Version 2.0 (the License);
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an AS IS BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//nolint:goconst
var _ = Describe("manager test", func() {

	var ctx = context.Background()
	var ns *corev1.Namespace
	var hostPod, otherHostPod *corev1.Pod

	BeforeEach(func() {
		hostPod = nil
		otherHostPod = nil
		ns = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-ns-",
			},
		}
		Expect(testClient.Create(ctx, ns)).To(Succeed())
	})

	AfterEach(func() {
		if hostPod != nil {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, hostPod))).To(Succeed())
		}
		if otherHostPod != nil {
			Expect(client.IgnoreNotFound(testClient.Delete(ctx, otherHostPod))).To(Succeed())
		}
		Expect(testClient.Delete(ctx, ns)).To(Succeed())
	})

	It("cahce only pods on the same node", func() {
		hostPod = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod",
				Namespace: ns.Name,
			},
			Spec: corev1.PodSpec{
				NodeName: testNodeName,
				Containers: []corev1.Container{
					{
						Name:  "test-container",
						Image: "test-image",
					},
				},
			},
		}
		Expect(testClient.Create(ctx, hostPod)).To(Succeed())

		otherHostPod = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "other-test-pod",
				Namespace: ns.Name,
			},
			Spec: corev1.PodSpec{
				NodeName: "other-node",
				Containers: []corev1.Container{
					{
						Name:  "test-container",
						Image: "test-image",
					},
				},
			},
		}
		Expect(testClient.Create(ctx, otherHostPod)).To(Succeed())

		listPods := &corev1.PodList{}
		Expect(mgrClient.List(ctx, listPods)).To(Succeed())
		Expect(listPods.Items).To(HaveLen(1))
		Expect(listPods.Items[0].Name).To(Equal(hostPod.Name))

		By("pod is cahced and expected to be found")
		err := mgrClient.Get(ctx, types.NamespacedName{Name: hostPod.Name, Namespace: ns.Name}, &corev1.Pod{})
		Expect(err).To(Succeed())

		By("pod is not cached and expected to be not found")
		err = mgrClient.Get(ctx, types.NamespacedName{Name: otherHostPod.Name, Namespace: ns.Name}, &corev1.Pod{})
		Expect(err).To(HaveOccurred())
	})

	It("leaves SyncPeriod unset so the controller-runtime default resync applies", func() {
		options := GetMgrCache(testNodeName)
		Expect(options.SyncPeriod).To(BeNil())
	})
})
