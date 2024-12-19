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

package controllers

import (
	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/k8snetworkplumbingwg/multus-cni.v4/pkg/types"
)

var _ = Describe("NetworkSelectionElement", func() {
	DescribeTable("Validate NetworkSelectionElement",
		func(dpuServiceInterface *dpuservicev1.DPUServiceInterface, expected types.NetworkSelectionElement) {
			resp := newNetworkSelectionElement(dpuServiceInterface)
			Expect(resp).To(Equal(expected))
		},
		Entry("network without namespace", &dpuservicev1.DPUServiceInterface{
			Spec: dpuservicev1.DPUServiceInterfaceSpec{
				Template: dpuservicev1.ServiceInterfaceSetSpecTemplate{
					Spec: dpuservicev1.ServiceInterfaceSetSpec{
						Template: dpuservicev1.ServiceInterfaceSpecTemplate{
							Spec: dpuservicev1.ServiceInterfaceSpec{
								InterfaceType: dpuservicev1.InterfaceTypeService,
								Service: &dpuservicev1.ServiceDef{
									ServiceID:     "service-one",
									Network:       "mybrsfc",
									InterfaceName: "net1",
								},
							},
						},
					},
				},
			},
		}, types.NetworkSelectionElement{
			Name:             "mybrsfc",
			InterfaceRequest: "net1",
		}),
		Entry("network with namespace", &dpuservicev1.DPUServiceInterface{
			Spec: dpuservicev1.DPUServiceInterfaceSpec{
				Template: dpuservicev1.ServiceInterfaceSetSpecTemplate{
					Spec: dpuservicev1.ServiceInterfaceSetSpec{
						Template: dpuservicev1.ServiceInterfaceSpecTemplate{
							Spec: dpuservicev1.ServiceInterfaceSpec{
								InterfaceType: dpuservicev1.InterfaceTypeService,
								Service: &dpuservicev1.ServiceDef{
									ServiceID:     "service-one",
									Network:       "my-namespace/mybrsfc",
									InterfaceName: "net1",
								},
							},
						},
					},
				},
			},
		}, types.NetworkSelectionElement{
			Name:             "mybrsfc",
			Namespace:        "my-namespace",
			InterfaceRequest: "net1",
		}),
	)
	DescribeTable("Validate AddNetworkAnnotationToServiceDaemonSet",
		func(dpuService *dpuservicev1.DPUService, networkSelectionByInterface map[string]types.NetworkSelectionElement, expected *dpuservicev1.ServiceDaemonSetValues, expectedErr error) {
			resp, err := addNetworkAnnotationToServiceDaemonSet(dpuService, networkSelectionByInterface)
			if expectedErr != nil {
				Expect(err).To(HaveOccurred())
				Expect(err).To(Equal(expectedErr))
			} else {
				Expect(err).ToNot(HaveOccurred())
				Expect(resp).To(Equal(expected))
			}
		},
		Entry("networks is nil", &dpuservicev1.DPUService{}, nil, &dpuservicev1.ServiceDaemonSetValues{Annotations: map[string]string{}}, nil),
		Entry("networks is empty", &dpuservicev1.DPUService{}, map[string]types.NetworkSelectionElement{}, &dpuservicev1.ServiceDaemonSetValues{Annotations: map[string]string{}}, nil),
		Entry("DPUService contains annotation", &dpuservicev1.DPUService{
			Spec: dpuservicev1.DPUServiceSpec{
				ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
					Annotations: map[string]string{
						networkAnnotationKey: `[{"name":"mybrsfc","namespace":"my-namespace","interface":"net1"}]`,
					},
				},
			},
		}, nil, &dpuservicev1.ServiceDaemonSetValues{
			Annotations: map[string]string{
				networkAnnotationKey: `[{"name":"mybrsfc","namespace":"my-namespace","interface":"net1"}]`,
			},
		}, nil),
		Entry("networks contains network for interface", &dpuservicev1.DPUService{
			Spec: dpuservicev1.DPUServiceSpec{
				ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
					Annotations: map[string]string{
						networkAnnotationKey: `[{"name":"mybrsfc","namespace":"my-namespace","interface":"net1"}]`,
					},
				},
			},
		}, map[string]types.NetworkSelectionElement{
			"net1": {
				Name:             "mybrsfc",
				Namespace:        "my-namespace",
				InterfaceRequest: "net1",
			},
		}, &dpuservicev1.ServiceDaemonSetValues{
			Annotations: map[string]string{
				networkAnnotationKey: `[{"name":"mybrsfc","namespace":"my-namespace","interface":"net1","cni-args":null}]`,
			},
		}, nil),
		Entry("networks contains network with interface that conflicts, ServiceDaemonSet takes precedence", &dpuservicev1.DPUService{
			Spec: dpuservicev1.DPUServiceSpec{
				ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
					Annotations: map[string]string{
						networkAnnotationKey: `[{"name":"mybrsfc","namespace":"my-namespace","interface":"net1"},{"name":"iprequest","interface":"myip1",` +
							`"cni-args":{"allocateDefaultGateway":true,"poolNames":["pool1"],"poolType":"cidrpool"}}]`,
					},
				},
			},
		}, map[string]types.NetworkSelectionElement{
			"net1": {
				Name:             "myotherbrsfc",
				Namespace:        "my-namespace",
				InterfaceRequest: "net1",
				CNIArgs: &map[string]interface{}{
					"allocateDefaultGateway": true,
				},
			},
		}, &dpuservicev1.ServiceDaemonSetValues{
			Annotations: map[string]string{
				networkAnnotationKey: `[{"name":"iprequest","interface":"myip1","cni-args":{"allocateDefaultGateway":true,"poolNames":["pool1"],"poolType":"cidrpool"}},` +
					`{"name":"mybrsfc","namespace":"my-namespace","interface":"net1","cni-args":{"allocateDefaultGateway":true}}]`,
			},
		}, nil),
		Entry("networks contains network with interface that conflicts, reset cni args", &dpuservicev1.DPUService{
			Spec: dpuservicev1.DPUServiceSpec{
				ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
					Annotations: map[string]string{
						networkAnnotationKey: `[{"name":"mybrsfc","namespace":"my-namespace","interface":"net1","cni-args":{}},` +
							`{"name":"iprequest","interface":"myip1","cni-args":{"allocateDefaultGateway":true,"poolNames":["pool1"],"poolType":"cidrpool"}}]`,
					},
				},
			},
		}, map[string]types.NetworkSelectionElement{
			"net1": {
				Name:             "mybrsfc",
				Namespace:        "my-namespace",
				InterfaceRequest: "net1",
				CNIArgs: &map[string]interface{}{
					"allocateDefaultGateway": true,
				},
			},
		}, &dpuservicev1.ServiceDaemonSetValues{
			Annotations: map[string]string{
				networkAnnotationKey: `[{"name":"iprequest","interface":"myip1","cni-args":{"allocateDefaultGateway":true,"poolNames":["pool1"],"poolType":"cidrpool"}},` +
					`{"name":"mybrsfc","namespace":"my-namespace","interface":"net1","cni-args":{}}]`,
			},
		}, nil),
		Entry("merge 2 networks for different interfaces", &dpuservicev1.DPUService{
			Spec: dpuservicev1.DPUServiceSpec{
				ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
					Annotations: map[string]string{
						networkAnnotationKey: `[{"name":"mybrsfc","namespace":"my-namespace","interface":"net1"}]`,
					},
				},
			},
		}, map[string]types.NetworkSelectionElement{
			"net2": {
				Name:             "mybrsfc1",
				Namespace:        "my-namespace",
				InterfaceRequest: "net2",
			},
		}, &dpuservicev1.ServiceDaemonSetValues{
			Annotations: map[string]string{
				networkAnnotationKey: `[{"name":"mybrsfc","namespace":"my-namespace","interface":"net1","cni-args":null}` +
					`,{"name":"mybrsfc1","namespace":"my-namespace","interface":"net2","cni-args":null}]`,
			},
		}, nil),
		Entry("2 interfaces in the same network", &dpuservicev1.DPUService{
			Spec: dpuservicev1.DPUServiceSpec{
				ServiceDaemonSet: &dpuservicev1.ServiceDaemonSetValues{
					Annotations: map[string]string{
						networkAnnotationKey: `[{"name":"mybrsfc","namespace":"my-namespace","interface":"net1"}]`,
					},
				},
			},
		}, map[string]types.NetworkSelectionElement{
			"net3": {
				Name:             "mybrsfc",
				Namespace:        "my-namespace",
				InterfaceRequest: "net3",
			},
			"net2": {
				Name:             "mybrsfc",
				Namespace:        "my-namespace",
				InterfaceRequest: "net2",
			},
		}, &dpuservicev1.ServiceDaemonSetValues{
			Annotations: map[string]string{
				networkAnnotationKey: `[{"name":"mybrsfc","namespace":"my-namespace","interface":"net1","cni-args":null}` +
					`,{"name":"mybrsfc","namespace":"my-namespace","interface":"net2","cni-args":null}` +
					`,{"name":"mybrsfc","namespace":"my-namespace","interface":"net3","cni-args":null}]`,
			},
		}, nil),
	)
})
