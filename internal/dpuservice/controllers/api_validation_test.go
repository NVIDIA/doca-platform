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

package controllers

import (
	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("API Validation", func() {
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
