---
title: "Prometheus Rules"
---

# DPF Prometheus Rules

[[_TOC_]]

## Introduction

This page introduces two sets of Prometheus rules, alert rules and recording rules, that complement the [Grafana dashboards](../dashboards/README.md) shipped with the dpf-operator Helm chart. **DPF does not ship these rules as part of the Helm chart.** They are reference `PrometheusRule` manifests living in [`deploy/helmfiles/prometheus-rules/`](https://github.com/NVIDIA/doca-platform/tree/public-main/deploy/helmfiles/prometheus-rules), deployed alongside the `kube-prometheus-stack` install: the `deploy/helmfiles/monitoring.yaml` helmfile applies them automatically, or apply them to any Prometheus Operator deployment manually with `kubectl apply --server-side -f deploy/helmfiles/prometheus-rules/`.

## Where to place rules

The manifests are `PrometheusRule` custom resources (`monitoring.coreos.com/v1`) that must land in a namespace your Prometheus instance watches. If you use the reference DPF install that means `dpf-operator-system`. A minimal skeleton looks like:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: <group-name>
  namespace: dpf-operator-system
  labels:
    app.kubernetes.io/part-of: doca-platform-framework
    # Required if your Prometheus selects rules by a specific label. The default
    # kube-prometheus-stack install uses `release=kube-prometheus-stack`.
    release: kube-prometheus-stack
spec:
  groups:
    - name: <group-name>
      rules: []
```

> [!NOTE]
> If the rules do not load, check your Prometheus instance's `ruleSelector` and `ruleNamespaceSelector`. The `PrometheusRule` labels must match the selector your Prometheus is configured with, otherwise the Prometheus Operator will silently skip the resource.

## Naming conventions

* **Alert names**: CamelCase, prefixed with `DPF` so the whole set is filterable by name (`DPFDPUNotReady`, `DPFDPUClusterAPIServerLatencyHigh`, `DPFOperatorPodNotReady`).
* **Recording-rule names**: lowercase, using the Prometheus community `<level>:<metric>:<operation>` shape, namespaced under `dpf:` (`dpf:dpu:ready`, `dpf:dpucluster:apiserver_request_duration:p99`).
* **Labels**: every rule carries `severity: critical|warning` and `service: doca-platform-framework`, so alerts and series can be filtered consistently in Prometheus, Grafana, and Alertmanager routes (`ALERTS{service="doca-platform-framework"}`, `dpf:*{service="doca-platform-framework"}`).

## Severity policy

| Severity   | Use when |
|------------|----------|
| `critical` | Service is impacted, pages someone, requires immediate action. |
| `warning`  | Degraded state or stuck reconcile, but no immediate user impact. |

## Documentation

* [Alert rule examples](alerts.md)
* [Recording rule examples](recording-rules.md)
