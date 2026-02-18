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
	"time"

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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("DPU: Node Effect Removal", func() {
	var (
		defaultDPUName        = "dpu-node-effect-removal-test"
		defaultNodeName       = "node-node-effect-removal-test"
		defaultDPUNodeName    = "dpu-node-node-effect-removal-test"
		defaultDPUDeviceName  = "dpu-device-node-effect-removal-test"
		defaultDPUClusterName = "dpu-cluster-node-effect-removal-test"
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
		dpu.Status.Phase = provisioningv1.DPUNodeEffectRemoval
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

	Context("NodeEffectRemoval", func() {
		It("should transition to DPUReady when NoEffect is set", func() {
			dpu := createBasicDPU(
				nil,
				&provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}, UpgradePolicy: provisioningv1.UpgradePolicy{ApplyOnLabelChange: nil}},
			)

			By("creating a Node in the DPUCluster")
			createNodeInDPUCluster(dpu, nil, nil)

			status, err := state.NodeEffectRemoval(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
				},
			)
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUReady))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectRemoved.String()),
					HaveField("Status", metav1.ConditionTrue),
				),
			))
		})

		It("should transition to DPUDeleting when DeletionTimestamp is set", func() {
			dpu := createBasicDPU(
				nil,
				&provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}, UpgradePolicy: provisioningv1.UpgradePolicy{ApplyOnLabelChange: nil}},
			)

			// Simulate deletion by setting a non-zero deletion timestamp
			now := metav1.Now()
			dpu.DeletionTimestamp = &now

			By("creating a Node in the DPUCluster")
			createNodeInDPUCluster(dpu, nil, nil)

			status, err := state.NodeEffectRemoval(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
				},
			)
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUDeleting))
		})

		It("should transition to DPUReady when DPUNodeMaintenance does not exist", func() {
			dpu := createBasicDPU(
				nil,
				&provisioningv1.NodeEffect{Action: provisioningv1.Action{Drain: ptr.To(true)}, UpgradePolicy: provisioningv1.UpgradePolicy{ApplyOnLabelChange: nil}},
			)

			By("creating a Node in the DPUCluster")
			createNodeInDPUCluster(dpu, nil, nil)

			status, err := state.NodeEffectRemoval(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
				},
			)
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUReady))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectRemoved.String()),
					HaveField("Status", metav1.ConditionTrue),
				),
			))
		})

		It("should remove requestor from DPUNodeMaintenance and wait for deletion", func() {
			dpu := createBasicDPU(
				nil,
				&provisioningv1.NodeEffect{Action: provisioningv1.Action{Drain: ptr.To(true)}, UpgradePolicy: provisioningv1.UpgradePolicy{ApplyOnLabelChange: nil}},
			)

			By("creating a Node in the DPUCluster")
			createNodeInDPUCluster(dpu, nil, nil)

			// Create DPUNodeMaintenance with the DPU as a requestor
			dpunodemaintenanceName, err := cutil.GenerateDPUNodeMaintenanceObjectName(dpu.Spec.DPUNodeName, dpu.Spec.NodeEffect)
			Expect(err).To(Succeed())

			dpunodemaintenance := &provisioningv1.DPUNodeMaintenance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dpunodemaintenanceName,
					Namespace: testNS.Name,
				},
				Spec: provisioningv1.DPUNodeMaintenanceSpec{
					DPUNodeName: dpu.Spec.DPUNodeName,
					NodeEffect:  dpu.Spec.NodeEffect,
					Requestor:   []string{dpu.Name, "other-requestor"},
				},
			}
			createObject(dpunodemaintenance)

			By("first run, should remove requestor and return in progress")
			status, err := state.NodeEffectRemoval(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
				},
			)
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUNodeEffectRemoval))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectRemoved.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "NodeEffectRemovalInProgress"),
				),
			))

			By("verify DPU requestor has been removed from DPUNodeMaintenance")
			updatedMaintenance := &provisioningv1.DPUNodeMaintenance{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: testNS.Name,
				Name:      dpunodemaintenanceName,
			}, updatedMaintenance)).To(Succeed())
			Expect(updatedMaintenance.Spec.Requestor).NotTo(ContainElement(dpu.Name))
			Expect(updatedMaintenance.Spec.Requestor).To(ContainElement("other-requestor"))
		})

		It("should transition to Ready when DPUNodeMaintenance is deleted after removing last requestor", func() {
			dpu := createBasicDPU(
				nil,
				&provisioningv1.NodeEffect{Action: provisioningv1.Action{CustomLabel: map[string]string{"test-label": "test-value"}}},
			)

			By("creating a Node in the DPUCluster")
			createNodeInDPUCluster(dpu, nil, nil)

			// Create DPUNodeMaintenance with only the DPU as a requestor
			dpunodemaintenanceName, err := cutil.GenerateDPUNodeMaintenanceObjectName(dpu.Spec.DPUNodeName, dpu.Spec.NodeEffect)
			Expect(err).To(Succeed())

			dpunodemaintenance := &provisioningv1.DPUNodeMaintenance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dpunodemaintenanceName,
					Namespace: testNS.Name,
				},
				Spec: provisioningv1.DPUNodeMaintenanceSpec{
					DPUNodeName: dpu.Spec.DPUNodeName,
					NodeEffect:  dpu.Spec.NodeEffect,
					Requestor:   []string{dpu.Name},
				},
			}
			createObject(dpunodemaintenance)

			By("first run, should remove requestor")
			status, err := state.NodeEffectRemoval(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
				},
			)
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUNodeEffectRemoval))

			By("verify DPUNodeMaintenance requestor is now empty")
			updatedMaintenance := &provisioningv1.DPUNodeMaintenance{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: testNS.Name,
				Name:      dpunodemaintenanceName,
			}, updatedMaintenance)).To(Succeed())
			Expect(updatedMaintenance.Spec.Requestor).To(BeEmpty())
		})
	})

	Context("RemoveRequestorFromDPUNodeMaintenance", func() {
		It("should return nil when NoEffect is set", func() {
			dpu := dpuObj(defaultDPUName)
			dpu.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Action: provisioningv1.Action{NoEffect: ptr.To(true)},
			}

			err := state.RemoveRequestorFromDPUNodeMaintenance(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
				},
			)
			Expect(err).To(Succeed())
		})

		It("should return nil when DPUNodeMaintenance does not exist", func() {
			node := nodeObj(defaultNodeName)
			createObject(node)

			dpuNode := dpuNodeObj(node.Name)
			createObject(dpuNode)
			patch := client.MergeFrom(dpuNode.DeepCopy())
			dpuNode.Status.KubeNodeRef = ptr.To(node.Name)
			Expect(k8sClient.Status().Patch(ctx, dpuNode, patch)).To(Succeed())

			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Action: provisioningv1.Action{Drain: ptr.To(true)},
			}

			err := state.RemoveRequestorFromDPUNodeMaintenance(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
				},
			)
			Expect(err).To(Succeed())
		})

		It("should remove DPU from requestors list", func() {
			node := nodeObj(defaultNodeName)
			createObject(node)

			dpuNode := dpuNodeObj(node.Name)
			createObject(dpuNode)
			patch := client.MergeFrom(dpuNode.DeepCopy())
			dpuNode.Status.KubeNodeRef = ptr.To(node.Name)
			Expect(k8sClient.Status().Patch(ctx, dpuNode, patch)).To(Succeed())

			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUDeviceName = "not-used" //nolint:goconst
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					Taint: &corev1.Taint{
						Key:    "test-taint",
						Value:  "test-value",
						Effect: corev1.TaintEffectNoSchedule,
					},
				},
			}
			createObject(dpu)

			// Create DPUNodeMaintenance with the DPU as a requestor
			dpunodemaintenanceName, err := cutil.GenerateDPUNodeMaintenanceObjectName(dpu.Spec.DPUNodeName, dpu.Spec.NodeEffect)
			Expect(err).To(Succeed())

			dpunodemaintenance := &provisioningv1.DPUNodeMaintenance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dpunodemaintenanceName,
					Namespace: testNS.Name,
				},
				Spec: provisioningv1.DPUNodeMaintenanceSpec{
					DPUNodeName: dpu.Spec.DPUNodeName,
					NodeEffect:  dpu.Spec.NodeEffect,
					Requestor:   []string{dpu.Name, "requestor-1", "requestor-2"},
				},
			}
			createObject(dpunodemaintenance)

			err = state.RemoveRequestorFromDPUNodeMaintenance(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
				},
			)
			Expect(err).To(Succeed())

			By("verify DPU has been removed from requestors")
			updatedMaintenance := &provisioningv1.DPUNodeMaintenance{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: testNS.Name,
				Name:      dpunodemaintenanceName,
			}, updatedMaintenance)).To(Succeed())
			Expect(updatedMaintenance.Spec.Requestor).NotTo(ContainElement(dpu.Name))
			Expect(updatedMaintenance.Spec.Requestor).To(HaveLen(2))
			Expect(updatedMaintenance.Spec.Requestor).To(ContainElements("requestor-1", "requestor-2"))
		})

		It("should not modify requestors if DPU is not in the list", func() {
			node := nodeObj(defaultNodeName)
			createObject(node)

			dpuNode := dpuNodeObj(node.Name)
			createObject(dpuNode)
			patch := client.MergeFrom(dpuNode.DeepCopy())
			dpuNode.Status.KubeNodeRef = ptr.To(node.Name)
			Expect(k8sClient.Status().Patch(ctx, dpuNode, patch)).To(Succeed())

			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUDeviceName = "not-used" //nolint:goconst
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					CustomLabel: map[string]string{"label-key": "label-value"},
				},
			}
			createObject(dpu)

			// Create DPUNodeMaintenance without the DPU as a requestor
			dpunodemaintenanceName, err := cutil.GenerateDPUNodeMaintenanceObjectName(dpu.Spec.DPUNodeName, dpu.Spec.NodeEffect)
			Expect(err).To(Succeed())

			dpunodemaintenance := &provisioningv1.DPUNodeMaintenance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dpunodemaintenanceName,
					Namespace: testNS.Name,
				},
				Spec: provisioningv1.DPUNodeMaintenanceSpec{
					DPUNodeName: dpu.Spec.DPUNodeName,
					NodeEffect:  dpu.Spec.NodeEffect,
					Requestor:   []string{"other-requestor-1", "other-requestor-2"},
				},
			}
			createObject(dpunodemaintenance)

			err = state.RemoveRequestorFromDPUNodeMaintenance(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
				},
			)
			Expect(err).To(Succeed())

			By("verify requestors are unchanged")
			updatedMaintenance := &provisioningv1.DPUNodeMaintenance{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: testNS.Name,
				Name:      dpunodemaintenanceName,
			}, updatedMaintenance)).To(Succeed())
			Expect(updatedMaintenance.Spec.Requestor).To(HaveLen(2))
			Expect(updatedMaintenance.Spec.Requestor).To(ContainElements("other-requestor-1", "other-requestor-2"))
		})

		It("should result in empty requestors when DPU is the only requestor", func() {
			node := nodeObj(defaultNodeName)
			createObject(node)

			dpuNode := dpuNodeObj(node.Name)
			createObject(dpuNode)
			patch := client.MergeFrom(dpuNode.DeepCopy())
			dpuNode.Status.KubeNodeRef = ptr.To(node.Name)
			Expect(k8sClient.Status().Patch(ctx, dpuNode, patch)).To(Succeed())

			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUDeviceName = "not-used" //nolint:goconst
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					Hold: ptr.To(true),
				},
			}
			createObject(dpu)

			// Create DPUNodeMaintenance with only the DPU as a requestor
			dpunodemaintenanceName, err := cutil.GenerateDPUNodeMaintenanceObjectName(dpu.Spec.DPUNodeName, dpu.Spec.NodeEffect)
			Expect(err).To(Succeed())

			dpunodemaintenance := &provisioningv1.DPUNodeMaintenance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dpunodemaintenanceName,
					Namespace: testNS.Name,
				},
				Spec: provisioningv1.DPUNodeMaintenanceSpec{
					DPUNodeName: dpu.Spec.DPUNodeName,
					NodeEffect:  dpu.Spec.NodeEffect,
					Requestor:   []string{dpu.Name},
				},
			}
			createObject(dpunodemaintenance)

			err = state.RemoveRequestorFromDPUNodeMaintenance(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
				},
			)
			Expect(err).To(Succeed())

			By("verify requestors list is now empty")
			updatedMaintenance := &provisioningv1.DPUNodeMaintenance{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: testNS.Name,
				Name:      dpunodemaintenanceName,
			}, updatedMaintenance)).To(Succeed())
			Expect(updatedMaintenance.Spec.Requestor).To(BeEmpty())
		})
	})

	Context("NodeEffectRemoval Timeout", func() {
		It("should transition to DPUError when timeout expires and DPUNodeMaintenance has requestors", func() {
			dpu := createBasicDPU(
				nil,
				&provisioningv1.NodeEffect{Action: provisioningv1.Action{Drain: ptr.To(true)}, UpgradePolicy: provisioningv1.UpgradePolicy{ApplyOnLabelChange: nil}},
			)

			// Pre-set the condition with a LastTransitionTime in the past to simulate elapsed time
			dpu.Status.Conditions = []metav1.Condition{
				{
					Type:               provisioningv1.DPUCondNodeEffectRemoved.String(),
					Status:             metav1.ConditionFalse,
					Reason:             "NodeEffectRemovalInProgress",
					Message:            "node effect removal is in progress",
					LastTransitionTime: metav1.NewTime(time.Now().Add(-1 * time.Hour)),
				},
			}

			By("creating a Node in the DPUCluster")
			createNodeInDPUCluster(dpu, nil, nil)

			// Create DPUNodeMaintenance with other requestors (DPU requestor already removed in previous reconcile)
			dpunodemaintenanceName, err := cutil.GenerateDPUNodeMaintenanceObjectName(dpu.Spec.DPUNodeName, dpu.Spec.NodeEffect)
			Expect(err).To(Succeed())

			dpunodemaintenance := &provisioningv1.DPUNodeMaintenance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dpunodemaintenanceName,
					Namespace: testNS.Name,
				},
				Spec: provisioningv1.DPUNodeMaintenanceSpec{
					DPUNodeName: dpu.Spec.DPUNodeName,
					NodeEffect:  dpu.Spec.NodeEffect,
					Requestor:   []string{"other-requestor"},
				},
			}
			createObject(dpunodemaintenance)

			status, err := state.NodeEffectRemoval(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						NodeEffectRemovalTimeout: 30 * time.Minute,
					},
				},
			)
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUError))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectRemoved.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "NodeEffectRemovalTimeout"),
				),
			))
		})

		It("should not timeout when DPUNodeMaintenance is in deletion phase", func() {
			dpu := createBasicDPU(
				nil,
				&provisioningv1.NodeEffect{Action: provisioningv1.Action{Drain: ptr.To(true)}, UpgradePolicy: provisioningv1.UpgradePolicy{ApplyOnLabelChange: nil}},
			)

			// Pre-set the condition with a LastTransitionTime in the past
			dpu.Status.Conditions = []metav1.Condition{
				{
					Type:               provisioningv1.DPUCondNodeEffectRemoved.String(),
					Status:             metav1.ConditionFalse,
					Reason:             "NodeEffectRemovalInProgress",
					Message:            "node effect removal is in progress",
					LastTransitionTime: metav1.NewTime(time.Now().Add(-1 * time.Hour)),
				},
			}

			By("creating a Node in the DPUCluster")
			createNodeInDPUCluster(dpu, nil, nil)

			// Create DPUNodeMaintenance with a finalizer, then delete it to set DeletionTimestamp
			dpunodemaintenanceName, err := cutil.GenerateDPUNodeMaintenanceObjectName(dpu.Spec.DPUNodeName, dpu.Spec.NodeEffect)
			Expect(err).To(Succeed())

			dpunodemaintenance := &provisioningv1.DPUNodeMaintenance{
				ObjectMeta: metav1.ObjectMeta{
					Name:       dpunodemaintenanceName,
					Namespace:  testNS.Name,
					Finalizers: []string{"test-finalizer"},
				},
				Spec: provisioningv1.DPUNodeMaintenanceSpec{
					DPUNodeName: dpu.Spec.DPUNodeName,
					NodeEffect:  dpu.Spec.NodeEffect,
					Requestor:   []string{"other-requestor"},
				},
			}
			createObject(dpunodemaintenance)

			By("deleting the DPUNodeMaintenance to set DeletionTimestamp (finalizer prevents actual deletion)")
			Expect(k8sClient.Delete(ctx, dpunodemaintenance)).To(Succeed())

			status, err := state.NodeEffectRemoval(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						NodeEffectRemovalTimeout: 30 * time.Minute,
					},
				},
			)
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUNodeEffectRemoval))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectRemoved.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "NodeEffectRemovalInProgress"),
				),
			))
		})

		It("should not timeout when timeout duration is 0 (disabled)", func() {
			dpu := createBasicDPU(
				nil,
				&provisioningv1.NodeEffect{Action: provisioningv1.Action{Drain: ptr.To(true)}, UpgradePolicy: provisioningv1.UpgradePolicy{ApplyOnLabelChange: nil}},
			)

			// Pre-set the condition with a LastTransitionTime far in the past
			dpu.Status.Conditions = []metav1.Condition{
				{
					Type:               provisioningv1.DPUCondNodeEffectRemoved.String(),
					Status:             metav1.ConditionFalse,
					Reason:             "NodeEffectRemovalInProgress",
					Message:            "node effect removal is in progress",
					LastTransitionTime: metav1.NewTime(time.Now().Add(-24 * time.Hour)),
				},
			}

			By("creating a Node in the DPUCluster")
			createNodeInDPUCluster(dpu, nil, nil)

			dpunodemaintenanceName, err := cutil.GenerateDPUNodeMaintenanceObjectName(dpu.Spec.DPUNodeName, dpu.Spec.NodeEffect)
			Expect(err).To(Succeed())

			dpunodemaintenance := &provisioningv1.DPUNodeMaintenance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dpunodemaintenanceName,
					Namespace: testNS.Name,
				},
				Spec: provisioningv1.DPUNodeMaintenanceSpec{
					DPUNodeName: dpu.Spec.DPUNodeName,
					NodeEffect:  dpu.Spec.NodeEffect,
					Requestor:   []string{"other-requestor"},
				},
			}
			createObject(dpunodemaintenance)

			status, err := state.NodeEffectRemoval(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						NodeEffectRemovalTimeout: 0,
					},
				},
			)
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUNodeEffectRemoval))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectRemoved.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "NodeEffectRemovalInProgress"),
				),
			))
		})

		It("should not timeout when within the timeout period", func() {
			dpu := createBasicDPU(
				nil,
				&provisioningv1.NodeEffect{Action: provisioningv1.Action{Drain: ptr.To(true)}, UpgradePolicy: provisioningv1.UpgradePolicy{ApplyOnLabelChange: nil}},
			)

			// Pre-set the condition with a recent LastTransitionTime (within timeout)
			dpu.Status.Conditions = []metav1.Condition{
				{
					Type:               provisioningv1.DPUCondNodeEffectRemoved.String(),
					Status:             metav1.ConditionFalse,
					Reason:             "NodeEffectRemovalInProgress",
					Message:            "node effect removal is in progress",
					LastTransitionTime: metav1.NewTime(time.Now().Add(-5 * time.Minute)),
				},
			}

			By("creating a Node in the DPUCluster")
			createNodeInDPUCluster(dpu, nil, nil)

			dpunodemaintenanceName, err := cutil.GenerateDPUNodeMaintenanceObjectName(dpu.Spec.DPUNodeName, dpu.Spec.NodeEffect)
			Expect(err).To(Succeed())

			dpunodemaintenance := &provisioningv1.DPUNodeMaintenance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dpunodemaintenanceName,
					Namespace: testNS.Name,
				},
				Spec: provisioningv1.DPUNodeMaintenanceSpec{
					DPUNodeName: dpu.Spec.DPUNodeName,
					NodeEffect:  dpu.Spec.NodeEffect,
					Requestor:   []string{"other-requestor"},
				},
			}
			createObject(dpunodemaintenance)

			status, err := state.NodeEffectRemoval(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						NodeEffectRemovalTimeout: 30 * time.Minute,
					},
				},
			)
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUNodeEffectRemoval))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectRemoved.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "NodeEffectRemovalInProgress"),
				),
			))
		})
	})

	Context("Label Change Scenarios", func() {
		It("DPU: Ready: should handle nil ApplyOnLabelChange gracefully", func() {
			dpu := createBasicDPU(
				map[string]string{"new": "label"},
				&provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}, UpgradePolicy: provisioningv1.UpgradePolicy{ApplyOnLabelChange: nil}},
			)

			By("creating a Node in the DPUCluster")
			createNodeInDPUCluster(dpu,
				map[string]string{cutil.LastAppliedLabelsOnDPUKey: `{"old":"label"}`},
				nil,
			)

			runForEachInterface(func(installInterface provisioningv1.DPUInstallInterfaceType) {
				status, err := state.NodeEffectRemoval(ctx, dpu,
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
})
