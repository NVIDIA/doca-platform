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

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/internal/utils"
	"github.com/nvidia/doca-platform/pkg/conditions"
	ecpfMock "github.com/nvidia/doca-platform/pkg/ecpf/mock"
	"github.com/nvidia/doca-platform/pkg/ovsutils"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// Exercises the multi-release window where legacy ServiceInterface and NSI reconcilers coexist on the same node.
var _ = Describe("legacy and NSI reconcilers coexisting", func() {
	var (
		mockCtrl        *gomock.Controller
		cleanupObjects  []client.Object
		sir             *ServiceInterfaceReconciler
		nsir            *NodeServiceInterfacesReconciler
		ovsMock         *ovsutils.MockAPI
		ecpfManagerMock *ecpfMock.MockECPFManager
		ctx             = context.Background()
		ns              *corev1.Namespace
	)

	BeforeEach(func() {
		cleanupObjects = []client.Object{}
		mockCtrl = gomock.NewController(GinkgoT())
		ovsMock = ovsutils.NewMockAPI(mockCtrl)
		ecpfManagerMock = ecpfMock.NewMockECPFManager(mockCtrl)

		sir = &ServiceInterfaceReconciler{
			Client:      testClient,
			NodeName:    testNodeName,
			OVS:         ovsMock,
			ECPFManager: ecpfManagerMock,
		}
		nsir = &NodeServiceInterfacesReconciler{
			Client:      testClient,
			NodeName:    testNodeName,
			OVS:         ovsMock,
			ECPFManager: ecpfManagerMock,
		}

		ns = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-ns-",
			},
		}
		Expect(testClient.Create(ctx, ns)).To(Succeed())
	})

	AfterEach(func() {
		Expect(testutils.CleanupWithFinalizerRemovalAndWait(ctx, testClient, cleanupObjects...)).To(Succeed())
		mockCtrl.Finish()
	})

	It("reconciles a legacy ServiceInterface and an NSI entry for the same node without cross-interference", func() {
		si := &dpuservicev1.ServiceInterface{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "legacy-pf",
				Namespace: ns.Name,
			},
			Spec: dpuservicev1.ServiceInterfaceSpec{
				Node:          &testNodeName,
				InterfaceType: dpuservicev1.InterfaceTypePF,
				PF:            &dpuservicev1.PF{ID: 1},
			},
		}
		controllerutil.AddFinalizer(si, ServiceInterfaceFinalizer)
		Expect(testClient.Create(ctx, si)).To(Succeed())
		cleanupObjects = append(cleanupObjects, si)

		nsi := &dpuservicev1.NodeServiceInterfaces{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "nsi-shard",
				Namespace: utils.NSIObjectsNamespace,
			},
			Spec: dpuservicev1.NodeServiceInterfacesSpec{
				Node: testNodeName,
				Type: dpuservicev1.NSITypeSFC,
				Interfaces: []dpuservicev1.InterfaceEntry{
					{
						Name:          ns.Name + "_nsi-vf",
						InterfaceType: dpuservicev1.InterfaceTypeVF,
						VF:            &dpuservicev1.VF{PFID: 0, VFID: 5},
					},
				},
			},
		}
		controllerutil.AddFinalizer(nsi, dpuservicev1.NodeServiceInterfacesFinalizer)
		Expect(testClient.Create(ctx, nsi)).To(Succeed())
		cleanupObjects = append(cleanupObjects, nsi)

		// Both reconcilers share the same OVS/ECPF mocks to prove their calls don't collide.
		ecpfManagerMock.EXPECT().GetRepresentorForPFServiceInterface(gomock.Any()).Return("pf0hpf", nil)
		ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any()).Return(nil)
		ovsMock.EXPECT().SetIfaceExternalIDs(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		ovsMock.EXPECT().SetPortExternalIDs(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

		siReq := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: si.Namespace, Name: si.Name}}
		_, err := sir.Reconcile(ctx, siReq)
		Expect(err).To(Succeed())

		ecpfManagerMock.EXPECT().GetRepresentorForVFServiceInterface(gomock.Any()).Return("pf0vf5", nil)
		ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any()).Return(nil)
		ovsMock.EXPECT().SetIfaceExternalIDs(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		ovsMock.EXPECT().SetPortExternalIDs(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

		nsiReq := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: nsi.Namespace, Name: nsi.Name}}
		_, err = nsir.Reconcile(ctx, nsiReq)
		Expect(err).To(Succeed())

		Expect(testClient.Get(ctx, siReq.NamespacedName, si)).To(Succeed())
		Expect(conditions.IsTrue(si, dpuservicev1.ServiceInterfaceReconciled)).To(BeTrue())
		Expect(controllerutil.ContainsFinalizer(si, ServiceInterfaceFinalizer)).To(BeTrue())

		Expect(testClient.Get(ctx, nsiReq.NamespacedName, nsi)).To(Succeed())
		Expect(conditions.IsTrue(nsi, dpuservicev1.NodeServiceInterfacesReconciled)).To(BeTrue())
		Expect(controllerutil.ContainsFinalizer(nsi, dpuservicev1.NodeServiceInterfacesFinalizer)).To(BeTrue())
	})
})
