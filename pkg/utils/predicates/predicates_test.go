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

package predicates

import (
	"testing"

	"github.com/nvidia/doca-platform/pkg/conditions"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

func TestPredicates(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Predicates Suite")
}

// mockConditionsObject implements conditions.GetSet and client.Object for testing
type mockConditionsObject struct {
	metav1.TypeMeta
	metav1.ObjectMeta
	conditions []metav1.Condition
}

func (m *mockConditionsObject) GetConditions() []metav1.Condition {
	return m.conditions
}

func (m *mockConditionsObject) SetConditions(conditions []metav1.Condition) {
	m.conditions = conditions
}

func (m *mockConditionsObject) DeepCopyObject() runtime.Object {
	if m == nil {
		return nil
	}
	out := new(mockConditionsObject)
	m.DeepCopyInto(out)
	return out
}

func (m *mockConditionsObject) DeepCopyInto(out *mockConditionsObject) {
	*out = *m
	out.TypeMeta = m.TypeMeta
	m.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	if m.conditions != nil {
		out.conditions = make([]metav1.Condition, len(m.conditions))
		for i := range m.conditions {
			m.conditions[i].DeepCopyInto(&out.conditions[i])
		}
	}
}

func (m *mockConditionsObject) GetObjectKind() schema.ObjectKind {
	return m
}

var _ = Describe("ReadyConditionChanged", func() {
	var predicateFuncs predicate.Funcs

	BeforeEach(func() {
		predicateFuncs = ReadyConditionChanged()
	})

	Context("When handling Create events", func() {
		It("should trigger on create", func() {
			obj := &mockConditionsObject{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				conditions: []metav1.Condition{
					{
						Type:   string(conditions.TypeReady),
						Status: metav1.ConditionTrue,
					},
				},
			}

			e := event.CreateEvent{Object: obj}
			Expect(predicateFuncs.Create(e)).To(BeTrue())
		})
	})

	Context("When handling Update events", func() {
		DescribeTable("should handle Ready condition changes",
			func(oldReady, newReady, shouldTrigger bool) {
				oldConditionStatus := metav1.ConditionFalse
				if oldReady {
					oldConditionStatus = metav1.ConditionTrue
				}
				oldObj := &mockConditionsObject{
					ObjectMeta: metav1.ObjectMeta{Name: "test"},
					conditions: []metav1.Condition{
						{
							Type:   string(conditions.TypeReady),
							Status: oldConditionStatus,
						},
					},
				}

				newConditionStatus := metav1.ConditionFalse
				if newReady {
					newConditionStatus = metav1.ConditionTrue
				}
				newObj := &mockConditionsObject{
					ObjectMeta: metav1.ObjectMeta{Name: "test"},
					conditions: []metav1.Condition{
						{
							Type:   string(conditions.TypeReady),
							Status: newConditionStatus,
						},
					},
				}

				e := event.UpdateEvent{
					ObjectOld: oldObj,
					ObjectNew: newObj,
				}
				Expect(predicateFuncs.Update(e)).To(Equal(shouldTrigger))
			},
			Entry("Ready false to true", false, true, true),
			Entry("Ready true to false", true, false, true),
			Entry("Ready true to true", true, true, false),
			Entry("Ready false to false", false, false, false),
		)

		It("should trigger when Ready condition appears", func() {
			oldObj := &mockConditionsObject{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				conditions: []metav1.Condition{},
			}
			newObj := &mockConditionsObject{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				conditions: []metav1.Condition{
					{
						Type:   string(conditions.TypeReady),
						Status: metav1.ConditionTrue,
					},
				},
			}

			e := event.UpdateEvent{
				ObjectOld: oldObj,
				ObjectNew: newObj,
			}
			Expect(predicateFuncs.Update(e)).To(BeTrue())
		})

		It("should not trigger for objects that don't implement GetSet", func() {
			// Use ConfigMap which implements client.Object but not conditions.GetSet
			oldObj := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
			}
			newObj := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
			}

			e := event.UpdateEvent{
				ObjectOld: oldObj,
				ObjectNew: newObj,
			}
			Expect(predicateFuncs.Update(e)).To(BeFalse())
		})
	})

	Context("When handling Delete events", func() {
		It("should trigger on delete", func() {
			obj := &mockConditionsObject{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				conditions: []metav1.Condition{
					{
						Type:   string(conditions.TypeReady),
						Status: metav1.ConditionTrue,
					},
				},
			}

			e := event.DeleteEvent{Object: obj}
			Expect(predicateFuncs.Delete(e)).To(BeTrue())
		})
	})

	Context("When handling Generic events", func() {
		It("should not trigger on generic events", func() {
			obj := &mockConditionsObject{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				conditions: []metav1.Condition{
					{
						Type:   string(conditions.TypeReady),
						Status: metav1.ConditionTrue,
					},
				},
			}

			e := event.GenericEvent{Object: obj}
			Expect(predicateFuncs.Generic(e)).To(BeFalse())
		})
	})
})
