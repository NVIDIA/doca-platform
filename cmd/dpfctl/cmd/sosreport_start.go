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
	"time"

	"github.com/nvidia/doca-platform/internal/dpfctl/sosreport"

	"github.com/spf13/cobra"
)

var sosreportStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start SOS report collection Jobs on cluster nodes",
	Long: `Create SOS report collection Jobs on the target cluster(s) and nodes.

By default, Jobs keep their pods alive after completion so that reports can
be downloaded with 'dpfctl sosreport download'. Use --nfs-server and
--nfs-path to write directly to an NFS mount instead.

For NFS output, the --nfs-path must exist on the NFS server. A timestamped
subdirectory is created automatically (use --nfs-no-subdir to disable).`,
	Example: `  # Start SOS reports on all nodes in all clusters
  dpfctl sosreport start

  # Start on specific host nodes with a case ID
  dpfctl sosreport start --target host --nodes worker-1,worker-2 --case-id CASE-12345

  # Start on all worker nodes using a label selector
  dpfctl sosreport start --node-selector node-role.kubernetes.io/worker=

  # Start with NFS output (auto-creates sosreport-<timestamp> subdirectory)
  dpfctl sosreport start --nfs-server 10.0.0.1 --nfs-path /exports/sos

  # Start with NFS output without subdirectory
  dpfctl sosreport start --nfs-server 10.0.0.1 --nfs-path /exports/sos --nfs-no-subdir`,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := startOpts()
		if err := sosreport.ValidateStartOptions(opts); err != nil {
			return err
		}

		targets, err := getTargets(cmd.Context())
		if err != nil {
			return err
		}
		defer targets.Close()

		hostClient, err := sosreport.GetHostClient(targets)
		if err != nil {
			return fmt.Errorf("get host client: %w", err)
		}
		if err := sosreport.Start(cmd.Context(), targets, hostClient, *opts); err != nil {
			return err
		}

		sosreport.Info("Use 'dpfctl sosreport status' to check progress")
		if outputMode() == sosreport.OutputLocal {
			sosreport.Info("Use 'dpfctl sosreport download' to download completed reports")
		}
		return nil
	},
}

func init() {
	sosreportCmd.AddCommand(sosreportStartCmd)

	f := sosreportStartCmd.Flags()
	f.StringVar(&sosOpts.caseID, "case-id", "", "Case ID for the SOS report (default: dpf-<timestamp>)")
	f.StringVar(&sosOpts.nfsServer, "nfs-server", "", "NFS server address (enables NFS output mode)")
	f.StringVar(&sosOpts.nfsPath, "nfs-path", "", "NFS export path (must exist on the NFS server)")
	f.BoolVar(&sosOpts.nfsNoSub, "nfs-no-subdir", false, "Write directly to --nfs-path without creating a subdirectory")
	must(sosreportStartCmd.RegisterFlagCompletionFunc("nfs-no-subdir", cobra.FixedCompletions([]string{"true", "false"}, cobra.ShellCompDirectiveNoFileComp)))

	f.DurationVar(&sosOpts.timeout, "timeout", 30*time.Minute, "Job active deadline timeout")
	must(sosreportStartCmd.RegisterFlagCompletionFunc("timeout", cobra.FixedCompletions([]string{"5m", "15m", "30m", "1h"}, cobra.ShellCompDirectiveNoFileComp)))
}
