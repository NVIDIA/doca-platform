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
	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"

	"github.com/container-storage-interface/spec/lib/go/csi"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc/codes"
)

var _ = Describe("Common Module", func() {
	Describe("ValidateVolumeCapability", func() {
		It("should return error when VolumeCapability is nil", func() {
			err := ValidateVolumeCapability(nil)
			CheckGRPCErr(err, codes.InvalidArgument, "VolumeCapability: field is required")
		})

		It("should return error when AccessMode is nil", func() {
			volCap := &csi.VolumeCapability{}
			err := ValidateVolumeCapability(volCap)
			CheckGRPCErr(err, codes.InvalidArgument, "VolumeCapability.AccessMode: field is required")
		})

		It("should return error when AccessType is not set", func() {
			volCap := &csi.VolumeCapability{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
			}
			err := ValidateVolumeCapability(volCap)
			CheckGRPCErr(err, codes.InvalidArgument, "VolumeCapability.AccessType: field is required")
		})

		It("should return error when AccessType is partially set", func() {
			volCap := &csi.VolumeCapability{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
				AccessType: &csi.VolumeCapability_Block{},
			}
			err := ValidateVolumeCapability(volCap)
			CheckGRPCErr(err, codes.InvalidArgument, "VolumeCapability.Block: field is required")
		})

		It("should return no error for valid Block access type", func() {
			volCap := &csi.VolumeCapability{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
				AccessType: &csi.VolumeCapability_Block{
					Block: &csi.VolumeCapability_BlockVolume{},
				},
			}
			err := ValidateVolumeCapability(volCap)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("ValidateVolumeCapabilities", func() {
		It("should return error when VolumeCapabilities is empty", func() {
			err := ValidateVolumeCapabilities(nil)
			CheckGRPCErr(err, codes.InvalidArgument, "VolumeCapabilities: field is required")
		})

		It("should return error when any VolumeCapability is nil", func() {
			volCaps := []*csi.VolumeCapability{
				nil,
			}
			err := ValidateVolumeCapabilities(volCaps)
			CheckGRPCErr(err, codes.InvalidArgument, "VolumeCapabilities: field is required")
		})

		It("should return error when any VolumeCapability is invalid", func() {
			volCaps := []*csi.VolumeCapability{
				{
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
					},
				},
			}
			err := ValidateVolumeCapabilities(volCaps)
			CheckGRPCErr(err, codes.InvalidArgument, "VolumeCapabilities[0].AccessType: field is required")
		})

		It("should validate all VolumeCapabilities successfully", func() {
			volCaps := []*csi.VolumeCapability{
				{
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
					},
					AccessType: &csi.VolumeCapability_Block{
						Block: &csi.VolumeCapability_BlockVolume{},
					},
				},
			}
			err := ValidateVolumeCapabilities(volCaps)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("CheckVolumeCapability", func() {
		It("should return error when AccessMode is nil", func() {
			err := CheckVolumeCapability("field", &csi.VolumeCapability{})
			CheckGRPCErr(err, codes.InvalidArgument, "field.AccessMode: field is required")
		})

		It("should return error for unsupported access type Mount", func() {
			volCap := &csi.VolumeCapability{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
				AccessType: &csi.VolumeCapability_Mount{},
			}
			err := CheckVolumeCapability("field", volCap)
			CheckGRPCErr(err, codes.Unimplemented, "accessType Mount is not supported")
		})
	})
})

var _ = Describe("Helpers", func() {
	Describe("FunctionTypeConfigFromStrings", func() {
		It("should return default values when both parameters are empty", func() {
			config, err := FunctionTypeConfigFromStrings("", "")
			Expect(err).NotTo(HaveOccurred())
			Expect(config.FunctionType).To(Equal(storagev1.FunctionTypeVF))
			Expect(config.HotplugFunction).To(BeFalse())
		})

		It("should succeed with valid pf and true hotplugFunction", func() {
			config, err := FunctionTypeConfigFromStrings("pf", "true")
			Expect(err).NotTo(HaveOccurred())
			Expect(config.FunctionType).To(Equal(storagev1.FunctionTypePF))
			Expect(config.HotplugFunction).To(BeTrue())
		})

		It("should return error with invalid functionType", func() {
			config, err := FunctionTypeConfigFromStrings("invalid", "")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("functionType: unsupported value invalid"))
			Expect(config).To(Equal(storagev1.FunctionTypeConfig{}))
		})

		It("should return error when hotplugFunction is true with vf functionType", func() {
			config, err := FunctionTypeConfigFromStrings("vf", "true")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("hotplugFunction: can only be true when functionType is pf"))
			Expect(config).To(Equal(storagev1.FunctionTypeConfig{}))
		})
	})

	Describe("FunctionTypeConfigAsStrings", func() {
		It("should convert pf functionType and true hotplugFunction", func() {
			config := storagev1.FunctionTypeConfig{
				FunctionType:    storagev1.FunctionTypePF,
				HotplugFunction: true,
			}
			functionType, hotplugFunction := FunctionTypeConfigAsStrings(config)
			Expect(functionType).To(Equal("pf"))
			Expect(hotplugFunction).To(Equal("true"))
		})

		It("should preserve values through round-trip conversion", func() {
			originalConfig := storagev1.FunctionTypeConfig{
				FunctionType:    storagev1.FunctionTypePF,
				HotplugFunction: true,
			}
			functionTypeStr, hotplugFunctionStr := FunctionTypeConfigAsStrings(originalConfig)
			convertedConfig, err := FunctionTypeConfigFromStrings(functionTypeStr, hotplugFunctionStr)
			Expect(err).NotTo(HaveOccurred())
			Expect(convertedConfig).To(Equal(originalConfig))
		})
	})
})
