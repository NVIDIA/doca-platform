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
	"errors"
	"fmt"
	"sync"
	"time"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/internal/features"
	"github.com/nvidia/doca-platform/internal/utils"
	"github.com/nvidia/doca-platform/pkg/conditions"
	"github.com/nvidia/doca-platform/pkg/openflow"
	"github.com/nvidia/doca-platform/pkg/ovsmodel"
	"github.com/nvidia/doca-platform/pkg/ovsutils"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ovsclient "github.com/ovn-org/libovsdb/client"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/kubernetes/scheme"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	kexecTesting "k8s.io/utils/exec/testing"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	ctrlConfig "sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
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
		expectedFlows := `cookie=0, table=0, priority=0, in_port=1 actions=learn(cookie=0,idle_timeout=10,table=1,priority=1,in_port=2,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),resubmit(,1)
cookie=0, table=1, priority=2, in_port=1, dl_dst=01:00:00:00:00:00/01:00:00:00:00:00 actions=output:2
cookie=0, table=1, priority=0, in_port=1 actions=output:2
cookie=0, table=0, priority=0, in_port=2 actions=learn(cookie=0,idle_timeout=10,table=1,priority=1,in_port=1,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),resubmit(,1)
cookie=0, table=1, priority=2, in_port=2, dl_dst=01:00:00:00:00:00/01:00:00:00:00:00 actions=output:1
cookie=0, table=1, priority=0, in_port=2 actions=output:1`
		openflowMock.EXPECT().AddFlows(gomock.Any(), expectedFlows, BridgeSFC).Return(nil)
		Expect(sc.GenerateAndApplyOpenFlows(ctx, ports, 0)).To(Succeed())
	})

	It("should succeed when ports has 3 ports", func() {
		ports = [][]string{{"1", "2", "3"}}
		expectedFlows := `cookie=0, table=0, priority=0, in_port=1 actions=learn(cookie=0,idle_timeout=10,table=1,priority=1,in_port=2,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),learn(cookie=0,idle_timeout=10,table=1,priority=1,in_port=3,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),resubmit(,1)
cookie=0, table=1, priority=2, in_port=1, dl_dst=01:00:00:00:00:00/01:00:00:00:00:00 actions=output:2,output:3
cookie=0, table=1, priority=0, in_port=1 actions=output:2,output:3
cookie=0, table=0, priority=0, in_port=2 actions=learn(cookie=0,idle_timeout=10,table=1,priority=1,in_port=1,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),learn(cookie=0,idle_timeout=10,table=1,priority=1,in_port=3,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),resubmit(,1)
cookie=0, table=1, priority=2, in_port=2, dl_dst=01:00:00:00:00:00/01:00:00:00:00:00 actions=output:1,output:3
cookie=0, table=1, priority=0, in_port=2 actions=output:1,output:3
cookie=0, table=0, priority=0, in_port=3 actions=learn(cookie=0,idle_timeout=10,table=1,priority=1,in_port=1,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),learn(cookie=0,idle_timeout=10,table=1,priority=1,in_port=2,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),resubmit(,1)
cookie=0, table=1, priority=2, in_port=3, dl_dst=01:00:00:00:00:00/01:00:00:00:00:00 actions=output:1,output:2
cookie=0, table=1, priority=0, in_port=3 actions=output:1,output:2`
		openflowMock.EXPECT().AddFlows(gomock.Any(), expectedFlows, BridgeSFC).Return(nil)
		Expect(sc.GenerateAndApplyOpenFlows(ctx, ports, 0)).To(Succeed())
	})

	It("should succeed when ports has 2 groups ports", func() {
		ports = [][]string{{"1", "2"}, {"3", "4"}}
		expectedFlows := `cookie=0, table=0, priority=0, in_port=1 actions=learn(cookie=0,idle_timeout=10,table=1,priority=1,in_port=2,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),resubmit(,1)
cookie=0, table=1, priority=2, in_port=1, dl_dst=01:00:00:00:00:00/01:00:00:00:00:00 actions=output:2
cookie=0, table=1, priority=0, in_port=1 actions=output:2
cookie=0, table=0, priority=0, in_port=2 actions=learn(cookie=0,idle_timeout=10,table=1,priority=1,in_port=1,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),resubmit(,1)
cookie=0, table=1, priority=2, in_port=2, dl_dst=01:00:00:00:00:00/01:00:00:00:00:00 actions=output:1
cookie=0, table=1, priority=0, in_port=2 actions=output:1`
		openflowMock.EXPECT().AddFlows(gomock.Any(), expectedFlows, BridgeSFC).Return(nil)
		expectedFlows = `cookie=0, table=0, priority=0, in_port=3 actions=learn(cookie=0,idle_timeout=10,table=1,priority=1,in_port=4,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),resubmit(,1)
cookie=0, table=1, priority=2, in_port=3, dl_dst=01:00:00:00:00:00/01:00:00:00:00:00 actions=output:4
cookie=0, table=1, priority=0, in_port=3 actions=output:4
cookie=0, table=0, priority=0, in_port=4 actions=learn(cookie=0,idle_timeout=10,table=1,priority=1,in_port=3,dl_dst=dl_src,output:NXM_OF_IN_PORT[]),resubmit(,1)
cookie=0, table=1, priority=2, in_port=4, dl_dst=01:00:00:00:00:00/01:00:00:00:00:00 actions=output:3
cookie=0, table=1, priority=0, in_port=4 actions=output:3`
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

// conditionMatcher matches an OVS lookup for the interface whose external-id condition equals the given value.
type conditionMatcher struct{ condition string }

func (m conditionMatcher) Matches(x interface{}) bool {
	iface, ok := x.(*ovsmodel.Interface)
	return ok && iface.ExternalIDs[ovsutils.DPFIDKey] == m.condition
}

func (m conditionMatcher) String() string {
	return "interface with condition " + m.condition
}

// Exercises buildChainPorts/resolveSwitchPort directly, proving a failing "subject" port never affects a healthy sibling port.
var _ = Describe("service chain controller port resolution", func() {
	var (
		mockCtrl      *gomock.Controller
		ovsMock       *ovsutils.MockAPI
		siControl     *dpuservicev1.ServiceInterface
		subjectLabels map[string]string
		sc            *dpuservicev1.ServiceChain
		ctx           = context.Background()
	)

	const (
		controlIface  = "test-iface-control"
		controlOfport = 10
	)

	setReady := func(si *dpuservicev1.ServiceInterface, ready bool) {
		status := metav1.ConditionTrue
		if !ready {
			status = metav1.ConditionFalse
		}
		si.Status.Conditions = []metav1.Condition{
			{
				Type:               string(conditions.TypeReady),
				Status:             status,
				Reason:             "Reason",
				Message:            "not ready for testing",
				LastTransitionTime: metav1.Now(),
				ObservedGeneration: si.Generation,
			},
		}
	}

	// registerOVSLookupForCondition makes ovsMock resolve condition to an OVS interface named name with ofport, re-read on every call.
	registerOVSLookupForCondition := func(condition, name string, ofport *int) {
		ovsMock.EXPECT().WhereAll(conditionMatcher{condition}, gomock.Any()).DoAndReturn(
			func(model interface{}, conds ...interface{}) ovsclient.ConditionalAPI {
				mockConditional := ovsutils.NewMockConditionalAPI(mockCtrl)
				mockConditional.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, result interface{}) error {
						*(result.(*[]ovsmodel.Interface)) = []ovsmodel.Interface{{
							Name:        name,
							Ofport:      ofport,
							ExternalIDs: map[string]string{ovsutils.DPFIDKey: condition},
						}}
						return nil
					},
				)
				return mockConditional
			},
		).AnyTimes()
		ovsMock.EXPECT().IsIfaceInBr(gomock.Any(), SFCBridge, name).Return(true, nil).AnyTimes()
	}

	// registerOVSLookup is registerOVSLookupForCondition for a legacy ServiceInterface.
	registerOVSLookup := func(si *dpuservicev1.ServiceInterface, name string, ofport *int) {
		registerOVSLookupForCondition(si.Namespace+"/"+si.Name, name, ofport)
	}

	// registerNSIOVSLookup is registerOVSLookupForCondition for an NSI InterfaceEntry, keyed by its own Name.
	registerNSIOVSLookup := func(entry dpuservicev1.InterfaceEntry, name string, ofport *int) {
		registerOVSLookupForCondition(entry.Name, name, ofport)
	}

	// registerOVSLookupNotFound simulates a ServiceInterface that is Ready but not yet bound in OVS.
	registerOVSLookupNotFound := func(si *dpuservicev1.ServiceInterface) {
		condition := si.Namespace + "/" + si.Name
		ovsMock.EXPECT().WhereAll(conditionMatcher{condition}, gomock.Any()).DoAndReturn(
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
		).AnyTimes()
	}

	// newNSIEntry builds a VF-type InterfaceEntry with the given labels.
	newNSIEntry := func(lbls map[string]string, vfID int) dpuservicev1.InterfaceEntry {
		return dpuservicev1.InterfaceEntry{
			Name:          "default_nsi-entry-subject",
			Labels:        lbls,
			InterfaceType: dpuservicev1.InterfaceTypeVF,
			VF:            &dpuservicev1.VF{PFID: 0, VFID: vfID},
		}
	}

	// newSFCNodeServiceInterfaces builds this node's "sfc"-typed NodeServiceInterfaces object, entries Ready by default.
	newSFCNodeServiceInterfaces := func(entries ...dpuservicev1.InterfaceEntry) *dpuservicev1.NodeServiceInterfaces {
		nsi := &dpuservicev1.NodeServiceInterfaces{
			ObjectMeta: metav1.ObjectMeta{Name: "test-nsi", Namespace: utils.NSIObjectsNamespace},
			Spec: dpuservicev1.NodeServiceInterfacesSpec{
				Node:       testNodeName,
				Type:       dpuservicev1.NSITypeSFC,
				Interfaces: entries,
			},
		}
		for _, entry := range entries {
			nsi.Status.InterfaceStatuses = append(nsi.Status.InterfaceStatuses, dpuservicev1.InterfaceEntryStatus{
				Name: entry.Name,
				Conditions: []metav1.Condition{{
					Type:               string(conditions.TypeReady),
					Status:             metav1.ConditionTrue,
					Reason:             "Reason",
					Message:            "ready for testing",
					LastTransitionTime: metav1.Now(),
					ObservedGeneration: nsi.Generation,
				}},
			})
		}
		return nsi
	}

	// setNSIEntryNotReady flips the status of the named entry within nsi to not-ready.
	setNSIEntryNotReady := func(nsi *dpuservicev1.NodeServiceInterfaces, entryName string) {
		for i := range nsi.Status.InterfaceStatuses {
			if nsi.Status.InterfaceStatuses[i].Name == entryName {
				nsi.Status.InterfaceStatuses[i].Conditions[0].Status = metav1.ConditionFalse
				nsi.Status.InterfaceStatuses[i].Conditions[0].Message = "underlying OVS error for testing"
				return
			}
		}
	}

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		ovsMock = ovsutils.NewMockAPI(mockCtrl)

		siControl = getTestVFServiceInterface(
			"test-service-interface-control", "default", testNodeName, 1,
			map[string]string{"svc.dpu.nvidia.com/interface": "control"}, nil,
		)
		setReady(siControl, true)
		registerOVSLookup(siControl, controlIface, ptr.To(controlOfport))

		subjectLabels = map[string]string{"svc.dpu.nvidia.com/interface": "subject"}

		sc = &dpuservicev1.ServiceChain{
			Spec: dpuservicev1.ServiceChainSpec{
				Node: &testNodeName,
				Switches: []dpuservicev1.Switch{
					{
						Ports: []dpuservicev1.Port{
							{ServiceInterface: dpuservicev1.ServiceIfc{MatchLabels: siControl.Labels}},
							{ServiceInterface: dpuservicev1.ServiceIfc{MatchLabels: subjectLabels}},
						},
					},
				},
			},
		}
		sc.Namespace = siControl.Namespace
		sc.Name = "test-service-chain-ports"
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	It("rejects multiple SFC NodeServiceInterfaces objects for the same node", func() {
		first := newSFCNodeServiceInterfaces()
		second := newSFCNodeServiceInterfaces()
		second.Name = "test-nsi-duplicate"

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme.Scheme).
			WithObjects(first, second).
			WithIndex(&dpuservicev1.NodeServiceInterfaces{}, utils.NSINodeFieldKey, utils.NSINodeIndexFunc).
			WithIndex(&dpuservicev1.NodeServiceInterfaces{}, utils.NSITypeFieldKey, utils.NSITypeIndexFunc).
			Build()
		scr := &ServiceChainReconciler{Client: fakeClient, NodeName: testNodeName}

		nsi, err := scr.getSFCNodeServiceInterfaces(ctx)
		Expect(err).To(MatchError(ContainSubstring("multiple SFC NodeServiceInterfaces objects")))
		Expect(nsi).To(BeNil())
	})

	It("loads the node's SFC NSI shard from the central namespace", func() {
		expected := newSFCNodeServiceInterfaces()
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme.Scheme).
			WithObjects(expected).
			WithIndex(&dpuservicev1.NodeServiceInterfaces{}, utils.NSINodeFieldKey, utils.NSINodeIndexFunc).
			WithIndex(&dpuservicev1.NodeServiceInterfaces{}, utils.NSITypeFieldKey, utils.NSITypeIndexFunc).
			Build()
		scr := &ServiceChainReconciler{Client: fakeClient, NodeName: testNodeName}

		nsi, err := scr.getSFCNodeServiceInterfaces(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(nsi).To(Equal(expected))
	})

	It("enqueues only this node's ServiceChains whose ports select a changed NSI entry", func() {
		otherNode := "other-node"
		matchA := map[string]string{"svc.dpu.nvidia.com/interface": "a"}
		matchB := map[string]string{"svc.dpu.nvidia.com/interface": "b"}
		chainWith := func(name, ns string, node *string, sel map[string]string) *dpuservicev1.ServiceChain {
			return &dpuservicev1.ServiceChain{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
				Spec: dpuservicev1.ServiceChainSpec{
					Node: node,
					Switches: []dpuservicev1.Switch{{Ports: []dpuservicev1.Port{{
						ServiceInterface: dpuservicev1.ServiceIfc{MatchLabels: sel},
					}}}},
				},
			}
		}
		// first/second match an entry in their own namespace; nonMatching is on this node but selects
		// no entry; otherNodeChain matches by labels but lives on another node.
		first := chainWith("first", "tenant-a", &testNodeName, matchA)
		second := chainWith("second", "tenant-b", &testNodeName, matchB)
		nonMatching := chainWith("non-matching", "tenant-a", &testNodeName, map[string]string{"svc.dpu.nvidia.com/interface": "z"})
		otherNodeChain := chainWith("other", "tenant-a", &otherNode, matchA)
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme.Scheme).
			WithObjects(first, second, nonMatching, otherNodeChain).
			WithIndex(&dpuservicev1.ServiceChain{}, serviceChainNodeNameKey, func(o client.Object) []string {
				sc := o.(*dpuservicev1.ServiceChain)
				if sc.Spec.Node == nil {
					return nil
				}
				return []string{*sc.Spec.Node}
			}).
			Build()
		scr := &ServiceChainReconciler{Client: fakeClient, NodeName: testNodeName}

		nsi := newSFCNodeServiceInterfaces(
			dpuservicev1.InterfaceEntry{Name: "tenant-a_entry-a", Labels: matchA, InterfaceType: dpuservicev1.InterfaceTypeVF, VF: &dpuservicev1.VF{PFID: 0, VFID: 3}},
			dpuservicev1.InterfaceEntry{Name: "tenant-b_entry-b", Labels: matchB, InterfaceType: dpuservicev1.InterfaceTypeVF, VF: &dpuservicev1.VF{PFID: 0, VFID: 4}},
		)

		reqs := scr.mapNSIToServiceChains(ctx, nsi)
		Expect(reqs).To(ConsistOf(
			ctrl.Request{NamespacedName: client.ObjectKeyFromObject(first)},
			ctrl.Request{NamespacedName: client.ObjectKeyFromObject(second)},
		))
	})

	It("adds a newly resolvable port alongside an existing one", func() {
		siSubject := getTestVFServiceInterface(
			"test-service-interface-subject", "default", testNodeName, 2, subjectLabels, nil,
		)
		setReady(siSubject, true)
		registerOVSLookup(siSubject, "test-iface-subject", ptr.To(20))

		fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(siControl, siSubject).Build()
		scr := &ServiceChainReconciler{Client: fakeClient, NodeName: testNodeName, OVS: ovsMock}

		ports, errs := scr.buildChainPorts(ctx, sc, nil)
		Expect(errs).To(BeEmpty())
		Expect(ports).To(Equal([][]string{{"10", "20"}}))
	})

	It("drops a port whose ServiceInterface was removed, without affecting other ports", func() {
		// No ServiceInterface matching subjectLabels is created, simulating removal.
		fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(siControl).Build()
		scr := &ServiceChainReconciler{Client: fakeClient, NodeName: testNodeName, OVS: ovsMock}

		ports, errs := scr.buildChainPorts(ctx, sc, nil)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0]).To(MatchError(ContainSubstring("no serviceInterface")))
		Expect(ports).To(Equal([][]string{{"10"}}))
	})

	It("drops a disconnected (not-ready) port, without affecting other ports", func() {
		siSubject := getTestVFServiceInterface(
			"test-service-interface-subject", "default", testNodeName, 2, subjectLabels, nil,
		)
		setReady(siSubject, false)

		fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(siControl, siSubject).Build()
		scr := &ServiceChainReconciler{Client: fakeClient, NodeName: testNodeName, OVS: ovsMock}

		ports, errs := scr.buildChainPorts(ctx, sc, nil)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0]).To(MatchError(ContainSubstring("is not ready")))
		Expect(ports).To(Equal([][]string{{"10"}}))
	})

	It("resolves an updated OVS ofport when the ServiceInterface's binding changes", func() {
		siSubject := getTestVFServiceInterface(
			"test-service-interface-subject", "default", testNodeName, 2, subjectLabels, nil,
		)
		setReady(siSubject, true)
		subjectOfport := ptr.To(20)
		registerOVSLookup(siSubject, "test-iface-subject", subjectOfport)

		fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(siControl, siSubject).Build()
		scr := &ServiceChainReconciler{Client: fakeClient, NodeName: testNodeName, OVS: ovsMock}

		ports, errs := scr.buildChainPorts(ctx, sc, nil)
		Expect(errs).To(BeEmpty())
		Expect(ports).To(Equal([][]string{{"10", "20"}}))

		By("rebinding the ServiceInterface to a different OVS port")
		*subjectOfport = 30

		ports, errs = scr.buildChainPorts(ctx, sc, nil)
		Expect(errs).To(BeEmpty())
		Expect(ports).To(Equal([][]string{{"10", "30"}}), "buildChainPorts must not cache stale resolutions")
	})

	It("keeps ports grouped per switch when the chain has multiple switches", func() {
		siSubject := getTestVFServiceInterface(
			"test-service-interface-subject", "default", testNodeName, 2, subjectLabels, nil,
		)
		setReady(siSubject, true)
		registerOVSLookup(siSubject, "test-iface-subject", ptr.To(20))

		sc.Spec.Switches = []dpuservicev1.Switch{
			{Ports: []dpuservicev1.Port{{ServiceInterface: dpuservicev1.ServiceIfc{MatchLabels: siControl.Labels}}}},
			{Ports: []dpuservicev1.Port{{ServiceInterface: dpuservicev1.ServiceIfc{MatchLabels: subjectLabels}}}},
		}

		fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(siControl, siSubject).Build()
		scr := &ServiceChainReconciler{Client: fakeClient, NodeName: testNodeName, OVS: ovsMock}

		ports, errs := scr.buildChainPorts(ctx, sc, nil)
		Expect(errs).To(BeEmpty())
		Expect(ports).To(Equal([][]string{{"10"}, {"20"}}), "each switch must keep its own ports slice, not a single flattened list")
	})

	It("errors without resolving a port when multiple ServiceInterfaces match the same labels", func() {
		dupA := getTestVFServiceInterface("test-service-interface-subject-a", "default", testNodeName, 2, subjectLabels, nil)
		dupB := getTestVFServiceInterface("test-service-interface-subject-b", "default", testNodeName, 3, subjectLabels, nil)
		setReady(dupA, true)
		setReady(dupB, true)

		fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(siControl, dupA, dupB).Build()
		scr := &ServiceChainReconciler{Client: fakeClient, NodeName: testNodeName, OVS: ovsMock}

		ports, errs := scr.buildChainPorts(ctx, sc, nil)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0]).To(MatchError(ContainSubstring("expected only one serviceInterface")))
		Expect(ports).To(Equal([][]string{{"10"}}))
	})

	It("drops a port whose ServiceInterface is ready but has no matching OVS interface yet", func() {
		siSubject := getTestVFServiceInterface(
			"test-service-interface-subject", "default", testNodeName, 2, subjectLabels, nil,
		)
		setReady(siSubject, true)
		registerOVSLookupNotFound(siSubject)

		fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(siControl, siSubject).Build()
		scr := &ServiceChainReconciler{Client: fakeClient, NodeName: testNodeName, OVS: ovsMock}

		ports, errs := scr.buildChainPorts(ctx, sc, nil)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0]).To(MatchError(ContainSubstring("failed to find matching interface")))
		Expect(ports).To(Equal([][]string{{"10"}}))
	})

	It("resolves a Service-type port via its backing pod", func() {
		const serviceID = "test-service-id"
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod",
				Namespace: "default",
				Labels:    map[string]string{dpuservicev1.DPFServiceIDLabelKey: serviceID},
			},
			Spec: corev1.PodSpec{NodeName: testNodeName},
		}

		siSubject := &dpuservicev1.ServiceInterface{
			ObjectMeta: metav1.ObjectMeta{Name: "test-service-interface-subject", Namespace: "default", Labels: subjectLabels},
			Spec: dpuservicev1.ServiceInterfaceSpec{
				InterfaceType: dpuservicev1.InterfaceTypeService,
				Node:          &testNodeName,
				Service:       &dpuservicev1.ServiceDef{ServiceID: serviceID, Network: "test-net", InterfaceName: "eth1"},
			},
		}
		setReady(siSubject, true)

		condition := pod.Namespace + "/" + pod.Name + "/" + siSubject.Spec.Service.InterfaceName
		ofport := 42
		ovsMock.EXPECT().WhereAll(conditionMatcher{condition}, gomock.Any()).DoAndReturn(
			func(model interface{}, conds ...interface{}) ovsclient.ConditionalAPI {
				mockConditional := ovsutils.NewMockConditionalAPI(mockCtrl)
				mockConditional.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, result interface{}) error {
						*(result.(*[]ovsmodel.Interface)) = []ovsmodel.Interface{{
							Name:        "test-iface-service",
							Ofport:      &ofport,
							ExternalIDs: map[string]string{ovsutils.DPFIDKey: condition},
						}}
						return nil
					},
				)
				return mockConditional
			},
		)
		ovsMock.EXPECT().IsIfaceInBr(gomock.Any(), SFCBridge, "test-iface-service").Return(true, nil)

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme.Scheme).
			WithIndex(&corev1.Pod{}, podNodeNameKey, func(o client.Object) []string {
				return []string{o.(*corev1.Pod).Spec.NodeName}
			}).
			WithObjects(siControl, siSubject, pod).
			Build()
		scr := &ServiceChainReconciler{Client: fakeClient, NodeName: testNodeName, OVS: ovsMock}

		ports, errs := scr.buildChainPorts(ctx, sc, nil)
		Expect(errs).To(BeEmpty())
		Expect(ports).To(Equal([][]string{{"10", "42"}}))
	})

	It("resolves a port whose interface exists only as an NSI entry", func() {
		entry := newNSIEntry(subjectLabels, 2)
		nsi := newSFCNodeServiceInterfaces(entry)
		registerNSIOVSLookup(entry, "test-iface-subject", ptr.To(20))

		fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(siControl).Build()
		scr := &ServiceChainReconciler{Client: fakeClient, NodeName: testNodeName, OVS: ovsMock}

		ports, errs := scr.buildChainPorts(ctx, sc, nsi)
		Expect(errs).To(BeEmpty())
		Expect(ports).To(Equal([][]string{{"10", "20"}}))
	})

	It("ignores matching NSI entries owned by another tenant namespace", func() {
		entry := newNSIEntry(subjectLabels, 2)
		otherEntry := newNSIEntry(subjectLabels, 3)
		otherEntry.Name = "other_nsi-entry-subject"
		nsi := newSFCNodeServiceInterfaces(entry, otherEntry)
		registerNSIOVSLookup(entry, "test-iface-subject", ptr.To(20))

		fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(siControl).Build()
		scr := &ServiceChainReconciler{Client: fakeClient, NodeName: testNodeName, OVS: ovsMock}

		ports, errs := scr.buildChainPorts(ctx, sc, nsi)
		Expect(errs).To(BeEmpty())
		Expect(ports).To(Equal([][]string{{"10", "20"}}))
	})

	It("falls back to a still-active legacy interface while its NSI entry is terminating", func() {
		entry := newNSIEntry(subjectLabels, 2)
		entry.Terminating = true
		nsi := newSFCNodeServiceInterfaces(entry)
		legacySubject := getTestVFServiceInterface(
			"test-service-interface-subject", "default", testNodeName, 2, subjectLabels, nil,
		)
		setReady(legacySubject, true)
		registerOVSLookup(legacySubject, "test-iface-subject", ptr.To(20))

		fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(siControl, legacySubject).Build()
		scr := &ServiceChainReconciler{Client: fakeClient, NodeName: testNodeName, OVS: ovsMock}

		ports, errs := scr.buildChainPorts(ctx, sc, nsi)
		Expect(errs).To(BeEmpty())
		Expect(ports).To(Equal([][]string{{"10", "20"}}), "a terminating NSI entry must not suppress a still-active legacy ServiceInterface")
	})

	It("prefers the NSI entry over a legacy ServiceInterface matching the same labels", func() {
		siSubject := getTestVFServiceInterface(
			"test-service-interface-subject", "default", testNodeName, 2, subjectLabels, nil,
		)
		setReady(siSubject, true)

		entry := newNSIEntry(subjectLabels, 3)
		nsi := newSFCNodeServiceInterfaces(entry)
		registerNSIOVSLookup(entry, "test-iface-subject-nsi", ptr.To(30))

		fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(siControl, siSubject).Build()
		scr := &ServiceChainReconciler{Client: fakeClient, NodeName: testNodeName, OVS: ovsMock}

		ports, errs := scr.buildChainPorts(ctx, sc, nsi)
		Expect(errs).To(BeEmpty())
		Expect(ports).To(Equal([][]string{{"10", "30"}}), "the NSI entry must win, the legacy ServiceInterface must be ignored")
	})

	It("drops a not-ready NSI entry, without affecting other ports", func() {
		entry := newNSIEntry(subjectLabels, 2)
		nsi := newSFCNodeServiceInterfaces(entry)
		setNSIEntryNotReady(nsi, entry.Name)

		fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(siControl).Build()
		scr := &ServiceChainReconciler{Client: fakeClient, NodeName: testNodeName, OVS: ovsMock}

		ports, errs := scr.buildChainPorts(ctx, sc, nsi)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0]).To(MatchError(ContainSubstring("is not ready")))
		Expect(errs[0]).To(MatchError(ContainSubstring("underlying OVS error for testing")))
		Expect(ports).To(Equal([][]string{{"10"}}))
	})

	It("drops a Terminating NSI entry, without affecting other ports", func() {
		entry := newNSIEntry(subjectLabels, 2)
		entry.Terminating = true
		nsi := newSFCNodeServiceInterfaces(entry)

		fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(siControl).Build()
		scr := &ServiceChainReconciler{Client: fakeClient, NodeName: testNodeName, OVS: ovsMock}

		ports, errs := scr.buildChainPorts(ctx, sc, nsi)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0]).To(MatchError(ContainSubstring("no serviceInterface")))
		Expect(ports).To(Equal([][]string{{"10"}}))
	})

	It("resolves a service-type NSI entry via its backing pod", func() {
		const serviceID = "test-service-id-nsi"
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod-nsi",
				Namespace: "default",
				Labels:    map[string]string{dpuservicev1.DPFServiceIDLabelKey: serviceID},
			},
			Spec: corev1.PodSpec{NodeName: testNodeName},
		}

		entry := dpuservicev1.InterfaceEntry{
			Name:          "default_nsi-entry-subject",
			Labels:        subjectLabels,
			InterfaceType: dpuservicev1.InterfaceTypeService,
			Service:       &dpuservicev1.ServiceDef{ServiceID: serviceID, Network: "test-net", InterfaceName: "eth1"},
		}
		nsi := newSFCNodeServiceInterfaces(entry)

		condition := pod.Namespace + "/" + pod.Name + "/" + entry.Service.InterfaceName
		ofport := 42
		ovsMock.EXPECT().WhereAll(conditionMatcher{condition}, gomock.Any()).DoAndReturn(
			func(model interface{}, conds ...interface{}) ovsclient.ConditionalAPI {
				mockConditional := ovsutils.NewMockConditionalAPI(mockCtrl)
				mockConditional.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, result interface{}) error {
						*(result.(*[]ovsmodel.Interface)) = []ovsmodel.Interface{{
							Name:        "test-iface-service-nsi",
							Ofport:      &ofport,
							ExternalIDs: map[string]string{ovsutils.DPFIDKey: condition},
						}}
						return nil
					},
				)
				return mockConditional
			},
		)
		ovsMock.EXPECT().IsIfaceInBr(gomock.Any(), SFCBridge, "test-iface-service-nsi").Return(true, nil)

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme.Scheme).
			WithIndex(&corev1.Pod{}, podNodeNameKey, func(o client.Object) []string {
				return []string{o.(*corev1.Pod).Spec.NodeName}
			}).
			WithObjects(siControl, pod).
			Build()
		scr := &ServiceChainReconciler{Client: fakeClient, NodeName: testNodeName, OVS: ovsMock}

		ports, errs := scr.buildChainPorts(ctx, sc, nsi)
		Expect(errs).To(BeEmpty())
		Expect(ports).To(Equal([][]string{{"10", "42"}}))
	})
})

//nolint:goconst
var _ = Describe("service chain controller", func() {
	var (
		mockCtrl       *gomock.Controller
		cleanupObjects []client.Object
		scr            *ServiceChainReconciler
		ofb            *MockBridge
		ovsMock        *ovsutils.MockAPI
		fakeExec       *kexecTesting.FakeExec
		wg             sync.WaitGroup
		testNS         *corev1.Namespace
		node           *corev1.Node
		testNode       = "test-node"
		nn             types.NamespacedName
		testCtx        context.Context
		testCancelFunc context.CancelFunc
		scMock         *MockServiceChainAPI
		openflowMock   *openflow.MockOpenFlowAPI
	)

	BeforeEach(func() {
		// These specs resolve ports from legacy ServiceInterface objects.
		featuregatetesting.SetFeatureGateDuringTest(GinkgoT(), features.MutableGates, features.NSIPathForSFC, false)

		testCtx, testCancelFunc = context.WithCancel(ctx)
		wg = sync.WaitGroup{}
		mockCtrl = gomock.NewController(GinkgoT())
		ofb = NewMockBridge(mockCtrl)
		ovsMock = ovsutils.NewMockAPI(mockCtrl)
		fakeExec = &kexecTesting.FakeExec{}
		scMock = NewMockServiceChainAPI(mockCtrl)
		openflowMock = openflow.NewMockOpenFlowAPI(mockCtrl)

		scr = &ServiceChainReconciler{
			Client:     testClient,
			NodeName:   testNodeName,
			BridgeName: BridgeSFC,
			OFBridge:   ofb,
			OVS:        ovsMock,
			Exec:       fakeExec,
			SC:         scMock,
			OPFlow:     openflowMock,
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
				ObservedGeneration: siList.Items[0].Generation,
			},
			{
				Type:               string(dpuservicev1.ServiceChainReconciled),
				Status:             metav1.ConditionFalse,
				Reason:             "Error",
				Message:            "dummy error message",
				LastTransitionTime: metav1.Now(),
				ObservedGeneration: siList.Items[0].Generation,
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

// onlyNotFound reports whether err is nil or only "not found" errors, unwrapping Reconcile's aggregate.
func onlyNotFound(err error) bool {
	if err == nil {
		return true
	}
	var agg kerrors.Aggregate
	if errors.As(err, &agg) {
		for _, e := range agg.Errors() {
			if !onlyNotFound(e) {
				return false
			}
		}
		return true
	}
	return apierrors.IsNotFound(err)
}

var _ = Describe("service chain controller flow application", func() {
	var (
		mockCtrl       *gomock.Controller
		cleanupObjects []client.Object
		scr            *ServiceChainReconciler
		ofb            *MockBridge
		ovsMock        *ovsutils.MockAPI
		scMock         *MockServiceChainAPI
		ctx            = context.Background()
		testNS         *corev1.Namespace
		node           *corev1.Node
		testNode       = "test-node-flow-apply"
		nn             types.NamespacedName
		si             *dpuservicev1.ServiceInterface
	)

	BeforeEach(func() {
		// These specs resolve ports from legacy ServiceInterface objects.
		featuregatetesting.SetFeatureGateDuringTest(GinkgoT(), features.MutableGates, features.NSIPathForSFC, false)

		mockCtrl = gomock.NewController(GinkgoT())
		ofb = NewMockBridge(mockCtrl)
		ovsMock = ovsutils.NewMockAPI(mockCtrl)
		scMock = NewMockServiceChainAPI(mockCtrl)

		scr = &ServiceChainReconciler{
			Client:     testClient,
			NodeName:   testNode,
			BridgeName: BridgeSFC,
			OFBridge:   ofb,
			OVS:        ovsMock,
			SC:         scMock,
		}

		By("creating namespace")
		testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "test-ns-flow-apply-"}}
		Expect(testClient.Create(ctx, testNS)).Should(Succeed())

		By("creating node")
		node = getTestNode(testNode)
		Expect(testClient.Create(ctx, node)).To(Succeed())
		cleanupObjects = append(cleanupObjects, node)

		By("creating a ready service interface")
		si = getTestVFServiceInterface(
			"test-service-interface",
			testNS.Name,
			testNode,
			1,
			map[string]string{"svc.dpu.nvidia.com/interface": "p0vf1"},
			nil,
		)
		Expect(testClient.Create(ctx, si)).To(Succeed())
		cleanupObjects = append(cleanupObjects, si)

		si.Status.Conditions = []metav1.Condition{
			{
				Type:               string(conditions.TypeReady),
				Status:             metav1.ConditionTrue,
				Reason:             "Success",
				Message:            "",
				LastTransitionTime: metav1.Now(),
				ObservedGeneration: si.Generation,
			},
		}
		Expect(testClient.Status().Update(ctx, si)).To(Succeed())

		nn = types.NamespacedName{
			Namespace: testNS.Name,
			Name:      "test-service-chain",
		}

		By("creating service chain")
		sc := &dpuservicev1.ServiceChain{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nn.Name,
				Namespace: nn.Namespace,
			},
			Spec: dpuservicev1.ServiceChainSpec{
				Node: &testNode,
				Switches: []dpuservicev1.Switch{
					{
						Ports: []dpuservicev1.Port{
							{
								ServiceInterface: dpuservicev1.ServiceIfc{
									MatchLabels: si.Labels,
								},
							},
						},
					},
				},
			},
		}
		Expect(testClient.Create(ctx, sc)).To(Succeed())
		cleanupObjects = append(cleanupObjects, sc)
	})

	AfterEach(func() {
		// Strip the finalizer directly: these tests call Reconcile without a manager, so reconcileDelete never runs.
		sc := &dpuservicev1.ServiceChain{}
		if testClient.Get(ctx, nn, sc) == nil {
			sc.Finalizers = nil
			Expect(testClient.Update(ctx, sc)).To(Succeed())
		}

		Expect(testutils.CleanupAndWait(ctx, testClient, cleanupObjects...)).To(Succeed())
		Expect(testClient.Delete(ctx, testNS)).To(Succeed())
		mockCtrl.Finish()
	})

	// expectPortLookup sets up OVS mock expectations for resolving the test ServiceInterface's ofport.
	expectPortLookup := func() {
		condition := si.Namespace + "/" + si.Name
		ofport := 7
		iface := ovsmodel.Interface{
			Name:        "test-iface",
			Ofport:      &ofport,
			ExternalIDs: map[string]string{ovsutils.DPFIDKey: condition},
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
	}

	It("applies flows on every reconcile, even across repeated reconciles with nothing changed", func() {
		// 1st reconcile: only adds the finalizer, ports aren't resolved yet.
		_, err := scr.Reconcile(ctx, ctrl.Request{NamespacedName: nn})
		Expect(err).ToNot(HaveOccurred())

		// Every subsequent reconcile resolves ports and re-applies flows fire-and-forget, even if unchanged.
		for range 3 {
			expectPortLookup()
			scMock.EXPECT().GenerateAndApplyOpenFlows(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
			_, err = scr.Reconcile(ctx, ctrl.Request{NamespacedName: nn})
			Expect(err).ToNot(HaveOccurred())
		}
	})

	It("deletes flows when the ServiceChain is deleted", func() {
		_, err := scr.Reconcile(ctx, ctrl.Request{NamespacedName: nn})
		Expect(err).ToNot(HaveOccurred())

		expectPortLookup()
		scMock.EXPECT().GenerateAndApplyOpenFlows(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		_, err = scr.Reconcile(ctx, ctrl.Request{NamespacedName: nn})
		Expect(err).ToNot(HaveOccurred())

		By("deleting the ServiceChain")
		existing := &dpuservicev1.ServiceChain{}
		Expect(testClient.Get(ctx, nn, existing)).To(Succeed())
		Expect(testClient.Delete(ctx, existing)).To(Succeed())

		// Removing the finalizer here can race the reconciler's own status patch into a tolerated NotFound.
		ofb.EXPECT().DeleteFlowsByCookie(hash(nn.String()), gomock.Any()).Return(nil).Times(1)
		_, err = scr.Reconcile(ctx, ctrl.Request{NamespacedName: nn})
		Expect(onlyNotFound(err)).To(BeTrue(), "unexpected error: %v", err)

		Expect(apierrors.IsNotFound(testClient.Get(ctx, nn, &dpuservicev1.ServiceChain{}))).To(BeTrue())
	})

	It("deletes flows when reconciling a ServiceChain that is already gone", func() {
		_, err := scr.Reconcile(ctx, ctrl.Request{NamespacedName: nn})
		Expect(err).ToNot(HaveOccurred())

		expectPortLookup()
		scMock.EXPECT().GenerateAndApplyOpenFlows(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		_, err = scr.Reconcile(ctx, ctrl.Request{NamespacedName: nn})
		Expect(err).ToNot(HaveOccurred())

		By("removing the ServiceChain without going through reconcileDelete")
		existing := &dpuservicev1.ServiceChain{}
		Expect(testClient.Get(ctx, nn, existing)).To(Succeed())
		existing.Finalizers = nil
		Expect(testClient.Update(ctx, existing)).To(Succeed())
		Expect(testClient.Delete(ctx, existing)).To(Succeed())

		ofb.EXPECT().DeleteFlowsByCookie(hash(nn.String()), gomock.Any()).Return(nil).Times(1)
		_, err = scr.Reconcile(ctx, ctrl.Request{NamespacedName: nn})
		Expect(err).ToNot(HaveOccurred())
	})

	It("re-applies flows when the spec generation changes even if the resolved ports end up identical", func() {
		_, err := scr.Reconcile(ctx, ctrl.Request{NamespacedName: nn})
		Expect(err).ToNot(HaveOccurred())

		expectPortLookup()
		scMock.EXPECT().GenerateAndApplyOpenFlows(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		_, err = scr.Reconcile(ctx, ctrl.Request{NamespacedName: nn})
		Expect(err).ToNot(HaveOccurred())

		By("bumping the ServiceChain's generation without changing the resolved ports")
		existing := &dpuservicev1.ServiceChain{}
		Expect(testClient.Get(ctx, nn, existing)).To(Succeed())
		existing.Spec.Switches[0].ServiceMTU = ptr.To(9000)
		Expect(testClient.Update(ctx, existing)).To(Succeed())

		// On generation change, flows are force-deleted then unconditionally re-applied once ports resolve.
		ofb.EXPECT().DeleteFlowsByCookie(hash(nn.String()), gomock.Any()).Return(nil).Times(1)
		expectPortLookup()
		scMock.EXPECT().GenerateAndApplyOpenFlows(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		_, err = scr.Reconcile(ctx, ctrl.Request{NamespacedName: nn})
		Expect(err).ToNot(HaveOccurred())
	})
})

// nsiFieldSelectorClient applies NodeServiceInterfaces field selectors client-side, since the API server envtest runs cannot select on CRD fields.
type nsiFieldSelectorClient struct {
	client.Client
}

func (c nsiFieldSelectorClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	nsiList, ok := list.(*dpuservicev1.NodeServiceInterfacesList)
	if !ok {
		return c.Client.List(ctx, list, opts...)
	}

	listOpts := &client.ListOptions{}
	listOpts.ApplyOptions(opts)
	selector := listOpts.FieldSelector
	listOpts.FieldSelector = nil
	if err := c.Client.List(ctx, nsiList, listOpts); err != nil {
		return err
	}
	if selector == nil {
		return nil
	}

	matching := nsiList.Items[:0]
	for _, item := range nsiList.Items {
		if selector.Matches(fields.Set{
			utils.NSINodeFieldKey: utils.NSINodeIndexFunc(&item)[0],
			utils.NSITypeFieldKey: utils.NSITypeIndexFunc(&item)[0],
		}) {
			matching = append(matching, item)
		}
	}
	nsiList.Items = matching
	return nil
}

// NSI-path counterpart of "service chain controller flow application", with ports resolved from this node's NSI shard.
var _ = Describe("service chain controller flow application on the NSI path", func() {
	var (
		mockCtrl       *gomock.Controller
		cleanupObjects []client.Object
		scr            *ServiceChainReconciler
		ofb            *MockBridge
		ovsMock        *ovsutils.MockAPI
		scMock         *MockServiceChainAPI
		ctx            = context.Background()
		testNS         *corev1.Namespace
		node           *corev1.Node
		testNode       = "test-node-flow-apply-nsi"
		nn             types.NamespacedName
		entryName      string
	)

	const (
		nsiIfaceName   = "test-iface-nsi"
		nsiIfaceOfport = 7
	)
	entryLabels := map[string]string{"svc.dpu.nvidia.com/interface": "p0vf1"}

	BeforeEach(func() {
		featuregatetesting.SetFeatureGateDuringTest(GinkgoT(), features.MutableGates, features.NSIPathForSFC, true)

		cleanupObjects = nil
		mockCtrl = gomock.NewController(GinkgoT())
		ofb = NewMockBridge(mockCtrl)
		ovsMock = ovsutils.NewMockAPI(mockCtrl)
		scMock = NewMockServiceChainAPI(mockCtrl)

		scr = &ServiceChainReconciler{
			Client:     nsiFieldSelectorClient{testClient},
			NodeName:   testNode,
			BridgeName: BridgeSFC,
			OFBridge:   ofb,
			OVS:        ovsMock,
			SC:         scMock,
		}

		By("creating namespace")
		testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "test-ns-flow-apply-nsi-"}}
		Expect(testClient.Create(ctx, testNS)).Should(Succeed())

		By("creating node")
		node = getTestNode(testNode)
		Expect(testClient.Create(ctx, node)).To(Succeed())
		cleanupObjects = append(cleanupObjects, node)

		By("creating this node's SFC NodeServiceInterfaces shard with one ready VF entry")
		entryName = testNS.Name + "_flow-apply-entry"
		nsi := &dpuservicev1.NodeServiceInterfaces{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "test-nsi-flow-apply-", Namespace: utils.NSIObjectsNamespace},
			Spec: dpuservicev1.NodeServiceInterfacesSpec{
				Node: testNode,
				Type: dpuservicev1.NSITypeSFC,
				Interfaces: []dpuservicev1.InterfaceEntry{{
					Name:          entryName,
					Labels:        entryLabels,
					InterfaceType: dpuservicev1.InterfaceTypeVF,
					VF:            &dpuservicev1.VF{PFID: 0, VFID: 1},
				}},
			},
		}
		Expect(testClient.Create(ctx, nsi)).To(Succeed())
		cleanupObjects = append(cleanupObjects, nsi)

		nsi.Status.InterfaceStatuses = []dpuservicev1.InterfaceEntryStatus{{
			Name: entryName,
			Conditions: []metav1.Condition{
				{
					Type:               string(conditions.TypeReady),
					Status:             metav1.ConditionTrue,
					Reason:             "Success",
					Message:            "",
					LastTransitionTime: metav1.Now(),
					ObservedGeneration: nsi.Generation,
				},
			},
		}}
		Expect(testClient.Status().Update(ctx, nsi)).To(Succeed())

		nn = types.NamespacedName{
			Namespace: testNS.Name,
			Name:      "test-service-chain-nsi",
		}

		By("creating service chain")
		sc := &dpuservicev1.ServiceChain{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nn.Name,
				Namespace: nn.Namespace,
			},
			Spec: dpuservicev1.ServiceChainSpec{
				Node: &testNode,
				Switches: []dpuservicev1.Switch{
					{
						Ports: []dpuservicev1.Port{
							{
								ServiceInterface: dpuservicev1.ServiceIfc{
									MatchLabels: entryLabels,
								},
							},
						},
					},
				},
			},
		}
		Expect(testClient.Create(ctx, sc)).To(Succeed())
		cleanupObjects = append(cleanupObjects, sc)
	})

	AfterEach(func() {
		// Strip the finalizer directly: these tests call Reconcile without a manager, so reconcileDelete never runs.
		sc := &dpuservicev1.ServiceChain{}
		if testClient.Get(ctx, nn, sc) == nil {
			sc.Finalizers = nil
			Expect(testClient.Update(ctx, sc)).To(Succeed())
		}

		Expect(testutils.CleanupAndWait(ctx, testClient, cleanupObjects...)).To(Succeed())
		Expect(testClient.Delete(ctx, testNS)).To(Succeed())
		mockCtrl.Finish()
	})

	// OVS keys the NSI entry by entry name, not by a namespace/name ServiceInterface reference.
	expectPortLookupWithOfport := func(ifaceName string, ofport int) {
		iface := ovsmodel.Interface{
			Name:        ifaceName,
			Ofport:      &ofport,
			ExternalIDs: map[string]string{ovsutils.DPFIDKey: entryName},
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
		ovsMock.EXPECT().IsIfaceInBr(gomock.Any(), SFCBridge, ifaceName).Return(true, nil)
	}

	expectPortLookup := func() {
		expectPortLookupWithOfport(nsiIfaceName, nsiIfaceOfport)
	}

	expectApply := func(ofports ...string) {
		scMock.EXPECT().GenerateAndApplyOpenFlows(gomock.Any(), [][]string{ofports}, hash(nn.String())).Return(nil).Times(1)
	}

	It("applies NSI-resolved flows on every reconcile, even across repeated reconciles with nothing changed", func() {
		// 1st reconcile: only adds the finalizer, ports aren't resolved yet.
		_, err := scr.Reconcile(ctx, ctrl.Request{NamespacedName: nn})
		Expect(err).ToNot(HaveOccurred())

		for range 3 {
			expectPortLookup()
			expectApply(fmt.Sprint(nsiIfaceOfport))
			_, err = scr.Reconcile(ctx, ctrl.Request{NamespacedName: nn})
			Expect(err).ToNot(HaveOccurred())
		}
	})

	It("re-applies flows with the new ofport when the NSI entry rebinds in OVS", func() {
		_, err := scr.Reconcile(ctx, ctrl.Request{NamespacedName: nn})
		Expect(err).ToNot(HaveOccurred())

		expectPortLookup()
		expectApply(fmt.Sprint(nsiIfaceOfport))
		_, err = scr.Reconcile(ctx, ctrl.Request{NamespacedName: nn})
		Expect(err).ToNot(HaveOccurred())

		// The entry is unchanged, so only the next reconcile's re-resolve picks the new binding up.
		By("rebinding the entry to a different OVS interface")
		expectPortLookupWithOfport("test-iface-nsi-rebound", 21)
		expectApply("21")
		_, err = scr.Reconcile(ctx, ctrl.Request{NamespacedName: nn})
		Expect(err).ToNot(HaveOccurred())
	})

	It("reports an error and applies no port when the entry's NSI shard is gone", func() {
		_, err := scr.Reconcile(ctx, ctrl.Request{NamespacedName: nn})
		Expect(err).ToNot(HaveOccurred())

		By("deleting this node's NSI shard")
		nsiList := &dpuservicev1.NodeServiceInterfacesList{}
		Expect(testClient.List(ctx, nsiList, client.InNamespace(utils.NSIObjectsNamespace))).To(Succeed())
		for i := range nsiList.Items {
			if nsiList.Items[i].Spec.Node == testNode {
				Expect(testClient.Delete(ctx, &nsiList.Items[i])).To(Succeed())
			}
		}

		// Flows still apply, so the chain converges to no ports instead of keeping stale ones.
		scMock.EXPECT().GenerateAndApplyOpenFlows(gomock.Any(), [][]string{{}}, hash(nn.String())).Return(nil).Times(1)
		_, err = scr.Reconcile(ctx, ctrl.Request{NamespacedName: nn})
		Expect(err).To(MatchError(ContainSubstring("no serviceInterface")))
	})

	It("deletes flows when the ServiceChain is deleted", func() {
		_, err := scr.Reconcile(ctx, ctrl.Request{NamespacedName: nn})
		Expect(err).ToNot(HaveOccurred())

		expectPortLookup()
		expectApply(fmt.Sprint(nsiIfaceOfport))
		_, err = scr.Reconcile(ctx, ctrl.Request{NamespacedName: nn})
		Expect(err).ToNot(HaveOccurred())

		By("deleting the ServiceChain")
		existing := &dpuservicev1.ServiceChain{}
		Expect(testClient.Get(ctx, nn, existing)).To(Succeed())
		Expect(testClient.Delete(ctx, existing)).To(Succeed())

		// Removing the finalizer here can race the reconciler's own status patch into a tolerated NotFound.
		ofb.EXPECT().DeleteFlowsByCookie(hash(nn.String()), gomock.Any()).Return(nil).Times(1)
		_, err = scr.Reconcile(ctx, ctrl.Request{NamespacedName: nn})
		Expect(onlyNotFound(err)).To(BeTrue(), "unexpected error: %v", err)

		Expect(apierrors.IsNotFound(testClient.Get(ctx, nn, &dpuservicev1.ServiceChain{}))).To(BeTrue())
	})

	It("re-applies flows when the spec generation changes even if the resolved ports end up identical", func() {
		_, err := scr.Reconcile(ctx, ctrl.Request{NamespacedName: nn})
		Expect(err).ToNot(HaveOccurred())

		expectPortLookup()
		expectApply(fmt.Sprint(nsiIfaceOfport))
		_, err = scr.Reconcile(ctx, ctrl.Request{NamespacedName: nn})
		Expect(err).ToNot(HaveOccurred())

		By("bumping the ServiceChain's generation without changing the resolved ports")
		existing := &dpuservicev1.ServiceChain{}
		Expect(testClient.Get(ctx, nn, existing)).To(Succeed())
		existing.Spec.Switches[0].ServiceMTU = ptr.To(9000)
		Expect(testClient.Update(ctx, existing)).To(Succeed())

		// On generation change, flows are force-deleted then unconditionally re-applied once ports resolve.
		ofb.EXPECT().DeleteFlowsByCookie(hash(nn.String()), gomock.Any()).Return(nil).Times(1)
		expectPortLookup()
		expectApply(fmt.Sprint(nsiIfaceOfport))
		_, err = scr.Reconcile(ctx, ctrl.Request{NamespacedName: nn})
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = Describe("ServiceChainReconciler TriggerResync", func() {
	var (
		fakeClient client.Client
		scr        *ServiceChainReconciler
		ctx        = context.Background()
		testNS     = "test-ns-resync"
		testNode   = "test-node-resync"
		otherNode  = "test-node-resync-other"
	)

	BeforeEach(func() {
		fakeClient = fake.NewClientBuilder().
			WithScheme(scheme.Scheme).
			WithIndex(&dpuservicev1.ServiceChain{}, serviceChainNodeNameKey, func(o client.Object) []string {
				sc := o.(*dpuservicev1.ServiceChain)
				if sc.Spec.Node == nil {
					return nil
				}
				return []string{*sc.Spec.Node}
			}).
			WithIndex(&dpuservicev1.ServiceInterface{}, utils.ServiceInterfaceNodeFieldKey, utils.ServiceInterfaceNodeIndexFunc).
			WithIndex(&dpuservicev1.NodeServiceInterfaces{}, utils.NSINodeFieldKey, utils.NSINodeIndexFunc).
			WithIndex(&dpuservicev1.NodeServiceInterfaces{}, utils.NSITypeFieldKey, utils.NSITypeIndexFunc).
			Build()
		scr = &ServiceChainReconciler{
			Client:   fakeClient,
			NodeName: testNode,
			resyncCh: make(chan event.GenericEvent),
		}
	})

	AfterEach(func() {
		// Closing resyncCh lets any spawned forwarding goroutine exit instead of leaking into the next spec.
		close(scr.resyncCh)
	})

	newChain := func(name, node string) *dpuservicev1.ServiceChain {
		sc := &dpuservicev1.ServiceChain{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: testNS,
			},
			Spec: dpuservicev1.ServiceChainSpec{
				Node: &node,
				Switches: []dpuservicev1.Switch{{Ports: []dpuservicev1.Port{{
					ServiceInterface: dpuservicev1.ServiceIfc{MatchLabels: map[string]string{"k": "v"}},
				}}}},
			},
		}
		Expect(fakeClient.Create(ctx, sc)).To(Succeed())
		return sc
	}

	It("emits a GenericEvent only for ServiceChains on this node", func() {
		mine := newChain("chain-mine", testNode)
		newChain("chain-other-node", otherNode)

		ch := scr.resyncCh
		received := make(chan event.GenericEvent, 10)
		go func() {
			for e := range ch {
				received <- e
			}
		}()

		Expect(scr.TriggerResync(ctx)).To(Succeed())

		var got event.GenericEvent
		Eventually(received).WithTimeout(2 * time.Second).Should(Receive(&got))
		Expect(got.Object.(*dpuservicev1.ServiceChain).Name).To(Equal(mine.Name))

		Consistently(received).WithTimeout(200 * time.Millisecond).ShouldNot(Receive())
	})

	It("maps a service Pod only to chains using its ServiceInterface", func() {
		mine := newChain("chain-mine", testNode)
		unrelated := &dpuservicev1.ServiceChain{
			ObjectMeta: metav1.ObjectMeta{Name: "chain-unrelated", Namespace: testNS},
			Spec: dpuservicev1.ServiceChainSpec{
				Node: &testNode,
				Switches: []dpuservicev1.Switch{{Ports: []dpuservicev1.Port{{
					ServiceInterface: dpuservicev1.ServiceIfc{MatchLabels: map[string]string{"other": "labels"}},
				}}}},
			},
		}
		Expect(fakeClient.Create(ctx, unrelated)).To(Succeed())

		serviceID := "test-service"
		si := &dpuservicev1.ServiceInterface{
			ObjectMeta: metav1.ObjectMeta{Name: "service-interface", Namespace: testNS, Labels: map[string]string{"k": "v"}},
			Spec: dpuservicev1.ServiceInterfaceSpec{
				Node:          &testNode,
				InterfaceType: dpuservicev1.InterfaceTypeService,
				Service: &dpuservicev1.ServiceDef{
					ServiceID:     serviceID,
					Network:       "test-network",
					InterfaceName: "eth0",
				},
			},
		}
		Expect(fakeClient.Create(ctx, si)).To(Succeed())

		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
			Namespace: testNS,
			Labels:    map[string]string{dpuservicev1.DPFServiceIDLabelKey: serviceID},
		}}
		reqs := scr.serviceChainsForPod(ctx, pod)
		Expect(reqs).To(ConsistOf(reconcile.Request{NamespacedName: client.ObjectKeyFromObject(mine)}))
	})

	It("maps a service Pod to chains via NSI entries when the NSI path is enabled", func() {
		featuregatetesting.SetFeatureGateDuringTest(GinkgoT(), features.MutableGates, features.NSIPathForSFC, true)

		serviceID := "test-service"
		mine := newChain("chain-nsi", testNode)

		// No legacy ServiceInterface exists for this service; only an NSI entry backs it.
		nsi := &dpuservicev1.NodeServiceInterfaces{
			ObjectMeta: metav1.ObjectMeta{Name: "test-nsi", Namespace: utils.NSIObjectsNamespace},
			Spec: dpuservicev1.NodeServiceInterfacesSpec{
				Node: testNode,
				Type: dpuservicev1.NSITypeSFC,
				Interfaces: []dpuservicev1.InterfaceEntry{{
					Name:          testNS + "_nsi-entry",
					Labels:        map[string]string{"k": "v"},
					InterfaceType: dpuservicev1.InterfaceTypeService,
					Service:       &dpuservicev1.ServiceDef{ServiceID: serviceID, Network: "test-network", InterfaceName: "eth0"},
				}},
			},
		}
		Expect(fakeClient.Create(ctx, nsi)).To(Succeed())

		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
			Namespace: testNS,
			Labels:    map[string]string{dpuservicev1.DPFServiceIDLabelKey: serviceID},
		}}
		reqs := scr.serviceChainsForPod(ctx, pod)
		Expect(reqs).To(ConsistOf(reconcile.Request{NamespacedName: client.ObjectKeyFromObject(mine)}))
	})

	It("skips ServiceChains with no node set", func() {
		newChain("chain-empty-node", "")
		unset := &dpuservicev1.ServiceChain{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "chain-nil-node",
				Namespace: testNS,
			},
			Spec: dpuservicev1.ServiceChainSpec{
				Switches: []dpuservicev1.Switch{{Ports: []dpuservicev1.Port{{
					ServiceInterface: dpuservicev1.ServiceIfc{MatchLabels: map[string]string{"k": "v"}},
				}}}},
			},
		}
		Expect(fakeClient.Create(ctx, unset)).To(Succeed())

		ch := scr.resyncCh
		received := make(chan event.GenericEvent, 10)
		go func() {
			for e := range ch {
				received <- e
			}
		}()

		Expect(scr.TriggerResync(ctx)).To(Succeed())
		Consistently(received).WithTimeout(200 * time.Millisecond).ShouldNot(Receive())
	})

	It("returns the context error if canceled while blocked sending", func() {
		newChain("chain-blocked", testNode)

		sendCtx, cancelSend := context.WithCancel(ctx)
		defer cancelSend()
		errCh := make(chan error, 1)
		go func() {
			// No consumer on resyncCh: TriggerResync must block on the send.
			errCh <- scr.TriggerResync(sendCtx)
		}()

		Consistently(errCh).WithTimeout(200 * time.Millisecond).ShouldNot(Receive())

		cancelSend()
		var err error
		Eventually(errCh).WithTimeout(2 * time.Second).Should(Receive(&err))
		Expect(err).To(MatchError(context.Canceled))
	})

	It("returns immediately with an error if canceled before the call, without blocking on the send", func() {
		newChain("chain-precanceled", testNode)

		cancelCtx, cancel := context.WithCancel(ctx)
		cancel()

		err := scr.TriggerResync(cancelCtx)
		Expect(err).To(MatchError(context.Canceled))
	})

	It("does not block a second caller while a resync is in flight and coalesces it", func() {
		newChain("chain-inflight", testNode)

		// First caller: nothing drains resyncCh, so it blocks on the send and holds resyncActive.
		firstCtx, cancelFirst := context.WithCancel(ctx)
		defer cancelFirst()
		firstErr := make(chan error, 1)
		go func() {
			defer GinkgoRecover()
			firstErr <- scr.TriggerResync(firstCtx)
		}()

		Eventually(func() bool {
			scr.resyncMu.Lock()
			defer scr.resyncMu.Unlock()
			return scr.resyncActive
		}).WithTimeout(2 * time.Second).Should(BeTrue())

		// Second caller must return promptly instead of blocking behind the first, marking a pass pending.
		secondErr := make(chan error, 1)
		go func() {
			defer GinkgoRecover()
			secondErr <- scr.TriggerResync(ctx)
		}()
		Eventually(secondErr).WithTimeout(2 * time.Second).Should(Receive(BeNil()))

		scr.resyncMu.Lock()
		pending := scr.resyncPending
		scr.resyncMu.Unlock()
		Expect(pending).To(BeTrue(), "the coalesced caller should mark another pass pending")

		cancelFirst()
		Eventually(firstErr).WithTimeout(2 * time.Second).Should(Receive(MatchError(context.Canceled)))
	})

	It("retries a pending resync even if the running pass fails", func() {
		newChain("chain-retry", testNode)

		callCount := 0
		scr.Client = interceptor.NewClient(fakeClient.(client.WithWatch), interceptor.Funcs{
			List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*dpuservicev1.ServiceChainList); ok {
					callCount++
					if callCount == 1 {
						// Simulate a concurrent caller marking another pass pending during this run,
						// then fail it: the loop must retry rather than drop the pending request.
						scr.resyncMu.Lock()
						scr.resyncPending = true
						scr.resyncMu.Unlock()
						return errors.New("transient list error")
					}
				}
				return c.List(ctx, list, opts...)
			},
		})

		go func() {
			for range scr.resyncCh {
			}
		}()

		Expect(scr.TriggerResync(ctx)).To(Succeed())
		Expect(callCount).To(Equal(2), "the pending request must trigger a second pass after the first failed")
	})
})
