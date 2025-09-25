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

package templates

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"k8s.io/apimachinery/pkg/runtime"
	syaml "sigs.k8s.io/yaml"
)

// TemplateDelimsAnnotation is the annotation key used to specify custom template delimiters
// in the DPU service configuration. The value should be a comma-separated string
// representing the opening and closing delimiters, e.g., "{{,}}".
// This is useful in cases where the default delimiters conflict with other templating
// systems or when users want to use different delimiters for their templates.
const TemplateDelimsAnnotation string = "svc.dpu.nvidia.com/template-delimiter"

// rendr processes a raw values using Go templating
// It takes the raw service configuration string and template parameters, and returns
// the processed configuration string or an error
func render(raw string, params any, annotations map[string]string) ([]byte, error) {
	// Create a new template and parse the configuration
	tmpl, err := template.New("serviceConfig").
		Option("missingkey=error").
		Delims(templateDelimsFromAnnotations(annotations)).
		Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to parse service configuration template: %w", err)
	}

	// Execute the template with the provided parameters
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, params); err != nil {
		return nil, fmt.Errorf("failed to execute service configuration template: %w", err)
	}

	return buf.Bytes(), nil
}

// Render processes a runtime.RawExtension using Go templating. It takes the raw data, template parameters,
// and annotations, and returns the processed data as a runtime.RawExtension
// or an error if the processing fails.
func Render(toRender *runtime.RawExtension, params any, annotations map[string]string) (*runtime.RawExtension, error) {
	if toRender == nil || toRender.Raw == nil {
		return nil, fmt.Errorf("no data to render: got nil RawExtension")
	}

	// Marshal the configuration to YAML
	copied := toRender.DeepCopy()
	data, err := syaml.JSONToYAML(copied.Raw)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal service configuration to YAML: %w", err)
	}
	// Apply templating to the YAML configuration
	templated, err := render(string(data), params, annotations)
	if err != nil {
		return nil, fmt.Errorf("failed to template service configuration: %w", err)
	}

	// Unmarshal the templated YAML back to a runtime.RawExtension
	rawObj, err := syaml.YAMLToJSON(templated)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal templated service configuration: %w", err)
	}
	// Update the Helm chart values with the processed configuration
	return &runtime.RawExtension{
		Raw: rawObj,
	}, nil
}

func templateDelimsFromAnnotations(annotations map[string]string) (string, string) {
	if delims, ok := annotations[TemplateDelimsAnnotation]; ok {
		if elems := strings.Split(delims, ","); len(elems) == 2 {
			return strings.TrimSpace(elems[0]), strings.TrimSpace(elems[1])
		}
	}
	return "{{", "}}"
}
