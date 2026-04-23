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
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nvidia/doca-platform/internal/dpfctl/sosreport"

	"github.com/spf13/cobra"
)

// cleanupExplicit tracks whether --cleanup was explicitly set on the command line.
// When false, the download command will interactively prompt the user.
// The collect command sets this to true to skip the prompt since it manages
// cleanup itself (always cleans up after a successful download).
var cleanupExplicit bool

var sosreportDownloadCmd = &cobra.Command{
	Use:   "download",
	Short: "Download completed SOS reports to local disk",
	Long: `Download SOS reports from completed Jobs to a local directory.

For local mode, downloads from the running Job pod after sosreport completes.`,
	Example: `  # Download all completed reports to current directory
  dpfctl sosreport download

  # Download to a specific directory
  dpfctl sosreport download --output-dir /tmp/sos-reports

  # Download and skip cleanup (keep pods/secrets)
  dpfctl sosreport download --cleanup=false

  # Download and create a single archive for ticket attachment
  dpfctl sosreport download --archive`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cleanupExplicit = cmd.Flags().Changed("cleanup")
		return runSOSReportDownload(cmd.Context())
	},
}

func init() {
	sosreportCmd.AddCommand(sosreportDownloadCmd)

	f := sosreportDownloadCmd.Flags()
	f.StringVar(&sosOpts.outputDir, "output-dir", "", "Local directory for downloaded reports (default: sosreport-<timestamp>)")
	must(sosreportDownloadCmd.RegisterFlagCompletionFunc("output-dir", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return nil, cobra.ShellCompDirectiveFilterDirs
	}))

	f.BoolVar(&sosOpts.cleanup, "cleanup", false, "Clean up resources after download (omit to be prompted)")
	must(sosreportDownloadCmd.RegisterFlagCompletionFunc("cleanup", cobra.FixedCompletions([]string{"true", "false"}, cobra.ShellCompDirectiveNoFileComp)))

	f.BoolVar(&sosOpts.archive, "archive", false, "Create a .tar.gz archive of all downloaded reports")
	must(sosreportDownloadCmd.RegisterFlagCompletionFunc("archive", cobra.FixedCompletions([]string{"true", "false"}, cobra.ShellCompDirectiveNoFileComp)))
}

func runSOSReportDownload(ctx context.Context) error {
	if sosOpts.outputDir == "" {
		if sosOpts.caseID != "" {
			sosOpts.outputDir = fmt.Sprintf("sosreport-%s", sosOpts.caseID)
		} else {
			sosOpts.outputDir = fmt.Sprintf("sosreport-%s", time.Now().Format("20060102-150405"))
		}
	}
	if err := os.MkdirAll(sosOpts.outputDir, 0o755); err != nil {
		return fmt.Errorf("create output directory %s: %w", sosOpts.outputDir, err)
	}

	targets, err := getTargets(ctx)
	if err != nil {
		return err
	}
	defer targets.Close()

	downloaded := sosreport.Download(ctx, targets, sosOpts.namespace, sosOpts.outputDir)

	if downloaded == 0 {
		sosreport.ResultFail("No completed SOS reports found to download")
		if !cleanupExplicit {
			sosreport.Info("Use 'dpfctl sosreport status' to check Job progress")
		}
		return nil
	}

	sosreport.Result("Downloaded %d report(s) to %s", downloaded, sosOpts.outputDir)

	if sosOpts.archive {
		sosreport.Step("Creating archive")
		archivePath, err := sosreport.CreateArchive(sosOpts.outputDir)
		if err != nil {
			return fmt.Errorf("failed to create archive: %w", err)
		}
		sosreport.Result("Archive created: %s", archivePath)
	}

	if shouldCleanup() {
		sosreport.Step("Cleaning up")
		sosreport.Cleanup(ctx, targets, sosOpts.namespace, sosOpts.caseID)
	}

	return nil
}

// shouldCleanup determines whether to clean up resources based on flags.
func shouldCleanup() bool {
	if cleanupExplicit {
		return sosOpts.cleanup
	}

	fmt.Println("\nCleanup will remove all SOS report Jobs, pods, and secrets.")
	fmt.Println("You will need to run 'dpfctl sosreport start' again to collect new reports.")
	fmt.Print("Do you want to cleanup all created resources? [Y/n] ")
	reader := bufio.NewReader(os.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer == "" || answer == "y" || answer == "yes"
}
