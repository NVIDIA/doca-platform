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

package getdpu

import (
	"context"
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GetLatestDPU Operation", func() {
	It("should never be skipped", func() {
		operation := &GetLatestDPU{}
		Expect(operation.ShouldSkip(&operations.Context{})).To(BeFalse())
	})

	It("should assign the LatestDPU in context on success", func() {
		expectedDPU := &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-dpu",
				Namespace: "test-ns",
			},
			Spec: provisioningv1.DPUSpec{
				DPUNodeName:   "test-dpu-node",
				DPUDeviceName: "test-dpu-device",
				BFB:           "bfb-test",
				SerialNumber:  "test-dpu-serial-number",
				DPUFlavor:     "test-dpu-flavor",
			},
		}

		operation := &GetLatestDPU{}
		operationCtx := operations.Context{
			Options: opts.Options{
				DPUNamespace: "test-ns",
				DPUName:      "test-dpu",
			},
			Client: &mockClient{
				getObjectFunc: func(execCtx context.Context, namespace, name string, obj client.Object) error {
					Expect(namespace).To(Equal("test-ns"))
					Expect(name).To(Equal("test-dpu"))
					dpu, ok := obj.(*provisioningv1.DPU)
					Expect(ok).To(BeTrue())
					expectedDPU.DeepCopyInto(dpu)
					return nil
				},
			},
		}
		Expect(operation.Execute(ctx, &operationCtx)).To(Succeed())
		Expect(operationCtx.LatestDPU).NotTo(BeNil())
		Expect(operationCtx.LatestDPU.Name).To(Equal("test-dpu"))
		Expect(operationCtx.LatestDPU.Namespace).To(Equal("test-ns"))
		Expect(operationCtx.LatestDPU.Spec).To(Equal(expectedDPU.Spec))
	})

	It("should return error when client fails", func() {
		operation := &GetLatestDPU{}
		operationCtx := operations.Context{
			Options: opts.Options{
				DPUNamespace: "test-ns",
				DPUName:      "test-dpu",
			},
			Client: &mockClient{
				getObjectFunc: func(execCtx context.Context, namespace, name string, obj client.Object) error {
					return fmt.Errorf("api server unavailable")
				},
			},
		}
		err := operation.Execute(ctx, &operationCtx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("api server unavailable"))
		Expect(operationCtx.LatestDPU).To(BeNil())
	})
})

type mockClient struct {
	getObjectFunc    func(execCtx context.Context, namespace, name string, obj client.Object) error
	updateStatusFunc func(execCtx context.Context, status provisioningv1.AgentStatus) error
	healthCheckFunc  func() error
}

func (m *mockClient) GetObject(execCtx context.Context, namespace, name string, obj client.Object) error {
	return m.getObjectFunc(execCtx, namespace, name, obj)
}

func (m *mockClient) UpdateStatus(execCtx context.Context, status provisioningv1.AgentStatus) error {
	return m.updateStatusFunc(execCtx, status)
}

func (m *mockClient) HealthCheck() error {
	return m.healthCheckFunc()
}
