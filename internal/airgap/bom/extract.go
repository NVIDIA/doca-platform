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

package bom

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
)

// FoundImage is an image reference found in a Kubernetes object.
type FoundImage struct {
	Raw string
	// Object identifies the object, Kind/namespace/name.
	Object string
	// Field is the path of the field the value was found in.
	Field string
	// Strict is true for a value that must be an image reference (a container image field). A
	// non-strict value was found by a heuristic (an --*-image= argument, an *_IMAGE environment
	// variable, an image field of a custom resource); Engine.Images drops it when it does not parse
	// or is a bare word.
	Strict bool
}

func (f FoundImage) String() string {
	return fmt.Sprintf("%s %s: %q", f.Object, f.Field, f.Raw)
}

var containerListFields = []string{"containers", "initContainers", "ephemeralContainers"}

// ImagesFromManifests parses multi-document YAML of Kubernetes objects (e.g. a rendered chart) and
// returns the images found in them, see ImagesFromObject. Documents that are empty or not
// objects are skipped.
func ImagesFromManifests(data []byte) ([]FoundImage, error) {
	objs, err := DecodeObjects(data)
	if err != nil {
		return nil, err
	}
	var out []FoundImage
	for _, obj := range objs {
		out = append(out, ImagesFromObject(obj.Object)...)
	}
	return out, nil
}

// DecodeObjects decodes multi-document YAML (or JSON) into objects. Documents without a kind,
// e.g. empty documents, are skipped. A List is flattened into its items.
func DecodeObjects(data []byte) ([]*unstructured.Unstructured, error) {
	dec := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	var out []*unstructured.Unstructured
	for {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("failed to decode YAML document: %w", err)
		}
		if m == nil {
			continue
		}
		obj := &unstructured.Unstructured{Object: m}
		if obj.GetKind() == "" {
			continue
		}
		if obj.IsList() {
			if err := obj.EachListItem(func(o runtime.Object) error {
				if u, ok := o.(*unstructured.Unstructured); ok {
					out = append(out, u)
				}
				return nil
			}); err != nil {
				return nil, err
			}
			continue
		}
		out = append(out, obj)
	}
	return out, nil
}

// ImagesFromObject returns the images of one Kubernetes object, of any kind, ordered by field path:
//
//   - the image of every container of every pod spec anywhere in the object, including workload
//     templates and custom resources embedding pod specs: strict;
//   - container arguments --<something>image=<ref>, environment variables named *_IMAGE with a
//     literal value, and any other string field named "image" or ending in "Image": non-strict.
//
// A container whose image field is empty is returned as well, so Engine.Images reports it as an
// error: an empty image usually means the chart got no value for it, and the BOM would miss it.
// An empty non-strict value is skipped.
//
// The non-strict search finds no image passed as a separate argument (--image <ref>), set with
// valueFrom, or used as an image volume. No chart in this repository uses these forms. Such an
// image is missing from the BOM without an error: airgap-bom check-release catches a missing
// first-party image, but nothing catches a missing third-party image.
func ImagesFromObject(obj map[string]any) []FoundImage {
	u := &unstructured.Unstructured{Object: obj}
	id := u.GetKind() + "/"
	if ns := u.GetNamespace(); ns != "" {
		id += ns + "/"
	}
	id += u.GetName()
	w := &walker{object: id}
	w.walk(obj, "")
	slices.SortFunc(w.found, func(a, b FoundImage) int {
		return cmp.Or(strings.Compare(a.Field, b.Field), strings.Compare(a.Raw, b.Raw))
	})
	return w.found
}

type walker struct {
	object string
	found  []FoundImage
}

func (w *walker) add(raw, field string, strict bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" && !strict {
		return
	}
	w.found = append(w.found, FoundImage{Raw: raw, Object: w.object, Field: field, Strict: strict})
}

// walk visits v at path.
func (w *walker) walk(v any, path string) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			child := t[k]
			childPath := path + "." + k
			if slices.Contains(containerListFields, k) {
				if list, ok := child.([]any); ok {
					for i, c := range list {
						if cm, ok := c.(map[string]any); ok {
							w.container(cm, fmt.Sprintf("%s[%d]", childPath, i))
						}
					}
					continue
				}
			}
			if s, ok := child.(string); ok {
				if k == "image" || strings.HasSuffix(k, "Image") {
					w.add(s, childPath, false)
				}
				continue
			}
			w.walk(child, childPath)
		}
	case []any:
		for i, c := range t {
			w.walk(c, fmt.Sprintf("%s[%d]", path, i))
		}
	}
}

// container records the images of one container and walks the rest of it.
func (w *walker) container(c map[string]any, path string) {
	if img, ok := c["image"].(string); ok {
		w.add(img, path+".image", true)
	}
	for _, field := range []string{"args", "command"} {
		list, _ := c[field].([]any)
		for i, a := range list {
			s, ok := a.(string)
			if !ok {
				continue
			}
			if v, ok := imageFlagValue(s); ok {
				w.add(v, fmt.Sprintf("%s.%s[%d]", path, field, i), false)
			}
		}
	}
	env, _ := c["env"].([]any)
	for i, e := range env {
		em, ok := e.(map[string]any)
		if !ok {
			continue
		}
		name, _ := em["name"].(string)
		value, ok := em["value"].(string)
		if ok && strings.HasSuffix(strings.ToUpper(name), "_IMAGE") {
			w.add(value, fmt.Sprintf("%s.env[%d]", path, i), false)
		}
	}
	// Containers can embed further objects only in unusual CRs; walk the remaining fields for
	// nested pod specs and image fields.
	for k, v := range c {
		if k == "image" || k == "args" || k == "command" || k == "env" {
			continue
		}
		w.walk(v, path+"."+k)
	}
}

// imageFlagValue returns the value of an argument --<name>image=<value>.
func imageFlagValue(arg string) (string, bool) {
	if !strings.HasPrefix(arg, "--") {
		return "", false
	}
	name, value, ok := strings.Cut(arg, "=")
	if !ok || !strings.HasSuffix(strings.ToLower(name), "image") {
		return "", false
	}
	return value, true
}
