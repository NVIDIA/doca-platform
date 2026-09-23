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

package agent

import (
	"context"
	"fmt"
	"strings"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/dns"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/getdpu"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/kubelet"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/laststartuptime"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/netplan"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations/nvconfig"
	hostutil "github.com/nvidia/doca-platform/internal/provisioning/hostagent/util"
	nvconfigutil "github.com/nvidia/doca-platform/internal/provisioning/utils/nvconfig"
	"github.com/nvidia/doca-platform/test/mock/dpuagent/pkg/config"
	"github.com/nvidia/doca-platform/test/mock/dpuagent/pkg/node"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// runInputs is what one agent run receives from the supervisor.
type runInputs struct {
	// rebootMethod is the element of agent.rebootMethod this run reports from Handle Reboot.
	rebootMethod provisioningv1.RebootMethodType
	// powerOff simulates `shutdown -h now` on the Redfish side (System Level Reset).
	powerOff func()
	joinMode config.JoinMode
	joiner   *node.Joiner
}

// op is a no-op operation that keeps the name, condition type, skip rule and status-update rule of
// the real operation and writes nothing but its own condition unless execute says otherwise.
type op struct {
	name         string
	condType     string
	skip         func(*operations.Context) bool
	updateBefore bool
	execute      func(context.Context, *operations.Context) error
}

func (o *op) Name() string          { return o.name }
func (o *op) ConditionType() string { return o.condType }
func (o *op) ShouldSkip(ctx *operations.Context) bool {
	return o.skip != nil && o.skip(ctx)
}
func (o *op) ShouldUpdateStatusBeforeContinue(*operations.Context) bool { return o.updateBefore }
func (o *op) Execute(ctx context.Context, optCtx *operations.Context) error {
	if o.execute == nil {
		return nil
	}
	return o.execute(ctx, optCtx)
}

func never(*operations.Context) bool { return false }

// alreadyTrue skips an operation whose condition the previous run already reported True, the way
// the real DNS operation does.
func alreadyTrue(condType string) func(*operations.Context) bool {
	return func(ctx *operations.Context) bool {
		if ctx.LatestDPU == nil || ctx.LatestDPU.Status.AgentStatus == nil {
			return false
		}
		cond := meta.FindStatusCondition(ctx.LatestDPU.Status.AgentStatus.Conditions, condType)
		return cond != nil && cond.Status == metav1.ConditionTrue
	}
}

// buildOperations returns the 28 operations of internal/provisioning/dpuagent NewDPUAgent in the
// same order with the same names, condition types, skip rules and status-update rules.
func buildOperations(in *runInputs) []operations.Operation {
	removeBuiltinKubelet, configureKubelet, startKubelet := kubeletOperations(in)
	return []operations.Operation{
		&op{name: "Load kernel modules", condType: "KernelModuleLoaded", skip: never},
		&op{name: "Configure Network", condType: "NetworkConfigured", skip: never},
		&netplan.CheckNetwork{},
		&laststartuptime.ReportLastStartupTime{},
		&getdpu.GetLatestDPU{},
		&op{name: "Configure DNS", condType: dns.CondDNSConfigured, skip: alreadyTrue(dns.CondDNSConfigured), updateBefore: true},
		&op{name: "Verify Static Files", condType: "StaticFilesVerified", skip: never},
		&op{name: "Install Packages", condType: "PackagesInstalled", updateBefore: true,
			skip: func(ctx *operations.Context) bool { return len(ctx.DPUFlavor.Spec.Packages) == 0 }},
		&op{name: "Manage Systemd Services", condType: "SystemdServicesManaged",
			skip: func(ctx *operations.Context) bool { return len(ctx.DPUFlavor.Spec.SystemdServices) == 0 }},
		removeBuiltinKubelet,
		&op{name: "Set Sysctl", condType: "SysctlParametersSet", skip: never},
		&op{name: "Check Sysctl Parameters", condType: "SysctlParametersChecked", skip: never},
		&op{name: "Configure Kernel Cmd Line", condType: "KernelCmdLineConfigured", skip: noKernelParameters},
		&op{name: "Configure Containerd", condType: "ContainerdConfigured", skip: never},
		&op{name: "Ensure DPU Mode", condType: "DpuModeEnsured", skip: never},
		// BF4 + Astra only. The real operation also drives the E/W NIC conditions from a runtime
		// loop the mock does not have; without them DPU Config does not wait for E/W runtime config.
		&op{name: "NIC provisioning", condType: "NICProvisioning",
			skip: func(ctx *operations.Context) bool { return !ctx.Options.AstraEnabled || ctx.Options.SkipAstra }},
		&op{name: "NVConfig", condType: nvconfig.CondNVConfigApplied, skip: never, updateBefore: true},
		&op{name: "Handle Reboot", condType: "RebootHandled", skip: never, updateBefore: true, execute: in.handleReboot},
		&op{name: "Check Kernel Cmd Line", condType: "KernelCmdLineChecked", skip: noKernelParameters},
		&op{name: "Configure SF", condType: "SFCreated", skip: never},
		&op{name: "Set VF MAC", condType: "VFMacSet", skip: never},
		&op{name: "Run OVS Script", condType: "OVSScriptRun",
			skip: func(ctx *operations.Context) bool {
				return strings.TrimSpace(ctx.DPUFlavor.Spec.OVS.RawConfigScript) == ""
			}},
		&op{name: "Set Netplan Underlay MTU", condType: "UnderlayNetplanMTUConfigured", skip: never},
		&op{name: "Check Bridge", condType: "BridgeChecked",
			skip: func(ctx *operations.Context) bool { return ctx.Options.ZeroTrustMode }},
		configureKubelet,
		startKubelet,
		&op{name: "Report Node Labels", condType: "NodeLabelsReported", skip: never, updateBefore: true},
		&op{name: "Release Host OS Init", condType: "ReleaseHostOSInit", skip: never, updateBefore: true, execute: releaseHostOSInit},
	}
}

func noKernelParameters(ctx *operations.Context) bool {
	return len(ctx.DPUFlavor.Spec.Grub.KernelParameters) == 0
}

// kubeletOperations returns the three kubelet operations. Remove Built-in Kubelet and Configure
// Kubelet only report their conditions in both modes; Start Kubelet is the one step that joins the
// DPU cluster. In simulated mode it replays kubeadm join and node registration in-process with
// client-go. In kubelet (VM) mode it is the real `systemctl start kubelet`, so the VM image is
// responsible for a kubelet that is already configured to join the DPU cluster.
func kubeletOperations(in *runInputs) (remove, configure, start operations.Operation) {
	remove = &op{name: "Remove Built-in Kubelet", condType: "BuiltinKubeletRemoved", skip: never}
	configure = &op{name: "Configure Kubelet", condType: "KubeletConfigured", skip: never, updateBefore: true}
	if in.joinMode == config.JoinKubelet {
		return remove, configure, &kubelet.StartKubelet{}
	}
	start = &op{name: "Start Kubelet", condType: "KubeletStarted", skip: never, execute: in.startKubelet}
	return remove, configure, start
}

// handleReboot replays reboot.HandleReboot on the device-query path with the method the
// configuration prescribes for this run. NoAction lets the pipeline continue; any other method is
// reported and the run then blocks until the host is rebooted (the run context is canceled).
func (in *runInputs) handleReboot(ctx context.Context, optCtx *operations.Context) error {
	method := in.rebootMethod
	setRebootMethodDiscoveryCondition(optCtx, method)

	var prev int32
	if optCtx.LatestDPU != nil && optCtx.LatestDPU.Status.AgentStatus != nil && optCtx.LatestDPU.Status.AgentStatus.RebootSequenceCount != nil {
		prev = *optCtx.LatestDPU.Status.AgentStatus.RebootSequenceCount
	}
	if method == provisioningv1.RebootMethodNoAction {
		optCtx.Status.RebootSequenceCount = ptr.To(int32(0))
		optCtx.Status.InitialBootID = nil
		optCtx.Status.RebootMethod = ptr.To(provisioningv1.RebootMethodNoAction)
		return nil
	}
	optCtx.Status.RebootSequenceCount = ptr.To(prev + 1)
	optCtx.Status.InitialBootID = ptr.To(optCtx.CurrentBootID)
	optCtx.Status.RebootMethod = ptr.To(method)
	if err := optCtx.UpdateStatusUntilSuccess(ctx); err != nil {
		return err
	}
	if method == provisioningv1.RebootMethodSystemLevelReset && in.powerOff != nil {
		klog.InfoS("System Level Reset reported; shutting the Arm down and waiting for the host reboot")
		in.powerOff()
	} else {
		klog.InfoS("reboot method reported; waiting for the host reboot", "method", method)
	}
	<-ctx.Done()
	return ctx.Err()
}

func setRebootMethodDiscoveryCondition(optCtx *operations.Context, method provisioningv1.RebootMethodType) {
	meta.SetStatusCondition(&optCtx.Status.Conditions, metav1.Condition{
		Type:               cutil.AgentCondRebootMethodDiscovery,
		Status:             metav1.ConditionTrue,
		Reason:             string(method),
		Message:            "mock-dpuagent: reboot method taken from agent.rebootMethod",
		LastTransitionTime: metav1.Now(),
	})
}

const kubeadmSecretJoinKey = "join"

// startKubelet replays what a kubelet does when it starts: run kubeadm join (read the join Secret
// with the agent identity and obtain the node client certificate), register the Node, then keep
// the Lease and Node status heartbeat running for the rest of this run.
func (in *runInputs) startKubelet(ctx context.Context, optCtx *operations.Context) error {
	if in.joiner == nil {
		return fmt.Errorf("no node joiner configured")
	}
	joinCmd := ""
	secret := &corev1.Secret{}
	err := optCtx.Client.Get(ctx, client.ObjectKey{Namespace: optCtx.Options.KubeadmSecretNamespace, Name: optCtx.Options.KubeadmSecretName}, secret)
	switch {
	case err == nil:
		joinCmd = string(secret.Data[kubeadmSecretJoinKey])
		if joinCmd == "" {
			return fmt.Errorf("kubeadm secret %s/%s has no %q key", optCtx.Options.KubeadmSecretNamespace, optCtx.Options.KubeadmSecretName, kubeadmSecretJoinKey)
		}
	case apierrors.IsNotFound(err) || apierrors.IsForbidden(err):
		// The controller deletes the join Secret after Cluster Config; a rebooted node keeps using
		// the certificate it already holds, exactly like a real kubelet.
		klog.InfoS("kubeadm join secret not readable, reusing existing node credentials", "err", err.Error())
	default:
		return fmt.Errorf("get kubeadm secret: %w", err)
	}
	if err := in.joiner.EnsureCredentials(ctx, joinCmd); err != nil {
		return err
	}
	version, err := in.joiner.ServerVersion()
	if err != nil {
		return fmt.Errorf("get DPU cluster version: %w", err)
	}
	if err := in.joiner.RegisterNode(ctx, version); err != nil {
		return err
	}
	optCtx.Status.KubeletVersion = &version
	go in.joiner.RunHeartbeat(ctx, version)
	return nil
}

// releaseHostOSInit mirrors hostosinit.ReleaseHostOSInit without mlxreg: the previous outcome is
// cleared, then a flavor without DELAY_HOST_OS_INIT user mode records Skipped and one with it
// records Succeeded, which is what Service Readiness waits for.
func releaseHostOSInit(ctx context.Context, optCtx *operations.Context) error {
	optCtx.ClearHostOSInit = true
	optCtx.Status.HostOSInit = nil
	if err := optCtx.UpdateStatusUntilSuccess(ctx); err != nil {
		return err
	}
	optCtx.ClearHostOSInit = false
	if !nvconfigutil.FlavorRequestsHostOSInitHold(optCtx.DPUFlavor.Spec.NVConfig) {
		reason, message := "ReleaseNotRequired", "DELAY_HOST_OS_INIT is not set to ENABLE_USER (0x3) in flavor nvconfig"
		optCtx.Status.HostOSInit = &provisioningv1.HostOSInitStatus{
			Skipped: &provisioningv1.HostOSInitSkipped{Reason: &reason, Message: &message},
		}
		hostutil.NewCondition("ReleaseHostOSInit").Success(message).Set(&optCtx.Status.Conditions)
		return optCtx.UpdateStatusUntilSuccess(ctx)
	}
	gate := optCtx.DPUFlavor.ReleaseGate()
	optCtx.Status.HostOSInit = &provisioningv1.HostOSInitStatus{
		Succeeded: &provisioningv1.HostOSInitSucceeded{Gate: gate},
	}
	hostutil.NewCondition("ReleaseHostOSInit").Success("").Set(&optCtx.Status.Conditions)
	return optCtx.UpdateStatusUntilSuccess(ctx)
}
