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
	"reflect"

	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/handlers/common"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/go-logr/logr"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ValidateVolumeCapabilities is a handler for ValidateVolumeCapabilities request
func (h *controller) ValidateVolumeCapabilities(
	ctx context.Context,
	req *csi.ValidateVolumeCapabilitiesRequest) (
	*csi.ValidateVolumeCapabilitiesResponse, error) {
	reqLog := logr.FromContextOrDiscard(ctx)

	if req.VolumeId == "" {
		return nil, common.FieldIsRequiredError("VolumeId")
	}
	if err := common.ValidateVolumeCapabilities(req.VolumeCapabilities); err != nil {
		return nil, err
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
		return nil, status.Error(codes.NotFound, "volume not found")
	}
	reqLog = reqLog.WithValues("name", apiVolume.GetName())
	reqLog.Info("volume found")
	if !reflect.DeepEqual(req.VolumeContext, getVolumeCtx(apiVolume)) {
		reqLog.Info("volume validation failed, volumeCtx mismatch")
		return &csi.ValidateVolumeCapabilitiesResponse{
			Message: "volumeCtx mismatch",
		}, nil
	}
	if !reflect.DeepEqual(
		convertCSIAccessModesToStorageAPIAccessModes(req.VolumeCapabilities),
		apiVolume.Spec.Request.AccessModes) {
		reqLog.Info("volume validation failed, accessModes mismatch")
		return &csi.ValidateVolumeCapabilitiesResponse{
			Message: "accessModes mismatch",
		}, nil
	}
	if !reflect.DeepEqual(
		convertCSIVolumeMode(req.VolumeCapabilities),
		apiVolume.Spec.Request.VolumeMode) {
		reqLog.Info("volume validation failed, volumeMode mismatch")
		return &csi.ValidateVolumeCapabilitiesResponse{
			Message: "volumeMode mismatch",
		}, nil
	}
	if !reflect.DeepEqual(
		req.Parameters,
		apiVolume.Spec.StorageParameters) {
		reqLog.Info("volume validation failed, parameters mismatch")
		return &csi.ValidateVolumeCapabilitiesResponse{
			Message: "parameters mismatch",
		}, nil
	}

	reqLog.Info("volume is valid")
	return &csi.ValidateVolumeCapabilitiesResponse{
		Confirmed: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
			VolumeContext:      req.VolumeContext,
			VolumeCapabilities: req.VolumeCapabilities,
			Parameters:         req.Parameters,
		},
	}, nil
}
