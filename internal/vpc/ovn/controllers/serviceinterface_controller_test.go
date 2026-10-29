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
	"sync"
	"time"

	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/common"
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/topology"
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/topology/mock"

	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	testutils "github.com/nvidia/doca-platform/test/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlcfg "sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var _ = Describe("ServiceInterfaceController", func() {
	var (
		cleanupObjs         []client.Object
		testCtx             context.Context
		testCancelFunc      context.CancelFunc
		wg                  sync.WaitGroup
		topologyManagerMock *mock.MockManager
	)

	// set shorter requeue for serviceinterface reconciler time to speed up testing
	relatedObjectsPendingRequeueTime = 1 * time.Second

	BeforeEach(func() {
		cleanupObjs = make([]client.Object, 0)
		testCtx, testCancelFunc = context.WithCancel(suiteCtx)
		wg = sync.WaitGroup{}
		By("overriding topologyManagerFromIsolationClassFn to return a mock")
		mockCtrl := gomock.NewController(GinkgoT())
		topologyManagerMock = mock.NewMockManager(mockCtrl)
		topologyManagerFromIsolationClassFn := func(ctx context.Context, isoCls *vpcv1.IsolationClass) (topology.Manager, error) {
			return topologyManagerMock, nil
		}

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

		remoteCache, err := dpucluster.SetupRemoteCacheWithManager(testCtx, testManager,
			dpucluster.OptionHostClient{Client: testManager.GetClient()},
			dpucluster.OptionScheme{Scheme: testManager.GetScheme()},
			dpucluster.OptionUserAgent{UserAgent: "ovn-vpc-controller"},
			dpucluster.OptionDisableFor{DisableFor: []client.Object{
				&corev1.ConfigMap{},
				&corev1.Secret{},
			}})
		Expect(err).ToNot(HaveOccurred())

		sir := &ServiceInterfaceReconciler{
			Client:                              testManager.GetClient(),
			Scheme:                              testManager.GetScheme(),
			RemoteCache:                         remoteCache,
			CleanupGate:                         NewCleanupGate(),
			topologyManagerFromIsolationClassFn: topologyManagerFromIsolationClassFn,
		}
		Expect(sir.SetupWithManager(testCtx, testManager)).To(Succeed())

		err = (&DPUClusterReconciler{
			Client:           testManager.GetClient(),
			Scheme:           testManager.GetScheme(),
			RemoteCache:      remoteCache,
			WatchRegisterers: []WatchRegisterer{sir},
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

	It("should reconcile successfully", func() {
		topologyManagerMock.EXPECT().PlugServiceInterface(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).MinTimes(1)
		topologyManagerMock.EXPECT().UnplugServiceInterface(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).MinTimes(1)

		By("creating pre-requisites IsolationClass, VPC and VirtualNetwork")
		isoCls := getTestIsolationClass("test")
		vpc := getTestVPC("test", "test", map[string]string{})
		vn := getTestDPUVirtualNetwork("test", "test", "10.0.0.0/16", nil)
		node := getTestNode("node-0", nil)
		si := getTestServiceInterface("test", "test", node.Name)

		Expect(testClient.Create(testCtx, isoCls)).To(Succeed())
		cleanupObjs = append(cleanupObjs, isoCls)

		Expect(testClient.Create(testCtx, vpc)).To(Succeed())
		cleanupObjs = append(cleanupObjs, vpc)

		Expect(testClient.Create(testCtx, vn)).To(Succeed())
		cleanupObjs = append(cleanupObjs, vn)

		// mark virtualNetwork as ready
		conditions.AddTrue(vn, conditions.TypeReady)
		Expect(testClient.Status().Update(testCtx, vn)).To(Succeed())

		Expect(testClient.Create(testCtx, node)).To(Succeed())
		cleanupObjs = append(cleanupObjs, node)

		By("creating ServiceInterface")
		Expect(dpuClusterTestClient.Create(testCtx, si)).To(Succeed())

		By("verifying ServiceInterface is reconciled successfully")
		Eventually(func(g Gomega) {
			g.Expect(dpuClusterTestClient.Get(testCtx, client.ObjectKeyFromObject(si), si)).To(Succeed())
			g.Expect(si.Annotations).To(HaveKey(common.LSPConnectedAnnotationKey))
			g.Expect(si.Annotations[common.LSPConnectedAnnotationKey]).To(Equal("true"))
			g.Expect(si.ObjectMeta.Finalizers).To(ContainElement(serviceInterfaceFinalizer))
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

		By("deleting ServiceInterface")
		Expect(dpuClusterTestClient.Delete(testCtx, si)).To(Succeed())

		By("ensure service interface is deleted")
		Eventually(func(g Gomega) {
			g.Expect(dpuClusterTestClient.Get(testCtx, client.ObjectKeyFromObject(si), si)).ToNot(Succeed())
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
	})

	It("should unplug ServiceInterface when node does not belong to VPC", func() {
		topologyManagerMock.EXPECT().PlugServiceInterface(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).MinTimes(1)
		topologyManagerMock.EXPECT().UnplugServiceInterface(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).MinTimes(1)

		By("creating pre-requisites IsolationClass, VPC and VirtualNetwork")
		nodeLabels := map[string]string{"foo": "bar"}
		isoCls := getTestIsolationClass("test")
		vpc := getTestVPC("test", "test", nodeLabels)
		vn := getTestDPUVirtualNetwork("test", "test", "10.0.0.0/16", nil)
		node := getTestNode("node-0", nodeLabels)
		si := getTestServiceInterface("test", "test", node.Name)

		Expect(testClient.Create(testCtx, isoCls)).To(Succeed())
		cleanupObjs = append(cleanupObjs, isoCls)

		Expect(testClient.Create(testCtx, vpc)).To(Succeed())
		cleanupObjs = append(cleanupObjs, vpc)

		Expect(testClient.Create(testCtx, vn)).To(Succeed())
		cleanupObjs = append(cleanupObjs, vn)

		// mark virtualNetwork as ready
		conditions.AddTrue(vn, conditions.TypeReady)
		Expect(testClient.Status().Update(testCtx, vn)).To(Succeed())

		Expect(testClient.Create(testCtx, node)).To(Succeed())
		cleanupObjs = append(cleanupObjs, node)

		By("creating ServiceInterface")
		Expect(dpuClusterTestClient.Create(testCtx, si)).To(Succeed())

		By("verifying ServiceInterface is reconciled successfully")
		Eventually(func(g Gomega) {
			g.Expect(dpuClusterTestClient.Get(testCtx, client.ObjectKeyFromObject(si), si)).To(Succeed())
			g.Expect(si.Annotations).To(HaveKey(common.LSPConnectedAnnotationKey))
			g.Expect(si.Annotations[common.LSPConnectedAnnotationKey]).To(Equal("true"))
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

		By("updating node labels to not belong to VPC")
		node.Labels["foo"] = "baz"
		Expect(testClient.Update(testCtx, node)).To(Succeed())

		By("verifying ServiceInterface is unplugged")
		Eventually(func(g Gomega) {
			g.Expect(dpuClusterTestClient.Get(testCtx, client.ObjectKeyFromObject(si), si)).To(Succeed())
			g.Expect(si.Annotations).To(HaveKey(common.LSPConnectedAnnotationKey))
			g.Expect(si.Annotations[common.LSPConnectedAnnotationKey]).To(Equal("false"))
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

		By("deleting ServiceInterface")
		Expect(dpuClusterTestClient.Delete(testCtx, si)).To(Succeed())

		By("ensure service interface is deleted")
		Eventually(func(g Gomega) {
			g.Expect(dpuClusterTestClient.Get(testCtx, client.ObjectKeyFromObject(si), si)).ToNot(Succeed())
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
	})

	It("should plug service interface after all pre-requisites are created", func() {
		node := getTestNode("node-0", nil)
		isoCls := getTestIsolationClass("test")
		vpc := getTestVPC("test", "test", map[string]string{})
		vn := getTestDPUVirtualNetwork("test", "test", "10.0.0.0/16", nil)
		si := getTestServiceInterface("test", "test", node.Name)

		By("create node")
		Expect(testClient.Create(testCtx, node)).To(Succeed())
		cleanupObjs = append(cleanupObjs, node)

		By("create serviceInterface")
		Expect(dpuClusterTestClient.Create(testCtx, si)).To(Succeed())

		By("ensure service interface is not plugged")
		Consistently(func(g Gomega) {
			g.Expect(dpuClusterTestClient.Get(testCtx, client.ObjectKeyFromObject(si), si)).To(Succeed())
			g.Expect(si.Annotations).ToNot(HaveKey(common.LSPConnectedAnnotationKey))
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

		By("create virtual network")
		Expect(testClient.Create(testCtx, vn)).To(Succeed())
		cleanupObjs = append(cleanupObjs, vn)

		By("ensure service interface is not plugged")
		Consistently(func(g Gomega) {
			g.Expect(dpuClusterTestClient.Get(testCtx, client.ObjectKeyFromObject(si), si)).To(Succeed())
			g.Expect(si.Annotations).ToNot(HaveKey(common.LSPConnectedAnnotationKey))
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

		By("create vpc")
		Expect(testClient.Create(testCtx, vpc)).To(Succeed())
		cleanupObjs = append(cleanupObjs, vpc)

		By("ensure service interface is not plugged")
		Consistently(func(g Gomega) {
			g.Expect(dpuClusterTestClient.Get(testCtx, client.ObjectKeyFromObject(si), si)).To(Succeed())
			g.Expect(si.Annotations).ToNot(HaveKey(common.LSPConnectedAnnotationKey))
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

		By("create isolation class")
		Expect(testClient.Create(testCtx, isoCls)).To(Succeed())
		cleanupObjs = append(cleanupObjs, isoCls)

		By("ensure service interface is not plugged")
		Consistently(func(g Gomega) {
			g.Expect(dpuClusterTestClient.Get(testCtx, client.ObjectKeyFromObject(si), si)).To(Succeed())
			g.Expect(si.Annotations).ToNot(HaveKey(common.LSPConnectedAnnotationKey))
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

		topologyManagerMock.EXPECT().PlugServiceInterface(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).MinTimes(1)
		topologyManagerMock.EXPECT().UnplugServiceInterface(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).MinTimes(1)

		By("set virtual network as ready")
		conditions.AddTrue(vn, conditions.TypeReady)
		Expect(testClient.Status().Update(testCtx, vn)).To(Succeed())

		By("ensure service interface is plugged")
		Eventually(func(g Gomega) {
			g.Expect(dpuClusterTestClient.Get(testCtx, client.ObjectKeyFromObject(si), si)).To(Succeed())
			g.Expect(si.Annotations).To(HaveKey(common.LSPConnectedAnnotationKey))
			g.Expect(si.Annotations[common.LSPConnectedAnnotationKey]).To(Equal("true"))
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

		By("set virtual network as not ready")
		conditions.AddFalse(vn, conditions.TypeReady, "Degraded", "some error")
		Expect(testClient.Status().Update(testCtx, vn)).To(Succeed())

		By("deleting ServiceInterface")
		Expect(dpuClusterTestClient.Delete(testCtx, si)).To(Succeed())

		By("ensure service interface is deleted")
		Eventually(func(g Gomega) {
			g.Expect(dpuClusterTestClient.Get(testCtx, client.ObjectKeyFromObject(si), si)).ToNot(Succeed())
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
	})

	It("should plug service interface after all pre-requisites are created - mac address unknown", func() {
		node := getTestNode("node-0", nil)
		isoCls := getTestIsolationClass("test")
		vpc := getTestVPC("test", "test", map[string]string{})
		vn := getTestDPUVirtualNetwork("test", "test", "10.0.0.0/16", nil)
		si := getTestServiceInterface("test", "test", node.Name)
		si.Annotations[common.LSPMACAddressAnnotationKey] = "unknown"

		By("create node")
		Expect(testClient.Create(testCtx, node)).To(Succeed())
		cleanupObjs = append(cleanupObjs, node)

		By("create serviceInterface")
		Expect(dpuClusterTestClient.Create(testCtx, si)).To(Succeed())

		By("ensure service interface is not plugged")
		Consistently(func(g Gomega) {
			g.Expect(dpuClusterTestClient.Get(testCtx, client.ObjectKeyFromObject(si), si)).To(Succeed())
			g.Expect(si.Annotations).ToNot(HaveKey(common.LSPConnectedAnnotationKey))
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

		By("create virtual network")
		Expect(testClient.Create(testCtx, vn)).To(Succeed())
		cleanupObjs = append(cleanupObjs, vn)

		By("create vpc")
		Expect(testClient.Create(testCtx, vpc)).To(Succeed())
		cleanupObjs = append(cleanupObjs, vpc)

		By("create isolation class")
		Expect(testClient.Create(testCtx, isoCls)).To(Succeed())
		cleanupObjs = append(cleanupObjs, isoCls)

		topologyManagerMock.EXPECT().PlugServiceInterface(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).MinTimes(1)
		topologyManagerMock.EXPECT().UnplugServiceInterface(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).MinTimes(1)

		By("set virtual network as ready")
		conditions.AddTrue(vn, conditions.TypeReady)
		Expect(testClient.Status().Update(testCtx, vn)).To(Succeed())

		By("ensure service interface is plugged")
		Eventually(func(g Gomega) {
			g.Expect(dpuClusterTestClient.Get(testCtx, client.ObjectKeyFromObject(si), si)).To(Succeed())
			g.Expect(si.Annotations).To(HaveKey(common.LSPConnectedAnnotationKey))
			g.Expect(si.Annotations[common.LSPConnectedAnnotationKey]).To(Equal("true"))
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

		By("deleting ServiceInterface")
		Expect(dpuClusterTestClient.Delete(testCtx, si)).To(Succeed())

		By("ensure service interface is deleted")
		Eventually(func(g Gomega) {
			g.Expect(dpuClusterTestClient.Get(testCtx, client.ObjectKeyFromObject(si), si)).ToNot(Succeed())
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
	})
})
