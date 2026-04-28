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

	addStartFlags(sosreportCollectCmd)
	addDownloadFlags(sosreportCollectCmd)
	addArchiveFlags(sosreportCollectCmd)

	f := sosreportCollectCmd.Flags()
	f.BoolVar(&sosOpts.cleanup, "cleanup", true, "Clean up resources after download")
	must(sosreportCollectCmd.RegisterFlagCompletionFunc("cleanup", cobra.FixedCompletions([]string{"true", "false"}, cobra.ShellCompDirectiveNoFileComp)))
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
	startedTargets, err := sosreport.Start(ctx, targets, hostClient, *opts)
	if err != nil {
		if ctx.Err() != nil {
			sosreport.Warn("Interrupted during start")
			return fmt.Errorf("interrupted")
		}
		return fmt.Errorf("start failed: %w", err)
	}

	// Step 2: Wait
	sosreport.Step("Waiting for SOS report Jobs to complete")
	if err := sosreport.WaitForAll(ctx, startedTargets, sosOpts.namespace, opts.CaseID, sosOpts.timeout); err != nil {
		if ctx.Err() != nil {
			sosreport.Warn("Interrupted")
		} else {
			sosreport.Warn("%v", err)
		}
	}

	// Step 3: For NFS mode, just clean up. For local mode, download then prompt for cleanup.
	if opts.Output == sosreport.OutputNFS {
		if opts.NFSSubDir != "" {
			if sosOpts.archiveOnly {
				sosreport.Result("Archive created on NFS: %s/%s.tar.gz", opts.NFSPath, opts.NFSSubDir)
			} else {
				sosreport.Result("Reports written to NFS: %s/%s", opts.NFSPath, opts.NFSSubDir)
				if sosOpts.archive {
					sosreport.Result("Archive created on NFS: %s/%s.tar.gz", opts.NFSPath, opts.NFSSubDir)
				}
			}
		} else {
			sosreport.Result("Reports written to NFS: %s", opts.NFSPath)
		}
		sosreport.Step("Cleaning up")
		sosreport.Cleanup(context.Background(), startedTargets, sosOpts.namespace, opts.CaseID)
		return nil
	}

	// Download — skip the cleanup prompt, collect handles cleanup explicitly.
	cleanupExplicit = true
	sosOpts.cleanup = false

	// Use a fresh context — the interrupt context (ctx) may already be
	// canceled if the user pressed Ctrl+C during the wait phase. We still
	// want to attempt downloading whatever reports are ready.
	if ctx.Err() != nil {
		sosreport.Warn("Wait was interrupted. Proceeding to download all available reports; results may be incomplete")
	}
	sosreport.Step("Downloading SOS reports")
	dlCtx, dlCancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer dlCancel()
	if err := runSOSReportDownload(dlCtx); err != nil {
		sosreport.Warn("Download failed: %v", err)
		sosreport.Warn("Skipping cleanup — resources preserved for retry via 'dpfctl sosreport download'")
		return fmt.Errorf("download failed: %w", err)
	}

	// Always clean up after collect — reuse existing startedTargets.
	sosreport.Step("Cleaning up")
	sosreport.Cleanup(context.Background(), startedTargets, sosOpts.namespace, opts.CaseID)
	return nil
}
