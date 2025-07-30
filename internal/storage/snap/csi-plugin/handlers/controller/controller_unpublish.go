/*
Copyright 2024 NVIDIA

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
	"errors"

	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/handlers/common"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/go-logr/logr"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

// ControllerUnpublishVolume is a handler for ControllerUnpublishVolume request
func (h *controller) ControllerUnpublishVolume(
	ctx context.Context,
	req *csi.ControllerUnpublishVolumeRequest) (
	*csi.ControllerUnpublishVolumeResponse, error) {

	reqLog := logr.FromContextOrDiscard(ctx)
	if req.VolumeId == "" {
		return nil, common.FieldIsRequiredError("VolumeId")
	}
	if req.NodeId == "" {
		return nil, common.FieldIsRequiredError("NodeId")
	}

	client, err := h.clusterhelper.GetClient(ctx)
	if err != nil {
		reqLog.Error(err, "can't retrieve client for cluster")
		return nil, status.Error(codes.Internal, "failed to get kubernetes client for target cluster")
	}

	// Get DPUVolume by name since VolumeID is the name
	volumeName := req.VolumeId
	apiVolume := &storagev1.DPUVolume{}
	if err := client.Get(ctx, types.NamespacedName{Name: volumeName, Namespace: h.cfg.Namespace}, apiVolume); err != nil {
		if apierrors.IsNotFound(err) {
			reqLog.Info("volume not found")
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		}
		reqLog.Error(err, "failed to read volume info")
		return nil, status.Error(codes.Internal, "failed to read volume info")
	}

	reqLog = reqLog.WithValues("name", apiVolume.GetName())
	reqLog.Info("volume found")

	dpuNodeName := req.NodeId
	attachmentName := generateDPUVolumeAttachmentName(dpuNodeName, volumeName)

	reqLog = reqLog.WithValues("name", attachmentName)

	desiredVolAttach := &storagev1.DPUVolumeAttachment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      attachmentName,
			Namespace: h.cfg.Namespace,
		},
	}

	if err := client.Delete(ctx, desiredVolAttach); err != nil {
		if apierrors.IsNotFound(err) {
			reqLog.Info("volume attachment not found")
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		}
		reqLog.Error(err, "failed to delete volume attachment")
		return nil, status.Error(codes.Internal, "failed to remove volume attachment")
	}

	reqLog.Info("volume attachment marked for deletion, wait for removal")
	err = wait.PollUntilContextTimeout(ctx, waitPoolInterval, waitHardTimeout, true, func(ctx context.Context) (bool, error) {
		if err := client.Get(ctx,
			types.NamespacedName{Name: desiredVolAttach.Name, Namespace: desiredVolAttach.Namespace}, desiredVolAttach); err != nil {
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			reqLog.Error(err, "failed to read volume attachment while waiting for removal, retry")
		}
		return false, nil
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			reqLog.Info("timeout occurred while waiting for volume attachment deletion")
			return nil, status.Error(codes.DeadlineExceeded, "timeout occurred while waiting for volume attachment deletion")
		} else {
			reqLog.Error(err, "volume attachment deletion failed")
			return nil, status.Error(codes.Internal, "volume attachment deletion failed")
		}
	}
	reqLog.Info("volume attachment removed")

	return &csi.ControllerUnpublishVolumeResponse{}, nil
}
