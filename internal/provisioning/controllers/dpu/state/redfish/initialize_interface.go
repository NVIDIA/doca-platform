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

	if !dpu.DeletionTimestamp.IsZero() {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	device := &provisioningv1.DPUDevice{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.DPUDeviceName}, device); err != nil {
		return *state, err
	}

	if device.Spec.BMCIP == nil {
		err := fmt.Errorf("DPUDevice %q has no BMCIP set", device.Name)
		cutil.SetDPUCondition(state,
			cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized),
				err, "BMCIPNotSpecified", err.Error()))
		return *state, err
	}

	// Check if DPUDevice is ready before proceeding
	if err := checkDPUDeviceReady(ctx, dpu, ctrlCtx); err != nil {
		err = fmt.Errorf("DPUDevice is not ready: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), err, "DPUDeviceNotReady", err.Error()))
		return *state, err
	}

	_, err := rfclient.NewTLSClient(ctx, device.BMCAddress(), dpu.Namespace, ctrlCtx.Client)
	if err != nil {
		err = fmt.Errorf("failed to create tls client: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), err, "FailedToCreateTLSClient", err.Error()))
		return *state, err
	}

	result, err := checkCapacity(ctx, dpu, device, ctrlCtx)
	if err != nil {
		err = fmt.Errorf("failed to check capacity: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), err, "FailedToCheckCapacity", err.Error()))
		return *state, err
	}
	switch result {
	case dutil.CapacityUnknown:
		// send a warning in the condition message, but continue the flow
		state.Phase = provisioningv1.DPUConfigFWParameters
		cond := cutil.NewCondition(
			string(provisioningv1.DPUCondInterfaceInitialized), nil, "UnableToCheckResources",
			fmt.Sprintf("WARNING: unable to check DPU CPU/Memory capacity, the DPUFlavor may be unfit for the DPU, err: %v", err))
		cutil.SetDPUCondition(state, cond)
		return *state, err
	case dutil.CapacityInsufficient:
		err = fmt.Errorf("not enough resources for the given DPUFlavor")
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), err, "FailedToCheckResources", err.Error()))
		state.Phase = provisioningv1.DPUError
		return *state, nil
	case dutil.CapacityRebootRequired:
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUConditionHostPowerCycle), nil, "", "Host power cycle required to transition from NicMode to DpuMode"))
		state.Phase = provisioningv1.DPURebooting
		return *state, nil
	case dutil.CapacitySatisfied:
		if state.DPUMode == provisioningv1.NicMode {
			state.DPUMode = provisioningv1.DpuMode
		}
	}

	state.Phase = provisioningv1.DPUConfigFWParameters
	cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondInterfaceInitialized), nil, "", ""))
	return *state, nil
}

func checkDPUDeviceReady(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) error {
	device := &provisioningv1.DPUDevice{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.DPUDeviceName}, device); err != nil {
		return err
	}

	if !conditions.IsTrue(device, provisioningv1.ConditionDpuDeviceReady) {
		return fmt.Errorf("DPUDevice %q is not ready", device.Name)
	}

	return nil
}

// checkCapacity checks if the DPU has sufficient resources for the flavor.
func checkCapacity(ctx context.Context, dpu *provisioningv1.DPU, device *provisioningv1.DPUDevice, ctrlCtx *dutil.ControllerContext) (dutil.CapacityResult, error) {
	log := log.FromContext(ctx)
	flavor := &provisioningv1.DPUFlavor{}
	if err := ctrlCtx.Client.Get(ctx, types.NamespacedName{Name: dpu.Spec.DPUFlavor, Namespace: dpu.Namespace}, flavor); err != nil {
		return dutil.CapacityUnknown, err
	}

	tlsClient, err := rfclient.NewTLSClient(ctx, device.BMCAddress(), dpu.Namespace, ctrlCtx.Client)
	if err != nil {
		return dutil.CapacityUnknown, err
	}

	// check capacity by description
	resp, desc, err := tlsClient.GetProductDescription()
	if err != nil || resp == nil || resp.StatusCode() != http.StatusOK || desc == nil || desc.Mode == "" {
		err = fmt.Errorf("failed to get description, err: %v, resp: %+v, desc: %+v", err, resp, desc)
		return dutil.CapacityUnknown, err
	}

	// Check for NicMode first, before checking capacity
	// If DPU is in NicMode, we need to set it to DpuMode and reboot before checking capacity
	if dpu.Status.DPUMode == provisioningv1.NicMode {

		if desc.Mode == rfclient.DpuMode {
			log.Info(fmt.Sprintf("DPU %s successfully set to DpuMode", device.BMCAddress()))
		} else {
			log.Info(fmt.Sprintf("DPU %s is in NicMode. Setting DPU mode to DpuMode", device.BMCAddress()))
			_, err = tlsClient.SetDpuMode(provisioningv1.DpuMode)
			if err != nil {
				log.Error(err, fmt.Sprintf("Failed to set DPU mode to DpuMode for DPU %s", device.BMCAddress()))
				return dutil.CapacityUnknown, err
			}
			log.Info(fmt.Sprintf("DPU %s is in NicMode. Set DPU mode to DpuMode, requires host power cycle", device.BMCAddress()))
			// Transition from a NIC mode to a DPU mode requires a host power cycle to take effect.
			return dutil.CapacityRebootRequired, nil
		}
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

	return check(desc.Description, dutil.ParseDescription), nil
}
