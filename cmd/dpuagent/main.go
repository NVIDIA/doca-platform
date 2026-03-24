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
	"fmt"
	"os"

	provisioningv1 "github.com/nvidia/doca-platform/api/provisioning/v1alpha1"
	"github.com/nvidia/doca-platform/cmd/dpuagent/opts"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/client"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/client/trustedhost"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/client/zerotrust"
	"github.com/nvidia/doca-platform/internal/provisioning/dpuagent/operations"

	"github.com/spf13/pflag"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/yaml"
)

func main() {
	defer klog.Flush()

	options := opts.Options{}
	pflag.BoolVar(&options.ZeroTrustMode, "zero-trust-mode", false, "Enable zero trust mode")
	pflag.Int32Var(&options.ControlPlaneMTU, "control-plane-mtu", 1500, "Control plane MTU")
	pflag.StringVar(&options.Kubeconfig, "kubeconfig", "", "Path to the kubeconfig file")
	pflag.StringVar(&options.DPUName, "dpu-name", "", "Name of the DPU")
	pflag.StringVar(&options.DPUNamespace, "dpu-namespace", "", "Namespace of the DPU")
	pflag.StringVar(&options.DPUUID, "dpu-uid", "", "UID of the DPU object, used to reject stale agent status updates")
	pflag.StringVar(&options.DPUFlavor, "dpuflavor", "", "Path to the DPU flavor YAML file")
	pflag.StringVar(&options.KubeadmSecretName, "kubeadm-secret-name", "", "Name of the Secret containing the Kubeadm join command")
	pflag.StringVar(&options.KubeadmSecretNamespace, "kubeadm-secret-namespace", "", "Namespace of the Secret containing the Kubeadm join command")
	pflag.BoolVar(&options.SkipSysctl, "skip-sysctl", false, "Skip sysctl configuration")
	pflag.BoolVar(&options.SkipNetworkConfig, "skip-network-config", false, "Skip network configuration")
	pflag.BoolVar(&options.SkipDNSConfig, "skip-dns-config", false, "Skip DNS configuration")
	pflag.BoolVar(&options.SkipContainerdConfigration, "skip-containerd-config", false, "Skip containerd configuration")
	pflag.BoolVar(&options.SkipSFConfig, "skip-sf-config", false, "Skip SF configuration")
	pflag.BoolVar(&options.SkipVFMac, "skip-vf-mac", false, "Skip VF MAC configuration")
	pflag.BoolVar(&options.SkipOVSRawScript, "skip-ovs-raw-script", false, "Skip OVS raw script configuration")
	pflag.BoolVar(&options.SkipKernelCmdLine, "skip-kernel-cmd-line", false, "Skip kernel cmd line configuration")
	pflag.BoolVar(&options.SkipRemoveBuiltinKubelet, "skip-remove-builtin-kubelet", false, "Skip removing the built-in kubelet configuration")
	pflag.BoolVar(&options.SkipConfigureKubelet, "skip-configure-kubelet", false, "Skip kubelet configuration")
	pflag.BoolVar(&options.SkipStartKubelet, "skip-start-kubelet", false, "Skip starting kubelet")
	pflag.BoolVar(&options.SkipRebootMethodDiscovery, "skip-reboot-method-discovery", false, "Skip MFT-based reboot method discovery")
	pflag.Parse()

	if err := options.Validate(); err != nil {
		klog.Errorf("failed to validate options: %v", err)
		os.Exit(1)
	}

	dpuFlavor := &provisioningv1.DPUFlavor{}
	parseFileOrDie(options.DPUFlavor, YamlParserFunc, dpuFlavor)

	var client client.Client
	if options.ZeroTrustMode {
		client = zerotrust.NewZerotrustClient(options.Kubeconfig, options.DPUName, options.DPUNamespace, options.DPUUID)
	} else {
		client = trustedhost.NewTrustedhostClient(options.DPUName, options.DPUNamespace, options.DPUUID)
	}
	optCtx := &operations.Context{
		Client:    client,
		DPUFlavor: *dpuFlavor,
		Options:   options,
	}

	execCtx := klog.NewContext(ctrl.SetupSignalHandler(), klog.Background())
	if err := dpuagent.NewDPUAgent(optCtx).Run(execCtx); err != nil {
		klog.Fatalf("failed to run DPU agent: %v", err)
	}
	klog.Info("Successfully ran DPU agent")
}

type ParseFunc func(data []byte, obj interface{}) error

func YamlParserFunc(data []byte, obj interface{}) error { return yaml.Unmarshal(data, obj) }

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
