/*
COPYRIGHT 2024 NVIDIA

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
	"sync"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/ovsutils"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	kexecTesting "k8s.io/utils/exec/testing"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlConfig "sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

//nolint:goconst
var _ = Describe("service chain controller", func() {
	var (
		mockCtrl       *gomock.Controller
		cleanupObjects []client.Object
		scr            *ServiceChainReconciler
		ofb            *MockBridge
		ovsMock        *ovsutils.MockAPI
		fakeExec       *kexecTesting.FakeExec
		ctx            = context.Background()
		wg             sync.WaitGroup
		testNS         *corev1.Namespace
		node           *corev1.Node
		testNode       = "test-node"
		nn             types.NamespacedName
		testCtx        context.Context
		testCancelFunc context.CancelFunc
		scMock         *MockServiceChainAPI
		openflowMock   *MockOpenFlowAPI
	)

	BeforeEach(func() {
		testCtx, testCancelFunc = context.WithCancel(ctx)
		wg = sync.WaitGroup{}
		mockCtrl = gomock.NewController(GinkgoT())
		ofb = NewMockBridge(mockCtrl)
		ovsMock = ovsutils.NewMockAPI(mockCtrl)
		fakeExec = &kexecTesting.FakeExec{}
		scMock = NewMockServiceChainAPI(mockCtrl)
		openflowMock = NewMockOpenFlowAPI(mockCtrl)

		scr = &ServiceChainReconciler{
			Client:   testClient,
			NodeName: testNodeName,
			OFBridge: ofb,
			OVS:      ovsMock,
			Exec:     fakeExec,
			SC:       scMock,
			OPFlow:   openflowMock,
		}

		testManager, err := ctrl.NewManager(cfg,
			ctrl.Options{
				Scheme: scheme.Scheme,
				// Set metrics server bind address to 0 to disable it.
				Metrics: server.Options{
					BindAddress: "0",
				},
				Controller: ctrlConfig.Controller{
					// this is needed since metrics are registered globally by controller runtime for each controller
					// and we want to allow multiple tests initializing the same controller name.
					SkipNameValidation: ptr.To(true),
				},
			})
		Expect(err).ToNot(HaveOccurred())
		Expect(scr.SetupWithManager(testCtx, testManager)).To(Succeed())

		wg.Add(1)
		go func() {
			defer wg.Done()
			defer GinkgoRecover()
			err := testManager.Start(testCtx)
			Expect(err).ToNot(HaveOccurred(), "failed to run manager")
		}()

		// Wait for the cache to be synced
		Expect(testManager.GetCache().WaitForCacheSync(testCtx)).To(BeTrue(), "cache sync failed")

		By("creating namespace")
		testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "test-ns-"}}
		Expect(testClient.Create(testCtx, testNS)).Should(Succeed())

		nn = types.NamespacedName{
			Namespace: testNS.Name,
			Name:      "test-service-chain",
		}

		By("creating node")
		node = getTestNode(testNode)
		Expect(testClient.Create(testCtx, node)).To(Succeed())
		cleanupObjects = append(cleanupObjects, node)
	})

	AfterEach(func() {
		Expect(testutils.CleanupAndWait(testCtx, testClient, cleanupObjects...)).To(Succeed())
		Expect(testClient.Delete(testCtx, testNS)).To(Succeed())
		mockCtrl.Finish()
		testCancelFunc()
		wg.Wait()
	})

	It("reconcile non existing object - consider as deleted", func() {
		nn = types.NamespacedName{
			Namespace: "non-existing",
			Name:      "non-existing",
		}

		ofb.EXPECT().DeleteFlowsByCookie(hash(nn.String()), gomock.Any()).Return(nil).Times(1)

		result, err := scr.Reconcile(testCtx, ctrl.Request{NamespacedName: nn})
		Expect(err).To(Succeed())
		Expect(result.Requeue).To(BeFalse()) //nolint:staticcheck // This type is deprecated but checking it is false is still part of this test.
		Expect(result.RequeueAfter).To(BeZero())
	})

	It("reconcile service chain with service interface that has virtual network", func() {
		ofb.EXPECT().DeleteFlowsByCookie(hash(nn.String()), gomock.Any()).Return(nil).AnyTimes()
		scMock.EXPECT().GenerateAndApplyOpenFlows(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		By("creating service interface")
		sip0vf2 := getTestVFServiceInterface(
			"test-service-interface-p0vf2",
			testNS.Name,
			testNode,
			0,
			2,
			map[string]string{"svc.dpu.nvidia.com/interface": "p0vf2"},
			ptr.To("test-vn"),
		)

		Expect(testClient.Create(testCtx, sip0vf2)).To(Succeed())
		cleanupObjects = append(cleanupObjects, sip0vf2)

		siList := &dpuservicev1.ServiceInterfaceList{}
		Eventually(func(g Gomega) int {
			g.Expect(testClient.List(testCtx, siList, client.InNamespace(testNS.Name))).To(Succeed())
			return len(siList.Items)
		}).WithTimeout(5 * time.Second).WithPolling(100 * time.Millisecond).Should(Equal(1))

		By("creating service chain")
		sc := &dpuservicev1.ServiceChain{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-service-chain",
				Namespace: testNS.Name,
			},
			Spec: dpuservicev1.ServiceChainSpec{
				Node: &testNode,
				Switches: []dpuservicev1.Switch{
					{
						Ports: []dpuservicev1.Port{
							{
								ServiceInterface: dpuservicev1.ServiceIfc{
									MatchLabels: sip0vf2.Labels,
								},
							},
						},
					},
				},
			},
		}
		Expect(testClient.Create(testCtx, sc)).To(Succeed())
		cleanupObjects = append(cleanupObjects, sc)

		By("checking service chain conditions")
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(sc), sc)).To(Succeed())
			g.Expect(sc.GetConditions()).To(ContainElement(And(
				HaveField("Type", string(dpuservicev1.ServiceChainReconciled)),
				HaveField("Status", metav1.ConditionFalse),
				HaveField("Reason", "Error"),
				HaveField("Message", ContainSubstring("has a virtual network, cannot be chained on br-sfc bridge")),
			)))
			g.Expect(sc.GetConditions()).To(ContainElement(And(
				HaveField("Type", string(conditions.TypeReady)),
				HaveField("Status", metav1.ConditionFalse),
				HaveField("Reason", "Pending"),
			)))
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
	})

	It("reconcile service chain with service interface that is in error state", func() {
		ofb.EXPECT().DeleteFlowsByCookie(hash(nn.String()), gomock.Any()).Return(nil).AnyTimes()
		scMock.EXPECT().GenerateAndApplyOpenFlows(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		By("creating service interface")
		sip0vf3 := getTestVFServiceInterface(
			"test-service-interface-p0vf3",
			testNS.Name,
			testNode,
			0,
			3,
			map[string]string{"svc.dpu.nvidia.com/interface": "p0vf3"},
			nil,
		)
		Expect(testClient.Create(testCtx, sip0vf3)).To(Succeed())
		cleanupObjects = append(cleanupObjects, sip0vf3)

		siList := &dpuservicev1.ServiceInterfaceList{}
		Eventually(func(g Gomega) int {
			g.Expect(testClient.List(testCtx, siList, client.InNamespace(testNS.Name))).To(Succeed())
			return len(siList.Items)
		}).WithTimeout(5 * time.Second).WithPolling(100 * time.Millisecond).Should(Equal(1))

		siList.Items[0].Status.Conditions = []metav1.Condition{
			{
				Type:               string(conditions.TypeReady),
				Status:             metav1.ConditionFalse,
				Reason:             "Pending",
				Message:            "The following conditions are not ready:\n* ServiceChainReconciled",
				LastTransitionTime: metav1.Now(),
			},
			{
				Type:               string(dpuservicev1.ServiceChainReconciled),
				Status:             metav1.ConditionFalse,
				Reason:             "Error",
				Message:            "dummy error message",
				LastTransitionTime: metav1.Now(),
			},
		}

		Expect(testClient.Status().Update(testCtx, &siList.Items[0])).To(Succeed())

		By("creating service chain")
		sc := &dpuservicev1.ServiceChain{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-service-chain",
				Namespace: testNS.Name,
			},
			Spec: dpuservicev1.ServiceChainSpec{
				Node: &testNode,
				Switches: []dpuservicev1.Switch{
					{
						Ports: []dpuservicev1.Port{
							{
								ServiceInterface: dpuservicev1.ServiceIfc{
									MatchLabels: sip0vf3.Labels,
								},
							},
						},
					},
				},
			},
		}
		Expect(testClient.Create(testCtx, sc)).To(Succeed())
		cleanupObjects = append(cleanupObjects, sc)

		By("checking service chain conditions")
		Eventually(func(g Gomega) {
			g.Expect(testClient.Get(testCtx, client.ObjectKeyFromObject(sc), sc)).To(Succeed())
			g.Expect(sc.GetConditions()).To(ContainElement(And(
				HaveField("Type", string(dpuservicev1.ServiceChainReconciled)),
				HaveField("Status", metav1.ConditionFalse),
				HaveField("Reason", "Error"),
				HaveField("Message", ContainSubstring("dummy error message")),
			)))
			g.Expect(sc.GetConditions()).To(ContainElement(And(
				HaveField("Type", string(conditions.TypeReady)),
				HaveField("Status", metav1.ConditionFalse),
				HaveField("Reason", "Pending"),
			)))
		}).WithPolling(500 * time.Millisecond).WithTimeout(5 * time.Second).Should(Succeed())
	})
})
