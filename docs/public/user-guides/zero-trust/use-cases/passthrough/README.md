---
title: "DPU Passthrough in DPF Zero Trust"
---


> [!NOTE]
> Follow this guide from the source GitHub repo at [github.com/NVIDIA/doca-platform](https://github.com/NVIDIA/doca-platform)
> and moving to the `docs/public/user-guides/zero-trust/use-cases/passthrough/README.md` for better formatting of
> the code.

This configuration provides instructions for deploying the NVIDIA DOCA Platform Framework (DPF) on high-performance,
bare-metal infrastructure in Zero Trust mode, utilizing DPU BMC and Redfish. It focuses on provisioning NVIDIA®
BlueField®-3 DPUs using DPF and enabling them to act as passthrough devices.

[TOC]

## Prerequisites

This guide should be run by cloning the repo from [github.com/NVIDIA/doca-platform](https://github.com/NVIDIA/doca-platform)
and moving to the `docs/public/user-guides/zero-trust/use-cases/passthrough` directory.

The system is set up as described in the [prerequisites](../../prerequisites/README.md).

### Software Prerequisites

The following tools must be installed on the machine where the commands contained in this guide run:

* kubectl
* helm
* envsubst

## Installation Guide

### 0. Required Variables

The following variables are required by this guide. A sensible default is provided where it makes sense, but many will
be specific to the target infrastructure.

Commands in this guide are run in the same directory that contains this readme.

<details markdown="1"><summary><b>Environment variables file</b></summary>

[embedmd]:# (manifests/00-env-vars/envvars.env sh)
```sh
## IP Address for the Kubernetes API server of the target cluster on which DPF is installed.
## This should never include a scheme or a port.
## e.g. 10.10.10.10
export TARGETCLUSTER_API_SERVER_HOST=

## Port for the Kubernetes API server of the target cluster on which DPF is installed.
## e.g. 6443
export TARGETCLUSTER_API_SERVER_PORT=

## Virtual IP used by the load balancer for the DPU Cluster. Must be a reserved IP from the management subnet and not
## allocated by DHCP.
export DPUCLUSTER_VIP=

## Interface on which the DPUCluster load balancer will listen. Should be the management interface of the control plane
## node.
export DPUCLUSTER_INTERFACE=

## The DPF REGISTRY is the Helm repository URL where the DPF Operator Chart resides.
## Usually this is the NVIDIA Helm NGC registry. For development purposes, this can be set to a different repository.
export REGISTRY=https://helm.ngc.nvidia.com/nvidia/doca

## The DPF TAG is the version of the DPF components which will be deployed in this guide.
export TAG=v25.10.1

## URL to the BFB used in the `bfb.yaml` and linked by the DPUSet.
export BFB_URL="https://content.mellanox.com/BlueField/BFBs/Ubuntu24.04/bf-bundle-3.2.1-34_25.11_ubuntu-24.04_64k_prod.bfb"

## IP_RANGE_START and IP_RANGE_END
## These define the IP range for DPU discovery via Redfish/BMC interfaces
## Example: If your DPUs have BMC IPs in range 192.168.1.100-110
## export IP_RANGE_START=192.168.1.100
## export IP_RANGE_END=192.168.1.110
export IP_RANGE_START=

export IP_RANGE_END=

# The password used for DPU BMC root login, must be the same for all DPUs
export BMC_ROOT_PASSWORD=
```
</details>

Modify the variables in `manifests/00-env-vars/envvars.env` to fit your environment, then source the file:

```shell
source manifests/00-env-vars/envvars.env
```

### 1. DPF Operator Installation

#### Create DPU BMC shared password secret

In Zero Trust mode, provisioning DPUs requires authentication with Redfish. In order to do that,
you must set the same root password to access the BMC for all DPUs DPF is going to manage.

For more information on how to set the BMC root password refer to [BlueField DPU Administrator Quick Start Guide](https://docs.nvidia.com/networking/display/bf3dpu/bluefield+dpu+administrator+quick+start+guide#src-2449222348_safe-id-Qmx1ZUZpZWxkRFBVQWRtaW5pc3RyYXRvclF1aWNrU3RhcnRHdWlkZS1TdGVwM-KAk0NoYW5nZURlZmF1bHRQYXNzd29yZA)

The password is provided to DPF by creating the following secret:

```shell
kubectl create secret generic -n dpf-operator-system bmc-shared-password --from-literal=password=$BMC_ROOT_PASSWORD
```

#### Additional Dependencies

Before deploying the DPF Operator, ensure that Helm is properly configured according to the
[Helm prerequisites](../../../../getting-started/helm-prerequisites.md).

> [!WARNING]
> This is a critical prerequisite step that must be completed for the DPF Operator to function properly.

#### Deploy the DPF Operator

A number of [environment variables](#0-required-variables) must be set before running this command.

##### HTTP Registry (default)

If the $REGISTRY is an HTTP Registry (default value) use this command:

```shell
helm repo add --force-update dpf-repository ${REGISTRY}
helm repo update
helm upgrade --install -n dpf-operator-system dpf-operator dpf-repository/dpf-operator --version=$TAG
```

---

##### OCI Registry

For development purposes, if the $REGISTRY is an OCI Registry use this command:

```shell
helm upgrade --install -n dpf-operator-system dpf-operator $REGISTRY/dpf-operator --version=$TAG
```

#### Verification

These verification commands may need to be run multiple times to ensure the condition is met.

Verify the DPF Operator installation with:

```shell
## Ensure the DPF Operator deployment is available.
kubectl rollout status deployment --namespace dpf-operator-system dpf-operator-controller-manager
## Ensure all pods in the DPF Operator system are ready.
kubectl wait --for=condition=ready --namespace dpf-operator-system pods --all
```

### 2. DPF System Installation

This section involves creating the DPF system components and some basic infrastructure required for a functioning
DPF-enabled cluster.

#### Deploy the DPF System components

A number of [environment variables](#0-required-variables) must be set before running this command.

```shell
kubectl create ns dpu-cplane-tenant1
cat manifests/02-dpf-system-installation/*.yaml | envsubst | kubectl apply -f -
```

This will create the following objects:
<details markdown="1"><summary><b>DPFOperatorConfig to install the DPF System components</b></summary>

[embedmd]:#(manifests/02-dpf-system-installation/operatorconfig.yaml)
```yaml
---
apiVersion: operator.dpu.nvidia.com/v1alpha1
kind: DPFOperatorConfig
metadata:
  name: dpfoperatorconfig
  namespace: dpf-operator-system
spec:
  deploymentMode: zero-trust
  overrides:
    kubernetesAPIServerVIP: $TARGETCLUSTER_API_SERVER_HOST
    kubernetesAPIServerPort: $TARGETCLUSTER_API_SERVER_PORT
  dpuDetector:
    disable: true
  provisioningController:
    dmsTimeout: 900
    installInterface:
      installViaRedfish:
        skipDPUNodeDiscovery: false
  kamajiClusterManager:
    disable: false
```
</details>

<details markdown="1"><summary><b>DPUCluster to serve as Kubernetes control plane for DPU nodes</b></summary>

[embedmd]:#(manifests/02-dpf-system-installation/dpucluster.yaml)
```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUCluster
metadata:
  name: dpu-cplane-tenant1
  namespace: dpu-cplane-tenant1
spec:
  type: kamaji
  maxNodes: 1000
  clusterEndpoint:
    # deploy keepalived instances on the nodes that match the given nodeSelector.
    keepalived:
      # interface on which keepalived will listen. Should be the oob interface of the control plane node.
      interface: $DPUCLUSTER_INTERFACE
      # Virtual IP reserved for the DPU Cluster load balancer. Must not be allocatable by DHCP.
      vip: $DPUCLUSTER_VIP
      # virtualRouterID must be in range [1,255], make sure the given virtualRouterID does not duplicate with any existing keepalived process running on the host
      virtualRouterID: 126
      nodeSelector:
        node-role.kubernetes.io/control-plane: ""
```
</details>

<details markdown="1"><summary><b>DPUDiscovery to discover DPUDevices or DPUNodes</b></summary>

[embedmd]:#(manifests/02-dpf-system-installation/dpudiscovery.yaml)
```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUDiscovery
metadata:
  name: dpu-discovery
  namespace: dpf-operator-system
spec:
  ipRangeSpec:
    ipRange:
      startIP: $IP_RANGE_START
      endIP: $IP_RANGE_END
```
</details>


#### Verification

These verification commands may need to be run multiple times to ensure the condition is met.

Verify the DPF System with:
```shell
## Ensure the provisioning and DPUService controller manager deployments are available.
kubectl rollout status deployment --namespace dpf-operator-system dpf-provisioning-controller-manager dpuservice-controller-manager
## Ensure all other deployments in the DPF Operator system are Available.
kubectl rollout status deployment --namespace dpf-operator-system
## Ensure bfb registry daemonset is available
kubectl rollout status daemonset --namespace dpf-operator-system bfb-registry
## Ensure the DPUCluster is ready for nodes to join.
kubectl wait --for=condition=ready --namespace dpu-cplane-tenant1 dpucluster --all
```

### 3. DPU Provisioning and Interface Plumbing

> [!WARNING]
> In case more than 1 DPU exists per node, the relevant selector should be applied in the DPUSet
> to select the appropriate DPU. See [DPUSet - DPU Selection](../../../../developer-guides/api/dpuset.md#dpu-selection)
> to understand more about the selectors.

In this step we provision our DPUs and we do the nessecary interface plumbing to enable the DPU to act as a passthrough
device.

The user is expected to create a DPUSet object to provision the DPUs and a DPUServiceChain to enable the nessecary
connectivity between the host and DPU interfaces.

> Check the [DPUSet documentation](../../../../developer-guides/api/dpuset.md) and [DPUServiceChain documentation](../../../../developer-guides/api/dpuservicechain.md)
> for more information about these objects.

#### Create the BFB, DPUSet and DPUServiceChain

A number of [environment variables](#0-required-variables) must be set before running this command.
```shell
cat manifests/03-dpf-object-installation/*.yaml | envsubst | kubectl apply -f -
```

This will deploy the following objects:
<details markdown="1"><summary><b>BFB to download the BFB to a shared volume</b></summary>

[embedmd]:#(manifests/03-dpf-object-installation/bfb.yaml)
```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: BFB
metadata:
  name: bf-bundle-$TAG
  namespace: dpf-operator-system
spec:
  url: $BFB_URL
```
</details>

<details markdown="1"><summary><b>DPUFlavor used for provisioning the DPUs</b></summary>

[embedmd]:#(manifests/03-dpf-object-installation/dpuflavor.yaml)
```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUFlavor
metadata:
  name: passthrough-$TAG
  namespace: dpf-operator-system
spec:
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
  - device: "*"
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

      _ovs-vsctl --if-exists del-br ovsbr1
      _ovs-vsctl --if-exists del-br ovsbr2
      _ovs-vsctl set Open_vSwitch . other_config:doca-init=true
      _ovs-vsctl set Open_vSwitch . other_config:dpdk-max-memzones=50000
      _ovs-vsctl set Open_vSwitch . other_config:hw-offload=true
      _ovs-vsctl set Open_vSwitch . other_config:pmd-quiet-idle=true
      _ovs-vsctl set Open_vSwitch . other_config:max-idle=20000
      _ovs-vsctl set Open_vSwitch . other_config:max-revalidator=5000
      _ovs-vsctl set Open_vSwitch . other_config:doca-congestion-threshold=60
      _ovs-vsctl set Open_vSwitch . other_config:flow-limit=500000
      _ovs-vsctl set Open_vSwitch . other_config:hw-offload-ct-unidir-udp-enabled=true
      if systemctl list-unit-files openvswitch-switch.service &>/dev/null; then
        systemctl restart openvswitch-switch
      elif systemctl list-unit-files openvswitch.service &>/dev/null; then
        systemctl restart openvswitch
      fi
      _ovs-vsctl --may-exist add-br br-sfc
      _ovs-vsctl set bridge br-sfc datapath_type=netdev
      _ovs-vsctl set bridge br-sfc fail_mode=secure
      _ovs-vsctl --may-exist add-port br-sfc p0
      _ovs-vsctl set Interface p0 type=dpdk
      _ovs-vsctl set Interface p0 mtu_request=9216
      _ovs-vsctl set Port p0 external_ids:dpf-type=physical

  bfcfgParameters:
  - UPDATE_ATF_UEFI=yes
  - UPDATE_DPU_OS=yes
  - WITH_NIC_FW_UPDATE=yes

  configFiles:
  - path: /etc/mellanox/mlnx-bf.conf
    operation: override
    raw: |
        ALLOW_SHARED_RQ="no"
        IPSEC_FULL_OFFLOAD="no"
        ENABLE_ESWITCH_MULTIPORT="yes"
    permissions: "0644"
  - path: /etc/mellanox/mlnx-ovs.conf
    operation: override
    raw: |
        CREATE_OVS_BRIDGES="no"
        OVS_DOCA="yes"
    permissions: "0644"
  - path: /etc/mellanox/mlnx-sf.conf
    operation: override
    raw: ""
    permissions: "0644"

```
</details>

<details markdown="1"><summary><b>DPUSet to provision DPUs on worker nodes</b></summary>

[embedmd]:#(manifests/03-dpf-object-installation/dpuset.yaml)
```yaml
---
apiVersion: provisioning.dpu.nvidia.com/v1alpha1
kind: DPUSet
metadata:
  name: passthrough
  namespace: dpf-operator-system
spec:
  strategy:
    type: OnDelete
  dpuNodeSelector:
    matchLabels:
      feature.node.kubernetes.io/dpu-enabled: "true"
  dpuTemplate:
    spec:
      dpuFlavor: passthrough-$TAG
      bfb:
        name: bf-bundle-$TAG
      nodeEffect:
        hold: true
```
</details>

<details markdown="1"><summary><b>DPUServiceInterfaces used by the DPUServiceChain</b></summary>

[embedmd]:#(manifests/03-dpf-object-installation/ifaces.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: p0
  namespace: dpf-operator-system
spec:
  template:
    spec:
      template:
        metadata:
          labels:
            interface: "p0"
        spec:
          interfaceType: physical
          physical:
            interfaceName: p0
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: p1
  namespace: dpf-operator-system
spec:
  template:
    spec:
      template:
        metadata:
          labels:
            interface: "p1"
        spec:
          interfaceType: physical
          physical:
            interfaceName: p1
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: pf0hpf
  namespace: dpf-operator-system
spec:
  template:
    spec:
      template:
        metadata:
          labels:
            interface: "pf0hpf"
        spec:
          interfaceType: pf
          pf:
            pfID: 0
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceInterface
metadata:
  name: pf1hpf
  namespace: dpf-operator-system
spec:
  template:
    spec:
      template:
        metadata:
          labels:
            interface: "pf1hpf"
        spec:
          interfaceType: pf
          pf:
            pfID: 1
```
</details>

<details markdown="1"><summary><b>DPUServiceChain to make the device act as passthrough device</b></summary>

[embedmd]:#(manifests/03-dpf-object-installation/dpuservicechain.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUServiceChain
metadata:
  name: passthrough
  namespace: dpf-operator-system
spec:
  template:
    spec:
      template:
        spec:
          switches:
            - ports:
              - serviceInterface:
                  matchLabels:
                    interface: p0
              - serviceInterface:
                  matchLabels:
                    interface: pf0hpf
            - ports:
              - serviceInterface:
                  matchLabels:
                    interface: p1
              - serviceInterface:
                  matchLabels:
                    interface: pf1hpf
```
</details>

#### Verification

The following verification commands may need to be run multiple times to ensure the condition is met.

```shell
## Ensure the DPUServiceChain is ready 
kubectl wait --for=condition=ready --namespace dpf-operator-system dpuservicechain passthrough
## Ensure the DPUServiceInterfaces are ready
kubectl wait --for=condition=ready --namespace dpf-operator-system dpuserviceinterface p0 p1 pf0hpf pf1hpf
## Ensure the BFB is ready
kubectl wait --for="jsonpath={.status.phase}=Ready" --namespace dpf-operator-system bfb bf-bundle
## Ensure the DPUs have the condition Initialized (this may take time)
kubectl wait --for=condition=Initialized --namespace dpf-operator-system dpu --all
```

or with `dpfctl`:

```shell
$ kubectl -n dpf-operator-system exec deploy/dpf-operator-controller-manager -- /dpfctl describe dpusets
NAME                                            NAMESPACE            STATUS       REASON         SINCE  MESSAGE
DPFOperatorConfig/dpfoperatorconfig             dpf-operator-system  Ready: True  Success        24m
├─DPUServiceChains
│ └─DPUServiceChain/passthrough                 dpf-operator-system  Ready: True  Success        7s
├─DPUServiceInterfaces
│ └─4 DPUServiceInterfaces...                   dpf-operator-system  Ready: True  Success        78m    See p0, p1, pf0hpf, pf1hpf
└─DPUSets
  └─DPUSet/passthrough                          dpf-operator-system
    ├─BFB/bf-bundle                             dpf-operator-system  Ready: True  Ready          78m    File: bf-bundle-3.2.1-34_25.11_ubuntu-24.04_64k_prod.bfb, DOCA: 3.2.1
    └─DPUs
      ├─DPU/dpu-node-mt2402xz0f6v-mt2402xz0f6v  dpf-operator-system
      │             └─Ready                                          False        OS Installing  1s
      └─DPU/dpu-node-mt2404xz0c98-mt2404xz0c98  dpf-operator-system
                    └─Ready                                          False        OS Installing  1s
```

#### Releasing the Node Effect Hold

Since the DPUSet is configured with `nodeEffect.hold: true`, the DPUs will pause at the "Node Effect"
phase and wait for external action before proceeding with provisioning. This gives the administrator control
over when the node effect is applied.

To check that DPUNodeMaintenance objects have been created and are in the hold state:

```shell
kubectl get dpunodemaintenances -n dpf-operator-system
```

Once you are ready for provisioning to proceed, release the hold by setting the annotation on the
DPUNodeMaintenance objects to `"false"`. You can do this per-node or all at once:

```shell
kubectl annotate --overwrite dpunodemaintenances -n dpf-operator-system --all provisioning.dpu.nvidia.com/wait-for-external-nodeeffect=false
```

After releasing the hold, the DPUs will proceed through the remaining provisioning phases (BFB installation,
OS installation, etc.).

#### Making the DPUs Ready

In order to make the DPUs ready, we will need to manually power cycle the host. This operation should be done in the
most graceful manner by gracefully shutting down the Host and DPU, powering off the server and then powering it on to
avoid corruption. This should happen when the object gives us the signal. The described flow can be automated by the
admin depending on the infrastructure.

The following verification command may need to be run multiple times to ensure the condition is met.

```shell
## Ensure the DPUs have the condition WaitingForManualPowerCycleOrReboot (this may take time)
kubectl wait --for=condition=WaitingForManualPowerCycleOrReboot --namespace dpf-operator-system dpu --all
```

or with `dpfctl`:

```shell
$ kubectl -n dpf-operator-system exec deploy/dpf-operator-controller-manager -- /dpfctl describe dpusets
NAME                                            NAMESPACE            STATUS       REASON                              SINCE  MESSAGE
DPFOperatorConfig/dpfoperatorconfig             dpf-operator-system
│           ├─Ready                                                  False        Pending                             36m    The following conditions are not ready:
│           │                                                                                                                * SystemComponentsReady
│           └─SystemComponentsReady                                  False        Error                               35m    System components must be ready for DPF Operator to continue:
│                                                                                                                              * nvidia-k8s-ipam: DPUService dpf-operator-system/nvidia-k8s-ipam is not ready
├─DPUServiceChains
│ └─DPUServiceChain/passthrough                 dpf-operator-system  Ready: True  Success                             34m
├─DPUServiceInterfaces
│ └─4 DPUServiceInterfaces...                   dpf-operator-system  Ready: True  Success                             34m    See p0, p1, pf0hpf, pf1hpf
└─DPUSets
  └─DPUSet/passthrough                          dpf-operator-system
    ├─BFB/bf-bundle                             dpf-operator-system  Ready: True  Ready                               34m    File: bf-bundle-3.2.1-34_25.11_ubuntu-24.04_64k_prod.bfb, DOCA: 3.2.1
    └─DPUs
      ├─DPU/dpu-node-mt2402xz0f6v-mt2402xz0f6v  dpf-operator-system
      │             ├─Rebooted                                       False        WaitingForManualPowerCycleOrReboot  12m
      │             └─Ready                                          False        Rebooting                           12m
      └─DPU/dpu-node-mt2404xz0c98-mt2404xz0c98  dpf-operator-system
                    ├─Rebooted                                       False        WaitingForManualPowerCycleOrReboot  10m
                    └─Ready                                          False        Rebooting                           10m
```

At this point, we have to power cycle the hosts. Once **all the hosts are back online**, we have to remove an annotation
from the DPUNodes. The user can choose to remove this annotation node by node but to make it simpler in this guide, we do
that all at once.

```shell
kubectl annotate dpunodes -n dpf-operator-system --all provisioning.dpu.nvidia.com/dpunode-external-reboot-required-
```

After this is done, we should expect that all DPUs become Ready:

```shell
kubectl wait --for="jsonpath={.status.phase}=Ready" --namespace dpf-operator-system dpu --all
```

or with dpfctl:

```shell
$ kubectl -n dpf-operator-system exec deploy/dpf-operator-controller-manager -- /dpfctl describe dpusets
NAME                                 NAMESPACE            STATUS       REASON    SINCE  MESSAGE
DPFOperatorConfig/dpfoperatorconfig  dpf-operator-system  Ready: True  Success   42s
├─DPUServiceChains
│ └─DPUServiceChain/passthrough      dpf-operator-system  Ready: True  Success   54m
├─DPUServiceInterfaces
│ └─4 DPUServiceInterfaces...        dpf-operator-system  Ready: True  Success   54m    See p0, p1, pf0hpf, pf1hpf
└─DPUSets
  └─DPUSet/passthrough               dpf-operator-system
    ├─BFB/bf-bundle                  dpf-operator-system  Ready: True  Ready     54m    File: bf-bundle-3.2.1-34_25.11_ubuntu-24.04_64k_prod.bfb, DOCA: 3.2.1
    └─DPUs
      └─2 DPUs...                    dpf-operator-system  Ready: True  DPUReady  2m33s  See dpu-node-mt2402xz0f6v-mt2402xz0f6v, dpu-node-mt2404xz0c98-mt2404xz0c98
```

#### Test Traffic

After the DPUs are provisioned and the rest of the objects are Ready, we can test traffic by assigning an IP on one of
the PFs on the host for each DPU, and run a simple ping. This assumes that the high speed ports of the DPUs are connected
and the DPUs can reach each other. Assuming the pf is named `ens5f0np0` then:

Host 1:

```shell
ip link set dev ens5f0np0 up
ip addr add 192.168.1.1/24 dev ens5f0np0
```

Host 2:

```shell
ip link set dev ens5f0np0 up
ip addr add 192.168.1.2/24 dev ens5f0np0
```

From Host 1:

```shell
$ ping 192.168.1.2 -c3
PING 192.168.1.2 (192.168.1.2) 56(84) bytes of data.
64 bytes from 192.168.1.2: icmp_seq=1 ttl=64 time=0.387 ms
64 bytes from 192.168.1.2: icmp_seq=2 ttl=64 time=0.344 ms
64 bytes from 192.168.1.2: icmp_seq=3 ttl=64 time=0.396 ms

--- 192.168.1.2 ping statistics ---
3 packets transmitted, 3 received, 0% packet loss, time 2053ms
rtt min/avg/max/mdev = 0.344/0.375/0.396/0.022 ms
```

## Uninstall

This section covers only the DPF related components and not the prerequisites as these must be managed by the admin.

### Delete the DPF Operator system and DPF Operator

```shell
kubectl delete -n dpf-operator-system dpfoperatorconfig dpfoperatorconfig --wait
helm uninstall -n dpf-operator-system dpf-operator --wait
```

Note: there can be a race condition with deleting the underlying Kamaji cluster which runs the DPU cluster control
plane in this guide. If that happens it may be necessary to remove finalizers manually from `DPUCluster` and `Datastore`
objects.
