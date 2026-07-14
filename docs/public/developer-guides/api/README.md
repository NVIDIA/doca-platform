---
title: "API"
---


[[_TOC_]]

This section contains comprehensive API documentation for the DOCA Platform Framework (DPF).

Each API document provides detailed specifications, examples, and usage guidelines. For implementation details, refer
to the API source code in the repository.

## API Documentation Files

* [API](api.md) - Complete API reference with all CRD specifications
* [DPUFlavor](dpuflavor.md) - DPUFlavor CRD defining configuration templates for DPU system-level settings
* [DPUFlavorTemplate](dpuflavortemplate.md) - DPUFlavorTemplate CRD for per-DPU DPUFlavor rendering from a template using DPUDevice.spec.values
* [DPUService](dpuservice.md) - DPUService CRD for deploying applications on DPU nodes
* [DPUDeployment](dpudeployment.md) - DPUDeployment CRD for provisioning multiple DPUs with multiple DPUServices running
    on top of them
* [DPUServiceInterface](dpuserviceinterface.md) - DPUServiceInterface CRD for declaring DPU network interfaces used in
    service chaining
* [DPUServiceChain](dpuservicechain.md) - DPUServiceChain CRD for traffic steering and service chaining
* [DPUSet](dpuset.md) - DPUSet CRD for managing DPU provisioning
* [DPUCluster](dpucluster.md) - DPUCluster CRD for DPU cluster control plane management
* [DPFOperatorConfig](dpfoperatorconfig.md) - DPFOperatorConfig CRD for managing the operator configuration
* [DPUServiceNAD](dpuservicenad.md) - DPUServiceNAD CRD for enabling secondary networks on the DPUCluster
* [DPUServiceIPAM](dpuserviceipam.md) - DPUServiceIPAM CRD for IP address management of the DPUServices
* [DPUServiceCredentialRequest](dpuservice-credential-request.md) - DPUServiceCredentialRequest CRD for enabling
    cross Kubernetes cluster access

## DPU Management Resources

* [DPUDevice](dpudevice.md) - DPUDevice CRD for managing individual DPU hardware devices
* [DPUNode](dpunode.md) - DPUNode CRD for node-level DPU management and host operations
* [DPUDiscovery](dpudiscovery.md) - DPUDiscovery CRD for automatic discovery of DPU devices in IP ranges
