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

package cmd

// Based on: https://github.com/kubernetes-sigs/crdify/blob/v0.4.0/cli/root.go

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime/schema"
	crconfig "sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/crdify/pkg/config"
	"sigs.k8s.io/crdify/pkg/loaders/composite"
	crdifyfile "sigs.k8s.io/crdify/pkg/loaders/file"
	"sigs.k8s.io/crdify/pkg/loaders/git"
	"sigs.k8s.io/crdify/pkg/loaders/kubernetes"
	"sigs.k8s.io/crdify/pkg/loaders/scheme"
	"sigs.k8s.io/crdify/pkg/runner"
	"sigs.k8s.io/crdify/pkg/validations"
	crdifyefr "sigs.k8s.io/crdify/pkg/validations/crd/existingfieldremoval"
)

const (
	removedFieldPrefix    = "removed field : "
	deprecationMarker     = "Deprecated:"
	typeChangedMarker     = "type changed : "
	emptyTypeMarker       = `-> ""`
	fieldSeparator        = " : "
	versionSeparator      = "^"
	validationDescription = "description"
	validationType        = "type"
)

func init() {
	loader := composite.NewComposite(
		map[string]composite.Loader{
			scheme.SchemeKubernetes: kubernetes.New(crconfig.GetConfig),
			scheme.SchemeFile:       crdifyfile.New(afero.OsFs{}),
			scheme.SchemeGit:        git.New(),
		},
	)

	var (
		crdifyConfigFile         string
		outputFormat             string
		allowRemovalDeprecations bool
		enableAllowList          bool
	)

	crdifyCmd := &cobra.Command{
		Use:   "crdify <old> <new>",
		Short: "crdify evaluates changes to Kubernetes CustomResourceDefinitions",
		Long: `crdify is a tool for evaluating changes to Kubernetes CustomResourceDefinitions
to help cluster administrators, gitops practitioners, and Kubernetes extension developers identify
changes that might result in a negative impact to clusters and/or users.

Example use cases:
    Ealuating a change in a CustomResourceDefinition on a Kubernetes Cluster with one in a file:
        $ crdify kube://{crd-name} file://{filepath}

    Evaluating a change from file to file:
        $ crdify file://{filepath} file://{filepath}

    Evaluating a change from git ref to git ref:
            $ crdify git://{ref}?path={filepath} git://{ref}?path={filepath}`,
		Args: cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			cfg, err := config.Load(crdifyConfigFile)
			if err != nil {
				log.Fatalf("loading config: %v", err)
			}

			run, err := runner.New(cfg, runner.DefaultRegistry())
			if err != nil {
				log.Fatalf("configuring validation runner: %v", err)
			}

			oldCrd, err := loader.Load(cmd.Context(), args[0])
			if err != nil {
				log.Fatalf("loading old CustomResourceDefinition: %v", err)
			}

			newCrd, err := loader.Load(cmd.Context(), args[1])
			if err != nil {
				log.Fatalf("loading new CustomResourceDefinition: %v", err)
			}

			results := run.Run(oldCrd, newCrd)
			if allowRemovalDeprecations {
				removeDeprecations(results)
			}
			if enableAllowList {
				if err := removeAllowedErrors(cfg, results, newCrd.Spec.Group, newCrd.Spec.Names.Kind); err != nil {
					log.Fatalf("removing allowed errors: %v", err)
				}
			}

			report, err := results.Render(runner.Format(outputFormat))
			if err != nil {
				// TODO: can we handle this better than spitting out an obtuse error?
				log.Fatalf("rendering run results: %v", err)
			}

			fmt.Print(report)
			if results.HasFailures() {
				os.Exit(1)
			}
		},
	}

	crdifyCmd.PersistentFlags().StringVar(&crdifyConfigFile, "config", "", "the filepath to load the check configurations from")
	crdifyCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", "plaintext", "the format the output should take when incompatibilities are identified. May be one of plaintext, markdown, json, yaml")
	crdifyCmd.PersistentFlags().BoolVar(&allowRemovalDeprecations, "allow-removal-deprecations", false, "if true, crdify will allow field removals that have been marked as deprecated in the same version.")
	crdifyCmd.PersistentFlags().BoolVar(&enableAllowList, "enable-allow-list", false, "if true, crdify will enable the allow list feature to ignore known issues.")

	rootCmd.AddCommand(crdifyCmd)
}

type allowList struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Field      string `json:"field"`
	Error      string `json:"error"`
}

func removeAllowedErrors(cfg *config.Config, r *runner.Results, groupName, kindName string) error {
	for _, validation := range cfg.Validations {
		rawList, ok := getAllowList(validation.Configuration)
		if !ok {
			continue
		}

		for _, raw := range rawList {
			item, err := extractAllowList(raw)
			if err != nil {
				return fmt.Errorf("extracting allowList item: %w", err)
			}

			gv, err := schema.ParseGroupVersion(item.APIVersion)
			if err != nil {
				return err
			}

			if gv.Group != groupName || item.Kind != kindName {
				continue
			}

			sameVersionResults, ok := r.SameVersionValidation[gv.Version]
			if !ok {
				continue
			}
			removeAllowedErrorFromResult(sameVersionResults, item)
		}
	}
	return nil
}

func getAllowList(cfg map[string]interface{}) ([]interface{}, bool) {
	if cfg == nil {
		return nil, false
	}
	raw, ok := cfg["allowList"]
	if !ok {
		return nil, false
	}
	list, ok := raw.([]interface{})
	return list, ok
}

func extractAllowList(i interface{}) (*allowList, error) {
	item := &allowList{}
	// Convert item to allowList using JSON marshaling/unmarshaling
	itemMap, ok := i.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid allowList item format")
	}

	// Marshal the map to JSON, then unmarshal to struct
	jsonData, err := json.Marshal(itemMap)
	if err != nil {
		return nil, err
	}

	if err = json.Unmarshal(jsonData, &item); err != nil {
		return nil, err
	}
	return item, nil
}

func removeAllowedErrorFromResult(sameVersionResults map[string][]validations.ComparisonResult, item *allowList) {
	for field, comparisonResults := range sameVersionResults {
		if field != versionSeparator+item.Field {
			continue
		}
		for i := range comparisonResults {
			comparisonResults[i].Errors = filterErrors(comparisonResults[i].Errors, func(err string) bool {
				return err != item.Error
			})
		}
		deleteResultsWithoutErrors(sameVersionResults, comparisonResults, field)
	}
}

func deleteResultsWithoutErrors(sameVersionResults map[string][]validations.ComparisonResult, results []validations.ComparisonResult, field string) {
	// Remove the field from results if there are no more active errors.
	hasActiveErrors := false
	for _, result := range results {
		if len(result.Errors) > 0 {
			hasActiveErrors = true
			break
		}
	}
	if !hasActiveErrors {
		delete(sameVersionResults, field)
	}
}

// removeDeprecations processes validation results to identify and handle potential field deprecations.
// It loops over all Warnings and looks for the deprecation marker. If found, it removes related
// errors from the results, effectively treating them as safe removals.
func removeDeprecations(r *runner.Results) *runner.Results {
	if r == nil {
		return r
	}

	// Find the existing field removal validation result
	var efrResult *validations.ComparisonResult
	for i, validation := range r.CRDValidation {
		if validation.Name == (&crdifyefr.ExistingFieldRemoval{}).Name() {
			efrResult = &r.CRDValidation[i]
			break
		}
	}

	if efrResult == nil || len(efrResult.Errors) == 0 {
		return r
	}

	// Extract potential deprecations from removed field errors
	potentialDeprecations := make(map[string][]string)
	for _, errorMsg := range efrResult.Errors {
		if !strings.HasPrefix(errorMsg, removedFieldPrefix) {
			continue
		}

		parts := strings.SplitN(errorMsg, fieldSeparator, 2)
		if len(parts) != 2 {
			continue
		}

		fieldParts := strings.SplitN(parts[1], versionSeparator, 2)
		if len(fieldParts) != 2 {
			continue
		}

		// We have to remove the trailing dot from the apiVersion to match how fields are stored in the results.
		apiVersion := strings.TrimRight(strings.TrimSpace(fieldParts[0]), ".")
		// Add the leading dot to the field to match how fields are stored in the results.
		field := versionSeparator + strings.TrimSpace(fieldParts[1])

		if potentialDeprecations[apiVersion] == nil {
			potentialDeprecations[apiVersion] = make([]string, 0)
		}
		potentialDeprecations[apiVersion] = append(potentialDeprecations[apiVersion], field)
	}

	// Process deprecation removal for each API version
	for apiVersion, fields := range potentialDeprecations {
		if results, exists := r.SameVersionValidation[apiVersion]; exists {
			removeDeprecationFromResults(efrResult, apiVersion, results, fields)
		}
	}

	return r
}

// removeDeprecationFromResults removes deprecation-related validation errors for specified fields.
func removeDeprecationFromResults(
	efrResult *validations.ComparisonResult,
	apiVersion string,
	sameVersionResults map[string][]validations.ComparisonResult,
	fields []string,
) {
	for _, field := range fields {
		comparisonResults, exists := sameVersionResults[field]
		if !exists {
			continue
		}

		if !deprecationFound(comparisonResults) {
			continue
		}

		// Process type errors if deprecation was found.
		// This is necessary to remove the type changed from "type" to "" error as it has been removed.
		for i, result := range comparisonResults {
			if result.Name != validationType {
				continue
			}
			comparisonResults[i].Errors = filterErrors(comparisonResults[i].Errors, func(err string) bool {
				return !strings.Contains(err, typeChangedMarker) && !strings.Contains(err, emptyTypeMarker)
			})
		}

		// Remove the field removal error from EFR result
		expectedError := fmt.Sprintf("%s%v.%v", removedFieldPrefix, apiVersion, field)
		for i, errorMsg := range efrResult.Errors {
			if errorMsg == expectedError {
				efrResult.Errors = append(efrResult.Errors[:i], efrResult.Errors[i+1:]...)
				break
			}
		}

		deleteResultsWithoutErrors(sameVersionResults, comparisonResults, field)
	}
}

func deprecationFound(comparisonResults []validations.ComparisonResult) bool {
	found := false
	for _, result := range comparisonResults {
		if result.Name != validationDescription {
			continue
		}
		for j := range result.Warnings {
			if strings.Contains(result.Warnings[j], deprecationMarker) {
				found = true
				break
			}
		}
	}
	return found
}

// filterErrors returns a new slice containing only the errors that satisfy the keep function.
func filterErrors(errors []string, keep func(string) bool) []string {
	if len(errors) == 0 {
		return errors
	}
	filtered := make([]string, 0, len(errors))
	for _, err := range errors {
		if keep(err) {
			filtered = append(filtered, err)
		}
	}
	return filtered
}
