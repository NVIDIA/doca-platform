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
	"errors"
	"sync"
	"time"

	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/common"
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/ovnlib"
	ovnlib_mock "gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/ovnlib/mock"
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/sbdb"
	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/topology"
	topology_mock "gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/topology/mock"

	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
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

var _ = Describe("TopologyCleaner", func() {
	var (
		cleanupObjs           []client.Object
		dpuClusterCleanupObjs []client.Object
		testCtx               context.Context
		testCancelFunc        context.CancelFunc
		wg                    sync.WaitGroup
		mockCtrl              *gomock.Controller
		topologyManagerMock   *topology_mock.MockManager
		ovnsbClientMock       *ovnlib_mock.MockOVNSBWrapper
		tc                    *TopologyCleaner
		remoteCache           *dpucluster.RemoteCache
		mgrClient             client.Client
	)

	//nolint:unparam
	getMatchObjCondFn := func(namespace, name string) func(o client.Object) bool {
		return func(o client.Object) bool {
			return namespace == o.GetNamespace() && name == o.GetName()
		}
	}

	BeforeEach(func() {
		cleanupObjs = make([]client.Object, 0)
		dpuClusterCleanupObjs = make([]client.Object, 0)
		testCtx, testCancelFunc = context.WithCancel(suiteCtx)
		wg = sync.WaitGroup{}

		mockCtrl = gomock.NewController(GinkgoT())
		topologyManagerMock = topology_mock.NewMockManager(mockCtrl)
		ovnsbClientMock = ovnlib_mock.NewMockOVNSBWrapper(mockCtrl)
		By("overriding topologyManagerFromIsolationClassFn to return a mock")
		topologyManagerFromIsolationClassFn := func(ctx context.Context, isoCls *vpcv1.IsolationClass) (topology.Manager, error) {
			return topologyManagerMock, nil
		}
		By("overriding ovnSBClientFromIsolationClassFn to return a mock")
		ovnSBClientFromIsolationClassFn := func(ctx context.Context, isoCls *vpcv1.IsolationClass) (ovnlib.OVNSBWrapper, error) {
			return ovnsbClientMock, nil
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
		mgrClient = testManager.GetClient()

		Expect(testManager.GetFieldIndexer().IndexField(
			testCtx,
			&vpcv1.DPUVirtualNetwork{},
			"spec.vpcName",
			func(o client.Object) []string {
				vn := o.(*vpcv1.DPUVirtualNetwork)
				return []string{vn.Spec.VPCName}
			})).To(Succeed())

		Expect(testManager.GetFieldIndexer().IndexField(
			testCtx,
			&vpcv1.DPUVPC{},
			"spec.isolationClassName",
			func(o client.Object) []string {
				vpc := o.(*vpcv1.DPUVPC)
				return []string{vpc.Spec.IsolationClassName}
			})).To(Succeed())

		remoteCache, err = dpucluster.SetupRemoteCacheWithManager(testCtx, testManager,
			dpucluster.OptionHostClient{Client: testManager.GetClient()},
			dpucluster.OptionScheme{Scheme: testManager.GetScheme()},
			dpucluster.OptionUserAgent{UserAgent: "ovn-vpc-controller"},
			dpucluster.OptionDisableFor{DisableFor: []client.Object{
				&corev1.ConfigMap{},
				&corev1.Secret{},
			}})
		Expect(err).ToNot(HaveOccurred())

		tc = &TopologyCleaner{
			Client:                              testManager.GetClient(),
			RemoteCache:                         remoteCache,
			CleanupGate:                         NewCleanupGate(),
			topologyManagerFromIsolationClassFn: topologyManagerFromIsolationClassFn,
			ovnSBClientFromIsolationClassFn:     ovnSBClientFromIsolationClassFn,
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			defer GinkgoRecover()
			err = testManager.Start(testCtx)
			Expect(err).ToNot(HaveOccurred(), "failed to run manager")
		}()

		Expect(testManager.GetCache().WaitForCacheSync(testCtx)).To(BeTrue())

		// wait for dpucluster to be ready in remote cache
		Eventually(func(g Gomega) {
			clients, err := remoteCache.ListClients()
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(clients).To(HaveLen(1))
		}).WithPolling(500 * time.Millisecond).WithTimeout(10 * time.Second).Should(Succeed())
	})

	AfterEach(func() {
		testCancelFunc()
		wg.Wait()
		// remove objects AFTER controller has stopped (to prevent it from re-adding finalizer)
		Expect(testutils.CleanupWithFinalizerRemovalAndWait(suiteCtx, testClient, cleanupObjs...)).To(Succeed())
		Expect(testutils.CleanupWithFinalizerRemovalAndWait(suiteCtx, dpuClusterTestClient, dpuClusterCleanupObjs...)).To(Succeed())
		mockCtrl.Finish()
	})

	Context("reconcile - cleanup ovn topology", func() {
		BeforeEach(func() {
			ovnsbClientMock.EXPECT().ListChassis(gomock.Any(), gomock.Any()).Return([]*sbdb.Chassis{}, nil).AnyTimes()
		})

		It("should reconcile successfully - no isolation class", func() {
			Expect(tc.reconcile(testCtx)).To(Succeed())
		})

		It("should reconcile successfully - only isolation class no stale resources in topology", func() {
			topologyManagerMock.EXPECT().ListVPCs(gomock.Any()).Return([]client.ObjectKey{}, nil).MinTimes(1)
			topologyManagerMock.EXPECT().ListServiceInterfaces(gomock.Any()).Return([]topology.ServiceInterfacRef{}, nil).MinTimes(1)

			isoCls := getTestIsolationClass("test")
			Expect(testClient.Create(testCtx, isoCls)).To(Succeed())
			cleanupObjs = append(cleanupObjs, isoCls)

			// wait until object is in cache as initial list call may already have been done
			// before objects were created
			Eventually(func(g Gomega) {
				g.Expect(mgrClient.Get(testCtx, client.ObjectKeyFromObject(isoCls), isoCls)).To(Succeed())
			}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(tc.reconcile(testCtx)).To(Succeed())
			}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
		})

		It("should reconcile successfully - stale VPCs in topology no VPC CRs", func() {
			vpcsInTopology := []client.ObjectKey{
				{Namespace: "default", Name: "vpc-1"},
				{Namespace: "default", Name: "vpc-2"},
			}
			topologyManagerMock.EXPECT().ListVPCs(gomock.Any()).Return(vpcsInTopology, nil).MinTimes(1)
			topologyManagerMock.EXPECT().ListServiceInterfaces(gomock.Any()).Return([]topology.ServiceInterfacRef{}, nil).MinTimes(1)
			topologyManagerMock.EXPECT().RemoveTopology(gomock.Any(), gomock.Cond(getMatchObjCondFn("default", "vpc-1"))).Return(nil).MinTimes(1)
			topologyManagerMock.EXPECT().RemoveTopology(gomock.Any(), gomock.Cond(getMatchObjCondFn("default", "vpc-2"))).Return(nil).MinTimes(1)

			// create isolation class
			isoCls := getTestIsolationClass("test")
			Expect(testClient.Create(testCtx, isoCls)).To(Succeed())
			cleanupObjs = append(cleanupObjs, isoCls)

			// wait until object is in cache as initial list call may already have been done
			// before objects were created
			Eventually(func(g Gomega) {
				g.Expect(mgrClient.Get(testCtx, client.ObjectKeyFromObject(isoCls), isoCls)).To(Succeed())
			}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(tc.reconcile(testCtx)).To(Succeed())
			}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
		})

		It("should reconcile successfully - stale vpc", func() {
			vpcsInTopology := []client.ObjectKey{
				{Namespace: "default", Name: "vpc-1"},
				{Namespace: "default", Name: "vpc-2"},
			}
			topologyManagerMock.EXPECT().ListVPCs(gomock.Any()).Return(vpcsInTopology, nil).MinTimes(1)
			topologyManagerMock.EXPECT().ListServiceInterfaces(gomock.Any()).Return([]topology.ServiceInterfacRef{}, nil).MinTimes(1)
			topologyManagerMock.EXPECT().RemoveTopology(gomock.Any(), gomock.Cond(getMatchObjCondFn("default", "vpc-1"))).Return(nil).MinTimes(1)

			// create isolation class
			isoCls := getTestIsolationClass("test")
			Expect(testClient.Create(testCtx, isoCls)).To(Succeed())
			cleanupObjs = append(cleanupObjs, isoCls)

			// create vpc
			vpc := getTestVPC("vpc-2", "test", map[string]string{})
			Expect(testClient.Create(testCtx, vpc)).To(Succeed())
			cleanupObjs = append(cleanupObjs, vpc)

			// wait until vpc is in remote cache as initial list call may already have been done
			// before objects were created
			Eventually(func(g Gomega) {
				g.Expect(mgrClient.Get(testCtx, client.ObjectKeyFromObject(vpc), vpc)).To(Succeed())
			}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(tc.reconcile(testCtx)).To(Succeed())
			}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
		})

		It("should reconcile successfully - stale service interfaces", func() {
			vpcsInTopology := []client.ObjectKey{
				{Namespace: "default", Name: "vpc-1"},
			}
			siInTopology := []topology.ServiceInterfacRef{
				{
					ServiceInterface: client.ObjectKey{Namespace: "default", Name: "si-1"},
					VirtualNetwork:   client.ObjectKey{Namespace: "default", Name: "vn-1"},
					VPC:              client.ObjectKey{Namespace: "default", Name: "vpc-1"},
				},
				{
					ServiceInterface: client.ObjectKey{Namespace: "default", Name: "si-2"},
					VirtualNetwork:   client.ObjectKey{Namespace: "default", Name: "vn-1"},
					VPC:              client.ObjectKey{Namespace: "default", Name: "vpc-1"},
				},
			}
			topologyManagerMock.EXPECT().ListVPCs(gomock.Any()).Return(vpcsInTopology, nil).MinTimes(1)
			topologyManagerMock.EXPECT().ListServiceInterfaces(gomock.Any()).Return(siInTopology, nil).MinTimes(1)
			topologyManagerMock.EXPECT().UnplugServiceInterface(gomock.Any(), gomock.Any(), gomock.Cond(getMatchObjCondFn("default", "si-1"))).Return(nil).MinTimes(1)

			// create isolation class
			isoCls := getTestIsolationClass("test")
			Expect(testClient.Create(testCtx, isoCls)).To(Succeed())
			cleanupObjs = append(cleanupObjs, isoCls)

			// create vpc
			vpc := getTestVPC("vpc-1", "test", map[string]string{})
			Expect(testClient.Create(testCtx, vpc)).To(Succeed())
			cleanupObjs = append(cleanupObjs, vpc)

			// create vn
			vn := getTestDPUVirtualNetwork("vn-1", "vpc-1", "10.0.0.0/24", map[string]string{})
			Expect(testClient.Create(testCtx, vn)).To(Succeed())
			cleanupObjs = append(cleanupObjs, vn)

			// create service interface
			si := getTestServiceInterface("si-2", "vn-1", "node-1")
			Expect(dpuClusterTestClient.Create(testCtx, si)).To(Succeed())
			cleanupObjs = append(cleanupObjs, si)

			// wait until objects are in local & remote cache as initial list call may already have been done
			// before objects were created
			Eventually(func(g Gomega) {
				dpuClient, err := remoteCache.GetClient(client.ObjectKey{Namespace: "default", Name: "envtest"})
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(dpuClient.Get(testCtx, client.ObjectKeyFromObject(si), si)).To(Succeed())
				g.Expect(mgrClient.Get(testCtx, client.ObjectKeyFromObject(vn), vn)).To(Succeed())
			}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(tc.reconcile(testCtx)).To(Succeed())
			}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
		})

		It("fails if listing VPCs in ovn fails", func() {
			topologyManagerMock.EXPECT().ListVPCs(gomock.Any()).Return([]client.ObjectKey{}, errors.New("failed by test")).MinTimes(1)
			topologyManagerMock.EXPECT().ListServiceInterfaces(gomock.Any()).Return([]topology.ServiceInterfacRef{}, nil).MinTimes(1)

			isoCls := getTestIsolationClass("test")
			Expect(testClient.Create(testCtx, isoCls)).To(Succeed())
			cleanupObjs = append(cleanupObjs, isoCls)

			Eventually(func(g Gomega) {
				g.Expect(tc.reconcile(testCtx)).ToNot(Succeed())
				g.Expect(mockCtrl.Satisfied()).To(BeTrue())
			}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
		})

		It("fails if listing service interfaces in ovn fails", func() {
			topologyManagerMock.EXPECT().ListVPCs(gomock.Any()).Return([]client.ObjectKey{}, nil).MinTimes(1)
			topologyManagerMock.EXPECT().ListServiceInterfaces(gomock.Any()).Return([]topology.ServiceInterfacRef{}, errors.New("failed by test")).MinTimes(1)

			isoCls := getTestIsolationClass("test")
			Expect(testClient.Create(testCtx, isoCls)).To(Succeed())
			cleanupObjs = append(cleanupObjs, isoCls)

			Eventually(func(g Gomega) {
				g.Expect(tc.reconcile(testCtx)).ToNot(Succeed())
				g.Expect(mockCtrl.Satisfied()).To(BeTrue())
			}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
		})
	})

	Context("reconcile - cleanup stale chassis", func() {
		BeforeEach(func() {
			topologyManagerMock.EXPECT().ListVPCs(gomock.Any()).Return([]client.ObjectKey{}, nil).MinTimes(1)
			topologyManagerMock.EXPECT().ListServiceInterfaces(gomock.Any()).Return([]topology.ServiceInterfacRef{}, nil).MinTimes(1)
		})

		It("should reconcile successfully - no chassis", func() {
			ovnsbClientMock.EXPECT().ListChassis(gomock.Any(), gomock.Any()).Return([]*sbdb.Chassis{}, nil).MinTimes(1)

			isoCls := getTestIsolationClass("test")
			Expect(testClient.Create(testCtx, isoCls)).To(Succeed())
			cleanupObjs = append(cleanupObjs, isoCls)

			// wait for isolation class to be in cache
			Eventually(func(g Gomega) {
				g.Expect(mgrClient.Get(testCtx, client.ObjectKeyFromObject(isoCls), isoCls)).To(Succeed())
			}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

			// reconcile
			Eventually(func(g Gomega) {
				g.Expect(tc.reconcile(testCtx)).To(Succeed())
			}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
		})

		It("should reconcile successfully - no stale chassis", func() {
			// single node single chassis, mock chassis and encap
			ovnsbClientMock.EXPECT().ListChassis(gomock.Any(), gomock.Any()).Return(
				[]*sbdb.Chassis{
					{Name: "dpu-node-1", UUID: "1111", Encaps: []string{"2222"}},
				},
				nil).MinTimes(1)
			ovnsbClientMock.EXPECT().GetEncap(gomock.Any(), gomock.Any()).Return(
				&sbdb.Encap{UUID: "2222", IP: "20.0.0.2"},
				nil).MinTimes(1)

			// create isolation class
			isoCls := getTestIsolationClass("test")
			Expect(testClient.Create(testCtx, isoCls)).To(Succeed())
			cleanupObjs = append(cleanupObjs, isoCls)

			// create node
			node := getTestNode("dpu-node-1", map[string]string{})
			Expect(dpuClusterTestClient.Create(testCtx, node)).To(Succeed())
			dpuClusterCleanupObjs = append(dpuClusterCleanupObjs, node)

			// wait for node to be present in remote cache and isolation class to be in cache
			Eventually(func(g Gomega) {
				dpuClient, err := remoteCache.GetClient(client.ObjectKey{Namespace: "default", Name: "envtest"})
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(dpuClient.Get(testCtx, client.ObjectKeyFromObject(node), node)).To(Succeed())
				g.Expect(mgrClient.Get(testCtx, client.ObjectKeyFromObject(isoCls), isoCls)).To(Succeed())
			}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

			// reconcile
			Eventually(func(g Gomega) {
				g.Expect(tc.reconcile(testCtx)).To(Succeed())
			}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
		})

		It("should reconcile successfully - stale chassis", func() {
			// single node, two chassis, mock chassis and encap
			ovnsbClientMock.EXPECT().ListChassis(gomock.Any(), gomock.Any()).Return(
				[]*sbdb.Chassis{
					{Name: "dpu-node-1", UUID: "1111", Encaps: []string{"2222"}},
					{Name: "dpu-node-2", UUID: "3333", Encaps: []string{"4444"}},
				},
				nil).MinTimes(1)
			ovnsbClientMock.EXPECT().DeleteChassis(gomock.Any(), &sbdb.ChassisDeleteParams{Name: "dpu-node-2"}).Return(nil).MinTimes(1)
			ovnsbClientMock.EXPECT().GetEncap(gomock.Any(), &sbdb.EncapGetParams{UUID: "2222"}).Return(
				&sbdb.Encap{UUID: "2222", IP: "20.0.0.2"},
				nil).MinTimes(1)

			// create isolation class
			isoCls := getTestIsolationClass("test")
			Expect(testClient.Create(testCtx, isoCls)).To(Succeed())
			cleanupObjs = append(cleanupObjs, isoCls)

			// create node
			node := getTestNode("dpu-node-1", map[string]string{})
			Expect(dpuClusterTestClient.Create(testCtx, node)).To(Succeed())
			dpuClusterCleanupObjs = append(dpuClusterCleanupObjs, node)

			// wait for node to be present in remote cache and isolation class to be in cache
			Eventually(func(g Gomega) {
				dpuClient, err := remoteCache.GetClient(client.ObjectKey{Namespace: "default", Name: "envtest"})
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(dpuClient.Get(testCtx, client.ObjectKeyFromObject(node), node)).To(Succeed())
				g.Expect(mgrClient.Get(testCtx, client.ObjectKeyFromObject(isoCls), isoCls)).To(Succeed())
			}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

			// reconcile
			Eventually(func(g Gomega) {
				g.Expect(tc.reconcile(testCtx)).To(Succeed())
			}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
		})

		It("should reconcile successfully - stale chassis - vtep ip mismatch", func() {
			// single node, single chassis, mock chassis and encap
			ovnsbClientMock.EXPECT().ListChassis(gomock.Any(), gomock.Any()).Return(
				[]*sbdb.Chassis{
					{Name: "dpu-node-1", UUID: "1111", Encaps: []string{"2222"}},
				},
				nil).MinTimes(1)
			ovnsbClientMock.EXPECT().DeleteChassis(gomock.Any(), &sbdb.ChassisDeleteParams{Name: "dpu-node-1"}).Return(nil).MinTimes(1)
			ovnsbClientMock.EXPECT().GetEncap(gomock.Any(), &sbdb.EncapGetParams{UUID: "2222"}).Return(
				&sbdb.Encap{UUID: "2222", IP: "20.0.0.4"}, // vtep ip mismatch
				nil).MinTimes(1)

			// create isolation class
			isoCls := getTestIsolationClass("test")
			Expect(testClient.Create(testCtx, isoCls)).To(Succeed())
			cleanupObjs = append(cleanupObjs, isoCls)

			// create node
			node := getTestNode("dpu-node-1", map[string]string{}) // vtep ip is 20.0.0.2
			Expect(dpuClusterTestClient.Create(testCtx, node)).To(Succeed())
			dpuClusterCleanupObjs = append(dpuClusterCleanupObjs, node)

			// wait for node to be present in remote cache and isolation class to be in cache
			Eventually(func(g Gomega) {
				dpuClient, err := remoteCache.GetClient(client.ObjectKey{Namespace: "default", Name: "envtest"})
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(dpuClient.Get(testCtx, client.ObjectKeyFromObject(node), node)).To(Succeed())
				g.Expect(mgrClient.Get(testCtx, client.ObjectKeyFromObject(isoCls), isoCls)).To(Succeed())
			}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

			// reconcile
			Eventually(func(g Gomega) {
				g.Expect(tc.reconcile(testCtx)).To(Succeed())
			}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
		})

		It("should reconcile successfully if node does not have vtep ip set", func() {
			// single node, single chassis, mock chassis and encap
			ovnsbClientMock.EXPECT().ListChassis(gomock.Any(), gomock.Any()).Return(
				[]*sbdb.Chassis{
					{Name: "dpu-node-1", UUID: "1111", Encaps: []string{"2222"}},
				},
				nil).MinTimes(1)

			// create isolation class
			isoCls := getTestIsolationClass("test")
			Expect(testClient.Create(testCtx, isoCls)).To(Succeed())
			cleanupObjs = append(cleanupObjs, isoCls)

			// create node
			node := getTestNode("dpu-node-1", map[string]string{})
			// remove vtep ip annotation
			delete(node.Annotations, common.OVNVtepIPAnnotationKey)
			Expect(dpuClusterTestClient.Create(testCtx, node)).To(Succeed())
			dpuClusterCleanupObjs = append(dpuClusterCleanupObjs, node)

			// wait for node to be present in remote cache and isolation class to be in cache
			Eventually(func(g Gomega) {
				dpuClient, err := remoteCache.GetClient(client.ObjectKey{Namespace: "default", Name: "envtest"})
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(dpuClient.Get(testCtx, client.ObjectKeyFromObject(node), node)).To(Succeed())
				g.Expect(mgrClient.Get(testCtx, client.ObjectKeyFromObject(isoCls), isoCls)).To(Succeed())
			}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

			// reconcile
			Eventually(func(g Gomega) {
				g.Expect(tc.reconcile(testCtx)).To(Succeed())
			}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
		})

		It("should fail if list chassis fails", func() {
			// single node, single chassis, mock chassis and encap
			ovnsbClientMock.EXPECT().ListChassis(gomock.Any(), gomock.Any()).Return(nil, errors.New("test error")).MinTimes(1)
			// create isolation class
			isoCls := getTestIsolationClass("test")
			Expect(testClient.Create(testCtx, isoCls)).To(Succeed())
			cleanupObjs = append(cleanupObjs, isoCls)

			// wait for isolation class to be in cache
			Eventually(func(g Gomega) {
				g.Expect(mgrClient.Get(testCtx, client.ObjectKeyFromObject(isoCls), isoCls)).To(Succeed())
			}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

			// reconcile
			Eventually(func(g Gomega) {
				g.Expect(tc.reconcile(testCtx)).ToNot(Succeed())
				g.Expect(mockCtrl.Satisfied()).To(BeTrue())
			}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
		})

		It("should fail if delete chassis fails", func() {
			// single node, two chassis, mock chassis and encap
			ovnsbClientMock.EXPECT().ListChassis(gomock.Any(), gomock.Any()).Return(
				[]*sbdb.Chassis{
					{Name: "node-1", UUID: "1111", Encaps: []string{"2222"}},
				},
				nil).MinTimes(1)
			ovnsbClientMock.EXPECT().DeleteChassis(gomock.Any(), &sbdb.ChassisDeleteParams{Name: "node-1"}).Return(errors.New("test error")).MinTimes(1)

			// create isolation class
			isoCls := getTestIsolationClass("test")
			Expect(testClient.Create(testCtx, isoCls)).To(Succeed())
			cleanupObjs = append(cleanupObjs, isoCls)

			// wait isolation class to be in cache
			Eventually(func(g Gomega) {
				g.Expect(mgrClient.Get(testCtx, client.ObjectKeyFromObject(isoCls), isoCls)).To(Succeed())
			}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

			// reconcile
			Eventually(func(g Gomega) {
				g.Expect(tc.reconcile(testCtx)).ToNot(Succeed())
				g.Expect(mockCtrl.Satisfied()).To(BeTrue())
			}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
		})

		It("should fail if get encap fails", func() {
			// single node, single chassis, mock chassis and encap
			ovnsbClientMock.EXPECT().ListChassis(gomock.Any(), gomock.Any()).Return(
				[]*sbdb.Chassis{
					{Name: "dpu-node-1", UUID: "1111", Encaps: []string{"2222"}},
				},
				nil).MinTimes(1)
			ovnsbClientMock.EXPECT().GetEncap(gomock.Any(), &sbdb.EncapGetParams{UUID: "2222"}).Return(
				nil, errors.New("test error")).MinTimes(1)

			// create isolation class
			isoCls := getTestIsolationClass("test")
			Expect(testClient.Create(testCtx, isoCls)).To(Succeed())
			cleanupObjs = append(cleanupObjs, isoCls)

			// create node
			node := getTestNode("dpu-node-1", map[string]string{})
			Expect(dpuClusterTestClient.Create(testCtx, node)).To(Succeed())
			dpuClusterCleanupObjs = append(dpuClusterCleanupObjs, node)

			// wait for node to be present in remote cache and isolation class to be in cache
			Eventually(func(g Gomega) {
				dpuClient, err := remoteCache.GetClient(client.ObjectKey{Namespace: "default", Name: "envtest"})
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(dpuClient.Get(testCtx, client.ObjectKeyFromObject(node), node)).To(Succeed())
				g.Expect(mgrClient.Get(testCtx, client.ObjectKeyFromObject(isoCls), isoCls)).To(Succeed())
			}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())

			// reconcile
			Eventually(func(g Gomega) {
				g.Expect(tc.reconcile(testCtx)).ToNot(Succeed())
				g.Expect(mockCtrl.Satisfied()).To(BeTrue())
			}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
		})
	})
})
