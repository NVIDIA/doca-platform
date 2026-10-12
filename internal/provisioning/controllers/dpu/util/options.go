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

package util

import (
	"sync"
	"time"

	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/allocator"
	rfclient "github.com/nvidia/doca-platform/internal/provisioning/controllers/dpu/state/redfish/client"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/future"
	"github.com/nvidia/doca-platform/pkg/certmanager"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	MaxRetryCount = 10
	// DefaultDPUMaxConcurrentReconciles is the default number of concurrent DPU reconcile workers.
	DefaultDPUMaxConcurrentReconciles int32 = 1
	// DefaultOSInstallRetries is the default maximum number of retryable OS
	// installation attempts in zero-trust mode before transitioning to Error.
	// Used when DPFOperatorConfig.spec.provisioningController.osInstallRetries is unset.
	DefaultOSInstallRetries int32 = 2
)

var BmcFwUpdateTaskMap sync.Map
var RebootTaskMap sync.Map
var HostNetworkTaskMap sync.Map

type TaskWithRetry struct {
	Task       *future.Future
	RetryCount int
}

type DPUOptions struct {
	PrarprouterdImageWithTag    string
	ImagePullSecrets            []corev1.LocalObjectReference
	DPUInstallInterface         string
	DeploymentMode              string
	BFCFGTemplateFile           string
	BFBRegistry                 string
	BFBPVC                      string
	BFBRegistryLoadBalancer     string
	CustomCASecretName          string
	MaxDPUParallelInstallations int32
	DPUMaxConcurrentReconciles  int32
	// KubernetesAPIServerVIP is the Kubernetes API server VIP configured for the DMS/hostagent
	// Pod (from DPFOperatorConfig KubernetesAPIServerVIP, passed via --dms-pod-envs). It is added
	// to the bfb-registry server certificate SANs so the hostagent's VIP-based NodePort download
	// passes TLS verification. Empty when no VIP override is configured.
	KubernetesAPIServerVIP string
	// CAIssuerRef is the cert-manager issuer that signs the bfb-registry server certificate when
	// the DPU reconciler is the one ensuring it, set from the controller's --ca-issuer-* flags.
	CAIssuerRef certmanager.IssuerReference
	// CATrustBundleConfigMapName is the ConfigMap the dpu-agent syncs the DPU OS trust store from,
	// set from the controller's --ca-trust-bundle-configmap-name flag. The per-DPU Role has to
	// grant it by name, otherwise the agent is denied the bundle it was told to read.
	CATrustBundleConfigMapName string
	// OSInstallTimeout is the maximum time allowed for OS installation.
	OSInstallTimeout time.Duration
	// OSInstallRetries is the maximum number of retryable OS installation attempts in
	// zero-trust mode before transitioning to Error. Defaults to DefaultOSInstallRetries.
	OSInstallRetries int32
	// FirmwareUpdateTimeout is the maximum time allowed for firmware update in zero-trust mode.
	FirmwareUpdateTimeout time.Duration
	// PreInstallAgentRegistrationTimeout is how long Initializing waits for preInstall.agentReported.
	// Set via --pre-install-agent-registration-timeout.
	PreInstallAgentRegistrationTimeout time.Duration
	// NodeEffectRemovalTimeout is the maximum time allowed for the Node Effect Removal phase.
	NodeEffectRemovalTimeout time.Duration
}

type ControllerContext struct {
	client.Client
	Scheme               *runtime.Scheme
	Options              DPUOptions
	Recorder             record.EventRecorder
	ClusterAllocator     allocator.Allocator
	JoinCommandGenerator NodeJoinCommandGenerator
	DPUArtifactGenerator DPUArtifactGenerator
	DPUInProvisioningMap *DPUInProvisioningMap
	DPFOperatorConfig    *operatorv1.DPFOperatorConfig
	// RedfishClients caches the verified mTLS BMC clients. It is owned by provisioning-manager
	// startup; state handlers never close the clients they get from it. Read it through
	// RedfishProvider, not directly.
	RedfishClients rfclient.ClientProvider
}

// RedfishProvider returns the provider state handlers get BMC clients from.
//
// NewDPUReconciler rejects a nil provider, so in the provisioning manager this always returns the
// process-wide one. The fallback covers tests that build a ControllerContext directly and reach a
// handler that talks to a BMC: they get an unshared provider rather than a nil dereference.
//
// The DPUDevice controller deliberately has no such fallback: it reads the field directly, so an
// unwired one fails immediately instead of quietly losing client reuse.
func (c *ControllerContext) RedfishProvider() rfclient.ClientProvider {
	if c.RedfishClients != nil {
		return c.RedfishClients
	}
	return rfclient.NewProvider(c.Client)
}

// ZeroTrustProvisioningFlow reports whether the cluster policy is zero-trust for provisioning
// phases that branch on ZT vs host-trusted (e.g. reboot completion, RebootMethodNoAction).
// When DeploymentMode is unset (legacy), Redfish install interface implies zero-trust flow.
func (o DPUOptions) ZeroTrustProvisioningFlow() bool {
	if o.DeploymentMode != "" {
		return o.DeploymentMode == string(operatorv1.DeploymentModeZeroTrust)
	}
	return o.DPUInstallInterface == string(provisioningv1.InstallViaRedFish)
}
