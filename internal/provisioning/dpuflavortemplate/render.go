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
// NOTE: this package intentionally omits availability hardening (bounded input/output,
// panic recovery, and rejection of nested template definitions). Those guards are tracked
// separately and must be added before the feature is enabled in a production path.
package dpuflavortemplate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"text/template"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"
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
func renderTemplate(body string, data map[string]interface{}) (string, error) {
	tmpl, err := template.New("dpuflavortemplate").Option("missingkey=error").Parse(body)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to render template: %w", err)
	}
	return buf.String(), nil
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
