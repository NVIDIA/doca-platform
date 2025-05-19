---
title: "DPF CLI - dpfctl"
---

[TOC]

**`dpfctl`** is a command-line tool designed to **visualize, debug, and troubleshoot DPU resources** in Kubernetes.  
It simplifies debugging by extracting and presenting resource conditions in a structured, human-readable format.

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

You can also download it directly from the command line (in this example we are using `dpfctl-linux-amd64` with version `v25.4.0`):

```shell
curl -L -o /usr/local/bin/dpfctl https://api.ngc.nvidia.com/v2/resources/nvidia/doca/dpfctl/versions/v25.4.0/files/dpfctl-linux-amd64
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

> [!NOTE]  
> Available flags can be found with `dpfctl describe --help`.

By default, `dpfctl describe` provides an overview of key **DPU-related resources and their conditions**.

Example output of `dpfctl describe all`:

```shell
> dpfctl describe all
NAME                                     NAMESPACE            READY  REASON             SINCE  MESSAGE
DPFOperatorConfig/dpfoperatorconfig      dpf-operator-system  True   Success            27h
├─DPUClusters
│ └─DPUCluster/dpu-cplane-tenant1        dpu-cplane-tenant1   True   HealthCheckPassed  29h
├─DPUServiceChains
│ └─2 DPUServiceChains...                dpf-operator-system  True   Success            27h    See hbn-to-fabric, ovn-to-hbn
├─DPUServiceCredentialRequests
│ └─DPUServiceCredentialRequest/ovn-dpu  dpf-operator-system  True   Success            27h
├─DPUServiceIPAMs
│ └─2 DPUServiceIPAMs...                 dpf-operator-system  True   Success            27h    See loopback, pool1
├─DPUServiceInterfaces
│ └─6 DPUServiceInterfaces...            dpf-operator-system  True   Success            27h    See app-sf, ovn, p0, p0-sf, p1, p1-sf
├─DPUServices
│ └─12 DPUServices...                    dpf-operator-system  True   Success            27h    See doca-blueman-service, doca-hbn, doca-telemetry-service, flannel, multus, nvidia-k8s-ipam,
│                                                                                              ovn-dpu, ovs-cni, ovs-helper, servicechainset-controller, sfc-controller, sriov-device-plugin
└─DPUSets
  └─DPUSet/dpuset                        dpf-operator-system
    ├─DPU/worker1-0000-08-00             dpf-operator-system  True   DPUNodeReady       27h
    └─DPU/worker2-0000-08-00             dpf-operator-system  True   DPUNodeReady       27h
```

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
> dpfctl describe all --show-resources dpuservice
NAME                                 NAMESPACE            READY  REASON   SINCE  MESSAGE
DPFOperatorConfig/dpfoperatorconfig  dpf-operator-system  True   Success  27h
└─DPUServices
  └─12 DPUServices...                dpf-operator-system  True   Success  27h    See doca-blueman-service, doca-hbn, doca-telemetry-service, flannel, multus, nvidia-k8s-ipam,
                                                                                 ovn-dpu, ovs-cni, ovs-helper, servicechainset-controller, sfc-controller, sriov-device-plugin
```

If you want to show multiple different resources you can add a comma-separated list of resources to the
`--show-resources` flag.

```shell
> dpfctl describe all --show-resources dpuservice,dpuset
NAME                                 NAMESPACE            READY  REASON        SINCE  MESSAGE
DPFOperatorConfig/dpfoperatorconfig  dpf-operator-system  True   Success       27h
├─DPUServices
│ └─12 DPUServices...                dpf-operator-system  True   Success       27h    See doca-blueman-service, doca-hbn, doca-telemetry-service, flannel, multus, nvidia-k8s-ipam,
│                                                                                     ovn-dpu, ovs-cni, ovs-helper, servicechainset-controller, sfc-controller, sriov-device-plugin
└─DPUSets
  └─DPUSet/dpuset                    dpf-operator-system
    ├─DPU/worker1-0000-08-00         dpf-operator-system  True   DPUNodeReady  27h
    └─DPU/worker2-0000-08-00         dpf-operator-system  True   DPUNodeReady  27h
```

To expand child objects:

```shell
> dpfctl describe all --show-resources dpuservice --expand-resources dpuservice
NAME                                                             NAMESPACE            READY  REASON             SINCE  MESSAGE
[...]
├─DPUServices
│ ├─DPUService/doca-blueman-service                              dpf-operator-system  True   Success            27h
│ │ └─Application/dpu-cplane-tenant1-doca-blueman-service        dpf-operator-system  True   Success            108s
│ ├─DPUService/doca-hbn                                          dpf-operator-system  True   Success            27h
│ │ └─Application/dpu-cplane-tenant1-doca-hbn                    dpf-operator-system  True   Success            108s
```

> [!NOTE]  
> The flag `--expand-resources` is currently supported only for `DPUServices`. Further support for other resources will
> be added in future releases.

#### Grouping

Grouping combines resources of the same kind. To disable grouping:

```shell
> dpfctl describe all --show-resources dpuservice --grouping=false
NAME                                       NAMESPACE            READY  REASON   SINCE  MESSAGE
DPFOperatorConfig/dpfoperatorconfig        dpf-operator-system  True   Success  27h
└─DPUServices
  ├─DPUService/doca-blueman-service        dpf-operator-system  True   Success  27h
  ├─DPUService/doca-hbn                    dpf-operator-system  True   Success  27h
  ├─DPUService/doca-telemetry-service      dpf-operator-system  True   Success  27h
  ├─DPUService/flannel                     dpf-operator-system  True   Success  27h
  ├─DPUService/multus                      dpf-operator-system  True   Success  27h
  ├─DPUService/nvidia-k8s-ipam             dpf-operator-system  True   Success  27h
  ├─DPUService/ovn-dpu                     dpf-operator-system  True   Success  27h
  ├─DPUService/ovs-cni                     dpf-operator-system  True   Success  27h
  ├─DPUService/ovs-helper                  dpf-operator-system  True   Success  27h
  ├─DPUService/servicechainset-controller  dpf-operator-system  True   Success  27h
  ├─DPUService/sfc-controller              dpf-operator-system  True   Success  27h
  └─DPUService/sriov-device-plugin         dpf-operator-system  True   Success  27h
```

#### Conditions

To show resource conditions:

```shell
dpfctl describe all --show-conditions dpuservice
```

Example output:

```shell
NAME                                             NAMESPACE            READY  REASON             SINCE  MESSAGE
├─DPUServices
│ ├─DPUService/doca-blueman-service              dpf-operator-system  True   Success            27h
│ │             ├─ApplicationPrereqsReconciled                        True   Success            28h
│ │             ├─ApplicationsReady                                   True   Success            27h
│ │             ├─ApplicationsReconciled                              True   Success            28h
│ │             └─DPUServiceInterfaceReconciled                       True   Success            28h
```

Use `all` or `failed` to show all conditions or only failed conditions, respectively.
