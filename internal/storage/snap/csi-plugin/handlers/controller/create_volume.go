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
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/config"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/handlers/common"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/go-logr/logr"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	// DefaultVolumeCapacityBytes is the default capacity for volumes if not specified explicitly
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
	if err := common.ValidateVolumeCapabilities(h.commonConfig.EmulationMode, req.VolumeCapabilities); err != nil {
		return nil, err
	}
	if req.VolumeContentSource != nil {
		return nil, status.Error(codes.InvalidArgument, "volume content source is not supported")
	}
	if err := common.ValidateCreateVolumeParameters(h.commonConfig, req.Parameters); err != nil {
		return nil, err
	}
	client, err := h.clusterhelper.GetClient(ctx)
	if err != nil {
		reqLog.Error(err, "can't retrieve client for cluster")
		return nil, status.Error(codes.Internal, "failed to get kubernetes client for target cluster")
	}

	desiredVolume := csiCreateVolumeRequestToDPUVolume(h.commonConfig, h.controllerConfig, req)
	apiVolume := &storagev1.DPUVolume{}
	if err := client.Create(ctx, desiredVolume); err != nil {
		if apierrors.IsAlreadyExists(err) {
			if err := client.Get(ctx,
				types.NamespacedName{Name: desiredVolume.Name, Namespace: desiredVolume.Namespace}, apiVolume); err != nil {
				reqLog.Error(err, "failed to read existing volume")
				return nil, status.Error(codes.Internal, "failed to read volume info from the dpu cluster")
			}
			if err := createVolumeValidateExisting(reqLog, desiredVolume, apiVolume); err != nil {
				return nil, err
			}
			reqLog.Info("volume object already exist with expected settings")
		} else {
			reqLog.Error(err, "failed to create volume")
			return nil, status.Error(codes.Internal, "failed to create volume")
		}
	}
	reqLog.Info("wait for volume provisioning")
	err = wait.PollUntilContextTimeout(ctx, waitPoolInterval, waitHardTimeout, true, func(ctx context.Context) (bool, error) {
		// here we poll cache, no calls to the API happen
		if err := client.Get(ctx,
			types.NamespacedName{Name: desiredVolume.Name, Namespace: desiredVolume.Namespace}, apiVolume); err != nil {
			if !apierrors.IsNotFound(err) {
				reqLog.Error(err, "failed to read volume while waiting for creation, retry")
			}
			return false, nil
		}
		if apiVolume.Status.Phase != nil && *apiVolume.Status.Phase == storagev1.DPUVolumePhaseBound {
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

	var capBytes int64
	if apiVolume.Status.State != nil && apiVolume.Status.State.VolumeInfo != nil &&
		apiVolume.Status.State.VolumeInfo.Capacity != nil {
		if storageCapacity, ok := apiVolume.Status.State.VolumeInfo.Capacity[corev1.ResourceStorage]; ok {
			capBytes, _ = storageCapacity.AsInt64()
		}
	}
	if capBytes == 0 {
		reqLog.Info("volume capacity is 0, returning error")
		// this should not happen, but we return an error to be safe.
		// we use internal error because it is not something the user can fix and this is propbaly caused by a bug in implementation.
		return nil, status.Error(codes.Internal, "volume capacity is 0")
	}
	volumeCtx, err := getCSIVolumeCtx(h.commonConfig, req.Parameters, apiVolume)
	if err != nil {
		reqLog.Error(err, "failed to get volume context")
		return nil, status.Error(codes.Internal, "failed to get volume context")
	}
	return &csi.CreateVolumeResponse{Volume: &csi.Volume{
		VolumeId:      apiVolume.Name,
		CapacityBytes: capBytes,
		VolumeContext: volumeCtx,
	}}, nil
}

// validate that existing volume has the same parameters as requested
func createVolumeValidateExisting(reqLog logr.Logger, desired, actual *storagev1.DPUVolume) error {
	if !equality.Semantic.DeepEqual(desired.Spec, actual.Spec) {
		reqLog.Info("volume exist with different parameters",
			"desired", desired.Spec,
			"actual", actual.Spec)
		return status.Error(codes.AlreadyExists, "volume already exist but with different parameters")
	}
	return nil
}

// convert csi CreateVolume request arguments to DPUVolume
func csiCreateVolumeRequestToDPUVolume(commonConfig config.Common, controllerConfig config.Controller, createReq *csi.CreateVolumeRequest) *storagev1.DPUVolume {
	requiredBytes := DefaultVolumeCapacityBytes
	if createReq.CapacityRange != nil && createReq.CapacityRange.RequiredBytes > 0 {
		requiredBytes = createReq.CapacityRange.RequiredBytes
	}

	limitBytes := int64(0)
	if createReq.CapacityRange != nil && createReq.CapacityRange.LimitBytes > 0 {
		limitBytes = createReq.CapacityRange.LimitBytes
	}

	policyName := createReq.Parameters[common.StorageClassPolicyKey]
	storageParameters := common.ExcludeCSIPluginStorageClassParameters(createReq.Parameters)

	// Create resource requirements
	storageReqs := corev1.ResourceList{
		corev1.ResourceStorage: *resource.NewQuantity(requiredBytes, resource.BinarySI),
	}

	vol := &storagev1.DPUVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name:      createReq.Name,
			Namespace: controllerConfig.Namespace,
		},
		Spec: storagev1.DPUVolumeSpec{
			DPUStoragePolicyName: policyName,
			Parameters:           storageParameters,
			AccessModes:          convertCSIAccessModesToStorageAPIAccessModes(createReq.VolumeCapabilities),
			Resources: corev1.VolumeResourceRequirements{
				Requests: storageReqs,
			},
			VolumeMode: getDPUVolumeMode(commonConfig),
		},
	}
	if limitBytes > 0 {
		vol.Spec.Resources.Limits = corev1.ResourceList{
			corev1.ResourceStorage: *resource.NewQuantity(limitBytes, resource.BinarySI),
		}
	}
	return vol
}
