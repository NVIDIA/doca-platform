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
	"fmt"
	"os"
	"path/filepath"

	"github.com/nvidia/doca-platform/hack/tools/dpfdev/pkg/htmlreport"
	"github.com/spf13/cobra"
)

var htmlReportOutput string

var htmlReportCmd = &cobra.Command{
	Use:   "htmlreport <dir>",
	Short: "Generate a static HTML viewer for artifact directories",
	Long: `Generate an HTML viewer for artifact directories collected by dpfdev collect.

The given directory is traversed to find every resource dump: a directory whose
<cluster> children contain a Resources/ or Events/ section. A single
artifacts-browser.html is written with one entry per dump found.

The viewer fetches files via relative URLs and must be served over HTTP from the
given directory, with the dump directories beneath it:

  <dir>/artifacts-browser.html            <- output
  <dir>/<dump>/<cluster>/Resources/...
  <dir>/<dump>/<cluster>/Events/...`,
	Example: `  # Traverse ./artifacts and write a single browser covering every dump found
  dpfdev htmlreport ./artifacts

  # Write to a custom path (must still be served from ./artifacts)
  dpfdev htmlreport ./artifacts -o ./artifacts/report.html`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		root := args[0]

		dumps, err := htmlreport.DiscoverDumps(root)
		if err != nil {
			return err
		}

		// Container logs live in logs/ directories, one per test phase; any other
		// *.log file is surfaced as a "CI Logs" entry. Both count as content
		// worth generating a report for.
		logDirs, err := htmlreport.DiscoverLogDirs(root)
		if err != nil {
			return err
		}
		ciLogs, err := htmlreport.DiscoverCILogs(root)
		if err != nil {
			return err
		}

		if len(dumps) == 0 && len(logDirs) == 0 && len(ciLogs) == 0 {
			fmt.Fprintf(os.Stderr, "no resource dumps or logs found under %s\n", root)
			return nil
		}

		output := htmlReportOutput
		if output == "" {
			output = filepath.Join(root, "artifacts-browser.html")
		}

		fmt.Fprintf(os.Stderr, "found %d resource dump(s) under %s:\n", len(dumps), root)
		for _, dump := range dumps {
			fmt.Fprintf(os.Stderr, "  - %s\n", dump)
		}
		for _, logDir := range logDirs {
			fmt.Fprintf(os.Stderr, "  - %s\n", logDir)
		}
		if len(ciLogs) > 0 {
			fmt.Fprintf(os.Stderr, "  - %d CI log(s)\n", len(ciLogs))
		}

		if err := htmlreport.Generate(root, dumps, output); err != nil {
			return err
		}

		fmt.Fprintf(os.Stderr, "report written to %s\n", output)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(htmlReportCmd)
	htmlReportCmd.Flags().StringVarP(&htmlReportOutput, "output", "o", "", "output HTML file path (default: <dir>/artifacts-browser.html)")
}
