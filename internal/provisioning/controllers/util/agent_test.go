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

package util

import (
	"context"
	"testing"
	"time"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func testAgentReportedTime() *metav1.Time {
	t := metav1.Now()
	return &t
}

func TestPreInstallAgentReported(t *testing.T) {
	t.Parallel()
	dpu := &provisioningv1.DPU{
		Status: provisioningv1.DPUStatus{
			AgentStatus: &provisioningv1.AgentStatus{
				PreInstall: &provisioningv1.AgentPreInstallStatus{
					AgentReported: testAgentReportedTime(),
				},
			},
		},
	}
	require.True(t, PreInstallAgentReported(dpu))
	require.False(t, PreInstallAgentReported(&provisioningv1.DPU{}))
}

func TestPreInstallNVConfigReported(t *testing.T) {
	t.Parallel()
	dpu := &provisioningv1.DPU{
		Status: provisioningv1.DPUStatus{
			AgentStatus: &provisioningv1.AgentStatus{
				PreInstall: &provisioningv1.AgentPreInstallStatus{
					Conditions: []metav1.Condition{{
						Type:   provisioningv1.DPUAgentConditionNVConfigApplied,
						Status: metav1.ConditionFalse,
					}},
				},
			},
		},
	}
	require.True(t, PreInstallNVConfigReported(dpu))
	require.False(t, PreInstallNVConfigReported(&provisioningv1.DPU{}))
}

func TestWaitPreInstallAgentRegistrationOrProceed(t *testing.T) {
	t.Parallel()
	dpu := &provisioningv1.DPU{ObjectMeta: metav1.ObjectMeta{Generation: 1}}
	state := &provisioningv1.DPUStatus{}

	state, proceed := WaitPreInstallAgentRegistrationOrProceed(context.Background(), dpu, state, time.Second)
	require.False(t, proceed)
	cond := meta.FindStatusCondition(state.Conditions, provisioningv1.DPUCondInitialized.String())
	require.NotNil(t, cond)
	require.Equal(t, provisioningv1.DPUCondInitialized.String(), cond.Reason)
	require.Empty(t, cond.Message)

	dpu.Status.AgentStatus = &provisioningv1.AgentStatus{
		PreInstall: &provisioningv1.AgentPreInstallStatus{AgentReported: testAgentReportedTime()},
	}
	_, proceed = WaitPreInstallAgentRegistrationOrProceed(context.Background(), dpu, state, time.Second)
	require.True(t, proceed)

	state = &provisioningv1.DPUStatus{
		Conditions: []metav1.Condition{{
			Type:               provisioningv1.DPUCondInitialized.String(),
			Status:             metav1.ConditionFalse,
			Reason:             provisioningv1.DPUCondInitialized.String(),
			LastTransitionTime: metav1.NewTime(time.Now().Add(-2 * time.Minute)),
		}},
	}
	dpu.Status.AgentStatus = nil
	state, proceed = WaitPreInstallAgentRegistrationOrProceed(context.Background(), dpu, state, time.Second)
	require.True(t, proceed)
	cond = meta.FindStatusCondition(state.Conditions, provisioningv1.DPUCondInitialized.String())
	require.Equal(t, provisioningv1.DPUCondInitialized.String(), cond.Reason)
}
