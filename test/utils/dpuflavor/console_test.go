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

package dpuflavor

import (
	"slices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("WithConsoleKernelParameter", func() {
	DescribeTable("replaces console parameters while preserving other parameters",
		func(input []string, console string, expected []string) {
			original := slices.Clone(input)

			Expect(WithConsoleKernelParameter(input, console)).To(Equal(expected))
			Expect(input).To(Equal(original), "the input slice must not be modified")
		},
		Entry("selects hvc0 for Host Trust",
			[]string{"console=hvc0", "console=ttyAMA0", "iommu.passthrough=1"},
			"hvc0",
			[]string{"iommu.passthrough=1", "console=hvc0"},
		),
		Entry("selects ttyAMA0 for Zero Trust",
			[]string{"console=hvc0", "earlycon=pl011,0x13010000", "console=ttyAMA0"},
			"ttyAMA0",
			[]string{"earlycon=pl011,0x13010000", "console=ttyAMA0"},
		),
		Entry("adds a console when none exists",
			[]string{"net.ifnames=0", "biosdevname=0"},
			"hvc0",
			[]string{"net.ifnames=0", "biosdevname=0", "console=hvc0"},
		),
		Entry("handles empty parameters",
			nil,
			"ttyAMA0",
			[]string{"console=ttyAMA0"},
		),
	)
})
