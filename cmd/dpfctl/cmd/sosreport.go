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
	"flag"
	"io"
	"os"
	"time"

	"github.com/nvidia/doca-platform/internal/dpfctl/sosreport"
	"github.com/nvidia/doca-platform/internal/dpfctl/util"
	"github.com/nvidia/doca-platform/internal/utils/tunnel"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
)

type sosreportFlags struct {
	cluster        string
	dpuCluster     string
	nodes          []string
	nodeSelector   string
	namespace      string
	image          string
	caseID         string
	nfsServer      string
	nfsPath        string
	nfsNoSub       bool
	nfsUID         int64
	timeout        time.Duration
	outputDir      string
	cleanup        bool
	archive        bool
	archiveOnly    bool
	requestsMemory string
	requestsCPU    string
	limitsMemory   string
	limitsCPU      string
}

var sosOpts sosreportFlags

const (
	requestsMemoryFlag = string(corev1.ResourceRequestsMemory)
	requestsCPUFlag    = string(corev1.ResourceRequestsCPU)
	limitsMemoryFlag   = string(corev1.ResourceLimitsMemory)
	limitsCPUFlag      = string(corev1.ResourceLimitsCPU)
)

var sosreportCmd = &cobra.Command{
	Use:   "sosreport",
	Short: "Collect SOS reports from DPF cluster nodes",
	Long: `Collect SOS reports from host and DPU cluster nodes using Kubernetes Jobs.

The sosreport command creates privileged Jobs on target nodes that run the
NVIDIA sosreport tool to collect system diagnostics. Reports can be written
to NFS or downloaded directly to your local machine.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if util.Verbose {
			tunnel.Stdout = os.Stderr
			tunnel.Stderr = os.Stderr
		} else {
			tunnel.Stdout = io.Discard
			tunnel.Stderr = io.Discard
			// Suppress port-forward teardown errors from runtime.HandleError.
			// These are internal to controller-runtime and not actionable for users;
			// --verbose restores full output for debugging.
			utilruntime.ErrorHandlers = []utilruntime.ErrorHandler{
				func(_ context.Context, _ error, _ string, _ ...any) {},
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(sosreportCmd)

	f := sosreportCmd.PersistentFlags()
	f.StringVar(&sosOpts.cluster, "target", "all", "Target environment: host, dpu, or all")
	must(sosreportCmd.RegisterFlagCompletionFunc("target", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"host", "dpu", "all"}, cobra.ShellCompDirectiveNoFileComp
	}))

	f.StringVar(&sosOpts.dpuCluster, "dpu-cluster", "", "Target a specific DPUCluster by name (defaults to all)")
	must(sosreportCmd.RegisterFlagCompletionFunc("dpu-cluster", completeDPUClusters))

	f.StringSliceVar(&sosOpts.nodes, "nodes", nil, "Target specific nodes across all targeted clusters (comma-separated)")
	must(sosreportCmd.RegisterFlagCompletionFunc("nodes", completeNodes))

	f.StringVar(&sosOpts.nodeSelector, "node-selector", "", "Label selector to filter nodes across all targeted clusters (e.g. node-role.kubernetes.io/worker=)")

	f.StringVar(&sosOpts.namespace, "namespace", "default", "Namespace for Jobs and Secrets")
	must(sosreportCmd.RegisterFlagCompletionFunc("namespace", completeNamespaces))

	f.StringVar(&sosOpts.caseID, "case-id", "", "Case ID for the SOS report (default: dpf-<timestamp>)")
	f.StringVar(&sosOpts.image, "image", "ghcr.io/nvidia/sosreport:latest", "SOS report container image")
	f.BoolVarP(&util.Verbose, "verbose", "v", false, "Show debug output including port-forward details")
	f.AddGoFlagSet(flag.CommandLine)
}

// addStartFlags registers the flags shared by the start and collect subcommands.
func addStartFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVar(&sosOpts.nfsServer, "nfs-server", "", "NFS server address (enables NFS output mode)")
	f.StringVar(&sosOpts.nfsPath, "nfs-path", "", "NFS export path (must exist on the NFS server)")
	f.BoolVar(&sosOpts.nfsNoSub, "nfs-no-subdir", false, "Write directly to --nfs-path without creating a subdirectory")
	must(cmd.RegisterFlagCompletionFunc("nfs-no-subdir", cobra.FixedCompletions([]string{"true", "false"}, cobra.ShellCompDirectiveNoFileComp)))
	f.Int64Var(&sosOpts.nfsUID, "nfs-uid", 0, "UID for NFS directory creation (use non-zero when NFS has root_squash)")
	f.DurationVar(&sosOpts.timeout, "timeout", 30*time.Minute, "Job active deadline timeout")
	must(cmd.RegisterFlagCompletionFunc("timeout", cobra.FixedCompletions([]string{"5m", "15m", "30m", "1h"}, cobra.ShellCompDirectiveNoFileComp)))
	f.StringVar(&sosOpts.requestsMemory, requestsMemoryFlag, sosreport.DefaultMemoryRequest, "Memory request for the sosreport container (Kubernetes quantity, e.g. 256Mi, 1Gi)")
	must(cmd.RegisterFlagCompletionFunc(requestsMemoryFlag, cobra.FixedCompletions([]string{"128Mi", "256Mi", "512Mi", "1Gi"}, cobra.ShellCompDirectiveNoFileComp)))
	f.StringVar(&sosOpts.requestsCPU, requestsCPUFlag, sosreport.DefaultCPURequest, "CPU request for the sosreport container (Kubernetes quantity, e.g. 100m, 1)")
	must(cmd.RegisterFlagCompletionFunc(requestsCPUFlag, cobra.FixedCompletions([]string{"50m", "100m", "250m", "500m", "1"}, cobra.ShellCompDirectiveNoFileComp)))
	f.StringVar(&sosOpts.limitsMemory, limitsMemoryFlag, sosreport.DefaultMemoryLimit, "Memory limit for the sosreport container (Kubernetes quantity, e.g. 256Mi, 1Gi)")
	must(cmd.RegisterFlagCompletionFunc(limitsMemoryFlag, cobra.FixedCompletions([]string{"512Mi", "1Gi", "2Gi", "4Gi"}, cobra.ShellCompDirectiveNoFileComp)))
	f.StringVar(&sosOpts.limitsCPU, limitsCPUFlag, "", "Optional CPU limit (empty by default; CPU limits cause throttling and are usually counterproductive)")
}

// addArchiveFlags registers the --archive and --archive-only flags.
// Used by start (NFS archiving), collect (both modes), and download (local archiving).
func addArchiveFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.BoolVar(&sosOpts.archive, "archive", false, "Create a .tar.gz archive of all reports")
	must(cmd.RegisterFlagCompletionFunc("archive", cobra.FixedCompletions([]string{"true", "false"}, cobra.ShellCompDirectiveNoFileComp)))
	f.BoolVar(&sosOpts.archiveOnly, "archive-only", false, "Remove individual report files after archiving (implies --archive)")
	must(cmd.RegisterFlagCompletionFunc("archive-only", cobra.FixedCompletions([]string{"true", "false"}, cobra.ShellCompDirectiveNoFileComp)))
}

// addDownloadFlags registers the flags shared by the collect and download subcommands.
func addDownloadFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVar(&sosOpts.outputDir, "output-dir", "", "Local directory for downloaded reports (default: sosreport-<timestamp>)")
	must(cmd.RegisterFlagCompletionFunc("output-dir", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return nil, cobra.ShellCompDirectiveFilterDirs
	}))
}

// startOpts builds StartOptions from the cobra flags.
func startOpts() *sosreport.StartOptions {
	return &sosreport.StartOptions{
		Namespace:    sosOpts.namespace,
		Image:        sosOpts.image,
		CaseID:       sosOpts.caseID,
		Output:       outputMode(),
		NFSServer:    sosOpts.nfsServer,
		NFSPath:      sosOpts.nfsPath,
		NFSNoSub:     sosOpts.nfsNoSub,
		NFSUID:       sosOpts.nfsUID,
		Archive:      sosOpts.archive,
		ArchiveOnly:  sosOpts.archiveOnly,
		Timeout:      sosOpts.timeout,
		Nodes:        sosOpts.nodes,
		NodeSelector: sosOpts.nodeSelector,
		Cluster:      sosOpts.cluster,
		DPUCluster:   sosOpts.dpuCluster,
		MemoryReq:    sosOpts.requestsMemory,
		MemoryLimit:  sosOpts.limitsMemory,
		CPUReq:       sosOpts.requestsCPU,
		CPULimit:     sosOpts.limitsCPU,
	}
}

// outputMode derives the output mode from flags.
func outputMode() sosreport.OutputMode {
	if sosOpts.nfsServer != "" {
		return sosreport.OutputNFS
	}
	return sosreport.OutputLocal
}

// getTargets is a helper that resolves cluster targets from flags.
func getTargets(ctx context.Context) (sosreport.ClusterTargets, error) {
	return sosreport.GetClusterTargets(ctx, sosOpts.cluster, sosOpts.dpuCluster)
}

func validateSOSReportCaseID() error {
	return sosreport.ValidateCaseID(sosOpts.caseID)
}

// completeDPUClusters provides shell completion for --dpu-cluster by listing DPUCluster names.
func completeDPUClusters(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	targets, err := sosreport.GetClusterTargets(cmd.Context(), "dpu", "")
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	defer targets.Close()
	names := make([]string, 0, len(targets))
	for _, t := range targets {
		names = append(names, t.Name)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

// completeNodes provides shell completion for --nodes by listing node names from all targeted clusters.
func completeNodes(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	targets, err := sosreport.GetClusterTargets(cmd.Context(), sosOpts.cluster, sosOpts.dpuCluster)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	defer targets.Close()
	var names []string
	for _, t := range targets {
		nodes, err := sosreport.ListNodes(cmd.Context(), t.Client, sosOpts.nodeSelector)
		if err != nil {
			continue
		}
		names = append(names, nodes...)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

// completeNamespaces provides shell completion for --namespace by listing cluster namespaces.
func completeNamespaces(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	targets, err := sosreport.GetClusterTargets(cmd.Context(), "host", "")
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	defer targets.Close()
	if len(targets) == 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return sosreport.ListNamespaces(cmd.Context(), targets[0].Client), cobra.ShellCompDirectiveNoFileComp
}
