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
	"reflect"
	"time"

	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/config"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/handlers/common"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/go-logr/logr"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	// DefaultVolumeCapacityBytes default capacity for volumes if no specified explicitly
	DefaultVolumeCapacityBytes int64 = 1073741824 // 1 GiB
)

// CreateVolume is a handler for CreateVolume request
func (h *controller) CreateVolume(
	ctx context.Context,
	req *csi.CreateVolumeRequest) (
	*csi.CreateVolumeResponse, error) {
	reqLog := logr.FromContextOrDiscard(ctx)
	if req.Name == "" {
		return nil, common.FieldIsRequiredError("Name")
	}
	if err := common.ValidateVolumeCapabilities(req.VolumeCapabilities); err != nil {
		return nil, err
	}
	if req.VolumeContentSource != nil {
		return nil, status.Error(codes.InvalidArgument, "volume content source is not supported")
	}

	if req.Parameters["policy"] == "" {
		return nil, common.FieldIsRequiredError("Parameters.policy")
	}
	_, dpuClusterClient, err := h.getClients(ctx)
	if err != nil {
		reqLog.Error(err, "can't retrieve clients for clusters")
		return nil, status.Error(codes.Internal, "failed to get kubernetes clients for target clusters")
	}

	desiredVolume := csiCreateVolumeRequestToStorageAPIVolume(h.cfg, req)
	apiVolume := &storagev1.Volume{}
	if err := dpuClusterClient.Create(ctx, desiredVolume); err != nil {
		if apierrors.IsAlreadyExists(err) {
			if err := dpuClusterClient.Get(ctx,
				types.NamespacedName{Name: desiredVolume.Name, Namespace: desiredVolume.Namespace}, apiVolume); err != nil {
				reqLog.Error(err, "failed to read existing volume")
				return nil, status.Error(codes.Internal, "failed to read volume info from the dpu cluster")
			}
			if err := createVolumeValidateExisting(reqLog, desiredVolume, apiVolume); err != nil {
				return nil, err
			}
			reqLog.Info("volume object already exist with expected settings")
		}
	}
	reqLog.Info("wait for volume provisioning")
	err = wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
		// here we poll cache, no calls to the API happen
		if err := dpuClusterClient.Get(ctx,
			types.NamespacedName{Name: desiredVolume.Name, Namespace: desiredVolume.Namespace}, apiVolume); err != nil {
			if !apierrors.IsNotFound(err) {
				reqLog.Error(err, "failed to read volume while waiting for creation, retry")
			}
			return false, nil
		}
		if apiVolume.Status.State == storagev1.VolumeStateAvailable {
			reqLog.Info("volume provisioned")
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			reqLog.Info("timeout occurred while waiting for volume creation")
			return nil, status.Error(codes.DeadlineExceeded, "timeout occurred while waiting for volume creation")
		} else {
			reqLog.Error(err, "volume creation failed")
			return nil, status.Error(codes.Internal, "volume creation failed")
		}
	}

	capBytes, _ := apiVolume.Spec.VolumeSpecDPU.Capacity.AsInt64()
	return &csi.CreateVolumeResponse{Volume: &csi.Volume{
		VolumeId:      string(apiVolume.UID),
		CapacityBytes: capBytes,
		VolumeContext: getVolumeCtx(apiVolume),
	}}, nil
}

// validate that existing volume has the same parameters as requested
func createVolumeValidateExisting(reqLog logr.Logger, desired, actual *storagev1.Volume) error {
	if !reflect.DeepEqual(desired.Spec.StorageParameters, actual.Spec.StorageParameters) {
		reqLog.Info("volume exist with different parameters",
			"desired", desired.Spec.StorageParameters,
			"actual", actual.Spec.StorageParameters)
		return status.Error(codes.AlreadyExists, "volume already exist but with different storage parameters")
	}

	if !equality.Semantic.DeepEqual(desired.Spec.Request, actual.Spec.Request) {
		reqLog.Info("volume exist with different request",
			"desired", desired.Spec.Request,
			"actual", actual.Spec.Request)
		return status.Error(codes.AlreadyExists, "volume already exist but with different request")
	}
	return nil
}

// convert csi CreateVolume request arguments to storageAPI Volume
func csiCreateVolumeRequestToStorageAPIVolume(cfg config.Controller, createReq *csi.CreateVolumeRequest) *storagev1.Volume {
	requiredBytes := DefaultVolumeCapacityBytes
	if createReq.CapacityRange != nil && createReq.CapacityRange.RequiredBytes > 0 {
		requiredBytes = createReq.CapacityRange.RequiredBytes
	}
	limitBytes := int64(0)
	if createReq.CapacityRange != nil && createReq.CapacityRange.LimitBytes > 0 {
		limitBytes = createReq.CapacityRange.LimitBytes
	}
	return &storagev1.Volume{
		ObjectMeta: metav1.ObjectMeta{
			Name:      createReq.Name,
			Namespace: cfg.TargetNamespace,
		},
		Spec: storagev1.VolumeSpec{
			StorageParameters: createReq.Parameters,
			Request: storagev1.VolumeRequest{
				CapacityRange: storagev1.CapacityRange{
					Request: *resource.NewQuantity(requiredBytes, resource.BinarySI),
					Limit:   *resource.NewQuantity(limitBytes, resource.BinarySI),
				},
				AccessModes: convertCSIAccessModesToStorageAPIAccessModes(createReq.VolumeCapabilities),
				VolumeMode:  convertCSIVolumeMode(createReq.VolumeCapabilities),
			},
		},
	}
}
