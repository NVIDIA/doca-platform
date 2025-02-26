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

package dpfctl

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	"github.com/nvidia/doca-platform/internal/conditions"

	"github.com/fatih/color"
	"github.com/olekukonko/tablewriter"
	. "github.com/onsi/gomega"
	gtype "github.com/onsi/gomega/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func Test_getRowName(t *testing.T) {
	tests := []struct {
		name   string
		object ctrlclient.Object
		expect string
	}{
		{
			name:   "Row name for objects should be kind/name",
			object: fakeObject("c1"),
			expect: "Object/c1",
		},
		{
			name:   "Row name for a deleting object should have deleted prefix",
			object: fakeObject("c1", withDeletionTimestamp),
			expect: "!! IN DELETION !! Object/c1",
		},
		{
			name:   "Row name for objects with meta name should be meta-name - kind/name",
			object: fakeObject("c1", withAnnotation(ObjectMetaNameAnnotation, "MetaName")),
			expect: "MetaName - Object/c1",
		},
		{
			name:   "Row name for virtual objects should be name",
			object: fakeObject("c1", withAnnotation(VirtualObjectAnnotation, "True")),
			expect: "c1",
		},
		{
			name: "Row name for group objects should be #-of-items kind",
			object: fakeObject("c1",
				withAnnotation(VirtualObjectAnnotation, "True"),
				withAnnotation(GroupObjectAnnotation, "True"),
				withAnnotation(GroupItemsAnnotation, "c1, c2, c3"),
			),
			expect: "3 Objects...",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			got := getRowName(tt.object)
			g.Expect(got).To(Equal(tt.expect))
		})
	}
}

func Test_newConditionDescriptor_readyColor(t *testing.T) {
	tests := []struct {
		name             string
		condition        *metav1.Condition
		expectReadyColor *color.Color
	}{
		{
			name:             "True condition should be green",
			condition:        &metav1.Condition{Status: metav1.ConditionTrue},
			expectReadyColor: green,
		},
		{
			name:             "Unknown condition should be white",
			condition:        &metav1.Condition{Status: metav1.ConditionUnknown},
			expectReadyColor: white,
		},
		{
			name:             "Condition without status should be gray",
			condition:        &metav1.Condition{},
			expectReadyColor: gray,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			got := newConditionDescriptor(tt.condition, false)
			g.Expect(got.readyColor).To(Equal(tt.expectReadyColor))
		})
	}
}

func Test_TreePrefix(t *testing.T) {
	tests := []struct {
		name         string
		objectTree   *ObjectTree
		expectPrefix []string
	}{
		{
			name: "First level child should get the right prefix",
			objectTree: func() *ObjectTree {
				root := fakeObject("root")
				objectTree := NewObjectTree(root, ObjectTreeOptions{})

				o1 := fakeObject("child1")
				o2 := fakeObject("child2")
				objectTree.Add(root, o1)
				objectTree.Add(root, o2)
				return objectTree
			}(),
			expectPrefix: []string{
				"Object/root",
				"├─Object/child1", // first objects gets ├─
				"└─Object/child2", // last objects gets └─
			},
		},
		{
			name: "Second level child should get the right prefix",
			objectTree: func() *ObjectTree {
				root := fakeObject("root")
				obectjTree := NewObjectTree(root, ObjectTreeOptions{})

				o1 := fakeObject("child1")
				o1_1 := fakeObject("child1.1")
				o1_2 := fakeObject("child1.2")
				o2 := fakeObject("child2")
				o2_1 := fakeObject("child2.1")
				o2_2 := fakeObject("child2.2")

				obectjTree.Add(root, o1)
				obectjTree.Add(o1, o1_1)
				obectjTree.Add(o1, o1_2)
				obectjTree.Add(root, o2)
				obectjTree.Add(o2, o2_1)
				obectjTree.Add(o2, o2_2)
				return obectjTree
			}(),
			expectPrefix: []string{
				"Object/root",
				"├─Object/child1",
				"│ ├─Object/child1.1", // first second level child gets pipes and ├─
				"│ └─Object/child1.2", // last second level child gets pipes and └─
				"└─Object/child2",
				"  ├─Object/child2.1", // first second level child spaces and ├─
				"  └─Object/child2.2", // last second level child gets spaces and └─
			},
		},
		{
			name: "Conditions should get the right prefix",
			objectTree: func() *ObjectTree {
				root := fakeObject("root")
				obectjTree := NewObjectTree(root, ObjectTreeOptions{})

				o1 := fakeObject("child1",
					withAnnotation(ShowObjectConditionsAnnotation, "True"),
					withCondition(trueCondition("C1.1")),
					withCondition(trueCondition("C1.2")),
				)
				o2 := fakeObject("child2",
					withAnnotation(ShowObjectConditionsAnnotation, "True"),
					withCondition(trueCondition("C2.1")),
					withCondition(trueCondition("C2.2")),
				)
				obectjTree.Add(root, o1)
				obectjTree.Add(root, o2)
				return obectjTree
			}(),
			expectPrefix: []string{
				"Object/root",
				"├─Object/child1",
				"│             ├─C1.1", // first condition child gets pipes and ├─
				"│             └─C1.2", // last condition child gets └─ and pipes and └─
				"└─Object/child2",
				"              ├─C2.1", // first condition child gets spaces and ├─
				"              └─C2.2", // last condition child gets spaces and └─
			},
		},
		{
			name: "Conditions should get the right prefix if the object has a child",
			objectTree: func() *ObjectTree {
				root := fakeObject("root")
				obectjTree := NewObjectTree(root, ObjectTreeOptions{})

				o1 := fakeObject("child1",
					withAnnotation(ShowObjectConditionsAnnotation, "True"),
					withCondition(trueCondition("C1.1")),
					withCondition(trueCondition("C1.2")),
				)
				o1_1 := fakeObject("child1.1")

				o2 := fakeObject("child2",
					withAnnotation(ShowObjectConditionsAnnotation, "True"),
					withCondition(trueCondition("C2.1")),
					withCondition(trueCondition("C2.2")),
				)
				o2_1 := fakeObject("child2.1")
				obectjTree.Add(root, o1)
				obectjTree.Add(o1, o1_1)
				obectjTree.Add(root, o2)
				obectjTree.Add(o2, o2_1)
				return obectjTree
			}(),
			expectPrefix: []string{
				"Object/root",
				"├─Object/child1",
				"│ │           ├─C1.1", // first condition child gets pipes, children pipe and ├─
				"│ │           └─C1.2", // last condition child gets pipes, children pipe and └─
				"│ └─Object/child1.1",
				"└─Object/child2",
				"  │           ├─C2.1", // first condition child gets spaces, children pipe and ├─
				"  │           └─C2.2", // last condition child gets spaces, children pipe and └─
				"  └─Object/child2.1",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			var output bytes.Buffer

			// Creates the output table
			tbl := tablewriter.NewWriter(&output)

			formatTableTree(tbl)

			addObjectRow("", tbl, tt.objectTree, tt.objectTree.GetRoot())
			tbl.Render()

			g.Expect(output.String()).Should(MatchTable(tt.expectPrefix))
		})
	}
}

type objectOption func(object ctrlclient.Object)

func fakeObject(name string, options ...objectOption) ctrlclient.Object {
	c := &dpuservicev1.DPUService{ // suing type DPUService for simplicity, but this could be any object
		TypeMeta: metav1.TypeMeta{
			Kind: "Object",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "ns",
			Name:      name,
			UID:       types.UID(name),
		},
	}
	for _, opt := range options {
		opt(c)
	}
	return c
}

func withAnnotation(name, value string) func(ctrlclient.Object) {
	return func(c ctrlclient.Object) {
		if c.GetAnnotations() == nil {
			c.SetAnnotations(map[string]string{})
		}
		a := c.GetAnnotations()
		a[name] = value
		c.SetAnnotations(a)
	}
}

func withCondition(c *metav1.Condition) func(ctrlclient.Object) {
	return func(m ctrlclient.Object) {
		setter := m.(conditions.GetSet)
		conditions.AddTrue(setter, conditions.ConditionType(c.Type))
	}
}

func withDeletionTimestamp(object ctrlclient.Object) {
	now := metav1.Now()
	object.SetDeletionTimestamp(&now)
}

type Table struct {
	tableData []string
}

func MatchTable(expected []string) gtype.GomegaMatcher {
	return &Table{tableData: expected}
}

func (t *Table) Match(actual interface{}) (bool, error) {
	tableString := actual.(string)
	tableParts := strings.Split(tableString, "\n")

	for i := range t.tableData {
		if !strings.HasPrefix(tableParts[i], t.tableData[i]) {
			return false, nil
		}
	}
	return true, nil
}

func (t *Table) FailureMessage(actual interface{}) string {
	actualTable := strings.Split(actual.(string), "\n")
	return fmt.Sprintf("Expected %v and received %v", t.tableData, actualTable)
}

func (t *Table) NegatedFailureMessage(actual interface{}) string {
	actualTable := strings.Split(actual.(string), "\n")
	return fmt.Sprintf("Expected %v and received %v", t.tableData, actualTable)
}

// trueCondition returns a condition with Status=True and the given type.
func trueCondition(t conditions.ConditionType) *metav1.Condition {
	return &metav1.Condition{
		Type:   string(t),
		Status: metav1.ConditionTrue,
	}
}
