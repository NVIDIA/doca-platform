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
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/ipmanager"

	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"
	testutils "github.com/nvidia/doca-platform/test/utils"
	"github.com/nvidia/doca-platform/test/utils/informer"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlcfg "sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var _ = Describe("DPUVirtualNetworkController", func() {
	var (
		cleanupObjs    []client.Object
		testCtx        context.Context
		testCancelFunc context.CancelFunc
		wg             sync.WaitGroup
		ipm            ipmanager.IPManager
	)

	BeforeEach(func() {
		cleanupObjs = make([]client.Object, 0)
		testCtx, testCancelFunc = context.WithCancel(suiteCtx)
		wg = sync.WaitGroup{}

		By("setting up and running the test manager")
		testManager, err := ctrl.NewManager(cfg,
			ctrl.Options{
				Scheme: testScheme,
				Client: client.Options{
					Cache: &client.CacheOptions{
						DisableFor: []client.Object{&corev1.Secret{}, &corev1.ConfigMap{}},
					},
				},
				// Set metrics server bind address to 0 to disable it.
				Metrics: server.Options{
					BindAddress: "0",
				},
				Controller: ctrlcfg.Controller{
					// this is needed since metrics are registered globally by controller runtime for each controller
					// and we want to allow multiple tests initializing the same controller name.
					SkipNameValidation: ptr.To(true),
				},
			},
		)
		Expect(err).ToNot(HaveOccurred())

		ipm = ipmanager.NewIPManager()
		Expect(ipm.Initialize(nil, nil, nil)).To(Succeed())

		err = (&DPUVirtualNetworkReconciler{
			Client:    testManager.GetClient(),
			Scheme:    testManager.GetScheme(),
			IPManager: ipm,
		}).SetupWithManager(testCtx, testManager)
		Expect(err).ToNot(HaveOccurred())

		wg.Add(1)
		go func() {
			defer wg.Done()
			defer GinkgoRecover()
			err = testManager.Start(testCtx)
			Expect(err).ToNot(HaveOccurred(), "failed to run manager")
		}()
	})

	AfterEach(func() {
		Expect(testutils.CleanupAndWait(suiteCtx, testClient, cleanupObjs...)).To(Succeed())
		testCancelFunc()
		wg.Wait()
	})

	It("should reconcile DPUVirtualNetwork successfully", func() {
		By("creating DPUVPC")
		dpuVPC := getTestVPC("my-vpc", "isolation-class", nil)
		Expect(testClient.Create(testCtx, dpuVPC)).To(Succeed())
		cleanupObjs = append(cleanupObjs, dpuVPC)

		ipm.AddVPC(ipmanager.ObjToID(dpuVPC))
		Expect(ipm.AddNetwork(ipmanager.ObjToID(dpuVPC), ipmanager.VPCClusterNetworkIPV4, ipmanager.VPCClusterCIDRIPV4)).To(Succeed())

		By("creating DPUVirtualNetwork")
		dpuVN := getTestDPUVirtualNetwork("test-vn", dpuVPC.Name, "10.0.0.0/16", nil)
		Expect(testClient.Create(testCtx, dpuVN)).To(Succeed())

		By("checking finalizer is added")
		Eventually(func() bool {
			err := testClient.Get(testCtx, client.ObjectKey{Namespace: dpuVN.Namespace, Name: dpuVN.Name}, dpuVN)
			if err != nil {
				return false
			}
			return controllerutil.ContainsFinalizer(dpuVN, dpuVirtualNetworkFinalizer)
		}).WithTimeout(5 * time.Second).WithPolling(500 * time.Millisecond).Should(BeTrue())

		By("checking DPUVirtualNetwork status ready")
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(testCtx, client.ObjectKey{Namespace: dpuVN.Namespace, Name: dpuVN.Name}, dpuVN)).To(Succeed())
			g.Expect(dpuVN.Status.Conditions).ToNot(BeEmpty())
			g.Expect(dpuVN.Status.Conditions).To(ContainElement(
				And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionTrue),
				),
			))
		}).WithTimeout(5 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())

		By("checking DPUVirtualNetwork annotations")
		_, err := common.LRPAddressFromAnnotation(dpuVN.Annotations)
		Expect(err).ToNot(HaveOccurred())

		By("deleting DPUVirtualNetwork")
		Expect(testClient.Delete(testCtx, dpuVN)).To(Succeed())
		Eventually(func() error {
			err := testClient.Get(testCtx, client.ObjectKey{Namespace: dpuVN.Namespace, Name: dpuVN.Name}, dpuVN)
			if err != nil && apierrors.IsNotFound(err) {
				return nil
			}
			return fmt.Errorf("DPUVirtualNetwork still exists.")
		}).WithTimeout(5 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())
	})

	It("should block deletion of DPUVirtualNetwork if DPUServiceInterfaces reference it", func() {
		By("creating DPUVPC")
		dpuVPC := getTestVPC("my-vpc", "isolation-class", nil)
		Expect(testClient.Create(testCtx, dpuVPC)).To(Succeed())
		cleanupObjs = append(cleanupObjs, dpuVPC)

		ipm.AddVPC(ipmanager.ObjToID(dpuVPC))
		Expect(ipm.AddNetwork(ipmanager.ObjToID(dpuVPC), ipmanager.VPCClusterNetworkIPV4, ipmanager.VPCClusterCIDRIPV4)).To(Succeed())

		By("creating DPUServiceInterface")
		dpuSI := getTestDPUServiceInterface("test-si", "test-vn")
		Expect(testClient.Create(testCtx, dpuSI)).To(Succeed())

		By("creating DPUVirtualNetwork")
		dpuVN := getTestDPUVirtualNetwork("test-vn", dpuVPC.Name, "10.0.0.0/16", nil)
		Expect(testClient.Create(testCtx, dpuVN)).To(Succeed())

		By("checking DPUVirtualNetwork status ready")
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(testCtx, client.ObjectKey{Namespace: dpuVN.Namespace, Name: dpuVN.Name}, dpuVN)).To(Succeed())
			g.Expect(dpuVN.Status.Conditions).To(ContainElement(
				And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionTrue),
				),
			))
		}).WithTimeout(5 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())

		By("deleting DPUVirtualNetwork")
		Expect(testClient.Delete(testCtx, dpuVN)).To(Succeed())

		By("ensuring DPUVirtualNetwork is not deleted")
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(testCtx, client.ObjectKey{Namespace: dpuVN.Namespace, Name: dpuVN.Name}, dpuVN)).To(Succeed())
			g.Expect(dpuVN.Status.Conditions).To(ContainElement(
				And(
					HaveField("Type", string(vpcv1.ConditionDPUVirtualNetworkReconciled)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonAwaitingDeletion)),
				),
			))
		}).WithTimeout(5 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())

		By("deleting DPUServiceInterface")
		Expect(testClient.Delete(testCtx, dpuSI)).To(Succeed())

		By("ensuring DPUVirtualNetwork is deleted")
		Eventually(func() error {
			err := testClient.Get(testCtx, client.ObjectKey{Namespace: dpuVN.Namespace, Name: dpuVN.Name}, dpuVN)
			if err != nil && apierrors.IsNotFound(err) {
				return nil
			}
			return fmt.Errorf("virtual network still exists. %w", err)
		}).WithTimeout(5 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())
	})

	It("Becomes ready after prereqs are satisfied", func() {
		dpuVPC := getTestVPC("my-vpc", "isolation-class", nil)

		By("creating DPUVirtualNetwork")
		dpuVN := getTestDPUVirtualNetwork("test-vn", dpuVPC.Name, "10.0.0.0/16", nil)
		Expect(testClient.Create(testCtx, dpuVN)).To(Succeed())

		By("checking DPUVirtualNetwork status not ready")
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(testCtx, client.ObjectKey{Namespace: dpuVN.Namespace, Name: dpuVN.Name}, dpuVN)).To(Succeed())
			g.Expect(dpuVN.Status.Conditions).ToNot(BeEmpty())
			g.Expect(dpuVN.Status.Conditions).To(ConsistOf(
				And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionFalse),
				),
				And(
					HaveField("Type", string(vpcv1.ConditionPreReqsReady)),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
				And(
					HaveField("Type", string(vpcv1.ConditionDPUVirtualNetworkReconciled)),
					HaveField("Status", metav1.ConditionUnknown),
					HaveField("Reason", string(conditions.ReasonPending)),
				),
			))
		}).WithTimeout(5 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())

		By("creating DPUVPC")
		Expect(testClient.Create(testCtx, dpuVPC)).To(Succeed())
		cleanupObjs = append(cleanupObjs, dpuVPC)

		ipm.AddVPC(ipmanager.ObjToID(dpuVPC))
		Expect(ipm.AddNetwork(ipmanager.ObjToID(dpuVPC), ipmanager.VPCClusterNetworkIPV4, ipmanager.VPCClusterCIDRIPV4)).To(Succeed())

		By("checking DPUVirtualNetwork status ready")
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(testCtx, client.ObjectKey{Namespace: dpuVN.Namespace, Name: dpuVN.Name}, dpuVN)).To(Succeed())
			g.Expect(dpuVN.Status.Conditions).ToNot(BeEmpty())
			g.Expect(dpuVN.Status.Conditions).To(ContainElement(
				And(
					HaveField("Type", string(conditions.TypeReady)),
					HaveField("Status", metav1.ConditionTrue),
				),
			))
		}).WithTimeout(5 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())

		By("deleting DPUVirtualNetwork")
		Expect(testClient.Delete(testCtx, dpuVN)).To(Succeed())
		Eventually(func() error {
			err := testClient.Get(testCtx, client.ObjectKey{Namespace: dpuVN.Namespace, Name: dpuVN.Name}, dpuVN)
			if err != nil {
				if apierrors.IsNotFound(err) {
					return nil
				}
				return err
			}
			return fmt.Errorf("DPUVirtualNetwork still exists")
		}).WithTimeout(5 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())
	})

	Context("State transitions", func() {
		var i *informer.TestInformer

		BeforeEach(func() {
			i = informer.NewInformer(cfg, vpcv1.DPUVPCGroupVersionKind, "default", "dpuvirtualnetworks")
			DeferCleanup(i.Cleanup)
			go i.Run()

			Eventually(i.HasSynced).WithTimeout(5 * time.Second).WithPolling(500 * time.Millisecond).Should(BeTrue())
		})

		It("transitions as expected", func() {
			dpuVPC := getTestVPC("my-vpc", "isolation-class", nil)
			dpuSI := getTestDPUServiceInterface("test-si", "test-vn")

			By("creating DPUVirtualNetwork")
			dpuVN := getTestDPUVirtualNetwork("test-vn", dpuVPC.Name, "10.0.0.0/16", nil)
			Expect(testClient.Create(testCtx, dpuVN)).To(Succeed())

			By("ensure initial conditions")
			Eventually(func(g Gomega) {
				var ev informer.Event
				g.Eventually(i.UpdateEvents).Should(Receive(&ev))
				oldObj := &vpcv1.DPUVirtualNetwork{}
				newObj := &vpcv1.DPUVirtualNetwork{}
				g.Expect(testClient.Scheme().Convert(ev.OldObj, oldObj, nil)).ToNot(HaveOccurred())
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())
				g.Expect(oldObj.Status.Conditions).To(BeEmpty())
				g.Expect(newObj.Status.Conditions).ToNot(BeEmpty())

				g.Expect(newObj.Status.Conditions).To(ConsistOf(
					And(
						HaveField("Type", string(conditions.TypeReady)),
						HaveField("Status", metav1.ConditionFalse),
					),
					And(
						HaveField("Type", string(vpcv1.ConditionPreReqsReady)),
						HaveField("Status", metav1.ConditionFalse),
						HaveField("Reason", string(conditions.ReasonPending)),
					),
					And(
						HaveField("Type", string(vpcv1.ConditionDPUVirtualNetworkReconciled)),
						HaveField("Status", metav1.ConditionUnknown),
					),
				))
			}).WithTimeout(5 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())

			By("Creating DPUVPC")
			Expect(testClient.Create(testCtx, dpuVPC)).To(Succeed())
			cleanupObjs = append(cleanupObjs, dpuVPC)

			By("checking DPUVirtualNetwork conditions")
			Eventually(func(g Gomega) {
				var ev informer.Event
				g.Eventually(i.UpdateEvents).Should(Receive(&ev))
				newObj := &vpcv1.DPUVirtualNetwork{}
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())
				g.Expect(newObj.Status.Conditions).To(ConsistOf(
					And(
						HaveField("Type", string(conditions.TypeReady)),
						HaveField("Status", metav1.ConditionFalse),
					),
					And(
						HaveField("Type", string(vpcv1.ConditionPreReqsReady)),
						HaveField("Status", metav1.ConditionTrue),
					),
					And(
						HaveField("Type", string(vpcv1.ConditionDPUVirtualNetworkReconciled)),
						HaveField("Status", metav1.ConditionFalse),
						HaveField("Reason", string(conditions.ReasonError)),
					),
				))
			}).WithTimeout(5 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())

			By("adding VPC to IPManager")
			ipm.AddVPC(ipmanager.ObjToID(dpuVPC))
			Expect(ipm.AddNetwork(ipmanager.ObjToID(dpuVPC), ipmanager.VPCClusterNetworkIPV4, ipmanager.VPCClusterCIDRIPV4)).To(Succeed())

			By("checking DPUVirtualNetwork conditions")
			Eventually(func(g Gomega) {
				var ev informer.Event
				g.Eventually(i.UpdateEvents).Should(Receive(&ev))
				newObj := &vpcv1.DPUVirtualNetwork{}
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())
				g.Expect(newObj.Status.Conditions).To(ConsistOf(
					And(
						HaveField("Type", string(conditions.TypeReady)),
						HaveField("Status", metav1.ConditionTrue),
					),
					And(
						HaveField("Type", string(vpcv1.ConditionPreReqsReady)),
						HaveField("Status", metav1.ConditionTrue),
					),
					And(
						HaveField("Type", string(vpcv1.ConditionDPUVirtualNetworkReconciled)),
						HaveField("Status", metav1.ConditionTrue),
					),
				))
			}).WithTimeout(5 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())

			By("Creating DPUServiceInterface")
			Expect(testClient.Create(testCtx, dpuSI)).To(Succeed())

			By("Deleting DPUVirtualNetwork")
			Expect(testClient.Delete(testCtx, dpuVN)).To(Succeed())

			By("checking DPUVirtualNetwork conditions")
			Eventually(func(g Gomega) {
				var ev informer.Event
				g.Eventually(i.UpdateEvents).Should(Receive(&ev))
				newObj := &vpcv1.DPUVirtualNetwork{}
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())
				g.Expect(newObj.Status.Conditions).To(ConsistOf(
					And(
						HaveField("Type", string(conditions.TypeReady)),
						HaveField("Status", metav1.ConditionFalse),
					),
					And(
						HaveField("Type", string(vpcv1.ConditionPreReqsReady)),
						HaveField("Status", metav1.ConditionTrue),
					),
					And(
						HaveField("Type", string(vpcv1.ConditionDPUVirtualNetworkReconciled)),
						HaveField("Status", metav1.ConditionFalse),
						HaveField("Reason", string(conditions.ReasonAwaitingDeletion)),
					),
				))
			}).WithTimeout(5 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())

			By("Deleting DPUServiceInterface")
			Expect(testClient.Delete(testCtx, dpuSI)).To(Succeed())

			By("ensuring DPUVirtualNetwork is deleted")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(testCtx, client.ObjectKey{Namespace: dpuVN.Namespace, Name: dpuVN.Name}, dpuVN)).ToNot(Succeed())
			}).WithTimeout(5 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())
		})
	})
})
