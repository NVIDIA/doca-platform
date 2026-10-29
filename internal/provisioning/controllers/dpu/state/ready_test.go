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

package state_test

import (
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	dpucluster "github.com/nvidia/doca-platform/pkg/dpucluster"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("DPU: Ready", func() {
	var (
		defaultDPUName        = "dpu-ready-test"
		defaultDPUNodeName    = "dpu-node-ready-test"
		defaultDPUDeviceName  = "dpu-device-ready-test"
		defaultDPUClusterName = "dpu-cluster-ready-test"
		strTrue               = "true"

		// Common objects created in BeforeEach
		dpuDevice        *provisioningv1.DPUDevice
		dpuNode          *provisioningv1.DPUNode
		dpuCluster       *provisioningv1.DPUCluster
		dpuClusterClient client.Client
	)

	BeforeEach(func() {
		By("prepare DPUDevice CR")
		dpuDevice = dpuDeviceObj(defaultDPUDeviceName)
		createObject(dpuDevice)

		By("prepare DPUNode CR")
		dpuNode = dpuNodeObj(defaultDPUNodeName)
		dpuNode.Finalizers = []string{provisioningv1.DPUNodeFinalizer}
		dpuNode.Labels[cutil.NodeFeatureDiscoveryLabelPrefix+cutil.DPUOOBBridgeConfiguredLabel] = strTrue
		dpuNode.Spec.DPUs = []provisioningv1.DPURef{
			{
				Name: dpuDevice.Name,
			},
		}
		createObject(dpuNode)
		patch := client.MergeFrom(dpuNode.DeepCopy())
		dpuNode.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaGNOI))
		Expect(k8sClient.Status().Patch(ctx, dpuNode, patch)).To(Succeed())

		By("prepare the DPUCluster")
		dpuCluster = dpuClusterObj(defaultDPUClusterName, "kamaji")
		kamajiSecret, err := testutils.GetFakeKamajiClusterSecretFromEnvtest(*dpuCluster, cfg)
		Expect(err).ToNot(HaveOccurred())
		createObject(kamajiSecret)
		createObject(dpuCluster)
		dpuClusterClient, err = dpucluster.NewConfig(k8sClient, dpuCluster).Client(ctx)
		Expect(err).ToNot(HaveOccurred())
	})

	// Helper function to create a basic DPU with common configuration
	createBasicDPU := func(nodeLabels map[string]string, nodeEffect *provisioningv1.NodeEffect) *provisioningv1.DPU {
		dpu := dpuObj(defaultDPUName)
		dpu.Name = defaultDPUName
		dpu.Spec.PCIAddress = ptr.To("0000-00-00")
		dpu.Spec.DPUNodeName = dpuNode.Name
		dpu.Spec.DPUDeviceName = dpuDevice.Name
		dpu.Spec.Cluster.Namespace = dpuCluster.Namespace
		dpu.Spec.Cluster.Name = dpuCluster.Name
		dpu.Spec.Cluster.NodeLabels = nodeLabels
		dpu.Spec.NodeEffect = nodeEffect
		dpu.Status.Phase = provisioningv1.DPUReady
		dpu.Status.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaGNOI))
		return dpu
	}

	// Helper function to create a node in the DPUCluster
	createNodeInDPUCluster := func(dpu *provisioningv1.DPU, annotations map[string]string, addresses []corev1.NodeAddress) *corev1.Node {
		nodeInDPUCluster := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:        dpu.Name,
				Annotations: annotations,
			},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{
						Type:   corev1.NodeReady,
						Status: corev1.ConditionTrue,
					},
				},
				Addresses: addresses,
			},
		}
		Expect(dpuClusterClient.Create(ctx, nodeInDPUCluster)).To(Succeed())
		DeferCleanup(testutils.CleanupAndWait, ctx, dpuClusterClient, nodeInDPUCluster)
		return nodeInDPUCluster
	}

	Context("Basic Functionality", func() {
		It("DPU: Ready: should stay in Ready state when no label changes", func() {
			dpu := createBasicDPU(
				map[string]string{"existing": "label"},
				&provisioningv1.NodeEffect{NoEffect: ptr.To(true)},
			)

			By("creating a Node in the DPUCluster")
			createNodeInDPUCluster(dpu,
				map[string]string{cutil.LastAppliedLabelsOnDPUKey: `{"existing":"label"}`},
				[]corev1.NodeAddress{
					{Type: corev1.NodeInternalIP, Address: "192.168.1.1"},
				},
			)

			runForEachInterface(func(installInterface provisioningv1.DPUInstallInterfaceType) {
				status, err := state.Ready(ctx, dpu,
					&dutil.ControllerContext{
						Client: k8sClient,
						Options: dutil.DPUOptions{
							DPUInstallInterface: string(installInterface),
						},
					},
				)
				Expect(err).To(Succeed())
				Expect(status.Phase).To(Equal(provisioningv1.DPUReady))
				Expect(status.Conditions).Should(ContainElements(
					And(
						HaveField("Type", provisioningv1.DPUCondReady.String()),
						HaveField("Status", metav1.ConditionTrue),
						HaveField("Reason", "DPUReady"),
					),
				))
			})
		})

		It("DPU: Ready: should transition to DPUClusterConfig when labels change and ApplyOnLabelChange is false", func() {
			dpu := createBasicDPU(
				map[string]string{"key1": "value1", "key2": "value2"},
				&provisioningv1.NodeEffect{NoEffect: ptr.To(true)},
			)

			By("creating a Node in the DPUCluster")
			createNodeInDPUCluster(dpu, nil, nil)

			runForEachInterface(func(installInterface provisioningv1.DPUInstallInterfaceType) {
				status, err := state.Ready(ctx, dpu,
					&dutil.ControllerContext{
						Client: k8sClient,
						Options: dutil.DPUOptions{
							DPUInstallInterface: string(installInterface),
						},
					},
				)
				Expect(err).To(Succeed())
				Expect(status.Phase).To(Equal(provisioningv1.DPUClusterConfig))
			})
		})

		It("DPU: Ready: should transition to DPUNodeEffect when labels change and ApplyOnLabelChange is true", func() {
			dpu := createBasicDPU(
				map[string]string{"new": "label"},
				&provisioningv1.NodeEffect{NoEffect: ptr.To(true), ApplyOnLabelChange: ptr.To(true)},
			)

			By("creating a Node in the DPUCluster")
			createNodeInDPUCluster(dpu,
				map[string]string{cutil.LastAppliedLabelsOnDPUKey: `{"old":"label"}`},
				nil,
			)

			runForEachInterface(func(installInterface provisioningv1.DPUInstallInterfaceType) {
				status, err := state.Ready(ctx, dpu,
					&dutil.ControllerContext{
						Client: k8sClient,
						Options: dutil.DPUOptions{
							DPUInstallInterface: string(installInterface),
						},
					},
				)
				Expect(err).To(Succeed())
				Expect(status.Phase).To(Equal(provisioningv1.DPUNodeEffect))
				Expect(status.PostProvisioningNodeEffect).To(Equal(ptr.To(true)))
			})
		})
	})

	Context("Label Change Scenarios", func() {
		It("DPU: Ready: should handle nil ApplyOnLabelChange gracefully", func() {
			dpu := createBasicDPU(
				map[string]string{"new": "label"},
				&provisioningv1.NodeEffect{NoEffect: ptr.To(true), ApplyOnLabelChange: nil},
			)

			By("creating a Node in the DPUCluster")
			createNodeInDPUCluster(dpu,
				map[string]string{cutil.LastAppliedLabelsOnDPUKey: `{"old":"label"}`},
				nil,
			)

			runForEachInterface(func(installInterface provisioningv1.DPUInstallInterfaceType) {
				status, err := state.Ready(ctx, dpu,
					&dutil.ControllerContext{
						Client: k8sClient,
						Options: dutil.DPUOptions{
							DPUInstallInterface: string(installInterface),
						},
					},
				)
				Expect(err).To(Succeed())
				Expect(status.Phase).To(Equal(provisioningv1.DPUClusterConfig))
			})
		})

		It("DPU: Ready: should handle empty label changes", func() {
			dpu := createBasicDPU(
				map[string]string{},
				&provisioningv1.NodeEffect{NoEffect: ptr.To(true), ApplyOnLabelChange: ptr.To(true)},
			)

			By("creating a Node in the DPUCluster")
			createNodeInDPUCluster(dpu,
				map[string]string{cutil.LastAppliedLabelsOnDPUKey: `{}`},
				nil,
			)

			runForEachInterface(func(installInterface provisioningv1.DPUInstallInterfaceType) {
				status, err := state.Ready(ctx, dpu,
					&dutil.ControllerContext{
						Client: k8sClient,
						Options: dutil.DPUOptions{
							DPUInstallInterface: string(installInterface),
						},
					},
				)
				Expect(err).To(Succeed())
				Expect(status.Phase).To(Equal(provisioningv1.DPUReady))
			})
		})

		It("DPU: Ready: should handle malformed last applied labels annotation", func() {
			dpu := createBasicDPU(
				map[string]string{"new": "label"},
				&provisioningv1.NodeEffect{NoEffect: ptr.To(true), ApplyOnLabelChange: ptr.To(true)},
			)

			By("creating a Node in the DPUCluster")
			createNodeInDPUCluster(dpu,
				map[string]string{cutil.LastAppliedLabelsOnDPUKey: `invalid json`},
				nil,
			)

			runForEachInterface(func(installInterface provisioningv1.DPUInstallInterfaceType) {
				status, err := state.Ready(ctx, dpu,
					&dutil.ControllerContext{
						Client: k8sClient,
						Options: dutil.DPUOptions{
							DPUInstallInterface: string(installInterface),
						},
					},
				)
				Expect(err).To(HaveOccurred())
				Expect(status.Phase).To(Equal(provisioningv1.DPUReady))
			})
		})

		It("DPU: Ready: should handle JSON unmarshaling error in last applied labels", func() {
			dpu := createBasicDPU(
				map[string]string{"new": "label"},
				&provisioningv1.NodeEffect{NoEffect: ptr.To(true), ApplyOnLabelChange: ptr.To(true)},
			)

			By("creating a Node in the DPUCluster with malformed JSON")
			createNodeInDPUCluster(dpu,
				map[string]string{cutil.LastAppliedLabelsOnDPUKey: `{"key": "value", "unclosed":}`},
				nil,
			)

			runForEachInterface(func(installInterface provisioningv1.DPUInstallInterfaceType) {
				status, err := state.Ready(ctx, dpu,
					&dutil.ControllerContext{
						Client: k8sClient,
						Options: dutil.DPUOptions{
							DPUInstallInterface: string(installInterface),
						},
					},
				)
				Expect(err).To(HaveOccurred())
				Expect(status.Phase).To(Equal(provisioningv1.DPUReady))
			})
		})

		It("DPU: Ready: should handle empty NodeLabels vs non-empty last applied", func() {
			dpu := createBasicDPU(
				map[string]string{}, // empty NodeLabels
				&provisioningv1.NodeEffect{NoEffect: ptr.To(true)},
			)

			By("creating a Node in the DPUCluster with existing labels")
			createNodeInDPUCluster(dpu,
				map[string]string{cutil.LastAppliedLabelsOnDPUKey: `{"existing":"label"}`},
				nil,
			)

			runForEachInterface(func(installInterface provisioningv1.DPUInstallInterfaceType) {
				status, err := state.Ready(ctx, dpu,
					&dutil.ControllerContext{
						Client: k8sClient,
						Options: dutil.DPUOptions{
							DPUInstallInterface: string(installInterface),
						},
					},
				)
				Expect(err).To(Succeed())
				Expect(status.Phase).To(Equal(provisioningv1.DPUClusterConfig))
			})
		})

		It("DPU: Ready: should handle non-empty NodeLabels vs empty last applied", func() {
			dpu := createBasicDPU(
				map[string]string{"new": "label"}, // non-empty NodeLabels
				&provisioningv1.NodeEffect{NoEffect: ptr.To(true)},
			)

			By("creating a Node in the DPUCluster with empty last applied labels")
			createNodeInDPUCluster(dpu,
				map[string]string{cutil.LastAppliedLabelsOnDPUKey: `{}`},
				nil,
			)

			runForEachInterface(func(installInterface provisioningv1.DPUInstallInterfaceType) {
				status, err := state.Ready(ctx, dpu,
					&dutil.ControllerContext{
						Client: k8sClient,
						Options: dutil.DPUOptions{
							DPUInstallInterface: string(installInterface),
						},
					},
				)
				Expect(err).To(Succeed())
				Expect(status.Phase).To(Equal(provisioningv1.DPUClusterConfig))
			})
		})
	})

	Context("Error Handling", func() {
		It("DPU: Ready: should handle DPU cluster not found", func() {
			// Create DPU with non-existent cluster
			dpu := dpuObj(defaultDPUName)
			dpu.Spec.Cluster = provisioningv1.K8sCluster{
				Name:      "non-existent-cluster",
				Namespace: testNS.Name,
			}
			dpu.Spec.NodeEffect = &provisioningv1.NodeEffect{NoEffect: ptr.To(true)}
			dpu.Status.Phase = provisioningv1.DPUReady

			runForEachInterface(func(installInterface provisioningv1.DPUInstallInterfaceType) {
				status, err := state.Ready(ctx, dpu,
					&dutil.ControllerContext{
						Client: k8sClient,
						Options: dutil.DPUOptions{
							DPUInstallInterface: string(installInterface),
						},
					},
				)
				Expect(err).To(HaveOccurred())
				Expect(status.Phase).To(Equal(provisioningv1.DPUReady))
				Expect(status.Conditions).Should(ContainElements(
					And(
						HaveField("Type", provisioningv1.DPUCondReady.String()),
						HaveField("Status", metav1.ConditionFalse),
						HaveField("Reason", "DPUClusterNotFound"),
					),
				))
			})
		})

		It("DPU: Ready: should handle node not found", func() {
			dpu := createBasicDPU(
				map[string]string{},
				&provisioningv1.NodeEffect{NoEffect: ptr.To(true)},
			)

			// Note: No node is created in the DPUCluster to simulate "node not found"

			runForEachInterface(func(installInterface provisioningv1.DPUInstallInterfaceType) {
				status, err := state.Ready(ctx, dpu,
					&dutil.ControllerContext{
						Client: k8sClient,
						Options: dutil.DPUOptions{
							DPUInstallInterface: string(installInterface),
						},
					},
				)
				Expect(err).To(HaveOccurred())
				Expect(status.Phase).To(Equal(provisioningv1.DPUReady))
				Expect(status.Conditions).Should(ContainElements(
					And(
						HaveField("Type", provisioningv1.DPUCondReady.String()),
						HaveField("Status", metav1.ConditionFalse),
						HaveField("Reason", "NodeNotFound"),
					),
				))
			})
		})

		It("DPU: Ready: should handle node not ready", func() {
			dpu := createBasicDPU(
				map[string]string{},
				&provisioningv1.NodeEffect{NoEffect: ptr.To(true)},
			)

			By("creating a Node in the DPUCluster that is not ready")
			nodeInDPUCluster := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: dpu.Name,
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionFalse,
						},
					},
				},
			}
			Expect(dpuClusterClient.Create(ctx, nodeInDPUCluster)).To(Succeed())
			DeferCleanup(testutils.CleanupAndWait, ctx, dpuClusterClient, nodeInDPUCluster)

			runForEachInterface(func(installInterface provisioningv1.DPUInstallInterfaceType) {
				status, err := state.Ready(ctx, dpu,
					&dutil.ControllerContext{
						Client: k8sClient,
						Options: dutil.DPUOptions{
							DPUInstallInterface: string(installInterface),
						},
					},
				)
				Expect(err).To(HaveOccurred())
				Expect(status.Phase).To(Equal(provisioningv1.DPUReady))
				Expect(status.Conditions).Should(ContainElements(
					And(
						HaveField("Type", provisioningv1.DPUCondReady.String()),
						HaveField("Status", metav1.ConditionFalse),
						HaveField("Reason", "NodeNotReady"),
					),
				))
			})
		})
	})

	Context("Address Update Logic", func() {
		It("DPU: Ready: should update addresses when they change", func() {
			dpu := createBasicDPU(
				map[string]string{},
				&provisioningv1.NodeEffect{NoEffect: ptr.To(true)},
			)

			By("creating a Node in the DPUCluster with addresses")
			createNodeInDPUCluster(dpu, nil, []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: "192.168.1.1"},
				{Type: corev1.NodeHostName, Address: "test-node"},
			})

			runForEachInterface(func(installInterface provisioningv1.DPUInstallInterfaceType) {
				status, err := state.Ready(ctx, dpu,
					&dutil.ControllerContext{
						Client: k8sClient,
						Options: dutil.DPUOptions{
							DPUInstallInterface: string(installInterface),
						},
					},
				)
				Expect(err).To(Succeed())
				Expect(status.Phase).To(Equal(provisioningv1.DPUReady))
				Expect(status.Addresses).To(HaveLen(2))
				Expect(status.Addresses).Should(ContainElements(
					And(
						HaveField("Type", corev1.NodeInternalIP),
						HaveField("Address", "192.168.1.1"),
					),
					And(
						HaveField("Type", corev1.NodeHostName),
						HaveField("Address", "test-node"),
					),
				))
			})
		})

		It("DPU: Ready: should not update addresses when they are the same", func() {
			dpu := createBasicDPU(
				map[string]string{},
				&provisioningv1.NodeEffect{NoEffect: ptr.To(true)},
			)
			// Set initial addresses in status
			dpu.Status.Addresses = []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: "192.168.1.1"},
				{Type: corev1.NodeHostName, Address: "test-node"},
			}

			By("creating a Node in the DPUCluster with same addresses")
			createNodeInDPUCluster(dpu, nil, []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: "192.168.1.1"},
				{Type: corev1.NodeHostName, Address: "test-node"},
			})

			runForEachInterface(func(installInterface provisioningv1.DPUInstallInterfaceType) {
				status, err := state.Ready(ctx, dpu,
					&dutil.ControllerContext{
						Client: k8sClient,
						Options: dutil.DPUOptions{
							DPUInstallInterface: string(installInterface),
						},
					},
				)
				Expect(err).To(Succeed())
				Expect(status.Phase).To(Equal(provisioningv1.DPUReady))
				Expect(status.Addresses).To(HaveLen(2))
				Expect(status.Addresses).Should(ContainElements(
					And(
						HaveField("Type", corev1.NodeInternalIP),
						HaveField("Address", "192.168.1.1"),
					),
					And(
						HaveField("Type", corev1.NodeHostName),
						HaveField("Address", "test-node"),
					),
				))
			})
		})
	})

	Context("Edge Cases", func() {
		It("DPU: Ready: should handle missing last applied labels annotation", func() {
			dpu := createBasicDPU(
				map[string]string{"new": "label"},
				&provisioningv1.NodeEffect{NoEffect: ptr.To(true), ApplyOnLabelChange: ptr.To(true)},
			)

			By("creating a Node in the DPUCluster without last applied labels annotation")
			createNodeInDPUCluster(dpu, nil, nil)

			runForEachInterface(func(installInterface provisioningv1.DPUInstallInterfaceType) {
				status, err := state.Ready(ctx, dpu,
					&dutil.ControllerContext{
						Client: k8sClient,
						Options: dutil.DPUOptions{
							DPUInstallInterface: string(installInterface),
						},
					},
				)
				Expect(err).To(Succeed())
				Expect(status.Phase).To(Equal(provisioningv1.DPUNodeEffect))
				Expect(status.PostProvisioningNodeEffect).To(Equal(ptr.To(true)))
			})
		})
	})
})
