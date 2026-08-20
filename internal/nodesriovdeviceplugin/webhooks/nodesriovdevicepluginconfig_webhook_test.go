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

package webhooks

import (
	"context"

	noderesourcesv1 "github.com/nvidia/doca-platform/api/noderesources/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

const testDefaultResourcePrefix = "test.com"

var _ = Describe("NodeSRIOVDevicePluginConfigValidator", func() {
	Context("type validation", func() {
		It("should reject non-NodeSRIOVDevicePluginConfig objects", func() {
			v := &NodeSRIOVDevicePluginConfigValidator{DefaultResourcePrefix: testDefaultResourcePrefix}
			warnings, err := v.ValidateCreate(context.Background(), &corev1.Pod{})
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsBadRequest(err)).To(BeTrue())
			Expect(warnings).To(BeEmpty())
		})
	})
	Context("spec validation", func() {
		It("should accept a minimal valid configuration", func() {
			v := &NodeSRIOVDevicePluginConfigValidator{DefaultResourcePrefix: testDefaultResourcePrefix}
			cfg := &noderesourcesv1.NodeSRIOVDevicePluginConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "cfg"},
				Spec: noderesourcesv1.NodeSRIOVDevicePluginConfigSpec{
					DevicePluginResources: []noderesourcesv1.DevicePluginResource{{
						Name:   "pods_vf",
						Type:   noderesourcesv1.DevicePluginResourceTypeVF,
						Ranges: []noderesourcesv1.FunctionRange{{PFIndex: 0, Start: ptr.To(int32(0)), End: ptr.To(int32(1))}},
					}},
				},
			}
			warnings, err := v.ValidateCreate(context.Background(), cfg)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeNil())
		})
		It("should accept a valid SF configuration", func() {
			v := &NodeSRIOVDevicePluginConfigValidator{DefaultResourcePrefix: testDefaultResourcePrefix}
			cfg := &noderesourcesv1.NodeSRIOVDevicePluginConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "cfg"},
				Spec: noderesourcesv1.NodeSRIOVDevicePluginConfigSpec{
					DevicePluginResources: []noderesourcesv1.DevicePluginResource{{
						Name:   "pods_sf",
						Type:   noderesourcesv1.DevicePluginResourceTypeSF,
						Ranges: []noderesourcesv1.FunctionRange{{PFIndex: 0, Start: ptr.To(int32(0)), End: ptr.To(int32(7))}},
					}},
				},
			}
			warnings, err := v.ValidateCreate(context.Background(), cfg)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeNil())
		})
		It("should return Invalid for an invalid configuration", func() {
			v := &NodeSRIOVDevicePluginConfigValidator{DefaultResourcePrefix: testDefaultResourcePrefix}
			cfg := &noderesourcesv1.NodeSRIOVDevicePluginConfig{ObjectMeta: metav1.ObjectMeta{Name: "cfg"}}
			warnings, err := v.ValidateUpdate(context.Background(), cfg, cfg)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
			Expect(warnings).To(BeNil())
		})
	})
	It("should do nothing on delete", func() {
		v := &NodeSRIOVDevicePluginConfigValidator{DefaultResourcePrefix: testDefaultResourcePrefix}
		warnings, err := v.ValidateDelete(context.Background(), &noderesourcesv1.NodeSRIOVDevicePluginConfig{})
		Expect(err).NotTo(HaveOccurred())
		Expect(warnings).To(BeNil())
	})
})
