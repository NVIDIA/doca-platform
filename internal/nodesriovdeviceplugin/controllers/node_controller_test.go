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

	noderesourcesv1 "github.com/nvidia/doca-platform/api/noderesources/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/util/flowcontrol"
	clocktesting "k8s.io/utils/clock/testing"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

const ownerConfigMapName = "test-owner-configmap"

var _ = Describe("NodeReconciler", Ordered, func() {
	var (
		ctx           context.Context
		cancel        context.CancelFunc
		managerStopCh chan struct{}
		testManager   ctrl.Manager
	)
	BeforeAll(func() {
		ctx, cancel = context.WithCancel(testCtx)

		By("starting manager with NodeReconciler")
		var err error
		testManager, err = ctrl.NewManager(cfg, ctrl.Options{
			Controller: config.Controller{
				SkipNameValidation: ptr.To(true),
			},
			Scheme: scheme.Scheme,
			Client: GetClientOptions(),
			// Set cache resync to one hour to avoid hiding issues caused by
			// poorly configured watches (tests should rely on events, not
			// frequent resyncs).
			Cache:   GetCacheOptions(testNamespace, time.Hour, ownerConfigMapName),
			Metrics: server.Options{BindAddress: "0"},
		})
		Expect(err).ToNot(HaveOccurred())

		Expect(SetupIndexers(ctx, testManager)).To(Succeed())

		err = (&NodeReconciler{
			Client:             testManager.GetClient(),
			Scheme:             testManager.GetScheme(),
			Namespace:          testNamespace,
			OwnerConfigMapName: ownerConfigMapName,
			DevicePluginConfig: DevicePluginConfig{
				Image:                 "test-device-plugin-image:latest",
				InitImage:             "test-init-image:latest",
				DefaultResourcePrefix: DefaultResourcePrefix,
			},
			FailedPodsBackoff: flowcontrol.NewBackOff(
				FailedPodBackoffBaseDelay, FailedPodBackoffMaxDelay),
		}).SetupWithManager(testManager)
		Expect(err).ToNot(HaveOccurred())

		managerStopCh = make(chan struct{})
		go func() {
			defer GinkgoRecover()
			defer close(managerStopCh)
			Expect(testManager.Start(ctx)).To(Succeed())
		}()
	})
	AfterAll(func() {
		cancel()
		Eventually(func(g Gomega) {
			g.Expect(managerStopCh).To(BeClosed())
		}).WithTimeout(10 * time.Second).Should(Succeed())
	})
	BeforeEach(func() {
		By("ensuring owner ConfigMap exists")
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ownerConfigMapName,
				Namespace: testNamespace,
			},
		}
		err := testClient.Create(ctx, cm)
		if err != nil && !apierrors.IsAlreadyExists(err) {
			Expect(err).ToNot(HaveOccurred())
		}
	})
	AfterEach(func() {
		By("cleaning up test objects")
		cleanupTestObjects(ctx, testClient)
	})
	Context("when reconciling a Node with target DPUs", func() {
		It("should create a device plugin pod when DPU has HostNetworkReady", func() {
			node, _, dpu, _ := createNodeWithDPU(ctx, "test-node-1", "test-dpunode-1",
				"test-dpu-1", "test-config-1")
			setDPUHostNetworkReady(ctx, dpu)

			Eventually(func(g Gomega) {
				pod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{
					Namespace: testNamespace,
					Name:      generatePodName(node.Name),
				}, pod)).To(Succeed())
				g.Expect(pod.Labels).To(HaveKeyWithValue(ManagedByLabelKey, ManagedByLabelValue))
				g.Expect(pod.Annotations).To(HaveKey(PodInputAnnotationKey))
				g.Expect(pod.Annotations).To(HaveKey(PodObjectHashAnnotationKey))
				g.Expect(pod.OwnerReferences).To(HaveLen(1))
				g.Expect(pod.OwnerReferences[0].APIVersion).To(Equal("v1"))
				g.Expect(pod.OwnerReferences[0].Kind).To(Equal("ConfigMap"))
				g.Expect(pod.OwnerReferences[0].Name).To(Equal(ownerConfigMapName))
				g.Expect(pod.OwnerReferences[0].Controller).NotTo(BeNil())
				g.Expect(*pod.OwnerReferences[0].Controller).To(BeTrue())

				cm := &corev1.ConfigMap{}
				g.Expect(testClient.Get(ctx, client.ObjectKey{
					Namespace: testNamespace,
					Name:      ownerConfigMapName,
				}, cm)).To(Succeed())
				g.Expect(pod.OwnerReferences[0].UID).To(Equal(cm.UID))
			}, testTimeout, testInterval).Should(Succeed())
		})
		It("should update the pod when a second DPU is added", func() {
			node, dpuNode, dpu1, _ := createNodeWithDPU(ctx, "test-node-second-dpu",
				"test-dpunode-second-dpu", "test-dpu-first", "test-config-first")
			setDPUHostNetworkReady(ctx, dpu1)

			podKey := client.ObjectKey{Namespace: testNamespace, Name: generatePodName(node.Name)}
			var originalHash string
			var originalInput string

			// Wait for pod to be created and capture the initial input/hash.
			Eventually(func(g Gomega) {
				pod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, podKey, pod)).To(Succeed())
				originalHash = pod.Annotations[PodObjectHashAnnotationKey]
				originalInput = pod.Annotations[PodInputAnnotationKey]
				g.Expect(originalHash).NotTo(BeEmpty())
				g.Expect(originalInput).To(ContainSubstring(dpu1.Spec.SerialNumber))
			}, testTimeout, testInterval).Should(Succeed())

			// Add a second DPU with a different config.
			secondConfig := &noderesourcesv1.NodeSRIOVDevicePluginConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test-config-second", Namespace: testNamespace},
				Spec: noderesourcesv1.NodeSRIOVDevicePluginConfigSpec{
					DevicePluginResources: []noderesourcesv1.DevicePluginResource{
						{
							Name:           "test-vf-second",
							ResourcePrefix: ptr.To("nvidia.com"),
							Type:           noderesourcesv1.DevicePluginResourceTypeVF,
							Ranges: []noderesourcesv1.FunctionRange{
								{PFIndex: 0, Start: ptr.To(int32(6)), End: ptr.To(int32(10))},
							},
						},
					},
				},
			}
			Expect(testClient.Create(ctx, secondConfig)).To(Succeed())

			dpu2 := &provisioningv1.DPU{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpu-second",
					Namespace: testNamespace,
					Annotations: map[string]string{
						DPUDevicePluginConfigAnnotationKey: secondConfig.Name,
					},
				},
				Spec: provisioningv1.DPUSpec{
					DPUNodeName:   dpuNode.Name,
					DPUDeviceName: "bf3-1",
					BFB:           ptr.To("test-bfb-second"),
					SerialNumber:  "SN654321",
					DPUFlavor:     "test-flavor",
					NodeEffect:    provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
				},
			}
			Expect(testClient.Create(ctx, dpu2)).To(Succeed())
			setDPUHostNetworkReady(ctx, dpu2)

			// Verify pod input/hash is updated to include the new DPU.
			Eventually(func(g Gomega) {
				pod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, podKey, pod)).To(Succeed())
				newHash := pod.Annotations[PodObjectHashAnnotationKey]
				newInput := pod.Annotations[PodInputAnnotationKey]
				g.Expect(newHash).NotTo(Equal(originalHash))
				g.Expect(newInput).NotTo(Equal(originalInput))
				g.Expect(newInput).To(ContainSubstring(dpu1.Spec.SerialNumber))
				g.Expect(newInput).To(ContainSubstring(dpu2.Spec.SerialNumber))
			}, testTimeout, testInterval).Should(Succeed())
		})
	})
	Context("when a node has no target DPUs", func() {
		It("should not create a device plugin pod", func() {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "test-node-no-dpus"},
			}
			Expect(testClient.Create(ctx, node)).To(Succeed())

			Consistently(func(g Gomega) {
				g.Expect(apierrors.IsNotFound(testClient.Get(ctx, client.ObjectKey{
					Namespace: testNamespace,
					Name:      generatePodName(node.Name),
				}, &corev1.Pod{}))).To(BeTrue())
			}, time.Second*3, testInterval).Should(Succeed())
		})
	})
	Context("when DPU HostNetworkReady condition is false", func() {
		It("should not create a device plugin pod", func() {
			node, _, _, _ := createNodeWithDPU(ctx, "test-node-no-hostnet",
				"test-dpunode-no-hostnet", "test-dpu-no-hostnet", "test-config-no-hostnet")

			Consistently(func(g Gomega) {
				pod := &corev1.Pod{}
				err := testClient.Get(ctx, client.ObjectKey{
					Namespace: testNamespace,
					Name:      generatePodName(node.Name),
				}, pod)
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}, time.Second*3, testInterval).Should(Succeed())
		})
		It("should create pod when HostNetworkReady transitions to true", func() {
			node, _, dpu, _ := createNodeWithDPU(ctx, "test-node-hostnet-transition",
				"test-dpunode-hostnet-transition", "test-dpu-hostnet-transition",
				"test-config-hostnet-transition")

			podKey := client.ObjectKey{Namespace: testNamespace, Name: generatePodName(node.Name)}

			// Verify pod is not created initially.
			Consistently(func(g Gomega) {
				g.Expect(apierrors.IsNotFound(testClient.Get(ctx, podKey, &corev1.Pod{}))).To(BeTrue())
			}, time.Second*2, testInterval).Should(Succeed())

			// Set HostNetworkReady condition to true.
			setDPUHostNetworkReady(ctx, dpu)

			// Verify pod is created.
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, podKey, &corev1.Pod{})).To(Succeed())
			}, testTimeout, testInterval).Should(Succeed())
		})
	})
	Context("when config is invalid", func() {
		It("should delete the pod and not recreate it when config becomes invalid", func() {
			node, _, dpu, config := createNodeWithDPU(ctx, "test-node-invalid-config",
				"test-dpunode-invalid-config", "test-dpu-invalid-config", "test-config-invalid")
			setDPUHostNetworkReady(ctx, dpu)

			podKey := client.ObjectKey{Namespace: testNamespace, Name: generatePodName(node.Name)}

			// Wait for pod to be created.
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, podKey, &corev1.Pod{})).To(Succeed())
			}, testTimeout, testInterval).Should(Succeed())

			// Update config to be invalid (overlap ranges).
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(config), config)).To(Succeed())
				config.Spec.DevicePluginResources[0].Ranges = []noderesourcesv1.FunctionRange{{PFIndex: 0}, {PFIndex: 0}}
				g.Expect(testClient.Update(ctx, config)).To(Succeed())
			}, testTimeout, testInterval).Should(Succeed())

			// Verify pod is deleted.
			Eventually(func(g Gomega) {
				g.Expect(apierrors.IsNotFound(testClient.Get(ctx, podKey, &corev1.Pod{}))).To(BeTrue())
			}, testTimeout, testInterval).Should(Succeed())

			// Verify pod is NOT recreated because config is now invalid.
			Consistently(func(g Gomega) {
				g.Expect(apierrors.IsNotFound(testClient.Get(ctx, podKey, &corev1.Pod{}))).To(BeTrue())
			}, time.Second*3, testInterval).Should(Succeed())
		})
	})
	Context("when a DPU annotation is removed", func() {
		It("should delete the device plugin pod when annotation is removed", func() {
			node, _, dpu, _ := createNodeWithDPU(ctx, "test-node-2", "test-dpunode-2",
				"test-dpu-2", "test-config-2")
			setDPUHostNetworkReady(ctx, dpu)

			podKey := client.ObjectKey{Namespace: testNamespace, Name: generatePodName(node.Name)}

			// Wait for pod to be created.
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, podKey, &corev1.Pod{})).To(Succeed())
			}, testTimeout, testInterval).Should(Succeed())

			// Remove the annotation from the DPU.
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpu), dpu)).To(Succeed())
				delete(dpu.Annotations, DPUDevicePluginConfigAnnotationKey)
				g.Expect(testClient.Update(ctx, dpu)).To(Succeed())
			}, testTimeout, testInterval).Should(Succeed())

			// Verify pod is deleted.
			Eventually(func(g Gomega) {
				g.Expect(apierrors.IsNotFound(testClient.Get(ctx, podKey, &corev1.Pod{}))).To(BeTrue())
			}, testTimeout, testInterval).Should(Succeed())
		})
	})
	Context("when config is updated", func() {
		It("should recreate the pod with new config", func() {
			node, _, dpu, config := createNodeWithDPU(ctx, "test-node-3", "test-dpunode-3",
				"test-dpu-3", "test-config-3")
			setDPUHostNetworkReady(ctx, dpu)

			podKey := client.ObjectKey{Namespace: testNamespace, Name: generatePodName(node.Name)}
			var originalHash string

			// Wait for pod to be created and capture the hash.
			Eventually(func(g Gomega) {
				pod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, podKey, pod)).To(Succeed())
				originalHash = pod.Annotations[PodObjectHashAnnotationKey]
				g.Expect(originalHash).NotTo(BeEmpty())
			}, testTimeout, testInterval).Should(Succeed())

			// Update the config.
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(config), config)).To(Succeed())
				config.Spec.DevicePluginResources[0].Ranges[0].End = ptr.To(int32(20))
				g.Expect(testClient.Update(ctx, config)).To(Succeed())
			}, testTimeout, testInterval).Should(Succeed())

			// Verify pod is recreated with new hash.
			Eventually(func(g Gomega) {
				pod := &corev1.Pod{}
				g.Expect(testClient.Get(ctx, podKey, pod)).To(Succeed())
				newHash := pod.Annotations[PodObjectHashAnnotationKey]
				g.Expect(newHash).NotTo(Equal(originalHash))
			}, testTimeout, testInterval).Should(Succeed())
		})
	})
	Context("when pod is in terminal state", func() {
		DescribeTable("should delete and recreate the pod",
			func(phase corev1.PodPhase) {
				node, _, dpu, _ := createNodeWithDPU(ctx, "test-node-terminal",
					"test-dpunode-terminal", "test-dpu-terminal", "test-config-terminal")
				setDPUHostNetworkReady(ctx, dpu)

				podKey := client.ObjectKey{Namespace: testNamespace, Name: generatePodName(node.Name)}
				var originalUID types.UID

				// Wait for pod to be created and capture the UID.
				Eventually(func(g Gomega) {
					pod := &corev1.Pod{}
					g.Expect(testClient.Get(ctx, podKey, pod)).To(Succeed())
					originalUID = pod.UID
				}, testTimeout, testInterval).Should(Succeed())

				// Set pod to terminal state.
				Eventually(func(g Gomega) {
					pod := &corev1.Pod{}
					g.Expect(testClient.Get(ctx, podKey, pod)).To(Succeed())
					pod.Status.Phase = phase
					g.Expect(testClient.Status().Update(ctx, pod)).To(Succeed())
				}, testTimeout, testInterval).Should(Succeed())

				// Verify pod is recreated with a new UID.
				Eventually(func(g Gomega) {
					pod := &corev1.Pod{}
					g.Expect(testClient.Get(ctx, podKey, pod)).To(Succeed())
					g.Expect(pod.UID).NotTo(Equal(originalUID))
				}, testTimeout, testInterval).Should(Succeed())
			},
			Entry("when pod phase is Failed", corev1.PodFailed),
			Entry("when pod phase is Succeeded", corev1.PodSucceeded),
		)
	})
})

// createNodeWithDPU creates all objects needed for a DPU setup.
// The DPU is created with HostNetworkReady condition set to false by default.
//
//nolint:unparam
func createNodeWithDPU(ctx context.Context, nodeName, dpuNodeName, dpuName, configName string) (
	*corev1.Node, *provisioningv1.DPUNode, *provisioningv1.DPU,
	*noderesourcesv1.NodeSRIOVDevicePluginConfig) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: nodeName},
	}
	ExpectWithOffset(1, testClient.Create(ctx, node)).To(Succeed())

	dpuNode := &provisioningv1.DPUNode{
		ObjectMeta: metav1.ObjectMeta{Name: dpuNodeName, Namespace: testNamespace},
		Spec:       provisioningv1.DPUNodeSpec{},
	}
	ExpectWithOffset(1, testClient.Create(ctx, dpuNode)).To(Succeed())

	EventuallyWithOffset(1, func(g Gomega) {
		g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpuNode), dpuNode)).To(Succeed())
		dpuNode.Status.KubeNodeRef = ptr.To(node.Name)
		g.Expect(testClient.Status().Update(ctx, dpuNode)).To(Succeed())
	}, testTimeout, testInterval).Should(Succeed())

	config := &noderesourcesv1.NodeSRIOVDevicePluginConfig{
		ObjectMeta: metav1.ObjectMeta{Name: configName, Namespace: testNamespace},
		Spec: noderesourcesv1.NodeSRIOVDevicePluginConfigSpec{
			DevicePluginResources: []noderesourcesv1.DevicePluginResource{
				{
					Name:           "test-vf",
					ResourcePrefix: ptr.To("nvidia.com"),
					Type:           noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.FunctionRange{
						{PFIndex: 0, Start: ptr.To(int32(1)), End: ptr.To(int32(5))},
					},
				},
			},
		},
	}
	ExpectWithOffset(1, testClient.Create(ctx, config)).To(Succeed())

	dpu := &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dpuName,
			Namespace: testNamespace,
			Annotations: map[string]string{
				DPUDevicePluginConfigAnnotationKey: config.Name,
			},
		},
		Spec: provisioningv1.DPUSpec{
			DPUNodeName:   dpuNode.Name,
			DPUDeviceName: "bf3-0",
			BFB:           ptr.To("test-bfb"),
			SerialNumber:  "SN123456",
			DPUFlavor:     "test-flavor",
			NodeEffect:    provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
		},
	}
	ExpectWithOffset(1, testClient.Create(ctx, dpu)).To(Succeed())

	// Set DPU status with HostNetworkReady condition set to false.
	EventuallyWithOffset(1, func(g Gomega) {
		g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpu), dpu)).To(Succeed())
		dpu.Status.Phase = provisioningv1.DPUHostNetworkConfiguration
		dpu.Status.Conditions = []metav1.Condition{
			{
				Type:               string(provisioningv1.DPUCondHostNetworkReady),
				Status:             metav1.ConditionFalse,
				Reason:             "Pending",
				LastTransitionTime: metav1.Now(),
			},
		}
		g.Expect(testClient.Status().Update(ctx, dpu)).To(Succeed())
	}, testTimeout, testInterval).Should(Succeed())

	return node, dpuNode, dpu, config
}

// setDPUHostNetworkReady sets the HostNetworkReady condition to true on the DPU.
func setDPUHostNetworkReady(ctx context.Context, dpu *provisioningv1.DPU) {
	EventuallyWithOffset(1, func(g Gomega) {
		g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(dpu), dpu)).To(Succeed())
		patch := client.MergeFrom(dpu.DeepCopy())
		dpu.Status.Phase = provisioningv1.DPUPhase(provisioningv1.PhaseReady)
		dpu.Status.Conditions = []metav1.Condition{
			{
				Type:               string(provisioningv1.DPUCondHostNetworkReady),
				Status:             metav1.ConditionTrue,
				Reason:             "Ready",
				LastTransitionTime: metav1.Now(),
			},
		}
		g.Expect(testClient.Status().Patch(ctx, dpu, patch)).To(Succeed())
	}, testTimeout, testInterval).Should(Succeed())
}

// cleanupTestObjects removes all test objects from the cluster.
func cleanupTestObjects(ctx context.Context, c client.Client) {
	allObjs := []client.Object{}
	dpuList := &provisioningv1.DPUList{}
	ExpectWithOffset(1, c.List(ctx, dpuList, client.InNamespace(testNamespace))).To(Succeed())
	for i := range dpuList.Items {
		allObjs = append(allObjs, &dpuList.Items[i])
	}

	configList := &noderesourcesv1.NodeSRIOVDevicePluginConfigList{}
	ExpectWithOffset(1, c.List(ctx, configList, client.InNamespace(testNamespace))).To(Succeed())
	for i := range configList.Items {
		allObjs = append(allObjs, &configList.Items[i])
	}

	dpuNodeList := &provisioningv1.DPUNodeList{}
	ExpectWithOffset(1, c.List(ctx, dpuNodeList, client.InNamespace(testNamespace))).To(Succeed())
	for i := range dpuNodeList.Items {
		allObjs = append(allObjs, &dpuNodeList.Items[i])
	}

	nodeList := &corev1.NodeList{}
	ExpectWithOffset(1, c.List(ctx, nodeList)).To(Succeed())
	for i := range nodeList.Items {
		allObjs = append(allObjs, &nodeList.Items[i])
	}

	cmList := &corev1.ConfigMapList{}
	ExpectWithOffset(1, c.List(ctx, cmList, client.InNamespace(testNamespace))).To(Succeed())
	for i := range cmList.Items {
		allObjs = append(allObjs, &cmList.Items[i])
	}

	podList := &corev1.PodList{}
	ExpectWithOffset(1, c.List(ctx, podList, client.InNamespace(testNamespace))).To(Succeed())
	for i := range podList.Items {
		allObjs = append(allObjs, &podList.Items[i])
	}

	ExpectWithOffset(1, testutils.CleanupAndWait(ctx, c, allObjs...)).To(Succeed())
}

var _ = Describe("handlePodInTerminalState unit tests", func() {
	var (
		ctx        context.Context
		fakeClock  *clocktesting.FakeClock
		fakeClient client.Client
		reconciler *NodeReconciler
		pod        *corev1.Pod
	)
	BeforeEach(func() {
		ctx = context.Background()
		fakeClock = clocktesting.NewFakeClock(time.Now())
		pod = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "test-namespace"},
		}
		fakeClient = fake.NewClientBuilder().
			WithScheme(scheme.Scheme).
			WithObjects(pod).
			Build()
		reconciler = &NodeReconciler{
			Client:            fakeClient,
			Namespace:         "test-namespace",
			FailedPodsBackoff: flowcontrol.NewFakeBackOff(time.Second, time.Minute, fakeClock),
		}
	})
	Context("when not in backoff", func() {
		It("should delete the pod and return empty result", func() {
			result, err := reconciler.handlePodInTerminalState(ctx, "test-node", pod)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(ctrl.Result{}))

			// Verify pod was deleted.
			err = fakeClient.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.Pod{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})
	})
	Context("when in backoff", func() {
		It("should return RequeueAfter without deleting the pod", func() {
			// Trigger backoff by calling Next.
			reconciler.FailedPodsBackoff.Next("test-node", fakeClock.Now())

			result, err := reconciler.handlePodInTerminalState(ctx, "test-node", pod)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			// Verify pod was NOT deleted.
			err = fakeClient.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.Pod{})
			Expect(err).NotTo(HaveOccurred())
		})
		It("should delete the pod after backoff expires", func() {
			// Trigger backoff.
			reconciler.FailedPodsBackoff.Next("test-node", fakeClock.Now())

			// Verify we're in backoff.
			result, err := reconciler.handlePodInTerminalState(ctx, "test-node", pod)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			// Advance clock past backoff duration.
			fakeClock.Step(result.RequeueAfter + time.Millisecond)

			// Now should be able to delete.
			result, err = reconciler.handlePodInTerminalState(ctx, "test-node", pod)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(ctrl.Result{}))

			// Verify pod was deleted.
			err = fakeClient.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.Pod{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})
	})
})
