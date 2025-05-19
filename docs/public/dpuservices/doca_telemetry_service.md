---
title: DOCA Telemetry Service
---

This documentation explains configuration and deployment of DOCA Telemetry service (DTS) as DPU service in DPF.

Main DTS concepts are explained in the [official DTS documentation](https://docs.nvidia.com/doca/sdk/doca+telemetry+service+guide/index.html).

While the official documentation provides a more comprehensive overview, DPUService users should consult it for its detailed list of telemetry counters and explanation of user options.

# Service configuration

[Official DTS documentation](https://docs.nvidia.com/doca/sdk/doca+telemetry+service+guide/index.html) explains configuration options.

General note: In the official documentation, all options should be specified in the `dts_config.ini` file using the `key=value` format.
In DPF the same options should be set via `DPUService.yaml` as `key: value` in yaml format.


## Default configuration

The default configuration is set to collect `sysfs` and `ethtool` counters and expose them via Prometheus endpoint.

## DPF configuration

User configuration is organized into the following groups:

* providers
* aggregator_providers
* general
* fluent

All user options should be set in DPUService under:

```
spec:
  helmChart:
    values:
      configMapData:
```

The following subsections explain each user option groups
### Providers:

Available data providers are listed here: [DTS providers](https://docs.nvidia.com/doca/sdk/doca+telemetry+service+guide/index.html#src-3355902696_id-.DOCATelemetryServiceGuidev2.10.0-Providers).

Note: some of the providers are supported only on host and not on DPU.

To enable data providers in DTS service:

1. Set data providers csv-line under `spec.helmChart.values.configMapData.providers`. E.g.:

```
spec:
  helmChart:
    values:
      configMapData:
        providers: "sysfs,ethtool"
```

2. Optionally, set aggregator providers csv-line under `spec.helmChart.values.configMapData.aggregator_providers`.

### General options

Except provider names, official documentation explains various options of `dts_config.ini` file in format of `key=value` pairs.

To set the these options in DPUService.yaml specify them in `key: value` pairs under `spec.helmChart.configMapData.general`.

For instance, DPU service exposes essential general options:

* [Sampling interval options](https://docs.nvidia.com/doca/sdk/doca+telemetry+service+guide/index.html#src-3355902696_id-.DOCATelemetryServiceGuidev2.10.0-SamplingInterval):
    * `update: 1000` to set the sample interval in milliseconds.
    * `sync-time-limit: 10000` buffer rotation time limit in seconds.


### Configure Fluent exporter

Additionally Fluent Bit export files can be set via `spec.helmChart.configMapData.fluent.DESTINATION`.
Available fluent export destinations are:

* forward
* kafka
* influxdb
* elasticsearch

Default values are exposed for each case in DPUService.yaml.
See more details regarding each destination in official documentation.

# Run Service

Start service with:

```
kubectl apply -f DPUService.yaml
```

Check the status of DPU service:

```
$ kubectl -n dpf-operator-system exec deploy/dpf-operator-controller-manager -- /dpfctl describe all --show-resources=dpuservice/doca-telemetry-service --show-conditions=all
NAME                                             NAMESPACE            READY  REASON   SINCE  MESSAGE
DPFOperatorConfig/dpfoperatorconfig              dpf-operator-system  True   Success  15h
│           ├─ImagePullSecretsReconciled                              True   Success  25d
│           ├─SystemComponentsReady                                   True   Success  15h
│           └─SystemComponentsReconciled                              True   Success  25d
└─DPUServices
  └─DPUService/doca-telemetry-service            dpf-operator-system  True   Success  21d
                ├─ApplicationPrereqsReconciled                        True   Success  25d
                ├─ApplicationsReady                                   True   Success  21d
                ├─ApplicationsReconciled                              True   Success  25d
                └─DPUServiceInterfaceReconciled                       True   Success  25d
```
