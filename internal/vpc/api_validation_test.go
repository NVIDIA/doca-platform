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

package vpc_test

import (
	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
	testutils "github.com/nvidia/doca-platform/test/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	Context("IsolationClass", func() {
		It("validate IsolationClass spec immutability", func() {
			ic := &vpcv1.IsolationClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
				},
				Spec: vpcv1.IsolationClassSpec{
					Provisioner: "some.provisioner",
				},
			}
			Expect(testClient.Create(ctx, ic)).To(Succeed())
			cleanupObjs = append(cleanupObjs, ic)

			ic.Spec.Provisioner = "some.other.provisioner"
			Expect(testClient.Update(ctx, ic)).ToNot(Succeed())
		})
	})

	Context("DPUVPC", func() {
		It("validate DPUVPC spec immutability", func() {
			dpuvpc := &vpcv1.DPUVPC{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: testNs.Name,
				},
				Spec: vpcv1.DPUVPCSpec{
					Tenant:             "test",
					IsolationClassName: "thatClass",
					InterNetworkAccess: false,
				},
			}
			Expect(testClient.Create(ctx, dpuvpc)).To(Succeed())
			cleanupObjs = append(cleanupObjs, dpuvpc)

			dpuvpc.Spec.IsolationClassName = "otherClass"
			Expect(testClient.Update(ctx, dpuvpc)).ToNot(Succeed())
		})
	})

	Context("DPUVirtualNetwork", func() {
		var dpuvn *vpcv1.DPUVirtualNetwork

		BeforeEach(func() {
			dpuvn = &vpcv1.DPUVirtualNetwork{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: testNs.Name,
				},
				Spec: vpcv1.DPUVirtualNetworkSpec{
					VPCName:          "foo",
					ExternallyRouted: false,
					Type:             vpcv1.BridgedVirtualNetworkType,
					BridgedNetwork:   &vpcv1.BridgedNetworkSpec{},
				},
			}
		})

		It("validate DPUVirtualNetwork spec immutability", func() {
			Expect(testClient.Create(ctx, dpuvn)).To(Succeed())
			cleanupObjs = append(cleanupObjs, dpuvn)
			dpuvn.Spec.VPCName = "bar"
			Expect(testClient.Update(ctx, dpuvn)).ToNot(Succeed())
		})

		It("validate DPUVirtualNetwork spec bridged network requirement", func() {
			dpuvn.Spec.Type = vpcv1.BridgedVirtualNetworkType
			dpuvn.Spec.BridgedNetwork = nil
			Expect(testClient.Create(ctx, dpuvn)).ToNot(Succeed())
		})
	})
})
