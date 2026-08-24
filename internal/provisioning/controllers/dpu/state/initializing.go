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

package state

import (
	"context"
	"fmt"
	"net"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/release"
	dpfutils "github.com/nvidia/doca-platform/internal/utils"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

//nolint:gocyclo // state handler intentionally keeps sequential guard checks for readability and condition fidelity.
func Initializing(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)
	state := dpu.Status.DeepCopy()

	// Check deletion condition
	if !dpu.DeletionTimestamp.IsZero() {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	// Initialize the DPF version in the status the DPU will be using.
	if dpu.Status.DPFVersion == nil || *dpu.Status.DPFVersion == "" {
		version := release.DPFVersion()
		state.DPFVersion = &version
	}

	// Check for the presence of the specified DPUNode
	dpuNode := &provisioningv1.DPUNode{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.DPUNodeName}, dpuNode); err != nil {
		if apierrors.IsNotFound(err) {
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondInitialized.String(), err, "DPUNodeNotFound", err.Error()))
			return *state, err
		}
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondInitialized.String(), err, "GetDPUNodeError", err.Error()))
		return *state, err
	}

	// Check for the presence of the specified DPUDevice
	dpuDevice := &provisioningv1.DPUDevice{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.DPUDeviceName}, dpuDevice); err != nil {
		if apierrors.IsNotFound(err) {
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondInitialized.String(), err, "DPUDeviceNotFound", err.Error()))
			return *state, err
		}
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondInitialized.String(), err, "GetDPUDeviceError", err.Error()))
		return *state, err
	}

	state.DPUType = dpuDevice.Status.DPUType
	state.DPUMode = dpuDevice.Status.DPUMode
	state.Hostless = dpuDevice.Labels[cutil.DPUDeviceHostlessLabel] == "true"

	// Sync SecureBoot status from DPUDevice
	if dpuDevice.Status.SecureBoot != nil {
		state.SecureBoot = dpuDevice.Status.SecureBoot
	}

	// Check if provisioning should be skipped
	if dpuDevice.Labels == nil {
		dpuDevice.Labels = make(map[string]string)
	}

	if _, exists := dpuDevice.Labels[cutil.SkipDpuProvisioningLabel]; exists {
		msg := fmt.Sprintf("skipping provisioning for DPU %s because %s label is set", cutil.GetNamespacedName(dpu), cutil.SkipDpuProvisioningLabel)
		logger.V(2).Info(msg)
		state.Phase = provisioningv1.DPUReady
		cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondInitialized, "Skipped", msg))
		return *state, nil
	}

	// Check if the DPU OOB bridge is configured for non-RedFish installation.
	// If not configured, set the condition and return.
	if dpuNode.Status.DPUInstallInterface != nil && *dpuNode.Status.DPUInstallInterface != string(provisioningv1.InstallViaRedFish) {
		if !meta.IsStatusConditionTrue(dpuNode.Status.Conditions, string(provisioningv1.DPUNodeConditionBridgeConfigured)) {
			err := fmt.Errorf("DPU OOB bridge is not configured")
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondInitialized.String(), err, "DPUOOBBridgeNotConfigured", err.Error()))
			return *state, err
		}
	}

	if dpuNode.Status.DPUInstallInterface == nil {
		err := fmt.Errorf("DPUInstallInterface is not provided")
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondInitialized.String(), err, "DPUInstallInterfaceNotProvided", err.Error()))
		return *state, err
	}

	if dpu.Status.DPUInstallInterface == nil {
		state.DPUInstallInterface = dpuNode.Status.DPUInstallInterface
		return *state, nil
	}

	switch *dpu.Status.DPUInstallInterface {
	case string(provisioningv1.InstallViaGNOI), string(provisioningv1.InstallViaHostAgent):
		if dpu.Spec.PCIAddress == nil {
			err := fmt.Errorf("PCI Address is not provided")
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondInitialized.String(), err, "PCIAddressNotProvided", err.Error()))
			return *state, nil
		}
	case string(provisioningv1.InstallViaRedFish):
		if dpuDevice.Spec.BMCIP == nil || net.ParseIP(*dpuDevice.Spec.BMCIP) == nil {
			err := fmt.Errorf("BMC IP is not valid")
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondInitialized.String(), err, "BMCIPNotValid", err.Error()))
			state.Phase = provisioningv1.DPUError
			return *state, nil
		}
	case string(provisioningv1.InstallViaMock):
	default:
		err := fmt.Errorf("invalid DPUInstallInterface: %s", *dpu.Status.DPUInstallInterface)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondInitialized.String(), err, "InvalidDPUInstallInterface", err.Error()))
		state.Phase = provisioningv1.DPUError
		return *state, nil
	}

	// Create DPUCluster in case it is not assigned
	if dpu.Spec.Cluster.Name == "" {
		rst, err := ctrlCtx.ClusterAllocator.Allocate(ctx, dpu)
		if err != nil {
			err = fmt.Errorf("failed to allocate DPUCluster: %w", err)
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondInitialized.String(), err, "DPUClusterNotAllocated", err.Error()))
			return *state, err
		}
		logger.V(2).Info(fmt.Sprintf("allocate cluster %s for DPU %s", rst, cutil.GetNamespacedName(dpu)))
		return *state, nil
	}

	// Check for the presence of the specified DPUCluster
	obj := &provisioningv1.DPUCluster{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Spec.Cluster.Namespace, Name: dpu.Spec.Cluster.Name}, obj); err != nil {
		if apierrors.IsNotFound(err) {
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondInitialized.String(), err, "DPUClusterNotFound", err.Error()))
			return *state, err
		}
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondInitialized.String(), err, "GetDPUClusterError", err.Error()))
		return *state, err
	}

	if !obj.DeletionTimestamp.IsZero() {
		err := fmt.Errorf("DPUCluster %s/%s is being deleted", obj.Namespace, obj.Name)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondInitialized.String(), err, "DPUClusterDeleting", err.Error()))
		return *state, err
	}

	// Stamp the DPU Agent identity mode exactly once, before advancing to Pending.
	// The mode is derived from the cluster DPFOperatorConfig and is immutable thereafter.
	if state.IdentityMode == nil {
		cfg, err := dpfutils.GetDPFOperatorConfig(ctx, ctrlCtx.Client)
		if err != nil {
			err = fmt.Errorf("reading DPFOperatorConfig to stamp identity mode: %w", err)
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondInitialized.String(), err, "IdentityModeStampDeferred", err.Error()))
			return *state, err
		}
		mode := provisioningv1.IdentityModeBootstrapToken
		if cutil.SpiffeEnabled(cfg) {
			mode = provisioningv1.IdentityModeSpiffe
		}
		state.IdentityMode = ptr.To(mode)
		logger.V(2).Info("stamped DPU identity mode", "identityMode", mode)
	}

	var proceed bool
	state, proceed = cutil.WaitPreInstallAgentRegistrationOrProceed(ctx, dpu, state, ctrlCtx.Options.PreInstallAgentRegistrationTimeout)
	if !proceed {
		return *state, nil
	}

	// Pending branches on DPU type (BlueFieldSoftware vs BFB). On the Redfish path, BMC
	// factory reset delays that stamp; leaving Initializing with Unknown would send BF4
	// DPUs down the empty-BFB path. Host-trusted never stamps dpuType (no BMC discover),
	// so Unknown is expected there and still selects BFB.
	if *dpu.Status.DPUInstallInterface == string(provisioningv1.InstallViaRedFish) &&
		!isKnownDPUType(state.DPUType) {
		err := fmt.Errorf("waiting for DPUDevice %s to report DPU type", dpu.Spec.DPUDeviceName)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondInitialized.String(), err, "DPUTypeUnknown", err.Error()))
		return *state, nil
	}

	state.Phase = provisioningv1.DPUPending
	cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondInitialized, "", ""))

	return *state, nil
}

// isKnownDPUType reports whether t is a concrete BlueField generation. Empty and Unknown mean the
// DPUDevice has not finished identifying the hardware yet.
func isKnownDPUType(t provisioningv1.DPUType) bool {
	switch t {
	case provisioningv1.DPUTypeBlueField2, provisioningv1.DPUTypeBlueField3, provisioningv1.DPUTypeBlueField4:
		return true
	default:
		return false
	}
}
