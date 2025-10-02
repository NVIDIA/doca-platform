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

import (
	"context"
	"strings"
	"testing"

	"github.com/spf13/afero"
	crconfig "sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/crdify/pkg/config"
	"sigs.k8s.io/crdify/pkg/loaders/composite"
	crdifyfile "sigs.k8s.io/crdify/pkg/loaders/file"
	"sigs.k8s.io/crdify/pkg/loaders/git"
	"sigs.k8s.io/crdify/pkg/loaders/kubernetes"
	"sigs.k8s.io/crdify/pkg/loaders/scheme"
	"sigs.k8s.io/crdify/pkg/runner"
)

func TestCrdifyValidation(t *testing.T) {
	tests := []struct {
		name                    string
		oldCrd                  string
		newCrd                  string
		configFile              string
		allowDeprecation        bool
		enableAllowList         bool
		expectedErrorMessages   []string
		unexpectedErrorMessages []string
	}{
		{
			name:             "Remove field without both flags --allow-removal-deprecations --enable-allow-list",
			oldCrd:           "file://test/crdify_base.yaml",
			newCrd:           "file://test/crdify_valid.yaml",
			configFile:       "test/crdify_config.yaml",
			allowDeprecation: false,
			enableAllowList:  false,
			expectedErrorMessages: []string{
				"removed field : v1.^.spec.size",
				`type changed : "string" -> ""`,
				"required fields: [color]",
			},
		},
		{
			name:                  "Remove field with deprecation is allowed",
			oldCrd:                "file://test/crdify_base.yaml",
			newCrd:                "file://test/crdify_valid.yaml",
			configFile:            "test/crdify_config.yaml",
			allowDeprecation:      true,
			enableAllowList:       true,
			expectedErrorMessages: []string{},
		},
		{
			name:             "Remove non-deprecated field not allowed",
			oldCrd:           "file://test/crdify_base.yaml",
			newCrd:           "file://test/crdify_fail.yaml",
			configFile:       "test/crdify_config.yaml",
			allowDeprecation: true,
			enableAllowList:  true,
			expectedErrorMessages: []string{
				"removed field : v1.^.spec.color",
				`type changed : "string" -> ""`,
				"required fields: [size]",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Run CLI. Pretty the same as in crdify.go main().
			loader := composite.NewComposite(
				map[string]composite.Loader{
					scheme.SchemeKubernetes: kubernetes.New(crconfig.GetConfig),
					scheme.SchemeFile:       crdifyfile.New(afero.OsFs{}),
					scheme.SchemeGit:        git.New(),
				},
			)

			cfg, err := config.Load(tt.configFile)
			if err != nil {
				t.Fatalf("loading config: %v", err)
			}

			run, err := runner.New(cfg, runner.DefaultRegistry())
			if err != nil {
				t.Fatalf("configuring validation runner: %v", err)
			}

			oldCrd, err := loader.Load(context.TODO(), tt.oldCrd)
			if err != nil {
				t.Fatalf("loading old CustomResourceDefinition: %v", err)
			}

			newCrd, err := loader.Load(context.TODO(), tt.newCrd)
			if err != nil {
				t.Fatalf("loading new CustomResourceDefinition: %v", err)
			}

			results := run.Run(oldCrd, newCrd)
			if tt.allowDeprecation {
				removeDeprecations(results)
			}
			if tt.enableAllowList {
				if err := removeAllowedErrors(cfg, results, newCrd.Spec.Group, newCrd.Spec.Names.Kind); err != nil {
					t.Fatalf("removing allowed errors: %v", err)
				}
			}
			// End CLI run.

			var allErrors []string
			for _, validation := range results.CRDValidation {
				allErrors = append(allErrors, validation.Errors...)
			}

			for _, versionResults := range results.SameVersionValidation {
				for _, validationList := range versionResults {
					for _, validation := range validationList {
						allErrors = append(allErrors, validation.Errors...)
					}
				}
			}

			totalErrors := len(allErrors)

			validateErrorMessages(t, allErrors, tt.expectedErrorMessages, tt.unexpectedErrorMessages)

			t.Logf("Validation results: %d errors, HasFailures: %v",
				totalErrors, results.HasFailures())
			t.Logf("All errors: %v", allErrors)
		})
	}
}

// validateErrorMessages checks that all expected messages are present and no unexpected messages are present
func validateErrorMessages(t *testing.T, actualMessages []string, expectedMessages []string, unexpectedMessages []string) {
	for _, expected := range expectedMessages {
		found := false
		for _, actual := range actualMessages {
			if strings.Contains(actual, expected) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected Error message containing '%s' not found. Actual Errors: %v",
				expected, actualMessages)
		}
	}

	for _, unexpected := range unexpectedMessages {
		for _, actual := range actualMessages {
			if strings.Contains(actual, unexpected) {
				t.Errorf("Unexpected Error message containing '%s' found: '%s'",
					unexpected, actual)
			}
		}
	}
}
