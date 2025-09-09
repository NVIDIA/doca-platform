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
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/handlers/common"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// NodePublishVolume is a handler for NodePublishVolume request
func (h *node) NodePublishVolume(ctx context.Context,
	req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	if req.VolumeId == "" {
		return nil, common.FieldIsRequiredError("VolumeID")
	}

	if req.StagingTargetPath == "" {
		return nil, common.FieldIsRequiredError("StagingTargetPath")
	}

	if req.TargetPath == "" {
		return nil, common.FieldIsRequiredError("TargetPath")
	}

	if err := common.ValidateVolumeCapability(h.commonConfig.EmulationMode, req.VolumeCapability); err != nil {
		return nil, err
	}
	switch h.commonConfig.EmulationMode {
	case config.EmulationModeNVMe:
		return h.publishNVMe(ctx, req)
	case config.EmulationModeVirtiofs:
		return h.publishVirtioFS(ctx, req)
	default:
		return nil, status.Error(codes.Unimplemented, "unsupported emulation mode selected")
	}
}
