/*
Copyright 2025 NVIDIA

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
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dpucluster "github.com/nvidia/doca-platform/pkg/dpucluster"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func markServiceCritical(dpuService *dpuservicev1.DPUService) {
	if dpuService.ObjectMeta.Labels == nil {
		dpuService.ObjectMeta.Labels = make(map[string]string)
	}
	dpuService.ObjectMeta.Labels[criticalDPUServiceLabel] = ""
	dpuService.Spec.Interfaces = nil
}

const (
	nodeName      string = "dpuready-test-node"
	dpuDeviceName string = nodeName + "0000-ca-00" //required field for DPU
	dpuName       string = "test-dpu"
)

var _ = Describe("DPUReadyController", func() {
	var (
		workerNode       *corev1.Node
		testNS           *corev1.Namespace
		dpu              *provisioningv1.DPU
		dpuCluster       provisioningv1.DPUCluster
		dpuClusterClient client.Client
	)

	defaultPauseDPUServiceReconciler := pauseDPUServiceReconciler
	BeforeEach(func() {
		By("Pausing other controllers that are not relevant for these tests")
		DeferCleanup(func() {
			pauseDPUServiceReconciler = defaultPauseDPUServiceReconciler
		})
		// These are modified to speed up the testing suite and also simplify the deletion logic
		pauseDPUServiceReconciler = true

		By("Creating the namespace for the test")
		testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "dpuready-testns-"}}

		Expect(testClient.Create(ctx, testNS)).To(Succeed())
		DeferCleanup(testClient.Delete, ctx, testNS)

		By("Creating the DPUCluster")
		dpuCluster = testutils.GetTestDPUCluster(testNS.Name, "envtest-dpuready")
		kamajiSecret, err := testutils.GetFakeKamajiClusterSecretFromEnvtest(dpuCluster, cfg)
		Expect(err).NotTo(HaveOccurred())
		Expect(testClient.Create(ctx, kamajiSecret)).To(Succeed())
		Expect(testClient.Create(ctx, &dpuCluster)).To(Succeed())
		DeferCleanup(testutils.CleanupAndWait, ctx, testClient, kamajiSecret)
		DeferCleanup(testutils.CleanupAndWait, ctx, testClient, &dpuCluster)

		dpuClusterClient, err = dpucluster.NewConfig(testClient, &dpuCluster).Client(ctx)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a DPU")
		dpu = &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dpuName,
				Namespace: testNS.Name,
			},
			Spec: provisioningv1.DPUSpec{
				DPUNodeName:   nodeName,
				DPUDeviceName: dpuDeviceName,
				SerialNumber:  "MT25066004C7",
				Cluster: provisioningv1.K8sCluster{
					Name:      dpuCluster.Name,
					Namespace: dpuCluster.Namespace,
				},
			},
		}
		Expect(testClient.Create(ctx, dpu)).To(Succeed())
		DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpu)

		By("Creating a worker Node")
		workerNode = &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName,
				Labels: map[string]string{
					dpuEnabledLabelKey: dpuEnabledLabelValue,
				},
			},
		}
		Expect(testClient.Create(ctx, workerNode)).To(Succeed())
		DeferCleanup(testutils.CleanupAndWait, ctx, testClient, workerNode)

		By("Creating a Node in the DPUCluster")
		nodeInDPUCluster := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: dpu.Name,
			},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{
						Type:   corev1.NodeReady,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}
		Expect(dpuClusterClient.Create(ctx, nodeInDPUCluster)).To(Succeed())
		DeferCleanup(testutils.CleanupAndWait, ctx, dpuClusterClient, nodeInDPUCluster)
	})
	Context("Worker Node Taint Management", func() {

		It("should ignore non-critical services", func() {
			By("Creating non-critical DPUService")
			dpuServices := getMinimalDPUServices(testNS.Name)
			Expect(testClient.Create(ctx, dpuServices[1])).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServices[1])

			By("Triggering node reconcile")
			Eventually(func(g Gomega) {
				updatedNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, types.NamespacedName{Name: workerNode.Name}, updatedNode)).To(Succeed())
				g.Expect(testutils.ForceObjectReconcileWithAnnotation(ctx, testClient, updatedNode)).To(Succeed())
			}).WithTimeout(5 * time.Second).Should(Succeed())

			By("Verifying worker node is not tainted")
			Eventually(func(g Gomega) {
				updatedNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, types.NamespacedName{Name: workerNode.Name}, updatedNode)).To(Succeed())
				g.Expect(updatedNode.Spec.Taints).NotTo(ContainElement(corev1.Taint{
					Key:    taintKey,
					Effect: taintEffect,
				}))
			}).WithTimeout(5 * time.Second).Should(Succeed())
		})

		It("should ignore service with not matching nodeSelector", func() {
			dpuNonExistentLabelWorkerNode := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "dpu-nonexistent-label-worker-node",
					Labels: map[string]string{
						dpuEnabledLabelKey: dpuEnabledLabelValue,
					},
				},
			}
			Expect(testClient.Create(ctx, dpuNonExistentLabelWorkerNode)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuNonExistentLabelWorkerNode)
			// Create another DPU, spec.dpuNodeName is an immutate field can't use previous dpu
			dpu2 := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpu-2",
					Namespace: testNS.Name,
				},
				Spec: provisioningv1.DPUSpec{
					DPUNodeName:   dpuNonExistentLabelWorkerNode.Name,
					DPUDeviceName: dpuDeviceName,
					SerialNumber:  "MT25066004C7",
					Cluster: provisioningv1.K8sCluster{
						Name:      dpuCluster.Name,
						Namespace: dpuCluster.Namespace,
					},
				},
			}
			Expect(testClient.Create(ctx, dpu2)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpu2)

			dpuServices := getMinimalDPUServices(testNS.Name)
			// mark service as critical
			markServiceCritical(dpuServices[0])
			dpuServices[0].Spec.Interfaces = nil
			dpuServices[0].Spec.ServiceDaemonSet.NodeSelector = &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{
								Key:      "Nonexistent_Label",
								Operator: corev1.NodeSelectorOpExists,
							},
						},
					},
				},
			}
			Expect(testClient.Create(ctx, dpuServices[0])).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServices[0])
			By("Triggering node reconcile")
			Eventually(func(g Gomega) {
				updatedNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, types.NamespacedName{Name: "dpu-nonexistent-label-worker-node"}, updatedNode)).To(Succeed())
				g.Expect(testutils.ForceObjectReconcileWithAnnotation(ctx, testClient, updatedNode)).To(Succeed())
			}).WithTimeout(5 * time.Second).Should(Succeed())

			Consistently(func(g Gomega) {
				updatedNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, types.NamespacedName{Name: "dpu-nonexistent-label-worker-node"}, updatedNode)).To(Succeed())
				g.Expect(updatedNode.Spec.Taints).NotTo(ContainElement(corev1.Taint{
					Key:    taintKey,
					Effect: taintEffect,
				}))
			}).WithTimeout(5 * time.Second).Should(Succeed())
		})
		It("should taint worker node when critical DPU services are not running", func() {
			dpuServices := getMinimalDPUServices(testNS.Name)
			// mark service as critical
			markServiceCritical(dpuServices[1])
			Expect(testClient.Create(ctx, dpuServices[1])).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServices[1])
			By("Triggering node reconcile")
			Eventually(func(g Gomega) {
				updatedNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, types.NamespacedName{Name: workerNode.Name}, updatedNode)).To(Succeed())
				// trigger reconcile
				g.Expect(testutils.ForceObjectReconcileWithAnnotation(ctx, testClient, updatedNode)).To(Succeed())
			}).WithTimeout(5 * time.Second).Should(Succeed())

			By("Verifying taint is added on the node")
			Eventually(func(g Gomega) {
				updatedNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, types.NamespacedName{Name: workerNode.Name}, updatedNode)).To(Succeed())
				g.Expect(updatedNode.Spec.Taints).To(ContainElement(corev1.Taint{
					Key:    taintKey,
					Effect: taintEffect,
				}))
			}).WithTimeout(1 * time.Minute).Should(Succeed())
		})

		It("should remove taint from worker node when all critical DPU services are running", func() {
			dpuServices := getMinimalDPUServices(testNS.Name)
			// mark service as critical
			markServiceCritical(dpuServices[1])
			dpuServices[1].Spec.ServiceID = ptr.To("service-one")
			Expect(testClient.Create(ctx, dpuServices[1])).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServices[1])

			By("Triggering node reconcile")
			Eventually(func(g Gomega) {
				updatedNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, types.NamespacedName{Name: workerNode.Name}, updatedNode)).To(Succeed())
				// trigger reconcile
				g.Expect(testutils.ForceObjectReconcileWithAnnotation(ctx, testClient, updatedNode)).To(Succeed())
			}).WithTimeout(5 * time.Second).Should(Succeed())

			By("Verifying taint is added on the node")
			Eventually(func(g Gomega) {
				updatedNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, types.NamespacedName{Name: workerNode.Name}, updatedNode)).To(Succeed())
				g.Expect(updatedNode.Spec.Taints).To(ContainElement(corev1.Taint{
					Key:    taintKey,
					Effect: taintEffect,
				}))
			}).WithTimeout(1 * time.Minute).Should(Succeed())

			By("Creating pod corresponding to the critical service")
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dpuServices[1].Name + "-test-pod",
					Namespace: "default",
					Labels: map[string]string{
						dpuservicev1.DPFServiceIDLabelKey: *dpuServices[1].Spec.ServiceID,
					},
				},
				Spec: corev1.PodSpec{
					NodeName: dpuName,
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "nginx:latest", // Any valid image name
						},
					},
					// Required to avoid validation errors
					RestartPolicy: corev1.RestartPolicyAlways,
				},
			}
			Expect(dpuClusterClient.Create(ctx, pod)).To(Succeed())
			originalPod := pod.DeepCopy()
			pod.Status = corev1.PodStatus{
				Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{
					{
						Type:               corev1.PodReady,
						Status:             corev1.ConditionTrue,
						LastTransitionTime: metav1.Now(),
					},
					{
						Type:               corev1.PodInitialized,
						Status:             corev1.ConditionTrue,
						LastTransitionTime: metav1.Now(),
					},
					{
						Type:               corev1.ContainersReady,
						Status:             corev1.ConditionTrue,
						LastTransitionTime: metav1.Now(),
					},
					{
						Type:               corev1.PodScheduled,
						Status:             corev1.ConditionTrue,
						LastTransitionTime: metav1.Now(),
					},
				},
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name: "test-container",
						State: corev1.ContainerState{
							Running: &corev1.ContainerStateRunning{
								StartedAt: metav1.Now(),
							},
						},
						Ready:        true,
						RestartCount: 0,
					},
				},
				PodIP:     "10.0.0.1",
				HostIP:    "192.168.1.1",
				StartTime: &metav1.Time{Time: time.Now()},
			}

			Expect(dpuClusterClient.Status().Patch(ctx, pod, client.MergeFrom(originalPod))).To(Succeed())
			// Wait for pod to be running
			Eventually(func(g Gomega) {
				updatedPod := &corev1.Pod{}
				g.Expect(dpuClusterClient.Get(ctx, types.NamespacedName{
					Name:      pod.Name,
					Namespace: pod.Namespace,
				}, updatedPod)).To(Succeed())
				g.Expect(updatedPod.Status.Phase).To(Equal(corev1.PodRunning))
			}).WithTimeout(5 * time.Second).Should(Succeed())

			By("Triggering node reconcile")
			Eventually(func(g Gomega) {
				updatedNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, types.NamespacedName{Name: workerNode.Name}, updatedNode)).To(Succeed())
				g.Expect(testutils.ForceObjectReconcileWithAnnotation(ctx, testClient, updatedNode)).To(Succeed())
			}).WithTimeout(5 * time.Second).Should(Succeed())

			By("Verifying taint is removed from the node")
			Eventually(func(g Gomega) {
				updatedNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, types.NamespacedName{Name: workerNode.Name}, updatedNode)).To(Succeed())
				g.Expect(updatedNode.Spec.Taints).NotTo(ContainElement(corev1.Taint{
					Key:    taintKey,
					Effect: taintEffect,
				}))
			}).WithTimeout(5 * time.Second).Should(Succeed())

			// Force immediate deletion
			gracePeriod := int64(0)
			deleteOpts := &client.DeleteOptions{
				GracePeriodSeconds: &gracePeriod,
			}
			Expect(dpuClusterClient.Delete(ctx, pod, deleteOpts)).To(Succeed())
			// Check pod state after deletion
			Eventually(func(g Gomega) {
				podList := &corev1.PodList{}
				g.Expect(dpuClusterClient.List(ctx, podList)).To(Succeed())
				g.Expect(podList.Items).To(BeEmpty())
			}).WithTimeout(30 * time.Second).Should(Succeed())
		})

		It("should add taint to worker node when one of the container in the critical service pod is not running", func() {
			dpuServices := getMinimalDPUServices(testNS.Name)
			// mark service as critical
			markServiceCritical(dpuServices[1])
			dpuServices[1].Spec.ServiceID = ptr.To("service-one")
			Expect(testClient.Create(ctx, dpuServices[1])).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServices[1])

			By("Triggering node reconcile")
			Eventually(func(g Gomega) {
				updatedNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, types.NamespacedName{Name: workerNode.Name}, updatedNode)).To(Succeed())
				// trigger reconcile
				g.Expect(testutils.ForceObjectReconcileWithAnnotation(ctx, testClient, updatedNode)).To(Succeed())
			}).WithTimeout(5 * time.Second).Should(Succeed())

			By("Verifying taint is added on the node")
			Eventually(func(g Gomega) {
				updatedNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, types.NamespacedName{Name: workerNode.Name}, updatedNode)).To(Succeed())
				g.Expect(updatedNode.Spec.Taints).To(ContainElement(corev1.Taint{
					Key:    taintKey,
					Effect: taintEffect,
				}))
			}).WithTimeout(1 * time.Minute).Should(Succeed())

			By("Creating pod corresponding to the critical service")
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dpuServices[1].Name + "-test-pod",
					Namespace: "default",
					Labels: map[string]string{
						dpuservicev1.DPFServiceIDLabelKey: *dpuServices[1].Spec.ServiceID,
					},
				},
				Spec: corev1.PodSpec{
					NodeName: dpuName,
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "nginx:latest", // Any valid image name
						},
						{
							Name:  "test-container-2",
							Image: "nginx:latest", // Any valid image name
						},
					},
					// Required to avoid validation errors
					RestartPolicy: corev1.RestartPolicyAlways,
				},
			}
			Expect(dpuClusterClient.Create(ctx, pod)).To(Succeed())
			originalPod := pod.DeepCopy()
			pod.Status = corev1.PodStatus{
				Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{
					{
						Type:               corev1.PodReady,
						Status:             corev1.ConditionFalse,
						LastTransitionTime: metav1.Now(),
					},
					{
						Type:               corev1.PodInitialized,
						Status:             corev1.ConditionTrue,
						LastTransitionTime: metav1.Now(),
					},
					{
						Type:               corev1.ContainersReady,
						Status:             corev1.ConditionFalse,
						LastTransitionTime: metav1.Now(),
					},
					{
						Type:               corev1.PodScheduled,
						Status:             corev1.ConditionTrue,
						LastTransitionTime: metav1.Now(),
					},
				},
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name: "test-container",
						State: corev1.ContainerState{
							Running: &corev1.ContainerStateRunning{
								StartedAt: metav1.Now(),
							},
						},
						Ready:        true,
						RestartCount: 0,
					},
					{
						Name: "test-container-2",
						State: corev1.ContainerState{
							Terminated: &corev1.ContainerStateTerminated{
								Reason:     "Error",
								ExitCode:   1,
								StartedAt:  metav1.Now(),
								FinishedAt: metav1.Now(),
							},
						},
						Ready:        false,
						RestartCount: 20,
					},
				},
				PodIP:     "10.0.0.1",
				HostIP:    "192.168.1.1",
				StartTime: &metav1.Time{Time: time.Now()},
			}

			Expect(dpuClusterClient.Status().Patch(ctx, pod, client.MergeFrom(originalPod))).To(Succeed())

			By("Triggering node reconcile")
			Eventually(func(g Gomega) {
				updatedNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, types.NamespacedName{Name: workerNode.Name}, updatedNode)).To(Succeed())
				g.Expect(testutils.ForceObjectReconcileWithAnnotation(ctx, testClient, updatedNode)).To(Succeed())
			}).WithTimeout(5 * time.Second).Should(Succeed())

			By("Verifying taint is added on the node")
			Eventually(func(g Gomega) {
				updatedNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, types.NamespacedName{Name: workerNode.Name}, updatedNode)).To(Succeed())
				g.Expect(updatedNode.Spec.Taints).To(ContainElement(corev1.Taint{
					Key:    taintKey,
					Effect: taintEffect,
				}))
			}).WithTimeout(1 * time.Minute).Should(Succeed())

			// Force immediate deletion
			gracePeriod := int64(0)
			deleteOpts := &client.DeleteOptions{
				GracePeriodSeconds: &gracePeriod,
			}
			Expect(dpuClusterClient.Delete(ctx, pod, deleteOpts)).To(Succeed())
			// Check pod state after deletion
			Eventually(func(g Gomega) {
				podList := &corev1.PodList{}
				g.Expect(dpuClusterClient.List(ctx, podList)).To(Succeed())
				g.Expect(podList.Items).To(BeEmpty())
			}).WithTimeout(30 * time.Second).Should(Succeed())
		})

		It("should taint worker node when it has mix of critical/non-critical services not running", func() {
			dpuServices := getMinimalDPUServices(testNS.Name)
			// mark service as critical
			markServiceCritical(dpuServices[1])
			Expect(testClient.Create(ctx, dpuServices[1])).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServices[1])

			By("Creating another dpu service but don't mark it as critical/required")
			dpuServices[0].Spec.Interfaces = nil
			Expect(testClient.Create(ctx, dpuServices[0])).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServices[0])

			By("Triggering node reconcile")
			Eventually(func(g Gomega) {
				updatedNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, types.NamespacedName{Name: workerNode.Name}, updatedNode)).To(Succeed())
				g.Expect(testutils.ForceObjectReconcileWithAnnotation(ctx, testClient, updatedNode)).To(Succeed())
			}).WithTimeout(5 * time.Second).Should(Succeed())

			By("Verifying taint is added on the node")
			Eventually(func(g Gomega) {
				updatedNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, types.NamespacedName{Name: workerNode.Name}, updatedNode)).To(Succeed())
				g.Expect(updatedNode.Spec.Taints).To(ContainElement(corev1.Taint{
					Key:    taintKey,
					Effect: taintEffect,
				}))
			}).WithTimeout(5 * time.Second).Should(Succeed())
		})

		It("should ignore control plane nodes", func() {
			controlPlaneNode := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "control-plane-node",
					Labels: map[string]string{
						"node-role.kubernetes.io/control-plane": "",
						dpuEnabledLabelKey:                      dpuEnabledLabelValue,
					},
				},
			}
			Expect(testClient.Create(ctx, controlPlaneNode)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, controlPlaneNode)
			// Create DPU service
			dpuServices := getMinimalDPUServices(testNS.Name)
			dpuServices[0].Spec.Interfaces = nil
			dpuServices[0].Spec.ServiceDaemonSet.NodeSelector = &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{
								Key:      "node-role.kubernetes.io/control-plane",
								Operator: "Exists",
							},
						},
					},
				},
			}
			Expect(testClient.Create(ctx, dpuServices[0])).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServices[0])

			By("Triggering node reconcile")
			Eventually(func(g Gomega) {
				updatedNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, types.NamespacedName{Name: "control-plane-node"}, updatedNode)).To(Succeed())
				g.Expect(testutils.ForceObjectReconcileWithAnnotation(ctx, testClient, updatedNode)).To(Succeed())
			}).WithTimeout(5 * time.Second).Should(Succeed())

			By("Verifying control-plane-node is not tainted")
			Consistently(func(g Gomega) {
				updatedNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, types.NamespacedName{Name: "control-plane-node"}, updatedNode)).To(Succeed())
				g.Expect(updatedNode.Spec.Taints).NotTo(ContainElement(corev1.Taint{
					Key:    taintKey,
					Effect: taintEffect,
				}))
			}).WithTimeout(5 * time.Second).Should(Succeed())
		})

		It("should ignore worker nodes without the dpu", func() {
			dpuDisableWorkerNode := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "dpu-disable-worker-node",
					Labels: map[string]string{
						"feature.node.kubernetes.io/dpu-disabled": "false",
					},
				},
			}
			Expect(testClient.Create(ctx, dpuDisableWorkerNode)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuDisableWorkerNode)
			// Create DPU service
			dpuServices := getMinimalDPUServices(testNS.Name)
			// mark service as critical
			markServiceCritical(dpuServices[1])
			dpuServices[0].Spec.Interfaces = nil
			dpuServices[0].Spec.ServiceDaemonSet.NodeSelector = &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{
								Key:      "feature.node.kubernetes.io/dpu-disabled",
								Operator: corev1.NodeSelectorOpIn,
								Values:   []string{dpuEnabledLabelValue},
							},
						},
					},
				},
			}
			Expect(testClient.Create(ctx, dpuServices[0])).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServices[0])

			By("Triggering node reconcile")
			Eventually(func(g Gomega) {
				updatedNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, types.NamespacedName{Name: "dpu-disable-worker-node"}, updatedNode)).To(Succeed())
				g.Expect(testutils.ForceObjectReconcileWithAnnotation(ctx, testClient, updatedNode)).To(Succeed())
			}).WithTimeout(5 * time.Second).Should(Succeed())

			By("Verifying worker node without dpu feature discovery label is not tainted")
			Consistently(func(g Gomega) {
				updatedNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, types.NamespacedName{Name: "dpu-disable-worker-node"}, updatedNode)).To(Succeed())
				g.Expect(updatedNode.Spec.Taints).NotTo(ContainElement(corev1.Taint{
					Key:    taintKey,
					Effect: taintEffect,
				}))
			}).WithTimeout(5 * time.Second).Should(Succeed())
		})
	})

})
