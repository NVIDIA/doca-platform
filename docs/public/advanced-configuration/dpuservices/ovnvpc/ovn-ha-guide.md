---
title: OVN High Availability (HA) Guide
---

To achieve high availability (HA) for OVN, deploy the `ovn-central` DPUService with the `enableHA` option enabled.

Assuming the deployment of all the DPF VPC OVN components are done based on the following guide.
[VPC OVN Deployment](../../../user-guides/zero-trust/use-cases/vpc/README.md)

### 0. Prerequisites

Requires at least three schedulable control-plane nodes for quorum.

Use `$HELM_REGISTRY_REPO_URL` and `TAG` to specify the Helm chart registry and your desired version respectively.

```sh
## The repository URL for the NVIDIA Helm chart registry.
## Usually this is the NVIDIA Helm NGC registry. For development purposes, this can be set to a different repository.
export HELM_REGISTRY_REPO_URL=https://helm.ngc.nvidia.com/nvidia/doca
## The DPF TAG is the version of the OVN components which will be deployed in this guide.
export TAG=v26.4.1
```

### 1. Deploy ovn-central using Helm

```shell
helm upgrade --install -n dpf-operator-system ovn-central ovn-vpc-repository/ovn-chart \
  --version=$TAG --wait -f examples/ovn-central-ha-helm-values.yaml
```

This will deploy `ovn-central` with `enableHA` set to `true`, it will create 3 ovn-central pods on the control-plane nodes.

[embedmd]:#(examples/ovn-central-ha-helm-values.yaml)
```yaml
exposedPorts:
  ports:
    ovnnb: true
    ovnsb: true
management:
  ovnCentral:
    enabled: true
    enableHA: true
    affinity:
      nodeAffinity:
        requiredDuringSchedulingIgnoredDuringExecution:
          nodeSelectorTerms:
            - matchExpressions:
                - key: "node-role.kubernetes.io/master"
                  operator: Exists
            - matchExpressions:
                - key: "node-role.kubernetes.io/control-plane"
                  operator: Exists
    tolerations:
      - key: node-role.kubernetes.io/master
        operator: Exists
        effect: NoSchedule
      - key: node-role.kubernetes.io/control-plane
        operator: Exists
        effect: NoSchedule
```
