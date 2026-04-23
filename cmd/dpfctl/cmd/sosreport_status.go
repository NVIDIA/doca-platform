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
	"time"

	"github.com/nvidia/doca-platform/internal/dpfctl/sosreport"

	"github.com/spf13/cobra"
)

var statusWatch bool
var statusInterval int

var sosreportStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show status of SOS report Jobs",
	Long:  `List all SOS report Jobs and display their current status.`,
	Example: `  # Show status of all SOS report Jobs
  dpfctl sosreport status

  # Show status for a specific target
  dpfctl sosreport status --target host

  # Show status for a specific case ID
  dpfctl sosreport status --case-id CASE-12345

  # Watch status with continuous refresh
  dpfctl sosreport status -w

  # Watch with a custom refresh interval (seconds)
  dpfctl sosreport status -w -i 10`,
	RunE: func(cmd *cobra.Command, args []string) error {
		targets, err := getTargets(cmd.Context())
		if err != nil {
			return err
		}
		defer targets.Close()

		if statusWatch {
			return sosreport.WatchStatus(cmd.Context(), targets, sosOpts.namespace, sosOpts.caseID, time.Duration(statusInterval)*time.Second)
		}
		return sosreport.Status(cmd.Context(), targets, sosOpts.namespace, sosOpts.caseID)
	},
}

func init() {
	sosreportCmd.AddCommand(sosreportStatusCmd)

	f := sosreportStatusCmd.Flags()
	f.StringVar(&sosOpts.caseID, "case-id", "", "Filter by case ID (default: show all)")

	f.BoolVarP(&statusWatch, "watch", "w", false, "Watch status with continuous refresh")
	must(sosreportStatusCmd.RegisterFlagCompletionFunc("watch", cobra.FixedCompletions([]string{"true", "false"}, cobra.ShellCompDirectiveNoFileComp)))

	f.IntVarP(&statusInterval, "interval", "i", 5, "Refresh interval in seconds when using --watch")
	must(sosreportStatusCmd.RegisterFlagCompletionFunc("interval", cobra.FixedCompletions([]string{"1", "2", "5", "10", "30"}, cobra.ShellCompDirectiveNoFileComp)))
}
