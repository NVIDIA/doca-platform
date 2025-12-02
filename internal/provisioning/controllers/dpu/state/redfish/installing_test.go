/*
Copyright 2025 NVIDIA

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

package redfish

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Installing", func() {
	Context("concatBFBAndBFCFGPath", func() {
		It("should return the correct path", func() {
			registry := "10.0.110.1:8080"
			bfbFile := "/bfb/dpf-operator-system-bfb-bundle.bfb"
			bfcfgFile := "/bfb/bfcfg/dpf-operator-system_dpu-node-mt25066004be-mt25066004be_ea091a0e-f0ae-4033-9db3-2ecf9a1dfe61"
			expected := "10.0.110.1:8080/bfb/??dpf-operator-system-bfb-bundle.bfb,bfcfg/dpf-operator-system_dpu-node-mt25066004be-mt25066004be_ea091a0e-f0ae-4033-9db3-2ecf9a1dfe61?/bfb-to-install"

			Expect(concatBFBAndBFCFGPath(registry, bfbFile, bfcfgFile)).To(Equal(expected))
			Expect(concatBFBAndBFCFGPath("http://"+registry, bfbFile, bfcfgFile)).To(Equal(expected))
			Expect(concatBFBAndBFCFGPath("https://"+registry, bfbFile, bfcfgFile)).To(Equal(expected))
		})
	})
})
