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

package controller

import (
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/config"

	"github.com/container-storage-interface/spec/lib/go/csi"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/utils/ptr"
)

var _ = Describe("Helpers", func() {
	Context("generateID", func() {
		It("should produce same ID for same inputs", func() {
			Expect(generateID("test", "foo")).To(
				Equal(generateID("test", "foo")))
		})
		It("should produce different IDs for different inputs", func() {
			Expect(generateID("test", "foo")).NotTo(
				Equal(generateID("test", "bar")))
		})
	})

	Context("generateDPUVolumeAttachmentName", func() {
		It("should generate simple hyphenated name when not too long", func() {
			name := generateDPUVolumeAttachmentName("dpu-node", "volume-name")
			Expect(name).To(Equal("dpu-node-volume-name"))
		})

		It("should generate hashed name when too long", func() {
			longNodeName := "very-long-dpu-node-name-that-exceeds-the-kubernetes-limit-when-combined-with-the-volume-name-very-long-dpu-node-name-that-exceeds-the-kubernetes-limit-when-combined-with-the-volume-name"
			longVolumeName := "very-long-volume-name-that-exceeds-the-kubernetes-limit-when-combined-with-the-node-name-very-long-volume-name-that-exceeds-the-kubernetes-limit-when-combined-with-the-node-name"

			name := generateDPUVolumeAttachmentName(longNodeName, longVolumeName)
			Expect(name).To(HavePrefix("va-"))
			Expect(len(name)).To(BeNumerically("<", validation.DNS1123SubdomainMaxLength))
		})

		It("should generate different names for different inputs", func() {
			name1 := generateDPUVolumeAttachmentName("dpu-node-1", "volume-1")
			name2 := generateDPUVolumeAttachmentName("dpu-node-2", "volume-1")
			name3 := generateDPUVolumeAttachmentName("dpu-node-1", "volume-2")

			Expect(name1).NotTo(Equal(name2))
			Expect(name1).NotTo(Equal(name3))
			Expect(name2).NotTo(Equal(name3))
		})
	})

	Context("getDPUVolumeMode", func() {
		It("should return Block mode for nvme emulation mode", func() {
			result := getDPUVolumeMode(config.Common{EmulationMode: config.EmulationModeNVMe})
			Expect(result).To(Equal(ptr.To(corev1.PersistentVolumeBlock)))
		})
		It("should return Filesystem mode for virtiofs emulation mode", func() {
			result := getDPUVolumeMode(config.Common{EmulationMode: config.EmulationModeVirtiofs})
			Expect(result).To(Equal(ptr.To(corev1.PersistentVolumeFilesystem)))
		})
		It("should return Block mode by default", func() {
			result := getDPUVolumeMode(config.Common{})
			Expect(result).To(Equal(ptr.To(corev1.PersistentVolumeBlock)))
		})
	})
	Context("convertCSIAccessModesToStorageAPIAccessModes", func() {
		It("should convert MULTI_NODE_MULTI_WRITER to ReadWriteMany", func() {
			volCaps := []*csi.VolumeCapability{{AccessMode: &csi.VolumeCapability_AccessMode{
				Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER,
			}}}
			result := convertCSIAccessModesToStorageAPIAccessModes(volCaps)
			Expect(result).To(ContainElement(corev1.ReadWriteMany))
		})
		It("should convert MULTI_NODE_READER_ONLY to ReadOnlyMany", func() {
			volCaps := []*csi.VolumeCapability{{AccessMode: &csi.VolumeCapability_AccessMode{
				Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
			}}}
			result := convertCSIAccessModesToStorageAPIAccessModes(volCaps)
			Expect(result).To(ContainElement(corev1.ReadOnlyMany))
		})
		It("should convert SINGLE_NODE_WRITER to ReadWriteOnce", func() {
			volCaps := []*csi.VolumeCapability{{AccessMode: &csi.VolumeCapability_AccessMode{
				Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
			}}}
			result := convertCSIAccessModesToStorageAPIAccessModes(volCaps)
			Expect(result).To(ContainElement(corev1.ReadWriteOnce))
		})
		It("should convert SINGLE_NODE_SINGLE_WRITER to ReadWriteOncePod", func() {
			volCaps := []*csi.VolumeCapability{{AccessMode: &csi.VolumeCapability_AccessMode{
				Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER,
			}}}
			result := convertCSIAccessModesToStorageAPIAccessModes(volCaps)
			Expect(result).To(ContainElement(corev1.ReadWriteOncePod))
		})
	})
})
