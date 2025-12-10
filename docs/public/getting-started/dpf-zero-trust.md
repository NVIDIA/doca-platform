---
title: "DPF Zero Trust"
---

[TOC]

# Get Started with DPF Zero Trust

This guide shows you how to deploy the NVIDIA DPF Operator prepared for the Zero Trust mode, which is designed for
bare-metal infrastructure with NVIDIA BlueField-3 DPUs.

## Before you Begin

To follow this guide, you need the following:

* **A Kubernetes cluster** with administrative access for DPF Operator deployment
* **Bare-metal infrastructure** with NVIDIA DPUs and BMC access
* **Network access** to the NVIDIA NGC Catalog for downloading DPF Operator charts and container images

### Detailed Requirements

For detailed requirements, ensure your system meets these prerequisites:

* **System Prerequisites**: See the [DPF System Prerequisites](../user-guides/zero-trust/prerequisites/system.md) for
  complete hardware and system requirements.
* **Helm Dependencies**: See the [Helm Prerequisites Guide](./helm-prerequisites.md) for required Helm charts that must
  be installed before the DPF Operator.

## Objectives

* Deploy the DPF Operator to your Kubernetes cluster
* Understand Zero Trust mode architecture and security model
* Verify successful installation and readiness for DPU provisioning
* Set up foundation for secure bare-metal DPU management

## Understanding Zero Trust Mode

In Zero Trust mode:

* **The host is considered an untrusted entity** towards the data center network
* **DPUs are managed through their Baseboard Management Controller (BMC)** via Redfish protocol
* **All management traffic occurs over the DPU's out-of-band (OOB) network** for secure isolation
* **The DPU acts as a security barrier** between the host and the network infrastructure
* **The host sees the DPU as a standard NIC** with no access to the internal DPU management plane
* **DPUs are provisioned and can run accelerated network services** with hardware-level isolation

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
helm upgrade --install -n dpf-operator-system dpf-operator dpf-repository/dpf-operator --version=v25.7.0
```

The command above does the following:

* Creates the `dpf-operator-system` namespace if it doesn't exist
* Installs the DPF Operator version v25.7.0 from the NVIDIA repository
* Configures the operator to manage DPU resources across your cluster

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

## What's Next?

This minimal setup provides a foundation for DPF Zero Trust mode. To proceed with DPU provisioning and making the DPUs
act as passthrough devices, explore the
**[DPU Passthrough in DPF Zero Trust](../user-guides/zero-trust/use-cases/passthrough/README.md)** guide. For further
Zero Trust mode use cases, refer to the [DPF Zero Trust Use Cases](../user-guides/zero-trust/README.md) documentation.

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
