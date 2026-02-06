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

package common

import (
	noderesourcesv1 "github.com/nvidia/doca-platform/api/noderesources/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/ptr"
)

const testDefaultResourcePrefix = "test.com"

var testDevicePluginResourcesPath = field.NewPath("spec").Child("devicePluginResources")

var _ = Describe("ValidateDevicePluginResources", func() {
	Context("valid configurations", func() {
		It("should reject empty resource list", func() {
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, []noderesourcesv1.DevicePluginResource{}).ToAggregate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("at least one resource"))
		})
		It("should accept single valid resource", func() {
			resources := []noderesourcesv1.DevicePluginResource{{
				Name: "pods_vf",
				Type: noderesourcesv1.DevicePluginResourceTypeVF,
				Ranges: []noderesourcesv1.VFRange{
					{PFIndex: 0, Start: ptr.To(int32(0)), End: ptr.To(int32(10))},
				},
			}}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).NotTo(HaveOccurred())
		})
		It("should accept multiple resources with different names", func() {
			resources := []noderesourcesv1.DevicePluginResource{
				{
					Name:   "pods_vf",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(0)), End: ptr.To(int32(5))}},
				},
				{
					Name:   "mgmt_vf",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(6)), End: ptr.To(int32(10))}},
				},
			}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).NotTo(HaveOccurred())
		})
		It("should accept same name with different prefixes", func() {
			resources := []noderesourcesv1.DevicePluginResource{
				{
					Name:           "pods_vf",
					ResourcePrefix: ptr.To("nvidia.com"),
					Type:           noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges:         []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(0)), End: ptr.To(int32(5))}},
				},
				{
					Name:           "pods_vf",
					ResourcePrefix: ptr.To("custom.io"),
					Type:           noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges:         []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(6)), End: ptr.To(int32(10))}},
				},
			}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).NotTo(HaveOccurred())
		})
		It("should accept non-overlapping ranges on same PF", func() {
			resources := []noderesourcesv1.DevicePluginResource{
				{
					Name:   "res1",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(0)), End: ptr.To(int32(5))}},
				},
				{
					Name:   "res2",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(6)), End: ptr.To(int32(10))}},
				},
			}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).NotTo(HaveOccurred())
		})
		It("should accept ranges on different PFs", func() {
			resources := []noderesourcesv1.DevicePluginResource{
				{
					Name:   "res1",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(0)), End: ptr.To(int32(10))}},
				},
				{
					Name:   "res2",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 1, Start: ptr.To(int32(0)), End: ptr.To(int32(10))}},
				},
			}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).NotTo(HaveOccurred())
		})
		It("should accept resource name with hyphens and underscores", func() {
			resources := []noderesourcesv1.DevicePluginResource{{
				Name:   "my-resource_name",
				Type:   noderesourcesv1.DevicePluginResourceTypeVF,
				Ranges: []noderesourcesv1.VFRange{{PFIndex: 0}},
			}}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).NotTo(HaveOccurred())
		})
		It("should accept range with only start specified", func() {
			resources := []noderesourcesv1.DevicePluginResource{{
				Name:   "res1",
				Type:   noderesourcesv1.DevicePluginResourceTypeVF,
				Ranges: []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(5))}},
			}}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).NotTo(HaveOccurred())
		})
		It("should accept range with only end specified", func() {
			resources := []noderesourcesv1.DevicePluginResource{{
				Name:   "res1",
				Type:   noderesourcesv1.DevicePluginResourceTypeVF,
				Ranges: []noderesourcesv1.VFRange{{PFIndex: 0, End: ptr.To(int32(10))}},
			}}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).NotTo(HaveOccurred())
		})
		It("should accept range with neither start nor end specified", func() {
			resources := []noderesourcesv1.DevicePluginResource{{
				Name:   "res1",
				Type:   noderesourcesv1.DevicePluginResourceTypeVF,
				Ranges: []noderesourcesv1.VFRange{{PFIndex: 0}},
			}}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).NotTo(HaveOccurred())
		})
		It("should accept range where start equals end", func() {
			resources := []noderesourcesv1.DevicePluginResource{{
				Name:   "res1",
				Type:   noderesourcesv1.DevicePluginResourceTypeVF,
				Ranges: []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(5)), End: ptr.To(int32(5))}},
			}}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).NotTo(HaveOccurred())
		})
		It("should accept adjacent ranges on same PF", func() {
			resources := []noderesourcesv1.DevicePluginResource{
				{
					Name:   "res1",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(0)), End: ptr.To(int32(4))}},
				},
				{
					Name:   "res2",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(5)), End: ptr.To(int32(10))}},
				},
			}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).NotTo(HaveOccurred())
		})
	})
	Context("resource name validation", func() {
		It("should reject empty resource name", func() {
			resources := []noderesourcesv1.DevicePluginResource{{
				Name:   "",
				Type:   noderesourcesv1.DevicePluginResourceTypeVF,
				Ranges: []noderesourcesv1.VFRange{{PFIndex: 0}},
			}}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid resource name"))
		})
		It("should reject resource name with spaces", func() {
			resources := []noderesourcesv1.DevicePluginResource{{
				Name:   "my resource",
				Type:   noderesourcesv1.DevicePluginResourceTypeVF,
				Ranges: []noderesourcesv1.VFRange{{PFIndex: 0}},
			}}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid resource name"))
		})
		It("should reject resource name with special characters", func() {
			resources := []noderesourcesv1.DevicePluginResource{{
				Name:   "my@resource",
				Type:   noderesourcesv1.DevicePluginResourceTypeVF,
				Ranges: []noderesourcesv1.VFRange{{PFIndex: 0}},
			}}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid resource name"))
		})
		It("should reject resource name with dots", func() {
			resources := []noderesourcesv1.DevicePluginResource{{
				Name:   "my.resource",
				Type:   noderesourcesv1.DevicePluginResourceTypeVF,
				Ranges: []noderesourcesv1.VFRange{{PFIndex: 0}},
			}}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid resource name"))
		})
		It("should reject resource name with slashes", func() {
			resources := []noderesourcesv1.DevicePluginResource{{
				Name:   "my/resource",
				Type:   noderesourcesv1.DevicePluginResourceTypeVF,
				Ranges: []noderesourcesv1.VFRange{{PFIndex: 0}},
			}}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid resource name"))
		})
	})
	Context("resource prefix validation", func() {
		It("should reject invalid resource prefix", func() {
			resources := []noderesourcesv1.DevicePluginResource{{
				Name:           "pods_vf",
				ResourcePrefix: ptr.To("Invalid.Prefix"),
				Type:           noderesourcesv1.DevicePluginResourceTypeVF,
				Ranges:         []noderesourcesv1.VFRange{{PFIndex: 0}},
			}}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid resource prefix"))
		})
		It("should reject invalid default resource prefix", func() {
			resources := []noderesourcesv1.DevicePluginResource{{
				Name:   "pods_vf",
				Type:   noderesourcesv1.DevicePluginResourceTypeVF,
				Ranges: []noderesourcesv1.VFRange{{PFIndex: 0}},
			}}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, "Invalid.Default", resources).ToAggregate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid default resource prefix"))
		})
	})
	Context("resource type validation", func() {
		It("should accept valid VF resource type", func() {
			resources := []noderesourcesv1.DevicePluginResource{{
				Name:   "pods_vf",
				Type:   noderesourcesv1.DevicePluginResourceTypeVF,
				Ranges: []noderesourcesv1.VFRange{{PFIndex: 0}},
			}}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).NotTo(HaveOccurred())
		})
		It("should reject unsupported resource type", func() {
			resources := []noderesourcesv1.DevicePluginResource{{
				Name:   "pods_sf",
				Type:   "sf",
				Ranges: []noderesourcesv1.VFRange{{PFIndex: 0}},
			}}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unsupported resource type"))
		})
		It("should reject empty resource type", func() {
			resources := []noderesourcesv1.DevicePluginResource{{
				Name:   "pods_vf",
				Type:   "",
				Ranges: []noderesourcesv1.VFRange{{PFIndex: 0}},
			}}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unsupported resource type"))
		})
		It("should include resource name in type error message", func() {
			resources := []noderesourcesv1.DevicePluginResource{{
				Name:   "my_custom_resource",
				Type:   "invalid_type",
				Ranges: []noderesourcesv1.VFRange{{PFIndex: 0}},
			}}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("my_custom_resource"))
		})
	})
	Context("resource name uniqueness validation", func() {
		It("should reject duplicate resource names with same prefix", func() {
			resources := []noderesourcesv1.DevicePluginResource{
				{
					Name:           "pods_vf",
					ResourcePrefix: ptr.To("nvidia.com"),
					Type:           noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges:         []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(0)), End: ptr.To(int32(5))}},
				},
				{
					Name:           "pods_vf",
					ResourcePrefix: ptr.To("nvidia.com"),
					Type:           noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges:         []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(6)), End: ptr.To(int32(10))}},
				},
			}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("duplicate resource"))
		})
		It("should reject duplicate resource names with nil prefix", func() {
			resources := []noderesourcesv1.DevicePluginResource{
				{
					Name:   "pods_vf",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(0)), End: ptr.To(int32(5))}},
				},
				{
					Name:   "pods_vf",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(6)), End: ptr.To(int32(10))}},
				},
			}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("duplicate resource"))
		})
		It("should reject conflict between explicit and default prefix", func() {
			resources := []noderesourcesv1.DevicePluginResource{
				{
					Name:   "pods_vf",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(0)), End: ptr.To(int32(5))}},
				},
				{
					Name:           "pods_vf",
					ResourcePrefix: ptr.To(testDefaultResourcePrefix),
					Type:           noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges:         []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(6)), End: ptr.To(int32(10))}},
				},
			}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("duplicate resource"))
		})
	})
	Context("VF range validation", func() {
		It("should reject empty ranges", func() {
			resources := []noderesourcesv1.DevicePluginResource{{
				Name:   "res1",
				Type:   noderesourcesv1.DevicePluginResourceTypeVF,
				Ranges: []noderesourcesv1.VFRange{},
			}}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no VF ranges specified"))
		})
		It("should reject range where start is greater than end", func() {
			resources := []noderesourcesv1.DevicePluginResource{{
				Name:   "res1",
				Type:   noderesourcesv1.DevicePluginResourceTypeVF,
				Ranges: []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(10)), End: ptr.To(int32(5))}},
			}}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("start"))
			Expect(err.Error()).To(ContainSubstring("greater than end"))
		})
	})
	Context("overlapping ranges validation", func() {
		It("should reject overlapping ranges on same PF within same resource", func() {
			resources := []noderesourcesv1.DevicePluginResource{{
				Name: "res1",
				Type: noderesourcesv1.DevicePluginResourceTypeVF,
				Ranges: []noderesourcesv1.VFRange{
					{PFIndex: 0, Start: ptr.To(int32(0)), End: ptr.To(int32(10))},
					{PFIndex: 0, Start: ptr.To(int32(5)), End: ptr.To(int32(15))},
				},
			}}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("overlapping"))
		})
		It("should reject overlapping ranges on same PF across different resources", func() {
			resources := []noderesourcesv1.DevicePluginResource{
				{
					Name:   "res1",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(0)), End: ptr.To(int32(10))}},
				},
				{
					Name:   "res2",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(5)), End: ptr.To(int32(15))}},
				},
			}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("overlapping"))
		})
		It("should reject fully contained range", func() {
			resources := []noderesourcesv1.DevicePluginResource{
				{
					Name:   "res1",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(0)), End: ptr.To(int32(20))}},
				},
				{
					Name:   "res2",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(5)), End: ptr.To(int32(10))}},
				},
			}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("overlapping"))
		})
		It("should reject identical ranges", func() {
			resources := []noderesourcesv1.DevicePluginResource{
				{
					Name:   "res1",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(0)), End: ptr.To(int32(10))}},
				},
				{
					Name:   "res2",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(0)), End: ptr.To(int32(10))}},
				},
			}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("overlapping"))
		})
		It("should reject overlapping single-point ranges", func() {
			resources := []noderesourcesv1.DevicePluginResource{
				{
					Name:   "res1",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(5)), End: ptr.To(int32(5))}},
				},
				{
					Name:   "res2",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(5)), End: ptr.To(int32(5))}},
				},
			}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("overlapping"))
		})
		It("should reject overlapping ranges with nil start", func() {
			resources := []noderesourcesv1.DevicePluginResource{
				{
					Name:   "res1",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0, End: ptr.To(int32(10))}},
				},
				{
					Name:   "res2",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(5)), End: ptr.To(int32(15))}},
				},
			}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("overlapping"))
		})
		It("should reject overlapping ranges with nil end", func() {
			resources := []noderesourcesv1.DevicePluginResource{
				{
					Name:   "res1",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(0)), End: ptr.To(int32(10))}},
				},
				{
					Name:   "res2",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(5))}},
				},
			}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("overlapping"))
		})
		It("should reject overlapping ranges with both nil start and end", func() {
			resources := []noderesourcesv1.DevicePluginResource{
				{
					Name:   "res1",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0}},
				},
				{
					Name:   "res2",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(5)), End: ptr.To(int32(10))}},
				},
			}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("overlapping"))
		})
		It("should include PF index in error message", func() {
			resources := []noderesourcesv1.DevicePluginResource{
				{
					Name:   "res1",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 2, Start: ptr.To(int32(0)), End: ptr.To(int32(10))}},
				},
				{
					Name:   "res2",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 2, Start: ptr.To(int32(5)), End: ptr.To(int32(15))}},
				},
			}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("PF 2"))
		})
		It("should include resource names in overlap error message", func() {
			resources := []noderesourcesv1.DevicePluginResource{
				{
					Name:   "first_resource",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(0)), End: ptr.To(int32(10))}},
				},
				{
					Name:   "second_resource",
					Type:   noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{{PFIndex: 0, Start: ptr.To(int32(5)), End: ptr.To(int32(15))}},
				},
			}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("first_resource"))
			Expect(err.Error()).To(ContainSubstring("second_resource"))
		})
	})
	Context("complex scenarios", func() {
		It("should validate multiple PFs with multiple ranges", func() {
			resources := []noderesourcesv1.DevicePluginResource{
				{
					Name: "res1",
					Type: noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{
						{PFIndex: 0, Start: ptr.To(int32(0)), End: ptr.To(int32(5))},
						{PFIndex: 1, Start: ptr.To(int32(0)), End: ptr.To(int32(5))},
					},
				},
				{
					Name: "res2",
					Type: noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{
						{PFIndex: 0, Start: ptr.To(int32(6)), End: ptr.To(int32(10))},
						{PFIndex: 1, Start: ptr.To(int32(6)), End: ptr.To(int32(10))},
					},
				},
			}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).NotTo(HaveOccurred())
		})
		It("should detect overlap only on affected PF", func() {
			resources := []noderesourcesv1.DevicePluginResource{
				{
					Name: "res1",
					Type: noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{
						{PFIndex: 0, Start: ptr.To(int32(0)), End: ptr.To(int32(10))},
						{PFIndex: 1, Start: ptr.To(int32(0)), End: ptr.To(int32(5))},
					},
				},
				{
					Name: "res2",
					Type: noderesourcesv1.DevicePluginResourceTypeVF,
					Ranges: []noderesourcesv1.VFRange{
						{PFIndex: 0, Start: ptr.To(int32(5)), End: ptr.To(int32(15))},
						{PFIndex: 1, Start: ptr.To(int32(6)), End: ptr.To(int32(10))},
					},
				},
			}
			err := ValidateDevicePluginResources(testDevicePluginResourcesPath, testDefaultResourcePrefix, resources).ToAggregate()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("PF 0"))
		})
	})
	Context("ValidateNodeSRIOVDevicePluginConfig", func() {
		It("should return errors under spec.devicePluginResources", func() {
			cfg := &noderesourcesv1.NodeSRIOVDevicePluginConfig{}
			errs := ValidateNodeSRIOVDevicePluginConfig(testDefaultResourcePrefix, cfg)
			Expect(errs).NotTo(BeEmpty())
			Expect(errs[0].Field).To(Equal("spec.devicePluginResources"))
		})
	})
})
