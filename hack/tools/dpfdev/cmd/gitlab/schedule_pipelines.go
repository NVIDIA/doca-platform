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

package gitlab

import (
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/nvidia/doca-platform/hack/tools/dpfdev/pkg/gitlab"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var schedulePipelinesDays int

func init() {
	scheduleCmd.AddCommand(schedulePipelinesCmd)
	schedulePipelinesCmd.Flags().IntVar(&schedulePipelinesDays, "days", 7, "Only show pipelines created in the last N days (0 for all)")
}

var schedulePipelinesCmd = &cobra.Command{
	Use:   "pipelines <schedule-id|description>",
	Short: "List the pipelines triggered by a scheduled pipeline",
	Long: `List the pipelines triggered by a scheduled pipeline over the last N days.

The argument is a schedule ID or the schedule description. A description
matches case-insensitively as a substring, so every matching schedule is
listed.`,
	Example: `  # Pipelines triggered by schedule 12506 in the last 7 days
  dpfdev gitlab schedule pipelines 12506

  # Last 14 days of the nightly release schedule
  dpfdev gitlab schedule pipelines "release-v26.11: Nightly release" --days 14`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if schedulePipelinesDays < 0 {
			return fmt.Errorf("invalid --days: %d (must be >= 0)", schedulePipelinesDays)
		}

		client, err := newGitLabClient(cmd)
		if err != nil {
			return err
		}

		progress("Fetching schedules from GitLab...")
		schedules, err := client.ListPipelineSchedules()
		if err != nil {
			return fmt.Errorf("failed to fetch pipeline schedules: %v", err)
		}

		matches := matchSchedules(schedules, args[0])
		if len(matches) == 0 {
			return fmt.Errorf("no pipeline schedule matches %q", args[0])
		}

		var since time.Time
		if schedulePipelinesDays > 0 {
			since = time.Now().AddDate(0, 0, -schedulePipelinesDays)
		}

		for _, schedule := range matches {
			progress("Fetching pipelines of schedule %d...", schedule.ID)
			pipelines, err := client.ListSchedulePipelines(schedule.ID, since)
			if err != nil {
				return fmt.Errorf("failed to fetch pipelines of schedule %d: %v", schedule.ID, err)
			}
			printSchedulePipelines(os.Stdout, schedule, pipelines, pipelineNotes(client, pipelines), colorEnabled())
		}

		return nil
	},
}

// pipelineNotes maps a pipeline ID to a note for pipelines whose plain status
// hides a problem. A pipeline whose allowed-to-fail trigger job failed reports
// success and "passed with warnings", so the failed downstream pipelines are
// named. Lookup errors are noted rather than fatal.
func pipelineNotes(client *gitlab.Client, pipelines []gitlab.SchedulePipeline) map[int]string {
	notes := map[int]string{}
	for _, pipeline := range pipelines {
		if pipeline.Status != "success" {
			continue
		}
		details, err := client.GetPipeline(pipeline.ID)
		if err != nil {
			notes[pipeline.ID] = fmt.Sprintf("details unavailable: %v", err)
			continue
		}
		if details.DetailedStatus.Label == "" || details.DetailedStatus.Label == "passed" {
			continue
		}
		note := details.DetailedStatus.Label
		bridges, err := client.ListPipelineBridges(pipeline.ID)
		if err != nil {
			notes[pipeline.ID] = fmt.Sprintf("%s (bridges unavailable: %v)", note, err)
			continue
		}
		for _, bridge := range bridges {
			if bridge.DownstreamPipeline != nil && bridge.DownstreamPipeline.Status == "failed" {
				note += fmt.Sprintf(", %s: downstream pipeline %d failed", bridge.Name, bridge.DownstreamPipeline.ID)
			} else if bridge.Status == "failed" {
				note += fmt.Sprintf(", %s failed", bridge.Name)
			}
		}
		notes[pipeline.ID] = note
	}
	return notes
}

// matchSchedules returns the schedule with the given numeric ID, or else the
// schedules whose description equals arg, or else contains it, ignoring case.
func matchSchedules(schedules []gitlab.PipelineSchedule, arg string) []gitlab.PipelineSchedule {
	if id, err := parseScheduleID(arg); err == nil {
		for _, schedule := range schedules {
			if schedule.ID == id {
				return []gitlab.PipelineSchedule{schedule}
			}
		}
		return nil
	}

	var exact, partial []gitlab.PipelineSchedule
	for _, schedule := range schedules {
		switch {
		case strings.EqualFold(schedule.Description, arg):
			exact = append(exact, schedule)
		case strings.Contains(strings.ToLower(schedule.Description), strings.ToLower(arg)):
			partial = append(partial, schedule)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return partial
}

const (
	ansiReset  = "\033[0m"
	ansiRed    = "\033[31m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiCyan   = "\033[36m"
	ansiGray   = "\033[90m"
)

// colorEnabled reports whether stdout is a terminal and NO_COLOR is unset.
func colorEnabled() bool {
	_, noColor := os.LookupEnv("NO_COLOR")
	return !noColor && term.IsTerminal(int(os.Stdout.Fd()))
}

// paint wraps text in the ANSI color when color is enabled. All colors have
// the same escape length so tabwriter keeps the columns aligned.
func paint(text, ansiColor string, color bool) string {
	if !color || text == "" {
		return text
	}
	return ansiColor + text + ansiReset
}

func pipelineStatusColor(status string) string {
	switch status {
	case "success":
		return ansiGreen
	case "failed":
		return ansiRed
	case "running":
		return ansiCyan
	case "pending", "created", "waiting_for_resource", "preparing":
		return ansiYellow
	default:
		return ansiGray
	}
}

// printSchedulePipelines prints the pipelines of one schedule in a table.
// color controls whether the status and notes carry ANSI colors.
func printSchedulePipelines(out io.Writer, schedule gitlab.PipelineSchedule, pipelines []gitlab.SchedulePipeline, notes map[int]string, color bool) {
	fmt.Fprintf(out, "\nSchedule %d: %s (ref %s)\n", schedule.ID, schedule.Description, strings.TrimPrefix(schedule.Ref, "refs/heads/"))
	if len(pipelines) == 0 {
		fmt.Fprintln(out, "No pipelines found.")
		return
	}

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PIPELINE\tSTATUS\tCREATED\tSHA\tURL\tNOTES")
	fmt.Fprintln(w, "--------\t------\t-------\t---\t---\t-----")
	for _, pipeline := range pipelines {
		sha := pipeline.SHA
		if len(sha) > 8 {
			sha = sha[:8]
		}
		status, statusColor := pipeline.Status, pipelineStatusColor(pipeline.Status)
		if notes[pipeline.ID] != "" {
			status, statusColor = status+" ⚠", ansiYellow
		}

		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\n",
			pipeline.ID,
			paint(status, statusColor, color),
			pipeline.CreatedAt.UTC().Format("2006-01-02 15:04 MST"),
			sha,
			pipeline.WebURL,
			paint(notes[pipeline.ID], ansiYellow, color),
		)
	}
	w.Flush()
	fmt.Fprintf(out, "\nTotal pipelines: %d\n", len(pipelines))
}
