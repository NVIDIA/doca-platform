# Host Network Configuration Prerequisite

## Overview
To facilitate connectivity between the Data Processing Unit (DPU) and the Kubernetes control plane (API server) and the DHCP server on the management network, a fundamental network configuration is necessary on the host. The appropriate solution may vary depending on the specific environment. This guide provides a basic example for a host with Ubuntu 24.04 OS.

## Assumption
This configuration is intended to be implemented during the host provisioning phase, prior to its integration into the Kubernetes cluster.

## Host-DPU Network Diagram
The solution involves setting up a linux bridge with an IP address and connecting the Out-of-Band (OOB) uplink (a 1 Gbps Ethernet port) to it. During the DPU provisioning process, a Virtual Function (VF) will be created for the DPU and connected to the bridge to enable connectivity.
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
      # the mac address of the oob
      macaddress: xx:xx:xx:xx:xx:xx
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


## Host Routes and network acceleration

When the hosts are connected both to the management network via the OOB interface, and to the high-speed network via the Bluefield PFs or VFs, the default route should be configured via the OOB. This means that the outgoing traffic will by default go through the management network.

This is an issue in case some external traffic is reaching the host via the high-speed IP address. This causes asymmetric traffic routing. In addition, if the outgoing traffic is via the OOB port instead of high-speed port, the performance are far worse since the traffic is not accelerated. Similarly, traffic from high-speed network reaching the host via the OOB network will cause asymmetric traffic.
To fix this, some source-based routing on the host is necessary, so that traffic coming to the high speed network will leave the host via the high speed network, and traffic coming from the OOB interface will also leave the host via the OOB port.

The script below demonstrates an example of source-based routing to ensure proper network acceleration. It assumes that :

* 192.168.0.10/24 is the host IP on the management network (OOB interface) and the gateway is 192.168.0.1
* 10.0.0.10/22 is the host IP address on the high speed network (interface ens1f0np0) and the gateway is 10.0.0.1

```sh
# change routing table for traffic to high speed network from host OOB to use OOB port
ip rule add priority 200 from 192.168.0.10/32 to 10.0.0.0/22 table 200
# Route via OOB Gateway
ip r a default via 192.168.0.1 dev br-dpu table 200

# change routing table for traffic to anywhere from host high-speed IP
ip rule add priority 201 from 10.0.0.10/32 table 201
# route via the high speed gateway
ip r a default via 10.0.0.1 dev ens1f0np0 table 201
```

