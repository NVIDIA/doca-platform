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
	"github.com/go-logr/logr"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// NodeUnstageVolume is a handler for NodeUnstageVolume request
func (h *node) NodeUnstageVolume(
	ctx context.Context,
	req *csi.NodeUnstageVolumeRequest) (
	*csi.NodeUnstageVolumeResponse, error) {
	reqLog := logr.FromContextOrDiscard(ctx)
	if req.VolumeId == "" {
		return nil, common.FieldIsRequiredError("VolumeID")
	}
	if req.StagingTargetPath == "" {
		return nil, common.FieldIsRequiredError("StagingTargetPath")
	}
	stagingPath := h.getStagingPath(req.StagingTargetPath, req.VolumeId)
	reqLog = reqLog.WithValues("stagingPath", stagingPath)

	if err := h.mount.UnmountAndRemove(stagingPath); err != nil {
		reqLog.Error(err, "failed to unmount staging path")
		return nil, status.Error(codes.Internal, "failed to unmount staging path")
	}
	reqLog.Info("volume unstaged")
	return &csi.NodeUnstageVolumeResponse{}, nil
}
