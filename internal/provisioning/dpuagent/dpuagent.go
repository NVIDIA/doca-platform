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
	"path/filepath"
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
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/nodelabels"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/nvconfig"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/ovsscript"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/packages"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/reboot"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/sfconfig"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/staticfiles"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/sysctl"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/systemd"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/vfmac"
	dpuutil "github.com/nvidia/doca-platform/internal/provisioning/dpuagent/util"
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const defaultRetryInterval = 30 * time.Second

const bootIDFile = "/proc/sys/kernel/random/boot_id"

const (
	defaultRunDir      = "/run/dpu-agent"
	doneMarkerFileName = "configuration-complete"
)

type DPUAgent struct {
	optCtx        *operations.Context
	operations    []operations.Operation
	retryInterval time.Duration
	runDir        string

	// rebootMethodDiscoveryFunc, if non-nil, replaces MFT tool probing (tests only).
	rebootMethodDiscoveryFunc func(context.Context) bool
	// writeDoneMarkerFunc, if non-nil, replaces the default marker writer (tests only).
	writeDoneMarkerFunc func(dir string) error
	// removeDoneMarkerFunc, if non-nil, replaces the default marker remover (tests only).
	removeDoneMarkerFunc func(dir string) error
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
		&packages.InstallPackages{},
		&systemd.ManageServices{},
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
		&nodelabels.ReportNodeLabels{},
	}
	return &DPUAgent{
		optCtx:     optCtx,
		operations: operations,
		runDir:     defaultRunDir,
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
	removeMarker := removeDoneMarker
	if d.removeDoneMarkerFunc != nil {
		removeMarker = d.removeDoneMarkerFunc
	}
	if err := removeMarker(d.runDir); err != nil {
		return fmt.Errorf("failed to remove stale done marker: %w", err)
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
				hostutil.NewCondition(op.ConditionType()).Success(dpuutil.TruncateConditionMessage(d.optCtx.CondMessage)).Set(&d.optCtx.Status.Conditions)
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
	writeMarker := writeDoneMarker
	if d.writeDoneMarkerFunc != nil {
		writeMarker = d.writeDoneMarkerFunc
	}
	if err := writeMarker(d.runDir); err != nil {
		return fmt.Errorf("failed to write done marker: %w", err)
	}
	d.updateStatusUntilSuccess(ctx)
	return nil
}

// updateStatusUntilSuccess fetches the latest DPU, verifies the UID, merges
// the in-memory AgentStatus fields, and patches until success.
func (d *DPUAgent) updateStatusUntilSuccess(ctx context.Context) {
	_ = wait.PollUntilContextCancel(ctx, 2*time.Second, true, func(updateCtx context.Context) (bool, error) {
		if err := d.updateStatus(updateCtx); err != nil {
			klog.Warningf("Failed to update DPU status: %v", err)
			return false, nil
		}
		return true, nil
	})
}

// updateStatus reads the latest DPU, validates UID, merges AgentStatus
// fields, and applies a status patch.
func (d *DPUAgent) updateStatus(ctx context.Context) error {
	latestDPU := &provisioningv1.DPU{}
	key := client.ObjectKey{Namespace: d.optCtx.Options.DPUNamespace, Name: d.optCtx.Options.DPUName}
	if err := d.optCtx.Client.Get(ctx, key, latestDPU); err != nil {
		return err
	}
	if string(latestDPU.UID) != d.optCtx.Options.DPUUID {
		return fmt.Errorf("stale DPU object: expected UID %s but got %s", d.optCtx.Options.DPUUID, latestDPU.UID)
	}
	patch := client.MergeFrom(latestDPU.DeepCopy())
	if latestDPU.Status.AgentStatus == nil {
		latestDPU.Status.AgentStatus = &provisioningv1.AgentStatus{
			Conditions: []metav1.Condition{},
		}
	}
	agentStatus := d.optCtx.Status
	if agentStatus.LastStartupTime != nil {
		latestDPU.Status.AgentStatus.LastStartupTime = agentStatus.LastStartupTime
	}
	if agentStatus.InitialBootID != nil {
		latestDPU.Status.AgentStatus.InitialBootID = agentStatus.InitialBootID
	}
	if agentStatus.RebootMethod != nil {
		latestDPU.Status.AgentStatus.RebootMethod = agentStatus.RebootMethod
	}
	if agentStatus.RebootSequenceCount != nil {
		latestDPU.Status.AgentStatus.RebootSequenceCount = agentStatus.RebootSequenceCount
	}
	if agentStatus.KubeletVersion != nil {
		latestDPU.Status.AgentStatus.KubeletVersion = agentStatus.KubeletVersion
	}
	if agentStatus.LastObservedPendingNVConfig != nil {
		latestDPU.Status.AgentStatus.LastObservedPendingNVConfig = agentStatus.LastObservedPendingNVConfig.DeepCopy()
	}
	for _, condition := range agentStatus.Conditions {
		meta.SetStatusCondition(&latestDPU.Status.AgentStatus.Conditions, condition)
	}
	return d.optCtx.Client.Status().Patch(ctx, latestDPU, patch)
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

func removeDoneMarker(dir string) error {
	markerPath := filepath.Join(dir, doneMarkerFileName)
	if err := os.Remove(markerPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale done marker %s: %w", markerPath, err)
	}
	return nil
}

func writeDoneMarker(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create run directory %s: %w", dir, err)
	}
	markerPath := filepath.Join(dir, doneMarkerFileName)
	if err := os.WriteFile(markerPath, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0644); err != nil {
		return fmt.Errorf("write done marker file: %w", err)
	}
	klog.Infof("Configuration complete, marker written to %s", markerPath)
	return nil
}
