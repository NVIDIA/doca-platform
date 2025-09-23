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

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/reboot"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	PodTemplateConfigMapKey     string = "pod-template"
	PodInfoVolumeName           string = "dpf-pod-info"
	PodInfoMountPath            string = "/etc/dpf-pod-info"
	PodInfoLabelsPath           string = "labels"
	PodInfoAnnotationsPath      string = "annotations"
	PodInfoLabelsFieldPath      string = "metadata.labels"
	PodInfoAnnotationsFieldPath string = "metadata.annotations"
	DPUNodeNameEnvVar           string = "DPUNODE_NAME"
)

func Rebooting(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)

	state := dpu.Status.DeepCopy()
	if !dpu.DeletionTimestamp.IsZero() {
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	dpuNode := &provisioningv1.DPUNode{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.DPUNodeName}, dpuNode); err != nil {
		if apierrors.IsNotFound(err) {
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondRebooted.String(), err, "DPUNodeNotFound", err.Error()))
			return *state, err
		}
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondRebooted.String(), err, "GetDPUNodeError", err.Error()))
		return *state, err
	}

	_, cond := cutil.GetDPUCondition(state, provisioningv1.DPUCondInterfaceInitialized.String())
	if cond == nil || cond.Status != metav1.ConditionTrue {
		err := fmt.Errorf("trying to reboot the host before %s", provisioningv1.DPUCondOSInstalled.String())
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondRebooted.String(), err, "InvalidState", err.Error()))
		return *state, err
	}

	rebootCommand, _, err := reboot.GenerateCmd(dpuNode.Annotations, dpu.Annotations)
	if err != nil {
		err = fmt.Errorf("failed to generate ipmitool command: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondRebooted.String(), err, "GenerateIPMIToolCommandError", err.Error()))
		return *state, err
	}

	// Return early and set node to ready if we should skip the powercycle/reboot command.
	// Note: skipping the powercycle/reboot may cause issues with the firmware installation and configuration.
	if rebootCommand == reboot.Skip {
		logger.Info("Warning not rebooting: this may cause issues with DPU firmware installation and configuration")
		state.Phase = provisioningv1.DPUHostNetworkConfiguration
		cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondRebooted, "", ""))
		return *state, nil
	}

	if dpuNode.Spec.NodeRebootMethod.GNOI != nil || dpuNode.Spec.NodeRebootMethod.HostAgent != nil { //nolint:staticcheck
		_, condition := cutil.GetDPUCondition(state, string(provisioningv1.DPUCondRebooted))
		if condition != nil && condition.Status == metav1.ConditionTrue {
			state.Phase = provisioningv1.DPUHostNetworkConfiguration
		}
		return *state, nil
	} else if dpuNode.Spec.NodeRebootMethod.External != nil || dpuNode.Spec.NodeRebootMethod.Script != nil {
		_, rebootCondition := cutil.GetDPUCondition(state, provisioningv1.DPUCondRebooted.String())
		if rebootCondition != nil && rebootCondition.Status == metav1.ConditionTrue {
			state.Phase = provisioningv1.DPUHostNetworkConfiguration
			logger.Info(fmt.Sprintf("DPU %s moves to Host Network Configuration phase", dpu.Name))
			if ctrlCtx.Options.DPUInstallInterface == string(provisioningv1.InstallViaRedFish) {
				state.Phase = provisioningv1.DPUClusterConfig
				logger.Info(fmt.Sprintf("DPU %s moves to DPU Cluster Configuration phase", dpu.Name))
			}
		}
		return *state, nil
	} else {
		panic("should not reach here")
	}
}
