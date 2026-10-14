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

package common

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("StorageClass", func() {
	Describe("ValidateCreateVolumeParameters", func() {
		It("should return error when policy parameter is missing", func() {
			params := map[string]string{}
			err := ValidateCreateVolumeParameters(params)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Parameters.policy: field is required"))
		})

		It("should succeed with valid policy", func() {
			params := map[string]string{
				StorageClassPolicyKey: "test-policy",
			}
			err := ValidateCreateVolumeParameters(params)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return error with invalid functionType", func() {
			params := map[string]string{
				StorageClassPolicyKey:       "test-policy",
				StorageClassFunctionTypeKey: "invalid",
			}
			err := ValidateCreateVolumeParameters(params)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unsupported value invalid"))
		})

		It("should return error when hotplugFunction is true with vf functionType", func() {
			params := map[string]string{
				StorageClassPolicyKey:          "test-policy",
				StorageClassFunctionTypeKey:    "vf",
				StorageClassHotplugFunctionKey: "true",
			}
			err := ValidateCreateVolumeParameters(params)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("can only be true when functionType is pf"))
		})
	})

	Describe("ExcludeCSIPluginStorageClassParameters", func() {
		It("should exclude all CSI plugin parameters", func() {
			params := map[string]string{
				StorageClassPolicyKey:          "test-policy",
				StorageClassFunctionTypeKey:    "pf",
				StorageClassHotplugFunctionKey: "true",
				"customParam":                  "customValue",
			}
			result := ExcludeCSIPluginStorageClassParameters(params)
			Expect(result).NotTo(HaveKey(StorageClassPolicyKey))
			Expect(result).NotTo(HaveKey(StorageClassFunctionTypeKey))
			Expect(result).NotTo(HaveKey(StorageClassHotplugFunctionKey))
			Expect(result).To(HaveKey("customParam"))
			Expect(result["customParam"]).To(Equal("customValue"))
		})

		It("should return empty map when input contains only CSI plugin parameters", func() {
			params := map[string]string{
				StorageClassPolicyKey:          "test-policy",
				StorageClassFunctionTypeKey:    "pf",
				StorageClassHotplugFunctionKey: "true",
			}
			result := ExcludeCSIPluginStorageClassParameters(params)
			Expect(result).To(BeEmpty())
		})
	})
})
