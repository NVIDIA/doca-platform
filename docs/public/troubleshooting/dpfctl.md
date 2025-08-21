---
title: "DPF CLI - dpfctl"
---

[TOC]

**`dpfctl`** is a command-line tool designed to **visualize, debug, and troubleshoot DPU resources** in Kubernetes.  
It simplifies debugging by extracting and presenting resource conditions in a structured, human-readable format.

It helps during provisioning and debugging of DPF by providing real-time visibility into resource states, conditions, and relationships.  
For example, during DPU provisioning you can quickly identify which resources are ready, which are failing, and understand the dependency chain between components.

![Example](dpfctl-example.png)

## How to Run

There are 2 ways to run `dpfctl`:

* **Install**: You can download & run `dpfctl` locally on your machine.
* **Execute Inside Kubernetes Pod**: With this way you can ensure that you are using the correct version of `dpfctl` that is
  compatible with the DPF Operator.

### Install

To download the latest version of `dpfctl`, you can find the latest version [on NGC doca/dpfctl](https://registry.ngc.nvidia.com/orgs/nvidia/teams/doca/resources/dpfctl).

There are 3 different versions available for different architectures:

* `dpfctl-linux-amd64`
* `dpfctl-linux-arm64`
* `dpfctl-darwin-arm64`

You can also download it directly from the command line (in this example we are using `dpfctl-linux-amd64` with version `v25.7.0`):

```shell
curl -L -o /usr/local/bin/dpfctl https://api.ngc.nvidia.com/v2/resources/nvidia/doca/dpfctl/versions/v25.7.0/files/dpfctl-linux-amd64
chmod +x /usr/local/bin/dpfctl
```

### Execute Inside Kubernetes Pod

To execute `dpfctl` from the running container:

```shell
kubectl -n dpf-operator-system exec deploy/dpf-operator-controller-manager -- /dpfctl describe all
```

#### Creating a Shell Alias

For convenience, you can create a shell alias to simplify your commands:

```shell
alias dpfctl="kubectl -n dpf-operator-system exec deploy/dpf-operator-controller-manager -- /dpfctl "
```

If you want to use `watch` according to `man watch`:

> If the last character of the alias value is a blank, then the next command word following the alias is also checked for alias expansion.

To use `watch` with custom arguments (like `-n1` for interval), add both aliases to your shell's configuration file (e.g., `.bashrc`, `.zshrc`, or `.profile`):

```shell
# Add to your shell's configuration file
alias dpfctl="kubectl -n dpf-operator-system exec deploy/dpf-operator-controller-manager -- /dpfctl "
alias watch="watch -c "
```

## Usage

```sh
dpfctl describe [command] [flags]
```

Available Commands:

| Command        | Description                                    |
|----------------|------------------------------------------------|
| all            | Describe all DPF resources                     |
| dpuclusters    | Describe DPF DPUClusters                       |
| dpudeployments | Describe DPF DPUDeployments                    |
| dpuservices    | Describe DPF DPUServices                       |
| dpusets        | Describe DPF DPUSets and DPU related resources |
| dpuvpcs        | Describe DPF DPUVPCs and VPC related resources |
| storage        | Describe DPF Storage resources                 |

> [!NOTE]  
> Available flags can be found with `dpfctl describe --help`.

By default, `dpfctl describe` provides an overview of key **DPU-related resources and their conditions**.

Example output of `dpfctl describe all`:

```shell
NAME                                                  NAMESPACE            STATUS       REASON             SINCE  MESSAGE
DPFOperatorConfig/dpfoperatorconfig                   dpf-operator-system  Ready: True  Success            23h
├─DPUClusters
│ └─DPUCluster/dpu-cplane-tenant1                     dpu-cplane-tenant1   Ready: True  HealthCheckPassed  4d20h
├─DPUDeployments
│ └─DPUDeployment/vpc-ovn                             dpf-operator-system  Ready: True  Success            23h
│   ├─DPUServiceChains
│   │ └─DPUServiceChain/vpc-ovn-trsq6                 dpf-operator-system  Ready: True  Success            23h
│   ├─DPUSets
│   │ └─DPUSet/vpc-ovn-dpuset1                        dpf-operator-system
│   │   ├─BFB/bf-bundle                               dpf-operator-system  Ready: True  Ready              3d17h  File: bf-bundle-3.1.0-53_25.07_ubuntu-22.04_prod.bfb, DOCA: 3.1.0
│   │   └─DPUs
│   │     └─4 DPUs...                                 dpf-operator-system  Ready: True  DPUReady           3d17h  See dpu-node-mt2310xz03lr-mt2310xz03lr, dpu-node-mt2310xz03m2-mt2310xz03m2,
│   │                                                                                                             dpu-node-mt2425601x13-mt2425601x13, dpu-node-mt2425601xqy-mt2425601xqy
│   └─Services
│     ├─DPUServiceTemplates
│     │ └─4 DPUServiceTemplates...                    dpf-operator-system  Ready: True  Success            3d17h  See ovn-central, ovn-controller, vpc-ovn-controller, vpc-ovn-node
│     └─DPUServices
│       └─4 DPUServices...                            dpf-operator-system  Ready: True  Success            3d1h   See ovn-central-9558p, ovn-controller-v5bkr, vpc-ovn-controller-7sbp6, vpc-ovn-node-r84zn
├─DPUServiceCredentialRequests
│ └─DPUServiceCredentialRequest/servicesetcontroller  dpf-operator-system  Ready: True  Success            4d20h
├─DPUServiceIPAMs
│ └─2 DPUServiceIPAMs...                              dpf-operator-system  Ready: True  Success            3d17h  See vpc-ippool-gateway, vpc-ippool-vtep
├─DPUServiceInterfaces
│ └─4 DPUServiceInterfaces...                         dpf-operator-system  Ready: True  Success            3d17h  See ovn-ext-patch, ovn-vtep-patch, p0, vf2-test
├─DPUVPCs
│ └─DPUVPC/myvpc                                      dpf-operator-system  Ready: True  Success            2d
│   ├─IsolationClass/ovn.vpc.dpu.nvidia.com                                Ready: True  Available          3d     Provisioner: ovn.vpc.dpu.nvidia.com
│   └─DPUVirtualNetwork/testnet1                      dpf-operator-system  Ready: True  Success            2d
│     └─DPUServiceInterface/vf2-test                  dpf-operator-system  Ready: True  Success            2d
└─System Components                                   dpf-operator-system
  └─DPUServices
    └─8 DPUServices...                                dpf-operator-system  Ready: True  Success            4d20h  See flannel, multus, nvidia-k8s-ipam, ovs-cni, servicechainset-controller,
                                                                                                                  servicechainset-rbac-and-crds, sfc-controller, sriov-device-plugin
```

Storage resources are not included in the output of `dpfctl describe all` command by default. To display storage resources, use the `--show-storage` flag.

The `dpfctl describe storage` command can be used to display only storage resources. It acts as a shortcut for
`dpfctl describe all --show-storage --show-resources dpuvolumes,dpuvolumeattachments,dpustoragevendors,dpustoragepolicies`.

### Kubeconfig

By default, `dpfctl` uses the kubeconfig file at `~/.kube/config`. To use a different kubeconfig file, specify it with
`--kubeconfig`, which is part of the global flags:

```shell
dpfctl describe all --kubeconfig /path/to/kubeconfig
```

Alternatively, set the `KUBECONFIG` environment variable:

```shell
export KUBECONFIG=/path/to/kubeconfig
dpfctl describe all
```

or

```shell
KUBECONFIG=/path/to/kubeconfig dpfctl describe all
```

### Visualizing the output

We can customize the output by using flags.

#### Show & Expand Resources

For example `dpfctl describe all --show-resources=dpuservice` will show only DPUService resources.

```shell
NAME                                 NAMESPACE            STATUS       REASON   SINCE  MESSAGE
DPFOperatorConfig/dpfoperatorconfig  dpf-operator-system  Ready: True  Success  23h
├─DPUServices
│ └─4 DPUServices...                 dpf-operator-system  Ready: True  Success  3d1h   See ovn-central-9558p, ovn-controller-v5bkr, vpc-ovn-controller-7sbp6, vpc-ovn-node-r84zn
└─System Components                  dpf-operator-system
  └─DPUServices
    └─8 DPUServices...               dpf-operator-system  Ready: True  Success  4d20h  See flannel, multus, nvidia-k8s-ipam, ovs-cni, servicechainset-controller,
                                                                                       servicechainset-rbac-and-crds, sfc-controller, sriov-device-plugin
```

If you want to show multiple different resources you can add a comma-separated list of resources to the
`--show-resources` flag.

```shell
> dpfctl describe all --show-resources dpuservice,dpuset
NAME                                 NAMESPACE            STATUS       REASON    SINCE  MESSAGE
DPFOperatorConfig/dpfoperatorconfig  dpf-operator-system  Ready: True  Success   23h
├─DPUServices
│ └─4 DPUServices...                 dpf-operator-system  Ready: True  Success   3d1h   See ovn-central-9558p, ovn-controller-v5bkr, vpc-ovn-controller-7sbp6, vpc-ovn-node-r84zn
├─DPUSets
│ └─DPUSet/vpc-ovn-dpuset1           dpf-operator-system
│   ├─BFB/bf-bundle                  dpf-operator-system  Ready: True  Ready     3d17h  File: bf-bundle-3.1.0-53_25.07_ubuntu-22.04_prod.bfb, DOCA: 3.1.0
│   └─DPUs
│     └─4 DPUs...                    dpf-operator-system  Ready: True  DPUReady  3d17h  See dpu-node-mt2310xz03lr-mt2310xz03lr, dpu-node-mt2310xz03m2-mt2310xz03m2,
│                                                                                       dpu-node-mt2425601x13-mt2425601x13, dpu-node-mt2425601xqy-mt2425601xqy
└─System Components                  dpf-operator-system
  └─DPUServices
    └─8 DPUServices...               dpf-operator-system  Ready: True  Success   4d20h  See flannel, multus, nvidia-k8s-ipam, ovs-cni, servicechainset-controller,
                                                                                        servicechainset-rbac-and-crds, sfc-controller, sriov-device-plugin
```

To expand child objects:

```shell
> dpfctl describe all --show-resources dpuservice --expand-resources dpuservice --grouping=false
NAME                                                                   NAMESPACE            READY        REASON   SINCE  MESSAGE
[...]
│ ├─DPUService/ovn-central-9558p                                       dpf-operator-system  Ready: True  Success  26h
│ │ └─Application/in-cluster-ovn-central-9558p                         dpf-operator-system
│ │               ├─Deployment/in-cluster-ovn-central-9558p                                 Synced       Healthy  31s
│ │               └─Ready                                                                   True         Success  31s
│ ├─DPUService/ovn-controller-v5bkr                                    dpf-operator-system  Ready: True  Success  47h
│ │ └─Application/dpu-cplane-tenant1-ovn-controller-v5bkr              dpf-operator-system
│ │               ├─DaemonSet/dpu-cplane-tenant1-ovn-controller-v5bkr                       Synced       Healthy  31s
│ │               └─Ready                                                                   True         Success  31s
```

> [!NOTE]  
> The flag `--expand-resources` is currently supported only for `DPUServices`. Further support for other resources will
> be added in future releases.

#### Grouping

Grouping combines resources of the same kind. To disable grouping:

```shell
> dpfctl describe all --show-resources dpuservice --grouping=false
NAME                                            NAMESPACE            STATUS       REASON   SINCE  MESSAGE
DPFOperatorConfig/dpfoperatorconfig             dpf-operator-system  Ready: True  Success  7m14s
├─DPUServices
│ ├─DPUService/ovn-central-9558p                dpf-operator-system  Ready: True  Success  26h
│ ├─DPUService/ovn-controller-v5bkr             dpf-operator-system  Ready: True  Success  47h
│ ├─DPUService/vpc-ovn-controller-7sbp6         dpf-operator-system  Ready: True  Success  2d2h
│ └─DPUService/vpc-ovn-node-r84zn               dpf-operator-system  Ready: True  Success  34m
└─System Components                             dpf-operator-system
  └─DPUServices
    ├─DPUService/flannel                        dpf-operator-system  Ready: True  Success  47h
    ├─DPUService/multus                         dpf-operator-system  Ready: True  Success  47h
    ├─DPUService/nvidia-k8s-ipam                dpf-operator-system  Ready: True  Success  47h
    ├─DPUService/ovs-cni                        dpf-operator-system  Ready: True  Success  25h
    ├─DPUService/servicechainset-controller     dpf-operator-system  Ready: True  Success  3d21h
    ├─DPUService/servicechainset-rbac-and-crds  dpf-operator-system  Ready: True  Success  3d21h
    ├─DPUService/sfc-controller                 dpf-operator-system  Ready: True  Success  26h
    └─DPUService/sriov-device-plugin            dpf-operator-system  Ready: True  Success  47h
```

#### Conditions

To show resource conditions:

```shell
dpfctl describe all --show-conditions=all
```

Example output:

```shell
NAME                                                   NAMESPACE            STATUS  REASON                SINCE  MESSAGE
[...]
└─System Components                                    dpf-operator-system
  └─DPUServices
    ├─DPUService/flannel                               dpf-operator-system
    │             ├─Ready                                                   True    Success               47h
    │             ├─ApplicationPrereqsReconciled                            True    Success               3d21h
    │             ├─ApplicationsReady                                       True    Success               47h
    │             ├─ApplicationsReconciled                                  True    Success               3d21h
    │             ├─ConfigPortsReconciled                                   True    Success               3d21h
    │             └─DPUServiceInterfaceReconciled                           True    Success               3d21h
```
