/*
Copyright 2026 NVIDIA

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

package dpuagent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/checkbridge"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/containerd"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/dns"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/dpumode"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/getdpu"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/grub"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/kernelmodule"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/kubelet"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/laststartuptime"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/netplan"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/nicprovisioning"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/nvconfig"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/ovsscript"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/reboot"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/sfconfig"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/staticfiles"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/sysctl"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/vfmac"
	dpuagentutil "github.com/nvidia/doca-platform/internal/provisioning/dpuagent/util"
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
)

const defaultRetryInterval = 30 * time.Second

const bootIDFile = "/proc/sys/kernel/random/boot_id"

type DPUAgent struct {
	optCtx        *operations.Context
	operations    []operations.Operation
	retryInterval time.Duration

	// rebootMethodDiscoveryFunc, if non-nil, replaces MFT tool probing (tests only).
	rebootMethodDiscoveryFunc func(context.Context) bool
}

func NewDPUAgent(optCtx *operations.Context) *DPUAgent {
	// The DPU Agent executes operations sequentially in the order defined in the slice.
	operations := []operations.Operation{
		&kernelmodule.LoadModule{},
		&netplan.ConfigureNetwork{},
		&netplan.CheckNetwork{},
		&laststartuptime.ReportLastStartupTime{},
		&getdpu.GetLatestDPU{},
		&nicprovisioning.NICProvisioning{},
		&dns.ConfigureDNS{},
		&staticfiles.VerifyStaticFiles{},
		&kubelet.RemoveBuiltinKubelet{},
		&sysctl.SetParams{},
		&sysctl.CheckParams{},
		&grub.ConfigureKernelCmdLine{},
		&containerd.ConfigureContainerd{},
		&dpumode.EnsureMode{},
		&nvconfig.ConfigureNVConfig{},
		&reboot.HandleReboot{},
		&grub.CheckKernelCmdLine{},
		&sfconfig.CreateSF{},
		&vfmac.SetVFMac{},
		&ovsscript.RunOVSScript{},
		&checkbridge.CheckBridge{},
		&kubelet.ConfigureKubelet{},
		&kubelet.StartKubelet{},
	}
	return &DPUAgent{
		optCtx:     optCtx,
		operations: operations,
	}
}

func (d *DPUAgent) Run(ctx context.Context) error {
	if d.retryInterval == 0 {
		d.retryInterval = defaultRetryInterval
	}
	d.optCtx.UpdateStatusUntilSuccess = d.updateStatusUntilSuccess
	d.optCtx.RebootMethodDiscovery = d.resolveRebootMethodDiscovery(ctx)
	d.optCtx.Status = provisioningv1.AgentStatus{
		Conditions:   []metav1.Condition{},
		RebootMethod: ptr.To(provisioningv1.RebootMethodUnknown),
	}
	if err := d.initCurrentBootID(); err != nil {
		return err
	}
	if err := d.initNSNIC(); err != nil {
		return err
	}
	for _, op := range d.operations {
		if op.ShouldSkip(d.optCtx) {
			klog.Infof("Skipping operation %s", op.Name())
			continue
		}

		// Execute the operations until success
		err := wait.PollUntilContextCancel(ctx, d.retryInterval, true, func(execCtx context.Context) (bool, error) {
			d.optCtx.CondMessage = ""
			err := op.Execute(execCtx, d.optCtx)
			if err != nil {
				klog.Errorf("[%s] Failed to execute, retrying. err: %v", op.Name(), err)
				hostutil.NewCondition(op.ConditionType()).Failure(err, "FailedToExecute").Set(&d.optCtx.Status.Conditions)
			} else {
				klog.Infof("[%s] Successfully executed", op.Name())
				hostutil.NewCondition(op.ConditionType()).Success(d.optCtx.CondMessage).Set(&d.optCtx.Status.Conditions)
			}

			if err != nil || op.ShouldUpdateStatusBeforeContinue(d.optCtx) {
				d.updateStatusUntilSuccess(execCtx)
			}
			return err == nil, nil
		})
		// The only reason for error here is context cancellation
		if err != nil {
			return fmt.Errorf("execution of operator %s aborted: %v", op.Name(), err)
		}
	}
	d.updateStatusUntilSuccess(ctx)
	return nil
}

// updateStatusUntilSuccess updates the status until success
func (d *DPUAgent) updateStatusUntilSuccess(ctx context.Context) {
	_ = wait.PollUntilContextCancel(ctx, 2*time.Second, true, func(updateCtx context.Context) (bool, error) {
		if err := d.optCtx.Client.UpdateStatus(updateCtx, d.optCtx.Status); err != nil {
			klog.Warningf("Failed to update DPU status: %v", err)
			return false, nil
		}
		return true, nil
	})
}

func (d *DPUAgent) resolveRebootMethodDiscovery(ctx context.Context) bool {
	if d.optCtx.Options.SkipRebootMethodDiscovery {
		klog.Infof("RebootMethodDiscovery=false: skip-reboot-method-discovery is set (legacy boot-ID path)")
		return false
	}
	if d.rebootMethodDiscoveryFunc != nil {
		return d.rebootMethodDiscoveryFunc(ctx)
	}
	return reboot.ResolveRebootMethodDiscovery(bash.Run)
}

func (d *DPUAgent) initCurrentBootID() error {
	currentBootID, err := os.ReadFile(bootIDFile)
	if err != nil {
		return fmt.Errorf("initialize current boot ID: %w", err)
	}
	d.optCtx.CurrentBootID = strings.TrimSpace(string(currentBootID))
	return nil
}

func (d *DPUAgent) initNSNIC() error {
	if d.optCtx.NSNIC != nil {
		return nil
	}
	dev, err := dpuagentutil.DiscoverNSNIC(hostutil.SysFSRoot)
	if err != nil {
		return fmt.Errorf("initialize N/S NIC: %w", err)
	}
	d.optCtx.NSNIC = dev
	return nil
}
