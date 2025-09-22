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

	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/handlers/common"

	"github.com/container-storage-interface/spec/lib/go/csi"
)

// NodeGetInfo is a handler for NodeGetInfo request
// the plugin uses Kubernetes node name as a CSI nodeID
func (h *node) NodeGetInfo(
	ctx context.Context,
	req *csi.NodeGetInfoRequest) (
	*csi.NodeGetInfoResponse, error) {
	return &csi.NodeGetInfoResponse{
		NodeId:            h.nodeConfig.NodeID,
		MaxVolumesPerNode: h.getMaxVolumesPerNode(),
	}, nil
}

// getMaxVolumesPerNode returns the maximum number of volumes that can be published to a node.
// The value is determined in the following order of precedence:
// 1. User-configured value if non-zero
// 2. Dynamically calculated value from runtime config if non-zero
// 3. Default fallback value (for hot-plugged PF scenarios mostly)
func (h *node) getMaxVolumesPerNode() int64 {
	if h.nodeConfig.MaxVolumesPerNode > 0 {
		return h.nodeConfig.MaxVolumesPerNode
	}
	if h.runtimeCfg.GetMaxVolumesPerNode() > 0 {
		return h.runtimeCfg.GetMaxVolumesPerNode()
	}
	return common.DefaultMaxVolumesPerNode
}
