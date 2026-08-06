/*
Copyright 2026 NVIDIA

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

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/internal/utils"
	"github.com/nvidia/doca-platform/pkg/conditions"
	oflow "github.com/nvidia/doca-platform/pkg/openflow"
	"github.com/nvidia/doca-platform/pkg/ovsmodel"
	"github.com/nvidia/doca-platform/pkg/ovsutils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ovsclient "github.com/ovn-org/libovsdb/client"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// Proves the firefly custom-flow path resolves legacy and NSI interfaces identically via getInterfaceCandidates.
var _ = Describe("service chain controller custom flows", func() {
	var (
		mockCtrl *gomock.Controller
		ovsMock  *ovsutils.MockAPI
		opFlow   *oflow.MockOpenFlowAPI
		ctx      = context.Background()
	)

	const (
		serviceID   = "firefly-service-id"
		uplinkPort  = "5"
		servicePort = "7"
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		ovsMock = ovsutils.NewMockAPI(mockCtrl)
		opFlow = oflow.NewMockOpenFlowAPI(mockCtrl)
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	It("adds PTP multicast flows for a Firefly chain resolved entirely from NSI entries", func() {
		uplinkLabels := map[string]string{"uplink": "p0"}
		serviceLabels := map[string]string{"svc.dpu.nvidia.com/interface": "firefly"}

		uplinkEntry := dpuservicev1.InterfaceEntry{
			Name:          "default_nsi-entry-uplink",
			Labels:        uplinkLabels,
			InterfaceType: dpuservicev1.InterfaceTypePhysical,
			Physical:      &dpuservicev1.Physical{InterfaceName: "p0"},
		}
		serviceEntry := dpuservicev1.InterfaceEntry{
			Name:          "default_nsi-entry-service",
			Labels:        serviceLabels,
			InterfaceType: dpuservicev1.InterfaceTypeService,
			Service:       &dpuservicev1.ServiceDef{ServiceID: serviceID, Network: "test-net", InterfaceName: "eth1"},
		}
		nsi := &dpuservicev1.NodeServiceInterfaces{
			ObjectMeta: metav1.ObjectMeta{Name: "test-nsi", Namespace: utils.NSIObjectsNamespace},
			Spec: dpuservicev1.NodeServiceInterfacesSpec{
				Node:       testNodeName,
				Type:       dpuservicev1.NSITypeSFC,
				Interfaces: []dpuservicev1.InterfaceEntry{uplinkEntry, serviceEntry},
			},
			Status: dpuservicev1.NodeServiceInterfacesStatus{
				InterfaceStatuses: []dpuservicev1.InterfaceEntryStatus{
					{Name: uplinkEntry.Name, Conditions: []metav1.Condition{{
						Type: string(conditions.TypeReady), Status: metav1.ConditionTrue, Reason: "Reason", LastTransitionTime: metav1.Now(),
					}}},
					{Name: serviceEntry.Name, Conditions: []metav1.Condition{{
						Type: string(conditions.TypeReady), Status: metav1.ConditionTrue, Reason: "Reason", LastTransitionTime: metav1.Now(),
					}}},
				},
			},
		}

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "firefly-pod",
				Namespace: "default",
				Labels: map[string]string{
					dpuservicev1.DPFServiceIDLabelKey: serviceID,
					CustomFlowsLabelKey:               CustomFlowsFireflyValue,
				},
			},
			Spec: corev1.PodSpec{NodeName: testNodeName},
		}

		registerCondition := func(condition, ofport string) {
			portNum := 0
			_, _ = fmt.Sscanf(ofport, "%d", &portNum)
			ovsMock.EXPECT().WhereAll(conditionMatcher{condition}, gomock.Any()).DoAndReturn(
				func(model interface{}, conds ...interface{}) ovsclient.ConditionalAPI {
					mockConditional := ovsutils.NewMockConditionalAPI(mockCtrl)
					mockConditional.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(
						func(ctx context.Context, result interface{}) error {
							*(result.(*[]ovsmodel.Interface)) = []ovsmodel.Interface{{
								Name:        condition,
								Ofport:      &portNum,
								ExternalIDs: map[string]string{ovsutils.DPFIDKey: condition},
							}}
							return nil
						},
					)
					return mockConditional
				},
			)
			ovsMock.EXPECT().IsIfaceInBr(gomock.Any(), SFCBridge, condition).Return(true, nil)
		}
		registerCondition(uplinkEntry.Name, uplinkPort)
		registerCondition(pod.Namespace+"/"+pod.Name+"/"+serviceEntry.Service.InterfaceName, servicePort)

		sc := &dpuservicev1.ServiceChain{
			ObjectMeta: metav1.ObjectMeta{Name: "firefly-chain", Namespace: "default"},
			Spec: dpuservicev1.ServiceChainSpec{
				Node: &testNodeName,
				Switches: []dpuservicev1.Switch{
					{
						Ports: []dpuservicev1.Port{
							{ServiceInterface: dpuservicev1.ServiceIfc{MatchLabels: uplinkLabels}},
							{ServiceInterface: dpuservicev1.ServiceIfc{MatchLabels: serviceLabels}},
						},
					},
				},
			},
		}

		namespacedName := sc.Namespace + "/" + sc.Name
		rxFlow := fmt.Sprintf("cookie=%d, table=0, priority=%d, in_port=%s, dl_dst=%s, actions=output=%s",
			hash(namespacedName), PriorityCustomFlows, uplinkPort, NonForwardablePTPMulticastMac, servicePort)
		txFlow := fmt.Sprintf("cookie=%d, table=0, priority=%d, in_port=%s, dl_dst=%s, actions=output=%s",
			hash(namespacedName), PriorityCustomFlows, servicePort, NonForwardablePTPMulticastMac, uplinkPort)
		opFlow.EXPECT().AddFlows(gomock.Any(), rxFlow, gomock.Any()).Return(nil)
		opFlow.EXPECT().AddFlows(gomock.Any(), txFlow, gomock.Any()).Return(nil)

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme.Scheme).
			WithIndex(&corev1.Pod{}, podNodeNameKey, func(o client.Object) []string {
				return []string{o.(*corev1.Pod).Spec.NodeName}
			}).
			WithObjects(pod).
			Build()
		scr := &ServiceChainReconciler{Client: fakeClient, NodeName: testNodeName, OVS: ovsMock, OPFlow: opFlow}

		Expect(scr.EnsureCustomFlowsForChain(ctx, sc, nsi)).To(Succeed())
	})

	It("skips Firefly flows while the service pod is absent", func() {
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme.Scheme).
			WithIndex(&corev1.Pod{}, podNodeNameKey, func(o client.Object) []string {
				return []string{o.(*corev1.Pod).Spec.NodeName}
			}).
			Build()
		scr := &ServiceChainReconciler{Client: fakeClient, NodeName: testNodeName}

		isFirefly, err := scr.isFireflyServicePod(ctx, "default", "missing-service")
		Expect(err).NotTo(HaveOccurred())
		Expect(isFirefly).To(BeFalse())
	})

	It("skips custom-flow validation for a physical-only switch", func() {
		p0Labels := map[string]string{"interface": "p0"}
		pf0hpfLabels := map[string]string{"interface": "pf0hpf"}
		p0Entry := dpuservicev1.InterfaceEntry{
			Name:          "default_p0",
			Labels:        p0Labels,
			InterfaceType: dpuservicev1.InterfaceTypePhysical,
			Physical:      &dpuservicev1.Physical{InterfaceName: "p0"},
		}
		pf0hpfEntry := dpuservicev1.InterfaceEntry{
			Name:          "default_pf0hpf",
			Labels:        pf0hpfLabels,
			InterfaceType: dpuservicev1.InterfaceTypePhysical,
			Physical:      &dpuservicev1.Physical{InterfaceName: "B21c1pf0"},
		}
		nsi := &dpuservicev1.NodeServiceInterfaces{
			Spec: dpuservicev1.NodeServiceInterfacesSpec{
				Node:       testNodeName,
				Type:       dpuservicev1.NSITypeSFC,
				Interfaces: []dpuservicev1.InterfaceEntry{p0Entry, pf0hpfEntry},
			},
			Status: dpuservicev1.NodeServiceInterfacesStatus{
				InterfaceStatuses: []dpuservicev1.InterfaceEntryStatus{
					{Name: p0Entry.Name, Conditions: []metav1.Condition{{Type: string(conditions.TypeReady), Status: metav1.ConditionTrue}}},
					{Name: pf0hpfEntry.Name, Conditions: []metav1.Condition{{Type: string(conditions.TypeReady), Status: metav1.ConditionTrue}}},
				},
			},
		}

		registerPhysicalPort := func(entryName, ofPort string) {
			portNum := 0
			_, _ = fmt.Sscanf(ofPort, "%d", &portNum)
			ovsMock.EXPECT().WhereAll(conditionMatcher{entryName}, gomock.Any()).DoAndReturn(
				func(model interface{}, conds ...interface{}) ovsclient.ConditionalAPI {
					mockConditional := ovsutils.NewMockConditionalAPI(mockCtrl)
					mockConditional.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(
						func(ctx context.Context, result interface{}) error {
							*(result.(*[]ovsmodel.Interface)) = []ovsmodel.Interface{{Name: entryName, Ofport: &portNum}}
							return nil
						},
					)
					return mockConditional
				},
			)
			ovsMock.EXPECT().IsIfaceInBr(gomock.Any(), SFCBridge, entryName).Return(true, nil)
		}
		registerPhysicalPort(p0Entry.Name, "1")
		registerPhysicalPort(pf0hpfEntry.Name, "2")

		sc := &dpuservicev1.ServiceChain{
			ObjectMeta: metav1.ObjectMeta{Name: "passthrough", Namespace: "default"},
			Spec: dpuservicev1.ServiceChainSpec{
				Node: &testNodeName,
				Switches: []dpuservicev1.Switch{{Ports: []dpuservicev1.Port{
					{ServiceInterface: dpuservicev1.ServiceIfc{MatchLabels: p0Labels}},
					{ServiceInterface: dpuservicev1.ServiceIfc{MatchLabels: pf0hpfLabels}},
				}}},
			},
		}
		scr := &ServiceChainReconciler{Client: fake.NewClientBuilder().WithScheme(scheme.Scheme).Build(), NodeName: testNodeName, OVS: ovsMock, OPFlow: opFlow}

		Expect(scr.EnsureCustomFlowsForChain(ctx, sc, nsi)).To(Succeed())
	})

	It("skips custom flows for a switch with a non-Firefly service and uplinks", func() {
		serviceLabels := map[string]string{"service": "non-firefly"}
		p0Labels := map[string]string{"interface": "p0"}
		p1Labels := map[string]string{"interface": "p1"}
		serviceEntry := dpuservicev1.InterfaceEntry{
			Name:          "default_non-firefly-service",
			Labels:        serviceLabels,
			InterfaceType: dpuservicev1.InterfaceTypeService,
			Service:       &dpuservicev1.ServiceDef{ServiceID: "non-firefly-service-id"},
		}
		p0Entry := dpuservicev1.InterfaceEntry{
			Name:          "default_p0",
			Labels:        p0Labels,
			InterfaceType: dpuservicev1.InterfaceTypePhysical,
			Physical:      &dpuservicev1.Physical{InterfaceName: "p0"},
		}
		p1Entry := dpuservicev1.InterfaceEntry{
			Name:          "default_p1",
			Labels:        p1Labels,
			InterfaceType: dpuservicev1.InterfaceTypePhysical,
			Physical:      &dpuservicev1.Physical{InterfaceName: "p1"},
		}
		nsi := &dpuservicev1.NodeServiceInterfaces{
			Spec: dpuservicev1.NodeServiceInterfacesSpec{
				Node:       testNodeName,
				Type:       dpuservicev1.NSITypeSFC,
				Interfaces: []dpuservicev1.InterfaceEntry{serviceEntry, p0Entry, p1Entry},
			},
			Status: dpuservicev1.NodeServiceInterfacesStatus{
				InterfaceStatuses: []dpuservicev1.InterfaceEntryStatus{
					{Name: serviceEntry.Name, Conditions: []metav1.Condition{{Type: string(conditions.TypeReady), Status: metav1.ConditionTrue}}},
					{Name: p0Entry.Name, Conditions: []metav1.Condition{{Type: string(conditions.TypeReady), Status: metav1.ConditionTrue}}},
					{Name: p1Entry.Name, Conditions: []metav1.Condition{{Type: string(conditions.TypeReady), Status: metav1.ConditionTrue}}},
				},
			},
		}

		registerPhysicalPort := func(entryName string, ofPort int) {
			ovsMock.EXPECT().WhereAll(conditionMatcher{entryName}, gomock.Any()).DoAndReturn(
				func(model interface{}, conds ...interface{}) ovsclient.ConditionalAPI {
					mockConditional := ovsutils.NewMockConditionalAPI(mockCtrl)
					mockConditional.EXPECT().List(gomock.Any(), gomock.Any()).DoAndReturn(
						func(ctx context.Context, result interface{}) error {
							*(result.(*[]ovsmodel.Interface)) = []ovsmodel.Interface{{Name: entryName, Ofport: &ofPort}}
							return nil
						},
					)
					return mockConditional
				},
			)
			ovsMock.EXPECT().IsIfaceInBr(gomock.Any(), SFCBridge, entryName).Return(true, nil)
		}
		registerPhysicalPort(p0Entry.Name, 1)
		registerPhysicalPort(p1Entry.Name, 2)

		nonFireflyPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "non-firefly-pod",
				Namespace: "default",
				Labels: map[string]string{
					dpuservicev1.DPFServiceIDLabelKey: serviceEntry.Service.ServiceID,
				},
			},
			Spec: corev1.PodSpec{NodeName: testNodeName},
		}
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme.Scheme).
			WithIndex(&corev1.Pod{}, podNodeNameKey, func(o client.Object) []string {
				return []string{o.(*corev1.Pod).Spec.NodeName}
			}).
			WithObjects(nonFireflyPod).
			Build()
		sc := &dpuservicev1.ServiceChain{
			ObjectMeta: metav1.ObjectMeta{Name: "non-firefly-chain", Namespace: "default"},
			Spec: dpuservicev1.ServiceChainSpec{
				Node: &testNodeName,
				Switches: []dpuservicev1.Switch{{Ports: []dpuservicev1.Port{
					{ServiceInterface: dpuservicev1.ServiceIfc{MatchLabels: serviceLabels}},
					{ServiceInterface: dpuservicev1.ServiceIfc{MatchLabels: p0Labels}},
					{ServiceInterface: dpuservicev1.ServiceIfc{MatchLabels: p1Labels}},
				}}},
			},
		}
		scr := &ServiceChainReconciler{Client: fakeClient, NodeName: testNodeName, OVS: ovsMock, OPFlow: opFlow}

		Expect(scr.EnsureCustomFlowsForChain(ctx, sc, nsi)).To(Succeed())
	})

	It("propagates failures while looking up the service pod", func() {
		expected := fmt.Errorf("list pods")
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme.Scheme).
			WithInterceptorFuncs(interceptor.Funcs{
				List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
					return expected
				},
			}).
			Build()
		scr := &ServiceChainReconciler{Client: fakeClient, NodeName: testNodeName}

		_, err := scr.isFireflyServicePod(ctx, "default", serviceID)
		Expect(err).To(MatchError(expected))
	})

	It("rejects a service entry without a Service definition", func() {
		entryLabels := map[string]string{"service": "missing-definition"}
		nsi := &dpuservicev1.NodeServiceInterfaces{
			Spec: dpuservicev1.NodeServiceInterfacesSpec{
				Interfaces: []dpuservicev1.InterfaceEntry{{
					Name:          "default_missing-definition",
					Labels:        entryLabels,
					InterfaceType: dpuservicev1.InterfaceTypeService,
				}},
			},
		}
		sc := &dpuservicev1.ServiceChain{ObjectMeta: metav1.ObjectMeta{Namespace: "default"}}
		port := dpuservicev1.Port{ServiceInterface: dpuservicev1.ServiceIfc{MatchLabels: entryLabels}}
		scr := &ServiceChainReconciler{Client: fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()}

		_, _, err := scr.resolveFireflyPort(ctx, sc, nsi, port)
		Expect(err).To(MatchError(ContainSubstring("service interface missing Service definition")))
	})
})
