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

package cmd

import (
	"testing"

	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

func TestCelExpression(t *testing.T) {
	tests := []struct {
		name     string
		field    deprecatedField
		expected string
	}{
		{
			name: "single segment path",
			field: deprecatedField{
				Segments: []pathSegment{{Name: "spec"}},
			},
			expected: "!has(object.spec)",
		},
		{
			name: "two segment path",
			field: deprecatedField{
				Segments: []pathSegment{{Name: "spec"}, {Name: "foo"}},
			},
			expected: "!has(object.spec.foo)",
		},
		{
			name: "deeply nested path",
			field: deprecatedField{
				Segments: []pathSegment{{Name: "spec"}, {Name: "a"}, {Name: "b"}, {Name: "c"}},
			},
			expected: "!has(object.spec.a) || !has(object.spec.a.b) || !has(object.spec.a.b.c)",
		},
		{
			name: "array field with simple prefix",
			field: deprecatedField{
				Segments: []pathSegment{{Name: "spec"}, {Name: "dpuSets", IsArray: true}, {Name: "nodeSelector"}},
			},
			expected: "!has(object.spec.dpuSets) || object.spec.dpuSets.all(e0, !has(e0.nodeSelector))",
		},
		{
			name: "array field with nested prefix",
			field: deprecatedField{
				Segments: []pathSegment{{Name: "spec"}, {Name: "dpus"}, {Name: "dpuSets", IsArray: true}, {Name: "nodeSelector"}},
			},
			expected: "!has(object.spec.dpus) || !has(object.spec.dpus.dpuSets) || object.spec.dpus.dpuSets.all(e0, !has(e0.nodeSelector))",
		},
		{
			name: "nested arrays with intermediate object",
			field: deprecatedField{
				Segments: []pathSegment{
					{Name: "spec"},
					{Name: "bar", IsArray: true},
					{Name: "baz"},
					{Name: "fuzz", IsArray: true},
					{Name: "deprecatedField"},
				},
			},
			expected: "!has(object.spec.bar) || object.spec.bar.all(e0, !has(e0.baz) || !has(e0.baz.fuzz) || e0.baz.fuzz.all(e1, !has(e1.deprecatedField)))",
		},
		{
			name: "nested arrays directly",
			field: deprecatedField{
				Segments: []pathSegment{
					{Name: "spec"},
					{Name: "bar", IsArray: true},
					{Name: "baz", IsArray: true},
					{Name: "deprecated"},
				},
			},
			expected: "!has(object.spec.bar) || object.spec.bar.all(e0, !has(e0.baz) || e0.baz.all(e1, !has(e1.deprecated)))",
		},
		{
			name: "triple nested arrays",
			field: deprecatedField{
				Segments: []pathSegment{
					{Name: "spec"},
					{Name: "a", IsArray: true},
					{Name: "b", IsArray: true},
					{Name: "c", IsArray: true},
					{Name: "d"},
				},
			},
			expected: "!has(object.spec.a) || object.spec.a.all(e0, !has(e0.b) || e0.b.all(e1, !has(e1.c) || e1.c.all(e2, !has(e2.d))))",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(tt.field.celExpression()).To(Equal(tt.expected))
		})
	}
}

func TestWarningMessage(t *testing.T) {
	tests := []struct {
		name     string
		field    deprecatedField
		expected string
	}{
		{
			name: "simple deprecated field",
			field: deprecatedField{
				Segments:    []pathSegment{{Name: "spec"}, {Name: "clusterSelector"}},
				Description: "Deprecated: Use dpuSelector instead.",
			},
			expected: "spec.clusterSelector is deprecated. Use dpuSelector instead.",
		},
		{
			name: "Deprecated: after description text",
			field: deprecatedField{
				Segments:    []pathSegment{{Name: "spec"}, {Name: "old"}},
				Description: "Some field.\n\nDeprecated: will be removed in v2.",
			},
			expected: "spec.old is deprecated. will be removed in v2.",
		},
		{
			name: "array field",
			field: deprecatedField{
				Segments:    []pathSegment{{Name: "spec"}, {Name: "dpuSets", IsArray: true}, {Name: "nodeSelector"}},
				Description: "Deprecated: Use nodeSelectorV2 instead.",
			},
			expected: "spec.dpuSets[].nodeSelector is deprecated. Use nodeSelectorV2 instead.",
		},
		{
			name: "multiline description collapses whitespace",
			field: deprecatedField{
				Segments:    []pathSegment{{Name: "spec"}, {Name: "foo"}},
				Description: "Deprecated:  Use bar\n   instead.",
			},
			expected: "spec.foo is deprecated. Use bar instead.",
		},
		{
			name: "nested array field",
			field: deprecatedField{
				Segments:    []pathSegment{{Name: "spec"}, {Name: "bar", IsArray: true}, {Name: "baz"}, {Name: "fuzz", IsArray: true}, {Name: "old"}},
				Description: "Deprecated: removed.",
			},
			expected: "spec.bar[].baz.fuzz[].old is deprecated. removed.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(tt.field.warningMessage()).To(Equal(tt.expected))
		})
	}
}

func TestIsDeprecated(t *testing.T) {
	tests := []struct {
		name        string
		description string
		expected    bool
	}{
		{
			name:        "starts with Deprecated:",
			description: "Deprecated: use something else.",
			expected:    true,
		},
		{
			name:        "Deprecated: in middle of description",
			description: "Some field.\n\nDeprecated: will be removed.",
			expected:    true,
		},
		{
			name:        "lowercase deprecated is not matched",
			description: "deprecated: will be removed.",
			expected:    false,
		},
		{
			name:        "not deprecated",
			description: "This field configures the selector.",
			expected:    false,
		},
		{
			name:        "empty description",
			description: "",
			expected:    false,
		},
		{
			name:        "DEPRECATED all caps is not matched",
			description: "DEPRECATED: removed in v2",
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(isDeprecated(tt.description)).To(Equal(tt.expected))
		})
	}
}

func TestFindDeprecatedFields(t *testing.T) {
	tests := []struct {
		name           string
		crd            *apiextensionsv1.CustomResourceDefinition
		expectedFields []deprecatedField
	}{
		{
			name: "simple deprecated field in spec",
			crd: makeCRD(map[string]apiextensionsv1.JSONSchemaProps{
				"spec": {
					Type: "object",
					Properties: map[string]apiextensionsv1.JSONSchemaProps{
						"foo": {
							Type:        "string",
							Description: "Deprecated: use bar instead.",
						},
						"bar": {
							Type:        "string",
							Description: "The new field.",
						},
					},
				},
			}),
			expectedFields: []deprecatedField{
				{Segments: []pathSegment{{Name: "spec"}, {Name: "foo"}}, Description: "Deprecated: use bar instead."},
			},
		},
		{
			name: "deprecated field in status is skipped",
			crd: makeCRD(map[string]apiextensionsv1.JSONSchemaProps{
				"status": {
					Type: "object",
					Properties: map[string]apiextensionsv1.JSONSchemaProps{
						"old": {
							Type:        "string",
							Description: "Deprecated: no longer reported.",
						},
					},
				},
			}),
			expectedFields: nil,
		},
		{
			name: "nested deprecated field",
			crd: makeCRD(map[string]apiextensionsv1.JSONSchemaProps{
				"spec": {
					Type: "object",
					Properties: map[string]apiextensionsv1.JSONSchemaProps{
						"config": {
							Type: "object",
							Properties: map[string]apiextensionsv1.JSONSchemaProps{
								"old": {
									Type:        "string",
									Description: "Deprecated: removed.",
								},
							},
						},
					},
				},
			}),
			expectedFields: []deprecatedField{
				{Segments: []pathSegment{{Name: "spec"}, {Name: "config"}, {Name: "old"}}, Description: "Deprecated: removed."},
			},
		},
		{
			name: "deprecated field inside array items",
			crd: makeCRD(map[string]apiextensionsv1.JSONSchemaProps{
				"spec": {
					Type: "object",
					Properties: map[string]apiextensionsv1.JSONSchemaProps{
						"items": {
							Type: "array",
							Items: &apiextensionsv1.JSONSchemaPropsOrArray{
								Schema: &apiextensionsv1.JSONSchemaProps{
									Type: "object",
									Properties: map[string]apiextensionsv1.JSONSchemaProps{
										"old": {
											Type:        "string",
											Description: "Deprecated: gone.",
										},
									},
								},
							},
						},
					},
				},
			}),
			expectedFields: []deprecatedField{
				{
					Segments:    []pathSegment{{Name: "spec"}, {Name: "items", IsArray: true}, {Name: "old"}},
					Description: "Deprecated: gone.",
				},
			},
		},
		{
			name: "deprecated field inside nested arrays",
			crd: makeCRD(map[string]apiextensionsv1.JSONSchemaProps{
				"spec": {
					Type: "object",
					Properties: map[string]apiextensionsv1.JSONSchemaProps{
						"outer": {
							Type: "array",
							Items: &apiextensionsv1.JSONSchemaPropsOrArray{
								Schema: &apiextensionsv1.JSONSchemaProps{
									Type: "object",
									Properties: map[string]apiextensionsv1.JSONSchemaProps{
										"inner": {
											Type: "array",
											Items: &apiextensionsv1.JSONSchemaPropsOrArray{
												Schema: &apiextensionsv1.JSONSchemaProps{
													Type: "object",
													Properties: map[string]apiextensionsv1.JSONSchemaProps{
														"old": {
															Type:        "string",
															Description: "Deprecated: gone.",
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			}),
			expectedFields: []deprecatedField{
				{
					Segments:    []pathSegment{{Name: "spec"}, {Name: "outer", IsArray: true}, {Name: "inner", IsArray: true}, {Name: "old"}},
					Description: "Deprecated: gone.",
				},
			},
		},
		{
			name: "deprecated field inside nested array with intermediate object",
			crd: makeCRD(map[string]apiextensionsv1.JSONSchemaProps{
				"spec": {
					Type: "object",
					Properties: map[string]apiextensionsv1.JSONSchemaProps{
						"bar": {
							Type: "array",
							Items: &apiextensionsv1.JSONSchemaPropsOrArray{
								Schema: &apiextensionsv1.JSONSchemaProps{
									Type: "object",
									Properties: map[string]apiextensionsv1.JSONSchemaProps{
										"baz": {
											Type: "object",
											Properties: map[string]apiextensionsv1.JSONSchemaProps{
												"fuzz": {
													Type: "array",
													Items: &apiextensionsv1.JSONSchemaPropsOrArray{
														Schema: &apiextensionsv1.JSONSchemaProps{
															Type: "object",
															Properties: map[string]apiextensionsv1.JSONSchemaProps{
																"old": {
																	Type:        "string",
																	Description: "Deprecated: gone.",
																},
															},
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			}),
			expectedFields: []deprecatedField{
				{
					Segments:    []pathSegment{{Name: "spec"}, {Name: "bar", IsArray: true}, {Name: "baz"}, {Name: "fuzz", IsArray: true}, {Name: "old"}},
					Description: "Deprecated: gone.",
				},
			},
		},
		{
			name: "no deprecated fields",
			crd: makeCRD(map[string]apiextensionsv1.JSONSchemaProps{
				"spec": {
					Type: "object",
					Properties: map[string]apiextensionsv1.JSONSchemaProps{
						"name": {
							Type:        "string",
							Description: "The name of the thing.",
						},
					},
				},
			}),
			expectedFields: nil,
		},
		{
			name:           "no schema",
			crd:            &apiextensionsv1.CustomResourceDefinition{},
			expectedFields: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			fields := findDeprecatedFields(tt.crd)
			if tt.expectedFields == nil {
				g.Expect(fields).To(BeEmpty())
			} else {
				g.Expect(fields).To(Equal(tt.expectedFields))
			}
		})
	}
}

// makeCRD creates a minimal CRD with the given top-level properties in a single version schema.
func makeCRD(properties map[string]apiextensionsv1.JSONSchemaProps) *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name: "v1alpha1",
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type:       "object",
							Properties: properties,
						},
					},
				},
			},
		},
	}
}
