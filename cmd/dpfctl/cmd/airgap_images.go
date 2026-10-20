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

var airgapImagesOpts airgapOptions

var airgapImagesExample = `# List the images of DPF v26.10.0 to copy into mirror.example.com:5000, as "source destination" lines
%[1]s airgap images --chart https://helm.ngc.nvidia.com/nvidia/doca/dpf-operator --version v26.10.0 \
  --mirror mirror.example.com:5000

# Copy them, keeping the digests
%[1]s airgap images --chart https://helm.ngc.nvidia.com/nvidia/doca/dpf-operator --version v26.10.0 \
  --mirror mirror.example.com:5000 |
  while read -r src dst; do skopeo copy --all --preserve-digests "docker://$src" "docker://$dst"; done

# Only the images the DPU cluster pulls, with those of the DPUServices you deploy
%[1]s airgap images --chart ./dpf-operator-v26.10.0.tgz --mirror mirror.example.com:5000 \
  --scope dpu -f my-dpuservices/
`

var airgapImagesCmd = &cobra.Command{
	Use:   "images",
	Short: "List the images to copy into a mirror registry",
	Long: "List the images to copy into a mirror registry for an air-gapped DPF installation, one \"source destination\" " +
		"line per image: those of the air-gap BOM every installation needs, of the optional services of the BOM you " +
		"select, and of the DPUServices of the manifests you pass with -f, rendered with their values. An image is " +
		"copied to <mirror>/<name>:<tag>, e.g. nvcr.io/nvidia/doca/doca_hbn:3.5.0 to <mirror>/doca_hbn:3.5.0, or with " +
		"--layout nested below the mirror under its upstream location, to <mirror>/nvcr.io/nvidia/doca/doca_hbn:3.5.0.",
	Example:      fmt.Sprintf(airgapImagesExample, rootCmd.Root().Name()),
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
		rel, opts, err := airgapPlan(cmd.Context(), &airgapImagesOpts, cmd.InOrStdin(), cmd.ErrOrStderr())
		if err != nil {
			return err
		}
		copies, err := mirror.ImageCopies(rel, opts)
		if err != nil {
			return err
		}
		return printImageCopies(cmd.OutOrStdout(), airgapImagesOpts.output, copies)
	},
}

func init() {
	airgapCmd.AddCommand(airgapImagesCmd)
	addAirgapFlags(airgapImagesCmd, &airgapImagesOpts)
}

func printImageCopies(out io.Writer, format string, copies []mirror.ImageCopy) error {
	if format == airgapOutputJSON {
		return printAirgapJSON(out, copies)
	}
	for _, c := range copies {
		if _, err := fmt.Fprintln(out, c.Source, c.Destination); err != nil {
			return err
		}
	}
	return nil
}
