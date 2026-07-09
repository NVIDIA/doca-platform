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
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func dpuWithIdentityMode(mode *provisioningv1.IdentityMode) *provisioningv1.DPU {
	return &provisioningv1.DPU{Status: provisioningv1.DPUStatus{IdentityMode: mode}}
}

var _ = Describe("SPIFFE helpers", func() {
	spiffe := provisioningv1.IdentityModeSpiffe
	bootstrap := provisioningv1.IdentityModeBootstrapToken

	DescribeTable("SpiffeEnabled",
		func(cfg *operatorv1.DPFOperatorConfig, want bool) {
			Expect(SpiffeEnabled(cfg)).To(Equal(want))
		},
		Entry("nil config", nil, false),
		Entry("nil security", &operatorv1.DPFOperatorConfig{}, false),
		Entry("security without spiffe", &operatorv1.DPFOperatorConfig{
			Spec: operatorv1.DPFOperatorConfigSpec{Security: &operatorv1.SecurityConfiguration{}},
		}, false),
		Entry("security with spiffe", &operatorv1.DPFOperatorConfig{
			Spec: operatorv1.DPFOperatorConfigSpec{Security: &operatorv1.SecurityConfiguration{
				SPIFFE: &operatorv1.SPIFFEConfiguration{},
			}},
		}, true),
	)

	DescribeTable("IsSpiffeDPU",
		func(dpu *provisioningv1.DPU, want bool) {
			Expect(IsSpiffeDPU(dpu)).To(Equal(want))
		},
		Entry("nil dpu", nil, false),
		Entry("nil identity mode (legacy)", dpuWithIdentityMode(nil), false),
		Entry("bootstrap-token mode", dpuWithIdentityMode(&bootstrap), false),
		Entry("spiffe mode", dpuWithIdentityMode(&spiffe), true),
	)

	DescribeTable("IsBootstrapTokenDPU",
		func(dpu *provisioningv1.DPU, want bool) {
			Expect(IsBootstrapTokenDPU(dpu)).To(Equal(want))
		},
		Entry("nil dpu", nil, false),
		Entry("nil identity mode (legacy treated as bootstrap-token)", dpuWithIdentityMode(nil), true),
		Entry("bootstrap-token mode", dpuWithIdentityMode(&bootstrap), true),
		Entry("spiffe mode", dpuWithIdentityMode(&spiffe), false),
	)
})
