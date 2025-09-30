/*
SPDX-FileCopyrightText: Copyright (c) 2025 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
SPDX-License-Identifier: LicenseRef-NvidiaProprietary

NVIDIA CORPORATION, its affiliates and licensors retain all intellectual
property and proprietary rights in and to this material, related
documentation and any modifications thereto. Any use, reproduction,
disclosure or distribution of this material and related documentation
 without an express license agreement from NVIDIA CORPORATION or
its affiliates is strictly prohibited.
*/

package controllers

import (
	"context"
	"fmt"
	"sync"
	"time"

	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/common"
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/node/nodeutils"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/ovsmodel"
	"github.com/nvidia/doca-platform/pkg/ovsutils"
	networkhelper_mock "github.com/nvidia/doca-platform/pkg/utils/networkhelper/mock"
	testutils "github.com/nvidia/doca-platform/test/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

//nolint:goconst
var _ = Describe("service interface controller", func() {
	var (
		mockCtrl          *gomock.Controller
		cleanupObjects    []client.Object
		sir               *ServiceInterfaceReconciler
		ovsMock           *ovsutils.MockAPI
		networkHelperMock *networkhelper_mock.MockNetworkHelper
		testCtx           context.Context
		testCancelFunc    context.CancelFunc
		testNode          = "test-node"
		wg                sync.WaitGroup
		node              *corev1.Node
		ns                *corev1.Namespace
	)

	BeforeEach(func() {
		testCtx, testCancelFunc = context.WithCancel(suiteCtx)
		wg = sync.WaitGroup{}
		mockCtrl = gomock.NewController(GinkgoT())
		ovsMock = ovsutils.NewMockAPI(mockCtrl)
		networkHelperMock = networkhelper_mock.NewMockNetworkHelper(mockCtrl)

		testManager, err := ctrl.NewManager(cfg,
			ctrl.Options{
				Scheme: scheme.Scheme,
				// Set metrics server bind address to 0 to disable it.
				Metrics: server.Options{
					BindAddress: "0",
				},
				Controller: config.Controller{
					// this is needed since metrics are registered globally by controller runtime for each controller
					// and we want to allow multiple tests initializing the same controller name.
					SkipNameValidation: ptr.To(true),
				},
			})
		Expect(err).ToNot(HaveOccurred())

		sir = &ServiceInterfaceReconciler{
			Client:        testManager.GetClient(),
			Scheme:        testManager.GetScheme(),
			NodeName:      testNode,
			OVS:           ovsMock,
			VFMapping:     getTestVFMapping(),
			NetworkHelper: networkHelperMock,
		}
		Expect(sir.SetupWithManager(testCtx, testManager)).To(Succeed())

		wg.Add(1)
		go func() {
			defer wg.Done()
			defer GinkgoRecover()
			err := testManager.Start(testCtx)
			Expect(err).ToNot(HaveOccurred(), "failed to run manager")
		}()

		// Wait for the cache to be synced
		Eventually(func() bool {
			return testManager.GetCache().WaitForCacheSync(testCtx)
		}).WithTimeout(5*time.Second).WithPolling(100*time.Millisecond).Should(BeTrue(), "cache sync failed")

		By("creating namespace")
		ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "test-ns-"}}
		Expect(testClient.Create(testCtx, ns)).Should(Succeed())

		By("creating node")
		node = getTestNode(testNode)
		Expect(testClient.Create(testCtx, node)).To(Succeed())
		cleanupObjects = append(cleanupObjects, node)
	})

	AfterEach(func() {
		Expect(testutils.CleanupWithFinalizerRemovalAndWait(suiteCtx, testClient, cleanupObjects...)).To(Succeed())
		mockCtrl.Finish()
		testCancelFunc()
		wg.Wait()
	})

	It("reconcile non existing object - consider as deleted", func() {
		nn := k8stypes.NamespacedName{
			Namespace: "non-existing",
			Name:      "non-existing",
		}

		result, err := sir.Reconcile(testCtx, ctrl.Request{NamespacedName: nn})
		Expect(err).To(Succeed())
		Expect(result).To(Equal(ctrl.Result{}))
	})

	It("reconcile successfully on a VF service interface", func() {
		ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(nil)
		ovsMock.EXPECT().SetIfaceExternalIDs(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(nil)
		ovsMock.EXPECT().DelPort(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(nil)

		networkHelperMock.EXPECT().GetVFRepresentorDPU(gomock.Any(), gomock.Any()).AnyTimes().Return("pf0vf2", nil)

		By("creating ServiceInterface")
		si := getTestServiceInterfaceTypeVF("pf0vf2", ns.Name, "test", node.Name, false)
		Expect(testClient.Create(testCtx, si)).To(Succeed())
		cleanupObjects = append(cleanupObjects, si)

		By("verifying ServiceInterface is reconciled successfully")
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(si), si)).To(Succeed())
			g.Expect(si.Annotations).To(HaveKey(common.LSPMACAddressAnnotationKey))
			g.Expect(si.Annotations[common.LSPMACAddressAnnotationKey]).To(Equal("00:00:00:00:00:01"))
			g.Expect(si.ObjectMeta.Finalizers).To(ContainElement(ServiceInterfaceFinalizer))
			g.Expect(si.GetConditions()).To(ContainElement(And(
				HaveField("Type", string(dpuservicev1.ServiceInterfaceReconciled)),
				HaveField("Status", metav1.ConditionTrue),
			)))
			g.Expect(si.GetConditions()).To(ContainElement(And(
				HaveField("Type", string(conditions.TypeReady)),
				HaveField("Status", metav1.ConditionTrue),
			)))
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
	})

	It("reconcile successfully on a VF service interface with unknown MAC annotation", func() {
		ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(nil)
		ovsMock.EXPECT().SetIfaceExternalIDs(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(nil)
		ovsMock.EXPECT().DelPort(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(nil)

		networkHelperMock.EXPECT().GetVFRepresentorDPU(gomock.Any(), gomock.Any()).AnyTimes().Return("pf0vf2", nil)

		By("creating ServiceInterface")
		si := getTestServiceInterfaceTypeVF("pf0vf2", ns.Name, "test", node.Name, true)
		Expect(testClient.Create(testCtx, si)).To(Succeed())
		cleanupObjects = append(cleanupObjects, si)

		By("verifying ServiceInterface is reconciled successfully")
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(si), si)).To(Succeed())
			g.Expect(si.Annotations).To(HaveKey(common.LSPMACAddressAnnotationKey))
			g.Expect(si.Annotations[common.LSPMACAddressAnnotationKey]).To(Equal(UnknownMACAddress))
			g.Expect(si.ObjectMeta.Finalizers).To(ContainElement(ServiceInterfaceFinalizer))
			g.Expect(si.GetConditions()).To(ContainElement(And(
				HaveField("Type", string(dpuservicev1.ServiceInterfaceReconciled)),
				HaveField("Status", metav1.ConditionTrue),
			)))
			g.Expect(si.GetConditions()).To(ContainElement(And(
				HaveField("Type", string(conditions.TypeReady)),
				HaveField("Status", metav1.ConditionTrue),
			)))
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
	})

	It("no reconcile for vf interface without virtual network", func() {
		By("creating ServiceInterface")
		si := getTestServiceInterfaceWithOutVirtualNetwork("test", ns.Name, node.Name)
		Expect(testClient.Create(testCtx, si)).To(Succeed())
		cleanupObjects = append(cleanupObjects, si)

		By("verifying ServiceInterface not reconciled")
		Consistently(func(g Gomega) {
			g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(si), si)).To(Succeed())
			g.Expect(si.Annotations).NotTo(HaveKey(common.LSPMACAddressAnnotationKey))
			g.Expect(si.ObjectMeta.Finalizers).NotTo(ContainElement(ServiceInterfaceFinalizer))
			// ServiceInterface should not have any conditions with this controller, controlled by sfc-controller
			g.Expect(si.GetConditions()).To(BeEmpty())
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
	})

	It("no reconcile for unsupported interface type", func() {
		By("creating ServiceInterface")
		si := getTestServiceInterfaceTypePhysical("test", ns.Name, node.Name)
		Expect(testClient.Create(testCtx, si)).To(Succeed())
		cleanupObjects = append(cleanupObjects, si)

		By("verifying ServiceInterface not reconciled")
		Consistently(func(g Gomega) {
			g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(si), si)).To(Succeed())
			g.Expect(si.Annotations).NotTo(HaveKey(common.LSPMACAddressAnnotationKey))
			g.Expect(si.ObjectMeta.Finalizers).NotTo(ContainElement(ServiceInterfaceFinalizer))
			// ServiceInterface should not have any conditions with this controller, controlled by sfc-controller
			g.Expect(si.GetConditions()).To(BeEmpty())
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
	})

	It("reconcile successfully on a PF service interface", func() {
		ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(nil)
		ovsMock.EXPECT().SetIfaceExternalIDs(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(nil)
		ovsMock.EXPECT().DelPort(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(nil)

		networkHelperMock.EXPECT().GetPFRepresentorDPU(gomock.Any()).AnyTimes().Return("pf0", nil)

		By("creating ServiceInterface")
		si := getTestServiceInterfaceTypePF("pf0", ns.Name, "test", node.Name, false)
		Expect(testClient.Create(testCtx, si)).To(Succeed())
		cleanupObjects = append(cleanupObjects, si)

		By("verifying ServiceInterface is reconciled successfully")
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(si), si)).To(Succeed())
			g.Expect(si.Annotations).To(HaveKey(common.LSPMACAddressAnnotationKey))
			g.Expect(si.Annotations[common.LSPMACAddressAnnotationKey]).To(Equal("00:00:00:00:00:03"))
			g.Expect(si.ObjectMeta.Finalizers).To(ContainElement(ServiceInterfaceFinalizer))
			g.Expect(si.GetConditions()).To(ContainElement(And(
				HaveField("Type", string(dpuservicev1.ServiceInterfaceReconciled)),
				HaveField("Status", metav1.ConditionTrue),
			)))
			g.Expect(si.GetConditions()).To(ContainElement(And(
				HaveField("Type", string(conditions.TypeReady)),
				HaveField("Status", metav1.ConditionTrue),
			)))
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
	})

	It("reconcile successfully on a PF service interface with unknown MAC annotation", func() {
		ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(nil)
		ovsMock.EXPECT().SetIfaceExternalIDs(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(nil)
		ovsMock.EXPECT().DelPort(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(nil)

		networkHelperMock.EXPECT().GetPFRepresentorDPU(gomock.Any()).AnyTimes().Return("pf0", nil)

		By("creating ServiceInterface")
		si := getTestServiceInterfaceTypePF("pf0", ns.Name, "test", node.Name, true)
		Expect(testClient.Create(testCtx, si)).To(Succeed())
		cleanupObjects = append(cleanupObjects, si)

		By("verifying ServiceInterface is reconciled successfully")
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(si), si)).To(Succeed())
			g.Expect(si.Annotations).To(HaveKey(common.LSPMACAddressAnnotationKey))
			g.Expect(si.Annotations[common.LSPMACAddressAnnotationKey]).To(Equal(UnknownMACAddress))
			g.Expect(si.ObjectMeta.Finalizers).To(ContainElement(ServiceInterfaceFinalizer))
			g.Expect(si.GetConditions()).To(ContainElement(And(
				HaveField("Type", string(dpuservicev1.ServiceInterfaceReconciled)),
				HaveField("Status", metav1.ConditionTrue),
			)))
			g.Expect(si.GetConditions()).To(ContainElement(And(
				HaveField("Type", string(conditions.TypeReady)),
				HaveField("Status", metav1.ConditionTrue),
			)))
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
	})

	It("reconcile error on a VF service interface", func() {
		ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(nil)
		ovsMock.EXPECT().SetIfaceExternalIDs(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(nil)
		ovsMock.EXPECT().DelPort(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(nil)

		networkHelperMock.EXPECT().GetVFRepresentorDPU(gomock.Any(), gomock.Any()).AnyTimes().Return("", fmt.Errorf("mock error"))

		By("creating ServiceInterface")
		si := getTestServiceInterfaceTypeVF("pf0vf2", ns.Name, "test", node.Name, false)
		Expect(testClient.Create(testCtx, si)).To(Succeed())
		cleanupObjects = append(cleanupObjects, si)

		By("verifying ServiceInterface is in error state")
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(si), si)).To(Succeed())
			g.Expect(si.ObjectMeta.Finalizers).To(ContainElement(ServiceInterfaceFinalizer))
			g.Expect(si.GetConditions()).To(ContainElement(And(
				HaveField("Type", string(dpuservicev1.ServiceInterfaceReconciled)),
				HaveField("Status", metav1.ConditionFalse),
				HaveField("Reason", string(conditions.ReasonError)),
				HaveField("Message", ContainSubstring("Failed to add interface")),
			)))
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
	})

	Context("service interface type service", func() {
		var pod *corev1.Pod
		BeforeEach(func() {
			By("creating pod with labels")
			pod = getPodWithLabels(ns.Name, "test-pod", node.Name, map[string]string{dpuservicev1.DPFServiceIDLabelKey: "test"})
			Expect(testClient.Create(testCtx, pod)).To(Succeed())
			// Don't add pod to cleanupObjects as we handle it separately in AfterEach
		})
		AfterEach(func() {
			if pod != nil {
				By("deleting pod")
				Expect(testClient.Delete(testCtx, pod, client.GracePeriodSeconds(0))).To(Succeed())

				// Wait for pod to be deleted
				Eventually(func() bool {
					err := testClient.Get(testCtx, client.ObjectKeyFromObject(pod), pod)
					return apierrors.IsNotFound(err)
				}).WithTimeout(10 * time.Second).WithPolling(500 * time.Millisecond).Should(BeTrue())
			}
		})
		It("reconcile successfully with service interface type service with unknown MAC annotation", func() {
			ovsMock.EXPECT().SetIfaceExternalIDs(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(nil)
			ovsMock.EXPECT().GetIfaceWithExternalIDs(gomock.Any(), gomock.Any()).AnyTimes().Return(&ovsmodel.Interface{Name: "test"}, nil)

			By("creating ServiceInterface")
			si := getTestServiceInterfaceTypeService("test", ns.Name, node.Name, true)
			Expect(testClient.Create(testCtx, si)).To(Succeed())
			cleanupObjects = append(cleanupObjects, si)

			By("verifying ServiceInterface is reconciled successfully")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(si), si)).To(Succeed())
				g.Expect(si.Annotations).To(HaveKey(common.LSPMACAddressAnnotationKey))
				g.Expect(si.Annotations[common.LSPMACAddressAnnotationKey]).To(Equal(UnknownMACAddress))
				g.Expect(si.ObjectMeta.Finalizers).To(ContainElement(ServiceInterfaceFinalizer))
				siConditions := si.GetConditions()
				g.Expect(siConditions).To(ContainElement(And(
					HaveField("Type", string(dpuservicev1.ServiceInterfaceReconciled)),
					HaveField("Status", metav1.ConditionTrue),
				)))
				g.Expect(siConditions).To(ContainElement(And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionTrue),
				)))
			}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
		})

		It("reconcile successfully with service interface type service with known MAC annotation", func() {
			macAddr := "00:00:00:00:00:01"
			ovsMock.EXPECT().SetIfaceExternalIDs(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(nil)
			ovsMock.EXPECT().GetIfaceWithExternalIDs(gomock.Any(), gomock.Any()).AnyTimes().Return(&ovsmodel.Interface{
				Name: "test",
				ExternalIDs: map[string]string{
					nodeutils.IfaceMacKey: macAddr,
				},
			}, nil)

			By("creating ServiceInterface")
			si := getTestServiceInterfaceTypeService("test", ns.Name, node.Name, false)
			Expect(testClient.Create(testCtx, si)).To(Succeed())
			cleanupObjects = append(cleanupObjects, si)

			By("verifying ServiceInterface is reconciled successfully")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(si), si)).To(Succeed())
				g.Expect(si.Annotations).To(HaveKey(common.LSPMACAddressAnnotationKey))
				g.Expect(si.Annotations[common.LSPMACAddressAnnotationKey]).To(Equal(macAddr))
				g.Expect(si.ObjectMeta.Finalizers).To(ContainElement(ServiceInterfaceFinalizer))
				siConditions := si.GetConditions()
				g.Expect(siConditions).To(ContainElement(And(
					HaveField("Type", string(dpuservicev1.ServiceInterfaceReconciled)),
					HaveField("Status", metav1.ConditionTrue),
				)))
				g.Expect(siConditions).To(ContainElement(And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionTrue),
				)))
			}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
		})
	})

	Context("service interface type service with creation order before pod", func() {
		var pod *corev1.Pod
		AfterEach(func() {
			if pod != nil {
				By("deleting pod")
				Expect(testClient.Delete(testCtx, pod, client.GracePeriodSeconds(0))).To(Succeed())

				// Wait for pod to be deleted
				Eventually(func() bool {
					err := testClient.Get(testCtx, client.ObjectKeyFromObject(pod), pod)
					return apierrors.IsNotFound(err)
				}).WithTimeout(10 * time.Second).WithPolling(500 * time.Millisecond).Should(BeTrue())
			}
		})

		It("reconcile fail till the pod is created", func() {
			macAddr := "00:00:00:00:00:01"
			ovsMock.EXPECT().SetIfaceExternalIDs(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(nil)
			ovsMock.EXPECT().GetIfaceWithExternalIDs(gomock.Any(), gomock.Any()).AnyTimes().Return(&ovsmodel.Interface{
				Name: "test",
				ExternalIDs: map[string]string{
					nodeutils.IfaceMacKey: macAddr,
				},
			}, nil)

			By("creating ServiceInterface")
			si := getTestServiceInterfaceTypeService("test", ns.Name, node.Name, false)
			Expect(testClient.Create(testCtx, si)).To(Succeed())
			cleanupObjects = append(cleanupObjects, si)

			By("verifying ServiceInterface failed to reconcile")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(si), si)).To(Succeed())
				g.Expect(si.Annotations).NotTo(HaveKey(common.LSPMACAddressAnnotationKey))
				g.Expect(si.ObjectMeta.Finalizers).To(ContainElement(ServiceInterfaceFinalizer))
				siConditions := si.GetConditions()
				errMsg := fmt.Sprintf("failed to get pod with labels: no pod in namespace(%s) matching labels(map[svc.dpu.nvidia.com/service:test]) on node(%s) found", ns.Name, node.Name)
				g.Expect(siConditions).To(ContainElement(And(
					HaveField("Type", string(dpuservicev1.ServiceInterfaceReconciled)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonError)),
					HaveField("Message", ContainSubstring(errMsg)),
				)))
				g.Expect(siConditions).To(ContainElement(And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				)))
			}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

			By("creating pod with labels")
			pod = getPodWithLabels(ns.Name, "test-pod", node.Name, map[string]string{dpuservicev1.DPFServiceIDLabelKey: "test"})
			Expect(testClient.Create(testCtx, pod)).To(Succeed())

			By("verifying ServiceInterface is reconciled successfully")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(si), si)).To(Succeed())
				g.Expect(si.Annotations).To(HaveKey(common.LSPMACAddressAnnotationKey))
				g.Expect(si.Annotations[common.LSPMACAddressAnnotationKey]).To(Equal(macAddr))
				g.Expect(si.ObjectMeta.Finalizers).To(ContainElement(ServiceInterfaceFinalizer))
				siConditions := si.GetConditions()
				g.Expect(siConditions).To(ContainElement(And(
					HaveField("Type", string(dpuservicev1.ServiceInterfaceReconciled)),
					HaveField("Status", metav1.ConditionTrue),
				)))
				g.Expect(siConditions).To(ContainElement(And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionTrue),
				)))
			}).WithPolling(500 * time.Millisecond).WithTimeout(10 * time.Second).Should(Succeed())
		})

		It("reconcile only the ServiceInterface that belongs to Pod's node", func() {
			macAddr := "00:00:00:00:00:01"
			ovsMock.EXPECT().SetIfaceExternalIDs(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(nil)
			ovsMock.EXPECT().GetIfaceWithExternalIDs(gomock.Any(), gomock.Any()).AnyTimes().Return(&ovsmodel.Interface{
				Name: "test",
				ExternalIDs: map[string]string{
					nodeutils.IfaceMacKey: macAddr,
				},
			}, nil)

			By("creating second node")
			node2 := getTestNode(testNode + "-2")
			Expect(testClient.Create(testCtx, node2)).To(Succeed())
			cleanupObjects = append(cleanupObjects, node2)

			By("creating ServiceInterface on first node")
			si := getTestServiceInterfaceTypeService("test", ns.Name, node.Name, false)
			Expect(testClient.Create(testCtx, si)).To(Succeed())
			cleanupObjects = append(cleanupObjects, si)

			By("creating ServiceInterface on second node")
			si2 := getTestServiceInterfaceTypeService("test2", ns.Name, node2.Name, false)
			Expect(testClient.Create(testCtx, si2)).To(Succeed())
			cleanupObjects = append(cleanupObjects, si2)

			By("creating pod on first node")
			pod = getPodWithLabels(ns.Name, "test-pod", node.Name, map[string]string{dpuservicev1.DPFServiceIDLabelKey: "test"})
			Expect(testClient.Create(testCtx, pod)).To(Succeed())

			By("verifying ServiceInterface on first node is reconciled successfully")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(si), si)).To(Succeed())
				g.Expect(si.Annotations).To(HaveKey(common.LSPMACAddressAnnotationKey))
				g.Expect(si.Annotations[common.LSPMACAddressAnnotationKey]).To(Equal(macAddr))
				g.Expect(si.ObjectMeta.Finalizers).To(ContainElement(ServiceInterfaceFinalizer))
				siConditions := si.GetConditions()
				g.Expect(siConditions).To(ContainElement(And(
					HaveField("Type", string(dpuservicev1.ServiceInterfaceReconciled)),
					HaveField("Status", metav1.ConditionTrue),
				)))
				g.Expect(siConditions).To(ContainElement(And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionTrue),
				)))
			}).WithPolling(500 * time.Millisecond).WithTimeout(10 * time.Second).Should(Succeed())

			By("verifying ServiceInterface on second node is not reconciled by this controller")
			Consistently(func(g Gomega) {
				g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(si2), si2)).To(Succeed())
				g.Expect(si2.Annotations).NotTo(HaveKey(common.LSPMACAddressAnnotationKey))
				g.Expect(si2.ObjectMeta.Finalizers).NotTo(ContainElement(ServiceInterfaceFinalizer))
				// This controller hasn't touched it, so no controller-specific conditions
				g.Expect(si2.GetConditions()).To(BeEmpty())
			}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
		})
	})
})
