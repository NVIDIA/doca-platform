---
title: "DOCA XPlane & DOCA Weave Additional Considerations"
---

[[_TOC_]]

# Additional Considerations

## How the fabric affects the deployment

Depending on the number of East/West (E/W) NICs, hardware planes, rails and on the fabric IP address scheme, the following manifests need to be adjusted. The existing manifests give a good starting point for what needs to change.

### DPUFlavor

* The number of rail bridges created - one per rail
* Rail bridge name is limited to 15 chars (linux netdev max name length)
* Uplink interfaces added to `br-xplane` - one uplink per rail per plane (i.e. rails × planes)

### DPUServiceConfiguration for doca-weave-dhcp-agent

* The number of DHCP networks to create and consume (`spec.serviceConfiguration.helmChart.values.weaveDHCPAgent.dhcpNetworks.networks`). Make sure to request the corresponding number of SFs.

### DPUServiceConfiguration for doca-weave-flow-controller

* `spec.serviceConfiguration.helmChart.values.weaveFlowController.underlayConfigMapData` must include:
  * `nicIDType`: The identifier scheme used for the NIC (`nicId`) in virtual network attachments. Only `mac` is supported.
  * `overlayNetworkPrefixLength`: The CIDR prefix length for each virtual network subnet (`subnetIpv4` / `subnetIpv6`; for example `12` for `/12`). Must match your underlying fabric IP scheme.
  * `softwarePlaneIDBitLength`: The number of address bits reserved for identifying the software plane (default 0).
  * `railIDBitLength`: The number of address bits reserved for identifying the rail within a software plane (default 0).
* The number of interface entries in `underlayConfigMapData.interfaces`
  * Each entry must contain the following fields:
    * `pciAddress`: The PCI address of the E/W NIC (any of the NIC's ECPFs may be specified)
    * `underlayInterface`: The matching E/W NIC rail bridge as created in the flavor
    * `overlayDHCPInterface`: The name of the DHCP interface as provided in the doca-weave-dhcp-agent DPUServiceConfiguration
    * `dhcpBridgeName`: The name of the DHCP bridge as provided in the doca-weave-dhcp-agent DPUServiceConfiguration
    * `dropBridgeName`: The bridge where unmatched traffic is dropped (no special constraints)

#### DPUServiceInterfaces

For each E/W NIC (Rail) we expect to have two DPUServiceInterfaces of type patch.

* For the rail bridge created in the flavor
* For the xplane bridge, to serve as downlink port (towards the matching rail bridge)

### DPUDeployment

For each E/W NIC (Rail) we expect to have a `switch` in the chain connecting the pair of
DPUServiceInterfaces described above.

## Kernel routes for multi-rail underlay

On multi-rail nodes, underlay IPv4 alone is not always enough: you usually install kernel routes on each `underlayInterface` so fabric traffic leaves through the right ToR peer (the `/31` gateway on that rail). Install one route per rail toward the cluster underlay aggregate. How you install routes (Netplan, `systemd-networkd`, `ip route`, or your image's network role) is cluster-specific. The examples below use `brcx-r0` / `brcx-r1`.

Illustrative addressing:

| Rail | Address on DPU | Device | ToR peer (gateway) |
|------|----------------|--------|-------------------|
| rail0 | `172.16.0.40/31` | `brcx-r0` | `172.16.0.41` |
| rail1 | `172.17.0.40/31` | `brcx-r1` | `172.17.0.41` |

### Rail routes

Each rail gets a route to the cluster underlay aggregate (`172.16.0.0/12` in this guide) via that rail's ToR peer. Example Netplan-style routes (one block per interface):

```yaml
# brcx-r0 (rail0)
routes:
  - to: 172.16.0.0/12
    via: 172.16.0.41
# brcx-r1 (rail1)
routes:
  - to: 172.16.0.0/12
    via: 172.17.0.41
```

Replace `172.16.0.0/12` with the prefix your network design requires.

The Weave service (`weave-flow-controller`) also programs source-based routing in OVS so underlay traffic for each rail is sent out that rail's NIC, keeping egress aligned with the source address on that interface. Behavior is gated by the `ENABLE_SRC_BASED_ROUTE` environment variable on the `weave-flow-controller` (default `true`).

## Host DHCP clients for overlay

Host overlay interfaces attached to a Weave virtual network obtain overlay IPv4 addresses and overlay routes from the Weave
DHCP agent. Overlay leases are `/31`. When Spectrum-X host-id is 0, the overlay address is `*.*.*.0` and
systemd-networkd may install option 121 routes with `scope host` instead of via the `/31` peer. Use NetworkManager on
those data-path interfaces.

See [Host DHCP clients for Weave overlay addressing and routes](dhcp-client-guidance.md).

## Accessing the DPUCluster

Some commands in this guide require direct access to the DPU k8s cluster.

below is a sample command of extracting the kubeconfig of a particular `DPUCluster`.
It can then be used together with `kubectl` command to perform operations on the DPU k8s cluster.

```shell
kubectl get secrets -n dpu-cplane-tenant1 dpu-cplane-tenant1-admin-kubeconfig -o jsonpath="{.data.admin\.conf}" | base64 --decode > ~/dpu-cluster1.kubeconfig
```