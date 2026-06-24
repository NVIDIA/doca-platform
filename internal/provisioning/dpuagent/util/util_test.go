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
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("TruncateConditionMessage", func() {
	It("returns trimmed message unchanged when within limit", func() {
		Expect(TruncateConditionMessage("  hello  ")).To(Equal("hello"))
	})

	It("truncates long messages with ellipsis", func() {
		msg := strings.Repeat("a", maxConditionMessageLen+10)
		truncated := TruncateConditionMessage(msg)
		Expect(truncated).To(HaveSuffix("…"))
		Expect(strings.TrimSuffix(truncated, "…")).To(HaveLen(maxConditionMessageLen - 1))
	})
})

var _ = Describe("IsBlueField4", func() {
	It("returns true only for BlueField4 DPU type", func() {
		Expect(IsBlueField4(nil)).To(BeFalse())
		Expect(IsBlueField4(&provisioningv1.DPU{})).To(BeFalse())
		Expect(IsBlueField4(&provisioningv1.DPU{
			Status: provisioningv1.DPUStatus{DPUType: provisioningv1.DPUTypeBlueField3},
		})).To(BeFalse())
		Expect(IsBlueField4(&provisioningv1.DPU{
			Status: provisioningv1.DPUStatus{DPUType: provisioningv1.DPUTypeBlueField4},
		})).To(BeTrue())
	})
})
