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

	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/handlers/common"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/go-logr/logr"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
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

	client, err := h.clusterhelper.GetClient(ctx)
	if err != nil {
		reqLog.Error(err, "can't retrieve client for cluster")
		return nil, status.Error(codes.Internal, "failed to get kubernetes client for target cluster")
	}

	// Get DPUVolume by name since VolumeID is the name
	volumeName := req.VolumeId
	apiVolume := &storagev1.DPUVolume{}
	if err := client.Get(ctx, types.NamespacedName{Name: volumeName, Namespace: h.controllerConfig.Namespace}, apiVolume); err != nil {
		if apierrors.IsNotFound(err) {
			reqLog.Info("volume not found")
			return nil, status.Error(codes.NotFound, "volume not found")
		}
		reqLog.Error(err, "failed to read volume info")
		return nil, status.Error(codes.Internal, "failed to read volume info")
	}

	reqLog = reqLog.WithValues("name", apiVolume.GetName())
	reqLog.Info("volume found")

	volumeCtx, err := getCSIVolumeCtx(req.Parameters, apiVolume)
	if err != nil {
		reqLog.Error(err, "failed to get volume context")
		return nil, status.Error(codes.Internal, "failed to get volume context")
	}
	if !equality.Semantic.DeepEqual(req.VolumeContext, volumeCtx) {
		reqLog.Info("volume validation failed, volumeCtx mismatch")
		return &csi.ValidateVolumeCapabilitiesResponse{
			Message: "volumeCtx mismatch",
		}, nil
	}
	if !equality.Semantic.DeepEqual(
		convertCSIAccessModesToStorageAPIAccessModes(req.VolumeCapabilities),
		apiVolume.Spec.AccessModes) {
		reqLog.Info("volume validation failed, accessModes mismatch")
		return &csi.ValidateVolumeCapabilitiesResponse{
			Message: "accessModes mismatch",
		}, nil
	}
	if !equality.Semantic.DeepEqual(
		convertCSIVolumeMode(req.VolumeCapabilities),
		apiVolume.Spec.VolumeMode) {
		reqLog.Info("volume validation failed, volumeMode mismatch")
		return &csi.ValidateVolumeCapabilitiesResponse{
			Message: "volumeMode mismatch",
		}, nil
	}
	if !equality.Semantic.DeepEqual(
		req.Parameters,
		apiVolume.Spec.Parameters) {
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
