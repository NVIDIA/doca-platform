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

package controller

import (
	"context"
	"fmt"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	operatorcontroller "github.com/nvidia/doca-platform/internal/operator/controllers"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	nvidiaNodeMaintenancev1 "github.com/Mellanox/maintenance-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("DPUNodeMaintenance", func() {

	const (
		DefaultNS                 = "dpunodemaintenance-ns-test"
		DefaultDPUNodeMaintenance = "dpunodemaintenance-test"
		DefaultDPFOperatorConfig  = "operator-config-test"
	)

	var (
		testNS        *corev1.Namespace
		testNode      *corev1.Node
		testDPUNode   *provisioningv1.DPUNode
		nodeName      string
		dpuDeviceName string
	)

	var createDPFOperatorConfig = func(ctx context.Context, name string) *operatorv1.DPFOperatorConfig {
		dpfOperatorConfig := &operatorv1.DPFOperatorConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace,
			},
			Spec: operatorv1.DPFOperatorConfigSpec{
				ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
					BFBPersistentVolumeClaimName: ptr.To("foo-pvc"),
				},
			},
		}

		Expect(k8sClient.Create(ctx, dpfOperatorConfig)).NotTo(HaveOccurred())
		return dpfOperatorConfig
	}

	var createNode = func(ctx context.Context, name string) *corev1.Node {
		node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name}}
		Expect(k8sClient.Create(ctx, node)).NotTo(HaveOccurred())
		return node
	}

	var createDPUNode = func(ctx context.Context, name string, dpuDeviceName string) *provisioningv1.DPUNode {
		dpuNode := &provisioningv1.DPUNode{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: testNS.Name,
				Labels: map[string]string{
					cutil.NodeFeatureDiscoveryLabelPrefix + cutil.DPUOOBBridgeConfiguredLabel: "true",
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: operatorv1.GroupVersion.String(),
						Kind:       operatorv1.DPFOperatorConfigKind,
						Name:       "fake-dpf-operator-config",
						UID:        "fake-uid-123",
						Controller: ptr.To(false),
					},
				},
			},
			Spec: provisioningv1.DPUNodeSpec{
				NodeRebootMethod: &provisioningv1.NodeRebootMethod{
					GNOI: &provisioningv1.GNOI{},
				},
				NodeDMSAddress: &provisioningv1.DMSAddress{IP: "1.1.1.1", Port: 1234},
				DPUs: []provisioningv1.DPURef{
					{Name: dpuDeviceName},
				},
			},
		}
		Expect(k8sClient.Create(ctx, dpuNode)).NotTo(HaveOccurred())
		latestDPUNode := &provisioningv1.DPUNode{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dpuNode), latestDPUNode)).To(Succeed())
		orig := latestDPUNode.DeepCopy()
		dpuNode.Status.KubeNodeRef = ptr.To(name)
		Expect(k8sClient.Status().Patch(ctx, dpuNode, client.MergeFrom(orig))).To(Succeed())
		return dpuNode
	}

	BeforeEach(func() {
		By("creating the namespaces")
		// Notes:
		// 1. Namespace usage limitation:
		// EnvTest does not support namespace deletion. Deleting a namespace will seem to succeed,
		// but the namespace will just be put in a Terminating state, and never actually be reclaimed.
		// See: https://book.kubebuilder.io/reference/envtest.html#namespace-usage-limitation
		// 2. the value in GenerateName is not defined as a constant intentionally,
		// because it shouldn't be referenced directly.
		// 3. testNS is the only way to reference the namespace in the test.
		// 4. always create a new namespace for each test, never reuse an existing namespace
		testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: DefaultNS}}
		Eventually(func() error {
			return k8sClient.Create(ctx, testNS)
		}).WithTimeout(10 * time.Second).Should(Succeed())

		// Generate unique names for each test run to avoid conflicts
		nodeName = fmt.Sprintf("node-test-%s", utilrand.String(5))
		dpuDeviceName = fmt.Sprintf("dpu-test-device-%s", utilrand.String(5))

		By("creating the dpfoperatorconfig")
		_ = createDPFOperatorConfig(ctx, DefaultDPFOperatorConfig)

		By("creating the node")
		testNode = createNode(ctx, nodeName)

		By("creating the dpuNode")
		testDPUNode = createDPUNode(ctx, nodeName, dpuDeviceName)
	})

	AfterEach(func() {
		By("deleting the dpuNode")
		Expect(k8sClient.Delete(ctx, testDPUNode)).To(Succeed())

		By("deleting the node")
		Expect(k8sClient.Delete(ctx, testNode)).To(Succeed())

		By("deleting the dpfoperatorconfig")
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &operatorv1.DPFOperatorConfig{ObjectMeta: metav1.ObjectMeta{Namespace: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace, Name: DefaultDPFOperatorConfig}}))).To(Succeed())

		By("deleting the namespace")
		Expect(k8sClient.Delete(ctx, testNS)).To(Succeed())
	})

	Context("obj test context", func() {

		It("DPUNodeMaintenance: fail to create with name exceeding the maximum length", func() {
			By("dpunodemaintenance object")
			obj := &provisioningv1.DPUNodeMaintenance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      utilrand.String(188),
					Namespace: testNS.Name,
				},
				Spec: provisioningv1.DPUNodeMaintenanceSpec{},
			}
			Expect(k8sClient.Create(ctx, obj)).To(HaveOccurred())
		})

		It("DPUNodeMaintenance: drain node effect should create nvidia node maintenance obj", func() {
			By("creating the dpunodemaintenance object")
			obj := &provisioningv1.DPUNodeMaintenance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      DefaultDPUNodeMaintenance,
					Namespace: testNS.Name,
				},
				Spec: provisioningv1.DPUNodeMaintenanceSpec{
					DPUNodeName: nodeName,
					NodeEffect: &provisioningv1.NodeEffect{
						Action: provisioningv1.Action{
							Drain: ptr.To(true),
						},
					},
					Requestor: []string{"test-requestor"},
				},
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())

			By("getting nvidia node maintenance obj")
			fetchedNodemaintenance := &nvidiaNodeMaintenancev1.NodeMaintenance{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: nodeName}, fetchedNodemaintenance)).To(Succeed())
			}, 10*time.Second).Should(Succeed())

			// NOTE: DPUNodeMaintenance Deletion Pattern
			// The DPUNodeMaintenance controller requires the Spec.Requestor field to be empty
			// before it will process deletion and remove the finalizer. This is by design:
			// - If Requestor is NOT empty, the controller calls reconcile() instead of reconcileDelete()
			// - Even in reconcileDelete(), it skips removal logic if Requestor is set
			// - This prevents accidental deletion while requestors are still active
			By("clearing requestor to trigger deletion")
			Eventually(func(g Gomega) {
				fetchedObj := &provisioningv1.DPUNodeMaintenance{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), fetchedObj)).To(Succeed())
				orig := fetchedObj.DeepCopy()
				fetchedObj.Spec.Requestor = []string{}
				g.Expect(k8sClient.Patch(ctx, fetchedObj, client.MergeFrom(orig))).To(Succeed())
			}, 10*time.Second).Should(Succeed())

			By("verifying nvidia node maintenance is removed")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: nodeName}, &nvidiaNodeMaintenancev1.NodeMaintenance{})
				g.Expect(client.IgnoreNotFound(err)).To(Succeed())
				g.Expect(err).To(HaveOccurred())
			}, 10*time.Second).Should(Succeed())

			By("verifying dpunodemaintenance is deleted")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), &provisioningv1.DPUNodeMaintenance{})
				g.Expect(client.IgnoreNotFound(err)).To(Succeed())
				g.Expect(err).To(HaveOccurred())
			}, 10*time.Second).Should(Succeed())
		})

		It("DPUNodeMaintenance: custom label effect should add label on node obj", func() {
			By("creating the dpunodemaintenance object")
			obj := &provisioningv1.DPUNodeMaintenance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      DefaultDPUNodeMaintenance,
					Namespace: testNS.Name,
				},
				Spec: provisioningv1.DPUNodeMaintenanceSpec{
					DPUNodeName: nodeName,
					NodeEffect: &provisioningv1.NodeEffect{
						Action: provisioningv1.Action{
							CustomLabel: map[string]string{"test-label": "test-value"},
						},
					},
					Requestor: []string{"test-requestor"},
				},
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())

			By("getting the node obj")
			fetchedNode := &corev1.Node{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: nodeName}, fetchedNode)).To(Succeed())
				g.Expect(fetchedNode.Labels["test-label"]).To(Equal("test-value"))
			}, 10*time.Second).Should(Succeed())

			// NOTE: DPUNodeMaintenance Deletion Pattern
			// The DPUNodeMaintenance controller requires the Spec.Requestor field to be empty
			// before it will process deletion and remove the finalizer. This is by design:
			// - If Requestor is NOT empty, the controller calls reconcile() instead of reconcileDelete()
			// - Even in reconcileDelete(), it skips removal logic if Requestor is set
			// - This prevents accidental deletion while requestors are still active
			By("clearing requestor to trigger deletion")
			Eventually(func(g Gomega) {
				fetchedObj := &provisioningv1.DPUNodeMaintenance{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), fetchedObj)).To(Succeed())
				orig := fetchedObj.DeepCopy()
				fetchedObj.Spec.Requestor = []string{}
				g.Expect(k8sClient.Patch(ctx, fetchedObj, client.MergeFrom(orig))).To(Succeed())
			}, 10*time.Second).Should(Succeed())

			By("verifying label is removed from node")
			Eventually(func(g Gomega) {
				fetchedNode := &corev1.Node{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: nodeName}, fetchedNode)).To(Succeed())
				_, hasLabel := fetchedNode.Labels["test-label"]
				g.Expect(hasLabel).To(BeFalse(), "Label should be removed from node")
			}, 10*time.Second).Should(Succeed())

			By("verifying dpunodemaintenance is deleted")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), &provisioningv1.DPUNodeMaintenance{})
				g.Expect(client.IgnoreNotFound(err)).To(Succeed())
				g.Expect(err).To(HaveOccurred())
			}, 10*time.Second).Should(Succeed())
		})

		It("DPUNodeMaintenance: taint node effect should add taint on node obj", func() {
			By("creating the dpunodemaintenance object")
			obj := &provisioningv1.DPUNodeMaintenance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      DefaultDPUNodeMaintenance,
					Namespace: testNS.Name,
				},
				Spec: provisioningv1.DPUNodeMaintenanceSpec{
					DPUNodeName: nodeName,
					NodeEffect: &provisioningv1.NodeEffect{
						Action: provisioningv1.Action{
							Taint: &corev1.Taint{
								Key:    "test-taint",
								Value:  "test-value",
								Effect: corev1.TaintEffectNoSchedule,
							},
						},
					},
					Requestor: []string{"test-requestor"},
				},
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())

			By("getting the node obj")
			fetchedNode := &corev1.Node{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: nodeName}, fetchedNode)).To(Succeed())
				g.Expect(fetchedNode.Spec.Taints).To(ContainElement(corev1.Taint{
					Key:    "test-taint",
					Value:  "test-value",
					Effect: corev1.TaintEffectNoSchedule,
				}))
			}, 10*time.Second).Should(Succeed())

			// NOTE: DPUNodeMaintenance Deletion Pattern
			// The DPUNodeMaintenance controller requires the Spec.Requestor field to be empty
			// before it will process deletion and remove the finalizer. This is by design:
			// - If Requestor is NOT empty, the controller calls reconcile() instead of reconcileDelete()
			// - Even in reconcileDelete(), it skips removal logic if Requestor is set
			// - This prevents accidental deletion while requestors are still active
			By("clearing requestor to trigger deletion")
			Eventually(func(g Gomega) {
				fetchedObj := &provisioningv1.DPUNodeMaintenance{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), fetchedObj)).To(Succeed())
				orig := fetchedObj.DeepCopy()
				fetchedObj.Spec.Requestor = []string{}
				g.Expect(k8sClient.Patch(ctx, fetchedObj, client.MergeFrom(orig))).To(Succeed())
			}, 10*time.Second).Should(Succeed())

			By("verifying taint is removed from node")
			Eventually(func(g Gomega) {
				fetchedNode := &corev1.Node{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: nodeName}, fetchedNode)).To(Succeed())
				g.Expect(fetchedNode.Spec.Taints).NotTo(ContainElement(corev1.Taint{
					Key:    "test-taint",
					Value:  "test-value",
					Effect: corev1.TaintEffectNoSchedule,
				}))
			}, 10*time.Second).Should(Succeed())

			By("verifying dpunodemaintenance is deleted")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), &provisioningv1.DPUNodeMaintenance{})
				g.Expect(client.IgnoreNotFound(err)).To(Succeed())
				g.Expect(err).To(HaveOccurred())
			}, 10*time.Second).Should(Succeed())
		})

		It("DPUNodeMaintenance: custom action node effect should create custom action job", func() {
			By("creating the configmap")
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
          args: ["-c", "echo 'DPF custom action' | tee /tmp/success "]`, testNS.Name))

			configMap := &corev1.ConfigMap{}
			err := yaml.UnmarshalStrict(yml, configMap)
			Expect(err).To(Succeed())
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			By("creating the dpunodemaintenance object")
			obj := &provisioningv1.DPUNodeMaintenance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      DefaultDPUNodeMaintenance,
					Namespace: testNS.Name,
				},
				Spec: provisioningv1.DPUNodeMaintenanceSpec{
					DPUNodeName: nodeName,
					NodeEffect: &provisioningv1.NodeEffect{
						Action: provisioningv1.Action{
							CustomAction: ptr.To("dpu-custom-action"),
						},
					},
					Requestor: []string{"test-requestor"},
				},
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())

			By("getting the job obj")
			jobName, err := cutil.GenerateDPUNodeMaintenanceObjectName(obj.Spec.DPUNodeName, obj.Spec.NodeEffect)
			Expect(err).To(Succeed())

			fetchedJob := &batchv1.Job{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: jobName}, fetchedJob)).To(Succeed())
			}, 10*time.Second).Should(Succeed())

			// NOTE: DPUNodeMaintenance Deletion Pattern
			// The DPUNodeMaintenance controller requires the Spec.Requestor field to be empty
			// before it will process deletion and remove the finalizer. This is by design:
			// - If Requestor is NOT empty, the controller calls reconcile() instead of reconcileDelete()
			// - Even in reconcileDelete(), it skips removal logic if Requestor is set
			// - This prevents accidental deletion while requestors are still active
			By("clearing requestor to trigger deletion")
			Eventually(func(g Gomega) {
				fetchedObj := &provisioningv1.DPUNodeMaintenance{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), fetchedObj)).To(Succeed())
				orig := fetchedObj.DeepCopy()
				fetchedObj.Spec.Requestor = []string{}
				g.Expect(k8sClient.Patch(ctx, fetchedObj, client.MergeFrom(orig))).To(Succeed())
			}, 10*time.Second).Should(Succeed())

			By("verifying custom action job is removed")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: jobName}, &batchv1.Job{})
				g.Expect(client.IgnoreNotFound(err)).To(Succeed())
				g.Expect(err).To(HaveOccurred())
			}, 10*time.Second).Should(Succeed())

			By("verifying dpunodemaintenance is deleted")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), &provisioningv1.DPUNodeMaintenance{})
				g.Expect(client.IgnoreNotFound(err)).To(Succeed())
				g.Expect(err).To(HaveOccurred())
			}, 10*time.Second).Should(Succeed())
		})

		It("DPUNodeMaintenance: hold node effect should add hold annotation on dpunodemaintenance obj", func() {

			By("creating the dpunodemaintenance object")
			obj := &provisioningv1.DPUNodeMaintenance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      DefaultDPUNodeMaintenance,
					Namespace: testNS.Name,
				},
				Spec: provisioningv1.DPUNodeMaintenanceSpec{
					DPUNodeName: nodeName,
					NodeEffect: &provisioningv1.NodeEffect{
						Action: provisioningv1.Action{
							Hold: ptr.To(true),
						},
					},
					Requestor: []string{"test-requestor"},
				},
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())

			By("getting the dpunodemaintenance obj")
			fetchedDPUNodeMaintenance := &provisioningv1.DPUNodeMaintenance{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: DefaultDPUNodeMaintenance}, fetchedDPUNodeMaintenance)).To(Succeed())
				g.Expect(fetchedDPUNodeMaintenance.Annotations[cutil.HoldNodeEffectKey]).To(Equal("true"))
			}, 10*time.Second).Should(Succeed())
		})

		It("DPUNodeMaintenance: observed generation should change when requestors are updated while NodeEffectApplied", func() {
			By("creating the dpunodemaintenance object")
			obj := &provisioningv1.DPUNodeMaintenance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      DefaultDPUNodeMaintenance,
					Namespace: testNS.Name,
				},
				Spec: provisioningv1.DPUNodeMaintenanceSpec{
					DPUNodeName: nodeName,
					NodeEffect: &provisioningv1.NodeEffect{
						Action: provisioningv1.Action{
							CustomLabel: map[string]string{"test-label": "test-value"},
						},
					},
					Requestor: []string{"test-requestor-1", "test-requestor-2"},
				},
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())

			By("waiting for NodeEffectApplied condition to be true")
			Eventually(func(g Gomega) {
				fetchedDPUNodeMaintenance := &provisioningv1.DPUNodeMaintenance{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: DefaultDPUNodeMaintenance}, fetchedDPUNodeMaintenance)).To(Succeed())
				g.Expect(fetchedDPUNodeMaintenance.Status.Conditions).To(ContainElement(
					And(
						HaveField("Type", "NodeEffectApplied"),
						HaveField("Status", metav1.ConditionTrue),
						HaveField("ObservedGeneration", int64(1)),
					),
				))
			}, 10*time.Second).Should(Succeed())

			By("updating the requestors")
			fetchedDPUNodeMaintenance := &provisioningv1.DPUNodeMaintenance{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: DefaultDPUNodeMaintenance}, fetchedDPUNodeMaintenance)).To(Succeed())
			orig := fetchedDPUNodeMaintenance.DeepCopy()
			fetchedDPUNodeMaintenance.Spec.Requestor = []string{"test-requestor-1"}
			Expect(k8sClient.Patch(ctx, fetchedDPUNodeMaintenance, client.MergeFrom(orig))).To(Succeed())

			By("verifying that observed generation has changed")
			Eventually(func(g Gomega) {
				fetchedDPUNodeMaintenance := &provisioningv1.DPUNodeMaintenance{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: testNS.Name, Name: DefaultDPUNodeMaintenance}, fetchedDPUNodeMaintenance)).To(Succeed())
				g.Expect(fetchedDPUNodeMaintenance.Status.Conditions).To(ContainElement(
					And(
						HaveField("Type", "NodeEffectApplied"),
						HaveField("Status", metav1.ConditionTrue),
						HaveField("ObservedGeneration", int64(2)),
					),
				))
			}, 10*time.Second).Should(Succeed())
		})
	})
})
