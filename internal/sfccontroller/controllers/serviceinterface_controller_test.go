/*
Copyright 2024 NVIDIA

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
	"github.com/nvidia/doca-platform/pkg/ovsutils"
	mock_networkhelper "github.com/nvidia/doca-platform/pkg/utils/networkhelper/mock"
	testutils "github.com/nvidia/doca-platform/test/utils"

	"github.com/fluxcd/pkg/runtime/conditions"
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
var _ = Describe("service interface controller", func() {
	var (
		mockCtrl          *gomock.Controller
		cleanupObjects    []client.Object
		sir               *ServiceInterfaceReconciler
		ovsMock           *ovsutils.MockAPI
		networkHelperMock *mock_networkhelper.MockNetworkHelper
		ctx               = context.Background()
		ns                *corev1.Namespace
	)

	BeforeEach(func() {
		cleanupObjects = []client.Object{}
		mockCtrl = gomock.NewController(GinkgoT())
		ovsMock = ovsutils.NewMockAPI(mockCtrl)
		networkHelperMock = mock_networkhelper.NewMockNetworkHelper(mockCtrl)

		sir = &ServiceInterfaceReconciler{
			Client:        testClient,
			NodeName:      testNodeName,
			OVS:           ovsMock,
			NetworkHelper: networkHelperMock,
		}

		ns = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-ns-",
			},
		}
		Expect(testClient.Create(ctx, ns)).To(Succeed())
		// note: envtest does not support namespace deletion, we use generated name so its fine to keep
		// the namespace around for the test duration.
		// see: https://book.kubebuilder.io/reference/envtest#namespace-usage-limitation
	})

	AfterEach(func() {
		Expect(testutils.CleanupWithFinalizerRemovalAndWait(ctx, testClient, cleanupObjects...)).To(Succeed())
		mockCtrl.Finish()
	})

	brOVN := "br-ovn"
	ovnIfaceSpec := dpuservicev1.ServiceInterfaceSpec{
		Node:          &testNodeName,
		InterfaceType: dpuservicev1.InterfaceTypeOVN,
		OVN: &dpuservicev1.OVN{
			ExternalBridge: &brOVN,
		},
	}

	peerBridge := "br-peer"
	peerPatchName := "peer-patch"
	patchIfaceSpec := dpuservicev1.ServiceInterfaceSpec{
		Node:          &testNodeName,
		InterfaceType: dpuservicev1.InterfaceTypePatch,
		Patch: &dpuservicev1.PatchDef{
			PeerBridge:    peerBridge,
			PeerPatchName: &peerPatchName,
		},
	}

	pfIfaceSpec := dpuservicev1.ServiceInterfaceSpec{
		Node:          &testNodeName,
		InterfaceType: dpuservicev1.InterfaceTypePF,
		PF: &dpuservicev1.PF{
			ID: 1,
		},
	}

	vfIfaceSpec := dpuservicev1.ServiceInterfaceSpec{
		Node:          &testNodeName,
		InterfaceType: dpuservicev1.InterfaceTypeVF,
		VF: &dpuservicev1.VF{
			PFID: 1,
			VFID: 2,
		},
	}

	physicalIfaceSpec := dpuservicev1.ServiceInterfaceSpec{
		Node:          &testNodeName,
		InterfaceType: dpuservicev1.InterfaceTypePhysical,
		Physical: &dpuservicev1.Physical{
			InterfaceName: "eth2",
		},
	}

	vlanIfaceSpec := dpuservicev1.ServiceInterfaceSpec{
		Node:          &testNodeName,
		InterfaceType: dpuservicev1.InterfaceTypeVLAN,
		Vlan: &dpuservicev1.VLAN{
			VlanID:             1,
			ParentInterfaceRef: "eth1",
		},
	}

	serviceIfaceSpec := dpuservicev1.ServiceInterfaceSpec{
		Node:          &testNodeName,
		InterfaceType: dpuservicev1.InterfaceTypeService,
		Service: &dpuservicev1.ServiceDef{
			ServiceID:     "test-service",
			Network:       "test-network",
			InterfaceName: "test-interface",
		},
	}

	Context("add interface", func() {
		var si *dpuservicev1.ServiceInterface

		BeforeEach(func() {
			si = &dpuservicev1.ServiceInterface{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service-interface",
					Namespace: ns.Name,
				},
			}
		})

		It("should add finalizer", func() {
			si := &dpuservicev1.ServiceInterface{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service-interface",
					Namespace: ns.Name,
				},
				Spec: dpuservicev1.ServiceInterfaceSpec{
					Node:          &testNodeName,
					InterfaceType: dpuservicev1.InterfaceTypePF,
					PF: &dpuservicev1.PF{
						ID: 1,
					},
				},
			}
			Expect(testClient.Create(ctx, si)).To(Succeed())
			cleanupObjects = append(cleanupObjects, si)

			_, err := sir.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
				Namespace: si.Namespace,
				Name:      si.Name,
			}})
			Expect(err).To(Succeed())

			Expect(testClient.Get(ctx, types.NamespacedName{
				Namespace: si.Namespace,
				Name:      si.Name,
			}, si)).To(Succeed())
			Expect(controllerutil.ContainsFinalizer(si, ServiceInterfaceFinalizer)).To(BeTrue())
		})

		DescribeTable("reconcile flow", func(spec dpuservicev1.ServiceInterfaceSpec, mocks func(), ready bool) {
			si := &dpuservicev1.ServiceInterface{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service-interface",
					Namespace: ns.Name,
				},
				Spec: spec,
			}
			controllerutil.AddFinalizer(si, ServiceInterfaceFinalizer)
			Expect(testClient.Create(ctx, si)).To(Succeed())
			cleanupObjects = append(cleanupObjects, si)

			// init mocks
			mocks()

			req := ctrl.Request{NamespacedName: types.NamespacedName{
				Namespace: si.Namespace,
				Name:      si.Name,
			}}

			result, err := sir.Reconcile(ctx, req)
			Expect(err).To(Succeed())
			Expect(result.RequeueAfter).NotTo(BeZero())

			Expect(testClient.Get(ctx, types.NamespacedName{
				Namespace: si.Namespace,
				Name:      si.Name,
			}, si)).To(Succeed())
			Expect(conditions.IsReady(si)).To(Equal(ready))
		},
			Entry("fail to set port external ids",
				physicalIfaceSpec,
				func() {
					ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
					ovsMock.EXPECT().SetIfaceExternalIDs(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
					ovsMock.EXPECT().SetPortExternalIDs(gomock.Any(), gomock.Any(), gomock.Any()).
						Return(errors.New("failed to set port external ids"))
				}, false),
			Entry("failed to add interface",
				physicalIfaceSpec,
				func() {
					ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
						Return(errors.New("failed to add port"))
				}, false),
			Entry("failed to get port name",
				pfIfaceSpec,
				func() {
					networkHelperMock.EXPECT().GetPFRepresentorDPU(gomock.Any()).Return("", fmt.Errorf("failed to get port name"))
				}, false),
			Entry("success pf interface",
				pfIfaceSpec,
				func() {
					networkHelperMock.EXPECT().GetPFRepresentorDPU(gomock.Any()).Return("pf0hpf", nil)
					ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
					ovsMock.EXPECT().SetIfaceExternalIDs(
						gomock.Any(),
						gomock.Any(),
						gomock.Eq(map[string]string{"dpf-id": client.ObjectKeyFromObject(si).String()}),
					).Return(nil)
					ovsMock.EXPECT().SetPortExternalIDs(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
				}, true),
			Entry("success vf interface",
				vfIfaceSpec,
				func() {
					networkHelperMock.EXPECT().GetVFRepresentorDPU(gomock.Any(), gomock.Any()).Return("pf0vf2", nil)
					ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
					ovsMock.EXPECT().SetIfaceExternalIDs(
						gomock.Any(),
						gomock.Any(),
						gomock.Eq(map[string]string{"dpf-id": client.ObjectKeyFromObject(si).String()}),
					).Return(nil)
					ovsMock.EXPECT().SetPortExternalIDs(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
				}, true),
			Entry("fail to set inteface external ids",
				physicalIfaceSpec,
				func() {
					ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
					ovsMock.EXPECT().SetIfaceExternalIDs(gomock.Any(), gomock.Any(), gomock.Any()).
						Return(errors.New("failed to set external ids"))
				}, false),
			Entry("success ovn interface",
				ovnIfaceSpec,
				func() {
					ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
						Return(nil)
					ovsMock.EXPECT().SetIfaceOptions(gomock.Any(), gomock.Any(), gomock.Any()).
						Return(nil)
					ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
						Return(nil)
					ovsMock.EXPECT().SetIfaceOptions(gomock.Any(), gomock.Any(), gomock.Any()).
						Return(nil)
					ovsMock.EXPECT().SetIfaceExternalIDs(gomock.Any(), gomock.Any(), gomock.Eq(map[string]string{"dpf-id": client.ObjectKeyFromObject(si).String()})).
						Return(nil)
				}, true),
			Entry("failed to add patch port",
				ovnIfaceSpec,
				func() {
					ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
						Return(errors.New("failed to add patch port"))
				}, false),
			Entry("failed to set interface options",
				ovnIfaceSpec,
				func() {
					ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
						Return(nil)
					ovsMock.EXPECT().SetIfaceOptions(gomock.Any(), gomock.Any(), gomock.Any()).
						Return(errors.New("failed to set interface options"))
				},
				false),
			Entry("failed to add second port",
				ovnIfaceSpec,
				func() {
					ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
						Return(nil)
					ovsMock.EXPECT().SetIfaceOptions(gomock.Any(), gomock.Any(), gomock.Any()).
						Return(nil)
					ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
						Return(errors.New("failed to add port"))
				}, false),
			Entry("failed to set interface external ids",
				ovnIfaceSpec,
				func() {
					ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
						Return(nil)
					ovsMock.EXPECT().SetIfaceOptions(gomock.Any(), gomock.Any(), gomock.Any()).
						Return(nil)
					ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
						Return(nil)
					ovsMock.EXPECT().SetIfaceOptions(gomock.Any(), gomock.Any(), gomock.Any()).
						Return(nil)
					ovsMock.EXPECT().SetIfaceExternalIDs(gomock.Any(), gomock.Any(), gomock.Any()).
						Return(errors.New("failed to set interface external ids"))
				}, false),
			Entry("failed to set interface options",
				ovnIfaceSpec,
				func() {
					ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
						Return(nil)
					ovsMock.EXPECT().SetIfaceOptions(gomock.Any(), gomock.Any(), gomock.Any()).
						Return(nil)
					ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
						Return(nil)
					ovsMock.EXPECT().SetIfaceOptions(gomock.Any(), gomock.Any(), gomock.Any()).
						Return(errors.New("failed to set interface options"))
				}, false),
			Entry("success patch interface",
				patchIfaceSpec,
				func() {
					ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
						Return(nil)
					ovsMock.EXPECT().SetIfaceOptions(gomock.Any(), gomock.Any(), gomock.Any()).
						Return(nil)
					ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
						Return(nil)
					ovsMock.EXPECT().SetIfaceOptions(gomock.Any(), gomock.Any(), gomock.Any()).
						Return(nil)
					ovsMock.EXPECT().SetIfaceExternalIDs(gomock.Any(), gomock.Any(), gomock.Eq(map[string]string{"dpf-id": client.ObjectKeyFromObject(si).String()})).
						Return(nil)
				}, true),
			Entry("failed to add patch port",
				patchIfaceSpec,
				func() {
					ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
						Return(errors.New("failed to add patch port"))
				}, false),
			Entry("failed to set interface options",
				patchIfaceSpec,
				func() {
					ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
						Return(nil)
					ovsMock.EXPECT().SetIfaceOptions(gomock.Any(), gomock.Any(), gomock.Any()).
						Return(errors.New("failed to set interface options"))
				},
				false),
			Entry("failed to add second port",
				patchIfaceSpec,
				func() {
					ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
						Return(nil)
					ovsMock.EXPECT().SetIfaceOptions(gomock.Any(), gomock.Any(), gomock.Any()).
						Return(nil)
					ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
						Return(errors.New("failed to add port"))
				}, false),
			Entry("failed to set interface external ids",
				patchIfaceSpec,
				func() {
					ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
						Return(nil)
					ovsMock.EXPECT().SetIfaceOptions(gomock.Any(), gomock.Any(), gomock.Any()).
						Return(nil)
					ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
						Return(nil)
					ovsMock.EXPECT().SetIfaceOptions(gomock.Any(), gomock.Any(), gomock.Any()).
						Return(nil)
					ovsMock.EXPECT().SetIfaceExternalIDs(gomock.Any(), gomock.Any(), gomock.Any()).
						Return(errors.New("failed to set interface external ids"))
				}, false),
			Entry("failed to set interface options",
				patchIfaceSpec,
				func() {
					ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
						Return(nil)
					ovsMock.EXPECT().SetIfaceOptions(gomock.Any(), gomock.Any(), gomock.Any()).
						Return(nil)
					ovsMock.EXPECT().AddPort(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
						Return(nil)
					ovsMock.EXPECT().SetIfaceOptions(gomock.Any(), gomock.Any(), gomock.Any()).
						Return(errors.New("failed to set interface options"))
				}, false),
			Entry("nothing to do for vlan type interface",
				vlanIfaceSpec,
				func() {},
				true),
			Entry("nothing to do for service type interface",
				serviceIfaceSpec,
				func() {},
				true),
		)
	})

	It("reconcile non existing object - consider as deleted", func() {
		nn := types.NamespacedName{
			Namespace: "non-existing",
			Name:      "non-existing",
		}

		result, err := sir.Reconcile(ctx, ctrl.Request{NamespacedName: nn})
		Expect(err).To(Succeed())
		Expect(result.RequeueAfter).To(BeZero())
	})

	Context("reconcile delete pf", func() {
		var deletedServiceInterface *dpuservicev1.ServiceInterface

		BeforeEach(func() {
			deletedServiceInterface = &dpuservicev1.ServiceInterface{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "deleted-service-interface",
					Namespace: ns.Name,
				},
				Spec: dpuservicev1.ServiceInterfaceSpec{
					Node:          &testNodeName,
					InterfaceType: dpuservicev1.InterfaceTypePF,
					PF: &dpuservicev1.PF{
						ID: 1,
					},
				},
			}

			controllerutil.AddFinalizer(deletedServiceInterface, ServiceInterfaceFinalizer)

			Expect(testClient.Create(ctx, deletedServiceInterface)).To(Succeed())
			cleanupObjects = append(cleanupObjects, deletedServiceInterface)
			Expect(testClient.Delete(ctx, deletedServiceInterface)).To(Succeed())
		})

		It("should return success", func() {

			networkHelperMock.EXPECT().GetPFRepresentorDPU(gomock.Any()).Return("pf0hpf", nil)
			ovsMock.EXPECT().DelPort(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

			result, err := sir.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
				Namespace: deletedServiceInterface.Namespace,
				Name:      deletedServiceInterface.Name,
			}})
			Expect(err).To(Succeed())
			Expect(result.RequeueAfter).To(BeZero())
		})

		It("should requeue if failed to delete port", func() {
			networkHelperMock.EXPECT().GetPFRepresentorDPU(gomock.Any()).Return("pf0hpf", nil)
			ovsMock.EXPECT().DelPort(gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("failed to delete port"))

			result, err := sir.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
				Namespace: deletedServiceInterface.Namespace,
				Name:      deletedServiceInterface.Name,
			}})
			Expect(err).To(Succeed())
			Expect(result.RequeueAfter).NotTo(BeZero())
		})

		It("should requeue if failed to get port name", func() {
			networkHelperMock.EXPECT().GetPFRepresentorDPU(gomock.Any()).Return("", fmt.Errorf("failed to get port name"))

			result, err := sir.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
				Namespace: deletedServiceInterface.Namespace,
				Name:      deletedServiceInterface.Name,
			}})
			Expect(err).To(Succeed())
			Expect(result.RequeueAfter).NotTo(BeZero())
		})
	})

	Context("reconcile delete vf", func() {
		var deletedServiceInterface *dpuservicev1.ServiceInterface

		BeforeEach(func() {
			deletedServiceInterface = &dpuservicev1.ServiceInterface{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "deleted-service-interface",
					Namespace: ns.Name,
				},
				Spec: dpuservicev1.ServiceInterfaceSpec{
					Node:          &testNodeName,
					InterfaceType: dpuservicev1.InterfaceTypeVF,
					VF: &dpuservicev1.VF{
						PFID: 0,
						VFID: 2,
					},
				},
			}

			controllerutil.AddFinalizer(deletedServiceInterface, ServiceInterfaceFinalizer)

			Expect(testClient.Create(ctx, deletedServiceInterface)).To(Succeed())
			cleanupObjects = append(cleanupObjects, deletedServiceInterface)
			Expect(testClient.Delete(ctx, deletedServiceInterface)).To(Succeed())
		})

		It("should return success", func() {

			networkHelperMock.EXPECT().GetVFRepresentorDPU(gomock.Any(), gomock.Any()).Return("pf0hvf2", nil)
			ovsMock.EXPECT().DelPort(gomock.Any(), gomock.Any(), "pf0hvf2").Return(nil)

			result, err := sir.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
				Namespace: deletedServiceInterface.Namespace,
				Name:      deletedServiceInterface.Name,
			}})
			Expect(err).To(Succeed())
			Expect(result.RequeueAfter).To(BeZero())
		})

		It("should requeue if failed to get port name", func() {
			networkHelperMock.EXPECT().GetVFRepresentorDPU(gomock.Any(), gomock.Any()).Return("", fmt.Errorf("failed to get port name"))

			result, err := sir.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
				Namespace: deletedServiceInterface.Namespace,
				Name:      deletedServiceInterface.Name,
			}})
			Expect(err).To(Succeed())
			Expect(result.RequeueAfter).NotTo(BeZero())
		})
	})

	Context("delete ovn port", func() {
		var deletedServiceInterface *dpuservicev1.ServiceInterface

		BeforeEach(func() {
			brOVN := "br-ovn"
			deletedServiceInterface = &dpuservicev1.ServiceInterface{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "deleted-service-interface",
					Namespace: ns.Name,
				},
				Spec: dpuservicev1.ServiceInterfaceSpec{
					Node:          &testNodeName,
					InterfaceType: dpuservicev1.InterfaceTypeOVN,
					OVN: &dpuservicev1.OVN{
						ExternalBridge: &brOVN,
					},
				},
			}

			controllerutil.AddFinalizer(deletedServiceInterface, ServiceInterfaceFinalizer)

			Expect(testClient.Create(ctx, deletedServiceInterface)).To(Succeed())
			cleanupObjects = append(cleanupObjects, deletedServiceInterface)
			Expect(testClient.Delete(ctx, deletedServiceInterface)).To(Succeed())
		})

		It("should return success", func() {
			// delete patch port between bridges
			ovsMock.EXPECT().DelPort(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
			ovsMock.EXPECT().DelPort(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

			result, err := sir.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
				Namespace: deletedServiceInterface.Namespace,
				Name:      deletedServiceInterface.Name,
			}})
			Expect(err).To(Succeed())
			Expect(result.RequeueAfter).To(BeZero())
		})

		It("should requeue if failed to delete port first patch port", func() {
			ovsMock.EXPECT().DelPort(gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("failed to delete port"))

			result, err := sir.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
				Namespace: deletedServiceInterface.Namespace,
				Name:      deletedServiceInterface.Name,
			}})
			Expect(err).To(Succeed())
			Expect(result.RequeueAfter).NotTo(BeZero())
		})

		It("should requeue if failed to delete port second patch port", func() {
			ovsMock.EXPECT().DelPort(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
			ovsMock.EXPECT().DelPort(gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("failed to delete port"))

			result, err := sir.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
				Namespace: deletedServiceInterface.Namespace,
				Name:      deletedServiceInterface.Name,
			}})
			Expect(err).To(Succeed())
			Expect(result.RequeueAfter).NotTo(BeZero())
		})
	})
})
