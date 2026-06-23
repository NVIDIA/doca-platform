---
title: "DOCA Weave - Deployment"
---

[[_TOC_]]

## Overview

This guide covers DPF deployment and wiring for DOCA Weave. East-west underlay topology and network configuration are site-specific, align them with your network design and the manifests in this page before you install.

## Prerequisites and underlay topology

Before installing, complete the [DPF prerequisites](#dpf-and-cluster-prerequisites) and confirm that OVS bridges, ports, and `serviceChains` match Weave's per-NIC settings.

### DPF and cluster prerequisites

Complete the [DPF Prerequisites for Zero Trust](../../../user-guides/zero-trust/prerequisites/README.md) (including [system prerequisites](../../../user-guides/zero-trust/prerequisites/system.md) and [Helm prerequisites](../../../getting-started/helm-prerequisites.md)). In this step, install only the DPF control plane. Do not deploy other DPUServices or provision DPUs yet. For the full Zero Trust platform workflow, see [Zero Trust deployment](../../../user-guides/zero-trust/README.md). This guide covers deployment steps and requirements for DOCA Weave only.

### Underlay fabric topology

Underlay layout is site-specific. On `DPUDeployment`, `spec.serviceChains` and `DPUServiceInterface` attach each NIC to `peerBridge` / `underlayInterface` and to per-NIC DHCP `bridge` settings, keep that consistent with `underlayConfigMapData` and your site design.

Create underlay-related OVS bridges (the bridges through which traffic eventually egresses onto the underlay fabric) in `DPUFlavor` `spec.ovs.rawConfigScript`. In the [example](#dpuflavor-underlay-bridges-for-weave) in this guide, that script creates `br-underlay-n0` and `br-underlay-n1`. These bridges may be chained directly to the east-west NIC physical ports or to XPLANE, depending on your site. Assign underlay IPv4 on the bridge named in `underlayInterface` (see [Underlay IPv4 and bridge names](#underlay-ipv4-and-bridge-names)). Full ASTRA, Spectrum-X, and XPLANE fabric designs are outside the scope of this guide.

### Underlay IPv4 and bridge names

The IPv4 addresses on `underlayInterface` are used for VXLAN encapsulation (examples use `br-underlay-n*`). Assigning addresses to those underlay interfaces is outside the scope of this guide. The flow controller reads `underlayConfigMapData` (see [`weave-flow-controller` environment variables](README.md#weave-flow-controller-environment-variables)) and uses the IPv4 address assigned on the interfaces you specify.

DOCA Weave requires a `/31` address on each `underlayInterface`: one address on the DPU and the peer on the ToR port for that link. Weave uses the DPU address for tunnel programming.

Multi-rail nodes have several east-west NICs, each connected to its own rail network (for example `br-underlay-n0` and `br-underlay-n1` in this guide). On multi-rail nodes you also need kernel routes on each underlay device. See [Kernel routes for multi-rail underlay](#kernel-routes-for-multi-rail-underlay).

DOCA Weave expects the underlay bridges named in `underlayInterface` to already exist on the DPU before the service starts. See [Underlay configuration](README.md#underlay-configuration) in the DOCA Weave service guide.

### Kernel routes for multi-rail underlay

On multi-rail nodes, underlay IPv4 alone is not always enough: you usually install kernel routes on each `underlayInterface` so fabric traffic leaves through the right ToR peer (the `/31` gateway on that rail). Common patterns are same-rail aggregates (a different prefix per rail) and cross-rail reachability (often a shared destination via each rail’s gateway). How you install routes (Netplan, `systemd-networkd`, `ip route`, or your image’s network role) is cluster-specific, the examples below use `br-underlay-n0` / `br-underlay-n1`.

Illustrative addressing:

| Rail | Address on DPU | Device | ToR peer (gateway) |
|------|----------------|--------|-------------------|
| rail0 | `172.0.1.40/31` | `br-underlay-n0` | `172.0.1.41` |
| rail1 | `172.32.1.40/31` | `br-underlay-n1` | `172.32.1.41` |

#### Same-rail routes

Same-rail routes steer traffic for that rail’s local fabric aggregate out the matching `underlayInterface`. Example Netplan-style routes (one block per interface). Adapt the routes to your network design:

```yaml
# br-underlay-n0 (rail0)
routes:
  - to: 172.0.0.0/11
    via: 172.0.1.41
# br-underlay-n1 (rail1)
routes:
  - to: 172.32.0.0/11
    via: 172.32.1.41
```

Because the destinations differ per rail, these entries usually coexist in the main routing table without conflict.

#### Cross-rail routes

Cross-rail examples often use the same destination prefix with different gateways on different devices. For example:

```yaml
# br-underlay-n0 (rail0)
routes:
  - to: 172.0.0.0/8
    via: 172.0.1.41
# br-underlay-n1 (rail1)
routes:
  - to: 172.0.0.0/8
    via: 172.32.1.41
```

Replace `172.0.0.0/8` with the prefixes your network design requires.

The Weave service (`weave-flow-controller`) also programs source-based routing in ovs so underlay traffic for each rail is sent out that rail’s NIC, keeping egress aligned with the source address on that interface. Behavior is gated by `ENABLE_SRC_BASED_ROUTE` (default `true`). See [`weave-flow-controller` environment variables](README.md#weave-flow-controller-environment-variables) for what is mirrored from the kernel’s main routing table.

## DOCA Weave component placement

Deploy DOCA Weave on every DPU that requires isolation for east-west traffic. The manifests below define `DPUDeployment` and `DPUFlavor` to run `weave-flow-controller` and `weave-dhcp-agent` on those DPUs.

### `DPUDeployment`

Notes:

* `spec.services` wires `weave-flow-controller` and `weave-dhcp-agent` to their `DPUServiceTemplate` and `DPUServiceConfiguration` objects.
* `spec.serviceChains` depends on your underlay topology (for example XPLANE or PF-based paths). Align it with `DPUServiceInterface` as described under [Underlay fabric topology](#underlay-fabric-topology).
* The reference snippet below omits `serviceChains`, define it per [Zero Trust deployment](../../../user-guides/zero-trust/README.md) and your site fabric.
* Checked-in example: `dpuservices/weave/DPUDeployment.yaml`. Underlay bridges come from the `DPUFlavor` [below](#dpuflavor-underlay-bridges-for-weave) (`dpuservices/weave/DPUFlavor.yaml`).

<details markdown="1"><summary>Reference Example</summary>

[embedmd]:#(../../../../../dpuservices/weave/DPUDeployment.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUDeployment
metadata:
  name: doca-weave
  namespace: dpf-operator-system
spec:
  dpus:
    bfb: bf-bundle-v26.5.0
    flavor: doca-weave-flavor-v26.5.0
    nodeEffect:
      noEffect: true
    dpuSets:
    - nameSuffix: "dpuset1"
      dpuNodeSelector:
        matchLabels:
          feature.node.kubernetes.io/dpu-enabled: "true"
  services:
    weave-flow-controller:
      serviceTemplate: weave-flow-controller
      serviceConfiguration: weave-flow-controller
    weave-dhcp-agent:
      serviceTemplate: weave-dhcp-agent
      serviceConfiguration: weave-dhcp-agent
  # serviceChains: define per XPLANE / PF based on your underlay topology.
```
</details>

### `DPUFlavor` (underlay bridges for Weave)

`spec.ovs.rawConfigScript` must create the underlay bridges named in `underlayConfigMapData.interfaces[].underlayInterface` (for example `br-underlay-n0`, `br-underlay-n1`). See [Underlay configuration](README.md#underlay-configuration) for which bridges you provide versus which the chart creates. Use the Weave reference `DPUFlavor` below, or add its OVS underlay bridges to your existing site `DPUFlavor`. See [DPUFlavor API](../../../developer-guides/api/dpuflavor.md). Set `spec.dpus.flavor` to the flavor `metadata.name` you deploy. This reference adds `br-underlay-n*`, `br-sfc`, `ecpf0`/`ecpf1` for Weave.

<details markdown="1"><summary>Reference Example</summary>

[embedmd]:#(../../../../../dpuservices/weave/DPUFlavor.yaml)
```yaml
---
# Reference DPUFlavor illustrating OVS bridges for DOCA Weave (dpf-weave).
# Shaped like published DPUFlavor examples (bfcfg, configFiles, grub, nvconfig, spec.ovs), see
# docs/public/developer-guides/api/dpuflavor.md. Merge OVS underlay bridges into your site DPUFlavor if you already have one.
# Underlay br-underlay-n0 / br-underlay-n1 must match underlayConfigMapData.interfaces[].underlayInterface.
# br-sfc / ecpf0, ecpf1: align with DPUServiceInterface / serviceChains and your site.
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavor
metadata:
  name: doca-weave-flavor-v26.5.0
  namespace: dpf-operator-system
spec:
  dpuMode: zero-trust
  bfcfgParameters:
  - UPDATE_ATF_UEFI=yes
  - UPDATE_DPU_OS=yes
  - WITH_NIC_FW_UPDATE=yes
  configFiles:
  - operation: override
    path: /etc/mellanox/mlnx-bf.conf
    permissions: "0644"
    raw: |
      ALLOW_SHARED_RQ="no"
      IPSEC_FULL_OFFLOAD="no"
      ENABLE_ESWITCH_MULTIPORT="yes"
  - operation: override
    path: /etc/mellanox/mlnx-ovs.conf
    permissions: "0644"
    raw: |
      CREATE_OVS_BRIDGES="no"
      OVS_DOCA="yes"
  - operation: override
    path: /etc/mellanox/mlnx-sf.conf
    permissions: "0644"
    raw: ""
  grub:
    kernelParameters:
    - console=hvc0
    - console=ttyAMA0
    - earlycon=pl011,0x13010000
    - fixrttc
    - net.ifnames=0
    - biosdevname=0
    - iommu.passthrough=1
    - cgroup_no_v1=net_prio,net_cls
    - hugepagesz=2048kB
    - hugepages=3072
  nvconfig:
  - device: '*'
    parameters:
    - PF_BAR2_ENABLE=0
    - PER_PF_NUM_SF=1
    - PF_TOTAL_SF=20
    - PF_SF_BAR_SIZE=10
    - NUM_PF_MSIX_VALID=0
    - PF_NUM_PF_MSIX_VALID=1
    - PF_NUM_PF_MSIX=228
    - INTERNAL_CPU_MODEL=1
    - INTERNAL_CPU_OFFLOAD_ENGINE=0
    - SRIOV_EN=1
    - NUM_OF_VFS=46
    - LAG_RESOURCE_ALLOCATION=1
    - LINK_TYPE_P1=ETH
    - LINK_TYPE_P2=ETH
  ovs:
    rawConfigScript: |
      _ovs-vsctl() {
        ovs-vsctl --no-wait --timeout 15 "$@"
      }

      _ovs-vsctl set Open_vSwitch . other_config:doca-init=true
      _ovs-vsctl set Open_vSwitch . other_config:dpdk-max-memzones=50000
      _ovs-vsctl set Open_vSwitch . other_config:hw-offload=true
      _ovs-vsctl set Open_vSwitch . other_config:pmd-quiet-idle=true
      _ovs-vsctl set Open_vSwitch . other_config:max-idle=20000
      _ovs-vsctl set Open_vSwitch . other_config:max-revalidator=5000
      _ovs-vsctl set Open_vSwitch . other_config:doca-congestion-threshold=60
      _ovs-vsctl set Open_vSwitch . other_config:hw-offload-ct-unidir-udp-enabled=true
      _ovs-vsctl --if-exists del-br ovsbr1
      _ovs-vsctl --if-exists del-br ovsbr2

      # --- DOCA Weave: service-dependent underlay bridges (match DPUServiceConfiguration underlayInterface) ---
      _ovs-vsctl --may-exist add-br br-sfc
      _ovs-vsctl --may-exist add-br br-underlay-n0
      _ovs-vsctl --may-exist add-br br-underlay-n1
      _ovs-vsctl set bridge br-sfc datapath_type=netdev
      _ovs-vsctl set bridge br-underlay-n0 datapath_type=netdev
      _ovs-vsctl set bridge br-underlay-n1 datapath_type=netdev
      _ovs-vsctl set bridge br-sfc fail_mode=secure
      _ovs-vsctl set bridge br-underlay-n0 fail_mode=standalone
      _ovs-vsctl set bridge br-underlay-n1 fail_mode=standalone
      _ovs-vsctl --may-exist add-port br-sfc ecpf0
      _ovs-vsctl set Interface ecpf0 type=dpdk
      _ovs-vsctl set Interface ecpf0 mtu_request=9216
      _ovs-vsctl set Port ecpf0 external_ids:dpf-type=physical
      _ovs-vsctl --may-exist add-port br-sfc ecpf1
      _ovs-vsctl set Interface ecpf1 type=dpdk
      _ovs-vsctl set Interface ecpf1 mtu_request=9216
      _ovs-vsctl set Port ecpf1 external_ids:dpf-type=physical
```
</details>

## Deploy and validate

After DPF is installed, deploy DOCA Weave by applying the `DPUDeployment`, `DPUServiceTemplate`, and `DPUServiceConfiguration` manifests in this guide (see [Manifest reference](#manifest-reference)). DPF provisions the DPUs and rolls out the `dpf-weave` chart on each DPU through that `DPUDeployment`. Follow [Zero Trust deployment](../../../user-guides/zero-trust/README.md) for the platform workflow. Align registry URL, chart `version`, BFB, and `DPUFlavor` with your DPF release and treat the manifests as version-controlled cluster configuration.

### Validate the deployment

After provisioning, you may need a manual host power cycle when the DPU reports `WaitingForManualPowerCycleOrReboot`. After power cycle, clear the reboot annotation and wait for readiness as your Zero Trust guide describes (for example `kubectl -n dpf-operator-system annotate dpunode --all provisioning.dpu.nvidia.com/dpunode-external-reboot-required-` when that annotation is used).

#### Cluster readiness

The reference `DPUDeployment` example uses `metadata.name`: `doca-weave`. Substitute the name of your deployment in the second command.

```shell
kubectl wait --for=condition=ready --namespace dpf-operator-system dpus --all
kubectl wait --for=condition=ready --namespace dpf-operator-system dpudeployment doca-weave
```

#### Inspect DPUDeployment and related objects

```shell
kubectl -n dpf-operator-system exec deploy/dpf-operator-controller-manager -- /dpfctl describe dpudeployments
```

The output also lists the `DPUServiceChain` objects driven from `spec.serviceChains`, useful for confirming that each NIC is attached to the expected `peerBridge` / `underlayInterface`.

## Manifest reference

Reference YAML for `DPUServiceTemplate` and `DPUServiceConfiguration`. Together they tell DPF which `dpf-weave` chart to run and how to configure `weave-flow-controller` and `weave-dhcp-agent`. Your `DPUDeployment` (above) must reference the same service names.

Work through [Prerequisites and underlay topology](#prerequisites-and-underlay-topology) and [Deploy and validate](#deploy-and-validate) before you apply these manifests. Then adjust the examples for your site: `underlayConfigMapData` (`pciAddress`, bridge names), `repoURL` / chart `version`, and the BFB / `DPUFlavor` names in `DPUDeployment`. Files live under `dpuservices/weave/`.

> [!IMPORTANT]
> IPv4 only for overlay and underlay in this release (IPv6 / mixed families are not supported).

<!-- -->

> [!NOTE]
> Examples assume two NICs (`n0`, `n1`). Scale `interfaces[]`, DHCP `networks[]`, `serviceChains`, and `DPUServiceInterface` to your topology. `repoURL` / `version` are illustrative, match your Helm registry and DPF chart.

### DPUServiceTemplate

Each template points at chart `dpf-weave`, `repoURL` / `version` follow [Helm prerequisites](../../../getting-started/helm-prerequisites.md) and your release.

<details markdown="1"><summary>Reference Example</summary>

[embedmd]:#(../../../../../dpuservices/weave/DPUServiceTemplate.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: weave-flow-controller
  namespace: dpf-operator-system
spec:
  deploymentServiceName: weave-flow-controller
  helmChart:
    source:
      repoURL: https://helm.ngc.nvidia.com/nvidia/doca
      version: v26.5.0
      chart: dpf-weave
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceTemplate
metadata:
  name: weave-dhcp-agent
  namespace: dpf-operator-system
spec:
  deploymentServiceName: weave-dhcp-agent
  helmChart:
    source:
      repoURL: https://helm.ngc.nvidia.com/nvidia/doca
      version: v26.5.0
      chart: dpf-weave
```
</details>

### DPUServiceConfiguration: flow controller

Helm values under `weaveFlowController.underlayConfigMapData` become a ConfigMap that `weave-flow-controller` reads at runtime. Define one entry per east-west NIC in `interfaces[]`:

| Field | Meaning |
|-------|---------|
| `pciAddress` | PCI DBDF of the NIC (for example `0000:01:00.0`, `0000:02:00.0`) |
| `underlayInterface` | Underlay OVS bridge name from `DPUFlavor` (15 characters or fewer) |
| `overlayDHCPInterface` | Logical NIC label paired with the [DHCP agent](#dpuserviceconfiguration-dhcp-agent) (`nic0`, `nic1`, …) |
| `dhcpBridgeName` | DHCP bridge the chart creates (for example `br-dhcp-n0`) |
| `dropBridgeName` | Drop bridge the chart creates; used only by the flow controller |

Notes:

* `dhcpBridgeName` and `overlayDHCPInterface` must match the DHCP agent configuration for the same NIC index.
* Use the same NIC order in `underlayConfigMapData`, `DPUServiceInterface`, and `spec.serviceChains`.
* See [Underlay configuration](README.md#underlay-configuration) for which bridges you create in `DPUFlavor` versus which the chart creates.

Example: `dpuservices/weave/DPUServiceConfiguration-flow-controller.yaml`

<details markdown="1"><summary>Reference Example</summary>

[embedmd]:#(../../../../../dpuservices/weave/DPUServiceConfiguration-flow-controller.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: weave-flow-controller
  namespace: dpf-operator-system
spec:
  deploymentServiceName: weave-flow-controller
  upgradePolicy:
    applyNodeEffect: false
  serviceConfiguration:
    helmChart:
      values:
        weaveFlowController:
          enabled: true
          underlayConfigMapData:
            # underlayInterface must match the underlay OVS bridge names DPUFlavor spec.ovs.rawConfigScript creates per PCI (br-underlay-n* here is illustrative).
            nicIDType: mac
            interfaces:
              - pciAddress: "0000:01:00.0"
                underlayInterface: br-underlay-n0
                overlayDHCPInterface: nic0
                dhcpBridgeName: br-dhcp-n0
                dropBridgeName: br-drop-n0
              - pciAddress: "0000:02:00.0"
                underlayInterface: br-underlay-n1
                overlayDHCPInterface: nic1
                dhcpBridgeName: br-dhcp-n1
                dropBridgeName: br-drop-n1
```
</details>

### DPUServiceConfiguration: DHCP agent

Helm values under `weaveDHCPAgent.dhcpNetworks` configure overlay DHCP on each east-west NIC. Add one `networks[]` entry per NIC:

| Field | Meaning |
|-------|---------|
| `name` | Label for this DHCP network (for example `dhcp-nic-n0`) |
| `bridge` | Must match `dhcpBridgeName` for the same NIC in the [flow controller](#dpuserviceconfiguration-flow-controller) `underlayConfigMapData` |
| `interfaceName` | Must match `overlayDHCPInterface` for that NIC (for example `nic0`) |
| `resourceName` | Node extended resource for the DHCP pod interface (for example `nvidia.com/bf_sf`) |

Notes:

* Keep the same NIC order as in `underlayConfigMapData` and `spec.serviceChains`.
* `dhcpNetworks.createNADs`: when `true`, the chart creates Multus `NetworkAttachmentDefinition` objects and attaches them to `weave-dhcp-agent`; when `false`, skip NAD creation.

Example: `dpuservices/weave/DPUServiceConfiguration-dhcp-agent.yaml`

<details markdown="1"><summary>Reference Example</summary>

[embedmd]:#(../../../../../dpuservices/weave/DPUServiceConfiguration-dhcp-agent.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceConfiguration
metadata:
  name: weave-dhcp-agent
  namespace: dpf-operator-system
spec:
  deploymentServiceName: weave-dhcp-agent
  upgradePolicy:
    applyNodeEffect: false
  serviceConfiguration:
    helmChart:
      values:
        weaveDHCPAgent:
          enabled: true
          dhcpNetworks:
            createNADs: true
            networks:
            - name: dhcp-nic-n0
              bridge: br-dhcp-n0
              resourceName: nvidia.com/bf_sf
              interfaceName: nic0
            - name: dhcp-nic-n1
              bridge: br-dhcp-n1
              resourceName: nvidia.com/bf_sf
              interfaceName: nic1
```
</details>
