---
title: VPC OVN upgrade guide
---

To achieve a zero‑downtime VPC OVN upgrade, upgrade the services in this order: vpc-ovn-node ovn-controller, ovn-central, then vpc-ovn-controller.

Assuming the deployment of all the DPF VPC OVN componenets are done based on the following guide.
[VPC OVN Deployment](../../../user-guides/zero-trust/use-cases/vpc/README.md)

### 0. Required variables

Use $TARGET_TAG as your desired version.

```sh
## The TARGET_TAG is the desired version of the DPF VPC OVN components which will be upgraded.
export TARGET_TAG=v25.10.1
```

### 1. Upgrade vpc-ovn-node service

Patch the `DPUServiceTemplate` of the vpc-ovn-node service with the upgraded chart version.

```shell
kubectl patch dpuservicetemplate vpc-ovn-node -n dpf-operator-system --type=json --patch="[{\"op\": \"replace\", \"path\": \"/spec/helmChart/source/version\", \"value\": \"$TARGET_TAG\"}]"
```

Ensure the `DPUDeployment` is Ready.

```shell
kubectl wait --for=condition=ready --namespace dpf-operator-system dpudeployment vpc-ovn
```

### 2. Upgrade ovn-controller service

Patch the `DPUServiceTemplate` of the ovn-controller service with the upgraded chart version.

```shell
kubectl patch dpuservicetemplate ovn-controller -n dpf-operator-system --type=json --patch="[{\"op\": \"replace\", \"path\": \"/spec/helmChart/source/version\", \"value\": \"$TARGET_TAG\"}]"
```

Ensure the `DPUDeployment` is Ready.

```shell
kubectl wait --for=condition=ready --namespace dpf-operator-system dpudeployment vpc-ovn
```

### 3. Upgrade ovn-central service

Patch the `DPUServiceTemplate` of the ovn-central service with the upgraded chart version.

```shell
kubectl patch dpuservicetemplate ovn-central -n dpf-operator-system --type=json --patch="[{\"op\": \"replace\", \"path\": \"/spec/helmChart/source/version\", \"value\": \"$TARGET_TAG\"}]"
```

Ensure the `DPUDeployment` is Ready.

```shell
kubectl wait --for=condition=ready --namespace dpf-operator-system dpudeployment vpc-ovn
```

### 4. Upgrade vpc-ovn-controller service

Patch the `DPUServiceTemplate` of the vpc-ovn-controller service with the upgraded chart version.

```shell
kubectl patch dpuservicetemplate vpc-ovn-controller -n dpf-operator-system --type=json --patch="[{\"op\": \"replace\", \"path\": \"/spec/helmChart/source/version\", \"value\": \"$TARGET_TAG\"}]"
```

Ensure the `DPUDeployment` is Ready.

```shell
kubectl wait --for=condition=ready --namespace dpf-operator-system dpudeployment vpc-ovn
```
