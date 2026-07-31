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

// NodePublishVolume is a handler for NodePublishVolume request
func (h *node) NodePublishVolume(ctx context.Context,
	req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	reqLog := logr.FromContextOrDiscard(ctx)
	if req.VolumeId == "" {
		return nil, common.FieldIsRequiredError("VolumeID")
	}

	if req.StagingTargetPath == "" {
		return nil, common.FieldIsRequiredError("StagingTargetPath")
	}

	if req.TargetPath == "" {
		return nil, common.FieldIsRequiredError("TargetPath")
	}

	if err := common.ValidateVolumeCapability(req.VolumeCapability); err != nil {
		return nil, err
	}

	if req.Readonly {
		return nil, status.Error(codes.Unimplemented, "readOnly volumes are not supported")
	}

	stagingPath := h.getStagingPath(req.StagingTargetPath, req.VolumeId)
	reqLog = reqLog.WithValues("stagingPath", stagingPath, "targetPath", req.TargetPath)

	if err := h.mount.EnsureFileExist(req.TargetPath, 0644); err != nil {
		reqLog.Error(err, "can't create publish path for the volume")
		return nil, status.Error(codes.Internal, "can't create staging path for the volume")
	}
	exist, _, err := h.mount.CheckMountExists(stagingPath, req.TargetPath)
	if err != nil {
		reqLog.Error(err, "error occurred while checking if the volume is published")
		return nil, status.Error(codes.Internal, "error occurred while checking if the volume is published")
	}
	if exist {
		reqLog.Info("volume already published")
		return &csi.NodePublishVolumeResponse{}, nil
	}
	if err := h.mount.Mount(stagingPath, req.TargetPath, "", []string{"bind"}); err != nil {
		reqLog.Error(err, "failed to publish volume, bind mount failed")
		return nil, status.Error(codes.Internal, "failed to publish volume, bind mount failed")
	}
	reqLog.Info("volume published")
	return &csi.NodePublishVolumeResponse{}, nil
}
