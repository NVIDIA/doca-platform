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

package node

import (
	"context"

	"github.com/container-storage-interface/spec/lib/go/csi"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("NodeGetCapabilities", func() {
	var (
		nodeHandler *node
	)

	BeforeEach(func() {
		nodeHandler = &node{}
	})

	It("should return NodeServiceCapability with RPC_STAGE_UNSTAGE_VOLUME", func() {
		resp, err := nodeHandler.NodeGetCapabilities(context.Background(), &csi.NodeGetCapabilitiesRequest{})

		Expect(err).NotTo(HaveOccurred())
		Expect(resp).NotTo(BeNil())
		Expect(resp.Capabilities).To(HaveLen(1))

		capability := resp.Capabilities[0]
		Expect(capability.GetRpc()).NotTo(BeNil())
		Expect(capability.GetRpc().Type).To(Equal(csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME))
	})
})
