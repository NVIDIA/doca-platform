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

package apivalidation_test

import (
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"
)

var _ = Describe("DPFOperatorConfig networking getters", func() {
	It("defaults GetDPUNodeOOBBridgeName to br-dpu when unset", func() {
		config := &operatorv1.DPFOperatorConfig{}
		Expect(config.Spec.Networking.GetDPUNodeOOBBridgeName()).To(Equal(operatorv1.DefaultDPUNodeOOBBridgeName))
	})

	It("returns configured DPUNodeOOBBridgeName", func() {
		config := &operatorv1.DPFOperatorConfig{
			Spec: operatorv1.DPFOperatorConfigSpec{
				Networking: &operatorv1.Networking{
					DPUNodeOOBBridgeName: ptr.To("br-ex"),
				},
			},
		}
		Expect(config.Spec.Networking.GetDPUNodeOOBBridgeName()).To(Equal("br-ex"))
	})
})
