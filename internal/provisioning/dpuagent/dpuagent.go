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
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/devicepluginregistry"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/dns"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/dpumode"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/getdpu"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/grub"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/hostosinit"
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
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/sriovconfig"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/staticfiles"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/sysctl"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/systemd"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/underlaymtu"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/vfmac"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/statusmanager"
	dpuutil "github.com/nvidia/doca-platform/internal/provisioning/dpuagent/util"
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"
	"github.com/nvidia/doca-platform/internal/provisioning/utils/bash"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
		&sriovconfig.ReconcileSF{}, // ReconcileSF should run before ReconcileVF (to enable hostless switchdev).
		&sriovconfig.ReconcileVF{},
		&vfmac.SetVFMac{},
		&ovsscript.RunOVSScript{},
		&underlaymtu.SetNetplanUnderlayMTU{},
		&checkbridge.CheckBridge{},
		&kubelet.ConfigureKubelet{},
		&devicepluginregistry.CleanPluginRegistry{},
		&kubelet.StartKubelet{},
		&nodelabels.ReportNodeLabels{},
		&hostosinit.ReleaseHostOSInit{},
	}
	optCtx.Status = statusmanager.New(optCtx.Client, optCtx.Options.DPUNamespace, optCtx.Options.DPUName, optCtx.Options.DPUUID)
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
	d.optCtx.Status.Start(ctx)
	d.optCtx.RebootMethodDiscovery = d.resolveRebootMethodDiscovery(ctx)
	d.optCtx.Status.UpdateLocal(func(s *provisioningv1.AgentStatus) {
		*s = provisioningv1.AgentStatus{
			Conditions:   []metav1.Condition{},
			RebootMethod: ptr.To(provisioningv1.RebootMethodUnknown),
		}
	})
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
				d.optCtx.Status.UpdateLocal(func(s *provisioningv1.AgentStatus) {
					hostutil.NewCondition(op.ConditionType()).Failure(err, "FailedToExecute").Set(&s.Conditions)
				})
			} else {
				klog.Infof("[%s] Successfully executed", op.Name())
				d.optCtx.Status.UpdateLocal(func(s *provisioningv1.AgentStatus) {
					hostutil.NewCondition(op.ConditionType()).Success(dpuutil.TruncateConditionMessage(d.optCtx.CondMessage)).Set(&s.Conditions)
				})
			}
			if err != nil || op.ShouldUpdateStatusBeforeContinue(d.optCtx) {
				if updateErr := d.optCtx.Status.UpdateRemote(true); updateErr != nil {
					return false, abortIfDPUGone(updateErr)
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
	if err := d.optCtx.Status.UpdateRemote(true); err != nil {
		return abortIfDPUGone(err)
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
	// Pre-install reports to a recreated DPU, so it has its own status manager without the
	// startup UID check. Operations reach it through the pre-install context's Status.
	preInstallStatus := statusmanager.New(d.optCtx.Client, d.optCtx.Options.DPUNamespace, d.optCtx.Options.DPUName, "")
	trigger := func() {
		defer func() {
			if r := recover(); r != nil {
				klog.Errorf("owned DPU reconcile panicked, recovered: %v", r)
			}
		}()
		if err := d.reconcileOwnedDPU(ctx, preInstallStatus); err != nil {
			klog.Warningf("owned DPU reconcile: %v", err)
		}
	}
	if d.optCtx.WatchClient == nil {
		klog.Info("No watch client configured; skipping owned DPU reconcile loop")
		return nil
	}
	preInstallStatus.Start(ctx)
	return dpuagentclient.RunDPUWatch(ctx, d.optCtx.WatchClient, d.optCtx.Options.DPUNamespace, d.optCtx.Options.DPUName, trigger)
}

func (d *DPUAgent) reconcileOwnedDPU(ctx context.Context, preInstallStatus *statusmanager.Manager) error {
	dpu := &provisioningv1.DPU{}
	if err := d.optCtx.Client.Get(ctx, client.ObjectKey{Namespace: d.optCtx.Options.DPUNamespace, Name: d.optCtx.Options.DPUName}, dpu); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get owned DPU: %w", err)
	}
	uidChanged := dpuUIDChanged(d.optCtx, dpu)

	if uidChanged {
		// Each reconcile starts from an empty pre-install status, as reported for this DPU only.
		preInstallStatus.UpdateLocal(func(s *provisioningv1.AgentStatus) {
			*s = provisioningv1.AgentStatus{}
		})
		localCtx := d.snapshotPreInstallCtx(dpu, preInstallStatus)

		if err := d.reportPreInstallAgentReported(dpu, &localCtx); err != nil {
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

func (d *DPUAgent) snapshotPreInstallCtx(dpu *provisioningv1.DPU, status *statusmanager.Manager) operations.Context {
	// Deliberately omit resolvedNVConfig / nsPorts caches: pre-install may swap
	// DPUFlavor and must re-resolve against discovery on this snapshot.
	return operations.Context{
		Options:               d.optCtx.Options,
		RebootMethodDiscovery: d.optCtx.RebootMethodDiscovery,
		Client:                d.optCtx.Client,
		WatchClient:           d.optCtx.WatchClient,
		K8sClient:             d.optCtx.K8sClient,
		DPUFlavor:             d.optCtx.DPUFlavor,
		LatestDPU:             dpu.DeepCopy(),
		DiscoverPorts:         d.optCtx.DiscoverPorts,
		CurrentBootID:         d.optCtx.CurrentBootID,
		Status:                status,
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

func (d *DPUAgent) reportPreInstallAgentReported(dpu *provisioningv1.DPU, optCtx *operations.Context) error {
	if preInstallAgentReported(dpu) {
		return nil
	}
	now := metav1.Now()
	optCtx.Status.UpdateLocal(func(s *provisioningv1.AgentStatus) {
		d.ensurePreInstallStatus(s)
		s.PreInstall.AgentReported = &now
	})
	klog.Infof("owned DPU reconcile: set preInstall.agentReported=%s for DPU %s/%s uid %s",
		now.Format(time.RFC3339), dpu.Namespace, dpu.Name, dpu.UID)
	d.updatePreInstallStatusUntilSuccess(optCtx)
	return nil
}

// StartCACertUpdateLoop starts the background CA certificate update loop.
func (d *DPUAgent) StartCACertUpdateLoop(ctx context.Context) {
	d.startCATrustBundleWatcher(ctx)
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
	if execErr != nil {
		klog.Errorf("[%s] Failed to execute (best-effort pre-install). err: %v", op.Name(), execErr)
	} else {
		klog.Infof("[%s] Successfully executed (pre-install)", op.Name())
	}
	optCtx.Status.UpdateLocal(func(s *provisioningv1.AgentStatus) {
		d.ensurePreInstallStatus(s)
		if execErr != nil {
			hostutil.NewCondition(op.ConditionType()).Failure(execErr, "FailedToExecute").Set(&s.PreInstall.Conditions)
		} else {
			hostutil.NewCondition(op.ConditionType()).Success(dpuutil.TruncateConditionMessage(optCtx.CondMessage)).Set(&s.PreInstall.Conditions)
		}
	})
	if op.ShouldUpdateStatusBeforeContinue(optCtx) {
		d.updatePreInstallStatusUntilSuccess(optCtx)
	}
	return nil
}

func (d *DPUAgent) ensurePreInstallStatus(s *provisioningv1.AgentStatus) {
	if s.PreInstall == nil {
		s.PreInstall = &provisioningv1.AgentPreInstallStatus{}
	}
	if s.PreInstall.Conditions == nil {
		s.PreInstall.Conditions = []metav1.Condition{}
	}
}

// updatePreInstallStatusUntilSuccess pushes the pre-install status until it succeeds and ignores
// the error. The pre-install manager has no UID, so while the DPU is missing it keeps retrying
// until the agent stops, and the owned-DPU watch waits for it, as before.
func (d *DPUAgent) updatePreInstallStatusUntilSuccess(optCtx *operations.Context) {
	_ = optCtx.Status.UpdateRemote(true)
}

func (d *DPUAgent) resolveRebootMethodDiscovery(ctx context.Context) bool {
	if d.optCtx.Options.SkipRebootMethodDiscovery {
		klog.Infof("RebootMethodDiscovery=false: skip-reboot-method-discovery is set (legacy boot-ID path)")
		return false
	}
	if d.optCtx.Options.DPUType == string(provisioningv1.DPUTypeBlueField4) {
		klog.Info("RebootMethodDiscovery=true: BlueField4 supports device-query path")
		return true
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
var errBootstrapAbortedForReprovision = operations.ErrBootstrapAborted

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

// abortIfDPUGone turns a status push error caused by a deleted or recreated DPU into
// errBootstrapAbortedForReprovision, so bootstrap hands over to the reprovision reconcile.
func abortIfDPUGone(err error) error {
	if apierrors.IsNotFound(err) || errors.Is(err, statusmanager.ErrStaleDPU) {
		klog.Infof("Skipping status update after DPU reprovision was detected: %v", err)
		return errBootstrapAbortedForReprovision
	}
	return err
}

func isBootstrapAbortErr(err error) bool {
	return errors.Is(err, operations.ErrBootstrapAborted)
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
