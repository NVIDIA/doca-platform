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

package operations

import (
	"context"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	pciutil "github.com/nvidia/doca-platform/internal/provisioning/utils/pci"

	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DefaultKubeletKubeconfigPath is the default path to the kubelet kubeconfig on DPU nodes.
const DefaultKubeletKubeconfigPath = "/etc/kubernetes/kubelet.conf"

// Operation is the interface for all operations.
// The same optCtx instance is passed to all operations, which can be used to pass data between operations.
// Since the optCtx is not thread-safe, do not execute operations in parallel.
type Operation interface {
	Name() string
	ConditionType() string
	Execute(execCtx context.Context, optCtx *Context) error
	ShouldSkip(optCtx *Context) bool
	ShouldUpdateStatusBeforeContinue(optCtx *Context) bool
}

// Context is the context for all operations.
// It is passed to all operations and can be used to pass data between operations.
// Since the Context is not thread-safe, do not access it from multiple goroutines.
type Context struct {
	// Options are the options for the DPU agent, loaded at startup (e.g. from command line).
	Options opts.Options

	// RebootMethodDiscovery is set once per agent run before operations execute.
	// When true, the agent reports RebootMethodDiscovery=True (device-query path) on status condition;
	RebootMethodDiscovery bool

	// Client is the controller-runtime client used to fetch Kubernetes objects
	// (e.g. DPU, Secret, BlueFieldSoftware). Both zero-trust and trusted-host
	// modes use the same client; trusted-host routes through an HTTP proxy.
	Client client.Client

	// WatchClient watches DPU CR changes for owned-DPU reprovision reconcile.
	WatchClient client.WithWatch

	// K8sClient is the typed kubernetes clientset, used by operations that need
	// APIs not exposed by the controller-runtime client (e.g. Discovery).
	K8sClient kubernetes.Interface

	// DPUFlavor is the desired configuration template for this DPU, loaded at startup (e.g. from --dpuflavor YAML).
	// Operations use it to apply sysctl, config files, grub, OVS, SF counts, and other flavor-defined settings.
	DPUFlavor provisioningv1.DPUFlavor

	// LatestDPU is the current DPU resource, typically populated by nvconfig.GetLatestDPU.
	// Operations use it to read spec/status and to decide behavior.
	LatestDPU *provisioningv1.DPU

	// Status is the in-memory DPU internal status. Operations read and update it;
	// the agent pushes it to the API via the runner's updateStatus method.
	Status provisioningv1.AgentStatus

	// CondMessage is cleared before each operation attempt. On success, dpuagent
	// truncates and writes it to the operation condition message.
	CondMessage string

	// DeferredNVConfigParams lists flavor NVConfig parameters not exposed by mlxconfig q
	// on the current pass (device-query path). Populated by nvconfig; reboot fails with
	// NoAction while any entries remain because they cannot be applied without a reboot.
	DeferredNVConfigParams []DeferredNVConfigParam

	// CurrentBootID caches the current host boot_id for this agent run so reboot
	// operations do not need to read it repeatedly.
	CurrentBootID string

	// nsPorts caches the discovered N/S NIC ports. Access via NSPorts().
	nsPorts []pciutil.NICPort

	// DiscoverPorts is the function used to discover NIC ports. If nil,
	// NewPortDiscoverer().DiscoverPorts is used.
	DiscoverPorts func() ([]pciutil.NICPort, error)

	// GrubConfigChanged is set by ConfigureKernelCmdLine when it writes a new grub config
	// and runs update-grub. HandleReboot uses this to force a reboot even when the reboot
	// method is NoAction, so that the new kernel parameters take effect.
	GrubConfigChanged bool

	// NICFirmwareRebootRequired is set by NICProvisioning when E/W NIC firmware
	// or configuration changes require a reboot before provisioning can continue.
	NICFirmwareRebootRequired bool

	// UpdateStatusUntilSuccess, when set, pushes Status to the API until success (e.g. agent's updateStatusUntilSuccess).
	// Used by operations that must ensure status is persisted before continuing (e.g. before shutdown).
	//
	// It may be invoked twice for the same execution: once from inside Execute
	// and once from Run() when ShouldUpdateStatusBeforeContinue is true. Calling it twice is safe: the same
	// status is pushed again, so the result is idempotent; at most it causes one redundant API call.
	// Returns an error when status update must abort (e.g. reprovision detection).
	UpdateStatusUntilSuccess func(context.Context) error
}

// DeferredNVConfigParam is one deferred mlxconfig set request for a PCI device.
type DeferredNVConfigParam struct {
	Device string
	Params string
}

// NSPorts returns the discovered N/S NIC ports, running discovery on the first
// successful call and caching the result. Errors are NOT cached so that the
// caller's retry loop will re-attempt discovery.
func (ctx *Context) NSPorts() ([]pciutil.NICPort, error) {
	if ctx.nsPorts != nil {
		return ctx.nsPorts, nil
	}
	discover := ctx.DiscoverPorts
	if discover == nil {
		discover = func() ([]pciutil.NICPort, error) {
			return pciutil.DefaultPortDiscoverer.DiscoverPhysicalPort(pciutil.NSPortFilter)
		}
	}
	ports, err := discover()
	if err != nil {
		return nil, err
	}
	ctx.nsPorts = ports
	return ports, nil
}
