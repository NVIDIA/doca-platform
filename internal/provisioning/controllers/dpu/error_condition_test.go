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

package dpu

import (
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestSetErrorPhaseFromCondition(t *testing.T) {
	errorCondition := func(status metav1.ConditionStatus) metav1.Condition {
		return metav1.Condition{
			Type:               provisioningv1.DPUCondError.String(),
			Status:             status,
			Reason:             "FatalFailure",
			Message:            "something went wrong on the DPU",
			LastTransitionTime: metav1.Now(),
		}
	}

	t.Run("keeps the phase when the condition is absent", func(t *testing.T) {
		g := NewWithT(t)
		dpu := &provisioningv1.DPU{}
		state := provisioningv1.DPUStatus{Phase: provisioningv1.DPUOSInstalling}
		setErrorPhaseFromCondition(dpu, &state)
		g.Expect(state.Phase).To(Equal(provisioningv1.DPUOSInstalling))
	})

	t.Run("keeps the phase when the condition is False", func(t *testing.T) {
		g := NewWithT(t)
		dpu := &provisioningv1.DPU{}
		state := provisioningv1.DPUStatus{
			Phase:      provisioningv1.DPUOSInstalling,
			Conditions: []metav1.Condition{errorCondition(metav1.ConditionFalse)},
		}
		setErrorPhaseFromCondition(dpu, &state)
		g.Expect(state.Phase).To(Equal(provisioningv1.DPUOSInstalling))
	})

	t.Run("moves to the Error phase when the condition is True", func(t *testing.T) {
		g := NewWithT(t)
		dpu := &provisioningv1.DPU{}
		state := provisioningv1.DPUStatus{
			Phase:      provisioningv1.DPUOSInstalling,
			Conditions: []metav1.Condition{errorCondition(metav1.ConditionTrue)},
		}
		setErrorPhaseFromCondition(dpu, &state)
		g.Expect(state.Phase).To(Equal(provisioningv1.DPUError))
	})

	t.Run("leaves a deleting DPU alone", func(t *testing.T) {
		g := NewWithT(t)
		dpu := &provisioningv1.DPU{ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: ptr.To(metav1.Now())}}
		state := provisioningv1.DPUStatus{
			Phase:      provisioningv1.DPUDeleting,
			Conditions: []metav1.Condition{errorCondition(metav1.ConditionTrue)},
		}
		setErrorPhaseFromCondition(dpu, &state)
		g.Expect(state.Phase).To(Equal(provisioningv1.DPUDeleting))
	})
}
