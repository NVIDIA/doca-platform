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
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Condition", func() {
	Context("NewCondition", Label("NewCondition"), func() {
		It("should create a new ConditionBuilder with the correct type", func() {
			builder := NewCondition("TestCondition")
			Expect(builder).NotTo(BeNil())
			Expect(builder.condType).To(Equal("TestCondition"))
		})
	})

	Context("ConditionBuilder.Success", Label("ConditionBuilder", "Success"), func() {
		It("should create a success condition with correct fields", func() {
			builder := NewCondition("Ready")
			setter := builder.Success("Operation completed")

			Expect(setter).NotTo(BeNil())
			Expect(setter.condition.Type).To(Equal("Ready"))
			Expect(setter.condition.Status).To(Equal(metav1.ConditionTrue))
			Expect(setter.condition.Reason).To(Equal("Ready"))
			Expect(setter.condition.Message).To(Equal("Operation completed"))
		})

		It("should create a success condition with empty message", func() {
			builder := NewCondition("Ready")
			setter := builder.Success("")

			Expect(setter.condition.Message).To(Equal(""))
		})
	})

	Context("ConditionBuilder.Failure", Label("ConditionBuilder", "Failure"), func() {
		It("should create a failure condition with correct fields", func() {
			builder := NewCondition("Ready")
			testErr := errors.New("something went wrong")
			setter := builder.Failure(testErr, "OperationFailed")

			Expect(setter).NotTo(BeNil())
			Expect(setter.condition.Type).To(Equal("Ready"))
			Expect(setter.condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(setter.condition.Reason).To(Equal("OperationFailed"))
			Expect(setter.condition.Message).To(Equal("something went wrong"))
		})
	})

	Context("ConditionSetter.Set", Label("ConditionSetter", "Set"), func() {
		It("should set a new condition on empty conditions slice", func() {
			var conditions []metav1.Condition
			builder := NewCondition("Ready")
			setter := builder.Success("All good")

			setter.Set(&conditions)

			Expect(conditions).To(HaveLen(1))
			Expect(conditions[0].Type).To(Equal("Ready"))
			Expect(conditions[0].Status).To(Equal(metav1.ConditionTrue))
		})

		It("should update an existing condition", func() {
			conditions := []metav1.Condition{
				{
					Type:    "Ready",
					Status:  metav1.ConditionFalse,
					Reason:  "NotReady",
					Message: "Initial state",
				},
			}
			builder := NewCondition("Ready")
			setter := builder.Success("Now ready")

			setter.Set(&conditions)

			Expect(conditions).To(HaveLen(1))
			Expect(conditions[0].Type).To(Equal("Ready"))
			Expect(conditions[0].Status).To(Equal(metav1.ConditionTrue))
			Expect(conditions[0].Message).To(Equal("Now ready"))
		})

		It("should add a new condition without affecting existing ones", func() {
			conditions := []metav1.Condition{
				{
					Type:    "Initialized",
					Status:  metav1.ConditionTrue,
					Reason:  "Initialized",
					Message: "Initialized",
				},
			}
			builder := NewCondition("Ready")
			setter := builder.Success("Ready now")

			setter.Set(&conditions)

			Expect(conditions).To(HaveLen(2))
			var foundInitialized, foundReady bool
			for _, c := range conditions {
				if c.Type == "Initialized" {
					foundInitialized = true
					Expect(c.Status).To(Equal(metav1.ConditionTrue))
				}
				if c.Type == "Ready" {
					foundReady = true
					Expect(c.Status).To(Equal(metav1.ConditionTrue))
				}
			}
			Expect(foundInitialized).To(BeTrue())
			Expect(foundReady).To(BeTrue())
		})
	})
})
