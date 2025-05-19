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
	"time"

	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/handlers/common"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/go-logr/logr"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

// DeleteVolume is a handler for DeleteVolume request
func (h *controller) DeleteVolume(
	ctx context.Context,
	req *csi.DeleteVolumeRequest) (
	*csi.DeleteVolumeResponse, error) {
	reqLog := logr.FromContextOrDiscard(ctx)

	if req.VolumeId == "" {
		return nil, common.FieldIsRequiredError("VolumeID")
	}
	_, dpuClusterClient, err := h.getClients(ctx)
	if err != nil {
		reqLog.Error(err, "can't retrieve clients for clusters")
		return nil, status.Error(codes.Internal, "failed to get kubernetes clients for target clusters")
	}
	apiVolume, err := getVolumeByID(ctx, dpuClusterClient, req.VolumeId)
	if err != nil {
		reqLog.Error(err, "failed to read volume info")
		return nil, status.Error(codes.Internal, "failed to read volume info")
	}
	if apiVolume == nil {
		reqLog.Info("volume not found")
		return &csi.DeleteVolumeResponse{}, nil
	}
	reqLog = reqLog.WithValues("name", apiVolume.GetName())
	reqLog.Info("volume found")
	if err := dpuClusterClient.Delete(ctx, apiVolume); err != nil {
		if apierrors.IsNotFound(err) {
			reqLog.Info("volume not found")
			return &csi.DeleteVolumeResponse{}, nil
		}
		reqLog.Error(err, "failed to delete volume")
		return nil, status.Error(codes.Internal, "failed to remove volume")
	}

	reqLog.Info("volume marked for deletion, wait for removal")
	err = wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
		if err := dpuClusterClient.Get(ctx,
			types.NamespacedName{Name: apiVolume.Name, Namespace: apiVolume.Namespace}, apiVolume); err != nil {
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			reqLog.Error(err, "failed to read volume while waiting for removal, retry")
		}
		return false, nil
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			reqLog.Info("timeout occurred while waiting for volume deletion")
			return nil, status.Error(codes.DeadlineExceeded, "timeout occurred while waiting for volume deletion")
		} else {
			reqLog.Error(err, "volume deletion failed")
			return nil, status.Error(codes.Internal, "volume deletion failed")
		}
	}
	reqLog.Info("volume removed")
	return &csi.DeleteVolumeResponse{}, nil
}
