---
title: "Alertmanager Example"
---

# DPF Alertmanager Example

[[_TOC_]]

The [alert rules](alerts.md) only produce
alerts; [Alertmanager](https://prometheus.io/docs/alerting/latest/alertmanager/) decides who hears about them. This page
provides a reference Alertmanager configuration tuned for the DPF alert taxonomy: every DPF alert carries
`service: doca-platform-framework` and `severity: critical|warning`. The component-health and lifecycle alerts identify
their subject through the `namespace` and `name` labels, while the aggregate alerts carry other identities (`cluster`
for the DPU-cluster control-plane alerts, `controller` for the reconcile alerts, `cluster`/`service_id` for replica
health, `pod` for the operator self-health alerts). The reference DPF install ships with Alertmanager disabled (
`alertmanager.enabled: false` in `deploy/helmfiles/values/kube-prometheus-stack.yaml`), so routing is an explicit
opt-in.

> [!NOTE]
> The receivers below are placeholders. Replace the webhook/Slack targets with your own endpoints before enabling; an
> Alertmanager without working receivers silently drops notifications. See the
> [Alertmanager integrations documentation](https://prometheus.io/docs/alerting/latest/integrations/) for the full list
> of supported notification targets.

## Enabling Alertmanager (kube-prometheus-stack)

Enable Alertmanager and set the configuration through the kube-prometheus-stack Helm values:

```yaml
alertmanager:
  enabled: true
  config:
    route:
      # Non-DPF alerts (e.g. the kube-prometheus-stack Watchdog and Kubernetes
      # system alerts) fall through to the default receiver.
      receiver: default
      group_by: [ "alertname", "namespace" ]
      group_wait: 30s
      group_interval: 5m
      repeat_interval: 12h
      routes:
        - matchers:
            - service = "doca-platform-framework"
          receiver: dpf-warning
          # Group per alert type: a provisioning wave that flips 50 DPUs to
          # Ready=False produces grouped DPFDPUNotReady notifications listing
          # the affected DPUs, not 50 separate pages.
          group_by: [ "alertname", "severity" ]
          routes:
            - matchers:
                - severity = "critical"
              receiver: dpf-critical
              # Critical pages repeat faster than warnings.
              repeat_interval: 4h

    inhibit_rules:
      # A critical alert on an object silences warning-level alerts for the
      # SAME object, e.g. DPFDPUClusterNotReady (critical) inhibits
      # DPFDPUClusterPhaseStuck (warning) for that cluster. Alertmanager treats
      # a missing label as an empty string when comparing `equal` labels, so
      # the =~ ".+" matchers restrict this rule to alerts that actually carry
      # an object identity. Without them, any critical alert lacking
      # namespace/name (e.g. DPFOperatorMetricsAbsent) would silence every
      # warning lacking those labels too (the DPU-cluster control-plane and
      # replica-health alerts), even though they watch unrelated systems.
      - source_matchers:
          - service = "doca-platform-framework"
          - severity = "critical"
          - namespace =~ ".+"
          - name =~ ".+"
        target_matchers:
          - service = "doca-platform-framework"
          - severity = "warning"
          - namespace =~ ".+"
          - name =~ ".+"
        equal: [ "namespace", "name" ]
      # The operator pod alerts identify by pod (not name), so the rule above
      # does not cover them. A pod that is not Ready is usually also
      # restarting; one page suffices.
      - source_matchers:
          - alertname = "DPFOperatorPodNotReady"
        target_matchers:
          - alertname = "DPFOperatorPodCrashLooping"
        equal: [ "namespace", "pod" ]

    receivers:
      - name: default
      - name: dpf-warning
        slack_configs:
          - api_url: https://hooks.slack.com/services/REPLACE/ME/PLEASE
            channel: "#dpf-alerts"
            send_resolved: true
            title: '{{ .CommonLabels.alertname }}: {{ .Alerts.Firing | len }} firing, {{ .Alerts.Resolved | len }} resolved'
            text: |-
              {{ range .Alerts.Firing }}FIRING: {{ .Annotations.summary }}
              {{ end }}{{ range .Alerts.Resolved }}RESOLVED: {{ .Annotations.summary }}
              {{ end }}
      - name: dpf-critical
        webhook_configs:
          # e.g. PagerDuty Events API proxy, Opsgenie, or an incident webhook.
          - url: https://REPLACE.ME/incident-webhook
            send_resolved: true
```

Apply by merging the block into your kube-prometheus-stack values (for the reference install:
`deploy/helmfiles/values/kube-prometheus-stack.yaml`, then re-apply `deploy/helmfiles/monitoring.yaml`).

## Routing rationale

* **`service = "doca-platform-framework"` as the routing key**: every DPF rule carries this label, so one matcher
  captures the whole set without enumerating alert names, including alerts you add later and alerts that carry no
  `namespace`/`name` identity.
* **Group by `alertname`, not by object**: DPU fleets change state in waves (BFB upgrades, host reboots, provisioning
  batches). Per-object grouping turns a planned 50-DPU re-flash into 50 notifications; per-alertname grouping collapses
  each wave into a handful of notifications listing the affected objects.
* **`group_wait: 30s`** only delays the very first notification of a new group so alerts that trip together are
  delivered together. Alerts in a wave rarely fire at the same instant (their `for:` windows range from 5 minutes to 1
  hour), so most of the batching comes from **`group_interval: 5m`**, which folds late arrivals into follow-up
  notifications instead of paging per object. **`repeat_interval`** re-pages unresolved criticals every 4 hours but
  re-sends warnings only every 12 hours.
* **Severity is the only escalation dimension**: `critical` means paging (service impact, immediate action), `warning`
  means a ticket or chat notification, consistent with the [severity policy](README.md#severity-policy).

## Maintenance windows

DPU re-flashing and host maintenance are routine operations that briefly trip the component-health alerts. Mute DPF
notifications during planned windows instead of lowering thresholds:

```yaml
alertmanager:
  config:
    time_intervals:
      - name: dpu-maintenance
        time_intervals:
          - weekdays: [ "saturday" ]
            times:
              - start_time: "02:00"
                end_time: "06:00"
    route:
      routes:
        - matchers:
            - service = "doca-platform-framework"
          mute_time_intervals: [ "dpu-maintenance" ]
          # ... receiver and child routes as above
```

For ad-hoc windows, prefer a silence (
`amtool silence add service=doca-platform-framework --duration 2h --comment "BFB upgrade wave"`) over editing the
configuration.

## AlertmanagerConfig CRD alternative

The Prometheus Operator also accepts namespaced `AlertmanagerConfig` resources (`monitoring.coreos.com/v1alpha1`) that
are merged into the global configuration. Be aware that by default the operator appends a `namespace` matcher to every
route from such a resource, so it only sees alerts originating from its own namespace, which does not fit the
cluster-scoped DPF alert set unless you set `alertmanagerConfigMatcherStrategy.type: None` on the Alertmanager resource.
For a single central routing policy like the one above, the global `alertmanager.config` values block is the simpler and
recommended path.

## Validating

Check the configuration and exercise the routing tree before rolling it out:

```bash
amtool check-config alertmanager.yaml

# Which receiver handles a critical DPF alert?
amtool config routes test --config.file=alertmanager.yaml \
  service=doca-platform-framework severity=critical alertname=DPFDPUClusterNotReady
# -> dpf-critical
```

Once deployed, `kubectl -n dpf-operator-system port-forward svc/kube-prometheus-stack-alertmanager 9093:9093` and open
`http://localhost:9093` to see grouped alerts, silences, and the routing status.
