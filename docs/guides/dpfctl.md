# DPF CLI - `dpfctl`

**`dpfctl`** is a command-line tool designed to **visualize, debug, and troubleshoot DPU resources** in Kubernetes.  
It simplifies debugging by extracting and presenting resource conditions in a structured, human-readable format.

<!-- toc -->
- [Install](#install)
- [Usage](#usage)
  - [Kubeconfig](#kubeconfig)
  - [Visualizing the output](#visualizing-the-output)
    - [Show &amp; Expand Resources](#show--expand-resources)
    - [Grouping](#grouping)
    - [Conditions](#conditions)
<!-- /toc -->

## Install

Currently, the `dpfctl` binary is only available inside the `dpf-system` container.  
To use the CLI, you can either run it directly from the container or extract the binary for easier access.

To execute `dpfctl` from the running container:

```shell
kubectl -n dpf-operator-system exec deploy/dpf-operator-controller-manager -- /dpfctl describe all
```

For convenience, you can create a shell alias to simplify commands:

```shell
alias dpfctl='kubectl -n dpf-operator-system exec deploy/dpf-operator-controller-manager -- /dpfctl'
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
  └─DPUService/doca-telemetry-service      dpf-operator-system  True   Success  27h
  └─DPUService/flannel                     dpf-operator-system  True   Success  27h
  └─DPUService/multus                      dpf-operator-system  True   Success  27h
  └─DPUService/nvidia-k8s-ipam             dpf-operator-system  True   Success  27h
  └─DPUService/ovn-dpu                     dpf-operator-system  True   Success  27h
  └─DPUService/ovs-cni                     dpf-operator-system  True   Success  27h
  └─DPUService/ovs-helper                  dpf-operator-system  True   Success  27h
  └─DPUService/servicechainset-controller  dpf-operator-system  True   Success  27h
  └─DPUService/sfc-controller              dpf-operator-system  True   Success  27h
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
