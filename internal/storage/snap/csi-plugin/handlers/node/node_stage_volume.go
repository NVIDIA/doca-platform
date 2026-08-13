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
	"strconv"

	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/config"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/handlers/common"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/pci"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/go-logr/logr"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	if err := common.ValidateVolumeCapability(h.commonConfig.EmulationMode, req.VolumeCapability); err != nil {
		return nil, err
	}
	if req.PublishContext[common.PublishCtxDevicePciAddress] == "" {
		return nil, common.FieldIsRequiredError("PublishContext.pciDeviceAddress")
	}

	rawPCIAddress := req.PublishContext[common.PublishCtxDevicePciAddress]
	funcVUID := req.PublishContext[common.PublishCtxFuncVUID]
	pciAddress, err := pci.ParsePCIAddress(rawPCIAddress)
	if err != nil {
		reqLog.Info("wrong PCI address format", "value", rawPCIAddress)
		return nil, status.Error(codes.InvalidArgument, "PublishContext.pciDeviceAddress contains invalid PCI address")
	}
	if funcVUID != "" {
		pciAddress, err = h.pci.ResolvePCIAddressByVUID(pciAddress, funcVUID)
		if err != nil {
			reqLog.Error(err, "failed to resolve PCI address by function VUID", "funcVUID", funcVUID)
			return nil, status.Error(codes.Internal, "failed to resolve PCI address by function VUID")
		}
	} else {
		reqLog.Info("function VUID is not available, assuming PCI domain 0000",
			"volumeID", req.VolumeId, "pciAddress", pciAddress)
	}

	switch h.commonConfig.EmulationMode {
	case config.EmulationModeNVMe:
		if req.PublishContext[common.PublishCtxNvmeNsID] == "" {
			return nil, common.FieldIsRequiredError("PublishContext.nvmeNsID")
		}
		nvmeNsID, err := strconv.ParseInt(req.PublishContext[common.PublishCtxNvmeNsID], 10, 32)
		if err != nil {
			reqLog.Info("wrong NVME NS ID value provided", "value", req.PublishContext[common.PublishCtxNvmeNsID])
			return nil, status.Error(codes.InvalidArgument, "PublishContext.nvmeNsID contains invalid NVME NS ID")
		}
		return h.stageNVMe(ctx, h.getNVMeStagingPath(req.StagingTargetPath, req.VolumeId), pciAddress, int32(nvmeNsID))
	case config.EmulationModeVirtiofs:
		virtiofsTag := req.PublishContext[common.PublishCtxVirtioFsTag]
		if virtiofsTag == "" {
			return nil, common.FieldIsRequiredError("PublishContext.virtioFSTag")
		}
		return h.stageVirtioFS(ctx, req.StagingTargetPath, pciAddress, virtiofsTag)
	default:
		return nil, status.Error(codes.Unimplemented, "unsupported emulation mode selected")
	}
}
