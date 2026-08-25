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
	"errors"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ErrRebindInProgress is returned when a host driver rebind is already running for the DPU UID.
var ErrRebindInProgress = errors.New("host driver rebind already in progress")

type UpdateStatusRequest struct {
	DPUName      string                     `json:"dpuName"`
	DPUNamespace string                     `json:"dpuNamespace"`
	DPUUID       string                     `json:"dpuUID,omitempty"`
	AgentStatus  provisioningv1.AgentStatus `json:"agentStatus"`
}

// ReportClockRequest carries the DPU clock reading to the host. The DPU agent cannot judge its own
// clock or write to the API server before its client certificate is issued, which is exactly what a
// skewed clock prevents, so the host compares and records the result on its behalf.
type ReportClockRequest struct {
	DPUName      string      `json:"dpuName"`
	DPUNamespace string      `json:"dpuNamespace"`
	DPUUID       string      `json:"dpuUID,omitempty"`
	DPUTime      metav1.Time `json:"dpuTime"`
}

// ReportClockResponse hands the host clock back to the DPU agent. The DPU has no maintained time
// source of its own: its RTC only advances while the card is powered, nothing resynchronizes it,
// and at report time the DPU has neither a route nor a resolver to reach NTP. The host is already
// the authority that judges the skew and writes it to the DPU object, so returning the reading it
// just took adds no trust the agent does not already place in the host.
type ReportClockResponse struct {
	HostTime metav1.Time `json:"hostTime"`
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

type RebindHostDriverRequest struct {
	DPUName      string `json:"dpuName"`
	DPUNamespace string `json:"dpuNamespace"`
	DPUUID       string `json:"dpuUID"`
}

type SetErrorRequest struct {
	DPUName      string `json:"dpuName"`
	DPUNamespace string `json:"dpuNamespace"`
	DPUUID       string `json:"dpuUID"`
	Reason       string `json:"reason"`
	Message      string `json:"message"`
}
