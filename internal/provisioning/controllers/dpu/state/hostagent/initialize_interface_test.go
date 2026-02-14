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
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestInitializeInterface(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "InitializeInterface Suite")
}

var _ = Describe("InitializeInterface", func() {
	var (
		ctx        context.Context
		dpu        *provisioningv1.DPU
		ctrlCtx    *dutil.ControllerContext
		defaultDPU = &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-dpu",
				Namespace: "default",
			},
			Spec: provisioningv1.DPUSpec{
				DPUNodeName:   "test-node",
				DPUDeviceName: "test-device",
				BFB:           "test-bfb",
				SerialNumber:  "test-serial",
				DPUFlavor:     "test-flavor",
			},
			Status: provisioningv1.DPUStatus{
				Phase: provisioningv1.DPUInitializeInterface,
			},
		}
	)

	BeforeEach(func() {
		ctx = context.Background()
		dpu = defaultDPU.DeepCopy()
		ctrlCtx = &dutil.ControllerContext{}
	})

	Context("when DPU is being deleted", func() {
		It("should set phase to DPUDeleting", func() {
			now := metav1.Now()
			dpu.DeletionTimestamp = &now

			status, err := InitializeInterface(ctx, dpu, ctrlCtx)

			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUDeleting))
		})
	})

	Context("when InterfaceInitialized condition is not set", func() {
		It("should return status without phase change", func() {
			status, err := InitializeInterface(ctx, dpu, ctrlCtx)

			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUInitializeInterface))
		})
	})

	Context("when InterfaceInitialized condition is False", func() {
		It("should return status without phase change", func() {
			cutil.SetDPUCondition(&dpu.Status, &metav1.Condition{
				Type:   string(provisioningv1.DPUCondInterfaceInitialized),
				Status: metav1.ConditionFalse,
				Reason: "NotReady",
			})

			status, err := InitializeInterface(ctx, dpu, ctrlCtx)

			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUInitializeInterface))
		})
	})

	Context("when InterfaceInitialized condition is True", func() {
		Context("and message contains DPUCondMessageModeUpdate", func() {
			It("should set phase to DPURebooting", func() {
				cutil.SetDPUCondition(&dpu.Status, &metav1.Condition{
					Type:    string(provisioningv1.DPUCondInterfaceInitialized),
					Status:  metav1.ConditionTrue,
					Reason:  "",
					Message: string(provisioningv1.DPUCondMessageModeUpdate),
				})

				status, err := InitializeInterface(ctx, dpu, ctrlCtx)

				Expect(err).NotTo(HaveOccurred())
				Expect(status.Phase).To(Equal(provisioningv1.DPURebooting))
			})
		})

		Context("and message does not contain DPUCondMessageModeUpdate", func() {
			It("should set phase to DPUConfigFWParameters with empty message", func() {
				cutil.SetDPUCondition(&dpu.Status, &metav1.Condition{
					Type:    string(provisioningv1.DPUCondInterfaceInitialized),
					Status:  metav1.ConditionTrue,
					Reason:  "",
					Message: "",
				})

				status, err := InitializeInterface(ctx, dpu, ctrlCtx)

				Expect(err).NotTo(HaveOccurred())
				Expect(status.Phase).To(Equal(provisioningv1.DPUConfigFWParameters))
			})
		})
	})
})
