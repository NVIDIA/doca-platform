/*
Copyright 2024 NVIDIA.

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

// DPUCNIProvisionerConfig is the format of the config file the DPU CNI Provisioner is expecting. Notice that
// this is one config for all DPU cluster nodes because this is how the Operator is going to deploy that.
type DPUCNIProvisionerConfig struct {
	// PerNodeConfig represents the configuration settings for each particular DPU cluster node. Key is the name of the
	// DPU cluster node.
	PerNodeConfig map[string]PerNodeConfig `json:"perNodeConfig"`
	// VTEPCIDR is the CIDR in which all the VTEP IPs of all the DPUs in the DPU cluster belong to. The provisioner adds
	// a routes using that information to enable traffic that needs to go from one Pod running on worker Node A to
	// another Pod running on worker Node B.
	VTEPCIDR string `json:"VTEPCIDR"`
	// HostCIDR is the CIDR of the host machines. The primary IPs of the hosts are part of this CIDR. The provisioner
	// will add a route for that CIDR related to traffic between Pods running on control plane and worker nodes.
	HostCIDR string `json:"hostCIDR"`
}

// PerNodeConfig is the struct that holds configuration specific to a particular node. This config is extracted out of
// the DPUCNIProvisionerConfig based on the node the DPU CNI Provisioner is running on.
type PerNodeConfig struct {
	// HostPF0 is the name of the PF0 on the host. This value is used for configuring the name of the br-ex OVS bridge.
	HostPF0 string `json:"hostPF0"`
	// VTEPIP repesents the IPs that will be assigned to the VTEP interface on each DPU.
	VTEPIP string `json:"vtepIP"`
	// Gateway is the gateway IP that will be configured on the routes related to OVN Kubernetes reaching its peer nodes
	// when traffic needs to go from one Pod running on Node A to another Pod running on Node B.
	Gateway string `json:"gateway"`
}
