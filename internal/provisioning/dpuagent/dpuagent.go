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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	dpuagentclient "github.com/nvidia/doca-platform/internal/provisioning/dpuagent/client"
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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
		&nicprovisioning.NICProvisioning{},
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
		if err := d.checkBootstrapAbort(ctx); err != nil {
			return err
		}
		if op.ShouldSkip(d.optCtx) {
			klog.Infof("Skipping operation %s", op.Name())
			continue
		}

		err := wait.PollUntilContextCancel(ctx, d.retryInterval, true, func(execCtx context.Context) (bool, error) {
			if err := d.checkBootstrapAbort(execCtx); err != nil {
				return false, err
			}
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
				if updateErr := d.updateStatusUntilSuccess(execCtx); updateErr != nil {
					return false, updateErr
				}
			}
			return err == nil, nil
		})
		if err != nil {
			if isBootstrapAbortErr(err) {
				return err
			}
			return fmt.Errorf("execution of operator %s aborted: %w", op.Name(), err)
		}
	}
	if err := d.checkBootstrapAbort(ctx); err != nil {
		return err
	}
	writeMarker := writeDoneMarker
	if d.writeDoneMarkerFunc != nil {
		writeMarker = d.writeDoneMarkerFunc
	}
	if err := writeMarker(d.runDir); err != nil {
		return fmt.Errorf("failed to write done marker: %w", err)
	}
	if err := d.updateStatusUntilSuccess(ctx); err != nil {
		return err
	}
	d.logNICProvisioningRetainedResources()
	return nil
}

func (d *DPUAgent) logNICProvisioningRetainedResources() {
	for _, op := range d.operations {
		if nicProvisioning, ok := op.(*nicprovisioning.NICProvisioning); ok {
			nicProvisioning.LogRetainedResources()
			return
		}
	}
}

// Shutdown releases resources that remain available after provisioning completes.
func (d *DPUAgent) Shutdown() error {
	for _, op := range d.operations {
		if nicProvisioning, ok := op.(*nicprovisioning.NICProvisioning); ok {
			return nicProvisioning.Shutdown()
		}
	}
	return nil
}

// StartDPUReconcileLoop starts the owned-DPU watch/reconcile loop in background.
func (d *DPUAgent) StartDPUReconcileLoop(ctx context.Context) {
	go func() {
		if err := d.runDPUReconcileLoop(ctx); err != nil && ctx.Err() == nil {
			klog.Fatalf("failed to run DPU agent reconcile loop: %v", err)
		}
	}()
}

// runDPUReconcileLoop blocks until ctx is canceled. Reconciles the owned DPU on Kubernetes
// watch wakeups (pre-install NVCONFIG at Config FW Parameters during reprovision).
func (d *DPUAgent) runDPUReconcileLoop(ctx context.Context) error {
	klog.Info("Starting owned DPU reconcile loop")
	trigger := func() {
		defer func() {
			if r := recover(); r != nil {
				klog.Errorf("owned DPU reconcile panicked, recovered: %v", r)
			}
		}()
		if err := d.reconcileOwnedDPU(ctx); err != nil {
			klog.Warningf("owned DPU reconcile: %v", err)
		}
	}
	if d.optCtx.WatchClient == nil {
		klog.Info("No watch client configured; skipping owned DPU reconcile loop")
		return nil
	}
	return dpuagentclient.RunDPUWatch(ctx, d.optCtx.WatchClient, d.optCtx.Options.DPUNamespace, d.optCtx.Options.DPUName, trigger)
}

func (d *DPUAgent) reconcileOwnedDPU(ctx context.Context) error {
	dpu := &provisioningv1.DPU{}
	if err := d.optCtx.Client.Get(ctx, client.ObjectKey{Namespace: d.optCtx.Options.DPUNamespace, Name: d.optCtx.Options.DPUName}, dpu); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get owned DPU: %w", err)
	}
	uidChanged := dpuUIDChanged(d.optCtx, dpu)

	localCtx := d.snapshotPreInstallCtx(dpu)

	if uidChanged {
		localCtx.Status.PreInstall = nil
		if err := d.reportPreInstallAgentReported(ctx, dpu, &localCtx); err != nil {
			return err
		}
		if nvconfig.ShouldConfigureNVConfig(&localCtx) {
			klog.Infof("DPU reconcile: best-effort pre-install NVCONFIG for DPU %s/%s phase %s",
				dpu.Namespace, dpu.Name, dpu.Status.Phase)
			preInstallOp := &nvconfig.PreInstallConfigureNVConfig{}
			if err := d.runPreInstallOperationOnce(ctx, preInstallOp, &localCtx); err != nil {
				return err
			}
		}
	}
	return nil
}

func (d *DPUAgent) snapshotPreInstallCtx(dpu *provisioningv1.DPU) operations.Context {
	return operations.Context{
		Options:                  d.optCtx.Options,
		RebootMethodDiscovery:    d.optCtx.RebootMethodDiscovery,
		Client:                   d.optCtx.Client,
		WatchClient:              d.optCtx.WatchClient,
		K8sClient:                d.optCtx.K8sClient,
		DPUFlavor:                d.optCtx.DPUFlavor,
		LatestDPU:                dpu.DeepCopy(),
		DiscoverPorts:            d.optCtx.DiscoverPorts,
		CurrentBootID:            d.optCtx.CurrentBootID,
		UpdateStatusUntilSuccess: d.optCtx.UpdateStatusUntilSuccess,
	}
}

// StartNICRuntimeConfigLoop starts the post-provisioning E/W NIC runtime config
// loop (first apply with retry, then periodic reapply). No-op when NIC provisioning
// did not start a DMS session.
func (d *DPUAgent) StartNICRuntimeConfigLoop(ctx context.Context) {
	for _, op := range d.operations {
		if nicProvisioning, ok := op.(*nicprovisioning.NICProvisioning); ok {
			nicProvisioning.StartRuntimeConfigLoop(ctx, d.optCtx)
			return
		}
	}
}

func (d *DPUAgent) reportPreInstallAgentReported(ctx context.Context, dpu *provisioningv1.DPU, optCtx *operations.Context) error {
	if preInstallAgentReported(dpu) {
		return nil
	}
	d.ensurePreInstallStatus(optCtx)
	now := metav1.Now()
	optCtx.Status.PreInstall.AgentReported = &now
	klog.Infof("owned DPU reconcile: set preInstall.agentReported=%s for DPU %s/%s uid %s",
		now.Format(time.RFC3339), dpu.Namespace, dpu.Name, dpu.UID)
	d.updatePreInstallStatusUntilSuccess(ctx, optCtx)
	return nil
}

// updateStatusUntilSuccess fetches the latest DPU, verifies the UID, merges
// the in-memory AgentStatus fields, and patches until success.
func (d *DPUAgent) updateStatusUntilSuccess(ctx context.Context) error {
	return wait.PollUntilContextCancel(ctx, 2*time.Second, true, func(updateCtx context.Context) (bool, error) {
		if err := d.checkBootstrapAbort(updateCtx); err != nil {
			if isBootstrapAbortErr(err) {
				klog.Info("Skipping status update after DPU reprovision was detected")
				// Propagate abort so bootstrap can hand over to pre-install reconcile.
				return false, err
			}
			klog.Warningf("Failed to check bootstrap abort before status update: %v", err)
			return false, nil
		}
		if err := d.updateStatus(updateCtx); err != nil {
			klog.Warningf("Failed to update DPU status: %v", err)
			return false, nil
		}
		return true, nil
	})
}

func preInstallAgentReported(dpu *provisioningv1.DPU) bool {
	if dpu == nil || dpu.Status.AgentStatus == nil || dpu.Status.AgentStatus.PreInstall == nil {
		return false
	}
	reported := dpu.Status.AgentStatus.PreInstall.AgentReported
	return reported != nil && !reported.IsZero()
}

func (d *DPUAgent) runPreInstallOperationOnce(ctx context.Context, op operations.Operation, optCtx *operations.Context) error {
	optCtx.CondMessage = ""
	execErr := op.Execute(ctx, optCtx)
	d.ensurePreInstallStatus(optCtx)
	if execErr != nil {
		klog.Errorf("[%s] Failed to execute (best-effort pre-install). err: %v", op.Name(), execErr)
		hostutil.NewCondition(op.ConditionType()).Failure(execErr, "FailedToExecute").Set(&optCtx.Status.PreInstall.Conditions)
	} else {
		klog.Infof("[%s] Successfully executed (pre-install)", op.Name())
		hostutil.NewCondition(op.ConditionType()).Success(dpuutil.TruncateConditionMessage(optCtx.CondMessage)).Set(&optCtx.Status.PreInstall.Conditions)
	}
	if op.ShouldUpdateStatusBeforeContinue(optCtx) {
		d.updatePreInstallStatusUntilSuccess(ctx, optCtx)
	}
	return nil
}

func (d *DPUAgent) ensurePreInstallStatus(optCtx *operations.Context) {
	if optCtx.Status.PreInstall == nil {
		optCtx.Status.PreInstall = &provisioningv1.AgentPreInstallStatus{}
	}
	if optCtx.Status.PreInstall.Conditions == nil {
		optCtx.Status.PreInstall.Conditions = []metav1.Condition{}
	}
}

func (d *DPUAgent) updatePreInstallStatusUntilSuccess(ctx context.Context, optCtx *operations.Context) {
	_ = wait.PollUntilContextCancel(ctx, 2*time.Second, true, func(updateCtx context.Context) (bool, error) {
		if err := d.updatePreInstallStatus(updateCtx, optCtx); err != nil {
			klog.Warningf("Failed to update DPU pre-install status: %v", err)
			return false, nil
		}
		return true, nil
	})
}

// updatePreInstallStatus patches only agentStatus.preInstall.* and intentionally
// does not merge regular agentStatus fields to avoid old-OS status pollution after adopt.
func (d *DPUAgent) updatePreInstallStatus(ctx context.Context, optCtx *operations.Context) error {
	if optCtx.Status.PreInstall == nil {
		return nil
	}

	latestDPU := &provisioningv1.DPU{}
	key := client.ObjectKey{Namespace: optCtx.Options.DPUNamespace, Name: optCtx.Options.DPUName}
	if err := optCtx.Client.Get(ctx, key, latestDPU); err != nil {
		return err
	}

	patch := client.MergeFrom(latestDPU.DeepCopy())
	if latestDPU.Status.AgentStatus == nil {
		latestDPU.Status.AgentStatus = &provisioningv1.AgentStatus{
			Conditions: []metav1.Condition{},
		}
	}
	if latestDPU.Status.AgentStatus.PreInstall == nil {
		latestDPU.Status.AgentStatus.PreInstall = &provisioningv1.AgentPreInstallStatus{
			Conditions: []metav1.Condition{},
		}
	}
	if reported := optCtx.Status.PreInstall.AgentReported; reported != nil && !reported.IsZero() {
		latestDPU.Status.AgentStatus.PreInstall.AgentReported = reported.DeepCopy()
	}
	for _, condition := range optCtx.Status.PreInstall.Conditions {
		meta.SetStatusCondition(&latestDPU.Status.AgentStatus.PreInstall.Conditions, condition)
	}
	return optCtx.Client.Status().Patch(ctx, latestDPU, patch)
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

// errBootstrapAbortedForReprovision indicates bootstrap exited so the owned-DPU watch can handle reprovision.
var errBootstrapAbortedForReprovision = errors.New("bootstrap aborted for DPU reprovision")

// checkBootstrapAbort refreshes the owned DPU and exits bootstrap when reprovision is detected.
func (d *DPUAgent) checkBootstrapAbort(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dpu := &provisioningv1.DPU{}
	err := d.optCtx.Client.Get(ctx, client.ObjectKey{Namespace: d.optCtx.Options.DPUNamespace, Name: d.optCtx.Options.DPUName}, dpu)
	if err != nil {
		if apierrors.IsNotFound(err) {
			klog.Info("DPU was deleted; aborting bootstrap for reprovision")
			return errBootstrapAbortedForReprovision
		}
		return err
	}
	if dpuUIDChanged(d.optCtx, dpu) {
		klog.Info("DPU was recreated with a new UID; aborting bootstrap for reprovision")
		return errBootstrapAbortedForReprovision
	}
	return nil
}

func isBootstrapAbortErr(err error) bool {
	return errors.Is(err, errBootstrapAbortedForReprovision)
}

// IsBootstrapAbortErr reports whether err indicates bootstrap exited for reprovision.
func IsBootstrapAbortErr(err error) bool {
	return isBootstrapAbortErr(err)
}

// dpuUIDChanged reports whether the runtime DPU UID differs from startup Options.DPUUID.
// This is a read-only reprovision check used by bootstrap-abort detection.
func dpuUIDChanged(optCtx *operations.Context, dpu *provisioningv1.DPU) bool {
	if optCtx == nil || dpu == nil {
		return false
	}
	return optCtx.Options.DPUUID != "" && optCtx.Options.DPUUID != string(dpu.UID)
}
