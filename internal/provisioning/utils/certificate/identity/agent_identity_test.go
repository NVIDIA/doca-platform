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

package identity

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("DPU Agent Identity", func() {
	Describe("DPUAgentUsername", func() {
		It("formats the username with the dpu prefix", func() {
			Expect(DPUAgentUsername("dpu-01")).To(Equal("da-dpu-01"))
		})
	})

	Describe("DPUNameFromAgentUsername", func() {
		It("extracts the DPU name from a valid username", func() {
			name, ok := DPUNameFromAgentUsername("da-dpu-01")
			Expect(ok).To(BeTrue())
			Expect(name).To(Equal("dpu-01"))
		})

		It("returns false when the prefix is missing", func() {
			name, ok := DPUNameFromAgentUsername("dpu-01")
			Expect(ok).To(BeFalse())
			Expect(name).To(BeEmpty())
		})

		It("returns false when the username has no suffix", func() {
			name, ok := DPUNameFromAgentUsername("da-")
			Expect(ok).To(BeFalse())
			Expect(name).To(BeEmpty())
		})
	})
})
