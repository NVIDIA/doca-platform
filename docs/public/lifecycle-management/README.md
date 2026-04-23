---
title: "Lifecycle Management"
---

# DPF Lifecycle Management

Guidance for managing the lifecycle of your DOCA Platform Framework (DPF) deployment.

## DPF Operator Lifecycle

When a new major version is released:

* New major version is supported
* Previous major version enters EOL (end-of-life)

**Upgrade Policy**: DPF follows an **n-1 upgrade policy**. You can upgrade from the previous major version to the
current major version. Patch upgrades are always supported. Skipping major versions is not supported.

## Ensuring Deployment Readiness

Use [`dpfctl`](../troubleshooting/dpfctl/README.md) to verify that your DPFOperatorConfig and all DPU resources are in a
`Ready` state before performing maintenance operations.

## DPF Operator Upgrade

**[DPF Operator Upgrade Guide](upgrade.md)** - Detailed instructions for upgrading DPF Operator with automated
prevalidations and safe rollback capabilities.
