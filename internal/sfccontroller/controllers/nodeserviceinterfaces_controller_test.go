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
	"errors"
	"fmt"

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

//nolint:goconst
var _ = Describe("node service interfaces controller", func() {
	var (
		mockCtrl        *gomock.Controller
		cleanupObjects  []client.Object
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

	newNSI := func(entries ...dpuservicev1.InterfaceEntry) *dpuservicev1.NodeServiceInterfaces {
		return &dpuservicev1.NodeServiceInterfaces{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-nsi",
				Namespace: utils.NSIObjectsNamespace,
			},
			Spec: dpuservicev1.NodeServiceInterfacesSpec{
				Node:       testNodeName,
				Type:       dpuservicev1.NSITypeSFC,
				Interfaces: entries,
			},
		}
	}

	reconcileReq := func(nsi *dpuservicev1.NodeServiceInterfaces) ctrl.Request {
		return ctrl.Request{NamespacedName: types.NamespacedName{Namespace: nsi.Namespace, Name: nsi.Name}}
	}

	// pfEntry is computed lazily since it needs the namespace created in BeforeEach.
	pfEntry := func() dpuservicev1.InterfaceEntry {
		return dpuservicev1.InterfaceEntry{
			Name:          ns.Name + "_pf-entry",
			InterfaceType: dpuservicev1.InterfaceTypePF,
			PF:            &dpuservicev1.PF{ID: 1},
		}
	}

	It("should add finalizer", func() {
		nsi := newNSI()
		Expect(testClient.Create(ctx, nsi)).To(Succeed())
		cleanupObjects = append(cleanupObjects, nsi)

		_, err := nsir.Reconcile(ctx, reconcileReq(nsi))
		Expect(err).To(Succeed())

		Expect(testClient.Get(ctx, types.NamespacedName{Namespace: nsi.Namespace, Name: nsi.Name}, nsi)).To(Succeed())
		Expect(controllerutil.ContainsFinalizer(nsi, dpuservicev1.NodeServiceInterfacesFinalizer)).To(BeTrue())
	})

	Context("reconcile active entries", func() {
		DescribeTable("reconcile flow", func(entry dpuservicev1.InterfaceEntry, mocks func(), ready bool) {
			entry.Name = ns.Name + "_" + entry.Name
			nsi := newNSI(entry)
			controllerutil.AddFinalizer(nsi, dpuservicev1.NodeServiceInterfacesFinalizer)
			Expect(testClient.Create(ctx, nsi)).To(Succeed())
			cleanupObjects = append(cleanupObjects, nsi)

			mocks()

			result, err := nsir.Reconcile(ctx, reconcileReq(nsi))
			Expect(err).To(Succeed())
			Expect(result.RequeueAfter).NotTo(BeZero())

			Expect(testClient.Get(ctx, types.NamespacedName{Namespace: nsi.Namespace, Name: nsi.Name}, nsi)).To(Succeed())
			Expect(conditions.IsTrue(nsi, dpuservicev1.NodeServiceInterfacesReconciled)).To(Equal(ready))

			entryStatus := nsi.GetEntryStatus(entry.Name)
			Expect(entryStatus).NotTo(BeNil())
			Expect(conditions.IsTrue(entryStatus, conditions.TypeReady)).To(Equal(ready))
		},
			Entry("success pf interface",
				dpuservicev1.InterfaceEntry{
					Name:          "pf-entry",
					InterfaceType: dpuservicev1.InterfaceTypePF,
					PF:            &dpuservicev1.PF{ID: 1},
				},
				func() {
					ecpfManagerMock.EXPECT().GetRepresentorForPFServiceInterface(gomock.Any()).Return("pf0hpf", nil)
					ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any()).Return(nil)
					ovsMock.EXPECT().SetIfaceExternalIDs(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
					ovsMock.EXPECT().SetPortExternalIDs(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
				}, true),
			Entry("failed to get port name",
				dpuservicev1.InterfaceEntry{
					Name:          "pf-entry",
					InterfaceType: dpuservicev1.InterfaceTypePF,
					PF:            &dpuservicev1.PF{ID: 1},
				},
				func() {
					ecpfManagerMock.EXPECT().GetRepresentorForPFServiceInterface(gomock.Any()).Return("", fmt.Errorf("failed to get port name"))
				}, false),
			Entry("success physical interface",
				dpuservicev1.InterfaceEntry{
					Name:          "physical-entry",
					InterfaceType: dpuservicev1.InterfaceTypePhysical,
					Physical:      &dpuservicev1.Physical{InterfaceName: "eth2"},
				},
				func() {
					ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any()).Return(nil)
					ovsMock.EXPECT().SetIfaceExternalIDs(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
					ovsMock.EXPECT().SetPortExternalIDs(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
				}, true),
			Entry("failed to add physical interface",
				dpuservicev1.InterfaceEntry{
					Name:          "physical-entry",
					InterfaceType: dpuservicev1.InterfaceTypePhysical,
					Physical:      &dpuservicev1.Physical{InterfaceName: "eth2"},
				},
				func() {
					ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any()).Return(errors.New("failed to add port"))
				}, false),
			Entry("success patch interface",
				dpuservicev1.InterfaceEntry{
					Name:          "patch-entry",
					InterfaceType: dpuservicev1.InterfaceTypePatch,
					Patch:         &dpuservicev1.PatchDef{PeerBridge: "br-peer"},
				},
				func() {
					ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any()).Return(nil)
					ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any()).Return(nil)
					ovsMock.EXPECT().SetIfaceOptions(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
					ovsMock.EXPECT().SetIfaceOptions(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
					ovsMock.EXPECT().SetIfaceExternalIDs(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
					ovsMock.EXPECT().SetIfaceExternalIDs(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
				}, true),
			Entry("nothing to do for vlan type interface",
				dpuservicev1.InterfaceEntry{
					Name:          "vlan-entry",
					InterfaceType: dpuservicev1.InterfaceTypeVLAN,
					Vlan:          &dpuservicev1.VLAN{VlanID: 1, ParentInterfaceRef: "eth1"},
				},
				func() {},
				true),
			Entry("nothing to do for service type interface",
				dpuservicev1.InterfaceEntry{
					Name:          "service-entry",
					InterfaceType: dpuservicev1.InterfaceTypeService,
					Service:       &dpuservicev1.ServiceDef{ServiceID: "test-service", Network: "test-network", InterfaceName: "test-interface"},
				},
				func() {},
				true),
			Entry("success ovn interface",
				dpuservicev1.InterfaceEntry{
					Name:          "ovn-entry",
					InterfaceType: dpuservicev1.InterfaceTypeOVN,
					OVN:           &dpuservicev1.OVN{},
				},
				func() {
					ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any()).Return(nil)
					ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any()).Return(nil)
					ovsMock.EXPECT().SetIfaceOptions(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
					ovsMock.EXPECT().SetIfaceOptions(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
					ovsMock.EXPECT().SetIfaceExternalIDs(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
				}, true),
		)
	})

	Context("reconcile terminating entry", func() {
		It("should release OVS resources and set ResourceReleased", func() {
			entry := pfEntry()
			entry.Terminating = true
			nsi := newNSI(entry)
			controllerutil.AddFinalizer(nsi, dpuservicev1.NodeServiceInterfacesFinalizer)
			Expect(testClient.Create(ctx, nsi)).To(Succeed())
			cleanupObjects = append(cleanupObjects, nsi)

			ecpfManagerMock.EXPECT().GetRepresentorForPFServiceInterface(gomock.Any()).Return("pf0hpf", nil)
			ovsMock.EXPECT().DelPort(gomock.Any(), gomock.Any(), "pf0hpf", gomock.Any()).Return(nil)

			_, err := nsir.Reconcile(ctx, reconcileReq(nsi))
			Expect(err).To(Succeed())

			Expect(testClient.Get(ctx, types.NamespacedName{Namespace: nsi.Namespace, Name: nsi.Name}, nsi)).To(Succeed())
			entryStatus := nsi.GetEntryStatus(entry.Name)
			Expect(entryStatus).NotTo(BeNil())
			Expect(conditions.IsTrue(entryStatus, dpuservicev1.ResourceReleased)).To(BeTrue())
		})

		It("should not touch OVS again once ResourceReleased is already set", func() {
			entry := pfEntry()
			entry.Terminating = true
			nsi := newNSI(entry)
			controllerutil.AddFinalizer(nsi, dpuservicev1.NodeServiceInterfacesFinalizer)
			Expect(testClient.Create(ctx, nsi)).To(Succeed())
			cleanupObjects = append(cleanupObjects, nsi)

			nsi.Status.InterfaceStatuses = []dpuservicev1.InterfaceEntryStatus{{Name: entry.Name}}
			Expect(testClient.Status().Update(ctx, nsi)).To(Succeed())
			conditions.AddTrue(nsi.GetEntryStatus(entry.Name), dpuservicev1.ResourceReleased)
			Expect(testClient.Status().Update(ctx, nsi)).To(Succeed())

			// no OVS mock expectations set: any call would fail the test.
			_, err := nsir.Reconcile(ctx, reconcileReq(nsi))
			Expect(err).To(Succeed())
		})

		It("should requeue with error if release fails", func() {
			entry := pfEntry()
			entry.Terminating = true
			nsi := newNSI(entry)
			controllerutil.AddFinalizer(nsi, dpuservicev1.NodeServiceInterfacesFinalizer)
			Expect(testClient.Create(ctx, nsi)).To(Succeed())
			cleanupObjects = append(cleanupObjects, nsi)

			ecpfManagerMock.EXPECT().GetRepresentorForPFServiceInterface(gomock.Any()).Return("pf0hpf", nil)
			ovsMock.EXPECT().DelPort(gomock.Any(), gomock.Any(), "pf0hpf", gomock.Any()).Return(errors.New("failed to delete port"))

			result, err := nsir.Reconcile(ctx, reconcileReq(nsi))
			Expect(err).To(Succeed())
			Expect(result.RequeueAfter).NotTo(BeZero())

			Expect(testClient.Get(ctx, types.NamespacedName{Namespace: nsi.Namespace, Name: nsi.Name}, nsi)).To(Succeed())
			entryStatus := nsi.GetEntryStatus(entry.Name)
			Expect(entryStatus).NotTo(BeNil())
			Expect(conditions.IsTrue(entryStatus, dpuservicev1.ResourceReleased)).To(BeFalse())
		})
	})

	Context("orphaned status pruning", func() {
		It("removes status entries with no matching spec entry", func() {
			nsi := newNSI()
			controllerutil.AddFinalizer(nsi, dpuservicev1.NodeServiceInterfacesFinalizer)
			Expect(testClient.Create(ctx, nsi)).To(Succeed())
			cleanupObjects = append(cleanupObjects, nsi)

			nsi.Status.InterfaceStatuses = []dpuservicev1.InterfaceEntryStatus{{Name: "stale-entry"}}
			Expect(testClient.Status().Update(ctx, nsi)).To(Succeed())

			_, err := nsir.Reconcile(ctx, reconcileReq(nsi))
			Expect(err).To(Succeed())

			Expect(testClient.Get(ctx, types.NamespacedName{Namespace: nsi.Namespace, Name: nsi.Name}, nsi)).To(Succeed())
			Expect(nsi.Status.InterfaceStatuses).To(BeEmpty())
		})
	})

	It("reconcile non existing object - consider as deleted", func() {
		nn := types.NamespacedName{Namespace: "non-existing", Name: "non-existing"}

		result, err := nsir.Reconcile(ctx, ctrl.Request{NamespacedName: nn})
		Expect(err).To(Succeed())
		Expect(result.RequeueAfter).To(BeZero())
	})

	It("ignores NodeServiceInterfaces outside the central namespace", func() {
		nsi := newNSI(pfEntry())
		nsi.Name = "tenant-nsi"
		nsi.Namespace = ns.Name
		Expect(testClient.Create(ctx, nsi)).To(Succeed())
		cleanupObjects = append(cleanupObjects, nsi)

		result, err := nsir.Reconcile(ctx, reconcileReq(nsi))
		Expect(err).To(Succeed())
		Expect(result.RequeueAfter).To(BeZero())

		Expect(testClient.Get(ctx, client.ObjectKeyFromObject(nsi), nsi)).To(Succeed())
		Expect(nsi.Finalizers).To(BeEmpty())
		Expect(nsi.Status.InterfaceStatuses).To(BeEmpty())
	})

	Context("reconcile delete", func() {
		var deletedNSI *dpuservicev1.NodeServiceInterfaces

		BeforeEach(func() {
			deletedNSI = newNSI(pfEntry())
			controllerutil.AddFinalizer(deletedNSI, dpuservicev1.NodeServiceInterfacesFinalizer)
			Expect(testClient.Create(ctx, deletedNSI)).To(Succeed())
			cleanupObjects = append(cleanupObjects, deletedNSI)
			Expect(testClient.Delete(ctx, deletedNSI)).To(Succeed())
		})

		It("cleans up every entry and removes the finalizer", func() {
			ecpfManagerMock.EXPECT().GetRepresentorForPFServiceInterface(gomock.Any()).Return("pf0hpf", nil)
			ovsMock.EXPECT().DelPort(gomock.Any(), gomock.Any(), "pf0hpf", gomock.Any()).Return(nil)

			result, err := nsir.Reconcile(ctx, reconcileReq(deletedNSI))
			Expect(err).To(Succeed())
			Expect(result.RequeueAfter).To(BeZero())
		})

		It("requeues with error if OVS cleanup fails", func() {
			ecpfManagerMock.EXPECT().GetRepresentorForPFServiceInterface(gomock.Any()).Return("pf0hpf", nil)
			ovsMock.EXPECT().DelPort(gomock.Any(), gomock.Any(), "pf0hpf", gomock.Any()).Return(errors.New("failed to delete port"))

			result, err := nsir.Reconcile(ctx, reconcileReq(deletedNSI))
			Expect(err).To(Succeed())
			Expect(result.RequeueAfter).NotTo(BeZero())

			Expect(testClient.Get(ctx, types.NamespacedName{Namespace: deletedNSI.Namespace, Name: deletedNSI.Name}, deletedNSI)).To(Succeed())
			Expect(controllerutil.ContainsFinalizer(deletedNSI, dpuservicev1.NodeServiceInterfacesFinalizer)).To(BeTrue())
		})
	})
})
