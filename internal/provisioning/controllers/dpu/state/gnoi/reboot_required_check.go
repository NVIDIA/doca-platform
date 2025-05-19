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

package gnoi

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	dmsutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util/dms"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

func RebootRequiredCheck(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)
	logger.Info("start check host reboot required")
	state := dpu.Status.DeepCopy()
	if !dpu.DeletionTimestamp.IsZero() {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	conn, err := dutil.CreateGRPCConnection(ctx, ctrlCtx.Client, dpu, ctrlCtx)
	if err != nil {
		err = fmt.Errorf("failed to create grpc connection: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondCheckedHostRebootNeed), err, "FailedToCreateGRPCConnection", err.Error()))
		state.Phase = provisioningv1.DPUError
		return *state, nil
	}
	defer conn.Close() //nolint: errcheck

	if _, ok := dpu.Labels[cutil.DPUDevicePCIAddressLabel]; !ok {
		err = fmt.Errorf("can not get pci address from DPU object's label")
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondCheckedHostRebootNeed), err, "DPUObjectMissingPCIAddressLabel", err.Error()))
		return *state, err
	}
	pciAddress := strings.Replace(dpu.Labels[cutil.DPUDevicePCIAddressLabel], "-", ":", -1)

	command := fmt.Sprintf("mlxreg -d %s.0 --get --reg_name MFRL|grep pci_rescan_required|grep -o '0x[0-9a-fA-F]\\+'", pciAddress)
	resp, err := dmsutil.ExecuteDMSDebugCmd(ctx, conn, command)
	if err != nil {
		err = fmt.Errorf("failed to check pci rescan required: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondCheckedHostRebootNeed), err, "FailedToCheckPCIRescanRequired", err.Error()))
		return *state, err
	}

	var result int64
	result, err = strconv.ParseInt(resp, 0, 64)
	if err != nil {
		err = fmt.Errorf("failed to convert hex to dec: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondCheckedHostRebootNeed), err, "FailedToConvertHexToDec", err.Error()))
		return *state, err
	}

	logger.Info(fmt.Sprintf("The pci_rescan_required result is %d for DPU %s", result, dpu.Name))
	if result == 1 {
		state.Phase = provisioningv1.DPURebooting
		cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondCheckedHostRebootNeed, "", ""))
		return *state, nil
	}

	// Get the current FW version
	command = fmt.Sprintf("flint -d %s.0 q |grep 'FW Version'|awk -F ': *' '{print $2}'", pciAddress)
	resp, err = dmsutil.ExecuteDMSDebugCmd(ctx, conn, command)
	if err != nil {
		err = fmt.Errorf("failed to check current firmware version: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondCheckedHostRebootNeed), err, "FailedToCheckCurrentFirmwareVersion", err.Error()))
		return *state, err
	}
	curVer := resp

	// Get the running FW version
	command = fmt.Sprintf("flint -d %s.0 q |grep 'FW Version(Running)'|awk -F ': *' '{print $2}'", pciAddress)
	resp, err = dmsutil.ExecuteDMSDebugCmd(ctx, conn, command)
	if err != nil {
		err = fmt.Errorf("failed to check running firmware version: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondCheckedHostRebootNeed), err, "FailedToCheckRunningFirmwareVersion", err.Error()))
		return *state, err
	}
	runningVer := resp

	logger.Info(fmt.Sprintf("The current FW version is %s, the running FW version is %s for DPU %s", curVer, runningVer, dpu.Name))
	if curVer != runningVer {
		// Get reset level
		resetLevel3 := false
		command = fmt.Sprintf("mlxfwreset -d %s.0 q|grep '3: Driver restart and PCI reset'|grep -oE 'Supported|Not supported|Not Supported'", pciAddress)
		resp, err = dmsutil.ExecuteDMSDebugCmd(ctx, conn, command)
		if err != nil {
			err = fmt.Errorf("failed to check reset level: %w", err)
			cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondCheckedHostRebootNeed), err, "FailedToCheckResetLevel", err.Error()))
			return *state, err
		}
		resetLevel3 = resp == "Supported"

		// Get sync level
		syncLevel1 := false
		command = fmt.Sprintf("mlxfwreset -d %s.0 q|grep '1: Driver is the owner'|grep -oE 'Supported|Not supported|Not Supported'", pciAddress)
		resp, err = dmsutil.ExecuteDMSDebugCmd(ctx, conn, command)
		if err != nil {
			err = fmt.Errorf("failed to check sync level: %w", err)
			cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondCheckedHostRebootNeed), err, "FailedToCheckSyncLevel", err.Error()))
			return *state, err
		}
		syncLevel1 = resp == "Supported"

		logger.Info(fmt.Sprintf("The reset levels 3 is %t, the sync level 1 is %t for DPU %s", resetLevel3, syncLevel1, dpu.Name))
		canReset := (resetLevel3 && syncLevel1)
		// If reset level3 and sync level 1 are supported, go to reset, otherwise
		// go to provisioningv1.DPURebooting phase.
		if canReset {
			// execute the FW reset.
			command = fmt.Sprintf("mlxfwreset -d %s.0 -y -l 3 --sync 1 r", pciAddress)
			_, err = dmsutil.ExecuteDMSDebugCmd(ctx, conn, command)
			if err != nil {
				err = fmt.Errorf("failed to reset firmware: %w", err)
				cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondCheckedHostRebootNeed), err, "FailedToResetFirmware", err.Error()))
				return *state, err
			}
			state.Phase = provisioningv1.DPUHostNetworkConfiguration
			cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondCheckedHostRebootNeed, "", ""))
			return *state, nil
		} else {
			state.Phase = provisioningv1.DPURebooting
			cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondCheckedHostRebootNeed, "", ""))
			return *state, nil
		}
	}

	state.Phase = provisioningv1.DPUHostNetworkConfiguration
	cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondCheckedHostRebootNeed, "", ""))
	return *state, nil
}
