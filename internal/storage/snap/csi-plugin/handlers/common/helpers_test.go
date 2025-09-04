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
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/config"

	"github.com/container-storage-interface/spec/lib/go/csi"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc/codes"
)

var _ = Describe("Common Module", func() {
	Describe("ValidateVolumeCapability", func() {
		It("should return error when VolumeCapability is nil", func() {
			err := ValidateVolumeCapability(config.EmulationModeNVMe, nil)
			CheckGRPCErr(err, codes.InvalidArgument, "VolumeCapability: field is required")
		})

		It("should return error when AccessMode is nil", func() {
			volCap := &csi.VolumeCapability{}
			err := ValidateVolumeCapability(config.EmulationModeNVMe, volCap)
			CheckGRPCErr(err, codes.InvalidArgument, "VolumeCapability.AccessMode: field is required")
		})

		It("should return error when AccessType is not set", func() {
			volCap := &csi.VolumeCapability{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
			}
			err := ValidateVolumeCapability(config.EmulationModeNVMe, volCap)
			CheckGRPCErr(err, codes.InvalidArgument, "VolumeCapability.AccessType: field is required")
		})

		It("should return error when AccessType is partially set", func() {
			volCap := &csi.VolumeCapability{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
				AccessType: &csi.VolumeCapability_Block{},
			}
			err := ValidateVolumeCapability(config.EmulationModeNVMe, volCap)
			CheckGRPCErr(err, codes.InvalidArgument, "VolumeCapability.Block: field is required")
		})

		It("should return no error for valid Block access type with NVMe emulation", func() {
			volCap := &csi.VolumeCapability{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
				AccessType: &csi.VolumeCapability_Block{
					Block: &csi.VolumeCapability_BlockVolume{},
				},
			}
			err := ValidateVolumeCapability(config.EmulationModeNVMe, volCap)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return error for Block access type with Virtiofs emulation", func() {
			volCap := &csi.VolumeCapability{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
				AccessType: &csi.VolumeCapability_Block{
					Block: &csi.VolumeCapability_BlockVolume{},
				},
			}
			err := ValidateVolumeCapability(config.EmulationModeVirtiofs, volCap)
			CheckGRPCErr(err, codes.Unimplemented, "VolumeCapability.Block is not supported")
		})

		It("should return no error for valid Mount access type with Virtiofs emulation", func() {
			volCap := &csi.VolumeCapability{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
				AccessType: &csi.VolumeCapability_Mount{
					Mount: &csi.VolumeCapability_MountVolume{},
				},
			}
			err := ValidateVolumeCapability(config.EmulationModeVirtiofs, volCap)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return error for Mount access type with NVMe emulation", func() {
			volCap := &csi.VolumeCapability{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
				AccessType: &csi.VolumeCapability_Mount{
					Mount: &csi.VolumeCapability_MountVolume{},
				},
			}
			err := ValidateVolumeCapability(config.EmulationModeNVMe, volCap)
			CheckGRPCErr(err, codes.Unimplemented, "VolumeCapability.Mount is not supported")
		})
	})

	Describe("ValidateVolumeCapabilities", func() {
		It("should return error when VolumeCapabilities is empty", func() {
			err := ValidateVolumeCapabilities(config.EmulationModeNVMe, nil)
			CheckGRPCErr(err, codes.InvalidArgument, "VolumeCapabilities: field is required")
		})

		It("should return error when any VolumeCapability is nil", func() {
			volCaps := []*csi.VolumeCapability{
				nil,
			}
			err := ValidateVolumeCapabilities(config.EmulationModeNVMe, volCaps)
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
			err := ValidateVolumeCapabilities(config.EmulationModeNVMe, volCaps)
			CheckGRPCErr(err, codes.InvalidArgument, "VolumeCapabilities[0].AccessType: field is required")
		})

		It("should validate all VolumeCapabilities successfully with NVMe emulation", func() {
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
			err := ValidateVolumeCapabilities(config.EmulationModeNVMe, volCaps)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should validate all VolumeCapabilities successfully with Virtiofs emulation", func() {
			volCaps := []*csi.VolumeCapability{
				{
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
					},
					AccessType: &csi.VolumeCapability_Mount{
						Mount: &csi.VolumeCapability_MountVolume{},
					},
				},
			}
			err := ValidateVolumeCapabilities(config.EmulationModeVirtiofs, volCaps)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("CheckVolumeCapability", func() {
		It("should return error when AccessMode is nil", func() {
			err := CheckVolumeCapability(config.EmulationModeNVMe, "field", &csi.VolumeCapability{})
			CheckGRPCErr(err, codes.InvalidArgument, "field.AccessMode: field is required")
		})

		It("should return error for unsupported access type Mount with NVMe emulation", func() {
			volCap := &csi.VolumeCapability{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
				AccessType: &csi.VolumeCapability_Mount{
					Mount: &csi.VolumeCapability_MountVolume{},
				},
			}
			err := CheckVolumeCapability(config.EmulationModeNVMe, "field", volCap)
			CheckGRPCErr(err, codes.Unimplemented, "field.Mount is not supported")
		})

		It("should return error for unsupported access type Block with Virtiofs emulation", func() {
			volCap := &csi.VolumeCapability{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
				AccessType: &csi.VolumeCapability_Block{
					Block: &csi.VolumeCapability_BlockVolume{},
				},
			}
			err := CheckVolumeCapability(config.EmulationModeVirtiofs, "field", volCap)
			CheckGRPCErr(err, codes.Unimplemented, "field.Block is not supported")
		})

		It("should return no error for supported Block access type with NVMe emulation", func() {
			volCap := &csi.VolumeCapability{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
				AccessType: &csi.VolumeCapability_Block{
					Block: &csi.VolumeCapability_BlockVolume{},
				},
			}
			err := CheckVolumeCapability(config.EmulationModeNVMe, "field", volCap)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return no error for supported Mount access type with Virtiofs emulation", func() {
			volCap := &csi.VolumeCapability{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
				AccessType: &csi.VolumeCapability_Mount{
					Mount: &csi.VolumeCapability_MountVolume{},
				},
			}
			err := CheckVolumeCapability(config.EmulationModeVirtiofs, "field", volCap)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})

var _ = Describe("Helpers", func() {
	Describe("FunctionTypeConfigFromStrings", func() {
		var (
			commonConfig config.Common
		)
		sharedCommonValidationTests := func(emulationMode string) {
			Context("Common validation tests for "+emulationMode, func() {
				BeforeEach(func() {
					commonConfig = config.Common{EmulationMode: emulationMode}
				})
				It("should return error functionType is invalid", func() {
					config, err := FunctionTypeConfigFromStrings(commonConfig, "invalid", "true")
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("functionType: unsupported value invalid, supported values are: vf, pf"))
					Expect(config).To(Equal(storagev1.FunctionTypeConfig{}))
				})
				It("should return error when hotplugFunction is not boolean", func() {
					config, err := FunctionTypeConfigFromStrings(commonConfig, "pf", "invalid")
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("hotplugFunction: is not a boolean value"))
					Expect(config).To(Equal(storagev1.FunctionTypeConfig{}))
				})
				It("should return error when hotplugFunction is true and functionType is vf", func() {
					config, err := FunctionTypeConfigFromStrings(commonConfig, "vf", "true")
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("hotplugFunction: must be false when functionType is vf"))
					Expect(config).To(Equal(storagev1.FunctionTypeConfig{}))
				})
				It("should return error when hotplugFunction is false and functionType is pf", func() {
					config, err := FunctionTypeConfigFromStrings(commonConfig, "pf", "false")
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("hotplugFunction: must be true when functionType is pf"))
					Expect(config).To(Equal(storagev1.FunctionTypeConfig{}))
				})
				It("should succeed with pf functionType", func() {
					config, err := FunctionTypeConfigFromStrings(commonConfig, "pf", "true")
					Expect(err).NotTo(HaveOccurred())
					Expect(config).To(Equal(storagev1.FunctionTypeConfig{
						FunctionType:    storagev1.FunctionTypePF,
						HotplugFunction: true,
					}))
				})
			})
		}

		sharedCommonValidationTests(config.EmulationModeNVMe)
		Context("NVMe emulation mode", func() {
			BeforeEach(func() {
				commonConfig = config.Common{EmulationMode: config.EmulationModeNVMe}
			})
			It("should succeed with default config", func() {
				config, err := FunctionTypeConfigFromStrings(commonConfig, "", "")
				Expect(err).NotTo(HaveOccurred())
				Expect(config).To(Equal(storagev1.FunctionTypeConfig{
					FunctionType:    storagev1.FunctionTypeVF,
					HotplugFunction: false,
				}))
			})
			It("should succeed with vf functionType", func() {
				config, err := FunctionTypeConfigFromStrings(commonConfig, "vf", "false")
				Expect(err).NotTo(HaveOccurred())
				Expect(config).To(Equal(storagev1.FunctionTypeConfig{
					FunctionType:    storagev1.FunctionTypeVF,
					HotplugFunction: false,
				}))
			})
		})
		sharedCommonValidationTests(config.EmulationModeVirtiofs)
		Context("Virtiofs emulation mode", func() {
			BeforeEach(func() {
				commonConfig = config.Common{EmulationMode: config.EmulationModeVirtiofs}
			})
			It("should return error with default config", func() {
				_, err := FunctionTypeConfigFromStrings(commonConfig, "", "")
				Expect(err).To(MatchError(ContainSubstring("functionType: must be pf, for the current plugin mode")))
			})
			It("should fail with vf functionType", func() {
				_, err := FunctionTypeConfigFromStrings(commonConfig, "vf", "false")
				Expect(err).To(MatchError(ContainSubstring("functionType: must be pf, for the current plugin mode")))
			})
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
			commonConfig := config.Common{EmulationMode: config.EmulationModeNVMe}
			functionTypeStr, hotplugFunctionStr := FunctionTypeConfigAsStrings(originalConfig)
			convertedConfig, err := FunctionTypeConfigFromStrings(commonConfig, functionTypeStr, hotplugFunctionStr)
			Expect(err).NotTo(HaveOccurred())
			Expect(convertedConfig).To(Equal(originalConfig))
		})
	})
})
