/*
Copyright 2024 NVIDIA

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

package main

import (
	"context"
	"flag"
	"fmt"
	"path/filepath"
	"runtime"

	"github.com/nvidia/doca-platform/test/utils/collector"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	artifactsDirectory string
	kubeconfig         string
)

func init() {
	flag.StringVar(&kubeconfig, "kubeconfig", "~/.kube/config", "path to the kubeconfig file")
	flag.StringVar(&artifactsDirectory, "artifactsDirectory", "", "path to the directory where artifacts are stored")
}
func main() {
	flag.Parse()

	ctx := context.Background()
	log := klog.FromContext(ctx)

	// use the current context in kubeconfig
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		panic(err.Error())
	}

	clusterClient, err := client.New(config, client.Options{})
	if err != nil {
		panic(err)
	}

	// Get the path where the binary is located
	_, basePath, _, _ := runtime.Caller(0)

	// If the artifacts directory is not set default to the project root.
	if artifactsDirectory == "" {
		artifactsDirectory = filepath.Join(filepath.Dir(basePath), "../../../artifacts")
	}

	inventoryManifestsDirectory := filepath.Join(filepath.Dir(basePath), "../../../internal/operator/inventory/manifests")

	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err)
	}

	clusters, err := collector.GetClusterCollectors(ctx, clusterClient, artifactsDirectory, inventoryManifestsDirectory, clientset)

	if err != nil {
		log.Error(err, "Failed to get cluster collectors")
	}
	resourceCollector := collector.New(clusters)
	log.Info(fmt.Sprintf("Running the resource resourceCollector. Artifacts will be stored in %s", artifactsDirectory))
	// Run the resourceCollector for the main cluster.

	if err := resourceCollector.Run(ctx); err != nil {
		panic(err)
	}
}
