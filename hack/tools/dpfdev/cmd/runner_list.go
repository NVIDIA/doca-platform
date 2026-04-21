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
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/nvidia/doca-platform/hack/tools/dpfdev/pkg/config"
	"github.com/nvidia/doca-platform/hack/tools/dpfdev/pkg/gitlab"

	"github.com/spf13/cobra"
)

var (
	showAll      bool
	showOffline  bool
	filterByTag  string
	filterByType string
)

func init() {
	runnerCmd.AddCommand(runnerListCmd)

	// Add flags
	runnerListCmd.Flags().BoolVar(&showAll, "all", false, "Show all runners including inactive and paused")
	runnerListCmd.Flags().BoolVar(&showOffline, "offline", false, "Show only offline runners")
	runnerListCmd.Flags().StringVar(&filterByTag, "tag", "", "Filter runners by tag (contains match)")
	runnerListCmd.Flags().StringVar(&filterByType, "type", "", "Filter runners by type (instance_type, group_type, project_type)")
}

var runnerListCmd = &cobra.Command{
	Use:   "list",
	Short: "List GitLab CI runners",
	Long:  `List all GitLab CI runners for the configured project with filtering options`,
	Example: `  # List all active and online runners
  dpfdev runner list

  # List all runners including inactive and paused
  dpfdev runner list --all

  # List only offline runners
  dpfdev runner list --offline

  # List runners with a specific tag
  dpfdev runner list --tag "type/docker,colossus"

  # List runners by type
  dpfdev runner list --type project_type`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Load config
		cfg, err := config.LoadConfig(configFile)
		if err != nil {
			return fmt.Errorf("failed to load config: %v", err)
		}

		// Get GitLab token from environment
		token := os.Getenv("GITLAB_TOKEN")
		if token == "" {
			return fmt.Errorf("GITLAB_TOKEN environment variable is not set")
		}

		// Create GitLab client
		client := gitlab.NewClient(cfg.GitLab.Endpoint, token, cfg.GitLab.ProjectID)

		// Fetch runners
		fmt.Println("Fetching runners from GitLab. This may take a while...")
		runners, err := client.ListRunners(filterByTag, filterByType)
		if err != nil {
			return fmt.Errorf("failed to fetch runners: %v", err)
		}

		// Filter runners
		filteredRunners := filterRunners(runners)

		// Fetch detailed runner information and current job state
		runnerList := make([]runnerWithJob, 0, len(filteredRunners))
		for _, runner := range filteredRunners {
			runnerDetails, err := client.GetRunnerDetails(runner.ID)
			if err != nil {
				return fmt.Errorf("failed to fetch runner details: %v", err)
			}
			jobState := "idle"
			runningJobs, err := client.GetRunnerJobs(runner.ID, "running", 1)
			if err != nil {
				return fmt.Errorf("failed to fetch runner jobs: %v", err)
			}
			if len(runningJobs) > 0 {
				jobState = fmt.Sprintf("running: %s", runningJobs[0].Name)
			}
			runnerList = append(runnerList, runnerWithJob{Details: runnerDetails, Job: jobState})
		}

		// Sort runners by description (alphabetically, stable ordering)
		sort.Slice(runnerList, func(i, j int) bool {
			return runnerList[i].Details.Description < runnerList[j].Details.Description
		})

		// Print runners
		printRunners(runnerList)

		return nil
	},
}

// filterRunners applies the command-line filters to the runner list
func filterRunners(runners []gitlab.Runner) []gitlab.Runner {
	var filtered []gitlab.Runner

	for _, runner := range runners {
		if !showAll {
			if !runner.Active || runner.Paused {
				continue
			}
		}

		if showOffline && runner.Online {
			continue
		}

		if !runner.Online && !showOffline && !showAll {
			continue
		}

		filtered = append(filtered, runner)
	}

	return filtered
}

// runnerWithJob pairs runner details with its current job state.
type runnerWithJob struct {
	Details *gitlab.RunnerDetails
	Job     string
}

// printRunners prints the runner list in a formatted table
func printRunners(runners []runnerWithJob) {
	if len(runners) == 0 {
		fmt.Println("No runners found matching the criteria.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tDESCRIPTION\tSTATUS\tJOB\tONLINE\tACTIVE\tPAUSED\tTYPE\tTAGS")
	fmt.Fprintln(w, "--\t----\t-----------\t------\t---\t------\t------\t------\t----\t----")

	for _, entry := range runners {
		runner := entry.Details
		name := runner.Name
		if name == "" {
			name = "-"
		}

		description := runner.Description
		if description == "" {
			description = "-"
		}

		status := runner.Status
		if status == "" {
			status = "-"
		}

		tags := strings.Join(runner.TagList, ", ")
		if tags == "" {
			tags = "-"
		}

		runnerType := runner.RunnerType
		if runnerType == "" {
			runnerType = "-"
		}

		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%t\t%t\t%t\t%s\t%s\n",
			runner.ID,
			name,
			description,
			status,
			entry.Job,
			runner.Online,
			runner.Active,
			runner.Paused,
			runnerType,
			tags,
		)
	}

	w.Flush()
	fmt.Printf("\nTotal runners: %d\n", len(runners))
}
