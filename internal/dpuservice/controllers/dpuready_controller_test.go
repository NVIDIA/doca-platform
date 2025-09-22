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
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	dpucluster "github.com/nvidia/doca-platform/pkg/dpucluster"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
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

var _ = Describe("DPUReadyReconciler", func() {
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
				Labels: map[string]string{
					cutil.HostNameDPULabelKey: workerNode.Name,
				},
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
		It("should taint worker node when it has multiple DPUs and one of the DPUs is not ready", func() {
			By("Creating another DPU")
			dpu2 := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dpuName + "2",
					Namespace: testNS.Name,
				},
				Spec: provisioningv1.DPUSpec{
					DPUNodeName:   nodeName,
					DPUDeviceName: dpuDeviceName + "2",
					SerialNumber:  "MT25066004C8",
					Cluster: provisioningv1.K8sCluster{
						Name:      dpuCluster.Name,
						Namespace: dpuCluster.Namespace,
					},
				},
			}
			Expect(testClient.Create(ctx, dpu2)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpu2)

			By("Creating a new node in the DPUCluster")
			nodeInDPUCluster := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: dpu2.Name,
					Labels: map[string]string{
						cutil.HostNameDPULabelKey: workerNode.Name,
					},
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

			By("Creating a DPUService")
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

			By("Creating a pod corresponding to the critical service")
			pod1 := &corev1.Pod{
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
			Expect(dpuClusterClient.Create(ctx, pod1)).To(Succeed())
			originalPod1 := pod1.DeepCopy()
			pod1.Status = corev1.PodStatus{
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
			Expect(dpuClusterClient.Status().Patch(ctx, pod1, client.MergeFrom(originalPod1))).To(Succeed())
			// Wait for pod to be running
			Eventually(func(g Gomega) {
				updatedPod := &corev1.Pod{}
				g.Expect(dpuClusterClient.Get(ctx, types.NamespacedName{
					Name:      pod1.Name,
					Namespace: pod1.Namespace,
				}, updatedPod)).To(Succeed())
				g.Expect(updatedPod.Status.Phase).To(Equal(corev1.PodRunning))
			}).WithTimeout(5 * time.Second).Should(Succeed())

			By("Triggering node reconcile")
			Eventually(func(g Gomega) {
				updatedNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, types.NamespacedName{Name: workerNode.Name}, updatedNode)).To(Succeed())
				g.Expect(testutils.ForceObjectReconcileWithAnnotation(ctx, testClient, updatedNode)).To(Succeed())
			}).WithTimeout(5 * time.Second).Should(Succeed())

			By("Verifying taint is not removed from the node")
			Consistently(func(g Gomega) {
				updatedNode := &corev1.Node{}
				g.Expect(testClient.Get(ctx, types.NamespacedName{Name: workerNode.Name}, updatedNode)).To(Succeed())
				g.Expect(updatedNode.Spec.Taints).To(ContainElement(corev1.Taint{
					Key:    taintKey,
					Effect: taintEffect,
				}))
			}).WithTimeout(5 * time.Second).Should(Succeed())

			By("Force immediate deletion of the pods")
			gracePeriod := int64(0)
			deleteOpts := &client.DeleteOptions{
				GracePeriodSeconds: &gracePeriod,
			}
			Expect(dpuClusterClient.Delete(ctx, pod1, deleteOpts)).To(Succeed())
			// Check pod state after deletion
			Eventually(func(g Gomega) {
				podList := &corev1.PodList{}
				g.Expect(dpuClusterClient.List(ctx, podList)).To(Succeed())
				g.Expect(podList.Items).To(BeEmpty())
			}).WithTimeout(30 * time.Second).Should(Succeed())
		})
		It("should remove taint from worker node when it has multiple DPUs and all the DPUs are ready", func() {
			By("Creating another DPU")
			dpu2 := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dpuName + "2",
					Namespace: testNS.Name,
				},
				Spec: provisioningv1.DPUSpec{
					DPUNodeName:   nodeName,
					DPUDeviceName: dpuDeviceName + "2",
					SerialNumber:  "MT25066004C8",
					Cluster: provisioningv1.K8sCluster{
						Name:      dpuCluster.Name,
						Namespace: dpuCluster.Namespace,
					},
				},
			}
			Expect(testClient.Create(ctx, dpu2)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpu2)

			By("Creating a new node in the DPUCluster")
			nodeInDPUCluster := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: dpu2.Name,
					Labels: map[string]string{
						cutil.HostNameDPULabelKey: workerNode.Name,
					},
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

			By("Creating a DPUService")
			dpuServices := getMinimalDPUServices(testNS.Name)
			// mark service as critical
			markServiceCritical(dpuServices[1])
			Expect(testClient.Create(ctx, dpuServices[1])).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServices[1])

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

			By("Creating a pod corresponding to the critical service")
			dpuServices[1].Spec.ServiceID = ptr.To("service-one")
			pod1 := &corev1.Pod{
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
			Expect(dpuClusterClient.Create(ctx, pod1)).To(Succeed())
			originalPod1 := pod1.DeepCopy()
			pod1.Status = corev1.PodStatus{
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
			Expect(dpuClusterClient.Status().Patch(ctx, pod1, client.MergeFrom(originalPod1))).To(Succeed())
			// Wait for pod to be running
			Eventually(func(g Gomega) {
				updatedPod := &corev1.Pod{}
				g.Expect(dpuClusterClient.Get(ctx, types.NamespacedName{
					Name:      pod1.Name,
					Namespace: pod1.Namespace,
				}, updatedPod)).To(Succeed())
				g.Expect(updatedPod.Status.Phase).To(Equal(corev1.PodRunning))
			}).WithTimeout(5 * time.Second).Should(Succeed())

			By("Creating a pod corresponding to the second DPU")
			pod2 := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dpuServices[1].Name + "-test-pod-2",
					Namespace: "default",
					Labels: map[string]string{
						dpuservicev1.DPFServiceIDLabelKey: *dpuServices[1].Spec.ServiceID,
					},
				},
				Spec: corev1.PodSpec{
					NodeName: dpu2.Name,
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
			Expect(dpuClusterClient.Create(ctx, pod2)).To(Succeed())
			originalPod2 := pod2.DeepCopy()
			pod2.Status = corev1.PodStatus{
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
				PodIP:     "10.0.0.2",
				HostIP:    "192.168.1.2",
				StartTime: &metav1.Time{Time: time.Now()},
			}
			Expect(dpuClusterClient.Status().Patch(ctx, pod2, client.MergeFrom(originalPod2))).To(Succeed())
			// Wait for pod to be running
			Eventually(func(g Gomega) {
				updatedPod := &corev1.Pod{}
				g.Expect(dpuClusterClient.Get(ctx, types.NamespacedName{
					Name:      pod2.Name,
					Namespace: pod2.Namespace,
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
			}).WithTimeout(1 * time.Minute).Should(Succeed())

			By("Force immediate deletion of the pods")
			gracePeriod := int64(0)
			deleteOpts := &client.DeleteOptions{
				GracePeriodSeconds: &gracePeriod,
			}
			Expect(dpuClusterClient.Delete(ctx, pod1, deleteOpts)).To(Succeed())
			Expect(dpuClusterClient.Delete(ctx, pod2, deleteOpts)).To(Succeed())
			// Check pod state after deletion
			Eventually(func(g Gomega) {
				podList := &corev1.PodList{}
				g.Expect(dpuClusterClient.List(ctx, podList)).To(Succeed())
				g.Expect(podList.Items).To(BeEmpty())
			}).WithTimeout(30 * time.Second).Should(Succeed())
		})
	})
})

var _ = Describe("podPredicate", func() {
	Describe("Label filter predicate", func() {
		var labelPredicate predicate.Predicate

		BeforeEach(func() {
			labelPredicate = newLabelPredicate()
		})

		It("should accept pods with DPFServiceIDLabelKey", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Labels: map[string]string{
						dpuservicev1.DPFServiceIDLabelKey: "service-123",
					},
				},
			}
			Expect(labelPredicate.Create(event.CreateEvent{Object: pod})).To(BeTrue())
			Expect(labelPredicate.Update(event.UpdateEvent{ObjectOld: pod, ObjectNew: pod})).To(BeTrue())
			Expect(labelPredicate.Delete(event.DeleteEvent{Object: pod})).To(BeTrue())
			Expect(labelPredicate.Generic(event.GenericEvent{Object: pod})).To(BeTrue())
		})

		It("should reject pods without DPFServiceIDLabelKey", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Labels: map[string]string{
						"some-other-label": "value",
					},
				},
			}

			Expect(labelPredicate.Create(event.CreateEvent{Object: pod})).To(BeFalse())
			Expect(labelPredicate.Update(event.UpdateEvent{ObjectOld: pod, ObjectNew: pod})).To(BeFalse())
			Expect(labelPredicate.Delete(event.DeleteEvent{Object: pod})).To(BeFalse())
			Expect(labelPredicate.Generic(event.GenericEvent{Object: pod})).To(BeFalse())
		})

		It("should reject pods with no labels", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
			}

			Expect(labelPredicate.Create(event.CreateEvent{Object: pod})).To(BeFalse())
			Expect(labelPredicate.Update(event.UpdateEvent{ObjectOld: pod, ObjectNew: pod})).To(BeFalse())
			Expect(labelPredicate.Delete(event.DeleteEvent{Object: pod})).To(BeFalse())
			Expect(labelPredicate.Generic(event.GenericEvent{Object: pod})).To(BeFalse())
		})
	})

	Describe("Phase transition predicate", func() {
		var phasePredicate predicate.Predicate

		BeforeEach(func() {
			phasePredicate = newPhasePredicate()
		})

		Describe("CreateFunc", func() {
			It("should reject all create events", func() {
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
					},
				}

				Expect(phasePredicate.Create(event.CreateEvent{Object: pod})).To(BeFalse())
			})
		})

		Describe("UpdateFunc", func() {
			It("should accept transition from non-Running to Running", func() {
				oldPod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodPending,
					},
				}

				newPod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
					},
				}

				Expect(phasePredicate.Update(event.UpdateEvent{
					ObjectOld: oldPod,
					ObjectNew: newPod,
				})).To(BeTrue())
			})

			It("should accept transition from Running to non-Running", func() {
				oldPod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
					},
				}

				newPod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodFailed,
					},
				}

				Expect(phasePredicate.Update(event.UpdateEvent{
					ObjectOld: oldPod,
					ObjectNew: newPod,
				})).To(BeTrue())
			})

			It("should accept when deletion timestamp is set", func() {
				now := metav1.Now()
				oldPod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
					},
				}

				newPod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test-pod",
						Namespace:         "default",
						DeletionTimestamp: &now,
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
					},
				}

				Expect(phasePredicate.Update(event.UpdateEvent{
					ObjectOld: oldPod,
					ObjectNew: newPod,
				})).To(BeTrue())
			})

			It("should reject when phase doesn't change from/to Running", func() {
				oldPod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodPending,
					},
				}

				newPod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodSucceeded,
					},
				}

				Expect(phasePredicate.Update(event.UpdateEvent{
					ObjectOld: oldPod,
					ObjectNew: newPod,
				})).To(BeFalse())
			})

			It("should reject when phase stays Running", func() {
				oldPod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
					},
				}

				newPod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
						Labels: map[string]string{
							"new-label": "value",
						},
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
					},
				}

				Expect(phasePredicate.Update(event.UpdateEvent{
					ObjectOld: oldPod,
					ObjectNew: newPod,
				})).To(BeFalse())
			})

			It("should reject when deletion timestamp was already set", func() {
				now := metav1.Now()
				oldPod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test-pod",
						Namespace:         "default",
						DeletionTimestamp: &now,
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
					},
				}

				newPod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test-pod",
						Namespace:         "default",
						DeletionTimestamp: &now,
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
					},
				}

				Expect(phasePredicate.Update(event.UpdateEvent{
					ObjectOld: oldPod,
					ObjectNew: newPod,
				})).To(BeFalse())
			})
		})

		Describe("DeleteFunc", func() {
			It("should accept all delete events", func() {
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
					},
				}

				Expect(phasePredicate.Delete(event.DeleteEvent{Object: pod})).To(BeTrue())
			})
		})

		Describe("GenericFunc", func() {
			It("should reject all generic events", func() {
				pod := &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
					},
				}

				Expect(phasePredicate.Generic(event.GenericEvent{Object: pod})).To(BeFalse())
			})
		})
	})
})

var _ = Describe("podEventHandler", func() {
	var (
		handler      *podEventHandler
		queue        workqueue.TypedRateLimitingInterface[ctrl.Request]
		hostNodeName string
	)

	BeforeEach(func() {
		hostNodeName = "host-node"
		handler = &podEventHandler{
			client: testClient,
		}

		// Create a new workqueue for each test
		queue = workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[ctrl.Request]())

		DeferCleanup(func() {
			queue.ShutDown()
		})
	})

	Describe("handlePodEventHelper", func() {
		It("should enqueue the host node when pod's node has the host label", func() {
			// Create a node with the host label
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "dpu-node",
					Labels: map[string]string{
						cutil.HostNameDPULabelKey: hostNodeName,
					},
				},
			}
			Expect(testClient.Create(ctx, node)).To(Succeed())
			DeferCleanup(testClient.Delete, ctx, node)

			// Create a pod on that node
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					NodeName: "dpu-node",
				},
			}

			// Call the helper
			handler.handlePodEventHelper(ctx, pod, queue)

			// Verify the host node was enqueued
			Eventually(func() bool {
				item, shutdown := queue.Get()
				if shutdown {
					return false
				}
				defer queue.Done(item)

				return item.Name == hostNodeName && item.Namespace == ""
			}).Should(BeTrue())
		})

		It("should not enqueue when node doesn't exist", func() {
			// Create a pod on a non-existent node
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					NodeName: "non-existent-node",
				},
			}

			// Call the helper
			handler.handlePodEventHelper(ctx, pod, queue)

			// Verify nothing was enqueued
			Consistently(func() int {
				return queue.Len()
			}, 1*time.Second).Should(Equal(0))
		})

		It("should not enqueue when node doesn't have host label", func() {
			// Create a node without the host label
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "dpu-node",
					Labels: map[string]string{
						"some-other-label": "value",
					},
				},
			}
			Expect(testClient.Create(ctx, node)).To(Succeed())
			DeferCleanup(testClient.Delete, ctx, node)

			// Create a pod on that node
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					NodeName: "dpu-node",
				},
			}

			// Call the helper
			handler.handlePodEventHelper(ctx, pod, queue)

			// Verify nothing was enqueued
			Consistently(func() int {
				return queue.Len()
			}, 1*time.Second).Should(Equal(0))
		})
	})

	Describe("Update", func() {
		It("should process pod update events correctly", func() {
			// Create a node with the host label
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "dpu-node",
					Labels: map[string]string{
						cutil.HostNameDPULabelKey: hostNodeName,
					},
				},
			}
			Expect(testClient.Create(ctx, node)).To(Succeed())
			DeferCleanup(testClient.Delete, ctx, node)

			// Create a pod
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					NodeName: "dpu-node",
				},
			}

			// Create an update event
			updateEvent := event.UpdateEvent{
				ObjectOld: pod,
				ObjectNew: pod,
			}

			// Call Update
			handler.Update(ctx, updateEvent, queue)

			// Verify the host node was enqueued
			Eventually(func() bool {
				item, shutdown := queue.Get()
				if shutdown {
					return false
				}
				defer queue.Done(item)

				return item.Name == hostNodeName && item.Namespace == ""
			}).Should(BeTrue())
		})

		It("should handle non-pod objects gracefully", func() {
			// Create a non-pod object
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "some-node",
				},
			}

			// Create an update event with a non-pod object
			updateEvent := event.UpdateEvent{
				ObjectOld: node,
				ObjectNew: node,
			}

			// Call Update - should not panic
			handler.Update(ctx, updateEvent, queue)

			// Verify nothing was enqueued
			Consistently(func() int {
				return queue.Len()
			}, 1*time.Second).Should(Equal(0))
		})
	})

	Describe("Delete", func() {
		It("should process pod delete events correctly", func() {
			// Create a node with the host label
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "dpu-node",
					Labels: map[string]string{
						cutil.HostNameDPULabelKey: hostNodeName,
					},
				},
			}
			Expect(testClient.Create(ctx, node)).To(Succeed())
			DeferCleanup(testClient.Delete, ctx, node)

			// Create a pod
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					NodeName: "dpu-node",
				},
			}

			// Create a delete event
			deleteEvent := event.DeleteEvent{
				Object: pod,
			}

			// Call Delete
			handler.Delete(ctx, deleteEvent, queue)

			// Verify the host node was enqueued
			Eventually(func() bool {
				item, shutdown := queue.Get()
				if shutdown {
					return false
				}
				defer queue.Done(item)

				return item.Name == hostNodeName && item.Namespace == ""
			}).Should(BeTrue())
		})

		It("should handle non-pod objects gracefully", func() {
			// Create a non-pod object
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "some-node",
				},
			}

			// Create a delete event with a non-pod object
			deleteEvent := event.DeleteEvent{
				Object: node,
			}

			// Call Delete - should not panic
			handler.Delete(ctx, deleteEvent, queue)

			// Verify nothing was enqueued
			Consistently(func() int {
				return queue.Len()
			}, 1*time.Second).Should(Equal(0))
		})
	})

	Describe("enqueue", func() {
		It("should deduplicate requests before enqueueing", func() {
			requests := []reconcile.Request{
				{NamespacedName: client.ObjectKey{Name: "node1"}},
				{NamespacedName: client.ObjectKey{Name: "node2"}},
				{NamespacedName: client.ObjectKey{Name: "node1"}}, // duplicate
				{NamespacedName: client.ObjectKey{Name: "node3"}},
				{NamespacedName: client.ObjectKey{Name: "node2"}}, // duplicate
			}

			handler.enqueue(requests, queue)

			// Should only have 3 unique items
			Expect(queue.Len()).To(Equal(3))

			// Verify the unique items
			uniqueItems := make(map[string]bool)
			for queue.Len() > 0 {
				item, _ := queue.Get()
				uniqueItems[item.Name] = true
				queue.Done(item)
			}

			Expect(uniqueItems).To(HaveLen(3))
			Expect(uniqueItems).To(HaveKey("node1"))
			Expect(uniqueItems).To(HaveKey("node2"))
			Expect(uniqueItems).To(HaveKey("node3"))
		})

		It("should handle empty request list", func() {
			handler.enqueue([]reconcile.Request{}, queue)
			Expect(queue.Len()).To(Equal(0))
		})

		It("should handle single request", func() {
			requests := []reconcile.Request{
				{NamespacedName: client.ObjectKey{Name: "single-node"}},
			}

			handler.enqueue(requests, queue)
			Expect(queue.Len()).To(Equal(1))

			item, _ := queue.Get()
			Expect(item.Name).To(Equal("single-node"))
			queue.Done(item)
		})
	})

	Describe("Create", func() {
		It("should be a no-op", func() {
			// Create event should not do anything as per the implementation
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
			}

			createEvent := event.CreateEvent{
				Object: pod,
			}

			// Call Create - should be a no-op
			handler.Create(ctx, createEvent, queue)

			// Verify nothing was enqueued
			Expect(queue.Len()).To(Equal(0))
		})
	})

	Describe("Generic", func() {
		It("should be a no-op", func() {
			// Generic event should not do anything as per the implementation
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
			}

			genericEvent := event.GenericEvent{
				Object: pod,
			}

			// Call Generic - should be a no-op
			handler.Generic(ctx, genericEvent, queue)

			// Verify nothing was enqueued
			Expect(queue.Len()).To(Equal(0))
		})
	})
})
