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
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

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
		// Collect defaults to cleanup=true, so always treat it as explicit.
		cleanupExplicit = true
		return runSOSReportCollect(cmd.Context())
	},
}

func init() {
	sosreportCmd.AddCommand(sosreportCollectCmd)

	f := sosreportCollectCmd.Flags()
	f.StringVar(&sosOpts.caseID, "case-id", "", "Case ID for the SOS report (default: dpf-<timestamp>)")
	f.StringVar(&sosOpts.nfsServer, "nfs-server", "", "NFS server address (enables NFS output mode)")
	f.StringVar(&sosOpts.nfsPath, "nfs-path", "", "NFS export path (must exist on the NFS server)")
	f.BoolVar(&sosOpts.nfsNoSub, "nfs-no-subdir", false, "Write directly to --nfs-path without creating a subdirectory")
	must(sosreportCollectCmd.RegisterFlagCompletionFunc("nfs-no-subdir", cobra.FixedCompletions([]string{"true", "false"}, cobra.ShellCompDirectiveNoFileComp)))

	f.DurationVar(&sosOpts.timeout, "timeout", 30*time.Minute, "Job active deadline timeout")
	must(sosreportCollectCmd.RegisterFlagCompletionFunc("timeout", cobra.FixedCompletions([]string{"5m", "15m", "30m", "1h"}, cobra.ShellCompDirectiveNoFileComp)))

	f.StringVar(&sosOpts.outputDir, "output-dir", "", "Local directory for downloaded reports (default: sosreport-<timestamp>)")
	must(sosreportCollectCmd.RegisterFlagCompletionFunc("output-dir", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return nil, cobra.ShellCompDirectiveFilterDirs
	}))

	f.BoolVar(&sosOpts.cleanup, "cleanup", true, "Clean up resources after download")
	must(sosreportCollectCmd.RegisterFlagCompletionFunc("cleanup", cobra.FixedCompletions([]string{"true", "false"}, cobra.ShellCompDirectiveNoFileComp)))

	f.BoolVar(&sosOpts.archive, "archive", false, "Create a .tar.gz archive of all downloaded reports")
	must(sosreportCollectCmd.RegisterFlagCompletionFunc("archive", cobra.FixedCompletions([]string{"true", "false"}, cobra.ShellCompDirectiveNoFileComp)))
}

func runSOSReportCollect(ctx context.Context) error {
	opts := startOpts()
	if err := sosreport.ValidateStartOptions(opts); err != nil {
		return err
	}

	// Cancel on interrupt (Ctrl+C) — skip waiting and go straight to download.
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt)
	defer cancel()

	targets, err := getTargets(ctx)
	if err != nil {
		return err
	}
	defer targets.Close()

	// Step 1: Start
	hostClient, err := sosreport.GetHostClient(targets)
	if err != nil {
		return fmt.Errorf("get host client: %w", err)
	}
	if err := sosreport.Start(ctx, targets, hostClient, *opts); err != nil {
		if ctx.Err() != nil {
			sosreport.Warn("Interrupted during start")
			return fmt.Errorf("interrupted")
		}
		return fmt.Errorf("start failed: %w", err)
	}

	// Step 2: Wait
	sosreport.Step("Waiting for SOS report Jobs to complete")
	if err := sosreport.WaitForAll(ctx, targets, sosOpts.namespace, opts.CaseID, sosOpts.timeout); err != nil {
		if ctx.Err() != nil {
			sosreport.Warn("Interrupted")
		} else {
			sosreport.Warn("%v", err)
		}
	}

	// Step 3: For NFS mode, just clean up. For local mode, download then prompt for cleanup.
	if opts.Output == sosreport.OutputNFS {
		sosreport.Step("Cleaning up")
		sosreport.Info("NFS output mode: reports were written to the configured NFS path")
		sosreport.Cleanup(context.Background(), targets, sosOpts.namespace, opts.CaseID)
		return nil
	}

	// Download — skip the cleanup prompt, collect handles cleanup explicitly.
	cleanupExplicit = true
	sosOpts.cleanup = false

	sosreport.Step("Downloading SOS reports")
	dlCtx, dlCancel := context.WithTimeout(ctx, 10*time.Minute)
	defer dlCancel()
	if err := runSOSReportDownload(dlCtx); err != nil {
		sosreport.Warn("Download failed: %v", err)
		sosreport.Warn("Skipping cleanup — resources preserved for retry via 'dpfctl sosreport download'")
		return fmt.Errorf("download failed: %w", err)
	}

	// Always clean up after collect — reuse existing targets.
	sosreport.Step("Cleaning up")
	sosreport.Cleanup(context.Background(), targets, sosOpts.namespace, opts.CaseID)
	return nil
}
