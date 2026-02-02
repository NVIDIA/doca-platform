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

package redfish

import (
	"context"
	"fmt"
	"net/http"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	rfclient "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/pkg/conditions"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func InitializeInterface(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	state := dpu.Status.DeepCopy()
	log := log.FromContext(ctx)

	if !dpu.DeletionTimestamp.IsZero() {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	device := &provisioningv1.DPUDevice{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.DPUDeviceName}, device); err != nil {
		return *state, err
	}

	// Check if DPUDevice is ready before proceeding
	if err := checkDPUDeviceReady(device); err != nil {
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), err, "DPUDeviceNotReady", err.Error()))
		return *state, err
	}

	tlsClient, err := rfclient.NewTLSClient(ctx, device.BMCAddress(), dpu.Namespace, ctrlCtx.Client)
	if err != nil {
		log.Error(err, fmt.Sprintf("Failed to create TLS client for DPU %s", device.BMCAddress()))
		return *state, err
	}

	descr, err := getProductDescription(tlsClient)
	if err != nil {
		log.Error(err, fmt.Sprintf("Failed to get product description for DPU %s", device.BMCAddress()))
		return *state, err
	}

	// Redfish returns DPU is in NIC mode - reqesting to change to DpuMode
	if descr.Mode == rfclient.NicMode {
		log.Info(fmt.Sprintf("DPU %s is in NicMode. Setting DPU mode to DpuMode", device.BMCAddress()))
		_, err := tlsClient.SetDpuMode(provisioningv1.DpuMode)
		if err != nil {
			err = fmt.Errorf("failed to request mode change: %w", err)
			cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), err, "FailedToRequestModeChange", err.Error()))
			return *state, err
		}

		log.Info(fmt.Sprintf("Host power cycle is required for DPU %s to transition from NicMode to DpuMode", device.BMCAddress()))
		// Transition from a NIC mode to a DPU mode requires a host power cycle to take effect.
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUConditionHostPowerCycle), nil, "", "Host power cycle required to transition from NicMode to DpuMode"))
		state.Phase = provisioningv1.DPURebooting
		return *state, nil
	}

	// DPU mode was in NIC mode, updating to DpuMode
	if dpu.Status.DPUMode != provisioningv1.DpuMode {
		log.Info(fmt.Sprintf("DPU %s successfully set to DpuMode", device.BMCAddress()))
		state.DPUMode = provisioningv1.DpuMode
	}

	// In NIC mode, DPU type was unknown, now it's in DPU mode - updating to the value from the Redfish
	if dpu.Status.DPUType == provisioningv1.DPUTypeUnknown {
		_, chassisInfo, err := tlsClient.GetChassis()
		if err != nil {
			log.Error(err, fmt.Sprintf("Failed to get chassis info for DPU %s", device.BMCAddress()))
			cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), err, "FailedToGetChassisInfo", err.Error()))
			return *state, err
		}

		state.DPUType = chassisInfo.GetBlueFieldVersion()
		if state.DPUType == provisioningv1.DPUTypeUnknown {
			err = fmt.Errorf("unknown DPU type")
			log.Error(err, fmt.Sprintf("Failed to get DPU type for DPU %s", device.BMCAddress()))
			cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), err, "FailedToGetDPUType", err.Error()))
			return *state, err
		}
	}

	result, err := checkCapacity(ctx, dpu, device, ctrlCtx, tlsClient)
	if err != nil {
		err = fmt.Errorf("failed to check capacity: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), err, "FailedToCheckCapacity", err.Error()))
		return *state, err
	}
	switch result {
	case dutil.CapacityInsufficient:
		err = fmt.Errorf("not enough resources for the given DPUFlavor")
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), err, "FailedToCheckResources", err.Error()))
		state.Phase = provisioningv1.DPUError
		return *state, nil
	case dutil.CapacitySatisfied:
		state.Phase = provisioningv1.DPUConfigFWParameters
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), nil, "", ""))
		return *state, nil
	default:
		// send a warning in the condition message, but continue the flow
		state.Phase = provisioningv1.DPUConfigFWParameters
		cond := cutil.NewCondition(
			string(provisioningv1.DPUCondInterfaceInitialized), nil, "UnableToCheckResources",
			fmt.Sprintf("WARNING: unable to check DPU CPU/Memory capacity, the DPUFlavor may be unfit for the DPU, err: %v", err))
		cutil.SetDPUCondition(state, cond)
		return *state, err
	}
}

func checkDPUDeviceReady(dpuDevice *provisioningv1.DPUDevice) error {
	if !conditions.IsTrue(dpuDevice, provisioningv1.ConditionDpuDeviceReady) {
		return fmt.Errorf("DPUDevice %q is not ready", dpuDevice.Name)
	}

	if dpuDevice.Spec.BMCIP == nil {
		return fmt.Errorf("DPUDevice %q has no BMCIP set", dpuDevice.Name)
	}

	return nil
}

func getProductDescription(tlsClient *rfclient.Client) (*rfclient.ProductSpecInfo, error) {
	resp, desc, err := tlsClient.GetProductDescription()
	if err != nil || resp == nil || resp.StatusCode() != http.StatusOK || desc == nil || desc.Mode == "" {
		return nil, fmt.Errorf("failed to get description, err: %v, resp: %+v, desc: %+v", err, resp, desc)
	}
	return desc, nil
}

// checkCapacity checks if the DPU has sufficient resources for the flavor.
func checkCapacity(ctx context.Context, dpu *provisioningv1.DPU, device *provisioningv1.DPUDevice, ctrlCtx *dutil.ControllerContext, tlsClient *rfclient.Client) (dutil.CapacityResult, error) {
	log := log.FromContext(ctx)
	flavor := &provisioningv1.DPUFlavor{}
	if err := ctrlCtx.Client.Get(ctx, types.NamespacedName{Name: dpu.Spec.DPUFlavor, Namespace: dpu.Namespace}, flavor); err != nil {
		return dutil.CapacityUnknown, err
	}
	// check capacity by description
	productSpecInfo, err := getProductDescription(tlsClient)
	if err != nil {
		log.Error(err, fmt.Sprintf("Failed to get product description for DPU %s", device.BMCAddress()))
		return dutil.CapacityUnknown, err
	}

	check := func(data string, parseFunc func(string) *dutil.BlueFieldSpecs) dutil.CapacityResult {
		bfSpecs := parseFunc(data)
		if bfSpecs == nil {
			return dutil.CapacityUnknown
		}
		log.Info("retrieved DPU specs", "bfSpecs", bfSpecs)
		return bfSpecs.CanSatisfy(flavor.Spec.DPUResources)
	}

	// check capacity by part number
	resp, pn, err := tlsClient.GetChassis()
	if err != nil || resp.StatusCode() != http.StatusOK {
		err = fmt.Errorf("failed to get part number, status code: %s, err: %v", resp.Status(), err)
		return dutil.CapacityUnknown, err
	}
	if result := check(pn.PartNumber, dutil.LookUpPartNumber); result != dutil.CapacityUnknown {
		return result, nil
	}

	return check(productSpecInfo.Description, dutil.ParseDescription), nil
}
