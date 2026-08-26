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
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/future"
	"github.com/nvidia/doca-platform/internal/provisioning/controllers/util/reboot"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	MaxRetryCount = 10
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
	// KubernetesAPIServerVIP is the Kubernetes API server VIP configured for the DMS/hostagent
	// Pod (from DPFOperatorConfig KubernetesAPIServerVIP, passed via --dms-pod-envs). It is added
	// to the bfb-registry server certificate SANs so the hostagent's VIP-based NodePort download
	// passes TLS verification. Empty when no VIP override is configured.
	KubernetesAPIServerVIP string
	// OSInstallTimeout is the maximum time allowed for OS installation in zero-trust mode.
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
	HostUptimeChecker    reboot.HostUptimeChecker
	DPUInProvisioningMap *DPUInProvisioningMap
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
