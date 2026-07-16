---
title: "Troubleshooting"
---

# Troubleshooting DOCA Platform Framework

This section provides comprehensive troubleshooting guidance for common issues you may encounter while deploying, configuring, or operating the DOCA Platform Framework (DPF).

## Quick Diagnostic Tools

### [DPF CLI (dpfctl)](dpfctl/README.md)
Command-line tool for visualizing, debugging, and troubleshooting DPU resources in Kubernetes. Essential for real-time visibility into resource states and conditions.

**Use when:**

* DPU provisioning is failing
* Need to understand resource dependencies
* Debugging component readiness issues

### [SOS Report Collection (dpfctl sosreport)](dpfctl/sosreport.md)
Collect system diagnostics from host and DPU cluster nodes for support cases.

**Use when:**

* Need detailed system information for support cases
* Investigating complex infrastructure issues
* Preparing diagnostic data for NVIDIA support

### [Diagnostic Dumps (dpfctl dump)](dpfctl/dump.md)

Collect diagnostic dumps from DPF infrastructure components, such as BlueField BMC diagnostic dumps.

**Use when:**

* A Zero Trust or BMC-driven workflow fails and BMC-side diagnostics are required
* Kubernetes resources do not contain enough information to debug a provisioning or reboot issue
* You need Redfish task artifacts and the raw BMC dump archive for escalation

### [Using the DOCA Debug Image](doca-debug-image.md)
How to start a privileged DOCA debug container with host networking and use release-aligned DOCA tooling for debugging.

**Use when:**

* You need DOCA release-aligned debug tooling, such as `mstflint`, available from a container
* You need to run debug tools with host networking

## DPU Cluster

### [Accessing the Kamaji DPU Cluster](kamaji-cluster-access.md)
How to retrieve the admin kubeconfig for a Kamaji-backed `DPUCluster` when direct cluster access is needed for advanced troubleshooting.

**Use when:**

* You need to inspect workloads or nodes running inside the DPU cluster directly
* DPF-level status fields do not provide enough detail for a specific investigation

## Common Issues

### Service Function Chaining (SFC)
If a `ServiceChain` or `ServiceChainSet` is stuck at `Ready=False` or flapping between `Ready` and `Pending`, the most common cause is a `ServiceInterface` uniqueness conflict. See the [DPUServiceChain Constraints](../../developer-guides/api/dpuservicechain.md#constraints) section for detailed error messages, root causes, and resolution steps.

## Escalation Path

If you cannot resolve the issue using the guides above:

1. **Collect Diagnostic Information**
   * Collect a [sosreport](dpfctl/sosreport.md) for your environment
   * Collect a [diagnostic dump](dpfctl/dump.md) when BMC-side information is needed

2. **Check Known Issues**
   * Review [Release Notes](../../release-notes/README.md) for known issues
   * Search the [GitHub repository](https://github.com/NVIDIA/doca-platform/issues) for similar problems

3. **Contact Support**
   * Open an issue on the [GitHub repository](https://github.com/NVIDIA/doca-platform/issues)
   * Include diagnostic information and steps to reproduce
   * For enterprise customers, contact NVIDIA support with your diagnostic package

## Additional Resources

* **[User Guides](../../user-guides/README.md)** - Operational procedures and best practices
* **[Architecture](../../developer-guides/architecture/README.md)** - Understanding system design for better troubleshooting
* **[API Reference](../../developer-guides/api/README.md)** - Complete API documentation for debugging configurations
