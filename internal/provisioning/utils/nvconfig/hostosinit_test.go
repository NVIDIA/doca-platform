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

package nvconfig

import (
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"
)

var _ = Describe("DELAY_HOST_OS_INIT parsing", func() {
	It("recognizes user-mode values in every spelling mlxconfig accepts", func() {
		for _, value := range []string{"3", "0x3", "0X3", "0x03", "0X03", "003", " 0x03 ", "ENABLE_USER", "enable_user"} {
			Expect(isDelayHostOSInitUserMode(value)).To(BeTrue(), "expected %q to hold the host", value)
		}
		for _, value := range []string{"0", "1", "0x1", "30", "0x30", "", "0x", "0x3x", "garbage"} {
			Expect(isDelayHostOSInitUserMode(value)).To(BeFalse(), "expected %q not to hold the host", value)
		}
	})

	It("matches the parameter name case-insensitively and ignores unrelated parameters", func() {
		Expect(ParamsRequestHostOSInitHold([]string{"PF_BAR2_ENABLE=0", "delay_host_os_init=0x3"})).To(BeTrue())
		Expect(ParamsRequestHostOSInitHold([]string{" DELAY_HOST_OS_INIT = 3 "})).To(BeTrue())
		Expect(ParamsRequestHostOSInitHold([]string{"PF_BAR2_ENABLE=0", "DELAY_HOST_OS_INIT=0"})).To(BeFalse())
		Expect(ParamsRequestHostOSInitHold([]string{"DELAY_HOST_OS_INIT"})).To(BeFalse())
		Expect(ParamsRequestHostOSInitHold(nil)).To(BeFalse())
	})

	It("finds a hold in any NVConfig entry of the flavor", func() {
		Expect(FlavorRequestsHostOSInitHold([]provisioningv1.NVConfig{
			{Device: ptr.To("p0"), Parameters: []string{"PF_BAR2_ENABLE=0"}},
			{Device: ptr.To("p1"), Parameters: []string{"DELAY_HOST_OS_INIT=0x03"}},
		})).To(BeTrue())

		Expect(FlavorRequestsHostOSInitHold([]provisioningv1.NVConfig{
			{Device: ptr.To("p0"), Parameters: []string{"PF_BAR2_ENABLE=0"}},
			{Device: ptr.To("p1"), Parameters: []string{"DELAY_HOST_OS_INIT=0x1"}},
		})).To(BeFalse())

		Expect(FlavorRequestsHostOSInitHold(nil)).To(BeFalse())
	})
})
