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
	"time"

	"github.com/nvidia/doca-platform/hack/tools/dpfdev/pkg/config"
	"github.com/nvidia/doca-platform/hack/tools/dpfdev/pkg/gitlab"

	"github.com/spf13/cobra"
)

var (
	runnerID int
	lastJobs int
)

const (
	defaultLastJobs = 10
)

func init() {
	runnerCmd.AddCommand(runnerJobShowCmd)

	// Add flags
	runnerJobShowCmd.Flags().IntVar(&runnerID, "id", 0, "Runner ID to show jobs for")
	runnerJobShowCmd.Flags().IntVar(&lastJobs, "last-jobs", 0, "Show last N jobs for the runner (requires --id)")
}

var runnerJobShowCmd = &cobra.Command{
	Use:   "job-show",
	Short: "Show current or recent jobs running on GitLab CI runners",
	Long:  `Display the current job running on each runner, or show recent jobs for a specific runner.`,
	Example: `  # Show current job on all runners
  dpfdev runner job-show

  # Show last jobs on a specific runner (default: 10 jobs)
  dpfdev runner job-show --id 12345

  # Show last N jobs on a specific runner
  dpfdev runner job-show --id 12345 --last-jobs 15`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Validate flags
		if lastJobs > 0 && runnerID == 0 {
			return fmt.Errorf("--last-jobs requires --id to be specified")
		}

		if runnerID > 0 && lastJobs == 0 {
			lastJobs = defaultLastJobs
		}

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

		if lastJobs > 0 {
			// Mode 1: Show last N jobs for specific runner
			return showLastJobs(client, runnerID, lastJobs)
		} else {
			// Mode 2: Show current job for all runners
			return showCurrentJobForAllRunners(client)
		}
	},
}

// showCurrentJobForAllRunners shows the current running job for each runner
func showCurrentJobForAllRunners(client *gitlab.Client) error {
	fmt.Println("Fetching runners...")
	runners, err := client.ListRunners("", "")
	if err != nil {
		return fmt.Errorf("failed to fetch runners: %v", err)
	}

	sort.Slice(runners, func(i, j int) bool {
		return runners[i].Description < runners[j].Description
	})

	fmt.Printf("Checking current jobs for %d runners...\n\n", len(runners))

	// Project-side running jobs by runner; names jobs on shared runners we do
	// not own (see gitlab.Client.ListProjectRunningJobs).
	projectJobs := runningJobsByRunner(client)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "RUNNER ID\tDESCRIPTION\tSTATUS\tJOB NAME\tBRANCH\tJOB REF")
	fmt.Fprintln(w, "---------\t-----------\t------\t--------\t------\t-------")

	for _, runner := range runners {
		description := runner.Description
		if description == "" {
			description = "-"
		}

		state, name, branch, ref := "IDLE", "-", "-", "-"
		switch st, job := runnerJobInfo(client, runner, projectJobs); st {
		case "running":
			state, name, branch, ref = "RUNNING", job.Name, job.Branch, job.URL
		case "busy":
			// Busy, but the job runs in a project not accessible with this token.
			state, name = "RUNNING", "(no access)"
		case "unknown":
			state = "N/A"
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\n", runner.ID, description, state, name, branch, ref)
	}

	w.Flush()
	return nil
}

// showLastJobs shows the last N jobs for a specific runner
func showLastJobs(client *gitlab.Client, runnerID int, limit int) error {
	// Get runner details
	runner, err := client.GetRunnerDetails(runnerID)
	if err != nil {
		return fmt.Errorf("failed to fetch runner details: %v", err)
	}

	// Get last N jobs for this runner.
	jobs, err := client.GetRunnerJobs(runnerID, "", limit)
	if err != nil {
		if !strings.Contains(err.Error(), "status 403") {
			return fmt.Errorf("failed to fetch jobs: %v", err)
		}
		// The runner-scoped jobs endpoint gates on runner ownership before
		// checking per-job visibility, so a group Maintainer gets 403 on shared
		// runners even when every job in the history is visible to them. Fall
		// back to the project side, which a Maintainer can read: it recovers
		// this project's jobs on the runner, just not cross-project history.
		return showRunnerJobsFromProject(client, runnerID, runner.Description, limit, err)
	}

	fmt.Printf("Last %d jobs for Runner %d (%s):\n\n", limit, runnerID, runner.Description)

	if len(jobs) == 0 {
		fmt.Println("No jobs found for this runner.")
		return nil
	}

	printJobsTable(jobs)
	return nil
}

// showRunnerJobsFromProject is the fallback used when the runner-scoped jobs
// endpoint is forbidden (a group Maintainer inspecting a shared runner it does
// not own). It reconstructs the runner's recent job history from the project
// side, which a Maintainer can read. Only this project's jobs on the runner are
// visible this way, not cross-project history.
func showRunnerJobsFromProject(client *gitlab.Client, runnerID int, description string, limit int, origErr error) error {
	jobs, err := client.ListProjectJobsForRunner(runnerID, limit)
	if err != nil {
		return fmt.Errorf("failed to fetch jobs: %v (project-side fallback also failed: %v)", origErr, err)
	}

	if len(jobs) == 0 {
		// Nothing of ours on this runner recently. Report whatever the aggregate
		// status endpoint can still tell us, without claiming it is idle.
		status, statusErr := client.GetRunnerJobExecutionStatus(runnerID)
		state := "UNKNOWN"
		switch {
		case status == "active":
			state = "RUNNING (a job in a project not accessible with this token)"
		case statusErr == nil && status != "":
			state = "IDLE"
		}
		fmt.Printf("No recent jobs of this project on Runner %d (%s); cross-project history requires the runner's group Owner role.\n", runnerID, description)
		fmt.Printf("Current status: %s\n", state)
		return nil
	}

	fmt.Printf("Last %d jobs for Runner %d (%s) in this project (cross-project history requires the runner's group Owner role):\n\n", len(jobs), runnerID, description)
	printJobsTable(jobs)
	return nil
}

// printJobsTable renders a table of jobs.
func printJobsTable(jobs []gitlab.Job) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "JOB ID\tJOB NAME\tSTATUS\tBRANCH\tCREATED\tDURATION\tJOB REF")
	fmt.Fprintln(w, "------\t--------\t------\t------\t------\t-------\t--------")

	for _, job := range jobs {
		duration := "-"
		if job.Duration > 0 {
			// Convert seconds (float64) to time.Duration for nice formatting
			d := time.Duration(job.Duration * float64(time.Second))
			if d > time.Second {
				// trim it down to the nearest second
				d = d.Round(time.Second)
			}
			duration = d.String()
		}

		createdAt := job.CreatedAt.UTC().Format("2006-01-02 15:04 MST")

		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
			job.ID,
			job.Name,
			job.Status,
			job.Branch,
			createdAt,
			duration,
			job.URL,
		)
	}

	w.Flush()
	fmt.Printf("\nTotal jobs: %d\n", len(jobs))
}
