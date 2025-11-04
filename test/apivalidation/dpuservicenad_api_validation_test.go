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

//nolint:goconst
package apivalidation_test

import (
	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

var _ = Describe("DPUServiceNAD API Validation", func() {
	var testNS *corev1.Namespace
	BeforeEach(func() {
		By("Creating the namespace")
		testNS = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "testns-"}}
		Expect(testClient.Create(ctx, testNS)).To(Succeed())
		DeferCleanup(testClient.Delete, ctx, testNS)
	})

	Context("When checking the DPUServiceNAD API validations", func() {
		DescribeTable("Validates resourceType correctly",
			func(resourceType string, expectError bool) {
				nad := getMinimalDPUServiceNAD(testNS.Name)
				nad.Spec.ResourceType = resourceType

				err := testClient.Create(ctx, nad)
				if expectError {
					Expect(err).To(HaveOccurred())
				} else {
					DeferCleanup(testClient.Delete, ctx, nad)
					Expect(err).ToNot(HaveOccurred())
				}
			},
			Entry("valid config using vf", "vf", false),
			Entry("valid config using sf", "sf", false),
			Entry("valid config using veth", "veth", false),
			Entry("invalid config - invalid resourceType", "invalid", true),
		)

		DescribeTable("Validates chainedCNIs plugin types correctly",
			func(chainedCNIs []dpuservicev1.CNIPlugin, expectError bool) {
				nad := getMinimalDPUServiceNAD(testNS.Name)
				nad.Spec.ChainedCNIs = chainedCNIs

				err := testClient.Create(ctx, nad)
				if expectError {
					Expect(err).To(HaveOccurred())
				} else {
					DeferCleanup(testClient.Delete, ctx, nad)
					Expect(err).ToNot(HaveOccurred())
				}
			},
			Entry("valid config - single rdma plugin", []dpuservicev1.CNIPlugin{{Type: ptr.To("rdma")}}, false),
			Entry("valid config - multiple rdma plugins", []dpuservicev1.CNIPlugin{
				{Type: ptr.To("rdma")},
				{Type: ptr.To("rdma")},
			}, false),
			Entry("invalid config - invalid plugin type", []dpuservicev1.CNIPlugin{{Type: ptr.To("invalid")}}, true),
		)

		It("should create DPUServiceNAD with all optional fields", func() {
			nad := getMinimalDPUServiceNAD(testNS.Name)
			nad.Spec.Bridge = "br-test"
			nad.Spec.ServiceMTU = 9000
			nad.Spec.IPAM = true
			nad.Spec.ChainedCNIs = []dpuservicev1.CNIPlugin{
				{Type: ptr.To("rdma")},
			}
			Expect(testClient.Create(ctx, nad)).To(Succeed())
			Expect(nad.Spec.Bridge).To(Equal("br-test"))
			Expect(nad.Spec.ServiceMTU).To(Equal(9000))
			Expect(nad.Spec.IPAM).To(BeTrue())
			Expect(nad.Spec.ChainedCNIs).To(HaveLen(1))
			Expect(*nad.Spec.ChainedCNIs[0].Type).To(Equal("rdma"))
			DeferCleanup(testClient.Delete, ctx, nad)
		})
	})
})

func getMinimalDPUServiceNAD(namespace string) *dpuservicev1.DPUServiceNAD {
	return &dpuservicev1.DPUServiceNAD{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-nad",
			Namespace: namespace,
		},
		Spec: dpuservicev1.DPUServiceNADSpec{
			ResourceType: "sf",
		},
	}
}
