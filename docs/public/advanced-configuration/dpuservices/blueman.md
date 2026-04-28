---
title: "DOCA Blueman Service"
---

## Overview
DOCA BlueMan is a standalone web dashboard that runs on the DPU, providing consolidated information about system health,
basic information, and telemetry counters through a single interface. All data is gathered from the DOCA Telemetry
Service (DTS).

## Prerequisites

* DTS service must be running
  * [DTS Guide](./doca-telemetry-service.md)

## Documentation

* [Official Blueman Documentation](https://docs.nvidia.com/doca/sdk/doca+blueman+service+guide/index.html)

## Deployment

### Starting the Service

To start the BlueMan service, you need to create a `DPUService` resource.  
This resource will define the service configuration and deployment details.

Configuration file:

<details markdown="1"><summary><b>DPUService</b></summary>

[embedmd]:#(../../../../dpuservices/blueman/DPUService.yaml)
```yaml
apiVersion: svc.dpu.nvidia.com/v1alpha1
kind: DPUService
metadata:
  name: doca-blueman-service
  namespace: dpf-operator-system
spec:
  helmChart:
    source:
      repoURL: https://helm.ngc.nvidia.com/nvidia/doca
      version: 1.0.8
      chart: doca-blueman
  serviceDaemonSet:
    updateStrategy:
      type: RollingUpdate
      rollingUpdate:
        maxUnavailable: 2
    labels:
      dpuservice.dpu.nvidia.com/name: doca-blueman-service
    annotations:
      dpuservice.dpu.nvidia.com/name: doca-blueman-service
```

</details>

```bash
kubectl apply -f DPUService.yaml
```

### Verification

```bash
kubectl -n dpf-operator-system exec deploy/dpf-operator-controller-manager -- /dpfctl describe all --show-resources=dpuservice/doca-blueman-service --show-conditions=dpuservices
NAME                                             NAMESPACE            STATUS       REASON   SINCE  MESSAGE
DPFOperatorConfig/dpfoperatorconfig              dpf-operator-system  Ready: True  Success  85m
└─DPUServices
  └─DPUService/doca-blueman-service              dpf-operator-system
                ├─Ready                                               True         Success  15m
                ├─ApplicationPrereqsReconciled                        True         Success  17m
                ├─ApplicationsReady                                   True         Success  15m
                ├─ApplicationsReconciled                              True         Success  17m
                ├─ConfigPortsReconciled                               True         Success  17m
                └─DPUServiceInterfaceReconciled                       True         Success  17m
```

## Network Configuration

### Accessing the UI

By default, the Blueman UI is not accessible from outside the DPF host. To enable external access, configure IP prerouting using the following commands:

```bash
iptables -t nat -A PREROUTING -p tcp --dport 10000 -j DNAT --to-destination $DPF_BF_IP:10000
iptables -t nat -A PREROUTING -p tcp --dport 443 -j DNAT --to-destination $DPF_BF_IP:443
```

### Accessing the Dashboard

Open your browser and navigate to:

```
https://$DPF_HOST_IP/login
```
