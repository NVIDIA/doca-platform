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
	"fmt"
	"os"

	"github.com/nvidia/doca-platform/cmd/storage/snap-csi-plugin/options"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/common"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/config"
	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/manager"

	"github.com/spf13/pflag"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
)

func main() {
	opts := options.New()
	opts.AddFlags(pflag.CommandLine)
	pflag.CommandLine.SortFlags = false
	pflag.Parse()
	ctrl.SetLogger(klog.Background())

	if err := opts.Validate(); err != nil {
		fmt.Println("invalid configuration: ", err.Error())
		pflag.Usage()
		os.Exit(1)
	}

	if err := run(opts); err != nil {
		klog.ErrorS(err, "critical error")
		os.Exit(1)
	}
}

// run in the entry point of the app
func run(opts *options.Options) error {
	klog.InfoS("start storage plugin", "name", common.PluginName, "version", common.VendorVersion)
	ctx := ctrl.SetupSignalHandler()
	net, address, err := options.ParseBindAddress(opts.BindAddress)
	if err != nil {
		return err
	}

	mgr, err := manager.New(config.PluginConfig{
		Common: config.Common{
			PluginMode: config.PluginMode(opts.PluginMode),
			ListenOptions: config.ServerBindOptions{
				Network: net,
				Address: address,
			},
		},
		Node: config.Node{
			NodeID:                 opts.NodeID,
			HostRootFS:             opts.HostRootFS,
			SnapControllerDeviceID: opts.SnapControllerDeviceID,
		},
		Controller: config.Controller{
			TargetNamespace:     opts.TargetNamespace,
			DPUClusterAPIHost:   opts.DPUClusterAPIHost,
			DPUClusterAPIPort:   opts.DPUClusterAPIPort,
			DPUClusterTokenFile: opts.DPUClusterTokenFile,
			DPUClusterCAFile:    opts.DPUClusterCAFile,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to initialize manager: %v", err)
	}
	err = mgr.Start(ctx)
	if err != nil {
		return fmt.Errorf("failed to start manager: %v", err)
	}
	return nil
}
