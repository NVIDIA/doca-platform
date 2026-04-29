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
	"os"
	"os/signal"

	"github.com/nvidia/doca-platform/internal/dpfctl/sosreport"

	"github.com/spf13/cobra"
)

var sosreportCollectCmd = &cobra.Command{
	Use:   "collect",
	Short: "Start, wait, and download SOS reports in one step",
	Long: `Convenience command that orchestrates the full SOS report workflow:
start Jobs, wait for completion, download reports, and clean up.

This is equivalent to running 'start', polling 'status' until completion,
then running 'download'.`,
	Example: `  # Collect SOS reports from all clusters
  dpfctl sosreport collect --output-dir /tmp/sos-reports

  # Collect from host cluster only with a specific case ID
  dpfctl sosreport collect --target host --case-id CASE-12345 --output-dir /tmp/sos`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSOSReportCollect(cmd)
	},
}

func init() {
	sosreportCmd.AddCommand(sosreportCollectCmd)

	addStartFlags(sosreportCollectCmd)
	addDownloadFlags(sosreportCollectCmd)
	addArchiveFlags(sosreportCollectCmd)

	f := sosreportCollectCmd.Flags()
	f.BoolVar(&sosOpts.cleanup, "cleanup", true, "Clean up resources after download")
	must(sosreportCollectCmd.RegisterFlagCompletionFunc("cleanup", cobra.FixedCompletions([]string{"true", "false"}, cobra.ShellCompDirectiveNoFileComp)))
}

func runSOSReportCollect(cmd *cobra.Command) error {
	// Cancel on interrupt (Ctrl+C) — skip waiting and go straight to download.
	ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer cancel()

	return sosreport.Collect(ctx, sosreport.CollectOptions{
		StartOptions: *startOpts(),
		OutputDir:    sosOpts.outputDir,
	})
}
