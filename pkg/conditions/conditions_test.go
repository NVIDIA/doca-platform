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
	"errors"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Mock object that implements the ObjectWithConditions and client.Object interfaces.
type MockObject struct {
	client.Object
	conditions []metav1.Condition
	generation int64
}

func (m *MockObject) GetConditions() []metav1.Condition {
	return m.conditions
}

func (m *MockObject) SetConditions(conds []metav1.Condition) {
	m.conditions = conds
}

func (m *MockObject) GetGeneration() int64 {
	return m.generation
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
		obj               *MockObject
		initialConditions []metav1.Condition
		expectedCondition metav1.Condition
	}{
		{
			name: "All conditions are ready",
			obj:  &MockObject{},
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
			obj:  &MockObject{},
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
				Message: ReadyConditionMessage(MessageNotReady, []string{"ApplicationsReady"}),
			},
		},
		{
			name: "One condition is failed",
			obj:  &MockObject{},
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
				Message: ReadyConditionMessage(MessageNotReady, []string{"ApplicationsReady"}),
			},
		},
		{
			name: "One condition is errored",
			obj:  &MockObject{},
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
				Message: ReadyConditionMessage(MessageNotReady, []string{"ApplicationsReady"}),
			},
		},
		{
			name: "One condition is errored, one has a failure",
			obj:  &MockObject{},
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
				Message: ReadyConditionMessage(MessageNotReady, []string{"ApplicationsReady", "SomethingReady"}),
			},
		},
		{
			name: "One condition is awaiting deletion",
			obj:  &MockObject{},
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
				Message: ReadyConditionMessage(MessageNotReady, []string{"ApplicationsReady"}),
			},
		},
		{
			name: "One condition is awaiting deletion, one is errored",
			obj:  &MockObject{},
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
				Message: ReadyConditionMessage(MessageNotReady, []string{"ApplicationsReady", "ApplicationPrereqsReconciled"}),
			},
		},
		{
			name: "One condition is awaiting deletion, one has a failure",
			obj:  &MockObject{},
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
				Message: ReadyConditionMessage(MessageNotReady, []string{"ApplicationsReady", "SomethingReady"}),
			},
		},
		{
			name: "All conditions are stale (observed generation < current generation)",
			obj:  &MockObject{generation: 2},
			initialConditions: []metav1.Condition{
				{
					Type:               "ApplicationsReady",
					Status:             metav1.ConditionFalse,
					Reason:             string(ReasonPending),
					Message:            "",
					ObservedGeneration: 1,
				},
				{
					Type:               "ApplicationPrereqsReconciled",
					Status:             metav1.ConditionFalse,
					Reason:             string(ReasonPending),
					Message:            "",
					ObservedGeneration: 1,
				},
			},
			expectedCondition: metav1.Condition{
				Type:    string(TypeReady),
				Status:  metav1.ConditionFalse,
				Reason:  string(ReasonPending),
				Message: "Reconciliation is in progress",
			},
		},
		{
			name: "Mix of stale and up-to-date not ready conditions",
			obj:  &MockObject{generation: 2},
			initialConditions: []metav1.Condition{
				{
					Type:               "ApplicationsReady",
					Status:             metav1.ConditionFalse,
					Reason:             string(ReasonPending),
					Message:            "",
					ObservedGeneration: 1, // stale
				},
				{
					Type:               "ApplicationPrereqsReconciled",
					Status:             metav1.ConditionFalse,
					Reason:             string(ReasonFailure),
					Message:            "",
					ObservedGeneration: 2, // current
				},
			},
			expectedCondition: metav1.Condition{
				Type:    string(TypeReady),
				Status:  metav1.ConditionFalse,
				Reason:  string(ReasonFailure),
				Message: ReadyConditionMessage(MessageNotReady, []string{"ApplicationPrereqsReconciled"}),
			},
		},
		{
			name: "Stale error condition with up-to-date pending condition",
			obj:  &MockObject{generation: 3},
			initialConditions: []metav1.Condition{
				{
					Type:               "ApplicationsReady",
					Status:             metav1.ConditionFalse,
					Reason:             string(ReasonError),
					Message:            "",
					ObservedGeneration: 1, // stale
				},
				{
					Type:               "ConfigReady",
					Status:             metav1.ConditionFalse,
					Reason:             string(ReasonPending),
					Message:            "",
					ObservedGeneration: 3, // current
				},
			},
			expectedCondition: metav1.Condition{
				Type:    string(TypeReady),
				Status:  metav1.ConditionFalse,
				Reason:  string(ReasonPending),
				Message: ReadyConditionMessage(MessageNotReady, []string{"ConfigReady"}),
			},
		},
		{
			name: "All conditions are ready but some have old generation",
			obj:  &MockObject{generation: 2},
			initialConditions: []metav1.Condition{
				{
					Type:               "ApplicationsReady",
					Status:             metav1.ConditionTrue,
					Reason:             string(ReasonSuccess),
					Message:            "",
					ObservedGeneration: 1, // stale but Ready=True
				},
				{
					Type:               "ConfigReady",
					Status:             metav1.ConditionTrue,
					Reason:             string(ReasonSuccess),
					Message:            "",
					ObservedGeneration: 2, // current
				},
			},
			expectedCondition: metav1.Condition{
				Type:   string(TypeReady),
				Status: metav1.ConditionTrue,
				Reason: string(ReasonSuccess),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.initialConditions) > 0 {
				tt.obj.SetConditions(tt.initialConditions)
			}
			SetSummary(tt.obj)

			readyCondition := Get(tt.obj, TypeReady)
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

// TestTypesAsStrings tests the TypesAsStrings function
func TestTypesAsStrings(t *testing.T) {
	g := NewWithT(t)
	tests := []struct {
		name     string
		types    []ConditionType
		expected []string
	}{
		{
			name:     "Empty array",
			types:    []ConditionType{},
			expected: []string{},
		},
		{
			name:     "Single item",
			types:    []ConditionType{TypeReady},
			expected: []string{"Ready"},
		},
		{
			name:     "Multiple items",
			types:    []ConditionType{TypeReady, "ApplicationsReady", "DPUServicesReady"},
			expected: []string{"Ready", "ApplicationsReady", "DPUServicesReady"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TypesAsStrings(tt.types)
			g.Expect(result).To(Equal(tt.expected))
		})
	}
}

// TestIsTrue tests the IsTrue function
func TestIsTrue(t *testing.T) {
	g := NewWithT(t)
	tests := []struct {
		name              string
		obj               *MockObject
		conditionType     ConditionType
		initialConditions []metav1.Condition
		expected          bool
	}{
		{
			name:          "Condition is true",
			obj:           &MockObject{},
			conditionType: TypeReady,
			initialConditions: []metav1.Condition{
				{
					Type:   string(TypeReady),
					Status: metav1.ConditionTrue,
					Reason: string(ReasonSuccess),
				},
			},
			expected: true,
		},
		{
			name:          "Condition is false",
			obj:           &MockObject{},
			conditionType: TypeReady,
			initialConditions: []metav1.Condition{
				{
					Type:   string(TypeReady),
					Status: metav1.ConditionFalse,
					Reason: string(ReasonFailure),
				},
			},
			expected: false,
		},
		{
			name:          "Condition is unknown",
			obj:           &MockObject{},
			conditionType: TypeReady,
			initialConditions: []metav1.Condition{
				{
					Type:   string(TypeReady),
					Status: metav1.ConditionUnknown,
					Reason: string(ReasonPending),
				},
			},
			expected: false,
		},
		{
			name:          "Condition is true but old generation",
			obj:           &MockObject{generation: 2},
			conditionType: TypeReady,
			initialConditions: []metav1.Condition{
				{
					Type:               string(TypeReady),
					Status:             metav1.ConditionTrue,
					Reason:             string(ReasonSuccess),
					ObservedGeneration: 1,
				},
			},
			expected: false,
		},
		{
			name:          "Condition does not exist",
			obj:           &MockObject{},
			conditionType: TypeReady,
			initialConditions: []metav1.Condition{
				{
					Type:   "SomeOtherCondition",
					Status: metav1.ConditionTrue,
					Reason: string(ReasonSuccess),
				},
			},
			expected: false,
		},
		{
			name:              "No conditions",
			obj:               &MockObject{},
			conditionType:     TypeReady,
			initialConditions: []metav1.Condition{},
			expected:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.obj.SetConditions(tt.initialConditions)
			result := IsTrue(tt.obj, tt.conditionType)
			g.Expect(result).To(Equal(tt.expected))
		})
	}
}

// TestReadyConditionMessage tests the ReadyConditionMessage function
func TestReadyConditionMessage(t *testing.T) {
	g := NewWithT(t)
	tests := []struct {
		name        string
		message     string
		unready     []string
		expectedMsg string
	}{
		{
			name:        "Empty unready conditions",
			message:     "Not ready",
			unready:     []string{},
			expectedMsg: "",
		},
		{
			name:        "One unready condition",
			message:     "Not ready",
			unready:     []string{"ApplicationsReady"},
			expectedMsg: "Not ready:\n* ApplicationsReady",
		},
		{
			name:        "Multiple unready conditions",
			message:     "Not ready",
			unready:     []string{"ApplicationsReady", "DPUServicesReady", "ConfigReconciled"},
			expectedMsg: "Not ready:\n* ApplicationsReady\n* DPUServicesReady\n* ConfigReconciled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ReadyConditionMessage(tt.message, tt.unready)
			g.Expect(result).To(Equal(tt.expectedMsg))
		})
	}
}

// TestJoinErrors tests the JoinErrors function
func TestJoinErrors(t *testing.T) {
	g := NewWithT(t)
	tests := []struct {
		name          string
		err           error
		indent        int
		expectedLines []string
		expectedNil   bool
	}{
		{
			name:        "Nil error returns nil",
			err:         nil,
			indent:      0,
			expectedNil: true,
		},
		{
			name:        "Empty error message",
			err:         errors.New(""),
			indent:      0,
			expectedNil: true,
		},
		{
			name:   "Single error with zero indent",
			err:    errors.New("single error"),
			indent: 0,
			expectedLines: []string{
				"* single error",
			},
		},
		{
			name:   "Single error with indent 1",
			err:    errors.New("single error"),
			indent: 1,
			expectedLines: []string{
				"  * single error",
			},
		},
		{
			name:   "Single error with indent 2",
			err:    errors.New("single error"),
			indent: 2,
			expectedLines: []string{
				"    * single error",
			},
		},
		{
			name:   "Aggregate error with multiple errors and zero indent",
			err:    kerrors.NewAggregate([]error{errors.New("first error"), errors.New("second error")}),
			indent: 0,
			expectedLines: []string{
				"* first error",
				"* second error",
			},
		},
		{
			name:   "Aggregate error with multiple errors and indent 1",
			err:    kerrors.NewAggregate([]error{errors.New("first error"), errors.New("second error")}),
			indent: 1,
			expectedLines: []string{
				"  * first error",
				"  * second error",
			},
		},
		{
			name:   "Error message already starting with asterisk",
			err:    kerrors.NewAggregate([]error{errors.New("first error"), errors.New("* already formatted")}),
			indent: 0,
			expectedLines: []string{
				"* first error* already formatted",
			},
		},
		{
			name:   "Error message with leading whitespace and asterisk",
			err:    kerrors.NewAggregate([]error{errors.New("first error"), errors.New("  * indented error")}),
			indent: 0,
			expectedLines: []string{
				"* first error  * indented error",
			},
		},
		{
			name:   "Error message starting with asterisk but no space",
			err:    kerrors.NewAggregate([]error{errors.New("first error"), errors.New("**no space**")}),
			indent: 0,
			expectedLines: []string{
				"* first error",
				"* **no space**",
			},
		},
		{
			name:   "Mixed error types - some with asterisk prefix, some without",
			err:    kerrors.NewAggregate([]error{errors.New("normal error"), errors.New("* prefixed error"), errors.New("another normal")}),
			indent: 1,
			expectedLines: []string{
				"  * normal error* prefixed error",
				"  * another normal",
			},
		},
		{
			name:   "Error with newlines",
			err:    errors.New("error with\nnewlines"),
			indent: 0,
			expectedLines: []string{
				"* error with",
				"newlines",
			},
		},
		{
			name: "Error with nested formatted errors",
			err: kerrors.NewAggregate([]error{
				errors.New("first error:\n"),
				kerrors.NewAggregate([]error{errors.New("  * second error\n  * third error")}),
			}),
			indent: 0,
			expectedLines: []string{
				"* first error:",
				"  * second error",
				"  * third error",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := JoinErrors(tt.err, tt.indent)

			if tt.expectedNil {
				g.Expect(result).ToNot(HaveOccurred())
			} else {
				g.Expect(result).To(HaveOccurred())
				expectedMsg := strings.Join(tt.expectedLines, "\n")
				g.Expect(result.Error()).To(Equal(expectedMsg), "got: %s", result.Error())
			}
		})
	}
}
