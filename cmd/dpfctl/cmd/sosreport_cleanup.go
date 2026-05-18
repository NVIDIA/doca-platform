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
	"github.com/nvidia/doca-platform/internal/dpfctl/sosreport"

	"github.com/spf13/cobra"
)

var sosreportCleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove all SOS report resources (Jobs, pods, secrets)",
	Long: `Clean up all resources created by 'dpfctl sosreport start'.

This deletes all SOS report Jobs (including running ones), their pods,
kubeconfig secrets, and download pods. Use this to abort in-progress
collections or clean up after a completed workflow.`,
	Example: `  # Clean up all SOS report resources
  dpfctl sosreport cleanup

  # Clean up only host cluster resources
  dpfctl sosreport cleanup --target host

  # Clean up resources for a specific case ID
  dpfctl sosreport cleanup --case-id CASE-12345`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateSOSReportCaseID(); err != nil {
			return err
		}

		targets, err := getTargets(cmd.Context())
		if err != nil {
			return err
		}
		defer targets.Close()

		sosreport.Step("Cleaning up SOS report resources")
		n := sosreport.Cleanup(cmd.Context(), targets, sosOpts.namespace, sosOpts.caseID)
		if n == 0 {
			sosreport.Info("No SOS report resources found")
		} else {
			sosreport.Result("Cleaned up %d Job(s) and associated resources", n)
		}
		return nil
	},
}

func init() {
	sosreportCmd.AddCommand(sosreportCleanupCmd)
}
