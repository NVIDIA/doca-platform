/*
Copyright 2025 NVIDIA

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
	"fmt"
	"os"
	"sort"
	"strings"

	dpuservicev1 "github.com/nvidia/doca-platform/api/dpuservice/v1alpha1"
	operatorv1 "github.com/nvidia/doca-platform/api/operator/v1alpha1"
	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	storagev1 "github.com/nvidia/doca-platform/api/storage/v1alpha1"
	vpcv1 "github.com/nvidia/doca-platform/api/vpc/v1alpha1"
	"github.com/nvidia/doca-platform/internal/dpfctl"
	kamajiv1 "github.com/nvidia/doca-platform/third_party/api/kamaji/api/v1alpha1"
	argov1 "github.com/nvidia/doca-platform/third_party/forked/argoproj/argo-cd/pkg/apis/application/v1alpha1"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type describeOptions struct {
	showOtherConditions string
	showResources       string
	expandResources     string
	output              string
	grouping            bool
	color               bool
	showStorage         bool
	showPending         bool
	issues              bool
}

var opts describeOptions

var exampleCmds = `# Show only DPUService resources
%[1]s describe %[2]s --show-resources=dpuservice

# Show all conditions for DPUService and DPU resources
%[1]s describe %[2]s --show-conditions=dpuservices,dpu

# Show conditions for a specific DPU
%[1]s describe %[2]s --show-conditions=dpu/dpf-test-0000-08-00

# Show all conditions for all resources
%[1]s describe %[2]s --show-conditions=all

# Expand the resources for a DPUService
%[1]s describe %[2]s --expand-resources=DPUService

# Show only resources with issues (non-Ready conditions)
%[1]s describe %[2]s --issues

# Run %[1]s for a different cluster
%[1]s describe %[2]s --kubeconfig /path/to/your/kubeconfig
# or
KUBECONFIG=/path/to/your/kubeconfig %[1]s describe
`

// describeCmd represents the describe command
var describeCmd = &cobra.Command{
	Use:     "describe",
	Short:   "Describe DPF resources",
	Long:    "Describe different kind of subsets of the DPF resources in your cluster.",
	Example: fmt.Sprintf(exampleCmds, rootCmd.Root().Name(), "<all,dpuclusters,dpudeployments,dpusets,dpuservices,dpuvpcs,storage>"),
}

// dpfAPIGroups are the API groups queried via discovery to determine which
// resources are actually installed on the cluster.
var dpfAPIGroups = []schema.GroupVersion{
	operatorv1.GroupVersion,
	provisioningv1.GroupVersion,
	dpuservicev1.GroupVersion,
	vpcv1.GroupVersion,
}

// completeResourceKinds queries the API server for installed resources in our
// API groups and returns their plural names.
func completeResourceKinds() []string {
	config, err := ctrl.GetConfig()
	if err != nil {
		return []string{}
	}

	dc, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return []string{}
	}

	result := []string{"all"}
	for _, group := range dpfAPIGroups {
		resources, err := dc.ServerResourcesForGroupVersion(group.String())
		if err != nil {
			continue
		}
		for _, r := range resources.APIResources {
			// Skip subresources (e.g. dpusets/status).
			if strings.Contains(r.Name, "/") {
				continue
			}
			result = append(result, r.Name)
		}
	}

	sort.Strings(result)
	return result
}

func init() {
	rootCmd.AddCommand(describeCmd)

	describeCmd.Flags().StringVar(&opts.showOtherConditions, "show-conditions", "",
		"list of comma separated kind or kind/name for which the command should show all the object's conditions (use 'all' to show conditions for everything).")
	must(describeCmd.RegisterFlagCompletionFunc("show-conditions", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return completeResourceKinds(), cobra.ShellCompDirectiveNoFileComp
	}))

	describeCmd.Flags().StringVar(&opts.showResources, "show-resources", "",
		"list of comma separated kind or kind/name for which the command should show all the object's resources (default value is 'all').")
	must(describeCmd.RegisterFlagCompletionFunc("show-resources", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return completeResourceKinds(), cobra.ShellCompDirectiveNoFileComp
	}))

	describeCmd.Flags().StringVar(&opts.expandResources, "expand-resources", "",
		"list of comma separated kind or kind/name for which the command should show all the object's child resources (default value is '').")
	must(describeCmd.RegisterFlagCompletionFunc("expand-resources", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return completeResourceKinds(), cobra.ShellCompDirectiveNoFileComp
	}))

	describeCmd.Flags().BoolVar(&opts.grouping, "grouping", true,
		"enable grouping of objects by kind.")
	must(describeCmd.RegisterFlagCompletionFunc("grouping", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"true", "false"}, cobra.ShellCompDirectiveNoFileComp
	}))

	describeCmd.Flags().BoolVarP(&opts.color, "color", "c", true,
		"Enable or disable color output; if not set color is enabled by default only if using tty. The flag is overridden by the NO_COLOR env variable if set.")
	must(describeCmd.RegisterFlagCompletionFunc("color", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"true", "false"}, cobra.ShellCompDirectiveNoFileComp
	}))

	describeCmd.Flags().StringVarP(&opts.output, "output", "o", "table",
		"Output format. One of: table, json, yaml.")
	must(describeCmd.RegisterFlagCompletionFunc("output", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"table", "json", "yaml"}, cobra.ShellCompDirectiveNoFileComp
	}))

	describeCmd.Flags().BoolVar(&opts.showPending, "show-pending", false,
		"Show conditions with Reason=Pending and Status=Unknown. These are hidden by default as they represent transient states.")
	must(describeCmd.RegisterFlagCompletionFunc("show-pending", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"true", "false"}, cobra.ShellCompDirectiveNoFileComp
	}))

	describeCmd.Flags().BoolVar(&opts.issues, "issues", false,
		"Show only resources with issues (non-Ready conditions). Healthy subtrees are hidden.")
	must(describeCmd.RegisterFlagCompletionFunc("issues", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"true", "false"}, cobra.ShellCompDirectiveNoFileComp
	}))

	// TODO: decide if we want to use Kubernetes cli-runtime here instead of the controller-runtime flags.
	// The cli-runtime has alot dependencies, but brings several generic flags that can be useful.
	//
	// Load the go flagset (i.e. controller-runtimes kubeconfig).
	describeCmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)
}

func runDescribe(subCmd string) error {
	ctx := context.Background()

	c, err := newClient()
	if err != nil {
		return err
	}

	options := dpfctl.ObjectTreeOptions{
		ShowResources:       opts.showResources,
		ShowOtherConditions: opts.showOtherConditions,
		ExpandResources:     opts.expandResources,
		Grouping:            opts.grouping,
		Colors:              opts.color,
		Output:              opts.output,
		ShowStorage:         opts.showStorage,
		ShowPending:         opts.showPending,
	}

	tree, err := dpfctl.Discover(ctx, c, options, subCmd)
	if err != nil {
		return err
	}

	if opts.issues {
		tree.PruneHealthy()
	}

	// Honor NO_COLOR env var first, then the CLI flag.
	if _, present := os.LookupEnv("NO_COLOR"); present {
		color.NoColor = true
	} else {
		color.NoColor = !opts.color
	}

	return dpfctl.PrintObjectTree(tree)
}

// newClient creates a client for querying DPF resources.
// Uses a direct client (no cache) to avoid the overhead of syncing watches
// for all resource types on startup, which is slow on large clusters.
func newClient() (client.Client, error) {
	config, err := ctrl.GetConfig()
	if err != nil {
		return nil, err
	}

	scheme := runtime.NewScheme()
	_ = operatorv1.AddToScheme(scheme)
	_ = provisioningv1.AddToScheme(scheme)
	_ = dpuservicev1.AddToScheme(scheme)
	_ = argov1.AddToScheme(scheme)
	_ = vpcv1.AddToScheme(scheme)
	_ = storagev1.AddToScheme(scheme)
	_ = kamajiv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	return client.New(config, client.Options{Scheme: scheme})
}
