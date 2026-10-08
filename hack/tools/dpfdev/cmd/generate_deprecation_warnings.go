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
	"bytes"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"text/template"

	"github.com/nvidia/doca-platform/pkg/deprecation"

	"github.com/spf13/cobra"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

var (
	crdDir           string
	outputDir        string
	goOutputFile     string
	rbacGoOutputFile string
)

func init() {
	generateDeprecationWarningsCmd.Flags().StringVar(&crdDir, "crd-dir", "deploy/charts/dpf-operator/templates/crds", "Directory containing CRD YAML files")
	generateDeprecationWarningsCmd.Flags().StringVar(&outputDir, "output-dir", "deploy/charts/dpf-operator/templates/deprecation-warnings", "Directory to write generated VAP YAML files")
	generateDeprecationWarningsCmd.Flags().StringVar(&goOutputFile, "go-output-file", "pkg/deprecation/zz_generated_deprecated_fields.go", "Path to write the generated Go source listing deprecated fields per GVK")
	generateDeprecationWarningsCmd.Flags().StringVar(&rbacGoOutputFile, "rbac-go-output-file", "internal/operator/controllers/zz_generated_deprecated_fields_rbac.go", "Path to write the generated Go source declaring the kubebuilder RBAC marker the deprecated-fields scanner needs")
	rootCmd.AddCommand(generateDeprecationWarningsCmd)
}

var generateDeprecationWarningsCmd = &cobra.Command{
	Use:   "generate-deprecation-warnings",
	Short: "Generate ValidatingAdmissionPolicy resources and a static Go list for deprecated CRD fields",
	Long: `Scans CRD YAML files for fields with "Deprecated:" in their description and generates:
  - ValidatingAdmissionPolicy + ValidatingAdmissionPolicyBinding resources that emit warnings
    when users set deprecated fields.
  - A Go source file (pkg/deprecation/zz_generated_deprecated_fields.go by default) listing every deprecated
    field per GVK, so the runtime DeprecatedFieldsNotInUse scanner doesn't need to discover CRDs or
    walk their schemas itself.
  - A Go source file (internal/operator/controllers/zz_generated_deprecated_fields_rbac.go by
    default) declaring the +kubebuilder:rbac marker the scanner needs, covering every API group
    that has a deprecated field, so that marker can never drift out of sync with the list above.`,
	RunE: runGenerateDeprecationWarnings,
}

// crdDeprecations holds all deprecated fields for a single CRD.
type crdDeprecations struct {
	APIGroup string
	Version  string
	Kind     string
	ListKind string
	Resource string
	Fields   []deprecation.DeprecatedField
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

		if len(crd.Spec.Versions) > 1 {
			return fmt.Errorf("CRD %q: only one APIVersion is supported currently, multiple APIVersions are not implemented yet", entry.Name())
		}
		if len(crd.Spec.Versions) == 0 || crd.Spec.Versions[0].Schema == nil || crd.Spec.Versions[0].Schema.OpenAPIV3Schema == nil {
			panic(fmt.Sprintf("CRD %q does not have an openAPI schema", entry.Name()))
		}

		fields := findDeprecatedFields(crd)
		if len(fields) == 0 {
			continue
		}

		versionName, ok := storageVersionName(crd)
		if !ok {
			panic(fmt.Sprintf("CRD %q has no storage version", entry.Name()))
		}

		allCRDDeprecations = append(allCRDDeprecations, crdDeprecations{
			APIGroup: crd.Spec.Group,
			Version:  versionName,
			Kind:     crd.Spec.Names.Kind,
			ListKind: crd.Spec.Names.ListKind,
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

	if err := writeGoFile(goOutputFile, allCRDDeprecations); err != nil {
		return fmt.Errorf("writing Go deprecations file %q: %w", goOutputFile, err)
	}
	fmt.Printf("Generated: %s\n", goOutputFile)

	if err := writeRBACGoFile(rbacGoOutputFile, allCRDDeprecations); err != nil {
		return fmt.Errorf("writing RBAC Go file %q: %w", rbacGoOutputFile, err)
	}
	fmt.Printf("Generated: %s\n", rbacGoOutputFile)

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
			Expression: celExpression(f),
			Message:    warningMessage(f),
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

// licenseHeader is the Apache license boilerplate shared by every generated Go file's header.
const licenseHeader = `/*
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

// Code generated by dpfdev generate-deprecation-warnings. DO NOT EDIT.
`

var goFileTemplate = template.Must(template.New("go").Parse(licenseHeader + `
package deprecation

import "k8s.io/apimachinery/pkg/runtime/schema"

// KnownDeprecations lists every deprecated field found across the DPF CRDs at build time.
var KnownDeprecations = []GVKDeprecations{
{{- range . }}
	{
		GVK:      schema.GroupVersionKind{Group: {{ printf "%q" .APIGroup }}, Version: {{ printf "%q" .Version }}, Kind: {{ printf "%q" .Kind }}},
		ListKind: {{ printf "%q" .ListKind }},
		Fields: []DeprecatedField{
{{- range .Fields }}
			{
				Segments: []PathSegment{ {{- range $i, $seg := .Segments }}{{ if $i }}, {{ end }}{Name: {{ printf "%q" $seg.Name }}{{ if $seg.IsArray }}, IsArray: true{{ end }}}{{ end }} },
				Description: {{ printf "%q" .Description }},
			},
{{- end }}
		},
	},
{{- end }}
}
`))

// writeGoFile emits a Go source file declaring KnownDeprecations, a static list of every
// deprecated field per GVK. This lets the runtime scanner (internal/operator/controllers)
// iterate a fixed, build-time list instead of discovering CRDs and walking their schemas itself.
func writeGoFile(filename string, allCRDDeprecations []crdDeprecations) error {
	return writeFormattedGoFile(filename, goFileTemplate, allCRDDeprecations)
}

var rbacGoFileTemplate = template.Must(template.New("rbac").Parse(licenseHeader + `
package controller

// These markers declare the RBAC the DeprecatedFieldsNotInUse scanner (reconcileDeprecatedFieldsUsage
// in deprecated_fields.go) needs: list+get on exactly the resources that currently have a
// deprecated field, grouped one marker per API group. They are generated from the same CRD scan as
// pkg/deprecation.KnownDeprecations, so they can never drift out of sync, and are deliberately
// independent of any other controller's RBAC markers.
{{- range . }}
// +kubebuilder:rbac:groups={{ .Group }},resources={{ .Resources }},verbs=list;get
{{- end }}
`))

type rbacMarker struct {
	Group     string
	Resources string
}

// writeRBACGoFile emits a Go source file declaring one +kubebuilder:rbac marker per API group with
// a deprecated field, scoped to exactly the resource plurals that group needs (not a wildcard), so
// the scanner's RBAC can never silently drift out of sync with deprecation.KnownDeprecations the
// way a hand-written marker could, and never grants more than what's actually scanned.
func writeRBACGoFile(filename string, allCRDDeprecations []crdDeprecations) error {
	resourcesByGroup := map[string][]string{}
	for _, crd := range allCRDDeprecations {
		resourcesByGroup[crd.APIGroup] = append(resourcesByGroup[crd.APIGroup], crd.Resource)
	}

	markers := make([]rbacMarker, 0, len(resourcesByGroup))
	for group, resources := range resourcesByGroup {
		sort.Strings(resources)
		markers = append(markers, rbacMarker{Group: group, Resources: strings.Join(slices.Compact(resources), ";")})
	}
	sort.Slice(markers, func(i, j int) bool { return markers[i].Group < markers[j].Group })

	return writeFormattedGoFile(filename, rbacGoFileTemplate, markers)
}

// writeFormattedGoFile renders tmpl with data, gofmts the result and writes it to filename.
func writeFormattedGoFile(filename string, tmpl *template.Template, data any) error {
	var b bytes.Buffer
	if err := tmpl.Execute(&b, data); err != nil {
		return fmt.Errorf("executing template: %w", err)
	}

	formatted, err := format.Source(b.Bytes())
	if err != nil {
		return fmt.Errorf("formatting generated source: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	return os.WriteFile(filename, formatted, 0o644)
}

// storageVersionName returns the name of crd's storage version, or false if it has none.
func storageVersionName(crd *apiextensionsv1.CustomResourceDefinition) (string, bool) {
	for _, version := range crd.Spec.Versions {
		if version.Storage {
			return version.Name, true
		}
	}
	return "", false
}

// findDeprecatedFields walks the CRD's storage-version OpenAPI v3 schema and returns all
// deprecated fields. Returns nil if the CRD has no versions or no schema.
func findDeprecatedFields(crd *apiextensionsv1.CustomResourceDefinition) []deprecation.DeprecatedField {
	var fields []deprecation.DeprecatedField

	for _, version := range crd.Spec.Versions {
		if !version.Storage {
			continue
		}
		if version.Schema == nil || version.Schema.OpenAPIV3Schema == nil {
			return nil
		}
		walkSchema(version.Schema.OpenAPIV3Schema, nil, &fields)
		break
	}

	sort.Slice(fields, func(i, j int) bool {
		return fields[i].JSONPath() < fields[j].JSONPath()
	})

	return fields
}

// walkSchema recursively walks a JSONSchemaProps and collects deprecated fields.
// segments tracks the structured path to the current position in the schema.
func walkSchema(schema *apiextensionsv1.JSONSchemaProps, segments []deprecation.PathSegment, result *[]deprecation.DeprecatedField) {
	if schema == nil {
		return
	}

	for propName, propSchema := range schema.Properties {
		currentSegments := make([]deprecation.PathSegment, len(segments)+1)
		copy(currentSegments, segments)
		currentSegments[len(segments)] = deprecation.PathSegment{Name: propName}

		// Check if this field's description contains "Deprecated:"
		if isDeprecated(propSchema.Description) {
			// Skip status fields, users don't set them.
			if len(currentSegments) > 0 && currentSegments[0].Name == "status" {
				continue
			}

			*result = append(*result, deprecation.DeprecatedField{
				Segments:    currentSegments,
				Description: propSchema.Description,
			})
			// Skip sub-fields of a deprecated field, the parent deprecation covers the entire subtree.
			continue
		}

		// Recurse into nested object properties or array items.
		switch {
		case propSchema.Type == "array" && propSchema.Items != nil && propSchema.Items.Schema != nil:
			arraySegments := make([]deprecation.PathSegment, len(currentSegments))
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

// celExpression returns the CEL expression for detecting whether d is set.
func celExpression(d deprecation.DeprecatedField) string {
	return buildCEL(d.Segments, "object", 0)
}

// buildCEL recursively builds a CEL expression for the given path segments.
// base is the current accessor (e.g. "object", "e0", "e1").
// depth tracks nesting level for generating unique iterator variable names.
func buildCEL(segments []deprecation.PathSegment, base string, depth int) string {
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

// warningMessage returns a human-readable deprecation warning message for d.
func warningMessage(d deprecation.DeprecatedField) string {
	// Extract the deprecation text: everything from "Deprecated:" onwards.
	idx := strings.Index(d.Description, "Deprecated:")
	if idx == -1 {
		return fmt.Sprintf("%s is deprecated.", d.JSONPath())
	}
	deprecationText := strings.TrimSpace(d.Description[idx+len("Deprecated:"):])
	// Clean up: collapse whitespace and trim.
	deprecationText = strings.Join(strings.Fields(deprecationText), " ")

	return fmt.Sprintf("%s is deprecated. %s", d.JSONPath(), deprecationText)
}
