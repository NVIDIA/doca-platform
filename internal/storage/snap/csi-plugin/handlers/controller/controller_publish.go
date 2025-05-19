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
	"time"

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

const (
	// this label is added to the VolumeAttachment object
	VolumeAttachmentVolumeIDLabel = "volumeId"
)

// ControllerPublishVolume is a handler for ControllerPublishVolume request
// currently not implemented
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

	hostClusterClient, dpuClusterClient, err := h.getClients(ctx)
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
		return &csi.ControllerPublishVolumeResponse{}, status.Error(codes.NotFound, "volume not found")
	}
	reqLog = reqLog.WithValues("name", apiVolume.GetName())
	reqLog.Info("volume found")

	dpuNodeName, err := getDPUNameByNodeName(ctx, hostClusterClient, req.NodeId)
	if err != nil {
		reqLog.Error(err, "failed to resolve DPU to node mapping")
		return nil, status.Error(codes.Internal, "failed to read volume info")
	}
	if dpuNodeName == "" {
		reqLog.Info("dpu node not found")
		return nil, status.Error(codes.NotFound, "dpu node not found")
	}
	attachmentName := generateVolumeAttachmentName(h.cfg.TargetNamespace, dpuNodeName, req.VolumeId)
	reqLog = reqLog.WithValues("name", attachmentName)
	desiredVolAttach := &storagev1.VolumeAttachment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      attachmentName,
			Namespace: h.cfg.TargetNamespace,
			Labels:    map[string]string{VolumeAttachmentVolumeIDLabel: req.VolumeId},
		},
		Spec: storagev1.VolumeAttachmentSpec{
			NodeName: dpuNodeName,
			Source: storagev1.VolumeSource{VolumeRef: &storagev1.ObjectRef{
				Kind:       storagev1.VolumeKind,
				APIVersion: storagev1.GroupVersion.String(),
				Namespace:  h.cfg.TargetNamespace,
				Name:       apiVolume.GetName(),
			}},
		},
	}
	apiVolAttach := &storagev1.VolumeAttachment{}
	if err := dpuClusterClient.Create(ctx, desiredVolAttach); err != nil {
		if apierrors.IsAlreadyExists(err) {
			if err := dpuClusterClient.Get(ctx,
				types.NamespacedName{Name: desiredVolAttach.Name, Namespace: desiredVolAttach.Namespace}, apiVolAttach); err != nil {
				reqLog.Error(err, "failed to read existing VolumeAttachment")
				return nil, status.Error(codes.Internal, "failed to read VolumeAttachment info from the dpu cluster")
			}
			if err := controllerPublishValidateExisting(reqLog, desiredVolAttach, apiVolAttach); err != nil {
				return nil, err
			}
			reqLog.Info("VolumeAttachment object already exist with expected settings")
		}
	}
	reqLog.Info("wait for volume attachment")
	err = wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
		// here we poll cache, no calls to the API happen
		if err := dpuClusterClient.Get(ctx,
			types.NamespacedName{Name: desiredVolAttach.Name, Namespace: desiredVolAttach.Namespace}, apiVolAttach); err != nil {
			if !apierrors.IsNotFound(err) {
				reqLog.Error(err, "failed to read VolumeAttachment while waiting for creation, retry")
			}
			return false, nil
		}
		if apiVolAttach.Status.DPU.Attached {
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
		common.PublishCtxDevicePciAddress:       apiVolAttach.Status.DPU.PCIDeviceAddress,
		common.PublishCtxNvmeNsID:               strconv.FormatInt(apiVolAttach.Status.DPU.BdevAttrs.NVMeNsID, 10),
	}
	for k, v := range apiVolAttach.Spec.Parameters {
		if k == common.PublishCtxNvVolumeName || k == common.PublishCtxNvVolumeAttachmentName {
			reqLog.Error(nil, "volume attachment parameters contain forbidden field", "field", k)
			return nil, status.Error(codes.Internal, "volume attachment parameters contain forbidden field: "+k)
		}
		publishCtx[k] = v
	}
	return &csi.ControllerPublishVolumeResponse{PublishContext: publishCtx}, nil
}

// validate that existing VolumeAttachment has the same parameters as requested
func controllerPublishValidateExisting(reqLog logr.Logger, desired, actual *storagev1.VolumeAttachment) error {
	if desired.Spec.NodeName != actual.Spec.NodeName {
		// this should never happen
		reqLog.Info("VolumeAttachment has different nodeName",
			"desired", desired.Spec.NodeName,
			"actual", actual.Spec.NodeName)
		return status.Error(codes.AlreadyExists, "VolumeAttachment already exist but contains different nodeName")
	}

	if !equality.Semantic.DeepEqual(desired.Spec.Source, actual.Spec.Source) {
		reqLog.Info("VolumeAttachment has different source",
			"desired", desired.Spec.Source,
			"actual", actual.Spec.Source)
		return status.Error(codes.AlreadyExists, "VolumeAttachment already exist but with different source")
	}
	return nil
}
