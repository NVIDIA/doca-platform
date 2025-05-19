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

package identity

import (
	"context"

	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/common"

	"github.com/container-storage-interface/spec/lib/go/csi"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Identity Handler", func() {
	var (
		handler Handler
		ctx     context.Context
	)

	BeforeEach(func() {
		var err error
		handler, err = New()
		Expect(err).NotTo(HaveOccurred())
		ctx = context.Background()
	})

	Describe("GetPluginInfo", func() {
		It("should return plugin name and vendor version", func() {
			req := &csi.GetPluginInfoRequest{}
			resp, err := handler.GetPluginInfo(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).NotTo(BeNil())
			Expect(resp.Name).To(Equal(common.PluginName))
			Expect(resp.VendorVersion).To(Equal(common.VendorVersion))
		})
	})

	Describe("GetPluginCapabilities", func() {
		It("should return controller service capability", func() {
			req := &csi.GetPluginCapabilitiesRequest{}
			resp, err := handler.GetPluginCapabilities(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).NotTo(BeNil())
			Expect(resp.Capabilities).To(HaveLen(1))
			Expect(resp.Capabilities[0].GetService().GetType()).To(Equal(csi.PluginCapability_Service_CONTROLLER_SERVICE))
		})
	})

	Describe("Probe", func() {
		It("should always return ready as true", func() {
			req := &csi.ProbeRequest{}
			resp, err := handler.Probe(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).NotTo(BeNil())
			Expect(resp.Ready).NotTo(BeNil())
			Expect(resp.Ready.Value).To(BeTrue())
		})
	})
})
