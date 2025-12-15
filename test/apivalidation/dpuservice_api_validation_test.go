/*
Copyright 2025 NVIDIA

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

package apivalidation_test

import (
	"fmt"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("dpuservice API Validation", func() {
	var testNs *corev1.Namespace
	var cleanupObjs []client.Object

	BeforeEach(func() {
		cleanupObjs = []client.Object{}
		testNs = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "test-api-validation-"}}
		Expect(testClient.Create(ctx, testNs)).To(Succeed())
	})

	AfterEach(func() {
		Expect(testutils.CleanupAndWait(ctx, testClient, cleanupObjs...)).To(Succeed())
		Expect(testClient.Delete(ctx, testNs)).To(Succeed())
	})

	Context("ServiceInterface", func() {
		Context("ServiceInterface of type Service", func() {
			Context("validate InterfaceName length", func() {
				var si *dpuservicev1.ServiceInterface

				BeforeEach(func() {
					si = &dpuservicev1.ServiceInterface{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test",
							Namespace: testNs.Name,
						},
						Spec: dpuservicev1.ServiceInterfaceSpec{
							InterfaceType: dpuservicev1.InterfaceTypeService,
							Service: &dpuservicev1.ServiceDef{
								ServiceID:     "test",
								Network:       "test",
								InterfaceName: "test",
							},
						},
					}
				})

				It("should not allow empty InterfaceName", func() {
					si.Spec.Service.InterfaceName = ""
					Expect(testClient.Create(ctx, si)).ToNot(Succeed())
				})

				It("should not allow InterfaceName to be too long", func() {
					si.Spec.Service.InterfaceName = utilrand.String(16)
					Expect(testClient.Create(ctx, si)).ToNot(Succeed())
				})

				It("should allow InterfaceName to be the maximum length", func() {
					si.Spec.Service.InterfaceName = utilrand.String(15)
					Expect(testClient.Create(ctx, si)).To(Succeed())
					cleanupObjs = append(cleanupObjs, si)
				})
			})
		})
		Context("validate ServiceInterface VirtualNetwork immutability", func() {
			Context("validate ServiceInterface VirtualNetwork immutability - VF", func() {
				var si *dpuservicev1.ServiceInterface

				BeforeEach(func() {
					si = &dpuservicev1.ServiceInterface{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test",
							Namespace: testNs.Name,
						},
						Spec: dpuservicev1.ServiceInterfaceSpec{
							InterfaceType: dpuservicev1.InterfaceTypeVF,
							VF: &dpuservicev1.VF{
								VFID: 0,
								PFID: 0,
							},
						},
					}
				})

				It("should not allow updating VirtualNetwork if not specified", func() {
					Expect(testClient.Create(ctx, si)).To(Succeed())
					cleanupObjs = append(cleanupObjs, si)
					si.Spec.VF.VirtualNetwork = ptr.To("vnet")
					Expect(testClient.Update(ctx, si)).ToNot(Succeed())
				})

				It("should not allow updating VirtualNetwork if specified", func() {
					si.Spec.VF.VirtualNetwork = ptr.To("vnet")
					Expect(testClient.Create(ctx, si)).To(Succeed())
					cleanupObjs = append(cleanupObjs, si)
					si.Spec.VF.VirtualNetwork = ptr.To("otherVnet")
					Expect(testClient.Update(ctx, si)).ToNot(Succeed())
				})

				It("should not allow removing VirtualNetwork if specified", func() {
					si.Spec.VF.VirtualNetwork = ptr.To("vnet")
					Expect(testClient.Create(ctx, si)).To(Succeed())
					cleanupObjs = append(cleanupObjs, si)
					si.Spec.VF.VirtualNetwork = nil
					Expect(testClient.Update(ctx, si)).ToNot(Succeed())
				})
			})

			Context("validate ServiceInterface VirtualNetwork immutability - PF", func() {
				var si *dpuservicev1.ServiceInterface

				BeforeEach(func() {
					si = &dpuservicev1.ServiceInterface{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test",
							Namespace: testNs.Name,
						},
						Spec: dpuservicev1.ServiceInterfaceSpec{
							InterfaceType: dpuservicev1.InterfaceTypePF,
							PF: &dpuservicev1.PF{
								ID: 0,
							},
						},
					}
				})

				It("should not allow updating VirtualNetwork if not specified", func() {
					Expect(testClient.Create(ctx, si)).To(Succeed())
					cleanupObjs = append(cleanupObjs, si)
					si.Spec.PF.VirtualNetwork = ptr.To("vnet")
					Expect(testClient.Update(ctx, si)).ToNot(Succeed())
				})

				It("should not allow updating VirtualNetwork if specified", func() {
					si.Spec.PF.VirtualNetwork = ptr.To("vnet")
					Expect(testClient.Create(ctx, si)).To(Succeed())
					cleanupObjs = append(cleanupObjs, si)
					si.Spec.PF.VirtualNetwork = ptr.To("otherVnet")
					Expect(testClient.Update(ctx, si)).ToNot(Succeed())
				})

				It("should not allow removing VirtualNetwork if specified", func() {
					si.Spec.PF.VirtualNetwork = ptr.To("vnet")
					Expect(testClient.Create(ctx, si)).To(Succeed())
					cleanupObjs = append(cleanupObjs, si)
					si.Spec.PF.VirtualNetwork = nil
					Expect(testClient.Update(ctx, si)).ToNot(Succeed())
				})
			})

			Context("validate ServiceInterface VirtualNetwork immutability - Service", func() {
				var si *dpuservicev1.ServiceInterface

				BeforeEach(func() {
					si = &dpuservicev1.ServiceInterface{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test",
							Namespace: testNs.Name,
						},
						Spec: dpuservicev1.ServiceInterfaceSpec{
							InterfaceType: dpuservicev1.InterfaceTypeService,
							Service: &dpuservicev1.ServiceDef{
								ServiceID:     "test",
								Network:       "test",
								InterfaceName: "iface",
							},
						},
					}
				})

				It("should not allow updating VirtualNetwork if not specified", func() {
					Expect(testClient.Create(ctx, si)).To(Succeed())
					cleanupObjs = append(cleanupObjs, si)
					si.Spec.Service.VirtualNetwork = ptr.To("vnet")
					Expect(testClient.Update(ctx, si)).ToNot(Succeed())
				})

				It("should not allow updating VirtualNetwork if specified", func() {
					si.Spec.Service.VirtualNetwork = ptr.To("vnet")
					Expect(testClient.Create(ctx, si)).To(Succeed())
					cleanupObjs = append(cleanupObjs, si)
					si.Spec.Service.VirtualNetwork = ptr.To("otherVnet")
					Expect(testClient.Update(ctx, si)).ToNot(Succeed())
				})

				It("should not allow removing VirtualNetwork if specified", func() {
					si.Spec.Service.VirtualNetwork = ptr.To("vnet")
					Expect(testClient.Create(ctx, si)).To(Succeed())
					cleanupObjs = append(cleanupObjs, si)
					si.Spec.Service.VirtualNetwork = nil
					Expect(testClient.Update(ctx, si)).ToNot(Succeed())
				})
			})
		})
	})
})

var _ = Describe("API Validations for DPUDeployment related objects", func() {
	var testNS *corev1.Namespace
	BeforeEach(func() {
		By("Creating the namespaces")
		testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-"}}
		Expect(testClient.Create(ctx, testNS)).To(Succeed())
		DeferCleanup(testClient.Delete, ctx, testNS)
	})
	Context("When checking the DPUServiceConfiguration API validations", func() {
		DescribeTable("Validates the interfaces and deployInCluster correctly", func(deployInCluster *bool, hasInterfaces bool, expectError bool) {
			dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
			dpuServiceConfiguration.Spec.ServiceConfiguration.DeployInCluster = deployInCluster
			if !hasInterfaces {
				dpuServiceConfiguration.Spec.Interfaces = nil
			}

			err := testClient.Create(ctx, dpuServiceConfiguration)
			if expectError {
				Expect(err).To(HaveOccurred())
			} else {
				Expect(err).ToNot(HaveOccurred())
			}
		},
			Entry("valid config - without specifying deployInCluster and with interfaces", nil, true, false),
			Entry("valid config - without specifying deployInCluster and without interfaces", nil, false, false),
			Entry("valid config - with deployInCluster=false and with interfaces", ptr.To(false), true, false),
			Entry("valid config - with deployInCluster=false and without interfaces", ptr.To(false), false, false),
			Entry("valid config - with deployInCluster=true and without interfaces", ptr.To(true), false, false),
			Entry("invalid config - with deployInCluster=true and with interfaces", ptr.To(true), true, true),
		)
		DescribeTable("Validates the ConfigPorts and deployInCluster correctly", func(deployInCluster *bool, hasConfigPorts bool, expectError bool) {
			dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
			dpuServiceConfiguration.Spec.Interfaces = nil
			dpuServiceConfiguration.Spec.ServiceConfiguration.DeployInCluster = deployInCluster
			if hasConfigPorts {
				dpuServiceConfiguration.Spec.ServiceConfiguration.ConfigPorts = &dpuservicev1.ConfigPorts{
					ServiceType: corev1.ServiceTypeNodePort,
					Ports:       []dpuservicev1.ConfigPort{},
				}
			}

			err := testClient.Create(ctx, dpuServiceConfiguration)
			if expectError {
				Expect(err).To(HaveOccurred())
			} else {
				Expect(err).ToNot(HaveOccurred())
			}
		},
			Entry("valid config - without specifying deployInCluster and with ConfigPorts", nil, true, false),
			Entry("valid config - without specifying deployInCluster and without ConfigPorts", nil, false, false),
			Entry("valid config - with deployInCluster=false and with ConfigPorts", ptr.To(false), true, false),
			Entry("valid config - with deployInCluster=false and without ConfigPorts", ptr.To(false), false, false),
			Entry("valid config - with deployInCluster=true and without ConfigPorts", ptr.To(true), false, false),
			Entry("invalid config - with deployInCluster=true and with ConfigPorts", ptr.To(true), true, true),
		)
		DescribeTable("Validates the restricted label/annotation in serviceDaemonSet", func(labels map[string]string, annotations map[string]string, expectError bool) {
			dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
			dpuServiceConfiguration.Spec.ServiceConfiguration.ServiceDaemonSet.Labels = labels
			dpuServiceConfiguration.Spec.ServiceConfiguration.ServiceDaemonSet.Annotations = annotations
			err := testClient.Create(ctx, dpuServiceConfiguration)
			if expectError {
				Expect(err).To(HaveOccurred())
			} else {
				Expect(err).ToNot(HaveOccurred())
			}
		},
			Entry("no labels no annotations", nil, nil, false),
			Entry("good labels good annotations", map[string]string{"l1": "v1"}, map[string]string{"a1": "v1"}, false),
			Entry("good labels", map[string]string{"l1": "v1"}, nil, false),
			Entry("good annotations", nil, map[string]string{"a1": "v1"}, false),
			Entry("exception labels", map[string]string{"svc.dpu.nvidia.com/custom-flows": ""}, nil, false),
			Entry("bad labels with exception labels included", map[string]string{"dpu.nvidia.com": "some", "svc.dpu.nvidia.com/custom-flows": ""}, nil, true),
			Entry("bad labels", map[string]string{"dpu.nvidia.com": "some"}, nil, true),
			Entry("bad labels - real key", map[string]string{"svc.dpu.nvidia.com/consumed-by-dpudeployment": ""}, nil, true),
			Entry("bad annotations", nil, map[string]string{"dpu.nvidia.com": "some"}, true),
			Entry("bad annotations - real key", nil, map[string]string{"svc.dpu.nvidia.com/dpuservice-version": ""}, true),
			Entry("bad labels good annotations", map[string]string{"dpu.nvidia.com": "some"}, map[string]string{"a1": "v1"}, true),
			Entry("good labels bad annotations", map[string]string{"l1": "v1"}, map[string]string{"dpu.nvidia.com": "some"}, true),
		)
		It("should not create the DPUServiceConfiguration with deploymentServiceName exceeding the maximum length", func() {
			dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
			dpuServiceConfiguration.Spec.DeploymentServiceName = utilrand.String(29)
			Expect(testClient.Create(ctx, dpuServiceConfiguration)).ToNot(Succeed())
		})
		It("should allow creation of DPUServiceConfiguration with deploymentServiceName at maximum length", func() {
			dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
			dpuServiceConfiguration.Spec.DeploymentServiceName = utilrand.String(28)
			Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
		})
		It("should not create the DPUServiceConfiguration with Interface Name exceeding the maximum length", func() {
			dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
			dpuServiceConfiguration.Spec.Interfaces[0].Name = utilrand.String(16)
			Expect(testClient.Create(ctx, dpuServiceConfiguration)).ToNot(Succeed())
		})
		It("should allow creation of DPUServiceConfiguration with Interface Name at maximum length", func() {
			dpuServiceConfiguration := getMinimalDPUServiceConfiguration(testNS.Name)
			dpuServiceConfiguration.Spec.Interfaces[0].Name = utilrand.String(15)
			Expect(testClient.Create(ctx, dpuServiceConfiguration)).To(Succeed())
		})
	})
	Context("When checking the DPUDeployment API validations", func() {
		It("should not create the DPUDeployment if system annotations are present", func() {
			dpuDeployment := getMinimalDPUDeployment(testNS.Name)
			dpuDeployment.Spec.DPUs.DPUSets = []dpuservicev1.DPUSet{
				{
					NameSuffix: "dpuset1",
					DPUAnnotations: map[string]string{
						"dpu.nvidia.com": "not allowed",
					},
				},
			}
			Expect(testClient.Create(ctx, dpuDeployment)).ToNot(Succeed())
			dpuDeployment.Spec.DPUs.DPUSets[0].DPUAnnotations = map[string]string{
				"annKey": "annVal",
			}
			Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
			dpuDeployment.Spec.DPUs.DPUSets[0].DPUAnnotations = map[string]string{
				"anything.dpu.nvidia.com": "not allowed",
			}
			Expect(testClient.Create(ctx, dpuDeployment)).ToNot(Succeed())
			dpuDeployment.Spec.DPUs.DPUSets[0].DPUAnnotations = map[string]string{
				"anything.dpu.nvidia.com/anything": "not allowed",
			}
			Expect(testClient.Create(ctx, dpuDeployment)).ToNot(Succeed())
		})
		It("should not create the DPUDeployment if name exceeds the maximum length", func() {
			dpuDeployment := getMinimalDPUDeployment(testNS.Name)
			dpuDeployment.Name = utilrand.String(21)
			Expect(testClient.Create(ctx, dpuDeployment)).ToNot(Succeed())
		})
		It("should not create the DPUDeployment if spec.dpus.dpuSets.nameSuffix exceeds the maximum length", func() {
			dpuDeployment := getMinimalDPUDeployment(testNS.Name)
			dpuDeployment.Spec.DPUs.DPUSets = []dpuservicev1.DPUSet{
				{
					NameSuffix: utilrand.String(25),
				},
			}
			Expect(testClient.Create(ctx, dpuDeployment)).ToNot(Succeed())
		})
		It("should not create the DPUDeployment if spec.dpus.dpuSets.nameSuffix is duplicated", func() {
			dpuDeployment := getMinimalDPUDeployment(testNS.Name)
			dpuDeployment.Spec.DPUs.DPUSets = []dpuservicev1.DPUSet{
				{
					NameSuffix: "dpuset1",
				},
				{
					NameSuffix: "dpuset1",
				},
			}
			Expect(testClient.Create(ctx, dpuDeployment)).ToNot(Succeed())
		})
		It("should not create the DPUDeployment if spec.services has key that exceeds the maximum length", func() {
			dpuDeployment := getMinimalDPUDeployment(testNS.Name)
			dpuDeployment.Spec.Services[utilrand.String(29)] = dpuservicev1.DPUDeploymentServiceConfiguration{}
			Expect(testClient.Create(ctx, dpuDeployment)).ToNot(Succeed())
		})
		It("should not create the DPUDeployment if spec.serviceChains references service with name that exceeds the maximum length", func() {
			dpuDeployment := getMinimalDPUDeployment(testNS.Name)
			dpuDeployment.Spec.ServiceChains.Switches[0].Ports[0].Service.Name = utilrand.String(29)
			Expect(testClient.Create(ctx, dpuDeployment)).ToNot(Succeed())
		})
		It("should not create the DPUDeployment if spec.serviceChains references service that has interfaceName that exceeds the maximum length", func() {
			dpuDeployment := getMinimalDPUDeployment(testNS.Name)
			dpuDeployment.Spec.ServiceChains.Switches[0].Ports[0].Service.InterfaceName = utilrand.String(16)
			Expect(testClient.Create(ctx, dpuDeployment)).ToNot(Succeed())
		})
		It("should create the DPUDeployment if spec.serviceChains references service that has interfaceName at maximum length", func() {
			dpuDeployment := getMinimalDPUDeployment(testNS.Name)
			dpuDeployment.Spec.ServiceChains.Switches[0].Ports[0].Service.InterfaceName = utilrand.String(15)
			Expect(testClient.Create(ctx, dpuDeployment)).To(Succeed())
		})
		DescribeTable("Validates creation of DPUDeployment with various spec.services configurations", func(services map[string]dpuservicev1.DPUDeploymentServiceConfiguration, expectError bool) {
			dpuDeployment := getMinimalDPUDeployment(testNS.Name)
			dpuDeployment.Spec.Services = services
			err := testClient.Create(ctx, dpuDeployment)
			if expectError {
				Expect(err).To(HaveOccurred())
			} else {
				Expect(err).ToNot(HaveOccurred())
			}
		},
			Entry("spec.services is nil", nil, true),
			Entry("spec.services is empty", make(map[string]dpuservicev1.DPUDeploymentServiceConfiguration), true),
			Entry("spec.services has one service", map[string]dpuservicev1.DPUDeploymentServiceConfiguration{"service-1": {ServiceTemplate: "service-1", ServiceConfiguration: "service-1"}}, false),
			Entry("spec.services has 50 services", func() map[string]dpuservicev1.DPUDeploymentServiceConfiguration {
				o := make(map[string]dpuservicev1.DPUDeploymentServiceConfiguration)

				for i := range 50 {
					serviceName := fmt.Sprintf("service-%d", i)
					o[serviceName] = dpuservicev1.DPUDeploymentServiceConfiguration{ServiceTemplate: serviceName, ServiceConfiguration: serviceName}
				}
				return o
			}(), false),
			Entry("spec.services has 51 services", func() map[string]dpuservicev1.DPUDeploymentServiceConfiguration {
				o := make(map[string]dpuservicev1.DPUDeploymentServiceConfiguration)

				for i := range 51 {
					serviceName := fmt.Sprintf("service-%d", i)
					o[serviceName] = dpuservicev1.DPUDeploymentServiceConfiguration{ServiceTemplate: serviceName, ServiceConfiguration: serviceName}
				}
				return o
			}(), true),
		)
		DescribeTable("Validates mutual exclusivity of deprecated and new selector fields", func(dpuSet dpuservicev1.DPUSet, expectError bool) {
			dpuDeployment := getMinimalDPUDeployment(testNS.Name)
			dpuDeployment.Spec.DPUs.DPUSets = []dpuservicev1.DPUSet{dpuSet}
			err := testClient.Create(ctx, dpuDeployment)
			if expectError {
				Expect(err).To(HaveOccurred())
			} else {
				Expect(err).ToNot(HaveOccurred())
			}
		},
			Entry("both nodeSelector and dpuNodeSelector are specified",
				dpuservicev1.DPUSet{
					NameSuffix: "dpuset1",
					NodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"nodekey1": "nodevalue1",
						},
					},
					DPUNodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"nodekey2": "nodevalue2",
						},
					},
				}, true),
			Entry("only nodeSelector is specified",
				dpuservicev1.DPUSet{
					NameSuffix: "dpuset1",
					NodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"nodekey1": "nodevalue1",
						},
					},
				}, false),
			Entry("only dpuNodeSelector is specified",
				dpuservicev1.DPUSet{
					NameSuffix: "dpuset1",
					DPUNodeSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"nodekey1": "nodevalue1",
						},
					},
				}, false),
			Entry("both dpuSelector and dpuDeviceSelector are specified",
				dpuservicev1.DPUSet{
					NameSuffix: "dpuset1",
					DPUSelector: map[string]string{
						"dpukey1": "dpuvalue1",
					},
					DPUDeviceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"dpukey2": "dpuvalue2",
						},
					},
				}, true),
			Entry("only dpuSelector is specified",
				dpuservicev1.DPUSet{
					NameSuffix: "dpuset1",
					DPUSelector: map[string]string{
						"dpukey1": "dpuvalue1",
					},
				}, false),
			Entry("only dpuDeviceSelector is specified",
				dpuservicev1.DPUSet{
					NameSuffix: "dpuset1",
					DPUDeviceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"dpukey1": "dpuvalue1",
						},
					},
				}, false),
		)
	})
})

func getMinimalDPUDeployment(namespace string) *dpuservicev1.DPUDeployment {
	return &dpuservicev1.DPUDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dpudeployment",
			Namespace: namespace,
		},
		Spec: dpuservicev1.DPUDeploymentSpec{
			DPUs: dpuservicev1.DPUs{
				BFB:    "somebfb",
				Flavor: "someflavor",
			},
			Services: map[string]dpuservicev1.DPUDeploymentServiceConfiguration{
				"someservice": {
					ServiceTemplate:      "sometemplate",
					ServiceConfiguration: "someconfiguration",
				},
			},
			ServiceChains: &dpuservicev1.ServiceChains{
				UpgradePolicy: dpuservicev1.UpgradePolicy{
					ApplyNodeEffect: ptr.To(false),
				},
				Switches: []dpuservicev1.DPUDeploymentSwitch{
					{
						Ports: []dpuservicev1.DPUDeploymentPort{
							{
								Service: &dpuservicev1.DPUDeploymentService{
									InterfaceName: "someinterface",
									Name:          "someservice",
								},
							},
						},
					},
				},
			},
		},
	}
}

func getMinimalDPUServiceConfiguration(namespace string) *dpuservicev1.DPUServiceConfiguration {
	serviceConfig := getMinimalDPUServiceConfigurationWithoutUpgradePolicy(namespace)
	serviceConfig.Spec.UpgradePolicy = dpuservicev1.UpgradePolicy{
		ApplyNodeEffect: ptr.To(false),
	}
	return serviceConfig
}

func getMinimalDPUServiceConfigurationWithoutUpgradePolicy(namespace string) *dpuservicev1.DPUServiceConfiguration {
	return &dpuservicev1.DPUServiceConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "someconfiguration",
			Namespace: namespace,
		},
		Spec: dpuservicev1.DPUServiceConfigurationSpec{
			DeploymentServiceName: "someservice",
			Interfaces: []dpuservicev1.ServiceInterfaceTemplate{
				{
					Name:    "someinterface",
					Network: "somenad",
				},
			},
		},
	}
}
