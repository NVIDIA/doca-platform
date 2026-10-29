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
	"errors"
	"path/filepath"
	"strconv"
	"time"

	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/handlers/common"
	utilsCommon "github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/common"
	utilsNvme "github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/nvme"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/pci"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/go-logr/logr"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/util/wait"
)

// NodeStageVolume is a handler for NodeStageVolume request
func (h *node) NodeStageVolume(
	ctx context.Context,
	req *csi.NodeStageVolumeRequest) (
	*csi.NodeStageVolumeResponse, error) {
	reqLog := logr.FromContextOrDiscard(ctx)
	if req.VolumeId == "" {
		return nil, common.FieldIsRequiredError("VolumeID")
	}
	if req.StagingTargetPath == "" {
		return nil, common.FieldIsRequiredError("StagingTargetPath")
	}
	if err := common.ValidateVolumeCapability(req.VolumeCapability); err != nil {
		return nil, err
	}
	if req.PublishContext[common.PublishCtxDevicePciAddress] == "" {
		return nil, common.FieldIsRequiredError("PublishContext.pciDeviceAddress")
	}

	stagingPath := h.getStagingPath(req.StagingTargetPath, req.VolumeId)
	reqLog = reqLog.WithValues("stagingPath", stagingPath)
	ctx = logr.NewContext(ctx, reqLog)

	pciAddress, err := pci.ParsePCIAddress(req.PublishContext[common.PublishCtxDevicePciAddress])
	if err != nil {
		reqLog.Info("wrong PCI address format", "value", req.PublishContext[common.PublishCtxDevicePciAddress])
		return nil, status.Error(codes.InvalidArgument, "PublishContext.pciDeviceAddress contains invalid PCI address")
	}
	if req.PublishContext[common.PublishCtxNvmeNsID] == "" {
		return nil, common.FieldIsRequiredError("PublishContext.nvmeNsID")
	}
	nvmeNsID, err := strconv.ParseInt(req.PublishContext[common.PublishCtxNvmeNsID], 10, 32)
	if err != nil {
		reqLog.Info("wrong NVME NS ID value provided", "value", req.PublishContext[common.PublishCtxDevicePciAddress])
		return nil, status.Error(codes.InvalidArgument, "PublishContext.nvmeNsID contains invalid NVME NS ID")
	}
	blockDevName, err := h.getBlockDevice(ctx, pciAddress, int32(nvmeNsID))
	if err != nil {
		return nil, err
	}
	if err := h.stageVolume(ctx, filepath.Join("/dev", blockDevName), stagingPath); err != nil {
		return nil, err
	}
	return &csi.NodeStageVolumeResponse{}, nil
}

// getBlockDevice returns block device for the volume, if block device can't be found tries to load NVME driver to the
// device and continue to wait for block device to appear until context is canceled.
func (h *node) getBlockDevice(ctx context.Context, pciAddress string, nvmeNsID int32) (string, error) {
	reqLog := logr.FromContextOrDiscard(ctx)
	var (
		blockDevName string
		err          error
	)
	blockDevName, err = h.nvme.GetBlockDeviceNameForNS(pciAddress, nvmeNsID)
	if err == nil {
		// block device found
		reqLog.Info("block device found", "device", blockDevName)
		return blockDevName, nil
	} else {
		if !getBlockDeviceNameForNSErrorIsRetryable(err) {
			reqLog.Error(err, "error occurred while trying to find block device for the volume")
			return "", status.Error(codes.Internal, "error occurred while trying to find block device for the volume")
		}
	}
	reqLog.Info("block device for the volume not found try to reload the driver for VF")
	if err := h.pci.UnloadDriver(pciAddress); err != nil {
		reqLog.Error(err, "error occurred while trying to unload NVME driver for the volume device")
		return "", status.Error(codes.Internal, "error occurred while trying to unload NVME driver for the volume device")
	}
	if err := h.pci.LoadDriver(pciAddress, utilsCommon.NVMEDriver); err != nil {
		reqLog.Error(err, "error occurred while trying to load NVME driver for the volume device")
		return "", status.Error(codes.Internal, "error occurred while trying to load NVME driver for the volume device")
	}
	err = wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
		blockDevName, err = h.nvme.GetBlockDeviceNameForNS(pciAddress, nvmeNsID)
		if err == nil {
			return true, nil
		} else {
			if !getBlockDeviceNameForNSErrorIsRetryable(err) {
				return false, err
			}
		}
		// device not yet found, continue waiting
		return false, nil
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			reqLog.Info("timeout occurred while waiting for block device for the volume")
			return "", status.Error(codes.DeadlineExceeded, "timeout occurred while waiting for block device for the volume")
		} else {
			reqLog.Error(err, "failed to discover block device for the volume")
			return "", status.Error(codes.Internal, "failed to discover block device for the volume")
		}
	}
	reqLog.Info("block device found", "device", blockDevName)
	return blockDevName, nil
}

func (h *node) stageVolume(ctx context.Context, devicePath, stagePath string) error {
	reqLog := logr.FromContextOrDiscard(ctx)
	if err := h.mount.EnsureFileExist(stagePath, 0644); err != nil {
		reqLog.Error(err, "can't create staging path for the volume")
		return status.Error(codes.Internal, "can't create staging path for the volume")
	}
	exist, _, err := h.mount.CheckMountExists(devicePath, stagePath)
	if err != nil {
		reqLog.Error(err, "error occurred while checking if the volume is staged")
		return status.Error(codes.Internal, "error occurred while checking if the volume is staged")
	}
	if exist {
		reqLog.Info("volume already staged")
		return nil
	}
	if err := h.mount.Mount(devicePath, stagePath, "", []string{"bind"}); err != nil {
		reqLog.Error(err, "failed to stage volume, bind mount failed")
		return status.Error(codes.Internal, "failed to stage volume, bind mount failed")
	}
	return nil
}

// returns true if error returned by GetBlockDeviceNameForNS is retryable
func getBlockDeviceNameForNSErrorIsRetryable(err error) bool {
	return errors.Is(err, utilsNvme.ErrBlockDeviceNotFound) || errors.Is(err, utilsNvme.ErrBlockDeviceIsInvalid)
}
