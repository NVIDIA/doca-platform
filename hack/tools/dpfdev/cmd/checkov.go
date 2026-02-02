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
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/util/yaml"
)

// This tool is a helper for analyzing Checkov scan results to exclude and filter specific security findings for helm charts.
// Checkov itself is python based which is why it has to be executed separately and the results have to be passed to this tool.

func init() {
	var (
		checkovConfigurationFile string
		chartName                string
		report                   string
	)

	checkovCmd := &cobra.Command{
		Use:   "checkov --config <config file> --chart-name <chart name> --report <path	or - for stdin>",
		Short: "Filter and report security findings from checkov JSON output",
		Long: `checkov processes JSON output from the checkov security scanner via stdin to identify findings that need attention.
It filters out known issues defined in a configuration file's exclusion list, helping teams focus on
new security and compliance issues that require remediation.

Example use cases:
    Analyzing a checkov report with exclusions with a report from stdin:
        $ cat report.json | dpfdev checkov --config .checkov.yaml --chart-name dpf-operator

    Analyzing a checkov report with exclusions with a report from a file:
        $ cat report.json | dpfdev checkov --config .checkov.yaml --chart-name dpf-operator --report some-report.json

    Using in a CI/CD pipeline to validate Helm charts:
        $ checkov -d charts/my-chart --framework helm --output json | dpfdev checkov --config .checkov.yaml --chart-name dpf-operator`,
		Args: cobra.ExactArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			// Get the exclusions for the given chart from the configuration file.
			chartExclusions, err := newCheckovExclusions(checkovConfigurationFile, chartName)
			if err != nil {
				log.Fatalf("loading checkov exclusions: %v", err)
			}

			// Get the checkov report from the given path or stdin.
			checkovReport, err := newCheckovReport(report)
			if err != nil {
				log.Fatalf("reading checkov report: %v", err)
			}

			// Analyze the checkov report and collect the failures and unused exclusions.
			failures, unusedExclusions := checkovReport.analyze(chartExclusions)

			// Print the failures if there are any.
			if len(failures) > 0 {
				fmt.Printf("# findings for helm chart %s (fix or add an exclusion to the configuration file):\n", chartName)
				for _, failure := range failures {
					fmt.Printf("%s\n", failure)
				}
			}

			// Print the unused exclusions if there are any.
			if len(unusedExclusions) > 0 {
				fmt.Printf("# unused exclusions for helm chart %s (remove from configuration file):\n", chartName)
				for _, exclusion := range unusedExclusions {
					fmt.Printf("%s\n", exclusion)
				}
			}

			if len(failures) > 0 || len(unusedExclusions) > 0 {
				os.Exit(1)
			}
		},
	}
	checkovCmd.PersistentFlags().StringVar(&checkovConfigurationFile, "config", "", "the filepath to load the check configurations from")
	cobra.MarkFlagRequired(checkovCmd.PersistentFlags(), "config")
	checkovCmd.PersistentFlags().StringVar(&chartName, "chart-name", "", "the name of the chart to load exclusions from the configuration file")
	cobra.MarkFlagRequired(checkovCmd.PersistentFlags(), "chart-name")
	checkovCmd.PersistentFlags().StringVar(&report, "report", "-", "the path to the checkov report, using \"-\" leads to read from stdin")

	rootCmd.AddCommand(checkovCmd)
}

func newCheckovExclusions(configFile, chartName string) (CheckovExclusions, error) {
	// Load and parse the configuration file to get the exclusions for the given chart.
	configBytes, err := os.ReadFile(configFile)
	if err != nil {
		return nil, fmt.Errorf("reading config file %s: %w", configFile, err)
	}

	checkovConfig := CheckovConfig{}
	err = yaml.Unmarshal(configBytes, &checkovConfig)
	if err != nil {
		return nil, fmt.Errorf("unmarshalling config file %s: %w", configFile, err)
	}

	return checkovConfig.Exclusions[chartName], nil
}

func newCheckovReport(report string) (*CheckovReport, error) {
	// Read the checkov report and parse it.
	checkovReportBytes, err := readCheckovReport(report)
	if err != nil {
		return nil, fmt.Errorf("reading checkov report from %s: %w", report, err)
	}

	if len(checkovReportBytes) == 0 {
		return nil, fmt.Errorf("expected checkov report input to not be empty")
	}

	checkovReport := &CheckovReport{}
	err = json.Unmarshal(checkovReportBytes, checkovReport)
	if err != nil {
		return nil, fmt.Errorf("unmarshalling checkov report from %s: %w", report, err)
	}

	return checkovReport, nil
}

func readCheckovReport(report string) ([]byte, error) {
	if report == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(report)
}

// CheckovFailure is the data struct used in the reports created by running checkov.
type CheckovFailure struct {
	CheckID       string   `json:"check_id"`
	Resource      string   `json:"resource"`
	EvaluatedKeys []string `json:"evaluated_keys"`

	description string
}

func (c CheckovFailure) String() string {
	description := ""
	if c.description != "" {
		description = fmt.Sprintf(" # %s", c.description)
	}
	s := fmt.Sprintf("- check_id: %s%s\n"+
		"  resource: %s",
		c.CheckID, description, c.Resource)

	if len(c.EvaluatedKeys) > 0 {
		s += fmt.Sprintf("\n  evaluated_keys:\n    - %s", strings.Join(c.EvaluatedKeys, "\n    - "))
	}

	return s
}

type CheckovReport struct {
	Results CheckovResults `json:"results"`
}

func (r *CheckovReport) analyze(chartExclusions CheckovExclusions) (failures []CheckovFailure, unusedExclusions []*CheckovExclusion) {
	// Collect the reported failures that are not excluded.
	for _, failure := range r.Results.FailedChecks {
		if chartExclusions.isExcluded(failure) {
			continue
		}
		failures = append(failures, CheckovFailure{
			CheckID:       failure.CheckID,
			description:   failure.CheckName,
			Resource:      failure.Resource,
			EvaluatedKeys: failure.CheckResult.EvaluatedKeys,
		})
	}

	// Collect the unused exclusions. They can be removed from the configuration file.
	for _, exclusion := range chartExclusions {
		if exclusion.used {
			continue
		}
		unusedExclusions = append(unusedExclusions, exclusion)
	}

	// Sort the slices to have a deterministic output.
	sort.SliceStable(failures, func(i, j int) bool {
		if failures[i].Resource < failures[j].Resource {
			return true
		}
		return failures[i].CheckID < failures[j].CheckID
	})
	sort.SliceStable(unusedExclusions, func(i, j int) bool {
		if unusedExclusions[i].Resource < unusedExclusions[j].Resource {
			return true
		}
		return unusedExclusions[i].CheckID < unusedExclusions[j].CheckID
	})

	return failures, unusedExclusions
}

type CheckovResults struct {
	FailedChecks []CheckovFailedCheck `json:"failed_checks"`
}

type CheckovFailedCheck struct {
	CheckID     string             `json:"check_id"`
	CheckName   string             `json:"check_name"`
	CheckResult CheckovCheckResult `json:"check_result"`
	FilePath    string             `json:"file_path"`
	Resource    string             `json:"resource"`
	Guideline   string             `json:"guideline"`
}

type CheckovCheckResult struct {
	Result        string   `json:"result"`
	EvaluatedKeys []string `json:"evaluated_keys"`
}

// CheckovConfig represents the configuration for this tool to define exclusions.
type CheckovConfig struct {
	// Exclusions are checkov failures which should not lead to a failure.
	Exclusions map[string]CheckovExclusions `json:"exclusions"`
}

// CheckovExclusions is a list of exclusions for checkov reports.
type CheckovExclusions []*CheckovExclusion

// CheckovExclusion is a exclusion for a checkov finding with a found bool to help tracking if the exclusion has been found during analyzation.
type CheckovExclusion struct {
	CheckovFailure
	used bool
}

func (e CheckovExclusions) isExcluded(failedCheck CheckovFailedCheck) bool {
	for i, exclusion := range e {
		if exclusion.CheckID != failedCheck.CheckID || exclusion.Resource != failedCheck.Resource {
			continue
		}
		if slices.Compare(exclusion.EvaluatedKeys, failedCheck.CheckResult.EvaluatedKeys) != 0 {
			continue
		}

		// Mark the exclusion as found to later on determine if it should not be dropped from the exclusion configuration.
		e[i].used = true
		return true
	}

	return false
}
