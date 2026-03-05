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
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/ipmanager"
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/topology"
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/topology/mock"

	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/dpucluster"
	testutils "github.com/nvidia/doca-platform/test/utils"
	"github.com/nvidia/doca-platform/test/utils/informer"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlcfg "sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

//nolint:goconst
var _ = Describe("DPUVPCController", func() {
	var (
		cleanupObjs         []client.Object
		testCtx             context.Context
		testCancelFunc      context.CancelFunc
		wg                  sync.WaitGroup
		topologyManagerMock *mock.MockManager
		ipm                 ipmanager.IPManager
	)

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

		Expect(testManager.GetFieldIndexer().IndexField(
			testCtx,
			&vpcv1.DPUVirtualNetwork{},
			"spec.vpcName",
			func(o client.Object) []string {
				vpc := o.(*vpcv1.DPUVirtualNetwork)
				return []string{vpc.Spec.VPCName}
			})).To(Succeed())

		remoteCache, err := dpucluster.SetupRemoteCacheWithManager(testCtx, testManager,
			dpucluster.OptionHostClient{Client: testManager.GetClient()},
			dpucluster.OptionScheme{Scheme: testManager.GetScheme()},
			dpucluster.OptionUserAgent{UserAgent: "ovn-vpc-controller"},
			dpucluster.OptionDisableFor{DisableFor: []client.Object{
				&corev1.ConfigMap{},
				&corev1.Secret{},
			}})
		Expect(err).ToNot(HaveOccurred())

		ipm = ipmanager.NewIPManager()
		vr := &DPUVPCReconciler{
			Client:                              testManager.GetClient(),
			Scheme:                              testManager.GetScheme(),
			IPManager:                           ipm,
			RemoteCache:                         remoteCache,
			CleanupGate:                         NewCleanupGate(),
			topologyManagerFromIsolationClassFn: topologyManagerFromIsolationClassFn,
		}
		Expect(vr.SetupWithManager(testCtx, testManager)).To(Succeed())

		err = (&DPUClusterReconciler{
			Client:           testManager.GetClient(),
			Scheme:           testManager.GetScheme(),
			RemoteCache:      remoteCache,
			WatchRegisterers: []WatchRegisterer{vr},
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
		testCancelFunc()
		wg.Wait()
		// remove objects AFTER controller has stopped (to prevent it from re-adding finalizer)
		Expect(testutils.CleanupWithFinalizerRemovalAndWait(suiteCtx, testClient, cleanupObjs...)).To(Succeed())
	})

	It("should reconcile successfully - no virtual networks", func() {
		topologyManagerMock.EXPECT().ApplyTopology(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).MinTimes(1)
		topologyManagerMock.EXPECT().RemoveTopology(gomock.Any(), gomock.Any()).Return(nil).MinTimes(1)

		isoCls := getTestIsolationClass("test")
		vpc := getTestVPC("test", "test", map[string]string{})
		node := getTestNode("node-0", nil)

		By("creating pre-requisites: dpu k8s nodes and IsolationClass ")
		Expect(dpuClusterTestClient.Create(testCtx, node)).To(Succeed())
		cleanupObjs = append(cleanupObjs, node)

		Expect(testClient.Create(testCtx, isoCls)).To(Succeed())
		cleanupObjs = append(cleanupObjs, isoCls)

		By("creating vpc")
		Expect(testClient.Create(testCtx, vpc)).To(Succeed())
		cleanupObjs = append(cleanupObjs, vpc)

		By("verifying vpc is reconciled successfully")
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(vpc), vpc)).To(Succeed())
			g.Expect(vpc.Annotations).To(HaveKeyWithValue(common.LRPAddressesAnnotationKey, "{\"ipv4\":\"100.64.0.1/16\"}"))
			g.Expect(vpc.ObjectMeta.Finalizers).To(ContainElement(dpuVPCFinalizer))
			g.Expect(conditions.IsTrue(vpc, conditions.TypeReady)).To(BeTrue())
			g.Expect(vpc.Status.VirtualNetworks).To(BeEmpty())
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

		By("verify node is labeled with vpc properly")
		Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(node), node)).To(Succeed())
		Expect(node.Labels).To(HaveKeyWithValue(common.OVNVPCNodeLabelKey, common.ObjectToLabelValue(vpc)))

		By("deleting vpc")
		Expect(testClient.Delete(testCtx, vpc)).To(Succeed())

		By("ensure vpc is deleted")
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(vpc), vpc)).ToNot(Succeed())
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

		By("ensure vpc label is removed from node")
		Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(node), node)).To(Succeed())
		Expect(node.Labels).ToNot(HaveKey(common.OVNVPCNodeLabelKey))
	})

	It("should reconcile successfully - isolationClass created after VPC", func() {
		topologyManagerMock.EXPECT().ApplyTopology(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).MinTimes(1)
		topologyManagerMock.EXPECT().RemoveTopology(gomock.Any(), gomock.Any()).Return(nil).MinTimes(1)

		isoCls := getTestIsolationClass("test")
		vpc := getTestVPC("test", "test", map[string]string{})
		node := getTestNode("node-0", nil)

		By("creating pre-requisites: dpu k8s nodes")
		Expect(dpuClusterTestClient.Create(testCtx, node)).To(Succeed())
		cleanupObjs = append(cleanupObjs, node)

		By("creating vpc")
		Expect(testClient.Create(testCtx, vpc)).To(Succeed())
		cleanupObjs = append(cleanupObjs, vpc)

		By("ensuring vpc is not ready")
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(vpc), vpc)).To(Succeed())
			g.Expect(conditions.IsTrue(vpc, conditions.TypeReady)).To(BeFalse())
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

		Consistently(func(g Gomega) {
			g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(vpc), vpc)).To(Succeed())
			g.Expect(conditions.IsTrue(vpc, conditions.TypeReady)).To(BeFalse())
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

		By("creating isolation class")
		Expect(testClient.Create(testCtx, isoCls)).To(Succeed())
		cleanupObjs = append(cleanupObjs, isoCls)

		By("verifying vpc is reconciled successfully")
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(vpc), vpc)).To(Succeed())
			g.Expect(conditions.IsTrue(vpc, conditions.TypeReady)).To(BeTrue())
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

		By("deleting vpc")
		Expect(testClient.Delete(testCtx, vpc)).To(Succeed())

		By("ensure vpc is deleted")
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(vpc), vpc)).ToNot(Succeed())
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
	})

	It("should reconcile successfully - 2 nodes, only one belongs to VPC", func() {
		topologyManagerMock.EXPECT().ApplyTopology(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).MinTimes(1)
		topologyManagerMock.EXPECT().RemoveTopology(gomock.Any(), gomock.Any()).Return(nil).MinTimes(1)

		isoCls := getTestIsolationClass("test")
		vpc := getTestVPC("test", "test", map[string]string{"tenant": "foo"})
		node0 := getTestNode("node-0", map[string]string{"tenant": "foo"})
		node1 := getTestNode("node-1", map[string]string{"tenant": "bar"})

		By("creating pre-requisites: dpu k8s nodes and IsolationClass ")
		Expect(dpuClusterTestClient.Create(testCtx, node0)).To(Succeed())
		cleanupObjs = append(cleanupObjs, node0)
		Expect(dpuClusterTestClient.Create(testCtx, node1)).To(Succeed())
		cleanupObjs = append(cleanupObjs, node1)

		Expect(testClient.Create(testCtx, isoCls)).To(Succeed())
		cleanupObjs = append(cleanupObjs, isoCls)

		By("creating vpc")
		Expect(testClient.Create(testCtx, vpc)).To(Succeed())
		cleanupObjs = append(cleanupObjs, vpc)

		By("verifying vpc is reconciled successfully")
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(vpc), vpc)).To(Succeed())
			g.Expect(conditions.IsTrue(vpc, conditions.TypeReady)).To(BeTrue())
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

		By("verify node0 is labeled with vpc properly")
		Expect(dpuClusterTestClient.Get(testCtx, client.ObjectKeyFromObject(node0), node0)).To(Succeed())
		Expect(node0.Labels).To(HaveKeyWithValue(common.OVNVPCNodeLabelKey, common.ObjectToLabelValue(vpc)))

		By("verify node1 is not labeled with vpc")
		Expect(dpuClusterTestClient.Get(testCtx, client.ObjectKeyFromObject(node1), node1)).To(Succeed())
		Expect(node1.Labels).ToNot(HaveKey(common.OVNVPCNodeLabelKey))

		By("deleting vpc")
		Expect(testClient.Delete(testCtx, vpc)).To(Succeed())

		By("ensure vpc is deleted")
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(vpc), vpc)).ToNot(Succeed())
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
	})

	It("should reconcile - with virtual networks", func() {
		topologyManagerMock.EXPECT().ApplyTopology(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).MinTimes(1)
		topologyManagerMock.EXPECT().RemoveTopology(gomock.Any(), gomock.Any()).Return(nil).MinTimes(1)

		isoCls := getTestIsolationClass("test")
		vpc := getTestVPC("test", "test", map[string]string{})
		node := getTestNode("node-0", nil)
		vn0 := getTestDPUVirtualNetwork("vn0", vpc.Name, "10.0.0.0/16", map[string]string{})
		vn1 := getTestDPUVirtualNetwork("vn1", vpc.Name, "10.1.0.0/16", map[string]string{})

		By("creating pre-requisites: dpu k8s nodes and IsolationClass ")
		Expect(dpuClusterTestClient.Create(testCtx, node)).To(Succeed())
		cleanupObjs = append(cleanupObjs, node)

		Expect(testClient.Create(testCtx, isoCls)).To(Succeed())
		cleanupObjs = append(cleanupObjs, isoCls)

		By("creating vpc")
		Expect(testClient.Create(testCtx, vpc)).To(Succeed())
		cleanupObjs = append(cleanupObjs, vpc)

		By("creating one virtual network")
		Expect(testClient.Create(testCtx, vn0)).To(Succeed())
		cleanupObjs = append(cleanupObjs, vn0)
		conditions.AddTrue(vn0, conditions.TypeReady)
		Expect(testClient.Status().Update(testCtx, vn0)).To(Succeed())

		By("verifying vpc is reconciled successfully - virtual network is present in status")
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(vpc), vpc)).To(Succeed())
			g.Expect(vpc.Annotations).To(HaveKeyWithValue(common.LRPAddressesAnnotationKey, "{\"ipv4\":\"100.64.0.1/16\"}"))
			g.Expect(conditions.IsTrue(vpc, conditions.TypeReady)).To(BeTrue())
			g.Expect(vpc.Status.VirtualNetworks).To(HaveLen(1))
			g.Expect(vpc.Status.VirtualNetworks[0].Name).To(Equal(vn0.Name))
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

		By("create another virtual Network")
		Expect(testClient.Create(testCtx, vn1)).To(Succeed())
		cleanupObjs = append(cleanupObjs, vn1)
		conditions.AddTrue(vn1, conditions.TypeReady)
		Expect(testClient.Status().Update(testCtx, vn1)).To(Succeed())

		By("verifying vpc is reconciled successfully - both virtual networks are present in status")
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(vpc), vpc)).To(Succeed())
			g.Expect(vpc.Annotations).To(HaveKeyWithValue(common.LRPAddressesAnnotationKey, "{\"ipv4\":\"100.64.0.1/16\"}"))
			g.Expect(conditions.IsTrue(vpc, conditions.TypeReady)).To(BeTrue())
			g.Expect(vpc.Status.VirtualNetworks).To(HaveLen(2))
			g.Expect(vpc.Status.VirtualNetworks[0].Name).To(Equal(vn0.Name))
			g.Expect(vpc.Status.VirtualNetworks[1].Name).To(Equal(vn1.Name))
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

		By("deleting virtual networks")
		Expect(testClient.Delete(testCtx, vn0)).To(Succeed())
		Expect(testClient.Delete(testCtx, vn1)).To(Succeed())

		By("deleting vpc")
		Expect(testClient.Delete(testCtx, vpc)).To(Succeed())

		By("ensure vpc is deleted")
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(vpc), vpc)).ToNot(Succeed())
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
	})

	It("should reconcile successfully - virtual network deleted", func() {
		topologyManagerMock.EXPECT().ApplyTopology(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).MinTimes(1)
		topologyManagerMock.EXPECT().RemoveTopology(gomock.Any(), gomock.Any()).Return(nil).MinTimes(1)

		isoCls := getTestIsolationClass("test")
		vpc := getTestVPC("test", "test", map[string]string{})
		node := getTestNode("node-0", nil)
		vn := getTestDPUVirtualNetwork("vn0", vpc.Name, "10.0.0.0/16", map[string]string{})

		By("creating pre-requisites: dpu k8s nodes and IsolationClass ")
		Expect(dpuClusterTestClient.Create(testCtx, node)).To(Succeed())
		cleanupObjs = append(cleanupObjs, node)

		Expect(testClient.Create(testCtx, isoCls)).To(Succeed())
		cleanupObjs = append(cleanupObjs, isoCls)

		By("creating virtual network")
		Expect(testClient.Create(testCtx, vn)).To(Succeed())
		cleanupObjs = append(cleanupObjs, vn)
		conditions.AddTrue(vn, conditions.TypeReady)
		Expect(testClient.Status().Update(testCtx, vn)).To(Succeed())

		By("creating vpc")
		Expect(testClient.Create(testCtx, vpc)).To(Succeed())
		cleanupObjs = append(cleanupObjs, vpc)

		By("verifying vpc is reconciled successfully - virtual networks is present in status")
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(vpc), vpc)).To(Succeed())
			g.Expect(conditions.IsTrue(vpc, conditions.TypeReady)).To(BeTrue())
			g.Expect(vpc.Status.VirtualNetworks).To(HaveLen(1))
			g.Expect(vpc.Status.VirtualNetworks[0].Name).To(Equal(vn.Name))
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

		By("deleting virtual networks")
		Expect(testClient.Delete(testCtx, vn)).To(Succeed())

		By("verifying virtual netwok is gone from vpc status")
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(vpc), vpc)).To(Succeed())
			g.Expect(conditions.IsTrue(vpc, conditions.TypeReady)).To(BeTrue())
			g.Expect(vpc.Status.VirtualNetworks).To(BeEmpty())
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

		By("deleting vpc")
		Expect(testClient.Delete(testCtx, vpc)).To(Succeed())

		By("ensure vpc is deleted")
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(vpc), vpc)).ToNot(Succeed())
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
	})

	It("should reconcile successfully - node added to vpc", func() {
		topologyManagerMock.EXPECT().ApplyTopology(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).MinTimes(1)
		topologyManagerMock.EXPECT().RemoveTopology(gomock.Any(), gomock.Any()).Return(nil).MinTimes(1)

		isoCls := getTestIsolationClass("test")
		vpc := getTestVPC("test", "test", map[string]string{})
		node := getTestNode("node-0", nil)

		By("creating pre-requisites: IsolationClass")
		Expect(testClient.Create(testCtx, isoCls)).To(Succeed())
		cleanupObjs = append(cleanupObjs, isoCls)

		By("creating vpc")
		Expect(testClient.Create(testCtx, vpc)).To(Succeed())
		cleanupObjs = append(cleanupObjs, vpc)

		By("verifying vpc is reconciled successfully")
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(vpc), vpc)).To(Succeed())
			g.Expect(vpc.Annotations).To(HaveKeyWithValue(common.LRPAddressesAnnotationKey, "{\"ipv4\":\"100.64.0.1/16\"}"))
			g.Expect(vpc.ObjectMeta.Finalizers).To(ContainElement(dpuVPCFinalizer))
			g.Expect(conditions.IsTrue(vpc, conditions.TypeReady)).To(BeTrue())
			g.Expect(vpc.Status.VirtualNetworks).To(BeEmpty())
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

		By("create node")
		Expect(dpuClusterTestClient.Create(testCtx, node)).To(Succeed())
		cleanupObjs = append(cleanupObjs, node)

		By("verify node labels and annotations")
		Eventually(func(g Gomega) {
			g.Expect(dpuClusterTestClient.Get(testCtx, client.ObjectKeyFromObject(node), node)).To(Succeed())
			g.Expect(node.Labels).To(HaveKeyWithValue(common.OVNVPCNodeLabelKey, common.ObjectToLabelValue(vpc)))
			g.Expect(node.Annotations).To(HaveKeyWithValue(common.LRPAddressesAnnotationKey, "{\"ipv4\":\"100.64.0.2/16\"}"))
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

		By("deleting vpc")
		Expect(testClient.Delete(testCtx, vpc)).To(Succeed())

		By("ensure vpc is deleted")
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(vpc), vpc)).ToNot(Succeed())
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

		By("ensure vpc label is removed from node")
		Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(node), node)).To(Succeed())
		Expect(node.Labels).ToNot(HaveKey(common.OVNVPCNodeLabelKey))
	})

	It("should reconcile successfully - node label update", func() {
		topologyManagerMock.EXPECT().ApplyTopology(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).MinTimes(1)
		topologyManagerMock.EXPECT().RemoveTopology(gomock.Any(), gomock.Any()).Return(nil).MinTimes(1)

		isoCls := getTestIsolationClass("test")
		vpc := getTestVPC("test", "test", map[string]string{"tenant": "foo"})
		node := getTestNode("node-0", map[string]string{"tenant": "bar"})

		By("creating pre-requisites: k8s Node and IsolationClass")
		Expect(dpuClusterTestClient.Create(testCtx, node)).To(Succeed())
		cleanupObjs = append(cleanupObjs, node)

		Expect(testClient.Create(testCtx, isoCls)).To(Succeed())
		cleanupObjs = append(cleanupObjs, isoCls)

		By("creating vpc")
		Expect(testClient.Create(testCtx, vpc)).To(Succeed())
		cleanupObjs = append(cleanupObjs, vpc)

		By("verifying vpc is reconciled successfully")
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(vpc), vpc)).To(Succeed())
			g.Expect(conditions.IsTrue(vpc, conditions.TypeReady)).To(BeTrue())
			g.Expect(vpc.Status.VirtualNetworks).To(BeEmpty())
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

		By("verify node is not labeled with vpc label")
		Expect(dpuClusterTestClient.Get(testCtx, client.ObjectKeyFromObject(node), node)).To(Succeed())
		Expect(node.Labels).ToNot(HaveKey(common.OVNVPCNodeLabelKey))

		By("update node labels to match vpc")
		node.Labels["tenant"] = "foo"
		Expect(dpuClusterTestClient.Update(testCtx, node)).To(Succeed())

		By("verify node labeled and annotations updated")
		Eventually(func(g Gomega) {
			g.Expect(dpuClusterTestClient.Get(testCtx, client.ObjectKeyFromObject(node), node)).To(Succeed())
			g.Expect(node.Labels).To(HaveKeyWithValue(common.OVNVPCNodeLabelKey, common.ObjectToLabelValue(vpc)))
			g.Expect(node.Annotations).To(HaveKeyWithValue(common.LRPAddressesAnnotationKey, "{\"ipv4\":\"100.64.0.2/16\"}"))
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

		By("update node labels to not match vpc")
		node.Labels["tenant"] = "bar"
		Expect(dpuClusterTestClient.Update(testCtx, node)).To(Succeed())

		By("verify node labels and annotations updated")
		Eventually(func(g Gomega) {
			g.Expect(dpuClusterTestClient.Get(testCtx, client.ObjectKeyFromObject(node), node)).To(Succeed())
			g.Expect(node.Labels).ToNot(HaveKey(common.OVNVPCNodeLabelKey))
			g.Expect(node.Labels).ToNot(HaveKey(common.LRPAddressesAnnotationKey))
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

		By("deleting vpc")
		Expect(testClient.Delete(testCtx, vpc)).To(Succeed())

		By("ensure vpc is deleted")
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(vpc), vpc)).ToNot(Succeed())
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
	})

	It("should reconcile successfully - node changes VPC", func() {
		topologyManagerMock.EXPECT().ApplyTopology(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).MinTimes(1)

		isoCls := getTestIsolationClass("test")
		vpcFoo := getTestVPC("test-foo", "test", map[string]string{"tenant": "foo"})
		vpcBar := getTestVPC("test-bar", "test", map[string]string{"tenant": "bar"})
		vnBar := getTestDPUVirtualNetwork("vn-bar", vpcBar.Name, "10.0.0.0/16", map[string]string{})
		node := getTestNode("node-0", map[string]string{"tenant": "foo"})
		node2 := getTestNode("node-1", map[string]string{"tenant": "bar"})

		By("creating pre-requisites: k8s Node and IsolationClass")
		Expect(dpuClusterTestClient.Create(testCtx, node)).To(Succeed())
		cleanupObjs = append(cleanupObjs, node)
		Expect(dpuClusterTestClient.Create(testCtx, node2)).To(Succeed())
		cleanupObjs = append(cleanupObjs, node2)

		Expect(testClient.Create(testCtx, isoCls)).To(Succeed())
		cleanupObjs = append(cleanupObjs, isoCls)

		By("creating virtual networks")
		Expect(testClient.Create(testCtx, vnBar)).To(Succeed())
		cleanupObjs = append(cleanupObjs, vnBar)
		conditions.AddTrue(vnBar, conditions.TypeReady)
		Expect(testClient.Status().Update(testCtx, vnBar)).To(Succeed())

		By("creating vpc")
		Expect(testClient.Create(testCtx, vpcFoo)).To(Succeed())
		cleanupObjs = append(cleanupObjs, vpcFoo)
		Expect(testClient.Create(testCtx, vpcBar)).To(Succeed())
		cleanupObjs = append(cleanupObjs, vpcBar)

		By("verifying vpcs are reconciled successfully")
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(vpcFoo), vpcFoo)).To(Succeed())
			g.Expect(conditions.IsTrue(vpcFoo, conditions.TypeReady)).To(BeTrue())

			g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(vpcBar), vpcBar)).To(Succeed())
			g.Expect(conditions.IsTrue(vpcBar, conditions.TypeReady)).To(BeTrue())
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

		By("verify node labels and annotations updated")
		Eventually(func(g Gomega) {
			g.Expect(dpuClusterTestClient.Get(testCtx, client.ObjectKeyFromObject(node), node)).To(Succeed())
			g.Expect(node.Labels).To(HaveKeyWithValue(common.OVNVPCNodeLabelKey, common.ObjectToLabelValue(vpcFoo)))
			g.Expect(node.Annotations).To(HaveKeyWithValue(common.LRPAddressesAnnotationKey, "{\"ipv4\":\"100.64.0.2/16\"}"))

			g.Expect(dpuClusterTestClient.Get(testCtx, client.ObjectKeyFromObject(node2), node2)).To(Succeed())
			g.Expect(node2.Labels).To(HaveKeyWithValue(common.OVNVPCNodeLabelKey, common.ObjectToLabelValue(vpcBar)))
			g.Expect(node2.Annotations).To(HaveKeyWithValue(common.LRPAddressesAnnotationKey, "{\"ipv4\":\"100.64.0.2/16\"}"))
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

		By("update node labels to match other vpc")
		node.Labels["tenant"] = "bar"
		Expect(dpuClusterTestClient.Update(testCtx, node)).To(Succeed())

		By("verify node labels and annotations updated")
		Eventually(func(g Gomega) {
			g.Expect(dpuClusterTestClient.Get(testCtx, client.ObjectKeyFromObject(node), node)).To(Succeed())
			g.Expect(node.Labels).To(HaveKeyWithValue(common.OVNVPCNodeLabelKey, common.ObjectToLabelValue(vpcBar)))
			g.Expect(node.Annotations).To(HaveKeyWithValue(common.LRPAddressesAnnotationKey, "{\"ipv4\":\"100.64.0.3/16\"}"))
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
	})

	It("should reconcile successfully - node deleted from vpc", func() {
		topologyManagerMock.EXPECT().ApplyTopology(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).MinTimes(1)

		isoCls := getTestIsolationClass("test")
		vpcFoo := getTestVPC("test-foo", "test", map[string]string{"tenant": "foo"})
		node1 := getTestNode("node-1", map[string]string{"tenant": "foo"})

		By("creating pre-requisites: k8s Node and IsolationClass")
		Expect(dpuClusterTestClient.Create(testCtx, node1)).To(Succeed())
		cleanupObjs = append(cleanupObjs, node1)

		Expect(testClient.Create(testCtx, isoCls)).To(Succeed())
		cleanupObjs = append(cleanupObjs, isoCls)

		By("creating vpc")
		Expect(testClient.Create(testCtx, vpcFoo)).To(Succeed())
		cleanupObjs = append(cleanupObjs, vpcFoo)

		By("verifying vpcs are reconciled successfully")
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(vpcFoo), vpcFoo)).To(Succeed())
			g.Expect(conditions.IsTrue(vpcFoo, conditions.TypeReady)).To(BeTrue())
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

		ipa := ipm.GetNetworkIPAllocator(ipmanager.ObjToID(vpcFoo), ipmanager.VPCClusterNetworkIPV4)
		Expect(ipa).ToNot(BeNil())

		By("verify node-1 labels and annotations updated")
		Eventually(func(g Gomega) {
			g.Expect(dpuClusterTestClient.Get(testCtx, client.ObjectKeyFromObject(node1), node1)).To(Succeed())
			g.Expect(node1.Labels).To(HaveKeyWithValue(common.OVNVPCNodeLabelKey, common.ObjectToLabelValue(vpcFoo)))
			g.Expect(node1.Annotations).To(HaveKey(common.LRPAddressesAnnotationKey))
			g.Expect(ipa.ListAllocationIDs()).To(HaveLen(2))
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

		By("deleting node-1")
		Expect(dpuClusterTestClient.Delete(testCtx, node1)).To(Succeed())

		By("wait for VPC IP allocation to be released for node-1")
		Eventually(func(g Gomega) {
			g.Expect(dpuClusterTestClient.Get(testCtx, client.ObjectKeyFromObject(node1), node1)).ToNot(Succeed())
			g.Expect(ipa.ListAllocationIDs()).To(HaveLen(1))
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
	})

	Context("State transitions", func() {
		var i *informer.TestInformer

		BeforeEach(func() {
			i = informer.NewInformer(cfg, vpcv1.DPUVPCGroupVersionKind, "default", "dpuvpcs")
			DeferCleanup(i.Cleanup)
			go i.Run()

			Eventually(i.HasSynced).WithTimeout(5 * time.Second).WithPolling(500 * time.Millisecond).Should(BeTrue())
		})

		It("transitions as expected", func() {
			topologyManagerMock.EXPECT().ApplyTopology(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).MinTimes(1)
			topologyManagerMock.EXPECT().RemoveTopology(gomock.Any(), gomock.Any()).Return(nil).MinTimes(1)

			isoCls := getTestIsolationClass("test-iso-cls")
			vpc := getTestVPC("test-vpc", isoCls.Name, map[string]string{})
			vn := getTestDPUVirtualNetwork("test-vn", vpc.Name, "10.0.0.0/16", nil)

			By("creating DPUVirtualNetwork")
			Expect(testClient.Create(testCtx, vn)).To(Succeed())
			cleanupObjs = append(cleanupObjs, vn)

			By("creating DPUVPC")
			Expect(testClient.Create(testCtx, vpc)).To(Succeed())
			cleanupObjs = append(cleanupObjs, vpc)

			By("verifying DPUVPC conditions")
			Eventually(func(g Gomega) {
				var ev informer.Event
				g.Eventually(i.UpdateEvents).Should(Receive(&ev))
				newObj := &vpcv1.DPUVPC{}
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())
				g.Expect(newObj.Status.Conditions).To(ConsistOf(
					And(
						HaveField("Type", string(vpcv1.ConditionPreReqsReady)),
						HaveField("Status", metav1.ConditionFalse),
						HaveField("Reason", string(conditions.ReasonPending)),
					),
					And(
						HaveField("Type", string(vpcv1.ConditionDPUVPCReconciled)),
						HaveField("Status", metav1.ConditionUnknown),
						HaveField("Reason", string(conditions.ReasonPending)),
					),
					And(
						HaveField("Type", string(vpcv1.ConditionDPUNodesReconciled)),
						HaveField("Status", metav1.ConditionUnknown),
						HaveField("Reason", string(conditions.ReasonPending)),
					),
					And(
						HaveField("Type", string(vpcv1.ConditionTopologyReconciled)),
						HaveField("Status", metav1.ConditionUnknown),
						HaveField("Reason", string(conditions.ReasonPending)),
					),
					And(
						HaveField("Type", string(conditions.TypeReady)),
						HaveField("Status", metav1.ConditionFalse),
					),
				))
			}).WithPolling(100 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

			By("creating IsolationClass")
			Expect(testClient.Create(testCtx, isoCls)).To(Succeed())
			cleanupObjs = append(cleanupObjs, isoCls)

			By("Mark DPUVirtualNetwork as ready")
			conditions.AddTrue(vn, conditions.TypeReady)
			Expect(testClient.Status().Update(testCtx, vn)).To(Succeed())

			By("verifying DPUVPC conditions all true")
			Eventually(func(g Gomega) {
				var ev informer.Event
				g.Eventually(i.UpdateEvents).Should(Receive(&ev))
				newObj := &vpcv1.DPUVPC{}
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())
				g.Expect(newObj.Status.Conditions).To(ConsistOf(
					And(
						HaveField("Type", string(vpcv1.ConditionPreReqsReady)),
						HaveField("Status", metav1.ConditionTrue),
					),
					And(
						HaveField("Type", string(vpcv1.ConditionDPUVPCReconciled)),
						HaveField("Status", metav1.ConditionTrue),
					),
					And(
						HaveField("Type", string(vpcv1.ConditionDPUNodesReconciled)),
						HaveField("Status", metav1.ConditionTrue),
					),
					And(
						HaveField("Type", string(vpcv1.ConditionTopologyReconciled)),
						HaveField("Status", metav1.ConditionTrue),
					),
					And(
						HaveField("Type", string(conditions.TypeReady)),
						HaveField("Status", metav1.ConditionTrue),
					),
				))
			}).WithPolling(100 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

			By("Delete DPUVPC")
			Expect(testClient.Delete(testCtx, vpc)).To(Succeed())

			Eventually(func(g Gomega) {
				var ev informer.Event
				g.Eventually(i.UpdateEvents).Should(Receive(&ev))
				newObj := &vpcv1.DPUVPC{}
				g.Expect(testClient.Scheme().Convert(ev.NewObj, newObj, nil)).ToNot(HaveOccurred())
				g.Expect(newObj.Status.Conditions).To(ContainElement(
					And(
						HaveField("Type", string(vpcv1.ConditionDPUVPCReconciled)),
						HaveField("Status", metav1.ConditionFalse),
						HaveField("Reason", string(conditions.ReasonAwaitingDeletion)),
					),
				))
			}).WithPolling(100 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

			By("Delete virtual network")
			Expect(testClient.Delete(testCtx, vn)).To(Succeed())

			By("ensuring DPUVPC is deleted")
			Eventually(func(g Gomega) {
				g.Expect(testClient.Get(testCtx, client.ObjectKey{Namespace: vpc.Namespace, Name: vpc.Name}, vpc)).ToNot(Succeed())
			}).WithTimeout(5 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())
		})
	})
})
