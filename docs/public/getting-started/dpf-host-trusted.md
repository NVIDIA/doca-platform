---
title: "DPF Host Trusted"
---

[[_TOC_]]

# Get Started with DPF Host Trusted

This guide shows you how to deploy the NVIDIA DPF Operator prepared for the Host Trusted mode, which is designed for
bare-metal infrastructure with NVIDIA BlueField-3 DPUs.

## Before you Begin

To follow this guide, you need the following:

* **A Kubernetes cluster** with administrative access for DPF Operator deployment
* **Bare-metal infrastructure** with NVIDIA DPUs
* **Network access** to the NVIDIA NGC Catalog for downloading DPF Operator charts and container images

For detailed requirements, ensure your system meets these prerequisites:

* **System Prerequisites**: See
  the [DPF System Prerequisites](../user-guides/host-trusted/prerequisites/system.md) for complete hardware and
  system requirements
* **Helm Dependencies**: See the [Helm Prerequisites Guide](./helm-prerequisites.md) for required Helm charts that must
  be installed before the DPF Operator
* **Network Configuration**: See
  the [Host Network Configuration Prerequisites](../user-guides/host-trusted/prerequisites/host-network-configuration-prerequisite.md)
  for detailed network setup requirements

## Objectives

* Deploy the DPF Operator to your Kubernetes cluster
* Understand Host Trusted mode architecture and security model
* Verify successful installation and readiness for DPU provisioning
* Set up foundation for DPU acceleration in vanilla Kubernetes environments

## Understanding Host Trusted Mode

In Host Trusted mode:

* **The host is considered a trusted entity** within the data center network
* **DPUs are managed through in-band communication** over the host (out-of-band DPU interface is not used)
* **The host participates as a worker node** in the Kubernetes cluster
* **DPUs are provisioned and can run accelerated network services** (HBN, OVN-K, etc.)

## Deploy the DPF Operator

### 1. Add the DPF Helm Repository

First, add the NVIDIA DPF Helm repository to access the DPF Operator charts:

```bash
helm repo add --force-update dpf-repository https://helm.ngc.nvidia.com/nvidia/doca
helm repo update
```

### 2. Install the DPF Operator

> [!NOTE]
> Ensure you have completed the [Helm Prerequisites Guide](./helm-prerequisites.md) before proceeding with the DPF
> Operator installation.

Deploy the DPF Operator to your Kubernetes cluster using Helm:

```bash
helm upgrade --install -n dpf-operator-system dpf-operator dpf-repository/dpf-operator --version=v26.4.1
```

The command above does the following:

* Creates the `dpf-operator-system` namespace if it does not exist
* Installs the DPF Operator version v26.4.1 from the NVIDIA repository

### 3. Verify the Installation

Check that the DPF Operator has been successfully deployed and is running:

```bash
kubectl rollout status deployment --namespace dpf-operator-system dpf-operator-controller-manager
```

The output should be similar to:

```
deployment "dpf-operator-controller-manager" successfully rolled out
```

You can also verify all components are running:

```bash
kubectl get pods -n dpf-operator-system
```

## Next Steps

This minimal setup provides a foundation for DPF Host Trusted mode. To proceed with DPU provisioning and making the DPUs
act as passthrough devices, explore the guide
**[DPF Minimal (passthrough)](../user-guides/host-trusted/use-cases/passthrough/README.md)**. For further Host Trusted
mode use cases,
refer to the [DPF Host Trusted Use Cases](../user-guides/host-trusted/README.md) documentation.

## Cleanup (Optional)

If you need to remove the DPF Operator from your cluster:

**1.** Uninstall the DPF Operator:

```bash
helm uninstall dpf-operator -n dpf-operator-system
```

**2.** Remove the namespace:

```bash
kubectl delete ns dpf-operator-system
```
