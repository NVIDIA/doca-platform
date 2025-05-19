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

package options

import (
	"fmt"
	"net/url"

	"github.com/nvidia/doca-platform/internal/storage/snap/csi-plugin/config"

	"github.com/spf13/pflag"
	logsv1 "k8s.io/component-base/logs/api/v1"
)

// Options contains everything necessary to create and run a storage-plugin server.
type Options struct {
	// common options
	PluginMode string
	// bind address for grpc server
	BindAddress string
	// configuration for the logger
	LoggingOptions *logsv1.LoggingConfiguration

	// node options
	// k8s node name of the node where plugin runs
	NodeID string
	// the path where the host root fs is mounted
	HostRootFS string
	// device ID of the snap controller to use
	SnapControllerDeviceID string

	// controller options
	// namespace in the DPU cluster to create Volume and VolumeAttachment objects
	TargetNamespace string
	// host address of the DPU cluster API server
	DPUClusterAPIHost string
	// port of the DPU cluster API server
	DPUClusterAPIPort string
	// path to the file that contains access token for the DPU cluster API server
	DPUClusterTokenFile string
	// path to the file that contains CA certificate for the DPU cluster API server
	DPUClusterCAFile string
}

// New returns Options initialized by default values
func New() *Options {
	return &Options{
		BindAddress:            config.DefaultBindNetwork + "://" + config.DefaultBindAddress,
		LoggingOptions:         logsv1.NewLoggingConfiguration(),
		HostRootFS:             config.DefaultHostRootFS,
		SnapControllerDeviceID: config.DefaultSnapDeviceID,
	}
}

// AddFlags adds flags to fs and binds them to options.
func (o *Options) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.PluginMode, "mode", o.PluginMode, `Plugin mode, can be "node" or "controller"
		"node" - server only Node CSI endpoints
		"controller" - server only Controller CSI endpoints,
		Identity CSI endpoints are always serverd`)
	fs.StringVar(&o.BindAddress, "bind-address", o.BindAddress,
		"GPRC server bind address. e.g.: tcp://127.0.0.1:9090, unix:///var/lib/foo")
	logsv1.AddFlags(o.LoggingOptions, fs)
	o.addControllerFlags(fs)
	o.addNodeFlags(fs)
}

func (o *Options) addControllerFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.TargetNamespace, "controller-target-namespace", o.TargetNamespace,
		"namespace in the DPU cluster to create Volume and VolumeAttachment objects, required for \"controller\" mode")
	fs.StringVar(&o.DPUClusterAPIHost, "controller-dpu-cluster-api-host", o.DPUClusterAPIHost,
		"DPU cluster API host, required for \"controller\" mode")
	fs.StringVar(&o.DPUClusterAPIPort, "controller-dpu-cluster-api-port", o.DPUClusterAPIPort,
		"DPU cluster API port, required for \"controller\" mode")
	fs.StringVar(&o.DPUClusterTokenFile, "controller-dpu-cluster-token-file", o.DPUClusterTokenFile,
		"path to the file that stores token file for DPU cluster API, required for \"controller\" mode")
	fs.StringVar(&o.DPUClusterCAFile, "controller-dpu-cluster-ca-file", o.DPUClusterCAFile,
		"path to the file that stores CA certificate for DPU cluster API, required for \"controller\" mode")
}

func (o *Options) addNodeFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.NodeID, "node-id", o.NodeID,
		"nodeID to use as CSI NodeID, required for \"node\" mode")
	fs.StringVar(&o.HostRootFS, "node-root-fs", o.HostRootFS,
		"the path where the host root fs is mounted")
	fs.StringVar(&o.SnapControllerDeviceID, "node-snap-controller-device-id", o.SnapControllerDeviceID,
		"device ID of the snap controller to use")
}

// Validate options
func (o *Options) Validate() error {
	if err := o.validateCommonFlags(); err != nil {
		return err
	}
	switch config.PluginMode(o.PluginMode) {
	case config.PluginModeNode:
		if err := o.validateNodeFlags(); err != nil {
			return err
		}
	case config.PluginModeController:
		if err := o.validateControllerFlags(); err != nil {
			return err
		}
	}
	return nil
}

func (o *Options) validateCommonFlags() error {
	if err := o.validatePluginMode(); err != nil {
		return err
	}
	if err := o.validateBindAddress(); err != nil {
		return err
	}
	if err := o.validateLogOptions(); err != nil {
		return err
	}
	return nil
}

func (o *Options) validateNodeFlags() error {
	if o.NodeID == "" {
		return fmt.Errorf("node-id is required")
	}
	return nil
}

func (o *Options) validateControllerFlags() error {
	if o.TargetNamespace == "" {
		return fmt.Errorf("target-namespace is required")
	}
	if o.DPUClusterAPIHost == "" {
		return fmt.Errorf("controller-dpu-cluster-api-host is required")
	}
	if o.DPUClusterAPIPort == "" {
		return fmt.Errorf("controller-dpu-cluster-api-port is required")
	}
	if o.DPUClusterTokenFile == "" {
		return fmt.Errorf("controller-dpu-cluster-token-file is required")
	}
	if o.DPUClusterCAFile == "" {
		return fmt.Errorf("controller-dpu-cluster-ca-file is required")
	}
	return nil
}

func (o *Options) validatePluginMode() error {
	switch config.PluginMode(o.PluginMode) {
	case config.PluginModeController, config.PluginModeNode:
		return nil
	}
	return fmt.Errorf("unsupported plugin mode: \"%s\", supported modes: %s, %s",
		o.PluginMode, config.PluginModeController, config.PluginModeNode)
}

func (o *Options) validateBindAddress() error {
	_, _, err := ParseBindAddress(o.BindAddress)
	if err != nil {
		return fmt.Errorf("invalid bind-address: %v", err)
	}
	return nil
}

func (o *Options) validateLogOptions() error {
	return logsv1.ValidateAndApply(o.LoggingOptions, nil)
}

// ParseBindAddress validate bind address and return it as net and address
func ParseBindAddress(addr string) (string, string, error) {
	u, err := url.Parse(addr)
	if err != nil {
		return "", "", err
	}
	switch u.Scheme {
	case config.NetTCP:
		return u.Scheme, u.Host, nil
	case config.NetUnix:
		return u.Scheme, u.Host + u.Path, nil
	default:
		return "", "", fmt.Errorf("unsupported scheme")
	}
}
