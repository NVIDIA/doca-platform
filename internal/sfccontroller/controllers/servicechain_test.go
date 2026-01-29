/*
  Copyright 2026 NVIDIA
  Licensed under the Apache License, Version 2.0 (the License);
  you may not use this file except in compliance with the License.
  You may obtain a copy of the License at
      http://www.apache.org/licenses/LICENSE-2.0
  Unless required by applicable law or agreed to in writing, software
  distributed under the License is distributed on an AS IS BASIS,
  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
  See the License for the specific language governing permissions and
  limitations under the License.
*/

package controller

import (
	"context"
	"fmt"

	"github.com/nvidia/doca-platform/pkg/openflow"
	"github.com/nvidia/doca-platform/pkg/ovsmodel"
	"github.com/nvidia/doca-platform/pkg/ovsutils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ovsclient "github.com/ovn-org/libovsdb/client"
	"go.uber.org/mock/gomock"
)

//nolint:goconst
var _ = Describe("servicechain GenerateAndApplyOpenFlows", func() {
	var (
		mockCtrl     *gomock.Controller
		ctx          = context.Background()
		sc           *ServiceChain
		ports        [][]string
		openflowMock *openflow.MockOpenFlowAPI
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		openflowMock = openflow.NewMockOpenFlowAPI(mockCtrl)
		sc = &ServiceChain{OPFlow: openflowMock}
		ports = nil
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	It("should succeed when ports is empty", func() {
		Expect(sc.GenerateAndApplyOpenFlows(ctx, ports, 0)).To(Succeed())
	})

	It("should succeed when ports has one port", func() {
		ports = [][]string{{"1"}}
		// no flows should be added
		Expect(sc.GenerateAndApplyOpenFlows(ctx, ports, 0)).To(Succeed())
	})

	It("should succeed when ports has two ports", func() {
		ports = [][]string{{"1", "2"}}
		expectedFlows := `cookie=0, table=0, priority=20, in_port=1 actions=learn(cookie=0,idle_timeout=10,table=0,priority=30,in_port=2,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),output:2
cookie=0, table=0, priority=20, in_port=2 actions=learn(cookie=0,idle_timeout=10,table=0,priority=30,in_port=1,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),output:1`
		openflowMock.EXPECT().AddFlows(gomock.Any(), expectedFlows, BridgeSFC).Return(nil)
		Expect(sc.GenerateAndApplyOpenFlows(ctx, ports, 0)).To(Succeed())
	})

	It("should succeed when ports has 3 ports", func() {
		ports = [][]string{{"1", "2", "3"}}
		expectedFlows := `cookie=0, table=0, priority=20, in_port=1 actions=learn(cookie=0,idle_timeout=10,table=0,priority=30,in_port=2,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),learn(cookie=0,idle_timeout=10,table=0,priority=30,in_port=3,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),output:2,output:3
cookie=0, table=0, priority=20, in_port=2 actions=learn(cookie=0,idle_timeout=10,table=0,priority=30,in_port=1,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),learn(cookie=0,idle_timeout=10,table=0,priority=30,in_port=3,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),output:1,output:3
cookie=0, table=0, priority=20, in_port=3 actions=learn(cookie=0,idle_timeout=10,table=0,priority=30,in_port=1,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),learn(cookie=0,idle_timeout=10,table=0,priority=30,in_port=2,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),output:1,output:2`
		openflowMock.EXPECT().AddFlows(gomock.Any(), expectedFlows, BridgeSFC).Return(nil)
		Expect(sc.GenerateAndApplyOpenFlows(ctx, ports, 0)).To(Succeed())
	})

	It("should succeed when ports has 2 groups ports", func() {
		ports = [][]string{{"1", "2"}, {"3", "4"}}
		expectedFlows := `cookie=0, table=0, priority=20, in_port=1 actions=learn(cookie=0,idle_timeout=10,table=0,priority=30,in_port=2,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),output:2
cookie=0, table=0, priority=20, in_port=2 actions=learn(cookie=0,idle_timeout=10,table=0,priority=30,in_port=1,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),output:1`
		openflowMock.EXPECT().AddFlows(gomock.Any(), expectedFlows, BridgeSFC).Return(nil)
		expectedFlows = `cookie=0, table=0, priority=20, in_port=3 actions=learn(cookie=0,idle_timeout=10,table=0,priority=30,in_port=4,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),output:4
cookie=0, table=0, priority=20, in_port=4 actions=learn(cookie=0,idle_timeout=10,table=0,priority=30,in_port=3,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),output:3`
		openflowMock.EXPECT().AddFlows(gomock.Any(), expectedFlows, BridgeSFC).Return(nil)
		Expect(sc.GenerateAndApplyOpenFlows(ctx, ports, 0)).To(Succeed())
	})

	It("should contionue even if one of the flows fails", func() {
		ports = [][]string{{"1", "2"}, {"3", "4"}}
		openflowMock.EXPECT().AddFlows(gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("add flows failed"))
		openflowMock.EXPECT().AddFlows(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		Expect(sc.GenerateAndApplyOpenFlows(ctx, ports, 0)).To(MatchError(ContainSubstring("add flows failed")))
	})

	It("should fail when adding flows fails", func() {
		ports = [][]string{{"1", "2"}}
		openflowMock.EXPECT().AddFlows(gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("add flows failed"))
		Expect(sc.GenerateAndApplyOpenFlows(ctx, ports, 0)).To(MatchError(ContainSubstring("add flows failed")))
	})
})

//nolint:goconst
var _ = Describe("findInterface", func() {
	var (
		mockCtrl *gomock.Controller
		ovsMock  *ovsutils.MockAPI
		ctx      = context.Background()
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		ovsMock = ovsutils.NewMockAPI(mockCtrl)
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	It("should find a single interface with matching dpf-id", func() {
		condition := "test-namespace/test-interface"
		ofport := 5
		testIface := ovsmodel.Interface{
			Name:        "test-iface",
			Ofport:      &ofport,
			ExternalIDs: map[string]string{"dpf-id": condition},
		}

		ovsMock.EXPECT().WhereAll(gomock.Any(), gomock.Any()).DoAndReturn(
			func(model interface{}, conds ...interface{}) ovsclient.ConditionalAPI {
				mockConditional := ovsutils.NewMockConditionalAPI(mockCtrl)
				mockConditional.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, result interface{}) error {
						*(result.(*[]ovsmodel.Interface)) = []ovsmodel.Interface{testIface}
						return nil
					},
				)
				return mockConditional
			},
		)
		ovsMock.EXPECT().IsIfaceInBr(gomock.Any(), SFCBridge, "test-iface").Return(true, nil)

		port, err := findInterface(ctx, ovsMock, condition)
		Expect(err).ToNot(HaveOccurred())
		Expect(port).To(Equal("5"))
	})

	It("should find correct interface when two interfaces have same dpf-id (patch port case)", func() {
		condition := "test-namespace/test-patch-interface"
		ofportPatch := 10
		ofportPeerPatch := 20

		patchIface := ovsmodel.Interface{
			Name:        "p_brsfc_to_peer_patch",
			Ofport:      &ofportPatch,
			ExternalIDs: map[string]string{"dpf-id": condition},
		}
		peerPatchIface := ovsmodel.Interface{
			Name:        "peer_patch",
			Ofport:      &ofportPeerPatch,
			ExternalIDs: map[string]string{"dpf-id": condition},
		}

		ovsMock.EXPECT().WhereAll(gomock.Any(), gomock.Any()).DoAndReturn(
			func(model interface{}, conds ...interface{}) ovsclient.ConditionalAPI {
				mockConditional := ovsutils.NewMockConditionalAPI(mockCtrl)
				mockConditional.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, result interface{}) error {
						// Return both interfaces - order matters for test coverage
						*(result.(*[]ovsmodel.Interface)) = []ovsmodel.Interface{patchIface, peerPatchIface}
						return nil
					},
				)
				return mockConditional
			},
		)

		// First interface is in br-sfc, should be selected
		ovsMock.EXPECT().IsIfaceInBr(gomock.Any(), SFCBridge, "p_brsfc_to_peer_patch").Return(true, nil)

		port, err := findInterface(ctx, ovsMock, condition)
		Expect(err).ToNot(HaveOccurred())
		Expect(port).To(Equal("10"))
	})

	It("should find correct interface when two interfaces exist and second one is in br-sfc", func() {
		condition := "test-namespace/test-patch-interface"
		ofportPatch := 10
		ofportPeerPatch := 20

		peerPatchIface := ovsmodel.Interface{
			Name:        "peer_patch",
			Ofport:      &ofportPeerPatch,
			ExternalIDs: map[string]string{"dpf-id": condition},
		}
		patchIface := ovsmodel.Interface{
			Name:        "p_brsfc_to_peer_patch",
			Ofport:      &ofportPatch,
			ExternalIDs: map[string]string{"dpf-id": condition},
		}

		ovsMock.EXPECT().WhereAll(gomock.Any(), gomock.Any()).DoAndReturn(
			func(model interface{}, conds ...interface{}) ovsclient.ConditionalAPI {
				mockConditional := ovsutils.NewMockConditionalAPI(mockCtrl)
				mockConditional.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, result interface{}) error {
						// Return both interfaces - peer patch first
						*(result.(*[]ovsmodel.Interface)) = []ovsmodel.Interface{peerPatchIface, patchIface}
						return nil
					},
				)
				return mockConditional
			},
		)

		// First interface is NOT in br-sfc
		ovsMock.EXPECT().IsIfaceInBr(gomock.Any(), SFCBridge, "peer_patch").Return(false, nil)
		// Second interface IS in br-sfc, should be selected
		ovsMock.EXPECT().IsIfaceInBr(gomock.Any(), SFCBridge, "p_brsfc_to_peer_patch").Return(true, nil)

		port, err := findInterface(ctx, ovsMock, condition)
		Expect(err).ToNot(HaveOccurred())
		Expect(port).To(Equal("10"))
	})

	It("should return error when two interfaces exist but neither is in br-sfc", func() {
		condition := "test-namespace/test-patch-interface"
		ofport := 10

		iface1 := ovsmodel.Interface{
			Name:        "iface1",
			Ofport:      &ofport,
			ExternalIDs: map[string]string{"dpf-id": condition},
		}
		iface2 := ovsmodel.Interface{
			Name:        "iface2",
			Ofport:      &ofport,
			ExternalIDs: map[string]string{"dpf-id": condition},
		}

		ovsMock.EXPECT().WhereAll(gomock.Any(), gomock.Any()).DoAndReturn(
			func(model interface{}, conds ...interface{}) ovsclient.ConditionalAPI {
				mockConditional := ovsutils.NewMockConditionalAPI(mockCtrl)
				mockConditional.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, result interface{}) error {
						*(result.(*[]ovsmodel.Interface)) = []ovsmodel.Interface{iface1, iface2}
						return nil
					},
				)
				return mockConditional
			},
		)

		ovsMock.EXPECT().IsIfaceInBr(gomock.Any(), SFCBridge, "iface1").Return(false, nil)
		ovsMock.EXPECT().IsIfaceInBr(gomock.Any(), SFCBridge, "iface2").Return(false, nil)

		port, err := findInterface(ctx, ovsMock, condition)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("neither interface with dpf-id"))
		Expect(port).To(BeEmpty())
	})

	It("should return error when more than 2 interfaces have the same dpf-id", func() {
		condition := "test-namespace/test-interface"
		ofport := 5

		iface1 := ovsmodel.Interface{
			Name:        "iface1",
			Ofport:      &ofport,
			ExternalIDs: map[string]string{"dpf-id": condition},
		}
		iface2 := ovsmodel.Interface{
			Name:        "iface2",
			Ofport:      &ofport,
			ExternalIDs: map[string]string{"dpf-id": condition},
		}
		iface3 := ovsmodel.Interface{
			Name:        "iface3",
			Ofport:      &ofport,
			ExternalIDs: map[string]string{"dpf-id": condition},
		}

		ovsMock.EXPECT().WhereAll(gomock.Any(), gomock.Any()).DoAndReturn(
			func(model interface{}, conds ...interface{}) ovsclient.ConditionalAPI {
				mockConditional := ovsutils.NewMockConditionalAPI(mockCtrl)
				mockConditional.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, result interface{}) error {
						*(result.(*[]ovsmodel.Interface)) = []ovsmodel.Interface{iface1, iface2, iface3}
						return nil
					},
				)
				return mockConditional
			},
		)

		port, err := findInterface(ctx, ovsMock, condition)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("found 3 interfaces"))
		Expect(err.Error()).To(ContainSubstring("expected at most 2"))
		Expect(port).To(BeEmpty())
	})

	It("should return error when no interface found with matching dpf-id", func() {
		condition := "test-namespace/non-existent"

		ovsMock.EXPECT().WhereAll(gomock.Any(), gomock.Any()).DoAndReturn(
			func(model interface{}, conds ...interface{}) ovsclient.ConditionalAPI {
				mockConditional := ovsutils.NewMockConditionalAPI(mockCtrl)
				mockConditional.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, result interface{}) error {
						*(result.(*[]ovsmodel.Interface)) = []ovsmodel.Interface{}
						return nil
					},
				)
				return mockConditional
			},
		)

		port, err := findInterface(ctx, ovsMock, condition)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to find matching interface"))
		Expect(port).To(BeEmpty())
	})

	It("should return error when interface has nil Ofport", func() {
		condition := "test-namespace/test-interface"

		iface := ovsmodel.Interface{
			Name:        "test-iface",
			Ofport:      nil,
			ExternalIDs: map[string]string{"dpf-id": condition},
		}

		ovsMock.EXPECT().WhereAll(gomock.Any(), gomock.Any()).DoAndReturn(
			func(model interface{}, conds ...interface{}) ovsclient.ConditionalAPI {
				mockConditional := ovsutils.NewMockConditionalAPI(mockCtrl)
				mockConditional.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, result interface{}) error {
						*(result.(*[]ovsmodel.Interface)) = []ovsmodel.Interface{iface}
						return nil
					},
				)
				return mockConditional
			},
		)
		ovsMock.EXPECT().IsIfaceInBr(gomock.Any(), SFCBridge, "test-iface").Return(true, nil)

		port, err := findInterface(ctx, ovsMock, condition)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("Ofport is nil"))
		Expect(port).To(BeEmpty())
	})

	It("should return error when OVS query fails", func() {
		condition := "test-namespace/test-interface"

		ovsMock.EXPECT().WhereAll(gomock.Any(), gomock.Any()).DoAndReturn(
			func(model interface{}, conds ...interface{}) ovsclient.ConditionalAPI {
				mockConditional := ovsutils.NewMockConditionalAPI(mockCtrl)
				mockConditional.EXPECT().List(gomock.Any(), gomock.Any()).Return(fmt.Errorf("OVS query failed"))
				return mockConditional
			},
		)

		port, err := findInterface(ctx, ovsMock, condition)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to get interface with external_ids"))
		Expect(port).To(BeEmpty())
	})
})
