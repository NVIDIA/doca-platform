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

	"gitlab-master.nvidia.com/doca-platform-foundation/ovn-vpc/ovn/node/nodeutils"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/pkg/ovsmodel"
	"github.com/nvidia/doca-platform/pkg/ovsutils"
	mock_networkhelper "github.com/nvidia/doca-platform/pkg/utils/networkhelper/mock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gomock "go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//nolint:goconst
var _ = Describe("stale ports cleanup", func() {
	var (
		ctrl                  *gomock.Controller
		ovsMock               *ovsutils.MockAPI
		networkHelperMock     *mock_networkhelper.MockNetworkHelper
		ovsConditionalAPIMock *ovsutils.MockConditionalAPI
		staleObjRemover       *StaleObjectRemover
		testNS                *corev1.Namespace
		cleanupObjects        []client.Object
		ctx                   = suiteCtx
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		ovsMock = ovsutils.NewMockAPI(ctrl)
		networkHelperMock = mock_networkhelper.NewMockNetworkHelper(ctrl)

		ovsConditionalAPIMock = ovsutils.NewMockConditionalAPI(ctrl)
		staleObjRemover = NewStaleObjectRemover(0, testClient, "test-node", ovsMock, networkHelperMock)
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
		Expect(staleObjRemover.removeStalePorts(ctx)).ShouldNot(Succeed())
	})

	It("failed to get port from ovs", func() {
		ovsMock.EXPECT().Get(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, bridge *ovsmodel.Bridge) error {
				bridge.Ports = []string{"1"}
				return nil
			},
		).Times(1)

		ovsConditionalAPIMock.EXPECT().List(gomock.Any(), gomock.Any()).Return(fmt.Errorf("failed getting list of ports")).Times(1)
		ovsMock.EXPECT().WhereAll(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(ovsConditionalAPIMock).Times(1)

		Expect(staleObjRemover.removeStalePorts(ctx)).ShouldNot(Succeed())
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
		ovsMock.EXPECT().WhereAll(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(ovsConditionalAPIMock).Times(1)

		ovsMock.EXPECT().GetIfaceWithName(gomock.Any(), gomock.Any()).Return(&ovsmodel.Interface{
			ExternalIDs: map[string]string{
				nodeutils.IfaceIDKey: "some-iface-id",
			},
		}, nil).Times(1)

		ovsMock.EXPECT().DelPort(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)

		Expect(staleObjRemover.removeStalePorts(ctx)).To(Succeed())
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
		ovsMock.EXPECT().WhereAll(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(ovsConditionalAPIMock).Times(1)

		ovsMock.EXPECT().GetIfaceWithName(gomock.Any(), gomock.Any()).Return(&ovsmodel.Interface{
			ExternalIDs: map[string]string{
				nodeutils.IfaceIDKey: "some-iface-id",
			},
		}, nil).Times(1)

		ovsMock.EXPECT().DelPort(gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("failed to delete port"))

		Expect(staleObjRemover.removeStalePorts(ctx)).NotTo(Succeed())
	})

	It("remove only stale ports, vpc service interface", func() {

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
				Node:          ptr.To("test-node"),
				VF: &dpuservicev1.VF{
					PFID:           0,
					VFID:           3,
					VirtualNetwork: ptr.To("testnet1"),
				},
			},
		}
		Expect(testClient.Create(ctx, serviceInterface)).To(Succeed())
		cleanupObjects = append(cleanupObjects, serviceInterface)

		networkHelperMock.EXPECT().GetVFRepresentorDPU("0", "3").Return("pf0vf3", nil).Times(1)

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
		ovsMock.EXPECT().WhereAll(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(ovsConditionalAPIMock).Times(1)

		ovsConditionalAPIMock.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, ports *[]ovsmodel.Port) error {
				*ports = []ovsmodel.Port{
					{Name: "pf0vf3"},
				}
				return nil
			},
		).Times(1)
		ovsMock.EXPECT().WhereAll(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(ovsConditionalAPIMock).Times(1)

		ovsMock.EXPECT().GetIfaceWithName(gomock.Any(), gomock.Any()).Return(&ovsmodel.Interface{
			ExternalIDs: map[string]string{
				nodeutils.IfaceIDKey: "some-iface-id",
			},
		}, nil).Times(2)

		ovsMock.EXPECT().DelPort(gomock.Any(), nodeutils.IntegrationBridge, "some-port").Return(nil).Times(1)

		Expect(staleObjRemover.removeStalePorts(ctx)).To(Succeed())
	})

	It("no ports to be removed", func() {

		ovsMock.EXPECT().Get(gomock.Any(), gomock.AssignableToTypeOf(&ovsmodel.Bridge{})).DoAndReturn(
			func(ctx context.Context, bridge *ovsmodel.Bridge) error {
				bridge.Ports = []string{"1"}
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
		ovsMock.EXPECT().WhereAll(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(ovsConditionalAPIMock).Times(1)

		// There is no port with ovn interfaces that has "iface-id" key in the list, so no port will be removed
		ovsMock.EXPECT().GetIfaceWithName(gomock.Any(), gomock.Any()).Return(&ovsmodel.Interface{
			ExternalIDs: map[string]string{
				"other-key": "other-value",
			},
		}, nil).Times(1)

		Expect(staleObjRemover.removeStalePorts(ctx)).To(Succeed())
	})
})
