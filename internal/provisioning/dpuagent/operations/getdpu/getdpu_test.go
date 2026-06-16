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
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(provisioningv1.AddToScheme(s))
	return s
}

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
				UID:       "test-uid-123",
			},
			Spec: provisioningv1.DPUSpec{
				DPUNodeName:   "test-dpu-node",
				DPUDeviceName: "test-dpu-device",
				BFB:           ptr.To("bfb-test"),
				SerialNumber:  "test-dpu-serial-number",
				DPUFlavor:     "test-dpu-flavor",
			},
		}
		fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(expectedDPU).Build()

		operation := &GetLatestDPU{}
		operationCtx := operations.Context{
			Options: opts.Options{
				DPUNamespace: "test-ns",
				DPUName:      "test-dpu",
				DPUUID:       "test-uid-123",
			},
			Client: fakeClient,
		}
		Expect(operation.Execute(ctx, &operationCtx)).To(Succeed())
		Expect(operationCtx.LatestDPU).NotTo(BeNil())
		Expect(operationCtx.LatestDPU.Name).To(Equal("test-dpu"))
		Expect(operationCtx.LatestDPU.Namespace).To(Equal("test-ns"))
		Expect(operationCtx.LatestDPU.Spec.DPUNodeName).To(Equal("test-dpu-node"))
	})

	It("should return error when DPU UID does not match", func() {
		dpu := &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-dpu",
				Namespace: "test-ns",
				UID:       "different-uid",
			},
		}
		fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(dpu).Build()

		operation := &GetLatestDPU{}
		operationCtx := operations.Context{
			Options: opts.Options{
				DPUNamespace: "test-ns",
				DPUName:      "test-dpu",
				DPUUID:       "expected-uid",
			},
			Client: fakeClient,
		}
		err := operation.Execute(ctx, &operationCtx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("stale DPU object"))
		Expect(operationCtx.LatestDPU).To(BeNil())
	})

	It("should return error when client fails", func() {
		fakeClient := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()

		operation := &GetLatestDPU{}
		operationCtx := operations.Context{
			Options: opts.Options{
				DPUNamespace: "test-ns",
				DPUName:      "test-dpu",
			},
			Client: fakeClient,
		}
		err := operation.Execute(ctx, &operationCtx)
		Expect(err).To(HaveOccurred())
		_ = fmt.Sprintf("%v", err)
		Expect(operationCtx.LatestDPU).To(BeNil())
	})
})
