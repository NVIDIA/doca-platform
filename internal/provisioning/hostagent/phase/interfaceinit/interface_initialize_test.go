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

package interfaceinit

import (
	"context"
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("Handle - Secure Boot Validation (Trusted Host mode)", func() {
	var (
		ctx context.Context
		dpu *provisioningv1.DPU
	)

	newHandler := func(queryRshim func(string) (*hostutil.RshimInfo, error)) *Handler {
		scheme := runtime.NewScheme()
		Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			Build()
		return &Handler{
			Client: fakeClient,
			GetDevice: func(sn string) (hostutil.Device, bool) {
				return hostutil.Device{Address: "0000:4d:00.0"}, true
			},
			QueryRshim: queryRshim,
			GetDPUMode: func(context.Context, string) (provisioningv1.DpuModeType, error) {
				return provisioningv1.DpuMode, nil
			},
		}
	}

	BeforeEach(func() {
		ctx = context.Background()
		dpu = &provisioningv1.DPU{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-dpu",
				Namespace: "default",
			},
			Spec: provisioningv1.DPUSpec{
				DPUDeviceName: "test-device",
				SerialNumber:  "SN123",
				DPUFlavor:     "test-flavor",
				PCIAddress:    ptr.To("0000:4d:00.0"),
			},
		}
	})

	It("should set TrustedHostModeNotSupported condition on mismatch without changing phase", func() {
		dpu.Spec.SecureBoot = ptr.To(true)
		handler := newHandler(func(pci string) (*hostutil.RshimInfo, error) {
			return &hostutil.RshimInfo{
				RshimName:         "rshim0",
				SecureBootEnabled: ptr.To(false),
			}, nil
		})

		status, _, err := handler.Handle(ctx, dpu)
		Expect(err).NotTo(HaveOccurred())
		// Host agent must NOT set the phase - only the provisioning controller does
		Expect(status.Phase).NotTo(Equal(provisioningv1.DPUError))
		// Condition should be set for the provisioning controller to detect
		cond := meta.FindStatusCondition(status.Conditions, string(provisioningv1.DPUCondInterfaceInitialized))
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal("TrustedHostModeNotSupported"))
		// Status should always reflect actual hardware state, even on mismatch
		Expect(status.SecureBoot).NotTo(BeNil())
		Expect(*status.SecureBoot.Enabled).To(BeFalse())
	})

	It("should sync SecureBoot status when states match", func() {
		dpu.Spec.SecureBoot = ptr.To(true)
		handler := newHandler(func(pci string) (*hostutil.RshimInfo, error) {
			return &hostutil.RshimInfo{
				RshimName:         "rshim0",
				SecureBootEnabled: ptr.To(true),
			}, nil
		})

		status, _, err := handler.Handle(ctx, dpu)
		// canSatisfy may fail (no DPUFlavor), but SecureBoot status should be synced
		if err != nil {
			Expect(err.Error()).NotTo(ContainSubstring("Secure Boot"))
		}
		Expect(status.SecureBoot).NotTo(BeNil())
		Expect(*status.SecureBoot.Enabled).To(BeTrue())
		Expect(status.Phase).NotTo(Equal(provisioningv1.DPUError))
	})

	It("should retry when rshim Secure Boot status is not available", func() {
		dpu.Spec.SecureBoot = ptr.To(true)
		handler := newHandler(func(pci string) (*hostutil.RshimInfo, error) {
			return &hostutil.RshimInfo{
				RshimName:         "rshim0",
				SecureBootEnabled: nil, // Not available
			}, nil
		})

		_, _, err := handler.Handle(ctx, dpu)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not available"))
	})

	It("should retry when rshim query fails", func() {
		dpu.Spec.SecureBoot = ptr.To(true)
		handler := newHandler(func(pci string) (*hostutil.RshimInfo, error) {
			return nil, fmt.Errorf("rshim device not found")
		})

		_, _, err := handler.Handle(ctx, dpu)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("rshim device not found"))
	})

	It("should retry when device is not found by serial number", func() {
		dpu.Spec.SecureBoot = ptr.To(true)
		scheme := runtime.NewScheme()
		Expect(provisioningv1.AddToScheme(scheme)).To(Succeed())
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
		handler := &Handler{
			Client: fakeClient,
			GetDevice: func(sn string) (hostutil.Device, bool) {
				return hostutil.Device{}, false // Not found
			},
			QueryRshim: hostutil.QueryRshimByPCI,
			GetDPUMode: func(context.Context, string) (provisioningv1.DpuModeType, error) {
				return provisioningv1.DpuMode, nil
			},
		}

		_, _, err := handler.Handle(ctx, dpu)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("device not found"))
	})

	It("should skip validation when spec.SecureBoot is nil", func() {
		dpu.Spec.SecureBoot = nil
		handler := newHandler(func(pci string) (*hostutil.RshimInfo, error) {
			Fail("QueryRshim should not be called when SecureBoot is nil")
			return nil, nil
		})

		status, _, _ := handler.Handle(ctx, dpu)
		Expect(status.SecureBoot).To(BeNil())
		Expect(status.Phase).NotTo(Equal(provisioningv1.DPUError))
	})
})
