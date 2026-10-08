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

package deprecation

import (
	"testing"

	. "github.com/onsi/gomega"
)

func TestJSONPath(t *testing.T) {
	g := NewWithT(t)

	g.Expect(DeprecatedField{Segments: []PathSegment{{Name: "spec"}, {Name: "foo"}}}.JSONPath()).To(Equal("spec.foo"))
	g.Expect(DeprecatedField{Segments: []PathSegment{
		{Name: "spec"}, {Name: "bar", IsArray: true}, {Name: "baz"}, {Name: "fuzz", IsArray: true}, {Name: "old"},
	}}.JSONPath()).To(Equal("spec.bar[].baz.fuzz[].old"))
}

func TestIsSetIn(t *testing.T) {
	tests := []struct {
		name     string
		field    DeprecatedField
		obj      map[string]any
		expected bool
	}{
		{
			name:     "top-level scalar set",
			field:    DeprecatedField{Segments: []PathSegment{{Name: "spec"}, {Name: "bmcIP"}}},
			obj:      map[string]any{"spec": map[string]any{"bmcIP": "10.0.0.1"}},
			expected: true,
		},
		{
			name:     "top-level scalar absent",
			field:    DeprecatedField{Segments: []PathSegment{{Name: "spec"}, {Name: "bmcIP"}}},
			obj:      map[string]any{"spec": map[string]any{}},
			expected: false,
		},
		{
			name:     "explicit empty string still counts as set",
			field:    DeprecatedField{Segments: []PathSegment{{Name: "spec"}, {Name: "bmcIP"}}},
			obj:      map[string]any{"spec": map[string]any{"bmcIP": ""}},
			expected: true,
		},
		{
			name:     "explicit false still counts as set",
			field:    DeprecatedField{Segments: []PathSegment{{Name: "spec"}, {Name: "flag"}}},
			obj:      map[string]any{"spec": map[string]any{"flag": false}},
			expected: true,
		},
		{
			name:     "explicit zero still counts as set",
			field:    DeprecatedField{Segments: []PathSegment{{Name: "spec"}, {Name: "count"}}},
			obj:      map[string]any{"spec": map[string]any{"count": int64(0)}},
			expected: true,
		},
		{
			name:     "explicit empty map still counts as set",
			field:    DeprecatedField{Segments: []PathSegment{{Name: "spec"}, {Name: "dpuSelector"}}},
			obj:      map[string]any{"spec": map[string]any{"dpuSelector": map[string]any{}}},
			expected: true,
		},
		{
			name:     "explicit empty list still counts as set",
			field:    DeprecatedField{Segments: []PathSegment{{Name: "spec"}, {Name: "exclusions"}}},
			obj:      map[string]any{"spec": map[string]any{"exclusions": []any{}}},
			expected: true,
		},
		{
			name:     "nested object field set",
			field:    DeprecatedField{Segments: []PathSegment{{Name: "spec"}, {Name: "config"}, {Name: "old"}}},
			obj:      map[string]any{"spec": map[string]any{"config": map[string]any{"old": "x"}}},
			expected: true,
		},
		{
			name:     "nested object field absent because parent absent",
			field:    DeprecatedField{Segments: []PathSegment{{Name: "spec"}, {Name: "config"}, {Name: "old"}}},
			obj:      map[string]any{"spec": map[string]any{}},
			expected: false,
		},
		{
			name: "array field set on one element",
			field: DeprecatedField{Segments: []PathSegment{
				{Name: "spec"}, {Name: "dpuSets", IsArray: true}, {Name: "nodeSelector"},
			}},
			obj: map[string]any{"spec": map[string]any{"dpuSets": []any{
				map[string]any{"name": "a"},
				map[string]any{"name": "b", "nodeSelector": map[string]any{}},
			}}},
			expected: true,
		},
		{
			name: "array field set on no elements",
			field: DeprecatedField{Segments: []PathSegment{
				{Name: "spec"}, {Name: "dpuSets", IsArray: true}, {Name: "nodeSelector"},
			}},
			obj: map[string]any{"spec": map[string]any{"dpuSets": []any{
				map[string]any{"name": "a"},
				map[string]any{"name": "b"},
			}}},
			expected: false,
		},
		{
			name: "array field absent entirely",
			field: DeprecatedField{Segments: []PathSegment{
				{Name: "spec"}, {Name: "dpuSets", IsArray: true}, {Name: "nodeSelector"},
			}},
			obj:      map[string]any{"spec": map[string]any{}},
			expected: false,
		},
		{
			name: "nested arrays, deeply set element",
			field: DeprecatedField{Segments: []PathSegment{
				{Name: "spec"}, {Name: "bar", IsArray: true}, {Name: "baz"}, {Name: "fuzz", IsArray: true}, {Name: "old"},
			}},
			obj: map[string]any{"spec": map[string]any{"bar": []any{
				map[string]any{"baz": map[string]any{"fuzz": []any{
					map[string]any{},
					map[string]any{"old": "x"},
				}}},
			}}},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(tt.field.IsSetIn(tt.obj)).To(Equal(tt.expected))
		})
	}
}
