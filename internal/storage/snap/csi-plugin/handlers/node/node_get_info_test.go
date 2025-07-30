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

	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/config"

	"github.com/container-storage-interface/spec/lib/go/csi"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("NodeGetInfo", func() {
	var (
		nodeHandler *node
	)

	BeforeEach(func() {
		nodeHandler = &node{
			cfg:        config.Node{NodeID: "test-node-id"},
			runtimeCfg: config.NewNodeRuntime(),
		}
	})

	It("should return NodeID from configuration", func() {
		resp, err := nodeHandler.NodeGetInfo(context.Background(), &csi.NodeGetInfoRequest{})
		Expect(err).NotTo(HaveOccurred())
		Expect(resp).NotTo(BeNil())
		Expect(resp.NodeId).To(Equal("test-node-id"))
	})

	It("should return MaxVolumesPerNode from runtime configuration", func() {
		nodeHandler.runtimeCfg.SetMaxVolumesPerNode(int64(100))
		resp, err := nodeHandler.NodeGetInfo(context.Background(), &csi.NodeGetInfoRequest{})
		Expect(err).NotTo(HaveOccurred())
		Expect(resp).NotTo(BeNil())
		Expect(resp.MaxVolumesPerNode).To(Equal(int64(100)))
	})
})
