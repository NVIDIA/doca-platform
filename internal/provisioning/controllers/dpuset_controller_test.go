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
			Spec:   provisioningv1.DPUSetSpec{},
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
			Expect(testutils.CleanupAndWait(ctx, k8sClient, testDPUDevice)).To(Succeed())
			Expect(testutils.CleanupAndWait(ctx, k8sClient, testDPUNode)).To(Succeed())
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

		It("DPUSet: should be removed in case the DPUDevice disappeared", func() {
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

			By("removing the DPUDevice")
			Expect(testutils.CleanupAndWait(ctx, k8sClient, testDPUDevice)).To(Succeed())

			By("checking a DPU does not exist")
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

		// TODO: add more test cases
	})
})
