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

package types

import (
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
)

type UpdateStatusRequest struct {
	DPUName      string                     `json:"dpuName"`
	DPUNamespace string                     `json:"dpuNamespace"`
	DPUUID       string                     `json:"dpuUID,omitempty"`
	AgentStatus  provisioningv1.AgentStatus `json:"agentStatus"`
}

type ConfigureHostVFsRequest struct {
	DPUName      string `json:"dpuName"`
	DPUNamespace string `json:"dpuNamespace"`
	VFCount      *int   `json:"vfCount"`
}

type TriggerRebootRequest struct {
	DPUName      string                          `json:"dpuName"`
	DPUNamespace string                          `json:"dpuNamespace"`
	DPUUID       string                          `json:"dpuUID"`
	RebootMethod provisioningv1.RebootMethodType `json:"rebootMethod"`
}
