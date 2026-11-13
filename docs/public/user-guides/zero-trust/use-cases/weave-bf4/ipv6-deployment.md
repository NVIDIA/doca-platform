---
title: "DOCA XPlane & DOCA Weave over IPv6"
---

This page is a delta on the [XPlane Weave deployment guide](README.md). Follow that guide from start to finish and
replace the environment variable values listed here. Every manifest under `manifests/` is shared between the two
address families, including the DPUFlavorTemplate. The only manifest edit is optional, selecting dhcpv6 instead of
the default slaac.

> [!IMPORTANT]
> Weave runs a single address family per deployment. An IPv6 underlay means an IPv6 overlay as well, there is no
> dual-stack mode.

[[_TOC_]]

## Fabric addressing

This example uses a dual-plane Spectrum-X RA 2.1.5 IPv6 fabric with one `/64` per NIC. It replaces the IPv4 scheme in
[Hardware and fabric Prerequisites](README.md#hardware-and-fabric-prerequisites):

```text
fd02:00[plane-id]:[rail-id][pod-id]:[su-id][host-id]::[p2p]/64
```

Each bracketed field is one byte, written in hex:

| Field | This example |
|-------|--------------|
| `[plane-id]` | `00` and `01`, one per software plane |
| `[rail-id]` | `00`, a single rail |
| `[pod-id]` | `00`, two-tier |
| `[su-id]` | `01` |
| `[host-id]` | `14` and `15`, one per DPU |
| `[p2p]` | `01` on the DPU, `02` on the leaf downlink port |

`[plane-id]` is the software plane, so `fd02:0:...` is software plane 0 and `fd02:1:...` is software plane 1.

That gives the following prefixes:

* overall network: `fd02::/24`
* software plane 0: `fd02:0::/32`
* software plane 1: `fd02:1::/32`
* rail networks:
  * rail 0 on software plane 0: `fd02:0:0::/40`
  * rail 0 on software plane 1: `fd02:1:0::/40`

> [!NOTE]
> These addresses are an example only, the same caveat the IPv4 guide carries. IPv6 fabrics differ in their prefix
> layout and in how many bits they give the plane and rail IDs, so adjust the values here to your own fabric.

Those three prefix lengths are what the flow controller encodes in `underlayConfigMapData`:

| Field | IPv4 | IPv6 | IPv6 boundary |
|-------|------|------|---------------|
| `overlayNetworkPrefixLength` | 12 | 24 | the fixed `fd02:00` part of the layout |
| `softwarePlaneIDBitLength` | 1 | 8 | `[plane-id]`, taking the prefix to `/32` |
| `railIDBitLength` | 3 | 8 | `[rail-id]`, taking the prefix to `/40` |

## Environment variables

Set these in `manifests/00-env-vars/envvars.env` in place of the IPv4 values, then source the file as described in
[Required Variables](README.md#0-required-variables). Everything else in that file is unchanged.

```sh
## Underlay address plan
export OVERLAY_NETWORK_PREFIX_LENGTH=24
export SOFTWARE_PLANE_ID_BIT_LENGTH=8
export RAIL_ID_BIT_LENGTH=8

## RAIL_NETWORK_SWP0 is the prefix for the rail network over software plane 0
## Example: fd02:0::/32
export RAIL_NETWORK_SWP0=
## RAIL_NETWORK_SWP1 is the prefix for the rail network over software plane 1
## Example: fd02:1::/32
export RAIL_NETWORK_SWP1=

## DPU 1, su-id 01 host-id 14
## Example: fd02:0:0:114::1/64
export DPU1_RAIL0_SWP0_IP=
## Example: fd02:0:0:114::2
export DPU1_RAIL0_SWP0_GW=
## Example: fd02:1:0:114::1/64
export DPU1_RAIL0_SWP1_IP=
## Example: fd02:1:0:114::2
export DPU1_RAIL0_SWP1_GW=

## DPU 2, su-id 01 host-id 15
## Example: fd02:0:0:115::1/64
export DPU2_RAIL0_SWP0_IP=
## Example: fd02:0:0:115::2
export DPU2_RAIL0_SWP0_GW=
## Example: fd02:1:0:115::1/64
export DPU2_RAIL0_SWP1_IP=
## Example: fd02:1:0:115::2
export DPU2_RAIL0_SWP1_GW=
```

Each underlay interface takes `::1` of its own `/64` and routes its software plane prefix via `::2`, the ToR peer on that rail.

> [!NOTE]
> The `weave-flow-controller` chart exposes an `UNDERLAY_IP_FAMILY` environment variable. It defaults to `auto`,
> where the controller discovers the family from the underlay bridge addresses, so the manifests leave it unset.

## Overlay address assignment

`underlayConfigMapData.ipv6AssignmentMode` decides how host PFs get their overlay address. It defaults to `slaac`,
so the manifests set nothing and the rest of this page assumes that mode.

| | `slaac`, the default | `dhcpv6` |
|-|----------------------|----------|
| Host overlay address | autoconfigured from the Router Advertisement prefix | leased by `weave-dhcp-agent` |
| Attachment status | no address, read it from the host | the leased address in `status.hostIpv6` |
| Netplan on the host PF | `accept-ra: true` | `accept-ra: true` and `dhcp6: true` |

Overlay routes come from RA Route Information Options in both modes, DHCPv6 carries no route option, so the PFs
always need `accept-ra`.

To lease addresses instead, add the field to `underlayConfigMapData` in
`manifests/03-xplane-weave-dpudeployment/dpuserviceconfig-doca-weave-flow-controller.yaml`:

```yaml
          underlayConfigMapData:
            ipv6AssignmentMode: dhcpv6
```

## Verification

After [Create BlueFieldSoftware and DPUFlavorTemplate](README.md#3-create-bluefieldsoftware-and-dpuflavortemplate)
and [Populate DPUDevices with E/W Network values](README.md#4-populate-dpudevices-with-ew-network-values),
`/etc/netplan/99-rails.yaml` on DPU 1 renders as follows, with the example values above:

```yaml
network:
  version: 2
  ethernets:
    brcx-r0swp0:
      addresses:
        - fd02:0:0:114::1/64
      routes:
        - to: fd02:0::/32
          via: fd02:0:0:114::2

    brcx-r0swp1:
      addresses:
        - fd02:1:0:114::1/64
      routes:
        - to: fd02:1::/32
          via: fd02:1:0:114::2
```

## Configure network isolation for E/W NICs and send traffic

This replaces
[step 6](README.md#6-optional---configure-network-isolation-for-ew-nics-and-send-traffic) of the main guide. The host
PF names, representor names and flow-controller pod lookup are unchanged, only the overlay subnet differs:

```shell
# virtual network configuration
VNET_OVERLAY_SUBNET=f001::/24
VNET_VNI=100
```

The subnet prefix length must match `OVERLAY_NETWORK_PREFIX_LENGTH`.

### Setup network isolation using DOCA Weave

Create the virtual network with `--subnet-v6` instead of `--subnet-v4`, the attachments are unchanged. The commands
below are executed against the DPU K8s cluster
([how to access dpucluster](additional-considerations.md#accessing-the-dpucluster)).

```shell
# Create the virtual network and attachments on each DPU
for pod in $WEAVE_FLOW_CONTROLLER_PODS; do
  kubectl exec -n dpf-operator-system "$pod" -- /vpcctl create-vnet --id shared-vnet --subnet-v6 "$VNET_OVERLAY_SUBNET" --vni "$VNET_VNI"
  kubectl exec -n dpf-operator-system "$pod" -- /vpcctl create-attachment --id "$NIC0_PF0_REP" --vnet-id shared-vnet --type pf --rep "$NIC0_PF0_REP"
  kubectl exec -n dpf-operator-system "$pod" -- /vpcctl create-attachment --id "$NIC1_PF0_REP" --vnet-id shared-vnet --type pf --rep "$NIC1_PF0_REP"
done
```

> [!NOTE]
> In the default `slaac` mode the attachment status carries no address, read it from the host as shown below. With
> `dhcpv6` the leased address appears as `status.hostIpv6`, see
> [Overlay address assignment](#overlay-address-assignment).

### Obtain overlay addresses

Overlay routes arrive in Router Advertisements, so the PFs always need `accept-ra`. Under `dhcpv6` add `dhcp6: true`
as well, the address itself is leased rather than autoconfigured. Keep one VRF per rail, both planes still advertise
overlapping overlay prefixes.

> [!NOTE]
> The NetworkManager workaround in
> [Host DHCP clients for Weave overlay addressing and routes](dhcp-client-guidance.md) is IPv4 only. IPv6 overlay
> prefixes arrive as RA Route Information Options rather than DHCP option 121, which systemd-networkd, the Netplan
> default, handles correctly.

```shell
# Create the below netplan file on both worker nodes, add "dhcp6: true" per PF under ipv6AssignmentMode: dhcpv6
cat > /etc/netplan/99-rails.yaml << EOF
network:
  version: 2
  ethernets:
    $NIC0_PF_NAME:
      accept-ra: true
    $NIC1_PF_NAME:
      accept-ra: true
  vrfs:
    rail0swp0:
      table: 100
      interfaces: [$NIC0_PF_NAME]
    rail0swp1:
      table: 101
      interfaces: [$NIC1_PF_NAME]
EOF
netplan apply

```

Read the address each PF picked up and confirm the overlay prefixes landed in the rail's table:

```shell
ip -6 addr show dev $NIC0_PF_NAME scope global
ip -6 addr show dev $NIC1_PF_NAME scope global
ip -6 route show vrf rail0swp0
ip -6 route show vrf rail0swp1
```

### Send traffic

Ping from one node to the addresses the other node picked up, one per software plane, each out of the matching rail
VRF. Under `slaac` the host chooses the address suffix, so read the targets with the command above rather than
assuming them.

**Example:**

```shell
# On worker node 2, read the global address of each PF, both fall inside the overlay subnet
ip -6 -brief addr show dev $NIC0_PF_NAME scope global
ip -6 -brief addr show dev $NIC1_PF_NAME scope global

# On worker node 1, ping each one from the rail VRF that carries it
NODE2_SWP0_IP=<address shown for $NIC0_PF_NAME>
NODE2_SWP1_IP=<address shown for $NIC1_PF_NAME>

# rail 0, software plane 0
ip vrf exec rail0swp0 ping -6 "$NODE2_SWP0_IP"
# rail 0, software plane 1
ip vrf exec rail0swp1 ping -6 "$NODE2_SWP1_IP"
```

A fabric with more than one rail is reached the same way, the rail ID sits in the overlay address and the ping still
leaves from the VRF of that rail. This deployment has a single rail, so the target below is illustrative,
`f001:0:100::/40` is rail 1 on software plane 0, so it is reached from `rail1swp0`:

```shell
# rail 1, software plane 0, suffix read from the remote host as above
ip vrf exec rail1swp0 ping -6 f001:0:100:115:<suffix>
```

### Cleanup netplan files and configuration

```shell
rm /etc/netplan/99-rails.yaml
netplan apply
ip link del rail0swp0
ip link del rail0swp1
```

Tear down the virtual network as described in
[Teardown network isolation using DOCA Weave](README.md#teardown-network-isolation-using-doca-weave), those commands
are the same for both address families.
