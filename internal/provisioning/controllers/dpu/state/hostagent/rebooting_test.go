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

package hostagent

import (
	"context"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("Rebooting", func() {
	var (
		ctx       context.Context
		namespace string
	)

	BeforeEach(func() {
		ctx = context.Background()
		namespace = "default"
	})

	DescribeTable("should accept host-agent reboot methods in the HostAgent handler",
		func(method provisioningv1.NodeRebootMethod) {
			dpuNode := testDPUNode(namespace, "test-dpunode", method)
			dpu := testDPU(namespace, "test-dpu", dpuNode.Name)
			cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondInterfaceInitialized, "", ""))

			status, err := Rebooting(ctx, dpu, testControllerContext(dpuNode))

			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPURebooting))
		},
		Entry("hostAgent", provisioningv1.NodeRebootMethod{HostAgent: &provisioningv1.HostAgent{}}),
		Entry("gNOI", provisioningv1.NodeRebootMethod{GNOI: &provisioningv1.GNOI{}}), //nolint:staticcheck // GNOI remains valid for the HostAgent handler.
	)

	It("should reject hostless DPUs in the HostAgent handler", func() {
		dpuNode := testDPUNode(namespace, "test-dpunode", provisioningv1.NodeRebootMethod{None: &provisioningv1.None{}})
		dpu := testDPU(namespace, "test-dpu", dpuNode.Name)
		dpu.Status.Hostless = true
		cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondInterfaceInitialized, "", ""))

		status, err := Rebooting(ctx, dpu, testControllerContext(dpuNode))

		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUError))
		Expect(status.Conditions).To(ContainElement(And(
			HaveField("Type", provisioningv1.DPUCondRebooted.String()),
			HaveField("Status", metav1.ConditionFalse),
			HaveField("Reason", "HostlessRequiresRedfish"),
		)))
	})

	It("should move to DPUError for unsupported nodeRebootMethod", func() {
		dpuNode := testDPUNode(namespace, "test-dpunode", provisioningv1.NodeRebootMethod{})
		dpu := testDPU(namespace, "test-dpu", dpuNode.Name)
		cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondInterfaceInitialized, "", ""))

		status, err := Rebooting(ctx, dpu, testControllerContext(dpuNode))

		Expect(err).NotTo(HaveOccurred())
		Expect(status.Phase).To(Equal(provisioningv1.DPUError))
		Expect(status.Conditions).To(ContainElement(And(
			HaveField("Type", provisioningv1.DPUCondRebooted.String()),
			HaveField("Status", metav1.ConditionFalse),
			HaveField("Reason", "UnsupportedNodeRebootMethod"),
		)))
	})
})

func testDPU(namespace, name, dpuNodeName string) *provisioningv1.DPU {
	return &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: provisioningv1.DPUSpec{
			DPUNodeName: dpuNodeName,
		},
		Status: provisioningv1.DPUStatus{
			Phase:   provisioningv1.DPURebooting,
			DPUMode: provisioningv1.DpuMode,
		},
	}
}

func testDPUNode(namespace, name string, rebootMethod provisioningv1.NodeRebootMethod) *provisioningv1.DPUNode {
	return &provisioningv1.DPUNode{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: provisioningv1.DPUNodeSpec{
			NodeRebootMethod: &rebootMethod,
		},
	}
}

func testControllerContext(objects ...runtime.Object) *dutil.ControllerContext {
	scheme := runtime.NewScheme()
	Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
	return &dutil.ControllerContext{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithRuntimeObjects(objects...).
			Build(),
		Scheme: scheme,
	}
}
