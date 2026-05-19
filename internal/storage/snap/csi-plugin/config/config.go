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

package config

import "sync/atomic"

const (
	// PluginModeController - server only CSI Controller endpoints
	PluginModeController = "controller"
	// PluginModeNode - server only CSI Node endpoints
	PluginModeNode = "node"
)

const (
	// NetUnix is network type for unix sockets
	NetUnix = "unix"
	// NetTCP is network type for tcp socket
	NetTCP = "tcp"
)

const (
	// DefaultBindNetwork is default network type to bind CSI server to
	DefaultBindNetwork = NetUnix
	// DefaultBindAddress is default address for CSI socket
	DefaultBindAddress = "csi.sock"
)

const (
	// DefaultSnapDeviceID default value for the snap device ID
	DefaultSnapDeviceID = "6001"
)

const (
	// DefaultVirtiofsFSTypeName is the default name of the Virtiofs fs type name
	DefaultVirtiofsFSTypeName = "virtiofs"
)

const (
	// DefaultPluginName is the default name of this CSI SP.
	DefaultPluginName = "csi.snap.nvidia.com"
)

const (
	// EmulationModeNVMe is the emulation mode for NVMe devices
	EmulationModeNVMe = "nvme"
	// EmulationModeVirtiofs is the emulation mode for Virtiofs devices
	EmulationModeVirtiofs = "virtiofs"
)

// ServerBindOptions holds bind config for the server
type ServerBindOptions struct {
	// Network is a network type in net.Listen format on which ServiceManager should
	// listen for GRPC requests
	// e.g. unix or tcp
	Network string
	// Address is a listen address for GRPC server
	Address string
}

// PluginConfig is a global config for plugin
type PluginConfig struct {
	Common
	Node
	Controller
}

// Common contains common configuration options that applies to node and controller modes
type Common struct {
	// Name is the name of the plugin, this value is reported by the CSI driver identity endpoint
	Name string
	// EmulationMode is the emulation mode of the plugin, can be "nvme" or "virtiofs"
	EmulationMode string
	// PluginMode is mode in which plugin works, can be "node" or "controller"
	PluginMode string
	// ListOptions contains listener configuration for GRPC server
	ListenOptions ServerBindOptions
}

// Node contains options that are specific for the "node" mode
type Node struct {
	// name of the k8s node on which plugin is running
	NodeID string
	// MaxVolumesPerNode defines the maximum number of volumes that can be published to a node.
	// This value can be set by the user and takes precedence over the dynamically calculated value.
	MaxVolumesPerNode int64
	// device ID of the snap controller to use, has meaning only for nvme emulation mode
	SnapControllerDeviceID string
	// controls if NVMe driver should be loaded or not during initialization of the plugin, has meaning only for nvme emulation mode
	NVMeLoadDriver bool
	// controls if NVMe VFs should be created or not during initialization of the plugin, has meaning only for nvme emulation mode
	NVMeCreateVFs bool
	// name of the virtiofs fs type, this value is used in mount command, has meaning only for virtiofs emulation mode
	VirtiofsFSTypeName string
	// controls if virtio-pci driver should be loaded or not during initialization of the plugin, has meaning only for virtiofs emulation mode
	VirtiofsLoadDriver bool
}

// Controller contains options that are specific for the "controller" mode
type Controller struct {
	// Namespace to create DPUVolume and DPUVolumeAttachment objects
	Namespace string
}

// NewNodeRuntime returns a new instance of NodeRuntime struct
func NewNodeRuntime() *NodeRuntime {
	return &NodeRuntime{}
}

// NodeRuntime contains runtime configuration specific for the "node" mode
type NodeRuntime struct {
	// maximum number of volumes that controller can publish to the node
	maxVolumesPerNode atomic.Int64
}

// SetMaxVolumesPerNode set MaxVolumesPerNode runtime option
func (nr *NodeRuntime) SetMaxVolumesPerNode(val int64) {
	nr.maxVolumesPerNode.Store(val)
}

// GetMaxVolumesPerNode returns value of MaxVolumesPerNode runtime option
func (nr *NodeRuntime) GetMaxVolumesPerNode() int64 {
	return nr.maxVolumesPerNode.Load()
}
