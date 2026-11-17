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

package e2e

import (
	"strings"
	"testing"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// sizeTestDPU returns a Ready DPU with a managedFields entry and one condition whose message is
// conditionMessageLen characters long.
func sizeTestDPU(conditionMessageLen int) *provisioningv1.DPU {
	return &provisioningv1.DPU{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dpu-0",
			Namespace: "dpf-operator-system",
			ManagedFields: []metav1.ManagedFieldsEntry{
				{
					Manager: "manager",
					FieldsV1: &metav1.FieldsV1{
						Raw: []byte(`{"f:status":{}}`),
					},
				},
			},
		},
		Status: provisioningv1.DPUStatus{
			Phase: provisioningv1.DPUReady,
			Conditions: []metav1.Condition{
				{
					Type:    "Ready",
					Status:  metav1.ConditionTrue,
					Reason:  "Ready",
					Message: strings.Repeat("x", conditionMessageLen),
				},
			},
		},
	}
}

// TestMeasureDPUObjectExcludesManagedFields checks that managedFields do not count towards the DPU size.
func TestMeasureDPUObjectExcludesManagedFields(t *testing.T) {
	g := NewWithT(t)
	dpu := sizeTestDPU(0)

	withManagedFields, _, err := measureDPUObject(dpu)
	g.Expect(err).NotTo(HaveOccurred())
	dpu.ManagedFields = nil
	withoutManagedFields, _, err := measureDPUObject(dpu)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(withManagedFields).To(Equal(withoutManagedFields))
}

// TestMeasureDPUObjectPartsLargestFirst checks that the parts are sorted by size, largest first.
func TestMeasureDPUObjectPartsLargestFirst(t *testing.T) {
	g := NewWithT(t)

	_, parts, err := measureDPUObject(sizeTestDPU(1000))
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(parts).NotTo(BeEmpty())
	g.Expect(parts[0].name).To(Equal("status.conditions"))
	g.Expect(parts[0].size).To(BeNumerically(">", 1000))
	for i := 1; i < len(parts); i++ {
		g.Expect(parts[i].size).To(BeNumerically("<=", parts[i-1].size))
	}
}

// TestDPUOverBudgetErrorWithinBudget checks that a DPU exactly at the budget passes.
func TestDPUOverBudgetErrorWithinBudget(t *testing.T) {
	g := NewWithT(t)
	dpu := sizeTestDPU(0)
	size, parts, err := measureDPUObject(dpu)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(dpuOverBudgetError(dpu, size, parts, size)).To(Succeed())
}

// TestDPUOverBudgetErrorOverBudget checks that a DPU over the budget fails with its size, the budget
// and the size of each part.
func TestDPUOverBudgetErrorOverBudget(t *testing.T) {
	g := NewWithT(t)
	dpu := sizeTestDPU(2000)
	size, parts, err := measureDPUObject(dpu)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(size).To(BeNumerically(">", 2000))

	err = dpuOverBudgetError(dpu, size, parts, 1024)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("DPU dpf-operator-system/dpu-0"))
	g.Expect(err.Error()).To(ContainSubstring("over the 1024 byte budget"))
	g.Expect(err.Error()).To(ContainSubstring("status.conditions:"))
	g.Expect(err.Error()).To(ContainSubstring("status.phase:"))
}
