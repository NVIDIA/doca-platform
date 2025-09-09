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
	"slices"

	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/handlers/common"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/go-logr/logr"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// publishVirtioFS implements publish volume logic for VirtioFS emulation mode
func (h *node) publishVirtioFS(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	reqLog := logr.FromContextOrDiscard(ctx)

	virtiofsTag := req.PublishContext[common.PublishCtxVirtioFsTag]
	if virtiofsTag == "" {
		return nil, common.FieldIsRequiredError("PublishContext.virtioFSTag")
	}
	reqLog = reqLog.WithValues("targetPath", req.TargetPath, "virtiofsTag", virtiofsTag)

	if err := h.mount.EnsureDirExist(req.TargetPath, 0755); err != nil {
		reqLog.Error(err, "can't create publish path for the volume")
		return nil, status.Error(codes.Internal, "can't create publish path for the volume")
	}
	// it is expected that validation is done before this point, so we can safely cast to MountVolumeCapability
	mountOptions := slices.Clone(req.VolumeCapability.AccessType.(*csi.VolumeCapability_Mount).Mount.MountFlags)
	if h.shouldPublishAsReadOnly(req) && !slices.Contains(mountOptions, "ro") {
		mountOptions = append(mountOptions, "ro")
	}
	slices.Sort(mountOptions)
	exist, existingMountOptions, err := h.mount.CheckMountExists(virtiofsTag, req.TargetPath)
	if err != nil {
		reqLog.Error(err, "error occurred while checking if the volume is published")
		return nil, status.Error(codes.Internal, "error occurred while checking if the volume is published")
	}
	if exist {
		slices.Sort(existingMountOptions.MountOptions)
		if slices.Equal(existingMountOptions.MountOptions, mountOptions) {
			reqLog.Info("volume already published")
			return &csi.NodePublishVolumeResponse{}, nil
		}
		reqLog.Error(nil, "volume already published with different mount options", "existingMountOptions", existingMountOptions.MountOptions, "mountOptions", mountOptions)
		return nil, status.Error(codes.AlreadyExists, "volume already published with different mount options")
	}
	reqLog.Info("mounting volume", "mountOptions", mountOptions)
	if err := h.mount.Mount(virtiofsTag, req.TargetPath, h.nodeConfig.VirtiofsFSTypeName, mountOptions); err != nil {
		reqLog.Error(err, "error occurred while trying to mount volume", "virtiofsTag", virtiofsTag)
		return nil, status.Error(codes.Internal, "error occurred while trying to mount volume")
	}
	reqLog.Info("volume published")
	return &csi.NodePublishVolumeResponse{}, nil
}

// returns true if the volume should be published as read only
func (h *node) shouldPublishAsReadOnly(req *csi.NodePublishVolumeRequest) bool {
	if req.Readonly {
		return true
	}
	switch req.VolumeCapability.AccessMode.Mode {
	case csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY, csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY:
		return true
	}
	return false
}
