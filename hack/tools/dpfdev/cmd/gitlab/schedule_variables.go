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
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var scheduleVariablesShowValues bool

func init() {
	scheduleCmd.AddCommand(scheduleVariablesCmd)
	scheduleVariablesCmd.Flags().BoolVar(&scheduleVariablesShowValues, "show-values", false,
		"Print raw variable values, including any plaintext secrets (default: redact values whose key names a secret)")
}

var scheduleVariablesCmd = &cobra.Command{
	Use:   "variables <schedule-id>",
	Short: "Fetch the variables of a scheduled pipeline",
	Long: `Fetch the variables of a scheduled pipeline.

This works for schedules owned by other users without taking ownership, as
long as the token has the Maintainer role on the project. GitLab omits the
variables for tokens below Maintainer that do not own the schedule.`,
	Example: `  # Show the variables of schedule 12506
  dpfdev gitlab schedule variables 12506`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		scheduleID, err := parseScheduleID(args[0])
		if err != nil {
			return err
		}

		client, err := newGitLabClient(cmd)
		if err != nil {
			return err
		}

		schedule, err := client.GetPipelineSchedule(scheduleID)
		if err != nil {
			return fmt.Errorf("failed to fetch pipeline schedule %d: %v", scheduleID, err)
		}

		fmt.Printf("Schedule %d: %s (ref %s, owner %s)\n\n", schedule.ID, schedule.Description, schedule.Ref, schedule.Owner.Username)

		if schedule.Variables == nil {
			return fmt.Errorf("GitLab did not return the variables of schedule %d: the token needs the Maintainer role or must own the schedule", scheduleID)
		}
		if len(schedule.Variables) == 0 {
			fmt.Println("No variables defined.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "KEY\tTYPE\tVALUE")
		fmt.Fprintln(w, "---\t----\t-----")
		for _, variable := range schedule.Variables {
			value := variable.Value
			if !scheduleVariablesShowValues {
				value = redactValue(variable.Key, variable.Value)
			}
			fmt.Fprintf(w, "%s\t%s\t%s\n", variable.Key, variable.VariableType, value)
		}
		w.Flush()

		return nil
	},
}
