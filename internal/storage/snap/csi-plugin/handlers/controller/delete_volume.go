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
	client, err := h.clusterhelper.GetClient(ctx)
	if err != nil {
		reqLog.Error(err, "can't retrieve client for cluster")
		return nil, status.Error(codes.Internal, "failed to get kubernetes client for target cluster")
	}

	// Get the DPUVolume by name since VolumeID is the name
	volumeName := req.VolumeId
	apiVolume := &storagev1.DPUVolume{}
	if err := client.Get(ctx, types.NamespacedName{Name: volumeName, Namespace: h.cfg.Namespace}, apiVolume); err != nil {
		if apierrors.IsNotFound(err) {
			reqLog.Info("volume not found")
			return &csi.DeleteVolumeResponse{}, nil
		}
		reqLog.Error(err, "failed to read volume info")
		return nil, status.Error(codes.Internal, "failed to read volume info")
	}

	reqLog = reqLog.WithValues("name", apiVolume.GetName())
	reqLog.Info("volume found")
	if err := client.Delete(ctx, apiVolume); err != nil {
		if apierrors.IsNotFound(err) {
			reqLog.Info("volume not found")
			return &csi.DeleteVolumeResponse{}, nil
		}
		reqLog.Error(err, "failed to delete volume")
		return nil, status.Error(codes.Internal, "failed to remove volume")
	}

	reqLog.Info("volume marked for deletion, wait for removal")
	err = wait.PollUntilContextTimeout(ctx, waitPoolInterval, waitHardTimeout, true, func(ctx context.Context) (bool, error) {
		if err := client.Get(ctx,
			types.NamespacedName{Name: volumeName, Namespace: h.cfg.Namespace}, apiVolume); err != nil {
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
