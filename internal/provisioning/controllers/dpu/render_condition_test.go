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
	cutil "github.com/nvidia/doca-platform/internal/provisioning/controllers/util"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func findCond(state provisioningv1.DPUStatus, t provisioningv1.DPUConditionType) *metav1.Condition {
	for i := range state.Conditions {
		if state.Conditions[i].Type == t.String() {
			return &state.Conditions[i]
		}
	}
	return nil
}

func TestSetDPUFlavorRenderedCondition(t *testing.T) {
	rendered := provisioningv1.DPUCondDPUFlavorRendered

	t.Run("ignores non-template DPUs", func(t *testing.T) {
		g := NewWithT(t)
		dpu := &provisioningv1.DPU{ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{cutil.RenderFailedReasonAnnotation: cutil.RenderFailedOnUpdate},
		}}
		state := provisioningv1.DPUStatus{}
		setDPUFlavorRenderedCondition(dpu, &state)
		g.Expect(findCond(state, rendered)).To(BeNil())
	})

	t.Run("sets False with the failure reason when annotated", func(t *testing.T) {
		g := NewWithT(t)
		dpu := &provisioningv1.DPU{ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{cutil.DPUFlavorTemplateNameLabel: "tmpl"},
			Annotations: map[string]string{
				cutil.RenderFailedReasonAnnotation:  cutil.RenderFailedOnUpdate,
				cutil.RenderFailedMessageAnnotation: "boom",
			},
		}}
		state := provisioningv1.DPUStatus{}
		setDPUFlavorRenderedCondition(dpu, &state)
		cond := findCond(state, rendered)
		g.Expect(cond).NotTo(BeNil())
		g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		g.Expect(cond.Reason).To(Equal(cutil.RenderFailedOnUpdate))
	})

	t.Run("sets True for a healthy template DPU (self-heal)", func(t *testing.T) {
		g := NewWithT(t)
		dpu := &provisioningv1.DPU{ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{cutil.DPUFlavorTemplateNameLabel: "tmpl"},
		}}
		state := provisioningv1.DPUStatus{}
		setDPUFlavorRenderedCondition(dpu, &state)
		cond := findCond(state, rendered)
		g.Expect(cond).NotTo(BeNil())
		g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
	})
}
