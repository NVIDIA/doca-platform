---
title: "Accessing the Kamaji DPU Cluster"
---

[[_TOC_]]

> **Warning:** Direct access to the DPU cluster is intended for advanced troubleshooting only.
> Under normal circumstances you should **not** need to interact with the DPU cluster directly.
> The primary interface for monitoring and managing DPU workloads is through the DPF custom resources
> (e.g. `DPUService`, `DPUSet`, `ServiceChain`) and their `.status` fields. Always consult those first
> before proceeding with the steps below.

## Overview

When a `DPUCluster` of type `kamaji` is created, DPF provisions a full Kubernetes control plane using
[Kamaji](https://kamaji.clastix.io/). The kubeconfig for that cluster is stored as a Kubernetes `Secret`
in the same namespace as the `DPUCluster` object. The secret name is recorded in the `DPUCluster`'s
`.spec.kubeconfig` field once the cluster becomes ready.

## Prerequisites

* `kubectl` configured to talk to the **host cluster** (the cluster where DPF is installed).
* Sufficient RBAC permissions to read `DPUCluster` and `Secret` objects in the DPF namespace
  (typically `dpf-operator-system`).

## Steps

### 1. Verify the DPUCluster is Ready

```shell
kubectl get dpucluster -n dpf-operator-system
```

The `Phase` column should show `Ready` before you proceed. If the cluster is still `Creating` or
`Pending`, wait until it transitions to `Ready`.

### 2. Retrieve the Kubeconfig Secret Name

The secret name is stored in `.spec.kubeconfig`:

```shell
KUBECONFIG_SECRET=$(kubectl get dpucluster <dpucluster-name> \
  -n dpf-operator-system \
  -o jsonpath='{.spec.kubeconfig}')

echo "Kubeconfig secret: ${KUBECONFIG_SECRET}"
```

For a Kamaji-backed cluster the secret name follows the pattern `<dpucluster-name>-admin-kubeconfig`.

### 3. Extract the Kubeconfig

The secret stores the kubeconfig under the key `admin.conf`:

```shell
kubectl get secret "${KUBECONFIG_SECRET}" \
  -n dpf-operator-system \
  -o go-template='{{index .data "admin.conf" | base64decode}}' \
  > /tmp/dpu-cluster-kubeconfig.yaml
```

### 4. Use the Kubeconfig

Pass the extracted kubeconfig to any `kubectl` command targeting the DPU cluster:

```shell
kubectl --kubeconfig /tmp/dpu-cluster-kubeconfig.yaml get nodes
```

Or export it as an environment variable for the current shell session:

```shell
export KUBECONFIG=/tmp/dpu-cluster-kubeconfig.yaml
kubectl get nodes
```

> **Security note:** The extracted file contains cluster-admin credentials. Delete it when you are
> done (`rm /tmp/dpu-cluster-kubeconfig.yaml`) and do not store it in version control.

## Related Documentation

* [DPUCluster API Reference](../../developer-guides/api/dpucluster.md) - Status fields, conditions, and Kamaji-specific troubleshooting
