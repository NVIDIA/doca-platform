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

package util

import (
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("CheckInstallationTimeout", func() {
	It("should return nil when timeout is zero", func() {
		state := &provisioningv1.DPUStatus{}
		Expect(CheckInstallationTimeout(state, 0)).To(Succeed())
	})

	It("should return nil when timeout is negative", func() {
		state := &provisioningv1.DPUStatus{}
		Expect(CheckInstallationTimeout(state, -1*time.Minute)).To(Succeed())
	})

	It("should return nil when BFBPrepared condition is not set", func() {
		state := &provisioningv1.DPUStatus{}
		Expect(CheckInstallationTimeout(state, 45*time.Minute)).To(Succeed())
	})

	It("should return nil when timeout has not been exceeded", func() {
		state := &provisioningv1.DPUStatus{}
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondBFBPrepared), nil, "Prepared", ""))
		Expect(CheckInstallationTimeout(state, 45*time.Minute)).To(Succeed())
	})

	It("should return error when timeout has been exceeded", func() {
		state := &provisioningv1.DPUStatus{
			Conditions: []metav1.Condition{
				{
					Type:               string(provisioningv1.DPUCondBFBPrepared),
					Status:             metav1.ConditionTrue,
					LastTransitionTime: metav1.Time{Time: time.Now().Add(-50 * time.Minute)},
					Reason:             "Prepared",
				},
			},
		}
		err := CheckInstallationTimeout(state, 45*time.Minute)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("OS installation timeout exceeded"))
	})
})
