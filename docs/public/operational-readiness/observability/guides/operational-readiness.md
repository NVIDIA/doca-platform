---
title: "DPU Operational Readiness"
---

[[_TOC_]]

## Introduction

DPU resources expose health information across three condition sets that track different lifecycle phases.

## Condition Lifecycle

* **Provisioning Phase** (`status.conditions`): High-level provisioning lifecycle (Initializing → Installing → Rebooting → Ready). Indicates the overall progress of the DPU through provisioning stages.
* **DPU Agent Phase** (`status.agentStatus.conditions`): Tracks individual DPU agent operations during provisioning (network configuration, NVConfig, kernel modules, kubelet setup, etc.). This is the place to look when provisioning is stuck — the agent retries each operation until it succeeds, and conditions report the current state of each step.
* **Operational Phase** (`status.operationalConditions`): Tracks runtime health after provisioning completes. Aggregates signals from Node-Problem-Detector, DPUService pods, DPUServiceInterfaces, and DPUServiceChains into a single view of DPU operational readiness.

A DPU can be in `Ready` provisioning state but have `OperationalReady: False` if runtime issues occur (e.g., OVS service down, pod failures).

## Operational Condition Types

| Condition Type                   | Description                                              |
|----------------------------------|----------------------------------------------------------|
| `OperationalReady`               | Overall operational health (aggregate of all conditions) |
| `NodeProblemsReady`              | Node-level health from Node-Problem-Detector             |
| `DPUServiceCriticalPodsReady`    | Critical DPU service pods are running                    |
| `DPUServiceNonCriticalPodsReady` | Non-critical DPU service pods are running                |
| `DPUServiceInterfacesReady`      | Service interfaces are configured                        |
| `DPUServiceChainsReady`          | Service chains are configured                            |

## Viewing Operational Conditions

Check DPU operational health:

```bash
$ kubectl -n dpf-operator-system get dpu
NAME                   READY   OPERATIONAL   PHASE   AGE
worker1-mt2413xz0b67   True    True          Ready   73d
worker2-mt2413xz0b6w   True    True          Ready   73d
```

Or the `status.operationalConditions` field:

```bash
$ kubectl -n dpf-operator-system get dpu worker1-mt2413xz0b67 -oyaml | yq -P .status.operationalConditions
- lastTransitionTime: "2026-04-16T21:01:42Z"
  message: All node health checks passing (9 Conditions)
  reason: NoProblemsDetected
  status: "True"
  type: NodeProblemsReady
- lastTransitionTime: "2026-03-26T19:12:03Z"
  message: All critical Pods are ready (0)
  reason: AllPodsReady
  status: "True"
  type: DPUServiceCriticalPodsReady
- lastTransitionTime: "2026-04-16T21:01:42Z"
  message: All Pods are ready (4)
  reason: AllPodsReady
  status: "True"
  type: DPUServiceNonCriticalPodsReady
- lastTransitionTime: "2026-03-26T19:13:50Z"
  message: All ServiceInterfaces are ready (3)
  reason: AllServiceInterfacesReady
  status: "True"
  type: DPUServiceInterfacesReady
- lastTransitionTime: "2026-04-14T15:12:52Z"
  message: All ServiceChains are ready (1)
  reason: AllServiceChainsReady
  status: "True"
  type: DPUServiceChainsReady
- lastTransitionTime: "2026-04-16T21:01:42Z"
  message: All operational conditions are ready
  reason: AllReady
  status: "True"
  type: OperationalReady
```

## Alerting Example

Use operational conditions for alerting in Prometheus. This is the `DPFDPUNotOperational` rule from the [DPF alert rule examples](../prometheus-rules/alerts.md):

```yaml
- alert: DPFDPUNotOperational
  expr: max by(namespace, name) (dpu_operational_conditions{type="OperationalReady", status="False"}) == 1
  for: 5m
  labels:
    severity: critical
    service: doca-platform-framework
  annotations:
    summary: "DPU {{ $labels.namespace }}/{{ $labels.name }} is not OperationalReady"
    description: "DPU finished provisioning but its runtime is degraded (OperationalReady=False). Inspect the DPU agent, networking, and the DPU cluster node."
```
