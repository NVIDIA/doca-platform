---
title: "DOCA Weave DHCP Client Guidance"
---

This page is for operators who configure **data-path interfaces** attached to a Weave virtual network so those
interfaces obtain an overlay IPv4 address and overlay routes from the Weave DHCP agent.

This guidance is **IPv4 DHCP only**. IPv6 overlay prefixes are advertised with Router Advertisement Route Information
Options, not DHCP option 121.

A related but different `/31` constraint applies to **DPU underlay** addressing. See
[Populate DPUDevices with E/W Network values](README.md#4-populate-dpudevices-with-ew-network-values).

[[_TOC_]]

## What Weave DHCP advertises

Weave overlay IPv4 addressing is a point-to-point **/31** (RFC 3021). Both addresses in the `/31` are usable hosts.
There is no dedicated network or broadcast address.

Spectrum-X copies the underlay host bits into the overlay address. When the Spectrum-X **host-id is 0**, the Weave DHCP
agent leases the `/31` zero address (last octet `.0`), for example `44.16.0.0/31`, which matches the subnet network id.
The DHCP next-hop is the peer (`44.16.0.1`).

The `weave-dhcp-agent` sends **DHCP option 121** (classless static routes) for the overlay subnet via the peer
gateway.

## Known limitation with systemd-networkd

Ubuntu Netplan defaults to **systemd-networkd**.

When the overlay lease is `*.*.*.0` (Spectrum-X host-id 0), systemd-networkd treats that address as a network id and
installs the connected prefix and/or option 121 destinations with **`scope host`**. Those routes stay local to the
interface and are not forwarded via the `/31` peer, so overlay east/west traffic fails.

Typical symptom:

* The PF has an IPv4 address (`ip -4 addr` shows `44.16.0.0/31` or similar).
* `ip route show vrf <rail>` (or the main table if you did not use a VRF) shows overlay prefixes with `scope host`
  instead of `via <dhcp-peer>`.
* Example of a bad route: `44.16.0.0/12 dev ens6np0 proto dhcp scope host`
* Example of a good route: `44.16.0.0/12 via 44.16.0.1 dev ens6np0 proto dhcp`

NetworkManager does not apply that host-id 0 restriction and installs option 121 via the peer.

> [!NOTE]
> This is a systemd-networkd bug with `/31` point-to-point links, tracked in
> [systemd/systemd#43841](https://github.com/systemd/systemd/issues/43841). The fix is merged upstream in
> [systemd/systemd#43843](https://github.com/systemd/systemd/pull/43843) but is not backported yet, so keep the
> NetworkManager workaround below until your distribution ships it.

## Impact

DPU isolation (OpenFlow) and `vpcctl` attachments can be READY while host overlay destinations are installed with
`scope host`. Those prefixes stay local to the PF and are not sent via the `/31` peer. Workloads that use those PFs
cannot reach other overlay endpoints until a compatible DHCP client installs option 121 as unicast
routes via the `/31` peer.

## Recommended workaround

Use **NetworkManager** as the DHCP client on **only** the Weave data-path interfaces (for example: the host PFs).
Leave management and OOB interfaces on systemd-networkd if that is already the cluster standard.

Prerequisite: NetworkManager is installed and running on the worker.

```shell
systemctl is-active NetworkManager
```

Keep one VRF per rail so both rails can advertise the same overlay supernet without conflicting routes.

Netplan example aligned with [Obtain IPs using DHCP](README.md#obtain-ips-using-dhcp). Set `renderer: NetworkManager`
per device, on each ethernet and on each VRF that owns them, so every other host NIC stays on systemd-networkd. Setting
the renderer on the VRF as well keeps the master device and its member on the same backend.

The VRF is only a routing container and needs no address of its own. Netplan renders `[ipv4] method=link-local` for a
NetworkManager device that has no IP configuration, which leaves a `169.254.x.y/16` address on every VRF, so disable
both families on the master with `networkmanager.passthrough`. See
[IPv4 link-local addresses on the rails](#ipv4-link-local-addresses-on-the-rails).

> [!WARNING]
> Do not set a top-level `renderer: NetworkManager` under `network:`. Netplan merges every file in `/etc/netplan/`
> into one configuration, so a top-level renderer in any file becomes the default for the whole machine and moves
> the management and OOB interfaces off systemd-networkd, which can drop your connection to the worker.

The lease carries addresses and routes, not the link MTU, so the same file sets `mtu` on the PFs. See
[Set the MTU on the data-path interfaces](#set-the-mtu-on-the-data-path-interfaces).

```yaml
network:
  version: 2
  ethernets:
    ens6np0:
      renderer: NetworkManager
      dhcp4: true
      mtu: 9000
    ens5np0:
      renderer: NetworkManager
      dhcp4: true
      mtu: 9000
  vrfs:
    rail0swp0:
      renderer: NetworkManager
      table: 100
      interfaces: [ens6np0]
      networkmanager:
        passthrough:
          ipv4.method: "disabled"
          ipv4.link-local: "disabled"
          ipv6.method: "disabled"
    rail0swp1:
      renderer: NetworkManager
      table: 101
      interfaces: [ens5np0]
      networkmanager:
        passthrough:
          ipv4.method: "disabled"
          ipv4.link-local: "disabled"
          ipv6.method: "disabled"
```

Replace `ens6np0` / `ens5np0` with the PF names on your workers, then apply the configuration:

```shell
sudo netplan apply
```

Netplan copies `networkmanager.passthrough` keys into the generated keyfile without validating them, so a typo is
silently ignored. Confirm they landed on the connection, which netplan names `netplan-<device>`.

```shell
nmcli -f ipv4.method,ipv4.link-local,ipv6.method connection show netplan-rail0swp0
```

`ipv4.link-local` needs NetworkManager 1.40 or later. Older versions ignore the key, which is harmless here because
`ipv4.method: "disabled"` already rules out link-local addressing.

## Set the MTU on the data-path interfaces

The `weave-dhcp-agent` lease carries no MTU, so netplan has to set it. The DPUFlavorTemplate requests
`mtu_request=9216` on the DPU uplinks and rail ports, so **9000** on the host PFs leaves room for the overlay headers
the DPU adds. Set `mtu` on the ethernets, as in the [recommended workaround](#recommended-workaround), the VRF master
needs none of its own.

```yaml
network:
  version: 2
  ethernets:
    ens6np0:
      renderer: NetworkManager
      dhcp4: true
      mtu: 9000
```

> [!NOTE]
> `9000` is an example. Any value works while it stays under the DPU uplink MTU minus the VXLAN overhead the DPU adds,
> 50 bytes over an IPv4 underlay and 70 over IPv6, counting the outer Ethernet, IP and UDP headers, the VXLAN header
> and the inner Ethernet header it carries. With `mtu_request=9216` that caps the host PFs at 9166 on IPv4 and 9146
> on IPv6, so 9000 is safe for either family.

Jumbo frames have to be enabled end to end. An underlay switch port or DPU uplink with a smaller MTU drops large
frames with no sign of it on the host while ARP and a default sized ping keep working, so confirm the path with a
do-not-fragment ping sized to the MTU (9000 minus 28 bytes of IPv4 and ICMP header):

```shell
ip link show ens6np0
ip vrf exec rail0swp0 ping -M do -s 8972 44.16.0.4
```

## Verify overlay routes

Confirm the lease and that option 121 landed as a unicast route via the DHCP peer, **not** `scope host`.

```shell
ip -4 addr show ens6np0
ip route show vrf rail0swp0
```

You should see the overlay supernet via the peer, for example:

```text
44.16.0.0/12 via 44.16.0.1 dev ens6np0 proto dhcp
```

If the same prefix appears with `scope host` and no `via`, the worker is still using systemd-networkd on that PF.

## IPv4 link-local addresses on the rails

A `169.254.x.y/16` address on a rail is IPv4 link-local (RFC 3927, also known as APIPA). NetworkManager self-assigns
it, the Weave DHCP agent never leases it.

Typical symptom:

* The VRF master has an address it should not have: `ip -4 addr show rail0swp0` shows
  `inet 169.254.x.y/16 brd 169.254.255.255 scope link`.
* `ip route show vrf rail0swp0` shows `169.254.0.0/16 dev <device> scope link` next to, or instead of, the overlay
  prefixes.
* The PF has no overlay lease: `ip -4 addr show ens6np0` shows a `169.254.x.y/16` address and no `/31`.

The cause differs per device:

* **VRF master.** Expected fallout of `[ipv4] method=link-local`, which is what netplan renders for a NetworkManager
  device with no IP configuration. The `networkmanager.passthrough` keys in the
  [recommended workaround](#recommended-workaround) turn it off.
* **Host PF.** NetworkManager fell back to link-local because the overlay lease never completed. The address is the
  symptom, not the cause: check that the `vpcctl` PF attachment exists, that `weave-dhcp-agent` runs on the DPU and
  that the DISCOVER leaves on the expected plane.

The NetworkManager journal separates the two cases, a failed lease logs a DHCPv4 timeout right before the link-local
address is configured.

```shell
nmcli device show ens6np0 | grep -E 'DHCP4|IP4'
journalctl -u NetworkManager -b | grep -iE 'dhcp|169\.254'
```

On NetworkManager 1.40 and later you can keep the fallback off the PFs as well, so a failed lease shows up as no IPv4
address instead of a plausible looking one.

```yaml
network:
  version: 2
  ethernets:
    ens6np0:
      renderer: NetworkManager
      dhcp4: true
      networkmanager:
        passthrough:
          ipv4.link-local: "disabled"
```

> [!WARNING]
> Do not disable link-local on the PFs with netplan's `link-local: []`. That key covers IPv6 as well, and the IPv6
> overlay prefixes arrive in Router Advertisements that need an IPv6 link-local address on the interface.

`netplan apply` does not remove an address that is already on a device, so flush the leftover one after fixing the
configuration.

```shell
sudo ip addr flush dev rail0swp0
```

## Stale routes after switching renderer

Handing an interface from systemd-networkd to NetworkManager can leave the old `scope host` entries behind, so a
correct Netplan file still shows bad routes. Flush the rail's table (the `table:` value from the VRF, `100` here) and
bounce the link so NetworkManager takes a fresh lease.

```shell
sudo ip route flush table 100 dev ens6np0
sudo ip link set ens6np0 down
sudo ip link set ens6np0 up
```
