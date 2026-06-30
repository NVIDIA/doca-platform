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
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/nvidia/doca-platform/hack/tools/dpfdev/pkg/config"
	"github.com/nvidia/doca-platform/hack/tools/dpfdev/pkg/gitlab"

	"github.com/spf13/cobra"
	"golang.org/x/term"
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
	printRunnersTo(os.Stdout, runners, colorEnabled())
}

// printRunnersTo prints the runner list in a formatted table. color controls
// whether ANSI status badges are included, which is useful for CI logs and tests.
func printRunnersTo(out io.Writer, runners []runnerWithJob, color bool) {
	if len(runners) == 0 {
		fmt.Fprintln(out, "No runners found matching the criteria.")
		return
	}

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tDESCRIPTION\tSTATUS\tJOB\tTAGS")
	fmt.Fprintln(w, "--\t-----------\t------\t---\t----")

	for _, entry := range runners {
		runner := entry.Details
		description := runner.Description
		if description == "" {
			description = "-"
		}

		tags := strings.Join(runner.TagList, ", ")
		if tags == "" {
			tags = "-"
		}

		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n",
			runner.ID,
			description,
			runnerStatusBadge(runner, color),
			jobStatusBadge(entry.Job, color),
			tags,
		)
	}

	w.Flush()
	fmt.Fprintf(out, "\nTotal runners: %d\n", len(runners))
}

const (
	ansiReset  = "\033[0m"
	ansiRed    = "\033[31m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiCyan   = "\033[36m"
)

func colorEnabled() bool {
	_, present := os.LookupEnv("NO_COLOR")
	return colorEnabledForFD(int(os.Stdout.Fd()), present)
}

func colorEnabledForFD(fd int, noColorPresent bool) bool {
	return !noColorPresent && term.IsTerminal(fd)
}

func runnerStatusBadge(runner *gitlab.RunnerDetails, color bool) string {
	text := "● ONLINE"
	ansiColor := ansiGreen

	switch {
	case !runner.Active:
		text = "○ INACTIVE"
		ansiColor = ansiRed
	case runner.Paused:
		text = "Ⅱ PAUSED"
		ansiColor = ansiYellow
	case !runner.Online:
		text = "● OFFLINE"
		ansiColor = ansiRed
	}

	if !color {
		return text
	}
	return ansiColor + text + ansiReset
}

func jobStatusBadge(job string, color bool) string {
	if job == "idle" {
		if color {
			return ansiCyan + "• IDLE" + ansiReset
		}
		return "• IDLE"
	}

	text := "▶ " + strings.TrimPrefix(job, "running: ")
	if color {
		return ansiYellow + text + ansiReset
	}
	return text
}
