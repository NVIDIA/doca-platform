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
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/nvidia/doca-platform/hack/tools/dpfdev/pkg/gitlab"

	"github.com/spf13/cobra"
)

var (
	scheduleListAll bool
)

func init() {
	scheduleCmd.AddCommand(scheduleListCmd)

	scheduleListCmd.Flags().BoolVar(&scheduleListAll, "all", false, "Show inactive schedules as well")
}

var scheduleListCmd = &cobra.Command{
	Use:   "list",
	Short: "List GitLab scheduled pipelines",
	Long:  `List the scheduled pipelines of the configured project.`,
	Example: `  # List active schedules
  dpfdev gitlab schedule list

  # List all schedules including inactive ones
  dpfdev gitlab schedule list --all`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newGitLabClient(cmd)
		if err != nil {
			return err
		}

		progress("Fetching schedules from GitLab...")
		schedules, err := client.ListPipelineSchedules()
		if err != nil {
			return fmt.Errorf("failed to fetch pipeline schedules: %v", err)
		}

		if !scheduleListAll {
			var active []gitlab.PipelineSchedule
			for _, schedule := range schedules {
				if schedule.Active {
					active = append(active, schedule)
				}
			}
			schedules = active
		}

		sort.Slice(schedules, func(i, j int) bool {
			return schedules[i].ID < schedules[j].ID
		})

		printSchedules(os.Stdout, schedules)

		return nil
	},
}

// printSchedules prints the schedule list in a formatted table.
func printSchedules(out io.Writer, schedules []gitlab.PipelineSchedule) {
	if len(schedules) == 0 {
		fmt.Fprintln(out, "No pipeline schedules found.")
		return
	}

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tDESCRIPTION\tREF\tCRON\tSTATUS\tOWNER\tNEXT RUN")
	fmt.Fprintln(w, "--\t-----------\t---\t----\t------\t-----\t--------")

	for _, schedule := range schedules {
		status := "active"
		if !schedule.Active {
			status = "inactive"
		}

		nextRun := "-"
		if schedule.Active && !schedule.NextRunAt.IsZero() {
			nextRun = schedule.NextRunAt.Format("2006-01-02 15:04 MST")
		}

		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
			schedule.ID,
			schedule.Description,
			strings.TrimPrefix(schedule.Ref, "refs/heads/"),
			schedule.Cron,
			status,
			schedule.Owner.Username,
			nextRun,
		)
	}

	w.Flush()
	fmt.Fprintf(out, "\nTotal schedules: %d\n", len(schedules))
}
