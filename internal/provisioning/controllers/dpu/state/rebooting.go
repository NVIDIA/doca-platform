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
	"strconv"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/util"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/future"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/reboot"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type dpuRebootingState struct {
	dpu *provisioningv1.DPU
}

func (st *dpuRebootingState) Handle(ctx context.Context, client client.Client, _ dutil.DPUOptions) (provisioningv1.DPUStatus, error) {
	logger := log.FromContext(ctx)

	rebootTaskName := generateRebootTaskName(st.dpu)
	state := st.dpu.Status.DeepCopy()
	if isDeleting(st.dpu) {
		dutil.RebootTaskMap.Delete(rebootTaskName)
		state.Phase = provisioningv1.DPUDeleting
		return *state, nil
	}

	nn := types.NamespacedName{
		Namespace: st.dpu.Namespace,
		Name:      st.dpu.Name,
	}

	dpu := provisioningv1.DPU{}
	if err := client.Get(ctx, nn, &dpu); err != nil {
		return *state, err
	}

	nn = types.NamespacedName{
		Namespace: "",
		Name:      dpu.Spec.NodeName,
	}
	node := &corev1.Node{}
	if err := client.Get(ctx, nn, node); err != nil {
		return *state, err
	}

	_, cond := cutil.GetDPUCondition(state, provisioningv1.DPUCondDMSRunning.String())
	if cond == nil || cond.Status != metav1.ConditionTrue {
		err := fmt.Errorf("trying to reboot the host before %s", provisioningv1.DPUCondOSInstalled.String())
		c := cutil.DPUCondition(provisioningv1.DPUCondRebooted, "InvalidState", err.Error())
		c.Status = metav1.ConditionFalse
		cutil.SetDPUCondition(state, c)
		return *state, err
	}

	duration := int(metav1.Now().Sub(cond.LastTransitionTime.Time).Seconds())
	// If we can not get uptime, the host should be rebooting
	uptime, err := HostUptime(st.dpu.Namespace, cutil.GenerateDMSPodName(st.dpu.Name), "")
	if err != nil {
		return *state, err
	}

	logger.V(3).Info(fmt.Sprintf("Rebooting duration is %d, host uptime is %d", duration, uptime))

	// If the pod is available after rebooting, move to next phase
	if duration > uptime {
		state.Phase = provisioningv1.DPUHostNetworkConfiguration
		cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondRebooted, "", ""))
		return *state, nil
	}

	if (dpu.Spec.NodeEffect != nil && dpu.Spec.NodeEffect.Drain != nil && dpu.Spec.NodeEffect.Drain.AutomaticNodeReboot) ||
		dpu.Spec.AutomaticNodeReboot {
		cmd, rebootType, err := reboot.GenerateCmd(node.Annotations, dpu.Annotations)
		if err != nil {
			logger.Error(err, "failed to generate ipmitool command")
			return *state, err
		}

		// Return early and set node to ready if we should skip the powercycle/reboot command.
		// Note: skipping the powercycle/reboot may cause issues with the firmware installation and configuration.
		if cmd == reboot.Skip {
			logger.Info("Warning not rebooting: this may cause issues with DPU firmware installation and configuration")
			state.Phase = provisioningv1.DPUHostNetworkConfiguration
			cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondRebooted, "", ""))
			return *state, nil
		} else if rebootType == reboot.PowerCycle {
			logger.Info(fmt.Sprintf("powercycle with command %q", cmd))
			if _, _, err := cutil.RemoteExec(st.dpu.Namespace, cutil.GenerateDMSPodName(st.dpu.Name), "", cmd); err != nil {
				// TODO: broadcast an event
				return *state, err
			}
		} else if rebootType == reboot.WarmReboot {
			if pciAddress, err := cutil.GetPCIAddrFromLabel(dpu.Labels, true); err != nil {
				logger.Error(err, "Failed to get pci address from node label", "dms", err)
				return *state, err
			} else {
				if dmsTask, ok := dutil.RebootTaskMap.Load(rebootTaskName); ok {
					result := dmsTask.(*future.Future)
					if result.GetState() == future.Ready {
						dutil.RebootTaskMap.Delete(rebootTaskName)
						if _, err := result.GetResult(); err == nil {
							cutil.SetDPUCondition(state, cutil.DPUCondition(provisioningv1.DPUCondRebooted, "", ""))
							return *state, nil
						} else {
							return updateState(state, provisioningv1.DPUError, err.Error()), err
						}
					}
				} else {
					rebootHandler(ctx, st.dpu, pciAddress, cmd)
				}
			}
		}
	} else {
		logger.Info("waiting for manual power cycle or reboot")
		c := cutil.DPUCondition(provisioningv1.DPUCondRebooted, "WaitingForManualPowerCycleOrReboot", "")
		c.Status = metav1.ConditionFalse
		cutil.SetDPUCondition(state, c)
		return *state, nil
	}

	return *state, nil
}

func rebootHandler(ctx context.Context, dpu *provisioningv1.DPU, pciAddress string, cmd string) {
	logger := log.FromContext(ctx)
	rebootTaskName := generateRebootTaskName(dpu)
	logger.V(3).Info(fmt.Sprintf("BF-SLR for %s", rebootTaskName))
	bfSLRShutdownARM := fmt.Sprintf("bf-slr.sh %s %s %s", pciAddress, cmd, "arm")
	bfSLRSRebootHost := fmt.Sprintf("bf-slr.sh %s %s %s", pciAddress, cmd, "host")

	dmsTask := future.New(func() (any, error) {
		// Shutdown ARM
		logger.V(3).Info(fmt.Sprintf("Bluefield System-Level-Reset ARM shutdown command: %s for dpu: %s", bfSLRShutdownARM, dpu.Name))
		if out, _, err := cutil.RemoteExec(dpu.Namespace, cutil.GenerateDMSPodName(dpu.Name), "", bfSLRShutdownARM); err != nil {
			logger.Error(err, fmt.Sprintf("DPU %s failed to shutdown ARM: %v, output: %s", dpu.Name, err, out))
			return future.Ready, err
		} else {
			logger.V(3).Info(fmt.Sprintf("DPU %s Bluefield System-Level-Reset result: %s", dpu.Name, out))
		}

		// Reboot Host
		logger.V(3).Info(fmt.Sprintf("Bluefield System-Level-Reset reboot host command: %s for dpu: %s", bfSLRSRebootHost, dpu.Name))
		if out, _, err := cutil.RemoteExec(dpu.Namespace, cutil.GenerateDMSPodName(dpu.Name), "", bfSLRSRebootHost); err != nil {
			logger.Error(err, fmt.Sprintf("Failed to reboot host: %v, output: %s", err, out))
			return future.Ready, err
		}

		return nil, nil
	})
	dutil.RebootTaskMap.Store(rebootTaskName, dmsTask)
}

func HostUptime(ns, name, container string) (int, error) {
	uptimeStr, _, err := cutil.RemoteExec(ns, name, container, "cat /proc/uptime")
	if err != nil {
		return -1, err
	}

	ts := strings.Fields(uptimeStr)
	if len(ts) != 2 {
		return -1, fmt.Errorf("uptime incorrect: %#v", ts)
	}

	uptime, err := strconv.ParseFloat(strings.TrimSpace(ts[0]), 64)
	if err != nil {
		return -1, err
	}

	return int(uptime), nil
}

func generateRebootTaskName(dpu *provisioningv1.DPU) string {
	return fmt.Sprintf("%s/%s", dpu.Namespace, dpu.Name)
}
