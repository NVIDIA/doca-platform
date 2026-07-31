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
	"strconv"

	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/handlers/common"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/go-logr/logr"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

// ControllerPublishVolume is a handler for ControllerPublishVolume request
func (h *controller) ControllerPublishVolume(
	ctx context.Context,
	req *csi.ControllerPublishVolumeRequest) (
	*csi.ControllerPublishVolumeResponse, error) {
	reqLog := logr.FromContextOrDiscard(ctx)
	if req.VolumeId == "" {
		return nil, common.FieldIsRequiredError("VolumeId")
	}
	if req.NodeId == "" {
		return nil, common.FieldIsRequiredError("NodeId")
	}

	if err := common.ValidateVolumeCapability(req.VolumeCapability); err != nil {
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

	dpuNodeName := req.NodeId
	attachmentName := generateDPUVolumeAttachmentName(dpuNodeName, volumeName)

	reqLog = reqLog.WithValues("name", attachmentName)
	functionTypeConfig, err := getFunctionTypeConfig(reqLog, req.VolumeContext)
	if err != nil {
		return nil, err
	}
	desiredVolAttach := &storagev1.DPUVolumeAttachment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      attachmentName,
			Namespace: h.controllerConfig.Namespace,
		},
		Spec: storagev1.DPUVolumeAttachmentSpec{
			DPUNodeName:        dpuNodeName,
			DPUVolumeName:      volumeName,
			FunctionTypeConfig: functionTypeConfig,
		},
	}

	apiVolAttach := &storagev1.DPUVolumeAttachment{}
	if err := client.Create(ctx, desiredVolAttach); err != nil {
		if apierrors.IsAlreadyExists(err) {
			if err := client.Get(ctx,
				types.NamespacedName{Name: desiredVolAttach.Name, Namespace: desiredVolAttach.Namespace}, apiVolAttach); err != nil {
				reqLog.Error(err, "failed to read existing DPUVolumeAttachment")
				return nil, status.Error(codes.Internal, "failed to read DPUVolumeAttachment info from the cluster")
			}
			if err := controllerPublishValidateExisting(reqLog, desiredVolAttach, apiVolAttach); err != nil {
				return nil, err
			}
			reqLog.Info("DPUVolumeAttachment object already exist with expected settings")
		} else {
			reqLog.Error(err, "failed to create DPUVolumeAttachment")
			return nil, status.Error(codes.Internal, "failed to create DPUVolumeAttachment")
		}
	}

	reqLog.Info("wait for volume attachment")
	err = wait.PollUntilContextTimeout(ctx, waitPoolInterval, waitHardTimeout, true, func(ctx context.Context) (bool, error) {
		// here we poll cache, no calls to the API happen
		if err := client.Get(ctx,
			types.NamespacedName{Name: desiredVolAttach.Name, Namespace: desiredVolAttach.Namespace}, apiVolAttach); err != nil {
			if !apierrors.IsNotFound(err) {
				reqLog.Error(err, "failed to read DPUVolumeAttachment while waiting for creation, retry")
			}
			return false, nil
		}
		if apiVolAttach.Status.DPUAttached != nil && *apiVolAttach.Status.DPUAttached {
			reqLog.Info("volume attached")
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			reqLog.Info("timeout occurred while waiting for volume attachment")
			return nil, status.Error(codes.DeadlineExceeded, "timeout occurred while waiting for volume attachment")
		} else {
			reqLog.Error(err, "volume attachment failed")
			return nil, status.Error(codes.Internal, "volume attachment failed")
		}
	}

	publishCtx := map[string]string{
		common.PublishCtxNvVolumeName:           apiVolume.GetName(),
		common.PublishCtxNvVolumeAttachmentName: apiVolAttach.GetName(),
	}

	if apiVolAttach.Status.DPU != nil && apiVolAttach.Status.DPU.PCIAddress != nil {
		publishCtx[common.PublishCtxDevicePciAddress] = *apiVolAttach.Status.DPU.PCIAddress
	}

	if apiVolAttach.Status.DPU != nil && apiVolAttach.Status.DPU.NVMEAttrs != nil && apiVolAttach.Status.DPU.NVMEAttrs.NamespaceID != nil {
		publishCtx[common.PublishCtxNvmeNsID] = strconv.FormatInt(*apiVolAttach.Status.DPU.NVMEAttrs.NamespaceID, 10)
	}

	for k, v := range apiVolAttach.Status.AttachmentMetadata {
		if k == common.PublishCtxNvVolumeName || k == common.PublishCtxNvVolumeAttachmentName {
			reqLog.Error(nil, "volume attachment parameters contain forbidden field", "field", k)
			return nil, status.Error(codes.Internal, "volume attachment parameters contain forbidden field: "+k)
		}
		publishCtx[k] = v
	}
	return &csi.ControllerPublishVolumeResponse{PublishContext: publishCtx}, nil
}

// validate that existing DPUVolumeAttachment has the same parameters as requested
func controllerPublishValidateExisting(reqLog logr.Logger, desired, actual *storagev1.DPUVolumeAttachment) error {
	if !equality.Semantic.DeepEqual(desired.Spec, actual.Spec) {
		reqLog.Info("DPUVolumeAttachment has different parameters",
			"desired", desired.Spec,
			"actual", actual.Spec)
		return status.Error(codes.AlreadyExists, "DPUVolumeAttachment already exist but contains different parameters")
	}
	return nil
}

// getFunctionTypeConfig constructs a FunctionTypeConfig from volume context parameters.
func getFunctionTypeConfig(reqLog logr.Logger, volCtx map[string]string) (storagev1.FunctionTypeConfig, error) {
	functionTypeConfig, err := common.FunctionTypeConfigFromStrings(volCtx[common.VolumeCtxFunctionType], volCtx[common.VolumeCtxHotplugFunction])
	if err != nil {
		reqLog.Error(err, "invalid function type config")
		return storagev1.FunctionTypeConfig{}, status.Error(codes.InvalidArgument, "invalid function type config")
	}
	return functionTypeConfig, nil
}
