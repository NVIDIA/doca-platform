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
