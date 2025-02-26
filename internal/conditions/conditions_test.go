/*
Copyright 2024 NVIDIA

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

package conditions

import (
	"fmt"
	"testing"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Mock object that implements the ObjectWithConditions and client.Object interfaces.
type MockObject struct {
	client.Object
	conditions []metav1.Condition
}

func (m *MockObject) GetConditions() []metav1.Condition {
	return m.conditions
}

func (m *MockObject) SetConditions(conds []metav1.Condition) {
	m.conditions = conds
}

// TestEnsureConditions tests the EnsureConditions function with table-driven tests
func TestEnsureConditions(t *testing.T) {
	g := NewWithT(t)
	tests := []struct {
		name               string
		obj                *MockObject
		allConditions      []ConditionType
		initialConditions  []metav1.Condition
		expectedConditions []metav1.Condition
	}{
		{
			name:          "Ensure unset conditions",
			obj:           &MockObject{},
			allConditions: []ConditionType{TypeReady, "ApplicationReady"},
			expectedConditions: []metav1.Condition{
				{
					Type:    string(TypeReady),
					Status:  metav1.ConditionUnknown,
					Reason:  string(ReasonPending),
					Message: "",
				},
				{
					Type:    "ApplicationReady",
					Status:  metav1.ConditionUnknown,
					Reason:  string(ReasonPending),
					Message: "",
				},
			},
		},
		{
			name:               "Ensure no conditions",
			obj:                &MockObject{},
			allConditions:      nil,
			expectedConditions: []metav1.Condition{},
		},
		{
			name: "Ensure conditions with existing Ready and ensure new ApplicationReady",
			obj: &MockObject{
				conditions: []metav1.Condition{
					{
						Type:    string(TypeReady),
						Status:  metav1.ConditionTrue,
						Reason:  string(ReasonSuccess),
						Message: "Already Ready",
					},
				},
			},
			allConditions: []ConditionType{TypeReady, "ApplicationReady"},
			expectedConditions: []metav1.Condition{
				{
					Type:    string(TypeReady),
					Status:  metav1.ConditionTrue,
					Reason:  string(ReasonSuccess),
					Message: "Already Ready",
				},
				{
					Type:    "ApplicationReady",
					Status:  metav1.ConditionUnknown,
					Reason:  string(ReasonPending),
					Message: "",
				},
			},
		},
		{
			name: "Do not overwrite status of existing conditions",
			obj: &MockObject{
				conditions: []metav1.Condition{
					{
						Type:    string(TypeReady),
						Status:  metav1.ConditionFalse,
						Reason:  string(ReasonFailure),
						Message: "Something failed",
					},
					{
						Type:    "ApplicationReady",
						Status:  metav1.ConditionTrue,
						Reason:  string(ReasonSuccess),
						Message: "Something is ready",
					},
				},
			},
			allConditions: []ConditionType{TypeReady, "ApplicationReady"},
			expectedConditions: []metav1.Condition{
				{
					Type:    string(TypeReady),
					Status:  metav1.ConditionFalse,
					Reason:  string(ReasonFailure),
					Message: "Something failed",
				},
				{
					Type:    "ApplicationReady",
					Status:  metav1.ConditionTrue,
					Reason:  string(ReasonSuccess),
					Message: "Something is ready",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.initialConditions) > 0 {
				tt.obj.SetConditions(tt.initialConditions)
			}
			EnsureConditions(tt.obj, tt.allConditions)

			conds := tt.obj.GetConditions()
			g.Expect(conds).To(HaveLen(len(tt.expectedConditions)))

			for _, expectedCond := range tt.expectedConditions {
				c := meta.FindStatusCondition(conds, expectedCond.Type)
				g.Expect(c).ToNot(BeNil())
				g.Expect(expectedCond.Status).To(Equal(c.Status))
				g.Expect(expectedCond.Reason).To(Equal(c.Reason))
				g.Expect(expectedCond.Message).To(Equal(c.Message))
			}
		})
	}
}

func TestAddTrueAndFalse(t *testing.T) {
	g := NewWithT(t)
	tests := []struct {
		name               string
		obj                *MockObject
		conditionType      ConditionType
		conditionReason    ConditionReason
		conditionMessage   ConditionMessage
		expectedConditions []metav1.Condition
		addTrue            bool
	}{
		{
			name:          "Add true condition for Ready",
			obj:           &MockObject{},
			conditionType: TypeReady,
			addTrue:       true,
			expectedConditions: []metav1.Condition{
				{
					Type:   string(TypeReady),
					Status: metav1.ConditionTrue,
					Reason: string(ReasonSuccess),
				},
			},
		},
		{
			name: "Change condition to True",
			obj: &MockObject{
				conditions: []metav1.Condition{
					{
						Type:    string(TypeReady),
						Status:  metav1.ConditionFalse,
						Reason:  string(ReasonPending),
						Message: "",
					},
				},
			},
			conditionType: TypeReady,
			addTrue:       true,
			expectedConditions: []metav1.Condition{
				{
					Type:   string(TypeReady),
					Status: metav1.ConditionTrue,
					Reason: string(ReasonSuccess),
				},
			},
		},
		{
			name:             "Add false condition for Ready",
			obj:              &MockObject{},
			conditionType:    TypeReady,
			conditionReason:  ReasonFailure,
			conditionMessage: "Something is broken",
			expectedConditions: []metav1.Condition{
				{
					Type:    string(TypeReady),
					Status:  metav1.ConditionFalse,
					Reason:  string(ReasonFailure),
					Message: "Something is broken",
				},
			},
		},
		{
			name: "Change condition to False",
			obj: &MockObject{
				conditions: []metav1.Condition{
					{
						Type:   string(TypeReady),
						Status: metav1.ConditionTrue,
						Reason: string(ReasonSuccess),
					},
				},
			},
			conditionType:    TypeReady,
			conditionReason:  ReasonFailure,
			conditionMessage: "Something is broken",
			expectedConditions: []metav1.Condition{
				{
					Type:    string(TypeReady),
					Status:  metav1.ConditionFalse,
					Reason:  string(ReasonFailure),
					Message: "Something is broken",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.addTrue {
				AddTrue(tt.obj, tt.conditionType)
			} else {
				AddFalse(tt.obj, tt.conditionType, tt.conditionReason, tt.conditionMessage)
			}

			conds := tt.obj.GetConditions()
			g.Expect(conds).To(HaveLen(len(tt.expectedConditions)))

			for _, expectedCond := range tt.expectedConditions {
				c := meta.FindStatusCondition(conds, expectedCond.Type)
				g.Expect(c).ToNot(BeNil())
				g.Expect(expectedCond.Status).To(Equal(c.Status))
				g.Expect(expectedCond.Reason).To(Equal(c.Reason))
				g.Expect(expectedCond.Message).To(Equal(c.Message))
			}
		})
	}
}

// TestSetSummary tests the SetSummary function with table-driven tests
func TestSetSummary(t *testing.T) {
	g := NewWithT(t)
	tests := []struct {
		name              string
		initialConditions []metav1.Condition
		expectedCondition metav1.Condition
	}{
		{
			name: "All conditions are ready",
			initialConditions: []metav1.Condition{
				{
					Type:    "ApplicationsReady",
					Status:  metav1.ConditionTrue,
					Reason:  string(ReasonSuccess),
					Message: "",
				},
				{
					Type:    "ApplicationPrereqsReconciled",
					Status:  metav1.ConditionTrue,
					Reason:  string(ReasonSuccess),
					Message: "",
				},
			},
			expectedCondition: metav1.Condition{
				Type:   string(TypeReady),
				Status: metav1.ConditionTrue,
				Reason: string(ReasonSuccess),
			},
		},
		{
			name: "One condition is not ready",
			initialConditions: []metav1.Condition{
				{
					Type:    "ApplicationsReady",
					Status:  metav1.ConditionFalse,
					Reason:  string(ReasonPending),
					Message: "",
				},
				{
					Type:    "ApplicationPrereqsReconciled",
					Status:  metav1.ConditionTrue,
					Reason:  string(ReasonSuccess),
					Message: "",
				},
			},
			expectedCondition: metav1.Condition{
				Type:    string(TypeReady),
				Status:  metav1.ConditionFalse,
				Reason:  string(ReasonPending),
				Message: fmt.Sprintf(MessageNotReadyTemplate, "ApplicationsReady"),
			},
		},
		{
			name: "One condition is failed",
			initialConditions: []metav1.Condition{
				{
					Type:    "ApplicationsReady",
					Status:  metav1.ConditionFalse,
					Reason:  string(ReasonFailure),
					Message: "",
				},
				{
					Type:    "ApplicationPrereqsReconciled",
					Status:  metav1.ConditionTrue,
					Reason:  string(ReasonSuccess),
					Message: "",
				},
			},
			expectedCondition: metav1.Condition{
				Type:    string(TypeReady),
				Status:  metav1.ConditionFalse,
				Reason:  string(ReasonFailure),
				Message: fmt.Sprintf(MessageNotReadyTemplate, "ApplicationsReady"),
			},
		},
		{
			name: "One condition is errored",
			initialConditions: []metav1.Condition{
				{
					Type:    "ApplicationsReady",
					Status:  metav1.ConditionFalse,
					Reason:  string(ReasonError),
					Message: "",
				},
				{
					Type:    "ApplicationPrereqsReconciled",
					Status:  metav1.ConditionTrue,
					Reason:  string(ReasonSuccess),
					Message: "",
				},
			},
			expectedCondition: metav1.Condition{
				Type:    string(TypeReady),
				Status:  metav1.ConditionFalse,
				Reason:  string(ReasonPending),
				Message: fmt.Sprintf(MessageNotReadyTemplate, "ApplicationsReady"),
			},
		},
		{
			name: "One condition is errored, one has a failure",
			initialConditions: []metav1.Condition{
				{
					Type:    "ApplicationsReady",
					Status:  metav1.ConditionFalse,
					Reason:  string(ReasonError),
					Message: "",
				},
				{
					Type:    "SomethingReady",
					Status:  metav1.ConditionFalse,
					Reason:  string(ReasonFailure),
					Message: "",
				},
			},
			expectedCondition: metav1.Condition{
				Type:    string(TypeReady),
				Status:  metav1.ConditionFalse,
				Reason:  string(ReasonFailure),
				Message: fmt.Sprintf(MessageNotReadyTemplate, "ApplicationsReady, SomethingReady"),
			},
		},
		{
			name: "One condition is awaiting deletion",
			initialConditions: []metav1.Condition{
				{
					Type:    "ApplicationsReady",
					Status:  metav1.ConditionFalse,
					Reason:  string(ReasonAwaitingDeletion),
					Message: "",
				},
				{
					Type:    "ApplicationPrereqsReconciled",
					Status:  metav1.ConditionTrue,
					Reason:  string(ReasonSuccess),
					Message: "",
				},
			},
			expectedCondition: metav1.Condition{
				Type:    string(TypeReady),
				Status:  metav1.ConditionFalse,
				Reason:  string(ReasonAwaitingDeletion),
				Message: fmt.Sprintf(MessageNotReadyTemplate, "ApplicationsReady"),
			},
		},
		{
			name: "One condition is awaiting deletion, one is errored",
			initialConditions: []metav1.Condition{
				{
					Type:    "ApplicationsReady",
					Status:  metav1.ConditionFalse,
					Reason:  string(ReasonAwaitingDeletion),
					Message: "",
				},
				{
					Type:    "ApplicationPrereqsReconciled",
					Status:  metav1.ConditionFalse,
					Reason:  string(ReasonError),
					Message: "",
				},
			},
			expectedCondition: metav1.Condition{
				Type:    string(TypeReady),
				Status:  metav1.ConditionFalse,
				Reason:  string(ReasonAwaitingDeletion),
				Message: fmt.Sprintf(MessageNotReadyTemplate, "ApplicationsReady, ApplicationPrereqsReconciled"),
			},
		},
		{
			name: "One condition is awaiting deletion, one has a failure",
			initialConditions: []metav1.Condition{
				{
					Type:    "ApplicationsReady",
					Status:  metav1.ConditionFalse,
					Reason:  string(ReasonAwaitingDeletion),
					Message: "",
				},
				{
					Type:    "SomethingReady",
					Status:  metav1.ConditionFalse,
					Reason:  string(ReasonFailure),
					Message: "",
				},
			},
			expectedCondition: metav1.Condition{
				Type:    string(TypeReady),
				Status:  metav1.ConditionFalse,
				Reason:  string(ReasonAwaitingDeletion),
				Message: fmt.Sprintf(MessageNotReadyTemplate, "ApplicationsReady, SomethingReady"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := &MockObject{}
			obj.SetConditions(tt.initialConditions)
			SetSummary(obj)

			readyCondition := Get(obj, TypeReady)
			g.Expect(readyCondition).NotTo(BeNil())
			g.Expect(tt.expectedCondition.Status).To(Equal(readyCondition.Status))
			g.Expect(tt.expectedCondition.Reason).To(Equal(readyCondition.Reason))
			g.Expect(tt.expectedCondition.Message).To(Equal(readyCondition.Message))
		})
	}
}

func TestGet(t *testing.T) {
	g := NewWithT(t)
	tests := []struct {
		name              string
		obj               *MockObject
		conditionType     ConditionType
		initialConditions []metav1.Condition
		expectedCondition *metav1.Condition
	}{
		{
			name:          "Get existing condition",
			obj:           &MockObject{},
			conditionType: TypeReady,
			initialConditions: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Success", Message: "Reconciliation successful"},
				{Type: "ApplicationsReady", Status: metav1.ConditionTrue, Reason: "Success", Message: "Reconciliation successful"},
			},
			expectedCondition: &metav1.Condition{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Success", Message: "Reconciliation successful"},
		},
		{
			name:              "Get non-existing condition",
			obj:               &MockObject{},
			conditionType:     "NonExistent",
			expectedCondition: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.initialConditions) > 0 {
				tt.obj.SetConditions(tt.initialConditions)
			}

			cond := Get(tt.obj, tt.conditionType)
			g.Expect(cond).To(BeComparableTo(tt.expectedCondition))
		})
	}
}

func Test_highestSeverityReason(t *testing.T) {
	tests := []struct {
		name   string
		first  ConditionReason
		second ConditionReason
		want   ConditionReason
	}{
		{
			name:   "Pending is the default when both reasons are unknown",
			first:  ConditionReason(""),
			second: ConditionReason("ReasonIneffable"),
			want:   ReasonPending,
		},
		{
			name:   "ReasonPending has higher severity than an unknown reason",
			first:  ConditionReason(""),
			second: ReasonPending,
			want:   ReasonPending,
		},
		{
			name:   "AwaitingDeletion has higher severity than an unknown reason",
			first:  ConditionReason("ReasonIneffable"),
			second: ReasonAwaitingDeletion,
			want:   ReasonAwaitingDeletion,
		},
		{
			name:   "AwaitingDeletion has higher severity than Failure",
			first:  ReasonFailure,
			second: ReasonAwaitingDeletion,
			want:   ReasonAwaitingDeletion,
		},
		{
			name:   "AwaitingDeletion has higher severity than Pending",
			first:  ReasonPending,
			second: ReasonAwaitingDeletion,
			want:   ReasonAwaitingDeletion,
		},
		{
			name:   "Failure has higher severity than Pending",
			first:  ReasonPending,
			second: ReasonFailure,
			want:   ReasonFailure,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := highestSeverityReason(tt.first, tt.second); got != tt.want {
				t.Errorf("highestSeverityReason() = %v, want %v", got, tt.want)
			}
		})
	}
}
