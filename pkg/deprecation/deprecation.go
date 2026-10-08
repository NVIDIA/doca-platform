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

// Package deprecation holds the types the runtime deprecated-field-usage scanner
// (internal/operator/controllers) needs to check whether a deprecated field is set on a live
// object. The CRD-schema discovery that produces these values (walking OpenAPI schemas for
// "Deprecated:" descriptions, building CEL expressions and warning messages) lives in
// hack/tools/dpfdev's codegen tool, which generates pkg/deprecation/zz_generated_deprecated_fields.go.
package deprecation

import (
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// PathSegment represents one named segment in a JSON path.
// If IsArray is true, this segment is an array and iteration is needed to inspect its elements.
type PathSegment struct {
	Name    string
	IsArray bool
}

// DeprecatedField represents a deprecated field found in a CRD schema.
type DeprecatedField struct {
	// Segments is the structured path to the deprecated field.
	// e.g. for spec.bar[].baz.fuzz[].old:
	// [{Name:"spec"}, {Name:"bar", IsArray:true}, {Name:"baz"}, {Name:"fuzz", IsArray:true}, {Name:"old"}]
	Segments []PathSegment
	// Description is the full description text of the field (containing the Deprecated: text)
	Description string
}

// GVKDeprecations groups every deprecated field found on a single CRD's storage version, keyed by
// GroupVersionKind. This is the shape hack/tools/dpfdev generates into
// zz_generated_deprecated_fields.go's KnownDeprecations for the runtime scanner to consume without needing to
// discover or walk CRD schemas itself.
type GVKDeprecations struct {
	GVK      schema.GroupVersionKind
	ListKind string
	Fields   []DeprecatedField
}

// JSONPath returns a human-readable path, e.g. "spec.dpuSets[].nodeSelector".
func (d DeprecatedField) JSONPath() string {
	var parts []string
	for _, seg := range d.Segments {
		if seg.IsArray {
			parts = append(parts, seg.Name+"[]")
		} else {
			parts = append(parts, seg.Name)
		}
	}
	return strings.Join(parts, ".")
}

// IsSetIn reports whether the deprecated field is set on obj, an unstructured object (e.g.
// unstructured.Unstructured.Object). For an array segment, the field is considered set if it is
// set on ANY element of the array.
func (d DeprecatedField) IsSetIn(obj map[string]any) bool {
	return fieldIsSet(obj, d.Segments)
}

// fieldIsSet walks segments against cur, which is either a map[string]any (the current
// object) or a []any (when the previous segment was an array).
func fieldIsSet(cur any, segments []PathSegment) bool {
	if len(segments) == 0 {
		return cur != nil
	}

	switch v := cur.(type) {
	case []any:
		// The previous segment was an array: the field is set if it's set on ANY element.
		for _, item := range v {
			itemMap, ok := item.(map[string]any)
			if ok && fieldIsSet(itemMap, segments) {
				return true
			}
		}
		return false
	case map[string]any:
		// Regular object: descend into the next segment.
		value, present := v[segments[0].Name]
		if !present {
			return false
		}
		if len(segments) == 1 {
			return true
		}
		return fieldIsSet(value, segments[1:])
	default:
		// Neither an object nor an array (e.g. nil, or a scalar where a nested path was expected).
		return false
	}
}
