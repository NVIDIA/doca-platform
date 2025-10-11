/*
Copyright 2024 NVIDIA

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

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	testutils "github.com/nvidia/doca-platform/test/utils"

	"github.com/fluxcd/pkg/runtime/patch"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("DPUSet", func() {
	const (
		dpuDeviceName       = "dpudevice-test"
		DefaultPCIAddress   = "0000-04-00"
		DPUNodeName         = "node0"
		DefaultSerialNumber = "MT25066004C7"
	)

	var (
		testNS        *corev1.Namespace
		testDPUDevice *provisioningv1.DPUDevice
		testDPUNode   *provisioningv1.DPUNode
	)

	var getObjKey = func(obj *provisioningv1.DPUSet) types.NamespacedName {
		return types.NamespacedName{
			Name:      obj.Name,
			Namespace: obj.Namespace,
		}
	}

	var createDPUSet = func(name string) *provisioningv1.DPUSet {
		return &provisioningv1.DPUSet{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: name,
				Namespace:    testNS.Name,
			},
			Spec: provisioningv1.DPUSetSpec{
				DPUTemplate: provisioningv1.DPUTemplate{
					Spec: provisioningv1.DPUTemplateSpec{
						DPUFlavor: "test-flavor",
					},
				},
			},
			Status: provisioningv1.DPUSetStatus{},
		}
	}

	var createDPUNode = func(ctx context.Context, namespace string, name string, devices []string) *provisioningv1.DPUNode {
		node := &provisioningv1.DPUNode{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      name,
			},
			Spec: provisioningv1.DPUNodeSpec{
				DPUs: []provisioningv1.DPURef{},
			},
		}
		for _, device := range devices {
			node.Spec.DPUs = append(node.Spec.DPUs, provisioningv1.DPURef{
				Name: device,
			})
		}
		Expect(k8sClient.Create(ctx, node)).To(Succeed())
		return node
	}

	var createDPUDevice = func(ctx context.Context, namespace string, name string, pciAddress string, dpuNodeName string) *provisioningv1.DPUDevice {
		dpuDevice := &provisioningv1.DPUDevice{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      name,
				Labels: map[string]string{
					// these labels should have been added by the DPUNode controller.
					cutil.DPUDevicePCIAddressLabel: pciAddress,
					cutil.DPUDeviceNumOfPFsLabel:   "2",
					cutil.DPUDevicePF0NameLabel:    "pf0",
					cutil.DPUNodeNameLabel:         dpuNodeName,
				},
			},
			Spec: provisioningv1.DPUDeviceSpec{
				SerialNumber: DefaultSerialNumber,
				NumberOfPFs:  ptr.To(2),
				PF0Name:      ptr.To("pf0"),
			},
		}
		Expect(k8sClient.Create(ctx, dpuDevice)).NotTo(HaveOccurred())
		patch := client.MergeFrom(dpuDevice.DeepCopy())
		dpuDevice.Status.PCIAddress = ptr.To(pciAddress)
		Expect(k8sClient.Status().Patch(ctx, dpuDevice, patch)).To(Succeed())
		return dpuDevice
	}

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
		testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "dpuset-controller-test"}}
		Eventually(func() error {
			return k8sClient.Create(ctx, testNS)
		}).WithTimeout(10 * time.Second).Should(Succeed())

		// DPUDevices are required to be created before DPUNodes due to the lack of dpudevice-controller, which will be implemented in the next release
		By("creating the DPUDevice")
		testDPUDevice = createDPUDevice(ctx, testNS.Name, dpuDeviceName, DefaultPCIAddress, DPUNodeName)

		By("creating the DPUNode")
		testDPUNode = createDPUNode(ctx, testNS.Name, DPUNodeName, []string{testDPUDevice.Name})
	})

	AfterEach(func() {
		By("deleting the DPUNode")
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, testDPUNode))).To(Succeed())

		By("deleting the DPUDevice")
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &provisioningv1.DPUDevice{ObjectMeta: metav1.ObjectMeta{Namespace: testNS.GetName(), Name: dpuDeviceName}}))).To(Succeed())
		Expect(k8sClient.DeleteAllOf(ctx, &provisioningv1.DPUSet{}, client.InNamespace(testNS.Name))).To(Succeed())

		By("deleting the namespace")
		Expect(k8sClient.Delete(ctx, testNS)).To(Succeed())
	})

	Context("obj test context", func() {
		ctx := context.Background()

		It("DPUSet: create and delete", func() {
			By("creating the obj")
			obj := createDPUSet("obj-dpuset")
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, obj))).To(Succeed())
			})

			objFetched := &provisioningv1.DPUSet{}

			By("checking the finalizer")
			Eventually(func(g Gomega) []string {
				g.Expect(k8sClient.Get(ctx, getObjKey(obj), objFetched)).To(Succeed())
				return objFetched.Finalizers
			}).WithTimeout(10 * time.Second).Should(ConsistOf([]string{provisioningv1.DPUSetFinalizer}))
		})

		It("DPUSet: should create single DPU - no dpuNodeSelector and no dpuSelector", func() {
			By("creating the obj")
			obj := createDPUSet("obj-dpuset")
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, obj))).To(Succeed())
			})

			dpuList := &provisioningv1.DPUList{}

			By("checking a DPU is created for the DPUDevice")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.List(ctx, dpuList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuList.Items).To(HaveLen(1))
				g.Expect(dpuList.Items[0].Labels).To(HaveKeyWithValue(cutil.DPUSetNameLabel, obj.Name))
				g.Expect(dpuList.Items[0].Labels).To(HaveKeyWithValue(cutil.DPUSetNamespaceLabel, obj.Namespace))
			}).WithTimeout(10 * time.Second).Should(Succeed())
			Expect(dpuList.Items[0].GetOwnerReferences()).To(ContainElements(metav1.OwnerReference{
				APIVersion:         provisioningv1.GroupVersion.String(),
				Kind:               provisioningv1.DPUSetKind,
				Name:               obj.Name,
				UID:                obj.GetUID(),
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(true),
			}))
		})

		It("DPUSet: should create DPU when DPUSet has maximum name length", func() {
			By("creating the obj")
			obj := createDPUSet("")
			obj.Name = utilrand.String(63)
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, obj))).To(Succeed())
			})

			dpuList := &provisioningv1.DPUList{}

			By("checking a DPU is created for the DPUDevice")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.List(ctx, dpuList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuList.Items).To(HaveLen(1))
				g.Expect(dpuList.Items[0].Labels).To(HaveKeyWithValue(cutil.DPUSetNameLabel, obj.Name))
				g.Expect(dpuList.Items[0].Labels).To(HaveKeyWithValue(cutil.DPUSetNamespaceLabel, obj.Namespace))
			}).WithTimeout(10 * time.Second).Should(Succeed())
			Expect(dpuList.Items[0].GetOwnerReferences()).To(ContainElements(metav1.OwnerReference{
				APIVersion:         provisioningv1.GroupVersion.String(),
				Kind:               provisioningv1.DPUSetKind,
				Name:               obj.Name,
				UID:                obj.GetUID(),
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(true),
			}))
		})

		It("DPUSet: should create DPU when DPUDevice and DPUNode have maximum name langth", func() {
			By("deleting the default DPUNode and DPUDevice since they are not needed for this test")
			Expect(testutils.CleanupWithFinalizerRemovalAndWait(ctx, k8sClient, testDPUDevice)).To(Succeed())
			Expect(testutils.CleanupWithFinalizerRemovalAndWait(ctx, k8sClient, testDPUNode)).To(Succeed())
			By("creating the DPUDevice and DPUNode with maximum name length")
			dpuNodeName := utilrand.String(48)
			dpuDeviceMaxLength := createDPUDevice(ctx, testNS.Name, utilrand.String(63), "0000-55-00", dpuNodeName)
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, dpuDeviceMaxLength))).To(Succeed())
			})
			dpuNodeMaxLength := createDPUNode(ctx, testNS.Name, dpuNodeName, []string{dpuDeviceMaxLength.Name})
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, dpuNodeMaxLength))).To(Succeed())
			})

			By("creating the obj")
			obj := createDPUSet("obj-dpuset")
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, obj))).To(Succeed())
			})

			dpuList := &provisioningv1.DPUList{}

			By("checking a DPU is created for the DPUDevice")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.List(ctx, dpuList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuList.Items).To(HaveLen(1))
				g.Expect(dpuList.Items[0].Labels).To(HaveKeyWithValue(cutil.DPUSetNameLabel, obj.Name))
				g.Expect(dpuList.Items[0].Labels).To(HaveKeyWithValue(cutil.DPUSetNamespaceLabel, obj.Namespace))
			}).WithTimeout(10 * time.Second).Should(Succeed())
			Expect(dpuList.Items[0].GetOwnerReferences()).To(ContainElements(metav1.OwnerReference{
				APIVersion:         provisioningv1.GroupVersion.String(),
				Kind:               provisioningv1.DPUSetKind,
				Name:               obj.Name,
				UID:                obj.GetUID(),
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(true),
			}))
		})

		It("DPUSet: should create single DPU - with dpuNodeSelector", func() {
			By("label the DPUNode with strong-node=true")
			patcher := patch.NewSerialPatcher(testDPUNode, k8sClient)
			testDPUNode.Labels = map[string]string{"strong-node": "true"}
			Expect(patcher.Patch(ctx, testDPUNode)).To(Succeed())

			By("creating dpuset with dpuNodeSelector")
			obj := createDPUSet("obj-dpuset")
			obj.Spec.DPUNodeSelector = &metav1.LabelSelector{
				MatchLabels: map[string]string{"strong-node": "true"},
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, obj))).To(Succeed())
			})

			dpuList := &provisioningv1.DPUList{}

			By("checking a DPU is created for the dpuDevice1")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.List(ctx, dpuList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuList.Items).To(HaveLen(1))
				g.Expect(dpuList.Items[0].Labels).To(HaveKeyWithValue(cutil.DPUSetNameLabel, obj.Name))
				g.Expect(dpuList.Items[0].Labels).To(HaveKeyWithValue(cutil.DPUSetNamespaceLabel, obj.Namespace))
				g.Expect(dpuList.Items[0].Spec.DPUDeviceName).To(Equal(testDPUDevice.Name))
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})

		It("DPUSet: should create single DPU - with dpuSelector", func() {
			By("creating dpuset with dpuSelector")
			obj := createDPUSet("obj-dpuset")
			obj.Spec.DPUSelector = map[string]string{
				cutil.DPUDevicePCIAddressLabel: DefaultPCIAddress,
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, obj))).To(Succeed())
			})

			dpuList := &provisioningv1.DPUList{}

			By("checking a DPU is created for the DPUDevice")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.List(ctx, dpuList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuList.Items).To(HaveLen(1))
				g.Expect(dpuList.Items[0].Labels).To(HaveKeyWithValue(cutil.DPUSetNameLabel, obj.Name))
				g.Expect(dpuList.Items[0].Labels).To(HaveKeyWithValue(cutil.DPUSetNamespaceLabel, obj.Namespace))
				g.Expect(dpuList.Items[0].Spec.DPUDeviceName).To(Equal(testDPUDevice.Name))
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})

		It("DPUSet: should create single DPU - with dpuNodeSelector and dpuSelector", func() {
			By("label the DPUNode with strong-node=true")
			patcher := patch.NewSerialPatcher(testDPUNode, k8sClient)
			testDPUNode.Labels = map[string]string{"strong-node": "true"}
			Expect(patcher.Patch(ctx, testDPUNode)).To(Succeed())

			By("creating the obj")
			obj := createDPUSet("obj-dpuset")
			obj.Spec.DPUNodeSelector = &metav1.LabelSelector{
				MatchLabels: map[string]string{"strong-node": "true"},
			}
			obj.Spec.DPUSelector = map[string]string{
				cutil.DPUDevicePCIAddressLabel: DefaultPCIAddress,
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, obj))).To(Succeed())
			})

			dpuList := &provisioningv1.DPUList{}

			By("checking a DPU is created for the DPUDevice")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.List(ctx, dpuList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuList.Items).To(HaveLen(1))
				g.Expect(dpuList.Items[0].Labels).To(HaveKeyWithValue(cutil.DPUDevicePCIAddressLabel, DefaultPCIAddress))
				g.Expect(dpuList.Items[0].Labels).To(HaveKeyWithValue(cutil.DPUSetNameLabel, obj.Name))
				g.Expect(dpuList.Items[0].Labels).To(HaveKeyWithValue(cutil.DPUSetNamespaceLabel, obj.Namespace))
				g.Expect(dpuList.Items[0].Spec.DPUDeviceName).To(Equal(testDPUDevice.Name))
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})

		It("DPUSet: should create few DPUs", func() {
			By("creating few DPUDevices in addition to predefined")
			dpuDevice5 := createDPUDevice(ctx, testNS.Name, "dpu-device5", "0000-55-00", DPUNodeName)
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, dpuDevice5))).To(Succeed())
			})
			dpuDevice6 := createDPUDevice(ctx, testNS.Name, "dpu-device6", "0000-66-00", DPUNodeName)
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, dpuDevice6))).To(Succeed())
			})
			patcher := patch.NewSerialPatcher(testDPUNode, k8sClient)
			testDPUNode.Spec.DPUs = append(testDPUNode.Spec.DPUs,
				provisioningv1.DPURef{
					Name: dpuDevice5.Name,
				},
				provisioningv1.DPURef{
					Name: dpuDevice6.Name,
				},
			)
			Expect(patcher.Patch(ctx, testDPUNode)).To(Succeed())

			By("creating the obj")
			obj := createDPUSet("obj-dpuset")
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, obj))).To(Succeed())
			})

			dpuList := &provisioningv1.DPUList{}

			By("checking a DPU is created for the DPUDevice")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.List(ctx, dpuList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuList.Items).To(HaveLen(3))
			}).WithTimeout(10 * time.Second).Should(Succeed())
			Expect(dpuList.Items[0].GetOwnerReferences()).To(ContainElements(metav1.OwnerReference{
				APIVersion:         provisioningv1.GroupVersion.String(),
				Kind:               provisioningv1.DPUSetKind,
				Name:               obj.Name,
				UID:                obj.GetUID(),
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(true),
			}))
			Expect(dpuList.Items[1].GetOwnerReferences()).To(ContainElements(metav1.OwnerReference{
				APIVersion:         provisioningv1.GroupVersion.String(),
				Kind:               provisioningv1.DPUSetKind,
				Name:               obj.Name,
				UID:                obj.GetUID(),
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(true),
			}))
			Expect(dpuList.Items[2].GetOwnerReferences()).To(ContainElements(metav1.OwnerReference{
				APIVersion:         provisioningv1.GroupVersion.String(),
				Kind:               provisioningv1.DPUSetKind,
				Name:               obj.Name,
				UID:                obj.GetUID(),
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(true),
			}))
		})

		It("DPUSet: prevent DPUDevice deletion while DPU is using it", func() {
			By("creating dpuset ")
			obj := createDPUSet("obj-dpuset")
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, obj))).To(Succeed())
			})

			dpuList := &provisioningv1.DPUList{}

			By("checking a DPU is created for the DPUDevice")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.List(ctx, dpuList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuList.Items).To(HaveLen(1))
				// in dpuset-controller, DPUs are named after the corresponding DPUDevices.
				g.Expect(dpuList.Items[0].Name).To(Equal(cutil.GenerateDPUName(testDPUNode.Name, testDPUDevice.Name)))
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("trying to remove the DPUDevice")
			Expect(k8sClient.Delete(ctx, testDPUDevice)).To(Succeed())

			By("checking a DPU still exist")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.List(ctx, dpuList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuList.Items).To(HaveLen(1))
				g.Expect(dpuList.Items[0].Name).To(Equal(cutil.GenerateDPUName(testDPUNode.Name, testDPUDevice.Name)))
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("Removing the DPUSet and DpuDevice")
			Expect(k8sClient.Delete(ctx, obj)).To(Succeed())
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, testDPUDevice))).To(Succeed())

			By("Checking the DPUDevice is removed")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.List(ctx, dpuList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuList.Items).To(BeEmpty())
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})

		It("DPUSet: should be able to recover DPU in case it is disappeared", func() {
			By("creating dpuset ")
			obj := createDPUSet("obj-dpuset")
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, obj))).To(Succeed())
			})

			dpuList := &provisioningv1.DPUList{}
			dpu := &provisioningv1.DPU{}
			// in dpuset-controller, DPUs are named after the corresponding DPUDevices.
			dpuName := cutil.GenerateDPUName(testDPUNode.Name, testDPUDevice.Name)

			By("checking a DPU is created for the DPUDevice")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.List(ctx, dpuList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuList.Items).To(HaveLen(1))
				g.Expect(dpuList.Items[0].Name).To(Equal(dpuName))
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("removing the DPU")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: obj.Namespace, Name: dpuName}, dpu)).To(Succeed())
			Expect(k8sClient.Delete(ctx, dpu)).To(Succeed())

			By("checking a new DPU is created for the DPUDevice")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.List(ctx, dpuList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuList.Items).To(HaveLen(1))
				g.Expect(dpuList.Items[0].Name).To(Equal(dpuName))
				// Compare UIDs
				g.Expect(dpuList.Items[0].GetUID()).NotTo(Equal(dpu.GetUID()))
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})

		It("DPUSet: fail to create with name exceeding the maximum length", func() {
			By("creating the obj")
			obj := createDPUSet("")
			obj.Name = utilrand.String(64)
			Expect(k8sClient.Create(ctx, obj)).To(HaveOccurred())
		})

		It("DPUSet: should propagate NodeEffect fields with NoEffect", func() {
			By("creating dpuset with NoEffect and additional fields")
			obj := createDPUSet("obj-dpuset")
			obj.Spec.DPUTemplate.Spec.NodeEffect = &provisioningv1.NodeEffect{
				// Only use NoEffect as the NodeEffect type
				Action: provisioningv1.Action{
					NoEffect: ptr.To(true),
				},
				// Test additional fields that can be set with any NodeEffect type
				UpgradePolicy: provisioningv1.UpgradePolicy{
					ApplyOnLabelChange: ptr.To(true),
					NodeMaintenanceAdditionalRequestors: []string{
						"test-requestor-1",
						"test-requestor-2",
						"test-requestor-3",
					},
				},
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, obj))).To(Succeed())
			})

			dpuList := &provisioningv1.DPUList{}

			By("checking NodeEffect fields are propagated correctly")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.List(ctx, dpuList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuList.Items).To(HaveLen(1))

				dpu := dpuList.Items[0]
				g.Expect(dpu.Spec.NodeEffect).ToNot(BeNil())

				// Verify NoEffect is set correctly
				g.Expect(dpu.Spec.NodeEffect.NoEffect).To(Equal(ptr.To(true)))

				// Verify other NodeEffect types are not set
				g.Expect(dpu.Spec.NodeEffect.Taint).To(BeNil())
				g.Expect(dpu.Spec.NodeEffect.CustomLabel).To(BeEmpty())
				g.Expect(dpu.Spec.NodeEffect.Drain).To(BeNil())
				g.Expect(dpu.Spec.NodeEffect.CustomAction).To(BeNil())
				g.Expect(dpu.Spec.NodeEffect.Hold).To(BeNil())

				// Verify additional fields are propagated
				g.Expect(dpu.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange).To(Equal(ptr.To(true)))
				g.Expect(dpu.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors).To(HaveLen(3))
				g.Expect(dpu.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors).To(ContainElements(
					"test-requestor-1",
					"test-requestor-2",
					"test-requestor-3",
				))
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})

		It("DPUSet: should support updating ApplyOnLabelChange from false to true after DPU provisioning", func() {
			By("creating dpuset with ApplyOnLabelChange set to false")
			obj := createDPUSet("obj-dpuset")
			obj.Spec.DPUTemplate.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					NoEffect: ptr.To(true),
				},
				UpgradePolicy: provisioningv1.UpgradePolicy{
					ApplyOnLabelChange: ptr.To(false),
				},
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, obj))).To(Succeed())
			})

			dpuList := &provisioningv1.DPUList{}

			By("checking initial DPU is created with ApplyOnLabelChange=false")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.List(ctx, dpuList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuList.Items).To(HaveLen(1))

				dpu := dpuList.Items[0]
				g.Expect(dpu.Spec.NodeEffect).ToNot(BeNil())
				g.Expect(dpu.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange).To(Equal(ptr.To(false)))
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("updating DPUSet to set ApplyOnLabelChange to true")
			patcher := patch.NewSerialPatcher(obj, k8sClient)
			obj.Spec.DPUTemplate.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange = ptr.To(true)
			Expect(patcher.Patch(ctx, obj)).To(Succeed())

			By("checking DPU is updated with ApplyOnLabelChange=true")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.List(ctx, dpuList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuList.Items).To(HaveLen(1))

				dpu := dpuList.Items[0]
				g.Expect(dpu.Spec.NodeEffect).ToNot(BeNil())
				g.Expect(dpu.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange).To(Equal(ptr.To(true)))
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})

		It("DPUSet: should support updating ApplyOnLabelChange from true to false after DPU provisioning", func() {
			By("creating dpuset with ApplyOnLabelChange set to true")
			obj := createDPUSet("obj-dpuset")
			obj.Spec.DPUTemplate.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					NoEffect: ptr.To(true),
				},
				UpgradePolicy: provisioningv1.UpgradePolicy{
					ApplyOnLabelChange: ptr.To(true),
				},
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, obj))).To(Succeed())
			})

			dpuList := &provisioningv1.DPUList{}

			By("checking initial DPU is created with ApplyOnLabelChange=true")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.List(ctx, dpuList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuList.Items).To(HaveLen(1))

				dpu := dpuList.Items[0]
				g.Expect(dpu.Spec.NodeEffect).ToNot(BeNil())
				g.Expect(dpu.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange).To(Equal(ptr.To(true)))
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("updating DPUSet to set ApplyOnLabelChange to false")
			patcher := patch.NewSerialPatcher(obj, k8sClient)
			obj.Spec.DPUTemplate.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange = ptr.To(false)
			Expect(patcher.Patch(ctx, obj)).To(Succeed())

			By("checking DPU is updated with ApplyOnLabelChange=false")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.List(ctx, dpuList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuList.Items).To(HaveLen(1))

				dpu := dpuList.Items[0]
				g.Expect(dpu.Spec.NodeEffect).ToNot(BeNil())
				g.Expect(dpu.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange).To(Equal(ptr.To(false)))
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})

		It("DPUSet: should support updating DPU NodeMaintenanceAdditionalRequestors when DPUSet changes", func() {
			By("creating dpuset with NodeMaintenanceAdditionalRequestors")
			obj := createDPUSet("obj-dpuset")
			obj.Spec.DPUTemplate.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					NoEffect: ptr.To(true),
				},
				UpgradePolicy: provisioningv1.UpgradePolicy{
					ApplyOnLabelChange:                  ptr.To(true),
					NodeMaintenanceAdditionalRequestors: []string{"test-requestor-1"},
				},
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, obj))).To(Succeed())
			})

			dpuList := &provisioningv1.DPUList{}

			By("checking initial DPU is created with NodeMaintenanceAdditionalRequestors")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.List(ctx, dpuList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuList.Items).To(HaveLen(1))

				dpu := dpuList.Items[0]
				g.Expect(dpu.Spec.NodeEffect).ToNot(BeNil())
				g.Expect(dpu.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors).To(HaveLen(1))
				g.Expect(dpu.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors[0]).To(Equal("test-requestor-1"))
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("updating DPUSet to add NodeMaintenanceAdditionalRequestors")
			patcher := patch.NewSerialPatcher(obj, k8sClient)
			obj.Spec.DPUTemplate.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors = []string{"test-requestor-1", "test-requestor-2", "test-requestor-3"}
			Expect(patcher.Patch(ctx, obj)).To(Succeed())

			By("checking DPU is updated with NodeMaintenanceAdditionalRequestors")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.List(ctx, dpuList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuList.Items).To(HaveLen(1))

				dpu := dpuList.Items[0]
				g.Expect(dpu.Spec.NodeEffect).ToNot(BeNil())
				g.Expect(dpu.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors).To(ContainElements(
					"test-requestor-1",
					"test-requestor-2",
					"test-requestor-3",
				))
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})

		It("DPUSet: should handle DPU with nil NodeEffect initially", func() {
			By("creating dpuset without NodeEffect")
			obj := createDPUSet("obj-dpuset")
			// Don't set NodeEffect - let it be nil
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, obj))).To(Succeed())
			})

			dpuList := &provisioningv1.DPUList{}

			By("checking initial DPU is created with default NodeEffect")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.List(ctx, dpuList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuList.Items).To(HaveLen(1))

				dpu := dpuList.Items[0]
				g.Expect(dpu.Spec.NodeEffect).ToNot(BeNil())
				g.Expect(dpu.Spec.NodeEffect.Drain).To(Equal(ptr.To(true))) // Default value
				g.Expect(dpu.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange).To(Equal(ptr.To(false)))
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("updating DPUSet to add NodeEffect with ApplyOnLabelChange=true")
			patcher := patch.NewSerialPatcher(obj, k8sClient)
			obj.Spec.DPUTemplate.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					NoEffect: ptr.To(true),
				},
				UpgradePolicy: provisioningv1.UpgradePolicy{
					ApplyOnLabelChange: ptr.To(true),
				},
			}
			Expect(patcher.Patch(ctx, obj)).To(Succeed())

			By("checking DPU is updated with ApplyOnLabelChange=true")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.List(ctx, dpuList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuList.Items).To(HaveLen(1))

				dpu := dpuList.Items[0]
				g.Expect(dpu.Spec.NodeEffect).ToNot(BeNil())
				g.Expect(dpu.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange).To(Equal(ptr.To(true)))
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})

		It("DPUSet: should handle DPU with NodeEffect but nil ApplyOnLabelChange initially", func() {
			By("creating dpuset with NodeEffect but no ApplyOnLabelChange")
			obj := createDPUSet("obj-dpuset")
			obj.Spec.DPUTemplate.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					NoEffect: ptr.To(true),
				},
				// ApplyOnLabelChange is nil
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, obj))).To(Succeed())
			})

			dpuList := &provisioningv1.DPUList{}

			By("checking initial DPU is created with nil ApplyOnLabelChange")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.List(ctx, dpuList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuList.Items).To(HaveLen(1))

				dpu := dpuList.Items[0]
				g.Expect(dpu.Spec.NodeEffect).ToNot(BeNil())
				g.Expect(dpu.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange).To(Equal(ptr.To(false)))
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("updating DPUSet to set ApplyOnLabelChange to true")
			patcher := patch.NewSerialPatcher(obj, k8sClient)
			obj.Spec.DPUTemplate.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange = ptr.To(true)
			Expect(patcher.Patch(ctx, obj)).To(Succeed())

			By("checking DPU is updated with ApplyOnLabelChange=true")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.List(ctx, dpuList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuList.Items).To(HaveLen(1))

				dpu := dpuList.Items[0]
				g.Expect(dpu.Spec.NodeEffect).ToNot(BeNil())
				g.Expect(dpu.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange).To(Equal(ptr.To(true)))
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})

		It("DPUSet: should handle no-op updates (same value)", func() {
			By("creating dpuset with ApplyOnLabelChange set to true")
			obj := createDPUSet("obj-dpuset")
			obj.Spec.DPUTemplate.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					NoEffect: ptr.To(true),
				},
				UpgradePolicy: provisioningv1.UpgradePolicy{
					ApplyOnLabelChange: ptr.To(true),
				},
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, obj))).To(Succeed())
			})

			dpuList := &provisioningv1.DPUList{}

			By("checking initial DPU is created with ApplyOnLabelChange=true")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.List(ctx, dpuList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuList.Items).To(HaveLen(1))

				dpu := dpuList.Items[0]
				g.Expect(dpu.Spec.NodeEffect).ToNot(BeNil())
				g.Expect(dpu.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange).To(Equal(ptr.To(true)))
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("updating DPUSet to set ApplyOnLabelChange to true again (no-op)")
			patcher := patch.NewSerialPatcher(obj, k8sClient)
			obj.Spec.DPUTemplate.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange = ptr.To(true) // Same value
			Expect(patcher.Patch(ctx, obj)).To(Succeed())

			By("checking DPU still has ApplyOnLabelChange=true (no change)")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.List(ctx, dpuList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuList.Items).To(HaveLen(1))

				dpu := dpuList.Items[0]
				g.Expect(dpu.Spec.NodeEffect).ToNot(BeNil())
				g.Expect(dpu.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange).To(Equal(ptr.To(true)))
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})

		It("DPUSet: should update ApplyOnLabelChange for multiple DPUs", func() {
			By("creating additional DPUDevices")
			dpuDevice2 := createDPUDevice(ctx, testNS.Name, "dpu-device2", "0000-55-00", DPUNodeName)
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, dpuDevice2))).To(Succeed())
			})
			dpuDevice3 := createDPUDevice(ctx, testNS.Name, "dpu-device3", "0000-66-00", DPUNodeName)
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, dpuDevice3))).To(Succeed())
			})

			By("updating DPUNode to include additional devices")
			patcher := patch.NewSerialPatcher(testDPUNode, k8sClient)
			testDPUNode.Spec.DPUs = append(testDPUNode.Spec.DPUs,
				provisioningv1.DPURef{Name: dpuDevice2.Name},
				provisioningv1.DPURef{Name: dpuDevice3.Name},
			)
			Expect(patcher.Patch(ctx, testDPUNode)).To(Succeed())

			By("creating dpuset with ApplyOnLabelChange set to false")
			obj := createDPUSet("obj-dpuset")
			obj.Spec.DPUTemplate.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					NoEffect: ptr.To(true),
				},
				UpgradePolicy: provisioningv1.UpgradePolicy{
					ApplyOnLabelChange: ptr.To(false),
				},
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, obj))).To(Succeed())
			})

			dpuList := &provisioningv1.DPUList{}

			By("checking all DPUs are created with ApplyOnLabelChange=false")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.List(ctx, dpuList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuList.Items).To(HaveLen(3))

				for _, dpu := range dpuList.Items {
					g.Expect(dpu.Spec.NodeEffect).ToNot(BeNil())
					g.Expect(dpu.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange).To(Equal(ptr.To(false)))
				}
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("updating DPUSet to set ApplyOnLabelChange to true")
			patcher = patch.NewSerialPatcher(obj, k8sClient)
			obj.Spec.DPUTemplate.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange = ptr.To(true)
			Expect(patcher.Patch(ctx, obj)).To(Succeed())

			By("checking all DPUs are updated with ApplyOnLabelChange=true")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.List(ctx, dpuList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuList.Items).To(HaveLen(3))

				for _, dpu := range dpuList.Items {
					g.Expect(dpu.Spec.NodeEffect).ToNot(BeNil())
					g.Expect(dpu.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange).To(Equal(ptr.To(true)))
				}
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})

		It("DPUSet: should propagate DPUFlavor field to created DPUs", func() {
			By("creating dpuset with custom dpuFlavor")
			obj := createDPUSet("obj-dpuset")
			obj.Spec.DPUTemplate.Spec.DPUFlavor = "custom-flavor"
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, obj))).To(Succeed())
			})

			dpuList := &provisioningv1.DPUList{}

			By("checking DPU is created with correct DPUFlavor")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.List(ctx, dpuList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuList.Items).To(HaveLen(1))

				dpu := dpuList.Items[0]
				g.Expect(dpu.Spec.DPUFlavor).To(Equal("custom-flavor"))
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})

		It("DPUSet: should update NodeEffect Action from Taint to Drain", func() {
			By("creating dpuset with Taint nodeEffect")
			obj := createDPUSet("obj-dpuset")
			obj.Spec.DPUTemplate.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					Taint: &corev1.Taint{
						Key:    "test-key",
						Value:  "test-value",
						Effect: corev1.TaintEffectNoSchedule,
					},
				},
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, obj))).To(Succeed())
			})

			dpuList := &provisioningv1.DPUList{}
			var createdDPU *provisioningv1.DPU

			By("checking initial DPU is created with Taint nodeEffect")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.List(ctx, dpuList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuList.Items).To(HaveLen(1))

				dpu := dpuList.Items[0]
				g.Expect(dpu.Spec.NodeEffect).ToNot(BeNil())
				g.Expect(dpu.Spec.NodeEffect.Taint).ToNot(BeNil())
				g.Expect(dpu.Spec.NodeEffect.Taint.Key).To(Equal("test-key"))
				g.Expect(dpu.Spec.NodeEffect.Taint.Value).To(Equal("test-value"))
				g.Expect(dpu.Spec.NodeEffect.Taint.Effect).To(Equal(corev1.TaintEffectNoSchedule))
				g.Expect(dpu.Spec.NodeEffect.Drain).To(BeNil())
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("patching DPU status to Ready state")
			Expect(k8sClient.List(ctx, dpuList, client.InNamespace(testNS.Name))).To(Succeed())
			createdDPU = &dpuList.Items[0]
			patchBase := client.MergeFrom(createdDPU.DeepCopy())
			createdDPU.Status.Phase = provisioningv1.DPUReady
			createdDPU.Status.Conditions = []metav1.Condition{
				{
					Type:               provisioningv1.DPUCondReady.String(),
					Status:             metav1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
					Reason:             "TestReady",
					Message:            "DPU is ready for testing",
				},
			}
			Expect(k8sClient.Status().Patch(ctx, createdDPU, patchBase)).To(Succeed())

			By("verifying DPU is in Ready phase")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(createdDPU), createdDPU)).To(Succeed())
				g.Expect(createdDPU.Status.Phase).To(Equal(provisioningv1.DPUReady))
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("updating DPUSet to change nodeEffect from Taint to Drain")
			patcher := patch.NewSerialPatcher(obj, k8sClient)
			obj.Spec.DPUTemplate.Spec.NodeEffect = &provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					Drain: ptr.To(true),
				},
			}
			Expect(patcher.Patch(ctx, obj)).To(Succeed())

			By("checking DPU is updated with Drain nodeEffect and remains in Ready phase")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.List(ctx, dpuList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuList.Items).To(HaveLen(1))

				dpu := dpuList.Items[0]
				g.Expect(dpu.Status.Phase).To(Equal(provisioningv1.DPUReady))
				g.Expect(dpu.Spec.NodeEffect).ToNot(BeNil())
				g.Expect(dpu.Spec.NodeEffect.Drain).ToNot(BeNil())
				g.Expect(dpu.Spec.NodeEffect.Drain).To(Equal(ptr.To(true)))
				g.Expect(dpu.Spec.NodeEffect.Taint).To(BeNil())
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})
		// TODO: add more test cases
	})
})
