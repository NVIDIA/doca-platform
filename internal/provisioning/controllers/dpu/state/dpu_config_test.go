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

package state_test

import (
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

var _ = Describe("Phase DPUConfig", func() {
	var defaultDPUName = "dpu-config-test"

	Context("waiting for agent", func() {
		It("should wait when AgentStatus is nil", func() {
			dpu := dpuObj(defaultDPUName)
			dpu.Status.Phase = provisioningv1.DPUConfig

			status, err := state.DPUConfig(ctx, dpu, &dutil.ControllerContext{})
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUConfig))
		})

		It("should wait when RebootMethod is nil", func() {
			dpu := dpuObj(defaultDPUName)
			dpu.Status.Phase = provisioningv1.DPUConfig
			dpu.Status.AgentStatus = &provisioningv1.AgentStatus{
				LastStartupTime: ptr.To(metav1.Now()),
			}

			status, err := state.DPUConfig(ctx, dpu, &dutil.ControllerContext{})
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUConfig))
		})

		It("should wait when RebootMethod is Unknown even if LastStartupTime changed", func() {
			oldTime := metav1.NewTime(metav1.Now().Add(-time.Hour))
			dpu := dpuObj(defaultDPUName)
			dpu.Status.Phase = provisioningv1.DPUConfig
			dpu.Status.AgentLastStartupTime = &oldTime
			dpu.Status.AgentStatus = &provisioningv1.AgentStatus{
				LastStartupTime: ptr.To(metav1.Now()),
				RebootMethod:    ptr.To(provisioningv1.RebootMethodUnknown),
			}

			status, err := state.DPUConfig(ctx, dpu, &dutil.ControllerContext{})
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUConfig))
			Expect(status.AgentLastStartupTime).To(Equal(&oldTime), "should not update AgentLastStartupTime while waiting for real RebootMethod")
		})
	})

	Context("stale RebootMethod guard", func() {
		It("should wait when AgentLastStartupTime equals LastStartupTime", func() {
			now := metav1.Now()
			dpu := dpuObj(defaultDPUName)
			dpu.Status.Phase = provisioningv1.DPUConfig
			dpu.Status.AgentLastStartupTime = &now
			dpu.Status.AgentStatus = &provisioningv1.AgentStatus{
				LastStartupTime: &now,
				RebootMethod:    ptr.To(provisioningv1.RebootMethodNoAction),
			}

			status, err := state.DPUConfig(ctx, dpu, &dutil.ControllerContext{})
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUConfig), "should not advance when startup time has not changed")
		})
	})

	Context("deletion", func() {
		It("should transition to DPUDeleting when DPU is being deleted", func() {
			dpu := dpuObj(defaultDPUName)
			dpu.Status.Phase = provisioningv1.DPUConfig
			now := metav1.Now()
			dpu.DeletionTimestamp = &now

			status, err := state.DPUConfig(ctx, dpu, &dutil.ControllerContext{})
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(provisioningv1.DPUDeleting))
		})
	})
})
