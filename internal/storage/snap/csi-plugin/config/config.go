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

// PluginMode contains plugin pluginMode
type PluginMode string

const (
	// PluginModeController - server only CSI Controller endpoints
	PluginModeController PluginMode = "controller"
	// PluginModeNode - server only CSI Node endpoints
	PluginModeNode PluginMode = "node"
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
	// DefaultHostRootFS is the default path where the host root fs is mounted
	DefaultHostRootFS = "/host"
	// DefaultSnapDeviceID default value for the snap device ID
	DefaultSnapDeviceID = "6001"
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
	// PluginMode is mode in which plugin works, can be "node" or "controller"
	PluginMode PluginMode
	// ListOptions contains listener configuration for GRPC server
	ListenOptions ServerBindOptions
}

// Node contains options that are specific for the "node" mode
type Node struct {
	// name of the k8s node on which plugin is running
	NodeID string
	// the path where the host root fs is mounted
	HostRootFS string
	// device ID of the snap controller to use
	SnapControllerDeviceID string
}

// Controller contains options that are specific for the "controller" mode
type Controller struct {
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
