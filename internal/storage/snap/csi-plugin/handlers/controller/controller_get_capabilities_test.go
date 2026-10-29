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
	"context"

	"github.com/container-storage-interface/spec/lib/go/csi"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ControllerGetCapabilities", func() {
	var (
		controllerHandler *controller
	)

	BeforeEach(func() {
		controllerHandler = &controller{}
	})

	It("should return expected ControllerServiceCapability", func() {
		resp, err := controllerHandler.ControllerGetCapabilities(context.Background(), &csi.ControllerGetCapabilitiesRequest{})
		Expect(err).NotTo(HaveOccurred())
		Expect(resp).NotTo(BeNil())
		Expect(resp.Capabilities).To(HaveLen(2))
		Expect(resp.Capabilities[0].GetRpc()).NotTo(BeNil())
		Expect(resp.Capabilities[0].GetRpc().Type).To(Equal(csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME))
		Expect(resp.Capabilities[1].GetRpc()).NotTo(BeNil())
		Expect(resp.Capabilities[1].GetRpc().Type).To(Equal(csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME))
	})
})
