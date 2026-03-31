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
	"time"

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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
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

func init() {
	rootCmd.AddCommand(describeCmd)

	describeCmd.Flags().StringVar(&opts.showOtherConditions, "show-conditions", "",
		"list of comma separated kind or kind/name for which the command should show all the object's conditions (use 'all' to show conditions for everything).")

	describeCmd.Flags().StringVar(&opts.showResources, "show-resources", "",
		"list of comma separated kind or kind/name for which the command should show all the object's resources (default value is 'all').")

	describeCmd.Flags().StringVar(&opts.expandResources, "expand-resources", "",
		"list of comma separated kind or kind/name for which the command should show all the object's child resources (default value is '', 'failed' to expand only failed DPUServices).")

	describeCmd.Flags().BoolVar(&opts.grouping, "grouping", true,
		"enable grouping of objects by kind.")

	describeCmd.Flags().BoolVarP(&opts.color, "color", "c", true,
		"Enable or disable color output; if not set color is enabled by default only if using tty. The flag is overridden by the NO_COLOR env variable if set.")

	describeCmd.Flags().StringVarP(&opts.output, "output", "o", "table",
		"Output format. One of: table, json, yaml.")

	describeCmd.Flags().BoolVar(&opts.showPending, "show-pending", false,
		"Show conditions with Reason=Pending and Status=Unknown. These are hidden by default as they represent transient states.")

	// TODO: decide if we want to use Kubernetes cli-runtime here instead of the controller-runtime flags.
	// The cli-runtime has alot dependencies, but brings several generic flags that can be useful.
	//
	// Load the go flagset (i.e. controller-runtimes kubeconfig).
	describeCmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)
}

func runDescribe(subCmd string) error {
	ctx := context.Background()

	c, cacheStopFunc, err := newCachedClient(ctx)
	if err != nil {
		return err
	}
	// Stop the cache when we're done to release resources
	defer cacheStopFunc()

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

	// Honor NO_COLOR env var first, then the CLI flag.
	if _, present := os.LookupEnv("NO_COLOR"); present {
		color.NoColor = true
	} else {
		color.NoColor = !opts.color
	}

	return dpfctl.PrintObjectTree(tree)
}

// newCachedClient creates a cached client for better performance when doing many List/Get calls.
// Returns the client, a stop function to cleanup the cache, and any error.
func newCachedClient(ctx context.Context) (client.Client, func(), error) {
	config, err := ctrl.GetConfig()
	if err != nil {
		return nil, nil, err
	}

	// Create a scheme with all the types we need
	scheme := runtime.NewScheme()
	_ = operatorv1.AddToScheme(scheme)
	_ = provisioningv1.AddToScheme(scheme)
	_ = dpuservicev1.AddToScheme(scheme)
	_ = argov1.AddToScheme(scheme)
	_ = vpcv1.AddToScheme(scheme)
	_ = storagev1.AddToScheme(scheme)
	_ = kamajiv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Create a cache to hold the objects
	cacheObj, err := cache.New(config, cache.Options{
		Scheme: scheme,
	})
	if err != nil {
		return nil, nil, err
	}

	// Start the cache in a goroutine
	cacheCtx, cacheCancel := context.WithTimeout(ctx, time.Minute)
	go func() {
		if err := cacheObj.Start(cacheCtx); err != nil {
			// Log error but don't fail - the cache will just not work
			fmt.Fprintf(os.Stderr, "Warning: cache failed to start: %v\n", err)
		}
	}()

	// Wait for the cache to sync
	if !cacheObj.WaitForCacheSync(cacheCtx) {
		cacheCancel()
		return nil, nil, fmt.Errorf("failed to sync cache")
	}

	// Create a client that uses the cache for reads
	// This approach follows the pattern used in pkg/dpucluster/accessor_client.go
	c, err := client.New(config, client.Options{
		Scheme: scheme,
		Cache: &client.CacheOptions{
			Reader:       cacheObj,
			Unstructured: true,
			// Don't cache secrets and configmaps as they're not needed and can be numerous
			DisableFor: []client.Object{&corev1.Secret{}, &corev1.ConfigMap{}},
		},
	})
	if err != nil {
		cacheCancel()
		return nil, nil, err
	}

	stopFunc := func() {
		cacheCancel()
	}

	return c, stopFunc, nil
}
