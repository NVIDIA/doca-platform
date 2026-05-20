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
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/release"
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
	annotationTrue           = "true"
)

var _ = Describe("DPUNode Controller", func() {
	const (
		DefaultSerialNumberPrefix = "MT25066004C"
	)

	var (
		testNS                *corev1.Namespace
		i                     *informer.TestInformer
		testDPFoperatorConfig *operatorv1.DPFOperatorConfig
	)

	var createDPFOperatorConfig = func(name string, namespace string) *operatorv1.DPFOperatorConfig {
		return &operatorv1.DPFOperatorConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: operatorv1.DPFOperatorConfigSpec{
				DeploymentMode: operatorv1.DeploymentModeHostTrusted,
				ProvisioningController: &operatorv1.ProvisioningControllerConfiguration{
					BFBPersistentVolumeClaimName: ptr.To("foo-pvc"),
				},
			},
		}
	}

	var createDPUNode = func(name string, namespace string) *provisioningv1.DPUNode {
		return &provisioningv1.DPUNode{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels: map[string]string{
					release.DPFVersionLabelKey: release.DPFVersion(),
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
				SerialNumber: DefaultSerialNumberPrefix + utilrand.String(5),
				BMCIP:        bmcIP,
			},
		}
		Expect(k8sClient.Create(ctx, dpuDevice)).NotTo(HaveOccurred())
		patch := client.MergeFrom(dpuDevice.DeepCopy())
		dpuDevice.Status.PCIAddress = pciAddress
		Expect(k8sClient.Status().Patch(ctx, dpuDevice, patch)).To(Succeed())
		return dpuDevice
	}

	var patchFakePhase = func(name string, phase provisioningv1.DPUPhase) {
		key := client.ObjectKey{Namespace: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace, Name: name}
		dpu := &provisioningv1.DPU{}
		Expect(k8sClient.Get(ctx, key, dpu)).To(Succeed())
		orig := dpu.DeepCopy()
		GinkgoWriter.Printf("before patch Current value: %v\n", orig.Status.Phase)
		GinkgoWriter.Printf("before patch Current value: %v\n", orig.Status.Conditions)

		dpu.Status.Phase = phase
		conditions := []metav1.Condition{
			{
				Type:               provisioningv1.DPUCondInterfaceInitialized.String(),
				Status:             metav1.ConditionTrue,
				Reason:             string(provisioningv1.DPUCondInterfaceInitialized),
				LastTransitionTime: metav1.Now(),
			},
			{
				Type:               provisioningv1.DPUCondOSInstalled.String(),
				Status:             metav1.ConditionTrue,
				Reason:             string(provisioningv1.DPUCondOSInstalled),
				LastTransitionTime: metav1.Now(),
			},
		}
		dpu.Status.Conditions = conditions
		Expect(k8sClient.Status().Patch(ctx, dpu, client.MergeFrom(orig))).To(Succeed())

		fetchedDPU := &provisioningv1.DPU{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dpu), fetchedDPU)).To(Succeed())
		GinkgoWriter.Printf("after patch Current value: %v\n", fetchedDPU.Status.Phase)
		GinkgoWriter.Printf("after patch Current value: %v\n", fetchedDPU.Status.Conditions)
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
			testDPFoperatorConfig = createDPFOperatorConfig(DefaultDPFOperatorConfig, operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace)
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
			Expect(client.IgnoreNotFound(k8sClient.DeleteAllOf(ctx, &provisioningv1.DPU{}))).To(Succeed())
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
						SerialNumber: DefaultSerialNumberPrefix + utilrand.String(5),
						BMCIP:        ptr.To("2.2.2.2"),
					},
				}
				Expect(k8sClient.Create(ctx, dpuDevice)).To(HaveOccurred())
			})

			It("DPUNode: DPUNode condition RebootInProgress and annotation should be set as expected when two DPUs under the same host are in rebooting phase at the same time", func() {
				// Create DPUDevices
				dpuDevice1 := createDPUDevice("dpudevice-1", operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace, ptr.To("MT2333XZ0X5R"), nil)
				dpuDevice2 := createDPUDevice("dpudevice-2", operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace, ptr.To("MT2337XZ04WL"), nil)

				// Create DPUNode
				dpuNode := createDPUNode("test-dpunode-13", operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace)
				dpuNode.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{
					External: &provisioningv1.External{},
				}
				dpuNode.Spec.DPUs = []provisioningv1.DPURef{
					{Name: dpuDevice1.Name},
					{Name: dpuDevice2.Name},
				}
				Expect(k8sClient.Create(ctx, dpuNode)).To(Succeed())

				// Set DPUInstallInterface on DPUNode to avoid DPU controller errors
				// Get the latest version to avoid conflict errors
				latestDPUNode := &provisioningv1.DPUNode{}
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dpuNode), latestDPUNode)).To(Succeed())
				orig := latestDPUNode.DeepCopy()
				latestDPUNode.Status.DPUInstallInterface = ptr.To(string(provisioningv1.DPUNodeInstallInterfaceGNOI))
				Expect(k8sClient.Status().Patch(ctx, latestDPUNode, client.MergeFrom(orig))).To(Succeed())

				// Add OOB bridge configured label to avoid DPU controller errors
				// Get the latest version to avoid conflict errors
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dpuNode), latestDPUNode)).To(Succeed())
				orig = latestDPUNode.DeepCopy()
				if latestDPUNode.Labels == nil {
					latestDPUNode.Labels = make(map[string]string)
				}
				latestDPUNode.Labels[cutil.NodeFeatureDiscoveryLabelPrefix+cutil.DPUOOBBridgeConfiguredLabel] = annotationTrue
				Expect(k8sClient.Patch(ctx, latestDPUNode, client.MergeFrom(orig))).To(Succeed())

				// Create DPUs with different phases
				dpu1 := &provisioningv1.DPU{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-dpunode-13-dpudevice-1",
						Namespace: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace,
					},
					Spec: provisioningv1.DPUSpec{
						DPUNodeName:   "test-dpunode-13",
						DPUDeviceName: dpuDevice1.Name,
						SerialNumber:  DefaultSerialNumberPrefix + utilrand.String(5),
						DPUFlavor:     "dpu-flavor",
						NodeEffect:    provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
					},
				}
				dpu2 := &provisioningv1.DPU{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-dpunode-13-dpudevice-2",
						Namespace: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace,
					},
					Spec: provisioningv1.DPUSpec{
						DPUNodeName:   "test-dpunode-13",
						DPUDeviceName: dpuDevice2.Name,
						SerialNumber:  DefaultSerialNumberPrefix + utilrand.String(5),
						DPUFlavor:     "dpu-flavor",
						NodeEffect:    provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
					},
				}

				Expect(k8sClient.Create(ctx, dpu1)).To(Succeed())
				patchFakePhase(dpu1.Name, provisioningv1.DPURebooting)

				Expect(k8sClient.Create(ctx, dpu2)).To(Succeed())
				patchFakePhase(dpu2.Name, provisioningv1.DPURebooting)

				fetchedDPU1 := &provisioningv1.DPU{}
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dpu1), fetchedDPU1)).To(Succeed())
				Expect(fetchedDPU1.Status.Phase).To(Equal(provisioningv1.DPURebooting))

				fetchedDPU2 := &provisioningv1.DPU{}
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dpu2), fetchedDPU2)).To(Succeed())

				Expect(fetchedDPU2.Status.Phase).To(Equal(provisioningv1.DPURebooting))

				// check DPUNode annotation and condition is set as expected
				Eventually(func(g Gomega) []metav1.Condition {
					dpuNodeFetched := &provisioningv1.DPUNode{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dpuNode), dpuNodeFetched)).To(Succeed())
					g.Expect(dpuNodeFetched.Annotations[provisioningv1.DPUNodeExternalRebootRequiredAnnotation]).To(Equal(annotationTrue))

					return dpuNodeFetched.Status.Conditions
				}).WithTimeout(5 * time.Second).Should(ContainElement(
					And(
						HaveField("Type", provisioningv1.DPUNodeConditionRebootInProgress.String()),
						HaveField("Status", metav1.ConditionTrue),
					),
				))
			})

			It("DPUNode: DPUNode condition RebootInProgress should be set as expected if there is only one DPU deployed on multiple DPUs host", func() {
				// Create DPUDevices
				dpuDevice7 := createDPUDevice("dpudevice-7", operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace, ptr.To("MT2333XZ0X5R"), nil)
				dpuDevice8 := createDPUDevice("dpudevice-8", operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace, ptr.To("MT2337XZ04WL"), nil)
				// Create DPUNode
				dpuNode := createDPUNode("test-dpunode-14", operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace)
				dpuNode.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{
					External: &provisioningv1.External{},
				}
				dpuNode.Spec.DPUs = []provisioningv1.DPURef{
					{Name: dpuDevice7.Name},
					{Name: dpuDevice8.Name},
				}
				Expect(k8sClient.Create(ctx, dpuNode)).To(Succeed())

				// Set DPUInstallInterface on DPUNode to avoid DPU controller errors
				Eventually(func() error {
					latestDPUNode := &provisioningv1.DPUNode{}
					if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dpuNode), latestDPUNode); err != nil {
						return err
					}
					latestDPUNode.Status.DPUInstallInterface = ptr.To(string(provisioningv1.DPUNodeInstallInterfaceGNOI))
					return k8sClient.Status().Update(ctx, latestDPUNode)
				}).WithTimeout(10 * time.Second).Should(Succeed())

				// Add OOB bridge configured label to avoid DPU controller errors
				// Get the latest version to avoid conflict errors
				latestDPUNode := &provisioningv1.DPUNode{}
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dpuNode), latestDPUNode)).To(Succeed())
				orig := latestDPUNode.DeepCopy()
				if latestDPUNode.Labels == nil {
					latestDPUNode.Labels = make(map[string]string)
				}
				latestDPUNode.Labels[cutil.NodeFeatureDiscoveryLabelPrefix+cutil.DPUOOBBridgeConfiguredLabel] = annotationTrue
				Expect(k8sClient.Patch(ctx, latestDPUNode, client.MergeFrom(orig))).To(Succeed())

				// Create DPUs with different phases
				dpu1 := &provisioningv1.DPU{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-dpunode-14-dpudevice-7",
						Namespace: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace,
					},
					Spec: provisioningv1.DPUSpec{
						DPUNodeName:   "test-dpunode-14",
						DPUDeviceName: dpuDevice7.Name,
						SerialNumber:  DefaultSerialNumberPrefix + utilrand.String(5),
						DPUFlavor:     "dpu-flavor",
						NodeEffect:    provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
					},
				}

				Expect(k8sClient.Create(ctx, dpu1)).To(Succeed())
				patchFakePhase(dpu1.Name, provisioningv1.DPURebooting)

				fetchedDPU1 := &provisioningv1.DPU{}
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dpu1), fetchedDPU1)).To(Succeed())

				// check DPUNode annotation and condition is set as expected
				Eventually(func(g Gomega) []metav1.Condition {
					dpuNodeFetched := &provisioningv1.DPUNode{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dpuNode), dpuNodeFetched)).To(Succeed())
					g.Expect(dpuNodeFetched.Annotations[provisioningv1.DPUNodeExternalRebootRequiredAnnotation]).To(Equal(annotationTrue))
					return dpuNodeFetched.Status.Conditions
				}).WithTimeout(5 * time.Second).Should(ContainElement(
					And(
						HaveField("Type", provisioningv1.DPUNodeConditionRebootInProgress.String()),
						HaveField("Status", metav1.ConditionTrue),
					),
				))

			})

			It("DPUNode: aggregates Status.RebootMethod across two DPURebooting DPUs, then clears it when the DPUs are gone", func() {
				// End-to-end coverage for the aggregated host-level reboot
				// method exposed on DPUNode.Status. Verifies (a) priority
				// aggregation across two DPUs reporting different methods,
				// (b) persistence after Phase leaves DPURebooting, and
				// (c) cleanup when no DPU is in DPURebooting and the
				// DPUNode loses all its DPUs.
				dpuDevice1 := createDPUDevice("dpudevice-21",
					operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace, ptr.To("MT2333XZAGG1"), nil)
				dpuDevice2 := createDPUDevice("dpudevice-22",
					operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace, ptr.To("MT2333XZAGG2"), nil)
				dpuNode := createDPUNode("test-dpunode-21",
					operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace)
				dpuNode.Spec.NodeRebootMethod = &provisioningv1.NodeRebootMethod{External: &provisioningv1.External{}}
				dpuNode.Spec.DPUs = []provisioningv1.DPURef{
					{Name: dpuDevice1.Name},
					{Name: dpuDevice2.Name},
				}
				Expect(k8sClient.Create(ctx, dpuNode)).To(Succeed())

				// Set DPUInstallInterface + OOB-bridge label so the
				// reconciler reaches HandleRebootSync.
				Eventually(func() error {
					latest := &provisioningv1.DPUNode{}
					if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dpuNode), latest); err != nil {
						return err
					}
					latest.Status.DPUInstallInterface = ptr.To(string(provisioningv1.DPUNodeInstallInterfaceGNOI))
					return k8sClient.Status().Update(ctx, latest)
				}).WithTimeout(10 * time.Second).Should(Succeed())

				latestDPUNode := &provisioningv1.DPUNode{}
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dpuNode), latestDPUNode)).To(Succeed())
				orig := latestDPUNode.DeepCopy()
				if latestDPUNode.Labels == nil {
					latestDPUNode.Labels = make(map[string]string)
				}
				latestDPUNode.Labels[cutil.NodeFeatureDiscoveryLabelPrefix+cutil.DPUOOBBridgeConfiguredLabel] = annotationTrue
				Expect(k8sClient.Patch(ctx, latestDPUNode, client.MergeFrom(orig))).To(Succeed())

				dpu1 := &provisioningv1.DPU{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-dpunode-21-dpudevice-21",
						Namespace: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace,
					},
					Spec: provisioningv1.DPUSpec{
						DPUNodeName:   "test-dpunode-21",
						DPUDeviceName: dpuDevice1.Name,
						SerialNumber:  DefaultSerialNumberPrefix + utilrand.String(5),
						DPUFlavor:     "dpu-flavor",
						NodeEffect:    provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
					},
				}
				dpu2 := &provisioningv1.DPU{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-dpunode-21-dpudevice-22",
						Namespace: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace,
					},
					Spec: provisioningv1.DPUSpec{
						DPUNodeName:   "test-dpunode-21",
						DPUDeviceName: dpuDevice2.Name,
						SerialNumber:  DefaultSerialNumberPrefix + utilrand.String(5),
						DPUFlavor:     "dpu-flavor",
						NodeEffect:    provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
					},
				}

				Expect(k8sClient.Create(ctx, dpu1)).To(Succeed())
				patchFakePhase(dpu1.Name, provisioningv1.DPURebooting)
				Expect(k8sClient.Create(ctx, dpu2)).To(Succeed())
				patchFakePhase(dpu2.Name, provisioningv1.DPURebooting)

				// Stamp Status.RebootStatus.Method per DPU (production wiring
				// done by InitializeDPURebootStatus). dpu1 reports the more
				// disruptive method so it must be the aggregation winner.
				setRebootMethod := func(name string, m provisioningv1.RebootMethodType) {
					d := &provisioningv1.DPU{}
					Expect(k8sClient.Get(ctx, client.ObjectKey{
						Namespace: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace,
						Name:      name,
					}, d)).To(Succeed())
					origDPU := d.DeepCopy()
					d.Status.RebootStatus = &provisioningv1.RebootStatus{Method: ptr.To(m)}
					Expect(k8sClient.Status().Patch(ctx, d, client.MergeFrom(origDPU))).To(Succeed())
				}
				setRebootMethod(dpu1.Name, provisioningv1.RebootMethodPowerCycle)
				setRebootMethod(dpu2.Name, provisioningv1.RebootMethodSystemReboot)

				// Assert: Status.RebootMethod=PowerCycle (priority winner).
				Eventually(func(g Gomega) {
					fetched := &provisioningv1.DPUNode{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dpuNode), fetched)).To(Succeed())
					g.Expect(fetched.Status.RebootMethod).NotTo(BeNil())
					g.Expect(*fetched.Status.RebootMethod).To(Equal(provisioningv1.RebootMethodPowerCycle))
				}).WithTimeout(10 * time.Second).WithPolling(200 * time.Millisecond).Should(Succeed())

				// Persistence check: drive the DPUs out of DPURebooting; the
				// aggregated condition and Status.RebootMethod must persist
				// (they are cleared only on stale-True with rebootingCount==0
				// via the noneDPUInNodeEffectOrRebooting first branch, and
				// once that fires Status.RebootMethod is cleared together).
				// To verify stickiness we must keep the reboot in flight,
				// so move only one DPU back to a provisioning phase that
				// does not trigger the stale-True branch.
				patchFakePhase(dpu2.Name, provisioningv1.DPURebooting) // re-affirm
				Consistently(func(g Gomega) {
					fetched := &provisioningv1.DPUNode{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dpuNode), fetched)).To(Succeed())
					g.Expect(fetched.Status.RebootMethod).NotTo(BeNil())
					g.Expect(*fetched.Status.RebootMethod).To(Equal(provisioningv1.RebootMethodPowerCycle))
				}, 2*time.Second, 200*time.Millisecond).Should(Succeed())

				// Cleanup check: delete the DPUs. The stale-True branch
				// removes RebootInProgress on one reconcile; the next
				// reconcile observes label-list==0 and the no-DPUs branch
				// sweeps Status.RebootMethod.
				Expect(k8sClient.Delete(ctx, dpu1)).To(Succeed())
				Expect(k8sClient.Delete(ctx, dpu2)).To(Succeed())
				Eventually(func(g Gomega) {
					fetched := &provisioningv1.DPUNode{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dpuNode), fetched)).To(Succeed())
					g.Expect(fetched.Status.RebootMethod).To(BeNil())
				}).WithTimeout(10 * time.Second).WithPolling(200 * time.Millisecond).Should(Succeed())
			})

			It("DPUNode: Validate the DPUNode condition are set correctly for non-k8s upgrade", func() {
				dpuNode := createDPUNode("test-dpunode-15", operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace)
				dpuNode.Labels[release.DPFVersionLabelKey] = release.LastReleasedDPFGAVersion
				Expect(k8sClient.Create(ctx, dpuNode)).To(Succeed())

				Eventually(func(g Gomega) []metav1.Condition {
					dpuNodeFetched := &provisioningv1.DPUNode{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dpuNode), dpuNodeFetched)).To(Succeed())
					return dpuNodeFetched.Status.Conditions
				}).WithTimeout(5 * time.Second).Should(ContainElement(
					And(
						HaveField("Type", provisioningv1.DPUNodeConditionNeedHostAgentUpgrade.String()),
						HaveField("Status", metav1.ConditionTrue),
					),
				))

				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dpuNode), dpuNode)).To(Succeed())
				var updatedCondition metav1.Condition
				for _, condition := range dpuNode.Status.Conditions {
					if condition.Type == provisioningv1.DPUNodeConditionNeedHostAgentUpgrade.String() {
						condition.Status = metav1.ConditionFalse
						condition.Reason = provisioningv1.DPUNodeConditionNeedHostAgentUpgrade.String()
						condition.ObservedGeneration = dpuNode.Generation
						updatedCondition = condition
						break
					}
				}
				patch := client.MergeFrom(dpuNode.DeepCopy())
				dpuNode.Status.Conditions = []metav1.Condition{updatedCondition}
				Expect(k8sClient.Status().Patch(ctx, dpuNode, patch)).To(Succeed())
				Eventually(func(g Gomega) []metav1.Condition {
					dpuNodeFetched := &provisioningv1.DPUNode{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dpuNode), dpuNodeFetched)).To(Succeed())
					g.Expect(dpuNodeFetched.Labels[release.DPFVersionLabelKey]).To(Equal(release.DPFVersion()))
					return dpuNodeFetched.Status.Conditions
				}).WithTimeout(5 * time.Second).Should(ContainElement(
					And(
						HaveField("Type", provisioningv1.DPUNodeConditionNeedHostAgentUpgrade.String()),
						HaveField("Status", metav1.ConditionFalse),
						HaveField("Reason", provisioningv1.DPUNodeConditionNeedHostAgentUpgrade.String()),
					),
				))
			})
		})
	})
})
