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

package controllers

import (
	"context"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var _ = Describe("Indexers", Ordered, func() {
	var (
		ctx             context.Context
		cancel          context.CancelFunc
		managerStopCh   chan struct{}
		indexersManager ctrl.Manager
		indexersClient  client.Client
	)
	BeforeAll(func() {
		ctx, cancel = context.WithCancel(testCtx)

		By("starting manager with indexers")
		var err error
		indexersManager, err = ctrl.NewManager(cfg, ctrl.Options{
			Controller: config.Controller{
				SkipNameValidation: ptr.To(true),
			},
			Scheme:  scheme.Scheme,
			Metrics: server.Options{BindAddress: "0"},
		})
		Expect(err).ToNot(HaveOccurred())

		Expect(SetupIndexers(ctx, indexersManager)).To(Succeed())

		indexersClient = indexersManager.GetClient()

		managerStopCh = make(chan struct{})
		go func() {
			defer GinkgoRecover()
			defer close(managerStopCh)
			Expect(indexersManager.Start(ctx)).To(Succeed())
		}()
	})
	AfterAll(func() {
		cancel()
		Eventually(managerStopCh).WithTimeout(10 * time.Second).Should(BeClosed())
	})
	It("should index DPU by spec.dpuNodeName", func() {
		dpuNodeName := "test-idx-dpunode"
		dpu := &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{Name: "test-idx-dpu", Namespace: testNamespace},
			Spec: provisioningv1.DPUSpec{
				DPUNodeName:   dpuNodeName,
				SerialNumber:  "MT25066004C7",
				DPUDeviceName: "test-device",
				BFB:           ptr.To("test-bfb"),
				DPUFlavor:     "test-flavor",
				NodeEffect:    provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
			},
		}
		Expect(testClient.Create(ctx, dpu)).To(Succeed())
		Eventually(func(g Gomega) {
			var dpuList provisioningv1.DPUList
			g.Expect(indexersClient.List(ctx, &dpuList,
				client.InNamespace(testNamespace),
				client.MatchingFields{dpuNodeNameField: dpuNodeName})).To(Succeed())
			g.Expect(dpuList.Items).To(HaveLen(1))
			g.Expect(dpuList.Items[0].Name).To(Equal(dpu.Name))
		}, testTimeout, testInterval).Should(Succeed())
		Expect(testClient.Delete(ctx, dpu)).To(Succeed())
	})
	It("should index DPUNode by status.kubeNodeRef", func() {
		kubeNodeRef := "test-idx-kubenoderef"
		dpuNode := &provisioningv1.DPUNode{
			ObjectMeta: metav1.ObjectMeta{Name: "test-idx-dpunode", Namespace: testNamespace},
			Spec:       provisioningv1.DPUNodeSpec{},
		}
		Expect(testClient.Create(ctx, dpuNode)).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuNode), dpuNode)).To(Succeed())
			dpuNode.Status.KubeNodeRef = ptr.To(kubeNodeRef)
			g.Expect(testClient.Status().Update(ctx, dpuNode)).To(Succeed())
		}, testTimeout, testInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			var dpuNodeList provisioningv1.DPUNodeList
			g.Expect(indexersClient.List(ctx, &dpuNodeList,
				client.InNamespace(testNamespace),
				client.MatchingFields{dpuNodeKubeNodeRefField: kubeNodeRef})).To(Succeed())
			g.Expect(dpuNodeList.Items).To(HaveLen(1))
			g.Expect(dpuNodeList.Items[0].Name).To(Equal(dpuNode.Name))
		}, testTimeout, testInterval).Should(Succeed())
		Expect(testClient.Delete(ctx, dpuNode)).To(Succeed())
	})
	It("should index Pod by target node from affinity", func() {
		targetNode := "test-idx-target-node"
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-idx-pod",
				Namespace: testNamespace,
				Labels:    map[string]string{ManagedByLabelKey: ManagedByLabelValue},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "test", Image: "busybox"},
				},
				Affinity: &corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchFields: []corev1.NodeSelectorRequirement{
										{
											Key:      metav1.ObjectNameField,
											Operator: corev1.NodeSelectorOpIn,
											Values:   []string{targetNode},
										},
									},
								},
							},
						},
					},
				},
			},
		}
		Expect(testClient.Create(ctx, pod)).To(Succeed())
		Eventually(func(g Gomega) {
			var podList corev1.PodList
			g.Expect(indexersClient.List(ctx, &podList,
				client.InNamespace(testNamespace),
				client.MatchingFields{podTargetNodeField: targetNode})).To(Succeed())
			g.Expect(podList.Items).To(HaveLen(1))
			g.Expect(podList.Items[0].Name).To(Equal(pod.Name))
		}, testTimeout, testInterval).Should(Succeed())
		Expect(testClient.Delete(ctx, pod)).To(Succeed())
	})
})
