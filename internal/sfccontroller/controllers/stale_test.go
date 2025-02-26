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
	"fmt"
	"math"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/internal/ovsmodel"
	"github.com/nvidia/doca-platform/internal/ovsutils"

	"antrea.io/antrea/pkg/ovs/openflow"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gomock "go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//nolint:goconst
var _ = Describe("stale flows cleanup", func() {
	var (
		ctrl           *gomock.Controller
		sor            *StaleObjectRemover
		ofb            *MockBridge
		zero           uint64
		testNS         *corev1.Namespace
		cleanupObjects []client.Object
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		ofb = NewMockBridge(ctrl)
		sor = NewStaleObjectRemover(0, testClient, ofb, nil)
		cleanupObjects = []client.Object{}
	})

	AfterEach(func() {
		for _, obj := range cleanupObjects {
			Expect(testClient.Delete(ctx, obj)).To(Succeed())
		}

		ctrl.Finish()
	})

	It("no flows", func() {
		ofb.EXPECT().DumpFlows(zero, zero).Return(map[uint64]*openflow.FlowStates{}, nil).Times(1)
		Expect(sor.removeStaleFlows(ctx)).To(Succeed())
	})

	It("stale flow", func() {
		ofb.EXPECT().DumpFlows(zero, zero).Return(map[uint64]*openflow.FlowStates{100: {}}, nil).Times(1)
		ofb.EXPECT().DeleteFlowsByCookie(uint64(100), uint64(math.MaxUint64)).Return(nil).Times(1)
		Expect(sor.removeStaleFlows(ctx)).To(Succeed())
	})

	It("delete only stale", func() {
		testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "test-ns-"}}
		Expect(testClient.Create(ctx, testNS)).To(Succeed())
		cleanupObjects = append(cleanupObjects, testNS)

		serviceChainNamespacedName := testNS.Name + "/" + "dpu-service-chain"
		ofb.EXPECT().DumpFlows(zero, zero).
			Return(map[uint64]*openflow.FlowStates{100: {}, hash(serviceChainNamespacedName): {}}, nil).Times(1)
		ofb.EXPECT().DeleteFlowsByCookie(uint64(100), uint64(math.MaxUint64)).Return(nil).Times(1)

		testServiceChain := &dpuservicev1.ServiceChain{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dpu-service-chain",
				Namespace: testNS.Name,
			},
			// spec is not relevant for the test
			Spec: dpuservicev1.ServiceChainSpec{
				Switches: []dpuservicev1.Switch{
					{
						Ports: []dpuservicev1.Port{
							{
								ServiceInterface: dpuservicev1.ServiceIfc{
									MatchLabels: map[string]string{"p1": "srv1"},
								},
							},
						},
					},
				},
			},
		}
		Expect(testClient.Create(ctx, testServiceChain)).To(Succeed())
		cleanupObjects = append(cleanupObjects, testServiceChain)

		Expect(sor.removeStaleFlows(ctx)).To(Succeed())
	})

	It("failed to dump flows", func() {
		ofb.EXPECT().DumpFlows(gomock.Any(), gomock.Any()).Return(nil, fmt.Errorf("error")).Times(1)
		Expect(sor.removeStaleFlows(ctx)).ShouldNot(Succeed())
	})

	It("failed to remove flows", func() {
		ofb.EXPECT().DumpFlows(zero, zero).Return(map[uint64]*openflow.FlowStates{100: {}}, nil).Times(1)
		ofb.EXPECT().DeleteFlowsByCookie(gomock.Any(), gomock.Any()).Return(fmt.Errorf("error")).Times(1)
		Expect(sor.removeStaleFlows(ctx)).ShouldNot(Succeed())
	})

})

//nolint:goconst
var _ = Describe("stale ports cleanup", func() {
	var (
		ctrl                  *gomock.Controller
		ovsMock               *ovsutils.MockAPI
		ovsConditionalAPIMock *ovsutils.MockConditionalAPI
		sor                   *StaleObjectRemover
		testNS                *corev1.Namespace
		cleanupObjects        []client.Object
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		ovsMock = ovsutils.NewMockAPI(ctrl)

		ovsConditionalAPIMock = ovsutils.NewMockConditionalAPI(ctrl)
		sor = NewStaleObjectRemover(0, testClient, nil, ovsMock)
		cleanupObjects = []client.Object{}
	})

	AfterEach(func() {
		for _, obj := range cleanupObjects {
			Expect(testClient.Delete(ctx, obj)).To(Succeed())
		}
		ctrl.Finish()
	})

	It("failed to get bridge from ovs", func() {
		ovsMock.EXPECT().Get(gomock.Any(), gomock.Any()).Return(fmt.Errorf("error getting ovs bridge")).Times(1)
		Expect(sor.removeStalePorts(ctx)).ShouldNot(Succeed())
	})

	It("failed to get port from ovs", func() {
		ovsMock.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, bridge *ovsmodel.Bridge) error {
				bridge.Ports = []string{"1"}
				return nil
			},
		).Times(1)

		ovsConditionalAPIMock.EXPECT().List(gomock.Any(), gomock.Any()).Return(fmt.Errorf("failed getting list of ports")).Times(1)
		ovsMock.EXPECT().WhereAll(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(ovsConditionalAPIMock).Times(1)

		Expect(sor.removeStalePorts(ctx)).ShouldNot(Succeed())
	})

	It("remove stale port, no service interface", func() {
		ovsMock.EXPECT().Get(gomock.Any(), gomock.AssignableToTypeOf(&ovsmodel.Bridge{})).DoAndReturn(
			func(ctx context.Context, bridge *ovsmodel.Bridge) error {
				bridge.Ports = []string{"1"}
				return nil
			},
		).Times(1)

		ovsConditionalAPIMock.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, ports *[]ovsmodel.Port) error {
				*ports = []ovsmodel.Port{
					{
						Name: "some-port",
					},
				}
				return nil
			},
		).Times(1)
		ovsMock.EXPECT().WhereAll(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(ovsConditionalAPIMock).Times(1)

		ovsMock.EXPECT().DelPort(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)

		Expect(sor.removeStalePorts(ctx)).To(Succeed())
	})

	It("failed to delete port", func() {
		ovsMock.EXPECT().Get(gomock.Any(), gomock.AssignableToTypeOf(&ovsmodel.Bridge{})).DoAndReturn(
			func(ctx context.Context, bridge *ovsmodel.Bridge) error {
				bridge.Ports = []string{"1"}
				return nil
			},
		).Times(1)

		ovsConditionalAPIMock.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, ports *[]ovsmodel.Port) error {
				*ports = []ovsmodel.Port{
					{
						Name: "some-port",
					},
				}
				return nil
			},
		).Times(1)
		ovsMock.EXPECT().WhereAll(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(ovsConditionalAPIMock).Times(1)

		ovsMock.EXPECT().DelPort(gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("failed to delete port"))

		Expect(sor.removeStalePorts(ctx)).NotTo(Succeed())
	})

	It("remove only stale ports", func() {

		testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "test-ns-"}}
		Expect(testClient.Create(ctx, testNS)).To(Succeed())
		cleanupObjects = append(cleanupObjects, testNS)

		serviceInterface := &dpuservicev1.ServiceInterface{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dpu-service-interface",
				Namespace: testNS.Name,
			},
			Spec: dpuservicev1.ServiceInterfaceSpec{
				InterfaceType: "vf",
				VF: &dpuservicev1.VF{
					PFID:               0,
					VFID:               3,
					ParentInterfaceRef: "pf0",
				},
			},
		}
		Expect(testClient.Create(ctx, serviceInterface)).To(Succeed())
		cleanupObjects = append(cleanupObjects, serviceInterface)

		ovsMock.EXPECT().Get(gomock.Any(), gomock.AssignableToTypeOf(&ovsmodel.Bridge{})).DoAndReturn(
			func(ctx context.Context, bridge *ovsmodel.Bridge) error {
				bridge.Ports = []string{"1", "2"}
				return nil
			},
		).Times(1)

		ovsConditionalAPIMock.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, ports *[]ovsmodel.Port) error {
				*ports = []ovsmodel.Port{
					{Name: "some-port"},
				}
				return nil
			},
		).Times(1)
		ovsMock.EXPECT().WhereAll(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(ovsConditionalAPIMock).Times(1)

		ovsConditionalAPIMock.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, ports *[]ovsmodel.Port) error {
				*ports = []ovsmodel.Port{
					{Name: "pf0vf3"},
				}
				return nil
			},
		).Times(1)
		ovsMock.EXPECT().WhereAll(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(ovsConditionalAPIMock).Times(1)

		ovsMock.EXPECT().DelPort(gomock.Any(), SFCBridge, "some-port").Return(nil).Times(1)

		Expect(sor.removeStalePorts(ctx)).To(Succeed())
	})

})
