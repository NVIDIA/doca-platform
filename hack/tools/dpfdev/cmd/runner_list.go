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

		// Project-side running jobs by runner; names jobs on shared runners we
		// do not own (see gitlab.Client.ListProjectRunningJobs).
		projectJobs := runningJobsByRunner(client)

		// Fetch detailed runner information and current job state. The per-runner
		// endpoints can 403 for a Maintainer on group runners, so they are
		// best-effort enrichment over the list data rather than fatal.
		runnerList := make([]runnerWithJob, 0, len(filteredRunners))
		for _, runner := range filteredRunners {
			details := runnerDetailsFromRunner(runner)
			if full, err := client.GetRunnerDetails(runner.ID); err == nil {
				details = full
			}

			runnerList = append(runnerList, runnerWithJob{Details: details, Job: runnerJobState(client, runner, projectJobs)})
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

// runnerJobClient is the subset of the GitLab client used to determine a
// runner's job state. It is an interface so the logic can be unit tested.
type runnerJobClient interface {
	GetRunnerJobExecutionStatus(runnerID int) (string, error)
	GetRunnerJobs(runnerID int, status string, limit int) ([]gitlab.Job, error)
}

// runnerJobInfo determines a runner's job state and, when the running job is
// visible to the caller, returns it. States: "running" (job populated), "busy"
// (running a job the caller cannot see), "idle", and "unknown" (GraphQL status
// unavailable on a shared runner, whose idle state cannot be trusted).
//
// The GraphQL jobExecutionStatus is the busy/idle source of truth: unlike the
// job listings it is readable even for shared runners the caller does not own
// (see GetRunnerJobExecutionStatus). The job itself comes from the prefetched
// project jobs, falling back to the runner-scoped endpoint for runners we own.
func runnerJobInfo(client runnerJobClient, runner gitlab.Runner, projectJobs map[int]gitlab.Job) (string, gitlab.Job) {
	if job, ok := projectJobs[runner.ID]; ok {
		return "running", job
	}

	status, err := client.GetRunnerJobExecutionStatus(runner.ID)
	statusKnown := err == nil && status != ""
	if statusKnown && status != "active" {
		return "idle", gitlab.Job{}
	}

	// Active or status unavailable: the runner-scoped endpoint may still have
	// the job (it works for runners we own).
	if jobs, err := client.GetRunnerJobs(runner.ID, "running", 1); err == nil && len(jobs) > 0 {
		return "running", jobs[0]
	}

	switch {
	case statusKnown: // active, but the job is not visible to this token
		return "busy", gitlab.Job{}
	case isSharedRunner(runner):
		return "unknown", gitlab.Job{}
	}
	return "idle", gitlab.Job{}
}

// runnerJobState renders runnerJobInfo's result as the JOB column value.
func runnerJobState(client runnerJobClient, runner gitlab.Runner, projectJobs map[int]gitlab.Job) string {
	state, job := runnerJobInfo(client, runner, projectJobs)
	if state == "running" {
		return "running: " + job.Name
	}
	return state
}

// runningJobsByRunner fetches the configured project's running jobs and indexes
// them by the runner executing them. It is best-effort: an error yields an empty
// map, and job-naming simply degrades to the runner-scoped lookup.
func runningJobsByRunner(client *gitlab.Client) map[int]gitlab.Job {
	byRunner := map[int]gitlab.Job{}
	jobs, err := client.ListProjectRunningJobs()
	if err != nil {
		return byRunner
	}
	for _, job := range jobs {
		if job.Runner != nil {
			byRunner[job.Runner.ID] = job
		}
	}
	return byRunner
}

// isSharedRunner reports whether the runner is shared beyond a single project,
// i.e. an instance or group runner. Job visibility for these runners is scoped
// to projects the caller can access (Reporter role or above), so a non-owner
// caller cannot see jobs the runner is executing for projects it is not a
// member of. Their reported "idle" state therefore cannot be trusted: only
// project runners report a job state we can rely on.
func isSharedRunner(r gitlab.Runner) bool {
	return r.IsShared || r.RunnerType == "instance_type" || r.RunnerType == "group_type"
}

// runnerDetailsFromRunner builds a RunnerDetails from the entry returned by the
// project runners list. It is used as a fallback when the per-runner details
// endpoint is not accessible (e.g. group runners for a project Maintainer).
// Tags and project associations are unavailable in this case and are left empty.
func runnerDetailsFromRunner(r gitlab.Runner) *gitlab.RunnerDetails {
	return &gitlab.RunnerDetails{
		Active:      r.Active,
		Paused:      r.Paused,
		Description: r.Description,
		ID:          r.ID,
		IPAddress:   r.IPAddress,
		IsShared:    r.IsShared,
		RunnerType:  r.RunnerType,
		Name:        r.Name,
		Online:      r.Online,
		Status:      r.Status,
	}
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
	ansiGray   = "\033[90m"
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
	text := "▶ " + strings.TrimPrefix(job, "running: ")
	ansiColor := ansiYellow

	switch job {
	case "idle":
		text, ansiColor = "• IDLE", ansiCyan
	case "unknown":
		text, ansiColor = "• N/A", ansiGray
	case "busy":
		// Running a job whose name is not visible to the caller.
		text = "▶ RUNNING"
	}

	if !color {
		return text
	}
	return ansiColor + text + ansiReset
}
