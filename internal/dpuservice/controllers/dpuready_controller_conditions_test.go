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
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	operatorcontroller "github.com/nvidia/doca-platform/internal/operator/controllers"
	sfcsetcontroller "github.com/nvidia/doca-platform/internal/servicechainset/controllers"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	testutils "github.com/nvidia/doca-platform/test/utils"

	"github.com/fluxcd/pkg/runtime/patch"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Conditions Aggregation", func() {
	var reconciler *DPUReadyReconciler

	BeforeEach(func() {
		reconciler = &DPUReadyReconciler{}
	})

	Describe("formatUnreadyItems", func() {
		Context("when the list is empty", func() {
			It("should return empty string", func() {
				result := formatUnreadyItems([]string{})
				Expect(result).To(Equal(""))
			})
		})

		Context("when the list is within the limit", func() {
			It("should return all items joined with commas", func() {
				items := []string{"pod1", "pod2", "pod3"}
				result := formatUnreadyItems(items)
				Expect(result).To(Equal("pod1, pod2, pod3"))
			})

			It("should handle single item", func() {
				items := []string{"pod1"}
				result := formatUnreadyItems(items)
				Expect(result).To(Equal("pod1"))
			})

			It("should handle exactly maxUnreadyItemsInMessage items", func() {
				items := []string{"pod1", "pod2", "pod3", "pod4", "pod5"}
				result := formatUnreadyItems(items)
				Expect(result).To(Equal("pod1, pod2, pod3, pod4, pod5"))
			})
		})

		Context("when the list exceeds the limit", func() {
			It("should handle many items over the limit", func() {
				items := []string{"pod1", "pod2", "pod3", "pod4", "pod5", "pod6", "pod7"}
				result := formatUnreadyItems(items)
				Expect(result).To(Equal("pod1, pod2, pod3, pod4, pod5, ... (2 more)"))
			})
		})
	})

	Describe("aggregateOperationalReadySummary", func() {
		Context("when all conditions are True", func() {
			It("should return True summary condition", func() {
				conditions := []metav1.Condition{
					{Type: "Condition1", Status: metav1.ConditionTrue},
					{Type: "Condition2", Status: metav1.ConditionTrue},
					{Type: "Condition3", Status: metav1.ConditionTrue},
				}

				result := reconciler.aggregateOperationalReadySummary(conditions)

				Expect(result.Type).To(Equal(string(provisioningv1.DPUOperationalCondReady)))
				Expect(result.Status).To(Equal(metav1.ConditionTrue))
				Expect(result.Reason).To(Equal("AllReady"))
				Expect(result.Message).To(ContainSubstring("All operational conditions are ready"))
			})
		})

		Context("when some conditions are False", func() {
			It("should return False summary condition with list of not ready conditions", func() {
				conditions := []metav1.Condition{
					{Type: "NodeProblemsReady", Status: metav1.ConditionTrue},
					{Type: "CriticalPodsReady", Status: metav1.ConditionFalse},
					{Type: "InterfacesReady", Status: metav1.ConditionFalse},
				}

				result := reconciler.aggregateOperationalReadySummary(conditions)

				Expect(result.Type).To(Equal(string(provisioningv1.DPUOperationalCondReady)))
				Expect(result.Status).To(Equal(metav1.ConditionFalse))
				Expect(result.Reason).To(Equal("NotReady"))
				Expect(result.Message).To(ContainSubstring("CriticalPodsReady"))
				Expect(result.Message).To(ContainSubstring("InterfacesReady"))
			})

			It("should handle all conditions being False", func() {
				conditions := []metav1.Condition{
					{Type: "Condition1", Status: metav1.ConditionFalse},
					{Type: "Condition2", Status: metav1.ConditionFalse},
				}

				result := reconciler.aggregateOperationalReadySummary(conditions)

				Expect(result.Status).To(Equal(metav1.ConditionFalse))
				Expect(result.Message).To(ContainSubstring("Condition1"))
				Expect(result.Message).To(ContainSubstring("Condition2"))
			})
		})

		Context("when some conditions are Unknown", func() {
			It("should return Unknown summary condition when no False conditions exist", func() {
				conditions := []metav1.Condition{
					{Type: "Condition1", Status: metav1.ConditionTrue},
					{Type: "Condition2", Status: metav1.ConditionUnknown},
				}

				result := reconciler.aggregateOperationalReadySummary(conditions)

				Expect(result.Type).To(Equal(string(provisioningv1.DPUOperationalCondReady)))
				Expect(result.Status).To(Equal(metav1.ConditionUnknown))
				Expect(result.Reason).To(Equal("Unknown"))
				Expect(result.Message).To(ContainSubstring("Unknown conditions: Condition2"))
			})

			It("should prioritize False over Unknown", func() {
				conditions := []metav1.Condition{
					{Type: "Condition1", Status: metav1.ConditionFalse},
					{Type: "Condition2", Status: metav1.ConditionUnknown},
				}

				result := reconciler.aggregateOperationalReadySummary(conditions)

				Expect(result.Status).To(Equal(metav1.ConditionFalse))
				Expect(result.Reason).To(Equal("NotReadyAndUnknown"))
			})
		})

		Context("when the condition list is empty", func() {
			It("should return True summary condition", func() {
				conditions := []metav1.Condition{}

				result := reconciler.aggregateOperationalReadySummary(conditions)

				Expect(result.Status).To(Equal(metav1.ConditionTrue))
				Expect(result.Reason).To(Equal("AllReady"))
			})
		})
	})

	Describe("nodeConditionsEqual", func() {
		Context("when node conditions are identical", func() {
			It("should return true", func() {
				a := []corev1.NodeCondition{
					{Type: "Ready", Status: corev1.ConditionTrue, Reason: "KubeletReady", Message: "kubelet is ready"},
					{Type: "DiskPressure", Status: corev1.ConditionFalse, Reason: "NoDiskPressure", Message: "no disk pressure"},
				}
				b := []corev1.NodeCondition{
					{Type: "Ready", Status: corev1.ConditionTrue, Reason: "KubeletReady", Message: "kubelet is ready"},
					{Type: "DiskPressure", Status: corev1.ConditionFalse, Reason: "NoDiskPressure", Message: "no disk pressure"},
				}

				Expect(nodeConditionsEqual(a, b)).To(BeTrue())
			})

			It("should ignore LastTransitionTime and LastHeartbeatTime", func() {
				now := metav1.Now()
				later := metav1.NewTime(now.Add(1))

				a := []corev1.NodeCondition{
					{Type: "Ready", Status: corev1.ConditionTrue, Reason: "Reason1", Message: "Message1", LastTransitionTime: now, LastHeartbeatTime: now},
				}
				b := []corev1.NodeCondition{
					{Type: "Ready", Status: corev1.ConditionTrue, Reason: "Reason1", Message: "Message1", LastTransitionTime: later, LastHeartbeatTime: later},
				}

				Expect(nodeConditionsEqual(a, b)).To(BeTrue())
			})
		})

		Context("when node conditions differ", func() {
			It("should return false for different Status", func() {
				a := []corev1.NodeCondition{
					{Type: "Ready", Status: corev1.ConditionTrue, Reason: "KubeletReady", Message: "kubelet is ready"},
				}
				b := []corev1.NodeCondition{
					{Type: "Ready", Status: corev1.ConditionFalse, Reason: "KubeletReady", Message: "kubelet is ready"},
				}

				Expect(nodeConditionsEqual(a, b)).To(BeFalse())
			})

			It("should return false for different Reason", func() {
				a := []corev1.NodeCondition{
					{Type: "Ready", Status: corev1.ConditionTrue, Reason: "Reason1", Message: "Message1"},
				}
				b := []corev1.NodeCondition{
					{Type: "Ready", Status: corev1.ConditionTrue, Reason: "Reason2", Message: "Message1"},
				}

				Expect(nodeConditionsEqual(a, b)).To(BeFalse())
			})

			It("should return false for different Message", func() {
				a := []corev1.NodeCondition{
					{Type: "Ready", Status: corev1.ConditionTrue, Reason: "Reason1", Message: "Message1"},
				}
				b := []corev1.NodeCondition{
					{Type: "Ready", Status: corev1.ConditionTrue, Reason: "Reason1", Message: "Message2"},
				}

				Expect(nodeConditionsEqual(a, b)).To(BeFalse())
			})

			It("should return false for different lengths", func() {
				a := []corev1.NodeCondition{
					{Type: "Ready", Status: corev1.ConditionTrue, Reason: "Reason1", Message: "Message1"},
				}
				b := []corev1.NodeCondition{
					{Type: "Ready", Status: corev1.ConditionTrue, Reason: "Reason1", Message: "Message1"},
					{Type: "DiskPressure", Status: corev1.ConditionFalse, Reason: "Reason2", Message: "Message2"},
				}

				Expect(nodeConditionsEqual(a, b)).To(BeFalse())
			})
		})

		Context("when both are empty", func() {
			It("should return true", func() {
				Expect(nodeConditionsEqual([]corev1.NodeCondition{}, []corev1.NodeCondition{})).To(BeTrue())
			})
		})
	})
})

var _ = Describe("DPUReadyReconciler Conditions", func() {
	var (
		testNS           *corev1.Namespace
		testConfig       *operatorv1.DPFOperatorConfig
		dpu              *provisioningv1.DPU
		dpuCluster       provisioningv1.DPUCluster
		dpuClusterClient client.Client
		dpuClusterNode   *corev1.Node
		dpuNodeObj       *provisioningv1.DPUNode
	)

	defaultPauseDPUServiceReconciler := pauseDPUServiceReconciler.Load()
	BeforeEach(func() {
		By("Pausing other controllers that are not relevant for these tests")
		DeferCleanup(func() {
			pauseDPUServiceReconciler.Store(defaultPauseDPUServiceReconciler)
		})
		// These are modified to speed up the testing suite and also simplify the deletion logic
		pauseDPUServiceReconciler.Store(true)

		By("Creating the namespaces for the test")
		testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "dpuready-"}}
		Expect(testClient.Create(ctx, testNS)).To(Succeed())
		DeferCleanup(testClient.Delete, ctx, testNS)
		// Create the DPF System Namespace
		err := testClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace}})
		if !apierrors.IsAlreadyExists(err) {
			Expect(err).ToNot(HaveOccurred())
		}
		// Apply and get the DPFOperatorConfig. There is a race condition between the separate test runs why we have to fetch the config.
		// A real config is necessary to run our reconcileArgoSecrets tests.
		if testConfig == nil {
			testConfig = getMinimalDPFOperatorConfig()
			Expect(client.IgnoreAlreadyExists(testClient.Create(ctx, testConfig))).To(Succeed())
		}
		Expect(testClient.Get(ctx, client.ObjectKeyFromObject(testConfig), testConfig)).To(Succeed())

		By("Creating the DPUCluster")
		dpuCluster = testutils.GetTestDPUCluster(testNS.Name, "test")
		kamajiSecret, err := testutils.GetFakeKamajiClusterSecretFromEnvtest(dpuCluster, cfg)
		Expect(err).NotTo(HaveOccurred())
		Expect(testClient.Create(ctx, kamajiSecret)).To(Succeed())
		Expect(testClient.Create(ctx, &dpuCluster)).To(Succeed())
		DeferCleanup(testutils.CleanupAndWait, ctx, testClient, kamajiSecret)
		DeferCleanup(testutils.CleanupAndWait, ctx, testClient, &dpuCluster)

		By("Marking the DPUCluster as ready")
		patcher := patch.NewSerialPatcher(&dpuCluster, testClient)
		dpuCluster.Status.Phase = provisioningv1.PhaseReady
		Expect(patcher.Patch(ctx, &dpuCluster, patch.WithFieldOwner("test"))).To(Succeed())

		dpuClusterClient, err = dpucluster.NewConfig(testClient, &dpuCluster).Client(ctx)
		Expect(err).ToNot(HaveOccurred())

		By("Creating a DPU")
		dpu = &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-dpu",
				Namespace: testNS.Name,
			},
			Spec: provisioningv1.DPUSpec{
				DPUNodeName:   "test-node",
				DPUDeviceName: "test-node-0000-ca-00",
				SerialNumber:  "MT25066004C7",
				DPUFlavor:     "dpu-flavor",
				Cluster: provisioningv1.K8sCluster{
					Name:      dpuCluster.Name,
					Namespace: dpuCluster.Namespace,
				},
				NodeEffect: provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
			},
		}
		Expect(testClient.Create(ctx, dpu)).To(Succeed())
		DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpu)

		By("Setting DPU to Ready phase")
		patcher = patch.NewSerialPatcher(dpu, testClient)
		dpu.Status.Phase = provisioningv1.DPUReady
		Expect(patcher.Patch(ctx, dpu, patch.WithFieldOwner("test"))).To(Succeed())

		By("Creating a management cluster Node")
		managementNode := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-mgmt-node",
				Labels: map[string]string{
					provisioningv1.DPUNodeNameLabel:      dpu.Spec.DPUNodeName,
					provisioningv1.DPUNodeNamespaceLabel: testNS.Name,
				},
			},
		}
		Expect(testClient.Create(ctx, managementNode)).To(Succeed())
		DeferCleanup(testutils.CleanupAndWait, ctx, testClient, managementNode)

		By("Creating a DPUNode")
		dpuNodeObj = &provisioningv1.DPUNode{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dpu.Spec.DPUNodeName,
				Namespace: testNS.Name,
			},
		}
		Expect(testClient.Create(ctx, dpuNodeObj)).To(Succeed())
		DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuNodeObj)

		By("Setting DPUNode KubeNodeRef")
		patcher = patch.NewSerialPatcher(dpuNodeObj, testClient)
		dpuNodeObj.Status.KubeNodeRef = ptr.To(managementNode.Name)
		Expect(patcher.Patch(ctx, dpuNodeObj, patch.WithFieldOwner("test"))).To(Succeed())

		By("Creating a Node in the DPUCluster with healthy conditions")
		dpuClusterNode = &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: dpu.Name,
				Labels: map[string]string{
					provisioningv1.DPUNodeNameLabel:      dpuNodeObj.Name,
					provisioningv1.DPUNodeNamespaceLabel: dpuNodeObj.Namespace,
				},
			},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
				},
			},
		}
		for _, cond := range provisioningv1.GetNodeProblemDetectorConditions() {
			dpuClusterNode.Status.Conditions = append(dpuClusterNode.Status.Conditions, corev1.NodeCondition{
				Type:   corev1.NodeConditionType(cond),
				Status: corev1.ConditionFalse,
			})
		}
		Expect(dpuClusterClient.Create(ctx, dpuClusterNode)).To(Succeed())
		DeferCleanup(testutils.CleanupWithFinalizerRemovalAndWait, ctx, dpuClusterClient, dpuClusterNode)

		By("Creating DPUServices")
		criticalService := &dpuservicev1.DPUService{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "critical-service",
				Namespace: testNS.Name,
				Labels: map[string]string{
					criticalDPUServiceLabel: "",
				},
			},
			Spec: dpuservicev1.DPUServiceSpec{
				ServiceID: ptr.To("critical-svc-id"),
				HelmChart: dpuservicev1.HelmChart{
					Source: dpuservicev1.ApplicationSource{
						RepoURL: "oci://example.com",
						Version: "v1.0.0",
						Chart:   "test-chart",
					},
				},
			},
		}
		Expect(testClient.Create(ctx, criticalService)).To(Succeed())
		DeferCleanup(testutils.CleanupAndWait, ctx, testClient, criticalService)

		nonCriticalService := &dpuservicev1.DPUService{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "non-critical-service",
				Namespace: testNS.Name,
			},
			Spec: dpuservicev1.DPUServiceSpec{
				ServiceID: ptr.To("non-critical-svc-id"),
				HelmChart: dpuservicev1.HelmChart{
					Source: dpuservicev1.ApplicationSource{
						RepoURL: "oci://example.com",
						Version: "v1.0.0",
						Chart:   "test-chart",
					},
				},
			},
		}
		Expect(testClient.Create(ctx, nonCriticalService)).To(Succeed())
		DeferCleanup(testutils.CleanupAndWait, ctx, testClient, nonCriticalService)
	})

	Context("Node Health Condition", func() {
		It("should not update DPU status observedGeneration when patching operational conditions", func() {
			By("Seeding DPU status observedGeneration and a stale operational condition")
			createdDPU := &provisioningv1.DPU{}
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpu), createdDPU)).To(Succeed())
			originalDPU := createdDPU.DeepCopy()
			statusObservedGeneration := createdDPU.Generation + 100
			createdDPU.Status.ObservedGeneration = statusObservedGeneration
			createdDPU.Status.OperationalConditions = []metav1.Condition{
				{
					Type:               string(provisioningv1.DPUOperationalCondNodeProblemsReady),
					Status:             metav1.ConditionFalse,
					LastTransitionTime: metav1.Now(),
					Reason:             "Stale",
					Message:            "stale condition",
				},
			}
			Expect(testClient.Status().Patch(ctx, createdDPU, client.MergeFrom(originalDPU))).To(Succeed())

			By("Triggering DPUReady reconciliation")
			Expect(testutils.ForceObjectReconcileWithAnnotation(ctx, testClient, dpuNodeObj)).To(Succeed())

			By("Verifying status observedGeneration is unchanged")
			Eventually(func(g Gomega) {
				updatedDPU := &provisioningv1.DPU{}
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpu), updatedDPU)).To(Succeed())
				g.Expect(updatedDPU.Status.ObservedGeneration).To(Equal(statusObservedGeneration))
				g.Expect(updatedDPU.Status.OperationalConditions).NotTo(BeEmpty())
				g.Expect(updatedDPU.Status.OperationalConditions).To(ContainElement(
					And(
						HaveField("Type", Equal(string(provisioningv1.DPUOperationalCondNodeProblemsReady))),
						HaveField("Status", Equal(metav1.ConditionTrue)),
						HaveField("Reason", Equal("NoProblemsDetected")),
					),
				))
			}).WithTimeout(10 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())
		})

		It("should set NodeProblemsReady to True when all node conditions are healthy", func() {
			By("Waiting for reconciliation")

			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpu), dpu)).To(Succeed())
				g.Expect(dpu.Status.OperationalConditions).NotTo(BeEmpty())
				g.Expect(dpu.Status.OperationalConditions).To(ContainElement(
					And(
						HaveField("Type", Equal(string(provisioningv1.DPUOperationalCondNodeProblemsReady))),
						HaveField("Status", Equal(metav1.ConditionTrue)),
						HaveField("Reason", Equal("NoProblemsDetected")),
					),
				))
			}).WithTimeout(10 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())
		})

		It("should set NodeProblemsReady to False when node has problems", func() {
			By("Updating node to have a problem")
			Expect(dpuClusterClient.Get(ctx, client.ObjectKeyFromObject(dpuClusterNode), dpuClusterNode)).To(Succeed())
			patcher := patch.NewSerialPatcher(dpuClusterNode, dpuClusterClient)
			for i, cond := range dpuClusterNode.Status.Conditions {
				if cond.Type != "OVSHealthy" {
					continue
				}
				dpuClusterNode.Status.Conditions[i] = corev1.NodeCondition{
					Type:   "OVSHealthy",
					Status: corev1.ConditionTrue,
					Reason: "vSwitchdDown",
				}
				break
			}
			Expect(patcher.Patch(ctx, dpuClusterNode, patch.WithFieldOwner("test"))).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpu), dpu)).To(Succeed())
				g.Expect(dpu.Status.OperationalConditions).To(ContainElement(
					And(
						HaveField("Type", Equal(string(provisioningv1.DPUOperationalCondNodeProblemsReady))),
						HaveField("Status", Equal(metav1.ConditionFalse)),
						HaveField("Reason", Equal("NodeProblemDetectorNotReady")),
						HaveField("Message", ContainSubstring("OVSHealthy=vSwitchdDown")),
					),
				))
			}).WithTimeout(10 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())
		})

		It("should set all operational conditions to Unknown when Node is being deleted", func() {
			patcher := patch.NewSerialPatcher(dpuClusterNode, testClient)
			// We have to set a finalizer to the Node and only delete it then.
			// If we don't set a finalizer and delete it, the Ginkgo test will fail because the
			// object does no longer exist during DeferCleanup()
			dpuClusterNode.SetFinalizers([]string{"foo.bar/test-finalizer"})
			Expect(patcher.Patch(ctx, dpuClusterNode, patch.WithFieldOwner("test"))).To(Succeed())
			By("Setting DeletionTimestamp on the Node in DPU cluster")
			Expect(dpuClusterClient.Delete(ctx, dpuClusterNode)).To(Succeed())

			By("Verifying all operational conditions are set to Unknown with AwaitingDeletion reason")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpu), dpu)).To(Succeed())
				g.Expect(dpu.Status.OperationalConditions).To(HaveLen(6))
				g.Expect(dpu.Status.OperationalConditions).To(ConsistOf(
					And(
						HaveField("Type", Equal(string(provisioningv1.DPUOperationalCondNodeProblemsReady))),
						HaveField("Status", Equal(metav1.ConditionUnknown)),
						HaveField("Reason", Equal("AwaitingDeletion")),
						HaveField("Message", ContainSubstring("node is being deleted")),
					),
					And(
						HaveField("Type", Equal(string(provisioningv1.DPUOperationalCondDPUServiceCriticalPodsReady))),
						HaveField("Status", Equal(metav1.ConditionUnknown)),
						HaveField("Reason", Equal("AwaitingDeletion")),
						HaveField("Message", ContainSubstring("node is being deleted")),
					),
					And(
						HaveField("Type", Equal(string(provisioningv1.DPUOperationalCondDPUServiceNonCriticalPodsReady))),
						HaveField("Status", Equal(metav1.ConditionUnknown)),
						HaveField("Reason", Equal("AwaitingDeletion")),
						HaveField("Message", ContainSubstring("node is being deleted")),
					),
					And(
						HaveField("Type", Equal(string(provisioningv1.DPUOperationalCondDPUServiceInterfacesReady))),
						HaveField("Status", Equal(metav1.ConditionUnknown)),
						HaveField("Reason", Equal("AwaitingDeletion")),
						HaveField("Message", ContainSubstring("node is being deleted")),
					),
					And(
						HaveField("Type", Equal(string(provisioningv1.DPUOperationalCondDPUServiceChainsReady))),
						HaveField("Status", Equal(metav1.ConditionUnknown)),
						HaveField("Reason", Equal("AwaitingDeletion")),
						HaveField("Message", ContainSubstring("node is being deleted")),
					),
					And(
						HaveField("Type", Equal(string(provisioningv1.DPUOperationalCondReady))),
						HaveField("Status", Equal(metav1.ConditionUnknown)),
						HaveField("Reason", Equal("AwaitingDeletion")),
						HaveField("Message", ContainSubstring("node is being deleted")),
					),
				))
			}).WithTimeout(10 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())
		})
	})

	Context("Node lifecycle edge cases", func() {
		It("should set all operational conditions to Unknown with Pending reason when Node hasn't joined and DPU is NOT being deleted", func() {
			By("Setting DPU phase to DPUClusterConfig (not yet ready)")
			patcher := patch.NewSerialPatcher(dpu, testClient)
			dpu.Status.Phase = provisioningv1.DPUClusterConfig
			Expect(patcher.Patch(ctx, dpu, patch.WithFieldOwner("test"))).To(Succeed())

			By("Deleting the Node in DPU cluster to simulate not joined yet")
			Expect(dpuClusterClient.Delete(ctx, dpuClusterNode)).To(Succeed())
			Eventually(func() bool {
				err := dpuClusterClient.Get(ctx, client.ObjectKeyFromObject(dpuClusterNode), dpuClusterNode)
				return apierrors.IsNotFound(err)
			}).WithTimeout(3 * time.Second).Should(BeTrue())

			By("Triggering reconciliation")
			Expect(testutils.ForceObjectReconcileWithAnnotation(ctx, testClient, dpuNodeObj)).To(Succeed())

			By("Verifying all operational conditions are set to Unknown with Pending reason")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpu), dpu)).To(Succeed())
				g.Expect(dpu.Status.OperationalConditions).To(HaveLen(6))
				for _, cond := range dpu.Status.OperationalConditions {
					g.Expect(cond.Status).To(Equal(metav1.ConditionUnknown))
					g.Expect(cond.Reason).To(Equal("Pending"))
					g.Expect(cond.Message).To(Or(
						ContainSubstring("not joined"),
						ContainSubstring("Waiting for node"),
					))
				}
			}).WithTimeout(5 * time.Second).WithPolling(200 * time.Millisecond).Should(Succeed())
		})

		It("should set all operational conditions to False with Error reason when Node is deleted but DPU is Ready", func() {
			By("Ensuring DPU is in Ready phase first")
			patcher := patch.NewSerialPatcher(dpu, testClient)
			dpu.Status.Phase = provisioningv1.DPUReady
			Expect(patcher.Patch(ctx, dpu, patch.WithFieldOwner("test"))).To(Succeed())

			By("Deleting the Node in DPU cluster (simulating kubectl delete node)")
			Expect(dpuClusterClient.Delete(ctx, dpuClusterNode)).To(Succeed())
			Eventually(func() bool {
				err := dpuClusterClient.Get(ctx, client.ObjectKeyFromObject(dpuClusterNode), dpuClusterNode)
				return apierrors.IsNotFound(err)
			}).WithTimeout(3 * time.Second).Should(BeTrue())

			By("Triggering reconciliation")
			Expect(testutils.ForceObjectReconcileWithAnnotation(ctx, testClient, dpuNodeObj)).To(Succeed())

			By("Verifying all operational conditions are set to False with Error reason")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpu), dpu)).To(Succeed())
				g.Expect(dpu.Status.OperationalConditions).To(HaveLen(6))
				for _, cond := range dpu.Status.OperationalConditions {
					g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
					g.Expect(cond.Reason).To(Equal(string(conditions.ReasonError)))
					g.Expect(cond.Message).To(ContainSubstring("missing from cluster"))
				}
			}).WithTimeout(5 * time.Second).WithPolling(200 * time.Millisecond).Should(Succeed())
		})

		It("should set all operational conditions to Unknown with AwaitingDeletion when Node doesn't exist AND DPU is being deleted", func() {
			By("Deleting the Node in DPU cluster")
			Expect(dpuClusterClient.Delete(ctx, dpuClusterNode)).To(Succeed())
			Eventually(func() bool {
				err := dpuClusterClient.Get(ctx, client.ObjectKeyFromObject(dpuClusterNode), dpuClusterNode)
				return apierrors.IsNotFound(err)
			}).WithTimeout(3 * time.Second).Should(BeTrue())

			By("Marking DPU for deletion")
			patcher := patch.NewSerialPatcher(dpu, testClient)
			dpu.SetFinalizers([]string{"test.nvidia.com/finalizer"})
			Expect(patcher.Patch(ctx, dpu, patch.WithFieldOwner("test"))).To(Succeed())
			Expect(testClient.Delete(ctx, dpu)).To(Succeed())

			By("Verifying all operational conditions are set to Unknown with AwaitingDeletion reason")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpu), dpu)).To(Succeed())
				g.Expect(dpu.DeletionTimestamp.IsZero()).To(BeFalse())
				g.Expect(dpu.Status.OperationalConditions).To(HaveLen(6))
				for _, cond := range dpu.Status.OperationalConditions {
					g.Expect(cond.Status).To(Equal(metav1.ConditionUnknown))
					g.Expect(cond.Reason).To(Equal("AwaitingDeletion"))
					g.Expect(cond.Message).To(ContainSubstring("being deleted"))
				}
			}).WithTimeout(5 * time.Second).WithPolling(200 * time.Millisecond).Should(Succeed())
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpu), dpu)).To(Succeed())
			patcher = patch.NewSerialPatcher(dpu, testClient)
			dpu.SetFinalizers([]string{})
			Expect(patcher.Patch(ctx, dpu, patch.WithFieldOwner("test"))).To(Succeed())
		})

		It("should set all operational conditions to Unknown with AwaitingDeletion when DPU is being deleted (even if node exists)", func() {
			By("Marking DPU for deletion while node still exists")
			patcher := patch.NewSerialPatcher(dpu, testClient)
			dpu.SetFinalizers([]string{"test.nvidia.com/finalizer"})
			Expect(patcher.Patch(ctx, dpu, patch.WithFieldOwner("test"))).To(Succeed())
			Expect(testClient.Delete(ctx, dpu)).To(Succeed())

			By("Verifying all operational conditions are set to Unknown with AwaitingDeletion reason")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpu), dpu)).To(Succeed())
				g.Expect(dpu.DeletionTimestamp.IsZero()).To(BeFalse())
				g.Expect(dpu.Status.OperationalConditions).To(HaveLen(6))
				for _, cond := range dpu.Status.OperationalConditions {
					g.Expect(cond.Status).To(Equal(metav1.ConditionUnknown))
					g.Expect(cond.Reason).To(Equal("AwaitingDeletion"))
					g.Expect(cond.Message).To(ContainSubstring("being deleted"))
				}
			}).WithTimeout(5 * time.Second).WithPolling(200 * time.Millisecond).Should(Succeed())
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpu), dpu)).To(Succeed())
			patcher = patch.NewSerialPatcher(dpu, testClient)
			dpu.SetFinalizers([]string{})
			Expect(patcher.Patch(ctx, dpu, patch.WithFieldOwner("test"))).To(Succeed())
		})

		It("should transition from Pending to normal aggregation when Node joins", func() {
			By("Setting DPU phase to DPUClusterConfig (not yet ready)")
			patcher := patch.NewSerialPatcher(dpu, testClient)
			dpu.Status.Phase = provisioningv1.DPUClusterConfig
			Expect(patcher.Patch(ctx, dpu, patch.WithFieldOwner("test"))).To(Succeed())

			By("Initially having no node (simulating not joined)")
			Expect(dpuClusterClient.Delete(ctx, dpuClusterNode)).To(Succeed())
			Eventually(func() bool {
				err := dpuClusterClient.Get(ctx, client.ObjectKeyFromObject(dpuClusterNode), dpuClusterNode)
				return apierrors.IsNotFound(err)
			}).WithTimeout(3 * time.Second).Should(BeTrue())

			By("Triggering reconciliation")
			Expect(testutils.ForceObjectReconcileWithAnnotation(ctx, testClient, dpuNodeObj)).To(Succeed())

			By("Waiting for Pending conditions")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpu), dpu)).To(Succeed())
				cond := meta.FindStatusCondition(dpu.Status.OperationalConditions,
					string(provisioningv1.DPUOperationalCondReady))
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Reason).To(Equal("Pending"))
			}).WithTimeout(5 * time.Second).WithPolling(200 * time.Millisecond).Should(Succeed())

			By("Creating the Node (simulating join)")
			newNode := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: dpu.Name,
					Labels: map[string]string{
						provisioningv1.DPUNodeNameLabel:      dpuNodeObj.Name,
						provisioningv1.DPUNodeNamespaceLabel: dpuNodeObj.Namespace,
					},
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
					},
				},
			}
			for _, cond := range provisioningv1.GetNodeProblemDetectorConditions() {
				newNode.Status.Conditions = append(newNode.Status.Conditions, corev1.NodeCondition{
					Type:   corev1.NodeConditionType(cond),
					Status: corev1.ConditionFalse,
				})
			}
			Expect(dpuClusterClient.Create(ctx, newNode)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, newNode)

			By("Triggering reconciliation")
			Expect(testutils.ForceObjectReconcileWithAnnotation(ctx, testClient, dpuNodeObj)).To(Succeed())

			By("Verifying conditions transition to normal aggregation")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpu), dpu)).To(Succeed())
				cond := meta.FindStatusCondition(dpu.Status.OperationalConditions,
					string(provisioningv1.DPUOperationalCondNodeProblemsReady))
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).To(Equal("NoProblemsDetected"))
			}).WithTimeout(5 * time.Second).WithPolling(200 * time.Millisecond).Should(Succeed())
			Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpu), dpu)).To(Succeed())
			patcher = patch.NewSerialPatcher(dpu, testClient)
			dpu.SetFinalizers([]string{})
			Expect(patcher.Patch(ctx, dpu, patch.WithFieldOwner("test"))).To(Succeed())
		})
	})

	Context("Pod Readiness Conditions", func() {
		It("should aggregate critical and non-critical pod conditions", func() {
			By("Creating a running critical pod")
			criticalPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "critical-pod",
					Namespace: testNS.Name,
					Labels: map[string]string{
						dpuservicev1.DPFServiceIDLabelKey: "critical-svc-id",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: dpu.Name,
					Containers: []corev1.Container{
						{Name: "test", Image: "test"},
					},
				},
			}
			Expect(dpuClusterClient.Create(ctx, criticalPod)).To(Succeed())
			DeferCleanup(dpuClusterClient.Delete, ctx, criticalPod)

			By("Setting critical pod status to Running and Ready")
			criticalPod.Status = corev1.PodStatus{
				Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{
					{Type: corev1.PodReady, Status: corev1.ConditionTrue},
				},
			}
			Expect(dpuClusterClient.Status().Update(ctx, criticalPod)).To(Succeed())

			By("Creating a not-ready non-critical pod")
			nonCriticalPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "non-critical-pod",
					Namespace: testNS.Name,
					Labels: map[string]string{
						dpuservicev1.DPFServiceIDLabelKey: "non-critical-svc-id",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: dpu.Name,
					Containers: []corev1.Container{
						{Name: "test", Image: "test"},
					},
				},
			}
			Expect(dpuClusterClient.Create(ctx, nonCriticalPod)).To(Succeed())
			DeferCleanup(dpuClusterClient.Delete, ctx, nonCriticalPod)

			By("Setting non-critical pod status to Pending")
			nonCriticalPod.Status = corev1.PodStatus{
				Phase: corev1.PodPending,
			}
			Expect(dpuClusterClient.Status().Update(ctx, nonCriticalPod)).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpu), dpu)).To(Succeed())
				g.Expect(dpu.Status.OperationalConditions).To(ContainElements(
					And(
						HaveField("Type", Equal(string(provisioningv1.DPUOperationalCondDPUServiceCriticalPodsReady))),
						HaveField("Status", Equal(metav1.ConditionTrue)),
						HaveField("Reason", Equal("AllPodsReady")),
					),
					And(
						HaveField("Type", Equal(string(provisioningv1.DPUOperationalCondDPUServiceNonCriticalPodsReady))),
						HaveField("Status", Equal(metav1.ConditionFalse)),
						HaveField("Reason", Equal("PodsNotReady")),
						HaveField("Message", ContainSubstring("non-critical-pod")),
					),
				))
			}).WithTimeout(10 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())
		})
	})

	Context("Service Interfaces and Chains Conditions", func() {
		It("should aggregate service interfaces readiness", func() {
			By("Creating a DPUServiceInterface")
			dpuServiceInterface := &dpuservicev1.DPUServiceInterface{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-interface",
					Namespace: testNS.Name,
					Labels: map[string]string{
						dpuservicev1.ParentDPUDeploymentNameLabel: "test-deployment",
					},
				},
			}
			dpuServiceInterface.Spec.Template.Spec.Template.Spec.InterfaceType = dpuservicev1.InterfaceTypePhysical
			dpuServiceInterface.Spec.Template.Spec.Template.Spec.Physical = &dpuservicev1.Physical{
				InterfaceName: "eth0",
			}
			Expect(testClient.Create(ctx, dpuServiceInterface)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceInterface)

			By("Creating a ServiceInterface in DPU cluster")
			serviceInterface := &dpuservicev1.ServiceInterface{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-interface-instance",
					Namespace: testNS.Name,
					Labels: map[string]string{
						sfcsetcontroller.ServiceInterfaceSetNameLabel:      dpuServiceInterface.Name,
						sfcsetcontroller.ServiceInterfaceSetNamespaceLabel: dpuServiceInterface.Namespace,
					},
				},
				Spec: dpuservicev1.ServiceInterfaceSpec{
					InterfaceType: dpuservicev1.InterfaceTypePhysical,
					Physical: &dpuservicev1.Physical{
						InterfaceName: "eth0",
					},
					Node: ptr.To(dpu.Name),
				},
			}
			Expect(dpuClusterClient.Create(ctx, serviceInterface)).To(Succeed())
			DeferCleanup(dpuClusterClient.Delete, ctx, serviceInterface)

			By("Setting ServiceInterface status as ready")
			// Fetch the latest version before updating status
			Expect(dpuClusterClient.Get(ctx, client.ObjectKeyFromObject(serviceInterface), serviceInterface)).To(Succeed())
			meta.SetStatusCondition(&serviceInterface.Status.Conditions, metav1.Condition{
				Type:               string(conditions.TypeReady),
				Status:             metav1.ConditionTrue,
				Reason:             "Ready",
				ObservedGeneration: serviceInterface.Generation,
			})
			Expect(dpuClusterClient.Status().Update(ctx, serviceInterface)).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpu), dpu)).To(Succeed())
				g.Expect(dpu.Status.OperationalConditions).To(ContainElement(
					And(
						HaveField("Type", Equal(string(provisioningv1.DPUOperationalCondDPUServiceInterfacesReady))),
						HaveField("Status", Equal(metav1.ConditionTrue)),
						HaveField("Reason", Equal("AllServiceInterfacesReady")),
					),
				))
			}).WithTimeout(10 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())
		})

		It("should aggregate service chains readiness", func() {
			By("Creating a DPUServiceChain")
			dpuServiceChain := &dpuservicev1.DPUServiceChain{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-chain",
					Namespace: testNS.Name,
					Labels: map[string]string{
						dpuservicev1.ParentDPUDeploymentNameLabel: "test-deployment",
					},
				},
			}
			dpuServiceChain.Spec.Template.Spec.Template.Spec.Switches = []dpuservicev1.Switch{{
				Ports: []dpuservicev1.Port{{
					ServiceInterface: dpuservicev1.ServiceIfc{
						MatchLabels: map[string]string{"test": "port"},
					},
				}},
			}}
			Expect(testClient.Create(ctx, dpuServiceChain)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, testClient, dpuServiceChain)

			By("Creating a ready ServiceChain in DPU cluster")
			serviceChain := &dpuservicev1.ServiceChain{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-chain-instance",
					Namespace: testNS.Name,
					Labels: map[string]string{
						sfcsetcontroller.ServiceChainSetNameLabel:      dpuServiceChain.Name,
						sfcsetcontroller.ServiceChainSetNamespaceLabel: dpuServiceChain.Namespace,
					},
				},
				Spec: dpuservicev1.ServiceChainSpec{
					Node: ptr.To(dpu.Name),
					Switches: []dpuservicev1.Switch{{
						Ports: []dpuservicev1.Port{{
							ServiceInterface: dpuservicev1.ServiceIfc{
								MatchLabels: map[string]string{"test": "port"},
							},
						}},
					}},
				},
			}
			Expect(dpuClusterClient.Create(ctx, serviceChain)).To(Succeed())
			DeferCleanup(dpuClusterClient.Delete, ctx, serviceChain)

			By("Setting ServiceChain as ready")
			// Fetch the latest version before updating status
			Expect(dpuClusterClient.Get(ctx, client.ObjectKeyFromObject(serviceChain), serviceChain)).To(Succeed())
			meta.SetStatusCondition(&serviceChain.Status.Conditions, metav1.Condition{
				Type:               string(conditions.TypeReady),
				Status:             metav1.ConditionTrue,
				Reason:             "Ready",
				ObservedGeneration: serviceChain.Generation,
			})
			Expect(dpuClusterClient.Status().Update(ctx, serviceChain)).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpu), dpu)).To(Succeed())
				g.Expect(dpu.Status.OperationalConditions).To(ContainElement(
					And(
						HaveField("Type", Equal(string(provisioningv1.DPUOperationalCondDPUServiceChainsReady))),
						HaveField("Status", Equal(metav1.ConditionTrue)),
						HaveField("Reason", Equal("AllServiceChainsReady")),
					),
				))
			}).WithTimeout(10 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())
		})
	})

	Context("Overall OperationalReady Condition", func() {
		It("should aggregate all conditions into OperationalReady", func() {
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpu), dpu)).To(Succeed())
				g.Expect(dpu.Status.OperationalConditions).NotTo(BeEmpty())

				operationalReadyCondition := meta.FindStatusCondition(dpu.Status.OperationalConditions,
					string(provisioningv1.DPUOperationalCondReady))
				g.Expect(operationalReadyCondition).NotTo(BeNil())
			}).WithTimeout(10 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())
		})

		It("should set OperationalReady to False when any sub-condition is False", func() {
			By("Making a node condition fail")
			Expect(dpuClusterClient.Get(ctx, client.ObjectKeyFromObject(dpuClusterNode), dpuClusterNode)).To(Succeed())
			patcher := patch.NewSerialPatcher(dpuClusterNode, dpuClusterClient)
			for i, cond := range dpuClusterNode.Status.Conditions {
				if cond.Type != "OVSHealthy" {
					continue
				}
				dpuClusterNode.Status.Conditions[i] = corev1.NodeCondition{
					Type:   "OVSHealthy",
					Status: corev1.ConditionTrue,
					Reason: "vSwitchdDown",
				}
				break
			}
			Expect(patcher.Patch(ctx, dpuClusterNode, patch.WithFieldOwner("test"))).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpu), dpu)).To(Succeed())
				g.Expect(dpu.Status.OperationalConditions).To(ContainElement(
					And(
						HaveField("Type", Equal(string(provisioningv1.DPUOperationalCondNodeProblemsReady))),
						HaveField("Status", Equal(metav1.ConditionFalse)),
						HaveField("Reason", Equal("NodeProblemDetectorNotReady")),
						HaveField("Message", ContainSubstring("OVSHealthy=vSwitchdDown")),
					),
				))
			}).WithTimeout(10 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())
		})
	})
})
