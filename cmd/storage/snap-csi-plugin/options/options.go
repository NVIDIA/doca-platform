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
	"k8s.io/apimachinery/pkg/util/validation"
	logsv1 "k8s.io/component-base/logs/api/v1"
)

// Options contains everything necessary to create and run a storage-plugin server.
type Options struct {
	// common options
	// Name defines the name of the plugin, this value is reported by the CSI driver identity endpoint
	Name string
	// EmulationMode defines the mode of the plugin, can be "nvme" or "virtiofs"
	EmulationMode string
	// PluginMode defines the mode of the plugin, can be "node" or "controller"
	PluginMode string

	// bind address for grpc server
	BindAddress string
	// configuration for the logger
	LoggingOptions *logsv1.LoggingConfiguration

	// node options
	// k8s node name of the node where plugin runs
	NodeID string
	// max number of volumes that can be published to the node
	// this option allows to explicitly set the max number of volumes that can be published to the node
	// if not set, the plugin will try to discover the max number of volumes that can be published to the node
	// based on the number of VFs on the SNAP controller
	MaxVolumesPerNode int64
	// device ID of the snap controller to use, has meaning only for nvme emulation mode
	SnapControllerDeviceID string
	// controls if NVMe driver should be loaded or not during initialization of the plugin, has meaning only for nvme emulation mode
	NVMeLoadDriver bool
	// controls if NVMe VFs should be created or not during initialization of the plugin, has meaning only for nvme emulation mode
	NVMeCreateVFs bool
	// name of the fs type name for virtiofs, has meaning only for virtiofs emulation mode
	VirtiofsFSTypeName string
	// controls if virtio-pci driver should be loaded or not during initialization of the plugin, has meaning only for virtiofs emulation mode
	VirtiofsLoadDriver bool

	// controller options
	// namespace to create DPUVolume and DPUVolumeAttachment objects in the host cluster
	Namespace string
}

// New returns Options initialized by default values
func New() *Options {
	return &Options{
		Name:                   config.DefaultPluginName,
		EmulationMode:          config.EmulationModeNVMe,
		BindAddress:            config.DefaultBindNetwork + "://" + config.DefaultBindAddress,
		LoggingOptions:         logsv1.NewLoggingConfiguration(),
		SnapControllerDeviceID: config.DefaultSnapDeviceID,
		NVMeLoadDriver:         true,
		NVMeCreateVFs:          true,
		VirtiofsFSTypeName:     config.DefaultVirtiofsFSTypeName,
		VirtiofsLoadDriver:     true,
	}
}

// AddFlags adds flags to fs and binds them to options.
func (o *Options) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.Name, "name", o.Name, "name of the plugin, this value is reported by the CSI driver identity endpoint")
	fs.StringVar(&o.EmulationMode, "emulation-mode", o.EmulationMode, "emulation mode for the plugin, can be \"nvme\" or \"virtiofs\"")
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
	fs.StringVar(&o.Namespace, "namespace", o.Namespace,
		"namespace to create DPUVolume and DPUVolumeAttachment objects, required for \"controller\" mode")
}

func (o *Options) addNodeFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.NodeID, "node-id", o.NodeID,
		"nodeID to use as CSI NodeID, required for \"node\" mode")
	fs.Int64Var(&o.MaxVolumesPerNode, "max-volumes-per-node", o.MaxVolumesPerNode,
		"max number of volumes that can be published to the node, if not set or set to 0, "+
			"the plugin will try to discover the max number of volumes that can be published to the node")
	fs.StringVar(&o.SnapControllerDeviceID, "node-snap-controller-device-id", o.SnapControllerDeviceID,
		"device ID of the snap controller to use")
	fs.BoolVar(&o.NVMeLoadDriver, "node-nvme-load-driver", o.NVMeLoadDriver,
		"controls if NVMe driver should be loaded or not during initialization of the plugin, has meaning only for nvme emulation mode")
	fs.BoolVar(&o.NVMeCreateVFs, "node-nvme-create-vfs", o.NVMeCreateVFs,
		"controls if NVMe VFs should be created or not during initialization of the plugin, has meaning only for nvme emulation mode")
	fs.StringVar(&o.VirtiofsFSTypeName, "node-virtiofs-filesystem-type", o.VirtiofsFSTypeName,
		"name of the virtiofs filesystem type, this value is used in mount command, has meaning only for virtiofs emulation mode")
	fs.BoolVar(&o.VirtiofsLoadDriver, "node-virtiofs-load-driver", o.VirtiofsLoadDriver,
		"controls if virtio-pci driver should be loaded or not during initialization of the plugin, has meaning only for virtiofs emulation mode")
}

// Validate options
func (o *Options) Validate() error {
	if err := o.validateCommonFlags(); err != nil {
		return err
	}
	switch o.PluginMode {
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
	if err := o.validateName(); err != nil {
		return err
	}
	if err := o.validateEmulationMode(); err != nil {
		return err
	}
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

func (o *Options) validateName() error {
	if o.Name == "" {
		return fmt.Errorf("name is required")
	}
	if errs := validation.IsDNS1123Subdomain(o.Name); len(errs) > 0 {
		return fmt.Errorf("name must follow domain name notation format: %v", errs)
	}
	return nil
}

func (o *Options) validateEmulationMode() error {
	if o.EmulationMode == "" {
		return fmt.Errorf("emulation mode is required")
	}
	switch o.EmulationMode {
	case config.EmulationModeNVMe, config.EmulationModeVirtiofs:
		return nil
	}
	return fmt.Errorf("unsupported emulation mode: \"%s\", supported modes: %s, %s",
		o.EmulationMode, config.EmulationModeNVMe, config.EmulationModeVirtiofs)
}

func (o *Options) validateNodeFlags() error {
	if o.NodeID == "" {
		return fmt.Errorf("node-id is required")
	}
	if o.MaxVolumesPerNode < 0 {
		return fmt.Errorf("max-volumes-per-node must be greater than 0")
	}
	if o.SnapControllerDeviceID == "" {
		return fmt.Errorf("node-snap-controller-device-id is required")
	}
	if o.VirtiofsFSTypeName == "" {
		return fmt.Errorf("node-virtiofs-filesystem-type is required")
	}
	return nil
}

func (o *Options) validateControllerFlags() error {
	if o.Namespace == "" {
		return fmt.Errorf("namespace is required")
	}
	return nil
}

func (o *Options) validatePluginMode() error {
	switch o.PluginMode {
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
