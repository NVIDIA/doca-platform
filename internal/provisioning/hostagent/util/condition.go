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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConditionSetter is a struct that sets a condition on a list of conditions.
// todo: move the setter and builder to controller util and substitute the existing condition functions
type ConditionSetter struct {
	condition metav1.Condition
}

func (s *ConditionSetter) Set(conditions *[]metav1.Condition) {
	meta.SetStatusCondition(conditions, s.condition)
}

type ConditionBuilder struct {
	condType string
}

func NewCondition(condType string) *ConditionBuilder {
	return &ConditionBuilder{
		condType: condType,
	}
}

func (b *ConditionBuilder) Success(message string) *ConditionSetter {
	return &ConditionSetter{
		condition: metav1.Condition{
			Type:    b.condType,
			Status:  metav1.ConditionTrue,
			Reason:  b.condType,
			Message: message,
		},
	}
}

func (b *ConditionBuilder) Failure(err error, reason string) *ConditionSetter {
	return &ConditionSetter{
		condition: metav1.Condition{
			Type:    b.condType,
			Status:  metav1.ConditionFalse,
			Reason:  reason,
			Message: err.Error(),
		},
	}
}
