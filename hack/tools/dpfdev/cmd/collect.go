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
	"path/filepath"

	"github.com/nvidia/doca-platform/test/utils/collector"

	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func init() {
	var (
		kubeconfig      string
		artifactsDir    string
		includeClusters bool
	)

	collectCmd := &cobra.Command{
		Use:   "collect",
		Short: "Collect Kubernetes resources for debugging",
		Long: `Collect Kubernetes resources, logs, and events from the cluster for debugging purposes.

This command collects:
- All DPF Operator resources (DPUs, DPUServices, DPUClusters, etc.)
- Core Kubernetes resources (Pods, Deployments, Services, etc.)
- Pod logs and events
- DPU cluster resources (if --include-clusters is set)

The collected artifacts are saved to the specified directory for analysis.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// Set up logging
			ctrllog.SetLogger(zap.New(zap.UseDevMode(true)))
			log := ctrllog.FromContext(ctx)

			config, err := ctrl.GetConfig()
			if err != nil {
				return fmt.Errorf("failed to get Kubernetes cluster config: %w", err)
			}

			// Create Kubernetes client
			k8sClient, err := client.New(config, client.Options{})
			if err != nil {
				return fmt.Errorf("failed to create Kubernetes client: %w", err)
			}

			// Create Kubernetes clientset
			clientset, err := kubernetes.NewForConfig(config)
			if err != nil {
				return fmt.Errorf("failed to create Kubernetes clientset: %w", err)
			}

			// Create artifacts directory
			if err := os.MkdirAll(artifactsDir, 0755); err != nil {
				return fmt.Errorf("failed to create artifacts directory: %w", err)
			}

			log.Info("Starting resource collection", "artifactsDir", artifactsDir)

			// Get collectors
			var collectors []*collector.Cluster
			if includeClusters {
				// Collect from main cluster and DPU clusters
				collectors, err = collector.GetClusterCollectors(ctx, collector.ClusterCollector{
					Client:     k8sClient,
					ClientSet:  clientset,
					RestConfig: config,
				}, artifactsDir)
				if err != nil {
					return fmt.Errorf("failed to get cluster collectors: %w", err)
				}
			} else {
				// Only collect from main cluster
				mainCluster, err := collector.NewCluster(k8sClient, filepath.Join(artifactsDir, "main"), clientset, "main")
				if err != nil {
					return fmt.Errorf("failed to create main cluster collector: %w", err)
				}
				collectors = []*collector.Cluster{mainCluster}
			}

			// Run collection
			c := collector.New(collectors)
			if err := c.Run(ctx); err != nil {
				return fmt.Errorf("resource collection failed: %w", err)
			}

			log.Info("Resource collection completed successfully in directory " + artifactsDir)
			return nil
		},
	}

	collectCmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "Path to kubeconfig file (default: $HOME/.kube/config)")
	collectCmd.Flags().StringVarP(&artifactsDir, "output-dir", "o", "./artifacts", "Directory to save collected artifacts")
	collectCmd.Flags().BoolVar(&includeClusters, "include-clusters", false, "Also collect resources from DPU clusters")

	rootCmd.AddCommand(collectCmd)
}
