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
	"encoding/json"
	"fmt"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	dmsutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util/dms"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/future"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/reboot"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	PodTemplateConfigMapKey string = "pod-template"
)

func Rebooting(ctx context.Context, dpu *provisioningv1.DPU, ctrlCtx *dutil.ControllerContext) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)

	rebootTaskName := generateRebootTaskName(dpu)
	state := dpu.Status.DeepCopy()
	if !dpu.DeletionTimestamp.IsZero() {
		dutil.RebootTaskMap.Delete(rebootTaskName)
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

	rebootCommand, rebootType, err := reboot.GenerateCmd(dpuNode.Annotations, dpu.Annotations)
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

	if dpuNode.Spec.NodeRebootMethod.GNOI != nil {
		// create DMS gRPC connection
		conn, err := dutil.CreateGRPCConnection(ctx, ctrlCtx.Client, dpu, ctrlCtx)
		if err != nil {
			err = fmt.Errorf("failed to create gRPC connection: %w", err)
			cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondRebooted), err, "FailedToCreateGRPCConnection", err.Error()))
			state.Phase = provisioningv1.DPUError
			return *state, nil
		}
		defer conn.Close() //nolint: errcheck

		// Checking the reboot is done or not for the GNOI reboot method.
		duration := int(metav1.Now().Sub(cond.LastTransitionTime.Time).Seconds())
		// If we can not get uptime, the host should be rebooting
		uptime, err := ctrlCtx.HostUptimeChecker.HostUptime(ctx, conn)
		if err != nil {
			err = fmt.Errorf("failed to get host uptime: %w", err)
			cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondRebooted), err, "FailedToGetHostUptime", ""))
			return *state, err
		}
		logger.V(3).Info(fmt.Sprintf("Rebooting duration is %d, host uptime is %d", duration, uptime))
		// If the pod is available after rebooting, move to next phase
		if duration > uptime {
			state.Phase = provisioningv1.DPUHostNetworkConfiguration
			cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondRebooted, "", ""))
			return *state, nil
		}

		if rebootType == reboot.PowerCycle {
			logger.Info(fmt.Sprintf("DPU %s powercycle command %q", dpu.Name, rebootCommand))
			resp, err := dmsutil.ExecuteDMSDebugCmd(ctx, conn, rebootCommand)
			if err != nil {
				err = fmt.Errorf("failed to execute powercycle command: %w, output: %s", err, resp)
				cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondRebooted), err, "FailedToExecutePowercycleCommand", resp))
				state.Phase = provisioningv1.DPUError
				return *state, nil
			}
			return *state, nil
		} else if rebootType == reboot.WarmReboot {
			logger.Info(fmt.Sprintf("DPU %s Bluefield System-Level-Reset", dpu.Name))

			if pciAddress, err := cutil.GetPCIAddrFromDPU(dpu, true); err != nil {
				err = fmt.Errorf("failed to get pci address from DPU label: %w", err)
				cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondRebooted), err, "FailedToGetPCIAddrFromDPU", err.Error()))
				return *state, err
			} else {
				if task, ok := dutil.RebootTaskMap.Load(rebootTaskName); ok {
					if err := checkRebootTask(ctx, dpuNode, dpu, pciAddress, rebootCommand, rebootTaskName, task); err != nil {
						err = fmt.Errorf("failed to check reboot task: %w", err)
						cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondRebooted), err, "RebootFailed", ""))
						return *state, err
					}
					return *state, nil
				} else {
					rebootHandler(ctx, dpuNode, dpu, pciAddress, rebootCommand, 0)
				}
			}

		}
	} else if dpuNode.Spec.NodeRebootMethod.External != nil {
		logger.Info("waiting for manual power cycle or reboot")
		if err := proccessExternalReboot(ctx, dpuNode, ctrlCtx, state); err != nil {
			err = fmt.Errorf("failed to process external reboot: %w", err)
			cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondRebooted), err, "FailedToProcessExternalReboot", err.Error()))
			return *state, err
		}
		return *state, nil
	} else if dpuNode.Spec.NodeRebootMethod.Script != nil {
		logger.Info("waiting for custom script reboot")
		c := cutil.DPUCondition(provisioningv1.DPUCondRebooted, "WaitingForScriptToRebootNode", "")
		// Check whether the external reboot is already triggerred.
		if _, existedCond := cutil.GetDPUCondition(state, c.Type); existedCond != nil {
			jobName := generateJobName(dpuNode)
			job := &batchv1.Job{}
			if err := ctrlCtx.Get(ctx, client.ObjectKey{Namespace: dpu.Namespace, Name: jobName}, job); err != nil {
				if apierrors.IsNotFound(err) {
					err = fmt.Errorf("job %s not found", jobName)
					cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondRebooted), err, "JobNotFound", err.Error()))
					return *state, err
				}
				err = fmt.Errorf("failed to fetch Job %s: %w", jobName, err)
				cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondRebooted), err, "FailedToFetchJob", err.Error()))
				return *state, err
			}

			if job.Status.Succeeded > 0 {
				if err := client.IgnoreNotFound(ctrlCtx.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationForeground))); err != nil {
					err = fmt.Errorf("failed to delete Job %s: %w", jobName, err)
					cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondRebooted), err, "FailedToDeleteJob", err.Error()))
					return *state, err
				}
				state.Phase = provisioningv1.DPUHostNetworkConfiguration
				if ctrlCtx.Options.DPUInstallInterface == string(provisioningv1.InstallViaRedFish) {
					state.Phase = provisioningv1.DPUClusterConfig
				}

				cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondRebooted, "", ""))
				return *state, nil
			} else if job.Status.Failed > 0 {
				// Not remove the failed job for user debuging.
				err = fmt.Errorf("the custom reboot script failed")
				cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondRebooted), err, "RebootFailed", err.Error()))
				state.Phase = provisioningv1.DPUError
				return *state, nil
			}
			return *state, nil
		}

		if err := createScriptJob(ctx, dpu, dpuNode, ctrlCtx); err != nil {
			err = fmt.Errorf("failed to create script job: %w", err)
			cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondRebooted), err, "FailedToCreateScriptJob", err.Error()))
			return *state, err
		}
		err = fmt.Errorf("waiting for script to reboot node")
		cutil.SetDPUCondition(state, cutil.NewCondition(string(provisioningv1.DPUCondRebooted), err, "WaitingForScriptToRebootNode", err.Error()))
		return *state, err
	} else {
		panic("should not reach here")
	}

	return *state, nil
}

func proccessExternalReboot(ctx context.Context, dpuNode *provisioningv1.DPUNode, ctrlCtx *dutil.ControllerContext, state *provisioningv1.DPUStatus) error {
	logger := log.FromContext(ctx)
	c := cutil.DPUCondition(provisioningv1.DPUCondRebooted, "WaitingForManualPowerCycleOrReboot", "")
	// Check whether the external reboot is already triggerred.
	if _, existedCond := cutil.GetDPUCondition(state, c.Type); existedCond != nil {
		if _, ok := dpuNode.Annotations[provisioningv1.DPUNodeExternalRebootRequiredAnnotation]; ok {
			return nil
		}

		state.Phase = provisioningv1.DPUHostNetworkConfiguration
		if ctrlCtx.Options.DPUInstallInterface == string(provisioningv1.InstallViaRedFish) {
			state.Phase = provisioningv1.DPUClusterConfig
		}

		cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondRebooted, "", ""))
		return nil
	}

	// Begin the external reboot.
	dpuNode.Annotations[provisioningv1.DPUNodeExternalRebootRequiredAnnotation] = "true"
	if err := ctrlCtx.Update(ctx, dpuNode); err != nil {
		logger.Error(err, fmt.Sprintf("failed to add annotation %s to DPUNode %s", provisioningv1.DPUNodeExternalRebootRequiredAnnotation, dpuNode.Name))
		return err
	}

	c.Status = metav1.ConditionFalse
	cutil.SetDPUCondition(state, c)
	return nil
}

func checkRebootTask(ctx context.Context, dpuNode *provisioningv1.DPUNode, dpu *provisioningv1.DPU, pciAddress string, rebootCommand string, rebootTaskName string, task any) error {
	logger := log.FromContext(ctx)
	rebootTaskWithRetry := task.(dutil.TaskWithRetry)
	retryCount := rebootTaskWithRetry.RetryCount
	rebootTask := rebootTaskWithRetry.Task
	if rebootTask.GetState() == future.Ready {
		dutil.RebootTaskMap.Delete(rebootTaskName)
		if _, err := rebootTask.GetResult(); err == nil {
			cutil.SetDPUCondition(&dpu.Status, cutil.DPUCondition(provisioningv1.DPUCondRebooted, "", ""))
			return nil
		} else {
			if retryCount >= dutil.MaxRetryCount {
				logger.V(3).Info(fmt.Sprintf("Reboot task %v failed with err: %v", rebootTaskName, err))
				dpu.Status.Phase = provisioningv1.DPUError
				cutil.SetDPUCondition(&dpu.Status, cutil.NewCondition(string(provisioningv1.DPUCondRebooted), err, "RebootFailed", ""))
				return err
			} else {
				msg := fmt.Sprintf("DMS task %v retried %d times, error: %v", rebootTaskName, retryCount, err)
				logger.Info(msg)
				// Retry the reboot process
				rebootHandler(ctx, dpuNode, dpu, pciAddress, rebootCommand, retryCount+1)
				return nil
			}
		}
	}

	logger.V(3).Info(fmt.Sprintf("Reboot task %v is being processed", rebootTaskName))
	return nil
}

func generateJobName(dpuNode *provisioningv1.DPUNode) string {
	return fmt.Sprintf("%s-reboot-script", dpuNode.Name)
}

func createScriptJob(ctx context.Context, dpu *provisioningv1.DPU, dpuNode *provisioningv1.DPUNode, ctrlCtx *dutil.ControllerContext) error {
	logger := log.FromContext(ctx)
	job := &batchv1.Job{}
	jobName := generateJobName(dpuNode)
	// Checking job existed or not, if yes, delete it.
	if err := ctrlCtx.Get(ctx, client.ObjectKey{Namespace: dpu.Namespace, Name: jobName}, job); err == nil {
		if err := client.IgnoreNotFound(ctrlCtx.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationForeground))); err != nil {
			logger.Error(err, fmt.Sprintf("Unable to delete Job %s, err: %v", jobName, err))
			return err
		}
	}

	configMap := &corev1.ConfigMap{}
	configMapNamespacedName := types.NamespacedName{
		Namespace: dpu.Namespace,
		Name:      dpuNode.Spec.NodeRebootMethod.Script.Name,
	}
	if err := ctrlCtx.Get(ctx, configMapNamespacedName, configMap); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Error(err, fmt.Sprintf("ConfigMap %s not found for DPUNode %s", dpuNode.Spec.NodeRebootMethod.Script.Name, dpuNode.Name))
			return err
		}
		logger.Error(err, fmt.Sprintf("Unable to fetch ConfigMap %s for DPUNode %s", dpuNode.Spec.NodeRebootMethod.Script.Name, dpuNode.Name))
		return err
	}

	podTemplateStr, ok := configMap.Data[PodTemplateConfigMapKey]
	if !ok {
		err := fmt.Errorf("%s not found in ConfigMap", PodTemplateConfigMapKey)
		logger.Error(err, fmt.Sprintf("ConfigMap is missing %s key", PodTemplateConfigMapKey))
		return err
	}

	var podTemplate corev1.PodTemplateSpec
	if err := json.Unmarshal([]byte(podTemplateStr), &podTemplate); err != nil {
		logger.Error(err, fmt.Sprintf("Unable to unmarshal pod template from ConfigMap %s for DPUNode %s", dpuNode.Spec.NodeRebootMethod.Script.Name, dpuNode.Name))
		return err
	}

	var backoffLimit int32 = 0
	job = &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: dpu.Namespace,
		},
		Spec: batchv1.JobSpec{
			Template:     podTemplate,
			BackoffLimit: &backoffLimit,
		},
	}

	if err := ctrlCtx.Create(ctx, job); err != nil {
		logger.Error(err, fmt.Sprintf("Unable to create Job for DPUNode %s", dpuNode.Name))
		return err
	}

	return nil
}

func rebootHandler(ctx context.Context, dpuNode *provisioningv1.DPUNode, dpu *provisioningv1.DPU, pciAddress string, cmd string, retry int) {
	logger := log.FromContext(ctx)
	rebootTaskName := generateRebootTaskName(dpu)
	logger.V(3).Info(fmt.Sprintf("BF-SLR for %s", rebootTaskName))
	if dpuNode.Status.KubeNodeRef == nil {
		// non-k8s environment
		// TODO: add the reboot command for non-k8s environment
	} else {
		// k8s environment
		bfSLRShutdownARM := fmt.Sprintf("bf-slr.sh %s %s %s", pciAddress, cmd, "arm")
		bfSLRSRebootHost := fmt.Sprintf("bf-slr.sh %s %s %s", pciAddress, cmd, "host")

		rebootTask := future.New(func() (any, error) {
			// Shutdown ARM
			logger.V(3).Info(fmt.Sprintf("Bluefield System-Level-Reset ARM shutdown command: %s for dpu: %s", bfSLRShutdownARM, dpu.Name))
			if out, errMsg, err := cutil.RemoteExec(dpu.Namespace, cutil.GenerateDMSPodName(dpuNode.Name), "", bfSLRShutdownARM); err != nil {
				logger.Error(err, fmt.Sprintf("DPU %s failed to shutdown ARM: %v, output: %s", dpu.Name, err, out))
				return future.Ready, fmt.Errorf("DPU %s reboot failed: %v, output: %s", dpu.Name, err, errMsg)
			} else {
				logger.V(3).Info(fmt.Sprintf("DPU %s Bluefield System-Level-Reset result: %s", dpu.Name, out))
			}

			// Reboot Host
			logger.V(3).Info(fmt.Sprintf("Bluefield System-Level-Reset reboot host command: %s for dpu: %s", bfSLRSRebootHost, dpu.Name))
			if out, _, err := cutil.RemoteExec(dpu.Namespace, cutil.GenerateDMSPodName(dpuNode.Name), "", bfSLRSRebootHost); err != nil {
				logger.Error(err, fmt.Sprintf("Failed to reboot host: %v, output: %s", err, out))
				return future.Ready, err
			}

			return nil, nil
		})
		rebootTaskWithRetry := dutil.TaskWithRetry{
			Task:       rebootTask,
			RetryCount: retry,
		}
		dutil.RebootTaskMap.Store(rebootTaskName, rebootTaskWithRetry)
	}
}

func generateRebootTaskName(dpu *provisioningv1.DPU) string {
	return fmt.Sprintf("%s/%s", dpu.Namespace, dpu.Name)
}
