//go:build linux

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

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

const (
	DPFDir = "/opt/dpf"
)

var (
	kubeconfig        string
	dpuflavorPath     string
	installConfigPath string
)

func main() {
	defer klog.Flush()

	pflag.StringVar(&kubeconfig, "kubeconfig", filepath.Join(DPFDir, "kubeconfig"), "Path to the kubeconfig file")
	pflag.StringVar(&dpuflavorPath, "dpuflavor", filepath.Join(DPFDir, "dpuflavor.yaml"), "Path to the DPU flavor file")
	pflag.StringVar(&installConfigPath, "config", filepath.Join(DPFDir, "install.config"), "Path to the install config file")
	pflag.Parse()

	client := buildClientOrDie(kubeconfig)
	dpuFlavor := &provisioningv1.DPUFlavor{}
	parseFileOrDie(dpuflavorPath, YamlParserFunc, dpuFlavor)
	installConfig := &operations.InstallConfig{}
	parseFileOrDie(installConfigPath, JSONParserFunc, installConfig)

	ctx := operations.Context{
		Client:        client,
		DPUFlavor:     *dpuFlavor,
		InstallConfig: *installConfig,
	}
	if err := dpuagent.NewDPUAgent(ctx).Run(); err != nil {
		klog.Fatalf("failed to run DPU agent: %v", err)
	}
	klog.Info("Successfully ran DPU agent")
}

type ParseFunc func(data []byte, obj interface{}) error

func YamlParserFunc(data []byte, obj interface{}) error { return yaml.Unmarshal(data, obj) }

func JSONParserFunc(data []byte, obj interface{}) error { return json.Unmarshal(data, obj) }

func parseFile(name string, parse ParseFunc, obj interface{}) error {
	data, err := os.ReadFile(name)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", name, err)
	}
	if err := parse(data, obj); err != nil {
		return fmt.Errorf("failed to parse file %s: %w", name, err)
	}
	return nil
}

func parseFileOrDie(name string, parse ParseFunc, obj interface{}) {
	err := parseFile(name, parse, obj)
	if err != nil {
		klog.Fatalf("failed to parse file %s: %v", name, err)
	}
}

func buildClientOrDie(kubeconfig string) client.Client {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(provisioningv1.AddToScheme(scheme))
	clientCfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		klog.Fatalf("failed to build client config: %v", err)
	}
	unCachedClient, err := client.New(clientCfg, client.Options{Scheme: scheme})
	if err != nil {
		klog.Fatalf("failed to create un-cached client: %v", err)
	}
	return unCachedClient
}
