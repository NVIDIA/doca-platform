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

	utilsCommon "github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/common"
	utilsPci "github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/utils/pci"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/go-logr/logr"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// stageVirtioFS implements stage volume logic for VirtioFS emulation mode
func (h *node) stageVirtioFS(ctx context.Context, stagingPath string, pciAddress string, virtiofsTag string) (*csi.NodeStageVolumeResponse, error) {
	reqLog := logr.FromContextOrDiscard(ctx)
	reqLog.Info("staging volume", "stagingPath", stagingPath, "pciAddress", pciAddress, "virtiofsTag", virtiofsTag)

	reqLog.Info("ensure volume device is bound to VirtioFS driver", "driver", utilsCommon.VirtioPCIDriver)
	if err := h.pci.LoadDriver(pciAddress, utilsCommon.VirtioPCIDriver); err != nil {
		if !errors.Is(err, utilsPci.ErrDeviceAlreadyBoundToDifferentDriver) {
			return handleVirtioFSDriverLoadError(reqLog, err)
		}
		reqLog.Info("Device is already bound to a different driver, trying to unload it")
		if err := h.pci.UnloadDriver(pciAddress); err != nil {
			reqLog.Error(err, "error occurred while trying to unload driver for the volume device")
			return nil, status.Error(codes.Internal, "error occurred while trying to unload driver for the volume device")
		}
		reqLog.Info("Driver unloaded, trying to load VirtioFS driver", "driver", utilsCommon.VirtioPCIDriver)
		if err := h.pci.LoadDriver(pciAddress, utilsCommon.VirtioPCIDriver); err != nil {
			return handleVirtioFSDriverLoadError(reqLog, err)
		}
		reqLog.Info("VirtioFS driver loaded")
	}
	return &csi.NodeStageVolumeResponse{}, nil
}

func handleVirtioFSDriverLoadError(reqLog logr.Logger, err error) (*csi.NodeStageVolumeResponse, error) {
	reqLog.Error(err, "error occurred while trying to load VirtioFS driver for the volume device", "driver", utilsCommon.VirtioPCIDriver)
	return nil, status.Error(codes.Internal, "error occurred while trying to load VirtioFS driver for the volume device")
}
