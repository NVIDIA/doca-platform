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
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/spf13/cobra"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

var (
	crdDir    string
	outputDir string
)

func init() {
	generateDeprecationWarningsCmd.Flags().StringVar(&crdDir, "crd-dir", "deploy/charts/dpf-operator/templates/crds", "Directory containing CRD YAML files")
	generateDeprecationWarningsCmd.Flags().StringVar(&outputDir, "output-dir", "deploy/charts/dpf-operator/templates/deprecation-warnings", "Directory to write generated VAP YAML files")
	rootCmd.AddCommand(generateDeprecationWarningsCmd)
}

var generateDeprecationWarningsCmd = &cobra.Command{
	Use:   "generate-deprecation-warnings",
	Short: "Generate ValidatingAdmissionPolicy resources for deprecated CRD fields",
	Long: `Scans CRD YAML files for fields with "Deprecated:" in their description and generates
ValidatingAdmissionPolicy + ValidatingAdmissionPolicyBinding resources that emit warnings
when users set deprecated fields.`,
	RunE: runGenerateDeprecationWarnings,
}

// crdDeprecations holds all deprecated fields for a single CRD.
type crdDeprecations struct {
	APIGroup string
	Resource string
	Fields   []deprecatedField
}

func runGenerateDeprecationWarnings(_ *cobra.Command, _ []string) error {
	// Read all CRD files.
	entries, err := os.ReadDir(crdDir)
	if err != nil {
		return fmt.Errorf("reading CRD directory %q: %w", crdDir, err)
	}

	var allCRDDeprecations []crdDeprecations
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		crd, err := loadCRD(filepath.Join(crdDir, entry.Name()))
		if err != nil {
			return fmt.Errorf("loading CRD %q: %w", entry.Name(), err)
		}

		fields := findDeprecatedFields(crd)
		if len(fields) == 0 {
			continue
		}

		allCRDDeprecations = append(allCRDDeprecations, crdDeprecations{
			APIGroup: crd.Spec.Group,
			Resource: crd.Spec.Names.Plural,
			Fields:   fields,
		})
	}

	// Sort for deterministic output.
	sort.Slice(allCRDDeprecations, func(i, j int) bool {
		return allCRDDeprecations[i].Resource+"."+allCRDDeprecations[i].APIGroup < allCRDDeprecations[j].Resource+"."+allCRDDeprecations[j].APIGroup
	})

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	// Generate VAP files.
	for _, crd := range allCRDDeprecations {
		filename := filepath.Join(outputDir, crd.Resource+"."+crd.APIGroup+".yaml")
		if err := writeVAPFile(filename, crd); err != nil {
			return fmt.Errorf("writing VAP for %s.%s: %w", crd.Resource, crd.APIGroup, err)
		}
		fmt.Printf("Generated: %s (%d deprecated fields)\n", filename, len(crd.Fields))
	}

	fmt.Printf("\nTotal: %d CRDs with deprecated fields\n", len(allCRDDeprecations))
	return nil
}

func loadCRD(path string) (*apiextensionsv1.CustomResourceDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	crd := &apiextensionsv1.CustomResourceDefinition{}
	if err := yaml.Unmarshal(data, crd); err != nil {
		return nil, err
	}
	return crd, nil
}

// findDeprecatedFields walks the CRD's OpenAPI v3 schema and returns all deprecated fields.
func findDeprecatedFields(crd *apiextensionsv1.CustomResourceDefinition) []deprecatedField {
	var fields []deprecatedField

	if len(crd.Spec.Versions) > 1 {
		panic("Only one APIVersion is supported currently, multiple APIVersions are not implemented yet")
	}
	if crd.Spec.Versions[0].Schema == nil || crd.Spec.Versions[0].Schema.OpenAPIV3Schema == nil {
		panic("CRD does not have a openAPI schema")
	}
	walkSchema(crd.Spec.Versions[0].Schema.OpenAPIV3Schema, nil, &fields)

	// Sort for deterministic output.
	sort.Slice(fields, func(i, j int) bool {
		return fields[i].jsonPath() < fields[j].jsonPath()
	})

	return fields
}

// walkSchema recursively walks a JSONSchemaProps and collects deprecated fields.
// segments tracks the structured path to the current position in the schema.
func walkSchema(schema *apiextensionsv1.JSONSchemaProps, segments []pathSegment, result *[]deprecatedField) {
	if schema == nil {
		return
	}

	for propName, propSchema := range schema.Properties {
		currentSegments := make([]pathSegment, len(segments)+1)
		copy(currentSegments, segments)
		currentSegments[len(segments)] = pathSegment{Name: propName}

		// Check if this field's description contains "Deprecated:"
		if isDeprecated(propSchema.Description) {
			// Skip status fields, users don't set them.
			if len(currentSegments) > 0 && currentSegments[0].Name == "status" {
				continue
			}

			*result = append(*result, deprecatedField{
				Segments:    currentSegments,
				Description: propSchema.Description,
			})
			// Skip sub-fields of a deprecated field, the parent deprecation covers the entire subtree.
			continue
		}

		// Recurse into nested object properties or array items.
		switch {
		case propSchema.Type == "array" && propSchema.Items != nil && propSchema.Items.Schema != nil:
			arraySegments := make([]pathSegment, len(currentSegments))
			copy(arraySegments, currentSegments)
			arraySegments[len(arraySegments)-1].IsArray = true
			walkSchema(propSchema.Items.Schema, arraySegments, result)
		case len(propSchema.Properties) > 0:
			walkSchema(&propSchema, currentSegments, result)
		}
	}
}

func isDeprecated(description string) bool {
	return strings.Contains(description, "Deprecated:")
}

// pathSegment represents one named segment in a JSON path.
// If IsArray is true, this segment is an array and iteration is needed in CEL.
type pathSegment struct {
	Name    string
	IsArray bool
}

// deprecatedField represents a deprecated field found in a CRD schema.
type deprecatedField struct {
	// Segments is the structured path to the deprecated field.
	// e.g. for spec.bar[].baz.fuzz[].old:
	// [{Name:"spec"}, {Name:"bar", IsArray:true}, {Name:"baz"}, {Name:"fuzz", IsArray:true}, {Name:"old"}]
	Segments []pathSegment
	// Description is the full description text of the field (containing the Deprecated: text)
	Description string
}

// celExpression returns the CEL expression for detecting this deprecated field is set.
func (d deprecatedField) celExpression() string {
	return buildCEL(d.Segments, "object", 0)
}

// buildCEL recursively builds a CEL expression for the given path segments.
// base is the current accessor (e.g. "object", "e0", "e1").
// depth tracks nesting level for generating unique iterator variable names.
func buildCEL(segments []pathSegment, base string, depth int) string {
	var clauses []string
	currentPath := base

	// At top level (depth=0), skip guard for first segment (e.g. "spec" is always present),
	// unless there is only one segment (then we must guard it).
	startGuardIdx := 0
	if depth == 0 && len(segments) > 1 {
		startGuardIdx = 1
	}

	for i, seg := range segments {
		currentPath = currentPath + "." + seg.Name

		if i >= startGuardIdx {
			clauses = append(clauses, fmt.Sprintf("!has(%s)", currentPath))
		}

		if seg.IsArray {
			// Unique CEL iterator variable per nesting level: e0, e1, e2, etc.
			varName := fmt.Sprintf("e%d", depth)
			inner := buildCEL(segments[i+1:], varName, depth+1)
			clauses = append(clauses, fmt.Sprintf("%s.all(%s, %s)", currentPath, varName, inner))
			return strings.Join(clauses, " || ")
		}
	}

	return strings.Join(clauses, " || ")
}

// jsonPath returns a human-readable path, e.g. "spec.dpuSets[].nodeSelector".
func (d deprecatedField) jsonPath() string {
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

// warningMessage returns a human-readable deprecation warning message.
func (d deprecatedField) warningMessage() string {
	// Extract the deprecation text: everything from "Deprecated:" onwards.
	idx := strings.Index(d.Description, "Deprecated:")
	if idx == -1 {
		return fmt.Sprintf("%s is deprecated.", d.jsonPath())
	}
	deprecationText := strings.TrimSpace(d.Description[idx+len("Deprecated:"):])
	// Clean up: collapse whitespace and trim.
	deprecationText = strings.Join(strings.Fields(deprecationText), " ")

	return fmt.Sprintf("%s is deprecated. %s", d.jsonPath(), deprecationText)
}

var vapTemplate = template.Must(template.New("vap").Parse(`{{- "{{-" }} if .Values.deprecationWarnings.enabled {{ "}}" }}
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicy
metadata:
  name: dpf-deprecation-{{ .Resource }}
  labels:
    app.kubernetes.io/part-of: dpf-operator-controller-manager
    dpu.nvidia.com/deprecation-warning: "true"
spec:
  failurePolicy: Ignore
  matchConstraints:
    resourceRules:
      - apiGroups: ["{{ .APIGroup }}"]
        apiVersions: ["*"]
        operations: ["CREATE", "UPDATE"]
        resources: ["{{ .Resource }}"]
  validations:
{{- range .Validations }}
    - expression: {{ printf "%q" .Expression }}
      message: >-
        {{ .Message }}
{{- end }}
---
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicyBinding
metadata:
  name: dpf-deprecation-{{ .Resource }}-binding
  labels:
    app.kubernetes.io/part-of: dpf-operator-controller-manager
    dpu.nvidia.com/deprecation-warning: "true"
spec:
  policyName: dpf-deprecation-{{ .Resource }}
  validationActions: [Warn]
{{ "{{-" }} end {{ "}}" }}
`))

type vapTemplateData struct {
	Resource    string
	APIGroup    string
	Validations []vapValidation
}

type vapValidation struct {
	Expression string
	Message    string
}

func writeVAPFile(filename string, crd crdDeprecations) error {
	var validations []vapValidation
	for _, f := range crd.Fields {
		validations = append(validations, vapValidation{
			Expression: f.celExpression(),
			Message:    f.warningMessage(),
		})
	}

	data := vapTemplateData{
		Resource:    crd.Resource,
		APIGroup:    crd.APIGroup,
		Validations: validations,
	}

	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	return vapTemplate.Execute(f, data)
}
