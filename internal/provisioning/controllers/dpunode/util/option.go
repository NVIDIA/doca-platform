/*
Copyright 2025 NVIDIA

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
	"time"

	corev1 "k8s.io/api/core/v1"
)

// HostAgentPodOptions represents the options used to configure a DMS Pod.
type HostAgentPodOptions struct {
	// HostAgentImageWithTag is the image reference with tag for the DMS container.
	HostAgentImageWithTag string
	// ImagePullSecrets is a list of secrets that will be used to authenticate the image pull request.
	ImagePullSecrets []corev1.LocalObjectReference
	// DMSTimeout is the max time in seconds within which a DMS API must respond, 0 is unlimited
	DMSTimeout int
	// DMSPodTimeout is the timeout duration for the DMS Pod to become Ready.
	DMSPodTimeout time.Duration
	// DMSPodEnvs is the environment variables to set in the DMS Pod.
	DMSPodEnvs []string
	// MultiDPUOperationsSyncWaitTime is the wait time between DPUs sync operations on the same node.
	MultiDPUOperationsSyncWaitTime time.Duration
	// MaxUnavailableDPUNodes is the maximum number of DPUNodes that are unavailable during the node effect period.
	MaxUnavailableDPUNodes int32
	// BFBRegistryAddress is the address of the BFB registry.
	BFBRegistryAddress string
}
