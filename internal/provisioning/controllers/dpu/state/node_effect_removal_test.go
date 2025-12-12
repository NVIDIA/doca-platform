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
		defaultDPUName  = "dpu-node-effect-removal-test"
		defaultNodeName = "node-node-effect-removal-test"
	)

	Context("NodeEffectRemoval", func() {
		It("should transition to DPUReady when NoEffect is set", func() {
			dpu := dpuObj(defaultDPUName)
			dpu.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Action: provisioningv1.Action{NoEffect: ptr.To(true)},
			}
			dpu.Status.Phase = provisioningv1.DPUNodeEffectRemoval

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
			dpu := dpuObj(defaultDPUName)
			dpu.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Action: provisioningv1.Action{Drain: ptr.To(true)},
			}
			dpu.Status.Phase = provisioningv1.DPUNodeEffectRemoval
			// Simulate deletion by setting a non-zero deletion timestamp
			now := metav1.Now()
			dpu.DeletionTimestamp = &now

			status, err := state.NodeEffectRemoval(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
				},
			)
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUDeleting))
		})

		It("should transition to DPUReady when DPUNodeMaintenance does not exist", func() {
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
			dpu.Status.Phase = provisioningv1.DPUNodeEffectRemoval

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
				Action: provisioningv1.Action{Drain: ptr.To(true)},
			}
			createObject(dpu)
			patch = client.MergeFrom(dpu.DeepCopy())
			dpu.Status.Phase = provisioningv1.DPUNodeEffectRemoval
			Expect(k8sClient.Status().Patch(ctx, dpu, patch)).To(Succeed())

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
					CustomLabel: map[string]string{"test-label": "test-value"},
				},
			}
			createObject(dpu)
			patch = client.MergeFrom(dpu.DeepCopy())
			dpu.Status.Phase = provisioningv1.DPUNodeEffectRemoval
			Expect(k8sClient.Status().Patch(ctx, dpu, patch)).To(Succeed())

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
})
