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
	"fmt"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	operatorcontroller "github.com/nvidia/doca-platform/internal/operator/controllers"
	dpusetcontroller "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpuset"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	testutils "github.com/nvidia/doca-platform/test/utils"

	"github.com/fluxcd/pkg/runtime/patch"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

var _ = Describe("DPUSet", func() {
	const (
		DefaultSerialNumberPrefix = "MT25066004C"
		DefaultPCIAddress         = "0000-04-00"
		DefaultDPFOperatorConfig  = "operator-config-test"
	)

	var (
		testNS        *corev1.Namespace
		testDPUDevice *provisioningv1.DPUDevice
		testDPUNode   *provisioningv1.DPUNode
		dpuDeviceName string
		dpuNodeName   string
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
				Strategy: provisioningv1.DPUSetStrategy{
					Type: provisioningv1.OnDeleteStrategyType,
				},
				DPUTemplate: provisioningv1.DPUTemplate{
					Spec: provisioningv1.DPUTemplateSpec{
						BFB:        provisioningv1.BFBReference{Name: "test-bfb"},
						DPUFlavor:  "test-flavor",
						NodeEffect: provisioningv1.NodeEffect{Action: provisioningv1.Action{NoEffect: ptr.To(true)}},
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
					cutil.DPUDevicePCIAddressLabel:  pciAddress,
					cutil.DPUDeviceNumOfPFsLabel:    "2",
					cutil.DPUDevicePF0NameLabel:     "pf0",
					provisioningv1.DPUNodeNameLabel: dpuNodeName,
				},
			},
			Spec: provisioningv1.DPUDeviceSpec{
				SerialNumber: DefaultSerialNumberPrefix + utilrand.String(5),
				NumberOfPFs:  ptr.To(2),
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

		// Generate unique names for each test run to avoid conflicts
		dpuDeviceName = fmt.Sprintf("dpudevice-test-%s", utilrand.String(5))
		dpuNodeName = fmt.Sprintf("node-%s", utilrand.String(5))

		By("creating the dpfoperatorconfig")
		_ = createDPFOperatorConfig(ctx, DefaultDPFOperatorConfig)

		// DPUDevices are required to be created before DPUNodes due to the lack of dpudevice-controller, which will be implemented in the next release
		By("creating the DPUDevice")
		testDPUDevice = createDPUDevice(ctx, testNS.Name, dpuDeviceName, DefaultPCIAddress, dpuNodeName)

		By("creating the DPUNode")
		testDPUNode = createDPUNode(ctx, testNS.Name, dpuNodeName, []string{testDPUDevice.Name})
	})

	AfterEach(func() {
		By("deleting the DPUNode")
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, testDPUNode))).To(Succeed())

		By("deleting the DPUDevice")
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &provisioningv1.DPUDevice{ObjectMeta: metav1.ObjectMeta{Namespace: testNS.GetName(), Name: dpuDeviceName}}))).To(Succeed())
		Expect(k8sClient.DeleteAllOf(ctx, &provisioningv1.DPUSet{}, client.InNamespace(testNS.Name))).To(Succeed())

		By("deleting the dpfoperatorconfig")
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &operatorv1.DPFOperatorConfig{ObjectMeta: metav1.ObjectMeta{Namespace: operatorcontroller.DefaultDPFOperatorConfigSingletonNamespace, Name: DefaultDPFOperatorConfig}}))).To(Succeed())

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
			obj.Spec.DPUDeviceSelector = &metav1.LabelSelector{
				MatchLabels: map[string]string{
					cutil.DPUDevicePCIAddressLabel: DefaultPCIAddress,
				},
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
			obj.Spec.DPUDeviceSelector = &metav1.LabelSelector{
				MatchLabels: map[string]string{
					cutil.DPUDevicePCIAddressLabel: DefaultPCIAddress,
				},
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
			dpuDevice5 := createDPUDevice(ctx, testNS.Name, "dpu-device5", "0000-55-00", dpuNodeName)
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, dpuDevice5))).To(Succeed())
			})
			dpuDevice6 := createDPUDevice(ctx, testNS.Name, "dpu-device6", "0000-66-00", dpuNodeName)
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

			By("checking DPU controller reconcile added finalizer to DPUDevice")
			dpuDeviceFetched := &provisioningv1.DPUDevice{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(testDPUDevice), dpuDeviceFetched)).To(Succeed())
				g.Expect(controllerutil.ContainsFinalizer(dpuDeviceFetched, provisioningv1.DPUDeviceFinalizer)).To(BeTrue())
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
			obj.Spec.DPUTemplate.Spec.NodeEffect = provisioningv1.NodeEffect{
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
			obj.Spec.DPUTemplate.Spec.NodeEffect = provisioningv1.NodeEffect{
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
				g.Expect(dpu.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange).To(Equal(ptr.To(true)))
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})

		It("DPUSet: should support updating ApplyOnLabelChange from true to false after DPU provisioning", func() {
			By("creating dpuset with ApplyOnLabelChange set to true")
			obj := createDPUSet("obj-dpuset")
			obj.Spec.DPUTemplate.Spec.NodeEffect = provisioningv1.NodeEffect{
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
				g.Expect(dpu.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange).To(Equal(ptr.To(false)))
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})

		It("DPUSet: should support updating DPU NodeMaintenanceAdditionalRequestors when DPUSet changes", func() {
			By("creating dpuset with NodeMaintenanceAdditionalRequestors")
			obj := createDPUSet("obj-dpuset")
			obj.Spec.DPUTemplate.Spec.NodeEffect = provisioningv1.NodeEffect{
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
				g.Expect(dpu.Spec.NodeEffect.UpgradePolicy.NodeMaintenanceAdditionalRequestors).To(ContainElements(
					"test-requestor-1",
					"test-requestor-2",
					"test-requestor-3",
				))
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})

		It("DPUSet: should propagate NodeEffect from DPUSet to DPU and handle updates", func() {
			By("creating dpuset with NoEffect NodeEffect")
			obj := createDPUSet("obj-dpuset")
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, obj))).To(Succeed())
			})

			dpuList := &provisioningv1.DPUList{}

			By("checking initial DPU is created with the specified NodeEffect")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.List(ctx, dpuList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuList.Items).To(HaveLen(1))

				dpu := dpuList.Items[0]
				g.Expect(dpu.Spec.NodeEffect.NoEffect).To(Equal(ptr.To(true)))
			}).WithTimeout(10 * time.Second).Should(Succeed())

			By("updating DPUSet to add NodeEffect with ApplyOnLabelChange=true")
			patcher := patch.NewSerialPatcher(obj, k8sClient)
			obj.Spec.DPUTemplate.Spec.NodeEffect = provisioningv1.NodeEffect{
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
				g.Expect(dpu.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange).To(Equal(ptr.To(true)))
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})

		It("DPUSet: should handle DPU with NodeEffect but nil ApplyOnLabelChange initially", func() {
			By("creating dpuset with NodeEffect but no ApplyOnLabelChange")
			obj := createDPUSet("obj-dpuset")
			obj.Spec.DPUTemplate.Spec.NodeEffect = provisioningv1.NodeEffect{
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
				g.Expect(dpu.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange).To(Equal(ptr.To(true)))
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})

		It("DPUSet: should handle no-op updates (same value)", func() {
			By("creating dpuset with ApplyOnLabelChange set to true")
			obj := createDPUSet("obj-dpuset")
			obj.Spec.DPUTemplate.Spec.NodeEffect = provisioningv1.NodeEffect{
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
				g.Expect(dpu.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange).To(Equal(ptr.To(true)))
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})

		It("DPUSet: should update ApplyOnLabelChange for multiple DPUs", func() {
			By("creating additional DPUDevices")
			dpuDevice2 := createDPUDevice(ctx, testNS.Name, "dpu-device2", "0000-55-00", dpuNodeName)
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, dpuDevice2))).To(Succeed())
			})
			dpuDevice3 := createDPUDevice(ctx, testNS.Name, "dpu-device3", "0000-66-00", dpuNodeName)
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
			obj.Spec.DPUTemplate.Spec.NodeEffect = provisioningv1.NodeEffect{
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
					g.Expect(dpu.Spec.NodeEffect.UpgradePolicy.ApplyOnLabelChange).To(Equal(ptr.To(true)))
				}
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})

		It("DPUSet: should propagate DPUFlavor and SecureBoot fields to created DPUs", func() {
			By("creating dpuset with custom dpuFlavor and SecureBoot enabled")
			obj := createDPUSet("obj-dpuset")
			obj.Spec.DPUTemplate.Spec.DPUFlavor = "custom-flavor"
			obj.Spec.DPUTemplate.Spec.SecureBoot = ptr.To(true)
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, obj))).To(Succeed())
			})

			dpuList := &provisioningv1.DPUList{}

			By("checking DPU is created with correct DPUFlavor and SecureBoot")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.List(ctx, dpuList, client.InNamespace(testNS.Name))).To(Succeed())
				g.Expect(dpuList.Items).To(HaveLen(1))

				dpu := dpuList.Items[0]
				g.Expect(dpu.Spec.DPUFlavor).To(Equal("custom-flavor"))
				g.Expect(dpu.Spec.SecureBoot).NotTo(BeNil())
				g.Expect(*dpu.Spec.SecureBoot).To(BeTrue())
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})

		It("DPUSet: should reject AstraEnabled in GNOI install interface suite", func() {
			By("creating dpuset with AstraEnabled enabled")
			obj := createDPUSet("obj-dpuset")
			obj.Spec.DPUTemplate.Spec.AstraEnabled = ptr.To(true)
			err := k8sClient.Create(ctx, obj)
			Expect(err).To(HaveOccurred())
		})

		It("DPUSet: should update NodeEffect Action from Taint to Drain", func() {
			By("creating dpuset with Taint nodeEffect")
			obj := createDPUSet("obj-dpuset")
			obj.Spec.DPUTemplate.Spec.NodeEffect = provisioningv1.NodeEffect{
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
					ObservedGeneration: createdDPU.Generation,
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
			obj.Spec.DPUTemplate.Spec.NodeEffect = provisioningv1.NodeEffect{
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
				g.Expect(dpu.Spec.NodeEffect.Drain).ToNot(BeNil())
				g.Expect(dpu.Spec.NodeEffect.Drain).To(Equal(ptr.To(true)))
				g.Expect(dpu.Spec.NodeEffect.Taint).To(BeNil())
			}).WithTimeout(10 * time.Second).Should(Succeed())
		})
		// TODO: add more test cases
	})

	Context("updateDPUs function", func() {
		var (
			testNamespace string
			dpuSet        *provisioningv1.DPUSet
			dpu           *provisioningv1.DPU
		)

		BeforeEach(func() {
			testNamespace = "dpuset-test-" + utilrand.String(5)
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: testNamespace,
				},
			}
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())

			DeferCleanup(func() {
				// Best effort cleanup - don't block on namespace deletion
				_ = k8sClient.Delete(ctx, ns)
			})
		})

		It("should update dpuMap with patched object when NodeEffect Action changes", func() {
			nodeName := "node-" + utilrand.String(5)
			deviceName := "device-" + utilrand.String(5)

			By("Creating a DPUNode")
			_ = createDPUNode(ctx, testNamespace, nodeName, []string{deviceName})

			By("Creating a DPUDevice")
			_ = createDPUDevice(ctx, testNamespace, deviceName, "0000-05-00", nodeName)

			By("Creating a DPUSet with Drain action")
			dpuSet = &provisioningv1.DPUSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpuset-" + utilrand.String(5),
					Namespace: testNamespace,
				},
				Spec: provisioningv1.DPUSetSpec{
					Strategy: provisioningv1.DPUSetStrategy{
						Type: provisioningv1.OnDeleteStrategyType,
					},
					DPUTemplate: provisioningv1.DPUTemplate{
						Spec: provisioningv1.DPUTemplateSpec{
							BFB:       provisioningv1.BFBReference{Name: "test-bfb"},
							DPUFlavor: "test-flavor",
							NodeEffect: provisioningv1.NodeEffect{
								Action: provisioningv1.Action{
									Drain: ptr.To(true),
								},
								UpgradePolicy: provisioningv1.UpgradePolicy{
									ApplyOnLabelChange: ptr.To(false),
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, dpuSet)).To(Succeed())

			By("Waiting for DPUSet to be reconciled and get its hash computed")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dpuSet), dpuSet)
				g.Expect(err).NotTo(HaveOccurred())
				_, exists := dpuSet.GetLabels()[cutil.DPUSetDPUTemplateSpecHashLabelKey]
				g.Expect(exists).To(BeTrue(), "DPUSet should have hash label")
			}).WithTimeout(5 * time.Second).WithPolling(100 * time.Millisecond).Should(Succeed())

			By("Waiting for auto-created DPU and updating it to desired test state")
			dpuName := cutil.GenerateDPUName(nodeName, deviceName)
			dpu = &provisioningv1.DPU{}
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: dpuName}, dpu)
				g.Expect(err).NotTo(HaveOccurred())
			}).WithTimeout(5 * time.Second).WithPolling(100 * time.Millisecond).Should(Succeed())

			By("Updating DPU spec to have NoEffect action (different from DPUSet's Drain)")
			patch := client.MergeFrom(dpu.DeepCopy())
			dpu.Labels[cutil.DPUSetDPUTemplateSpecHashLabelKey] = "oldhash456"
			dpu.Spec.NodeEffect = provisioningv1.NodeEffect{
				Action: provisioningv1.Action{
					NoEffect: ptr.To(true), // Different from DPUSet's Drain
				},
				UpgradePolicy: provisioningv1.UpgradePolicy{
					ApplyOnLabelChange: ptr.To(false),
				},
			}
			Expect(k8sClient.Patch(ctx, dpu, patch)).To(Succeed())

			By("Waiting for DPU spec update to complete")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: dpuName}, dpu)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dpu.Spec.NodeEffect.NoEffect).To(Equal(ptr.To(true)))
			}).WithTimeout(5 * time.Second).WithPolling(100 * time.Millisecond).Should(Succeed())

			By("Updating DPU status to Ready phase")
			statusPatch := client.MergeFrom(dpu.DeepCopy())
			dpu.Status.Phase = provisioningv1.DPUReady
			dpu.Status.ObservedGeneration = dpu.Generation
			Expect(k8sClient.Status().Patch(ctx, dpu, statusPatch)).To(Succeed())

			// Refresh to get the latest state
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: dpuName}, dpu)).To(Succeed())
			initialGeneration := dpu.Generation

			By("Getting dpuMap using the real getDPUsMap function")
			reconciler := &dpusetcontroller.DPUSetReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			dpuMap, err := reconciler.GetDPUsMap(ctx, dpuSet)
			Expect(err).NotTo(HaveOccurred())
			Expect(dpuMap).To(HaveLen(1), "DPU should be in the map")

			By("Calling updateDPUs")
			dpusUpdated, err := reconciler.UpdateDPUs(ctx, dpuSet, dpuMap)
			Expect(err).NotTo(HaveOccurred())
			Expect(dpusUpdated).To(BeTrue(), "DPUs should have been updated")

			By("Calling updateDPUSetStatus with dpusUpdated=true to fetch fresh data")
			err = reconciler.UpdateDPUSetStatus(ctx, dpuSet, dpusUpdated)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying DPUSet status reflects fresh DPU state (not ready because Generation != ObservedGeneration)")
			readyCondition := meta.FindStatusCondition(dpuSet.Status.Conditions, "Ready")
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse),
				"DPUSet should NOT be ready because DPU has unreconciled changes (Generation > ObservedGeneration)")

			By("Fetching fresh DPU to verify it was actually patched")
			freshDPU := &provisioningv1.DPU{}
			err = reconciler.Get(ctx, client.ObjectKey{
				Namespace: dpuSet.Namespace,
				Name:      dpuName,
			}, freshDPU)
			Expect(err).NotTo(HaveOccurred())
			Expect(freshDPU.Generation).To(BeNumerically(">", initialGeneration),
				"Generation should be incremented after patching")
			Expect(freshDPU.Spec.NodeEffect.Drain).To(Equal(ptr.To(true)),
				"Drain action should be updated in the API server")

			By("Simulating DPU reconciliation - updating DPU status to reconcile the changes")
			statusPatch = client.MergeFrom(freshDPU.DeepCopy())
			freshDPU.Status.Phase = provisioningv1.DPUReady
			freshDPU.Status.ObservedGeneration = freshDPU.Generation
			Expect(k8sClient.Status().Patch(ctx, freshDPU, statusPatch)).To(Succeed())

			By("Waiting for cache to see updated DPU status before UpdateDPUSetStatus")
			Eventually(func(g Gomega) {
				dpuMap, err := reconciler.GetDPUsMap(ctx, dpuSet)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dpuMap).To(HaveLen(1))
				for _, d := range dpuMap {
					g.Expect(d.Status.Phase).To(Equal(provisioningv1.DPUReady), "DPU phase should be Ready")
					g.Expect(d.Status.ObservedGeneration).To(Equal(d.Generation), "DPU ObservedGeneration should match Generation")
				}
			}).WithTimeout(5 * time.Second).WithPolling(100 * time.Millisecond).Should(Succeed())

			By("Calling updateDPUs again and UpdateDPUSetStatus until DPUSet becomes ready")
			// Retry in Eventually: cache may briefly show a stale DPU (e.g. Generation changed by another
			// controller) so GetDPUsMap inside UpdateDPUSetStatus can see ObservedGeneration != Generation.
			Eventually(func(g Gomega) {
				dpuMap, err := reconciler.GetDPUsMap(ctx, dpuSet)
				g.Expect(err).NotTo(HaveOccurred())
				dpusUpdated, err := reconciler.UpdateDPUs(ctx, dpuSet, dpuMap)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dpusUpdated).To(BeFalse(), "No DPU updates should be needed now")
				err = reconciler.UpdateDPUSetStatus(ctx, dpuSet, dpusUpdated)
				g.Expect(err).NotTo(HaveOccurred())
				readyCondition := meta.FindStatusCondition(dpuSet.Status.Conditions, "Ready")
				g.Expect(readyCondition).NotTo(BeNil())
				g.Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue),
					"DPUSet should be ready now that DPU has reconciled the changes and is ready")
			}).WithTimeout(10 * time.Second).WithPolling(200 * time.Millisecond).Should(Succeed())
		})

		It("should NOT update dpuMap when no changes are needed", func() {
			nodeName := "node-" + utilrand.String(5)
			deviceName := "device-" + utilrand.String(5)

			By("Creating a DPUNode")
			_ = createDPUNode(ctx, testNamespace, nodeName, []string{deviceName})

			By("Creating a DPUDevice")
			_ = createDPUDevice(ctx, testNamespace, deviceName, "0000-05-00", nodeName)

			By("Creating a DPUSet with ApplyOnLabelChange=false")
			dpuSet = &provisioningv1.DPUSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-dpuset-" + utilrand.String(5),
					Namespace: testNamespace,
				},
				Spec: provisioningv1.DPUSetSpec{
					Strategy: provisioningv1.DPUSetStrategy{
						Type: provisioningv1.OnDeleteStrategyType,
					},
					DPUTemplate: provisioningv1.DPUTemplate{
						Spec: provisioningv1.DPUTemplateSpec{
							BFB:       provisioningv1.BFBReference{Name: "test-bfb"},
							DPUFlavor: "test-flavor",
							NodeEffect: provisioningv1.NodeEffect{
								Action: provisioningv1.Action{
									NoEffect: ptr.To(true),
								},
								UpgradePolicy: provisioningv1.UpgradePolicy{
									ApplyOnLabelChange: ptr.To(false),
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, dpuSet)).To(Succeed())

			By("Waiting for DPUSet to be reconciled and get its hash computed")
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dpuSet), dpuSet)
				g.Expect(err).NotTo(HaveOccurred())
				_, exists := dpuSet.GetLabels()[cutil.DPUSetDPUTemplateSpecHashLabelKey]
				g.Expect(exists).To(BeTrue(), "DPUSet should have hash label")
			}).WithTimeout(5 * time.Second).WithPolling(100 * time.Millisecond).Should(Succeed())

			By("Waiting for auto-created DPU (already has matching config - no changes needed)")
			dpuName := cutil.GenerateDPUName(nodeName, deviceName)
			dpu = &provisioningv1.DPU{}
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: dpuName}, dpu)
				g.Expect(err).NotTo(HaveOccurred())
			}).WithTimeout(5 * time.Second).WithPolling(100 * time.Millisecond).Should(Succeed())

			By("Verifying DPU has correct config (same as DPUSet - no update needed)")
			// The auto-created DPU should already have the same ApplyOnLabelChange and hash as the DPUSet
			// This test verifies that when there are no changes, updateDPUs doesn't patch

			By("Updating DPU status to Ready phase")
			statusPatch := client.MergeFrom(dpu.DeepCopy())
			dpu.Status.Phase = provisioningv1.DPUReady
			dpu.Status.ObservedGeneration = dpu.Generation
			Expect(k8sClient.Status().Patch(ctx, dpu, statusPatch)).To(Succeed())

			By("Waiting for cache to see updated DPU status before GetDPUsMap/UpdateDPUSetStatus")
			reconciler := &dpusetcontroller.DPUSetReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			Eventually(func(g Gomega) {
				dpuMap, err := reconciler.GetDPUsMap(ctx, dpuSet)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dpuMap).To(HaveLen(1))
				for _, d := range dpuMap {
					g.Expect(d.Status.Phase).To(Equal(provisioningv1.DPUReady), "DPU phase should be Ready")
					g.Expect(d.Status.ObservedGeneration).To(Equal(d.Generation), "DPU ObservedGeneration should match Generation")
				}
			}).WithTimeout(5 * time.Second).WithPolling(100 * time.Millisecond).Should(Succeed())

			// Refresh to get the latest state
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: dpuName}, dpu)).To(Succeed())
			initialGeneration := dpu.Generation

			By("Getting dpuMap using the real getDPUsMap function")
			dpuMap, err := reconciler.GetDPUsMap(ctx, dpuSet)
			Expect(err).NotTo(HaveOccurred())
			Expect(dpuMap).To(HaveLen(1), "DPU should be in the map")

			By("Calling updateDPUs")
			dpusUpdated, err := reconciler.UpdateDPUs(ctx, dpuSet, dpuMap)
			Expect(err).NotTo(HaveOccurred())
			Expect(dpusUpdated).To(BeFalse(), "DPUs should NOT have been updated when no changes are needed")

			By("Calling updateDPUSetStatus with dpusUpdated=false to use cached data")
			err = reconciler.UpdateDPUSetStatus(ctx, dpuSet, dpusUpdated)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying DPUSet status is correct (should use cached data since dpusUpdated=false)")
			// Since DPU is ready and no updates were made, DPUSet should be ready
			readyCondition := meta.FindStatusCondition(dpuSet.Status.Conditions, "Ready")
			Expect(readyCondition).NotTo(BeNil())
			// DPU Phase is Ready and Generation==ObservedGeneration, so DPUSet should be ready
			Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue),
				"DPUSet should be ready when DPU is ready and no updates needed")

			By("Verifying DPU Generation remained unchanged")
			freshDPU := &provisioningv1.DPU{}
			err = reconciler.Get(ctx, client.ObjectKey{
				Namespace: dpuSet.Namespace,
				Name:      dpuName,
			}, freshDPU)
			Expect(err).NotTo(HaveOccurred())
			Expect(freshDPU.Generation).To(Equal(initialGeneration),
				"Generation should not change when no update is needed")
		})
	})
})
