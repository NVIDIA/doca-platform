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
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	maintenancev1alpha1 "github.com/Mellanox/maintenance-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
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
				NoEffect: ptr.To(true),
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
						HaveField("Reason", provisioningv1.DPUCondNodeEffectReady.String()),
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
				Drain: ptr.To(true),
			}
			createObject(dpu)
			patch = client.MergeFrom(dpu.DeepCopy())
			dpu.Status.Phase = provisioningv1.DPUNodeEffect
			Expect(k8sClient.Status().Patch(ctx, dpu, patch)).To(Succeed())
			By("first run, should create node maintenance CR")
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
			maintenance := &maintenancev1alpha1.NodeMaintenance{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: dpu.Namespace,
				Name:      dpu.Spec.DPUNodeName,
			}, maintenance)).To(Succeed())

			By("second run, should return node maintenance is not ready")
			status, err = state.NodeEffect(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
					},
				},
			)
			Expect(err).To(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUNodeEffect))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "NodeMaintenanceIsNotReady"),
				),
			))

			By("when node maintenance is ready, should transition to the next phase")
			patch = client.MergeFrom(maintenance.DeepCopy())
			maintenance.Status.Conditions = []metav1.Condition{
				{
					Type:               maintenancev1alpha1.ConditionTypeReady,
					Status:             metav1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
					Reason:             "ANonEmptyReason",
				},
			}
			Expect(k8sClient.Status().Patch(ctx, maintenance, patch)).To(Succeed())
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
					HaveField("Reason", provisioningv1.DPUCondNodeEffectReady.String()),
				),
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
				CustomLabel: map[string]string{
					labelKey: labelValue,
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
			Expect(status.Phase).To(Equal(provisioningv1.DPUInitializeInterface))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondNodeEffectReady.String()),
				),
			))
			fetchedDPUNode := &provisioningv1.DPUNode{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: dpu.Namespace,
				Name:      dpu.Spec.DPUNodeName,
			}, fetchedDPUNode)).To(Succeed())
			Expect(fetchedDPUNode.Labels).To(HaveKeyWithValue(labelKey, labelValue))
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
				Taint: &corev1.Taint{
					Key:    taintKey,
					Value:  taintValue,
					Effect: corev1.TaintEffectNoSchedule,
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
			Expect(status.Phase).To(Equal(provisioningv1.DPUInitializeInterface))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondNodeEffectReady.String()),
				),
			))
			fetchedNode := &corev1.Node{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: node.Name,
			}, fetchedNode)).To(Succeed())
			Expect(fetchedNode.Spec.Taints).To(ContainElement(corev1.Taint{
				Key:    taintKey,
				Value:  taintValue,
				Effect: corev1.TaintEffectNoSchedule,
			}))
		})

		It("Taint(K8s) with multiple taints", func() {
			taintKey := "dpf.nvidia.com/test-label" //nolint:goconst
			taintValue := "test-value"              //nolint:goconst
			node := nodeObj(defaultNodeName)
			node.Spec.Taints = []corev1.Taint{
				{
					Key:    taintKey,
					Value:  taintValue,
					Effect: corev1.TaintEffectNoSchedule,
				},
			}
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
				Taint: &corev1.Taint{
					Key:    taintKey,
					Value:  "another-value",
					Effect: corev1.TaintEffectNoSchedule,
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
			Expect(status.Phase).To(Equal(provisioningv1.DPUInitializeInterface))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
					HaveField("Status", metav1.ConditionTrue),
					HaveField("Reason", provisioningv1.DPUCondNodeEffectReady.String()),
				),
			))

			By("should not modify the original taints on the Node")
			fetchedNode := &corev1.Node{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: node.Name,
			}, fetchedNode)).To(Succeed())
			Expect(fetchedNode.Spec.Taints).To(ContainElement(corev1.Taint{
				Key:    taintKey,
				Value:  taintValue,
				Effect: corev1.TaintEffectNoSchedule,
			}))
		})

		It("CustomAction(K8s)", func() {
			yml := []byte(fmt.Sprintf(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: dpu-custom-action
  namespace: %s
data:
  pod.yaml: |-
    apiVersion: v1
    kind: Pod
    metadata:
      name: dpf-test-pod
    spec:
      restartPolicy: "Never"
      nodeSelector: 
        noderole: control-plane
      containers:
        - name: dpp-test-container
          image: alpine
          command: ["/bin/sh"]
          args: ["-c", "echo 'DPF custom action' | tee /tmp/sucess "]`, testNS.Name))
			configMap := &corev1.ConfigMap{}
			err := yaml.UnmarshalStrict(yml, configMap)
			createObject(configMap)

			Expect(err).To(Succeed())
			node := nodeObj(defaultNodeName)
			createObject(node)

			dpuNode := dpuNodeObj(node.Name)
			createObject(dpuNode)
			patch := client.MergeFrom(dpuNode.DeepCopy())
			dpuNode.Status.KubeNodeRef = ptr.To(node.Name)
			Expect(k8sClient.Status().Patch(ctx, dpuNode, patch)).To(Succeed())

			dpu := dpuObj(defaultDPUName)
			dpu.CreationTimestamp = metav1.Now()
			dpu.Spec.DPUDeviceName = "not-used"
			dpu.Spec.DPUNodeName = dpuNode.Name
			dpu.Spec.NodeEffect = &provisioningv1.NodeEffect{
				CustomAction: ptr.To(configMap.Name),
			}
			dpu.Status.Phase = provisioningv1.DPUNodeEffect

			jobName := state.GetCustomActionJobName(dpu.Spec.NodeEffect, dpu)
			DeferCleanup(func() {
				client.IgnoreNotFound(k8sClient.Delete(ctx, &batchv1.Job{ //nolint:errcheck
					ObjectMeta: metav1.ObjectMeta{
						Namespace: dpu.Namespace,
						Name:      jobName,
					},
				}))
			})

			By("first run, should create job")
			status, err := state.NodeEffect(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
					},
				},
			)
			Expect(err).To(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUNodeEffect))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "JobIsStarted"),
				),
			))
			fetchedJob := &batchv1.Job{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: dpu.Namespace,
				Name:      jobName,
			}, fetchedJob)).To(Succeed())

			By("second run, should return job is not finished")
			status, err = state.NodeEffect(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
					},
				},
			)
			Expect(err).To(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUNodeEffect))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "JobIsNotFinished"),
				),
			))
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
				Hold: ptr.To(true),
			}
			createObject(dpu)
			patch = client.MergeFrom(dpu.DeepCopy())
			dpu.Status.Phase = provisioningv1.DPUNodeEffect
			Expect(k8sClient.Status().Patch(ctx, dpu, patch)).To(Succeed())

			By("first run, should add hold node effect annotation to the DPU")
			status, err := state.NodeEffect(ctx, dpu,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
					},
				},
			)
			Expect(err).To(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUNodeEffect))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "ExternalNodeEffect"),
				),
			))
			fetchedDPU := &provisioningv1.DPU{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: dpu.Namespace,
				Name:      dpu.Name,
			}, fetchedDPU)).To(Succeed())
			Expect(fetchedDPU.Annotations).To(HaveKeyWithValue(cutil.HoldNodeEffectKey, "true"))

			By("second run, should wait until annotation value is false")
			status, err = state.NodeEffect(ctx, fetchedDPU,
				&dutil.ControllerContext{
					Client: k8sClient,
					Options: dutil.DPUOptions{
						DPUInstallInterface: string(provisioningv1.InstallViaGNOI),
					},
				},
			)
			Expect(err).To(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUNodeEffect))
			Expect(status.Conditions).Should(ContainElements(
				And(
					HaveField("Type", provisioningv1.DPUCondNodeEffectReady.String()),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "ExternalNodeEffect"),
				),
			))
			fetchedDPU = &provisioningv1.DPU{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: dpu.Namespace,
				Name:      dpu.Name,
			}, fetchedDPU)).To(Succeed())
			Expect(fetchedDPU.Annotations).To(HaveKeyWithValue(cutil.HoldNodeEffectKey, "true"))

			By("update the annotation so the process can proceed")
			patch = client.MergeFrom(fetchedDPU.DeepCopy())
			fetchedDPU.Annotations[cutil.HoldNodeEffectKey] = "false"
			Expect(k8sClient.Patch(ctx, fetchedDPU, patch)).To(Succeed())
			fetchedDPU = &provisioningv1.DPU{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: dpu.Namespace,
				Name:      dpu.Name,
			}, fetchedDPU)).To(Succeed())
			Expect(fetchedDPU.Annotations).To(HaveKeyWithValue(cutil.HoldNodeEffectKey, "false"))

			By("third run, should proceed to the next phase")
			status, err = state.NodeEffect(ctx, fetchedDPU,
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
					HaveField("Reason", provisioningv1.DPUCondNodeEffectReady.String()),
				),
			))
		})
	})
})
