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
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	operatorcontroller "github.com/nvidia/doca-platform/internal/operator/controllers"
	"github.com/nvidia/doca-platform/test/utils/informer"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DefaultDPFOperatorConfig = "test-dpfoperatorconfig"
)

var _ = Describe("DPUNode Controller", func() {
	const (
		DefaultSerialNumber = "MT25066004C7"
	)

	var (
		testNS                *corev1.Namespace
		i                     *informer.TestInformer
		testDPFoperatorConfig *operatorv1.DPFOperatorConfig
	)

	const (
		testIP   string = "1.1.1.1"
		testPort uint16 = 1111
	)

	var createDPFOperatorConfig = func(name string, namespace string) *operatorv1.DPFOperatorConfig {
		return &operatorv1.DPFOperatorConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: operatorv1.DPFOperatorConfigSpec{
				ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
					BFBPersistentVolumeClaimName: "foo-pvc",
				},
			},
		}
	}

	var createDPUNode = func(name string, namespace string) *provisioningv1.DPUNode {
		return &provisioningv1.DPUNode{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
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
			Spec: provisioningv1.DPUNodeSpec{},
		}
	}

	var createDPUDevice = func(name string, namespace string, pciAddress *string, bmcIP *string) *provisioningv1.DPUDevice {
		dpuDevice := &provisioningv1.DPUDevice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: provisioningv1.DPUDeviceSpec{
				SerialNumber: DefaultSerialNumber,
				BMCIP:        bmcIP,
			},
		}
		Expect(k8sClient.Create(ctx, dpuDevice)).NotTo(HaveOccurred())
		patch := client.MergeFrom(dpuDevice.DeepCopy())
		dpuDevice.Status.PCIAddress = pciAddress
		Expect(k8sClient.Status().Patch(ctx, dpuDevice, patch)).To(Succeed())
		return dpuDevice
	}

	Describe("DPUNode", func() {
		BeforeEach(func() {
			By("creating the namespace")

			// Notes:
			// 1. Namespace usage limitation:
			// EnvTest does not support namespace deletion. Deleting a namespace will seem to succeed,
			// but the namespace will just be put in a Terminating state, and never actually be reclaimed.
			// See: https://book.kubebuilder.io/reference/envtest.html#namespace-usage-limitation
			// 2. the value in GenerateName is not defined as a constant intentionally,
			// because it shouldn't be referenced directly.
			// 3. testNS is the only way to reference the namespace in the test.
			// 4. always create a new namespace for each test, never reuse an existing namespace
			testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "provisioning"}}
			Eventually(func() error {
				return k8sClient.Create(ctx, testNS)
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("Creating the dpfoperatorconfig")
			testDPFoperatorConfig = createDPFOperatorConfig(DefaultDPFOperatorConfig, testNS.Name)
			Expect(k8sClient.Create(ctx, testDPFoperatorConfig)).To(Succeed())

			By("Creating the informer infrastructure for DPUNode")
			i = informer.NewInformer(cfg, provisioningv1.DPUNodeGroupVersionKind, operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace, "dpunodes")
			DeferCleanup(i.Cleanup)
			go i.Run()
			Eventually(i.HasSynced).WithTimeout(5 * time.Second).WithPolling(100 * time.Millisecond).Should(BeTrue())
		})
		AfterEach(func() {
			Expect(client.IgnoreNotFound(k8sClient.DeleteAllOf(ctx, &corev1.Node{}))).To(Succeed())
			Expect(client.IgnoreNotFound(k8sClient.DeleteAllOf(ctx, &provisioningv1.DPUNode{}))).To(Succeed())
			Expect(client.IgnoreNotFound(k8sClient.DeleteAllOf(ctx, &provisioningv1.DPUDevice{}))).To(Succeed())
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, testDPFoperatorConfig))).To(Succeed())
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNS.Name}}))).To(Succeed())
		})

		Context("obj test context", func() {
			ctx := context.Background()

			It("DPUNode: Create Node - check DPUNode owner ref", func() {
				node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "dpunode-test-node-2", Namespace: testNS.Name,
					Labels:      map[string]string{"test-label": ""},
					Annotations: map[string]string{"test-annotation": ""}}}
				Expect(k8sClient.Create(ctx, node)).NotTo(HaveOccurred())

				dpuNode := createDPUNode("dpunode-test-node-2", testNS.Name)
				dpuNode.ObjectMeta.OwnerReferences = append(dpuNode.ObjectMeta.OwnerReferences, metav1.OwnerReference{
					APIVersion: "/v1, Kind=Node",
					Kind:       "Node",
					Name:       node.Name,
					UID:        node.UID,
				})
				Expect(k8sClient.Create(ctx, dpuNode)).To(Succeed())

				By("Check DPUNode owner ref")
				dpuNodeFetched := &provisioningv1.DPUNode{}
				Eventually(func(g Gomega) []metav1.OwnerReference {
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dpuNode), dpuNodeFetched)).To(Succeed())
					return dpuNodeFetched.OwnerReferences
				}).WithTimeout(10 * time.Second).Should(ContainElement(metav1.OwnerReference{
					APIVersion: "/v1, Kind=Node",
					Kind:       "Node",
					Name:       node.Name,
					UID:        node.UID,
				}))
			})
			It("DPUNode: fail to create with name exceeding the maximum length", func() {
				// The current limit is based on the certificate name for dms that can't have commonName exceeding 64
				// chars. In addition, the dmsinit.sh creates a DPUDevice out of the name of the DPUNode + a hyphen
				// (1 char) + the pci address of the device (max 10 chars) = 59 chars that must not exceed 63 which is
				// the limit of the DPUDevice.
				// TODO: Add e2e test for k8s environment with one of the nodes having name with length 48 and verify
				// that provisioning works as expected.
				dpuNode := createDPUNode(utilrand.String(49), testNS.Name)
				Expect(k8sClient.Create(ctx, dpuNode)).To(HaveOccurred())
			})
			It("DPUNode: Create Node - check DPUNode kubeNodeRef, labels & annotations copy", func() {
				node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "dpunode-test-node-3", Namespace: testNS.Name,
					Labels:      map[string]string{"test-label": ""},
					Annotations: map[string]string{"test-annotation": ""}}}
				Expect(k8sClient.Create(ctx, node)).NotTo(HaveOccurred())

				dpuNode := createDPUNode("dpunode-test-node-3", testNS.Name)
				dpuNode.ObjectMeta.OwnerReferences = append(dpuNode.ObjectMeta.OwnerReferences, metav1.OwnerReference{
					APIVersion: "/v1, Kind=Node",
					Kind:       "Node",
					Name:       node.Name,
					UID:        node.UID,
				})
				Expect(k8sClient.Create(ctx, dpuNode)).To(Succeed())

				By("Check DPUNode kubeNodeRef, labels & annotations copy")
				dpuNodeFetched := &provisioningv1.DPUNode{}
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dpuNode), dpuNodeFetched)).To(Succeed())
					g.Expect(dpuNodeFetched.Status.KubeNodeRef).NotTo(BeNil())
					g.Expect(dpuNodeFetched.Labels).To(HaveKeyWithValue("test-label", ""))
					g.Expect(dpuNodeFetched.Annotations).To(HaveKeyWithValue("test-annotation", ""))
				}).WithTimeout(10 * time.Second).Should(Succeed())
				Expect(*dpuNodeFetched.Status.KubeNodeRef).To(Equal(node.Name))

				By("Add new label to Node and verify DPUNode gets it")
				nodeFetched := &corev1.Node{}
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(node), nodeFetched)).To(Succeed())
				nodeFetchedCopy := nodeFetched.DeepCopy()
				nodeFetched.Labels["new-test-label"] = "new-value"
				Expect(k8sClient.Patch(ctx, nodeFetched, client.MergeFrom(nodeFetchedCopy))).To(Succeed())

				Eventually(func(g Gomega) map[string]string {
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dpuNode), dpuNodeFetched)).To(Succeed())
					return dpuNodeFetched.Labels
				}).WithTimeout(10 * time.Second).Should(HaveKeyWithValue("new-test-label", "new-value"))
			})

			It("DPUNode: Validate DPUNode.Status.DPUInstallInterface=gNOI", func() {
				dpuNode := createDPUNode("dpunode-test-node-5", operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace)
				Expect(k8sClient.Create(ctx, dpuNode)).To(Succeed())

				dpuNodeFetched := &provisioningv1.DPUNode{}
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dpuNode), dpuNodeFetched)).To(Succeed())
					g.Expect(dpuNodeFetched.Status.DPUInstallInterface).NotTo(BeNil())
				}).WithTimeout(30 * time.Second).Should(Succeed())
				Expect(*dpuNodeFetched.Status.DPUInstallInterface).To(Equal(string(provisioningv1.DPUNodeInstallInterfaceGNOI)))
			})
			It("DPUNode: Validate DPUDevice params - good flow gNOI", func() {
				dpuDevice := createDPUDevice("dpudevice-3", operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace, ptr.To("0000-04-00"), nil)
				dpuNode := createDPUNode("dpunode-test-node-6", operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace)
				dpuNode.Spec.DPUs = []provisioningv1.DPURef{
					{
						Name: dpuDevice.Name,
					},
				}
				dpuNode.Spec.NodeDMSAddress = &provisioningv1.DMSAddress{IP: testIP, Port: testPort}
				Expect(k8sClient.Create(ctx, dpuNode)).To(Succeed())

				dpuNodeFetched := &provisioningv1.DPUNode{}

				Eventually(func(g Gomega) []metav1.Condition {
					ev := &informer.Event{}
					g.Eventually(i.UpdateEvents).Should(Receive(ev))
					oldDPUNodeObj := &provisioningv1.DPUNode{}
					newDPUNodeObj := &provisioningv1.DPUNode{}
					g.Expect(k8sClient.Scheme().Convert(ev.OldObj, oldDPUNodeObj, nil)).ToNot(HaveOccurred())
					g.Expect(k8sClient.Scheme().Convert(ev.NewObj, newDPUNodeObj, nil)).ToNot(HaveOccurred())

					dpuNodeFetched = newDPUNodeObj
					return dpuNodeFetched.Status.Conditions
				}).WithTimeout(30 * time.Second).Should(ConsistOf(
					And(
						HaveField("Type", provisioningv1.DPUNodeConditionReady.String()),
						HaveField("Status", metav1.ConditionFalse),
						HaveField("Reason", "DMSServerNotReady"),
					),
				))
			})
			It("DPUNode: Validate DPUDevice params - bad flow - gNOI, no PCIAddress", func() {
				dpuDevice := createDPUDevice("dpudevice-4", operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace, nil, ptr.To("1.1.1.1"))
				dpuNode := createDPUNode("dpunode-test-node-7", operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace)
				dpuNode.Spec.DPUs = []provisioningv1.DPURef{
					{
						Name: dpuDevice.Name,
					},
				}
				dpuNode.Spec.NodeDMSAddress = &provisioningv1.DMSAddress{IP: testIP, Port: testPort}
				Expect(k8sClient.Create(ctx, dpuNode)).To(Succeed())

				dpuNodeFetched := &provisioningv1.DPUNode{}

				Eventually(func(g Gomega) []metav1.Condition {
					ev := &informer.Event{}
					g.Eventually(i.UpdateEvents).Should(Receive(ev))
					oldDPUNodeObj := &provisioningv1.DPUNode{}
					newDPUNodeObj := &provisioningv1.DPUNode{}
					g.Expect(k8sClient.Scheme().Convert(ev.OldObj, oldDPUNodeObj, nil)).ToNot(HaveOccurred())
					g.Expect(k8sClient.Scheme().Convert(ev.NewObj, newDPUNodeObj, nil)).ToNot(HaveOccurred())

					dpuNodeFetched = newDPUNodeObj
					return dpuNodeFetched.Status.Conditions
				}).WithTimeout(30 * time.Second).Should(ConsistOf(
					And(
						HaveField("Type", provisioningv1.DPUNodeConditionInvalidDPUDetails.String()),
						HaveField("Status", metav1.ConditionTrue),
						HaveField("Reason", string(provisioningv1.DPUNodeConditionInvalidDPUDetails)),
					),
				))
			})
			It("DPUNode: Check DMS Server Readiness - expect error, Validate DPUNode condition Ready=false, DMSServerNotReady", func() {
				dpuNode := createDPUNode("dpunode-test-node-8", operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace)
				dpuNode.Spec.NodeDMSAddress = &provisioningv1.DMSAddress{IP: testIP, Port: testPort}
				Expect(k8sClient.Create(ctx, dpuNode)).To(Succeed())

				dpuNodeFetched := &provisioningv1.DPUNode{}

				Eventually(func(g Gomega) []metav1.Condition {
					ev := &informer.Event{}
					g.Eventually(i.UpdateEvents).Should(Receive(ev))
					oldDPUNodeObj := &provisioningv1.DPUNode{}
					newDPUNodeObj := &provisioningv1.DPUNode{}
					g.Expect(k8sClient.Scheme().Convert(ev.OldObj, oldDPUNodeObj, nil)).ToNot(HaveOccurred())
					g.Expect(k8sClient.Scheme().Convert(ev.NewObj, newDPUNodeObj, nil)).ToNot(HaveOccurred())

					dpuNodeFetched = newDPUNodeObj
					return dpuNodeFetched.Status.Conditions
				}).WithTimeout(30 * time.Second).Should(ConsistOf(
					And(
						HaveField("Type", provisioningv1.DPUNodeConditionReady.String()),
						HaveField("Status", metav1.ConditionFalse),
						HaveField("Reason", "DMSServerNotReady"),
					),
				))
			})
			It("DPUNode: NodeDMSAdress is not set - expect error", func() {
				dpuNode := createDPUNode("dpunode-test-node-9", operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace)
				Expect(k8sClient.Create(ctx, dpuNode)).To(Succeed())

				dpuNodeFetched := &provisioningv1.DPUNode{}

				Eventually(func(g Gomega) []metav1.Condition {
					ev := &informer.Event{}
					g.Eventually(i.UpdateEvents).Should(Receive(ev))
					oldDPUNodeObj := &provisioningv1.DPUNode{}
					newDPUNodeObj := &provisioningv1.DPUNode{}
					g.Expect(k8sClient.Scheme().Convert(ev.OldObj, oldDPUNodeObj, nil)).ToNot(HaveOccurred())
					g.Expect(k8sClient.Scheme().Convert(ev.NewObj, newDPUNodeObj, nil)).ToNot(HaveOccurred())

					dpuNodeFetched = newDPUNodeObj
					return dpuNodeFetched.Status.Conditions
				}).WithTimeout(30 * time.Second).Should(ConsistOf(
					And(
						HaveField("Type", provisioningv1.DPUNodeConditionReady.String()),
						HaveField("Status", metav1.ConditionFalse),
						HaveField("Reason", "NoNodeDMSAddress"),
					),
				))
			})
			It("DPUNode: Validate DPUNode.Status.DPUInstallInterface=redfish, Validate DPUNode condition Ready", func() {
				Skip("Skip this test due to race condition - changing DPUNodeReconciler.DPUInstallInterface during tests are running")
				By("Change dpuNodeReconciler.DPUInstallInterface to redfish")
				dpunodeReconciler.DPUInstallInterface = ptr.To(string(provisioningv1.InstallViaRedFish))
				dpuNode := createDPUNode("dpunode-test-node-10", operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace)
				Expect(k8sClient.Create(ctx, dpuNode)).To(Succeed())

				dpuNodeFetched := &provisioningv1.DPUNode{}

				Eventually(func(g Gomega) []metav1.Condition {
					ev := &informer.Event{}
					g.Eventually(i.UpdateEvents).Should(Receive(ev))
					oldDPUNodeObj := &provisioningv1.DPUNode{}
					newDPUNodeObj := &provisioningv1.DPUNode{}
					g.Expect(k8sClient.Scheme().Convert(ev.OldObj, oldDPUNodeObj, nil)).ToNot(HaveOccurred())
					g.Expect(k8sClient.Scheme().Convert(ev.NewObj, newDPUNodeObj, nil)).ToNot(HaveOccurred())

					dpuNodeFetched = newDPUNodeObj
					return dpuNodeFetched.Status.Conditions
				}).WithTimeout(30 * time.Second).Should(ConsistOf(
					And(
						HaveField("Type", provisioningv1.DPUNodeConditionReady.String()),
						HaveField("Status", metav1.ConditionTrue),
					),
				))
				Expect(*dpuNodeFetched.Status.DPUInstallInterface).To(Equal(string(provisioningv1.DPUNodeInstallIntrefaceRedfish)))

			})
			It("DPUNode: Validate DPUDevice params - bad flow - redfish, no BMC IP", func() {
				Skip("Skip this test due to race condition - changing DPUNodeReconciler.DPUInstallInterface during tests are running")
				dpuDevice := createDPUDevice("dpudevice-5", operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace, ptr.To("0000-03-00"), nil)
				dpuNode := createDPUNode("dpunode-test-node-11", operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace)
				dpuNode.Spec.DPUs = []provisioningv1.DPURef{
					{
						Name: dpuDevice.Name,
					},
				}
				dpuNode.Spec.NodeDMSAddress = &provisioningv1.DMSAddress{IP: testIP, Port: testPort}
				Expect(k8sClient.Create(ctx, dpuNode)).To(Succeed())

				dpuNodeFetched := &provisioningv1.DPUNode{}

				Eventually(func(g Gomega) []metav1.Condition {
					ev := &informer.Event{}
					g.Eventually(i.UpdateEvents).Should(Receive(ev))
					oldDPUNodeObj := &provisioningv1.DPUNode{}
					newDPUNodeObj := &provisioningv1.DPUNode{}
					g.Expect(k8sClient.Scheme().Convert(ev.OldObj, oldDPUNodeObj, nil)).ToNot(HaveOccurred())
					g.Expect(k8sClient.Scheme().Convert(ev.NewObj, newDPUNodeObj, nil)).ToNot(HaveOccurred())

					dpuNodeFetched = newDPUNodeObj
					return dpuNodeFetched.Status.Conditions
				}).WithTimeout(30 * time.Second).Should(ConsistOf(
					And(
						HaveField("Type", provisioningv1.DPUNodeConditionInvalidDPUDetails.String()),
						HaveField("Status", metav1.ConditionTrue),
						HaveField("Reason", string(provisioningv1.DPUNodeConditionInvalidDPUDetails)),
					),
				))
			})
			It("DPUNode: Validate DPUDevice params - good flow - redfish", func() {
				Skip("Skip this test due to race condition - changing DPUNodeReconciler.DPUInstallInterface during tests are running")
				dpuDevice := createDPUDevice("dpudevice-6", operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace, ptr.To("0000-03-00"), ptr.To("2.2.2.2"))
				dpuNode := createDPUNode("dpunode-test-node-12", operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace)
				dpuNode.Spec.DPUs = []provisioningv1.DPURef{
					{
						Name: dpuDevice.Name,
					},
				}
				Expect(k8sClient.Create(ctx, dpuNode)).To(Succeed())

				dpuNodeFetched := &provisioningv1.DPUNode{}

				Eventually(func(g Gomega) []metav1.Condition {
					ev := &informer.Event{}
					g.Eventually(i.UpdateEvents).Should(Receive(ev))
					oldDPUNodeObj := &provisioningv1.DPUNode{}
					newDPUNodeObj := &provisioningv1.DPUNode{}
					g.Expect(k8sClient.Scheme().Convert(ev.OldObj, oldDPUNodeObj, nil)).ToNot(HaveOccurred())
					g.Expect(k8sClient.Scheme().Convert(ev.NewObj, newDPUNodeObj, nil)).ToNot(HaveOccurred())

					dpuNodeFetched = newDPUNodeObj
					return dpuNodeFetched.Status.Conditions
				}).WithTimeout(30 * time.Second).Should(ConsistOf(
					And(
						HaveField("Type", provisioningv1.DPUNodeConditionReady.String()),
						HaveField("Status", metav1.ConditionTrue),
					),
				))
			})
			It("DPUDevice: fail to create with name exceeding the maximum length", func() {
				// The current limit is based on the fact that DPUDevice name is added as value in labels of other
				// objects.
				// TODO: Ideally, add e2e test to verify that all objects can be created as expected and provisioning
				// works for 63 chars.
				dpuDevice := &provisioningv1.DPUDevice{
					ObjectMeta: metav1.ObjectMeta{
						Name:      utilrand.String(64),
						Namespace: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace,
					},
					Spec: provisioningv1.DPUDeviceSpec{
						SerialNumber: DefaultSerialNumber,
						BMCIP:        ptr.To("2.2.2.2"),
					},
				}
				Expect(k8sClient.Create(ctx, dpuDevice)).To(HaveOccurred())
			})
		})
	})
})
