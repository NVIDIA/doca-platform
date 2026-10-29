---
title: "Enable Custom Certificate Authority for DMS"
---

[TOC]

This document provides instructions on how to configure DPF to use a custom Certificate Authority (CA) for mutual TLS
(mTLS) authentication between the provisioning controller and DOCA Management Service (DMS) in non-Kubernetes(only Kubernetes
control plane) system. To enable mTLS, the user needs to create a Kubernetes Secret containing the required certificates
and keys, and configure the `DPFOperatorConfig` to use this Secret.

## Step 1: Prepare Certificates and Keys

Ensure have the following files ready:

* **Server Certificate**: A PEM-encoded certificate for the server (tls.crt).
* **Private Key**: A PEM-encoded private key corresponding to the server certificate (tls.key).
* **CA Certificate**: A PEM-encoded certificate for the custom Certificate Authority (ca.crt).

## Step 2: Create a Kubernetes Secret

Create a Kubernetes Secret of type `kubernetes.io/tls` that includes the `tls.crt`, `tls.key`, and `ca.crt` fields in
`dpf-operator-system` namespace.

```bash
kubectl create secret tls custom-ca-secret  --cert=tls.crt --key=tls.key --certificate-authority=ca.crt -n dpf-operator-system
```

## Step 3: Configure DPFOperatorConfig

When creating or updating the `DPFOperatorConfig`, specify the name of the Secret created in the previous step.

### Example DPFOperatorConfig Configuration

In the `DPFOperatorConfig` configuration, set the `customCASecretName` field to the name of the Secret (e.g., custom-ca-secret):

```yaml
apiVersion: operator.dpu.nvidia.com/v1alpha1
kind: DPFOperatorConfig
metadata:
  name: dpfoperatorconfig
  namespace: dpf-operator-system
spec:
  provisioningController:
    bfbPVCName: "bfb-pvc"
    customCASecretName: "custom-ca-secret"
  kamajiClusterManager: {}
```
