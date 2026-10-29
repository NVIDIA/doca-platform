---
title: "Troubleshooting"
---

# Troubleshooting DOCA Platform Framework

This section provides comprehensive troubleshooting guidance for common issues you may encounter while deploying, configuring, or operating the DOCA Platform Framework (DPF).

## Quick Diagnostic Tools

### 🔍 [DPF CLI (dpfctl)](dpfctl.md)
Command-line tool for visualizing, debugging, and troubleshooting DPU resources in Kubernetes. Essential for real-time visibility into resource states and conditions.

**Use when:**

* DPU provisioning is failing
* Need to understand resource dependencies
* Debugging component readiness issues

### 📊 [System Reports (sosreport)](sos-report.md)
Generate comprehensive system reports for deeper analysis and support requests.

**Use when:**

* Need detailed system information for support cases
* Investigating complex infrastructure issues
* Preparing diagnostic data for NVIDIA support

## Escalation Path

If you cannot resolve the issue using the guides above:

1. **Collect Diagnostic Information**
   * Generate a [sosreport](sos-report.md) for your environment

2. **Check Known Issues**
   * Review [Release Notes](../release-notes/README.md) for known issues
   * Search the [GitHub repository](https://github.com/NVIDIA/doca-platform/issues) for similar problems

3. **Contact Support**
   * Open an issue on the [GitHub repository](https://github.com/NVIDIA/doca-platform/issues)
   * Include diagnostic information and steps to reproduce
   * For enterprise customers, contact NVIDIA support with your diagnostic package

## Additional Resources

* **[User Guides](../user-guides/README.md)** - Operational procedures and best practices
* **[Architecture](../developer-guides/architecture/README.md)** - Understanding system design for better troubleshooting
* **[API Reference](../developer-guides/api/README.md)** - Complete API documentation for debugging configurations
