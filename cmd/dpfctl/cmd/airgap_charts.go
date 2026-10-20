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
	"io"

	"github.com/nvidia/doca-platform/internal/airgap/mirror"

	"github.com/spf13/cobra"
)

var airgapChartsOpts airgapOptions

var airgapChartsExample = `# List the helm charts of DPF v26.10.0 to copy into mirror.example.com:5000, as
# "repository name version destination" lines
%[1]s airgap charts --chart https://helm.ngc.nvidia.com/nvidia/doca/dpf-operator --version v26.10.0 \
  --mirror mirror.example.com:5000

# Copy them (an oci:// repository is pulled as <repository>/<name>)
%[1]s airgap charts --chart https://helm.ngc.nvidia.com/nvidia/doca/dpf-operator --version v26.10.0 \
  --mirror mirror.example.com:5000 |
  while read -r repo name version dst; do
    case "$repo" in
      oci://*) helm pull "$repo/$name" --version "$version" ;;
      *) helm pull "$name" --repo "$repo" --version "$version" ;;
    esac
    helm push "$name-$version.tgz" "$dst"
  done
`

var airgapChartsCmd = &cobra.Command{
	Use:   "charts",
	Short: "List the helm charts to copy into a mirror registry",
	Long: "List the helm charts to copy into a mirror registry for an air-gapped DPF installation, one \"repository " +
		"name version destination\" line per chart: the DPF charts every installation needs, and the charts of the " +
		"optional services of the BOM you select and of the DPUServices of the manifests you pass with -f. The charts " +
		"are pushed to oci://<mirror>, or with --layout nested to an OCI repository below the mirror named after the " +
		"upstream location of their repository, e.g. those of https://helm.ngc.nvidia.com/nvidia/doca to " +
		"oci://<mirror>/helm.ngc.nvidia.com/nvidia/doca.",
	Example:      fmt.Sprintf(airgapChartsExample, rootCmd.Root().Name()),
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		rel, opts, err := airgapPlan(cmd.Context(), &airgapChartsOpts, cmd.InOrStdin(), cmd.ErrOrStderr())
		if err != nil {
			return err
		}
		copies, err := mirror.ChartCopies(rel, opts)
		if err != nil {
			return err
		}
		return printChartCopies(cmd.OutOrStdout(), airgapChartsOpts.output, copies)
	},
}

func init() {
	airgapCmd.AddCommand(airgapChartsCmd)
	addAirgapFlags(airgapChartsCmd, &airgapChartsOpts)
}

func printChartCopies(out io.Writer, format string, copies []mirror.ChartCopy) error {
	if format == airgapOutputJSON {
		return printAirgapJSON(out, copies)
	}
	for _, c := range copies {
		if _, err := fmt.Fprintln(out, c.RepoURL, c.Name, c.Version, c.Destination); err != nil {
			return err
		}
	}
	return nil
}
