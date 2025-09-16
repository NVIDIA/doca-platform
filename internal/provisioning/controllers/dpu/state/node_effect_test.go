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

var _ = Describe("DPU: Node Effect", func() {
	var (
		defaultDPUName  = "dpu-node-effect-test"
		defaultNodeName = "node-node-effect-test"
	)

	Context("successful cases", func() {
		It("NoEffect", func() {
			dpu := dpuObj(defaultDPUName)
			dpu.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Action: provisioningv1.Action{NoEffect: ptr.To(true)},
			}
			dpu.Status.Phase = provisioningv1.DPUNodeEffect

			runForEachInterface(func(installInterface provisioningv1.DPUInstallInterfaceType) {
				status, err := state.NodeEffect(ctx, dpu,
					&dutil.ControllerContext{
						Client: k8sClient,
					},
				)
				Expect(err).To(Succeed())
				Expect(status.Phase).To(Equal(provisioningv1.DPUInitializeInterface))
				Expect(status.Conditions).Should(ContainElements(
					And(
						HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
						HaveField("Status", metav1.ConditionTrue),
						HaveField("Reason", "NoEffect"),
					),
				))
			})
		})

		It("Drain(K8s)", func() {
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
				UpgradePolicy: provisioningv1.UpgradePolicy{
					NodeMaintenanceAdditionalRequestors: []string{"test-requestor-1", "test-requestor-2"},
				},
			}
			createObject(dpu)
			patch = client.MergeFrom(dpu.DeepCopy())
			dpu.Status.Phase = provisioningv1.DPUNodeEffect
			Expect(k8sClient.Status().Patch(ctx, dpu, patch)).To(Succeed())

			By("first run, should create dpunodemaintenance CR")
			status, err := state.NodeEffect(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
					},
				},
			)
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUNodeEffect))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "CreatedDPUNodeMaintenance"),
				),
			))
			dpunodemaintenance := &provisioningv1.DPUNodeMaintenance{}
			dpunodemaintenanceName, err := cutil.GenerateDPUNodeMaintenanceObjectName(dpu.Spec.DPUNodeName, dpu.Spec.NodeEffect)
			Expect(err).To(Succeed())
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: dpu.Namespace,
				Name:      dpunodemaintenanceName,
			}, dpunodemaintenance)).To(Succeed())
			By("verify NodeMaintenance CR has correct requestors")
			// Verify AdditionalRequestors
			Expect(dpunodemaintenance.Spec.Requestor).To(ContainElements(
				"test-requestor-1",
				"test-requestor-2",
				dpu.Name,
			))

			By("second run, should return NodeEffectProcessing")
			status, err = state.NodeEffect(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
					},
				},
			)
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUNodeEffect))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "NodeEffectProcessing"),
				),
			))

			By("when dpunodemaintenance is ready, should transition to the next phase")
			patch = client.MergeFrom(dpunodemaintenance.DeepCopy())
			dpunodemaintenance.Status.Conditions = []metav1.Condition{
				{
					Type:               string(provisioningv1.ConditionNodeEffectApplied),
					Status:             metav1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
					Reason:             "NodeEffectApplied",
				},
			}
			Expect(k8sClient.Status().Patch(ctx, dpunodemaintenance, patch)).To(Succeed())

			status, err = state.NodeEffect(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
					},
				},
			)
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUInitializeInterface))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", "NodeEffectCompleted"),
				),
			))
		})

		It("Drain(K8s) with empty requestors", func() {
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
					Drain: ptr.To(true),
				},
				UpgradePolicy: provisioningv1.UpgradePolicy{
					NodeMaintenanceAdditionalRequestors: []string{}, // Empty list,
				},
			}
			createObject(dpu)
			patch = client.MergeFrom(dpu.DeepCopy())
			dpu.Status.Phase = provisioningv1.DPUNodeEffect
			Expect(k8sClient.Status().Patch(ctx, dpu, patch)).To(Succeed())

			By("first run, should create dpunodemaintenance CR")
			status, err := state.NodeEffect(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
					},
				},
			)
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUNodeEffect))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "CreatedDPUNodeMaintenance"),
				),
			))
			dpunodemaintenance := &provisioningv1.DPUNodeMaintenance{}
			dpunodemaintenanceName, err := cutil.GenerateDPUNodeMaintenanceObjectName(dpu.Spec.DPUNodeName, dpu.Spec.NodeEffect)
			Expect(err).To(Succeed())
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: dpu.Namespace,
				Name:      dpunodemaintenanceName,
			}, dpunodemaintenance)).To(Succeed())
			By("verify NodeMaintenance CR has correct requestors")
			// should only have dpu.node in requestor
			Expect(dpunodemaintenance.Spec.Requestor).To(ContainElements(
				dpu.Name,
			))
		})

		It("CustomLabel(K8s)", func() {
			node := nodeObj(defaultNodeName)
			createObject(node)

			dpuNode := dpuNodeObj(node.Name)
			createObject(dpuNode)
			patch := client.MergeFrom(dpuNode.DeepCopy())
			dpuNode.Status.KubeNodeRef = ptr.To(node.Name)
			Expect(k8sClient.Status().Patch(ctx, dpuNode, patch)).To(Succeed())

			labelKey := "dpf.nvidia.com/test-label" //nolint:goconst
			labelValue := "test-value"              //nolint:goconst
			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUDeviceName = "not-used" //nolint:goconst
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					CustomLabel: map[string]string{
						labelKey: labelValue,
					},
				},
			}
			dpu.Status.Phase = provisioningv1.DPUNodeEffect

			status, err := state.NodeEffect(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
					},
				},
			)
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUNodeEffect))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "CreatedDPUNodeMaintenance"),
				),
			))
			dpunodemaintenance := &provisioningv1.DPUNodeMaintenance{}
			dpunodemaintenanceName, err := cutil.GenerateDPUNodeMaintenanceObjectName(dpu.Spec.DPUNodeName, dpu.Spec.NodeEffect)
			Expect(err).To(Succeed())
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: dpu.Namespace,
				Name:      dpunodemaintenanceName,
			}, dpunodemaintenance)).To(Succeed())
		})

		It("Taint(K8s)", func() {
			node := nodeObj(defaultNodeName)
			createObject(node)

			dpuNode := dpuNodeObj(node.Name)
			createObject(dpuNode)
			patch := client.MergeFrom(dpuNode.DeepCopy())
			dpuNode.Status.KubeNodeRef = ptr.To(node.Name)
			Expect(k8sClient.Status().Patch(ctx, dpuNode, patch)).To(Succeed())

			taintKey := "dpf.nvidia.com/test-label" //nolint:goconst
			taintValue := "test-value"              //nolint:goconst
			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUDeviceName = "not-used" //nolint:goconst
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					Taint: &corev1.Taint{
						Key:    taintKey,
						Value:  taintValue,
						Effect: corev1.TaintEffectNoSchedule,
					},
				},
			}
			dpu.Status.Phase = provisioningv1.DPUNodeEffect

			status, err := state.NodeEffect(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
					},
				},
			)
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUNodeEffect))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "CreatedDPUNodeMaintenance"),
				),
			))
			dpunodemaintenance := &provisioningv1.DPUNodeMaintenance{}
			dpunodemaintenanceName, err := cutil.GenerateDPUNodeMaintenanceObjectName(dpu.Spec.DPUNodeName, dpu.Spec.NodeEffect)
			Expect(err).To(Succeed())
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: dpu.Namespace,
				Name:      dpunodemaintenanceName,
			}, dpunodemaintenance)).To(Succeed())
		})

		It("CustomAction(K8s)", func() {
			node := nodeObj(defaultNodeName)
			createObject(node)

			dpuNode := dpuNodeObj(node.Name)
			createObject(dpuNode)
			patch := client.MergeFrom(dpuNode.DeepCopy())
			dpuNode.Status.KubeNodeRef = ptr.To(node.Name)
			Expect(k8sClient.Status().Patch(ctx, dpuNode, patch)).To(Succeed())
			//nolint:goconst
			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUDeviceName = "not-used" //nolint:goconst
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					CustomAction: ptr.To("configmap"),
				},
			}
			dpu.Status.Phase = provisioningv1.DPUNodeEffect

			status, err := state.NodeEffect(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
					},
				},
			)
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUNodeEffect))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "CreatedDPUNodeMaintenance"),
				),
			))
			dpunodemaintenance := &provisioningv1.DPUNodeMaintenance{}
			dpunodemaintenanceName, err := cutil.GenerateDPUNodeMaintenanceObjectName(dpu.Spec.DPUNodeName, dpu.Spec.NodeEffect)
			Expect(err).To(Succeed())
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: dpu.Namespace,
				Name:      dpunodemaintenanceName,
			}, dpunodemaintenance)).To(Succeed())
		})

		It("Hold(K8s)", func() {
			node := nodeObj(defaultNodeName)
			createObject(node)

			dpuNode := dpuNodeObj(node.Name)
			createObject(dpuNode)
			patch := client.MergeFrom(dpuNode.DeepCopy())
			dpuNode.Status.KubeNodeRef = ptr.To(node.Name)
			Expect(k8sClient.Status().Patch(ctx, dpuNode, patch)).To(Succeed())

			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUDeviceName = "not-used"
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					Hold: ptr.To(true),
				},
			}
			dpu.Status.Phase = provisioningv1.DPUNodeEffect

			status, err := state.NodeEffect(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
					},
				},
			)
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUNodeEffect))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "CreatedDPUNodeMaintenance"),
				),
			))
			dpunodemaintenance := &provisioningv1.DPUNodeMaintenance{}
			dpunodemaintenanceName, err := cutil.GenerateDPUNodeMaintenanceObjectName(dpu.Spec.DPUNodeName, dpu.Spec.NodeEffect)
			Expect(err).To(Succeed())
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: dpu.Namespace,
				Name:      dpunodemaintenanceName,
			}, dpunodemaintenance)).To(Succeed())
		})
	})

	Context("Post-Provisioning Node Effect", func() {
		It("should transition to DPUClusterConfig when PostProvisioningNodeEffect is true and NoEffect completes", func() {
			dpu := dpuObj(defaultDPUName)
			dpu.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					NoEffect: ptr.To(true),
				},
			}
			dpu.Status.Phase = provisioningv1.DPUNodeEffect
			dpu.Status.PostProvisioningNodeEffect = ptr.To(true)

			runForEachInterface(func(installInterface provisioningv1.DPUInstallInterfaceType) {
				status, err := state.NodeEffect(ctx, dpu,
					&dutil.ControllerContext{
						Client: k8sClient,
					},
				)
				Expect(err).To(Succeed())
				Expect(status.Phase).To(Equal(provisioningv1.DPUClusterConfig))
				Expect(status.PostProvisioningNodeEffect).To(BeNil())
				Expect(status.Conditions).Should(ContainElements(
					And(
						HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
						HaveField("Status", metav1.ConditionTrue),
						HaveField("Reason", "NoEffect"),
					),
				))
			})
		})

		It("should transition to DPUClusterConfig when PostProvisioningNodeEffect is true and Taint completes", func() {
			node := nodeObj(defaultNodeName)
			createObject(node)

			dpuNode := dpuNodeObj(node.Name)
			createObject(dpuNode)
			patch := client.MergeFrom(dpuNode.DeepCopy())
			dpuNode.Status.KubeNodeRef = ptr.To(node.Name)
			Expect(k8sClient.Status().Patch(ctx, dpuNode, patch)).To(Succeed())

			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUDeviceName = "not-used"
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					Taint: &corev1.Taint{
						Key:    "dpf.nvidia.com/test-taint",
						Value:  "test-value",
						Effect: corev1.TaintEffectNoSchedule,
					},
				},
			}
			dpu.Status.Phase = provisioningv1.DPUNodeEffect
			dpu.Status.PostProvisioningNodeEffect = ptr.To(true)

			By("first run, should create dpunodemaintenance CR")
			status, err := state.NodeEffect(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
					},
				},
			)
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUNodeEffect))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "CreatedDPUNodeMaintenance"),
				),
			))

			dpunodemaintenance := &provisioningv1.DPUNodeMaintenance{}
			dpunodemaintenanceName, err := cutil.GenerateDPUNodeMaintenanceObjectName(dpu.Spec.DPUNodeName, dpu.Spec.NodeEffect)
			Expect(err).To(Succeed())
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: dpu.Namespace,
				Name:      dpunodemaintenanceName,
			}, dpunodemaintenance)).To(Succeed())

			patch = client.MergeFrom(dpunodemaintenance.DeepCopy())
			dpunodemaintenance.Status.Conditions = []metav1.Condition{
				{
					Type:               string(provisioningv1.ConditionNodeEffectApplied),
					Status:             metav1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
					Reason:             "NodeEffectApplied",
				},
			}
			Expect(k8sClient.Status().Patch(ctx, dpunodemaintenance, patch)).To(Succeed())

			runForEachInterface(func(installInterface provisioningv1.DPUInstallInterfaceType) {
				status, err := state.NodeEffect(ctx, dpu,
					&dutil.ControllerContext{
						Client: k8sClient,
						Options: dutil.DPUOptions{
							DPUInstallInterface: string(installInterface),
						},
					},
				)
				Expect(err).To(Succeed())
				Expect(status.Phase).To(Equal(provisioningv1.DPUClusterConfig))
				Expect(status.PostProvisioningNodeEffect).To(BeNil())
				Expect(status.Conditions).Should(ContainElements(
					And(
						HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
						HaveField("Status", metav1.ConditionTrue),
						HaveField("Reason", "NodeEffectCompleted"),
					),
				))
			})
		})

		It("should add additional requestors when PostProvisioningNodeEffect is true and Drain", func() {
			node := nodeObj(defaultNodeName)
			createObject(node)

			dpuNode := dpuNodeObj(node.Name)
			createObject(dpuNode)
			patch := client.MergeFrom(dpuNode.DeepCopy())
			dpuNode.Status.KubeNodeRef = ptr.To(node.Name)
			Expect(k8sClient.Status().Patch(ctx, dpuNode, patch)).To(Succeed())

			dpu := dpuObj(defaultDPUName)
			dpu.Spec.DPUDeviceName = "not-used"
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					Drain: ptr.To(true),
				},
				UpgradePolicy: provisioningv1.UpgradePolicy{
					NodeMaintenanceAdditionalRequestors: []string{
						"post-provisioning-service",
						"coordination-service",
					},
				},
			}
			dpu.Status.Phase = provisioningv1.DPUNodeEffect
			dpu.Status.PostProvisioningNodeEffect = ptr.To(true)

			By("first run, should create dpunodemaintenance CR")
			status, err := state.NodeEffect(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
					},
				},
			)
			Expect(err).To(Succeed())
			Expect(status.Phase).To(Equal(provisioningv1.DPUNodeEffect))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "CreatedDPUNodeMaintenance"),
				),
			))

			dpunodemaintenance := &provisioningv1.DPUNodeMaintenance{}
			dpunodemaintenanceName, err := cutil.GenerateDPUNodeMaintenanceObjectName(dpu.Spec.DPUNodeName, dpu.Spec.NodeEffect)
			Expect(err).To(Succeed())
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: dpu.Namespace,
				Name:      dpunodemaintenanceName,
			}, dpunodemaintenance)).To(Succeed())
			By("verify NodeMaintenance CR has correct requestors")
			// Verify AdditionalRequestors
			Expect(dpunodemaintenance.Spec.Requestor).To(ContainElements(
				"post-provisioning-service",
				"coordination-service",
				dpu.Name,
			))
		})
	})
})
