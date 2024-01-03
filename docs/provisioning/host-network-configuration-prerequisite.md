# Host Network Configuration Prerequisite

## Overview
To facilitate connectivity between the Data Processing Unit (DPU) and the Kubernetes control plane (API server) and the DHCP server on the management network, a fundamental network configuration is necessary on the host. The appropriate solution may vary depending on the specific environment. This guide provides a basic example for a host with Ubuntu 24.04 OS.

## Assumption
This configuration is intended to be implemented during the host provisioning phase, prior to its integration into the Kubernetes cluster.

## Host-DPU Network Diagram
The solution involves setting up a network bridge with an IP address and connecting the Out-of-Band (OOB) uplink (a 1 Gbps Ethernet port) to it. During the DPU provisioning process, a Virtual Function (VF) will be created for the DPU and connected to the bridge to enable connectivity.
```
             +--------------------------------+  +-------------+
             | Host                           |  | DPU         |
             |        +---------+             |  |             |
Management---|--OOB---|  br-dpu |--ensXf0vf0--|--|--pf0vf0     |
  Network    | uplink +---------+             |  | representor |
             | (No IP)    IP                  |  |             |
             |                                |  |             |
             +--------------------------------+  +-------------+
```

## Example Solution - Set Up The Bridge Using Netplan 
Utilize  [Netplan](https://netplan.readthedocs.io/en/stable/) to configure the necessary network interfaces. Netplan configuration files are executed at boot time before the kubelet process.

The following YAML illustrates the requirements for the Netplan configuration. It ensures the following:
1. No IP, gateway or DNS nameservers is configured on the `oob` interface
2. The `oob` interface is connected to the bridge `br-dpu`.
3. The bridge is always named `br-dpu`. The name of the OOB interface does not matter.
4. All network connectivity requirements, such as IP, gateway, and nameservers, are configured on br-dpu.
```yaml
network:
  ethernets:
    oob:
      # dhcp4 is always no for oob
      dhcp4: no
      match:
        # the mac address of the oob
        macaddress: xx:xx:xx:xx:xx:xx
      set-name: oob
  bridges:
    br-dpu:
      interfaces: [oob]
      critical: true
      # other bridge configurations
  # uncomment the line below to use NetworkManager as backend renderer 
  # renderer: NetworkManager
  version: 2
```

In addition to the above configuration, an IP address and other settings are required to make br-dpu work. You can either set them statically or configure them through DHCP
### Option 1: Set br-dpu through DHCP
To let the `br-dpu` get its configuration via DHCP, add `dhcp4: yes` like the following:
```yaml
network:
  ethernets:
    oob:
      # dhcp4 is always no for oob
      dhcp4: no
      match:
        # the mac address of the oob
        macaddress: xx:xx:xx:xx:xx:xx
      set-name: oob
  bridges:
    br-dpu:
      interfaces: [oob]
      critical: true
      dhcp4: yes
      dhcp4-overrides:
        route-metric: 5
  version: 2
```
> Note:
> 
> Please ensure that the configuration on `br-dpu` matches the configuration on your DHCP server. You may need additional configurations to make the DHCP procedure work. For example:
> 1. Set `macaddress` to explicitly assign a MAC address to `br-dpu`
> 2. Set `dhcp-identifier: mac` to use MAC address as the DHCP client identifier

### Option 2: Set br-dpu statically
If DHCP is disabled on br-dpu, you must assign an IP address and a default gateway to `br-dpu`. `nameservers` are optional
```yaml
network:
  ethernets:
    oob:
      #dhcp4 is always no for oob
      dhcp4: no
      match:
        # the mac address of the oob
        macaddress: xx:xx:xx:xx:xx:xx
      set-name: oob
  bridges:
    br-dpu:
      interfaces: [oob]
      critical: true
      # the addresses of the bridge
      addresses: ["xx.xx.xx.xx"]
      routes:
        # the default gateway for the bridge
        - to: default
          via: "xx.xx.xx.xx"
          metric: 5
      # DNS nameservers for the bridge
      nameservers:
        addresses:
          - xx.xx.xx.xx
        search:
          - "xxx"
  version: 2
```
> Note:
> 
> The metric of the default route must be at least 2
