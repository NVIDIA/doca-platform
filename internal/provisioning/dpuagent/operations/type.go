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
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type OperationType int

const (
	RunOnce OperationType = iota
	RunOnEachReboot
)

type Operation interface {
	Name() string
	Type() OperationType
	Execute(ctx Context) error
}

type Context struct {
	Client        client.Client
	DPUFlavor     provisioningv1.DPUFlavor
	InstallConfig InstallConfig
}

type InstallMode string

const (
	ZeroTrustMode   InstallMode = "zero-trust"
	TrustedHostMode InstallMode = "trusted-host"
)

type InstallConfig struct {
	Mode           InstallMode            `json:"mode"`
	KubeadmJoinCmd string                 `json:"kubeadmJoinCmd"`
	DPU            provisioningv1.DPU     `json:"dpu"`
	DPUNode        provisioningv1.DPUNode `json:"dpuNode"`
	// ControlPlaneMTU is populated by the provisioning controller using the value from the DPFOperatorConfig.
	ControlPlaneMTU int32 `json:"controlPlaneMTU"`
}
