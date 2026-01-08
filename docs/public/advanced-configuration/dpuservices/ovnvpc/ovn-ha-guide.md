---
title: OVN High Availability (HA) guide
---

To achieve high availability (HA) for OVN, deploy the `ovn-central` DPUService with the `enableHA` option enabled.

### 0. Prerequisites

Requires at least three schedulable control-plane nodes for quorum.

Use `$HELM_REGISTRY_REPO_URL` and `TAG` to specify the Helm chart registry and your desired version respectively.

```sh
## The repository URL for the NVIDIA Helm chart registry.
## Usually this is the NVIDIA Helm NGC registry. For development purposes, this can be set to a different repository.
export HELM_REGISTRY_REPO_URL=https://helm.ngc.nvidia.com/nvidia/doca
## The DPF TAG is the version of the OVN components which will be deployed in this guide.
export TAG=v25.10.1
```

### 1. Deploy ovn-central DPUService

```shell
cat examples/dpuservice-ovn-central.yaml | envsubst | kubectl apply -f -
```

This will deploy `ovn-central` DPUService with `enableHA` set to `true`, it will create 3 ovn-central pods on the control-plane nodes.

[embedmd]:#(examples/dpuservice-ovn-central.yaml)
```yaml
---
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUService
metadata:
  name: ovn-central
  namespace: dpf-operator-system
spec:
  deployInCluster: true
  helmChart:
    source:
      repoURL: $HELM_REGISTRY_REPO_URL
      version: $TAG
      chart: ovn-chart
    values:
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
