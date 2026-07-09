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

// Package dpuflavortemplate renders a DPUFlavorTemplate body into a concrete DPUFlavor
// using per-device values from DPUDevice.spec.values.
//
// Rendering is hardened for availability: the template body and the rendered output are
// size-bounded, execution panics are converted into render errors, and {{define}},
// {{block}} and {{template}} actions are rejected statically because template invocation
// enables unbounded recursion, and stack exhaustion cannot be recovered.
package dpuflavortemplate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"text/template"
	"text/template/parse"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"
)

const (
	// maxTemplateBytes bounds the template body accepted by Render. It mirrors the
	// MaxLength validation on DPUFlavorTemplate.spec.template so out-of-cluster callers
	// (e.g. rendering local files) are bounded too.
	maxTemplateBytes = 1 << 20 // 1 MiB
	// maxRenderedBytes bounds the rendered output. It matches the default etcd request
	// size ceiling: a larger render could never be stored as a DPUFlavor anyway.
	maxRenderedBytes = 1536 << 10 // 1.5 MiB
)

// Render executes the template body against the given values and unmarshals the result
// into a typed DPUFlavor. The dpuResources and systemReservedResources fields from the
// template spec are not templated: when set they take precedence and are stamped onto the
// returned flavor's spec, overriding anything the rendered body may contain.
func Render(spec provisioningv1.DPUFlavorTemplateSpec, values *runtime.RawExtension) (*provisioningv1.DPUFlavor, error) {
	data, err := decodeValues(values)
	if err != nil {
		return nil, err
	}

	rendered, err := renderTemplate(spec.Template, data)
	if err != nil {
		return nil, err
	}

	flavor := &provisioningv1.DPUFlavor{}
	if err := yaml.Unmarshal([]byte(rendered), flavor); err != nil {
		return nil, fmt.Errorf("rendered template is not a valid DPUFlavor: %w", err)
	}

	// Deep copy so the rendered flavor never shares map storage with the template spec, which
	// may be a cached object the caller must not mutate.
	if spec.DPUResources != nil {
		flavor.Spec.DPUResources = spec.DPUResources.DeepCopy()
	}
	if spec.SystemReservedResources != nil {
		flavor.Spec.SystemReservedResources = spec.SystemReservedResources.DeepCopy()
	}

	return flavor, nil
}

// renderTemplate executes a Go text/template body against data. A reference to a key that
// is absent from the values fails the render rather than emitting "<no value>".
func renderTemplate(body string, data map[string]interface{}) (rendered string, err error) {
	if len(body) > maxTemplateBytes {
		return "", fmt.Errorf("template body is %d bytes, exceeding the %d byte limit", len(body), maxTemplateBytes)
	}
	// text/template re-panics unexpected runtime errors instead of returning them;
	// convert those into render errors so a hostile template cannot crash the caller.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("failed to render template: panic: %v", r)
		}
	}()
	tmpl, err := template.New("dpuflavortemplate").Option("missingkey=error").Parse(body)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}
	if err := rejectAssociatedTemplates(tmpl); err != nil {
		return "", err
	}
	out := &limitedWriter{limit: maxRenderedBytes}
	if err := tmpl.Execute(out, data); err != nil {
		return "", fmt.Errorf("failed to render template: %w", err)
	}
	return out.buf.String(), nil
}

// rejectAssociatedTemplates fails templates that declare or invoke associated templates.
// {{define}} and {{block}} declare associated templates and {{template}} invokes one
// (including the root template itself); invocation depth is unlimited in text/template,
// and the resulting stack exhaustion is a fatal error that recover cannot intercept.
func rejectAssociatedTemplates(tmpl *template.Template) error {
	if len(tmpl.Templates()) > 1 {
		return errors.New("template must not declare nested templates: {{define}} and {{block}} are not supported")
	}
	if tmpl.Tree != nil && hasTemplateNode(tmpl.Tree.Root) {
		return errors.New("template must not invoke templates: {{template}} is not supported")
	}
	return nil
}

// hasTemplateNode reports whether the parse tree contains a {{template}} invocation,
// descending into the bodies of if/range/with actions.
func hasTemplateNode(node parse.Node) bool {
	switch n := node.(type) {
	case *parse.TemplateNode:
		return true
	case *parse.ListNode:
		if n == nil {
			return false
		}
		for _, item := range n.Nodes {
			if hasTemplateNode(item) {
				return true
			}
		}
	case *parse.IfNode:
		return hasTemplateNode(n.List) || hasTemplateNode(n.ElseList)
	case *parse.RangeNode:
		return hasTemplateNode(n.List) || hasTemplateNode(n.ElseList)
	case *parse.WithNode:
		return hasTemplateNode(n.List) || hasTemplateNode(n.ElseList)
	}
	return false
}

// limitedWriter accumulates template output and fails any write that would grow it past
// limit, bounding the memory an amplifying template (e.g. nested ranges) can consume.
type limitedWriter struct {
	buf   bytes.Buffer
	limit int
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if w.buf.Len()+len(p) > w.limit {
		return 0, fmt.Errorf("rendered output exceeds the %d byte limit", w.limit)
	}
	return w.buf.Write(p)
}

// decodeValues turns DPUDevice.spec.values into a map usable by text/template. A nil or
// empty value yields an empty map so templates without placeholders still render.
func decodeValues(values *runtime.RawExtension) (map[string]interface{}, error) {
	out := map[string]interface{}{}
	if values == nil || len(values.Raw) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(values.Raw, &out); err != nil {
		return nil, fmt.Errorf("failed to decode DPUDevice.spec.values: %w", err)
	}
	return out, nil
}
