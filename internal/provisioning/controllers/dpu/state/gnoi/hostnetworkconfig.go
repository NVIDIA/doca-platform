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

package gnoi

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/dms"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/future"

	logr "github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type inProgressError struct{}

func (e *inProgressError) Error() string {
	return "operation in progress"
}

const (
	InitContainerMaxRestartCount = 10
	NumofVFDefaultValue          = "16"
)

func getNumOfVFsFromFlavor(flavor *provisioningv1.DPUFlavor) (string, bool) {
	regex := regexp.MustCompile(`^NUM_OF_VFS=([0-9]+)`)
	for _, nvconfig := range flavor.Spec.NVConfig {
		for _, parmeter := range nvconfig.Parameters {
			matches := regex.FindStringSubmatch(parmeter)
			if len(matches) == 2 {
				return matches[1], true
			}
		}
	}
	return "", false
}

func SetupNetwork(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)
	state := dpu.Status.DeepCopy()

	hostNetworkTaskName := generateHostNetworkTaskName(dpu)

	if !dpu.DeletionTimestamp.IsZero() {
		dutil.HostNetworkTaskMap.Delete(hostNetworkTaskName)
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	numVFs := NumofVFDefaultValue
	flavor := &provisioningv1.DPUFlavor{}
	if err := ctrlCtx.Get(ctx, types.NamespacedName{Namespace: dpu.Namespace, Name: dpu.Spec.DPUFlavor}, flavor); err != nil {
		if apierrors.IsNotFound(err) {
			cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondHostNetworkReady.String(), err, "DPUFlavorNotFound", err.Error()))
			return *state, err
		}
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondHostNetworkReady.String(), err, "GetDPUFlavorError", err.Error()))
		return *state, err
	}

	if num, ok := getNumOfVFsFromFlavor(flavor); ok {
		numVFs = num
	}

	pciAddress, err := cutil.GetPCIAddrFromDPU(dpu, false)
	if err != nil {
		err = fmt.Errorf("failed to get pci address from DPU label: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondHostNetworkReady.String(), err, "GetPCIAddrFromDPUError", err.Error()))
		return *state, err
	}

	// Get control-plane MTU to pass it to the hostnetwork.sh.
	dpfOperatorConfigList := operatorv1.DPFOperatorConfigList{}
	if err := ctrlCtx.List(ctx, &dpfOperatorConfigList); err != nil {
		err = fmt.Errorf("list DPFOperatorConfigs: %w", err)
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondHostNetworkReady.String(), err, "ListDPFOperatorConfigsError", err.Error()))
		return *state, err
	}
	if len(dpfOperatorConfigList.Items) == 0 || len(dpfOperatorConfigList.Items) > 1 {
		err = fmt.Errorf("exactly one DPFOperatorConfig necessary")
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondHostNetworkReady.String(), err, "DPFOperatorConfigCountError", err.Error()))
		return *state, err
	}
	if dpfOperatorConfigList.Items[0].Spec.Networking == nil {
		err = fmt.Errorf("DPFOperatorConfig networking section is missing")
		cutil.SetDPUCondition(state, cutil.NewCondition(provisioningv1.DPUCondHostNetworkReady.String(), err, "DPFOperatorConfigNetworkingSectionMissing", err.Error()))
		return *state, err
	}
	controlPlaneMTU := *dpfOperatorConfigList.Items[0].Spec.Networking.ControlPlaneMTU

	if task, ok := dutil.HostNetworkTaskMap.Load(hostNetworkTaskName); ok {
		if err := checkHostNetworkTask(&logger, ctx, dpu, ctrlCtx, pciAddress, numVFs, controlPlaneMTU, hostNetworkTaskName, task); err != nil {
			if errors.Is(err, &inProgressError{}) {
				cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondHostNetworkReady), err, "HostNetworkSetupInProgress", err.Error()))
				return *state, err
			}
			err = fmt.Errorf("failed after multiple retries")
			cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondHostNetworkReady), err, "HostNetworkSetupFailed", err.Error()))
			state.Phase = provisioningv1.DPUError
			return *state, nil
		}
		cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondHostNetworkReady, string(provisioningv1.DPUCondHostNetworkReady), ""))
		state.Phase = provisioningv1.DPUClusterConfig
		return *state, nil
	} else {
		hostNetworkHandler(ctx, dpu, ctrlCtx, pciAddress, numVFs, controlPlaneMTU, 0)
	}
	return *state, err
}

func hostNetworkHandler(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext, pciAddress string, numVFs string, controlPlaneMTU int, retry int) {
	logger := log.FromContext(ctx)
	hostNetworkTaskName := generateHostNetworkTaskName(dpu)

	rebootTask := future.New(func() (any, error) {
		logger.V(3).Info(fmt.Sprintf("Host Network Setup for dpu: %s: check br-dpu bridge and connect VF", dpu.Name))
		// Call DMS Debug command to run hostnetwork.sh script
		conn, err := dutil.CreateGRPCConnection(ctx, ctrlCtx.Client, dpu, ctrlCtx)
		if err != nil {
			logger.Error(err, fmt.Sprintf("Error creating gRPC connection: %v", err))
			return nil, err
		}
		defer conn.Close() //nolint: errcheck

		command := fmt.Sprintf("%s --num_of_vfs %s --device_pci_address %s --control_plane_mtu %d", dms.HostNetworkScript, numVFs, pciAddress, controlPlaneMTU)
		_, err = dms.ExecuteDMSDebugCmd(ctx, conn, command)
		if err != nil {
			logger.Error(err, fmt.Sprintf("Error running hostnetwork.sh script: %v", err))
			return nil, err
		}
		return future.Ready, nil
	})
	rebootTaskWithRetry := dutil.TaskWithRetry{
		Task:       rebootTask,
		RetryCount: retry,
	}
	dutil.HostNetworkTaskMap.Store(hostNetworkTaskName, rebootTaskWithRetry)
}

func checkHostNetworkTask(logger *logr.Logger, ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext, pciAddress string, numVFs string, controlPlaneMTU int, hostNetworkTaskName string, task any) error {
	taskWithRetry := task.(dutil.TaskWithRetry)
	retryCount := taskWithRetry.RetryCount
	hostNetworkTask := taskWithRetry.Task
	if hostNetworkTask.GetState() != future.Ready {
		logger.V(3).Info(fmt.Sprintf("HostNetwork task %v is being processed", hostNetworkTaskName))
		return &inProgressError{}
	}

	dutil.HostNetworkTaskMap.Delete(hostNetworkTaskName)
	_, err := hostNetworkTask.GetResult()
	if err == nil {
		logger.V(3).Info(fmt.Sprintf("HostNetwork task %v is finished", hostNetworkTaskName))
		return nil
	}

	if retryCount >= dutil.MaxRetryCount {
		logger.V(3).Info(fmt.Sprintf("HostNetwork task %v failed with err: %v", hostNetworkTaskName, err))
		return err
	}

	msg := fmt.Sprintf("HostNetwork task %v retried %d times, error: %v", hostNetworkTaskName, retryCount, err)
	logger.Info(msg)
	// Retry the host network setup process
	hostNetworkHandler(ctx, dpu, ctrlCtx, pciAddress, numVFs, controlPlaneMTU, retryCount+1)
	return &inProgressError{}
}

func generateHostNetworkTaskName(dpu *provisioningv1.DPU) string {
	return fmt.Sprintf("%s/%s", dpu.Namespace, dpu.Name)
}
